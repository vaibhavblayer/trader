package cli

import (
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestParseCSVFlagPreservesTimeframeCase(t *testing.T) {
	values := parseCSVFlag("15min, 1day,15min")
	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d: %#v", len(values), values)
	}
	if values[0] != "15min" || values[1] != "1day" {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestParseBacktestSetups(t *testing.T) {
	setups, err := parseBacktestSetups("base,sl2tp4,short_sl2tp4")
	if err != nil {
		t.Fatalf("parse setups: %v", err)
	}
	if len(setups) != 3 {
		t.Fatalf("expected 3 setups, got %d", len(setups))
	}
	if setups[0].StopLoss != 3 || setups[1].TakeProfit != 4 || !setups[2].AllowShort {
		t.Fatalf("unexpected setups: %#v", setups)
	}
}

func TestReliabilityVerdict(t *testing.T) {
	thresholds := backtestGridThresholds{
		MinTrades:           20,
		MinValidationTrades: 5,
		MinProfitFactor:     1.2,
		MinReturn:           0,
		MinValidationReturn: 0,
		MaxDrawdown:         10,
	}

	pass := backtestGridResult{
		Trades:              25,
		ValidationTrades:    6,
		ReturnPct:           8,
		TrainReturnPct:      4,
		ValidationReturnPct: 3,
		ProfitFactor:        1.6,
		Expectancy:          100,
		MaxDrawdownPct:      6,
	}
	verdict, reason := reliabilityVerdict(pass, thresholds)
	if verdict != "PASS" || reason != "" {
		t.Fatalf("expected PASS, got %s %s", verdict, reason)
	}

	watch := pass
	watch.Trades = 8
	verdict, reason = reliabilityVerdict(watch, thresholds)
	if verdict != "WATCH" || reason != "low_trade_count" {
		t.Fatalf("expected WATCH low_trade_count, got %s %s", verdict, reason)
	}

	reject := pass
	reject.ValidationReturnPct = -1
	verdict, reason = reliabilityVerdict(reject, thresholds)
	if verdict != "REJECT" || reason != "validation_return_below_min" {
		t.Fatalf("expected validation reject, got %s %s", verdict, reason)
	}

	reject = pass
	reject.TrainReturnPct = -1
	verdict, reason = reliabilityVerdict(reject, thresholds)
	if verdict != "REJECT" || reason != "train_return_below_min" {
		t.Fatalf("expected train reject, got %s %s", verdict, reason)
	}
}

func TestClassifyBacktestRegime(t *testing.T) {
	candles := make([]models.Candle, 20)
	start := time.Date(2026, 1, 1, 9, 15, 0, 0, time.UTC)
	for i := range candles {
		close := 100 + float64(i)
		candles[i] = models.Candle{
			Timestamp: start.Add(time.Duration(i) * time.Hour),
			Open:      close - 0.5,
			High:      close + 1,
			Low:       close - 1,
			Close:     close,
			Volume:    1000,
		}
	}

	if regime := classifyBacktestRegime(candles); regime != "trend_up" {
		t.Fatalf("expected trend_up, got %s", regime)
	}
}
