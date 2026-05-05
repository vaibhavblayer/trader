package models

import "time"

// PaperPrediction represents an AI paper-trading prediction.
type PaperPrediction struct {
	ID          string
	Symbol      string
	Action      string // BUY, SELL
	Confidence  float64
	EntryPrice  float64
	TargetPrice float64
	StopLoss    float64
	TimeWindow  time.Duration
	CreatedAt   time.Time
	ExpiresAt   time.Time
	Reasoning   string

	// Outcome tracking
	Evaluated  bool
	ExitPrice  float64
	Outcome    string // RIGHT, WRONG, EXPIRED
	PnLPercent float64
}

// PaperPredictionStats holds prediction accuracy statistics.
type PaperPredictionStats struct {
	TotalPredictions   int
	RightPredictions   int
	WrongPredictions   int
	ExpiredPredictions int
	WinRate            float64
	AvgConfidence      float64
	AvgPnLPercent      float64
	BestPrediction     float64
	WorstPrediction    float64
}
