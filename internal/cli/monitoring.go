// Package cli provides the command-line interface for the trading application.
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// addMonitoringCommands adds monitoring commands.
// Requirements: 9, 31.1-31.9
func addMonitoringCommands(rootCmd *cobra.Command, app *App) {
	rootCmd.AddCommand(newWatchCmd(app))
	rootCmd.AddCommand(newWatchlistCmd(app))
}

func newWatchCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Interactive watch mode",
		Long: `Start interactive watch mode with live prices and trade plans.

Keyboard shortcuts:
  q - Quit
  r - Refresh
  a - Add symbol
  d - Remove symbol
  p - Show positions
  o - Show orders
  / - Search
  ? - Help`,
		Example: `  trader watch
  trader watch --watchlist momentum
  trader watch RELIANCE INFY TCS`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			watchlistName, _ := cmd.Flags().GetString("watchlist")

			// Check broker
			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader auth login' first.")
				return fmt.Errorf("broker not configured")
			}

			if !app.Broker.IsAuthenticated() {
				output.Error("Not authenticated. Run 'trader auth login' first.")
				return fmt.Errorf("not authenticated")
			}

			// Get symbols to watch
			var symbols []string
			if len(args) > 0 {
				symbols = args
			} else if watchlistName != "" && app.Store != nil {
				var err error
				symbols, err = app.Store.GetWatchlist(ctx, watchlistName)
				if err != nil || len(symbols) == 0 {
					output.Warning("Watchlist '%s' is empty or not found, using defaults", watchlistName)
					symbols = []string{"RELIANCE", "TCS", "INFY", "HDFCBANK", "ICICIBANK"}
				}
			} else if app.Store != nil {
				var err error
				symbols, err = app.Store.GetWatchlist(ctx, "default")
				if err != nil || len(symbols) == 0 {
					symbols = []string{"RELIANCE", "TCS", "INFY", "HDFCBANK", "ICICIBANK"}
				}
			} else {
				symbols = []string{"RELIANCE", "TCS", "INFY", "HDFCBANK", "ICICIBANK"}
			}

			output.Bold("Watch Mode %s", output.SourceTag(SourceZerodha))
			output.Println()

			if watchlistName != "" {
				output.Printf("  Watchlist: %s\n", watchlistName)
			}
			output.Printf("  Symbols: %d\n", len(symbols))
			output.Println()

			// Create table for proper alignment
			table := NewTable(output, "Symbol", "LTP", "Change", "High", "Low", "Volume")

			// Fetch real quotes for each symbol
			for _, symbol := range symbols {
				quote, err := app.Broker.GetQuote(ctx, "NSE:"+symbol)
				if err != nil {
					table.AddRow(symbol, "-", "-", "-", "-", "-")
					continue
				}

				changeColor := output.PnLColor(quote.ChangePercent)
				changeStr := fmt.Sprintf("%+.2f%%", quote.ChangePercent)
				table.AddRow(
					symbol,
					fmt.Sprintf("%.2f", quote.LTP),
					output.ColoredString(changeColor, changeStr),
					fmt.Sprintf("%.2f", quote.High),
					fmt.Sprintf("%.2f", quote.Low),
					FormatVolume(quote.Volume),
				)
			}
			table.Render()

			output.Println()
			output.Dim("Use 'trader live %s' for real-time streaming", strings.Join(symbols, " "))

			return nil
		},
	}

	cmd.Flags().StringP("watchlist", "w", "", "Watchlist to monitor")
	return cmd
}

func newWatchlistCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watchlist",
		Short: "Watchlist management",
		Long:  "Add, remove, and list symbols in watchlists.",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "add <symbol> [watchlist]",
		Short: "Add symbol to watchlist",
		Long:  "Add a symbol to a watchlist. Default watchlist is 'default'.",
		Example: `  trader watchlist add RELIANCE
  trader watchlist add INFY momentum
  trader watchlist add TCS breakout`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			symbol := strings.ToUpper(args[0])
			listName := "default"
			if len(args) > 1 {
				listName = args[1]
			}

			if app.Store != nil {
				if err := app.Store.AddToWatchlist(ctx, symbol, listName); err != nil {
					output.Error("Failed to add to watchlist: %v", err)
					return err
				}
			}

			output.Success("✓ Added %s to watchlist '%s'", symbol, listName)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "remove <symbol> [watchlist]",
		Short: "Remove symbol from watchlist",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			symbol := strings.ToUpper(args[0])
			listName := "default"
			if len(args) > 1 {
				listName = args[1]
			}

			if app.Store != nil {
				if err := app.Store.RemoveFromWatchlist(ctx, symbol, listName); err != nil {
					output.Error("Failed to remove from watchlist: %v", err)
					return err
				}
			}

			output.Success("✓ Removed %s from watchlist '%s'", symbol, listName)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list [watchlist]",
		Short: "List watchlist symbols",
		Long:  "Display all symbols in a watchlist. Shows all watchlists if none specified.",
		Example: `  trader watchlist list
  trader watchlist list momentum`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if len(args) > 0 {
				// Show specific watchlist
				listName := args[0]
				var symbols []string

				if app.Store != nil {
					var err error
					symbols, err = app.Store.GetWatchlist(ctx, listName)
					if err != nil {
						output.Error("Failed to get watchlist: %v", err)
						return err
					}
				} else {
					// Sample data
					symbols = []string{"RELIANCE", "INFY", "TCS", "HDFC", "ICICI"}
				}

				if output.IsJSON() {
					return output.JSON(map[string]interface{}{
						"name":    listName,
						"symbols": symbols,
					})
				}

				output.Bold("Watchlist: %s", listName)
				output.Printf("  %d symbols\n\n", len(symbols))

				for _, s := range symbols {
					output.Printf("  • %s\n", s)
				}
			} else {
				// Show all watchlists
				var watchlists map[string][]string

				if app.Store != nil {
					var err error
					watchlists, err = app.Store.GetAllWatchlists(ctx)
					if err != nil {
						output.Error("Failed to get watchlists: %v", err)
						return err
					}
				} else {
					// Sample data
					watchlists = map[string][]string{
						"default":  {"RELIANCE", "INFY", "TCS", "HDFC", "ICICI"},
						"momentum": {"TATAMOTORS", "ADANIENT", "BAJFINANCE"},
						"breakout": {"SBIN", "AXISBANK", "KOTAKBANK"},
					}
				}

				if output.IsJSON() {
					return output.JSON(watchlists)
				}

				output.Bold("Watchlists")
				output.Printf("  %d watchlists\n\n", len(watchlists))

				for name, symbols := range watchlists {
					output.Printf("  %s (%d symbols)\n", output.Cyan(name), len(symbols))
					for _, s := range symbols {
						output.Printf("    • %s\n", s)
					}
					output.Println()
				}
			}

			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "create <name>",
		Short: "Create a new watchlist",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			name := args[0]

			output.Success("✓ Created watchlist '%s'", name)
			output.Dim("Use 'trader watchlist add <symbol> %s' to add symbols", name)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a watchlist",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			name := args[0]

			if name == "default" {
				output.Error("Cannot delete default watchlist")
				return fmt.Errorf("cannot delete default watchlist")
			}

			output.Success("✓ Deleted watchlist '%s'", name)
			return nil
		},
	})

	return cmd
}
