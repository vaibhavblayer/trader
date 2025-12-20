package agents

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"zerodha-trader/internal/analysis"
)

// Feature: zerodha-go-trader, Property 6: Agent output contains all required fields
// Validates: Requirements 11.6
//
// Property: For any valid AnalysisResult, all required fields must be present and valid.
// Required fields: AgentName, Recommendation, Confidence (0-100), Reasoning, Timestamp.
// For BUY/SELL recommendations: EntryPrice, StopLoss, Targets, RiskReward are also required.
func TestProperty_AgentOutputContainsAllRequiredFields(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	// Generator for agent names
	agentNames := []string{"technical", "research", "news", "risk", "trader"}

	// Generator for recommendations
	recommendations := []Recommendation{Buy, Sell, Hold}

	// Generator for valid AnalysisResult
	resultGen := gen.Struct(reflect.TypeOf(AnalysisResult{}), map[string]gopter.Gen{
		"AgentName":      gen.OneConstOf(agentNames[0], agentNames[1], agentNames[2], agentNames[3], agentNames[4]),
		"Recommendation": gen.OneConstOf(recommendations[0], recommendations[1], recommendations[2]),
		"Confidence":     gen.Float64Range(0, 100),
		"Reasoning":      gen.AnyString().SuchThat(func(s string) bool { return len(s) > 0 }),
		"EntryPrice":     gen.Float64Range(1, 10000),
		"StopLoss":       gen.Float64Range(1, 10000),
		"Targets":        gen.SliceOfN(3, gen.Float64Range(1, 10000)),
		"RiskReward":     gen.Float64Range(0.1, 10),
		"Timestamp":      gen.Const(time.Now()),
	})

	properties.Property("Agent output contains all required fields", prop.ForAll(
		func(result AnalysisResult) bool {
			// Check required fields for all results
			if result.AgentName == "" {
				return false
			}

			if result.Recommendation == "" {
				return false
			}

			// Confidence must be in [0, 100]
			if result.Confidence < 0 || result.Confidence > 100 {
				return false
			}

			if result.Reasoning == "" {
				return false
			}

			if result.Timestamp.IsZero() {
				return false
			}

			// For BUY/SELL recommendations, additional fields are required
			if result.Recommendation == Buy || result.Recommendation == Sell {
				if result.EntryPrice <= 0 {
					return false
				}
				if result.StopLoss <= 0 {
					return false
				}
				if len(result.Targets) == 0 {
					return false
				}
				if result.RiskReward <= 0 {
					return false
				}
			}

			// Validate using the Validate method
			err := result.Validate()
			return err == nil
		},
		resultGen,
	))

	properties.TestingRun(t)
}

// Feature: zerodha-go-trader, Property 7: Confidence aggregation produces weighted average in [0, 100]
// Validates: Requirements 11.6, 13.3
//
// Property: For any set of agent results with valid confidences (0-100) and weights (0-1),
// the consensus calculation should produce a weighted average confidence that is also in [0, 100].
func TestProperty_ConfidenceAggregationProducesWeightedAverageInRange(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	// Generator for agent weights
	weightsGen := gen.MapOf(
		gen.OneConstOf("technical", "research", "news", "risk"),
		gen.Float64Range(0.1, 1.0),
	).SuchThat(func(m map[string]float64) bool {
		return len(m) > 0
	})

	// Generator for agent results - used for documentation
	_ = func(name string) gopter.Gen {
		return gen.Struct(reflect.TypeOf(AnalysisResult{}), map[string]gopter.Gen{
			"AgentName":      gen.Const(name),
			"Recommendation": gen.OneConstOf(Buy, Sell, Hold),
			"Confidence":     gen.Float64Range(0, 100),
			"Reasoning":      gen.Const("Test reasoning"),
			"EntryPrice":     gen.Float64Range(100, 1000),
			"StopLoss":       gen.Float64Range(90, 900),
			"Targets":        gen.SliceOfN(1, gen.Float64Range(110, 1100)),
			"RiskReward":     gen.Float64Range(1, 5),
			"Timestamp":      gen.Const(time.Now()),
		})
	}

	properties.Property("Confidence aggregation produces weighted average in [0, 100]", prop.ForAll(
		func(weights map[string]float64) bool {
			// Create trader agent with the weights
			traderAgent := NewTraderAgent(nil, weights, 1.0)

			// Generate results for each agent in weights
			results := make(map[string]*AnalysisResult)
			for name := range weights {
				// Generate a result for this agent
				result := &AnalysisResult{
					AgentName:      name,
					Recommendation: []Recommendation{Buy, Sell, Hold}[time.Now().UnixNano()%3],
					Confidence:     float64(time.Now().UnixNano()%101), // 0-100
					Reasoning:      "Test reasoning",
					EntryPrice:     100.0,
					StopLoss:       95.0,
					Targets:        []float64{110.0},
					RiskReward:     2.0,
					Timestamp:      time.Now(),
				}
				results[name] = result
			}

			// Calculate consensus
			consensus := traderAgent.calculateConsensus(results)

			// Verify weighted score is in [0, 100]
			if consensus.WeightedScore < 0 || consensus.WeightedScore > 100 {
				return false
			}

			// Verify total agents count
			if consensus.TotalAgents != len(results) {
				return false
			}

			// Verify agreeing agents is <= total agents
			if consensus.AgreeingAgents > consensus.TotalAgents {
				return false
			}

			return true
		},
		weightsGen,
	))

	// Additional property: Test with specific agent results
	properties.Property("Consensus calculation with multiple agents produces valid confidence", prop.ForAll(
		func(conf1, conf2, conf3, conf4 float64) bool {
			// Create trader agent with default weights
			weights := map[string]float64{
				"technical": 0.35,
				"research":  0.25,
				"news":      0.15,
				"risk":      0.25,
			}
			traderAgent := NewTraderAgent(nil, weights, 1.0)

			// Create results with varying confidences
			results := map[string]*AnalysisResult{
				"technical": {
					AgentName:      "technical",
					Recommendation: Buy,
					Confidence:     conf1,
					Reasoning:      "Technical analysis",
					Timestamp:      time.Now(),
				},
				"research": {
					AgentName:      "research",
					Recommendation: Buy,
					Confidence:     conf2,
					Reasoning:      "Research analysis",
					Timestamp:      time.Now(),
				},
				"news": {
					AgentName:      "news",
					Recommendation: Hold,
					Confidence:     conf3,
					Reasoning:      "News analysis",
					Timestamp:      time.Now(),
				},
				"risk": {
					AgentName:      "risk",
					Recommendation: Hold,
					Confidence:     conf4,
					Reasoning:      "Risk analysis",
					Timestamp:      time.Now(),
				},
			}

			// Calculate consensus
			consensus := traderAgent.calculateConsensus(results)

			// Verify weighted score is in [0, 100]
			if consensus.WeightedScore < 0 || consensus.WeightedScore > 100 {
				return false
			}

			return true
		},
		gen.Float64Range(0, 100),
		gen.Float64Range(0, 100),
		gen.Float64Range(0, 100),
		gen.Float64Range(0, 100),
	))

	properties.TestingRun(t)
}

// TestTechnicalAgentRuleBasedAnalysis tests that the technical agent produces valid results.
func TestTechnicalAgentRuleBasedAnalysis(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	// Generator for current price
	priceGen := gen.Float64Range(100, 5000)

	// Generator for signal score
	signalScoreGen := gen.Float64Range(-100, 100)

	properties.Property("Technical agent produces valid results for any price and signal score", prop.ForAll(
		func(price, signalScore float64) bool {
			agent := NewTechnicalAgent(nil, 0.35)

			req := AnalysisRequest{
				Symbol:       "TEST",
				CurrentPrice: price,
				SignalScore: &analysis.SignalScore{
					Score:          signalScore,
					Recommendation: analysis.SignalRecommendation(scoreToRecommendation(signalScore)),
				},
			}

			result, err := agent.Analyze(context.Background(), req)
			if err != nil {
				return false
			}

			// Verify result has required fields
			if result.AgentName != "technical" {
				return false
			}

			if result.Confidence < 0 || result.Confidence > 100 {
				return false
			}

			if result.Timestamp.IsZero() {
				return false
			}

			return true
		},
		priceGen,
		signalScoreGen,
	))

	properties.TestingRun(t)
}

// TestRiskAgentPositionSizing tests that risk agent calculates valid position sizes.
func TestRiskAgentPositionSizing(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	// Generator for portfolio value
	portfolioValueGen := gen.Float64Range(100000, 10000000)

	// Generator for current price
	priceGen := gen.Float64Range(100, 5000)

	properties.Property("Risk agent calculates non-negative position size", prop.ForAll(
		func(portfolioValue, price float64) bool {
			agent := NewRiskAgent(nil, 0.25)

			req := AnalysisRequest{
				Symbol:       "TEST",
				CurrentPrice: price,
				Portfolio: &PortfolioState{
					TotalValue:    portfolioValue,
					AvailableCash: portfolioValue * 0.5,
				},
			}

			positionSize := agent.CalculatePositionSize(req)

			// Position size should be non-negative
			if positionSize < 0 {
				return false
			}

			// Position size should not exceed max position percent of portfolio
			maxPositionValue := portfolioValue * 0.05 // 5% default
			if positionSize*price > maxPositionValue*1.01 { // Allow 1% tolerance
				return false
			}

			return true
		},
		portfolioValueGen,
		priceGen,
	))

	properties.TestingRun(t)
}

// Helper function for testing
func scoreToRecommendation(score float64) string {
	switch {
	case score >= 70:
		return "STRONG_BUY"
	case score >= 40:
		return "BUY"
	case score >= 15:
		return "WEAK_BUY"
	case score >= -15:
		return "NEUTRAL"
	case score >= -40:
		return "WEAK_SELL"
	case score >= -70:
		return "SELL"
	default:
		return "STRONG_SELL"
	}
}
