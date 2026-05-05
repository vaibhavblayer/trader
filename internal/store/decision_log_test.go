package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"zerodha-trader/internal/models"
)

func TestSQLiteDecisionLogsRoundTripInOrder(t *testing.T) {
	ctx := context.Background()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "decision_logs.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	decision := &models.Decision{
		ID:         "DEC_TEST_1",
		Timestamp:  time.Date(2026, 5, 4, 9, 30, 0, 0, time.UTC),
		Symbol:     "RELIANCE",
		Action:     "BUY",
		Confidence: 82,
		Outcome:    models.OutcomePending,
	}
	if err := store.SaveDecision(ctx, decision); err != nil {
		t.Fatalf("save decision: %v", err)
	}

	later := decision.Timestamp.Add(time.Minute)
	earlier := decision.Timestamp.Add(30 * time.Second)
	logs := []*models.DecisionLog{
		{
			ID:         "LOG_2",
			DecisionID: decision.ID,
			Timestamp:  later,
			Stage:      models.DecisionStageOrderSubmitted,
			Symbol:     decision.Symbol,
			Action:     decision.Action,
			Status:     "SUBMITTED",
			Message:    "broker submit",
			Payload: map[string]interface{}{
				"quantity": 10,
			},
		},
		{
			ID:         "LOG_1",
			DecisionID: decision.ID,
			Timestamp:  earlier,
			Stage:      models.DecisionStageGenerated,
			Symbol:     decision.Symbol,
			Action:     decision.Action,
			Status:     "OK",
			Message:    "generated",
			Payload: map[string]interface{}{
				"confidence": 82,
			},
		},
	}

	for _, log := range logs {
		if err := store.SaveDecisionLog(ctx, log); err != nil {
			t.Fatalf("save log %s: %v", log.ID, err)
		}
	}

	got, err := store.GetDecisionLogs(ctx, decision.ID)
	if err != nil {
		t.Fatalf("get logs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(got))
	}
	if got[0].ID != "LOG_1" || got[1].ID != "LOG_2" {
		t.Fatalf("logs not chronological: %#v", got)
	}
	if got[0].Payload["confidence"].(float64) != 82 {
		t.Fatalf("payload not restored: %#v", got[0].Payload)
	}
}
