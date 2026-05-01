// Package cli provides the command-line interface for the trading application.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/agents"
	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

// Prediction represents an AI prediction for tracking.
type Prediction struct {
	ID           string
	Symbol       string
	Action       string  // BUY, SELL
	Confidence   float64
	EntryPrice   float64
	TargetPrice  float64
	StopLoss     float64
	TimeWindow   time.Duration
	CreatedAt    time.Time
	ExpiresAt    time.Time
	Reasoning    string
	
	// Outcome tracking
	Evaluated    bool
	ExitPrice    float64
	Outcome      string // RIGHT, WRONG, EXPIRED
	PnLPercent   float64
}

// PaperTracker tracks AI predictions without executing trades.
type PaperTracker struct {
	mu          sync.RWMutex
	predictions map[string]*Prediction
	history     []*Prediction
	stats       PaperStats
}

// GetRecentHistory returns the last N evaluated predictions for context.
func (pt *PaperTracker) GetRecentHistory(n int) []*Prediction {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	
	if len(pt.history) == 0 {
		return nil
	}
	
	start := 0
	if len(pt.history) > n {
		start = len(pt.history) - n
	}
	
	result := make([]*Prediction, len(pt.history)-start)
	copy(result, pt.history[start:])
	return result
}

// PaperStats holds prediction accuracy statistics.
type PaperStats struct {
	TotalPredictions int
	RightPredictions int
	WrongPredictions int
	ExpiredPredictions int
	WinRate          float64
	AvgConfidence    float64
	AvgPnLPercent    float64
	BestPrediction   float64
	WorstPrediction  float64
}

// NewPaperTracker creates a new paper trading tracker.
func NewPaperTracker() *PaperTracker {
	return &PaperTracker{
		predictions: make(map[string]*Prediction),
		history:     make([]*Prediction, 0),
	}
}

// AddPrediction adds a new prediction to track.
func (pt *PaperTracker) AddPrediction(p *Prediction) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	
	p.ID = fmt.Sprintf("%s-%d", p.Symbol, time.Now().UnixNano())
	pt.predictions[p.ID] = p
	pt.stats.TotalPredictions++
	pt.stats.AvgConfidence = ((pt.stats.AvgConfidence * float64(pt.stats.TotalPredictions-1)) + p.Confidence) / float64(pt.stats.TotalPredictions)
}

// EvaluatePrediction evaluates a prediction against current price.
// Outcomes: RIGHT (target hit), WRONG (stop loss hit), EXPIRED (time ran out)
// Win rate only counts RIGHT vs WRONG for honest feedback.
func (pt *PaperTracker) EvaluatePrediction(id string, currentPrice float64) *Prediction {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	
	p, ok := pt.predictions[id]
	if !ok || p.Evaluated {
		return nil
	}
	
	p.ExitPrice = currentPrice
	p.Evaluated = true
	
	// Calculate P&L
	if p.Action == "BUY" {
		p.PnLPercent = ((currentPrice - p.EntryPrice) / p.EntryPrice) * 100
	} else {
		p.PnLPercent = ((p.EntryPrice - currentPrice) / p.EntryPrice) * 100
	}
	
	// Determine outcome
	now := time.Now()
	if now.After(p.ExpiresAt) {
		// Time expired - always mark as EXPIRED (separate from RIGHT/WRONG)
		p.Outcome = "EXPIRED"
		pt.stats.ExpiredPredictions++
	} else {
		// Check if target or stop loss hit
		if p.Action == "BUY" {
			if currentPrice >= p.TargetPrice {
				p.Outcome = "RIGHT"
				pt.stats.RightPredictions++
			} else if currentPrice <= p.StopLoss {
				p.Outcome = "WRONG"
				pt.stats.WrongPredictions++
			}
		} else {
			if currentPrice <= p.TargetPrice {
				p.Outcome = "RIGHT"
				pt.stats.RightPredictions++
			} else if currentPrice >= p.StopLoss {
				p.Outcome = "WRONG"
				pt.stats.WrongPredictions++
			}
		}
	}
	
	// Update stats
	if p.Outcome != "" {
		pt.history = append(pt.history, p)
		delete(pt.predictions, id)
		
		// Update average P&L (includes all outcomes)
		evaluated := pt.stats.RightPredictions + pt.stats.WrongPredictions + pt.stats.ExpiredPredictions
		pt.stats.AvgPnLPercent = ((pt.stats.AvgPnLPercent * float64(evaluated-1)) + p.PnLPercent) / float64(evaluated)
		
		// Update best/worst
		if p.PnLPercent > pt.stats.BestPrediction {
			pt.stats.BestPrediction = p.PnLPercent
		}
		if p.PnLPercent < pt.stats.WorstPrediction {
			pt.stats.WorstPrediction = p.PnLPercent
		}
		
		// Win rate only counts decisive outcomes (RIGHT vs WRONG)
		// EXPIRED trades don't count - they indicate signal didn't play out
		decisiveCount := pt.stats.RightPredictions + pt.stats.WrongPredictions
		if decisiveCount > 0 {
			pt.stats.WinRate = float64(pt.stats.RightPredictions) / float64(decisiveCount) * 100
		}
	}
	
	return p
}

// GetActivePredictions returns all active predictions.
func (pt *PaperTracker) GetActivePredictions() []*Prediction {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	
	result := make([]*Prediction, 0, len(pt.predictions))
	for _, p := range pt.predictions {
		result = append(result, p)
	}
	return result
}

// GetStats returns current statistics.
func (pt *PaperTracker) GetStats() PaperStats {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	return pt.stats
}

// CheckExpiredPredictions checks and evaluates expired predictions.
// EXPIRED is now a separate outcome - not counted in win rate calculation.
// Win rate = RIGHT / (RIGHT + WRONG), EXPIRED trades are tracked separately.
func (pt *PaperTracker) CheckExpiredPredictions(prices map[string]float64) []*Prediction {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	
	var expired []*Prediction
	now := time.Now()
	
	for id, p := range pt.predictions {
		if now.After(p.ExpiresAt) && !p.Evaluated {
			price, ok := prices[p.Symbol]
			if !ok {
				continue
			}
			
			p.ExitPrice = price
			p.Evaluated = true
			
			// Calculate P&L
			if p.Action == "BUY" {
				p.PnLPercent = ((price - p.EntryPrice) / p.EntryPrice) * 100
			} else {
				p.PnLPercent = ((p.EntryPrice - price) / p.EntryPrice) * 100
			}
			
			// EXPIRED is always EXPIRED - separate from RIGHT/WRONG
			// This prevents inflating win rate with lucky expired trades
			p.Outcome = "EXPIRED"
			pt.stats.ExpiredPredictions++
			
			pt.history = append(pt.history, p)
			delete(pt.predictions, id)
			expired = append(expired, p)
			
			// Update P&L stats (include expired in P&L tracking)
			evaluated := pt.stats.RightPredictions + pt.stats.WrongPredictions + pt.stats.ExpiredPredictions
			pt.stats.AvgPnLPercent = ((pt.stats.AvgPnLPercent * float64(evaluated-1)) + p.PnLPercent) / float64(evaluated)
			if p.PnLPercent > pt.stats.BestPrediction {
				pt.stats.BestPrediction = p.PnLPercent
			}
			if p.PnLPercent < pt.stats.WorstPrediction {
				pt.stats.WorstPrediction = p.PnLPercent
			}
			
			// Win rate only counts RIGHT vs WRONG (not EXPIRED)
			// This gives honest feedback about prediction quality
			decisiveCount := pt.stats.RightPredictions + pt.stats.WrongPredictions
			if decisiveCount > 0 {
				pt.stats.WinRate = float64(pt.stats.RightPredictions) / float64(decisiveCount) * 100
			}
		}
	}
	
	return expired
}

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

	return cmd
}

// PredictionResult holds a prediction along with its chain of thought.
type PredictionResult struct {
	Prediction   *Prediction
	ChainOfThought *agents.ChainOfThought
}

// getAIPrediction gets an AI prediction for a symbol.
func getAIPrediction(ctx context.Context, app *App, symbol string, currentPrice float64, timeWindow time.Duration, threshold float64, tracker *PaperTracker, useTools bool) (*Prediction, error) {
	result, err := getAIPredictionVerbose(ctx, app, symbol, currentPrice, timeWindow, threshold, tracker, useTools)
	if err != nil {
		return nil, err
	}
	return result.Prediction, nil
}

// getAIPredictionVerbose gets an AI prediction with full chain of thought.
func getAIPredictionVerbose(ctx context.Context, app *App, symbol string, currentPrice float64, timeWindow time.Duration, threshold float64, tracker *PaperTracker, useTools bool) (*PredictionResult, error) {
	if useTools {
		return getAIPredictionWithToolsVerbose(ctx, app, symbol, currentPrice, timeWindow, threshold, tracker)
	}
	pred, err := getAIPredictionSimple(ctx, app, symbol, currentPrice, timeWindow, threshold, tracker)
	return &PredictionResult{Prediction: pred}, err
}

// getAIPredictionWithTools uses OpenAI function calling for AI predictions.
func getAIPredictionWithTools(ctx context.Context, app *App, symbol string, currentPrice float64, timeWindow time.Duration, threshold float64, tracker *PaperTracker) (*Prediction, error) {
	result, err := getAIPredictionWithToolsVerbose(ctx, app, symbol, currentPrice, timeWindow, threshold, tracker)
	if err != nil {
		return nil, err
	}
	return result.Prediction, nil
}

// getAIPredictionWithToolsVerbose uses OpenAI function calling and returns chain of thought.
func getAIPredictionWithToolsVerbose(ctx context.Context, app *App, symbol string, currentPrice float64, timeWindow time.Duration, threshold float64, tracker *PaperTracker) (*PredictionResult, error) {
	// Create tool executor
	toolExecutor := agents.NewToolExecutor(app.Broker)
	
	// Get recent prediction history for this symbol
	recentHistory := tracker.GetRecentHistory(10)
	var symbolHistory []*Prediction
	for _, p := range recentHistory {
		if p.Symbol == symbol {
			symbolHistory = append(symbolHistory, p)
		}
	}

	// Build prompt for AI with history context
	prompt := buildToolBasedPrompt(symbol, currentPrice, timeWindow, symbolHistory, tracker.GetStats())

	// Execution-grade prompt with HARD GATES and REGIME LOCKS
	systemPrompt := `You are an expert NSE intraday trader. Analyze the stock and make a trading decision.

TOOLS TO USE:
- calculate_rsi: RSI value + direction (current vs previous)
- analyze_volume: Volume ratio vs 20-period average
- calculate_ema_crossover: EMA9/EMA21 trend direction
- calculate_vwap: Price deviation from VWAP
- calculate_adx: Trend strength

=== HARD GATES (ALL MUST PASS) ===

1. RSI REGIME LOCK (eliminates chop/transition zones):
   - BUY allowed ONLY if: RSI > 55 AND RSI rising (current > previous)
   - SELL allowed ONLY if: RSI < 45 AND RSI falling (current < previous)
   - RSI between 45-55 = CHOP ZONE = NO_TRADE always
   - RSI rising but below 55 = NO_TRADE (noise bounce, not trend)
   - RSI falling but above 45 = NO_TRADE (pullback, not reversal)

2. VOLUME EXPANSION GATE:
   - Volume ratio must be > 1.3x average for any trade
   - Low volume = low participation = unreliable signal

3. EMA ALIGNMENT (MANDATORY):
   - BUY: Price must be ABOVE EMA9, EMA9 > EMA21 (bullish structure)
   - SELL: Price must be BELOW EMA9, EMA9 < EMA21 (bearish structure)
   - If EMA disagrees with trade direction = NO_TRADE

4. VWAP EXHAUSTION BLOCK:
   - If price is >0.7% above VWAP = NO BUY (stretched, likely to revert)
   - If price is >0.7% below VWAP = NO SELL (stretched, likely to bounce)
   - Exhausted moves have poor risk/reward

5. TREND STRENGTH:
   - ADX must be > 25 for any trade (was 20, now stricter)
   - ADX < 25 = weak trend = NO_TRADE

=== CONFIDENCE CALCULATION (mechanized) ===
Base = 45, add points:
- RSI slope strong (>5 points move): +10
- Volume ratio >2x: +15, >1.5x: +10, >1.3x: +5
- ADX >35: +15, >30: +10, >25: +5
- EMA cleanly aligned: +10
- VWAP confirms direction (within 0.3%): +5

=== OUTPUT JSON ===
{
  "action": "BUY" or "SELL" or "NO_TRADE",
  "gates_passed": {
    "rsi_regime": true/false,
    "volume_expansion": true/false,
    "ema_alignment": true/false,
    "vwap_not_exhausted": true/false,
    "trend_strength": true/false
  },
  "signal_quality": {
    "rsi_value": 58,
    "rsi_direction": "rising/falling",
    "volume_ratio": 1.5,
    "vwap_deviation_pct": 0.3,
    "adx_value": 28,
    "ema_trend": "bullish/bearish"
  },
  "confidence": <calculated 45-85>,
  "hold_duration": "3m" or "5m" or "10m" or "15m" or "30m",
  "target_price": <price>,
  "stop_loss": <price>,
  "reasoning": "brief: which gates passed/failed"
}

CRITICAL RULES:
- If ANY gate is false → action MUST be "NO_TRADE"
- RSI 45-55 = ALWAYS NO_TRADE (chop zone)
- NO_TRADE is correct risk avoidance, not a failed prediction
- Only trade in CLEAR regimes with strong momentum + participation`

	// Get AI response with tools (verbose to capture chain of thought)
	tools := agents.GetToolDefinitions()
	cot, err := app.LLMClient.CompleteWithToolsVerbose(ctx, systemPrompt, prompt, tools, toolExecutor)
	if err != nil {
		return nil, fmt.Errorf("AI analysis failed: %w", err)
	}

	// Parse response with gate validation
	prediction, err := parsePredictionResponseWithGates(cot.Response, symbol, currentPrice, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	// Filter by threshold
	if prediction == nil || prediction.Confidence < threshold {
		return &PredictionResult{Prediction: nil, ChainOfThought: cot}, nil
	}

	return &PredictionResult{Prediction: prediction, ChainOfThought: cot}, nil
}

// getAIPredictionBacktest uses BacktestToolExecutor with pre-fetched candles.
// This ensures the AI sees data as of the simulation time, not current time.
func getAIPredictionBacktest(ctx context.Context, app *App, symbol string, candles []models.Candle, currentIndex int, currentPrice float64, timeWindow time.Duration, threshold float64, tracker *PaperTracker) (*PredictionResult, error) {
	// Create backtest tool executor with candles up to current simulation point
	toolExecutor := agents.NewBacktestToolExecutor(symbol, candles, currentIndex)
	
	// Get recent prediction history for this symbol
	recentHistory := tracker.GetRecentHistory(10)
	var symbolHistory []*Prediction
	for _, p := range recentHistory {
		if p.Symbol == symbol {
			symbolHistory = append(symbolHistory, p)
		}
	}

	// Build prompt for AI with history context
	prompt := buildToolBasedPrompt(symbol, currentPrice, timeWindow, symbolHistory, tracker.GetStats())

	// Execution-grade prompt with HARD GATES and REGIME LOCKS (same as live)
	systemPrompt := `You are an expert NSE intraday trader. Analyze the stock and make a trading decision.

TOOLS TO USE:
- calculate_rsi: RSI value + direction (current vs previous)
- analyze_volume: Volume ratio vs 20-period average
- calculate_ema_crossover: EMA9/EMA21 trend direction
- calculate_vwap: Price deviation from VWAP
- calculate_adx: Trend strength

=== HARD GATES (ALL MUST PASS) ===

1. RSI REGIME LOCK (eliminates chop/transition zones):
   - BUY allowed ONLY if: RSI > 55 AND RSI rising (current > previous)
   - SELL allowed ONLY if: RSI < 45 AND RSI falling (current < previous)
   - RSI between 45-55 = CHOP ZONE = NO_TRADE always
   - RSI rising but below 55 = NO_TRADE (noise bounce, not trend)
   - RSI falling but above 45 = NO_TRADE (pullback, not reversal)

2. VOLUME EXPANSION GATE:
   - Volume ratio must be > 1.3x average for any trade
   - Low volume = low participation = unreliable signal

3. EMA ALIGNMENT (MANDATORY):
   - BUY: Price must be ABOVE EMA9, EMA9 > EMA21 (bullish structure)
   - SELL: Price must be BELOW EMA9, EMA9 < EMA21 (bearish structure)
   - If EMA disagrees with trade direction = NO_TRADE

4. VWAP EXHAUSTION BLOCK:
   - If price is >0.7% above VWAP = NO BUY (stretched, likely to revert)
   - If price is >0.7% below VWAP = NO SELL (stretched, likely to bounce)
   - Exhausted moves have poor risk/reward

5. TREND STRENGTH:
   - ADX must be > 25 for any trade (was 20, now stricter)
   - ADX < 25 = weak trend = NO_TRADE

=== CONFIDENCE CALCULATION (mechanized) ===
Base = 45, add points:
- RSI slope strong (>5 points move): +10
- Volume ratio >2x: +15, >1.5x: +10, >1.3x: +5
- ADX >35: +15, >30: +10, >25: +5
- EMA cleanly aligned: +10
- VWAP confirms direction (within 0.3%): +5

=== OUTPUT JSON ===
{
  "action": "BUY" or "SELL" or "NO_TRADE",
  "gates_passed": {
    "rsi_regime": true/false,
    "volume_expansion": true/false,
    "ema_alignment": true/false,
    "vwap_not_exhausted": true/false,
    "trend_strength": true/false
  },
  "signal_quality": {
    "rsi_value": 58,
    "rsi_direction": "rising/falling",
    "volume_ratio": 1.5,
    "vwap_deviation_pct": 0.3,
    "adx_value": 28,
    "ema_trend": "bullish/bearish"
  },
  "confidence": <calculated 45-85>,
  "hold_duration": "3m" or "5m" or "10m" or "15m" or "30m",
  "target_price": <price>,
  "stop_loss": <price>,
  "reasoning": "brief: which gates passed/failed"
}

CRITICAL RULES:
- If ANY gate is false → action MUST be "NO_TRADE"
- RSI 45-55 = ALWAYS NO_TRADE (chop zone)
- NO_TRADE is correct risk avoidance, not a failed prediction
- Only trade in CLEAR regimes with strong momentum + participation`

	// Get AI response with tools (verbose to capture chain of thought)
	tools := agents.GetToolDefinitions()
	cot, err := app.LLMClient.CompleteWithToolsVerbose(ctx, systemPrompt, prompt, tools, toolExecutor)
	if err != nil {
		return nil, fmt.Errorf("AI analysis failed: %w", err)
	}

	// Parse response with gate validation
	prediction, err := parsePredictionResponseWithGates(cot.Response, symbol, currentPrice, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	// Filter by threshold
	if prediction == nil || prediction.Confidence < threshold {
		return &PredictionResult{Prediction: nil, ChainOfThought: cot}, nil
	}

	return &PredictionResult{Prediction: prediction, ChainOfThought: cot}, nil
}

// getAIPredictionSimple uses simple prompts without tools (fallback).
func getAIPredictionSimple(ctx context.Context, app *App, symbol string, currentPrice float64, timeWindow time.Duration, threshold float64, tracker *PaperTracker) (*Prediction, error) {
	// Get historical data for context
	candles, err := app.Broker.GetHistorical(ctx, broker.HistoricalRequest{
		Symbol:    symbol,
		Exchange:  models.NSE,
		Timeframe: "15min",
		From:      time.Now().Add(-24 * time.Hour),
		To:        time.Now(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get historical data: %w", err)
	}

	// Get recent prediction history for this symbol
	recentHistory := tracker.GetRecentHistory(10)
	var symbolHistory []*Prediction
	for _, p := range recentHistory {
		if p.Symbol == symbol {
			symbolHistory = append(symbolHistory, p)
		}
	}

	// Build prompt for AI with history context
	prompt := buildPredictionPromptWithHistory(symbol, currentPrice, candles, timeWindow, symbolHistory, tracker.GetStats())

	// Get AI response
	systemPrompt := `You are an aggressive intraday trader analyzing Indian stock market (NSE).
Analyze the given stock data and provide a trading prediction.

IMPORTANT: You MUST make a BUY or SELL prediction. Do NOT say HOLD.
- Look at the recent price movement and momentum
- If price went up in recent candles, predict BUY
- If price went down in recent candles, predict SELL
- Always provide a prediction with confidence level

CRITICAL: You will see your PREVIOUS PREDICTIONS and their OUTCOMES. Learn from them!

Respond ONLY with valid JSON in this exact format:
{
  "action": "BUY" or "SELL",
  "confidence": 0-100,
  "target_price": number,
  "stop_loss": number,
  "reasoning": "brief explanation"
}

Rules:
- ALWAYS choose BUY or SELL, never HOLD
- Target should be 0.3-1% from entry for short windows
- Stop loss should be 0.3-0.5% from entry
- Be decisive!`

	response, err := app.LLMClient.CompleteWithSystem(ctx, systemPrompt, prompt)
	if err != nil {
		return nil, fmt.Errorf("AI analysis failed: %w", err)
	}

	// Parse response
	prediction, err := parsePredictionResponse(response, symbol, currentPrice, timeWindow)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	// Filter by threshold
	if prediction == nil || prediction.Confidence < threshold {
		return nil, nil
	}

	return prediction, nil
}

// getAnalysisTimeframe maps the prediction time window to an appropriate analysis timeframe.
// For short windows, use shorter timeframes to capture relevant price action.
// Supported timeframes: 1min, 5min, 15min, 30min, 1hour, 1day
func getAnalysisTimeframe(timeWindow time.Duration) string {
	switch {
	case timeWindow <= 3*time.Minute:
		return "1min" // For 1-3 min windows, use 1min candles
	case timeWindow <= 10*time.Minute:
		return "5min" // For 3-10 min windows, use 5min candles
	case timeWindow <= 30*time.Minute:
		return "15min" // For 10-30 min windows, use 15min candles
	case timeWindow <= 1*time.Hour:
		return "30min" // For 30-60 min windows, use 30min candles
	case timeWindow <= 4*time.Hour:
		return "1hour" // For 1-4 hour windows, use hourly candles
	default:
		return "1day" // For longer windows, use daily candles
	}
}

// getMarketSession returns the current market session description based on IST time.
// This helps AI understand market dynamics at different times of day.
func getMarketSession(t time.Time) string {
	hour := t.Hour()
	minute := t.Minute()
	totalMins := hour*60 + minute

	switch {
	case totalMins < 9*60+15:
		return "PRE-MARKET (market closed)"
	case totalMins < 9*60+45:
		return "OPENING (9:15-9:45) - HIGH VOLATILITY, avoid new positions, wait for trend"
	case totalMins < 11*60+30:
		return "MORNING SESSION (9:45-11:30) - BEST TRADING WINDOW, trends establish, good volume"
	case totalMins < 13*60:
		return "LUNCH LULL (11:30-13:00) - LOW VOLUME, choppy, avoid trading"
	case totalMins < 14*60+30:
		return "AFTERNOON SESSION (13:00-14:30) - Volume picks up, trend continuation"
	case totalMins < 15*60+30:
		return "CLOSING (14:30-15:30) - SQUARE-OFF PRESSURE, high volatility, quick reversals"
	default:
		return "AFTER-MARKET (market closed)"
	}
}

// buildToolBasedPrompt builds the prompt for tool-based AI prediction.
func buildToolBasedPrompt(symbol string, currentPrice float64, timeWindow time.Duration, history []*Prediction, stats PaperStats) string {
	var sb strings.Builder

	// Get IST time
	ist, _ := time.LoadLocation("Asia/Kolkata")
	now := time.Now().In(ist)
	
	// Determine market session
	marketSession := getMarketSession(now)

	sb.WriteString(fmt.Sprintf("Analyze %s for a trading decision.\n\n", symbol))
	sb.WriteString(fmt.Sprintf("Current Price: %.2f\n", currentPrice))
	sb.WriteString(fmt.Sprintf("Time Window: %s\n", timeWindow))
	sb.WriteString(fmt.Sprintf("Current Time: %s IST\n", now.Format("15:04:05")))
	sb.WriteString(fmt.Sprintf("Market Session: %s\n\n", marketSession))

	// Add previous predictions and outcomes for learning
	if len(history) > 0 {
		sb.WriteString("=== YOUR PREVIOUS PREDICTIONS (Learn from these!) ===\n")
		rightCount := 0
		wrongCount := 0
		for _, p := range history {
			outcomeEmoji := "❌"
			if p.Outcome == "RIGHT" {
				outcomeEmoji = "✅"
				rightCount++
			} else {
				wrongCount++
			}
			sb.WriteString(fmt.Sprintf("  %s %s @ %.2f → %s (P&L: %.2f%%) - %s\n",
				outcomeEmoji, p.Action, p.EntryPrice, p.Outcome, p.PnLPercent, p.Reasoning))
		}
		sb.WriteString(fmt.Sprintf("\nYour recent accuracy: %d RIGHT, %d WRONG\n", rightCount, wrongCount))
		
		// Add learning hints based on patterns
		if wrongCount > rightCount && len(history) >= 3 {
			sb.WriteString("⚠️ IMPORTANT: Your recent predictions have been mostly WRONG. Consider:\n")
			sb.WriteString("  - Being more conservative with confidence levels\n")
			sb.WriteString("  - Setting tighter stop losses\n")
			sb.WriteString("  - Waiting for clearer signals before predicting BUY/SELL\n")
		}
		if stats.WinRate > 0 && stats.WinRate < 45 {
			sb.WriteString(fmt.Sprintf("⚠️ Overall win rate is low (%.1f%%). Adjust your strategy!\n", stats.WinRate))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("INSTRUCTIONS:\n")
	sb.WriteString("1. Use the available tools to analyze the stock\n")
	sb.WriteString("2. Check RSI, Bollinger Bands, and candlestick patterns\n")
	sb.WriteString("3. Look at support/resistance levels\n")
	sb.WriteString("4. Make your prediction based on the tool results\n\n")
	sb.WriteString("Start by calling some analysis tools, then provide your prediction.")

	return sb.String()
}

// buildPredictionPrompt builds the prompt for AI prediction.
func buildPredictionPrompt(symbol string, currentPrice float64, candles []models.Candle, timeWindow time.Duration) string {
	return buildPredictionPromptWithHistory(symbol, currentPrice, candles, timeWindow, nil, PaperStats{})
}

// buildPredictionPromptWithHistory builds the prompt with previous decision history.
func buildPredictionPromptWithHistory(symbol string, currentPrice float64, candles []models.Candle, timeWindow time.Duration, history []*Prediction, stats PaperStats) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Symbol: %s\n", symbol))
	sb.WriteString(fmt.Sprintf("Current Price: %.2f\n", currentPrice))
	sb.WriteString(fmt.Sprintf("Time Window: %s\n", timeWindow))
	sb.WriteString(fmt.Sprintf("Current Time: %s IST\n\n", time.Now().Format("15:04:05")))

	// Add previous predictions and outcomes for learning
	if len(history) > 0 {
		sb.WriteString("=== YOUR PREVIOUS PREDICTIONS (Learn from these!) ===\n")
		rightCount := 0
		wrongCount := 0
		for _, p := range history {
			outcomeEmoji := "❌"
			if p.Outcome == "RIGHT" {
				outcomeEmoji = "✅"
				rightCount++
			} else {
				wrongCount++
			}
			sb.WriteString(fmt.Sprintf("  %s %s @ %.2f → %s (P&L: %.2f%%) - %s\n",
				outcomeEmoji, p.Action, p.EntryPrice, p.Outcome, p.PnLPercent, p.Reasoning))
		}
		sb.WriteString(fmt.Sprintf("\nYour recent accuracy: %d RIGHT, %d WRONG\n", rightCount, wrongCount))
		
		// Add learning hints based on patterns
		if wrongCount > rightCount && len(history) >= 3 {
			sb.WriteString("⚠️ IMPORTANT: Your recent predictions have been mostly WRONG. Consider:\n")
			sb.WriteString("  - Being more conservative with confidence levels\n")
			sb.WriteString("  - Setting tighter stop losses\n")
			sb.WriteString("  - Waiting for clearer signals before predicting BUY/SELL\n")
		}
		if stats.WinRate > 0 && stats.WinRate < 45 {
			sb.WriteString(fmt.Sprintf("⚠️ Overall win rate is low (%.1f%%). Adjust your strategy!\n", stats.WinRate))
		}
		sb.WriteString("\n")
	}

	// Add recent candles
	sb.WriteString("Recent 15-minute candles (last 10):\n")
	start := 0
	if len(candles) > 10 {
		start = len(candles) - 10
	}
	for i := start; i < len(candles); i++ {
		c := candles[i]
		change := ((c.Close - c.Open) / c.Open) * 100
		sb.WriteString(fmt.Sprintf("  %s: O=%.2f H=%.2f L=%.2f C=%.2f V=%d (%.2f%%)\n",
			c.Timestamp.Format("15:04"), c.Open, c.High, c.Low, c.Close, c.Volume, change))
	}

	// Calculate some basic indicators
	if len(candles) >= 5 {
		// Simple momentum
		recent := candles[len(candles)-5:]
		avgVolume := 0.0
		priceChange := 0.0
		for i, c := range recent {
			avgVolume += float64(c.Volume)
			if i > 0 {
				priceChange += c.Close - recent[i-1].Close
			}
		}
		avgVolume /= 5
		
		sb.WriteString(fmt.Sprintf("\nMomentum (5 candles): %.2f\n", priceChange))
		sb.WriteString(fmt.Sprintf("Avg Volume: %.0f\n", avgVolume))
	}

	// Day's range
	if len(candles) > 0 {
		dayHigh := candles[0].High
		dayLow := candles[0].Low
		for _, c := range candles {
			if c.High > dayHigh {
				dayHigh = c.High
			}
			if c.Low < dayLow {
				dayLow = c.Low
			}
		}
		sb.WriteString(fmt.Sprintf("Day Range: %.2f - %.2f\n", dayLow, dayHigh))
		
		// Position in range
		if dayHigh > dayLow {
			position := (currentPrice - dayLow) / (dayHigh - dayLow) * 100
			sb.WriteString(fmt.Sprintf("Position in Range: %.1f%%\n", position))
		}
	}

	sb.WriteString("\nProvide your prediction:")
	return sb.String()
}

// parsePredictionResponse parses the AI response into a Prediction.
// The AI can specify hold_duration (e.g., "3m", "5m", "15m") which overrides the CLI timeWindow.
func parsePredictionResponse(response string, symbol string, currentPrice float64, timeWindow time.Duration) (*Prediction, error) {
	// Extract JSON from response
	response = strings.TrimSpace(response)
	
	// Find JSON in response
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON found in response")
	}
	jsonStr := response[start : end+1]

	var result struct {
		Action       string  `json:"action"`
		Confidence   float64 `json:"confidence"`
		HoldDuration string  `json:"hold_duration"` // AI-specified duration like "3m", "5m", "15m"
		TargetPrice  float64 `json:"target_price"`
		StopLoss     float64 `json:"stop_loss"`
		Reasoning    string  `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Skip NO_TRADE, HOLD, or empty actions
	action := strings.ToUpper(strings.TrimSpace(result.Action))
	if action == "NO_TRADE" || action == "NOTRADE" || action == "NO TRADE" || 
	   action == "HOLD" || action == "WAIT" || action == "" {
		return nil, nil
	}

	// Validate
	if result.TargetPrice <= 0 {
		result.TargetPrice = currentPrice * 1.01 // Default 1% target
	}
	if result.StopLoss <= 0 {
		result.StopLoss = currentPrice * 0.99 // Default 1% stop
	}

	// Parse hold_duration from AI response, fallback to CLI timeWindow
	holdDuration := timeWindow
	if result.HoldDuration != "" {
		if parsed, err := time.ParseDuration(result.HoldDuration); err == nil && parsed > 0 {
			holdDuration = parsed
		}
	}

	now := time.Now()
	return &Prediction{
		Symbol:      symbol,
		Action:      result.Action,
		Confidence:  result.Confidence,
		EntryPrice:  currentPrice,
		TargetPrice: result.TargetPrice,
		StopLoss:    result.StopLoss,
		TimeWindow:  holdDuration,
		CreatedAt:   now,
		ExpiresAt:   now.Add(holdDuration),
		Reasoning:   result.Reasoning,
	}, nil
}

// parsePredictionResponseWithGates parses AI response with hard gate validation.
// Enforces: RSI regime lock, volume expansion, EMA alignment, VWAP exhaustion, trend strength.
func parsePredictionResponseWithGates(response string, symbol string, currentPrice float64, timeWindow time.Duration) (*Prediction, error) {
	// Extract JSON from response
	response = strings.TrimSpace(response)
	
	// Find JSON in response
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no JSON found in response")
	}
	jsonStr := response[start : end+1]

	var result struct {
		Action       string  `json:"action"`
		GatesPassed  struct {
			RSIRegime        bool `json:"rsi_regime"`
			RSIDirection     bool `json:"rsi_direction"`     // backward compat
			VolumeExpansion  bool `json:"volume_expansion"`
			EMAAlignment     bool `json:"ema_alignment"`
			VWAPNotExhausted bool `json:"vwap_not_exhausted"`
			TrendStrength    bool `json:"trend_strength"`
		} `json:"gates_passed"`
		SignalQuality struct {
			RSIValue         float64 `json:"rsi_value"`
			RSIDirection     string  `json:"rsi_direction"`
			RSISlope         string  `json:"rsi_slope"`      // backward compat
			VolumeRatio      float64 `json:"volume_ratio"`
			VWAPDeviationPct float64 `json:"vwap_deviation_pct"`
			ADXValue         float64 `json:"adx_value"`
			EMATrend         string  `json:"ema_trend"`
			MTFAligned       bool    `json:"mtf_aligned"`
		} `json:"signal_quality"`
		Confidence   float64 `json:"confidence"`
		HoldDuration string  `json:"hold_duration"`
		TargetPrice  float64 `json:"target_price"`
		StopLoss     float64 `json:"stop_loss"`
		Reasoning    string  `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Skip NO_TRADE, HOLD, or empty actions - these are correct risk avoidance
	action := strings.ToUpper(strings.TrimSpace(result.Action))
	
	// Only allow BUY or SELL - everything else returns nil
	if action != "BUY" && action != "SELL" {
		return nil, nil
	}

	// HARD GATE ENFORCEMENT: All 5 gates must pass
	// Use RSIRegime if present, fall back to RSIDirection for backward compat
	rsiGate := result.GatesPassed.RSIRegime || result.GatesPassed.RSIDirection
	
	allGatesPassed := rsiGate && 
		result.GatesPassed.VolumeExpansion && 
		result.GatesPassed.EMAAlignment &&
		result.GatesPassed.VWAPNotExhausted &&
		result.GatesPassed.TrendStrength
	
	if !allGatesPassed {
		// AI tried to trade but gates didn't pass - reject as safety net
		return nil, nil
	}

	// Validate prices
	if result.TargetPrice <= 0 {
		result.TargetPrice = currentPrice * 1.01
	}
	if result.StopLoss <= 0 {
		result.StopLoss = currentPrice * 0.99
	}

	// Parse hold_duration
	holdDuration := timeWindow
	if result.HoldDuration != "" {
		if parsed, err := time.ParseDuration(result.HoldDuration); err == nil && parsed > 0 {
			holdDuration = parsed
		}
	}

	// Build reasoning with signal quality
	reasoning := result.Reasoning
	if reasoning == "" {
		reasoning = fmt.Sprintf("RSI=%.0f %s | Vol=%.1fx | VWAP=%.2f%% | ADX=%.0f | EMA=%s",
			result.SignalQuality.RSIValue,
			result.SignalQuality.RSIDirection,
			result.SignalQuality.VolumeRatio,
			result.SignalQuality.VWAPDeviationPct,
			result.SignalQuality.ADXValue,
			result.SignalQuality.EMATrend)
	}

	now := time.Now()
	return &Prediction{
		Symbol:      symbol,
		Action:      result.Action,
		Confidence:  result.Confidence,
		EntryPrice:  currentPrice,
		TargetPrice: result.TargetPrice,
		StopLoss:    result.StopLoss,
		TimeWindow:  holdDuration,
		CreatedAt:   now,
		ExpiresAt:   now.Add(holdDuration),
		Reasoning:   reasoning,
	}, nil
}

// displayPaperTrading displays the paper trading view.
func displayPaperTrading(output *Output, symbols []string, ticks map[string]models.Tick, tracker *PaperTracker) {
	displayPaperTradingWithStatus(output, symbols, ticks, tracker, "")
}

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
		candles, err := app.Broker.GetHistorical(ctx, broker.HistoricalRequest{
			Symbol:    symbol,
			Exchange:  models.Exchange(exchange),
			Timeframe: "15min",
			From:      from,
			To:        to,
		})
		if err != nil {
			output.Error("Failed to fetch data for %s: %v", symbol, err)
			continue
		}

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
