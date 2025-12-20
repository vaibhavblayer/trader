// Package patterns provides chart and candlestick pattern detection.
package patterns

import (
	"zerodha-trader/internal/models"
)

// MarketStructureType represents the type of market structure event.
type MarketStructureType string

const (
	StructureBOS   MarketStructureType = "BOS"   // Break of Structure
	StructureChoCH MarketStructureType = "CHOCH" // Change of Character
)

// PriceActionAnalyzer performs price action analysis.
type PriceActionAnalyzer struct {
	swingStrength int     // Number of bars for swing point confirmation
	fvgMinSize    float64 // Minimum size for Fair Value Gap as % of price
}

// NewPriceActionAnalyzer creates a new price action analyzer.
func NewPriceActionAnalyzer() *PriceActionAnalyzer {
	return &PriceActionAnalyzer{
		swingStrength: 3,
		fvgMinSize:    0.002, // 0.2% minimum gap
	}
}

func (p *PriceActionAnalyzer) Name() string {
	return "PriceActionAnalyzer"
}

// PriceActionResult contains comprehensive price action analysis.
type PriceActionResult struct {
	SwingHighs       []SwingPointPA
	SwingLows        []SwingPointPA
	StructureBreaks  []StructureBreak
	OrderBlocks      []OrderBlock
	FairValueGaps    []FairValueGap
	InsideBars       []BarPattern
	OutsideBars      []BarPattern
	PinBars          []BarPattern
	CurrentStructure MarketStructure
}

// SwingPointPA represents a swing point in price action analysis.
type SwingPointPA struct {
	Index    int
	Price    float64
	IsHigh   bool
	Broken   bool // Whether this swing has been broken
	BrokenAt int  // Index where it was broken
}

// StructureBreak represents a break of structure or change of character.
type StructureBreak struct {
	Type       MarketStructureType
	Index      int
	Price      float64
	Direction  string // "bullish" or "bearish"
	SwingIndex int    // Index of the swing that was broken
	SwingPrice float64
}

// OrderBlock represents an order block zone.
type OrderBlock struct {
	Type       string  // "bullish" or "bearish"
	UpperPrice float64
	LowerPrice float64
	StartIndex int
	EndIndex   int
	Mitigated  bool // Whether price has returned to this zone
	Strength   int  // Based on the move away from the block
}

// FairValueGap represents a fair value gap (imbalance).
type FairValueGap struct {
	Type       string  // "bullish" or "bearish"
	UpperPrice float64
	LowerPrice float64
	Index      int
	Filled     bool    // Whether the gap has been filled
	FilledAt   int     // Index where it was filled
	Size       float64 // Size of the gap as percentage
}

// BarPattern represents a bar pattern (inside bar, outside bar, pin bar).
type BarPattern struct {
	Type      string // "inside", "outside", "pin_bullish", "pin_bearish"
	Index     int
	High      float64
	Low       float64
	Direction string // Expected direction
}

// MarketStructure represents the current market structure.
type MarketStructure struct {
	Trend           string // "bullish", "bearish", "ranging"
	LastSwingHigh   *SwingPointPA
	LastSwingLow    *SwingPointPA
	HigherHighs     int
	HigherLows      int
	LowerHighs      int
	LowerLows       int
}

// Analyze performs comprehensive price action analysis.
func (p *PriceActionAnalyzer) Analyze(candles []models.Candle) (*PriceActionResult, error) {
	if len(candles) < p.swingStrength*2+1 {
		return nil, nil
	}

	result := &PriceActionResult{}

	// Find swing highs and lows
	result.SwingHighs, result.SwingLows = p.findSwingPoints(candles)

	// Analyze market structure
	result.CurrentStructure = p.analyzeMarketStructure(result.SwingHighs, result.SwingLows)

	// Detect structure breaks (BOS and ChoCH)
	result.StructureBreaks = p.detectStructureBreaks(candles, result.SwingHighs, result.SwingLows)

	// Find order blocks
	result.OrderBlocks = p.findOrderBlocks(candles)

	// Find fair value gaps
	result.FairValueGaps = p.findFairValueGaps(candles)

	// Find bar patterns
	result.InsideBars = p.findInsideBars(candles)
	result.OutsideBars = p.findOutsideBars(candles)
	result.PinBars = p.findPinBars(candles)

	// Mark broken swings
	p.markBrokenSwings(candles, result.SwingHighs, result.SwingLows)

	return result, nil
}

// findSwingPoints identifies swing highs and lows.
func (p *PriceActionAnalyzer) findSwingPoints(candles []models.Candle) ([]SwingPointPA, []SwingPointPA) {
	var highs, lows []SwingPointPA
	n := len(candles)

	for i := p.swingStrength; i < n-p.swingStrength; i++ {
		// Check for swing high
		isSwingHigh := true
		for j := 1; j <= p.swingStrength; j++ {
			if candles[i].High <= candles[i-j].High || candles[i].High <= candles[i+j].High {
				isSwingHigh = false
				break
			}
		}
		if isSwingHigh {
			highs = append(highs, SwingPointPA{
				Index:  i,
				Price:  candles[i].High,
				IsHigh: true,
			})
		}

		// Check for swing low
		isSwingLow := true
		for j := 1; j <= p.swingStrength; j++ {
			if candles[i].Low >= candles[i-j].Low || candles[i].Low >= candles[i+j].Low {
				isSwingLow = false
				break
			}
		}
		if isSwingLow {
			lows = append(lows, SwingPointPA{
				Index:  i,
				Price:  candles[i].Low,
				IsHigh: false,
			})
		}
	}

	return highs, lows
}

// analyzeMarketStructure determines the current market structure.
func (p *PriceActionAnalyzer) analyzeMarketStructure(highs, lows []SwingPointPA) MarketStructure {
	structure := MarketStructure{
		Trend: "ranging",
	}

	if len(highs) > 0 {
		structure.LastSwingHigh = &highs[len(highs)-1]
	}
	if len(lows) > 0 {
		structure.LastSwingLow = &lows[len(lows)-1]
	}

	// Count higher highs/lows and lower highs/lows
	for i := 1; i < len(highs); i++ {
		if highs[i].Price > highs[i-1].Price {
			structure.HigherHighs++
		} else {
			structure.LowerHighs++
		}
	}

	for i := 1; i < len(lows); i++ {
		if lows[i].Price > lows[i-1].Price {
			structure.HigherLows++
		} else {
			structure.LowerLows++
		}
	}

	// Determine trend
	if structure.HigherHighs > structure.LowerHighs && structure.HigherLows > structure.LowerLows {
		structure.Trend = "bullish"
	} else if structure.LowerHighs > structure.HigherHighs && structure.LowerLows > structure.HigherLows {
		structure.Trend = "bearish"
	}

	return structure
}

// detectStructureBreaks detects BOS and ChoCH events.
func (p *PriceActionAnalyzer) detectStructureBreaks(candles []models.Candle, highs, lows []SwingPointPA) []StructureBreak {
	var breaks []StructureBreak
	n := len(candles)

	// Track the current trend for ChoCH detection
	currentTrend := "unknown"
	if len(highs) >= 2 && len(lows) >= 2 {
		if highs[len(highs)-1].Price > highs[len(highs)-2].Price &&
			lows[len(lows)-1].Price > lows[len(lows)-2].Price {
			currentTrend = "bullish"
		} else if highs[len(highs)-1].Price < highs[len(highs)-2].Price &&
			lows[len(lows)-1].Price < lows[len(lows)-2].Price {
			currentTrend = "bearish"
		}
	}

	// Check for breaks of swing highs (bullish BOS/ChoCH)
	for i := range highs {
		swing := &highs[i]
		for j := swing.Index + 1; j < n; j++ {
			if candles[j].Close > swing.Price {
				breakType := StructureBOS
				if currentTrend == "bearish" {
					breakType = StructureChoCH
				}

				breaks = append(breaks, StructureBreak{
					Type:       breakType,
					Index:      j,
					Price:      candles[j].Close,
					Direction:  "bullish",
					SwingIndex: swing.Index,
					SwingPrice: swing.Price,
				})
				break
			}
		}
	}

	// Check for breaks of swing lows (bearish BOS/ChoCH)
	for i := range lows {
		swing := &lows[i]
		for j := swing.Index + 1; j < n; j++ {
			if candles[j].Close < swing.Price {
				breakType := StructureBOS
				if currentTrend == "bullish" {
					breakType = StructureChoCH
				}

				breaks = append(breaks, StructureBreak{
					Type:       breakType,
					Index:      j,
					Price:      candles[j].Close,
					Direction:  "bearish",
					SwingIndex: swing.Index,
					SwingPrice: swing.Price,
				})
				break
			}
		}
	}

	return breaks
}


// findOrderBlocks identifies order block zones.
func (p *PriceActionAnalyzer) findOrderBlocks(candles []models.Candle) []OrderBlock {
	var blocks []OrderBlock
	n := len(candles)

	for i := 1; i < n-1; i++ {
		// Bullish Order Block: Last bearish candle before a strong bullish move
		if p.isBearish(candles[i]) && p.isStrongBullishMove(candles, i+1) {
			block := OrderBlock{
				Type:       "bullish",
				UpperPrice: candles[i].Open,
				LowerPrice: candles[i].Close,
				StartIndex: i,
				EndIndex:   i,
				Strength:   p.calculateMoveStrength(candles, i+1, true),
			}

			// Check if mitigated
			for j := i + 2; j < n; j++ {
				if candles[j].Low <= block.UpperPrice {
					block.Mitigated = true
					break
				}
			}

			blocks = append(blocks, block)
		}

		// Bearish Order Block: Last bullish candle before a strong bearish move
		if p.isBullish(candles[i]) && p.isStrongBearishMove(candles, i+1) {
			block := OrderBlock{
				Type:       "bearish",
				UpperPrice: candles[i].Close,
				LowerPrice: candles[i].Open,
				StartIndex: i,
				EndIndex:   i,
				Strength:   p.calculateMoveStrength(candles, i+1, false),
			}

			// Check if mitigated
			for j := i + 2; j < n; j++ {
				if candles[j].High >= block.LowerPrice {
					block.Mitigated = true
					break
				}
			}

			blocks = append(blocks, block)
		}
	}

	return blocks
}

// findFairValueGaps identifies fair value gaps (imbalances).
func (p *PriceActionAnalyzer) findFairValueGaps(candles []models.Candle) []FairValueGap {
	var gaps []FairValueGap
	n := len(candles)

	for i := 1; i < n-1; i++ {
		// Bullish FVG: Gap between candle[i-1].high and candle[i+1].low
		if candles[i+1].Low > candles[i-1].High {
			gapSize := (candles[i+1].Low - candles[i-1].High) / candles[i].Close
			if gapSize >= p.fvgMinSize {
				gap := FairValueGap{
					Type:       "bullish",
					UpperPrice: candles[i+1].Low,
					LowerPrice: candles[i-1].High,
					Index:      i,
					Size:       gapSize * 100, // As percentage
				}

				// Check if filled
				for j := i + 2; j < n; j++ {
					if candles[j].Low <= gap.LowerPrice {
						gap.Filled = true
						gap.FilledAt = j
						break
					}
				}

				gaps = append(gaps, gap)
			}
		}

		// Bearish FVG: Gap between candle[i-1].low and candle[i+1].high
		if candles[i+1].High < candles[i-1].Low {
			gapSize := (candles[i-1].Low - candles[i+1].High) / candles[i].Close
			if gapSize >= p.fvgMinSize {
				gap := FairValueGap{
					Type:       "bearish",
					UpperPrice: candles[i-1].Low,
					LowerPrice: candles[i+1].High,
					Index:      i,
					Size:       gapSize * 100, // As percentage
				}

				// Check if filled
				for j := i + 2; j < n; j++ {
					if candles[j].High >= gap.UpperPrice {
						gap.Filled = true
						gap.FilledAt = j
						break
					}
				}

				gaps = append(gaps, gap)
			}
		}
	}

	return gaps
}

// findInsideBars identifies inside bar patterns.
func (p *PriceActionAnalyzer) findInsideBars(candles []models.Candle) []BarPattern {
	var patterns []BarPattern
	n := len(candles)

	for i := 1; i < n; i++ {
		// Inside bar: current bar's range is within previous bar's range
		if candles[i].High < candles[i-1].High && candles[i].Low > candles[i-1].Low {
			direction := "neutral"
			if p.isBullish(candles[i-1]) {
				direction = "bullish"
			} else if p.isBearish(candles[i-1]) {
				direction = "bearish"
			}

			patterns = append(patterns, BarPattern{
				Type:      "inside",
				Index:     i,
				High:      candles[i].High,
				Low:       candles[i].Low,
				Direction: direction,
			})
		}
	}

	return patterns
}

// findOutsideBars identifies outside bar patterns.
func (p *PriceActionAnalyzer) findOutsideBars(candles []models.Candle) []BarPattern {
	var patterns []BarPattern
	n := len(candles)

	for i := 1; i < n; i++ {
		// Outside bar: current bar's range engulfs previous bar's range
		if candles[i].High > candles[i-1].High && candles[i].Low < candles[i-1].Low {
			direction := "neutral"
			if p.isBullish(candles[i]) {
				direction = "bullish"
			} else if p.isBearish(candles[i]) {
				direction = "bearish"
			}

			patterns = append(patterns, BarPattern{
				Type:      "outside",
				Index:     i,
				High:      candles[i].High,
				Low:       candles[i].Low,
				Direction: direction,
			})
		}
	}

	return patterns
}

// findPinBars identifies pin bar patterns.
func (p *PriceActionAnalyzer) findPinBars(candles []models.Candle) []BarPattern {
	var patterns []BarPattern
	n := len(candles)

	for i := 0; i < n; i++ {
		c := candles[i]
		body := p.bodySize(c)
		rng := c.High - c.Low

		if rng == 0 {
			continue
		}

		upperShadow := c.High - maxFloat(c.Open, c.Close)
		lowerShadow := minFloat(c.Open, c.Close) - c.Low

		// Pin bar criteria: one shadow at least 2x the body, other shadow small
		bodyRatio := body / rng

		// Bullish pin bar: long lower shadow
		if lowerShadow >= body*2 && upperShadow <= body*0.5 && bodyRatio < 0.4 {
			patterns = append(patterns, BarPattern{
				Type:      "pin_bullish",
				Index:     i,
				High:      c.High,
				Low:       c.Low,
				Direction: "bullish",
			})
		}

		// Bearish pin bar: long upper shadow
		if upperShadow >= body*2 && lowerShadow <= body*0.5 && bodyRatio < 0.4 {
			patterns = append(patterns, BarPattern{
				Type:      "pin_bearish",
				Index:     i,
				High:      c.High,
				Low:       c.Low,
				Direction: "bearish",
			})
		}
	}

	return patterns
}

// markBrokenSwings marks swing points that have been broken.
func (p *PriceActionAnalyzer) markBrokenSwings(candles []models.Candle, highs, lows []SwingPointPA) {
	n := len(candles)

	for i := range highs {
		for j := highs[i].Index + 1; j < n; j++ {
			if candles[j].Close > highs[i].Price {
				highs[i].Broken = true
				highs[i].BrokenAt = j
				break
			}
		}
	}

	for i := range lows {
		for j := lows[i].Index + 1; j < n; j++ {
			if candles[j].Close < lows[i].Price {
				lows[i].Broken = true
				lows[i].BrokenAt = j
				break
			}
		}
	}
}

// Helper functions

func (p *PriceActionAnalyzer) isBullish(c models.Candle) bool {
	return c.Close > c.Open
}

func (p *PriceActionAnalyzer) isBearish(c models.Candle) bool {
	return c.Close < c.Open
}

func (p *PriceActionAnalyzer) bodySize(c models.Candle) float64 {
	if c.Close > c.Open {
		return c.Close - c.Open
	}
	return c.Open - c.Close
}

func (p *PriceActionAnalyzer) isStrongBullishMove(candles []models.Candle, idx int) bool {
	if idx >= len(candles) {
		return false
	}

	c := candles[idx]
	body := c.Close - c.Open
	rng := c.High - c.Low

	if rng == 0 || body <= 0 {
		return false
	}

	// Strong move: body > 60% of range and at least 1% price move
	return body/rng >= 0.6 && body/c.Open >= 0.01
}

func (p *PriceActionAnalyzer) isStrongBearishMove(candles []models.Candle, idx int) bool {
	if idx >= len(candles) {
		return false
	}

	c := candles[idx]
	body := c.Open - c.Close
	rng := c.High - c.Low

	if rng == 0 || body <= 0 {
		return false
	}

	// Strong move: body > 60% of range and at least 1% price move
	return body/rng >= 0.6 && body/c.Open >= 0.01
}

func (p *PriceActionAnalyzer) calculateMoveStrength(candles []models.Candle, idx int, isBullish bool) int {
	if idx >= len(candles) {
		return 1
	}

	c := candles[idx]
	var body float64
	if isBullish {
		body = c.Close - c.Open
	} else {
		body = c.Open - c.Close
	}

	movePercent := body / c.Open * 100

	if movePercent >= 3 {
		return 5
	}
	if movePercent >= 2 {
		return 4
	}
	if movePercent >= 1.5 {
		return 3
	}
	if movePercent >= 1 {
		return 2
	}
	return 1
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// GetUnfilledFVGs returns fair value gaps that haven't been filled yet.
func (p *PriceActionAnalyzer) GetUnfilledFVGs(result *PriceActionResult) []FairValueGap {
	var unfilled []FairValueGap
	for _, gap := range result.FairValueGaps {
		if !gap.Filled {
			unfilled = append(unfilled, gap)
		}
	}
	return unfilled
}

// GetUnmitigatedOrderBlocks returns order blocks that haven't been mitigated.
func (p *PriceActionAnalyzer) GetUnmitigatedOrderBlocks(result *PriceActionResult) []OrderBlock {
	var unmitigated []OrderBlock
	for _, block := range result.OrderBlocks {
		if !block.Mitigated {
			unmitigated = append(unmitigated, block)
		}
	}
	return unmitigated
}

// GetRecentStructureBreaks returns structure breaks within the last N bars.
func (p *PriceActionAnalyzer) GetRecentStructureBreaks(result *PriceActionResult, totalBars, lookback int) []StructureBreak {
	var recent []StructureBreak
	threshold := totalBars - lookback

	for _, sb := range result.StructureBreaks {
		if sb.Index >= threshold {
			recent = append(recent, sb)
		}
	}
	return recent
}
