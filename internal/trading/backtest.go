// Package trading provides trading operations including backtesting.
package trading

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"zerodha-trader/internal/models"
	"zerodha-trader/internal/store"
)

// DefaultBacktestEngine implements the BacktestEngine interface.
type DefaultBacktestEngine struct {
	store store.DataStore
}

// NewBacktestEngine creates a new backtest engine.
func NewBacktestEngine(dataStore store.DataStore) *DefaultBacktestEngine {
	return &DefaultBacktestEngine{
		store: dataStore,
	}
}

// SignalGenerator is a function that generates trading signals from candles.
type SignalGenerator func(candles []models.Candle, index int) (signal string, confidence float64)

type backtestMetricsState struct {
	equity      float64
	maxDrawdown float64
}

// Run executes a backtest with the given configuration.
func (be *DefaultBacktestEngine) Run(ctx context.Context, config BacktestConfig) (*BacktestResult, error) {
	if err := be.validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Apply defaults
	config = be.applyDefaults(config)

	// Fetch historical data
	timeframe := config.Timeframe
	if timeframe == "" {
		timeframe = "1day"
	}
	candles, err := be.store.GetCandles(ctx, config.Symbol, timeframe, config.StartDate, config.EndDate)
	if err != nil {
		return nil, fmt.Errorf("fetching candles: %w", err)
	}
	if len(candles) < 50 {
		return nil, fmt.Errorf("insufficient data: need at least 50 candles, got %d", len(candles))
	}

	return be.RunOnCandles(ctx, config, candles)
}

// RunOnCandles executes a backtest on pre-fetched candle data.
// This is the core simulation loop, usable from both the engine and CLI.
func (be *DefaultBacktestEngine) RunOnCandles(ctx context.Context, config BacktestConfig, candles []models.Candle) (*BacktestResult, error) {
	return be.RunEventDrivenOnCandles(ctx, config, candles)
}

// calculatePositionSize determines how many shares to buy.
func (be *DefaultBacktestEngine) calculatePositionSize(capital, price float64, config BacktestConfig) int {
	maxPct := config.MaxPositionPercent
	if maxPct <= 0 || maxPct > 100 {
		maxPct = 95
	}
	availableCapital := capital * (maxPct / 100)
	if availableCapital <= 0 || price <= 0 {
		return 0
	}
	return int(availableCapital / price)
}

// warmupPeriod returns the number of bars needed before signals can fire.
func (be *DefaultBacktestEngine) warmupPeriod(strategy string, params map[string]interface{}) int {
	def, ok := DefaultStrategyRegistry().Definition(strategy)
	if !ok {
		return 0
	}
	return def.Warmup(params)
}

// getSignalGenerator returns a signal generator for the given strategy.
// All strategies use the real indicators from analysis/indicators.
func (be *DefaultBacktestEngine) getSignalGenerator(strategy string, params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	def, ok := DefaultStrategyRegistry().Definition(strategy)
	if !ok {
		return func(candles []models.Candle, index int) (string, float64) {
			return "HOLD", 0
		}
	}
	return def.Build(be, params, allCandles)
}

// calculateMetrics computes all performance metrics from the trade list and equity curve.
func (be *DefaultBacktestEngine) calculateMetrics(result *BacktestResult, initialCapital float64, state backtestMetricsState) {
	result.TotalTrades = len(result.Trades)
	if result.TotalTrades == 0 {
		result.EndCapital = state.equity
		return
	}

	var grossProfit, grossLoss float64
	var winHoldBars, lossHoldBars int
	var largestWin, largestLoss float64
	var consecutiveWins, consecutiveLosses int
	var maxConsecWins, maxConsecLosses int

	for _, trade := range result.Trades {
		result.TotalCosts += trade.TotalCosts
		result.TotalSlippage += trade.SlippageCost
		result.TotalTurnover += (trade.EntryPrice + trade.ExitPrice) * float64(trade.Quantity)
		if trade.PnL > 0 {
			result.WinningTrades++
			grossProfit += trade.PnL
			winHoldBars += trade.HoldBars
			if trade.PnL > largestWin {
				largestWin = trade.PnL
			}
			consecutiveWins++
			consecutiveLosses = 0
			if consecutiveWins > maxConsecWins {
				maxConsecWins = consecutiveWins
			}
		} else {
			result.LosingTrades++
			grossLoss += trade.PnL // negative
			lossHoldBars += trade.HoldBars
			if trade.PnL < largestLoss {
				largestLoss = trade.PnL
			}
			consecutiveLosses++
			consecutiveWins = 0
			if consecutiveLosses > maxConsecLosses {
				maxConsecLosses = consecutiveLosses
			}
		}
	}

	result.GrossProfit = grossProfit
	result.GrossLoss = grossLoss
	result.NetProfit = grossProfit + grossLoss
	result.LargestWin = largestWin
	result.LargestLoss = largestLoss
	result.MaxConsecutiveWins = maxConsecWins
	result.MaxConsecutiveLosses = maxConsecLosses
	result.EndCapital = state.equity

	// Total return
	result.TotalReturn = (state.equity - initialCapital) / initialCapital * 100

	// Annualized return
	if len(result.EquityCurve) > 1 {
		first := result.EquityCurve[0].Timestamp
		last := result.EquityCurve[len(result.EquityCurve)-1].Timestamp
		days := last.Sub(first).Hours() / 24
		if days > 0 {
			years := days / 365.25
			if state.equity > 0 && initialCapital > 0 {
				result.AnnualizedReturn = (math.Pow(state.equity/initialCapital, 1/years) - 1) * 100
			}
		}
	}

	// Win rate
	result.WinRate = float64(result.WinningTrades) / float64(result.TotalTrades) * 100

	// Max drawdown
	result.MaxDrawdown = state.maxDrawdown * 100

	// Average win/loss
	if result.WinningTrades > 0 {
		result.AvgWin = grossProfit / float64(result.WinningTrades)
	}
	if result.LosingTrades > 0 {
		result.AvgLoss = grossLoss / float64(result.LosingTrades)
	}

	// Profit factor
	if grossLoss != 0 {
		result.ProfitFactor = grossProfit / math.Abs(grossLoss)
	}

	// Expectancy per trade
	result.Expectancy = result.NetProfit / float64(result.TotalTrades)

	// Average hold bars
	totalBars := 0
	for _, t := range result.Trades {
		totalBars += t.HoldBars
	}
	result.AvgHoldBars = totalBars / result.TotalTrades
	if result.WinningTrades > 0 {
		result.AvgWinHoldBars = winHoldBars / result.WinningTrades
	}
	if result.LosingTrades > 0 {
		result.AvgLossHoldBars = lossHoldBars / result.LosingTrades
	}

	// Sharpe ratio (proper: annualized mean excess return / annualized std dev)
	result.SharpeRatio = be.calculateSharpeRatio(result.EquityCurve, initialCapital)

	// Sortino ratio (penalizes only downside volatility)
	result.SortinoRatio = be.calculateSortinoRatio(result.EquityCurve, initialCapital)

	// Calmar ratio (annualized return / max drawdown)
	if result.MaxDrawdown > 0 {
		result.CalmarRatio = result.AnnualizedReturn / result.MaxDrawdown
	}
}

// calculateSharpeRatio computes the annualized Sharpe ratio from the equity curve.
func (be *DefaultBacktestEngine) calculateSharpeRatio(equityCurve []EquityPoint, initialCapital float64) float64 {
	if len(equityCurve) < 2 {
		return 0
	}

	returns := make([]float64, len(equityCurve)-1)
	for i := 1; i < len(equityCurve); i++ {
		if equityCurve[i-1].Equity > 0 {
			returns[i-1] = (equityCurve[i].Equity - equityCurve[i-1].Equity) / equityCurve[i-1].Equity
		}
	}

	meanReturn := sliceMean(returns)
	stdDev := sliceStdDev(returns)

	if stdDev == 0 {
		return 0
	}

	riskFreeDaily := 0.065 / 252 // 6.5% Indian risk-free rate (T-bill proxy)
	return (meanReturn - riskFreeDaily) / stdDev * math.Sqrt(252)
}

// calculateSortinoRatio computes the annualized Sortino ratio (downside deviation only).
func (be *DefaultBacktestEngine) calculateSortinoRatio(equityCurve []EquityPoint, initialCapital float64) float64 {
	if len(equityCurve) < 2 {
		return 0
	}

	returns := make([]float64, len(equityCurve)-1)
	for i := 1; i < len(equityCurve); i++ {
		if equityCurve[i-1].Equity > 0 {
			returns[i-1] = (equityCurve[i].Equity - equityCurve[i-1].Equity) / equityCurve[i-1].Equity
		}
	}

	meanReturn := sliceMean(returns)
	riskFreeDaily := 0.065 / 252

	// Downside deviation: std dev of returns below the risk-free rate
	var sumSqNeg float64
	var countNeg int
	for _, r := range returns {
		if r < riskFreeDaily {
			diff := r - riskFreeDaily
			sumSqNeg += diff * diff
			countNeg++
		}
	}

	if countNeg == 0 {
		return 0
	}

	downsideDev := math.Sqrt(sumSqNeg / float64(len(returns)))
	if downsideDev == 0 {
		return 0
	}

	return (meanReturn - riskFreeDaily) / downsideDev * math.Sqrt(252)
}

// Validation and defaults

func (be *DefaultBacktestEngine) validateConfig(config BacktestConfig) error {
	if err := be.validateSimulationConfig(config); err != nil {
		return err
	}
	if config.StartDate.IsZero() {
		return fmt.Errorf("start date is required")
	}
	if config.EndDate.IsZero() {
		return fmt.Errorf("end date is required")
	}
	if config.EndDate.Before(config.StartDate) {
		return fmt.Errorf("end date must be after start date")
	}
	return nil
}

func (be *DefaultBacktestEngine) validateSimulationConfig(config BacktestConfig) error {
	if config.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if config.InitialCapital <= 0 {
		return fmt.Errorf("initial capital must be positive")
	}
	if config.Strategy != "" {
		if _, ok := DefaultStrategyRegistry().Definition(config.Strategy); !ok {
			return fmt.Errorf("unknown strategy %q (available: %s)", config.Strategy, strings.Join(AvailableStrategies(), ", "))
		}
	}
	if config.ExecutionTiming != "" && config.ExecutionTiming != "next_open" && config.ExecutionTiming != "same_close" {
		return fmt.Errorf("execution_timing must be next_open or same_close")
	}
	if config.MaxFillVolumePercent < 0 || config.MaxFillVolumePercent > 100 {
		return fmt.Errorf("max_fill_volume_percent must be between 0 and 100")
	}
	return nil
}

func (be *DefaultBacktestEngine) applyDefaults(config BacktestConfig) BacktestConfig {
	if config.Strategy == "" {
		config.Strategy = "ema_crossover"
	} else {
		config.Strategy = NormalizeStrategyName(config.Strategy)
	}
	if config.Timeframe == "" {
		config.Timeframe = "1day"
	}
	if config.ExecutionTiming == "" {
		config.ExecutionTiming = "next_open"
	}
	if config.Slippage == 0 {
		config.Slippage = 0.001 // 0.1%
	}
	if config.Commission == 0 && config.BrokerageRate == 0 {
		config.Commission = 0.0003 // 0.03% (Zerodha intraday)
	}
	if config.ExchangeFeeRate == 0 {
		config.ExchangeFeeRate = 0.0000297
	}
	if config.SEBIRate == 0 {
		config.SEBIRate = 0.000001
	}
	if config.STTRate == 0 {
		config.STTRate = 0.00025
	}
	if config.StampDutyRate == 0 {
		config.StampDutyRate = 0.00003
	}
	if config.GSTRate == 0 {
		config.GSTRate = 0.18
	}
	if config.MaxPositionPercent == 0 {
		if def, ok := DefaultStrategyRegistry().Definition(config.Strategy); ok && def.Risk.DefaultMaxPositionPercent > 0 {
			config.MaxPositionPercent = def.Risk.DefaultMaxPositionPercent
		} else {
			config.MaxPositionPercent = 95
		}
	}
	if config.Parameters == nil {
		config.Parameters = make(map[string]interface{})
	}
	return config
}

// Helper functions

func sliceMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var s float64
	for _, v := range values {
		s += v
	}
	return s / float64(len(values))
}

func sliceStdDev(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	m := sliceMean(values)
	var variance float64
	for _, v := range values {
		diff := v - m
		variance += diff * diff
	}
	variance /= float64(len(values))
	return math.Sqrt(variance)
}

func getIntParam(params map[string]interface{}, key string, defaultVal int) int {
	if params == nil {
		return defaultVal
	}
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case float64:
			return int(val)
		}
	}
	return defaultVal
}

func getFloatParam(params map[string]interface{}, key string, defaultVal float64) float64 {
	if params == nil {
		return defaultVal
	}
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		}
	}
	return defaultVal
}

// GenerateEquityCurveASCII generates an ASCII chart of the equity curve.
func (be *DefaultBacktestEngine) GenerateEquityCurveASCII(result *BacktestResult, width, height int) string {
	if len(result.EquityCurve) == 0 {
		return "No data to display"
	}

	minEquity := result.EquityCurve[0].Equity
	maxEquity := result.EquityCurve[0].Equity
	for _, point := range result.EquityCurve {
		if point.Equity < minEquity {
			minEquity = point.Equity
		}
		if point.Equity > maxEquity {
			maxEquity = point.Equity
		}
	}

	equityRange := maxEquity - minEquity
	if equityRange == 0 {
		equityRange = 1
	}
	minEquity -= equityRange * 0.05
	maxEquity += equityRange * 0.05
	equityRange = maxEquity - minEquity

	grid := make([][]rune, height)
	for i := range grid {
		grid[i] = make([]rune, width)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}

	step := len(result.EquityCurve) / width
	if step == 0 {
		step = 1
	}

	for x := 0; x < width && x*step < len(result.EquityCurve); x++ {
		point := result.EquityCurve[x*step]
		y := int((point.Equity - minEquity) / equityRange * float64(height-1))
		if y >= 0 && y < height {
			grid[height-1-y][x] = '\u2588'
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Equity Curve (%.0f - %.0f)\n", minEquity, maxEquity))
	sb.WriteString(strings.Repeat("\u2500", width+2) + "\n")

	for _, row := range grid {
		sb.WriteRune('\u2502')
		sb.WriteString(string(row))
		sb.WriteRune('\u2502')
		sb.WriteRune('\n')
	}

	sb.WriteString(strings.Repeat("\u2500", width+2) + "\n")
	return sb.String()
}

// CompareStrategies compares backtest results across different strategies.
func (be *DefaultBacktestEngine) CompareStrategies(results map[string]*BacktestResult) []StrategyComparison {
	var comparisons []StrategyComparison

	for name, result := range results {
		comparisons = append(comparisons, StrategyComparison{
			Strategy:         name,
			TotalReturn:      result.TotalReturn,
			AnnualizedReturn: result.AnnualizedReturn,
			WinRate:          result.WinRate,
			MaxDrawdown:      result.MaxDrawdown,
			SharpeRatio:      result.SharpeRatio,
			SortinoRatio:     result.SortinoRatio,
			CalmarRatio:      result.CalmarRatio,
			TotalTrades:      result.TotalTrades,
			ProfitFactor:     result.ProfitFactor,
			Expectancy:       result.Expectancy,
			MaxConsecLosses:  result.MaxConsecutiveLosses,
		})
	}

	sort.Slice(comparisons, func(i, j int) bool {
		return comparisons[i].SharpeRatio > comparisons[j].SharpeRatio
	})

	return comparisons
}

// StrategyComparison represents a comparison of strategy performance.
type StrategyComparison struct {
	Strategy         string
	TotalReturn      float64
	AnnualizedReturn float64
	WinRate          float64
	MaxDrawdown      float64
	SharpeRatio      float64
	SortinoRatio     float64
	CalmarRatio      float64
	TotalTrades      int
	ProfitFactor     float64
	Expectancy       float64
	MaxConsecLosses  int
}
