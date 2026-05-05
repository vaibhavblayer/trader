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

type paperCandidateHealthResult struct {
	CandidateID       string    `json:"candidate_id"`
	Symbol            string    `json:"symbol"`
	Strategy          string    `json:"strategy"`
	Variant           string    `json:"variant"`
	Status            string    `json:"status"`
	PromotedAt        time.Time `json:"promoted_at"`
	AgeDays           float64   `json:"age_days"`
	TotalPredictions  int       `json:"total_predictions"`
	ActivePredictions int       `json:"active_predictions"`
	Evaluated         int       `json:"evaluated"`
	Decisive          int       `json:"decisive"`
	LastPredictionAt  time.Time `json:"last_prediction_at,omitempty"`
	LastEvaluatedAt   time.Time `json:"last_evaluated_at,omitempty"`
	ProbeStatus       string    `json:"probe_status,omitempty"`
	ProbeReason       string    `json:"probe_reason,omitempty"`
	ProbeRegimeMode   string    `json:"probe_regime_mode,omitempty"`
	ProbeRegimeGate   string    `json:"probe_regime_gate,omitempty"`
	ProbeRegime       string    `json:"probe_regime,omitempty"`
	ProbeSignal       string    `json:"probe_signal,omitempty"`
	Health            string    `json:"health"`
	Flags             []string  `json:"flags,omitempty"`
}

func newPaperCandidateHealthCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "candidate-health",
		Short: "Report stale and no-signal paper candidates",
		Long: `Report operational health for promoted paper candidates.

The report uses stored paper predictions to flag stale candidates with no
forward evidence. With --probe it also runs a dry candidate guardrail check to
show whether the candidate is currently blocked, open, no-signal, or tradeable.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPaperCandidateHealth(cmd, app)
		},
	}
	cmd.Flags().String("symbol", "", "Filter by symbol")
	cmd.Flags().String("strategy", "", "Filter by strategy")
	cmd.Flags().String("status", models.PaperCandidateStatusActive, "Candidate status to review")
	cmd.Flags().Int("limit", 100, "Maximum candidates to review")
	cmd.Flags().Int("days", 90, "Number of recent days of prediction evidence")
	cmd.Flags().Int("stale-days", 14, "Flag active candidates older than N days without forward evidence")
	cmd.Flags().Bool("probe", false, "Dry-run current guardrails to classify no-signal/blocked candidates")
	cmd.Flags().Int("probe-days", 120, "Historical lookback days for probe")
	cmd.Flags().Int("probe-min-candles", 80, "Minimum candles required for probe")
	cmd.Flags().Int("probe-regime-window", 50, "Candles used to classify probe regime")
	cmd.Flags().String("probe-regime-mode", regimeModeStrict, "Probe regime guardrail mode: strict, allow-unknown, or explore")
	return cmd
}

func runPaperCandidateHealth(cmd *cobra.Command, app *App) error {
	output := NewOutput(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if app.Store == nil {
		return fmt.Errorf("paper candidate store is not available")
	}
	symbol, _ := cmd.Flags().GetString("symbol")
	strategy, _ := cmd.Flags().GetString("strategy")
	status, _ := cmd.Flags().GetString("status")
	limit, _ := cmd.Flags().GetInt("limit")
	days, _ := cmd.Flags().GetInt("days")
	staleDays, _ := cmd.Flags().GetInt("stale-days")
	probe, _ := cmd.Flags().GetBool("probe")
	probeDays, _ := cmd.Flags().GetInt("probe-days")
	probeMinCandles, _ := cmd.Flags().GetInt("probe-min-candles")
	probeRegimeWindow, _ := cmd.Flags().GetInt("probe-regime-window")
	probeRegimeModeFlag, _ := cmd.Flags().GetString("probe-regime-mode")
	probeRegimeMode, err := parseCandidateRegimeMode(probeRegimeModeFlag)
	if err != nil {
		return err
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
	if probe && app.Broker == nil {
		return fmt.Errorf("broker not configured")
	}
	results, err := buildPaperCandidateHealth(ctx, app, candidates, paperCandidateHealthOptions{
		Days:              days,
		StaleDays:         staleDays,
		Probe:             probe,
		ProbeDays:         probeDays,
		ProbeMinCandles:   probeMinCandles,
		ProbeRegimeWindow: probeRegimeWindow,
		ProbeRegimeMode:   probeRegimeMode,
	})
	if err != nil {
		return err
	}
	if output.IsJSON() {
		return output.JSON(results)
	}
	displayPaperCandidateHealth(output, results)
	return nil
}

type paperCandidateHealthOptions struct {
	Days              int
	StaleDays         int
	Probe             bool
	ProbeDays         int
	ProbeMinCandles   int
	ProbeRegimeWindow int
	ProbeRegimeMode   string
}

func buildPaperCandidateHealth(ctx context.Context, app *App, candidates []models.PaperCandidate, opts paperCandidateHealthOptions) ([]paperCandidateHealthResult, error) {
	if opts.StaleDays <= 0 {
		opts.StaleDays = 14
	}
	results := make([]paperCandidateHealthResult, 0, len(candidates))
	probeResults := map[string]candidateRunResult{}
	if opts.Probe && len(candidates) > 0 {
		for _, result := range runPaperCandidates(ctx, app, candidates, candidateRunOptions{
			Days:         opts.ProbeDays,
			MinCandles:   opts.ProbeMinCandles,
			RegimeWindow: opts.ProbeRegimeWindow,
			RegimeMode:   opts.ProbeRegimeMode,
			TimeWindow:   24 * time.Hour,
			DryRun:       true,
		}) {
			probeResults[result.CandidateID] = result
		}
	}

	now := time.Now()
	for _, candidate := range candidates {
		predictions, err := app.Store.GetPaperPredictions(ctx, store.PaperPredictionFilter{
			SetupName: fmt.Sprintf("candidate:%s", candidate.ID),
			StartDate: candidateHealthStartDate(opts.Days),
		})
		if err != nil {
			return nil, err
		}
		result := buildPaperCandidateHealthResult(candidate, predictions, now, opts.StaleDays)
		if probe, ok := probeResults[candidate.ID]; ok {
			result.ProbeStatus = probe.Status
			result.ProbeReason = probe.Reason
			result.ProbeRegimeMode = probe.RegimeMode
			result.ProbeRegimeGate = probe.RegimeGate
			result.ProbeRegime = probe.Regime
			result.ProbeSignal = probe.Signal
			result.Flags = append(result.Flags, paperCandidateProbeFlags(probe)...)
			result.Health = paperCandidateHealthStatus(result.Flags)
		}
		results = append(results, result)
	}
	return results, nil
}

func buildPaperCandidateHealthResult(candidate models.PaperCandidate, predictions []models.PaperPrediction, now time.Time, staleDays int) paperCandidateHealthResult {
	result := paperCandidateHealthResult{
		CandidateID: candidate.ID,
		Symbol:      candidate.Symbol,
		Strategy:    candidate.Strategy,
		Variant:     candidate.ParamVariant,
		Status:      candidate.Status,
		PromotedAt:  candidate.PromotedAt,
		Health:      "OK",
	}
	if !candidate.PromotedAt.IsZero() {
		result.AgeDays = now.Sub(candidate.PromotedAt).Hours() / 24
	}
	stats := calculatePaperCandidateOutcomeStats(predictions)
	result.TotalPredictions = stats.total
	result.ActivePredictions = stats.active
	result.Evaluated = stats.evaluated
	result.Decisive = stats.decisive
	for _, prediction := range predictions {
		if prediction.CreatedAt.After(result.LastPredictionAt) {
			result.LastPredictionAt = prediction.CreatedAt
		}
		if prediction.Evaluated && prediction.CreatedAt.After(result.LastEvaluatedAt) {
			result.LastEvaluatedAt = prediction.CreatedAt
		}
	}
	if strings.EqualFold(candidate.Status, models.PaperCandidateStatusActive) && result.AgeDays >= float64(staleDays) {
		if result.TotalPredictions == 0 {
			result.Flags = append(result.Flags, "stale_no_predictions")
		} else if result.Evaluated == 0 && result.ActivePredictions == 0 {
			result.Flags = append(result.Flags, "stale_no_outcomes")
		}
	}
	if result.ActivePredictions > 0 {
		result.Flags = append(result.Flags, "active_prediction_open")
	}
	result.Health = paperCandidateHealthStatus(result.Flags)
	return result
}

func candidateHealthStartDate(days int) time.Time {
	if days <= 0 {
		return time.Time{}
	}
	return time.Now().AddDate(0, 0, -days)
}

func paperCandidateProbeFlags(result candidateRunResult) []string {
	switch strings.ToUpper(result.Status) {
	case "NO_SIGNAL":
		return []string{"probe_no_signal"}
	case "BLOCK":
		return []string{"probe_blocked:" + result.Reason}
	case "ERROR":
		return []string{"probe_error"}
	case "OPEN":
		return []string{"active_prediction_open"}
	default:
		return nil
	}
}

func paperCandidateHealthStatus(flags []string) string {
	if len(flags) == 0 {
		return "OK"
	}
	for _, flag := range flags {
		if strings.Contains(flag, "error") || strings.Contains(flag, "stale") {
			return "WARN"
		}
	}
	return "INFO"
}

func displayPaperCandidateHealth(output *Output, results []paperCandidateHealthResult) {
	output.Bold("Paper Candidate Health")
	output.Println()
	if len(results) == 0 {
		output.Info("No paper candidates matched")
		return
	}
	table := NewTable(output, "Health", "Symbol", "Strategy", "Variant", "Age", "Pred", "Eval", "Dec", "Last Pred", "Probe", "Mode", "Regime", "Gate", "Flags")
	for _, result := range results {
		table.AddRow(
			result.Health,
			result.Symbol,
			result.Strategy,
			result.Variant,
			fmt.Sprintf("%.0fd", result.AgeDays),
			fmt.Sprintf("%d", result.TotalPredictions),
			fmt.Sprintf("%d", result.Evaluated),
			fmt.Sprintf("%d", result.Decisive),
			formatOptionalDateTime(result.LastPredictionAt),
			result.ProbeStatus,
			result.ProbeRegimeMode,
			result.ProbeRegime,
			result.ProbeRegimeGate,
			strings.Join(result.Flags, ","),
		)
	}
	table.Render()
}

func formatOptionalDateTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return FormatDateTime(value)
}
