package cli

import (
	"testing"
	"time"
)

func TestParsePaperSoakLoopClock(t *testing.T) {
	got, err := parsePaperSoakLoopClock("09:15")
	if err != nil {
		t.Fatalf("parse clock: %v", err)
	}
	if got != 9*60+15 {
		t.Fatalf("expected 555 minutes, got %d", got)
	}
	if _, err := parsePaperSoakLoopClock("24:00"); err == nil {
		t.Fatal("expected invalid clock parse error")
	}
}

func TestWithinPaperSoakLoopWindow(t *testing.T) {
	inMarket := time.Date(2026, 5, 6, 10, 0, 0, 0, time.Local)
	ok, err := withinPaperSoakLoopWindow(inMarket, "09:15", "15:30")
	if err != nil {
		t.Fatalf("window check: %v", err)
	}
	if !ok {
		t.Fatal("expected 10:00 inside market window")
	}

	afterMarket := time.Date(2026, 5, 6, 16, 0, 0, 0, time.Local)
	ok, err = withinPaperSoakLoopWindow(afterMarket, "09:15", "15:30")
	if err != nil {
		t.Fatalf("window check: %v", err)
	}
	if ok {
		t.Fatal("expected 16:00 outside market window")
	}

	overnight := time.Date(2026, 5, 6, 23, 0, 0, 0, time.Local)
	ok, err = withinPaperSoakLoopWindow(overnight, "22:00", "02:00")
	if err != nil {
		t.Fatalf("overnight window check: %v", err)
	}
	if !ok {
		t.Fatal("expected 23:00 inside overnight window")
	}
}

func TestPaperSoakLoopCommandSummary(t *testing.T) {
	got := paperSoakLoopCommandSummary(paperSoakRunOptions{
		Symbol:     "INFY",
		Strategy:   "intraday_momentum",
		RegimeMode: regimeModeExplore,
		DryRun:     true,
	}, 15*time.Minute, 3, true, "09:15", "15:30", true)
	want := "paper soak-loop --symbol INFY --strategy intraday_momentum --regime-mode explore --interval 15m0s --max-runs 3 --start 09:15 --end 15:30 --once-now --dry-run"
	if got != want {
		t.Fatalf("unexpected command summary:\nwant %q\n got %q", want, got)
	}
}
