package cli

import (
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestScanResultFromCandlesIncludesIntradayMetadata(t *testing.T) {
	last := time.Now().Add(-10 * time.Minute).Truncate(time.Minute)
	candles := discoveryTestCandles(last, 20, 15*time.Minute)

	result, ok := scanResultFromCandles("RELIANCE", candles, symbolDiscoveryOptions{
		Timeframe:    "15minute",
		MinCandles:   20,
		MaxCandleAge: 30 * time.Minute,
	})
	if !ok {
		t.Fatal("expected scan result")
	}
	if result.Timeframe != "15minute" || result.Candles != 20 {
		t.Fatalf("unexpected metadata: %#v", result)
	}
	if result.LastCandleAt == nil || !result.LastCandleAt.Equal(last) {
		t.Fatalf("unexpected last candle timestamp: %#v", result.LastCandleAt)
	}
	if result.LastCandleAgeMinutes < 9 || result.LastCandleAgeMinutes > 11 {
		t.Fatalf("unexpected candle age minutes: %d", result.LastCandleAgeMinutes)
	}
}

func TestScanResultFromCandlesRejectsStaleAndThinIntradayData(t *testing.T) {
	stale := discoveryTestCandles(time.Now().Add(-2*time.Hour), 20, 15*time.Minute)
	if _, reason, ok := scanResultFromCandlesWithReason("RELIANCE", stale, symbolDiscoveryOptions{
		Timeframe:    "15minute",
		MinCandles:   20,
		MaxCandleAge: 30 * time.Minute,
	}); ok || reason != scanRejectStale {
		t.Fatalf("expected stale candles to be rejected, got ok=%v reason=%s", ok, reason)
	}

	freshButThin := discoveryTestCandles(time.Now().Add(-10*time.Minute), 10, 15*time.Minute)
	if _, reason, ok := scanResultFromCandlesWithReason("RELIANCE", freshButThin, symbolDiscoveryOptions{
		Timeframe:    "15minute",
		MinCandles:   20,
		MaxCandleAge: 30 * time.Minute,
	}); ok || reason != scanRejectThin {
		t.Fatalf("expected thin candle history to be rejected, got ok=%v reason=%s", ok, reason)
	}
}

func discoveryTestCandles(last time.Time, count int, step time.Duration) []models.Candle {
	candles := make([]models.Candle, count)
	start := last.Add(-time.Duration(count-1) * step)
	for i := range candles {
		price := 100 + float64(i)
		candles[i] = models.Candle{
			Timestamp: start.Add(time.Duration(i) * step),
			Open:      price - 0.5,
			High:      price + 1,
			Low:       price - 1,
			Close:     price,
			Volume:    int64(1000 + i*10),
		}
	}
	return candles
}
