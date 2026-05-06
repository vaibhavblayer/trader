package trading

import (
	"context"
	"strings"
	"testing"
)

func TestLatestSignalDiagnosticExplainsMultiIndicatorNoSignal(t *testing.T) {
	engine := NewBacktestEngine(nil)
	candles := backtestCandles(makeIncreasingPrices(80, 100, 0.1), 1000)

	diagnostic, err := engine.LatestSignalDiagnostic(BacktestConfig{
		Symbol:         "TEST",
		InitialCapital: 100000,
		Strategy:       "multi_indicator",
		Parameters: map[string]interface{}{
			"short_period":      9,
			"long_period":       21,
			"rsi_min":           30.0,
			"rsi_max":           70.0,
			"adx_threshold":     20.0,
			"volume_multiplier": 1.2,
		},
	}, candles)
	if err != nil {
		t.Fatalf("diagnostic: %v", err)
	}
	if diagnostic.Signal != "HOLD" {
		t.Fatalf("expected HOLD, got %#v", diagnostic)
	}
	if !strings.HasPrefix(diagnostic.Reason, "failed:") {
		t.Fatalf("expected failed gate reason, got %#v", diagnostic)
	}
	if len(diagnostic.Gates) == 0 {
		t.Fatalf("expected gate diagnostics")
	}
}

func TestLatestSignalDiagnosticMatchesDonchianBreakout(t *testing.T) {
	engine := NewBacktestEngine(nil)
	prices := make([]float64, 30)
	for i := range prices {
		prices[i] = 100
	}
	prices[len(prices)-1] = 110
	candles := backtestCandles(prices, 1000)

	diagnostic, err := engine.LatestSignalDiagnostic(BacktestConfig{
		Symbol:         "TEST",
		InitialCapital: 100000,
		Strategy:       "donchian_breakout",
		Parameters:     map[string]interface{}{"period": 20},
	}, candles)
	if err != nil {
		t.Fatalf("diagnostic: %v", err)
	}
	if diagnostic.Signal != "BUY" || diagnostic.Confidence <= 0 || diagnostic.Reason != "latest_signal_buy" {
		t.Fatalf("unexpected diagnostic: %#v", diagnostic)
	}

	signal, confidence, err := engine.LatestSignal(BacktestConfig{
		Symbol:         "TEST",
		InitialCapital: 100000,
		Strategy:       "donchian_breakout",
		Parameters:     map[string]interface{}{"period": 20},
	}, candles)
	if err != nil {
		t.Fatalf("latest signal: %v", err)
	}
	if signal != diagnostic.Signal || confidence != diagnostic.Confidence {
		t.Fatalf("diagnostic mismatch with latest signal: signal=%s/%.1f diagnostic=%#v", signal, confidence, diagnostic)
	}

	result, err := engine.RunEventDrivenOnCandles(context.Background(), BacktestConfig{
		Symbol:             "TEST",
		InitialCapital:     100000,
		Strategy:           "donchian_breakout",
		Parameters:         map[string]interface{}{"period": 20},
		MaxPositionPercent: 10,
	}, candles)
	if err != nil {
		t.Fatalf("backtest: %v", err)
	}
	if result.SignalActivity.BuySignals == 0 {
		t.Fatalf("expected buy signal activity, got %#v", result.SignalActivity)
	}
}

func TestLatestSignalDiagnosticMatchesIntradayMomentum(t *testing.T) {
	engine := NewBacktestEngine(nil)
	prices := make([]float64, 40)
	for i := range prices {
		prices[i] = 100
	}
	prices[len(prices)-1] = 103
	candles := backtestCandles(prices, 1000)

	diagnostic, err := engine.LatestSignalDiagnostic(BacktestConfig{
		Symbol:         "TEST",
		InitialCapital: 100000,
		Strategy:       "intraday_momentum",
		Parameters: map[string]interface{}{
			"mode":              "breakout",
			"lookback_period":   5,
			"ema_period":        8,
			"volume_period":     12,
			"volume_multiplier": 0.9,
			"min_move_pct":      0.05,
			"min_range_pct":     0.05,
		},
	}, candles)
	if err != nil {
		t.Fatalf("diagnostic: %v", err)
	}
	if diagnostic.Signal != "BUY" || diagnostic.Confidence <= 0 || diagnostic.Reason != "latest_signal_buy" {
		t.Fatalf("unexpected diagnostic: %#v", diagnostic)
	}

	result, err := engine.RunEventDrivenOnCandles(context.Background(), BacktestConfig{
		Symbol:             "TEST",
		InitialCapital:     100000,
		Strategy:           "intraday_momentum",
		Parameters:         map[string]interface{}{"mode": "breakout", "lookback_period": 5, "ema_period": 8, "volume_period": 12, "volume_multiplier": 0.9, "min_move_pct": 0.05, "min_range_pct": 0.05},
		MaxPositionPercent: 10,
	}, candles)
	if err != nil {
		t.Fatalf("backtest: %v", err)
	}
	if result.SignalActivity.BuySignals == 0 {
		t.Fatalf("expected intraday momentum signal activity, got %#v", result.SignalActivity)
	}
}

func makeIncreasingPrices(count int, start, step float64) []float64 {
	values := make([]float64, count)
	for i := range values {
		values[i] = start + float64(i)*step
	}
	return values
}
