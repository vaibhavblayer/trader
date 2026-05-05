// Package cli provides the command-line interface for the trading application.
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

// runBacktestMode runs the paper trading in backtest mode using historical data.
func runBacktestMode(ctx context.Context, app *App, output *Output, symbols []string, exchange string, timeWindow time.Duration, threshold float64, useTools bool, days int, fromDate, toDate string, verbose bool) error {
	// Parse date range
	var from, to time.Time
	var err error

	if fromDate != "" {
		from, err = time.Parse("2006-01-02", fromDate)
		if err != nil {
			output.Error("Invalid from date format. Use YYYY-MM-DD")
			return err
		}
	} else {
		from = time.Now().AddDate(0, 0, -days)
	}

	if toDate != "" {
		to, err = time.Parse("2006-01-02", toDate)
		if err != nil {
			output.Error("Invalid to date format. Use YYYY-MM-DD")
			return err
		}
		// Set to end of day
		to = to.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	} else {
		to = time.Now()
	}

	output.Info("🔄 AI Paper Trading - Backtest Mode")
	output.Printf("  Symbols:    %v\n", symbols)
	output.Printf("  Period:     %s to %s\n", from.Format("2006-01-02"), to.Format("2006-01-02"))
	output.Printf("  Window:     %s\n", timeWindow)
	output.Printf("  Threshold:  %.0f%%\n", threshold)
	if useTools {
		output.Printf("  AI Mode:    Tools (function calling)\n")
	} else {
		output.Printf("  AI Mode:    Simple\n")
	}
	output.Println()

	tracker := NewPaperTracker()

	for _, symbol := range symbols {
		output.Bold("📊 Analyzing %s", symbol)
		output.Println()

		// Fetch historical data
		output.Dim("Fetching historical data...")
		candles, report, err := app.getQualityHistorical(ctx, broker.HistoricalRequest{
			Symbol:    symbol,
			Exchange:  models.Exchange(exchange),
			Timeframe: "15min",
			From:      from,
			To:        to,
		}, 20, false)
		if err != nil {
			output.Error("Failed to fetch usable data for %s: %v", symbol, err)
			continue
		}
		logQualityWarnings(app, report)

		if len(candles) < 20 {
			output.Warning("Insufficient data for %s (%d candles)", symbol, len(candles))
			continue
		}

		output.Success("Got %d candles from %s to %s",
			len(candles),
			candles[0].Timestamp.Format("Jan 02 15:04"),
			candles[len(candles)-1].Timestamp.Format("Jan 02 15:04"))

		// Simulate predictions at different points
		// We'll make predictions every N candles and check if they would have been right
		step := 4 // Every 4 candles (1 hour for 15min candles)
		if len(candles) < 50 {
			step = 2
		}

		output.Println()
		output.Info("Running AI analysis at %d points...", (len(candles)-20)/step)
		output.Println()

		for i := 20; i < len(candles)-int(timeWindow.Minutes()/15); i += step {
			currentCandle := candles[i]
			currentPrice := currentCandle.Close

			// Get AI prediction with chain of thought
			// In backtest mode with tools, use BacktestToolExecutor for accurate historical data
			var result *PredictionResult
			var err error
			if useTools {
				result, err = getAIPredictionBacktest(ctx, app, symbol, candles, i, currentPrice, timeWindow, threshold, tracker)
			} else {
				result, err = getAIPredictionVerbose(ctx, app, symbol, currentPrice, timeWindow, threshold, tracker, useTools)
			}
			if err != nil {
				output.Dim("  %s: AI error - %v", currentCandle.Timestamp.Format("Jan 02 15:04"), err)
				continue
			}

			// Show verbose chain of thought if enabled - with full transparency
			if verbose && result.ChainOfThought != nil {
				output.Println()
				output.Bold("  ╔══════════════════════════════════════════════════════════════")
				output.Bold("  ║ 🔍 ANALYSIS: %s @ ₹%.2f (%s)", symbol, currentPrice, currentCandle.Timestamp.Format("Jan 02 15:04"))
				output.Bold("  ╠══════════════════════════════════════════════════════════════")

				// Show each tool call with symbolic interpretation
				if len(result.ChainOfThought.ToolCalls) > 0 {
					output.Printf("  ║ TOOL CALLS:\n")
					for i, tc := range result.ChainOfThought.ToolCalls {
						output.Printf("  ║ ┌─ [%d] %s\n", i+1, tc.ToolName)
						// Show arguments if present
						if tc.Arguments != "" && tc.Arguments != "{}" {
							output.Printf("  ║ │  Args: %s\n", tc.Arguments)
						}
						// Parse and show symbolic interpretation
						symbolic := parseToolResultSymbolic(tc.ToolName, tc.Result)
						for _, line := range symbolic {
							output.Printf("  ║ │  → %s\n", line)
						}
						output.Printf("  ║ └─\n")
					}
				}

				// Show gate evaluation
				output.Printf("  ╠══════════════════════════════════════════════════════════════\n")
				output.Printf("  ║ GATE EVALUATION:\n")
				gateResults := extractGateResults(result.ChainOfThought.Response)
				for _, gate := range gateResults {
					output.Printf("  ║   %s\n", gate)
				}

				// Show final decision
				output.Printf("  ╠══════════════════════════════════════════════════════════════\n")
				if result.Prediction != nil {
					output.Printf("  ║ DECISION: %s (Confidence: %.0f%%)\n", result.Prediction.Action, result.Prediction.Confidence)
					output.Printf("  ║ REASONING: %s\n", result.Prediction.Reasoning)
				} else {
					output.Printf("  ║ DECISION: NO_TRADE (gates failed or insufficient edge)\n")
				}
				output.Printf("  ╚══════════════════════════════════════════════════════════════\n")
				output.Println()
			}

			prediction := result.Prediction
			if prediction == nil {
				// NO_TRADE is risk avoidance - don't score it, just show as avoided
				if !verbose {
					output.Dim("  %s @ ₹%.2f: AVOIDED (no clear edge)", currentCandle.Timestamp.Format("Jan 02 15:04"), currentPrice)
				}
				// Track avoidance count but don't score as RIGHT/WRONG
				tracker.mu.Lock()
				tracker.stats.ExpiredPredictions++ // Reuse this field for "avoided" count
				tracker.mu.Unlock()
				continue
			}

			// Double-check: if AI returned NO_TRADE action, treat as avoided (safety net)
			action := strings.ToUpper(strings.TrimSpace(prediction.Action))

			// Only allow explicit BUY or SELL - everything else is avoided
			if action != "BUY" && action != "SELL" {
				if !verbose {
					output.Dim("  %s @ ₹%.2f: AVOIDED (no trade signal)", currentCandle.Timestamp.Format("Jan 02 15:04"), currentPrice)
				}
				tracker.mu.Lock()
				tracker.stats.ExpiredPredictions++
				tracker.mu.Unlock()
				continue
			}

			// Find the candle at expiry time
			candlesForExpiry := int(timeWindow.Minutes() / 15)
			if candlesForExpiry < 1 {
				candlesForExpiry = 1 // At least 1 candle forward
			}
			expiryIdx := i + candlesForExpiry
			if expiryIdx >= len(candles) {
				expiryIdx = len(candles) - 1
			}
			expiryCandle := candles[expiryIdx]
			exitPrice := expiryCandle.Close

			// Calculate actual P&L
			var actualPnL float64
			if prediction.Action == "BUY" {
				actualPnL = ((exitPrice - currentPrice) / currentPrice) * 100
			} else {
				actualPnL = ((currentPrice - exitPrice) / currentPrice) * 100
			}

			// Determine outcome
			outcome := "WRONG"
			outcomeEmoji := "❌"
			if actualPnL > 0 {
				outcome = "RIGHT"
				outcomeEmoji = "✅"
			}

			// Update prediction with actual results
			prediction.ExitPrice = exitPrice
			prediction.PnLPercent = actualPnL
			prediction.Outcome = outcome
			prediction.Evaluated = true

			// Update tracker stats manually
			tracker.mu.Lock()
			tracker.stats.TotalPredictions++
			if outcome == "RIGHT" {
				tracker.stats.RightPredictions++
			} else {
				tracker.stats.WrongPredictions++
			}
			evaluated := tracker.stats.RightPredictions + tracker.stats.WrongPredictions
			tracker.stats.AvgPnLPercent = ((tracker.stats.AvgPnLPercent * float64(evaluated-1)) + actualPnL) / float64(evaluated)
			if actualPnL > tracker.stats.BestPrediction {
				tracker.stats.BestPrediction = actualPnL
			}
			if actualPnL < tracker.stats.WorstPrediction {
				tracker.stats.WorstPrediction = actualPnL
			}
			tracker.stats.WinRate = float64(tracker.stats.RightPredictions) / float64(evaluated) * 100
			tracker.history = append(tracker.history, prediction)
			tracker.mu.Unlock()

			// Print result
			output.Printf("  %s %s @ ₹%.2f → %s @ ₹%.2f = %s %.2f%% (Conf: %.0f%%)\n",
				outcomeEmoji,
				prediction.Action,
				currentPrice,
				expiryCandle.Timestamp.Format("15:04"),
				exitPrice,
				outcome,
				actualPnL,
				prediction.Confidence)
		}

		output.Println()
	}

	// Print final stats
	stats := tracker.GetStats()
	output.Println()
	output.Bold("📈 Backtest Results")
	output.Println()

	// Calculate win rate only from actual trades (not avoided)
	actualTrades := stats.RightPredictions + stats.WrongPredictions
	winRate := 0.0
	if actualTrades > 0 {
		winRate = float64(stats.RightPredictions) / float64(actualTrades) * 100
	}

	winRateColor := ""
	if winRate >= 60 {
		winRateColor = "\033[32m" // Green
	} else if winRate < 50 && actualTrades > 0 {
		winRateColor = "\033[31m" // Red
	}

	// ExpiredPredictions is reused for "avoided" count in backtest
	avoidedCount := stats.ExpiredPredictions

	output.Printf("  Actual Trades: %d (Avoided: %d)\n", actualTrades, avoidedCount)
	output.Printf("  Right: %d | Wrong: %d\n", stats.RightPredictions, stats.WrongPredictions)
	if actualTrades > 0 {
		fmt.Printf("  Win Rate: %s%.1f%%\033[0m (of %d trades)\n", winRateColor, winRate, actualTrades)
		output.Printf("  Avg P&L: %.2f%%\n", stats.AvgPnLPercent)
		output.Printf("  Best: +%.2f%% | Worst: %.2f%%\n", stats.BestPrediction, stats.WorstPrediction)
	} else {
		output.Printf("  No trades taken - all situations correctly avoided\n")
	}
	output.Println()

	// Show recent predictions
	if len(tracker.history) > 0 {
		output.Bold("Recent Trades:")
		start := 0
		if len(tracker.history) > 10 {
			start = len(tracker.history) - 10
		}
		for _, p := range tracker.history[start:] {
			emoji := "❌"
			if p.Outcome == "RIGHT" {
				emoji = "✅"
			}
			output.Printf("  %s %s %s: %.2f%% - %s\n", emoji, p.Symbol, p.Action, p.PnLPercent, p.Reasoning)
		}
	}

	return nil
}
