// Package trading provides trading operations and utilities.
package trading

import (
	"context"
	"fmt"
	"sync"
	"time"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

// MarginManager handles margin calculations and tracking for Indian markets.
type MarginManager struct {
	broker       broker.Broker
	multipliers  map[string]float64 // MIS margin multipliers by symbol
	peakMargins  []PeakMarginRecord
	mu           sync.RWMutex
	lastUpdate   time.Time
}

// PeakMarginRecord tracks peak margin usage (SEBI requirement).
type PeakMarginRecord struct {
	Timestamp     time.Time
	MarginUsed    float64
	MarginAvail   float64
	Utilization   float64
	Segment       string
}

// MarginRequirement represents margin requirement for an order.
type MarginRequirement struct {
	Symbol          string
	Exchange        models.Exchange
	Product         models.ProductType
	Quantity        int
	Price           float64
	RequiredMargin  float64
	AvailableMargin float64
	Shortfall       float64
	MISMultiplier   float64
	VARMargin       float64
	ELMMargin       float64
	ExposureMargin  float64
	SpanMargin      float64 // For F&O
	DeliveryMargin  float64 // For delivery trades
}

// MarginUtilization represents current margin utilization.
type MarginUtilization struct {
	Segment         string
	TotalMargin     float64
	UsedMargin      float64
	AvailableMargin float64
	Utilization     float64
	CollateralValue float64
	CashBalance     float64
}

// WhatIfResult represents result of margin what-if calculation.
type WhatIfResult struct {
	Symbol           string
	Side             models.OrderSide
	Quantity         int
	Price            float64
	RequiredMargin   float64
	PostTradeMargin  float64
	PostTradeAvail   float64
	CanExecute       bool
	ShortfallAmount  float64
}

// NewMarginManager creates a new margin manager.
func NewMarginManager(b broker.Broker) *MarginManager {
	return &MarginManager{
		broker:      b,
		multipliers: make(map[string]float64),
		peakMargins: make([]PeakMarginRecord, 0),
	}
}

// GetMISMultiplier returns the MIS margin multiplier for a symbol.
// Default multipliers based on Zerodha's typical values.
func (m *MarginManager) GetMISMultiplier(symbol string, exchange models.Exchange) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", exchange, symbol)
	if mult, ok := m.multipliers[key]; ok {
		return mult
	}

	// Default multipliers based on exchange/segment
	switch exchange {
	case models.NFO:
		return 1.0 // F&O typically requires full margin
	case models.MCX:
		return 1.0 // Commodity requires full margin
	case models.CDS:
		return 1.0 // Currency requires full margin
	default:
		// Equity MIS typically gets 5x leverage (20% margin)
		return 5.0
	}
}

// SetMISMultiplier sets the MIS margin multiplier for a symbol.
func (m *MarginManager) SetMISMultiplier(symbol string, exchange models.Exchange, multiplier float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s:%s", exchange, symbol)
	m.multipliers[key] = multiplier
}

// CalculateRequiredMargin calculates margin required for an order.
func (m *MarginManager) CalculateRequiredMargin(ctx context.Context, order *models.Order) (*MarginRequirement, error) {
	// Get current margins
	margins, err := m.broker.GetMargins(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get margins: %w", err)
	}

	// Get quote for current price if not provided
	price := order.Price
	if price == 0 {
		quote, err := m.broker.GetQuote(ctx, fmt.Sprintf("%s:%s", order.Exchange, order.Symbol))
		if err != nil {
			return nil, fmt.Errorf("failed to get quote: %w", err)
		}
		price = quote.LTP
	}

	orderValue := price * float64(order.Quantity)
	var requiredMargin float64

	switch order.Product {
	case models.ProductCNC:
		// Delivery requires full value
		requiredMargin = orderValue
	case models.ProductMIS:
		// MIS gets leverage based on multiplier
		multiplier := m.GetMISMultiplier(order.Symbol, order.Exchange)
		if multiplier > 0 {
			requiredMargin = orderValue / multiplier
		} else {
			requiredMargin = orderValue
		}
	case models.ProductNRML:
		// F&O normal margin - typically SPAN + Exposure
		// Simplified calculation - actual would use SPAN calculator
		requiredMargin = orderValue * 0.15 // ~15% for F&O
	default:
		requiredMargin = orderValue
	}

	// Get available margin based on segment
	var availableMargin float64
	switch order.Exchange {
	case models.MCX:
		availableMargin = margins.Commodity.Available
	default:
		availableMargin = margins.Equity.Available
	}

	shortfall := 0.0
	if requiredMargin > availableMargin {
		shortfall = requiredMargin - availableMargin
	}

	return &MarginRequirement{
		Symbol:          order.Symbol,
		Exchange:        order.Exchange,
		Product:         order.Product,
		Quantity:        order.Quantity,
		Price:           price,
		RequiredMargin:  requiredMargin,
		AvailableMargin: availableMargin,
		Shortfall:       shortfall,
		MISMultiplier:   m.GetMISMultiplier(order.Symbol, order.Exchange),
		DeliveryMargin:  orderValue,
	}, nil
}

// GetMarginUtilization returns current margin utilization by segment.
func (m *MarginManager) GetMarginUtilization(ctx context.Context) ([]MarginUtilization, error) {
	margins, err := m.broker.GetMargins(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get margins: %w", err)
	}

	balance, err := m.broker.GetBalance(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}

	utilizations := []MarginUtilization{
		{
			Segment:         "Equity",
			TotalMargin:     margins.Equity.Total,
			UsedMargin:      margins.Equity.Used,
			AvailableMargin: margins.Equity.Available,
			Utilization:     calculateUtilization(margins.Equity.Used, margins.Equity.Total),
			CollateralValue: balance.CollateralValue,
			CashBalance:     balance.AvailableCash,
		},
		{
			Segment:         "Commodity",
			TotalMargin:     margins.Commodity.Total,
			UsedMargin:      margins.Commodity.Used,
			AvailableMargin: margins.Commodity.Available,
			Utilization:     calculateUtilization(margins.Commodity.Used, margins.Commodity.Total),
		},
	}

	return utilizations, nil
}

// RecordPeakMargin records current margin usage for SEBI peak margin tracking.
func (m *MarginManager) RecordPeakMargin(ctx context.Context) error {
	margins, err := m.broker.GetMargins(ctx)
	if err != nil {
		return fmt.Errorf("failed to get margins: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Record equity segment
	if margins.Equity.Total > 0 {
		m.peakMargins = append(m.peakMargins, PeakMarginRecord{
			Timestamp:   time.Now(),
			MarginUsed:  margins.Equity.Used,
			MarginAvail: margins.Equity.Available,
			Utilization: calculateUtilization(margins.Equity.Used, margins.Equity.Total),
			Segment:     "Equity",
		})
	}

	// Record commodity segment
	if margins.Commodity.Total > 0 {
		m.peakMargins = append(m.peakMargins, PeakMarginRecord{
			Timestamp:   time.Now(),
			MarginUsed:  margins.Commodity.Used,
			MarginAvail: margins.Commodity.Available,
			Utilization: calculateUtilization(margins.Commodity.Used, margins.Commodity.Total),
			Segment:     "Commodity",
		})
	}

	// Keep only last 7 days of records
	cutoff := time.Now().AddDate(0, 0, -7)
	filtered := make([]PeakMarginRecord, 0)
	for _, r := range m.peakMargins {
		if r.Timestamp.After(cutoff) {
			filtered = append(filtered, r)
		}
	}
	m.peakMargins = filtered

	return nil
}

// GetPeakMargins returns peak margin records for a date range.
func (m *MarginManager) GetPeakMargins(from, to time.Time) []PeakMarginRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var records []PeakMarginRecord
	for _, r := range m.peakMargins {
		if (r.Timestamp.Equal(from) || r.Timestamp.After(from)) &&
			(r.Timestamp.Equal(to) || r.Timestamp.Before(to)) {
			records = append(records, r)
		}
	}
	return records
}

// GetPeakUtilization returns the peak margin utilization for a segment on a date.
func (m *MarginManager) GetPeakUtilization(date time.Time, segment string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	startOfDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	endOfDay := startOfDay.AddDate(0, 0, 1)

	var peak float64
	for _, r := range m.peakMargins {
		if r.Segment == segment &&
			r.Timestamp.After(startOfDay) &&
			r.Timestamp.Before(endOfDay) &&
			r.Utilization > peak {
			peak = r.Utilization
		}
	}
	return peak
}

// CheckMarginShortfall checks if there's a margin shortfall risk.
func (m *MarginManager) CheckMarginShortfall(ctx context.Context, threshold float64) (bool, float64, error) {
	utilizations, err := m.GetMarginUtilization(ctx)
	if err != nil {
		return false, 0, err
	}

	for _, u := range utilizations {
		if u.Utilization >= threshold {
			return true, u.Utilization, nil
		}
	}
	return false, 0, nil
}

// WhatIfMargin calculates margin impact of a hypothetical trade.
func (m *MarginManager) WhatIfMargin(ctx context.Context, symbol string, exchange models.Exchange, side models.OrderSide, quantity int, price float64, product models.ProductType) (*WhatIfResult, error) {
	// Get current margins
	margins, err := m.broker.GetMargins(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get margins: %w", err)
	}

	// Get price if not provided
	if price == 0 {
		quote, err := m.broker.GetQuote(ctx, fmt.Sprintf("%s:%s", exchange, symbol))
		if err != nil {
			return nil, fmt.Errorf("failed to get quote: %w", err)
		}
		price = quote.LTP
	}

	orderValue := price * float64(quantity)
	var requiredMargin float64

	switch product {
	case models.ProductCNC:
		requiredMargin = orderValue
	case models.ProductMIS:
		multiplier := m.GetMISMultiplier(symbol, exchange)
		if multiplier > 0 {
			requiredMargin = orderValue / multiplier
		} else {
			requiredMargin = orderValue
		}
	case models.ProductNRML:
		requiredMargin = orderValue * 0.15
	default:
		requiredMargin = orderValue
	}

	var currentAvail float64
	switch exchange {
	case models.MCX:
		currentAvail = margins.Commodity.Available
	default:
		currentAvail = margins.Equity.Available
	}

	postTradeAvail := currentAvail - requiredMargin
	canExecute := postTradeAvail >= 0
	shortfall := 0.0
	if !canExecute {
		shortfall = -postTradeAvail
	}

	return &WhatIfResult{
		Symbol:          symbol,
		Side:            side,
		Quantity:        quantity,
		Price:           price,
		RequiredMargin:  requiredMargin,
		PostTradeMargin: requiredMargin,
		PostTradeAvail:  postTradeAvail,
		CanExecute:      canExecute,
		ShortfallAmount: shortfall,
	}, nil
}

func calculateUtilization(used, total float64) float64 {
	if total == 0 {
		return 0
	}
	return (used / total) * 100
}
