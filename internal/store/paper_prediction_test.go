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
