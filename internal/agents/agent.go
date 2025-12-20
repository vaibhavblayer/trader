// Package agents provides AI agent interfaces and implementations for trading decisions.
package agents

import (
	"context"
	"fmt"
	"time"

	"zerodha-trader/internal/analysis"
	"zerodha-trader/internal/models"
)

// Agent defines the interface for AI trading agents.
// Requirements: 11.1, 14.1-14.2
type Agent interface {
	// Name returns the unique name of the agent.
	Name() string
	// Analyze performs analysis and returns a trading recommendation.
	Analyze(ctx context.Context, req AnalysisRequest) (*AnalysisResult, error)
	// Weight returns the agent's weight for consensus calculation (0-1).
	Weight() float64
}

// AnalysisRequest contains all data needed for agent analysis.
type AnalysisRequest struct {
	Symbol           string
	Candles          map[string][]models.Candle // timeframe -> candles
	Indicators       map[string][]float64
	MultiIndicators  map[string]map[string][]float64
	Patterns         []analysis.Pattern
	Levels           *LevelData
	News             []NewsItem
	Research         *ResearchReport
	Portfolio        *PortfolioState
	MarketState      *MarketState
	SignalScore      *analysis.SignalScore
	CurrentPrice     float64
}

// LevelData contains support and resistance level information.
type LevelData struct {
	NearestSupport    float64
	NearestResistance float64
	SupportStrength   int
	ResistanceStrength int
	SupplyZones       []PriceZone
	DemandZones       []PriceZone
}

// PriceZone represents a supply or demand zone.
type PriceZone struct {
	Upper    float64
	Lower    float64
	Strength int
}

// AnalysisResult contains the agent's analysis output.
// All fields are required for a valid result (Property 6).
type AnalysisResult struct {
	AgentName      string
	Recommendation Recommendation
	Confidence     float64 // 0-100
	Reasoning      string
	EntryPrice     float64
	StopLoss       float64
	Targets        []float64
	RiskReward     float64
	Timestamp      time.Time
}

// Validate checks if the AnalysisResult contains all required fields.
func (r *AnalysisResult) Validate() error {
	if r.AgentName == "" {
		return fmt.Errorf("agent name is required")
	}
	if r.Recommendation == "" {
		return fmt.Errorf("recommendation is required")
	}
	if r.Confidence < 0 || r.Confidence > 100 {
		return fmt.Errorf("confidence must be between 0 and 100, got %f", r.Confidence)
	}
	if r.Reasoning == "" {
		return fmt.Errorf("reasoning is required")
	}
	if r.Timestamp.IsZero() {
		return fmt.Errorf("timestamp is required")
	}
	// Entry, SL, Targets are only required for BUY/SELL recommendations
	if r.Recommendation != Hold {
		if r.EntryPrice <= 0 {
			return fmt.Errorf("entry price is required for %s recommendation", r.Recommendation)
		}
		if r.StopLoss <= 0 {
			return fmt.Errorf("stop loss is required for %s recommendation", r.Recommendation)
		}
		if len(r.Targets) == 0 {
			return fmt.Errorf("at least one target is required for %s recommendation", r.Recommendation)
		}
		if r.RiskReward <= 0 {
			return fmt.Errorf("risk reward ratio is required for %s recommendation", r.Recommendation)
		}
	}
	return nil
}

// Recommendation represents a trading recommendation.
type Recommendation string

const (
	Buy  Recommendation = "BUY"
	Sell Recommendation = "SELL"
	Hold Recommendation = "HOLD"
)

// NewsItem represents a news article.
type NewsItem struct {
	Title       string
	Source      string
	URL         string
	Summary     string
	Sentiment   float64 // -1 to 1
	Relevance   float64 // 0 to 1
	PublishedAt time.Time
	Timestamp   time.Time
}

// ResearchReport contains fundamental research data.
type ResearchReport struct {
	Symbol          string
	CompanyName     string
	Sector          string
	Industry        string
	MarketCap       float64
	PE              float64
	PB              float64
	ROE             float64
	DebtToEquity    float64
	DividendYield   float64
	RevenueGrowth   float64
	ProfitGrowth    float64
	AnalystRating   string
	PriceTargets    []float64
	AvgPriceTarget  float64
	KeyHighlights   []string
	Risks           []string
	Catalysts       []string
	LastUpdated     time.Time
}

// PortfolioState represents the current portfolio state.
type PortfolioState struct {
	TotalValue         float64
	AvailableCash      float64
	UsedMargin         float64
	Positions          []models.Position
	Holdings           []models.Holding
	DailyPnL           float64
	DailyPnLPercent    float64
	SectorExposure     map[string]float64
	OpenPositionCount  int
	MaxPositionSize    float64
	RemainingDayTrades int
}

// MarketState represents the current market conditions.
type MarketState struct {
	NiftyLevel      float64
	NiftyChange     float64
	BankNiftyLevel  float64
	BankNiftyChange float64
	VIXLevel        float64
	VIXChange       float64
	MarketTrend     string // BULLISH, BEARISH, SIDEWAYS
	MarketRegime    MarketRegime
	Breadth         MarketBreadth
	Status          models.MarketStatus
	FIIData         *InstitutionalData
	DIIData         *InstitutionalData
}

// MarketRegime represents the current market regime.
type MarketRegime string

const (
	RegimeTrendingUp   MarketRegime = "TRENDING_UP"
	RegimeTrendingDown MarketRegime = "TRENDING_DOWN"
	RegimeRanging      MarketRegime = "RANGING"
	RegimeHighVolatility MarketRegime = "HIGH_VOLATILITY"
)

// MarketBreadth represents market breadth indicators.
type MarketBreadth struct {
	Advances     int
	Declines     int
	Unchanged    int
	NewHighs     int
	NewLows      int
	AdvDecRatio  float64
	PCR          float64 // Put-Call Ratio
}

// InstitutionalData represents FII/DII trading data.
type InstitutionalData struct {
	BuyValue  float64
	SellValue float64
	NetValue  float64
	Date      time.Time
}

// BaseAgent provides common functionality for all agents.
type BaseAgent struct {
	name   string
	weight float64
}

// NewBaseAgent creates a new base agent with the given name and weight.
func NewBaseAgent(name string, weight float64) BaseAgent {
	if weight < 0 {
		weight = 0
	}
	if weight > 1 {
		weight = 1
	}
	return BaseAgent{
		name:   name,
		weight: weight,
	}
}

// Name returns the agent's name.
func (b *BaseAgent) Name() string {
	return b.name
}

// Weight returns the agent's weight for consensus calculation.
func (b *BaseAgent) Weight() float64 {
	return b.weight
}

// CreateResult creates a new AnalysisResult with common fields populated.
func (b *BaseAgent) CreateResult(rec Recommendation, confidence float64, reasoning string) *AnalysisResult {
	return &AnalysisResult{
		AgentName:      b.name,
		Recommendation: rec,
		Confidence:     confidence,
		Reasoning:      reasoning,
		Timestamp:      time.Now(),
	}
}

// CalculateRiskReward calculates the risk-reward ratio for a trade.
func CalculateRiskReward(entry, stopLoss float64, targets []float64, isBuy bool) float64 {
	if len(targets) == 0 || entry <= 0 || stopLoss <= 0 {
		return 0
	}

	var risk, reward float64
	if isBuy {
		risk = entry - stopLoss
		reward = targets[0] - entry // Use first target for R:R
	} else {
		risk = stopLoss - entry
		reward = entry - targets[0]
	}

	if risk <= 0 {
		return 0
	}

	return reward / risk
}

// ClampConfidence ensures confidence is within valid range [0, 100].
func ClampConfidence(confidence float64) float64 {
	if confidence < 0 {
		return 0
	}
	if confidence > 100 {
		return 100
	}
	return confidence
}
