// Package broker provides broker integration implementations.
package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	kiteconnect "github.com/zerodha/gokiteconnect/v4"

	"zerodha-trader/internal/models"
)

// ZerodhaBroker implements the Broker interface for Zerodha Kite Connect.
type ZerodhaBroker struct {
	client        *kiteconnect.Client
	apiKey        string
	apiSecret     string
	userID        string
	accessToken   string
	tokenPath     string
	authenticated bool
	instruments   map[string]models.Instrument
	mu            sync.RWMutex
}

// ZerodhaConfig holds configuration for Zerodha broker.
type ZerodhaConfig struct {
	APIKey    string
	APISecret string
	UserID    string
	TokenPath string
}

// NewZerodhaBroker creates a new Zerodha broker instance.
// It automatically loads any saved session from disk.
func NewZerodhaBroker(cfg ZerodhaConfig) *ZerodhaBroker {
	client := kiteconnect.New(cfg.APIKey)
	
	tokenPath := cfg.TokenPath
	if tokenPath == "" {
		homeDir, _ := os.UserHomeDir()
		tokenPath = filepath.Join(homeDir, ".config", "zerodha-trader", "session.json")
	}
	
	zb := &ZerodhaBroker{
		client:      client,
		apiKey:      cfg.APIKey,
		apiSecret:   cfg.APISecret,
		userID:      cfg.UserID,
		tokenPath:   tokenPath,
		instruments: make(map[string]models.Instrument),
	}
	
	// Automatically load saved session if available
	_ = zb.loadSession()
	
	return zb
}

// sessionData represents persisted session data.
type sessionData struct {
	AccessToken string    `json:"access_token"`
	UserID      string    `json:"user_id"`
	ExpiresAt   time.Time `json:"expires_at"`
}


// Login authenticates with Zerodha using OAuth flow.
// It first tries to load a persisted session, then falls back to OAuth.
func (z *ZerodhaBroker) Login(ctx context.Context) error {
	// Try to load existing session
	if err := z.loadSession(); err == nil && z.authenticated {
		// Verify session is still valid
		if _, err := z.client.GetUserProfile(); err == nil {
			return nil
		}
	}
	
	// Need fresh authentication - return login URL for user
	loginURL := z.client.GetLoginURL()
	return fmt.Errorf("authentication required: please visit %s and complete login, then call CompleteLogin with the request token", loginURL)
}

// CompleteLogin completes the OAuth flow with the request token.
func (z *ZerodhaBroker) CompleteLogin(ctx context.Context, requestToken string) error {
	session, err := z.client.GenerateSession(requestToken, z.apiSecret)
	if err != nil {
		return fmt.Errorf("failed to generate session: %w", err)
	}
	
	z.mu.Lock()
	z.accessToken = session.AccessToken
	z.authenticated = true
	z.client.SetAccessToken(session.AccessToken)
	z.mu.Unlock()
	
	// Persist session
	if err := z.saveSession(session.AccessToken); err != nil {
		// Log but don't fail - session is valid
		fmt.Printf("warning: failed to persist session: %v\n", err)
	}
	
	return nil
}

// Logout invalidates the session and clears stored credentials.
func (z *ZerodhaBroker) Logout(ctx context.Context) error {
	z.mu.Lock()
	defer z.mu.Unlock()
	
	if z.authenticated {
		if _, err := z.client.InvalidateAccessToken(); err != nil {
			// Log but continue with local cleanup
			fmt.Printf("warning: failed to invalidate token: %v\n", err)
		}
	}
	
	z.accessToken = ""
	z.authenticated = false
	
	// Remove persisted session
	if err := os.Remove(z.tokenPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove session file: %w", err)
	}
	
	return nil
}

// IsAuthenticated returns whether the broker is authenticated.
func (z *ZerodhaBroker) IsAuthenticated() bool {
	z.mu.RLock()
	defer z.mu.RUnlock()
	return z.authenticated
}

// RefreshSession refreshes the access token.
func (z *ZerodhaBroker) RefreshSession(ctx context.Context) error {
	z.mu.Lock()
	defer z.mu.Unlock()
	
	session, err := z.client.RenewAccessToken(z.accessToken, z.apiSecret)
	if err != nil {
		z.authenticated = false
		return fmt.Errorf("failed to refresh session: %w", err)
	}
	
	z.accessToken = session.AccessToken
	z.client.SetAccessToken(session.AccessToken)
	
	if err := z.saveSession(session.AccessToken); err != nil {
		fmt.Printf("warning: failed to persist refreshed session: %v\n", err)
	}
	
	return nil
}

func (z *ZerodhaBroker) loadSession() error {
	data, err := os.ReadFile(z.tokenPath)
	if err != nil {
		return err
	}
	
	var session sessionData
	if err := json.Unmarshal(data, &session); err != nil {
		return err
	}
	
	// Check if session is expired (Zerodha tokens expire at 6 AM next day)
	if time.Now().After(session.ExpiresAt) {
		return fmt.Errorf("session expired")
	}
	
	z.mu.Lock()
	z.accessToken = session.AccessToken
	z.authenticated = true
	z.client.SetAccessToken(session.AccessToken)
	z.mu.Unlock()
	
	return nil
}

func (z *ZerodhaBroker) saveSession(accessToken string) error {
	// Ensure directory exists
	dir := filepath.Dir(z.tokenPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	
	// Zerodha tokens expire at 6 AM IST next day
	loc, _ := time.LoadLocation("Asia/Kolkata")
	now := time.Now().In(loc)
	expiresAt := time.Date(now.Year(), now.Month(), now.Day()+1, 6, 0, 0, 0, loc)
	
	session := sessionData{
		AccessToken: accessToken,
		UserID:      z.userID,
		ExpiresAt:   expiresAt,
	}
	
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	
	// Write with restricted permissions
	return os.WriteFile(z.tokenPath, data, 0600)
}


// GetQuote fetches real-time quote for a symbol.
func (z *ZerodhaBroker) GetQuote(ctx context.Context, symbol string) (*models.Quote, error) {
	if !z.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}
	
	quotes, err := z.client.GetQuote(symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to get quote: %w", err)
	}
	
	q, ok := quotes[symbol]
	if !ok {
		return nil, fmt.Errorf("quote not found for symbol: %s", symbol)
	}
	
	return &models.Quote{
		Symbol:        symbol,
		LTP:           q.LastPrice,
		Open:          q.OHLC.Open,
		High:          q.OHLC.High,
		Low:           q.OHLC.Low,
		Close:         q.OHLC.Close,
		Volume:        int64(q.Volume),
		Change:        q.NetChange,
		ChangePercent: (q.NetChange / q.OHLC.Close) * 100,
		Timestamp:     q.LastTradeTime.Time,
	}, nil
}

// GetHistorical fetches historical OHLCV data.
func (z *ZerodhaBroker) GetHistorical(ctx context.Context, req HistoricalRequest) ([]models.Candle, error) {
	if !z.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}
	
	// Get instrument token
	token, err := z.getInstrumentToken(req.Symbol, req.Exchange)
	if err != nil {
		return nil, err
	}
	
	// Map timeframe to Kite interval
	interval := mapTimeframeToInterval(req.Timeframe)
	
	data, err := z.client.GetHistoricalData(int(token), interval, req.From, req.To, false, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get historical data: %w", err)
	}
	
	candles := make([]models.Candle, len(data))
	for i, d := range data {
		candles[i] = models.Candle{
			Timestamp: d.Date.Time,
			Open:      d.Open,
			High:      d.High,
			Low:       d.Low,
			Close:     d.Close,
			Volume:    int64(d.Volume),
		}
	}
	
	return candles, nil
}

// GetInstruments fetches all instruments for an exchange.
func (z *ZerodhaBroker) GetInstruments(ctx context.Context, exchange models.Exchange) ([]models.Instrument, error) {
	if !z.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}
	
	instruments, err := z.client.GetInstruments()
	if err != nil {
		return nil, fmt.Errorf("failed to get instruments: %w", err)
	}
	
	// Filter by exchange
	var filtered []kiteconnect.Instrument
	for _, inst := range instruments {
		if inst.Exchange == string(exchange) {
			filtered = append(filtered, inst)
		}
	}
	
	result := make([]models.Instrument, len(filtered))
	for i, inst := range filtered {
		result[i] = models.Instrument{
			Token:     uint32(inst.InstrumentToken),
			Symbol:    inst.Tradingsymbol,
			Name:      inst.Name,
			Exchange:  models.Exchange(inst.Exchange),
			Segment:   inst.Segment,
			LotSize:   int(inst.LotSize),
			TickSize:  inst.TickSize,
			Expiry:    inst.Expiry.Time,
			Strike:    inst.StrikePrice,
			InstrType: inst.InstrumentType,
		}
		
		// Cache instrument
		key := fmt.Sprintf("%s:%s", inst.Exchange, inst.Tradingsymbol)
		z.mu.Lock()
		z.instruments[key] = result[i]
		z.mu.Unlock()
	}
	
	return result, nil
}

func (z *ZerodhaBroker) getInstrumentToken(symbol string, exchange models.Exchange) (uint32, error) {
	key := fmt.Sprintf("%s:%s", exchange, symbol)
	
	z.mu.RLock()
	inst, ok := z.instruments[key]
	z.mu.RUnlock()
	
	if ok {
		return inst.Token, nil
	}
	
	// Fetch instruments if not cached
	if _, err := z.GetInstruments(context.Background(), exchange); err != nil {
		return 0, err
	}
	
	z.mu.RLock()
	inst, ok = z.instruments[key]
	z.mu.RUnlock()
	
	if !ok {
		return 0, fmt.Errorf("instrument not found: %s", symbol)
	}
	
	return inst.Token, nil
}

func mapTimeframeToInterval(tf string) string {
	switch tf {
	case "1min":
		return "minute"
	case "5min":
		return "5minute"
	case "15min":
		return "15minute"
	case "30min":
		return "30minute"
	case "1hour":
		return "60minute"
	case "1day":
		return "day"
	default:
		return "day"
	}
}


// PlaceOrder places a new order.
func (z *ZerodhaBroker) PlaceOrder(ctx context.Context, order *models.Order) (*OrderResult, error) {
	if !z.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}
	
	params := kiteconnect.OrderParams{
		Exchange:        string(order.Exchange),
		Tradingsymbol:   order.Symbol,
		TransactionType: string(order.Side),
		OrderType:       string(order.Type),
		Product:         string(order.Product),
		Quantity:        order.Quantity,
		Price:           order.Price,
		TriggerPrice:    order.TriggerPrice,
		Validity:        order.Validity,
		Tag:             order.Tag,
	}
	
	if params.Validity == "" {
		params.Validity = "DAY"
	}
	
	resp, err := z.client.PlaceOrder(kiteconnect.VarietyRegular, params)
	if err != nil {
		return nil, fmt.Errorf("failed to place order: %w", err)
	}
	
	return &OrderResult{
		OrderID: resp.OrderID,
		Status:  "PLACED",
		Message: "Order placed successfully",
	}, nil
}

// ModifyOrder modifies an existing order.
func (z *ZerodhaBroker) ModifyOrder(ctx context.Context, orderID string, order *models.Order) error {
	if !z.IsAuthenticated() {
		return fmt.Errorf("not authenticated")
	}
	
	params := kiteconnect.OrderParams{
		Exchange:        string(order.Exchange),
		Tradingsymbol:   order.Symbol,
		TransactionType: string(order.Side),
		OrderType:       string(order.Type),
		Product:         string(order.Product),
		Quantity:        order.Quantity,
		Price:           order.Price,
		TriggerPrice:    order.TriggerPrice,
		Validity:        order.Validity,
	}
	
	_, err := z.client.ModifyOrder(kiteconnect.VarietyRegular, orderID, params)
	if err != nil {
		return fmt.Errorf("failed to modify order: %w", err)
	}
	
	return nil
}

// CancelOrder cancels an existing order.
func (z *ZerodhaBroker) CancelOrder(ctx context.Context, orderID string) error {
	if !z.IsAuthenticated() {
		return fmt.Errorf("not authenticated")
	}
	
	_, err := z.client.CancelOrder(kiteconnect.VarietyRegular, orderID, nil)
	if err != nil {
		return fmt.Errorf("failed to cancel order: %w", err)
	}
	
	return nil
}

// GetOrders fetches all orders for the day.
func (z *ZerodhaBroker) GetOrders(ctx context.Context) ([]models.Order, error) {
	if !z.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}
	
	orders, err := z.client.GetOrders()
	if err != nil {
		return nil, fmt.Errorf("failed to get orders: %w", err)
	}
	
	result := make([]models.Order, len(orders))
	for i, o := range orders {
		result[i] = models.Order{
			ID:           o.OrderID,
			Symbol:       o.TradingSymbol,
			Exchange:     models.Exchange(o.Exchange),
			Side:         models.OrderSide(o.TransactionType),
			Type:         models.OrderType(o.OrderType),
			Product:      models.ProductType(o.Product),
			Quantity:     int(o.Quantity),
			Price:        o.Price,
			TriggerPrice: o.TriggerPrice,
			Validity:     o.Validity,
			Tag:          o.Tag,
			Status:       o.Status,
			FilledQty:    int(o.FilledQuantity),
			AveragePrice: o.AveragePrice,
			PlacedAt:     o.OrderTimestamp.Time,
		}
	}
	
	return result, nil
}

// GetOrderHistory fetches order history for a date range.
// Note: Zerodha Kite Connect API only provides orders for the current day.
// For historical orders, this method returns orders from the current day
// that fall within the specified date range.
func (z *ZerodhaBroker) GetOrderHistory(ctx context.Context, from, to time.Time) ([]models.Order, error) {
	if !z.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}
	
	// Zerodha API only provides current day orders
	// For historical data, users need to use the Kite Console or export trades
	orders, err := z.client.GetOrders()
	if err != nil {
		return nil, fmt.Errorf("failed to get orders: %w", err)
	}
	
	// Filter orders by date range
	var result []models.Order
	for _, o := range orders {
		orderTime := o.OrderTimestamp.Time
		if (orderTime.Equal(from) || orderTime.After(from)) && (orderTime.Equal(to) || orderTime.Before(to)) {
			result = append(result, models.Order{
				ID:           o.OrderID,
				Symbol:       o.TradingSymbol,
				Exchange:     models.Exchange(o.Exchange),
				Side:         models.OrderSide(o.TransactionType),
				Type:         models.OrderType(o.OrderType),
				Product:      models.ProductType(o.Product),
				Quantity:     int(o.Quantity),
				Price:        o.Price,
				TriggerPrice: o.TriggerPrice,
				Validity:     o.Validity,
				Tag:          o.Tag,
				Status:       o.Status,
				FilledQty:    int(o.FilledQuantity),
				AveragePrice: o.AveragePrice,
				PlacedAt:     orderTime,
			})
		}
	}
	
	return result, nil
}


// PlaceGTT places a Good Till Triggered order.
func (z *ZerodhaBroker) PlaceGTT(ctx context.Context, gtt *models.GTTOrder) (*GTTResult, error) {
	if !z.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}
	
	// Build trigger based on type
	var trigger kiteconnect.Trigger
	
	if gtt.TriggerType == "two-leg" && len(gtt.Orders) >= 2 {
		// OCO trigger with upper and lower legs
		trigger = &kiteconnect.GTTOneCancelsOtherTrigger{
			Upper: kiteconnect.TriggerParams{
				TriggerValue: gtt.Orders[0].Price * 1.01, // Upper trigger
				LimitPrice:   gtt.Orders[0].Price,
				Quantity:     float64(gtt.Orders[0].Quantity),
			},
			Lower: kiteconnect.TriggerParams{
				TriggerValue: gtt.Orders[1].Price * 0.99, // Lower trigger
				LimitPrice:   gtt.Orders[1].Price,
				Quantity:     float64(gtt.Orders[1].Quantity),
			},
		}
	} else if len(gtt.Orders) > 0 {
		// Single leg trigger
		trigger = &kiteconnect.GTTSingleLegTrigger{
			TriggerParams: kiteconnect.TriggerParams{
				TriggerValue: gtt.TriggerPrice,
				LimitPrice:   gtt.Orders[0].Price,
				Quantity:     float64(gtt.Orders[0].Quantity),
			},
		}
	} else {
		return nil, fmt.Errorf("GTT order must have at least one leg")
	}
	
	transactionType := "BUY"
	product := "CNC"
	if len(gtt.Orders) > 0 {
		transactionType = string(gtt.Orders[0].Side)
		product = string(gtt.Orders[0].Product)
	}
	
	params := kiteconnect.GTTParams{
		Tradingsymbol:   gtt.Symbol,
		Exchange:        string(gtt.Exchange),
		LastPrice:       gtt.LastPrice,
		TransactionType: transactionType,
		Product:         product,
		Trigger:         trigger,
	}
	
	resp, err := z.client.PlaceGTT(params)
	if err != nil {
		return nil, fmt.Errorf("failed to place GTT: %w", err)
	}
	
	return &GTTResult{
		TriggerID: fmt.Sprintf("%d", resp.TriggerID),
		Status:    "ACTIVE",
		Message:   "GTT placed successfully",
	}, nil
}

// ModifyGTT modifies an existing GTT order.
func (z *ZerodhaBroker) ModifyGTT(ctx context.Context, gttID string, gtt *models.GTTOrder) error {
	if !z.IsAuthenticated() {
		return fmt.Errorf("not authenticated")
	}
	
	var triggerID int
	fmt.Sscanf(gttID, "%d", &triggerID)
	
	// Build trigger based on type
	var trigger kiteconnect.Trigger
	
	if gtt.TriggerType == "two-leg" && len(gtt.Orders) >= 2 {
		trigger = &kiteconnect.GTTOneCancelsOtherTrigger{
			Upper: kiteconnect.TriggerParams{
				TriggerValue: gtt.Orders[0].Price * 1.01,
				LimitPrice:   gtt.Orders[0].Price,
				Quantity:     float64(gtt.Orders[0].Quantity),
			},
			Lower: kiteconnect.TriggerParams{
				TriggerValue: gtt.Orders[1].Price * 0.99,
				LimitPrice:   gtt.Orders[1].Price,
				Quantity:     float64(gtt.Orders[1].Quantity),
			},
		}
	} else if len(gtt.Orders) > 0 {
		trigger = &kiteconnect.GTTSingleLegTrigger{
			TriggerParams: kiteconnect.TriggerParams{
				TriggerValue: gtt.TriggerPrice,
				LimitPrice:   gtt.Orders[0].Price,
				Quantity:     float64(gtt.Orders[0].Quantity),
			},
		}
	} else {
		return fmt.Errorf("GTT order must have at least one leg")
	}
	
	transactionType := "BUY"
	product := "CNC"
	if len(gtt.Orders) > 0 {
		transactionType = string(gtt.Orders[0].Side)
		product = string(gtt.Orders[0].Product)
	}
	
	params := kiteconnect.GTTParams{
		Tradingsymbol:   gtt.Symbol,
		Exchange:        string(gtt.Exchange),
		LastPrice:       gtt.LastPrice,
		TransactionType: transactionType,
		Product:         product,
		Trigger:         trigger,
	}
	
	_, err := z.client.ModifyGTT(triggerID, params)
	if err != nil {
		return fmt.Errorf("failed to modify GTT: %w", err)
	}
	
	return nil
}

// CancelGTT cancels an existing GTT order.
func (z *ZerodhaBroker) CancelGTT(ctx context.Context, gttID string) error {
	if !z.IsAuthenticated() {
		return fmt.Errorf("not authenticated")
	}
	
	var triggerID int
	fmt.Sscanf(gttID, "%d", &triggerID)
	
	_, err := z.client.DeleteGTT(triggerID)
	if err != nil {
		return fmt.Errorf("failed to cancel GTT: %w", err)
	}
	
	return nil
}

// GetGTTs fetches all GTT orders.
func (z *ZerodhaBroker) GetGTTs(ctx context.Context) ([]models.GTTOrder, error) {
	if !z.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}
	
	gtts, err := z.client.GetGTTs()
	if err != nil {
		return nil, fmt.Errorf("failed to get GTTs: %w", err)
	}
	
	result := make([]models.GTTOrder, len(gtts))
	for i, g := range gtts {
		orders := make([]models.GTTOrderLeg, len(g.Orders))
		for j, o := range g.Orders {
			orders[j] = models.GTTOrderLeg{
				Side:     models.OrderSide(o.TransactionType),
				Type:     models.OrderType(o.OrderType),
				Product:  models.ProductType(o.Product),
				Quantity: int(o.Quantity),
				Price:    o.Price,
			}
		}
		
		triggerType := "single"
		if g.Type == kiteconnect.GTTTypeOCO {
			triggerType = "two-leg"
		}
		
		triggerPrice := 0.0
		if len(g.Condition.TriggerValues) > 0 {
			triggerPrice = g.Condition.TriggerValues[0]
		}
		
		result[i] = models.GTTOrder{
			ID:           fmt.Sprintf("%d", g.ID),
			Symbol:       g.Condition.Tradingsymbol,
			Exchange:     models.Exchange(g.Condition.Exchange),
			TriggerType:  triggerType,
			TriggerPrice: triggerPrice,
			LastPrice:    g.Condition.LastPrice,
			Orders:       orders,
			Status:       g.Status,
			CreatedAt:    g.CreatedAt.Time,
			UpdatedAt:    g.UpdatedAt.Time,
		}
	}
	
	return result, nil
}


// GetPositions fetches current positions.
func (z *ZerodhaBroker) GetPositions(ctx context.Context) ([]models.Position, error) {
	if !z.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}
	
	positions, err := z.client.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}
	
	// Combine day and net positions
	allPositions := append(positions.Day, positions.Net...)
	
	result := make([]models.Position, 0, len(allPositions))
	seen := make(map[string]bool)
	
	for _, p := range allPositions {
		key := fmt.Sprintf("%s:%s:%s", p.Exchange, p.Tradingsymbol, p.Product)
		if seen[key] {
			continue
		}
		seen[key] = true
		
		if p.Quantity == 0 {
			continue
		}
		
		pnl := (p.LastPrice - p.AveragePrice) * float64(p.Quantity) * float64(p.Multiplier)
		pnlPercent := 0.0
		if p.AveragePrice > 0 {
			pnlPercent = ((p.LastPrice - p.AveragePrice) / p.AveragePrice) * 100
		}
		
		result = append(result, models.Position{
			Symbol:       p.Tradingsymbol,
			Exchange:     models.Exchange(p.Exchange),
			Product:      models.ProductType(p.Product),
			Quantity:     int(p.Quantity),
			AveragePrice: p.AveragePrice,
			LTP:          p.LastPrice,
			PnL:          pnl,
			PnLPercent:   pnlPercent,
			Value:        p.LastPrice * float64(p.Quantity) * float64(p.Multiplier),
			Multiplier:   int(p.Multiplier),
		})
	}
	
	return result, nil
}

// GetHoldings fetches delivery holdings.
func (z *ZerodhaBroker) GetHoldings(ctx context.Context) ([]models.Holding, error) {
	if !z.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}
	
	holdings, err := z.client.GetHoldings()
	if err != nil {
		return nil, fmt.Errorf("failed to get holdings: %w", err)
	}
	
	result := make([]models.Holding, len(holdings))
	for i, h := range holdings {
		investedValue := h.AveragePrice * float64(h.Quantity)
		currentValue := h.LastPrice * float64(h.Quantity)
		pnl := currentValue - investedValue
		pnlPercent := 0.0
		if investedValue > 0 {
			pnlPercent = (pnl / investedValue) * 100
		}
		
		result[i] = models.Holding{
			Symbol:        h.Tradingsymbol,
			Quantity:      int(h.Quantity),
			AveragePrice:  h.AveragePrice,
			LTP:           h.LastPrice,
			PnL:           pnl,
			PnLPercent:    pnlPercent,
			InvestedValue: investedValue,
			CurrentValue:  currentValue,
		}
	}
	
	return result, nil
}

// GetBalance fetches account balance.
func (z *ZerodhaBroker) GetBalance(ctx context.Context) (*models.Balance, error) {
	if !z.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}
	
	margins, err := z.client.GetUserMargins()
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}
	
	equity := margins.Equity
	
	return &models.Balance{
		AvailableCash:   equity.Available.Cash,
		UsedMargin:      equity.Used.Debits,
		TotalEquity:     equity.Net,
		CollateralValue: equity.Available.Collateral,
	}, nil
}

// GetMargins fetches margin details.
func (z *ZerodhaBroker) GetMargins(ctx context.Context) (*models.Margins, error) {
	if !z.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}
	
	margins, err := z.client.GetUserMargins()
	if err != nil {
		return nil, fmt.Errorf("failed to get margins: %w", err)
	}
	
	return &models.Margins{
		Equity: models.SegmentMargin{
			Available: margins.Equity.Available.Cash + margins.Equity.Available.Collateral,
			Used:      margins.Equity.Used.Debits,
			Total:     margins.Equity.Net,
		},
		Commodity: models.SegmentMargin{
			Available: margins.Commodity.Available.Cash + margins.Commodity.Available.Collateral,
			Used:      margins.Commodity.Used.Debits,
			Total:     margins.Commodity.Net,
		},
	}, nil
}


// GetOptionChain fetches option chain for a symbol.
func (z *ZerodhaBroker) GetOptionChain(ctx context.Context, symbol string, expiry time.Time) (*models.OptionChain, error) {
	if !z.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}
	
	// Get spot price
	quote, err := z.GetQuote(ctx, fmt.Sprintf("NSE:%s", symbol))
	if err != nil {
		return nil, fmt.Errorf("failed to get spot price: %w", err)
	}
	
	// Fetch NFO instruments for the symbol
	instruments, err := z.GetInstruments(ctx, models.NFO)
	if err != nil {
		return nil, fmt.Errorf("failed to get instruments: %w", err)
	}
	
	// Filter options for the symbol and expiry
	strikeMap := make(map[float64]*models.OptionStrike)
	
	for _, inst := range instruments {
		if inst.Name != symbol {
			continue
		}
		
		// Check expiry (same day)
		if !sameDay(inst.Expiry, expiry) {
			continue
		}
		
		strike, ok := strikeMap[inst.Strike]
		if !ok {
			strike = &models.OptionStrike{Strike: inst.Strike}
			strikeMap[inst.Strike] = strike
		}
		
		// Get quote for this option
		optSymbol := fmt.Sprintf("NFO:%s", inst.Symbol)
		optQuote, err := z.GetQuote(ctx, optSymbol)
		if err != nil {
			continue // Skip if quote fails
		}
		
		optData := &models.OptionData{
			LTP:    optQuote.LTP,
			Volume: optQuote.Volume,
		}
		
		if inst.InstrType == "CE" {
			strike.Call = optData
		} else if inst.InstrType == "PE" {
			strike.Put = optData
		}
	}
	
	// Convert map to sorted slice
	strikes := make([]models.OptionStrike, 0, len(strikeMap))
	for _, s := range strikeMap {
		strikes = append(strikes, *s)
	}
	
	return &models.OptionChain{
		Symbol:    symbol,
		SpotPrice: quote.LTP,
		Expiry:    expiry,
		Strikes:   strikes,
	}, nil
}

// GetFuturesChain fetches futures chain for a symbol.
func (z *ZerodhaBroker) GetFuturesChain(ctx context.Context, symbol string) (*models.FuturesChain, error) {
	if !z.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}
	
	// Get spot price
	quote, err := z.GetQuote(ctx, fmt.Sprintf("NSE:%s", symbol))
	if err != nil {
		return nil, fmt.Errorf("failed to get spot price: %w", err)
	}
	
	// Fetch NFO instruments for the symbol
	instruments, err := z.GetInstruments(ctx, models.NFO)
	if err != nil {
		return nil, fmt.Errorf("failed to get instruments: %w", err)
	}
	
	// Filter futures for the symbol
	var expiries []models.FuturesExpiry
	
	for _, inst := range instruments {
		if inst.Name != symbol || inst.InstrType != "FUT" {
			continue
		}
		
		// Get quote for this future
		futSymbol := fmt.Sprintf("NFO:%s", inst.Symbol)
		futQuote, err := z.GetQuote(ctx, futSymbol)
		if err != nil {
			continue
		}
		
		basis := futQuote.LTP - quote.LTP
		basisPercent := 0.0
		if quote.LTP > 0 {
			basisPercent = (basis / quote.LTP) * 100
		}
		
		expiries = append(expiries, models.FuturesExpiry{
			Expiry:       inst.Expiry,
			LTP:          futQuote.LTP,
			Volume:       futQuote.Volume,
			Basis:        basis,
			BasisPercent: basisPercent,
		})
	}
	
	return &models.FuturesChain{
		Symbol:    symbol,
		SpotPrice: quote.LTP,
		Expiries:  expiries,
	}, nil
}

func sameDay(t1, t2 time.Time) bool {
	y1, m1, d1 := t1.Date()
	y2, m2, d2 := t2.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

// GetLoginURL returns the Zerodha login URL for OAuth.
func (z *ZerodhaBroker) GetLoginURL() string {
	return z.client.GetLoginURL()
}

// Ensure ZerodhaBroker implements Broker interface
var _ Broker = (*ZerodhaBroker)(nil)
