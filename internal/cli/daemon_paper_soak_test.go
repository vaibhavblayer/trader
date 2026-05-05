package cli

import (
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestDuePaperSoakRun(t *testing.T) {
	now := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	if duePaperSoakRun(nil, now) {
		t.Fatal("nil daemon state should not be due")
	}
	if duePaperSoakRun(&models.DaemonState{}, now) {
		t.Fatal("disabled paper soak should not be due")
	}
	if !duePaperSoakRun(&models.DaemonState{PaperSoakEnabled: true}, now) {
		t.Fatal("enabled paper soak with no next time should be due")
	}
	if duePaperSoakRun(&models.DaemonState{
		PaperSoakEnabled:   true,
		NextPaperSoakRunAt: now.Add(time.Minute),
	}, now) {
		t.Fatal("future paper soak should not be due")
	}
	if !duePaperSoakRun(&models.DaemonState{
		PaperSoakEnabled:   true,
		NextPaperSoakRunAt: now.Add(-time.Second),
	}, now) {
		t.Fatal("past paper soak should be due")
	}
}
