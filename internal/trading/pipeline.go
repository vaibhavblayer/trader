// Package trading provides trading operations and decision pipeline.
package trading

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"zerodha-trader/internal/agents"
	"zerodha-trader/internal/analysis"
	"zerodha-trader/internal/config"
	"zerodha-trader/internal/models"
	"zerodha-trader/internal/store"
)

// DecisionPipeline aggregates data and coordinates AI-driven trade decisions.
// Requirements: 13.1-13.10
type DecisionPipeline struct {
	orchestrator *agents.Orchestrator
	riskAgent    *agents.RiskAgent
	config       *config.AgentConfig
	riskConfig   *config.RiskConfig
	store        store.DataStore
	notifier     Notifier

	// State tracking
	mu                sync.RWMutex
	dailyTrades       int
	dailyLoss         float64
	lastTradeAt       time.Time
	consecutiveLosses int
	decisionHistory   []DecisionRecord
}

// Notifier defines the interface for sending notifications.
type Notifier interface {
	SendTrade(ctx context.Context, symbol string, decision *models.Decision) error
	SendAlert(ctx context.Context, message string) error
	SendError(ctx context.Context, err error, context string) error
}

// DecisionRecord tracks decision outcomes for accuracy measurement.
type DecisionRecord struct {
	DecisionID string
	Symbol     string
	Action     string
	Confidence float64
	Executed   bool
	Outcome    models.DecisionOutcome
	PnL        float64
	Timestamp  time.Time
}

// NewDecisionPipeline creates a new decision pipeline.
func NewDecisionPipeline(
	orchestrator *agents.Orchestrator,
	riskAgent *agents.RiskAgent,
	agentConfig *config.AgentConfig,
	riskConfig *config.RiskConfig,
	dataStore store.DataStore,
	notifier Notifier,
) *DecisionPipeline {
	return &DecisionPipeline{
		orchestrator:    orchestrator,
		riskAgent:       riskAgent,
		config:          agentConfig,
		riskConfig:      riskConfig,
		store:           dataStore,
		notifier:        notifier,
		decisionHistory: make([]DecisionRecord, 0),
	}
}

// PipelineInput contains all data needed for the decision pipeline.
type PipelineInput struct {
	Symbol       string
	CurrentPrice float64

	// Technical data
	Candles     map[string][]models.Candle
	Indicators  map[string][]float64
	Patterns    []analysis.Pattern
	SignalScore *analysis.SignalScore
	Levels      *agents.LevelData

	// Fundamental data
	Research *agents.ResearchReport
	News     []agents.NewsItem

	// Context
	Portfolio   *agents.PortfolioState
	MarketState *agents.MarketState
}

// PipelineOutput contains the decision pipeline result.
type PipelineOutput struct {
	Decision       *models.Decision
	ShouldExecute  bool
	ExecutionBlock string // Reason if execution is blocked
	RiskAssessment *RiskAssessment
}

// RiskAssessment contains detailed risk evaluation.
type RiskAssessment struct {
	Approved           bool
	Violations         []string
	SuggestedSize      float64
	MaxAllowedSize     float64
	PortfolioImpact    float64
	SectorExposure     float64
	DailyLossRemaining float64
	RiskRewardRatio    float64
}

// Process runs the full decision pipeline for a symbol.
// Requirements: 13.1-13.10
func (p *DecisionPipeline) Process(ctx context.Context, input PipelineInput) (*PipelineOutput, error) {
	output := &PipelineOutput{}

	// Step 1: Aggregate data into analysis request
	req := p.buildAnalysisRequest(input)

	// Step 2: Run agents through orchestrator
	decision, err := p.orchestrator.ProcessSymbol(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("processing symbol through agents: %w", err)
	}
	output.Decision = decision

	// Step 3: Validate AI response against risk parameters
	riskAssessment := p.validateAgainstRisk(ctx, decision, input)
	output.RiskAssessment = riskAssessment

	// Update decision with risk check
	if decision.RiskCheck == nil {
		decision.RiskCheck = &models.RiskCheckResult{
			Approved:        riskAssessment.Approved,
			Violations:      riskAssessment.Violations,
			PositionSize:    riskAssessment.SuggestedSize,
			PortfolioImpact: riskAssessment.PortfolioImpact,
			SectorExposure:  riskAssessment.SectorExposure,
			DailyLossStatus: riskAssessment.DailyLossRemaining,
		}
	}

	// Step 4: Determine if we should execute
	shouldExecute, blockReason := p.shouldExecute(decision)
	output.ShouldExecute = shouldExecute
	output.ExecutionBlock = blockReason
	decision.Executed = shouldExecute

	// Step 5: Save decision to store
	if p.store != nil {
		if err := p.store.SaveDecision(ctx, decision); err != nil {
			if p.notifier != nil {
				p.notifier.SendError(ctx, err, "saving decision")
			}
		}
	}

	// Step 6: Track for accuracy measurement
	p.trackDecision(decision)

	// Step 7: Send notifications based on mode
	p.sendNotifications(ctx, decision, shouldExecute, blockReason)

	return output, nil
}

// buildAnalysisRequest converts pipeline input to agent analysis request.
func (p *DecisionPipeline) buildAnalysisRequest(input PipelineInput) agents.AnalysisRequest {
	return agents.AnalysisRequest{
		Symbol:       input.Symbol,
		CurrentPrice: input.CurrentPrice,
		Candles:      input.Candles,
		Indicators:   input.Indicators,
		Patterns:     input.Patterns,
		SignalScore:  input.SignalScore,
		Levels:       input.Levels,
		Research:     input.Research,
		News:         input.News,
		Portfolio:    input.Portfolio,
		MarketState:  input.MarketState,
	}
}

// validateAgainstRisk validates the decision against risk parameters.
func (p *DecisionPipeline) validateAgainstRisk(ctx context.Context, decision *models.Decision, input PipelineInput) *RiskAssessment {
	assessment := &RiskAssessment{
		Approved:   true,
		Violations: []string{},
	}

	if p.riskAgent == nil {
		return assessment
	}

	// Build request for risk check
	req := agents.AnalysisRequest{
		Symbol:       input.Symbol,
		CurrentPrice: input.CurrentPrice,
		Portfolio:    input.Portfolio,
		MarketState:  input.MarketState,
		Research:     input.Research,
	}

	// Perform risk check
	riskCheck := p.riskAgent.CheckRisk(ctx, req)

	assessment.Approved = riskCheck.Approved
	assessment.Violations = riskCheck.Violations
	assessment.SuggestedSize = riskCheck.SuggestedSize
	assessment.MaxAllowedSize = riskCheck.MaxAllowedSize
	assessment.PortfolioImpact = riskCheck.PortfolioImpact
	assessment.SectorExposure = riskCheck.SectorExposure
	assessment.DailyLossRemaining = riskCheck.DailyLossRemaining

	// Calculate risk-reward from decision
	if decision.AgentResults != nil {
		for _, result := range decision.AgentResults {
			if result.RiskReward > 0 {
				assessment.RiskRewardRatio = result.RiskReward
				break
			}
		}
	}

	// Check minimum risk-reward
	if p.riskConfig != nil && assessment.RiskRewardRatio > 0 {
		if assessment.RiskRewardRatio < p.riskConfig.MinRiskReward {
			assessment.Approved = false
			assessment.Violations = append(assessment.Violations,
				fmt.Sprintf("risk-reward ratio %.2f below minimum %.2f",
					assessment.RiskRewardRatio, p.riskConfig.MinRiskReward))
		}
	}

	return assessment
}


// shouldExecute determines if a decision should be auto-executed.
// Requirements: 26.1-26.12, 62.1-62.6
// Property 8: Auto-execute only when confidence >= threshold and risk approved
func (p *DecisionPipeline) shouldExecute(decision *models.Decision) (bool, string) {
	if p.config == nil {
		return false, "agent config not configured"
	}

	// Check 1: Operating mode
	switch p.config.AutonomousMode {
	case "MANUAL":
		return false, "operating in MANUAL mode"
	case "NOTIFY_ONLY":
		return false, "operating in NOTIFY_ONLY mode"
	case "SEMI_AUTO":
		// Only execute if unanimous
		if decision.Consensus != nil && decision.Consensus.AgreeingAgents < decision.Consensus.TotalAgents {
			return false, fmt.Sprintf("SEMI_AUTO mode requires unanimous consensus (%d/%d agents agree)",
				decision.Consensus.AgreeingAgents, decision.Consensus.TotalAgents)
		}
	case "FULL_AUTO":
		// Continue to other checks
	default:
		return false, fmt.Sprintf("unknown operating mode: %s", p.config.AutonomousMode)
	}

	// Check 2: Confidence threshold
	if decision.Confidence < p.config.AutoExecuteThreshold {
		return false, fmt.Sprintf("confidence %.1f%% below threshold %.1f%%",
			decision.Confidence, p.config.AutoExecuteThreshold)
	}

	// Check 3: Risk approval
	if decision.RiskCheck != nil && !decision.RiskCheck.Approved {
		violations := strings.Join(decision.RiskCheck.Violations, "; ")
		return false, fmt.Sprintf("risk check failed: %s", violations)
	}

	// Check 4: Daily trade limit
	p.mu.RLock()
	dailyTrades := p.dailyTrades
	dailyLoss := p.dailyLoss
	lastTradeAt := p.lastTradeAt
	consecutiveLosses := p.consecutiveLosses
	p.mu.RUnlock()

	if dailyTrades >= p.config.MaxDailyTrades {
		return false, fmt.Sprintf("daily trade limit reached: %d trades", dailyTrades)
	}

	// Check 5: Daily loss limit
	if dailyLoss >= p.config.MaxDailyLoss {
		return false, fmt.Sprintf("daily loss limit reached: ₹%.2f", dailyLoss)
	}

	// Check 6: Cooldown period
	if p.config.CooldownMinutes > 0 {
		cooldown := time.Duration(p.config.CooldownMinutes) * time.Minute
		if time.Since(lastTradeAt) < cooldown {
			remaining := cooldown - time.Since(lastTradeAt)
			return false, fmt.Sprintf("cooldown period active: %.0f seconds remaining", remaining.Seconds())
		}
	}

	// Check 7: Consecutive loss limit
	if p.config.ConsecutiveLossLimit > 0 && consecutiveLosses >= p.config.ConsecutiveLossLimit {
		return false, fmt.Sprintf("consecutive loss limit reached: %d losses", consecutiveLosses)
	}

	// Check 8: Action must be BUY or SELL (not HOLD)
	if decision.Action == "HOLD" {
		return false, "decision is HOLD, no trade to execute"
	}

	return true, ""
}

// trackDecision records a decision for accuracy tracking.
func (p *DecisionPipeline) trackDecision(decision *models.Decision) {
	p.mu.Lock()
	defer p.mu.Unlock()

	record := DecisionRecord{
		DecisionID: decision.ID,
		Symbol:     decision.Symbol,
		Action:     decision.Action,
		Confidence: decision.Confidence,
		Executed:   decision.Executed,
		Outcome:    decision.Outcome,
		PnL:        decision.PnL,
		Timestamp:  decision.Timestamp,
	}

	p.decisionHistory = append(p.decisionHistory, record)

	// Keep only last 1000 decisions in memory
	if len(p.decisionHistory) > 1000 {
		p.decisionHistory = p.decisionHistory[len(p.decisionHistory)-1000:]
	}
}

// sendNotifications sends appropriate notifications based on decision and mode.
func (p *DecisionPipeline) sendNotifications(ctx context.Context, decision *models.Decision, shouldExecute bool, blockReason string) {
	if p.notifier == nil {
		return
	}

	// Always notify for executed trades
	if shouldExecute {
		p.notifier.SendTrade(ctx, decision.Symbol, decision)
		return
	}

	// For non-executed decisions, notify based on mode
	if p.config != nil {
		switch p.config.AutonomousMode {
		case "NOTIFY_ONLY":
			// Send notification about the recommendation
			msg := fmt.Sprintf("Trade recommendation for %s: %s (Confidence: %.1f%%). Not executed: %s",
				decision.Symbol, decision.Action, decision.Confidence, blockReason)
			p.notifier.SendAlert(ctx, msg)
		case "SEMI_AUTO":
			// Notify if confidence is high but not unanimous
			if decision.Confidence >= p.config.AutoExecuteThreshold {
				msg := fmt.Sprintf("High confidence trade for %s requires approval: %s (Confidence: %.1f%%). Blocked: %s",
					decision.Symbol, decision.Action, decision.Confidence, blockReason)
				p.notifier.SendAlert(ctx, msg)
			}
		}
	}
}

// RecordTradeOutcome records the outcome of an executed trade.
func (p *DecisionPipeline) RecordTradeOutcome(decisionID string, outcome models.DecisionOutcome, pnl float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Update decision history
	for i := range p.decisionHistory {
		if p.decisionHistory[i].DecisionID == decisionID {
			p.decisionHistory[i].Outcome = outcome
			p.decisionHistory[i].PnL = pnl
			break
		}
	}

	// Update daily counters
	if pnl < 0 {
		p.dailyLoss += -pnl
		p.consecutiveLosses++
	} else {
		p.consecutiveLosses = 0
	}

	p.dailyTrades++
	p.lastTradeAt = time.Now()
}

// GetAccuracyStats returns AI decision accuracy statistics.
func (p *DecisionPipeline) GetAccuracyStats() *AccuracyStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := &AccuracyStats{
		ByConfidenceRange: make(map[string]*RangeStats),
	}

	for _, record := range p.decisionHistory {
		stats.TotalDecisions++

		if record.Executed {
			stats.ExecutedTrades++

			switch record.Outcome {
			case models.OutcomeWin:
				stats.WinningTrades++
				stats.TotalPnL += record.PnL
			case models.OutcomeLoss:
				stats.LosingTrades++
				stats.TotalPnL += record.PnL
			}
		}

		// Track by confidence range
		rangeKey := getConfidenceRange(record.Confidence)
		if _, ok := stats.ByConfidenceRange[rangeKey]; !ok {
			stats.ByConfidenceRange[rangeKey] = &RangeStats{}
		}
		rangeStats := stats.ByConfidenceRange[rangeKey]
		rangeStats.Total++
		if record.Executed {
			rangeStats.Executed++
			if record.Outcome == models.OutcomeWin {
				rangeStats.Wins++
			}
		}
	}

	// Calculate rates
	if stats.ExecutedTrades > 0 {
		stats.WinRate = float64(stats.WinningTrades) / float64(stats.ExecutedTrades) * 100
		stats.AvgPnL = stats.TotalPnL / float64(stats.ExecutedTrades)
	}

	// Calculate accuracy by range
	for _, rangeStats := range stats.ByConfidenceRange {
		if rangeStats.Executed > 0 {
			rangeStats.WinRate = float64(rangeStats.Wins) / float64(rangeStats.Executed) * 100
		}
	}

	return stats
}

// AccuracyStats contains AI decision accuracy statistics.
type AccuracyStats struct {
	TotalDecisions    int
	ExecutedTrades    int
	WinningTrades     int
	LosingTrades      int
	WinRate           float64
	TotalPnL          float64
	AvgPnL            float64
	ByConfidenceRange map[string]*RangeStats
}

// RangeStats contains statistics for a confidence range.
type RangeStats struct {
	Total    int
	Executed int
	Wins     int
	WinRate  float64
}

func getConfidenceRange(confidence float64) string {
	switch {
	case confidence >= 90:
		return "90-100"
	case confidence >= 80:
		return "80-90"
	case confidence >= 70:
		return "70-80"
	case confidence >= 60:
		return "60-70"
	case confidence >= 50:
		return "50-60"
	default:
		return "0-50"
	}
}

// ResetDailyCounters resets daily tracking counters.
func (p *DecisionPipeline) ResetDailyCounters() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.dailyTrades = 0
	p.dailyLoss = 0
	p.consecutiveLosses = 0
}

// GetDailyStats returns current daily statistics.
func (p *DecisionPipeline) GetDailyStats() DailyStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return DailyStats{
		Trades:            p.dailyTrades,
		Loss:              p.dailyLoss,
		ConsecutiveLosses: p.consecutiveLosses,
		LastTradeAt:       p.lastTradeAt,
	}
}

// DailyStats contains daily trading statistics.
type DailyStats struct {
	Trades            int
	Loss              float64
	ConsecutiveLosses int
	LastTradeAt       time.Time
}

// SetDailyStats sets daily statistics (for testing or state recovery).
func (p *DecisionPipeline) SetDailyStats(trades int, loss float64, consecutiveLosses int, lastTradeAt time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.dailyTrades = trades
	p.dailyLoss = loss
	p.consecutiveLosses = consecutiveLosses
	p.lastTradeAt = lastTradeAt
}

// CanTrade checks if trading is currently allowed.
func (p *DecisionPipeline) CanTrade() (bool, string) {
	if p.config == nil {
		return false, "agent config not configured"
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.dailyTrades >= p.config.MaxDailyTrades {
		return false, fmt.Sprintf("daily trade limit reached: %d", p.dailyTrades)
	}

	if p.dailyLoss >= p.config.MaxDailyLoss {
		return false, fmt.Sprintf("daily loss limit reached: ₹%.2f", p.dailyLoss)
	}

	if p.config.ConsecutiveLossLimit > 0 && p.consecutiveLosses >= p.config.ConsecutiveLossLimit {
		return false, fmt.Sprintf("consecutive loss limit reached: %d", p.consecutiveLosses)
	}

	return true, ""
}
