// Package cli provides the command-line interface for the trading application.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"zerodha-trader/internal/agents"
	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/config"
	"zerodha-trader/internal/logging"
	"zerodha-trader/internal/security"
	"zerodha-trader/internal/store"
	"zerodha-trader/internal/trading"
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
	Zerodha   *broker.ZerodhaBroker
	Broker    broker.Broker
	Ticker    broker.Ticker
	Store     store.DataStore
	LLMClient agents.LLMClient
	Access    *security.AccessController
	Validator *security.InputValidator
	Risk      *trading.RiskManager
}

// NewRootCmd creates the root command for the CLI.
// Requirements: 21.1-21.13
func NewRootCmd(cfg *config.Config, logger zerolog.Logger) *cobra.Command {
	app := &App{
		Config:    cfg,
		Logger:    logger,
		Access:    newAccessController(cfg, logger),
		Validator: security.NewInputValidator(cfg.Security.StrictValidation),
		Risk:      trading.NewRiskManager(cfg.Risk),
	}

	// Initialize SQLite store before broker setup so paper trading can restore durable state.
	configDir := cfg.ConfigDir
	if configDir == "" {
		configDir = config.DefaultConfigDir()
	}
	dbPath := filepath.Join(configDir, "trader.db")
	dataStore, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to initialize store, some features may be unavailable")
	} else {
		app.Store = dataStore
		logger.Debug().Msg("SQLite store initialized")
	}
	var paperLedger broker.PaperLedger
	if dataStore != nil {
		paperLedger = dataStore
	}

	// Initialize broker if credentials are available
	if cfg.Credentials.Zerodha.APIKey != "" {
		zerodhaBroker := broker.NewZerodhaBroker(broker.ZerodhaConfig{
			APIKey:    cfg.Credentials.Zerodha.APIKey,
			APISecret: cfg.Credentials.Zerodha.APISecret,
			UserID:    cfg.Credentials.Zerodha.UserID,
		})
		app.Zerodha = zerodhaBroker
		logger.Debug().Msg("Zerodha broker initialized")
		if cfg.IsPaperMode() {
			app.Broker = broker.NewPaperBroker(broker.PaperBrokerConfig{
				DataBroker: zerodhaBroker,
				FillModel: broker.PaperFillModel{
					SlippageRate:        cfg.Risk.MaxSlippage / 100,
					AllowPartialFills:   true,
					MaxFillDepthPercent: 25,
				},
				Ledger: paperLedger,
			})
			logger.Debug().Msg("Paper broker initialized with Zerodha market data")
		} else {
			app.Broker = zerodhaBroker
		}
		app.Broker = broker.NewSafeBroker(app.Broker)
		caps := cfg.SafetyCapabilities()
		app.Broker = broker.NewPolicyBroker(app.Broker, broker.ExecutionPolicy{
			Profile:     cfg.SafetyProfile(),
			AllowOrders: caps.BrokerOrders,
			AllowGTT:    caps.BrokerGTT,
			AllowModify: caps.BrokerOrders,
			AllowCancel: caps.BrokerOrders,
		})
		logger.Debug().Msg("Safe broker execution wrapper enabled")

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

	// Initialize LLM client if OpenAI API key is available
	if cfg.Credentials.OpenAI.APIKey != "" {
		if cfg.Agents.ReasoningEffort != "" {
			app.LLMClient = agents.NewOpenAIClientWithReasoning(
				cfg.Credentials.OpenAI.APIKey, cfg.Agents.Model, cfg.Agents.ReasoningEffort,
			)
			logger.Debug().
				Str("model", cfg.Agents.Model).
				Str("reasoning_effort", cfg.Agents.ReasoningEffort).
				Msg("OpenAI LLM client initialized with reasoning")
		} else {
			app.LLMClient = agents.NewOpenAIClient(cfg.Credentials.OpenAI.APIKey, cfg.Agents.Model)
			logger.Debug().Str("model", cfg.Agents.Model).Msg("OpenAI LLM client initialized")
		}
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
	addPaperCommands(rootCmd, app)
	addJournalCommands(rootCmd, app)
	addUtilityCommands(rootCmd, app)
	addHelpCommands(rootCmd, app)

	return rootCmd
}

// addCoreCommands adds core utility commands.
func addCoreCommands(rootCmd *cobra.Command, app *App) {
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newConfigCmd(app))
	rootCmd.AddCommand(newCompletionCmd())
}

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for trader.

To load completions:

Bash:
  $ source <(trader completion bash)
  # To load completions for each session, execute once:
  # Linux:
  $ trader completion bash > /etc/bash_completion.d/trader
  # macOS:
  $ trader completion bash > $(brew --prefix)/etc/bash_completion.d/trader

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ trader completion zsh > "${fpath[1]}/_trader"
  # Or for Oh My Zsh:
  $ trader completion zsh > ~/.oh-my-zsh/completions/_trader

  # You will need to start a new shell for this setup to take effect.

Fish:
  $ trader completion fish | source
  # To load completions for each session, execute once:
  $ trader completion fish > ~/.config/fish/completions/trader.fish

PowerShell:
  PS> trader completion powershell | Out-String | Invoke-Expression
  # To load completions for every new session, run:
  PS> trader completion powershell > trader.ps1
  # and source this file from your PowerShell profile.
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			}
			return nil
		},
	}
	return cmd
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
				// Redact credentials in JSON output
				safe := map[string]interface{}{
					"trading":       app.Config.Trading,
					"risk":          app.Config.Risk,
					"ui":            app.Config.UI,
					"notifications": app.Config.Notifications,
					"security":      app.Config.Security,
					"agents": map[string]interface{}{
						"model":                  app.Config.Agents.Model,
						"reasoning_effort":       app.Config.Agents.ReasoningEffort,
						"autonomous_mode":        app.Config.Agents.AutonomousMode,
						"auto_execute_threshold": app.Config.Agents.AutoExecuteThreshold,
						"max_daily_trades":       app.Config.Agents.MaxDailyTrades,
						"max_daily_loss":         app.Config.Agents.MaxDailyLoss,
						"max_position_size":      app.Config.Agents.MaxPositionSize,
						"cooldown_minutes":       app.Config.Agents.CooldownMinutes,
						"consecutive_loss_limit": app.Config.Agents.ConsecutiveLossLimit,
						"enabled_agents":         app.Config.Agents.EnabledAgents,
						"agent_weights":          app.Config.Agents.AgentWeights,
					},
					"credentials": map[string]interface{}{
						"zerodha_configured": app.Config.Credentials.Zerodha.APIKey != "",
						"openai_configured":  app.Config.Credentials.OpenAI.APIKey != "",
						"tavily_configured":  app.Config.Credentials.Tavily.APIKey != "",
					},
				}
				return output.JSON(safe)
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

	cmd.AddCommand(newConfigSetCmd(app))

	return cmd
}

// newConfigSetCmd creates the config set subcommand for changing settings from the CLI.
func newConfigSetCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Set a configuration value from the CLI.

Supported keys:
  model              AI model name (e.g., gpt-5.4-mini, gpt-4o, gpt-5.2)
  reasoning          Reasoning effort for reasoning models (low, medium, high, off)
  mode               Trading mode (paper, live)
  safety-profile     Safety profile (backtest, paper, live-readonly, live-trading)
  autonomous         Autonomous mode (MANUAL, NOTIFY_ONLY, SEMI_AUTO, FULL_AUTO)
  threshold          Auto-execute confidence threshold (0-100)
  max-trades         Maximum daily trades
  max-loss           Maximum daily loss in INR
  cooldown           Cooldown between trades in minutes
  max-position       Maximum position size in INR
  stop-after-losses  Stop after N consecutive losses`,
		Example: `  trader config set model gpt-5.4-mini
  trader config set reasoning high
  trader config set mode paper
  trader config set safety-profile live-readonly
  trader config set autonomous NOTIFY_ONLY
  trader config set threshold 85
  trader config set max-trades 15
  trader config set max-loss 10000`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			if err := app.checkModifyConfig(context.Background()); err != nil {
				output.Error("%v", err)
				return err
			}
			key := args[0]
			value := args[1]

			configDir := config.DefaultConfigDir()

			switch key {
			case "model":
				if err := updateAgentsToml(configDir, "model", value); err != nil {
					output.Error("Failed to update model: %v", err)
					return err
				}
				app.Config.Agents.Model = value
				output.Success("✓ AI model set to: %s", value)
				output.Dim("Restart any running daemon for changes to take effect")

			case "reasoning":
				validEfforts := map[string]bool{"low": true, "medium": true, "high": true, "off": true}
				if !validEfforts[value] {
					output.Error("Invalid reasoning effort: %s", value)
					output.Println("  Valid: low, medium, high, off")
					return fmt.Errorf("invalid reasoning effort")
				}
				tomlValue := value
				if value == "off" {
					tomlValue = "" // empty string disables it
				}
				if err := updateAgentsToml(configDir, "reasoning_effort", tomlValue); err != nil {
					output.Error("Failed to update reasoning effort: %v", err)
					return err
				}
				app.Config.Agents.ReasoningEffort = tomlValue
				if value == "off" {
					output.Success("✓ Reasoning effort disabled (using model default)")
				} else {
					output.Success("✓ Reasoning effort set to: %s", value)
				}
				output.Dim("Restart any running daemon for changes to take effect")

			case "mode":
				if value != "paper" && value != "live" {
					output.Error("Invalid mode: %s (must be 'paper' or 'live')", value)
					return fmt.Errorf("invalid mode")
				}
				if err := updateConfigToml(configDir, "trading", "mode", value); err != nil {
					output.Error("Failed to update mode: %v", err)
					return err
				}
				profile := config.SafetyProfilePaper
				if value == "live" {
					profile = config.SafetyProfileLiveReadOnly
				}
				if err := updateConfigToml(configDir, "trading", "safety_profile", profile); err != nil {
					output.Error("Failed to update safety profile: %v", err)
					return err
				}
				app.Config.Trading.Mode = value
				app.Config.Trading.SafetyProfile = profile
				output.Success("✓ Trading mode set to: %s", value)
				output.Dim("Safety profile set to: %s", profile)

			case "safety-profile":
				validProfiles := map[string]bool{
					config.SafetyProfileBacktest:     true,
					config.SafetyProfilePaper:        true,
					config.SafetyProfileLiveReadOnly: true,
					config.SafetyProfileLiveTrading:  true,
				}
				if !validProfiles[value] {
					output.Error("Invalid safety profile: %s", value)
					output.Println("  Valid: backtest, paper, live-readonly, live-trading")
					return fmt.Errorf("invalid safety profile")
				}
				if value == config.SafetyProfilePaper && app.Config.Trading.Mode == "live" {
					output.Error("Safety profile paper requires trading mode paper")
					return fmt.Errorf("invalid safety profile for mode")
				}
				if (value == config.SafetyProfileLiveReadOnly || value == config.SafetyProfileLiveTrading) && app.Config.Trading.Mode != "live" {
					output.Error("Safety profile %s requires trading mode live", value)
					return fmt.Errorf("invalid safety profile for mode")
				}
				if err := updateConfigToml(configDir, "trading", "safety_profile", value); err != nil {
					output.Error("Failed to update safety profile: %v", err)
					return err
				}
				app.Config.Trading.SafetyProfile = value
				output.Success("✓ Safety profile set to: %s", value)
				output.Dim("Restart any running daemon for changes to take effect")

			case "autonomous":
				validModes := map[string]bool{"MANUAL": true, "NOTIFY_ONLY": true, "SEMI_AUTO": true, "FULL_AUTO": true}
				if !validModes[value] {
					output.Error("Invalid autonomous mode: %s", value)
					output.Println("  Valid: MANUAL, NOTIFY_ONLY, SEMI_AUTO, FULL_AUTO")
					return fmt.Errorf("invalid autonomous mode")
				}
				if err := updateAgentsToml(configDir, "autonomous_mode", value); err != nil {
					output.Error("Failed to update autonomous mode: %v", err)
					return err
				}
				app.Config.Agents.AutonomousMode = value
				output.Success("✓ Autonomous mode set to: %s", value)

			case "threshold":
				var threshold float64
				if _, err := fmt.Sscanf(value, "%f", &threshold); err != nil || threshold < 0 || threshold > 100 {
					output.Error("Invalid threshold: %s (must be 0-100)", value)
					return fmt.Errorf("invalid threshold")
				}
				if err := updateAgentsToml(configDir, "auto_execute_threshold", value); err != nil {
					output.Error("Failed to update threshold: %v", err)
					return err
				}
				app.Config.Agents.AutoExecuteThreshold = threshold
				output.Success("✓ Auto-execute threshold set to: %.0f%%", threshold)

			case "max-trades":
				var maxTrades int
				if _, err := fmt.Sscanf(value, "%d", &maxTrades); err != nil || maxTrades < 0 {
					output.Error("Invalid max-trades: %s (must be a positive integer)", value)
					return fmt.Errorf("invalid max-trades")
				}
				if err := updateAgentsToml(configDir, "max_daily_trades", value); err != nil {
					output.Error("Failed to update max trades: %v", err)
					return err
				}
				app.Config.Agents.MaxDailyTrades = maxTrades
				output.Success("✓ Max daily trades set to: %d", maxTrades)

			case "max-loss":
				var maxLoss float64
				if _, err := fmt.Sscanf(value, "%f", &maxLoss); err != nil || maxLoss < 0 {
					output.Error("Invalid max-loss: %s (must be a positive number)", value)
					return fmt.Errorf("invalid max-loss")
				}
				if err := updateAgentsToml(configDir, "max_daily_loss", value); err != nil {
					output.Error("Failed to update max loss: %v", err)
					return err
				}
				app.Config.Agents.MaxDailyLoss = maxLoss
				output.Success("✓ Max daily loss set to: ₹%.0f", maxLoss)

			case "cooldown":
				var cooldown int
				if _, err := fmt.Sscanf(value, "%d", &cooldown); err != nil || cooldown < 0 {
					output.Error("Invalid cooldown: %s (must be a positive integer)", value)
					return fmt.Errorf("invalid cooldown")
				}
				if err := updateAgentsToml(configDir, "cooldown_minutes", value); err != nil {
					output.Error("Failed to update cooldown: %v", err)
					return err
				}
				app.Config.Agents.CooldownMinutes = cooldown
				output.Success("✓ Cooldown set to: %d minutes", cooldown)

			case "max-position":
				var maxPos float64
				if _, err := fmt.Sscanf(value, "%f", &maxPos); err != nil || maxPos < 0 {
					output.Error("Invalid max-position: %s (must be a positive number)", value)
					return fmt.Errorf("invalid max-position")
				}
				if err := updateAgentsToml(configDir, "max_position_size", value); err != nil {
					output.Error("Failed to update max position: %v", err)
					return err
				}
				app.Config.Agents.MaxPositionSize = maxPos
				output.Success("✓ Max position size set to: ₹%.0f", maxPos)

			case "stop-after-losses":
				var limit int
				if _, err := fmt.Sscanf(value, "%d", &limit); err != nil || limit < 0 {
					output.Error("Invalid stop-after-losses: %s (must be a positive integer)", value)
					return fmt.Errorf("invalid stop-after-losses")
				}
				if err := updateAgentsToml(configDir, "consecutive_loss_limit", value); err != nil {
					output.Error("Failed to update consecutive loss limit: %v", err)
					return err
				}
				app.Config.Agents.ConsecutiveLossLimit = limit
				output.Success("✓ Consecutive loss limit set to: %d", limit)

			default:
				output.Error("Unknown config key: %s", key)
				output.Println("  Run 'trader config set --help' to see supported keys")
				return fmt.Errorf("unknown key: %s", key)
			}

			return nil
		},
	}

	return cmd
}

// updateAgentsToml updates a top-level key in agents.toml.
func updateAgentsToml(configDir, key, value string) error {
	return updateTomlFile(configDir, "agents.toml", key, value)
}

// updateConfigToml updates a key in a section of config.toml.
func updateConfigToml(configDir, section, key, value string) error {
	filePath := configDir + "/config.toml"
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	inSection := false
	found := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track which section we're in
		if strings.HasPrefix(trimmed, "[") {
			inSection = strings.TrimSpace(trimmed) == "["+section+"]"
		}

		// Find the key in the right section
		if inSection && strings.HasPrefix(trimmed, key+" ") || inSection && strings.HasPrefix(trimmed, key+"=") {
			lines[i] = fmt.Sprintf(`%s = "%s"`, key, value)
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("key '%s' not found in [%s] section", key, section)
	}

	return os.WriteFile(filePath, []byte(strings.Join(lines, "\n")), 0644)
}

// updateTomlFile updates a top-level key in a TOML file.
// If the key doesn't exist, it inserts it after the closest related key.
func updateTomlFile(configDir, filename, key, value string) error {
	filePath := configDir + "/" + filename
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", filename, err)
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	found := false
	firstSectionIdx := len(lines) // index of first [section] header

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track where sections start
		if strings.HasPrefix(trimmed, "[") && firstSectionIdx == len(lines) {
			firstSectionIdx = i
		}

		// Skip lines inside [sections] — we only want top-level keys
		if i >= firstSectionIdx {
			break
		}

		if strings.HasPrefix(trimmed, key+" ") || strings.HasPrefix(trimmed, key+"=") {
			if isNumericValue(value) {
				lines[i] = fmt.Sprintf(`%s = %s`, key, value)
			} else {
				lines[i] = fmt.Sprintf(`%s = "%s"`, key, value)
			}
			found = true
			break
		}
	}

	if !found {
		// Key doesn't exist yet — insert it before the first section header
		var newLine string
		if isNumericValue(value) {
			newLine = fmt.Sprintf(`%s = %s`, key, value)
		} else {
			newLine = fmt.Sprintf(`%s = "%s"`, key, value)
		}

		// Find a good insertion point: after the "model" line for reasoning_effort,
		// or just before the first section header otherwise
		insertIdx := firstSectionIdx
		for i := 0; i < firstSectionIdx; i++ {
			trimmed := strings.TrimSpace(lines[i])
			// Insert reasoning_effort right after model line
			if key == "reasoning_effort" && strings.HasPrefix(trimmed, "model") {
				insertIdx = i + 1
				break
			}
		}

		// Insert the new line
		newLines := make([]string, 0, len(lines)+1)
		newLines = append(newLines, lines[:insertIdx]...)
		newLines = append(newLines, newLine)
		newLines = append(newLines, lines[insertIdx:]...)
		lines = newLines
	}

	return os.WriteFile(filePath, []byte(strings.Join(lines, "\n")), 0644)
}

// isNumericValue checks if a string looks like a number.
func isNumericValue(s string) bool {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return err == nil
}

func showConfig(output *Output, cfg *config.Config) error {
	output.Bold("Trading Configuration")
	output.Printf("  Mode:            %s\n", cfg.Trading.Mode)
	output.Printf("  Safety Profile:  %s\n", cfg.SafetyProfile())
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
	if cfg.Agents.ReasoningEffort != "" {
		output.Printf("  Reasoning:       %s\n", cfg.Agents.ReasoningEffort)
	} else {
		output.Printf("  Reasoning:       default\n")
	}
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
