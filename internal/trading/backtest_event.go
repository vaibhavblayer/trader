package trading

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"zerodha-trader/internal/analysis/quality"
	"zerodha-trader/internal/models"
)

type eventBacktestState struct {
	cash           float64
	position       int
	entryPrice     float64
	entryIndex     int
	entryTime      time.Time
	entryCosts     float64
	entryNotional  float64
	entrySlippage  float64
	entryPartial   bool
	trailingStop   float64
	highSinceEntry float64
	lowSinceEntry  float64
	peakEquity     float64
	maxDrawdown    float64
}

type pendingBacktestOrder struct {
	Signal    string
	Reason    string
	CreatedAt time.Time
}

type backtestFill struct {
	price        float64
	quantity     int
	partial      bool
	slippageCost float64
	costs        float64
	turnover     float64
}

// RunEventDrivenOnCandles executes a bar-by-bar backtest with next-event execution.
func (be *DefaultBacktestEngine) RunEventDrivenOnCandles(ctx context.Context, config BacktestConfig, candles []models.Candle) (*BacktestResult, error) {
	config = be.applyDefaults(config)
	if err := be.validateSimulationConfig(config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if len(candles) < 2 {
		return nil, fmt.Errorf("insufficient data: need at least 2 candles, got %d", len(candles))
	}
	report := quality.ValidateCandles(candles, quality.Config{
		Symbol:          config.Symbol,
		Timeframe:       config.Timeframe,
		MinCandles:      2,
		CheckStaleness:  false,
		AllowZeroVolume: config.AllowPartialFills,
	})
	if !report.Valid() {
		return nil, fmt.Errorf("data quality check failed: %s", report.Error())
	}

	result := &BacktestResult{
		EquityCurve:  make([]EquityPoint, 0, len(candles)),
		Trades:       make([]BacktestTrade, 0),
		StartCapital: config.InitialCapital,
	}
	state := &eventBacktestState{
		cash:       config.InitialCapital,
		peakEquity: config.InitialCapital,
	}

	signalGen := be.getSignalGenerator(config.Strategy, config.Parameters, candles)
	warmup := be.warmupPeriod(config.Strategy, config.Parameters)
	var pending *pendingBacktestOrder

	for i := 0; i < len(candles); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		candle := candles[i]

		if state.position != 0 {
			if trade := be.checkEventRiskExit(state, candle, config, i); trade != nil {
				result.Trades = append(result.Trades, *trade)
			}
		}

		if pending != nil && state.position == 0 && canExecutePending(config, pending, candle, i) {
			if opened := be.executePendingEntry(state, pending, candle, config, result, i); !opened {
				result.RejectedSignals++
			}
			pending = nil
		}

		if i >= warmup {
			signal, _ := signalGen(candles[:i+1], i)
			signal = strings.ToUpper(signal)
			if state.position == 0 && pending == nil && isExecutableBacktestSignal(signal) {
				pending = &pendingBacktestOrder{
					Signal:    signal,
					Reason:    "signal_entry",
					CreatedAt: candle.Timestamp,
				}
			} else if state.position != 0 && isReversalSignal(state.position, signal) {
				if trade := be.closeEventPosition(state, candle, config, "signal_reversal", i); trade != nil {
					result.Trades = append(result.Trades, *trade)
				}
				pending = &pendingBacktestOrder{
					Signal:    signal,
					Reason:    "signal_reversal",
					CreatedAt: candle.Timestamp,
				}
			}
		}

		be.updateEventTrailingStop(state, candle, config)
		equity := markToMarket(state, candle.Close)
		be.recordEventEquity(result, state, candle.Timestamp, equity)
	}

	if state.position != 0 {
		lastIndex := len(candles) - 1
		if trade := be.closeEventPosition(state, candles[lastIndex], config, "end_of_backtest", lastIndex); trade != nil {
			result.Trades = append(result.Trades, *trade)
		}
		be.recordEventEquity(result, state, candles[lastIndex].Timestamp, state.cash)
	}

	result.EndCapital = state.cash
	be.calculateMetrics(result, config.InitialCapital, backtestMetricsState{
		equity:      result.EndCapital,
		maxDrawdown: state.maxDrawdown,
	})
	return result, nil
}

func canExecutePending(config BacktestConfig, pending *pendingBacktestOrder, candle models.Candle, index int) bool {
	if pending == nil {
		return false
	}
	if config.ExecutionTiming == "same_close" {
		return true
	}
	return index > 0 && candle.Timestamp.After(pending.CreatedAt)
}

func (be *DefaultBacktestEngine) executePendingEntry(state *eventBacktestState, pending *pendingBacktestOrder, candle models.Candle, config BacktestConfig, result *BacktestResult, barIndex int) bool {
	if pending.Signal == "SELL" && !config.AllowShort {
		return false
	}

	sideMultiplier := 1
	if pending.Signal == "SELL" {
		sideMultiplier = -1
	}

	basePrice := entryBasePrice(config, candle)
	quantity := be.calculatePositionSize(markToMarket(state, basePrice), basePrice, config)
	if quantity <= 0 {
		return false
	}

	fill := buildBacktestFill(basePrice, quantity, sideMultiplier, true, candle, config)
	if fill.quantity <= 0 {
		return false
	}
	if fill.partial {
		result.PartialFills++
	}

	if sideMultiplier > 0 {
		requiredCash := fill.turnover + fill.costs
		if requiredCash > state.cash {
			fill.quantity = int(state.cash / (fill.price * (1 + estimatedCostRate(config))))
			if fill.quantity <= 0 {
				return false
			}
			fill = buildBacktestFill(basePrice, fill.quantity, sideMultiplier, true, candle, config)
			fill.partial = true
			result.PartialFills++
		}
		state.cash -= fill.turnover + fill.costs
		state.position = fill.quantity
	} else {
		state.cash += fill.turnover - fill.costs
		state.position = -fill.quantity
	}

	state.entryPrice = fill.price
	state.entryIndex = barIndex
	state.entryTime = candle.Timestamp
	state.entryCosts = fill.costs
	state.entryNotional = fill.turnover
	state.entrySlippage = fill.slippageCost
	state.entryPartial = fill.partial
	state.highSinceEntry = candle.High
	state.lowSinceEntry = candle.Low
	state.trailingStop = 0
	return true
}

func (be *DefaultBacktestEngine) checkEventRiskExit(state *eventBacktestState, candle models.Candle, config BacktestConfig, barIndex int) *BacktestTrade {
	if state.position > 0 {
		if config.StopLossPercent > 0 {
			stopPrice := state.entryPrice * (1 - config.StopLossPercent/100)
			if candle.Low <= stopPrice {
				return be.closeEventPositionAtPrice(state, stopPrice, candle, config, "stop_loss", barIndex)
			}
		}
		if config.TakeProfitPercent > 0 {
			targetPrice := state.entryPrice * (1 + config.TakeProfitPercent/100)
			if candle.High >= targetPrice {
				return be.closeEventPositionAtPrice(state, targetPrice, candle, config, "take_profit", barIndex)
			}
		}
		if config.TrailingStopPercent > 0 && state.trailingStop > 0 && candle.Low <= state.trailingStop {
			return be.closeEventPositionAtPrice(state, state.trailingStop, candle, config, "trailing_stop", barIndex)
		}
	}
	if state.position < 0 {
		if config.StopLossPercent > 0 {
			stopPrice := state.entryPrice * (1 + config.StopLossPercent/100)
			if candle.High >= stopPrice {
				return be.closeEventPositionAtPrice(state, stopPrice, candle, config, "stop_loss", barIndex)
			}
		}
		if config.TakeProfitPercent > 0 {
			targetPrice := state.entryPrice * (1 - config.TakeProfitPercent/100)
			if candle.Low <= targetPrice {
				return be.closeEventPositionAtPrice(state, targetPrice, candle, config, "take_profit", barIndex)
			}
		}
		if config.TrailingStopPercent > 0 && state.trailingStop > 0 && candle.High >= state.trailingStop {
			return be.closeEventPositionAtPrice(state, state.trailingStop, candle, config, "trailing_stop", barIndex)
		}
	}
	return nil
}

func (be *DefaultBacktestEngine) closeEventPosition(state *eventBacktestState, candle models.Candle, config BacktestConfig, reason string, barIndex int) *BacktestTrade {
	if state.position == 0 {
		return nil
	}
	return be.closeEventPositionAtPrice(state, candle.Close, candle, config, reason, barIndex)
}

func (be *DefaultBacktestEngine) closeEventPositionAtPrice(state *eventBacktestState, basePrice float64, candle models.Candle, config BacktestConfig, reason string, barIndex int) *BacktestTrade {
	if state.position == 0 {
		return nil
	}

	qty := int(math.Abs(float64(state.position)))
	sideMultiplier := -1
	side := "LONG"
	if state.position < 0 {
		sideMultiplier = 1
		side = "SHORT"
	}
	fill := buildBacktestFill(basePrice, qty, sideMultiplier, false, candle, config)

	grossPnL := 0.0
	if state.position > 0 {
		state.cash += fill.turnover - fill.costs
		grossPnL = fill.turnover - state.entryNotional
	} else {
		state.cash -= fill.turnover + fill.costs
		grossPnL = state.entryNotional - fill.turnover
	}
	totalCosts := state.entryCosts + fill.costs
	pnl := grossPnL - totalCosts

	pnlPercent := 0.0
	if state.entryNotional > 0 {
		pnlPercent = pnl / state.entryNotional * 100
	}

	trade := &BacktestTrade{
		EntryTime:    state.entryTime,
		ExitTime:     candle.Timestamp,
		Symbol:       config.Symbol,
		Side:         side,
		EntryPrice:   state.entryPrice,
		ExitPrice:    fill.price,
		Quantity:     qty,
		PnL:          pnl,
		GrossPnL:     grossPnL,
		EntryCosts:   state.entryCosts,
		ExitCosts:    fill.costs,
		TotalCosts:   totalCosts,
		SlippageCost: state.entrySlippage + fill.slippageCost,
		PnLPercent:   pnlPercent,
		ExitReason:   reason,
		HoldBars:     barIndex - state.entryIndex,
		PartialFill:  state.entryPartial || fill.partial,
	}

	state.position = 0
	state.entryPrice = 0
	state.entryIndex = 0
	state.entryTime = time.Time{}
	state.entryCosts = 0
	state.entryNotional = 0
	state.entrySlippage = 0
	state.entryPartial = false
	state.trailingStop = 0
	state.highSinceEntry = 0
	state.lowSinceEntry = 0

	return trade
}

func buildBacktestFill(basePrice float64, requestedQty int, sideMultiplier int, isEntry bool, candle models.Candle, config BacktestConfig) backtestFill {
	qty := requestedQty
	partial := false
	if config.AllowPartialFills && config.MaxFillVolumePercent > 0 && candle.Volume > 0 {
		maxQty := int(float64(candle.Volume) * (config.MaxFillVolumePercent / 100))
		if maxQty < qty {
			qty = maxQty
			partial = true
		}
	}
	if qty <= 0 || basePrice <= 0 {
		return backtestFill{}
	}

	price := basePrice
	if sideMultiplier > 0 {
		price = basePrice * (1 + config.Slippage)
	} else {
		price = basePrice * (1 - config.Slippage)
	}
	turnover := price * float64(qty)
	return backtestFill{
		price:        price,
		quantity:     qty,
		partial:      partial,
		slippageCost: math.Abs(price-basePrice) * float64(qty),
		costs:        transactionCosts(turnover, sideMultiplier, isEntry, config),
		turnover:     turnover,
	}
}

func transactionCosts(turnover float64, sideMultiplier int, isEntry bool, config BacktestConfig) float64 {
	brokerageRate := config.BrokerageRate
	if brokerageRate == 0 {
		brokerageRate = config.Commission
	}
	brokerage := turnover*brokerageRate + config.BrokeragePerLeg
	exchange := turnover * config.ExchangeFeeRate
	sebi := turnover * config.SEBIRate
	stt := 0.0
	if sideMultiplier < 0 {
		stt = turnover * config.STTRate
	}
	stamp := 0.0
	if sideMultiplier > 0 {
		stamp = turnover * config.StampDutyRate
	}
	gst := (brokerage + exchange) * config.GSTRate
	return brokerage + exchange + sebi + stt + stamp + gst
}

func estimatedCostRate(config BacktestConfig) float64 {
	rate := config.BrokerageRate + config.Commission + config.STTRate + config.ExchangeFeeRate + config.SEBIRate + config.StampDutyRate
	if config.GSTRate > 0 {
		rate += config.GSTRate * (config.BrokerageRate + config.Commission + config.ExchangeFeeRate)
	}
	if rate <= 0 {
		return 0.001
	}
	return rate
}

func entryBasePrice(config BacktestConfig, candle models.Candle) float64 {
	if config.ExecutionTiming == "same_close" {
		return candle.Close
	}
	return candle.Open
}

func markToMarket(state *eventBacktestState, price float64) float64 {
	if state.position > 0 {
		return state.cash + float64(state.position)*price
	}
	if state.position < 0 {
		return state.cash - float64(-state.position)*price
	}
	return state.cash
}

func (be *DefaultBacktestEngine) updateEventTrailingStop(state *eventBacktestState, candle models.Candle, config BacktestConfig) {
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
}

func (be *DefaultBacktestEngine) recordEventEquity(result *BacktestResult, state *eventBacktestState, timestamp time.Time, equity float64) {
	if equity > state.peakEquity {
		state.peakEquity = equity
	}
	drawdown := 0.0
	if state.peakEquity > 0 {
		drawdown = (state.peakEquity - equity) / state.peakEquity
	}
	if drawdown > state.maxDrawdown {
		state.maxDrawdown = drawdown
	}
	result.EquityCurve = append(result.EquityCurve, EquityPoint{
		Timestamp: timestamp,
		Equity:    equity,
		Drawdown:  drawdown,
	})
}

func isExecutableBacktestSignal(signal string) bool {
	return signal == "BUY" || signal == "SELL"
}

func isReversalSignal(position int, signal string) bool {
	return (position > 0 && signal == "SELL") || (position < 0 && signal == "BUY")
}
