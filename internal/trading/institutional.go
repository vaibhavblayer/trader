// Package trading provides trading operations and utilities.
package trading

import (
	"context"
	"sort"
	"sync"
	"time"

	"zerodha-trader/internal/store"
)

// DealType represents types of institutional deals.
type DealType string

const (
	DealBulk  DealType = "BULK"
	DealBlock DealType = "BLOCK"
)

// InstitutionalDeal represents a bulk or block deal.
type InstitutionalDeal struct {
	ID           string
	Symbol       string
	DealType     DealType
	Date         time.Time
	ClientName   string
	BuySell      string // BUY or SELL
	Quantity     int64
	Price        float64
	Value        float64
	Exchange     string
	Remarks      string
}

// DealAlert represents an alert for institutional deal.
type DealAlert struct {
	Deal      InstitutionalDeal
	Message   string
	Timestamp time.Time
}

// InstitutionalFlowTracker tracks bulk and block deals for Indian markets.
type InstitutionalFlowTracker struct {
	store      store.DataStore
	deals      []InstitutionalDeal
	watchlist  map[string]bool // Symbols to watch
	alerts     []DealAlert
	mu         sync.RWMutex
}

// NewInstitutionalFlowTracker creates a new institutional flow tracker.
func NewInstitutionalFlowTracker(s store.DataStore) *InstitutionalFlowTracker {
	return &InstitutionalFlowTracker{
		store:     s,
		deals:     make([]InstitutionalDeal, 0),
		watchlist: make(map[string]bool),
		alerts:    make([]DealAlert, 0),
	}
}

// AddDeal adds an institutional deal.
func (t *InstitutionalFlowTracker) AddDeal(deal *InstitutionalDeal) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.deals = append(t.deals, *deal)

	// Check if symbol is in watchlist
	if t.watchlist[deal.Symbol] {
		t.alerts = append(t.alerts, DealAlert{
			Deal:      *deal,
			Message:   formatDealAlert(deal),
			Timestamp: time.Now(),
		})
	}
}

// AddToWatchlist adds a symbol to the watchlist.
func (t *InstitutionalFlowTracker) AddToWatchlist(symbol string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.watchlist[symbol] = true
}

// RemoveFromWatchlist removes a symbol from the watchlist.
func (t *InstitutionalFlowTracker) RemoveFromWatchlist(symbol string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.watchlist, symbol)
}

// GetBulkDeals returns bulk deals for a date range.
func (t *InstitutionalFlowTracker) GetBulkDeals(from, to time.Time) []InstitutionalDeal {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var deals []InstitutionalDeal
	for _, d := range t.deals {
		if d.DealType == DealBulk &&
			(d.Date.Equal(from) || d.Date.After(from)) &&
			(d.Date.Equal(to) || d.Date.Before(to)) {
			deals = append(deals, d)
		}
	}

	// Sort by date descending
	sort.Slice(deals, func(i, j int) bool {
		return deals[i].Date.After(deals[j].Date)
	})

	return deals
}

// GetBlockDeals returns block deals for a date range.
func (t *InstitutionalFlowTracker) GetBlockDeals(from, to time.Time) []InstitutionalDeal {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var deals []InstitutionalDeal
	for _, d := range t.deals {
		if d.DealType == DealBlock &&
			(d.Date.Equal(from) || d.Date.After(from)) &&
			(d.Date.Equal(to) || d.Date.Before(to)) {
			deals = append(deals, d)
		}
	}

	sort.Slice(deals, func(i, j int) bool {
		return deals[i].Date.After(deals[j].Date)
	})

	return deals
}

// GetDealsForSymbol returns all deals for a symbol.
func (t *InstitutionalFlowTracker) GetDealsForSymbol(symbol string) []InstitutionalDeal {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var deals []InstitutionalDeal
	for _, d := range t.deals {
		if d.Symbol == symbol {
			deals = append(deals, d)
		}
	}

	sort.Slice(deals, func(i, j int) bool {
		return deals[i].Date.After(deals[j].Date)
	})

	return deals
}

// GetTodaysDeals returns deals from today.
func (t *InstitutionalFlowTracker) GetTodaysDeals() []InstitutionalDeal {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	endOfDay := startOfDay.AddDate(0, 0, 1)
	
	t.mu.RLock()
	defer t.mu.RUnlock()

	var deals []InstitutionalDeal
	for _, d := range t.deals {
		if d.Date.After(startOfDay) && d.Date.Before(endOfDay) {
			deals = append(deals, d)
		}
	}

	return deals
}

// GetAlerts returns deal alerts.
func (t *InstitutionalFlowTracker) GetAlerts() []DealAlert {
	t.mu.RLock()
	defer t.mu.RUnlock()

	alerts := make([]DealAlert, len(t.alerts))
	copy(alerts, t.alerts)
	return alerts
}

// ClearAlerts clears all deal alerts.
func (t *InstitutionalFlowTracker) ClearAlerts() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.alerts = make([]DealAlert, 0)
}

// GetDealPatterns analyzes deal patterns for a symbol.
func (t *InstitutionalFlowTracker) GetDealPatterns(symbol string, days int) *DealPattern {
	t.mu.RLock()
	defer t.mu.RUnlock()

	cutoff := time.Now().AddDate(0, 0, -days)
	
	var buyDeals, sellDeals int
	var buyValue, sellValue float64

	for _, d := range t.deals {
		if d.Symbol != symbol || d.Date.Before(cutoff) {
			continue
		}

		if d.BuySell == "BUY" {
			buyDeals++
			buyValue += d.Value
		} else {
			sellDeals++
			sellValue += d.Value
		}
	}

	netFlow := buyValue - sellValue
	sentiment := "NEUTRAL"
	if netFlow > 0 {
		sentiment = "BULLISH"
	} else if netFlow < 0 {
		sentiment = "BEARISH"
	}

	return &DealPattern{
		Symbol:     symbol,
		Period:     days,
		BuyDeals:   buyDeals,
		SellDeals:  sellDeals,
		BuyValue:   buyValue,
		SellValue:  sellValue,
		NetFlow:    netFlow,
		Sentiment:  sentiment,
	}
}

// DealPattern represents deal pattern analysis.
type DealPattern struct {
	Symbol    string
	Period    int
	BuyDeals  int
	SellDeals int
	BuyValue  float64
	SellValue float64
	NetFlow   float64
	Sentiment string
}

// GetTopBulkDealStocks returns stocks with most bulk deals.
func (t *InstitutionalFlowTracker) GetTopBulkDealStocks(days int, limit int) []SymbolDealCount {
	t.mu.RLock()
	defer t.mu.RUnlock()

	cutoff := time.Now().AddDate(0, 0, -days)
	counts := make(map[string]int)

	for _, d := range t.deals {
		if d.DealType == DealBulk && d.Date.After(cutoff) {
			counts[d.Symbol]++
		}
	}

	var result []SymbolDealCount
	for symbol, count := range counts {
		result = append(result, SymbolDealCount{Symbol: symbol, Count: count})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Count > result[j].Count
	})

	if len(result) > limit {
		result = result[:limit]
	}

	return result
}

// SymbolDealCount represents deal count for a symbol.
type SymbolDealCount struct {
	Symbol string
	Count  int
}

// GetLargestDeals returns largest deals by value.
func (t *InstitutionalFlowTracker) GetLargestDeals(days int, limit int) []InstitutionalDeal {
	t.mu.RLock()
	defer t.mu.RUnlock()

	cutoff := time.Now().AddDate(0, 0, -days)
	var deals []InstitutionalDeal

	for _, d := range t.deals {
		if d.Date.After(cutoff) {
			deals = append(deals, d)
		}
	}

	sort.Slice(deals, func(i, j int) bool {
		return deals[i].Value > deals[j].Value
	})

	if len(deals) > limit {
		deals = deals[:limit]
	}

	return deals
}

// CheckWatchlistDeals checks for deals in watchlist stocks.
func (t *InstitutionalFlowTracker) CheckWatchlistDeals(ctx context.Context) []DealAlert {
	t.mu.RLock()
	defer t.mu.RUnlock()

	today := time.Now()
	startOfDay := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())

	var alerts []DealAlert
	for _, d := range t.deals {
		if t.watchlist[d.Symbol] && d.Date.After(startOfDay) {
			alerts = append(alerts, DealAlert{
				Deal:      d,
				Message:   formatDealAlert(&d),
				Timestamp: time.Now(),
			})
		}
	}

	return alerts
}

func formatDealAlert(deal *InstitutionalDeal) string {
	return deal.ClientName + " " + deal.BuySell + " " + deal.Symbol + " - " + 
		formatValue(deal.Value) + " (" + string(deal.DealType) + " deal)"
}

func formatValue(value float64) string {
	if value >= 10000000 { // 1 crore
		return formatFloat(value/10000000) + " Cr"
	} else if value >= 100000 { // 1 lakh
		return formatFloat(value/100000) + " L"
	}
	return formatFloat(value)
}

func formatFloat(f float64) string {
	return string(rune(int(f)))
}
