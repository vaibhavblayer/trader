package broker

import (
	"context"
	"fmt"
	"time"

	"zerodha-trader/internal/models"
)

// ExecutionPolicy is the broker-level write policy for a safety profile.
type ExecutionPolicy struct {
	Profile     string
	AllowOrders bool
	AllowGTT    bool
	AllowModify bool
	AllowCancel bool
}

// PolicyBroker blocks broker writes that are not allowed by the active safety profile.
type PolicyBroker struct {
	base   Broker
	policy ExecutionPolicy
}

// NewPolicyBroker wraps a broker with profile-level write enforcement.
func NewPolicyBroker(base Broker, policy ExecutionPolicy) *PolicyBroker {
	return &PolicyBroker{base: base, policy: policy}
}

func (p *PolicyBroker) Login(ctx context.Context) error  { return p.base.Login(ctx) }
func (p *PolicyBroker) Logout(ctx context.Context) error { return p.base.Logout(ctx) }
func (p *PolicyBroker) IsAuthenticated() bool            { return p.base.IsAuthenticated() }
func (p *PolicyBroker) RefreshSession(ctx context.Context) error {
	return p.base.RefreshSession(ctx)
}
func (p *PolicyBroker) GetQuote(ctx context.Context, symbol string) (*models.Quote, error) {
	return p.base.GetQuote(ctx, symbol)
}
func (p *PolicyBroker) GetHistorical(ctx context.Context, req HistoricalRequest) ([]models.Candle, error) {
	return p.base.GetHistorical(ctx, req)
}
func (p *PolicyBroker) GetInstruments(ctx context.Context, exchange models.Exchange) ([]models.Instrument, error) {
	return p.base.GetInstruments(ctx, exchange)
}
func (p *PolicyBroker) GetInstrumentToken(ctx context.Context, symbol string, exchange models.Exchange) (uint32, error) {
	return p.base.GetInstrumentToken(ctx, symbol, exchange)
}

func (p *PolicyBroker) PlaceOrder(ctx context.Context, order *models.Order) (*OrderResult, error) {
	if !p.policy.AllowOrders {
		return nil, p.blocked("place order")
	}
	return p.base.PlaceOrder(ctx, order)
}

func (p *PolicyBroker) ModifyOrder(ctx context.Context, orderID string, order *models.Order) error {
	if !p.policy.AllowModify {
		return p.blocked("modify order")
	}
	return p.base.ModifyOrder(ctx, orderID, order)
}

func (p *PolicyBroker) CancelOrder(ctx context.Context, orderID string) error {
	if !p.policy.AllowCancel {
		return p.blocked("cancel order")
	}
	return p.base.CancelOrder(ctx, orderID)
}

func (p *PolicyBroker) GetOrders(ctx context.Context) ([]models.Order, error) {
	return p.base.GetOrders(ctx)
}
func (p *PolicyBroker) GetOrderHistory(ctx context.Context, from, to time.Time) ([]models.Order, error) {
	return p.base.GetOrderHistory(ctx, from, to)
}

func (p *PolicyBroker) PlaceGTT(ctx context.Context, gtt *models.GTTOrder) (*GTTResult, error) {
	if !p.policy.AllowGTT {
		return nil, p.blocked("place GTT")
	}
	return p.base.PlaceGTT(ctx, gtt)
}

func (p *PolicyBroker) ModifyGTT(ctx context.Context, gttID string, gtt *models.GTTOrder) error {
	if !p.policy.AllowGTT {
		return p.blocked("modify GTT")
	}
	return p.base.ModifyGTT(ctx, gttID, gtt)
}

func (p *PolicyBroker) CancelGTT(ctx context.Context, gttID string) error {
	if !p.policy.AllowGTT {
		return p.blocked("cancel GTT")
	}
	return p.base.CancelGTT(ctx, gttID)
}

func (p *PolicyBroker) GetGTTs(ctx context.Context) ([]models.GTTOrder, error) {
	return p.base.GetGTTs(ctx)
}
func (p *PolicyBroker) GetPositions(ctx context.Context) ([]models.Position, error) {
	return p.base.GetPositions(ctx)
}
func (p *PolicyBroker) GetHoldings(ctx context.Context) ([]models.Holding, error) {
	return p.base.GetHoldings(ctx)
}
func (p *PolicyBroker) GetBalance(ctx context.Context) (*models.Balance, error) {
	return p.base.GetBalance(ctx)
}
func (p *PolicyBroker) GetMargins(ctx context.Context) (*models.Margins, error) {
	return p.base.GetMargins(ctx)
}
func (p *PolicyBroker) GetOptionChain(ctx context.Context, symbol string, expiry time.Time) (*models.OptionChain, error) {
	return p.base.GetOptionChain(ctx, symbol, expiry)
}
func (p *PolicyBroker) GetFuturesChain(ctx context.Context, symbol string) (*models.FuturesChain, error) {
	return p.base.GetFuturesChain(ctx, symbol)
}

func (p *PolicyBroker) blocked(action string) error {
	return fmt.Errorf("%s blocked by safety profile %s", action, p.policy.Profile)
}
