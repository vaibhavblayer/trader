package cli

import "testing"

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
