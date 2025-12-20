// Package trading provides trading operations including position management.
package trading

import (
	"context"
	"fmt"
	"sync"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

// DefaultPositionManager implements the PositionManager interface.
// Requirements: 8.1-8.5
type DefaultPositionManager struct {
	broker broker.Broker
	mu     sync.RWMutex

	// Cached positions for quick access
	positions map[string]*models.Position
}

// NewPositionManager creates a new position manager.
func NewPositionManager(b broker.Broker) *DefaultPositionManager {
	return &DefaultPositionManager{
		broker:    b,
		positions: make(map[string]*models.Position),
	}
}

// GetPositions fetches current positions from the broker.
// Requirement 8.1: WHEN a user requests positions, THE Position_Manager SHALL fetch current positions from Zerodha
func (pm *DefaultPositionManager) GetPositions(ctx context.Context) ([]models.Position, error) {
	positions, err := pm.broker.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching positions from broker: %w", err)
	}

	// Update cache
	pm.mu.Lock()
	pm.positions = make(map[string]*models.Position)
	for i := range positions {
		pm.positions[positions[i].Symbol] = &positions[i]
	}
	pm.mu.Unlock()

	return positions, nil
}

// GetPosition fetches a specific position by symbol.
func (pm *DefaultPositionManager) GetPosition(ctx context.Context, symbol string) (*models.Position, error) {
	// First check cache
	pm.mu.RLock()
	if pos, ok := pm.positions[symbol]; ok {
		pm.mu.RUnlock()
		return pos, nil
	}
	pm.mu.RUnlock()

	// Fetch fresh positions
	positions, err := pm.GetPositions(ctx)
	if err != nil {
		return nil, err
	}

	for i := range positions {
		if positions[i].Symbol == symbol {
			return &positions[i], nil
		}
	}

	return nil, fmt.Errorf("position not found for symbol: %s", symbol)
}

// ExitPosition exits a single position by placing a closing order.
// Requirement 8.3: WHEN a user exits a position, THE Position_Manager SHALL place a closing order for the full quantity
func (pm *DefaultPositionManager) ExitPosition(ctx context.Context, symbol string) error {
	position, err := pm.GetPosition(ctx, symbol)
	if err != nil {
		return fmt.Errorf("getting position: %w", err)
	}

	if position.Quantity == 0 {
		return fmt.Errorf("no open position for symbol: %s", symbol)
	}

	// Determine order side (opposite of position)
	var side models.OrderSide
	if position.Quantity > 0 {
		side = models.OrderSideSell
	} else {
		side = models.OrderSideBuy
	}

	// Calculate absolute quantity
	qty := position.Quantity
	if qty < 0 {
		qty = -qty
	}

	// Create closing order
	order := &models.Order{
		Symbol:   symbol,
		Exchange: position.Exchange,
		Side:     side,
		Type:     models.OrderTypeMarket,
		Product:  position.Product,
		Quantity: qty,
		Validity: "DAY",
		Tag:      "exit_position",
	}

	// Place the order
	result, err := pm.broker.PlaceOrder(ctx, order)
	if err != nil {
		return fmt.Errorf("placing exit order: %w", err)
	}

	if result.Status == "REJECTED" {
		return fmt.Errorf("exit order rejected: %s", result.Message)
	}

	// Remove from cache
	pm.mu.Lock()
	delete(pm.positions, symbol)
	pm.mu.Unlock()

	return nil
}

// ExitAllPositions exits all open positions.
// Requirement 8.4: THE Position_Manager SHALL support exiting all positions with a single command
func (pm *DefaultPositionManager) ExitAllPositions(ctx context.Context) error {
	positions, err := pm.GetPositions(ctx)
	if err != nil {
		return fmt.Errorf("fetching positions: %w", err)
	}

	var exitErrors []error
	for _, pos := range positions {
		if pos.Quantity == 0 {
			continue
		}

		if err := pm.ExitPosition(ctx, pos.Symbol); err != nil {
			exitErrors = append(exitErrors, fmt.Errorf("%s: %w", pos.Symbol, err))
		}
	}

	if len(exitErrors) > 0 {
		return fmt.Errorf("failed to exit some positions: %v", exitErrors)
	}

	return nil
}

// GetUnrealizedPnL calculates total unrealized P&L across all positions.
func (pm *DefaultPositionManager) GetUnrealizedPnL(ctx context.Context) (float64, error) {
	positions, err := pm.GetPositions(ctx)
	if err != nil {
		return 0, fmt.Errorf("fetching positions: %w", err)
	}

	var totalPnL float64
	for _, pos := range positions {
		totalPnL += pos.PnL
	}

	return totalPnL, nil
}

// PositionSummary represents a summary of all positions.
// Requirement 8.2: THE Position_Manager SHALL display symbol, quantity, average price, LTP, P&L, and P&L percentage
type PositionSummary struct {
	Positions       []PositionDetail
	TotalPnL        float64
	TotalPnLPercent float64
	TotalValue      float64
	PositionCount   int
}

// PositionDetail represents detailed position information.
type PositionDetail struct {
	Symbol       string
	Exchange     models.Exchange
	Product      models.ProductType
	Quantity     int
	AveragePrice float64
	LTP          float64
	PnL          float64
	PnLPercent   float64
	Value        float64
	ExitCommand  string // Quick exit command for CLI display
}

// GetPositionSummary returns a summary of all positions with details.
// Requirement 8.2, 8.5: Display position details and quick exit commands
func (pm *DefaultPositionManager) GetPositionSummary(ctx context.Context) (*PositionSummary, error) {
	positions, err := pm.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching positions: %w", err)
	}

	summary := &PositionSummary{
		Positions: make([]PositionDetail, 0, len(positions)),
	}

	var totalInvested float64

	for _, pos := range positions {
		if pos.Quantity == 0 {
			continue
		}

		detail := PositionDetail{
			Symbol:       pos.Symbol,
			Exchange:     pos.Exchange,
			Product:      pos.Product,
			Quantity:     pos.Quantity,
			AveragePrice: pos.AveragePrice,
			LTP:          pos.LTP,
			PnL:          pos.PnL,
			PnLPercent:   pos.PnLPercent,
			Value:        pos.Value,
			ExitCommand:  fmt.Sprintf("exit %s", pos.Symbol), // Requirement 8.5
		}

		summary.Positions = append(summary.Positions, detail)
		summary.TotalPnL += pos.PnL
		summary.TotalValue += pos.Value

		// Calculate invested value for percentage
		qty := pos.Quantity
		if qty < 0 {
			qty = -qty
		}
		totalInvested += pos.AveragePrice * float64(qty)
	}

	summary.PositionCount = len(summary.Positions)

	// Calculate total P&L percentage
	if totalInvested > 0 {
		summary.TotalPnLPercent = (summary.TotalPnL / totalInvested) * 100
	}

	return summary, nil
}

// HasOpenPositions returns true if there are any open positions.
func (pm *DefaultPositionManager) HasOpenPositions(ctx context.Context) (bool, error) {
	positions, err := pm.GetPositions(ctx)
	if err != nil {
		return false, err
	}

	for _, pos := range positions {
		if pos.Quantity != 0 {
			return true, nil
		}
	}

	return false, nil
}

// GetPositionsByProduct returns positions filtered by product type.
func (pm *DefaultPositionManager) GetPositionsByProduct(ctx context.Context, product models.ProductType) ([]models.Position, error) {
	positions, err := pm.GetPositions(ctx)
	if err != nil {
		return nil, err
	}

	var filtered []models.Position
	for _, pos := range positions {
		if pos.Product == product && pos.Quantity != 0 {
			filtered = append(filtered, pos)
		}
	}

	return filtered, nil
}

// GetMISPositions returns all MIS (intraday) positions.
func (pm *DefaultPositionManager) GetMISPositions(ctx context.Context) ([]models.Position, error) {
	return pm.GetPositionsByProduct(ctx, models.ProductMIS)
}

// GetCNCPositions returns all CNC (delivery) positions.
func (pm *DefaultPositionManager) GetCNCPositions(ctx context.Context) ([]models.Position, error) {
	return pm.GetPositionsByProduct(ctx, models.ProductCNC)
}

// RefreshPositions forces a refresh of the position cache.
func (pm *DefaultPositionManager) RefreshPositions(ctx context.Context) error {
	_, err := pm.GetPositions(ctx)
	return err
}
