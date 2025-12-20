// Package mtf provides multi-timeframe analysis functionality.
package mtf

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"zerodha-trader/internal/analysis"
	"zerodha-trader/internal/analysis/indicators"
	"zerodha-trader/internal/analysis/patterns"
	"zerodha-trader/internal/analysis/scoring"
	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

// Timeframe represents a trading timeframe.
type Timeframe string

const (
	Timeframe5Min  Timeframe = "5minute"
	Timeframe15Min Timeframe = "15minute"
	Timeframe1Hour Timeframe = "60minute"
	Timeframe1Day  Timeframe = "day"
)

// AllTimeframes returns all supported timeframes for MTF analysis.
func AllTimeframes() []Timeframe {
	return []Timeframe{Timeframe5Min, Timeframe15Min, Timeframe1Hour, Timeframe1Day}
}

// ConfluenceLevel represents the level of timeframe confluence.
type ConfluenceLevel string

const (
	ConfluenceStrong   ConfluenceLevel = "STRONG"   // 4/4 agree
	ConfluenceModerate ConfluenceLevel = "MODERATE" // 3/4 agree
	ConfluenceWeak     ConfluenceLevel = "WEAK"     // 2/4 agree
	ConfluenceNone     ConfluenceLevel = "NONE"     // Less than 2 agree
)

// TimeframeAnalysis contains analysis results for a single timeframe.
type TimeframeAnalysis struct {
	Timeframe      Timeframe
	Trend          patterns.TrendDirection
	TrendStrength  patterns.TrendStrength
	Signal         analysis.SignalRecommendation
	SignalScore    float64
	ADXValue       float64
	RSIValue       float64
	MACDHistogram  float64
	SuperTrendDir  float64 // 1 = bullish, -1 = bearish
	KeyLevels      []analysis.Level
	CandleCount    int
	Error          error
}

// MTFResult contains the complete multi-timeframe analysis result.
type MTFResult struct {
	Symbol         string
	Timeframes     map[Timeframe]*TimeframeAnalysis
	Confluence     ConfluenceLevel
	TrendAlignment bool
	OverallTrend   patterns.TrendDirection
	OverallSignal  analysis.SignalRecommendation
	OverallScore   float64
	BullishCount   int
	BearishCount   int
	NeutralCount   int
}

// Analyzer performs multi-timeframe analysis.
type Analyzer struct {
	broker        broker.Broker
	engine        *indicators.Engine
	trendAnalyzer *patterns.TrendAnalyzer
	scorer        *scoring.SignalScorer
}

// NewAnalyzer creates a new MTF analyzer.
func NewAnalyzer(b broker.Broker) *Analyzer {
	engine := indicators.NewEngine(4)
	
	// Register indicators
	engine.RegisterIndicator(indicators.NewRSI(14))
	engine.RegisterIndicator(indicators.NewEMA(9))
	engine.RegisterIndicator(indicators.NewEMA(21))
	engine.RegisterIndicator(indicators.NewEMA(50))
	engine.RegisterMultiIndicator(indicators.NewMACD(12, 26, 9))
	engine.RegisterMultiIndicator(indicators.NewADX(14))
	engine.RegisterMultiIndicator(indicators.NewSuperTrend(10, 3.0))
	
	return &Analyzer{
		broker:        b,
		engine:        engine,
		trendAnalyzer: patterns.NewTrendAnalyzer(),
		scorer:        scoring.NewSignalScorer(engine),
	}
}

// NewAnalyzerWithEngine creates a new MTF analyzer with a custom indicator engine.
func NewAnalyzerWithEngine(b broker.Broker, engine *indicators.Engine) *Analyzer {
	return &Analyzer{
		broker:        b,
		engine:        engine,
		trendAnalyzer: patterns.NewTrendAnalyzer(),
		scorer:        scoring.NewSignalScorer(engine),
	}
}


// Analyze performs multi-timeframe analysis for a symbol.
// It fetches data and calculates indicators for all timeframes concurrently.
func (a *Analyzer) Analyze(ctx context.Context, symbol string, exchange models.Exchange) (*MTFResult, error) {
	timeframes := AllTimeframes()
	
	result := &MTFResult{
		Symbol:     symbol,
		Timeframes: make(map[Timeframe]*TimeframeAnalysis),
	}
	
	// Fetch and analyze all timeframes concurrently
	var wg sync.WaitGroup
	var mu sync.Mutex
	
	for _, tf := range timeframes {
		wg.Add(1)
		go func(tf Timeframe) {
			defer wg.Done()
			
			analysis, err := a.analyzeTimeframe(ctx, symbol, exchange, tf)
			if err != nil {
				analysis = &TimeframeAnalysis{
					Timeframe: tf,
					Error:     err,
				}
			}
			
			mu.Lock()
			result.Timeframes[tf] = analysis
			mu.Unlock()
		}(tf)
	}
	
	wg.Wait()
	
	// Calculate confluence and alignment
	a.calculateConfluence(result)
	
	return result, nil
}

// AnalyzeWithCandles performs multi-timeframe analysis using provided candle data.
// This is useful when candle data is already available or for testing.
func (a *Analyzer) AnalyzeWithCandles(ctx context.Context, symbol string, candlesByTimeframe map[Timeframe][]models.Candle) (*MTFResult, error) {
	result := &MTFResult{
		Symbol:     symbol,
		Timeframes: make(map[Timeframe]*TimeframeAnalysis),
	}
	
	// Analyze all timeframes concurrently
	var wg sync.WaitGroup
	var mu sync.Mutex
	
	for tf, candles := range candlesByTimeframe {
		wg.Add(1)
		go func(tf Timeframe, candles []models.Candle) {
			defer wg.Done()
			
			analysis := a.analyzeCandles(ctx, tf, candles)
			
			mu.Lock()
			result.Timeframes[tf] = analysis
			mu.Unlock()
		}(tf, candles)
	}
	
	wg.Wait()
	
	// Calculate confluence and alignment
	a.calculateConfluence(result)
	
	return result, nil
}

// analyzeTimeframe fetches data and analyzes a single timeframe.
func (a *Analyzer) analyzeTimeframe(ctx context.Context, symbol string, exchange models.Exchange, tf Timeframe) (*TimeframeAnalysis, error) {
	// Fetch historical data
	req := broker.HistoricalRequest{
		Symbol:    symbol,
		Exchange:  exchange,
		Timeframe: string(tf),
	}
	
	candles, err := a.broker.GetHistorical(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s data: %w", tf, err)
	}
	
	if len(candles) < 50 {
		return nil, fmt.Errorf("insufficient data for %s: got %d candles, need at least 50", tf, len(candles))
	}
	
	return a.analyzeCandles(ctx, tf, candles), nil
}

// analyzeCandles performs analysis on candle data for a timeframe.
func (a *Analyzer) analyzeCandles(ctx context.Context, tf Timeframe, candles []models.Candle) *TimeframeAnalysis {
	result := &TimeframeAnalysis{
		Timeframe:   tf,
		CandleCount: len(candles),
	}
	
	if len(candles) < 50 {
		result.Error = fmt.Errorf("insufficient data: got %d candles, need at least 50", len(candles))
		return result
	}
	
	// Calculate signal score
	signalScore, err := a.scorer.Score(ctx, candles)
	if err == nil {
		result.Signal = signalScore.Recommendation
		result.SignalScore = signalScore.Score
	}
	
	// Analyze trend
	trendResult, err := a.trendAnalyzer.Analyze(candles)
	if err == nil && trendResult != nil {
		result.Trend = trendResult.Direction
		result.TrendStrength = trendResult.Strength
		result.ADXValue = trendResult.ADXValue
	}
	
	// Calculate individual indicators for display
	a.calculateIndicatorValues(candles, result)
	
	return result
}


// calculateIndicatorValues calculates individual indicator values for display.
func (a *Analyzer) calculateIndicatorValues(candles []models.Candle, result *TimeframeAnalysis) {
	n := len(candles)
	
	// RSI
	rsi := indicators.NewRSI(14)
	if rsiValues, err := rsi.Calculate(candles); err == nil && len(rsiValues) > 0 {
		result.RSIValue = getLastNonZero(rsiValues)
	}
	
	// MACD
	macd := indicators.NewMACD(12, 26, 9)
	if macdValues, err := macd.Calculate(candles); err == nil {
		if hist, ok := macdValues["histogram"]; ok && len(hist) > 0 {
			result.MACDHistogram = hist[n-1]
		}
	}
	
	// SuperTrend
	st := indicators.NewSuperTrend(10, 3.0)
	if stValues, err := st.Calculate(candles); err == nil {
		if dir, ok := stValues["direction"]; ok && len(dir) > 0 {
			result.SuperTrendDir = dir[n-1]
		}
	}
}

// calculateConfluence calculates the confluence level and trend alignment.
func (a *Analyzer) calculateConfluence(result *MTFResult) {
	bullish := 0
	bearish := 0
	neutral := 0
	
	for _, analysis := range result.Timeframes {
		if analysis == nil || analysis.Error != nil {
			continue
		}
		
		switch analysis.Trend {
		case patterns.TrendUp:
			bullish++
		case patterns.TrendDown:
			bearish++
		default:
			neutral++
		}
	}
	
	result.BullishCount = bullish
	result.BearishCount = bearish
	result.NeutralCount = neutral
	
	total := bullish + bearish + neutral
	if total == 0 {
		result.Confluence = ConfluenceNone
		return
	}
	
	// Determine confluence level
	maxAgreement := maxInt(bullish, bearish)
	
	switch {
	case maxAgreement == 4:
		result.Confluence = ConfluenceStrong
	case maxAgreement == 3:
		result.Confluence = ConfluenceModerate
	case maxAgreement == 2:
		result.Confluence = ConfluenceWeak
	default:
		result.Confluence = ConfluenceNone
	}
	
	// Determine trend alignment
	result.TrendAlignment = maxAgreement >= 3
	
	// Determine overall trend
	if bullish > bearish {
		result.OverallTrend = patterns.TrendUp
	} else if bearish > bullish {
		result.OverallTrend = patterns.TrendDown
	} else {
		result.OverallTrend = patterns.TrendSideways
	}
	
	// Calculate overall signal and score
	a.calculateOverallSignal(result)
}

// calculateOverallSignal calculates the overall signal based on all timeframes.
func (a *Analyzer) calculateOverallSignal(result *MTFResult) {
	var totalScore float64
	var count int
	
	// Weight timeframes: higher timeframes have more weight
	weights := map[Timeframe]float64{
		Timeframe5Min:  0.15,
		Timeframe15Min: 0.20,
		Timeframe1Hour: 0.30,
		Timeframe1Day:  0.35,
	}
	
	var weightedScore float64
	var totalWeight float64
	
	for tf, analysis := range result.Timeframes {
		if analysis == nil || analysis.Error != nil {
			continue
		}
		
		weight := weights[tf]
		weightedScore += analysis.SignalScore * weight
		totalWeight += weight
		totalScore += analysis.SignalScore
		count++
	}
	
	if totalWeight > 0 {
		result.OverallScore = weightedScore / totalWeight
	} else if count > 0 {
		result.OverallScore = totalScore / float64(count)
	}
	
	// Determine overall signal recommendation
	result.OverallSignal = scoreToRecommendation(result.OverallScore)
}

// scoreToRecommendation converts a numeric score to a recommendation.
func scoreToRecommendation(score float64) analysis.SignalRecommendation {
	switch {
	case score >= 70:
		return analysis.StrongBuy
	case score >= 40:
		return analysis.Buy
	case score >= 15:
		return analysis.WeakBuy
	case score >= -15:
		return analysis.Neutral
	case score >= -40:
		return analysis.WeakSell
	case score >= -70:
		return analysis.Sell
	default:
		return analysis.StrongSell
	}
}

// getLastNonZero returns the last non-zero value from a slice.
func getLastNonZero(values []float64) float64 {
	for i := len(values) - 1; i >= 0; i-- {
		if values[i] != 0 {
			return values[i]
		}
	}
	return 0
}

// maxInt returns the maximum of two integers.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// IsBullishConfluence returns true if there is bullish confluence across timeframes.
func (r *MTFResult) IsBullishConfluence() bool {
	return r.BullishCount >= 3
}

// IsBearishConfluence returns true if there is bearish confluence across timeframes.
func (r *MTFResult) IsBearishConfluence() bool {
	return r.BearishCount >= 3
}

// HasStrongConfluence returns true if there is strong confluence (4/4 agreement).
func (r *MTFResult) HasStrongConfluence() bool {
	return r.Confluence == ConfluenceStrong
}

// GetTimeframeAnalysis returns the analysis for a specific timeframe.
func (r *MTFResult) GetTimeframeAnalysis(tf Timeframe) *TimeframeAnalysis {
	if r.Timeframes == nil {
		return nil
	}
	return r.Timeframes[tf]
}

// GetTrendSummary returns a summary of trends across all timeframes.
func (r *MTFResult) GetTrendSummary() map[Timeframe]patterns.TrendDirection {
	summary := make(map[Timeframe]patterns.TrendDirection)
	for tf, analysis := range r.Timeframes {
		if analysis != nil && analysis.Error == nil {
			summary[tf] = analysis.Trend
		}
	}
	return summary
}

// GetSignalSummary returns a summary of signals across all timeframes.
func (r *MTFResult) GetSignalSummary() map[Timeframe]analysis.SignalRecommendation {
	summary := make(map[Timeframe]analysis.SignalRecommendation)
	for tf, tfAnalysis := range r.Timeframes {
		if tfAnalysis != nil && tfAnalysis.Error == nil {
			summary[tf] = tfAnalysis.Signal
		}
	}
	return summary
}


// FormatResult formats the MTF result for display.
func (r *MTFResult) FormatResult() string {
	var sb strings.Builder
	
	sb.WriteString(fmt.Sprintf("Multi-Timeframe Analysis: %s\n", r.Symbol))
	sb.WriteString(strings.Repeat("─", 60) + "\n\n")
	
	// Timeframe details
	sb.WriteString("Timeframe Analysis:\n")
	sb.WriteString(fmt.Sprintf("%-12s %-10s %-12s %-8s %-8s %-8s\n", 
		"Timeframe", "Trend", "Strength", "Signal", "RSI", "ADX"))
	sb.WriteString(strings.Repeat("-", 60) + "\n")
	
	for _, tf := range AllTimeframes() {
		analysis := r.Timeframes[tf]
		if analysis == nil || analysis.Error != nil {
			sb.WriteString(fmt.Sprintf("%-12s %-10s\n", tf, "N/A"))
			continue
		}
		
		sb.WriteString(fmt.Sprintf("%-12s %-10s %-12s %-8s %-8.1f %-8.1f\n",
			tf,
			analysis.Trend,
			analysis.TrendStrength,
			formatSignalShort(analysis.Signal),
			analysis.RSIValue,
			analysis.ADXValue,
		))
	}
	
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("─", 60) + "\n")
	
	// Summary
	sb.WriteString("Summary:\n")
	sb.WriteString(fmt.Sprintf("  Confluence:      %s\n", r.Confluence))
	sb.WriteString(fmt.Sprintf("  Trend Alignment: %v\n", r.TrendAlignment))
	sb.WriteString(fmt.Sprintf("  Overall Trend:   %s\n", r.OverallTrend))
	sb.WriteString(fmt.Sprintf("  Overall Signal:  %s (%.1f)\n", r.OverallSignal, r.OverallScore))
	sb.WriteString(fmt.Sprintf("  Bullish TFs:     %d\n", r.BullishCount))
	sb.WriteString(fmt.Sprintf("  Bearish TFs:     %d\n", r.BearishCount))
	sb.WriteString(fmt.Sprintf("  Neutral TFs:     %d\n", r.NeutralCount))
	
	return sb.String()
}

// formatSignalShort returns a short form of the signal recommendation.
func formatSignalShort(signal analysis.SignalRecommendation) string {
	switch signal {
	case analysis.StrongBuy:
		return "S.BUY"
	case analysis.Buy:
		return "BUY"
	case analysis.WeakBuy:
		return "W.BUY"
	case analysis.Neutral:
		return "NEUT"
	case analysis.WeakSell:
		return "W.SELL"
	case analysis.Sell:
		return "SELL"
	case analysis.StrongSell:
		return "S.SELL"
	default:
		return "N/A"
	}
}
