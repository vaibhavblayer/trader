# Trading Autonomy TODO

This list tracks the long-term fixes needed before this CLI should be trusted with autonomous live trading.

## In Progress

None.

## Later

- [ ] Historical calibration per setup/gate/symbol/timeframe.
- [ ] Per-gate expectancy reports.
- [ ] Implement broker-backed REST API server or remove the public `api start` surface.
- [ ] Replace portfolio beta, VaR, Greeks, and hedge placeholders with data-backed analytics before exposing them in active workflows.
- [ ] Post-trade review workflow that links setup, order, fill, exit, and P&L.

## Done

- [x] Paper prediction calibration and expectancy reports by confidence, action, and symbol.
- [x] Kill-switch command and persistent daemon state.
- [x] Slippage and execution-quality reports by symbol, side, and order type.
- [x] Persistent paper prediction tracker history and accuracy restore for live paper mode.
- [x] Persistent paper broker ledger for orders, GTTs, positions, balances, and paper execution events.
- [x] LLM role reduction: explanation, summarization, news classification, and review only; no direct live order authority.
- [x] Separate safety profiles for backtest, paper, live-readonly, and live-trading.
- [x] Data quality gates for stale candles, missing candles, bad volume, market session mismatch, and symbol/token mismatch.
- [x] Broker execution hardening: idempotency, duplicate-order prevention, order reconciliation, partial-fill handling, and safe retries.
- [x] Strategy registry with separate setup modules, gates, risk model, and metrics per strategy.
- [x] More realistic paper trading simulator using spread, slippage, fees, and depth-based partial fills instead of ideal fills.
- [x] Event-based backtesting engine with next-bar execution, transaction costs, slippage, partial fills, and explicit execution assumptions.
- [x] Structured trade decision log for generated, risk-checked, execution-selected, broker-submitted, and protective-order decision events.
- [x] Hard risk manager before manual, planned, and autonomous broker execution paths.
