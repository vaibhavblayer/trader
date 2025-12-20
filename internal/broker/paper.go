// Package broker provides broker integration implementations.
package broker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"zerodha-trader/internal/models"
)

// PaperBroker implements the Broker interface for paper trading simulation.
type PaperBroker struct {
	// Real broker for market data
	dataBroker Broker
	ticker     Ticker
	
	// Simulated state
	positions  map[string]*models.Position
	holdings   map[string]*models.Holding
	orders     map[string]*models.Order
	gttOrders  map[string]*models.GTTOrder
	balance    *models.Balance
	
	// Order tracking
	orderCounter int
	gttCounter   int
	
	// Price cache for simulation
	priceCache map[string]float64
	
	mu sync.RWMutex
}

// PaperBrokerConfig holds configuration for paper broker.
type PaperBrokerConfig struct {
	DataBroker     Broker
	Ticker         Ticker
	InitialBalance float64
}

// NewPaperBroker creates a new paper trading broker.
func NewPaperBroker(cfg PaperBrokerConfig) *PaperBroker {
	initialBalance := cfg.InitialBalance
	if initialBalance == 0 {
		initialBalance = 1000000 // 10 lakhs default
	}
	
	return &PaperBroker{
		dataBroker: cfg.DataBroker,
		ticker:     cfg.Ticker,
		positions:  make(map[string]*models.Position),
		holdings:   make(map[string]*models.Holding),
		orders:     make(map[string]*models.Order),
		gttOrders:  make(map[string]*models.GTTOrder),
		balance: &models.Balance{
			AvailableCash: initialBalance,
			TotalEquity:   initialBalance,
		},
		priceCache: make(map[string]float64),
	}
}


// Login is a no-op for paper trading.
func (p *PaperBroker) Login(ctx context.Context) error {
	return nil
}

// Logout is a no-op for paper trading.
func (p *PaperBroker) Logout(ctx context.Context) error {
	return nil
}

// IsAuthenticated always returns true for paper trading.
func (p *PaperBroker) IsAuthenticated() bool {
	return true
}

// RefreshSession is a no-op for paper trading.
func (p *PaperBroker) RefreshSession(ctx context.Context) error {
	return nil
}

// GetQuote fetches real-time quote from the data broker.
func (p *PaperBroker) GetQuote(ctx context.Context, symbol string) (*models.Quote, error) {
	if p.dataBroker != nil {
		quote, err := p.dataBroker.GetQuote(ctx, symbol)
		if err == nil {
			p.mu.Lock()
			p.priceCache[symbol] = quote.LTP
			p.mu.Unlock()
		}
		return quote, err
	}
	return nil, fmt.Errorf("no data broker configured")
}

// GetHistorical fetches historical data from the data broker.
func (p *PaperBroker) GetHistorical(ctx context.Context, req HistoricalRequest) ([]models.Candle, error) {
	if p.dataBroker != nil {
		return p.dataBroker.GetHistorical(ctx, req)
	}
	return nil, fmt.Errorf("no data broker configured")
}

// GetInstruments fetches instruments from the data broker.
func (p *PaperBroker) GetInstruments(ctx context.Context, exchange models.Exchange) ([]models.Instrument, error) {
	if p.dataBroker != nil {
		return p.dataBroker.GetInstruments(ctx, exchange)
	}
	return nil, fmt.Errorf("no data broker configured")
}

// PlaceOrder simulates order placement.
func (p *PaperBroker) PlaceOrder(ctx context.Context, order *models.Order) (*OrderResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	// Generate order ID
	p.orderCounter++
	orderID := fmt.Sprintf("PAPER_%d_%d", time.Now().Unix(), p.orderCounter)
	
	// Get current price
	price := p.getPrice(order.Symbol)
	if price == 0 {
		// Try to fetch from data broker
		if p.dataBroker != nil {
			quote, err := p.dataBroker.GetQuote(ctx, order.Symbol)
			if err == nil {
				price = quote.LTP
				p.priceCache[order.Symbol] = price
			}
		}
	}
	
	// Determine execution price
	execPrice := price
	if order.Type == models.OrderTypeLimit {
		execPrice = order.Price
	}
	
	// Check if order can be filled
	canFill := true
	if order.Type == models.OrderTypeLimit {
		if order.Side == models.OrderSideBuy && price > order.Price {
			canFill = false
		}
		if order.Side == models.OrderSideSell && price < order.Price {
			canFill = false
		}
	}
	
	// Calculate order value
	orderValue := execPrice * float64(order.Quantity)
	
	// Check balance for buy orders
	if order.Side == models.OrderSideBuy && canFill {
		if p.balance.AvailableCash < orderValue {
			return nil, fmt.Errorf("insufficient funds: need %.2f, have %.2f", orderValue, p.balance.AvailableCash)
		}
	}
	
	// Create order record
	newOrder := &models.Order{
		ID:           orderID,
		Symbol:       order.Symbol,
		Exchange:     order.Exchange,
		Side:         order.Side,
		Type:         order.Type,
		Product:      order.Product,
		Quantity:     order.Quantity,
		Price:        order.Price,
		TriggerPrice: order.TriggerPrice,
		Validity:     order.Validity,
		Tag:          order.Tag,
		PlacedAt:     time.Now(),
	}
	
	if canFill {
		newOrder.Status = "COMPLETE"
		newOrder.FilledQty = order.Quantity
		newOrder.AveragePrice = execPrice
		
		// Update position
		p.updatePosition(order.Symbol, order.Exchange, order.Product, order.Side, order.Quantity, execPrice)
		
		// Update balance
		if order.Side == models.OrderSideBuy {
			p.balance.AvailableCash -= orderValue
		} else {
			p.balance.AvailableCash += orderValue
		}
	} else {
		newOrder.Status = "OPEN"
	}
	
	p.orders[orderID] = newOrder
	
	return &OrderResult{
		OrderID: orderID,
		Status:  newOrder.Status,
		Message: "Paper order placed",
	}, nil
}


// ModifyOrder simulates order modification.
func (p *PaperBroker) ModifyOrder(ctx context.Context, orderID string, order *models.Order) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	existing, ok := p.orders[orderID]
	if !ok {
		return fmt.Errorf("order not found: %s", orderID)
	}
	
	if existing.Status != "OPEN" {
		return fmt.Errorf("cannot modify order with status: %s", existing.Status)
	}
	
	existing.Price = order.Price
	existing.TriggerPrice = order.TriggerPrice
	existing.Quantity = order.Quantity
	
	return nil
}

// CancelOrder simulates order cancellation.
func (p *PaperBroker) CancelOrder(ctx context.Context, orderID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	order, ok := p.orders[orderID]
	if !ok {
		return fmt.Errorf("order not found: %s", orderID)
	}
	
	if order.Status != "OPEN" {
		return fmt.Errorf("cannot cancel order with status: %s", order.Status)
	}
	
	order.Status = "CANCELLED"
	return nil
}

// GetOrders returns all paper orders.
func (p *PaperBroker) GetOrders(ctx context.Context) ([]models.Order, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	orders := make([]models.Order, 0, len(p.orders))
	for _, o := range p.orders {
		orders = append(orders, *o)
	}
	return orders, nil
}

// GetOrderHistory returns paper orders within a date range.
func (p *PaperBroker) GetOrderHistory(ctx context.Context, from, to time.Time) ([]models.Order, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	var orders []models.Order
	for _, o := range p.orders {
		if (o.PlacedAt.Equal(from) || o.PlacedAt.After(from)) && (o.PlacedAt.Equal(to) || o.PlacedAt.Before(to)) {
			orders = append(orders, *o)
		}
	}
	return orders, nil
}

// PlaceGTT simulates GTT order placement.
func (p *PaperBroker) PlaceGTT(ctx context.Context, gtt *models.GTTOrder) (*GTTResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.gttCounter++
	gttID := fmt.Sprintf("PAPER_GTT_%d_%d", time.Now().Unix(), p.gttCounter)
	
	newGTT := &models.GTTOrder{
		ID:           gttID,
		Symbol:       gtt.Symbol,
		Exchange:     gtt.Exchange,
		TriggerType:  gtt.TriggerType,
		TriggerPrice: gtt.TriggerPrice,
		LastPrice:    gtt.LastPrice,
		Orders:       gtt.Orders,
		Status:       "ACTIVE",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	
	p.gttOrders[gttID] = newGTT
	
	return &GTTResult{
		TriggerID: gttID,
		Status:    "ACTIVE",
		Message:   "Paper GTT placed",
	}, nil
}

// ModifyGTT simulates GTT modification.
func (p *PaperBroker) ModifyGTT(ctx context.Context, gttID string, gtt *models.GTTOrder) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	existing, ok := p.gttOrders[gttID]
	if !ok {
		return fmt.Errorf("GTT not found: %s", gttID)
	}
	
	existing.TriggerPrice = gtt.TriggerPrice
	existing.Orders = gtt.Orders
	existing.UpdatedAt = time.Now()
	
	return nil
}

// CancelGTT simulates GTT cancellation.
func (p *PaperBroker) CancelGTT(ctx context.Context, gttID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	gtt, ok := p.gttOrders[gttID]
	if !ok {
		return fmt.Errorf("GTT not found: %s", gttID)
	}
	
	gtt.Status = "CANCELLED"
	gtt.UpdatedAt = time.Now()
	return nil
}

// GetGTTs returns all paper GTT orders.
func (p *PaperBroker) GetGTTs(ctx context.Context) ([]models.GTTOrder, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	gtts := make([]models.GTTOrder, 0, len(p.gttOrders))
	for _, g := range p.gttOrders {
		gtts = append(gtts, *g)
	}
	return gtts, nil
}


// GetPositions returns simulated positions.
func (p *PaperBroker) GetPositions(ctx context.Context) ([]models.Position, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	positions := make([]models.Position, 0, len(p.positions))
	for _, pos := range p.positions {
		// Update P&L with current price
		price := p.priceCache[pos.Symbol]
		if price > 0 {
			pos.LTP = price
			pos.PnL = (price - pos.AveragePrice) * float64(pos.Quantity)
			if pos.AveragePrice > 0 {
				pos.PnLPercent = ((price - pos.AveragePrice) / pos.AveragePrice) * 100
			}
			pos.Value = price * float64(pos.Quantity)
		}
		positions = append(positions, *pos)
	}
	return positions, nil
}

// GetHoldings returns simulated holdings.
func (p *PaperBroker) GetHoldings(ctx context.Context) ([]models.Holding, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	holdings := make([]models.Holding, 0, len(p.holdings))
	for _, h := range p.holdings {
		// Update P&L with current price
		price := p.priceCache[h.Symbol]
		if price > 0 {
			h.LTP = price
			h.CurrentValue = price * float64(h.Quantity)
			h.PnL = h.CurrentValue - h.InvestedValue
			if h.InvestedValue > 0 {
				h.PnLPercent = (h.PnL / h.InvestedValue) * 100
			}
		}
		holdings = append(holdings, *h)
	}
	return holdings, nil
}

// GetBalance returns simulated balance.
func (p *PaperBroker) GetBalance(ctx context.Context) (*models.Balance, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	// Calculate total equity including positions
	totalEquity := p.balance.AvailableCash
	for _, pos := range p.positions {
		price := p.priceCache[pos.Symbol]
		if price > 0 {
			totalEquity += price * float64(pos.Quantity)
		}
	}
	
	return &models.Balance{
		AvailableCash:   p.balance.AvailableCash,
		UsedMargin:      p.balance.TotalEquity - p.balance.AvailableCash,
		TotalEquity:     totalEquity,
		CollateralValue: 0,
	}, nil
}

// GetMargins returns simulated margins.
func (p *PaperBroker) GetMargins(ctx context.Context) (*models.Margins, error) {
	balance, _ := p.GetBalance(ctx)
	
	return &models.Margins{
		Equity: models.SegmentMargin{
			Available: balance.AvailableCash,
			Used:      balance.UsedMargin,
			Total:     balance.TotalEquity,
		},
		Commodity: models.SegmentMargin{},
	}, nil
}

// GetOptionChain fetches option chain from data broker.
func (p *PaperBroker) GetOptionChain(ctx context.Context, symbol string, expiry time.Time) (*models.OptionChain, error) {
	if p.dataBroker != nil {
		return p.dataBroker.GetOptionChain(ctx, symbol, expiry)
	}
	return nil, fmt.Errorf("no data broker configured")
}

// GetFuturesChain fetches futures chain from data broker.
func (p *PaperBroker) GetFuturesChain(ctx context.Context, symbol string) (*models.FuturesChain, error) {
	if p.dataBroker != nil {
		return p.dataBroker.GetFuturesChain(ctx, symbol)
	}
	return nil, fmt.Errorf("no data broker configured")
}


// updatePosition updates or creates a position based on trade.
func (p *PaperBroker) updatePosition(symbol string, exchange models.Exchange, product models.ProductType, side models.OrderSide, qty int, price float64) {
	key := fmt.Sprintf("%s:%s:%s", exchange, symbol, product)
	
	pos, exists := p.positions[key]
	if !exists {
		pos = &models.Position{
			Symbol:   symbol,
			Exchange: exchange,
			Product:  product,
		}
		p.positions[key] = pos
	}
	
	if side == models.OrderSideBuy {
		// Calculate new average price
		totalValue := pos.AveragePrice*float64(pos.Quantity) + price*float64(qty)
		pos.Quantity += qty
		if pos.Quantity > 0 {
			pos.AveragePrice = totalValue / float64(pos.Quantity)
		}
	} else {
		pos.Quantity -= qty
		// If position is closed, remove it
		if pos.Quantity == 0 {
			delete(p.positions, key)
			return
		}
		// If position flipped to short
		if pos.Quantity < 0 {
			pos.AveragePrice = price
		}
	}
	
	pos.LTP = price
	pos.Value = price * float64(pos.Quantity)
	pos.PnL = (price - pos.AveragePrice) * float64(pos.Quantity)
	if pos.AveragePrice > 0 {
		pos.PnLPercent = ((price - pos.AveragePrice) / pos.AveragePrice) * 100
	}
}

// getPrice returns cached price for a symbol.
func (p *PaperBroker) getPrice(symbol string) float64 {
	return p.priceCache[symbol]
}

// UpdatePrice updates the cached price for a symbol.
func (p *PaperBroker) UpdatePrice(symbol string, price float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.priceCache[symbol] = price
}

// ProcessTick processes a tick and updates prices.
func (p *PaperBroker) ProcessTick(tick models.Tick) {
	p.mu.Lock()
	p.priceCache[tick.Symbol] = tick.LTP
	p.mu.Unlock()
	
	// Check GTT triggers
	p.checkGTTTriggers(tick)
}

// checkGTTTriggers checks if any GTT orders should be triggered.
func (p *PaperBroker) checkGTTTriggers(tick models.Tick) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	for _, gtt := range p.gttOrders {
		if gtt.Status != "ACTIVE" || gtt.Symbol != tick.Symbol {
			continue
		}
		
		triggered := false
		if gtt.TriggerType == "single" {
			// Single trigger - check if price crossed trigger
			if len(gtt.Orders) > 0 {
				if gtt.Orders[0].Side == models.OrderSideBuy && tick.LTP >= gtt.TriggerPrice {
					triggered = true
				}
				if gtt.Orders[0].Side == models.OrderSideSell && tick.LTP <= gtt.TriggerPrice {
					triggered = true
				}
			}
		}
		
		if triggered {
			gtt.Status = "TRIGGERED"
			gtt.UpdatedAt = time.Now()
			// In a real implementation, we would place the order here
		}
	}
}

// Reset resets the paper broker to initial state.
func (p *PaperBroker) Reset(initialBalance float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.positions = make(map[string]*models.Position)
	p.holdings = make(map[string]*models.Holding)
	p.orders = make(map[string]*models.Order)
	p.gttOrders = make(map[string]*models.GTTOrder)
	p.balance = &models.Balance{
		AvailableCash: initialBalance,
		TotalEquity:   initialBalance,
	}
	p.orderCounter = 0
	p.gttCounter = 0
}

// GetTrades returns all completed trades.
func (p *PaperBroker) GetTrades() []models.Order {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	trades := make([]models.Order, 0)
	for _, o := range p.orders {
		if o.Status == "COMPLETE" {
			trades = append(trades, *o)
		}
	}
	return trades
}

// IsPaperTrading returns true to indicate this is a paper broker.
func (p *PaperBroker) IsPaperTrading() bool {
	return true
}

// Ensure PaperBroker implements Broker interface
var _ Broker = (*PaperBroker)(nil)
