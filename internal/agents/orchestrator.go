// Package agents provides AI agent implementations for trading decisions.
package agents

import (
	"context"
	"fmt"
	"sync"
	"time"

	"zerodha-trader/internal/config"
	"zerodha-trader/internal/models"
	"zerodha-trader/internal/store"
)

// Orchestrator coordinates multiple agents running in parallel.
// Requirements: 11.1, 11.7-11.12, 14.1-14.6
type Orchestrator struct {
	agents      []Agent
	traderAgent *TraderAgent
	riskAgent   *RiskAgent
	config      *config.AgentConfig
	store       store.DataStore
	notifier    Notifier

	// State
	mu                sync.RWMutex
	running           bool
	paused            bool
	dailyTrades       int
	dailyLoss         float64
	lastTradeAt       time.Time
	consecutiveLosses int

	// Channels
	stopChan chan struct{}
}

// Notifier defines the interface for sending notifications.
type Notifier interface {
	SendTrade(ctx context.Context, symbol string, decision *models.Decision) error
	SendAlert(ctx context.Context, message string) error
	SendError(ctx context.Context, err error, context string) error
}

// OrchestratorStatus represents the current status of the orchestrator.
type OrchestratorStatus struct {
	Running           bool
	Paused            bool
	DailyTrades       int
	DailyLoss         float64
	LastTradeAt       time.Time
	ConsecutiveLosses int
	EnabledAgents     []string
}

// NewOrchestrator creates a new agent orchestrator.
func NewOrchestrator(
	agents []Agent,
	traderAgent *TraderAgent,
	riskAgent *RiskAgent,
	agentConfig *config.AgentConfig,
	dataStore store.DataStore,
	notifier Notifier,
) *Orchestrator {
	return &Orchestrator{
		agents:      agents,
		traderAgent: traderAgent,
		riskAgent:   riskAgent,
		config:      agentConfig,
		store:       dataStore,
		notifier:    notifier,
		stopChan:    make(chan struct{}),
	}
}

// Start starts the orchestrator.
func (o *Orchestrator) Start(ctx context.Context) error {
	o.mu.Lock()
	if o.running {
		o.mu.Unlock()
		return fmt.Errorf("orchestrator already running")
	}
	o.running = true
	o.paused = false
	o.mu.Unlock()

	// Reset daily counters at start
	o.resetDailyCounters()

	return nil
}

// Stop stops the orchestrator.
func (o *Orchestrator) Stop() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.running {
		return fmt.Errorf("orchestrator not running")
	}

	o.running = false
	close(o.stopChan)
	o.stopChan = make(chan struct{}) // Reset for next start

	return nil
}

// Pause pauses the orchestrator.
func (o *Orchestrator) Pause() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.running {
		return fmt.Errorf("orchestrator not running")
	}

	o.paused = true
	return nil
}

// Resume resumes the orchestrator.
func (o *Orchestrator) Resume() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.running {
		return fmt.Errorf("orchestrator not running")
	}

	o.paused = false
	return nil
}

// GetStatus returns the current orchestrator status.
func (o *Orchestrator) GetStatus() *OrchestratorStatus {
	o.mu.RLock()
	defer o.mu.RUnlock()

	enabledAgents := make([]string, 0, len(o.agents))
	for _, agent := range o.agents {
		enabledAgents = append(enabledAgents, agent.Name())
	}

	return &OrchestratorStatus{
		Running:           o.running,
		Paused:            o.paused,
		DailyTrades:       o.dailyTrades,
		DailyLoss:         o.dailyLoss,
		LastTradeAt:       o.lastTradeAt,
		ConsecutiveLosses: o.consecutiveLosses,
		EnabledAgents:     enabledAgents,
	}
}

// ProcessSymbol processes a symbol through all agents and returns a decision.
func (o *Orchestrator) ProcessSymbol(ctx context.Context, req AnalysisRequest) (*models.Decision, error) {
	o.mu.RLock()
	if o.paused {
		o.mu.RUnlock()
		return nil, fmt.Errorf("orchestrator is paused")
	}
	o.mu.RUnlock()

	// Run all agents in parallel with timeout
	results, err := o.runAgentsParallel(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("running agents: %w", err)
	}

	// Make final decision using trader agent
	decision, err := o.traderAgent.MakeFinalDecision(ctx, req, results)
	if err != nil {
		return nil, fmt.Errorf("making final decision: %w", err)
	}

	// Perform risk check and enhanced position sizing
	if o.riskAgent != nil {
		riskCheck := o.riskAgent.CheckRisk(ctx, req)
		decision.RiskCheck = &models.RiskCheckResult{
			Approved:        riskCheck.Approved,
			Violations:      riskCheck.Violations,
			PositionSize:    riskCheck.SuggestedSize,
			PortfolioImpact: riskCheck.PortfolioImpact,
			SectorExposure:  riskCheck.SectorExposure,
			DailyLossStatus: riskCheck.DailyLossRemaining,
			VolAdjustedSize: riskCheck.SuggestedSize, // already vol-adjusted in CalculatePositionSize
		}
		decision.PositionSizeQty = int(riskCheck.SuggestedSize)

		// Calculate max loss if stop is hit
		if decision.StopLoss > 0 && decision.EntryPrice > 0 && riskCheck.SuggestedSize > 0 {
			var riskPerShare float64
			if decision.Action == "BUY" {
				riskPerShare = decision.EntryPrice - decision.StopLoss
			} else {
				riskPerShare = decision.StopLoss - decision.EntryPrice
			}
			decision.RiskCheck.MaxLossAmount = riskPerShare * riskCheck.SuggestedSize

			// Reject if max loss exceeds daily loss remaining
			if riskCheck.DailyLossRemaining > 0 && decision.RiskCheck.MaxLossAmount > riskCheck.DailyLossRemaining {
				decision.RiskCheck.Approved = false
				decision.RiskCheck.Violations = append(decision.RiskCheck.Violations,
					fmt.Sprintf("max loss ₹%.0f exceeds daily remaining ₹%.0f",
						decision.RiskCheck.MaxLossAmount, riskCheck.DailyLossRemaining))
			}
		}

		// Validate risk-reward ratio
		if decision.RiskRewardRatio > 0 && decision.RiskRewardRatio < 1.5 {
			decision.RiskCheck.Approved = false
			decision.RiskCheck.Violations = append(decision.RiskCheck.Violations,
				fmt.Sprintf("risk-reward ratio %.1f below minimum 1.5", decision.RiskRewardRatio))
		}
	}

	// Check if we should execute
	decision.Executed = o.shouldExecute(decision)

	// Save decision to store
	if o.store != nil {
		if err := o.store.SaveDecision(ctx, decision); err != nil {
			if o.notifier != nil {
				o.notifier.SendError(ctx, err, "saving decision")
			}
		} else {
			o.logDecisionEvent(ctx, decision, models.DecisionStageGenerated, "OK", "decision generated", map[string]interface{}{
				"confidence":    decision.Confidence,
				"entry_price":   decision.EntryPrice,
				"stop_loss":     decision.StopLoss,
				"targets":       decision.Targets,
				"agent_count":   len(results),
				"risk_approved": decision.RiskCheck == nil || decision.RiskCheck.Approved,
				"authority":     "deterministic_consensus",
			})
			if decision.RiskCheck != nil {
				status := "APPROVED"
				message := "risk check approved"
				if !decision.RiskCheck.Approved {
					status = "REJECTED"
					message = "risk check rejected"
				}
				o.logDecisionEvent(ctx, decision, models.DecisionStageRiskChecked, status, message, map[string]interface{}{
					"violations":       decision.RiskCheck.Violations,
					"position_size":    decision.RiskCheck.PositionSize,
					"portfolio_impact": decision.RiskCheck.PortfolioImpact,
					"max_loss_amount":  decision.RiskCheck.MaxLossAmount,
				})
			}
			stage := models.DecisionStageExecutionBlocked
			status := "BLOCKED"
			message := "decision not selected for execution"
			if decision.Executed {
				stage = models.DecisionStageExecutionSelected
				status = "SELECTED"
				message = "decision selected for execution"
			}
			o.logDecisionEvent(ctx, decision, stage, status, message, map[string]interface{}{
				"mode":       o.autonomousMode(),
				"confidence": decision.Confidence,
				"threshold":  o.autoExecuteThreshold(),
			})
		}
	}

	return decision, nil
}

func (o *Orchestrator) logDecisionEvent(ctx context.Context, decision *models.Decision, stage models.DecisionLogStage, status, message string, payload map[string]interface{}) {
	if o.store == nil || decision == nil || decision.ID == "" {
		return
	}
	if err := o.store.SaveDecisionLog(ctx, &models.DecisionLog{
		ID:         fmt.Sprintf("DLOG_%d", time.Now().UnixNano()),
		DecisionID: decision.ID,
		Timestamp:  time.Now(),
		Stage:      stage,
		Symbol:     decision.Symbol,
		Action:     decision.Action,
		Status:     status,
		Message:    message,
		Payload:    payload,
	}); err != nil && o.notifier != nil {
		o.notifier.SendError(ctx, err, "saving decision log")
	}
}

func (o *Orchestrator) autonomousMode() string {
	if o.config == nil {
		return "UNKNOWN"
	}
	return o.config.AutonomousMode
}

func (o *Orchestrator) autoExecuteThreshold() float64 {
	if o.config == nil {
		return 0
	}
	return o.config.AutoExecuteThreshold
}

// agentResult holds the result from a single agent.
type agentResult struct {
	name   string
	result *AnalysisResult
	err    error
}

// runAgentsParallel runs all agents in parallel with timeout handling.
func (o *Orchestrator) runAgentsParallel(ctx context.Context, req AnalysisRequest) (map[string]*AnalysisResult, error) {
	// Create timeout context
	timeout := 30 * time.Second
	if o.config != nil && o.config.CooldownMinutes > 0 {
		timeout = time.Duration(o.config.CooldownMinutes) * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Channel for results
	resultChan := make(chan agentResult, len(o.agents))

	// Start all agents in parallel
	var wg sync.WaitGroup
	for _, agent := range o.agents {
		wg.Add(1)
		go func(a Agent) {
			defer wg.Done()

			result, err := a.Analyze(ctx, req)
			resultChan <- agentResult{
				name:   a.Name(),
				result: result,
				err:    err,
			}
		}(agent)
	}

	// Close channel when all agents complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	results := make(map[string]*AnalysisResult)
	var errors []string

	for ar := range resultChan {
		if ar.err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", ar.name, ar.err))
			continue
		}
		if ar.result != nil {
			results[ar.name] = ar.result
		}
	}

	// Log errors but continue if we have some results
	if len(errors) > 0 && o.notifier != nil {
		o.notifier.SendAlert(ctx, fmt.Sprintf("Agent errors: %s", errors))
	}

	// Need at least one result
	if len(results) == 0 {
		return nil, fmt.Errorf("all agents failed: %v", errors)
	}

	return results, nil
}

// shouldExecute determines if a decision should be auto-executed.
func (o *Orchestrator) shouldExecute(decision *models.Decision) bool {
	if o.config == nil {
		return false
	}

	// Check operating mode
	switch o.config.AutonomousMode {
	case "MANUAL":
		return false
	case "NOTIFY_ONLY":
		return false
	case "SEMI_AUTO":
		// Only execute if unanimous
		if decision.Consensus != nil && decision.Consensus.AgreeingAgents < decision.Consensus.TotalAgents {
			return false
		}
	case "FULL_AUTO":
		// Continue to other checks
	default:
		return false
	}

	// Check confidence threshold
	if decision.Confidence < o.config.AutoExecuteThreshold {
		return false
	}

	// Check risk approval
	if decision.RiskCheck != nil && !decision.RiskCheck.Approved {
		return false
	}

	// Check daily limits
	o.mu.RLock()
	defer o.mu.RUnlock()

	if o.dailyTrades >= o.config.MaxDailyTrades {
		return false
	}

	if o.dailyLoss >= o.config.MaxDailyLoss {
		return false
	}

	// Check cooldown
	if o.config.CooldownMinutes > 0 {
		cooldown := time.Duration(o.config.CooldownMinutes) * time.Minute
		if time.Since(o.lastTradeAt) < cooldown {
			return false
		}
	}

	// Check consecutive losses
	if o.consecutiveLosses >= o.config.ConsecutiveLossLimit {
		return false
	}

	return true
}

// RecordTrade records a trade execution and updates agent accuracy tracking.
func (o *Orchestrator) RecordTrade(pnl float64) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.dailyTrades++
	o.lastTradeAt = time.Now()

	if pnl < 0 {
		o.dailyLoss += -pnl
		o.consecutiveLosses++
	} else {
		o.consecutiveLosses = 0
	}
}

// RecordDecisionOutcome records the outcome of a decision and updates agent accuracy.
// This is the feedback loop: agents that were right get their weight boosted,
// agents that were wrong get their weight reduced.
func (o *Orchestrator) RecordDecisionOutcome(ctx context.Context, decisionID string, outcome models.DecisionOutcome, pnl float64) error {
	if o.store == nil {
		return fmt.Errorf("data store not configured")
	}

	// Update the decision in the store
	if err := o.store.UpdateDecisionOutcome(ctx, decisionID, outcome, pnl); err != nil {
		return fmt.Errorf("updating decision outcome: %w", err)
	}

	// Fetch the decision to analyze agent accuracy
	decision, err := o.store.GetDecisionByID(ctx, decisionID)
	if err != nil || decision == nil {
		return err
	}

	// Determine if the decision was correct
	wasCorrect := (outcome == models.OutcomeWin)
	actualDirection := decision.Action // BUY or SELL

	// Update agent weights based on who was right
	if o.traderAgent != nil && decision.AgentResults != nil {
		for agentName, agentResult := range decision.AgentResults {
			agentAgreed := agentResult.Recommendation == actualDirection
			agentWasRight := (agentAgreed && wasCorrect) || (!agentAgreed && !wasCorrect)

			currentWeight := o.traderAgent.getAgentWeight(agentName)

			// Small incremental adjustment (±2% of current weight)
			// This prevents wild swings but allows gradual adaptation
			adjustment := currentWeight * 0.02
			if agentWasRight {
				o.traderAgent.agentWeights[agentName] = currentWeight + adjustment
			} else {
				newWeight := currentWeight - adjustment
				if newWeight < 0.05 {
					newWeight = 0.05 // floor: never zero out an agent
				}
				o.traderAgent.agentWeights[agentName] = newWeight
			}
		}

		// Normalize weights so they sum to 1.0
		o.normalizeAgentWeights()
	}

	return nil
}

// normalizeAgentWeights ensures agent weights sum to 1.0.
func (o *Orchestrator) normalizeAgentWeights() {
	if o.traderAgent == nil {
		return
	}

	var total float64
	for _, w := range o.traderAgent.agentWeights {
		total += w
	}
	if total <= 0 {
		return
	}
	for name, w := range o.traderAgent.agentWeights {
		o.traderAgent.agentWeights[name] = w / total
	}
}

// resetDailyCounters resets daily tracking counters.
func (o *Orchestrator) resetDailyCounters() {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.dailyTrades = 0
	o.dailyLoss = 0
	o.consecutiveLosses = 0
}

// GetDecisionStats returns statistics about AI decisions.
func (o *Orchestrator) GetDecisionStats(ctx context.Context, days int) (*models.AIStats, error) {
	if o.store == nil {
		return nil, fmt.Errorf("data store not configured")
	}

	dateRange := store.DateRange{
		Start: time.Now().AddDate(0, 0, -days),
		End:   time.Now(),
	}

	return o.store.GetDecisionStats(ctx, dateRange)
}

// AddAgent adds an agent to the orchestrator.
func (o *Orchestrator) AddAgent(agent Agent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.agents = append(o.agents, agent)
}

// RemoveAgent removes an agent from the orchestrator.
func (o *Orchestrator) RemoveAgent(name string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	for i, agent := range o.agents {
		if agent.Name() == name {
			o.agents = append(o.agents[:i], o.agents[i+1:]...)
			return
		}
	}
}

// GetAgents returns the list of registered agents.
func (o *Orchestrator) GetAgents() []Agent {
	o.mu.RLock()
	defer o.mu.RUnlock()

	agents := make([]Agent, len(o.agents))
	copy(agents, o.agents)
	return agents
}

// SetConfig updates the orchestrator configuration.
func (o *Orchestrator) SetConfig(cfg *config.AgentConfig) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.config = cfg
}

// CanTrade checks if trading is currently allowed.
func (o *Orchestrator) CanTrade() (bool, string) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	if !o.running {
		return false, "orchestrator not running"
	}

	if o.paused {
		return false, "orchestrator is paused"
	}

	if o.config != nil {
		if o.dailyTrades >= o.config.MaxDailyTrades {
			return false, fmt.Sprintf("daily trade limit reached: %d", o.dailyTrades)
		}

		if o.dailyLoss >= o.config.MaxDailyLoss {
			return false, fmt.Sprintf("daily loss limit reached: %.2f", o.dailyLoss)
		}

		if o.consecutiveLosses >= o.config.ConsecutiveLossLimit {
			return false, fmt.Sprintf("consecutive loss limit reached: %d", o.consecutiveLosses)
		}
	}

	return true, ""
}
