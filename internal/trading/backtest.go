// Package trading provides trading operations including backtesting.
package trading

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"zerodha-trader/internal/analysis/indicators"
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

// backtestState holds the mutable state during a backtest simulation.
type backtestState struct {
	capital        float64
	equity         float64
	position       int     // positive = long, negative = short, 0 = flat
	entryPrice     float64
	entryIndex     int
	entryTime      time.Time
	peakEquity     float64
	maxDrawdown    float64
	trailingStop   float64 // current trailing stop price (0 = inactive)
	highSinceEntry float64 // highest price since entry (for trailing stop)
	lowSinceEntry  float64 // lowest price since entry (for short trailing stop)
}

// Run executes a backtest with the given configuration.
func (be *DefaultBacktestEngine) Run(ctx context.Context, config BacktestConfig) (*BacktestResult, error) {
	if err := be.validateConfig(config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Apply defaults
	config = be.applyDefaults(config)

	// Fetch historical data
	candles, err := be.store.GetCandles(ctx, config.Symbol, "1day", config.StartDate, config.EndDate)
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
	config = be.applyDefaults(config)

	result := &BacktestResult{
		EquityCurve:  make([]EquityPoint, 0, len(candles)),
		Trades:       make([]BacktestTrade, 0),
		StartCapital: config.InitialCapital,
	}

	state := &backtestState{
		capital:    config.InitialCapital,
		equity:     config.InitialCapital,
		peakEquity: config.InitialCapital,
	}

	signalGen := be.getSignalGenerator(config.Strategy, config.Parameters, candles)
	warmup := be.warmupPeriod(config.Strategy, config.Parameters)

	for i := warmup; i < len(candles); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		candle := candles[i]

		// Check risk-based exits first (stop loss, take profit, trailing stop)
		if state.position != 0 {
			exitTrade := be.checkRiskExits(state, candle, config, i)
			if exitTrade != nil {
				result.Trades = append(result.Trades, *exitTrade)
			}
		}

		// Generate and process signal only if flat after risk exits
		if state.position == 0 {
			signal, _ := signalGen(candles[:i+1], i)
			trade := be.processSignal(state, signal, candle, config, i)
			if trade != nil {
				result.Trades = append(result.Trades, *trade)
			}
		} else {
			// Check for reversal signals
			signal, _ := signalGen(candles[:i+1], i)
			if (state.position > 0 && signal == "SELL") || (state.position < 0 && signal == "BUY") {
				closeTrade := be.closePosition(state, candle, config, "signal_reversal", i)
				if closeTrade != nil {
					result.Trades = append(result.Trades, *closeTrade)
				}
				// Open new position in opposite direction
				newTrade := be.processSignal(state, signal, candle, config, i)
				if newTrade != nil {
					result.Trades = append(result.Trades, *newTrade)
				}
			}
		}

		// Update trailing stop tracking
		if state.position > 0 && candle.High > state.highSinceEntry {
			state.highSinceEntry = candle.High
			if config.TrailingStopPercent > 0 {
				state.trailingStop = state.highSinceEntry * (1 - config.TrailingStopPercent/100)
			}
		}
		if state.position < 0 && (candle.Low < state.lowSinceEntry || state.lowSinceEntry == 0) {
			state.lowSinceEntry = candle.Low
			if config.TrailingStopPercent > 0 {
				state.trailingStop = state.lowSinceEntry * (1 + config.TrailingStopPercent/100)
			}
		}

		// Mark to market
		currentEquity := state.capital
		if state.position > 0 {
			currentEquity += float64(state.position) * (candle.Close - state.entryPrice)
		} else if state.position < 0 {
			currentEquity += float64(-state.position) * (state.entryPrice - candle.Close)
		}
		state.equity = currentEquity

		if currentEquity > state.peakEquity {
			state.peakEquity = currentEquity
		}
		drawdown := 0.0
		if state.peakEquity > 0 {
			drawdown = (state.peakEquity - currentEquity) / state.peakEquity
		}
		if drawdown > state.maxDrawdown {
			state.maxDrawdown = drawdown
		}

		result.EquityCurve = append(result.EquityCurve, EquityPoint{
			Timestamp: candle.Timestamp,
			Equity:    currentEquity,
			Drawdown:  drawdown,
		})
	}

	// Close any open position at end
	if state.position != 0 {
		lastCandle := candles[len(candles)-1]
		trade := be.closePosition(state, lastCandle, config, "end_of_backtest", len(candles)-1)
		if trade != nil {
			result.Trades = append(result.Trades, *trade)
		}
	}

	be.calculateMetrics(result, config.InitialCapital, state)
	return result, nil
}

// checkRiskExits checks stop loss, take profit, and trailing stop conditions.
func (be *DefaultBacktestEngine) checkRiskExits(state *backtestState, candle models.Candle, config BacktestConfig, barIndex int) *BacktestTrade {
	if state.position > 0 {
		// Long position exits
		if config.StopLossPercent > 0 {
			stopPrice := state.entryPrice * (1 - config.StopLossPercent/100)
			if candle.Low <= stopPrice {
				return be.closePositionAtPrice(state, stopPrice, candle, config, "stop_loss", barIndex)
			}
		}
		if config.TakeProfitPercent > 0 {
			targetPrice := state.entryPrice * (1 + config.TakeProfitPercent/100)
			if candle.High >= targetPrice {
				return be.closePositionAtPrice(state, targetPrice, candle, config, "take_profit", barIndex)
			}
		}
		if config.TrailingStopPercent > 0 && state.trailingStop > 0 {
			if candle.Low <= state.trailingStop {
				return be.closePositionAtPrice(state, state.trailingStop, candle, config, "trailing_stop", barIndex)
			}
		}
	} else if state.position < 0 {
		// Short position exits
		if config.StopLossPercent > 0 {
			stopPrice := state.entryPrice * (1 + config.StopLossPercent/100)
			if candle.High >= stopPrice {
				return be.closePositionAtPrice(state, stopPrice, candle, config, "stop_loss", barIndex)
			}
		}
		if config.TakeProfitPercent > 0 {
			targetPrice := state.entryPrice * (1 - config.TakeProfitPercent/100)
			if candle.Low <= targetPrice {
				return be.closePositionAtPrice(state, targetPrice, candle, config, "take_profit", barIndex)
			}
		}
		if config.TrailingStopPercent > 0 && state.trailingStop > 0 {
			if candle.High >= state.trailingStop {
				return be.closePositionAtPrice(state, state.trailingStop, candle, config, "trailing_stop", barIndex)
			}
		}
	}
	return nil
}

// closePositionAtPrice closes at a specific price (for stop/target fills).
func (be *DefaultBacktestEngine) closePositionAtPrice(state *backtestState, price float64, candle models.Candle, config BacktestConfig, reason string, barIndex int) *BacktestTrade {
	if state.position == 0 {
		return nil
	}

	qty := state.position
	if qty < 0 {
		qty = -qty
	}

	var pnl float64
	var side string
	if state.position > 0 {
		pnl = float64(state.position) * (price - state.entryPrice)
		side = "LONG"
	} else {
		pnl = float64(-state.position) * (state.entryPrice - price)
		side = "SHORT"
	}

	// Deduct commission on exit
	commission := price * float64(qty) * config.Commission
	pnl -= commission

	pnlPercent := 0.0
	if state.entryPrice > 0 {
		if state.position > 0 {
			pnlPercent = (price - state.entryPrice) / state.entryPrice * 100
		} else {
			pnlPercent = (state.entryPrice - price) / state.entryPrice * 100
		}
	}

	trade := &BacktestTrade{
		EntryTime:  state.entryTime,
		ExitTime:   candle.Timestamp,
		Symbol:     config.Symbol,
		Side:       side,
		EntryPrice: state.entryPrice,
		ExitPrice:  price,
		Quantity:   qty,
		PnL:        pnl,
		PnLPercent: pnlPercent,
		ExitReason: reason,
		HoldBars:   barIndex - state.entryIndex,
	}

	// Return capital
	state.capital += pnl + (state.entryPrice * float64(qty))
	state.position = 0
	state.entryPrice = 0
	state.entryTime = time.Time{}
	state.entryIndex = 0
	state.trailingStop = 0
	state.highSinceEntry = 0
	state.lowSinceEntry = 0

	return trade
}

// processSignal opens a new position based on a signal.
func (be *DefaultBacktestEngine) processSignal(state *backtestState, signal string, candle models.Candle, config BacktestConfig, barIndex int) *BacktestTrade {
	slippage := config.Slippage

	switch signal {
	case "BUY":
		if state.position != 0 {
			return nil
		}
		executionPrice := candle.Close * (1 + slippage)
		positionSize := be.calculatePositionSize(state.capital, executionPrice, config)
		if positionSize > 0 {
			commission := executionPrice * float64(positionSize) * config.Commission
			state.capital -= float64(positionSize)*executionPrice + commission
			state.position = positionSize
			state.entryPrice = executionPrice
			state.entryTime = candle.Timestamp
			state.entryIndex = barIndex
			state.highSinceEntry = candle.High
			state.lowSinceEntry = candle.Low
			state.trailingStop = 0
		}

	case "SELL":
		if state.position != 0 || !config.AllowShort {
			return nil
		}
		executionPrice := candle.Close * (1 - slippage)
		positionSize := be.calculatePositionSize(state.capital, executionPrice, config)
		if positionSize > 0 {
			commission := executionPrice * float64(positionSize) * config.Commission
			state.capital -= commission // short: capital held as margin
			state.position = -positionSize
			state.entryPrice = executionPrice
			state.entryTime = candle.Timestamp
			state.entryIndex = barIndex
			state.highSinceEntry = candle.High
			state.lowSinceEntry = candle.Low
			state.trailingStop = 0
		}
	}

	return nil
}

// closePosition closes the current position at market close price.
func (be *DefaultBacktestEngine) closePosition(state *backtestState, candle models.Candle, config BacktestConfig, reason string, barIndex int) *BacktestTrade {
	if state.position == 0 {
		return nil
	}

	slippage := config.Slippage
	var exitPrice float64
	if state.position > 0 {
		exitPrice = candle.Close * (1 - slippage)
	} else {
		exitPrice = candle.Close * (1 + slippage)
	}

	return be.closePositionAtPrice(state, exitPrice, candle, config, reason, barIndex)
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
	switch strings.ToLower(strategy) {
	case "sma_crossover":
		long := getIntParam(params, "long_period", 20)
		return long + 2
	case "ema_crossover":
		long := getIntParam(params, "long_period", 21)
		return long + 2
	case "rsi_oversold":
		period := getIntParam(params, "period", 14)
		return period + 2
	case "macd":
		return 35 // 26 slow + 9 signal
	case "supertrend":
		period := getIntParam(params, "atr_period", 10)
		return period + 5
	case "bollinger_breakout":
		period := getIntParam(params, "period", 20)
		return period + 2
	case "adx_trend":
		period := getIntParam(params, "period", 14)
		return period*2 + 2
	case "donchian_breakout":
		period := getIntParam(params, "period", 20)
		return period + 2
	case "multi_indicator":
		return 40
	default:
		return 30
	}
}

// getSignalGenerator returns a signal generator for the given strategy.
// All strategies use the real indicators from analysis/indicators.
func (be *DefaultBacktestEngine) getSignalGenerator(strategy string, params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	switch strings.ToLower(strategy) {
	case "sma_crossover":
		return be.smaCrossoverStrategy(params, allCandles)
	case "ema_crossover":
		return be.emaCrossoverStrategy(params, allCandles)
	case "rsi_oversold":
		return be.rsiStrategy(params, allCandles)
	case "macd":
		return be.macdStrategy(params, allCandles)
	case "supertrend":
		return be.supertrendStrategy(params, allCandles)
	case "bollinger_breakout":
		return be.bollingerStrategy(params, allCandles)
	case "adx_trend":
		return be.adxTrendStrategy(params, allCandles)
	case "donchian_breakout":
		return be.donchianStrategy(params, allCandles)
	case "multi_indicator":
		return be.multiIndicatorStrategy(params, allCandles)
	default:
		return be.emaCrossoverStrategy(params, allCandles)
	}
}

// smaCrossoverStrategy uses the real SMA indicator.
func (be *DefaultBacktestEngine) smaCrossoverStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	shortPeriod := getIntParam(params, "short_period", 10)
	longPeriod := getIntParam(params, "long_period", 20)

	shortSMA := indicators.NewSMA(shortPeriod)
	longSMA := indicators.NewSMA(longPeriod)
	shortVals, _ := shortSMA.Calculate(allCandles)
	longVals, _ := longSMA.Calculate(allCandles)

	return func(candles []models.Candle, index int) (string, float64) {
		if shortVals == nil || longVals == nil || index < 1 {
			return "HOLD", 0
		}
		if index >= len(shortVals) || index >= len(longVals) {
			return "HOLD", 0
		}
		if shortVals[index-1] <= longVals[index-1] && shortVals[index] > longVals[index] {
			return "BUY", 70
		}
		if shortVals[index-1] >= longVals[index-1] && shortVals[index] < longVals[index] {
			return "SELL", 70
		}
		return "HOLD", 0
	}
}

// emaCrossoverStrategy uses the real EMA indicator.
func (be *DefaultBacktestEngine) emaCrossoverStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	shortPeriod := getIntParam(params, "short_period", 9)
	longPeriod := getIntParam(params, "long_period", 21)

	shortEMA := indicators.NewEMA(shortPeriod)
	longEMA := indicators.NewEMA(longPeriod)
	shortVals, _ := shortEMA.Calculate(allCandles)
	longVals, _ := longEMA.Calculate(allCandles)

	return func(candles []models.Candle, index int) (string, float64) {
		if shortVals == nil || longVals == nil || index < 1 {
			return "HOLD", 0
		}
		if index >= len(shortVals) || index >= len(longVals) {
			return "HOLD", 0
		}
		if shortVals[index-1] <= longVals[index-1] && shortVals[index] > longVals[index] {
			return "BUY", 70
		}
		if shortVals[index-1] >= longVals[index-1] && shortVals[index] < longVals[index] {
			return "SELL", 70
		}
		return "HOLD", 0
	}
}

// rsiStrategy uses the real RSI indicator with proper Wilder smoothing.
func (be *DefaultBacktestEngine) rsiStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	period := getIntParam(params, "period", 14)
	oversold := getFloatParam(params, "oversold", 30.0)
	overbought := getFloatParam(params, "overbought", 70.0)

	rsi := indicators.NewRSI(period)
	rsiVals, _ := rsi.Calculate(allCandles)

	return func(candles []models.Candle, index int) (string, float64) {
		if rsiVals == nil || index < 1 || index >= len(rsiVals) {
			return "HOLD", 0
		}
		prev := rsiVals[index-1]
		curr := rsiVals[index]
		if prev <= oversold && curr > oversold {
			return "BUY", 65
		}
		if prev >= overbought && curr < overbought {
			return "SELL", 65
		}
		return "HOLD", 0
	}
}

// macdStrategy uses the real MACD indicator (proper EMA of MACD for signal line).
func (be *DefaultBacktestEngine) macdStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	fast := getIntParam(params, "fast_period", 12)
	slow := getIntParam(params, "slow_period", 26)
	signal := getIntParam(params, "signal_period", 9)

	macd := indicators.NewMACD(fast, slow, signal)
	macdVals, _ := macd.Calculate(allCandles)

	return func(candles []models.Candle, index int) (string, float64) {
		if macdVals == nil || index < 1 {
			return "HOLD", 0
		}
		macdLine := macdVals["macd"]
		signalLine := macdVals["signal"]
		if macdLine == nil || signalLine == nil || index >= len(macdLine) || index >= len(signalLine) {
			return "HOLD", 0
		}
		if macdLine[index-1] <= signalLine[index-1] && macdLine[index] > signalLine[index] {
			return "BUY", 75
		}
		if macdLine[index-1] >= signalLine[index-1] && macdLine[index] < signalLine[index] {
			return "SELL", 75
		}
		return "HOLD", 0
	}
}

// supertrendStrategy uses the real SuperTrend indicator.
func (be *DefaultBacktestEngine) supertrendStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	atrPeriod := getIntParam(params, "atr_period", 10)
	multiplier := getFloatParam(params, "multiplier", 3.0)

	st := indicators.NewSuperTrend(atrPeriod, multiplier)
	stVals, _ := st.Calculate(allCandles)

	return func(candles []models.Candle, index int) (string, float64) {
		if stVals == nil || index < 1 {
			return "HOLD", 0
		}
		direction := stVals["direction"]
		if direction == nil || index >= len(direction) {
			return "HOLD", 0
		}
		// direction: 1 = bullish, -1 = bearish
		if direction[index-1] <= 0 && direction[index] > 0 {
			return "BUY", 80
		}
		if direction[index-1] >= 0 && direction[index] < 0 {
			return "SELL", 80
		}
		return "HOLD", 0
	}
}

// bollingerStrategy: buy on lower band touch + reversal, sell on upper band touch.
func (be *DefaultBacktestEngine) bollingerStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	period := getIntParam(params, "period", 20)
	stdDev := getFloatParam(params, "std_dev", 2.0)

	bb := indicators.NewBollingerBands(period, stdDev)
	bbVals, _ := bb.Calculate(allCandles)

	return func(candles []models.Candle, index int) (string, float64) {
		if bbVals == nil || index < 1 || index >= len(allCandles) {
			return "HOLD", 0
		}
		upper := bbVals["upper"]
		lower := bbVals["lower"]
		if upper == nil || lower == nil || index >= len(upper) || index >= len(lower) {
			return "HOLD", 0
		}
		close := candles[index].Close
		prevClose := candles[index-1].Close

		// Buy: price was below lower band and now closes above it (mean reversion)
		if prevClose <= lower[index-1] && close > lower[index] {
			return "BUY", 65
		}
		// Sell: price was above upper band and now closes below it
		if prevClose >= upper[index-1] && close < upper[index] {
			return "SELL", 65
		}
		return "HOLD", 0
	}
}

// adxTrendStrategy: enter on strong trend (ADX > 25) in direction of +DI/-DI.
func (be *DefaultBacktestEngine) adxTrendStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	period := getIntParam(params, "period", 14)
	threshold := getFloatParam(params, "threshold", 25.0)

	adx := indicators.NewADX(period)
	adxVals, _ := adx.Calculate(allCandles)

	return func(candles []models.Candle, index int) (string, float64) {
		if adxVals == nil || index < 1 {
			return "HOLD", 0
		}
		adxLine := adxVals["adx"]
		plusDI := adxVals["plus_di"]
		minusDI := adxVals["minus_di"]
		if adxLine == nil || plusDI == nil || minusDI == nil {
			return "HOLD", 0
		}
		if index >= len(adxLine) || index >= len(plusDI) || index >= len(minusDI) {
			return "HOLD", 0
		}

		// Only trade when trend is strong
		if adxLine[index] < threshold {
			return "HOLD", 0
		}

		// +DI crosses above -DI in strong trend
		if plusDI[index-1] <= minusDI[index-1] && plusDI[index] > minusDI[index] {
			return "BUY", 75
		}
		// -DI crosses above +DI in strong trend
		if minusDI[index-1] <= plusDI[index-1] && minusDI[index] > plusDI[index] {
			return "SELL", 75
		}
		return "HOLD", 0
	}
}

// donchianStrategy: Donchian channel breakout (turtle trading style).
func (be *DefaultBacktestEngine) donchianStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	period := getIntParam(params, "period", 20)

	dc := indicators.NewDonchianChannels(period)
	dcVals, _ := dc.Calculate(allCandles)

	return func(candles []models.Candle, index int) (string, float64) {
		if dcVals == nil || index < 1 || index >= len(allCandles) {
			return "HOLD", 0
		}
		upper := dcVals["upper"]
		lower := dcVals["lower"]
		if upper == nil || lower == nil || index >= len(upper) || index >= len(lower) {
			return "HOLD", 0
		}

		close := candles[index].Close
		// Breakout above upper channel
		if close > upper[index-1] && upper[index-1] > 0 {
			return "BUY", 80
		}
		// Breakdown below lower channel
		if close < lower[index-1] && lower[index-1] > 0 {
			return "SELL", 80
		}
		return "HOLD", 0
	}
}

// multiIndicatorStrategy: combines EMA crossover + RSI filter + volume confirmation.
func (be *DefaultBacktestEngine) multiIndicatorStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	// EMA
	shortEMA := indicators.NewEMA(9)
	longEMA := indicators.NewEMA(21)
	shortVals, _ := shortEMA.Calculate(allCandles)
	longVals, _ := longEMA.Calculate(allCandles)

	// RSI filter
	rsi := indicators.NewRSI(14)
	rsiVals, _ := rsi.Calculate(allCandles)

	// ADX for trend strength
	adx := indicators.NewADX(14)
	adxVals, _ := adx.Calculate(allCandles)

	// Volume: compute 20-period average volume for confirmation
	var avgVolumes []float64
	if len(allCandles) >= 20 {
		avgVolumes = make([]float64, len(allCandles))
		for i := 19; i < len(allCandles); i++ {
			var volSum float64
			for j := i - 19; j <= i; j++ {
				volSum += float64(allCandles[j].Volume)
			}
			avgVolumes[i] = volSum / 20
		}
	}

	return func(candles []models.Candle, index int) (string, float64) {
		if shortVals == nil || longVals == nil || rsiVals == nil || index < 1 {
			return "HOLD", 0
		}
		if index >= len(shortVals) || index >= len(longVals) || index >= len(rsiVals) {
			return "HOLD", 0
		}

		emaCross := shortVals[index-1] <= longVals[index-1] && shortVals[index] > longVals[index]
		emaCrossDown := shortVals[index-1] >= longVals[index-1] && shortVals[index] < longVals[index]

		rsiOK := rsiVals[index] > 30 && rsiVals[index] < 70
		volumeOK := true
		if avgVolumes != nil && index < len(avgVolumes) && avgVolumes[index] > 0 {
			volumeOK = float64(candles[index].Volume) > avgVolumes[index]*1.2
		}

		// ADX trend strength filter
		trendStrong := true
		if adxVals != nil {
			if adxLine, ok := adxVals["adx"]; ok && index < len(adxLine) {
				trendStrong = adxLine[index] > 20
			}
		}

		confidence := 60.0
		if volumeOK {
			confidence += 10
		}
		if trendStrong {
			confidence += 10
		}

		if emaCross && rsiOK && (volumeOK || trendStrong) {
			return "BUY", confidence
		}
		if emaCrossDown && rsiOK && (volumeOK || trendStrong) {
			return "SELL", confidence
		}
		return "HOLD", 0
	}
}

// calculateMetrics computes all performance metrics from the trade list and equity curve.
func (be *DefaultBacktestEngine) calculateMetrics(result *BacktestResult, initialCapital float64, state *backtestState) {
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

func (be *DefaultBacktestEngine) applyDefaults(config BacktestConfig) BacktestConfig {
	if config.Slippage == 0 {
		config.Slippage = 0.001 // 0.1%
	}
	if config.Commission == 0 {
		config.Commission = 0.0003 // 0.03% (Zerodha intraday)
	}
	if config.MaxPositionPercent == 0 {
		config.MaxPositionPercent = 95
	}
	if config.StopLossPercent == 0 {
		config.StopLossPercent = 3.0 // default 3% stop loss
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

// AvailableStrategies returns the list of supported strategy names.
func AvailableStrategies() []string {
	return []string{
		"ema_crossover",
		"sma_crossover",
		"rsi_oversold",
		"macd",
		"supertrend",
		"bollinger_breakout",
		"adx_trend",
		"donchian_breakout",
		"multi_indicator",
	}
}
