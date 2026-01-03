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
func mapTimeframe(tf string) string {
	switch tf {
	case "1min":
		return "minute"
	case "5min":
		return "5minute"
	case "15min":
		return "15minute"
	case "30min":
		return "30minute"
	case "1hour":
		return "60minute"
	case "1day":
		return "day"
	default:
		return "15minute"
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
