package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const configTemplate = `# Zerodha Go Trader Configuration

[trading]
# Trading mode: "live" or "paper"
mode = "paper"
# Default product type: MIS, CNC, NRML
default_product = "MIS"
# Default exchange: NSE, BSE
default_exchange = "NSE"

[risk]
# Maximum position size as percentage of portfolio
max_position_percent = 10.0
# Maximum exposure per sector as percentage
max_sector_exposure = 30.0
# Maximum number of concurrent positions
max_concurrent_positions = 5
# Minimum risk-reward ratio
min_risk_reward = 2.0
# Trailing stop-loss percentage
trailing_stop_percent = 1.0
# Daily loss limit in INR
daily_loss_limit = 5000.0
# Maximum slippage alert threshold
max_slippage = 0.5

[security]
# Enable read-only mode (blocks all trading operations)
read_only_mode = false
# Session timeout duration (e.g., "8h", "30m")
session_timeout = "8h"
# Encrypt credentials at rest
encrypt_credentials = true
# Enable audit logging for all trading actions
audit_enabled = true
# Enable strict input validation
strict_validation = true

[ui]
# Enable colored output
color_enabled = true
# Date format
date_format = "02-Jan-2006"
# Time format
time_format = "15:04:05"

[notifications]
# Enable notifications
enabled = false
# Notification level: all, trades_only, errors_only
level = "all"

[notifications.webhook]
enabled = false
url = ""

[notifications.telegram]
enabled = false
bot_token = ""
chat_id = ""

[notifications.email]
enabled = false
smtp_host = ""
smtp_port = 587
username = ""
password = ""
from = ""
to = ""
`

const credentialsTemplate = `# Zerodha Go Trader Credentials
# WARNING: Keep this file secure! Do not commit to version control.

[zerodha]
api_key = ""
api_secret = ""
user_id = ""

[openai]
api_key = ""

[tavily]
api_key = ""
`

const agentsTemplate = `# Zerodha Go Trader Agent Configuration

# LLM model to use
model = "gpt-4o"
# Temperature for LLM responses (0.0 - 1.0)
temperature = 0.7
# Maximum tokens for LLM responses
max_tokens = 4096

# Operating mode: FULL_AUTO, SEMI_AUTO, NOTIFY_ONLY, MANUAL
autonomous_mode = "MANUAL"
# Minimum confidence for automatic execution (0-100)
auto_execute_threshold = 80.0
# Maximum trades per day
max_daily_trades = 10
# Maximum daily loss in INR (stop trading if exceeded)
max_daily_loss = 5000.0
# Maximum position size in INR
max_position_size = 100000.0
# Cooldown between trades in minutes
cooldown_minutes = 5
# Stop trading after this many consecutive losses
consecutive_loss_limit = 3

# Enabled agents
enabled_agents = ["technical", "research", "news", "risk", "trader"]

# Agent weights for consensus calculation
[agent_weights]
technical = 0.3
research = 0.2
news = 0.15
risk = 0.15
trader = 0.2
`

func createTemplateConfig(configDir, name string) error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	path := filepath.Join(configDir, name+".toml")
	if err := os.WriteFile(path, []byte(configTemplate), 0644); err != nil {
		return fmt.Errorf("writing config template: %w", err)
	}

	return fmt.Errorf("config file not found, created template at %s", path)
}

func createTemplateCredentials(configDir string) error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	path := filepath.Join(configDir, "credentials.toml")
	// Use restricted permissions for credentials file
	if err := os.WriteFile(path, []byte(credentialsTemplate), 0600); err != nil {
		return fmt.Errorf("writing credentials template: %w", err)
	}

	return fmt.Errorf("credentials file not found, created template at %s", path)
}

func createTemplateAgentConfig(configDir string) error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	path := filepath.Join(configDir, "agents.toml")
	if err := os.WriteFile(path, []byte(agentsTemplate), 0644); err != nil {
		return fmt.Errorf("writing agents template: %w", err)
	}

	return fmt.Errorf("agents config file not found, created template at %s", path)
}
