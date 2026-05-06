package trading

import (
	"context"
	"math"
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestEventBacktestExecutesSignalOnNextOpen(t *testing.T) {
	engine := NewBacktestEngine(nil)
	candles := backtestCandles([]float64{100, 110, 120, 130, 140}, 1000)

	result, err := engine.RunEventDrivenOnCandles(context.Background(), BacktestConfig{
		Symbol:              "TEST",
		InitialCapital:      100000,
		Strategy:            "buy_and_hold",
		ExecutionTiming:     "next_open",
		MaxPositionPercent:  10,
		StopLossPercent:     0,
		TakeProfitPercent:   0,
		TrailingStopPercent: 0,
		Slippage:            0.001,
		Commission:          0.0003,
	}, candles)
	if err != nil {
		t.Fatalf("run backtest: %v", err)
	}
	if len(result.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(result.Trades))
	}

	trade := result.Trades[0]
	if !trade.EntryTime.Equal(candles[2].Timestamp) {
		t.Fatalf("expected next-open entry at candle 2, got %s", trade.EntryTime)
	}
	wantEntry := candles[2].Open * 1.001
	if math.Abs(trade.EntryPrice-wantEntry) > 0.0001 {
		t.Fatalf("expected entry %.4f, got %.4f", wantEntry, trade.EntryPrice)
	}
}

func TestEventBacktestTracksCostsAndSlippage(t *testing.T) {
	engine := NewBacktestEngine(nil)
	candles := backtestCandles([]float64{100, 110, 120, 130, 140}, 1000)

	result, err := engine.RunEventDrivenOnCandles(context.Background(), BacktestConfig{
		Symbol:             "TEST",
		InitialCapital:     100000,
		Strategy:           "buy_and_hold",
		MaxPositionPercent: 10,
		Slippage:           0.002,
		Commission:         0.001,
		BrokeragePerLeg:    20,
	}, candles)
	if err != nil {
		t.Fatalf("run backtest: %v", err)
	}
	if result.TotalCosts <= 0 {
		t.Fatalf("expected costs to be tracked")
	}
	if result.TotalSlippage <= 0 {
		t.Fatalf("expected slippage to be tracked")
	}
	if result.Trades[0].GrossPnL <= result.Trades[0].PnL {
		t.Fatalf("expected net pnl below gross pnl after costs")
	}
}

func TestEventBacktestPartialFillCapsQuantityByVolume(t *testing.T) {
	engine := NewBacktestEngine(nil)
	candles := backtestCandles([]float64{100, 100, 100, 100}, 10)

	result, err := engine.RunEventDrivenOnCandles(context.Background(), BacktestConfig{
		Symbol:               "TEST",
		InitialCapital:       100000,
		Strategy:             "buy_and_hold",
		MaxPositionPercent:   95,
		AllowPartialFills:    true,
		MaxFillVolumePercent: 50,
		Slippage:             0.001,
		Commission:           0.0003,
	}, candles)
	if err != nil {
		t.Fatalf("run backtest: %v", err)
	}
	if len(result.Trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(result.Trades))
	}
	if result.Trades[0].Quantity != 5 {
		t.Fatalf("expected quantity capped at 5, got %d", result.Trades[0].Quantity)
	}
	if result.PartialFills == 0 || !result.Trades[0].PartialFill {
		t.Fatalf("expected partial fill to be recorded")
	}
}

func TestEventBacktestRecordsSignalActivity(t *testing.T) {
	engine := NewBacktestEngine(nil)
	candles := backtestCandles([]float64{100, 101, 102, 103, 104}, 1000)

	result, err := engine.RunEventDrivenOnCandles(context.Background(), BacktestConfig{
		Symbol:             "TEST",
		InitialCapital:     100000,
		Strategy:           "buy_and_hold",
		MaxPositionPercent: 10,
		Slippage:           0.001,
		Commission:         0.0003,
	}, candles)
	if err != nil {
		t.Fatalf("run backtest: %v", err)
	}
	if result.SignalActivity.EvaluatedBars != 4 {
		t.Fatalf("expected 4 evaluated bars, got %#v", result.SignalActivity)
	}
	if result.SignalActivity.BuySignals != 1 || result.SignalActivity.SellSignals != 0 || result.SignalActivity.HoldSignals != 3 {
		t.Fatalf("unexpected signal counts: %#v", result.SignalActivity)
	}
	if math.Abs(result.SignalActivity.SignalRatePct-25) > 0.0001 {
		t.Fatalf("expected 25%% signal rate, got %.4f", result.SignalActivity.SignalRatePct)
	}
}

func backtestCandles(opens []float64, volume int64) []models.Candle {
	candles := make([]models.Candle, len(opens))
	start := time.Date(2026, 5, 4, 9, 15, 0, 0, time.UTC)
	for i, open := range opens {
		candles[i] = models.Candle{
			Timestamp: start.Add(time.Duration(i) * time.Minute),
			Open:      open,
			High:      open + 2,
			Low:       open - 2,
			Close:     open + 1,
			Volume:    volume,
		}
	}
	return candles
}
