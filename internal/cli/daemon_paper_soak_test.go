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

func TestSoakTickerInterval(t *testing.T) {
	if got := soakTickerInterval(3 * time.Second); got != time.Second {
		t.Fatalf("expected second-level scheduler tick, got %s", got)
	}
	if got := soakTickerInterval(500 * time.Millisecond); got != 500*time.Millisecond {
		t.Fatalf("expected sub-second interval preserved, got %s", got)
	}
	if got := soakTickerInterval(0); got != time.Hour {
		t.Fatalf("expected default hour, got %s", got)
	}
}

func TestParseCandidateRegimeRotation(t *testing.T) {
	rotation, err := parseCandidateRegimeRotation("strict,allow-unknown,explore")
	if err != nil {
		t.Fatalf("parse rotation: %v", err)
	}
	if len(rotation) != 3 || rotation[1] != regimeModeAllowUnknown {
		t.Fatalf("unexpected rotation: %#v", rotation)
	}
	weighted, err := parseCandidateRegimeRotation("strict, strict, explore")
	if err != nil {
		t.Fatalf("parse weighted rotation: %v", err)
	}
	if len(weighted) != 3 || weighted[0] != regimeModeStrict || weighted[1] != regimeModeStrict || weighted[2] != regimeModeExplore {
		t.Fatalf("unexpected weighted rotation: %#v", weighted)
	}
	all, err := parseCandidateRegimeRotation("all")
	if err != nil {
		t.Fatalf("parse all rotation: %v", err)
	}
	if len(all) != 3 || all[2] != regimeModeExplore {
		t.Fatalf("unexpected all rotation: %#v", all)
	}
}

func TestScheduledPaperSoakRegimeRotationAdvances(t *testing.T) {
	rotation := []string{regimeModeStrict, regimeModeAllowUnknown, regimeModeExplore}
	state := &models.DaemonState{PaperSoakRegimeIndex: 1}
	opts := daemonPaperSoakOptions{RegimeMode: regimeModeStrict, RegimeRotation: rotation}
	if got := scheduledPaperSoakRegimeMode(state, opts); got != regimeModeAllowUnknown {
		t.Fatalf("expected allow-unknown, got %s", got)
	}
	advancePaperSoakRegimeRotation(state, rotation)
	if state.PaperSoakRegimeIndex != 2 {
		t.Fatalf("expected index 2, got %d", state.PaperSoakRegimeIndex)
	}
	advancePaperSoakRegimeRotation(state, rotation)
	if state.PaperSoakRegimeIndex != 0 {
		t.Fatalf("expected wrap to index 0, got %d", state.PaperSoakRegimeIndex)
	}
}

func TestInitialPaperSoakRegimeIndexPreservesSameRotation(t *testing.T) {
	rotation := []string{regimeModeStrict, regimeModeExplore}
	persisted := &models.DaemonState{PaperSoakRegimeRotation: rotation, PaperSoakRegimeIndex: 5}
	if got := initialPaperSoakRegimeIndex(persisted, rotation); got != 1 {
		t.Fatalf("expected normalized preserved index 1, got %d", got)
	}
	if got := initialPaperSoakRegimeIndex(persisted, []string{regimeModeStrict}); got != 0 {
		t.Fatalf("expected reset index 0 for changed rotation, got %d", got)
	}
}
