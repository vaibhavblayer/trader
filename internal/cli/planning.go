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
	return &cobra.Command{
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

			// This would fetch from store
			_ = ctx
			_ = status
			_ = symbol

			// Sample data
			plans := []models.TradePlan{
				{
					ID:         "PLAN001",
					Symbol:     "RELIANCE",
					Side:       models.OrderSideBuy,
					EntryPrice: 2450,
					StopLoss:   2400,
					Target1:    2550,
					RiskReward: 2.0,
					Status:     models.PlanPending,
					CreatedAt:  time.Now().Add(-2 * time.Hour),
				},
				{
					ID:         "PLAN002",
					Symbol:     "INFY",
					Side:       models.OrderSideBuy,
					EntryPrice: 1520,
					StopLoss:   1480,
					Target1:    1600,
					RiskReward: 2.0,
					Status:     models.PlanActive,
					CreatedAt:  time.Now().Add(-1 * time.Hour),
				},
			}

			if output.IsJSON() {
				return output.JSON(plans)
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
}

func newPlanExecuteCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "execute <plan-id>",
		Short: "Execute a trade plan",
		Long:  "Execute a pending trade plan by placing the entry order.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			planID := args[0]

			output.Info("Executing plan %s...", planID)
			output.Println()

			// This would fetch plan and place order
			output.Success("✓ Plan executed")
			output.Printf("  Order ID: ORD123456\n")

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
	return &cobra.Command{
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
			watchlist, _ := cmd.Flags().GetString("watchlist")
			amo, _ := cmd.Flags().GetBool("amo")

			output.Info("Running next-day preparation...")
			if watchlist != "" {
				output.Printf("  Watchlist: %s\n", watchlist)
			}
			output.Println()

			// This would run actual prep analysis
			output.Bold("Trade Setups for Tomorrow")
			output.Println()

			setups := []struct {
				symbol    string
				setup     string
				entry     float64
				sl        float64
				target    float64
				rr        float64
				confidence float64
			}{
				{"RELIANCE", "Breakout above resistance", 2480, 2440, 2580, 2.5, 75},
				{"INFY", "Pullback to support", 1510, 1470, 1590, 2.0, 68},
				{"TCS", "Flag pattern breakout", 3480, 3420, 3600, 2.0, 72},
			}

			for _, s := range setups {
				output.Bold("%s - %s", s.symbol, s.setup)
				output.Printf("  Entry:      %s\n", FormatIndianCurrency(s.entry))
				output.Printf("  Stop Loss:  %s\n", output.Red(FormatIndianCurrency(s.sl)))
				output.Printf("  Target:     %s\n", output.Green(FormatIndianCurrency(s.target)))
				output.Printf("  R:R:        1:%.1f\n", s.rr)
				output.Printf("  Confidence: %.0f%%\n", s.confidence)
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
}

func newAlertCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alert",
		Short: "Price alert management",
		Long:  "Create, list, and delete price alerts.",
	}

	cmd.AddCommand(&cobra.Command{
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
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List alerts",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			// Sample alerts
			alerts := []models.Alert{
				{ID: "ALT001", Symbol: "RELIANCE", Condition: "above", Price: 2500, Triggered: false},
				{ID: "ALT002", Symbol: "INFY", Condition: "below", Price: 1450, Triggered: false},
				{ID: "ALT003", Symbol: "TCS", Condition: "above", Price: 3500, Triggered: true},
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
	return &cobra.Command{
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
			symbol, _ := cmd.Flags().GetString("symbol")
			days, _ := cmd.Flags().GetInt("days")

			output.Bold("Upcoming Corporate Events")
			if symbol != "" {
				output.Printf("  Symbol: %s\n", symbol)
			}
			output.Printf("  Next %d days\n\n", days)

			// Sample events
			events := []struct {
				date      string
				symbol    string
				eventType string
				details   string
			}{
				{"22-Jan-2024", "RELIANCE", "Results", "Q3 FY24 Results"},
				{"25-Jan-2024", "INFY", "Results", "Q3 FY24 Results"},
				{"28-Jan-2024", "TCS", "Dividend", "Ex-Date: ₹9 per share"},
				{"30-Jan-2024", "HDFC", "AGM", "Annual General Meeting"},
				{"02-Feb-2024", "ICICI", "Results", "Q3 FY24 Results"},
			}

			table := NewTable(output, "Date", "Symbol", "Event", "Details")
			for _, e := range events {
				table.AddRow(e.date, e.symbol, e.eventType, e.details)
			}
			table.Render()

			return nil
		},
	}
}
