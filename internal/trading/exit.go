// Package trading provides trading operations including exit management.
package trading

import (
	"context"
	"fmt"
	"sync"
	"time"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
	"zerodha-trader/pkg/utils"
)

// DefaultExitManager implements the ExitManager interface.
// Requirements: 65.6-65.10
type DefaultExitManager struct {
	broker          broker.Broker
	positionManager *DefaultPositionManager
	mu              sync.RWMutex

	// Trailing stop configurations
	trailingStops map[string]*TrailingStopConfig

	// Time-based exit configurations
	timeExits map[string]*TimeExitConfig

	// Scale-out configurations
	scaleOuts map[string]*ScaleOutConfig

	// MIS auto-exit configuration
	misExitBuffer time.Duration // Buffer before 3:15 PM square-off
}

// TrailingStopConfig holds trailing stop configuration for a symbol.
type TrailingStopConfig struct {
	Symbol        string
	Percent       float64
	InitialPrice  float64
	HighestPrice  float64 // For long positions
	LowestPrice   float64 // For short positions
	TriggerPrice  float64
	IsLong        bool
	ActivatedAt   time.Time
}

// TimeExitConfig holds time-based exit configuration for a symbol.
type TimeExitConfig struct {
	Symbol     string
	Duration   time.Duration
	MaxCandles int
	EntryTime  time.Time
	ExitTime   time.Time
}

// ScaleOutConfig holds scale-out configuration for a symbol.
type ScaleOutConfig struct {
	Symbol         string
	Targets        []ScaleOutTarget
	ExecutedLevels []int // Indices of executed targets
	TotalQuantity  int
	RemainingQty   int
}

// NewExitManager creates a new exit manager.
func NewExitManager(b broker.Broker, pm *DefaultPositionManager) *DefaultExitManager {
	return &DefaultExitManager{
		broker:          b,
		positionManager: pm,
		trailingStops:   make(map[string]*TrailingStopConfig),
		timeExits:       make(map[string]*TimeExitConfig),
		scaleOuts:       make(map[string]*ScaleOutConfig),
		misExitBuffer:   15 * time.Minute, // Default 15 minutes before 3:15 PM
	}
}

// SetTrailingStop sets a trailing stop-loss for a symbol.
// Requirement 65.6: THE system SHALL support trailing stop-loss that adjusts as price moves favorably
func (em *DefaultExitManager) SetTrailingStop(symbol string, percent float64) error {
	if percent <= 0 || percent > 100 {
		return fmt.Errorf("trailing stop percent must be between 0 and 100")
	}

	em.mu.Lock()
	defer em.mu.Unlock()

	// Get current position to determine direction
	ctx := context.Background()
	position, err := em.positionManager.GetPosition(ctx, symbol)
	if err != nil {
		return fmt.Errorf("getting position for trailing stop: %w", err)
	}

	if position.Quantity == 0 {
		return fmt.Errorf("no open position for symbol: %s", symbol)
	}

	isLong := position.Quantity > 0
	currentPrice := position.LTP

	config := &TrailingStopConfig{
		Symbol:       symbol,
		Percent:      percent,
		InitialPrice: currentPrice,
		IsLong:       isLong,
		ActivatedAt:  time.Now(),
	}

	if isLong {
		config.HighestPrice = currentPrice
		config.TriggerPrice = currentPrice * (1 - percent/100)
	} else {
		config.LowestPrice = currentPrice
		config.TriggerPrice = currentPrice * (1 + percent/100)
	}

	em.trailingStops[symbol] = config
	return nil
}

// UpdateTrailingStop updates the trailing stop based on current price.
func (em *DefaultExitManager) UpdateTrailingStop(symbol string, currentPrice float64) (bool, float64) {
	em.mu.Lock()
	defer em.mu.Unlock()

	config, ok := em.trailingStops[symbol]
	if !ok {
		return false, 0
	}

	if config.IsLong {
		// For long positions, trail up
		if currentPrice > config.HighestPrice {
			config.HighestPrice = currentPrice
			config.TriggerPrice = currentPrice * (1 - config.Percent/100)
		}
		// Check if stop is hit
		if currentPrice <= config.TriggerPrice {
			return true, config.TriggerPrice
		}
	} else {
		// For short positions, trail down
		if currentPrice < config.LowestPrice {
			config.LowestPrice = currentPrice
			config.TriggerPrice = currentPrice * (1 + config.Percent/100)
		}
		// Check if stop is hit
		if currentPrice >= config.TriggerPrice {
			return true, config.TriggerPrice
		}
	}

	return false, config.TriggerPrice
}

// SetTimeBasedExit sets a time-based exit for a symbol.
// Requirement 65.8: THE system SHALL support time-based exits (exit if target not hit within N candles/hours)
func (em *DefaultExitManager) SetTimeBasedExit(symbol string, duration time.Duration) error {
	if duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}

	em.mu.Lock()
	defer em.mu.Unlock()

	config := &TimeExitConfig{
		Symbol:    symbol,
		Duration:  duration,
		EntryTime: time.Now(),
		ExitTime:  time.Now().Add(duration),
	}

	em.timeExits[symbol] = config
	return nil
}

// SetTimeBasedExitCandles sets a candle-based exit for a symbol.
func (em *DefaultExitManager) SetTimeBasedExitCandles(symbol string, maxCandles int, candleDuration time.Duration) error {
	if maxCandles <= 0 {
		return fmt.Errorf("max candles must be positive")
	}

	em.mu.Lock()
	defer em.mu.Unlock()

	duration := time.Duration(maxCandles) * candleDuration

	config := &TimeExitConfig{
		Symbol:     symbol,
		Duration:   duration,
		MaxCandles: maxCandles,
		EntryTime:  time.Now(),
		ExitTime:   time.Now().Add(duration),
	}

	em.timeExits[symbol] = config
	return nil
}

// SetScaleOutTargets sets scale-out targets for a symbol.
// Requirement 65.10: WHEN a position hits partial target, THE system SHALL support scaling out (exit partial quantity)
func (em *DefaultExitManager) SetScaleOutTargets(symbol string, targets []ScaleOutTarget) error {
	if len(targets) == 0 {
		return fmt.Errorf("at least one target is required")
	}

	// Validate targets
	var totalPercent float64
	for _, t := range targets {
		if t.Price <= 0 {
			return fmt.Errorf("target price must be positive")
		}
		if t.Percent <= 0 || t.Percent > 100 {
			return fmt.Errorf("target percent must be between 0 and 100")
		}
		totalPercent += t.Percent
	}

	if totalPercent > 100 {
		return fmt.Errorf("total scale-out percent cannot exceed 100%%")
	}

	em.mu.Lock()
	defer em.mu.Unlock()

	// Get current position quantity
	ctx := context.Background()
	position, err := em.positionManager.GetPosition(ctx, symbol)
	if err != nil {
		return fmt.Errorf("getting position for scale-out: %w", err)
	}

	qty := position.Quantity
	if qty < 0 {
		qty = -qty
	}

	config := &ScaleOutConfig{
		Symbol:         symbol,
		Targets:        targets,
		ExecutedLevels: make([]int, 0),
		TotalQuantity:  qty,
		RemainingQty:   qty,
	}

	em.scaleOuts[symbol] = config
	return nil
}

// SetMISExitBuffer sets the buffer time before MIS square-off.
// Requirement 65.7: THE system SHALL auto-exit MIS positions by 3:00 PM IST (configurable buffer before square-off)
func (em *DefaultExitManager) SetMISExitBuffer(buffer time.Duration) {
	em.mu.Lock()
	defer em.mu.Unlock()
	em.misExitBuffer = buffer
}

// CheckExits checks all exit conditions and returns signals for positions that should be exited.
func (em *DefaultExitManager) CheckExits(ctx context.Context) ([]ExitSignal, error) {
	var signals []ExitSignal

	// Get current positions
	positions, err := em.positionManager.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching positions: %w", err)
	}

	em.mu.RLock()
	defer em.mu.RUnlock()

	for _, pos := range positions {
		if pos.Quantity == 0 {
			continue
		}

		// Check trailing stop
		if config, ok := em.trailingStops[pos.Symbol]; ok {
			triggered, triggerPrice := em.checkTrailingStopLocked(config, pos.LTP)
			if triggered {
				qty := pos.Quantity
				if qty < 0 {
					qty = -qty
				}
				signals = append(signals, ExitSignal{
					Symbol:   pos.Symbol,
					Reason:   ExitReasonTrailingStop,
					Price:    triggerPrice,
					Quantity: qty,
				})
				continue
			}
		}

		// Check time-based exit
		if config, ok := em.timeExits[pos.Symbol]; ok {
			if time.Now().After(config.ExitTime) {
				qty := pos.Quantity
				if qty < 0 {
					qty = -qty
				}
				signals = append(signals, ExitSignal{
					Symbol:   pos.Symbol,
					Reason:   ExitReasonTimeLimit,
					Price:    pos.LTP,
					Quantity: qty,
				})
				continue
			}
		}

		// Check scale-out targets
		if config, ok := em.scaleOuts[pos.Symbol]; ok {
			scaleSignals := em.checkScaleOutTargetsLocked(config, pos)
			signals = append(signals, scaleSignals...)
		}

		// Check MIS auto-exit
		if pos.Product == models.ProductMIS {
			if em.shouldExitMIS() {
				qty := pos.Quantity
				if qty < 0 {
					qty = -qty
				}
				signals = append(signals, ExitSignal{
					Symbol:   pos.Symbol,
					Reason:   ExitReasonMISSquareOff,
					Price:    pos.LTP,
					Quantity: qty,
				})
			}
		}
	}

	return signals, nil
}

// checkTrailingStopLocked checks if trailing stop is triggered (must hold read lock).
func (em *DefaultExitManager) checkTrailingStopLocked(config *TrailingStopConfig, currentPrice float64) (bool, float64) {
	if config.IsLong {
		// Update highest price
		if currentPrice > config.HighestPrice {
			config.HighestPrice = currentPrice
			config.TriggerPrice = currentPrice * (1 - config.Percent/100)
		}
		// Check if stop is hit
		if currentPrice <= config.TriggerPrice {
			return true, config.TriggerPrice
		}
	} else {
		// Update lowest price
		if currentPrice < config.LowestPrice {
			config.LowestPrice = currentPrice
			config.TriggerPrice = currentPrice * (1 + config.Percent/100)
		}
		// Check if stop is hit
		if currentPrice >= config.TriggerPrice {
			return true, config.TriggerPrice
		}
	}
	return false, config.TriggerPrice
}

// checkScaleOutTargetsLocked checks scale-out targets (must hold read lock).
func (em *DefaultExitManager) checkScaleOutTargetsLocked(config *ScaleOutConfig, pos models.Position) []ExitSignal {
	var signals []ExitSignal
	isLong := pos.Quantity > 0

	for i, target := range config.Targets {
		// Skip already executed targets
		executed := false
		for _, idx := range config.ExecutedLevels {
			if idx == i {
				executed = true
				break
			}
		}
		if executed {
			continue
		}

		// Check if target is hit
		targetHit := false
		if isLong && pos.LTP >= target.Price {
			targetHit = true
		} else if !isLong && pos.LTP <= target.Price {
			targetHit = true
		}

		if targetHit {
			// Calculate quantity to exit
			var exitQty int
			if target.Quantity > 0 {
				exitQty = target.Quantity
			} else {
				exitQty = int(float64(config.TotalQuantity) * target.Percent / 100)
			}

			if exitQty > config.RemainingQty {
				exitQty = config.RemainingQty
			}

			if exitQty > 0 {
				signals = append(signals, ExitSignal{
					Symbol:   pos.Symbol,
					Reason:   ExitReasonTarget,
					Price:    target.Price,
					Quantity: exitQty,
				})

				// Mark as executed
				config.ExecutedLevels = append(config.ExecutedLevels, i)
				config.RemainingQty -= exitQty
			}
		}
	}

	return signals
}

// shouldExitMIS checks if MIS positions should be auto-exited.
func (em *DefaultExitManager) shouldExitMIS() bool {
	status := utils.GetMarketStatus()
	return status == models.MarketMISSquareOffWarn
}

// ExecuteExitSignal executes an exit signal by placing an order.
func (em *DefaultExitManager) ExecuteExitSignal(ctx context.Context, signal ExitSignal) error {
	position, err := em.positionManager.GetPosition(ctx, signal.Symbol)
	if err != nil {
		return fmt.Errorf("getting position: %w", err)
	}

	// Determine order side (opposite of position)
	var side models.OrderSide
	if position.Quantity > 0 {
		side = models.OrderSideSell
	} else {
		side = models.OrderSideBuy
	}

	// Create exit order
	order := &models.Order{
		Symbol:   signal.Symbol,
		Exchange: position.Exchange,
		Side:     side,
		Type:     models.OrderTypeMarket,
		Product:  position.Product,
		Quantity: signal.Quantity,
		Validity: "DAY",
		Tag:      fmt.Sprintf("exit_%s", signal.Reason),
	}

	// Place the order
	result, err := em.broker.PlaceOrder(ctx, order)
	if err != nil {
		return fmt.Errorf("placing exit order: %w", err)
	}

	if result.Status == "REJECTED" {
		return fmt.Errorf("exit order rejected: %s", result.Message)
	}

	// Clean up configurations if fully exited
	em.cleanupAfterExit(signal.Symbol, signal.Quantity, position.Quantity)

	return nil
}

// cleanupAfterExit removes configurations if position is fully closed.
func (em *DefaultExitManager) cleanupAfterExit(symbol string, exitedQty, totalQty int) {
	em.mu.Lock()
	defer em.mu.Unlock()

	absTotal := totalQty
	if absTotal < 0 {
		absTotal = -absTotal
	}

	// If fully exited, clean up all configurations
	if exitedQty >= absTotal {
		delete(em.trailingStops, symbol)
		delete(em.timeExits, symbol)
		delete(em.scaleOuts, symbol)
	}
}

// RemoveTrailingStop removes trailing stop for a symbol.
func (em *DefaultExitManager) RemoveTrailingStop(symbol string) {
	em.mu.Lock()
	defer em.mu.Unlock()
	delete(em.trailingStops, symbol)
}

// RemoveTimeExit removes time-based exit for a symbol.
func (em *DefaultExitManager) RemoveTimeExit(symbol string) {
	em.mu.Lock()
	defer em.mu.Unlock()
	delete(em.timeExits, symbol)
}

// RemoveScaleOut removes scale-out configuration for a symbol.
func (em *DefaultExitManager) RemoveScaleOut(symbol string) {
	em.mu.Lock()
	defer em.mu.Unlock()
	delete(em.scaleOuts, symbol)
}

// GetTrailingStopStatus returns the current trailing stop status for a symbol.
func (em *DefaultExitManager) GetTrailingStopStatus(symbol string) (*TrailingStopConfig, bool) {
	em.mu.RLock()
	defer em.mu.RUnlock()
	config, ok := em.trailingStops[symbol]
	if !ok {
		return nil, false
	}
	// Return a copy
	copy := *config
	return &copy, true
}

// GetTimeExitStatus returns the current time exit status for a symbol.
func (em *DefaultExitManager) GetTimeExitStatus(symbol string) (*TimeExitConfig, bool) {
	em.mu.RLock()
	defer em.mu.RUnlock()
	config, ok := em.timeExits[symbol]
	if !ok {
		return nil, false
	}
	// Return a copy
	copy := *config
	return &copy, true
}

// GetScaleOutStatus returns the current scale-out status for a symbol.
func (em *DefaultExitManager) GetScaleOutStatus(symbol string) (*ScaleOutConfig, bool) {
	em.mu.RLock()
	defer em.mu.RUnlock()
	config, ok := em.scaleOuts[symbol]
	if !ok {
		return nil, false
	}
	// Return a copy
	configCopy := *config
	configCopy.Targets = make([]ScaleOutTarget, len(config.Targets))
	for i, t := range config.Targets {
		configCopy.Targets[i] = t
	}
	configCopy.ExecutedLevels = make([]int, len(config.ExecutedLevels))
	for i, l := range config.ExecutedLevels {
		configCopy.ExecutedLevels[i] = l
	}
	return &configCopy, true
}

// GetAllExitConfigs returns all active exit configurations.
func (em *DefaultExitManager) GetAllExitConfigs() map[string][]string {
	em.mu.RLock()
	defer em.mu.RUnlock()

	configs := make(map[string][]string)

	for symbol := range em.trailingStops {
		configs[symbol] = append(configs[symbol], "trailing_stop")
	}

	for symbol := range em.timeExits {
		configs[symbol] = append(configs[symbol], "time_exit")
	}

	for symbol := range em.scaleOuts {
		configs[symbol] = append(configs[symbol], "scale_out")
	}

	return configs
}
