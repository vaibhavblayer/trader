package cli

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"zerodha-trader/internal/store"
)

func TestPersistentPaperTrackerRestoresPredictionsAndStats(t *testing.T) {
	ctx := context.Background()
	dataStore, err := store.NewSQLiteStore(filepath.Join(t.TempDir(), "tracker.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer dataStore.Close()

	tracker, err := NewPersistentPaperTracker(ctx, dataStore)
	if err != nil {
		t.Fatalf("new tracker: %v", err)
	}
	tracker.AddPrediction(&Prediction{
		Symbol:      "RELIANCE",
		Action:      "BUY",
		Confidence:  70,
		EntryPrice:  100,
		TargetPrice: 105,
		StopLoss:    98,
		TimeWindow:  time.Hour,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Hour),
		SetupName:   "llm_simple",
		Timeframe:   "1h",
	})

	active := tracker.GetActivePredictions()
	if len(active) != 1 {
		t.Fatalf("expected active prediction, got %d", len(active))
	}
	if got := tracker.EvaluatePrediction(active[0].ID, 106); got == nil || got.Outcome != "RIGHT" {
		t.Fatalf("expected right prediction, got %#v", got)
	}

	restored, err := NewPersistentPaperTracker(ctx, dataStore)
	if err != nil {
		t.Fatalf("restore tracker: %v", err)
	}
	if len(restored.GetActivePredictions()) != 0 {
		t.Fatal("expected no active predictions after evaluation")
	}
	history := restored.GetRecentHistory(10)
	if len(history) != 1 || history[0].Outcome != "RIGHT" {
		t.Fatalf("expected restored evaluated history, got %#v", history)
	}
	stats := restored.GetStats()
	if stats.TotalPredictions != 1 || stats.RightPredictions != 1 || stats.WinRate != 100 {
		t.Fatalf("expected restored stats, got %#v", stats)
	}
}
