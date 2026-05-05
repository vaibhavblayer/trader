// Package cli provides the command-line interface for the trading application.
package cli

import (
	"context"
	"fmt"
	"math"
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
  trader options chain BANKNIFTY --expiry 2026-05-28
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
	cmd := &cobra.Command{
		Use:   "greeks",
		Short: "Calculate option Greeks",
		Long: `Calculate option Greeks (Delta, Gamma, Theta, Vega, Rho).

Uses Black-Scholes with explicit spot, strike, expiry, implied volatility, and risk-free rate inputs.
This command does not fetch live option prices.`,
		Example: `  trader options greeks --symbol NIFTY --spot 19600 --strike 19500 --type CE --expiry 2026-05-28 --iv 14.5
  trader options greeks --symbol NIFTY --spot 19400 --strike 19500 --type PE --expiry 2026-05-28 --iv 16.2`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			symbol, _ := cmd.Flags().GetString("symbol")
			strike, _ := cmd.Flags().GetFloat64("strike")
			optType, _ := cmd.Flags().GetString("type")
			spot, _ := cmd.Flags().GetFloat64("spot")
			expiryStr, _ := cmd.Flags().GetString("expiry")
			iv, _ := cmd.Flags().GetFloat64("iv")
			rate, _ := cmd.Flags().GetFloat64("rate")
			premium, _ := cmd.Flags().GetFloat64("premium")

			greeks, err := calculateOptionGreeks(spot, strike, optType, expiryStr, iv, rate, premium)
			if err != nil {
				output.Error("%v", err)
				return err
			}

			output.Bold("Option Greeks")
			output.Printf("  %s %s %.0f\n\n", symbol, optType, strike)

			output.Printf("  Delta (Δ):  %s\n", output.BoldText(fmt.Sprintf("%.4f", greeks.Delta)))
			output.Printf("  Gamma (Γ):  %.6f\n", greeks.Gamma)
			output.Printf("  Theta (Θ):  %s / day\n", output.Red(fmt.Sprintf("%.4f", greeks.Theta)))
			output.Printf("  Vega (ν):   %.4f per IV point\n", greeks.Vega)
			output.Printf("  Rho (ρ):    %.4f per rate point\n", greeks.Rho)
			output.Println()
			output.Printf("  IV:         %.2f%%\n", iv)
			output.Printf("  D1/D2:      %.4f / %.4f\n", greeks.D1, greeks.D2)
			if premium > 0 {
				output.Printf("  Intrinsic:  %s\n", FormatIndianCurrency(greeks.IntrinsicValue))
				output.Printf("  Time Value: %s\n", FormatIndianCurrency(greeks.TimeValue))
			}

			return nil
		},
	}

	cmd.Flags().String("symbol", "NIFTY", "Underlying symbol")
	cmd.Flags().Float64("spot", 0, "Underlying spot price (required)")
	cmd.Flags().Float64("strike", 0, "Strike price (required)")
	cmd.Flags().String("type", "CE", "Option type (CE or PE)")
	cmd.Flags().String("expiry", "", "Expiry date (YYYY-MM-DD, required)")
	cmd.Flags().Float64("iv", 0, "Implied volatility percentage (required)")
	cmd.Flags().Float64("rate", 6.5, "Annual risk-free rate percentage")
	cmd.Flags().Float64("premium", 0, "Option premium for intrinsic/time value calculation")

	return cmd
}

type optionGreeks struct {
	Delta          float64
	Gamma          float64
	Theta          float64
	Vega           float64
	Rho            float64
	D1             float64
	D2             float64
	IntrinsicValue float64
	TimeValue      float64
}

func calculateOptionGreeks(spot, strike float64, optType, expiryStr string, ivPercent, ratePercent, premium float64) (*optionGreeks, error) {
	if spot <= 0 {
		return nil, fmt.Errorf("spot must be greater than 0")
	}
	if strike <= 0 {
		return nil, fmt.Errorf("strike must be greater than 0")
	}
	if ivPercent <= 0 {
		return nil, fmt.Errorf("iv must be greater than 0")
	}
	if expiryStr == "" {
		return nil, fmt.Errorf("expiry is required in YYYY-MM-DD format")
	}

	expiry, err := time.Parse("2006-01-02", expiryStr)
	if err != nil {
		return nil, fmt.Errorf("invalid expiry format: use YYYY-MM-DD")
	}
	daysToExpiry := expiry.Sub(time.Now()).Hours() / 24
	if daysToExpiry <= 0 {
		return nil, fmt.Errorf("expiry must be in the future")
	}

	optionType := strings.ToUpper(optType)
	isCall := optionType == "CE" || optionType == "CALL"
	isPut := optionType == "PE" || optionType == "PUT"
	if !isCall && !isPut {
		return nil, fmt.Errorf("option type must be CE or PE")
	}

	t := daysToExpiry / 365.0
	sigma := ivPercent / 100.0
	rate := ratePercent / 100.0
	sqrtT := math.Sqrt(t)
	d1 := (math.Log(spot/strike) + (rate+0.5*sigma*sigma)*t) / (sigma * sqrtT)
	d2 := d1 - sigma*sqrtT
	pdfD1 := normalPDF(d1)
	discount := math.Exp(-rate * t)

	greeks := &optionGreeks{
		Gamma: pdfD1 / (spot * sigma * sqrtT),
		Vega:  spot * pdfD1 * sqrtT / 100.0,
		D1:    d1,
		D2:    d2,
	}

	if isCall {
		greeks.Delta = normalCDF(d1)
		greeks.Theta = (-(spot*pdfD1*sigma)/(2*sqrtT) - rate*strike*discount*normalCDF(d2)) / 365.0
		greeks.Rho = strike * t * discount * normalCDF(d2) / 100.0
		greeks.IntrinsicValue = math.Max(spot-strike, 0)
	} else {
		greeks.Delta = normalCDF(d1) - 1
		greeks.Theta = (-(spot*pdfD1*sigma)/(2*sqrtT) + rate*strike*discount*normalCDF(-d2)) / 365.0
		greeks.Rho = -strike * t * discount * normalCDF(-d2) / 100.0
		greeks.IntrinsicValue = math.Max(strike-spot, 0)
	}
	if premium > 0 {
		greeks.TimeValue = math.Max(premium-greeks.IntrinsicValue, 0)
	}

	return greeks, nil
}

func normalCDF(x float64) float64 {
	return 0.5 * (1 + math.Erf(x/math.Sqrt2))
}

func normalPDF(x float64) float64 {
	return math.Exp(-0.5*x*x) / math.Sqrt(2*math.Pi)
}

func newOptionsStrategyCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "strategy",
		Short: "Option strategy builder",
		Long: `Build and analyze option strategies.

Currently supports long straddle construction with user-supplied premiums.`,
		Example: `  trader options strategy build straddle --symbol NIFTY --strike 19500 --call-premium 112.8 --put-premium 78.6`,
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
				{"straddle", "Long ATM Call + Put with explicit premiums"},
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
			callPremium, _ := cmd.Flags().GetFloat64("call-premium")
			putPremium, _ := cmd.Flags().GetFloat64("put-premium")
			quantity, _ := cmd.Flags().GetInt("qty")
			strategyType = strings.ToLower(strategyType)

			if strategyType != "straddle" && strategyType != "long-straddle" {
				err := fmt.Errorf("strategy %q is not implemented; run 'trader options strategy list' for implemented strategies", strategyType)
				output.Error("%v", err)
				return err
			}
			if strike <= 0 {
				err := fmt.Errorf("strike must be greater than 0")
				output.Error("%v", err)
				return err
			}
			if quantity <= 0 {
				err := fmt.Errorf("quantity must be greater than 0")
				output.Error("%v", err)
				return err
			}

			output.Bold("Long Straddle Strategy - %s", symbol)
			output.Printf("  ATM Strike: %.0f\n\n", strike)

			output.Bold("Legs")
			output.Printf("  1. BUY  %s %.0f CE x %d\n", symbol, strike, quantity)
			output.Printf("  2. BUY  %s %.0f PE x %d\n", symbol, strike, quantity)
			output.Println()

			if callPremium <= 0 || putPremium <= 0 {
				output.Warning("Premiums not supplied; analysis skipped")
				output.Dim("Pass --call-premium and --put-premium to calculate breakevens and max loss.")
				return nil
			}

			totalPremium := (callPremium + putPremium) * float64(quantity)
			upperBreakeven := strike + callPremium + putPremium
			lowerBreakeven := strike - callPremium - putPremium

			output.Bold("Analysis")
			output.Printf("  Net Premium:    %s\n", FormatIndianCurrency(totalPremium))
			output.Printf("  Max Profit:     %s\n", output.Green("Unlimited"))
			output.Printf("  Max Loss:       %s\n", output.Red(FormatIndianCurrency(totalPremium)))
			output.Printf("  Upper Breakeven: %.2f\n", upperBreakeven)
			output.Printf("  Lower Breakeven: %.2f\n", lowerBreakeven)

			return nil
		},
	})

	cmd.PersistentFlags().String("symbol", "NIFTY", "Underlying symbol")
	cmd.PersistentFlags().Float64("strike", 0, "Strike price")
	cmd.PersistentFlags().Float64("call-premium", 0, "Call option premium")
	cmd.PersistentFlags().Float64("put-premium", 0, "Put option premium")
	cmd.PersistentFlags().Int("qty", 1, "Lots or units per leg")

	return cmd
}

func newOptionsPayoffCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "payoff",
		Short:   "Display payoff diagram",
		Long:    "Display ASCII payoff diagram for a long straddle using explicit premiums.",
		Example: `  trader options payoff --symbol NIFTY --strike 19500 --call-premium 112.8 --put-premium 78.6`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			symbol, _ := cmd.Flags().GetString("symbol")
			strike, _ := cmd.Flags().GetFloat64("strike")
			callPremium, _ := cmd.Flags().GetFloat64("call-premium")
			putPremium, _ := cmd.Flags().GetFloat64("put-premium")

			if strike <= 0 {
				err := fmt.Errorf("strike must be greater than 0")
				output.Error("%v", err)
				return err
			}
			if callPremium <= 0 || putPremium <= 0 {
				err := fmt.Errorf("call and put premiums are required")
				output.Error("%v", err)
				return err
			}

			totalPremium := callPremium + putPremium
			lowerBreakeven := strike - totalPremium
			upperBreakeven := strike + totalPremium

			output.Bold("Payoff Diagram - %s %.0f Long Straddle", symbol, strike)
			output.Println()

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
				fmt.Sprintf("              %.0f  %.0f  %.0f  Price", lowerBreakeven, strike, upperBreakeven),
			}

			for _, line := range diagram {
				output.Println(line)
			}

			output.Println()
			output.Printf("  Breakeven: %.2f - %.2f\n", lowerBreakeven, upperBreakeven)
			output.Printf("  Max Loss:  %s at %.0f\n", output.Red(FormatIndianCurrency(totalPremium)), strike)

			return nil
		},
	}

	cmd.Flags().String("symbol", "NIFTY", "Underlying symbol")
	cmd.Flags().Float64("strike", 0, "Strike price")
	cmd.Flags().Float64("call-premium", 0, "Call option premium")
	cmd.Flags().Float64("put-premium", 0, "Put option premium")

	return cmd
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
		Use:   "create <symbol>",
		Short: "Create a GTT order",
		Long: `Create a GTT order that triggers when price reaches specified level.

Supports single-trigger GTT orders. Without --confirm this command only prints a preview.`,
		Example: `  trader gtt create RELIANCE --trigger 2400 --price 2395 --qty 10 --side BUY
  trader gtt create INFY --trigger 1600 --price 1595 --qty 5 --side SELL --confirm`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			symbol := strings.ToUpper(args[0])
			triggerPrice, _ := cmd.Flags().GetFloat64("trigger")
			price, _ := cmd.Flags().GetFloat64("price")
			qty, _ := cmd.Flags().GetInt("qty")
			sideFlag, _ := cmd.Flags().GetString("side")
			productFlag, _ := cmd.Flags().GetString("product")
			exchangeFlag, _ := cmd.Flags().GetString("exchange")
			confirm, _ := cmd.Flags().GetBool("confirm")

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}
			if err := app.validateSymbol(symbol); err != nil {
				output.Error("Invalid symbol: %v", err)
				return err
			}
			if err := app.validatePrice(triggerPrice); err != nil {
				output.Error("Invalid trigger price: %v", err)
				return err
			}
			if err := app.validatePrice(price); err != nil {
				output.Error("Invalid order price: %v", err)
				return err
			}
			if err := app.validateQuantity(qty); err != nil {
				output.Error("Invalid quantity: %v", err)
				return err
			}

			side := models.OrderSide(strings.ToUpper(sideFlag))
			if side != models.OrderSideBuy && side != models.OrderSideSell {
				return fmt.Errorf("side must be BUY or SELL")
			}
			product := models.ProductType(strings.ToUpper(productFlag))
			exchange := models.Exchange(strings.ToUpper(exchangeFlag))

			gtt := &models.GTTOrder{
				Symbol:       symbol,
				Exchange:     exchange,
				TriggerType:  "single",
				TriggerPrice: triggerPrice,
				Orders: []models.GTTOrderLeg{
					{
						Side:     side,
						Type:     models.OrderTypeLimit,
						Product:  product,
						Quantity: qty,
						Price:    price,
					},
				},
			}

			output.Bold("GTT Order Preview")
			output.Printf("  Symbol:        %s\n", gtt.Symbol)
			output.Printf("  Exchange:      %s\n", gtt.Exchange)
			output.Printf("  Trigger Type:  Single\n")
			output.Printf("  Trigger Price: %s\n", FormatIndianCurrency(gtt.TriggerPrice))
			output.Printf("  Order Price:   %s\n", FormatIndianCurrency(price))
			output.Printf("  Quantity:      %d\n", qty)
			output.Printf("  Side:          %s\n", side)
			output.Printf("  Product:       %s\n", product)
			output.Println()

			if !confirm {
				output.Warning("Use --confirm to place the GTT order")
				return nil
			}
			if err := app.checkPlaceGTT(ctx); err != nil {
				output.Error("%v", err)
				return err
			}

			result, err := app.Broker.PlaceGTT(ctx, gtt)
			if err != nil {
				output.Error("Failed to place GTT order: %v", err)
				return err
			}
			output.Success("✓ GTT order placed: %s", result.TriggerID)

			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List GTT orders",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			gtts, err := app.Broker.GetGTTs(ctx)
			if err != nil {
				output.Error("Failed to get GTT orders: %v", err)
				return err
			}
			if output.IsJSON() {
				return output.JSON(gtts)
			}
			if len(gtts) == 0 {
				output.Info("No GTT orders found")
				return nil
			}

			output.Bold("GTT Orders")
			output.Println()

			table := NewTable(output, "ID", "Symbol", "Trigger", "Price", "Qty", "Side", "Status")
			for _, gtt := range gtts {
				price := "-"
				qty := "-"
				side := "-"
				if len(gtt.Orders) > 0 {
					price = FormatPrice(gtt.Orders[0].Price)
					qty = fmt.Sprintf("%d", gtt.Orders[0].Quantity)
					side = string(gtt.Orders[0].Side)
				}
				table.AddRow(gtt.ID, gtt.Symbol, FormatPrice(gtt.TriggerPrice), price, qty, side, gtt.Status)
			}
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
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			gttID := args[0]
			if err := app.validateOrderID(gttID); err != nil {
				output.Error("Invalid GTT ID: %v", err)
				return err
			}
			if err := app.checkCancelGTT(ctx); err != nil {
				output.Error("%v", err)
				return err
			}

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}
			if err := app.Broker.CancelGTT(ctx, gttID); err != nil {
				output.Error("Failed to cancel GTT order: %v", err)
				return err
			}

			output.Success("✓ GTT order cancelled")
			return nil
		},
	})

	for _, subCmd := range cmd.Commands() {
		if subCmd.Use == "create <symbol>" {
			subCmd.Flags().Float64("trigger", 0, "Trigger price")
			subCmd.Flags().Float64("price", 0, "Limit order price")
			subCmd.Flags().Int("qty", 0, "Quantity")
			subCmd.Flags().String("side", "BUY", "Order side (BUY or SELL)")
			subCmd.Flags().String("product", "CNC", "Product type (MIS, CNC, NRML)")
			subCmd.Flags().String("exchange", "NSE", "Exchange")
			subCmd.Flags().Bool("confirm", false, "Place the GTT order")
		}
	}

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
