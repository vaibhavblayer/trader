package security

import (
	"context"
	"testing"
)

func TestLiveReadOnlyBlocksOrderButAllowsConfigChange(t *testing.T) {
	ac := NewProfileAccessController("live-readonly", false, nil)
	if err := ac.CheckPermission(context.Background(), OpPlaceOrder); err == nil {
		t.Fatal("expected live-readonly to block order placement")
	}
	if err := ac.CheckPermission(context.Background(), OpModifyConfig); err != nil {
		t.Fatalf("expected live-readonly to allow config changes, got %v", err)
	}
}

func TestPaperProfileAllowsOrder(t *testing.T) {
	ac := NewProfileAccessController("paper", false, nil)
	if err := ac.CheckPermission(context.Background(), OpPlaceOrder); err != nil {
		t.Fatalf("expected paper profile to allow paper orders, got %v", err)
	}
}

func TestBacktestProfileBlocksWrites(t *testing.T) {
	ac := NewProfileAccessController("backtest", false, nil)
	if err := ac.CheckPermission(context.Background(), OpSavePlan); err == nil {
		t.Fatal("expected backtest profile to block state writes")
	}
}
