// Package trading provides trading operations and utilities.
package trading

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

// ExpiryType represents types of expiry.
type ExpiryType string

const (
	ExpiryWeekly  ExpiryType = "WEEKLY"
	ExpiryMonthly ExpiryType = "MONTHLY"
)

// ExpiryInfo represents expiry information.
type ExpiryInfo struct {
	Date       time.Time
	Type       ExpiryType
	Symbol     string // Underlying symbol
	DaysToExp  int
	IsToday    bool
	IsTomorrow bool
}

// ExpiryPosition represents an F&O position with expiry details.
type ExpiryPosition struct {
	Symbol           string
	InstrumentType   string // FUT, CE, PE
	Strike           float64
	Expiry           time.Time
	Quantity         int
	AveragePrice     float64
	LTP              float64
	PnL              float64
	DaysToExpiry     int
	IsITM            bool
	PhysicalDelivery bool
	DeliveryMargin   float64
}

// RolloverSuggestion represents a rollover suggestion.
type RolloverSuggestion struct {
	CurrentPosition ExpiryPosition
	NextExpiry      time.Time
	RollCost        float64 // Cost to roll (basis difference)
	Recommendation  string
}

// ExpiryManager manages F&O expiry tracking for Indian markets.
type ExpiryManager struct {
	broker         broker.Broker
	sessionManager *SessionManager
	expiries       map[string][]time.Time // symbol -> expiry dates
	mu             sync.RWMutex
}

// NewExpiryManager creates a new expiry manager.
func NewExpiryManager(b broker.Broker, sm *SessionManager) *ExpiryManager {
	return &ExpiryManager{
		broker:         b,
		sessionManager: sm,
		expiries:       make(map[string][]time.Time),
	}
}

// SetExpiries sets expiry dates for a symbol.
func (m *ExpiryManager) SetExpiries(symbol string, expiries []time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Sort expiries
	sort.Slice(expiries, func(i, j int) bool {
		return expiries[i].Before(expiries[j])
	})
	m.expiries[symbol] = expiries
}

// GetExpiries returns expiry dates for a symbol.
func (m *ExpiryManager) GetExpiries(symbol string) []time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()

	expiries, ok := m.expiries[symbol]
	if !ok {
		return nil
	}

	result := make([]time.Time, len(expiries))
	copy(result, expiries)
	return result
}

// GetNextExpiry returns the next expiry date for a symbol.
func (m *ExpiryManager) GetNextExpiry(symbol string) *time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()

	expiries, ok := m.expiries[symbol]
	if !ok || len(expiries) == 0 {
		return nil
	}

	now := time.Now()
	for _, exp := range expiries {
		if exp.After(now) {
			return &exp
		}
	}
	return nil
}

// GetWeeklyExpiry returns the next weekly expiry (Thursday).
func (m *ExpiryManager) GetWeeklyExpiry() time.Time {
	now := time.Now()
	daysUntilThursday := (int(time.Thursday) - int(now.Weekday()) + 7) % 7
	if daysUntilThursday == 0 && now.Hour() >= 15 {
		daysUntilThursday = 7 // If it's Thursday after market close, get next Thursday
	}
	return time.Date(now.Year(), now.Month(), now.Day()+daysUntilThursday, 15, 30, 0, 0, now.Location())
}

// GetMonthlyExpiry returns the last Thursday of the current month.
func (m *ExpiryManager) GetMonthlyExpiry() time.Time {
	now := time.Now()
	// Get first day of next month
	firstOfNextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
	// Go back to last day of current month
	lastDay := firstOfNextMonth.AddDate(0, 0, -1)

	// Find last Thursday
	for lastDay.Weekday() != time.Thursday {
		lastDay = lastDay.AddDate(0, 0, -1)
	}

	return time.Date(lastDay.Year(), lastDay.Month(), lastDay.Day(), 15, 30, 0, 0, now.Location())
}

// GetExpiryInfo returns detailed expiry information.
func (m *ExpiryManager) GetExpiryInfo(symbol string, expiry time.Time) *ExpiryInfo {
	now := time.Now()
	daysToExp := int(expiry.Sub(now).Hours() / 24)

	expiryType := ExpiryMonthly
	// Check if it's a weekly expiry (not last Thursday of month)
	monthlyExpiry := m.GetMonthlyExpiry()
	if !sameDay(expiry, monthlyExpiry) {
		expiryType = ExpiryWeekly
	}

	return &ExpiryInfo{
		Date:       expiry,
		Type:       expiryType,
		Symbol:     symbol,
		DaysToExp:  daysToExp,
		IsToday:    sameDay(expiry, now),
		IsTomorrow: sameDay(expiry, now.AddDate(0, 0, 1)),
	}
}

// GetExpiryPositions returns F&O positions with expiry details.
func (m *ExpiryManager) GetExpiryPositions(ctx context.Context) ([]ExpiryPosition, error) {
	positions, err := m.broker.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	var expiryPositions []ExpiryPosition
	now := time.Now()

	for _, p := range positions {
		// Only F&O positions
		if p.Exchange != models.NFO {
			continue
		}

		// Parse instrument details (simplified - real implementation would use instrument master)
		instrType := "FUT"
		var strike float64
		var expiry time.Time

		// Calculate days to expiry
		daysToExpiry := int(expiry.Sub(now).Hours() / 24)

		// Check if ITM (simplified)
		isITM := false
		physicalDelivery := false
		deliveryMargin := 0.0

		// Stock options have physical delivery
		if instrType == "CE" || instrType == "PE" {
			physicalDelivery = true
			// Delivery margin is typically full value of underlying
			deliveryMargin = p.LTP * float64(p.Quantity) * float64(p.Multiplier)
		}

		expiryPositions = append(expiryPositions, ExpiryPosition{
			Symbol:           p.Symbol,
			InstrumentType:   instrType,
			Strike:           strike,
			Expiry:           expiry,
			Quantity:         p.Quantity,
			AveragePrice:     p.AveragePrice,
			LTP:              p.LTP,
			PnL:              p.PnL,
			DaysToExpiry:     daysToExpiry,
			IsITM:            isITM,
			PhysicalDelivery: physicalDelivery,
			DeliveryMargin:   deliveryMargin,
		})
	}

	return expiryPositions, nil
}

// GetExpiringPositions returns positions expiring within N days.
func (m *ExpiryManager) GetExpiringPositions(ctx context.Context, days int) ([]ExpiryPosition, error) {
	positions, err := m.GetExpiryPositions(ctx)
	if err != nil {
		return nil, err
	}

	var expiring []ExpiryPosition
	for _, p := range positions {
		if p.DaysToExpiry <= days {
			expiring = append(expiring, p)
		}
	}

	return expiring, nil
}

// GetExpiryDayWarnings returns warnings for expiry day positions.
func (m *ExpiryManager) GetExpiryDayWarnings(ctx context.Context) ([]string, error) {
	positions, err := m.GetExpiryPositions(ctx)
	if err != nil {
		return nil, err
	}

	var warnings []string
	for _, p := range positions {
		if p.DaysToExpiry == 0 {
			if p.IsITM && p.PhysicalDelivery {
				warnings = append(warnings, fmt.Sprintf(
					"⚠️ %s is ITM and expires today. Physical delivery margin of ₹%.2f required.",
					p.Symbol, p.DeliveryMargin))
			} else if p.IsITM {
				warnings = append(warnings, fmt.Sprintf(
					"⚠️ %s is ITM and expires today. Auto square-off at 3:00 PM.",
					p.Symbol))
			}
		} else if p.DaysToExpiry == 1 {
			warnings = append(warnings, fmt.Sprintf(
				"ℹ️ %s expires tomorrow. Consider rolling or closing position.",
				p.Symbol))
		}
	}

	return warnings, nil
}

// GetAutoSquareOffTime returns auto square-off time for ITM options.
func (m *ExpiryManager) GetAutoSquareOffTime() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 15, 0, 0, 0, now.Location())
}

// SuggestRollover suggests rollover for expiring positions.
func (m *ExpiryManager) SuggestRollover(ctx context.Context, position ExpiryPosition) (*RolloverSuggestion, error) {
	// Get next expiry
	nextExpiry := m.GetNextExpiry(position.Symbol)
	if nextExpiry == nil {
		return nil, fmt.Errorf("no next expiry found for %s", position.Symbol)
	}

	// Calculate roll cost (simplified - would need actual futures prices)
	rollCost := 0.0 // Basis difference between current and next month

	recommendation := "HOLD"
	if position.DaysToExpiry <= 3 {
		recommendation = "ROLL"
	}

	return &RolloverSuggestion{
		CurrentPosition: position,
		NextExpiry:      *nextExpiry,
		RollCost:        rollCost,
		Recommendation:  recommendation,
	}, nil
}

// GetPhysicalDeliveryMargin calculates physical delivery margin requirement.
func (m *ExpiryManager) GetPhysicalDeliveryMargin(ctx context.Context, position ExpiryPosition) float64 {
	if !position.PhysicalDelivery {
		return 0
	}

	// Physical delivery margin is typically the full value of underlying
	// For options, it's strike * lot size
	return position.Strike * float64(position.Quantity) * float64(1) // Multiplier
}

// GetExpiryDayPnL returns P&L for positions that expired on a specific date.
func (m *ExpiryManager) GetExpiryDayPnL(positions []ExpiryPosition, expiryDate time.Time) float64 {
	var totalPnL float64
	for _, p := range positions {
		if sameDay(p.Expiry, expiryDate) {
			totalPnL += p.PnL
		}
	}
	return totalPnL
}

// GetUpcomingExpiries returns upcoming expiry dates.
func (m *ExpiryManager) GetUpcomingExpiries(days int) []ExpiryInfo {
	now := time.Now()
	cutoff := now.AddDate(0, 0, days)

	var expiries []ExpiryInfo

	// Add weekly expiries
	weekly := m.GetWeeklyExpiry()
	for weekly.Before(cutoff) {
		expiries = append(expiries, *m.GetExpiryInfo("NIFTY", weekly))
		weekly = weekly.AddDate(0, 0, 7)
	}

	// Sort by date
	sort.Slice(expiries, func(i, j int) bool {
		return expiries[i].Date.Before(expiries[j].Date)
	})

	return expiries
}

// IsExpiryDay checks if today is an expiry day.
func (m *ExpiryManager) IsExpiryDay() bool {
	now := time.Now()
	return now.Weekday() == time.Thursday
}

// GetTimeToExpiry returns time remaining until expiry.
func (m *ExpiryManager) GetTimeToExpiry(expiry time.Time) time.Duration {
	now := time.Now()
	if expiry.Before(now) {
		return 0
	}
	return expiry.Sub(now)
}

// sameDay checks if two times are on the same day.
func sameDay(t1, t2 time.Time) bool {
	y1, m1, d1 := t1.Date()
	y2, m2, d2 := t2.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}
