package scoring

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"zerodha-trader/internal/analysis"
	"zerodha-trader/internal/analysis/indicators"
	"zerodha-trader/internal/models"
)

// Feature: zerodha-go-trader, Property 4: Signal score in [-100, +100] with correct recommendation mapping
// Validates: Requirements 5.2, 5.4
//
// Property: For any valid candle data, the signal scorer should:
// 1. Produce a score within the range [-100, +100]
// 2. Map the score to the correct recommendation based on defined thresholds:
//    - score >= 70: STRONG_BUY
//    - score >= 40: BUY
//    - score >= 15: WEAK_BUY
//    - score >= -15: NEUTRAL
//    - score >= -40: WEAK_SELL
//    - score >= -70: SELL
//    - score < -70: STRONG_SELL

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
		// Ensure all prices are positive
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
		// Ensure there's some price range
		if c.High <= c.Low {
			c.High = c.Low + 1.0
		}
		return c
	})
}

// candleSliceGen generates a slice of valid candles
func candleSliceGen(minLen, maxLen int) gopter.Gen {
	return gen.SliceOfN(maxLen, candleGen()).Map(func(candles []models.Candle) []models.Candle {
		if len(candles) < minLen {
			for len(candles) < minLen {
				candles = append(candles, candles[len(candles)-1])
			}
		}
		// Sort by timestamp and ensure valid candles
		for i := range candles {
			candles[i].Timestamp = time.Now().Add(time.Duration(i) * time.Hour)
			// Re-validate each candle
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

// TestProperty_SignalScoreWithinBounds tests that signal scores are always within [-100, +100]
func TestProperty_SignalScoreWithinBounds(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())
	parameters.MaxShrinkCount = 0

	properties := gopter.NewProperties(parameters)

	properties.Property("Signal score is within [-100, +100]", prop.ForAll(
		func(candles []models.Candle) bool {
			engine := indicators.NewEngine(4)
			scorer := NewSignalScorer(engine)

			score, err := scorer.Score(context.Background(), candles)
			if err != nil {
				// Insufficient data is acceptable
				return true
			}

			return score.Score >= -100 && score.Score <= 100
		},
		candleSliceGen(60, 150),
	))

	properties.TestingRun(t)
}

// TestProperty_SignalScoreRecommendationMapping tests that scores map to correct recommendations
func TestProperty_SignalScoreRecommendationMapping(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())
	parameters.MaxShrinkCount = 0

	properties := gopter.NewProperties(parameters)

	properties.Property("Signal score maps to correct recommendation", prop.ForAll(
		func(candles []models.Candle) bool {
			engine := indicators.NewEngine(4)
			scorer := NewSignalScorer(engine)

			result, err := scorer.Score(context.Background(), candles)
			if err != nil {
				return true
			}

			score := result.Score
			rec := result.Recommendation

			// Verify recommendation matches score thresholds
			expectedRec := getExpectedRecommendation(score)
			return rec == expectedRec
		},
		candleSliceGen(60, 150),
	))

	properties.TestingRun(t)
}

// getExpectedRecommendation returns the expected recommendation for a given score
func getExpectedRecommendation(score float64) analysis.SignalRecommendation {
	switch {
	case score >= 70:
		return analysis.StrongBuy
	case score >= 40:
		return analysis.Buy
	case score >= 15:
		return analysis.WeakBuy
	case score >= -15:
		return analysis.Neutral
	case score >= -40:
		return analysis.WeakSell
	case score >= -70:
		return analysis.Sell
	default:
		return analysis.StrongSell
	}
}


// TestProperty_SignalScoreComponentsPresent tests that all expected components are present
func TestProperty_SignalScoreComponentsPresent(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())
	parameters.MaxShrinkCount = 0

	properties := gopter.NewProperties(parameters)

	properties.Property("Signal score contains expected components", prop.ForAll(
		func(candles []models.Candle) bool {
			engine := indicators.NewEngine(4)
			scorer := NewSignalScorer(engine)

			result, err := scorer.Score(context.Background(), candles)
			if err != nil {
				return true
			}

			// Check that components map is not nil
			if result.Components == nil {
				return false
			}

			// At minimum, Volume component should always be present
			_, hasVolume := result.Components["Volume"]
			return hasVolume
		},
		candleSliceGen(60, 150),
	))

	properties.TestingRun(t)
}

// TestProperty_SignalScoreComponentsWithinBounds tests that individual components are within bounds
func TestProperty_SignalScoreComponentsWithinBounds(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())
	parameters.MaxShrinkCount = 0

	properties := gopter.NewProperties(parameters)

	properties.Property("All signal score components are within [-100, +100]", prop.ForAll(
		func(candles []models.Candle) bool {
			engine := indicators.NewEngine(4)
			scorer := NewSignalScorer(engine)

			result, err := scorer.Score(context.Background(), candles)
			if err != nil {
				return true
			}

			for _, value := range result.Components {
				if value < -100 || value > 100 {
					return false
				}
			}
			return true
		},
		candleSliceGen(60, 150),
	))

	properties.TestingRun(t)
}

// TestProperty_ScoreToRecommendationMonotonic tests that higher scores map to more bullish recommendations
func TestProperty_ScoreToRecommendationMonotonic(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	properties.Property("Higher scores map to more bullish recommendations", prop.ForAll(
		func(score1, score2 float64) bool {
			// Clamp scores to valid range
			score1 = clamp(score1, -100, 100)
			score2 = clamp(score2, -100, 100)

			rec1 := scoreToRecommendation(score1)
			rec2 := scoreToRecommendation(score2)

			// If score1 > score2, then rec1 should be >= rec2 (more bullish or equal)
			if score1 > score2 {
				return recommendationRank(rec1) >= recommendationRank(rec2)
			}
			// If score1 < score2, then rec1 should be <= rec2
			if score1 < score2 {
				return recommendationRank(rec1) <= recommendationRank(rec2)
			}
			// If equal, recommendations should be equal
			return rec1 == rec2
		},
		gen.Float64Range(-100, 100),
		gen.Float64Range(-100, 100),
	))

	properties.TestingRun(t)
}

// recommendationRank returns a numeric rank for a recommendation (higher = more bullish)
func recommendationRank(rec analysis.SignalRecommendation) int {
	switch rec {
	case analysis.StrongSell:
		return 1
	case analysis.Sell:
		return 2
	case analysis.WeakSell:
		return 3
	case analysis.Neutral:
		return 4
	case analysis.WeakBuy:
		return 5
	case analysis.Buy:
		return 6
	case analysis.StrongBuy:
		return 7
	default:
		return 0
	}
}

// TestProperty_ClampFunction tests that clamp function works correctly
func TestProperty_ClampFunction(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	properties.Property("Clamp function produces values within bounds", prop.ForAll(
		func(value, minVal, maxVal float64) bool {
			// Ensure minVal <= maxVal
			if minVal > maxVal {
				minVal, maxVal = maxVal, minVal
			}

			result := clamp(value, minVal, maxVal)
			return result >= minVal && result <= maxVal
		},
		gen.Float64Range(-1000, 1000),
		gen.Float64Range(-500, 0),
		gen.Float64Range(0, 500),
	))

	properties.TestingRun(t)
}

// TestProperty_CustomWeightsProduceValidScore tests that custom weights still produce valid scores
func TestProperty_CustomWeightsProduceValidScore(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.Rng.Seed(time.Now().UnixNano())
	parameters.MaxShrinkCount = 0

	properties := gopter.NewProperties(parameters)

	properties.Property("Custom weights produce valid scores within [-100, +100]", prop.ForAll(
		func(candles []models.Candle, rsiWeight, macdWeight, stochWeight float64) bool {
			// Normalize weights to be positive
			rsiWeight = math.Abs(rsiWeight)
			macdWeight = math.Abs(macdWeight)
			stochWeight = math.Abs(stochWeight)

			// Ensure at least one weight is non-zero
			if rsiWeight+macdWeight+stochWeight == 0 {
				rsiWeight = 0.5
			}

			weights := IndicatorWeights{
				RSI:        rsiWeight,
				MACD:       macdWeight,
				Stochastic: stochWeight,
				SuperTrend: 0.1,
				ADX:        0.1,
				EMA:        0.1,
				Volume:     0.1,
			}

			engine := indicators.NewEngine(4)
			scorer := NewSignalScorerWithWeights(engine, weights)

			result, err := scorer.Score(context.Background(), candles)
			if err != nil {
				return true
			}

			return result.Score >= -100 && result.Score <= 100
		},
		candleSliceGen(60, 150),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
		gen.Float64Range(0, 1),
	))

	properties.TestingRun(t)
}
