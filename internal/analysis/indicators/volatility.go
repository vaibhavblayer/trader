package indicators

import (
	"fmt"
	"math"

	"zerodha-trader/internal/models"
)

// ATR calculates the Average True Range.
type ATR struct {
	period int
}

// NewATR creates a new ATR indicator.
func NewATR(period int) *ATR {
	return &ATR{period: period}
}

func (a *ATR) Name() string {
	return fmt.Sprintf("ATR_%d", a.period)
}

func (a *ATR) Period() int {
	return a.period
}

func (a *ATR) Calculate(candles []models.Candle) ([]float64, error) {
	if a.period <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < a.period+1 {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	result := make([]float64, n)
	tr := make([]float64, n)

	// First TR is just high - low
	tr[0] = candles[0].High - candles[0].Low

	// Calculate True Range for remaining candles
	for i := 1; i < n; i++ {
		tr[i] = trueRange(candles[i], candles[i-1])
	}

	// First ATR is SMA of TR
	result[a.period-1] = mean(tr[:a.period])

	// Subsequent ATR using Wilder smoothing
	for i := a.period; i < n; i++ {
		result[i] = (result[i-1]*float64(a.period-1) + tr[i]) / float64(a.period)
	}

	return result, nil
}

// BollingerBands calculates Bollinger Bands.
type BollingerBands struct {
	period    int
	stdDevMul float64
}

// NewBollingerBands creates a new Bollinger Bands indicator.
func NewBollingerBands(period int, stdDevMul float64) *BollingerBands {
	return &BollingerBands{
		period:    period,
		stdDevMul: stdDevMul,
	}
}

func (b *BollingerBands) Name() string {
	return fmt.Sprintf("BollingerBands_%d_%.1f", b.period, b.stdDevMul)
}

func (b *BollingerBands) Period() int {
	return b.period
}

func (b *BollingerBands) Calculate(candles []models.Candle) (map[string][]float64, error) {
	if b.period <= 0 || b.stdDevMul <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < b.period {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	closes := closePrices(candles)

	middle := make([]float64, n)
	upper := make([]float64, n)
	lower := make([]float64, n)
	bandwidth := make([]float64, n)
	percentB := make([]float64, n)

	for i := b.period - 1; i < n; i++ {
		slice := closes[i-b.period+1 : i+1]
		sma := mean(slice)
		sd := stdDev(slice)

		middle[i] = sma
		upper[i] = sma + b.stdDevMul*sd
		lower[i] = sma - b.stdDevMul*sd

		// Bandwidth = (Upper - Lower) / Middle
		if middle[i] != 0 {
			bandwidth[i] = (upper[i] - lower[i]) / middle[i]
		}

		// %B = (Price - Lower) / (Upper - Lower)
		bandWidth := upper[i] - lower[i]
		if bandWidth != 0 {
			percentB[i] = (closes[i] - lower[i]) / bandWidth
		}
	}

	return map[string][]float64{
		"middle":    middle,
		"upper":     upper,
		"lower":     lower,
		"bandwidth": bandwidth,
		"percent_b": percentB,
	}, nil
}

// KeltnerChannels calculates Keltner Channels.
type KeltnerChannels struct {
	emaPeriod  int
	atrPeriod  int
	multiplier float64
}

// NewKeltnerChannels creates a new Keltner Channels indicator.
func NewKeltnerChannels(emaPeriod, atrPeriod int, multiplier float64) *KeltnerChannels {
	return &KeltnerChannels{
		emaPeriod:  emaPeriod,
		atrPeriod:  atrPeriod,
		multiplier: multiplier,
	}
}

func (k *KeltnerChannels) Name() string {
	return fmt.Sprintf("KeltnerChannels_%d_%d_%.1f", k.emaPeriod, k.atrPeriod, k.multiplier)
}

func (k *KeltnerChannels) Period() int {
	if k.emaPeriod > k.atrPeriod {
		return k.emaPeriod
	}
	return k.atrPeriod
}

func (k *KeltnerChannels) Calculate(candles []models.Candle) (map[string][]float64, error) {
	if k.emaPeriod <= 0 || k.atrPeriod <= 0 || k.multiplier <= 0 {
		return nil, ErrInvalidPeriod
	}
	minPeriod := k.emaPeriod
	if k.atrPeriod > minPeriod {
		minPeriod = k.atrPeriod
	}
	if len(candles) < minPeriod+1 {
		return nil, ErrInsufficientData
	}

	n := len(candles)

	// Calculate EMA of typical price
	tp := make([]float64, n)
	for i := 0; i < n; i++ {
		tp[i] = typicalPrice(candles[i])
	}
	ema := CalculateEMA(tp, k.emaPeriod)

	// Calculate ATR
	atr := &ATR{period: k.atrPeriod}
	atrValues, err := atr.Calculate(candles)
	if err != nil {
		return nil, err
	}

	middle := make([]float64, n)
	upper := make([]float64, n)
	lower := make([]float64, n)

	startIdx := minPeriod - 1
	for i := startIdx; i < n; i++ {
		middle[i] = ema[i]
		upper[i] = ema[i] + k.multiplier*atrValues[i]
		lower[i] = ema[i] - k.multiplier*atrValues[i]
	}

	return map[string][]float64{
		"middle": middle,
		"upper":  upper,
		"lower":  lower,
	}, nil
}


// DonchianChannels calculates Donchian Channels.
type DonchianChannels struct {
	period int
}

// NewDonchianChannels creates a new Donchian Channels indicator.
func NewDonchianChannels(period int) *DonchianChannels {
	return &DonchianChannels{period: period}
}

func (d *DonchianChannels) Name() string {
	return fmt.Sprintf("DonchianChannels_%d", d.period)
}

func (d *DonchianChannels) Period() int {
	return d.period
}

func (d *DonchianChannels) Calculate(candles []models.Candle) (map[string][]float64, error) {
	if d.period <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < d.period {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	highs := highPrices(candles)
	lows := lowPrices(candles)

	upper := make([]float64, n)
	lower := make([]float64, n)
	middle := make([]float64, n)

	for i := d.period - 1; i < n; i++ {
		upper[i] = highest(highs[i-d.period+1 : i+1])
		lower[i] = lowest(lows[i-d.period+1 : i+1])
		middle[i] = (upper[i] + lower[i]) / 2
	}

	return map[string][]float64{
		"upper":  upper,
		"lower":  lower,
		"middle": middle,
	}, nil
}

// HistoricalVolatility calculates Historical Volatility (annualized).
type HistoricalVolatility struct {
	period      int
	tradingDays int // typically 252 for annual
}

// NewHistoricalVolatility creates a new Historical Volatility indicator.
func NewHistoricalVolatility(period, tradingDays int) *HistoricalVolatility {
	return &HistoricalVolatility{
		period:      period,
		tradingDays: tradingDays,
	}
}

func (h *HistoricalVolatility) Name() string {
	return fmt.Sprintf("HistoricalVolatility_%d", h.period)
}

func (h *HistoricalVolatility) Period() int {
	return h.period
}

func (h *HistoricalVolatility) Calculate(candles []models.Candle) ([]float64, error) {
	if h.period <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < h.period+1 {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	result := make([]float64, n)
	closes := closePrices(candles)

	// Calculate log returns
	logReturns := make([]float64, n)
	for i := 1; i < n; i++ {
		if closes[i-1] > 0 {
			logReturns[i] = math.Log(closes[i] / closes[i-1])
		}
	}

	// Calculate rolling standard deviation of log returns
	annualizationFactor := math.Sqrt(float64(h.tradingDays))
	for i := h.period; i < n; i++ {
		slice := logReturns[i-h.period+1 : i+1]
		sd := stdDev(slice)
		result[i] = sd * annualizationFactor * 100 // as percentage
	}

	return result, nil
}
