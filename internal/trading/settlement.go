// Package trading provides trading operations and utilities.
package trading

import (
	"context"
	"fmt"
	"sync"
	"time"

	"zerodha-trader/internal/broker"
)

// SettlementStatus represents the settlement status of shares.
type SettlementStatus string

const (
	SettlementPending   SettlementStatus = "PENDING"
	SettlementInTransit SettlementStatus = "IN_TRANSIT"
	SettlementSettled   SettlementStatus = "SETTLED"
)

// UnsettledHolding represents shares that are not yet settled.
type UnsettledHolding struct {
	Symbol          string
	Quantity        int
	BuyDate         time.Time
	SettlementDate  time.Time
	Status          SettlementStatus
	AvailableForSell bool
	BTSTAvailable   bool
	DeliveryMargin  float64
}

// SettlementInfo represents settlement information.
type SettlementInfo struct {
	TradeDate       time.Time
	SettlementDate  time.Time
	DaysToSettle    int
	SettlementType  string // T+1
}

// BTSTInfo represents BTST (Buy Today Sell Tomorrow) information.
type BTSTInfo struct {
	Symbol           string
	Quantity         int
	BuyPrice         float64
	CanSellBTST      bool
	BTSTMargin       float64 // Additional margin for BTST
	ShortDeliveryRisk bool
}

// SettlementTracker tracks settlement for Indian markets (T+1).
type SettlementTracker struct {
	broker          broker.Broker
	sessionManager  *SessionManager
	unsettled       map[string][]UnsettledHolding // symbol -> unsettled holdings
	mu              sync.RWMutex
}

// NewSettlementTracker creates a new settlement tracker.
func NewSettlementTracker(b broker.Broker, sm *SessionManager) *SettlementTracker {
	return &SettlementTracker{
		broker:         b,
		sessionManager: sm,
		unsettled:      make(map[string][]UnsettledHolding),
	}
}

// CalculateSettlementDate calculates T+1 settlement date.
func (t *SettlementTracker) CalculateSettlementDate(tradeDate time.Time) time.Time {
	// T+1 settlement - next trading day
	settlement := tradeDate.AddDate(0, 0, 1)

	// Skip weekends
	for settlement.Weekday() == time.Saturday || settlement.Weekday() == time.Sunday {
		settlement = settlement.AddDate(0, 0, 1)
	}

	// Skip holidays
	for t.sessionManager.IsHoliday(settlement) {
		settlement = settlement.AddDate(0, 0, 1)
		// Skip weekends again
		for settlement.Weekday() == time.Saturday || settlement.Weekday() == time.Sunday {
			settlement = settlement.AddDate(0, 0, 1)
		}
	}

	return settlement
}

// GetSettlementInfo returns settlement information for a trade date.
func (t *SettlementTracker) GetSettlementInfo(tradeDate time.Time) *SettlementInfo {
	settlementDate := t.CalculateSettlementDate(tradeDate)
	daysToSettle := int(settlementDate.Sub(tradeDate).Hours() / 24)

	return &SettlementInfo{
		TradeDate:      tradeDate,
		SettlementDate: settlementDate,
		DaysToSettle:   daysToSettle,
		SettlementType: "T+1",
	}
}

// RecordBuy records a buy trade for settlement tracking.
func (t *SettlementTracker) RecordBuy(symbol string, quantity int, price float64, tradeDate time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	settlementDate := t.CalculateSettlementDate(tradeDate)
	now := time.Now()

	status := SettlementPending
	if now.After(settlementDate) {
		status = SettlementSettled
	} else if now.After(tradeDate) {
		status = SettlementInTransit
	}

	holding := UnsettledHolding{
		Symbol:           symbol,
		Quantity:         quantity,
		BuyDate:          tradeDate,
		SettlementDate:   settlementDate,
		Status:           status,
		AvailableForSell: status == SettlementSettled,
		BTSTAvailable:    status == SettlementInTransit,
		DeliveryMargin:   price * float64(quantity),
	}

	t.unsettled[symbol] = append(t.unsettled[symbol], holding)
}

// GetUnsettledHoldings returns all unsettled holdings.
func (t *SettlementTracker) GetUnsettledHoldings() []UnsettledHolding {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var all []UnsettledHolding
	now := time.Now()

	for _, holdings := range t.unsettled {
		for _, h := range holdings {
			// Update status based on current time
			if now.After(h.SettlementDate) {
				h.Status = SettlementSettled
				h.AvailableForSell = true
				h.BTSTAvailable = false
			} else if now.After(h.BuyDate) {
				h.Status = SettlementInTransit
				h.BTSTAvailable = true
			}

			if h.Status != SettlementSettled {
				all = append(all, h)
			}
		}
	}

	return all
}

// GetUnsettledForSymbol returns unsettled holdings for a symbol.
func (t *SettlementTracker) GetUnsettledForSymbol(symbol string) []UnsettledHolding {
	t.mu.RLock()
	defer t.mu.RUnlock()

	holdings, ok := t.unsettled[symbol]
	if !ok {
		return nil
	}

	var unsettled []UnsettledHolding
	now := time.Now()

	for _, h := range holdings {
		if now.After(h.SettlementDate) {
			continue // Already settled
		}
		unsettled = append(unsettled, h)
	}

	return unsettled
}

// GetBTSTAvailability returns BTST availability for a symbol.
func (t *SettlementTracker) GetBTSTAvailability(ctx context.Context, symbol string) (*BTSTInfo, error) {
	t.mu.RLock()
	holdings, ok := t.unsettled[symbol]
	t.mu.RUnlock()

	if !ok || len(holdings) == 0 {
		return &BTSTInfo{
			Symbol:      symbol,
			CanSellBTST: false,
		}, nil
	}

	now := time.Now()
	var btstQty int
	var totalValue float64

	for _, h := range holdings {
		// BTST available if bought yesterday and not yet settled
		if h.Status == SettlementInTransit || (now.After(h.BuyDate) && now.Before(h.SettlementDate)) {
			btstQty += h.Quantity
			totalValue += h.DeliveryMargin
		}
	}

	// BTST margin is typically 40% of trade value
	btstMargin := totalValue * 0.40

	return &BTSTInfo{
		Symbol:            symbol,
		Quantity:          btstQty,
		CanSellBTST:       btstQty > 0,
		BTSTMargin:        btstMargin,
		ShortDeliveryRisk: true, // BTST always has short delivery risk
	}, nil
}

// GetDeliveryMarginRequired returns delivery margin required for unsettled holdings.
func (t *SettlementTracker) GetDeliveryMarginRequired() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var totalMargin float64
	now := time.Now()

	for _, holdings := range t.unsettled {
		for _, h := range holdings {
			if now.Before(h.SettlementDate) {
				totalMargin += h.DeliveryMargin
			}
		}
	}

	return totalMargin
}

// CheckShortDeliveryRisk checks if selling would cause short delivery.
func (t *SettlementTracker) CheckShortDeliveryRisk(ctx context.Context, symbol string, sellQty int) (bool, string) {
	// Get current holdings from broker
	holdings, err := t.broker.GetHoldings(ctx)
	if err != nil {
		return true, "Unable to verify holdings"
	}

	var settledQty int
	for _, h := range holdings {
		if h.Symbol == symbol {
			settledQty = h.Quantity
			break
		}
	}

	// Get unsettled quantity
	unsettled := t.GetUnsettledForSymbol(symbol)
	var unsettledQty int
	for _, u := range unsettled {
		unsettledQty += u.Quantity
	}

	// Total available = settled + unsettled (BTST)
	totalAvailable := settledQty + unsettledQty

	if sellQty > totalAvailable {
		return true, fmt.Sprintf("Selling %d shares but only %d available (settled: %d, BTST: %d)",
			sellQty, totalAvailable, settledQty, unsettledQty)
	}

	// If selling more than settled, it's BTST with short delivery risk
	if sellQty > settledQty {
		btstQty := sellQty - settledQty
		return true, fmt.Sprintf("BTST sale of %d shares - short delivery risk if buyer doesn't deliver", btstQty)
	}

	return false, ""
}

// CleanupSettled removes settled holdings from tracking.
func (t *SettlementTracker) CleanupSettled() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()

	for symbol, holdings := range t.unsettled {
		var pending []UnsettledHolding
		for _, h := range holdings {
			if now.Before(h.SettlementDate) {
				pending = append(pending, h)
			}
		}
		if len(pending) > 0 {
			t.unsettled[symbol] = pending
		} else {
			delete(t.unsettled, symbol)
		}
	}
}

// GetSettlementCalendar returns settlement dates for next N trading days.
func (t *SettlementTracker) GetSettlementCalendar(days int) []SettlementInfo {
	var calendar []SettlementInfo
	current := time.Now()

	for i := 0; i < days; i++ {
		// Skip weekends and holidays
		for current.Weekday() == time.Saturday || current.Weekday() == time.Sunday || t.sessionManager.IsHoliday(current) {
			current = current.AddDate(0, 0, 1)
		}

		info := t.GetSettlementInfo(current)
		calendar = append(calendar, *info)
		current = current.AddDate(0, 0, 1)
	}

	return calendar
}
