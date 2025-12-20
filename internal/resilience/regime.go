package resilience

import (
	"fmt"
	"sync"
	"time"

	"zerodha-trader/internal/models"
)

// MarketRegime represents the current market regime.
type MarketRegime string

const (
	RegimeTrendingUp    MarketRegime = "TRENDING_UP"
	RegimeTrendingDown  MarketRegime = "TRENDING_DOWN"
	RegimeRanging       MarketRegime = "RANGING"
	RegimeHighVolatility MarketRegime = "HIGH_VOLATILITY"
	RegimeUnknown       MarketRegime = "UNKNOWN"
)

// VIXLevel represents VIX-based volatility levels.
type VIXLevel string

const (
	VIXLow      VIXLevel = "LOW"       // VIX < 15
	VIXNormal   VIXLevel = "NORMAL"    // 15 <= VIX < 20
	VIXElevated VIXLevel = "ELEVATED"  // 20 <= VIX < 25
	VIXHigh     VIXLevel = "HIGH"      // 25 <= VIX < 30
	VIXExtreme  VIXLevel = "EXTREME"   // VIX >= 30
)

// RegimeConfig holds configuration for regime detection.
type RegimeConfig struct {
	// VIX thresholds
	VIXLowThreshold      float64
	VIXNormalThreshold   float64
	VIXElevatedThreshold float64
	VIXHighThreshold     float64

	// Trend detection
	TrendStrengthThreshold float64 // ADX threshold for trending
	RangingATRMultiplier   float64 // ATR multiplier for ranging detection

	// Confidence adjustments by regime
	TrendingUpAdjustment    float64
	TrendingDownAdjustment  float64
	RangingAdjustment       float64
	HighVolatilityAdjustment float64
}

// DefaultRegimeConfig returns default regime detection configuration.
func DefaultRegimeConfig() RegimeConfig {
	return RegimeConfig{
		VIXLowThreshold:          15.0,
		VIXNormalThreshold:       20.0,
		VIXElevatedThreshold:     25.0,
		VIXHighThreshold:         30.0,
		TrendStrengthThreshold:   25.0,
		RangingATRMultiplier:     1.5,
		TrendingUpAdjustment:     1.1,  // 10% boost
		TrendingDownAdjustment:   0.9,  // 10% reduction
		RangingAdjustment:        0.85, // 15% reduction
		HighVolatilityAdjustment: 0.7,  // 30% reduction
	}
}

// MarketRegimeDetector detects and tracks market regime.
type MarketRegimeDetector struct {
	mu     sync.RWMutex
	config RegimeConfig

	// Current state
	currentRegime MarketRegime
	currentVIX    float64
	vixLevel      VIXLevel
	lastUpdate    time.Time

	// Historical data for detection
	recentCandles []models.Candle
	adxValue      float64
	atrValue      float64
	trendDirection int // 1 = up, -1 = down, 0 = neutral
}

// NewMarketRegimeDetector creates a new regime detector.
func NewMarketRegimeDetector(config RegimeConfig) *MarketRegimeDetector {
	return &MarketRegimeDetector{
		config:        config,
		currentRegime: RegimeUnknown,
		vixLevel:      VIXNormal,
	}
}

// UpdateVIX updates the current VIX level.
func (d *MarketRegimeDetector) UpdateVIX(vix float64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.currentVIX = vix
	d.vixLevel = d.classifyVIX(vix)
	d.lastUpdate = time.Now()
	d.detectRegime()
}

// UpdateIndicators updates ADX and ATR values for regime detection.
func (d *MarketRegimeDetector) UpdateIndicators(adx, atr float64, trendDirection int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.adxValue = adx
	d.atrValue = atr
	d.trendDirection = trendDirection
	d.lastUpdate = time.Now()
	d.detectRegime()
}

// UpdateCandles updates recent candles for analysis.
func (d *MarketRegimeDetector) UpdateCandles(candles []models.Candle) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.recentCandles = candles
	d.lastUpdate = time.Now()
}

func (d *MarketRegimeDetector) classifyVIX(vix float64) VIXLevel {
	switch {
	case vix < d.config.VIXLowThreshold:
		return VIXLow
	case vix < d.config.VIXNormalThreshold:
		return VIXNormal
	case vix < d.config.VIXElevatedThreshold:
		return VIXElevated
	case vix < d.config.VIXHighThreshold:
		return VIXHigh
	default:
		return VIXExtreme
	}
}

func (d *MarketRegimeDetector) detectRegime() {
	// High volatility takes precedence
	if d.vixLevel == VIXHigh || d.vixLevel == VIXExtreme {
		d.currentRegime = RegimeHighVolatility
		return
	}

	// Check for trending market using ADX
	if d.adxValue >= d.config.TrendStrengthThreshold {
		if d.trendDirection > 0 {
			d.currentRegime = RegimeTrendingUp
		} else if d.trendDirection < 0 {
			d.currentRegime = RegimeTrendingDown
		} else {
			d.currentRegime = RegimeRanging
		}
		return
	}

	// Default to ranging
	d.currentRegime = RegimeRanging
}

// GetRegime returns the current market regime.
func (d *MarketRegimeDetector) GetRegime() MarketRegime {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.currentRegime
}

// GetVIXLevel returns the current VIX level classification.
func (d *MarketRegimeDetector) GetVIXLevel() VIXLevel {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.vixLevel
}

// GetCurrentVIX returns the current VIX value.
func (d *MarketRegimeDetector) GetCurrentVIX() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.currentVIX
}

// AdjustConfidence adjusts a confidence score based on current regime.
func (d *MarketRegimeDetector) AdjustConfidence(confidence float64) float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var adjustment float64
	switch d.currentRegime {
	case RegimeTrendingUp:
		adjustment = d.config.TrendingUpAdjustment
	case RegimeTrendingDown:
		adjustment = d.config.TrendingDownAdjustment
	case RegimeRanging:
		adjustment = d.config.RangingAdjustment
	case RegimeHighVolatility:
		adjustment = d.config.HighVolatilityAdjustment
	default:
		adjustment = 1.0
	}

	adjusted := confidence * adjustment
	if adjusted > 100 {
		adjusted = 100
	}
	if adjusted < 0 {
		adjusted = 0
	}

	return adjusted
}

// GetRegimeInfo returns comprehensive regime information.
func (d *MarketRegimeDetector) GetRegimeInfo() RegimeInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return RegimeInfo{
		Regime:         d.currentRegime,
		VIX:            d.currentVIX,
		VIXLevel:       d.vixLevel,
		ADX:            d.adxValue,
		ATR:            d.atrValue,
		TrendDirection: d.trendDirection,
		LastUpdate:     d.lastUpdate,
		Recommendation: d.getRecommendation(),
	}
}

func (d *MarketRegimeDetector) getRecommendation() string {
	switch d.currentRegime {
	case RegimeTrendingUp:
		return "Favorable for long positions. Consider trend-following strategies."
	case RegimeTrendingDown:
		return "Favorable for short positions or hedging. Exercise caution with longs."
	case RegimeRanging:
		return "Range-bound market. Consider mean-reversion strategies. Avoid breakout trades."
	case RegimeHighVolatility:
		return "High volatility detected. Reduce position sizes. Consider hedging."
	default:
		return "Insufficient data for regime detection."
	}
}

// ShouldReduceExposure returns true if current regime suggests reducing exposure.
func (d *MarketRegimeDetector) ShouldReduceExposure() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.currentRegime == RegimeHighVolatility ||
		d.vixLevel == VIXHigh ||
		d.vixLevel == VIXExtreme
}

// GetPositionSizeMultiplier returns a multiplier for position sizing based on regime.
func (d *MarketRegimeDetector) GetPositionSizeMultiplier() float64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	switch d.vixLevel {
	case VIXLow:
		return 1.0
	case VIXNormal:
		return 1.0
	case VIXElevated:
		return 0.8
	case VIXHigh:
		return 0.6
	case VIXExtreme:
		return 0.4
	default:
		return 1.0
	}
}

// RegimeInfo contains comprehensive regime information.
type RegimeInfo struct {
	Regime         MarketRegime
	VIX            float64
	VIXLevel       VIXLevel
	ADX            float64
	ATR            float64
	TrendDirection int
	LastUpdate     time.Time
	Recommendation string
}

// String returns a human-readable representation.
func (r RegimeInfo) String() string {
	direction := "Neutral"
	if r.TrendDirection > 0 {
		direction = "Up"
	} else if r.TrendDirection < 0 {
		direction = "Down"
	}

	return "Regime: " + string(r.Regime) +
		" | VIX: " + formatFloat(r.VIX) + " (" + string(r.VIXLevel) + ")" +
		" | ADX: " + formatFloat(r.ADX) +
		" | Trend: " + direction
}

func formatFloat(f float64) string {
	return fmt.Sprintf("%.2f", f)
}

// DefaultRegimeDetector is a global instance.
var DefaultRegimeDetector = NewMarketRegimeDetector(DefaultRegimeConfig())
