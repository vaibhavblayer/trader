package broker

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"zerodha-trader/internal/models"
)

const generatedTagPrefix = "ZT"

// SafeBroker wraps a broker with order idempotency, duplicate detection, and orderbook reconciliation.
type SafeBroker struct {
	base Broker
	now  func() time.Time
}

// NewSafeBroker creates a defensive broker wrapper for execution paths.
func NewSafeBroker(base Broker) *SafeBroker {
	return &SafeBroker{
		base: base,
		now:  time.Now,
	}
}

// StableOrderTag returns a deterministic short tag for an order and time bucket.
func StableOrderTag(order *models.Order, ts time.Time) string {
	if order == nil {
		return ""
	}
	bucket := ts.Format("20060102T1504")
	fingerprint := fmt.Sprintf("%s|%s|%s|%s|%s|%d|%.4f|%.4f|%s",
		strings.ToUpper(order.Symbol),
		order.Exchange,
		order.Side,
		order.Type,
		order.Product,
		order.Quantity,
		order.Price,
		order.TriggerPrice,
		bucket,
	)
	sum := sha1.Sum([]byte(fingerprint))
	return generatedTagPrefix + strings.ToUpper(hex.EncodeToString(sum[:])[:14])
}

// IntentOrderTag returns a stable tag for a known logical intent such as a plan or decision ID.
func IntentOrderTag(intent string, order *models.Order) string {
	if order == nil {
		return ""
	}
	fingerprint := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%d|%.4f|%.4f",
		intent,
		strings.ToUpper(order.Symbol),
		order.Exchange,
		order.Side,
		order.Type,
		order.Product,
		order.Quantity,
		order.Price,
		order.TriggerPrice,
	)
	sum := sha1.Sum([]byte(fingerprint))
	return generatedTagPrefix + strings.ToUpper(hex.EncodeToString(sum[:])[:14])
}

func (s *SafeBroker) Login(ctx context.Context) error {
	return s.base.Login(ctx)
}

func (s *SafeBroker) Logout(ctx context.Context) error {
	return s.base.Logout(ctx)
}

func (s *SafeBroker) IsAuthenticated() bool {
	return s.base.IsAuthenticated()
}

func (s *SafeBroker) RefreshSession(ctx context.Context) error {
	return s.base.RefreshSession(ctx)
}

func (s *SafeBroker) GetQuote(ctx context.Context, symbol string) (*models.Quote, error) {
	return s.base.GetQuote(ctx, symbol)
}

func (s *SafeBroker) GetHistorical(ctx context.Context, req HistoricalRequest) ([]models.Candle, error) {
	return s.base.GetHistorical(ctx, req)
}

func (s *SafeBroker) GetInstruments(ctx context.Context, exchange models.Exchange) ([]models.Instrument, error) {
	return s.base.GetInstruments(ctx, exchange)
}

func (s *SafeBroker) GetInstrumentToken(ctx context.Context, symbol string, exchange models.Exchange) (uint32, error) {
	return s.base.GetInstrumentToken(ctx, symbol, exchange)
}

// PlaceOrder submits an order once, then reconciles against the broker orderbook.
func (s *SafeBroker) PlaceOrder(ctx context.Context, order *models.Order) (*OrderResult, error) {
	if order == nil {
		return nil, fmt.Errorf("order is required")
	}
	prepared := *order
	if prepared.Tag == "" {
		prepared.Tag = StableOrderTag(&prepared, s.now())
	}
	if prepared.Validity == "" {
		prepared.Validity = "DAY"
	}

	existing, err := s.findExistingByTag(ctx, &prepared)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return resultFromOrder(existing, "duplicate order suppressed", true), nil
	}

	result, placeErr := s.base.PlaceOrder(ctx, &prepared)
	if placeErr != nil {
		reconciled, reconcileErr := s.reconcileSubmittedOrder(ctx, &prepared, nil)
		if reconcileErr == nil && reconciled != nil {
			return resultFromOrder(reconciled, "order reconciled after broker error: "+placeErr.Error(), false), nil
		}
		return nil, placeErr
	}
	if result == nil {
		result = &OrderResult{}
	}
	if result.Tag == "" {
		result.Tag = prepared.Tag
	}

	reconciled, reconcileErr := s.reconcileSubmittedOrder(ctx, &prepared, result)
	if reconcileErr == nil && reconciled != nil {
		return resultFromOrder(reconciled, result.Message, false), nil
	}
	if result.OrderID != "" || reconcileErr == nil {
		return result, nil
	}
	return result, fmt.Errorf("order placed but reconciliation failed: %w", reconcileErr)
}

func (s *SafeBroker) ModifyOrder(ctx context.Context, orderID string, order *models.Order) error {
	if orderID == "" {
		return fmt.Errorf("order ID is required")
	}
	existing, err := s.findOrderByID(ctx, orderID)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("order not found: %s", orderID)
	}
	if !isOpenOrderStatus(existing.Status) {
		return fmt.Errorf("cannot modify order with status: %s", existing.Status)
	}
	if err := s.base.ModifyOrder(ctx, orderID, order); err != nil {
		reconciled, reconcileErr := s.findOrderByID(ctx, orderID)
		if reconcileErr == nil && reconciled != nil && sameModification(reconciled, order) {
			return nil
		}
		return err
	}
	return nil
}

func (s *SafeBroker) CancelOrder(ctx context.Context, orderID string) error {
	if orderID == "" {
		return fmt.Errorf("order ID is required")
	}
	existing, err := s.findOrderByID(ctx, orderID)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("order not found: %s", orderID)
	}
	if isCancelledOrderStatus(existing.Status) {
		return nil
	}
	if !isOpenOrderStatus(existing.Status) {
		return fmt.Errorf("cannot cancel order with status: %s", existing.Status)
	}
	if err := s.base.CancelOrder(ctx, orderID); err != nil {
		reconciled, reconcileErr := s.findOrderByID(ctx, orderID)
		if reconcileErr == nil && reconciled != nil && isCancelledOrderStatus(reconciled.Status) {
			return nil
		}
		return err
	}
	return nil
}

func (s *SafeBroker) GetOrders(ctx context.Context) ([]models.Order, error) {
	return s.base.GetOrders(ctx)
}

func (s *SafeBroker) GetOrderHistory(ctx context.Context, from, to time.Time) ([]models.Order, error) {
	return s.base.GetOrderHistory(ctx, from, to)
}

func (s *SafeBroker) PlaceGTT(ctx context.Context, gtt *models.GTTOrder) (*GTTResult, error) {
	return s.base.PlaceGTT(ctx, gtt)
}

func (s *SafeBroker) ModifyGTT(ctx context.Context, gttID string, gtt *models.GTTOrder) error {
	return s.base.ModifyGTT(ctx, gttID, gtt)
}

func (s *SafeBroker) CancelGTT(ctx context.Context, gttID string) error {
	return s.base.CancelGTT(ctx, gttID)
}

func (s *SafeBroker) GetGTTs(ctx context.Context) ([]models.GTTOrder, error) {
	return s.base.GetGTTs(ctx)
}

func (s *SafeBroker) GetPositions(ctx context.Context) ([]models.Position, error) {
	return s.base.GetPositions(ctx)
}

func (s *SafeBroker) GetHoldings(ctx context.Context) ([]models.Holding, error) {
	return s.base.GetHoldings(ctx)
}

func (s *SafeBroker) GetBalance(ctx context.Context) (*models.Balance, error) {
	return s.base.GetBalance(ctx)
}

func (s *SafeBroker) GetMargins(ctx context.Context) (*models.Margins, error) {
	return s.base.GetMargins(ctx)
}

func (s *SafeBroker) GetOptionChain(ctx context.Context, symbol string, expiry time.Time) (*models.OptionChain, error) {
	return s.base.GetOptionChain(ctx, symbol, expiry)
}

func (s *SafeBroker) GetFuturesChain(ctx context.Context, symbol string) (*models.FuturesChain, error) {
	return s.base.GetFuturesChain(ctx, symbol)
}

func (s *SafeBroker) findExistingByTag(ctx context.Context, order *models.Order) (*models.Order, error) {
	if order.Tag == "" {
		return nil, nil
	}
	orders, err := s.getOrdersWithRetry(ctx)
	if err != nil {
		return nil, fmt.Errorf("pre-flight order reconciliation failed: %w", err)
	}
	for i := range orders {
		if orders[i].Tag != order.Tag {
			continue
		}
		if !sameOrderIntent(&orders[i], order) {
			return nil, fmt.Errorf("idempotency tag collision for %s", order.Tag)
		}
		if suppressDuplicateStatus(orders[i].Status) {
			return &orders[i], nil
		}
	}
	return nil, nil
}

func (s *SafeBroker) reconcileSubmittedOrder(ctx context.Context, order *models.Order, result *OrderResult) (*models.Order, error) {
	orders, err := s.getOrdersWithRetry(ctx)
	if err != nil {
		return nil, err
	}
	for i := range orders {
		if result != nil && result.OrderID != "" && orders[i].ID == result.OrderID {
			return &orders[i], nil
		}
	}
	for i := range orders {
		if order.Tag != "" && orders[i].Tag == order.Tag && sameOrderIntent(&orders[i], order) {
			return &orders[i], nil
		}
	}
	return nil, fmt.Errorf("submitted order not found in broker orderbook")
}

func (s *SafeBroker) findOrderByID(ctx context.Context, orderID string) (*models.Order, error) {
	orders, err := s.getOrdersWithRetry(ctx)
	if err != nil {
		return nil, fmt.Errorf("order reconciliation failed: %w", err)
	}
	for i := range orders {
		if orders[i].ID == orderID {
			return &orders[i], nil
		}
	}
	return nil, nil
}

func (s *SafeBroker) getOrdersWithRetry(ctx context.Context) ([]models.Order, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		orders, err := s.base.GetOrders(ctx)
		if err == nil {
			return orders, nil
		}
		lastErr = err
		if attempt == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	return nil, lastErr
}

func resultFromOrder(order *models.Order, message string, duplicate bool) *OrderResult {
	if message == "" {
		message = "order reconciled"
	}
	return &OrderResult{
		OrderID:      order.ID,
		Status:       order.Status,
		Message:      message,
		Tag:          order.Tag,
		FilledQty:    order.FilledQty,
		AveragePrice: order.AveragePrice,
		Reconciled:   true,
		Duplicate:    duplicate,
	}
}

func sameOrderIntent(a, b *models.Order) bool {
	if a == nil || b == nil {
		return false
	}
	return strings.EqualFold(a.Symbol, b.Symbol) &&
		a.Exchange == b.Exchange &&
		a.Side == b.Side &&
		a.Type == b.Type &&
		a.Product == b.Product &&
		a.Quantity == b.Quantity &&
		priceEqual(a.Price, b.Price) &&
		priceEqual(a.TriggerPrice, b.TriggerPrice)
}

func priceEqual(a, b float64) bool {
	if a > b {
		return a-b < 0.0001
	}
	return b-a < 0.0001
}

func suppressDuplicateStatus(status string) bool {
	switch strings.ToUpper(status) {
	case "OPEN", "COMPLETE", "COMPLETED", "PARTIAL", "UPDATE", "PUT ORDER REQ RECEIVED", "VALIDATION PENDING":
		return true
	default:
		return false
	}
}

func isOpenOrderStatus(status string) bool {
	switch strings.ToUpper(status) {
	case "OPEN", "PARTIAL", "UPDATE", "PUT ORDER REQ RECEIVED", "VALIDATION PENDING", "TRIGGER PENDING":
		return true
	default:
		return false
	}
}

func isCancelledOrderStatus(status string) bool {
	switch strings.ToUpper(status) {
	case "CANCELLED", "CANCELLED AMO":
		return true
	default:
		return false
	}
}

func sameModification(existing, wanted *models.Order) bool {
	if existing == nil || wanted == nil {
		return false
	}
	return existing.Quantity == wanted.Quantity &&
		priceEqual(existing.Price, wanted.Price) &&
		priceEqual(existing.TriggerPrice, wanted.TriggerPrice)
}
