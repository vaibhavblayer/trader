// Package stream provides real-time data streaming and distribution functionality.
package stream

import (
	"context"
	"math"
	"sync"
	"time"

	"zerodha-trader/internal/models"
	"zerodha-trader/internal/notify"
	"zerodha-trader/internal/store"
)

// AlertCondition represents the type of alert condition.
type AlertCondition string

const (
	// AlertConditionAbove triggers when price goes above the target.
	AlertConditionAbove AlertCondition = "above"
	// AlertConditionBelow triggers when price goes below the target.
	AlertConditionBelow AlertCondition = "below"
	// AlertConditionPercentChange triggers when price changes by a percentage.
	AlertConditionPercentChange AlertCondition = "percent_change"
	// AlertConditionCrossAbove triggers when price crosses above the target.
	AlertConditionCrossAbove AlertCondition = "cross_above"
	// AlertConditionCrossBelow triggers when price crosses below the target.
	AlertConditionCrossBelow AlertCondition = "cross_below"
)

// AlertMonitor monitors ticks for alert conditions.
// It implements the Consumer interface to receive ticks from the Hub.
type AlertMonitor struct {
	store    store.DataStore
	notifier notify.Notifier
	alerts   map[string][]*AlertState // symbol -> alerts
	mu       sync.RWMutex
	
	// Track previous prices for cross-type alerts
	prevPrices map[string]float64
	prevMu     sync.RWMutex
	
	// Callback for alert triggers
	onTrigger func(*models.Alert, models.Tick)
}

// AlertState holds the runtime state of an alert.
type AlertState struct {
	Alert       *models.Alert
	LastChecked time.Time
	CheckCount  int
}

// NewAlertMonitor creates a new alert monitor.
func NewAlertMonitor(dataStore store.DataStore, notifier notify.Notifier) *AlertMonitor {
	return &AlertMonitor{
		store:      dataStore,
		notifier:   notifier,
		alerts:     make(map[string][]*AlertState),
		prevPrices: make(map[string]float64),
	}
}

// SetOnTrigger sets a callback function to be called when an alert triggers.
func (m *AlertMonitor) SetOnTrigger(fn func(*models.Alert, models.Tick)) {
	m.onTrigger = fn
}

// LoadAlerts loads active alerts from the data store.
func (m *AlertMonitor) LoadAlerts(ctx context.Context) error {
	if m.store == nil {
		return nil
	}

	alerts, err := m.store.GetActiveAlerts(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear existing alerts
	m.alerts = make(map[string][]*AlertState)

	// Group alerts by symbol
	for i := range alerts {
		alert := &alerts[i]
		state := &AlertState{
			Alert: alert,
		}
		m.alerts[alert.Symbol] = append(m.alerts[alert.Symbol], state)
	}

	return nil
}


// AddAlert adds a new alert to monitor.
func (m *AlertMonitor) AddAlert(alert *models.Alert) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state := &AlertState{
		Alert: alert,
	}
	m.alerts[alert.Symbol] = append(m.alerts[alert.Symbol], state)
}

// RemoveAlert removes an alert by ID.
func (m *AlertMonitor) RemoveAlert(alertID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for symbol, states := range m.alerts {
		for i, state := range states {
			if state.Alert.ID == alertID {
				m.alerts[symbol] = append(states[:i], states[i+1:]...)
				if len(m.alerts[symbol]) == 0 {
					delete(m.alerts, symbol)
				}
				return
			}
		}
	}
}

// GetAlerts returns all active alerts.
func (m *AlertMonitor) GetAlerts() []*models.Alert {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var alerts []*models.Alert
	for _, states := range m.alerts {
		for _, state := range states {
			if !state.Alert.Triggered {
				alerts = append(alerts, state.Alert)
			}
		}
	}
	return alerts
}

// GetAlertsForSymbol returns active alerts for a specific symbol.
func (m *AlertMonitor) GetAlertsForSymbol(symbol string) []*models.Alert {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var alerts []*models.Alert
	for _, state := range m.alerts[symbol] {
		if !state.Alert.Triggered {
			alerts = append(alerts, state.Alert)
		}
	}
	return alerts
}

// OnTick implements the Consumer interface.
// It checks all alerts for the tick's symbol.
func (m *AlertMonitor) OnTick(tick models.Tick) {
	m.Check(tick)
}

// Symbols implements the Consumer interface.
// Returns nil to receive all ticks.
func (m *AlertMonitor) Symbols() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	symbols := make([]string, 0, len(m.alerts))
	for symbol := range m.alerts {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// Check checks all alerts for a given tick.
func (m *AlertMonitor) Check(tick models.Tick) {
	m.mu.RLock()
	states := m.alerts[tick.Symbol]
	m.mu.RUnlock()

	if len(states) == 0 {
		return
	}

	// Get previous price for cross-type alerts
	m.prevMu.RLock()
	prevPrice := m.prevPrices[tick.Symbol]
	m.prevMu.RUnlock()

	for _, state := range states {
		if state.Alert.Triggered {
			continue
		}

		state.LastChecked = time.Now()
		state.CheckCount++

		if m.isTriggered(state.Alert, tick, prevPrice) {
			m.trigger(state, tick)
		}
	}

	// Update previous price
	m.prevMu.Lock()
	m.prevPrices[tick.Symbol] = tick.LTP
	m.prevMu.Unlock()
}

// isTriggered checks if an alert condition is met.
func (m *AlertMonitor) isTriggered(alert *models.Alert, tick models.Tick, prevPrice float64) bool {
	condition := AlertCondition(alert.Condition)

	switch condition {
	case AlertConditionAbove:
		return tick.LTP >= alert.Price

	case AlertConditionBelow:
		return tick.LTP <= alert.Price

	case AlertConditionPercentChange:
		if tick.Close == 0 {
			return false
		}
		change := math.Abs((tick.LTP - tick.Close) / tick.Close * 100)
		return change >= alert.Price

	case AlertConditionCrossAbove:
		// Price crossed above the target
		if prevPrice == 0 {
			return false
		}
		return prevPrice < alert.Price && tick.LTP >= alert.Price

	case AlertConditionCrossBelow:
		// Price crossed below the target
		if prevPrice == 0 {
			return false
		}
		return prevPrice > alert.Price && tick.LTP <= alert.Price

	default:
		// Default to simple above/below check
		if alert.Condition == "above" {
			return tick.LTP >= alert.Price
		}
		return tick.LTP <= alert.Price
	}
}


// trigger handles an alert being triggered.
func (m *AlertMonitor) trigger(state *AlertState, tick models.Tick) {
	alert := state.Alert
	alert.Triggered = true
	now := time.Now()
	alert.TriggeredAt = &now

	// Update in data store
	if m.store != nil {
		ctx := context.Background()
		m.store.TriggerAlert(ctx, alert.ID)
	}

	// Send notification
	if m.notifier != nil {
		ctx := context.Background()
		m.notifier.SendAlert(ctx, alert, tick)
	}

	// Call trigger callback
	if m.onTrigger != nil {
		m.onTrigger(alert, tick)
	}

	// Remove from active alerts
	m.mu.Lock()
	states := m.alerts[alert.Symbol]
	for i, s := range states {
		if s.Alert.ID == alert.ID {
			m.alerts[alert.Symbol] = append(states[:i], states[i+1:]...)
			break
		}
	}
	if len(m.alerts[alert.Symbol]) == 0 {
		delete(m.alerts, alert.Symbol)
	}
	m.mu.Unlock()
}

// GetAlertCount returns the number of active alerts.
func (m *AlertMonitor) GetAlertCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, states := range m.alerts {
		for _, state := range states {
			if !state.Alert.Triggered {
				count++
			}
		}
	}
	return count
}

// ClearAlerts removes all alerts.
func (m *AlertMonitor) ClearAlerts() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alerts = make(map[string][]*AlertState)
}

// CreateAlert is a helper to create and add an alert.
func (m *AlertMonitor) CreateAlert(symbol string, condition AlertCondition, price float64) *models.Alert {
	alert := &models.Alert{
		ID:        generateAlertID(),
		Symbol:    symbol,
		Condition: string(condition),
		Price:     price,
		Triggered: false,
		CreatedAt: time.Now(),
	}

	// Save to store if available
	if m.store != nil {
		ctx := context.Background()
		m.store.SaveAlert(ctx, alert)
	}

	m.AddAlert(alert)
	return alert
}

// generateAlertID generates a unique alert ID.
func generateAlertID() string {
	return time.Now().Format("20060102150405.000000")
}

// AlertStats contains statistics about alerts.
type AlertStats struct {
	TotalAlerts     int
	TriggeredAlerts int
	ActiveAlerts    int
	BySymbol        map[string]int
	ByCondition     map[string]int
}

// GetStats returns alert statistics.
func (m *AlertMonitor) GetStats() AlertStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := AlertStats{
		BySymbol:    make(map[string]int),
		ByCondition: make(map[string]int),
	}

	for symbol, states := range m.alerts {
		for _, state := range states {
			stats.TotalAlerts++
			if state.Alert.Triggered {
				stats.TriggeredAlerts++
			} else {
				stats.ActiveAlerts++
			}
			stats.BySymbol[symbol]++
			stats.ByCondition[state.Alert.Condition]++
		}
	}

	return stats
}
