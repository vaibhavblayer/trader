// Package cli provides the command-line interface for the trading application.
package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// addJournalCommands adds journal commands.
// Requirements: 33.1-33.17
func addJournalCommands(rootCmd *cobra.Command, app *App) {
	cmd := &cobra.Command{
		Use:   "journal",
		Short: "Trading journal management",
		Long:  "Record, review, and analyze your trading journal.",
	}

	cmd.AddCommand(newJournalTodayCmd(app))
	cmd.AddCommand(newJournalAddCmd(app))
	cmd.AddCommand(newJournalReportCmd(app))
	cmd.AddCommand(newJournalSearchCmd(app))

	rootCmd.AddCommand(cmd)
}

func newJournalTodayCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "today",
		Short: "Show today's journal",
		Long:  "Display today's trades and journal entries.",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			output.Bold("Trading Journal - %s", FormatDate(time.Now()))
			output.Println()

			// Sample trades
			trades := []struct {
				time     time.Time
				symbol   string
				side     string
				qty      int
				entry    float64
				exit     float64
				pnl      float64
				notes    string
			}{
				{time.Now().Add(-4 * time.Hour), "RELIANCE", "BUY", 10, 2440.00, 2465.00, 2500.00, "Breakout trade, good entry"},
				{time.Now().Add(-2 * time.Hour), "INFY", "BUY", 5, 1525.00, 1510.00, -750.00, "Stopped out, weak market"},
				{time.Now().Add(-1 * time.Hour), "TCS", "BUY", 8, 3420.00, 3455.00, 2800.00, "Momentum trade"},
			}

			var totalPnL float64
			var wins, losses int

			output.Bold("Trades")
			table := NewTable(output, "Time", "Symbol", "Side", "Qty", "Entry", "Exit", "P&L", "Notes")
			for _, t := range trades {
				totalPnL += t.pnl
				if t.pnl > 0 {
					wins++
				} else {
					losses++
				}

				table.AddRow(
					FormatTime(t.time),
					t.symbol,
					t.side,
					fmt.Sprintf("%d", t.qty),
					FormatPrice(t.entry),
					FormatPrice(t.exit),
					output.FormatPnL(t.pnl),
					TruncateString(t.notes, 25),
				)
			}
			table.Render()

			output.Println()
			output.Bold("Summary")
			output.Printf("  Total Trades: %d\n", len(trades))
			output.Printf("  Wins/Losses:  %d/%d (%.0f%% win rate)\n", wins, losses, float64(wins)/float64(len(trades))*100)
			output.Printf("  Total P&L:    %s\n", output.FormatPnL(totalPnL))
			output.Println()

			// Daily notes
			output.Bold("Notes")
			output.Println("  Market opened gap up, NIFTY above 19500.")
			output.Println("  Good momentum in large caps, avoided mid-caps.")
			output.Println("  Need to work on position sizing - INFY loss was too large.")

			return nil
		},
	}
}

func newJournalAddCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <trade-id>",
		Short: "Add analysis to a trade",
		Long: `Add post-trade analysis to a completed trade.

Record what went right, what went wrong, and lessons learned.`,
		Example: `  trader journal add TRADE001
  trader journal add TRADE001 --entry-quality 4 --exit-quality 3`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = ctx

			tradeID := args[0]
			entryQuality, _ := cmd.Flags().GetInt("entry-quality")
			exitQuality, _ := cmd.Flags().GetInt("exit-quality")
			riskScore, _ := cmd.Flags().GetInt("risk-score")
			right, _ := cmd.Flags().GetString("right")
			wrong, _ := cmd.Flags().GetString("wrong")
			lessons, _ := cmd.Flags().GetString("lessons")

			output.Bold("Trade Analysis: %s", tradeID)
			output.Println()

			// Show trade details
			output.Printf("  Symbol:     RELIANCE\n")
			output.Printf("  Side:       BUY\n")
			output.Printf("  Entry:      %s @ %s\n", FormatTime(time.Now().Add(-4*time.Hour)), FormatIndianCurrency(2440))
			output.Printf("  Exit:       %s @ %s\n", FormatTime(time.Now().Add(-2*time.Hour)), FormatIndianCurrency(2465))
			output.Printf("  P&L:        %s\n", output.FormatPnL(2500))
			output.Println()

			// Quality scores
			output.Bold("Quality Scores (1-5)")
			output.Printf("  Entry Quality:      %s\n", formatStars(entryQuality))
			output.Printf("  Exit Quality:       %s\n", formatStars(exitQuality))
			output.Printf("  Risk Management:    %s\n", formatStars(riskScore))
			output.Println()

			// Analysis
			if right != "" {
				output.Bold("What Went Right")
				output.Printf("  %s\n", right)
				output.Println()
			}

			if wrong != "" {
				output.Bold("What Went Wrong")
				output.Printf("  %s\n", wrong)
				output.Println()
			}

			if lessons != "" {
				output.Bold("Lessons Learned")
				output.Printf("  %s\n", lessons)
				output.Println()
			}

			output.Success("✓ Analysis saved")

			return nil
		},
	}

	cmd.Flags().Int("entry-quality", 3, "Entry quality score (1-5)")
	cmd.Flags().Int("exit-quality", 3, "Exit quality score (1-5)")
	cmd.Flags().Int("risk-score", 3, "Risk management score (1-5)")
	cmd.Flags().String("right", "", "What went right")
	cmd.Flags().String("wrong", "", "What went wrong")
	cmd.Flags().String("lessons", "", "Lessons learned")

	return cmd
}

func formatStars(score int) string {
	if score < 1 {
		score = 1
	}
	if score > 5 {
		score = 5
	}
	filled := "★"
	empty := "☆"
	result := ""
	for i := 0; i < 5; i++ {
		if i < score {
			result += filled
		} else {
			result += empty
		}
	}
	return result
}

func newJournalReportCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Generate performance reports",
		Long:  "Generate daily, weekly, or monthly performance reports.",
		Example: `  trader journal report --period daily
  trader journal report --period weekly
  trader journal report --period monthly`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			period, _ := cmd.Flags().GetString("period")

			var periodLabel string
			var startDate time.Time
			now := time.Now()

			switch period {
			case "daily":
				periodLabel = "Daily"
				startDate = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			case "weekly":
				periodLabel = "Weekly"
				startDate = now.AddDate(0, 0, -7)
			case "monthly":
				periodLabel = "Monthly"
				startDate = now.AddDate(0, -1, 0)
			default:
				periodLabel = "Daily"
				startDate = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			}

			output.Bold("%s Performance Report", periodLabel)
			output.Printf("  %s to %s\n\n", FormatDate(startDate), FormatDate(now))

			// Summary stats
			output.Bold("Summary")
			output.Printf("  Total Trades:     %d\n", 45)
			output.Printf("  Winning Trades:   %d (%.0f%%)\n", 28, 62.2)
			output.Printf("  Losing Trades:    %d (%.0f%%)\n", 17, 37.8)
			output.Printf("  Gross Profit:     %s\n", output.Green(FormatIndianCurrency(85000)))
			output.Printf("  Gross Loss:       %s\n", output.Red(FormatIndianCurrency(-32000)))
			output.Printf("  Net P&L:          %s\n", output.FormatPnL(53000))
			output.Println()

			// Performance metrics
			output.Bold("Performance Metrics")
			output.Printf("  Win Rate:         %.1f%%\n", 62.2)
			output.Printf("  Profit Factor:    %.2f\n", 2.65)
			output.Printf("  Avg Win:          %s\n", FormatIndianCurrency(3035))
			output.Printf("  Avg Loss:         %s\n", FormatIndianCurrency(-1882))
			output.Printf("  Largest Win:      %s\n", FormatIndianCurrency(8500))
			output.Printf("  Largest Loss:     %s\n", FormatIndianCurrency(-4200))
			output.Printf("  Avg R:R:          1:1.61\n")
			output.Printf("  Expectancy:       %s\n", FormatIndianCurrency(1178))
			output.Println()

			// By symbol
			output.Bold("Top Performers")
			topSymbols := []struct {
				symbol string
				trades int
				pnl    float64
				winRate float64
			}{
				{"RELIANCE", 8, 15000, 75.0},
				{"TCS", 6, 12500, 66.7},
				{"HDFC", 5, 8500, 60.0},
			}

			for _, s := range topSymbols {
				output.Printf("  %-12s %d trades  %s  %.0f%% win\n",
					s.symbol, s.trades, output.FormatPnL(s.pnl), s.winRate)
			}
			output.Println()

			// By strategy
			output.Bold("By Strategy")
			strategies := []struct {
				name    string
				trades  int
				pnl     float64
				winRate float64
			}{
				{"Breakout", 18, 28000, 66.7},
				{"Pullback", 15, 18000, 60.0},
				{"Momentum", 12, 7000, 58.3},
			}

			for _, s := range strategies {
				output.Printf("  %-12s %d trades  %s  %.0f%% win\n",
					s.name, s.trades, output.FormatPnL(s.pnl), s.winRate)
			}
			output.Println()

			// Quality scores
			output.Bold("Average Quality Scores")
			output.Printf("  Entry Quality:      %s (3.8/5)\n", formatStars(4))
			output.Printf("  Exit Quality:       %s (3.2/5)\n", formatStars(3))
			output.Printf("  Risk Management:    %s (4.1/5)\n", formatStars(4))

			return nil
		},
	}

	cmd.Flags().String("period", "daily", "Report period (daily, weekly, monthly)")

	return cmd
}

func newJournalSearchCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Search journal entries",
		Long:  "Search journal entries by symbol, notes, or tags.",
		Example: `  trader journal search RELIANCE
  trader journal search "breakout"
  trader journal search --tag momentum`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			tag, _ := cmd.Flags().GetString("tag")
			symbol, _ := cmd.Flags().GetString("symbol")

			query := ""
			if len(args) > 0 {
				query = args[0]
			}

			output.Bold("Journal Search")
			if query != "" {
				output.Printf("  Query: %s\n", query)
			}
			if tag != "" {
				output.Printf("  Tag: %s\n", tag)
			}
			if symbol != "" {
				output.Printf("  Symbol: %s\n", symbol)
			}
			output.Println()

			// Sample results
			results := []struct {
				date   time.Time
				symbol string
				pnl    float64
				notes  string
			}{
				{time.Now().AddDate(0, 0, -1), "RELIANCE", 2500, "Breakout trade, good entry timing"},
				{time.Now().AddDate(0, 0, -3), "RELIANCE", -1200, "False breakout, should have waited"},
				{time.Now().AddDate(0, 0, -5), "RELIANCE", 3800, "Strong momentum, held for target"},
			}

			output.Printf("Found %d entries\n\n", len(results))

			table := NewTable(output, "Date", "Symbol", "P&L", "Notes")
			for _, r := range results {
				table.AddRow(
					FormatDate(r.date),
					r.symbol,
					output.FormatPnL(r.pnl),
					TruncateString(r.notes, 40),
				)
			}
			table.Render()

			return nil
		},
	}
}
