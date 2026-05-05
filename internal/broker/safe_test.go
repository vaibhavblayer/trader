package broker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestSafeBrokerSuppressesDuplicateTaggedOrder(t *testing.T) {
	base := newFakeExecutionBroker()
	safe := NewSafeBroker(base)
	order := testExecutionOrder()
	order.Tag = IntentOrderTag("decision:D1", order)

	first, err := safe.PlaceOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("first place order: %v", err)
	}
	second, err := safe.PlaceOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("second place order: %v", err)
	}

	if base.placeCalls != 1 {
		t.Fatalf("expected one broker submission, got %d", base.placeCalls)
	}
	if !second.Duplicate {
		t.Fatalf("expected duplicate result")
	}
	if second.OrderID != first.OrderID {
		t.Fatalf("expected duplicate to return existing order ID %s, got %s", first.OrderID, second.OrderID)
	}
}

func TestSafeBrokerReconcilesAfterAmbiguousPlaceError(t *testing.T) {
	base := newFakeExecutionBroker()
	base.placeErrAfterCreate = fmt.Errorf("network timeout")
	safe := NewSafeBroker(base)
	order := testExecutionOrder()
	order.Tag = IntentOrderTag("decision:D2", order)

	result, err := safe.PlaceOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("expected reconciliation to recover order, got %v", err)
	}
	if result.OrderID == "" || !result.Reconciled {
		t.Fatalf("expected reconciled order result, got %+v", result)
	}
	if base.placeCalls != 1 {
		t.Fatalf("expected one broker submission, got %d", base.placeCalls)
	}
}

func TestSafeBrokerRejectsTagCollision(t *testing.T) {
	base := newFakeExecutionBroker()
	safe := NewSafeBroker(base)
	order := testExecutionOrder()
	order.Tag = "ZT_COLLISION"
	if _, err := safe.PlaceOrder(context.Background(), order); err != nil {
		t.Fatalf("place order: %v", err)
	}

	other := testExecutionOrder()
	other.Tag = order.Tag
	other.Quantity = order.Quantity + 1
	if _, err := safe.PlaceOrder(context.Background(), other); err == nil {
		t.Fatal("expected tag collision error")
	}
}

func TestStableOrderTagIsShortAndDeterministic(t *testing.T) {
	order := testExecutionOrder()
	ts := time.Date(2026, 5, 4, 9, 30, 12, 0, time.UTC)
	first := StableOrderTag(order, ts)
	second := StableOrderTag(order, ts.Add(30*time.Second))
	if first != second {
		t.Fatalf("expected same minute bucket tag, got %s and %s", first, second)
	}
	if len(first) > 20 {
		t.Fatalf("expected Zerodha-compatible short tag, got %q", first)
	}
}

func TestSafeBrokerCancelIsIdempotentAfterCancelledStatus(t *testing.T) {
	base := newFakeExecutionBroker()
	safe := NewSafeBroker(base)
	order := testExecutionOrder()
	result, err := safe.PlaceOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("place order: %v", err)
	}
	if err := safe.CancelOrder(context.Background(), result.OrderID); err != nil {
		t.Fatalf("cancel order: %v", err)
	}
	if err := safe.CancelOrder(context.Background(), result.OrderID); err != nil {
		t.Fatalf("second cancel should be idempotent, got %v", err)
	}
	if base.cancelCalls != 1 {
		t.Fatalf("expected one broker cancel call, got %d", base.cancelCalls)
	}
}

func TestSafeBrokerRetriesOrderbookReads(t *testing.T) {
	base := newFakeExecutionBroker()
	base.getOrdersFailures = 1
	safe := NewSafeBroker(base)

	if _, err := safe.PlaceOrder(context.Background(), testExecutionOrder()); err != nil {
		t.Fatalf("expected orderbook retry to recover, got %v", err)
	}
	if base.getOrdersCalls < 2 {
		t.Fatalf("expected retry, got %d get orders calls", base.getOrdersCalls)
	}
}

func testExecutionOrder() *models.Order {
	return &models.Order{
		Symbol:   "RELIANCE",
		Exchange: models.NSE,
		Side:     models.OrderSideBuy,
		Type:     models.OrderTypeLimit,
		Product:  models.ProductMIS,
		Quantity: 10,
		Price:    2500,
	}
}

type fakeExecutionBroker struct {
	orders              []models.Order
	placeCalls          int
	cancelCalls         int
	getOrdersCalls      int
	getOrdersFailures   int
	placeErrAfterCreate error
}

func newFakeExecutionBroker() *fakeExecutionBroker {
	return &fakeExecutionBroker{}
}

func (f *fakeExecutionBroker) Login(ctx context.Context) error  { return nil }
func (f *fakeExecutionBroker) Logout(ctx context.Context) error { return nil }
func (f *fakeExecutionBroker) IsAuthenticated() bool            { return true }
func (f *fakeExecutionBroker) RefreshSession(ctx context.Context) error {
	return nil
}
func (f *fakeExecutionBroker) GetQuote(ctx context.Context, symbol string) (*models.Quote, error) {
	return nil, nil
}
func (f *fakeExecutionBroker) GetHistorical(ctx context.Context, req HistoricalRequest) ([]models.Candle, error) {
	return nil, nil
}
func (f *fakeExecutionBroker) GetInstruments(ctx context.Context, exchange models.Exchange) ([]models.Instrument, error) {
	return nil, nil
}
func (f *fakeExecutionBroker) GetInstrumentToken(ctx context.Context, symbol string, exchange models.Exchange) (uint32, error) {
	return 0, nil
}
func (f *fakeExecutionBroker) PlaceOrder(ctx context.Context, order *models.Order) (*OrderResult, error) {
	f.placeCalls++
	id := fmt.Sprintf("ORD_%d", f.placeCalls)
	record := *order
	record.ID = id
	record.Status = "OPEN"
	record.PlacedAt = time.Now()
	f.orders = append(f.orders, record)
	if f.placeErrAfterCreate != nil {
		return nil, f.placeErrAfterCreate
	}
	return &OrderResult{OrderID: id, Status: "PLACED", Tag: order.Tag}, nil
}
func (f *fakeExecutionBroker) ModifyOrder(ctx context.Context, orderID string, order *models.Order) error {
	return nil
}
func (f *fakeExecutionBroker) CancelOrder(ctx context.Context, orderID string) error {
	f.cancelCalls++
	for i := range f.orders {
		if f.orders[i].ID == orderID {
			f.orders[i].Status = "CANCELLED"
			return nil
		}
	}
	return nil
}
func (f *fakeExecutionBroker) GetOrders(ctx context.Context) ([]models.Order, error) {
	f.getOrdersCalls++
	if f.getOrdersFailures > 0 {
		f.getOrdersFailures--
		return nil, fmt.Errorf("temporary orderbook error")
	}
	orders := make([]models.Order, len(f.orders))
	copy(orders, f.orders)
	return orders, nil
}
func (f *fakeExecutionBroker) GetOrderHistory(ctx context.Context, from, to time.Time) ([]models.Order, error) {
	return f.GetOrders(ctx)
}
func (f *fakeExecutionBroker) PlaceGTT(ctx context.Context, gtt *models.GTTOrder) (*GTTResult, error) {
	return nil, nil
}
func (f *fakeExecutionBroker) ModifyGTT(ctx context.Context, gttID string, gtt *models.GTTOrder) error {
	return nil
}
func (f *fakeExecutionBroker) CancelGTT(ctx context.Context, gttID string) error {
	return nil
}
func (f *fakeExecutionBroker) GetGTTs(ctx context.Context) ([]models.GTTOrder, error) {
	return nil, nil
}
func (f *fakeExecutionBroker) GetPositions(ctx context.Context) ([]models.Position, error) {
	return nil, nil
}
func (f *fakeExecutionBroker) GetHoldings(ctx context.Context) ([]models.Holding, error) {
	return nil, nil
}
func (f *fakeExecutionBroker) GetBalance(ctx context.Context) (*models.Balance, error) {
	return nil, nil
}
func (f *fakeExecutionBroker) GetMargins(ctx context.Context) (*models.Margins, error) {
	return nil, nil
}
func (f *fakeExecutionBroker) GetOptionChain(ctx context.Context, symbol string, expiry time.Time) (*models.OptionChain, error) {
	return nil, nil
}
func (f *fakeExecutionBroker) GetFuturesChain(ctx context.Context, symbol string) (*models.FuturesChain, error) {
	return nil, nil
}
