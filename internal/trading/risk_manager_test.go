package trading

import (
	"strings"
	"testing"
	"time"

	"zerodha-trader/internal/config"
	"zerodha-trader/internal/models"
)

func TestRiskManagerAllowsValidProtectedOrder(t *testing.T) {
	manager := NewRiskManager(config.RiskConfig{
		MaxPositionPercent:     10,
		MaxConcurrentPositions: 5,
		MinRiskReward:          2,
		DailyLossLimit:         5000,
		RequireStopLoss:        true,
		RequireTarget:          true,
	})

	decision := manager.CheckOrder(OrderRiskRequest{
		Order: &models.Order{
			Symbol:   "RELIANCE",
			Exchange: models.NSE,
			Side:     models.OrderSideBuy,
			Product:  models.ProductMIS,
			Quantity: 10,
			Price:    100,
		},
		Balance:  &models.Balance{TotalEquity: 100000, AvailableCash: 50000},
		StopLoss: 95,
		Target:   112,
	})

	if !decision.Approved {
		t.Fatalf("expected order to be approved, got %v", decision.Violations)
	}
	if decision.RiskReward < 2 {
		t.Fatalf("expected risk reward to be calculated, got %.2f", decision.RiskReward)
	}
}

func TestRiskManagerBlocksUnvaluedNewExposure(t *testing.T) {
	manager := NewRiskManager(config.RiskConfig{})

	decision := manager.CheckOrder(OrderRiskRequest{
		Order: &models.Order{
			Symbol:   "INFY",
			Exchange: models.NSE,
			Side:     models.OrderSideBuy,
			Product:  models.ProductMIS,
			Quantity: 1,
		},
		Balance: &models.Balance{TotalEquity: 100000},
	})

	assertRiskViolation(t, decision, "reference price")
}

func TestRiskManagerBlocksMissingStopAndTargetWhenRequired(t *testing.T) {
	manager := NewRiskManager(config.RiskConfig{
		MaxPositionPercent:     10,
		MaxConcurrentPositions: 5,
		RequireStopLoss:        true,
		RequireTarget:          true,
	})

	decision := manager.CheckOrder(OrderRiskRequest{
		Order: &models.Order{
			Symbol:   "TCS",
			Exchange: models.NSE,
			Side:     models.OrderSideBuy,
			Product:  models.ProductMIS,
			Quantity: 5,
			Price:    100,
		},
		Balance: &models.Balance{TotalEquity: 100000},
	})

	assertRiskViolation(t, decision, "stop loss")
	assertRiskViolation(t, decision, "target")
}

func TestRiskManagerBlocksPoorRiskReward(t *testing.T) {
	manager := NewRiskManager(config.RiskConfig{
		MaxPositionPercent:     10,
		MaxConcurrentPositions: 5,
		MinRiskReward:          2,
		RequireStopLoss:        true,
		RequireTarget:          true,
	})

	decision := manager.CheckOrder(OrderRiskRequest{
		Order: &models.Order{
			Symbol:   "TCS",
			Exchange: models.NSE,
			Side:     models.OrderSideBuy,
			Product:  models.ProductMIS,
			Quantity: 5,
			Price:    100,
		},
		Balance:  &models.Balance{TotalEquity: 100000},
		StopLoss: 95,
		Target:   105,
	})

	assertRiskViolation(t, decision, "risk/reward")
}

func TestRiskManagerAllowsReducingOrderWithoutReferencePrice(t *testing.T) {
	manager := NewRiskManager(config.RiskConfig{
		RequireStopLoss: true,
		RequireTarget:   true,
	})

	decision := manager.CheckOrder(OrderRiskRequest{
		Order: &models.Order{
			Symbol:   "RELIANCE",
			Exchange: models.NSE,
			Side:     models.OrderSideSell,
			Product:  models.ProductMIS,
			Quantity: 10,
		},
		Positions: []models.Position{{
			Symbol:   "RELIANCE",
			Exchange: models.NSE,
			Product:  models.ProductMIS,
			Quantity: 10,
		}},
	})

	if !decision.Approved {
		t.Fatalf("expected reducing order to be approved, got %v", decision.Violations)
	}
}

func TestRiskManagerBlocksPositionSizeLimit(t *testing.T) {
	manager := NewRiskManager(config.RiskConfig{
		MaxPositionPercent:     5,
		MaxConcurrentPositions: 5,
		RequireStopLoss:        false,
		RequireTarget:          false,
	})

	decision := manager.CheckOrder(OrderRiskRequest{
		Order: &models.Order{
			Symbol:   "RELIANCE",
			Exchange: models.NSE,
			Side:     models.OrderSideBuy,
			Product:  models.ProductMIS,
			Quantity: 100,
			Price:    100,
		},
		Balance: &models.Balance{TotalEquity: 100000},
	})

	assertRiskViolation(t, decision, "position value")
}

func TestRiskManagerBlocksDailyTradeLimit(t *testing.T) {
	manager := NewRiskManager(config.RiskConfig{
		MaxPositionPercent:     10,
		MaxConcurrentPositions: 5,
		MaxDailyTrades:         2,
		RequireStopLoss:        false,
		RequireTarget:          false,
	})
	now := time.Date(2026, 5, 4, 10, 0, 0, 0, time.Local)

	decision := manager.CheckOrder(OrderRiskRequest{
		Order: &models.Order{
			Symbol:   "RELIANCE",
			Exchange: models.NSE,
			Side:     models.OrderSideBuy,
			Product:  models.ProductMIS,
			Quantity: 1,
			Price:    100,
		},
		Balance: &models.Balance{TotalEquity: 100000},
		TodayOrders: []models.Order{
			{Side: models.OrderSideBuy, PlacedAt: now.Add(-time.Hour)},
			{Side: models.OrderSideSell, PlacedAt: now.Add(-30 * time.Minute)},
		},
		Now: now,
	})

	assertRiskViolation(t, decision, "daily trade limit")
}

func assertRiskViolation(t *testing.T, decision OrderRiskDecision, want string) {
	t.Helper()
	if decision.Approved {
		t.Fatalf("expected risk decision to be rejected")
	}
	got := strings.Join(decision.Violations, "; ")
	if !strings.Contains(got, want) {
		t.Fatalf("expected violation containing %q, got %q", want, got)
	}
}
