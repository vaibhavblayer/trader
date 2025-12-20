package indicators

import (
	"fmt"

	"zerodha-trader/internal/models"
)

// SMA calculates Simple Moving Average.
type SMA struct {
	period int
}

// NewSMA creates a new SMA indicator.
func NewSMA(period int) *SMA {
	return &SMA{period: period}
}

func (s *SMA) Name() string {
	return fmt.Sprintf("SMA_%d", s.period)
}

func (s *SMA) Period() int {
	return s.period
}

func (s *SMA) Calculate(candles []models.Candle) ([]float64, error) {
	if s.period <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < s.period {
		return nil, ErrInsufficientData
	}

	result := make([]float64, len(candles))
	closes := closePrices(candles)

	for i := s.period - 1; i < len(candles); i++ {
		result[i] = mean(closes[i-s.period+1 : i+1])
	}

	return result, nil
}

// EMA calculates Exponential Moving Average.
type EMA struct {
	period int
}

// NewEMA creates a new EMA indicator.
func NewEMA(period int) *EMA {
	return &EMA{period: period}
}

func (e *EMA) Name() string {
	return fmt.Sprintf("EMA_%d", e.period)
}

func (e *EMA) Period() int {
	return e.period
}

func (e *EMA) Calculate(candles []models.Candle) ([]float64, error) {
	if e.period <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < e.period {
		return nil, ErrInsufficientData
	}

	result := make([]float64, len(candles))
	closes := closePrices(candles)
	multiplier := 2.0 / float64(e.period+1)

	// First EMA is SMA
	result[e.period-1] = mean(closes[:e.period])

	// Calculate EMA for remaining values
	for i := e.period; i < len(candles); i++ {
		result[i] = (closes[i]-result[i-1])*multiplier + result[i-1]
	}

	return result, nil
}

// CalculateEMA calculates EMA on raw values (helper for other indicators).
func CalculateEMA(values []float64, period int) []float64 {
	if len(values) < period || period <= 0 {
		return nil
	}

	result := make([]float64, len(values))
	multiplier := 2.0 / float64(period+1)

	result[period-1] = mean(values[:period])

	for i := period; i < len(values); i++ {
		result[i] = (values[i]-result[i-1])*multiplier + result[i-1]
	}

	return result
}


// MACD calculates Moving Average Convergence Divergence.
type MACD struct {
	fastPeriod   int
	slowPeriod   int
	signalPeriod int
}

// NewMACD creates a new MACD indicator with default periods (12, 26, 9).
func NewMACD(fast, slow, signal int) *MACD {
	return &MACD{
		fastPeriod:   fast,
		slowPeriod:   slow,
		signalPeriod: signal,
	}
}

func (m *MACD) Name() string {
	return fmt.Sprintf("MACD_%d_%d_%d", m.fastPeriod, m.slowPeriod, m.signalPeriod)
}

func (m *MACD) Period() int {
	return m.slowPeriod + m.signalPeriod - 1
}

func (m *MACD) Calculate(candles []models.Candle) (map[string][]float64, error) {
	if m.fastPeriod <= 0 || m.slowPeriod <= 0 || m.signalPeriod <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < m.Period() {
		return nil, ErrInsufficientData
	}

	closes := closePrices(candles)
	fastEMA := CalculateEMA(closes, m.fastPeriod)
	slowEMA := CalculateEMA(closes, m.slowPeriod)

	// MACD Line = Fast EMA - Slow EMA
	macdLine := make([]float64, len(candles))
	for i := m.slowPeriod - 1; i < len(candles); i++ {
		macdLine[i] = fastEMA[i] - slowEMA[i]
	}

	// Signal Line = EMA of MACD Line
	signalLine := make([]float64, len(candles))
	startIdx := m.slowPeriod - 1
	macdValues := macdLine[startIdx:]
	signalEMA := CalculateEMA(macdValues, m.signalPeriod)
	for i := 0; i < len(signalEMA); i++ {
		signalLine[startIdx+i] = signalEMA[i]
	}

	// Histogram = MACD Line - Signal Line
	histogram := make([]float64, len(candles))
	for i := m.Period() - 1; i < len(candles); i++ {
		histogram[i] = macdLine[i] - signalLine[i]
	}

	return map[string][]float64{
		"macd":      macdLine,
		"signal":    signalLine,
		"histogram": histogram,
	}, nil
}

// ADX calculates Average Directional Index with +DI and -DI.
type ADX struct {
	period int
}

// NewADX creates a new ADX indicator.
func NewADX(period int) *ADX {
	return &ADX{period: period}
}

func (a *ADX) Name() string {
	return fmt.Sprintf("ADX_%d", a.period)
}

func (a *ADX) Period() int {
	return a.period * 2
}

func (a *ADX) Calculate(candles []models.Candle) (map[string][]float64, error) {
	if a.period <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < a.Period() {
		return nil, ErrInsufficientData
	}

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
		tr[i] = trueRange(candles[i], candles[i-1])
	}

	// Smooth using Wilder's method
	smoothPlusDM := wilderSmooth(plusDM, a.period)
	smoothMinusDM := wilderSmooth(minusDM, a.period)
	smoothTR := wilderSmooth(tr, a.period)

	// Calculate +DI and -DI
	plusDI := make([]float64, n)
	minusDI := make([]float64, n)
	dx := make([]float64, n)

	for i := a.period; i < n; i++ {
		if smoothTR[i] != 0 {
			plusDI[i] = 100 * smoothPlusDM[i] / smoothTR[i]
			minusDI[i] = 100 * smoothMinusDM[i] / smoothTR[i]
		}
		diSum := plusDI[i] + minusDI[i]
		if diSum != 0 {
			dx[i] = 100 * abs(plusDI[i]-minusDI[i]) / diSum
		}
	}

	// Calculate ADX (smoothed DX)
	adx := wilderSmooth(dx[a.period:], a.period)
	adxResult := make([]float64, n)
	for i := 0; i < len(adx); i++ {
		adxResult[a.period+i] = adx[i]
	}

	return map[string][]float64{
		"adx":     adxResult,
		"plus_di": plusDI,
		"minus_di": minusDI,
	}, nil
}


// SuperTrend calculates the SuperTrend indicator.
type SuperTrend struct {
	atrPeriod  int
	multiplier float64
}

// NewSuperTrend creates a new SuperTrend indicator.
func NewSuperTrend(atrPeriod int, multiplier float64) *SuperTrend {
	return &SuperTrend{
		atrPeriod:  atrPeriod,
		multiplier: multiplier,
	}
}

func (s *SuperTrend) Name() string {
	return fmt.Sprintf("SuperTrend_%d_%.1f", s.atrPeriod, s.multiplier)
}

func (s *SuperTrend) Period() int {
	return s.atrPeriod
}

func (s *SuperTrend) Calculate(candles []models.Candle) (map[string][]float64, error) {
	if s.atrPeriod <= 0 || s.multiplier <= 0 {
		return nil, ErrInvalidPeriod
	}
	if len(candles) < s.atrPeriod {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	atr := &ATR{period: s.atrPeriod}
	atrValues, err := atr.Calculate(candles)
	if err != nil {
		return nil, err
	}

	superTrend := make([]float64, n)
	direction := make([]float64, n) // 1 = bullish, -1 = bearish

	upperBand := make([]float64, n)
	lowerBand := make([]float64, n)

	for i := s.atrPeriod - 1; i < n; i++ {
		hl2 := (candles[i].High + candles[i].Low) / 2
		upperBand[i] = hl2 + s.multiplier*atrValues[i]
		lowerBand[i] = hl2 - s.multiplier*atrValues[i]

		if i == s.atrPeriod-1 {
			superTrend[i] = upperBand[i]
			direction[i] = -1
			continue
		}

		// Adjust bands based on previous values
		if lowerBand[i] < lowerBand[i-1] && candles[i-1].Close > lowerBand[i-1] {
			lowerBand[i] = lowerBand[i-1]
		}
		if upperBand[i] > upperBand[i-1] && candles[i-1].Close < upperBand[i-1] {
			upperBand[i] = upperBand[i-1]
		}

		// Determine trend direction
		if superTrend[i-1] == upperBand[i-1] {
			if candles[i].Close > upperBand[i] {
				superTrend[i] = lowerBand[i]
				direction[i] = 1
			} else {
				superTrend[i] = upperBand[i]
				direction[i] = -1
			}
		} else {
			if candles[i].Close < lowerBand[i] {
				superTrend[i] = upperBand[i]
				direction[i] = -1
			} else {
				superTrend[i] = lowerBand[i]
				direction[i] = 1
			}
		}
	}

	return map[string][]float64{
		"supertrend": superTrend,
		"direction":  direction,
		"upper_band": upperBand,
		"lower_band": lowerBand,
	}, nil
}

// ParabolicSAR calculates the Parabolic Stop and Reverse indicator.
type ParabolicSAR struct {
	afStart float64
	afStep  float64
	afMax   float64
}

// NewParabolicSAR creates a new Parabolic SAR indicator.
func NewParabolicSAR(afStart, afStep, afMax float64) *ParabolicSAR {
	return &ParabolicSAR{
		afStart: afStart,
		afStep:  afStep,
		afMax:   afMax,
	}
}

func (p *ParabolicSAR) Name() string {
	return "ParabolicSAR"
}

func (p *ParabolicSAR) Period() int {
	return 2
}

func (p *ParabolicSAR) Calculate(candles []models.Candle) (map[string][]float64, error) {
	if len(candles) < 2 {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	sar := make([]float64, n)
	direction := make([]float64, n) // 1 = bullish, -1 = bearish

	// Initialize
	isUpTrend := candles[1].Close > candles[0].Close
	af := p.afStart
	var ep float64

	if isUpTrend {
		sar[0] = candles[0].Low
		ep = candles[0].High
		direction[0] = 1
	} else {
		sar[0] = candles[0].High
		ep = candles[0].Low
		direction[0] = -1
	}

	for i := 1; i < n; i++ {
		if isUpTrend {
			sar[i] = sar[i-1] + af*(ep-sar[i-1])
			sar[i] = min(sar[i], candles[i-1].Low)
			if i >= 2 {
				sar[i] = min(sar[i], candles[i-2].Low)
			}

			if candles[i].Low < sar[i] {
				isUpTrend = false
				sar[i] = ep
				ep = candles[i].Low
				af = p.afStart
			} else {
				if candles[i].High > ep {
					ep = candles[i].High
					af = min(af+p.afStep, p.afMax)
				}
			}
		} else {
			sar[i] = sar[i-1] + af*(ep-sar[i-1])
			sar[i] = max(sar[i], candles[i-1].High)
			if i >= 2 {
				sar[i] = max(sar[i], candles[i-2].High)
			}

			if candles[i].High > sar[i] {
				isUpTrend = true
				sar[i] = ep
				ep = candles[i].High
				af = p.afStart
			} else {
				if candles[i].Low < ep {
					ep = candles[i].Low
					af = min(af+p.afStep, p.afMax)
				}
			}
		}

		if isUpTrend {
			direction[i] = 1
		} else {
			direction[i] = -1
		}
	}

	return map[string][]float64{
		"sar":       sar,
		"direction": direction,
	}, nil
}


// IchimokuCloud calculates the Ichimoku Cloud indicator.
type IchimokuCloud struct {
	tenkanPeriod  int
	kijunPeriod   int
	senkouBPeriod int
	displacement  int
}

// NewIchimokuCloud creates a new Ichimoku Cloud indicator with default periods.
func NewIchimokuCloud(tenkan, kijun, senkouB, displacement int) *IchimokuCloud {
	return &IchimokuCloud{
		tenkanPeriod:  tenkan,
		kijunPeriod:   kijun,
		senkouBPeriod: senkouB,
		displacement:  displacement,
	}
}

func (i *IchimokuCloud) Name() string {
	return "IchimokuCloud"
}

func (i *IchimokuCloud) Period() int {
	return i.senkouBPeriod + i.displacement
}

func (i *IchimokuCloud) Calculate(candles []models.Candle) (map[string][]float64, error) {
	if len(candles) < i.senkouBPeriod {
		return nil, ErrInsufficientData
	}

	n := len(candles)
	highs := highPrices(candles)
	lows := lowPrices(candles)

	tenkanSen := make([]float64, n)
	kijunSen := make([]float64, n)
	senkouSpanA := make([]float64, n+i.displacement)
	senkouSpanB := make([]float64, n+i.displacement)
	chikouSpan := make([]float64, n)

	// Tenkan-sen (Conversion Line)
	for j := i.tenkanPeriod - 1; j < n; j++ {
		h := highest(highs[j-i.tenkanPeriod+1 : j+1])
		l := lowest(lows[j-i.tenkanPeriod+1 : j+1])
		tenkanSen[j] = (h + l) / 2
	}

	// Kijun-sen (Base Line)
	for j := i.kijunPeriod - 1; j < n; j++ {
		h := highest(highs[j-i.kijunPeriod+1 : j+1])
		l := lowest(lows[j-i.kijunPeriod+1 : j+1])
		kijunSen[j] = (h + l) / 2
	}

	// Senkou Span A (Leading Span A) - displaced forward
	for j := i.kijunPeriod - 1; j < n; j++ {
		senkouSpanA[j+i.displacement] = (tenkanSen[j] + kijunSen[j]) / 2
	}

	// Senkou Span B (Leading Span B) - displaced forward
	for j := i.senkouBPeriod - 1; j < n; j++ {
		h := highest(highs[j-i.senkouBPeriod+1 : j+1])
		l := lowest(lows[j-i.senkouBPeriod+1 : j+1])
		senkouSpanB[j+i.displacement] = (h + l) / 2
	}

	// Chikou Span (Lagging Span) - displaced backward
	for j := i.displacement; j < n; j++ {
		chikouSpan[j-i.displacement] = candles[j].Close
	}

	// Trim to original length
	senkouSpanA = senkouSpanA[:n]
	senkouSpanB = senkouSpanB[:n]

	return map[string][]float64{
		"tenkan_sen":    tenkanSen,
		"kijun_sen":     kijunSen,
		"senkou_span_a": senkouSpanA,
		"senkou_span_b": senkouSpanB,
		"chikou_span":   chikouSpan,
	}, nil
}
