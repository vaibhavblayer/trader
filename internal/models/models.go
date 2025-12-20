// Package models provides domain models for the trading application.
package models

import (
	"time"
)

// Exchange represents a stock exchange.
type Exchange string

const (
	NSE Exchange = "NSE"
	BSE Exchange = "BSE"
	NFO Exchange = "NFO" // F&O
	CDS Exchange = "CDS" // Currency
	MCX Exchange = "MCX" // Commodity
)

// OrderSide represents the side of an order.
type OrderSide string

const (
	OrderSideBuy  OrderSide = "BUY"
	OrderSideSell OrderSide = "SELL"
)

// OrderType represents the type of an order.
type OrderType string

const (
	OrderTypeMarket    OrderType = "MARKET"
	OrderTypeLimit     OrderType = "LIMIT"
	OrderTypeStopLoss  OrderType = "SL"
	OrderTypeStopLossM OrderType = "SL-M"
)

// ProductType represents the product type of an order.
type ProductType string

const (
	ProductMIS  ProductType = "MIS"  // Intraday
	ProductCNC  ProductType = "CNC"  // Delivery
	ProductNRML ProductType = "NRML" // F&O Normal
)

// MarketStatus represents the current market status.
type MarketStatus string

const (
	MarketOpen              MarketStatus = "OPEN"
	MarketPreOpen           MarketStatus = "PRE_OPEN"
	MarketClosed            MarketStatus = "CLOSED"
	MarketMISSquareOffWarn  MarketStatus = "MIS_SQUAREOFF_WARNING"
)

// Candle represents OHLCV data for a time period.
type Candle struct {
	Timestamp time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    int64
}

// Tick represents real-time market data.
type Tick struct {
	Symbol       string
	LTP          float64
	Open         float64
	High         float64
	Low          float64
	Close        float64
	Volume       int64
	BuyQuantity  int64
	SellQuantity int64
	BidPrice     float64
	AskPrice     float64
	Timestamp    time.Time
}

// Quote represents a market quote.
type Quote struct {
	Symbol        string
	LTP           float64
	Open          float64
	High          float64
	Low           float64
	Close         float64
	Volume        int64
	Change        float64
	ChangePercent float64
	Timestamp     time.Time
}

// Instrument represents a tradeable instrument.
type Instrument struct {
	Token       uint32
	Symbol      string
	Name        string
	Exchange    Exchange
	Segment     string
	LotSize     int
	TickSize    float64
	Expiry      time.Time
	Strike      float64
	InstrType   string
}
