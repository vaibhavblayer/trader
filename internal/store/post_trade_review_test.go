package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

func TestSQLitePostTradeReviewReport(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "post_trade_review.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	now := time.Now()
	prediction := &models.PaperPrediction{
		ID:          "PRED_REVIEW",
		Symbol:      "RELIANCE",
		Action:      "BUY",
		Confidence:  82,
		EntryPrice:  100,
		TargetPrice: 106,
		StopLoss:    98,
		TimeWindow:  time.Hour,
		CreatedAt:   now.Add(-30 * time.Minute),
		ExpiresAt:   now.Add(30 * time.Minute),
		Reasoning:   "review setup",
		SetupName:   "llm_hard_gates",
		Timeframe:   "1h",
		Gates: []models.PaperPredictionGate{
			{Name: "rsi_regime", Passed: true},
			{Name: "volume_expansion", Passed: true},
		},
		Evaluated:  true,
		ExitPrice:  106,
		Outcome:    "RIGHT",
		PnLPercent: 6,
	}
	if err := store.SavePaperPrediction(ctx, prediction); err != nil {
		t.Fatalf("save prediction: %v", err)
	}
	if err := store.AppendPaperLedger(ctx, &broker.PaperLedgerEvent{
		Timestamp: now.Add(-5 * time.Minute),
		Type:      "ORDER_PLACED",
		RefID:     "PAPER_REVIEW_ENTRY",
		Symbol:    "RELIANCE",
		Payload: map[string]interface{}{
			"status":         "COMPLETE",
			"side":           "BUY",
			"order_type":     "MARKET",
			"quantity":       10,
			"filled_qty":     10,
			"expected_price": 100.0,
			"actual_price":   100.4,
			"slippage_bp":    40.0,
			"order_value":    1004.0,
			"costs":          2.5,
		},
	}); err != nil {
		t.Fatalf("append ledger: %v", err)
	}
	if err := store.LogTrade(ctx, &models.Trade{
		ID:           "TRADE_REVIEW",
		Timestamp:    now,
		Symbol:       "RELIANCE",
		Exchange:     models.NSE,
		Side:         models.OrderSideBuy,
		Product:      models.ProductCNC,
		Quantity:     10,
		EntryPrice:   100,
		ExitPrice:    106,
		PnL:          60,
		PnLPercent:   6,
		Strategy:     "llm_hard_gates",
		OrderIDs:     []string{"PAPER_REVIEW_ENTRY"},
		IsPaper:      true,
		HoldDuration: time.Hour,
	}); err != nil {
		t.Fatalf("log trade: %v", err)
	}

	paperOnly := true
	report, err := store.GetPostTradeReviewReport(ctx, models.PostTradeReviewFilter{
		StartDate: now.Add(-24 * time.Hour),
		EndDate:   now.Add(time.Hour),
		IsPaper:   &paperOnly,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("review report: %v", err)
	}
	if report.TotalTrades != 1 || report.WithPrediction != 1 || report.WithExecution != 1 {
		t.Fatalf("unexpected link counts: %#v", report)
	}
	if report.Winners != 1 || report.NetPnL != 60 || report.AvgPnLPercent != 6 {
		t.Fatalf("unexpected performance stats: %#v", report)
	}
	if report.AvgSlippageBp != 40 || report.TotalCosts != 2.5 {
		t.Fatalf("unexpected execution stats: %#v", report)
	}
	if len(report.Trades) != 1 || report.Trades[0].PredictionID != prediction.ID || report.Trades[0].GatesPassed != 2 {
		t.Fatalf("unexpected trade review: %#v", report.Trades)
	}
	if len(report.BySetup) != 1 || report.BySetup[0].Key != "llm_hard_gates" {
		t.Fatalf("unexpected setup grouping: %#v", report.BySetup)
	}
}

func TestSQLitePostTradeReviewFlagsMissingLinks(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "post_trade_review_missing.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	now := time.Now()
	if err := store.LogTrade(ctx, &models.Trade{
		ID:         "TRADE_MISSING",
		Timestamp:  now,
		Symbol:     "INFY",
		Exchange:   models.NSE,
		Side:       models.OrderSideSell,
		Product:    models.ProductCNC,
		Quantity:   5,
		EntryPrice: 200,
		ExitPrice:  198,
		PnL:        10,
		PnLPercent: 1,
		OrderIDs:   []string{"UNKNOWN_ORDER"},
		IsPaper:    true,
	}); err != nil {
		t.Fatalf("log trade: %v", err)
	}

	report, err := store.GetPostTradeReviewReport(ctx, models.PostTradeReviewFilter{Limit: 10})
	if err != nil {
		t.Fatalf("review report: %v", err)
	}
	if report.MissingPrediction != 1 || report.MissingExecution != 1 {
		t.Fatalf("expected missing links, got %#v", report)
	}
	if len(report.Trades) != 1 || len(report.Trades[0].ReviewFlags) != 2 {
		t.Fatalf("expected missing review flags, got %#v", report.Trades)
	}
}
