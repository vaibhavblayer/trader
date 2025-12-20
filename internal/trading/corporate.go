// Package trading provides trading operations and utilities.
package trading

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"zerodha-trader/internal/models"
	"zerodha-trader/internal/store"
)

// CorporateActionType represents types of corporate actions.
type CorporateActionType string

const (
	ActionDividend    CorporateActionType = "DIVIDEND"
	ActionBonus       CorporateActionType = "BONUS"
	ActionSplit       CorporateActionType = "SPLIT"
	ActionRightsIssue CorporateActionType = "RIGHTS"
	ActionBuyback     CorporateActionType = "BUYBACK"
	ActionMerger      CorporateActionType = "MERGER"
	ActionDemerger    CorporateActionType = "DEMERGER"
	ActionAGM         CorporateActionType = "AGM"
	ActionEGM         CorporateActionType = "EGM"
)

// CorporateAction represents a corporate action event.
type CorporateAction struct {
	ID           string
	Symbol       string
	ActionType   CorporateActionType
	ExDate       time.Time
	RecordDate   time.Time
	Description  string
	Ratio        string  // For bonus/split (e.g., "1:1", "2:1")
	Amount       float64 // For dividend
	OldFaceValue float64 // For split
	NewFaceValue float64 // For split
	Premium      float64 // For rights issue
	CreatedAt    time.Time
}

// CorporateActionsHandler manages corporate actions for Indian markets.
type CorporateActionsHandler struct {
	store   store.DataStore
	actions map[string][]CorporateAction // symbol -> actions
	mu      sync.RWMutex
}

// DividendInfo represents dividend information for a holding.
type DividendInfo struct {
	Symbol       string
	DividendRate float64
	ExDate       time.Time
	RecordDate   time.Time
	PaymentDate  time.Time
	Yield        float64
}

// AdjustmentFactor represents price adjustment factor for corporate actions.
type AdjustmentFactor struct {
	Symbol     string
	Date       time.Time
	Factor     float64
	ActionType CorporateActionType
	Reason     string
}

// NewCorporateActionsHandler creates a new corporate actions handler.
func NewCorporateActionsHandler(s store.DataStore) *CorporateActionsHandler {
	return &CorporateActionsHandler{
		store:   s,
		actions: make(map[string][]CorporateAction),
	}
}

// AddCorporateAction adds a corporate action.
func (h *CorporateActionsHandler) AddCorporateAction(ctx context.Context, action *CorporateAction) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if action.ID == "" {
		action.ID = fmt.Sprintf("%s-%s-%s", action.Symbol, action.ActionType, action.ExDate.Format("20060102"))
	}
	action.CreatedAt = time.Now()

	h.actions[action.Symbol] = append(h.actions[action.Symbol], *action)

	// Sort by ex-date
	sort.Slice(h.actions[action.Symbol], func(i, j int) bool {
		return h.actions[action.Symbol][i].ExDate.Before(h.actions[action.Symbol][j].ExDate)
	})

	// Save to store as event
	event := &models.CorporateEvent{
		ID:          action.ID,
		Symbol:      action.Symbol,
		EventType:   string(action.ActionType),
		Date:        action.ExDate,
		Description: action.Description,
	}
	return h.store.SaveEvent(ctx, event)
}

// GetUpcomingActions returns upcoming corporate actions for symbols.
func (h *CorporateActionsHandler) GetUpcomingActions(ctx context.Context, symbols []string, days int) ([]CorporateAction, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	now := time.Now()
	cutoff := now.AddDate(0, 0, days)

	var upcoming []CorporateAction
	for _, symbol := range symbols {
		actions, ok := h.actions[symbol]
		if !ok {
			continue
		}
		for _, a := range actions {
			if a.ExDate.After(now) && a.ExDate.Before(cutoff) {
				upcoming = append(upcoming, a)
			}
		}
	}

	// Sort by ex-date
	sort.Slice(upcoming, func(i, j int) bool {
		return upcoming[i].ExDate.Before(upcoming[j].ExDate)
	})

	return upcoming, nil
}

// GetActionsForSymbol returns all corporate actions for a symbol.
func (h *CorporateActionsHandler) GetActionsForSymbol(symbol string) []CorporateAction {
	h.mu.RLock()
	defer h.mu.RUnlock()

	actions, ok := h.actions[symbol]
	if !ok {
		return nil
	}

	// Return a copy
	result := make([]CorporateAction, len(actions))
	copy(result, actions)
	return result
}

// CalculateAdjustmentFactor calculates price adjustment factor for a corporate action.
func (h *CorporateActionsHandler) CalculateAdjustmentFactor(action *CorporateAction) *AdjustmentFactor {
	var factor float64
	var reason string

	switch action.ActionType {
	case ActionBonus:
		// Parse ratio like "1:1" (1 bonus for every 1 held)
		var bonus, held int
		fmt.Sscanf(action.Ratio, "%d:%d", &bonus, &held)
		if held > 0 {
			factor = float64(held) / float64(held+bonus)
			reason = fmt.Sprintf("Bonus %s", action.Ratio)
		}
	case ActionSplit:
		// Split adjusts price based on face value change
		if action.OldFaceValue > 0 && action.NewFaceValue > 0 {
			factor = action.NewFaceValue / action.OldFaceValue
			reason = fmt.Sprintf("Split from ₹%.0f to ₹%.0f", action.OldFaceValue, action.NewFaceValue)
		}
	case ActionRightsIssue:
		// Rights issue doesn't directly adjust historical prices
		factor = 1.0
		reason = "Rights issue - no price adjustment"
	default:
		factor = 1.0
		reason = "No adjustment required"
	}

	if factor == 0 {
		factor = 1.0
	}

	return &AdjustmentFactor{
		Symbol:     action.Symbol,
		Date:       action.ExDate,
		Factor:     factor,
		ActionType: action.ActionType,
		Reason:     reason,
	}
}

// AdjustCandles adjusts historical candle data for corporate actions.
func (h *CorporateActionsHandler) AdjustCandles(candles []models.Candle, actions []CorporateAction) []models.Candle {
	if len(candles) == 0 || len(actions) == 0 {
		return candles
	}

	// Sort actions by date descending (apply most recent first)
	sortedActions := make([]CorporateAction, len(actions))
	copy(sortedActions, actions)
	sort.Slice(sortedActions, func(i, j int) bool {
		return sortedActions[i].ExDate.After(sortedActions[j].ExDate)
	})

	adjusted := make([]models.Candle, len(candles))
	copy(adjusted, candles)

	for _, action := range sortedActions {
		factor := h.CalculateAdjustmentFactor(&action)
		if factor.Factor == 1.0 {
			continue
		}

		// Adjust candles before the ex-date
		for i := range adjusted {
			if adjusted[i].Timestamp.Before(action.ExDate) {
				adjusted[i].Open *= factor.Factor
				adjusted[i].High *= factor.Factor
				adjusted[i].Low *= factor.Factor
				adjusted[i].Close *= factor.Factor
				// Volume is adjusted inversely
				if factor.Factor > 0 {
					adjusted[i].Volume = int64(float64(adjusted[i].Volume) / factor.Factor)
				}
			}
		}
	}

	return adjusted
}

// AdjustCostBasis adjusts position cost basis after corporate action.
func (h *CorporateActionsHandler) AdjustCostBasis(position *models.Position, action *CorporateAction) (newQty int, newAvgPrice float64) {
	switch action.ActionType {
	case ActionBonus:
		var bonus, held int
		fmt.Sscanf(action.Ratio, "%d:%d", &bonus, &held)
		if held > 0 {
			bonusShares := (position.Quantity / held) * bonus
			newQty = position.Quantity + bonusShares
			// Cost basis remains same, but per-share cost reduces
			totalCost := position.AveragePrice * float64(position.Quantity)
			newAvgPrice = totalCost / float64(newQty)
		}
	case ActionSplit:
		if action.OldFaceValue > 0 && action.NewFaceValue > 0 {
			splitRatio := action.OldFaceValue / action.NewFaceValue
			newQty = int(float64(position.Quantity) * splitRatio)
			newAvgPrice = position.AveragePrice / splitRatio
		}
	default:
		newQty = position.Quantity
		newAvgPrice = position.AveragePrice
	}
	return
}

// GetDividendInfo returns dividend information for holdings.
func (h *CorporateActionsHandler) GetDividendInfo(holdings []models.Holding) []DividendInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var dividends []DividendInfo
	now := time.Now()
	futureWindow := now.AddDate(0, 3, 0) // Next 3 months

	for _, holding := range holdings {
		actions, ok := h.actions[holding.Symbol]
		if !ok {
			continue
		}

		for _, action := range actions {
			if action.ActionType != ActionDividend {
				continue
			}
			if action.ExDate.Before(now) || action.ExDate.After(futureWindow) {
				continue
			}

			yield := 0.0
			if holding.LTP > 0 {
				yield = (action.Amount / holding.LTP) * 100
			}

			dividends = append(dividends, DividendInfo{
				Symbol:       holding.Symbol,
				DividendRate: action.Amount,
				ExDate:       action.ExDate,
				RecordDate:   action.RecordDate,
				Yield:        yield,
			})
		}
	}

	return dividends
}

// CalculateDividendYield calculates dividend yield for a holding.
func (h *CorporateActionsHandler) CalculateDividendYield(symbol string, ltp float64) float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	actions, ok := h.actions[symbol]
	if !ok || ltp == 0 {
		return 0
	}

	// Sum dividends from last 12 months
	oneYearAgo := time.Now().AddDate(-1, 0, 0)
	var totalDividend float64

	for _, action := range actions {
		if action.ActionType == ActionDividend && action.ExDate.After(oneYearAgo) {
			totalDividend += action.Amount
		}
	}

	return (totalDividend / ltp) * 100
}

// GetRightsEntitlement calculates rights issue entitlement.
func (h *CorporateActionsHandler) GetRightsEntitlement(holding *models.Holding, action *CorporateAction) (entitledShares int, cost float64) {
	if action.ActionType != ActionRightsIssue {
		return 0, 0
	}

	var rights, held int
	fmt.Sscanf(action.Ratio, "%d:%d", &rights, &held)
	if held > 0 {
		entitledShares = (holding.Quantity / held) * rights
		cost = float64(entitledShares) * action.Premium
	}
	return
}

// AlertBeforeRecordDate checks if any holdings have upcoming record dates.
func (h *CorporateActionsHandler) AlertBeforeRecordDate(holdings []models.Holding, daysAhead int) []CorporateAction {
	h.mu.RLock()
	defer h.mu.RUnlock()

	now := time.Now()
	alertWindow := now.AddDate(0, 0, daysAhead)

	var alerts []CorporateAction
	holdingSymbols := make(map[string]bool)
	for _, h := range holdings {
		holdingSymbols[h.Symbol] = true
	}

	for symbol := range holdingSymbols {
		actions, ok := h.actions[symbol]
		if !ok {
			continue
		}

		for _, action := range actions {
			if action.ActionType == ActionDividend &&
				action.RecordDate.After(now) &&
				action.RecordDate.Before(alertWindow) {
				alerts = append(alerts, action)
			}
		}
	}

	return alerts
}
