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

	// Adjust based on patterns (with reliability weighting)
	patternScore := a.analyzePatterns(req.Patterns)

	// Adjust based on levels
	levelScore := a.analyzeLevels(req)

	// Market regime adjustment: trend-following signals are stronger in trending
	// regimes, mean-reversion signals are stronger in ranging regimes
	regimeFactor := 1.0
	if req.MarketState != nil {
		switch req.MarketState.MarketRegime {
		case RegimeTrendingUp:
			if baseScore > 0 {
				regimeFactor = 1.2 // boost bullish signals in uptrend
			} else if baseScore < 0 {
				regimeFactor = 0.7 // dampen bearish signals in uptrend
			}
		case RegimeTrendingDown:
			if baseScore < 0 {
				regimeFactor = 1.2
			} else if baseScore > 0 {
				regimeFactor = 0.7
			}
		case RegimeHighVolatility:
			regimeFactor = 0.6 // reduce all signals in high vol
		}
	}

	// Combine scores with regime adjustment
	totalScore := (baseScore*0.5 + patternScore*0.3 + levelScore*0.2) * regimeFactor

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

	// Calculate confidence: higher absolute score = higher confidence
	// but cap it — rule-based analysis shouldn't claim >85% confidence
	rawConf := 50 + abs(totalScore)/2
	if rawConf > 85 {
		rawConf = 85
	}
	result.Confidence = ClampConfidence(rawConf)

	// Add pattern analysis to reasoning
	if len(req.Patterns) > 0 {
		reasoning.WriteString(fmt.Sprintf("Patterns: %s. ", a.summarizePatterns(req.Patterns)))
	}

	// Add signal score to reasoning
	if req.SignalScore != nil {
		reasoning.WriteString(fmt.Sprintf("Signal score: %.1f (%s). ", req.SignalScore.Score, req.SignalScore.Recommendation))
	}

	// Add regime context
	if req.MarketState != nil {
		reasoning.WriteString(fmt.Sprintf("Market regime: %s. ", req.MarketState.MarketRegime))
	}

	result.Reasoning = reasoning.String()

	// Calculate entry, SL, targets for BUY/SELL
	if result.Recommendation != Hold && req.CurrentPrice > 0 {
		a.calculateTradeLevels(result, req)
	}

	result.Timestamp = time.Now()
	return result, nil
}

// analyzePatterns scores the detected patterns, weighted by reliability.
// Reversal patterns near S/R levels are stronger. Continuation patterns
// in a trending regime are stronger. Conflicting patterns cancel out.
func (a *TechnicalAgent) analyzePatterns(patterns []analysis.Pattern) float64 {
	if len(patterns) == 0 {
		return 0
	}

	var bullishScore, bearishScore float64
	var bullishCount, bearishCount int

	for _, p := range patterns {
		// Base score from pattern strength (0-1)
		weight := p.Strength

		// Boost completed patterns, penalize incomplete ones
		if p.Completion > 0 {
			weight *= p.Completion
		}

		// Volume-confirmed patterns are more reliable
		if p.VolumeConfirm {
			weight *= 1.3
		}

		switch p.Direction {
		case analysis.PatternBullish:
			bullishScore += weight * 100
			bullishCount++
		case analysis.PatternBearish:
			bearishScore += weight * 100
			bearishCount++
		}
	}

	// If both bullish and bearish patterns are present, they partially cancel
	// but the stronger side still wins with reduced confidence
	if bullishCount > 0 && bearishCount > 0 {
		// Conflicting patterns: reduce the net score
		net := bullishScore - bearishScore
		// Apply a conflict penalty proportional to the weaker side
		weaker := bullishScore
		if bearishScore < weaker {
			weaker = bearishScore
		}
		conflictPenalty := weaker * 0.5 // 50% of the weaker signal cancels out
		if net > 0 {
			net -= conflictPenalty
		} else {
			net += conflictPenalty
		}
		return clampScore(net/float64(len(patterns)), -100, 100)
	}

	// No conflict: normalize by count
	score := (bullishScore - bearishScore) / float64(len(patterns))
	return clampScore(score, -100, 100)
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
// Uses ATR-based dynamic stops when indicator data is available, falling back
// to support/resistance levels, then to fixed percentages.
func (a *TechnicalAgent) calculateTradeLevels(result *AnalysisResult, req AnalysisRequest) {
	price := req.CurrentPrice

	// Try to get ATR for dynamic stop-loss sizing
	atrPercent := 0.0
	if atrVals, ok := req.Indicators["ATR_14"]; ok && len(atrVals) > 0 {
		lastATR := atrVals[len(atrVals)-1]
		if lastATR > 0 && price > 0 {
			atrPercent = lastATR / price
		}
	}

	if result.Recommendation == Buy {
		result.EntryPrice = price

		// Stop-loss: prefer ATR-based, then S/R-based, then fixed %
		if atrPercent > 0 {
			// 2x ATR below entry — adapts to current volatility
			result.StopLoss = price * (1 - 2*atrPercent)
		} else if req.Levels != nil && req.Levels.NearestSupport > 0 {
			result.StopLoss = req.Levels.NearestSupport * 0.99
		} else {
			result.StopLoss = price * 0.98
		}

		// Targets: prefer R:R multiples of risk, anchored to resistance if available
		risk := price - result.StopLoss
		if risk <= 0 {
			risk = price * 0.02
		}

		if req.Levels != nil && req.Levels.NearestResistance > 0 {
			t1 := req.Levels.NearestResistance
			// Ensure T1 gives at least 1:1 R:R
			if t1-price < risk {
				t1 = price + risk
			}
			result.Targets = []float64{
				t1,
				price + risk*2.5,
				price + risk*4,
			}
		} else {
			result.Targets = []float64{
				price + risk*1.5,
				price + risk*2.5,
				price + risk*4,
			}
		}
	} else if result.Recommendation == Sell {
		result.EntryPrice = price

		if atrPercent > 0 {
			result.StopLoss = price * (1 + 2*atrPercent)
		} else if req.Levels != nil && req.Levels.NearestResistance > 0 {
			result.StopLoss = req.Levels.NearestResistance * 1.01
		} else {
			result.StopLoss = price * 1.02
		}

		risk := result.StopLoss - price
		if risk <= 0 {
			risk = price * 0.02
		}

		if req.Levels != nil && req.Levels.NearestSupport > 0 {
			t1 := req.Levels.NearestSupport
			if price-t1 < risk {
				t1 = price - risk
			}
			result.Targets = []float64{
				t1,
				price - risk*2.5,
				price - risk*4,
			}
		} else {
			result.Targets = []float64{
				price - risk*1.5,
				price - risk*2.5,
				price - risk*4,
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
