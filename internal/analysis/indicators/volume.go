package indicators

import (
	"fmt"

	"zerodha-trader/internal/models"
)

// VWAP calculates Volume Weighted Average Price.
type VWAP struct{}

// NewVWAP creates a new VWAP indicator.
func NewVWAP() *VWAP {
	return &VWAP{}
}

func (v *VWAP) Name() string {
	return "VWAP"
}

func (v *VWAP) Period() int {
	return 1
}

func (v *VWAP) Calculate(candles []models.Candle) ([]float64, error) {
	if len(candles) == 0 {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	result := make([]float64, n)

	var cumulativeTPV float64 // Cumulative Typical Price * Volume
	var cumulativeVol float64 // Cumulative Volume

	for i := 0; i < n; i++ {
		tp := typicalPrice(candles[i])
		cumulativeTPV += tp * float64(candles[i].Volume)
		cumulativeVol += float64(candles[i].Volume)

		if cumulativeVol != 0 {
			result[i] = cumulativeTPV / cumulativeVol
		}
	}

	return result, nil
}

// OBV calculates On-Balance Volume.
type OBV struct{}

// NewOBV creates a new OBV indicator.
func NewOBV() *OBV {
	return &OBV{}
}

func (o *OBV) Name() string {
	return "OBV"
}

func (o *OBV) Period() int {
	return 1
}

func (o *OBV) Calculate(candles []models.Candle) ([]float64, error) {
	if len(candles) == 0 {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	result := make([]float64, n)
	result[0] = float64(candles[0].Volume)

	for i := 1; i < n; i++ {
		if candles[i].Close > candles[i-1].Close {
			result[i] = result[i-1] + float64(candles[i].Volume)
		} else if candles[i].Close < candles[i-1].Close {
			result[i] = result[i-1] - float64(candles[i].Volume)
		} else {
			result[i] = result[i-1]
		}
	}

	return result, nil
}

// MFI calculates Money Flow Index.
type MFI struct {
	period int
}

// NewMFI creates a new MFI indicator.
func NewMFI(period int) *MFI {
	return &MFI{period: period}
}

func (m *MFI) Name() string {
	return fmt.Sprintf("MFI_%d", m.period)
}

func (m *MFI) Period() int {
	return m.period
}

func (m *MFI) Calculate(candles []models.Candle) ([]float64, error) {
	if m.period <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < m.period+1 {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	result := make([]float64, n)

	// Calculate raw money flow
	rawMF := make([]float64, n)
	for i := 0; i < n; i++ {
		rawMF[i] = typicalPrice(candles[i]) * float64(candles[i].Volume)
	}

	for i := m.period; i < n; i++ {
		var positiveMF, negativeMF float64

		for j := i - m.period + 1; j <= i; j++ {
			if j > 0 {
				currentTP := typicalPrice(candles[j])
				prevTP := typicalPrice(candles[j-1])

				if currentTP > prevTP {
					positiveMF += rawMF[j]
				} else if currentTP < prevTP {
					negativeMF += rawMF[j]
				}
			}
		}

		if negativeMF == 0 {
			result[i] = 100
		} else {
			mfRatio := positiveMF / negativeMF
			result[i] = 100 - (100 / (1 + mfRatio))
		}
	}

	return result, nil
}


// CMF calculates Chaikin Money Flow.
type CMF struct {
	period int
}

// NewCMF creates a new CMF indicator.
func NewCMF(period int) *CMF {
	return &CMF{period: period}
}

func (c *CMF) Name() string {
	return fmt.Sprintf("CMF_%d", c.period)
}

func (c *CMF) Period() int {
	return c.period
}

func (c *CMF) Calculate(candles []models.Candle) ([]float64, error) {
	if c.period <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < c.period {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	result := make([]float64, n)

	// Calculate Money Flow Multiplier and Money Flow Volume
	mfv := make([]float64, n)
	for i := 0; i < n; i++ {
		hl := candles[i].High - candles[i].Low
		if hl != 0 {
			mfm := ((candles[i].Close - candles[i].Low) - (candles[i].High - candles[i].Close)) / hl
			mfv[i] = mfm * float64(candles[i].Volume)
		}
	}

	for i := c.period - 1; i < n; i++ {
		var sumMFV, sumVol float64
		for j := i - c.period + 1; j <= i; j++ {
			sumMFV += mfv[j]
			sumVol += float64(candles[j].Volume)
		}

		if sumVol != 0 {
			result[i] = sumMFV / sumVol
		}
	}

	return result, nil
}

// ADLine calculates Accumulation/Distribution Line.
type ADLine struct{}

// NewADLine creates a new A/D Line indicator.
func NewADLine() *ADLine {
	return &ADLine{}
}

func (a *ADLine) Name() string {
	return "ADLine"
}

func (a *ADLine) Period() int {
	return 1
}

func (a *ADLine) Calculate(candles []models.Candle) ([]float64, error) {
	if len(candles) == 0 {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	result := make([]float64, n)

	var cumAD float64
	for i := 0; i < n; i++ {
		hl := candles[i].High - candles[i].Low
		if hl != 0 {
			mfm := ((candles[i].Close - candles[i].Low) - (candles[i].High - candles[i].Close)) / hl
			adv := mfm * float64(candles[i].Volume)
			cumAD += adv
		}
		result[i] = cumAD
	}

	return result, nil
}

// ForceIndex calculates the Force Index.
type ForceIndex struct {
	period int
}

// NewForceIndex creates a new Force Index indicator.
func NewForceIndex(period int) *ForceIndex {
	return &ForceIndex{period: period}
}

func (f *ForceIndex) Name() string {
	return fmt.Sprintf("ForceIndex_%d", f.period)
}

func (f *ForceIndex) Period() int {
	return f.period
}

func (f *ForceIndex) Calculate(candles []models.Candle) ([]float64, error) {
	if f.period <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < f.period+1 {
		return nil, ErrInsufficientData
	}

	n := len(candles)

	// Calculate raw Force Index
	rawFI := make([]float64, n)
	for i := 1; i < n; i++ {
		rawFI[i] = (candles[i].Close - candles[i-1].Close) * float64(candles[i].Volume)
	}

	// Apply EMA smoothing
	result := CalculateEMA(rawFI, f.period)

	return result, nil
}

// VolumeProfile calculates volume distribution at price levels.
type VolumeProfile struct {
	numBins int
}

// NewVolumeProfile creates a new Volume Profile indicator.
func NewVolumeProfile(numBins int) *VolumeProfile {
	return &VolumeProfile{numBins: numBins}
}

func (v *VolumeProfile) Name() string {
	return fmt.Sprintf("VolumeProfile_%d", v.numBins)
}

func (v *VolumeProfile) Period() int {
	return 1
}

// VolumeProfileResult holds the volume profile data.
type VolumeProfileResult struct {
	PriceLevels []float64
	Volumes     []int64
	POC         float64 // Point of Control (price with highest volume)
	VAH         float64 // Value Area High
	VAL         float64 // Value Area Low
}

// CalculateProfile calculates the volume profile for the given candles.
func (v *VolumeProfile) CalculateProfile(candles []models.Candle) (*VolumeProfileResult, error) {
	if len(candles) == 0 {
		return nil, ErrInsufficientData
	}
	if v.numBins <= 0 {
		return nil, ErrInvalidPeriod
	}

	// Find price range
	highs := highPrices(candles)
	lows := lowPrices(candles)
	maxPrice := highest(highs)
	minPrice := lowest(lows)

	if maxPrice == minPrice {
		return &VolumeProfileResult{
			PriceLevels: []float64{maxPrice},
			Volumes:     []int64{candles[0].Volume},
			POC:         maxPrice,
			VAH:         maxPrice,
			VAL:         minPrice,
		}, nil
	}

	binSize := (maxPrice - minPrice) / float64(v.numBins)
	priceLevels := make([]float64, v.numBins)
	volumes := make([]int64, v.numBins)

	for i := 0; i < v.numBins; i++ {
		priceLevels[i] = minPrice + float64(i)*binSize + binSize/2
	}

	// Distribute volume across bins
	for _, c := range candles {
		tp := typicalPrice(c)
		binIdx := int((tp - minPrice) / binSize)
		if binIdx >= v.numBins {
			binIdx = v.numBins - 1
		}
		if binIdx < 0 {
			binIdx = 0
		}
		volumes[binIdx] += c.Volume
	}

	// Find POC (Point of Control)
	var maxVol int64
	pocIdx := 0
	for i, vol := range volumes {
		if vol > maxVol {
			maxVol = vol
			pocIdx = i
		}
	}

	// Calculate Value Area (70% of volume)
	var totalVol int64
	for _, vol := range volumes {
		totalVol += vol
	}
	targetVol := int64(float64(totalVol) * 0.7)

	// Expand from POC until we capture 70% of volume
	vahIdx, valIdx := pocIdx, pocIdx
	var vaVol int64 = volumes[pocIdx]

	for vaVol < targetVol && (vahIdx < v.numBins-1 || valIdx > 0) {
		var upperVol, lowerVol int64
		if vahIdx < v.numBins-1 {
			upperVol = volumes[vahIdx+1]
		}
		if valIdx > 0 {
			lowerVol = volumes[valIdx-1]
		}

		if upperVol >= lowerVol && vahIdx < v.numBins-1 {
			vahIdx++
			vaVol += volumes[vahIdx]
		} else if valIdx > 0 {
			valIdx--
			vaVol += volumes[valIdx]
		} else {
			break
		}
	}

	return &VolumeProfileResult{
		PriceLevels: priceLevels,
		Volumes:     volumes,
		POC:         priceLevels[pocIdx],
		VAH:         priceLevels[vahIdx],
		VAL:         priceLevels[valIdx],
	}, nil
}
