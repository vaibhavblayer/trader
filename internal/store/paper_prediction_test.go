package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestSQLitePaperPredictionRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "paper_predictions.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	prediction := &models.PaperPrediction{
		ID:          "PRED_1",
		Symbol:      "RELIANCE",
		Action:      "BUY",
		Confidence:  72,
		EntryPrice:  2500,
		TargetPrice: 2550,
		StopLoss:    2475,
		TimeWindow:  15 * time.Minute,
		CreatedAt:   time.Now().Add(-time.Minute),
		ExpiresAt:   time.Now().Add(14 * time.Minute),
		Reasoning:   "test setup",
	}
	if err := store.SavePaperPrediction(ctx, prediction); err != nil {
		t.Fatalf("save prediction: %v", err)
	}

	prediction.Evaluated = true
	prediction.ExitPrice = 2560
	prediction.Outcome = "RIGHT"
	prediction.PnLPercent = 2.4
	if err := store.SavePaperPrediction(ctx, prediction); err != nil {
		t.Fatalf("update prediction: %v", err)
	}

	evaluated := true
	got, err := store.GetPaperPredictions(ctx, PaperPredictionFilter{Evaluated: &evaluated})
	if err != nil {
		t.Fatalf("get predictions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 prediction, got %d", len(got))
	}
	if got[0].ID != prediction.ID || got[0].Outcome != "RIGHT" || got[0].TimeWindow != 15*time.Minute {
		t.Fatalf("prediction not restored: %#v", got[0])
	}
}

func TestSQLitePaperPredictionReport(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "paper_prediction_report.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	now := time.Now()
	predictions := []*models.PaperPrediction{
		{
			ID:          "PRED_RIGHT",
			Symbol:      "RELIANCE",
			Action:      "BUY",
			Confidence:  85,
			EntryPrice:  100,
			TargetPrice: 105,
			StopLoss:    98,
			TimeWindow:  time.Hour,
			CreatedAt:   now.Add(-3 * time.Hour),
			ExpiresAt:   now.Add(-2 * time.Hour),
			Evaluated:   true,
			ExitPrice:   106,
			Outcome:     "RIGHT",
			PnLPercent:  6,
		},
		{
			ID:          "PRED_WRONG",
			Symbol:      "RELIANCE",
			Action:      "BUY",
			Confidence:  82,
			EntryPrice:  100,
			TargetPrice: 105,
			StopLoss:    98,
			TimeWindow:  time.Hour,
			CreatedAt:   now.Add(-2 * time.Hour),
			ExpiresAt:   now.Add(-time.Hour),
			Evaluated:   true,
			ExitPrice:   97,
			Outcome:     "WRONG",
			PnLPercent:  -3,
		},
		{
			ID:          "PRED_EXPIRED",
			Symbol:      "INFY",
			Action:      "SELL",
			Confidence:  65,
			EntryPrice:  100,
			TargetPrice: 95,
			StopLoss:    103,
			TimeWindow:  time.Hour,
			CreatedAt:   now.Add(-time.Hour),
			ExpiresAt:   now,
			Evaluated:   true,
			ExitPrice:   99,
			Outcome:     "EXPIRED",
			PnLPercent:  1,
		},
	}
	for _, prediction := range predictions {
		if err := store.SavePaperPrediction(ctx, prediction); err != nil {
			t.Fatalf("save %s: %v", prediction.ID, err)
		}
	}

	report, err := store.GetPaperPredictionReport(ctx, PaperPredictionFilter{})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if report.TotalPredictions != 3 || report.Decisive != 2 || report.ExpiredPredictions != 1 {
		t.Fatalf("unexpected report counts: %#v", report)
	}
	if report.WinRate != 50 {
		t.Fatalf("expected win rate 50, got %.2f", report.WinRate)
	}
	if report.Expectancy != 1.5 {
		t.Fatalf("expected decisive expectancy 1.5, got %.2f", report.Expectancy)
	}
	if len(report.ByConfidence) == 0 || len(report.BySymbol) != 2 || len(report.ByAction) != 2 {
		t.Fatalf("expected grouped stats, got %#v", report)
	}
}
