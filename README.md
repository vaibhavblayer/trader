# Zerodha Go Trader

An AI-powered autonomous day trading CLI for the Indian stock market (NSE/BSE), built in Go with Zerodha Kite Connect API integration.

## Features

### Market Data
- Real-time quotes and live streaming
- Historical OHLCV data with multiple timeframes
- Market breadth indicators (A/D ratio, VIX, PCR)

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

## Configuration

On first run, config templates are created at `~/.config/zerodha-trader/`:

### credentials.toml
```toml
[zerodha]
api_key = "your_api_key"
api_secret = "your_api_secret"
user_id = "your_user_id"
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

## Usage

### Authentication
```bash
# Login (opens browser for OAuth)
./trader login

# Check login status
./trader balance
```

### Market Data
```bash
# Get quote
./trader quote RELIANCE

# Historical data
./trader data TCS -d 100 -t 1day

# Live streaming
./trader live RELIANCE INFY TCS
```

### Analysis
```bash
# Full technical analysis
./trader analyze RELIANCE

# Signal score
./trader signal TCS

# Multi-timeframe analysis
./trader mtf INFY

# AI research report
./trader research HDFC

# Scan for setups
./trader scan --preset momentum
./trader scan --rsi-below 30 --volume-above 2
```

### Trading
```bash
# Place orders
./trader buy RELIANCE --qty 10 --price 2450
./trader sell INFY --qty 5 --type MARKET

# View positions
./trader positions
./trader holdings

# Exit positions
./trader exit RELIANCE
./trader exit-all
```

### Planning
```bash
# Next-day preparation
./trader prep

# Trade plans
./trader plan add RELIANCE --entry 2450 --sl 2400 --target 2550
./trader plan list
./trader plan execute PLAN001
```

### AI Decisions
```bash
# View AI decisions
./trader decisions list
./trader decisions stats --days 30

# Autonomous trader daemon
./trader trader start
./trader trader status
./trader trader stop
```

### Indian Market
```bash
# Margin calculator
./trader margin RELIANCE --qty 100 --product MIS

# Circuit limits
./trader circuit RELIANCE

# F&O expiry
./trader expiry --index NIFTY

# Settlement info
./trader settlement RELIANCE

# Surveillance status
./trader surveillance RELIANCE
```

## Project Structure

```
├── cmd/trader/          # CLI entry point
├── internal/
│   ├── agents/          # AI trading agents
│   ├── analysis/        # Technical analysis
│   │   ├── indicators/  # RSI, MACD, BB, etc.
│   │   ├── patterns/    # Candlestick & chart patterns
│   │   ├── scoring/     # Signal scoring
│   │   └── mtf/         # Multi-timeframe
│   ├── broker/          # Zerodha & paper trading
│   ├── cli/             # Command implementations
│   ├── config/          # Configuration management
│   ├── models/          # Data models
│   ├── store/           # SQLite persistence
│   ├── stream/          # Real-time data hub
│   ├── trading/         # Order execution & Indian market
│   ├── resilience/      # Circuit breakers & health
│   ├── security/        # Audit & validation
│   └── notify/          # Notifications
└── pkg/utils/           # Shared utilities
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
│   │   ├── margin.go           # Margin calculations
│   │   └── ...                 # Other trading utilities
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
│   │   ├── health.go           # Health checks
│   │   └── ...                 # Other resilience utilities
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





## Disclaimer

This software is for educational purposes only. Trading in financial markets involves substantial risk of loss. Past performance is not indicative of future results. Always do your own research and consider consulting a financial advisor before making investment decisions.

## License

MIT License
