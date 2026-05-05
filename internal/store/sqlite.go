// Package store provides data persistence implementations.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

// SQLiteStore implements DataStore using SQLite.
type SQLiteStore struct {
	db        *sql.DB
	mu        sync.RWMutex
	syncTimes map[string]time.Time
}

// NewSQLiteStore creates a new SQLite-based data store.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for concurrent access
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	store := &SQLiteStore{
		db:        db,
		syncTimes: make(map[string]time.Time),
	}

	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

// initSchema creates all required tables and indexes.
func (s *SQLiteStore) initSchema() error {
	schema := `
	-- Candles table for historical OHLCV data
	CREATE TABLE IF NOT EXISTS candles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT NOT NULL,
		timeframe TEXT NOT NULL,
		timestamp DATETIME NOT NULL,
		open REAL NOT NULL,
		high REAL NOT NULL,
		low REAL NOT NULL,
		close REAL NOT NULL,
		volume INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(symbol, timeframe, timestamp)
	);

	-- Trades table for completed trades
	CREATE TABLE IF NOT EXISTS trades (
		id TEXT PRIMARY KEY,
		timestamp DATETIME NOT NULL,
		symbol TEXT NOT NULL,
		exchange TEXT NOT NULL,
		side TEXT NOT NULL,
		product TEXT NOT NULL,
		quantity INTEGER NOT NULL,
		entry_price REAL NOT NULL,
		exit_price REAL,
		pnl REAL,
		pnl_percent REAL,
		strategy TEXT,
		order_ids TEXT,
		is_paper INTEGER DEFAULT 0,
		decision_id TEXT,
		hold_duration INTEGER,
		slippage REAL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Trade analysis table
	CREATE TABLE IF NOT EXISTS trade_analysis (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trade_id TEXT NOT NULL UNIQUE,
		what_went_right TEXT,
		what_went_wrong TEXT,
		lessons_learned TEXT,
		entry_quality INTEGER,
		exit_quality INTEGER,
		risk_management_score INTEGER,
		emotional_notes TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (trade_id) REFERENCES trades(id)
	);

	-- Trade context table
	CREATE TABLE IF NOT EXISTS trade_context (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trade_id TEXT NOT NULL UNIQUE,
		nifty_level REAL,
		sector_index REAL,
		vix_level REAL,
		market_trend TEXT,
		news_events TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (trade_id) REFERENCES trades(id)
	);

	-- Agent decisions table
	CREATE TABLE IF NOT EXISTS agent_decisions (
		id TEXT PRIMARY KEY,
		timestamp DATETIME NOT NULL,
		symbol TEXT NOT NULL,
		action TEXT NOT NULL,
		confidence REAL NOT NULL,
		agent_results TEXT,
		consensus TEXT,
		risk_check TEXT,
		executed INTEGER DEFAULT 0,
		order_id TEXT,
		outcome TEXT DEFAULT 'PENDING',
		pnl REAL,
		reasoning TEXT,
		market_condition TEXT,
		entry_price REAL,
		stop_loss REAL,
		targets TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Append-only decision lifecycle log
	CREATE TABLE IF NOT EXISTS decision_logs (
		id TEXT PRIMARY KEY,
		decision_id TEXT NOT NULL,
		timestamp DATETIME NOT NULL,
		stage TEXT NOT NULL,
		symbol TEXT NOT NULL,
		action TEXT NOT NULL,
		status TEXT NOT NULL,
		message TEXT,
		payload TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (decision_id) REFERENCES agent_decisions(id)
	);

	-- Trade plans table
	CREATE TABLE IF NOT EXISTS trade_plans (
		id TEXT PRIMARY KEY,
		symbol TEXT NOT NULL,
		side TEXT NOT NULL,
		entry_price REAL NOT NULL,
		stop_loss REAL NOT NULL,
		target1 REAL,
		target2 REAL,
		target3 REAL,
		quantity INTEGER NOT NULL,
		risk_reward REAL,
		status TEXT DEFAULT 'PENDING',
		notes TEXT,
		reasoning TEXT,
		source TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		executed_at DATETIME
	);

	-- Watchlist table
	CREATE TABLE IF NOT EXISTS watchlist (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT NOT NULL,
		list_name TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(symbol, list_name)
	);

	-- Alerts table
	CREATE TABLE IF NOT EXISTS alerts (
		id TEXT PRIMARY KEY,
		symbol TEXT NOT NULL,
		condition TEXT NOT NULL,
		price REAL NOT NULL,
		triggered INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		triggered_at DATETIME
	);

	-- Journal entries table
	CREATE TABLE IF NOT EXISTS journal (
		id TEXT PRIMARY KEY,
		trade_id TEXT,
		date DATE NOT NULL,
		content TEXT NOT NULL,
		tags TEXT,
		mood TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Corporate events table
	CREATE TABLE IF NOT EXISTS events (
		id TEXT PRIMARY KEY,
		symbol TEXT NOT NULL,
		event_type TEXT NOT NULL,
		date DATE NOT NULL,
		description TEXT,
		details TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Screener queries table
	CREATE TABLE IF NOT EXISTS screener_queries (
		name TEXT PRIMARY KEY,
		filters TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Execution quality table
	CREATE TABLE IF NOT EXISTS execution_quality (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		order_id TEXT NOT NULL,
		symbol TEXT NOT NULL,
		expected_price REAL NOT NULL,
		actual_price REAL NOT NULL,
		slippage REAL NOT NULL,
		latency_ms INTEGER,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Health logs table
	CREATE TABLE IF NOT EXISTS health_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		component TEXT NOT NULL,
		status TEXT NOT NULL,
		message TEXT,
		metrics TEXT
	);

	-- Durable paper trading account snapshot
	CREATE TABLE IF NOT EXISTS paper_state (
		id TEXT PRIMARY KEY,
		state TEXT NOT NULL,
		updated_at DATETIME NOT NULL
	);

	-- Append-only paper trading ledger
	CREATE TABLE IF NOT EXISTS paper_ledger (
		id TEXT PRIMARY KEY,
		timestamp DATETIME NOT NULL,
		type TEXT NOT NULL,
		ref_id TEXT,
		symbol TEXT,
		payload TEXT
	);

	-- Paper prediction tracker
	CREATE TABLE IF NOT EXISTS paper_predictions (
		id TEXT PRIMARY KEY,
		symbol TEXT NOT NULL,
		action TEXT NOT NULL,
		confidence REAL NOT NULL,
		entry_price REAL NOT NULL,
		target_price REAL NOT NULL,
		stop_loss REAL NOT NULL,
		time_window_ns INTEGER NOT NULL,
		created_at DATETIME NOT NULL,
		expires_at DATETIME NOT NULL,
		reasoning TEXT,
		setup_name TEXT NOT NULL,
		timeframe TEXT NOT NULL,
		gates TEXT NOT NULL DEFAULT '[]',
		evaluated INTEGER DEFAULT 0,
		exit_price REAL,
		outcome TEXT,
		pnl_percent REAL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Promoted backtest candidates for disciplined paper soak
	CREATE TABLE IF NOT EXISTS paper_candidates (
		id TEXT PRIMARY KEY,
		status TEXT NOT NULL,
		symbol TEXT NOT NULL,
		exchange TEXT NOT NULL,
		strategy TEXT NOT NULL,
		param_variant TEXT NOT NULL,
		parameters TEXT,
		timeframe TEXT NOT NULL,
		setup TEXT NOT NULL,
		source TEXT NOT NULL,
		verdict TEXT NOT NULL,
		reason TEXT,
		days INTEGER NOT NULL,
		candles INTEGER NOT NULL,
		trades INTEGER NOT NULL,
		validation_trades INTEGER NOT NULL,
		return_pct REAL NOT NULL,
		train_return_pct REAL NOT NULL,
		validation_return_pct REAL NOT NULL,
		win_rate REAL NOT NULL,
		profit_factor REAL NOT NULL,
		expectancy REAL NOT NULL,
		max_drawdown_pct REAL NOT NULL,
		sharpe_ratio REAL NOT NULL,
		stop_loss_percent REAL,
		take_profit_percent REAL,
		trailing_stop_percent REAL,
		allow_short INTEGER DEFAULT 0,
		allowed_regimes TEXT NOT NULL DEFAULT '[]',
		blocked_regimes TEXT NOT NULL DEFAULT '[]',
		regime_stats TEXT NOT NULL DEFAULT '[]',
		promoted_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);

	-- Durable autonomous daemon state and control gates
	CREATE TABLE IF NOT EXISTS daemon_state (
		id TEXT PRIMARY KEY,
		state TEXT NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS daemon_events (
		id TEXT PRIMARY KEY,
		timestamp DATETIME NOT NULL,
		type TEXT NOT NULL,
		status TEXT NOT NULL,
		reason TEXT,
		actor TEXT,
		pid INTEGER,
		hostname TEXT
	);

	-- Sync status table
	CREATE TABLE IF NOT EXISTS sync_status (
		data_type TEXT PRIMARY KEY,
		last_sync DATETIME NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Create indexes for performance
	CREATE INDEX IF NOT EXISTS idx_candles_symbol_timeframe ON candles(symbol, timeframe);
	CREATE INDEX IF NOT EXISTS idx_candles_timestamp ON candles(timestamp);
	CREATE INDEX IF NOT EXISTS idx_trades_symbol ON trades(symbol);
	CREATE INDEX IF NOT EXISTS idx_trades_timestamp ON trades(timestamp);
	CREATE INDEX IF NOT EXISTS idx_decisions_symbol ON agent_decisions(symbol);
	CREATE INDEX IF NOT EXISTS idx_decisions_timestamp ON agent_decisions(timestamp);
	CREATE INDEX IF NOT EXISTS idx_decision_logs_decision ON decision_logs(decision_id, timestamp);
	CREATE INDEX IF NOT EXISTS idx_plans_symbol ON trade_plans(symbol);
	CREATE INDEX IF NOT EXISTS idx_plans_status ON trade_plans(status);
	CREATE INDEX IF NOT EXISTS idx_alerts_symbol ON alerts(symbol);
	CREATE INDEX IF NOT EXISTS idx_alerts_triggered ON alerts(triggered);
	CREATE INDEX IF NOT EXISTS idx_events_symbol ON events(symbol);
	CREATE INDEX IF NOT EXISTS idx_events_date ON events(date);
	CREATE INDEX IF NOT EXISTS idx_journal_date ON journal(date);
	CREATE INDEX IF NOT EXISTS idx_watchlist_list ON watchlist(list_name);
	CREATE INDEX IF NOT EXISTS idx_paper_ledger_timestamp ON paper_ledger(timestamp);
	CREATE INDEX IF NOT EXISTS idx_paper_ledger_ref ON paper_ledger(ref_id);
	CREATE INDEX IF NOT EXISTS idx_paper_predictions_symbol ON paper_predictions(symbol);
	CREATE INDEX IF NOT EXISTS idx_paper_predictions_created ON paper_predictions(created_at);
	CREATE INDEX IF NOT EXISTS idx_paper_predictions_evaluated ON paper_predictions(evaluated);
	CREATE INDEX IF NOT EXISTS idx_paper_candidates_symbol ON paper_candidates(symbol);
	CREATE INDEX IF NOT EXISTS idx_paper_candidates_status ON paper_candidates(status);
	CREATE INDEX IF NOT EXISTS idx_paper_candidates_strategy ON paper_candidates(strategy);
	CREATE INDEX IF NOT EXISTS idx_daemon_events_timestamp ON daemon_events(timestamp);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	if _, err := s.db.Exec(`
		UPDATE alerts
		SET id = 'ALERT_' || rowid || '_' || strftime('%s', COALESCE(created_at, CURRENT_TIMESTAMP))
		WHERE id IS NULL OR TRIM(id) = ''
	`); err != nil {
		return fmt.Errorf("failed to backfill alert IDs: %w", err)
	}

	return nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// ============================================================================
// Candles Methods
// ============================================================================

// SaveCandles saves candles to the database.
func (s *SQLiteStore) SaveCandles(ctx context.Context, symbol, timeframe string, candles []models.Candle) error {
	if len(candles) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO candles (symbol, timeframe, timestamp, open, high, low, close, volume)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, c := range candles {
		_, err := stmt.ExecContext(ctx, symbol, timeframe, c.Timestamp, c.Open, c.High, c.Low, c.Close, c.Volume)
		if err != nil {
			return fmt.Errorf("failed to insert candle: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetCandles retrieves candles from the database.
func (s *SQLiteStore) GetCandles(ctx context.Context, symbol, timeframe string, from, to time.Time) ([]models.Candle, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT timestamp, open, high, low, close, volume
		FROM candles
		WHERE symbol = ? AND timeframe = ? AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp ASC
	`, symbol, timeframe, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to query candles: %w", err)
	}
	defer rows.Close()

	var candles []models.Candle
	for rows.Next() {
		var c models.Candle
		if err := rows.Scan(&c.Timestamp, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume); err != nil {
			return nil, fmt.Errorf("failed to scan candle: %w", err)
		}
		candles = append(candles, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating candles: %w", err)
	}

	return candles, nil
}

// GetCandlesFreshness returns the timestamp of the most recent candle.
func (s *SQLiteStore) GetCandlesFreshness(ctx context.Context, symbol, timeframe string) (time.Time, error) {
	var timestamp sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT MAX(timestamp) FROM candles WHERE symbol = ? AND timeframe = ?
	`, symbol, timeframe).Scan(&timestamp)
	if err != nil && err != sql.ErrNoRows {
		return time.Time{}, fmt.Errorf("failed to get candles freshness: %w", err)
	}
	if !timestamp.Valid {
		return time.Time{}, nil
	}
	return timestamp.Time, nil
}

// ============================================================================
// Trades Methods
// ============================================================================

// LogTrade saves a trade to the database.
func (s *SQLiteStore) LogTrade(ctx context.Context, trade *models.Trade) error {
	orderIDs, _ := json.Marshal(trade.OrderIDs)
	isPaper := 0
	if trade.IsPaper {
		isPaper = 1
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO trades (id, timestamp, symbol, exchange, side, product, quantity, entry_price, exit_price, pnl, pnl_percent, strategy, order_ids, is_paper, decision_id, hold_duration, slippage)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, trade.ID, trade.Timestamp, trade.Symbol, trade.Exchange, trade.Side, trade.Product, trade.Quantity, trade.EntryPrice, trade.ExitPrice, trade.PnL, trade.PnLPercent, trade.Strategy, string(orderIDs), isPaper, trade.DecisionID, trade.HoldDuration.Nanoseconds(), trade.Slippage)
	if err != nil {
		return fmt.Errorf("failed to log trade: %w", err)
	}
	return nil
}

// GetTrades retrieves trades from the database.
func (s *SQLiteStore) GetTrades(ctx context.Context, filter TradeFilter) ([]models.Trade, error) {
	query := "SELECT id, timestamp, symbol, exchange, side, product, quantity, entry_price, exit_price, pnl, pnl_percent, strategy, order_ids, is_paper, decision_id, hold_duration, slippage FROM trades WHERE 1=1"
	args := []interface{}{}

	if filter.Symbol != "" {
		query += " AND symbol = ?"
		args = append(args, filter.Symbol)
	}
	if !filter.StartDate.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, filter.StartDate)
	}
	if !filter.EndDate.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, filter.EndDate)
	}
	if filter.Side != "" {
		query += " AND side = ?"
		args = append(args, filter.Side)
	}
	if filter.IsPaper != nil {
		isPaper := 0
		if *filter.IsPaper {
			isPaper = 1
		}
		query += " AND is_paper = ?"
		args = append(args, isPaper)
	}

	query += " ORDER BY timestamp DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query trades: %w", err)
	}
	defer rows.Close()

	var trades []models.Trade
	for rows.Next() {
		var t models.Trade
		var orderIDsJSON string
		var isPaper int
		var holdDurationNs int64

		if err := rows.Scan(&t.ID, &t.Timestamp, &t.Symbol, &t.Exchange, &t.Side, &t.Product, &t.Quantity, &t.EntryPrice, &t.ExitPrice, &t.PnL, &t.PnLPercent, &t.Strategy, &orderIDsJSON, &isPaper, &t.DecisionID, &holdDurationNs, &t.Slippage); err != nil {
			return nil, fmt.Errorf("failed to scan trade: %w", err)
		}

		json.Unmarshal([]byte(orderIDsJSON), &t.OrderIDs)
		t.IsPaper = isPaper == 1
		t.HoldDuration = time.Duration(holdDurationNs)
		trades = append(trades, t)
	}

	return trades, rows.Err()
}

// SaveTradeAnalysis saves trade analysis to the database.
func (s *SQLiteStore) SaveTradeAnalysis(ctx context.Context, analysis *models.TradeAnalysis) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO trade_analysis (trade_id, what_went_right, what_went_wrong, lessons_learned, entry_quality, exit_quality, risk_management_score, emotional_notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, analysis.TradeID, analysis.WhatWentRight, analysis.WhatWentWrong, analysis.LessonsLearned, analysis.EntryQuality, analysis.ExitQuality, analysis.RiskManagementScore, analysis.EmotionalNotes)
	if err != nil {
		return fmt.Errorf("failed to save trade analysis: %w", err)
	}

	if analysis.MarketContext != nil {
		_, err = s.db.ExecContext(ctx, `
			INSERT OR REPLACE INTO trade_context (trade_id, nifty_level, sector_index, vix_level, market_trend, news_events)
			VALUES (?, ?, ?, ?, ?, ?)
		`, analysis.TradeID, analysis.MarketContext.NiftyLevel, analysis.MarketContext.SectorIndex, analysis.MarketContext.VIXLevel, analysis.MarketContext.MarketTrend, analysis.MarketContext.NewsEvents)
		if err != nil {
			return fmt.Errorf("failed to save trade context: %w", err)
		}
	}

	return nil
}

// SaveJournalEntry saves a journal entry to the database.
func (s *SQLiteStore) SaveJournalEntry(ctx context.Context, entry *models.JournalEntry) error {
	tags, _ := json.Marshal(entry.Tags)
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO journal (id, trade_id, date, content, tags, mood, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, entry.ID, entry.TradeID, entry.Date, entry.Content, string(tags), entry.Mood, entry.CreatedAt, entry.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to save journal entry: %w", err)
	}
	return nil
}

// GetJournal retrieves journal entries from the database.
func (s *SQLiteStore) GetJournal(ctx context.Context, filter JournalFilter) ([]models.JournalEntry, error) {
	query := "SELECT id, trade_id, date, content, tags, mood, created_at, updated_at FROM journal WHERE 1=1"
	args := []interface{}{}

	if filter.TradeID != "" {
		query += " AND trade_id = ?"
		args = append(args, filter.TradeID)
	}
	if !filter.StartDate.IsZero() {
		query += " AND date >= ?"
		args = append(args, filter.StartDate)
	}
	if !filter.EndDate.IsZero() {
		query += " AND date <= ?"
		args = append(args, filter.EndDate)
	}

	query += " ORDER BY date DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query journal: %w", err)
	}
	defer rows.Close()

	var entries []models.JournalEntry
	for rows.Next() {
		var e models.JournalEntry
		var tagsJSON string
		if err := rows.Scan(&e.ID, &e.TradeID, &e.Date, &e.Content, &tagsJSON, &e.Mood, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan journal entry: %w", err)
		}
		json.Unmarshal([]byte(tagsJSON), &e.Tags)
		entries = append(entries, e)
	}

	return entries, rows.Err()
}

// ============================================================================
// Trade Plans Methods
// ============================================================================

// SavePlan saves a trade plan to the database.
func (s *SQLiteStore) SavePlan(ctx context.Context, plan *models.TradePlan) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO trade_plans (id, symbol, side, entry_price, stop_loss, target1, target2, target3, quantity, risk_reward, status, notes, reasoning, source, created_at, executed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, plan.ID, plan.Symbol, plan.Side, plan.EntryPrice, plan.StopLoss, plan.Target1, plan.Target2, plan.Target3, plan.Quantity, plan.RiskReward, plan.Status, plan.Notes, plan.Reasoning, plan.Source, plan.CreatedAt, plan.ExecutedAt)
	if err != nil {
		return fmt.Errorf("failed to save trade plan: %w", err)
	}
	return nil
}

// GetPlans retrieves trade plans from the database.
func (s *SQLiteStore) GetPlans(ctx context.Context, filter PlanFilter) ([]models.TradePlan, error) {
	query := "SELECT id, symbol, side, entry_price, stop_loss, target1, target2, target3, quantity, risk_reward, status, notes, reasoning, source, created_at, executed_at FROM trade_plans WHERE 1=1"
	args := []interface{}{}

	if filter.Symbol != "" {
		query += " AND symbol = ?"
		args = append(args, filter.Symbol)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.Source != "" {
		query += " AND source = ?"
		args = append(args, filter.Source)
	}

	query += " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query trade plans: %w", err)
	}
	defer rows.Close()

	var plans []models.TradePlan
	for rows.Next() {
		var p models.TradePlan
		if err := rows.Scan(&p.ID, &p.Symbol, &p.Side, &p.EntryPrice, &p.StopLoss, &p.Target1, &p.Target2, &p.Target3, &p.Quantity, &p.RiskReward, &p.Status, &p.Notes, &p.Reasoning, &p.Source, &p.CreatedAt, &p.ExecutedAt); err != nil {
			return nil, fmt.Errorf("failed to scan trade plan: %w", err)
		}
		plans = append(plans, p)
	}

	return plans, rows.Err()
}

// UpdatePlanStatus updates the status of a trade plan.
func (s *SQLiteStore) UpdatePlanStatus(ctx context.Context, planID string, status models.PlanStatus) error {
	var executedAt interface{}
	if status == models.PlanExecuted {
		executedAt = time.Now()
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE trade_plans SET status = ?, executed_at = ? WHERE id = ?
	`, status, executedAt, planID)
	if err != nil {
		return fmt.Errorf("failed to update plan status: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("trade plan not found: %s", planID)
	}

	return nil
}

// ============================================================================
// AI Decisions Methods
// ============================================================================

// SaveDecision saves an AI decision to the database.
func (s *SQLiteStore) SaveDecision(ctx context.Context, decision *models.Decision) error {
	agentResults, _ := json.Marshal(decision.AgentResults)
	consensus, _ := json.Marshal(decision.Consensus)
	riskCheck, _ := json.Marshal(decision.RiskCheck)
	targets, _ := json.Marshal(decision.Targets)
	executed := 0
	if decision.Executed {
		executed = 1
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO agent_decisions (id, timestamp, symbol, action, confidence, agent_results, consensus, risk_check, executed, order_id, outcome, pnl, reasoning, market_condition, entry_price, stop_loss, targets)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, decision.ID, decision.Timestamp, decision.Symbol, decision.Action, decision.Confidence, string(agentResults), string(consensus), string(riskCheck), executed, decision.OrderID, decision.Outcome, decision.PnL, decision.Reasoning, decision.MarketCondition, decision.EntryPrice, decision.StopLoss, string(targets))
	if err != nil {
		return fmt.Errorf("failed to save decision: %w", err)
	}
	return nil
}

// SaveDecisionLog appends a lifecycle event for a trading decision.
func (s *SQLiteStore) SaveDecisionLog(ctx context.Context, log *models.DecisionLog) error {
	if log == nil {
		return fmt.Errorf("decision log is required")
	}
	if log.ID == "" {
		log.ID = fmt.Sprintf("DLOG_%d", time.Now().UnixNano())
	}
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now()
	}
	payload, err := json.Marshal(log.Payload)
	if err != nil {
		return fmt.Errorf("marshal decision log payload: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO decision_logs (id, decision_id, timestamp, stage, symbol, action, status, message, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, log.ID, log.DecisionID, log.Timestamp, log.Stage, log.Symbol, log.Action, log.Status, log.Message, string(payload))
	if err != nil {
		return fmt.Errorf("failed to save decision log: %w", err)
	}
	return nil
}

// GetDecisionLogs retrieves lifecycle events for a trading decision in chronological order.
func (s *SQLiteStore) GetDecisionLogs(ctx context.Context, decisionID string) ([]models.DecisionLog, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, decision_id, timestamp, stage, symbol, action, status, COALESCE(message, ''), COALESCE(payload, '{}')
		FROM decision_logs
		WHERE decision_id = ?
		ORDER BY timestamp ASC, created_at ASC
	`, decisionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query decision logs: %w", err)
	}
	defer rows.Close()

	var logs []models.DecisionLog
	for rows.Next() {
		var log models.DecisionLog
		var payloadJSON string
		if err := rows.Scan(&log.ID, &log.DecisionID, &log.Timestamp, &log.Stage, &log.Symbol, &log.Action, &log.Status, &log.Message, &payloadJSON); err != nil {
			return nil, fmt.Errorf("failed to scan decision log: %w", err)
		}
		if err := json.Unmarshal([]byte(payloadJSON), &log.Payload); err != nil {
			log.Payload = map[string]interface{}{"raw": payloadJSON}
		}
		logs = append(logs, log)
	}

	return logs, rows.Err()
}

// GetDecisions retrieves AI decisions from the database.
func (s *SQLiteStore) GetDecisions(ctx context.Context, filter DecisionFilter) ([]models.Decision, error) {
	query := "SELECT id, timestamp, symbol, action, confidence, agent_results, consensus, risk_check, executed, order_id, outcome, pnl, reasoning, COALESCE(market_condition, ''), COALESCE(entry_price, 0), COALESCE(stop_loss, 0), COALESCE(targets, '[]') FROM agent_decisions WHERE 1=1"
	args := []interface{}{}

	if filter.Symbol != "" {
		query += " AND symbol = ?"
		args = append(args, filter.Symbol)
	}
	if !filter.StartDate.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, filter.StartDate)
	}
	if !filter.EndDate.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, filter.EndDate)
	}
	if filter.Executed != nil {
		executed := 0
		if *filter.Executed {
			executed = 1
		}
		query += " AND executed = ?"
		args = append(args, executed)
	}
	if filter.Outcome != "" {
		query += " AND outcome = ?"
		args = append(args, filter.Outcome)
	}

	query += " ORDER BY timestamp DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query decisions: %w", err)
	}
	defer rows.Close()

	var decisions []models.Decision
	for rows.Next() {
		var d models.Decision
		var agentResultsJSON, consensusJSON, riskCheckJSON, targetsJSON string
		var executed int

		if err := rows.Scan(&d.ID, &d.Timestamp, &d.Symbol, &d.Action, &d.Confidence, &agentResultsJSON, &consensusJSON, &riskCheckJSON, &executed, &d.OrderID, &d.Outcome, &d.PnL, &d.Reasoning, &d.MarketCondition, &d.EntryPrice, &d.StopLoss, &targetsJSON); err != nil {
			return nil, fmt.Errorf("failed to scan decision: %w", err)
		}

		json.Unmarshal([]byte(agentResultsJSON), &d.AgentResults)
		json.Unmarshal([]byte(consensusJSON), &d.Consensus)
		json.Unmarshal([]byte(riskCheckJSON), &d.RiskCheck)
		json.Unmarshal([]byte(targetsJSON), &d.Targets)
		d.Executed = executed == 1
		decisions = append(decisions, d)
	}

	return decisions, rows.Err()
}

// GetDecisionStats retrieves AI decision statistics.
func (s *SQLiteStore) GetDecisionStats(ctx context.Context, dateRange DateRange) (*models.AIStats, error) {
	stats := &models.AIStats{
		ByAgent:           make(map[string]*models.AgentStats),
		ByMarketCondition: make(map[string]*models.ConditionStats),
	}

	// Get total decisions and executed trades
	var executedTrades sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), SUM(CASE WHEN executed = 1 THEN 1 ELSE 0 END)
		FROM agent_decisions
		WHERE timestamp >= ? AND timestamp <= ?
	`, dateRange.Start, dateRange.End).Scan(&stats.TotalDecisions, &executedTrades)
	if err != nil {
		return nil, fmt.Errorf("failed to get decision stats: %w", err)
	}
	if executedTrades.Valid {
		stats.ExecutedTrades = int(executedTrades.Int64)
	}

	// Get win rate and average P&L
	var winRate, avgPnL, avgConfidence sql.NullFloat64
	err = s.db.QueryRowContext(ctx, `
		SELECT 
			COALESCE(AVG(CASE WHEN outcome = 'WIN' THEN 1.0 ELSE 0.0 END) * 100, 0),
			COALESCE(AVG(pnl), 0),
			COALESCE(AVG(confidence), 0)
		FROM agent_decisions
		WHERE timestamp >= ? AND timestamp <= ? AND executed = 1
	`, dateRange.Start, dateRange.End).Scan(&winRate, &avgPnL, &avgConfidence)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get win rate stats: %w", err)
	}
	if winRate.Valid {
		stats.WinRate = winRate.Float64
	}
	if avgPnL.Valid {
		stats.AvgPnL = avgPnL.Float64
	}
	if avgConfidence.Valid {
		stats.AvgConfidence = avgConfidence.Float64
	}

	// Get stats by market condition
	rows, err := s.db.QueryContext(ctx, `
		SELECT 
			COALESCE(market_condition, 'UNKNOWN'),
			COUNT(*),
			COALESCE(AVG(CASE WHEN outcome = 'WIN' THEN 1.0 ELSE 0.0 END) * 100, 0),
			COALESCE(AVG(pnl), 0)
		FROM agent_decisions
		WHERE timestamp >= ? AND timestamp <= ? AND executed = 1 AND outcome IN ('WIN', 'LOSS')
		GROUP BY market_condition
	`, dateRange.Start, dateRange.End)
	if err != nil {
		return nil, fmt.Errorf("failed to get market condition stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var condition string
		var totalTrades int
		var condWinRate, condAvgPnL float64
		if err := rows.Scan(&condition, &totalTrades, &condWinRate, &condAvgPnL); err != nil {
			return nil, fmt.Errorf("failed to scan market condition stats: %w", err)
		}
		if condition == "" {
			condition = "UNKNOWN"
		}
		stats.ByMarketCondition[condition] = &models.ConditionStats{
			Condition:   condition,
			TotalTrades: totalTrades,
			WinRate:     condWinRate,
			AvgPnL:      condAvgPnL,
		}
	}

	// Get agent stats by parsing agent_results JSON
	// We need to get all decisions and parse agent results to calculate per-agent accuracy
	decisionRows, err := s.db.QueryContext(ctx, `
		SELECT agent_results, outcome
		FROM agent_decisions
		WHERE timestamp >= ? AND timestamp <= ? AND executed = 1 AND outcome IN ('WIN', 'LOSS')
	`, dateRange.Start, dateRange.End)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent stats: %w", err)
	}
	defer decisionRows.Close()

	// Track agent performance
	agentCalls := make(map[string]int)
	agentCorrect := make(map[string]int)
	agentConfidenceSum := make(map[string]float64)

	for decisionRows.Next() {
		var agentResultsJSON string
		var outcome string
		if err := decisionRows.Scan(&agentResultsJSON, &outcome); err != nil {
			continue
		}

		var agentResults map[string]*models.AgentResult
		if err := json.Unmarshal([]byte(agentResultsJSON), &agentResults); err != nil {
			continue
		}

		isWin := outcome == "WIN"
		for agentName, result := range agentResults {
			if result == nil {
				continue
			}
			agentCalls[agentName]++
			agentConfidenceSum[agentName] += result.Confidence

			// Check if agent's recommendation was correct
			// BUY recommendation is correct if trade was a WIN
			// SELL recommendation is correct if trade was a WIN (assuming we followed it)
			// HOLD is neutral
			if result.Recommendation != "HOLD" {
				if isWin {
					agentCorrect[agentName]++
				}
			}
		}
	}

	// Calculate agent stats
	for agentName, calls := range agentCalls {
		correct := agentCorrect[agentName]
		accuracy := 0.0
		if calls > 0 {
			accuracy = float64(correct) / float64(calls) * 100
		}
		avgConf := 0.0
		if calls > 0 {
			avgConf = agentConfidenceSum[agentName] / float64(calls)
		}
		stats.ByAgent[agentName] = &models.AgentStats{
			Name:          agentName,
			TotalCalls:    calls,
			CorrectCalls:  correct,
			Accuracy:      accuracy,
			AvgConfidence: avgConf,
		}
	}

	return stats, nil
}

// GetDecisionByID retrieves a single decision by ID.
func (s *SQLiteStore) GetDecisionByID(ctx context.Context, id string) (*models.Decision, error) {
	var d models.Decision
	var agentResultsJSON, consensusJSON, riskCheckJSON, targetsJSON string
	var executed int

	err := s.db.QueryRowContext(ctx, `
		SELECT id, timestamp, symbol, action, confidence, agent_results, consensus, risk_check, executed, order_id, outcome, pnl, reasoning, COALESCE(market_condition, ''), COALESCE(entry_price, 0), COALESCE(stop_loss, 0), COALESCE(targets, '[]')
		FROM agent_decisions WHERE id = ?
	`, id).Scan(&d.ID, &d.Timestamp, &d.Symbol, &d.Action, &d.Confidence, &agentResultsJSON, &consensusJSON, &riskCheckJSON, &executed, &d.OrderID, &d.Outcome, &d.PnL, &d.Reasoning, &d.MarketCondition, &d.EntryPrice, &d.StopLoss, &targetsJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get decision: %w", err)
	}

	json.Unmarshal([]byte(agentResultsJSON), &d.AgentResults)
	json.Unmarshal([]byte(consensusJSON), &d.Consensus)
	json.Unmarshal([]byte(riskCheckJSON), &d.RiskCheck)
	json.Unmarshal([]byte(targetsJSON), &d.Targets)
	d.Executed = executed == 1

	return &d, nil
}

// LoadPaperState loads the durable paper trading snapshot.
func (s *SQLiteStore) LoadPaperState(ctx context.Context) (*broker.PaperState, error) {
	var stateJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT state FROM paper_state WHERE id = 'default'
	`).Scan(&stateJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load paper state: %w", err)
	}

	var state broker.PaperState
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return nil, fmt.Errorf("failed to decode paper state: %w", err)
	}
	return &state, nil
}

// SavePaperState saves the durable paper trading snapshot.
func (s *SQLiteStore) SavePaperState(ctx context.Context, state *broker.PaperState) error {
	if state == nil {
		return fmt.Errorf("paper state is required")
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now()
	}

	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to encode paper state: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO paper_state (id, state, updated_at)
		VALUES ('default', ?, ?)
		ON CONFLICT(id) DO UPDATE SET state = excluded.state, updated_at = excluded.updated_at
	`, string(stateJSON), state.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to save paper state: %w", err)
	}
	return nil
}

// AppendPaperLedger appends a paper trading ledger event.
func (s *SQLiteStore) AppendPaperLedger(ctx context.Context, event *broker.PaperLedgerEvent) error {
	if event == nil {
		return fmt.Errorf("paper ledger event is required")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if strings.TrimSpace(event.ID) == "" {
		event.ID = fmt.Sprintf("PAPER_LEDGER_%d", time.Now().UnixNano())
	}

	payloadJSON, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("failed to encode paper ledger payload: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO paper_ledger (id, timestamp, type, ref_id, symbol, payload)
		VALUES (?, ?, ?, ?, ?, ?)
	`, event.ID, event.Timestamp, event.Type, event.RefID, event.Symbol, string(payloadJSON))
	if err != nil {
		return fmt.Errorf("failed to append paper ledger event: %w", err)
	}
	return nil
}

// GetPaperLedger returns recent paper trading ledger events.
func (s *SQLiteStore) GetPaperLedger(ctx context.Context, limit int) ([]broker.PaperLedgerEvent, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, timestamp, type, COALESCE(ref_id, ''), COALESCE(symbol, ''), COALESCE(payload, '{}')
		FROM paper_ledger
		ORDER BY timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query paper ledger: %w", err)
	}
	defer rows.Close()

	events := make([]broker.PaperLedgerEvent, 0)
	for rows.Next() {
		var event broker.PaperLedgerEvent
		var payloadJSON string
		if err := rows.Scan(&event.ID, &event.Timestamp, &event.Type, &event.RefID, &event.Symbol, &payloadJSON); err != nil {
			return nil, fmt.Errorf("failed to scan paper ledger event: %w", err)
		}
		if payloadJSON != "" {
			if err := json.Unmarshal([]byte(payloadJSON), &event.Payload); err != nil {
				return nil, fmt.Errorf("failed to decode paper ledger payload: %w", err)
			}
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// GetExecutionQualityReport summarizes persisted execution quality signals.
func (s *SQLiteStore) GetExecutionQualityReport(ctx context.Context, filter models.ExecutionQualityFilter) (*models.ExecutionQualityReport, error) {
	if filter.SlippageAlertBp <= 0 {
		filter.SlippageAlertBp = 50
	}
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	report := &models.ExecutionQualityReport{
		GeneratedAt: time.Now(),
		StartDate:   filter.StartDate,
		EndDate:     filter.EndDate,
		Symbol:      filter.Symbol,
	}

	overall := newExecutionQualityAccumulator("overall")
	bySymbol := make(map[string]*executionQualityAccumulator)
	byOrderType := make(map[string]*executionQualityAccumulator)
	bySide := make(map[string]*executionQualityAccumulator)

	orders, err := s.loadPaperExecutionOrders(ctx, filter)
	if err != nil {
		return nil, err
	}
	for _, order := range orders {
		overall.addOrder(order)
		executionQualityGroup(bySymbol, order.Symbol).addOrder(order)
		executionQualityGroup(byOrderType, order.OrderType).addOrder(order)
		executionQualityGroup(bySide, order.Side).addOrder(order)
		if order.Filled() && order.SlippageBp >= filter.SlippageAlertBp {
			report.HighSlippageOrders = append(report.HighSlippageOrders, order.Sample())
		}
	}

	issues, err := s.loadExecutionIssues(ctx, filter)
	if err != nil {
		return nil, err
	}
	for _, issue := range issues {
		overall.addIssue(issue)
		executionQualityGroup(bySymbol, issue.Symbol).addIssue(issue)
		executionQualityGroup(bySide, issue.Action).addIssue(issue)
		if len(report.RecentIssues) < filter.Limit {
			report.RecentIssues = append(report.RecentIssues, issue)
		}
	}

	overallStats := overall.stats()
	report.TotalOrders = overallStats.TotalOrders
	report.FilledOrders = overallStats.FilledOrders
	report.OpenOrders = overallStats.OpenOrders
	report.CancelledOrders = overallStats.CancelledOrders
	report.PartialFills = overallStats.PartialFills
	report.RejectedOrders = overallStats.RejectedOrders
	report.BlockedExecutions = overallStats.BlockedExecutions
	report.ProtectiveFailures = overall.protectiveFailures
	report.FillRate = overallStats.FillRate
	report.PartialFillRate = overallStats.PartialFillRate
	report.RejectionRate = overallStats.RejectionRate
	report.AvgFilledQty = overallStats.AvgFilledQty
	report.AvgSlippageBp = overallStats.AvgSlippageBp
	report.MaxAdverseSlippageBp = overallStats.MaxAdverseSlippageBp
	report.TotalCosts = overallStats.TotalCosts
	report.TotalTurnover = overallStats.TotalTurnover
	report.CostBp = overallStats.CostBp
	report.BySymbol = executionQualityStatsSlice(bySymbol)
	report.ByOrderType = executionQualityStatsSlice(byOrderType)
	report.BySide = executionQualityStatsSlice(bySide)
	sort.Slice(report.HighSlippageOrders, func(i, j int) bool {
		return report.HighSlippageOrders[i].SlippageBp > report.HighSlippageOrders[j].SlippageBp
	})
	if len(report.HighSlippageOrders) > filter.Limit {
		report.HighSlippageOrders = report.HighSlippageOrders[:filter.Limit]
	}

	return report, nil
}

type executionQualityOrder struct {
	Timestamp  time.Time
	OrderID    string
	Symbol     string
	Side       string
	OrderType  string
	Status     string
	Quantity   int
	FilledQty  int
	Expected   float64
	Actual     float64
	SlippageBp float64
	Costs      float64
	Turnover   float64
}

func (o executionQualityOrder) Filled() bool {
	return o.FilledQty > 0 || o.Status == "COMPLETE" || o.Status == "PARTIAL"
}

func (o executionQualityOrder) Sample() models.ExecutionQualitySample {
	return models.ExecutionQualitySample{
		Timestamp:  o.Timestamp,
		OrderID:    o.OrderID,
		Symbol:     o.Symbol,
		Side:       o.Side,
		OrderType:  o.OrderType,
		Status:     o.Status,
		Quantity:   o.Quantity,
		FilledQty:  o.FilledQty,
		Expected:   o.Expected,
		Actual:     o.Actual,
		SlippageBp: o.SlippageBp,
		Costs:      o.Costs,
	}
}

func (s *SQLiteStore) loadPaperExecutionOrders(ctx context.Context, filter models.ExecutionQualityFilter) ([]executionQualityOrder, error) {
	query := `
		SELECT timestamp, type, COALESCE(ref_id, ''), COALESCE(symbol, ''), COALESCE(payload, '{}')
		FROM paper_ledger
		WHERE type IN ('ORDER_PLACED', 'ORDER_CANCELLED', 'ORDER_REJECTED')
	`
	args := []interface{}{}
	if filter.Symbol != "" {
		query += " AND symbol = ?"
		args = append(args, filter.Symbol)
	}
	if !filter.StartDate.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, filter.StartDate)
	}
	if !filter.EndDate.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, filter.EndDate)
	}
	query += " ORDER BY timestamp ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query paper execution ledger: %w", err)
	}
	defer rows.Close()

	orders := make(map[string]*executionQualityOrder)
	orderSequence := make([]string, 0)
	for rows.Next() {
		var timestamp time.Time
		var eventType, refID, symbol, payloadJSON string
		if err := rows.Scan(&timestamp, &eventType, &refID, &symbol, &payloadJSON); err != nil {
			return nil, fmt.Errorf("failed to scan paper execution ledger: %w", err)
		}
		payload := map[string]interface{}{}
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			payload = map[string]interface{}{}
		}
		if refID == "" {
			refID = fmt.Sprintf("%s_%d", eventType, timestamp.UnixNano())
		}
		order, ok := orders[refID]
		if !ok {
			order = &executionQualityOrder{OrderID: refID, Timestamp: timestamp, Symbol: symbol}
			orders[refID] = order
			orderSequence = append(orderSequence, refID)
		}
		if timestamp.After(order.Timestamp) {
			order.Timestamp = timestamp
		}
		if order.Symbol == "" {
			order.Symbol = symbol
		}
		switch eventType {
		case "ORDER_PLACED":
			order.Status = strings.ToUpper(stringFromPayload(payload, "status"))
			order.Side = strings.ToUpper(stringFromPayload(payload, "side"))
			order.OrderType = strings.ToUpper(stringFromPayload(payload, "order_type"))
			order.Quantity = intFromPayload(payload, "quantity")
			order.FilledQty = intFromPayload(payload, "filled_qty")
			order.Expected = firstNonZeroFloat(floatFromPayload(payload, "expected_price"), floatFromPayload(payload, "requested_price"))
			order.Actual = firstNonZeroFloat(floatFromPayload(payload, "actual_price"), floatFromPayload(payload, "average_price"))
			order.SlippageBp = firstNonZeroFloat(floatFromPayload(payload, "slippage_bp"), sideAdjustedSlippageBp(order.Side, order.Expected, order.Actual))
			order.Costs = floatFromPayload(payload, "costs")
			order.Turnover = firstNonZeroFloat(floatFromPayload(payload, "order_value"), order.Actual*float64(order.FilledQty))
		case "ORDER_CANCELLED":
			order.Status = "CANCELLED"
		case "ORDER_REJECTED":
			order.Status = "REJECTED"
			order.Side = strings.ToUpper(stringFromPayload(payload, "side"))
			order.OrderType = strings.ToUpper(stringFromPayload(payload, "order_type"))
			order.Quantity = intFromPayload(payload, "quantity")
			order.Expected = firstNonZeroFloat(floatFromPayload(payload, "expected_price"), floatFromPayload(payload, "requested_price"))
			order.Actual = floatFromPayload(payload, "actual_price")
			order.Costs = floatFromPayload(payload, "costs")
			order.Turnover = floatFromPayload(payload, "order_value")
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]executionQualityOrder, 0, len(orders))
	for _, id := range orderSequence {
		order := orders[id]
		if order.Status == "" {
			order.Status = "UNKNOWN"
		}
		if order.Symbol == "" {
			order.Symbol = "UNKNOWN"
		}
		if order.Side == "" {
			order.Side = "UNKNOWN"
		}
		if order.OrderType == "" {
			order.OrderType = "UNKNOWN"
		}
		result = append(result, *order)
	}
	return result, nil
}

func (s *SQLiteStore) loadExecutionIssues(ctx context.Context, filter models.ExecutionQualityFilter) ([]models.ExecutionQualityIssue, error) {
	query := `
		SELECT timestamp, decision_id, symbol, action, stage, status, COALESCE(message, ''), COALESCE(payload, '{}')
		FROM decision_logs
		WHERE (
			stage = 'EXECUTION_BLOCKED'
			OR stage = 'ORDER_REJECTED'
			OR (stage = 'PROTECTIVE_ORDER' AND status IN ('BLOCKED', 'FAILED'))
		)
	`
	args := []interface{}{}
	if filter.Symbol != "" {
		query += " AND symbol = ?"
		args = append(args, filter.Symbol)
	}
	if !filter.StartDate.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, filter.StartDate)
	}
	if !filter.EndDate.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, filter.EndDate)
	}
	query += " ORDER BY timestamp DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query execution issues: %w", err)
	}
	defer rows.Close()

	issues := make([]models.ExecutionQualityIssue, 0)
	for rows.Next() {
		var issue models.ExecutionQualityIssue
		var payloadJSON string
		if err := rows.Scan(&issue.Timestamp, &issue.DecisionID, &issue.Symbol, &issue.Action, &issue.Stage, &issue.Status, &issue.Message, &payloadJSON); err != nil {
			return nil, fmt.Errorf("failed to scan execution issue: %w", err)
		}
		payload := map[string]interface{}{}
		_ = json.Unmarshal([]byte(payloadJSON), &payload)
		issue.Source = "decision_log"
		issue.OrderID = stringFromPayload(payload, "order_id")
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

type executionQualityAccumulator struct {
	key                string
	totalOrders        int
	filledOrders       int
	openOrders         int
	cancelledOrders    int
	partialFills       int
	rejectedOrders     int
	blockedExecutions  int
	protectiveFailures int
	filledQtySum       int
	slippageSum        float64
	maxAdverseSlip     float64
	costs              float64
	turnover           float64
}

func newExecutionQualityAccumulator(key string) *executionQualityAccumulator {
	return &executionQualityAccumulator{key: key}
}

func executionQualityGroup(groups map[string]*executionQualityAccumulator, key string) *executionQualityAccumulator {
	if key == "" {
		key = "UNKNOWN"
	}
	group, ok := groups[key]
	if !ok {
		group = newExecutionQualityAccumulator(key)
		groups[key] = group
	}
	return group
}

func (a *executionQualityAccumulator) addOrder(order executionQualityOrder) {
	a.totalOrders++
	switch order.Status {
	case "COMPLETE":
		a.filledOrders++
	case "PARTIAL":
		a.filledOrders++
		a.partialFills++
	case "OPEN":
		a.openOrders++
	case "CANCELLED":
		a.cancelledOrders++
	case "REJECTED":
		a.rejectedOrders++
	default:
		if order.Filled() {
			a.filledOrders++
		}
	}
	if order.FilledQty > 0 {
		a.filledQtySum += order.FilledQty
	}
	if order.Filled() {
		a.slippageSum += order.SlippageBp
		if order.SlippageBp > a.maxAdverseSlip {
			a.maxAdverseSlip = order.SlippageBp
		}
	}
	a.costs += order.Costs
	a.turnover += order.Turnover
}

func (a *executionQualityAccumulator) addIssue(issue models.ExecutionQualityIssue) {
	switch issue.Stage {
	case string(models.DecisionStageExecutionBlocked):
		a.blockedExecutions++
	case string(models.DecisionStageOrderRejected):
		a.rejectedOrders++
	case string(models.DecisionStageProtectiveOrder):
		a.protectiveFailures++
	}
}

func (a *executionQualityAccumulator) stats() models.ExecutionQualityGroup {
	stats := models.ExecutionQualityGroup{
		Key:                  a.key,
		TotalOrders:          a.totalOrders,
		FilledOrders:         a.filledOrders,
		OpenOrders:           a.openOrders,
		CancelledOrders:      a.cancelledOrders,
		PartialFills:         a.partialFills,
		RejectedOrders:       a.rejectedOrders,
		BlockedExecutions:    a.blockedExecutions,
		ProtectiveFailures:   a.protectiveFailures,
		TotalCosts:           a.costs,
		TotalTurnover:        a.turnover,
		MaxAdverseSlippageBp: a.maxAdverseSlip,
	}
	if a.totalOrders > 0 {
		stats.FillRate = float64(a.filledOrders) / float64(a.totalOrders) * 100
		stats.RejectionRate = float64(a.rejectedOrders) / float64(a.totalOrders) * 100
	}
	if a.filledOrders > 0 {
		stats.PartialFillRate = float64(a.partialFills) / float64(a.filledOrders) * 100
		stats.AvgFilledQty = float64(a.filledQtySum) / float64(a.filledOrders)
		stats.AvgSlippageBp = a.slippageSum / float64(a.filledOrders)
	}
	if a.turnover > 0 {
		stats.CostBp = a.costs / a.turnover * 10000
	}
	return stats
}

func executionQualityStatsSlice(groups map[string]*executionQualityAccumulator) []models.ExecutionQualityGroup {
	result := make([]models.ExecutionQualityGroup, 0, len(groups))
	for _, group := range groups {
		result = append(result, group.stats())
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Key < result[j].Key
	})
	return result
}

func stringFromPayload(payload map[string]interface{}, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

func intFromPayload(payload map[string]interface{}, key string) int {
	value, ok := payload[key]
	if !ok || value == nil {
		return 0
	}
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	default:
		return 0
	}
}

func floatFromPayload(payload map[string]interface{}, key string) float64 {
	value, ok := payload[key]
	if !ok || value == nil {
		return 0
	}
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	default:
		return 0
	}
}

func firstNonZeroFloat(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func sideAdjustedSlippageBp(side string, expected, actual float64) float64 {
	if expected <= 0 || actual <= 0 {
		return 0
	}
	if side == "SELL" {
		return (expected - actual) / expected * 10000
	}
	return (actual - expected) / expected * 10000
}

// GetPostTradeReviewReport links completed trades with prediction and execution context.
func (s *SQLiteStore) GetPostTradeReviewReport(ctx context.Context, filter models.PostTradeReviewFilter) (*models.PostTradeReviewReport, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	report := &models.PostTradeReviewReport{
		GeneratedAt: time.Now(),
		StartDate:   filter.StartDate,
		EndDate:     filter.EndDate,
		Symbol:      filter.Symbol,
		Trades:      []models.PostTradeReviewTrade{},
	}

	trades, err := s.GetTrades(ctx, TradeFilter{
		Symbol:    filter.Symbol,
		StartDate: filter.StartDate,
		EndDate:   filter.EndDate,
		IsPaper:   filter.IsPaper,
		Limit:     filter.Limit,
	})
	if err != nil {
		return nil, err
	}
	predictions, err := s.GetPaperPredictions(ctx, PaperPredictionFilter{
		Symbol:    filter.Symbol,
		StartDate: predictionReviewStart(filter.StartDate),
		EndDate:   predictionReviewEnd(filter.EndDate),
		Limit:     filter.Limit * 5,
	})
	if err != nil {
		return nil, err
	}
	orders, err := s.loadPaperExecutionOrders(ctx, models.ExecutionQualityFilter{
		Symbol:    filter.Symbol,
		StartDate: filter.StartDate,
		EndDate:   filter.EndDate,
	})
	if err != nil {
		return nil, err
	}
	ordersByID := make(map[string]executionQualityOrder, len(orders))
	for _, order := range orders {
		ordersByID[order.OrderID] = order
	}

	overall := newPostTradeReviewAccumulator("overall")
	bySetup := make(map[string]*postTradeReviewAccumulator)
	bySymbol := make(map[string]*postTradeReviewAccumulator)
	byStrategy := make(map[string]*postTradeReviewAccumulator)

	for _, trade := range trades {
		review := buildPostTradeReviewTrade(trade, matchTradePrediction(trade, predictions), ordersByID)
		report.Trades = append(report.Trades, review)
		overall.add(review)
		postTradeReviewGroup(bySetup, reviewSetupKey(review)).add(review)
		postTradeReviewGroup(bySymbol, review.Symbol).add(review)
		postTradeReviewGroup(byStrategy, reviewStrategyKey(review)).add(review)
	}

	stats := overall.stats()
	report.TotalTrades = stats.Trades
	report.ReviewedTrades = len(report.Trades)
	report.Winners = stats.Winners
	report.Losers = stats.Losers
	report.NetPnL = stats.NetPnL
	report.AvgPnLPercent = stats.AvgPnLPercent
	report.WithPrediction = stats.WithPrediction
	report.WithExecution = stats.WithExecution
	report.MissingPrediction = stats.MissingPrediction
	report.MissingExecution = stats.MissingExecution
	report.AvgSlippageBp = stats.AvgSlippageBp
	report.TotalCosts = stats.TotalCosts
	report.BySetup = postTradeReviewStatsSlice(bySetup)
	report.BySymbol = postTradeReviewStatsSlice(bySymbol)
	report.ByStrategy = postTradeReviewStatsSlice(byStrategy)

	return report, nil
}

func predictionReviewStart(start time.Time) time.Time {
	if start.IsZero() {
		return start
	}
	return start.Add(-24 * time.Hour)
}

func predictionReviewEnd(end time.Time) time.Time {
	if end.IsZero() {
		return end
	}
	return end.Add(24 * time.Hour)
}

func matchTradePrediction(trade models.Trade, predictions []models.PaperPrediction) *models.PaperPrediction {
	var best *models.PaperPrediction
	for i := range predictions {
		prediction := &predictions[i]
		if !strings.EqualFold(prediction.Symbol, trade.Symbol) {
			continue
		}
		if !strings.EqualFold(prediction.Action, string(trade.Side)) {
			continue
		}
		if prediction.CreatedAt.After(trade.Timestamp) {
			continue
		}
		if !prediction.ExpiresAt.IsZero() && trade.Timestamp.After(prediction.ExpiresAt.Add(5*time.Minute)) {
			continue
		}
		if best == nil || prediction.CreatedAt.After(best.CreatedAt) {
			best = prediction
		}
	}
	return best
}

func buildPostTradeReviewTrade(trade models.Trade, prediction *models.PaperPrediction, ordersByID map[string]executionQualityOrder) models.PostTradeReviewTrade {
	review := models.PostTradeReviewTrade{
		TradeID:     trade.ID,
		Timestamp:   trade.Timestamp,
		Symbol:      trade.Symbol,
		Side:        string(trade.Side),
		Strategy:    trade.Strategy,
		Quantity:    trade.Quantity,
		EntryPrice:  trade.EntryPrice,
		ExitPrice:   trade.ExitPrice,
		PnL:         trade.PnL,
		PnLPercent:  trade.PnLPercent,
		IsPaper:     trade.IsPaper,
		DecisionID:  trade.DecisionID,
		OrderIDs:    append([]string(nil), trade.OrderIDs...),
		ReviewFlags: make([]string, 0),
	}
	if prediction != nil {
		review.PredictionID = prediction.ID
		review.SetupName = prediction.SetupName
		review.Timeframe = prediction.Timeframe
		review.PredictionConfidence = prediction.Confidence
		review.PredictionOutcome = prediction.Outcome
		review.GatesTotal = len(prediction.Gates)
		for _, gate := range prediction.Gates {
			if gate.Passed {
				review.GatesPassed++
			}
		}
		if prediction.Outcome != "" && trade.PnLPercent != 0 {
			if prediction.Outcome == "RIGHT" && trade.PnLPercent < 0 {
				review.ReviewFlags = append(review.ReviewFlags, "prediction_right_trade_lost")
			}
			if prediction.Outcome == "WRONG" && trade.PnLPercent > 0 {
				review.ReviewFlags = append(review.ReviewFlags, "prediction_wrong_trade_won")
			}
		}
	} else {
		review.ReviewFlags = append(review.ReviewFlags, "missing_prediction")
	}

	var slippageSum float64
	for _, orderID := range trade.OrderIDs {
		order, ok := ordersByID[orderID]
		if !ok {
			continue
		}
		review.ExecutionOrders++
		review.ExecutionCosts += order.Costs
		if order.Filled() {
			review.FilledOrders++
			slippageSum += order.SlippageBp
		}
		if order.Status == "PARTIAL" {
			review.PartialFills++
		}
	}
	if review.FilledOrders > 0 {
		review.AvgSlippageBp = slippageSum / float64(review.FilledOrders)
	}
	if len(trade.OrderIDs) > 0 && review.ExecutionOrders == 0 {
		review.ReviewFlags = append(review.ReviewFlags, "missing_execution")
	}
	if review.PartialFills > 0 {
		review.ReviewFlags = append(review.ReviewFlags, "partial_fill")
	}
	if review.AvgSlippageBp >= 50 {
		review.ReviewFlags = append(review.ReviewFlags, "high_slippage")
	}
	return review
}

func reviewSetupKey(review models.PostTradeReviewTrade) string {
	if review.SetupName == "" {
		return "NO_PREDICTION"
	}
	return review.SetupName
}

func reviewStrategyKey(review models.PostTradeReviewTrade) string {
	if review.Strategy == "" {
		return "UNKNOWN"
	}
	return review.Strategy
}

type postTradeReviewAccumulator struct {
	key           string
	trades        int
	winners       int
	losers        int
	pnl           float64
	pnlPercent    float64
	predictions   int
	confidenceSum float64
	executions    int
	missingPred   int
	missingExec   int
	slippageSum   float64
	costs         float64
}

func newPostTradeReviewAccumulator(key string) *postTradeReviewAccumulator {
	return &postTradeReviewAccumulator{key: key}
}

func postTradeReviewGroup(groups map[string]*postTradeReviewAccumulator, key string) *postTradeReviewAccumulator {
	if key == "" {
		key = "UNKNOWN"
	}
	group, ok := groups[key]
	if !ok {
		group = newPostTradeReviewAccumulator(key)
		groups[key] = group
	}
	return group
}

func (a *postTradeReviewAccumulator) add(review models.PostTradeReviewTrade) {
	a.trades++
	a.pnl += review.PnL
	a.pnlPercent += review.PnLPercent
	if review.PnL > 0 || review.PnLPercent > 0 {
		a.winners++
	} else if review.PnL < 0 || review.PnLPercent < 0 {
		a.losers++
	}
	if review.PredictionID != "" {
		a.predictions++
		a.confidenceSum += review.PredictionConfidence
	} else {
		a.missingPred++
	}
	if review.ExecutionOrders > 0 {
		a.executions++
		a.slippageSum += review.AvgSlippageBp
	} else {
		a.missingExec++
	}
	a.costs += review.ExecutionCosts
}

func (a *postTradeReviewAccumulator) stats() models.PostTradeReviewGroup {
	stats := models.PostTradeReviewGroup{
		Key:               a.key,
		Trades:            a.trades,
		Winners:           a.winners,
		Losers:            a.losers,
		NetPnL:            a.pnl,
		WithPrediction:    a.predictions,
		WithExecution:     a.executions,
		TotalCosts:        a.costs,
		MissingPrediction: a.missingPred,
		MissingExecution:  a.missingExec,
	}
	if a.trades > 0 {
		stats.WinRate = float64(a.winners) / float64(a.trades) * 100
		stats.AvgPnLPercent = a.pnlPercent / float64(a.trades)
	}
	if a.predictions > 0 {
		stats.AvgConfidence = a.confidenceSum / float64(a.predictions)
	}
	if a.executions > 0 {
		stats.AvgSlippageBp = a.slippageSum / float64(a.executions)
	}
	return stats
}

func postTradeReviewStatsSlice(groups map[string]*postTradeReviewAccumulator) []models.PostTradeReviewGroup {
	result := make([]models.PostTradeReviewGroup, 0, len(groups))
	for _, group := range groups {
		result = append(result, group.stats())
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Trades == result[j].Trades {
			return result[i].Key < result[j].Key
		}
		return result[i].Trades > result[j].Trades
	})
	return result
}

// SavePaperPrediction inserts or updates a paper prediction.
func (s *SQLiteStore) SavePaperPrediction(ctx context.Context, prediction *models.PaperPrediction) error {
	if prediction == nil {
		return fmt.Errorf("paper prediction is required")
	}
	if strings.TrimSpace(prediction.ID) == "" {
		return fmt.Errorf("paper prediction ID is required")
	}
	if strings.TrimSpace(prediction.SetupName) == "" {
		return fmt.Errorf("paper prediction setup name is required")
	}
	if strings.TrimSpace(prediction.Timeframe) == "" {
		return fmt.Errorf("paper prediction timeframe is required")
	}

	evaluated := 0
	if prediction.Evaluated {
		evaluated = 1
	}
	gates := prediction.Gates
	if gates == nil {
		gates = []models.PaperPredictionGate{}
	}
	gatesJSON, err := json.Marshal(gates)
	if err != nil {
		return fmt.Errorf("failed to encode paper prediction gates: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO paper_predictions (
			id, symbol, action, confidence, entry_price, target_price, stop_loss,
			time_window_ns, created_at, expires_at, reasoning, setup_name, timeframe, gates, evaluated,
			exit_price, outcome, pnl_percent, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			symbol = excluded.symbol,
			action = excluded.action,
			confidence = excluded.confidence,
			entry_price = excluded.entry_price,
			target_price = excluded.target_price,
			stop_loss = excluded.stop_loss,
			time_window_ns = excluded.time_window_ns,
			created_at = excluded.created_at,
			expires_at = excluded.expires_at,
			reasoning = excluded.reasoning,
			setup_name = excluded.setup_name,
			timeframe = excluded.timeframe,
			gates = excluded.gates,
			evaluated = excluded.evaluated,
			exit_price = excluded.exit_price,
			outcome = excluded.outcome,
			pnl_percent = excluded.pnl_percent,
			updated_at = excluded.updated_at
	`, prediction.ID, prediction.Symbol, prediction.Action, prediction.Confidence, prediction.EntryPrice,
		prediction.TargetPrice, prediction.StopLoss, prediction.TimeWindow.Nanoseconds(),
		prediction.CreatedAt, prediction.ExpiresAt, prediction.Reasoning, prediction.SetupName,
		prediction.Timeframe, string(gatesJSON), evaluated,
		prediction.ExitPrice, prediction.Outcome, prediction.PnLPercent, time.Now())
	if err != nil {
		return fmt.Errorf("failed to save paper prediction: %w", err)
	}
	return nil
}

// GetPaperPredictions returns stored paper predictions.
func (s *SQLiteStore) GetPaperPredictions(ctx context.Context, filter PaperPredictionFilter) ([]models.PaperPrediction, error) {
	query := `
		SELECT id, symbol, action, confidence, entry_price, target_price, stop_loss,
			time_window_ns, created_at, expires_at, COALESCE(reasoning, ''),
			setup_name, timeframe, gates, evaluated,
			COALESCE(exit_price, 0), COALESCE(outcome, ''), COALESCE(pnl_percent, 0)
		FROM paper_predictions WHERE 1=1
	`
	args := []interface{}{}
	if filter.Symbol != "" {
		query += " AND symbol = ?"
		args = append(args, filter.Symbol)
	}
	if filter.SetupName != "" {
		query += " AND setup_name = ?"
		args = append(args, filter.SetupName)
	}
	if filter.Timeframe != "" {
		query += " AND timeframe = ?"
		args = append(args, filter.Timeframe)
	}
	if !filter.StartDate.IsZero() {
		query += " AND created_at >= ?"
		args = append(args, filter.StartDate)
	}
	if !filter.EndDate.IsZero() {
		query += " AND created_at <= ?"
		args = append(args, filter.EndDate)
	}
	if filter.Evaluated != nil {
		evaluated := 0
		if *filter.Evaluated {
			evaluated = 1
		}
		query += " AND evaluated = ?"
		args = append(args, evaluated)
	}
	query += " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query paper predictions: %w", err)
	}
	defer rows.Close()

	predictions := make([]models.PaperPrediction, 0)
	for rows.Next() {
		var prediction models.PaperPrediction
		var timeWindowNS int64
		var evaluated int
		var gatesJSON string
		if err := rows.Scan(
			&prediction.ID,
			&prediction.Symbol,
			&prediction.Action,
			&prediction.Confidence,
			&prediction.EntryPrice,
			&prediction.TargetPrice,
			&prediction.StopLoss,
			&timeWindowNS,
			&prediction.CreatedAt,
			&prediction.ExpiresAt,
			&prediction.Reasoning,
			&prediction.SetupName,
			&prediction.Timeframe,
			&gatesJSON,
			&evaluated,
			&prediction.ExitPrice,
			&prediction.Outcome,
			&prediction.PnLPercent,
		); err != nil {
			return nil, fmt.Errorf("failed to scan paper prediction: %w", err)
		}
		prediction.TimeWindow = time.Duration(timeWindowNS)
		prediction.Evaluated = evaluated == 1
		if err := json.Unmarshal([]byte(gatesJSON), &prediction.Gates); err != nil {
			return nil, fmt.Errorf("failed to decode paper prediction gates for %s: %w", prediction.ID, err)
		}
		predictions = append(predictions, prediction)
	}
	return predictions, rows.Err()
}

// GetPaperPredictionReport returns calibration and expectancy stats for paper predictions.
func (s *SQLiteStore) GetPaperPredictionReport(ctx context.Context, filter PaperPredictionFilter) (*models.PaperPredictionReport, error) {
	predictions, err := s.GetPaperPredictions(ctx, filter)
	if err != nil {
		return nil, err
	}

	report := &models.PaperPredictionReport{
		GeneratedAt: time.Now(),
		StartDate:   filter.StartDate,
		EndDate:     filter.EndDate,
		Symbol:      filter.Symbol,
	}

	overall := newPaperPredictionAccumulator("overall")
	bySymbol := make(map[string]*paperPredictionAccumulator)
	byAction := make(map[string]*paperPredictionAccumulator)
	byConfidence := make(map[string]*paperPredictionAccumulator)

	for _, prediction := range predictions {
		overall.add(prediction)
		paperPredictionGroup(bySymbol, prediction.Symbol).add(prediction)
		paperPredictionGroup(byAction, prediction.Action).add(prediction)
		paperPredictionGroup(byConfidence, confidenceBucket(prediction.Confidence)).add(prediction)
	}

	overallStats := overall.stats()
	report.TotalPredictions = overallStats.TotalPredictions
	report.ActivePredictions = overallStats.ActivePredictions
	report.Evaluated = overallStats.Evaluated
	report.Decisive = overallStats.Decisive
	report.RightPredictions = overallStats.RightPredictions
	report.WrongPredictions = overallStats.WrongPredictions
	report.ExpiredPredictions = overallStats.ExpiredPredictions
	report.WinRate = overallStats.WinRate
	report.AvgConfidence = overallStats.AvgConfidence
	report.AvgPnLPercent = overallStats.AvgPnLPercent
	report.Expectancy = overallStats.Expectancy
	report.BestPrediction = overallStats.BestPrediction
	report.WorstPrediction = overallStats.WorstPrediction
	report.ExpiredRate = overallStats.ExpiredRate

	report.BySymbol = paperPredictionStatsSlice(bySymbol)
	report.ByAction = paperPredictionStatsSlice(byAction)
	report.ByConfidence = paperPredictionConfidenceStatsSlice(byConfidence)
	for _, bucket := range report.ByConfidence {
		if bucket.Decisive >= 5 && bucket.AvgConfidence-bucket.WinRate >= 15 {
			report.Overconfidence = append(report.Overconfidence, models.CalibrationWarning{
				Bucket:        bucket.Key,
				AvgConfidence: bucket.AvgConfidence,
				WinRate:       bucket.WinRate,
				Gap:           bucket.AvgConfidence - bucket.WinRate,
				SampleSize:    bucket.Decisive,
			})
		}
	}

	return report, nil
}

// GetHistoricalCalibrationReport returns setup and gate expectancy from paper prediction outcomes.
func (s *SQLiteStore) GetHistoricalCalibrationReport(ctx context.Context, filter PaperPredictionFilter) (*models.HistoricalCalibrationReport, error) {
	predictions, err := s.GetPaperPredictions(ctx, filter)
	if err != nil {
		return nil, err
	}
	report := &models.HistoricalCalibrationReport{
		GeneratedAt: time.Now(),
		StartDate:   filter.StartDate,
		EndDate:     filter.EndDate,
		Symbol:      filter.Symbol,
		SetupName:   filter.SetupName,
		Timeframe:   filter.Timeframe,
	}

	overall := newCalibrationAccumulator("overall")
	bySetup := make(map[string]*calibrationAccumulator)
	byGate := make(map[string]*calibrationAccumulator)
	bySymbol := make(map[string]*calibrationAccumulator)
	byTimeframe := make(map[string]*calibrationAccumulator)
	byAction := make(map[string]*calibrationAccumulator)

	for _, prediction := range predictions {
		overall.add(prediction)
		calibrationGroup(bySetup, prediction.SetupName).add(prediction)
		calibrationGroup(bySymbol, prediction.Symbol).add(prediction)
		calibrationGroup(byTimeframe, prediction.Timeframe).add(prediction)
		calibrationGroup(byAction, prediction.Action).add(prediction)
		for _, gate := range prediction.Gates {
			status := "FAIL"
			if gate.Passed {
				status = "PASS"
			}
			calibrationGroup(byGate, gate.Name+"="+status).add(prediction)
		}
	}

	overallStats := overall.stats()
	report.TotalPredictions = overallStats.TotalPredictions
	report.Evaluated = overallStats.Evaluated
	report.Decisive = overallStats.Decisive
	report.WinRate = overallStats.WinRate
	report.AvgConfidence = overallStats.AvgConfidence
	report.AvgPnLPercent = overallStats.AvgPnLPercent
	report.Expectancy = overallStats.Expectancy
	report.BySetup = calibrationStatsSlice(bySetup)
	report.ByGate = calibrationStatsSlice(byGate)
	report.BySymbol = calibrationStatsSlice(bySymbol)
	report.ByTimeframe = calibrationStatsSlice(byTimeframe)
	report.ByAction = calibrationStatsSlice(byAction)

	return report, nil
}

// SavePaperCandidate inserts or updates a promoted paper-soak candidate.
func (s *SQLiteStore) SavePaperCandidate(ctx context.Context, candidate *models.PaperCandidate) error {
	if candidate == nil {
		return fmt.Errorf("paper candidate is required")
	}
	if strings.TrimSpace(candidate.ID) == "" {
		return fmt.Errorf("paper candidate ID is required")
	}
	if strings.TrimSpace(candidate.Symbol) == "" {
		return fmt.Errorf("paper candidate symbol is required")
	}
	if strings.TrimSpace(candidate.Strategy) == "" {
		return fmt.Errorf("paper candidate strategy is required")
	}
	if strings.TrimSpace(candidate.Timeframe) == "" {
		return fmt.Errorf("paper candidate timeframe is required")
	}
	if strings.TrimSpace(candidate.Setup) == "" {
		return fmt.Errorf("paper candidate setup is required")
	}
	if candidate.Status == "" {
		candidate.Status = models.PaperCandidateStatusActive
	}
	if candidate.PromotedAt.IsZero() {
		candidate.PromotedAt = time.Now()
	}
	candidate.UpdatedAt = time.Now()

	allowedJSON, err := json.Marshal(candidate.AllowedRegimes)
	if err != nil {
		return fmt.Errorf("failed to encode allowed regimes: %w", err)
	}
	blockedJSON, err := json.Marshal(candidate.BlockedRegimes)
	if err != nil {
		return fmt.Errorf("failed to encode blocked regimes: %w", err)
	}
	regimeStatsJSON, err := json.Marshal(candidate.RegimeStats)
	if err != nil {
		return fmt.Errorf("failed to encode regime stats: %w", err)
	}
	allowShort := 0
	if candidate.AllowShort {
		allowShort = 1
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO paper_candidates (
			id, status, symbol, exchange, strategy, param_variant, parameters, timeframe, setup,
			source, verdict, reason, days, candles, trades, validation_trades, return_pct,
			train_return_pct, validation_return_pct, win_rate, profit_factor, expectancy,
			max_drawdown_pct, sharpe_ratio, stop_loss_percent, take_profit_percent,
			trailing_stop_percent, allow_short, allowed_regimes, blocked_regimes, regime_stats,
			promoted_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			symbol = excluded.symbol,
			exchange = excluded.exchange,
			strategy = excluded.strategy,
			param_variant = excluded.param_variant,
			parameters = excluded.parameters,
			timeframe = excluded.timeframe,
			setup = excluded.setup,
			source = excluded.source,
			verdict = excluded.verdict,
			reason = excluded.reason,
			days = excluded.days,
			candles = excluded.candles,
			trades = excluded.trades,
			validation_trades = excluded.validation_trades,
			return_pct = excluded.return_pct,
			train_return_pct = excluded.train_return_pct,
			validation_return_pct = excluded.validation_return_pct,
			win_rate = excluded.win_rate,
			profit_factor = excluded.profit_factor,
			expectancy = excluded.expectancy,
			max_drawdown_pct = excluded.max_drawdown_pct,
			sharpe_ratio = excluded.sharpe_ratio,
			stop_loss_percent = excluded.stop_loss_percent,
			take_profit_percent = excluded.take_profit_percent,
			trailing_stop_percent = excluded.trailing_stop_percent,
			allow_short = excluded.allow_short,
			allowed_regimes = excluded.allowed_regimes,
			blocked_regimes = excluded.blocked_regimes,
			regime_stats = excluded.regime_stats,
			updated_at = excluded.updated_at
	`, candidate.ID, candidate.Status, candidate.Symbol, candidate.Exchange, candidate.Strategy,
		candidate.ParamVariant, candidate.Parameters, candidate.Timeframe, candidate.Setup,
		candidate.Source, candidate.Verdict, candidate.Reason, candidate.Days, candidate.Candles,
		candidate.Trades, candidate.ValidationTrades, candidate.ReturnPct, candidate.TrainReturnPct,
		candidate.ValidationReturnPct, candidate.WinRate, candidate.ProfitFactor, candidate.Expectancy,
		candidate.MaxDrawdownPct, candidate.SharpeRatio, candidate.StopLossPercent,
		candidate.TakeProfitPercent, candidate.TrailingStopPercent, allowShort,
		string(allowedJSON), string(blockedJSON), string(regimeStatsJSON), candidate.PromotedAt, candidate.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to save paper candidate: %w", err)
	}
	return nil
}

// GetPaperCandidates returns promoted paper-soak candidates.
func (s *SQLiteStore) GetPaperCandidates(ctx context.Context, filter models.PaperCandidateFilter) ([]models.PaperCandidate, error) {
	query := `
		SELECT id, status, symbol, exchange, strategy, param_variant, COALESCE(parameters, ''),
			timeframe, setup, source, verdict, COALESCE(reason, ''), days, candles, trades,
			validation_trades, return_pct, train_return_pct, validation_return_pct, win_rate,
			profit_factor, expectancy, max_drawdown_pct, sharpe_ratio, COALESCE(stop_loss_percent, 0),
			COALESCE(take_profit_percent, 0), COALESCE(trailing_stop_percent, 0), allow_short,
			allowed_regimes, blocked_regimes, regime_stats, promoted_at, updated_at
		FROM paper_candidates WHERE 1=1
	`
	args := []interface{}{}
	if filter.Symbol != "" {
		query += " AND symbol = ?"
		args = append(args, filter.Symbol)
	}
	if filter.Strategy != "" {
		query += " AND strategy = ?"
		args = append(args, filter.Strategy)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	query += " ORDER BY status ASC, validation_return_pct DESC, return_pct DESC, updated_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query paper candidates: %w", err)
	}
	defer rows.Close()

	candidates := make([]models.PaperCandidate, 0)
	for rows.Next() {
		var candidate models.PaperCandidate
		var allowShort int
		var allowedJSON, blockedJSON, regimeStatsJSON string
		if err := rows.Scan(
			&candidate.ID,
			&candidate.Status,
			&candidate.Symbol,
			&candidate.Exchange,
			&candidate.Strategy,
			&candidate.ParamVariant,
			&candidate.Parameters,
			&candidate.Timeframe,
			&candidate.Setup,
			&candidate.Source,
			&candidate.Verdict,
			&candidate.Reason,
			&candidate.Days,
			&candidate.Candles,
			&candidate.Trades,
			&candidate.ValidationTrades,
			&candidate.ReturnPct,
			&candidate.TrainReturnPct,
			&candidate.ValidationReturnPct,
			&candidate.WinRate,
			&candidate.ProfitFactor,
			&candidate.Expectancy,
			&candidate.MaxDrawdownPct,
			&candidate.SharpeRatio,
			&candidate.StopLossPercent,
			&candidate.TakeProfitPercent,
			&candidate.TrailingStopPercent,
			&allowShort,
			&allowedJSON,
			&blockedJSON,
			&regimeStatsJSON,
			&candidate.PromotedAt,
			&candidate.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan paper candidate: %w", err)
		}
		candidate.AllowShort = allowShort == 1
		if err := json.Unmarshal([]byte(allowedJSON), &candidate.AllowedRegimes); err != nil {
			return nil, fmt.Errorf("failed to decode allowed regimes for %s: %w", candidate.ID, err)
		}
		if err := json.Unmarshal([]byte(blockedJSON), &candidate.BlockedRegimes); err != nil {
			return nil, fmt.Errorf("failed to decode blocked regimes for %s: %w", candidate.ID, err)
		}
		if err := json.Unmarshal([]byte(regimeStatsJSON), &candidate.RegimeStats); err != nil {
			return nil, fmt.Errorf("failed to decode regime stats for %s: %w", candidate.ID, err)
		}
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

type calibrationAccumulator struct {
	key           string
	total         int
	evaluated     int
	right         int
	wrong         int
	expired       int
	confidenceSum float64
	pnlSum        float64
	decisivePnL   float64
}

func newCalibrationAccumulator(key string) *calibrationAccumulator {
	return &calibrationAccumulator{key: key}
}

func calibrationGroup(groups map[string]*calibrationAccumulator, key string) *calibrationAccumulator {
	if key == "" {
		key = "UNKNOWN"
	}
	group, ok := groups[key]
	if !ok {
		group = newCalibrationAccumulator(key)
		groups[key] = group
	}
	return group
}

func (a *calibrationAccumulator) add(prediction models.PaperPrediction) {
	a.total++
	a.confidenceSum += prediction.Confidence
	if !prediction.Evaluated {
		return
	}
	a.evaluated++
	a.pnlSum += prediction.PnLPercent
	switch prediction.Outcome {
	case "RIGHT":
		a.right++
		a.decisivePnL += prediction.PnLPercent
	case "WRONG":
		a.wrong++
		a.decisivePnL += prediction.PnLPercent
	case "EXPIRED":
		a.expired++
	}
}

func (a *calibrationAccumulator) stats() models.CalibrationGroupStats {
	stats := models.CalibrationGroupStats{
		Key:                a.key,
		TotalPredictions:   a.total,
		Evaluated:          a.evaluated,
		RightPredictions:   a.right,
		WrongPredictions:   a.wrong,
		ExpiredPredictions: a.expired,
	}
	stats.Decisive = stats.RightPredictions + stats.WrongPredictions
	if a.total > 0 {
		stats.AvgConfidence = a.confidenceSum / float64(a.total)
	}
	if stats.Evaluated > 0 {
		stats.AvgPnLPercent = a.pnlSum / float64(stats.Evaluated)
	}
	if stats.Decisive > 0 {
		stats.WinRate = float64(stats.RightPredictions) / float64(stats.Decisive) * 100
		stats.Expectancy = a.decisivePnL / float64(stats.Decisive)
	}
	return stats
}

func calibrationStatsSlice(groups map[string]*calibrationAccumulator) []models.CalibrationGroupStats {
	result := make([]models.CalibrationGroupStats, 0, len(groups))
	for _, group := range groups {
		result = append(result, group.stats())
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Decisive == result[j].Decisive {
			return result[i].Key < result[j].Key
		}
		return result[i].Decisive > result[j].Decisive
	})
	return result
}

type paperPredictionAccumulator struct {
	key            string
	total          int
	active         int
	evaluated      int
	right          int
	wrong          int
	expired        int
	confidenceSum  float64
	pnlSum         float64
	decisivePnLSum float64
	best           float64
	worst          float64
	seenPnL        bool
}

func newPaperPredictionAccumulator(key string) *paperPredictionAccumulator {
	return &paperPredictionAccumulator{key: key}
}

func paperPredictionGroup(groups map[string]*paperPredictionAccumulator, key string) *paperPredictionAccumulator {
	if key == "" {
		key = "UNKNOWN"
	}
	group, ok := groups[key]
	if !ok {
		group = newPaperPredictionAccumulator(key)
		groups[key] = group
	}
	return group
}

func (a *paperPredictionAccumulator) add(prediction models.PaperPrediction) {
	a.total++
	a.confidenceSum += prediction.Confidence
	if !prediction.Evaluated {
		a.active++
		return
	}

	a.evaluated++
	a.pnlSum += prediction.PnLPercent
	if !a.seenPnL || prediction.PnLPercent > a.best {
		a.best = prediction.PnLPercent
	}
	if !a.seenPnL || prediction.PnLPercent < a.worst {
		a.worst = prediction.PnLPercent
	}
	a.seenPnL = true

	switch prediction.Outcome {
	case "RIGHT":
		a.right++
		a.decisivePnLSum += prediction.PnLPercent
	case "WRONG":
		a.wrong++
		a.decisivePnLSum += prediction.PnLPercent
	case "EXPIRED":
		a.expired++
	}
}

func (a *paperPredictionAccumulator) stats() models.PaperPredictionGroupStats {
	stats := models.PaperPredictionGroupStats{
		Key:                a.key,
		TotalPredictions:   a.total,
		ActivePredictions:  a.active,
		Evaluated:          a.evaluated,
		RightPredictions:   a.right,
		WrongPredictions:   a.wrong,
		ExpiredPredictions: a.expired,
	}
	stats.Decisive = stats.RightPredictions + stats.WrongPredictions
	if a.total > 0 {
		stats.AvgConfidence = a.confidenceSum / float64(a.total)
	}
	if stats.Evaluated > 0 {
		stats.AvgPnLPercent = a.pnlSum / float64(stats.Evaluated)
		stats.ExpiredRate = float64(stats.ExpiredPredictions) / float64(stats.Evaluated) * 100
		stats.BestPrediction = a.best
		stats.WorstPrediction = a.worst
	}
	if stats.Decisive > 0 {
		stats.WinRate = float64(stats.RightPredictions) / float64(stats.Decisive) * 100
		stats.Expectancy = a.decisivePnLSum / float64(stats.Decisive)
	}
	return stats
}

func paperPredictionStatsSlice(groups map[string]*paperPredictionAccumulator) []models.PaperPredictionGroupStats {
	result := make([]models.PaperPredictionGroupStats, 0, len(groups))
	for _, group := range groups {
		result = append(result, group.stats())
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Key < result[j].Key
	})
	return result
}

func paperPredictionConfidenceStatsSlice(groups map[string]*paperPredictionAccumulator) []models.PaperPredictionGroupStats {
	buckets := []string{"<50", "50-59", "60-69", "70-79", "80-89", "90-100"}
	result := make([]models.PaperPredictionGroupStats, 0, len(groups))
	for _, bucket := range buckets {
		group, ok := groups[bucket]
		if ok {
			result = append(result, group.stats())
		}
	}
	return result
}

func confidenceBucket(confidence float64) string {
	switch {
	case confidence < 50:
		return "<50"
	case confidence < 60:
		return "50-59"
	case confidence < 70:
		return "60-69"
	case confidence < 80:
		return "70-79"
	case confidence < 90:
		return "80-89"
	default:
		return "90-100"
	}
}

// SaveDaemonState persists the autonomous daemon runtime state.
func (s *SQLiteStore) SaveDaemonState(ctx context.Context, state *models.DaemonState) error {
	if state == nil {
		return fmt.Errorf("daemon state is required")
	}
	if state.ID == "" {
		state.ID = "default"
	}
	now := time.Now()
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = now
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to encode daemon state: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO daemon_state (id, state, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET state = excluded.state, updated_at = excluded.updated_at
	`, state.ID, string(stateJSON), state.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to save daemon state: %w", err)
	}
	return nil
}

// LoadDaemonState loads the current autonomous daemon runtime state.
func (s *SQLiteStore) LoadDaemonState(ctx context.Context) (*models.DaemonState, error) {
	var stateJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT state FROM daemon_state WHERE id = 'default'
	`).Scan(&stateJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load daemon state: %w", err)
	}
	var state models.DaemonState
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return nil, fmt.Errorf("failed to decode daemon state: %w", err)
	}
	return &state, nil
}

// AppendDaemonEvent records a daemon control state change.
func (s *SQLiteStore) AppendDaemonEvent(ctx context.Context, event *models.DaemonEvent) error {
	if event == nil {
		return fmt.Errorf("daemon event is required")
	}
	if event.ID == "" {
		event.ID = fmt.Sprintf("DEVT_%d", time.Now().UnixNano())
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO daemon_events (id, timestamp, type, status, reason, actor, pid, hostname)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, event.ID, event.Timestamp, event.Type, event.Status, event.Reason, event.Actor, event.PID, event.Hostname)
	if err != nil {
		return fmt.Errorf("failed to append daemon event: %w", err)
	}
	return nil
}

// GetDaemonEvents returns recent daemon control events.
func (s *SQLiteStore) GetDaemonEvents(ctx context.Context, limit int) ([]models.DaemonEvent, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, timestamp, type, status, COALESCE(reason, ''), COALESCE(actor, ''), COALESCE(pid, 0), COALESCE(hostname, '')
		FROM daemon_events
		ORDER BY timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query daemon events: %w", err)
	}
	defer rows.Close()

	events := make([]models.DaemonEvent, 0, limit)
	for rows.Next() {
		var event models.DaemonEvent
		if err := rows.Scan(
			&event.ID,
			&event.Timestamp,
			&event.Type,
			&event.Status,
			&event.Reason,
			&event.Actor,
			&event.PID,
			&event.Hostname,
		); err != nil {
			return nil, fmt.Errorf("failed to scan daemon event: %w", err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// UpdateDecisionOutcome updates the outcome and P&L of a decision.
func (s *SQLiteStore) UpdateDecisionOutcome(ctx context.Context, id string, outcome models.DecisionOutcome, pnl float64) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_decisions SET outcome = ?, pnl = ? WHERE id = ?
	`, outcome, pnl, id)
	if err != nil {
		return fmt.Errorf("failed to update decision outcome: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("decision not found: %s", id)
	}

	return nil
}

// ============================================================================
// Watchlist Methods
// ============================================================================

// AddToWatchlist adds a symbol to a watchlist.
func (s *SQLiteStore) AddToWatchlist(ctx context.Context, symbol, listName string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO watchlist (symbol, list_name) VALUES (?, ?)
	`, symbol, listName)
	if err != nil {
		return fmt.Errorf("failed to add to watchlist: %w", err)
	}
	return nil
}

// RemoveFromWatchlist removes a symbol from a watchlist.
func (s *SQLiteStore) RemoveFromWatchlist(ctx context.Context, symbol, listName string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM watchlist WHERE symbol = ? AND list_name = ?
	`, symbol, listName)
	if err != nil {
		return fmt.Errorf("failed to remove from watchlist: %w", err)
	}
	return nil
}

// DeleteWatchlist removes every symbol from a watchlist.
func (s *SQLiteStore) DeleteWatchlist(ctx context.Context, listName string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM watchlist WHERE list_name = ?
	`, listName)
	if err != nil {
		return fmt.Errorf("failed to delete watchlist: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check delete result: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("watchlist not found or already empty: %s", listName)
	}

	return nil
}

// GetWatchlist retrieves symbols in a watchlist.
func (s *SQLiteStore) GetWatchlist(ctx context.Context, listName string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT symbol FROM watchlist WHERE list_name = ? ORDER BY created_at ASC
	`, listName)
	if err != nil {
		return nil, fmt.Errorf("failed to query watchlist: %w", err)
	}
	defer rows.Close()

	var symbols []string
	for rows.Next() {
		var symbol string
		if err := rows.Scan(&symbol); err != nil {
			return nil, fmt.Errorf("failed to scan symbol: %w", err)
		}
		symbols = append(symbols, symbol)
	}

	return symbols, rows.Err()
}

// GetAllWatchlists retrieves all watchlists.
func (s *SQLiteStore) GetAllWatchlists(ctx context.Context) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT list_name, symbol FROM watchlist ORDER BY list_name, created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query watchlists: %w", err)
	}
	defer rows.Close()

	watchlists := make(map[string][]string)
	for rows.Next() {
		var listName, symbol string
		if err := rows.Scan(&listName, &symbol); err != nil {
			return nil, fmt.Errorf("failed to scan watchlist entry: %w", err)
		}
		watchlists[listName] = append(watchlists[listName], symbol)
	}

	return watchlists, rows.Err()
}

// ============================================================================
// Alerts Methods
// ============================================================================

// SaveAlert saves an alert to the database.
func (s *SQLiteStore) SaveAlert(ctx context.Context, alert *models.Alert) error {
	if strings.TrimSpace(alert.ID) == "" {
		alert.ID = fmt.Sprintf("ALERT_%d", time.Now().UnixNano())
	}

	triggered := 0
	if alert.Triggered {
		triggered = 1
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO alerts (id, symbol, condition, price, triggered, created_at, triggered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, alert.ID, alert.Symbol, alert.Condition, alert.Price, triggered, alert.CreatedAt, alert.TriggeredAt)
	if err != nil {
		return fmt.Errorf("failed to save alert: %w", err)
	}
	return nil
}

// GetActiveAlerts retrieves all active (non-triggered) alerts.
func (s *SQLiteStore) GetActiveAlerts(ctx context.Context) ([]models.Alert, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, symbol, condition, price, triggered, created_at, triggered_at
		FROM alerts WHERE triggered = 0 ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query alerts: %w", err)
	}
	defer rows.Close()

	var alerts []models.Alert
	for rows.Next() {
		var a models.Alert
		var triggered int
		if err := rows.Scan(&a.ID, &a.Symbol, &a.Condition, &a.Price, &triggered, &a.CreatedAt, &a.TriggeredAt); err != nil {
			return nil, fmt.Errorf("failed to scan alert: %w", err)
		}
		a.Triggered = triggered == 1
		alerts = append(alerts, a)
	}

	return alerts, rows.Err()
}

// TriggerAlert marks an alert as triggered.
func (s *SQLiteStore) TriggerAlert(ctx context.Context, alertID string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE alerts SET triggered = 1, triggered_at = ? WHERE id = ?
	`, time.Now(), alertID)
	if err != nil {
		return fmt.Errorf("failed to trigger alert: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("alert not found: %s", alertID)
	}

	return nil
}

// DeleteAlert removes an alert from the database.
func (s *SQLiteStore) DeleteAlert(ctx context.Context, alertID string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM alerts WHERE id = ?
	`, alertID)
	if err != nil {
		return fmt.Errorf("failed to delete alert: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check delete result: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("alert not found: %s", alertID)
	}

	return nil
}

// ============================================================================
// Events Methods
// ============================================================================

// SaveEvent saves a corporate event to the database.
func (s *SQLiteStore) SaveEvent(ctx context.Context, event *models.CorporateEvent) error {
	details, _ := json.Marshal(event.Details)

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO events (id, symbol, event_type, date, description, details, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, event.ID, event.Symbol, event.EventType, event.Date, event.Description, string(details), event.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to save event: %w", err)
	}
	return nil
}

// GetUpcomingEvents retrieves upcoming corporate events.
func (s *SQLiteStore) GetUpcomingEvents(ctx context.Context, symbols []string, days int) ([]models.CorporateEvent, error) {
	endDate := time.Now().AddDate(0, 0, days)

	query := `
		SELECT id, symbol, event_type, date, description, details, created_at
		FROM events WHERE date >= ? AND date <= ?
	`
	args := []interface{}{time.Now(), endDate}

	if len(symbols) > 0 {
		placeholders := make([]string, len(symbols))
		for i := range symbols {
			placeholders[i] = "?"
			args = append(args, symbols[i])
		}
		query += " AND symbol IN (" + strings.Join(placeholders, ",") + ")"
	}

	query += " ORDER BY date ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	var events []models.CorporateEvent
	for rows.Next() {
		var e models.CorporateEvent
		var detailsJSON string
		if err := rows.Scan(&e.ID, &e.Symbol, &e.EventType, &e.Date, &e.Description, &detailsJSON, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}
		json.Unmarshal([]byte(detailsJSON), &e.Details)
		events = append(events, e)
	}

	return events, rows.Err()
}

// ============================================================================
// Screener Queries Methods
// ============================================================================

// SaveScreenerQuery saves a screener query.
func (s *SQLiteStore) SaveScreenerQuery(ctx context.Context, name string, query ScreenerQuery) error {
	filters, _ := json.Marshal(query.Filters)

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO screener_queries (name, filters, updated_at)
		VALUES (?, ?, ?)
	`, name, string(filters), time.Now())
	if err != nil {
		return fmt.Errorf("failed to save screener query: %w", err)
	}
	return nil
}

// GetScreenerQuery retrieves a screener query by name.
func (s *SQLiteStore) GetScreenerQuery(ctx context.Context, name string) (*ScreenerQuery, error) {
	var filtersJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT filters FROM screener_queries WHERE name = ?
	`, name).Scan(&filtersJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get screener query: %w", err)
	}

	query := &ScreenerQuery{Name: name}
	json.Unmarshal([]byte(filtersJSON), &query.Filters)
	return query, nil
}

// ListScreenerQueries lists all saved screener query names.
func (s *SQLiteStore) ListScreenerQueries(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name FROM screener_queries ORDER BY name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list screener queries: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan query name: %w", err)
		}
		names = append(names, name)
	}

	return names, rows.Err()
}

// ============================================================================
// Sync Methods
// ============================================================================

// GetLastSync returns the last sync time for a data type.
func (s *SQLiteStore) GetLastSync(dataType string) time.Time {
	s.mu.RLock()
	if t, ok := s.syncTimes[dataType]; ok {
		s.mu.RUnlock()
		return t
	}
	s.mu.RUnlock()

	var lastSync time.Time
	err := s.db.QueryRow(`
		SELECT last_sync FROM sync_status WHERE data_type = ?
	`, dataType).Scan(&lastSync)
	if err != nil {
		return time.Time{}
	}

	s.mu.Lock()
	s.syncTimes[dataType] = lastSync
	s.mu.Unlock()

	return lastSync
}

// SetLastSync sets the last sync time for a data type.
func (s *SQLiteStore) SetLastSync(dataType string, t time.Time) error {
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO sync_status (data_type, last_sync, updated_at)
		VALUES (?, ?, ?)
	`, dataType, t, time.Now())
	if err != nil {
		return fmt.Errorf("failed to set last sync: %w", err)
	}

	s.mu.Lock()
	s.syncTimes[dataType] = t
	s.mu.Unlock()

	return nil
}
