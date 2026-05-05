package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestSQLitePaperExperimentRunRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "paper_experiments.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	started := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	run := &models.PaperExperimentRun{
		ID:                     "PAPER_SOAK_1",
		Source:                 "cli",
		Command:                "paper soak-run --regime-mode explore --dry-run",
		StartedAt:              started,
		FinishedAt:             started.Add(2 * time.Second),
		Symbol:                 "HDFCBANK",
		Strategy:               "multi_indicator",
		Status:                 models.PaperCandidateStatusActive,
		RegimeMode:             "explore",
		DryRun:                 true,
		Limit:                  5,
		CandidateDays:          120,
		MinCandles:             80,
		RegimeWindow:           50,
		TimeWindow:             24 * time.Hour,
		EvaluateDays:           30,
		ReviewDays:             90,
		CandidatesLoaded:       1,
		CandidatesChecked:      1,
		PredictionsCreated:     0,
		Blocked:                0,
		NoSignal:               1,
		ExploratoryPredictions: 2,
		ExploratoryDecisive:    1,
		ReadinessDecision:      "WARN",
		ReadinessReasons:       []string{"need at least 20 decisive samples, got 0"},
	}
	if err := store.SavePaperExperimentRun(ctx, run); err != nil {
		t.Fatalf("save run: %v", err)
	}

	got, err := store.GetPaperExperimentRuns(ctx, models.PaperExperimentRunFilter{
		Symbol:     "HDFCBANK",
		RegimeMode: "explore",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("get runs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 run, got %d", len(got))
	}
	if got[0].ID != run.ID || got[0].RegimeMode != "explore" || !got[0].DryRun {
		t.Fatalf("run not restored: %#v", got[0])
	}
	if got[0].TimeWindow != 24*time.Hour || len(got[0].ReadinessReasons) != 1 {
		t.Fatalf("duration or reasons not restored: %#v", got[0])
	}
}
