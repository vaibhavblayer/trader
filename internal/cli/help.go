// Package cli provides the command-line interface for the trading application.
package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

// addHelpCommands adds help and documentation commands.
// Requirements: 64.1-64.8
func addHelpCommands(rootCmd *cobra.Command, app *App) {
	rootCmd.AddCommand(newCommandsCmd(app))
	rootCmd.AddCommand(newExamplesCmd(app))
	rootCmd.AddCommand(newQuickstartCmd(app))
}

func newCommandsCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "commands",
		Short: "List all commands by category",
		Long:  "Display all available commands organized by category.",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			output.Bold("Zerodha Go Trader Commands")
			output.Println()

			categories := []struct {
				name     string
				commands []struct {
					cmd  string
					desc string
				}
			}{
				{
					name: "Authentication",
					commands: []struct {
						cmd  string
						desc string
					}{
						{"login", "Login to Zerodha Kite Connect"},
						{"logout", "Logout and clear session"},
					},
				},
				{
					name: "Market Data",
					commands: []struct {
						cmd  string
						desc string
					}{
						{"quote <symbol>", "Get real-time quote"},
						{"data <symbol>", "Get historical OHLCV data"},
						{"live <symbols...>", "Stream live prices (WebSocket)"},
						{"live -w nifty50", "Stream predefined watchlist"},
						{"breadth", "Market breadth indicators"},
					},
				},
				{
					name: "Analysis",
					commands: []struct {
						cmd  string
						desc string
					}{
						{"analyze <symbol>", "Full technical analysis"},
						{"signal <symbol>", "Composite signal score"},
						{"mtf <symbol>", "Multi-timeframe analysis"},
						{"scan", "Stock screener"},
						{"research <symbol>", "AI research report"},
					},
				},
				{
					name: "Trading",
					commands: []struct {
						cmd  string
						desc string
					}{
						{"buy <symbol> <qty>", "Place buy order"},
						{"sell <symbol> <qty>", "Place sell order"},
						{"positions", "View open positions"},
						{"holdings", "View delivery holdings"},
						{"exit <symbol>", "Exit a position"},
						{"exit-all", "Exit all positions"},
						{"orders", "View orders"},
						{"balance", "Account balance"},
					},
				},
				{
					name: "Derivatives",
					commands: []struct {
						cmd  string
						desc string
					}{
						{"options chain <symbol>", "Option chain"},
						{"options greeks", "Calculate Greeks"},
						{"options strategy", "Strategy builder"},
						{"options payoff", "Payoff diagram"},
						{"futures chain <symbol>", "Futures chain"},
						{"futures rollover", "Roll position"},
						{"gtt create/list/cancel", "GTT orders"},
						{"bracket create", "Bracket order"},
					},
				},
				{
					name: "Planning",
					commands: []struct {
						cmd  string
						desc string
					}{
						{"plan add/list/execute/cancel", "Trade plan management"},
						{"prep", "Next-day preparation"},
						{"alert add/list/delete", "Price alerts"},
						{"events", "Corporate events calendar"},
					},
				},
				{
					name: "Monitoring",
					commands: []struct {
						cmd  string
						desc string
					}{
						{"watch", "Interactive watch mode"},
						{"live <symbols...>", "Real-time streaming"},
						{"live -w nifty50/banknifty/it", "Stream sector watchlist"},
						{"watchlist add/remove/list", "Watchlist management"},
					},
				},
				{
					name: "Autonomous Trading",
					commands: []struct {
						cmd  string
						desc string
					}{
						{"trader start/stop/status", "Daemon control"},
						{"trader pause/resume", "Pause/resume trading"},
						{"trader decisions", "View AI decisions"},
						{"trader config", "View/edit config"},
						{"trader health", "System diagnostics"},
					},
				},
				{
					name: "Journal",
					commands: []struct {
						cmd  string
						desc string
					}{
						{"journal today", "Today's journal"},
						{"journal add <trade-id>", "Add trade analysis"},
						{"journal report", "Performance reports"},
						{"journal search", "Search entries"},
					},
				},
				{
					name: "Utilities",
					commands: []struct {
						cmd  string
						desc string
					}{
						{"backtest", "Strategy backtesting"},
						{"export candles/trades/journal", "Data export"},
						{"config show/edit/validate", "Configuration"},
						{"api start", "REST API server"},
					},
				},
				{
					name: "Help",
					commands: []struct {
						cmd  string
						desc string
					}{
						{"help <command>", "Detailed help"},
						{"commands", "List all commands"},
						{"examples", "Common workflows"},
						{"quickstart", "New user guide"},
						{"version", "Version information"},
					},
				},
			}

			for _, cat := range categories {
				output.Bold(cat.name)
				for _, c := range cat.commands {
					output.Printf("  %-30s %s\n", output.Cyan(c.cmd), c.desc)
				}
				output.Println()
			}

			output.Dim("Use 'trader help <command>' for detailed help on any command")

			return nil
		},
	}
}

func newExamplesCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "examples",
		Short: "Show common workflow examples",
		Long:  "Display examples of common trading workflows.",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			output.Bold("Common Workflow Examples")
			output.Println()

			examples := []struct {
				title    string
				commands []string
			}{
				{
					title: "Morning Routine",
					commands: []string{
						"trader login                    # Login to Zerodha",
						"trader breadth                  # Check market breadth",
						"trader scan --preset momentum   # Find momentum stocks",
						"trader analyze RELIANCE         # Analyze a stock",
						"trader signal RELIANCE          # Get signal score",
					},
				},
				{
					title: "Live Streaming",
					commands: []string{
						"trader live RELIANCE INFY TCS   # Stream multiple symbols",
						"trader live -w nifty50          # Stream NIFTY 50 stocks",
						"trader live -w banknifty        # Stream Bank NIFTY stocks",
						"trader live -w it               # Stream IT sector",
						"trader live -w pharma           # Stream Pharma sector",
						"trader live RELIANCE --mode full # Full tick data with depth",
					},
				},
				{
					title: "AI Paper Trading",
					commands: []string{
						"trader paper RELIANCE INFY TCS  # Track AI predictions",
						"trader paper -w nifty50         # Paper trade NIFTY 50",
						"trader paper RELIANCE -t 15m    # 15-minute prediction window",
						"trader paper -c 70              # Only show 70%+ confidence",
						"trader paper -i 30              # Analyze every 30 seconds",
					},
				},
				{
					title: "Place a Trade with Stop-Loss",
					commands: []string{
						"trader quote RELIANCE           # Check current price",
						"trader buy RELIANCE 10 --sl 2400 --target 2550",
						"trader positions                # Verify position",
					},
				},
				{
					title: "Options Trading",
					commands: []string{
						"trader options chain NIFTY      # View option chain",
						"trader options greeks --symbol NIFTY --strike 19500 --type CE",
						"trader options strategy build straddle --symbol NIFTY",
						"trader options payoff           # View payoff diagram",
					},
				},
				{
					title: "Set Up Trade Plans",
					commands: []string{
						"trader plan add RELIANCE --entry 2450 --sl 2400 --target 2550",
						"trader plan list                # View all plans",
						"trader alert add RELIANCE --above 2440  # Alert near entry",
					},
				},
				{
					title: "End of Day Review",
					commands: []string{
						"trader positions                # Check open positions",
						"trader journal today            # Review today's trades",
						"trader journal add TRADE001 --entry-quality 4 --lessons 'Good entry'",
						"trader prep                     # Prepare for tomorrow",
					},
				},
				{
					title: "Start Autonomous Trading",
					commands: []string{
						"trader trader config            # Review settings",
						"trader trader start --dry-run   # Test without trading",
						"trader trader start             # Start trading",
						"trader trader status            # Check status",
						"trader trader decisions list    # View AI decisions",
					},
				},
				{
					title: "Backtest a Strategy",
					commands: []string{
						"trader backtest --strategy momentum --symbol RELIANCE --days 365",
						"trader backtest --strategy breakout --watchlist nifty50",
					},
				},
				{
					title: "Export Data",
					commands: []string{
						"trader export candles RELIANCE --days 365 --format csv",
						"trader export trades --format json",
						"trader journal report --period monthly",
					},
				},
			}

			for _, ex := range examples {
				output.Bold(ex.title)
				for _, c := range ex.commands {
					parts := strings.SplitN(c, "#", 2)
					if len(parts) == 2 {
						output.Printf("  %s %s\n", output.Cyan(strings.TrimSpace(parts[0])), output.DimText(strings.TrimSpace(parts[1])))
					} else {
						output.Printf("  %s\n", output.Cyan(c))
					}
				}
				output.Println()
			}

			return nil
		},
	}
}

func newQuickstartCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "quickstart",
		Short: "New user guide",
		Long:  "Step-by-step guide for new users.",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)

			output.Bold("Zerodha Go Trader - Quick Start Guide")
			output.Println()

			steps := []struct {
				step  int
				title string
				desc  string
				cmd   string
			}{
				{
					step:  1,
					title: "Configure Credentials",
					desc:  "Edit the credentials file with your Zerodha API key and secret.",
					cmd:   "trader config path  # Shows config directory",
				},
				{
					step:  2,
					title: "Login to Zerodha",
					desc:  "Authenticate with Zerodha Kite Connect.",
					cmd:   "trader login",
				},
				{
					step:  3,
					title: "Check Account Balance",
					desc:  "Verify your account is connected.",
					cmd:   "trader balance",
				},
				{
					step:  4,
					title: "Get a Quote",
					desc:  "Fetch real-time price for a stock.",
					cmd:   "trader quote RELIANCE",
				},
				{
					step:  5,
					title: "Analyze a Stock",
					desc:  "Run technical analysis on a stock.",
					cmd:   "trader analyze RELIANCE",
				},
				{
					step:  6,
					title: "Create a Watchlist",
					desc:  "Add stocks to your watchlist.",
					cmd:   "trader watchlist add RELIANCE",
				},
				{
					step:  7,
					title: "Set Up Paper Trading",
					desc:  "Practice without real money.",
					cmd:   "Edit config.toml: trading.mode = \"paper\"",
				},
				{
					step:  8,
					title: "Place Your First Trade",
					desc:  "Buy a stock (in paper mode first!).",
					cmd:   "trader buy RELIANCE 10 --sl 2400 --target 2550",
				},
			}

			for _, s := range steps {
				output.Printf("%s Step %d: %s\n", output.Cyan("→"), s.step, output.BoldText(s.title))
				output.Printf("  %s\n", s.desc)
				output.Printf("  %s\n\n", output.DimText(s.cmd))
			}

			output.Bold("Configuration Files")
			output.Println()
			output.Printf("  %s - API credentials (Zerodha, OpenAI)\n", output.Cyan("credentials.toml"))
			output.Printf("  %s - Trading settings, risk parameters\n", output.Cyan("config.toml"))
			output.Printf("  %s - AI agent configuration\n", output.Cyan("agents.toml"))
			output.Println()

			output.Bold("Getting Help")
			output.Println()
			output.Printf("  %s - List all commands\n", output.Cyan("trader commands"))
			output.Printf("  %s - Common workflows\n", output.Cyan("trader examples"))
			output.Printf("  %s - Help for any command\n", output.Cyan("trader help <command>"))
			output.Println()

			output.Bold("Important Notes")
			output.Println()
			output.Printf("  %s Always start with paper trading mode\n", output.Yellow("⚠"))
			output.Printf("  %s Set appropriate risk limits in config.toml\n", output.Yellow("⚠"))
			output.Printf("  %s Review AI decisions before enabling autonomous mode\n", output.Yellow("⚠"))
			output.Printf("  %s Keep your API credentials secure\n", output.Yellow("⚠"))

			return nil
		},
	}
}
