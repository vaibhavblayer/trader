package models

import "time"

// PaperExperimentRun records one operational paper-soak experiment cycle.
type PaperExperimentRun struct {
	ID                     string        `json:"id"`
	Source                 string        `json:"source"`
	Command                string        `json:"command,omitempty"`
	StartedAt              time.Time     `json:"started_at"`
	FinishedAt             time.Time     `json:"finished_at"`
	Symbol                 string        `json:"symbol,omitempty"`
	Strategy               string        `json:"strategy,omitempty"`
	Status                 string        `json:"status,omitempty"`
	RegimeMode             string        `json:"regime_mode"`
	DryRun                 bool          `json:"dry_run"`
	ApplyReview            bool          `json:"apply_review"`
	Limit                  int           `json:"limit"`
	CandidateDays          int           `json:"candidate_days"`
	MinCandles             int           `json:"min_candles"`
	RegimeWindow           int           `json:"regime_window"`
	TimeWindow             time.Duration `json:"time_window"`
	EvaluateDays           int           `json:"evaluate_days"`
	ReviewDays             int           `json:"review_days"`
	CandidatesLoaded       int           `json:"candidates_loaded"`
	CandidatesChecked      int           `json:"candidates_checked"`
	PredictionsCreated     int           `json:"predictions_created"`
	OpenPredictions        int           `json:"open_predictions"`
	Blocked                int           `json:"blocked"`
	NoSignal               int           `json:"no_signal"`
	Errors                 int           `json:"errors"`
	OutcomesEvaluated      int           `json:"outcomes_evaluated"`
	CandidatesPaused       int           `json:"candidates_paused"`
	CandidatesReady        int           `json:"candidates_ready"`
	TrustedPredictions     int           `json:"trusted_predictions"`
	ExploratoryPredictions int           `json:"exploratory_predictions"`
	TrustedDecisive        int           `json:"trusted_decisive"`
	ExploratoryDecisive    int           `json:"exploratory_decisive"`
	ReadinessDecision      string        `json:"readiness_decision,omitempty"`
	ReadinessReasons       []string      `json:"readiness_reasons,omitempty"`
	Notes                  string        `json:"notes,omitempty"`
	CreatedAt              time.Time     `json:"created_at"`
}

// PaperExperimentRunFilter filters paper experiment ledger runs.
type PaperExperimentRunFilter struct {
	Symbol     string
	Strategy   string
	RegimeMode string
	Source     string
	StartDate  time.Time
	EndDate    time.Time
	Limit      int
}
