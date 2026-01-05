// Package agents provides AI agent implementations for trading decisions.
package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"

	"zerodha-trader/internal/analysis"
)

// TechnicalAgent analyzes technical indicators and patterns to provide trading recommendations.
// Requirements: 11.3
type TechnicalAgent struct {
	BaseAgent
	llmClient LLMClient
}

// LLMClient defines the interface for LLM interactions.
type LLMClient interface {
	// Complete sends a prompt to the LLM and returns the response.
	Complete(ctx context.Context, prompt string) (string, error)
	// CompleteWithSystem sends a prompt with a system message.
	CompleteWithSystem(ctx context.Context, system, prompt string) (string, error)
	// CompleteWithTools sends a prompt with tools and handles tool calls.
	CompleteWithTools(ctx context.Context, systemPrompt, userPrompt string, tools []openai.Tool, executor ToolExecutorInterface) (string, error)
	// CompleteWithToolsVerbose sends a prompt with tools and returns the full chain of thought.
	CompleteWithToolsVerbose(ctx context.Context, systemPrompt, userPrompt string, tools []openai.Tool, executor ToolExecutorInterface) (*ChainOfThought, error)
}

// NewTechnicalAgent creates a new technical analysis agent.
func NewTechnicalAgent(llmClient LLMClient, weight float64) *TechnicalAgent {
	return &TechnicalAgent{
		BaseAgent: NewBaseAgent("technical", weight),
		llmClient: llmClient,
	}
}

// Analyze performs technical analysis on the given request data.
func (a *TechnicalAgent) Analyze(ctx context.Context, req AnalysisRequest) (*AnalysisResult, error) {
	// Build analysis context
	analysisContext := a.buildAnalysisContext(req)

	// If no LLM client, perform rule-based analysis
	if a.llmClient == nil {
		return a.ruleBasedAnalysis(req)
	}

	// Use LLM for interpretation
	systemPrompt := `You are a technical analysis expert for Indian stock markets. 
Analyze the provided technical data and provide a trading recommendation.
Your response must be in the following exact format:
RECOMMENDATION: BUY|SELL|HOLD
CONFIDENCE: <number 0-100>
ENTRY: <price or N/A>
STOPLOSS: <price or N/A>
TARGET1: <price or N/A>
TARGET2: <price or N/A>
TARGET3: <price or N/A>
REASONING: <your analysis in one paragraph>`

	response, err := a.llmClient.CompleteWithSystem(ctx, systemPrompt, analysisContext)
	if err != nil {
		// Fallback to rule-based analysis
		return a.ruleBasedAnalysis(req)
	}

	return a.parseResponse(response, req)
}

// buildAnalysisContext creates a structured context string for LLM analysis.
func (a *TechnicalAgent) buildAnalysisContext(req AnalysisRequest) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Symbol: %s\n", req.Symbol))
	sb.WriteString(fmt.Sprintf("Current Price: %.2f\n\n", req.CurrentPrice))

	// Signal Score
	if req.SignalScore != nil {
		sb.WriteString(fmt.Sprintf("Signal Score: %.2f (%s)\n", req.SignalScore.Score, req.SignalScore.Recommendation))
		sb.WriteString("Component Scores:\n")
		for name, score := range req.SignalScore.Components {
			sb.WriteString(fmt.Sprintf("  - %s: %.2f\n", name, score))
		}
		sb.WriteString(fmt.Sprintf("Volume Confirmation: %v\n\n", req.SignalScore.VolumeConfirm))
	}

	// Key Indicators
	sb.WriteString("Key Indicators:\n")
	for name, values := range req.Indicators {
		if len(values) > 0 {
			sb.WriteString(fmt.Sprintf("  - %s: %.2f\n", name, values[len(values)-1]))
		}
	}
	sb.WriteString("\n")

	// Support/Resistance Levels
	if req.Levels != nil {
		sb.WriteString("Support/Resistance Levels:\n")
		sb.WriteString(fmt.Sprintf("  - Nearest Support: %.2f (Strength: %d)\n", req.Levels.NearestSupport, req.Levels.SupportStrength))
		sb.WriteString(fmt.Sprintf("  - Nearest Resistance: %.2f (Strength: %d)\n", req.Levels.NearestResistance, req.Levels.ResistanceStrength))
		sb.WriteString("\n")
	}

	// Patterns
	if len(req.Patterns) > 0 {
		sb.WriteString("Detected Patterns:\n")
		for _, p := range req.Patterns {
			sb.WriteString(fmt.Sprintf("  - %s (%s, Strength: %.2f)\n", p.Name, p.Direction, p.Strength))
		}
		sb.WriteString("\n")
	}

	// Market State
	if req.MarketState != nil {
		sb.WriteString("Market Context:\n")
		sb.WriteString(fmt.Sprintf("  - Nifty: %.2f (%.2f%%)\n", req.MarketState.NiftyLevel, req.MarketState.NiftyChange))
		sb.WriteString(fmt.Sprintf("  - VIX: %.2f\n", req.MarketState.VIXLevel))
		sb.WriteString(fmt.Sprintf("  - Market Trend: %s\n", req.MarketState.MarketTrend))
		sb.WriteString(fmt.Sprintf("  - Market Regime: %s\n", req.MarketState.MarketRegime))
	}

	return sb.String()
}

// ruleBasedAnalysis performs analysis without LLM using predefined rules.
func (a *TechnicalAgent) ruleBasedAnalysis(req AnalysisRequest) (*AnalysisResult, error) {
	result := a.CreateResult(Hold, 50, "")

	// Start with signal score if available
	var baseScore float64
	if req.SignalScore != nil {
		baseScore = req.SignalScore.Score
	}

	// Adjust based on patterns
	patternScore := a.analyzePatterns(req.Patterns)
	
	// Adjust based on levels
	levelScore := a.analyzeLevels(req)

	// Combine scores
	totalScore := baseScore*0.5 + patternScore*0.3 + levelScore*0.2

	// Determine recommendation
	var reasoning strings.Builder
	if totalScore >= 40 {
		result.Recommendation = Buy
		reasoning.WriteString("Bullish signals detected. ")
	} else if totalScore <= -40 {
		result.Recommendation = Sell
		reasoning.WriteString("Bearish signals detected. ")
	} else {
		result.Recommendation = Hold
		reasoning.WriteString("Mixed signals, no clear direction. ")
	}

	// Calculate confidence based on signal strength
	result.Confidence = ClampConfidence(50 + abs(totalScore)/2)

	// Add pattern analysis to reasoning
	if len(req.Patterns) > 0 {
		reasoning.WriteString(fmt.Sprintf("Patterns: %s. ", a.summarizePatterns(req.Patterns)))
	}

	// Add signal score to reasoning
	if req.SignalScore != nil {
		reasoning.WriteString(fmt.Sprintf("Signal score: %.1f (%s). ", req.SignalScore.Score, req.SignalScore.Recommendation))
	}

	result.Reasoning = reasoning.String()

	// Calculate entry, SL, targets for BUY/SELL
	if result.Recommendation != Hold && req.CurrentPrice > 0 {
		a.calculateTradeLevels(result, req)
	}

	result.Timestamp = time.Now()
	return result, nil
}

// analyzePatterns scores the detected patterns.
func (a *TechnicalAgent) analyzePatterns(patterns []analysis.Pattern) float64 {
	if len(patterns) == 0 {
		return 0
	}

	var score float64
	for _, p := range patterns {
		patternScore := p.Strength * 100
		switch p.Direction {
		case analysis.PatternBullish:
			score += patternScore
		case analysis.PatternBearish:
			score -= patternScore
		}
	}

	// Normalize to -100 to 100
	if len(patterns) > 0 {
		score /= float64(len(patterns))
	}

	return score
}

// analyzeLevels scores based on support/resistance proximity.
func (a *TechnicalAgent) analyzeLevels(req AnalysisRequest) float64 {
	if req.Levels == nil || req.CurrentPrice <= 0 {
		return 0
	}

	var score float64

	// Near support = bullish, near resistance = bearish
	if req.Levels.NearestSupport > 0 {
		distToSupport := (req.CurrentPrice - req.Levels.NearestSupport) / req.CurrentPrice
		if distToSupport < 0.02 { // Within 2% of support
			score += 30 * float64(req.Levels.SupportStrength)
		}
	}

	if req.Levels.NearestResistance > 0 {
		distToResistance := (req.Levels.NearestResistance - req.CurrentPrice) / req.CurrentPrice
		if distToResistance < 0.02 { // Within 2% of resistance
			score -= 30 * float64(req.Levels.ResistanceStrength)
		}
	}

	return clampScore(score, -100, 100)
}

// summarizePatterns creates a brief summary of detected patterns.
func (a *TechnicalAgent) summarizePatterns(patterns []analysis.Pattern) string {
	if len(patterns) == 0 {
		return "none"
	}

	var names []string
	for _, p := range patterns {
		names = append(names, p.Name)
	}

	if len(names) > 3 {
		return fmt.Sprintf("%s and %d more", strings.Join(names[:3], ", "), len(names)-3)
	}
	return strings.Join(names, ", ")
}

// calculateTradeLevels sets entry, stop-loss, and targets based on analysis.
func (a *TechnicalAgent) calculateTradeLevels(result *AnalysisResult, req AnalysisRequest) {
	price := req.CurrentPrice

	if result.Recommendation == Buy {
		result.EntryPrice = price
		
		// Stop-loss below nearest support or 2% below entry
		if req.Levels != nil && req.Levels.NearestSupport > 0 {
			result.StopLoss = req.Levels.NearestSupport * 0.99 // 1% below support
		} else {
			result.StopLoss = price * 0.98 // 2% below entry
		}

		// Targets based on resistance or percentage
		if req.Levels != nil && req.Levels.NearestResistance > 0 {
			result.Targets = []float64{
				req.Levels.NearestResistance,
				req.Levels.NearestResistance * 1.02,
				req.Levels.NearestResistance * 1.05,
			}
		} else {
			result.Targets = []float64{
				price * 1.02, // 2%
				price * 1.04, // 4%
				price * 1.06, // 6%
			}
		}
	} else if result.Recommendation == Sell {
		result.EntryPrice = price

		// Stop-loss above nearest resistance or 2% above entry
		if req.Levels != nil && req.Levels.NearestResistance > 0 {
			result.StopLoss = req.Levels.NearestResistance * 1.01 // 1% above resistance
		} else {
			result.StopLoss = price * 1.02 // 2% above entry
		}

		// Targets based on support or percentage
		if req.Levels != nil && req.Levels.NearestSupport > 0 {
			result.Targets = []float64{
				req.Levels.NearestSupport,
				req.Levels.NearestSupport * 0.98,
				req.Levels.NearestSupport * 0.95,
			}
		} else {
			result.Targets = []float64{
				price * 0.98, // 2%
				price * 0.96, // 4%
				price * 0.94, // 6%
			}
		}
	}

	// Calculate risk-reward ratio
	result.RiskReward = CalculateRiskReward(
		result.EntryPrice,
		result.StopLoss,
		result.Targets,
		result.Recommendation == Buy,
	)
}

// parseResponse parses the LLM response into an AnalysisResult.
func (a *TechnicalAgent) parseResponse(response string, req AnalysisRequest) (*AnalysisResult, error) {
	result := a.CreateResult(Hold, 50, "")
	
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "RECOMMENDATION:") {
			rec := strings.TrimSpace(strings.TrimPrefix(line, "RECOMMENDATION:"))
			switch strings.ToUpper(rec) {
			case "BUY":
				result.Recommendation = Buy
			case "SELL":
				result.Recommendation = Sell
			default:
				result.Recommendation = Hold
			}
		} else if strings.HasPrefix(line, "CONFIDENCE:") {
			fmt.Sscanf(strings.TrimPrefix(line, "CONFIDENCE:"), "%f", &result.Confidence)
			result.Confidence = ClampConfidence(result.Confidence)
		} else if strings.HasPrefix(line, "ENTRY:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "ENTRY:"))
			if val != "N/A" {
				fmt.Sscanf(val, "%f", &result.EntryPrice)
			}
		} else if strings.HasPrefix(line, "STOPLOSS:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "STOPLOSS:"))
			if val != "N/A" {
				fmt.Sscanf(val, "%f", &result.StopLoss)
			}
		} else if strings.HasPrefix(line, "TARGET1:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "TARGET1:"))
			if val != "N/A" {
				var t float64
				fmt.Sscanf(val, "%f", &t)
				if t > 0 {
					result.Targets = append(result.Targets, t)
				}
			}
		} else if strings.HasPrefix(line, "TARGET2:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "TARGET2:"))
			if val != "N/A" {
				var t float64
				fmt.Sscanf(val, "%f", &t)
				if t > 0 {
					result.Targets = append(result.Targets, t)
				}
			}
		} else if strings.HasPrefix(line, "TARGET3:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "TARGET3:"))
			if val != "N/A" {
				var t float64
				fmt.Sscanf(val, "%f", &t)
				if t > 0 {
					result.Targets = append(result.Targets, t)
				}
			}
		} else if strings.HasPrefix(line, "REASONING:") {
			result.Reasoning = strings.TrimSpace(strings.TrimPrefix(line, "REASONING:"))
		}
	}

	// Calculate risk-reward if we have entry, SL, and targets
	if result.EntryPrice > 0 && result.StopLoss > 0 && len(result.Targets) > 0 {
		result.RiskReward = CalculateRiskReward(
			result.EntryPrice,
			result.StopLoss,
			result.Targets,
			result.Recommendation == Buy,
		)
	}

	// Fallback to rule-based if parsing failed
	if result.Reasoning == "" {
		return a.ruleBasedAnalysis(req)
	}

	result.Timestamp = time.Now()
	return result, nil
}

// Helper functions
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func clampScore(value, minVal, maxVal float64) float64 {
	if value < minVal {
		return minVal
	}
	if value > maxVal {
		return maxVal
	}
	return value
}
