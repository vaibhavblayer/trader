// Package trading provides trading operations including position management,
// exit strategies, portfolio analysis, and backtesting.
package trading

import (
	"context"
	"time"

	"zerodha-trader/internal/models"
)

// PositionManager handles position tracking and management.
type PositionManager interface {
	GetPositions(ctx context.Context) ([]models.Position, error)
	GetPosition(ctx context.Context, symbol string) (*models.Position, error)
	ExitPosition(ctx context.Context, symbol string) error
	ExitAllPositions(ctx context.Context) error
	GetUnrealizedPnL(ctx context.Context) (float64, error)
}

// ExitManager handles exit strategies.
type ExitManager interface {
	SetTrailingStop(symbol string, percent float64) error
	SetTimeBasedExit(symbol string, duration time.Duration) error
	SetScaleOutTargets(symbol string, targets []ScaleOutTarget) error
	CheckExits(ctx context.Context) ([]ExitSignal, error)
}

// ScaleOutTarget represents a scale-out target.
type ScaleOutTarget struct {
	Price    float64
	Quantity int
	Percent  float64
}

// ExitSignal represents an exit signal.
type ExitSignal struct {
	Symbol   string
	Reason   ExitReason
	Price    float64
	Quantity int
}

// ExitReason represents the reason for an exit.
type ExitReason string

const (
	ExitReasonTrailingStop ExitReason = "trailing_stop"
	ExitReasonTimeLimit    ExitReason = "time_limit"
	ExitReasonTarget       ExitReason = "target"
	ExitReasonStopLoss     ExitReason = "stop_loss"
	ExitReasonMISSquareOff ExitReason = "mis_square_off"
)

// PortfolioAnalyzer provides portfolio analysis functionality.
type PortfolioAnalyzer interface {
	GetPortfolioSummary(ctx context.Context) (*PortfolioSummary, error)
	GetSectorExposure(ctx context.Context) (map[string]float64, error)
	GetPortfolioGreeks(ctx context.Context) (*PortfolioGreeks, error)
	GetPortfolioBeta(ctx context.Context) (float64, error)
	GetVaR(ctx context.Context, confidence float64) (float64, error)
	SuggestHedges(ctx context.Context) ([]HedgeSuggestion, error)
}

// PortfolioSummary represents a portfolio summary.
type PortfolioSummary struct {
	TotalValue      float64
	InvestedValue   float64
	CurrentValue    float64
	TotalPnL        float64
	TotalPnLPercent float64
	DayPnL          float64
	DayPnLPercent   float64
	PositionCount   int
	HoldingCount    int
}

// PortfolioGreeks represents portfolio-level Greeks.
type PortfolioGreeks struct {
	Delta float64
	Gamma float64
	Theta float64
	Vega  float64
}

// HedgeSuggestion represents a hedging suggestion.
type HedgeSuggestion struct {
	Type        string
	Symbol      string
	Action      string
	Quantity    int
	Reason      string
	ExpectedCost float64
}

// BacktestEngine provides backtesting functionality.
type BacktestEngine interface {
	Run(ctx context.Context, config BacktestConfig) (*BacktestResult, error)
}

// BacktestConfig represents backtesting configuration.
type BacktestConfig struct {
	Symbol        string
	StartDate     time.Time
	EndDate       time.Time
	InitialCapital float64
	Strategy      string
	Parameters    map[string]interface{}
	Slippage      float64
	Commission    float64
}

// BacktestResult represents backtesting results.
type BacktestResult struct {
	TotalReturn     float64
	AnnualizedReturn float64
	WinRate         float64
	MaxDrawdown     float64
	SharpeRatio     float64
	TotalTrades     int
	WinningTrades   int
	LosingTrades    int
	AvgWin          float64
	AvgLoss         float64
	ProfitFactor    float64
	EquityCurve     []EquityPoint
	Trades          []BacktestTrade
}

// EquityPoint represents a point on the equity curve.
type EquityPoint struct {
	Timestamp time.Time
	Equity    float64
}

// BacktestTrade represents a trade in backtesting.
type BacktestTrade struct {
	EntryTime  time.Time
	ExitTime   time.Time
	Symbol     string
	Side       string
	EntryPrice float64
	ExitPrice  float64
	Quantity   int
	PnL        float64
	PnLPercent float64
}
