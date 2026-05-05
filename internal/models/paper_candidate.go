package models

import "time"

const (
	PaperCandidateStatusActive = "ACTIVE"
	PaperCandidateStatusPaused = "PAUSED"
)

// PaperCandidate is a promoted backtest setup eligible for controlled paper soak.
type PaperCandidate struct {
	ID                  string
	Status              string
	Symbol              string
	Exchange            string
	Strategy            string
	ParamVariant        string
	Parameters          string
	Timeframe           string
	Setup               string
	Source              string
	Verdict             string
	Reason              string
	Days                int
	Candles             int
	Trades              int
	ValidationTrades    int
	ReturnPct           float64
	TrainReturnPct      float64
	ValidationReturnPct float64
	WinRate             float64
	ProfitFactor        float64
	Expectancy          float64
	MaxDrawdownPct      float64
	SharpeRatio         float64
	StopLossPercent     float64
	TakeProfitPercent   float64
	TrailingStopPercent float64
	AllowShort          bool
	AllowedRegimes      []string
	BlockedRegimes      []string
	RegimeStats         []PaperCandidateRegimeStat
	PromotedAt          time.Time
	UpdatedAt           time.Time
}

// PaperCandidateRegimeStat records backtest expectancy for one entry regime.
type PaperCandidateRegimeStat struct {
	Regime      string  `json:"regime"`
	Trades      int     `json:"trades"`
	Wins        int     `json:"wins"`
	WinRate     float64 `json:"win_rate"`
	TotalPnL    float64 `json:"total_pnl"`
	Expectancy  float64 `json:"expectancy"`
	AvgHoldBars float64 `json:"avg_hold_bars"`
}

// PaperCandidateFilter filters promoted paper-soak candidates.
type PaperCandidateFilter struct {
	Symbol   string
	Strategy string
	Status   string
	Limit    int
}
