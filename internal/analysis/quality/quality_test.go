package quality

import (
	"strings"
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestValidateCandlesPassesValidIntradaySeries(t *testing.T) {
	candles := qualityCandles(time.Date(2026, 5, 5, 9, 15, 0, 0, ist()), 15*time.Minute, 20)
	report := ValidateCandles(candles, Config{
		Symbol:         "RELIANCE",
		Exchange:       models.NSE,
		Timeframe:      "15min",
		MinCandles:     20,
		Now:            candles[len(candles)-1].Timestamp.Add(15 * time.Minute),
		CheckStaleness: true,
	})
	if !report.Valid() {
		t.Fatalf("expected valid report, got %s", report.Error())
	}
}

func TestValidateCandlesRejectsInvalidOHLC(t *testing.T) {
	candles := qualityCandles(time.Date(2026, 5, 5, 9, 15, 0, 0, ist()), 15*time.Minute, 5)
	candles[2].High = candles[2].Low - 1

	report := ValidateCandles(candles, Config{Timeframe: "15min", MinCandles: 5})
	if report.Valid() || !strings.Contains(report.Error(), "ohlcv_shape") {
		t.Fatalf("expected ohlcv failure, got %+v", report)
	}
}

func TestValidateCandlesRejectsSameSessionGap(t *testing.T) {
	candles := qualityCandles(time.Date(2026, 5, 5, 9, 15, 0, 0, ist()), 15*time.Minute, 5)
	candles[3].Timestamp = candles[2].Timestamp.Add(45 * time.Minute)
	candles[4].Timestamp = candles[3].Timestamp.Add(15 * time.Minute)

	report := ValidateCandles(candles, Config{Timeframe: "15min", MinCandles: 5})
	if report.Valid() || !strings.Contains(report.Error(), "missing_candles") {
		t.Fatalf("expected missing candle failure, got %+v", report)
	}
}

func TestValidateCandlesRejectsStaleIntradayDuringMarket(t *testing.T) {
	candles := qualityCandles(time.Date(2026, 5, 5, 9, 15, 0, 0, ist()), 15*time.Minute, 5)
	report := ValidateCandles(candles, Config{
		Timeframe:      "15min",
		MinCandles:     5,
		Now:            time.Date(2026, 5, 5, 12, 0, 0, 0, ist()),
		CheckStaleness: true,
	})
	if report.Valid() || !strings.Contains(report.Error(), "staleness") {
		t.Fatalf("expected staleness failure, got %+v", report)
	}
}

func TestValidateCandlesRejectsLatestZeroVolume(t *testing.T) {
	candles := qualityCandles(time.Date(2026, 5, 5, 9, 15, 0, 0, ist()), 15*time.Minute, 5)
	candles[len(candles)-1].Volume = 0

	report := ValidateCandles(candles, Config{Timeframe: "15min", MinCandles: 5})
	if report.Valid() || !strings.Contains(report.Error(), "volume") {
		t.Fatalf("expected volume failure, got %+v", report)
	}
}

func TestValidateCandlesRejectsIntradaySessionMismatch(t *testing.T) {
	candles := qualityCandles(time.Date(2026, 5, 5, 8, 0, 0, 0, ist()), 15*time.Minute, 5)

	report := ValidateCandles(candles, Config{Timeframe: "15min", MinCandles: 5})
	if report.Valid() || !strings.Contains(report.Error(), "session") {
		t.Fatalf("expected session failure, got %+v", report)
	}
}

func qualityCandles(start time.Time, step time.Duration, count int) []models.Candle {
	candles := make([]models.Candle, count)
	for i := range candles {
		price := 100 + float64(i)
		candles[i] = models.Candle{
			Timestamp: start.Add(time.Duration(i) * step),
			Open:      price,
			High:      price + 2,
			Low:       price - 2,
			Close:     price + 1,
			Volume:    1000,
		}
	}
	return candles
}

func ist() *time.Location {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	return loc
}
