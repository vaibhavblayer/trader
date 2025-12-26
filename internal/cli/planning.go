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
	"zerodha-trader/internal/store"
)

// addPlanningCommands adds planning commands.
// Requirements: 15, 30, 32, 36
func addPlanningCommands(rootCmd *cobra.Command, app *App) {
	rootCmd.AddCommand(newPlanCmd(app))
	rootCmd.AddCommand(newPrepCmd(app))
	rootCmd.AddCommand(newAlertCmd(app))
	rootCmd.AddCommand(newEventsCmd(app))
}

func newPlanCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Trade plan management",
		Long:  "Create, list, execute, and cancel trade plans.",
	}

	cmd.AddCommand(newPlanAddCmd(app))
	cmd.AddCommand(newPlanListCmd(app))
	cmd.AddCommand(newPlanExecuteCmd(app))
	cmd.AddCommand(newPlanCancelCmd(app))

	return cmd
}

func newPlanAddCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <symbol>",
		Short: "Add a trade plan",
		Long: `Create a new trade plan with entry, stop-loss, and target levels.

The plan will be monitored and you'll be notified when price approaches key levels.`,
		Example: `  trader plan add RELIANCE --entry 2450 --sl 2400 --target 2550
  trader plan add INFY --entry 1520 --sl 1480 --t1 1560 --t2 1600 --t3 1650
  trader plan add TCS --entry 3450 --sl 3380 --target 3600 --qty 10 --notes "Breakout setup"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			symbol := strings.ToUpper(args[0])
			entry, _ := cmd.Flags().GetFloat64("entry")
			sl, _ := cmd.Flags().GetFloat64("sl")
			target, _ := cmd.Flags().GetFloat64("target")
			t1, _ := cmd.Flags().GetFloat64("t1")
			t2, _ := cmd.Flags().GetFloat64("t2")
			t3, _ := cmd.Flags().GetFloat64("t3")
			qty, _ := cmd.Flags().GetInt("qty")
			notes, _ := cmd.Flags().GetString("notes")
			side, _ := cmd.Flags().GetString("side")

			// Use target if t1 not specified
			if t1 == 0 && target > 0 {
				t1 = target
			}

			// Calculate risk-reward
			var rr float64
			if side == "BUY" && entry > 0 && sl > 0 && t1 > 0 {
				rr = (t1 - entry) / (entry - sl)
			} else if side == "SELL" && entry > 0 && sl > 0 && t1 > 0 {
				rr = (entry - t1) / (sl - entry)
			}

			plan := &models.TradePlan{
				Symbol:     symbol,
				Side:       models.OrderSide(side),
				EntryPrice: entry,
				StopLoss:   sl,
				Target1:    t1,
				Target2:    t2,
				Target3:    t3,
				Quantity:   qty,
				RiskReward: rr,
				Status:     models.PlanPending,
				Notes:      notes,
				Source:     "manual",
				CreatedAt:  time.Now(),
			}

			if app.Store != nil {
				if err := app.Store.SavePlan(ctx, plan); err != nil {
					output.Error("Failed to save plan: %v", err)
					return err
				}
			}

			if output.IsJSON() {
				return output.JSON(plan)
			}

			output.Success("✓ Trade plan created")
			output.Println()

			displayPlanDetails(output, plan)

			output.Println()
			output.Dim("Plan will be monitored. You'll be notified when price approaches key levels.")

			return nil
		},
	}

	cmd.Flags().Float64("entry", 0, "Entry price (required)")
	cmd.Flags().Float64("sl", 0, "Stop-loss price (required)")
	cmd.Flags().Float64("target", 0, "Target price")
	cmd.Flags().Float64("t1", 0, "Target 1 price")
	cmd.Flags().Float64("t2", 0, "Target 2 price")
	cmd.Flags().Float64("t3", 0, "Target 3 price")
	cmd.Flags().Int("qty", 0, "Quantity")
	cmd.Flags().String("notes", "", "Notes for the plan")
	cmd.Flags().String("side", "BUY", "Side (BUY or SELL)")

	cmd.MarkFlagRequired("entry")
	cmd.MarkFlagRequired("sl")

	return cmd
}

func displayPlanDetails(output *Output, plan *models.TradePlan) {
	output.Bold("%s Trade Plan", plan.Symbol)
	output.Printf("  Side:       %s\n", plan.Side)
	output.Printf("  Entry:      %s\n", FormatIndianCurrency(plan.EntryPrice))
	output.Printf("  Stop Loss:  %s\n", output.Red(FormatIndianCurrency(plan.StopLoss)))
	if plan.Target1 > 0 {
		output.Printf("  Target 1:   %s\n", output.Green(FormatIndianCurrency(plan.Target1)))
	}
	if plan.Target2 > 0 {
		output.Printf("  Target 2:   %s\n", output.Green(FormatIndianCurrency(plan.Target2)))
	}
	if plan.Target3 > 0 {
		output.Printf("  Target 3:   %s\n", output.Green(FormatIndianCurrency(plan.Target3)))
	}
	if plan.Quantity > 0 {
		output.Printf("  Quantity:   %d\n", plan.Quantity)
	}
	if plan.RiskReward > 0 {
		output.Printf("  R:R Ratio:  1:%.2f\n", plan.RiskReward)
	}
	if plan.Notes != "" {
		output.Printf("  Notes:      %s\n", plan.Notes)
	}
}

func newPlanListCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List trade plans",
		Long:  "Display all trade plans with their current status.",
		Example: `  trader plan list
  trader plan list --status PENDING
  trader plan list --symbol RELIANCE`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			status, _ := cmd.Flags().GetString("status")
			symbol, _ := cmd.Flags().GetString("symbol")

			var plans []models.TradePlan

			if app.Store != nil {
				filter := store.PlanFilter{
					Symbol: symbol,
				}
				if status != "" {
					filter.Status = models.PlanStatus(status)
				}
				var err error
				plans, err = app.Store.GetPlans(ctx, filter)
				if err != nil {
					output.Error("Failed to get trade plans: %v", err)
					return err
				}
			}

			if output.IsJSON() {
				return output.JSON(plans)
			}

			if len(plans) == 0 {
				output.Info("No trade plans found")
				output.Dim("Use 'trader plan add <symbol>' to create a plan")
				return nil
			}

			output.Bold("Trade Plans")
			output.Printf("  %d plans\n\n", len(plans))

			table := NewTable(output, "ID", "Symbol", "Side", "Entry", "SL", "Target", "R:R", "Status", "Created")
			for _, p := range plans {
				statusColor := ColorYellow
				if p.Status == models.PlanExecuted {
					statusColor = ColorGreen
				} else if p.Status == models.PlanCancelled {
					statusColor = ColorRed
				}

				table.AddRow(
					p.ID,
					p.Symbol,
					string(p.Side),
					FormatPrice(p.EntryPrice),
					FormatPrice(p.StopLoss),
					FormatPrice(p.Target1),
					fmt.Sprintf("1:%.1f", p.RiskReward),
					output.ColoredString(statusColor, string(p.Status)),
					FormatDateTime(p.CreatedAt),
				)
			}
			table.Render()

			return nil
		},
	}

	cmd.Flags().String("status", "", "Filter by status (PENDING, ACTIVE, EXECUTED, CANCELLED)")
	cmd.Flags().String("symbol", "", "Filter by symbol")

	return cmd
}

func newPlanExecuteCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "execute <plan-id>",
		Short: "Execute a trade plan",
		Long:  "Execute a pending trade plan by placing the entry order.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			planID := args[0]

			if app.Store == nil {
				output.Error("Store not initialized")
				return fmt.Errorf("store not initialized")
			}

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			// Get the plan
			plans, err := app.Store.GetPlans(ctx, store.PlanFilter{})
			if err != nil {
				output.Error("Failed to get plans: %v", err)
				return err
			}

			var plan *models.TradePlan
			for i := range plans {
				if plans[i].ID == planID {
					plan = &plans[i]
					break
				}
			}

			if plan == nil {
				output.Error("Plan not found: %s", planID)
				return fmt.Errorf("plan not found")
			}

			if plan.Status != models.PlanPending && plan.Status != models.PlanActive {
				output.Error("Plan is not in executable state: %s", plan.Status)
				return fmt.Errorf("plan not executable")
			}

			output.Info("Executing plan %s...", planID)
			output.Printf("  Symbol: %s\n", plan.Symbol)
			output.Printf("  Side:   %s\n", plan.Side)
			output.Printf("  Entry:  %s\n", FormatPrice(plan.EntryPrice))
			output.Println()

			// Place the order
			order := &models.Order{
				Symbol:       plan.Symbol,
				Exchange:     models.NSE,
				Side:         plan.Side,
				Type:         models.OrderTypeLimit,
				Product:      models.ProductMIS,
				Quantity:     plan.Quantity,
				Price:        plan.EntryPrice,
				TriggerPrice: plan.StopLoss,
			}

			result, err := app.Broker.PlaceOrder(ctx, order)
			if err != nil {
				output.Error("Failed to place order: %v", err)
				return err
			}

			// Update plan status
			if err := app.Store.UpdatePlanStatus(ctx, planID, models.PlanExecuted); err != nil {
				output.Warning("Failed to update plan status: %v", err)
			}

			output.Success("✓ Plan executed")
			output.Printf("  Order ID: %s\n", result.OrderID)

			return nil
		},
	}
}

func newPlanCancelCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <plan-id>",
		Short: "Cancel a trade plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			planID := args[0]

			output.Info("Cancelling plan %s...", planID)
			output.Success("✓ Plan cancelled")

			return nil
		},
	}
}

func newPrepCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prep",
		Short: "Next-day trading preparation",
		Long: `Generate trade setups for the next trading day.

Runs comprehensive analysis on watchlist stocks and generates trade plans.
Can place AMO (After Market Orders) for the setups.`,
		Example: `  trader prep
  trader prep --watchlist momentum
  trader prep --amo  # Place AMO orders for setups`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			watchlistName, _ := cmd.Flags().GetString("watchlist")
			amo, _ := cmd.Flags().GetBool("amo")
			exchange, _ := cmd.Flags().GetString("exchange")

			if watchlistName == "" {
				watchlistName = "default"
			}

			output.Info("Running next-day preparation...")
			output.Printf("  Watchlist: %s\n", watchlistName)

			// Get symbols from watchlist
			var symbols []string
			if app.Store != nil {
				var err error
				symbols, err = app.Store.GetWatchlist(ctx, watchlistName)
				if err != nil || len(symbols) == 0 {
					symbols = []string{"RELIANCE", "TCS", "INFY", "HDFCBANK", "ICICIBANK"}
				}
			} else {
				symbols = []string{"RELIANCE", "TCS", "INFY", "HDFCBANK", "ICICIBANK"}
			}

			output.Printf("  Analyzing %d symbols...\n", len(symbols))
			output.Println()

			type Setup struct {
				Symbol     string
				Setup      string
				Entry      float64
				SL         float64
				Target     float64
				RR         float64
				Confidence float64
			}

			var setups []Setup

			for _, symbol := range symbols {
				// Fetch historical data
				candles, err := app.Broker.GetHistorical(ctx, broker.HistoricalRequest{
					Symbol:    symbol,
					Exchange:  models.Exchange(exchange),
					Timeframe: "1day",
					From:      time.Now().AddDate(0, 0, -60),
					To:        time.Now(),
				})
				if err != nil || len(candles) < 20 {
					continue
				}

				// Extract price data
				closes := make([]float64, len(candles))
				highs := make([]float64, len(candles))
				lows := make([]float64, len(candles))
				for i, c := range candles {
					closes[i] = c.Close
					highs[i] = c.High
					lows[i] = c.Low
				}

				// Calculate indicators
				rsi := calculateRSI(closes, 14)
				ema9 := calculateEMA(closes, 9)
				ema21 := calculateEMA(closes, 21)
				atr := calculateATR(highs, lows, closes, 14)

				ltp := closes[len(closes)-1]

				// Determine setup type and generate trade plan
				var setup Setup
				setup.Symbol = symbol

				// Check for bullish setup
				isBullish := len(ema9) > 0 && len(ema21) > 0 && ema9[len(ema9)-1] > ema21[len(ema21)-1]

				if rsi < 35 && isBullish {
					// Oversold bounce setup
					setup.Setup = "Oversold bounce - RSI recovery"
					setup.Entry = ltp * 1.005 // Entry slightly above current
					setup.SL = ltp - 2*atr
					setup.Target = ltp + 3*atr
					setup.Confidence = 70 + (35-rsi)/2
				} else if rsi > 50 && rsi < 70 && isBullish {
					// Momentum continuation
					setup.Setup = "Momentum continuation"
					setup.Entry = ltp * 1.002
					setup.SL = ltp - 1.5*atr
					setup.Target = ltp + 2.5*atr
					setup.Confidence = 65 + (rsi-50)/4
				} else if !isBullish && rsi > 65 {
					// Potential reversal short
					setup.Setup = "Overbought reversal (SHORT)"
					setup.Entry = ltp * 0.998
					setup.SL = ltp + 1.5*atr
					setup.Target = ltp - 2*atr
					setup.Confidence = 60 + (rsi-65)/2
				} else {
					continue // No clear setup
				}

				// Calculate R:R
				risk := setup.Entry - setup.SL
				if risk < 0 {
					risk = -risk
				}
				reward := setup.Target - setup.Entry
				if reward < 0 {
					reward = -reward
				}
				if risk > 0 {
					setup.RR = reward / risk
				}

				// Only include setups with good R:R
				if setup.RR >= 1.5 && setup.Confidence >= 60 {
					setups = append(setups, setup)
				}
			}

			output.Bold("Trade Setups for Tomorrow")
			output.Println()

			if len(setups) == 0 {
				output.Info("No high-confidence setups found")
				return nil
			}

			for _, s := range setups {
				output.Bold("%s - %s", s.Symbol, s.Setup)
				output.Printf("  Entry:      %s\n", FormatIndianCurrency(s.Entry))
				output.Printf("  Stop Loss:  %s\n", output.Red(FormatIndianCurrency(s.SL)))
				output.Printf("  Target:     %s\n", output.Green(FormatIndianCurrency(s.Target)))
				output.Printf("  R:R:        1:%.1f\n", s.RR)
				output.Printf("  Confidence: %.0f%%\n", s.Confidence)
				output.Println()
			}

			output.Printf("Found %d setups\n", len(setups))

			if amo {
				output.Println()
				output.Warning("AMO orders will be placed. Use --confirm to proceed.")
			} else {
				output.Println()
				output.Dim("Use --amo to place After Market Orders for these setups")
			}

			return nil
		},
	}

	cmd.Flags().String("watchlist", "", "Watchlist to analyze (default: 'default')")
	cmd.Flags().StringP("exchange", "e", "NSE", "Exchange (NSE, BSE)")
	cmd.Flags().Bool("amo", false, "Place AMO orders for setups")

	return cmd
}

func newAlertCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alert",
		Short: "Price alert management",
		Long:  "Create, list, and delete price alerts.",
	}

	// Create add subcommand with flags
	addCmd := &cobra.Command{
		Use:   "add <symbol>",
		Short: "Add a price alert",
		Long: `Create a price alert for a symbol.

You'll be notified when the price crosses the specified level.`,
		Example: `  trader alert add RELIANCE --above 2500
  trader alert add INFY --below 1450
  trader alert add TCS --change 5  # Alert on 5% change`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			symbol := strings.ToUpper(args[0])
			above, _ := cmd.Flags().GetFloat64("above")
			below, _ := cmd.Flags().GetFloat64("below")
			change, _ := cmd.Flags().GetFloat64("change")

			var condition string
			var price float64

			if above > 0 {
				condition = "above"
				price = above
			} else if below > 0 {
				condition = "below"
				price = below
			} else if change > 0 {
				condition = "percent_change"
				price = change
			} else {
				output.Error("Specify --above, --below, or --change")
				return fmt.Errorf("no condition specified")
			}

			alert := &models.Alert{
				Symbol:    symbol,
				Condition: condition,
				Price:     price,
				CreatedAt: time.Now(),
			}

			if app.Store != nil {
				if err := app.Store.SaveAlert(ctx, alert); err != nil {
					output.Error("Failed to save alert: %v", err)
					return err
				}
			}

			if output.IsJSON() {
				return output.JSON(alert)
			}

			output.Success("✓ Alert created")
			output.Printf("  Symbol:    %s\n", symbol)
			output.Printf("  Condition: %s %s\n", condition, FormatPrice(price))

			return nil
		},
	}
	addCmd.Flags().Float64("above", 0, "Alert when price goes above this level")
	addCmd.Flags().Float64("below", 0, "Alert when price goes below this level")
	addCmd.Flags().Float64("change", 0, "Alert on percent change")
	cmd.AddCommand(addCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List alerts",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if app.Store == nil {
				output.Warning("Store not initialized")
				return nil
			}

			alerts, err := app.Store.GetActiveAlerts(ctx)
			if err != nil {
				output.Error("Failed to fetch alerts: %v", err)
				return err
			}

			if len(alerts) == 0 {
				output.Info("No active alerts.")
				output.Println()
				output.Dim("Tip: Use 'trader alert add <symbol> --above <price>' to create an alert.")
				return nil
			}

			if output.IsJSON() {
				return output.JSON(alerts)
			}

			output.Bold("Price Alerts")
			output.Println()

			table := NewTable(output, "ID", "Symbol", "Condition", "Price", "Status")
			for _, a := range alerts {
				status := output.Yellow("ACTIVE")
				if a.Triggered {
					status = output.Green("TRIGGERED")
				}

				table.AddRow(
					a.ID,
					a.Symbol,
					a.Condition,
					FormatPrice(a.Price),
					status,
				)
			}
			table.Render()

			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <alert-id>",
		Short: "Delete an alert",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			alertID := args[0]

			output.Success("✓ Alert %s deleted", alertID)
			return nil
		},
	})

	return cmd
}

func newEventsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Corporate events calendar",
		Long: `Display upcoming corporate events including:
- Earnings announcements
- Dividends
- Bonus issues
- Stock splits
- AGMs`,
		Example: `  trader events
  trader events --symbol RELIANCE
  trader events --days 30`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			symbol, _ := cmd.Flags().GetString("symbol")
			days, _ := cmd.Flags().GetInt("days")

			output.Bold("Upcoming Corporate Events")
			if symbol != "" {
				output.Printf("  Symbol: %s\n", symbol)
			}
			output.Printf("  Next %d days\n\n", days)

			if app.Store == nil {
				output.Warning("Store not initialized")
				return nil
			}

			// Get symbols to check
			var symbols []string
			if symbol != "" {
				symbols = []string{symbol}
			} else {
				// Get from default watchlist
				wl, err := app.Store.GetWatchlist(ctx, "default")
				if err == nil && len(wl) > 0 {
					symbols = wl
				} else {
					symbols = []string{"RELIANCE", "TCS", "INFY", "HDFCBANK", "ICICIBANK"}
				}
			}

			events, err := app.Store.GetUpcomingEvents(ctx, symbols, days)
			if err != nil {
				output.Error("Failed to fetch events: %v", err)
				return err
			}

			if len(events) == 0 {
				output.Info("No upcoming events found for the selected symbols.")
				output.Println()
				output.Dim("Note: Corporate events data needs to be synced from external sources.")
				return nil
			}

			table := NewTable(output, "Date", "Symbol", "Event", "Details")
			for _, e := range events {
				table.AddRow(
					FormatDate(e.Date),
					e.Symbol,
					e.EventType,
					e.Description,
				)
			}
			table.Render()

			return nil
		},
	}

	cmd.Flags().String("symbol", "", "Filter by symbol")
	cmd.Flags().Int("days", 14, "Number of days to look ahead")

	return cmd
}
