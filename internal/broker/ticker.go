// Package broker provides broker integration implementations.
package broker

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	kitemodels "github.com/zerodha/gokiteconnect/v4/models"
	kiteticker "github.com/zerodha/gokiteconnect/v4/ticker"

	"zerodha-trader/internal/models"
)

// ZerodhaTicker implements the Ticker interface for Zerodha WebSocket streaming.
type ZerodhaTicker struct {
	ticker      *kiteticker.Ticker
	apiKey      string
	accessToken string
	
	// Handlers
	onTick       func(models.Tick)
	onError      func(error)
	onConnect    func()
	onDisconnect func()
	
	// State
	connected    bool
	subscribed   map[uint32]TickMode
	symbolTokens map[string]uint32
	tokenSymbols map[uint32]string
	
	// Reconnection
	reconnecting bool
	maxRetries   int
	baseDelay    time.Duration
	
	mu      sync.RWMutex
	writeMu sync.Mutex // Protects websocket writes (Subscribe, SetMode)
}

// ZerodhaTickerConfig holds configuration for the ticker.
type ZerodhaTickerConfig struct {
	APIKey      string
	AccessToken string
	MaxRetries  int
	BaseDelay   time.Duration
}

// NewZerodhaTicker creates a new Zerodha ticker instance.
func NewZerodhaTicker(cfg ZerodhaTickerConfig) *ZerodhaTicker {
	maxRetries := cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = 5
	}
	
	baseDelay := cfg.BaseDelay
	if baseDelay == 0 {
		baseDelay = time.Second
	}
	
	return &ZerodhaTicker{
		apiKey:       cfg.APIKey,
		accessToken:  cfg.AccessToken,
		subscribed:   make(map[uint32]TickMode),
		symbolTokens: make(map[string]uint32),
		tokenSymbols: make(map[uint32]string),
		maxRetries:   maxRetries,
		baseDelay:    baseDelay,
	}
}


// Connect establishes WebSocket connection with Kite Connect.
func (t *ZerodhaTicker) Connect(ctx context.Context) error {
	t.mu.Lock()
	if t.connected {
		t.mu.Unlock()
		return nil
	}
	
	// Create ticker instance
	t.ticker = kiteticker.New(t.apiKey, t.accessToken)
	
	// Channel to signal connection
	connectedCh := make(chan struct{})
	
	// Track first connection
	firstConnect := true
	
	// Set up callbacks
	t.ticker.OnConnect(func() {
		t.mu.Lock()
		t.connected = true
		t.reconnecting = false
		isFirst := firstConnect
		firstConnect = false
		t.mu.Unlock()
		
		// Signal connection
		select {
		case connectedCh <- struct{}{}:
		default:
		}
		
		// On reconnection, resubscribe to previously subscribed symbols
		// On first connection, the external handler will subscribe
		if !isFirst {
			t.resubscribe()
			// Don't call external onConnect on reconnection to avoid duplicate subscriptions
			return
		}
		
		if t.onConnect != nil {
			go t.onConnect()
		}
	})
	
	t.ticker.OnClose(func(code int, reason string) {
		t.mu.Lock()
		wasConnected := t.connected
		t.connected = false
		t.mu.Unlock()
		
		if t.onDisconnect != nil && wasConnected {
			go t.onDisconnect()
		}
		
		// Attempt reconnection
		go t.reconnect(ctx)
	})
	
	t.ticker.OnError(func(err error) {
		if t.onError != nil {
			go t.onError(err)
		}
	})
	
	t.ticker.OnTick(func(tick kitemodels.Tick) {
		if t.onTick != nil {
			modelTick := t.convertTick(tick)
			go t.onTick(modelTick)
		}
	})
	
	t.ticker.OnReconnect(func(attempt int, delay time.Duration) {
		t.mu.Lock()
		t.reconnecting = true
		t.mu.Unlock()
	})
	
	t.mu.Unlock() // Release lock before starting connection
	
	// Start connection in goroutine
	go t.ticker.Serve()
	
	// Wait for connection or timeout
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-connectedCh:
		return nil
	case <-time.After(30 * time.Second):
		t.mu.RLock()
		connected := t.connected
		t.mu.RUnlock()
		if !connected {
			return fmt.Errorf("connection timeout")
		}
		return nil
	}
}

// Disconnect closes the WebSocket connection.
func (t *ZerodhaTicker) Disconnect() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	if t.ticker != nil {
		t.ticker.Close()
		t.connected = false
	}
	
	return nil
}

// Subscribe subscribes to symbols with the specified mode.
func (t *ZerodhaTicker) Subscribe(symbols []string, mode TickMode) error {
	t.mu.Lock()
	
	if !t.connected {
		t.mu.Unlock()
		return fmt.Errorf("not connected")
	}
	
	tokens := make([]uint32, 0, len(symbols))
	for _, symbol := range symbols {
		token, ok := t.symbolTokens[symbol]
		if !ok {
			// Symbol not registered - skip
			continue
		}
		tokens = append(tokens, token)
		t.subscribed[token] = mode
	}
	t.mu.Unlock()
	
	if len(tokens) == 0 {
		return nil
	}
	
	// Lock for websocket writes
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	
	// Subscribe to tokens
	if err := t.ticker.Subscribe(tokens); err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}
	
	// Set mode
	kiteMode := kiteticker.ModeQuote
	if mode == TickModeFull {
		kiteMode = kiteticker.ModeFull
	}
	
	if err := t.ticker.SetMode(kiteMode, tokens); err != nil {
		return fmt.Errorf("failed to set mode: %w", err)
	}
	
	return nil
}

// Unsubscribe unsubscribes from symbols.
func (t *ZerodhaTicker) Unsubscribe(symbols []string) error {
	t.mu.Lock()
	
	if !t.connected {
		t.mu.Unlock()
		return fmt.Errorf("not connected")
	}
	
	tokens := make([]uint32, 0, len(symbols))
	for _, symbol := range symbols {
		token, ok := t.symbolTokens[symbol]
		if ok {
			tokens = append(tokens, token)
			delete(t.subscribed, token)
		}
	}
	t.mu.Unlock()
	
	if len(tokens) == 0 {
		return nil
	}
	
	// Lock for websocket writes
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	
	if err := t.ticker.Unsubscribe(tokens); err != nil {
		return fmt.Errorf("failed to unsubscribe: %w", err)
	}
	
	return nil
}

// OnTick sets the tick handler.
func (t *ZerodhaTicker) OnTick(handler func(models.Tick)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onTick = handler
}

// OnError sets the error handler.
func (t *ZerodhaTicker) OnError(handler func(error)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onError = handler
}

// OnConnect sets the connect handler.
func (t *ZerodhaTicker) OnConnect(handler func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onConnect = handler
}

// OnDisconnect sets the disconnect handler.
func (t *ZerodhaTicker) OnDisconnect(handler func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onDisconnect = handler
}


// RegisterSymbol registers a symbol with its instrument token.
func (t *ZerodhaTicker) RegisterSymbol(symbol string, token uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.symbolTokens[symbol] = token
	t.tokenSymbols[token] = symbol
}

// RegisterSymbols registers multiple symbols with their tokens.
func (t *ZerodhaTicker) RegisterSymbols(symbolTokens map[string]uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for symbol, token := range symbolTokens {
		t.symbolTokens[symbol] = token
		t.tokenSymbols[token] = symbol
	}
}

// IsConnected returns whether the ticker is connected.
func (t *ZerodhaTicker) IsConnected() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.connected
}

// convertTick converts a Kite ticker tick to our model.
func (t *ZerodhaTicker) convertTick(tick kitemodels.Tick) models.Tick {
	t.mu.RLock()
	symbol := t.tokenSymbols[tick.InstrumentToken]
	t.mu.RUnlock()
	
	return models.Tick{
		Symbol:       symbol,
		LTP:          tick.LastPrice,
		Open:         tick.OHLC.Open,
		High:         tick.OHLC.High,
		Low:          tick.OHLC.Low,
		Close:        tick.OHLC.Close,
		Volume:       int64(tick.VolumeTraded),
		BuyQuantity:  int64(tick.TotalBuyQuantity),
		SellQuantity: int64(tick.TotalSellQuantity),
		BidPrice:     getBestBid(tick),
		AskPrice:     getBestAsk(tick),
		Timestamp:    tick.Timestamp.Time,
	}
}

func getBestBid(tick kitemodels.Tick) float64 {
	if len(tick.Depth.Buy) > 0 {
		return tick.Depth.Buy[0].Price
	}
	return 0
}

func getBestAsk(tick kitemodels.Tick) float64 {
	if len(tick.Depth.Sell) > 0 {
		return tick.Depth.Sell[0].Price
	}
	return 0
}

// reconnect attempts to reconnect with exponential backoff.
func (t *ZerodhaTicker) reconnect(ctx context.Context) {
	t.mu.Lock()
	if t.reconnecting {
		t.mu.Unlock()
		return
	}
	t.reconnecting = true
	t.mu.Unlock()
	
	for attempt := 0; attempt < t.maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}
		
		// Exponential backoff
		delay := t.baseDelay * time.Duration(math.Pow(2, float64(attempt)))
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		
		time.Sleep(delay)
		
		// Try to reconnect
		t.mu.Lock()
		if t.connected {
			t.reconnecting = false
			t.mu.Unlock()
			return
		}
		t.mu.Unlock()
		
		// Recreate ticker and connect
		if err := t.Connect(ctx); err == nil {
			return
		}
	}
	
	t.mu.Lock()
	t.reconnecting = false
	t.mu.Unlock()
	
	if t.onError != nil {
		t.onError(fmt.Errorf("max reconnection attempts reached"))
	}
}

// resubscribe resubscribes to all previously subscribed symbols.
func (t *ZerodhaTicker) resubscribe() {
	t.mu.RLock()
	subscribed := make(map[uint32]TickMode)
	for token, mode := range t.subscribed {
		subscribed[token] = mode
	}
	t.mu.RUnlock()
	
	if len(subscribed) == 0 {
		return
	}
	
	// Group by mode
	quoteTokens := make([]uint32, 0)
	fullTokens := make([]uint32, 0)
	
	for token, mode := range subscribed {
		if mode == TickModeFull {
			fullTokens = append(fullTokens, token)
		} else {
			quoteTokens = append(quoteTokens, token)
		}
	}
	
	// Lock for websocket writes
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	
	// Subscribe and set modes
	if len(quoteTokens) > 0 {
		t.ticker.Subscribe(quoteTokens)
		t.ticker.SetMode(kiteticker.ModeQuote, quoteTokens)
	}
	
	if len(fullTokens) > 0 {
		t.ticker.Subscribe(fullTokens)
		t.ticker.SetMode(kiteticker.ModeFull, fullTokens)
	}
}

// Ensure ZerodhaTicker implements Ticker interface
var _ Ticker = (*ZerodhaTicker)(nil)
