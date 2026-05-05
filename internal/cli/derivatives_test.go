package cli

import (
	"math"
	"testing"
	"time"
)

func TestCalculateOptionGreeksRejectsMissingInputs(t *testing.T) {
	if _, err := calculateOptionGreeks(0, 19500, "CE", "2026-05-28", 14.5, 6.5, 0); err == nil {
		t.Fatal("expected missing spot to fail")
	}
	if _, err := calculateOptionGreeks(19600, 19500, "XX", "2026-05-28", 14.5, 6.5, 0); err == nil {
		t.Fatal("expected invalid option type to fail")
	}
}

func TestCalculateOptionGreeksProducesFiniteValues(t *testing.T) {
	expiry := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	got, err := calculateOptionGreeks(19600, 19500, "CE", expiry, 14.5, 6.5, 120)
	if err != nil {
		t.Fatalf("calculate greeks: %v", err)
	}

	values := []float64{got.Delta, got.Gamma, got.Theta, got.Vega, got.Rho, got.D1, got.D2, got.IntrinsicValue, got.TimeValue}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			t.Fatalf("expected finite greek value, got %#v", got)
		}
	}
	if got.Delta <= 0 || got.Delta >= 1 {
		t.Fatalf("expected call delta between 0 and 1, got %.4f", got.Delta)
	}
	if got.IntrinsicValue != 100 {
		t.Fatalf("expected intrinsic value 100, got %.2f", got.IntrinsicValue)
	}
}
