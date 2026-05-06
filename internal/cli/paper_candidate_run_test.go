package cli

import "testing"

func TestCandidateRunLookbackDaysDailyPadsForTradingCalendar(t *testing.T) {
	got := candidateRunLookbackDays(120, 80, "1day")
	if got < 170 {
		t.Fatalf("expected daily lookback to cover weekends and holidays, got %d", got)
	}
}

func TestCandidateRunLookbackDaysKeepsLargerUserLookback(t *testing.T) {
	got := candidateRunLookbackDays(260, 80, "1day")
	if got != 260 {
		t.Fatalf("expected explicit larger lookback to be preserved, got %d", got)
	}
}

func TestCandidateRunLookbackDaysIntradayUsesSessionCapacity(t *testing.T) {
	got := candidateRunLookbackDays(5, 80, "15minute")
	if got < 14 {
		t.Fatalf("expected intraday lookback to cover enough sessions, got %d", got)
	}
	if got > 30 {
		t.Fatalf("expected intraday lookback to stay bounded, got %d", got)
	}
}
