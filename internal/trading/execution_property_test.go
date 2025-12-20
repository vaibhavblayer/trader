package trading

import (
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"zerodha-trader/internal/config"
	"zerodha-trader/internal/models"
)

// Feature: zerodha-go-trader, Property 8: Auto-execute only when confidence >= threshold and risk approved
// Validates: Requirements 26.4, 27.9
//
// Property: For any decision, auto-execution should only occur when:
// 1. Confidence >= AutoExecuteThreshold
// 2. RiskCheck.Approved == true (or RiskCheck is nil)
// 3. Operating mode allows execution (FULL_AUTO or SEMI_AUTO with unanimous consensus)
// 4. Daily limits not exceeded
// 5. Cooldown period elapsed
// 6. Action is BUY or SELL (not HOLD)
func TestProperty_AutoExecuteOnlyWhenConfidenceAndRiskApproved(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	// Generator for confidence values
	confidenceGen := gen.Float64Range(0, 100)

	// Generator for threshold values
	thresholdGen := gen.Float64Range(50, 95)

	// Generator for risk approval
	riskApprovedGen := gen.Bool()

	properties.Property("Auto-execute only when confidence >= threshold AND risk approved", prop.ForAll(
		func(confidence, threshold float64, riskApproved bool) bool {
			// Create config with the threshold
			cfg := &config.AgentConfig{
				AutonomousMode:       "FULL_AUTO",
				AutoExecuteThreshold: threshold,
				MaxDailyTrades:       100,
				MaxDailyLoss:         100000,
				CooldownMinutes:      0,
				ConsecutiveLossLimit: 10,
			}

			checker := NewExecutionChecker(cfg)

			// Create decision with the confidence and risk approval
			decision := &models.Decision{
				ID:         "TEST-001",
				Symbol:     "RELIANCE",
				Action:     "BUY",
				Confidence: confidence,
				Consensus: &models.ConsensusDetails{
					TotalAgents:    4,
					AgreeingAgents: 4,
					WeightedScore:  confidence,
				},
			}

			// Set risk check based on riskApproved
			if !riskApproved {
				decision.RiskCheck = &models.RiskCheckResult{
					Approved:   false,
					Violations: []string{"test violation"},
				}
			} else {
				decision.RiskCheck = &models.RiskCheckResult{
					Approved:   true,
					Violations: []string{},
				}
			}

			// Create clean state (no limits hit)
			state := ExecutionState{
				DailyTrades:       0,
				DailyLoss:         0,
				LastTradeAt:       time.Time{},
				ConsecutiveLosses: 0,
			}

			result := checker.CheckExecution(decision, state)

			// Property 8: Auto-execute ONLY when confidence >= threshold AND risk approved
			meetsConfidence := confidence >= threshold
			meetsRisk := riskApproved

			// If both conditions are met, should execute
			// If either condition fails, should NOT execute
			expectedExecute := meetsConfidence && meetsRisk

			if result.ShouldExecute != expectedExecute {
				t.Logf("FAILED: confidence=%.2f, threshold=%.2f, riskApproved=%v, expected=%v, got=%v, reason=%s",
					confidence, threshold, riskApproved, expectedExecute, result.ShouldExecute, result.BlockReason)
				return false
			}

			return true
		},
		confidenceGen,
		thresholdGen,
		riskApprovedGen,
	))

	properties.TestingRun(t)
}

// TestProperty_NoExecutionWhenConfidenceBelowThreshold tests that execution is blocked
// when confidence is below threshold, regardless of other conditions.
func TestProperty_NoExecutionWhenConfidenceBelowThreshold(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	// Generator for threshold (fixed high value)
	thresholdGen := gen.Float64Range(70, 95)

	// Generator for confidence below threshold
	// We'll generate a delta and subtract from threshold
	deltaGen := gen.Float64Range(0.1, 30)

	properties.Property("No execution when confidence below threshold", prop.ForAll(
		func(threshold, delta float64) bool {
			confidence := threshold - delta
			if confidence < 0 {
				confidence = 0
			}

			cfg := &config.AgentConfig{
				AutonomousMode:       "FULL_AUTO",
				AutoExecuteThreshold: threshold,
				MaxDailyTrades:       100,
				MaxDailyLoss:         100000,
				CooldownMinutes:      0,
				ConsecutiveLossLimit: 10,
			}

			checker := NewExecutionChecker(cfg)

			// Create decision with confidence below threshold but risk approved
			decision := &models.Decision{
				ID:         "TEST-002",
				Symbol:     "TCS",
				Action:     "SELL",
				Confidence: confidence,
				RiskCheck: &models.RiskCheckResult{
					Approved: true,
				},
				Consensus: &models.ConsensusDetails{
					TotalAgents:    4,
					AgreeingAgents: 4,
				},
			}

			state := ExecutionState{}

			result := checker.CheckExecution(decision, state)

			// Should NOT execute because confidence < threshold
			if result.ShouldExecute {
				t.Logf("FAILED: Should not execute when confidence (%.2f) < threshold (%.2f)",
					confidence, threshold)
				return false
			}

			// Verify the block reason mentions confidence
			if result.BlockReason == "" || !containsAny(result.BlockReason, "confidence", "threshold") {
				t.Logf("FAILED: Block reason should mention confidence: %s", result.BlockReason)
				return false
			}

			return true
		},
		thresholdGen,
		deltaGen,
	))

	properties.TestingRun(t)
}

// TestProperty_NoExecutionWhenRiskNotApproved tests that execution is blocked
// when risk check fails, regardless of confidence.
func TestProperty_NoExecutionWhenRiskNotApproved(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	// Generator for high confidence (above any reasonable threshold)
	confidenceGen := gen.Float64Range(85, 100)

	// Generator for violation messages - use predefined violations instead of random strings
	violationGen := gen.OneConstOf(
		"position size exceeded",
		"sector exposure limit",
		"daily loss limit",
		"risk-reward ratio too low",
		"insufficient margin",
	)

	properties.Property("No execution when risk not approved", prop.ForAll(
		func(confidence float64, violation string) bool {
			cfg := &config.AgentConfig{
				AutonomousMode:       "FULL_AUTO",
				AutoExecuteThreshold: 80, // Below our confidence
				MaxDailyTrades:       100,
				MaxDailyLoss:         100000,
				CooldownMinutes:      0,
				ConsecutiveLossLimit: 10,
			}

			checker := NewExecutionChecker(cfg)

			// Create decision with high confidence but risk NOT approved
			decision := &models.Decision{
				ID:         "TEST-003",
				Symbol:     "INFY",
				Action:     "BUY",
				Confidence: confidence,
				RiskCheck: &models.RiskCheckResult{
					Approved:   false,
					Violations: []string{violation},
				},
				Consensus: &models.ConsensusDetails{
					TotalAgents:    4,
					AgreeingAgents: 4,
				},
			}

			state := ExecutionState{}

			result := checker.CheckExecution(decision, state)

			// Should NOT execute because risk not approved
			if result.ShouldExecute {
				t.Logf("FAILED: Should not execute when risk not approved (confidence=%.2f)", confidence)
				return false
			}

			// Verify the block reason mentions risk
			if result.BlockReason == "" || !containsAny(result.BlockReason, "risk", "violation") {
				t.Logf("FAILED: Block reason should mention risk: %s", result.BlockReason)
				return false
			}

			return true
		},
		confidenceGen,
		violationGen,
	))

	properties.TestingRun(t)
}

// TestProperty_ExecutionBlockedByDailyLimits tests that daily limits block execution.
func TestProperty_ExecutionBlockedByDailyLimits(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	// Generator for max daily trades
	maxTradesGen := gen.IntRange(1, 20)

	// Generator for current trades (at or above limit)
	currentTradesGen := gen.IntRange(0, 30)

	properties.Property("Execution blocked when daily trade limit reached", prop.ForAll(
		func(maxTrades, currentTrades int) bool {
			cfg := &config.AgentConfig{
				AutonomousMode:       "FULL_AUTO",
				AutoExecuteThreshold: 70,
				MaxDailyTrades:       maxTrades,
				MaxDailyLoss:         100000,
				CooldownMinutes:      0,
				ConsecutiveLossLimit: 10,
			}

			checker := NewExecutionChecker(cfg)

			decision := &models.Decision{
				ID:         "TEST-004",
				Symbol:     "HDFC",
				Action:     "BUY",
				Confidence: 90, // High confidence
				RiskCheck: &models.RiskCheckResult{
					Approved: true,
				},
				Consensus: &models.ConsensusDetails{
					TotalAgents:    4,
					AgreeingAgents: 4,
				},
			}

			state := ExecutionState{
				DailyTrades: currentTrades,
			}

			result := checker.CheckExecution(decision, state)

			// If current trades >= max trades, should NOT execute
			if currentTrades >= maxTrades {
				if result.ShouldExecute {
					t.Logf("FAILED: Should not execute when daily trades (%d) >= limit (%d)",
						currentTrades, maxTrades)
					return false
				}
			}

			return true
		},
		maxTradesGen,
		currentTradesGen,
	))

	properties.TestingRun(t)
}

// TestProperty_ExecutionBlockedByCooldown tests that cooldown period blocks execution.
func TestProperty_ExecutionBlockedByCooldown(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	// Generator for cooldown minutes
	cooldownGen := gen.IntRange(1, 30)

	// Generator for minutes since last trade
	minutesSinceGen := gen.IntRange(0, 60)

	properties.Property("Execution blocked during cooldown period", prop.ForAll(
		func(cooldownMinutes, minutesSince int) bool {
			cfg := &config.AgentConfig{
				AutonomousMode:       "FULL_AUTO",
				AutoExecuteThreshold: 70,
				MaxDailyTrades:       100,
				MaxDailyLoss:         100000,
				CooldownMinutes:      cooldownMinutes,
				ConsecutiveLossLimit: 10,
			}

			checker := NewExecutionChecker(cfg)

			decision := &models.Decision{
				ID:         "TEST-005",
				Symbol:     "SBIN",
				Action:     "SELL",
				Confidence: 90,
				RiskCheck: &models.RiskCheckResult{
					Approved: true,
				},
				Consensus: &models.ConsensusDetails{
					TotalAgents:    4,
					AgreeingAgents: 4,
				},
			}

			lastTradeAt := time.Now().Add(-time.Duration(minutesSince) * time.Minute)
			state := ExecutionState{
				LastTradeAt: lastTradeAt,
			}

			result := checker.CheckExecution(decision, state)

			// If minutes since last trade < cooldown, should NOT execute
			if minutesSince < cooldownMinutes {
				if result.ShouldExecute {
					t.Logf("FAILED: Should not execute during cooldown (elapsed=%d min, cooldown=%d min)",
						minutesSince, cooldownMinutes)
					return false
				}
			}

			return true
		},
		cooldownGen,
		minutesSinceGen,
	))

	properties.TestingRun(t)
}

// TestProperty_OperatingModeBlocksExecution tests that MANUAL and NOTIFY_ONLY modes block execution.
func TestProperty_OperatingModeBlocksExecution(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	// Generator for non-auto modes
	modeGen := gen.OneConstOf("MANUAL", "NOTIFY_ONLY")

	// Generator for confidence
	confidenceGen := gen.Float64Range(80, 100)

	properties.Property("MANUAL and NOTIFY_ONLY modes block execution", prop.ForAll(
		func(mode string, confidence float64) bool {
			cfg := &config.AgentConfig{
				AutonomousMode:       mode,
				AutoExecuteThreshold: 70,
				MaxDailyTrades:       100,
				MaxDailyLoss:         100000,
				CooldownMinutes:      0,
				ConsecutiveLossLimit: 10,
			}

			checker := NewExecutionChecker(cfg)

			decision := &models.Decision{
				ID:         "TEST-006",
				Symbol:     "ICICI",
				Action:     "BUY",
				Confidence: confidence,
				RiskCheck: &models.RiskCheckResult{
					Approved: true,
				},
				Consensus: &models.ConsensusDetails{
					TotalAgents:    4,
					AgreeingAgents: 4,
				},
			}

			state := ExecutionState{}

			result := checker.CheckExecution(decision, state)

			// Should NOT execute in MANUAL or NOTIFY_ONLY mode
			if result.ShouldExecute {
				t.Logf("FAILED: Should not execute in %s mode", mode)
				return false
			}

			// Verify block reason mentions the mode
			if !containsAny(result.BlockReason, mode, "mode") {
				t.Logf("FAILED: Block reason should mention mode: %s", result.BlockReason)
				return false
			}

			return true
		},
		modeGen,
		confidenceGen,
	))

	properties.TestingRun(t)
}

// TestProperty_HoldActionBlocksExecution tests that HOLD decisions are not executed.
func TestProperty_HoldActionBlocksExecution(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	// Generator for confidence
	confidenceGen := gen.Float64Range(80, 100)

	properties.Property("HOLD action blocks execution", prop.ForAll(
		func(confidence float64) bool {
			cfg := &config.AgentConfig{
				AutonomousMode:       "FULL_AUTO",
				AutoExecuteThreshold: 70,
				MaxDailyTrades:       100,
				MaxDailyLoss:         100000,
				CooldownMinutes:      0,
				ConsecutiveLossLimit: 10,
			}

			checker := NewExecutionChecker(cfg)

			decision := &models.Decision{
				ID:         "TEST-007",
				Symbol:     "WIPRO",
				Action:     "HOLD", // HOLD action
				Confidence: confidence,
				RiskCheck: &models.RiskCheckResult{
					Approved: true,
				},
				Consensus: &models.ConsensusDetails{
					TotalAgents:    4,
					AgreeingAgents: 4,
				},
			}

			state := ExecutionState{}

			result := checker.CheckExecution(decision, state)

			// Should NOT execute HOLD decisions
			if result.ShouldExecute {
				t.Logf("FAILED: Should not execute HOLD decision")
				return false
			}

			return true
		},
		confidenceGen,
	))

	properties.TestingRun(t)
}

// Helper function to check if string contains any of the substrings
func containsAny(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if len(substr) > 0 && len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}
