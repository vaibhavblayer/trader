package models

import "time"

// Trade represents a completed trade.
type Trade struct {
	ID           string
	Timestamp    time.Time
	Symbol       string
	Exchange     Exchange
	Side         OrderSide
	Product      ProductType
	Quantity     int
	EntryPrice   float64
	ExitPrice    float64
	PnL          float64
	PnLPercent   float64
	Strategy     string
	OrderIDs     []string
	IsPaper      bool
	DecisionID   string
	HoldDuration time.Duration
	Slippage     float64
}

// TradeAnalysis represents analysis of a trade.
type TradeAnalysis struct {
	TradeID             string
	WhatWentRight       string
	WhatWentWrong       string
	LessonsLearned      string
	EntryQuality        int // 1-5
	ExitQuality         int // 1-5
	RiskManagementScore int // 1-5
	EmotionalNotes      string
	MarketContext       *TradeContext
}

// TradeContext represents market context during a trade.
type TradeContext struct {
	NiftyLevel  float64
	SectorIndex float64
	VIXLevel    float64
	MarketTrend string
	NewsEvents  string
}

// TradePlan represents a planned trade.
type TradePlan struct {
	ID         string
	Symbol     string
	Side       OrderSide
	EntryPrice float64
	StopLoss   float64
	Target1    float64
	Target2    float64
	Target3    float64
	Quantity   int
	RiskReward float64
	Status     PlanStatus
	Notes      string
	Reasoning  string
	Source     string // "manual", "ai", "prep"
	CreatedAt  time.Time
	ExecutedAt *time.Time
}

// PlanStatus represents the status of a trade plan.
type PlanStatus string

const (
	PlanPending   PlanStatus = "PENDING"
	PlanActive    PlanStatus = "ACTIVE"
	PlanExecuted  PlanStatus = "EXECUTED"
	PlanCancelled PlanStatus = "CANCELLED"
	PlanExpired   PlanStatus = "EXPIRED"
)

// JournalEntry represents a trading journal entry.
type JournalEntry struct {
	ID        string
	TradeID   string
	Date      time.Time
	Content   string
	Tags      []string
	Mood      string
	CreatedAt time.Time
	UpdatedAt time.Time
}
