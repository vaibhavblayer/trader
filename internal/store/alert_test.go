package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestSQLiteAlertsAlwaysHaveIDs(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "alerts.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	alert := &models.Alert{
		Symbol:    "TCS",
		Condition: "above",
		Price:     3200,
		CreatedAt: time.Now(),
	}
	if err := store.SaveAlert(ctx, alert); err != nil {
		t.Fatalf("save alert: %v", err)
	}
	if !strings.HasPrefix(alert.ID, "ALERT_") {
		t.Fatalf("expected generated alert ID, got %q", alert.ID)
	}

	alerts, err := store.GetActiveAlerts(ctx)
	if err != nil {
		t.Fatalf("get alerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if strings.TrimSpace(alerts[0].ID) == "" {
		t.Fatal("expected stored alert ID")
	}

	if err := store.DeleteAlert(ctx, alerts[0].ID); err != nil {
		t.Fatalf("delete alert: %v", err)
	}
	alerts, err = store.GetActiveAlerts(ctx)
	if err != nil {
		t.Fatalf("get alerts after delete: %v", err)
	}
	if len(alerts) != 0 {
		t.Fatalf("expected no active alerts after delete, got %d", len(alerts))
	}
}

func TestSQLiteAlertBlankIDBackfill(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "alerts_backfill.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `
		INSERT INTO alerts (id, symbol, condition, price, triggered, created_at)
		VALUES ('', 'INFY', 'below', 1400, 0, ?)
	`, time.Now()); err != nil {
		t.Fatalf("insert blank alert: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	store, err = NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store.Close()

	alerts, err := store.GetActiveAlerts(ctx)
	if err != nil {
		t.Fatalf("get alerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if !strings.HasPrefix(alerts[0].ID, "ALERT_") {
		t.Fatalf("expected backfilled alert ID, got %q", alerts[0].ID)
	}
}
