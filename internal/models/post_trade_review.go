package models

import "time"

// PostTradeReviewFilter filters post-trade review reports.
type PostTradeReviewFilter struct {
	StartDate time.Time
	EndDate   time.Time
	Symbol    string
	IsPaper   *bool
	Limit     int
}

// PostTradeReviewReport links completed trades to prediction and execution context.
type PostTradeReviewReport struct {
	GeneratedAt       time.Time
	StartDate         time.Time
	EndDate           time.Time
	Symbol            string
	TotalTrades       int
	ReviewedTrades    int
	Winners           int
	Losers            int
	NetPnL            float64
	AvgPnLPercent     float64
	WithPrediction    int
	WithExecution     int
	MissingPrediction int
	MissingExecution  int
	AvgSlippageBp     float64
	TotalCosts        float64
	BySetup           []PostTradeReviewGroup
	BySymbol          []PostTradeReviewGroup
	ByStrategy        []PostTradeReviewGroup
	Trades            []PostTradeReviewTrade
}

// PostTradeReviewGroup summarizes post-trade performance for one grouping key.
type PostTradeReviewGroup struct {
	Key               string
	Trades            int
	Winners           int
	Losers            int
	WinRate           float64
	NetPnL            float64
	AvgPnLPercent     float64
	WithPrediction    int
	WithExecution     int
	AvgConfidence     float64
	AvgSlippageBp     float64
	TotalCosts        float64
	MissingPrediction int
	MissingExecution  int
}

// PostTradeReviewTrade is one completed trade plus its available decision context.
type PostTradeReviewTrade struct {
	TradeID              string
	Timestamp            time.Time
	Symbol               string
	Side                 string
	Strategy             string
	Quantity             int
	EntryPrice           float64
	ExitPrice            float64
	PnL                  float64
	PnLPercent           float64
	IsPaper              bool
	DecisionID           string
	OrderIDs             []string
	PredictionID         string
	SetupName            string
	Timeframe            string
	PredictionConfidence float64
	PredictionOutcome    string
	GatesPassed          int
	GatesTotal           int
	ExecutionOrders      int
	FilledOrders         int
	PartialFills         int
	AvgSlippageBp        float64
	ExecutionCosts       float64
	ReviewFlags          []string
}
