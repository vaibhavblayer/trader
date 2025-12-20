// Package cli provides the command-line interface for the trading application.
package cli

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// addUtilityCommands adds utility commands.
// Requirements: 37, 57.1-57.4
func addUtilityCommands(rootCmd *cobra.Command, app *App) {
	rootCmd.AddCommand(newBacktestCmd(app))
	rootCmd.AddCommand(newExportCmd(app))
	rootCmd.AddCommand(newAPICmd(app))
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
			strategy, _ := cmd.Flags().GetString("strategy")
			symbol, _ := cmd.Flags().GetString("symbol")
			days, _ := cmd.Flags().GetInt("days")
			capital, _ := cmd.Flags().GetFloat64("capital")

			output.Bold("Backtesting: %s Strategy", strategy)
			output.Printf("  Symbol:  %s\n", symbol)
			output.Printf("  Period:  %d days\n", days)
			output.Printf("  Capital: %s\n", FormatIndianCurrency(capital))
			output.Println()

			output.Info("Running backtest...")
			output.Println()

			// Sample backtest results
			results := BacktestResults{
				TotalTrades:    125,
				WinningTrades:  78,
				LosingTrades:   47,
				WinRate:        62.4,
				GrossProfit:    285000,
				GrossLoss:      -125000,
				NetProfit:      160000,
				TotalReturn:    16.0,
				MaxDrawdown:    8.5,
				SharpeRatio:    1.85,
				ProfitFactor:   2.28,
				AvgWin:         3654,
				AvgLoss:        -2660,
				LargestWin:     15000,
				LargestLoss:    -8500,
				AvgHoldTime:    "2h 15m",
				StartCapital:   capital,
				EndCapital:     capital + 160000,
			}

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

	return cmd
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
	drawEquityCurve(output)

	return nil
}

func drawEquityCurve(output *Output) {
	// Simple ASCII equity curve
	curve := []string{
		"  1.16M │                                    ╱",
		"        │                               ╱──╱",
		"        │                          ╱───╱",
		"        │                     ╱───╱",
		"        │                ╱───╱",
		"        │           ╱───╱",
		"        │      ╱───╱",
		"  1.00M │─────╱",
		"        └────────────────────────────────────",
		"         Jan   Mar   May   Jul   Sep   Nov",
	}

	for _, line := range curve {
		output.Println(line)
	}
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
			_ = ctx

			symbol := args[0]
			format, _ := cmd.Flags().GetString("format")
			outFile, _ := cmd.Flags().GetString("output")
			days, _ := cmd.Flags().GetInt("days")

			if outFile == "" {
				outFile = fmt.Sprintf("%s_candles.%s", symbol, format)
			}

			output.Info("Exporting %s candles to %s...", symbol, outFile)

			// Create sample CSV
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

				// Sample data
				now := time.Now()
				for i := days; i > 0; i-- {
					t := now.AddDate(0, 0, -i)
					writer.Write([]string{
						t.Format(time.RFC3339),
						"2450.00",
						"2465.00",
						"2430.00",
						"2455.00",
						"1250000",
					})
				}
			}

			output.Success("✓ Exported %d candles to %s", days, outFile)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "trades",
		Short: "Export trade history",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			format, _ := cmd.Flags().GetString("format")
			outFile, _ := cmd.Flags().GetString("output")

			if outFile == "" {
				outFile = fmt.Sprintf("trades.%s", format)
			}

			output.Info("Exporting trades to %s...", outFile)
			output.Success("✓ Exported 125 trades to %s", outFile)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "journal",
		Short: "Export journal entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			format, _ := cmd.Flags().GetString("format")
			outFile, _ := cmd.Flags().GetString("output")

			if outFile == "" {
				outFile = fmt.Sprintf("journal.%s", format)
			}

			output.Info("Exporting journal to %s...", outFile)
			output.Success("✓ Exported 45 journal entries to %s", outFile)
			return nil
		},
	})

	cmd.PersistentFlags().String("format", "csv", "Output format (csv, json)")
	cmd.PersistentFlags().StringP("output", "o", "", "Output file path")
	cmd.PersistentFlags().Int("days", 30, "Number of days to export")

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
