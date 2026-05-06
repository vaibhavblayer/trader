package cli

import (
	"context"
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

type fakePaperCandidateStore struct {
	candidates []models.PaperCandidate
}

func (s *fakePaperCandidateStore) SavePaperCandidate(_ context.Context, candidate *models.PaperCandidate) error {
	s.candidates = append(s.candidates, *candidate)
	return nil
}

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
	setups, err := parseBacktestSetups("base,sl1tp2,sl2tp4,short_sl1tp2,short_sl2tp4")
	if err != nil {
		t.Fatalf("parse setups: %v", err)
	}
	if len(setups) != 5 {
		t.Fatalf("expected 5 setups, got %d", len(setups))
	}
	if setups[0].StopLoss != 3 || setups[1].TakeProfit != 2 || setups[2].TakeProfit != 4 || !setups[3].AllowShort || setups[3].StopLoss != 1 || !setups[4].AllowShort {
		t.Fatalf("unexpected setups: %#v", setups)
	}
}

func TestStrategyParameterVariants(t *testing.T) {
	variants := strategyParameterVariants("supertrend", "research")
	if len(variants) < 3 {
		t.Fatalf("expected research variants, got %#v", variants)
	}
	if variants[0].Name == "" || variants[0].ParameterS == "" {
		t.Fatalf("expected named variant with formatted params: %#v", variants[0])
	}

	defaults := strategyParameterVariants("supertrend", "default")
	if len(defaults) != 1 || defaults[0].Name != "default" || defaults[0].ParameterS != "" {
		t.Fatalf("unexpected default variants: %#v", defaults)
	}

	intraday := strategyParameterVariants("intraday_momentum", "research")
	if len(intraday) < 4 {
		t.Fatalf("expected intraday momentum research variants, got %#v", intraday)
	}
	if intraday[0].Name == "" || intraday[0].ParameterS == "" {
		t.Fatalf("expected named intraday variant with formatted params: %#v", intraday[0])
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

func TestAggregateBacktestRegimeSummary(t *testing.T) {
	results := []backtestGridResult{
		{
			Verdict:      "WATCH",
			Strategy:     "supertrend",
			ParamVariant: "atr10_mult3",
			Setup:        "sl2tp4",
			Regimes: []backtestRegimeTradeStat{
				{Regime: "trend_up", Trades: 2, Wins: 1, TotalPnL: 1000, Expectancy: 500, AvgHoldBars: 5},
			},
		},
		{
			Verdict:      "REJECT",
			Strategy:     "supertrend",
			ParamVariant: "atr10_mult3",
			Setup:        "sl2tp4",
			Regimes: []backtestRegimeTradeStat{
				{Regime: "trend_up", Trades: 1, Wins: 1, TotalPnL: 500, Expectancy: 500, AvgHoldBars: 3},
			},
		},
	}

	summary := aggregateBacktestRegimeSummary(results)
	if len(summary) != 1 {
		t.Fatalf("expected 1 summary row, got %d", len(summary))
	}
	if summary[0].Trades != 3 || summary[0].Wins != 2 || summary[0].BestVerdict != "WATCH" {
		t.Fatalf("unexpected summary: %#v", summary[0])
	}
	if summary[0].Expectancy != 500 {
		t.Fatalf("expected expectancy 500, got %.2f", summary[0].Expectancy)
	}
}

func TestPromoteBacktestGridCandidatesUsesRegimeGuardrails(t *testing.T) {
	store := &fakePaperCandidateStore{}
	results := []backtestGridResult{
		{
			Verdict:             "PASS",
			Symbol:              "HDFCBANK",
			Exchange:            "NSE",
			Strategy:            "multi_indicator",
			ParamVariant:        "fast",
			Timeframe:           "1day",
			Setup:               "short_sl2tp4",
			Days:                1095,
			SignalBars:          700,
			BuySignals:          12,
			SellSignals:         10,
			HoldSignals:         678,
			DirectionalSignals:  22,
			SignalRatePct:       3.14,
			TradeConversionPct:  90.91,
			Trades:              34,
			ValidationTrades:    10,
			ReturnPct:           5.57,
			TrainReturnPct:      4.8,
			ValidationReturnPct: 0.77,
			ProfitFactor:        1.28,
			AllowShort:          true,
			Regimes: []backtestRegimeTradeStat{
				{Regime: "range", Trades: 27, Expectancy: 1611},
				{Regime: "trend_down", Trades: 2, Expectancy: -5762},
				{Regime: "thin_sample", Trades: 1, Expectancy: 10000},
			},
		},
		{Verdict: "REJECT", Symbol: "INFY", Strategy: "supertrend"},
	}

	applyEvidenceAwareCandidateScores(results, nil, nil)
	promoted, err := promoteBacktestGridCandidates(context.Background(), store, results, map[string]bool{"PASS": true}, models.PaperCandidateStatusActive, 2, 0)
	if err != nil {
		t.Fatalf("promote candidates: %v", err)
	}
	if len(promoted) != 1 || len(store.candidates) != 1 {
		t.Fatalf("expected one promoted candidate, got %d/%d", len(promoted), len(store.candidates))
	}
	if promoted[0].ID == "" || promoted[0].AllowedRegimes[0] != "range" {
		t.Fatalf("unexpected promoted candidate: %#v", promoted[0])
	}
	if len(promoted[0].BlockedRegimes) != 1 || promoted[0].BlockedRegimes[0] != "trend_down" {
		t.Fatalf("unexpected blocked regimes: %#v", promoted[0].BlockedRegimes)
	}
	if promoted[0].SignalBars != 700 || promoted[0].BuySignals != 12 || promoted[0].SignalRatePct != 3.14 {
		t.Fatalf("signal activity not promoted: %#v", promoted[0])
	}
}

func TestParseParameterString(t *testing.T) {
	params := parseParameterString("short_period=5;multiplier=2.5;require_adx=true;label=fast")
	if params["short_period"] != 5 {
		t.Fatalf("expected int param, got %#v", params["short_period"])
	}
	if params["multiplier"] != 2.5 {
		t.Fatalf("expected float param, got %#v", params["multiplier"])
	}
	if params["require_adx"] != true {
		t.Fatalf("expected bool param, got %#v", params["require_adx"])
	}
	if params["label"] != "fast" {
		t.Fatalf("expected string param, got %#v", params["label"])
	}
}
