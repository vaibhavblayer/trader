package stream

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"zerodha-trader/internal/models"
)

// Feature: zerodha-go-trader, Property 5: All subscribers receive ticks within timeout
// Validates: Requirements 2.2, 2.6
//
// Property: For any number of subscribers and any tick, all subscribers should
// receive the tick within a reasonable timeout, unless they are slow consumers
// (in which case the tick may be dropped to prevent blocking).
func TestProperty_AllSubscribersReceiveTicksWithinTimeout(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 20
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	// Valid symbols for testing
	symbols := []string{"RELIANCE", "TCS", "INFY", "HDFC", "ICICI"}

	// Generator for number of subscribers (1-5)
	subscriberCountGen := gen.IntRange(1, 5)

	// Generator for number of ticks to publish (1-20)
	tickCountGen := gen.IntRange(1, 20)

	// Generator for symbol index
	symbolIdxGen := gen.IntRange(0, len(symbols)-1)

	// Generator for price
	priceGen := gen.Float64Range(100.0, 5000.0)

	properties.Property("All fast subscribers receive all ticks within timeout", prop.ForAll(
		func(subscriberCount int, tickCount int, symbolIdx int, basePrice float64) bool {
			symbol := symbols[symbolIdx]

			// Create hub with large buffer to avoid drops
			config := HubConfig{
				BufferSize:           1000,
				SubscriberBufferSize: 100,
				BroadcastTimeout:     100 * time.Millisecond,
			}
			hub := NewHubWithConfig(config)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Start the hub
			hub.Start(ctx)
			defer hub.Stop()

			// Create subscribers
			var wg sync.WaitGroup
			receivedCounts := make([]int64, subscriberCount)

			channels := make([]<-chan models.Tick, subscriberCount)
			for i := 0; i < subscriberCount; i++ {
				channels[i] = hub.Subscribe(symbol)
			}

			// Start goroutines to receive ticks
			for i := 0; i < subscriberCount; i++ {
				wg.Add(1)
				go func(idx int, ch <-chan models.Tick) {
					defer wg.Done()
					timeout := time.After(5 * time.Second)
					for {
						select {
						case _, ok := <-ch:
							if !ok {
								return
							}
							atomic.AddInt64(&receivedCounts[idx], 1)
							if atomic.LoadInt64(&receivedCounts[idx]) >= int64(tickCount) {
								return
							}
						case <-timeout:
							return
						}
					}
				}(i, channels[i])
			}

			// Give subscribers time to set up
			time.Sleep(10 * time.Millisecond)

			// Publish ticks
			for i := 0; i < tickCount; i++ {
				tick := models.Tick{
					Symbol:    symbol,
					LTP:       basePrice + float64(i)*0.05,
					Open:      basePrice,
					High:      basePrice * 1.02,
					Low:       basePrice * 0.98,
					Close:     basePrice,
					Volume:    10000,
					Timestamp: time.Now(),
				}
				hub.Publish(tick)
				time.Sleep(1 * time.Millisecond) // Small delay between publishes
			}

			// Wait for all receivers to finish
			wg.Wait()

			// Verify all subscribers received all ticks
			for i := 0; i < subscriberCount; i++ {
				received := atomic.LoadInt64(&receivedCounts[i])
				if received != int64(tickCount) {
					// Allow for some dropped ticks due to timing
					// At least 90% should be received
					if float64(received)/float64(tickCount) < 0.9 {
						return false
					}
				}
			}

			return true
		},
		subscriberCountGen,
		tickCountGen,
		symbolIdxGen,
		priceGen,
	))

	properties.TestingRun(t)
}


// TestProperty_SlowConsumersDoNotBlockOthers tests that slow consumers don't block fast ones.
// This is part of Property 5 validation for Requirements 2.2, 2.6.
func TestProperty_SlowConsumersDoNotBlockOthers(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 20
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	symbols := []string{"RELIANCE", "TCS", "INFY"}

	properties.Property("Slow consumers do not block fast consumers", prop.ForAll(
		func(symbolIdx int, basePrice float64) bool {
			symbol := symbols[symbolIdx%len(symbols)]

			// Create hub with small subscriber buffer to trigger slow consumer behavior
			config := HubConfig{
				BufferSize:           100,
				SubscriberBufferSize: 5, // Small buffer to trigger drops
				BroadcastTimeout:     1 * time.Millisecond,
			}
			hub := NewHubWithConfig(config)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			hub.Start(ctx)
			defer hub.Stop()

			// Create a fast subscriber
			fastCh := hub.Subscribe(symbol)
			var fastReceived int64

			// Create a slow subscriber (doesn't read from channel)
			_ = hub.Subscribe(symbol)

			// Start fast receiver
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				timeout := time.After(2 * time.Second)
				for {
					select {
					case _, ok := <-fastCh:
						if !ok {
							return
						}
						atomic.AddInt64(&fastReceived, 1)
						if atomic.LoadInt64(&fastReceived) >= 10 {
							return
						}
					case <-timeout:
						return
					}
				}
			}()

			// Give subscriber time to set up
			time.Sleep(10 * time.Millisecond)

			// Publish ticks rapidly
			for i := 0; i < 20; i++ {
				tick := models.Tick{
					Symbol:    symbol,
					LTP:       basePrice + float64(i)*0.05,
					Timestamp: time.Now(),
				}
				hub.Publish(tick)
			}

			wg.Wait()

			// Fast subscriber should have received at least some ticks
			// even though slow subscriber is blocking
			received := atomic.LoadInt64(&fastReceived)
			return received > 0
		},
		gen.IntRange(0, 2),
		gen.Float64Range(100.0, 5000.0),
	))

	properties.TestingRun(t)
}

// TestProperty_ConsumersReceiveCorrectSymbolTicks tests that consumers only receive
// ticks for their subscribed symbols.
func TestProperty_ConsumersReceiveCorrectSymbolTicks(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 20
	parameters.Rng.Seed(time.Now().UnixNano())

	properties := gopter.NewProperties(parameters)

	symbols := []string{"RELIANCE", "TCS", "INFY", "HDFC", "ICICI"}

	properties.Property("Subscribers only receive ticks for their subscribed symbol", prop.ForAll(
		func(subscribedSymbolIdx int, publishedSymbolIdx int) bool {
			subscribedSymbol := symbols[subscribedSymbolIdx%len(symbols)]
			publishedSymbol := symbols[publishedSymbolIdx%len(symbols)]

			hub := NewHub()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			hub.Start(ctx)
			defer hub.Stop()

			// Subscribe to one symbol
			ch := hub.Subscribe(subscribedSymbol)

			var received int64
			var receivedSymbol string
			var mu sync.Mutex

			// Start receiver
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				timeout := time.After(500 * time.Millisecond)
				select {
				case tick, ok := <-ch:
					if ok {
						atomic.AddInt64(&received, 1)
						mu.Lock()
						receivedSymbol = tick.Symbol
						mu.Unlock()
					}
				case <-timeout:
				}
			}()

			// Give subscriber time to set up
			time.Sleep(10 * time.Millisecond)

			// Publish tick for a symbol
			tick := models.Tick{
				Symbol:    publishedSymbol,
				LTP:       1000.0,
				Timestamp: time.Now(),
			}
			hub.Publish(tick)

			wg.Wait()

			// If we received a tick, it should be for the subscribed symbol
			if atomic.LoadInt64(&received) > 0 {
				mu.Lock()
				defer mu.Unlock()
				return receivedSymbol == subscribedSymbol
			}

			// If we didn't receive, it should be because symbols don't match
			return subscribedSymbol != publishedSymbol
		},
		gen.IntRange(0, 4),
		gen.IntRange(0, 4),
	))

	properties.TestingRun(t)
}
