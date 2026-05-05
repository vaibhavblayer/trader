// Package cli provides the command-line interface for the trading application.
package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

// parseToolResultSymbolic parses tool output into symbolic expressions for transparent logging.
// Example: RSI tool result → "RSI: 48.07 < 50 → BEARISH"
func parseToolResultSymbolic(toolName string, result string) []string {
	var lines []string

	// Check for error first
	if strings.Contains(result, "Error") || strings.Contains(result, "error") {
		// Extract just the error message
		resultLines := strings.Split(result, "\n")
		for _, line := range resultLines {
			line = strings.TrimSpace(line)
			if line != "" && len(lines) < 2 {
				lines = append(lines, line)
			}
		}
		return lines
	}

	// Parse text output - extract key values line by line
	resultLines := strings.Split(result, "\n")

	switch toolName {
	case "calculate_rsi":
		var rsi, prevRsi float64
		var momentum string
		for _, line := range resultLines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Current:") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					fmt.Sscanf(parts[1], "%f", &rsi)
				}
				if strings.Contains(line, "BEARISH") {
					momentum = "BEARISH"
				} else if strings.Contains(line, "BULLISH") {
					momentum = "BULLISH"
				} else {
					momentum = "NEUTRAL"
				}
			} else if strings.HasPrefix(line, "Previous:") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					fmt.Sscanf(parts[1], "%f", &prevRsi)
				}
			}
		}

		rsiZone := momentum
		if rsi > 70 {
			rsiZone = "OVERBOUGHT"
		} else if rsi > 55 {
			rsiZone = "BULLISH"
		} else if rsi < 30 {
			rsiZone = "OVERSOLD"
		} else if rsi < 45 {
			rsiZone = "BEARISH"
		} else if rsi >= 45 && rsi <= 55 {
			rsiZone = "CHOP ZONE (45-55)"
		}
		lines = append(lines, fmt.Sprintf("RSI: %.2f → %s", rsi, rsiZone))

		if prevRsi > 0 {
			direction := "FLAT"
			if rsi > prevRsi+0.5 {
				direction = "RISING ↑"
			} else if rsi < prevRsi-0.5 {
				direction = "FALLING ↓"
			}
			lines = append(lines, fmt.Sprintf("Direction: %.2f → %.2f = %s", prevRsi, rsi, direction))
		}

	case "analyze_volume":
		var currVol, avgVol, ratio float64
		for _, line := range resultLines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Current Volume:") {
				fmt.Sscanf(line, "Current Volume: %f", &currVol)
			} else if strings.Contains(line, "Avg Volume:") || strings.Contains(line, "Average") {
				parts := strings.Split(line, ":")
				if len(parts) >= 2 {
					fmt.Sscanf(strings.TrimSpace(parts[1]), "%f", &avgVol)
				}
			} else if strings.HasPrefix(line, "Volume Ratio:") {
				fmt.Sscanf(line, "Volume Ratio: %f", &ratio)
			}
		}

		if ratio == 0 && avgVol > 0 {
			ratio = currVol / avgVol
		}

		volStatus := "LOW"
		if ratio > 2.0 {
			volStatus = "HIGH EXPANSION ✓✓"
		} else if ratio > 1.5 {
			volStatus = "GOOD EXPANSION ✓"
		} else if ratio > 1.3 {
			volStatus = "ACCEPTABLE ✓"
		} else if ratio > 1.0 {
			volStatus = "NORMAL"
		}
		lines = append(lines, fmt.Sprintf("Volume: %.0f vs Avg %.0f = %.2fx → %s", currVol, avgVol, ratio, volStatus))

	case "calculate_ema_crossover":
		var ema9, ema21 float64
		var trend string
		for _, line := range resultLines {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "Fast EMA") || strings.Contains(line, "EMA(9)") {
				parts := strings.Split(line, ":")
				if len(parts) >= 2 {
					fmt.Sscanf(strings.TrimSpace(parts[1]), "%f", &ema9)
				}
			} else if strings.Contains(line, "Slow EMA") || strings.Contains(line, "EMA(21)") {
				parts := strings.Split(line, ":")
				if len(parts) >= 2 {
					fmt.Sscanf(strings.TrimSpace(parts[1]), "%f", &ema21)
				}
			} else if strings.Contains(line, "Trend:") {
				parts := strings.Split(line, ":")
				if len(parts) >= 2 {
					trend = strings.TrimSpace(parts[1])
				}
			}
		}

		alignment := "UNKNOWN"
		if ema9 > ema21 {
			alignment = "BULLISH (EMA9 > EMA21)"
		} else if ema9 < ema21 {
			alignment = "BEARISH (EMA9 < EMA21)"
		}
		lines = append(lines, fmt.Sprintf("EMA9: %.2f | EMA21: %.2f", ema9, ema21))
		if trend != "" {
			lines = append(lines, fmt.Sprintf("Trend: %s → %s", trend, alignment))
		} else {
			lines = append(lines, fmt.Sprintf("Structure: %s", alignment))
		}

	case "calculate_vwap":
		var vwap, price, deviation float64
		for _, line := range resultLines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "VWAP:") {
				fmt.Sscanf(line, "VWAP: %f", &vwap)
			} else if strings.HasPrefix(line, "Current Price:") {
				fmt.Sscanf(line, "Current Price: %f", &price)
			} else if strings.HasPrefix(line, "Deviation:") {
				// Parse "Deviation: -0.18%" - handle the % sign
				devStr := strings.TrimPrefix(line, "Deviation:")
				devStr = strings.TrimSpace(devStr)
				devStr = strings.TrimSuffix(devStr, "%")
				fmt.Sscanf(devStr, "%f", &deviation)
			}
		}

		exhaustion := "OK"
		if deviation > 0.7 {
			exhaustion = "STRETCHED HIGH ✗"
		} else if deviation < -0.7 {
			exhaustion = "STRETCHED LOW ✗"
		} else if deviation > 0.3 {
			exhaustion = "ABOVE VWAP"
		} else if deviation < -0.3 {
			exhaustion = "BELOW VWAP"
		} else {
			exhaustion = "AT VWAP ✓"
		}
		lines = append(lines, fmt.Sprintf("VWAP: %.2f | Price: %.2f | Dev: %.2f%%", vwap, price, deviation))
		lines = append(lines, fmt.Sprintf("Exhaustion Check: %s", exhaustion))

	case "calculate_adx":
		var adx, plusDI, minusDI float64
		for _, line := range resultLines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "ADX:") || strings.HasPrefix(line, "Current ADX:") {
				parts := strings.Fields(line)
				for i, p := range parts {
					if (p == "ADX:" || p == "Current") && i+1 < len(parts) {
						fmt.Sscanf(parts[i+1], "%f", &adx)
						if adx > 0 {
							break
						}
					}
				}
			} else if strings.HasPrefix(line, "+DI:") {
				fmt.Sscanf(line, "+DI: %f", &plusDI)
			} else if strings.HasPrefix(line, "-DI:") {
				fmt.Sscanf(line, "-DI: %f", &minusDI)
			}
		}

		strength := "NO TREND ✗"
		if adx > 35 {
			strength = "STRONG TREND ✓✓"
		} else if adx > 25 {
			strength = "TRENDING ✓"
		} else if adx > 20 {
			strength = "WEAK TREND"
		}
		lines = append(lines, fmt.Sprintf("ADX: %.2f → %s", adx, strength))
		if plusDI > 0 || minusDI > 0 {
			lines = append(lines, fmt.Sprintf("+DI: %.2f | -DI: %.2f", plusDI, minusDI))
		}

	case "calculate_bollinger_bands":
		var upper, middle, lower, price float64
		for _, line := range resultLines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Upper Band:") {
				fmt.Sscanf(line, "Upper Band: %f", &upper)
			} else if strings.HasPrefix(line, "Middle Band:") {
				fmt.Sscanf(line, "Middle Band: %f", &middle)
			} else if strings.HasPrefix(line, "Lower Band:") {
				fmt.Sscanf(line, "Lower Band: %f", &lower)
			} else if strings.HasPrefix(line, "Current Price:") {
				fmt.Sscanf(line, "Current Price: %f", &price)
			}
		}

		position := "MIDDLE"
		if price > 0 && upper > 0 && lower > 0 {
			if price > upper {
				position = "ABOVE UPPER ⚠"
			} else if price > middle+(upper-middle)*0.8 {
				position = "NEAR UPPER"
			} else if price < lower {
				position = "BELOW LOWER ⚠"
			} else if price < middle-(middle-lower)*0.8 {
				position = "NEAR LOWER"
			}
		}
		lines = append(lines, fmt.Sprintf("BB: [%.2f - %.2f - %.2f]", lower, middle, upper))
		lines = append(lines, fmt.Sprintf("Price: %.2f → %s", price, position))

	case "calculate_atr":
		var atr, pctOfPrice float64
		for _, line := range resultLines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Current ATR:") {
				fmt.Sscanf(line, "Current ATR: %f", &atr)
				if idx := strings.Index(line, "("); idx > 0 {
					fmt.Sscanf(line[idx:], "(%f%% of price)", &pctOfPrice)
				}
			}
		}
		lines = append(lines, fmt.Sprintf("ATR: %.2f (%.2f%% of price)", atr, pctOfPrice))

	case "detect_candlestick_patterns":
		var patterns []string
		for _, line := range resultLines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "•") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") {
				pattern := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(line, "•"), "-"), "*")
				pattern = strings.TrimSpace(pattern)
				if pattern != "" && len(patterns) < 5 {
					patterns = append(patterns, pattern)
				}
			}
		}
		if len(patterns) > 0 {
			lines = append(lines, "Patterns found:")
			for _, p := range patterns {
				lines = append(lines, fmt.Sprintf("  • %s", p))
			}
		} else {
			lines = append(lines, "No significant patterns")
		}

	case "get_support_resistance":
		var r1, r2, s1, s2, pivot float64
		for _, line := range resultLines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Pivot:") || strings.HasPrefix(line, "PP:") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					fmt.Sscanf(parts[1], "%f", &pivot)
				}
			} else if strings.HasPrefix(line, "R1:") {
				fmt.Sscanf(line, "R1: %f", &r1)
			} else if strings.HasPrefix(line, "R2:") {
				fmt.Sscanf(line, "R2: %f", &r2)
			} else if strings.HasPrefix(line, "S1:") {
				fmt.Sscanf(line, "S1: %f", &s1)
			} else if strings.HasPrefix(line, "S2:") {
				fmt.Sscanf(line, "S2: %f", &s2)
			}
		}
		if pivot > 0 {
			lines = append(lines, fmt.Sprintf("Pivot: %.2f", pivot))
		}
		if r1 > 0 || r2 > 0 {
			lines = append(lines, fmt.Sprintf("Resistance: R1=%.2f R2=%.2f", r1, r2))
		}
		if s1 > 0 || s2 > 0 {
			lines = append(lines, fmt.Sprintf("Support: S1=%.2f S2=%.2f", s1, s2))
		}

	default:
		// For unknown tools, show first few meaningful lines
		count := 0
		for _, line := range resultLines {
			line = strings.TrimSpace(line)
			if line != "" && count < 5 {
				lines = append(lines, line)
				count++
			}
		}
	}

	if len(lines) == 0 {
		// Fallback: show first few lines of raw output
		count := 0
		for _, line := range resultLines {
			line = strings.TrimSpace(line)
			if line != "" && count < 3 {
				lines = append(lines, line)
				count++
			}
		}
	}

	if len(lines) == 0 {
		lines = append(lines, "(no data)")
	}

	return lines
}

// extractGateResults parses the AI response JSON to extract gate pass/fail status.
func extractGateResults(response string) []string {
	var lines []string

	// Find JSON in response
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end <= start {
		return []string{"(could not parse gates)"}
	}
	jsonStr := response[start : end+1]

	var result struct {
		GatesPassed struct {
			RSIRegime        bool `json:"rsi_regime"`
			RSIDirection     bool `json:"rsi_direction"`
			VolumeExpansion  bool `json:"volume_expansion"`
			EMAAlignment     bool `json:"ema_alignment"`
			VWAPNotExhausted bool `json:"vwap_not_exhausted"`
			TrendStrength    bool `json:"trend_strength"`
		} `json:"gates_passed"`
		SignalQuality struct {
			RSIValue         float64 `json:"rsi_value"`
			RSIDirection     string  `json:"rsi_direction"`
			VolumeRatio      float64 `json:"volume_ratio"`
			VWAPDeviationPct float64 `json:"vwap_deviation_pct"`
			ADXValue         float64 `json:"adx_value"`
			EMATrend         string  `json:"ema_trend"`
		} `json:"signal_quality"`
		Confidence float64 `json:"confidence"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return []string{"(could not parse gates)"}
	}

	// Format gate results with pass/fail indicators
	passIcon := "✓"
	failIcon := "✗"

	// RSI Regime Gate
	rsiGate := result.GatesPassed.RSIRegime || result.GatesPassed.RSIDirection
	icon := failIcon
	if rsiGate {
		icon = passIcon
	}
	lines = append(lines, fmt.Sprintf("[%s] RSI Regime: %.1f %s (need >55↑ for BUY, <45↓ for SELL)",
		icon, result.SignalQuality.RSIValue, result.SignalQuality.RSIDirection))

	// Volume Expansion Gate
	icon = failIcon
	if result.GatesPassed.VolumeExpansion {
		icon = passIcon
	}
	lines = append(lines, fmt.Sprintf("[%s] Volume Expansion: %.2fx (need >1.3x)",
		icon, result.SignalQuality.VolumeRatio))

	// EMA Alignment Gate
	icon = failIcon
	if result.GatesPassed.EMAAlignment {
		icon = passIcon
	}
	lines = append(lines, fmt.Sprintf("[%s] EMA Alignment: %s",
		icon, result.SignalQuality.EMATrend))

	// VWAP Exhaustion Gate
	icon = failIcon
	if result.GatesPassed.VWAPNotExhausted {
		icon = passIcon
	}
	lines = append(lines, fmt.Sprintf("[%s] VWAP Not Exhausted: %.2f%% (need <0.7%%)",
		icon, result.SignalQuality.VWAPDeviationPct))

	// Trend Strength Gate
	icon = failIcon
	if result.GatesPassed.TrendStrength {
		icon = passIcon
	}
	lines = append(lines, fmt.Sprintf("[%s] Trend Strength: ADX %.1f (need >25)",
		icon, result.SignalQuality.ADXValue))

	// Summary
	allPassed := rsiGate &&
		result.GatesPassed.VolumeExpansion &&
		result.GatesPassed.EMAAlignment &&
		result.GatesPassed.VWAPNotExhausted &&
		result.GatesPassed.TrendStrength

	if allPassed {
		lines = append(lines, fmt.Sprintf("─── ALL GATES PASSED (Confidence: %.0f%%) ───", result.Confidence))
	} else {
		lines = append(lines, "─── GATES FAILED → NO_TRADE ───")
	}

	return lines
}
