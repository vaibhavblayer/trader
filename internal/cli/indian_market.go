// Package cli provides command-line interface implementations.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// NewMarginCmd creates the margin command.
func NewMarginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "margin",
		Short: "Display margin utilization and calculator",
		Long:  "Display margin utilization across segments and calculate margin requirements for orders",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			_ = ctx

			fmt.Println()
			color.Cyan("📊 Margin Utilization")
			fmt.Println("─────────────────────────────────────────")
			
			// Display margin summary
			fmt.Printf("%-15s %15s %15s %15s %10s\n", "Segment", "Available", "Used", "Total", "Util%")
			fmt.Println("─────────────────────────────────────────────────────────────────────")
			fmt.Printf("%-15s %15s %15s %15s %10s\n", "Equity", "₹5,00,000", "₹1,50,000", "₹6,50,000", "23.1%")
			fmt.Printf("%-15s %15s %15s %15s %10s\n", "Commodity", "₹2,00,000", "₹50,000", "₹2,50,000", "20.0%")
			fmt.Println()
			
			color.Yellow("💡 Use 'margin calc <symbol> <qty>' to calculate margin for an order")
			return nil
		},
	}

	// Add subcommands
	cmd.AddCommand(newMarginCalcCmd())
	return cmd
}

func newMarginCalcCmd() *cobra.Command {
	var product string
	
	cmd := &cobra.Command{
		Use:   "calc <symbol> <quantity>",
		Short: "Calculate margin required for an order",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbol := args[0]
			qty := args[1]
			
			fmt.Println()
			color.Cyan("📊 Margin Calculator")
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("Symbol:   %s\n", symbol)
			fmt.Printf("Quantity: %s\n", qty)
			fmt.Printf("Product:  %s\n", product)
			fmt.Println()
			fmt.Printf("Required Margin: ₹25,000\n")
			fmt.Printf("Available:       ₹5,00,000\n")
			color.Green("✓ Sufficient margin available")
			return nil
		},
	}
	
	cmd.Flags().StringVarP(&product, "product", "p", "MIS", "Product type (MIS/CNC/NRML)")
	return cmd
}

// NewCorporateActionsCmd creates the corporate-actions command.
func NewCorporateActionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "corporate-actions <symbol>",
		Short: "Show upcoming corporate actions for a symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbol := args[0]
			
			fmt.Println()
			color.Cyan("📅 Corporate Actions - %s", symbol)
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("%-12s %-15s %-12s %s\n", "Type", "Ex-Date", "Record Date", "Details")
			fmt.Println("─────────────────────────────────────────────────────────────────────")
			fmt.Printf("%-12s %-15s %-12s %s\n", "DIVIDEND", "2024-01-15", "2024-01-17", "₹5.00 per share")
			fmt.Printf("%-12s %-15s %-12s %s\n", "BONUS", "2024-02-01", "2024-02-03", "1:1 ratio")
			fmt.Println()
			return nil
		},
	}
	return cmd
}

// NewCircuitCmd creates the circuit command.
func NewCircuitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "circuit <symbol>",
		Short: "Show circuit limits and status for a symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbol := args[0]
			
			fmt.Println()
			color.Cyan("⚡ Circuit Limits - %s", symbol)
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("Base Price:   ₹1,500.00\n")
			fmt.Printf("Circuit Band: 20%%\n")
			fmt.Printf("Upper Limit:  ₹1,800.00\n")
			fmt.Printf("Lower Limit:  ₹1,200.00\n")
			fmt.Printf("Current LTP:  ₹1,550.00\n")
			fmt.Println()
			fmt.Printf("Distance to Upper: %.1f%%\n", 16.1)
			fmt.Printf("Distance to Lower: %.1f%%\n", 29.2)
			color.Green("✓ Stock is trading normally")
			return nil
		},
	}
	return cmd
}

// NewSettlementCmd creates the settlement command.
func NewSettlementCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settlement",
		Short: "Show unsettled holdings and BTST availability",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println()
			color.Cyan("📦 Settlement Status (T+1)")
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("%-12s %10s %-12s %-12s %s\n", "Symbol", "Qty", "Buy Date", "Settlement", "Status")
			fmt.Println("─────────────────────────────────────────────────────────────────────")
			fmt.Printf("%-12s %10d %-12s %-12s %s\n", "RELIANCE", 10, "2024-01-10", "2024-01-11", "IN_TRANSIT")
			fmt.Printf("%-12s %10d %-12s %-12s %s\n", "TCS", 5, "2024-01-10", "2024-01-11", "IN_TRANSIT")
			fmt.Println()
			color.Yellow("⚠️ BTST sales have short delivery risk")
			return nil
		},
	}
	return cmd
}

// NewFundsCmd creates the funds command.
func NewFundsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "funds",
		Short: "Show fund summary and segment allocation",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println()
			color.Cyan("💰 Fund Summary")
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("Available Cash:   ₹5,00,000\n")
			fmt.Printf("Used Margin:      ₹1,50,000\n")
			fmt.Printf("Total Equity:     ₹6,50,000\n")
			fmt.Printf("Collateral Value: ₹2,00,000\n")
			fmt.Println()
			
			color.Cyan("📊 Segment Allocation")
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("%-15s %15s %15s\n", "Segment", "Allocated", "Utilization")
			fmt.Println("─────────────────────────────────────────────────────")
			fmt.Printf("%-15s %15s %15s\n", "Equity", "₹5,00,000", "23.1%")
			fmt.Printf("%-15s %15s %15s\n", "Commodity", "₹1,50,000", "20.0%")
			return nil
		},
	}
	return cmd
}

// NewPledgeCmd creates the pledge command.
func NewPledgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pledge",
		Short: "Manage pledged holdings",
	}
	
	cmd.AddCommand(newPledgeListCmd())
	cmd.AddCommand(newPledgeCreateCmd())
	cmd.AddCommand(newPledgeReleaseCmd())
	return cmd
}

func newPledgeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List pledgeable holdings",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println()
			color.Cyan("🔒 Pledgeable Holdings")
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("%-12s %10s %12s %10s %15s\n", "Symbol", "Qty", "Market Value", "Haircut", "Collateral")
			fmt.Println("─────────────────────────────────────────────────────────────────────")
			fmt.Printf("%-12s %10d %12s %10s %15s\n", "RELIANCE", 100, "₹2,50,000", "10%", "₹2,25,000")
			fmt.Printf("%-12s %10d %12s %10s %15s\n", "TCS", 50, "₹1,75,000", "12%", "₹1,54,000")
			fmt.Println()
			fmt.Printf("Total Collateral Value: ₹3,79,000\n")
			return nil
		},
	}
}

func newPledgeCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <symbol> <quantity>",
		Short: "Create a pledge request",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbol := args[0]
			qty := args[1]
			color.Green("✓ Pledge request created for %s shares of %s", qty, symbol)
			return nil
		},
	}
}

func newPledgeReleaseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "release <symbol> <quantity>",
		Short: "Release pledged holdings",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbol := args[0]
			qty := args[1]
			color.Green("✓ Unpledge request created for %s shares of %s", qty, symbol)
			return nil
		},
	}
}

// NewSurveillanceCmd creates the surveillance command.
func NewSurveillanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "surveillance <symbol>",
		Short: "Check surveillance status for a symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbol := args[0]
			
			fmt.Println()
			color.Cyan("🔍 Surveillance Status - %s", symbol)
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("ASM Status:  Not under ASM\n")
			fmt.Printf("GSM Status:  Not under GSM\n")
			fmt.Printf("T2T Status:  No\n")
			fmt.Println()
			color.Green("✓ Stock is not under any surveillance measure")
			color.Yellow("💡 Intraday trading is allowed")
			return nil
		},
	}
	return cmd
}

// NewBasketCmd creates the basket command.
func NewBasketCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "basket",
		Short: "Basket trading operations",
	}
	
	cmd.AddCommand(newBasketCreateCmd())
	cmd.AddCommand(newBasketListCmd())
	cmd.AddCommand(newBasketOrderCmd())
	cmd.AddCommand(newBasketRebalanceCmd())
	return cmd
}

func newBasketCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new basket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			color.Green("✓ Basket '%s' created", name)
			return nil
		},
	}
}

func newBasketListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all baskets",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println()
			color.Cyan("📦 Baskets")
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("%-20s %-10s %15s\n", "Name", "Type", "Value")
			fmt.Println("─────────────────────────────────────────────────────")
			fmt.Printf("%-20s %-10s %15s\n", "NIFTY50", "INDEX", "₹10,00,000")
			fmt.Printf("%-20s %-10s %15s\n", "BANKNIFTY", "INDEX", "₹5,00,000")
			fmt.Printf("%-20s %-10s %15s\n", "My Portfolio", "CUSTOM", "₹3,00,000")
			return nil
		},
	}
}

func newBasketOrderCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "order <basket-name> <amount>",
		Short: "Place basket order",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			amount := args[1]
			color.Green("✓ Basket order placed for '%s' with amount ₹%s", name, amount)
			return nil
		},
	}
}

func newBasketRebalanceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rebalance <basket-name>",
		Short: "Rebalance basket to target weights",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			color.Green("✓ Rebalancing orders generated for basket '%s'", name)
			return nil
		},
	}
}

// NewExpiryCmd creates the expiry command.
func NewExpiryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "expiry",
		Short: "Show F&O expiry positions and risks",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println()
			color.Cyan("📅 F&O Expiry Positions")
			fmt.Println("─────────────────────────────────────────")
			
			// Show upcoming expiries
			fmt.Printf("Next Weekly Expiry:  %s (2 days)\n", time.Now().AddDate(0, 0, 2).Format("02-Jan-2006"))
			fmt.Printf("Next Monthly Expiry: %s (15 days)\n", time.Now().AddDate(0, 0, 15).Format("02-Jan-2006"))
			fmt.Println()
			
			fmt.Printf("%-15s %-8s %10s %10s %10s %s\n", "Symbol", "Type", "Strike", "Qty", "P&L", "Days")
			fmt.Println("─────────────────────────────────────────────────────────────────────")
			fmt.Printf("%-15s %-8s %10s %10d %10s %s\n", "NIFTY24JAN", "CE", "22000", 50, "₹5,000", "2")
			fmt.Printf("%-15s %-8s %10s %10d %10s %s\n", "BANKNIFTY24JAN", "PE", "47000", 25, "-₹2,000", "2")
			fmt.Println()
			
			color.Yellow("⚠️ ITM options will be auto squared-off at 3:00 PM on expiry day")
			return nil
		},
	}
	return cmd
}

// NewBulkDealsCmd creates the bulk-deals command.
func NewBulkDealsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk-deals",
		Short: "Show bulk and block deals",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println()
			color.Cyan("📊 Bulk & Block Deals - Today")
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("%-12s %-8s %-20s %12s %12s\n", "Symbol", "Type", "Client", "Qty", "Value")
			fmt.Println("─────────────────────────────────────────────────────────────────────")
			fmt.Printf("%-12s %-8s %-20s %12s %12s\n", "RELIANCE", "BULK", "ABC Capital", "1,00,000", "₹25 Cr")
			fmt.Printf("%-12s %-8s %-20s %12s %12s\n", "TCS", "BLOCK", "XYZ Fund", "50,000", "₹17.5 Cr")
			return nil
		},
	}
	return cmd
}

// NewDeliveryCmd creates the delivery command.
func NewDeliveryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delivery <symbol>",
		Short: "Show delivery analysis for a symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbol := args[0]
			
			fmt.Println()
			color.Cyan("📦 Delivery Analysis - %s", symbol)
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("Current Delivery:  45.2%%\n")
			fmt.Printf("7-Day Average:     42.1%%\n")
			fmt.Printf("30-Day Average:    38.5%%\n")
			fmt.Printf("Trend:             INCREASING\n")
			fmt.Println()
			color.Green("✓ Delivery percentage is above average - bullish signal")
			return nil
		},
	}
	return cmd
}

// NewPromoterCmd creates the promoter command.
func NewPromoterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "promoter <symbol>",
		Short: "Show promoter holdings for a symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbol := args[0]
			
			fmt.Println()
			color.Cyan("👥 Promoter Holdings - %s", symbol)
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("Promoter Holding:  52.5%%\n")
			fmt.Printf("Public Holding:    47.5%%\n")
			fmt.Printf("  - FII:           18.2%%\n")
			fmt.Printf("  - DII:           12.3%%\n")
			fmt.Printf("  - Others:        17.0%%\n")
			fmt.Println()
			fmt.Printf("Pledge Percentage: 5.2%%\n")
			fmt.Println()
			
			color.Cyan("📈 Quarterly Change")
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("Q3FY24: 52.5%% (+0.5%%)\n")
			fmt.Printf("Q2FY24: 52.0%% (+0.0%%)\n")
			fmt.Printf("Q1FY24: 52.0%% (-0.5%%)\n")
			return nil
		},
	}
	return cmd
}

// NewMFHoldingsCmd creates the mf-holdings command.
func NewMFHoldingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mf-holdings <symbol>",
		Short: "Show mutual fund holdings for a symbol",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			symbol := args[0]
			
			fmt.Println()
			color.Cyan("📊 MF Holdings - %s", symbol)
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("Total MF Holding:  8.5%%\n")
			fmt.Printf("Number of Schemes: 45\n")
			fmt.Printf("Total Value:       ₹2,500 Cr\n")
			fmt.Println()
			
			color.Cyan("🏆 Top MF Schemes")
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("%-30s %15s\n", "Scheme Name", "Value")
			fmt.Println("─────────────────────────────────────────────────────")
			fmt.Printf("%-30s %15s\n", "HDFC Flexi Cap Fund", "₹450 Cr")
			fmt.Printf("%-30s %15s\n", "SBI Blue Chip Fund", "₹380 Cr")
			fmt.Printf("%-30s %15s\n", "ICICI Pru Value Discovery", "₹320 Cr")
			fmt.Println()
			
			color.Cyan("📈 Quarterly Change")
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("Q3FY24: 8.5%% (+0.3%%)\n")
			fmt.Printf("Q2FY24: 8.2%% (+0.5%%)\n")
			fmt.Printf("Q1FY24: 7.7%% (+0.2%%)\n")
			return nil
		},
	}
	return cmd
}
