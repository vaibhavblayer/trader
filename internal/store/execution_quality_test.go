package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

func TestSQLiteExecutionQualityReport(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "execution_quality.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	now := time.Now()
	events := []*broker.PaperLedgerEvent{
		{
			Timestamp: now.Add(-4 * time.Hour),
			Type:      "ORDER_PLACED",
			RefID:     "PAPER_COMPLETE",
			Symbol:    "RELIANCE",
			Payload: map[string]interface{}{
				"status":         "COMPLETE",
				"side":           "BUY",
				"order_type":     "MARKET",
				"quantity":       10,
				"filled_qty":     10,
				"expected_price": 100.0,
				"actual_price":   100.5,
				"slippage_bp":    50.0,
				"order_value":    1005.0,
				"costs":          2.0,
			},
		},
		{
			Timestamp: now.Add(-3 * time.Hour),
			Type:      "ORDER_PLACED",
			RefID:     "PAPER_PARTIAL",
			Symbol:    "INFY",
			Payload: map[string]interface{}{
				"status":         "PARTIAL",
				"side":           "SELL",
				"order_type":     "LIMIT",
				"quantity":       20,
				"filled_qty":     5,
				"expected_price": 200.0,
				"actual_price":   199.0,
				"slippage_bp":    50.0,
				"order_value":    995.0,
				"costs":          1.5,
			},
		},
		{
			Timestamp: now.Add(-2 * time.Hour),
			Type:      "ORDER_PLACED",
			RefID:     "PAPER_CANCELLED",
			Symbol:    "TCS",
			Payload: map[string]interface{}{
				"status":     "OPEN",
				"side":       "BUY",
				"order_type": "LIMIT",
				"quantity":   3,
			},
		},
		{
			Timestamp: now.Add(-90 * time.Minute),
			Type:      "ORDER_CANCELLED",
			RefID:     "PAPER_CANCELLED",
			Symbol:    "TCS",
			Payload: map[string]interface{}{
				"status": "CANCELLED",
			},
		},
		{
			Timestamp: now.Add(-time.Hour),
			Type:      "ORDER_REJECTED",
			RefID:     "PAPER_REJECTED",
			Symbol:    "RELIANCE",
			Payload: map[string]interface{}{
				"side":       "BUY",
				"order_type": "MARKET",
				"quantity":   1000,
				"reason":     "insufficient funds",
			},
		},
	}
	for _, event := range events {
		if err := store.AppendPaperLedger(ctx, event); err != nil {
			t.Fatalf("append ledger: %v", err)
		}
	}
	if err := store.SaveDecisionLog(ctx, &models.DecisionLog{
		DecisionID: "DEC_BLOCKED",
		Timestamp:  now.Add(-30 * time.Minute),
		Stage:      models.DecisionStageExecutionBlocked,
		Symbol:     "RELIANCE",
		Action:     "BUY",
		Status:     "RISK_REJECTED",
		Message:    "risk rejected",
	}); err != nil {
		t.Fatalf("save decision log: %v", err)
	}

	report, err := store.GetExecutionQualityReport(ctx, models.ExecutionQualityFilter{
		StartDate:       now.Add(-24 * time.Hour),
		EndDate:         now.Add(time.Hour),
		SlippageAlertBp: 40,
		Limit:           10,
	})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if report.TotalOrders != 4 || report.FilledOrders != 2 || report.PartialFills != 1 || report.CancelledOrders != 1 || report.RejectedOrders != 1 {
		t.Fatalf("unexpected order counts: %#v", report)
	}
	if report.BlockedExecutions != 1 {
		t.Fatalf("expected one blocked execution, got %d", report.BlockedExecutions)
	}
	if report.FillRate != 50 {
		t.Fatalf("expected fill rate 50, got %.2f", report.FillRate)
	}
	if report.AvgSlippageBp != 50 {
		t.Fatalf("expected avg slippage 50 bp, got %.2f", report.AvgSlippageBp)
	}
	if len(report.HighSlippageOrders) != 2 {
		t.Fatalf("expected high slippage samples, got %#v", report.HighSlippageOrders)
	}
	if len(report.BySymbol) != 3 || len(report.ByOrderType) != 2 || len(report.BySide) != 2 {
		t.Fatalf("expected grouped stats, got %#v", report)
	}
}
