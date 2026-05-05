package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/models"
)

func newExecutionCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "execution",
		Short: "Execution quality reports",
		Long:  "Report slippage, costs, partial fills, rejected orders, and blocked execution events.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExecutionReport(cmd, app)
		},
	}
	reportCmd := &cobra.Command{
		Use:   "report",
		Short: "Show execution quality report",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExecutionReport(cmd, app)
		},
	}
	addExecutionReportFlags(cmd)
	addExecutionReportFlags(reportCmd)
	cmd.AddCommand(reportCmd)
	return cmd
}

func addExecutionReportFlags(cmd *cobra.Command) {
	cmd.Flags().Int("days", 30, "Number of recent days to analyze")
	cmd.Flags().String("symbol", "", "Filter by symbol")
	cmd.Flags().Float64("slippage-alert-bp", 50, "Highlight orders with adverse slippage at or above this many basis points")
	cmd.Flags().Int("limit", 20, "Maximum issue and high-slippage samples")
}

func runExecutionReport(cmd *cobra.Command, app *App) error {
	output := NewOutput(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if app.Store == nil {
		output.Warning("Execution quality store is not available")
		return nil
	}

	days, _ := cmd.Flags().GetInt("days")
	symbol, _ := cmd.Flags().GetString("symbol")
	alertBp, _ := cmd.Flags().GetFloat64("slippage-alert-bp")
	limit, _ := cmd.Flags().GetInt("limit")
	filter := models.ExecutionQualityFilter{
		Symbol:          strings.ToUpper(strings.TrimSpace(symbol)),
		SlippageAlertBp: alertBp,
		Limit:           limit,
	}
	if days > 0 {
		filter.StartDate = time.Now().AddDate(0, 0, -days)
		filter.EndDate = time.Now()
	}

	report, err := app.Store.GetExecutionQualityReport(ctx, filter)
	if err != nil {
		output.Error("Failed to get execution quality report: %v", err)
		return err
	}
	if output.IsJSON() {
		return output.JSON(report)
	}
	if report.TotalOrders == 0 && report.BlockedExecutions == 0 && report.ProtectiveFailures == 0 {
		output.Info("No execution quality events found")
		return nil
	}

	output.Bold("Execution Quality Report")
	if days > 0 {
		output.Printf("  Period: last %d days\n", days)
	}
	if filter.Symbol != "" {
		output.Printf("  Symbol: %s\n", filter.Symbol)
	}
	output.Println()

	output.Printf("  Orders:       %d total | %d filled | %d open | %d cancelled | %d rejected\n",
		report.TotalOrders, report.FilledOrders, report.OpenOrders, report.CancelledOrders, report.RejectedOrders)
	output.Printf("  Fill Rate:    %.1f%%\n", report.FillRate)
	output.Printf("  Partial:      %d (%.1f%% of filled)\n", report.PartialFills, report.PartialFillRate)
	output.Printf("  Blocks:       %d execution | %d protective\n", report.BlockedExecutions, report.ProtectiveFailures)
	output.Printf("  Slippage:     avg %.1f bp | worst %.1f bp\n", report.AvgSlippageBp, report.MaxAdverseSlippageBp)
	output.Printf("  Costs:        %s total | %.1f bp of turnover\n", FormatIndianCurrency(report.TotalCosts), report.CostBp)
	output.Printf("  Turnover:     %s\n", FormatIndianCurrency(report.TotalTurnover))
	output.Println()

	printExecutionGroups(output, "By Symbol", report.BySymbol)
	printExecutionGroups(output, "By Order Type", report.ByOrderType)
	printExecutionGroups(output, "By Side", report.BySide)
	printHighSlippage(output, report.HighSlippageOrders)
	printExecutionIssues(output, report.RecentIssues)
	return nil
}

func printExecutionGroups(output *Output, title string, groups []models.ExecutionQualityGroup) {
	if len(groups) == 0 {
		return
	}
	output.Bold(title)
	table := NewTable(output, "Group", "Orders", "Filled", "Fill", "Partial", "Rejected", "Blocked", "Protect", "Avg Slip", "Cost")
	for _, group := range groups {
		table.AddRow(
			group.Key,
			fmt.Sprintf("%d", group.TotalOrders),
			fmt.Sprintf("%d", group.FilledOrders),
			fmt.Sprintf("%.1f%%", group.FillRate),
			fmt.Sprintf("%d", group.PartialFills),
			fmt.Sprintf("%d", group.RejectedOrders),
			fmt.Sprintf("%d", group.BlockedExecutions),
			fmt.Sprintf("%d", group.ProtectiveFailures),
			fmt.Sprintf("%.1f bp", group.AvgSlippageBp),
			fmt.Sprintf("%.1f bp", group.CostBp),
		)
	}
	table.Render()
	output.Println()
}

func printHighSlippage(output *Output, samples []models.ExecutionQualitySample) {
	if len(samples) == 0 {
		return
	}
	output.Bold("High Slippage Orders")
	table := NewTable(output, "Time", "Order", "Symbol", "Side", "Type", "Filled", "Expected", "Actual", "Slip")
	for _, sample := range samples {
		table.AddRow(
			FormatDateTime(sample.Timestamp),
			sample.OrderID,
			sample.Symbol,
			sample.Side,
			sample.OrderType,
			fmt.Sprintf("%d/%d", sample.FilledQty, sample.Quantity),
			FormatPrice(sample.Expected),
			FormatPrice(sample.Actual),
			fmt.Sprintf("%.1f bp", sample.SlippageBp),
		)
	}
	table.Render()
	output.Println()
}

func printExecutionIssues(output *Output, issues []models.ExecutionQualityIssue) {
	if len(issues) == 0 {
		return
	}
	output.Bold("Recent Execution Issues")
	table := NewTable(output, "Time", "Symbol", "Action", "Stage", "Status", "Message")
	for _, issue := range issues {
		table.AddRow(
			FormatDateTime(issue.Timestamp),
			issue.Symbol,
			issue.Action,
			issue.Stage,
			issue.Status,
			truncateString(issue.Message, 60),
		)
	}
	table.Render()
	output.Println()
}
