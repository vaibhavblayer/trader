package cli

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
	"zerodha-trader/internal/store"
)

type paperEvaluationResult struct {
	ID         string    `json:"id"`
	Symbol     string    `json:"symbol"`
	Action     string    `json:"action"`
	SetupName  string    `json:"setup_name"`
	Timeframe  string    `json:"timeframe"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	EntryPrice float64   `json:"entry_price"`
	ExitPrice  float64   `json:"exit_price,omitempty"`
	Outcome    string    `json:"outcome,omitempty"`
	PnLPercent float64   `json:"pnl_percent,omitempty"`
	Status     string    `json:"status"`
	Reason     string    `json:"reason,omitempty"`
	Candles    int       `json:"candles"`
	DryRun     bool      `json:"dry_run,omitempty"`
}

func newPaperEvaluateCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evaluate",
		Short: "Evaluate open paper predictions from market candles",
		Long: `Evaluate active paper predictions against historical candles.

The evaluator replays candles after each prediction was created, marks target
hits as RIGHT, stop-loss hits as WRONG, and expired unresolved predictions as
EXPIRED. If target and stop are both inside the same candle, the result is
treated as WRONG to keep forward-test evidence conservative.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPaperEvaluation(cmd, app)
		},
	}
	cmd.Flags().String("symbol", "", "Filter by symbol")
	cmd.Flags().String("exchange", string(models.NSE), "Exchange for historical candles")
	cmd.Flags().String("eval-timeframe", "", "Historical candle timeframe for evaluation (default: derived from prediction window)")
	cmd.Flags().Int("days", 30, "Only evaluate predictions created in the last N days")
	cmd.Flags().Int("limit", 100, "Maximum active predictions to evaluate")
	cmd.Flags().Bool("dry-run", false, "Evaluate without saving outcomes")
	cmd.Flags().Bool("report", true, "Show calibration report after evaluation")
	return cmd
}

func runPaperEvaluation(cmd *cobra.Command, app *App) error {
	output := NewOutput(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if app.Store == nil {
		return fmt.Errorf("paper prediction store is not available")
	}
	if app.Broker == nil {
		return fmt.Errorf("broker not configured")
	}

	symbol, _ := cmd.Flags().GetString("symbol")
	exchangeFlag, _ := cmd.Flags().GetString("exchange")
	evalTimeframe, _ := cmd.Flags().GetString("eval-timeframe")
	days, _ := cmd.Flags().GetInt("days")
	limit, _ := cmd.Flags().GetInt("limit")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	showReport, _ := cmd.Flags().GetBool("report")

	evaluated := false
	filter := store.PaperPredictionFilter{
		Symbol:    strings.ToUpper(strings.TrimSpace(symbol)),
		Evaluated: &evaluated,
		Limit:     limit,
	}
	if days > 0 {
		filter.StartDate = time.Now().AddDate(0, 0, -days)
		filter.EndDate = time.Now()
	}

	predictions, err := app.Store.GetPaperPredictions(ctx, filter)
	if err != nil {
		return err
	}

	results := evaluatePaperPredictions(ctx, app, predictions, paperEvaluationOptions{
		Exchange:      models.Exchange(strings.ToUpper(strings.TrimSpace(exchangeFlag))),
		EvalTimeframe: evalTimeframe,
		Now:           time.Now(),
		DryRun:        dryRun,
	})

	if output.IsJSON() {
		payload := struct {
			Results []paperEvaluationResult       `json:"results"`
			Report  *models.PaperPredictionReport `json:"report,omitempty"`
			Error   string                        `json:"error,omitempty"`
		}{Results: results}
		if showReport {
			report, err := app.Store.GetPaperPredictionReport(ctx, paperEvaluationReportFilter(filter))
			if err != nil {
				payload.Error = err.Error()
			} else {
				payload.Report = report
			}
		}
		return output.JSON(payload)
	}

	displayPaperEvaluationResults(output, results, dryRun)
	if showReport {
		report, err := app.Store.GetPaperPredictionReport(ctx, paperEvaluationReportFilter(filter))
		if err != nil {
			return err
		}
		output.Println()
		printPaperEvaluationReport(output, report)
	}
	return nil
}

type paperEvaluationOptions struct {
	Exchange      models.Exchange
	EvalTimeframe string
	Now           time.Time
	DryRun        bool
}

func evaluatePaperPredictions(ctx context.Context, app *App, predictions []models.PaperPrediction, opts paperEvaluationOptions) []paperEvaluationResult {
	if opts.Exchange == "" {
		opts.Exchange = models.NSE
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}

	results := make([]paperEvaluationResult, 0, len(predictions))
	for _, prediction := range predictions {
		result := paperEvaluationResult{
			ID:         prediction.ID,
			Symbol:     prediction.Symbol,
			Action:     prediction.Action,
			SetupName:  prediction.SetupName,
			Timeframe:  prediction.Timeframe,
			CreatedAt:  prediction.CreatedAt,
			ExpiresAt:  prediction.ExpiresAt,
			EntryPrice: prediction.EntryPrice,
			Status:     "OPEN",
			DryRun:     opts.DryRun,
		}
		if prediction.Evaluated {
			result.Status = "SKIP"
			result.Reason = "already_evaluated"
			results = append(results, result)
			continue
		}
		if prediction.ExpiresAt.IsZero() && prediction.TimeWindow > 0 {
			prediction.ExpiresAt = prediction.CreatedAt.Add(prediction.TimeWindow)
			result.ExpiresAt = prediction.ExpiresAt
		}

		evalTimeframe := normalizeEvaluationTimeframe(opts.EvalTimeframe)
		if evalTimeframe == "" {
			evalTimeframe = predictionEvaluationTimeframe(prediction)
		}
		result.Timeframe = evalTimeframe

		tfDuration := historicalTimeframeDuration(evalTimeframe)
		from := prediction.CreatedAt
		if tfDuration > 0 {
			from = from.Add(-tfDuration)
		}
		to := opts.Now
		if !prediction.ExpiresAt.IsZero() && prediction.ExpiresAt.Before(to) {
			to = prediction.ExpiresAt
			if tfDuration > 0 {
				to = to.Add(tfDuration)
			}
		}
		if !to.After(from) {
			result.Reason = "waiting_for_future_candles"
			results = append(results, result)
			continue
		}

		candles, err := app.Broker.GetHistorical(ctx, broker.HistoricalRequest{
			Symbol:    prediction.Symbol,
			Exchange:  opts.Exchange,
			Timeframe: evalTimeframe,
			From:      from,
			To:        to,
		})
		if err != nil {
			result.Status = "ERROR"
			result.Reason = err.Error()
			results = append(results, result)
			continue
		}
		result.Candles = len(candles)
		evaluated, ok, reason := evaluatePaperPredictionFromCandles(prediction, candles, opts.Now, tfDuration)
		result.Reason = reason
		if !ok {
			results = append(results, result)
			continue
		}
		result.Status = "EVALUATED"
		result.ExitPrice = evaluated.ExitPrice
		result.Outcome = evaluated.Outcome
		result.PnLPercent = evaluated.PnLPercent
		if !opts.DryRun {
			if err := app.Store.SavePaperPrediction(ctx, &evaluated); err != nil {
				result.Status = "ERROR"
				result.Reason = err.Error()
			}
		}
		results = append(results, result)
	}
	return results
}

func evaluatePaperPredictionFromCandles(prediction models.PaperPrediction, candles []models.Candle, now time.Time, timeframe time.Duration) (models.PaperPrediction, bool, string) {
	if prediction.EntryPrice <= 0 {
		return prediction, false, "invalid_entry_price"
	}
	if prediction.ExpiresAt.IsZero() && prediction.TimeWindow > 0 {
		prediction.ExpiresAt = prediction.CreatedAt.Add(prediction.TimeWindow)
	}

	var lastEligible *models.Candle
	for i := range candles {
		candle := candles[i]
		if !paperEvaluationCandleOverlaps(candle.Timestamp, timeframe, prediction.CreatedAt, prediction.ExpiresAt) {
			continue
		}
		lastEligible = &candle
		if hitTargetAndStop(prediction, candle) {
			return completePaperPrediction(prediction, prediction.StopLoss, "WRONG"), true, "target_and_stop_same_candle_conservative_stop"
		}
		if hitTarget(prediction, candle) {
			return completePaperPrediction(prediction, prediction.TargetPrice, "RIGHT"), true, "target_hit"
		}
		if hitStop(prediction, candle) {
			return completePaperPrediction(prediction, prediction.StopLoss, "WRONG"), true, "stop_loss_hit"
		}
	}

	if !prediction.ExpiresAt.IsZero() && !now.Before(prediction.ExpiresAt) {
		exitPrice := prediction.EntryPrice
		if lastEligible != nil && lastEligible.Close > 0 {
			exitPrice = lastEligible.Close
		}
		return completePaperPrediction(prediction, exitPrice, "EXPIRED"), true, "expired_without_target_or_stop"
	}
	return prediction, false, "not_expired_no_target_or_stop"
}

func completePaperPrediction(prediction models.PaperPrediction, exitPrice float64, outcome string) models.PaperPrediction {
	prediction.Evaluated = true
	prediction.ExitPrice = exitPrice
	prediction.Outcome = outcome
	if strings.EqualFold(prediction.Action, "SELL") {
		prediction.PnLPercent = ((prediction.EntryPrice - exitPrice) / prediction.EntryPrice) * 100
	} else {
		prediction.PnLPercent = ((exitPrice - prediction.EntryPrice) / prediction.EntryPrice) * 100
	}
	if math.IsNaN(prediction.PnLPercent) || math.IsInf(prediction.PnLPercent, 0) {
		prediction.PnLPercent = 0
	}
	return prediction
}

func paperEvaluationCandleOverlaps(candleStart time.Time, timeframe time.Duration, predictionStart, predictionEnd time.Time) bool {
	if predictionEnd.IsZero() {
		predictionEnd = time.Now()
	}
	candleEnd := candleStart
	if timeframe > 0 {
		candleEnd = candleStart.Add(timeframe)
	}
	return candleEnd.After(predictionStart) && !candleStart.After(predictionEnd)
}

func hitTargetAndStop(prediction models.PaperPrediction, candle models.Candle) bool {
	return hitTarget(prediction, candle) && hitStop(prediction, candle)
}

func hitTarget(prediction models.PaperPrediction, candle models.Candle) bool {
	if prediction.TargetPrice <= 0 {
		return false
	}
	if strings.EqualFold(prediction.Action, "SELL") {
		return candle.Low <= prediction.TargetPrice
	}
	return candle.High >= prediction.TargetPrice
}

func hitStop(prediction models.PaperPrediction, candle models.Candle) bool {
	if prediction.StopLoss <= 0 {
		return false
	}
	if strings.EqualFold(prediction.Action, "SELL") {
		return candle.High >= prediction.StopLoss
	}
	return candle.Low <= prediction.StopLoss
}

func predictionEvaluationTimeframe(prediction models.PaperPrediction) string {
	if prediction.TimeWindow > 0 && prediction.TimeWindow <= 48*time.Hour {
		return "15min"
	}
	if normalized := normalizeEvaluationTimeframe(prediction.Timeframe); normalized != "" {
		return normalized
	}
	return "1day"
}

func normalizeEvaluationTimeframe(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return ""
	case "1m", "1min", "minute":
		return "1min"
	case "3m", "5m", "5min", "5minute":
		return "5min"
	case "10m", "15m", "15min", "15minute":
		return "15min"
	case "30m", "30min", "30minute":
		return "30min"
	case "1h", "60m", "1hour", "60minute":
		return "1hour"
	case "1d", "day", "1day":
		return "1day"
	default:
		return value
	}
}

func historicalTimeframeDuration(timeframe string) time.Duration {
	switch normalizeEvaluationTimeframe(timeframe) {
	case "1min":
		return time.Minute
	case "5min":
		return 5 * time.Minute
	case "15min":
		return 15 * time.Minute
	case "30min":
		return 30 * time.Minute
	case "1hour":
		return time.Hour
	case "1day":
		return 24 * time.Hour
	default:
		return 0
	}
}

func paperEvaluationReportFilter(filter store.PaperPredictionFilter) store.PaperPredictionFilter {
	filter.Evaluated = nil
	filter.Limit = 0
	return filter
}

func displayPaperEvaluationResults(output *Output, results []paperEvaluationResult, dryRun bool) {
	title := "Paper Outcome Evaluation"
	if dryRun {
		title += " (dry run)"
	}
	output.Bold(title)
	output.Println()
	if len(results) == 0 {
		output.Info("No active paper predictions matched")
		return
	}
	table := NewTable(output, "Status", "Symbol", "Action", "Setup", "TF", "Entry", "Exit", "Outcome", "P&L", "Reason")
	for _, result := range results {
		exit := "-"
		pnl := "-"
		outcome := result.Outcome
		if result.ExitPrice > 0 {
			exit = FormatPrice(result.ExitPrice)
		}
		if result.Outcome != "" {
			pnl = FormatPercent(result.PnLPercent)
		} else {
			outcome = "-"
		}
		table.AddRow(
			result.Status,
			result.Symbol,
			result.Action,
			result.SetupName,
			result.Timeframe,
			FormatPrice(result.EntryPrice),
			exit,
			outcome,
			pnl,
			result.Reason,
		)
	}
	table.Render()
}

func printPaperEvaluationReport(output *Output, report *models.PaperPredictionReport) {
	if report == nil || report.TotalPredictions == 0 {
		output.Info("No paper predictions available for calibration")
		return
	}
	output.Bold("Updated Paper Calibration")
	output.Printf("  Total:       %d (%d active, %d evaluated)\n", report.TotalPredictions, report.ActivePredictions, report.Evaluated)
	output.Printf("  Decisive:    %d | Right: %d | Wrong: %d | Expired: %d\n", report.Decisive, report.RightPredictions, report.WrongPredictions, report.ExpiredPredictions)
	output.Printf("  Win Rate:    %.1f%%\n", report.WinRate)
	output.Printf("  Avg P&L:     %.2f%%\n", report.AvgPnLPercent)
	output.Printf("  Expectancy:  %.2f%% per decisive prediction\n", report.Expectancy)
}
