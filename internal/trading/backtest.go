// Package trading provides trading operations including backtesting.
package trading

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"zerodha-trader/internal/models"
	"zerodha-trader/internal/store"
)

// DefaultBacktestEngine implements the BacktestEngine interface.
// Requirements: 37.1-37.8
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

// Run executes a backtest with the given configuration.
// Requirements: 37.1-37.8
func (be *DefaultBacktestEngine) Run(ctx context.Context, config BacktestConfig) (*BacktestResult, error) {
	// Validate config
	if err := be.validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Fetch historical data
	candles, err := be.store.GetCandles(ctx, config.Symbol, "1day", config.StartDate, config.EndDate)
	if err != nil {
		return nil, fmt.Errorf("fetching candles: %w", err)
	}

	if len(candles) < 20 {
		return nil, fmt.Errorf("insufficient data: need at least 20 candles, got %d", len(candles))
	}

	// Initialize result
	result := &BacktestResult{
		EquityCurve: make([]EquityPoint, 0),
		Trades:      make([]BacktestTrade, 0),
	}

	// Initialize state
	state := &backtestState{
		capital:     config.InitialCapital,
		equity:      config.InitialCapital,
		position:    0,
		entryPrice:  0,
		entryTime:   time.Time{},
		peakEquity:  config.InitialCapital,
		maxDrawdown: 0,
	}

	// Get signal generator for strategy
	signalGen := be.getSignalGenerator(config.Strategy, config.Parameters)

	// Run simulation
	// Requirement 37.2: THE backtest mode SHALL simulate trades based on agent decisions using historical prices
	for i := 20; i < len(candles); i++ { // Start after warmup period
		candle := candles[i]

		// Generate signal
		signal, confidence := signalGen(candles[:i+1], i)

		// Process signal
		trade := be.processSignal(state, signal, confidence, candle, config)
		if trade != nil {
			result.Trades = append(result.Trades, *trade)
		}

		// Update equity curve
		currentEquity := state.capital
		if state.position != 0 {
			// Mark to market
			currentEquity += float64(state.position) * (candle.Close - state.entryPrice)
		}
		state.equity = currentEquity

		// Track drawdown
		if currentEquity > state.peakEquity {
			state.peakEquity = currentEquity
		}
		drawdown := (state.peakEquity - currentEquity) / state.peakEquity
		if drawdown > state.maxDrawdown {
			state.maxDrawdown = drawdown
		}

		result.EquityCurve = append(result.EquityCurve, EquityPoint{
			Timestamp: candle.Timestamp,
			Equity:    currentEquity,
		})
	}

	// Close any open position at the end
	if state.position != 0 {
		lastCandle := candles[len(candles)-1]
		trade := be.closePosition(state, lastCandle, config, "end_of_backtest")
		if trade != nil {
			result.Trades = append(result.Trades, *trade)
		}
	}

	// Calculate metrics
	// Requirement 37.3: THE backtest mode SHALL calculate: total return, win rate, max drawdown, Sharpe ratio
	be.calculateMetrics(result, config.InitialCapital, state)

	return result, nil
}

// backtestState holds the state during backtesting.
type backtestState struct {
	capital     float64
	equity      float64
	position    int
	entryPrice  float64
	entryTime   time.Time
	peakEquity  float64
	maxDrawdown float64
}

// validateConfig validates the backtest configuration.
func (be *DefaultBacktestEngine) validateConfig(config BacktestConfig) error {
	if config.Symbol == "" {
		return fmt.Errorf("symbol is required")
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
	if config.InitialCapital <= 0 {
		return fmt.Errorf("initial capital must be positive")
	}
	return nil
}

// getSignalGenerator returns a signal generator for the given strategy.
func (be *DefaultBacktestEngine) getSignalGenerator(strategy string, params map[string]interface{}) SignalGenerator {
	switch strings.ToLower(strategy) {
	case "sma_crossover":
		return be.smaCrossoverStrategy(params)
	case "rsi_oversold":
		return be.rsiOversoldStrategy(params)
	case "macd":
		return be.macdStrategy(params)
	default:
		// Default to SMA crossover
		return be.smaCrossoverStrategy(params)
	}
}

// smaCrossoverStrategy implements a simple SMA crossover strategy.
func (be *DefaultBacktestEngine) smaCrossoverStrategy(params map[string]interface{}) SignalGenerator {
	shortPeriod := 10
	longPeriod := 20

	if p, ok := params["short_period"].(int); ok {
		shortPeriod = p
	}
	if p, ok := params["long_period"].(int); ok {
		longPeriod = p
	}

	return func(candles []models.Candle, index int) (string, float64) {
		if index < longPeriod {
			return "HOLD", 0
		}

		shortSMA := be.calculateSMA(candles, index, shortPeriod)
		longSMA := be.calculateSMA(candles, index, longPeriod)
		prevShortSMA := be.calculateSMA(candles, index-1, shortPeriod)
		prevLongSMA := be.calculateSMA(candles, index-1, longPeriod)

		// Crossover detection
		if prevShortSMA <= prevLongSMA && shortSMA > longSMA {
			return "BUY", 70
		}
		if prevShortSMA >= prevLongSMA && shortSMA < longSMA {
			return "SELL", 70
		}

		return "HOLD", 0
	}
}

// rsiOversoldStrategy implements an RSI oversold/overbought strategy.
func (be *DefaultBacktestEngine) rsiOversoldStrategy(params map[string]interface{}) SignalGenerator {
	period := 14
	oversold := 30.0
	overbought := 70.0

	if p, ok := params["period"].(int); ok {
		period = p
	}
	if p, ok := params["oversold"].(float64); ok {
		oversold = p
	}
	if p, ok := params["overbought"].(float64); ok {
		overbought = p
	}

	return func(candles []models.Candle, index int) (string, float64) {
		if index < period+1 {
			return "HOLD", 0
		}

		rsi := be.calculateRSI(candles, index, period)
		prevRSI := be.calculateRSI(candles, index-1, period)

		// Buy when RSI crosses above oversold
		if prevRSI <= oversold && rsi > oversold {
			return "BUY", 65
		}
		// Sell when RSI crosses below overbought
		if prevRSI >= overbought && rsi < overbought {
			return "SELL", 65
		}

		return "HOLD", 0
	}
}

// macdStrategy implements a MACD crossover strategy.
func (be *DefaultBacktestEngine) macdStrategy(params map[string]interface{}) SignalGenerator {
	fastPeriod := 12
	slowPeriod := 26
	signalPeriod := 9

	if p, ok := params["fast_period"].(int); ok {
		fastPeriod = p
	}
	if p, ok := params["slow_period"].(int); ok {
		slowPeriod = p
	}
	if p, ok := params["signal_period"].(int); ok {
		signalPeriod = p
	}

	return func(candles []models.Candle, index int) (string, float64) {
		if index < slowPeriod+signalPeriod {
			return "HOLD", 0
		}

		macd, signal := be.calculateMACD(candles, index, fastPeriod, slowPeriod, signalPeriod)
		prevMACD, prevSignal := be.calculateMACD(candles, index-1, fastPeriod, slowPeriod, signalPeriod)

		// MACD crossover
		if prevMACD <= prevSignal && macd > signal {
			return "BUY", 75
		}
		if prevMACD >= prevSignal && macd < signal {
			return "SELL", 75
		}

		return "HOLD", 0
	}
}

// processSignal processes a trading signal and returns a trade if executed.
func (be *DefaultBacktestEngine) processSignal(state *backtestState, signal string, confidence float64, candle models.Candle, config BacktestConfig) *BacktestTrade {
	// Apply slippage to execution price
	// Requirement 37.6: THE backtest mode SHALL account for slippage and transaction costs
	slippage := config.Slippage
	if slippage == 0 {
		slippage = 0.001 // Default 0.1% slippage
	}

	switch signal {
	case "BUY":
		if state.position <= 0 {
			// Close short position if any
			var closeTrade *BacktestTrade
			if state.position < 0 {
				closeTrade = be.closePosition(state, candle, config, "signal_reversal")
			}

			// Open long position
			executionPrice := candle.Close * (1 + slippage)
			positionSize := be.calculatePositionSize(state.capital, executionPrice, config)

			if positionSize > 0 {
				commission := executionPrice * float64(positionSize) * config.Commission
				state.capital -= commission
				state.position = positionSize
				state.entryPrice = executionPrice
				state.entryTime = candle.Timestamp
			}

			return closeTrade
		}

	case "SELL":
		if state.position >= 0 {
			// Close long position if any
			var closeTrade *BacktestTrade
			if state.position > 0 {
				closeTrade = be.closePosition(state, candle, config, "signal_reversal")
			}

			// Open short position (if allowed)
			// For simplicity, we only go long in this implementation
			return closeTrade
		}
	}

	return nil
}

// closePosition closes the current position and returns the trade.
func (be *DefaultBacktestEngine) closePosition(state *backtestState, candle models.Candle, config BacktestConfig, reason string) *BacktestTrade {
	if state.position == 0 {
		return nil
	}

	slippage := config.Slippage
	if slippage == 0 {
		slippage = 0.001
	}

	var exitPrice float64
	var side string
	if state.position > 0 {
		exitPrice = candle.Close * (1 - slippage) // Sell at lower price
		side = "LONG"
	} else {
		exitPrice = candle.Close * (1 + slippage) // Buy to cover at higher price
		side = "SHORT"
	}

	qty := state.position
	if qty < 0 {
		qty = -qty
	}

	pnl := float64(state.position) * (exitPrice - state.entryPrice)
	commission := exitPrice * float64(qty) * config.Commission
	pnl -= commission

	pnlPercent := (exitPrice - state.entryPrice) / state.entryPrice * 100
	if state.position < 0 {
		pnlPercent = -pnlPercent
	}

	trade := &BacktestTrade{
		EntryTime:  state.entryTime,
		ExitTime:   candle.Timestamp,
		Symbol:     config.Symbol,
		Side:       side,
		EntryPrice: state.entryPrice,
		ExitPrice:  exitPrice,
		Quantity:   qty,
		PnL:        pnl,
		PnLPercent: pnlPercent,
	}

	// Update state
	state.capital += pnl + (state.entryPrice * float64(qty)) // Return capital + P&L
	state.position = 0
	state.entryPrice = 0
	state.entryTime = time.Time{}

	return trade
}

// calculatePositionSize calculates position size based on capital.
func (be *DefaultBacktestEngine) calculatePositionSize(capital, price float64, config BacktestConfig) int {
	// Use 95% of capital for position
	availableCapital := capital * 0.95
	return int(availableCapital / price)
}

// calculateMetrics calculates backtest performance metrics.
// Requirement 37.3: Calculate total return, win rate, max drawdown, Sharpe ratio
func (be *DefaultBacktestEngine) calculateMetrics(result *BacktestResult, initialCapital float64, state *backtestState) {
	result.TotalTrades = len(result.Trades)

	if result.TotalTrades == 0 {
		return
	}

	// Calculate win/loss stats
	var totalPnL float64
	var wins, losses []float64

	for _, trade := range result.Trades {
		totalPnL += trade.PnL
		if trade.PnL > 0 {
			result.WinningTrades++
			wins = append(wins, trade.PnL)
		} else {
			result.LosingTrades++
			losses = append(losses, trade.PnL)
		}
	}

	// Total return
	result.TotalReturn = (state.equity - initialCapital) / initialCapital * 100

	// Annualized return
	if len(result.EquityCurve) > 1 {
		days := result.EquityCurve[len(result.EquityCurve)-1].Timestamp.Sub(result.EquityCurve[0].Timestamp).Hours() / 24
		if days > 0 {
			years := days / 365
			result.AnnualizedReturn = (math.Pow(state.equity/initialCapital, 1/years) - 1) * 100
		}
	}

	// Win rate
	if result.TotalTrades > 0 {
		result.WinRate = float64(result.WinningTrades) / float64(result.TotalTrades) * 100
	}

	// Max drawdown
	result.MaxDrawdown = state.maxDrawdown * 100

	// Average win/loss
	if len(wins) > 0 {
		for _, w := range wins {
			result.AvgWin += w
		}
		result.AvgWin /= float64(len(wins))
	}

	if len(losses) > 0 {
		for _, l := range losses {
			result.AvgLoss += l
		}
		result.AvgLoss /= float64(len(losses))
	}

	// Profit factor
	if result.AvgLoss != 0 && len(losses) > 0 {
		totalWins := result.AvgWin * float64(len(wins))
		totalLosses := math.Abs(result.AvgLoss) * float64(len(losses))
		if totalLosses > 0 {
			result.ProfitFactor = totalWins / totalLosses
		}
	}

	// Sharpe ratio (simplified)
	result.SharpeRatio = be.calculateSharpeRatio(result.EquityCurve, initialCapital)
}

// calculateSharpeRatio calculates the Sharpe ratio from equity curve.
func (be *DefaultBacktestEngine) calculateSharpeRatio(equityCurve []EquityPoint, initialCapital float64) float64 {
	if len(equityCurve) < 2 {
		return 0
	}

	// Calculate daily returns
	returns := make([]float64, len(equityCurve)-1)
	for i := 1; i < len(equityCurve); i++ {
		returns[i-1] = (equityCurve[i].Equity - equityCurve[i-1].Equity) / equityCurve[i-1].Equity
	}

	// Calculate mean return
	var meanReturn float64
	for _, r := range returns {
		meanReturn += r
	}
	meanReturn /= float64(len(returns))

	// Calculate standard deviation
	var variance float64
	for _, r := range returns {
		variance += (r - meanReturn) * (r - meanReturn)
	}
	variance /= float64(len(returns))
	stdDev := math.Sqrt(variance)

	if stdDev == 0 {
		return 0
	}

	// Annualize (assuming daily returns)
	riskFreeRate := 0.05 / 252 // 5% annual risk-free rate
	sharpe := (meanReturn - riskFreeRate) / stdDev * math.Sqrt(252)

	return sharpe
}

// Helper functions for indicator calculations

func (be *DefaultBacktestEngine) calculateSMA(candles []models.Candle, index, period int) float64 {
	if index < period-1 {
		return 0
	}

	var sum float64
	for i := index - period + 1; i <= index; i++ {
		sum += candles[i].Close
	}
	return sum / float64(period)
}

func (be *DefaultBacktestEngine) calculateEMA(candles []models.Candle, index, period int) float64 {
	if index < period-1 {
		return be.calculateSMA(candles, index, period)
	}

	multiplier := 2.0 / float64(period+1)
	ema := be.calculateSMA(candles, period-1, period)

	for i := period; i <= index; i++ {
		ema = (candles[i].Close-ema)*multiplier + ema
	}

	return ema
}

func (be *DefaultBacktestEngine) calculateRSI(candles []models.Candle, index, period int) float64 {
	if index < period {
		return 50
	}

	var gains, losses float64
	for i := index - period + 1; i <= index; i++ {
		change := candles[i].Close - candles[i-1].Close
		if change > 0 {
			gains += change
		} else {
			losses -= change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

func (be *DefaultBacktestEngine) calculateMACD(candles []models.Candle, index, fastPeriod, slowPeriod, signalPeriod int) (float64, float64) {
	fastEMA := be.calculateEMA(candles, index, fastPeriod)
	slowEMA := be.calculateEMA(candles, index, slowPeriod)
	macd := fastEMA - slowEMA

	// Calculate signal line (EMA of MACD)
	// Simplified: use current MACD as signal approximation
	signal := macd * 0.9 // Approximation

	return macd, signal
}

// GenerateEquityCurveASCII generates an ASCII chart of the equity curve.
// Requirement 37.8: THE backtest mode SHALL visualize equity curve (ASCII chart in terminal)
func (be *DefaultBacktestEngine) GenerateEquityCurveASCII(result *BacktestResult, width, height int) string {
	if len(result.EquityCurve) == 0 {
		return "No data to display"
	}

	// Find min/max equity
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

	// Add padding
	equityRange := maxEquity - minEquity
	if equityRange == 0 {
		equityRange = 1
	}
	minEquity -= equityRange * 0.05
	maxEquity += equityRange * 0.05
	equityRange = maxEquity - minEquity

	// Create chart grid
	grid := make([][]rune, height)
	for i := range grid {
		grid[i] = make([]rune, width)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}

	// Sample points to fit width
	step := len(result.EquityCurve) / width
	if step == 0 {
		step = 1
	}

	// Plot points
	for x := 0; x < width && x*step < len(result.EquityCurve); x++ {
		point := result.EquityCurve[x*step]
		y := int((point.Equity - minEquity) / equityRange * float64(height-1))
		if y >= 0 && y < height {
			grid[height-1-y][x] = '█'
		}
	}

	// Build string
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Equity Curve (%.0f - %.0f)\n", minEquity, maxEquity))
	sb.WriteString(strings.Repeat("─", width+2) + "\n")

	for _, row := range grid {
		sb.WriteRune('│')
		sb.WriteString(string(row))
		sb.WriteRune('│')
		sb.WriteRune('\n')
	}

	sb.WriteString(strings.Repeat("─", width+2) + "\n")

	return sb.String()
}

// CompareStrategies compares backtest results across different strategies.
// Requirement 37.7: THE CLI SHALL compare backtest results across different strategies or parameters
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
			TotalTrades:      result.TotalTrades,
			ProfitFactor:     result.ProfitFactor,
		})
	}

	// Sort by Sharpe ratio descending
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
	TotalTrades      int
	ProfitFactor     float64
}
