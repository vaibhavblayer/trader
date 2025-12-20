// Package trading provides trading operations and utilities.
package trading

import (
	"context"
	"fmt"
	"sync"
	"time"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

// CircuitBand represents circuit limit bands.
type CircuitBand int

const (
	CircuitBand2  CircuitBand = 2
	CircuitBand5  CircuitBand = 5
	CircuitBand10 CircuitBand = 10
	CircuitBand20 CircuitBand = 20
)

// CircuitStatus represents the circuit status of a stock.
type CircuitStatus string

const (
	CircuitNone       CircuitStatus = "NONE"
	CircuitUpperHit   CircuitStatus = "UPPER_CIRCUIT"
	CircuitLowerHit   CircuitStatus = "LOWER_CIRCUIT"
	CircuitNearUpper  CircuitStatus = "NEAR_UPPER"
	CircuitNearLower  CircuitStatus = "NEAR_LOWER"
)

// CircuitLimit represents circuit limits for a stock.
type CircuitLimit struct {
	Symbol       string
	Exchange     models.Exchange
	Band         CircuitBand
	UpperLimit   float64
	LowerLimit   float64
	BasePrice    float64
	LTP          float64
	Status       CircuitStatus
	DistToUpper  float64 // Percentage distance to upper circuit
	DistToLower  float64 // Percentage distance to lower circuit
	LastUpdated  time.Time
}

// CircuitMonitor monitors circuit limits for Indian markets.
type CircuitMonitor struct {
	broker       broker.Broker
	limits       map[string]*CircuitLimit // symbol -> limits
	mu           sync.RWMutex
	nearThreshold float64 // Percentage threshold for "near circuit" alerts
}

// NewCircuitMonitor creates a new circuit monitor.
func NewCircuitMonitor(b broker.Broker) *CircuitMonitor {
	return &CircuitMonitor{
		broker:        b,
		limits:        make(map[string]*CircuitLimit),
		nearThreshold: 2.0, // Alert when within 2% of circuit
	}
}

// SetNearThreshold sets the threshold for "near circuit" alerts.
func (m *CircuitMonitor) SetNearThreshold(threshold float64) {
	m.nearThreshold = threshold
}

// FetchCircuitLimits fetches circuit limits for a symbol.
func (m *CircuitMonitor) FetchCircuitLimits(ctx context.Context, symbol string, exchange models.Exchange) (*CircuitLimit, error) {
	quote, err := m.broker.GetQuote(ctx, fmt.Sprintf("%s:%s", exchange, symbol))
	if err != nil {
		return nil, fmt.Errorf("failed to get quote: %w", err)
	}

	// Calculate circuit limits based on previous close
	// Default to 20% band for most stocks
	band := CircuitBand20
	basePrice := quote.Close
	if basePrice == 0 {
		basePrice = quote.LTP
	}

	upperLimit := basePrice * (1 + float64(band)/100)
	lowerLimit := basePrice * (1 - float64(band)/100)

	// Calculate distances
	distToUpper := 0.0
	distToLower := 0.0
	if quote.LTP > 0 {
		distToUpper = ((upperLimit - quote.LTP) / quote.LTP) * 100
		distToLower = ((quote.LTP - lowerLimit) / quote.LTP) * 100
	}

	// Determine status
	status := CircuitNone
	if quote.LTP >= upperLimit {
		status = CircuitUpperHit
	} else if quote.LTP <= lowerLimit {
		status = CircuitLowerHit
	} else if distToUpper <= m.nearThreshold {
		status = CircuitNearUpper
	} else if distToLower <= m.nearThreshold {
		status = CircuitNearLower
	}

	limit := &CircuitLimit{
		Symbol:      symbol,
		Exchange:    exchange,
		Band:        band,
		UpperLimit:  upperLimit,
		LowerLimit:  lowerLimit,
		BasePrice:   basePrice,
		LTP:         quote.LTP,
		Status:      status,
		DistToUpper: distToUpper,
		DistToLower: distToLower,
		LastUpdated: time.Now(),
	}

	// Cache the limit
	m.mu.Lock()
	key := fmt.Sprintf("%s:%s", exchange, symbol)
	m.limits[key] = limit
	m.mu.Unlock()

	return limit, nil
}

// SetCircuitBand sets the circuit band for a symbol.
func (m *CircuitMonitor) SetCircuitBand(symbol string, exchange models.Exchange, band CircuitBand) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s:%s", exchange, symbol)
	if limit, ok := m.limits[key]; ok {
		limit.Band = band
		// Recalculate limits
		limit.UpperLimit = limit.BasePrice * (1 + float64(band)/100)
		limit.LowerLimit = limit.BasePrice * (1 - float64(band)/100)
	}
}

// GetCircuitLimit returns cached circuit limit for a symbol.
func (m *CircuitMonitor) GetCircuitLimit(symbol string, exchange models.Exchange) *CircuitLimit {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", exchange, symbol)
	return m.limits[key]
}

// IsCircuitLocked checks if a stock is circuit locked.
func (m *CircuitMonitor) IsCircuitLocked(ctx context.Context, symbol string, exchange models.Exchange) (bool, CircuitStatus, error) {
	limit, err := m.FetchCircuitLimits(ctx, symbol, exchange)
	if err != nil {
		return false, CircuitNone, err
	}

	locked := limit.Status == CircuitUpperHit || limit.Status == CircuitLowerHit
	return locked, limit.Status, nil
}

// ValidateOrderPrice validates if an order price is within circuit limits.
func (m *CircuitMonitor) ValidateOrderPrice(ctx context.Context, symbol string, exchange models.Exchange, price float64) (bool, string, error) {
	limit, err := m.FetchCircuitLimits(ctx, symbol, exchange)
	if err != nil {
		return false, "", err
	}

	if price > limit.UpperLimit {
		return false, fmt.Sprintf("Price %.2f exceeds upper circuit limit %.2f", price, limit.UpperLimit), nil
	}
	if price < limit.LowerLimit {
		return false, fmt.Sprintf("Price %.2f below lower circuit limit %.2f", price, limit.LowerLimit), nil
	}

	return true, "", nil
}

// GetCircuitLockedStocks returns list of circuit-locked stocks from a list.
func (m *CircuitMonitor) GetCircuitLockedStocks(ctx context.Context, symbols []string, exchange models.Exchange) ([]CircuitLimit, error) {
	var locked []CircuitLimit

	for _, symbol := range symbols {
		limit, err := m.FetchCircuitLimits(ctx, symbol, exchange)
		if err != nil {
			continue // Skip on error
		}

		if limit.Status == CircuitUpperHit || limit.Status == CircuitLowerHit {
			locked = append(locked, *limit)
		}
	}

	return locked, nil
}

// FilterNonCircuitStocks filters out circuit-locked stocks from a list.
func (m *CircuitMonitor) FilterNonCircuitStocks(ctx context.Context, symbols []string, exchange models.Exchange) ([]string, error) {
	var filtered []string

	for _, symbol := range symbols {
		limit, err := m.FetchCircuitLimits(ctx, symbol, exchange)
		if err != nil {
			continue // Skip on error
		}

		if limit.Status != CircuitUpperHit && limit.Status != CircuitLowerHit {
			filtered = append(filtered, symbol)
		}
	}

	return filtered, nil
}

// GetNearCircuitStocks returns stocks approaching circuit limits.
func (m *CircuitMonitor) GetNearCircuitStocks(ctx context.Context, symbols []string, exchange models.Exchange) ([]CircuitLimit, error) {
	var nearCircuit []CircuitLimit

	for _, symbol := range symbols {
		limit, err := m.FetchCircuitLimits(ctx, symbol, exchange)
		if err != nil {
			continue
		}

		if limit.Status == CircuitNearUpper || limit.Status == CircuitNearLower {
			nearCircuit = append(nearCircuit, *limit)
		}
	}

	return nearCircuit, nil
}

// UpdateLTP updates the LTP and recalculates circuit status.
func (m *CircuitMonitor) UpdateLTP(symbol string, exchange models.Exchange, ltp float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := fmt.Sprintf("%s:%s", exchange, symbol)
	limit, ok := m.limits[key]
	if !ok {
		return
	}

	limit.LTP = ltp
	limit.LastUpdated = time.Now()

	// Recalculate distances
	if ltp > 0 {
		limit.DistToUpper = ((limit.UpperLimit - ltp) / ltp) * 100
		limit.DistToLower = ((ltp - limit.LowerLimit) / ltp) * 100
	}

	// Update status
	if ltp >= limit.UpperLimit {
		limit.Status = CircuitUpperHit
	} else if ltp <= limit.LowerLimit {
		limit.Status = CircuitLowerHit
	} else if limit.DistToUpper <= m.nearThreshold {
		limit.Status = CircuitNearUpper
	} else if limit.DistToLower <= m.nearThreshold {
		limit.Status = CircuitNearLower
	} else {
		limit.Status = CircuitNone
	}
}

// GetAllCircuitBands returns available circuit bands.
func GetAllCircuitBands() []CircuitBand {
	return []CircuitBand{CircuitBand2, CircuitBand5, CircuitBand10, CircuitBand20}
}

// CircuitBandString returns string representation of circuit band.
func (b CircuitBand) String() string {
	return fmt.Sprintf("%d%%", b)
}
