// Package resilience provides system resilience patterns including circuit breaker,
// health monitoring, and graceful degradation.
package resilience

import (
	"context"
	"errors"
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState string

const (
	CircuitClosed   CircuitState = "CLOSED"   // Normal operation
	CircuitOpen     CircuitState = "OPEN"     // Failing, rejecting requests
	CircuitHalfOpen CircuitState = "HALF_OPEN" // Testing if service recovered
)

// CircuitBreakerConfig holds circuit breaker configuration.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of failures before opening the circuit
	FailureThreshold int
	// SuccessThreshold is the number of successes in half-open state to close
	SuccessThreshold int
	// Timeout is how long to wait before transitioning from open to half-open
	Timeout time.Duration
	// MaxConcurrent is the maximum concurrent requests allowed (0 = unlimited)
	MaxConcurrent int
}

// DefaultCircuitBreakerConfig returns sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
		MaxConcurrent:    0,
	}
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	name   string
	config CircuitBreakerConfig

	mu              sync.RWMutex
	state           CircuitState
	failures        int
	successes       int
	lastFailureTime time.Time
	lastStateChange time.Time
	concurrent      int

	// Metrics
	totalRequests   int64
	totalFailures   int64
	totalSuccesses  int64
	totalRejected   int64
	totalTimeouts   int64
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(name string, config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		name:            name,
		config:          config,
		state:           CircuitClosed,
		lastStateChange: time.Now(),
	}
}

// ErrCircuitOpen is returned when the circuit is open.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// ErrTooManyConcurrent is returned when max concurrent requests exceeded.
var ErrTooManyConcurrent = errors.New("too many concurrent requests")

// Execute runs the given function with circuit breaker protection.
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	if err := cb.allowRequest(); err != nil {
		return err
	}

	cb.mu.Lock()
	cb.concurrent++
	cb.totalRequests++
	cb.mu.Unlock()

	defer func() {
		cb.mu.Lock()
		cb.concurrent--
		cb.mu.Unlock()
	}()

	// Execute with context
	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()

	select {
	case err := <-done:
		if err != nil {
			cb.recordFailure()
			return err
		}
		cb.recordSuccess()
		return nil
	case <-ctx.Done():
		cb.mu.Lock()
		cb.totalTimeouts++
		cb.mu.Unlock()
		cb.recordFailure()
		return ctx.Err()
	}
}

// ExecuteWithResult runs a function that returns a result with circuit breaker protection.
func ExecuteWithResult[T any](cb *CircuitBreaker, ctx context.Context, fn func() (T, error)) (T, error) {
	var zero T

	if err := cb.allowRequest(); err != nil {
		return zero, err
	}

	cb.mu.Lock()
	cb.concurrent++
	cb.totalRequests++
	cb.mu.Unlock()

	defer func() {
		cb.mu.Lock()
		cb.concurrent--
		cb.mu.Unlock()
	}()

	type result struct {
		value T
		err   error
	}

	done := make(chan result, 1)
	go func() {
		v, err := fn()
		done <- result{value: v, err: err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			cb.recordFailure()
			return zero, r.err
		}
		cb.recordSuccess()
		return r.value, nil
	case <-ctx.Done():
		cb.mu.Lock()
		cb.totalTimeouts++
		cb.mu.Unlock()
		cb.recordFailure()
		return zero, ctx.Err()
	}
}

func (cb *CircuitBreaker) allowRequest() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Check concurrent limit
	if cb.config.MaxConcurrent > 0 && cb.concurrent >= cb.config.MaxConcurrent {
		cb.totalRejected++
		return ErrTooManyConcurrent
	}

	switch cb.state {
	case CircuitClosed:
		return nil
	case CircuitOpen:
		// Check if timeout has passed
		if time.Since(cb.lastFailureTime) > cb.config.Timeout {
			cb.transitionTo(CircuitHalfOpen)
			return nil
		}
		cb.totalRejected++
		return ErrCircuitOpen
	case CircuitHalfOpen:
		return nil
	}

	return nil
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalSuccesses++

	switch cb.state {
	case CircuitHalfOpen:
		cb.successes++
		if cb.successes >= cb.config.SuccessThreshold {
			cb.transitionTo(CircuitClosed)
		}
	case CircuitClosed:
		// Reset failure count on success
		cb.failures = 0
	}
}

func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalFailures++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case CircuitClosed:
		cb.failures++
		if cb.failures >= cb.config.FailureThreshold {
			cb.transitionTo(CircuitOpen)
		}
	case CircuitHalfOpen:
		// Any failure in half-open goes back to open
		cb.transitionTo(CircuitOpen)
	}
}

func (cb *CircuitBreaker) transitionTo(state CircuitState) {
	cb.state = state
	cb.lastStateChange = time.Now()
	cb.failures = 0
	cb.successes = 0
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Name returns the circuit breaker name.
func (cb *CircuitBreaker) Name() string {
	return cb.name
}

// Stats returns circuit breaker statistics.
func (cb *CircuitBreaker) Stats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return CircuitBreakerStats{
		Name:            cb.name,
		State:           cb.state,
		TotalRequests:   cb.totalRequests,
		TotalSuccesses:  cb.totalSuccesses,
		TotalFailures:   cb.totalFailures,
		TotalRejected:   cb.totalRejected,
		TotalTimeouts:   cb.totalTimeouts,
		CurrentFailures: cb.failures,
		LastFailureTime: cb.lastFailureTime,
		LastStateChange: cb.lastStateChange,
		Concurrent:      cb.concurrent,
	}
}

// Reset resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = CircuitClosed
	cb.failures = 0
	cb.successes = 0
	cb.lastStateChange = time.Now()
}

// CircuitBreakerStats holds circuit breaker statistics.
type CircuitBreakerStats struct {
	Name            string
	State           CircuitState
	TotalRequests   int64
	TotalSuccesses  int64
	TotalFailures   int64
	TotalRejected   int64
	TotalTimeouts   int64
	CurrentFailures int
	LastFailureTime time.Time
	LastStateChange time.Time
	Concurrent      int
}

// FailureRate returns the failure rate as a percentage.
func (s CircuitBreakerStats) FailureRate() float64 {
	if s.TotalRequests == 0 {
		return 0
	}
	return float64(s.TotalFailures) / float64(s.TotalRequests) * 100
}
