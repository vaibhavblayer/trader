package trading

import (
	"fmt"
	"math"
	"strings"
	"time"

	"zerodha-trader/internal/config"
	"zerodha-trader/internal/models"
)

// OrderRiskRequest contains the broker/account state needed to approve an order.
type OrderRiskRequest struct {
	Order          *models.Order
	Balance        *models.Balance
	Positions      []models.Position
	TodayOrders    []models.Order
	ReferencePrice float64
	StopLoss       float64
	Target         float64
	Now            time.Time
}

// OrderRiskDecision is the hard gate outcome for an order.
type OrderRiskDecision struct {
	Approved       bool
	Violations     []string
	Warnings       []string
	ChecksPassed   []string
	OrderValue     float64
	ProjectedValue float64
	RiskReward     float64
	MaxLossAmount  float64
}

// RiskManager enforces non-negotiable execution risk controls.
type RiskManager struct {
	config config.RiskConfig
}

// NewRiskManager creates a hard risk manager with conservative defaults.
func NewRiskManager(cfg config.RiskConfig) *RiskManager {
	if cfg.MaxPositionPercent == 0 {
		cfg.MaxPositionPercent = 10
	}
	if cfg.MaxConcurrentPositions == 0 {
		cfg.MaxConcurrentPositions = 5
	}
	if cfg.MinRiskReward == 0 {
		cfg.MinRiskReward = 2
	}
	if cfg.DailyLossLimit == 0 {
		cfg.DailyLossLimit = 5000
	}
	return &RiskManager{config: cfg}
}

// CheckOrder returns whether an order is allowed to reach the broker.
func (m *RiskManager) CheckOrder(req OrderRiskRequest) OrderRiskDecision {
	decision := OrderRiskDecision{
		Approved:     true,
		Violations:   []string{},
		Warnings:     []string{},
		ChecksPassed: []string{},
	}

	if req.Order == nil {
		return decision.reject("order is required")
	}

	order := req.Order
	if order.Quantity <= 0 {
		return decision.reject("quantity must be positive")
	}
	if order.Side != models.OrderSideBuy && order.Side != models.OrderSideSell {
		return decision.reject(fmt.Sprintf("unsupported order side: %s", order.Side))
	}

	price := effectiveRiskPrice(order, req)
	existing := findPosition(req.Positions, order.Symbol, order.Exchange, order.Product)
	reducing := reducesExposure(order, existing)
	increasing := !reducing

	if price <= 0 && increasing {
		return decision.reject("reference price is required to risk-check new exposure")
	}
	if price <= 0 {
		decision.Warnings = append(decision.Warnings, "reference price unavailable for reducing order")
		decision.ChecksPassed = append(decision.ChecksPassed, "reducing_order")
		return decision
	}

	decision.OrderValue = float64(order.Quantity) * price * multiplier(existing)
	projectedQty := projectedQuantity(order, existing)
	decision.ProjectedValue = math.Abs(float64(projectedQty)) * price * multiplier(existing)

	if m.config.MaxOrderValue > 0 && decision.OrderValue > m.config.MaxOrderValue {
		decision.Violations = append(decision.Violations,
			fmt.Sprintf("order value %.2f exceeds max_order_value %.2f", decision.OrderValue, m.config.MaxOrderValue))
	} else {
		decision.ChecksPassed = append(decision.ChecksPassed, "order_value")
	}

	if increasing {
		m.checkAccountLimits(req, &decision)
		m.checkPositionLimits(req, existing, decision.ProjectedValue, &decision)
		m.checkProtectiveRisk(req, price, &decision)
	} else {
		decision.ChecksPassed = append(decision.ChecksPassed, "reducing_order")
	}

	decision.Approved = len(decision.Violations) == 0
	return decision
}

func (m *RiskManager) checkAccountLimits(req OrderRiskRequest, decision *OrderRiskDecision) {
	if m.config.DailyLossLimit > 0 {
		dailyPnL := dailyPnL(req.Positions)
		if dailyPnL < 0 && -dailyPnL >= m.config.DailyLossLimit {
			decision.Violations = append(decision.Violations,
				fmt.Sprintf("daily loss limit reached: %.2f of %.2f", -dailyPnL, m.config.DailyLossLimit))
		} else {
			decision.ChecksPassed = append(decision.ChecksPassed, "daily_loss_limit")
		}
	}

	if req.Balance == nil {
		decision.Violations = append(decision.Violations, "account balance is required to risk-check new exposure")
		return
	}

	if req.Balance.TotalEquity <= 0 {
		decision.Violations = append(decision.Violations, "total equity must be positive")
		return
	}
	decision.ChecksPassed = append(decision.ChecksPassed, "account_balance")

	if m.config.MaxDailyTrades > 0 {
		todayOrders := countTodaysOrders(req.TodayOrders, req.Now)
		if todayOrders >= m.config.MaxDailyTrades {
			decision.Violations = append(decision.Violations,
				fmt.Sprintf("daily trade limit reached: %d of %d", todayOrders, m.config.MaxDailyTrades))
		} else {
			decision.ChecksPassed = append(decision.ChecksPassed, "daily_trade_limit")
		}
	}
}

func (m *RiskManager) checkPositionLimits(req OrderRiskRequest, existing *models.Position, projectedValue float64, decision *OrderRiskDecision) {
	if req.Balance != nil && req.Balance.TotalEquity > 0 && m.config.MaxPositionPercent > 0 {
		maxPositionValue := req.Balance.TotalEquity * (m.config.MaxPositionPercent / 100)
		if projectedValue > maxPositionValue {
			decision.Violations = append(decision.Violations,
				fmt.Sprintf("projected position value %.2f exceeds %.2f%% limit %.2f",
					projectedValue, m.config.MaxPositionPercent, maxPositionValue))
		} else {
			decision.ChecksPassed = append(decision.ChecksPassed, "position_size")
		}
	}

	if m.config.MaxConcurrentPositions > 0 && (existing == nil || existing.Quantity == 0) {
		openPositions := countOpenPositions(req.Positions)
		if openPositions >= m.config.MaxConcurrentPositions {
			decision.Violations = append(decision.Violations,
				fmt.Sprintf("max concurrent positions reached: %d of %d", openPositions, m.config.MaxConcurrentPositions))
		} else {
			decision.ChecksPassed = append(decision.ChecksPassed, "concurrent_positions")
		}
	}
}

func (m *RiskManager) checkProtectiveRisk(req OrderRiskRequest, entry float64, decision *OrderRiskDecision) {
	order := req.Order
	if m.config.RequireStopLoss && req.StopLoss <= 0 {
		decision.Violations = append(decision.Violations, "stop loss is required for new exposure")
	}
	if m.config.RequireTarget && req.Target <= 0 {
		decision.Violations = append(decision.Violations, "target is required for new exposure")
	}

	if req.StopLoss <= 0 || req.Target <= 0 {
		return
	}

	risk := math.Abs(entry - req.StopLoss)
	reward := math.Abs(req.Target - entry)
	if risk <= 0 {
		decision.Violations = append(decision.Violations, "stop loss must differ from entry")
		return
	}

	if order.Side == models.OrderSideBuy && (req.StopLoss >= entry || req.Target <= entry) {
		decision.Violations = append(decision.Violations, "BUY requires stop below entry and target above entry")
		return
	}
	if order.Side == models.OrderSideSell && (req.StopLoss <= entry || req.Target >= entry) {
		decision.Violations = append(decision.Violations, "SELL requires stop above entry and target below entry")
		return
	}

	decision.RiskReward = reward / risk
	decision.MaxLossAmount = risk * float64(order.Quantity)
	if m.config.MinRiskReward > 0 && decision.RiskReward < m.config.MinRiskReward {
		decision.Violations = append(decision.Violations,
			fmt.Sprintf("risk/reward %.2f below minimum %.2f", decision.RiskReward, m.config.MinRiskReward))
	} else {
		decision.ChecksPassed = append(decision.ChecksPassed, "risk_reward")
	}
}

func (d OrderRiskDecision) reject(reason string) OrderRiskDecision {
	d.Approved = false
	d.Violations = append(d.Violations, reason)
	return d
}

func (d OrderRiskDecision) Error() string {
	if d.Approved {
		return ""
	}
	return strings.Join(d.Violations, "; ")
}

func effectiveRiskPrice(order *models.Order, req OrderRiskRequest) float64 {
	if order.Price > 0 {
		return order.Price
	}
	if req.ReferencePrice > 0 {
		return req.ReferencePrice
	}
	return 0
}

func findPosition(positions []models.Position, symbol string, exchange models.Exchange, product models.ProductType) *models.Position {
	for i := range positions {
		p := &positions[i]
		if p.Symbol != symbol {
			continue
		}
		if exchange != "" && p.Exchange != "" && p.Exchange != exchange {
			continue
		}
		if product != "" && p.Product != "" && p.Product != product {
			continue
		}
		return p
	}
	return nil
}

func reducesExposure(order *models.Order, position *models.Position) bool {
	if position == nil || position.Quantity == 0 {
		return false
	}
	if position.Quantity > 0 {
		return order.Side == models.OrderSideSell
	}
	return order.Side == models.OrderSideBuy
}

func projectedQuantity(order *models.Order, position *models.Position) int {
	current := 0
	if position != nil {
		current = position.Quantity
	}
	if order.Side == models.OrderSideBuy {
		return current + order.Quantity
	}
	return current - order.Quantity
}

func multiplier(position *models.Position) float64 {
	if position == nil || position.Multiplier <= 0 {
		return 1
	}
	return float64(position.Multiplier)
}

func countOpenPositions(positions []models.Position) int {
	count := 0
	for _, p := range positions {
		if p.Quantity != 0 {
			count++
		}
	}
	return count
}

func dailyPnL(positions []models.Position) float64 {
	var pnl float64
	for _, p := range positions {
		pnl += p.PnL
	}
	return pnl
}

func countTodaysOrders(orders []models.Order, now time.Time) int {
	if len(orders) == 0 {
		return 0
	}
	if now.IsZero() {
		now = time.Now()
	}
	count := 0
	for _, order := range orders {
		if order.Side != models.OrderSideBuy && order.Side != models.OrderSideSell {
			continue
		}
		if order.PlacedAt.IsZero() || sameDate(order.PlacedAt, now) {
			count++
		}
	}
	return count
}

func sameDate(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
