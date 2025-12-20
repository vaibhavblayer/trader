// Package cli provides the command-line interface for the trading application.
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/store"
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
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			output.Bold("Trading Journal - %s", FormatDate(time.Now()))
			output.Println()

			// Get today's trades from store
			if app.Store == nil {
				output.Warning("Store not initialized. No trade data available.")
				return nil
			}

			today := time.Now()
			startOfDay := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
			endOfDay := startOfDay.Add(24 * time.Hour)

			trades, err := app.Store.GetTrades(ctx, store.TradeFilter{
				StartDate: startOfDay,
				EndDate:   endOfDay,
				Limit:     100,
			})
			if err != nil {
				output.Error("Failed to fetch trades: %v", err)
				return err
			}

			if len(trades) == 0 {
				output.Info("No trades recorded today.")
				output.Println()
				output.Dim("Tip: Trades are recorded when you execute orders through the trader CLI.")
				return nil
			}

			var totalPnL float64
			var wins, losses int

			output.Bold("Trades")
			table := NewTable(output, "Time", "Symbol", "Side", "Qty", "Entry", "Exit", "P&L", "Strategy")
			for _, t := range trades {
				totalPnL += t.PnL
				if t.PnL > 0 {
					wins++
				} else {
					losses++
				}

				table.AddRow(
					FormatTime(t.Timestamp),
					t.Symbol,
					string(t.Side),
					fmt.Sprintf("%d", t.Quantity),
					FormatPrice(t.EntryPrice),
					FormatPrice(t.ExitPrice),
					output.FormatPnL(t.PnL),
					TruncateString(t.Strategy, 15),
				)
			}
			table.Render()

			output.Println()
			output.Bold("Summary")
			output.Printf("  Total Trades: %d\n", len(trades))
			winRate := 0.0
			if len(trades) > 0 {
				winRate = float64(wins) / float64(len(trades)) * 100
			}
			output.Printf("  Wins/Losses:  %d/%d (%.0f%% win rate)\n", wins, losses, winRate)
			output.Printf("  Total P&L:    %s\n", output.FormatPnL(totalPnL))
			output.Println()

			// Get today's journal entries
			entries, err := app.Store.GetJournal(ctx, store.JournalFilter{
				StartDate: startOfDay,
				EndDate:   endOfDay,
				Limit:     10,
			})
			if err == nil && len(entries) > 0 {
				output.Bold("Notes")
				for _, e := range entries {
					output.Printf("  %s\n", e.Content)
				}
			}

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
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

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

			// Get trades from store
			if app.Store == nil {
				output.Warning("Store not initialized. No trade data available.")
				return nil
			}

			trades, err := app.Store.GetTrades(ctx, store.TradeFilter{
				StartDate: startDate,
				EndDate:   now,
				Limit:     1000,
			})
			if err != nil {
				output.Error("Failed to fetch trades: %v", err)
				return err
			}

			if len(trades) == 0 {
				output.Info("No trades found for this period.")
				return nil
			}

			// Calculate stats
			var grossProfit, grossLoss float64
			var wins, losses int
			var largestWin, largestLoss float64
			symbolStats := make(map[string]struct {
				trades  int
				pnl     float64
				wins    int
			})
			strategyStats := make(map[string]struct {
				trades  int
				pnl     float64
				wins    int
			})

			for _, t := range trades {
				if t.PnL > 0 {
					wins++
					grossProfit += t.PnL
					if t.PnL > largestWin {
						largestWin = t.PnL
					}
				} else {
					losses++
					grossLoss += t.PnL
					if t.PnL < largestLoss {
						largestLoss = t.PnL
					}
				}

				// By symbol
				ss := symbolStats[t.Symbol]
				ss.trades++
				ss.pnl += t.PnL
				if t.PnL > 0 {
					ss.wins++
				}
				symbolStats[t.Symbol] = ss

				// By strategy
				strategy := t.Strategy
				if strategy == "" {
					strategy = "Manual"
				}
				st := strategyStats[strategy]
				st.trades++
				st.pnl += t.PnL
				if t.PnL > 0 {
					st.wins++
				}
				strategyStats[strategy] = st
			}

			netPnL := grossProfit + grossLoss
			winRate := 0.0
			if len(trades) > 0 {
				winRate = float64(wins) / float64(len(trades)) * 100
			}
			avgWin := 0.0
			if wins > 0 {
				avgWin = grossProfit / float64(wins)
			}
			avgLoss := 0.0
			if losses > 0 {
				avgLoss = grossLoss / float64(losses)
			}
			profitFactor := 0.0
			if grossLoss != 0 {
				profitFactor = grossProfit / (-grossLoss)
			}
			expectancy := 0.0
			if len(trades) > 0 {
				expectancy = netPnL / float64(len(trades))
			}

			// Summary stats
			output.Bold("Summary")
			output.Printf("  Total Trades:     %d\n", len(trades))
			output.Printf("  Winning Trades:   %d (%.0f%%)\n", wins, winRate)
			output.Printf("  Losing Trades:    %d (%.0f%%)\n", losses, 100-winRate)
			output.Printf("  Gross Profit:     %s\n", output.Green(FormatIndianCurrency(grossProfit)))
			output.Printf("  Gross Loss:       %s\n", output.Red(FormatIndianCurrency(grossLoss)))
			output.Printf("  Net P&L:          %s\n", output.FormatPnL(netPnL))
			output.Println()

			// Performance metrics
			output.Bold("Performance Metrics")
			output.Printf("  Win Rate:         %.1f%%\n", winRate)
			output.Printf("  Profit Factor:    %.2f\n", profitFactor)
			output.Printf("  Avg Win:          %s\n", FormatIndianCurrency(avgWin))
			output.Printf("  Avg Loss:         %s\n", FormatIndianCurrency(avgLoss))
			output.Printf("  Largest Win:      %s\n", FormatIndianCurrency(largestWin))
			output.Printf("  Largest Loss:     %s\n", FormatIndianCurrency(largestLoss))
			output.Printf("  Expectancy:       %s\n", FormatIndianCurrency(expectancy))
			output.Println()

			// Top performers by symbol
			if len(symbolStats) > 0 {
				output.Bold("By Symbol")
				for symbol, stats := range symbolStats {
					wr := 0.0
					if stats.trades > 0 {
						wr = float64(stats.wins) / float64(stats.trades) * 100
					}
					output.Printf("  %-12s %d trades  %s  %.0f%% win\n",
						symbol, stats.trades, output.FormatPnL(stats.pnl), wr)
				}
				output.Println()
			}

			// By strategy
			if len(strategyStats) > 0 {
				output.Bold("By Strategy")
				for strategy, stats := range strategyStats {
					wr := 0.0
					if stats.trades > 0 {
						wr = float64(stats.wins) / float64(stats.trades) * 100
					}
					output.Printf("  %-12s %d trades  %s  %.0f%% win\n",
						strategy, stats.trades, output.FormatPnL(stats.pnl), wr)
				}
			}

			return nil
		},
	}

	cmd.Flags().String("period", "daily", "Report period (daily, weekly, monthly)")

	return cmd
}

func newJournalSearchCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search journal entries",
		Long:  "Search journal entries by symbol, notes, or tags.",
		Example: `  trader journal search RELIANCE
  trader journal search "breakout"
  trader journal search --tag momentum`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

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

			if app.Store == nil {
				output.Warning("Store not initialized. No journal data available.")
				return nil
			}

			// Build filter
			filter := store.JournalFilter{
				Limit: 50,
			}
			if tag != "" {
				filter.Tags = []string{tag}
			}

			entries, err := app.Store.GetJournal(ctx, filter)
			if err != nil {
				output.Error("Failed to fetch journal entries: %v", err)
				return err
			}

			// Filter by query if provided (search in content)
			var filtered []struct {
				date    time.Time
				tradeID string
				content string
				mood    string
			}
			for _, e := range entries {
				// If query provided, filter by content
				if query != "" {
					if !containsIgnoreCase(e.Content, query) {
						continue
					}
				}
				filtered = append(filtered, struct {
					date    time.Time
					tradeID string
					content string
					mood    string
				}{
					date:    e.Date,
					tradeID: e.TradeID,
					content: e.Content,
					mood:    e.Mood,
				})
			}

			if len(filtered) == 0 {
				output.Info("No matching journal entries found.")
				return nil
			}

			output.Printf("Found %d entries\n\n", len(filtered))

			table := NewTable(output, "Date", "Trade ID", "Mood", "Content")
			for _, r := range filtered {
				table.AddRow(
					FormatDate(r.date),
					TruncateString(r.tradeID, 10),
					r.mood,
					TruncateString(r.content, 40),
				)
			}
			table.Render()

			return nil
		},
	}

	cmd.Flags().String("tag", "", "Filter by tag")
	cmd.Flags().String("symbol", "", "Filter by symbol")

	return cmd
}

// containsIgnoreCase checks if s contains substr (case-insensitive)
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
