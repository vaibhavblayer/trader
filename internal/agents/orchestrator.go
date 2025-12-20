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

	// Perform risk check
	if o.riskAgent != nil {
		riskCheck := o.riskAgent.CheckRisk(ctx, req)
		decision.RiskCheck = &models.RiskCheckResult{
			Approved:        riskCheck.Approved,
			Violations:      riskCheck.Violations,
			PositionSize:    riskCheck.SuggestedSize,
			PortfolioImpact: riskCheck.PortfolioImpact,
			SectorExposure:  riskCheck.SectorExposure,
			DailyLossStatus: riskCheck.DailyLossRemaining,
		}
	}

	// Check if we should execute
	decision.Executed = o.shouldExecute(decision)

	// Save decision to store
	if o.store != nil {
		if err := o.store.SaveDecision(ctx, decision); err != nil {
			// Log error but don't fail
			if o.notifier != nil {
				o.notifier.SendError(ctx, err, "saving decision")
			}
		}
	}

	return decision, nil
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

// RecordTrade records a trade execution for tracking.
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
