// Package cli provides the command-line interface for the trading application.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"zerodha-trader/internal/agents"
	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

const paperToolSystemPrompt = `You are an expert NSE intraday trader. Analyze the stock and make a trading decision.

TOOLS TO USE:
- calculate_rsi: RSI value + direction (current vs previous)
- analyze_volume: Volume ratio vs 20-period average
- calculate_ema_crossover: EMA9/EMA21 trend direction
- calculate_vwap: Price deviation from VWAP
- calculate_adx: Trend strength

=== HARD GATES (ALL MUST PASS) ===

1. RSI REGIME LOCK (eliminates chop/transition zones):
   - BUY allowed ONLY if: RSI > 55 AND RSI rising (current > previous)
   - SELL allowed ONLY if: RSI < 45 AND RSI falling (current < previous)
   - RSI between 45-55 = CHOP ZONE = NO_TRADE always
   - RSI rising but below 55 = NO_TRADE (noise bounce, not trend)
   - RSI falling but above 45 = NO_TRADE (pullback, not reversal)

2. VOLUME EXPANSION GATE:
   - Volume ratio must be > 1.3x average for any trade
   - Low volume = low participation = unreliable signal

3. EMA ALIGNMENT (MANDATORY):
   - BUY: Price must be ABOVE EMA9, EMA9 > EMA21 (bullish structure)
   - SELL: Price must be BELOW EMA9, EMA9 < EMA21 (bearish structure)
   - If EMA disagrees with trade direction = NO_TRADE

4. VWAP EXHAUSTION BLOCK:
   - If price is >0.7% above VWAP = NO BUY (stretched, likely to revert)
   - If price is >0.7% below VWAP = NO SELL (stretched, likely to bounce)
   - Exhausted moves have poor risk/reward

5. TREND STRENGTH:
   - ADX must be > 25 for any trade (was 20, now stricter)
   - ADX < 25 = weak trend = NO_TRADE

=== CONFIDENCE CALCULATION (mechanized) ===
Base = 45, add points:
- RSI slope strong (>5 points move): +10
- Volume ratio >2x: +15, >1.5x: +10, >1.3x: +5
- ADX >35: +15, >30: +10, >25: +5
- EMA cleanly aligned: +10
- VWAP confirms direction (within 0.3%): +5

=== OUTPUT JSON ===
{
  "action": "BUY" or "SELL" or "NO_TRADE",
  "gates_passed": {
    "rsi_regime": true/false,
    "volume_expansion": true/false,
    "ema_alignment": true/false,
    "vwap_not_exhausted": true/false,
    "trend_strength": true/false
  },
  "signal_quality": {
    "rsi_value": 58,
    "rsi_direction": "rising/falling",
    "volume_ratio": 1.5,
    "vwap_deviation_pct": 0.3,
    "adx_value": 28,
    "ema_trend": "bullish/bearish"
  },
  "confidence": <calculated 45-85>,
  "hold_duration": "3m" or "5m" or "10m" or "15m" or "30m",
  "target_price": <price>,
  "stop_loss": <price>,
  "reasoning": "brief: which gates passed/failed"
}

CRITICAL RULES:
- If ANY gate is false → action MUST be "NO_TRADE"
- RSI 45-55 = ALWAYS NO_TRADE (chop zone)
- NO_TRADE is correct risk avoidance, not a failed prediction
- Only trade in CLEAR regimes with strong momentum + participation`

// PredictionResult holds a prediction along with its chain of thought.
type PredictionResult struct {
	Prediction     *Prediction
	ChainOfThought *agents.ChainOfThought
}

// getAIPredictionVerbose gets an AI prediction with full chain of thought.
func getAIPredictionVerbose(ctx context.Context, app *App, symbol string, currentPrice float64, timeWindow time.Duration, threshold float64, tracker *PaperTracker, useTools bool) (*PredictionResult, error) {
	if useTools {
		return getAIPredictionWithToolsVerbose(ctx, app, symbol, currentPrice, timeWindow, threshold, tracker)
	}
	pred, err := getAIPredictionSimple(ctx, app, symbol, currentPrice, timeWindow, threshold, tracker)
	return &PredictionResult{Prediction: pred}, err
}

// getAIPredictionWithToolsVerbose uses OpenAI function calling and returns chain of thought.
func getAIPredictionWithToolsVerbose(ctx context.Context, app *App, symbol string, currentPrice float64, timeWindow time.Duration, threshold float64, tracker *PaperTracker) (*PredictionResult, error) {
	// Create tool executor
	toolExecutor := agents.NewToolExecutor(app.Broker)

	// Get recent prediction history for this symbol
	recentHistory := tracker.GetRecentHistory(10)
	var symbolHistory []*Prediction
	for _, p := range recentHistory {
		if p.Symbol == symbol {
			symbolHistory = append(symbolHistory, p)
		}
	}

	// Build prompt for AI with history context
	prompt := buildToolBasedPrompt(symbol, currentPrice, timeWindow, symbolHistory, tracker.GetStats())

	// Execution-grade prompt with HARD GATES and REGIME LOCKS
	systemPrompt := paperToolSystemPrompt

	// Get AI response with tools (verbose to capture chain of thought)
	tools := agents.GetToolDefinitions()
	cot, err := app.LLMClient.CompleteWithToolsVerbose(ctx, systemPrompt, prompt, tools, toolExecutor)
	if err != nil {
		return nil, fmt.Errorf("AI analysis failed: %w", err)
	}

	// Parse response with gate validation
	prediction, err := parsePredictionResponseWithGates(cot.Response, symbol, currentPrice, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	// Filter by threshold
	if prediction == nil || prediction.Confidence < threshold {
		return &PredictionResult{Prediction: nil, ChainOfThought: cot}, nil
	}

	return &PredictionResult{Prediction: prediction, ChainOfThought: cot}, nil
}

// getAIPredictionBacktest uses BacktestToolExecutor with pre-fetched candles.
// This ensures the AI sees data as of the simulation time, not current time.
func getAIPredictionBacktest(ctx context.Context, app *App, symbol string, candles []models.Candle, currentIndex int, currentPrice float64, timeWindow time.Duration, threshold float64, tracker *PaperTracker) (*PredictionResult, error) {
	// Create backtest tool executor with candles up to current simulation point
	toolExecutor := agents.NewBacktestToolExecutor(symbol, candles, currentIndex)

	// Get recent prediction history for this symbol
	recentHistory := tracker.GetRecentHistory(10)
	var symbolHistory []*Prediction
	for _, p := range recentHistory {
		if p.Symbol == symbol {
			symbolHistory = append(symbolHistory, p)
		}
	}

	// Build prompt for AI with history context
	prompt := buildToolBasedPrompt(symbol, currentPrice, timeWindow, symbolHistory, tracker.GetStats())

	// Execution-grade prompt with HARD GATES and REGIME LOCKS (same as live)
	systemPrompt := paperToolSystemPrompt

	// Get AI response with tools (verbose to capture chain of thought)
	tools := agents.GetToolDefinitions()
	cot, err := app.LLMClient.CompleteWithToolsVerbose(ctx, systemPrompt, prompt, tools, toolExecutor)
	if err != nil {
		return nil, fmt.Errorf("AI analysis failed: %w", err)
	}

	// Parse response with gate validation
	prediction, err := parsePredictionResponseWithGates(cot.Response, symbol, currentPrice, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	// Filter by threshold
	if prediction == nil || prediction.Confidence < threshold {
		return &PredictionResult{Prediction: nil, ChainOfThought: cot}, nil
	}

	return &PredictionResult{Prediction: prediction, ChainOfThought: cot}, nil
}

// getAIPredictionSimple uses simple prompts without tools (fallback).
func getAIPredictionSimple(ctx context.Context, app *App, symbol string, currentPrice float64, timeWindow time.Duration, threshold float64, tracker *PaperTracker) (*Prediction, error) {
	// Get historical data for context
	candles, err := app.Broker.GetHistorical(ctx, broker.HistoricalRequest{
		Symbol:    symbol,
		Exchange:  models.NSE,
		Timeframe: "15min",
		From:      time.Now().Add(-24 * time.Hour),
		To:        time.Now(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get historical data: %w", err)
	}

	// Get recent prediction history for this symbol
	recentHistory := tracker.GetRecentHistory(10)
	var symbolHistory []*Prediction
	for _, p := range recentHistory {
		if p.Symbol == symbol {
			symbolHistory = append(symbolHistory, p)
		}
	}

	// Build prompt for AI with history context
	prompt := buildPredictionPromptWithHistory(symbol, currentPrice, candles, timeWindow, symbolHistory, tracker.GetStats())

	// Get AI response
	systemPrompt := `You are a risk-first intraday trading analyst for Indian stock market (NSE).
	Analyze the given stock data and provide a trading prediction only when there is a clear edge.

	IMPORTANT:
	- Use NO_TRADE when the setup is unclear, choppy, low-volume, or risk/reward is poor.
	- Do not force a BUY or SELL from weak recent movement alone.
	- Only use BUY or SELL when direction, momentum, and risk levels are coherent.

	CRITICAL: You will see your PREVIOUS PREDICTIONS and their OUTCOMES. Learn from them!

	Respond ONLY with valid JSON in this exact format:
	{
	  "action": "BUY" or "SELL" or "NO_TRADE",
	  "confidence": 0-100,
	  "target_price": number,
	  "stop_loss": number,
	  "reasoning": "brief explanation"
	}

	Rules:
	- BUY requires target_price > current price and stop_loss < current price.
	- SELL requires target_price < current price and stop_loss > current price.
	- Target should be 0.3-1% from entry for short windows.
	- Stop loss should be 0.3-0.5% from entry.
	- If these levels are not coherent, return NO_TRADE.`

	response, err := app.LLMClient.CompleteWithSystem(ctx, systemPrompt, prompt)
	if err != nil {
		return nil, fmt.Errorf("AI analysis failed: %w", err)
	}

	// Parse response
	prediction, err := parsePredictionResponse(response, symbol, currentPrice, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	// Filter by threshold
	if prediction == nil || prediction.Confidence < threshold {
		return nil, nil
	}

	return prediction, nil
}

// getMarketSession returns the current market session description based on IST time.
// This helps AI understand market dynamics at different times of day.
func getMarketSession(t time.Time) string {
	hour := t.Hour()
	minute := t.Minute()
	totalMins := hour*60 + minute

	switch {
	case totalMins < 9*60+15:
		return "PRE-MARKET (market closed)"
	case totalMins < 9*60+45:
		return "OPENING (9:15-9:45) - HIGH VOLATILITY, avoid new positions, wait for trend"
	case totalMins < 11*60+30:
		return "MORNING SESSION (9:45-11:30) - BEST TRADING WINDOW, trends establish, good volume"
	case totalMins < 13*60:
		return "LUNCH LULL (11:30-13:00) - LOW VOLUME, choppy, avoid trading"
	case totalMins < 14*60+30:
		return "AFTERNOON SESSION (13:00-14:30) - Volume picks up, trend continuation"
	case totalMins < 15*60+30:
		return "CLOSING (14:30-15:30) - SQUARE-OFF PRESSURE, high volatility, quick reversals"
	default:
		return "AFTER-MARKET (market closed)"
	}
}

// buildToolBasedPrompt builds the prompt for tool-based AI prediction.
func buildToolBasedPrompt(symbol string, currentPrice float64, timeWindow time.Duration, history []*Prediction, stats PaperStats) string {
	var sb strings.Builder

	// Get IST time
	ist, _ := time.LoadLocation("Asia/Kolkata")
	now := time.Now().In(ist)

	// Determine market session
	marketSession := getMarketSession(now)

	sb.WriteString(fmt.Sprintf("Analyze %s for a trading decision.\n\n", symbol))
	sb.WriteString(fmt.Sprintf("Current Price: %.2f\n", currentPrice))
	sb.WriteString(fmt.Sprintf("Time Window: %s\n", timeWindow))
	sb.WriteString(fmt.Sprintf("Current Time: %s IST\n", now.Format("15:04:05")))
	sb.WriteString(fmt.Sprintf("Market Session: %s\n\n", marketSession))

	// Add previous predictions and outcomes for learning
	if len(history) > 0 {
		sb.WriteString("=== YOUR PREVIOUS PREDICTIONS (Learn from these!) ===\n")
		rightCount := 0
		wrongCount := 0
		for _, p := range history {
			outcomeEmoji := "❌"
			if p.Outcome == "RIGHT" {
				outcomeEmoji = "✅"
				rightCount++
			} else {
				wrongCount++
			}
			sb.WriteString(fmt.Sprintf("  %s %s @ %.2f → %s (P&L: %.2f%%) - %s\n",
				outcomeEmoji, p.Action, p.EntryPrice, p.Outcome, p.PnLPercent, p.Reasoning))
		}
		sb.WriteString(fmt.Sprintf("\nYour recent accuracy: %d RIGHT, %d WRONG\n", rightCount, wrongCount))

		// Add learning hints based on patterns
		if wrongCount > rightCount && len(history) >= 3 {
			sb.WriteString("⚠️ IMPORTANT: Your recent predictions have been mostly WRONG. Consider:\n")
			sb.WriteString("  - Being more conservative with confidence levels\n")
			sb.WriteString("  - Setting tighter stop losses\n")
			sb.WriteString("  - Waiting for clearer signals before predicting BUY/SELL\n")
		}
		if stats.WinRate > 0 && stats.WinRate < 45 {
			sb.WriteString(fmt.Sprintf("⚠️ Overall win rate is low (%.1f%%). Adjust your strategy!\n", stats.WinRate))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("INSTRUCTIONS:\n")
	sb.WriteString("1. Use the available tools to analyze the stock\n")
	sb.WriteString("2. Check RSI, Bollinger Bands, and candlestick patterns\n")
	sb.WriteString("3. Look at support/resistance levels\n")
	sb.WriteString("4. Make your prediction based on the tool results\n\n")
	sb.WriteString("Start by calling some analysis tools, then provide your prediction.")

	return sb.String()
}

// buildPredictionPromptWithHistory builds the prompt with previous decision history.
func buildPredictionPromptWithHistory(symbol string, currentPrice float64, candles []models.Candle, timeWindow time.Duration, history []*Prediction, stats PaperStats) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Symbol: %s\n", symbol))
	sb.WriteString(fmt.Sprintf("Current Price: %.2f\n", currentPrice))
	sb.WriteString(fmt.Sprintf("Time Window: %s\n", timeWindow))
	sb.WriteString(fmt.Sprintf("Current Time: %s IST\n\n", time.Now().Format("15:04:05")))

	// Add previous predictions and outcomes for learning
	if len(history) > 0 {
		sb.WriteString("=== YOUR PREVIOUS PREDICTIONS (Learn from these!) ===\n")
		rightCount := 0
		wrongCount := 0
		for _, p := range history {
			outcomeEmoji := "❌"
			if p.Outcome == "RIGHT" {
				outcomeEmoji = "✅"
				rightCount++
			} else {
				wrongCount++
			}
			sb.WriteString(fmt.Sprintf("  %s %s @ %.2f → %s (P&L: %.2f%%) - %s\n",
				outcomeEmoji, p.Action, p.EntryPrice, p.Outcome, p.PnLPercent, p.Reasoning))
		}
		sb.WriteString(fmt.Sprintf("\nYour recent accuracy: %d RIGHT, %d WRONG\n", rightCount, wrongCount))

		// Add learning hints based on patterns
		if wrongCount > rightCount && len(history) >= 3 {
			sb.WriteString("⚠️ IMPORTANT: Your recent predictions have been mostly WRONG. Consider:\n")
			sb.WriteString("  - Being more conservative with confidence levels\n")
			sb.WriteString("  - Setting tighter stop losses\n")
			sb.WriteString("  - Waiting for clearer signals before predicting BUY/SELL\n")
		}
		if stats.WinRate > 0 && stats.WinRate < 45 {
			sb.WriteString(fmt.Sprintf("⚠️ Overall win rate is low (%.1f%%). Adjust your strategy!\n", stats.WinRate))
		}
		sb.WriteString("\n")
	}

	// Add recent candles
	sb.WriteString("Recent 15-minute candles (last 10):\n")
	start := 0
	if len(candles) > 10 {
		start = len(candles) - 10
	}
	for i := start; i < len(candles); i++ {
		c := candles[i]
		change := ((c.Close - c.Open) / c.Open) * 100
		sb.WriteString(fmt.Sprintf("  %s: O=%.2f H=%.2f L=%.2f C=%.2f V=%d (%.2f%%)\n",
			c.Timestamp.Format("15:04"), c.Open, c.High, c.Low, c.Close, c.Volume, change))
	}

	// Calculate some basic indicators
	if len(candles) >= 5 {
		// Simple momentum
		recent := candles[len(candles)-5:]
		avgVolume := 0.0
		priceChange := 0.0
		for i, c := range recent {
			avgVolume += float64(c.Volume)
			if i > 0 {
				priceChange += c.Close - recent[i-1].Close
			}
		}
		avgVolume /= 5

		sb.WriteString(fmt.Sprintf("\nMomentum (5 candles): %.2f\n", priceChange))
		sb.WriteString(fmt.Sprintf("Avg Volume: %.0f\n", avgVolume))
	}

	// Day's range
	if len(candles) > 0 {
		dayHigh := candles[0].High
		dayLow := candles[0].Low
		for _, c := range candles {
			if c.High > dayHigh {
				dayHigh = c.High
			}
			if c.Low < dayLow {
				dayLow = c.Low
			}
		}
		sb.WriteString(fmt.Sprintf("Day Range: %.2f - %.2f\n", dayLow, dayHigh))

		// Position in range
		if dayHigh > dayLow {
			position := (currentPrice - dayLow) / (dayHigh - dayLow) * 100
			sb.WriteString(fmt.Sprintf("Position in Range: %.1f%%\n", position))
		}
	}

	sb.WriteString("\nProvide your prediction:")
	return sb.String()
}

// parsePredictionResponse parses the AI response into a Prediction.
// The AI can specify hold_duration (e.g., "3m", "5m", "15m") which overrides the CLI timeWindow.
func parsePredictionResponse(response string, symbol string, currentPrice float64, timeWindow time.Duration) (*Prediction, error) {
	jsonStr, err := extractJSONObject(response)
	if err != nil {
		return nil, err
	}

	var result struct {
		Action       string  `json:"action"`
		Confidence   float64 `json:"confidence"`
		HoldDuration string  `json:"hold_duration"` // AI-specified duration like "3m", "5m", "15m"
		TargetPrice  float64 `json:"target_price"`
		StopLoss     float64 `json:"stop_loss"`
		Reasoning    string  `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	action, tradeable := normalizePredictionAction(result.Action)
	if !tradeable {
		return nil, nil
	}

	if err := validatePredictionRisk(action, currentPrice, result.TargetPrice, result.StopLoss); err != nil {
		return nil, nil
	}

	// Parse hold_duration from AI response, fallback to CLI timeWindow
	holdDuration := parsePredictionHoldDuration(result.HoldDuration, timeWindow)

	now := time.Now()
	return &Prediction{
		Symbol:      symbol,
		Action:      action,
		Confidence:  clampPredictionConfidence(result.Confidence),
		EntryPrice:  currentPrice,
		TargetPrice: result.TargetPrice,
		StopLoss:    result.StopLoss,
		TimeWindow:  holdDuration,
		CreatedAt:   now,
		ExpiresAt:   now.Add(holdDuration),
		Reasoning:   result.Reasoning,
		SetupName:   "llm_simple",
		Timeframe:   predictionTimeframeLabel(holdDuration),
	}, nil
}

// parsePredictionResponseWithGates parses AI response with hard gate validation.
// Enforces: RSI regime lock, volume expansion, EMA alignment, VWAP exhaustion, trend strength.
func parsePredictionResponseWithGates(response string, symbol string, currentPrice float64, timeWindow time.Duration) (*Prediction, error) {
	jsonStr, err := extractJSONObject(response)
	if err != nil {
		return nil, err
	}

	var result struct {
		Action      string `json:"action"`
		GatesPassed struct {
			RSIRegime        bool `json:"rsi_regime"`
			RSIDirection     bool `json:"rsi_direction"` // backward compat
			VolumeExpansion  bool `json:"volume_expansion"`
			EMAAlignment     bool `json:"ema_alignment"`
			VWAPNotExhausted bool `json:"vwap_not_exhausted"`
			TrendStrength    bool `json:"trend_strength"`
		} `json:"gates_passed"`
		SignalQuality struct {
			RSIValue         float64 `json:"rsi_value"`
			RSIDirection     string  `json:"rsi_direction"`
			RSISlope         string  `json:"rsi_slope"` // backward compat
			VolumeRatio      float64 `json:"volume_ratio"`
			VWAPDeviationPct float64 `json:"vwap_deviation_pct"`
			ADXValue         float64 `json:"adx_value"`
			EMATrend         string  `json:"ema_trend"`
			MTFAligned       bool    `json:"mtf_aligned"`
		} `json:"signal_quality"`
		Confidence   float64 `json:"confidence"`
		HoldDuration string  `json:"hold_duration"`
		TargetPrice  float64 `json:"target_price"`
		StopLoss     float64 `json:"stop_loss"`
		Reasoning    string  `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	action, tradeable := normalizePredictionAction(result.Action)
	if !tradeable {
		return nil, nil
	}

	// HARD GATE ENFORCEMENT: All 5 gates must pass
	// Use RSIRegime if present, fall back to RSIDirection for backward compat
	rsiGate := result.GatesPassed.RSIRegime || result.GatesPassed.RSIDirection

	allGatesPassed := rsiGate &&
		result.GatesPassed.VolumeExpansion &&
		result.GatesPassed.EMAAlignment &&
		result.GatesPassed.VWAPNotExhausted &&
		result.GatesPassed.TrendStrength

	if !allGatesPassed {
		// AI tried to trade but gates didn't pass - reject as safety net
		return nil, nil
	}

	if err := validatePredictionRisk(action, currentPrice, result.TargetPrice, result.StopLoss); err != nil {
		return nil, nil
	}

	holdDuration := parsePredictionHoldDuration(result.HoldDuration, timeWindow)

	// Build reasoning with signal quality
	reasoning := result.Reasoning
	if reasoning == "" {
		reasoning = fmt.Sprintf("RSI=%.0f %s | Vol=%.1fx | VWAP=%.2f%% | ADX=%.0f | EMA=%s",
			result.SignalQuality.RSIValue,
			result.SignalQuality.RSIDirection,
			result.SignalQuality.VolumeRatio,
			result.SignalQuality.VWAPDeviationPct,
			result.SignalQuality.ADXValue,
			result.SignalQuality.EMATrend)
	}

	now := time.Now()
	return &Prediction{
		Symbol:      symbol,
		Action:      action,
		Confidence:  clampPredictionConfidence(result.Confidence),
		EntryPrice:  currentPrice,
		TargetPrice: result.TargetPrice,
		StopLoss:    result.StopLoss,
		TimeWindow:  holdDuration,
		CreatedAt:   now,
		ExpiresAt:   now.Add(holdDuration),
		Reasoning:   reasoning,
		SetupName:   "llm_hard_gates",
		Timeframe:   predictionTimeframeLabel(holdDuration),
		Gates: []models.PaperPredictionGate{
			{Name: "rsi_regime", Passed: rsiGate},
			{Name: "volume_expansion", Passed: result.GatesPassed.VolumeExpansion},
			{Name: "ema_alignment", Passed: result.GatesPassed.EMAAlignment},
			{Name: "vwap_not_exhausted", Passed: result.GatesPassed.VWAPNotExhausted},
			{Name: "trend_strength", Passed: result.GatesPassed.TrendStrength},
		},
	}, nil
}

func predictionTimeframeLabel(duration time.Duration) string {
	if duration <= 0 {
		return ""
	}
	if duration%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(duration/time.Hour))
	}
	if duration%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(duration/time.Minute))
	}
	if duration%time.Second == 0 {
		return fmt.Sprintf("%ds", int(duration/time.Second))
	}
	return duration.String()
}

func extractJSONObject(response string) (string, error) {
	response = strings.TrimSpace(response)
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end <= start {
		return "", fmt.Errorf("no JSON found in response")
	}
	return response[start : end+1], nil
}

func normalizePredictionAction(action string) (string, bool) {
	action = strings.ToUpper(strings.TrimSpace(action))
	action = strings.ReplaceAll(action, " ", "_")
	action = strings.ReplaceAll(action, "-", "_")
	switch action {
	case "BUY", "SELL":
		return action, true
	default:
		return "", false
	}
}

func validatePredictionRisk(action string, currentPrice, targetPrice, stopLoss float64) error {
	if currentPrice <= 0 {
		return fmt.Errorf("current price must be positive")
	}
	if targetPrice <= 0 || stopLoss <= 0 {
		return fmt.Errorf("target and stop loss must be positive")
	}

	switch action {
	case "BUY":
		if targetPrice <= currentPrice {
			return fmt.Errorf("BUY target must be above entry")
		}
		if stopLoss >= currentPrice {
			return fmt.Errorf("BUY stop loss must be below entry")
		}
	case "SELL":
		if targetPrice >= currentPrice {
			return fmt.Errorf("SELL target must be below entry")
		}
		if stopLoss <= currentPrice {
			return fmt.Errorf("SELL stop loss must be above entry")
		}
	default:
		return fmt.Errorf("unsupported action: %s", action)
	}

	risk := currentPrice - stopLoss
	reward := targetPrice - currentPrice
	if action == "SELL" {
		risk = stopLoss - currentPrice
		reward = currentPrice - targetPrice
	}
	if risk <= 0 || reward <= 0 {
		return fmt.Errorf("risk and reward must be positive")
	}
	if reward/risk < 1.0 {
		return fmt.Errorf("risk reward below 1:1")
	}
	return nil
}

func parsePredictionHoldDuration(value string, fallback time.Duration) time.Duration {
	if fallback <= 0 {
		fallback = 15 * time.Minute
	}
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	if parsed < time.Minute {
		return time.Minute
	}
	if parsed > 2*time.Hour {
		return 2 * time.Hour
	}
	return parsed
}

func clampPredictionConfidence(confidence float64) float64 {
	if confidence < 0 {
		return 0
	}
	if confidence > 100 {
		return 100
	}
	return confidence
}
