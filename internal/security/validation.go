// Package security provides credential encryption, audit logging, and security controls.
package security

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Validation patterns
var (
	// Symbol pattern: uppercase letters, numbers, and limited special chars
	symbolPattern = regexp.MustCompile(`^[A-Z0-9&-]{1,20}$`)

	// Order ID pattern: alphanumeric with limited special chars
	orderIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,50}$`)

	// Watchlist name pattern: alphanumeric with spaces and underscores
	watchlistPattern = regexp.MustCompile(`^[A-Za-z0-9_ -]{1,50}$`)

	// API key patterns for detection (not validation)
	apiKeyPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(api[_-]?key|apikey|api[_-]?secret|secret[_-]?key|access[_-]?token|auth[_-]?token|bearer)[=:\s]+["']?([A-Za-z0-9_\-\.]{20,})["']?`),
		regexp.MustCompile(`(?i)(sk-[A-Za-z0-9]{20,})`),                    // OpenAI keys
		regexp.MustCompile(`(?i)(tvly-[A-Za-z0-9]{20,})`),                  // Tavily keys
		regexp.MustCompile(`(?i)([A-Za-z0-9]{32,})`),                       // Generic long tokens
	}

	// SQL injection patterns
	sqlInjectionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(union\s+select|select\s+\*|drop\s+table|insert\s+into|delete\s+from|update\s+.*\s+set)`),
		regexp.MustCompile(`(?i)(--|;|'|"|\\x00|\\n|\\r)`),
		regexp.MustCompile(`(?i)(or\s+1\s*=\s*1|and\s+1\s*=\s*1)`),
	}

	// Command injection patterns
	cmdInjectionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`[;&|$\x60]`),
		regexp.MustCompile(`(?i)(rm\s+-rf|wget|curl|bash|sh\s+-c|eval|exec)`),
	}
)

// ValidationError represents a validation error.
type ValidationError struct {
	Field   string
	Value   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error for %s: %s", e.Field, e.Message)
}

// InputValidator provides input validation functionality.
type InputValidator struct {
	strictMode bool
}

// NewInputValidator creates a new input validator.
func NewInputValidator(strictMode bool) *InputValidator {
	return &InputValidator{strictMode: strictMode}
}

// ValidateSymbol validates a stock symbol.
func (v *InputValidator) ValidateSymbol(symbol string) error {
	symbol = strings.TrimSpace(strings.ToUpper(symbol))

	if symbol == "" {
		return &ValidationError{Field: "symbol", Value: symbol, Message: "symbol cannot be empty"}
	}

	if len(symbol) > 20 {
		return &ValidationError{Field: "symbol", Value: symbol, Message: "symbol too long (max 20 characters)"}
	}

	if !symbolPattern.MatchString(symbol) {
		return &ValidationError{Field: "symbol", Value: symbol, Message: "invalid symbol format"}
	}

	// Check for injection attempts
	if v.containsInjection(symbol) {
		return &ValidationError{Field: "symbol", Value: symbol, Message: "invalid characters detected"}
	}

	return nil
}

// ValidateOrderID validates an order ID.
func (v *InputValidator) ValidateOrderID(orderID string) error {
	orderID = strings.TrimSpace(orderID)

	if orderID == "" {
		return &ValidationError{Field: "order_id", Value: orderID, Message: "order ID cannot be empty"}
	}

	if len(orderID) > 50 {
		return &ValidationError{Field: "order_id", Value: orderID, Message: "order ID too long (max 50 characters)"}
	}

	if !orderIDPattern.MatchString(orderID) {
		return &ValidationError{Field: "order_id", Value: orderID, Message: "invalid order ID format"}
	}

	return nil
}

// ValidateWatchlistName validates a watchlist name.
func (v *InputValidator) ValidateWatchlistName(name string) error {
	name = strings.TrimSpace(name)

	if name == "" {
		return &ValidationError{Field: "watchlist_name", Value: name, Message: "watchlist name cannot be empty"}
	}

	if len(name) > 50 {
		return &ValidationError{Field: "watchlist_name", Value: name, Message: "watchlist name too long (max 50 characters)"}
	}

	if !watchlistPattern.MatchString(name) {
		return &ValidationError{Field: "watchlist_name", Value: name, Message: "invalid watchlist name format"}
	}

	// Check for injection attempts
	if v.containsInjection(name) {
		return &ValidationError{Field: "watchlist_name", Value: name, Message: "invalid characters detected"}
	}

	return nil
}

// ValidateQuantity validates a trade quantity.
func (v *InputValidator) ValidateQuantity(qty int) error {
	if qty <= 0 {
		return &ValidationError{Field: "quantity", Value: fmt.Sprintf("%d", qty), Message: "quantity must be positive"}
	}

	if qty > 10000000 { // 1 crore max
		return &ValidationError{Field: "quantity", Value: fmt.Sprintf("%d", qty), Message: "quantity exceeds maximum allowed"}
	}

	return nil
}

// ValidatePrice validates a price value.
func (v *InputValidator) ValidatePrice(price float64) error {
	if price < 0 {
		return &ValidationError{Field: "price", Value: fmt.Sprintf("%.2f", price), Message: "price cannot be negative"}
	}

	if price > 1000000000 { // 100 crore max
		return &ValidationError{Field: "price", Value: fmt.Sprintf("%.2f", price), Message: "price exceeds maximum allowed"}
	}

	return nil
}

// ValidateText validates free-form text input.
func (v *InputValidator) ValidateText(field, text string, maxLen int) error {
	if len(text) > maxLen {
		return &ValidationError{Field: field, Value: text[:50] + "...", Message: fmt.Sprintf("text too long (max %d characters)", maxLen)}
	}

	// Check for injection attempts in strict mode
	if v.strictMode && v.containsInjection(text) {
		return &ValidationError{Field: field, Value: MaskSensitive(text), Message: "potentially dangerous content detected"}
	}

	return nil
}

// containsInjection checks for SQL or command injection patterns.
func (v *InputValidator) containsInjection(input string) bool {
	// Check SQL injection patterns
	for _, pattern := range sqlInjectionPatterns {
		if pattern.MatchString(input) {
			return true
		}
	}

	// Check command injection patterns
	for _, pattern := range cmdInjectionPatterns {
		if pattern.MatchString(input) {
			return true
		}
	}

	return false
}

// SanitizeSymbol sanitizes a symbol input.
func SanitizeSymbol(symbol string) string {
	// Convert to uppercase and trim
	symbol = strings.TrimSpace(strings.ToUpper(symbol))

	// Remove any non-alphanumeric characters except & and -
	var result strings.Builder
	for _, r := range symbol {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '&' || r == '-' {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// SanitizeText sanitizes free-form text by removing potentially dangerous characters.
func SanitizeText(text string) string {
	// Remove null bytes and other control characters
	var result strings.Builder
	for _, r := range text {
		if r >= 32 && r != 127 { // Printable ASCII and Unicode
			result.WriteRune(r)
		}
	}
	return result.String()
}

// MaskSensitive masks sensitive data in a string.
func MaskSensitive(input string) string {
	result := input

	// Mask API keys and tokens
	for _, pattern := range apiKeyPatterns {
		result = pattern.ReplaceAllStringFunc(result, func(match string) string {
			if len(match) > 8 {
				return match[:4] + strings.Repeat("*", len(match)-8) + match[len(match)-4:]
			}
			return strings.Repeat("*", len(match))
		})
	}

	return result
}

// MaskCredential masks a credential value for logging.
func MaskCredential(value string) string {
	if len(value) == 0 {
		return ""
	}
	if len(value) <= 4 {
		return strings.Repeat("*", len(value))
	}
	if len(value) <= 8 {
		return value[:2] + strings.Repeat("*", len(value)-2)
	}
	return value[:4] + strings.Repeat("*", len(value)-8) + value[len(value)-4:]
}

// ContainsSensitiveData checks if a string contains sensitive data patterns.
func ContainsSensitiveData(input string) bool {
	for _, pattern := range apiKeyPatterns {
		if pattern.MatchString(input) {
			return true
		}
	}
	return false
}
