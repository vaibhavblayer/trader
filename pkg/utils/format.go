// Package utils provides shared utility functions.
package utils

import (
	"fmt"
	"strings"
)

// FormatIndianCurrency formats a number in Indian currency format (lakhs, crores).
func FormatIndianCurrency(amount float64) string {
	negative := amount < 0
	if negative {
		amount = -amount
	}

	// Format with 2 decimal places
	str := fmt.Sprintf("%.2f", amount)
	parts := strings.Split(str, ".")
	intPart := parts[0]
	decPart := parts[1]

	// Apply Indian numbering system
	formatted := formatIndianNumber(intPart)

	result := "₹" + formatted + "." + decPart
	if negative {
		result = "-" + result
	}
	return result
}

// FormatIndianNumber formats an integer string in Indian numbering system.
func formatIndianNumber(s string) string {
	n := len(s)
	if n <= 3 {
		return s
	}

	// First group of 3 from right
	result := s[n-3:]
	s = s[:n-3]

	// Then groups of 2
	for len(s) > 0 {
		if len(s) >= 2 {
			result = s[len(s)-2:] + "," + result
			s = s[:len(s)-2]
		} else {
			result = s + "," + result
			s = ""
		}
	}

	return result
}

// FormatPercent formats a percentage with sign.
func FormatPercent(value float64) string {
	sign := ""
	if value > 0 {
		sign = "+"
	}
	return fmt.Sprintf("%s%.2f%%", sign, value)
}

// FormatPnL formats P&L with color indicators.
func FormatPnL(pnl float64) string {
	formatted := FormatIndianCurrency(pnl)
	if pnl > 0 {
		return "+" + formatted
	}
	return formatted
}

// FormatQuantity formats a quantity with commas.
func FormatQuantity(qty int64) string {
	return formatIndianNumber(fmt.Sprintf("%d", qty))
}

// FormatLakhs formats a number in lakhs.
func FormatLakhs(amount float64) string {
	lakhs := amount / 100000
	return fmt.Sprintf("%.2f L", lakhs)
}

// FormatCrores formats a number in crores.
func FormatCrores(amount float64) string {
	crores := amount / 10000000
	return fmt.Sprintf("%.2f Cr", crores)
}

// FormatCompact formats a number in compact form (L/Cr).
func FormatCompact(amount float64) string {
	absAmount := amount
	if absAmount < 0 {
		absAmount = -absAmount
	}

	if absAmount >= 10000000 {
		return FormatCrores(amount)
	} else if absAmount >= 100000 {
		return FormatLakhs(amount)
	}
	return FormatIndianCurrency(amount)
}
