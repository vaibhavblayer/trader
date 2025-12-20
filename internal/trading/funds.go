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

// FundSummary represents overall fund summary.
type FundSummary struct {
	AvailableCash   float64
	UsedMargin      float64
	TotalEquity     float64
	CollateralValue float64
	OpeningBalance  float64
	PayinCredit     float64
	PayoutDebit     float64
	Timestamp       time.Time
}

// SegmentFunds represents funds allocated to a segment.
type SegmentFunds struct {
	Segment         string
	AvailableMargin float64
	UsedMargin      float64
	TotalMargin     float64
	Utilization     float64
}

// FundAlert represents a fund-related alert.
type FundAlert struct {
	Type        string
	Message     string
	Severity    string // INFO, WARNING, CRITICAL
	Timestamp   time.Time
}

// FundManager manages fund tracking and alerts for Indian markets.
type FundManager struct {
	broker           broker.Broker
	lowBalanceThresh float64
	alerts           []FundAlert
	mu               sync.RWMutex
	lastFetch        time.Time
	cachedSummary    *FundSummary
}

// NewFundManager creates a new fund manager.
func NewFundManager(b broker.Broker) *FundManager {
	return &FundManager{
		broker:           b,
		lowBalanceThresh: 10000, // Default ₹10,000 threshold
		alerts:           make([]FundAlert, 0),
	}
}

// SetLowBalanceThreshold sets the low balance alert threshold.
func (f *FundManager) SetLowBalanceThreshold(threshold float64) {
	f.lowBalanceThresh = threshold
}

// GetFundSummary returns the current fund summary.
func (f *FundManager) GetFundSummary(ctx context.Context) (*FundSummary, error) {
	balance, err := f.broker.GetBalance(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}

	summary := &FundSummary{
		AvailableCash:   balance.AvailableCash,
		UsedMargin:      balance.UsedMargin,
		TotalEquity:     balance.TotalEquity,
		CollateralValue: balance.CollateralValue,
		Timestamp:       time.Now(),
	}

	// Cache the summary
	f.mu.Lock()
	f.cachedSummary = summary
	f.lastFetch = time.Now()
	f.mu.Unlock()

	// Check for alerts
	f.checkAlerts(summary)

	return summary, nil
}

// GetSegmentFunds returns fund allocation by segment.
func (f *FundManager) GetSegmentFunds(ctx context.Context) ([]SegmentFunds, error) {
	margins, err := f.broker.GetMargins(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get margins: %w", err)
	}

	segments := []SegmentFunds{
		{
			Segment:         "Equity",
			AvailableMargin: margins.Equity.Available,
			UsedMargin:      margins.Equity.Used,
			TotalMargin:     margins.Equity.Total,
			Utilization:     calculateUtilization(margins.Equity.Used, margins.Equity.Total),
		},
		{
			Segment:         "Commodity",
			AvailableMargin: margins.Commodity.Available,
			UsedMargin:      margins.Commodity.Used,
			TotalMargin:     margins.Commodity.Total,
			Utilization:     calculateUtilization(margins.Commodity.Used, margins.Commodity.Total),
		},
	}

	return segments, nil
}

// GetAvailableCash returns available cash balance.
func (f *FundManager) GetAvailableCash(ctx context.Context) (float64, error) {
	summary, err := f.GetFundSummary(ctx)
	if err != nil {
		return 0, err
	}
	return summary.AvailableCash, nil
}

// GetCollateralValue returns collateral value from pledged holdings.
func (f *FundManager) GetCollateralValue(ctx context.Context) (float64, error) {
	summary, err := f.GetFundSummary(ctx)
	if err != nil {
		return 0, err
	}
	return summary.CollateralValue, nil
}

// CheckLowBalance checks if balance is below threshold.
func (f *FundManager) CheckLowBalance(ctx context.Context) (bool, float64, error) {
	summary, err := f.GetFundSummary(ctx)
	if err != nil {
		return false, 0, err
	}

	isLow := summary.AvailableCash < f.lowBalanceThresh
	return isLow, summary.AvailableCash, nil
}

// GetAlerts returns current fund alerts.
func (f *FundManager) GetAlerts() []FundAlert {
	f.mu.RLock()
	defer f.mu.RUnlock()

	alerts := make([]FundAlert, len(f.alerts))
	copy(alerts, f.alerts)
	return alerts
}

// ClearAlerts clears all fund alerts.
func (f *FundManager) ClearAlerts() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts = make([]FundAlert, 0)
}

// checkAlerts checks for fund-related alerts.
func (f *FundManager) checkAlerts(summary *FundSummary) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Clear old alerts
	f.alerts = make([]FundAlert, 0)

	// Check low balance
	if summary.AvailableCash < f.lowBalanceThresh {
		severity := "WARNING"
		if summary.AvailableCash < f.lowBalanceThresh/2 {
			severity = "CRITICAL"
		}
		f.alerts = append(f.alerts, FundAlert{
			Type:      "LOW_BALANCE",
			Message:   fmt.Sprintf("Available cash ₹%.2f is below threshold ₹%.2f", summary.AvailableCash, f.lowBalanceThresh),
			Severity:  severity,
			Timestamp: time.Now(),
		})
	}

	// Check high margin utilization
	if summary.TotalEquity > 0 {
		utilization := (summary.UsedMargin / summary.TotalEquity) * 100
		if utilization > 80 {
			severity := "WARNING"
			if utilization > 95 {
				severity = "CRITICAL"
			}
			f.alerts = append(f.alerts, FundAlert{
				Type:      "HIGH_UTILIZATION",
				Message:   fmt.Sprintf("Margin utilization at %.1f%%", utilization),
				Severity:  severity,
				Timestamp: time.Now(),
			})
		}
	}
}

// CanAffordOrder checks if there are sufficient funds for an order.
func (f *FundManager) CanAffordOrder(ctx context.Context, orderValue float64, product models.ProductType) (bool, float64, error) {
	summary, err := f.GetFundSummary(ctx)
	if err != nil {
		return false, 0, err
	}

	var requiredMargin float64
	switch product {
	case models.ProductCNC:
		requiredMargin = orderValue
	case models.ProductMIS:
		requiredMargin = orderValue * 0.20 // ~5x leverage
	case models.ProductNRML:
		requiredMargin = orderValue * 0.15 // F&O margin
	default:
		requiredMargin = orderValue
	}

	canAfford := summary.AvailableCash >= requiredMargin
	shortfall := 0.0
	if !canAfford {
		shortfall = requiredMargin - summary.AvailableCash
	}

	return canAfford, shortfall, nil
}

// GetIntradayPayin returns intraday payin credits.
func (f *FundManager) GetIntradayPayin(ctx context.Context) (float64, error) {
	// In a real implementation, this would fetch from broker API
	// For now, return 0 as placeholder
	return 0, nil
}

// GetFundHistory returns fund balance history (placeholder).
func (f *FundManager) GetFundHistory(days int) []FundSummary {
	// In a real implementation, this would fetch from stored history
	return nil
}

// GetCachedSummary returns the cached fund summary if available and fresh.
func (f *FundManager) GetCachedSummary(maxAge time.Duration) *FundSummary {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.cachedSummary == nil {
		return nil
	}

	if time.Since(f.lastFetch) > maxAge {
		return nil
	}

	return f.cachedSummary
}

// RefreshFunds forces a refresh of fund data.
func (f *FundManager) RefreshFunds(ctx context.Context) error {
	_, err := f.GetFundSummary(ctx)
	return err
}
