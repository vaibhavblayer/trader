package models

import "time"

// AutonomyReadinessStatus is the result of one readiness check.
type AutonomyReadinessStatus string

const (
	AutonomyReadinessPass AutonomyReadinessStatus = "PASS"
	AutonomyReadinessWarn AutonomyReadinessStatus = "WARN"
	AutonomyReadinessFail AutonomyReadinessStatus = "FAIL"
)

// AutonomyReadinessDecision is the aggregate autonomy decision.
type AutonomyReadinessDecision string

const (
	AutonomyDecisionReady   AutonomyReadinessDecision = "READY"
	AutonomyDecisionWarn    AutonomyReadinessDecision = "WARN"
	AutonomyDecisionBlocked AutonomyReadinessDecision = "BLOCKED"
)

// AutonomyReadinessReport summarizes whether autonomous operation is currently allowed.
type AutonomyReadinessReport struct {
	GeneratedAt time.Time
	Phase       string
	StartDate   time.Time
	EndDate     time.Time
	Symbol      string
	Decision    AutonomyReadinessDecision
	Reasons     []string
	Checks      []AutonomyReadinessCheck
	Summary     AutonomyReadinessSummary
}

// AutonomyReadinessCheck is one pass/warn/fail readiness gate.
type AutonomyReadinessCheck struct {
	Name    string
	Status  AutonomyReadinessStatus
	Message string
	Details map[string]interface{}
}

// AutonomyReadinessSummary carries the key metrics used by the readiness decision.
type AutonomyReadinessSummary struct {
	TradingMode            string
	SafetyProfile          string
	AutonomousMode         string
	KillSwitchActive       bool
	DaemonStatus           string
	PaperPredictions       int
	PaperDecisive          int
	PaperWinRate           float64
	PaperExpectancy        float64
	CalibrationDecisive    int
	CalibrationExpectancy  float64
	ExecutionOrders        int
	ExecutionFillRate      float64
	ExecutionRejectionRate float64
	ExecutionAvgSlippageBp float64
	ReviewedTrades         int
	PostTradeAvgPnLPercent float64
	MissingPredictionRate  float64
	MissingExecutionRate   float64
}

// PaperSoakReport summarizes autonomous paper-trading soak progress.
type PaperSoakReport struct {
	GeneratedAt        time.Time
	StartDate          time.Time
	EndDate            time.Time
	Symbol             string
	Readiness          *AutonomyReadinessReport
	Sessions           int
	Decisions          int
	Predictions        int
	Decisive           int
	WinRate            float64
	Expectancy         float64
	AvgPnLPercent      float64
	ExecutionOrders    int
	ReviewedTrades     int
	ReadinessFailures  int
	KillSwitchEvents   int
	RecommendedCommand string
}
