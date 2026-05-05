package broker

import (
	"context"
	"testing"

	"zerodha-trader/internal/models"
)

type memoryPaperLedger struct {
	state  *PaperState
	events []PaperLedgerEvent
}

func (m *memoryPaperLedger) LoadPaperState(ctx context.Context) (*PaperState, error) {
	return m.state, nil
}

func (m *memoryPaperLedger) SavePaperState(ctx context.Context, state *PaperState) error {
	stateCopy := *state
	m.state = &stateCopy
	return nil
}

func (m *memoryPaperLedger) AppendPaperLedger(ctx context.Context, event *PaperLedgerEvent) error {
	eventCopy := *event
	m.events = append(m.events, eventCopy)
	return nil
}

func (m *memoryPaperLedger) GetPaperLedger(ctx context.Context, limit int) ([]PaperLedgerEvent, error) {
	return m.events, nil
}

func TestPaperBrokerPersistsAndRestoresState(t *testing.T) {
	ctx := context.Background()
	ledger := &memoryPaperLedger{}

	b := NewPaperBroker(PaperBrokerConfig{
		InitialBalance: 100000,
		Ledger:         ledger,
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
	if result.OrderID == "" {
		t.Fatal("expected paper order id")
	}
	if ledger.state == nil {
		t.Fatal("expected persisted paper state")
	}
	if len(ledger.events) != 1 || ledger.events[0].Type != "ORDER_PLACED" {
		t.Fatalf("expected order ledger event, got %#v", ledger.events)
	}

	restored := NewPaperBroker(PaperBrokerConfig{
		InitialBalance: 100000,
		Ledger:         ledger,
	})
	orders, err := restored.GetOrders(ctx)
	if err != nil {
		t.Fatalf("get restored orders: %v", err)
	}
	if len(orders) != 1 || orders[0].ID != result.OrderID {
		t.Fatalf("expected restored order %s, got %#v", result.OrderID, orders)
	}

	positions, err := restored.GetPositions(ctx)
	if err != nil {
		t.Fatalf("get restored positions: %v", err)
	}
	if len(positions) != 1 || positions[0].Quantity != 10 {
		t.Fatalf("expected restored position, got %#v", positions)
	}
}
