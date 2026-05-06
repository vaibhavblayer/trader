package trading

import (
	"fmt"
	"math"
	"strings"

	"zerodha-trader/internal/models"
)

func evaluateIntradayMomentum(params map[string]interface{}, candles []models.Candle, index int, emaValue float64) (string, float64, []StrategyGateDiagnostic) {
	lookback := getIntParam(params, "lookback_period", 8)
	volumePeriod := getIntParam(params, "volume_period", 20)
	volumeMultiplier := getFloatParam(params, "volume_multiplier", 1.1)
	minMovePct := getFloatParam(params, "min_move_pct", 0.08)
	minRangePct := getFloatParam(params, "min_range_pct", 0.12)
	requireVolume := getBoolParam(params, "require_volume", false)
	requireRange := getBoolParam(params, "require_range", false)
	mode := strings.ToLower(getStringParam(params, "mode", "breakout"))
	if lookback < 1 {
		lookback = 1
	}
	if index < 1 || index >= len(candles) {
		return "HOLD", 0, []StrategyGateDiagnostic{{Name: "data", Passed: false, Value: fmt.Sprintf("index=%d candles=%d", index, len(candles)), Threshold: "valid latest candle"}}
	}

	candle := candles[index]
	prev := candles[index-1]
	if candle.Close <= 0 || candle.Open <= 0 || prev.Close <= 0 {
		return "HOLD", 0, []StrategyGateDiagnostic{{Name: "price", Passed: false, Value: "non_positive", Threshold: "positive OHLC"}}
	}

	priorHigh, priorLow := priorRange(candles, index, lookback)
	movePct := (candle.Close - prev.Close) / prev.Close * 100
	bodyPct := (candle.Close - candle.Open) / candle.Open * 100
	rangePct := (candle.High - candle.Low) / candle.Close * 100
	volumeRatio := latestVolumeRatio(candles, index, volumePeriod)

	breakoutUp := priorHigh > 0 && candle.Close > priorHigh
	breakoutDown := priorLow > 0 && candle.Close < priorLow
	continuationUp := movePct >= minMovePct && bodyPct >= minMovePct
	continuationDown := movePct <= -minMovePct && bodyPct <= -minMovePct
	if mode == "continuation" {
		breakoutUp = continuationUp
		breakoutDown = continuationDown
	} else if mode == "hybrid" {
		breakoutUp = breakoutUp || continuationUp
		breakoutDown = breakoutDown || continuationDown
	}

	trendUp := candle.Close > emaValue
	trendDown := candle.Close < emaValue
	volumeOK := volumeRatio >= volumeMultiplier
	rangeOK := rangePct >= minRangePct
	activityOK := volumeOK || rangeOK
	if requireVolume && requireRange {
		activityOK = volumeOK && rangeOK
	} else if requireVolume {
		activityOK = volumeOK
	} else if requireRange {
		activityOK = rangeOK
	}

	buyOK := breakoutUp && trendUp && activityOK
	sellOK := breakoutDown && trendDown && activityOK
	signal := "HOLD"
	if buyOK {
		signal = "BUY"
	} else if sellOK {
		signal = "SELL"
	}

	confidence := 0.0
	if signal != "HOLD" {
		confidence = 60
		if volumeOK {
			confidence += 10
		}
		if rangeOK {
			confidence += 10
		}
		if (signal == "BUY" && breakoutUp && continuationUp) || (signal == "SELL" && breakoutDown && continuationDown) {
			confidence += 5
		}
		if confidence > 90 {
			confidence = 90
		}
	}

	gates := []StrategyGateDiagnostic{
		{Name: "momentum_trigger", Passed: breakoutUp || breakoutDown, Value: fmt.Sprintf("mode=%s close=%.2f high=%.2f low=%.2f move=%.2f%% body=%.2f%%", mode, candle.Close, priorHigh, priorLow, movePct, bodyPct), Threshold: "breakout or continuation"},
		{Name: "ema_alignment", Passed: (breakoutUp && trendUp) || (breakoutDown && trendDown), Value: fmt.Sprintf("close=%.2f ema=%.2f", candle.Close, emaValue), Threshold: "long above EMA, short below EMA"},
		{Name: "volume_filter", Passed: volumeOK, Value: fmt.Sprintf("%.2fx", volumeRatio), Threshold: fmt.Sprintf(">=%.2fx", volumeMultiplier)},
		{Name: "range_filter", Passed: rangeOK, Value: fmt.Sprintf("%.2f%%", rangePct), Threshold: fmt.Sprintf(">=%.2f%%", minRangePct)},
		{Name: "activity_confirmation", Passed: activityOK, Value: activityConfirmationValue(volumeOK, rangeOK, requireVolume, requireRange), Threshold: activityConfirmationThreshold(requireVolume, requireRange)},
	}
	return signal, confidence, gates
}

func activityConfirmationValue(volumeOK, rangeOK, requireVolume, requireRange bool) string {
	parts := []string{
		fmt.Sprintf("volume=%t", volumeOK),
		fmt.Sprintf("range=%t", rangeOK),
	}
	if requireVolume {
		parts = append(parts, "require_volume=true")
	}
	if requireRange {
		parts = append(parts, "require_range=true")
	}
	return strings.Join(parts, " ")
}

func activityConfirmationThreshold(requireVolume, requireRange bool) string {
	switch {
	case requireVolume && requireRange:
		return "volume && range"
	case requireVolume:
		return "volume"
	case requireRange:
		return "range"
	default:
		return "volume || range"
	}
}

func priorRange(candles []models.Candle, index int, lookback int) (float64, float64) {
	if lookback < 1 {
		lookback = 1
	}
	start := index - lookback
	if start < 0 {
		start = 0
	}
	high := 0.0
	low := math.MaxFloat64
	for i := start; i < index; i++ {
		if candles[i].High > high {
			high = candles[i].High
		}
		if candles[i].Low > 0 && candles[i].Low < low {
			low = candles[i].Low
		}
	}
	if low == math.MaxFloat64 {
		low = 0
	}
	return high, low
}

func latestVolumeRatio(candles []models.Candle, index int, period int) float64 {
	if period <= 1 || index <= 0 {
		return 1
	}
	start := index - period
	if start < 0 {
		start = 0
	}
	var sum float64
	var count int
	for i := start; i < index; i++ {
		sum += float64(candles[i].Volume)
		count++
	}
	if count == 0 {
		return 1
	}
	avg := sum / float64(count)
	if avg <= 0 {
		return 1
	}
	return float64(candles[index].Volume) / avg
}
