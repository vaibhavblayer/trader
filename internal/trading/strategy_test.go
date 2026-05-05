package trading

import (
	"context"
	"strings"
	"testing"
)

func TestDefaultStrategyRegistryHasDefinitions(t *testing.T) {
	defs := AvailableStrategyDefinitions()
	if len(defs) == 0 {
		t.Fatal("expected built-in strategy definitions")
	}

	for _, def := range defs {
		if def.Name == "" || def.Description == "" || def.Category == "" {
			t.Fatalf("strategy %q has incomplete metadata", def.Name)
		}
		if len(def.Gates) == 0 {
			t.Fatalf("strategy %s has no gates", def.Name)
		}
		if len(def.Metrics) == 0 {
			t.Fatalf("strategy %s has no metrics", def.Name)
		}
		if def.Warmup(nil) < 0 {
			t.Fatalf("strategy %s has invalid warmup", def.Name)
		}
	}
}

func TestAvailableStrategiesIncludesBaseline(t *testing.T) {
	names := strings.Join(AvailableStrategies(), ",")
	if !strings.Contains(names, "buy_and_hold") {
		t.Fatalf("expected buy_and_hold in available strategies, got %s", names)
	}
}

func TestBacktestRejectsUnknownStrategy(t *testing.T) {
	engine := NewBacktestEngine(nil)
	_, err := engine.RunOnCandles(context.Background(), BacktestConfig{
		Symbol:         "TEST",
		InitialCapital: 100000,
		Strategy:       "typo_strategy",
	}, backtestCandles([]float64{100, 101}, 1000))
	if err == nil {
		t.Fatal("expected unknown strategy to be rejected")
	}
	if !strings.Contains(err.Error(), "unknown strategy") {
		t.Fatalf("expected unknown strategy error, got %v", err)
	}
}
