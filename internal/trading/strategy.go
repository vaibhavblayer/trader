package trading

import (
	"fmt"
	"sort"
	"strings"

	"zerodha-trader/internal/models"
)

// StrategyBuilder creates a signal generator for one backtest strategy.
type StrategyBuilder func(engine *DefaultBacktestEngine, params map[string]interface{}, candles []models.Candle) SignalGenerator

// StrategyWarmupFunc returns the number of bars needed before the strategy can emit signals.
type StrategyWarmupFunc func(params map[string]interface{}) int

// StrategyGate describes a deterministic condition that must be true for a setup to be valid.
type StrategyGate struct {
	Name        string
	Description string
}

// StrategyRiskModel describes the default risk envelope expected by a strategy.
type StrategyRiskModel struct {
	DefaultStopLossPercent    float64
	DefaultTakeProfitPercent  float64
	DefaultTrailingStopPct    float64
	DefaultMaxPositionPercent float64
	MinRiskReward             float64
	AllowShort                bool
}

// StrategyMetric describes a metric that should be reviewed for this strategy.
type StrategyMetric struct {
	Name        string
	Description string
}

// StrategyDefinition is the registry record for a backtest strategy.
type StrategyDefinition struct {
	Name        string
	Description string
	Category    string
	Parameters  map[string]interface{}
	Gates       []StrategyGate
	Risk        StrategyRiskModel
	Metrics     []StrategyMetric
	Warmup      StrategyWarmupFunc
	Build       StrategyBuilder
}

// StrategyRegistry owns available strategy definitions.
type StrategyRegistry struct {
	strategies map[string]StrategyDefinition
}

// NewStrategyRegistry creates an empty strategy registry.
func NewStrategyRegistry() *StrategyRegistry {
	return &StrategyRegistry{strategies: make(map[string]StrategyDefinition)}
}

// Register adds a strategy to the registry.
func (r *StrategyRegistry) Register(def StrategyDefinition) error {
	name := NormalizeStrategyName(def.Name)
	if name == "" {
		return fmt.Errorf("strategy name is required")
	}
	if def.Build == nil {
		return fmt.Errorf("strategy %s builder is required", name)
	}
	if def.Warmup == nil {
		return fmt.Errorf("strategy %s warmup function is required", name)
	}
	def.Name = name
	r.strategies[name] = def
	return nil
}

// Definition returns a strategy definition by name.
func (r *StrategyRegistry) Definition(name string) (StrategyDefinition, bool) {
	def, ok := r.strategies[NormalizeStrategyName(name)]
	return def, ok
}

// Definitions returns all strategies sorted by name.
func (r *StrategyRegistry) Definitions() []StrategyDefinition {
	defs := make([]StrategyDefinition, 0, len(r.strategies))
	for _, def := range r.strategies {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Name < defs[j].Name
	})
	return defs
}

// Names returns all strategy names sorted by name.
func (r *StrategyRegistry) Names() []string {
	defs := r.Definitions()
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	return names
}

// NormalizeStrategyName canonicalizes user-provided strategy names.
func NormalizeStrategyName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// DefaultStrategyRegistry returns the built-in strategy registry.
func DefaultStrategyRegistry() *StrategyRegistry {
	r := NewStrategyRegistry()
	mustRegisterStrategy(r, StrategyDefinition{
		Name:        "ema_crossover",
		Description: "EMA 9/21 crossover",
		Category:    "trend",
		Parameters:  map[string]interface{}{"short_period": 9, "long_period": 21},
		Gates: []StrategyGate{
			{Name: "ema_cross", Description: "short EMA crosses the long EMA"},
			{Name: "warmup", Description: "long EMA has enough history"},
		},
		Risk: StrategyRiskModel{
			DefaultStopLossPercent:    3,
			DefaultMaxPositionPercent: 95,
			MinRiskReward:             1.5,
			AllowShort:                true,
		},
		Metrics: commonStrategyMetrics("crossovers", "trend follow-through"),
		Warmup: func(params map[string]interface{}) int {
			return getIntParam(params, "long_period", 21) + 2
		},
		Build: func(engine *DefaultBacktestEngine, params map[string]interface{}, candles []models.Candle) SignalGenerator {
			return engine.emaCrossoverStrategy(params, candles)
		},
	})
	mustRegisterStrategy(r, StrategyDefinition{
		Name:        "sma_crossover",
		Description: "SMA 10/20 crossover",
		Category:    "trend",
		Parameters:  map[string]interface{}{"short_period": 10, "long_period": 20},
		Gates: []StrategyGate{
			{Name: "sma_cross", Description: "short SMA crosses the long SMA"},
			{Name: "warmup", Description: "long SMA has enough history"},
		},
		Risk: StrategyRiskModel{
			DefaultStopLossPercent:    3,
			DefaultMaxPositionPercent: 95,
			MinRiskReward:             1.5,
			AllowShort:                true,
		},
		Metrics: commonStrategyMetrics("crossovers", "trend follow-through"),
		Warmup: func(params map[string]interface{}) int {
			return getIntParam(params, "long_period", 20) + 2
		},
		Build: func(engine *DefaultBacktestEngine, params map[string]interface{}, candles []models.Candle) SignalGenerator {
			return engine.smaCrossoverStrategy(params, candles)
		},
	})
	mustRegisterStrategy(r, StrategyDefinition{
		Name:        "rsi_oversold",
		Description: "RSI oversold/overbought reversal",
		Category:    "mean_reversion",
		Parameters:  map[string]interface{}{"period": 14, "oversold": 30.0, "overbought": 70.0},
		Gates: []StrategyGate{
			{Name: "rsi_recovery", Description: "RSI exits oversold or overbought territory"},
			{Name: "warmup", Description: "RSI has enough Wilder smoothing history"},
		},
		Risk: StrategyRiskModel{
			DefaultStopLossPercent:    2,
			DefaultTakeProfitPercent:  4,
			DefaultMaxPositionPercent: 50,
			MinRiskReward:             2,
			AllowShort:                true,
		},
		Metrics: commonStrategyMetrics("RSI reversals", "mean-reversion hold time"),
		Warmup: func(params map[string]interface{}) int {
			return getIntParam(params, "period", 14) + 2
		},
		Build: func(engine *DefaultBacktestEngine, params map[string]interface{}, candles []models.Candle) SignalGenerator {
			return engine.rsiStrategy(params, candles)
		},
	})
	mustRegisterStrategy(r, StrategyDefinition{
		Name:        "macd",
		Description: "MACD signal line crossover",
		Category:    "momentum",
		Parameters:  map[string]interface{}{"fast_period": 12, "slow_period": 26, "signal_period": 9},
		Gates: []StrategyGate{
			{Name: "macd_cross", Description: "MACD crosses its signal line"},
			{Name: "warmup", Description: "slow EMA plus signal line has enough history"},
		},
		Risk: StrategyRiskModel{
			DefaultStopLossPercent:    3,
			DefaultMaxPositionPercent: 75,
			MinRiskReward:             1.5,
			AllowShort:                true,
		},
		Metrics: commonStrategyMetrics("MACD crosses", "momentum persistence"),
		Warmup: func(params map[string]interface{}) int {
			return getIntParam(params, "slow_period", 26) + getIntParam(params, "signal_period", 9)
		},
		Build: func(engine *DefaultBacktestEngine, params map[string]interface{}, candles []models.Candle) SignalGenerator {
			return engine.macdStrategy(params, candles)
		},
	})
	mustRegisterStrategy(r, StrategyDefinition{
		Name:        "supertrend",
		Description: "SuperTrend direction change",
		Category:    "trend",
		Parameters:  map[string]interface{}{"atr_period": 10, "multiplier": 3.0},
		Gates: []StrategyGate{
			{Name: "direction_flip", Description: "SuperTrend direction flips"},
			{Name: "atr_warmup", Description: "ATR has enough history"},
		},
		Risk: StrategyRiskModel{
			DefaultStopLossPercent:    2.5,
			DefaultTrailingStopPct:    2,
			DefaultMaxPositionPercent: 75,
			MinRiskReward:             1.5,
			AllowShort:                true,
		},
		Metrics: commonStrategyMetrics("direction flips", "trailing-stop exits"),
		Warmup: func(params map[string]interface{}) int {
			return getIntParam(params, "atr_period", 10) + 5
		},
		Build: func(engine *DefaultBacktestEngine, params map[string]interface{}, candles []models.Candle) SignalGenerator {
			return engine.supertrendStrategy(params, candles)
		},
	})
	mustRegisterStrategy(r, StrategyDefinition{
		Name:        "bollinger_breakout",
		Description: "Bollinger Band mean reversion",
		Category:    "mean_reversion",
		Parameters:  map[string]interface{}{"period": 20, "std_dev": 2.0},
		Gates: []StrategyGate{
			{Name: "band_reclaim", Description: "price reclaims an outer Bollinger Band"},
			{Name: "warmup", Description: "band window has enough history"},
		},
		Risk: StrategyRiskModel{
			DefaultStopLossPercent:    2,
			DefaultTakeProfitPercent:  3,
			DefaultMaxPositionPercent: 50,
			MinRiskReward:             1.5,
			AllowShort:                true,
		},
		Metrics: commonStrategyMetrics("band reclaims", "snapback expectancy"),
		Warmup: func(params map[string]interface{}) int {
			return getIntParam(params, "period", 20) + 2
		},
		Build: func(engine *DefaultBacktestEngine, params map[string]interface{}, candles []models.Candle) SignalGenerator {
			return engine.bollingerStrategy(params, candles)
		},
	})
	mustRegisterStrategy(r, StrategyDefinition{
		Name:        "adx_trend",
		Description: "ADX trend strength + DI crossover",
		Category:    "trend",
		Parameters:  map[string]interface{}{"period": 14, "threshold": 25.0},
		Gates: []StrategyGate{
			{Name: "adx_strength", Description: "ADX is above the trend threshold"},
			{Name: "di_cross", Description: "+DI/-DI crosses in trend direction"},
		},
		Risk: StrategyRiskModel{
			DefaultStopLossPercent:    3,
			DefaultTrailingStopPct:    2,
			DefaultMaxPositionPercent: 75,
			MinRiskReward:             1.5,
			AllowShort:                true,
		},
		Metrics: commonStrategyMetrics("DI crosses", "ADX-filtered expectancy"),
		Warmup: func(params map[string]interface{}) int {
			return getIntParam(params, "period", 14)*2 + 2
		},
		Build: func(engine *DefaultBacktestEngine, params map[string]interface{}, candles []models.Candle) SignalGenerator {
			return engine.adxTrendStrategy(params, candles)
		},
	})
	mustRegisterStrategy(r, StrategyDefinition{
		Name:        "donchian_breakout",
		Description: "Donchian channel breakout",
		Category:    "breakout",
		Parameters:  map[string]interface{}{"period": 20},
		Gates: []StrategyGate{
			{Name: "channel_break", Description: "close breaks the prior Donchian channel"},
			{Name: "warmup", Description: "channel window has enough history"},
		},
		Risk: StrategyRiskModel{
			DefaultStopLossPercent:    3,
			DefaultTrailingStopPct:    2,
			DefaultMaxPositionPercent: 75,
			MinRiskReward:             2,
			AllowShort:                true,
		},
		Metrics: commonStrategyMetrics("channel breaks", "breakout continuation"),
		Warmup: func(params map[string]interface{}) int {
			return getIntParam(params, "period", 20) + 2
		},
		Build: func(engine *DefaultBacktestEngine, params map[string]interface{}, candles []models.Candle) SignalGenerator {
			return engine.donchianStrategy(params, candles)
		},
	})
	mustRegisterStrategy(r, StrategyDefinition{
		Name:        "multi_indicator",
		Description: "EMA + RSI + ADX + volume filter",
		Category:    "composite",
		Gates: []StrategyGate{
			{Name: "ema_cross", Description: "EMA crossover fires"},
			{Name: "rsi_filter", Description: "RSI is not overextended"},
			{Name: "volume_or_adx", Description: "volume expansion or ADX confirms the signal"},
		},
		Risk: StrategyRiskModel{
			DefaultStopLossPercent:    3,
			DefaultMaxPositionPercent: 50,
			MinRiskReward:             2,
			AllowShort:                true,
		},
		Metrics: commonStrategyMetrics("composite signals", "filter contribution"),
		Warmup: func(params map[string]interface{}) int {
			return 40
		},
		Build: func(engine *DefaultBacktestEngine, params map[string]interface{}, candles []models.Candle) SignalGenerator {
			return engine.multiIndicatorStrategy(params, candles)
		},
	})
	mustRegisterStrategy(r, StrategyDefinition{
		Name:        "buy_and_hold",
		Description: "Baseline next-bar buy-and-hold",
		Category:    "baseline",
		Gates: []StrategyGate{
			{Name: "single_entry", Description: "enter once after the first warmup bar"},
		},
		Risk: StrategyRiskModel{
			DefaultMaxPositionPercent: 95,
			AllowShort:                false,
		},
		Metrics: []StrategyMetric{
			{Name: "benchmark_return", Description: "baseline return after modeled costs"},
			{Name: "drawdown", Description: "passive drawdown over the same period"},
		},
		Warmup: func(params map[string]interface{}) int {
			return 1
		},
		Build: func(engine *DefaultBacktestEngine, params map[string]interface{}, candles []models.Candle) SignalGenerator {
			return engine.buyAndHoldStrategy()
		},
	})
	return r
}

func mustRegisterStrategy(r *StrategyRegistry, def StrategyDefinition) {
	if err := r.Register(def); err != nil {
		panic(err)
	}
}

func commonStrategyMetrics(signalName, edgeName string) []StrategyMetric {
	return []StrategyMetric{
		{Name: "signals", Description: signalName},
		{Name: "expectancy", Description: edgeName},
		{Name: "drawdown", Description: "strategy max drawdown"},
		{Name: "cost_drag", Description: "transaction cost and slippage drag"},
	}
}

// AvailableStrategies returns the list of supported strategy names.
func AvailableStrategies() []string {
	return DefaultStrategyRegistry().Names()
}

// AvailableStrategyDefinitions returns the built-in strategy definitions.
func AvailableStrategyDefinitions() []StrategyDefinition {
	return DefaultStrategyRegistry().Definitions()
}
