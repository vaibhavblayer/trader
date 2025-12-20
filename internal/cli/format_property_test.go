// Package cli provides the command-line interface for the trading application.
package cli

import (
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// Property 9: Currency formatting produces correct Indian numbering
// **Validates: Requirements 21.6**
//
// For any non-negative amount, FormatIndianCurrency should:
// 1. Start with ₹ symbol
// 2. Have exactly 2 decimal places
// 3. Use Indian numbering system (groups of 2 after first 3 digits from right)
// 4. Preserve the numeric value when parsed back
func TestProperty9_IndianCurrencyFormatting(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	properties := gopter.NewProperties(parameters)

	// Property: FormatIndianCurrency produces valid Indian number format
	properties.Property("FormatIndianCurrency produces valid Indian format", prop.ForAll(
		func(amount float64) bool {
			// Skip NaN and Inf
			if math.IsNaN(amount) || math.IsInf(amount, 0) {
				return true
			}

			// Limit to reasonable range to avoid floating point issues
			if math.Abs(amount) > 1e15 {
				return true
			}

			formatted := FormatIndianCurrency(amount)

			// 1. Must start with ₹ (or -₹ for negative)
			if amount >= 0 {
				if !strings.HasPrefix(formatted, "₹") {
					t.Logf("Expected ₹ prefix for %f, got %s", amount, formatted)
					return false
				}
			} else {
				if !strings.HasPrefix(formatted, "-₹") {
					t.Logf("Expected -₹ prefix for %f, got %s", amount, formatted)
					return false
				}
			}

			// 2. Must have exactly 2 decimal places
			parts := strings.Split(formatted, ".")
			if len(parts) != 2 {
				t.Logf("Expected decimal point for %f, got %s", amount, formatted)
				return false
			}
			if len(parts[1]) != 2 {
				t.Logf("Expected 2 decimal places for %f, got %s", amount, formatted)
				return false
			}

			// 3. Verify Indian numbering pattern
			// Remove ₹ and - prefix, and decimal part
			numPart := strings.TrimPrefix(formatted, "-")
			numPart = strings.TrimPrefix(numPart, "₹")
			numPart = strings.Split(numPart, ".")[0]

			// Indian format: first group from right is 3 digits, then groups of 2
			// Pattern: optional groups of 2 digits with comma, then 1-3 digits
			indianPattern := regexp.MustCompile(`^(\d{1,2},)*\d{1,3}$`)
			if !indianPattern.MatchString(numPart) {
				t.Logf("Invalid Indian format for %f: %s (numPart: %s)", amount, formatted, numPart)
				return false
			}

			return true
		},
		gen.Float64Range(-1e12, 1e12),
	))

	// Property: FormatIndianCurrency preserves value (round-trip)
	properties.Property("FormatIndianCurrency preserves value", prop.ForAll(
		func(amount float64) bool {
			// Skip NaN and Inf
			if math.IsNaN(amount) || math.IsInf(amount, 0) {
				return true
			}

			// Limit to reasonable range
			if math.Abs(amount) > 1e12 {
				return true
			}

			formatted := FormatIndianCurrency(amount)

			// Parse back the value
			parsed := parseIndianCurrency(formatted)

			// Should be equal within 2 decimal places (due to formatting)
			roundedAmount := math.Round(amount*100) / 100
			diff := math.Abs(parsed - roundedAmount)

			if diff > 0.01 {
				t.Logf("Value not preserved: original=%f, formatted=%s, parsed=%f", amount, formatted, parsed)
				return false
			}

			return true
		},
		gen.Float64Range(-1e9, 1e9),
	))

	// Property: FormatIndianCurrency handles edge cases
	properties.Property("FormatIndianCurrency handles small amounts", prop.ForAll(
		func(amount float64) bool {
			// Test amounts less than 1000 (no comma needed)
			formatted := FormatIndianCurrency(amount)

			// Should still have ₹ prefix and 2 decimal places
			if amount >= 0 {
				if !strings.HasPrefix(formatted, "₹") {
					return false
				}
			}

			parts := strings.Split(formatted, ".")
			return len(parts) == 2 && len(parts[1]) == 2
		},
		gen.Float64Range(0, 999.99),
	))

	// Property: FormatPercent produces correct format
	properties.Property("FormatPercent produces correct format", prop.ForAll(
		func(value float64) bool {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return true
			}

			formatted := FormatPercent(value)

			// Must end with %
			if !strings.HasSuffix(formatted, "%") {
				t.Logf("Expected %% suffix for %f, got %s", value, formatted)
				return false
			}

			// Positive values should have + prefix
			if value > 0 && !strings.HasPrefix(formatted, "+") {
				t.Logf("Expected + prefix for positive %f, got %s", value, formatted)
				return false
			}

			return true
		},
		gen.Float64Range(-100, 100),
	))

	// Property: FormatCompact uses correct units
	properties.Property("FormatCompact uses correct units", prop.ForAll(
		func(amount float64) bool {
			if math.IsNaN(amount) || math.IsInf(amount, 0) {
				return true
			}

			formatted := FormatCompact(amount)
			absAmount := math.Abs(amount)

			// Check correct unit is used
			if absAmount >= 10000000 { // 1 crore
				if !strings.Contains(formatted, "Cr") {
					t.Logf("Expected Cr for %f, got %s", amount, formatted)
					return false
				}
			} else if absAmount >= 100000 { // 1 lakh
				if !strings.Contains(formatted, "L") {
					t.Logf("Expected L for %f, got %s", amount, formatted)
					return false
				}
			} else {
				// Should be regular currency format
				if !strings.HasPrefix(formatted, "₹") && !strings.HasPrefix(formatted, "-₹") {
					t.Logf("Expected ₹ for %f, got %s", amount, formatted)
					return false
				}
			}

			return true
		},
		gen.Float64Range(-1e10, 1e10),
	))

	// Property: FormatVolume uses correct units
	properties.Property("FormatVolume uses correct units", prop.ForAll(
		func(volume int64) bool {
			if volume < 0 {
				volume = -volume
			}

			formatted := FormatVolume(volume)

			if volume >= 10000000 { // 1 crore
				if !strings.Contains(formatted, "Cr") {
					t.Logf("Expected Cr for %d, got %s", volume, formatted)
					return false
				}
			} else if volume >= 100000 { // 1 lakh
				if !strings.Contains(formatted, "L") {
					t.Logf("Expected L for %d, got %s", volume, formatted)
					return false
				}
			} else if volume >= 1000 {
				if !strings.Contains(formatted, "K") {
					t.Logf("Expected K for %d, got %s", volume, formatted)
					return false
				}
			}

			return true
		},
		gen.Int64Range(0, 1e12),
	))

	properties.TestingRun(t)
}

// parseIndianCurrency parses an Indian currency formatted string back to float64
func parseIndianCurrency(s string) float64 {
	// Check for negative
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	
	// Remove ₹ symbol and commas
	s = strings.TrimPrefix(s, "₹")
	s = strings.ReplaceAll(s, ",", "")

	// Parse the number
	var parsed float64
	for i, c := range s {
		if c == '.' {
			// Parse decimal part
			decPart := s[i+1:]
			for j, d := range decPart {
				if d >= '0' && d <= '9' {
					parsed += float64(d-'0') / math.Pow(10, float64(j+1))
				}
			}
			break
		}
		if c >= '0' && c <= '9' {
			parsed = parsed*10 + float64(c-'0')
		}
	}

	if negative {
		parsed = -parsed
	}

	return parsed
}

// TestIndianNumberFormatExamples tests specific examples of Indian number formatting
func TestIndianNumberFormatExamples(t *testing.T) {
	testCases := []struct {
		amount   float64
		expected string
	}{
		{0, "₹0.00"},
		{1, "₹1.00"},
		{10, "₹10.00"},
		{100, "₹100.00"},
		{1000, "₹1,000.00"},
		{10000, "₹10,000.00"},
		{100000, "₹1,00,000.00"},      // 1 lakh
		{1000000, "₹10,00,000.00"},    // 10 lakhs
		{10000000, "₹1,00,00,000.00"}, // 1 crore
		{-1234.56, "-₹1,234.56"},
		{12345678.90, "₹1,23,45,678.90"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			result := FormatIndianCurrency(tc.amount)
			if result != tc.expected {
				t.Errorf("FormatIndianCurrency(%f) = %s, want %s", tc.amount, result, tc.expected)
			}
		})
	}
}

// TestFormatPercentExamples tests specific examples of percentage formatting
func TestFormatPercentExamples(t *testing.T) {
	testCases := []struct {
		value    float64
		expected string
	}{
		{0, "0.00%"},
		{1.5, "+1.50%"},
		{-2.5, "-2.50%"},
		{100, "+100.00%"},
		{-100, "-100.00%"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			result := FormatPercent(tc.value)
			if result != tc.expected {
				t.Errorf("FormatPercent(%f) = %s, want %s", tc.value, result, tc.expected)
			}
		})
	}
}
