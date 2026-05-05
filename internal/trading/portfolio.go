// Package trading provides trading operations including portfolio analysis.
package trading

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

// DefaultPortfolioAnalyzer implements the PortfolioAnalyzer interface.
// Requirements: 51.1-51.8
type DefaultPortfolioAnalyzer struct {
	broker          broker.Broker
	positionManager *DefaultPositionManager
	mu              sync.RWMutex

	// Sector mappings (symbol -> sector)
	sectorMap map[string]string
}

// NewPortfolioAnalyzer creates a new portfolio analyzer.
func NewPortfolioAnalyzer(b broker.Broker, pm *DefaultPositionManager) *DefaultPortfolioAnalyzer {
	return &DefaultPortfolioAnalyzer{
		broker:          b,
		positionManager: pm,
		sectorMap:       make(map[string]string),
	}
}

// GetPortfolioSummary returns a consolidated portfolio view across all segments.
// Requirement 51.1: THE CLI SHALL display consolidated portfolio view across all segments
func (pa *DefaultPortfolioAnalyzer) GetPortfolioSummary(ctx context.Context) (*PortfolioSummary, error) {
	// Get positions
	positions, err := pa.positionManager.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching positions: %w", err)
	}

	// Get holdings
	holdings, err := pa.broker.GetHoldings(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching holdings: %w", err)
	}

	summary := &PortfolioSummary{}

	// Calculate position values
	var positionInvested, positionCurrent float64
	for _, pos := range positions {
		if pos.Quantity == 0 {
			continue
		}
		summary.PositionCount++
		qty := pos.Quantity
		if qty < 0 {
			qty = -qty
		}
		invested := pos.AveragePrice * float64(qty)
		current := pos.LTP * float64(qty)
		positionInvested += invested
		positionCurrent += current
		summary.DayPnL += pos.PnL
	}

	// Calculate holding values
	for _, hold := range holdings {
		if hold.Quantity == 0 {
			continue
		}
		summary.HoldingCount++
		summary.InvestedValue += hold.InvestedValue
		summary.CurrentValue += hold.CurrentValue
		summary.TotalPnL += hold.PnL
	}

	// Add position values
	summary.InvestedValue += positionInvested
	summary.CurrentValue += positionCurrent
	summary.TotalPnL += summary.DayPnL

	// Calculate total value
	summary.TotalValue = summary.CurrentValue

	// Calculate percentages
	if summary.InvestedValue > 0 {
		summary.TotalPnLPercent = (summary.TotalPnL / summary.InvestedValue) * 100
		summary.DayPnLPercent = (summary.DayPnL / summary.InvestedValue) * 100
	}

	return summary, nil
}

// GetSectorExposure returns sector-wise exposure breakdown.
// Requirement 51.4: THE CLI SHALL display sector-wise exposure breakdown
func (pa *DefaultPortfolioAnalyzer) GetSectorExposure(ctx context.Context) (map[string]float64, error) {
	positions, err := pa.positionManager.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching positions: %w", err)
	}

	holdings, err := pa.broker.GetHoldings(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching holdings: %w", err)
	}

	sectorValues := make(map[string]float64)
	var totalValue float64

	// Add position values by sector
	for _, pos := range positions {
		if pos.Quantity == 0 {
			continue
		}
		sector := pa.getSector(pos.Symbol)
		value := pos.Value
		if value < 0 {
			value = -value
		}
		sectorValues[sector] += value
		totalValue += value
	}

	// Add holding values by sector
	for _, hold := range holdings {
		if hold.Quantity == 0 {
			continue
		}
		sector := pa.getSector(hold.Symbol)
		sectorValues[sector] += hold.CurrentValue
		totalValue += hold.CurrentValue
	}

	// Convert to percentages
	exposure := make(map[string]float64)
	if totalValue > 0 {
		for sector, value := range sectorValues {
			exposure[sector] = (value / totalValue) * 100
		}
	}

	return exposure, nil
}

// GetPortfolioGreeks returns portfolio-level Greeks only when data-backed inputs are available.
func (pa *DefaultPortfolioAnalyzer) GetPortfolioGreeks(ctx context.Context) (*PortfolioGreeks, error) {
	return nil, fmt.Errorf("portfolio Greeks require data-backed option Greeks; this workflow is disabled until broker Greeks or validated option-chain inputs are wired")
}

// GetPortfolioBeta returns portfolio beta only when historical return data is available.
func (pa *DefaultPortfolioAnalyzer) GetPortfolioBeta(ctx context.Context) (float64, error) {
	return 0, fmt.Errorf("portfolio beta requires historical portfolio and benchmark returns; this workflow is disabled until data-backed beta is implemented")
}

// GetVaR returns Value at Risk only when historical return data is available.
func (pa *DefaultPortfolioAnalyzer) GetVaR(ctx context.Context, confidence float64) (float64, error) {
	if confidence <= 0 || confidence >= 1 {
		return 0, fmt.Errorf("confidence must be between 0 and 1")
	}
	return 0, fmt.Errorf("portfolio VaR requires historical portfolio returns; this workflow is disabled until data-backed VaR is implemented")
}

// SuggestHedges returns hedge ideas only when beta, volatility, and instrument inputs are data-backed.
func (pa *DefaultPortfolioAnalyzer) SuggestHedges(ctx context.Context) ([]HedgeSuggestion, error) {
	return nil, fmt.Errorf("hedge suggestions require data-backed beta, volatility, and hedge instrument pricing; this workflow is disabled until those inputs are implemented")
}

// SetSectorMapping sets the sector mapping for a symbol.
func (pa *DefaultPortfolioAnalyzer) SetSectorMapping(symbol, sector string) {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	pa.sectorMap[symbol] = sector
}

// getSector returns the sector for a symbol.
func (pa *DefaultPortfolioAnalyzer) getSector(symbol string) string {
	pa.mu.RLock()
	defer pa.mu.RUnlock()
	if sector, ok := pa.sectorMap[symbol]; ok {
		return sector
	}
	return "Unknown"
}

// LoadSectorMappings loads sector mappings from a map.
func (pa *DefaultPortfolioAnalyzer) LoadSectorMappings(mappings map[string]string) {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	for symbol, sector := range mappings {
		pa.sectorMap[symbol] = sector
	}
}

// SegmentBreakdown represents portfolio breakdown by segment.
type SegmentBreakdown struct {
	Segment    string
	Value      float64
	Percentage float64
	PnL        float64
	Positions  int
}

// GetSegmentBreakdown returns portfolio breakdown by exchange segment.
func (pa *DefaultPortfolioAnalyzer) GetSegmentBreakdown(ctx context.Context) ([]SegmentBreakdown, error) {
	positions, err := pa.positionManager.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching positions: %w", err)
	}

	segmentData := make(map[models.Exchange]*SegmentBreakdown)
	var totalValue float64

	for _, pos := range positions {
		if pos.Quantity == 0 {
			continue
		}

		if _, ok := segmentData[pos.Exchange]; !ok {
			segmentData[pos.Exchange] = &SegmentBreakdown{
				Segment: string(pos.Exchange),
			}
		}

		value := pos.Value
		if value < 0 {
			value = -value
		}

		segmentData[pos.Exchange].Value += value
		segmentData[pos.Exchange].PnL += pos.PnL
		segmentData[pos.Exchange].Positions++
		totalValue += value
	}

	// Convert to slice and calculate percentages
	var breakdown []SegmentBreakdown
	for _, data := range segmentData {
		if totalValue > 0 {
			data.Percentage = (data.Value / totalValue) * 100
		}
		breakdown = append(breakdown, *data)
	}

	// Sort by value descending
	sort.Slice(breakdown, func(i, j int) bool {
		return breakdown[i].Value > breakdown[j].Value
	})

	return breakdown, nil
}

// GetMarginUtilization returns margin utilization across segments.
func (pa *DefaultPortfolioAnalyzer) GetMarginUtilization(ctx context.Context) ([]MarginUtilization, error) {
	margins, err := pa.broker.GetMargins(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching margins: %w", err)
	}

	var utilization []MarginUtilization

	// Equity segment
	if margins.Equity.Total > 0 {
		utilization = append(utilization, MarginUtilization{
			Segment:         "Equity",
			AvailableMargin: margins.Equity.Available,
			UsedMargin:      margins.Equity.Used,
			TotalMargin:     margins.Equity.Total,
			Utilization:     (margins.Equity.Used / margins.Equity.Total) * 100,
		})
	}

	// Commodity segment
	if margins.Commodity.Total > 0 {
		utilization = append(utilization, MarginUtilization{
			Segment:         "Commodity",
			AvailableMargin: margins.Commodity.Available,
			UsedMargin:      margins.Commodity.Used,
			TotalMargin:     margins.Commodity.Total,
			Utilization:     (margins.Commodity.Used / margins.Commodity.Total) * 100,
		})
	}

	return utilization, nil
}

// RiskMetrics represents portfolio risk metrics.
// Requirement 51.8: THE Risk_Agent SHALL monitor portfolio-level risk metrics
type RiskMetrics struct {
	Beta              float64
	VaR95             float64
	VaR99             float64
	MaxDrawdown       float64
	SharpeRatio       float64
	ConcentrationRisk float64 // Highest single position %
	SectorRisk        float64 // Highest sector exposure %
}

// GetRiskMetrics returns comprehensive portfolio risk metrics.
func (pa *DefaultPortfolioAnalyzer) GetRiskMetrics(ctx context.Context) (*RiskMetrics, error) {
	metrics := &RiskMetrics{}

	beta, err := pa.GetPortfolioBeta(ctx)
	if err != nil {
		return nil, fmt.Errorf("calculating beta: %w", err)
	}
	metrics.Beta = beta

	// Get VaR at different confidence levels
	var95, err := pa.GetVaR(ctx, 0.95)
	if err != nil {
		return nil, fmt.Errorf("calculating VaR 95: %w", err)
	}
	metrics.VaR95 = var95

	var99, err := pa.GetVaR(ctx, 0.99)
	if err != nil {
		return nil, fmt.Errorf("calculating VaR 99: %w", err)
	}
	metrics.VaR99 = var99

	// Get sector exposure for concentration risk
	sectorExposure, err := pa.GetSectorExposure(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting sector exposure: %w", err)
	}

	// Find highest sector exposure
	for _, exposure := range sectorExposure {
		if exposure > metrics.SectorRisk {
			metrics.SectorRisk = exposure
		}
	}

	// Calculate concentration risk (highest single position)
	positions, err := pa.positionManager.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching positions: %w", err)
	}

	summary, err := pa.GetPortfolioSummary(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting portfolio summary: %w", err)
	}

	if summary.TotalValue > 0 {
		for _, pos := range positions {
			if pos.Quantity == 0 {
				continue
			}
			value := math.Abs(pos.Value)
			concentration := (value / summary.TotalValue) * 100
			if concentration > metrics.ConcentrationRisk {
				metrics.ConcentrationRisk = concentration
			}
		}
	}

	return metrics, nil
}
