package setup

import (
	"context"
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestEngineProducesExecutableBullishSetup(t *testing.T) {
	candles := trendCandles(90, 100, 0.18, 10000)
	candles[len(candles)-1].Volume = 18000

	cfg := DefaultConfig()
	cfg.MaxVWAPDeviationPct = 10
	cfg.RequireMTFAlignment = true
	engine := NewEngine(cfg)

	result, err := engine.Evaluate(context.Background(), Request{
		Symbol:  "TEST",
		Candles: candles,
		HigherTimeframes: map[string][]models.Candle{
			"15min": trendCandles(80, 100, 0.12, 12000),
		},
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if result.Action != ActionBuy {
		t.Fatalf("expected BUY setup, got %s with invalidations %v and indicators %+v", result.Action, result.Invalidations, result.Indicators)
	}
	if result.EntryPrice <= 0 || result.StopLoss >= result.EntryPrice || len(result.Targets) == 0 {
		t.Fatalf("invalid bullish risk geometry: entry %.2f sl %.2f targets %v", result.EntryPrice, result.StopLoss, result.Targets)
	}
	if result.RiskReward < cfg.MinRiskReward {
		t.Fatalf("expected risk reward >= %.2f, got %.2f", cfg.MinRiskReward, result.RiskReward)
	}
	assertGate(t, result, "volume", true)
	assertGate(t, result, "mtf_alignment", true)
}

func TestEngineBlocksWhenHigherTimeframeOpposes(t *testing.T) {
	candles := trendCandles(90, 100, 0.18, 10000)
	candles[len(candles)-1].Volume = 18000

	cfg := DefaultConfig()
	cfg.MaxVWAPDeviationPct = 10
	engine := NewEngine(cfg)

	result, err := engine.Evaluate(context.Background(), Request{
		Symbol:  "TEST",
		Candles: candles,
		HigherTimeframes: map[string][]models.Candle{
			"15min": trendCandles(80, 130, -0.12, 12000),
		},
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if result.Action != ActionNoTrade {
		t.Fatalf("expected NO_TRADE, got %s", result.Action)
	}
	assertGate(t, result, "mtf_alignment", false)
}

func TestEngineBlocksWeakVolume(t *testing.T) {
	candles := trendCandles(90, 100, 0.18, 10000)
	candles[len(candles)-1].Volume = 9000

	cfg := DefaultConfig()
	cfg.MaxVWAPDeviationPct = 10
	cfg.RequireMTFAlignment = false
	engine := NewEngine(cfg)

	result, err := engine.Evaluate(context.Background(), Request{
		Symbol:  "TEST",
		Candles: candles,
	})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if result.Action != ActionNoTrade {
		t.Fatalf("expected NO_TRADE, got %s", result.Action)
	}
	assertGate(t, result, "volume", false)
}

func TestEngineAsOfIndexIgnoresFutureCandles(t *testing.T) {
	prefix := trendCandles(90, 100, 0.18, 10000)
	prefix[len(prefix)-1].Volume = 18000
	future := append([]models.Candle{}, prefix...)
	future = append(future, trendCandles(20, 130, -0.9, 50000)...)

	cfg := DefaultConfig()
	cfg.MaxVWAPDeviationPct = 10
	cfg.RequireMTFAlignment = false
	engine := NewEngine(cfg)

	prefixResult, err := engine.Evaluate(context.Background(), Request{
		Symbol:  "TEST",
		Candles: prefix,
	})
	if err != nil {
		t.Fatalf("prefix Evaluate returned error: %v", err)
	}
	asOfResult, err := engine.Evaluate(context.Background(), Request{
		Symbol:    "TEST",
		Candles:   future,
		AsOfIndex: len(prefix) - 1,
	})
	if err != nil {
		t.Fatalf("as-of Evaluate returned error: %v", err)
	}
	if prefixResult.Action != asOfResult.Action ||
		prefixResult.Regime != asOfResult.Regime ||
		prefixResult.Indicators.Close != asOfResult.Indicators.Close {
		t.Fatalf("future data changed as-of result: prefix=%s/%s/%.2f asof=%s/%s/%.2f",
			prefixResult.Action, prefixResult.Regime, prefixResult.Indicators.Close,
			asOfResult.Action, asOfResult.Regime, asOfResult.Indicators.Close)
	}
}

func trendCandles(count int, start, step float64, volume int64) []models.Candle {
	candles := make([]models.Candle, count)
	ts := time.Date(2026, 1, 1, 10, 0, 0, 0, time.FixedZone("IST", 5*60*60+30*60))
	price := start
	for i := range candles {
		open := price
		close := price + step
		high := max(open, close) + 0.08
		low := min(open, close) - 0.08
		candles[i] = models.Candle{
			Timestamp: ts.Add(time.Duration(i) * time.Minute),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
		}
		price = close
	}
	return candles
}

func assertGate(t *testing.T, result *Setup, name string, want bool) {
	t.Helper()
	for _, gate := range result.Gates {
		if gate.Name == name {
			if gate.Passed != want {
				t.Fatalf("gate %s passed=%v, want %v; reason=%s", name, gate.Passed, want, gate.Reason)
			}
			return
		}
	}
	t.Fatalf("gate %s not found in %+v", name, result.Gates)
}
