package indicators

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"zerodha-trader/internal/models"
)

// Feature: zerodha-go-trader, Property 3: All indicator values within mathematical bounds
// Validates: Requirements 4.1-4.14
//
// Property: For any valid candle data, all indicator calculations should produce
// values within their mathematically defined bounds:
// - RSI: [0, 100]
// - Stochastic %K and %D: [0, 100]
// - Williams %R: [-100, 0]
// - CCI: unbounded but typically [-200, 200]
// - MFI: [0, 100]
// - CMF: [-1, 1]
// - ADX: [0, 100]
// - +DI/-DI: [0, 100]

// candleGen generates valid candle data with realistic OHLCV values
func candleGen() gopter.Gen {
	return gen.Struct(reflect.TypeOf(models.Candle{}), map[string]gopter.Gen{
		"Timestamp": gen.TimeRange(time.Now().Add(-365*24*time.Hour), time.Hour),
		"Open":      gen.Float64Range(100.0, 1000.0),
		"High":      gen.Float64Range(100.0, 1000.0),
		"Low":       gen.Float64Range(100.0, 1000.0),
		"Close":     gen.Float64Range(100.0, 1000.0),
		"Volume":    gen.Int64Range(1000, 10000000),
	}).Map(func(c models.Candle) models.Candle {
		// Ensure all prices are positive (avoid zero/negative values)
		if c.Open <= 0 {
			c.Open = 100.0
		}
		if c.High <= 0 {
			c.High = 100.0
		}
		if c.Low <= 0 {
			c.Low = 100.0
		}
		if c.Close <= 0 {
			c.Close = 100.0
		}
		// Ensure OHLC constraints: High >= max(Open, Close) and Low <= min(Open, Close)
		c.High = math.Max(c.High, math.Max(c.Open, c.Close))
		c.Low = math.Min(c.Low, math.Min(c.Open, c.Close))
		if c.Low > c.High {
			c.Low, c.High = c.High, c.Low
		}
		// Ensure there's some price range (avoid flat candles where High == Low)
		if c.High <= c.Low {
			c.High = c.Low + 1.0 // Add minimum range
		}
		return c
	})
}

// candleSliceGen generates a slice of valid candles
func candleSliceGen(minLen, maxLen int) gopter.Gen {
	return gen.SliceOfN(maxLen, candleGen()).Map(func(candles []models.Candle) []models.Candle {
		if len(candles) < minLen {
			// Pad with copies if needed
			for len(candles) < minLen {
				candles = append(candles, candles[len(candles)-1])
			}
		}
		// Sort by timestamp and ensure valid candles
		for i := range candles {
			candles[i].Timestamp = time.Now().Add(time.Duration(i) * time.Hour)
			// Re-validate each candle after shrinking
			if candles[i].Open <= 0 {
				candles[i].Open = 100.0
			}
			if candles[i].High <= 0 {
				candles[i].High = 100.0
			}
			if candles[i].Low <= 0 {
				candles[i].Low = 100.0
			}
			if candles[i].Close <= 0 {
				candles[i].Close = 100.0
			}
			// Ensure OHLC constraints
			candles[i].High = math.Max(candles[i].High, math.Max(candles[i].Open, candles[i].Close))
			candles[i].Low = math.Min(candles[i].Low, math.Min(candles[i].Open, candles[i].Close))
			if candles[i].Low > candles[i].High {
				candles[i].Low, candles[i].High = candles[i].High, candles[i].Low
			}
			if candles[i].High <= candles[i].Low {
				candles[i].High = candles[i].Low + 1.0
			}
		}
		return candles
	})
}

func TestProperty_RSIWithinBounds(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	properties.Property("RSI values are within [0, 100]", prop.ForAll(
		func(candles []models.Candle) bool {
			rsi := NewRSI(14)
			values, err := rsi.Calculate(candles)
			if err != nil {
				// Insufficient data is acceptable
				return true
			}

			for i, v := range values {
				// Skip zero values (before indicator starts)
				if i < rsi.Period() {
					continue
				}
				if v < 0 || v > 100 {
					return false
				}
			}
			return true
		},
		candleSliceGen(20, 100),
	))

	properties.TestingRun(t)
}

func TestProperty_StochasticWithinBounds(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	properties.Property("Stochastic %K and %D values are within [0, 100]", prop.ForAll(
		func(candles []models.Candle) bool {
			stoch := NewStochastic(14, 3, 3)
			values, err := stoch.Calculate(candles)
			if err != nil {
				return true
			}

			percentK := values["percent_k"]
			percentD := values["percent_d"]

			for i := stoch.Period(); i < len(percentK); i++ {
				if percentK[i] < 0 || percentK[i] > 100 {
					return false
				}
			}

			for i := stoch.Period(); i < len(percentD); i++ {
				if percentD[i] < 0 || percentD[i] > 100 {
					return false
				}
			}

			return true
		},
		candleSliceGen(25, 100),
	))

	properties.TestingRun(t)
}


func TestProperty_WilliamsRWithinBounds(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())
	// Disable shrinking to prevent gopter from producing invalid candle data
	// (shrinking can produce zero/negative values that bypass generator constraints)
	parameters.MaxShrinkCount = 0

	properties := gopter.NewProperties(parameters)

	properties.Property("Williams %R values are within [-100, 0]", prop.ForAll(
		func(candles []models.Candle) bool {
			wr := NewWilliamsR(14)
			values, err := wr.Calculate(candles)
			if err != nil {
				return true
			}

			for i := wr.Period() - 1; i < len(values); i++ {
				if values[i] < -100 || values[i] > 0 {
					return false
				}
			}
			return true
		},
		candleSliceGen(20, 100),
	))

	properties.TestingRun(t)
}

func TestProperty_MFIWithinBounds(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	properties.Property("MFI values are within [0, 100]", prop.ForAll(
		func(candles []models.Candle) bool {
			mfi := NewMFI(14)
			values, err := mfi.Calculate(candles)
			if err != nil {
				return true
			}

			for i := mfi.Period(); i < len(values); i++ {
				if values[i] < 0 || values[i] > 100 {
					return false
				}
			}
			return true
		},
		candleSliceGen(20, 100),
	))

	properties.TestingRun(t)
}

func TestProperty_CMFWithinBounds(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	properties.Property("CMF values are within [-1, 1]", prop.ForAll(
		func(candles []models.Candle) bool {
			cmf := NewCMF(20)
			values, err := cmf.Calculate(candles)
			if err != nil {
				return true
			}

			for i := cmf.Period() - 1; i < len(values); i++ {
				if values[i] < -1 || values[i] > 1 {
					return false
				}
			}
			return true
		},
		candleSliceGen(25, 100),
	))

	properties.TestingRun(t)
}

func TestProperty_ADXWithinBounds(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	properties.Property("ADX, +DI, -DI values are within [0, 100]", prop.ForAll(
		func(candles []models.Candle) bool {
			adx := NewADX(14)
			values, err := adx.Calculate(candles)
			if err != nil {
				return true
			}

			adxValues := values["adx"]
			plusDI := values["plus_di"]
			minusDI := values["minus_di"]

			for i := adx.Period(); i < len(adxValues); i++ {
				if adxValues[i] < 0 || adxValues[i] > 100 {
					return false
				}
				if plusDI[i] < 0 || plusDI[i] > 100 {
					return false
				}
				if minusDI[i] < 0 || minusDI[i] > 100 {
					return false
				}
			}
			return true
		},
		candleSliceGen(35, 100),
	))

	properties.TestingRun(t)
}

func TestProperty_BollingerBandsOrdering(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	properties.Property("Bollinger Bands: Lower <= Middle <= Upper", prop.ForAll(
		func(candles []models.Candle) bool {
			bb := NewBollingerBands(20, 2.0)
			values, err := bb.Calculate(candles)
			if err != nil {
				return true
			}

			upper := values["upper"]
			middle := values["middle"]
			lower := values["lower"]

			for i := bb.Period() - 1; i < len(upper); i++ {
				if lower[i] > middle[i] || middle[i] > upper[i] {
					return false
				}
			}
			return true
		},
		candleSliceGen(25, 100),
	))

	properties.TestingRun(t)
}

func TestProperty_SMAIsAverageOfPrices(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	properties.Property("SMA is the arithmetic mean of closing prices over the period", prop.ForAll(
		func(candles []models.Candle) bool {
			period := 10
			sma := NewSMA(period)
			values, err := sma.Calculate(candles)
			if err != nil {
				return true
			}

			closes := closePrices(candles)

			for i := period - 1; i < len(values); i++ {
				expectedMean := mean(closes[i-period+1 : i+1])
				// Allow small floating point tolerance
				if math.Abs(values[i]-expectedMean) > 0.0001 {
					return false
				}
			}
			return true
		},
		candleSliceGen(15, 50),
	))

	properties.TestingRun(t)
}

func TestProperty_UltimateOscillatorWithinBounds(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	properties.Property("Ultimate Oscillator values are within [0, 100]", prop.ForAll(
		func(candles []models.Candle) bool {
			uo := NewUltimateOscillator(7, 14, 28)
			values, err := uo.Calculate(candles)
			if err != nil {
				return true
			}

			for i := uo.Period(); i < len(values); i++ {
				if values[i] < 0 || values[i] > 100 {
					return false
				}
			}
			return true
		},
		candleSliceGen(35, 100),
	))

	properties.TestingRun(t)
}

func TestProperty_ATRIsNonNegative(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	properties.Property("ATR values are non-negative", prop.ForAll(
		func(candles []models.Candle) bool {
			atr := NewATR(14)
			values, err := atr.Calculate(candles)
			if err != nil {
				return true
			}

			for i := atr.Period() - 1; i < len(values); i++ {
				if values[i] < 0 {
					return false
				}
			}
			return true
		},
		candleSliceGen(20, 100),
	))

	properties.TestingRun(t)
}

func TestProperty_FibonacciLevelsOrdering(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	properties.Property("Fibonacci levels are properly ordered between swing high and low", prop.ForAll(
		func(candles []models.Candle) bool {
			fib := NewFibonacciRetracement(20)
			levels, err := fib.Calculate(candles)
			if err != nil {
				return true
			}

			// All retracement levels should be between swing high and low
			minLevel := math.Min(levels.SwingHigh, levels.SwingLow)
			maxLevel := math.Max(levels.SwingHigh, levels.SwingLow)

			// Check main retracement levels (0% to 100%)
			retracementLevels := []float64{
				levels.Level0, levels.Level236, levels.Level382,
				levels.Level500, levels.Level618, levels.Level786, levels.Level1000,
			}

			for _, level := range retracementLevels {
				if level < minLevel || level > maxLevel {
					return false
				}
			}

			return true
		},
		candleSliceGen(25, 100),
	))

	properties.TestingRun(t)
}

func TestProperty_PivotPointsOrdering(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	properties.Property("Pivot points: S3 < S2 < S1 < Pivot < R1 < R2 < R3", prop.ForAll(
		func(candle models.Candle) bool {
			pp := NewStandardPivotPoints()
			levels := pp.CalculateFromCandle(candle)

			// Check ordering
			return levels.S3 < levels.S2 &&
				levels.S2 < levels.S1 &&
				levels.S1 < levels.Pivot &&
				levels.Pivot < levels.R1 &&
				levels.R1 < levels.R2 &&
				levels.R2 < levels.R3
		},
		candleGen(),
	))

	properties.TestingRun(t)
}
