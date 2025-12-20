// Package trading provides trading operations and execution logic.
package trading

import (
	"fmt"
	"strings"
	"time"

	"zerodha-trader/internal/config"
	"zerodha-trader/internal/models"
)

// ExecutionChecker validates whether a trade decision should be auto-executed.
// Requirements: 26.1-26.12, 62.1-62.6
// Property 8: Auto-execute only when confidence >= threshold and risk approved
type ExecutionChecker struct {
	config *config.AgentConfig
}

// ExecutionState represents the current trading state.
type ExecutionState struct {
	DailyTrades       int
	DailyLoss         float64
	LastTradeAt       time.Time
	ConsecutiveLosses int
}

// ExecutionResult contains the result of an execution check.
type ExecutionResult struct {
	ShouldExecute bool
	BlockReason   string
	ChecksPassed  []string
	ChecksFailed  []string
}

// NewExecutionChecker creates a new execution checker.
func NewExecutionChecker(agentConfig *config.AgentConfig) *ExecutionChecker {
	return &ExecutionChecker{
		config: agentConfig,
	}
}

// CheckExecution determines if a decision should be auto-executed.
// This is the core execution logic that implements Property 8:
// Auto-execute only when confidence >= threshold AND risk approved.
func (e *ExecutionChecker) CheckExecution(decision *models.Decision, state ExecutionState) ExecutionResult {
	result := ExecutionResult{
		ShouldExecute: true,
		ChecksPassed:  []string{},
		ChecksFailed:  []string{},
	}

	if e.config == nil {
		result.ShouldExecute = false
		result.BlockReason = "agent config not configured"
		result.ChecksFailed = append(result.ChecksFailed, "config")
		return result
	}

	// Check 1: Operating mode
	modeOK, modeReason := e.checkOperatingMode(decision)
	if !modeOK {
		result.ShouldExecute = false
		result.BlockReason = modeReason
		result.ChecksFailed = append(result.ChecksFailed, "operating_mode")
		return result
	}
	result.ChecksPassed = append(result.ChecksPassed, "operating_mode")

	// Check 2: Confidence threshold (CRITICAL for Property 8)
	confOK, confReason := e.checkConfidenceThreshold(decision)
	if !confOK {
		result.ShouldExecute = false
		result.BlockReason = confReason
		result.ChecksFailed = append(result.ChecksFailed, "confidence_threshold")
		return result
	}
	result.ChecksPassed = append(result.ChecksPassed, "confidence_threshold")

	// Check 3: Risk approval (CRITICAL for Property 8)
	riskOK, riskReason := e.checkRiskApproval(decision)
	if !riskOK {
		result.ShouldExecute = false
		result.BlockReason = riskReason
		result.ChecksFailed = append(result.ChecksFailed, "risk_approval")
		return result
	}
	result.ChecksPassed = append(result.ChecksPassed, "risk_approval")

	// Check 4: Daily trade limit
	limitOK, limitReason := e.checkDailyTradeLimit(state)
	if !limitOK {
		result.ShouldExecute = false
		result.BlockReason = limitReason
		result.ChecksFailed = append(result.ChecksFailed, "daily_trade_limit")
		return result
	}
	result.ChecksPassed = append(result.ChecksPassed, "daily_trade_limit")

	// Check 5: Daily loss limit
	lossOK, lossReason := e.checkDailyLossLimit(state)
	if !lossOK {
		result.ShouldExecute = false
		result.BlockReason = lossReason
		result.ChecksFailed = append(result.ChecksFailed, "daily_loss_limit")
		return result
	}
	result.ChecksPassed = append(result.ChecksPassed, "daily_loss_limit")

	// Check 6: Cooldown period
	cooldownOK, cooldownReason := e.checkCooldownPeriod(state)
	if !cooldownOK {
		result.ShouldExecute = false
		result.BlockReason = cooldownReason
		result.ChecksFailed = append(result.ChecksFailed, "cooldown_period")
		return result
	}
	result.ChecksPassed = append(result.ChecksPassed, "cooldown_period")

	// Check 7: Consecutive loss limit
	consLossOK, consLossReason := e.checkConsecutiveLossLimit(state)
	if !consLossOK {
		result.ShouldExecute = false
		result.BlockReason = consLossReason
		result.ChecksFailed = append(result.ChecksFailed, "consecutive_loss_limit")
		return result
	}
	result.ChecksPassed = append(result.ChecksPassed, "consecutive_loss_limit")

	// Check 8: Action must be BUY or SELL
	actionOK, actionReason := e.checkActionType(decision)
	if !actionOK {
		result.ShouldExecute = false
		result.BlockReason = actionReason
		result.ChecksFailed = append(result.ChecksFailed, "action_type")
		return result
	}
	result.ChecksPassed = append(result.ChecksPassed, "action_type")

	return result
}

// checkOperatingMode validates the operating mode allows execution.
func (e *ExecutionChecker) checkOperatingMode(decision *models.Decision) (bool, string) {
	switch e.config.AutonomousMode {
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
		return true, ""
	case "FULL_AUTO":
		return true, ""
	default:
		return false, fmt.Sprintf("unknown operating mode: %s", e.config.AutonomousMode)
	}
}

// checkConfidenceThreshold validates confidence meets threshold.
// This is a critical check for Property 8.
func (e *ExecutionChecker) checkConfidenceThreshold(decision *models.Decision) (bool, string) {
	if decision.Confidence < e.config.AutoExecuteThreshold {
		return false, fmt.Sprintf("confidence %.1f%% below threshold %.1f%%",
			decision.Confidence, e.config.AutoExecuteThreshold)
	}
	return true, ""
}

// checkRiskApproval validates risk check passed.
// This is a critical check for Property 8.
func (e *ExecutionChecker) checkRiskApproval(decision *models.Decision) (bool, string) {
	if decision.RiskCheck != nil && !decision.RiskCheck.Approved {
		violations := strings.Join(decision.RiskCheck.Violations, "; ")
		if violations == "" {
			violations = "risk check failed"
		}
		return false, fmt.Sprintf("risk check failed: %s", violations)
	}
	return true, ""
}

// checkDailyTradeLimit validates daily trade count is within limit.
func (e *ExecutionChecker) checkDailyTradeLimit(state ExecutionState) (bool, string) {
	if e.config.MaxDailyTrades > 0 && state.DailyTrades >= e.config.MaxDailyTrades {
		return false, fmt.Sprintf("daily trade limit reached: %d trades", state.DailyTrades)
	}
	return true, ""
}

// checkDailyLossLimit validates daily loss is within limit.
func (e *ExecutionChecker) checkDailyLossLimit(state ExecutionState) (bool, string) {
	if e.config.MaxDailyLoss > 0 && state.DailyLoss >= e.config.MaxDailyLoss {
		return false, fmt.Sprintf("daily loss limit reached: ₹%.2f", state.DailyLoss)
	}
	return true, ""
}

// checkCooldownPeriod validates cooldown period has elapsed.
func (e *ExecutionChecker) checkCooldownPeriod(state ExecutionState) (bool, string) {
	if e.config.CooldownMinutes > 0 && !state.LastTradeAt.IsZero() {
		cooldown := time.Duration(e.config.CooldownMinutes) * time.Minute
		elapsed := time.Since(state.LastTradeAt)
		if elapsed < cooldown {
			remaining := cooldown - elapsed
			return false, fmt.Sprintf("cooldown period active: %.0f seconds remaining", remaining.Seconds())
		}
	}
	return true, ""
}

// checkConsecutiveLossLimit validates consecutive losses are within limit.
func (e *ExecutionChecker) checkConsecutiveLossLimit(state ExecutionState) (bool, string) {
	if e.config.ConsecutiveLossLimit > 0 && state.ConsecutiveLosses >= e.config.ConsecutiveLossLimit {
		return false, fmt.Sprintf("consecutive loss limit reached: %d losses", state.ConsecutiveLosses)
	}
	return true, ""
}

// checkActionType validates the action is executable (BUY or SELL).
func (e *ExecutionChecker) checkActionType(decision *models.Decision) (bool, string) {
	if decision.Action == "HOLD" || decision.Action == "" {
		return false, "decision is HOLD, no trade to execute"
	}
	return true, ""
}

// ShouldAutoExecute is a convenience method that returns just the boolean result.
// Property 8: Auto-execute only when confidence >= threshold and risk approved.
func (e *ExecutionChecker) ShouldAutoExecute(decision *models.Decision, state ExecutionState) bool {
	result := e.CheckExecution(decision, state)
	return result.ShouldExecute
}

// GetOperatingMode returns the current operating mode.
func (e *ExecutionChecker) GetOperatingMode() string {
	if e.config == nil {
		return "UNKNOWN"
	}
	return e.config.AutonomousMode
}

// IsAutoExecuteEnabled returns true if auto-execution is enabled.
func (e *ExecutionChecker) IsAutoExecuteEnabled() bool {
	if e.config == nil {
		return false
	}
	return e.config.AutonomousMode == "FULL_AUTO" || e.config.AutonomousMode == "SEMI_AUTO"
}

// GetConfidenceThreshold returns the configured confidence threshold.
func (e *ExecutionChecker) GetConfidenceThreshold() float64 {
	if e.config == nil {
		return 100 // Effectively disable auto-execution
	}
	return e.config.AutoExecuteThreshold
}
