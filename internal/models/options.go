package models

import "time"

// OptionChain represents an option chain.
type OptionChain struct {
	Symbol    string
	SpotPrice float64
	Expiry    time.Time
	Strikes   []OptionStrike
}

// OptionStrike represents a single strike in the option chain.
type OptionStrike struct {
	Strike float64
	Call   *OptionData
	Put    *OptionData
}

// OptionData represents option data for a single contract.
type OptionData struct {
	LTP    float64
	OI     int64
	Volume int64
	IV     float64
	Greeks OptionGreeks
}

// OptionGreeks represents option Greeks.
type OptionGreeks struct {
	Delta float64
	Gamma float64
	Theta float64
	Vega  float64
	Rho   float64
}

// FuturesChain represents a futures chain.
type FuturesChain struct {
	Symbol    string
	SpotPrice float64
	Expiries  []FuturesExpiry
}

// FuturesExpiry represents a single futures expiry.
type FuturesExpiry struct {
	Expiry       time.Time
	LTP          float64
	OI           int64
	Volume       int64
	Basis        float64 // Futures - Spot
	BasisPercent float64
}

// OptionStrategy represents an option strategy.
type OptionStrategy struct {
	Name       string
	Legs       []OptionLeg
	MaxProfit  float64
	MaxLoss    float64
	Breakevens []float64
	NetPremium float64
}

// OptionLeg represents a leg of an option strategy.
type OptionLeg struct {
	Strike   float64
	Type     string // CALL, PUT
	Side     OrderSide
	Quantity int
	Premium  float64
}
