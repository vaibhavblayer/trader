package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
	"zerodha-trader/internal/store"
	"zerodha-trader/internal/trading"
)

type candidateRunResult struct {
	CandidateID string  `json:"candidate_id"`
	Symbol      string  `json:"symbol"`
	Strategy    string  `json:"strategy"`
	Variant     string  `json:"variant"`
	Timeframe   string  `json:"timeframe"`
	Setup       string  `json:"setup"`
	Regime      string  `json:"regime"`
	Signal      string  `json:"signal"`
	Confidence  float64 `json:"confidence"`
	Status      string  `json:"status"`
	Reason      string  `json:"reason,omitempty"`
	Prediction  string  `json:"prediction_id,omitempty"`
}

func newPaperCandidateRunCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "candidate-run",
		Short: "Run promoted paper candidates with regime guardrails",
		Long: `Evaluate promoted backtest candidates against current historical candles.

This command does not ask the LLM for discretion. It loads ACTIVE promoted
candidates, checks current regime against their allowed/blocked guardrails,
runs the same deterministic strategy variant used in backtests, and records a
paper prediction only when the latest candle emits BUY or SELL.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			if app.Store == nil {
				return fmt.Errorf("paper candidate store is not available")
			}
			if app.Broker == nil {
				return fmt.Errorf("broker not configured")
			}

			symbol, _ := cmd.Flags().GetString("symbol")
			strategy, _ := cmd.Flags().GetString("strategy")
			status, _ := cmd.Flags().GetString("status")
			days, _ := cmd.Flags().GetInt("days")
			minCandles, _ := cmd.Flags().GetInt("min-candles")
			regimeWindow, _ := cmd.Flags().GetInt("regime-window")
			timeWindowStr, _ := cmd.Flags().GetString("window")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			limit, _ := cmd.Flags().GetInt("limit")

			timeWindow, err := time.ParseDuration(timeWindowStr)
			if err != nil {
				return fmt.Errorf("invalid window %q: %w", timeWindowStr, err)
			}

			candidates, err := app.Store.GetPaperCandidates(ctx, models.PaperCandidateFilter{
				Symbol:   strings.ToUpper(strings.TrimSpace(symbol)),
				Strategy: trading.NormalizeStrategyName(strategy),
				Status:   strings.ToUpper(strings.TrimSpace(status)),
				Limit:    limit,
			})
			if err != nil {
				return err
			}
			results := runPaperCandidates(ctx, app, candidates, candidateRunOptions{
				Days:         days,
				MinCandles:   minCandles,
				RegimeWindow: regimeWindow,
				TimeWindow:   timeWindow,
				DryRun:       dryRun,
			})

			if output.IsJSON() {
				return output.JSON(results)
			}
			displayCandidateRunResults(output, results, dryRun)
			return nil
		},
	}
	cmd.Flags().String("symbol", "", "Filter candidates by symbol")
	cmd.Flags().String("strategy", "", "Filter candidates by strategy")
	cmd.Flags().String("status", models.PaperCandidateStatusActive, "Candidate status to run")
	cmd.Flags().Int("days", 120, "Historical lookback days for signal evaluation")
	cmd.Flags().Int("min-candles", 80, "Minimum candles required")
	cmd.Flags().Int("regime-window", 50, "Candles used to classify current regime")
	cmd.Flags().String("window", "24h", "Paper prediction evaluation window")
	cmd.Flags().Bool("dry-run", false, "Evaluate without saving paper predictions")
	cmd.Flags().Int("limit", 20, "Maximum candidates to evaluate")
	return cmd
}

type candidateRunOptions struct {
	Days         int
	MinCandles   int
	RegimeWindow int
	TimeWindow   time.Duration
	DryRun       bool
}

func runPaperCandidates(ctx context.Context, app *App, candidates []models.PaperCandidate, opts candidateRunOptions) []candidateRunResult {
	if opts.Days <= 0 {
		opts.Days = 120
	}
	if opts.MinCandles <= 0 {
		opts.MinCandles = 80
	}
	if opts.RegimeWindow <= 1 {
		opts.RegimeWindow = 50
	}
	if opts.TimeWindow <= 0 {
		opts.TimeWindow = 24 * time.Hour
	}

	results := make([]candidateRunResult, 0, len(candidates))
	engine := trading.NewBacktestEngine(nil)
	for _, candidate := range candidates {
		result := candidateRunResult{
			CandidateID: candidate.ID,
			Symbol:      candidate.Symbol,
			Strategy:    candidate.Strategy,
			Variant:     candidate.ParamVariant,
			Timeframe:   candidate.Timeframe,
			Setup:       candidate.Setup,
			Status:      "SKIP",
		}
		if candidate.Status != models.PaperCandidateStatusActive {
			result.Reason = "candidate_not_active"
			results = append(results, result)
			continue
		}
		if app.Store != nil {
			active, err := hasActivePaperCandidatePrediction(ctx, app.Store, candidate.ID)
			if err != nil {
				result.Status = "ERROR"
				result.Reason = err.Error()
				results = append(results, result)
				continue
			}
			if active {
				result.Status = "OPEN"
				result.Reason = "active_prediction_exists"
				results = append(results, result)
				continue
			}
		}

		candles, _, err := app.getQualityHistorical(ctx, broker.HistoricalRequest{
			Symbol:    candidate.Symbol,
			Exchange:  models.Exchange(candidate.Exchange),
			Timeframe: candidate.Timeframe,
			From:      time.Now().AddDate(0, 0, -opts.Days),
			To:        time.Now(),
		}, opts.MinCandles, false)
		if err != nil {
			result.Status = "ERROR"
			result.Reason = err.Error()
			results = append(results, result)
			continue
		}
		if len(candles) < opts.MinCandles {
			result.Reason = fmt.Sprintf("insufficient_candles:%d", len(candles))
			results = append(results, result)
			continue
		}

		start := len(candles) - opts.RegimeWindow
		if start < 0 {
			start = 0
		}
		result.Regime = classifyBacktestRegime(candles[start:])
		if isRegimeListed(candidate.BlockedRegimes, result.Regime) {
			result.Status = "BLOCK"
			result.Reason = "blocked_regime"
			results = append(results, result)
			continue
		}
		if len(candidate.AllowedRegimes) > 0 && !isRegimeListed(candidate.AllowedRegimes, result.Regime) {
			result.Status = "BLOCK"
			result.Reason = "regime_not_allowed"
			results = append(results, result)
			continue
		}

		params := parseParameterString(candidate.Parameters)
		signal, confidence, err := engine.LatestSignal(trading.BacktestConfig{
			Symbol:              candidate.Symbol,
			Timeframe:           candidate.Timeframe,
			Strategy:            candidate.Strategy,
			Parameters:          params,
			StopLossPercent:     candidate.StopLossPercent,
			TakeProfitPercent:   candidate.TakeProfitPercent,
			TrailingStopPercent: candidate.TrailingStopPercent,
			AllowShort:          candidate.AllowShort,
			InitialCapital:      1000000,
		}, candles)
		if err != nil {
			result.Status = "ERROR"
			result.Reason = err.Error()
			results = append(results, result)
			continue
		}
		result.Signal = signal
		result.Confidence = confidence
		if !isPaperCandidateSignalTradeable(signal, candidate.AllowShort) {
			result.Status = "NO_SIGNAL"
			result.Reason = "latest_signal_" + strings.ToLower(signal)
			results = append(results, result)
			continue
		}
		if opts.DryRun {
			result.Status = "DRY_SIGNAL"
			results = append(results, result)
			continue
		}

		prediction := paperPredictionFromCandidate(candidate, candles[len(candles)-1], signal, confidence, result.Regime, opts.TimeWindow)
		if err := app.Store.SavePaperPrediction(ctx, prediction); err != nil {
			result.Status = "ERROR"
			result.Reason = err.Error()
			results = append(results, result)
			continue
		}
		result.Status = "PREDICTED"
		result.Prediction = prediction.ID
		results = append(results, result)
	}
	return results
}

func hasActivePaperCandidatePrediction(ctx context.Context, dataStore interface {
	GetPaperPredictions(context.Context, store.PaperPredictionFilter) ([]models.PaperPrediction, error)
}, candidateID string) (bool, error) {
	if dataStore == nil {
		return false, nil
	}
	evaluated := false
	predictions, err := dataStore.GetPaperPredictions(ctx, store.PaperPredictionFilter{
		SetupName: fmt.Sprintf("candidate:%s", candidateID),
		Evaluated: &evaluated,
		Limit:     1,
	})
	if err != nil {
		return false, err
	}
	return len(predictions) > 0, nil
}

func paperPredictionFromCandidate(candidate models.PaperCandidate, candle models.Candle, signal string, confidence float64, regime string, window time.Duration) *models.PaperPrediction {
	entry := candle.Close
	action := strings.ToUpper(signal)
	target := entry
	stop := entry
	if action == "BUY" {
		if candidate.TakeProfitPercent > 0 {
			target = entry * (1 + candidate.TakeProfitPercent/100)
		}
		if candidate.StopLossPercent > 0 {
			stop = entry * (1 - candidate.StopLossPercent/100)
		}
	} else {
		if candidate.TakeProfitPercent > 0 {
			target = entry * (1 - candidate.TakeProfitPercent/100)
		}
		if candidate.StopLossPercent > 0 {
			stop = entry * (1 + candidate.StopLossPercent/100)
		}
	}
	now := time.Now()
	return &models.PaperPrediction{
		ID:          fmt.Sprintf("CAND_%s_%d", candidate.ID, now.UnixNano()),
		Symbol:      candidate.Symbol,
		Action:      action,
		Confidence:  clampPredictionConfidence(confidence),
		EntryPrice:  entry,
		TargetPrice: target,
		StopLoss:    stop,
		TimeWindow:  window,
		CreatedAt:   now,
		ExpiresAt:   now.Add(window),
		Reasoning: fmt.Sprintf(
			"candidate=%s strategy=%s variant=%s setup=%s regime=%s backtest_ret=%.2f val=%.2f pf=%.2f",
			candidate.ID, candidate.Strategy, candidate.ParamVariant, candidate.Setup, regime,
			candidate.ReturnPct, candidate.ValidationReturnPct, candidate.ProfitFactor,
		),
		SetupName: fmt.Sprintf("candidate:%s", candidate.ID),
		Timeframe: candidate.Timeframe,
		Gates: []models.PaperPredictionGate{
			{Name: "candidate_promoted", Passed: true, Reason: candidate.Verdict},
			{Name: "regime_allowed", Passed: true, Reason: regime},
			{Name: "deterministic_signal", Passed: true, Reason: action},
		},
	}
}

func isPaperCandidateSignalTradeable(signal string, allowShort bool) bool {
	switch strings.ToUpper(signal) {
	case "BUY":
		return true
	case "SELL":
		return allowShort
	default:
		return false
	}
}

func isRegimeListed(regimes []string, regime string) bool {
	for _, item := range regimes {
		if strings.EqualFold(item, regime) {
			return true
		}
	}
	return false
}

func parseParameterString(value string) map[string]interface{} {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	params := make(map[string]interface{})
	for _, part := range strings.Split(value, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, raw, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		raw = strings.TrimSpace(raw)
		if boolValue, ok := parseBoolLiteral(raw); ok {
			params[key] = boolValue
			continue
		}
		if intValue, err := strconv.Atoi(raw); err == nil {
			params[key] = intValue
			continue
		}
		if floatValue, err := strconv.ParseFloat(raw, 64); err == nil {
			params[key] = floatValue
			continue
		}
		params[key] = raw
	}
	if len(params) == 0 {
		return nil
	}
	return params
}

func parseBoolLiteral(value string) (bool, bool) {
	switch strings.ToLower(value) {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

func displayCandidateRunResults(output *Output, results []candidateRunResult, dryRun bool) {
	title := "Candidate Paper Run"
	if dryRun {
		title += " (dry run)"
	}
	output.Bold(title)
	output.Println()
	if len(results) == 0 {
		output.Info("No candidates matched")
		return
	}
	table := NewTable(output, "Status", "Symbol", "Strategy", "Variant", "TF", "Regime", "Signal", "Conf", "Reason", "Prediction")
	for _, result := range results {
		table.AddRow(
			result.Status,
			result.Symbol,
			result.Strategy,
			result.Variant,
			result.Timeframe,
			result.Regime,
			result.Signal,
			FormatConfidence(result.Confidence),
			result.Reason,
			result.Prediction,
		)
	}
	table.Render()
}
