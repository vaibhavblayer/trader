// Package trading provides trading operations including prep mode for next-day planning.
package trading

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"zerodha-trader/internal/agents"
	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
	"zerodha-trader/internal/store"
)

// PrepMode handles next-day trade planning.
// Requirements: 32.1-32.10
type PrepMode struct {
	orchestrator *agents.Orchestrator
	broker       broker.Broker
	store        store.DataStore
}

// NewPrepMode creates a new prep mode handler.
func NewPrepMode(
	orchestrator *agents.Orchestrator,
	b broker.Broker,
	s store.DataStore,
) *PrepMode {
	return &PrepMode{
		orchestrator: orchestrator,
		broker:       b,
		store:        s,
	}
}

// PrepConfig holds configuration for prep mode.
type PrepConfig struct {
	Symbols          []string // Symbols to analyze
	MaxPlans         int      // Maximum number of plans to generate
	MinConfidence    float64  // Minimum confidence threshold
	MinRiskReward    float64  // Minimum risk-reward ratio
	IncludeWatchlist bool     // Include watchlist symbols
	PlaceAMO         bool     // Place AMO orders for high-confidence plans
	AMOThreshold     float64  // Confidence threshold for AMO placement
}

// PrepResult holds the result of prep mode analysis.
type PrepResult struct {
	Plans           []PrepPlan
	Summary         *PrepSummary
	AMOOrdersPlaced []AMOOrder
	GeneratedAt     time.Time
	NextTradingDay  time.Time
}

// PrepPlan represents a trade plan for the next day.
// Requirement 32.4: THE prep mode SHALL identify: entry zones, stop-loss levels, target levels, position sizing
type PrepPlan struct {
	Symbol       string
	Side         models.OrderSide
	EntryZone    PriceZone
	StopLoss     float64
	Target1      float64
	Target2      float64
	Target3      float64
	Quantity     int
	RiskReward   float64
	Confidence   float64
	Reasoning    string
	Signals      []string
	Priority     int // 1 = highest
	Status       models.PlanStatus
	AgentResults map[string]*models.AgentResult
}

// PriceZone represents a price zone for entry.
type PriceZone struct {
	Low  float64
	High float64
	Ideal float64
}

// PrepSummary summarizes the prep mode results.
// Requirement 32.8: THE prep mode SHALL generate a summary report of all planned trades
type PrepSummary struct {
	TotalSymbolsAnalyzed int
	PlansGenerated       int
	BuyPlans             int
	SellPlans            int
	HighConfidencePlans  int
	AverageConfidence    float64
	AverageRiskReward    float64
	TopSectors           []string
	MarketOutlook        string
}

// AMOOrder represents an After Market Order placed during prep.
// Requirement 32.7: THE prep mode SHALL support AMO (After Market Order) placement for next-day execution
type AMOOrder struct {
	Symbol   string
	Side     models.OrderSide
	Type     models.OrderType
	Price    float64
	Quantity int
	OrderID  string
	Status   string
}

// Run executes prep mode analysis.
// Requirement 32.1: THE CLI SHALL support a prep command for creating next-day trade plans
// Requirement 32.2: THE prep mode SHALL analyze stocks and generate trade setups for the next trading session
func (pm *PrepMode) Run(ctx context.Context, config PrepConfig) (*PrepResult, error) {
	result := &PrepResult{
		Plans:           make([]PrepPlan, 0),
		AMOOrdersPlaced: make([]AMOOrder, 0),
		GeneratedAt:     time.Now(),
		NextTradingDay:  pm.getNextTradingDay(),
	}

	// Get symbols to analyze
	symbols := config.Symbols
	if config.IncludeWatchlist {
		watchlistSymbols, err := pm.getWatchlistSymbols(ctx)
		if err == nil {
			symbols = append(symbols, watchlistSymbols...)
		}
	}

	// Remove duplicates
	symbols = pm.uniqueSymbols(symbols)

	if len(symbols) == 0 {
		return nil, fmt.Errorf("no symbols to analyze")
	}

	// Analyze each symbol
	// Requirement 32.3: THE Agent_Orchestrator SHALL run comprehensive analysis (technical, fundamental, news) for prep mode
	var plans []PrepPlan
	for _, symbol := range symbols {
		plan, err := pm.analyzeSymbol(ctx, symbol, config)
		if err != nil {
			continue // Skip failed symbols
		}

		if plan != nil && plan.Confidence >= config.MinConfidence {
			plans = append(plans, *plan)
		}
	}

	// Sort by confidence and risk-reward
	// Requirement 32.9: THE Agent_Orchestrator SHALL prioritize plans by confidence level and risk-reward ratio
	sort.Slice(plans, func(i, j int) bool {
		// Primary sort by confidence
		if plans[i].Confidence != plans[j].Confidence {
			return plans[i].Confidence > plans[j].Confidence
		}
		// Secondary sort by risk-reward
		return plans[i].RiskReward > plans[j].RiskReward
	})

	// Assign priorities
	for i := range plans {
		plans[i].Priority = i + 1
	}

	// Limit number of plans
	if config.MaxPlans > 0 && len(plans) > config.MaxPlans {
		plans = plans[:config.MaxPlans]
	}

	// Save plans to store
	// Requirement 32.5: THE prep mode SHALL save plans to Data_Store with status "PENDING" for next-day execution
	for i := range plans {
		plans[i].Status = models.PlanPending
		if err := pm.savePlan(ctx, &plans[i]); err != nil {
			// Log error but continue
			continue
		}
	}

	result.Plans = plans

	// Place AMO orders for high-confidence plans
	if config.PlaceAMO {
		result.AMOOrdersPlaced = pm.placeAMOOrders(ctx, plans, config.AMOThreshold)
	}

	// Generate summary
	result.Summary = pm.generateSummary(plans, len(symbols))

	return result, nil
}

// analyzeSymbol analyzes a single symbol and generates a trade plan.
func (pm *PrepMode) analyzeSymbol(ctx context.Context, symbol string, config PrepConfig) (*PrepPlan, error) {
	// Get current quote
	quote, err := pm.broker.GetQuote(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("getting quote: %w", err)
	}

	// Get historical data
	endDate := time.Now()
	startDate := endDate.AddDate(0, -3, 0) // 3 months of data

	candles, err := pm.store.GetCandles(ctx, symbol, "1day", startDate, endDate)
	if err != nil || len(candles) < 20 {
		return nil, fmt.Errorf("insufficient historical data")
	}

	// Build analysis request
	req := agents.AnalysisRequest{
		Symbol:       symbol,
		CurrentPrice: quote.LTP,
		Candles:      map[string][]models.Candle{"1day": candles},
	}

	// Run through orchestrator
	decision, err := pm.orchestrator.ProcessSymbol(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("processing symbol: %w", err)
	}

	// Skip HOLD decisions
	if decision.Action == "HOLD" {
		return nil, nil
	}

	// Build trade plan
	plan := &PrepPlan{
		Symbol:       symbol,
		Confidence:   decision.Confidence,
		Reasoning:    decision.Reasoning,
		AgentResults: decision.AgentResults,
		Signals:      make([]string, 0),
	}

	// Set side
	if decision.Action == "BUY" {
		plan.Side = models.OrderSideBuy
	} else {
		plan.Side = models.OrderSideSell
	}

	// Extract entry, SL, targets from agent results
	pm.extractLevels(plan, decision)

	// Calculate risk-reward
	if plan.StopLoss > 0 && plan.Target1 > 0 {
		if plan.Side == models.OrderSideBuy {
			risk := plan.EntryZone.Ideal - plan.StopLoss
			reward := plan.Target1 - plan.EntryZone.Ideal
			if risk > 0 {
				plan.RiskReward = reward / risk
			}
		} else {
			risk := plan.StopLoss - plan.EntryZone.Ideal
			reward := plan.EntryZone.Ideal - plan.Target1
			if risk > 0 {
				plan.RiskReward = reward / risk
			}
		}
	}

	// Check minimum risk-reward
	if config.MinRiskReward > 0 && plan.RiskReward < config.MinRiskReward {
		return nil, nil
	}

	// Extract signals from agent results
	for name, result := range decision.AgentResults {
		if result != nil && result.Recommendation != "" {
			plan.Signals = append(plan.Signals, fmt.Sprintf("%s: %s", name, result.Recommendation))
		}
	}

	return plan, nil
}

// extractLevels extracts entry, SL, and target levels from decision.
func (pm *PrepMode) extractLevels(plan *PrepPlan, decision *models.Decision) {
	// Default to current price for entry
	plan.EntryZone = PriceZone{
		Low:   0,
		High:  0,
		Ideal: 0,
	}

	// Extract from agent results
	for _, result := range decision.AgentResults {
		if result == nil {
			continue
		}

		// Entry price
		if result.EntryPrice > 0 {
			if plan.EntryZone.Ideal == 0 {
				plan.EntryZone.Ideal = result.EntryPrice
				plan.EntryZone.Low = result.EntryPrice * 0.99  // 1% below
				plan.EntryZone.High = result.EntryPrice * 1.01 // 1% above
			}
		}

		// Stop loss
		if result.StopLoss > 0 && plan.StopLoss == 0 {
			plan.StopLoss = result.StopLoss
		}

		// Targets
		if len(result.Targets) > 0 {
			if plan.Target1 == 0 && len(result.Targets) >= 1 {
				plan.Target1 = result.Targets[0]
			}
			if plan.Target2 == 0 && len(result.Targets) >= 2 {
				plan.Target2 = result.Targets[1]
			}
			if plan.Target3 == 0 && len(result.Targets) >= 3 {
				plan.Target3 = result.Targets[2]
			}
		}
	}
}

// savePlan saves a prep plan to the data store.
func (pm *PrepMode) savePlan(ctx context.Context, plan *PrepPlan) error {
	tradePlan := &models.TradePlan{
		ID:         fmt.Sprintf("PREP_%s_%d", plan.Symbol, time.Now().UnixNano()),
		Symbol:     plan.Symbol,
		Side:       plan.Side,
		EntryPrice: plan.EntryZone.Ideal,
		StopLoss:   plan.StopLoss,
		Target1:    plan.Target1,
		Target2:    plan.Target2,
		Target3:    plan.Target3,
		Quantity:   plan.Quantity,
		RiskReward: plan.RiskReward,
		Status:     plan.Status,
		Notes:      strings.Join(plan.Signals, "; "),
		Reasoning:  plan.Reasoning,
		Source:     "prep",
		CreatedAt:  time.Now(),
	}

	return pm.store.SavePlan(ctx, tradePlan)
}

// placeAMOOrders places AMO orders for high-confidence plans.
func (pm *PrepMode) placeAMOOrders(ctx context.Context, plans []PrepPlan, threshold float64) []AMOOrder {
	var orders []AMOOrder

	for _, plan := range plans {
		if plan.Confidence < threshold {
			continue
		}

		order := &models.Order{
			Symbol:   plan.Symbol,
			Exchange: models.NSE, // Default to NSE
			Side:     plan.Side,
			Type:     models.OrderTypeLimit,
			Product:  models.ProductMIS,
			Quantity: plan.Quantity,
			Price:    plan.EntryZone.Ideal,
			Validity: "DAY",
			Tag:      "prep_amo",
		}

		result, err := pm.broker.PlaceOrder(ctx, order)
		if err != nil {
			continue
		}

		orders = append(orders, AMOOrder{
			Symbol:   plan.Symbol,
			Side:     plan.Side,
			Type:     models.OrderTypeLimit,
			Price:    plan.EntryZone.Ideal,
			Quantity: plan.Quantity,
			OrderID:  result.OrderID,
			Status:   result.Status,
		})
	}

	return orders
}

// generateSummary generates a summary of prep results.
func (pm *PrepMode) generateSummary(plans []PrepPlan, totalAnalyzed int) *PrepSummary {
	summary := &PrepSummary{
		TotalSymbolsAnalyzed: totalAnalyzed,
		PlansGenerated:       len(plans),
	}

	if len(plans) == 0 {
		return summary
	}

	var totalConfidence, totalRR float64

	for _, plan := range plans {
		if plan.Side == models.OrderSideBuy {
			summary.BuyPlans++
		} else {
			summary.SellPlans++
		}

		if plan.Confidence >= 80 {
			summary.HighConfidencePlans++
		}

		totalConfidence += plan.Confidence
		totalRR += plan.RiskReward
	}

	summary.AverageConfidence = totalConfidence / float64(len(plans))
	summary.AverageRiskReward = totalRR / float64(len(plans))

	// Determine market outlook based on buy/sell ratio
	if summary.BuyPlans > summary.SellPlans*2 {
		summary.MarketOutlook = "Bullish"
	} else if summary.SellPlans > summary.BuyPlans*2 {
		summary.MarketOutlook = "Bearish"
	} else {
		summary.MarketOutlook = "Neutral"
	}

	return summary
}

// getWatchlistSymbols gets symbols from the default watchlist.
func (pm *PrepMode) getWatchlistSymbols(ctx context.Context) ([]string, error) {
	return pm.store.GetWatchlist(ctx, "default")
}

// uniqueSymbols removes duplicate symbols.
func (pm *PrepMode) uniqueSymbols(symbols []string) []string {
	seen := make(map[string]bool)
	var unique []string

	for _, s := range symbols {
		if !seen[s] {
			seen[s] = true
			unique = append(unique, s)
		}
	}

	return unique
}

// getNextTradingDay returns the next trading day.
func (pm *PrepMode) getNextTradingDay() time.Time {
	now := time.Now()
	next := now.AddDate(0, 0, 1)

	// Skip weekends
	for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday {
		next = next.AddDate(0, 0, 1)
	}

	// Set to market open time (9:15 AM IST)
	loc, _ := time.LoadLocation("Asia/Kolkata")
	return time.Date(next.Year(), next.Month(), next.Day(), 9, 15, 0, 0, loc)
}

// GetPendingPlans returns all pending plans for the next trading day.
// Requirement 32.6: WHEN market opens, THE CLI SHALL display pending plans with current prices and distances
func (pm *PrepMode) GetPendingPlans(ctx context.Context) ([]PrepPlanWithDistance, error) {
	filter := store.PlanFilter{
		Status: models.PlanPending,
		Source: "prep",
	}

	plans, err := pm.store.GetPlans(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("fetching plans: %w", err)
	}

	var result []PrepPlanWithDistance

	for _, plan := range plans {
		// Get current price
		quote, err := pm.broker.GetQuote(ctx, plan.Symbol)
		if err != nil {
			continue
		}

		pwd := PrepPlanWithDistance{
			Plan:         plan,
			CurrentPrice: quote.LTP,
		}

		// Calculate distances
		if plan.EntryPrice > 0 {
			pwd.EntryDistance = ((quote.LTP - plan.EntryPrice) / plan.EntryPrice) * 100
		}
		if plan.StopLoss > 0 {
			pwd.SLDistance = ((quote.LTP - plan.StopLoss) / plan.StopLoss) * 100
		}
		if plan.Target1 > 0 {
			pwd.Target1Distance = ((plan.Target1 - quote.LTP) / quote.LTP) * 100
		}

		result = append(result, pwd)
	}

	return result, nil
}

// PrepPlanWithDistance represents a plan with current price distances.
type PrepPlanWithDistance struct {
	Plan            models.TradePlan
	CurrentPrice    float64
	EntryDistance   float64 // % distance from entry
	SLDistance      float64 // % distance from SL
	Target1Distance float64 // % distance to target
}

// ReviewPlan allows reviewing and modifying a plan before market open.
// Requirement 32.10: THE prep mode SHALL support reviewing and modifying plans before market open
func (pm *PrepMode) ReviewPlan(ctx context.Context, planID string, updates *PlanUpdates) error {
	filter := store.PlanFilter{}
	plans, err := pm.store.GetPlans(ctx, filter)
	if err != nil {
		return fmt.Errorf("fetching plans: %w", err)
	}

	var targetPlan *models.TradePlan
	for i := range plans {
		if plans[i].ID == planID {
			targetPlan = &plans[i]
			break
		}
	}

	if targetPlan == nil {
		return fmt.Errorf("plan not found: %s", planID)
	}

	// Apply updates
	if updates.EntryPrice > 0 {
		targetPlan.EntryPrice = updates.EntryPrice
	}
	if updates.StopLoss > 0 {
		targetPlan.StopLoss = updates.StopLoss
	}
	if updates.Target1 > 0 {
		targetPlan.Target1 = updates.Target1
	}
	if updates.Target2 > 0 {
		targetPlan.Target2 = updates.Target2
	}
	if updates.Target3 > 0 {
		targetPlan.Target3 = updates.Target3
	}
	if updates.Quantity > 0 {
		targetPlan.Quantity = updates.Quantity
	}
	if updates.Notes != "" {
		targetPlan.Notes = updates.Notes
	}

	// Recalculate risk-reward
	if targetPlan.StopLoss > 0 && targetPlan.Target1 > 0 {
		if targetPlan.Side == models.OrderSideBuy {
			risk := targetPlan.EntryPrice - targetPlan.StopLoss
			reward := targetPlan.Target1 - targetPlan.EntryPrice
			if risk > 0 {
				targetPlan.RiskReward = reward / risk
			}
		} else {
			risk := targetPlan.StopLoss - targetPlan.EntryPrice
			reward := targetPlan.EntryPrice - targetPlan.Target1
			if risk > 0 {
				targetPlan.RiskReward = reward / risk
			}
		}
	}

	return pm.store.SavePlan(ctx, targetPlan)
}

// PlanUpdates represents updates to a trade plan.
type PlanUpdates struct {
	EntryPrice float64
	StopLoss   float64
	Target1    float64
	Target2    float64
	Target3    float64
	Quantity   int
	Notes      string
}

// CancelPlan cancels a pending plan.
func (pm *PrepMode) CancelPlan(ctx context.Context, planID string) error {
	return pm.store.UpdatePlanStatus(ctx, planID, models.PlanCancelled)
}

// ExecutePlan marks a plan as executed.
func (pm *PrepMode) ExecutePlan(ctx context.Context, planID string) error {
	return pm.store.UpdatePlanStatus(ctx, planID, models.PlanExecuted)
}

// GenerateReport generates a formatted report of prep results.
func (pm *PrepMode) GenerateReport(result *PrepResult) string {
	var sb strings.Builder

	sb.WriteString("═══════════════════════════════════════════════════════════════\n")
	sb.WriteString("                    PREP MODE REPORT                           \n")
	sb.WriteString("═══════════════════════════════════════════════════════════════\n\n")

	sb.WriteString(fmt.Sprintf("Generated: %s\n", result.GeneratedAt.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("Next Trading Day: %s\n\n", result.NextTradingDay.Format("2006-01-02")))

	// Summary
	if result.Summary != nil {
		sb.WriteString("SUMMARY\n")
		sb.WriteString("───────────────────────────────────────────────────────────────\n")
		sb.WriteString(fmt.Sprintf("Symbols Analyzed: %d\n", result.Summary.TotalSymbolsAnalyzed))
		sb.WriteString(fmt.Sprintf("Plans Generated: %d\n", result.Summary.PlansGenerated))
		sb.WriteString(fmt.Sprintf("Buy Plans: %d | Sell Plans: %d\n", result.Summary.BuyPlans, result.Summary.SellPlans))
		sb.WriteString(fmt.Sprintf("High Confidence (≥80%%): %d\n", result.Summary.HighConfidencePlans))
		sb.WriteString(fmt.Sprintf("Avg Confidence: %.1f%% | Avg R:R: %.2f\n", result.Summary.AverageConfidence, result.Summary.AverageRiskReward))
		sb.WriteString(fmt.Sprintf("Market Outlook: %s\n\n", result.Summary.MarketOutlook))
	}

	// Plans
	if len(result.Plans) > 0 {
		sb.WriteString("TRADE PLANS\n")
		sb.WriteString("───────────────────────────────────────────────────────────────\n")

		for _, plan := range result.Plans {
			sb.WriteString(fmt.Sprintf("\n#%d %s - %s (Confidence: %.1f%%)\n",
				plan.Priority, plan.Symbol, plan.Side, plan.Confidence))
			sb.WriteString(fmt.Sprintf("   Entry: %.2f (%.2f - %.2f)\n",
				plan.EntryZone.Ideal, plan.EntryZone.Low, plan.EntryZone.High))
			sb.WriteString(fmt.Sprintf("   SL: %.2f | T1: %.2f | T2: %.2f | T3: %.2f\n",
				plan.StopLoss, plan.Target1, plan.Target2, plan.Target3))
			sb.WriteString(fmt.Sprintf("   R:R: %.2f\n", plan.RiskReward))
			if len(plan.Signals) > 0 {
				sb.WriteString(fmt.Sprintf("   Signals: %s\n", strings.Join(plan.Signals, ", ")))
			}
		}
		sb.WriteString("\n")
	}

	// AMO Orders
	if len(result.AMOOrdersPlaced) > 0 {
		sb.WriteString("AMO ORDERS PLACED\n")
		sb.WriteString("───────────────────────────────────────────────────────────────\n")

		for _, order := range result.AMOOrdersPlaced {
			sb.WriteString(fmt.Sprintf("   %s %s @ %.2f x %d (Order: %s)\n",
				order.Side, order.Symbol, order.Price, order.Quantity, order.OrderID))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("═══════════════════════════════════════════════════════════════\n")

	return sb.String()
}
