// Package cli provides the command-line interface for the trading application.
package cli

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// FormatIndianCurrency formats a number in Indian currency format (lakhs, crores).
// Requirements: 21.6
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

// formatIndianNumber formats an integer string in Indian numbering system.
// Indian system: 1,00,00,000 (1 crore) vs Western: 10,000,000
func formatIndianNumber(s string) string {
	n := len(s)
	if n <= 3 {
		return s
	}

	// First group of 3 from right (hundreds)
	result := s[n-3:]
	s = s[:n-3]

	// Then groups of 2 (thousands, lakhs, crores)
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

// FormatPnL formats P&L with sign.
func FormatPnL(pnl float64) string {
	formatted := FormatIndianCurrency(pnl)
	if pnl > 0 {
		return "+" + formatted
	}
	return formatted
}

// FormatQuantity formats a quantity with Indian numbering.
func FormatQuantity(qty int64) string {
	return formatIndianNumber(fmt.Sprintf("%d", qty))
}

// FormatLakhs formats a number in lakhs.
func FormatLakhs(amount float64) string {
	lakhs := amount / 100000
	if lakhs < 0 {
		return fmt.Sprintf("-%.2f L", -lakhs)
	}
	return fmt.Sprintf("%.2f L", lakhs)
}

// FormatCrores formats a number in crores.
func FormatCrores(amount float64) string {
	crores := amount / 10000000
	if crores < 0 {
		return fmt.Sprintf("-%.2f Cr", -crores)
	}
	return fmt.Sprintf("%.2f Cr", crores)
}

// FormatCompact formats a number in compact form (L/Cr).
func FormatCompact(amount float64) string {
	absAmount := math.Abs(amount)

	if absAmount >= 10000000 { // 1 crore
		return FormatCrores(amount)
	} else if absAmount >= 100000 { // 1 lakh
		return FormatLakhs(amount)
	}
	return FormatIndianCurrency(amount)
}

// FormatVolume formats volume in compact form.
func FormatVolume(volume int64) string {
	if volume >= 10000000 { // 1 crore
		return fmt.Sprintf("%.2f Cr", float64(volume)/10000000)
	} else if volume >= 100000 { // 1 lakh
		return fmt.Sprintf("%.2f L", float64(volume)/100000)
	} else if volume >= 1000 {
		return fmt.Sprintf("%.2f K", float64(volume)/1000)
	}
	return fmt.Sprintf("%d", volume)
}

// FormatPrice formats a price with appropriate decimal places.
func FormatPrice(price float64) string {
	if price >= 1000 {
		return fmt.Sprintf("%.2f", price)
	} else if price >= 100 {
		return fmt.Sprintf("%.2f", price)
	} else if price >= 10 {
		return fmt.Sprintf("%.2f", price)
	}
	return fmt.Sprintf("%.4f", price)
}

// FormatTime formats a time in IST.
func FormatTime(t time.Time) string {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	return t.In(ist).Format("15:04:05")
}

// FormatDate formats a date.
func FormatDate(t time.Time) string {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	return t.In(ist).Format("02-Jan-2006")
}

// FormatDateTime formats a datetime.
func FormatDateTime(t time.Time) string {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	return t.In(ist).Format("02-Jan-2006 15:04:05")
}

// FormatDuration formats a duration in human-readable form.
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	return fmt.Sprintf("%dd %dh", days, hours)
}

// FormatRiskReward formats a risk-reward ratio.
func FormatRiskReward(rr float64) string {
	return fmt.Sprintf("1:%.2f", rr)
}

// FormatConfidence formats a confidence percentage.
func FormatConfidence(conf float64) string {
	return fmt.Sprintf("%.0f%%", conf)
}

// FormatChange formats a price change.
func FormatChange(change, changePct float64) string {
	sign := ""
	if change > 0 {
		sign = "+"
	}
	return fmt.Sprintf("%s%.2f (%s%.2f%%)", sign, change, sign, changePct)
}

// FormatOHLC formats OHLC data.
func FormatOHLC(open, high, low, close float64) string {
	return fmt.Sprintf("O: %.2f  H: %.2f  L: %.2f  C: %.2f", open, high, low, close)
}

// FormatBidAsk formats bid/ask spread.
func FormatBidAsk(bid, ask float64) string {
	spread := ask - bid
	spreadPct := (spread / bid) * 100
	return fmt.Sprintf("Bid: %.2f  Ask: %.2f  Spread: %.2f (%.2f%%)", bid, ask, spread, spreadPct)
}

// FormatGreeks formats option Greeks.
func FormatGreeks(delta, gamma, theta, vega float64) string {
	return fmt.Sprintf("Δ: %.4f  Γ: %.4f  Θ: %.4f  ν: %.4f", delta, gamma, theta, vega)
}

// FormatIV formats implied volatility.
func FormatIV(iv float64) string {
	return fmt.Sprintf("%.2f%%", iv*100)
}

// FormatOI formats open interest.
func FormatOI(oi int64) string {
	return FormatVolume(oi)
}

// FormatPCR formats put-call ratio.
func FormatPCR(pcr float64) string {
	return fmt.Sprintf("%.2f", pcr)
}

// FormatSignalScore formats a signal score.
func FormatSignalScore(score float64) string {
	return fmt.Sprintf("%.0f", score)
}

// TruncateString truncates a string to max length with ellipsis.
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// PadRight pads a string to the right.
func PadRight(s string, length int) string {
	if len(s) >= length {
		return s
	}
	return s + strings.Repeat(" ", length-len(s))
}

// PadLeft pads a string to the left.
func PadLeft(s string, length int) string {
	if len(s) >= length {
		return s
	}
	return strings.Repeat(" ", length-len(s)) + s
}

// Center centers a string.
func Center(s string, length int) string {
	if len(s) >= length {
		return s
	}
	padding := length - len(s)
	left := padding / 2
	right := padding - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}
