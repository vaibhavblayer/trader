// Package patterns provides chart and candlestick pattern detection.
package patterns

import (
	"math"
	"sort"

	"zerodha-trader/internal/models"
)

// VolumeAnalyzer performs comprehensive volume analysis.
type VolumeAnalyzer struct {
	profileBins      int     // Number of price bins for volume profile
	valueAreaPercent float64 // Percentage of volume for value area (typically 70%)
	climaxMultiplier float64 // Volume multiplier for climax detection
	dryUpThreshold   float64 // Volume threshold for dry-up detection
}

// NewVolumeAnalyzer creates a new volume analyzer.
func NewVolumeAnalyzer() *VolumeAnalyzer {
	return &VolumeAnalyzer{
		profileBins:      50,
		valueAreaPercent: 0.70,
		climaxMultiplier: 2.0,
		dryUpThreshold:   0.5,
	}
}

func (v *VolumeAnalyzer) Name() string {
	return "VolumeAnalyzer"
}

// VolumeAnalysisResult contains comprehensive volume analysis.
type VolumeAnalysisResult struct {
	Profile        *VolumeProfile
	POC            float64 // Point of Control
	VAH            float64 // Value Area High
	VAL            float64 // Value Area Low
	HVNs           []float64 // High Volume Nodes
	LVNs           []float64 // Low Volume Nodes
	VolumeClimaxes []VolumeClimax
	VolumeDryUps   []VolumeDryUp
	AverageVolume  float64
	CurrentVolume  int64
	VolumeRatio    float64 // Current volume / Average volume
}

// VolumeProfile represents volume distribution at price levels.
type VolumeProfile struct {
	PriceLevels []float64
	Volumes     []int64
	TotalVolume int64
	HighPrice   float64
	LowPrice    float64
	BinSize     float64
}

// VolumeClimax represents a volume climax event.
type VolumeClimax struct {
	Index       int
	Volume      int64
	Price       float64
	IsBullish   bool
	Multiplier  float64 // How many times average volume
}

// VolumeDryUp represents a volume dry-up event.
type VolumeDryUp struct {
	StartIndex int
	EndIndex   int
	AvgVolume  float64
	Ratio      float64 // Volume / Average volume
}

// Analyze performs comprehensive volume analysis.
func (v *VolumeAnalyzer) Analyze(candles []models.Candle) (*VolumeAnalysisResult, error) {
	if len(candles) < 20 {
		return nil, nil
	}

	result := &VolumeAnalysisResult{}

	// Calculate volume profile
	result.Profile = v.calculateVolumeProfile(candles)

	// Find POC, VAH, VAL
	result.POC, result.VAH, result.VAL = v.findValueArea(result.Profile)

	// Find high and low volume nodes
	result.HVNs, result.LVNs = v.findVolumeNodes(result.Profile)

	// Detect volume climaxes
	result.VolumeClimaxes = v.detectVolumeClimaxes(candles)

	// Detect volume dry-ups
	result.VolumeDryUps = v.detectVolumeDryUps(candles)

	// Calculate average and current volume
	result.AverageVolume = v.calculateAverageVolume(candles)
	result.CurrentVolume = candles[len(candles)-1].Volume
	if result.AverageVolume > 0 {
		result.VolumeRatio = float64(result.CurrentVolume) / result.AverageVolume
	}

	return result, nil
}

// calculateVolumeProfile creates a volume profile from candle data.
func (v *VolumeAnalyzer) calculateVolumeProfile(candles []models.Candle) *VolumeProfile {
	if len(candles) == 0 {
		return nil
	}

	// Find price range
	highPrice := candles[0].High
	lowPrice := candles[0].Low
	var totalVolume int64

	for _, c := range candles {
		if c.High > highPrice {
			highPrice = c.High
		}
		if c.Low < lowPrice {
			lowPrice = c.Low
		}
		totalVolume += c.Volume
	}

	if highPrice == lowPrice {
		return &VolumeProfile{
			PriceLevels: []float64{highPrice},
			Volumes:     []int64{totalVolume},
			TotalVolume: totalVolume,
			HighPrice:   highPrice,
			LowPrice:    lowPrice,
			BinSize:     0,
		}
	}

	binSize := (highPrice - lowPrice) / float64(v.profileBins)
	priceLevels := make([]float64, v.profileBins)
	volumes := make([]int64, v.profileBins)

	// Initialize price levels (center of each bin)
	for i := 0; i < v.profileBins; i++ {
		priceLevels[i] = lowPrice + float64(i)*binSize + binSize/2
	}

	// Distribute volume across bins
	for _, c := range candles {
		// Use typical price for volume distribution
		tp := (c.High + c.Low + c.Close) / 3
		binIdx := int((tp - lowPrice) / binSize)
		if binIdx >= v.profileBins {
			binIdx = v.profileBins - 1
		}
		if binIdx < 0 {
			binIdx = 0
		}
		volumes[binIdx] += c.Volume
	}

	return &VolumeProfile{
		PriceLevels: priceLevels,
		Volumes:     volumes,
		TotalVolume: totalVolume,
		HighPrice:   highPrice,
		LowPrice:    lowPrice,
		BinSize:     binSize,
	}
}

// findValueArea finds POC, VAH, and VAL from volume profile.
func (v *VolumeAnalyzer) findValueArea(profile *VolumeProfile) (poc, vah, val float64) {
	if profile == nil || len(profile.Volumes) == 0 {
		return 0, 0, 0
	}

	// Find POC (Point of Control) - price level with highest volume
	maxVolIdx := 0
	var maxVol int64
	for i, vol := range profile.Volumes {
		if vol > maxVol {
			maxVol = vol
			maxVolIdx = i
		}
	}
	poc = profile.PriceLevels[maxVolIdx]

	// Calculate Value Area (70% of total volume)
	targetVolume := int64(float64(profile.TotalVolume) * v.valueAreaPercent)

	// Expand from POC until we capture target volume
	vahIdx := maxVolIdx
	valIdx := maxVolIdx
	var vaVolume int64 = profile.Volumes[maxVolIdx]

	for vaVolume < targetVolume && (vahIdx < len(profile.Volumes)-1 || valIdx > 0) {
		var upperVol, lowerVol int64
		if vahIdx < len(profile.Volumes)-1 {
			upperVol = profile.Volumes[vahIdx+1]
		}
		if valIdx > 0 {
			lowerVol = profile.Volumes[valIdx-1]
		}

		if upperVol >= lowerVol && vahIdx < len(profile.Volumes)-1 {
			vahIdx++
			vaVolume += profile.Volumes[vahIdx]
		} else if valIdx > 0 {
			valIdx--
			vaVolume += profile.Volumes[valIdx]
		} else {
			break
		}
	}

	vah = profile.PriceLevels[vahIdx]
	val = profile.PriceLevels[valIdx]

	return poc, vah, val
}

// findVolumeNodes identifies high and low volume nodes.
func (v *VolumeAnalyzer) findVolumeNodes(profile *VolumeProfile) (hvns, lvns []float64) {
	if profile == nil || len(profile.Volumes) < 3 {
		return nil, nil
	}

	// Calculate average volume per bin
	avgVolume := float64(profile.TotalVolume) / float64(len(profile.Volumes))

	// Find HVNs (volume > 1.5x average) and LVNs (volume < 0.5x average)
	for i, vol := range profile.Volumes {
		volFloat := float64(vol)
		if volFloat > avgVolume*1.5 {
			hvns = append(hvns, profile.PriceLevels[i])
		} else if volFloat < avgVolume*0.5 && volFloat > 0 {
			lvns = append(lvns, profile.PriceLevels[i])
		}
	}

	return hvns, lvns
}


// detectVolumeClimaxes identifies volume climax events.
func (v *VolumeAnalyzer) detectVolumeClimaxes(candles []models.Candle) []VolumeClimax {
	var climaxes []VolumeClimax
	n := len(candles)

	if n < 20 {
		return climaxes
	}

	// Calculate rolling average volume
	for i := 20; i < n; i++ {
		// Calculate average volume for previous 20 bars
		var avgVol float64
		for j := i - 20; j < i; j++ {
			avgVol += float64(candles[j].Volume)
		}
		avgVol /= 20

		// Check if current volume is a climax
		currentVol := float64(candles[i].Volume)
		multiplier := currentVol / avgVol

		if multiplier >= v.climaxMultiplier {
			isBullish := candles[i].Close > candles[i].Open

			climaxes = append(climaxes, VolumeClimax{
				Index:      i,
				Volume:     candles[i].Volume,
				Price:      candles[i].Close,
				IsBullish:  isBullish,
				Multiplier: multiplier,
			})
		}
	}

	return climaxes
}

// detectVolumeDryUps identifies periods of unusually low volume.
func (v *VolumeAnalyzer) detectVolumeDryUps(candles []models.Candle) []VolumeDryUp {
	var dryUps []VolumeDryUp
	n := len(candles)

	if n < 20 {
		return dryUps
	}

	// Calculate overall average volume
	avgVolume := v.calculateAverageVolume(candles)

	// Find consecutive periods of low volume
	var currentDryUp *VolumeDryUp

	for i := 0; i < n; i++ {
		ratio := float64(candles[i].Volume) / avgVolume

		if ratio <= v.dryUpThreshold {
			if currentDryUp == nil {
				currentDryUp = &VolumeDryUp{
					StartIndex: i,
					EndIndex:   i,
					AvgVolume:  float64(candles[i].Volume),
					Ratio:      ratio,
				}
			} else {
				currentDryUp.EndIndex = i
				// Update average volume in dry-up period
				count := currentDryUp.EndIndex - currentDryUp.StartIndex + 1
				currentDryUp.AvgVolume = (currentDryUp.AvgVolume*float64(count-1) + float64(candles[i].Volume)) / float64(count)
				currentDryUp.Ratio = currentDryUp.AvgVolume / avgVolume
			}
		} else {
			if currentDryUp != nil {
				// Only record if dry-up lasted at least 3 bars
				if currentDryUp.EndIndex-currentDryUp.StartIndex >= 2 {
					dryUps = append(dryUps, *currentDryUp)
				}
				currentDryUp = nil
			}
		}
	}

	// Don't forget the last dry-up if it extends to the end
	if currentDryUp != nil && currentDryUp.EndIndex-currentDryUp.StartIndex >= 2 {
		dryUps = append(dryUps, *currentDryUp)
	}

	return dryUps
}

// calculateAverageVolume calculates the average volume over all candles.
func (v *VolumeAnalyzer) calculateAverageVolume(candles []models.Candle) float64 {
	if len(candles) == 0 {
		return 0
	}

	var total int64
	for _, c := range candles {
		total += c.Volume
	}
	return float64(total) / float64(len(candles))
}

// GetVolumeAtPrice returns the volume at a specific price level.
func (v *VolumeAnalyzer) GetVolumeAtPrice(profile *VolumeProfile, price float64) int64 {
	if profile == nil || len(profile.Volumes) == 0 {
		return 0
	}

	binIdx := int((price - profile.LowPrice) / profile.BinSize)
	if binIdx < 0 {
		binIdx = 0
	}
	if binIdx >= len(profile.Volumes) {
		binIdx = len(profile.Volumes) - 1
	}

	return profile.Volumes[binIdx]
}

// IsHighVolumeNode checks if a price is at a high volume node.
func (v *VolumeAnalyzer) IsHighVolumeNode(result *VolumeAnalysisResult, price float64) bool {
	if result == nil {
		return false
	}

	tolerance := (result.Profile.HighPrice - result.Profile.LowPrice) * 0.02 // 2% tolerance

	for _, hvn := range result.HVNs {
		if math.Abs(price-hvn) <= tolerance {
			return true
		}
	}
	return false
}

// IsLowVolumeNode checks if a price is at a low volume node.
func (v *VolumeAnalyzer) IsLowVolumeNode(result *VolumeAnalysisResult, price float64) bool {
	if result == nil {
		return false
	}

	tolerance := (result.Profile.HighPrice - result.Profile.LowPrice) * 0.02 // 2% tolerance

	for _, lvn := range result.LVNs {
		if math.Abs(price-lvn) <= tolerance {
			return true
		}
	}
	return false
}

// IsInValueArea checks if a price is within the value area.
func (v *VolumeAnalyzer) IsInValueArea(result *VolumeAnalysisResult, price float64) bool {
	if result == nil {
		return false
	}
	return price >= result.VAL && price <= result.VAH
}

// GetVolumeScore returns a score from 0 to 100 based on current volume relative to average.
func (v *VolumeAnalyzer) GetVolumeScore(result *VolumeAnalysisResult) float64 {
	if result == nil || result.AverageVolume == 0 {
		return 50
	}

	// Score based on volume ratio
	// Ratio of 1.0 = 50, Ratio of 2.0 = 75, Ratio of 0.5 = 25
	score := 50 + (result.VolumeRatio-1)*25

	// Clamp to [0, 100]
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}

// GetTopVolumeNodes returns the top N high volume nodes sorted by volume.
func (v *VolumeAnalyzer) GetTopVolumeNodes(profile *VolumeProfile, n int) []struct {
	Price  float64
	Volume int64
} {
	if profile == nil || len(profile.Volumes) == 0 {
		return nil
	}

	// Create slice of price-volume pairs
	type priceVolume struct {
		Price  float64
		Volume int64
	}

	pairs := make([]priceVolume, len(profile.Volumes))
	for i := range profile.Volumes {
		pairs[i] = priceVolume{
			Price:  profile.PriceLevels[i],
			Volume: profile.Volumes[i],
		}
	}

	// Sort by volume descending
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].Volume > pairs[j].Volume
	})

	// Return top N
	if n > len(pairs) {
		n = len(pairs)
	}

	result := make([]struct {
		Price  float64
		Volume int64
	}, n)

	for i := 0; i < n; i++ {
		result[i].Price = pairs[i].Price
		result[i].Volume = pairs[i].Volume
	}

	return result
}
