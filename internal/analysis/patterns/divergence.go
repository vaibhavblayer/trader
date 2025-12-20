// Package patterns provides chart and candlestick pattern detection.
package patterns

import (
	"zerodha-trader/internal/models"
)

// DivergenceType represents the type of divergence.
type DivergenceType string

const (
	DivergenceRegularBullish DivergenceType = "REGULAR_BULLISH"
	DivergenceRegularBearish DivergenceType = "REGULAR_BEARISH"
	DivergenceHiddenBullish  DivergenceType = "HIDDEN_BULLISH"
	DivergenceHiddenBearish  DivergenceType = "HIDDEN_BEARISH"
)

// DivergenceDetector detects price-indicator divergences.
type DivergenceDetector struct {
	swingStrength    int     // Number of bars for swing point confirmation
	minDivergenceBars int    // Minimum bars between swing points
	maxDivergenceBars int    // Maximum bars between swing points
	tolerance        float64 // Tolerance for price/indicator comparison
}

// NewDivergenceDetector creates a new divergence detector.
func NewDivergenceDetector() *DivergenceDetector {
	return &DivergenceDetector{
		swingStrength:    3,
		minDivergenceBars: 5,
		maxDivergenceBars: 50,
		tolerance:        0.001, // 0.1% tolerance
	}
}

func (d *DivergenceDetector) Name() string {
	return "DivergenceDetector"
}

// Divergence represents a detected divergence.
type Divergence struct {
	Type           DivergenceType
	Indicator      string
	StartIndex     int
	EndIndex       int
	PriceStart     float64
	PriceEnd       float64
	IndicatorStart float64
	IndicatorEnd   float64
	Strength       float64 // 0-1 based on divergence magnitude
}

// DivergenceResult contains all detected divergences.
type DivergenceResult struct {
	Divergences       []Divergence
	ConfluenceCount   int      // Number of indicators showing same divergence
	ConfluenceType    DivergenceType
	StrongestDivergence *Divergence
}

// DetectWithIndicator detects divergences between price and a single indicator.
func (d *DivergenceDetector) DetectWithIndicator(candles []models.Candle, indicator []float64, indicatorName string) []Divergence {
	if len(candles) != len(indicator) || len(candles) < d.maxDivergenceBars {
		return nil
	}

	var divergences []Divergence

	// Find swing points in price
	priceSwingHighs, priceSwingLows := d.findPriceSwings(candles)

	// Find swing points in indicator
	indicatorSwingHighs, indicatorSwingLows := d.findIndicatorSwings(indicator)

	// Detect regular bullish divergence (price makes lower low, indicator makes higher low)
	divergences = append(divergences, d.detectRegularBullish(candles, indicator, indicatorName, priceSwingLows, indicatorSwingLows)...)

	// Detect regular bearish divergence (price makes higher high, indicator makes lower high)
	divergences = append(divergences, d.detectRegularBearish(candles, indicator, indicatorName, priceSwingHighs, indicatorSwingHighs)...)

	// Detect hidden bullish divergence (price makes higher low, indicator makes lower low)
	divergences = append(divergences, d.detectHiddenBullish(candles, indicator, indicatorName, priceSwingLows, indicatorSwingLows)...)

	// Detect hidden bearish divergence (price makes lower high, indicator makes higher high)
	divergences = append(divergences, d.detectHiddenBearish(candles, indicator, indicatorName, priceSwingHighs, indicatorSwingHighs)...)

	return divergences
}

// swingPoint represents a swing high or low point.
type swingPoint struct {
	index int
	value float64
}

// findPriceSwings finds swing highs and lows in price data.
func (d *DivergenceDetector) findPriceSwings(candles []models.Candle) (highs, lows []swingPoint) {
	n := len(candles)

	for i := d.swingStrength; i < n-d.swingStrength; i++ {
		// Check for swing high
		isSwingHigh := true
		for j := 1; j <= d.swingStrength; j++ {
			if candles[i].High <= candles[i-j].High || candles[i].High <= candles[i+j].High {
				isSwingHigh = false
				break
			}
		}
		if isSwingHigh {
			highs = append(highs, swingPoint{index: i, value: candles[i].High})
		}

		// Check for swing low
		isSwingLow := true
		for j := 1; j <= d.swingStrength; j++ {
			if candles[i].Low >= candles[i-j].Low || candles[i].Low >= candles[i+j].Low {
				isSwingLow = false
				break
			}
		}
		if isSwingLow {
			lows = append(lows, swingPoint{index: i, value: candles[i].Low})
		}
	}

	return highs, lows
}

// findIndicatorSwings finds swing highs and lows in indicator data.
func (d *DivergenceDetector) findIndicatorSwings(indicator []float64) (highs, lows []swingPoint) {
	n := len(indicator)

	for i := d.swingStrength; i < n-d.swingStrength; i++ {
		// Skip zero values (indicator not calculated yet)
		if indicator[i] == 0 {
			continue
		}

		// Check for swing high
		isSwingHigh := true
		for j := 1; j <= d.swingStrength; j++ {
			if indicator[i] <= indicator[i-j] || indicator[i] <= indicator[i+j] {
				isSwingHigh = false
				break
			}
		}
		if isSwingHigh {
			highs = append(highs, swingPoint{index: i, value: indicator[i]})
		}

		// Check for swing low
		isSwingLow := true
		for j := 1; j <= d.swingStrength; j++ {
			if indicator[i] >= indicator[i-j] || indicator[i] >= indicator[i+j] {
				isSwingLow = false
				break
			}
		}
		if isSwingLow {
			lows = append(lows, swingPoint{index: i, value: indicator[i]})
		}
	}

	return highs, lows
}

// detectRegularBullish detects regular bullish divergence.
// Price makes lower low, indicator makes higher low.
func (d *DivergenceDetector) detectRegularBullish(candles []models.Candle, indicator []float64, indicatorName string, priceLows, indicatorLows []swingPoint) []Divergence {
	var divergences []Divergence

	for i := 0; i < len(priceLows)-1; i++ {
		for j := i + 1; j < len(priceLows); j++ {
			first := priceLows[i]
			second := priceLows[j]

			// Check bar distance
			barDist := second.index - first.index
			if barDist < d.minDivergenceBars || barDist > d.maxDivergenceBars {
				continue
			}

			// Price makes lower low
			if second.value >= first.value {
				continue
			}

			// Find corresponding indicator lows
			indFirst := d.findNearestSwing(indicatorLows, first.index)
			indSecond := d.findNearestSwing(indicatorLows, second.index)

			if indFirst == nil || indSecond == nil {
				continue
			}

			// Indicator makes higher low
			if indSecond.value <= indFirst.value {
				continue
			}

			strength := d.calculateDivergenceStrength(first.value, second.value, indFirst.value, indSecond.value)

			divergences = append(divergences, Divergence{
				Type:           DivergenceRegularBullish,
				Indicator:      indicatorName,
				StartIndex:     first.index,
				EndIndex:       second.index,
				PriceStart:     first.value,
				PriceEnd:       second.value,
				IndicatorStart: indFirst.value,
				IndicatorEnd:   indSecond.value,
				Strength:       strength,
			})
		}
	}

	return divergences
}

// detectRegularBearish detects regular bearish divergence.
// Price makes higher high, indicator makes lower high.
func (d *DivergenceDetector) detectRegularBearish(candles []models.Candle, indicator []float64, indicatorName string, priceHighs, indicatorHighs []swingPoint) []Divergence {
	var divergences []Divergence

	for i := 0; i < len(priceHighs)-1; i++ {
		for j := i + 1; j < len(priceHighs); j++ {
			first := priceHighs[i]
			second := priceHighs[j]

			// Check bar distance
			barDist := second.index - first.index
			if barDist < d.minDivergenceBars || barDist > d.maxDivergenceBars {
				continue
			}

			// Price makes higher high
			if second.value <= first.value {
				continue
			}

			// Find corresponding indicator highs
			indFirst := d.findNearestSwing(indicatorHighs, first.index)
			indSecond := d.findNearestSwing(indicatorHighs, second.index)

			if indFirst == nil || indSecond == nil {
				continue
			}

			// Indicator makes lower high
			if indSecond.value >= indFirst.value {
				continue
			}

			strength := d.calculateDivergenceStrength(first.value, second.value, indFirst.value, indSecond.value)

			divergences = append(divergences, Divergence{
				Type:           DivergenceRegularBearish,
				Indicator:      indicatorName,
				StartIndex:     first.index,
				EndIndex:       second.index,
				PriceStart:     first.value,
				PriceEnd:       second.value,
				IndicatorStart: indFirst.value,
				IndicatorEnd:   indSecond.value,
				Strength:       strength,
			})
		}
	}

	return divergences
}


// detectHiddenBullish detects hidden bullish divergence.
// Price makes higher low, indicator makes lower low.
func (d *DivergenceDetector) detectHiddenBullish(candles []models.Candle, indicator []float64, indicatorName string, priceLows, indicatorLows []swingPoint) []Divergence {
	var divergences []Divergence

	for i := 0; i < len(priceLows)-1; i++ {
		for j := i + 1; j < len(priceLows); j++ {
			first := priceLows[i]
			second := priceLows[j]

			// Check bar distance
			barDist := second.index - first.index
			if barDist < d.minDivergenceBars || barDist > d.maxDivergenceBars {
				continue
			}

			// Price makes higher low
			if second.value <= first.value {
				continue
			}

			// Find corresponding indicator lows
			indFirst := d.findNearestSwing(indicatorLows, first.index)
			indSecond := d.findNearestSwing(indicatorLows, second.index)

			if indFirst == nil || indSecond == nil {
				continue
			}

			// Indicator makes lower low
			if indSecond.value >= indFirst.value {
				continue
			}

			strength := d.calculateDivergenceStrength(first.value, second.value, indFirst.value, indSecond.value)

			divergences = append(divergences, Divergence{
				Type:           DivergenceHiddenBullish,
				Indicator:      indicatorName,
				StartIndex:     first.index,
				EndIndex:       second.index,
				PriceStart:     first.value,
				PriceEnd:       second.value,
				IndicatorStart: indFirst.value,
				IndicatorEnd:   indSecond.value,
				Strength:       strength,
			})
		}
	}

	return divergences
}

// detectHiddenBearish detects hidden bearish divergence.
// Price makes lower high, indicator makes higher high.
func (d *DivergenceDetector) detectHiddenBearish(candles []models.Candle, indicator []float64, indicatorName string, priceHighs, indicatorHighs []swingPoint) []Divergence {
	var divergences []Divergence

	for i := 0; i < len(priceHighs)-1; i++ {
		for j := i + 1; j < len(priceHighs); j++ {
			first := priceHighs[i]
			second := priceHighs[j]

			// Check bar distance
			barDist := second.index - first.index
			if barDist < d.minDivergenceBars || barDist > d.maxDivergenceBars {
				continue
			}

			// Price makes lower high
			if second.value >= first.value {
				continue
			}

			// Find corresponding indicator highs
			indFirst := d.findNearestSwing(indicatorHighs, first.index)
			indSecond := d.findNearestSwing(indicatorHighs, second.index)

			if indFirst == nil || indSecond == nil {
				continue
			}

			// Indicator makes higher high
			if indSecond.value <= indFirst.value {
				continue
			}

			strength := d.calculateDivergenceStrength(first.value, second.value, indFirst.value, indSecond.value)

			divergences = append(divergences, Divergence{
				Type:           DivergenceHiddenBearish,
				Indicator:      indicatorName,
				StartIndex:     first.index,
				EndIndex:       second.index,
				PriceStart:     first.value,
				PriceEnd:       second.value,
				IndicatorStart: indFirst.value,
				IndicatorEnd:   indSecond.value,
				Strength:       strength,
			})
		}
	}

	return divergences
}

// findNearestSwing finds the swing point nearest to the given index.
func (d *DivergenceDetector) findNearestSwing(swings []swingPoint, targetIndex int) *swingPoint {
	if len(swings) == 0 {
		return nil
	}

	var nearest *swingPoint
	minDist := d.maxDivergenceBars

	for i := range swings {
		dist := absInt(swings[i].index - targetIndex)
		if dist < minDist {
			minDist = dist
			nearest = &swings[i]
		}
	}

	// Only return if within reasonable distance
	if minDist <= d.swingStrength*2 {
		return nearest
	}
	return nil
}

// calculateDivergenceStrength calculates the strength of a divergence.
func (d *DivergenceDetector) calculateDivergenceStrength(priceStart, priceEnd, indStart, indEnd float64) float64 {
	// Calculate percentage changes
	priceChange := 0.0
	if priceStart != 0 {
		priceChange = (priceEnd - priceStart) / priceStart
	}

	indChange := 0.0
	if indStart != 0 {
		indChange = (indEnd - indStart) / indStart
	}

	// Strength is based on the magnitude of the divergence
	// Higher divergence = stronger signal
	divergenceMagnitude := absFloat(priceChange - indChange)

	// Normalize to 0-1 range (assuming max divergence of 50%)
	strength := divergenceMagnitude / 0.5
	if strength > 1 {
		strength = 1
	}

	return strength
}

// absFloat returns the absolute value of a float64.
func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// abs returns the absolute value of an int.
func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// DetectMultiIndicator detects divergences across multiple indicators and finds confluence.
func (d *DivergenceDetector) DetectMultiIndicator(candles []models.Candle, indicators map[string][]float64) *DivergenceResult {
	result := &DivergenceResult{}

	// Detect divergences for each indicator
	for name, values := range indicators {
		divs := d.DetectWithIndicator(candles, values, name)
		result.Divergences = append(result.Divergences, divs...)
	}

	if len(result.Divergences) == 0 {
		return result
	}

	// Find confluence (multiple indicators showing same type of divergence)
	typeCount := make(map[DivergenceType]int)
	for _, div := range result.Divergences {
		typeCount[div.Type]++
	}

	// Find the most common divergence type
	maxCount := 0
	for divType, count := range typeCount {
		if count > maxCount {
			maxCount = count
			result.ConfluenceType = divType
		}
	}
	result.ConfluenceCount = maxCount

	// Find the strongest divergence
	var strongest *Divergence
	for i := range result.Divergences {
		if strongest == nil || result.Divergences[i].Strength > strongest.Strength {
			strongest = &result.Divergences[i]
		}
	}
	result.StrongestDivergence = strongest

	return result
}

// IsBullishDivergence checks if a divergence type is bullish.
func IsBullishDivergence(divType DivergenceType) bool {
	return divType == DivergenceRegularBullish || divType == DivergenceHiddenBullish
}

// IsBearishDivergence checks if a divergence type is bearish.
func IsBearishDivergence(divType DivergenceType) bool {
	return divType == DivergenceRegularBearish || divType == DivergenceHiddenBearish
}

// IsRegularDivergence checks if a divergence type is regular (reversal signal).
func IsRegularDivergence(divType DivergenceType) bool {
	return divType == DivergenceRegularBullish || divType == DivergenceRegularBearish
}

// IsHiddenDivergence checks if a divergence type is hidden (continuation signal).
func IsHiddenDivergence(divType DivergenceType) bool {
	return divType == DivergenceHiddenBullish || divType == DivergenceHiddenBearish
}

// GetRecentDivergences returns divergences that end within the last N bars.
func (d *DivergenceDetector) GetRecentDivergences(divergences []Divergence, totalBars, lookback int) []Divergence {
	var recent []Divergence
	threshold := totalBars - lookback

	for _, div := range divergences {
		if div.EndIndex >= threshold {
			recent = append(recent, div)
		}
	}

	return recent
}
