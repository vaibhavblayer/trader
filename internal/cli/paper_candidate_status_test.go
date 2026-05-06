package cli

import (
	"context"
	"path/filepath"
	"testing"

	"zerodha-trader/internal/models"
	storepkg "zerodha-trader/internal/store"
)

func TestSetPaperCandidateStatus(t *testing.T) {
	ctx := context.Background()
	dataStore, err := storepkg.NewSQLiteStore(filepath.Join(t.TempDir(), "candidate_status.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer dataStore.Close()

	candidate := &models.PaperCandidate{
		ID:                "reliance_nse_intraday_momentum_micro_15minute_sl1tp2_30",
		Status:            models.PaperCandidateStatusActive,
		Symbol:            "RELIANCE",
		Exchange:          "NSE",
		Strategy:          "intraday_momentum",
		ParamVariant:      "micro",
		Timeframe:         "15minute",
		Setup:             "sl1tp2",
		Source:            "test",
		Verdict:           "PASS",
		Days:              30,
		Candles:           100,
		CandidateScore:    82.5,
		EvidenceScore:     70,
		EvidenceSentiment: "mixed",
	}
	if err := dataStore.SavePaperCandidate(ctx, candidate); err != nil {
		t.Fatalf("save candidate: %v", err)
	}

	paused, err := setPaperCandidateStatus(ctx, dataStore, candidate.ID, models.PaperCandidateStatusPaused)
	if err != nil {
		t.Fatalf("pause candidate: %v", err)
	}
	if paused.OldStatus != models.PaperCandidateStatusActive || paused.NewStatus != models.PaperCandidateStatusPaused {
		t.Fatalf("unexpected pause result: %#v", paused)
	}

	loaded, err := dataStore.GetPaperCandidates(ctx, models.PaperCandidateFilter{ID: candidate.ID})
	if err != nil {
		t.Fatalf("load paused candidate: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Status != models.PaperCandidateStatusPaused {
		t.Fatalf("candidate not paused: %#v", loaded)
	}
	if loaded[0].CandidateScore != candidate.CandidateScore || loaded[0].EvidenceScore != candidate.EvidenceScore {
		t.Fatalf("status update lost scoring metadata: %#v", loaded[0])
	}

	activated, err := setPaperCandidateStatus(ctx, dataStore, candidate.ID, models.PaperCandidateStatusActive)
	if err != nil {
		t.Fatalf("activate candidate: %v", err)
	}
	if activated.OldStatus != models.PaperCandidateStatusPaused || activated.NewStatus != models.PaperCandidateStatusActive {
		t.Fatalf("unexpected activate result: %#v", activated)
	}
}
