// Package broker provides broker integration interfaces and implementations.
package broker

import (
	"context"
	"time"

	"zerodha-trader/internal/models"
)

// Broker defines the interface for broker operations.
type Broker interface {
	// Authentication
	Login(ctx context.Context) error
	Logout(ctx context.Context) error
	IsAuthenticated() bool
	RefreshSession(ctx context.Context) error

	// Market Data
	GetQuote(ctx context.Context, symbol string) (*models.Quote, error)
	GetHistorical(ctx context.Context, req HistoricalRequest) ([]models.Candle, error)
	GetInstruments(ctx context.Context, exchange models.Exchange) ([]models.Instrument, error)
	GetInstrumentToken(ctx context.Context, symbol string, exchange models.Exchange) (uint32, error)

	// Orders
	PlaceOrder(ctx context.Context, order *models.Order) (*OrderResult, error)
	ModifyOrder(ctx context.Context, orderID string, order *models.Order) error
	CancelOrder(ctx context.Context, orderID string) error
	GetOrders(ctx context.Context) ([]models.Order, error)
	GetOrderHistory(ctx context.Context, from, to time.Time) ([]models.Order, error)

	// GTT Orders
	PlaceGTT(ctx context.Context, gtt *models.GTTOrder) (*GTTResult, error)
	ModifyGTT(ctx context.Context, gttID string, gtt *models.GTTOrder) error
	CancelGTT(ctx context.Context, gttID string) error
	GetGTTs(ctx context.Context) ([]models.GTTOrder, error)

	// Positions & Holdings
	GetPositions(ctx context.Context) ([]models.Position, error)
	GetHoldings(ctx context.Context) ([]models.Holding, error)

	// Account
	GetBalance(ctx context.Context) (*models.Balance, error)
	GetMargins(ctx context.Context) (*models.Margins, error)

	// Options
	GetOptionChain(ctx context.Context, symbol string, expiry time.Time) (*models.OptionChain, error)

	// Futures
	GetFuturesChain(ctx context.Context, symbol string) (*models.FuturesChain, error)
}

// Ticker defines the interface for real-time market data streaming.
type Ticker interface {
	Connect(ctx context.Context) error
	Disconnect() error
	Subscribe(symbols []string, mode TickMode) error
	Unsubscribe(symbols []string) error
	RegisterSymbol(symbol string, token uint32)
	OnTick(handler func(models.Tick))
	OnError(handler func(error))
	OnConnect(handler func())
	OnDisconnect(handler func())
}

// TickMode represents the subscription mode for ticks.
type TickMode string

const (
	TickModeQuote TickMode = "quote"
	TickModeFull  TickMode = "full"
)

// HistoricalRequest represents a request for historical data.
type HistoricalRequest struct {
	Symbol    string
	Exchange  models.Exchange
	Timeframe string
	From      time.Time
	To        time.Time
}

// OrderResult represents the result of an order placement.
type OrderResult struct {
	OrderID string
	Status  string
	Message string
}

// GTTResult represents the result of a GTT order placement.
type GTTResult struct {
	TriggerID string
	Status    string
	Message   string
}
