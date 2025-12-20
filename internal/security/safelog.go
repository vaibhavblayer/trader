// Package security provides credential encryption, audit logging, and security controls.
package security

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rs/zerolog"
)

// sensitiveFields contains field names that should be masked in logs.
var sensitiveFields = map[string]bool{
	"api_key":      true,
	"api_secret":   true,
	"apikey":       true,
	"apisecret":    true,
	"secret":       true,
	"password":     true,
	"token":        true,
	"access_token": true,
	"auth_token":   true,
	"bearer":       true,
	"credential":   true,
	"credentials":  true,
	"private_key":  true,
	"secret_key":   true,
}

// sensitivePatterns contains regex patterns for sensitive data.
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|api[_-]?secret|secret[_-]?key|access[_-]?token|auth[_-]?token|bearer|password)[=:\s]+["']?([^\s"']+)["']?`),
	regexp.MustCompile(`(?i)(sk-[A-Za-z0-9]{20,})`),   // OpenAI keys
	regexp.MustCompile(`(?i)(tvly-[A-Za-z0-9]{20,})`), // Tavily keys
}

// SafeLogger wraps zerolog.Logger to automatically mask sensitive data.
type SafeLogger struct {
	logger zerolog.Logger
}

// NewSafeLogger creates a new safe logger that masks sensitive data.
func NewSafeLogger(logger zerolog.Logger) *SafeLogger {
	return &SafeLogger{logger: logger}
}

// Debug logs a debug message with sensitive data masked.
func (sl *SafeLogger) Debug() *SafeEvent {
	return &SafeEvent{event: sl.logger.Debug()}
}

// Info logs an info message with sensitive data masked.
func (sl *SafeLogger) Info() *SafeEvent {
	return &SafeEvent{event: sl.logger.Info()}
}

// Warn logs a warning message with sensitive data masked.
func (sl *SafeLogger) Warn() *SafeEvent {
	return &SafeEvent{event: sl.logger.Warn()}
}

// Error logs an error message with sensitive data masked.
func (sl *SafeLogger) Error() *SafeEvent {
	return &SafeEvent{event: sl.logger.Error()}
}

// Fatal logs a fatal message with sensitive data masked.
func (sl *SafeLogger) Fatal() *SafeEvent {
	return &SafeEvent{event: sl.logger.Fatal()}
}

// With creates a child logger with additional context.
func (sl *SafeLogger) With() *SafeContext {
	return &SafeContext{ctx: sl.logger.With()}
}

// SafeEvent wraps zerolog.Event to mask sensitive data.
type SafeEvent struct {
	event *zerolog.Event
}

// Str adds a string field, masking if sensitive.
func (se *SafeEvent) Str(key, val string) *SafeEvent {
	if isSensitiveField(key) {
		se.event = se.event.Str(key, MaskCredential(val))
	} else {
		se.event = se.event.Str(key, maskSensitiveInString(val))
	}
	return se
}

// Int adds an integer field.
func (se *SafeEvent) Int(key string, val int) *SafeEvent {
	se.event = se.event.Int(key, val)
	return se
}

// Int64 adds an int64 field.
func (se *SafeEvent) Int64(key string, val int64) *SafeEvent {
	se.event = se.event.Int64(key, val)
	return se
}

// Float64 adds a float64 field.
func (se *SafeEvent) Float64(key string, val float64) *SafeEvent {
	se.event = se.event.Float64(key, val)
	return se
}

// Bool adds a boolean field.
func (se *SafeEvent) Bool(key string, val bool) *SafeEvent {
	se.event = se.event.Bool(key, val)
	return se
}

// Err adds an error field, masking sensitive data in the error message.
func (se *SafeEvent) Err(err error) *SafeEvent {
	if err != nil {
		maskedErr := fmt.Errorf("%s", maskSensitiveInString(err.Error()))
		se.event = se.event.Err(maskedErr)
	}
	return se
}

// Interface adds an interface field.
func (se *SafeEvent) Interface(key string, val interface{}) *SafeEvent {
	// Convert to string and mask if needed
	strVal := fmt.Sprintf("%v", val)
	if isSensitiveField(key) || ContainsSensitiveData(strVal) {
		se.event = se.event.Str(key, maskSensitiveInString(strVal))
	} else {
		se.event = se.event.Interface(key, val)
	}
	return se
}

// Msg sends the event with a message.
func (se *SafeEvent) Msg(msg string) {
	se.event.Msg(maskSensitiveInString(msg))
}

// Msgf sends the event with a formatted message.
func (se *SafeEvent) Msgf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	se.event.Msg(maskSensitiveInString(msg))
}

// Send sends the event without a message.
func (se *SafeEvent) Send() {
	se.event.Send()
}

// SafeContext wraps zerolog.Context to mask sensitive data.
type SafeContext struct {
	ctx zerolog.Context
}

// Str adds a string field to the context, masking if sensitive.
func (sc *SafeContext) Str(key, val string) *SafeContext {
	if isSensitiveField(key) {
		sc.ctx = sc.ctx.Str(key, MaskCredential(val))
	} else {
		sc.ctx = sc.ctx.Str(key, maskSensitiveInString(val))
	}
	return sc
}

// Int adds an integer field to the context.
func (sc *SafeContext) Int(key string, val int) *SafeContext {
	sc.ctx = sc.ctx.Int(key, val)
	return sc
}

// Logger returns the logger with the context applied.
func (sc *SafeContext) Logger() *SafeLogger {
	return &SafeLogger{logger: sc.ctx.Logger()}
}

// isSensitiveField checks if a field name is sensitive.
func isSensitiveField(field string) bool {
	return sensitiveFields[strings.ToLower(field)]
}

// maskSensitiveInString masks sensitive patterns in a string.
func maskSensitiveInString(input string) string {
	result := input

	for _, pattern := range sensitivePatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			// Find the sensitive part and mask it
			parts := strings.SplitN(match, "=", 2)
			if len(parts) == 2 {
				return parts[0] + "=" + MaskCredential(strings.Trim(parts[1], "\"' "))
			}
			parts = strings.SplitN(match, ":", 2)
			if len(parts) == 2 {
				return parts[0] + ":" + MaskCredential(strings.Trim(parts[1], "\"' "))
			}
			// For patterns like sk-xxx, mask the whole thing
			return MaskCredential(match)
		})
	}

	return result
}

// LogWithoutCredentials creates a copy of a map with credentials masked.
func LogWithoutCredentials(data map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range data {
		if isSensitiveField(k) {
			if strVal, ok := v.(string); ok {
				result[k] = MaskCredential(strVal)
			} else {
				result[k] = "***"
			}
		} else if strVal, ok := v.(string); ok {
			result[k] = maskSensitiveInString(strVal)
		} else {
			result[k] = v
		}
	}
	return result
}
