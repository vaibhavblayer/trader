package store

import (
	"context"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"zerodha-trader/internal/models"
)

// Feature: zerodha-go-trader, Property 1: Candle round-trip consistency
// Validates: Requirements 3.2
//
// Property: For any valid candle data, saving candles to the database and then
// retrieving them should produce equivalent candle data (round-trip consistency).
func TestProperty_CandleRoundTripConsistency(t *testing.T) {
	// Create a temporary database for testing
	dbPath := "test_candles_property.db"
	defer os.Remove(dbPath)

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	// Valid symbols for testing
	symbols := []string{"RELIANCE", "TCS", "INFY", "HDFC", "ICICI", "SBIN", "BHARTI", "ITC", "KOTAKBANK", "LT"}

	// Generator for valid timeframes
	timeframeGen := gen.OneConstOf("1min", "5min", "15min", "30min", "1hour", "1day")

	// Generator for candle count (1-20 candles)
	countGen := gen.IntRange(1, 20)

	// Generator for valid OHLCV values
	priceGen := gen.Float64Range(100.0, 5000.0)
	volumeGen := gen.Int64Range(1000, 1000000)

	properties.Property("Candle round-trip: save then retrieve produces equivalent data", prop.ForAll(
		func(symbolIdx int, timeframe string, count int, basePrice float64, baseVolume int64) bool {
			ctx := context.Background()
			symbol := symbols[symbolIdx%len(symbols)]

			// Generate unique symbol+timeframe combo to avoid conflicts between test runs
			uniqueSymbol := fmt.Sprintf("%s_%d", symbol, time.Now().UnixNano()%10000)

			// Generate candles with valid OHLC relationships
			candles := generateTestCandles(count, basePrice, baseVolume)

			// Save candles
			err := store.SaveCandles(ctx, uniqueSymbol, timeframe, candles)
			if err != nil {
				t.Logf("Failed to save candles: %v", err)
				return false
			}

			// Retrieve candles
			from := candles[0].Timestamp.Add(-time.Second)
			to := candles[len(candles)-1].Timestamp.Add(time.Second)
			retrieved, err := store.GetCandles(ctx, uniqueSymbol, timeframe, from, to)
			if err != nil {
				t.Logf("Failed to get candles: %v", err)
				return false
			}

			// Verify count matches
			if len(retrieved) != len(candles) {
				t.Logf("Count mismatch: expected %d, got %d", len(candles), len(retrieved))
				return false
			}

			// Verify each candle matches (within floating point tolerance)
			for i, orig := range candles {
				ret := retrieved[i]
				if !candlesEqual(orig, ret) {
					t.Logf("Candle mismatch at index %d: original=%+v, retrieved=%+v", i, orig, ret)
					return false
				}
			}

			return true
		},
		gen.IntRange(0, len(symbols)-1),
		timeframeGen,
		countGen,
		priceGen,
		volumeGen,
	))

	// Additional property: Empty candles should not cause errors
	properties.Property("Empty candles: saving empty slice should succeed", prop.ForAll(
		func(symbolIdx int, timeframe string) bool {
			ctx := context.Background()
			symbol := symbols[symbolIdx%len(symbols)]
			uniqueSymbol := fmt.Sprintf("%s_empty_%d", symbol, time.Now().UnixNano()%10000)

			err := store.SaveCandles(ctx, uniqueSymbol, timeframe, []models.Candle{})
			return err == nil
		},
		gen.IntRange(0, len(symbols)-1),
		timeframeGen,
	))

	properties.TestingRun(t)
}

// generateTestCandles creates valid candles for testing
func generateTestCandles(count int, basePrice float64, baseVolume int64) []models.Candle {
	candles := make([]models.Candle, count)
	baseTime := time.Date(2024, 1, 1, 9, 15, 0, 0, time.UTC)

	for i := 0; i < count; i++ {
		// Generate OHLC with valid relationships
		variation := float64(i%10) * 0.01 * basePrice
		open := basePrice + variation
		close := basePrice + variation*0.5

		// Ensure high >= max(open, close) and low <= min(open, close)
		high := math.Max(open, close) * 1.01
		low := math.Min(open, close) * 0.99

		candles[i] = models.Candle{
			Timestamp: baseTime.Add(time.Duration(i) * time.Minute),
			Open:      roundToDecimal(open, 2),
			High:      roundToDecimal(high, 2),
			Low:       roundToDecimal(low, 2),
			Close:     roundToDecimal(close, 2),
			Volume:    baseVolume + int64(i*1000),
		}
	}

	return candles
}

// roundToDecimal rounds a float to specified decimal places
func roundToDecimal(val float64, places int) float64 {
	multiplier := math.Pow(10, float64(places))
	return math.Round(val*multiplier) / multiplier
}

// candlesEqual compares two candles for equality with floating point tolerance.
func candlesEqual(a, b models.Candle) bool {
	const tolerance = 0.01

	if !a.Timestamp.Equal(b.Timestamp) {
		return false
	}
	if !floatEqual(a.Open, b.Open, tolerance) {
		return false
	}
	if !floatEqual(a.High, b.High, tolerance) {
		return false
	}
	if !floatEqual(a.Low, b.Low, tolerance) {
		return false
	}
	if !floatEqual(a.Close, b.Close, tolerance) {
		return false
	}
	if a.Volume != b.Volume {
		return false
	}
	return true
}

// floatEqual compares two floats with a tolerance.
func floatEqual(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}
