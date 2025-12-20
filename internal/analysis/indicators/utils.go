package indicators

import (
	"errors"
	"math"

	"zerodha-trader/internal/models"
)

var (
	// ErrInsufficientData is returned when there's not enough data for calculation.
	ErrInsufficientData = errors.New("insufficient data for calculation")
	// ErrInvalidPeriod is returned when the period is invalid.
	ErrInvalidPeriod = errors.New("invalid period")
)

// max returns the maximum of two float64 values.
func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// min returns the minimum of two float64 values.
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// abs returns the absolute value of a float64.
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// sum calculates the sum of a slice of float64.
func sum(values []float64) float64 {
	var total float64
	for _, v := range values {
		total += v
	}
	return total
}

// mean calculates the arithmetic mean of a slice of float64.
func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	return sum(values) / float64(len(values))
}

// stdDev calculates the standard deviation of a slice of float64.
func stdDev(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	m := mean(values)
	var variance float64
	for _, v := range values {
		diff := v - m
		variance += diff * diff
	}
	variance /= float64(len(values))
	return math.Sqrt(variance)
}

// trueRange calculates the true range for a candle.
func trueRange(current, previous models.Candle) float64 {
	highLow := current.High - current.Low
	highClose := abs(current.High - previous.Close)
	lowClose := abs(current.Low - previous.Close)
	return max(highLow, max(highClose, lowClose))
}

// typicalPrice calculates the typical price (HLC/3) for a candle.
func typicalPrice(c models.Candle) float64 {
	return (c.High + c.Low + c.Close) / 3
}

// closePrices extracts close prices from candles.
func closePrices(candles []models.Candle) []float64 {
	prices := make([]float64, len(candles))
	for i, c := range candles {
		prices[i] = c.Close
	}
	return prices
}

// highPrices extracts high prices from candles.
func highPrices(candles []models.Candle) []float64 {
	prices := make([]float64, len(candles))
	for i, c := range candles {
		prices[i] = c.High
	}
	return prices
}

// lowPrices extracts low prices from candles.
func lowPrices(candles []models.Candle) []float64 {
	prices := make([]float64, len(candles))
	for i, c := range candles {
		prices[i] = c.Low
	}
	return prices
}

// volumes extracts volumes from candles.
func volumes(candles []models.Candle) []int64 {
	vols := make([]int64, len(candles))
	for i, c := range candles {
		vols[i] = c.Volume
	}
	return vols
}

// highest returns the highest value in a slice.
func highest(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	h := values[0]
	for _, v := range values[1:] {
		if v > h {
			h = v
		}
	}
	return h
}

// lowest returns the lowest value in a slice.
func lowest(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	l := values[0]
	for _, v := range values[1:] {
		if v < l {
			l = v
		}
	}
	return l
}

// highestIndex returns the index of the highest value in a slice.
func highestIndex(values []float64) int {
	if len(values) == 0 {
		return -1
	}
	idx := 0
	h := values[0]
	for i, v := range values[1:] {
		if v > h {
			h = v
			idx = i + 1
		}
	}
	return idx
}

// lowestIndex returns the index of the lowest value in a slice.
func lowestIndex(values []float64) int {
	if len(values) == 0 {
		return -1
	}
	idx := 0
	l := values[0]
	for i, v := range values[1:] {
		if v < l {
			l = v
			idx = i + 1
		}
	}
	return idx
}

// wilder smoothing (used in RSI, ADX, etc.)
func wilderSmooth(values []float64, period int) []float64 {
	if len(values) < period {
		return nil
	}
	result := make([]float64, len(values))
	
	// First value is SMA
	result[period-1] = mean(values[:period])
	
	// Subsequent values use Wilder smoothing
	multiplier := 1.0 / float64(period)
	for i := period; i < len(values); i++ {
		result[i] = result[i-1] + multiplier*(values[i]-result[i-1])
	}
	
	return result
}
