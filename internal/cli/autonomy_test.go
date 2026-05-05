package cli

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/config"
	"zerodha-trader/internal/models"
	storepkg "zerodha-trader/internal/store"
)

func TestBuildAutonomyReadinessReportUsesDurableReports(t *testing.T) {
	ctx := context.Background()
	dataStore, err := storepkg.NewSQLiteStore(filepath.Join(t.TempDir(), "readiness.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer dataStore.Close()
	now := time.Now()
	app := &App{
		Config: &config.Config{
			Trading: config.TradingConfig{Mode: "paper", SafetyProfile: config.SafetyProfilePaper},
			Agents:  config.AgentConfig{AutonomousMode: "SEMI_AUTO"},
		},
		Store: dataStore,
	}
	seedReadinessData(t, ctx, dataStore, now)

	report, err := buildAutonomyReadinessReport(ctx, app, autonomyReadinessOptions{
		Days:               30,
		Phase:              autonomyPhasePaperSoak,
		MinDecisive:        1,
		MinReviewedTrades:  1,
		MinWinRate:         50,
		MinExpectancy:      0,
		MaxSlippageBp:      50,
		MaxRejectionRate:   10,
		MaxMissingLinkRate: 20,
	})
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if report.Decision == models.AutonomyDecisionBlocked {
		t.Fatalf("expected non-blocked readiness, got %#v", report)
	}
	if report.Summary.PaperDecisive != 1 || report.Summary.PaperWinRate != 100 || report.Summary.PaperExpectancy != 6 {
		t.Fatalf("expected seeded summary, got %#v", report.Summary)
	}
	if report.Summary.ReviewedTrades != 1 || report.Summary.ExecutionOrders != 1 {
		t.Fatalf("expected linked execution/review stats, got %#v", report.Summary)
	}
}

func TestBuildAutonomyReadinessReportBlocksKillSwitch(t *testing.T) {
	ctx := context.Background()
	dataStore, err := storepkg.NewSQLiteStore(filepath.Join(t.TempDir(), "readiness_blocked.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer dataStore.Close()
	app := &App{
		Config: &config.Config{
			Trading: config.TradingConfig{Mode: "paper", SafetyProfile: config.SafetyProfilePaper},
			Agents:  config.AgentConfig{AutonomousMode: "SEMI_AUTO"},
		},
		Store: dataStore,
	}
	if err := dataStore.SaveDaemonState(ctx, &models.DaemonState{
		ID:               defaultDaemonStateID,
		Status:           models.DaemonStatusPaused,
		UpdatedAt:        time.Now(),
		KillSwitchActive: true,
		KillSwitchReason: "test halt",
	}); err != nil {
		t.Fatalf("save daemon state: %v", err)
	}

	report, err := buildAutonomyReadinessReport(ctx, app, autonomyReadinessOptions{
		Days:              30,
		Phase:             autonomyPhasePaperSoak,
		MinDecisive:       1,
		MinReviewedTrades: 1,
	})
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if report.Decision != models.AutonomyDecisionBlocked {
		t.Fatalf("expected blocked readiness, got %#v", report)
	}
}

func seedReadinessData(t *testing.T, ctx context.Context, dataStore storepkg.DataStore, now time.Time) {
	t.Helper()
	prediction := &models.PaperPrediction{
		ID:          "PRED_READY",
		Symbol:      "RELIANCE",
		Action:      "BUY",
		Confidence:  80,
		EntryPrice:  100,
		TargetPrice: 106,
		StopLoss:    98,
		TimeWindow:  time.Hour,
		CreatedAt:   now.Add(-30 * time.Minute),
		ExpiresAt:   now.Add(30 * time.Minute),
		SetupName:   "llm_hard_gates",
		Timeframe:   "1h",
		Gates: []models.PaperPredictionGate{
			{Name: "rsi_regime", Passed: true},
		},
		Evaluated:  true,
		ExitPrice:  106,
		Outcome:    "RIGHT",
		PnLPercent: 6,
	}
	if err := dataStore.SavePaperPrediction(ctx, prediction); err != nil {
		t.Fatalf("save prediction: %v", err)
	}
	ledger, ok := dataStore.(broker.PaperLedger)
	if !ok {
		t.Fatal("store does not implement paper ledger")
	}
	if err := ledger.AppendPaperLedger(ctx, &broker.PaperLedgerEvent{
		Timestamp: now.Add(-5 * time.Minute),
		Type:      "ORDER_PLACED",
		RefID:     "PAPER_READY",
		Symbol:    "RELIANCE",
		Payload: map[string]interface{}{
			"status":         "COMPLETE",
			"side":           "BUY",
			"order_type":     "MARKET",
			"quantity":       10,
			"filled_qty":     10,
			"expected_price": 100.0,
			"actual_price":   100.2,
			"slippage_bp":    20.0,
			"order_value":    1002.0,
			"costs":          2.0,
		},
	}); err != nil {
		t.Fatalf("append ledger: %v", err)
	}
	if err := dataStore.LogTrade(ctx, &models.Trade{
		ID:         "TRADE_READY",
		Timestamp:  now,
		Symbol:     "RELIANCE",
		Exchange:   models.NSE,
		Side:       models.OrderSideBuy,
		Product:    models.ProductCNC,
		Quantity:   10,
		EntryPrice: 100,
		ExitPrice:  106,
		PnL:        60,
		PnLPercent: 6,
		Strategy:   "llm_hard_gates",
		OrderIDs:   []string{"PAPER_READY"},
		IsPaper:    true,
	}); err != nil {
		t.Fatalf("log trade: %v", err)
	}
}
