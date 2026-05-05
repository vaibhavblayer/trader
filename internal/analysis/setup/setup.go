// Package setup turns technical indicators into deterministic trade setups.
package setup

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"zerodha-trader/internal/analysis/indicators"
	"zerodha-trader/internal/models"
)

// Action is the executable direction produced by the setup engine.
type Action string

const (
	ActionBuy     Action = "BUY"
	ActionSell    Action = "SELL"
	ActionNoTrade Action = "NO_TRADE"
)

// Regime describes the current technical market state.
type Regime string

const (
	RegimeUnknown      Regime = "UNKNOWN"
	RegimeTrendingUp   Regime = "TRENDING_UP"
	RegimeTrendingDown Regime = "TRENDING_DOWN"
	RegimeRanging      Regime = "RANGING"
	RegimeHighVol      Regime = "HIGH_VOLATILITY"
)

// GateResult is one deterministic rule evaluation.
type GateResult struct {
	Name    string
	Passed  bool
	Reason  string
	Metrics map[string]float64
}

// Setup is the final output of deterministic technical analysis.
type Setup struct {
	Symbol        string
	Action        Action
	Regime        Regime
	Confidence    float64
	EntryPrice    float64
	StopLoss      float64
	Targets       []float64
	RiskReward    float64
	Invalidations []string
	Reasons       []string
	Gates         []GateResult
	AsOf          time.Time
	Indicators    Snapshot
}

// Snapshot contains the indicator values used for the decision.
type Snapshot struct {
	Close         float64
	RSI           float64
	PreviousRSI   float64
	EMA9          float64
	EMA21         float64
	EMA50         float64
	VWAP          float64
	VWAPDeviation float64
	ADX           float64
	PlusDI        float64
	MinusDI       float64
	ATR           float64
	ATRPercent    float64
	VolumeRatio   float64
	AverageVolume float64
	CurrentVolume float64
	MTFBullish    int
	MTFBearish    int
	MTFNeutral    int
	MTFTimeframes int
}

// Request is the data needed to evaluate a setup.
type Request struct {
	Symbol           string
	Candles          []models.Candle
	HigherTimeframes map[string][]models.Candle
	AsOfIndex        int // <=0 means use the last candle
	Now              time.Time
}

// Config controls deterministic setup gates.
type Config struct {
	MinCandles          int
	RSIBuyThreshold     float64
	RSISellThreshold    float64
	MinVolumeRatio      float64
	MinAverageVolume    float64
	MinADX              float64
	HighVolATRPercent   float64
	MaxVWAPDeviationPct float64
	MinRiskReward       float64
	ATRStopMultiplier   float64
	TargetRMultiples    []float64
	AvoidFirstMinutes   int
	AvoidLastMinutes    int
	RequireMTFAlignment bool
}

// DefaultConfig returns a conservative intraday setup configuration.
func DefaultConfig() Config {
	return Config{
		MinCandles:          60,
		RSIBuyThreshold:     55,
		RSISellThreshold:    45,
		MinVolumeRatio:      1.3,
		MinAverageVolume:    1000,
		MinADX:              25,
		HighVolATRPercent:   3.0,
		MaxVWAPDeviationPct: 0.7,
		MinRiskReward:       1.5,
		ATRStopMultiplier:   1.2,
		TargetRMultiples:    []float64{1.5, 2.0, 3.0},
		AvoidFirstMinutes:   15,
		AvoidLastMinutes:    10,
		RequireMTFAlignment: true,
	}
}

// Engine evaluates deterministic technical setups.
type Engine struct {
	config Config
}

// NewEngine creates a setup engine.
func NewEngine(config Config) *Engine {
	config = normalizeConfig(config)
	return &Engine{config: config}
}

// NewDefaultEngine creates a setup engine with conservative defaults.
func NewDefaultEngine() *Engine {
	return NewEngine(DefaultConfig())
}

// Evaluate evaluates the setup as of Request.AsOfIndex or the last candle.
func (e *Engine) Evaluate(ctx context.Context, req Request) (*Setup, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	candles, err := visibleCandles(req.Candles, req.AsOfIndex)
	if err != nil {
		return nil, err
	}
	if len(candles) < e.config.MinCandles {
		return nil, fmt.Errorf("insufficient data: need at least %d candles, got %d", e.config.MinCandles, len(candles))
	}

	snapshot, err := e.snapshot(candles)
	if err != nil {
		return nil, err
	}

	setup := &Setup{
		Symbol:     req.Symbol,
		Action:     ActionNoTrade,
		Regime:     e.detectRegime(snapshot),
		EntryPrice: snapshot.Close,
		AsOf:       candles[len(candles)-1].Timestamp,
		Indicators: snapshot,
	}

	candidate := e.candidateAction(snapshot)
	setup.Gates = append(setup.Gates,
		e.sessionGate(candles[len(candles)-1].Timestamp),
		e.regimeGate(setup.Regime, candidate),
		e.volumeGate(snapshot),
		e.momentumGate(snapshot, candidate),
		e.emaGate(snapshot, candidate),
		e.vwapGate(snapshot, candidate),
		e.adxGate(snapshot),
	)

	mtfGate := e.mtfGate(req.HigherTimeframes, candidate, &setup.Indicators)
	if mtfGate.Name != "" {
		setup.Gates = append(setup.Gates, mtfGate)
	}

	for _, gate := range setup.Gates {
		if gate.Passed {
			setup.Reasons = append(setup.Reasons, gate.Reason)
			continue
		}
		setup.Invalidations = append(setup.Invalidations, gate.Reason)
	}

	if candidate == ActionNoTrade || len(setup.Invalidations) > 0 {
		setup.Confidence = confidenceFromGates(setup.Gates, false)
		return setup, nil
	}

	e.applyRiskGeometry(setup, candidate, snapshot)
	if setup.RiskReward < e.config.MinRiskReward {
		setup.Invalidations = append(setup.Invalidations,
			fmt.Sprintf("risk reward %.2f below minimum %.2f", setup.RiskReward, e.config.MinRiskReward))
		setup.Action = ActionNoTrade
		setup.Confidence = confidenceFromGates(setup.Gates, false)
		return setup, nil
	}

	setup.Action = candidate
	setup.Confidence = confidenceFromGates(setup.Gates, true)
	setup.Reasons = append(setup.Reasons,
		fmt.Sprintf("%s setup accepted with %.2f risk-reward", candidate, setup.RiskReward))
	return setup, nil
}

func normalizeConfig(config Config) Config {
	defaults := DefaultConfig()
	if config.MinCandles == 0 {
		config.MinCandles = defaults.MinCandles
	}
	if config.RSIBuyThreshold == 0 {
		config.RSIBuyThreshold = defaults.RSIBuyThreshold
	}
	if config.RSISellThreshold == 0 {
		config.RSISellThreshold = defaults.RSISellThreshold
	}
	if config.MinVolumeRatio == 0 {
		config.MinVolumeRatio = defaults.MinVolumeRatio
	}
	if config.MinAverageVolume == 0 {
		config.MinAverageVolume = defaults.MinAverageVolume
	}
	if config.MinADX == 0 {
		config.MinADX = defaults.MinADX
	}
	if config.HighVolATRPercent == 0 {
		config.HighVolATRPercent = defaults.HighVolATRPercent
	}
	if config.MaxVWAPDeviationPct == 0 {
		config.MaxVWAPDeviationPct = defaults.MaxVWAPDeviationPct
	}
	if config.MinRiskReward == 0 {
		config.MinRiskReward = defaults.MinRiskReward
	}
	if config.ATRStopMultiplier == 0 {
		config.ATRStopMultiplier = defaults.ATRStopMultiplier
	}
	if len(config.TargetRMultiples) == 0 {
		config.TargetRMultiples = defaults.TargetRMultiples
	}
	return config
}

func visibleCandles(candles []models.Candle, asOfIndex int) ([]models.Candle, error) {
	if len(candles) == 0 {
		return nil, fmt.Errorf("candles are required")
	}
	if asOfIndex <= 0 {
		return candles, nil
	}
	if asOfIndex >= len(candles) {
		return nil, fmt.Errorf("as_of_index %d out of range for %d candles", asOfIndex, len(candles))
	}
	return candles[:asOfIndex+1], nil
}

func (e *Engine) snapshot(candles []models.Candle) (Snapshot, error) {
	n := len(candles)
	close := candles[n-1].Close
	rsiValues, err := indicators.NewRSI(14).Calculate(candles)
	if err != nil {
		return Snapshot{}, err
	}
	ema9, err := indicators.NewEMA(9).Calculate(candles)
	if err != nil {
		return Snapshot{}, err
	}
	ema21, err := indicators.NewEMA(21).Calculate(candles)
	if err != nil {
		return Snapshot{}, err
	}
	ema50, err := indicators.NewEMA(50).Calculate(candles)
	if err != nil {
		return Snapshot{}, err
	}
	vwap, err := indicators.NewVWAP().Calculate(candles)
	if err != nil {
		return Snapshot{}, err
	}
	adx, err := indicators.NewADX(14).Calculate(candles)
	if err != nil {
		return Snapshot{}, err
	}
	atr, err := indicators.NewATR(14).Calculate(candles)
	if err != nil {
		return Snapshot{}, err
	}

	avgVolume, volumeRatio := volumeStats(candles, 20)
	atrPct := 0.0
	if close > 0 {
		atrPct = atr[n-1] / close * 100
	}
	vwapDeviation := 0.0
	if vwap[n-1] > 0 {
		vwapDeviation = (close - vwap[n-1]) / vwap[n-1] * 100
	}

	return Snapshot{
		Close:         close,
		RSI:           rsiValues[n-1],
		PreviousRSI:   rsiValues[n-2],
		EMA9:          ema9[n-1],
		EMA21:         ema21[n-1],
		EMA50:         ema50[n-1],
		VWAP:          vwap[n-1],
		VWAPDeviation: vwapDeviation,
		ADX:           adx["adx"][n-1],
		PlusDI:        adx["plus_di"][n-1],
		MinusDI:       adx["minus_di"][n-1],
		ATR:           atr[n-1],
		ATRPercent:    atrPct,
		VolumeRatio:   volumeRatio,
		AverageVolume: avgVolume,
		CurrentVolume: float64(candles[n-1].Volume),
	}, nil
}

func volumeStats(candles []models.Candle, period int) (float64, float64) {
	n := len(candles)
	if n < period+1 {
		return 0, 0
	}
	var avg float64
	for i := n - period - 1; i < n-1; i++ {
		avg += float64(candles[i].Volume)
	}
	avg /= float64(period)
	if avg <= 0 {
		return avg, 0
	}
	return avg, float64(candles[n-1].Volume) / avg
}

func (e *Engine) detectRegime(s Snapshot) Regime {
	if s.ATRPercent >= e.config.HighVolATRPercent {
		return RegimeHighVol
	}
	if s.ADX >= e.config.MinADX {
		if s.PlusDI > s.MinusDI {
			return RegimeTrendingUp
		}
		if s.MinusDI > s.PlusDI {
			return RegimeTrendingDown
		}
	}
	if s.ADX > 0 && s.ADX < 18 {
		return RegimeRanging
	}
	return RegimeUnknown
}

func (e *Engine) candidateAction(s Snapshot) Action {
	if s.RSI > e.config.RSIBuyThreshold && s.RSI >= s.PreviousRSI {
		return ActionBuy
	}
	if s.RSI < e.config.RSISellThreshold && s.RSI <= s.PreviousRSI {
		return ActionSell
	}
	return ActionNoTrade
}

func (e *Engine) sessionGate(ts time.Time) GateResult {
	if ts.IsZero() || (e.config.AvoidFirstMinutes == 0 && e.config.AvoidLastMinutes == 0) {
		return pass("session", "session gate not configured", nil)
	}
	minutes := ts.Hour()*60 + ts.Minute()
	open := 9*60 + 15
	close := 15*60 + 30
	if e.config.AvoidFirstMinutes > 0 && minutes >= open && minutes < open+e.config.AvoidFirstMinutes {
		return fail("session", "inside opening avoidance window", nil)
	}
	if e.config.AvoidLastMinutes > 0 && minutes > close-e.config.AvoidLastMinutes && minutes <= close {
		return fail("session", "inside closing avoidance window", nil)
	}
	if minutes < open || minutes > close {
		return fail("session", "outside regular market hours", nil)
	}
	return pass("session", "regular session window", nil)
}

func (e *Engine) regimeGate(regime Regime, action Action) GateResult {
	if action == ActionNoTrade {
		return fail("regime", "no directional candidate", nil)
	}
	if regime == RegimeHighVol {
		return fail("regime", "high volatility regime blocks new setup", nil)
	}
	if action == ActionBuy && regime == RegimeTrendingDown {
		return fail("regime", "buy blocked in downtrend regime", nil)
	}
	if action == ActionSell && regime == RegimeTrendingUp {
		return fail("regime", "sell blocked in uptrend regime", nil)
	}
	return pass("regime", fmt.Sprintf("%s compatible with %s", regime, action), nil)
}

func (e *Engine) volumeGate(s Snapshot) GateResult {
	metrics := map[string]float64{
		"avg_volume":     s.AverageVolume,
		"current_volume": s.CurrentVolume,
		"volume_ratio":   s.VolumeRatio,
	}
	if s.AverageVolume < e.config.MinAverageVolume {
		return fail("volume", "average volume below liquidity floor", metrics)
	}
	if s.VolumeRatio < e.config.MinVolumeRatio {
		return fail("volume", "volume expansion below threshold", metrics)
	}
	return pass("volume", "volume expansion confirms participation", metrics)
}

func (e *Engine) momentumGate(s Snapshot, action Action) GateResult {
	metrics := map[string]float64{"rsi": s.RSI, "previous_rsi": s.PreviousRSI}
	switch action {
	case ActionBuy:
		return pass("momentum", "RSI is above bullish threshold and rising", metrics)
	case ActionSell:
		return pass("momentum", "RSI is below bearish threshold and falling", metrics)
	default:
		return fail("momentum", "RSI is in neutral or non-confirming regime", metrics)
	}
}

func (e *Engine) emaGate(s Snapshot, action Action) GateResult {
	metrics := map[string]float64{"close": s.Close, "ema9": s.EMA9, "ema21": s.EMA21, "ema50": s.EMA50}
	switch action {
	case ActionBuy:
		if s.Close > s.EMA9 && s.EMA9 > s.EMA21 && s.EMA21 >= s.EMA50 {
			return pass("ema_alignment", "price and EMAs are bullishly aligned", metrics)
		}
		return fail("ema_alignment", "bullish EMA alignment missing", metrics)
	case ActionSell:
		if s.Close < s.EMA9 && s.EMA9 < s.EMA21 && s.EMA21 <= s.EMA50 {
			return pass("ema_alignment", "price and EMAs are bearishly aligned", metrics)
		}
		return fail("ema_alignment", "bearish EMA alignment missing", metrics)
	default:
		return fail("ema_alignment", "no directional candidate for EMA gate", metrics)
	}
}

func (e *Engine) vwapGate(s Snapshot, action Action) GateResult {
	metrics := map[string]float64{"vwap": s.VWAP, "vwap_deviation_pct": s.VWAPDeviation}
	switch action {
	case ActionBuy:
		if s.VWAPDeviation > e.config.MaxVWAPDeviationPct {
			return fail("vwap", "buy is too stretched above VWAP", metrics)
		}
		return pass("vwap", "buy is not exhausted above VWAP", metrics)
	case ActionSell:
		if s.VWAPDeviation < -e.config.MaxVWAPDeviationPct {
			return fail("vwap", "sell is too stretched below VWAP", metrics)
		}
		return pass("vwap", "sell is not exhausted below VWAP", metrics)
	default:
		return fail("vwap", "no directional candidate for VWAP gate", metrics)
	}
}

func (e *Engine) adxGate(s Snapshot) GateResult {
	metrics := map[string]float64{"adx": s.ADX, "plus_di": s.PlusDI, "minus_di": s.MinusDI}
	if s.ADX < e.config.MinADX {
		return fail("trend_strength", "ADX below trend-strength threshold", metrics)
	}
	return pass("trend_strength", "ADX confirms trend strength", metrics)
}

func (e *Engine) mtfGate(timeframes map[string][]models.Candle, action Action, snapshot *Snapshot) GateResult {
	if len(timeframes) == 0 {
		if e.config.RequireMTFAlignment {
			return fail("mtf_alignment", "higher timeframe confirmation required but unavailable", nil)
		}
		return GateResult{}
	}

	keys := make([]string, 0, len(timeframes))
	for key := range timeframes {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var bullish, bearish, neutral int
	for _, key := range keys {
		dir := higherTimeframeDirection(timeframes[key])
		switch dir {
		case ActionBuy:
			bullish++
		case ActionSell:
			bearish++
		default:
			neutral++
		}
	}
	snapshot.MTFBullish = bullish
	snapshot.MTFBearish = bearish
	snapshot.MTFNeutral = neutral
	snapshot.MTFTimeframes = len(keys)

	metrics := map[string]float64{
		"bullish":    float64(bullish),
		"bearish":    float64(bearish),
		"neutral":    float64(neutral),
		"timeframes": float64(len(keys)),
	}
	if action == ActionBuy && bearish > 0 {
		return fail("mtf_alignment", "higher timeframe bearish opposition", metrics)
	}
	if action == ActionSell && bullish > 0 {
		return fail("mtf_alignment", "higher timeframe bullish opposition", metrics)
	}
	if e.config.RequireMTFAlignment {
		if action == ActionBuy && bullish == 0 {
			return fail("mtf_alignment", "buy lacks higher timeframe confirmation", metrics)
		}
		if action == ActionSell && bearish == 0 {
			return fail("mtf_alignment", "sell lacks higher timeframe confirmation", metrics)
		}
	}
	return pass("mtf_alignment", "higher timeframes do not oppose setup", metrics)
}

func higherTimeframeDirection(candles []models.Candle) Action {
	if len(candles) < 50 {
		return ActionNoTrade
	}
	n := len(candles)
	ema9, err := indicators.NewEMA(9).Calculate(candles)
	if err != nil {
		return ActionNoTrade
	}
	ema21, err := indicators.NewEMA(21).Calculate(candles)
	if err != nil {
		return ActionNoTrade
	}
	close := candles[n-1].Close
	if close > ema9[n-1] && ema9[n-1] > ema21[n-1] {
		return ActionBuy
	}
	if close < ema9[n-1] && ema9[n-1] < ema21[n-1] {
		return ActionSell
	}
	return ActionNoTrade
}

func (e *Engine) applyRiskGeometry(setup *Setup, action Action, s Snapshot) {
	atr := s.ATR
	if atr <= 0 {
		atr = math.Max(s.Close*0.005, 0.05)
	}
	risk := atr * e.config.ATRStopMultiplier
	if action == ActionBuy {
		setup.StopLoss = s.Close - risk
		for _, multiple := range e.config.TargetRMultiples {
			setup.Targets = append(setup.Targets, s.Close+risk*multiple)
		}
	} else {
		setup.StopLoss = s.Close + risk
		for _, multiple := range e.config.TargetRMultiples {
			setup.Targets = append(setup.Targets, s.Close-risk*multiple)
		}
	}
	if len(setup.Targets) > 0 {
		if action == ActionBuy {
			setup.RiskReward = (setup.Targets[0] - s.Close) / (s.Close - setup.StopLoss)
		} else {
			setup.RiskReward = (s.Close - setup.Targets[0]) / (setup.StopLoss - s.Close)
		}
	}
}

func confidenceFromGates(gates []GateResult, executable bool) float64 {
	if len(gates) == 0 {
		return 0
	}
	var passed int
	for _, gate := range gates {
		if gate.Passed {
			passed++
		}
	}
	conf := float64(passed) / float64(len(gates)) * 85
	if executable {
		conf += 10
	}
	if conf > 95 {
		return 95
	}
	return conf
}

func pass(name, reason string, metrics map[string]float64) GateResult {
	return GateResult{Name: name, Passed: true, Reason: reason, Metrics: metrics}
}

func fail(name, reason string, metrics map[string]float64) GateResult {
	return GateResult{Name: name, Passed: false, Reason: reason, Metrics: metrics}
}

// Summary returns a compact, stable explanation for display and LLM context.
func (s *Setup) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s setup: regime=%s confidence=%.1f", s.Symbol, s.Action, s.Regime, s.Confidence)
	if s.Action != ActionNoTrade {
		fmt.Fprintf(&b, " entry=%.2f sl=%.2f rr=%.2f", s.EntryPrice, s.StopLoss, s.RiskReward)
	}
	if len(s.Invalidations) > 0 {
		fmt.Fprintf(&b, " invalidations=%s", strings.Join(s.Invalidations, "; "))
	}
	return b.String()
}
