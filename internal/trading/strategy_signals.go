package trading

import (
	"zerodha-trader/internal/analysis/indicators"
	"zerodha-trader/internal/models"
)

func (be *DefaultBacktestEngine) buyAndHoldStrategy() SignalGenerator {
	return func(candles []models.Candle, index int) (string, float64) {
		if index == 1 {
			return "BUY", 100
		}
		return "HOLD", 0
	}
}

// smaCrossoverStrategy uses the real SMA indicator.
func (be *DefaultBacktestEngine) smaCrossoverStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	shortPeriod := getIntParam(params, "short_period", 10)
	longPeriod := getIntParam(params, "long_period", 20)

	shortSMA := indicators.NewSMA(shortPeriod)
	longSMA := indicators.NewSMA(longPeriod)
	shortVals, _ := shortSMA.Calculate(allCandles)
	longVals, _ := longSMA.Calculate(allCandles)

	return func(candles []models.Candle, index int) (string, float64) {
		if shortVals == nil || longVals == nil || index < 1 {
			return "HOLD", 0
		}
		if index >= len(shortVals) || index >= len(longVals) {
			return "HOLD", 0
		}
		if shortVals[index-1] <= longVals[index-1] && shortVals[index] > longVals[index] {
			return "BUY", 70
		}
		if shortVals[index-1] >= longVals[index-1] && shortVals[index] < longVals[index] {
			return "SELL", 70
		}
		return "HOLD", 0
	}
}

// emaCrossoverStrategy uses the real EMA indicator.
func (be *DefaultBacktestEngine) emaCrossoverStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	shortPeriod := getIntParam(params, "short_period", 9)
	longPeriod := getIntParam(params, "long_period", 21)

	shortEMA := indicators.NewEMA(shortPeriod)
	longEMA := indicators.NewEMA(longPeriod)
	shortVals, _ := shortEMA.Calculate(allCandles)
	longVals, _ := longEMA.Calculate(allCandles)

	return func(candles []models.Candle, index int) (string, float64) {
		if shortVals == nil || longVals == nil || index < 1 {
			return "HOLD", 0
		}
		if index >= len(shortVals) || index >= len(longVals) {
			return "HOLD", 0
		}
		if shortVals[index-1] <= longVals[index-1] && shortVals[index] > longVals[index] {
			return "BUY", 70
		}
		if shortVals[index-1] >= longVals[index-1] && shortVals[index] < longVals[index] {
			return "SELL", 70
		}
		return "HOLD", 0
	}
}

// rsiStrategy uses the real RSI indicator with proper Wilder smoothing.
func (be *DefaultBacktestEngine) rsiStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	period := getIntParam(params, "period", 14)
	oversold := getFloatParam(params, "oversold", 30.0)
	overbought := getFloatParam(params, "overbought", 70.0)

	rsi := indicators.NewRSI(period)
	rsiVals, _ := rsi.Calculate(allCandles)

	return func(candles []models.Candle, index int) (string, float64) {
		if rsiVals == nil || index < 1 || index >= len(rsiVals) {
			return "HOLD", 0
		}
		prev := rsiVals[index-1]
		curr := rsiVals[index]
		if prev <= oversold && curr > oversold {
			return "BUY", 65
		}
		if prev >= overbought && curr < overbought {
			return "SELL", 65
		}
		return "HOLD", 0
	}
}

// macdStrategy uses the real MACD indicator.
func (be *DefaultBacktestEngine) macdStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	fast := getIntParam(params, "fast_period", 12)
	slow := getIntParam(params, "slow_period", 26)
	signal := getIntParam(params, "signal_period", 9)

	macd := indicators.NewMACD(fast, slow, signal)
	macdVals, _ := macd.Calculate(allCandles)

	return func(candles []models.Candle, index int) (string, float64) {
		if macdVals == nil || index < 1 {
			return "HOLD", 0
		}
		macdLine := macdVals["macd"]
		signalLine := macdVals["signal"]
		if macdLine == nil || signalLine == nil || index >= len(macdLine) || index >= len(signalLine) {
			return "HOLD", 0
		}
		if macdLine[index-1] <= signalLine[index-1] && macdLine[index] > signalLine[index] {
			return "BUY", 75
		}
		if macdLine[index-1] >= signalLine[index-1] && macdLine[index] < signalLine[index] {
			return "SELL", 75
		}
		return "HOLD", 0
	}
}

// supertrendStrategy uses the real SuperTrend indicator.
func (be *DefaultBacktestEngine) supertrendStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	atrPeriod := getIntParam(params, "atr_period", 10)
	multiplier := getFloatParam(params, "multiplier", 3.0)

	st := indicators.NewSuperTrend(atrPeriod, multiplier)
	stVals, _ := st.Calculate(allCandles)

	return func(candles []models.Candle, index int) (string, float64) {
		if stVals == nil || index < 1 {
			return "HOLD", 0
		}
		direction := stVals["direction"]
		if direction == nil || index >= len(direction) {
			return "HOLD", 0
		}
		if direction[index-1] <= 0 && direction[index] > 0 {
			return "BUY", 80
		}
		if direction[index-1] >= 0 && direction[index] < 0 {
			return "SELL", 80
		}
		return "HOLD", 0
	}
}

// bollingerStrategy buys on lower-band reclaim and sells on upper-band rejection.
func (be *DefaultBacktestEngine) bollingerStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	period := getIntParam(params, "period", 20)
	stdDev := getFloatParam(params, "std_dev", 2.0)

	bb := indicators.NewBollingerBands(period, stdDev)
	bbVals, _ := bb.Calculate(allCandles)

	return func(candles []models.Candle, index int) (string, float64) {
		if bbVals == nil || index < 1 || index >= len(allCandles) {
			return "HOLD", 0
		}
		upper := bbVals["upper"]
		lower := bbVals["lower"]
		if upper == nil || lower == nil || index >= len(upper) || index >= len(lower) {
			return "HOLD", 0
		}
		close := candles[index].Close
		prevClose := candles[index-1].Close

		if prevClose <= lower[index-1] && close > lower[index] {
			return "BUY", 65
		}
		if prevClose >= upper[index-1] && close < upper[index] {
			return "SELL", 65
		}
		return "HOLD", 0
	}
}

// adxTrendStrategy enters on strong trend in the direction of +DI/-DI.
func (be *DefaultBacktestEngine) adxTrendStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	period := getIntParam(params, "period", 14)
	threshold := getFloatParam(params, "threshold", 25.0)

	adx := indicators.NewADX(period)
	adxVals, _ := adx.Calculate(allCandles)

	return func(candles []models.Candle, index int) (string, float64) {
		if adxVals == nil || index < 1 {
			return "HOLD", 0
		}
		adxLine := adxVals["adx"]
		plusDI := adxVals["plus_di"]
		minusDI := adxVals["minus_di"]
		if adxLine == nil || plusDI == nil || minusDI == nil {
			return "HOLD", 0
		}
		if index >= len(adxLine) || index >= len(plusDI) || index >= len(minusDI) {
			return "HOLD", 0
		}
		if adxLine[index] < threshold {
			return "HOLD", 0
		}
		if plusDI[index-1] <= minusDI[index-1] && plusDI[index] > minusDI[index] {
			return "BUY", 75
		}
		if minusDI[index-1] <= plusDI[index-1] && minusDI[index] > plusDI[index] {
			return "SELL", 75
		}
		return "HOLD", 0
	}
}

// donchianStrategy trades Donchian channel breakouts.
func (be *DefaultBacktestEngine) donchianStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	period := getIntParam(params, "period", 20)

	dc := indicators.NewDonchianChannels(period)
	dcVals, _ := dc.Calculate(allCandles)

	return func(candles []models.Candle, index int) (string, float64) {
		if dcVals == nil || index < 1 || index >= len(allCandles) {
			return "HOLD", 0
		}
		upper := dcVals["upper"]
		lower := dcVals["lower"]
		if upper == nil || lower == nil || index >= len(upper) || index >= len(lower) {
			return "HOLD", 0
		}

		close := candles[index].Close
		if close > upper[index-1] && upper[index-1] > 0 {
			return "BUY", 80
		}
		if close < lower[index-1] && lower[index-1] > 0 {
			return "SELL", 80
		}
		return "HOLD", 0
	}
}

// multiIndicatorStrategy combines EMA crossover with RSI, ADX, and volume filters.
func (be *DefaultBacktestEngine) multiIndicatorStrategy(params map[string]interface{}, allCandles []models.Candle) SignalGenerator {
	shortEMA := indicators.NewEMA(9)
	longEMA := indicators.NewEMA(21)
	shortVals, _ := shortEMA.Calculate(allCandles)
	longVals, _ := longEMA.Calculate(allCandles)

	rsi := indicators.NewRSI(14)
	rsiVals, _ := rsi.Calculate(allCandles)

	adx := indicators.NewADX(14)
	adxVals, _ := adx.Calculate(allCandles)

	var avgVolumes []float64
	if len(allCandles) >= 20 {
		avgVolumes = make([]float64, len(allCandles))
		for i := 19; i < len(allCandles); i++ {
			var volSum float64
			for j := i - 19; j <= i; j++ {
				volSum += float64(allCandles[j].Volume)
			}
			avgVolumes[i] = volSum / 20
		}
	}

	return func(candles []models.Candle, index int) (string, float64) {
		if shortVals == nil || longVals == nil || rsiVals == nil || index < 1 {
			return "HOLD", 0
		}
		if index >= len(shortVals) || index >= len(longVals) || index >= len(rsiVals) {
			return "HOLD", 0
		}

		emaCross := shortVals[index-1] <= longVals[index-1] && shortVals[index] > longVals[index]
		emaCrossDown := shortVals[index-1] >= longVals[index-1] && shortVals[index] < longVals[index]
		rsiOK := rsiVals[index] > 30 && rsiVals[index] < 70

		volumeOK := true
		if avgVolumes != nil && index < len(avgVolumes) && avgVolumes[index] > 0 {
			volumeOK = float64(candles[index].Volume) > avgVolumes[index]*1.2
		}

		trendStrong := true
		if adxVals != nil {
			if adxLine, ok := adxVals["adx"]; ok && index < len(adxLine) {
				trendStrong = adxLine[index] > 20
			}
		}

		confidence := 60.0
		if volumeOK {
			confidence += 10
		}
		if trendStrong {
			confidence += 10
		}

		if emaCross && rsiOK && (volumeOK || trendStrong) {
			return "BUY", confidence
		}
		if emaCrossDown && rsiOK && (volumeOK || trendStrong) {
			return "SELL", confidence
		}
		return "HOLD", 0
	}
}
