package broker

import (
	"context"
	"testing"
)

func TestPolicyBrokerBlocksOrdersWhenProfileDisallows(t *testing.T) {
	base := newFakeExecutionBroker()
	policy := NewPolicyBroker(base, ExecutionPolicy{Profile: "live-readonly"})

	if _, err := policy.PlaceOrder(context.Background(), testExecutionOrder()); err == nil {
		t.Fatal("expected order to be blocked")
	}
	if base.placeCalls != 0 {
		t.Fatalf("expected no broker submission, got %d", base.placeCalls)
	}
}

func TestPolicyBrokerAllowsOrdersWhenProfileAllows(t *testing.T) {
	base := newFakeExecutionBroker()
	policy := NewPolicyBroker(base, ExecutionPolicy{
		Profile:     "paper",
		AllowOrders: true,
	})

	if _, err := policy.PlaceOrder(context.Background(), testExecutionOrder()); err != nil {
		t.Fatalf("expected order to be allowed, got %v", err)
	}
	if base.placeCalls != 1 {
		t.Fatalf("expected one broker submission, got %d", base.placeCalls)
	}
}
