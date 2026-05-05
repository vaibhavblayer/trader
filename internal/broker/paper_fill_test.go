package broker

import (
	"context"
	"math"
	"testing"

	"zerodha-trader/internal/models"
)

func TestPaperBrokerAppliesSpreadSlippageAndCosts(t *testing.T) {
	ctx := context.Background()
	b := NewPaperBroker(PaperBrokerConfig{
		InitialBalance: 100000,
		FillModel: PaperFillModel{
			SlippageRate:   0.001,
			SpreadRate:     0.002,
			CommissionRate: 0.001,
			FlatFee:        10,
		},
	})
	b.UpdatePrice("RELIANCE", 100)

	result, err := b.PlaceOrder(ctx, &models.Order{
		Symbol:   "RELIANCE",
		Exchange: models.NSE,
		Side:     models.OrderSideBuy,
		Type:     models.OrderTypeMarket,
		Product:  models.ProductMIS,
		Quantity: 10,
	})
	if err != nil {
		t.Fatalf("place order: %v", err)
	}
	if result.Status != "COMPLETE" {
		t.Fatalf("expected complete order, got %s", result.Status)
	}

	orders, err := b.GetOrders(ctx)
	if err != nil {
		t.Fatalf("get orders: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(orders))
	}
	wantPrice := 100 * (1 + 0.002/2) * (1 + 0.001)
	if math.Abs(orders[0].AveragePrice-wantPrice) > 0.0001 {
		t.Fatalf("expected average price %.4f, got %.4f", wantPrice, orders[0].AveragePrice)
	}

	balance, err := b.GetBalance(ctx)
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	orderValue := wantPrice * 10
	wantCash := 100000 - orderValue - orderValue*0.001 - 10
	if math.Abs(balance.AvailableCash-wantCash) > 0.01 {
		t.Fatalf("expected cash %.2f, got %.2f", wantCash, balance.AvailableCash)
	}
}

func TestPaperBrokerUsesTickBidAskAndPartialDepth(t *testing.T) {
	ctx := context.Background()
	b := NewPaperBroker(PaperBrokerConfig{
		InitialBalance: 100000,
		FillModel: PaperFillModel{
			SlippageRate:        0,
			SpreadRate:          0.001,
			CommissionRate:      0.0003,
			AllowPartialFills:   true,
			MaxFillDepthPercent: 50,
		},
	})
	b.ProcessTick(models.Tick{
		Symbol:       "INFY",
		LTP:          100,
		BidPrice:     99.95,
		AskPrice:     100.05,
		BuyQuantity:  40,
		SellQuantity: 10,
	})

	result, err := b.PlaceOrder(ctx, &models.Order{
		Symbol:   "INFY",
		Exchange: models.NSE,
		Side:     models.OrderSideBuy,
		Type:     models.OrderTypeMarket,
		Product:  models.ProductMIS,
		Quantity: 20,
	})
	if err != nil {
		t.Fatalf("place order: %v", err)
	}
	if result.Status != "PARTIAL" {
		t.Fatalf("expected partial order, got %s", result.Status)
	}

	orders, err := b.GetOrders(ctx)
	if err != nil {
		t.Fatalf("get orders: %v", err)
	}
	if orders[0].FilledQty != 5 {
		t.Fatalf("expected filled quantity 5, got %d", orders[0].FilledQty)
	}
	wantPrice := 100.05 * (1 + 0.0005)
	if math.Abs(orders[0].AveragePrice-wantPrice) > 0.0001 {
		t.Fatalf("expected ask fill %.4f, got %.4f", wantPrice, orders[0].AveragePrice)
	}
}

func TestPaperBrokerLimitOrderStaysOpenWhenNotMarketableAfterCosts(t *testing.T) {
	ctx := context.Background()
	b := NewPaperBroker(PaperBrokerConfig{InitialBalance: 100000})
	b.UpdatePrice("TCS", 100)

	result, err := b.PlaceOrder(ctx, &models.Order{
		Symbol:   "TCS",
		Exchange: models.NSE,
		Side:     models.OrderSideBuy,
		Type:     models.OrderTypeLimit,
		Product:  models.ProductMIS,
		Quantity: 10,
		Price:    100,
	})
	if err != nil {
		t.Fatalf("place order: %v", err)
	}
	if result.Status != "OPEN" {
		t.Fatalf("expected open order, got %s", result.Status)
	}
}
