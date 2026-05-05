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
	fillModel  PaperFillModel
	ledger     PaperLedger

	// Simulated state
	positions map[string]*models.Position
	holdings  map[string]*models.Holding
	orders    map[string]*models.Order
	gttOrders map[string]*models.GTTOrder
	balance   *models.Balance

	// Order tracking
	orderCounter int
	gttCounter   int

	// Price cache for simulation
	priceCache map[string]float64
	tickCache  map[string]models.Tick

	mu sync.RWMutex
}

// PaperBrokerConfig holds configuration for paper broker.
type PaperBrokerConfig struct {
	DataBroker     Broker
	Ticker         Ticker
	InitialBalance float64
	FillModel      PaperFillModel
	Ledger         PaperLedger
}

// PaperFillModel controls paper execution assumptions.
type PaperFillModel struct {
	SlippageRate          float64 // fraction, e.g. 0.001 = 0.1%
	SpreadRate            float64 // fraction around LTP when bid/ask is unavailable
	CommissionRate        float64 // fraction of turnover
	FlatFee               float64 // flat fee per filled order
	AllowPartialFills     bool
	MaxFillDepthPercent   float64 // max fill as % of opposite-side depth when tick depth exists
	RejectMarketIfNoPrice bool
}

type paperFill struct {
	CanFill      bool
	Price        float64
	Quantity     int
	OrderValue   float64
	Costs        float64
	Partial      bool
	RejectReason string
}

// NewPaperBroker creates a new paper trading broker.
func NewPaperBroker(cfg PaperBrokerConfig) *PaperBroker {
	initialBalance := cfg.InitialBalance
	if initialBalance == 0 {
		initialBalance = 1000000 // 10 lakhs default
	}

	fillModel := cfg.FillModel
	if fillModel.SlippageRate == 0 {
		fillModel.SlippageRate = 0.0005
	}
	if fillModel.SpreadRate == 0 {
		fillModel.SpreadRate = 0.0005
	}
	if fillModel.CommissionRate == 0 {
		fillModel.CommissionRate = 0.0003
	}
	if fillModel.MaxFillDepthPercent == 0 {
		fillModel.MaxFillDepthPercent = 100
	}

	p := &PaperBroker{
		dataBroker: cfg.DataBroker,
		ticker:     cfg.Ticker,
		fillModel:  fillModel,
		ledger:     cfg.Ledger,
		positions:  make(map[string]*models.Position),
		holdings:   make(map[string]*models.Holding),
		orders:     make(map[string]*models.Order),
		gttOrders:  make(map[string]*models.GTTOrder),
		balance: &models.Balance{
			AvailableCash: initialBalance,
			TotalEquity:   initialBalance,
		},
		priceCache: make(map[string]float64),
		tickCache:  make(map[string]models.Tick),
	}

	if cfg.Ledger != nil {
		if state, err := cfg.Ledger.LoadPaperState(context.Background()); err == nil && state != nil {
			p.loadStateLocked(state)
		}
	}

	return p
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

// GetInstrumentToken returns the instrument token for a symbol.
func (p *PaperBroker) GetInstrumentToken(ctx context.Context, symbol string, exchange models.Exchange) (uint32, error) {
	if p.dataBroker != nil {
		return p.dataBroker.GetInstrumentToken(ctx, symbol, exchange)
	}
	return 0, fmt.Errorf("no data broker configured")
}

// PlaceOrder simulates order placement.
func (p *PaperBroker) PlaceOrder(ctx context.Context, order *models.Order) (*OrderResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Generate order ID
	p.orderCounter++
	orderID := fmt.Sprintf("PAPER_%d_%d", time.Now().Unix(), p.orderCounter)

	fill := p.simulateFillLocked(ctx, order)
	if fill.RejectReason != "" {
		return nil, fmt.Errorf("%s", fill.RejectReason)
	}

	if order.Side == models.OrderSideBuy && fill.CanFill {
		required := fill.OrderValue + fill.Costs
		if p.balance.AvailableCash < required {
			return nil, fmt.Errorf("insufficient funds: need %.2f, have %.2f", required, p.balance.AvailableCash)
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

	if fill.CanFill {
		newOrder.Status = "COMPLETE"
		if fill.Partial {
			newOrder.Status = "PARTIAL"
		}
		newOrder.FilledQty = fill.Quantity
		newOrder.AveragePrice = fill.Price

		// Update position
		p.updatePosition(order.Symbol, order.Exchange, order.Product, order.Side, fill.Quantity, fill.Price)

		// Update balance
		if order.Side == models.OrderSideBuy {
			p.balance.AvailableCash -= fill.OrderValue + fill.Costs
		} else {
			p.balance.AvailableCash += fill.OrderValue - fill.Costs
		}
	} else {
		newOrder.Status = "OPEN"
	}

	p.orders[orderID] = newOrder
	if err := p.persistLocked(ctx, &PaperLedgerEvent{
		Type:   "ORDER_PLACED",
		RefID:  orderID,
		Symbol: newOrder.Symbol,
		Payload: map[string]interface{}{
			"status":        newOrder.Status,
			"side":          newOrder.Side,
			"quantity":      newOrder.Quantity,
			"filled_qty":    newOrder.FilledQty,
			"average_price": newOrder.AveragePrice,
			"tag":           newOrder.Tag,
		},
	}); err != nil {
		return nil, err
	}

	return &OrderResult{
		OrderID:      orderID,
		Status:       newOrder.Status,
		Message:      "Paper order placed",
		Tag:          newOrder.Tag,
		FilledQty:    newOrder.FilledQty,
		AveragePrice: newOrder.AveragePrice,
	}, nil
}

func (p *PaperBroker) simulateFillLocked(ctx context.Context, order *models.Order) paperFill {
	if order == nil {
		return paperFill{RejectReason: "order is required"}
	}
	if order.Quantity <= 0 {
		return paperFill{RejectReason: "quantity must be positive"}
	}

	ltp := p.getPrice(order.Symbol)
	if ltp == 0 && p.dataBroker != nil {
		quote, err := p.dataBroker.GetQuote(ctx, order.Symbol)
		if err == nil && quote != nil {
			ltp = quote.LTP
			p.priceCache[order.Symbol] = ltp
		}
	}
	if ltp <= 0 {
		if p.fillModel.RejectMarketIfNoPrice || order.Type == models.OrderTypeMarket {
			return paperFill{RejectReason: "market price unavailable for paper fill"}
		}
		return paperFill{CanFill: false}
	}

	price := p.paperExecutablePrice(order, ltp)
	if price <= 0 {
		return paperFill{CanFill: false}
	}

	if order.Type == models.OrderTypeLimit {
		if order.Side == models.OrderSideBuy && price > order.Price {
			return paperFill{CanFill: false}
		}
		if order.Side == models.OrderSideSell && price < order.Price {
			return paperFill{CanFill: false}
		}
	}

	qty := order.Quantity
	partial := false
	if p.fillModel.AllowPartialFills {
		if maxQty := p.maxPaperFillQuantity(order); maxQty > 0 && maxQty < qty {
			qty = maxQty
			partial = true
		}
	}
	if qty <= 0 {
		return paperFill{CanFill: false}
	}

	orderValue := price * float64(qty)
	costs := orderValue*p.fillModel.CommissionRate + p.fillModel.FlatFee
	return paperFill{
		CanFill:    true,
		Price:      price,
		Quantity:   qty,
		OrderValue: orderValue,
		Costs:      costs,
		Partial:    partial,
	}
}

func (p *PaperBroker) paperExecutablePrice(order *models.Order, ltp float64) float64 {
	tick := p.tickCache[order.Symbol]
	price := ltp
	if order.Side == models.OrderSideBuy {
		if tick.AskPrice > 0 {
			price = tick.AskPrice
		} else {
			price = ltp * (1 + p.fillModel.SpreadRate/2)
		}
		price *= 1 + p.fillModel.SlippageRate
	} else {
		if tick.BidPrice > 0 {
			price = tick.BidPrice
		} else {
			price = ltp * (1 - p.fillModel.SpreadRate/2)
		}
		price *= 1 - p.fillModel.SlippageRate
	}
	return price
}

func (p *PaperBroker) maxPaperFillQuantity(order *models.Order) int {
	tick := p.tickCache[order.Symbol]
	depthQty := int64(0)
	if order.Side == models.OrderSideBuy {
		depthQty = tick.SellQuantity
	} else {
		depthQty = tick.BuyQuantity
	}
	if depthQty <= 0 || p.fillModel.MaxFillDepthPercent <= 0 {
		return 0
	}
	return int(float64(depthQty) * (p.fillModel.MaxFillDepthPercent / 100))
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

	return p.persistLocked(ctx, &PaperLedgerEvent{
		Type:   "ORDER_MODIFIED",
		RefID:  orderID,
		Symbol: existing.Symbol,
		Payload: map[string]interface{}{
			"price":         existing.Price,
			"trigger_price": existing.TriggerPrice,
			"quantity":      existing.Quantity,
		},
	})
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
	return p.persistLocked(ctx, &PaperLedgerEvent{
		Type:   "ORDER_CANCELLED",
		RefID:  orderID,
		Symbol: order.Symbol,
		Payload: map[string]interface{}{
			"status": order.Status,
		},
	})
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
	if err := p.persistLocked(ctx, &PaperLedgerEvent{
		Type:   "GTT_PLACED",
		RefID:  gttID,
		Symbol: newGTT.Symbol,
		Payload: map[string]interface{}{
			"trigger_price": newGTT.TriggerPrice,
			"status":        newGTT.Status,
			"trigger_type":  newGTT.TriggerType,
		},
	}); err != nil {
		return nil, err
	}

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

	return p.persistLocked(ctx, &PaperLedgerEvent{
		Type:   "GTT_MODIFIED",
		RefID:  gttID,
		Symbol: existing.Symbol,
		Payload: map[string]interface{}{
			"trigger_price": existing.TriggerPrice,
			"status":        existing.Status,
		},
	})
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
	return p.persistLocked(ctx, &PaperLedgerEvent{
		Type:   "GTT_CANCELLED",
		RefID:  gttID,
		Symbol: gtt.Symbol,
		Payload: map[string]interface{}{
			"status": gtt.Status,
		},
	})
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
	key := paperPositionKey(exchange, symbol, product)

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

func paperPositionKey(exchange models.Exchange, symbol string, product models.ProductType) string {
	return fmt.Sprintf("%s:%s:%s", exchange, symbol, product)
}

func (p *PaperBroker) loadStateLocked(state *PaperState) {
	p.positions = make(map[string]*models.Position, len(state.Positions))
	for _, pos := range state.Positions {
		posCopy := pos
		p.positions[paperPositionKey(pos.Exchange, pos.Symbol, pos.Product)] = &posCopy
	}

	p.holdings = make(map[string]*models.Holding, len(state.Holdings))
	for _, holding := range state.Holdings {
		holdingCopy := holding
		p.holdings[holding.Symbol] = &holdingCopy
	}

	p.orders = make(map[string]*models.Order, len(state.Orders))
	for _, order := range state.Orders {
		orderCopy := order
		p.orders[order.ID] = &orderCopy
	}

	p.gttOrders = make(map[string]*models.GTTOrder, len(state.GTTOrders))
	for _, gtt := range state.GTTOrders {
		gttCopy := gtt
		p.gttOrders[gtt.ID] = &gttCopy
	}

	p.balance = &models.Balance{
		AvailableCash:   state.Balance.AvailableCash,
		UsedMargin:      state.Balance.UsedMargin,
		TotalEquity:     state.Balance.TotalEquity,
		CollateralValue: state.Balance.CollateralValue,
	}
	p.orderCounter = state.OrderCounter
	p.gttCounter = state.GTTCounter
}

func (p *PaperBroker) snapshotLocked() *PaperState {
	state := &PaperState{
		Balance: models.Balance{
			AvailableCash:   p.balance.AvailableCash,
			UsedMargin:      p.balance.UsedMargin,
			TotalEquity:     p.balance.TotalEquity,
			CollateralValue: p.balance.CollateralValue,
		},
		OrderCounter: p.orderCounter,
		GTTCounter:   p.gttCounter,
		UpdatedAt:    time.Now(),
	}

	state.Positions = make([]models.Position, 0, len(p.positions))
	for _, pos := range p.positions {
		state.Positions = append(state.Positions, *pos)
	}
	state.Holdings = make([]models.Holding, 0, len(p.holdings))
	for _, holding := range p.holdings {
		state.Holdings = append(state.Holdings, *holding)
	}
	state.Orders = make([]models.Order, 0, len(p.orders))
	for _, order := range p.orders {
		state.Orders = append(state.Orders, *order)
	}
	state.GTTOrders = make([]models.GTTOrder, 0, len(p.gttOrders))
	for _, gtt := range p.gttOrders {
		state.GTTOrders = append(state.GTTOrders, *gtt)
	}

	return state
}

func (p *PaperBroker) persistLocked(ctx context.Context, event *PaperLedgerEvent) error {
	if p.ledger == nil {
		return nil
	}
	if err := p.ledger.SavePaperState(ctx, p.snapshotLocked()); err != nil {
		return fmt.Errorf("saving paper state: %w", err)
	}
	if event != nil {
		if err := p.ledger.AppendPaperLedger(ctx, event); err != nil {
			return fmt.Errorf("appending paper ledger: %w", err)
		}
	}
	return nil
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
	p.tickCache[tick.Symbol] = tick
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
			_ = p.persistLocked(context.Background(), &PaperLedgerEvent{
				Type:   "GTT_TRIGGERED",
				RefID:  gtt.ID,
				Symbol: gtt.Symbol,
				Payload: map[string]interface{}{
					"trigger_price": gtt.TriggerPrice,
					"ltp":           tick.LTP,
				},
			})
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
	p.priceCache = make(map[string]float64)
	p.tickCache = make(map[string]models.Tick)
	p.balance = &models.Balance{
		AvailableCash: initialBalance,
		TotalEquity:   initialBalance,
	}
	p.orderCounter = 0
	p.gttCounter = 0
	_ = p.persistLocked(context.Background(), &PaperLedgerEvent{
		Type: "RESET",
		Payload: map[string]interface{}{
			"initial_balance": initialBalance,
		},
	})
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
