// Package agents provides AI agent implementations for trading decisions.
package agents

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"zerodha-trader/internal/models"
)

// TraderAgent synthesizes inputs from all agents and makes final trading decisions.
// Requirements: 11.6, 13.3
type TraderAgent struct {
	BaseAgent
	llmClient    LLMClient
	agentWeights map[string]float64
}

// NewTraderAgent creates a new trader agent.
func NewTraderAgent(llmClient LLMClient, agentWeights map[string]float64, weight float64) *TraderAgent {
	// Default weights if not provided
	if agentWeights == nil {
		agentWeights = map[string]float64{
			"technical": 0.35,
			"research":  0.25,
			"news":      0.15,
			"risk":      0.25,
		}
	}

	return &TraderAgent{
		BaseAgent:    NewBaseAgent("trader", weight),
		llmClient:    llmClient,
		agentWeights: agentWeights,
	}
}

// Analyze is not typically called directly for TraderAgent.
// Use MakeFinalDecision instead with results from other agents.
func (a *TraderAgent) Analyze(ctx context.Context, req AnalysisRequest) (*AnalysisResult, error) {
	// TraderAgent needs results from other agents
	// This method provides a basic analysis if called directly
	result := a.CreateResult(Hold, 50, "")
	result.Reasoning = "TraderAgent requires results from other agents. Use MakeFinalDecision method."
	result.Timestamp = time.Now()
	return result, nil
}

// MakeFinalDecision synthesizes all agent results and makes a final trading decision.
func (a *TraderAgent) MakeFinalDecision(ctx context.Context, req AnalysisRequest, agentResults map[string]*AnalysisResult) (*models.Decision, error) {
	decision := &models.Decision{
		ID:           generateDecisionID(),
		Timestamp:    time.Now(),
		Symbol:       req.Symbol,
		AgentResults: make(map[string]*models.AgentResult),
	}

	// Convert agent results to model format
	for name, result := range agentResults {
		decision.AgentResults[name] = &models.AgentResult{
			AgentName:      result.AgentName,
			Recommendation: string(result.Recommendation),
			Confidence:     result.Confidence,
			Reasoning:      result.Reasoning,
			EntryPrice:     result.EntryPrice,
			StopLoss:       result.StopLoss,
			Targets:        result.Targets,
			RiskReward:     result.RiskReward,
			Timestamp:      result.Timestamp,
		}
	}

	// Calculate consensus
	consensus := a.calculateConsensus(agentResults)
	decision.Consensus = consensus
	decision.Action = string(a.determineAction(consensus))
	decision.Confidence = consensus.WeightedScore

	// If we have an LLM, use it for final reasoning
	if a.llmClient != nil {
		reasoning, err := a.generateReasoning(ctx, req, agentResults, consensus)
		if err == nil {
			decision.Reasoning = reasoning
		}
	}

	// If no LLM reasoning, generate rule-based reasoning
	if decision.Reasoning == "" {
		decision.Reasoning = a.generateRuleBasedReasoning(agentResults, consensus)
	}

	return decision, nil
}

// ConsensusResult contains the consensus calculation details.
type ConsensusResult struct {
	Action         Recommendation
	Confidence     float64
	BuyScore       float64
	SellScore      float64
	HoldScore      float64
	AgreeingAgents int
	TotalAgents    int
}

// calculateConsensus calculates weighted consensus from agent results.
// Property 7: Confidence aggregation produces weighted average in [0, 100]
func (a *TraderAgent) calculateConsensus(results map[string]*AnalysisResult) *models.ConsensusDetails {
	var buyScore, sellScore, holdScore float64
	var totalWeight float64
	var agreeingAgents int

	// First pass: calculate weighted scores
	for name, result := range results {
		weight := a.getAgentWeight(name)
		totalWeight += weight

		// Weight the confidence by agent weight
		weightedConfidence := result.Confidence * weight

		switch result.Recommendation {
		case Buy:
			buyScore += weightedConfidence
		case Sell:
			sellScore += weightedConfidence
		case Hold:
			holdScore += weightedConfidence
		}
	}

	// Normalize scores
	if totalWeight > 0 {
		buyScore /= totalWeight
		sellScore /= totalWeight
		holdScore /= totalWeight
	}

	// Determine winning action
	var action Recommendation
	var confidence float64

	if buyScore > sellScore && buyScore > holdScore {
		action = Buy
		confidence = buyScore
	} else if sellScore > buyScore && sellScore > holdScore {
		action = Sell
		confidence = sellScore
	} else {
		action = Hold
		confidence = holdScore
	}

	// Count agreeing agents
	for _, result := range results {
		if result.Recommendation == action {
			agreeingAgents++
		}
	}

	// Ensure confidence is in valid range [0, 100]
	confidence = ClampConfidence(confidence)

	// Build calculation explanation
	calculation := fmt.Sprintf(
		"Buy: %.1f%%, Sell: %.1f%%, Hold: %.1f%% (weighted by agent importance)",
		buyScore, sellScore, holdScore,
	)

	return &models.ConsensusDetails{
		TotalAgents:    len(results),
		AgreeingAgents: agreeingAgents,
		WeightedScore:  confidence,
		Calculation:    calculation,
	}
}

// getAgentWeight returns the weight for an agent.
func (a *TraderAgent) getAgentWeight(agentName string) float64 {
	if weight, ok := a.agentWeights[agentName]; ok {
		return weight
	}
	return 0.1 // Default weight for unknown agents
}

// determineAction determines the final action from consensus.
func (a *TraderAgent) determineAction(consensus *models.ConsensusDetails) Recommendation {
	// Parse the calculation to determine action
	// The consensus already has the weighted score
	
	// If confidence is too low, default to Hold
	if consensus.WeightedScore < 40 {
		return Hold
	}

	// If less than half of agents agree, be cautious
	if consensus.AgreeingAgents < consensus.TotalAgents/2 {
		return Hold
	}

	// Parse from calculation string (format: "Buy: X%, Sell: Y%, Hold: Z%")
	var buyScore, sellScore, holdScore float64
	fmt.Sscanf(consensus.Calculation, "Buy: %f%%, Sell: %f%%, Hold: %f%%", &buyScore, &sellScore, &holdScore)

	if buyScore > sellScore && buyScore > holdScore {
		return Buy
	} else if sellScore > buyScore && sellScore > holdScore {
		return Sell
	}
	return Hold
}

// generateReasoning uses LLM to generate final reasoning.
func (a *TraderAgent) generateReasoning(ctx context.Context, req AnalysisRequest, results map[string]*AnalysisResult, consensus *models.ConsensusDetails) (string, error) {
	// Build context for LLM
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Symbol: %s\n", req.Symbol))
	sb.WriteString(fmt.Sprintf("Current Price: %.2f\n\n", req.CurrentPrice))

	sb.WriteString("Agent Recommendations:\n")
	for name, result := range results {
		sb.WriteString(fmt.Sprintf("- %s: %s (Confidence: %.1f%%)\n", name, result.Recommendation, result.Confidence))
		sb.WriteString(fmt.Sprintf("  Reasoning: %s\n", result.Reasoning))
	}

	sb.WriteString(fmt.Sprintf("\nConsensus: %s\n", consensus.Calculation))
	sb.WriteString(fmt.Sprintf("Agreeing Agents: %d/%d\n", consensus.AgreeingAgents, consensus.TotalAgents))

	systemPrompt := `You are a senior trader synthesizing recommendations from multiple analysis agents.
Provide a concise final trading decision with clear reasoning.
Your response should be a single paragraph explaining the decision.`

	response, err := a.llmClient.CompleteWithSystem(ctx, systemPrompt, sb.String())
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(response), nil
}

// generateRuleBasedReasoning generates reasoning without LLM.
func (a *TraderAgent) generateRuleBasedReasoning(results map[string]*AnalysisResult, consensus *models.ConsensusDetails) string {
	var reasons []string

	// Summarize each agent's view
	for name, result := range results {
		reasons = append(reasons, fmt.Sprintf("%s recommends %s (%.0f%% confidence)",
			name, result.Recommendation, result.Confidence))
	}

	// Add consensus summary
	action := a.determineAction(consensus)
	reasons = append(reasons, fmt.Sprintf("Final decision: %s with %.0f%% confidence (%d/%d agents agree)",
		action, consensus.WeightedScore, consensus.AgreeingAgents, consensus.TotalAgents))

	return strings.Join(reasons, ". ") + "."
}

// GetBestEntry returns the best entry price from agent results.
func (a *TraderAgent) GetBestEntry(results map[string]*AnalysisResult, action Recommendation) float64 {
	var entries []float64
	var weights []float64

	for name, result := range results {
		if result.Recommendation == action && result.EntryPrice > 0 {
			entries = append(entries, result.EntryPrice)
			weights = append(weights, a.getAgentWeight(name))
		}
	}

	if len(entries) == 0 {
		return 0
	}

	// Weighted average of entry prices
	var weightedSum, totalWeight float64
	for i, entry := range entries {
		weightedSum += entry * weights[i]
		totalWeight += weights[i]
	}

	return weightedSum / totalWeight
}

// GetBestStopLoss returns the best stop-loss from agent results.
func (a *TraderAgent) GetBestStopLoss(results map[string]*AnalysisResult, action Recommendation) float64 {
	var stopLosses []float64

	for _, result := range results {
		if result.Recommendation == action && result.StopLoss > 0 {
			stopLosses = append(stopLosses, result.StopLoss)
		}
	}

	if len(stopLosses) == 0 {
		return 0
	}

	// For BUY: use the lowest (most conservative) stop-loss
	// For SELL: use the highest (most conservative) stop-loss
	if action == Buy {
		minSL := stopLosses[0]
		for _, sl := range stopLosses[1:] {
			if sl < minSL {
				minSL = sl
			}
		}
		return minSL
	}

	maxSL := stopLosses[0]
	for _, sl := range stopLosses[1:] {
		if sl > maxSL {
			maxSL = sl
		}
	}
	return maxSL
}

// GetBestTargets returns consolidated targets from agent results.
func (a *TraderAgent) GetBestTargets(results map[string]*AnalysisResult, action Recommendation) []float64 {
	var allTargets []float64

	for _, result := range results {
		if result.Recommendation == action && len(result.Targets) > 0 {
			allTargets = append(allTargets, result.Targets...)
		}
	}

	if len(allTargets) == 0 {
		return nil
	}

	// Sort and deduplicate targets
	targets := uniqueSortedTargets(allTargets, action == Buy)

	// Return up to 3 targets
	if len(targets) > 3 {
		return targets[:3]
	}
	return targets
}

// IsUnanimous checks if all agents agree on the recommendation.
func (a *TraderAgent) IsUnanimous(results map[string]*AnalysisResult) bool {
	if len(results) == 0 {
		return false
	}

	var firstRec Recommendation
	first := true

	for _, result := range results {
		if first {
			firstRec = result.Recommendation
			first = false
			continue
		}
		if result.Recommendation != firstRec {
			return false
		}
	}

	return true
}

// GetAverageConfidence returns the weighted average confidence.
func (a *TraderAgent) GetAverageConfidence(results map[string]*AnalysisResult) float64 {
	var weightedSum, totalWeight float64

	for name, result := range results {
		weight := a.getAgentWeight(name)
		weightedSum += result.Confidence * weight
		totalWeight += weight
	}

	if totalWeight == 0 {
		return 0
	}

	return ClampConfidence(weightedSum / totalWeight)
}

// Helper functions

func generateDecisionID() string {
	return fmt.Sprintf("DEC-%d", time.Now().UnixNano())
}

func uniqueSortedTargets(targets []float64, ascending bool) []float64 {
	if len(targets) == 0 {
		return nil
	}

	// Remove duplicates (within 0.1% tolerance)
	unique := make([]float64, 0, len(targets))
	for _, t := range targets {
		isDuplicate := false
		for _, u := range unique {
			if math.Abs(t-u)/u < 0.001 {
				isDuplicate = true
				break
			}
		}
		if !isDuplicate {
			unique = append(unique, t)
		}
	}

	// Sort
	for i := 0; i < len(unique)-1; i++ {
		for j := i + 1; j < len(unique); j++ {
			if ascending {
				if unique[i] > unique[j] {
					unique[i], unique[j] = unique[j], unique[i]
				}
			} else {
				if unique[i] < unique[j] {
					unique[i], unique[j] = unique[j], unique[i]
				}
			}
		}
	}

	return unique
}
