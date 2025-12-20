// Package agents provides AI agent implementations for trading decisions.
package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"zerodha-trader/internal/config"
	"zerodha-trader/internal/models"
)

// RiskAgent evaluates position sizing, portfolio exposure, and risk management.
// Requirements: 11.5, 27.1-27.9
type RiskAgent struct {
	BaseAgent
	config *config.RiskConfig
}

// NewRiskAgent creates a new risk management agent.
func NewRiskAgent(riskConfig *config.RiskConfig, weight float64) *RiskAgent {
	// Use defaults if config is nil
	if riskConfig == nil {
		riskConfig = &config.RiskConfig{
			MaxPositionPercent:     5.0,   // 5% max per position
			MaxSectorExposure:      25.0,  // 25% max per sector
			MaxConcurrentPositions: 10,
			MinRiskReward:          2.0,   // 1:2 minimum
			TrailingStopPercent:    2.0,
			DailyLossLimit:         5000,
			MaxSlippage:            0.5,
		}
	}

	return &RiskAgent{
		BaseAgent: NewBaseAgent("risk", weight),
		config:    riskConfig,
	}
}

// Analyze performs risk analysis and provides a recommendation.
func (a *RiskAgent) Analyze(ctx context.Context, req AnalysisRequest) (*AnalysisResult, error) {
	result := a.CreateResult(Hold, 50, "")

	// Perform risk checks
	riskCheck := a.CheckRisk(ctx, req)

	// Build reasoning from risk check
	var reasons []string
	if !riskCheck.Approved {
		reasons = append(reasons, riskCheck.Violations...)
		result.Recommendation = Hold
		result.Confidence = 30 // Low confidence when risk checks fail
	} else {
		reasons = append(reasons, "all risk checks passed")
		// Risk agent doesn't make directional calls, just validates
		result.Recommendation = Hold
		result.Confidence = 70
	}

	// Add position sizing recommendation
	if req.Portfolio != nil && req.CurrentPrice > 0 {
		suggestedSize := a.CalculatePositionSize(req)
		reasons = append(reasons, fmt.Sprintf("suggested position size: %.0f shares", suggestedSize))
	}

	// Add volatility assessment
	if req.MarketState != nil {
		volatilityAssessment := a.assessVolatility(req.MarketState)
		reasons = append(reasons, volatilityAssessment)
	}

	result.Reasoning = fmt.Sprintf("Risk assessment: %s.", strings.Join(reasons, "; "))
	result.Timestamp = time.Now()

	return result, nil
}

// RiskCheckResult contains the result of a risk check.
type RiskCheckResult struct {
	Approved           bool
	Violations         []string
	SuggestedSize      float64
	MaxAllowedSize     float64
	PortfolioImpact    float64
	SectorExposure     float64
	DailyLossRemaining float64
	RiskRewardRatio    float64
	VolatilityFactor   float64
}

// CheckRisk performs comprehensive risk checks for a potential trade.
func (a *RiskAgent) CheckRisk(ctx context.Context, req AnalysisRequest) *RiskCheckResult {
	result := &RiskCheckResult{
		Approved:   true,
		Violations: []string{},
	}

	if req.Portfolio == nil {
		result.Violations = append(result.Violations, "portfolio state not available")
		result.Approved = false
		return result
	}

	// Check 1: Maximum position size as percentage of portfolio
	if req.CurrentPrice > 0 && req.Portfolio.TotalValue > 0 {
		maxPositionValue := req.Portfolio.TotalValue * (a.config.MaxPositionPercent / 100)
		result.MaxAllowedSize = maxPositionValue / req.CurrentPrice

		// Calculate portfolio impact
		result.PortfolioImpact = (req.CurrentPrice * result.MaxAllowedSize) / req.Portfolio.TotalValue * 100
	}

	// Check 2: Maximum sector exposure
	if req.Research != nil && req.Research.Sector != "" {
		currentSectorExposure := req.Portfolio.SectorExposure[req.Research.Sector]
		result.SectorExposure = currentSectorExposure

		if currentSectorExposure >= a.config.MaxSectorExposure {
			result.Violations = append(result.Violations,
				fmt.Sprintf("sector exposure limit reached: %.1f%% (max: %.1f%%)",
					currentSectorExposure, a.config.MaxSectorExposure))
			result.Approved = false
		}
	}

	// Check 3: Maximum concurrent positions
	if req.Portfolio.OpenPositionCount >= a.config.MaxConcurrentPositions {
		result.Violations = append(result.Violations,
			fmt.Sprintf("max concurrent positions reached: %d (max: %d)",
				req.Portfolio.OpenPositionCount, a.config.MaxConcurrentPositions))
		result.Approved = false
	}

	// Check 4: Daily loss limit
	if req.Portfolio.DailyPnL < 0 {
		dailyLoss := -req.Portfolio.DailyPnL
		result.DailyLossRemaining = a.config.DailyLossLimit - dailyLoss

		if dailyLoss >= a.config.DailyLossLimit {
			result.Violations = append(result.Violations,
				fmt.Sprintf("daily loss limit reached: ₹%.2f (limit: ₹%.2f)",
					dailyLoss, a.config.DailyLossLimit))
			result.Approved = false
		} else if result.DailyLossRemaining < a.config.DailyLossLimit*0.2 {
			result.Violations = append(result.Violations,
				fmt.Sprintf("approaching daily loss limit: ₹%.2f remaining",
					result.DailyLossRemaining))
		}
	} else {
		result.DailyLossRemaining = a.config.DailyLossLimit
	}

	// Check 5: Available cash/margin
	if req.Portfolio.AvailableCash <= 0 {
		result.Violations = append(result.Violations, "insufficient available cash")
		result.Approved = false
	}

	// Check 6: Volatility-based position sizing
	if req.MarketState != nil {
		result.VolatilityFactor = a.calculateVolatilityFactor(req.MarketState)
		if result.VolatilityFactor < 1.0 {
			result.MaxAllowedSize *= result.VolatilityFactor
		}
	}

	// Calculate suggested position size
	result.SuggestedSize = a.CalculatePositionSize(req)

	return result
}

// CalculatePositionSize calculates the recommended position size.
func (a *RiskAgent) CalculatePositionSize(req AnalysisRequest) float64 {
	if req.Portfolio == nil || req.CurrentPrice <= 0 {
		return 0
	}

	// Base position size from max position percent
	maxPositionValue := req.Portfolio.TotalValue * (a.config.MaxPositionPercent / 100)
	baseSize := maxPositionValue / req.CurrentPrice

	// Adjust for volatility
	volatilityFactor := 1.0
	if req.MarketState != nil {
		volatilityFactor = a.calculateVolatilityFactor(req.MarketState)
	}

	// Adjust for available cash
	cashFactor := 1.0
	if req.Portfolio.AvailableCash < maxPositionValue {
		cashFactor = req.Portfolio.AvailableCash / maxPositionValue
	}

	// Adjust for daily loss remaining
	lossLimitFactor := 1.0
	if req.Portfolio.DailyPnL < 0 {
		dailyLoss := -req.Portfolio.DailyPnL
		remaining := a.config.DailyLossLimit - dailyLoss
		if remaining < a.config.DailyLossLimit*0.5 {
			lossLimitFactor = remaining / (a.config.DailyLossLimit * 0.5)
		}
	}

	// Apply all factors
	adjustedSize := baseSize * volatilityFactor * cashFactor * lossLimitFactor

	// Round down to whole shares
	return float64(int(adjustedSize))
}

// calculateVolatilityFactor returns a factor to reduce position size during high volatility.
func (a *RiskAgent) calculateVolatilityFactor(market *MarketState) float64 {
	if market == nil {
		return 1.0
	}

	// VIX-based adjustment
	// VIX < 15: Normal (factor = 1.0)
	// VIX 15-20: Slightly elevated (factor = 0.9)
	// VIX 20-25: Elevated (factor = 0.75)
	// VIX 25-30: High (factor = 0.5)
	// VIX > 30: Very high (factor = 0.25)
	vix := market.VIXLevel
	switch {
	case vix < 15:
		return 1.0
	case vix < 20:
		return 0.9
	case vix < 25:
		return 0.75
	case vix < 30:
		return 0.5
	default:
		return 0.25
	}
}

// assessVolatility returns a string assessment of current volatility.
func (a *RiskAgent) assessVolatility(market *MarketState) string {
	if market == nil {
		return "volatility data unavailable"
	}

	vix := market.VIXLevel
	switch {
	case vix < 15:
		return fmt.Sprintf("low volatility (VIX: %.1f)", vix)
	case vix < 20:
		return fmt.Sprintf("normal volatility (VIX: %.1f)", vix)
	case vix < 25:
		return fmt.Sprintf("elevated volatility (VIX: %.1f) - consider reduced position sizes", vix)
	case vix < 30:
		return fmt.Sprintf("high volatility (VIX: %.1f) - significantly reduce position sizes", vix)
	default:
		return fmt.Sprintf("extreme volatility (VIX: %.1f) - avoid new positions", vix)
	}
}

// ValidateRiskReward checks if a trade meets minimum risk-reward requirements.
func (a *RiskAgent) ValidateRiskReward(entry, stopLoss float64, targets []float64, isBuy bool) (bool, float64) {
	rr := CalculateRiskReward(entry, stopLoss, targets, isBuy)
	return rr >= a.config.MinRiskReward, rr
}

// CalculateTrailingStop calculates the trailing stop-loss price.
func (a *RiskAgent) CalculateTrailingStop(entryPrice, currentPrice float64, isBuy bool) float64 {
	trailPercent := a.config.TrailingStopPercent / 100

	if isBuy {
		// For long positions, trail below current price
		if currentPrice > entryPrice {
			return currentPrice * (1 - trailPercent)
		}
		return entryPrice * (1 - trailPercent)
	}

	// For short positions, trail above current price
	if currentPrice < entryPrice {
		return currentPrice * (1 + trailPercent)
	}
	return entryPrice * (1 + trailPercent)
}

// ShouldHaltTrading checks if trading should be halted based on conditions.
func (a *RiskAgent) ShouldHaltTrading(portfolio *PortfolioState, market *MarketState, consecutiveLosses int, maxConsecutiveLosses int) (bool, string) {
	if portfolio == nil {
		return true, "portfolio state unavailable"
	}

	// Check daily loss limit
	if portfolio.DailyPnL < 0 && -portfolio.DailyPnL >= a.config.DailyLossLimit {
		return true, fmt.Sprintf("daily loss limit reached: ₹%.2f", -portfolio.DailyPnL)
	}

	// Check consecutive losses
	if consecutiveLosses >= maxConsecutiveLosses {
		return true, fmt.Sprintf("consecutive loss limit reached: %d losses", consecutiveLosses)
	}

	// Check extreme volatility
	if market != nil && market.VIXLevel > 35 {
		return true, fmt.Sprintf("extreme market volatility: VIX %.1f", market.VIXLevel)
	}

	// Check market status
	if market != nil && market.Status == models.MarketClosed {
		return true, "market is closed"
	}

	return false, ""
}

// GetRiskMetrics returns current risk metrics for the portfolio.
func (a *RiskAgent) GetRiskMetrics(portfolio *PortfolioState) map[string]float64 {
	metrics := make(map[string]float64)

	if portfolio == nil {
		return metrics
	}

	// Portfolio utilization
	if portfolio.TotalValue > 0 {
		metrics["cash_utilization"] = (portfolio.TotalValue - portfolio.AvailableCash) / portfolio.TotalValue * 100
		metrics["margin_utilization"] = portfolio.UsedMargin / portfolio.TotalValue * 100
	}

	// Daily P&L metrics
	metrics["daily_pnl"] = portfolio.DailyPnL
	metrics["daily_pnl_percent"] = portfolio.DailyPnLPercent
	metrics["daily_loss_remaining"] = a.config.DailyLossLimit + portfolio.DailyPnL // DailyPnL is negative for losses

	// Position metrics
	metrics["open_positions"] = float64(portfolio.OpenPositionCount)
	metrics["max_positions"] = float64(a.config.MaxConcurrentPositions)
	metrics["position_utilization"] = float64(portfolio.OpenPositionCount) / float64(a.config.MaxConcurrentPositions) * 100

	// Sector exposure
	var maxSectorExposure float64
	for _, exposure := range portfolio.SectorExposure {
		if exposure > maxSectorExposure {
			maxSectorExposure = exposure
		}
	}
	metrics["max_sector_exposure"] = maxSectorExposure

	return metrics
}

// AdjustForVolatility adjusts trade parameters based on current volatility.
func (a *RiskAgent) AdjustForVolatility(entry, stopLoss float64, targets []float64, market *MarketState, isBuy bool) (float64, []float64) {
	if market == nil {
		return stopLoss, targets
	}

	factor := a.calculateVolatilityFactor(market)
	if factor >= 1.0 {
		return stopLoss, targets
	}

	// Widen stop-loss during high volatility
	var adjustedSL float64
	if isBuy {
		risk := entry - stopLoss
		adjustedSL = entry - (risk / factor) // Wider stop
	} else {
		risk := stopLoss - entry
		adjustedSL = entry + (risk / factor)
	}

	// Reduce targets during high volatility
	adjustedTargets := make([]float64, len(targets))
	for i, t := range targets {
		if isBuy {
			reward := t - entry
			adjustedTargets[i] = entry + (reward * factor)
		} else {
			reward := entry - t
			adjustedTargets[i] = entry - (reward * factor)
		}
	}

	return adjustedSL, adjustedTargets
}
