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

// BasketType represents types of baskets.
type BasketType string

const (
	BasketIndex  BasketType = "INDEX"
	BasketSector BasketType = "SECTOR"
	BasketCustom BasketType = "CUSTOM"
)

// BasketConstituent represents a constituent of a basket.
type BasketConstituent struct {
	Symbol   string
	Weight   float64 // Weight in percentage
	Quantity int     // Quantity for order
	Price    float64 // Current price
	Value    float64 // Current value
}

// Basket represents a basket of stocks.
type Basket struct {
	ID           string
	Name         string
	Type         BasketType
	Constituents []BasketConstituent
	TotalValue   float64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// BasketOrder represents an order for a basket.
type BasketOrder struct {
	BasketID    string
	Orders      []models.Order
	TotalValue  float64
	Status      string
	ExecutedAt  *time.Time
}

// BasketPerformance represents basket performance metrics.
type BasketPerformance struct {
	BasketID       string
	BasketName     string
	InvestedValue  float64
	CurrentValue   float64
	PnL            float64
	PnLPercent     float64
	BenchmarkPnL   float64 // Benchmark comparison
	Alpha          float64 // Excess return over benchmark
}

// BasketManager manages basket trading for Indian markets.
type BasketManager struct {
	broker  broker.Broker
	baskets map[string]*Basket
	mu      sync.RWMutex
}

// NewBasketManager creates a new basket manager.
func NewBasketManager(b broker.Broker) *BasketManager {
	return &BasketManager{
		broker:  b,
		baskets: make(map[string]*Basket),
	}
}

// CreateBasket creates a new basket.
func (m *BasketManager) CreateBasket(name string, basketType BasketType, constituents []BasketConstituent) (*Basket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := fmt.Sprintf("BSK-%d", time.Now().UnixNano())
	basket := &Basket{
		ID:           id,
		Name:         name,
		Type:         basketType,
		Constituents: constituents,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// Normalize weights to 100%
	var totalWeight float64
	for _, c := range constituents {
		totalWeight += c.Weight
	}
	if totalWeight > 0 {
		for i := range basket.Constituents {
			basket.Constituents[i].Weight = (basket.Constituents[i].Weight / totalWeight) * 100
		}
	}

	m.baskets[id] = basket
	return basket, nil
}

// GetBasket returns a basket by ID.
func (m *BasketManager) GetBasket(id string) *Basket {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.baskets[id]
}

// GetBasketByName returns a basket by name.
func (m *BasketManager) GetBasketByName(name string) *Basket {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, basket := range m.baskets {
		if basket.Name == name {
			return basket
		}
	}
	return nil
}

// ListBaskets returns all baskets.
func (m *BasketManager) ListBaskets() []*Basket {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var baskets []*Basket
	for _, basket := range m.baskets {
		baskets = append(baskets, basket)
	}
	return baskets
}

// DeleteBasket deletes a basket.
func (m *BasketManager) DeleteBasket(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.baskets[id]; !ok {
		return fmt.Errorf("basket not found: %s", id)
	}
	delete(m.baskets, id)
	return nil
}

// UpdateBasketPrices updates prices for basket constituents.
func (m *BasketManager) UpdateBasketPrices(ctx context.Context, basketID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	basket, ok := m.baskets[basketID]
	if !ok {
		return fmt.Errorf("basket not found: %s", basketID)
	}

	var totalValue float64
	for i := range basket.Constituents {
		quote, err := m.broker.GetQuote(ctx, fmt.Sprintf("NSE:%s", basket.Constituents[i].Symbol))
		if err != nil {
			continue
		}
		basket.Constituents[i].Price = quote.LTP
		basket.Constituents[i].Value = quote.LTP * float64(basket.Constituents[i].Quantity)
		totalValue += basket.Constituents[i].Value
	}

	basket.TotalValue = totalValue
	basket.UpdatedAt = time.Now()
	return nil
}

// CalculateBasketQuantities calculates quantities for each constituent based on investment amount.
func (m *BasketManager) CalculateBasketQuantities(ctx context.Context, basketID string, investmentAmount float64) ([]BasketConstituent, error) {
	m.mu.RLock()
	basket, ok := m.baskets[basketID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("basket not found: %s", basketID)
	}

	var result []BasketConstituent
	for _, c := range basket.Constituents {
		// Get current price
		quote, err := m.broker.GetQuote(ctx, fmt.Sprintf("NSE:%s", c.Symbol))
		if err != nil {
			continue
		}

		// Calculate allocation based on weight
		allocation := investmentAmount * (c.Weight / 100)
		quantity := int(allocation / quote.LTP)

		if quantity > 0 {
			result = append(result, BasketConstituent{
				Symbol:   c.Symbol,
				Weight:   c.Weight,
				Quantity: quantity,
				Price:    quote.LTP,
				Value:    quote.LTP * float64(quantity),
			})
		}
	}

	return result, nil
}

// CreateBasketOrder creates orders for all basket constituents.
func (m *BasketManager) CreateBasketOrder(ctx context.Context, basketID string, side models.OrderSide, investmentAmount float64, product models.ProductType) (*BasketOrder, error) {
	constituents, err := m.CalculateBasketQuantities(ctx, basketID, investmentAmount)
	if err != nil {
		return nil, err
	}

	basket := m.GetBasket(basketID)
	if basket == nil {
		return nil, fmt.Errorf("basket not found: %s", basketID)
	}

	var orders []models.Order
	var totalValue float64

	for _, c := range constituents {
		order := models.Order{
			Symbol:   c.Symbol,
			Exchange: models.NSE,
			Side:     side,
			Type:     models.OrderTypeMarket,
			Product:  product,
			Quantity: c.Quantity,
			Price:    c.Price,
		}
		orders = append(orders, order)
		totalValue += c.Value
	}

	return &BasketOrder{
		BasketID:   basketID,
		Orders:     orders,
		TotalValue: totalValue,
		Status:     "PENDING",
	}, nil
}

// ExecuteBasketOrder executes all orders in a basket order.
func (m *BasketManager) ExecuteBasketOrder(ctx context.Context, basketOrder *BasketOrder) error {
	for i := range basketOrder.Orders {
		_, err := m.broker.PlaceOrder(ctx, &basketOrder.Orders[i])
		if err != nil {
			return fmt.Errorf("failed to place order for %s: %w", basketOrder.Orders[i].Symbol, err)
		}
	}

	now := time.Now()
	basketOrder.ExecutedAt = &now
	basketOrder.Status = "EXECUTED"
	return nil
}

// GetBasketPerformance calculates basket performance vs benchmark.
func (m *BasketManager) GetBasketPerformance(ctx context.Context, basketID string, holdings []models.Holding, benchmarkSymbol string) (*BasketPerformance, error) {
	basket := m.GetBasket(basketID)
	if basket == nil {
		return nil, fmt.Errorf("basket not found: %s", basketID)
	}

	// Calculate basket value from holdings
	var investedValue, currentValue float64
	holdingMap := make(map[string]models.Holding)
	for _, h := range holdings {
		holdingMap[h.Symbol] = h
	}

	for _, c := range basket.Constituents {
		if h, ok := holdingMap[c.Symbol]; ok {
			investedValue += h.InvestedValue
			currentValue += h.CurrentValue
		}
	}

	pnl := currentValue - investedValue
	pnlPercent := 0.0
	if investedValue > 0 {
		pnlPercent = (pnl / investedValue) * 100
	}

	// Get benchmark performance
	var benchmarkPnL float64
	if benchmarkSymbol != "" {
		quote, err := m.broker.GetQuote(ctx, fmt.Sprintf("NSE:%s", benchmarkSymbol))
		if err == nil && quote.Close > 0 {
			benchmarkPnL = quote.ChangePercent
		}
	}

	alpha := pnlPercent - benchmarkPnL

	return &BasketPerformance{
		BasketID:      basketID,
		BasketName:    basket.Name,
		InvestedValue: investedValue,
		CurrentValue:  currentValue,
		PnL:           pnl,
		PnLPercent:    pnlPercent,
		BenchmarkPnL:  benchmarkPnL,
		Alpha:         alpha,
	}, nil
}

// RebalanceBasket calculates rebalancing orders to match target weights.
func (m *BasketManager) RebalanceBasket(ctx context.Context, basketID string, holdings []models.Holding) ([]models.Order, error) {
	basket := m.GetBasket(basketID)
	if basket == nil {
		return nil, fmt.Errorf("basket not found: %s", basketID)
	}

	// Calculate current portfolio value
	holdingMap := make(map[string]models.Holding)
	var totalValue float64
	for _, h := range holdings {
		holdingMap[h.Symbol] = h
		totalValue += h.CurrentValue
	}

	if totalValue == 0 {
		return nil, fmt.Errorf("no holdings to rebalance")
	}

	var orders []models.Order

	for _, c := range basket.Constituents {
		targetValue := totalValue * (c.Weight / 100)
		currentValue := 0.0
		currentQty := 0

		if h, ok := holdingMap[c.Symbol]; ok {
			currentValue = h.CurrentValue
			currentQty = h.Quantity
		}

		// Get current price
		quote, err := m.broker.GetQuote(ctx, fmt.Sprintf("NSE:%s", c.Symbol))
		if err != nil {
			continue
		}

		diff := targetValue - currentValue
		qtyDiff := int(diff / quote.LTP)

		if qtyDiff > 0 {
			// Need to buy
			orders = append(orders, models.Order{
				Symbol:   c.Symbol,
				Exchange: models.NSE,
				Side:     models.OrderSideBuy,
				Type:     models.OrderTypeMarket,
				Product:  models.ProductCNC,
				Quantity: qtyDiff,
			})
		} else if qtyDiff < 0 && currentQty > 0 {
			// Need to sell
			sellQty := -qtyDiff
			if sellQty > currentQty {
				sellQty = currentQty
			}
			orders = append(orders, models.Order{
				Symbol:   c.Symbol,
				Exchange: models.NSE,
				Side:     models.OrderSideSell,
				Type:     models.OrderTypeMarket,
				Product:  models.ProductCNC,
				Quantity: sellQty,
			})
		}
	}

	return orders, nil
}

// GetNifty50Constituents returns NIFTY 50 constituents with weights.
func (m *BasketManager) GetNifty50Constituents() []BasketConstituent {
	// Top NIFTY 50 constituents with approximate weights
	return []BasketConstituent{
		{Symbol: "RELIANCE", Weight: 10.5},
		{Symbol: "HDFCBANK", Weight: 8.2},
		{Symbol: "ICICIBANK", Weight: 7.5},
		{Symbol: "INFY", Weight: 6.8},
		{Symbol: "TCS", Weight: 4.5},
		{Symbol: "KOTAKBANK", Weight: 3.8},
		{Symbol: "HINDUNILVR", Weight: 3.5},
		{Symbol: "ITC", Weight: 3.2},
		{Symbol: "SBIN", Weight: 3.0},
		{Symbol: "BHARTIARTL", Weight: 2.8},
		{Symbol: "AXISBANK", Weight: 2.5},
		{Symbol: "LT", Weight: 2.3},
		{Symbol: "BAJFINANCE", Weight: 2.2},
		{Symbol: "ASIANPAINT", Weight: 2.0},
		{Symbol: "MARUTI", Weight: 1.8},
	}
}

// GetBankNiftyConstituents returns Bank NIFTY constituents with weights.
func (m *BasketManager) GetBankNiftyConstituents() []BasketConstituent {
	return []BasketConstituent{
		{Symbol: "HDFCBANK", Weight: 28.0},
		{Symbol: "ICICIBANK", Weight: 24.0},
		{Symbol: "KOTAKBANK", Weight: 12.0},
		{Symbol: "AXISBANK", Weight: 11.0},
		{Symbol: "SBIN", Weight: 10.0},
		{Symbol: "INDUSINDBK", Weight: 5.0},
		{Symbol: "BANDHANBNK", Weight: 3.0},
		{Symbol: "FEDERALBNK", Weight: 2.5},
		{Symbol: "IDFCFIRSTB", Weight: 2.0},
		{Symbol: "PNB", Weight: 1.5},
		{Symbol: "AUBANK", Weight: 1.0},
	}
}

// CreateIndexBasket creates a basket based on an index.
func (m *BasketManager) CreateIndexBasket(indexName string) (*Basket, error) {
	var constituents []BasketConstituent

	switch indexName {
	case "NIFTY50":
		constituents = m.GetNifty50Constituents()
	case "BANKNIFTY":
		constituents = m.GetBankNiftyConstituents()
	default:
		return nil, fmt.Errorf("unknown index: %s", indexName)
	}

	return m.CreateBasket(indexName, BasketIndex, constituents)
}

// SortConstituentsByWeight sorts constituents by weight descending.
func SortConstituentsByWeight(constituents []BasketConstituent) {
	sort.Slice(constituents, func(i, j int) bool {
		return constituents[i].Weight > constituents[j].Weight
	})
}
