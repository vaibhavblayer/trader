// Package performance provides performance benchmarks and tests.
// Requirements: 18, 24 (Concurrent Data Processing, Asynchronous Operations)
package performance

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"zerodha-trader/internal/analysis/indicators"
	"zerodha-trader/internal/analysis/scoring"
	"zerodha-trader/internal/models"
	"zerodha-trader/internal/stream"
)

// BenchmarkWorkerPool benchmarks the worker pool performance.
func BenchmarkWorkerPool(b *testing.B) {
	pool := NewWorkerPool(4)
	pool.Start()
	defer pool.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		wg.Add(1)
		pool.Submit(func() {
			// Simulate some work
			time.Sleep(time.Microsecond)
			wg.Done()
		})
		wg.Wait()
	}
}

// BenchmarkWorkerPoolParallel benchmarks parallel task submission.
func BenchmarkWorkerPoolParallel(b *testing.B) {
	pool := NewWorkerPool(8)
	pool.Start()
	defer pool.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			done := make(chan struct{})
			pool.Submit(func() {
				close(done)
			})
			<-done
		}
	})
}

// BenchmarkRateLimiter benchmarks the rate limiter.
func BenchmarkRateLimiter(b *testing.B) {
	limiter := NewRateLimiter(10000, 100) // 10k requests/sec

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow()
	}
}

// BenchmarkObjectPool benchmarks object pool vs direct allocation.
func BenchmarkObjectPool(b *testing.B) {
	type TestObject struct {
		Data [1024]byte
	}

	pool := NewObjectPool(func() *TestObject {
		return &TestObject{}
	})

	b.Run("WithPool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			obj := pool.Get()
			pool.Put(obj)
		}
	})

	b.Run("WithoutPool", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = &TestObject{}
		}
	})
}

// BenchmarkBatchProcessor benchmarks batch processing.
func BenchmarkBatchProcessor(b *testing.B) {
	var processed int64

	processor := NewBatchProcessor(100, func(items []int) error {
		atomic.AddInt64(&processed, int64(len(items)))
		return nil
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		processor.Add(i)
	}
	processor.Flush()
}

// BenchmarkStreamHub benchmarks the stream hub tick distribution.
func BenchmarkStreamHub(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := stream.NewHub()
	hub.Start(ctx)
	defer hub.Stop()

	// Create subscribers
	numSubscribers := 10
	channels := make([]<-chan models.Tick, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		channels[i] = hub.Subscribe("TEST")
	}

	// Start consumers
	var wg sync.WaitGroup
	for _, ch := range channels {
		wg.Add(1)
		go func(c <-chan models.Tick) {
			defer wg.Done()
			for range c {
				// Consume ticks
			}
		}(ch)
	}

	tick := models.Tick{
		Symbol:    "TEST",
		LTP:       1000.0,
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hub.Publish(tick)
	}

	hub.Stop()
	wg.Wait()
}

// BenchmarkIndicatorCalculation benchmarks indicator calculations.
func BenchmarkIndicatorCalculation(b *testing.B) {
	// Generate test candles
	candles := generateTestCandles(500)

	b.Run("RSI", func(b *testing.B) {
		rsi := indicators.NewRSI(14)
		for i := 0; i < b.N; i++ {
			rsi.Calculate(candles)
		}
	})

	b.Run("MACD", func(b *testing.B) {
		macd := indicators.NewMACD(12, 26, 9)
		for i := 0; i < b.N; i++ {
			macd.Calculate(candles)
		}
	})

	b.Run("BollingerBands", func(b *testing.B) {
		bb := indicators.NewBollingerBands(20, 2.0)
		for i := 0; i < b.N; i++ {
			bb.Calculate(candles)
		}
	})

	b.Run("ATR", func(b *testing.B) {
		atr := indicators.NewATR(14)
		for i := 0; i < b.N; i++ {
			atr.Calculate(candles)
		}
	})

	b.Run("ADX", func(b *testing.B) {
		adx := indicators.NewADX(14)
		for i := 0; i < b.N; i++ {
			adx.Calculate(candles)
		}
	})
}

// BenchmarkIndicatorEngine benchmarks parallel indicator calculation.
func BenchmarkIndicatorEngine(b *testing.B) {
	engine := indicators.NewEngine(4)
	candles := generateTestCandles(500)

	// Register indicators
	engine.RegisterIndicator(indicators.NewRSI(14))
	engine.RegisterIndicator(indicators.NewSMA(20))
	engine.RegisterIndicator(indicators.NewEMA(20))
	engine.RegisterIndicator(indicators.NewATR(14))
	engine.RegisterMultiIndicator(indicators.NewMACD(12, 26, 9))
	engine.RegisterMultiIndicator(indicators.NewBollingerBands(20, 2.0))
	engine.RegisterMultiIndicator(indicators.NewADX(14))

	b.Run("Sequential", func(b *testing.B) {
		rsi := indicators.NewRSI(14)
		macd := indicators.NewMACD(12, 26, 9)
		bb := indicators.NewBollingerBands(20, 2.0)
		atr := indicators.NewATR(14)
		adx := indicators.NewADX(14)
		sma := indicators.NewSMA(20)
		ema := indicators.NewEMA(20)

		for i := 0; i < b.N; i++ {
			rsi.Calculate(candles)
			macd.Calculate(candles)
			bb.Calculate(candles)
			atr.Calculate(candles)
			adx.Calculate(candles)
			sma.Calculate(candles)
			ema.Calculate(candles)
		}
	})

	b.Run("Parallel", func(b *testing.B) {
		ctx := context.Background()
		for i := 0; i < b.N; i++ {
			engine.CalculateAll(ctx, candles)
		}
	})
}

// BenchmarkSignalScoring benchmarks signal scoring.
func BenchmarkSignalScoring(b *testing.B) {
	candles := generateTestCandles(500)
	engine := indicators.NewEngine(4)
	scorer := scoring.NewSignalScorer(engine)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scorer.Score(ctx, candles)
	}
}

// BenchmarkConcurrentSymbolProcessing benchmarks processing multiple symbols concurrently.
func BenchmarkConcurrentSymbolProcessing(b *testing.B) {
	symbols := []string{"RELIANCE", "TCS", "INFY", "HDFC", "ICICI", "SBIN", "WIPRO", "HCLTECH", "BHARTIARTL", "ITC"}
	candlesMap := make(map[string][]models.Candle)
	for _, sym := range symbols {
		candlesMap[sym] = generateTestCandles(500)
	}

	engine := indicators.NewEngine(4)
	scorer := scoring.NewSignalScorer(engine)
	ctx := context.Background()

	b.Run("Sequential", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, sym := range symbols {
				scorer.Score(ctx, candlesMap[sym])
			}
		}
	})

	b.Run("Parallel", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var wg sync.WaitGroup
			for _, sym := range symbols {
				wg.Add(1)
				go func(s string) {
					defer wg.Done()
					scorer.Score(ctx, candlesMap[s])
				}(sym)
			}
			wg.Wait()
		}
	})

	b.Run("WorkerPool", func(b *testing.B) {
		pool := NewWorkerPool(4)
		pool.Start()
		defer pool.Stop()

		for i := 0; i < b.N; i++ {
			var wg sync.WaitGroup
			for _, sym := range symbols {
				wg.Add(1)
				s := sym
				pool.Submit(func() {
					defer wg.Done()
					scorer.Score(ctx, candlesMap[s])
				})
			}
			wg.Wait()
		}
	})
}

// generateTestCandles generates test candle data.
func generateTestCandles(count int) []models.Candle {
	candles := make([]models.Candle, count)
	basePrice := 1000.0
	baseTime := time.Now().Add(-time.Duration(count) * time.Minute)

	for i := 0; i < count; i++ {
		// Generate realistic price movement
		change := (float64(i%20) - 10) * 0.5
		open := basePrice + change
		high := open + float64(i%5)*0.5
		low := open - float64(i%5)*0.5
		close := open + (float64(i%10)-5)*0.3

		candles[i] = models.Candle{
			Timestamp: baseTime.Add(time.Duration(i) * time.Minute),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    int64(10000 + i*100),
		}

		basePrice = close
	}

	return candles
}

// TestWorkerPoolFunctionality tests worker pool basic functionality.
func TestWorkerPoolFunctionality(t *testing.T) {
	pool := NewWorkerPool(4)
	pool.Start()

	// Test basic task submission
	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		submitted := pool.Submit(func() {
			atomic.AddInt64(&counter, 1)
			wg.Done()
		})
		if !submitted {
			wg.Done() // Decrement if not submitted
		}
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for tasks to complete")
	}

	pool.Stop()

	if counter < 90 { // Allow some tolerance
		t.Errorf("Expected at least 90 tasks completed, got %d", counter)
	}

	stats := pool.Stats()
	t.Logf("Pool stats: TasksTotal=%d, TasksDone=%d", stats.TasksTotal, stats.TasksDone)
}

// TestRateLimiterFunctionality tests rate limiter basic functionality.
func TestRateLimiterFunctionality(t *testing.T) {
	limiter := NewRateLimiter(100, 10) // 100 requests/sec, burst of 10

	// Should allow burst
	allowed := 0
	for i := 0; i < 15; i++ {
		if limiter.Allow() {
			allowed++
		}
	}

	if allowed < 10 {
		t.Errorf("Expected at least 10 allowed in burst, got %d", allowed)
	}

	// Wait for refill
	time.Sleep(100 * time.Millisecond)

	// Should allow more
	if !limiter.Allow() {
		t.Error("Expected to allow after refill")
	}
}

// TestBatchProcessorFunctionality tests batch processor basic functionality.
func TestBatchProcessorFunctionality(t *testing.T) {
	var batches [][]int

	processor := NewBatchProcessor(5, func(items []int) error {
		batch := make([]int, len(items))
		copy(batch, items)
		batches = append(batches, batch)
		return nil
	})

	// Add 12 items (should create 2 full batches + 2 remaining)
	for i := 0; i < 12; i++ {
		processor.Add(i)
	}
	processor.Flush()

	if len(batches) != 3 {
		t.Errorf("Expected 3 batches, got %d", len(batches))
	}

	if len(batches[0]) != 5 || len(batches[1]) != 5 || len(batches[2]) != 2 {
		t.Error("Batch sizes incorrect")
	}
}

// TestMemoryStats tests memory stats retrieval.
func TestMemoryStats(t *testing.T) {
	stats := MemoryStats()

	if stats.Alloc == 0 {
		t.Error("Expected non-zero Alloc")
	}

	if stats.Goroutines == 0 {
		t.Error("Expected non-zero Goroutines")
	}

	t.Logf("Memory Stats: Alloc=%d, HeapAlloc=%d, Goroutines=%d",
		stats.Alloc, stats.HeapAlloc, stats.Goroutines)
}
