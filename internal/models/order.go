package models

import "time"

// Order represents a trading order.
type Order struct {
	ID           string
	Symbol       string
	Exchange     Exchange
	Side         OrderSide
	Type         OrderType
	Product      ProductType
	Quantity     int
	Price        float64
	TriggerPrice float64
	Validity     string // DAY, IOC
	Tag          string
	Status       string
	FilledQty    int
	AveragePrice float64
	PlacedAt     time.Time
}

// GTTOrder represents a Good Till Triggered order.
type GTTOrder struct {
	ID           string
	Symbol       string
	Exchange     Exchange
	TriggerType  string // single, two-leg
	TriggerPrice float64
	LastPrice    float64
	Orders       []GTTOrderLeg
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// GTTOrderLeg represents a leg of a GTT order.
type GTTOrderLeg struct {
	Side     OrderSide
	Type     OrderType
	Product  ProductType
	Quantity int
	Price    float64
}

// Position represents an open trading position.
type Position struct {
	Symbol       string
	Exchange     Exchange
	Product      ProductType
	Quantity     int
	AveragePrice float64
	LTP          float64
	PnL          float64
	PnLPercent   float64
	Value        float64
	Multiplier   int // For F&O lot size
}

// Holding represents a delivery holding.
type Holding struct {
	Symbol        string
	Quantity      int
	AveragePrice  float64
	LTP           float64
	PnL           float64
	PnLPercent    float64
	InvestedValue float64
	CurrentValue  float64
}

// Balance represents account balance.
type Balance struct {
	AvailableCash   float64
	UsedMargin      float64
	TotalEquity     float64
	CollateralValue float64
}

// Margins represents margin details.
type Margins struct {
	Equity    SegmentMargin
	Commodity SegmentMargin
}

// SegmentMargin represents margin for a segment.
type SegmentMargin struct {
	Available float64
	Used      float64
	Total     float64
}
