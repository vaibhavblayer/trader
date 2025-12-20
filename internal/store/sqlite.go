// Package store provides data persistence implementations.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
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
	CREATE INDEX IF NOT EXISTS idx_plans_symbol ON trade_plans(symbol);
	CREATE INDEX IF NOT EXISTS idx_plans_status ON trade_plans(status);
	CREATE INDEX IF NOT EXISTS idx_alerts_symbol ON alerts(symbol);
	CREATE INDEX IF NOT EXISTS idx_alerts_triggered ON alerts(triggered);
	CREATE INDEX IF NOT EXISTS idx_events_symbol ON events(symbol);
	CREATE INDEX IF NOT EXISTS idx_events_date ON events(date);
	CREATE INDEX IF NOT EXISTS idx_journal_date ON journal(date);
	CREATE INDEX IF NOT EXISTS idx_watchlist_list ON watchlist(list_name);
	`

	_, err := s.db.Exec(schema)
	return err
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
