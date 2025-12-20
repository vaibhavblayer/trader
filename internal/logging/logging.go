// Package logging provides structured logging functionality.
package logging

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

// LogConfig holds logging configuration.
type LogConfig struct {
	Level      string
	Console    bool
	File       bool
	FilePath   string
	MaxSize    int // megabytes
	MaxBackups int
	MaxAge     int // days
}

// DefaultLogConfig returns the default logging configuration.
func DefaultLogConfig() LogConfig {
	home, _ := os.UserHomeDir()
	return LogConfig{
		Level:      "info",
		Console:    true,
		File:       true,
		FilePath:   filepath.Join(home, ".config", "zerodha-trader", "logs", "trader.log"),
		MaxSize:    100,
		MaxBackups: 7,
		MaxAge:     30,
	}
}

// NewLogger creates a new logger with default configuration.
func NewLogger() zerolog.Logger {
	return NewLoggerWithConfig(DefaultLogConfig())
}

// NewLoggerWithConfig creates a new logger with the specified configuration.
func NewLoggerWithConfig(cfg LogConfig) zerolog.Logger {
	var writers []io.Writer

	// Console writer
	if cfg.Console {
		consoleWriter := zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
			FormatLevel: func(i interface{}) string {
				if ll, ok := i.(string); ok {
					switch ll {
					case "debug":
						return "\033[36mDBG\033[0m"
					case "info":
						return "\033[32mINF\033[0m"
					case "warn":
						return "\033[33mWRN\033[0m"
					case "error":
						return "\033[31mERR\033[0m"
					default:
						return ll
					}
				}
				return "???"
			},
		}
		writers = append(writers, consoleWriter)
	}

	// File writer with rotation
	if cfg.File {
		// Ensure log directory exists
		logDir := filepath.Dir(cfg.FilePath)
		if err := os.MkdirAll(logDir, 0755); err == nil {
			fileWriter := &lumberjack.Logger{
				Filename:   cfg.FilePath,
				MaxSize:    cfg.MaxSize,
				MaxBackups: cfg.MaxBackups,
				MaxAge:     cfg.MaxAge,
				Compress:   true,
			}
			writers = append(writers, fileWriter)
		}
	}

	// Create multi-writer
	var writer io.Writer
	if len(writers) == 0 {
		writer = os.Stdout
	} else if len(writers) == 1 {
		writer = writers[0]
	} else {
		writer = zerolog.MultiLevelWriter(writers...)
	}

	// Set log level
	level := parseLevel(cfg.Level)
	zerolog.SetGlobalLevel(level)

	// Create logger
	logger := zerolog.New(writer).
		With().
		Timestamp().
		Caller().
		Logger()

	return logger
}

func parseLevel(level string) zerolog.Level {
	switch level {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

// SetDebugLevel sets the global log level to debug.
func SetDebugLevel() {
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
}

// SetInfoLevel sets the global log level to info.
func SetInfoLevel() {
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
}

// ContextKey is the type for context keys.
type ContextKey string

const (
	// LoggerKey is the context key for the logger.
	LoggerKey ContextKey = "logger"
	// RequestIDKey is the context key for request ID.
	RequestIDKey ContextKey = "request_id"
	// SymbolKey is the context key for symbol.
	SymbolKey ContextKey = "symbol"
)

// WithLogger adds a logger to the context.
func WithLogger(ctx context.Context, logger zerolog.Logger) context.Context {
	return context.WithValue(ctx, LoggerKey, logger)
}

// FromContext retrieves the logger from context.
func FromContext(ctx context.Context) zerolog.Logger {
	if logger, ok := ctx.Value(LoggerKey).(zerolog.Logger); ok {
		return logger
	}
	return zerolog.Nop()
}

// WithSymbol adds a symbol to the logger context.
func WithSymbol(logger zerolog.Logger, symbol string) zerolog.Logger {
	return logger.With().Str("symbol", symbol).Logger()
}

// WithOrderID adds an order ID to the logger context.
func WithOrderID(logger zerolog.Logger, orderID string) zerolog.Logger {
	return logger.With().Str("order_id", orderID).Logger()
}

// WithAgent adds an agent name to the logger context.
func WithAgent(logger zerolog.Logger, agentName string) zerolog.Logger {
	return logger.With().Str("agent", agentName).Logger()
}

// WithOperation adds an operation name to the logger context.
func WithOperation(logger zerolog.Logger, operation string) zerolog.Logger {
	return logger.With().Str("operation", operation).Logger()
}

// LogTrade logs a trade event.
func LogTrade(logger zerolog.Logger, symbol, side string, qty int, price float64) {
	logger.Info().
		Str("event", "trade").
		Str("symbol", symbol).
		Str("side", side).
		Int("quantity", qty).
		Float64("price", price).
		Msg("Trade executed")
}

// LogOrder logs an order event.
func LogOrder(logger zerolog.Logger, orderID, symbol, side, status string) {
	logger.Info().
		Str("event", "order").
		Str("order_id", orderID).
		Str("symbol", symbol).
		Str("side", side).
		Str("status", status).
		Msg("Order update")
}

// LogDecision logs an AI decision.
func LogDecision(logger zerolog.Logger, symbol, action string, confidence float64, reasoning string) {
	logger.Info().
		Str("event", "decision").
		Str("symbol", symbol).
		Str("action", action).
		Float64("confidence", confidence).
		Str("reasoning", reasoning).
		Msg("AI decision")
}

// LogAlert logs an alert trigger.
func LogAlert(logger zerolog.Logger, alertID, symbol, condition string, price float64) {
	logger.Info().
		Str("event", "alert").
		Str("alert_id", alertID).
		Str("symbol", symbol).
		Str("condition", condition).
		Float64("price", price).
		Msg("Alert triggered")
}

// LogAPICall logs an API call.
func LogAPICall(logger zerolog.Logger, method, endpoint string, duration time.Duration, err error) {
	event := logger.Debug().
		Str("event", "api_call").
		Str("method", method).
		Str("endpoint", endpoint).
		Dur("duration", duration)
	
	if err != nil {
		event.Err(err).Msg("API call failed")
	} else {
		event.Msg("API call completed")
	}
}
