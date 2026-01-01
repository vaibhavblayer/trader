# Zerodha Go Trader

An AI-powered autonomous day trading CLI for the Indian stock market (NSE/BSE), built in Go with Zerodha Kite Connect API integration.

## Features

### Market Data
- Real-time quotes and live WebSocket streaming
- Multi-symbol live streaming with table display
- Predefined sector watchlists (NIFTY 50, Bank NIFTY, IT, Auto, Pharma, FMCG)
- Historical OHLCV data with multiple timeframes
- Market breadth indicators (A/D ratio, VIX, PCR)
- Voice notifications for price alerts (macOS)

### Technical Analysis
- 20+ indicators (RSI, MACD, Bollinger Bands, SuperTrend, ADX, etc.)
- Candlestick pattern detection (Engulfing, Doji, Hammer, etc.)
- Chart pattern recognition (Head & Shoulders, Triangles, Flags)
- Support/Resistance and Fibonacci levels
- Multi-timeframe analysis with confluence scoring

### AI-Powered Trading
- Multiple specialized agents (Technical, News, Research, Risk)
- Consensus-based decision making
- Configurable autonomous modes (Manual, Notify, Semi-Auto, Full-Auto)
- Full transparency with decision audit trail

### Indian Market Specific
- MIS margin multipliers and peak margin tracking (SEBI compliant)
- Circuit limit monitoring
- Pre-market/closing session support
- T+1 settlement tracking
- ASM/GSM/T2T surveillance alerts
- F&O expiry management with rollover suggestions
- FII/DII and MF flow tracking

### Order Management
- Regular, bracket, and cover orders
- GTT (Good Till Triggered) orders
- Position and portfolio management
- Risk controls and daily limits

## Requirements

- Go 1.21+
- Zerodha Kite Connect API credentials

## Installation

```bash
# Clone the repository
git clone https://github.com/yourusername/zerodha-trader.git
cd zerodha-trader

# Build
go build -o trader ./cmd/trader

# Or install globally
go install ./cmd/trader
```

### Shell Completion (Tab Completion)

Enable tab completion for commands, flags, and arguments:

```bash
# Zsh (macOS default / Oh My Zsh)
mkdir -p ~/.oh-my-zsh/completions
trader completion zsh > ~/.oh-my-zsh/completions/_trader
source ~/.zshrc

# Bash (Linux)
trader completion bash > /etc/bash_completion.d/trader

# Bash (macOS with Homebrew)
trader completion bash > $(brew --prefix)/etc/bash_completion.d/trader

# Fish
trader completion fish > ~/.config/fish/completions/trader.fish
```

After setup, press Tab to autocomplete commands and flags:
```bash
trader li<Tab>        # completes to 'trader live'
trader live --<Tab>   # shows available flags
```

## Configuration

On first run, config templates are created at `~/.config/zerodha-trader/`:

### credentials.toml
```toml
[zerodha]
api_key = "your_api_key"
api_secret = "your_api_secret"
user_id = "your_user_id"
# Optional: For auto-login (no browser required)
password = "your_kite_password"
totp_secret = "your_totp_secret"  # From Zerodha Console > TOTP setup
```

### Auto-Login Setup (Recommended)

Zerodha requires daily authentication. To automate this:

1. Enable TOTP in Zerodha Console > My Profile > Password & Security
2. When setting up TOTP, copy the **secret key** (not the QR code)
3. Add `password` and `totp_secret` to credentials.toml
4. Run `trader autologin` instead of `trader login`

```bash
# Daily login (no browser needed)
trader autologin

# Check auth status
trader auth-status
```

### config.toml
```toml
[trading]
mode = "PAPER"  # PAPER or LIVE
default_product = "MIS"
default_exchange = "NSE"

[risk]
max_position_percent = 5.0
max_daily_loss = 5000
max_concurrent_positions = 5

[agents]
model = "gpt-4"
autonomous_mode = "NOTIFY_ONLY"  # MANUAL, NOTIFY_ONLY, SEMI_AUTO, FULL_AUTO
auto_execute_threshold = 85
```

## Commands

```
AUTHENTICATION
  login                     Login to Zerodha (OAuth flow)
  login --token <token>     Complete login with request token
  logout                    Logout and clear session
  balance                   View account balance and margins

MARKET DATA
  quote <symbol>            Get real-time quote
  data <symbol>             Get historical OHLCV data
    -t, --timeframe         Timeframe (1min, 5min, 15min, 30min, 1hour, 1day)
    -d, --days              Number of days of history
    -l, --limit             Limit number of candles (0 for all)
  live [symbols...]         Stream live prices (WebSocket)
    -w, --watchlist         Use predefined or custom watchlist
    -m, --mode              Tick mode (quote, full)
    -e, --exchange          Exchange (NSE, BSE, NFO)
  breadth                   Market breadth indicators (A/D, VIX, PCR)

LIVE STREAMING WATCHLISTS
  trader live RELIANCE INFY TCS           # Multiple symbols
  trader live --watchlist nifty50         # NIFTY 50 constituents (50 stocks)
  trader live --watchlist banknifty       # Bank NIFTY stocks (12 stocks)
  trader live --watchlist it              # IT sector (10 stocks)
  trader live --watchlist auto            # Auto sector (10 stocks)
  trader live --watchlist pharma          # Pharma sector (10 stocks)
  trader live --watchlist fmcg            # FMCG sector (10 stocks)
  trader live --watchlist default         # Your custom default watchlist
  trader live -w nifty50 --mode full      # Full tick data with market depth

ANALYSIS
  analyze <symbol>          Full technical analysis
    -t, --timeframe         Timeframe for analysis
    --detailed              Show detailed breakdown
  signal <symbol>           Composite signal score (-100 to +100)
  mtf <symbol>              Multi-timeframe analysis
  scan                      Scan stocks based on criteria
    --preset                Preset screener (momentum, oversold, breakout)
    --rsi-below/above       RSI filter
    --volume-above          Volume filter (multiplier of avg)
  research <symbol>         AI-powered research report

TRADING
  buy <symbol> <qty>        Place buy order
    -p, --price             Limit price (0 for market)
    --sl                    Stop-loss price
    --target                Target price
    --product               Product type (MIS, CNC, NRML)
  sell <symbol> <qty>       Place sell order
  positions                 View open positions
  holdings                  View delivery holdings
  orders                    View today's orders
  exit <symbol>             Exit a position
  exit-all                  Exit all positions (--force to confirm)

DERIVATIVES
  options chain <symbol>    Display option chain
    --expiry                Expiry date (YYYY-MM-DD)
    --strikes               Number of strikes around ATM
  options greeks            Calculate option Greeks
  options strategy          Option strategy builder
  options payoff            Display payoff diagram
  futures chain <symbol>    Display futures chain
  futures rollover          Roll futures to next expiry
  gtt create                Create GTT order
  gtt list                  List GTT orders
  gtt cancel <id>           Cancel GTT order
  bracket create            Create bracket order with SL and target

PLANNING
  plan add <symbol>         Create trade plan
    --entry                 Entry price
    --sl                    Stop-loss price
    --target/--t1/--t2/--t3 Target prices
    --qty                   Quantity
  plan list                 List trade plans
  plan execute <id>         Execute a trade plan
  plan cancel <id>          Cancel a trade plan
  prep                      Next-day trading preparation
    --watchlist             Watchlist to analyze
    --amo                   Place AMO orders for setups
  alert add <symbol>        Create price alert
    --above/--below         Price level
    --change                Percent change
  alert list                List active alerts
  events                    Corporate events calendar

MONITORING
  watch                     Interactive watch mode
    -w, --watchlist         Watchlist to monitor
  watchlist add <symbol>    Add to watchlist
  watchlist remove <symbol> Remove from watchlist
  watchlist list            List all watchlists
  watchlist create <name>   Create new watchlist

AI TRADING
  trader start              Start autonomous trading daemon
    --dry-run               Run without executing trades
    --watchlist             Watchlist to monitor
    --interval              Scan interval in seconds
  trader stop               Stop the daemon
  trader status             Show daemon status
  trader pause              Pause trading (analysis continues)
  trader resume             Resume trading
  trader config             View trader configuration
  trader health             System health diagnostics
  decisions list            List recent AI decisions
    --limit                 Max decisions to show
    --symbol                Filter by symbol
    --executed              Show only executed
    --days                  Filter by days
  decisions show <id>       Show decision details
  decisions stats           AI performance statistics

JOURNAL
  journal today             Show today's trades and notes
  journal add <trade-id>    Add analysis to a trade
  journal report            Generate performance report
    --period                daily, weekly, monthly
  journal search            Search journal entries

UTILITY
  backtest                  Backtest trading strategies
    --strategy              Strategy to test (momentum, breakout)
    --symbol                Symbol to backtest
    --days                  Number of days
    --capital               Starting capital
  export candles <symbol>   Export candle data to CSV
  export trades             Export trade history
  export journal            Export journal entries
  api start                 Start REST API server

INDIAN MARKET
  margin                    Margin utilization
  margin calc <symbol>      Calculate margin for order
  circuit <symbol>          Circuit limits and status
  settlement                T+1 settlement status
  surveillance <symbol>     ASM/GSM/T2T status
  expiry                    F&O expiry positions
  funds                     Fund summary
  pledge list               List pledgeable holdings
  pledge create             Create pledge request
  basket create <name>      Create basket
  basket order              Place basket order
  delivery <symbol>         Delivery analysis
  promoter <symbol>         Promoter holdings
  mf-holdings <symbol>      Mutual fund holdings
  bulk-deals                Bulk and block deals
  corporate-actions         Corporate actions calendar

CONFIG
  config show               Show current configuration
  config path               Show config directory
  config validate           Validate config files
  version                   Print version info

GLOBAL FLAGS
  --json                    Output in JSON format
  --debug                   Enable debug logging
  -e, --exchange            Exchange (NSE, BSE, NFO)
```

## Project Structure

```
zerodha-trader/
├── cmd/trader/
│   └── main.go                 # Entry point - initializes config, logger, runs CLI
│
├── internal/
│   ├── agents/                 # AI Trading Agents
│   │   ├── agent.go            # Agent interface, base types, AnalysisRequest/Result
│   │   ├── llm.go              # OpenAI client for GPT integration
│   │   ├── orchestrator.go     # Coordinates all agents, consensus, auto-execution
│   │   ├── technical.go        # Technical analysis agent (EMA, patterns, levels)
│   │   ├── research.go         # Fundamental research agent (PE, growth, targets)
│   │   ├── news.go             # News sentiment agent
│   │   ├── risk.go             # Risk assessment agent
│   │   └── trader.go           # Final decision maker, synthesizes all agents
│   │
│   ├── analysis/               # Technical Analysis Engine
│   │   ├── analysis.go         # Core analysis types
│   │   ├── indicators/         # Technical indicators
│   │   │   ├── engine.go       # Indicator calculation engine
│   │   │   ├── trend.go        # EMA, SMA, MACD, ADX
│   │   │   ├── momentum.go     # RSI, Stochastic, CCI
│   │   │   ├── volatility.go   # ATR, Bollinger Bands
│   │   │   ├── volume.go       # OBV, VWAP, Volume Profile
│   │   │   └── levels.go       # Support/Resistance detection
│   │   ├── patterns/           # Chart pattern detection
│   │   │   ├── candlestick.go  # Doji, Hammer, Engulfing, etc.
│   │   │   ├── chart.go        # Head & Shoulders, Triangles
│   │   │   ├── trend.go        # Trend lines, channels
│   │   │   ├── levels.go       # S/R zones
│   │   │   ├── volume.go       # Volume patterns
│   │   │   ├── divergence.go   # RSI/MACD divergence
│   │   │   └── priceaction.go  # Price action patterns
│   │   ├── mtf/                # Multi-timeframe analysis
│   │   │   └── mtf.go          # Combines signals across timeframes
│   │   └── scoring/            # Signal scoring
│   │       ├── scorer.go       # Composite signal scoring
│   │       └── screener.go     # Stock screening
│   │
│   ├── broker/                 # Broker Integration
│   │   ├── broker.go           # Broker interface (orders, quotes, positions)
│   │   ├── zerodha.go          # Zerodha Kite Connect implementation
│   │   ├── ticker.go           # WebSocket live streaming
│   │   ├── paper.go            # Paper trading simulator
│   │   └── segment.go          # Market segments (NSE, BSE, NFO)
│   │
│   ├── cli/                    # Command Line Interface
│   │   ├── root.go             # App struct, command registration
│   │   ├── auth.go             # login, logout, status commands
│   │   ├── data.go             # candles, quote, breadth commands
│   │   ├── analyze.go          # analyze, signal, scan, research commands
│   │   ├── trade.go            # buy, sell, bracket, gtt commands
│   │   ├── trader.go           # Autonomous trading daemon
│   │   ├── planning.go         # plan create/execute, prep commands
│   │   ├── derivatives.go      # options chain, futures chain
│   │   ├── monitoring.go       # watch, watchlist, live commands
│   │   ├── journal.go          # Trading journal commands
│   │   ├── utility.go          # backtest, export commands
│   │   ├── output.go           # Output formatting, colors, tables
│   │   ├── format.go           # Price/currency formatting
│   │   └── help.go             # Help and examples
│   │
│   ├── config/                 # Configuration
│   │   ├── config.go           # Config structs, loading, validation
│   │   └── templates.go        # Default config file templates
│   │
│   ├── models/                 # Domain Models
│   │   ├── models.go           # Exchange, OrderType, Candle, Quote, Tick
│   │   ├── order.go            # Order, GTTOrder, Position, Holding
│   │   ├── trade.go            # Trade execution records
│   │   ├── decision.go         # AI Decision, Consensus, RiskCheck
│   │   └── options.go          # Options chain models
│   │
│   ├── store/                  # Data Persistence
│   │   ├── store.go            # DataStore interface
│   │   ├── sqlite.go           # SQLite implementation
│   │   ├── sync.go             # Data synchronization
│   │   └── indian_market.go    # Indian market specific data
│   │
│   ├── trading/                # Trading Logic
│   │   ├── trading.go          # Core trading types
│   │   ├── execution.go        # Order execution, auto-execute checks
│   │   ├── backtest.go         # Backtesting engine
│   │   ├── portfolio.go        # Portfolio management
│   │   ├── position.go         # Position sizing
│   │   ├── exit.go             # Exit strategies
│   │   ├── prep.go             # Pre-market preparation
│   │   ├── pipeline.go         # Trading pipeline
│   │   ├── basket.go           # Basket orders
│   │   └── margin.go           # Margin calculations
│   │
│   ├── stream/                 # Real-time Streaming
│   │   ├── hub.go              # WebSocket hub, subscriptions
│   │   ├── alerts.go           # Price alerts
│   │   └── plans.go            # Trade plan monitoring
│   │
│   ├── notify/                 # Notifications
│   │   ├── notify.go           # Notifier interface
│   │   └── terminal.go         # Terminal notifications
│   │
│   ├── resilience/             # System Resilience
│   │   ├── circuitbreaker.go   # Circuit breaker pattern
│   │   └── health.go           # Health checks
│   │
│   ├── security/               # Security
│   │   ├── security.go         # Security utilities
│   │   ├── validation.go       # Input validation
│   │   ├── audit.go            # Audit logging
│   │   └── readonly.go         # Read-only mode
│   │
│   ├── logging/                # Logging
│   │   └── logging.go          # Zerolog setup
│   │
│   └── errors/                 # Error Handling
│       └── errors.go           # Custom error types
│
├── pkg/utils/                  # Shared Utilities
│   ├── format.go               # Formatting helpers
│   ├── market.go               # Market hours, holidays
│   └── retry.go                # Retry logic
│
└── Config files at ~/.config/zerodha-trader/
    ├── config.toml             # Main config (trading mode, risk)
    ├── credentials.toml        # API keys (Zerodha, OpenAI)
    ├── agents.toml             # AI agent config (model, thresholds)
    └── trader.db               # SQLite database
```

## Testing

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run property-based tests
go test -v ./internal/... -run Property
```

## Safety Features

- Paper trading mode for testing
- Daily loss limits
- Position size limits
- Consecutive loss circuit breaker
- Cooldown between trades
- Full audit trail of all decisions
- Read-only mode option

## Disclaimer

This software is for educational purposes only. Trading in financial markets involves substantial risk of loss. Past performance is not indicative of future results. Always do your own research and consider consulting a financial advisor before making investment decisions.

## License

MIT License
