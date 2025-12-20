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

// PledgeStatus represents the status of a pledge request.
type PledgeStatus string

const (
	PledgePending  PledgeStatus = "PENDING"
	PledgeApproved PledgeStatus = "APPROVED"
	PledgeRejected PledgeStatus = "REJECTED"
	PledgeReleased PledgeStatus = "RELEASED"
)

// PledgeableHolding represents a holding that can be pledged.
type PledgeableHolding struct {
	Symbol          string
	Quantity        int
	LTP             float64
	MarketValue     float64
	HaircutPercent  float64
	CollateralValue float64
	PledgedQty      int
	AvailableQty    int
	IsPledged       bool
}

// PledgeRequest represents a pledge/unpledge request.
type PledgeRequest struct {
	ID          string
	Symbol      string
	Quantity    int
	Type        string // "PLEDGE" or "UNPLEDGE"
	Status      PledgeStatus
	RequestedAt time.Time
	ProcessedAt *time.Time
	Reason      string
}

// CollateralSummary represents overall collateral summary.
type CollateralSummary struct {
	TotalMarketValue    float64
	TotalCollateralValue float64
	TotalPledgedValue   float64
	AvailableToPledge   float64
	MarginBenefit       float64
	Timestamp           time.Time
}

// CollateralManager manages pledged holdings for margin benefit.
type CollateralManager struct {
	broker       broker.Broker
	haircuts     map[string]float64 // symbol -> haircut percentage
	pledgeReqs   []PledgeRequest
	mu           sync.RWMutex
}

// NewCollateralManager creates a new collateral manager.
func NewCollateralManager(b broker.Broker) *CollateralManager {
	return &CollateralManager{
		broker:     b,
		haircuts:   make(map[string]float64),
		pledgeReqs: make([]PledgeRequest, 0),
	}
}

// SetHaircut sets the haircut percentage for a symbol.
func (c *CollateralManager) SetHaircut(symbol string, haircut float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.haircuts[symbol] = haircut
}

// GetHaircut returns the haircut percentage for a symbol.
func (c *CollateralManager) GetHaircut(symbol string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if haircut, ok := c.haircuts[symbol]; ok {
		return haircut
	}

	// Default haircuts based on typical values
	// Large cap: 10-15%, Mid cap: 15-25%, Small cap: 25-50%
	return 20.0 // Default 20% haircut
}

// GetPledgeableHoldings returns holdings that can be pledged.
func (c *CollateralManager) GetPledgeableHoldings(ctx context.Context) ([]PledgeableHolding, error) {
	holdings, err := c.broker.GetHoldings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get holdings: %w", err)
	}

	var pledgeable []PledgeableHolding
	for _, h := range holdings {
		haircut := c.GetHaircut(h.Symbol)
		marketValue := h.LTP * float64(h.Quantity)
		collateralValue := marketValue * (1 - haircut/100)

		pledgeable = append(pledgeable, PledgeableHolding{
			Symbol:          h.Symbol,
			Quantity:        h.Quantity,
			LTP:             h.LTP,
			MarketValue:     marketValue,
			HaircutPercent:  haircut,
			CollateralValue: collateralValue,
			PledgedQty:      0, // Would be fetched from broker in real implementation
			AvailableQty:    h.Quantity,
			IsPledged:       false,
		})
	}

	return pledgeable, nil
}

// CalculateCollateralValue calculates collateral value after haircut.
func (c *CollateralManager) CalculateCollateralValue(symbol string, quantity int, ltp float64) float64 {
	haircut := c.GetHaircut(symbol)
	marketValue := ltp * float64(quantity)
	return marketValue * (1 - haircut/100)
}

// GetCollateralSummary returns overall collateral summary.
func (c *CollateralManager) GetCollateralSummary(ctx context.Context) (*CollateralSummary, error) {
	holdings, err := c.GetPledgeableHoldings(ctx)
	if err != nil {
		return nil, err
	}

	var totalMarket, totalCollateral, totalPledged float64
	for _, h := range holdings {
		totalMarket += h.MarketValue
		totalCollateral += h.CollateralValue
		if h.IsPledged {
			totalPledged += h.CollateralValue
		}
	}

	// Get current margin benefit from broker
	balance, err := c.broker.GetBalance(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}

	return &CollateralSummary{
		TotalMarketValue:     totalMarket,
		TotalCollateralValue: totalCollateral,
		TotalPledgedValue:    totalPledged,
		AvailableToPledge:    totalCollateral - totalPledged,
		MarginBenefit:        balance.CollateralValue,
		Timestamp:            time.Now(),
	}, nil
}

// CreatePledgeRequest creates a pledge request.
func (c *CollateralManager) CreatePledgeRequest(symbol string, quantity int) (*PledgeRequest, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := &PledgeRequest{
		ID:          fmt.Sprintf("PLG-%d", time.Now().UnixNano()),
		Symbol:      symbol,
		Quantity:    quantity,
		Type:        "PLEDGE",
		Status:      PledgePending,
		RequestedAt: time.Now(),
	}

	c.pledgeReqs = append(c.pledgeReqs, *req)
	return req, nil
}

// CreateUnpledgeRequest creates an unpledge request.
func (c *CollateralManager) CreateUnpledgeRequest(symbol string, quantity int) (*PledgeRequest, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	req := &PledgeRequest{
		ID:          fmt.Sprintf("UPL-%d", time.Now().UnixNano()),
		Symbol:      symbol,
		Quantity:    quantity,
		Type:        "UNPLEDGE",
		Status:      PledgePending,
		RequestedAt: time.Now(),
	}

	c.pledgeReqs = append(c.pledgeReqs, *req)
	return req, nil
}

// GetPledgeRequests returns all pledge requests.
func (c *CollateralManager) GetPledgeRequests() []PledgeRequest {
	c.mu.RLock()
	defer c.mu.RUnlock()

	reqs := make([]PledgeRequest, len(c.pledgeReqs))
	copy(reqs, c.pledgeReqs)
	return reqs
}

// GetPendingRequests returns pending pledge requests.
func (c *CollateralManager) GetPendingRequests() []PledgeRequest {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var pending []PledgeRequest
	for _, req := range c.pledgeReqs {
		if req.Status == PledgePending {
			pending = append(pending, req)
		}
	}
	return pending
}

// UpdateRequestStatus updates the status of a pledge request.
func (c *CollateralManager) UpdateRequestStatus(id string, status PledgeStatus, reason string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := range c.pledgeReqs {
		if c.pledgeReqs[i].ID == id {
			c.pledgeReqs[i].Status = status
			c.pledgeReqs[i].Reason = reason
			now := time.Now()
			c.pledgeReqs[i].ProcessedAt = &now
			return nil
		}
	}

	return fmt.Errorf("pledge request not found: %s", id)
}

// GetMarginBenefit returns the margin benefit from pledged holdings.
func (c *CollateralManager) GetMarginBenefit(ctx context.Context) (float64, error) {
	balance, err := c.broker.GetBalance(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get balance: %w", err)
	}
	return balance.CollateralValue, nil
}

// CheckCollateralDrop checks if collateral value has dropped significantly.
func (c *CollateralManager) CheckCollateralDrop(ctx context.Context, threshold float64) (bool, float64, error) {
	summary, err := c.GetCollateralSummary(ctx)
	if err != nil {
		return false, 0, err
	}

	// Compare current collateral with pledged value
	if summary.TotalPledgedValue > 0 {
		dropPercent := ((summary.TotalPledgedValue - summary.MarginBenefit) / summary.TotalPledgedValue) * 100
		if dropPercent > threshold {
			return true, dropPercent, nil
		}
	}

	return false, 0, nil
}

// GetHoldingCollateralValue returns collateral value for a specific holding.
func (c *CollateralManager) GetHoldingCollateralValue(holding *models.Holding) float64 {
	return c.CalculateCollateralValue(holding.Symbol, holding.Quantity, holding.LTP)
}

// SimulatePledge simulates pledging holdings and returns potential margin benefit.
func (c *CollateralManager) SimulatePledge(holdings []PledgeableHolding) float64 {
	var totalBenefit float64
	for _, h := range holdings {
		if !h.IsPledged && h.AvailableQty > 0 {
			totalBenefit += h.CollateralValue
		}
	}
	return totalBenefit
}
