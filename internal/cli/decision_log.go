package cli

import (
	"context"
	"fmt"
	"time"

	"zerodha-trader/internal/models"
)

func (app *App) logDecisionEvent(ctx context.Context, decision *models.Decision, stage models.DecisionLogStage, status, message string, payload map[string]interface{}) {
	if app.Store == nil || decision == nil || decision.ID == "" {
		return
	}
	if err := app.Store.SaveDecisionLog(ctx, &models.DecisionLog{
		ID:         fmt.Sprintf("DLOG_%d", time.Now().UnixNano()),
		DecisionID: decision.ID,
		Timestamp:  time.Now(),
		Stage:      stage,
		Symbol:     decision.Symbol,
		Action:     decision.Action,
		Status:     status,
		Message:    message,
		Payload:    payload,
	}); err != nil {
		app.Logger.Warn().Err(err).Str("decision_id", decision.ID).Str("stage", string(stage)).Msg("Failed to save decision log")
	}
}
