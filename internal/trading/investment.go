// Package trading provides trading operations including investment tracking.
package trading

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/store"
)

// InvestmentTracker tracks long-term investments.
// Requirements: 52.1-52.7
type InvestmentTracker struct {
	broker broker.Broker
	store  store.DataStore
}

// NewInvestmentTracker creates a new investment tracker.
func NewInvestmentTracker(b broker.Broker, s store.DataStore) *InvestmentTracker {
	return &InvestmentTracker{
		broker: b,
		store:  s,
	}
}

// Investment represents a long-term investment holding.
// Requirement 52.1: THE CLI SHALL support an investment mode for long-term holdings
type Investment struct {
	Symbol           string
	Quantity         int
	AveragePrice     float64
	CurrentPrice     float64
	InvestedValue    float64
	CurrentValue     float64
	UnrealizedPnL    float64
	UnrealizedPnLPct float64
	DividendsReceived float64
	TotalReturn      float64
	TotalReturnPct   float64
	PurchaseDate     time.Time
	HoldingPeriod    time.Duration
}

// InvestmentSummary represents a summary of all investments.
type InvestmentSummary struct {
	Investments       []Investment
	TotalInvested     float64
	TotalCurrentValue float64
	TotalPnL          float64
	TotalPnLPercent   float64
	TotalDividends    float64
	XIRR              float64
	HoldingCount      int
}

// GetInvestments returns all long-term investment holdings.
// Requirement 52.2: THE CLI SHALL track cost basis, dividends received, and total return for investments
func (it *InvestmentTracker) GetInvestments(ctx context.Context) (*InvestmentSummary, error) {
	holdings, err := it.broker.GetHoldings(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching holdings: %w", err)
	}

	summary := &InvestmentSummary{
		Investments: make([]Investment, 0, len(holdings)),
	}

	for _, hold := range holdings {
		if hold.Quantity == 0 {
			continue
		}

		investment := Investment{
			Symbol:           hold.Symbol,
			Quantity:         hold.Quantity,
			AveragePrice:     hold.AveragePrice,
			CurrentPrice:     hold.LTP,
			InvestedValue:    hold.InvestedValue,
			CurrentValue:     hold.CurrentValue,
			UnrealizedPnL:    hold.PnL,
			UnrealizedPnLPct: hold.PnLPercent,
		}

		// Calculate total return including dividends
		// In a real implementation, dividends would be fetched from a database
		investment.TotalReturn = investment.UnrealizedPnL + investment.DividendsReceived
		if investment.InvestedValue > 0 {
			investment.TotalReturnPct = (investment.TotalReturn / investment.InvestedValue) * 100
		}

		summary.Investments = append(summary.Investments, investment)
		summary.TotalInvested += investment.InvestedValue
		summary.TotalCurrentValue += investment.CurrentValue
		summary.TotalPnL += investment.UnrealizedPnL
		summary.TotalDividends += investment.DividendsReceived
		summary.HoldingCount++
	}

	// Calculate total P&L percentage
	if summary.TotalInvested > 0 {
		summary.TotalPnLPercent = (summary.TotalPnL / summary.TotalInvested) * 100
	}

	return summary, nil
}

// CashFlow represents a cash flow for XIRR calculation.
type CashFlow struct {
	Date   time.Time
	Amount float64 // Negative for outflows (purchases), positive for inflows (sales, dividends)
}

// CalculateXIRR calculates the Extended Internal Rate of Return.
// Requirement 52.3: THE CLI SHALL calculate XIRR (Extended Internal Rate of Return) for investments
func (it *InvestmentTracker) CalculateXIRR(cashFlows []CashFlow) (float64, error) {
	if len(cashFlows) < 2 {
		return 0, fmt.Errorf("need at least 2 cash flows for XIRR calculation")
	}

	// Sort cash flows by date
	sort.Slice(cashFlows, func(i, j int) bool {
		return cashFlows[i].Date.Before(cashFlows[j].Date)
	})

	// Newton-Raphson method to find XIRR
	rate := 0.1 // Initial guess
	maxIterations := 100
	tolerance := 0.0001

	for i := 0; i < maxIterations; i++ {
		npv := it.calculateNPV(cashFlows, rate)
		npvDerivative := it.calculateNPVDerivative(cashFlows, rate)

		if math.Abs(npvDerivative) < 1e-10 {
			break
		}

		newRate := rate - npv/npvDerivative

		if math.Abs(newRate-rate) < tolerance {
			return newRate * 100, nil // Return as percentage
		}

		rate = newRate
	}

	return rate * 100, nil
}

// calculateNPV calculates Net Present Value for XIRR.
func (it *InvestmentTracker) calculateNPV(cashFlows []CashFlow, rate float64) float64 {
	if len(cashFlows) == 0 {
		return 0
	}

	baseDate := cashFlows[0].Date
	var npv float64

	for _, cf := range cashFlows {
		years := cf.Date.Sub(baseDate).Hours() / (24 * 365)
		npv += cf.Amount / math.Pow(1+rate, years)
	}

	return npv
}

// calculateNPVDerivative calculates the derivative of NPV for Newton-Raphson.
func (it *InvestmentTracker) calculateNPVDerivative(cashFlows []CashFlow, rate float64) float64 {
	if len(cashFlows) == 0 {
		return 0
	}

	baseDate := cashFlows[0].Date
	var derivative float64

	for _, cf := range cashFlows {
		years := cf.Date.Sub(baseDate).Hours() / (24 * 365)
		if years > 0 {
			derivative -= years * cf.Amount / math.Pow(1+rate, years+1)
		}
	}

	return derivative
}

// BenchmarkComparison represents performance comparison with a benchmark.
// Requirement 52.4: THE CLI SHALL display investment performance vs benchmark (NIFTY, SENSEX)
type BenchmarkComparison struct {
	InvestmentReturn  float64
	BenchmarkReturn   float64
	Alpha             float64 // Excess return over benchmark
	BenchmarkName     string
	StartDate         time.Time
	EndDate           time.Time
}

// CompareWithBenchmark compares investment performance with a benchmark index.
func (it *InvestmentTracker) CompareWithBenchmark(ctx context.Context, benchmarkSymbol string, startDate, endDate time.Time) (*BenchmarkComparison, error) {
	// Get investment summary
	summary, err := it.GetInvestments(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting investments: %w", err)
	}

	// Get benchmark data
	benchmarkCandles, err := it.store.GetCandles(ctx, benchmarkSymbol, "1day", startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("fetching benchmark data: %w", err)
	}

	if len(benchmarkCandles) < 2 {
		return nil, fmt.Errorf("insufficient benchmark data")
	}

	// Calculate benchmark return
	startPrice := benchmarkCandles[0].Close
	endPrice := benchmarkCandles[len(benchmarkCandles)-1].Close
	benchmarkReturn := ((endPrice - startPrice) / startPrice) * 100

	comparison := &BenchmarkComparison{
		InvestmentReturn: summary.TotalPnLPercent,
		BenchmarkReturn:  benchmarkReturn,
		Alpha:            summary.TotalPnLPercent - benchmarkReturn,
		BenchmarkName:    benchmarkSymbol,
		StartDate:        startDate,
		EndDate:          endDate,
	}

	return comparison, nil
}

// SIPPlan represents a Systematic Investment Plan.
// Requirement 52.5: THE CLI SHALL support SIP (Systematic Investment Plan) tracking
type SIPPlan struct {
	ID              string
	Symbol          string
	Amount          float64
	Frequency       SIPFrequency
	StartDate       time.Time
	NextDate        time.Time
	TotalInvested   float64
	UnitsAccumulated float64
	CurrentValue    float64
	Returns         float64
	ReturnsPercent  float64
	Installments    []SIPInstallment
	IsActive        bool
}

// SIPFrequency represents the frequency of SIP investments.
type SIPFrequency string

const (
	SIPWeekly    SIPFrequency = "weekly"
	SIPBiWeekly  SIPFrequency = "biweekly"
	SIPMonthly   SIPFrequency = "monthly"
	SIPQuarterly SIPFrequency = "quarterly"
)

// SIPInstallment represents a single SIP installment.
type SIPInstallment struct {
	Date     time.Time
	Amount   float64
	Price    float64
	Units    float64
}

// SIPTracker tracks SIP plans.
type SIPTracker struct {
	plans map[string]*SIPPlan
}

// NewSIPTracker creates a new SIP tracker.
func NewSIPTracker() *SIPTracker {
	return &SIPTracker{
		plans: make(map[string]*SIPPlan),
	}
}

// CreateSIPPlan creates a new SIP plan.
func (st *SIPTracker) CreateSIPPlan(symbol string, amount float64, frequency SIPFrequency, startDate time.Time) *SIPPlan {
	id := fmt.Sprintf("SIP_%s_%d", symbol, time.Now().UnixNano())

	plan := &SIPPlan{
		ID:           id,
		Symbol:       symbol,
		Amount:       amount,
		Frequency:    frequency,
		StartDate:    startDate,
		NextDate:     startDate,
		Installments: make([]SIPInstallment, 0),
		IsActive:     true,
	}

	st.plans[id] = plan
	return plan
}

// RecordInstallment records a SIP installment.
func (st *SIPTracker) RecordInstallment(planID string, date time.Time, price float64) error {
	plan, ok := st.plans[planID]
	if !ok {
		return fmt.Errorf("SIP plan not found: %s", planID)
	}

	units := plan.Amount / price

	installment := SIPInstallment{
		Date:   date,
		Amount: plan.Amount,
		Price:  price,
		Units:  units,
	}

	plan.Installments = append(plan.Installments, installment)
	plan.TotalInvested += plan.Amount
	plan.UnitsAccumulated += units

	// Update next date
	plan.NextDate = st.calculateNextDate(date, plan.Frequency)

	return nil
}

// calculateNextDate calculates the next SIP date based on frequency.
func (st *SIPTracker) calculateNextDate(current time.Time, frequency SIPFrequency) time.Time {
	switch frequency {
	case SIPWeekly:
		return current.AddDate(0, 0, 7)
	case SIPBiWeekly:
		return current.AddDate(0, 0, 14)
	case SIPMonthly:
		return current.AddDate(0, 1, 0)
	case SIPQuarterly:
		return current.AddDate(0, 3, 0)
	default:
		return current.AddDate(0, 1, 0)
	}
}

// UpdateCurrentValue updates the current value of a SIP plan.
func (st *SIPTracker) UpdateCurrentValue(planID string, currentPrice float64) error {
	plan, ok := st.plans[planID]
	if !ok {
		return fmt.Errorf("SIP plan not found: %s", planID)
	}

	plan.CurrentValue = plan.UnitsAccumulated * currentPrice
	plan.Returns = plan.CurrentValue - plan.TotalInvested
	if plan.TotalInvested > 0 {
		plan.ReturnsPercent = (plan.Returns / plan.TotalInvested) * 100
	}

	return nil
}

// GetSIPPlan returns a SIP plan by ID.
func (st *SIPTracker) GetSIPPlan(planID string) (*SIPPlan, bool) {
	plan, ok := st.plans[planID]
	return plan, ok
}

// GetAllSIPPlans returns all SIP plans.
func (st *SIPTracker) GetAllSIPPlans() []*SIPPlan {
	plans := make([]*SIPPlan, 0, len(st.plans))
	for _, plan := range st.plans {
		plans = append(plans, plan)
	}
	return plans
}

// GetActiveSIPPlans returns all active SIP plans.
func (st *SIPTracker) GetActiveSIPPlans() []*SIPPlan {
	var active []*SIPPlan
	for _, plan := range st.plans {
		if plan.IsActive {
			active = append(active, plan)
		}
	}
	return active
}

// StopSIPPlan stops a SIP plan.
func (st *SIPTracker) StopSIPPlan(planID string) error {
	plan, ok := st.plans[planID]
	if !ok {
		return fmt.Errorf("SIP plan not found: %s", planID)
	}
	plan.IsActive = false
	return nil
}

// CalculateSIPXIRR calculates XIRR for a SIP plan.
func (st *SIPTracker) CalculateSIPXIRR(planID string, currentPrice float64) (float64, error) {
	plan, ok := st.plans[planID]
	if !ok {
		return 0, fmt.Errorf("SIP plan not found: %s", planID)
	}

	if len(plan.Installments) < 1 {
		return 0, fmt.Errorf("no installments recorded")
	}

	// Build cash flows
	cashFlows := make([]CashFlow, 0, len(plan.Installments)+1)

	// Add installments as outflows (negative)
	for _, inst := range plan.Installments {
		cashFlows = append(cashFlows, CashFlow{
			Date:   inst.Date,
			Amount: -inst.Amount,
		})
	}

	// Add current value as inflow (positive)
	currentValue := plan.UnitsAccumulated * currentPrice
	cashFlows = append(cashFlows, CashFlow{
		Date:   time.Now(),
		Amount: currentValue,
	})

	// Calculate XIRR
	tracker := &InvestmentTracker{}
	return tracker.CalculateXIRR(cashFlows)
}

// InvestmentReport represents a comprehensive investment report.
// Requirement 52.6: THE CLI SHALL separate investment P&L from trading P&L in reports
type InvestmentReport struct {
	Summary           *InvestmentSummary
	SIPPlans          []*SIPPlan
	BenchmarkComparison *BenchmarkComparison
	TopPerformers     []Investment
	BottomPerformers  []Investment
	SectorAllocation  map[string]float64
	GeneratedAt       time.Time
}

// GenerateReport generates a comprehensive investment report.
func (it *InvestmentTracker) GenerateReport(ctx context.Context, benchmarkSymbol string) (*InvestmentReport, error) {
	summary, err := it.GetInvestments(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting investments: %w", err)
	}

	report := &InvestmentReport{
		Summary:          summary,
		SectorAllocation: make(map[string]float64),
		GeneratedAt:      time.Now(),
	}

	// Sort investments by return percentage
	investments := make([]Investment, len(summary.Investments))
	copy(investments, summary.Investments)

	sort.Slice(investments, func(i, j int) bool {
		return investments[i].TotalReturnPct > investments[j].TotalReturnPct
	})

	// Top performers
	topCount := 5
	if len(investments) < topCount {
		topCount = len(investments)
	}
	report.TopPerformers = investments[:topCount]

	// Bottom performers
	bottomCount := 5
	if len(investments) < bottomCount {
		bottomCount = len(investments)
	}
	report.BottomPerformers = investments[len(investments)-bottomCount:]

	// Get benchmark comparison if symbol provided
	if benchmarkSymbol != "" {
		endDate := time.Now()
		startDate := endDate.AddDate(-1, 0, 0) // 1 year ago
		comparison, err := it.CompareWithBenchmark(ctx, benchmarkSymbol, startDate, endDate)
		if err == nil {
			report.BenchmarkComparison = comparison
		}
	}

	return report, nil
}

// DividendRecord represents a dividend payment record.
type DividendRecord struct {
	Symbol       string
	ExDate       time.Time
	RecordDate   time.Time
	PaymentDate  time.Time
	Amount       float64
	Quantity     int
	TotalAmount  float64
}

// DividendTracker tracks dividend payments.
type DividendTracker struct {
	records []DividendRecord
}

// NewDividendTracker creates a new dividend tracker.
func NewDividendTracker() *DividendTracker {
	return &DividendTracker{
		records: make([]DividendRecord, 0),
	}
}

// RecordDividend records a dividend payment.
func (dt *DividendTracker) RecordDividend(record DividendRecord) {
	record.TotalAmount = record.Amount * float64(record.Quantity)
	dt.records = append(dt.records, record)
}

// GetDividendsBySymbol returns all dividends for a symbol.
func (dt *DividendTracker) GetDividendsBySymbol(symbol string) []DividendRecord {
	var dividends []DividendRecord
	for _, record := range dt.records {
		if record.Symbol == symbol {
			dividends = append(dividends, record)
		}
	}
	return dividends
}

// GetTotalDividends returns total dividends received.
func (dt *DividendTracker) GetTotalDividends() float64 {
	var total float64
	for _, record := range dt.records {
		total += record.TotalAmount
	}
	return total
}

// GetDividendsByYear returns dividends grouped by year.
func (dt *DividendTracker) GetDividendsByYear() map[int]float64 {
	byYear := make(map[int]float64)
	for _, record := range dt.records {
		year := record.PaymentDate.Year()
		byYear[year] += record.TotalAmount
	}
	return byYear
}
