package trading

import (
	"fmt"
	"strings"

	"zerodha-trader/internal/analysis/indicators"
	"zerodha-trader/internal/models"
)

// LatestSignalDiagnostic evaluates the latest strategy signal and explains gate outcomes.
func (be *DefaultBacktestEngine) LatestSignalDiagnostic(config BacktestConfig, candles []models.Candle) (StrategySignalDiagnostic, error) {
	config = be.applyDefaults(config)
	if err := be.validateSimulationConfig(config); err != nil {
		return StrategySignalDiagnostic{}, fmt.Errorf("invalid config: %w", err)
	}
	if len(candles) < 2 {
		return StrategySignalDiagnostic{}, fmt.Errorf("insufficient data: need at least 2 candles, got %d", len(candles))
	}
	warmup := be.warmupPeriod(config.Strategy, config.Parameters)
	index := len(candles) - 1
	if index < warmup {
		return StrategySignalDiagnostic{
			Signal: "HOLD",
			Reason: fmt.Sprintf("warmup:%d/%d", index, warmup),
			Gates:  []StrategyGateDiagnostic{{Name: "warmup", Passed: false, Value: fmt.Sprintf("%d", index), Threshold: fmt.Sprintf(">=%d", warmup)}},
		}, nil
	}

	switch config.Strategy {
	case "multi_indicator":
		return diagnoseMultiIndicator(config.Parameters, candles)
	case "supertrend":
		return diagnoseSuperTrend(config.Parameters, candles)
	case "donchian_breakout":
		return diagnoseDonchian(config.Parameters, candles)
	case "intraday_momentum":
		return diagnoseIntradayMomentum(config.Parameters, candles)
	default:
		signal, confidence, err := be.LatestSignal(config, candles)
		if err != nil {
			return StrategySignalDiagnostic{}, err
		}
		return StrategySignalDiagnostic{
			Signal:     signal,
			Confidence: confidence,
			Reason:     latestSignalReason(signal),
			Gates:      []StrategyGateDiagnostic{{Name: "strategy_signal", Passed: signal == "BUY" || signal == "SELL", Value: signal, Threshold: "BUY|SELL"}},
		}, nil
	}
}

func diagnoseIntradayMomentum(params map[string]interface{}, candles []models.Candle) (StrategySignalDiagnostic, error) {
	index := len(candles) - 1
	emaPeriod := getIntParam(params, "ema_period", 9)
	emaVals, err := indicators.NewEMA(emaPeriod).Calculate(candles)
	if err != nil {
		return StrategySignalDiagnostic{}, err
	}
	if emaVals == nil || index >= len(emaVals) {
		return StrategySignalDiagnostic{}, fmt.Errorf("intraday momentum EMA unavailable")
	}
	signal, confidence, gates := evaluateIntradayMomentum(params, candles, index, emaVals[index])
	return StrategySignalDiagnostic{Signal: signal, Confidence: confidence, Reason: diagnosticReason(signal, gates), Gates: gates}, nil
}

func diagnoseMultiIndicator(params map[string]interface{}, candles []models.Candle) (StrategySignalDiagnostic, error) {
	index := len(candles) - 1
	shortPeriod := getIntParam(params, "short_period", 9)
	longPeriod := getIntParam(params, "long_period", 21)
	rsiPeriod := getIntParam(params, "rsi_period", 14)
	rsiMin := getFloatParam(params, "rsi_min", 30.0)
	rsiMax := getFloatParam(params, "rsi_max", 70.0)
	adxPeriod := getIntParam(params, "adx_period", 14)
	adxThreshold := getFloatParam(params, "adx_threshold", 20.0)
	volumePeriod := getIntParam(params, "volume_period", 20)
	volumeMultiplier := getFloatParam(params, "volume_multiplier", 1.2)
	requireVolume := getBoolParam(params, "require_volume", false)
	requireADX := getBoolParam(params, "require_adx", false)

	shortVals, err := indicators.NewEMA(shortPeriod).Calculate(candles)
	if err != nil {
		return StrategySignalDiagnostic{}, err
	}
	longVals, err := indicators.NewEMA(longPeriod).Calculate(candles)
	if err != nil {
		return StrategySignalDiagnostic{}, err
	}
	rsiVals, err := indicators.NewRSI(rsiPeriod).Calculate(candles)
	if err != nil {
		return StrategySignalDiagnostic{}, err
	}

	emaCrossUp := shortVals[index-1] <= longVals[index-1] && shortVals[index] > longVals[index]
	emaCrossDown := shortVals[index-1] >= longVals[index-1] && shortVals[index] < longVals[index]
	rsiValue := rsiVals[index]
	rsiOK := rsiValue > rsiMin && rsiValue < rsiMax

	volumeRatio := 0.0
	volumeOK := true
	if volumePeriod > 1 && len(candles) >= volumePeriod && index >= volumePeriod-1 {
		var volSum float64
		for i := index - volumePeriod + 1; i <= index; i++ {
			volSum += float64(candles[i].Volume)
		}
		avgVolume := volSum / float64(volumePeriod)
		if avgVolume > 0 {
			volumeRatio = float64(candles[index].Volume) / avgVolume
			volumeOK = volumeRatio > volumeMultiplier
		}
	}

	adxValue := 0.0
	trendStrong := true
	if adxVals, err := indicators.NewADX(adxPeriod).Calculate(candles); err == nil {
		if adxLine := adxVals["adx"]; index < len(adxLine) {
			adxValue = adxLine[index]
			trendStrong = adxValue > adxThreshold
		}
	}

	confirmationOK := volumeOK || trendStrong
	if requireVolume {
		confirmationOK = confirmationOK && volumeOK
	}
	if requireADX {
		confirmationOK = confirmationOK && trendStrong
	}

	confidence := 60.0
	if volumeOK {
		confidence += 10
	}
	if trendStrong {
		confidence += 10
	}

	signal := "HOLD"
	if emaCrossUp && rsiOK && confirmationOK {
		signal = "BUY"
	} else if emaCrossDown && rsiOK && confirmationOK {
		signal = "SELL"
	}
	if signal == "HOLD" {
		confidence = 0
	}

	gates := []StrategyGateDiagnostic{
		{Name: "ema_cross", Passed: emaCrossUp || emaCrossDown, Value: fmt.Sprintf("short=%.2f long=%.2f", shortVals[index], longVals[index]), Threshold: "cross"},
		{Name: "rsi_filter", Passed: rsiOK, Value: fmt.Sprintf("%.2f", rsiValue), Threshold: fmt.Sprintf("%.2f<rsi<%.2f", rsiMin, rsiMax)},
		{Name: "volume_filter", Passed: volumeOK, Value: fmt.Sprintf("%.2fx", volumeRatio), Threshold: fmt.Sprintf(">%.2fx", volumeMultiplier)},
		{Name: "adx_filter", Passed: trendStrong, Value: fmt.Sprintf("%.2f", adxValue), Threshold: fmt.Sprintf(">%.2f", adxThreshold)},
		{Name: "confirmation", Passed: confirmationOK, Value: confirmationValue(volumeOK, trendStrong, requireVolume, requireADX), Threshold: confirmationThreshold(requireVolume, requireADX)},
	}
	return StrategySignalDiagnostic{Signal: signal, Confidence: confidence, Reason: diagnosticReason(signal, gates), Gates: gates}, nil
}

func diagnoseSuperTrend(params map[string]interface{}, candles []models.Candle) (StrategySignalDiagnostic, error) {
	index := len(candles) - 1
	atrPeriod := getIntParam(params, "atr_period", 10)
	multiplier := getFloatParam(params, "multiplier", 3.0)
	values, err := indicators.NewSuperTrend(atrPeriod, multiplier).Calculate(candles)
	if err != nil {
		return StrategySignalDiagnostic{}, err
	}
	direction := values["direction"]
	if direction == nil || index >= len(direction) {
		return StrategySignalDiagnostic{}, fmt.Errorf("supertrend direction unavailable")
	}
	flipUp := direction[index-1] <= 0 && direction[index] > 0
	flipDown := direction[index-1] >= 0 && direction[index] < 0
	signal := "HOLD"
	if flipUp {
		signal = "BUY"
	} else if flipDown {
		signal = "SELL"
	}
	gates := []StrategyGateDiagnostic{
		{Name: "direction_flip", Passed: flipUp || flipDown, Value: fmt.Sprintf("prev=%.0f curr=%.0f", direction[index-1], direction[index]), Threshold: "sign change"},
	}
	confidence := 0.0
	if signal != "HOLD" {
		confidence = 80
	}
	return StrategySignalDiagnostic{Signal: signal, Confidence: confidence, Reason: diagnosticReason(signal, gates), Gates: gates}, nil
}

func diagnoseDonchian(params map[string]interface{}, candles []models.Candle) (StrategySignalDiagnostic, error) {
	index := len(candles) - 1
	period := getIntParam(params, "period", 20)
	values, err := indicators.NewDonchianChannels(period).Calculate(candles)
	if err != nil {
		return StrategySignalDiagnostic{}, err
	}
	upper := values["upper"]
	lower := values["lower"]
	if upper == nil || lower == nil || index >= len(upper) || index >= len(lower) {
		return StrategySignalDiagnostic{}, fmt.Errorf("donchian channels unavailable")
	}
	close := candles[index].Close
	breakUp := close > upper[index-1] && upper[index-1] > 0
	breakDown := close < lower[index-1] && lower[index-1] > 0
	signal := "HOLD"
	if breakUp {
		signal = "BUY"
	} else if breakDown {
		signal = "SELL"
	}
	gates := []StrategyGateDiagnostic{
		{Name: "channel_break", Passed: breakUp || breakDown, Value: fmt.Sprintf("close=%.2f upper=%.2f lower=%.2f", close, upper[index-1], lower[index-1]), Threshold: "close outside prior channel"},
	}
	confidence := 0.0
	if signal != "HOLD" {
		confidence = 80
	}
	return StrategySignalDiagnostic{Signal: signal, Confidence: confidence, Reason: diagnosticReason(signal, gates), Gates: gates}, nil
}

func diagnosticReason(signal string, gates []StrategyGateDiagnostic) string {
	if signal == "BUY" || signal == "SELL" {
		return latestSignalReason(signal)
	}
	failed := make([]string, 0)
	for _, gate := range gates {
		if !gate.Passed {
			failed = append(failed, gate.Name)
		}
	}
	if len(failed) == 0 {
		return "latest_signal_hold"
	}
	return "failed:" + strings.Join(failed, ",")
}

func latestSignalReason(signal string) string {
	return "latest_signal_" + strings.ToLower(signal)
}

func confirmationValue(volumeOK, trendStrong, requireVolume, requireADX bool) string {
	parts := []string{
		fmt.Sprintf("volume=%t", volumeOK),
		fmt.Sprintf("adx=%t", trendStrong),
	}
	if requireVolume {
		parts = append(parts, "require_volume=true")
	}
	if requireADX {
		parts = append(parts, "require_adx=true")
	}
	return strings.Join(parts, " ")
}

func confirmationThreshold(requireVolume, requireADX bool) string {
	switch {
	case requireVolume && requireADX:
		return "volume && adx"
	case requireVolume:
		return "volume"
	case requireADX:
		return "adx"
	default:
		return "volume || adx"
	}
}
