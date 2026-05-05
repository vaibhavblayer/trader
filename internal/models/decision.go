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

	// Enhanced fields for better prediction tracking
	MarketRegime     string   // regime at time of decision
	VIXAtDecision    float64  // VIX level when decision was made
	ATRPercent       float64  // ATR as % of price (volatility context)
	SignalScore      float64  // composite signal score at decision time
	MTFConfluence    string   // multi-timeframe confluence level
	PatternContext   []string // patterns active at decision time
	PositionSizeQty  int      // actual quantity recommended
	RiskRewardRatio  float64  // R:R at entry
	CalibratedConf   float64  // confidence after historical calibration
	ConflictingCount int      // number of agents that disagreed
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
	HistAccuracy   float64 // this agent's historical accuracy for this signal type
}

// ConsensusDetails represents consensus calculation details.
type ConsensusDetails struct {
	TotalAgents    int
	AgreeingAgents int
	WeightedScore  float64
	Calculation    string

	// Enhanced consensus fields
	BuyScore   float64 // raw weighted buy score
	SellScore  float64 // raw weighted sell score
	HoldScore  float64 // raw weighted hold score
	Conviction float64 // margin between top two scores (0-100)
	Unanimous  bool    // all agents agree
}

// RiskCheckResult represents the result of a risk check.
type RiskCheckResult struct {
	Approved        bool
	Violations      []string
	PositionSize    float64
	PortfolioImpact float64
	SectorExposure  float64
	DailyLossStatus float64

	// Enhanced risk fields
	KellyFraction   float64 // Kelly criterion suggested fraction
	VolAdjustedSize float64 // volatility-adjusted position size
	MaxLossAmount   float64 // max loss if stop hit
	CorrelationRisk float64 // correlation with existing positions (0-1)
}

// DecisionOutcome represents the outcome of a decision.
type DecisionOutcome string

const (
	OutcomePending DecisionOutcome = "PENDING"
	OutcomeWin     DecisionOutcome = "WIN"
	OutcomeLoss    DecisionOutcome = "LOSS"
	OutcomeSkipped DecisionOutcome = "SKIPPED"
)

// DecisionLogStage identifies the lifecycle stage captured in a decision log.
type DecisionLogStage string

const (
	DecisionStageGenerated         DecisionLogStage = "GENERATED"
	DecisionStageRiskChecked       DecisionLogStage = "RISK_CHECKED"
	DecisionStageExecutionSelected DecisionLogStage = "EXECUTION_SELECTED"
	DecisionStageExecutionBlocked  DecisionLogStage = "EXECUTION_BLOCKED"
	DecisionStageOrderSubmitted    DecisionLogStage = "ORDER_SUBMITTED"
	DecisionStageOrderAccepted     DecisionLogStage = "ORDER_ACCEPTED"
	DecisionStageOrderRejected     DecisionLogStage = "ORDER_REJECTED"
	DecisionStageProtectiveOrder   DecisionLogStage = "PROTECTIVE_ORDER"
)

// DecisionLog is an append-only event for a trading decision lifecycle.
type DecisionLog struct {
	ID         string
	DecisionID string
	Timestamp  time.Time
	Stage      DecisionLogStage
	Symbol     string
	Action     string
	Status     string
	Message    string
	Payload    map[string]interface{}
}

// AIStats represents AI performance statistics.
type AIStats struct {
	TotalDecisions    int
	ExecutedTrades    int
	WinRate           float64
	AvgPnL            float64
	AvgConfidence     float64
	ByAgent           map[string]*AgentStats
	ByMarketCondition map[string]*ConditionStats

	// Enhanced stats
	CalibrationError float64 // avg |confidence - actual_win_rate|
	ProfitFactor     float64
	SharpeRatio      float64
	AvgRiskReward    float64
	BestRegime       string // which market regime performs best
	WorstRegime      string
}

// AgentStats represents statistics for a single agent.
type AgentStats struct {
	Name          string
	TotalCalls    int
	CorrectCalls  int
	Accuracy      float64
	AvgConfidence float64

	// Enhanced agent stats
	CalibrationError float64            // how well confidence predicts outcomes
	ProfitContrib    float64            // P&L attributed to this agent's calls
	BestSignal       string             // BUY or SELL - which direction is more accurate
	ByRegime         map[string]float64 // accuracy per market regime
}

// ConditionStats represents statistics by market condition.
type ConditionStats struct {
	Condition   string
	TotalTrades int
	WinRate     float64
	AvgPnL      float64
}
