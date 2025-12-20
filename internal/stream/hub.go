// Package stream provides real-time data streaming and distribution functionality.
package stream

import (
	"context"
	"sync"
	"time"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

// HubConfig holds configuration for the Stream Hub.
type HubConfig struct {
	// BufferSize is the size of the internal tick channel buffer.
	BufferSize int
	// SubscriberBufferSize is the size of each subscriber's channel buffer.
	SubscriberBufferSize int
	// BroadcastTimeout is the maximum time to wait when sending to a subscriber.
	BroadcastTimeout time.Duration
	// SlowConsumerDropThreshold is the number of consecutive drops before logging.
	SlowConsumerDropThreshold int
}

// DefaultHubConfig returns the default hub configuration.
func DefaultHubConfig() HubConfig {
	return HubConfig{
		BufferSize:                1000,
		SubscriberBufferSize:      100,
		BroadcastTimeout:          10 * time.Millisecond,
		SlowConsumerDropThreshold: 10,
	}
}

// Hub manages WebSocket data distribution to multiple consumers.
// It implements a fan-out pattern where ticks from a single source
// are distributed to multiple subscribers via channels.
type Hub struct {
	config      HubConfig
	ticker      broker.Ticker
	mu          sync.RWMutex
	subscribers map[string][]*Subscriber
	tickChan    chan models.Tick
	done        chan struct{}
	started     bool
	consumers   []Consumer
	consumersMu sync.RWMutex

	// Metrics
	ticksReceived   uint64
	ticksBroadcast  uint64
	ticksDropped    uint64
	metricsMu       sync.RWMutex
}

// Subscriber represents a channel subscriber with metadata.
type Subscriber struct {
	ID           string
	Channel      chan models.Tick
	DroppedCount int
	CreatedAt    time.Time
}

// NewHub creates a new stream hub with default configuration.
func NewHub() *Hub {
	return NewHubWithConfig(DefaultHubConfig())
}

// NewHubWithConfig creates a new stream hub with custom configuration.
func NewHubWithConfig(config HubConfig) *Hub {
	return &Hub{
		config:      config,
		subscribers: make(map[string][]*Subscriber),
		tickChan:    make(chan models.Tick, config.BufferSize),
		done:        make(chan struct{}),
		consumers:   make([]Consumer, 0),
	}
}

// NewHubWithTicker creates a new stream hub with a ticker.
func NewHubWithTicker(ticker broker.Ticker) *Hub {
	h := NewHub()
	h.ticker = ticker
	return h
}


// SetTicker sets the ticker for the hub.
func (h *Hub) SetTicker(ticker broker.Ticker) {
	h.ticker = ticker
}

// Start begins the hub's distribution loop.
// It starts a goroutine that listens for ticks and broadcasts them to subscribers.
func (h *Hub) Start(ctx context.Context) error {
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return nil
	}
	h.started = true
	h.mu.Unlock()

	// Start the broadcast goroutine
	go h.broadcastLoop(ctx)

	// If we have a ticker, connect and set up handlers
	if h.ticker != nil {
		h.ticker.OnTick(func(tick models.Tick) {
			h.Publish(tick)
		})

		h.ticker.OnError(func(err error) {
			// Log error but continue - the ticker handles reconnection
		})

		if err := h.ticker.Connect(ctx); err != nil {
			return err
		}
	}

	return nil
}

// broadcastLoop is the main loop that distributes ticks to subscribers.
func (h *Hub) broadcastLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.done:
			return
		case tick := <-h.tickChan:
			h.metricsMu.Lock()
			h.ticksReceived++
			h.metricsMu.Unlock()

			h.broadcast(tick)
			h.notifyConsumers(tick)
		}
	}
}

// Stop stops the hub and closes all subscriber channels.
func (h *Hub) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.started {
		return
	}

	close(h.done)
	h.started = false

	// Close all subscriber channels
	for symbol, subs := range h.subscribers {
		for _, sub := range subs {
			close(sub.Channel)
		}
		delete(h.subscribers, symbol)
	}

	// Disconnect ticker if present
	if h.ticker != nil {
		h.ticker.Disconnect()
	}
}

// Subscribe adds a subscriber for a symbol and returns a channel to receive ticks.
func (h *Hub) Subscribe(symbol string) <-chan models.Tick {
	return h.SubscribeWithID(symbol, "")
}

// SubscribeWithID adds a subscriber with a specific ID for a symbol.
func (h *Hub) SubscribeWithID(symbol, id string) <-chan models.Tick {
	ch := make(chan models.Tick, h.config.SubscriberBufferSize)
	sub := &Subscriber{
		ID:        id,
		Channel:   ch,
		CreatedAt: time.Now(),
	}

	h.mu.Lock()
	h.subscribers[symbol] = append(h.subscribers[symbol], sub)
	h.mu.Unlock()

	// Subscribe to ticker if available
	if h.ticker != nil {
		h.ticker.Subscribe([]string{symbol}, broker.TickModeFull)
	}

	return ch
}

// SubscribeMultiple subscribes to multiple symbols at once.
func (h *Hub) SubscribeMultiple(symbols []string) map[string]<-chan models.Tick {
	result := make(map[string]<-chan models.Tick)
	for _, symbol := range symbols {
		result[symbol] = h.Subscribe(symbol)
	}
	return result
}

// Unsubscribe removes a subscriber channel for a symbol.
func (h *Hub) Unsubscribe(symbol string, ch <-chan models.Tick) {
	h.mu.Lock()
	defer h.mu.Unlock()

	subs := h.subscribers[symbol]
	for i, sub := range subs {
		if sub.Channel == ch {
			// Close the channel
			close(sub.Channel)
			// Remove from slice
			h.subscribers[symbol] = append(subs[:i], subs[i+1:]...)
			break
		}
	}

	// If no more subscribers for this symbol, unsubscribe from ticker
	if len(h.subscribers[symbol]) == 0 {
		delete(h.subscribers, symbol)
		if h.ticker != nil {
			h.ticker.Unsubscribe([]string{symbol})
		}
	}
}

// UnsubscribeAll removes all subscribers for a symbol.
func (h *Hub) UnsubscribeAll(symbol string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	subs := h.subscribers[symbol]
	for _, sub := range subs {
		close(sub.Channel)
	}
	delete(h.subscribers, symbol)

	if h.ticker != nil {
		h.ticker.Unsubscribe([]string{symbol})
	}
}


// Publish sends a tick to the hub for distribution.
// This is non-blocking - if the internal buffer is full, the tick is dropped.
func (h *Hub) Publish(tick models.Tick) {
	select {
	case h.tickChan <- tick:
	default:
		// Drop tick if channel is full (slow consumer protection)
		h.metricsMu.Lock()
		h.ticksDropped++
		h.metricsMu.Unlock()
	}
}

// PublishWithTimeout sends a tick with a timeout.
// Returns true if the tick was published, false if it timed out.
func (h *Hub) PublishWithTimeout(tick models.Tick, timeout time.Duration) bool {
	select {
	case h.tickChan <- tick:
		return true
	case <-time.After(timeout):
		h.metricsMu.Lock()
		h.ticksDropped++
		h.metricsMu.Unlock()
		return false
	}
}

// broadcast sends a tick to all subscribers of that symbol.
// Uses non-blocking sends to prevent slow consumers from blocking others.
func (h *Hub) broadcast(tick models.Tick) {
	h.mu.RLock()
	subs := h.subscribers[tick.Symbol]
	h.mu.RUnlock()

	for _, sub := range subs {
		select {
		case sub.Channel <- tick:
			h.metricsMu.Lock()
			h.ticksBroadcast++
			h.metricsMu.Unlock()
		default:
			// Skip slow consumers - non-blocking
			sub.DroppedCount++
			h.metricsMu.Lock()
			h.ticksDropped++
			h.metricsMu.Unlock()
		}
	}
}

// BroadcastAll sends a tick to all subscribers regardless of symbol.
// Useful for system-wide notifications.
func (h *Hub) BroadcastAll(tick models.Tick) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, subs := range h.subscribers {
		for _, sub := range subs {
			select {
			case sub.Channel <- tick:
			default:
				sub.DroppedCount++
			}
		}
	}
}

// GetSubscriberCount returns the number of subscribers for a symbol.
func (h *Hub) GetSubscriberCount(symbol string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers[symbol])
}

// GetTotalSubscriberCount returns the total number of subscribers across all symbols.
func (h *Hub) GetTotalSubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	count := 0
	for _, subs := range h.subscribers {
		count += len(subs)
	}
	return count
}

// GetSubscribedSymbols returns all symbols with active subscribers.
func (h *Hub) GetSubscribedSymbols() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	symbols := make([]string, 0, len(h.subscribers))
	for symbol := range h.subscribers {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// GetMetrics returns hub metrics.
func (h *Hub) GetMetrics() HubMetrics {
	h.metricsMu.RLock()
	defer h.metricsMu.RUnlock()

	return HubMetrics{
		TicksReceived:  h.ticksReceived,
		TicksBroadcast: h.ticksBroadcast,
		TicksDropped:   h.ticksDropped,
		Subscribers:    h.GetTotalSubscriberCount(),
		Symbols:        len(h.GetSubscribedSymbols()),
	}
}

// HubMetrics contains hub performance metrics.
type HubMetrics struct {
	TicksReceived  uint64
	TicksBroadcast uint64
	TicksDropped   uint64
	Subscribers    int
	Symbols        int
}

// IsStarted returns whether the hub is running.
func (h *Hub) IsStarted() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.started
}


// Consumer represents a tick consumer that processes ticks.
type Consumer interface {
	// OnTick is called when a new tick is received.
	OnTick(tick models.Tick)
	// Symbols returns the symbols this consumer is interested in.
	// Return nil or empty slice to receive all ticks.
	Symbols() []string
}

// RegisterConsumer adds a consumer to receive ticks.
// Each consumer runs in its own goroutine.
func (h *Hub) RegisterConsumer(consumer Consumer) {
	h.consumersMu.Lock()
	h.consumers = append(h.consumers, consumer)
	h.consumersMu.Unlock()
}

// UnregisterConsumer removes a consumer.
func (h *Hub) UnregisterConsumer(consumer Consumer) {
	h.consumersMu.Lock()
	defer h.consumersMu.Unlock()

	for i, c := range h.consumers {
		if c == consumer {
			h.consumers = append(h.consumers[:i], h.consumers[i+1:]...)
			break
		}
	}
}

// notifyConsumers sends a tick to all registered consumers.
// Each consumer is notified in a separate goroutine to prevent blocking.
func (h *Hub) notifyConsumers(tick models.Tick) {
	h.consumersMu.RLock()
	consumers := make([]Consumer, len(h.consumers))
	copy(consumers, h.consumers)
	h.consumersMu.RUnlock()

	for _, consumer := range consumers {
		symbols := consumer.Symbols()
		// If consumer has no symbol filter, or tick symbol matches
		if len(symbols) == 0 || containsSymbol(symbols, tick.Symbol) {
			// Run in goroutine to prevent blocking
			go consumer.OnTick(tick)
		}
	}
}

// containsSymbol checks if a symbol is in the list.
func containsSymbol(symbols []string, symbol string) bool {
	for _, s := range symbols {
		if s == symbol {
			return true
		}
	}
	return false
}

// ConsumerFunc is a function adapter for Consumer interface.
type ConsumerFunc struct {
	symbols   []string
	onTickFn  func(models.Tick)
}

// NewConsumerFunc creates a new ConsumerFunc.
func NewConsumerFunc(symbols []string, onTick func(models.Tick)) *ConsumerFunc {
	return &ConsumerFunc{
		symbols:  symbols,
		onTickFn: onTick,
	}
}

// OnTick implements Consumer.
func (c *ConsumerFunc) OnTick(tick models.Tick) {
	if c.onTickFn != nil {
		c.onTickFn(tick)
	}
}

// Symbols implements Consumer.
func (c *ConsumerFunc) Symbols() []string {
	return c.symbols
}
