// Package cli provides the command-line interface for the trading application.
package cli

import (
	"encoding/json"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"zerodha-trader/internal/agents"
	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/config"
	"zerodha-trader/internal/logging"
	"zerodha-trader/internal/store"
)

// Version information
const (
	Version   = "0.1.0"
	BuildDate = "2024-01-01"
)

// App holds the application dependencies.
type App struct {
	Config    *config.Config
	Logger    zerolog.Logger
	Broker    broker.Broker
	Ticker    broker.Ticker
	Store     store.DataStore
	LLMClient agents.LLMClient
}

// NewRootCmd creates the root command for the CLI.
// Requirements: 21.1-21.13
func NewRootCmd(cfg *config.Config, logger zerolog.Logger) *cobra.Command {
	app := &App{
		Config: cfg,
		Logger: logger,
	}

	// Initialize broker if credentials are available
	if cfg.Credentials.Zerodha.APIKey != "" {
		zerodhaBroker := broker.NewZerodhaBroker(broker.ZerodhaConfig{
			APIKey:    cfg.Credentials.Zerodha.APIKey,
			APISecret: cfg.Credentials.Zerodha.APISecret,
			UserID:    cfg.Credentials.Zerodha.UserID,
		})
		app.Broker = zerodhaBroker
		logger.Debug().Msg("Zerodha broker initialized")

		// Initialize ticker if broker is authenticated
		if zerodhaBroker.IsAuthenticated() {
			ticker, err := zerodhaBroker.CreateTicker()
			if err != nil {
				logger.Warn().Err(err).Msg("Failed to create ticker")
			} else {
				app.Ticker = ticker
				logger.Debug().Msg("Zerodha ticker initialized")
			}
		}
	}

	// Initialize SQLite store
	dbPath := config.DefaultConfigDir() + "/trader.db"
	dataStore, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to initialize store, some features may be unavailable")
	} else {
		app.Store = dataStore
		logger.Debug().Msg("SQLite store initialized")
	}

	// Initialize LLM client if OpenAI API key is available
	if cfg.Credentials.OpenAI.APIKey != "" {
		app.LLMClient = agents.NewOpenAIClient(cfg.Credentials.OpenAI.APIKey, cfg.Agents.Model)
		logger.Debug().Str("model", cfg.Agents.Model).Msg("OpenAI LLM client initialized")
	}

	rootCmd := &cobra.Command{
		Use:   "trader",
		Short: "Zerodha Go Trader - AI-powered day trading CLI",
		Long: `Zerodha Go Trader is an autonomous day trading CLI for the Indian stock market.

It integrates with Zerodha Kite Connect API and uses AI agents for trading decisions.
Features include real-time market data, technical analysis, and automated trading.

Use 'trader help <command>' for more information about a command.
Use 'trader examples' to see common workflows.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Handle debug flag
			debug, _ := cmd.Flags().GetBool("debug")
			if debug {
				logging.SetDebugLevel()
				app.Logger = app.Logger.Level(zerolog.DebugLevel)
			}
			return nil
		},
	}

	// Global flags
	rootCmd.PersistentFlags().String("config", "", "config directory (default: ~/.config/zerodha-trader)")
	rootCmd.PersistentFlags().Bool("json", false, "output in JSON format")
	rootCmd.PersistentFlags().Bool("debug", false, "enable debug logging")

	// Add all command groups
	addCoreCommands(rootCmd, app)
	addAuthCommands(rootCmd, app)
	addMarketDataCommands(rootCmd, app)
	addAnalysisCommands(rootCmd, app)
	addTradingCommands(rootCmd, app)
	addDerivativesCommands(rootCmd, app)
	addPlanningCommands(rootCmd, app)
	addMonitoringCommands(rootCmd, app)
	addTraderCommands(rootCmd, app)
	addJournalCommands(rootCmd, app)
	addUtilityCommands(rootCmd, app)
	addHelpCommands(rootCmd, app)

	return rootCmd
}

// addCoreCommands adds core utility commands.
func addCoreCommands(rootCmd *cobra.Command, app *App) {
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newConfigCmd(app))
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			output := NewOutput(cmd)
			if output.IsJSON() {
				output.JSON(map[string]string{
					"version":    Version,
					"build_date": BuildDate,
				})
			} else {
				output.Printf("Zerodha Go Trader v%s\n", Version)
				output.Dim("Build date: %s", BuildDate)
			}
		},
	}
}

func newConfigCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration management",
		Long:  "View and manage application configuration.",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			if output.IsJSON() {
				return output.JSON(app.Config)
			}
			return showConfig(output, app.Config)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Show configuration directory path",
		Run: func(cmd *cobra.Command, args []string) {
			output := NewOutput(cmd)
			if output.IsJSON() {
				output.JSON(map[string]string{"path": config.DefaultConfigDir()})
			} else {
				output.Println(config.DefaultConfigDir())
			}
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate configuration files",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			if err := app.Config.Validate(); err != nil {
				output.Error("Configuration validation failed: %v", err)
				return err
			}
			if output.IsJSON() {
				output.JSON(map[string]bool{"valid": true})
			} else {
				output.Success("✓ Configuration is valid")
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "edit",
		Short: "Open configuration file in editor",
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			configPath := config.DefaultConfigDir() + "/config.toml"
			output.Info("Configuration file: %s", configPath)
			output.Println("Edit this file to change settings.")
			return nil
		},
	})

	return cmd
}

func showConfig(output *Output, cfg *config.Config) error {
	output.Bold("Trading Configuration")
	output.Printf("  Mode:            %s\n", cfg.Trading.Mode)
	output.Printf("  Default Product: %s\n", cfg.Trading.DefaultProduct)
	output.Printf("  Default Exchange: %s\n", cfg.Trading.DefaultExchange)
	output.Println()

	output.Bold("Risk Configuration")
	output.Printf("  Max Position %%:  %.1f%%\n", cfg.Risk.MaxPositionPercent)
	output.Printf("  Max Sector Exp:  %.1f%%\n", cfg.Risk.MaxSectorExposure)
	output.Printf("  Max Positions:   %d\n", cfg.Risk.MaxConcurrentPositions)
	output.Printf("  Min Risk/Reward: %.1f\n", cfg.Risk.MinRiskReward)
	output.Printf("  Daily Loss Limit: %s\n", FormatIndianCurrency(cfg.Risk.DailyLossLimit))
	output.Println()

	output.Bold("Agent Configuration")
	output.Printf("  Model:           %s\n", cfg.Agents.Model)
	output.Printf("  Autonomous Mode: %s\n", cfg.Agents.AutonomousMode)
	output.Printf("  Auto Threshold:  %.0f%%\n", cfg.Agents.AutoExecuteThreshold)
	output.Printf("  Max Daily Trades: %d\n", cfg.Agents.MaxDailyTrades)
	output.Printf("  Cooldown:        %d min\n", cfg.Agents.CooldownMinutes)
	output.Println()

	output.Bold("Notifications")
	output.Printf("  Enabled:         %v\n", cfg.Notifications.Enabled)
	output.Printf("  Level:           %s\n", cfg.Notifications.Level)
	output.Printf("  Webhook:         %v\n", cfg.Notifications.Webhook.Enabled)
	output.Printf("  Telegram:        %v\n", cfg.Notifications.Telegram.Enabled)
	output.Printf("  Email:           %v\n", cfg.Notifications.Email.Enabled)

	return nil
}

// showConfigJSON outputs config as JSON.
func showConfigJSON(cfg *config.Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
