package cli

import (
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestBuildPaperCandidateHealthResultFlagsStaleNoPredictions(t *testing.T) {
	now := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	result := buildPaperCandidateHealthResult(models.PaperCandidate{
		ID:         "C1",
		Status:     models.PaperCandidateStatusActive,
		Symbol:     "HDFCBANK",
		Strategy:   "multi_indicator",
		PromotedAt: now.AddDate(0, 0, -20),
	}, nil, now, 14)

	if result.Health != "WARN" || len(result.Flags) != 1 || result.Flags[0] != "stale_no_predictions" {
		t.Fatalf("expected stale_no_predictions warning, got %#v", result)
	}
}

func TestBuildPaperCandidateHealthResultTracksEvidence(t *testing.T) {
	now := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	result := buildPaperCandidateHealthResult(models.PaperCandidate{
		ID:         "C1",
		Status:     models.PaperCandidateStatusActive,
		Symbol:     "HDFCBANK",
		Strategy:   "multi_indicator",
		PromotedAt: now.AddDate(0, 0, -2),
	}, []models.PaperPrediction{
		{
			ID:         "P1",
			CreatedAt:  now.Add(-2 * time.Hour),
			Evaluated:  true,
			Outcome:    "RIGHT",
			PnLPercent: 1,
		},
		{
			ID:        "P2",
			CreatedAt: now.Add(-time.Hour),
		},
	}, now, 14)

	if result.Health != "INFO" || result.TotalPredictions != 2 || result.ActivePredictions != 1 || result.Evaluated != 1 || result.Decisive != 1 {
		t.Fatalf("unexpected health result: %#v", result)
	}
	if result.LastPredictionAt.IsZero() || result.LastEvaluatedAt.IsZero() {
		t.Fatalf("expected last prediction/evaluation timestamps: %#v", result)
	}
}
