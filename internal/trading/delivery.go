// Package trading provides trading operations and utilities.
package trading

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// DeliveryData represents daily delivery data for a stock.
type DeliveryData struct {
	Symbol            string
	Date              time.Time
	TotalVolume       int64
	DeliveryVolume    int64
	DeliveryPercent   float64
	AveragePrice      float64
	DeliveryValue     float64
}

// DeliveryAnalysis represents delivery analysis for a stock.
type DeliveryAnalysis struct {
	Symbol              string
	CurrentDelivery     float64
	AvgDelivery7D       float64
	AvgDelivery30D      float64
	DeliveryTrend       string // INCREASING, DECREASING, STABLE
	IsUnusual           bool
	UnusualReason       string
}

// DeliveryAlert represents an alert for unusual delivery.
type DeliveryAlert struct {
	Symbol    string
	Message   string
	Severity  string
	Timestamp time.Time
}

// DeliveryAnalyzer analyzes delivery data for Indian markets.
type DeliveryAnalyzer struct {
	data      map[string][]DeliveryData // symbol -> daily data
	alerts    []DeliveryAlert
	threshold float64 // Threshold for unusual delivery (e.g., 1.5x average)
	mu        sync.RWMutex
}

// NewDeliveryAnalyzer creates a new delivery analyzer.
func NewDeliveryAnalyzer() *DeliveryAnalyzer {
	return &DeliveryAnalyzer{
		data:      make(map[string][]DeliveryData),
		alerts:    make([]DeliveryAlert, 0),
		threshold: 1.5, // 50% above average is unusual
	}
}

// SetThreshold sets the threshold for unusual delivery detection.
func (a *DeliveryAnalyzer) SetThreshold(threshold float64) {
	a.threshold = threshold
}

// AddDeliveryData adds delivery data for a symbol.
func (a *DeliveryAnalyzer) AddDeliveryData(data *DeliveryData) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Calculate delivery percentage if not provided
	if data.DeliveryPercent == 0 && data.TotalVolume > 0 {
		data.DeliveryPercent = float64(data.DeliveryVolume) / float64(data.TotalVolume) * 100
	}

	// Calculate delivery value
	data.DeliveryValue = float64(data.DeliveryVolume) * data.AveragePrice

	a.data[data.Symbol] = append(a.data[data.Symbol], *data)

	// Sort by date
	sort.Slice(a.data[data.Symbol], func(i, j int) bool {
		return a.data[data.Symbol][i].Date.Before(a.data[data.Symbol][j].Date)
	})

	// Keep only last 90 days
	if len(a.data[data.Symbol]) > 90 {
		a.data[data.Symbol] = a.data[data.Symbol][len(a.data[data.Symbol])-90:]
	}
}

// GetDeliveryData returns delivery data for a symbol.
func (a *DeliveryAnalyzer) GetDeliveryData(symbol string, days int) []DeliveryData {
	a.mu.RLock()
	defer a.mu.RUnlock()

	data, ok := a.data[symbol]
	if !ok {
		return nil
	}

	if days >= len(data) {
		result := make([]DeliveryData, len(data))
		copy(result, data)
		return result
	}

	result := make([]DeliveryData, days)
	copy(result, data[len(data)-days:])
	return result
}

// GetAverageDelivery returns average delivery percentage for a period.
func (a *DeliveryAnalyzer) GetAverageDelivery(symbol string, days int) float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	data, ok := a.data[symbol]
	if !ok || len(data) == 0 {
		return 0
	}

	count := days
	if count > len(data) {
		count = len(data)
	}

	var sum float64
	for i := len(data) - count; i < len(data); i++ {
		sum += data[i].DeliveryPercent
	}

	return sum / float64(count)
}

// GetCurrentDelivery returns the most recent delivery percentage.
func (a *DeliveryAnalyzer) GetCurrentDelivery(symbol string) float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()

	data, ok := a.data[symbol]
	if !ok || len(data) == 0 {
		return 0
	}

	return data[len(data)-1].DeliveryPercent
}

// AnalyzeDelivery performs delivery analysis for a symbol.
func (a *DeliveryAnalyzer) AnalyzeDelivery(symbol string) *DeliveryAnalysis {
	current := a.GetCurrentDelivery(symbol)
	avg7D := a.GetAverageDelivery(symbol, 7)
	avg30D := a.GetAverageDelivery(symbol, 30)

	// Determine trend
	trend := "STABLE"
	if avg7D > avg30D*1.1 {
		trend = "INCREASING"
	} else if avg7D < avg30D*0.9 {
		trend = "DECREASING"
	}

	// Check for unusual delivery
	isUnusual := false
	unusualReason := ""
	if avg30D > 0 && current > avg30D*a.threshold {
		isUnusual = true
		unusualReason = fmt.Sprintf("Current delivery %.1f%% is %.1fx above 30-day average %.1f%%",
			current, current/avg30D, avg30D)
	}

	return &DeliveryAnalysis{
		Symbol:          symbol,
		CurrentDelivery: current,
		AvgDelivery7D:   avg7D,
		AvgDelivery30D:  avg30D,
		DeliveryTrend:   trend,
		IsUnusual:       isUnusual,
		UnusualReason:   unusualReason,
	}
}

// CheckUnusualDelivery checks for unusual delivery in a list of symbols.
func (a *DeliveryAnalyzer) CheckUnusualDelivery(symbols []string) []DeliveryAlert {
	var alerts []DeliveryAlert

	for _, symbol := range symbols {
		analysis := a.AnalyzeDelivery(symbol)
		if analysis.IsUnusual {
			alerts = append(alerts, DeliveryAlert{
				Symbol:    symbol,
				Message:   analysis.UnusualReason,
				Severity:  "INFO",
				Timestamp: time.Now(),
			})
		}
	}

	return alerts
}

// FilterByDeliveryPercent filters symbols by minimum delivery percentage.
func (a *DeliveryAnalyzer) FilterByDeliveryPercent(symbols []string, minPercent float64) []string {
	var filtered []string

	for _, symbol := range symbols {
		current := a.GetCurrentDelivery(symbol)
		if current >= minPercent {
			filtered = append(filtered, symbol)
		}
	}

	return filtered
}

// GetHighDeliveryStocks returns stocks with delivery above threshold.
func (a *DeliveryAnalyzer) GetHighDeliveryStocks(minPercent float64) []DeliveryData {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var result []DeliveryData

	for _, data := range a.data {
		if len(data) == 0 {
			continue
		}
		latest := data[len(data)-1]
		if latest.DeliveryPercent >= minPercent {
			result = append(result, latest)
		}
	}

	// Sort by delivery percentage descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].DeliveryPercent > result[j].DeliveryPercent
	})

	return result
}

// GetDeliveryTrend returns delivery trend data for charting.
func (a *DeliveryAnalyzer) GetDeliveryTrend(symbol string, days int) []float64 {
	data := a.GetDeliveryData(symbol, days)
	if data == nil {
		return nil
	}

	trend := make([]float64, len(data))
	for i, d := range data {
		trend[i] = d.DeliveryPercent
	}

	return trend
}

// GetAlerts returns delivery alerts.
func (a *DeliveryAnalyzer) GetAlerts() []DeliveryAlert {
	a.mu.RLock()
	defer a.mu.RUnlock()

	alerts := make([]DeliveryAlert, len(a.alerts))
	copy(alerts, a.alerts)
	return alerts
}

// ClearAlerts clears all delivery alerts.
func (a *DeliveryAnalyzer) ClearAlerts() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.alerts = make([]DeliveryAlert, 0)
}

// CompareDelivery compares delivery between two symbols.
func (a *DeliveryAnalyzer) CompareDelivery(symbol1, symbol2 string, days int) (float64, float64) {
	avg1 := a.GetAverageDelivery(symbol1, days)
	avg2 := a.GetAverageDelivery(symbol2, days)
	return avg1, avg2
}

// GetDeliveryRanking ranks symbols by delivery percentage.
func (a *DeliveryAnalyzer) GetDeliveryRanking(symbols []string) []SymbolDelivery {
	var ranking []SymbolDelivery

	for _, symbol := range symbols {
		current := a.GetCurrentDelivery(symbol)
		ranking = append(ranking, SymbolDelivery{
			Symbol:          symbol,
			DeliveryPercent: current,
		})
	}

	sort.Slice(ranking, func(i, j int) bool {
		return ranking[i].DeliveryPercent > ranking[j].DeliveryPercent
	})

	return ranking
}

// SymbolDelivery represents delivery data for ranking.
type SymbolDelivery struct {
	Symbol          string
	DeliveryPercent float64
}
