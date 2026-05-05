package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteDeleteWatchlist(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "watchlists.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	for _, symbol := range []string{"RELIANCE", "TCS"} {
		if err := store.AddToWatchlist(ctx, symbol, "smoke"); err != nil {
			t.Fatalf("add %s: %v", symbol, err)
		}
	}

	if err := store.DeleteWatchlist(ctx, "smoke"); err != nil {
		t.Fatalf("delete watchlist: %v", err)
	}

	symbols, err := store.GetWatchlist(ctx, "smoke")
	if err != nil {
		t.Fatalf("get watchlist: %v", err)
	}
	if len(symbols) != 0 {
		t.Fatalf("expected empty watchlist after delete, got %#v", symbols)
	}

	if err := store.DeleteWatchlist(ctx, "smoke"); err == nil {
		t.Fatal("expected missing watchlist delete to fail")
	}
}
