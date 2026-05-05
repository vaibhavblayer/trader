package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/models"
	"zerodha-trader/internal/store"
)

func newCalibrationCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "calibration",
		Short: "Historical setup and gate calibration reports",
		Long:  "Report expectancy and win rate by setup, gate, symbol, timeframe, and action from paper prediction outcomes.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCalibrationReport(cmd, app)
		},
	}
	reportCmd := &cobra.Command{
		Use:   "report",
		Short: "Show historical calibration report",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCalibrationReport(cmd, app)
		},
	}
	addCalibrationFlags(cmd)
	addCalibrationFlags(reportCmd)
	cmd.AddCommand(reportCmd)
	return cmd
}

func addCalibrationFlags(cmd *cobra.Command) {
	cmd.Flags().Int("days", 90, "Number of recent days to analyze")
	cmd.Flags().String("symbol", "", "Filter by symbol")
	cmd.Flags().String("setup", "", "Filter by setup name")
	cmd.Flags().String("timeframe", "", "Filter by prediction timeframe")
}

func runCalibrationReport(cmd *cobra.Command, app *App) error {
	output := NewOutput(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if app.Store == nil {
		output.Warning("Calibration store is not available")
		return nil
	}

	days, _ := cmd.Flags().GetInt("days")
	symbol, _ := cmd.Flags().GetString("symbol")
	setupName, _ := cmd.Flags().GetString("setup")
	timeframe, _ := cmd.Flags().GetString("timeframe")
	filter := store.PaperPredictionFilter{
		Symbol:    strings.ToUpper(strings.TrimSpace(symbol)),
		SetupName: strings.TrimSpace(setupName),
		Timeframe: strings.TrimSpace(timeframe),
	}
	if days > 0 {
		filter.StartDate = time.Now().AddDate(0, 0, -days)
		filter.EndDate = time.Now()
	}

	report, err := app.Store.GetHistoricalCalibrationReport(ctx, filter)
	if err != nil {
		output.Error("Failed to get calibration report: %v", err)
		return err
	}
	if output.IsJSON() {
		return output.JSON(report)
	}
	if report.TotalPredictions == 0 {
		output.Info("No calibration samples found")
		return nil
	}

	output.Bold("Historical Calibration")
	if days > 0 {
		output.Printf("  Period: last %d days\n", days)
	}
	if filter.Symbol != "" {
		output.Printf("  Symbol: %s\n", filter.Symbol)
	}
	if filter.SetupName != "" {
		output.Printf("  Setup: %s\n", filter.SetupName)
	}
	if filter.Timeframe != "" {
		output.Printf("  Timeframe: %s\n", filter.Timeframe)
	}
	output.Println()

	output.Printf("  Samples:     %d total | %d evaluated | %d decisive\n", report.TotalPredictions, report.Evaluated, report.Decisive)
	output.Printf("  Win Rate:    %.1f%%\n", report.WinRate)
	output.Printf("  Avg Conf:    %.1f%%\n", report.AvgConfidence)
	output.Printf("  Avg P&L:     %.2f%%\n", report.AvgPnLPercent)
	output.Printf("  Expectancy:  %.2f%% per decisive prediction\n", report.Expectancy)
	output.Println()

	printCalibrationGroups(output, "By Setup", report.BySetup)
	printCalibrationGroups(output, "By Gate", report.ByGate)
	printCalibrationGroups(output, "By Symbol", report.BySymbol)
	printCalibrationGroups(output, "By Timeframe", report.ByTimeframe)
	printCalibrationGroups(output, "By Action", report.ByAction)
	return nil
}

func printCalibrationGroups(output *Output, title string, groups []models.CalibrationGroupStats) {
	if len(groups) == 0 {
		return
	}
	output.Bold(title)
	table := NewTable(output, "Group", "Total", "Eval", "Decisive", "Win", "Avg Conf", "Avg P&L", "Expectancy")
	for _, group := range groups {
		table.AddRow(
			group.Key,
			fmt.Sprintf("%d", group.TotalPredictions),
			fmt.Sprintf("%d", group.Evaluated),
			fmt.Sprintf("%d", group.Decisive),
			fmt.Sprintf("%.1f%%", group.WinRate),
			fmt.Sprintf("%.1f%%", group.AvgConfidence),
			fmt.Sprintf("%.2f%%", group.AvgPnLPercent),
			fmt.Sprintf("%.2f%%", group.Expectancy),
		)
	}
	table.Render()
	output.Println()
}
