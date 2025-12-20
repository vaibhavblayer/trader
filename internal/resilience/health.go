package resilience

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"
)

// HealthStatus represents the health status of a component.
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "HEALTHY"
	HealthStatusDegraded  HealthStatus = "DEGRADED"
	HealthStatusUnhealthy HealthStatus = "UNHEALTHY"
	HealthStatusUnknown   HealthStatus = "UNKNOWN"
)

// ComponentHealth represents the health of a single component.
type ComponentHealth struct {
	Name        string
	Status      HealthStatus
	Message     string
	LastCheck   time.Time
	Latency     time.Duration
	Details     map[string]interface{}
}

// HealthCheck represents a health check function.
type HealthCheck func(ctx context.Context) ComponentHealth

// HealthMonitor monitors system health.
type HealthMonitor struct {
	mu sync.RWMutex

	// Configuration
	checkInterval     time.Duration
	memoryThreshold   uint64 // Bytes
	goroutineThreshold int

	// State
	startTime       time.Time
	components      map[string]HealthCheck
	componentHealth map[string]ComponentHealth
	overallStatus   HealthStatus

	// Heartbeat
	lastHeartbeat time.Time
	heartbeatChan chan struct{}

	// Alerts
	onAlert func(alert HealthAlert)

	// Metrics
	totalChecks    int64
	failedChecks   int64
	panicRecoveries int64

	// Context for shutdown
	ctx    context.Context
	cancel context.CancelFunc
}

// HealthMonitorConfig holds health monitor configuration.
type HealthMonitorConfig struct {
	CheckInterval      time.Duration
	MemoryThresholdMB  uint64
	GoroutineThreshold int
	HeartbeatInterval  time.Duration
}

// DefaultHealthMonitorConfig returns default configuration.
func DefaultHealthMonitorConfig() HealthMonitorConfig {
	return HealthMonitorConfig{
		CheckInterval:      30 * time.Second,
		MemoryThresholdMB:  500, // 500 MB
		GoroutineThreshold: 1000,
		HeartbeatInterval:  60 * time.Second,
	}
}

// NewHealthMonitor creates a new health monitor.
func NewHealthMonitor(config HealthMonitorConfig) *HealthMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	return &HealthMonitor{
		checkInterval:      config.CheckInterval,
		memoryThreshold:    config.MemoryThresholdMB * 1024 * 1024,
		goroutineThreshold: config.GoroutineThreshold,
		startTime:          time.Now(),
		components:         make(map[string]HealthCheck),
		componentHealth:    make(map[string]ComponentHealth),
		overallStatus:      HealthStatusUnknown,
		heartbeatChan:      make(chan struct{}),
		ctx:                ctx,
		cancel:             cancel,
	}
}

// RegisterComponent registers a health check for a component.
func (m *HealthMonitor) RegisterComponent(name string, check HealthCheck) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.components[name] = check
}

// SetAlertCallback sets the callback for health alerts.
func (m *HealthMonitor) SetAlertCallback(callback func(HealthAlert)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onAlert = callback
}

// Start starts the health monitoring loop.
func (m *HealthMonitor) Start() {
	go m.monitorLoop()
	go m.heartbeatLoop()
}

// Stop stops the health monitor.
func (m *HealthMonitor) Stop() {
	m.cancel()
	close(m.heartbeatChan)
}

func (m *HealthMonitor) monitorLoop() {
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	// Initial check
	m.runHealthChecks()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.runHealthChecks()
		}
	}
}

func (m *HealthMonitor) heartbeatLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			m.lastHeartbeat = time.Now()
			m.mu.Unlock()

			// Log heartbeat (would integrate with logging system)
			m.logHeartbeat()
		}
	}
}

func (m *HealthMonitor) runHealthChecks() {
	m.mu.Lock()
	components := make(map[string]HealthCheck)
	for k, v := range m.components {
		components[k] = v
	}
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	results := make(chan ComponentHealth, len(components)+2)

	// Run component checks
	for name, check := range components {
		wg.Add(1)
		go func(n string, c HealthCheck) {
			defer wg.Done()
			defer m.recoverPanic(n)

			start := time.Now()
			health := c(ctx)
			health.Name = n
			health.LastCheck = time.Now()
			health.Latency = time.Since(start)
			results <- health
		}(name, check)
	}

	// Add system checks
	wg.Add(1)
	go func() {
		defer wg.Done()
		results <- m.checkMemory()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		results <- m.checkGoroutines()
	}()

	wg.Wait()
	close(results)

	// Collect results
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalChecks++
	hasUnhealthy := false
	hasDegraded := false

	for health := range results {
		m.componentHealth[health.Name] = health

		switch health.Status {
		case HealthStatusUnhealthy:
			hasUnhealthy = true
			m.failedChecks++
			m.sendAlert(HealthAlert{
				Type:      AlertComponentUnhealthy,
				Component: health.Name,
				Status:    health.Status,
				Message:   health.Message,
				Timestamp: time.Now(),
			})
		case HealthStatusDegraded:
			hasDegraded = true
		}
	}

	// Update overall status
	if hasUnhealthy {
		m.overallStatus = HealthStatusUnhealthy
	} else if hasDegraded {
		m.overallStatus = HealthStatusDegraded
	} else {
		m.overallStatus = HealthStatusHealthy
	}
}

func (m *HealthMonitor) checkMemory() ComponentHealth {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	health := ComponentHealth{
		Name:      "memory",
		LastCheck: time.Now(),
		Details: map[string]interface{}{
			"alloc_mb":       memStats.Alloc / 1024 / 1024,
			"total_alloc_mb": memStats.TotalAlloc / 1024 / 1024,
			"sys_mb":         memStats.Sys / 1024 / 1024,
			"num_gc":         memStats.NumGC,
		},
	}

	if memStats.Alloc > m.memoryThreshold {
		health.Status = HealthStatusDegraded
		health.Message = fmt.Sprintf("Memory usage high: %d MB", memStats.Alloc/1024/1024)
	} else {
		health.Status = HealthStatusHealthy
		health.Message = fmt.Sprintf("Memory usage: %d MB", memStats.Alloc/1024/1024)
	}

	return health
}

func (m *HealthMonitor) checkGoroutines() ComponentHealth {
	numGoroutines := runtime.NumGoroutine()

	health := ComponentHealth{
		Name:      "goroutines",
		LastCheck: time.Now(),
		Details: map[string]interface{}{
			"count": numGoroutines,
		},
	}

	if numGoroutines > m.goroutineThreshold {
		health.Status = HealthStatusDegraded
		health.Message = fmt.Sprintf("High goroutine count: %d", numGoroutines)
	} else {
		health.Status = HealthStatusHealthy
		health.Message = fmt.Sprintf("Goroutine count: %d", numGoroutines)
	}

	return health
}

func (m *HealthMonitor) recoverPanic(component string) {
	if r := recover(); r != nil {
		m.mu.Lock()
		m.panicRecoveries++
		m.mu.Unlock()

		m.sendAlert(HealthAlert{
			Type:      AlertPanicRecovered,
			Component: component,
			Status:    HealthStatusUnhealthy,
			Message:   fmt.Sprintf("Panic recovered: %v", r),
			Timestamp: time.Now(),
		})
	}
}

func (m *HealthMonitor) sendAlert(alert HealthAlert) {
	if m.onAlert != nil {
		m.onAlert(alert)
	}
}

func (m *HealthMonitor) logHeartbeat() {
	// This would integrate with the logging system
	// For now, it's a placeholder
}

// GetHealth returns the current health status.
func (m *HealthMonitor) GetHealth() SystemHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	components := make([]ComponentHealth, 0, len(m.componentHealth))
	for _, h := range m.componentHealth {
		components = append(components, h)
	}

	return SystemHealth{
		Status:          m.overallStatus,
		Uptime:          time.Since(m.startTime),
		StartTime:       m.startTime,
		LastHeartbeat:   m.lastHeartbeat,
		Components:      components,
		Goroutines:      runtime.NumGoroutine(),
		MemoryAllocMB:   memStats.Alloc / 1024 / 1024,
		MemorySysMB:     memStats.Sys / 1024 / 1024,
		NumGC:           memStats.NumGC,
		TotalChecks:     m.totalChecks,
		FailedChecks:    m.failedChecks,
		PanicRecoveries: m.panicRecoveries,
	}
}

// GetComponentHealth returns health for a specific component.
func (m *HealthMonitor) GetComponentHealth(name string) (ComponentHealth, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	health, ok := m.componentHealth[name]
	return health, ok
}

// IsHealthy returns true if the system is healthy.
func (m *HealthMonitor) IsHealthy() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.overallStatus == HealthStatusHealthy
}

// SystemHealth represents overall system health.
type SystemHealth struct {
	Status          HealthStatus
	Uptime          time.Duration
	StartTime       time.Time
	LastHeartbeat   time.Time
	Components      []ComponentHealth
	Goroutines      int
	MemoryAllocMB   uint64
	MemorySysMB     uint64
	NumGC           uint32
	TotalChecks     int64
	FailedChecks    int64
	PanicRecoveries int64
}

// ToJSON returns the health status as JSON.
func (h SystemHealth) ToJSON() ([]byte, error) {
	return json.Marshal(h)
}

// HealthAlertType represents the type of health alert.
type HealthAlertType string

const (
	AlertComponentUnhealthy HealthAlertType = "COMPONENT_UNHEALTHY"
	AlertHighMemory         HealthAlertType = "HIGH_MEMORY"
	AlertHighGoroutines     HealthAlertType = "HIGH_GOROUTINES"
	AlertPanicRecovered     HealthAlertType = "PANIC_RECOVERED"
	AlertWebSocketDisconnect HealthAlertType = "WEBSOCKET_DISCONNECT"
)

// HealthAlert represents a health alert.
type HealthAlert struct {
	Type      HealthAlertType
	Component string
	Status    HealthStatus
	Message   string
	Timestamp time.Time
}

// HealthHTTPHandler returns an HTTP handler for health checks.
func (m *HealthMonitor) HealthHTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		health := m.GetHealth()

		w.Header().Set("Content-Type", "application/json")

		switch health.Status {
		case HealthStatusHealthy:
			w.WriteHeader(http.StatusOK)
		case HealthStatusDegraded:
			w.WriteHeader(http.StatusOK) // Still operational
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		data, _ := health.ToJSON()
		w.Write(data)
	}
}

// LivenessHTTPHandler returns an HTTP handler for liveness checks.
func (m *HealthMonitor) LivenessHTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"alive"}`))
	}
}

// ReadinessHTTPHandler returns an HTTP handler for readiness checks.
func (m *HealthMonitor) ReadinessHTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		health := m.GetHealth()

		w.Header().Set("Content-Type", "application/json")

		if health.Status == HealthStatusHealthy || health.Status == HealthStatusDegraded {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ready"}`))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"not_ready"}`))
		}
	}
}

// WebSocketHealthCheck creates a health check for WebSocket connections.
func WebSocketHealthCheck(isConnected func() bool, lastMessageTime func() time.Time) HealthCheck {
	return func(ctx context.Context) ComponentHealth {
		health := ComponentHealth{
			Name:      "websocket",
			LastCheck: time.Now(),
			Details:   make(map[string]interface{}),
		}

		connected := isConnected()
		lastMsg := lastMessageTime()

		health.Details["connected"] = connected
		health.Details["last_message"] = lastMsg

		if !connected {
			health.Status = HealthStatusUnhealthy
			health.Message = "WebSocket disconnected"
			return health
		}

		// Check if we've received messages recently (within 5 minutes)
		if time.Since(lastMsg) > 5*time.Minute {
			health.Status = HealthStatusDegraded
			health.Message = fmt.Sprintf("No messages for %v", time.Since(lastMsg).Round(time.Second))
			return health
		}

		health.Status = HealthStatusHealthy
		health.Message = "WebSocket connected and receiving data"
		return health
	}
}

// DatabaseHealthCheck creates a health check for database connections.
func DatabaseHealthCheck(ping func(ctx context.Context) error) HealthCheck {
	return func(ctx context.Context) ComponentHealth {
		health := ComponentHealth{
			Name:      "database",
			LastCheck: time.Now(),
		}

		start := time.Now()
		err := ping(ctx)
		health.Latency = time.Since(start)

		if err != nil {
			health.Status = HealthStatusUnhealthy
			health.Message = fmt.Sprintf("Database ping failed: %v", err)
			return health
		}

		if health.Latency > 100*time.Millisecond {
			health.Status = HealthStatusDegraded
			health.Message = fmt.Sprintf("Database slow: %v", health.Latency)
			return health
		}

		health.Status = HealthStatusHealthy
		health.Message = fmt.Sprintf("Database healthy: %v", health.Latency)
		return health
	}
}

// APIHealthCheck creates a health check for external API connections.
func APIHealthCheck(name string, check func(ctx context.Context) (time.Duration, error)) HealthCheck {
	return func(ctx context.Context) ComponentHealth {
		health := ComponentHealth{
			Name:      name,
			LastCheck: time.Now(),
		}

		latency, err := check(ctx)
		health.Latency = latency

		if err != nil {
			health.Status = HealthStatusUnhealthy
			health.Message = fmt.Sprintf("API check failed: %v", err)
			return health
		}

		if latency > 2*time.Second {
			health.Status = HealthStatusDegraded
			health.Message = fmt.Sprintf("API slow: %v", latency)
			return health
		}

		health.Status = HealthStatusHealthy
		health.Message = fmt.Sprintf("API healthy: %v", latency)
		return health
	}
}

// DefaultHealthMonitor is a global instance.
var DefaultHealthMonitor = NewHealthMonitor(DefaultHealthMonitorConfig())
