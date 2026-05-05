package cli

import (
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestBuildPaperCandidateReviewPausesNegativeForwardEdge(t *testing.T) {
	now := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	candidate := models.PaperCandidate{
		ID:             "HDFCBANK_multi_fast",
		Status:         models.PaperCandidateStatusActive,
		Symbol:         "HDFCBANK",
		Strategy:       "multi_indicator",
		ParamVariant:   "fast",
		MaxDrawdownPct: 5,
	}
	predictions := []models.PaperPrediction{
		reviewPrediction("P1", now.Add(-4*time.Hour), "RIGHT", 1.0),
		reviewPrediction("P2", now.Add(-3*time.Hour), "WRONG", -2.0),
		reviewPrediction("P3", now.Add(-2*time.Hour), "WRONG", -1.5),
		reviewPrediction("P4", now.Add(-time.Hour), "WRONG", -1.0),
	}

	result := buildPaperCandidateReview(candidate, predictions, paperCandidateReviewOptions{
		MinDecisive:    3,
		MinEvaluated:   10,
		MinExpectancy:  0,
		MinPF:          1,
		MaxExpiredRate: 60,
		MaxLossStreak:  5,
		MaxDDMultiple:  1.5,
	})

	if result.Action != "PAUSE" {
		t.Fatalf("expected pause, got %#v", result)
	}
	if result.Expectancy >= 0 || result.ProfitFactor >= 1 {
		t.Fatalf("expected weak stats, got %#v", result)
	}
}

func TestBuildPaperCandidateReviewKeepsInsufficientSample(t *testing.T) {
	now := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	candidate := models.PaperCandidate{
		ID:           "HDFCBANK_multi_fast",
		Status:       models.PaperCandidateStatusActive,
		Symbol:       "HDFCBANK",
		Strategy:     "multi_indicator",
		ParamVariant: "fast",
	}
	predictions := []models.PaperPrediction{
		reviewPrediction("P1", now.Add(-time.Hour), "WRONG", -2.0),
	}

	result := buildPaperCandidateReview(candidate, predictions, paperCandidateReviewOptions{
		MinDecisive:    5,
		MinEvaluated:   10,
		MinExpectancy:  0,
		MinPF:          1,
		MaxExpiredRate: 60,
		MaxLossStreak:  5,
		MaxDDMultiple:  1.5,
	})

	if result.Action != "KEEP" || result.Reason != "forward_evidence_ok" {
		t.Fatalf("expected keep with small sample, got %#v", result)
	}
}

func TestBuildPaperCandidateReviewMarksGraduationReady(t *testing.T) {
	now := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	candidate := models.PaperCandidate{
		ID:             "HDFCBANK_multi_fast",
		Status:         models.PaperCandidateStatusActive,
		Symbol:         "HDFCBANK",
		Strategy:       "multi_indicator",
		ParamVariant:   "fast",
		MaxDrawdownPct: 5,
	}
	predictions := []models.PaperPrediction{
		reviewPrediction("P1", now.Add(-5*time.Hour), "RIGHT", 1.0),
		reviewPrediction("P2", now.Add(-4*time.Hour), "RIGHT", 1.2),
		reviewPrediction("P3", now.Add(-3*time.Hour), "RIGHT", 0.8),
		reviewPrediction("P4", now.Add(-2*time.Hour), "WRONG", -0.4),
		reviewPrediction("P5", now.Add(-time.Hour), "RIGHT", 1.0),
	}

	result := buildPaperCandidateReview(candidate, predictions, paperCandidateReviewOptions{
		MinDecisive:    3,
		MinEvaluated:   5,
		MinExpectancy:  0,
		MinPF:          1,
		MaxExpiredRate: 60,
		MaxLossStreak:  5,
		MaxDDMultiple:  1.5,
		Graduate: paperCandidateGraduationOptions{
			MinEvaluated:   5,
			MinDecisive:    5,
			MinExpectancy:  0.2,
			MinPF:          1.2,
			MaxExpiredRate: 20,
			MaxDDMultiple:  1,
		},
	})

	if result.Action != "READY" || !result.GraduationReady {
		t.Fatalf("expected graduation ready, got %#v", result)
	}
}

func reviewPrediction(id string, created time.Time, outcome string, pnl float64) models.PaperPrediction {
	return models.PaperPrediction{
		ID:          id,
		Symbol:      "HDFCBANK",
		Action:      "BUY",
		EntryPrice:  100,
		TargetPrice: 102,
		StopLoss:    99,
		TimeWindow:  time.Hour,
		CreatedAt:   created,
		ExpiresAt:   created.Add(time.Hour),
		SetupName:   "candidate:HDFCBANK_multi_fast",
		Timeframe:   "15min",
		Evaluated:   true,
		ExitPrice:   100 + pnl,
		Outcome:     outcome,
		PnLPercent:  pnl,
	}
}
