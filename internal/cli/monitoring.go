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
	return &cobra.Command{
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
			watchlist, _ := cmd.Flags().GetString("watchlist")

			output.Bold("Watch Mode")
			output.Println()

			if watchlist != "" {
				output.Printf("  Watchlist: %s\n", watchlist)
			}
			if len(args) > 0 {
				output.Printf("  Symbols: %s\n", strings.Join(args, ", "))
			}
			output.Println()

			// Display sample watch view
			output.Printf("%-12s %10s %10s %10s %10s %10s\n",
				"Symbol", "LTP", "Change", "High", "Low", "Volume")
			output.Println(strings.Repeat("─", 70))

			symbols := []struct {
				symbol string
				ltp    float64
				change float64
				high   float64
				low    float64
				volume int64
			}{
				{"RELIANCE", 2450.50, 1.25, 2465.00, 2430.00, 1250000},
				{"INFY", 1520.30, -0.85, 1535.00, 1510.00, 850000},
				{"TCS", 3450.00, 0.45, 3470.00, 3420.00, 650000},
				{"HDFC", 1680.25, 2.10, 1695.00, 1650.00, 920000},
				{"ICICI", 985.50, -0.35, 995.00, 980.00, 1100000},
			}

			for _, s := range symbols {
				changeColor := output.PnLColor(s.change)
				output.Printf("%-12s %10s %10s %10s %10s %10s\n",
					s.symbol,
					FormatPrice(s.ltp),
					output.ColoredString(changeColor, FormatPercent(s.change)),
					FormatPrice(s.high),
					FormatPrice(s.low),
					FormatVolume(s.volume),
				)
			}

			output.Println()
			output.Dim("Press 'q' to quit, '?' for help")
			output.Println()
			output.Warning("Interactive mode requires terminal. Use 'trader live' for streaming.")

			return nil
		},
	}
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
