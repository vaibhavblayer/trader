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

	// Index data for beta calculation
	indexReturns map[string][]float64
}

// NewPortfolioAnalyzer creates a new portfolio analyzer.
func NewPortfolioAnalyzer(b broker.Broker, pm *DefaultPositionManager) *DefaultPortfolioAnalyzer {
	return &DefaultPortfolioAnalyzer{
		broker:          b,
		positionManager: pm,
		sectorMap:       make(map[string]string),
		indexReturns:    make(map[string][]float64),
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

// GetPortfolioGreeks calculates portfolio-level Greeks for options positions.
// Requirement 51.2: THE CLI SHALL calculate portfolio Greeks for options positions
func (pa *DefaultPortfolioAnalyzer) GetPortfolioGreeks(ctx context.Context) (*PortfolioGreeks, error) {
	positions, err := pa.positionManager.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching positions: %w", err)
	}

	greeks := &PortfolioGreeks{}

	for _, pos := range positions {
		// Only F&O positions have Greeks
		if pos.Exchange != models.NFO {
			continue
		}

		// Get option chain to fetch Greeks
		// Note: In a real implementation, we'd fetch Greeks from the broker
		// For now, we'll use placeholder logic
		optionGreeks := pa.estimateGreeks(pos)

		qty := float64(pos.Quantity)
		greeks.Delta += optionGreeks.Delta * qty
		greeks.Gamma += optionGreeks.Gamma * qty
		greeks.Theta += optionGreeks.Theta * qty
		greeks.Vega += optionGreeks.Vega * qty
	}

	return greeks, nil
}

// estimateGreeks estimates Greeks for a position (placeholder implementation).
func (pa *DefaultPortfolioAnalyzer) estimateGreeks(pos models.Position) models.OptionGreeks {
	// In a real implementation, this would fetch actual Greeks from the broker
	// or calculate them using Black-Scholes
	return models.OptionGreeks{
		Delta: 0.5,  // Placeholder
		Gamma: 0.05, // Placeholder
		Theta: -0.1, // Placeholder
		Vega:  0.2,  // Placeholder
	}
}

// GetPortfolioBeta calculates portfolio beta relative to an index.
// Requirement 51.3: THE CLI SHALL calculate portfolio beta and correlation with indices
func (pa *DefaultPortfolioAnalyzer) GetPortfolioBeta(ctx context.Context) (float64, error) {
	positions, err := pa.positionManager.GetPositions(ctx)
	if err != nil {
		return 0, fmt.Errorf("fetching positions: %w", err)
	}

	holdings, err := pa.broker.GetHoldings(ctx)
	if err != nil {
		return 0, fmt.Errorf("fetching holdings: %w", err)
	}

	// Calculate weighted average beta
	var totalValue float64
	var weightedBeta float64

	// Process positions
	for _, pos := range positions {
		if pos.Quantity == 0 {
			continue
		}
		value := pos.Value
		if value < 0 {
			value = -value
		}
		beta := pa.getStockBeta(pos.Symbol)
		weightedBeta += beta * value
		totalValue += value
	}

	// Process holdings
	for _, hold := range holdings {
		if hold.Quantity == 0 {
			continue
		}
		beta := pa.getStockBeta(hold.Symbol)
		weightedBeta += beta * hold.CurrentValue
		totalValue += hold.CurrentValue
	}

	if totalValue == 0 {
		return 1.0, nil // Default beta
	}

	return weightedBeta / totalValue, nil
}

// getStockBeta returns the beta for a stock (placeholder implementation).
func (pa *DefaultPortfolioAnalyzer) getStockBeta(symbol string) float64 {
	// In a real implementation, this would fetch historical beta
	// or calculate it from price data
	// For now, return a default beta of 1.0
	return 1.0
}

// GetVaR calculates Value at Risk for the portfolio.
// Requirement 51.5: THE CLI SHALL calculate portfolio VaR (Value at Risk)
func (pa *DefaultPortfolioAnalyzer) GetVaR(ctx context.Context, confidence float64) (float64, error) {
	if confidence <= 0 || confidence >= 1 {
		return 0, fmt.Errorf("confidence must be between 0 and 1")
	}

	summary, err := pa.GetPortfolioSummary(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting portfolio summary: %w", err)
	}

	// Get portfolio volatility (simplified calculation)
	volatility := pa.estimatePortfolioVolatility(ctx)

	// Calculate VaR using parametric method
	// VaR = Portfolio Value * Z-score * Volatility * sqrt(time horizon)
	// Assuming 1-day VaR
	zScore := pa.getZScore(confidence)
	var1Day := summary.TotalValue * zScore * volatility

	return var1Day, nil
}

// estimatePortfolioVolatility estimates portfolio volatility.
func (pa *DefaultPortfolioAnalyzer) estimatePortfolioVolatility(ctx context.Context) float64 {
	// Simplified: assume 2% daily volatility
	// In a real implementation, this would calculate from historical returns
	return 0.02
}

// getZScore returns the Z-score for a given confidence level.
func (pa *DefaultPortfolioAnalyzer) getZScore(confidence float64) float64 {
	// Common Z-scores
	switch {
	case confidence >= 0.99:
		return 2.326
	case confidence >= 0.95:
		return 1.645
	case confidence >= 0.90:
		return 1.282
	default:
		return 1.0
	}
}

// SuggestHedges suggests hedging opportunities for the portfolio.
// Requirement 51.7: THE CLI SHALL identify portfolio hedging opportunities
func (pa *DefaultPortfolioAnalyzer) SuggestHedges(ctx context.Context) ([]HedgeSuggestion, error) {
	var suggestions []HedgeSuggestion

	// Get portfolio summary
	summary, err := pa.GetPortfolioSummary(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting portfolio summary: %w", err)
	}

	// Get portfolio beta
	beta, err := pa.GetPortfolioBeta(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting portfolio beta: %w", err)
	}

	// Get sector exposure
	sectorExposure, err := pa.GetSectorExposure(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting sector exposure: %w", err)
	}

	// Suggest index hedge if portfolio is significantly long
	if summary.TotalValue > 100000 && beta > 0.5 {
		hedgeValue := summary.TotalValue * beta * 0.5 // Hedge 50% of beta exposure
		suggestions = append(suggestions, HedgeSuggestion{
			Type:         "index_put",
			Symbol:       "NIFTY",
			Action:       "BUY PUT",
			Quantity:     int(hedgeValue / 50), // Approximate lot value
			Reason:       fmt.Sprintf("Portfolio beta of %.2f suggests index hedge", beta),
			ExpectedCost: hedgeValue * 0.02, // Approximate premium
		})
	}

	// Suggest sector hedges for concentrated positions
	for sector, exposure := range sectorExposure {
		if exposure > 30 { // More than 30% in one sector
			suggestions = append(suggestions, HedgeSuggestion{
				Type:         "sector_diversification",
				Symbol:       sector,
				Action:       "REDUCE",
				Quantity:     0,
				Reason:       fmt.Sprintf("Sector %s has %.1f%% exposure, consider diversifying", sector, exposure),
				ExpectedCost: 0,
			})
		}
	}

	// Suggest VIX hedge during low volatility
	// In a real implementation, we'd check actual VIX levels
	suggestions = append(suggestions, HedgeSuggestion{
		Type:         "volatility_hedge",
		Symbol:       "INDIAVIX",
		Action:       "MONITOR",
		Quantity:     0,
		Reason:       "Consider VIX calls when volatility is low for tail risk protection",
		ExpectedCost: 0,
	})

	return suggestions, nil
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

	// Get beta
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
