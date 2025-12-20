// Package patterns provides chart and candlestick pattern detection.
package patterns

import (
	"zerodha-trader/internal/analysis"
	"zerodha-trader/internal/models"
)

// CandlestickDetector detects candlestick patterns in price data.
type CandlestickDetector struct {
	// Configuration for pattern detection
	dojiThreshold      float64 // Body size as % of range for doji
	longBodyThreshold  float64 // Body size as % of range for long body
	shadowThreshold    float64 // Shadow size as % of body for hammer/shooting star
	volumeConfirmRatio float64 // Volume ratio for confirmation
}

// NewCandlestickDetector creates a new candlestick pattern detector.
func NewCandlestickDetector() *CandlestickDetector {
	return &CandlestickDetector{
		dojiThreshold:      0.1,  // Body < 10% of range
		longBodyThreshold:  0.6,  // Body > 60% of range
		shadowThreshold:    2.0,  // Shadow >= 2x body
		volumeConfirmRatio: 1.5,  // Volume >= 1.5x average
	}
}

func (d *CandlestickDetector) Name() string {
	return "CandlestickDetector"
}

// Detect detects all candlestick patterns in the given candles.
func (d *CandlestickDetector) Detect(candles []models.Candle) ([]analysis.Pattern, error) {
	if len(candles) < 3 {
		return nil, nil
	}

	var patterns []analysis.Pattern

	// Calculate average volume for confirmation
	avgVolume := d.calculateAverageVolume(candles)

	// Detect single-candle patterns
	for i := 0; i < len(candles); i++ {
		if p := d.detectDoji(candles, i, avgVolume); p != nil {
			patterns = append(patterns, *p)
		}
		if p := d.detectHammer(candles, i, avgVolume); p != nil {
			patterns = append(patterns, *p)
		}
		if p := d.detectInvertedHammer(candles, i, avgVolume); p != nil {
			patterns = append(patterns, *p)
		}
		if p := d.detectHangingMan(candles, i, avgVolume); p != nil {
			patterns = append(patterns, *p)
		}
		if p := d.detectShootingStar(candles, i, avgVolume); p != nil {
			patterns = append(patterns, *p)
		}
		if p := d.detectMarubozu(candles, i, avgVolume); p != nil {
			patterns = append(patterns, *p)
		}
		if p := d.detectSpinningTop(candles, i, avgVolume); p != nil {
			patterns = append(patterns, *p)
		}
	}

	// Detect two-candle patterns
	for i := 1; i < len(candles); i++ {
		if p := d.detectEngulfing(candles, i, avgVolume); p != nil {
			patterns = append(patterns, *p)
		}
		if p := d.detectPiercingLine(candles, i, avgVolume); p != nil {
			patterns = append(patterns, *p)
		}
		if p := d.detectDarkCloudCover(candles, i, avgVolume); p != nil {
			patterns = append(patterns, *p)
		}
		if p := d.detectTweezer(candles, i, avgVolume); p != nil {
			patterns = append(patterns, *p)
		}
		if p := d.detectHarami(candles, i, avgVolume); p != nil {
			patterns = append(patterns, *p)
		}
	}

	// Detect three-candle patterns
	for i := 2; i < len(candles); i++ {
		if p := d.detectMorningStar(candles, i, avgVolume); p != nil {
			patterns = append(patterns, *p)
		}
		if p := d.detectEveningStar(candles, i, avgVolume); p != nil {
			patterns = append(patterns, *p)
		}
		if p := d.detectThreeWhiteSoldiers(candles, i, avgVolume); p != nil {
			patterns = append(patterns, *p)
		}
		if p := d.detectThreeBlackCrows(candles, i, avgVolume); p != nil {
			patterns = append(patterns, *p)
		}
	}

	return patterns, nil
}

// Helper functions for candle analysis
func (d *CandlestickDetector) bodySize(c models.Candle) float64 {
	return abs(c.Close - c.Open)
}

func (d *CandlestickDetector) candleRange(c models.Candle) float64 {
	return c.High - c.Low
}

func (d *CandlestickDetector) upperShadow(c models.Candle) float64 {
	return c.High - max(c.Open, c.Close)
}

func (d *CandlestickDetector) lowerShadow(c models.Candle) float64 {
	return min(c.Open, c.Close) - c.Low
}

func (d *CandlestickDetector) isBullish(c models.Candle) bool {
	return c.Close > c.Open
}

func (d *CandlestickDetector) isBearish(c models.Candle) bool {
	return c.Close < c.Open
}

func (d *CandlestickDetector) calculateAverageVolume(candles []models.Candle) float64 {
	if len(candles) == 0 {
		return 0
	}
	var total int64
	for _, c := range candles {
		total += c.Volume
	}
	return float64(total) / float64(len(candles))
}

func (d *CandlestickDetector) hasVolumeConfirmation(c models.Candle, avgVolume float64) bool {
	if avgVolume == 0 {
		return false
	}
	return float64(c.Volume) >= avgVolume*d.volumeConfirmRatio
}

func (d *CandlestickDetector) calculateStrength(baseStrength float64, volumeConfirm bool) float64 {
	if volumeConfirm {
		return min(1.0, baseStrength*1.2)
	}
	return baseStrength
}

// isInDowntrend checks if there's a downtrend before the given index
func (d *CandlestickDetector) isInDowntrend(candles []models.Candle, idx int) bool {
	if idx < 3 {
		return false
	}
	// Check if the last 3 candles show a downtrend
	return candles[idx-1].Close < candles[idx-2].Close &&
		candles[idx-2].Close < candles[idx-3].Close
}

// isInUptrend checks if there's an uptrend before the given index
func (d *CandlestickDetector) isInUptrend(candles []models.Candle, idx int) bool {
	if idx < 3 {
		return false
	}
	// Check if the last 3 candles show an uptrend
	return candles[idx-1].Close > candles[idx-2].Close &&
		candles[idx-2].Close > candles[idx-3].Close
}


// Single-candle pattern detection

// detectDoji detects Doji patterns (open ≈ close)
func (d *CandlestickDetector) detectDoji(candles []models.Candle, idx int, avgVolume float64) *analysis.Pattern {
	c := candles[idx]
	rng := d.candleRange(c)
	if rng == 0 {
		return nil
	}

	body := d.bodySize(c)
	bodyRatio := body / rng

	if bodyRatio > d.dojiThreshold {
		return nil
	}

	volumeConfirm := d.hasVolumeConfirmation(c, avgVolume)

	return &analysis.Pattern{
		Name:          "Doji",
		Type:          analysis.PatternTypeCandlestick,
		Direction:     analysis.PatternNeutral,
		StartIndex:    idx,
		EndIndex:      idx,
		Strength:      d.calculateStrength(0.5, volumeConfirm),
		VolumeConfirm: volumeConfirm,
	}
}

// detectHammer detects Hammer patterns (bullish reversal at bottom)
func (d *CandlestickDetector) detectHammer(candles []models.Candle, idx int, avgVolume float64) *analysis.Pattern {
	c := candles[idx]
	body := d.bodySize(c)
	if body == 0 {
		return nil
	}

	lowerShadow := d.lowerShadow(c)
	upperShadow := d.upperShadow(c)

	// Hammer: long lower shadow, small upper shadow, small body at top
	if lowerShadow < body*d.shadowThreshold {
		return nil
	}
	if upperShadow > body*0.5 {
		return nil
	}

	// Should be in a downtrend for valid hammer
	if !d.isInDowntrend(candles, idx) {
		return nil
	}

	volumeConfirm := d.hasVolumeConfirmation(c, avgVolume)

	return &analysis.Pattern{
		Name:          "Hammer",
		Type:          analysis.PatternTypeCandlestick,
		Direction:     analysis.PatternBullish,
		StartIndex:    idx,
		EndIndex:      idx,
		Strength:      d.calculateStrength(0.7, volumeConfirm),
		VolumeConfirm: volumeConfirm,
	}
}

// detectInvertedHammer detects Inverted Hammer patterns (bullish reversal at bottom)
func (d *CandlestickDetector) detectInvertedHammer(candles []models.Candle, idx int, avgVolume float64) *analysis.Pattern {
	c := candles[idx]
	body := d.bodySize(c)
	if body == 0 {
		return nil
	}

	lowerShadow := d.lowerShadow(c)
	upperShadow := d.upperShadow(c)

	// Inverted Hammer: long upper shadow, small lower shadow, small body at bottom
	if upperShadow < body*d.shadowThreshold {
		return nil
	}
	if lowerShadow > body*0.5 {
		return nil
	}

	// Should be in a downtrend
	if !d.isInDowntrend(candles, idx) {
		return nil
	}

	volumeConfirm := d.hasVolumeConfirmation(c, avgVolume)

	return &analysis.Pattern{
		Name:          "Inverted Hammer",
		Type:          analysis.PatternTypeCandlestick,
		Direction:     analysis.PatternBullish,
		StartIndex:    idx,
		EndIndex:      idx,
		Strength:      d.calculateStrength(0.6, volumeConfirm),
		VolumeConfirm: volumeConfirm,
	}
}

// detectHangingMan detects Hanging Man patterns (bearish reversal at top)
func (d *CandlestickDetector) detectHangingMan(candles []models.Candle, idx int, avgVolume float64) *analysis.Pattern {
	c := candles[idx]
	body := d.bodySize(c)
	if body == 0 {
		return nil
	}

	lowerShadow := d.lowerShadow(c)
	upperShadow := d.upperShadow(c)

	// Hanging Man: same shape as hammer but in uptrend
	if lowerShadow < body*d.shadowThreshold {
		return nil
	}
	if upperShadow > body*0.5 {
		return nil
	}

	// Should be in an uptrend
	if !d.isInUptrend(candles, idx) {
		return nil
	}

	volumeConfirm := d.hasVolumeConfirmation(c, avgVolume)

	return &analysis.Pattern{
		Name:          "Hanging Man",
		Type:          analysis.PatternTypeCandlestick,
		Direction:     analysis.PatternBearish,
		StartIndex:    idx,
		EndIndex:      idx,
		Strength:      d.calculateStrength(0.7, volumeConfirm),
		VolumeConfirm: volumeConfirm,
	}
}

// detectShootingStar detects Shooting Star patterns (bearish reversal at top)
func (d *CandlestickDetector) detectShootingStar(candles []models.Candle, idx int, avgVolume float64) *analysis.Pattern {
	c := candles[idx]
	body := d.bodySize(c)
	if body == 0 {
		return nil
	}

	lowerShadow := d.lowerShadow(c)
	upperShadow := d.upperShadow(c)

	// Shooting Star: long upper shadow, small lower shadow, small body at bottom
	if upperShadow < body*d.shadowThreshold {
		return nil
	}
	if lowerShadow > body*0.5 {
		return nil
	}

	// Should be in an uptrend
	if !d.isInUptrend(candles, idx) {
		return nil
	}

	volumeConfirm := d.hasVolumeConfirmation(c, avgVolume)

	return &analysis.Pattern{
		Name:          "Shooting Star",
		Type:          analysis.PatternTypeCandlestick,
		Direction:     analysis.PatternBearish,
		StartIndex:    idx,
		EndIndex:      idx,
		Strength:      d.calculateStrength(0.7, volumeConfirm),
		VolumeConfirm: volumeConfirm,
	}
}

// detectMarubozu detects Marubozu patterns (strong momentum candle)
func (d *CandlestickDetector) detectMarubozu(candles []models.Candle, idx int, avgVolume float64) *analysis.Pattern {
	c := candles[idx]
	rng := d.candleRange(c)
	if rng == 0 {
		return nil
	}

	body := d.bodySize(c)
	bodyRatio := body / rng

	// Marubozu: body is almost the entire range (>90%)
	if bodyRatio < 0.9 {
		return nil
	}

	volumeConfirm := d.hasVolumeConfirmation(c, avgVolume)
	direction := analysis.PatternBullish
	if d.isBearish(c) {
		direction = analysis.PatternBearish
	}

	return &analysis.Pattern{
		Name:          "Marubozu",
		Type:          analysis.PatternTypeCandlestick,
		Direction:     direction,
		StartIndex:    idx,
		EndIndex:      idx,
		Strength:      d.calculateStrength(0.8, volumeConfirm),
		VolumeConfirm: volumeConfirm,
	}
}

// detectSpinningTop detects Spinning Top patterns (indecision)
func (d *CandlestickDetector) detectSpinningTop(candles []models.Candle, idx int, avgVolume float64) *analysis.Pattern {
	c := candles[idx]
	rng := d.candleRange(c)
	if rng == 0 {
		return nil
	}

	body := d.bodySize(c)
	bodyRatio := body / rng
	upperShadow := d.upperShadow(c)
	lowerShadow := d.lowerShadow(c)

	// Spinning Top: small body with shadows on both sides
	if bodyRatio > 0.3 || bodyRatio < d.dojiThreshold {
		return nil
	}
	// Both shadows should be significant
	if upperShadow < body || lowerShadow < body {
		return nil
	}

	volumeConfirm := d.hasVolumeConfirmation(c, avgVolume)

	return &analysis.Pattern{
		Name:          "Spinning Top",
		Type:          analysis.PatternTypeCandlestick,
		Direction:     analysis.PatternNeutral,
		StartIndex:    idx,
		EndIndex:      idx,
		Strength:      d.calculateStrength(0.4, volumeConfirm),
		VolumeConfirm: volumeConfirm,
	}
}


// Two-candle pattern detection

// detectEngulfing detects Bullish and Bearish Engulfing patterns
func (d *CandlestickDetector) detectEngulfing(candles []models.Candle, idx int, avgVolume float64) *analysis.Pattern {
	if idx < 1 {
		return nil
	}

	prev := candles[idx-1]
	curr := candles[idx]

	prevBody := d.bodySize(prev)
	currBody := d.bodySize(curr)

	// Current body must be larger than previous
	if currBody <= prevBody {
		return nil
	}

	volumeConfirm := d.hasVolumeConfirmation(curr, avgVolume)

	// Bullish Engulfing: bearish candle followed by bullish candle that engulfs it
	if d.isBearish(prev) && d.isBullish(curr) {
		if curr.Open <= prev.Close && curr.Close >= prev.Open {
			return &analysis.Pattern{
				Name:          "Bullish Engulfing",
				Type:          analysis.PatternTypeCandlestick,
				Direction:     analysis.PatternBullish,
				StartIndex:    idx - 1,
				EndIndex:      idx,
				Strength:      d.calculateStrength(0.8, volumeConfirm),
				VolumeConfirm: volumeConfirm,
			}
		}
	}

	// Bearish Engulfing: bullish candle followed by bearish candle that engulfs it
	if d.isBullish(prev) && d.isBearish(curr) {
		if curr.Open >= prev.Close && curr.Close <= prev.Open {
			return &analysis.Pattern{
				Name:          "Bearish Engulfing",
				Type:          analysis.PatternTypeCandlestick,
				Direction:     analysis.PatternBearish,
				StartIndex:    idx - 1,
				EndIndex:      idx,
				Strength:      d.calculateStrength(0.8, volumeConfirm),
				VolumeConfirm: volumeConfirm,
			}
		}
	}

	return nil
}

// detectPiercingLine detects Piercing Line pattern (bullish reversal)
func (d *CandlestickDetector) detectPiercingLine(candles []models.Candle, idx int, avgVolume float64) *analysis.Pattern {
	if idx < 1 {
		return nil
	}

	prev := candles[idx-1]
	curr := candles[idx]

	// First candle must be bearish, second must be bullish
	if !d.isBearish(prev) || !d.isBullish(curr) {
		return nil
	}

	// Current opens below previous low
	if curr.Open >= prev.Low {
		return nil
	}

	// Current closes above midpoint of previous body
	prevMidpoint := (prev.Open + prev.Close) / 2
	if curr.Close < prevMidpoint {
		return nil
	}

	// Current should not close above previous open
	if curr.Close >= prev.Open {
		return nil
	}

	volumeConfirm := d.hasVolumeConfirmation(curr, avgVolume)

	return &analysis.Pattern{
		Name:          "Piercing Line",
		Type:          analysis.PatternTypeCandlestick,
		Direction:     analysis.PatternBullish,
		StartIndex:    idx - 1,
		EndIndex:      idx,
		Strength:      d.calculateStrength(0.7, volumeConfirm),
		VolumeConfirm: volumeConfirm,
	}
}

// detectDarkCloudCover detects Dark Cloud Cover pattern (bearish reversal)
func (d *CandlestickDetector) detectDarkCloudCover(candles []models.Candle, idx int, avgVolume float64) *analysis.Pattern {
	if idx < 1 {
		return nil
	}

	prev := candles[idx-1]
	curr := candles[idx]

	// First candle must be bullish, second must be bearish
	if !d.isBullish(prev) || !d.isBearish(curr) {
		return nil
	}

	// Current opens above previous high
	if curr.Open <= prev.High {
		return nil
	}

	// Current closes below midpoint of previous body
	prevMidpoint := (prev.Open + prev.Close) / 2
	if curr.Close > prevMidpoint {
		return nil
	}

	// Current should not close below previous open
	if curr.Close <= prev.Open {
		return nil
	}

	volumeConfirm := d.hasVolumeConfirmation(curr, avgVolume)

	return &analysis.Pattern{
		Name:          "Dark Cloud Cover",
		Type:          analysis.PatternTypeCandlestick,
		Direction:     analysis.PatternBearish,
		StartIndex:    idx - 1,
		EndIndex:      idx,
		Strength:      d.calculateStrength(0.7, volumeConfirm),
		VolumeConfirm: volumeConfirm,
	}
}

// detectTweezer detects Tweezer Top and Bottom patterns
func (d *CandlestickDetector) detectTweezer(candles []models.Candle, idx int, avgVolume float64) *analysis.Pattern {
	if idx < 1 {
		return nil
	}

	prev := candles[idx-1]
	curr := candles[idx]

	tolerance := d.candleRange(prev) * 0.05 // 5% tolerance

	volumeConfirm := d.hasVolumeConfirmation(curr, avgVolume)

	// Tweezer Bottom: two candles with same low in downtrend
	if abs(prev.Low-curr.Low) <= tolerance {
		if d.isBearish(prev) && d.isBullish(curr) && d.isInDowntrend(candles, idx-1) {
			return &analysis.Pattern{
				Name:          "Tweezer Bottom",
				Type:          analysis.PatternTypeCandlestick,
				Direction:     analysis.PatternBullish,
				StartIndex:    idx - 1,
				EndIndex:      idx,
				Strength:      d.calculateStrength(0.65, volumeConfirm),
				VolumeConfirm: volumeConfirm,
			}
		}
	}

	// Tweezer Top: two candles with same high in uptrend
	if abs(prev.High-curr.High) <= tolerance {
		if d.isBullish(prev) && d.isBearish(curr) && d.isInUptrend(candles, idx-1) {
			return &analysis.Pattern{
				Name:          "Tweezer Top",
				Type:          analysis.PatternTypeCandlestick,
				Direction:     analysis.PatternBearish,
				StartIndex:    idx - 1,
				EndIndex:      idx,
				Strength:      d.calculateStrength(0.65, volumeConfirm),
				VolumeConfirm: volumeConfirm,
			}
		}
	}

	return nil
}

// detectHarami detects Bullish and Bearish Harami patterns
func (d *CandlestickDetector) detectHarami(candles []models.Candle, idx int, avgVolume float64) *analysis.Pattern {
	if idx < 1 {
		return nil
	}

	prev := candles[idx-1]
	curr := candles[idx]

	prevBody := d.bodySize(prev)
	currBody := d.bodySize(curr)

	// Current body must be smaller and contained within previous body
	if currBody >= prevBody {
		return nil
	}

	volumeConfirm := d.hasVolumeConfirmation(curr, avgVolume)

	// Bullish Harami: bearish candle followed by smaller bullish candle inside it
	if d.isBearish(prev) && d.isBullish(curr) {
		if curr.Open >= prev.Close && curr.Close <= prev.Open {
			return &analysis.Pattern{
				Name:          "Bullish Harami",
				Type:          analysis.PatternTypeCandlestick,
				Direction:     analysis.PatternBullish,
				StartIndex:    idx - 1,
				EndIndex:      idx,
				Strength:      d.calculateStrength(0.6, volumeConfirm),
				VolumeConfirm: volumeConfirm,
			}
		}
	}

	// Bearish Harami: bullish candle followed by smaller bearish candle inside it
	if d.isBullish(prev) && d.isBearish(curr) {
		if curr.Open <= prev.Close && curr.Close >= prev.Open {
			return &analysis.Pattern{
				Name:          "Bearish Harami",
				Type:          analysis.PatternTypeCandlestick,
				Direction:     analysis.PatternBearish,
				StartIndex:    idx - 1,
				EndIndex:      idx,
				Strength:      d.calculateStrength(0.6, volumeConfirm),
				VolumeConfirm: volumeConfirm,
			}
		}
	}

	return nil
}


// Three-candle pattern detection

// detectMorningStar detects Morning Star pattern (bullish reversal)
func (d *CandlestickDetector) detectMorningStar(candles []models.Candle, idx int, avgVolume float64) *analysis.Pattern {
	if idx < 2 {
		return nil
	}

	first := candles[idx-2]
	second := candles[idx-1]
	third := candles[idx]

	// First candle: long bearish
	firstBody := d.bodySize(first)
	firstRange := d.candleRange(first)
	if firstRange == 0 || firstBody/firstRange < d.longBodyThreshold || !d.isBearish(first) {
		return nil
	}

	// Second candle: small body (star) - gaps down
	secondBody := d.bodySize(second)
	secondRange := d.candleRange(second)
	if secondRange > 0 && secondBody/secondRange > 0.3 {
		return nil
	}
	// Star should gap down from first candle
	if max(second.Open, second.Close) >= first.Close {
		return nil
	}

	// Third candle: long bullish that closes above midpoint of first
	thirdBody := d.bodySize(third)
	thirdRange := d.candleRange(third)
	if thirdRange == 0 || thirdBody/thirdRange < d.longBodyThreshold || !d.isBullish(third) {
		return nil
	}
	firstMidpoint := (first.Open + first.Close) / 2
	if third.Close < firstMidpoint {
		return nil
	}

	volumeConfirm := d.hasVolumeConfirmation(third, avgVolume)

	return &analysis.Pattern{
		Name:          "Morning Star",
		Type:          analysis.PatternTypeCandlestick,
		Direction:     analysis.PatternBullish,
		StartIndex:    idx - 2,
		EndIndex:      idx,
		Strength:      d.calculateStrength(0.85, volumeConfirm),
		VolumeConfirm: volumeConfirm,
	}
}

// detectEveningStar detects Evening Star pattern (bearish reversal)
func (d *CandlestickDetector) detectEveningStar(candles []models.Candle, idx int, avgVolume float64) *analysis.Pattern {
	if idx < 2 {
		return nil
	}

	first := candles[idx-2]
	second := candles[idx-1]
	third := candles[idx]

	// First candle: long bullish
	firstBody := d.bodySize(first)
	firstRange := d.candleRange(first)
	if firstRange == 0 || firstBody/firstRange < d.longBodyThreshold || !d.isBullish(first) {
		return nil
	}

	// Second candle: small body (star) - gaps up
	secondBody := d.bodySize(second)
	secondRange := d.candleRange(second)
	if secondRange > 0 && secondBody/secondRange > 0.3 {
		return nil
	}
	// Star should gap up from first candle
	if min(second.Open, second.Close) <= first.Close {
		return nil
	}

	// Third candle: long bearish that closes below midpoint of first
	thirdBody := d.bodySize(third)
	thirdRange := d.candleRange(third)
	if thirdRange == 0 || thirdBody/thirdRange < d.longBodyThreshold || !d.isBearish(third) {
		return nil
	}
	firstMidpoint := (first.Open + first.Close) / 2
	if third.Close > firstMidpoint {
		return nil
	}

	volumeConfirm := d.hasVolumeConfirmation(third, avgVolume)

	return &analysis.Pattern{
		Name:          "Evening Star",
		Type:          analysis.PatternTypeCandlestick,
		Direction:     analysis.PatternBearish,
		StartIndex:    idx - 2,
		EndIndex:      idx,
		Strength:      d.calculateStrength(0.85, volumeConfirm),
		VolumeConfirm: volumeConfirm,
	}
}

// detectThreeWhiteSoldiers detects Three White Soldiers pattern (bullish continuation)
func (d *CandlestickDetector) detectThreeWhiteSoldiers(candles []models.Candle, idx int, avgVolume float64) *analysis.Pattern {
	if idx < 2 {
		return nil
	}

	first := candles[idx-2]
	second := candles[idx-1]
	third := candles[idx]

	// All three candles must be bullish
	if !d.isBullish(first) || !d.isBullish(second) || !d.isBullish(third) {
		return nil
	}

	// Each candle should have a decent body
	firstRange := d.candleRange(first)
	secondRange := d.candleRange(second)
	thirdRange := d.candleRange(third)
	if firstRange == 0 || secondRange == 0 || thirdRange == 0 {
		return nil
	}

	firstBodyRatio := d.bodySize(first) / firstRange
	secondBodyRatio := d.bodySize(second) / secondRange
	thirdBodyRatio := d.bodySize(third) / thirdRange

	if firstBodyRatio < 0.5 || secondBodyRatio < 0.5 || thirdBodyRatio < 0.5 {
		return nil
	}

	// Each candle should open within the body of the previous candle
	if second.Open < first.Open || second.Open > first.Close {
		return nil
	}
	if third.Open < second.Open || third.Open > second.Close {
		return nil
	}

	// Each candle should close higher than the previous
	if second.Close <= first.Close || third.Close <= second.Close {
		return nil
	}

	volumeConfirm := d.hasVolumeConfirmation(third, avgVolume)

	return &analysis.Pattern{
		Name:          "Three White Soldiers",
		Type:          analysis.PatternTypeCandlestick,
		Direction:     analysis.PatternBullish,
		StartIndex:    idx - 2,
		EndIndex:      idx,
		Strength:      d.calculateStrength(0.9, volumeConfirm),
		VolumeConfirm: volumeConfirm,
	}
}

// detectThreeBlackCrows detects Three Black Crows pattern (bearish continuation)
func (d *CandlestickDetector) detectThreeBlackCrows(candles []models.Candle, idx int, avgVolume float64) *analysis.Pattern {
	if idx < 2 {
		return nil
	}

	first := candles[idx-2]
	second := candles[idx-1]
	third := candles[idx]

	// All three candles must be bearish
	if !d.isBearish(first) || !d.isBearish(second) || !d.isBearish(third) {
		return nil
	}

	// Each candle should have a decent body
	firstRange := d.candleRange(first)
	secondRange := d.candleRange(second)
	thirdRange := d.candleRange(third)
	if firstRange == 0 || secondRange == 0 || thirdRange == 0 {
		return nil
	}

	firstBodyRatio := d.bodySize(first) / firstRange
	secondBodyRatio := d.bodySize(second) / secondRange
	thirdBodyRatio := d.bodySize(third) / thirdRange

	if firstBodyRatio < 0.5 || secondBodyRatio < 0.5 || thirdBodyRatio < 0.5 {
		return nil
	}

	// Each candle should open within the body of the previous candle
	if second.Open > first.Open || second.Open < first.Close {
		return nil
	}
	if third.Open > second.Open || third.Open < second.Close {
		return nil
	}

	// Each candle should close lower than the previous
	if second.Close >= first.Close || third.Close >= second.Close {
		return nil
	}

	volumeConfirm := d.hasVolumeConfirmation(third, avgVolume)

	return &analysis.Pattern{
		Name:          "Three Black Crows",
		Type:          analysis.PatternTypeCandlestick,
		Direction:     analysis.PatternBearish,
		StartIndex:    idx - 2,
		EndIndex:      idx,
		Strength:      d.calculateStrength(0.9, volumeConfirm),
		VolumeConfirm: volumeConfirm,
	}
}

// Helper functions
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
