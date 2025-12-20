// Package stream provides real-time data streaming and distribution functionality.
package stream

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"zerodha-trader/internal/models"
	"zerodha-trader/internal/notify"
	"zerodha-trader/internal/store"
)

// PlanLevelType represents the type of price level in a trade plan.
type PlanLevelType string

const (
	PlanLevelEntry   PlanLevelType = "entry"
	PlanLevelStopLoss PlanLevelType = "stop_loss"
	PlanLevelTarget1 PlanLevelType = "target1"
	PlanLevelTarget2 PlanLevelType = "target2"
	PlanLevelTarget3 PlanLevelType = "target3"
)

// PlanNotification represents a notification about a trade plan level.
type PlanNotification struct {
	Plan         *models.TradePlan
	Level        PlanLevelType
	LevelPrice   float64
	CurrentPrice float64
	Distance     float64 // Percentage distance from level
	Approaching  bool    // True if approaching, false if crossed
	Timestamp    time.Time
}

// PlanMonitor monitors ticks for trade plan levels.
// It implements the Consumer interface to receive ticks from the Hub.
type PlanMonitor struct {
	store    store.DataStore
	notifier notify.Notifier
	plans    map[string][]*PlanState // symbol -> plans
	mu       sync.RWMutex

	// Configuration
	approachThreshold float64 // Percentage threshold for "approaching" notifications
	notifyOnce        bool    // Only notify once per level

	// Callbacks
	onApproaching func(PlanNotification)
	onCrossed     func(PlanNotification)
}

// PlanState holds the runtime state of a trade plan.
type PlanState struct {
	Plan           *models.TradePlan
	NotifiedLevels map[PlanLevelType]bool // Track which levels have been notified
	LastChecked    time.Time
	LastPrice      float64
}

// PlanMonitorConfig holds configuration for the plan monitor.
type PlanMonitorConfig struct {
	ApproachThreshold float64 // Default 0.5%
	NotifyOnce        bool    // Default true
}

// DefaultPlanMonitorConfig returns the default configuration.
func DefaultPlanMonitorConfig() PlanMonitorConfig {
	return PlanMonitorConfig{
		ApproachThreshold: 0.5,
		NotifyOnce:        true,
	}
}

// NewPlanMonitor creates a new trade plan monitor.
func NewPlanMonitor(dataStore store.DataStore, notifier notify.Notifier) *PlanMonitor {
	config := DefaultPlanMonitorConfig()
	return NewPlanMonitorWithConfig(dataStore, notifier, config)
}

// NewPlanMonitorWithConfig creates a new trade plan monitor with custom config.
func NewPlanMonitorWithConfig(dataStore store.DataStore, notifier notify.Notifier, config PlanMonitorConfig) *PlanMonitor {
	return &PlanMonitor{
		store:             dataStore,
		notifier:          notifier,
		plans:             make(map[string][]*PlanState),
		approachThreshold: config.ApproachThreshold,
		notifyOnce:        config.NotifyOnce,
	}
}

// SetOnApproaching sets a callback for when price approaches a level.
func (m *PlanMonitor) SetOnApproaching(fn func(PlanNotification)) {
	m.onApproaching = fn
}

// SetOnCrossed sets a callback for when price crosses a level.
func (m *PlanMonitor) SetOnCrossed(fn func(PlanNotification)) {
	m.onCrossed = fn
}


// LoadPlans loads active trade plans from the data store.
func (m *PlanMonitor) LoadPlans(ctx context.Context) error {
	if m.store == nil {
		return nil
	}

	// Get pending and active plans
	filter := store.PlanFilter{
		Status: models.PlanPending,
	}
	pendingPlans, err := m.store.GetPlans(ctx, filter)
	if err != nil {
		return err
	}

	filter.Status = models.PlanActive
	activePlans, err := m.store.GetPlans(ctx, filter)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Clear existing plans
	m.plans = make(map[string][]*PlanState)

	// Add all plans
	allPlans := append(pendingPlans, activePlans...)
	for i := range allPlans {
		plan := &allPlans[i]
		state := &PlanState{
			Plan:           plan,
			NotifiedLevels: make(map[PlanLevelType]bool),
		}
		m.plans[plan.Symbol] = append(m.plans[plan.Symbol], state)
	}

	return nil
}

// AddPlan adds a trade plan to monitor.
func (m *PlanMonitor) AddPlan(plan *models.TradePlan) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state := &PlanState{
		Plan:           plan,
		NotifiedLevels: make(map[PlanLevelType]bool),
	}
	m.plans[plan.Symbol] = append(m.plans[plan.Symbol], state)
}

// RemovePlan removes a trade plan by ID.
func (m *PlanMonitor) RemovePlan(planID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for symbol, states := range m.plans {
		for i, state := range states {
			if state.Plan.ID == planID {
				m.plans[symbol] = append(states[:i], states[i+1:]...)
				if len(m.plans[symbol]) == 0 {
					delete(m.plans, symbol)
				}
				return
			}
		}
	}
}

// GetPlans returns all monitored plans.
func (m *PlanMonitor) GetPlans() []*models.TradePlan {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var plans []*models.TradePlan
	for _, states := range m.plans {
		for _, state := range states {
			plans = append(plans, state.Plan)
		}
	}
	return plans
}

// GetPlansForSymbol returns plans for a specific symbol.
func (m *PlanMonitor) GetPlansForSymbol(symbol string) []*models.TradePlan {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var plans []*models.TradePlan
	for _, state := range m.plans[symbol] {
		plans = append(plans, state.Plan)
	}
	return plans
}

// OnTick implements the Consumer interface.
func (m *PlanMonitor) OnTick(tick models.Tick) {
	m.Check(tick)
}

// Symbols implements the Consumer interface.
func (m *PlanMonitor) Symbols() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	symbols := make([]string, 0, len(m.plans))
	for symbol := range m.plans {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// Check checks all trade plans for a given tick.
func (m *PlanMonitor) Check(tick models.Tick) {
	m.mu.RLock()
	states := m.plans[tick.Symbol]
	m.mu.RUnlock()

	if len(states) == 0 {
		return
	}

	for _, state := range states {
		m.checkPlanLevels(state, tick)
		state.LastChecked = time.Now()
		state.LastPrice = tick.LTP
	}
}


// checkPlanLevels checks all levels of a trade plan against the current tick.
func (m *PlanMonitor) checkPlanLevels(state *PlanState, tick models.Tick) {
	plan := state.Plan

	// Define levels to check based on plan side
	levels := []struct {
		levelType PlanLevelType
		price     float64
	}{
		{PlanLevelEntry, plan.EntryPrice},
		{PlanLevelStopLoss, plan.StopLoss},
		{PlanLevelTarget1, plan.Target1},
		{PlanLevelTarget2, plan.Target2},
		{PlanLevelTarget3, plan.Target3},
	}

	for _, level := range levels {
		if level.price == 0 {
			continue
		}

		// Skip if already notified and notifyOnce is enabled
		if m.notifyOnce && state.NotifiedLevels[level.levelType] {
			continue
		}

		distance := m.calculateDistance(tick.LTP, level.price)

		// Check if approaching
		if math.Abs(distance) <= m.approachThreshold {
			m.notifyApproaching(state, level.levelType, level.price, tick, distance)
		}

		// Check if crossed (using previous price if available)
		if state.LastPrice > 0 {
			if m.hasCrossed(state.LastPrice, tick.LTP, level.price) {
				m.notifyCrossed(state, level.levelType, level.price, tick, distance)
			}
		}
	}
}

// calculateDistance calculates the percentage distance from current price to target.
func (m *PlanMonitor) calculateDistance(currentPrice, targetPrice float64) float64 {
	if targetPrice == 0 {
		return 0
	}
	return ((currentPrice - targetPrice) / targetPrice) * 100
}

// hasCrossed checks if price has crossed a level.
func (m *PlanMonitor) hasCrossed(prevPrice, currentPrice, levelPrice float64) bool {
	// Crossed from below
	if prevPrice < levelPrice && currentPrice >= levelPrice {
		return true
	}
	// Crossed from above
	if prevPrice > levelPrice && currentPrice <= levelPrice {
		return true
	}
	return false
}

// notifyApproaching handles notification when price approaches a level.
func (m *PlanMonitor) notifyApproaching(state *PlanState, levelType PlanLevelType, levelPrice float64, tick models.Tick, distance float64) {
	notification := PlanNotification{
		Plan:         state.Plan,
		Level:        levelType,
		LevelPrice:   levelPrice,
		CurrentPrice: tick.LTP,
		Distance:     distance,
		Approaching:  true,
		Timestamp:    time.Now(),
	}

	// Mark as notified
	state.NotifiedLevels[levelType] = true

	// Call callback
	if m.onApproaching != nil {
		m.onApproaching(notification)
	}

	// Send notification
	if m.notifier != nil {
		m.sendNotification(notification)
	}
}

// notifyCrossed handles notification when price crosses a level.
func (m *PlanMonitor) notifyCrossed(state *PlanState, levelType PlanLevelType, levelPrice float64, tick models.Tick, distance float64) {
	notification := PlanNotification{
		Plan:         state.Plan,
		Level:        levelType,
		LevelPrice:   levelPrice,
		CurrentPrice: tick.LTP,
		Distance:     distance,
		Approaching:  false,
		Timestamp:    time.Now(),
	}

	// Mark as notified
	state.NotifiedLevels[levelType] = true

	// Call callback
	if m.onCrossed != nil {
		m.onCrossed(notification)
	}

	// Send notification
	if m.notifier != nil {
		m.sendNotification(notification)
	}
}

// sendNotification sends a notification through the notifier.
func (m *PlanMonitor) sendNotification(pn PlanNotification) {
	ctx := context.Background()

	var title, message string
	if pn.Approaching {
		title = fmt.Sprintf("%s approaching %s", pn.Plan.Symbol, pn.Level)
		message = fmt.Sprintf("LTP: %.2f, %s: %.2f (%.2f%% away)",
			pn.CurrentPrice, pn.Level, pn.LevelPrice, math.Abs(pn.Distance))
	} else {
		title = fmt.Sprintf("%s crossed %s", pn.Plan.Symbol, pn.Level)
		message = fmt.Sprintf("LTP: %.2f crossed %s at %.2f",
			pn.CurrentPrice, pn.Level, pn.LevelPrice)
	}

	notification := notify.Notification{
		Type:    notify.NotificationAlert,
		Title:   title,
		Message: message,
		Data: map[string]interface{}{
			"plan_id":       pn.Plan.ID,
			"symbol":        pn.Plan.Symbol,
			"level":         string(pn.Level),
			"level_price":   pn.LevelPrice,
			"current_price": pn.CurrentPrice,
			"distance":      pn.Distance,
			"approaching":   pn.Approaching,
		},
	}

	m.notifier.Send(ctx, notification)
}


// GetPlanStatus returns the current status of a plan with price distances.
func (m *PlanMonitor) GetPlanStatus(planID string, currentPrice float64) *PlanStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, states := range m.plans {
		for _, state := range states {
			if state.Plan.ID == planID {
				return m.calculatePlanStatus(state.Plan, currentPrice)
			}
		}
	}
	return nil
}

// PlanStatus represents the current status of a trade plan.
type PlanStatus struct {
	Plan            *models.TradePlan
	CurrentPrice    float64
	EntryDistance   float64 // Percentage distance to entry
	StopLossDistance float64 // Percentage distance to stop loss
	Target1Distance float64 // Percentage distance to target 1
	Target2Distance float64 // Percentage distance to target 2
	Target3Distance float64 // Percentage distance to target 3
	NearestLevel    PlanLevelType
	NearestDistance float64
}

// calculatePlanStatus calculates the status of a plan.
func (m *PlanMonitor) calculatePlanStatus(plan *models.TradePlan, currentPrice float64) *PlanStatus {
	status := &PlanStatus{
		Plan:         plan,
		CurrentPrice: currentPrice,
	}

	if plan.EntryPrice > 0 {
		status.EntryDistance = m.calculateDistance(currentPrice, plan.EntryPrice)
	}
	if plan.StopLoss > 0 {
		status.StopLossDistance = m.calculateDistance(currentPrice, plan.StopLoss)
	}
	if plan.Target1 > 0 {
		status.Target1Distance = m.calculateDistance(currentPrice, plan.Target1)
	}
	if plan.Target2 > 0 {
		status.Target2Distance = m.calculateDistance(currentPrice, plan.Target2)
	}
	if plan.Target3 > 0 {
		status.Target3Distance = m.calculateDistance(currentPrice, plan.Target3)
	}

	// Find nearest level
	minDistance := math.MaxFloat64
	levels := []struct {
		levelType PlanLevelType
		distance  float64
	}{
		{PlanLevelEntry, status.EntryDistance},
		{PlanLevelStopLoss, status.StopLossDistance},
		{PlanLevelTarget1, status.Target1Distance},
		{PlanLevelTarget2, status.Target2Distance},
		{PlanLevelTarget3, status.Target3Distance},
	}

	for _, level := range levels {
		absDistance := math.Abs(level.distance)
		if absDistance < minDistance && absDistance > 0 {
			minDistance = absDistance
			status.NearestLevel = level.levelType
			status.NearestDistance = level.distance
		}
	}

	return status
}

// GetAllPlanStatuses returns status for all plans with current prices.
func (m *PlanMonitor) GetAllPlanStatuses(prices map[string]float64) []*PlanStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var statuses []*PlanStatus
	for symbol, states := range m.plans {
		price, ok := prices[symbol]
		if !ok {
			continue
		}
		for _, state := range states {
			status := m.calculatePlanStatus(state.Plan, price)
			statuses = append(statuses, status)
		}
	}
	return statuses
}

// GetPlanCount returns the number of monitored plans.
func (m *PlanMonitor) GetPlanCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, states := range m.plans {
		count += len(states)
	}
	return count
}

// ClearPlans removes all plans.
func (m *PlanMonitor) ClearPlans() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plans = make(map[string][]*PlanState)
}

// ResetNotifications resets the notification state for a plan.
func (m *PlanMonitor) ResetNotifications(planID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, states := range m.plans {
		for _, state := range states {
			if state.Plan.ID == planID {
				state.NotifiedLevels = make(map[PlanLevelType]bool)
				return
			}
		}
	}
}

// SetApproachThreshold sets the approach threshold percentage.
func (m *PlanMonitor) SetApproachThreshold(threshold float64) {
	m.approachThreshold = threshold
}

// GetApproachThreshold returns the current approach threshold.
func (m *PlanMonitor) GetApproachThreshold() float64 {
	return m.approachThreshold
}
