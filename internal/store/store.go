// Package store provides data persistence interfaces and implementations.
package store

import (
	"context"
	"time"

	"zerodha-trader/internal/models"
)

// DataStore defines the interface for data persistence.
type DataStore interface {
	// Candles
	SaveCandles(ctx context.Context, symbol, timeframe string, candles []models.Candle) error
	GetCandles(ctx context.Context, symbol, timeframe string, from, to time.Time) ([]models.Candle, error)
	GetCandlesFreshness(ctx context.Context, symbol, timeframe string) (time.Time, error)

	// Trades & Journal
	LogTrade(ctx context.Context, trade *models.Trade) error
	GetTrades(ctx context.Context, filter TradeFilter) ([]models.Trade, error)
	SaveTradeAnalysis(ctx context.Context, analysis *models.TradeAnalysis) error
	SaveJournalEntry(ctx context.Context, entry *models.JournalEntry) error
	GetJournal(ctx context.Context, filter JournalFilter) ([]models.JournalEntry, error)

	// Trade Plans
	SavePlan(ctx context.Context, plan *models.TradePlan) error
	GetPlans(ctx context.Context, filter PlanFilter) ([]models.TradePlan, error)
	UpdatePlanStatus(ctx context.Context, planID string, status models.PlanStatus) error

	// AI Decisions
	SaveDecision(ctx context.Context, decision *models.Decision) error
	GetDecisions(ctx context.Context, filter DecisionFilter) ([]models.Decision, error)
	GetDecisionByID(ctx context.Context, id string) (*models.Decision, error)
	GetDecisionStats(ctx context.Context, dateRange DateRange) (*models.AIStats, error)
	UpdateDecisionOutcome(ctx context.Context, id string, outcome models.DecisionOutcome, pnl float64) error

	// Watchlist
	AddToWatchlist(ctx context.Context, symbol, listName string) error
	RemoveFromWatchlist(ctx context.Context, symbol, listName string) error
	GetWatchlist(ctx context.Context, listName string) ([]string, error)
	GetAllWatchlists(ctx context.Context) (map[string][]string, error)

	// Alerts
	SaveAlert(ctx context.Context, alert *models.Alert) error
	GetActiveAlerts(ctx context.Context) ([]models.Alert, error)
	TriggerAlert(ctx context.Context, alertID string) error

	// Events Calendar
	SaveEvent(ctx context.Context, event *models.CorporateEvent) error
	GetUpcomingEvents(ctx context.Context, symbols []string, days int) ([]models.CorporateEvent, error)

	// Screener Queries
	SaveScreenerQuery(ctx context.Context, name string, query ScreenerQuery) error
	GetScreenerQuery(ctx context.Context, name string) (*ScreenerQuery, error)
	ListScreenerQueries(ctx context.Context) ([]string, error)

	// Sync
	GetLastSync(dataType string) time.Time
	SetLastSync(dataType string, t time.Time) error

	// Lifecycle
	Close() error
}

// TradeFilter represents filters for querying trades.
type TradeFilter struct {
	Symbol    string
	StartDate time.Time
	EndDate   time.Time
	Side      string
	IsPaper   *bool
	Limit     int
}

// JournalFilter represents filters for querying journal entries.
type JournalFilter struct {
	TradeID   string
	StartDate time.Time
	EndDate   time.Time
	Tags      []string
	Limit     int
}

// PlanFilter represents filters for querying trade plans.
type PlanFilter struct {
	Symbol string
	Status models.PlanStatus
	Source string
	Limit  int
}

// DecisionFilter represents filters for querying AI decisions.
type DecisionFilter struct {
	Symbol    string
	StartDate time.Time
	EndDate   time.Time
	Executed  *bool
	Outcome   string
	Limit     int
}

// DateRange represents a date range.
type DateRange struct {
	Start time.Time
	End   time.Time
}

// ScreenerQuery represents a saved screener query.
type ScreenerQuery struct {
	Name    string
	Filters []ScreenerFilter
}

// ScreenerFilter represents a single screener filter.
type ScreenerFilter struct {
	Field    string
	Operator string
	Value    interface{}
}
