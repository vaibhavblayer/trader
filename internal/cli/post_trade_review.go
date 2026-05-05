package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/models"
)

func newPostTradeReviewCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "review",
		Aliases: []string{"post-trade"},
		Short:   "Post-trade review workflow",
		Long:    "Link completed trades with prediction setup, gates, execution quality, and P&L.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPostTradeReviewReport(cmd, app)
		},
	}
	reportCmd := &cobra.Command{
		Use:   "report",
		Short: "Show post-trade review report",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPostTradeReviewReport(cmd, app)
		},
	}
	addPostTradeReviewFlags(cmd)
	addPostTradeReviewFlags(reportCmd)
	cmd.AddCommand(reportCmd)
	return cmd
}

func addPostTradeReviewFlags(cmd *cobra.Command) {
	cmd.Flags().Int("days", 30, "Number of recent days to review")
	cmd.Flags().String("symbol", "", "Filter by symbol")
	cmd.Flags().Bool("paper-only", false, "Only include paper trades")
	cmd.Flags().Bool("live-only", false, "Only include live trades")
	cmd.Flags().Int("limit", 50, "Maximum completed trades to review")
}

func runPostTradeReviewReport(cmd *cobra.Command, app *App) error {
	output := NewOutput(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if app.Store == nil {
		output.Warning("Post-trade review store is not available")
		return nil
	}

	days, _ := cmd.Flags().GetInt("days")
	symbol, _ := cmd.Flags().GetString("symbol")
	paperOnly, _ := cmd.Flags().GetBool("paper-only")
	liveOnly, _ := cmd.Flags().GetBool("live-only")
	limit, _ := cmd.Flags().GetInt("limit")
	if paperOnly && liveOnly {
		return fmt.Errorf("--paper-only and --live-only cannot be used together")
	}

	filter := models.PostTradeReviewFilter{
		Symbol: strings.ToUpper(strings.TrimSpace(symbol)),
		Limit:  limit,
	}
	if paperOnly || liveOnly {
		isPaper := paperOnly
		filter.IsPaper = &isPaper
	}
	if days > 0 {
		filter.StartDate = time.Now().AddDate(0, 0, -days)
		filter.EndDate = time.Now()
	}

	report, err := app.Store.GetPostTradeReviewReport(ctx, filter)
	if err != nil {
		output.Error("Failed to get post-trade review report: %v", err)
		return err
	}
	if output.IsJSON() {
		return output.JSON(report)
	}
	if report.TotalTrades == 0 {
		output.Info("No completed trades found for post-trade review")
		return nil
	}

	output.Bold("Post-Trade Review")
	if days > 0 {
		output.Printf("  Period: last %d days\n", days)
	}
	if filter.Symbol != "" {
		output.Printf("  Symbol: %s\n", filter.Symbol)
	}
	if filter.IsPaper != nil {
		mode := "live"
		if *filter.IsPaper {
			mode = "paper"
		}
		output.Printf("  Mode:   %s\n", mode)
	}
	output.Println()

	output.Printf("  Trades:       %d reviewed | %d winners | %d losers\n", report.ReviewedTrades, report.Winners, report.Losers)
	output.Printf("  Net P&L:      %s | Avg: %s\n", FormatPnL(report.NetPnL), FormatPercent(report.AvgPnLPercent))
	output.Printf("  Prediction:   %d linked | %d missing\n", report.WithPrediction, report.MissingPrediction)
	output.Printf("  Execution:    %d linked | %d missing | Avg slip %.1f bp | Costs %s\n",
		report.WithExecution, report.MissingExecution, report.AvgSlippageBp, FormatIndianCurrency(report.TotalCosts))
	output.Println()

	printPostTradeReviewGroups(output, "By Setup", report.BySetup)
	printPostTradeReviewGroups(output, "By Symbol", report.BySymbol)
	printPostTradeReviewGroups(output, "By Strategy", report.ByStrategy)
	printPostTradeReviewTrades(output, report.Trades)
	return nil
}

func printPostTradeReviewGroups(output *Output, title string, groups []models.PostTradeReviewGroup) {
	if len(groups) == 0 {
		return
	}
	output.Bold(title)
	table := NewTable(output, "Group", "Trades", "Win", "P&L", "Avg", "Pred", "Exec", "Conf", "Slip")
	for _, group := range groups {
		table.AddRow(
			group.Key,
			fmt.Sprintf("%d", group.Trades),
			fmt.Sprintf("%.0f%%", group.WinRate),
			FormatPnL(group.NetPnL),
			FormatPercent(group.AvgPnLPercent),
			fmt.Sprintf("%d/%d", group.WithPrediction, group.Trades),
			fmt.Sprintf("%d/%d", group.WithExecution, group.Trades),
			fmt.Sprintf("%.0f%%", group.AvgConfidence),
			fmt.Sprintf("%.1f", group.AvgSlippageBp),
		)
	}
	table.Render()
	output.Println()
}

func printPostTradeReviewTrades(output *Output, trades []models.PostTradeReviewTrade) {
	if len(trades) == 0 {
		return
	}
	output.Bold("Recent Trades")
	table := NewTable(output, "Time", "Trade", "Symbol", "Side", "Setup", "Gates", "P&L", "Slip", "Flags")
	for _, trade := range trades {
		setup := trade.SetupName
		if setup == "" {
			setup = "-"
		}
		gates := "-"
		if trade.GatesTotal > 0 {
			gates = fmt.Sprintf("%d/%d", trade.GatesPassed, trade.GatesTotal)
		}
		flags := "-"
		if len(trade.ReviewFlags) > 0 {
			flags = strings.Join(trade.ReviewFlags, ",")
		}
		table.AddRow(
			trade.Timestamp.Format("2006-01-02 15:04"),
			trade.TradeID,
			trade.Symbol,
			trade.Side,
			setup,
			gates,
			FormatPercent(trade.PnLPercent),
			fmt.Sprintf("%.1f", trade.AvgSlippageBp),
			flags,
		)
	}
	table.Render()
}
