package cli

import (
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestPaperSoakRunCounters(t *testing.T) {
	candidateRuns := []candidateRunResult{
		{Status: "PREDICTED"},
		{Status: "BLOCK"},
		{Status: "block"},
		{Status: "NO_SIGNAL"},
		{Status: "ERROR"},
	}
	if got := countCandidateRunStatus(candidateRuns, "BLOCK"); got != 2 {
		t.Fatalf("expected 2 blocked candidates, got %d", got)
	}
	if got := countCandidateRunStatus(candidateRuns, "PREDICTED"); got != 1 {
		t.Fatalf("expected 1 prediction, got %d", got)
	}

	evaluations := []paperEvaluationResult{
		{Status: "EVALUATED"},
		{Status: "OPEN"},
		{Status: "evaluated"},
	}
	if got := countPaperEvaluationStatus(evaluations, "EVALUATED"); got != 2 {
		t.Fatalf("expected 2 evaluated outcomes, got %d", got)
	}

	reviews := []paperCandidateReviewResult{
		{Action: "KEEP"},
		{Action: "PAUSE"},
		{Action: "READY"},
		{Action: "ready"},
	}
	if got := countCandidateReviewAction(reviews, "READY"); got != 2 {
		t.Fatalf("expected 2 ready candidates, got %d", got)
	}
}

func TestPaperExperimentRunFromSoakReport(t *testing.T) {
	started := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	report := paperSoakRunReport{
		StartedAt:         started,
		FinishedAt:        started.Add(time.Second),
		Source:            "cli",
		Command:           "paper soak-run --regime-mode explore",
		Symbol:            "HDFCBANK",
		Strategy:          "multi_indicator",
		Status:            models.PaperCandidateStatusActive,
		RegimeMode:        regimeModeExplore,
		DryRun:            true,
		CandidatesLoaded:  1,
		CandidatesChecked: 1,
		NoSignal:          1,
		ReadinessDecision: "WARN",
		CandidateReview:   []paperCandidateReviewResult{{TrustedPredictions: 2, ExploratoryPredictions: 1, Decisive: 1, ExploratoryDecisive: 1}},
		Readiness:         &models.AutonomyReadinessReport{Reasons: []string{"sample too small"}},
	}
	run := paperExperimentRunFromSoakReport(report, paperSoakRunOptions{
		Limit:         5,
		CandidateDays: 120,
		MinCandles:    80,
		RegimeWindow:  50,
		TimeWindow:    24 * time.Hour,
		EvaluateDays:  30,
		ReviewDays:    90,
	})
	if run.ID == "" || run.Source != "cli" || run.RegimeMode != regimeModeExplore {
		t.Fatalf("unexpected run identity: %#v", run)
	}
	if run.TrustedPredictions != 2 || run.ExploratoryPredictions != 1 || run.TrustedDecisive != 1 || run.ExploratoryDecisive != 1 {
		t.Fatalf("unexpected evidence counts: %#v", run)
	}
	if len(run.ReadinessReasons) != 1 || run.TimeWindow != 24*time.Hour {
		t.Fatalf("unexpected run details: %#v", run)
	}
}

func TestSummarizePaperExperimentRuns(t *testing.T) {
	runs := []models.PaperExperimentRun{
		{Source: "cli", RegimeMode: regimeModeStrict, CandidatesChecked: 2, PredictionsCreated: 1, Blocked: 1},
		{Source: "cli", RegimeMode: regimeModeStrict, CandidatesChecked: 2, NoSignal: 2, DryRun: true},
		{Source: "daemon", RegimeMode: regimeModeExplore, CandidatesChecked: 1, PredictionsCreated: 1, ExploratoryPredictions: 1},
	}
	summaries := summarizePaperExperimentRuns(runs)
	if len(summaries) != 2 {
		t.Fatalf("expected two groups, got %#v", summaries)
	}
	if summaries[0].Runs != 2 || summaries[0].DryRuns != 1 || summaries[0].CandidatesChecked != 4 {
		t.Fatalf("unexpected strict summary: %#v", summaries[0])
	}
	if summaries[0].PredictionRate != 25 || summaries[0].BlockRate != 25 || summaries[0].NoSignalRate != 50 {
		t.Fatalf("unexpected rates: %#v", summaries[0])
	}
}

func TestComparePaperExperimentCohortsUsesCandidateRegimeOutcomes(t *testing.T) {
	now := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	runs := []models.PaperExperimentRun{
		{Source: "cli", RegimeMode: regimeModeStrict, CandidatesChecked: 4, PredictionsCreated: 2},
		{Source: "cli", RegimeMode: regimeModeExplore, CandidatesChecked: 4, PredictionsCreated: 3, ExploratoryPredictions: 3},
	}
	predictions := []models.PaperPrediction{
		cohortPrediction("STRICT_RIGHT", now.Add(-4*time.Hour), regimeModeStrict, "RIGHT", 2.0),
		cohortPrediction("STRICT_WRONG", now.Add(-3*time.Hour), regimeModeStrict, "WRONG", -1.0),
		cohortPrediction("EXPLORE_RIGHT", now.Add(-2*time.Hour), regimeModeExplore, "RIGHT", 3.0),
		cohortPrediction("EXPLORE_RIGHT_2", now.Add(-time.Hour), regimeModeExplore, "RIGHT", 2.0),
		{
			ID:          "LLM_IGNORED",
			Symbol:      "HDFCBANK",
			SetupName:   "llm_simple",
			CreatedAt:   now,
			Evaluated:   true,
			Outcome:     "WRONG",
			PnLPercent:  -10,
			TimeWindow:  time.Hour,
			EntryPrice:  100,
			TargetPrice: 105,
			StopLoss:    98,
		},
	}

	comparison := comparePaperExperimentCohorts(runs, predictions, paperExperimentComparisonOptions{
		MinOutcomeDecisive: 2,
		MinWinRate:         50,
		MinExpectancy:      0,
	})
	if len(comparison) != 2 {
		t.Fatalf("expected two cohorts, got %#v", comparison)
	}
	if comparison[0].PaperDecisive != 2 || comparison[0].PaperExpectancy != 0.5 {
		t.Fatalf("unexpected strict outcome stats: %#v", comparison[0])
	}
	if comparison[1].RegimeMode != regimeModeExplore || comparison[1].Verdict != "LEADING" {
		t.Fatalf("expected explore cohort to lead, got %#v", comparison)
	}
}

func cohortPrediction(id string, created time.Time, regimeMode string, outcome string, pnl float64) models.PaperPrediction {
	return models.PaperPrediction{
		ID:          id,
		Symbol:      "HDFCBANK",
		Action:      "BUY",
		EntryPrice:  100,
		TargetPrice: 105,
		StopLoss:    98,
		TimeWindow:  time.Hour,
		CreatedAt:   created,
		ExpiresAt:   created.Add(time.Hour),
		SetupName:   "candidate:HDFCBANK_multi_fast",
		Timeframe:   "1day",
		Gates: []models.PaperPredictionGate{
			{Name: "regime_mode", Passed: regimeMode == regimeModeStrict, Reason: regimeMode},
		},
		Evaluated:  true,
		ExitPrice:  100 + pnl,
		Outcome:    outcome,
		PnLPercent: pnl,
	}
}
