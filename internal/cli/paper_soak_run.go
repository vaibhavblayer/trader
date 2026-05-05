package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/models"
	"zerodha-trader/internal/store"
	"zerodha-trader/internal/trading"
)

type paperSoakRunReport struct {
	GeneratedAt        time.Time                       `json:"generated_at"`
	Symbol             string                          `json:"symbol,omitempty"`
	Strategy           string                          `json:"strategy,omitempty"`
	DryRun             bool                            `json:"dry_run"`
	ApplyReview        bool                            `json:"apply_review"`
	CandidatesLoaded   int                             `json:"candidates_loaded"`
	CandidatesChecked  int                             `json:"candidates_checked"`
	PredictionsCreated int                             `json:"predictions_created"`
	OpenPredictions    int                             `json:"open_predictions"`
	Blocked            int                             `json:"blocked"`
	NoSignal           int                             `json:"no_signal"`
	Errors             int                             `json:"errors"`
	OutcomesEvaluated  int                             `json:"outcomes_evaluated"`
	CandidatesPaused   int                             `json:"candidates_paused"`
	CandidatesReady    int                             `json:"candidates_ready"`
	ReadinessDecision  string                          `json:"readiness_decision,omitempty"`
	CandidateRun       []candidateRunResult            `json:"candidate_run"`
	Evaluations        []paperEvaluationResult         `json:"evaluations"`
	CandidateReview    []paperCandidateReviewResult    `json:"candidate_review"`
	Readiness          *models.AutonomyReadinessReport `json:"readiness,omitempty"`
}

func newPaperSoakRunCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "soak-run",
		Short: "Run one candidate-aware paper soak cycle",
		Long: `Run one operational paper-soak cycle.

The cycle evaluates outstanding paper predictions first, runs active promoted
candidates through regime and signal guardrails, reviews candidate forward
evidence for pause/ready status, and optionally prints autonomy readiness.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPaperSoakRun(cmd, app)
		},
	}
	cmd.Flags().String("symbol", "", "Filter by symbol")
	cmd.Flags().String("strategy", "", "Filter by strategy")
	cmd.Flags().String("status", models.PaperCandidateStatusActive, "Candidate status to run")
	cmd.Flags().String("exchange", string(models.NSE), "Exchange for prediction evaluation candles")
	cmd.Flags().Int("limit", 100, "Maximum candidates and predictions to process")
	cmd.Flags().Int("candidate-days", 120, "Historical lookback days for candidate signal evaluation")
	cmd.Flags().Int("min-candles", 80, "Minimum candles required for candidate signal evaluation")
	cmd.Flags().Int("regime-window", 50, "Candles used to classify current regime")
	cmd.Flags().String("window", "24h", "Paper prediction evaluation window for new candidate signals")
	cmd.Flags().Int("evaluate-days", 30, "Only evaluate predictions created in the last N days")
	cmd.Flags().String("eval-timeframe", "", "Historical candle timeframe for outcome evaluation")
	cmd.Flags().Int("review-days", 90, "Number of recent days to review candidate outcomes")
	cmd.Flags().Bool("apply-review", false, "Apply PAUSED status to candidates that fail review")
	cmd.Flags().Bool("dry-run", false, "Run without saving new predictions, outcomes, or candidate pauses")
	cmd.Flags().Bool("readiness", true, "Include autonomy readiness after the soak cycle")
	cmd.Flags().Int("readiness-days", 30, "Number of recent days for readiness checks")
	cmd.Flags().Int("min-decisive", 20, "Minimum decisive paper predictions for readiness")
	cmd.Flags().Int("min-reviewed-trades", 0, "Minimum post-trade reviews for readiness")
	cmd.Flags().Float64("min-win-rate", 50, "Minimum paper prediction win rate for readiness")
	cmd.Flags().Float64("min-expectancy", 0, "Minimum expectancy per decisive prediction for readiness")
	return cmd
}

func runPaperSoakRun(cmd *cobra.Command, app *App) error {
	output := NewOutput(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	if app.Store == nil {
		return fmt.Errorf("paper store is not available")
	}
	if app.Broker == nil {
		return fmt.Errorf("broker not configured")
	}

	symbol, _ := cmd.Flags().GetString("symbol")
	strategy, _ := cmd.Flags().GetString("strategy")
	status, _ := cmd.Flags().GetString("status")
	exchangeFlag, _ := cmd.Flags().GetString("exchange")
	limit, _ := cmd.Flags().GetInt("limit")
	candidateDays, _ := cmd.Flags().GetInt("candidate-days")
	minCandles, _ := cmd.Flags().GetInt("min-candles")
	regimeWindow, _ := cmd.Flags().GetInt("regime-window")
	windowFlag, _ := cmd.Flags().GetString("window")
	evaluateDays, _ := cmd.Flags().GetInt("evaluate-days")
	evalTimeframe, _ := cmd.Flags().GetString("eval-timeframe")
	reviewDays, _ := cmd.Flags().GetInt("review-days")
	applyReview, _ := cmd.Flags().GetBool("apply-review")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	includeReadiness, _ := cmd.Flags().GetBool("readiness")
	readinessDays, _ := cmd.Flags().GetInt("readiness-days")
	minDecisive, _ := cmd.Flags().GetInt("min-decisive")
	minReviewedTrades, _ := cmd.Flags().GetInt("min-reviewed-trades")
	minWinRate, _ := cmd.Flags().GetFloat64("min-win-rate")
	minExpectancy, _ := cmd.Flags().GetFloat64("min-expectancy")

	timeWindow, err := time.ParseDuration(windowFlag)
	if err != nil {
		return fmt.Errorf("invalid window %q: %w", windowFlag, err)
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	normalizedStrategy := trading.NormalizeStrategyName(strategy)

	report := paperSoakRunReport{
		GeneratedAt: time.Now(),
		Symbol:      symbol,
		Strategy:    normalizedStrategy,
		DryRun:      dryRun,
		ApplyReview: applyReview && !dryRun,
	}

	evaluationFilter := store.PaperPredictionFilter{
		Symbol: symbol,
		Limit:  limit,
	}
	evaluated := false
	evaluationFilter.Evaluated = &evaluated
	if evaluateDays > 0 {
		evaluationFilter.StartDate = time.Now().AddDate(0, 0, -evaluateDays)
		evaluationFilter.EndDate = time.Now()
	}
	openPredictions, err := app.Store.GetPaperPredictions(ctx, evaluationFilter)
	if err != nil {
		return err
	}
	report.OpenPredictions = len(openPredictions)
	report.Evaluations = evaluatePaperPredictions(ctx, app, openPredictions, paperEvaluationOptions{
		Exchange:      models.Exchange(strings.ToUpper(strings.TrimSpace(exchangeFlag))),
		EvalTimeframe: evalTimeframe,
		Now:           time.Now(),
		DryRun:        dryRun,
	})
	report.OutcomesEvaluated = countPaperEvaluationStatus(report.Evaluations, "EVALUATED")
	report.Errors += countPaperEvaluationStatus(report.Evaluations, "ERROR")

	candidates, err := app.Store.GetPaperCandidates(ctx, models.PaperCandidateFilter{
		Symbol:   symbol,
		Strategy: normalizedStrategy,
		Status:   strings.ToUpper(strings.TrimSpace(status)),
		Limit:    limit,
	})
	if err != nil {
		return err
	}
	report.CandidatesLoaded = len(candidates)
	report.CandidateRun = runPaperCandidates(ctx, app, candidates, candidateRunOptions{
		Days:         candidateDays,
		MinCandles:   minCandles,
		RegimeWindow: regimeWindow,
		TimeWindow:   timeWindow,
		DryRun:       dryRun,
	})
	report.CandidatesChecked = len(report.CandidateRun)
	report.PredictionsCreated = countCandidateRunStatus(report.CandidateRun, "PREDICTED")
	report.Blocked = countCandidateRunStatus(report.CandidateRun, "BLOCK")
	report.NoSignal = countCandidateRunStatus(report.CandidateRun, "NO_SIGNAL")
	report.OpenPredictions += countCandidateRunStatus(report.CandidateRun, "OPEN")
	report.Errors += countCandidateRunStatus(report.CandidateRun, "ERROR")

	report.CandidateReview, err = reviewPaperCandidates(ctx, app.Store, candidates, paperCandidateReviewOptions{
		Days:  reviewDays,
		Apply: applyReview && !dryRun,
	})
	if err != nil {
		return err
	}
	report.CandidatesPaused = countCandidateReviewAction(report.CandidateReview, "PAUSE")
	report.CandidatesReady = countCandidateReviewAction(report.CandidateReview, "READY")

	if includeReadiness {
		readiness, err := buildAutonomyReadinessReport(ctx, app, autonomyReadinessOptions{
			Days:               readinessDays,
			Symbol:             symbol,
			Phase:              autonomyPhasePaperSoak,
			MinDecisive:        minDecisive,
			MinReviewedTrades:  minReviewedTrades,
			MinWinRate:         minWinRate,
			MinExpectancy:      minExpectancy,
			MaxSlippageBp:      50,
			MaxRejectionRate:   10,
			MaxMissingLinkRate: 20,
		})
		if err != nil {
			return err
		}
		report.Readiness = readiness
		report.ReadinessDecision = string(readiness.Decision)
	}

	if output.IsJSON() {
		return output.JSON(report)
	}
	displayPaperSoakRunReport(output, report)
	return nil
}

func countCandidateRunStatus(results []candidateRunResult, status string) int {
	count := 0
	for _, result := range results {
		if strings.EqualFold(result.Status, status) {
			count++
		}
	}
	return count
}

func countPaperEvaluationStatus(results []paperEvaluationResult, status string) int {
	count := 0
	for _, result := range results {
		if strings.EqualFold(result.Status, status) {
			count++
		}
	}
	return count
}

func countCandidateReviewAction(results []paperCandidateReviewResult, action string) int {
	count := 0
	for _, result := range results {
		if strings.EqualFold(result.Action, action) {
			count++
		}
	}
	return count
}

func displayPaperSoakRunReport(output *Output, report paperSoakRunReport) {
	title := "Paper Soak Run"
	if report.DryRun {
		title += " (dry run)"
	}
	output.Bold(title)
	output.Println()
	if report.Symbol != "" {
		output.Printf("  Symbol:      %s\n", report.Symbol)
	}
	if report.Strategy != "" {
		output.Printf("  Strategy:    %s\n", report.Strategy)
	}
	output.Printf("  Candidates:  %d loaded | %d checked\n", report.CandidatesLoaded, report.CandidatesChecked)
	output.Printf("  Predictions: %d created | %d open before/blocked by active prediction\n", report.PredictionsCreated, report.OpenPredictions)
	output.Printf("  Outcomes:    %d evaluated\n", report.OutcomesEvaluated)
	output.Printf("  Review:      %d pause flags | %d ready\n", report.CandidatesPaused, report.CandidatesReady)
	output.Printf("  Signals:     %d blocked | %d no-signal | %d errors\n", report.Blocked, report.NoSignal, report.Errors)
	if report.ReadinessDecision != "" {
		output.Printf("  Readiness:   %s\n", report.ReadinessDecision)
	}
	output.Println()

	displayCandidateRunResults(output, report.CandidateRun, report.DryRun)
	output.Println()
	displayPaperEvaluationResults(output, report.Evaluations, report.DryRun)
	output.Println()
	displayPaperCandidateReviewResults(output, report.CandidateReview, report.ApplyReview)
	if report.Readiness != nil {
		output.Println()
		printAutonomyReadiness(output, report.Readiness)
	}
}
