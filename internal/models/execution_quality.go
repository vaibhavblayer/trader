package models

import "time"

// ExecutionQualityFilter filters persisted execution quality reports.
type ExecutionQualityFilter struct {
	StartDate       time.Time
	EndDate         time.Time
	Symbol          string
	SlippageAlertBp float64
	Limit           int
}

// ExecutionQualityReport summarizes order execution quality from durable logs.
type ExecutionQualityReport struct {
	GeneratedAt          time.Time
	StartDate            time.Time
	EndDate              time.Time
	Symbol               string
	TotalOrders          int
	FilledOrders         int
	OpenOrders           int
	CancelledOrders      int
	PartialFills         int
	RejectedOrders       int
	BlockedExecutions    int
	ProtectiveFailures   int
	FillRate             float64
	PartialFillRate      float64
	RejectionRate        float64
	AvgFilledQty         float64
	AvgSlippageBp        float64
	MaxAdverseSlippageBp float64
	TotalCosts           float64
	TotalTurnover        float64
	CostBp               float64
	BySymbol             []ExecutionQualityGroup
	ByOrderType          []ExecutionQualityGroup
	BySide               []ExecutionQualityGroup
	HighSlippageOrders   []ExecutionQualitySample
	RecentIssues         []ExecutionQualityIssue
}

// ExecutionQualityGroup summarizes execution quality for one grouping key.
type ExecutionQualityGroup struct {
	Key                  string
	TotalOrders          int
	FilledOrders         int
	OpenOrders           int
	CancelledOrders      int
	PartialFills         int
	RejectedOrders       int
	BlockedExecutions    int
	ProtectiveFailures   int
	FillRate             float64
	PartialFillRate      float64
	RejectionRate        float64
	AvgFilledQty         float64
	AvgSlippageBp        float64
	MaxAdverseSlippageBp float64
	TotalCosts           float64
	TotalTurnover        float64
	CostBp               float64
}

// ExecutionQualitySample represents an order with notable execution quality.
type ExecutionQualitySample struct {
	Timestamp  time.Time
	OrderID    string
	Symbol     string
	Side       string
	OrderType  string
	Status     string
	Quantity   int
	FilledQty  int
	Expected   float64
	Actual     float64
	SlippageBp float64
	Costs      float64
}

// ExecutionQualityIssue represents rejected, blocked, or failed execution lifecycle events.
type ExecutionQualityIssue struct {
	Timestamp  time.Time
	Source     string
	DecisionID string
	OrderID    string
	Symbol     string
	Action     string
	Stage      string
	Status     string
	Message    string
}
