package indicators

import (
	"zerodha-trader/internal/models"
)

// FibonacciLevels represents Fibonacci retracement levels.
type FibonacciLevels struct {
	SwingHigh float64
	SwingLow  float64
	IsUptrend bool
	Level0    float64 // 0%
	Level236  float64 // 23.6%
	Level382  float64 // 38.2%
	Level500  float64 // 50%
	Level618  float64 // 61.8%
	Level786  float64 // 78.6%
	Level1000 float64 // 100%
	Level1272 float64 // 127.2% extension
	Level1618 float64 // 161.8% extension
}

// FibonacciRetracement calculates Fibonacci retracement levels.
type FibonacciRetracement struct {
	lookbackPeriod int
}

// NewFibonacciRetracement creates a new Fibonacci Retracement calculator.
func NewFibonacciRetracement(lookbackPeriod int) *FibonacciRetracement {
	return &FibonacciRetracement{lookbackPeriod: lookbackPeriod}
}

func (f *FibonacciRetracement) Name() string {
	return "FibonacciRetracement"
}

func (f *FibonacciRetracement) Period() int {
	return f.lookbackPeriod
}

// Calculate finds swing high/low and calculates Fibonacci levels.
func (f *FibonacciRetracement) Calculate(candles []models.Candle) (*FibonacciLevels, error) {
	if len(candles) < f.lookbackPeriod {
		return nil, ErrInsufficientData
	}

	// Use the lookback period to find swing high and low
	lookbackCandles := candles
	if len(candles) > f.lookbackPeriod {
		lookbackCandles = candles[len(candles)-f.lookbackPeriod:]
	}

	highs := highPrices(lookbackCandles)
	lows := lowPrices(lookbackCandles)

	swingHigh := highest(highs)
	swingLow := lowest(lows)

	highIdx := highestIndex(highs)
	lowIdx := lowestIndex(lows)

	// Determine trend direction based on which came first
	isUptrend := lowIdx < highIdx

	return f.CalculateLevels(swingHigh, swingLow, isUptrend), nil
}

// CalculateLevels calculates Fibonacci levels from given swing points.
func (f *FibonacciRetracement) CalculateLevels(swingHigh, swingLow float64, isUptrend bool) *FibonacciLevels {
	diff := swingHigh - swingLow

	levels := &FibonacciLevels{
		SwingHigh: swingHigh,
		SwingLow:  swingLow,
		IsUptrend: isUptrend,
	}

	if isUptrend {
		// Retracement from high to low (price went up, now retracing down)
		levels.Level0 = swingHigh
		levels.Level236 = swingHigh - diff*0.236
		levels.Level382 = swingHigh - diff*0.382
		levels.Level500 = swingHigh - diff*0.500
		levels.Level618 = swingHigh - diff*0.618
		levels.Level786 = swingHigh - diff*0.786
		levels.Level1000 = swingLow
		levels.Level1272 = swingLow - diff*0.272
		levels.Level1618 = swingLow - diff*0.618
	} else {
		// Retracement from low to high (price went down, now retracing up)
		levels.Level0 = swingLow
		levels.Level236 = swingLow + diff*0.236
		levels.Level382 = swingLow + diff*0.382
		levels.Level500 = swingLow + diff*0.500
		levels.Level618 = swingLow + diff*0.618
		levels.Level786 = swingLow + diff*0.786
		levels.Level1000 = swingHigh
		levels.Level1272 = swingHigh + diff*0.272
		levels.Level1618 = swingHigh + diff*0.618
	}

	return levels
}

// PivotPoints represents standard pivot point levels.
type PivotPoints struct {
	Pivot float64 // Central pivot
	R1    float64 // Resistance 1
	R2    float64 // Resistance 2
	R3    float64 // Resistance 3
	S1    float64 // Support 1
	S2    float64 // Support 2
	S3    float64 // Support 3
}

// StandardPivotPoints calculates standard pivot points.
type StandardPivotPoints struct{}

// NewStandardPivotPoints creates a new Standard Pivot Points calculator.
func NewStandardPivotPoints() *StandardPivotPoints {
	return &StandardPivotPoints{}
}

func (s *StandardPivotPoints) Name() string {
	return "StandardPivotPoints"
}

func (s *StandardPivotPoints) Period() int {
	return 1
}

// Calculate calculates pivot points from the previous period's OHLC.
func (s *StandardPivotPoints) Calculate(high, low, close float64) *PivotPoints {
	pivot := (high + low + close) / 3

	return &PivotPoints{
		Pivot: pivot,
		R1:    2*pivot - low,
		R2:    pivot + (high - low),
		R3:    high + 2*(pivot-low),
		S1:    2*pivot - high,
		S2:    pivot - (high - low),
		S3:    low - 2*(high-pivot),
	}
}

// CalculateFromCandle calculates pivot points from a candle.
func (s *StandardPivotPoints) CalculateFromCandle(candle models.Candle) *PivotPoints {
	return s.Calculate(candle.High, candle.Low, candle.Close)
}

// CalculateFromCandles calculates pivot points from the last candle in the slice.
func (s *StandardPivotPoints) CalculateFromCandles(candles []models.Candle) (*PivotPoints, error) {
	if len(candles) == 0 {
		return nil, ErrInsufficientData
	}
	return s.CalculateFromCandle(candles[len(candles)-1]), nil
}

// WoodiePivotPoints calculates Woodie pivot points.
type WoodiePivotPoints struct{}

// NewWoodiePivotPoints creates a new Woodie Pivot Points calculator.
func NewWoodiePivotPoints() *WoodiePivotPoints {
	return &WoodiePivotPoints{}
}

func (w *WoodiePivotPoints) Name() string {
	return "WoodiePivotPoints"
}

// Calculate calculates Woodie pivot points.
func (w *WoodiePivotPoints) Calculate(high, low, close float64) *PivotPoints {
	pivot := (high + low + 2*close) / 4

	return &PivotPoints{
		Pivot: pivot,
		R1:    2*pivot - low,
		R2:    pivot + (high - low),
		R3:    high + 2*(pivot-low),
		S1:    2*pivot - high,
		S2:    pivot - (high - low),
		S3:    low - 2*(high-pivot),
	}
}

// CamarillaPivotPoints calculates Camarilla pivot points.
type CamarillaPivotPoints struct{}

// NewCamarillaPivotPoints creates a new Camarilla Pivot Points calculator.
func NewCamarillaPivotPoints() *CamarillaPivotPoints {
	return &CamarillaPivotPoints{}
}

func (c *CamarillaPivotPoints) Name() string {
	return "CamarillaPivotPoints"
}

// Calculate calculates Camarilla pivot points.
func (c *CamarillaPivotPoints) Calculate(high, low, close float64) *PivotPoints {
	diff := high - low

	return &PivotPoints{
		Pivot: (high + low + close) / 3,
		R1:    close + diff*1.1/12,
		R2:    close + diff*1.1/6,
		R3:    close + diff*1.1/4,
		S1:    close - diff*1.1/12,
		S2:    close - diff*1.1/6,
		S3:    close - diff*1.1/4,
	}
}

// DeMark pivot points
type DeMarkPivotPoints struct{}

// NewDeMarkPivotPoints creates a new DeMark Pivot Points calculator.
func NewDeMarkPivotPoints() *DeMarkPivotPoints {
	return &DeMarkPivotPoints{}
}

func (d *DeMarkPivotPoints) Name() string {
	return "DeMarkPivotPoints"
}

// Calculate calculates DeMark pivot points.
func (d *DeMarkPivotPoints) Calculate(open, high, low, close float64) *PivotPoints {
	var x float64

	if close < open {
		x = high + 2*low + close
	} else if close > open {
		x = 2*high + low + close
	} else {
		x = high + low + 2*close
	}

	pivot := x / 4

	return &PivotPoints{
		Pivot: pivot,
		R1:    x/2 - low,
		S1:    x/2 - high,
		// DeMark only defines R1 and S1, others set to 0
	}
}
