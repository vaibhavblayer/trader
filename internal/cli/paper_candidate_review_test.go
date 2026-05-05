package cli

import (
	"strings"
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
		reviewPrediction("P1", now.Add(-4*time.Hour), "RIGHT", 1.0, "range"),
		reviewPrediction("P2", now.Add(-3*time.Hour), "WRONG", -2.0, "range"),
		reviewPrediction("P3", now.Add(-2*time.Hour), "WRONG", -1.5, "range"),
		reviewPrediction("P4", now.Add(-time.Hour), "WRONG", -1.0, "range"),
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
		reviewPrediction("P1", now.Add(-time.Hour), "WRONG", -2.0, "range"),
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
		reviewPrediction("P1", now.Add(-5*time.Hour), "RIGHT", 1.0, "trend_up"),
		reviewPrediction("P2", now.Add(-4*time.Hour), "RIGHT", 1.2, "trend_up"),
		reviewPrediction("P3", now.Add(-3*time.Hour), "RIGHT", 0.8, "trend_up"),
		reviewPrediction("P4", now.Add(-2*time.Hour), "WRONG", -0.4, "trend_up"),
		reviewPrediction("P5", now.Add(-time.Hour), "RIGHT", 1.0, "trend_up"),
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

func TestBuildPaperCandidateReviewFlagsWeakForwardRegime(t *testing.T) {
	now := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	candidate := models.PaperCandidate{
		ID:           "HDFCBANK_multi_fast",
		Status:       models.PaperCandidateStatusActive,
		Symbol:       "HDFCBANK",
		Strategy:     "multi_indicator",
		ParamVariant: "fast",
	}
	predictions := []models.PaperPrediction{
		reviewPrediction("P1", now.Add(-5*time.Hour), "RIGHT", 1.5, "trend_up"),
		reviewPrediction("P2", now.Add(-4*time.Hour), "RIGHT", 1.2, "trend_up"),
		reviewPrediction("P3", now.Add(-3*time.Hour), "WRONG", -1.0, "range"),
		reviewPrediction("P4", now.Add(-2*time.Hour), "WRONG", -1.0, "range"),
		reviewPrediction("P5", now.Add(-time.Hour), "WRONG", -1.0, "range"),
	}

	result := buildPaperCandidateReview(candidate, predictions, paperCandidateReviewOptions{
		MinDecisive:       10,
		MinRegimeDecisive: 3,
		MinEvaluated:      10,
		MinExpectancy:     0,
		MinPF:             1,
		MaxExpiredRate:    60,
		MaxLossStreak:     5,
		MaxDDMultiple:     1.5,
	})

	if len(result.RegimeStats) != 2 {
		t.Fatalf("expected two regime groups, got %#v", result.RegimeStats)
	}
	if len(result.RegimeFlags) != 1 || !strings.Contains(result.RegimeFlags[0], "range") {
		t.Fatalf("expected range regime flag, got %#v", result.RegimeFlags)
	}
}

func TestBuildPaperCandidateReviewSeparatesExploratoryEvidence(t *testing.T) {
	now := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	candidate := models.PaperCandidate{
		ID:           "HDFCBANK_multi_fast",
		Status:       models.PaperCandidateStatusActive,
		Symbol:       "HDFCBANK",
		Strategy:     "multi_indicator",
		ParamVariant: "fast",
	}
	predictions := []models.PaperPrediction{
		reviewPrediction("P1", now.Add(-2*time.Hour), "RIGHT", 1.0, "trend_up"),
		exploratoryReviewPrediction("P2", now.Add(-time.Hour), "WRONG", -5.0, "range"),
	}

	result := buildPaperCandidateReview(candidate, predictions, paperCandidateReviewOptions{
		MinDecisive:    1,
		MinEvaluated:   1,
		MinExpectancy:  0,
		MinPF:          1,
		MaxExpiredRate: 60,
		MaxLossStreak:  5,
		MaxDDMultiple:  1.5,
	})

	if result.TotalPredictions != 2 || result.TrustedPredictions != 1 || result.ExploratoryPredictions != 1 {
		t.Fatalf("expected split prediction counts, got %#v", result)
	}
	if result.Decisive != 1 || result.Right != 1 || result.Wrong != 0 {
		t.Fatalf("expected trusted-only decisive stats, got %#v", result)
	}
	if result.Action != "KEEP" {
		t.Fatalf("expected trusted evidence to drive action, got %#v", result)
	}
}

func TestEvaluateCandidateRegimeModes(t *testing.T) {
	candidate := models.PaperCandidate{
		AllowedRegimes: []string{"trend_up"},
		BlockedRegimes: []string{"range"},
	}
	if decision := evaluateCandidateRegime(candidate, "range", regimeModeStrict); decision.Allowed || decision.Gate != "BLOCK" {
		t.Fatalf("strict should block explicit blocked regime, got %#v", decision)
	}
	if decision := evaluateCandidateRegime(candidate, "trend_down", regimeModeAllowUnknown); !decision.Allowed || decision.Gate != "ALLOW_UNKNOWN" || decision.Exploratory {
		t.Fatalf("allow-unknown should allow unlisted non-blocked regime, got %#v", decision)
	}
	if decision := evaluateCandidateRegime(candidate, "range", regimeModeExplore); !decision.Allowed || decision.Gate != "EXPLORE" || !decision.Exploratory {
		t.Fatalf("explore should tag blocked regime as exploratory, got %#v", decision)
	}
}

func reviewPrediction(id string, created time.Time, outcome string, pnl float64, regime string) models.PaperPrediction {
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
		Gates: []models.PaperPredictionGate{
			{Name: "regime_allowed", Passed: true, Reason: regime},
		},
		Evaluated:  true,
		ExitPrice:  100 + pnl,
		Outcome:    outcome,
		PnLPercent: pnl,
	}
}

func exploratoryReviewPrediction(id string, created time.Time, outcome string, pnl float64, regime string) models.PaperPrediction {
	prediction := reviewPrediction(id, created, outcome, pnl, regime)
	prediction.Gates = append(prediction.Gates,
		models.PaperPredictionGate{Name: "regime_mode", Passed: false, Reason: regimeModeExplore},
		models.PaperPredictionGate{Name: "regime_gate", Passed: true, Reason: "EXPLORE:explore_unlisted_regime"},
		models.PaperPredictionGate{Name: "exploratory_regime", Passed: false, Reason: "explore_unlisted_regime"},
	)
	return prediction
}
