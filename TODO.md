# Trading Autonomy TODO

This list tracks the long-term fixes needed before this CLI should be trusted with autonomous live trading.

## In Progress

None.

## Later

- [ ] Migrate the general LLM client and tool-calling flows from Chat Completions to the OpenAI Responses API while preserving the current no-direct-live-order authority boundary.

## Done

- [x] Manual activation and pause controls for promoted paper candidates.
- [x] Repeatable paper soak loop command that reuses the candidate-aware soak-run engine.
- [x] Discovery-to-paper feedback loop with score-cohort paper outcome review and stale/no-forward-evidence auto-demotion through paper candidate review.
- [x] Evidence-aware candidate scoring that combines technical discovery, signal activity, backtest quality, regime expectancy, and cited LLM research evidence into one promotion score.
- [x] LLM web-research evidence layer using OpenAI Responses API web search for cited news, catalyst, event-risk, and sector-context reports with no direct trade authority.
- [x] Intraday discovery workflow with configurable scan timeframe, lookback, minimum candles, candle freshness filters, and candle metadata in discovery output.
- [x] Backtest/discovery signal-rate metrics for BUY/SELL/HOLD activity, signal rate, and trade conversion by grid row and promoted paper candidate.
- [x] Strategy signal diagnostics for latest-bar no-signal outcomes, including gate-level reasons for multi-indicator, SuperTrend, and Donchian discovery candidates.
- [x] Autonomy readiness report with pass/warn/block decisions across safety, kill switch, calibration, execution quality, and post-trade review.
- [x] Paper soak workflow for readiness-gated soak planning, status, and reporting.
- [x] Removed the unimplemented public `api start` surface.
- [x] Disabled portfolio beta, VaR, Greeks, and hedge placeholder workflows until data-backed analytics are implemented.
- [x] Post-trade review workflow that links setup, order, fill, exit, execution quality, and P&L.
- [x] Paper prediction calibration and expectancy reports by confidence, action, and symbol.
- [x] Historical calibration and per-gate expectancy reports by setup, gate, symbol, timeframe, and action.
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
