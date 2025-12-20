// Package analysis provides technical analysis functionality including indicators,
// pattern detection, and signal scoring.
package analysis

import (
	"zerodha-trader/internal/models"
)

// Indicator defines the interface for technical indicators.
type Indicator interface {
	Name() string
	Calculate(candles []models.Candle) ([]float64, error)
	Period() int
}

// MultiValueIndicator defines the interface for indicators that return multiple values.
type MultiValueIndicator interface {
	Name() string
	Calculate(candles []models.Candle) (map[string][]float64, error)
	Period() int
}

// PatternDetector defines the interface for pattern detection.
type PatternDetector interface {
	Name() string
	Detect(candles []models.Candle) ([]Pattern, error)
}

// Pattern represents a detected chart or candlestick pattern.
type Pattern struct {
	Name           string
	Type           PatternType
	Direction      PatternDirection
	StartIndex     int
	EndIndex       int
	Strength       float64
	TargetPrice    float64
	Completion     float64
	VolumeConfirm  bool
}

// PatternType represents the type of pattern.
type PatternType string

const (
	PatternTypeCandlestick PatternType = "candlestick"
	PatternTypeChart       PatternType = "chart"
)

// PatternDirection represents the expected direction of a pattern.
type PatternDirection string

const (
	PatternBullish PatternDirection = "bullish"
	PatternBearish PatternDirection = "bearish"
	PatternNeutral PatternDirection = "neutral"
)

// SignalScore represents a composite signal score.
type SignalScore struct {
	Score          float64
	Recommendation SignalRecommendation
	Components     map[string]float64
	VolumeConfirm  bool
}

// SignalRecommendation represents the signal recommendation.
type SignalRecommendation string

const (
	StrongBuy  SignalRecommendation = "STRONG_BUY"
	Buy        SignalRecommendation = "BUY"
	WeakBuy    SignalRecommendation = "WEAK_BUY"
	Neutral    SignalRecommendation = "NEUTRAL"
	WeakSell   SignalRecommendation = "WEAK_SELL"
	Sell       SignalRecommendation = "SELL"
	StrongSell SignalRecommendation = "STRONG_SELL"
)

// Level represents a support or resistance level.
type Level struct {
	Price      float64
	Type       LevelType
	Strength   int
	TouchCount int
	Source     string
}

// LevelType represents the type of price level.
type LevelType string

const (
	LevelSupport    LevelType = "support"
	LevelResistance LevelType = "resistance"
)
