// Package broker provides broker integration implementations.
package broker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"zerodha-trader/internal/models"
)

// Segment represents a trading segment.
type Segment string

const (
	SegmentNSEEquity  Segment = "NSE"
	SegmentBSEEquity  Segment = "BSE"
	SegmentNSEFO      Segment = "NFO"
	SegmentCurrency   Segment = "CDS"
	SegmentCommodity  Segment = "MCX"
)

// SegmentInfo contains segment-specific information.
type SegmentInfo struct {
	Segment       Segment
	Exchange      models.Exchange
	Description   string
	TradingHours  TradingHours
	LotSizeMultiplier int
	TickSize      float64
}

// TradingHours represents trading hours for a segment.
type TradingHours struct {
	PreOpenStart  time.Duration // From midnight
	PreOpenEnd    time.Duration
	MarketOpen    time.Duration
	MarketClose   time.Duration
	PostCloseEnd  time.Duration
}

// SegmentManager manages multi-segment trading operations.
type SegmentManager struct {
	broker      Broker
	instruments map[string]models.Instrument // key: exchange:symbol
	segments    map[Segment]*SegmentInfo
	lotSizes    map[string]int // key: exchange:symbol
	mu          sync.RWMutex
}

// NewSegmentManager creates a new segment manager.
func NewSegmentManager(broker Broker) *SegmentManager {
	sm := &SegmentManager{
		broker:      broker,
		instruments: make(map[string]models.Instrument),
		segments:    make(map[Segment]*SegmentInfo),
		lotSizes:    make(map[string]int),
	}
	
	// Initialize segment info
	sm.initSegments()
	
	return sm
}


// initSegments initializes segment information.
func (sm *SegmentManager) initSegments() {
	// IST timezone offset (5:30 from UTC)
	ist := 5*time.Hour + 30*time.Minute
	
	sm.segments[SegmentNSEEquity] = &SegmentInfo{
		Segment:     SegmentNSEEquity,
		Exchange:    models.NSE,
		Description: "NSE Equity",
		TradingHours: TradingHours{
			PreOpenStart: 9*time.Hour - ist,
			PreOpenEnd:   9*time.Hour + 8*time.Minute - ist,
			MarketOpen:   9*time.Hour + 15*time.Minute - ist,
			MarketClose:  15*time.Hour + 30*time.Minute - ist,
			PostCloseEnd: 16*time.Hour - ist,
		},
		LotSizeMultiplier: 1,
		TickSize:          0.05,
	}
	
	sm.segments[SegmentBSEEquity] = &SegmentInfo{
		Segment:     SegmentBSEEquity,
		Exchange:    models.BSE,
		Description: "BSE Equity",
		TradingHours: TradingHours{
			PreOpenStart: 9*time.Hour - ist,
			PreOpenEnd:   9*time.Hour + 8*time.Minute - ist,
			MarketOpen:   9*time.Hour + 15*time.Minute - ist,
			MarketClose:  15*time.Hour + 30*time.Minute - ist,
			PostCloseEnd: 16*time.Hour - ist,
		},
		LotSizeMultiplier: 1,
		TickSize:          0.05,
	}
	
	sm.segments[SegmentNSEFO] = &SegmentInfo{
		Segment:     SegmentNSEFO,
		Exchange:    models.NFO,
		Description: "NSE Futures & Options",
		TradingHours: TradingHours{
			PreOpenStart: 9*time.Hour - ist,
			PreOpenEnd:   9*time.Hour + 8*time.Minute - ist,
			MarketOpen:   9*time.Hour + 15*time.Minute - ist,
			MarketClose:  15*time.Hour + 30*time.Minute - ist,
			PostCloseEnd: 16*time.Hour - ist,
		},
		LotSizeMultiplier: 1, // Varies by instrument
		TickSize:          0.05,
	}
	
	sm.segments[SegmentCurrency] = &SegmentInfo{
		Segment:     SegmentCurrency,
		Exchange:    models.CDS,
		Description: "Currency Derivatives",
		TradingHours: TradingHours{
			MarketOpen:  9*time.Hour - ist,
			MarketClose: 17*time.Hour - ist,
		},
		LotSizeMultiplier: 1000, // Standard lot for USDINR
		TickSize:          0.0025,
	}
	
	sm.segments[SegmentCommodity] = &SegmentInfo{
		Segment:     SegmentCommodity,
		Exchange:    models.MCX,
		Description: "MCX Commodity",
		TradingHours: TradingHours{
			MarketOpen:  9*time.Hour - ist,
			MarketClose: 23*time.Hour + 30*time.Minute - ist,
		},
		LotSizeMultiplier: 1, // Varies by commodity
		TickSize:          1.0,
	}
}

// LoadInstruments loads and caches instruments for a segment.
func (sm *SegmentManager) LoadInstruments(ctx context.Context, segment Segment) error {
	segInfo, ok := sm.segments[segment]
	if !ok {
		return fmt.Errorf("unknown segment: %s", segment)
	}
	
	instruments, err := sm.broker.GetInstruments(ctx, segInfo.Exchange)
	if err != nil {
		return fmt.Errorf("failed to load instruments for %s: %w", segment, err)
	}
	
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	for _, inst := range instruments {
		key := fmt.Sprintf("%s:%s", inst.Exchange, inst.Symbol)
		sm.instruments[key] = inst
		sm.lotSizes[key] = inst.LotSize
	}
	
	return nil
}

// LoadAllInstruments loads instruments for all segments.
func (sm *SegmentManager) LoadAllInstruments(ctx context.Context) error {
	for segment := range sm.segments {
		if err := sm.LoadInstruments(ctx, segment); err != nil {
			return err
		}
	}
	return nil
}

// GetInstrument returns instrument info for a symbol.
func (sm *SegmentManager) GetInstrument(exchange models.Exchange, symbol string) (*models.Instrument, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	key := fmt.Sprintf("%s:%s", exchange, symbol)
	inst, ok := sm.instruments[key]
	if !ok {
		return nil, fmt.Errorf("instrument not found: %s", key)
	}
	return &inst, nil
}

// GetLotSize returns the lot size for a symbol.
func (sm *SegmentManager) GetLotSize(exchange models.Exchange, symbol string) int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	key := fmt.Sprintf("%s:%s", exchange, symbol)
	if lotSize, ok := sm.lotSizes[key]; ok {
		return lotSize
	}
	return 1
}

// GetSegmentInfo returns segment information.
func (sm *SegmentManager) GetSegmentInfo(segment Segment) (*SegmentInfo, error) {
	info, ok := sm.segments[segment]
	if !ok {
		return nil, fmt.Errorf("unknown segment: %s", segment)
	}
	return info, nil
}

// GetSegmentForExchange returns the segment for an exchange.
func (sm *SegmentManager) GetSegmentForExchange(exchange models.Exchange) Segment {
	switch exchange {
	case models.NSE:
		return SegmentNSEEquity
	case models.BSE:
		return SegmentBSEEquity
	case models.NFO:
		return SegmentNSEFO
	case models.CDS:
		return SegmentCurrency
	case models.MCX:
		return SegmentCommodity
	default:
		return SegmentNSEEquity
	}
}


// ValidateOrder validates an order against segment-specific rules.
func (sm *SegmentManager) ValidateOrder(order *models.Order) error {
	segment := sm.GetSegmentForExchange(order.Exchange)
	segInfo, ok := sm.segments[segment]
	if !ok {
		return fmt.Errorf("unknown segment for exchange: %s", order.Exchange)
	}
	
	// Get lot size
	lotSize := sm.GetLotSize(order.Exchange, order.Symbol)
	
	// Validate quantity is multiple of lot size
	if lotSize > 1 && order.Quantity%lotSize != 0 {
		return fmt.Errorf("quantity must be multiple of lot size %d for %s", lotSize, order.Symbol)
	}
	
	// Validate price tick size
	if order.Price > 0 && segInfo.TickSize > 0 {
		// Check if price is valid tick
		remainder := order.Price - float64(int(order.Price/segInfo.TickSize))*segInfo.TickSize
		if remainder > 0.0001 {
			return fmt.Errorf("price must be multiple of tick size %.4f", segInfo.TickSize)
		}
	}
	
	// Validate product type for segment
	if segment == SegmentNSEFO || segment == SegmentCurrency || segment == SegmentCommodity {
		if order.Product == models.ProductCNC {
			return fmt.Errorf("CNC product not allowed for %s segment", segment)
		}
	}
	
	return nil
}

// GetExpiryDates returns expiry dates for F&O instruments.
func (sm *SegmentManager) GetExpiryDates(ctx context.Context, symbol string) ([]time.Time, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	expiries := make(map[time.Time]bool)
	
	for key, inst := range sm.instruments {
		if inst.Name == symbol && (inst.Exchange == models.NFO || inst.Exchange == models.CDS || inst.Exchange == models.MCX) {
			if !inst.Expiry.IsZero() {
				expiries[inst.Expiry] = true
			}
			_ = key // suppress unused warning
		}
	}
	
	result := make([]time.Time, 0, len(expiries))
	for exp := range expiries {
		result = append(result, exp)
	}
	
	return result, nil
}

// GetStrikePrices returns available strike prices for an option.
func (sm *SegmentManager) GetStrikePrices(ctx context.Context, symbol string, expiry time.Time) ([]float64, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	strikes := make(map[float64]bool)
	
	for _, inst := range sm.instruments {
		if inst.Name == symbol && inst.Exchange == models.NFO {
			if sameDay(inst.Expiry, expiry) && inst.Strike > 0 {
				strikes[inst.Strike] = true
			}
		}
	}
	
	result := make([]float64, 0, len(strikes))
	for strike := range strikes {
		result = append(result, strike)
	}
	
	return result, nil
}

// GetFOSymbol constructs F&O trading symbol.
func (sm *SegmentManager) GetFOSymbol(underlying string, expiry time.Time, strike float64, optionType string) string {
	// Format: NIFTY24DEC19500CE
	expiryStr := expiry.Format("06Jan")
	
	if optionType == "" {
		// Futures
		return fmt.Sprintf("%s%sFUT", underlying, expiryStr)
	}
	
	// Options
	return fmt.Sprintf("%s%s%.0f%s", underlying, expiryStr, strike, optionType)
}

// ParseFOSymbol parses F&O trading symbol into components.
func (sm *SegmentManager) ParseFOSymbol(symbol string) (*FOSymbolInfo, error) {
	// This is a simplified parser - real implementation would be more robust
	info := &FOSymbolInfo{
		Symbol: symbol,
	}
	
	// Check if it's a futures symbol
	if len(symbol) > 3 && symbol[len(symbol)-3:] == "FUT" {
		info.IsFutures = true
		// Extract underlying and expiry
		// Format: NIFTY24DECFUT
	} else if len(symbol) > 2 {
		// Options - ends with CE or PE
		suffix := symbol[len(symbol)-2:]
		if suffix == "CE" || suffix == "PE" {
			info.IsOption = true
			info.OptionType = suffix
		}
	}
	
	return info, nil
}

// FOSymbolInfo contains parsed F&O symbol information.
type FOSymbolInfo struct {
	Symbol     string
	Underlying string
	Expiry     time.Time
	Strike     float64
	OptionType string
	IsFutures  bool
	IsOption   bool
}

// FormatPosition formats position with segment-specific information.
func (sm *SegmentManager) FormatPosition(pos *models.Position) string {
	segment := sm.GetSegmentForExchange(pos.Exchange)
	lotSize := sm.GetLotSize(pos.Exchange, pos.Symbol)
	
	lots := pos.Quantity
	if lotSize > 1 {
		lots = pos.Quantity / lotSize
	}
	
	switch segment {
	case SegmentNSEFO, SegmentCurrency, SegmentCommodity:
		return fmt.Sprintf("%s %s: %d lots (%d qty) @ %.2f, P&L: %.2f (%.2f%%)",
			pos.Exchange, pos.Symbol, lots, pos.Quantity, pos.AveragePrice, pos.PnL, pos.PnLPercent)
	default:
		return fmt.Sprintf("%s %s: %d qty @ %.2f, P&L: %.2f (%.2f%%)",
			pos.Exchange, pos.Symbol, pos.Quantity, pos.AveragePrice, pos.PnL, pos.PnLPercent)
	}
}

// GetAllSegments returns all available segments.
func (sm *SegmentManager) GetAllSegments() []Segment {
	return []Segment{
		SegmentNSEEquity,
		SegmentBSEEquity,
		SegmentNSEFO,
		SegmentCurrency,
		SegmentCommodity,
	}
}

// IsMarketOpen checks if market is open for a segment.
func (sm *SegmentManager) IsMarketOpen(segment Segment) bool {
	segInfo, ok := sm.segments[segment]
	if !ok {
		return false
	}
	
	now := time.Now()
	// Convert to duration from midnight
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	sinceMidnight := now.Sub(midnight)
	
	return sinceMidnight >= segInfo.TradingHours.MarketOpen && sinceMidnight <= segInfo.TradingHours.MarketClose
}
