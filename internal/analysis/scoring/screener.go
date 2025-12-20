package scoring

import (
	"context"
	"fmt"
	"sync"

	"zerodha-trader/internal/analysis/indicators"
	"zerodha-trader/internal/models"
	"zerodha-trader/internal/store"
)

// FilterType represents the type of screener filter.
type FilterType string

const (
	FilterRSI           FilterType = "rsi"
	FilterGap           FilterType = "gap"
	FilterVolume        FilterType = "volume"
	FilterPrice         FilterType = "price"
	FilterMarketCap     FilterType = "market_cap"
	FilterSector        FilterType = "sector"
	FilterSignalScore   FilterType = "signal_score"
	FilterMACD          FilterType = "macd"
	FilterSuperTrend    FilterType = "supertrend"
	FilterBollingerBand FilterType = "bollinger"
)

// FilterOperator represents the comparison operator for a filter.
type FilterOperator string

const (
	OpGreaterThan      FilterOperator = ">"
	OpLessThan         FilterOperator = "<"
	OpGreaterThanEqual FilterOperator = ">="
	OpLessThanEqual    FilterOperator = "<="
	OpEqual            FilterOperator = "="
	OpNotEqual         FilterOperator = "!="
	OpCrossAbove       FilterOperator = "cross_above"
	OpCrossBelow       FilterOperator = "cross_below"
)

// Filter represents a single screener filter condition.
type Filter struct {
	Type     FilterType
	Operator FilterOperator
	Value    float64
	Period   int    // For indicators that need a period
	Extra    string // For sector filter, etc.
}

// ScreenerResult represents the result of screening a single symbol.
type ScreenerResult struct {
	Symbol       string
	Score        float64
	Matches      map[string]float64 // Filter name -> actual value
	SignalScore  *float64           // Optional signal score
	Passed       bool
	Error        error
}

// Screener provides stock screening functionality with concurrent processing.
type Screener struct {
	engine      *indicators.Engine
	scorer      *SignalScorer
	dataStore   store.DataStore
	concurrency int
}

// NewScreener creates a new stock screener.
func NewScreener(engine *indicators.Engine, dataStore store.DataStore, concurrency int) *Screener {
	if concurrency <= 0 {
		concurrency = 4
	}
	return &Screener{
		engine:      engine,
		scorer:      NewSignalScorer(engine),
		dataStore:   dataStore,
		concurrency: concurrency,
	}
}

// Scan scans the given symbols with the specified filters.
// All filters are combined with AND logic.
func (s *Screener) Scan(ctx context.Context, symbols []string, filters []Filter, candleProvider CandleProvider) ([]ScreenerResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	results := make([]ScreenerResult, 0, len(symbols))
	resultChan := make(chan ScreenerResult, len(symbols))
	workChan := make(chan string, len(symbols))

	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < s.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for symbol := range workChan {
				select {
				case <-ctx.Done():
					return
				default:
					result := s.scanSymbol(ctx, symbol, filters, candleProvider)
					resultChan <- result
				}
			}
		}()
	}

	// Send work
	go func() {
		for _, symbol := range symbols {
			select {
			case <-ctx.Done():
				break
			case workChan <- symbol:
			}
		}
		close(workChan)
	}()

	// Wait for workers and close result channel
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	for result := range resultChan {
		if result.Passed {
			results = append(results, result)
		}
	}

	// Sort by score (highest first)
	sortResultsByScore(results)

	return results, nil
}

// CandleProvider is a function that provides candles for a symbol.
type CandleProvider func(ctx context.Context, symbol string) ([]models.Candle, error)

// scanSymbol scans a single symbol against all filters.
func (s *Screener) scanSymbol(ctx context.Context, symbol string, filters []Filter, candleProvider CandleProvider) ScreenerResult {
	result := ScreenerResult{
		Symbol:  symbol,
		Matches: make(map[string]float64),
		Passed:  true,
	}

	// Get candles for the symbol
	candles, err := candleProvider(ctx, symbol)
	if err != nil {
		result.Error = err
		result.Passed = false
		return result
	}

	if len(candles) < 50 {
		result.Error = fmt.Errorf("insufficient data: need at least 50 candles")
		result.Passed = false
		return result
	}

	// Apply each filter
	for _, filter := range filters {
		passed, value, err := s.applyFilter(candles, filter)
		if err != nil {
			result.Error = err
			result.Passed = false
			return result
		}

		filterKey := fmt.Sprintf("%s_%s_%.2f", filter.Type, filter.Operator, filter.Value)
		result.Matches[filterKey] = value

		if !passed {
			result.Passed = false
			return result
		}
	}

	// Calculate signal score for passed symbols
	signalScore, err := s.scorer.Score(ctx, candles)
	if err == nil {
		result.SignalScore = &signalScore.Score
		result.Score = signalScore.Score
	}

	return result
}


// applyFilter applies a single filter to the candles.
func (s *Screener) applyFilter(candles []models.Candle, filter Filter) (bool, float64, error) {
	switch filter.Type {
	case FilterRSI:
		return s.applyRSIFilter(candles, filter)
	case FilterGap:
		return s.applyGapFilter(candles, filter)
	case FilterVolume:
		return s.applyVolumeFilter(candles, filter)
	case FilterPrice:
		return s.applyPriceFilter(candles, filter)
	case FilterSignalScore:
		return s.applySignalScoreFilter(candles, filter)
	case FilterMACD:
		return s.applyMACDFilter(candles, filter)
	case FilterSuperTrend:
		return s.applySuperTrendFilter(candles, filter)
	case FilterBollingerBand:
		return s.applyBollingerFilter(candles, filter)
	default:
		return false, 0, fmt.Errorf("unknown filter type: %s", filter.Type)
	}
}

// applyRSIFilter applies RSI filter.
func (s *Screener) applyRSIFilter(candles []models.Candle, filter Filter) (bool, float64, error) {
	period := filter.Period
	if period <= 0 {
		period = 14
	}

	rsi := indicators.NewRSI(period)
	values, err := rsi.Calculate(candles)
	if err != nil {
		return false, 0, err
	}

	// Get last non-zero RSI value
	var lastRSI float64
	for i := len(values) - 1; i >= 0; i-- {
		if values[i] != 0 {
			lastRSI = values[i]
			break
		}
	}

	return compareValues(lastRSI, filter.Operator, filter.Value), lastRSI, nil
}

// applyGapFilter applies gap filter (percentage gap from previous close).
func (s *Screener) applyGapFilter(candles []models.Candle, filter Filter) (bool, float64, error) {
	n := len(candles)
	if n < 2 {
		return false, 0, fmt.Errorf("insufficient data for gap filter")
	}

	prevClose := candles[n-2].Close
	currOpen := candles[n-1].Open

	if prevClose == 0 {
		return false, 0, fmt.Errorf("previous close is zero")
	}

	gapPercent := ((currOpen - prevClose) / prevClose) * 100

	return compareValues(gapPercent, filter.Operator, filter.Value), gapPercent, nil
}

// applyVolumeFilter applies volume filter (multiple of average volume).
func (s *Screener) applyVolumeFilter(candles []models.Candle, filter Filter) (bool, float64, error) {
	n := len(candles)
	period := filter.Period
	if period <= 0 {
		period = 20
	}

	if n < period+1 {
		return false, 0, fmt.Errorf("insufficient data for volume filter")
	}

	// Calculate average volume over the period (excluding current candle)
	var avgVolume float64
	for i := n - period - 1; i < n-1; i++ {
		avgVolume += float64(candles[i].Volume)
	}
	avgVolume /= float64(period)

	if avgVolume == 0 {
		return false, 0, fmt.Errorf("average volume is zero")
	}

	currentVolume := float64(candles[n-1].Volume)
	volumeRatio := currentVolume / avgVolume

	return compareValues(volumeRatio, filter.Operator, filter.Value), volumeRatio, nil
}

// applyPriceFilter applies price filter.
func (s *Screener) applyPriceFilter(candles []models.Candle, filter Filter) (bool, float64, error) {
	n := len(candles)
	if n == 0 {
		return false, 0, fmt.Errorf("no candles")
	}

	currentPrice := candles[n-1].Close
	return compareValues(currentPrice, filter.Operator, filter.Value), currentPrice, nil
}

// applySignalScoreFilter applies signal score filter.
func (s *Screener) applySignalScoreFilter(candles []models.Candle, filter Filter) (bool, float64, error) {
	score, err := s.scorer.Score(context.Background(), candles)
	if err != nil {
		return false, 0, err
	}

	return compareValues(score.Score, filter.Operator, filter.Value), score.Score, nil
}

// applyMACDFilter applies MACD filter.
func (s *Screener) applyMACDFilter(candles []models.Candle, filter Filter) (bool, float64, error) {
	macd := indicators.NewMACD(12, 26, 9)
	values, err := macd.Calculate(candles)
	if err != nil {
		return false, 0, err
	}

	n := len(candles)
	histogram := values["histogram"]

	// For crossover detection
	if filter.Operator == OpCrossAbove || filter.Operator == OpCrossBelow {
		if n < 2 {
			return false, 0, fmt.Errorf("insufficient data for crossover detection")
		}
		currHist := histogram[n-1]
		prevHist := histogram[n-2]

		if filter.Operator == OpCrossAbove {
			// MACD crossed above signal (histogram went from negative to positive)
			return prevHist < 0 && currHist > 0, currHist, nil
		}
		// MACD crossed below signal (histogram went from positive to negative)
		return prevHist > 0 && currHist < 0, currHist, nil
	}

	// Regular comparison on histogram value
	currHist := histogram[n-1]
	return compareValues(currHist, filter.Operator, filter.Value), currHist, nil
}

// applySuperTrendFilter applies SuperTrend filter.
func (s *Screener) applySuperTrendFilter(candles []models.Candle, filter Filter) (bool, float64, error) {
	st := indicators.NewSuperTrend(10, 3.0)
	values, err := st.Calculate(candles)
	if err != nil {
		return false, 0, err
	}

	n := len(candles)
	direction := values["direction"]
	currDirection := direction[n-1]

	// Direction: 1 = bullish, -1 = bearish
	// Filter value: 1 for bullish, -1 for bearish
	return compareValues(currDirection, filter.Operator, filter.Value), currDirection, nil
}

// applyBollingerFilter applies Bollinger Band filter.
func (s *Screener) applyBollingerFilter(candles []models.Candle, filter Filter) (bool, float64, error) {
	bb := indicators.NewBollingerBands(20, 2.0)
	values, err := bb.Calculate(candles)
	if err != nil {
		return false, 0, err
	}

	n := len(candles)
	percentB := values["percent_b"]
	currPercentB := percentB[n-1]

	// %B: 0 = at lower band, 1 = at upper band, 0.5 = at middle
	return compareValues(currPercentB, filter.Operator, filter.Value), currPercentB, nil
}

// compareValues compares two values using the given operator.
func compareValues(actual float64, op FilterOperator, expected float64) bool {
	switch op {
	case OpGreaterThan:
		return actual > expected
	case OpLessThan:
		return actual < expected
	case OpGreaterThanEqual:
		return actual >= expected
	case OpLessThanEqual:
		return actual <= expected
	case OpEqual:
		return actual == expected
	case OpNotEqual:
		return actual != expected
	default:
		return false
	}
}

// sortResultsByScore sorts results by score in descending order.
func sortResultsByScore(results []ScreenerResult) {
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}


// PresetScreener represents a pre-built screener configuration.
type PresetScreener struct {
	Name        string
	Description string
	Filters     []Filter
}

// GetPresetScreeners returns all available pre-built screeners.
func GetPresetScreeners() []PresetScreener {
	return []PresetScreener{
		MomentumScreener(),
		OversoldScreener(),
		OverboughtScreener(),
		BreakoutScreener(),
		ReversalScreener(),
		VolumeBreakoutScreener(),
		TrendFollowingScreener(),
	}
}

// MomentumScreener returns a screener for momentum stocks.
func MomentumScreener() PresetScreener {
	return PresetScreener{
		Name:        "momentum",
		Description: "Stocks with strong upward momentum",
		Filters: []Filter{
			{Type: FilterRSI, Operator: OpGreaterThan, Value: 50, Period: 14},
			{Type: FilterRSI, Operator: OpLessThan, Value: 70, Period: 14},
			{Type: FilterMACD, Operator: OpGreaterThan, Value: 0},
			{Type: FilterSuperTrend, Operator: OpEqual, Value: 1}, // Bullish
			{Type: FilterVolume, Operator: OpGreaterThan, Value: 1.0, Period: 20},
		},
	}
}

// OversoldScreener returns a screener for oversold stocks.
func OversoldScreener() PresetScreener {
	return PresetScreener{
		Name:        "oversold",
		Description: "Stocks that are potentially oversold",
		Filters: []Filter{
			{Type: FilterRSI, Operator: OpLessThan, Value: 30, Period: 14},
		},
	}
}

// OverboughtScreener returns a screener for overbought stocks.
func OverboughtScreener() PresetScreener {
	return PresetScreener{
		Name:        "overbought",
		Description: "Stocks that are potentially overbought",
		Filters: []Filter{
			{Type: FilterRSI, Operator: OpGreaterThan, Value: 70, Period: 14},
		},
	}
}

// BreakoutScreener returns a screener for breakout candidates.
func BreakoutScreener() PresetScreener {
	return PresetScreener{
		Name:        "breakout",
		Description: "Stocks breaking out with volume",
		Filters: []Filter{
			{Type: FilterVolume, Operator: OpGreaterThan, Value: 2.0, Period: 20},
			{Type: FilterBollingerBand, Operator: OpGreaterThan, Value: 1.0}, // Above upper band
			{Type: FilterMACD, Operator: OpCrossAbove, Value: 0},
		},
	}
}

// ReversalScreener returns a screener for potential reversal candidates.
func ReversalScreener() PresetScreener {
	return PresetScreener{
		Name:        "reversal",
		Description: "Stocks showing potential reversal signals",
		Filters: []Filter{
			{Type: FilterRSI, Operator: OpLessThan, Value: 35, Period: 14},
			{Type: FilterBollingerBand, Operator: OpLessThan, Value: 0.1}, // Near lower band
			{Type: FilterVolume, Operator: OpGreaterThan, Value: 1.5, Period: 20},
		},
	}
}

// VolumeBreakoutScreener returns a screener for volume breakouts.
func VolumeBreakoutScreener() PresetScreener {
	return PresetScreener{
		Name:        "volume_breakout",
		Description: "Stocks with unusual volume activity",
		Filters: []Filter{
			{Type: FilterVolume, Operator: OpGreaterThan, Value: 3.0, Period: 20},
		},
	}
}

// TrendFollowingScreener returns a screener for trend-following setups.
func TrendFollowingScreener() PresetScreener {
	return PresetScreener{
		Name:        "trend_following",
		Description: "Stocks in strong uptrend",
		Filters: []Filter{
			{Type: FilterSuperTrend, Operator: OpEqual, Value: 1},
			{Type: FilterRSI, Operator: OpGreaterThan, Value: 50, Period: 14},
			{Type: FilterRSI, Operator: OpLessThan, Value: 65, Period: 14},
			{Type: FilterMACD, Operator: OpGreaterThan, Value: 0},
		},
	}
}

// GetPresetByName returns a preset screener by name.
func GetPresetByName(name string) (*PresetScreener, error) {
	presets := GetPresetScreeners()
	for _, preset := range presets {
		if preset.Name == name {
			return &preset, nil
		}
	}
	return nil, fmt.Errorf("preset screener not found: %s", name)
}

// SaveQuery saves a custom screener query to the data store.
func (s *Screener) SaveQuery(ctx context.Context, name string, filters []Filter) error {
	if s.dataStore == nil {
		return fmt.Errorf("data store not configured")
	}

	storeFilters := make([]store.ScreenerFilter, len(filters))
	for i, f := range filters {
		storeFilters[i] = store.ScreenerFilter{
			Field:    string(f.Type),
			Operator: string(f.Operator),
			Value:    f.Value,
		}
	}

	query := store.ScreenerQuery{
		Name:    name,
		Filters: storeFilters,
	}

	return s.dataStore.SaveScreenerQuery(ctx, name, query)
}

// LoadQuery loads a custom screener query from the data store.
func (s *Screener) LoadQuery(ctx context.Context, name string) ([]Filter, error) {
	if s.dataStore == nil {
		return nil, fmt.Errorf("data store not configured")
	}

	query, err := s.dataStore.GetScreenerQuery(ctx, name)
	if err != nil {
		return nil, err
	}
	if query == nil {
		return nil, fmt.Errorf("screener query not found: %s", name)
	}

	filters := make([]Filter, len(query.Filters))
	for i, f := range query.Filters {
		filters[i] = Filter{
			Type:     FilterType(f.Field),
			Operator: FilterOperator(f.Operator),
			Value:    f.Value.(float64),
		}
	}

	return filters, nil
}

// ListSavedQueries lists all saved screener query names.
func (s *Screener) ListSavedQueries(ctx context.Context) ([]string, error) {
	if s.dataStore == nil {
		return nil, fmt.Errorf("data store not configured")
	}

	return s.dataStore.ListScreenerQueries(ctx)
}
