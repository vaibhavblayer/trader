// Package agents provides AI agent interfaces and implementations for trading decisions.
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"

	"zerodha-trader/internal/analysis"
	"zerodha-trader/internal/analysis/indicators"
	"zerodha-trader/internal/analysis/patterns"
	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

// ToolDefinition represents an OpenAI function tool definition.
type ToolDefinition struct {
	Type     string                         `json:"type"`
	Function openai.FunctionDefinition      `json:"function"`
}

// ToolExecutor executes AI tool calls against the trading system.
type ToolExecutor struct {
	broker           broker.Broker
	indicatorEngine  *indicators.Engine
	candleDetector   *patterns.CandlestickDetector
	chartDetector    *patterns.ChartPatternDetector
}

// NewToolExecutor creates a new tool executor.
func NewToolExecutor(b broker.Broker) *ToolExecutor {
	// Initialize indicator engine with common indicators
	engine := indicators.NewEngine(4)
	
	// Register momentum indicators
	engine.RegisterIndicator(indicators.NewRSI(14))
	engine.RegisterIndicator(indicators.NewRSI(7))
	engine.RegisterIndicator(indicators.NewCCI(20))
	engine.RegisterIndicator(indicators.NewWilliamsR(14))
	engine.RegisterIndicator(indicators.NewROC(12))
	engine.RegisterIndicator(indicators.NewMomentum(10))
	
	// Register volatility indicators
	engine.RegisterIndicator(indicators.NewATR(14))
	
	// Register multi-value indicators
	engine.RegisterMultiIndicator(indicators.NewBollingerBands(20, 2.0))
	engine.RegisterMultiIndicator(indicators.NewStochastic(14, 3, 3))
	engine.RegisterMultiIndicator(indicators.NewKeltnerChannels(20, 10, 2.0))
	engine.RegisterMultiIndicator(indicators.NewDonchianChannels(20))
	
	return &ToolExecutor{
		broker:          b,
		indicatorEngine: engine,
		candleDetector:  patterns.NewCandlestickDetector(),
		chartDetector:   patterns.NewChartPatternDetector(),
	}
}

// BacktestToolExecutor executes AI tool calls using pre-fetched historical candles.
// This is used in backtest mode to ensure tools see data as of the simulation time,
// not current time.
type BacktestToolExecutor struct {
	candles          []models.Candle  // Pre-fetched candles for the symbol
	symbol           string           // Symbol these candles are for
	currentIndex     int              // Current position in the candle array (simulation time)
	indicatorEngine  *indicators.Engine
	candleDetector   *patterns.CandlestickDetector
	chartDetector    *patterns.ChartPatternDetector
}

// NewBacktestToolExecutor creates a tool executor for backtest mode.
// candles: the full historical candle data
// currentIndex: the index representing "now" in the simulation (tools will only see candles[0:currentIndex+1])
func NewBacktestToolExecutor(symbol string, candles []models.Candle, currentIndex int) *BacktestToolExecutor {
	// Initialize indicator engine with common indicators
	engine := indicators.NewEngine(4)
	
	// Register momentum indicators
	engine.RegisterIndicator(indicators.NewRSI(14))
	engine.RegisterIndicator(indicators.NewRSI(7))
	engine.RegisterIndicator(indicators.NewCCI(20))
	engine.RegisterIndicator(indicators.NewWilliamsR(14))
	engine.RegisterIndicator(indicators.NewROC(12))
	engine.RegisterIndicator(indicators.NewMomentum(10))
	
	// Register volatility indicators
	engine.RegisterIndicator(indicators.NewATR(14))
	
	// Register multi-value indicators
	engine.RegisterMultiIndicator(indicators.NewBollingerBands(20, 2.0))
	engine.RegisterMultiIndicator(indicators.NewStochastic(14, 3, 3))
	engine.RegisterMultiIndicator(indicators.NewKeltnerChannels(20, 10, 2.0))
	engine.RegisterMultiIndicator(indicators.NewDonchianChannels(20))
	
	return &BacktestToolExecutor{
		candles:         candles,
		symbol:          symbol,
		currentIndex:    currentIndex,
		indicatorEngine: engine,
		candleDetector:  patterns.NewCandlestickDetector(),
		chartDetector:   patterns.NewChartPatternDetector(),
	}
}

// getVisibleCandles returns candles visible at the current simulation time
func (bte *BacktestToolExecutor) getVisibleCandles() []models.Candle {
	if bte.currentIndex >= len(bte.candles) {
		return bte.candles
	}
	return bte.candles[:bte.currentIndex+1]
}

// ExecuteTool executes a tool call using cached candles instead of API.
func (bte *BacktestToolExecutor) ExecuteTool(ctx context.Context, toolName string, args json.RawMessage) (string, error) {
	var params map[string]interface{}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	// Get visible candles (up to current simulation time)
	candles := bte.getVisibleCandles()
	
	switch toolName {
	case "get_historical_data":
		return bte.executeGetHistoricalData(candles, params)
	case "calculate_rsi":
		return bte.executeCalculateRSI(candles, params)
	case "calculate_bollinger_bands":
		return bte.executeCalculateBollingerBands(candles, params)
	case "calculate_fibonacci_levels":
		return bte.executeCalculateFibonacci(candles, params)
	case "get_support_resistance":
		return bte.executeGetSupportResistance(candles, params)
	case "detect_candlestick_patterns":
		return bte.executeDetectCandlestickPatterns(candles, params)
	case "detect_chart_patterns":
		return bte.executeDetectChartPatterns(candles, params)
	case "calculate_atr":
		return bte.executeCalculateATR(candles, params)
	case "calculate_stochastic":
		return bte.executeCalculateStochastic(candles, params)
	case "get_mtf_analysis":
		return bte.executeGetMTFAnalysis(candles, params)
	case "calculate_macd":
		return bte.executeCalculateMACD(candles, params)
	case "calculate_ema_crossover":
		return bte.executeCalculateEMACrossover(candles, params)
	case "calculate_adx":
		return bte.executeCalculateADX(candles, params)
	case "analyze_volume":
		return bte.executeAnalyzeVolume(candles, params)
	case "calculate_vwap":
		return bte.executeCalculateVWAP(candles, params)
	default:
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
}

// executeGetHistoricalData returns historical data from cached candles
func (bte *BacktestToolExecutor) executeGetHistoricalData(candles []models.Candle, params map[string]interface{}) (string, error) {
	symbol := strings.ToUpper(getStringParam(params, "symbol", bte.symbol))
	timeframe := getStringParam(params, "timeframe", "15min")
	
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Historical data for %s (%s timeframe, %d candles):\n\n", symbol, timeframe, len(candles)))
	
	// Show last 20 candles
	start := 0
	if len(candles) > 20 {
		start = len(candles) - 20
	}
	
	sb.WriteString("Recent candles:\n")
	for i := start; i < len(candles); i++ {
		c := candles[i]
		change := ((c.Close - c.Open) / c.Open) * 100
		sb.WriteString(fmt.Sprintf("  %s: O=%.2f H=%.2f L=%.2f C=%.2f V=%d (%.2f%%)\n",
			c.Timestamp.Format("2006-01-02 15:04"), c.Open, c.High, c.Low, c.Close, c.Volume, change))
	}
	
	if len(candles) > 0 {
		first := candles[0]
		last := candles[len(candles)-1]
		high := first.High
		low := first.Low
		for _, c := range candles {
			if c.High > high { high = c.High }
			if c.Low < low { low = c.Low }
		}
		sb.WriteString(fmt.Sprintf("\nSummary:\n  Period High: %.2f\n  Period Low: %.2f\n  Current Price: %.2f\n",
			high, low, last.Close))
	}
	return sb.String(), nil
}

// executeCalculateRSI calculates RSI on cached candles
func (bte *BacktestToolExecutor) executeCalculateRSI(candles []models.Candle, params map[string]interface{}) (string, error) {
	period := getIntParam(params, "period", 14)
	
	if len(candles) < period+1 {
		return "", fmt.Errorf("insufficient data for RSI calculation (need %d, have %d)", period+1, len(candles))
	}

	rsi := indicators.NewRSI(period)
	values, err := rsi.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate RSI: %w", err)
	}

	current := values[len(values)-1]
	prev := values[len(values)-2]
	
	var signal string
	if current > 70 {
		signal = "OVERBOUGHT - potential sell signal"
	} else if current < 30 {
		signal = "OVERSOLD - potential buy signal"
	} else if current > 50 {
		signal = "BULLISH momentum"
	} else {
		signal = "BEARISH momentum"
	}
	
	trend := "rising"
	if current < prev {
		trend = "falling"
	}

	return fmt.Sprintf("RSI(%d) for %s (15min):\n  Current: %.2f (%s)\n  Previous: %.2f\n  Trend: %s\n  Signal: %s",
		period, bte.symbol, current, signal, prev, trend, signal), nil
}

// executeCalculateBollingerBands calculates Bollinger Bands on cached candles
func (bte *BacktestToolExecutor) executeCalculateBollingerBands(candles []models.Candle, params map[string]interface{}) (string, error) {
	period := getIntParam(params, "period", 20)
	stdDev := getFloatParam(params, "std_dev", 2.0)
	
	if len(candles) < period {
		return "", fmt.Errorf("insufficient data for Bollinger Bands (need %d, have %d)", period, len(candles))
	}

	bb := indicators.NewBollingerBands(period, stdDev)
	values, err := bb.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate Bollinger Bands: %w", err)
	}

	n := len(candles) - 1
	upper := values["upper"][n]
	middle := values["middle"][n]
	lower := values["lower"][n]
	percentB := values["percent_b"][n]
	bandwidth := values["bandwidth"][n]
	currentPrice := candles[n].Close

	var position string
	if percentB > 1 {
		position = "ABOVE upper band - overbought"
	} else if percentB < 0 {
		position = "BELOW lower band - oversold"
	} else if percentB > 0.8 {
		position = "Near upper band - potential resistance"
	} else if percentB < 0.2 {
		position = "Near lower band - potential support"
	} else {
		position = "Within bands - neutral"
	}

	return fmt.Sprintf("Bollinger Bands(%d, %.1f) for %s (15min):\n  Upper Band: %.2f\n  Middle Band: %.2f\n  Lower Band: %.2f\n  Current Price: %.2f\n  %%B: %.2f\n  Bandwidth: %.4f\n  Position: %s",
		period, stdDev, bte.symbol, upper, middle, lower, currentPrice, percentB, bandwidth, position), nil
}

// executeCalculateFibonacci calculates Fibonacci levels on cached candles
func (bte *BacktestToolExecutor) executeCalculateFibonacci(candles []models.Candle, params map[string]interface{}) (string, error) {
	lookback := getIntParam(params, "lookback", 50)
	if lookback > len(candles) {
		lookback = len(candles)
	}

	fib := indicators.NewFibonacciRetracement(lookback)
	levels, err := fib.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate Fibonacci: %w", err)
	}

	currentPrice := candles[len(candles)-1].Close
	trend := "UPTREND"
	if !levels.IsUptrend {
		trend = "DOWNTREND"
	}

	return fmt.Sprintf("Fibonacci Levels for %s (15min, %d period lookback):\n  Trend: %s\n  Swing High: %.2f\n  Swing Low: %.2f\n  Current Price: %.2f\n\n  Retracement Levels:\n    23.6%%: %.2f\n    38.2%%: %.2f\n    50.0%%: %.2f\n    61.8%%: %.2f\n    78.6%%: %.2f",
		bte.symbol, lookback, trend, levels.SwingHigh, levels.SwingLow, currentPrice,
		levels.Level236, levels.Level382, levels.Level500, levels.Level618, levels.Level786), nil
}

// executeGetSupportResistance calculates pivot points on cached candles
func (bte *BacktestToolExecutor) executeGetSupportResistance(candles []models.Candle, params map[string]interface{}) (string, error) {
	if len(candles) < 2 {
		return "", fmt.Errorf("insufficient data for pivot calculation")
	}

	// Use previous candle for pivot calculation
	prevCandle := candles[len(candles)-2]
	currentPrice := candles[len(candles)-1].Close

	pivot := indicators.NewStandardPivotPoints()
	levels := pivot.CalculateFromCandle(prevCandle)

	var position string
	if currentPrice > levels.R1 {
		position = "Above R1 - bullish"
	} else if currentPrice < levels.S1 {
		position = "Below S1 - bearish"
	} else if currentPrice > levels.Pivot {
		position = "Above Pivot - mildly bullish"
	} else {
		position = "Below Pivot - mildly bearish"
	}

	return fmt.Sprintf("Support/Resistance for %s (15min):\n\nStandard Pivot Points:\n  R3: %.2f\n  R2: %.2f\n  R1: %.2f\n  Pivot: %.2f\n  S1: %.2f\n  S2: %.2f\n  S3: %.2f\n\nCurrent Price: %.2f\nPosition: %s",
		bte.symbol, levels.R3, levels.R2, levels.R1, levels.Pivot, levels.S1, levels.S2, levels.S3, currentPrice, position), nil
}

// executeDetectCandlestickPatterns detects patterns on cached candles
func (bte *BacktestToolExecutor) executeDetectCandlestickPatterns(candles []models.Candle, params map[string]interface{}) (string, error) {
	patterns, err := bte.candleDetector.Detect(candles)
	if err != nil {
		return "", fmt.Errorf("failed to detect patterns: %w", err)
	}

	if len(patterns) == 0 {
		return fmt.Sprintf("No candlestick patterns detected for %s (15min)", bte.symbol), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Candlestick Patterns for %s (15min):\n\n", bte.symbol))

	// Group by recent patterns (last 10 candles)
	recentThreshold := len(candles) - 10
	var recentPatterns []analysis.Pattern
	
	for _, p := range patterns {
		if p.EndIndex >= recentThreshold {
			recentPatterns = append(recentPatterns, p)
		}
	}

	if len(recentPatterns) > 0 {
		sb.WriteString("RECENT PATTERNS (last 10 candles):\n")
		for _, p := range recentPatterns {
			direction := "Neutral"
			if p.Direction == analysis.PatternBullish {
				direction = "BULLISH ↑"
			} else if p.Direction == analysis.PatternBearish {
				direction = "BEARISH ↓"
			}
			volumeStr := ""
			if p.VolumeConfirm {
				volumeStr = " [Volume Confirmed]"
			}
			sb.WriteString(fmt.Sprintf("  • %s - %s (Strength: %.0f%%)%s\n", p.Name, direction, p.Strength*100, volumeStr))
		}
	}

	// Summary
	bullish := 0
	bearish := 0
	for _, p := range recentPatterns {
		if p.Direction == analysis.PatternBullish {
			bullish++
		} else if p.Direction == analysis.PatternBearish {
			bearish++
		}
	}
	sb.WriteString(fmt.Sprintf("\nSummary: %d bullish, %d bearish patterns in recent candles", bullish, bearish))

	return sb.String(), nil
}

// executeDetectChartPatterns detects chart patterns on cached candles
func (bte *BacktestToolExecutor) executeDetectChartPatterns(candles []models.Candle, params map[string]interface{}) (string, error) {
	patterns, err := bte.chartDetector.Detect(candles)
	if err != nil {
		return "", fmt.Errorf("failed to detect patterns: %w", err)
	}

	if len(patterns) == 0 {
		return fmt.Sprintf("No chart patterns detected for %s (15min)", bte.symbol), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Chart Patterns for %s (15min):\n\n", bte.symbol))

	for _, p := range patterns {
		direction := "Neutral"
		if p.Direction == analysis.PatternBullish {
			direction = "BULLISH ↑"
		} else if p.Direction == analysis.PatternBearish {
			direction = "BEARISH ↓"
		}
		sb.WriteString(fmt.Sprintf("• %s - %s (Strength: %.0f%%)\n", p.Name, direction, p.Strength*100))
	}

	return sb.String(), nil
}

// executeCalculateATR calculates ATR on cached candles
func (bte *BacktestToolExecutor) executeCalculateATR(candles []models.Candle, params map[string]interface{}) (string, error) {
	period := getIntParam(params, "period", 14)
	
	if len(candles) < period+1 {
		return "", fmt.Errorf("insufficient data for ATR calculation (need %d, have %d)", period+1, len(candles))
	}

	atr := indicators.NewATR(period)
	values, err := atr.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate ATR: %w", err)
	}

	current := values[len(values)-1]
	prev := values[len(values)-2]
	currentPrice := candles[len(candles)-1].Close
	atrPercent := (current / currentPrice) * 100

	longSL := currentPrice - (current * 1.5)
	shortSL := currentPrice + (current * 1.5)

	trend := "increasing (higher volatility)"
	if current < prev {
		trend = "decreasing (lower volatility)"
	}

	return fmt.Sprintf("ATR(%d) for %s (15min):\n  Current ATR: %.2f (%.2f%% of price)\n  Previous ATR: %.2f\n  Trend: %s\n\nSuggested Stop Loss Levels (1.5x ATR):\n  For LONG: %.2f (%.2f%% below current)\n  For SHORT: %.2f (%.2f%% above current)\n\nCurrent Price: %.2f",
		period, bte.symbol, current, atrPercent, prev, trend,
		longSL, ((currentPrice-longSL)/currentPrice)*100,
		shortSL, ((shortSL-currentPrice)/currentPrice)*100,
		currentPrice), nil
}

// executeCalculateStochastic calculates Stochastic on cached candles
func (bte *BacktestToolExecutor) executeCalculateStochastic(candles []models.Candle, params map[string]interface{}) (string, error) {
	kPeriod := getIntParam(params, "k_period", 14)
	dPeriod := getIntParam(params, "d_period", 3)
	
	if len(candles) < kPeriod+dPeriod {
		return "", fmt.Errorf("insufficient data for Stochastic calculation")
	}

	stoch := indicators.NewStochastic(kPeriod, dPeriod, 3)
	values, err := stoch.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate Stochastic: %w", err)
	}

	n := len(candles) - 1
	percentK := values["percent_k"][n]
	percentD := values["percent_d"][n]
	prevK := values["percent_k"][n-1]
	prevD := values["percent_d"][n-1]

	var signal string
	if percentK > 80 && percentD > 80 {
		signal = "OVERBOUGHT - potential sell signal"
	} else if percentK < 20 && percentD < 20 {
		signal = "OVERSOLD - potential buy signal"
	} else if percentK > percentD && prevK <= prevD {
		signal = "BULLISH CROSSOVER - %K crossed above %D"
	} else if percentK < percentD && prevK >= prevD {
		signal = "BEARISH CROSSOVER - %K crossed below %D"
	} else if percentK > percentD {
		signal = "Bullish momentum (%K above %D)"
	} else {
		signal = "Bearish momentum (%K below %D)"
	}

	return fmt.Sprintf("Stochastic(%d,%d) for %s (15min):\n  %%K: %.2f\n  %%D: %.2f\n  Previous %%K: %.2f\n  Previous %%D: %.2f\n\n  Signal: %s",
		kPeriod, dPeriod, bte.symbol, percentK, percentD, prevK, prevD, signal), nil
}

// executeGetMTFAnalysis performs analysis on cached candles (single timeframe only in backtest)
func (bte *BacktestToolExecutor) executeGetMTFAnalysis(candles []models.Candle, params map[string]interface{}) (string, error) {
	if len(candles) < 20 {
		return fmt.Sprintf("Insufficient data for MTF analysis on %s", bte.symbol), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Analysis for %s (15min timeframe):\n\n", bte.symbol))

	// Calculate RSI
	rsi := indicators.NewRSI(14)
	rsiValues, _ := rsi.Calculate(candles)
	currentRSI := rsiValues[len(rsiValues)-1]

	// Calculate trend using SMA
	last := candles[len(candles)-1]
	sum := 0.0
	for i := len(candles) - 20; i < len(candles); i++ {
		sum += candles[i].Close
	}
	sma20 := sum / 20

	trend := "BULLISH ↑"
	if last.Close < sma20 {
		trend = "BEARISH ↓"
	}

	rsiSignal := "Neutral"
	if currentRSI > 70 {
		rsiSignal = "Overbought"
	} else if currentRSI < 30 {
		rsiSignal = "Oversold"
	} else if currentRSI > 50 {
		rsiSignal = "Strong momentum"
	}

	sb.WriteString(fmt.Sprintf("15min:\n  Trend: %s (Price: %.2f, SMA20: %.2f)\n  RSI: %.1f (%s)\n",
		trend, last.Close, sma20, currentRSI, rsiSignal))

	return sb.String(), nil
}

// GetToolDefinitions returns all available tool definitions for OpenAI function calling.
func GetToolDefinitions() []openai.Tool {
	return []openai.Tool{
		// Historical Data Tool
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "get_historical_data",
				Description: "Fetch historical OHLCV candle data for a symbol. Use this to analyze price history, calculate indicators, or backtest patterns.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"symbol": {
							"type": "string",
							"description": "Stock symbol (e.g., RELIANCE, TCS, INFY)"
						},
						"timeframe": {
							"type": "string",
							"enum": ["1min", "5min", "15min", "30min", "1hour", "1day"],
							"description": "Candle timeframe"
						},
						"periods": {
							"type": "integer",
							"description": "Number of periods to fetch (max 500)",
							"default": 100
						}
					},
					"required": ["symbol", "timeframe"]
				}`),
			},
		},
		// RSI Tool
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "calculate_rsi",
				Description: "Calculate RSI (Relative Strength Index) for a symbol. RSI > 70 indicates overbought, RSI < 30 indicates oversold.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"symbol": {
							"type": "string",
							"description": "Stock symbol"
						},
						"timeframe": {
							"type": "string",
							"enum": ["1min", "5min", "15min", "30min", "1hour", "1day"],
							"description": "Candle timeframe"
						},
						"period": {
							"type": "integer",
							"description": "RSI period (default 14)",
							"default": 14
						}
					},
					"required": ["symbol", "timeframe"]
				}`),
			},
		},
		// Bollinger Bands Tool
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "calculate_bollinger_bands",
				Description: "Calculate Bollinger Bands for a symbol. Returns upper, middle, lower bands and %B indicator.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"symbol": {
							"type": "string",
							"description": "Stock symbol"
						},
						"timeframe": {
							"type": "string",
							"enum": ["1min", "5min", "15min", "30min", "1hour", "1day"],
							"description": "Candle timeframe"
						},
						"period": {
							"type": "integer",
							"description": "Period for SMA (default 20)",
							"default": 20
						},
						"std_dev": {
							"type": "number",
							"description": "Standard deviation multiplier (default 2.0)",
							"default": 2.0
						}
					},
					"required": ["symbol", "timeframe"]
				}`),
			},
		},
		// Fibonacci Levels Tool
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "calculate_fibonacci_levels",
				Description: "Calculate Fibonacci retracement and extension levels based on recent swing high/low.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"symbol": {
							"type": "string",
							"description": "Stock symbol"
						},
						"timeframe": {
							"type": "string",
							"enum": ["1min", "5min", "15min", "30min", "1hour", "1day"],
							"description": "Candle timeframe"
						},
						"lookback": {
							"type": "integer",
							"description": "Lookback period to find swing points (default 50)",
							"default": 50
						}
					},
					"required": ["symbol", "timeframe"]
				}`),
			},
		},
		// Support/Resistance Tool
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "get_support_resistance",
				Description: "Calculate pivot points and support/resistance levels for a symbol.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"symbol": {
							"type": "string",
							"description": "Stock symbol"
						},
						"timeframe": {
							"type": "string",
							"enum": ["15min", "30min", "1hour", "1day"],
							"description": "Candle timeframe for pivot calculation"
						}
					},
					"required": ["symbol", "timeframe"]
				}`),
			},
		},
		// Candlestick Patterns Tool
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "detect_candlestick_patterns",
				Description: "Detect candlestick patterns like Doji, Hammer, Engulfing, Morning Star, etc.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"symbol": {
							"type": "string",
							"description": "Stock symbol"
						},
						"timeframe": {
							"type": "string",
							"enum": ["1min", "5min", "15min", "30min", "1hour", "1day"],
							"description": "Candle timeframe"
						}
					},
					"required": ["symbol", "timeframe"]
				}`),
			},
		},
		// Chart Patterns Tool
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "detect_chart_patterns",
				Description: "Detect chart patterns like Head & Shoulders, Double Top/Bottom, Triangles, Wedges, etc.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"symbol": {
							"type": "string",
							"description": "Stock symbol"
						},
						"timeframe": {
							"type": "string",
							"enum": ["15min", "30min", "1hour", "1day"],
							"description": "Candle timeframe (longer timeframes work better)"
						}
					},
					"required": ["symbol", "timeframe"]
				}`),
			},
		},
		// ATR Tool
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "calculate_atr",
				Description: "Calculate ATR (Average True Range) for volatility analysis and stop loss placement.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"symbol": {
							"type": "string",
							"description": "Stock symbol"
						},
						"timeframe": {
							"type": "string",
							"enum": ["1min", "5min", "15min", "30min", "1hour", "1day"],
							"description": "Candle timeframe"
						},
						"period": {
							"type": "integer",
							"description": "ATR period (default 14)",
							"default": 14
						}
					},
					"required": ["symbol", "timeframe"]
				}`),
			},
		},
		// Stochastic Tool
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "calculate_stochastic",
				Description: "Calculate Stochastic Oscillator (%K and %D). Values > 80 indicate overbought, < 20 indicate oversold.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"symbol": {
							"type": "string",
							"description": "Stock symbol"
						},
						"timeframe": {
							"type": "string",
							"enum": ["1min", "5min", "15min", "30min", "1hour", "1day"],
							"description": "Candle timeframe"
						},
						"k_period": {
							"type": "integer",
							"description": "%K period (default 14)",
							"default": 14
						},
						"d_period": {
							"type": "integer",
							"description": "%D period (default 3)",
							"default": 3
						}
					},
					"required": ["symbol", "timeframe"]
				}`),
			},
		},
		// Multi-Timeframe Analysis Tool
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "get_mtf_analysis",
				Description: "Get multi-timeframe analysis showing trend alignment across different timeframes.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"symbol": {
							"type": "string",
							"description": "Stock symbol"
						}
					},
					"required": ["symbol"]
				}`),
			},
		},
		// MACD Tool
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "calculate_macd",
				Description: "Calculate MACD (Moving Average Convergence Divergence). Shows trend direction and momentum. Bullish when MACD crosses above signal line, bearish when below.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"symbol": {
							"type": "string",
							"description": "Stock symbol"
						},
						"timeframe": {
							"type": "string",
							"enum": ["5min", "15min", "30min", "1hour", "1day"],
							"description": "Candle timeframe"
						},
						"fast_period": {
							"type": "integer",
							"description": "Fast EMA period (default 12)",
							"default": 12
						},
						"slow_period": {
							"type": "integer",
							"description": "Slow EMA period (default 26)",
							"default": 26
						},
						"signal_period": {
							"type": "integer",
							"description": "Signal line period (default 9)",
							"default": 9
						}
					},
					"required": ["symbol", "timeframe"]
				}`),
			},
		},
		// EMA Crossover Tool
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "calculate_ema_crossover",
				Description: "Calculate EMA crossover signals. Uses fast and slow EMAs to identify trend changes. Golden cross (fast > slow) is bullish, death cross (fast < slow) is bearish.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"symbol": {
							"type": "string",
							"description": "Stock symbol"
						},
						"timeframe": {
							"type": "string",
							"enum": ["5min", "15min", "30min", "1hour", "1day"],
							"description": "Candle timeframe"
						},
						"fast_period": {
							"type": "integer",
							"description": "Fast EMA period (default 9)",
							"default": 9
						},
						"slow_period": {
							"type": "integer",
							"description": "Slow EMA period (default 21)",
							"default": 21
						}
					},
					"required": ["symbol", "timeframe"]
				}`),
			},
		},
		// ADX Tool
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "calculate_adx",
				Description: "Calculate ADX (Average Directional Index) for trend strength. ADX > 25 indicates strong trend, < 20 indicates weak/ranging market. +DI > -DI is bullish, -DI > +DI is bearish.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"symbol": {
							"type": "string",
							"description": "Stock symbol"
						},
						"timeframe": {
							"type": "string",
							"enum": ["5min", "15min", "30min", "1hour", "1day"],
							"description": "Candle timeframe"
						},
						"period": {
							"type": "integer",
							"description": "ADX period (default 14)",
							"default": 14
						}
					},
					"required": ["symbol", "timeframe"]
				}`),
			},
		},
		// Volume Analysis Tool
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "analyze_volume",
				Description: "Analyze volume patterns and trends. High volume confirms price moves, low volume suggests weak moves. Volume spikes often precede breakouts.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"symbol": {
							"type": "string",
							"description": "Stock symbol"
						},
						"timeframe": {
							"type": "string",
							"enum": ["5min", "15min", "30min", "1hour", "1day"],
							"description": "Candle timeframe"
						}
					},
					"required": ["symbol", "timeframe"]
				}`),
			},
		},
		// VWAP Tool
		{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        "calculate_vwap",
				Description: "Calculate VWAP (Volume Weighted Average Price). Price above VWAP is bullish, below is bearish. Institutional traders often use VWAP as a benchmark.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"symbol": {
							"type": "string",
							"description": "Stock symbol"
						},
						"timeframe": {
							"type": "string",
							"enum": ["5min", "15min", "30min"],
							"description": "Candle timeframe (intraday only)"
						}
					},
					"required": ["symbol", "timeframe"]
				}`),
			},
		},
	}
}


// ExecuteTool executes a tool call and returns the result as a string.
func (te *ToolExecutor) ExecuteTool(ctx context.Context, toolName string, args json.RawMessage) (string, error) {
	var params map[string]interface{}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	switch toolName {
	case "get_historical_data":
		return te.executeGetHistoricalData(ctx, params)
	case "calculate_rsi":
		return te.executeCalculateRSI(ctx, params)
	case "calculate_bollinger_bands":
		return te.executeCalculateBollingerBands(ctx, params)
	case "calculate_fibonacci_levels":
		return te.executeCalculateFibonacci(ctx, params)
	case "get_support_resistance":
		return te.executeGetSupportResistance(ctx, params)
	case "detect_candlestick_patterns":
		return te.executeDetectCandlestickPatterns(ctx, params)
	case "detect_chart_patterns":
		return te.executeDetectChartPatterns(ctx, params)
	case "calculate_atr":
		return te.executeCalculateATR(ctx, params)
	case "calculate_stochastic":
		return te.executeCalculateStochastic(ctx, params)
	case "get_mtf_analysis":
		return te.executeGetMTFAnalysis(ctx, params)
	case "calculate_macd":
		return te.executeCalculateMACD(ctx, params)
	case "calculate_ema_crossover":
		return te.executeCalculateEMACrossover(ctx, params)
	case "calculate_adx":
		return te.executeCalculateADX(ctx, params)
	case "analyze_volume":
		return te.executeAnalyzeVolume(ctx, params)
	case "calculate_vwap":
		return te.executeCalculateVWAP(ctx, params)
	default:
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
}

// Helper to get string param with default
func getStringParam(params map[string]interface{}, key, defaultVal string) string {
	if v, ok := params[key].(string); ok {
		return v
	}
	return defaultVal
}

// Helper to get int param with default
func getIntParam(params map[string]interface{}, key string, defaultVal int) int {
	if v, ok := params[key].(float64); ok {
		return int(v)
	}
	return defaultVal
}

// Helper to get float param with default
func getFloatParam(params map[string]interface{}, key string, defaultVal float64) float64 {
	if v, ok := params[key].(float64); ok {
		return v
	}
	return defaultVal
}

// mapTimeframe maps user-friendly timeframe to broker format
// Note: The broker's GetHistorical expects the short format (15min, not 15minute)
func mapTimeframe(tf string) string {
	// Return as-is since broker handles the conversion
	switch tf {
	case "1min", "5min", "15min", "30min", "1hour", "1day":
		return tf
	case "minute":
		return "1min"
	case "5minute":
		return "5min"
	case "15minute":
		return "15min"
	case "30minute":
		return "30min"
	case "60minute":
		return "1hour"
	case "day":
		return "1day"
	default:
		return "15min"
	}
}

// fetchCandles fetches historical candles for a symbol
func (te *ToolExecutor) fetchCandles(ctx context.Context, symbol, timeframe string, periods int) ([]models.Candle, error) {
	// Calculate time range based on timeframe and periods
	// Use a minimum lookback to ensure we have enough data
	now := time.Now()
	var from time.Time
	
	switch timeframe {
	case "1min":
		from = now.Add(-time.Duration(periods) * time.Minute)
	case "5min":
		from = now.Add(-time.Duration(periods*5) * time.Minute)
	case "15min":
		// Ensure at least 10 days of data for 15min candles to get enough for indicators
		minDays := 10
		calculatedFrom := now.Add(-time.Duration(periods*15) * time.Minute)
		minFrom := now.AddDate(0, 0, -minDays)
		if calculatedFrom.After(minFrom) {
			from = minFrom
		} else {
			from = calculatedFrom
		}
	case "30min":
		// Ensure at least 10 days
		minDays := 10
		calculatedFrom := now.Add(-time.Duration(periods*30) * time.Minute)
		minFrom := now.AddDate(0, 0, -minDays)
		if calculatedFrom.After(minFrom) {
			from = minFrom
		} else {
			from = calculatedFrom
		}
	case "1hour":
		// Ensure at least 15 days
		minDays := 15
		calculatedFrom := now.Add(-time.Duration(periods) * time.Hour)
		minFrom := now.AddDate(0, 0, -minDays)
		if calculatedFrom.After(minFrom) {
			from = minFrom
		} else {
			from = calculatedFrom
		}
	case "1day":
		from = now.AddDate(0, 0, -periods)
	default:
		from = now.AddDate(0, 0, -10) // Default to 10 days
	}

	return te.broker.GetHistorical(ctx, broker.HistoricalRequest{
		Symbol:    symbol,
		Exchange:  models.NSE,
		Timeframe: mapTimeframe(timeframe),
		From:      from,
		To:        now,
	})
}

// executeGetHistoricalData fetches historical OHLCV data
func (te *ToolExecutor) executeGetHistoricalData(ctx context.Context, params map[string]interface{}) (string, error) {
	symbol := strings.ToUpper(getStringParam(params, "symbol", ""))
	timeframe := getStringParam(params, "timeframe", "15min")
	periods := getIntParam(params, "periods", 100)
	
	if symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}
	if periods > 500 {
		periods = 500
	}

	candles, err := te.fetchCandles(ctx, symbol, timeframe, periods)
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	// Format response with recent candles
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Historical data for %s (%s timeframe, %d candles):\n\n", symbol, timeframe, len(candles)))
	
	// Show last 20 candles in detail
	start := 0
	if len(candles) > 20 {
		start = len(candles) - 20
	}
	
	sb.WriteString("Recent candles:\n")
	for i := start; i < len(candles); i++ {
		c := candles[i]
		change := ((c.Close - c.Open) / c.Open) * 100
		sb.WriteString(fmt.Sprintf("  %s: O=%.2f H=%.2f L=%.2f C=%.2f V=%d (%.2f%%)\n",
			c.Timestamp.Format("2006-01-02 15:04"), c.Open, c.High, c.Low, c.Close, c.Volume, change))
	}
	
	// Add summary stats
	if len(candles) > 0 {
		first := candles[0]
		last := candles[len(candles)-1]
		high := first.High
		low := first.Low
		totalVol := int64(0)
		
		for _, c := range candles {
			if c.High > high {
				high = c.High
			}
			if c.Low < low {
				low = c.Low
			}
			totalVol += c.Volume
		}
		
		sb.WriteString(fmt.Sprintf("\nSummary:\n"))
		sb.WriteString(fmt.Sprintf("  Period High: %.2f\n", high))
		sb.WriteString(fmt.Sprintf("  Period Low: %.2f\n", low))
		sb.WriteString(fmt.Sprintf("  Current Price: %.2f\n", last.Close))
		sb.WriteString(fmt.Sprintf("  Total Volume: %d\n", totalVol))
		sb.WriteString(fmt.Sprintf("  Price Change: %.2f%%\n", ((last.Close-first.Open)/first.Open)*100))
	}

	return sb.String(), nil
}

// executeCalculateRSI calculates RSI
func (te *ToolExecutor) executeCalculateRSI(ctx context.Context, params map[string]interface{}) (string, error) {
	symbol := strings.ToUpper(getStringParam(params, "symbol", ""))
	timeframe := getStringParam(params, "timeframe", "15min")
	period := getIntParam(params, "period", 14)
	
	if symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}

	candles, err := te.fetchCandles(ctx, symbol, timeframe, period*3)
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	rsi := indicators.NewRSI(period)
	values, err := rsi.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate RSI: %w", err)
	}

	// Get current and recent RSI values
	current := values[len(values)-1]
	prev := values[len(values)-2]
	
	var signal string
	if current > 70 {
		signal = "OVERBOUGHT - potential sell signal"
	} else if current < 30 {
		signal = "OVERSOLD - potential buy signal"
	} else if current > 50 {
		signal = "BULLISH momentum"
	} else {
		signal = "BEARISH momentum"
	}
	
	trend := "rising"
	if current < prev {
		trend = "falling"
	}

	return fmt.Sprintf("RSI(%d) for %s (%s):\n  Current: %.2f (%s)\n  Previous: %.2f\n  Trend: %s\n  Signal: %s",
		period, symbol, timeframe, current, signal, prev, trend, signal), nil
}

// executeCalculateBollingerBands calculates Bollinger Bands
func (te *ToolExecutor) executeCalculateBollingerBands(ctx context.Context, params map[string]interface{}) (string, error) {
	symbol := strings.ToUpper(getStringParam(params, "symbol", ""))
	timeframe := getStringParam(params, "timeframe", "15min")
	period := getIntParam(params, "period", 20)
	stdDev := getFloatParam(params, "std_dev", 2.0)
	
	if symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}

	candles, err := te.fetchCandles(ctx, symbol, timeframe, period*2)
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	bb := indicators.NewBollingerBands(period, stdDev)
	values, err := bb.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate Bollinger Bands: %w", err)
	}

	n := len(candles) - 1
	upper := values["upper"][n]
	middle := values["middle"][n]
	lower := values["lower"][n]
	percentB := values["percent_b"][n]
	bandwidth := values["bandwidth"][n]
	currentPrice := candles[n].Close

	var position string
	if percentB > 1 {
		position = "ABOVE upper band - overbought"
	} else if percentB < 0 {
		position = "BELOW lower band - oversold"
	} else if percentB > 0.8 {
		position = "Near upper band - potential resistance"
	} else if percentB < 0.2 {
		position = "Near lower band - potential support"
	} else {
		position = "Within bands - neutral"
	}

	return fmt.Sprintf("Bollinger Bands(%d, %.1f) for %s (%s):\n  Upper Band: %.2f\n  Middle Band: %.2f\n  Lower Band: %.2f\n  Current Price: %.2f\n  %%B: %.2f\n  Bandwidth: %.4f\n  Position: %s",
		period, stdDev, symbol, timeframe, upper, middle, lower, currentPrice, percentB, bandwidth, position), nil
}


// executeCalculateFibonacci calculates Fibonacci levels
func (te *ToolExecutor) executeCalculateFibonacci(ctx context.Context, params map[string]interface{}) (string, error) {
	symbol := strings.ToUpper(getStringParam(params, "symbol", ""))
	timeframe := getStringParam(params, "timeframe", "15min")
	lookback := getIntParam(params, "lookback", 50)
	
	if symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}

	candles, err := te.fetchCandles(ctx, symbol, timeframe, lookback)
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	fib := indicators.NewFibonacciRetracement(lookback)
	levels, err := fib.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate Fibonacci: %w", err)
	}

	currentPrice := candles[len(candles)-1].Close
	trend := "UPTREND"
	if !levels.IsUptrend {
		trend = "DOWNTREND"
	}

	// Find nearest levels
	var nearestSupport, nearestResistance float64
	allLevels := []float64{levels.Level236, levels.Level382, levels.Level500, levels.Level618, levels.Level786}
	
	for _, lvl := range allLevels {
		if lvl < currentPrice && lvl > nearestSupport {
			nearestSupport = lvl
		}
		if lvl > currentPrice && (nearestResistance == 0 || lvl < nearestResistance) {
			nearestResistance = lvl
		}
	}

	return fmt.Sprintf("Fibonacci Levels for %s (%s, %d period lookback):\n  Trend: %s\n  Swing High: %.2f\n  Swing Low: %.2f\n  Current Price: %.2f\n\n  Retracement Levels:\n    0%% (Start): %.2f\n    23.6%%: %.2f\n    38.2%%: %.2f\n    50.0%%: %.2f\n    61.8%%: %.2f\n    78.6%%: %.2f\n    100%% (End): %.2f\n\n  Extension Levels:\n    127.2%%: %.2f\n    161.8%%: %.2f\n\n  Nearest Support: %.2f\n  Nearest Resistance: %.2f",
		symbol, timeframe, lookback, trend, levels.SwingHigh, levels.SwingLow, currentPrice,
		levels.Level0, levels.Level236, levels.Level382, levels.Level500, levels.Level618, levels.Level786, levels.Level1000,
		levels.Level1272, levels.Level1618, nearestSupport, nearestResistance), nil
}

// executeGetSupportResistance calculates pivot points
func (te *ToolExecutor) executeGetSupportResistance(ctx context.Context, params map[string]interface{}) (string, error) {
	symbol := strings.ToUpper(getStringParam(params, "symbol", ""))
	timeframe := getStringParam(params, "timeframe", "1day")
	
	if symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}

	// Fetch enough data for pivot calculation
	candles, err := te.fetchCandles(ctx, symbol, timeframe, 5)
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	if len(candles) < 2 {
		return "", fmt.Errorf("insufficient data for pivot calculation")
	}

	// Use previous candle for pivot calculation
	prevCandle := candles[len(candles)-2]
	currentPrice := candles[len(candles)-1].Close

	// Calculate standard pivot points
	pivot := indicators.NewStandardPivotPoints()
	levels := pivot.CalculateFromCandle(prevCandle)

	// Calculate Camarilla pivots too
	camarilla := indicators.NewCamarillaPivotPoints()
	camLevels := camarilla.Calculate(prevCandle.High, prevCandle.Low, prevCandle.Close)

	var position string
	if currentPrice > levels.R1 {
		position = "Above R1 - bullish"
	} else if currentPrice < levels.S1 {
		position = "Below S1 - bearish"
	} else if currentPrice > levels.Pivot {
		position = "Above Pivot - mildly bullish"
	} else {
		position = "Below Pivot - mildly bearish"
	}

	return fmt.Sprintf("Support/Resistance for %s (based on %s data):\n\nStandard Pivot Points:\n  R3: %.2f\n  R2: %.2f\n  R1: %.2f\n  Pivot: %.2f\n  S1: %.2f\n  S2: %.2f\n  S3: %.2f\n\nCamarilla Pivots:\n  R3: %.2f\n  R2: %.2f\n  R1: %.2f\n  S1: %.2f\n  S2: %.2f\n  S3: %.2f\n\nCurrent Price: %.2f\nPosition: %s",
		symbol, timeframe,
		levels.R3, levels.R2, levels.R1, levels.Pivot, levels.S1, levels.S2, levels.S3,
		camLevels.R3, camLevels.R2, camLevels.R1, camLevels.S1, camLevels.S2, camLevels.S3,
		currentPrice, position), nil
}

// executeDetectCandlestickPatterns detects candlestick patterns
func (te *ToolExecutor) executeDetectCandlestickPatterns(ctx context.Context, params map[string]interface{}) (string, error) {
	symbol := strings.ToUpper(getStringParam(params, "symbol", ""))
	timeframe := getStringParam(params, "timeframe", "15min")
	
	if symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}

	candles, err := te.fetchCandles(ctx, symbol, timeframe, 50)
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	patterns, err := te.candleDetector.Detect(candles)
	if err != nil {
		return "", fmt.Errorf("failed to detect patterns: %w", err)
	}

	if len(patterns) == 0 {
		return fmt.Sprintf("No candlestick patterns detected for %s (%s)", symbol, timeframe), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Candlestick Patterns for %s (%s):\n\n", symbol, timeframe))

	// Group by recent patterns (last 10 candles)
	recentThreshold := len(candles) - 10
	var recentPatterns, olderPatterns []analysis.Pattern
	
	for _, p := range patterns {
		if p.EndIndex >= recentThreshold {
			recentPatterns = append(recentPatterns, p)
		} else {
			olderPatterns = append(olderPatterns, p)
		}
	}

	if len(recentPatterns) > 0 {
		sb.WriteString("RECENT PATTERNS (last 10 candles):\n")
		for _, p := range recentPatterns {
			direction := "Neutral"
			if p.Direction == analysis.PatternBullish {
				direction = "BULLISH ↑"
			} else if p.Direction == analysis.PatternBearish {
				direction = "BEARISH ↓"
			}
			volumeStr := ""
			if p.VolumeConfirm {
				volumeStr = " [Volume Confirmed]"
			}
			sb.WriteString(fmt.Sprintf("  • %s - %s (Strength: %.0f%%)%s\n", p.Name, direction, p.Strength*100, volumeStr))
		}
	}

	if len(olderPatterns) > 0 && len(olderPatterns) <= 10 {
		sb.WriteString("\nOLDER PATTERNS:\n")
		for _, p := range olderPatterns {
			direction := "Neutral"
			if p.Direction == analysis.PatternBullish {
				direction = "Bullish"
			} else if p.Direction == analysis.PatternBearish {
				direction = "Bearish"
			}
			sb.WriteString(fmt.Sprintf("  • %s - %s (Strength: %.0f%%)\n", p.Name, direction, p.Strength*100))
		}
	}

	// Summary
	bullish := 0
	bearish := 0
	for _, p := range recentPatterns {
		if p.Direction == analysis.PatternBullish {
			bullish++
		} else if p.Direction == analysis.PatternBearish {
			bearish++
		}
	}
	
	sb.WriteString(fmt.Sprintf("\nSummary: %d bullish, %d bearish patterns in recent candles", bullish, bearish))

	return sb.String(), nil
}

// executeDetectChartPatterns detects chart patterns
func (te *ToolExecutor) executeDetectChartPatterns(ctx context.Context, params map[string]interface{}) (string, error) {
	symbol := strings.ToUpper(getStringParam(params, "symbol", ""))
	timeframe := getStringParam(params, "timeframe", "1hour")
	
	if symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}

	candles, err := te.fetchCandles(ctx, symbol, timeframe, 100)
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	patterns, err := te.chartDetector.Detect(candles)
	if err != nil {
		return "", fmt.Errorf("failed to detect patterns: %w", err)
	}

	if len(patterns) == 0 {
		return fmt.Sprintf("No chart patterns detected for %s (%s). Chart patterns require more data and clear price structures.", symbol, timeframe), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Chart Patterns for %s (%s):\n\n", symbol, timeframe))

	for _, p := range patterns {
		direction := "Neutral"
		if p.Direction == analysis.PatternBullish {
			direction = "BULLISH ↑"
		} else if p.Direction == analysis.PatternBearish {
			direction = "BEARISH ↓"
		}
		
		sb.WriteString(fmt.Sprintf("• %s\n", p.Name))
		sb.WriteString(fmt.Sprintf("  Direction: %s\n", direction))
		sb.WriteString(fmt.Sprintf("  Strength: %.0f%%\n", p.Strength*100))
		if p.TargetPrice > 0 {
			sb.WriteString(fmt.Sprintf("  Target Price: %.2f\n", p.TargetPrice))
		}
		if p.Completion > 0 {
			sb.WriteString(fmt.Sprintf("  Completion: %.0f%%\n", p.Completion*100))
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}


// executeCalculateATR calculates ATR
func (te *ToolExecutor) executeCalculateATR(ctx context.Context, params map[string]interface{}) (string, error) {
	symbol := strings.ToUpper(getStringParam(params, "symbol", ""))
	timeframe := getStringParam(params, "timeframe", "15min")
	period := getIntParam(params, "period", 14)
	
	if symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}

	candles, err := te.fetchCandles(ctx, symbol, timeframe, period*3)
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	atr := indicators.NewATR(period)
	values, err := atr.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate ATR: %w", err)
	}

	current := values[len(values)-1]
	prev := values[len(values)-2]
	currentPrice := candles[len(candles)-1].Close
	atrPercent := (current / currentPrice) * 100

	// Suggest stop loss levels
	longSL := currentPrice - (current * 1.5)
	shortSL := currentPrice + (current * 1.5)

	trend := "increasing (higher volatility)"
	if current < prev {
		trend = "decreasing (lower volatility)"
	}

	return fmt.Sprintf("ATR(%d) for %s (%s):\n  Current ATR: %.2f (%.2f%% of price)\n  Previous ATR: %.2f\n  Trend: %s\n\nSuggested Stop Loss Levels (1.5x ATR):\n  For LONG: %.2f (%.2f%% below current)\n  For SHORT: %.2f (%.2f%% above current)\n\nCurrent Price: %.2f",
		period, symbol, timeframe, current, atrPercent, prev, trend,
		longSL, ((currentPrice-longSL)/currentPrice)*100,
		shortSL, ((shortSL-currentPrice)/currentPrice)*100,
		currentPrice), nil
}

// executeCalculateStochastic calculates Stochastic Oscillator
func (te *ToolExecutor) executeCalculateStochastic(ctx context.Context, params map[string]interface{}) (string, error) {
	symbol := strings.ToUpper(getStringParam(params, "symbol", ""))
	timeframe := getStringParam(params, "timeframe", "15min")
	kPeriod := getIntParam(params, "k_period", 14)
	dPeriod := getIntParam(params, "d_period", 3)
	
	if symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}

	candles, err := te.fetchCandles(ctx, symbol, timeframe, (kPeriod+dPeriod)*3)
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	stoch := indicators.NewStochastic(kPeriod, dPeriod, 3)
	values, err := stoch.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate Stochastic: %w", err)
	}

	n := len(candles) - 1
	percentK := values["percent_k"][n]
	percentD := values["percent_d"][n]
	prevK := values["percent_k"][n-1]
	prevD := values["percent_d"][n-1]

	var signal string
	if percentK > 80 && percentD > 80 {
		signal = "OVERBOUGHT - potential sell signal"
	} else if percentK < 20 && percentD < 20 {
		signal = "OVERSOLD - potential buy signal"
	} else if percentK > percentD && prevK <= prevD {
		signal = "BULLISH CROSSOVER - %K crossed above %D"
	} else if percentK < percentD && prevK >= prevD {
		signal = "BEARISH CROSSOVER - %K crossed below %D"
	} else if percentK > percentD {
		signal = "Bullish momentum (%K above %D)"
	} else {
		signal = "Bearish momentum (%K below %D)"
	}

	return fmt.Sprintf("Stochastic(%d,%d) for %s (%s):\n  %%K: %.2f\n  %%D: %.2f\n  Previous %%K: %.2f\n  Previous %%D: %.2f\n\n  Signal: %s",
		kPeriod, dPeriod, symbol, timeframe, percentK, percentD, prevK, prevD, signal), nil
}

// executeGetMTFAnalysis performs multi-timeframe analysis
func (te *ToolExecutor) executeGetMTFAnalysis(ctx context.Context, params map[string]interface{}) (string, error) {
	symbol := strings.ToUpper(getStringParam(params, "symbol", ""))
	
	if symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}

	timeframes := []string{"5min", "15min", "1hour", "1day"}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Multi-Timeframe Analysis for %s:\n\n", symbol))

	for _, tf := range timeframes {
		candles, err := te.fetchCandles(ctx, symbol, tf, 50)
		if err != nil {
			sb.WriteString(fmt.Sprintf("%s: Error fetching data\n", tf))
			continue
		}

		if len(candles) < 20 {
			sb.WriteString(fmt.Sprintf("%s: Insufficient data\n", tf))
			continue
		}

		// Calculate RSI
		rsi := indicators.NewRSI(14)
		rsiValues, _ := rsi.Calculate(candles)
		currentRSI := rsiValues[len(rsiValues)-1]

		// Calculate trend (simple: compare current close to 20-period SMA)
		closes := make([]float64, len(candles))
		for i, c := range candles {
			closes[i] = c.Close
		}
		sma20 := mean(closes[len(closes)-20:])
		currentPrice := candles[len(candles)-1].Close

		trend := "BULLISH ↑"
		if currentPrice < sma20 {
			trend = "BEARISH ↓"
		}

		// Momentum
		momentum := "Neutral"
		if currentRSI > 60 {
			momentum = "Strong"
		} else if currentRSI < 40 {
			momentum = "Weak"
		}

		sb.WriteString(fmt.Sprintf("%s:\n  Trend: %s (Price: %.2f, SMA20: %.2f)\n  RSI: %.1f (%s momentum)\n\n",
			tf, trend, currentPrice, sma20, currentRSI, momentum))
	}

	// Overall assessment
	sb.WriteString("Note: Look for alignment across timeframes for higher probability trades.")

	return sb.String(), nil
}

// Helper function for mean calculation
func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}


// executeCalculateMACD calculates MACD indicator
func (te *ToolExecutor) executeCalculateMACD(ctx context.Context, params map[string]interface{}) (string, error) {
	symbol := strings.ToUpper(getStringParam(params, "symbol", ""))
	timeframe := getStringParam(params, "timeframe", "15min")
	fastPeriod := getIntParam(params, "fast_period", 12)
	slowPeriod := getIntParam(params, "slow_period", 26)
	signalPeriod := getIntParam(params, "signal_period", 9)
	
	if symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}

	candles, err := te.fetchCandles(ctx, symbol, timeframe, slowPeriod*3)
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	macd := indicators.NewMACD(fastPeriod, slowPeriod, signalPeriod)
	values, err := macd.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate MACD: %w", err)
	}

	n := len(candles) - 1
	macdLine := values["macd"][n]
	signalLine := values["signal"][n]
	histogram := values["histogram"][n]
	prevMacd := values["macd"][n-1]
	prevSignal := values["signal"][n-1]
	prevHistogram := values["histogram"][n-1]

	var signal string
	if macdLine > signalLine && prevMacd <= prevSignal {
		signal = "BULLISH CROSSOVER - MACD crossed above Signal line (BUY signal)"
	} else if macdLine < signalLine && prevMacd >= prevSignal {
		signal = "BEARISH CROSSOVER - MACD crossed below Signal line (SELL signal)"
	} else if macdLine > signalLine && macdLine > 0 {
		signal = "STRONG BULLISH - MACD above signal and above zero"
	} else if macdLine < signalLine && macdLine < 0 {
		signal = "STRONG BEARISH - MACD below signal and below zero"
	} else if macdLine > signalLine {
		signal = "BULLISH - MACD above signal line"
	} else {
		signal = "BEARISH - MACD below signal line"
	}

	histogramTrend := "expanding"
	if abs(histogram) < abs(prevHistogram) {
		histogramTrend = "contracting (momentum weakening)"
	}

	return fmt.Sprintf("MACD(%d,%d,%d) for %s (%s):\n  MACD Line: %.4f\n  Signal Line: %.4f\n  Histogram: %.4f (%s)\n  Previous Histogram: %.4f\n\n  Signal: %s",
		fastPeriod, slowPeriod, signalPeriod, symbol, timeframe,
		macdLine, signalLine, histogram, histogramTrend, prevHistogram, signal), nil
}

// executeCalculateEMACrossover calculates EMA crossover signals
func (te *ToolExecutor) executeCalculateEMACrossover(ctx context.Context, params map[string]interface{}) (string, error) {
	symbol := strings.ToUpper(getStringParam(params, "symbol", ""))
	timeframe := getStringParam(params, "timeframe", "15min")
	fastPeriod := getIntParam(params, "fast_period", 9)
	slowPeriod := getIntParam(params, "slow_period", 21)
	
	if symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}

	candles, err := te.fetchCandles(ctx, symbol, timeframe, slowPeriod*3)
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	fastEMA := indicators.NewEMA(fastPeriod)
	slowEMA := indicators.NewEMA(slowPeriod)
	
	fastValues, err := fastEMA.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate fast EMA: %w", err)
	}
	
	slowValues, err := slowEMA.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate slow EMA: %w", err)
	}

	n := len(candles) - 1
	currentFast := fastValues[n]
	currentSlow := slowValues[n]
	prevFast := fastValues[n-1]
	prevSlow := slowValues[n-1]
	currentPrice := candles[n].Close

	var signal string
	var trend string
	
	if currentFast > currentSlow && prevFast <= prevSlow {
		signal = "GOLDEN CROSS - Fast EMA crossed above Slow EMA (BULLISH)"
		trend = "BULLISH ↑"
	} else if currentFast < currentSlow && prevFast >= prevSlow {
		signal = "DEATH CROSS - Fast EMA crossed below Slow EMA (BEARISH)"
		trend = "BEARISH ↓"
	} else if currentFast > currentSlow {
		signal = "Bullish trend - Fast EMA above Slow EMA"
		trend = "BULLISH ↑"
	} else {
		signal = "Bearish trend - Fast EMA below Slow EMA"
		trend = "BEARISH ↓"
	}

	// Calculate distance from EMAs
	distFromFast := ((currentPrice - currentFast) / currentFast) * 100
	distFromSlow := ((currentPrice - currentSlow) / currentSlow) * 100

	return fmt.Sprintf("EMA Crossover (EMA%d/EMA%d) for %s (%s):\n  Fast EMA(%d): %.2f\n  Slow EMA(%d): %.2f\n  Current Price: %.2f\n  Price vs Fast EMA: %.2f%%\n  Price vs Slow EMA: %.2f%%\n\n  Trend: %s\n  Signal: %s",
		fastPeriod, slowPeriod, symbol, timeframe,
		fastPeriod, currentFast, slowPeriod, currentSlow,
		currentPrice, distFromFast, distFromSlow, trend, signal), nil
}

// executeCalculateADX calculates ADX for trend strength
func (te *ToolExecutor) executeCalculateADX(ctx context.Context, params map[string]interface{}) (string, error) {
	symbol := strings.ToUpper(getStringParam(params, "symbol", ""))
	timeframe := getStringParam(params, "timeframe", "15min")
	period := getIntParam(params, "period", 14)
	
	if symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}

	candles, err := te.fetchCandles(ctx, symbol, timeframe, period*4)
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	adx := indicators.NewADX(period)
	values, err := adx.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate ADX: %w", err)
	}

	n := len(candles) - 1
	adxValue := values["adx"][n]
	plusDI := values["plus_di"][n]
	minusDI := values["minus_di"][n]
	prevADX := values["adx"][n-1]

	var trendStrength string
	if adxValue > 50 {
		trendStrength = "VERY STRONG trend"
	} else if adxValue > 25 {
		trendStrength = "STRONG trend"
	} else if adxValue > 20 {
		trendStrength = "MODERATE trend"
	} else {
		trendStrength = "WEAK/RANGING market (avoid trend-following)"
	}

	var direction string
	if plusDI > minusDI {
		direction = "BULLISH (+DI > -DI)"
	} else {
		direction = "BEARISH (-DI > +DI)"
	}

	adxTrend := "strengthening"
	if adxValue < prevADX {
		adxTrend = "weakening"
	}

	return fmt.Sprintf("ADX(%d) for %s (%s):\n  ADX: %.2f (%s)\n  +DI: %.2f\n  -DI: %.2f\n  Previous ADX: %.2f\n  ADX Trend: %s\n\n  Direction: %s\n  Strength: %s",
		period, symbol, timeframe, adxValue, trendStrength, plusDI, minusDI, prevADX, adxTrend, direction, trendStrength), nil
}

// executeAnalyzeVolume analyzes volume patterns
func (te *ToolExecutor) executeAnalyzeVolume(ctx context.Context, params map[string]interface{}) (string, error) {
	symbol := strings.ToUpper(getStringParam(params, "symbol", ""))
	timeframe := getStringParam(params, "timeframe", "15min")
	
	if symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}

	candles, err := te.fetchCandles(ctx, symbol, timeframe, 50)
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	if len(candles) < 20 {
		return "", fmt.Errorf("insufficient data for volume analysis")
	}

	// Calculate average volume (20 period)
	var totalVol int64
	for i := len(candles) - 21; i < len(candles)-1; i++ {
		totalVol += candles[i].Volume
	}
	avgVolume := float64(totalVol) / 20.0

	currentCandle := candles[len(candles)-1]
	currentVolume := float64(currentCandle.Volume)
	volumeRatio := currentVolume / avgVolume

	// Analyze recent volume trend
	var recentVolSum int64
	for i := len(candles) - 5; i < len(candles); i++ {
		recentVolSum += candles[i].Volume
	}
	recentAvg := float64(recentVolSum) / 5.0
	
	var olderVolSum int64
	for i := len(candles) - 10; i < len(candles)-5; i++ {
		olderVolSum += candles[i].Volume
	}
	olderAvg := float64(olderVolSum) / 5.0

	volumeTrend := "INCREASING"
	if recentAvg < olderAvg*0.9 {
		volumeTrend = "DECREASING"
	} else if recentAvg < olderAvg*1.1 {
		volumeTrend = "STABLE"
	}

	// Volume signal
	var signal string
	priceChange := currentCandle.Close - currentCandle.Open
	if volumeRatio > 2.0 && priceChange > 0 {
		signal = "STRONG BULLISH - High volume on up move (institutional buying)"
	} else if volumeRatio > 2.0 && priceChange < 0 {
		signal = "STRONG BEARISH - High volume on down move (institutional selling)"
	} else if volumeRatio > 1.5 && priceChange > 0 {
		signal = "BULLISH - Above average volume on up move"
	} else if volumeRatio > 1.5 && priceChange < 0 {
		signal = "BEARISH - Above average volume on down move"
	} else if volumeRatio < 0.5 {
		signal = "LOW VOLUME - Weak conviction, be cautious"
	} else {
		signal = "NEUTRAL - Normal volume"
	}

	return fmt.Sprintf("Volume Analysis for %s (%s):\n  Current Volume: %d\n  20-Period Avg Volume: %.0f\n  Volume Ratio: %.2fx average\n  Recent Volume Trend: %s\n\n  Signal: %s\n\n  Interpretation:\n  - Volume > 1.5x avg with price up = Bullish confirmation\n  - Volume > 1.5x avg with price down = Bearish confirmation\n  - Low volume moves are often unreliable",
		symbol, timeframe, currentCandle.Volume, avgVolume, volumeRatio, volumeTrend, signal), nil
}

// executeCalculateVWAP calculates VWAP
func (te *ToolExecutor) executeCalculateVWAP(ctx context.Context, params map[string]interface{}) (string, error) {
	symbol := strings.ToUpper(getStringParam(params, "symbol", ""))
	timeframe := getStringParam(params, "timeframe", "15min")
	
	if symbol == "" {
		return "", fmt.Errorf("symbol is required")
	}

	candles, err := te.fetchCandles(ctx, symbol, timeframe, 50)
	if err != nil {
		return "", fmt.Errorf("failed to fetch data: %w", err)
	}

	if len(candles) < 10 {
		return "", fmt.Errorf("insufficient data for VWAP calculation")
	}

	// Calculate VWAP for today's session (or available data)
	var cumulativeTPV float64 // Typical Price * Volume
	var cumulativeVolume int64

	for _, c := range candles {
		typicalPrice := (c.High + c.Low + c.Close) / 3
		cumulativeTPV += typicalPrice * float64(c.Volume)
		cumulativeVolume += c.Volume
	}

	vwap := cumulativeTPV / float64(cumulativeVolume)
	currentPrice := candles[len(candles)-1].Close
	deviation := ((currentPrice - vwap) / vwap) * 100

	var signal string
	if currentPrice > vwap*1.01 {
		signal = "BULLISH - Price significantly above VWAP (buyers in control)"
	} else if currentPrice > vwap {
		signal = "MILDLY BULLISH - Price above VWAP"
	} else if currentPrice < vwap*0.99 {
		signal = "BEARISH - Price significantly below VWAP (sellers in control)"
	} else if currentPrice < vwap {
		signal = "MILDLY BEARISH - Price below VWAP"
	} else {
		signal = "NEUTRAL - Price at VWAP"
	}

	return fmt.Sprintf("VWAP for %s (%s):\n  VWAP: %.2f\n  Current Price: %.2f\n  Deviation: %.2f%%\n\n  Signal: %s\n\n  Usage:\n  - Price above VWAP = Bullish bias\n  - Price below VWAP = Bearish bias\n  - VWAP often acts as support/resistance",
		symbol, timeframe, vwap, currentPrice, deviation, signal), nil
}

// Backtest implementations for new tools

// executeCalculateMACD calculates MACD on cached candles
func (bte *BacktestToolExecutor) executeCalculateMACD(candles []models.Candle, params map[string]interface{}) (string, error) {
	fastPeriod := getIntParam(params, "fast_period", 12)
	slowPeriod := getIntParam(params, "slow_period", 26)
	signalPeriod := getIntParam(params, "signal_period", 9)
	
	if len(candles) < slowPeriod+signalPeriod {
		return "", fmt.Errorf("insufficient data for MACD calculation")
	}

	macd := indicators.NewMACD(fastPeriod, slowPeriod, signalPeriod)
	values, err := macd.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate MACD: %w", err)
	}

	n := len(candles) - 1
	macdLine := values["macd"][n]
	signalLine := values["signal"][n]
	histogram := values["histogram"][n]
	prevMacd := values["macd"][n-1]
	prevSignal := values["signal"][n-1]
	prevHistogram := values["histogram"][n-1]

	var signal string
	if macdLine > signalLine && prevMacd <= prevSignal {
		signal = "BULLISH CROSSOVER - MACD crossed above Signal line (BUY signal)"
	} else if macdLine < signalLine && prevMacd >= prevSignal {
		signal = "BEARISH CROSSOVER - MACD crossed below Signal line (SELL signal)"
	} else if macdLine > signalLine && macdLine > 0 {
		signal = "STRONG BULLISH - MACD above signal and above zero"
	} else if macdLine < signalLine && macdLine < 0 {
		signal = "STRONG BEARISH - MACD below signal and below zero"
	} else if macdLine > signalLine {
		signal = "BULLISH - MACD above signal line"
	} else {
		signal = "BEARISH - MACD below signal line"
	}

	histogramTrend := "expanding"
	if abs(histogram) < abs(prevHistogram) {
		histogramTrend = "contracting (momentum weakening)"
	}

	return fmt.Sprintf("MACD(%d,%d,%d) for %s (15min):\n  MACD Line: %.4f\n  Signal Line: %.4f\n  Histogram: %.4f (%s)\n  Previous Histogram: %.4f\n\n  Signal: %s",
		fastPeriod, slowPeriod, signalPeriod, bte.symbol,
		macdLine, signalLine, histogram, histogramTrend, prevHistogram, signal), nil
}

// executeCalculateEMACrossover calculates EMA crossover on cached candles
func (bte *BacktestToolExecutor) executeCalculateEMACrossover(candles []models.Candle, params map[string]interface{}) (string, error) {
	fastPeriod := getIntParam(params, "fast_period", 9)
	slowPeriod := getIntParam(params, "slow_period", 21)
	
	if len(candles) < slowPeriod+1 {
		return "", fmt.Errorf("insufficient data for EMA crossover calculation")
	}

	fastEMA := indicators.NewEMA(fastPeriod)
	slowEMA := indicators.NewEMA(slowPeriod)
	
	fastValues, err := fastEMA.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate fast EMA: %w", err)
	}
	
	slowValues, err := slowEMA.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate slow EMA: %w", err)
	}

	n := len(candles) - 1
	currentFast := fastValues[n]
	currentSlow := slowValues[n]
	prevFast := fastValues[n-1]
	prevSlow := slowValues[n-1]
	currentPrice := candles[n].Close

	var signal string
	var trend string
	
	if currentFast > currentSlow && prevFast <= prevSlow {
		signal = "GOLDEN CROSS - Fast EMA crossed above Slow EMA (BULLISH)"
		trend = "BULLISH ↑"
	} else if currentFast < currentSlow && prevFast >= prevSlow {
		signal = "DEATH CROSS - Fast EMA crossed below Slow EMA (BEARISH)"
		trend = "BEARISH ↓"
	} else if currentFast > currentSlow {
		signal = "Bullish trend - Fast EMA above Slow EMA"
		trend = "BULLISH ↑"
	} else {
		signal = "Bearish trend - Fast EMA below Slow EMA"
		trend = "BEARISH ↓"
	}

	distFromFast := ((currentPrice - currentFast) / currentFast) * 100
	distFromSlow := ((currentPrice - currentSlow) / currentSlow) * 100

	return fmt.Sprintf("EMA Crossover (EMA%d/EMA%d) for %s (15min):\n  Fast EMA(%d): %.2f\n  Slow EMA(%d): %.2f\n  Current Price: %.2f\n  Price vs Fast EMA: %.2f%%\n  Price vs Slow EMA: %.2f%%\n\n  Trend: %s\n  Signal: %s",
		fastPeriod, slowPeriod, bte.symbol,
		fastPeriod, currentFast, slowPeriod, currentSlow,
		currentPrice, distFromFast, distFromSlow, trend, signal), nil
}

// executeCalculateADX calculates ADX on cached candles
func (bte *BacktestToolExecutor) executeCalculateADX(candles []models.Candle, params map[string]interface{}) (string, error) {
	period := getIntParam(params, "period", 14)
	
	if len(candles) < period*2+1 {
		return "", fmt.Errorf("insufficient data for ADX calculation")
	}

	adx := indicators.NewADX(period)
	values, err := adx.Calculate(candles)
	if err != nil {
		return "", fmt.Errorf("failed to calculate ADX: %w", err)
	}

	n := len(candles) - 1
	adxValue := values["adx"][n]
	plusDI := values["plus_di"][n]
	minusDI := values["minus_di"][n]
	prevADX := values["adx"][n-1]

	var trendStrength string
	if adxValue > 50 {
		trendStrength = "VERY STRONG trend"
	} else if adxValue > 25 {
		trendStrength = "STRONG trend"
	} else if adxValue > 20 {
		trendStrength = "MODERATE trend"
	} else {
		trendStrength = "WEAK/RANGING market (avoid trend-following)"
	}

	var direction string
	if plusDI > minusDI {
		direction = "BULLISH (+DI > -DI)"
	} else {
		direction = "BEARISH (-DI > +DI)"
	}

	adxTrend := "strengthening"
	if adxValue < prevADX {
		adxTrend = "weakening"
	}

	return fmt.Sprintf("ADX(%d) for %s (15min):\n  ADX: %.2f (%s)\n  +DI: %.2f\n  -DI: %.2f\n  Previous ADX: %.2f\n  ADX Trend: %s\n\n  Direction: %s\n  Strength: %s",
		period, bte.symbol, adxValue, trendStrength, plusDI, minusDI, prevADX, adxTrend, direction, trendStrength), nil
}

// executeAnalyzeVolume analyzes volume on cached candles
func (bte *BacktestToolExecutor) executeAnalyzeVolume(candles []models.Candle, params map[string]interface{}) (string, error) {
	if len(candles) < 20 {
		return "", fmt.Errorf("insufficient data for volume analysis")
	}

	// Calculate average volume (20 period)
	var totalVol int64
	for i := len(candles) - 21; i < len(candles)-1; i++ {
		totalVol += candles[i].Volume
	}
	avgVolume := float64(totalVol) / 20.0

	currentCandle := candles[len(candles)-1]
	currentVolume := float64(currentCandle.Volume)
	volumeRatio := currentVolume / avgVolume

	// Analyze recent volume trend
	var recentVolSum int64
	for i := len(candles) - 5; i < len(candles); i++ {
		recentVolSum += candles[i].Volume
	}
	recentAvg := float64(recentVolSum) / 5.0
	
	var olderVolSum int64
	for i := len(candles) - 10; i < len(candles)-5; i++ {
		olderVolSum += candles[i].Volume
	}
	olderAvg := float64(olderVolSum) / 5.0

	volumeTrend := "INCREASING"
	if recentAvg < olderAvg*0.9 {
		volumeTrend = "DECREASING"
	} else if recentAvg < olderAvg*1.1 {
		volumeTrend = "STABLE"
	}

	// Volume signal
	var signal string
	priceChange := currentCandle.Close - currentCandle.Open
	if volumeRatio > 2.0 && priceChange > 0 {
		signal = "STRONG BULLISH - High volume on up move (institutional buying)"
	} else if volumeRatio > 2.0 && priceChange < 0 {
		signal = "STRONG BEARISH - High volume on down move (institutional selling)"
	} else if volumeRatio > 1.5 && priceChange > 0 {
		signal = "BULLISH - Above average volume on up move"
	} else if volumeRatio > 1.5 && priceChange < 0 {
		signal = "BEARISH - Above average volume on down move"
	} else if volumeRatio < 0.5 {
		signal = "LOW VOLUME - Weak conviction, be cautious"
	} else {
		signal = "NEUTRAL - Normal volume"
	}

	return fmt.Sprintf("Volume Analysis for %s (15min):\n  Current Volume: %d\n  20-Period Avg Volume: %.0f\n  Volume Ratio: %.2fx average\n  Recent Volume Trend: %s\n\n  Signal: %s",
		bte.symbol, currentCandle.Volume, avgVolume, volumeRatio, volumeTrend, signal), nil
}

// executeCalculateVWAP calculates VWAP on cached candles
func (bte *BacktestToolExecutor) executeCalculateVWAP(candles []models.Candle, params map[string]interface{}) (string, error) {
	if len(candles) < 10 {
		return "", fmt.Errorf("insufficient data for VWAP calculation")
	}

	// Calculate VWAP
	var cumulativeTPV float64
	var cumulativeVolume int64

	for _, c := range candles {
		typicalPrice := (c.High + c.Low + c.Close) / 3
		cumulativeTPV += typicalPrice * float64(c.Volume)
		cumulativeVolume += c.Volume
	}

	vwap := cumulativeTPV / float64(cumulativeVolume)
	currentPrice := candles[len(candles)-1].Close
	deviation := ((currentPrice - vwap) / vwap) * 100

	var signal string
	if currentPrice > vwap*1.01 {
		signal = "BULLISH - Price significantly above VWAP (buyers in control)"
	} else if currentPrice > vwap {
		signal = "MILDLY BULLISH - Price above VWAP"
	} else if currentPrice < vwap*0.99 {
		signal = "BEARISH - Price significantly below VWAP (sellers in control)"
	} else if currentPrice < vwap {
		signal = "MILDLY BEARISH - Price below VWAP"
	} else {
		signal = "NEUTRAL - Price at VWAP"
	}

	return fmt.Sprintf("VWAP for %s (15min):\n  VWAP: %.2f\n  Current Price: %.2f\n  Deviation: %.2f%%\n\n  Signal: %s",
		bte.symbol, vwap, currentPrice, deviation, signal), nil
}
