// Package patterns provides chart and candlestick pattern detection.
package patterns

import (
	"math"
	"sort"

	"zerodha-trader/internal/analysis"
	"zerodha-trader/internal/models"
)

// LevelAnalyzer identifies support and resistance levels in price data.
type LevelAnalyzer struct {
	pivotStrength     int     // Number of bars on each side for pivot confirmation
	clusterTolerance  float64 // Percentage tolerance for clustering levels
	minTouches        int     // Minimum touches to confirm a level
	maPeriods         []int   // Moving average periods for dynamic S/R
}

// NewLevelAnalyzer creates a new support/resistance level analyzer.
func NewLevelAnalyzer() *LevelAnalyzer {
	return &LevelAnalyzer{
		pivotStrength:    3,
		clusterTolerance: 0.01, // 1% tolerance
		minTouches:       2,
		maPeriods:        []int{20, 50, 100, 200},
	}
}

func (l *LevelAnalyzer) Name() string {
	return "LevelAnalyzer"
}

// PriceLevel represents a support or resistance level with metadata.
type PriceLevel struct {
	Price      float64
	Type       analysis.LevelType
	Strength   int     // Number of touches
	Source     string  // Source of the level (pivot, ma, trendline, etc.)
	FirstTouch int     // Index of first touch
	LastTouch  int     // Index of last touch
}

// LevelAnalysisResult contains all identified levels.
type LevelAnalysisResult struct {
	HorizontalLevels []PriceLevel
	DynamicLevels    []PriceLevel
	TrendlineLevels  []PriceLevel
	SupplyZones      []Zone
	DemandZones      []Zone
	PreviousPeriods  PreviousPeriodLevels
}

// Zone represents a supply or demand zone.
type Zone struct {
	UpperPrice float64
	LowerPrice float64
	Type       string // "supply" or "demand"
	Strength   int
	StartIndex int
	EndIndex   int
}

// PreviousPeriodLevels contains high/low levels from previous periods.
type PreviousPeriodLevels struct {
	PrevDayHigh   float64
	PrevDayLow    float64
	PrevWeekHigh  float64
	PrevWeekLow   float64
	PrevMonthHigh float64
	PrevMonthLow  float64
}

// Analyze performs comprehensive support/resistance analysis.
func (l *LevelAnalyzer) Analyze(candles []models.Candle) (*LevelAnalysisResult, error) {
	if len(candles) < l.pivotStrength*2+1 {
		return nil, nil
	}

	result := &LevelAnalysisResult{}

	// Find horizontal levels from price pivots
	result.HorizontalLevels = l.findHorizontalLevels(candles)

	// Find dynamic S/R from moving averages
	result.DynamicLevels = l.findDynamicLevels(candles)

	// Find trendline support/resistance
	result.TrendlineLevels = l.findTrendlineLevels(candles)

	// Find supply and demand zones
	result.SupplyZones, result.DemandZones = l.findSupplyDemandZones(candles)

	// Calculate previous period high/low
	result.PreviousPeriods = l.calculatePreviousPeriodLevels(candles)

	return result, nil
}

// findHorizontalLevels identifies horizontal support and resistance from price pivots.
func (l *LevelAnalyzer) findHorizontalLevels(candles []models.Candle) []PriceLevel {
	var levels []PriceLevel
	n := len(candles)

	// Find pivot highs and lows
	var pivotHighs, pivotLows []struct {
		index int
		price float64
	}

	for i := l.pivotStrength; i < n-l.pivotStrength; i++ {
		// Check for pivot high
		isPivotHigh := true
		for j := 1; j <= l.pivotStrength; j++ {
			if candles[i].High <= candles[i-j].High || candles[i].High <= candles[i+j].High {
				isPivotHigh = false
				break
			}
		}
		if isPivotHigh {
			pivotHighs = append(pivotHighs, struct {
				index int
				price float64
			}{i, candles[i].High})
		}

		// Check for pivot low
		isPivotLow := true
		for j := 1; j <= l.pivotStrength; j++ {
			if candles[i].Low >= candles[i-j].Low || candles[i].Low >= candles[i+j].Low {
				isPivotLow = false
				break
			}
		}
		if isPivotLow {
			pivotLows = append(pivotLows, struct {
				index int
				price float64
			}{i, candles[i].Low})
		}
	}

	// Cluster pivot highs into resistance levels
	resistanceLevels := l.clusterPivots(pivotHighs)
	for _, level := range resistanceLevels {
		if level.touches >= l.minTouches {
			levels = append(levels, PriceLevel{
				Price:      level.price,
				Type:       analysis.LevelResistance,
				Strength:   level.touches,
				Source:     "pivot",
				FirstTouch: level.firstIdx,
				LastTouch:  level.lastIdx,
			})
		}
	}

	// Cluster pivot lows into support levels
	supportLevels := l.clusterPivots(pivotLows)
	for _, level := range supportLevels {
		if level.touches >= l.minTouches {
			levels = append(levels, PriceLevel{
				Price:      level.price,
				Type:       analysis.LevelSupport,
				Strength:   level.touches,
				Source:     "pivot",
				FirstTouch: level.firstIdx,
				LastTouch:  level.lastIdx,
			})
		}
	}

	return levels
}

type clusteredLevel struct {
	price    float64
	touches  int
	firstIdx int
	lastIdx  int
}

// clusterPivots groups nearby pivot points into single levels.
func (l *LevelAnalyzer) clusterPivots(pivots []struct {
	index int
	price float64
}) []clusteredLevel {
	if len(pivots) == 0 {
		return nil
	}

	// Sort by price
	sorted := make([]struct {
		index int
		price float64
	}, len(pivots))
	copy(sorted, pivots)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].price < sorted[j].price
	})

	var clusters []clusteredLevel
	currentCluster := clusteredLevel{
		price:    sorted[0].price,
		touches:  1,
		firstIdx: sorted[0].index,
		lastIdx:  sorted[0].index,
	}

	for i := 1; i < len(sorted); i++ {
		// Check if this pivot is within tolerance of current cluster
		if math.Abs(sorted[i].price-currentCluster.price)/currentCluster.price <= l.clusterTolerance {
			// Add to current cluster
			currentCluster.touches++
			currentCluster.price = (currentCluster.price*float64(currentCluster.touches-1) + sorted[i].price) / float64(currentCluster.touches)
			if sorted[i].index < currentCluster.firstIdx {
				currentCluster.firstIdx = sorted[i].index
			}
			if sorted[i].index > currentCluster.lastIdx {
				currentCluster.lastIdx = sorted[i].index
			}
		} else {
			// Save current cluster and start new one
			clusters = append(clusters, currentCluster)
			currentCluster = clusteredLevel{
				price:    sorted[i].price,
				touches:  1,
				firstIdx: sorted[i].index,
				lastIdx:  sorted[i].index,
			}
		}
	}
	clusters = append(clusters, currentCluster)

	return clusters
}

// findDynamicLevels identifies dynamic support/resistance from moving averages.
func (l *LevelAnalyzer) findDynamicLevels(candles []models.Candle) []PriceLevel {
	var levels []PriceLevel
	n := len(candles)
	currentPrice := candles[n-1].Close

	for _, period := range l.maPeriods {
		if n < period {
			continue
		}

		// Calculate SMA
		var sum float64
		for i := n - period; i < n; i++ {
			sum += candles[i].Close
		}
		ma := sum / float64(period)

		// Determine if MA is support or resistance
		levelType := analysis.LevelSupport
		if ma > currentPrice {
			levelType = analysis.LevelResistance
		}

		// Count touches
		touches := l.countMATouches(candles, ma, period)

		levels = append(levels, PriceLevel{
			Price:      ma,
			Type:       levelType,
			Strength:   touches,
			Source:     "ma",
			FirstTouch: n - period,
			LastTouch:  n - 1,
		})
	}

	return levels
}

// countMATouches counts how many times price has touched the moving average.
func (l *LevelAnalyzer) countMATouches(candles []models.Candle, ma float64, period int) int {
	touches := 0
	n := len(candles)
	tolerance := ma * 0.005 // 0.5% tolerance

	for i := period; i < n; i++ {
		// Recalculate MA at this point
		var sum float64
		for j := i - period; j < i; j++ {
			sum += candles[j].Close
		}
		currentMA := sum / float64(period)

		// Check if price touched MA
		if candles[i].Low <= currentMA+tolerance && candles[i].High >= currentMA-tolerance {
			touches++
		}
	}

	return touches
}


// findTrendlineLevels identifies trendline support and resistance.
func (l *LevelAnalyzer) findTrendlineLevels(candles []models.Candle) []PriceLevel {
	var levels []PriceLevel
	n := len(candles)

	if n < 20 {
		return levels
	}

	// Find swing points for trendline construction
	var swingHighs, swingLows []struct {
		index int
		price float64
	}

	for i := l.pivotStrength; i < n-l.pivotStrength; i++ {
		// Check for swing high
		isSwingHigh := true
		for j := 1; j <= l.pivotStrength; j++ {
			if candles[i].High <= candles[i-j].High || candles[i].High <= candles[i+j].High {
				isSwingHigh = false
				break
			}
		}
		if isSwingHigh {
			swingHighs = append(swingHighs, struct {
				index int
				price float64
			}{i, candles[i].High})
		}

		// Check for swing low
		isSwingLow := true
		for j := 1; j <= l.pivotStrength; j++ {
			if candles[i].Low >= candles[i-j].Low || candles[i].Low >= candles[i+j].Low {
				isSwingLow = false
				break
			}
		}
		if isSwingLow {
			swingLows = append(swingLows, struct {
				index int
				price float64
			}{i, candles[i].Low})
		}
	}

	// Find resistance trendlines (connecting swing highs)
	if len(swingHighs) >= 2 {
		for i := 0; i < len(swingHighs)-1; i++ {
			for j := i + 1; j < len(swingHighs); j++ {
				// Calculate trendline
				slope := (swingHighs[j].price - swingHighs[i].price) / float64(swingHighs[j].index-swingHighs[i].index)
				intercept := swingHighs[i].price - slope*float64(swingHighs[i].index)

				// Project to current bar
				currentLevel := slope*float64(n-1) + intercept

				// Count touches
				touches := l.countTrendlineTouches(candles, slope, intercept, true)

				if touches >= l.minTouches && currentLevel > 0 {
					levels = append(levels, PriceLevel{
						Price:      currentLevel,
						Type:       analysis.LevelResistance,
						Strength:   touches,
						Source:     "trendline",
						FirstTouch: swingHighs[i].index,
						LastTouch:  swingHighs[j].index,
					})
				}
			}
		}
	}

	// Find support trendlines (connecting swing lows)
	if len(swingLows) >= 2 {
		for i := 0; i < len(swingLows)-1; i++ {
			for j := i + 1; j < len(swingLows); j++ {
				// Calculate trendline
				slope := (swingLows[j].price - swingLows[i].price) / float64(swingLows[j].index-swingLows[i].index)
				intercept := swingLows[i].price - slope*float64(swingLows[i].index)

				// Project to current bar
				currentLevel := slope*float64(n-1) + intercept

				// Count touches
				touches := l.countTrendlineTouches(candles, slope, intercept, false)

				if touches >= l.minTouches && currentLevel > 0 {
					levels = append(levels, PriceLevel{
						Price:      currentLevel,
						Type:       analysis.LevelSupport,
						Strength:   touches,
						Source:     "trendline",
						FirstTouch: swingLows[i].index,
						LastTouch:  swingLows[j].index,
					})
				}
			}
		}
	}

	return levels
}

// countTrendlineTouches counts how many times price has touched a trendline.
func (l *LevelAnalyzer) countTrendlineTouches(candles []models.Candle, slope, intercept float64, isResistance bool) int {
	touches := 0
	n := len(candles)

	for i := 0; i < n; i++ {
		trendlinePrice := slope*float64(i) + intercept
		tolerance := trendlinePrice * 0.005 // 0.5% tolerance

		if isResistance {
			// Check if high touched trendline
			if candles[i].High >= trendlinePrice-tolerance && candles[i].High <= trendlinePrice+tolerance {
				touches++
			}
		} else {
			// Check if low touched trendline
			if candles[i].Low >= trendlinePrice-tolerance && candles[i].Low <= trendlinePrice+tolerance {
				touches++
			}
		}
	}

	return touches
}

// findSupplyDemandZones identifies supply and demand zones.
func (l *LevelAnalyzer) findSupplyDemandZones(candles []models.Candle) ([]Zone, []Zone) {
	var supplyZones, demandZones []Zone
	n := len(candles)

	if n < 10 {
		return supplyZones, demandZones
	}

	// Look for strong moves away from consolidation areas
	for i := 5; i < n-5; i++ {
		// Check for demand zone (strong bullish move from a base)
		if l.isStrongBullishMove(candles, i) {
			// Find the base before the move
			baseStart, baseEnd := l.findBaseBeforeMove(candles, i, true)
			if baseStart >= 0 {
				zone := Zone{
					UpperPrice: l.findHighestInRange(candles, baseStart, baseEnd),
					LowerPrice: l.findLowestInRange(candles, baseStart, baseEnd),
					Type:       "demand",
					Strength:   l.calculateZoneStrength(candles, baseStart, baseEnd, i),
					StartIndex: baseStart,
					EndIndex:   baseEnd,
				}
				demandZones = append(demandZones, zone)
			}
		}

		// Check for supply zone (strong bearish move from a base)
		if l.isStrongBearishMove(candles, i) {
			// Find the base before the move
			baseStart, baseEnd := l.findBaseBeforeMove(candles, i, false)
			if baseStart >= 0 {
				zone := Zone{
					UpperPrice: l.findHighestInRange(candles, baseStart, baseEnd),
					LowerPrice: l.findLowestInRange(candles, baseStart, baseEnd),
					Type:       "supply",
					Strength:   l.calculateZoneStrength(candles, baseStart, baseEnd, i),
					StartIndex: baseStart,
					EndIndex:   baseEnd,
				}
				supplyZones = append(supplyZones, zone)
			}
		}
	}

	return supplyZones, demandZones
}

// isStrongBullishMove checks if there's a strong bullish move at the given index.
func (l *LevelAnalyzer) isStrongBullishMove(candles []models.Candle, idx int) bool {
	if idx < 1 || idx >= len(candles) {
		return false
	}

	// Check for a large bullish candle
	c := candles[idx]
	body := c.Close - c.Open
	rng := c.High - c.Low

	if rng == 0 {
		return false
	}

	// Body should be at least 60% of range and bullish
	return body > 0 && body/rng >= 0.6 && body/c.Open >= 0.02 // At least 2% move
}

// isStrongBearishMove checks if there's a strong bearish move at the given index.
func (l *LevelAnalyzer) isStrongBearishMove(candles []models.Candle, idx int) bool {
	if idx < 1 || idx >= len(candles) {
		return false
	}

	// Check for a large bearish candle
	c := candles[idx]
	body := c.Open - c.Close
	rng := c.High - c.Low

	if rng == 0 {
		return false
	}

	// Body should be at least 60% of range and bearish
	return body > 0 && body/rng >= 0.6 && body/c.Open >= 0.02 // At least 2% move
}

// findBaseBeforeMove finds the consolidation base before a strong move.
func (l *LevelAnalyzer) findBaseBeforeMove(candles []models.Candle, moveIdx int, isBullish bool) (int, int) {
	if moveIdx < 3 {
		return -1, -1
	}

	// Look back for a consolidation area (small range candles)
	baseEnd := moveIdx - 1
	baseStart := baseEnd

	avgRange := l.calculateAverageRange(candles, maxInt(0, moveIdx-20), moveIdx)

	for i := baseEnd; i >= maxInt(0, moveIdx-10); i-- {
		candleRange := candles[i].High - candles[i].Low
		// Base candles should have smaller than average range
		if candleRange <= avgRange*0.7 {
			baseStart = i
		} else {
			break
		}
	}

	if baseEnd-baseStart < 2 {
		return -1, -1
	}

	return baseStart, baseEnd
}

// calculateAverageRange calculates the average candle range in a given range.
func (l *LevelAnalyzer) calculateAverageRange(candles []models.Candle, start, end int) float64 {
	if end <= start {
		return 0
	}

	var sum float64
	for i := start; i < end; i++ {
		sum += candles[i].High - candles[i].Low
	}
	return sum / float64(end-start)
}

// findHighestInRange finds the highest price in a range.
func (l *LevelAnalyzer) findHighestInRange(candles []models.Candle, start, end int) float64 {
	highest := candles[start].High
	for i := start + 1; i <= end && i < len(candles); i++ {
		if candles[i].High > highest {
			highest = candles[i].High
		}
	}
	return highest
}

// findLowestInRange finds the lowest price in a range.
func (l *LevelAnalyzer) findLowestInRange(candles []models.Candle, start, end int) float64 {
	lowest := candles[start].Low
	for i := start + 1; i <= end && i < len(candles); i++ {
		if candles[i].Low < lowest {
			lowest = candles[i].Low
		}
	}
	return lowest
}

// calculateZoneStrength calculates the strength of a supply/demand zone.
func (l *LevelAnalyzer) calculateZoneStrength(candles []models.Candle, baseStart, baseEnd, moveIdx int) int {
	strength := 1

	// Strength based on move size
	moveSize := math.Abs(candles[moveIdx].Close - candles[baseEnd].Close)
	if moveSize/candles[baseEnd].Close > 0.05 {
		strength++
	}
	if moveSize/candles[baseEnd].Close > 0.1 {
		strength++
	}

	// Strength based on base duration
	baseDuration := baseEnd - baseStart
	if baseDuration >= 3 {
		strength++
	}
	if baseDuration >= 5 {
		strength++
	}

	return strength
}

// calculatePreviousPeriodLevels calculates high/low from previous day/week/month.
func (l *LevelAnalyzer) calculatePreviousPeriodLevels(candles []models.Candle) PreviousPeriodLevels {
	result := PreviousPeriodLevels{}
	n := len(candles)

	if n < 2 {
		return result
	}

	// Previous day (assuming daily candles, use last candle)
	// For intraday, this would need timestamp analysis
	if n >= 2 {
		result.PrevDayHigh = candles[n-2].High
		result.PrevDayLow = candles[n-2].Low
	}

	// Previous week (last 5 trading days)
	if n >= 6 {
		weekStart := maxInt(0, n-6)
		weekEnd := n - 1
		result.PrevWeekHigh = l.findHighestInRange(candles, weekStart, weekEnd)
		result.PrevWeekLow = l.findLowestInRange(candles, weekStart, weekEnd)
	}

	// Previous month (last 20 trading days)
	if n >= 21 {
		monthStart := maxInt(0, n-21)
		monthEnd := n - 1
		result.PrevMonthHigh = l.findHighestInRange(candles, monthStart, monthEnd)
		result.PrevMonthLow = l.findLowestInRange(candles, monthStart, monthEnd)
	}

	return result
}

// maxInt returns the maximum of two integers.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// GetNearestLevels returns the nearest support and resistance levels to the current price.
func (l *LevelAnalyzer) GetNearestLevels(result *LevelAnalysisResult, currentPrice float64) (support, resistance *PriceLevel) {
	if result == nil {
		return nil, nil
	}

	// Combine all levels
	var allLevels []PriceLevel
	allLevels = append(allLevels, result.HorizontalLevels...)
	allLevels = append(allLevels, result.DynamicLevels...)
	allLevels = append(allLevels, result.TrendlineLevels...)

	var nearestSupport, nearestResistance *PriceLevel
	minSupportDist := math.MaxFloat64
	minResistanceDist := math.MaxFloat64

	for i := range allLevels {
		level := &allLevels[i]
		dist := math.Abs(level.Price - currentPrice)

		if level.Price < currentPrice && dist < minSupportDist {
			minSupportDist = dist
			nearestSupport = level
		}
		if level.Price > currentPrice && dist < minResistanceDist {
			minResistanceDist = dist
			nearestResistance = level
		}
	}

	return nearestSupport, nearestResistance
}
