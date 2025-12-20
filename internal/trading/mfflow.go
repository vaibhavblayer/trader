// Package trading provides trading operations and utilities.
package trading

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// MFHolding represents mutual fund holding data.
type MFHolding struct {
	Symbol          string
	Quarter         string // e.g., "Q3FY24"
	Date            time.Time
	MFPercent       float64 // Total MF holding percentage
	NumSchemes      int     // Number of MF schemes holding
	TotalShares     int64
	TotalValue      float64
}

// MFSchemeHolding represents holding by a specific MF scheme.
type MFSchemeHolding struct {
	Symbol      string
	SchemeName  string
	AMCName     string
	Quantity    int64
	Value       float64
	PercentAUM  float64 // Percentage of scheme's AUM
	Quarter     string
}

// SIPFlowData represents SIP flow data.
type SIPFlowData struct {
	Month       time.Time
	GrossInflow float64
	Redemption  float64
	NetInflow   float64
	TotalAUM    float64
}

// MFAlert represents an alert for MF activity.
type MFAlert struct {
	Symbol    string
	AlertType string
	Message   string
	Severity  string
	Timestamp time.Time
}

// MFFlowTracker tracks mutual fund holdings and flows.
type MFFlowTracker struct {
	holdings      map[string][]MFHolding       // symbol -> quarterly holdings
	schemeHoldings map[string][]MFSchemeHolding // symbol -> scheme holdings
	sipFlows      []SIPFlowData
	alerts        []MFAlert
	mu            sync.RWMutex
}

// NewMFFlowTracker creates a new MF flow tracker.
func NewMFFlowTracker() *MFFlowTracker {
	return &MFFlowTracker{
		holdings:       make(map[string][]MFHolding),
		schemeHoldings: make(map[string][]MFSchemeHolding),
		sipFlows:       make([]SIPFlowData, 0),
		alerts:         make([]MFAlert, 0),
	}
}

// AddHolding adds MF holding data.
func (t *MFFlowTracker) AddHolding(holding *MFHolding) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.holdings[holding.Symbol] = append(t.holdings[holding.Symbol], *holding)

	// Sort by date
	sort.Slice(t.holdings[holding.Symbol], func(i, j int) bool {
		return t.holdings[holding.Symbol][i].Date.Before(t.holdings[holding.Symbol][j].Date)
	})

	// Check for significant changes
	t.checkHoldingChange(holding.Symbol)
}

// GetCurrentHolding returns the most recent MF holding.
func (t *MFFlowTracker) GetCurrentHolding(symbol string) *MFHolding {
	t.mu.RLock()
	defer t.mu.RUnlock()

	holdings, ok := t.holdings[symbol]
	if !ok || len(holdings) == 0 {
		return nil
	}

	return &holdings[len(holdings)-1]
}

// GetHoldingHistory returns MF holding history.
func (t *MFFlowTracker) GetHoldingHistory(symbol string, quarters int) []MFHolding {
	t.mu.RLock()
	defer t.mu.RUnlock()

	holdings, ok := t.holdings[symbol]
	if !ok {
		return nil
	}

	if quarters >= len(holdings) {
		result := make([]MFHolding, len(holdings))
		copy(result, holdings)
		return result
	}

	result := make([]MFHolding, quarters)
	copy(result, holdings[len(holdings)-quarters:])
	return result
}

// GetHoldingChange returns change in MF holding over quarters.
func (t *MFFlowTracker) GetHoldingChange(symbol string, quarters int) float64 {
	history := t.GetHoldingHistory(symbol, quarters)
	if len(history) < 2 {
		return 0
	}

	oldest := history[0].MFPercent
	latest := history[len(history)-1].MFPercent
	return latest - oldest
}

// GetHighestMFHoldings returns stocks with highest MF holdings.
func (t *MFFlowTracker) GetHighestMFHoldings(limit int) []MFHolding {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var latest []MFHolding

	for _, holdings := range t.holdings {
		if len(holdings) > 0 {
			latest = append(latest, holdings[len(holdings)-1])
		}
	}

	sort.Slice(latest, func(i, j int) bool {
		return latest[i].MFPercent > latest[j].MFPercent
	})

	if len(latest) > limit {
		latest = latest[:limit]
	}

	return latest
}

// GetNewEntries returns stocks that MFs have newly entered.
func (t *MFFlowTracker) GetNewEntries(quarters int) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var newEntries []string

	for symbol, holdings := range t.holdings {
		if len(holdings) < 2 {
			continue
		}

		// Check if MF holding was 0 in older quarter but positive now
		startIdx := len(holdings) - quarters
		if startIdx < 0 {
			startIdx = 0
		}

		if holdings[startIdx].MFPercent == 0 && holdings[len(holdings)-1].MFPercent > 0 {
			newEntries = append(newEntries, symbol)
		}
	}

	return newEntries
}

// GetExits returns stocks that MFs have exited.
func (t *MFFlowTracker) GetExits(quarters int) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var exits []string

	for symbol, holdings := range t.holdings {
		if len(holdings) < 2 {
			continue
		}

		startIdx := len(holdings) - quarters
		if startIdx < 0 {
			startIdx = 0
		}

		if holdings[startIdx].MFPercent > 0 && holdings[len(holdings)-1].MFPercent == 0 {
			exits = append(exits, symbol)
		}
	}

	return exits
}

// AddSchemeHolding adds scheme-level holding data.
func (t *MFFlowTracker) AddSchemeHolding(holding *MFSchemeHolding) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.schemeHoldings[holding.Symbol] = append(t.schemeHoldings[holding.Symbol], *holding)
}

// GetSchemeHoldings returns scheme holdings for a symbol.
func (t *MFFlowTracker) GetSchemeHoldings(symbol string) []MFSchemeHolding {
	t.mu.RLock()
	defer t.mu.RUnlock()

	holdings, ok := t.schemeHoldings[symbol]
	if !ok {
		return nil
	}

	result := make([]MFSchemeHolding, len(holdings))
	copy(result, holdings)

	// Sort by value descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].Value > result[j].Value
	})

	return result
}

// AddSIPFlow adds SIP flow data.
func (t *MFFlowTracker) AddSIPFlow(flow *SIPFlowData) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.sipFlows = append(t.sipFlows, *flow)

	// Sort by month
	sort.Slice(t.sipFlows, func(i, j int) bool {
		return t.sipFlows[i].Month.Before(t.sipFlows[j].Month)
	})
}

// GetSIPFlows returns SIP flow data.
func (t *MFFlowTracker) GetSIPFlows(months int) []SIPFlowData {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if months >= len(t.sipFlows) {
		result := make([]SIPFlowData, len(t.sipFlows))
		copy(result, t.sipFlows)
		return result
	}

	result := make([]SIPFlowData, months)
	copy(result, t.sipFlows[len(t.sipFlows)-months:])
	return result
}

// GetLatestSIPFlow returns the most recent SIP flow data.
func (t *MFFlowTracker) GetLatestSIPFlow() *SIPFlowData {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.sipFlows) == 0 {
		return nil
	}

	return &t.sipFlows[len(t.sipFlows)-1]
}

// checkHoldingChange checks for significant MF holding changes.
func (t *MFFlowTracker) checkHoldingChange(symbol string) {
	holdings := t.holdings[symbol]
	if len(holdings) < 2 {
		return
	}

	prev := holdings[len(holdings)-2]
	curr := holdings[len(holdings)-1]
	change := curr.MFPercent - prev.MFPercent

	if change >= 2.0 {
		t.alerts = append(t.alerts, MFAlert{
			Symbol:    symbol,
			AlertType: "MF_INCREASE",
			Message:   fmt.Sprintf("MF holding increased by %.2f%% to %.2f%%", change, curr.MFPercent),
			Severity:  "INFO",
			Timestamp: time.Now(),
		})
	} else if change <= -2.0 {
		t.alerts = append(t.alerts, MFAlert{
			Symbol:    symbol,
			AlertType: "MF_DECREASE",
			Message:   fmt.Sprintf("MF holding decreased by %.2f%% to %.2f%%", -change, curr.MFPercent),
			Severity:  "WARNING",
			Timestamp: time.Now(),
		})
	}

	// Check for new entry
	if prev.MFPercent == 0 && curr.MFPercent > 0 {
		t.alerts = append(t.alerts, MFAlert{
			Symbol:    symbol,
			AlertType: "MF_NEW_ENTRY",
			Message:   fmt.Sprintf("MFs have entered %s with %.2f%% holding", symbol, curr.MFPercent),
			Severity:  "INFO",
			Timestamp: time.Now(),
		})
	}

	// Check for exit
	if prev.MFPercent > 0 && curr.MFPercent == 0 {
		t.alerts = append(t.alerts, MFAlert{
			Symbol:    symbol,
			AlertType: "MF_EXIT",
			Message:   fmt.Sprintf("MFs have exited %s completely", symbol),
			Severity:  "WARNING",
			Timestamp: time.Now(),
		})
	}
}

// GetAlerts returns MF alerts.
func (t *MFFlowTracker) GetAlerts() []MFAlert {
	t.mu.RLock()
	defer t.mu.RUnlock()

	alerts := make([]MFAlert, len(t.alerts))
	copy(alerts, t.alerts)
	return alerts
}

// ClearAlerts clears all MF alerts.
func (t *MFFlowTracker) ClearAlerts() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.alerts = make([]MFAlert, 0)
}

// GetMFBuyingStocks returns stocks where MFs are increasing holdings.
func (t *MFFlowTracker) GetMFBuyingStocks(quarters int) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var buying []string

	for symbol := range t.holdings {
		change := t.GetHoldingChange(symbol, quarters)
		if change > 0 {
			buying = append(buying, symbol)
		}
	}

	return buying
}

// GetMFSellingStocks returns stocks where MFs are decreasing holdings.
func (t *MFFlowTracker) GetMFSellingStocks(quarters int) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var selling []string

	for symbol := range t.holdings {
		change := t.GetHoldingChange(symbol, quarters)
		if change < 0 {
			selling = append(selling, symbol)
		}
	}

	return selling
}

// GetTopSchemesByAUM returns top MF schemes by AUM in a stock.
func (t *MFFlowTracker) GetTopSchemesByAUM(symbol string, limit int) []MFSchemeHolding {
	holdings := t.GetSchemeHoldings(symbol)
	if len(holdings) > limit {
		holdings = holdings[:limit]
	}
	return holdings
}
