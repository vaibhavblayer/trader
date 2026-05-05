package cli

import (
	"context"
	"fmt"
	"time"

	"zerodha-trader/internal/analysis/quality"
	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

func (app *App) getQualityHistorical(ctx context.Context, req broker.HistoricalRequest, minCandles int, checkStaleness bool) ([]models.Candle, *quality.Report, error) {
	if app.Broker == nil {
		return nil, nil, fmt.Errorf("broker not configured")
	}
	if req.Symbol == "" {
		return nil, nil, fmt.Errorf("symbol is required")
	}
	if req.Exchange == "" {
		req.Exchange = models.NSE
	}
	if req.Timeframe == "" {
		req.Timeframe = "1day"
	}
	if _, err := app.Broker.GetInstrumentToken(ctx, req.Symbol, req.Exchange); err != nil {
		return nil, nil, fmt.Errorf("symbol/token validation failed for %s:%s: %w", req.Exchange, req.Symbol, err)
	}

	candles, err := app.Broker.GetHistorical(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	report := quality.ValidateCandles(candles, quality.Config{
		Symbol:         req.Symbol,
		Exchange:       req.Exchange,
		Timeframe:      req.Timeframe,
		MinCandles:     minCandles,
		Now:            time.Now(),
		CheckStaleness: checkStaleness,
	})
	if !report.Valid() {
		return candles, &report, fmt.Errorf("data quality check failed: %s", report.Error())
	}
	return candles, &report, nil
}

func logQualityWarnings(app *App, report *quality.Report) {
	if app == nil || report == nil {
		return
	}
	for _, gate := range report.Gates {
		if gate.Severity == quality.SeverityWarn {
			app.Logger.Warn().
				Str("symbol", report.Symbol).
				Str("exchange", string(report.Exchange)).
				Str("timeframe", report.Timeframe).
				Str("gate", gate.Name).
				Msg(gate.Message)
		}
	}
}
