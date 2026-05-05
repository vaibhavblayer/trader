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
	SetupName   string
	Timeframe   string
	Gates       []PaperPredictionGate

	// Outcome tracking
	Evaluated  bool
	ExitPrice  float64
	Outcome    string // RIGHT, WRONG, EXPIRED
	PnLPercent float64
}

// PaperPredictionGate records one setup gate state at prediction time.
type PaperPredictionGate struct {
	Name   string
	Passed bool
	Reason string
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

// PaperPredictionReport summarizes calibrated paper prediction performance.
type PaperPredictionReport struct {
	GeneratedAt        time.Time
	StartDate          time.Time
	EndDate            time.Time
	Symbol             string
	TotalPredictions   int
	ActivePredictions  int
	Evaluated          int
	Decisive           int
	RightPredictions   int
	WrongPredictions   int
	ExpiredPredictions int
	WinRate            float64
	AvgConfidence      float64
	AvgPnLPercent      float64
	Expectancy         float64
	BestPrediction     float64
	WorstPrediction    float64
	ExpiredRate        float64
	Overconfidence     []CalibrationWarning
	BySymbol           []PaperPredictionGroupStats
	ByAction           []PaperPredictionGroupStats
	ByConfidence       []PaperPredictionGroupStats
}

// PaperPredictionGroupStats summarizes prediction performance for one group.
type PaperPredictionGroupStats struct {
	Key                string
	TotalPredictions   int
	ActivePredictions  int
	Evaluated          int
	Decisive           int
	RightPredictions   int
	WrongPredictions   int
	ExpiredPredictions int
	WinRate            float64
	AvgConfidence      float64
	AvgPnLPercent      float64
	Expectancy         float64
	BestPrediction     float64
	WorstPrediction    float64
	ExpiredRate        float64
}

// CalibrationWarning flags confidence buckets that underperform their stated confidence.
type CalibrationWarning struct {
	Bucket        string
	AvgConfidence float64
	WinRate       float64
	Gap           float64
	SampleSize    int
}

// HistoricalCalibrationReport summarizes expectancy across setup dimensions.
type HistoricalCalibrationReport struct {
	GeneratedAt      time.Time
	StartDate        time.Time
	EndDate          time.Time
	Symbol           string
	SetupName        string
	Timeframe        string
	TotalPredictions int
	Evaluated        int
	Decisive         int
	WinRate          float64
	AvgConfidence    float64
	AvgPnLPercent    float64
	Expectancy       float64
	BySetup          []CalibrationGroupStats
	ByGate           []CalibrationGroupStats
	BySymbol         []CalibrationGroupStats
	ByTimeframe      []CalibrationGroupStats
	ByAction         []CalibrationGroupStats
}

// CalibrationGroupStats summarizes calibration for one setup grouping.
type CalibrationGroupStats struct {
	Key                string
	TotalPredictions   int
	Evaluated          int
	Decisive           int
	RightPredictions   int
	WrongPredictions   int
	ExpiredPredictions int
	WinRate            float64
	AvgConfidence      float64
	AvgPnLPercent      float64
	Expectancy         float64
}
