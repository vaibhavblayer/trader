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
	Type         string
	Symbol       string
	Action       string
	Quantity     int
	Reason       string
	ExpectedCost float64
}

// BacktestEngine provides backtesting functionality.
type BacktestEngine interface {
	Run(ctx context.Context, config BacktestConfig) (*BacktestResult, error)
}

// BacktestConfig represents backtesting configuration.
type BacktestConfig struct {
	Symbol         string
	StartDate      time.Time
	EndDate        time.Time
	Timeframe      string
	InitialCapital float64
	Strategy       string
	Parameters     map[string]interface{}
	Slippage       float64 // as fraction, e.g. 0.001 = 0.1%
	Commission     float64 // as fraction, e.g. 0.0003 = 0.03%

	// Risk management
	StopLossPercent     float64 // e.g. 3.0 means 3% stop loss
	TakeProfitPercent   float64 // e.g. 6.0 means 6% take profit
	TrailingStopPercent float64 // e.g. 2.0 means 2% trailing stop
	MaxPositionPercent  float64 // max % of capital per trade (default 95)
	AllowShort          bool    // allow short selling

	// Event-based execution model
	ExecutionTiming      string  // "next_open" (default) or "same_close"
	AllowPartialFills    bool    // allow quantity to be capped by modeled volume
	MaxFillVolumePercent float64 // max fill as % of candle volume (0 disables)

	// Transaction cost model. All rates are fractions unless noted.
	BrokerageRate   float64 // broker fee as fraction of turnover
	BrokeragePerLeg float64 // flat broker fee per entry/exit leg
	STTRate         float64 // securities transaction tax rate
	ExchangeFeeRate float64
	SEBIRate        float64
	StampDutyRate   float64
	GSTRate         float64 // GST on brokerage + exchange fees
}

// BacktestResult represents backtesting results.
type BacktestResult struct {
	// Core metrics
	TotalReturn      float64
	AnnualizedReturn float64
	WinRate          float64
	MaxDrawdown      float64
	SharpeRatio      float64
	SortinoRatio     float64
	CalmarRatio      float64

	// Trade counts
	TotalTrades   int
	WinningTrades int
	LosingTrades  int

	// P&L
	GrossProfit   float64
	GrossLoss     float64
	NetProfit     float64
	TotalCosts    float64
	TotalTurnover float64
	TotalSlippage float64
	AvgWin        float64
	AvgLoss       float64
	LargestWin    float64
	LargestLoss   float64
	ProfitFactor  float64
	Expectancy    float64

	// Streaks
	MaxConsecutiveWins   int
	MaxConsecutiveLosses int

	// Time
	AvgHoldBars     int
	AvgWinHoldBars  int
	AvgLossHoldBars int
	PartialFills    int
	RejectedSignals int
	SignalActivity  SignalActivity

	// Capital
	StartCapital float64
	EndCapital   float64

	// Curves
	EquityCurve []EquityPoint
	Trades      []BacktestTrade
}

// SignalActivity summarizes how often a strategy emits actionable signals.
type SignalActivity struct {
	EvaluatedBars      int
	BuySignals         int
	SellSignals        int
	HoldSignals        int
	DirectionalSignals int
	SignalRatePct      float64
	BuySignalRatePct   float64
	SellSignalRatePct  float64
	TradeConversionPct float64
}

// StrategySignalDiagnostic explains the latest strategy signal and its gates.
type StrategySignalDiagnostic struct {
	Signal     string
	Confidence float64
	Reason     string
	Gates      []StrategyGateDiagnostic
}

// StrategyGateDiagnostic records one signal gate outcome.
type StrategyGateDiagnostic struct {
	Name      string
	Passed    bool
	Value     string
	Threshold string
}

// EquityPoint represents a point on the equity curve.
type EquityPoint struct {
	Timestamp time.Time
	Equity    float64
	Drawdown  float64 // current drawdown as fraction
}

// BacktestTrade represents a trade in backtesting.
type BacktestTrade struct {
	EntryTime    time.Time
	ExitTime     time.Time
	Symbol       string
	Side         string
	EntryPrice   float64
	ExitPrice    float64
	Quantity     int
	PnL          float64
	GrossPnL     float64
	EntryCosts   float64
	ExitCosts    float64
	TotalCosts   float64
	SlippageCost float64
	PnLPercent   float64
	ExitReason   string
	HoldBars     int
	PartialFill  bool
}
