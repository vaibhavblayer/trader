// Package cli provides the command-line interface for the trading application.
package cli

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

// addPaperCommands adds paper trading commands.
func addPaperCommands(rootCmd *cobra.Command, app *App) {
	rootCmd.AddCommand(newPaperCmd(app))
}

func newPaperCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "paper [symbols...]",
		Short: "AI paper trading - track predictions without real trades",
		Long: `Watch live market data with AI predictions and track accuracy.

The AI will analyze symbols and make BUY/SELL predictions with:
- Confidence level (0-100%)
- Target price and stop loss
- Time window for the prediction

AI TOOLS MODE (default):
The AI uses function calling to access real analysis tools:
- RSI, Bollinger Bands, Stochastic indicators
- Fibonacci retracement levels
- Support/Resistance (pivot points)
- Candlestick pattern detection
- Chart pattern detection
- ATR for volatility analysis
- Multi-timeframe analysis

BACKTEST MODE:
Use --backtest to replay historical data and test AI predictions.
Works on weekends/holidays when market is closed.

After the time window expires, the prediction is evaluated as RIGHT or WRONG
based on whether the price moved in the predicted direction.

No actual trades are executed - this is for tracking AI accuracy only.`,
		Example: `  # Live mode (requires market open)
  trader paper RELIANCE INFY TCS
  trader paper --watchlist nifty50
  
  # Backtest mode (works anytime)
  trader paper RELIANCE --backtest              # Last 1 day
  trader paper RELIANCE --backtest --days 5     # Last 5 days
  trader paper TCS --backtest --from 2026-01-02 # Specific date
  trader paper INFY --backtest --from 2026-01-01 --to 2026-01-03`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Skip validation if help flag is set
			helpFlag, _ := cmd.Flags().GetBool("help")
			if helpFlag {
				return cmd.Help()
			}

			output := NewOutput(cmd)
			ctx := context.Background()

			mode, _ := cmd.Flags().GetString("mode")
			exchange, _ := cmd.Flags().GetString("exchange")
			watchlistName, _ := cmd.Flags().GetString("watchlist")
			windowStr, _ := cmd.Flags().GetString("window")
			threshold, _ := cmd.Flags().GetFloat64("threshold")
			interval, _ := cmd.Flags().GetInt("interval")
			useTools, _ := cmd.Flags().GetBool("tools")
			simpleMode, _ := cmd.Flags().GetBool("simple")
			backtestMode, _ := cmd.Flags().GetBool("backtest")
			backtestDays, _ := cmd.Flags().GetInt("days")
			fromDate, _ := cmd.Flags().GetString("from")
			toDate, _ := cmd.Flags().GetString("to")
			verbose, _ := cmd.Flags().GetBool("verbose")

			// Simple mode overrides tools
			if simpleMode {
				useTools = false
			}

			// Check if user accidentally passed --help as flag value
			if watchlistName == "--help" || watchlistName == "-h" ||
				windowStr == "--help" || windowStr == "-h" {
				return cmd.Help()
			}

			// Parse time window
			timeWindow, err := time.ParseDuration(windowStr)
			if err != nil {
				timeWindow = 5 * time.Minute
			}

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			if app.LLMClient == nil {
				output.Error("LLM client not configured. Check your OpenAI API key.")
				return fmt.Errorf("llm client not configured")
			}

			// Get symbols
			var symbols []string
			if watchlistName != "" {
				symbols = getPredefinedWatchlist(watchlistName, app, ctx)
				if len(symbols) == 0 {
					output.Error("Watchlist '%s' not found or empty", watchlistName)
					return fmt.Errorf("watchlist not found")
				}
				output.Info("Using watchlist: %s (%d symbols)", watchlistName, len(symbols))
			} else if len(args) > 0 {
				symbols = make([]string, len(args))
				for i, s := range args {
					symbols[i] = strings.ToUpper(s)
				}
			} else {
				symbols = []string{"RELIANCE", "TCS", "INFY", "HDFCBANK", "ICICIBANK"}
				output.Info("Using default symbols")
			}

			// BACKTEST MODE
			if backtestMode {
				return runBacktestMode(ctx, app, output, symbols, exchange, timeWindow, threshold, useTools, backtestDays, fromDate, toDate, verbose)
			}

			// LIVE MODE - requires ticker
			if app.Ticker == nil {
				output.Error("Ticker not configured. Run 'trader login' first.")
				output.Info("Tip: Use --backtest flag to test with historical data")
				return fmt.Errorf("ticker not configured")
			}

			// Fetch and register instrument tokens
			output.Info("Fetching instrument tokens...")
			validSymbols := make([]string, 0, len(symbols))
			for _, symbol := range symbols {
				token, err := app.Broker.GetInstrumentToken(ctx, symbol, models.Exchange(exchange))
				if err != nil {
					output.Warning("Symbol %s not found", symbol)
					continue
				}
				app.Ticker.RegisterSymbol(symbol, token)
				validSymbols = append(validSymbols, symbol)
			}

			if len(validSymbols) == 0 {
				output.Error("No valid symbols found")
				return fmt.Errorf("no valid symbols")
			}

			output.Info("Starting AI Paper Trading Mode")
			output.Printf("  Symbols:    %d\n", len(validSymbols))
			output.Printf("  Window:     %s\n", timeWindow)
			output.Printf("  Threshold:  %.0f%%\n", threshold)
			output.Printf("  Interval:   %ds\n", interval)
			if useTools {
				output.Printf("  AI Mode:    Tools (function calling)\n")
			} else {
				output.Printf("  AI Mode:    Simple (no tools)\n")
			}
			output.Println()
			output.Dim("Press Ctrl+C to stop")
			output.Println()

			// Initialize tracker
			tracker := NewPaperTracker()

			// Track latest ticks
			latestTicks := make(map[string]models.Tick)
			var tickMu sync.Mutex

			// Set up tick handlers
			tickMode := broker.TickModeQuote
			if mode == "full" {
				tickMode = broker.TickModeFull
			}

			app.Ticker.OnTick(func(tick models.Tick) {
				tickMu.Lock()
				latestTicks[tick.Symbol] = tick
				tickMu.Unlock()
			})

			app.Ticker.OnError(func(err error) {
				output.Error("Ticker error: %v", err)
			})

			app.Ticker.OnConnect(func() {
				output.Success("Connected to ticker")
				if err := app.Ticker.Subscribe(validSymbols, tickMode); err != nil {
					output.Error("Failed to subscribe: %v", err)
				}
			})

			app.Ticker.OnDisconnect(func() {
				output.Warning("Disconnected from ticker")
			})

			if err := app.Ticker.Connect(ctx); err != nil {
				output.Error("Failed to connect: %v", err)
				return err
			}
			defer app.Ticker.Disconnect()

			// Analysis ticker
			analysisTicker := time.NewTicker(time.Duration(interval) * time.Second)
			defer analysisTicker.Stop()

			// Display ticker - only used in non-verbose mode
			var displayTicker *time.Ticker
			if !verbose {
				displayTicker = time.NewTicker(500 * time.Millisecond)
				defer displayTicker.Stop()
			}

			// Track last analysis time per symbol
			lastAnalysis := make(map[string]time.Time)

			// Track last AI status for display
			var lastAIStatus string
			var lastAIStatusMu sync.Mutex

			// In verbose mode, show initial header
			if verbose {
				output.Bold("🤖 AI Paper Trading - Live Verbose Mode")
				output.Printf("  Symbols: %v | Window: %s | Threshold: %.0f%% | Interval: %ds\n", validSymbols, timeWindow, threshold, interval)
				output.Println()
				output.Dim("Live data updates every analysis cycle. Analysis logs scroll below.")
				output.Println()
			}

			// Helper to print sticky header in verbose mode
			printVerboseHeader := func(ticksCopy map[string]models.Tick) {
				stats := tracker.GetStats()

				// Print separator and header
				fmt.Println()
				fmt.Println("\033[1;36m┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓\033[0m")
				fmt.Printf("\033[1;36m┃\033[0m 📊 \033[1mLIVE DATA\033[0m @ %s%s\033[1;36m┃\033[0m\n",
					time.Now().Format("15:04:05"), strings.Repeat(" ", 55))
				fmt.Println("\033[1;36m┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫\033[0m")

				for _, sym := range validSymbols {
					if tick, ok := ticksCopy[sym]; ok {
						change := 0.0
						if tick.Close > 0 {
							change = ((tick.LTP - tick.Close) / tick.Close) * 100
						}
						changeStr := fmt.Sprintf("%+.2f%%", change)
						if change > 0 {
							changeStr = fmt.Sprintf("\033[32m%+.2f%%\033[0m", change)
						} else if change < 0 {
							changeStr = fmt.Sprintf("\033[31m%+.2f%%\033[0m", change)
						}
						fmt.Printf("\033[1;36m┃\033[0m  \033[1m%-10s\033[0m ₹%-10.2f %s  Vol: %-12s \033[1;36m┃\033[0m\n",
							sym, tick.LTP, changeStr, FormatVolume(tick.Volume))
					}
				}

				// Stats line
				decisiveCount := stats.RightPredictions + stats.WrongPredictions
				winRateStr := fmt.Sprintf("%.1f%%", stats.WinRate)
				if stats.WinRate >= 60 {
					winRateStr = fmt.Sprintf("\033[32m%.1f%%\033[0m", stats.WinRate)
				} else if stats.WinRate < 40 && decisiveCount > 0 {
					winRateStr = fmt.Sprintf("\033[31m%.1f%%\033[0m", stats.WinRate)
				}
				fmt.Println("\033[1;36m┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫\033[0m")
				fmt.Printf("\033[1;36m┃\033[0m  ✓ Right: \033[32m%d\033[0m  ✗ Wrong: \033[31m%d\033[0m  ⏰ Expired: %d  │  Win Rate: %s  │  Avg P&L: %.2f%% \033[1;36m┃\033[0m\n",
					stats.RightPredictions, stats.WrongPredictions, stats.ExpiredPredictions, winRateStr, stats.AvgPnLPercent)
				fmt.Println("\033[1;36m┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛\033[0m")
			}

			for {
				select {
				case <-func() <-chan time.Time {
					if displayTicker != nil {
						return displayTicker.C
					}
					// Return a channel that never fires for verbose mode
					return make(chan time.Time)
				}():
					tickMu.Lock()
					prices := make(map[string]float64)
					for sym, tick := range latestTicks {
						prices[sym] = tick.LTP
					}
					tickMu.Unlock()

					// Check expired predictions
					expired := tracker.CheckExpiredPredictions(prices)
					for _, p := range expired {
						speakPredictionResult(p)
						lastAIStatusMu.Lock()
						if p.Outcome == "RIGHT" {
							lastAIStatus = fmt.Sprintf("✓ %s %s prediction was RIGHT (+%.2f%%)", p.Action, p.Symbol, p.PnLPercent)
						} else {
							lastAIStatus = fmt.Sprintf("✗ %s %s prediction was WRONG (%.2f%%)", p.Action, p.Symbol, p.PnLPercent)
						}
						lastAIStatusMu.Unlock()
					}

					// Display (only in non-verbose mode)
					lastAIStatusMu.Lock()
					displayPaperTradingWithStatus(output, validSymbols, latestTicks, tracker, lastAIStatus)
					lastAIStatusMu.Unlock()

				case <-analysisTicker.C:
					tickMu.Lock()
					ticksCopy := make(map[string]models.Tick)
					for k, v := range latestTicks {
						ticksCopy[k] = v
					}
					tickMu.Unlock()

					// Check expired predictions in verbose mode
					if verbose {
						tickMu.Lock()
						prices := make(map[string]float64)
						for sym, tick := range latestTicks {
							prices[sym] = tick.LTP
						}
						tickMu.Unlock()

						expired := tracker.CheckExpiredPredictions(prices)
						for _, p := range expired {
							speakPredictionResult(p)
							if p.Outcome == "RIGHT" {
								fmt.Printf("  \033[1;32m✅ PREDICTION RESULT: %s %s was RIGHT (+%.2f%%)\033[0m\n", p.Action, p.Symbol, p.PnLPercent)
							} else if p.Outcome == "WRONG" {
								fmt.Printf("  \033[1;31m❌ PREDICTION RESULT: %s %s was WRONG (%.2f%%)\033[0m\n", p.Action, p.Symbol, p.PnLPercent)
							} else {
								fmt.Printf("  \033[33m⏰ PREDICTION EXPIRED: %s %s (%.2f%%)\033[0m\n", p.Action, p.Symbol, p.PnLPercent)
							}
						}
					}

					// Check if any symbol needs analysis
					needsAnalysis := false
					for _, symbol := range validSymbols {
						tick, ok := ticksCopy[symbol]
						if !ok || tick.LTP == 0 {
							continue
						}
						if last, ok := lastAnalysis[symbol]; !ok || time.Since(last) >= time.Duration(interval)*time.Second {
							needsAnalysis = true
							break
						}
					}

					// In verbose mode, show live data header only when analysis will happen
					if verbose && needsAnalysis {
						printVerboseHeader(ticksCopy)

						// Show active predictions
						predictions := tracker.GetActivePredictions()
						if len(predictions) > 0 {
							fmt.Println("\033[33m  📌 Active Predictions:\033[0m")
							for _, p := range predictions {
								remaining := time.Until(p.ExpiresAt)
								actionColor := "\033[32m" // Green for BUY
								if p.Action == "SELL" {
									actionColor = "\033[31m" // Red for SELL
								}
								fmt.Printf("     %s%s\033[0m %s @ ₹%.2f → Target: ₹%.2f, SL: ₹%.2f (expires in %dm%ds)\n",
									actionColor, p.Action, p.Symbol, p.EntryPrice, p.TargetPrice, p.StopLoss,
									int(remaining.Minutes()), int(remaining.Seconds())%60)
							}
						}
					}

					// Analyze each symbol
					for _, symbol := range validSymbols {
						tick, ok := ticksCopy[symbol]
						if !ok || tick.LTP == 0 {
							continue
						}

						// Skip if recently analyzed
						if last, ok := lastAnalysis[symbol]; ok && time.Since(last) < time.Duration(interval)*time.Second {
							continue
						}

						// Update status (for non-verbose mode)
						if !verbose {
							lastAIStatusMu.Lock()
							lastAIStatus = fmt.Sprintf("🔍 Analyzing %s at ₹%.2f...", symbol, tick.LTP)
							lastAIStatusMu.Unlock()
						}

						// Get AI prediction with verbose output
						result, err := getAIPredictionVerbose(ctx, app, symbol, tick.LTP, timeWindow, threshold, tracker, useTools)
						lastAnalysis[symbol] = time.Now()

						if err != nil {
							if verbose {
								fmt.Printf("  ⚠ AI error for %s: %v\n", symbol, err)
							} else {
								lastAIStatusMu.Lock()
								lastAIStatus = fmt.Sprintf("⚠ AI error for %s: %v", symbol, err)
								lastAIStatusMu.Unlock()
							}
							continue
						}

						// Show verbose chain of thought if enabled
						if verbose && result.ChainOfThought != nil {
							fmt.Println("  ╔══════════════════════════════════════════════════════════════")
							fmt.Printf("  ║ 🔍 ANALYSIS: %s @ ₹%.2f (%s)\n", symbol, tick.LTP, time.Now().Format("15:04:05"))
							fmt.Println("  ╠══════════════════════════════════════════════════════════════")

							// Show each tool call with symbolic interpretation
							if len(result.ChainOfThought.ToolCalls) > 0 {
								fmt.Println("  ║ TOOL CALLS:")
								for i, tc := range result.ChainOfThought.ToolCalls {
									fmt.Printf("  ║ ┌─ [%d] %s\n", i+1, tc.ToolName)
									// Show arguments if present
									if tc.Arguments != "" && tc.Arguments != "{}" {
										fmt.Printf("  ║ │  Args: %s\n", tc.Arguments)
									}
									// Parse and show symbolic interpretation
									symbolic := parseToolResultSymbolic(tc.ToolName, tc.Result)
									for _, line := range symbolic {
										fmt.Printf("  ║ │  → %s\n", line)
									}
									fmt.Println("  ║ └─")
								}
							}

							// Show gate evaluation
							fmt.Println("  ╠══════════════════════════════════════════════════════════════")
							fmt.Println("  ║ GATE EVALUATION:")
							gateResults := extractGateResults(result.ChainOfThought.Response)
							for _, gate := range gateResults {
								fmt.Printf("  ║   %s\n", gate)
							}

							// Show final decision
							fmt.Println("  ╠══════════════════════════════════════════════════════════════")
							if result.Prediction != nil {
								fmt.Printf("  ║ DECISION: %s (Confidence: %.0f%%)\n", result.Prediction.Action, result.Prediction.Confidence)
								fmt.Printf("  ║ REASONING: %s\n", result.Prediction.Reasoning)
								fmt.Printf("  ║ TARGET: ₹%.2f | STOP LOSS: ₹%.2f | WINDOW: %s\n",
									result.Prediction.TargetPrice, result.Prediction.StopLoss, result.Prediction.TimeWindow)
							} else {
								fmt.Println("  ║ DECISION: NO_TRADE (gates failed or insufficient edge)")
							}
							fmt.Println("  ╚══════════════════════════════════════════════════════════════")
							fmt.Println()
						}

						prediction := result.Prediction
						if prediction != nil {
							tracker.AddPrediction(prediction)
							speakNewPrediction(prediction)
							if verbose {
								fmt.Printf("  🎯 NEW PREDICTION: %s %s @ ₹%.2f (%.0f%% conf)\n",
									prediction.Action, symbol, prediction.EntryPrice, prediction.Confidence)
								fmt.Printf("     Target: ₹%.2f | SL: ₹%.2f | Window: %s\n",
									prediction.TargetPrice, prediction.StopLoss, prediction.TimeWindow)
								fmt.Printf("     Reason: %s\n\n", prediction.Reasoning)
							} else {
								lastAIStatusMu.Lock()
								lastAIStatus = fmt.Sprintf("🎯 NEW: %s %s @ ₹%.2f (%.0f%% conf) → Target: ₹%.2f, SL: ₹%.2f\n   📊 Reason: %s",
									prediction.Action, symbol, prediction.EntryPrice, prediction.Confidence,
									prediction.TargetPrice, prediction.StopLoss, prediction.Reasoning)
								lastAIStatusMu.Unlock()
							}
						} else {
							if !verbose {
								lastAIStatusMu.Lock()
								lastAIStatus = fmt.Sprintf("⏸ AI suggests HOLD for %s (no clear signal)", symbol)
								lastAIStatusMu.Unlock()
							}
						}
					}
				}
			}
		},
	}

	cmd.Flags().StringP("mode", "m", "quote", "Tick mode (quote, full)")
	cmd.Flags().StringP("exchange", "e", "NSE", "Exchange (NSE, BSE, NFO)")
	cmd.Flags().StringP("watchlist", "w", "", "Watchlist name")
	cmd.Flags().StringP("window", "t", "5m", "Prediction time window (e.g., 5m, 15m, 1h)")
	cmd.Flags().Float64P("threshold", "c", 65.0, "Minimum confidence threshold for predictions")
	cmd.Flags().IntP("interval", "i", 60, "Analysis interval in seconds")
	cmd.Flags().Bool("tools", true, "Enable AI tools/function calling for analysis (default: true)")
	cmd.Flags().Bool("simple", false, "Use simple mode without tools (faster but less accurate)")

	// Backtest flags
	cmd.Flags().Bool("backtest", false, "Run in backtest mode using historical data")
	cmd.Flags().Int("days", 1, "Number of days to backtest (default: 1)")
	cmd.Flags().String("from", "", "Start date for backtest (YYYY-MM-DD)")
	cmd.Flags().String("to", "", "End date for backtest (YYYY-MM-DD)")
	cmd.Flags().BoolP("verbose", "v", false, "Show AI reasoning and tool calls (chain of thought)")

	cmd.AddCommand(newPaperLedgerCmd(app))
	cmd.AddCommand(newPaperStateCmd(app))

	return cmd
}

func newPaperLedgerCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ledger",
		Short: "Show persistent paper trading ledger events",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			ledger, ok := app.Store.(broker.PaperLedger)
			if !ok || ledger == nil {
				output.Warning("Paper ledger is not available")
				return nil
			}

			limit, _ := cmd.Flags().GetInt("limit")
			events, err := ledger.GetPaperLedger(ctx, limit)
			if err != nil {
				output.Error("Failed to get paper ledger: %v", err)
				return err
			}
			if output.IsJSON() {
				return output.JSON(events)
			}
			if len(events) == 0 {
				output.Info("No paper ledger events found")
				return nil
			}

			output.Bold("Paper Trading Ledger")
			output.Println()
			table := NewTable(output, "Time", "Type", "Symbol", "Ref")
			for _, event := range events {
				table.AddRow(FormatDateTime(event.Timestamp), event.Type, event.Symbol, event.RefID)
			}
			table.Render()
			return nil
		},
	}
	cmd.Flags().Int("limit", 20, "Maximum events to show")
	return cmd
}

func newPaperStateCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "state",
		Short: "Show persistent paper trading account state",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			ledger, ok := app.Store.(broker.PaperLedger)
			if !ok || ledger == nil {
				output.Warning("Paper state is not available")
				return nil
			}

			state, err := ledger.LoadPaperState(ctx)
			if err != nil {
				output.Error("Failed to load paper state: %v", err)
				return err
			}
			if output.IsJSON() {
				return output.JSON(state)
			}
			if state == nil {
				output.Info("No persistent paper state found")
				return nil
			}

			output.Bold("Paper Trading State")
			output.Printf("  Updated:        %s\n", FormatDateTime(state.UpdatedAt))
			output.Printf("  Available Cash: %s\n", FormatIndianCurrency(state.Balance.AvailableCash))
			output.Printf("  Total Equity:   %s\n", FormatIndianCurrency(state.Balance.TotalEquity))
			output.Printf("  Orders:         %d\n", len(state.Orders))
			output.Printf("  Positions:      %d\n", len(state.Positions))
			output.Printf("  GTT Orders:     %d\n", len(state.GTTOrders))
			return nil
		},
	}
}
