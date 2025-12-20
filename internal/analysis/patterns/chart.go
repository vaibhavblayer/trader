// Package patterns provides chart and candlestick pattern detection.
package patterns

import (
	"math"

	"zerodha-trader/internal/analysis"
	"zerodha-trader/internal/models"
)

// ChartPatternDetector detects chart patterns in price data.
type ChartPatternDetector struct {
	minPatternBars    int     // Minimum bars for pattern formation
	maxPatternBars    int     // Maximum bars for pattern formation
	tolerancePercent  float64 // Tolerance for level matching
	minSwingStrength  int     // Minimum bars for swing point confirmation
}

// NewChartPatternDetector creates a new chart pattern detector.
func NewChartPatternDetector() *ChartPatternDetector {
	return &ChartPatternDetector{
		minPatternBars:   10,
		maxPatternBars:   100,
		tolerancePercent: 0.02, // 2% tolerance
		minSwingStrength: 3,
	}
}

func (d *ChartPatternDetector) Name() string {
	return "ChartPatternDetector"
}

// SwingPoint represents a swing high or low point.
type SwingPoint struct {
	Index     int
	Price     float64
	IsHigh    bool
	Strength  int // Number of bars confirming the swing
}

// Detect detects all chart patterns in the given candles.
func (d *ChartPatternDetector) Detect(candles []models.Candle) ([]analysis.Pattern, error) {
	if len(candles) < d.minPatternBars {
		return nil, nil
	}

	var patterns []analysis.Pattern

	// Find swing points
	swings := d.findSwingPoints(candles)
	if len(swings) < 3 {
		return nil, nil
	}

	// Detect various chart patterns
	if p := d.detectHeadAndShoulders(candles, swings); p != nil {
		patterns = append(patterns, *p)
	}
	if p := d.detectInverseHeadAndShoulders(candles, swings); p != nil {
		patterns = append(patterns, *p)
	}
	if p := d.detectDoubleTop(candles, swings); p != nil {
		patterns = append(patterns, *p)
	}
	if p := d.detectDoubleBottom(candles, swings); p != nil {
		patterns = append(patterns, *p)
	}
	if p := d.detectTripleTop(candles, swings); p != nil {
		patterns = append(patterns, *p)
	}
	if p := d.detectTripleBottom(candles, swings); p != nil {
		patterns = append(patterns, *p)
	}
	if p := d.detectAscendingTriangle(candles, swings); p != nil {
		patterns = append(patterns, *p)
	}
	if p := d.detectDescendingTriangle(candles, swings); p != nil {
		patterns = append(patterns, *p)
	}
	if p := d.detectSymmetricalTriangle(candles, swings); p != nil {
		patterns = append(patterns, *p)
	}
	if p := d.detectRisingWedge(candles, swings); p != nil {
		patterns = append(patterns, *p)
	}
	if p := d.detectFallingWedge(candles, swings); p != nil {
		patterns = append(patterns, *p)
	}
	if p := d.detectFlag(candles, swings); p != nil {
		patterns = append(patterns, *p)
	}
	if p := d.detectPennant(candles, swings); p != nil {
		patterns = append(patterns, *p)
	}
	if p := d.detectCupAndHandle(candles, swings); p != nil {
		patterns = append(patterns, *p)
	}
	if p := d.detectRoundingBottom(candles, swings); p != nil {
		patterns = append(patterns, *p)
	}
	if p := d.detectRectangle(candles, swings); p != nil {
		patterns = append(patterns, *p)
	}

	return patterns, nil
}

// findSwingPoints identifies swing highs and lows in the price data.
func (d *ChartPatternDetector) findSwingPoints(candles []models.Candle) []SwingPoint {
	var swings []SwingPoint
	n := len(candles)

	for i := d.minSwingStrength; i < n-d.minSwingStrength; i++ {
		// Check for swing high
		isSwingHigh := true
		for j := 1; j <= d.minSwingStrength; j++ {
			if candles[i].High <= candles[i-j].High || candles[i].High <= candles[i+j].High {
				isSwingHigh = false
				break
			}
		}
		if isSwingHigh {
			swings = append(swings, SwingPoint{
				Index:    i,
				Price:    candles[i].High,
				IsHigh:   true,
				Strength: d.minSwingStrength,
			})
		}

		// Check for swing low
		isSwingLow := true
		for j := 1; j <= d.minSwingStrength; j++ {
			if candles[i].Low >= candles[i-j].Low || candles[i].Low >= candles[i+j].Low {
				isSwingLow = false
				break
			}
		}
		if isSwingLow {
			swings = append(swings, SwingPoint{
				Index:    i,
				Price:    candles[i].Low,
				IsHigh:   false,
				Strength: d.minSwingStrength,
			})
		}
	}

	return swings
}

// Helper to check if two prices are approximately equal
func (d *ChartPatternDetector) pricesEqual(p1, p2 float64) bool {
	if p1 == 0 {
		return p2 == 0
	}
	return math.Abs(p1-p2)/p1 <= d.tolerancePercent
}

// getSwingHighs returns only swing highs from the swing points
func (d *ChartPatternDetector) getSwingHighs(swings []SwingPoint) []SwingPoint {
	var highs []SwingPoint
	for _, s := range swings {
		if s.IsHigh {
			highs = append(highs, s)
		}
	}
	return highs
}

// getSwingLows returns only swing lows from the swing points
func (d *ChartPatternDetector) getSwingLows(swings []SwingPoint) []SwingPoint {
	var lows []SwingPoint
	for _, s := range swings {
		if !s.IsHigh {
			lows = append(lows, s)
		}
	}
	return lows
}

// calculateTargetPrice calculates the target price based on pattern height
func (d *ChartPatternDetector) calculateTargetPrice(breakoutPrice, patternHeight float64, isBullish bool) float64 {
	if isBullish {
		return breakoutPrice + patternHeight
	}
	return breakoutPrice - patternHeight
}

// calculateCompletion calculates pattern completion percentage
func (d *ChartPatternDetector) calculateCompletion(candles []models.Candle, neckline float64, isBullish bool) float64 {
	if len(candles) == 0 {
		return 0
	}
	lastClose := candles[len(candles)-1].Close
	
	if isBullish {
		if lastClose > neckline {
			return 1.0 // Pattern complete (breakout)
		}
		// Calculate how close we are to neckline
		return 0.8 // Pattern forming
	}
	
	if lastClose < neckline {
		return 1.0 // Pattern complete (breakdown)
	}
	return 0.8 // Pattern forming
}


// Head and Shoulders pattern detection

// detectHeadAndShoulders detects Head and Shoulders pattern (bearish reversal)
func (d *ChartPatternDetector) detectHeadAndShoulders(candles []models.Candle, swings []SwingPoint) *analysis.Pattern {
	highs := d.getSwingHighs(swings)
	lows := d.getSwingLows(swings)

	if len(highs) < 3 || len(lows) < 2 {
		return nil
	}

	// Look for pattern in recent swings
	for i := len(highs) - 1; i >= 2; i-- {
		head := highs[i-1]
		leftShoulder := highs[i-2]
		rightShoulder := highs[i]

		// Head must be higher than both shoulders
		if head.Price <= leftShoulder.Price || head.Price <= rightShoulder.Price {
			continue
		}

		// Shoulders should be approximately equal
		if !d.pricesEqual(leftShoulder.Price, rightShoulder.Price) {
			continue
		}

		// Find neckline (lows between shoulders and head)
		var necklineLows []SwingPoint
		for _, low := range lows {
			if low.Index > leftShoulder.Index && low.Index < rightShoulder.Index {
				necklineLows = append(necklineLows, low)
			}
		}

		if len(necklineLows) < 2 {
			continue
		}

		// Calculate neckline
		neckline := (necklineLows[0].Price + necklineLows[len(necklineLows)-1].Price) / 2
		patternHeight := head.Price - neckline
		targetPrice := d.calculateTargetPrice(neckline, patternHeight, false)
		completion := d.calculateCompletion(candles, neckline, false)

		return &analysis.Pattern{
			Name:        "Head and Shoulders",
			Type:        analysis.PatternTypeChart,
			Direction:   analysis.PatternBearish,
			StartIndex:  leftShoulder.Index,
			EndIndex:    rightShoulder.Index,
			Strength:    0.85,
			TargetPrice: targetPrice,
			Completion:  completion,
		}
	}

	return nil
}

// detectInverseHeadAndShoulders detects Inverse Head and Shoulders pattern (bullish reversal)
func (d *ChartPatternDetector) detectInverseHeadAndShoulders(candles []models.Candle, swings []SwingPoint) *analysis.Pattern {
	highs := d.getSwingHighs(swings)
	lows := d.getSwingLows(swings)

	if len(lows) < 3 || len(highs) < 2 {
		return nil
	}

	// Look for pattern in recent swings
	for i := len(lows) - 1; i >= 2; i-- {
		head := lows[i-1]
		leftShoulder := lows[i-2]
		rightShoulder := lows[i]

		// Head must be lower than both shoulders
		if head.Price >= leftShoulder.Price || head.Price >= rightShoulder.Price {
			continue
		}

		// Shoulders should be approximately equal
		if !d.pricesEqual(leftShoulder.Price, rightShoulder.Price) {
			continue
		}

		// Find neckline (highs between shoulders and head)
		var necklineHighs []SwingPoint
		for _, high := range highs {
			if high.Index > leftShoulder.Index && high.Index < rightShoulder.Index {
				necklineHighs = append(necklineHighs, high)
			}
		}

		if len(necklineHighs) < 2 {
			continue
		}

		// Calculate neckline
		neckline := (necklineHighs[0].Price + necklineHighs[len(necklineHighs)-1].Price) / 2
		patternHeight := neckline - head.Price
		targetPrice := d.calculateTargetPrice(neckline, patternHeight, true)
		completion := d.calculateCompletion(candles, neckline, true)

		return &analysis.Pattern{
			Name:        "Inverse Head and Shoulders",
			Type:        analysis.PatternTypeChart,
			Direction:   analysis.PatternBullish,
			StartIndex:  leftShoulder.Index,
			EndIndex:    rightShoulder.Index,
			Strength:    0.85,
			TargetPrice: targetPrice,
			Completion:  completion,
		}
	}

	return nil
}

// Double Top/Bottom pattern detection

// detectDoubleTop detects Double Top pattern (bearish reversal)
func (d *ChartPatternDetector) detectDoubleTop(candles []models.Candle, swings []SwingPoint) *analysis.Pattern {
	highs := d.getSwingHighs(swings)
	lows := d.getSwingLows(swings)

	if len(highs) < 2 || len(lows) < 1 {
		return nil
	}

	// Look for two approximately equal highs
	for i := len(highs) - 1; i >= 1; i-- {
		first := highs[i-1]
		second := highs[i]

		if !d.pricesEqual(first.Price, second.Price) {
			continue
		}

		// Find the low between the two highs (neckline)
		var middleLow *SwingPoint
		for j := range lows {
			if lows[j].Index > first.Index && lows[j].Index < second.Index {
				if middleLow == nil || lows[j].Price < middleLow.Price {
					middleLow = &lows[j]
				}
			}
		}

		if middleLow == nil {
			continue
		}

		neckline := middleLow.Price
		patternHeight := first.Price - neckline
		targetPrice := d.calculateTargetPrice(neckline, patternHeight, false)
		completion := d.calculateCompletion(candles, neckline, false)

		return &analysis.Pattern{
			Name:        "Double Top",
			Type:        analysis.PatternTypeChart,
			Direction:   analysis.PatternBearish,
			StartIndex:  first.Index,
			EndIndex:    second.Index,
			Strength:    0.75,
			TargetPrice: targetPrice,
			Completion:  completion,
		}
	}

	return nil
}

// detectDoubleBottom detects Double Bottom pattern (bullish reversal)
func (d *ChartPatternDetector) detectDoubleBottom(candles []models.Candle, swings []SwingPoint) *analysis.Pattern {
	highs := d.getSwingHighs(swings)
	lows := d.getSwingLows(swings)

	if len(lows) < 2 || len(highs) < 1 {
		return nil
	}

	// Look for two approximately equal lows
	for i := len(lows) - 1; i >= 1; i-- {
		first := lows[i-1]
		second := lows[i]

		if !d.pricesEqual(first.Price, second.Price) {
			continue
		}

		// Find the high between the two lows (neckline)
		var middleHigh *SwingPoint
		for j := range highs {
			if highs[j].Index > first.Index && highs[j].Index < second.Index {
				if middleHigh == nil || highs[j].Price > middleHigh.Price {
					middleHigh = &highs[j]
				}
			}
		}

		if middleHigh == nil {
			continue
		}

		neckline := middleHigh.Price
		patternHeight := neckline - first.Price
		targetPrice := d.calculateTargetPrice(neckline, patternHeight, true)
		completion := d.calculateCompletion(candles, neckline, true)

		return &analysis.Pattern{
			Name:        "Double Bottom",
			Type:        analysis.PatternTypeChart,
			Direction:   analysis.PatternBullish,
			StartIndex:  first.Index,
			EndIndex:    second.Index,
			Strength:    0.75,
			TargetPrice: targetPrice,
			Completion:  completion,
		}
	}

	return nil
}

// detectTripleTop detects Triple Top pattern (bearish reversal)
func (d *ChartPatternDetector) detectTripleTop(candles []models.Candle, swings []SwingPoint) *analysis.Pattern {
	highs := d.getSwingHighs(swings)
	lows := d.getSwingLows(swings)

	if len(highs) < 3 || len(lows) < 2 {
		return nil
	}

	// Look for three approximately equal highs
	for i := len(highs) - 1; i >= 2; i-- {
		first := highs[i-2]
		second := highs[i-1]
		third := highs[i]

		if !d.pricesEqual(first.Price, second.Price) || !d.pricesEqual(second.Price, third.Price) {
			continue
		}

		// Find the lowest low between the highs (neckline)
		var necklinePrice float64 = math.MaxFloat64
		for _, low := range lows {
			if low.Index > first.Index && low.Index < third.Index {
				if low.Price < necklinePrice {
					necklinePrice = low.Price
				}
			}
		}

		if necklinePrice == math.MaxFloat64 {
			continue
		}

		patternHeight := first.Price - necklinePrice
		targetPrice := d.calculateTargetPrice(necklinePrice, patternHeight, false)
		completion := d.calculateCompletion(candles, necklinePrice, false)

		return &analysis.Pattern{
			Name:        "Triple Top",
			Type:        analysis.PatternTypeChart,
			Direction:   analysis.PatternBearish,
			StartIndex:  first.Index,
			EndIndex:    third.Index,
			Strength:    0.8,
			TargetPrice: targetPrice,
			Completion:  completion,
		}
	}

	return nil
}

// detectTripleBottom detects Triple Bottom pattern (bullish reversal)
func (d *ChartPatternDetector) detectTripleBottom(candles []models.Candle, swings []SwingPoint) *analysis.Pattern {
	highs := d.getSwingHighs(swings)
	lows := d.getSwingLows(swings)

	if len(lows) < 3 || len(highs) < 2 {
		return nil
	}

	// Look for three approximately equal lows
	for i := len(lows) - 1; i >= 2; i-- {
		first := lows[i-2]
		second := lows[i-1]
		third := lows[i]

		if !d.pricesEqual(first.Price, second.Price) || !d.pricesEqual(second.Price, third.Price) {
			continue
		}

		// Find the highest high between the lows (neckline)
		var necklinePrice float64 = 0
		for _, high := range highs {
			if high.Index > first.Index && high.Index < third.Index {
				if high.Price > necklinePrice {
					necklinePrice = high.Price
				}
			}
		}

		if necklinePrice == 0 {
			continue
		}

		patternHeight := necklinePrice - first.Price
		targetPrice := d.calculateTargetPrice(necklinePrice, patternHeight, true)
		completion := d.calculateCompletion(candles, necklinePrice, true)

		return &analysis.Pattern{
			Name:        "Triple Bottom",
			Type:        analysis.PatternTypeChart,
			Direction:   analysis.PatternBullish,
			StartIndex:  first.Index,
			EndIndex:    third.Index,
			Strength:    0.8,
			TargetPrice: targetPrice,
			Completion:  completion,
		}
	}

	return nil
}


// Triangle pattern detection

// detectAscendingTriangle detects Ascending Triangle pattern (bullish continuation)
func (d *ChartPatternDetector) detectAscendingTriangle(candles []models.Candle, swings []SwingPoint) *analysis.Pattern {
	highs := d.getSwingHighs(swings)
	lows := d.getSwingLows(swings)

	if len(highs) < 2 || len(lows) < 2 {
		return nil
	}

	// Look for flat resistance (equal highs) and rising support (higher lows)
	for i := len(highs) - 1; i >= 1; i-- {
		// Check for flat resistance
		if !d.pricesEqual(highs[i].Price, highs[i-1].Price) {
			continue
		}

		resistance := highs[i].Price
		startIdx := highs[i-1].Index
		endIdx := highs[i].Index

		// Find lows within this range
		var patternLows []SwingPoint
		for _, low := range lows {
			if low.Index >= startIdx && low.Index <= endIdx {
				patternLows = append(patternLows, low)
			}
		}

		if len(patternLows) < 2 {
			continue
		}

		// Check for rising lows
		isRising := true
		for j := 1; j < len(patternLows); j++ {
			if patternLows[j].Price <= patternLows[j-1].Price {
				isRising = false
				break
			}
		}

		if !isRising {
			continue
		}

		patternHeight := resistance - patternLows[0].Price
		targetPrice := d.calculateTargetPrice(resistance, patternHeight, true)
		completion := d.calculateCompletion(candles, resistance, true)

		return &analysis.Pattern{
			Name:        "Ascending Triangle",
			Type:        analysis.PatternTypeChart,
			Direction:   analysis.PatternBullish,
			StartIndex:  startIdx,
			EndIndex:    endIdx,
			Strength:    0.7,
			TargetPrice: targetPrice,
			Completion:  completion,
		}
	}

	return nil
}

// detectDescendingTriangle detects Descending Triangle pattern (bearish continuation)
func (d *ChartPatternDetector) detectDescendingTriangle(candles []models.Candle, swings []SwingPoint) *analysis.Pattern {
	highs := d.getSwingHighs(swings)
	lows := d.getSwingLows(swings)

	if len(highs) < 2 || len(lows) < 2 {
		return nil
	}

	// Look for flat support (equal lows) and falling resistance (lower highs)
	for i := len(lows) - 1; i >= 1; i-- {
		// Check for flat support
		if !d.pricesEqual(lows[i].Price, lows[i-1].Price) {
			continue
		}

		support := lows[i].Price
		startIdx := lows[i-1].Index
		endIdx := lows[i].Index

		// Find highs within this range
		var patternHighs []SwingPoint
		for _, high := range highs {
			if high.Index >= startIdx && high.Index <= endIdx {
				patternHighs = append(patternHighs, high)
			}
		}

		if len(patternHighs) < 2 {
			continue
		}

		// Check for falling highs
		isFalling := true
		for j := 1; j < len(patternHighs); j++ {
			if patternHighs[j].Price >= patternHighs[j-1].Price {
				isFalling = false
				break
			}
		}

		if !isFalling {
			continue
		}

		patternHeight := patternHighs[0].Price - support
		targetPrice := d.calculateTargetPrice(support, patternHeight, false)
		completion := d.calculateCompletion(candles, support, false)

		return &analysis.Pattern{
			Name:        "Descending Triangle",
			Type:        analysis.PatternTypeChart,
			Direction:   analysis.PatternBearish,
			StartIndex:  startIdx,
			EndIndex:    endIdx,
			Strength:    0.7,
			TargetPrice: targetPrice,
			Completion:  completion,
		}
	}

	return nil
}

// detectSymmetricalTriangle detects Symmetrical Triangle pattern (continuation)
func (d *ChartPatternDetector) detectSymmetricalTriangle(candles []models.Candle, swings []SwingPoint) *analysis.Pattern {
	highs := d.getSwingHighs(swings)
	lows := d.getSwingLows(swings)

	if len(highs) < 2 || len(lows) < 2 {
		return nil
	}

	// Look for converging trendlines (lower highs and higher lows)
	for i := len(highs) - 1; i >= 1; i-- {
		// Check for lower highs
		if highs[i].Price >= highs[i-1].Price {
			continue
		}

		startIdx := highs[i-1].Index
		endIdx := highs[i].Index

		// Find lows within this range
		var patternLows []SwingPoint
		for _, low := range lows {
			if low.Index >= startIdx && low.Index <= endIdx {
				patternLows = append(patternLows, low)
			}
		}

		if len(patternLows) < 2 {
			continue
		}

		// Check for higher lows
		isRising := true
		for j := 1; j < len(patternLows); j++ {
			if patternLows[j].Price <= patternLows[j-1].Price {
				isRising = false
				break
			}
		}

		if !isRising {
			continue
		}

		// Determine direction based on prior trend
		direction := analysis.PatternNeutral
		if startIdx > 10 {
			priorTrend := candles[startIdx].Close - candles[startIdx-10].Close
			if priorTrend > 0 {
				direction = analysis.PatternBullish
			} else {
				direction = analysis.PatternBearish
			}
		}

		patternHeight := highs[i-1].Price - patternLows[0].Price
		midPrice := (highs[i].Price + patternLows[len(patternLows)-1].Price) / 2
		var targetPrice float64
		if direction == analysis.PatternBullish {
			targetPrice = d.calculateTargetPrice(midPrice, patternHeight, true)
		} else {
			targetPrice = d.calculateTargetPrice(midPrice, patternHeight, false)
		}

		return &analysis.Pattern{
			Name:        "Symmetrical Triangle",
			Type:        analysis.PatternTypeChart,
			Direction:   direction,
			StartIndex:  startIdx,
			EndIndex:    endIdx,
			Strength:    0.65,
			TargetPrice: targetPrice,
			Completion:  0.8,
		}
	}

	return nil
}

// Wedge pattern detection

// detectRisingWedge detects Rising Wedge pattern (bearish reversal)
func (d *ChartPatternDetector) detectRisingWedge(candles []models.Candle, swings []SwingPoint) *analysis.Pattern {
	highs := d.getSwingHighs(swings)
	lows := d.getSwingLows(swings)

	if len(highs) < 2 || len(lows) < 2 {
		return nil
	}

	// Look for converging upward trendlines (higher highs and higher lows, but converging)
	for i := len(highs) - 1; i >= 1; i-- {
		// Check for higher highs
		if highs[i].Price <= highs[i-1].Price {
			continue
		}

		startIdx := highs[i-1].Index
		endIdx := highs[i].Index

		// Find lows within this range
		var patternLows []SwingPoint
		for _, low := range lows {
			if low.Index >= startIdx && low.Index <= endIdx {
				patternLows = append(patternLows, low)
			}
		}

		if len(patternLows) < 2 {
			continue
		}

		// Check for higher lows
		isRising := true
		for j := 1; j < len(patternLows); j++ {
			if patternLows[j].Price <= patternLows[j-1].Price {
				isRising = false
				break
			}
		}

		if !isRising {
			continue
		}

		// Check for convergence (slope of highs < slope of lows)
		highSlope := (highs[i].Price - highs[i-1].Price) / float64(highs[i].Index-highs[i-1].Index)
		lowSlope := (patternLows[len(patternLows)-1].Price - patternLows[0].Price) / float64(patternLows[len(patternLows)-1].Index-patternLows[0].Index)

		if highSlope >= lowSlope {
			continue
		}

		patternHeight := highs[i].Price - patternLows[len(patternLows)-1].Price
		targetPrice := d.calculateTargetPrice(patternLows[len(patternLows)-1].Price, patternHeight, false)

		return &analysis.Pattern{
			Name:        "Rising Wedge",
			Type:        analysis.PatternTypeChart,
			Direction:   analysis.PatternBearish,
			StartIndex:  startIdx,
			EndIndex:    endIdx,
			Strength:    0.7,
			TargetPrice: targetPrice,
			Completion:  0.8,
		}
	}

	return nil
}

// detectFallingWedge detects Falling Wedge pattern (bullish reversal)
func (d *ChartPatternDetector) detectFallingWedge(candles []models.Candle, swings []SwingPoint) *analysis.Pattern {
	highs := d.getSwingHighs(swings)
	lows := d.getSwingLows(swings)

	if len(highs) < 2 || len(lows) < 2 {
		return nil
	}

	// Look for converging downward trendlines (lower highs and lower lows, but converging)
	for i := len(lows) - 1; i >= 1; i-- {
		// Check for lower lows
		if lows[i].Price >= lows[i-1].Price {
			continue
		}

		startIdx := lows[i-1].Index
		endIdx := lows[i].Index

		// Find highs within this range
		var patternHighs []SwingPoint
		for _, high := range highs {
			if high.Index >= startIdx && high.Index <= endIdx {
				patternHighs = append(patternHighs, high)
			}
		}

		if len(patternHighs) < 2 {
			continue
		}

		// Check for lower highs
		isFalling := true
		for j := 1; j < len(patternHighs); j++ {
			if patternHighs[j].Price >= patternHighs[j-1].Price {
				isFalling = false
				break
			}
		}

		if !isFalling {
			continue
		}

		// Check for convergence (slope of lows > slope of highs, both negative)
		lowSlope := (lows[i].Price - lows[i-1].Price) / float64(lows[i].Index-lows[i-1].Index)
		highSlope := (patternHighs[len(patternHighs)-1].Price - patternHighs[0].Price) / float64(patternHighs[len(patternHighs)-1].Index-patternHighs[0].Index)

		if lowSlope <= highSlope {
			continue
		}

		patternHeight := patternHighs[len(patternHighs)-1].Price - lows[i].Price
		targetPrice := d.calculateTargetPrice(patternHighs[len(patternHighs)-1].Price, patternHeight, true)

		return &analysis.Pattern{
			Name:        "Falling Wedge",
			Type:        analysis.PatternTypeChart,
			Direction:   analysis.PatternBullish,
			StartIndex:  startIdx,
			EndIndex:    endIdx,
			Strength:    0.7,
			TargetPrice: targetPrice,
			Completion:  0.8,
		}
	}

	return nil
}


// Flag and Pennant pattern detection

// detectFlag detects Flag pattern (continuation)
func (d *ChartPatternDetector) detectFlag(candles []models.Candle, swings []SwingPoint) *analysis.Pattern {
	if len(candles) < 20 {
		return nil
	}

	highs := d.getSwingHighs(swings)
	lows := d.getSwingLows(swings)

	if len(highs) < 2 || len(lows) < 2 {
		return nil
	}

	// Look for a strong move (pole) followed by a parallel channel (flag)
	n := len(candles)
	
	// Find potential pole (strong move in last 20-50 bars)
	for poleEnd := n - 10; poleEnd >= 20; poleEnd-- {
		poleStart := poleEnd - 10
		if poleStart < 0 {
			continue
		}

		poleMove := candles[poleEnd].Close - candles[poleStart].Close
		poleRange := candles[poleEnd].High - candles[poleStart].Low
		
		// Pole should be a strong move (at least 5% of price)
		if math.Abs(poleMove)/candles[poleStart].Close < 0.05 {
			continue
		}

		isBullishPole := poleMove > 0

		// Look for flag formation after pole
		flagStart := poleEnd
		flagEnd := n - 1

		if flagEnd-flagStart < 5 {
			continue
		}

		// Flag should be a small counter-trend channel
		var flagHighs, flagLows []SwingPoint
		for _, h := range highs {
			if h.Index >= flagStart && h.Index <= flagEnd {
				flagHighs = append(flagHighs, h)
			}
		}
		for _, l := range lows {
			if l.Index >= flagStart && l.Index <= flagEnd {
				flagLows = append(flagLows, l)
			}
		}

		if len(flagHighs) < 2 || len(flagLows) < 2 {
			continue
		}

		// Check for parallel channel (flag)
		highSlope := (flagHighs[len(flagHighs)-1].Price - flagHighs[0].Price) / float64(flagHighs[len(flagHighs)-1].Index-flagHighs[0].Index)
		lowSlope := (flagLows[len(flagLows)-1].Price - flagLows[0].Price) / float64(flagLows[len(flagLows)-1].Index-flagLows[0].Index)

		// Slopes should be similar (parallel)
		if math.Abs(highSlope-lowSlope) > 0.01 {
			continue
		}

		// Flag should be counter-trend
		if isBullishPole && highSlope > 0 {
			continue
		}
		if !isBullishPole && highSlope < 0 {
			continue
		}

		direction := analysis.PatternBullish
		if !isBullishPole {
			direction = analysis.PatternBearish
		}

		targetPrice := d.calculateTargetPrice(candles[flagEnd].Close, math.Abs(poleRange), isBullishPole)

		return &analysis.Pattern{
			Name:        "Flag",
			Type:        analysis.PatternTypeChart,
			Direction:   direction,
			StartIndex:  poleStart,
			EndIndex:    flagEnd,
			Strength:    0.7,
			TargetPrice: targetPrice,
			Completion:  0.8,
		}
	}

	return nil
}

// detectPennant detects Pennant pattern (continuation)
func (d *ChartPatternDetector) detectPennant(candles []models.Candle, swings []SwingPoint) *analysis.Pattern {
	if len(candles) < 20 {
		return nil
	}

	highs := d.getSwingHighs(swings)
	lows := d.getSwingLows(swings)

	if len(highs) < 2 || len(lows) < 2 {
		return nil
	}

	n := len(candles)

	// Look for a strong move (pole) followed by a converging triangle (pennant)
	for poleEnd := n - 10; poleEnd >= 20; poleEnd-- {
		poleStart := poleEnd - 10
		if poleStart < 0 {
			continue
		}

		poleMove := candles[poleEnd].Close - candles[poleStart].Close
		poleRange := candles[poleEnd].High - candles[poleStart].Low

		// Pole should be a strong move
		if math.Abs(poleMove)/candles[poleStart].Close < 0.05 {
			continue
		}

		isBullishPole := poleMove > 0

		// Look for pennant formation after pole
		pennantStart := poleEnd
		pennantEnd := n - 1

		if pennantEnd-pennantStart < 5 {
			continue
		}

		// Pennant should be a converging triangle
		var pennantHighs, pennantLows []SwingPoint
		for _, h := range highs {
			if h.Index >= pennantStart && h.Index <= pennantEnd {
				pennantHighs = append(pennantHighs, h)
			}
		}
		for _, l := range lows {
			if l.Index >= pennantStart && l.Index <= pennantEnd {
				pennantLows = append(pennantLows, l)
			}
		}

		if len(pennantHighs) < 2 || len(pennantLows) < 2 {
			continue
		}

		// Check for converging lines
		highSlope := (pennantHighs[len(pennantHighs)-1].Price - pennantHighs[0].Price) / float64(pennantHighs[len(pennantHighs)-1].Index-pennantHighs[0].Index)
		lowSlope := (pennantLows[len(pennantLows)-1].Price - pennantLows[0].Price) / float64(pennantLows[len(pennantLows)-1].Index-pennantLows[0].Index)

		// Highs should be falling, lows should be rising (converging)
		if highSlope >= 0 || lowSlope <= 0 {
			continue
		}

		direction := analysis.PatternBullish
		if !isBullishPole {
			direction = analysis.PatternBearish
		}

		targetPrice := d.calculateTargetPrice(candles[pennantEnd].Close, math.Abs(poleRange), isBullishPole)

		return &analysis.Pattern{
			Name:        "Pennant",
			Type:        analysis.PatternTypeChart,
			Direction:   direction,
			StartIndex:  poleStart,
			EndIndex:    pennantEnd,
			Strength:    0.7,
			TargetPrice: targetPrice,
			Completion:  0.8,
		}
	}

	return nil
}

// Other pattern detection

// detectCupAndHandle detects Cup and Handle pattern (bullish continuation)
func (d *ChartPatternDetector) detectCupAndHandle(candles []models.Candle, swings []SwingPoint) *analysis.Pattern {
	lows := d.getSwingLows(swings)
	highs := d.getSwingHighs(swings)

	if len(lows) < 3 || len(highs) < 2 {
		return nil
	}

	// Look for U-shaped cup followed by small handle
	for i := len(lows) - 1; i >= 2; i-- {
		// Cup should have two highs at similar levels with a low in between
		cupLow := lows[i-1]
		
		// Find highs before and after the cup low
		var leftHigh, rightHigh *SwingPoint
		for j := range highs {
			if highs[j].Index < cupLow.Index {
				if leftHigh == nil || highs[j].Index > leftHigh.Index {
					leftHigh = &highs[j]
				}
			}
			if highs[j].Index > cupLow.Index {
				if rightHigh == nil || highs[j].Index < rightHigh.Index {
					rightHigh = &highs[j]
				}
			}
		}

		if leftHigh == nil || rightHigh == nil {
			continue
		}

		// Cup rim should be at similar levels
		if !d.pricesEqual(leftHigh.Price, rightHigh.Price) {
			continue
		}

		// Cup should be U-shaped (not V-shaped) - check depth
		cupDepth := leftHigh.Price - cupLow.Price
		cupWidth := rightHigh.Index - leftHigh.Index
		if cupWidth < 10 || cupDepth/leftHigh.Price < 0.1 {
			continue
		}

		// Look for handle (small pullback after right high)
		var handleLow *SwingPoint
		for j := range lows {
			if lows[j].Index > rightHigh.Index {
				if handleLow == nil || lows[j].Index < handleLow.Index {
					handleLow = &lows[j]
				}
			}
		}

		if handleLow == nil {
			continue
		}

		// Handle should be shallow (less than 50% of cup depth)
		handleDepth := rightHigh.Price - handleLow.Price
		if handleDepth > cupDepth*0.5 {
			continue
		}

		targetPrice := d.calculateTargetPrice(rightHigh.Price, cupDepth, true)

		return &analysis.Pattern{
			Name:        "Cup and Handle",
			Type:        analysis.PatternTypeChart,
			Direction:   analysis.PatternBullish,
			StartIndex:  leftHigh.Index,
			EndIndex:    handleLow.Index,
			Strength:    0.8,
			TargetPrice: targetPrice,
			Completion:  0.85,
		}
	}

	return nil
}

// detectRoundingBottom detects Rounding Bottom pattern (bullish reversal)
func (d *ChartPatternDetector) detectRoundingBottom(candles []models.Candle, swings []SwingPoint) *analysis.Pattern {
	if len(candles) < 30 {
		return nil
	}

	lows := d.getSwingLows(swings)
	if len(lows) < 5 {
		return nil
	}

	// Look for a gradual U-shaped bottom
	n := len(candles)
	
	// Find the lowest point in recent history
	var lowestIdx int
	var lowestPrice float64 = math.MaxFloat64
	for i := n/3; i < 2*n/3; i++ {
		if candles[i].Low < lowestPrice {
			lowestPrice = candles[i].Low
			lowestIdx = i
		}
	}

	// Check for gradual descent before lowest point
	leftSlope := (candles[lowestIdx].Close - candles[0].Close) / float64(lowestIdx)
	if leftSlope >= 0 {
		return nil
	}

	// Check for gradual ascent after lowest point
	rightSlope := (candles[n-1].Close - candles[lowestIdx].Close) / float64(n-1-lowestIdx)
	if rightSlope <= 0 {
		return nil
	}

	// Slopes should be roughly symmetric
	if math.Abs(leftSlope+rightSlope) > math.Abs(leftSlope)*0.5 {
		return nil
	}

	patternHeight := max(candles[0].High, candles[n-1].High) - lowestPrice
	targetPrice := d.calculateTargetPrice(candles[n-1].Close, patternHeight, true)

	return &analysis.Pattern{
		Name:        "Rounding Bottom",
		Type:        analysis.PatternTypeChart,
		Direction:   analysis.PatternBullish,
		StartIndex:  0,
		EndIndex:    n - 1,
		Strength:    0.7,
		TargetPrice: targetPrice,
		Completion:  0.75,
	}
}

// detectRectangle detects Rectangle pattern (continuation)
func (d *ChartPatternDetector) detectRectangle(candles []models.Candle, swings []SwingPoint) *analysis.Pattern {
	highs := d.getSwingHighs(swings)
	lows := d.getSwingLows(swings)

	if len(highs) < 2 || len(lows) < 2 {
		return nil
	}

	// Look for horizontal support and resistance
	for i := len(highs) - 1; i >= 1; i-- {
		// Check for flat resistance
		if !d.pricesEqual(highs[i].Price, highs[i-1].Price) {
			continue
		}

		resistance := highs[i].Price
		startIdx := highs[i-1].Index
		endIdx := highs[i].Index

		// Find lows within this range
		var patternLows []SwingPoint
		for _, low := range lows {
			if low.Index >= startIdx && low.Index <= endIdx {
				patternLows = append(patternLows, low)
			}
		}

		if len(patternLows) < 2 {
			continue
		}

		// Check for flat support
		isFlat := true
		for j := 1; j < len(patternLows); j++ {
			if !d.pricesEqual(patternLows[j].Price, patternLows[0].Price) {
				isFlat = false
				break
			}
		}

		if !isFlat {
			continue
		}

		support := patternLows[0].Price
		patternHeight := resistance - support

		// Determine direction based on prior trend
		direction := analysis.PatternNeutral
		if startIdx > 10 {
			priorTrend := candles[startIdx].Close - candles[startIdx-10].Close
			if priorTrend > 0 {
				direction = analysis.PatternBullish
			} else {
				direction = analysis.PatternBearish
			}
		}

		var targetPrice float64
		if direction == analysis.PatternBullish {
			targetPrice = d.calculateTargetPrice(resistance, patternHeight, true)
		} else {
			targetPrice = d.calculateTargetPrice(support, patternHeight, false)
		}

		return &analysis.Pattern{
			Name:        "Rectangle",
			Type:        analysis.PatternTypeChart,
			Direction:   direction,
			StartIndex:  startIdx,
			EndIndex:    endIdx,
			Strength:    0.65,
			TargetPrice: targetPrice,
			Completion:  0.8,
		}
	}

	return nil
}
