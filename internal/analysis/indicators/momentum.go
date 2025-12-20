package indicators

import (
	"fmt"

	"zerodha-trader/internal/models"
)

// RSI calculates the Relative Strength Index.
type RSI struct {
	period int
}

// NewRSI creates a new RSI indicator.
func NewRSI(period int) *RSI {
	return &RSI{period: period}
}

func (r *RSI) Name() string {
	return fmt.Sprintf("RSI_%d", r.period)
}

func (r *RSI) Period() int {
	return r.period
}

func (r *RSI) Calculate(candles []models.Candle) ([]float64, error) {
	if r.period <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < r.period+1 {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	result := make([]float64, n)
	closes := closePrices(candles)

	gains := make([]float64, n)
	losses := make([]float64, n)

	// Calculate gains and losses
	for i := 1; i < n; i++ {
		change := closes[i] - closes[i-1]
		if change > 0 {
			gains[i] = change
		} else {
			losses[i] = -change
		}
	}

	// First average using SMA
	avgGain := mean(gains[1 : r.period+1])
	avgLoss := mean(losses[1 : r.period+1])

	if avgLoss == 0 {
		result[r.period] = 100
	} else {
		rs := avgGain / avgLoss
		result[r.period] = 100 - (100 / (1 + rs))
	}

	// Subsequent values using Wilder smoothing
	for i := r.period + 1; i < n; i++ {
		avgGain = (avgGain*float64(r.period-1) + gains[i]) / float64(r.period)
		avgLoss = (avgLoss*float64(r.period-1) + losses[i]) / float64(r.period)

		if avgLoss == 0 {
			result[i] = 100
		} else {
			rs := avgGain / avgLoss
			result[i] = 100 - (100 / (1 + rs))
		}
	}

	return result, nil
}

// Stochastic calculates the Stochastic Oscillator (%K and %D).
type Stochastic struct {
	kPeriod int
	dPeriod int
	smooth  int
}

// NewStochastic creates a new Stochastic indicator.
func NewStochastic(kPeriod, dPeriod, smooth int) *Stochastic {
	return &Stochastic{
		kPeriod: kPeriod,
		dPeriod: dPeriod,
		smooth:  smooth,
	}
}

func (s *Stochastic) Name() string {
	return fmt.Sprintf("Stochastic_%d_%d_%d", s.kPeriod, s.dPeriod, s.smooth)
}

func (s *Stochastic) Period() int {
	return s.kPeriod + s.dPeriod
}

func (s *Stochastic) Calculate(candles []models.Candle) (map[string][]float64, error) {
	if s.kPeriod <= 0 || s.dPeriod <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < s.Period() {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	highs := highPrices(candles)
	lows := lowPrices(candles)
	closes := closePrices(candles)

	rawK := make([]float64, n)
	percentK := make([]float64, n)
	percentD := make([]float64, n)

	// Calculate raw %K
	for i := s.kPeriod - 1; i < n; i++ {
		highestHigh := highest(highs[i-s.kPeriod+1 : i+1])
		lowestLow := lowest(lows[i-s.kPeriod+1 : i+1])

		if highestHigh == lowestLow {
			rawK[i] = 50
		} else {
			rawK[i] = 100 * (closes[i] - lowestLow) / (highestHigh - lowestLow)
		}
	}

	// Smooth %K
	if s.smooth > 1 {
		for i := s.kPeriod + s.smooth - 2; i < n; i++ {
			percentK[i] = mean(rawK[i-s.smooth+1 : i+1])
		}
	} else {
		copy(percentK, rawK)
	}

	// Calculate %D (SMA of %K)
	startIdx := s.kPeriod - 1
	if s.smooth > 1 {
		startIdx = s.kPeriod + s.smooth - 2
	}
	for i := startIdx + s.dPeriod - 1; i < n; i++ {
		percentD[i] = mean(percentK[i-s.dPeriod+1 : i+1])
	}

	return map[string][]float64{
		"percent_k": percentK,
		"percent_d": percentD,
	}, nil
}


// CCI calculates the Commodity Channel Index.
type CCI struct {
	period int
}

// NewCCI creates a new CCI indicator.
func NewCCI(period int) *CCI {
	return &CCI{period: period}
}

func (c *CCI) Name() string {
	return fmt.Sprintf("CCI_%d", c.period)
}

func (c *CCI) Period() int {
	return c.period
}

func (c *CCI) Calculate(candles []models.Candle) ([]float64, error) {
	if c.period <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < c.period {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	result := make([]float64, n)

	// Calculate typical prices
	tp := make([]float64, n)
	for i := 0; i < n; i++ {
		tp[i] = typicalPrice(candles[i])
	}

	for i := c.period - 1; i < n; i++ {
		tpSlice := tp[i-c.period+1 : i+1]
		sma := mean(tpSlice)

		// Calculate mean deviation
		var meanDev float64
		for _, v := range tpSlice {
			meanDev += abs(v - sma)
		}
		meanDev /= float64(c.period)

		if meanDev == 0 {
			result[i] = 0
		} else {
			result[i] = (tp[i] - sma) / (0.015 * meanDev)
		}
	}

	return result, nil
}

// WilliamsR calculates Williams %R.
type WilliamsR struct {
	period int
}

// NewWilliamsR creates a new Williams %R indicator.
func NewWilliamsR(period int) *WilliamsR {
	return &WilliamsR{period: period}
}

func (w *WilliamsR) Name() string {
	return fmt.Sprintf("WilliamsR_%d", w.period)
}

func (w *WilliamsR) Period() int {
	return w.period
}

func (w *WilliamsR) Calculate(candles []models.Candle) ([]float64, error) {
	if w.period <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < w.period {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	result := make([]float64, n)
	highs := highPrices(candles)
	lows := lowPrices(candles)
	closes := closePrices(candles)

	for i := w.period - 1; i < n; i++ {
		highestHigh := highest(highs[i-w.period+1 : i+1])
		lowestLow := lowest(lows[i-w.period+1 : i+1])

		if highestHigh == lowestLow {
			result[i] = -50
		} else {
			wr := -100 * (highestHigh - closes[i]) / (highestHigh - lowestLow)
			// Clamp to valid range [-100, 0] to handle edge cases
			// where close might be slightly outside the high-low range
			result[i] = max(-100, min(0, wr))
		}
	}

	return result, nil
}

// ROC calculates Rate of Change.
type ROC struct {
	period int
}

// NewROC creates a new ROC indicator.
func NewROC(period int) *ROC {
	return &ROC{period: period}
}

func (r *ROC) Name() string {
	return fmt.Sprintf("ROC_%d", r.period)
}

func (r *ROC) Period() int {
	return r.period
}

func (r *ROC) Calculate(candles []models.Candle) ([]float64, error) {
	if r.period <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < r.period+1 {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	result := make([]float64, n)
	closes := closePrices(candles)

	for i := r.period; i < n; i++ {
		if closes[i-r.period] != 0 {
			result[i] = 100 * (closes[i] - closes[i-r.period]) / closes[i-r.period]
		}
	}

	return result, nil
}

// Momentum calculates the Momentum indicator.
type Momentum struct {
	period int
}

// NewMomentum creates a new Momentum indicator.
func NewMomentum(period int) *Momentum {
	return &Momentum{period: period}
}

func (m *Momentum) Name() string {
	return fmt.Sprintf("Momentum_%d", m.period)
}

func (m *Momentum) Period() int {
	return m.period
}

func (m *Momentum) Calculate(candles []models.Candle) ([]float64, error) {
	if m.period <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < m.period+1 {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	result := make([]float64, n)
	closes := closePrices(candles)

	for i := m.period; i < n; i++ {
		result[i] = closes[i] - closes[i-m.period]
	}

	return result, nil
}


// UltimateOscillator calculates the Ultimate Oscillator.
type UltimateOscillator struct {
	period1 int
	period2 int
	period3 int
}

// NewUltimateOscillator creates a new Ultimate Oscillator indicator.
func NewUltimateOscillator(period1, period2, period3 int) *UltimateOscillator {
	return &UltimateOscillator{
		period1: period1,
		period2: period2,
		period3: period3,
	}
}

func (u *UltimateOscillator) Name() string {
	return fmt.Sprintf("UltimateOscillator_%d_%d_%d", u.period1, u.period2, u.period3)
}

func (u *UltimateOscillator) Period() int {
	return u.period3
}

func (u *UltimateOscillator) Calculate(candles []models.Candle) ([]float64, error) {
	if u.period1 <= 0 || u.period2 <= 0 || u.period3 <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < u.period3+1 {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	result := make([]float64, n)

	// Calculate Buying Pressure (BP) and True Range (TR)
	bp := make([]float64, n)
	tr := make([]float64, n)

	for i := 1; i < n; i++ {
		trueLow := min(candles[i].Low, candles[i-1].Close)
		trueHigh := max(candles[i].High, candles[i-1].Close)
		bp[i] = candles[i].Close - trueLow
		tr[i] = trueHigh - trueLow
	}

	for i := u.period3; i < n; i++ {
		// Calculate averages for each period
		var avg1, avg2, avg3 float64

		bpSum1 := sum(bp[i-u.period1+1 : i+1])
		trSum1 := sum(tr[i-u.period1+1 : i+1])
		if trSum1 != 0 {
			avg1 = bpSum1 / trSum1
		}

		bpSum2 := sum(bp[i-u.period2+1 : i+1])
		trSum2 := sum(tr[i-u.period2+1 : i+1])
		if trSum2 != 0 {
			avg2 = bpSum2 / trSum2
		}

		bpSum3 := sum(bp[i-u.period3+1 : i+1])
		trSum3 := sum(tr[i-u.period3+1 : i+1])
		if trSum3 != 0 {
			avg3 = bpSum3 / trSum3
		}

		// Ultimate Oscillator formula with weights 4, 2, 1
		result[i] = 100 * (4*avg1 + 2*avg2 + avg3) / 7
	}

	return result, nil
}
