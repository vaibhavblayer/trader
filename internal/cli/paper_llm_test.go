package cli

import (
	"testing"
	"time"
)

func TestParsePredictionResponseRejectsInvalidTradeGeometry(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{
			name: "buy target below entry",
			response: `{
				"action": "BUY",
				"confidence": 80,
				"target_price": 99,
				"stop_loss": 98,
				"reasoning": "invalid"
			}`,
		},
		{
			name: "sell stop below entry",
			response: `{
				"action": "SELL",
				"confidence": 80,
				"target_price": 98,
				"stop_loss": 99,
				"reasoning": "invalid"
			}`,
		},
		{
			name: "no trade",
			response: `{
				"action": "NO_TRADE",
				"confidence": 80,
				"target_price": 0,
				"stop_loss": 0,
				"reasoning": "avoid"
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prediction, err := parsePredictionResponse(tt.response, "RELIANCE", 100, 15*time.Minute)
			if err != nil {
				t.Fatalf("parsePredictionResponse returned error: %v", err)
			}
			if prediction != nil {
				t.Fatalf("expected nil prediction, got %#v", prediction)
			}
		})
	}
}

func TestParsePredictionResponseAcceptsValidTrade(t *testing.T) {
	response := `{
		"action": "buy",
		"confidence": 120,
		"hold_duration": "5m",
		"target_price": 102,
		"stop_loss": 99,
		"reasoning": "valid"
	}`

	prediction, err := parsePredictionResponse(response, "RELIANCE", 100, 15*time.Minute)
	if err != nil {
		t.Fatalf("parsePredictionResponse returned error: %v", err)
	}
	if prediction == nil {
		t.Fatal("expected prediction")
	}
	if prediction.Action != "BUY" {
		t.Fatalf("expected normalized BUY action, got %s", prediction.Action)
	}
	if prediction.Confidence != 100 {
		t.Fatalf("expected confidence clamp to 100, got %f", prediction.Confidence)
	}
	if prediction.TimeWindow != 5*time.Minute {
		t.Fatalf("expected 5m hold duration, got %s", prediction.TimeWindow)
	}
}

func TestParsePredictionResponseWithGatesRequiresAllGatesAndValidRisk(t *testing.T) {
	valid := `{
		"action": "SELL",
		"gates_passed": {
			"rsi_regime": true,
			"volume_expansion": true,
			"ema_alignment": true,
			"vwap_not_exhausted": true,
			"trend_strength": true
		},
		"signal_quality": {
			"rsi_value": 40,
			"rsi_direction": "falling",
			"volume_ratio": 1.8,
			"vwap_deviation_pct": -0.2,
			"adx_value": 28,
			"ema_trend": "bearish"
		},
		"confidence": 75,
		"hold_duration": "10m",
		"target_price": 98,
		"stop_loss": 101,
		"reasoning": "valid"
	}`

	prediction, err := parsePredictionResponseWithGates(valid, "RELIANCE", 100, 15*time.Minute)
	if err != nil {
		t.Fatalf("parsePredictionResponseWithGates returned error: %v", err)
	}
	if prediction == nil {
		t.Fatal("expected valid gated prediction")
	}

	invalidGate := `{
		"action": "SELL",
		"gates_passed": {
			"rsi_regime": true,
			"volume_expansion": false,
			"ema_alignment": true,
			"vwap_not_exhausted": true,
			"trend_strength": true
		},
		"confidence": 75,
		"target_price": 98,
		"stop_loss": 101
	}`

	prediction, err = parsePredictionResponseWithGates(invalidGate, "RELIANCE", 100, 15*time.Minute)
	if err != nil {
		t.Fatalf("parsePredictionResponseWithGates returned error: %v", err)
	}
	if prediction != nil {
		t.Fatalf("expected nil prediction when gate fails, got %#v", prediction)
	}
}
