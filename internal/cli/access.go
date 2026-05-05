package cli

import (
	"context"
	"path/filepath"

	"github.com/rs/zerolog"

	"zerodha-trader/internal/config"
	"zerodha-trader/internal/security"
)

func newAccessController(cfg *config.Config, logger zerolog.Logger) *security.AccessController {
	var auditLogger *security.AuditLogger
	if cfg.Security.AuditEnabled {
		configDir := cfg.ConfigDir
		if configDir == "" {
			configDir = config.DefaultConfigDir()
		}
		auditConfig := security.DefaultAuditConfig()
		auditConfig.LogDir = filepath.Join(configDir, "audit")
		var err error
		auditLogger, err = security.NewAuditLogger(auditConfig)
		if err != nil {
			logger.Warn().Err(err).Msg("Failed to initialize audit logger")
		} else if cfg.Credentials.Zerodha.UserID != "" {
			auditLogger.SetUserID(cfg.Credentials.Zerodha.UserID)
		}
	}

	return security.NewProfileAccessController(cfg.SafetyProfile(), cfg.Security.ReadOnlyMode, auditLogger)
}

func (app *App) checkPermission(ctx context.Context, op security.OperationType) error {
	if app.Access == nil {
		return nil
	}
	return app.Access.CheckPermission(ctx, op)
}

func (app *App) checkPlaceOrder(ctx context.Context) error {
	return app.checkPermission(ctx, security.OpPlaceOrder)
}

func (app *App) checkPlaceGTT(ctx context.Context) error {
	return app.checkPermission(ctx, security.OpPlaceGTT)
}

func (app *App) checkCancelGTT(ctx context.Context) error {
	return app.checkPermission(ctx, security.OpCancelGTT)
}

func (app *App) checkExitPosition(ctx context.Context) error {
	return app.checkPermission(ctx, security.OpExitPosition)
}

func (app *App) checkSavePlan(ctx context.Context) error {
	return app.checkPermission(ctx, security.OpSavePlan)
}

func (app *App) checkModifyPlan(ctx context.Context) error {
	return app.checkPermission(ctx, security.OpModifyPlan)
}

func (app *App) checkExecutePlan(ctx context.Context) error {
	return app.checkPermission(ctx, security.OpExecutePlan)
}

func (app *App) checkSaveAlert(ctx context.Context) error {
	return app.checkPermission(ctx, security.OpSaveAlert)
}

func (app *App) checkModifyWatchlist(ctx context.Context) error {
	return app.checkPermission(ctx, security.OpModifyWatchlist)
}

func (app *App) checkAutoTrade(ctx context.Context) error {
	return app.checkPermission(ctx, security.OpAutoTrade)
}

func (app *App) checkModifyConfig(ctx context.Context) error {
	return app.checkPermission(ctx, security.OpModifyConfig)
}
