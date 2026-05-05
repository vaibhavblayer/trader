package cli

import (
	"context"
	"fmt"
	"time"

	"zerodha-trader/internal/models"
	"zerodha-trader/internal/trading"
)

func (app *App) checkOrderRisk(ctx context.Context, order *models.Order, stopLoss, target float64) (*trading.OrderRiskDecision, error) {
	if app.Risk == nil {
		return nil, nil
	}
	if app.Broker == nil {
		return nil, fmt.Errorf("broker not configured")
	}

	positions, err := app.Broker.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching positions for risk check: %w", err)
	}

	balance, err := app.Broker.GetBalance(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching balance for risk check: %w", err)
	}

	orders, err := app.Broker.GetOrders(ctx)
	if err != nil {
		app.Logger.Warn().Err(err).Msg("Failed to fetch orders for risk check")
	}

	referencePrice := order.Price
	if referencePrice <= 0 {
		if quote, err := app.Broker.GetQuote(ctx, order.Symbol); err == nil && quote != nil {
			referencePrice = quote.LTP
		} else if err != nil {
			app.Logger.Warn().Err(err).Str("symbol", order.Symbol).Msg("Failed to fetch quote for risk check")
		}
	}

	decision := app.Risk.CheckOrder(trading.OrderRiskRequest{
		Order:          order,
		Balance:        balance,
		Positions:      positions,
		TodayOrders:    orders,
		ReferencePrice: referencePrice,
		StopLoss:       stopLoss,
		Target:         target,
		Now:            time.Now(),
	})
	if !decision.Approved {
		return &decision, fmt.Errorf("risk check failed: %s", decision.Error())
	}
	return &decision, nil
}
