// Package trading provides trading operations and utilities.
package trading

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// PromoterHolding represents promoter holding data.
type PromoterHolding struct {
	Symbol           string
	Quarter          string // e.g., "Q3FY24"
	Date             time.Time
	PromoterPercent  float64
	PublicPercent    float64
	DIIPercent       float64
	FIIPercent       float64
	PledgePercent    float64 // Percentage of promoter holding pledged
	PledgeValue      float64
}

// SASTDisclosure represents SAST (Substantial Acquisition of Shares) disclosure.
type SASTDisclosure struct {
	Symbol          string
	Date            time.Time
	AcquirerName    string
	TransactionType string // ACQUISITION, DISPOSAL
	SharesBefore    float64
	SharesAcquired  int64
	SharesAfter     float64
	Mode            string // MARKET, OFF_MARKET, OPEN_OFFER
}

// InsiderTrade represents insider/KMP trade.
type InsiderTrade struct {
	Symbol          string
	Date            time.Time
	PersonName      string
	Designation     string // MD, CFO, CS, etc.
	TransactionType string // BUY, SELL
	Quantity        int64
	Price           float64
	Value           float64
}

// PromoterAlert represents an alert for promoter activity.
type PromoterAlert struct {
	Symbol    string
	AlertType string
	Message   string
	Severity  string
	Timestamp time.Time
}

// PromoterTracker tracks promoter holdings and insider trades.
type PromoterTracker struct {
	holdings     map[string][]PromoterHolding // symbol -> quarterly holdings
	disclosures  []SASTDisclosure
	insiderTrades []InsiderTrade
	alerts       []PromoterAlert
	mu           sync.RWMutex
}

// NewPromoterTracker creates a new promoter tracker.
func NewPromoterTracker() *PromoterTracker {
	return &PromoterTracker{
		holdings:      make(map[string][]PromoterHolding),
		disclosures:   make([]SASTDisclosure, 0),
		insiderTrades: make([]InsiderTrade, 0),
		alerts:        make([]PromoterAlert, 0),
	}
}

// AddHolding adds promoter holding data.
func (t *PromoterTracker) AddHolding(holding *PromoterHolding) {
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

// GetCurrentHolding returns the most recent promoter holding.
func (t *PromoterTracker) GetCurrentHolding(symbol string) *PromoterHolding {
	t.mu.RLock()
	defer t.mu.RUnlock()

	holdings, ok := t.holdings[symbol]
	if !ok || len(holdings) == 0 {
		return nil
	}

	return &holdings[len(holdings)-1]
}

// GetHoldingHistory returns promoter holding history.
func (t *PromoterTracker) GetHoldingHistory(symbol string, quarters int) []PromoterHolding {
	t.mu.RLock()
	defer t.mu.RUnlock()

	holdings, ok := t.holdings[symbol]
	if !ok {
		return nil
	}

	if quarters >= len(holdings) {
		result := make([]PromoterHolding, len(holdings))
		copy(result, holdings)
		return result
	}

	result := make([]PromoterHolding, quarters)
	copy(result, holdings[len(holdings)-quarters:])
	return result
}

// GetHoldingChange returns change in promoter holding over quarters.
func (t *PromoterTracker) GetHoldingChange(symbol string, quarters int) float64 {
	history := t.GetHoldingHistory(symbol, quarters)
	if len(history) < 2 {
		return 0
	}

	oldest := history[0].PromoterPercent
	latest := history[len(history)-1].PromoterPercent
	return latest - oldest
}

// AddSASTDisclosure adds a SAST disclosure.
func (t *PromoterTracker) AddSASTDisclosure(disclosure *SASTDisclosure) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.disclosures = append(t.disclosures, *disclosure)

	// Generate alert for significant acquisition/disposal
	if disclosure.SharesAcquired > 0 {
		changePercent := disclosure.SharesAfter - disclosure.SharesBefore
		if changePercent >= 1.0 || changePercent <= -1.0 {
			t.alerts = append(t.alerts, PromoterAlert{
				Symbol:    disclosure.Symbol,
				AlertType: "SAST",
				Message: fmt.Sprintf("%s %s %.2f%% stake in %s",
					disclosure.AcquirerName, disclosure.TransactionType, 
					absFloat(changePercent), disclosure.Symbol),
				Severity:  "INFO",
				Timestamp: time.Now(),
			})
		}
	}
}

// GetSASTDisclosures returns SAST disclosures for a symbol.
func (t *PromoterTracker) GetSASTDisclosures(symbol string) []SASTDisclosure {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var disclosures []SASTDisclosure
	for _, d := range t.disclosures {
		if d.Symbol == symbol {
			disclosures = append(disclosures, d)
		}
	}

	sort.Slice(disclosures, func(i, j int) bool {
		return disclosures[i].Date.After(disclosures[j].Date)
	})

	return disclosures
}

// AddInsiderTrade adds an insider trade.
func (t *PromoterTracker) AddInsiderTrade(trade *InsiderTrade) {
	t.mu.Lock()
	defer t.mu.Unlock()

	trade.Value = float64(trade.Quantity) * trade.Price
	t.insiderTrades = append(t.insiderTrades, *trade)

	// Generate alert for significant trades
	if trade.Value >= 1000000 { // 10 lakh
		t.alerts = append(t.alerts, PromoterAlert{
			Symbol:    trade.Symbol,
			AlertType: "INSIDER",
			Message: fmt.Sprintf("%s (%s) %s %d shares of %s at ₹%.2f",
				trade.PersonName, trade.Designation, trade.TransactionType,
				trade.Quantity, trade.Symbol, trade.Price),
			Severity:  "INFO",
			Timestamp: time.Now(),
		})
	}
}

// GetInsiderTrades returns insider trades for a symbol.
func (t *PromoterTracker) GetInsiderTrades(symbol string, days int) []InsiderTrade {
	t.mu.RLock()
	defer t.mu.RUnlock()

	cutoff := time.Now().AddDate(0, 0, -days)
	var trades []InsiderTrade

	for _, trade := range t.insiderTrades {
		if trade.Symbol == symbol && trade.Date.After(cutoff) {
			trades = append(trades, trade)
		}
	}

	sort.Slice(trades, func(i, j int) bool {
		return trades[i].Date.After(trades[j].Date)
	})

	return trades
}

// GetPledgePercent returns pledge percentage for a symbol.
func (t *PromoterTracker) GetPledgePercent(symbol string) float64 {
	holding := t.GetCurrentHolding(symbol)
	if holding == nil {
		return 0
	}
	return holding.PledgePercent
}

// GetHighPledgeStocks returns stocks with high pledge percentage.
func (t *PromoterTracker) GetHighPledgeStocks(minPledge float64) []PromoterHolding {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []PromoterHolding

	for symbol, holdings := range t.holdings {
		if len(holdings) == 0 {
			continue
		}
		latest := holdings[len(holdings)-1]
		if latest.PledgePercent >= minPledge {
			result = append(result, latest)
		}
		_ = symbol // Avoid unused variable warning
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].PledgePercent > result[j].PledgePercent
	})

	return result
}

// checkHoldingChange checks for significant holding changes.
func (t *PromoterTracker) checkHoldingChange(symbol string) {
	holdings := t.holdings[symbol]
	if len(holdings) < 2 {
		return
	}

	prev := holdings[len(holdings)-2]
	curr := holdings[len(holdings)-1]
	change := curr.PromoterPercent - prev.PromoterPercent

	if change >= 2.0 {
		t.alerts = append(t.alerts, PromoterAlert{
			Symbol:    symbol,
			AlertType: "HOLDING_INCREASE",
			Message:   fmt.Sprintf("Promoter holding increased by %.2f%% to %.2f%%", change, curr.PromoterPercent),
			Severity:  "INFO",
			Timestamp: time.Now(),
		})
	} else if change <= -2.0 {
		t.alerts = append(t.alerts, PromoterAlert{
			Symbol:    symbol,
			AlertType: "HOLDING_DECREASE",
			Message:   fmt.Sprintf("Promoter holding decreased by %.2f%% to %.2f%%", -change, curr.PromoterPercent),
			Severity:  "WARNING",
			Timestamp: time.Now(),
		})
	}

	// Check pledge increase
	pledgeChange := curr.PledgePercent - prev.PledgePercent
	if pledgeChange >= 5.0 {
		t.alerts = append(t.alerts, PromoterAlert{
			Symbol:    symbol,
			AlertType: "PLEDGE_INCREASE",
			Message:   fmt.Sprintf("Promoter pledge increased by %.2f%% to %.2f%%", pledgeChange, curr.PledgePercent),
			Severity:  "WARNING",
			Timestamp: time.Now(),
		})
	}
}

// GetAlerts returns promoter alerts.
func (t *PromoterTracker) GetAlerts() []PromoterAlert {
	t.mu.RLock()
	defer t.mu.RUnlock()

	alerts := make([]PromoterAlert, len(t.alerts))
	copy(alerts, t.alerts)
	return alerts
}

// ClearAlerts clears all promoter alerts.
func (t *PromoterTracker) ClearAlerts() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.alerts = make([]PromoterAlert, 0)
}

// GetPromoterBuyingStocks returns stocks where promoters are buying.
func (t *PromoterTracker) GetPromoterBuyingStocks(quarters int) []string {
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

// GetPromoterSellingStocks returns stocks where promoters are selling.
func (t *PromoterTracker) GetPromoterSellingStocks(quarters int) []string {
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

func absFloat(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
