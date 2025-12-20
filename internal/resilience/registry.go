package resilience

import (
	"context"
	"sync"
	"time"
)

// CircuitBreakerRegistry manages multiple circuit breakers.
type CircuitBreakerRegistry struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
	config   CircuitBreakerConfig
}

// NewCircuitBreakerRegistry creates a new registry with default config.
func NewCircuitBreakerRegistry(config CircuitBreakerConfig) *CircuitBreakerRegistry {
	return &CircuitBreakerRegistry{
		breakers: make(map[string]*CircuitBreaker),
		config:   config,
	}
}

// Get returns or creates a circuit breaker for the given name.
func (r *CircuitBreakerRegistry) Get(name string) *CircuitBreaker {
	r.mu.RLock()
	if cb, ok := r.breakers[name]; ok {
		r.mu.RUnlock()
		return cb
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if cb, ok := r.breakers[name]; ok {
		return cb
	}

	cb := NewCircuitBreaker(name, r.config)
	r.breakers[name] = cb
	return cb
}

// GetWithConfig returns or creates a circuit breaker with custom config.
func (r *CircuitBreakerRegistry) GetWithConfig(name string, config CircuitBreakerConfig) *CircuitBreaker {
	r.mu.Lock()
	defer r.mu.Unlock()

	if cb, ok := r.breakers[name]; ok {
		return cb
	}

	cb := NewCircuitBreaker(name, config)
	r.breakers[name] = cb
	return cb
}

// AllStats returns statistics for all circuit breakers.
func (r *CircuitBreakerRegistry) AllStats() []CircuitBreakerStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := make([]CircuitBreakerStats, 0, len(r.breakers))
	for _, cb := range r.breakers {
		stats = append(stats, cb.Stats())
	}
	return stats
}

// ResetAll resets all circuit breakers.
func (r *CircuitBreakerRegistry) ResetAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, cb := range r.breakers {
		cb.Reset()
	}
}

// RetryWithBackoff executes a function with exponential backoff and circuit breaker.
type RetryWithBackoff struct {
	MaxAttempts   int
	InitialDelay  time.Duration
	MaxDelay      time.Duration
	BackoffFactor float64
	Jitter        bool
}

// DefaultRetryWithBackoff returns default retry configuration.
func DefaultRetryWithBackoff() RetryWithBackoff {
	return RetryWithBackoff{
		MaxAttempts:   3,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      10 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        true,
	}
}

// Execute runs the function with retry and backoff.
func (r RetryWithBackoff) Execute(ctx context.Context, fn func() error) error {
	var lastErr error
	delay := r.InitialDelay

	for attempt := 0; attempt < r.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := fn(); err != nil {
			lastErr = err

			// Don't sleep after the last attempt
			if attempt < r.MaxAttempts-1 {
				sleepDuration := delay
				if r.Jitter {
					// Add up to 25% jitter
					jitter := time.Duration(float64(delay) * 0.25)
					sleepDuration = delay + jitter/2
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(sleepDuration):
				}

				delay = time.Duration(float64(delay) * r.BackoffFactor)
				if delay > r.MaxDelay {
					delay = r.MaxDelay
				}
			}
		} else {
			return nil
		}
	}

	return lastErr
}

// ExecuteWithResult runs a function that returns a result with retry and backoff.
func RetryWithBackoffResult[T any](ctx context.Context, r RetryWithBackoff, fn func() (T, error)) (T, error) {
	var zero T
	var lastErr error
	delay := r.InitialDelay

	for attempt := 0; attempt < r.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		default:
		}

		result, err := fn()
		if err != nil {
			lastErr = err

			if attempt < r.MaxAttempts-1 {
				sleepDuration := delay
				if r.Jitter {
					jitter := time.Duration(float64(delay) * 0.25)
					sleepDuration = delay + jitter/2
				}

				select {
				case <-ctx.Done():
					return zero, ctx.Err()
				case <-time.After(sleepDuration):
				}

				delay = time.Duration(float64(delay) * r.BackoffFactor)
				if delay > r.MaxDelay {
					delay = r.MaxDelay
				}
			}
		} else {
			return result, nil
		}
	}

	return zero, lastErr
}

// ExecuteWithCircuitBreaker combines retry with circuit breaker.
func (r RetryWithBackoff) ExecuteWithCircuitBreaker(ctx context.Context, cb *CircuitBreaker, fn func() error) error {
	return r.Execute(ctx, func() error {
		return cb.Execute(ctx, fn)
	})
}

// GracefulDegrader provides fallback functionality when services are unavailable.
type GracefulDegrader struct {
	mu        sync.RWMutex
	fallbacks map[string]interface{}
}

// NewGracefulDegrader creates a new graceful degrader.
func NewGracefulDegrader() *GracefulDegrader {
	return &GracefulDegrader{
		fallbacks: make(map[string]interface{}),
	}
}

// SetFallback sets a fallback value for a key.
func (g *GracefulDegrader) SetFallback(key string, value interface{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fallbacks[key] = value
}

// GetFallback returns the fallback value for a key.
func (g *GracefulDegrader) GetFallback(key string) (interface{}, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	v, ok := g.fallbacks[key]
	return v, ok
}

// ExecuteWithFallback executes a function and returns fallback on error.
func ExecuteWithFallback[T any](ctx context.Context, fn func() (T, error), fallback T) T {
	result, err := fn()
	if err != nil {
		return fallback
	}
	return result
}

// ServiceStatus represents the status of an external service.
type ServiceStatus struct {
	Name        string
	Available   bool
	LastCheck   time.Time
	LastSuccess time.Time
	LastError   error
	Latency     time.Duration
}

// ServiceMonitor monitors external service availability.
type ServiceMonitor struct {
	mu       sync.RWMutex
	services map[string]*ServiceStatus
}

// NewServiceMonitor creates a new service monitor.
func NewServiceMonitor() *ServiceMonitor {
	return &ServiceMonitor{
		services: make(map[string]*ServiceStatus),
	}
}

// UpdateStatus updates the status of a service.
func (m *ServiceMonitor) UpdateStatus(name string, available bool, latency time.Duration, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	status, ok := m.services[name]
	if !ok {
		status = &ServiceStatus{Name: name}
		m.services[name] = status
	}

	status.Available = available
	status.LastCheck = time.Now()
	status.Latency = latency
	status.LastError = err

	if available {
		status.LastSuccess = time.Now()
	}
}

// GetStatus returns the status of a service.
func (m *ServiceMonitor) GetStatus(name string) *ServiceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if status, ok := m.services[name]; ok {
		return status
	}
	return nil
}

// AllStatuses returns all service statuses.
func (m *ServiceMonitor) AllStatuses() []*ServiceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]*ServiceStatus, 0, len(m.services))
	for _, s := range m.services {
		statuses = append(statuses, s)
	}
	return statuses
}

// IsAvailable checks if a service is available.
func (m *ServiceMonitor) IsAvailable(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if status, ok := m.services[name]; ok {
		return status.Available
	}
	return false
}
