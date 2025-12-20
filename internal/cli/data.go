// Package cli provides the command-line interface for the trading application.
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

// addMarketDataCommands adds market data commands.
// Requirements: 2, 3, 55
func addMarketDataCommands(rootCmd *cobra.Command, app *App) {
	rootCmd.AddCommand(newQuoteCmd(app))
	rootCmd.AddCommand(newDataCmd(app))
	rootCmd.AddCommand(newLiveCmd(app))
	rootCmd.AddCommand(newBreadthCmd(app))
}

func newQuoteCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quote <symbol>",
		Short: "Get real-time quote for a symbol",
		Long: `Fetch and display real-time market quote for a symbol.

The quote includes LTP, OHLC, volume, and change information.`,
		Example: `  trader quote RELIANCE
  trader quote INFY --exchange BSE
  trader quote NIFTY23DEC21000CE --exchange NFO`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			symbol := strings.ToUpper(args[0])
			exchange, _ := cmd.Flags().GetString("exchange")

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			// Format symbol with exchange if provided
			fullSymbol := symbol
			if exchange != "" {
				fullSymbol = exchange + ":" + symbol
			}

			quote, err := app.Broker.GetQuote(ctx, fullSymbol)
			if err != nil {
				output.Error("Failed to get quote: %v", err)
				return err
			}

			if output.IsJSON() {
				return output.JSON(quote)
			}

			return displayQuote(output, quote)
		},
	}

	cmd.Flags().StringP("exchange", "e", "NSE", "Exchange (NSE, BSE, NFO, CDS, MCX)")

	return cmd
}

func displayQuote(output *Output, quote *models.Quote) error {
	output.Bold("%s", quote.Symbol)
	output.Println()

	// LTP with change
	changeColor := output.PnLColor(quote.Change)
	ltp := fmt.Sprintf("%.2f", quote.LTP)
	change := FormatChange(quote.Change, quote.ChangePercent)

	output.Printf("  LTP: %s  %s\n", output.BoldText(ltp), output.ColoredString(changeColor, change))
	output.Println()

	// OHLC
	output.Printf("  Open:   %s\n", FormatPrice(quote.Open))
	output.Printf("  High:   %s\n", output.Green(FormatPrice(quote.High)))
	output.Printf("  Low:    %s\n", output.Red(FormatPrice(quote.Low)))
	output.Printf("  Close:  %s\n", FormatPrice(quote.Close))
	output.Println()

	// Volume
	output.Printf("  Volume: %s\n", FormatVolume(quote.Volume))
	output.Println()

	// Timestamp
	output.Dim("  Updated: %s", FormatDateTime(quote.Timestamp))

	return nil
}

func newDataCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "data <symbol>",
		Short: "Get historical OHLCV data",
		Long: `Fetch historical OHLCV (Open, High, Low, Close, Volume) data for a symbol.

Data is cached locally for faster subsequent access.`,
		Example: `  trader data RELIANCE
  trader data INFY --timeframe 15min --days 30
  trader data NIFTY50 --timeframe 1day --days 365`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			symbol := strings.ToUpper(args[0])
			exchange, _ := cmd.Flags().GetString("exchange")
			timeframe, _ := cmd.Flags().GetString("timeframe")
			days, _ := cmd.Flags().GetInt("days")
			limit, _ := cmd.Flags().GetInt("limit")

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			// Calculate date range
			to := time.Now()
			from := to.AddDate(0, 0, -days)

			req := broker.HistoricalRequest{
				Symbol:    symbol,
				Exchange:  models.Exchange(exchange),
				Timeframe: timeframe,
				From:      from,
				To:        to,
			}

			candles, err := app.Broker.GetHistorical(ctx, req)
			if err != nil {
				output.Error("Failed to get historical data: %v", err)
				return err
			}

			// Apply limit
			if limit > 0 && len(candles) > limit {
				candles = candles[len(candles)-limit:]
			}

			if output.IsJSON() {
				return output.JSON(map[string]interface{}{
					"symbol":    symbol,
					"exchange":  exchange,
					"timeframe": timeframe,
					"from":      from.Format(time.RFC3339),
					"to":        to.Format(time.RFC3339),
					"count":     len(candles),
					"candles":   candles,
				})
			}

			return displayCandles(output, symbol, timeframe, candles)
		},
	}

	cmd.Flags().StringP("exchange", "e", "NSE", "Exchange (NSE, BSE, NFO, CDS, MCX)")
	cmd.Flags().StringP("timeframe", "t", "1day", "Timeframe (1min, 5min, 15min, 30min, 1hour, 1day)")
	cmd.Flags().IntP("days", "d", 30, "Number of days of history")
	cmd.Flags().IntP("limit", "l", 0, "Limit number of candles to display (0 for all)")

	return cmd
}

func displayCandles(output *Output, symbol, timeframe string, candles []models.Candle) error {
	output.Bold("%s - %s", symbol, timeframe)
	output.Printf("  %d candles\n\n", len(candles))

	// Create table
	table := NewTable(output, "Date/Time", "Open", "High", "Low", "Close", "Volume", "Change")

	for i, c := range candles {
		var change string
		if i > 0 {
			pctChange := ((c.Close - candles[i-1].Close) / candles[i-1].Close) * 100
			change = output.ColoredString(output.PnLColor(pctChange), FormatPercent(pctChange))
		} else {
			change = "-"
		}

		dateStr := FormatDateTime(c.Timestamp)
		if timeframe == "1day" {
			dateStr = FormatDate(c.Timestamp)
		}

		table.AddRow(
			dateStr,
			FormatPrice(c.Open),
			output.Green(FormatPrice(c.High)),
			output.Red(FormatPrice(c.Low)),
			FormatPrice(c.Close),
			FormatVolume(c.Volume),
			change,
		)
	}

	table.Render()
	return nil
}

func newLiveCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "live <symbols...>",
		Short: "Stream live prices for symbols",
		Long: `Stream real-time price updates for one or more symbols.

Press Ctrl+C to stop streaming.`,
		Example: `  trader live RELIANCE
  trader live RELIANCE INFY TCS
  trader live NIFTY50 BANKNIFTY --mode full`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			symbols := make([]string, len(args))
			for i, s := range args {
				symbols[i] = strings.ToUpper(s)
			}

			mode, _ := cmd.Flags().GetString("mode")

			if app.Ticker == nil {
				output.Error("Ticker not configured. Run 'trader login' first.")
				return fmt.Errorf("ticker not configured")
			}

			output.Info("Streaming live prices for: %s", strings.Join(symbols, ", "))
			output.Dim("Press Ctrl+C to stop")
			output.Println()

			// Create header
			table := NewTable(output, "Symbol", "LTP", "Change", "Volume", "Bid", "Ask", "Time")
			table.Render()

			// Subscribe to symbols
			tickMode := broker.TickModeQuote
			if mode == "full" {
				tickMode = broker.TickModeFull
			}

			ctx := context.Background()
			if err := app.Ticker.Connect(ctx); err != nil {
				output.Error("Failed to connect: %v", err)
				return err
			}
			defer app.Ticker.Disconnect()

			if err := app.Ticker.Subscribe(symbols, tickMode); err != nil {
				output.Error("Failed to subscribe: %v", err)
				return err
			}

			// Handle ticks
			app.Ticker.OnTick(func(tick models.Tick) {
				change := ((tick.LTP - tick.Close) / tick.Close) * 100
				changeStr := output.ColoredString(output.PnLColor(change), FormatPercent(change))

				output.Printf("\r%-12s %10s %10s %10s %10s %10s %s",
					tick.Symbol,
					FormatPrice(tick.LTP),
					changeStr,
					FormatVolume(tick.Volume),
					FormatPrice(tick.BidPrice),
					FormatPrice(tick.AskPrice),
					FormatTime(tick.Timestamp),
				)
			})

			app.Ticker.OnError(func(err error) {
				output.Error("Ticker error: %v", err)
			})

			// Wait for interrupt
			select {}
		},
	}

	cmd.Flags().StringP("mode", "m", "quote", "Tick mode (quote, full)")

	return cmd
}

func newBreadthCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "breadth",
		Short: "Display market breadth indicators",
		Long: `Display market breadth indicators including:
- Advance/Decline ratio for NSE
- New highs vs new lows
- Sector-wise performance
- India VIX and trend
- FII/DII cash market data
- Put-Call Ratio (PCR) for NIFTY and BANKNIFTY`,
		Example: `  trader breadth
  trader breadth --detailed`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			detailed, _ := cmd.Flags().GetBool("detailed")

			// This would fetch real data from the broker
			// For now, display placeholder structure
			breadth := MarketBreadthData{
				Advances:       1250,
				Declines:       750,
				Unchanged:      100,
				NewHighs:       45,
				NewLows:        12,
				AdvDecRatio:    1.67,
				NiftyLevel:     19500.50,
				NiftyChange:    0.85,
				BankNiftyLevel: 44200.25,
				BankNiftyChange: 1.20,
				VIXLevel:       12.50,
				VIXChange:      -5.2,
				NiftyPCR:       0.95,
				BankNiftyPCR:   0.88,
				FIINetValue:    1250.50,
				DIINetValue:    -850.25,
			}

			if output.IsJSON() {
				return output.JSON(breadth)
			}

			return displayBreadth(output, breadth, detailed)
		},
	}

	cmd.Flags().Bool("detailed", false, "Show detailed sector breakdown")

	return cmd
}

// MarketBreadthData holds market breadth information.
type MarketBreadthData struct {
	Advances        int
	Declines        int
	Unchanged       int
	NewHighs        int
	NewLows         int
	AdvDecRatio     float64
	NiftyLevel      float64
	NiftyChange     float64
	BankNiftyLevel  float64
	BankNiftyChange float64
	VIXLevel        float64
	VIXChange       float64
	NiftyPCR        float64
	BankNiftyPCR    float64
	FIINetValue     float64
	DIINetValue     float64
}

func displayBreadth(output *Output, data MarketBreadthData, detailed bool) error {
	output.Bold("Market Breadth - NSE")
	output.Println()

	// Indices
	output.Printf("  NIFTY 50:    %s  %s\n",
		output.BoldText(FormatPrice(data.NiftyLevel)),
		output.ColoredString(output.PnLColor(data.NiftyChange), FormatPercent(data.NiftyChange)))
	output.Printf("  BANK NIFTY:  %s  %s\n",
		output.BoldText(FormatPrice(data.BankNiftyLevel)),
		output.ColoredString(output.PnLColor(data.BankNiftyChange), FormatPercent(data.BankNiftyChange)))
	output.Println()

	// Advance/Decline
	output.Bold("Advance/Decline")
	advBar := createBar(data.Advances, data.Advances+data.Declines, 30)
	output.Printf("  Advances:  %s %d\n", output.Green(advBar), data.Advances)
	decBar := createBar(data.Declines, data.Advances+data.Declines, 30)
	output.Printf("  Declines:  %s %d\n", output.Red(decBar), data.Declines)
	output.Printf("  Unchanged: %d\n", data.Unchanged)
	output.Printf("  A/D Ratio: %.2f\n", data.AdvDecRatio)
	output.Println()

	// New Highs/Lows
	output.Bold("New Highs/Lows")
	output.Printf("  New Highs: %s %d\n", output.Green("▲"), data.NewHighs)
	output.Printf("  New Lows:  %s %d\n", output.Red("▼"), data.NewLows)
	output.Println()

	// VIX
	output.Bold("India VIX")
	vixTrend := "→"
	if data.VIXChange > 0 {
		vixTrend = "↑"
	} else if data.VIXChange < 0 {
		vixTrend = "↓"
	}
	output.Printf("  Level: %.2f %s %s\n",
		data.VIXLevel,
		vixTrend,
		output.ColoredString(output.PnLColor(-data.VIXChange), FormatPercent(data.VIXChange)))
	output.Println()

	// PCR
	output.Bold("Put-Call Ratio")
	output.Printf("  NIFTY PCR:     %.2f\n", data.NiftyPCR)
	output.Printf("  BANKNIFTY PCR: %.2f\n", data.BankNiftyPCR)
	output.Println()

	// FII/DII
	output.Bold("FII/DII Activity (Cr)")
	output.Printf("  FII Net: %s\n", output.ColoredString(output.PnLColor(data.FIINetValue), FormatCompact(data.FIINetValue*10000000)))
	output.Printf("  DII Net: %s\n", output.ColoredString(output.PnLColor(data.DIINetValue), FormatCompact(data.DIINetValue*10000000)))

	if detailed {
		output.Println()
		output.Bold("Sector Performance")
		sectors := []struct {
			name   string
			change float64
		}{
			{"IT", 1.5},
			{"Banking", 0.8},
			{"Pharma", -0.3},
			{"Auto", 0.5},
			{"FMCG", -0.1},
			{"Metal", 2.1},
			{"Realty", -1.2},
			{"Energy", 0.9},
		}

		for _, s := range sectors {
			bar := createHeatBar(s.change, 20)
			output.Printf("  %-10s %s %s\n", s.name, bar, output.ColoredString(output.PnLColor(s.change), FormatPercent(s.change)))
		}
	}

	return nil
}

func createBar(value, total, width int) string {
	if total == 0 {
		return strings.Repeat("░", width)
	}
	filled := (value * width) / total
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func createHeatBar(change float64, width int) string {
	// Normalize change to -3% to +3% range
	normalized := change / 3.0
	if normalized > 1 {
		normalized = 1
	} else if normalized < -1 {
		normalized = -1
	}

	mid := width / 2
	if change >= 0 {
		filled := int(normalized * float64(mid))
		return strings.Repeat("░", mid) + strings.Repeat("█", filled) + strings.Repeat("░", mid-filled)
	}
	filled := int(-normalized * float64(mid))
	return strings.Repeat("░", mid-filled) + strings.Repeat("█", filled) + strings.Repeat("░", mid)
}
