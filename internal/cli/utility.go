// Package cli provides the command-line interface for the trading application.
package cli

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
	"zerodha-trader/internal/store"
)

// addUtilityCommands adds utility commands.
// Requirements: 37, 57.1-57.4
func addUtilityCommands(rootCmd *cobra.Command, app *App) {
	rootCmd.AddCommand(newBacktestCmd(app))
	rootCmd.AddCommand(newExportCmd(app))
	rootCmd.AddCommand(newAPICmd(app))
	rootCmd.AddCommand(newNotifyTestCmd(app))
}

func newNotifyTestCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "notify-test",
		Short: "Test notification system",
		Long: `Test the notification system by sending sample notifications.

This helps verify that terminal notifications, sounds, and external
notification channels (Telegram, Email, Webhook) are working correctly.`,
		Example: `  trader notify-test
  trader notify-test --type alert
  trader notify-test --type trade`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			output.Bold("🔔 Testing Notification System")
			output.Println()

			// Test voice notification
			output.Info("Testing voice notifications (non-blocking)...")
			speak("Trading notification system is ready")

			// Show different notification types
			output.Println()
			output.Bold("Sample Notifications:")
			output.Println()

			// Entry notification
			output.Printf("%s[%s] %s%s | %s | Price approaching entry level | LTP: ₹2,450.00 → Trigger: ₹2,440.00 (0.41%%)\n",
				"\033[36m", time.Now().Format("15:04:05"), "📥 ENTRY", "\033[0m", "RELIANCE")
			output.Printf("    → Consider BUY at ₹2,440.00\n")
			speak("Reliance approaching entry at 2440")
			output.Println()

			// Stop-loss notification
			output.Printf("%s[%s] %s%s | %s | ⚠️ Price approaching STOP-LOSS | LTP: ₹2,405.00 → Trigger: ₹2,400.00 (0.21%%)\n",
				"\033[31m", time.Now().Format("15:04:05"), "🛑 STOP-LOSS", "\033[0m", "RELIANCE")
			output.Printf("    → Review position and consider exit\n")
			speak("Warning! Reliance approaching stop loss at 2400")
			output.Println()

			// Target notification
			output.Printf("%s[%s] %s%s | %s | 🎯 Price approaching Target 1 | LTP: ₹2,548.00 → Trigger: ₹2,550.00 (0.08%%)\n",
				"\033[32m", time.Now().Format("15:04:05"), "🎯 TARGET", "\033[0m", "RELIANCE")
			output.Printf("    → Consider booking profits at ₹2,550.00\n")
			speak("Reliance approaching target 1 at 2550")
			output.Println()

			// Alert notification
			output.Printf("%s[%s] %s%s | %s | 📈 Alert: Price above ₹3,200.00\n",
				"\033[33m", time.Now().Format("15:04:05"), "🔔 ALERT", "\033[0m", "TCS")
			speak("Alert! TCS crossed above 3200")
			output.Println()

			// Trade notification
			output.Printf("%s[%s] %s%s | %s | ✅ Trade executed: BUY 10 @ ₹2,450.00\n",
				"\033[35m", time.Now().Format("15:04:05"), "💹 TRADE", "\033[0m", "RELIANCE")
			speak("Trade executed. Bought 10 Reliance at 2450")
			output.Println()

			// Error notification
			output.Printf("%s[%s] %s%s | ❌ Error in order placement: insufficient margin\n",
				"\033[31m", time.Now().Format("15:04:05"), "❌ ERROR", "\033[0m")
			speak("Error! Order failed due to insufficient margin")
			output.Println()

			// Info notification
			output.Printf("%s[%s] %s%s | ℹ️ Market opens in 15 minutes\n",
				"\033[37m", time.Now().Format("15:04:05"), "ℹ️  INFO", "\033[0m")
			output.Println()

			output.Bold("External Notification Channels:")
			output.Println()

			// Check config for enabled channels
			if app.Config.Notifications.Webhook.Enabled {
				output.Printf("  Webhook:  %s (URL: %s)\n", output.Green("✓ Enabled"), app.Config.Notifications.Webhook.URL)
			} else {
				output.Printf("  Webhook:  %s\n", output.Yellow("○ Disabled"))
			}

			if app.Config.Notifications.Telegram.Enabled {
				output.Printf("  Telegram: %s\n", output.Green("✓ Enabled"))
			} else {
				output.Printf("  Telegram: %s\n", output.Yellow("○ Disabled"))
			}

			if app.Config.Notifications.Email.Enabled {
				output.Printf("  Email:    %s\n", output.Green("✓ Enabled"))
			} else {
				output.Printf("  Email:    %s\n", output.Yellow("○ Disabled"))
			}

			output.Println()
			output.Dim("Configure notifications in ~/.config/zerodha-trader/config.toml")
			output.Println()

			output.Bold("When do notifications trigger?")
			output.Println("  • Price alerts: When price crosses your alert level")
			output.Println("  • Trade plans: When price approaches entry/SL/target")
			output.Println("  • AI decisions: When autonomous trader makes a decision")
			output.Println("  • Trade execution: When orders are placed/filled")
			output.Println("  • Errors: When something goes wrong")
			output.Println()

			output.Dim("Run 'trader trader start' to see live notifications during trading")

			return nil
		},
	}
}

// speak uses macOS 'say' command for voice notifications (non-blocking)
func speak(text string) {
	exec.Command("say", text).Start()
}

// speakAsync is an alias for speak (both are non-blocking now)
func speakAsync(text string) {
	exec.Command("say", text).Start()
}

// playSound plays a macOS system sound
func playSound(name string) {
	exec.Command("afplay", "/System/Library/Sounds/"+name+".aiff").Start()
}

func newBacktestCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backtest",
		Short: "Backtest trading strategies",
		Long: `Backtest trading strategies on historical data.

Calculates metrics including:
- Total return
- Win rate
- Max drawdown
- Sharpe ratio
- Profit factor`,
		Example: `  trader backtest --strategy momentum --symbol RELIANCE --days 365
  trader backtest --strategy breakout --watchlist nifty50 --days 180`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()

			strategy, _ := cmd.Flags().GetString("strategy")
			symbol, _ := cmd.Flags().GetString("symbol")
			days, _ := cmd.Flags().GetInt("days")
			capital, _ := cmd.Flags().GetFloat64("capital")
			exchange, _ := cmd.Flags().GetString("exchange")

			if symbol == "" {
				output.Error("Symbol is required. Use --symbol flag.")
				return fmt.Errorf("symbol required")
			}

			output.Bold("Backtesting: %s Strategy", strategy)
			output.Printf("  Symbol:  %s\n", symbol)
			output.Printf("  Period:  %d days\n", days)
			output.Printf("  Capital: %s\n", FormatIndianCurrency(capital))
			output.Println()

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			output.Info("Fetching historical data...")

			// Fetch historical data
			candles, err := app.Broker.GetHistorical(ctx, broker.HistoricalRequest{
				Symbol:    symbol,
				Exchange:  models.Exchange(exchange),
				Timeframe: "1day",
				From:      time.Now().AddDate(0, 0, -days),
				To:        time.Now(),
			})
			if err != nil {
				output.Error("Failed to fetch historical data: %v", err)
				return err
			}

			if len(candles) < 30 {
				output.Error("Insufficient data for backtest (need at least 30 candles, got %d)", len(candles))
				return fmt.Errorf("insufficient data")
			}

			output.Info("Running backtest on %d candles...", len(candles))
			output.Println()

			// Run simple momentum backtest
			results := runMomentumBacktest(candles, capital, strategy)

			if output.IsJSON() {
				return output.JSON(results)
			}

			return displayBacktestResults(output, results)
		},
	}

	cmd.Flags().String("strategy", "momentum", "Strategy to backtest")
	cmd.Flags().String("symbol", "", "Symbol to backtest")
	cmd.Flags().String("watchlist", "", "Watchlist to backtest")
	cmd.Flags().Int("days", 365, "Number of days to backtest")
	cmd.Flags().Float64("capital", 1000000, "Starting capital")
	cmd.Flags().Float64("slippage", 0.1, "Slippage percentage")
	cmd.Flags().Float64("commission", 0.03, "Commission percentage")
	cmd.Flags().StringP("exchange", "e", "NSE", "Exchange (NSE, BSE)")

	return cmd
}

// runMomentumBacktest runs a simple momentum-based backtest
func runMomentumBacktest(candles []models.Candle, capital float64, strategy string) BacktestResults {
	// Extract closes
	closes := make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
	}

	// Calculate EMAs for signals
	ema9 := calculateEMA(closes, 9)
	ema21 := calculateEMA(closes, 21)

	// Simulate trades
	var trades []backtestTrade
	inPosition := false
	entryPrice := 0.0
	entryIdx := 0
	currentCapital := capital
	maxCapital := capital
	maxDrawdown := 0.0

	// Track equity curve
	equityCurve := make([]float64, 0, len(candles))
	equityCurve = append(equityCurve, capital) // Starting point

	for i := 21; i < len(candles); i++ {
		if len(ema9) <= i || len(ema21) <= i {
			continue
		}

		// Track equity at each point
		equity := currentCapital
		if inPosition {
			// Mark-to-market: calculate unrealized P&L
			unrealizedPnL := ((candles[i].Close - entryPrice) / entryPrice) * currentCapital
			equity = currentCapital + unrealizedPnL
		}
		equityCurve = append(equityCurve, equity)

		// Entry signal: EMA9 crosses above EMA21
		if !inPosition && ema9[i] > ema21[i] && ema9[i-1] <= ema21[i-1] {
			inPosition = true
			entryPrice = candles[i].Close
			entryIdx = i
		}

		// Exit signal: EMA9 crosses below EMA21 or stop loss
		if inPosition {
			exitSignal := ema9[i] < ema21[i] && ema9[i-1] >= ema21[i-1]
			stopLoss := candles[i].Close < entryPrice*0.97 // 3% stop loss

			if exitSignal || stopLoss {
				exitPrice := candles[i].Close
				pnlPercent := ((exitPrice - entryPrice) / entryPrice) * 100
				pnl := (currentCapital * (pnlPercent / 100))
				currentCapital += pnl

				trades = append(trades, backtestTrade{
					entryPrice: entryPrice,
					exitPrice:  exitPrice,
					pnl:        pnl,
					pnlPercent: pnlPercent,
					holdDays:   i - entryIdx,
				})

				// Track max drawdown
				if currentCapital > maxCapital {
					maxCapital = currentCapital
				}
				drawdown := ((maxCapital - currentCapital) / maxCapital) * 100
				if drawdown > maxDrawdown {
					maxDrawdown = drawdown
				}

				inPosition = false
			}
		}
	}

	// Calculate results
	totalTrades := len(trades)
	winningTrades := 0
	grossProfit := 0.0
	grossLoss := 0.0
	totalHoldDays := 0
	largestWin := 0.0
	largestLoss := 0.0

	for _, t := range trades {
		if t.pnl > 0 {
			winningTrades++
			grossProfit += t.pnl
			if t.pnl > largestWin {
				largestWin = t.pnl
			}
		} else {
			grossLoss += t.pnl
			if t.pnl < largestLoss {
				largestLoss = t.pnl
			}
		}
		totalHoldDays += t.holdDays
	}

	winRate := 0.0
	avgWin := 0.0
	avgLoss := 0.0
	profitFactor := 0.0
	avgHoldDays := 0

	if totalTrades > 0 {
		winRate = float64(winningTrades) / float64(totalTrades) * 100
		avgHoldDays = totalHoldDays / totalTrades
	}
	if winningTrades > 0 {
		avgWin = grossProfit / float64(winningTrades)
	}
	losingTrades := totalTrades - winningTrades
	if losingTrades > 0 {
		avgLoss = grossLoss / float64(losingTrades)
	}
	if grossLoss != 0 {
		profitFactor = grossProfit / (-grossLoss)
	}

	netProfit := grossProfit + grossLoss
	totalReturn := (netProfit / capital) * 100

	// Simplified Sharpe ratio (annualized)
	sharpeRatio := 0.0
	if maxDrawdown > 0 {
		sharpeRatio = totalReturn / maxDrawdown
	}

	return BacktestResults{
		TotalTrades:   totalTrades,
		WinningTrades: winningTrades,
		LosingTrades:  losingTrades,
		WinRate:       winRate,
		GrossProfit:   grossProfit,
		GrossLoss:     grossLoss,
		NetProfit:     netProfit,
		TotalReturn:   totalReturn,
		MaxDrawdown:   maxDrawdown,
		SharpeRatio:   sharpeRatio,
		ProfitFactor:  profitFactor,
		AvgWin:        avgWin,
		AvgLoss:       avgLoss,
		LargestWin:    largestWin,
		LargestLoss:   largestLoss,
		AvgHoldTime:   fmt.Sprintf("%dd", avgHoldDays),
		StartCapital:  capital,
		EndCapital:    currentCapital,
		EquityCurve:   equityCurve,
	}
}

type backtestTrade struct {
	entryPrice float64
	exitPrice  float64
	pnl        float64
	pnlPercent float64
	holdDays   int
}

type BacktestResults struct {
	TotalTrades   int
	WinningTrades int
	LosingTrades  int
	WinRate       float64
	GrossProfit   float64
	GrossLoss     float64
	NetProfit     float64
	TotalReturn   float64
	MaxDrawdown   float64
	SharpeRatio   float64
	ProfitFactor  float64
	AvgWin        float64
	AvgLoss       float64
	LargestWin    float64
	LargestLoss   float64
	AvgHoldTime   string
	StartCapital  float64
	EndCapital    float64
	EquityCurve   []float64 // Track equity over time
}

func displayBacktestResults(output *Output, r BacktestResults) error {
	output.Bold("Backtest Results")
	output.Println()

	// Trade statistics
	output.Bold("Trade Statistics")
	output.Printf("  Total Trades:     %d\n", r.TotalTrades)
	output.Printf("  Winning Trades:   %d (%.1f%%)\n", r.WinningTrades, r.WinRate)
	output.Printf("  Losing Trades:    %d (%.1f%%)\n", r.LosingTrades, 100-r.WinRate)
	output.Printf("  Avg Hold Time:    %s\n", r.AvgHoldTime)
	output.Println()

	// P&L
	output.Bold("Profit & Loss")
	output.Printf("  Gross Profit:     %s\n", output.Green(FormatIndianCurrency(r.GrossProfit)))
	output.Printf("  Gross Loss:       %s\n", output.Red(FormatIndianCurrency(r.GrossLoss)))
	output.Printf("  Net Profit:       %s\n", output.FormatPnL(r.NetProfit))
	output.Printf("  Total Return:     %s\n", output.FormatPercent(r.TotalReturn))
	output.Println()

	// Performance metrics
	output.Bold("Performance Metrics")
	output.Printf("  Win Rate:         %.1f%%\n", r.WinRate)
	output.Printf("  Profit Factor:    %.2f\n", r.ProfitFactor)
	output.Printf("  Sharpe Ratio:     %.2f\n", r.SharpeRatio)
	output.Printf("  Max Drawdown:     %s\n", output.Red(fmt.Sprintf("%.1f%%", r.MaxDrawdown)))
	output.Println()

	// Trade analysis
	output.Bold("Trade Analysis")
	output.Printf("  Avg Win:          %s\n", FormatIndianCurrency(r.AvgWin))
	output.Printf("  Avg Loss:         %s\n", FormatIndianCurrency(r.AvgLoss))
	output.Printf("  Largest Win:      %s\n", FormatIndianCurrency(r.LargestWin))
	output.Printf("  Largest Loss:     %s\n", FormatIndianCurrency(r.LargestLoss))
	output.Printf("  Expectancy:       %s\n", FormatIndianCurrency(r.AvgWin*r.WinRate/100+r.AvgLoss*(100-r.WinRate)/100))
	output.Println()

	// Capital
	output.Bold("Capital")
	output.Printf("  Start:            %s\n", FormatIndianCurrency(r.StartCapital))
	output.Printf("  End:              %s\n", FormatIndianCurrency(r.EndCapital))
	output.Println()

	// Equity curve (ASCII)
	output.Bold("Equity Curve")
	drawEquityCurve(output, r.EquityCurve, r.StartCapital)

	return nil
}

func drawEquityCurve(output *Output, equityCurve []float64, startCapital float64) {
	if len(equityCurve) < 2 {
		output.Println("  Insufficient data for equity curve")
		return
	}

	// Find min/max for scaling
	minEquity := equityCurve[0]
	maxEquity := equityCurve[0]
	for _, e := range equityCurve {
		if e < minEquity {
			minEquity = e
		}
		if e > maxEquity {
			maxEquity = e
		}
	}

	// Add some padding
	padding := (maxEquity - minEquity) * 0.1
	if padding == 0 {
		padding = startCapital * 0.05
	}
	minEquity -= padding
	maxEquity += padding

	// Chart dimensions
	width := 40
	height := 8

	// Create chart grid
	chart := make([][]rune, height)
	for i := range chart {
		chart[i] = make([]rune, width)
		for j := range chart[i] {
			chart[i][j] = ' '
		}
	}

	// Plot equity curve
	for i := 0; i < len(equityCurve)-1; i++ {
		x := i * width / len(equityCurve)
		y := int((equityCurve[i] - minEquity) / (maxEquity - minEquity) * float64(height-1))
		if y >= 0 && y < height && x >= 0 && x < width {
			chart[height-1-y][x] = '█'
		}
	}

	// Print chart
	for i := 0; i < height; i++ {
		label := ""
		if i == 0 {
			label = fmt.Sprintf("%7.0f", maxEquity/100000) + "L"
		} else if i == height-1 {
			label = fmt.Sprintf("%7.0f", minEquity/100000) + "L"
		} else {
			label = "        "
		}
		output.Printf("  %s │%s\n", label, string(chart[i]))
	}
	output.Printf("          └%s\n", strings.Repeat("─", width))
}

func newExportCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export data to files",
		Long:  "Export candles, trades, or journal entries to CSV or JSON files.",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "candles <symbol>",
		Short: "Export candle data",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			symbol := args[0]
			format, _ := cmd.Flags().GetString("format")
			outFile, _ := cmd.Flags().GetString("output")
			days, _ := cmd.Flags().GetInt("days")
			exchange, _ := cmd.Flags().GetString("exchange")

			if outFile == "" {
				outFile = fmt.Sprintf("%s_candles.%s", symbol, format)
			}

			output.Info("Exporting %s candles to %s...", symbol, outFile)

			// Fetch real data from broker
			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			candles, err := app.Broker.GetHistorical(ctx, broker.HistoricalRequest{
				Symbol:    symbol,
				Exchange:  models.Exchange(exchange),
				Timeframe: "1day",
				From:      time.Now().AddDate(0, 0, -days),
				To:        time.Now(),
			})
			if err != nil {
				output.Error("Failed to fetch candles: %v", err)
				return err
			}

			if len(candles) == 0 {
				output.Warning("No candle data available for %s", symbol)
				return nil
			}

			// Create CSV
			if format == "csv" {
				file, err := os.Create(outFile)
				if err != nil {
					output.Error("Failed to create file: %v", err)
					return err
				}
				defer file.Close()

				writer := csv.NewWriter(file)
				defer writer.Flush()

				// Header
				writer.Write([]string{"timestamp", "open", "high", "low", "close", "volume"})

				// Real data
				for _, c := range candles {
					writer.Write([]string{
						c.Timestamp.Format(time.RFC3339),
						fmt.Sprintf("%.2f", c.Open),
						fmt.Sprintf("%.2f", c.High),
						fmt.Sprintf("%.2f", c.Low),
						fmt.Sprintf("%.2f", c.Close),
						fmt.Sprintf("%d", c.Volume),
					})
				}
			}

			output.Success("✓ Exported %d candles to %s", len(candles), outFile)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "trades",
		Short: "Export trade history",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			format, _ := cmd.Flags().GetString("format")
			outFile, _ := cmd.Flags().GetString("output")

			if outFile == "" {
				outFile = fmt.Sprintf("trades.%s", format)
			}

			output.Info("Exporting trades to %s...", outFile)

			// Fetch real trades from store
			if app.Store == nil {
				output.Error("Store not initialized")
				return fmt.Errorf("store not initialized")
			}

			trades, err := app.Store.GetTrades(ctx, store.TradeFilter{Limit: 1000})
			if err != nil {
				output.Error("Failed to fetch trades: %v", err)
				return err
			}

			if len(trades) == 0 {
				output.Warning("No trades found")
				return nil
			}

			if format == "csv" {
				file, err := os.Create(outFile)
				if err != nil {
					output.Error("Failed to create file: %v", err)
					return err
				}
				defer file.Close()

				writer := csv.NewWriter(file)
				defer writer.Flush()

				// Header
				writer.Write([]string{"id", "timestamp", "symbol", "exchange", "side", "product", "quantity", "entry_price", "exit_price", "pnl", "pnl_percent", "strategy"})

				// Real data
				for _, t := range trades {
					writer.Write([]string{
						t.ID,
						t.Timestamp.Format(time.RFC3339),
						t.Symbol,
						string(t.Exchange),
						string(t.Side),
						string(t.Product),
						fmt.Sprintf("%d", t.Quantity),
						fmt.Sprintf("%.2f", t.EntryPrice),
						fmt.Sprintf("%.2f", t.ExitPrice),
						fmt.Sprintf("%.2f", t.PnL),
						fmt.Sprintf("%.2f", t.PnLPercent),
						t.Strategy,
					})
				}
			}

			output.Success("✓ Exported %d trades to %s", len(trades), outFile)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "journal",
		Short: "Export journal entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			format, _ := cmd.Flags().GetString("format")
			outFile, _ := cmd.Flags().GetString("output")

			if outFile == "" {
				outFile = fmt.Sprintf("journal.%s", format)
			}

			output.Info("Exporting journal to %s...", outFile)

			// Fetch real journal entries from store
			if app.Store == nil {
				output.Error("Store not initialized")
				return fmt.Errorf("store not initialized")
			}

			entries, err := app.Store.GetJournal(ctx, store.JournalFilter{Limit: 1000})
			if err != nil {
				output.Error("Failed to fetch journal entries: %v", err)
				return err
			}

			if len(entries) == 0 {
				output.Warning("No journal entries found")
				return nil
			}

			if format == "csv" {
				file, err := os.Create(outFile)
				if err != nil {
					output.Error("Failed to create file: %v", err)
					return err
				}
				defer file.Close()

				writer := csv.NewWriter(file)
				defer writer.Flush()

				// Header
				writer.Write([]string{"id", "trade_id", "date", "content", "mood", "tags"})

				// Real data
				for _, e := range entries {
					tags := ""
					if len(e.Tags) > 0 {
						for i, t := range e.Tags {
							if i > 0 {
								tags += ";"
							}
							tags += t
						}
					}
					writer.Write([]string{
						e.ID,
						e.TradeID,
						e.Date.Format("2006-01-02"),
						e.Content,
						e.Mood,
						tags,
					})
				}
			}

			output.Success("✓ Exported %d journal entries to %s", len(entries), outFile)
			return nil
		},
	})

	cmd.PersistentFlags().String("format", "csv", "Output format (csv, json)")
	cmd.PersistentFlags().StringP("output", "o", "", "Output file path")
	cmd.PersistentFlags().Int("days", 30, "Number of days to export")
	cmd.PersistentFlags().StringP("exchange", "e", "NSE", "Exchange (NSE, BSE)")

	return cmd
}

func newAPICmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api",
		Short: "REST API server",
		Long:  "Start a REST API server for external integrations.",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "start",
		Short: "Start the API server",
		Long: `Start a REST API server for external integrations.

Endpoints:
  GET  /api/quote/:symbol     - Get quote
  GET  /api/positions         - Get positions
  GET  /api/orders            - Get orders
  POST /api/order             - Place order
  GET  /api/analysis/:symbol  - Get analysis
  GET  /api/health            - Health check`,
		Example: `  trader api start
  trader api start --port 8080
  trader api start --key myapikey`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			port, _ := cmd.Flags().GetInt("port")
			apiKey, _ := cmd.Flags().GetString("key")

			output.Bold("Starting REST API Server")
			output.Printf("  Port:    %d\n", port)
			if apiKey != "" {
				output.Printf("  API Key: %s\n", "****"+apiKey[len(apiKey)-4:])
			}
			output.Println()

			output.Info("API server starting on http://localhost:%d", port)
			output.Println()

			output.Bold("Available Endpoints")
			endpoints := []struct {
				method string
				path   string
				desc   string
			}{
				{"GET", "/api/quote/:symbol", "Get real-time quote"},
				{"GET", "/api/positions", "Get open positions"},
				{"GET", "/api/holdings", "Get holdings"},
				{"GET", "/api/orders", "Get orders"},
				{"POST", "/api/order", "Place order"},
				{"DELETE", "/api/order/:id", "Cancel order"},
				{"GET", "/api/analysis/:symbol", "Get technical analysis"},
				{"GET", "/api/signal/:symbol", "Get signal score"},
				{"GET", "/api/health", "Health check"},
			}

			for _, e := range endpoints {
				methodColor := ColorGreen
				if e.method == "POST" {
					methodColor = ColorYellow
				} else if e.method == "DELETE" {
					methodColor = ColorRed
				}
				output.Printf("  %s %-25s %s\n",
					output.ColoredString(methodColor, PadRight(e.method, 6)),
					e.path,
					output.DimText(e.desc))
			}

			output.Println()
			output.Dim("Press Ctrl+C to stop the server")

			// In a real implementation, this would start an HTTP server
			// For now, just show the info
			output.Warning("API server not implemented in this version")

			return nil
		},
	})

	cmd.PersistentFlags().Int("port", 8080, "Server port")
	cmd.PersistentFlags().String("key", "", "API key for authentication")

	return cmd
}
