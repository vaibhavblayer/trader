package cli

import (
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestEvaluatePaperPredictionFromCandlesTargetHit(t *testing.T) {
	created := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	prediction := models.PaperPrediction{
		ID:          "PRED_TARGET",
		Symbol:      "RELIANCE",
		Action:      "BUY",
		EntryPrice:  100,
		TargetPrice: 105,
		StopLoss:    98,
		TimeWindow:  time.Hour,
		CreatedAt:   created,
		ExpiresAt:   created.Add(time.Hour),
		SetupName:   "candidate:test",
		Timeframe:   "15min",
	}
	candles := []models.Candle{
		{Timestamp: created.Add(15 * time.Minute), Open: 101, High: 105.5, Low: 100.5, Close: 105},
	}

	got, ok, reason := evaluatePaperPredictionFromCandles(prediction, candles, created.Add(30*time.Minute), 15*time.Minute)
	if !ok || got.Outcome != "RIGHT" || got.ExitPrice != 105 || reason != "target_hit" {
		t.Fatalf("expected target hit, got ok=%v reason=%s prediction=%#v", ok, reason, got)
	}
	if got.PnLPercent != 5 {
		t.Fatalf("expected 5%% pnl, got %.2f", got.PnLPercent)
	}
}

func TestEvaluatePaperPredictionFromCandlesAmbiguousIsConservativeLoss(t *testing.T) {
	created := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	prediction := models.PaperPrediction{
		ID:          "PRED_AMBIGUOUS",
		Symbol:      "RELIANCE",
		Action:      "BUY",
		EntryPrice:  100,
		TargetPrice: 105,
		StopLoss:    98,
		TimeWindow:  time.Hour,
		CreatedAt:   created,
		ExpiresAt:   created.Add(time.Hour),
		SetupName:   "candidate:test",
		Timeframe:   "15min",
	}
	candles := []models.Candle{
		{Timestamp: created.Add(15 * time.Minute), Open: 101, High: 106, Low: 97, Close: 104},
	}

	got, ok, reason := evaluatePaperPredictionFromCandles(prediction, candles, created.Add(30*time.Minute), 15*time.Minute)
	if !ok || got.Outcome != "WRONG" || got.ExitPrice != 98 || reason != "target_and_stop_same_candle_conservative_stop" {
		t.Fatalf("expected conservative loss, got ok=%v reason=%s prediction=%#v", ok, reason, got)
	}
}

func TestEvaluatePaperPredictionFromCandlesExpiredUsesLastClose(t *testing.T) {
	created := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)
	prediction := models.PaperPrediction{
		ID:          "PRED_EXPIRED",
		Symbol:      "INFY",
		Action:      "SELL",
		EntryPrice:  100,
		TargetPrice: 95,
		StopLoss:    103,
		TimeWindow:  30 * time.Minute,
		CreatedAt:   created,
		ExpiresAt:   created.Add(30 * time.Minute),
		SetupName:   "candidate:test",
		Timeframe:   "15min",
	}
	candles := []models.Candle{
		{Timestamp: created.Add(15 * time.Minute), Open: 100, High: 101, Low: 98, Close: 99},
	}

	got, ok, reason := evaluatePaperPredictionFromCandles(prediction, candles, created.Add(time.Hour), 15*time.Minute)
	if !ok || got.Outcome != "EXPIRED" || got.ExitPrice != 99 || reason != "expired_without_target_or_stop" {
		t.Fatalf("expected expired outcome, got ok=%v reason=%s prediction=%#v", ok, reason, got)
	}
	if got.PnLPercent != 1 {
		t.Fatalf("expected 1%% pnl for sell expiry, got %.2f", got.PnLPercent)
	}
}
