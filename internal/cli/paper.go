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
	"zerodha-trader/internal/store"
	"zerodha-trader/internal/trading"
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
			tracker, err := NewPersistentPaperTracker(ctx, app.Store)
			if err != nil {
				output.Warning("Failed to load paper prediction history: %v", err)
				tracker = NewPaperTracker()
			}

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
	cmd.AddCommand(newPaperPredictionsCmd(app))
	cmd.AddCommand(newPaperCandidatesCmd(app))
	cmd.AddCommand(newPaperCandidateRunCmd(app))
	cmd.AddCommand(newPaperCandidateHealthCmd(app))
	cmd.AddCommand(newPaperCandidateReviewCmd(app))
	cmd.AddCommand(newPaperEvaluateCmd(app))
	cmd.AddCommand(newPaperSoakRunCmd(app))
	cmd.AddCommand(newPaperExperimentsCmd(app))
	cmd.AddCommand(newPaperStatsCmd(app))

	return cmd
}

func newPaperExperimentsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "experiments",
		Short: "Show paper soak experiment run ledger",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if app.Store == nil {
				output.Warning("Paper experiment store is not available")
				return nil
			}

			limit, _ := cmd.Flags().GetInt("limit")
			days, _ := cmd.Flags().GetInt("days")
			symbol, _ := cmd.Flags().GetString("symbol")
			strategy, _ := cmd.Flags().GetString("strategy")
			regimeMode, _ := cmd.Flags().GetString("regime-mode")
			source, _ := cmd.Flags().GetString("source")
			summary, _ := cmd.Flags().GetBool("summary")
			compare, _ := cmd.Flags().GetBool("compare")
			minOutcomeDecisive, _ := cmd.Flags().GetInt("min-outcome-decisive")
			minWinRate, _ := cmd.Flags().GetFloat64("min-win-rate")
			minExpectancy, _ := cmd.Flags().GetFloat64("min-expectancy")

			filter := models.PaperExperimentRunFilter{
				Symbol:     strings.ToUpper(strings.TrimSpace(symbol)),
				Strategy:   trading.NormalizeStrategyName(strategy),
				RegimeMode: strings.ToLower(strings.TrimSpace(regimeMode)),
				Source:     strings.ToLower(strings.TrimSpace(source)),
				Limit:      limit,
			}
			if filter.RegimeMode != "" {
				parsedMode, err := parseCandidateRegimeMode(filter.RegimeMode)
				if err != nil {
					return err
				}
				filter.RegimeMode = parsedMode
			}
			if days > 0 {
				filter.StartDate = time.Now().AddDate(0, 0, -days)
				filter.EndDate = time.Now()
			}

			runs, err := app.Store.GetPaperExperimentRuns(ctx, filter)
			if err != nil {
				output.Error("Failed to get paper experiments: %v", err)
				return err
			}
			if compare {
				predictions, err := app.Store.GetPaperPredictions(ctx, store.PaperPredictionFilter{
					Symbol:    filter.Symbol,
					StartDate: filter.StartDate,
					EndDate:   filter.EndDate,
				})
				if err != nil {
					output.Error("Failed to get paper predictions: %v", err)
					return err
				}
				comparison := comparePaperExperimentCohorts(runs, predictions, paperExperimentComparisonOptions{
					MinOutcomeDecisive: minOutcomeDecisive,
					MinWinRate:         minWinRate,
					MinExpectancy:      minExpectancy,
				})
				if output.IsJSON() {
					return output.JSON(comparison)
				}
				displayPaperExperimentComparison(output, comparison)
				return nil
			}
			if summary {
				summaries := summarizePaperExperimentRuns(runs)
				if output.IsJSON() {
					return output.JSON(summaries)
				}
				displayPaperExperimentSummary(output, summaries)
				return nil
			}
			if output.IsJSON() {
				return output.JSON(runs)
			}
			displayPaperExperimentRuns(output, runs)
			return nil
		},
	}
	cmd.Flags().Int("limit", 50, "Maximum experiment runs to show")
	cmd.Flags().Int("days", 30, "Number of recent days to include")
	cmd.Flags().String("symbol", "", "Filter by symbol")
	cmd.Flags().String("strategy", "", "Filter by strategy")
	cmd.Flags().String("regime-mode", "", "Filter by regime mode: strict, allow-unknown, or explore")
	cmd.Flags().String("source", "", "Filter by source, for example cli or daemon")
	cmd.Flags().Bool("summary", false, "Group experiment runs by source and regime mode")
	cmd.Flags().Bool("compare", false, "Compare experiment cohorts using run flow and realized paper outcomes")
	cmd.Flags().Int("min-outcome-decisive", 5, "Minimum decisive paper outcomes before judging a cohort")
	cmd.Flags().Float64("min-win-rate", 50, "Minimum paper win rate for a leading cohort")
	cmd.Flags().Float64("min-expectancy", 0, "Minimum paper expectancy for a leading cohort")
	return cmd
}

type paperExperimentSummary struct {
	Source                 string  `json:"source"`
	RegimeMode             string  `json:"regime_mode"`
	Runs                   int     `json:"runs"`
	DryRuns                int     `json:"dry_runs"`
	CandidatesChecked      int     `json:"candidates_checked"`
	PredictionsCreated     int     `json:"predictions_created"`
	OutcomesEvaluated      int     `json:"outcomes_evaluated"`
	Blocked                int     `json:"blocked"`
	NoSignal               int     `json:"no_signal"`
	Errors                 int     `json:"errors"`
	CandidatesPaused       int     `json:"candidates_paused"`
	CandidatesReady        int     `json:"candidates_ready"`
	TrustedPredictions     int     `json:"trusted_predictions"`
	ExploratoryPredictions int     `json:"exploratory_predictions"`
	TrustedDecisive        int     `json:"trusted_decisive"`
	ExploratoryDecisive    int     `json:"exploratory_decisive"`
	PredictionRate         float64 `json:"prediction_rate"`
	BlockRate              float64 `json:"block_rate"`
	NoSignalRate           float64 `json:"no_signal_rate"`
	ErrorRate              float64 `json:"error_rate"`
}

type paperExperimentComparisonOptions struct {
	MinOutcomeDecisive int
	MinWinRate         float64
	MinExpectancy      float64
}

type paperExperimentCohortComparison struct {
	Source                 string  `json:"source"`
	RegimeMode             string  `json:"regime_mode"`
	Runs                   int     `json:"runs"`
	DryRuns                int     `json:"dry_runs"`
	CandidatesChecked      int     `json:"candidates_checked"`
	PredictionsCreated     int     `json:"predictions_created"`
	PredictionRate         float64 `json:"prediction_rate"`
	BlockRate              float64 `json:"block_rate"`
	NoSignalRate           float64 `json:"no_signal_rate"`
	ErrorRate              float64 `json:"error_rate"`
	TrustedPredictions     int     `json:"trusted_predictions"`
	ExploratoryPredictions int     `json:"exploratory_predictions"`
	PaperPredictions       int     `json:"paper_predictions"`
	PaperEvaluated         int     `json:"paper_evaluated"`
	PaperDecisive          int     `json:"paper_decisive"`
	PaperWinRate           float64 `json:"paper_win_rate"`
	PaperExpectancy        float64 `json:"paper_expectancy"`
	PaperProfitFactor      float64 `json:"paper_profit_factor"`
	PaperExpiredRate       float64 `json:"paper_expired_rate"`
	Score                  float64 `json:"score"`
	Verdict                string  `json:"verdict"`
	Reason                 string  `json:"reason"`
}

func summarizePaperExperimentRuns(runs []models.PaperExperimentRun) []paperExperimentSummary {
	byKey := make(map[string]*paperExperimentSummary)
	order := make([]string, 0)
	for _, run := range runs {
		key := run.Source + "|" + run.RegimeMode
		item, ok := byKey[key]
		if !ok {
			item = &paperExperimentSummary{Source: run.Source, RegimeMode: run.RegimeMode}
			byKey[key] = item
			order = append(order, key)
		}
		item.Runs++
		if run.DryRun {
			item.DryRuns++
		}
		item.CandidatesChecked += run.CandidatesChecked
		item.PredictionsCreated += run.PredictionsCreated
		item.OutcomesEvaluated += run.OutcomesEvaluated
		item.Blocked += run.Blocked
		item.NoSignal += run.NoSignal
		item.Errors += run.Errors
		item.CandidatesPaused += run.CandidatesPaused
		item.CandidatesReady += run.CandidatesReady
		item.TrustedPredictions += run.TrustedPredictions
		item.ExploratoryPredictions += run.ExploratoryPredictions
		item.TrustedDecisive += run.TrustedDecisive
		item.ExploratoryDecisive += run.ExploratoryDecisive
	}
	result := make([]paperExperimentSummary, 0, len(order))
	for _, key := range order {
		item := byKey[key]
		if item.CandidatesChecked > 0 {
			denominator := float64(item.CandidatesChecked)
			item.PredictionRate = float64(item.PredictionsCreated) / denominator * 100
			item.BlockRate = float64(item.Blocked) / denominator * 100
			item.NoSignalRate = float64(item.NoSignal) / denominator * 100
			item.ErrorRate = float64(item.Errors) / denominator * 100
		}
		result = append(result, *item)
	}
	return result
}

func comparePaperExperimentCohorts(runs []models.PaperExperimentRun, predictions []models.PaperPrediction, opts paperExperimentComparisonOptions) []paperExperimentCohortComparison {
	if opts.MinOutcomeDecisive <= 0 {
		opts.MinOutcomeDecisive = 5
	}
	if opts.MinWinRate <= 0 {
		opts.MinWinRate = 50
	}
	summaries := summarizePaperExperimentRuns(runs)
	byModePredictions := groupCandidatePredictionsByRegimeMode(predictions)
	result := make([]paperExperimentCohortComparison, 0, len(summaries))
	bestIndex := -1
	bestScore := 0.0
	for _, summary := range summaries {
		modePredictions := byModePredictions[summary.RegimeMode]
		stats := calculatePaperCandidateOutcomeStats(modePredictions)
		item := paperExperimentCohortComparison{
			Source:                 summary.Source,
			RegimeMode:             summary.RegimeMode,
			Runs:                   summary.Runs,
			DryRuns:                summary.DryRuns,
			CandidatesChecked:      summary.CandidatesChecked,
			PredictionsCreated:     summary.PredictionsCreated,
			PredictionRate:         summary.PredictionRate,
			BlockRate:              summary.BlockRate,
			NoSignalRate:           summary.NoSignalRate,
			ErrorRate:              summary.ErrorRate,
			TrustedPredictions:     summary.TrustedPredictions,
			ExploratoryPredictions: summary.ExploratoryPredictions,
			PaperPredictions:       stats.total,
			PaperEvaluated:         stats.evaluated,
			PaperDecisive:          stats.decisive,
			PaperWinRate:           stats.winRate,
			PaperExpectancy:        stats.expectancy,
			PaperProfitFactor:      stats.profitFactor,
			PaperExpiredRate:       stats.expiredRate,
		}
		item.Score = paperExperimentCohortScore(item, opts)
		item.Verdict, item.Reason = paperExperimentCohortVerdict(item, opts)
		if item.Verdict == "PROMISING" && (bestIndex == -1 || item.Score > bestScore) {
			bestIndex = len(result)
			bestScore = item.Score
		}
		result = append(result, item)
	}
	if bestIndex >= 0 {
		result[bestIndex].Verdict = "LEADING"
		result[bestIndex].Reason = "best flow-adjusted cohort with acceptable realized evidence"
	}
	return result
}

func groupCandidatePredictionsByRegimeMode(predictions []models.PaperPrediction) map[string][]models.PaperPrediction {
	result := make(map[string][]models.PaperPrediction)
	for _, prediction := range predictions {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(prediction.SetupName)), "candidate:") {
			continue
		}
		mode := paperPredictionRegimeMode(prediction)
		if mode == "" {
			mode = regimeModeStrict
		}
		result[mode] = append(result[mode], prediction)
	}
	return result
}

func paperPredictionRegimeMode(prediction models.PaperPrediction) string {
	for _, gate := range prediction.Gates {
		if strings.EqualFold(gate.Name, "regime_mode") {
			mode, err := parseCandidateRegimeMode(gate.Reason)
			if err == nil {
				return mode
			}
			return strings.ToLower(strings.TrimSpace(gate.Reason))
		}
	}
	return ""
}

func paperExperimentCohortScore(item paperExperimentCohortComparison, opts paperExperimentComparisonOptions) float64 {
	if item.CandidatesChecked == 0 || item.ErrorRate > 0 {
		return 0
	}
	flow := item.PredictionRate - item.ErrorRate*2
	if item.PaperDecisive < opts.MinOutcomeDecisive {
		return flow * 0.25
	}
	outcome := item.PaperExpectancy*20 + item.PaperWinRate - item.PaperExpiredRate*0.25
	if item.PaperProfitFactor > 0 && item.PaperProfitFactor < 1 {
		outcome -= (1 - item.PaperProfitFactor) * 25
	}
	return flow + outcome
}

func paperExperimentCohortVerdict(item paperExperimentCohortComparison, opts paperExperimentComparisonOptions) (string, string) {
	switch {
	case item.Runs == 0:
		return "NO_RUNS", "no experiment runs in the selected period"
	case item.CandidatesChecked == 0:
		return "NO_FLOW", "no candidates checked"
	case item.ErrorRate > 0:
		return "INVESTIGATE", fmt.Sprintf("error rate %.1f%%", item.ErrorRate)
	case item.PredictionsCreated == 0:
		return "LOW_FLOW", "no predictions created by this cohort"
	case item.PaperDecisive < opts.MinOutcomeDecisive:
		return "COLLECT_EVIDENCE", fmt.Sprintf("need at least %d decisive outcomes, got %d", opts.MinOutcomeDecisive, item.PaperDecisive)
	case item.PaperWinRate < opts.MinWinRate:
		return "WEAK_OUTCOME", fmt.Sprintf("win rate %.1f%% < %.1f%%", item.PaperWinRate, opts.MinWinRate)
	case item.PaperExpectancy < opts.MinExpectancy:
		return "WEAK_OUTCOME", fmt.Sprintf("expectancy %.2f%% < %.2f%%", item.PaperExpectancy, opts.MinExpectancy)
	case item.PaperProfitFactor > 0 && item.PaperProfitFactor < 1:
		return "WEAK_OUTCOME", fmt.Sprintf("profit factor %.2f < 1.00", item.PaperProfitFactor)
	default:
		return "PROMISING", "flow and realized outcomes meet thresholds"
	}
}

func displayPaperExperimentRuns(output *Output, runs []models.PaperExperimentRun) {
	output.Bold("Paper Experiment Runs")
	output.Println()
	if len(runs) == 0 {
		output.Info("No paper experiment runs found")
		return
	}
	table := NewTable(output, "Time", "Source", "Mode", "Symbol", "Strategy", "Dry", "Cand", "Pred", "Block", "NoSig", "Eval", "Pause", "Ready", "Xplr", "Readiness")
	for _, run := range runs {
		dryRun := "NO"
		if run.DryRun {
			dryRun = "YES"
		}
		table.AddRow(
			FormatDateTime(run.StartedAt),
			run.Source,
			run.RegimeMode,
			emptyDash(run.Symbol),
			emptyDash(run.Strategy),
			dryRun,
			fmt.Sprintf("%d", run.CandidatesChecked),
			fmt.Sprintf("%d", run.PredictionsCreated),
			fmt.Sprintf("%d", run.Blocked),
			fmt.Sprintf("%d", run.NoSignal),
			fmt.Sprintf("%d", run.OutcomesEvaluated),
			fmt.Sprintf("%d", run.CandidatesPaused),
			fmt.Sprintf("%d", run.CandidatesReady),
			fmt.Sprintf("%d", run.ExploratoryPredictions),
			emptyDash(run.ReadinessDecision),
		)
	}
	table.Render()
}

func displayPaperExperimentSummary(output *Output, summaries []paperExperimentSummary) {
	output.Bold("Paper Experiment Summary")
	output.Println()
	if len(summaries) == 0 {
		output.Info("No paper experiment runs found")
		return
	}
	table := NewTable(output, "Source", "Mode", "Runs", "Dry", "Cand", "Pred", "Pred%", "Block%", "NoSig%", "Err%", "Eval", "Ready", "Xplr")
	for _, summary := range summaries {
		table.AddRow(
			summary.Source,
			summary.RegimeMode,
			fmt.Sprintf("%d", summary.Runs),
			fmt.Sprintf("%d", summary.DryRuns),
			fmt.Sprintf("%d", summary.CandidatesChecked),
			fmt.Sprintf("%d", summary.PredictionsCreated),
			fmt.Sprintf("%.1f%%", summary.PredictionRate),
			fmt.Sprintf("%.1f%%", summary.BlockRate),
			fmt.Sprintf("%.1f%%", summary.NoSignalRate),
			fmt.Sprintf("%.1f%%", summary.ErrorRate),
			fmt.Sprintf("%d", summary.OutcomesEvaluated),
			fmt.Sprintf("%d", summary.CandidatesReady),
			fmt.Sprintf("%d", summary.ExploratoryPredictions),
		)
	}
	table.Render()
}

func displayPaperExperimentComparison(output *Output, comparisons []paperExperimentCohortComparison) {
	output.Bold("Paper Experiment Cohort Comparison")
	output.Println()
	if len(comparisons) == 0 {
		output.Info("No paper experiment runs found")
		return
	}
	table := NewTable(output, "Verdict", "Source", "Mode", "Runs", "Cand", "Pred%", "Block%", "NoSig%", "Paper", "Dec", "Win", "Expect", "PF", "Score", "Reason")
	for _, item := range comparisons {
		table.AddRow(
			item.Verdict,
			item.Source,
			item.RegimeMode,
			fmt.Sprintf("%d", item.Runs),
			fmt.Sprintf("%d", item.CandidatesChecked),
			fmt.Sprintf("%.1f%%", item.PredictionRate),
			fmt.Sprintf("%.1f%%", item.BlockRate),
			fmt.Sprintf("%.1f%%", item.NoSignalRate),
			fmt.Sprintf("%d", item.PaperPredictions),
			fmt.Sprintf("%d", item.PaperDecisive),
			fmt.Sprintf("%.1f%%", item.PaperWinRate),
			FormatPercent(item.PaperExpectancy),
			fmt.Sprintf("%.2f", item.PaperProfitFactor),
			fmt.Sprintf("%.1f", item.Score),
			item.Reason,
		)
	}
	table.Render()
}

func emptyDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func newPaperCandidatesCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "candidates",
		Short: "Show promoted paper-soak candidates",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if app.Store == nil {
				output.Warning("Paper candidate store is not available")
				return nil
			}

			limit, _ := cmd.Flags().GetInt("limit")
			symbol, _ := cmd.Flags().GetString("symbol")
			strategy, _ := cmd.Flags().GetString("strategy")
			status, _ := cmd.Flags().GetString("status")
			regime, _ := cmd.Flags().GetString("regime")
			regime = strings.ToLower(strings.TrimSpace(regime))
			filter := models.PaperCandidateFilter{
				Symbol:   strings.ToUpper(strings.TrimSpace(symbol)),
				Strategy: strings.ToLower(strings.TrimSpace(strategy)),
				Status:   strings.ToUpper(strings.TrimSpace(status)),
				Limit:    limit,
			}

			candidates, err := app.Store.GetPaperCandidates(ctx, filter)
			if err != nil {
				output.Error("Failed to get paper candidates: %v", err)
				return err
			}
			if output.IsJSON() {
				return output.JSON(candidates)
			}
			if len(candidates) == 0 {
				output.Info("No promoted paper candidates found")
				return nil
			}

			output.Bold("Paper Soak Candidates")
			output.Println()
			headers := []string{"Status", "Symbol", "Strategy", "Variant", "TF", "Setup", "Ret", "Val", "Tr", "VTr", "PF", "DD", "Allowed", "Blocked"}
			if regime != "" {
				headers = append(headers, "Regime Gate")
			}
			table := NewTable(output, headers...)
			for _, candidate := range candidates {
				row := []string{
					candidate.Status,
					candidate.Symbol,
					candidate.Strategy,
					candidate.ParamVariant,
					candidate.Timeframe,
					candidate.Setup,
					FormatPercent(candidate.ReturnPct),
					FormatPercent(candidate.ValidationReturnPct),
					fmt.Sprintf("%d", candidate.Trades),
					fmt.Sprintf("%d", candidate.ValidationTrades),
					fmt.Sprintf("%.2f", candidate.ProfitFactor),
					fmt.Sprintf("%.1f%%", candidate.MaxDrawdownPct),
					strings.Join(candidate.AllowedRegimes, ","),
					strings.Join(candidate.BlockedRegimes, ","),
				}
				if regime != "" {
					row = append(row, paperCandidateRegimeGate(candidate, regime))
				}
				table.AddRow(row...)
			}
			table.Render()
			return nil
		},
	}
	cmd.Flags().Int("limit", 20, "Maximum candidates to show")
	cmd.Flags().String("symbol", "", "Filter by symbol")
	cmd.Flags().String("strategy", "", "Filter by strategy")
	cmd.Flags().String("status", models.PaperCandidateStatusActive, "Filter by status")
	cmd.Flags().String("regime", "", "Show candidate gate decision for a current regime")
	return cmd
}

func paperCandidateRegimeGate(candidate models.PaperCandidate, regime string) string {
	for _, blocked := range candidate.BlockedRegimes {
		if strings.EqualFold(blocked, regime) {
			return "BLOCK"
		}
	}
	for _, allowed := range candidate.AllowedRegimes {
		if strings.EqualFold(allowed, regime) {
			return "ALLOW"
		}
	}
	return "UNKNOWN"
}

func newPaperStatsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show paper prediction calibration and expectancy",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if app.Store == nil {
				output.Warning("Paper prediction store is not available")
				return nil
			}

			days, _ := cmd.Flags().GetInt("days")
			symbol, _ := cmd.Flags().GetString("symbol")
			filter := store.PaperPredictionFilter{
				Symbol: strings.ToUpper(strings.TrimSpace(symbol)),
			}
			if days > 0 {
				filter.StartDate = time.Now().AddDate(0, 0, -days)
				filter.EndDate = time.Now()
			}

			report, err := app.Store.GetPaperPredictionReport(ctx, filter)
			if err != nil {
				output.Error("Failed to get paper stats: %v", err)
				return err
			}
			if output.IsJSON() {
				return output.JSON(report)
			}
			if report.TotalPredictions == 0 {
				output.Info("No paper predictions found")
				return nil
			}

			output.Bold("Paper Prediction Calibration")
			if days > 0 {
				output.Printf("  Period: last %d days\n", days)
			}
			if filter.Symbol != "" {
				output.Printf("  Symbol: %s\n", filter.Symbol)
			}
			output.Println()

			output.Printf("  Total:       %d (%d active, %d evaluated)\n", report.TotalPredictions, report.ActivePredictions, report.Evaluated)
			output.Printf("  Decisive:    %d | Right: %d | Wrong: %d | Expired: %d\n", report.Decisive, report.RightPredictions, report.WrongPredictions, report.ExpiredPredictions)
			output.Printf("  Win Rate:    %.1f%%\n", report.WinRate)
			output.Printf("  Avg Conf:    %.1f%%\n", report.AvgConfidence)
			output.Printf("  Avg P&L:     %.2f%%\n", report.AvgPnLPercent)
			output.Printf("  Expectancy:  %.2f%% per decisive prediction\n", report.Expectancy)
			output.Printf("  Expired:     %.1f%% of evaluated\n", report.ExpiredRate)
			output.Printf("  Best/Worst:  %.2f%% / %.2f%%\n", report.BestPrediction, report.WorstPrediction)
			output.Println()

			printPaperGroupStats(output, "By Confidence", report.ByConfidence)
			printPaperGroupStats(output, "By Action", report.ByAction)
			if filter.Symbol == "" {
				printPaperGroupStats(output, "By Symbol", report.BySymbol)
			}

			if len(report.Overconfidence) > 0 {
				output.Bold("Calibration Warnings")
				for _, warning := range report.Overconfidence {
					output.Warning("%s: avg confidence %.1f%% vs win rate %.1f%% over %d decisive predictions",
						warning.Bucket,
						warning.AvgConfidence,
						warning.WinRate,
						warning.SampleSize)
				}
			}

			return nil
		},
	}
	cmd.Flags().Int("days", 30, "Number of recent days to analyze")
	cmd.Flags().String("symbol", "", "Filter by symbol")
	return cmd
}

func printPaperGroupStats(output *Output, title string, groups []models.PaperPredictionGroupStats) {
	if len(groups) == 0 {
		return
	}
	output.Bold(title)
	table := NewTable(output, "Group", "Total", "Decisive", "Win", "Avg Conf", "Avg P&L", "Expectancy", "Expired")
	for _, group := range groups {
		table.AddRow(
			group.Key,
			fmt.Sprintf("%d", group.TotalPredictions),
			fmt.Sprintf("%d", group.Decisive),
			fmt.Sprintf("%.1f%%", group.WinRate),
			fmt.Sprintf("%.1f%%", group.AvgConfidence),
			fmt.Sprintf("%.2f%%", group.AvgPnLPercent),
			fmt.Sprintf("%.2f%%", group.Expectancy),
			fmt.Sprintf("%.1f%%", group.ExpiredRate),
		)
	}
	table.Render()
	output.Println()
}

func newPaperPredictionsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "predictions",
		Short: "Show persistent paper prediction history",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if app.Store == nil {
				output.Warning("Paper prediction store is not available")
				return nil
			}

			limit, _ := cmd.Flags().GetInt("limit")
			activeOnly, _ := cmd.Flags().GetBool("active")
			symbol, _ := cmd.Flags().GetString("symbol")
			filter := store.PaperPredictionFilter{
				Symbol: strings.ToUpper(strings.TrimSpace(symbol)),
				Limit:  limit,
			}
			if activeOnly {
				evaluated := false
				filter.Evaluated = &evaluated
			}

			predictions, err := app.Store.GetPaperPredictions(ctx, filter)
			if err != nil {
				output.Error("Failed to get paper predictions: %v", err)
				return err
			}
			if output.IsJSON() {
				return output.JSON(predictions)
			}
			if len(predictions) == 0 {
				output.Info("No paper predictions found")
				return nil
			}

			output.Bold("Paper Predictions")
			output.Println()
			table := NewTable(output, "Time", "Symbol", "Action", "Conf", "Entry", "Exit", "Outcome", "P&L")
			for _, prediction := range predictions {
				exit := "-"
				if prediction.Evaluated {
					exit = FormatPrice(prediction.ExitPrice)
				}
				outcome := prediction.Outcome
				if outcome == "" {
					outcome = "ACTIVE"
				}
				pnl := "-"
				if prediction.Evaluated {
					pnl = FormatPercent(prediction.PnLPercent)
				}
				table.AddRow(
					FormatDateTime(prediction.CreatedAt),
					prediction.Symbol,
					prediction.Action,
					FormatConfidence(prediction.Confidence),
					FormatPrice(prediction.EntryPrice),
					exit,
					outcome,
					pnl,
				)
			}
			table.Render()
			return nil
		},
	}
	cmd.Flags().Int("limit", 20, "Maximum predictions to show")
	cmd.Flags().Bool("active", false, "Show only active predictions")
	cmd.Flags().String("symbol", "", "Filter by symbol")
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
