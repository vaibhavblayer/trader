// Package config provides configuration management for the trading application.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

// Config holds all application configuration.
type Config struct {
	Trading       TradingConfig       `mapstructure:"trading"`
	Risk          RiskConfig          `mapstructure:"risk"`
	UI            UIConfig            `mapstructure:"ui"`
	Notifications NotificationConfig  `mapstructure:"notifications"`
	Security      SecurityConfig      `mapstructure:"security"`
	Credentials   Credentials         `mapstructure:"-"` // Loaded separately
	Agents        AgentConfig         `mapstructure:"-"` // Loaded separately
}

// TradingConfig holds trading-related configuration.
type TradingConfig struct {
	Mode            string `mapstructure:"mode"`             // "live", "paper"
	DefaultProduct  string `mapstructure:"default_product"`  // MIS, CNC, NRML
	DefaultExchange string `mapstructure:"default_exchange"` // NSE, BSE
}

// RiskConfig holds risk management configuration.
type RiskConfig struct {
	MaxPositionPercent     float64 `mapstructure:"max_position_percent"`
	MaxSectorExposure      float64 `mapstructure:"max_sector_exposure"`
	MaxConcurrentPositions int     `mapstructure:"max_concurrent_positions"`
	MinRiskReward          float64 `mapstructure:"min_risk_reward"`
	TrailingStopPercent    float64 `mapstructure:"trailing_stop_percent"`
	DailyLossLimit         float64 `mapstructure:"daily_loss_limit"`
	MaxSlippage            float64 `mapstructure:"max_slippage"`
}

// UIConfig holds UI-related configuration.
type UIConfig struct {
	ColorEnabled bool   `mapstructure:"color_enabled"`
	DateFormat   string `mapstructure:"date_format"`
	TimeFormat   string `mapstructure:"time_format"`
}

// SecurityConfig holds security-related configuration.
type SecurityConfig struct {
	ReadOnlyMode       bool          `mapstructure:"read_only_mode"`
	SessionTimeout     time.Duration `mapstructure:"session_timeout"`
	EncryptCredentials bool          `mapstructure:"encrypt_credentials"`
	AuditEnabled       bool          `mapstructure:"audit_enabled"`
	StrictValidation   bool          `mapstructure:"strict_validation"`
}

// NotificationConfig holds notification configuration.
type NotificationConfig struct {
	Enabled  bool              `mapstructure:"enabled"`
	Level    string            `mapstructure:"level"` // all, trades_only, errors_only
	Webhook  WebhookConfig     `mapstructure:"webhook"`
	Telegram TelegramConfig    `mapstructure:"telegram"`
	Email    EmailConfig       `mapstructure:"email"`
}

// WebhookConfig holds webhook notification configuration.
type WebhookConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	URL     string `mapstructure:"url"`
}

// TelegramConfig holds Telegram notification configuration.
type TelegramConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	BotToken string `mapstructure:"bot_token"`
	ChatID   string `mapstructure:"chat_id"`
}

// EmailConfig holds email notification configuration.
type EmailConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	SMTPHost string `mapstructure:"smtp_host"`
	SMTPPort int    `mapstructure:"smtp_port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`
	To       string `mapstructure:"to"`
}

// Credentials holds API credentials.
type Credentials struct {
	Zerodha ZerodhaCredentials `mapstructure:"zerodha"`
	OpenAI  OpenAICredentials  `mapstructure:"openai"`
	Tavily  TavilyCredentials  `mapstructure:"tavily"`
}

// ZerodhaCredentials holds Zerodha API credentials.
type ZerodhaCredentials struct {
	APIKey     string `mapstructure:"api_key"`
	APISecret  string `mapstructure:"api_secret"`
	UserID     string `mapstructure:"user_id"`
	Password   string `mapstructure:"password"`    // For auto-login
	TOTPSecret string `mapstructure:"totp_secret"` // For auto-login with 2FA
}

// OpenAICredentials holds OpenAI API credentials.
type OpenAICredentials struct {
	APIKey string `mapstructure:"api_key"`
}

// TavilyCredentials holds Tavily API credentials.
type TavilyCredentials struct {
	APIKey string `mapstructure:"api_key"`
}

// AgentConfig holds AI agent configuration.
type AgentConfig struct {
	Model                string             `mapstructure:"model"`
	AutonomousMode       string             `mapstructure:"autonomous_mode"` // FULL_AUTO, SEMI_AUTO, NOTIFY_ONLY, MANUAL
	AutoExecuteThreshold float64            `mapstructure:"auto_execute_threshold"`
	MaxDailyTrades       int                `mapstructure:"max_daily_trades"`
	MaxDailyLoss         float64            `mapstructure:"max_daily_loss"`
	MaxPositionSize      float64            `mapstructure:"max_position_size"`
	CooldownMinutes      int                `mapstructure:"cooldown_minutes"`
	ConsecutiveLossLimit int                `mapstructure:"consecutive_loss_limit"`
	EnabledAgents        []string           `mapstructure:"enabled_agents"`
	AgentWeights         map[string]float64 `mapstructure:"agent_weights"`
}

// DefaultConfigDir returns the default configuration directory.
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".config/zerodha-trader"
	}
	return filepath.Join(home, ".config", "zerodha-trader")
}

// Load loads configuration from the specified directory.
// If configDir is empty, uses the default config directory.
func Load(configDir string) (*Config, error) {
	if configDir == "" {
		configDir = DefaultConfigDir()
	}

	cfg := &Config{}

	// Load main config
	if err := loadConfigFile(configDir, "config", cfg); err != nil {
		return nil, fmt.Errorf("loading config.toml: %w", err)
	}

	// Load credentials
	if err := loadCredentials(configDir, &cfg.Credentials); err != nil {
		return nil, fmt.Errorf("loading credentials.toml: %w", err)
	}

	// Load agent config
	if err := loadAgentConfig(configDir, &cfg.Agents); err != nil {
		return nil, fmt.Errorf("loading agents.toml: %w", err)
	}

	// Apply environment variable overrides
	applyEnvOverrides(cfg)

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

func loadConfigFile(configDir, name string, target interface{}) error {
	v := viper.New()
	v.SetConfigName(name)
	v.SetConfigType("toml")
	v.AddConfigPath(configDir)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Config file not found, create template
			return createTemplateConfig(configDir, name)
		}
		return err
	}

	return v.Unmarshal(target)
}

func loadCredentials(configDir string, creds *Credentials) error {
	v := viper.New()
	v.SetConfigName("credentials")
	v.SetConfigType("toml")
	v.AddConfigPath(configDir)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return createTemplateCredentials(configDir)
		}
		return err
	}

	return v.Unmarshal(creds)
}

func loadAgentConfig(configDir string, agents *AgentConfig) error {
	v := viper.New()
	v.SetConfigName("agents")
	v.SetConfigType("toml")
	v.AddConfigPath(configDir)

	// Set defaults
	v.SetDefault("model", "gpt-5.2")
	v.SetDefault("autonomous_mode", "MANUAL")
	v.SetDefault("auto_execute_threshold", 80.0)
	v.SetDefault("max_daily_trades", 10)
	v.SetDefault("max_daily_loss", 5000.0)
	v.SetDefault("max_position_size", 100000.0)
	v.SetDefault("cooldown_minutes", 5)
	v.SetDefault("consecutive_loss_limit", 3)
	v.SetDefault("enabled_agents", []string{"technical", "research", "news", "risk", "trader"})

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return createTemplateAgentConfig(configDir)
		}
		return err
	}

	return v.Unmarshal(agents)
}

func applyEnvOverrides(cfg *Config) {
	// Zerodha credentials
	if v := os.Getenv("ZERODHA_API_KEY"); v != "" {
		cfg.Credentials.Zerodha.APIKey = v
	}
	if v := os.Getenv("ZERODHA_API_SECRET"); v != "" {
		cfg.Credentials.Zerodha.APISecret = v
	}
	if v := os.Getenv("ZERODHA_USER_ID"); v != "" {
		cfg.Credentials.Zerodha.UserID = v
	}

	// OpenAI credentials
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.Credentials.OpenAI.APIKey = v
	}

	// Tavily credentials
	if v := os.Getenv("TAVILY_API_KEY"); v != "" {
		cfg.Credentials.Tavily.APIKey = v
	}

	// Trading mode
	if v := os.Getenv("TRADING_MODE"); v != "" {
		cfg.Trading.Mode = v
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	// Validate trading mode
	if c.Trading.Mode != "" && c.Trading.Mode != "live" && c.Trading.Mode != "paper" {
		return fmt.Errorf("invalid trading mode: %s (must be 'live' or 'paper')", c.Trading.Mode)
	}

	// Validate risk parameters
	if c.Risk.MaxPositionPercent < 0 || c.Risk.MaxPositionPercent > 100 {
		return fmt.Errorf("max_position_percent must be between 0 and 100")
	}
	if c.Risk.MaxSectorExposure < 0 || c.Risk.MaxSectorExposure > 100 {
		return fmt.Errorf("max_sector_exposure must be between 0 and 100")
	}
	if c.Risk.MinRiskReward < 0 {
		return fmt.Errorf("min_risk_reward must be non-negative")
	}

	// Validate agent config
	if c.Agents.AutoExecuteThreshold < 0 || c.Agents.AutoExecuteThreshold > 100 {
		return fmt.Errorf("auto_execute_threshold must be between 0 and 100")
	}

	return nil
}

// IsPaperMode returns true if paper trading mode is enabled.
func (c *Config) IsPaperMode() bool {
	return c.Trading.Mode == "paper"
}
