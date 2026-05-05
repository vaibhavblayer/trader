package broker

import (
	"context"
	"time"

	"zerodha-trader/internal/models"
)

// PaperState is the durable snapshot of a paper trading account.
type PaperState struct {
	Positions    []models.Position
	Holdings     []models.Holding
	Orders       []models.Order
	GTTOrders    []models.GTTOrder
	Balance      models.Balance
	OrderCounter int
	GTTCounter   int
	UpdatedAt    time.Time
}

// PaperLedgerEvent is an append-only paper trading event.
type PaperLedgerEvent struct {
	ID        string
	Timestamp time.Time
	Type      string
	RefID     string
	Symbol    string
	Payload   map[string]interface{}
}

// PaperLedger persists paper trading state and append-only events.
type PaperLedger interface {
	LoadPaperState(ctx context.Context) (*PaperState, error)
	SavePaperState(ctx context.Context, state *PaperState) error
	AppendPaperLedger(ctx context.Context, event *PaperLedgerEvent) error
	GetPaperLedger(ctx context.Context, limit int) ([]PaperLedgerEvent, error)
}
