// Package indicators provides technical indicator calculations with parallel processing.
package indicators

import (
	"context"
	"fmt"
	"sync"

	"zerodha-trader/internal/models"
)

// Indicator defines the interface for single-value technical indicators.
type Indicator interface {
	Name() string
	Calculate(candles []models.Candle) ([]float64, error)
	Period() int
}

// MultiValueIndicator defines the interface for indicators that return multiple values.
type MultiValueIndicator interface {
	Name() string
	Calculate(candles []models.Candle) (map[string][]float64, error)
	Period() int
}

// IndicatorResult holds the result of an indicator calculation.
type IndicatorResult struct {
	Name   string
	Values []float64
	Error  error
}

// MultiIndicatorResult holds the result of a multi-value indicator calculation.
type MultiIndicatorResult struct {
	Name   string
	Values map[string][]float64
	Error  error
}

// Engine provides parallel indicator calculation using a worker pool.
type Engine struct {
	workers     int
	indicators  map[string]Indicator
	multiIndics map[string]MultiValueIndicator
	mu          sync.RWMutex
}

// NewEngine creates a new indicator engine with the specified number of workers.
func NewEngine(workers int) *Engine {
	if workers <= 0 {
		workers = 4
	}
	return &Engine{
		workers:     workers,
		indicators:  make(map[string]Indicator),
		multiIndics: make(map[string]MultiValueIndicator),
	}
}

// RegisterIndicator registers a single-value indicator.
func (e *Engine) RegisterIndicator(ind Indicator) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.indicators[ind.Name()] = ind
}

// RegisterMultiIndicator registers a multi-value indicator.
func (e *Engine) RegisterMultiIndicator(ind MultiValueIndicator) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.multiIndics[ind.Name()] = ind
}


// CalculateAll calculates all registered indicators in parallel.
func (e *Engine) CalculateAll(ctx context.Context, candles []models.Candle) (map[string][]float64, map[string]map[string][]float64, error) {
	e.mu.RLock()
	indicators := make([]Indicator, 0, len(e.indicators))
	for _, ind := range e.indicators {
		indicators = append(indicators, ind)
	}
	multiIndics := make([]MultiValueIndicator, 0, len(e.multiIndics))
	for _, ind := range e.multiIndics {
		multiIndics = append(multiIndics, ind)
	}
	e.mu.RUnlock()

	singleResults := make(map[string][]float64)
	multiResults := make(map[string]map[string][]float64)
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Create work channels
	singleWork := make(chan Indicator, len(indicators))
	multiWork := make(chan MultiValueIndicator, len(multiIndics))

	// Start workers for single-value indicators
	for i := 0; i < e.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ind := range singleWork {
				select {
				case <-ctx.Done():
					return
				default:
					values, err := ind.Calculate(candles)
					if err == nil {
						mu.Lock()
						singleResults[ind.Name()] = values
						mu.Unlock()
					}
				}
			}
		}()
	}

	// Start workers for multi-value indicators
	for i := 0; i < e.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ind := range multiWork {
				select {
				case <-ctx.Done():
					return
				default:
					values, err := ind.Calculate(candles)
					if err == nil {
						mu.Lock()
						multiResults[ind.Name()] = values
						mu.Unlock()
					}
				}
			}
		}()
	}

	// Send work
	for _, ind := range indicators {
		singleWork <- ind
	}
	close(singleWork)

	for _, ind := range multiIndics {
		multiWork <- ind
	}
	close(multiWork)

	wg.Wait()

	return singleResults, multiResults, nil
}

// Calculate calculates a specific indicator by name.
func (e *Engine) Calculate(ctx context.Context, name string, candles []models.Candle) ([]float64, error) {
	e.mu.RLock()
	ind, ok := e.indicators[name]
	e.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("indicator %s not found", name)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return ind.Calculate(candles)
	}
}

// CalculateMulti calculates a specific multi-value indicator by name.
func (e *Engine) CalculateMulti(ctx context.Context, name string, candles []models.Candle) (map[string][]float64, error) {
	e.mu.RLock()
	ind, ok := e.multiIndics[name]
	e.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("multi-value indicator %s not found", name)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return ind.Calculate(candles)
	}
}

// CalculateSelected calculates only the specified indicators in parallel.
func (e *Engine) CalculateSelected(ctx context.Context, candles []models.Candle, names []string) (map[string][]float64, error) {
	e.mu.RLock()
	indicators := make([]Indicator, 0, len(names))
	for _, name := range names {
		if ind, ok := e.indicators[name]; ok {
			indicators = append(indicators, ind)
		}
	}
	e.mu.RUnlock()

	results := make(map[string][]float64)
	var mu sync.Mutex
	var wg sync.WaitGroup

	work := make(chan Indicator, len(indicators))

	for i := 0; i < e.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ind := range work {
				select {
				case <-ctx.Done():
					return
				default:
					values, err := ind.Calculate(candles)
					if err == nil {
						mu.Lock()
						results[ind.Name()] = values
						mu.Unlock()
					}
				}
			}
		}()
	}

	for _, ind := range indicators {
		work <- ind
	}
	close(work)

	wg.Wait()

	return results, nil
}

// ListIndicators returns the names of all registered single-value indicators.
func (e *Engine) ListIndicators() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	names := make([]string, 0, len(e.indicators))
	for name := range e.indicators {
		names = append(names, name)
	}
	return names
}

// ListMultiIndicators returns the names of all registered multi-value indicators.
func (e *Engine) ListMultiIndicators() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	names := make([]string, 0, len(e.multiIndics))
	for name := range e.multiIndics {
		names = append(names, name)
	}
	return names
}
