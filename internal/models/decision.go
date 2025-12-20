package models

import "time"

// Decision represents an AI trading decision.
type Decision struct {
	ID              string
	Timestamp       time.Time
	Symbol          string
	Action          string // BUY, SELL, HOLD
	Confidence      float64
	AgentResults    map[string]*AgentResult
	Consensus       *ConsensusDetails
	RiskCheck       *RiskCheckResult
	Executed        bool
	OrderID         string
	Outcome         DecisionOutcome
	PnL             float64
	Reasoning       string
	MarketCondition string // TRENDING_UP, TRENDING_DOWN, RANGING, HIGH_VOLATILITY
	EntryPrice      float64
	StopLoss        float64
	Targets         []float64
}

// AgentResult represents the result from a single agent.
type AgentResult struct {
	AgentName      string
	Recommendation string
	Confidence     float64
	Reasoning      string
	EntryPrice     float64
	StopLoss       float64
	Targets        []float64
	RiskReward     float64
	Timestamp      time.Time
}

// ConsensusDetails represents consensus calculation details.
type ConsensusDetails struct {
	TotalAgents    int
	AgreeingAgents int
	WeightedScore  float64
	Calculation    string
}

// RiskCheckResult represents the result of a risk check.
type RiskCheckResult struct {
	Approved        bool
	Violations      []string
	PositionSize    float64
	PortfolioImpact float64
	SectorExposure  float64
	DailyLossStatus float64
}

// DecisionOutcome represents the outcome of a decision.
type DecisionOutcome string

const (
	OutcomePending DecisionOutcome = "PENDING"
	OutcomeWin     DecisionOutcome = "WIN"
	OutcomeLoss    DecisionOutcome = "LOSS"
	OutcomeSkipped DecisionOutcome = "SKIPPED"
)

// AIStats represents AI performance statistics.
type AIStats struct {
	TotalDecisions    int
	ExecutedTrades    int
	WinRate           float64
	AvgPnL            float64
	AvgConfidence     float64
	ByAgent           map[string]*AgentStats
	ByMarketCondition map[string]*ConditionStats
}

// AgentStats represents statistics for a single agent.
type AgentStats struct {
	Name          string
	TotalCalls    int
	CorrectCalls  int
	Accuracy      float64
	AvgConfidence float64
}

// ConditionStats represents statistics by market condition.
type ConditionStats struct {
	Condition    string
	TotalTrades  int
	WinRate      float64
	AvgPnL       float64
}
