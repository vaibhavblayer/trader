package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"zerodha-trader/internal/models"
)

const defaultDaemonStateID = "default"

func daemonActor() string {
	if user := os.Getenv("USER"); user != "" {
		return user
	}
	return "cli"
}

func daemonHostname() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "unknown"
	}
	return host
}

func newDefaultDaemonState(app *App) *models.DaemonState {
	state := &models.DaemonState{
		ID:              defaultDaemonStateID,
		Status:          models.DaemonStatusStopped,
		UpdatedAt:       time.Now(),
		SafetyProfile:   "",
		Mode:            "",
		IntervalSeconds: 0,
	}
	if app != nil && app.Config != nil {
		state.Mode = app.Config.Agents.AutonomousMode
		state.SafetyProfile = app.Config.SafetyProfile()
	}
	return state
}

func loadDaemonStateOrDefault(ctx context.Context, app *App) (*models.DaemonState, error) {
	if app == nil || app.Store == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	state, err := app.Store.LoadDaemonState(ctx)
	if err != nil {
		return nil, err
	}
	if state == nil {
		state = newDefaultDaemonState(app)
	}
	if state.ID == "" {
		state.ID = defaultDaemonStateID
	}
	return state, nil
}

func saveDaemonStateWithEvent(ctx context.Context, app *App, state *models.DaemonState, eventType, reason string) error {
	if app == nil || app.Store == nil {
		return fmt.Errorf("database not initialized")
	}
	now := time.Now()
	state.ID = defaultDaemonStateID
	state.UpdatedAt = now
	if err := app.Store.SaveDaemonState(ctx, state); err != nil {
		return err
	}
	return app.Store.AppendDaemonEvent(ctx, &models.DaemonEvent{
		Timestamp: now,
		Type:      eventType,
		Status:    state.Status,
		Reason:    reason,
		Actor:     daemonActor(),
		PID:       os.Getpid(),
		Hostname:  daemonHostname(),
	})
}

func daemonHeartbeatFresh(state *models.DaemonState) bool {
	if state == nil || state.LastHeartbeatAt.IsZero() {
		return false
	}
	interval := time.Duration(state.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Minute
	}
	staleAfter := interval*3 + 15*time.Second
	if staleAfter < 2*time.Minute {
		staleAfter = 2 * time.Minute
	}
	return time.Since(state.LastHeartbeatAt) <= staleAfter
}

func daemonBlockReason(state *models.DaemonState) string {
	if state == nil {
		return ""
	}
	if state.KillSwitchActive {
		if state.KillSwitchReason != "" {
			return "daemon kill switch active: " + state.KillSwitchReason
		}
		return "daemon kill switch active"
	}
	if state.Paused || state.Status == models.DaemonStatusPaused {
		return "daemon is paused"
	}
	if state.StopRequested || state.Status == models.DaemonStatusStopRequested {
		return "daemon stop requested"
	}
	return ""
}

func (app *App) checkDaemonExecutionGate(ctx context.Context) error {
	if app == nil || app.Store == nil {
		return nil
	}
	state, err := app.Store.LoadDaemonState(ctx)
	if err != nil {
		return fmt.Errorf("checking daemon control state: %w", err)
	}
	if reason := daemonBlockReason(state); reason != "" {
		return fmt.Errorf("autonomous execution blocked: %s", reason)
	}
	return nil
}

func refreshDaemonRuntimeState(ctx context.Context, app *App, current *models.DaemonState) (*models.DaemonState, bool, bool, string, error) {
	if app == nil || app.Store == nil {
		return current, false, false, "", nil
	}
	state, err := app.Store.LoadDaemonState(ctx)
	if err != nil {
		return current, false, false, "", err
	}
	if state == nil {
		state = current
	}
	if state == nil {
		state = newDefaultDaemonState(app)
	}

	now := time.Now()
	if current != nil {
		state.PID = current.PID
		state.Hostname = current.Hostname
		state.StartedAt = current.StartedAt
		state.Watchlist = current.Watchlist
		state.Symbols = current.Symbols
		state.DryRun = current.DryRun
		state.IntervalSeconds = current.IntervalSeconds
		state.PaperSoakEnabled = current.PaperSoakEnabled
		state.PaperSoakOnly = current.PaperSoakOnly
		state.PaperSoakInterval = current.PaperSoakInterval
		state.PaperSoakSymbol = current.PaperSoakSymbol
		state.PaperSoakStrategy = current.PaperSoakStrategy
		state.PaperSoakDryRun = current.PaperSoakDryRun
		state.LastPaperSoakRunAt = current.LastPaperSoakRunAt
		state.NextPaperSoakRunAt = current.NextPaperSoakRunAt
		state.LastPaperSoakSummary = current.LastPaperSoakSummary
		state.Mode = current.Mode
		state.SafetyProfile = current.SafetyProfile
	}
	state.ID = defaultDaemonStateID
	state.LastHeartbeatAt = now
	state.UpdatedAt = now

	if state.StopRequested || state.Status == models.DaemonStatusStopRequested {
		state.Status = models.DaemonStatusStopRequested
		state.StopRequested = true
		state.Message = "stop requested"
		if err := app.Store.SaveDaemonState(ctx, state); err != nil {
			return state, true, false, "", err
		}
		return state, true, false, "stop requested", nil
	}
	if state.KillSwitchActive {
		state.Status = models.DaemonStatusPaused
		state.Paused = true
		state.Message = daemonBlockReason(state)
		if err := app.Store.SaveDaemonState(ctx, state); err != nil {
			return state, false, true, state.Message, err
		}
		return state, false, true, state.Message, nil
	}
	if state.Paused || state.Status == models.DaemonStatusPaused {
		state.Status = models.DaemonStatusPaused
		state.Paused = true
		state.Message = "daemon is paused"
		if err := app.Store.SaveDaemonState(ctx, state); err != nil {
			return state, false, true, state.Message, err
		}
		return state, false, true, state.Message, nil
	}

	state.Status = models.DaemonStatusRunning
	state.Paused = false
	state.Message = "running"
	if err := app.Store.SaveDaemonState(ctx, state); err != nil {
		return state, false, false, "", err
	}
	return state, false, false, "", nil
}

func markDaemonStopped(ctx context.Context, app *App, reason string) {
	if app == nil || app.Store == nil {
		return
	}
	state, err := app.Store.LoadDaemonState(ctx)
	if err != nil {
		return
	}
	if state == nil {
		state = newDefaultDaemonState(app)
	}
	state.Status = models.DaemonStatusStopped
	state.StopRequested = false
	state.Paused = false
	state.PID = 0
	state.Message = reason
	state.UpdatedAt = time.Now()
	_ = saveDaemonStateWithEvent(ctx, app, state, "STOPPED", reason)
}
