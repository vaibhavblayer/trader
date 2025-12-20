// Package scoring provides signal scoring and stock screening functionality.
package scoring

import (
	"context"
	"fmt"

	"zerodha-trader/internal/analysis"
	"zerodha-trader/internal/analysis/indicators"
	"zerodha-trader/internal/models"
)

// SignalScorer combines multiple technical indicators into a composite score.
type SignalScorer struct {
	engine  *indicators.Engine
	weights IndicatorWeights
}

// IndicatorWeights defines the weights for each indicator in the composite score.
type IndicatorWeights struct {
	RSI        float64
	MACD       float64
	Stochastic float64
	SuperTrend float64
	ADX        float64
	EMA        float64
	Volume     float64
}

// DefaultWeights returns the default indicator weights.
func DefaultWeights() IndicatorWeights {
	return IndicatorWeights{
		RSI:        0.20,
		MACD:       0.20,
		Stochastic: 0.15,
		SuperTrend: 0.15,
		ADX:        0.10,
		EMA:        0.10,
		Volume:     0.10,
	}
}

// NewSignalScorer creates a new signal scorer with the given indicator engine.
func NewSignalScorer(engine *indicators.Engine) *SignalScorer {
	return &SignalScorer{
		engine:  engine,
		weights: DefaultWeights(),
	}
}

// NewSignalScorerWithWeights creates a new signal scorer with custom weights.
func NewSignalScorerWithWeights(engine *indicators.Engine, weights IndicatorWeights) *SignalScorer {
	return &SignalScorer{
		engine:  engine,
		weights: weights,
	}
}

// Score calculates a composite signal score for the given candles.
// Returns a score from -100 (strong sell) to +100 (strong buy).
func (s *SignalScorer) Score(ctx context.Context, candles []models.Candle) (*analysis.SignalScore, error) {
	if len(candles) < 50 {
		return nil, fmt.Errorf("insufficient data: need at least 50 candles, got %d", len(candles))
	}

	components := make(map[string]float64)
	var totalScore float64
	var totalWeight float64

	// Calculate RSI score
	rsiScore, err := s.calculateRSIScore(candles)
	if err == nil {
		components["RSI"] = rsiScore
		totalScore += rsiScore * s.weights.RSI
		totalWeight += s.weights.RSI
	}

	// Calculate MACD score
	macdScore, err := s.calculateMACDScore(candles)
	if err == nil {
		components["MACD"] = macdScore
		totalScore += macdScore * s.weights.MACD
		totalWeight += s.weights.MACD
	}

	// Calculate Stochastic score
	stochScore, err := s.calculateStochasticScore(candles)
	if err == nil {
		components["Stochastic"] = stochScore
		totalScore += stochScore * s.weights.Stochastic
		totalWeight += s.weights.Stochastic
	}

	// Calculate SuperTrend score
	superTrendScore, err := s.calculateSuperTrendScore(candles)
	if err == nil {
		components["SuperTrend"] = superTrendScore
		totalScore += superTrendScore * s.weights.SuperTrend
		totalWeight += s.weights.SuperTrend
	}

	// Calculate ADX score
	adxScore, err := s.calculateADXScore(candles)
	if err == nil {
		components["ADX"] = adxScore
		totalScore += adxScore * s.weights.ADX
		totalWeight += s.weights.ADX
	}

	// Calculate EMA crossover score
	emaScore, err := s.calculateEMAScore(candles)
	if err == nil {
		components["EMA"] = emaScore
		totalScore += emaScore * s.weights.EMA
		totalWeight += s.weights.EMA
	}

	// Calculate volume confirmation
	volumeConfirm, volumeScore := s.calculateVolumeConfirmation(candles)
	components["Volume"] = volumeScore
	totalScore += volumeScore * s.weights.Volume
	totalWeight += s.weights.Volume

	// Normalize score if not all indicators were calculated
	var finalScore float64
	if totalWeight > 0 {
		finalScore = totalScore / totalWeight
	}

	// Clamp score to [-100, +100]
	finalScore = clamp(finalScore, -100, 100)

	return &analysis.SignalScore{
		Score:          finalScore,
		Recommendation: scoreToRecommendation(finalScore),
		Components:     components,
		VolumeConfirm:  volumeConfirm,
	}, nil
}


// calculateRSIScore calculates the RSI component score.
// RSI < 30: Oversold (bullish), RSI > 70: Overbought (bearish)
func (s *SignalScorer) calculateRSIScore(candles []models.Candle) (float64, error) {
	rsi := indicators.NewRSI(14)
	values, err := rsi.Calculate(candles)
	if err != nil {
		return 0, err
	}

	lastRSI := values[len(values)-1]
	if lastRSI == 0 {
		// Find last non-zero value
		for i := len(values) - 1; i >= 0; i-- {
			if values[i] != 0 {
				lastRSI = values[i]
				break
			}
		}
	}

	// Convert RSI to score:
	// RSI 0-30: +100 to +33 (oversold = bullish)
	// RSI 30-50: +33 to 0 (neutral-bullish)
	// RSI 50-70: 0 to -33 (neutral-bearish)
	// RSI 70-100: -33 to -100 (overbought = bearish)
	var score float64
	if lastRSI <= 30 {
		score = 100 - (lastRSI/30)*67
	} else if lastRSI <= 50 {
		score = 33 - ((lastRSI-30)/20)*33
	} else if lastRSI <= 70 {
		score = -((lastRSI - 50) / 20) * 33
	} else {
		score = -33 - ((lastRSI-70)/30)*67
	}

	return score, nil
}

// calculateMACDScore calculates the MACD component score.
func (s *SignalScorer) calculateMACDScore(candles []models.Candle) (float64, error) {
	macd := indicators.NewMACD(12, 26, 9)
	values, err := macd.Calculate(candles)
	if err != nil {
		return 0, err
	}

	macdLine := values["macd"]
	signalLine := values["signal"]
	histogram := values["histogram"]

	n := len(candles)
	if n < 2 {
		return 0, fmt.Errorf("insufficient data for MACD score")
	}

	// Get current and previous values
	currMACD := macdLine[n-1]
	currSignal := signalLine[n-1]
	currHist := histogram[n-1]
	prevHist := histogram[n-2]

	var score float64

	// MACD above signal line = bullish, below = bearish
	if currMACD > currSignal {
		score = 50
	} else {
		score = -50
	}

	// Histogram momentum
	if currHist > prevHist {
		score += 25 // Increasing momentum
	} else {
		score -= 25 // Decreasing momentum
	}

	// Histogram sign
	if currHist > 0 {
		score += 25
	} else {
		score -= 25
	}

	return clamp(score, -100, 100), nil
}

// calculateStochasticScore calculates the Stochastic component score.
func (s *SignalScorer) calculateStochasticScore(candles []models.Candle) (float64, error) {
	stoch := indicators.NewStochastic(14, 3, 3)
	values, err := stoch.Calculate(candles)
	if err != nil {
		return 0, err
	}

	percentK := values["percent_k"]
	percentD := values["percent_d"]

	n := len(candles)
	currK := percentK[n-1]
	currD := percentD[n-1]

	// Find last non-zero values
	for i := n - 1; i >= 0 && currK == 0; i-- {
		currK = percentK[i]
	}
	for i := n - 1; i >= 0 && currD == 0; i-- {
		currD = percentD[i]
	}

	var score float64

	// Oversold/Overbought zones
	if currK < 20 {
		score = 50 + (20-currK)*2.5 // Oversold = bullish
	} else if currK > 80 {
		score = -50 - (currK-80)*2.5 // Overbought = bearish
	} else {
		// Neutral zone: scale from -50 to +50
		score = 50 - (currK-20)*1.67
	}

	// %K/%D crossover
	if currK > currD {
		score += 25 // Bullish crossover
	} else {
		score -= 25 // Bearish crossover
	}

	return clamp(score, -100, 100), nil
}

// calculateSuperTrendScore calculates the SuperTrend component score.
func (s *SignalScorer) calculateSuperTrendScore(candles []models.Candle) (float64, error) {
	st := indicators.NewSuperTrend(10, 3.0)
	values, err := st.Calculate(candles)
	if err != nil {
		return 0, err
	}

	direction := values["direction"]
	n := len(candles)

	// Count recent trend direction
	bullishCount := 0
	bearishCount := 0
	lookback := min(10, n)

	for i := n - lookback; i < n; i++ {
		if direction[i] > 0 {
			bullishCount++
		} else if direction[i] < 0 {
			bearishCount++
		}
	}

	// Current direction has more weight
	var score float64
	if direction[n-1] > 0 {
		score = 50 + float64(bullishCount)*5
	} else {
		score = -50 - float64(bearishCount)*5
	}

	return clamp(score, -100, 100), nil
}


// calculateADXScore calculates the ADX component score.
func (s *SignalScorer) calculateADXScore(candles []models.Candle) (float64, error) {
	adx := indicators.NewADX(14)
	values, err := adx.Calculate(candles)
	if err != nil {
		return 0, err
	}

	adxLine := values["adx"]
	plusDI := values["plus_di"]
	minusDI := values["minus_di"]

	n := len(candles)
	currADX := adxLine[n-1]
	currPlusDI := plusDI[n-1]
	currMinusDI := minusDI[n-1]

	// Find last non-zero values
	for i := n - 1; i >= 0 && currADX == 0; i-- {
		currADX = adxLine[i]
		currPlusDI = plusDI[i]
		currMinusDI = minusDI[i]
	}

	var score float64

	// ADX strength factor (0-100)
	// ADX > 25 indicates strong trend
	strengthFactor := currADX / 50 // Normalize to 0-2 range
	if strengthFactor > 1 {
		strengthFactor = 1
	}

	// Direction based on +DI vs -DI
	if currPlusDI > currMinusDI {
		score = 100 * strengthFactor // Bullish trend
	} else {
		score = -100 * strengthFactor // Bearish trend
	}

	// Weak trend (ADX < 20) reduces score magnitude
	if currADX < 20 {
		score *= currADX / 20
	}

	return clamp(score, -100, 100), nil
}

// calculateEMAScore calculates the EMA crossover component score.
func (s *SignalScorer) calculateEMAScore(candles []models.Candle) (float64, error) {
	ema9 := indicators.NewEMA(9)
	ema21 := indicators.NewEMA(21)
	ema50 := indicators.NewEMA(50)

	values9, err := ema9.Calculate(candles)
	if err != nil {
		return 0, err
	}
	values21, err := ema21.Calculate(candles)
	if err != nil {
		return 0, err
	}
	values50, err := ema50.Calculate(candles)
	if err != nil {
		return 0, err
	}

	n := len(candles)
	curr9 := values9[n-1]
	curr21 := values21[n-1]
	curr50 := values50[n-1]
	currPrice := candles[n-1].Close

	var score float64

	// Price above/below EMAs
	if currPrice > curr9 {
		score += 25
	} else {
		score -= 25
	}

	if currPrice > curr21 {
		score += 25
	} else {
		score -= 25
	}

	if currPrice > curr50 {
		score += 25
	} else {
		score -= 25
	}

	// EMA alignment (9 > 21 > 50 = bullish, 9 < 21 < 50 = bearish)
	if curr9 > curr21 && curr21 > curr50 {
		score += 25 // Perfect bullish alignment
	} else if curr9 < curr21 && curr21 < curr50 {
		score -= 25 // Perfect bearish alignment
	}

	return clamp(score, -100, 100), nil
}

// calculateVolumeConfirmation checks if volume confirms the price action.
func (s *SignalScorer) calculateVolumeConfirmation(candles []models.Candle) (bool, float64) {
	n := len(candles)
	if n < 20 {
		return false, 0
	}

	// Calculate average volume over last 20 periods
	var avgVolume float64
	for i := n - 20; i < n-1; i++ {
		avgVolume += float64(candles[i].Volume)
	}
	avgVolume /= 19

	currentVolume := float64(candles[n-1].Volume)
	priceChange := candles[n-1].Close - candles[n-2].Close

	// Volume ratio
	volumeRatio := currentVolume / avgVolume
	if avgVolume == 0 {
		return false, 0
	}

	// Volume confirms if:
	// - Price up with above-average volume = bullish confirmation
	// - Price down with above-average volume = bearish confirmation
	// - Price move with below-average volume = weak signal

	var score float64
	confirmed := false

	if volumeRatio > 1.5 {
		// Strong volume
		if priceChange > 0 {
			score = 100 * (volumeRatio - 1) / 2 // Cap at 100
			confirmed = true
		} else if priceChange < 0 {
			score = -100 * (volumeRatio - 1) / 2
			confirmed = true
		}
	} else if volumeRatio > 1.0 {
		// Above average volume
		if priceChange > 0 {
			score = 50 * (volumeRatio - 1)
			confirmed = true
		} else if priceChange < 0 {
			score = -50 * (volumeRatio - 1)
			confirmed = true
		}
	} else {
		// Below average volume - weak signal
		if priceChange > 0 {
			score = 25 * volumeRatio
		} else if priceChange < 0 {
			score = -25 * volumeRatio
		}
	}

	return confirmed, clamp(score, -100, 100)
}

// scoreToRecommendation converts a numeric score to a recommendation.
func scoreToRecommendation(score float64) analysis.SignalRecommendation {
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

// clamp restricts a value to the given range.
func clamp(value, minVal, maxVal float64) float64 {
	if value < minVal {
		return minVal
	}
	if value > maxVal {
		return maxVal
	}
	return value
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
