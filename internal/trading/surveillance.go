// Package trading provides trading operations and utilities.
package trading

import (
	"fmt"
	"sync"
	"time"

	"zerodha-trader/internal/models"
)

// SurveillanceCategory represents surveillance categories.
type SurveillanceCategory string

const (
	SurveillanceASM  SurveillanceCategory = "ASM"  // Additional Surveillance Measure
	SurveillanceGSM  SurveillanceCategory = "GSM"  // Graded Surveillance Measure
	SurveillanceT2T  SurveillanceCategory = "T2T"  // Trade to Trade
	SurveillanceNone SurveillanceCategory = "NONE"
)

// ASMStage represents ASM stages (1 or 2).
type ASMStage int

const (
	ASMStage1 ASMStage = 1
	ASMStage2 ASMStage = 2
)

// GSMStage represents GSM stages (1-6).
type GSMStage int

const (
	GSMStage1 GSMStage = 1
	GSMStage2 GSMStage = 2
	GSMStage3 GSMStage = 3
	GSMStage4 GSMStage = 4
	GSMStage5 GSMStage = 5
	GSMStage6 GSMStage = 6
)

// SurveillanceStatus represents surveillance status of a stock.
type SurveillanceStatus struct {
	Symbol            string
	Category          SurveillanceCategory
	ASMStage          ASMStage
	GSMStage          GSMStage
	IsT2T             bool
	AdditionalMargin  float64 // Additional margin percentage required
	Reason            string
	EffectiveDate     time.Time
	LastUpdated       time.Time
}

// SurveillanceAlert represents an alert for surveillance status change.
type SurveillanceAlert struct {
	Symbol      string
	OldCategory SurveillanceCategory
	NewCategory SurveillanceCategory
	Message     string
	Timestamp   time.Time
}

// SurveillanceMonitor monitors surveillance status for Indian markets.
type SurveillanceMonitor struct {
	statuses map[string]*SurveillanceStatus // symbol -> status
	alerts   []SurveillanceAlert
	mu       sync.RWMutex
}

// NewSurveillanceMonitor creates a new surveillance monitor.
func NewSurveillanceMonitor() *SurveillanceMonitor {
	return &SurveillanceMonitor{
		statuses: make(map[string]*SurveillanceStatus),
		alerts:   make([]SurveillanceAlert, 0),
	}
}

// SetSurveillanceStatus sets the surveillance status for a symbol.
func (m *SurveillanceMonitor) SetSurveillanceStatus(status *SurveillanceStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()

	oldStatus := m.statuses[status.Symbol]
	status.LastUpdated = time.Now()
	m.statuses[status.Symbol] = status

	// Generate alert if status changed
	if oldStatus != nil && oldStatus.Category != status.Category {
		m.alerts = append(m.alerts, SurveillanceAlert{
			Symbol:      status.Symbol,
			OldCategory: oldStatus.Category,
			NewCategory: status.Category,
			Message:     fmt.Sprintf("%s moved from %s to %s", status.Symbol, oldStatus.Category, status.Category),
			Timestamp:   time.Now(),
		})
	}
}

// GetSurveillanceStatus returns surveillance status for a symbol.
func (m *SurveillanceMonitor) GetSurveillanceStatus(symbol string) *SurveillanceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.statuses[symbol]
}

// IsASM checks if a stock is under ASM.
func (m *SurveillanceMonitor) IsASM(symbol string) (bool, ASMStage) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status, ok := m.statuses[symbol]
	if !ok || status.Category != SurveillanceASM {
		return false, 0
	}
	return true, status.ASMStage
}

// IsGSM checks if a stock is under GSM.
func (m *SurveillanceMonitor) IsGSM(symbol string) (bool, GSMStage) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status, ok := m.statuses[symbol]
	if !ok || status.Category != SurveillanceGSM {
		return false, 0
	}
	return true, status.GSMStage
}

// IsT2T checks if a stock is under T2T (Trade to Trade).
func (m *SurveillanceMonitor) IsT2T(symbol string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status, ok := m.statuses[symbol]
	if !ok {
		return false
	}
	return status.IsT2T
}

// GetAdditionalMargin returns additional margin required for a surveillance stock.
func (m *SurveillanceMonitor) GetAdditionalMargin(symbol string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status, ok := m.statuses[symbol]
	if !ok {
		return 0
	}

	// Calculate additional margin based on category and stage
	switch status.Category {
	case SurveillanceASM:
		switch status.ASMStage {
		case ASMStage1:
			return 50.0 // 50% additional margin
		case ASMStage2:
			return 100.0 // 100% additional margin
		}
	case SurveillanceGSM:
		switch status.GSMStage {
		case GSMStage1:
			return 25.0
		case GSMStage2:
			return 50.0
		case GSMStage3:
			return 75.0
		case GSMStage4:
			return 100.0
		case GSMStage5:
			return 100.0 // Plus trading restrictions
		case GSMStage6:
			return 100.0 // Plus severe restrictions
		}
	}

	return status.AdditionalMargin
}

// ShouldWarnBeforeTrading checks if warning should be shown before trading.
func (m *SurveillanceMonitor) ShouldWarnBeforeTrading(symbol string) (bool, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status, ok := m.statuses[symbol]
	if !ok || status.Category == SurveillanceNone {
		return false, ""
	}

	var warning string
	switch status.Category {
	case SurveillanceASM:
		warning = fmt.Sprintf("⚠️ %s is under ASM Stage %d. Additional margin of %.0f%% required. %s",
			symbol, status.ASMStage, m.GetAdditionalMargin(symbol), status.Reason)
	case SurveillanceGSM:
		warning = fmt.Sprintf("⚠️ %s is under GSM Stage %d. Additional margin of %.0f%% required. %s",
			symbol, status.GSMStage, m.GetAdditionalMargin(symbol), status.Reason)
	case SurveillanceT2T:
		warning = fmt.Sprintf("⚠️ %s is under T2T segment. Intraday trading not allowed. %s",
			symbol, status.Reason)
	}

	return true, warning
}

// FilterSurveillanceStocks filters out surveillance stocks from a list.
func (m *SurveillanceMonitor) FilterSurveillanceStocks(symbols []string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var filtered []string
	for _, symbol := range symbols {
		status, ok := m.statuses[symbol]
		if !ok || status.Category == SurveillanceNone {
			filtered = append(filtered, symbol)
		}
	}
	return filtered
}

// GetSurveillanceStocks returns all stocks under surveillance.
func (m *SurveillanceMonitor) GetSurveillanceStocks() []SurveillanceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var stocks []SurveillanceStatus
	for _, status := range m.statuses {
		if status.Category != SurveillanceNone {
			stocks = append(stocks, *status)
		}
	}
	return stocks
}

// GetASMStocks returns all ASM stocks.
func (m *SurveillanceMonitor) GetASMStocks() []SurveillanceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var stocks []SurveillanceStatus
	for _, status := range m.statuses {
		if status.Category == SurveillanceASM {
			stocks = append(stocks, *status)
		}
	}
	return stocks
}

// GetGSMStocks returns all GSM stocks.
func (m *SurveillanceMonitor) GetGSMStocks() []SurveillanceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var stocks []SurveillanceStatus
	for _, status := range m.statuses {
		if status.Category == SurveillanceGSM {
			stocks = append(stocks, *status)
		}
	}
	return stocks
}

// GetT2TStocks returns all T2T stocks.
func (m *SurveillanceMonitor) GetT2TStocks() []SurveillanceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var stocks []SurveillanceStatus
	for _, status := range m.statuses {
		if status.IsT2T {
			stocks = append(stocks, *status)
		}
	}
	return stocks
}

// CheckHoldingsForSurveillance checks if any holdings are under surveillance.
func (m *SurveillanceMonitor) CheckHoldingsForSurveillance(holdings []models.Holding) []SurveillanceAlert {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var alerts []SurveillanceAlert
	for _, h := range holdings {
		status, ok := m.statuses[h.Symbol]
		if ok && status.Category != SurveillanceNone {
			alerts = append(alerts, SurveillanceAlert{
				Symbol:      h.Symbol,
				NewCategory: status.Category,
				Message:     fmt.Sprintf("Holding %s is under %s surveillance", h.Symbol, status.Category),
				Timestamp:   time.Now(),
			})
		}
	}
	return alerts
}

// GetAlerts returns surveillance alerts.
func (m *SurveillanceMonitor) GetAlerts() []SurveillanceAlert {
	m.mu.RLock()
	defer m.mu.RUnlock()

	alerts := make([]SurveillanceAlert, len(m.alerts))
	copy(alerts, m.alerts)
	return alerts
}

// ClearAlerts clears all surveillance alerts.
func (m *SurveillanceMonitor) ClearAlerts() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alerts = make([]SurveillanceAlert, 0)
}

// CanTradeIntraday checks if intraday trading is allowed for a symbol.
func (m *SurveillanceMonitor) CanTradeIntraday(symbol string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status, ok := m.statuses[symbol]
	if !ok {
		return true
	}

	// T2T stocks cannot be traded intraday
	if status.IsT2T {
		return false
	}

	// GSM Stage 5 and 6 have trading restrictions
	if status.Category == SurveillanceGSM && status.GSMStage >= GSMStage5 {
		return false
	}

	return true
}
