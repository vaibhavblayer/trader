// Package patterns provides chart and candlestick pattern detection.
package patterns

import (
	"math"

	"zerodha-trader/internal/models"
)

// TrendDirection represents the direction of a trend.
type TrendDirection string

const (
	TrendUp      TrendDirection = "UP"
	TrendDown    TrendDirection = "DOWN"
	TrendSideways TrendDirection = "SIDEWAYS"
)

// TrendPhase represents the phase of a market cycle.
type TrendPhase string

const (
	PhaseAccumulation TrendPhase = "ACCUMULATION"
	PhaseMarkup       TrendPhase = "MARKUP"
	PhaseDistribution TrendPhase = "DISTRIBUTION"
	PhaseMarkdown     TrendPhase = "MARKDOWN"
)

// TrendStrength represents the strength of a trend.
type TrendStrength string

const (
	StrengthWeak     TrendStrength = "WEAK"
	StrengthModerate TrendStrength = "MODERATE"
	StrengthStrong   TrendStrength = "STRONG"
	StrengthVeryStrong TrendStrength = "VERY_STRONG"
)

// TrendAnalyzer analyzes trend direction, strength, and phases.
type TrendAnalyzer struct {
	adxPeriod        int
	swingStrength    int
	trendThreshold   float64 // ADX threshold for trending market
}

// NewTrendAnalyzer creates a new trend analyzer.
func NewTrendAnalyzer() *TrendAnalyzer {
	return &TrendAnalyzer{
		adxPeriod:      14,
		swingStrength:  3,
		trendThreshold: 25.0,
	}
}

func (t *TrendAnalyzer) Name() string {
	return "TrendAnalyzer"
}

// TrendAnalysisResult contains comprehensive trend analysis.
type TrendAnalysisResult struct {
	Direction       TrendDirection
	Strength        TrendStrength
	Phase           TrendPhase
	ADXValue        float64
	PlusDI          float64
	MinusDI         float64
	TrendDuration   int     // Number of bars in current trend
	AverageMoveSize float64 // Average price move per bar
	HigherHighs     int     // Count of higher highs
	HigherLows      int     // Count of higher lows
	LowerHighs      int     // Count of lower highs
	LowerLows       int     // Count of lower lows
	SwingPoints     []TrendSwingPoint
}

// TrendSwingPoint represents a swing point in trend analysis.
type TrendSwingPoint struct {
	Index    int
	Price    float64
	IsHigh   bool
	IsHigher bool // Higher high/low or lower high/low
}

// Analyze performs comprehensive trend analysis.
func (t *TrendAnalyzer) Analyze(candles []models.Candle) (*TrendAnalysisResult, error) {
	if len(candles) < t.adxPeriod*2 {
		return nil, nil
	}

	result := &TrendAnalysisResult{}

	// Calculate ADX and DI values
	adx, plusDI, minusDI := t.calculateADX(candles)
	n := len(candles)
	result.ADXValue = adx[n-1]
	result.PlusDI = plusDI[n-1]
	result.MinusDI = minusDI[n-1]

	// Determine trend direction using ADX
	result.Direction = t.determineTrendDirection(result.ADXValue, result.PlusDI, result.MinusDI)

	// Determine trend strength
	result.Strength = t.determineTrendStrength(result.ADXValue)

	// Find swing points and analyze structure
	result.SwingPoints = t.findSwingPoints(candles)
	result.HigherHighs, result.HigherLows, result.LowerHighs, result.LowerLows = t.analyzeSwingStructure(result.SwingPoints)

	// Determine trend phase
	result.Phase = t.determineTrendPhase(candles, result)

	// Calculate trend duration
	result.TrendDuration = t.calculateTrendDuration(candles, result.Direction)

	// Calculate average move size
	result.AverageMoveSize = t.calculateAverageMoveSize(candles, result.TrendDuration)

	return result, nil
}

// calculateADX calculates ADX, +DI, and -DI values.
func (t *TrendAnalyzer) calculateADX(candles []models.Candle) ([]float64, []float64, []float64) {
	n := len(candles)
	plusDM := make([]float64, n)
	minusDM := make([]float64, n)
	tr := make([]float64, n)

	// Calculate +DM, -DM, and TR
	for i := 1; i < n; i++ {
		upMove := candles[i].High - candles[i-1].High
		downMove := candles[i-1].Low - candles[i].Low

		if upMove > downMove && upMove > 0 {
			plusDM[i] = upMove
		}
		if downMove > upMove && downMove > 0 {
			minusDM[i] = downMove
		}
		tr[i] = t.trueRange(candles[i], candles[i-1])
	}

	// Smooth using Wilder's method
	smoothPlusDM := t.wilderSmooth(plusDM, t.adxPeriod)
	smoothMinusDM := t.wilderSmooth(minusDM, t.adxPeriod)
	smoothTR := t.wilderSmooth(tr, t.adxPeriod)

	// Calculate +DI and -DI
	plusDI := make([]float64, n)
	minusDI := make([]float64, n)
	dx := make([]float64, n)

	for i := t.adxPeriod; i < n; i++ {
		if smoothTR[i] != 0 {
			plusDI[i] = 100 * smoothPlusDM[i] / smoothTR[i]
			minusDI[i] = 100 * smoothMinusDM[i] / smoothTR[i]
		}
		diSum := plusDI[i] + minusDI[i]
		if diSum != 0 {
			dx[i] = 100 * math.Abs(plusDI[i]-minusDI[i]) / diSum
		}
	}

	// Calculate ADX (smoothed DX)
	adx := make([]float64, n)
	if len(dx) > t.adxPeriod {
		smoothedDX := t.wilderSmooth(dx[t.adxPeriod:], t.adxPeriod)
		for i := 0; i < len(smoothedDX); i++ {
			adx[t.adxPeriod+i] = smoothedDX[i]
		}
	}

	return adx, plusDI, minusDI
}

// trueRange calculates the true range for a candle.
func (t *TrendAnalyzer) trueRange(current, previous models.Candle) float64 {
	highLow := current.High - current.Low
	highClose := math.Abs(current.High - previous.Close)
	lowClose := math.Abs(current.Low - previous.Close)
	return math.Max(highLow, math.Max(highClose, lowClose))
}

// wilderSmooth applies Wilder's smoothing method.
func (t *TrendAnalyzer) wilderSmooth(values []float64, period int) []float64 {
	if len(values) < period {
		return make([]float64, len(values))
	}

	result := make([]float64, len(values))

	// First value is SMA
	var sum float64
	for i := 0; i < period; i++ {
		sum += values[i]
	}
	result[period-1] = sum / float64(period)

	// Subsequent values use Wilder smoothing
	multiplier := 1.0 / float64(period)
	for i := period; i < len(values); i++ {
		result[i] = result[i-1] + multiplier*(values[i]-result[i-1])
	}

	return result
}

// determineTrendDirection determines the trend direction based on ADX and DI values.
func (t *TrendAnalyzer) determineTrendDirection(adx, plusDI, minusDI float64) TrendDirection {
	if adx < t.trendThreshold {
		return TrendSideways
	}

	if plusDI > minusDI {
		return TrendUp
	}
	return TrendDown
}

// determineTrendStrength determines the strength of the trend based on ADX.
func (t *TrendAnalyzer) determineTrendStrength(adx float64) TrendStrength {
	if adx < 20 {
		return StrengthWeak
	}
	if adx < 40 {
		return StrengthModerate
	}
	if adx < 60 {
		return StrengthStrong
	}
	return StrengthVeryStrong
}


// findSwingPoints identifies swing highs and lows for trend analysis.
func (t *TrendAnalyzer) findSwingPoints(candles []models.Candle) []TrendSwingPoint {
	var swings []TrendSwingPoint
	n := len(candles)

	for i := t.swingStrength; i < n-t.swingStrength; i++ {
		// Check for swing high
		isSwingHigh := true
		for j := 1; j <= t.swingStrength; j++ {
			if candles[i].High <= candles[i-j].High || candles[i].High <= candles[i+j].High {
				isSwingHigh = false
				break
			}
		}
		if isSwingHigh {
			swings = append(swings, TrendSwingPoint{
				Index:  i,
				Price:  candles[i].High,
				IsHigh: true,
			})
		}

		// Check for swing low
		isSwingLow := true
		for j := 1; j <= t.swingStrength; j++ {
			if candles[i].Low >= candles[i-j].Low || candles[i].Low >= candles[i+j].Low {
				isSwingLow = false
				break
			}
		}
		if isSwingLow {
			swings = append(swings, TrendSwingPoint{
				Index:  i,
				Price:  candles[i].Low,
				IsHigh: false,
			})
		}
	}

	// Mark higher/lower swings
	t.markSwingRelationships(swings)

	return swings
}

// markSwingRelationships marks whether each swing is higher or lower than the previous.
func (t *TrendAnalyzer) markSwingRelationships(swings []TrendSwingPoint) {
	var lastHigh, lastLow *TrendSwingPoint

	for i := range swings {
		if swings[i].IsHigh {
			if lastHigh != nil {
				swings[i].IsHigher = swings[i].Price > lastHigh.Price
			}
			lastHigh = &swings[i]
		} else {
			if lastLow != nil {
				swings[i].IsHigher = swings[i].Price > lastLow.Price
			}
			lastLow = &swings[i]
		}
	}
}

// analyzeSwingStructure counts higher highs, higher lows, lower highs, and lower lows.
func (t *TrendAnalyzer) analyzeSwingStructure(swings []TrendSwingPoint) (hh, hl, lh, ll int) {
	for _, swing := range swings {
		if swing.IsHigh {
			if swing.IsHigher {
				hh++
			} else {
				lh++
			}
		} else {
			if swing.IsHigher {
				hl++
			} else {
				ll++
			}
		}
	}
	return
}

// determineTrendPhase determines the current market phase.
func (t *TrendAnalyzer) determineTrendPhase(candles []models.Candle, result *TrendAnalysisResult) TrendPhase {
	n := len(candles)
	if n < 50 {
		return PhaseAccumulation
	}

	// Calculate price position relative to recent range
	recentHigh := candles[n-1].High
	recentLow := candles[n-1].Low
	for i := n - 20; i < n; i++ {
		if candles[i].High > recentHigh {
			recentHigh = candles[i].High
		}
		if candles[i].Low < recentLow {
			recentLow = candles[i].Low
		}
	}

	currentPrice := candles[n-1].Close
	priceRange := recentHigh - recentLow
	if priceRange == 0 {
		return PhaseAccumulation
	}

	pricePosition := (currentPrice - recentLow) / priceRange

	// Analyze volume trend
	avgVolume := t.calculateAverageVolume(candles, n-20, n)
	recentVolume := t.calculateAverageVolume(candles, n-5, n)
	volumeIncreasing := recentVolume > avgVolume*1.2

	// Determine phase based on trend direction, price position, and volume
	switch result.Direction {
	case TrendUp:
		if pricePosition > 0.7 && !volumeIncreasing {
			return PhaseDistribution
		}
		return PhaseMarkup

	case TrendDown:
		if pricePosition < 0.3 && !volumeIncreasing {
			return PhaseAccumulation
		}
		return PhaseMarkdown

	default: // Sideways
		if pricePosition < 0.3 {
			return PhaseAccumulation
		}
		if pricePosition > 0.7 {
			return PhaseDistribution
		}
		// Check swing structure for more clues
		if result.HigherLows > result.LowerLows {
			return PhaseAccumulation
		}
		if result.LowerHighs > result.HigherHighs {
			return PhaseDistribution
		}
		return PhaseAccumulation
	}
}

// calculateAverageVolume calculates average volume in a range.
func (t *TrendAnalyzer) calculateAverageVolume(candles []models.Candle, start, end int) float64 {
	if end <= start || start < 0 {
		return 0
	}

	var sum int64
	for i := start; i < end && i < len(candles); i++ {
		sum += candles[i].Volume
	}
	return float64(sum) / float64(end-start)
}

// calculateTrendDuration calculates how long the current trend has been in effect.
func (t *TrendAnalyzer) calculateTrendDuration(candles []models.Candle, direction TrendDirection) int {
	n := len(candles)
	if n < 2 {
		return 0
	}

	duration := 0

	switch direction {
	case TrendUp:
		// Count bars since last significant lower low
		for i := n - 1; i > 0; i-- {
			if candles[i].Close < candles[i-1].Close*0.98 { // 2% drop
				break
			}
			duration++
		}

	case TrendDown:
		// Count bars since last significant higher high
		for i := n - 1; i > 0; i-- {
			if candles[i].Close > candles[i-1].Close*1.02 { // 2% rise
				break
			}
			duration++
		}

	default:
		// For sideways, count bars in the range
		if n < 20 {
			return n
		}
		rangeHigh := candles[n-1].High
		rangeLow := candles[n-1].Low
		for i := n - 20; i < n; i++ {
			if candles[i].High > rangeHigh {
				rangeHigh = candles[i].High
			}
			if candles[i].Low < rangeLow {
				rangeLow = candles[i].Low
			}
		}
		rangeSize := (rangeHigh - rangeLow) / rangeLow

		// If range is less than 5%, count as sideways duration
		if rangeSize < 0.05 {
			for i := n - 1; i > 0; i-- {
				if candles[i].High > rangeHigh*1.02 || candles[i].Low < rangeLow*0.98 {
					break
				}
				duration++
			}
		}
	}

	return duration
}

// calculateAverageMoveSize calculates the average price move per bar during the trend.
func (t *TrendAnalyzer) calculateAverageMoveSize(candles []models.Candle, duration int) float64 {
	n := len(candles)
	if duration <= 0 || n < 2 {
		return 0
	}

	startIdx := maxInt(0, n-duration)
	totalMove := math.Abs(candles[n-1].Close - candles[startIdx].Close)

	return totalMove / float64(duration)
}

// IsUptrend returns true if the market is in an uptrend.
func (t *TrendAnalyzer) IsUptrend(result *TrendAnalysisResult) bool {
	if result == nil {
		return false
	}
	return result.Direction == TrendUp
}

// IsDowntrend returns true if the market is in a downtrend.
func (t *TrendAnalyzer) IsDowntrend(result *TrendAnalysisResult) bool {
	if result == nil {
		return false
	}
	return result.Direction == TrendDown
}

// IsSideways returns true if the market is ranging/sideways.
func (t *TrendAnalyzer) IsSideways(result *TrendAnalysisResult) bool {
	if result == nil {
		return false
	}
	return result.Direction == TrendSideways
}

// GetTrendScore returns a score from -100 to +100 indicating trend strength and direction.
func (t *TrendAnalyzer) GetTrendScore(result *TrendAnalysisResult) float64 {
	if result == nil {
		return 0
	}

	// Base score from ADX
	score := result.ADXValue

	// Adjust based on direction
	if result.Direction == TrendDown {
		score = -score
	} else if result.Direction == TrendSideways {
		score = 0
	}

	// Adjust based on swing structure
	if result.HigherHighs > result.LowerHighs && result.HigherLows > result.LowerLows {
		score *= 1.2 // Strong uptrend structure
	} else if result.LowerHighs > result.HigherHighs && result.LowerLows > result.HigherLows {
		score *= 1.2 // Strong downtrend structure
	}

	// Clamp to [-100, 100]
	if score > 100 {
		score = 100
	}
	if score < -100 {
		score = -100
	}

	return score
}
