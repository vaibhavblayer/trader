// Package performance provides performance optimization utilities and monitoring.
// Requirements: 18, 24 (Concurrent Data Processing, Asynchronous Operations)
package performance

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// WorkerPool manages a pool of workers for concurrent task execution.
// This optimizes concurrent operations by reusing goroutines.
type WorkerPool struct {
	workers    int
	taskQueue  chan func()
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	running    atomic.Bool
	tasksTotal atomic.Uint64
	tasksDone  atomic.Uint64
}

// NewWorkerPool creates a new worker pool with the specified number of workers.
// If workers is 0, it defaults to runtime.NumCPU().
func NewWorkerPool(workers int) *WorkerPool {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	ctx, cancel := context.WithCancel(context.Background())
	pool := &WorkerPool{
		workers:   workers,
		taskQueue: make(chan func(), workers*100), // Buffer for 100x workers
		ctx:       ctx,
		cancel:    cancel,
	}

	return pool
}

// Start starts the worker pool.
func (p *WorkerPool) Start() {
	if p.running.Swap(true) {
		return // Already running
	}

	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
}

// worker is the main worker loop.
func (p *WorkerPool) worker() {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case task, ok := <-p.taskQueue:
			if !ok {
				return
			}
			task()
			p.tasksDone.Add(1)
		}
	}
}

// Submit submits a task to the worker pool.
// Returns false if the pool is not running or the queue is full.
func (p *WorkerPool) Submit(task func()) bool {
	if !p.running.Load() {
		return false
	}

	select {
	case p.taskQueue <- task:
		p.tasksTotal.Add(1)
		return true
	default:
		return false // Queue full
	}
}

// SubmitWait submits a task and waits for it to complete.
func (p *WorkerPool) SubmitWait(task func()) bool {
	done := make(chan struct{})
	wrapped := func() {
		task()
		close(done)
	}

	if !p.Submit(wrapped) {
		return false
	}

	<-done
	return true
}

// Stop stops the worker pool and waits for all workers to finish.
func (p *WorkerPool) Stop() {
	if !p.running.Swap(false) {
		return // Not running
	}

	p.cancel()
	close(p.taskQueue)
	p.wg.Wait()
}

// Stats returns pool statistics.
func (p *WorkerPool) Stats() PoolStats {
	return PoolStats{
		Workers:    p.workers,
		Running:    p.running.Load(),
		TasksTotal: p.tasksTotal.Load(),
		TasksDone:  p.tasksDone.Load(),
		QueueLen:   len(p.taskQueue),
	}
}

// PoolStats contains worker pool statistics.
type PoolStats struct {
	Workers    int
	Running    bool
	TasksTotal uint64
	TasksDone  uint64
	QueueLen   int
}

// BatchProcessor processes items in batches for improved efficiency.
type BatchProcessor[T any] struct {
	batchSize int
	processor func([]T) error
	items     []T
	mu        sync.Mutex
}

// NewBatchProcessor creates a new batch processor.
func NewBatchProcessor[T any](batchSize int, processor func([]T) error) *BatchProcessor[T] {
	return &BatchProcessor[T]{
		batchSize: batchSize,
		processor: processor,
		items:     make([]T, 0, batchSize),
	}
}

// Add adds an item to the batch. If the batch is full, it's processed.
func (b *BatchProcessor[T]) Add(item T) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.items = append(b.items, item)
	if len(b.items) >= b.batchSize {
		return b.flush()
	}
	return nil
}

// Flush processes any remaining items in the batch.
func (b *BatchProcessor[T]) Flush() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.flush()
}

func (b *BatchProcessor[T]) flush() error {
	if len(b.items) == 0 {
		return nil
	}

	err := b.processor(b.items)
	b.items = b.items[:0] // Reset slice but keep capacity
	return err
}

// RateLimiter implements a token bucket rate limiter.
type RateLimiter struct {
	rate       float64 // tokens per second
	burst      int     // max tokens
	tokens     float64
	lastUpdate time.Time
	mu         sync.Mutex
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(rate float64, burst int) *RateLimiter {
	return &RateLimiter{
		rate:       rate,
		burst:      burst,
		tokens:     float64(burst),
		lastUpdate: time.Now(),
	}
}

// Allow checks if a request is allowed under the rate limit.
func (r *RateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.lastUpdate).Seconds()
	r.lastUpdate = now

	// Add tokens based on elapsed time
	r.tokens += elapsed * r.rate
	if r.tokens > float64(r.burst) {
		r.tokens = float64(r.burst)
	}

	if r.tokens >= 1 {
		r.tokens--
		return true
	}
	return false
}

// Wait waits until a request is allowed.
func (r *RateLimiter) Wait(ctx context.Context) error {
	for {
		if r.Allow() {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond * 10):
			// Try again
		}
	}
}

// ObjectPool provides a pool of reusable objects to reduce allocations.
type ObjectPool[T any] struct {
	pool sync.Pool
}

// NewObjectPool creates a new object pool with the given factory function.
func NewObjectPool[T any](factory func() T) *ObjectPool[T] {
	return &ObjectPool[T]{
		pool: sync.Pool{
			New: func() interface{} {
				return factory()
			},
		},
	}
}

// Get retrieves an object from the pool.
func (p *ObjectPool[T]) Get() T {
	return p.pool.Get().(T)
}

// Put returns an object to the pool.
func (p *ObjectPool[T]) Put(obj T) {
	p.pool.Put(obj)
}

// MemoryStats returns current memory statistics.
func MemoryStats() MemStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return MemStats{
		Alloc:        m.Alloc,
		TotalAlloc:   m.TotalAlloc,
		Sys:          m.Sys,
		NumGC:        m.NumGC,
		HeapAlloc:    m.HeapAlloc,
		HeapSys:      m.HeapSys,
		HeapIdle:     m.HeapIdle,
		HeapInuse:    m.HeapInuse,
		HeapReleased: m.HeapReleased,
		HeapObjects:  m.HeapObjects,
		Goroutines:   runtime.NumGoroutine(),
	}
}

// MemStats contains memory statistics.
type MemStats struct {
	Alloc        uint64 // bytes allocated and still in use
	TotalAlloc   uint64 // bytes allocated (even if freed)
	Sys          uint64 // bytes obtained from system
	NumGC        uint32 // number of completed GC cycles
	HeapAlloc    uint64 // bytes allocated on heap
	HeapSys      uint64 // bytes obtained from system for heap
	HeapIdle     uint64 // bytes in idle spans
	HeapInuse    uint64 // bytes in non-idle spans
	HeapReleased uint64 // bytes released to OS
	HeapObjects  uint64 // number of allocated objects
	Goroutines   int    // number of goroutines
}

// FormatBytes formats bytes into human-readable format.
func FormatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return formatBytesUnit(bytes, "B")
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB"}
	return formatBytesUnit(bytes/div, units[exp])
}

func formatBytesUnit(value uint64, unit string) string {
	return string(rune('0'+value/100)) + string(rune('0'+(value/10)%10)) + string(rune('0'+value%10)) + " " + unit
}
