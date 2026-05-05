package models

import "time"

// DaemonStatus describes the persisted autonomous daemon state.
type DaemonStatus string

const (
	DaemonStatusStopped       DaemonStatus = "STOPPED"
	DaemonStatusRunning       DaemonStatus = "RUNNING"
	DaemonStatusPaused        DaemonStatus = "PAUSED"
	DaemonStatusStopRequested DaemonStatus = "STOP_REQUESTED"
)

// DaemonState is the durable control plane for the autonomous trader daemon.
type DaemonState struct {
	ID                      string
	Status                  DaemonStatus
	PID                     int
	Hostname                string
	StartedAt               time.Time
	UpdatedAt               time.Time
	LastHeartbeatAt         time.Time
	Watchlist               string
	Symbols                 []string
	DryRun                  bool
	IntervalSeconds         int
	PaperSoakEnabled        bool
	PaperSoakOnly           bool
	PaperSoakInterval       time.Duration
	PaperSoakSymbol         string
	PaperSoakStrategy       string
	PaperSoakRegimeMode     string
	PaperSoakRegimeRotation []string
	PaperSoakRegimeIndex    int
	LastPaperSoakRegimeMode string
	PaperSoakDryRun         bool
	LastPaperSoakRunAt      time.Time
	NextPaperSoakRunAt      time.Time
	LastPaperSoakSummary    string
	Mode                    string
	SafetyProfile           string
	StopRequested           bool
	Paused                  bool
	KillSwitchActive        bool
	KillSwitchReason        string
	KillSwitchActivatedAt   *time.Time
	KillSwitchActivatedBy   string
	Message                 string
}

// DaemonEvent records an auditable daemon control state change.
type DaemonEvent struct {
	ID        string
	Timestamp time.Time
	Type      string
	Status    DaemonStatus
	Reason    string
	Actor     string
	PID       int
	Hostname  string
}
