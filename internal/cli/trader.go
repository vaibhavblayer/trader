// Package cli provides the command-line interface for the trading application.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"zerodha-trader/internal/agents"
	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
	"zerodha-trader/internal/store"
)

// addTraderCommands adds autonomous trading commands.
// Requirements: 26, 62.6, 65.21-65.26
func addTraderCommands(rootCmd *cobra.Command, app *App) {
	cmd := &cobra.Command{
		Use:   "trader",
		Short: "Autonomous trading daemon control",
		Long:  "Start, stop, and manage the autonomous trading daemon.",
	}

	cmd.AddCommand(newTraderStartCmd(app))
	cmd.AddCommand(newTraderStopCmd(app))
	cmd.AddCommand(newTraderStatusCmd(app))
	cmd.AddCommand(newTraderPauseCmd(app))
	cmd.AddCommand(newTraderResumeCmd(app))
	cmd.AddCommand(newTraderDecisionsCmd(app))
	cmd.AddCommand(newTraderConfigCmd(app))
	cmd.AddCommand(newTraderHealthCmd(app))

	rootCmd.AddCommand(cmd)

	// Also add standalone decisions command at root level for easier access
	// Requirements: 63.1-63.4
	rootCmd.AddCommand(newDecisionsCmd(app))
}

// newDecisionsCmd creates a standalone decisions command at root level.
// Requirements: 63.1-63.4
func newDecisionsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "decisions",
		Short: "View AI trading decisions",
		Long: `Display AI trading decisions with full transparency.

This command provides access to:
- Recent decision history with outcomes
- Detailed view of individual decisions
- AI performance statistics and metrics
- Agent accuracy tracking`,
		Example: `  trader decisions list
  trader decisions show <decision-id>
  trader decisions stats --days 30`,
	}

	// Reuse the same subcommands from trader decisions
	decisionsCmd := newTraderDecisionsCmd(app)
	for _, subCmd := range decisionsCmd.Commands() {
		cmd.AddCommand(subCmd)
	}

	return cmd
}

func newTraderStartCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the autonomous trading daemon",
		Long: `Start the autonomous trading daemon.

The daemon will:
- Monitor watchlist symbols
- Run AI agents for analysis
- Execute trades based on confidence thresholds
- Send notifications for all actions`,
		Example: `  trader trader start
  trader trader start --dry-run
  trader trader start --watchlist momentum`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			watchlist, _ := cmd.Flags().GetString("watchlist")
			interval, _ := cmd.Flags().GetInt("interval")

			output.Bold("Starting Autonomous Trading Daemon")
			output.Println()

			// Validate prerequisites
			if app.Broker == nil {
				output.Error("Broker not initialized. Please run 'trader auth login' first.")
				return fmt.Errorf("broker not initialized")
			}

			if !app.Broker.IsAuthenticated() {
				output.Error("Not authenticated. Please run 'trader auth login' first.")
				return fmt.Errorf("not authenticated")
			}

			// Display configuration
			output.Printf("  Mode:             %s\n", app.Config.Agents.AutonomousMode)
			output.Printf("  Safety Profile:   %s\n", app.Config.SafetyProfile())
			output.Printf("  Confidence:       %.0f%%\n", app.Config.Agents.AutoExecuteThreshold)
			output.Printf("  Max Daily Trades: %d\n", app.Config.Agents.MaxDailyTrades)
			output.Printf("  Max Daily Loss:   %s\n", FormatIndianCurrency(app.Config.Agents.MaxDailyLoss))
			output.Printf("  Cooldown:         %d min\n", app.Config.Agents.CooldownMinutes)
			output.Printf("  Scan Interval:    %d sec\n", interval)
			if watchlist != "" {
				output.Printf("  Watchlist:        %s\n", watchlist)
			}
			output.Println()

			if dryRun {
				output.Warning("🔍 DRY RUN MODE - No actual trades will be executed")
				output.Println()
			}
			if !app.Config.SafetyCapabilities().AutoTrade {
				output.Warning("Safety profile blocks autonomous execution; daemon will analyze only")
				output.Println()
			}

			if app.Config.IsPaperMode() {
				output.Warning("📝 PAPER TRADING MODE")
				output.Println()
			}

			// Get watchlist symbols
			symbols, err := getWatchlistSymbols(app, watchlist)
			if err != nil {
				output.Error("Failed to get watchlist: %v", err)
				return err
			}
			output.Printf("  Monitoring %d symbols\n", len(symbols))
			output.Println()

			// Create orchestrator with agents
			orchestrator := createOrchestrator(app)

			// Start the daemon
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle graceful shutdown
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

			go func() {
				<-sigChan
				output.Println()
				output.Info("Shutting down daemon...")
				cancel()
			}()

			if err := orchestrator.Start(ctx); err != nil {
				output.Error("Failed to start orchestrator: %v", err)
				return err
			}

			output.Success("✓ Daemon started")
			output.Println()
			output.Dim("Press Ctrl+C to stop")
			output.Println()

			// Main trading loop
			ticker := time.NewTicker(time.Duration(interval) * time.Second)
			defer ticker.Stop()

			scanCount := 0
			for {
				select {
				case <-ctx.Done():
					output.Info("Daemon stopped")
					return nil
				case <-ticker.C:
					scanCount++
					output.Dim("[%s] Scan #%d - Analyzing %d symbols...",
						time.Now().Format("15:04:05"), scanCount, len(symbols))

					// Process each symbol
					for _, symbol := range symbols {
						decision, err := processSymbol(ctx, app, orchestrator, symbol, dryRun)
						if err != nil {
							output.Dim("  %s: error - %v", symbol, err)
							continue
						}

						if decision == nil {
							continue
						}

						// Display decision
						displayDecision(output, decision, dryRun)

						// Execute if approved
						if decision.Executed && !dryRun {
							executeDecision(ctx, app, output, decision)
						}
					}
				}
			}
		},
	}

	cmd.Flags().Bool("dry-run", false, "Run without executing trades")
	cmd.Flags().String("watchlist", "default", "Watchlist to monitor")
	cmd.Flags().Int("interval", 60, "Scan interval in seconds")

	return cmd
}

func newTraderStopCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the autonomous trading daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			err := fmt.Errorf("persistent daemon control is not implemented; stop the foreground trader process with Ctrl+C or your process manager")
			output.Error("%v", err)
			return err
		},
	}
}

func newTraderStatusCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			status := struct {
				Tracked               bool     `json:"tracked"`
				Mode                  string   `json:"mode"`
				SafetyProfile         string   `json:"safety_profile"`
				AutoTradeAllowed      bool     `json:"auto_trade_allowed"`
				LLMOrderAuthority     bool     `json:"llm_order_authority"`
				AutoExecuteThreshold  float64  `json:"auto_execute_threshold"`
				MaxDailyTrades        int      `json:"max_daily_trades"`
				MaxDailyLoss          float64  `json:"max_daily_loss"`
				ConsecutiveLossLimit  int      `json:"consecutive_loss_limit"`
				EnabledAgents         []string `json:"enabled_agents"`
				PersistentStateStatus string   `json:"persistent_state_status"`
			}{
				Tracked:               false,
				Mode:                  app.Config.Agents.AutonomousMode,
				SafetyProfile:         app.Config.SafetyProfile(),
				AutoTradeAllowed:      app.Config.SafetyCapabilities().AutoTrade,
				LLMOrderAuthority:     app.Config.SafetyCapabilities().LLMOrderAuthority,
				AutoExecuteThreshold:  app.Config.Agents.AutoExecuteThreshold,
				MaxDailyTrades:        app.Config.Agents.MaxDailyTrades,
				MaxDailyLoss:          app.Config.Agents.MaxDailyLoss,
				ConsecutiveLossLimit:  app.Config.Agents.ConsecutiveLossLimit,
				EnabledAgents:         app.Config.Agents.EnabledAgents,
				PersistentStateStatus: "not implemented",
			}

			if output.IsJSON() {
				return output.JSON(status)
			}

			output.Bold("Autonomous Trading Daemon Status")
			output.Println()

			output.Printf("  Runtime State: %s\n", output.Yellow("not tracked by this command"))
			output.Printf("  Mode:          %s\n", status.Mode)
			output.Printf("  Safety Profile: %s\n", status.SafetyProfile)
			output.Printf("  Profile Auto Trading: %s\n", formatBoolStatus(output, status.AutoTradeAllowed))
			output.Printf("  Profile LLM Orders:   %s\n", formatBoolStatus(output, status.LLMOrderAuthority))
			output.Println()

			output.Bold("Configured Limits")
			output.Printf("  Confidence:   %.0f%%\n", status.AutoExecuteThreshold)
			output.Printf("  Daily Trades: %d\n", status.MaxDailyTrades)
			output.Printf("  Daily Loss:   %s\n", FormatIndianCurrency(status.MaxDailyLoss))
			output.Printf("  Loss Streak:  %d\n", status.ConsecutiveLossLimit)
			output.Println()

			output.Bold("Enabled Agents")
			for _, agent := range status.EnabledAgents {
				output.Printf("  • %s\n", agent)
			}
			output.Println()
			output.Dim("Persistent daemon runtime state is not available yet; this command reports configuration only.")

			return nil
		},
	}
}

func formatBoolStatus(output *Output, enabled bool) string {
	if enabled {
		return output.Green("enabled")
	}
	return output.Yellow("disabled")
}

func formatPassStatus(output *Output, passed bool) string {
	if passed {
		return output.Green("PASS")
	}
	return output.Yellow("WARN")
}

func uniqueDecisionDisplayIDs(decisions []models.Decision) map[string]string {
	const minPrefixLen = 12

	result := make(map[string]string, len(decisions))
	for _, decision := range decisions {
		id := decision.ID
		prefixLen := minPrefixLen
		if prefixLen > len(id) {
			prefixLen = len(id)
		}

		for prefixLen < len(id) {
			prefix := id[:prefixLen]
			unique := true
			for _, other := range decisions {
				if other.ID != id && strings.HasPrefix(other.ID, prefix) {
					unique = false
					break
				}
			}
			if unique {
				break
			}
			prefixLen++
		}
		result[id] = id[:prefixLen]
	}

	return result
}

func resolveDecisionID(ctx context.Context, dataStore store.DataStore, input string) (string, error) {
	if dataStore == nil {
		return "", fmt.Errorf("database not initialized")
	}

	decision, err := dataStore.GetDecisionByID(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to get decision: %w", err)
	}
	if decision != nil {
		return decision.ID, nil
	}

	decisions, err := dataStore.GetDecisions(ctx, store.DecisionFilter{Limit: 1000})
	if err != nil {
		return "", fmt.Errorf("failed to search decisions: %w", err)
	}

	matches := make([]string, 0, 4)
	for _, decision := range decisions {
		if strings.HasPrefix(decision.ID, input) {
			matches = append(matches, decision.ID)
		}
	}

	switch len(matches) {
	case 0:
		return input, nil
	case 1:
		return matches[0], nil
	default:
		if len(matches) > 5 {
			matches = append(matches[:5], fmt.Sprintf("...and %d more", len(matches)-5))
		}
		return "", fmt.Errorf("decision ID prefix %q is ambiguous; matches: %s", input, strings.Join(matches, ", "))
	}
}

func newTraderPauseCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "pause",
		Short: "Pause the trading daemon",
		Long:  "Pause trading without stopping the daemon. Analysis continues but no trades are executed.",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			err := fmt.Errorf("persistent daemon pause/resume state is not implemented yet")
			output.Error("%v", err)
			return err
		},
	}
}

func newTraderResumeCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: "Resume the trading daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			err := fmt.Errorf("persistent daemon pause/resume state is not implemented yet")
			output.Error("%v", err)
			return err
		},
	}
}

func newTraderDecisionsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "decisions",
		Short: "View AI trading decisions",
		Long:  "Display recent AI trading decisions with reasoning.",
	}

	// decisions list command
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List recent decisions",
		Long: `List recent AI trading decisions with outcomes.

Shows timestamp, symbol, action, confidence, execution status, and P&L for each decision.`,
		Example: `  trader decisions list
  trader decisions list --limit 20
  trader decisions list --symbol RELIANCE
  trader decisions list --executed
  trader decisions list --days 7`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Check if store is available
			if app.Store == nil {
				output.Error("Database not initialized. Please check your configuration.")
				return fmt.Errorf("store not initialized")
			}

			// Get filter options
			limit, _ := cmd.Flags().GetInt("limit")
			symbol, _ := cmd.Flags().GetString("symbol")
			executedOnly, _ := cmd.Flags().GetBool("executed")
			days, _ := cmd.Flags().GetInt("days")

			// Build filter
			filter := store.DecisionFilter{
				Symbol: symbol,
				Limit:  limit,
			}
			if executedOnly {
				executed := true
				filter.Executed = &executed
			}
			if days > 0 {
				filter.StartDate = time.Now().AddDate(0, 0, -days)
				filter.EndDate = time.Now()
			}

			// Get decisions from store
			decisions, err := app.Store.GetDecisions(ctx, filter)
			if err != nil {
				output.Error("Failed to get decisions: %v", err)
				return err
			}

			if output.IsJSON() {
				return output.JSON(decisions)
			}

			if len(decisions) == 0 {
				output.Info("No decisions found")
				return nil
			}

			output.Bold("Recent AI Decisions %s", output.SourceTag(SourceAI))
			output.Println()

			displayIDs := uniqueDecisionDisplayIDs(decisions)
			table := NewTable(output, "ID", "Time", "Symbol", "Action", "Confidence", "Executed", "Outcome", "P&L")
			for _, d := range decisions {
				actionColor := ColorYellow
				if d.Action == "BUY" {
					actionColor = ColorGreen
				} else if d.Action == "SELL" {
					actionColor = ColorRed
				}

				executed := output.Red("✗")
				if d.Executed {
					executed = output.Green("✓")
				}

				outcome := string(d.Outcome)
				outcomeColor := ColorYellow
				if d.Outcome == models.OutcomeWin {
					outcomeColor = ColorGreen
				} else if d.Outcome == models.OutcomeLoss {
					outcomeColor = ColorRed
				}

				pnl := "-"
				if d.Executed && d.Outcome != models.OutcomePending {
					pnl = output.FormatPnL(d.PnL)
				}

				table.AddRow(
					displayIDs[d.ID],
					FormatTime(d.Timestamp),
					d.Symbol,
					output.ColoredString(actionColor, d.Action),
					FormatConfidence(d.Confidence),
					executed,
					output.ColoredString(outcomeColor, outcome),
					pnl,
				)
			}
			table.Render()

			output.Println()
			output.Dim("Use 'trader decisions show <id>' for full details; shown IDs are unique prefixes.")

			return nil
		},
	}
	listCmd.Flags().Int("limit", 10, "Maximum number of decisions to show")
	listCmd.Flags().String("symbol", "", "Filter by symbol")
	listCmd.Flags().Bool("executed", false, "Show only executed decisions")
	listCmd.Flags().Int("days", 0, "Filter by number of days")
	cmd.AddCommand(listCmd)

	// decisions show command
	showCmd := &cobra.Command{
		Use:   "show <decision-id>",
		Short: "Show decision details",
		Long: `Show full details of a specific AI trading decision.

Displays:
- Decision metadata (timestamp, symbol, action, confidence)
- Entry price, stop loss, and targets
- Individual agent recommendations and reasoning
- Consensus calculation details
- Risk assessment results
- Execution status and outcome`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			decisionID := args[0]
			resolvedID, err := resolveDecisionID(ctx, app.Store, decisionID)
			if err != nil {
				output.Error("%v", err)
				return nil
			}

			// Get decision from store
			decision, err := app.Store.GetDecisionByID(ctx, resolvedID)
			if err != nil {
				output.Error("Failed to get decision: %v", err)
				return err
			}
			if decision == nil {
				output.Error("Decision not found: %s", decisionID)
				return nil
			}
			logs, err := app.Store.GetDecisionLogs(ctx, resolvedID)
			if err != nil {
				output.Warning("Failed to get decision logs: %v", err)
			}

			if output.IsJSON() {
				return output.JSON(map[string]interface{}{
					"decision": decision,
					"logs":     logs,
				})
			}

			// Display decision header
			output.Bold("Decision: %s", decision.ID)
			output.Println()

			// Basic info
			output.Printf("  Timestamp:        %s\n", FormatDateTime(decision.Timestamp))
			output.Printf("  Symbol:           %s\n", decision.Symbol)

			actionColor := ColorYellow
			if decision.Action == "BUY" {
				actionColor = ColorGreen
			} else if decision.Action == "SELL" {
				actionColor = ColorRed
			}
			output.Printf("  Action:           %s\n", output.ColoredString(actionColor, decision.Action))
			output.Printf("  Confidence:       %.1f%%\n", decision.Confidence)

			if decision.MarketCondition != "" {
				output.Printf("  Market Condition: %s\n", decision.MarketCondition)
			}
			output.Println()

			// Trade parameters
			if decision.EntryPrice > 0 {
				output.Bold("Trade Parameters")
				output.Printf("  Entry Price:  %s\n", FormatIndianCurrency(decision.EntryPrice))
				if decision.StopLoss > 0 {
					output.Printf("  Stop Loss:    %s\n", FormatIndianCurrency(decision.StopLoss))
				}
				if len(decision.Targets) > 0 {
					for i, target := range decision.Targets {
						output.Printf("  Target %d:     %s\n", i+1, FormatIndianCurrency(target))
					}
				}
				// Calculate R:R if we have entry and stop loss
				if decision.StopLoss > 0 && len(decision.Targets) > 0 {
					risk := decision.EntryPrice - decision.StopLoss
					if risk < 0 {
						risk = -risk
					}
					reward := decision.Targets[0] - decision.EntryPrice
					if reward < 0 {
						reward = -reward
					}
					if risk > 0 {
						rr := reward / risk
						output.Printf("  R:R Ratio:    1:%.2f\n", rr)
					}
				}
				output.Println()
			}

			// Agent recommendations
			if len(decision.AgentResults) > 0 {
				output.Bold("Agent Recommendations")
				for agentName, result := range decision.AgentResults {
					if result == nil {
						continue
					}
					recColor := ColorYellow
					if result.Recommendation == "BUY" || result.Recommendation == "APPROVED" {
						recColor = ColorGreen
					} else if result.Recommendation == "SELL" || result.Recommendation == "REJECTED" {
						recColor = ColorRed
					}
					output.Printf("  %-12s %s (%.0f%%)\n", agentName, output.ColoredString(recColor, result.Recommendation), result.Confidence)
					if result.Reasoning != "" {
						// Wrap reasoning text
						reasoning := result.Reasoning
						if len(reasoning) > 60 {
							reasoning = reasoning[:60] + "..."
						}
						output.Printf("               %s\n", output.DimText(reasoning))
					}
				}
				output.Println()
			}

			// Consensus details
			if decision.Consensus != nil {
				output.Bold("Consensus")
				output.Printf("  Total Agents:    %d\n", decision.Consensus.TotalAgents)
				output.Printf("  Agreeing Agents: %d\n", decision.Consensus.AgreeingAgents)
				output.Printf("  Weighted Score:  %.1f\n", decision.Consensus.WeightedScore)
				if decision.Consensus.Calculation != "" {
					output.Printf("  Calculation:     %s\n", decision.Consensus.Calculation)
				}
				output.Println()
			}

			// Risk check
			if decision.RiskCheck != nil {
				output.Bold("Risk Assessment")
				if decision.RiskCheck.Approved {
					output.Printf("  Status:          %s\n", output.Green("✓ APPROVED"))
				} else {
					output.Printf("  Status:          %s\n", output.Red("✗ REJECTED"))
				}
				if decision.RiskCheck.PositionSize > 0 {
					output.Printf("  Position Size:   %s\n", FormatIndianCurrency(decision.RiskCheck.PositionSize))
				}
				if decision.RiskCheck.PortfolioImpact > 0 {
					output.Printf("  Portfolio Impact: %.1f%%\n", decision.RiskCheck.PortfolioImpact)
				}
				if decision.RiskCheck.SectorExposure > 0 {
					output.Printf("  Sector Exposure: %.1f%%\n", decision.RiskCheck.SectorExposure)
				}
				if len(decision.RiskCheck.Violations) > 0 {
					output.Printf("  Violations:\n")
					for _, v := range decision.RiskCheck.Violations {
						output.Printf("    • %s\n", output.Red(v))
					}
				}
				output.Println()
			}

			// Execution status
			output.Bold("Execution")
			if decision.Executed {
				output.Printf("  Status:   %s\n", output.Green("✓ EXECUTED"))
				if decision.OrderID != "" {
					output.Printf("  Order ID: %s\n", decision.OrderID)
				}
				outcomeColor := ColorYellow
				if decision.Outcome == models.OutcomeWin {
					outcomeColor = ColorGreen
				} else if decision.Outcome == models.OutcomeLoss {
					outcomeColor = ColorRed
				}
				output.Printf("  Outcome:  %s\n", output.ColoredString(outcomeColor, string(decision.Outcome)))
				if decision.Outcome != models.OutcomePending {
					output.Printf("  P&L:      %s\n", output.FormatPnL(decision.PnL))
				}
			} else {
				output.Printf("  Status:   %s\n", output.Yellow("○ NOT EXECUTED"))
			}
			output.Println()

			if len(logs) > 0 {
				output.Bold("Decision Log")
				for _, log := range logs {
					output.Printf("  %s  %-18s %-12s %s\n",
						FormatTime(log.Timestamp),
						log.Stage,
						log.Status,
						log.Message)
				}
				output.Println()
			}

			// Reasoning
			if decision.Reasoning != "" {
				output.Bold("Reasoning")
				output.Println("  " + decision.Reasoning)
			}

			return nil
		},
	}
	cmd.AddCommand(showCmd)

	// decisions stats command
	statsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Show decision statistics",
		Long: `Show AI decision performance statistics.

Displays:
- Total decisions and executed trades
- Win rate and average P&L
- Average confidence score
- Accuracy by agent
- Performance by market condition`,
		Example: `  trader decisions stats
  trader decisions stats --days 30
  trader decisions stats --days 7`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			days, _ := cmd.Flags().GetInt("days")

			// Build date range
			dateRange := store.DateRange{
				Start: time.Now().AddDate(0, 0, -days),
				End:   time.Now(),
			}

			// Get stats from store
			stats, err := app.Store.GetDecisionStats(ctx, dateRange)
			if err != nil {
				output.Error("Failed to get decision stats: %v", err)
				return err
			}

			if output.IsJSON() {
				return output.JSON(stats)
			}

			output.Bold("AI Decision Statistics")
			output.Printf("  Last %d days\n\n", days)

			// Overall stats
			output.Printf("  Total Decisions:  %d\n", stats.TotalDecisions)
			output.Printf("  Executed Trades:  %d\n", stats.ExecutedTrades)

			winRateColor := ColorYellow
			if stats.WinRate >= 60 {
				winRateColor = ColorGreen
			} else if stats.WinRate < 50 {
				winRateColor = ColorRed
			}
			output.Printf("  Win Rate:         %s\n", output.ColoredString(winRateColor, fmt.Sprintf("%.1f%%", stats.WinRate)))
			output.Printf("  Avg Confidence:   %.1f%%\n", stats.AvgConfidence)
			output.Printf("  Avg P&L:          %s\n", output.FormatPnL(stats.AvgPnL))
			output.Println()

			// Agent accuracy
			if len(stats.ByAgent) > 0 {
				output.Bold("By Agent Accuracy")
				for agentName, agentStats := range stats.ByAgent {
					bar := createBar(int(agentStats.Accuracy), 100, 20)
					output.Printf("  %-12s %s %.1f%% (%d calls)\n", agentName, bar, agentStats.Accuracy, agentStats.TotalCalls)
				}
				output.Println()
			}

			// Market condition stats
			if len(stats.ByMarketCondition) > 0 {
				output.Bold("By Market Condition")
				table := NewTable(output, "Condition", "Trades", "Win Rate", "Avg P&L")
				for _, condStats := range stats.ByMarketCondition {
					winRateStr := fmt.Sprintf("%.1f%%", condStats.WinRate)
					if condStats.WinRate >= 60 {
						winRateStr = output.Green(winRateStr)
					} else if condStats.WinRate < 50 {
						winRateStr = output.Red(winRateStr)
					}
					table.AddRow(
						condStats.Condition,
						fmt.Sprintf("%d", condStats.TotalTrades),
						winRateStr,
						output.FormatPnL(condStats.AvgPnL),
					)
				}
				table.Render()
			}

			return nil
		},
	}
	statsCmd.Flags().Int("days", 30, "Number of days to analyze")
	cmd.AddCommand(statsCmd)

	return cmd
}

func newTraderConfigCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "View/edit trader configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			output.Bold("Trader Configuration")
			output.Println()

			output.Printf("  Model:              %s\n", app.Config.Agents.Model)
			if app.Config.Agents.ReasoningEffort != "" {
				output.Printf("  Reasoning:          %s\n", app.Config.Agents.ReasoningEffort)
			} else {
				output.Printf("  Reasoning:          default\n")
			}
			output.Printf("  Autonomous Mode:    %s\n", app.Config.Agents.AutonomousMode)
			output.Printf("  Auto Threshold:     %.0f%%\n", app.Config.Agents.AutoExecuteThreshold)
			output.Printf("  Max Daily Trades:   %d\n", app.Config.Agents.MaxDailyTrades)
			output.Printf("  Max Daily Loss:     %s\n", FormatIndianCurrency(app.Config.Agents.MaxDailyLoss))
			output.Printf("  Max Position Size:  %s\n", FormatIndianCurrency(app.Config.Agents.MaxPositionSize))
			output.Printf("  Cooldown:           %d min\n", app.Config.Agents.CooldownMinutes)
			output.Printf("  Consec. Loss Limit: %d\n", app.Config.Agents.ConsecutiveLossLimit)
			output.Println()

			output.Bold("Enabled Agents")
			for _, agent := range app.Config.Agents.EnabledAgents {
				weight := app.Config.Agents.AgentWeights[agent]
				output.Printf("  • %-12s (weight: %.2f)\n", agent, weight)
			}
			output.Println()

			output.Dim("Edit ~/.config/zerodha-trader/agents.toml to change settings")

			return nil
		},
	}
}

func newTraderHealthCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "System health diagnostics",
		Long: `Display system health including:
- Local runtime health
- Configuration and database readiness
- Broker and LLM client initialization
- Active safety profile capabilities`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)

			health := struct {
				MemoryMB               uint64 `json:"memory_mb"`
				Goroutines             int    `json:"goroutines"`
				ConfigLoaded           bool   `json:"config_loaded"`
				StoreReady             bool   `json:"store_ready"`
				BrokerInitialized      bool   `json:"broker_initialized"`
				BrokerAuthenticated    bool   `json:"broker_authenticated"`
				LLMClientConfigured    bool   `json:"llm_client_configured"`
				TradingMode            string `json:"trading_mode"`
				SafetyProfile          string `json:"safety_profile"`
				ProfileAutoTrade       bool   `json:"profile_auto_trade"`
				ProfileLLMOrders       bool   `json:"profile_llm_orders"`
				RuntimeStateTracked    bool   `json:"runtime_state_tracked"`
				ExternalServicesProbed bool   `json:"external_services_probed"`
			}{
				MemoryMB:               mem.Alloc / 1024 / 1024,
				Goroutines:             runtime.NumGoroutine(),
				ConfigLoaded:           app.Config != nil,
				StoreReady:             app.Store != nil,
				BrokerInitialized:      app.Broker != nil,
				LLMClientConfigured:    app.LLMClient != nil,
				RuntimeStateTracked:    false,
				ExternalServicesProbed: false,
			}
			if app.Config != nil {
				health.TradingMode = app.Config.Trading.Mode
				health.SafetyProfile = app.Config.SafetyProfile()
				capabilities := app.Config.SafetyCapabilities()
				health.ProfileAutoTrade = capabilities.AutoTrade
				health.ProfileLLMOrders = capabilities.LLMOrderAuthority
			}
			if app.Broker != nil {
				health.BrokerAuthenticated = app.Broker.IsAuthenticated()
			}

			if output.IsJSON() {
				return output.JSON(health)
			}

			output.Bold("System Health")
			output.Println()

			output.Bold("System")
			output.Printf("  Memory:     %d MB\n", health.MemoryMB)
			output.Printf("  Goroutines: %d\n", health.Goroutines)
			output.Println()

			output.Bold("Local Checks")
			output.Printf("  Config Loaded        %s\n", formatPassStatus(output, health.ConfigLoaded))
			output.Printf("  SQLite Store         %s\n", formatPassStatus(output, health.StoreReady))
			output.Printf("  Broker Initialized   %s\n", formatPassStatus(output, health.BrokerInitialized))
			output.Printf("  Broker Authenticated %s\n", formatPassStatus(output, health.BrokerAuthenticated))
			output.Printf("  LLM Client           %s\n", formatPassStatus(output, health.LLMClientConfigured))
			output.Println()

			output.Bold("Safety")
			output.Printf("  Trading Mode:  %s\n", health.TradingMode)
			output.Printf("  Safety Profile: %s\n", health.SafetyProfile)
			output.Printf("  Auto Trading:  %s\n", formatBoolStatus(output, health.ProfileAutoTrade))
			output.Printf("  LLM Orders:    %s\n", formatBoolStatus(output, health.ProfileLLMOrders))
			output.Println()

			output.Dim("External Zerodha/OpenAI/WebSocket connectivity is not probed by this local health command.")
			output.Dim("Persistent daemon runtime state is not available yet.")

			return nil
		},
	}
}

// getWatchlistSymbols retrieves symbols from the specified watchlist.
func getWatchlistSymbols(app *App, watchlistName string) ([]string, error) {
	if app.Store == nil {
		// Return default symbols if no store
		return []string{"RELIANCE", "TCS", "INFY", "HDFCBANK", "ICICIBANK"}, nil
	}

	ctx := context.Background()
	symbols, err := app.Store.GetWatchlist(ctx, watchlistName)
	if err != nil {
		return nil, err
	}

	if len(symbols) == 0 {
		// Return default symbols
		return []string{"RELIANCE", "TCS", "INFY", "HDFCBANK", "ICICIBANK"}, nil
	}

	return symbols, nil
}

// createOrchestrator creates an orchestrator with all enabled agents.
func createOrchestrator(app *App) *agents.Orchestrator {
	var agentList []agents.Agent
	decisionLLM := app.llmForTradeDecisions()

	// Get agent weights from config
	weights := app.Config.Agents.AgentWeights
	if weights == nil {
		weights = map[string]float64{
			"technical": 0.35,
			"research":  0.25,
			"news":      0.15,
			"risk":      0.25,
		}
	}

	// Create enabled agents
	for _, agentName := range app.Config.Agents.EnabledAgents {
		weight := weights[agentName]
		if weight == 0 {
			weight = 0.2 // Default weight
		}

		switch agentName {
		case "technical":
			agentList = append(agentList, agents.NewTechnicalAgent(decisionLLM, weight))
		case "research":
			// WebSearchClient is optional - pass nil for now
			agentList = append(agentList, agents.NewResearchAgent(app.LLMClient, nil, weight))
		case "news":
			// WebSearchClient is optional - pass nil for now
			agentList = append(agentList, agents.NewNewsAgent(app.LLMClient, nil, weight))
		}
	}

	// Create trader and risk agents
	traderAgent := agents.NewTraderAgent(app.LLMClient, weights, 1.0)
	riskAgent := agents.NewRiskAgent(nil, weights["risk"])

	return agents.NewOrchestrator(
		agentList,
		traderAgent,
		riskAgent,
		&app.Config.Agents,
		app.Store,
		nil, // notifier - can be added later
	)
}

func (app *App) llmForTradeDecisions() agents.LLMClient {
	if app == nil || app.Config == nil {
		return nil
	}
	if !app.Config.SafetyCapabilities().LLMOrderAuthority {
		return nil
	}
	return app.LLMClient
}

// processSymbol analyzes a symbol and returns a trading decision.
func processSymbol(ctx context.Context, app *App, orchestrator *agents.Orchestrator, symbol string, dryRun bool) (*models.Decision, error) {
	// Format symbol with exchange prefix for Zerodha API
	fullSymbol := "NSE:" + symbol

	// Get current quote
	quote, err := app.Broker.GetQuote(ctx, fullSymbol)
	if err != nil {
		return nil, fmt.Errorf("failed to get quote: %w", err)
	}

	// Get historical data for analysis
	candles, _, err := app.getQualityHistorical(ctx, broker.HistoricalRequest{
		Symbol:    symbol,
		Exchange:  models.NSE,
		Timeframe: "day",
		From:      time.Now().AddDate(0, -3, 0),
		To:        time.Now(),
	}, 50, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get usable historical data: %w", err)
	}

	// Build analysis request
	req := agents.AnalysisRequest{
		Symbol:       symbol,
		CurrentPrice: quote.LTP,
		Candles: map[string][]models.Candle{
			"day": candles,
		},
	}

	// Process through orchestrator
	decision, err := orchestrator.ProcessSymbol(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("analysis failed: %w", err)
	}
	if !dryRun && !app.Config.SafetyCapabilities().AutoTrade {
		decision.Executed = false
		if decision.Reasoning != "" {
			decision.Reasoning += " "
		}
		decision.Reasoning += "Safety profile blocks autonomous execution."
	}

	return decision, nil
}

// displayDecision shows a trading decision in the output.
func displayDecision(output *Output, decision *models.Decision, dryRun bool) {
	if decision.Action == "HOLD" {
		return // Don't display HOLD decisions
	}

	actionColor := ColorYellow
	if decision.Action == "BUY" {
		actionColor = ColorGreen
	} else if decision.Action == "SELL" {
		actionColor = ColorRed
	}

	output.Println()
	output.Bold("🤖 AI Decision: %s %s", output.ColoredString(actionColor, decision.Action), decision.Symbol)
	output.Printf("   Confidence: %.1f%%\n", decision.Confidence)

	if decision.EntryPrice > 0 {
		output.Printf("   Entry: %s | SL: %s\n",
			FormatIndianCurrency(decision.EntryPrice),
			FormatIndianCurrency(decision.StopLoss))
	}

	if len(decision.Targets) > 0 {
		output.Printf("   Targets: %s", FormatIndianCurrency(decision.Targets[0]))
		for i := 1; i < len(decision.Targets) && i < 3; i++ {
			output.Printf(", %s", FormatIndianCurrency(decision.Targets[i]))
		}
		output.Println()
	}

	if decision.Reasoning != "" {
		output.Printf("   Reason: %s\n", truncateString(decision.Reasoning, 80))
	}

	if decision.Executed {
		if dryRun {
			output.Printf("   Status: %s\n", output.Yellow("⚡ WOULD EXECUTE (dry-run)"))
		} else {
			output.Printf("   Status: %s\n", output.Green("⚡ EXECUTING"))
		}
	} else {
		output.Dim("   Status: ○ Not executed (below threshold or risk rejected)")
	}
}

// executeDecision places an order based on the AI decision.
func executeDecision(ctx context.Context, app *App, output *Output, decision *models.Decision) {
	if err := app.checkAutoTrade(ctx); err != nil {
		output.Error("   %v", err)
		return
	}
	if err := app.validateSymbol(decision.Symbol); err != nil {
		output.Error("   Invalid symbol: %v", err)
		return
	}
	if err := app.validatePrice(decision.EntryPrice); err != nil {
		output.Error("   Invalid entry price: %v", err)
		return
	}
	if err := app.validatePrice(decision.StopLoss); err != nil {
		output.Error("   Invalid stop loss: %v", err)
		return
	}

	// Calculate position size based on config
	positionSize := calculatePositionSize(app, decision)
	if err := app.validateQuantity(positionSize); err != nil {
		output.Error("   Invalid position size: %v", err)
		return
	}

	// Determine order side
	side := models.OrderSideBuy
	if decision.Action == "SELL" {
		side = models.OrderSideSell
	}

	// Create order
	order := &models.Order{
		Symbol:   decision.Symbol,
		Exchange: models.NSE,
		Side:     side,
		Product:  models.ProductMIS, // Intraday
		Type:     models.OrderTypeLimit,
		Quantity: positionSize,
		Price:    decision.EntryPrice,
	}
	order.Tag = broker.IntentOrderTag("decision:"+decision.ID, order)

	target := 0.0
	if len(decision.Targets) > 0 {
		target = decision.Targets[0]
	}
	riskDecision, err := app.checkOrderRisk(ctx, order, decision.StopLoss, target)
	if err != nil {
		payload := map[string]interface{}{}
		if riskDecision != nil {
			payload["violations"] = riskDecision.Violations
			payload["warnings"] = riskDecision.Warnings
			payload["order_value"] = riskDecision.OrderValue
			payload["projected_value"] = riskDecision.ProjectedValue
			payload["risk_reward"] = riskDecision.RiskReward
		}
		app.logDecisionEvent(ctx, decision, models.DecisionStageExecutionBlocked, "RISK_REJECTED", err.Error(), payload)
		output.Error("   %v", err)
		return
	}
	if riskDecision != nil && riskDecision.RiskReward > 0 {
		output.Dim("   Risk approved: R:R %.2f", riskDecision.RiskReward)
	}
	riskPayload := map[string]interface{}{}
	if riskDecision != nil {
		riskPayload["order_value"] = riskDecision.OrderValue
		riskPayload["projected_value"] = riskDecision.ProjectedValue
		riskPayload["risk_reward"] = riskDecision.RiskReward
	}
	app.logDecisionEvent(ctx, decision, models.DecisionStageRiskChecked, "APPROVED", "hard risk check approved", riskPayload)
	app.logDecisionEvent(ctx, decision, models.DecisionStageOrderSubmitted, "SUBMITTED", "submitting entry order to broker", map[string]interface{}{
		"symbol":   order.Symbol,
		"side":     order.Side,
		"type":     order.Type,
		"product":  order.Product,
		"quantity": order.Quantity,
		"price":    order.Price,
		"tag":      order.Tag,
	})

	// Place the order
	result, err := app.Broker.PlaceOrder(ctx, order)
	if err != nil {
		app.logDecisionEvent(ctx, decision, models.DecisionStageOrderRejected, "BROKER_ERROR", err.Error(), map[string]interface{}{
			"symbol":   order.Symbol,
			"side":     order.Side,
			"quantity": order.Quantity,
			"price":    order.Price,
		})
		output.Error("   ❌ Order failed: %v", err)
		return
	}

	output.Success("   ✓ Order placed: %s", result.OrderID)
	app.logDecisionEvent(ctx, decision, models.DecisionStageOrderAccepted, result.Status, result.Message, map[string]interface{}{
		"order_id": result.OrderID,
		"status":   result.Status,
	})

	// Update decision with order ID
	decision.OrderID = result.OrderID

	// Save to store
	if app.Store != nil {
		if err := app.Store.SaveDecision(ctx, decision); err != nil {
			output.Dim("   Warning: Failed to save decision: %v", err)
		}
	}

	// Place stop-loss order (GTT)
	if decision.StopLoss > 0 {
		if err := app.checkPlaceGTT(ctx); err != nil {
			app.logDecisionEvent(ctx, decision, models.DecisionStageProtectiveOrder, "BLOCKED", err.Error(), map[string]interface{}{
				"trigger_price": decision.StopLoss,
				"quantity":      positionSize,
			})
			output.Dim("   Warning: Stop-loss GTT blocked: %v", err)
			return
		}
		slSide := models.OrderSideSell
		if decision.Action == "SELL" {
			slSide = models.OrderSideBuy
		}

		slOrder := &models.GTTOrder{
			Symbol:       decision.Symbol,
			Exchange:     models.NSE,
			TriggerType:  "single",
			TriggerPrice: decision.StopLoss,
			Orders: []models.GTTOrderLeg{
				{
					Side:     slSide,
					Product:  models.ProductMIS,
					Type:     models.OrderTypeMarket,
					Quantity: positionSize,
				},
			},
		}

		gttResult, err := app.Broker.PlaceGTT(ctx, slOrder)
		if err != nil {
			app.logDecisionEvent(ctx, decision, models.DecisionStageProtectiveOrder, "FAILED", err.Error(), map[string]interface{}{
				"trigger_price": decision.StopLoss,
				"quantity":      positionSize,
			})
			output.Dim("   Warning: Failed to place SL order: %v", err)
		} else {
			app.logDecisionEvent(ctx, decision, models.DecisionStageProtectiveOrder, "PLACED", "stop-loss GTT placed", map[string]interface{}{
				"trigger_id":    gttResult.TriggerID,
				"trigger_price": decision.StopLoss,
				"quantity":      positionSize,
			})
			output.Success("   ✓ Stop-loss GTT placed: %s", gttResult.TriggerID)
		}
	}
}

// calculatePositionSize determines the number of shares to trade.
func calculatePositionSize(app *App, decision *models.Decision) int {
	maxPosition := app.Config.Agents.MaxPositionSize
	if maxPosition <= 0 {
		maxPosition = 100000 // Default ₹1 lakh
	}

	if decision.EntryPrice <= 0 {
		return 1
	}

	// Calculate quantity based on max position size
	quantity := int(maxPosition / decision.EntryPrice)
	if quantity < 1 {
		quantity = 1
	}

	return quantity
}

// truncateString truncates a string to the specified length.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
