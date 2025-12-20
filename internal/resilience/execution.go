package resilience

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ExecutionQuality tracks order execution quality metrics.
type ExecutionQuality struct {
	OrderID       string
	Symbol        string
	ExpectedPrice float64
	ActualPrice   float64
	Slippage      float64
	SlippagePct   float64
	LatencyMs     int64
	Timestamp     time.Time
	OrderType     string
	Side          string
	Rejected      bool
	RejectReason  string
}

// ExecutionQualityTracker tracks and analyzes execution quality.
type ExecutionQualityTracker struct {
	mu sync.RWMutex

	// Configuration
	slippageAlertThreshold float64 // Alert when slippage exceeds this percentage
	latencyAlertThreshold  int64   // Alert when latency exceeds this (ms)

	// Metrics
	executions       []ExecutionQuality
	totalExecutions  int64
	totalRejections  int64
	totalSlippage    float64
	totalLatency     int64
	maxSlippage      float64
	maxLatency       int64

	// Rolling window for recent metrics
	windowSize    int
	recentMetrics []ExecutionQuality

	// Alert callback
	onAlert func(alert ExecutionAlert)
}

// ExecutionTrackerConfig holds configuration for execution tracking.
type ExecutionTrackerConfig struct {
	SlippageAlertThreshold float64 // Percentage
	LatencyAlertThreshold  int64   // Milliseconds
	WindowSize             int     // Number of recent executions to track
	MaxStoredExecutions    int     // Maximum executions to store
}

// DefaultExecutionTrackerConfig returns default configuration.
func DefaultExecutionTrackerConfig() ExecutionTrackerConfig {
	return ExecutionTrackerConfig{
		SlippageAlertThreshold: 0.5,  // 0.5%
		LatencyAlertThreshold:  1000, // 1 second
		WindowSize:             100,
		MaxStoredExecutions:    1000,
	}
}

// NewExecutionQualityTracker creates a new execution quality tracker.
func NewExecutionQualityTracker(config ExecutionTrackerConfig) *ExecutionQualityTracker {
	return &ExecutionQualityTracker{
		slippageAlertThreshold: config.SlippageAlertThreshold,
		latencyAlertThreshold:  config.LatencyAlertThreshold,
		windowSize:             config.WindowSize,
		executions:             make([]ExecutionQuality, 0, config.MaxStoredExecutions),
		recentMetrics:          make([]ExecutionQuality, 0, config.WindowSize),
	}
}

// SetAlertCallback sets the callback for execution alerts.
func (t *ExecutionQualityTracker) SetAlertCallback(callback func(ExecutionAlert)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onAlert = callback
}

// RecordExecution records an order execution.
func (t *ExecutionQualityTracker) RecordExecution(exec ExecutionQuality) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Calculate slippage
	if exec.ExpectedPrice > 0 {
		exec.Slippage = exec.ActualPrice - exec.ExpectedPrice
		exec.SlippagePct = (exec.Slippage / exec.ExpectedPrice) * 100
	}

	exec.Timestamp = time.Now()

	// Update metrics
	t.totalExecutions++
	t.totalSlippage += exec.SlippagePct
	t.totalLatency += exec.LatencyMs

	if exec.SlippagePct > t.maxSlippage {
		t.maxSlippage = exec.SlippagePct
	}
	if exec.LatencyMs > t.maxLatency {
		t.maxLatency = exec.LatencyMs
	}

	// Store execution
	t.executions = append(t.executions, exec)

	// Update rolling window
	t.recentMetrics = append(t.recentMetrics, exec)
	if len(t.recentMetrics) > t.windowSize {
		t.recentMetrics = t.recentMetrics[1:]
	}

	// Check for alerts
	t.checkAlerts(exec)
}

// RecordRejection records an order rejection.
func (t *ExecutionQualityTracker) RecordRejection(orderID, symbol, reason string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.totalRejections++

	exec := ExecutionQuality{
		OrderID:      orderID,
		Symbol:       symbol,
		Rejected:     true,
		RejectReason: reason,
		Timestamp:    time.Now(),
	}

	t.executions = append(t.executions, exec)

	// Alert on rejection
	if t.onAlert != nil {
		t.onAlert(ExecutionAlert{
			Type:      AlertOrderRejected,
			OrderID:   orderID,
			Symbol:    symbol,
			Message:   fmt.Sprintf("Order rejected: %s", reason),
			Timestamp: time.Now(),
		})
	}
}

func (t *ExecutionQualityTracker) checkAlerts(exec ExecutionQuality) {
	if t.onAlert == nil {
		return
	}

	// Check slippage threshold
	if exec.SlippagePct > t.slippageAlertThreshold || exec.SlippagePct < -t.slippageAlertThreshold {
		t.onAlert(ExecutionAlert{
			Type:      AlertHighSlippage,
			OrderID:   exec.OrderID,
			Symbol:    exec.Symbol,
			Value:     exec.SlippagePct,
			Threshold: t.slippageAlertThreshold,
			Message:   fmt.Sprintf("High slippage: %.2f%% (threshold: %.2f%%)", exec.SlippagePct, t.slippageAlertThreshold),
			Timestamp: time.Now(),
		})
	}

	// Check latency threshold
	if exec.LatencyMs > t.latencyAlertThreshold {
		t.onAlert(ExecutionAlert{
			Type:      AlertHighLatency,
			OrderID:   exec.OrderID,
			Symbol:    exec.Symbol,
			Value:     float64(exec.LatencyMs),
			Threshold: float64(t.latencyAlertThreshold),
			Message:   fmt.Sprintf("High latency: %dms (threshold: %dms)", exec.LatencyMs, t.latencyAlertThreshold),
			Timestamp: time.Now(),
		})
	}
}

// GetStats returns execution quality statistics.
func (t *ExecutionQualityTracker) GetStats() ExecutionStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := ExecutionStats{
		TotalExecutions: t.totalExecutions,
		TotalRejections: t.totalRejections,
		MaxSlippage:     t.maxSlippage,
		MaxLatency:      t.maxLatency,
	}

	if t.totalExecutions > 0 {
		stats.AvgSlippage = t.totalSlippage / float64(t.totalExecutions)
		stats.AvgLatency = t.totalLatency / t.totalExecutions
		stats.RejectionRate = float64(t.totalRejections) / float64(t.totalExecutions+t.totalRejections) * 100
	}

	// Calculate recent metrics
	if len(t.recentMetrics) > 0 {
		var recentSlippage float64
		var recentLatency int64
		for _, m := range t.recentMetrics {
			recentSlippage += m.SlippagePct
			recentLatency += m.LatencyMs
		}
		stats.RecentAvgSlippage = recentSlippage / float64(len(t.recentMetrics))
		stats.RecentAvgLatency = recentLatency / int64(len(t.recentMetrics))
	}

	return stats
}

// GetRecentExecutions returns recent executions.
func (t *ExecutionQualityTracker) GetRecentExecutions(limit int) []ExecutionQuality {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if limit <= 0 || limit > len(t.recentMetrics) {
		limit = len(t.recentMetrics)
	}

	result := make([]ExecutionQuality, limit)
	copy(result, t.recentMetrics[len(t.recentMetrics)-limit:])
	return result
}

// GetExecutionsBySymbol returns executions for a specific symbol.
func (t *ExecutionQualityTracker) GetExecutionsBySymbol(symbol string) []ExecutionQuality {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []ExecutionQuality
	for _, exec := range t.executions {
		if exec.Symbol == symbol {
			result = append(result, exec)
		}
	}
	return result
}

// GenerateReport generates an execution quality report.
func (t *ExecutionQualityTracker) GenerateReport(ctx context.Context) *ExecutionReport {
	t.mu.RLock()
	defer t.mu.RUnlock()

	report := &ExecutionReport{
		GeneratedAt: time.Now(),
		Stats:       t.GetStats(),
	}

	// Group by symbol
	symbolStats := make(map[string]*SymbolExecutionStats)
	for _, exec := range t.executions {
		if exec.Rejected {
			continue
		}

		stats, ok := symbolStats[exec.Symbol]
		if !ok {
			stats = &SymbolExecutionStats{Symbol: exec.Symbol}
			symbolStats[exec.Symbol] = stats
		}

		stats.Count++
		stats.TotalSlippage += exec.SlippagePct
		stats.TotalLatency += exec.LatencyMs
		if exec.SlippagePct > stats.MaxSlippage {
			stats.MaxSlippage = exec.SlippagePct
		}
	}

	// Calculate averages
	for _, stats := range symbolStats {
		if stats.Count > 0 {
			stats.AvgSlippage = stats.TotalSlippage / float64(stats.Count)
			stats.AvgLatency = stats.TotalLatency / int64(stats.Count)
		}
		report.BySymbol = append(report.BySymbol, *stats)
	}

	// Identify worst performers
	for _, exec := range t.executions {
		if exec.SlippagePct > t.slippageAlertThreshold {
			report.HighSlippageOrders = append(report.HighSlippageOrders, exec)
		}
	}

	return report
}

// ExecutionStats holds execution quality statistics.
type ExecutionStats struct {
	TotalExecutions   int64
	TotalRejections   int64
	AvgSlippage       float64
	MaxSlippage       float64
	AvgLatency        int64
	MaxLatency        int64
	RejectionRate     float64
	RecentAvgSlippage float64
	RecentAvgLatency  int64
}

// ExecutionReport holds a comprehensive execution quality report.
type ExecutionReport struct {
	GeneratedAt        time.Time
	Stats              ExecutionStats
	BySymbol           []SymbolExecutionStats
	HighSlippageOrders []ExecutionQuality
}

// SymbolExecutionStats holds execution stats for a symbol.
type SymbolExecutionStats struct {
	Symbol        string
	Count         int64
	TotalSlippage float64
	AvgSlippage   float64
	MaxSlippage   float64
	TotalLatency  int64
	AvgLatency    int64
}

// ExecutionAlertType represents the type of execution alert.
type ExecutionAlertType string

const (
	AlertHighSlippage  ExecutionAlertType = "HIGH_SLIPPAGE"
	AlertHighLatency   ExecutionAlertType = "HIGH_LATENCY"
	AlertOrderRejected ExecutionAlertType = "ORDER_REJECTED"
)

// ExecutionAlert represents an execution quality alert.
type ExecutionAlert struct {
	Type      ExecutionAlertType
	OrderID   string
	Symbol    string
	Value     float64
	Threshold float64
	Message   string
	Timestamp time.Time
}

// DefaultExecutionTracker is a global instance.
var DefaultExecutionTracker = NewExecutionQualityTracker(DefaultExecutionTrackerConfig())
