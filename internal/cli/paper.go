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
		// Time expired - check if it would have been profitable
		if p.PnLPercent > 0 {
			p.Outcome = "RIGHT"
			pt.stats.RightPredictions++
		} else {
			p.Outcome = "EXPIRED"
			pt.stats.ExpiredPredictions++
		}
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
		
		// Update average P&L
		evaluated := pt.stats.RightPredictions + pt.stats.WrongPredictions + pt.stats.ExpiredPredictions
		pt.stats.AvgPnLPercent = ((pt.stats.AvgPnLPercent * float64(evaluated-1)) + p.PnLPercent) / float64(evaluated)
		
		// Update best/worst
		if p.PnLPercent > pt.stats.BestPrediction {
			pt.stats.BestPrediction = p.PnLPercent
		}
		if p.PnLPercent < pt.stats.WorstPrediction {
			pt.stats.WorstPrediction = p.PnLPercent
		}
		
		// Update win rate
		if evaluated > 0 {
			pt.stats.WinRate = float64(pt.stats.RightPredictions) / float64(evaluated) * 100
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
			
			if p.PnLPercent > 0 {
				p.Outcome = "RIGHT"
				pt.stats.RightPredictions++
			} else {
				p.Outcome = "WRONG"
				pt.stats.WrongPredictions++
			}
			
			pt.history = append(pt.history, p)
			delete(pt.predictions, id)
			expired = append(expired, p)
			
			// Update stats
			evaluated := pt.stats.RightPredictions + pt.stats.WrongPredictions + pt.stats.ExpiredPredictions
			pt.stats.AvgPnLPercent = ((pt.stats.AvgPnLPercent * float64(evaluated-1)) + p.PnLPercent) / float64(evaluated)
			if p.PnLPercent > pt.stats.BestPrediction {
				pt.stats.BestPrediction = p.PnLPercent
			}
			if p.PnLPercent < pt.stats.WorstPrediction {
				pt.stats.WorstPrediction = p.PnLPercent
			}
			if evaluated > 0 {
				pt.stats.WinRate = float64(pt.stats.RightPredictions) / float64(evaluated) * 100
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
				return runBacktestMode(ctx, app, output, symbols, exchange, timeWindow, threshold, useTools, backtestDays, fromDate, toDate)
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

			// Display ticker
			displayTicker := time.NewTicker(500 * time.Millisecond)
			defer displayTicker.Stop()

			// Track last analysis time per symbol
			lastAnalysis := make(map[string]time.Time)
			
			// Track last AI status for display
			var lastAIStatus string
			var lastAIStatusMu sync.Mutex

			for {
				select {
				case <-displayTicker.C:
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

					// Display
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

						// Update status
						lastAIStatusMu.Lock()
						lastAIStatus = fmt.Sprintf("🔍 Analyzing %s at ₹%.2f...", symbol, tick.LTP)
						lastAIStatusMu.Unlock()

						// Get AI prediction
						prediction, err := getAIPrediction(ctx, app, symbol, tick.LTP, timeWindow, threshold, tracker, useTools)
						lastAnalysis[symbol] = time.Now()
						
						if err != nil {
							lastAIStatusMu.Lock()
							lastAIStatus = fmt.Sprintf("⚠ AI error for %s: %v", symbol, err)
							lastAIStatusMu.Unlock()
							continue
						}

						if prediction != nil {
							tracker.AddPrediction(prediction)
							speakNewPrediction(prediction)
							lastAIStatusMu.Lock()
							lastAIStatus = fmt.Sprintf("🎯 NEW: %s %s @ ₹%.2f (%.0f%% conf) → Target: ₹%.2f, SL: ₹%.2f", 
								prediction.Action, symbol, prediction.EntryPrice, prediction.Confidence,
								prediction.TargetPrice, prediction.StopLoss)
							lastAIStatusMu.Unlock()
						} else {
							lastAIStatusMu.Lock()
							lastAIStatus = fmt.Sprintf("⏸ AI suggests HOLD for %s (no clear signal)", symbol)
							lastAIStatusMu.Unlock()
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
	cmd.Flags().Float64P("threshold", "c", 60.0, "Minimum confidence threshold for predictions")
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


// getAIPrediction gets an AI prediction for a symbol.
func getAIPrediction(ctx context.Context, app *App, symbol string, currentPrice float64, timeWindow time.Duration, threshold float64, tracker *PaperTracker, useTools bool) (*Prediction, error) {
	if useTools {
		return getAIPredictionWithTools(ctx, app, symbol, currentPrice, timeWindow, threshold, tracker)
	}
	return getAIPredictionSimple(ctx, app, symbol, currentPrice, timeWindow, threshold, tracker)
}

// getAIPredictionWithTools uses OpenAI function calling for AI predictions.
func getAIPredictionWithTools(ctx context.Context, app *App, symbol string, currentPrice float64, timeWindow time.Duration, threshold float64, tracker *PaperTracker) (*Prediction, error) {
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

	systemPrompt := `You are an aggressive intraday trader analyzing Indian stock market (NSE).
You have access to powerful analysis tools. USE THEM to make informed decisions!

AVAILABLE TOOLS:
- get_historical_data: Fetch OHLCV candle data
- calculate_rsi: Calculate RSI indicator  
- calculate_bollinger_bands: Calculate Bollinger Bands
- calculate_fibonacci_levels: Calculate Fibonacci retracement levels
- get_support_resistance: Get pivot points and S/R levels
- detect_candlestick_patterns: Detect patterns like Doji, Hammer, Engulfing
- detect_chart_patterns: Detect patterns like Head & Shoulders, Triangles
- calculate_atr: Calculate ATR for volatility and stop loss
- calculate_stochastic: Calculate Stochastic Oscillator
- get_mtf_analysis: Multi-timeframe trend analysis

WORKFLOW:
1. First, use 2-3 tools to gather data (RSI, Bollinger, patterns, etc.)
2. Analyze the tool results
3. Make your BUY or SELL prediction based on REAL DATA from tools

IMPORTANT: You MUST make a BUY or SELL prediction. Do NOT say HOLD.
- If RSI > 50 or price above middle Bollinger band, lean BUY
- If RSI < 50 or price below middle Bollinger band, lean SELL
- Always provide a prediction!

After using tools, respond with valid JSON:
{
  "action": "BUY" or "SELL",
  "confidence": 0-100,
  "target_price": number,
  "stop_loss": number,
  "reasoning": "brief explanation including which tools/indicators you used"
}

Rules:
- ALWAYS choose BUY or SELL, never HOLD
- USE AT LEAST 2 TOOLS before making a prediction
- Target should be 0.3-1% from entry for short windows
- Stop loss should be 0.3-0.5% from entry
- Be decisive!`

	// Get AI response with tools
	tools := agents.GetToolDefinitions()
	response, err := app.LLMClient.CompleteWithTools(ctx, systemPrompt, prompt, tools, toolExecutor)
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

// buildToolBasedPrompt builds the prompt for tool-based AI prediction.
func buildToolBasedPrompt(symbol string, currentPrice float64, timeWindow time.Duration, history []*Prediction, stats PaperStats) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Analyze %s for a trading decision.\n\n", symbol))
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
		Action      string  `json:"action"`
		Confidence  float64 `json:"confidence"`
		TargetPrice float64 `json:"target_price"`
		StopLoss    float64 `json:"stop_loss"`
		Reasoning   string  `json:"reasoning"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Skip HOLD actions
	if result.Action == "HOLD" || result.Action == "" {
		return nil, nil
	}

	// Validate
	if result.TargetPrice <= 0 {
		result.TargetPrice = currentPrice * 1.01 // Default 1% target
	}
	if result.StopLoss <= 0 {
		result.StopLoss = currentPrice * 0.99 // Default 1% stop
	}

	now := time.Now()
	return &Prediction{
		Symbol:      symbol,
		Action:      result.Action,
		Confidence:  result.Confidence,
		EntryPrice:  currentPrice,
		TargetPrice: result.TargetPrice,
		StopLoss:    result.StopLoss,
		TimeWindow:  timeWindow,
		CreatedAt:   now,
		ExpiresAt:   now.Add(timeWindow),
		Reasoning:   result.Reasoning,
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
	output.Bold("🤖 AI Paper Trading Mode")
	output.Printf("  %s | %d symbols | %d active predictions\n\n",
		time.Now().Format("15:04:05"), len(symbols), len(predictions))

	// Stats bar
	winRateColor := "\033[33m" // Yellow
	if stats.WinRate >= 60 {
		winRateColor = "\033[32m" // Green
	} else if stats.WinRate < 50 && stats.TotalPredictions > 0 {
		winRateColor = "\033[31m" // Red
	}

	fmt.Printf("Stats: Total=%d | %sWin Rate=%.1f%%\033[0m | Avg P&L=%.2f%% | Best=%.2f%% | Worst=%.2f%%\n\n",
		stats.TotalPredictions, winRateColor, stats.WinRate, stats.AvgPnLPercent, stats.BestPrediction, stats.WorstPrediction)

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

// runBacktestMode runs the paper trading in backtest mode using historical data.
func runBacktestMode(ctx context.Context, app *App, output *Output, symbols []string, exchange string, timeWindow time.Duration, threshold float64, useTools bool, days int, fromDate, toDate string) error {
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

			// Get AI prediction
			prediction, err := getAIPrediction(ctx, app, symbol, currentPrice, timeWindow, threshold, tracker, useTools)
			if err != nil {
				output.Dim("  %s: AI error - %v", currentCandle.Timestamp.Format("Jan 02 15:04"), err)
				continue
			}

			if prediction == nil {
				output.Dim("  %s @ ₹%.2f: HOLD (no signal)", currentCandle.Timestamp.Format("Jan 02 15:04"), currentPrice)
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
	
	winRateColor := ""
	if stats.WinRate >= 60 {
		winRateColor = "\033[32m" // Green
	} else if stats.WinRate < 50 {
		winRateColor = "\033[31m" // Red
	}
	
	output.Printf("  Total Predictions: %d\n", stats.TotalPredictions)
	output.Printf("  Right: %d | Wrong: %d\n", stats.RightPredictions, stats.WrongPredictions)
	fmt.Printf("  Win Rate: %s%.1f%%\033[0m\n", winRateColor, stats.WinRate)
	output.Printf("  Avg P&L: %.2f%%\n", stats.AvgPnLPercent)
	output.Printf("  Best: +%.2f%% | Worst: %.2f%%\n", stats.BestPrediction, stats.WorstPrediction)
	output.Println()

	// Show recent predictions
	if len(tracker.history) > 0 {
		output.Bold("Recent Predictions:")
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
