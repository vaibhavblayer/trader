package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestSQLitePaperCandidateRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "paper_candidates.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	candidate := &models.PaperCandidate{
		ID:                  "hdfcbank_nse_multi_indicator_fast_1day_short_sl2tp4_1095",
		Status:              models.PaperCandidateStatusActive,
		Symbol:              "HDFCBANK",
		Exchange:            "NSE",
		Strategy:            "multi_indicator",
		ParamVariant:        "fast",
		Parameters:          "short_period=5;long_period=13",
		Timeframe:           "1day",
		Setup:               "short_sl2tp4",
		Source:              "backtest_grid",
		Verdict:             "PASS",
		Days:                1095,
		Candles:             742,
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
		WinRate:             44.1,
		ProfitFactor:        1.28,
		Expectancy:          1600,
		MaxDrawdownPct:      5,
		SharpeRatio:         0.7,
		CandidateScore:      78.5,
		EvidenceScore:       63,
		EvidenceSentiment:   "mixed",
		EvidenceConfidence:  72,
		EvidenceSources:     6,
		ScoreReason:         "backtest=80;evidence=63",
		StopLossPercent:     2,
		TakeProfitPercent:   4,
		AllowShort:          true,
		AllowedRegimes:      []string{"range", "trend_up"},
		BlockedRegimes:      []string{"trend_down"},
		RegimeStats: []models.PaperCandidateRegimeStat{
			{Regime: "range", Trades: 27, Wins: 11, WinRate: 40.7, TotalPnL: 43507.31, Expectancy: 1611.38},
		},
		PromotedAt: time.Now().Add(-time.Minute),
	}
	if err := store.SavePaperCandidate(ctx, candidate); err != nil {
		t.Fatalf("save candidate: %v", err)
	}

	got, err := store.GetPaperCandidates(ctx, models.PaperCandidateFilter{Symbol: "HDFCBANK", Status: models.PaperCandidateStatusActive})
	if err != nil {
		t.Fatalf("get candidates: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(got))
	}
	if got[0].ID != candidate.ID || got[0].Strategy != "multi_indicator" || !got[0].AllowShort {
		t.Fatalf("candidate not restored: %#v", got[0])
	}
	if len(got[0].AllowedRegimes) != 2 || got[0].AllowedRegimes[0] != "range" {
		t.Fatalf("allowed regimes not restored: %#v", got[0].AllowedRegimes)
	}
	if len(got[0].RegimeStats) != 1 || got[0].RegimeStats[0].Regime != "range" {
		t.Fatalf("regime stats not restored: %#v", got[0].RegimeStats)
	}
	if got[0].SignalBars != candidate.SignalBars || got[0].BuySignals != candidate.BuySignals || got[0].SignalRatePct != candidate.SignalRatePct {
		t.Fatalf("signal activity not restored: %#v", got[0])
	}
	if got[0].CandidateScore != candidate.CandidateScore || got[0].EvidenceScore != candidate.EvidenceScore || got[0].EvidenceSentiment != candidate.EvidenceSentiment {
		t.Fatalf("candidate scoring not restored: %#v", got[0])
	}

	byID, err := store.GetPaperCandidates(ctx, models.PaperCandidateFilter{ID: candidate.ID})
	if err != nil {
		t.Fatalf("get candidate by id: %v", err)
	}
	if len(byID) != 1 || byID[0].ID != candidate.ID {
		t.Fatalf("expected candidate by id, got %#v", byID)
	}
}
