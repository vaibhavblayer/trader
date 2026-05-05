package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

func TestSQLitePaperStateAndLedgerRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "paper.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	state := &broker.PaperState{
		Balance: models.Balance{
			AvailableCash: 90000,
			TotalEquity:   100000,
		},
		Positions: []models.Position{
			{
				Symbol:       "RELIANCE",
				Exchange:     models.NSE,
				Product:      models.ProductMIS,
				Quantity:     10,
				AveragePrice: 100,
			},
		},
		Orders: []models.Order{
			{
				ID:       "PAPER_1",
				Symbol:   "RELIANCE",
				Exchange: models.NSE,
				Side:     models.OrderSideBuy,
				Type:     models.OrderTypeMarket,
				Product:  models.ProductMIS,
				Quantity: 10,
				Status:   "COMPLETE",
				PlacedAt: time.Now(),
			},
		},
		OrderCounter: 1,
		UpdatedAt:    time.Now(),
	}
	if err := store.SavePaperState(ctx, state); err != nil {
		t.Fatalf("save paper state: %v", err)
	}

	event := &broker.PaperLedgerEvent{
		Type:   "ORDER_PLACED",
		RefID:  "PAPER_1",
		Symbol: "RELIANCE",
		Payload: map[string]interface{}{
			"status": "COMPLETE",
		},
	}
	if err := store.AppendPaperLedger(ctx, event); err != nil {
		t.Fatalf("append paper ledger: %v", err)
	}

	gotState, err := store.LoadPaperState(ctx)
	if err != nil {
		t.Fatalf("load paper state: %v", err)
	}
	if gotState == nil || gotState.Balance.AvailableCash != 90000 || len(gotState.Orders) != 1 {
		t.Fatalf("state not restored: %#v", gotState)
	}

	events, err := store.GetPaperLedger(ctx, 10)
	if err != nil {
		t.Fatalf("get paper ledger: %v", err)
	}
	if len(events) != 1 || events[0].Type != "ORDER_PLACED" || events[0].RefID != "PAPER_1" {
		t.Fatalf("ledger not restored: %#v", events)
	}
	if events[0].Payload["status"] != "COMPLETE" {
		t.Fatalf("payload not restored: %#v", events[0].Payload)
	}
}
