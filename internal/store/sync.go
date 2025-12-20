// Package store provides data persistence implementations.
package store

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"zerodha-trader/internal/models"
)

// SyncDataType represents the type of data being synced.
type SyncDataType string

const (
	SyncTypeCandles   SyncDataType = "candles"
	SyncTypeTrades    SyncDataType = "trades"
	SyncTypeOrders    SyncDataType = "orders"
	SyncTypePositions SyncDataType = "positions"
	SyncTypeHoldings  SyncDataType = "holdings"
	SyncTypeQuotes    SyncDataType = "quotes"
)

// SyncStatus represents the current sync status.
type SyncStatus struct {
	DataType     SyncDataType
	LastSync     time.Time
	IsStale      bool
	StaleMinutes int
	Error        error
}

// DataFreshness represents the freshness of cached data.
type DataFreshness struct {
	DataType    SyncDataType
	LastUpdated time.Time
	IsFresh     bool
	Age         time.Duration
}

// SyncConfig holds configuration for the sync manager.
type SyncConfig struct {
	// StaleThresholds defines how old data can be before it's considered stale (in minutes)
	StaleThresholds map[SyncDataType]int
	// AutoSyncInterval is how often to attempt auto-sync when online
	AutoSyncInterval time.Duration
	// ConnectionCheckInterval is how often to check connectivity
	ConnectionCheckInterval time.Duration
	// ConnectionCheckURL is the URL to ping for connectivity check
	ConnectionCheckURL string
}

// DefaultSyncConfig returns default sync configuration.
func DefaultSyncConfig() *SyncConfig {
	return &SyncConfig{
		StaleThresholds: map[SyncDataType]int{
			SyncTypeCandles:   60,   // 1 hour
			SyncTypeTrades:    5,    // 5 minutes
			SyncTypeOrders:    1,    // 1 minute
			SyncTypePositions: 1,    // 1 minute
			SyncTypeHoldings:  60,   // 1 hour
			SyncTypeQuotes:    1,    // 1 minute
		},
		AutoSyncInterval:        5 * time.Minute,
		ConnectionCheckInterval: 30 * time.Second,
		ConnectionCheckURL:      "https://api.kite.trade",
	}
}

// SyncManager manages offline mode and data synchronization.
type SyncManager struct {
	store      DataStore
	config     *SyncConfig
	isOnline   bool
	mu         sync.RWMutex
	httpClient *http.Client

	// Callbacks for sync events
	onOnline       func()
	onOffline      func()
	onSyncComplete func(dataType SyncDataType)
	onStaleData    func(dataType SyncDataType, age time.Duration)

	// Stop channel for background goroutines
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewSyncManager creates a new sync manager.
func NewSyncManager(store DataStore, config *SyncConfig) *SyncManager {
	if config == nil {
		config = DefaultSyncConfig()
	}

	return &SyncManager{
		store:    store,
		config:   config,
		isOnline: true, // Assume online initially
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout: 3 * time.Second,
				}).DialContext,
			},
		},
		stopCh: make(chan struct{}),
	}
}


// SetOnlineCallback sets the callback for when connection is restored.
func (sm *SyncManager) SetOnlineCallback(fn func()) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onOnline = fn
}

// SetOfflineCallback sets the callback for when connection is lost.
func (sm *SyncManager) SetOfflineCallback(fn func()) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onOffline = fn
}

// SetSyncCompleteCallback sets the callback for when sync completes.
func (sm *SyncManager) SetSyncCompleteCallback(fn func(dataType SyncDataType)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onSyncComplete = fn
}

// SetStaleDataCallback sets the callback for when stale data is detected.
func (sm *SyncManager) SetStaleDataCallback(fn func(dataType SyncDataType, age time.Duration)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onStaleData = fn
}

// Start starts the background connectivity monitoring.
func (sm *SyncManager) Start(ctx context.Context) {
	sm.wg.Add(1)
	go sm.monitorConnectivity(ctx)
}

// Stop stops the sync manager.
func (sm *SyncManager) Stop() {
	close(sm.stopCh)
	sm.wg.Wait()
}

// IsOnline returns whether the system is currently online.
func (sm *SyncManager) IsOnline() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.isOnline
}

// CheckConnectivity checks if the system can reach the broker API.
func (sm *SyncManager) CheckConnectivity() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, sm.config.ConnectionCheckURL, nil)
	if err != nil {
		return false
	}

	resp, err := sm.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Any response (even 4xx) means we're online
	return true
}

// monitorConnectivity runs in the background to monitor connectivity.
func (sm *SyncManager) monitorConnectivity(ctx context.Context) {
	defer sm.wg.Done()

	ticker := time.NewTicker(sm.config.ConnectionCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sm.stopCh:
			return
		case <-ticker.C:
			wasOnline := sm.IsOnline()
			isOnline := sm.CheckConnectivity()

			sm.mu.Lock()
			sm.isOnline = isOnline
			onOnline := sm.onOnline
			onOffline := sm.onOffline
			sm.mu.Unlock()

			// Trigger callbacks on state change
			if !wasOnline && isOnline && onOnline != nil {
				onOnline()
			} else if wasOnline && !isOnline && onOffline != nil {
				onOffline()
			}
		}
	}
}

// GetDataFreshness returns the freshness status of cached data.
func (sm *SyncManager) GetDataFreshness(dataType SyncDataType) *DataFreshness {
	lastSync := sm.store.GetLastSync(string(dataType))
	age := time.Since(lastSync)

	threshold := sm.config.StaleThresholds[dataType]
	if threshold == 0 {
		threshold = 60 // Default 1 hour
	}

	isFresh := age < time.Duration(threshold)*time.Minute

	return &DataFreshness{
		DataType:    dataType,
		LastUpdated: lastSync,
		IsFresh:     isFresh,
		Age:         age,
	}
}

// GetAllDataFreshness returns freshness status for all data types.
func (sm *SyncManager) GetAllDataFreshness() map[SyncDataType]*DataFreshness {
	result := make(map[SyncDataType]*DataFreshness)
	for dataType := range sm.config.StaleThresholds {
		result[dataType] = sm.GetDataFreshness(dataType)
	}
	return result
}

// IsDataStale checks if a specific data type is stale.
func (sm *SyncManager) IsDataStale(dataType SyncDataType) bool {
	freshness := sm.GetDataFreshness(dataType)
	return !freshness.IsFresh
}

// WarnIfStale checks if data is stale and triggers callback if so.
func (sm *SyncManager) WarnIfStale(dataType SyncDataType) bool {
	freshness := sm.GetDataFreshness(dataType)
	if !freshness.IsFresh {
		sm.mu.RLock()
		callback := sm.onStaleData
		sm.mu.RUnlock()

		if callback != nil {
			callback(dataType, freshness.Age)
		}
		return true
	}
	return false
}

// MarkSynced marks a data type as synced.
func (sm *SyncManager) MarkSynced(dataType SyncDataType) error {
	err := sm.store.SetLastSync(string(dataType), time.Now())
	if err != nil {
		return fmt.Errorf("failed to mark %s as synced: %w", dataType, err)
	}

	sm.mu.RLock()
	callback := sm.onSyncComplete
	sm.mu.RUnlock()

	if callback != nil {
		callback(dataType)
	}

	return nil
}

// GetSyncStatus returns the sync status for a data type.
func (sm *SyncManager) GetSyncStatus(dataType SyncDataType) *SyncStatus {
	lastSync := sm.store.GetLastSync(string(dataType))
	threshold := sm.config.StaleThresholds[dataType]
	if threshold == 0 {
		threshold = 60
	}

	age := time.Since(lastSync)
	isStale := age > time.Duration(threshold)*time.Minute

	return &SyncStatus{
		DataType:     dataType,
		LastSync:     lastSync,
		IsStale:      isStale,
		StaleMinutes: int(age.Minutes()),
	}
}

// GetAllSyncStatus returns sync status for all data types.
func (sm *SyncManager) GetAllSyncStatus() []*SyncStatus {
	var statuses []*SyncStatus
	for dataType := range sm.config.StaleThresholds {
		statuses = append(statuses, sm.GetSyncStatus(dataType))
	}
	return statuses
}


// CachedDataProvider wraps data fetching with caching and offline support.
type CachedDataProvider struct {
	syncManager *SyncManager
	store       DataStore
}

// NewCachedDataProvider creates a new cached data provider.
func NewCachedDataProvider(syncManager *SyncManager, store DataStore) *CachedDataProvider {
	return &CachedDataProvider{
		syncManager: syncManager,
		store:       store,
	}
}

// GetCandlesWithCache fetches candles, using cache when offline or for recent data.
func (cdp *CachedDataProvider) GetCandlesWithCache(
	ctx context.Context,
	symbol, timeframe string,
	from, to time.Time,
	fetchFn func(ctx context.Context, symbol, timeframe string, from, to time.Time) ([]models.Candle, error),
) ([]models.Candle, bool, error) {
	// Check if we have cached data
	cachedCandles, err := cdp.store.GetCandles(ctx, symbol, timeframe, from, to)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get cached candles: %w", err)
	}

	// If offline, return cached data
	if !cdp.syncManager.IsOnline() {
		cdp.syncManager.WarnIfStale(SyncTypeCandles)
		return cachedCandles, true, nil
	}

	// Check freshness - if data is fresh enough, use cache
	freshness := cdp.syncManager.GetDataFreshness(SyncTypeCandles)
	if freshness.IsFresh && len(cachedCandles) > 0 {
		return cachedCandles, true, nil
	}

	// Fetch fresh data
	candles, err := fetchFn(ctx, symbol, timeframe, from, to)
	if err != nil {
		// If fetch fails but we have cached data, use it
		if len(cachedCandles) > 0 {
			cdp.syncManager.WarnIfStale(SyncTypeCandles)
			return cachedCandles, true, nil
		}
		return nil, false, fmt.Errorf("failed to fetch candles and no cache available: %w", err)
	}

	// Save to cache
	if err := cdp.store.SaveCandles(ctx, symbol, timeframe, candles); err != nil {
		// Log but don't fail - we have the data
		fmt.Printf("warning: failed to cache candles: %v\n", err)
	}

	// Mark as synced
	cdp.syncManager.MarkSynced(SyncTypeCandles)

	return candles, false, nil
}

// FormatFreshness returns a human-readable freshness string.
func FormatFreshness(freshness *DataFreshness) string {
	if freshness.LastUpdated.IsZero() {
		return "Never synced"
	}

	age := freshness.Age
	var ageStr string

	switch {
	case age < time.Minute:
		ageStr = "just now"
	case age < time.Hour:
		ageStr = fmt.Sprintf("%d minutes ago", int(age.Minutes()))
	case age < 24*time.Hour:
		ageStr = fmt.Sprintf("%d hours ago", int(age.Hours()))
	default:
		ageStr = fmt.Sprintf("%d days ago", int(age.Hours()/24))
	}

	if freshness.IsFresh {
		return fmt.Sprintf("Updated %s", ageStr)
	}
	return fmt.Sprintf("⚠️ Stale data - Updated %s", ageStr)
}

// FormatSyncStatus returns a human-readable sync status string.
func FormatSyncStatus(status *SyncStatus) string {
	if status.LastSync.IsZero() {
		return fmt.Sprintf("%s: Never synced", status.DataType)
	}

	timeStr := status.LastSync.Format("15:04:05")
	if status.IsStale {
		return fmt.Sprintf("%s: ⚠️ Stale (last sync: %s, %d min ago)", status.DataType, timeStr, status.StaleMinutes)
	}
	return fmt.Sprintf("%s: ✓ Fresh (last sync: %s)", status.DataType, timeStr)
}


// TradeSyncResult represents the result of a trade sync operation.
type TradeSyncResult struct {
	TotalOrders     int
	NewTrades       int
	UpdatedTrades   int
	SkippedTrades   int
	Errors          []error
	SyncedAt        time.Time
}

// TradeReconciliation represents the reconciliation of local vs broker trades.
type TradeReconciliation struct {
	LocalOnly  []models.Trade // Trades only in local DB
	BrokerOnly []models.Order // Orders only in broker (not yet synced)
	Matched    []TradePair    // Matched trades
	Conflicts  []TradeConflict // Trades with conflicting data
}

// TradePair represents a matched local trade and broker order.
type TradePair struct {
	LocalTrade  models.Trade
	BrokerOrder models.Order
}

// TradeConflict represents a conflict between local and broker data.
type TradeConflict struct {
	LocalTrade  models.Trade
	BrokerOrder models.Order
	Differences []string
}

// TradeSync handles synchronization of trades with the broker.
type TradeSync struct {
	store       DataStore
	syncManager *SyncManager
}

// NewTradeSync creates a new trade sync handler.
func NewTradeSync(store DataStore, syncManager *SyncManager) *TradeSync {
	return &TradeSync{
		store:       store,
		syncManager: syncManager,
	}
}

// SyncTradesFromOrders syncs trades from broker order history.
func (ts *TradeSync) SyncTradesFromOrders(ctx context.Context, orders []models.Order) (*TradeSyncResult, error) {
	result := &TradeSyncResult{
		TotalOrders: len(orders),
		SyncedAt:    time.Now(),
	}

	// Get existing trades to avoid duplicates
	existingTrades, err := ts.store.GetTrades(ctx, TradeFilter{Limit: 10000})
	if err != nil {
		return nil, fmt.Errorf("failed to get existing trades: %w", err)
	}

	// Build a map of existing order IDs
	existingOrderIDs := make(map[string]bool)
	for _, trade := range existingTrades {
		for _, orderID := range trade.OrderIDs {
			existingOrderIDs[orderID] = true
		}
	}

	// Process completed orders
	for _, order := range orders {
		// Skip non-completed orders
		if order.Status != "COMPLETE" && order.Status != "FILLED" {
			result.SkippedTrades++
			continue
		}

		// Skip if already synced
		if existingOrderIDs[order.ID] {
			result.SkippedTrades++
			continue
		}

		// Create trade from order
		trade := ts.orderToTrade(order)

		// Save trade
		if err := ts.store.LogTrade(ctx, &trade); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to save trade for order %s: %w", order.ID, err))
			continue
		}

		result.NewTrades++
	}

	// Mark trades as synced
	if err := ts.syncManager.MarkSynced(SyncTypeTrades); err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("failed to mark trades as synced: %w", err))
	}

	return result, nil
}

// orderToTrade converts a broker order to a trade.
func (ts *TradeSync) orderToTrade(order models.Order) models.Trade {
	// Generate a unique trade ID
	tradeID := fmt.Sprintf("sync_%s_%d", order.ID, time.Now().UnixNano())

	// Determine entry/exit price based on order side
	entryPrice := order.AveragePrice
	if entryPrice == 0 {
		entryPrice = order.Price
	}

	return models.Trade{
		ID:         tradeID,
		Timestamp:  order.PlacedAt,
		Symbol:     order.Symbol,
		Exchange:   order.Exchange,
		Side:       order.Side,
		Product:    order.Product,
		Quantity:   order.FilledQty,
		EntryPrice: entryPrice,
		OrderIDs:   []string{order.ID},
		IsPaper:    false,
		Strategy:   "synced",
	}
}

// ReconcileTrades compares local trades with broker records.
func (ts *TradeSync) ReconcileTrades(ctx context.Context, brokerOrders []models.Order, dateRange DateRange) (*TradeReconciliation, error) {
	reconciliation := &TradeReconciliation{}

	// Get local trades for the date range
	localTrades, err := ts.store.GetTrades(ctx, TradeFilter{
		StartDate: dateRange.Start,
		EndDate:   dateRange.End,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get local trades: %w", err)
	}

	// Build maps for comparison
	localByOrderID := make(map[string]models.Trade)
	for _, trade := range localTrades {
		for _, orderID := range trade.OrderIDs {
			localByOrderID[orderID] = trade
		}
	}

	brokerByOrderID := make(map[string]models.Order)
	for _, order := range brokerOrders {
		if order.Status == "COMPLETE" || order.Status == "FILLED" {
			brokerByOrderID[order.ID] = order
		}
	}

	// Find matches, broker-only, and local-only
	matchedOrderIDs := make(map[string]bool)

	for orderID, brokerOrder := range brokerByOrderID {
		if localTrade, exists := localByOrderID[orderID]; exists {
			// Check for conflicts
			conflicts := ts.findConflicts(localTrade, brokerOrder)
			if len(conflicts) > 0 {
				reconciliation.Conflicts = append(reconciliation.Conflicts, TradeConflict{
					LocalTrade:  localTrade,
					BrokerOrder: brokerOrder,
					Differences: conflicts,
				})
			} else {
				reconciliation.Matched = append(reconciliation.Matched, TradePair{
					LocalTrade:  localTrade,
					BrokerOrder: brokerOrder,
				})
			}
			matchedOrderIDs[orderID] = true
		} else {
			reconciliation.BrokerOnly = append(reconciliation.BrokerOnly, brokerOrder)
		}
	}

	// Find local-only trades
	for _, trade := range localTrades {
		hasMatch := false
		for _, orderID := range trade.OrderIDs {
			if matchedOrderIDs[orderID] {
				hasMatch = true
				break
			}
		}
		if !hasMatch && !trade.IsPaper {
			reconciliation.LocalOnly = append(reconciliation.LocalOnly, trade)
		}
	}

	return reconciliation, nil
}

// findConflicts finds differences between local trade and broker order.
func (ts *TradeSync) findConflicts(trade models.Trade, order models.Order) []string {
	var conflicts []string

	// Check quantity
	if trade.Quantity != order.FilledQty && order.FilledQty > 0 {
		conflicts = append(conflicts, fmt.Sprintf("quantity mismatch: local=%d, broker=%d", trade.Quantity, order.FilledQty))
	}

	// Check price (with tolerance for floating point)
	priceDiff := trade.EntryPrice - order.AveragePrice
	if priceDiff < 0 {
		priceDiff = -priceDiff
	}
	if priceDiff > 0.01 && order.AveragePrice > 0 {
		conflicts = append(conflicts, fmt.Sprintf("price mismatch: local=%.2f, broker=%.2f", trade.EntryPrice, order.AveragePrice))
	}

	// Check side
	if string(trade.Side) != string(order.Side) {
		conflicts = append(conflicts, fmt.Sprintf("side mismatch: local=%s, broker=%s", trade.Side, order.Side))
	}

	return conflicts
}

// ImportHistoricalTrades imports historical trades from broker.
func (ts *TradeSync) ImportHistoricalTrades(ctx context.Context, orders []models.Order) (*TradeSyncResult, error) {
	return ts.SyncTradesFromOrders(ctx, orders)
}

// HandleModifiedOrder handles a modified order by updating the corresponding trade.
func (ts *TradeSync) HandleModifiedOrder(ctx context.Context, order models.Order) error {
	// Find the trade with this order ID
	trades, err := ts.store.GetTrades(ctx, TradeFilter{Limit: 10000})
	if err != nil {
		return fmt.Errorf("failed to get trades: %w", err)
	}

	for _, trade := range trades {
		for _, orderID := range trade.OrderIDs {
			if orderID == order.ID {
				// Update trade with new order data
				trade.Quantity = order.FilledQty
				if order.AveragePrice > 0 {
					trade.EntryPrice = order.AveragePrice
				}
				// Re-save the trade
				if err := ts.store.LogTrade(ctx, &trade); err != nil {
					return fmt.Errorf("failed to update trade: %w", err)
				}
				return nil
			}
		}
	}

	return nil // Order not found in any trade - might be new
}

// HandleCancelledOrder handles a cancelled order by marking the trade appropriately.
func (ts *TradeSync) HandleCancelledOrder(ctx context.Context, orderID string) error {
	// Find the trade with this order ID
	trades, err := ts.store.GetTrades(ctx, TradeFilter{Limit: 10000})
	if err != nil {
		return fmt.Errorf("failed to get trades: %w", err)
	}

	for _, trade := range trades {
		for i, oid := range trade.OrderIDs {
			if oid == orderID {
				// Remove the cancelled order from the trade
				trade.OrderIDs = append(trade.OrderIDs[:i], trade.OrderIDs[i+1:]...)
				
				// If no orders left, the trade was fully cancelled
				if len(trade.OrderIDs) == 0 {
					trade.Strategy = "cancelled"
				}
				
				if err := ts.store.LogTrade(ctx, &trade); err != nil {
					return fmt.Errorf("failed to update trade: %w", err)
				}
				return nil
			}
		}
	}

	return nil // Order not found - might not have been synced yet
}
