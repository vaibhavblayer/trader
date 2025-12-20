// Package store provides data persistence implementations.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// InitIndianMarketSchema creates tables for Indian market specific features.
func (s *SQLiteStore) InitIndianMarketSchema() error {
	schema := `
	-- Corporate actions table
	CREATE TABLE IF NOT EXISTS corporate_actions (
		id TEXT PRIMARY KEY,
		symbol TEXT NOT NULL,
		action_type TEXT NOT NULL,
		ex_date DATE NOT NULL,
		record_date DATE,
		description TEXT,
		ratio TEXT,
		amount REAL,
		old_face_value REAL,
		new_face_value REAL,
		premium REAL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Surveillance status table
	CREATE TABLE IF NOT EXISTS surveillance_status (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT NOT NULL,
		category TEXT NOT NULL,
		asm_stage INTEGER,
		gsm_stage INTEGER,
		is_t2t INTEGER DEFAULT 0,
		additional_margin REAL,
		reason TEXT,
		effective_date DATE,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(symbol)
	);

	-- Bulk/Block deals table
	CREATE TABLE IF NOT EXISTS bulk_deals (
		id TEXT PRIMARY KEY,
		symbol TEXT NOT NULL,
		deal_type TEXT NOT NULL,
		date DATE NOT NULL,
		client_name TEXT,
		buy_sell TEXT NOT NULL,
		quantity INTEGER NOT NULL,
		price REAL NOT NULL,
		value REAL NOT NULL,
		exchange TEXT,
		remarks TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Delivery data table
	CREATE TABLE IF NOT EXISTS delivery_data (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT NOT NULL,
		date DATE NOT NULL,
		total_volume INTEGER NOT NULL,
		delivery_volume INTEGER NOT NULL,
		delivery_percent REAL NOT NULL,
		average_price REAL,
		delivery_value REAL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(symbol, date)
	);

	-- Promoter holdings table
	CREATE TABLE IF NOT EXISTS promoter_holdings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT NOT NULL,
		quarter TEXT NOT NULL,
		date DATE NOT NULL,
		promoter_percent REAL NOT NULL,
		public_percent REAL,
		dii_percent REAL,
		fii_percent REAL,
		pledge_percent REAL,
		pledge_value REAL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(symbol, quarter)
	);

	-- MF holdings table
	CREATE TABLE IF NOT EXISTS mf_holdings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT NOT NULL,
		quarter TEXT NOT NULL,
		date DATE NOT NULL,
		mf_percent REAL NOT NULL,
		num_schemes INTEGER,
		total_shares INTEGER,
		total_value REAL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(symbol, quarter)
	);

	-- Baskets table
	CREATE TABLE IF NOT EXISTS baskets (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		type TEXT NOT NULL,
		constituents TEXT NOT NULL,
		total_value REAL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Pledge status table
	CREATE TABLE IF NOT EXISTS pledge_status (
		id TEXT PRIMARY KEY,
		symbol TEXT NOT NULL,
		quantity INTEGER NOT NULL,
		type TEXT NOT NULL,
		status TEXT NOT NULL,
		requested_at DATETIME NOT NULL,
		processed_at DATETIME,
		reason TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Peak margin records table
	CREATE TABLE IF NOT EXISTS peak_margins (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		margin_used REAL NOT NULL,
		margin_avail REAL NOT NULL,
		utilization REAL NOT NULL,
		segment TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Circuit limits table
	CREATE TABLE IF NOT EXISTS circuit_limits (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT NOT NULL,
		exchange TEXT NOT NULL,
		band INTEGER NOT NULL,
		upper_limit REAL NOT NULL,
		lower_limit REAL NOT NULL,
		base_price REAL NOT NULL,
		date DATE NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(symbol, exchange, date)
	);

	-- Insider trades table
	CREATE TABLE IF NOT EXISTS insider_trades (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT NOT NULL,
		date DATE NOT NULL,
		person_name TEXT NOT NULL,
		designation TEXT,
		transaction_type TEXT NOT NULL,
		quantity INTEGER NOT NULL,
		price REAL NOT NULL,
		value REAL NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- SAST disclosures table
	CREATE TABLE IF NOT EXISTS sast_disclosures (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT NOT NULL,
		date DATE NOT NULL,
		acquirer_name TEXT NOT NULL,
		transaction_type TEXT NOT NULL,
		shares_before REAL,
		shares_acquired INTEGER,
		shares_after REAL,
		mode TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Create indexes for Indian market tables
	CREATE INDEX IF NOT EXISTS idx_corporate_actions_symbol ON corporate_actions(symbol);
	CREATE INDEX IF NOT EXISTS idx_corporate_actions_date ON corporate_actions(ex_date);
	CREATE INDEX IF NOT EXISTS idx_surveillance_symbol ON surveillance_status(symbol);
	CREATE INDEX IF NOT EXISTS idx_bulk_deals_symbol ON bulk_deals(symbol);
	CREATE INDEX IF NOT EXISTS idx_bulk_deals_date ON bulk_deals(date);
	CREATE INDEX IF NOT EXISTS idx_delivery_symbol ON delivery_data(symbol);
	CREATE INDEX IF NOT EXISTS idx_delivery_date ON delivery_data(date);
	CREATE INDEX IF NOT EXISTS idx_promoter_symbol ON promoter_holdings(symbol);
	CREATE INDEX IF NOT EXISTS idx_mf_symbol ON mf_holdings(symbol);
	CREATE INDEX IF NOT EXISTS idx_peak_margins_timestamp ON peak_margins(timestamp);
	CREATE INDEX IF NOT EXISTS idx_insider_trades_symbol ON insider_trades(symbol);
	CREATE INDEX IF NOT EXISTS idx_sast_symbol ON sast_disclosures(symbol);
	`

	_, err := s.db.Exec(schema)
	return err
}

// SaveCorporateAction saves a corporate action to the database.
func (s *SQLiteStore) SaveCorporateAction(ctx context.Context, action *CorporateActionRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO corporate_actions 
		(id, symbol, action_type, ex_date, record_date, description, ratio, amount, old_face_value, new_face_value, premium)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, action.ID, action.Symbol, action.ActionType, action.ExDate, action.RecordDate, 
		action.Description, action.Ratio, action.Amount, action.OldFaceValue, action.NewFaceValue, action.Premium)
	return err
}

// GetCorporateActions retrieves corporate actions for a symbol.
func (s *SQLiteStore) GetCorporateActions(ctx context.Context, symbol string, from, to time.Time) ([]CorporateActionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, symbol, action_type, ex_date, record_date, description, ratio, amount, old_face_value, new_face_value, premium
		FROM corporate_actions
		WHERE symbol = ? AND ex_date >= ? AND ex_date <= ?
		ORDER BY ex_date DESC
	`, symbol, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var actions []CorporateActionRecord
	for rows.Next() {
		var a CorporateActionRecord
		if err := rows.Scan(&a.ID, &a.Symbol, &a.ActionType, &a.ExDate, &a.RecordDate, 
			&a.Description, &a.Ratio, &a.Amount, &a.OldFaceValue, &a.NewFaceValue, &a.Premium); err != nil {
			return nil, err
		}
		actions = append(actions, a)
	}
	return actions, rows.Err()
}

// SaveSurveillanceStatus saves surveillance status to the database.
func (s *SQLiteStore) SaveSurveillanceStatus(ctx context.Context, status *SurveillanceRecord) error {
	isT2T := 0
	if status.IsT2T {
		isT2T = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO surveillance_status 
		(symbol, category, asm_stage, gsm_stage, is_t2t, additional_margin, reason, effective_date, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, status.Symbol, status.Category, status.ASMStage, status.GSMStage, isT2T, 
		status.AdditionalMargin, status.Reason, status.EffectiveDate, time.Now())
	return err
}

// GetSurveillanceStatus retrieves surveillance status for a symbol.
func (s *SQLiteStore) GetSurveillanceStatus(ctx context.Context, symbol string) (*SurveillanceRecord, error) {
	var status SurveillanceRecord
	var isT2T int
	err := s.db.QueryRowContext(ctx, `
		SELECT symbol, category, asm_stage, gsm_stage, is_t2t, additional_margin, reason, effective_date
		FROM surveillance_status WHERE symbol = ?
	`, symbol).Scan(&status.Symbol, &status.Category, &status.ASMStage, &status.GSMStage, 
		&isT2T, &status.AdditionalMargin, &status.Reason, &status.EffectiveDate)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	status.IsT2T = isT2T == 1
	return &status, nil
}

// SaveBulkDeal saves a bulk/block deal to the database.
func (s *SQLiteStore) SaveBulkDeal(ctx context.Context, deal *BulkDealRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO bulk_deals 
		(id, symbol, deal_type, date, client_name, buy_sell, quantity, price, value, exchange, remarks)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, deal.ID, deal.Symbol, deal.DealType, deal.Date, deal.ClientName, deal.BuySell, 
		deal.Quantity, deal.Price, deal.Value, deal.Exchange, deal.Remarks)
	return err
}

// GetBulkDeals retrieves bulk/block deals for a date range.
func (s *SQLiteStore) GetBulkDeals(ctx context.Context, from, to time.Time, dealType string) ([]BulkDealRecord, error) {
	query := `SELECT id, symbol, deal_type, date, client_name, buy_sell, quantity, price, value, exchange, remarks
		FROM bulk_deals WHERE date >= ? AND date <= ?`
	args := []interface{}{from, to}
	
	if dealType != "" {
		query += " AND deal_type = ?"
		args = append(args, dealType)
	}
	query += " ORDER BY date DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deals []BulkDealRecord
	for rows.Next() {
		var d BulkDealRecord
		if err := rows.Scan(&d.ID, &d.Symbol, &d.DealType, &d.Date, &d.ClientName, &d.BuySell, 
			&d.Quantity, &d.Price, &d.Value, &d.Exchange, &d.Remarks); err != nil {
			return nil, err
		}
		deals = append(deals, d)
	}
	return deals, rows.Err()
}

// SaveDeliveryData saves delivery data to the database.
func (s *SQLiteStore) SaveDeliveryData(ctx context.Context, data *DeliveryRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO delivery_data 
		(symbol, date, total_volume, delivery_volume, delivery_percent, average_price, delivery_value)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, data.Symbol, data.Date, data.TotalVolume, data.DeliveryVolume, data.DeliveryPercent, 
		data.AveragePrice, data.DeliveryValue)
	return err
}

// GetDeliveryData retrieves delivery data for a symbol.
func (s *SQLiteStore) GetDeliveryData(ctx context.Context, symbol string, days int) ([]DeliveryRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT symbol, date, total_volume, delivery_volume, delivery_percent, average_price, delivery_value
		FROM delivery_data
		WHERE symbol = ?
		ORDER BY date DESC
		LIMIT ?
	`, symbol, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var data []DeliveryRecord
	for rows.Next() {
		var d DeliveryRecord
		if err := rows.Scan(&d.Symbol, &d.Date, &d.TotalVolume, &d.DeliveryVolume, 
			&d.DeliveryPercent, &d.AveragePrice, &d.DeliveryValue); err != nil {
			return nil, err
		}
		data = append(data, d)
	}
	return data, rows.Err()
}

// SavePromoterHolding saves promoter holding data to the database.
func (s *SQLiteStore) SavePromoterHolding(ctx context.Context, holding *PromoterHoldingRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO promoter_holdings 
		(symbol, quarter, date, promoter_percent, public_percent, dii_percent, fii_percent, pledge_percent, pledge_value)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, holding.Symbol, holding.Quarter, holding.Date, holding.PromoterPercent, holding.PublicPercent, 
		holding.DIIPercent, holding.FIIPercent, holding.PledgePercent, holding.PledgeValue)
	return err
}

// GetPromoterHoldings retrieves promoter holdings for a symbol.
func (s *SQLiteStore) GetPromoterHoldings(ctx context.Context, symbol string, quarters int) ([]PromoterHoldingRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT symbol, quarter, date, promoter_percent, public_percent, dii_percent, fii_percent, pledge_percent, pledge_value
		FROM promoter_holdings
		WHERE symbol = ?
		ORDER BY date DESC
		LIMIT ?
	`, symbol, quarters)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var holdings []PromoterHoldingRecord
	for rows.Next() {
		var h PromoterHoldingRecord
		if err := rows.Scan(&h.Symbol, &h.Quarter, &h.Date, &h.PromoterPercent, &h.PublicPercent, 
			&h.DIIPercent, &h.FIIPercent, &h.PledgePercent, &h.PledgeValue); err != nil {
			return nil, err
		}
		holdings = append(holdings, h)
	}
	return holdings, rows.Err()
}

// SaveMFHolding saves MF holding data to the database.
func (s *SQLiteStore) SaveMFHolding(ctx context.Context, holding *MFHoldingRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO mf_holdings 
		(symbol, quarter, date, mf_percent, num_schemes, total_shares, total_value)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, holding.Symbol, holding.Quarter, holding.Date, holding.MFPercent, 
		holding.NumSchemes, holding.TotalShares, holding.TotalValue)
	return err
}

// GetMFHoldings retrieves MF holdings for a symbol.
func (s *SQLiteStore) GetMFHoldings(ctx context.Context, symbol string, quarters int) ([]MFHoldingRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT symbol, quarter, date, mf_percent, num_schemes, total_shares, total_value
		FROM mf_holdings
		WHERE symbol = ?
		ORDER BY date DESC
		LIMIT ?
	`, symbol, quarters)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var holdings []MFHoldingRecord
	for rows.Next() {
		var h MFHoldingRecord
		if err := rows.Scan(&h.Symbol, &h.Quarter, &h.Date, &h.MFPercent, 
			&h.NumSchemes, &h.TotalShares, &h.TotalValue); err != nil {
			return nil, err
		}
		holdings = append(holdings, h)
	}
	return holdings, rows.Err()
}

// SaveBasket saves a basket to the database.
func (s *SQLiteStore) SaveBasket(ctx context.Context, basket *BasketRecord) error {
	constituentsJSON, _ := json.Marshal(basket.Constituents)
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO baskets 
		(id, name, type, constituents, total_value, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, basket.ID, basket.Name, basket.Type, string(constituentsJSON), basket.TotalValue, basket.CreatedAt, time.Now())
	return err
}

// GetBasket retrieves a basket by ID.
func (s *SQLiteStore) GetBasket(ctx context.Context, id string) (*BasketRecord, error) {
	var basket BasketRecord
	var constituentsJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, type, constituents, total_value, created_at, updated_at
		FROM baskets WHERE id = ?
	`, id).Scan(&basket.ID, &basket.Name, &basket.Type, &constituentsJSON, &basket.TotalValue, &basket.CreatedAt, &basket.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(constituentsJSON), &basket.Constituents)
	return &basket, nil
}

// ListBaskets retrieves all baskets.
func (s *SQLiteStore) ListBaskets(ctx context.Context) ([]BasketRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, type, constituents, total_value, created_at, updated_at
		FROM baskets ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var baskets []BasketRecord
	for rows.Next() {
		var b BasketRecord
		var constituentsJSON string
		if err := rows.Scan(&b.ID, &b.Name, &b.Type, &constituentsJSON, &b.TotalValue, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(constituentsJSON), &b.Constituents)
		baskets = append(baskets, b)
	}
	return baskets, rows.Err()
}

// SavePeakMargin saves a peak margin record.
func (s *SQLiteStore) SavePeakMargin(ctx context.Context, record *PeakMarginRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO peak_margins (timestamp, margin_used, margin_avail, utilization, segment)
		VALUES (?, ?, ?, ?, ?)
	`, record.Timestamp, record.MarginUsed, record.MarginAvail, record.Utilization, record.Segment)
	return err
}

// GetPeakMargins retrieves peak margin records for a date range.
func (s *SQLiteStore) GetPeakMargins(ctx context.Context, from, to time.Time) ([]PeakMarginRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT timestamp, margin_used, margin_avail, utilization, segment
		FROM peak_margins
		WHERE timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp DESC
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []PeakMarginRecord
	for rows.Next() {
		var r PeakMarginRecord
		if err := rows.Scan(&r.Timestamp, &r.MarginUsed, &r.MarginAvail, &r.Utilization, &r.Segment); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// Record types for Indian market features

// CorporateActionRecord represents a corporate action database record.
type CorporateActionRecord struct {
	ID           string
	Symbol       string
	ActionType   string
	ExDate       time.Time
	RecordDate   time.Time
	Description  string
	Ratio        string
	Amount       float64
	OldFaceValue float64
	NewFaceValue float64
	Premium      float64
}

// SurveillanceRecord represents a surveillance status database record.
type SurveillanceRecord struct {
	Symbol           string
	Category         string
	ASMStage         int
	GSMStage         int
	IsT2T            bool
	AdditionalMargin float64
	Reason           string
	EffectiveDate    time.Time
}

// BulkDealRecord represents a bulk/block deal database record.
type BulkDealRecord struct {
	ID         string
	Symbol     string
	DealType   string
	Date       time.Time
	ClientName string
	BuySell    string
	Quantity   int64
	Price      float64
	Value      float64
	Exchange   string
	Remarks    string
}

// DeliveryRecord represents a delivery data database record.
type DeliveryRecord struct {
	Symbol          string
	Date            time.Time
	TotalVolume     int64
	DeliveryVolume  int64
	DeliveryPercent float64
	AveragePrice    float64
	DeliveryValue   float64
}

// PromoterHoldingRecord represents a promoter holding database record.
type PromoterHoldingRecord struct {
	Symbol          string
	Quarter         string
	Date            time.Time
	PromoterPercent float64
	PublicPercent   float64
	DIIPercent      float64
	FIIPercent      float64
	PledgePercent   float64
	PledgeValue     float64
}

// MFHoldingRecord represents a MF holding database record.
type MFHoldingRecord struct {
	Symbol      string
	Quarter     string
	Date        time.Time
	MFPercent   float64
	NumSchemes  int
	TotalShares int64
	TotalValue  float64
}

// BasketRecord represents a basket database record.
type BasketRecord struct {
	ID           string
	Name         string
	Type         string
	Constituents []BasketConstituentRecord
	TotalValue   float64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// BasketConstituentRecord represents a basket constituent.
type BasketConstituentRecord struct {
	Symbol   string  `json:"symbol"`
	Weight   float64 `json:"weight"`
	Quantity int     `json:"quantity"`
}

// PeakMarginRecord represents a peak margin database record.
type PeakMarginRecord struct {
	Timestamp   time.Time
	MarginUsed  float64
	MarginAvail float64
	Utilization float64
	Segment     string
}

// Ensure fmt import is used
var _ = fmt.Sprintf
