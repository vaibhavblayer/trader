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

## Disclaimer

This software is for educational purposes only. Trading in financial markets involves substantial risk of loss. Past performance is not indicative of future results. Always do your own research and consider consulting a financial advisor before making investment decisions.

## License

MIT License
