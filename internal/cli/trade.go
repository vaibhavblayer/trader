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

// addTradingCommands adds trading commands.
// Requirements: 7, 8, 47
func addTradingCommands(rootCmd *cobra.Command, app *App) {
	rootCmd.AddCommand(newBuyCmd(app))
	rootCmd.AddCommand(newSellCmd(app))
	rootCmd.AddCommand(newPositionsCmd(app))
	rootCmd.AddCommand(newHoldingsCmd(app))
	rootCmd.AddCommand(newExitCmd(app))
	rootCmd.AddCommand(newExitAllCmd(app))
	rootCmd.AddCommand(newOrdersCmd(app))
	rootCmd.AddCommand(newBalanceCmd(app))
}

func newBuyCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "buy <symbol> <quantity>",
		Short: "Place a buy order",
		Long: `Place a buy order for a symbol.

Supports market, limit, and stop-loss orders.
Can specify stop-loss and target prices for bracket orders.`,
		Example: `  trader buy RELIANCE 10
  trader buy INFY 5 --price 1500 --sl 1450 --target 1600
  trader buy TCS 10 --product MIS --type LIMIT --price 3400`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			symbol := strings.ToUpper(args[0])
			qty := 0
			fmt.Sscanf(args[1], "%d", &qty)

			if qty <= 0 {
				output.Error("Invalid quantity: %s", args[1])
				return fmt.Errorf("invalid quantity")
			}

			price, _ := cmd.Flags().GetFloat64("price")
			sl, _ := cmd.Flags().GetFloat64("sl")
			target, _ := cmd.Flags().GetFloat64("target")
			product, _ := cmd.Flags().GetString("product")
			orderType, _ := cmd.Flags().GetString("type")
			exchange, _ := cmd.Flags().GetString("exchange")

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			// Determine order type
			ot := models.OrderTypeMarket
			if price > 0 {
				ot = models.OrderTypeLimit
			}
			if orderType != "" {
				ot = models.OrderType(orderType)
			}

			order := &models.Order{
				Symbol:   symbol,
				Exchange: models.Exchange(exchange),
				Side:     models.OrderSideBuy,
				Type:     ot,
				Product:  models.ProductType(product),
				Quantity: qty,
				Price:    price,
			}

			// Show order preview
			output.Bold("Order Preview")
			output.Printf("  Symbol:   %s\n", symbol)
			output.Printf("  Side:     %s\n", output.Green("BUY"))
			output.Printf("  Quantity: %d\n", qty)
			output.Printf("  Type:     %s\n", ot)
			output.Printf("  Product:  %s\n", product)
			if price > 0 {
				output.Printf("  Price:    %s\n", FormatIndianCurrency(price))
			}
			if sl > 0 {
				output.Printf("  Stop Loss: %s\n", FormatIndianCurrency(sl))
			}
			if target > 0 {
				output.Printf("  Target:   %s\n", FormatIndianCurrency(target))
			}
			output.Println()

			// Check if paper mode
			if app.Config.IsPaperMode() {
				output.Warning("📝 PAPER TRADING MODE")
			}

			// Place order
			result, err := app.Broker.PlaceOrder(ctx, order)
			if err != nil {
				output.Error("Order failed: %v", err)
				return err
			}

			if output.IsJSON() {
				return output.JSON(result)
			}

			output.Success("✓ Order placed successfully!")
			output.Printf("  Order ID: %s\n", result.OrderID)
			output.Printf("  Status:   %s\n", result.Status)
			output.Println()
			output.Dim("Use 'trader orders' to check order status")

			return nil
		},
	}

	cmd.Flags().Float64P("price", "p", 0, "Limit price (0 for market order)")
	cmd.Flags().Float64("sl", 0, "Stop-loss price")
	cmd.Flags().Float64("target", 0, "Target price")
	cmd.Flags().String("product", "MIS", "Product type (MIS, CNC, NRML)")
	cmd.Flags().String("type", "", "Order type (MARKET, LIMIT, SL, SL-M)")
	cmd.Flags().StringP("exchange", "e", "NSE", "Exchange (NSE, BSE, NFO)")

	return cmd
}

func newSellCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sell <symbol> <quantity>",
		Short: "Place a sell order",
		Long: `Place a sell order for a symbol.

Supports market, limit, and stop-loss orders.`,
		Example: `  trader sell RELIANCE 10
  trader sell INFY 5 --price 1550
  trader sell TCS 10 --product MIS --type LIMIT --price 3500`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			symbol := strings.ToUpper(args[0])
			qty := 0
			fmt.Sscanf(args[1], "%d", &qty)

			if qty <= 0 {
				output.Error("Invalid quantity: %s", args[1])
				return fmt.Errorf("invalid quantity")
			}

			price, _ := cmd.Flags().GetFloat64("price")
			product, _ := cmd.Flags().GetString("product")
			orderType, _ := cmd.Flags().GetString("type")
			exchange, _ := cmd.Flags().GetString("exchange")

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			// Determine order type
			ot := models.OrderTypeMarket
			if price > 0 {
				ot = models.OrderTypeLimit
			}
			if orderType != "" {
				ot = models.OrderType(orderType)
			}

			order := &models.Order{
				Symbol:   symbol,
				Exchange: models.Exchange(exchange),
				Side:     models.OrderSideSell,
				Type:     ot,
				Product:  models.ProductType(product),
				Quantity: qty,
				Price:    price,
			}

			// Show order preview
			output.Bold("Order Preview")
			output.Printf("  Symbol:   %s\n", symbol)
			output.Printf("  Side:     %s\n", output.Red("SELL"))
			output.Printf("  Quantity: %d\n", qty)
			output.Printf("  Type:     %s\n", ot)
			output.Printf("  Product:  %s\n", product)
			if price > 0 {
				output.Printf("  Price:    %s\n", FormatIndianCurrency(price))
			}
			output.Println()

			// Check if paper mode
			if app.Config.IsPaperMode() {
				output.Warning("📝 PAPER TRADING MODE")
			}

			// Place order
			result, err := app.Broker.PlaceOrder(ctx, order)
			if err != nil {
				output.Error("Order failed: %v", err)
				return err
			}

			if output.IsJSON() {
				return output.JSON(result)
			}

			output.Success("✓ Order placed successfully!")
			output.Printf("  Order ID: %s\n", result.OrderID)
			output.Printf("  Status:   %s\n", result.Status)

			return nil
		},
	}

	cmd.Flags().Float64P("price", "p", 0, "Limit price (0 for market order)")
	cmd.Flags().String("product", "MIS", "Product type (MIS, CNC, NRML)")
	cmd.Flags().String("type", "", "Order type (MARKET, LIMIT, SL, SL-M)")
	cmd.Flags().StringP("exchange", "e", "NSE", "Exchange (NSE, BSE, NFO)")

	return cmd
}

func newPositionsCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "positions",
		Short: "View open positions",
		Long:  "Display all open trading positions with P&L.",
		Example: `  trader positions
  trader positions --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			positions, err := app.Broker.GetPositions(ctx)
			if err != nil {
				output.Error("Failed to get positions: %v", err)
				return err
			}

			if output.IsJSON() {
				return output.JSON(positions)
			}

			if len(positions) == 0 {
				output.Info("No open positions")
				return nil
			}

			return displayPositions(output, positions)
		},
	}
}

func displayPositions(output *Output, positions []models.Position) error {
	output.Bold("Open Positions")
	output.Printf("  %d positions\n\n", len(positions))

	var totalPnL float64
	table := NewTable(output, "Symbol", "Qty", "Avg Price", "LTP", "P&L", "P&L %", "Product")

	for _, p := range positions {
		totalPnL += p.PnL
		table.AddRow(
			p.Symbol,
			fmt.Sprintf("%d", p.Quantity),
			FormatPrice(p.AveragePrice),
			FormatPrice(p.LTP),
			output.FormatPnL(p.PnL),
			output.FormatPercent(p.PnLPercent),
			string(p.Product),
		)
	}

	table.Render()

	output.Println()
	output.Printf("  Total P&L: %s\n", output.FormatPnL(totalPnL))
	output.Println()

	// Quick exit commands
	output.Bold("Quick Actions")
	for _, p := range positions {
		if p.Quantity > 0 {
			output.Printf("  Exit %s: trader exit %s\n", p.Symbol, p.Symbol)
		}
	}

	return nil
}

func newHoldingsCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "holdings",
		Short: "View delivery holdings",
		Long:  "Display all delivery holdings with P&L.",
		Example: `  trader holdings
  trader holdings --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			holdings, err := app.Broker.GetHoldings(ctx)
			if err != nil {
				output.Error("Failed to get holdings: %v", err)
				return err
			}

			if output.IsJSON() {
				return output.JSON(holdings)
			}

			if len(holdings) == 0 {
				output.Info("No holdings")
				return nil
			}

			return displayHoldings(output, holdings)
		},
	}
}

func displayHoldings(output *Output, holdings []models.Holding) error {
	output.Bold("Holdings")
	output.Printf("  %d stocks\n\n", len(holdings))

	var totalInvested, totalCurrent, totalPnL float64
	table := NewTable(output, "Symbol", "Qty", "Avg Price", "LTP", "Invested", "Current", "P&L", "P&L %")

	for _, h := range holdings {
		totalInvested += h.InvestedValue
		totalCurrent += h.CurrentValue
		totalPnL += h.PnL

		table.AddRow(
			h.Symbol,
			fmt.Sprintf("%d", h.Quantity),
			FormatPrice(h.AveragePrice),
			FormatPrice(h.LTP),
			FormatCompact(h.InvestedValue),
			FormatCompact(h.CurrentValue),
			output.FormatPnL(h.PnL),
			output.FormatPercent(h.PnLPercent),
		)
	}

	table.Render()

	output.Println()
	output.Bold("Summary")
	output.Printf("  Invested:  %s\n", FormatCompact(totalInvested))
	output.Printf("  Current:   %s\n", FormatCompact(totalCurrent))
	output.Printf("  Total P&L: %s\n", output.FormatPnL(totalPnL))
	totalPnLPct := (totalPnL / totalInvested) * 100
	output.Printf("  Returns:   %s\n", output.FormatPercent(totalPnLPct))

	return nil
}

func newExitCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "exit <symbol>",
		Short: "Exit a position",
		Long:  "Close an open position by placing a market order.",
		Example: `  trader exit RELIANCE
  trader exit INFY --product MIS`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			symbol := strings.ToUpper(args[0])
			product, _ := cmd.Flags().GetString("product")

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			// Get positions to find the one to exit
			positions, err := app.Broker.GetPositions(ctx)
			if err != nil {
				output.Error("Failed to get positions: %v", err)
				return err
			}

			var position *models.Position
			for _, p := range positions {
				if p.Symbol == symbol && (product == "" || string(p.Product) == product) {
					position = &p
					break
				}
			}

			if position == nil {
				output.Error("No open position found for %s", symbol)
				return fmt.Errorf("position not found")
			}

			// Determine exit side
			side := models.OrderSideSell
			qty := position.Quantity
			if qty < 0 {
				side = models.OrderSideBuy
				qty = -qty
			}

			order := &models.Order{
				Symbol:   symbol,
				Exchange: position.Exchange,
				Side:     side,
				Type:     models.OrderTypeMarket,
				Product:  position.Product,
				Quantity: qty,
			}

			output.Info("Exiting %s position...", symbol)
			output.Printf("  Quantity: %d\n", qty)
			output.Printf("  Side:     %s\n", side)
			output.Println()

			result, err := app.Broker.PlaceOrder(ctx, order)
			if err != nil {
				output.Error("Exit failed: %v", err)
				return err
			}

			if output.IsJSON() {
				return output.JSON(result)
			}

			output.Success("✓ Position exit order placed!")
			output.Printf("  Order ID: %s\n", result.OrderID)

			return nil
		},
	}
}

func newExitAllCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "exit-all",
		Short: "Exit all positions",
		Long:  "Close all open positions by placing market orders.",
		Example: `  trader exit-all
  trader exit-all --product MIS`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			product, _ := cmd.Flags().GetString("product")
			force, _ := cmd.Flags().GetBool("force")

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			positions, err := app.Broker.GetPositions(ctx)
			if err != nil {
				output.Error("Failed to get positions: %v", err)
				return err
			}

			// Filter positions
			var toExit []models.Position
			for _, p := range positions {
				if p.Quantity != 0 && (product == "" || string(p.Product) == product) {
					toExit = append(toExit, p)
				}
			}

			if len(toExit) == 0 {
				output.Info("No positions to exit")
				return nil
			}

			output.Warning("About to exit %d positions:", len(toExit))
			for _, p := range toExit {
				output.Printf("  %s: %d @ %s (P&L: %s)\n",
					p.Symbol, p.Quantity, FormatPrice(p.LTP), output.FormatPnL(p.PnL))
			}
			output.Println()

			if !force {
				output.Warning("Use --force to confirm exit")
				return nil
			}

			// Exit all positions
			var exitCount int
			for _, p := range toExit {
				side := models.OrderSideSell
				qty := p.Quantity
				if qty < 0 {
					side = models.OrderSideBuy
					qty = -qty
				}

				order := &models.Order{
					Symbol:   p.Symbol,
					Exchange: p.Exchange,
					Side:     side,
					Type:     models.OrderTypeMarket,
					Product:  p.Product,
					Quantity: qty,
				}

				_, err := app.Broker.PlaceOrder(ctx, order)
				if err != nil {
					output.Error("Failed to exit %s: %v", p.Symbol, err)
				} else {
					output.Success("✓ Exited %s", p.Symbol)
					exitCount++
				}
			}

			output.Println()
			output.Printf("Exited %d/%d positions\n", exitCount, len(toExit))

			return nil
		},
	}
}

func newOrdersCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "orders",
		Short: "View orders",
		Long:  "Display all orders for the day.",
		Example: `  trader orders
  trader orders --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			orders, err := app.Broker.GetOrders(ctx)
			if err != nil {
				output.Error("Failed to get orders: %v", err)
				return err
			}

			if output.IsJSON() {
				return output.JSON(orders)
			}

			if len(orders) == 0 {
				output.Info("No orders today")
				return nil
			}

			return displayOrders(output, orders)
		},
	}
}

func displayOrders(output *Output, orders []models.Order) error {
	output.Bold("Orders")
	output.Printf("  %d orders\n\n", len(orders))

	table := NewTable(output, "Time", "Symbol", "Side", "Type", "Qty", "Price", "Status")

	for _, o := range orders {
		sideColor := ColorGreen
		if o.Side == models.OrderSideSell {
			sideColor = ColorRed
		}

		statusColor := ColorYellow
		if o.Status == "COMPLETE" {
			statusColor = ColorGreen
		} else if o.Status == "REJECTED" || o.Status == "CANCELLED" {
			statusColor = ColorRed
		}

		priceStr := "MARKET"
		if o.Price > 0 {
			priceStr = FormatPrice(o.Price)
		}

		table.AddRow(
			FormatTime(o.PlacedAt),
			o.Symbol,
			output.ColoredString(sideColor, string(o.Side)),
			string(o.Type),
			fmt.Sprintf("%d", o.Quantity),
			priceStr,
			output.ColoredString(statusColor, o.Status),
		)
	}

	table.Render()
	return nil
}

func newBalanceCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "balance",
		Short: "View account balance",
		Long:  "Display account balance and margin details.",
		Example: `  trader balance
  trader balance --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			balance, err := app.Broker.GetBalance(ctx)
			if err != nil {
				output.Error("Failed to get balance: %v", err)
				return err
			}

			margins, err := app.Broker.GetMargins(ctx)
			if err != nil {
				// Non-fatal, continue without margins
				margins = nil
			}

			if output.IsJSON() {
				return output.JSON(map[string]interface{}{
					"balance": balance,
					"margins": margins,
				})
			}

			return displayBalance(output, balance, margins)
		},
	}
}

func displayBalance(output *Output, balance *models.Balance, margins *models.Margins) error {
	output.Bold("Account Balance")
	output.Println()

	output.Printf("  Available Cash:   %s\n", output.Green(FormatIndianCurrency(balance.AvailableCash)))
	output.Printf("  Used Margin:      %s\n", FormatIndianCurrency(balance.UsedMargin))
	output.Printf("  Total Equity:     %s\n", output.BoldText(FormatIndianCurrency(balance.TotalEquity)))
	if balance.CollateralValue > 0 {
		output.Printf("  Collateral Value: %s\n", FormatIndianCurrency(balance.CollateralValue))
	}

	if margins != nil {
		output.Println()
		output.Bold("Margins")
		output.Printf("  Equity:\n")
		output.Printf("    Available: %s\n", FormatIndianCurrency(margins.Equity.Available))
		output.Printf("    Used:      %s\n", FormatIndianCurrency(margins.Equity.Used))
		output.Printf("    Total:     %s\n", FormatIndianCurrency(margins.Equity.Total))

		if margins.Commodity.Total > 0 {
			output.Printf("  Commodity:\n")
			output.Printf("    Available: %s\n", FormatIndianCurrency(margins.Commodity.Available))
			output.Printf("    Used:      %s\n", FormatIndianCurrency(margins.Commodity.Used))
			output.Printf("    Total:     %s\n", FormatIndianCurrency(margins.Commodity.Total))
		}
	}

	return nil
}
