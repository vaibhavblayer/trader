// Package quality validates market data before analysis, backtesting, or execution logic uses it.
package quality

import (
	"fmt"
	"math"
	"strings"
	"time"

	"zerodha-trader/internal/models"
)

// Severity is the outcome of a data-quality gate.
type Severity string

const (
	SeverityPass Severity = "PASS"
	SeverityWarn Severity = "WARN"
	SeverityFail Severity = "FAIL"
)

// Gate is one data-quality check.
type Gate struct {
	Name     string
	Severity Severity
	Message  string
}

// Report is the aggregate result of data-quality validation.
type Report struct {
	Symbol    string
	Exchange  models.Exchange
	Timeframe string
	Gates     []Gate
}

// Valid returns true when no gate failed.
func (r Report) Valid() bool {
	for _, gate := range r.Gates {
		if gate.Severity == SeverityFail {
			return false
		}
	}
	return true
}

// Error summarizes failed gates.
func (r Report) Error() string {
	var failures []string
	for _, gate := range r.Gates {
		if gate.Severity == SeverityFail {
			failures = append(failures, fmt.Sprintf("%s: %s", gate.Name, gate.Message))
		}
	}
	if len(failures) == 0 {
		return ""
	}
	return strings.Join(failures, "; ")
}

// Config controls candle data-quality validation.
type Config struct {
	Symbol          string
	Exchange        models.Exchange
	Timeframe       string
	MinCandles      int
	Now             time.Time
	CheckStaleness  bool
	MaxStaleness    time.Duration
	AllowZeroVolume bool
}

// ValidateCandles validates OHLCV consistency, gaps, staleness, and session alignment.
func ValidateCandles(candles []models.Candle, cfg Config) Report {
	cfg = normalizeConfig(cfg)
	report := Report{Symbol: cfg.Symbol, Exchange: cfg.Exchange, Timeframe: cfg.Timeframe}

	if len(candles) < cfg.MinCandles {
		report.fail("min_candles", "need at least %d candles, got %d", cfg.MinCandles, len(candles))
		return report
	}
	report.pass("min_candles", "got %d candles", len(candles))

	tfDuration, hasDuration := TimeframeDuration(cfg.Timeframe)
	zeroVolumes := 0
	for i, candle := range candles {
		if err := validateCandleShape(candle); err != nil {
			report.fail("ohlcv_shape", "candle %d invalid: %v", i, err)
			return report
		}
		if candle.Volume <= 0 {
			zeroVolumes++
		}
		if i > 0 {
			prev := candles[i-1]
			if !candle.Timestamp.After(prev.Timestamp) {
				report.fail("timestamp_order", "candle %d timestamp %s is not after previous %s",
					i, candle.Timestamp.Format(time.RFC3339), prev.Timestamp.Format(time.RFC3339))
				return report
			}
			if hasDuration && isSameISTTradingDate(prev.Timestamp, candle.Timestamp) {
				gap := candle.Timestamp.Sub(prev.Timestamp)
				if gap > tfDuration+tfDuration/2 {
					report.fail("missing_candles", "gap %s between %s and %s exceeds %s timeframe",
						gap, prev.Timestamp.Format(time.RFC3339), candle.Timestamp.Format(time.RFC3339), cfg.Timeframe)
					return report
				}
			}
		}
	}
	report.pass("ohlcv_shape", "prices and ranges are valid")
	report.pass("timestamp_order", "timestamps are strictly increasing")
	report.pass("missing_candles", "no same-session gaps detected")

	if zeroVolumes > 0 {
		ratio := float64(zeroVolumes) / float64(len(candles))
		if !cfg.AllowZeroVolume && candles[len(candles)-1].Volume <= 0 {
			report.fail("volume", "latest candle has non-positive volume")
			return report
		}
		if !cfg.AllowZeroVolume && ratio > 0.10 {
			report.fail("volume", "%.1f%% of candles have non-positive volume", ratio*100)
			return report
		}
		report.warn("volume", "%d candles have non-positive volume", zeroVolumes)
	} else {
		report.pass("volume", "volume is positive")
	}

	if cfg.CheckStaleness {
		validateStaleness(&report, candles[len(candles)-1], cfg, tfDuration, hasDuration)
	}
	validateSession(&report, candles[len(candles)-1], cfg.Timeframe)

	return report
}

// TimeframeDuration returns a candle duration for supported intraday timeframes.
func TimeframeDuration(timeframe string) (time.Duration, bool) {
	switch strings.ToLower(strings.TrimSpace(timeframe)) {
	case "1min", "minute", "1minute":
		return time.Minute, true
	case "3min", "3minute":
		return 3 * time.Minute, true
	case "5min", "5minute":
		return 5 * time.Minute, true
	case "10min", "10minute":
		return 10 * time.Minute, true
	case "15min", "15minute":
		return 15 * time.Minute, true
	case "30min", "30minute":
		return 30 * time.Minute, true
	case "1hour", "60min", "60minute":
		return time.Hour, true
	case "1day", "day":
		return 24 * time.Hour, true
	default:
		return 0, false
	}
}

func normalizeConfig(cfg Config) Config {
	cfg.Symbol = strings.ToUpper(strings.TrimSpace(cfg.Symbol))
	if cfg.Timeframe == "" {
		cfg.Timeframe = "1day"
	}
	if cfg.MinCandles <= 0 {
		cfg.MinCandles = 1
	}
	if cfg.Now.IsZero() {
		cfg.Now = time.Now()
	}
	if cfg.MaxStaleness <= 0 {
		if d, ok := TimeframeDuration(cfg.Timeframe); ok {
			if d >= 24*time.Hour {
				cfg.MaxStaleness = 96 * time.Hour
			} else {
				cfg.MaxStaleness = 3 * d
			}
		}
	}
	return cfg
}

func validateCandleShape(c models.Candle) error {
	if c.Timestamp.IsZero() {
		return fmt.Errorf("timestamp is zero")
	}
	if c.Open <= 0 || c.High <= 0 || c.Low <= 0 || c.Close <= 0 {
		return fmt.Errorf("OHLC must be positive")
	}
	if math.IsNaN(c.Open) || math.IsNaN(c.High) || math.IsNaN(c.Low) || math.IsNaN(c.Close) {
		return fmt.Errorf("OHLC contains NaN")
	}
	if c.High < c.Low {
		return fmt.Errorf("high %.2f below low %.2f", c.High, c.Low)
	}
	if c.High < c.Open || c.High < c.Close {
		return fmt.Errorf("high %.2f below open/close", c.High)
	}
	if c.Low > c.Open || c.Low > c.Close {
		return fmt.Errorf("low %.2f above open/close", c.Low)
	}
	if c.Volume < 0 {
		return fmt.Errorf("volume is negative")
	}
	return nil
}

func validateStaleness(report *Report, last models.Candle, cfg Config, tfDuration time.Duration, hasDuration bool) {
	now := cfg.Now
	if now.Before(last.Timestamp) {
		report.warn("staleness", "latest candle timestamp is in the future")
		return
	}
	if isIntraday(cfg.Timeframe) {
		session := marketSessionAt(now)
		if session != "NORMAL" && session != "CLOSING" {
			report.pass("staleness", "market is not in normal trading session")
			return
		}
	}
	maxStaleness := cfg.MaxStaleness
	if maxStaleness <= 0 && hasDuration {
		maxStaleness = 3 * tfDuration
	}
	if maxStaleness > 0 && now.Sub(last.Timestamp) > maxStaleness {
		report.fail("staleness", "latest candle %s is stale by %s", last.Timestamp.Format(time.RFC3339), now.Sub(last.Timestamp).Round(time.Second))
		return
	}
	report.pass("staleness", "latest candle is fresh")
}

func validateSession(report *Report, last models.Candle, timeframe string) {
	if !isIntraday(timeframe) {
		report.pass("session", "daily timeframe does not require intraday session alignment")
		return
	}
	session := marketSessionAt(last.Timestamp)
	switch session {
	case "NORMAL", "CLOSING":
		report.pass("session", "latest candle aligns with market session")
	default:
		report.fail("session", "latest candle timestamp %s is outside normal trading session (%s)",
			last.Timestamp.Format(time.RFC3339), session)
	}
}

func marketSessionAt(t time.Time) string {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	t = t.In(loc)
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return "CLOSED"
	}
	minutes := t.Hour()*60 + t.Minute()
	switch {
	case minutes >= 9*60 && minutes < 9*60+8:
		return "PRE_OPEN"
	case minutes >= 9*60+8 && minutes < 9*60+15:
		return "PRE_OPEN_MATCH"
	case minutes >= 9*60+15 && minutes < 15*60+20:
		return "NORMAL"
	case minutes >= 15*60+20 && minutes < 15*60+30:
		return "CLOSING"
	case minutes >= 15*60+40 && minutes < 16*60:
		return "POST_CLOSE"
	default:
		return "CLOSED"
	}
}

func isIntraday(timeframe string) bool {
	d, ok := TimeframeDuration(timeframe)
	return ok && d < 24*time.Hour
}

func isSameISTTradingDate(a, b time.Time) bool {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	aa := a.In(loc)
	bb := b.In(loc)
	return aa.Year() == bb.Year() && aa.YearDay() == bb.YearDay()
}

func (r *Report) pass(name, format string, args ...interface{}) {
	r.Gates = append(r.Gates, Gate{Name: name, Severity: SeverityPass, Message: fmt.Sprintf(format, args...)})
}

func (r *Report) warn(name, format string, args ...interface{}) {
	r.Gates = append(r.Gates, Gate{Name: name, Severity: SeverityWarn, Message: fmt.Sprintf(format, args...)})
}

func (r *Report) fail(name, format string, args ...interface{}) {
	r.Gates = append(r.Gates, Gate{Name: name, Severity: SeverityFail, Message: fmt.Sprintf(format, args...)})
}
