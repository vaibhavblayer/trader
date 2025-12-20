package utils

import (
	"context"
	"math"
	"time"
)

// RetryConfig holds retry configuration.
type RetryConfig struct {
	MaxAttempts     int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	BackoffFactor   float64
	RetryableErrors []error
}

// DefaultRetryConfig returns the default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:   3,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      10 * time.Second,
		BackoffFactor: 2.0,
	}
}

// Retry executes a function with exponential backoff retry.
func Retry(ctx context.Context, cfg RetryConfig, fn func() error) error {
	var lastErr error
	delay := cfg.InitialDelay

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if err := fn(); err != nil {
			lastErr = err

			// Check if context is cancelled
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			// Don't sleep after the last attempt
			if attempt < cfg.MaxAttempts-1 {
				time.Sleep(delay)
				delay = time.Duration(float64(delay) * cfg.BackoffFactor)
				if delay > cfg.MaxDelay {
					delay = cfg.MaxDelay
				}
			}
		} else {
			return nil
		}
	}

	return lastErr
}

// RetryWithResult executes a function with exponential backoff retry and returns a result.
func RetryWithResult[T any](ctx context.Context, cfg RetryConfig, fn func() (T, error)) (T, error) {
	var lastErr error
	var zero T
	delay := cfg.InitialDelay

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		result, err := fn()
		if err != nil {
			lastErr = err

			// Check if context is cancelled
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			default:
			}

			// Don't sleep after the last attempt
			if attempt < cfg.MaxAttempts-1 {
				time.Sleep(delay)
				delay = time.Duration(float64(delay) * cfg.BackoffFactor)
				if delay > cfg.MaxDelay {
					delay = cfg.MaxDelay
				}
			}
		} else {
			return result, nil
		}
	}

	return zero, lastErr
}

// CalculateBackoff calculates the backoff duration for a given attempt.
func CalculateBackoff(attempt int, initialDelay, maxDelay time.Duration, factor float64) time.Duration {
	delay := float64(initialDelay) * math.Pow(factor, float64(attempt))
	if delay > float64(maxDelay) {
		delay = float64(maxDelay)
	}
	return time.Duration(delay)
}
