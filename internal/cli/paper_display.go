// Package cli provides the command-line interface for the trading application.
package cli

import (
	"fmt"
	"strings"
	"time"

	"zerodha-trader/internal/models"
)

// displayPaperTradingWithStatus displays the paper trading view with AI status.
func displayPaperTradingWithStatus(output *Output, symbols []string, ticks map[string]models.Tick, tracker *PaperTracker, aiStatus string) {
	// Clear screen
	fmt.Print("\033[H\033[2J")

	stats := tracker.GetStats()
	predictions := tracker.GetActivePredictions()

	// Header
	output.Bold("🤖 AI Paper Trading Mode (Hard Gates Enabled)")
	output.Printf("  %s | %d symbols | %d active predictions\n\n",
		time.Now().Format("15:04:05"), len(symbols), len(predictions))

	// Stats bar - show RIGHT/WRONG/EXPIRED separately
	winRateColor := "\033[33m" // Yellow
	if stats.WinRate >= 60 {
		winRateColor = "\033[32m" // Green
	} else if stats.WinRate < 50 && (stats.RightPredictions+stats.WrongPredictions) > 0 {
		winRateColor = "\033[31m" // Red
	}

	// Show decisive (RIGHT+WRONG) vs EXPIRED separately for transparency
	decisiveCount := stats.RightPredictions + stats.WrongPredictions
	fmt.Printf("Stats: R=%d W=%d E=%d | %sWin=%.1f%%\033[0m (of %d decisive) | P&L=%.2f%% | Best=+%.2f%% | Worst=%.2f%%\n\n",
		stats.RightPredictions, stats.WrongPredictions, stats.ExpiredPredictions,
		winRateColor, stats.WinRate, decisiveCount,
		stats.AvgPnLPercent, stats.BestPrediction, stats.WorstPrediction)

	// Active predictions
	if len(predictions) > 0 {
		output.Bold("Active Predictions")
		fmt.Printf("%-10s %6s %8s %10s %10s %10s %8s %8s\n",
			"Symbol", "Action", "Conf", "Entry", "Target", "SL", "Current", "Expires")
		fmt.Println(strings.Repeat("─", 85))

		for _, p := range predictions {
			tick, ok := ticks[p.Symbol]
			currentPrice := 0.0
			if ok {
				currentPrice = tick.LTP
			}

			// Calculate current P&L
			pnl := 0.0
			if currentPrice > 0 && p.EntryPrice > 0 {
				if p.Action == "BUY" {
					pnl = ((currentPrice - p.EntryPrice) / p.EntryPrice) * 100
				} else {
					pnl = ((p.EntryPrice - currentPrice) / p.EntryPrice) * 100
				}
			}

			// Color for action
			actionColor := "\033[33m" // Yellow
			if p.Action == "BUY" {
				actionColor = "\033[32m" // Green
			} else if p.Action == "SELL" {
				actionColor = "\033[31m" // Red
			}

			// Color for P&L
			pnlColor := "\033[0m"
			if pnl > 0 {
				pnlColor = "\033[32m"
			} else if pnl < 0 {
				pnlColor = "\033[31m"
			}

			// Time remaining
			remaining := time.Until(p.ExpiresAt)
			expiresStr := fmt.Sprintf("%dm%ds", int(remaining.Minutes()), int(remaining.Seconds())%60)
			if remaining < 0 {
				expiresStr = "EXPIRED"
			}

			fmt.Printf("%-10s %s%6s\033[0m %7.0f%% %10.2f %10.2f %10.2f %s%8.2f\033[0m %8s\n",
				p.Symbol, actionColor, p.Action, p.Confidence,
				p.EntryPrice, p.TargetPrice, p.StopLoss,
				pnlColor, currentPrice, expiresStr)
		}
		fmt.Println()
	}

	// Live prices
	output.Bold("Live Prices")
	fmt.Printf("%-12s %12s %10s %12s\n", "Symbol", "LTP", "Change", "Volume")
	fmt.Println(strings.Repeat("─", 50))

	for _, symbol := range symbols {
		tick, ok := ticks[symbol]
		if !ok {
			fmt.Printf("%-12s %12s %10s %12s\n", symbol, "-", "-", "-")
			continue
		}

		change := 0.0
		if tick.Close > 0 {
			change = ((tick.LTP - tick.Close) / tick.Close) * 100
		}

		changeColor := "\033[0m"
		if change > 0 {
			changeColor = "\033[32m"
		} else if change < 0 {
			changeColor = "\033[31m"
		}

		fmt.Printf("%-12s %12.2f %s%10.2f%%\033[0m %12s\n",
			symbol, tick.LTP, changeColor, change, FormatVolume(tick.Volume))
	}

	fmt.Println()

	// Show AI status
	if aiStatus != "" {
		fmt.Printf("AI: %s\n", aiStatus)
		fmt.Println()
	}

	output.Dim("Press Ctrl+C to stop | Predictions auto-evaluate on expiry")
}

// speakNewPrediction announces a new prediction via voice.
func speakNewPrediction(p *Prediction) {
	msg := fmt.Sprintf("AI predicts %s for %s with %.0f percent confidence. Target %.0f, stop loss %.0f",
		p.Action, p.Symbol, p.Confidence, p.TargetPrice, p.StopLoss)
	speak(msg)
}

// speakPredictionResult announces prediction result via voice.
func speakPredictionResult(p *Prediction) {
	var msg string
	if p.Outcome == "RIGHT" {
		msg = fmt.Sprintf("%s prediction for %s was correct! Profit %.1f percent", p.Action, p.Symbol, p.PnLPercent)
	} else {
		msg = fmt.Sprintf("%s prediction for %s was wrong. Loss %.1f percent", p.Action, p.Symbol, -p.PnLPercent)
	}
	speak(msg)
}
