package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestSQLiteDaemonStateRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "daemon_state.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	now := time.Now().Truncate(time.Second)
	killAt := now.Add(time.Minute)
	state := &models.DaemonState{
		Status:                models.DaemonStatusPaused,
		PID:                   1234,
		Hostname:              "test-host",
		StartedAt:             now,
		UpdatedAt:             now,
		LastHeartbeatAt:       now,
		Watchlist:             "momentum",
		Symbols:               []string{"RELIANCE", "INFY"},
		DryRun:                true,
		IntervalSeconds:       30,
		PaperSoakEnabled:      true,
		PaperSoakOnly:         true,
		PaperSoakInterval:     time.Hour,
		PaperSoakSymbol:       "HDFCBANK",
		PaperSoakDryRun:       true,
		LastPaperSoakRunAt:    now.Add(-time.Hour),
		NextPaperSoakRunAt:    now.Add(time.Hour),
		LastPaperSoakSummary:  "candidates=1",
		Mode:                  "FULL_AUTO",
		SafetyProfile:         "paper",
		Paused:                true,
		KillSwitchActive:      true,
		KillSwitchReason:      "test halt",
		KillSwitchActivatedAt: &killAt,
		KillSwitchActivatedBy: "tester",
		Message:               "paused",
	}

	if err := store.SaveDaemonState(ctx, state); err != nil {
		t.Fatalf("save daemon state: %v", err)
	}
	got, err := store.LoadDaemonState(ctx)
	if err != nil {
		t.Fatalf("load daemon state: %v", err)
	}
	if got == nil {
		t.Fatal("expected daemon state")
	}
	if got.Status != models.DaemonStatusPaused || !got.KillSwitchActive || got.KillSwitchReason != "test halt" {
		t.Fatalf("state not restored: %#v", got)
	}
	if len(got.Symbols) != 2 || got.Symbols[0] != "RELIANCE" || got.Symbols[1] != "INFY" {
		t.Fatalf("symbols not restored: %#v", got.Symbols)
	}
	if !got.PaperSoakEnabled || !got.PaperSoakOnly || got.PaperSoakInterval != time.Hour || got.PaperSoakSymbol != "HDFCBANK" {
		t.Fatalf("paper soak state not restored: %#v", got)
	}
}

func TestSQLiteDaemonEvents(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "daemon_events.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	if err := store.AppendDaemonEvent(ctx, &models.DaemonEvent{
		Type:   "KILL_SWITCH_ENGAGED",
		Status: models.DaemonStatusPaused,
		Reason: "test",
		Actor:  "tester",
	}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	events, err := store.GetDaemonEvents(ctx, 10)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	if len(events) != 1 || events[0].Type != "KILL_SWITCH_ENGAGED" || events[0].Status != models.DaemonStatusPaused {
		t.Fatalf("unexpected events: %#v", events)
	}
}
