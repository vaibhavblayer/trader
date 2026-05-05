package cli

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/models"
	"zerodha-trader/internal/store"
)

type paperCandidateReviewResult struct {
	CandidateID       string                            `json:"candidate_id"`
	Symbol            string                            `json:"symbol"`
	Strategy          string                            `json:"strategy"`
	Variant           string                            `json:"variant"`
	Status            string                            `json:"status"`
	Action            string                            `json:"action"`
	Reason            string                            `json:"reason"`
	TotalPredictions  int                               `json:"total_predictions"`
	ActivePredictions int                               `json:"active_predictions"`
	Evaluated         int                               `json:"evaluated"`
	Decisive          int                               `json:"decisive"`
	Right             int                               `json:"right"`
	Wrong             int                               `json:"wrong"`
	Expired           int                               `json:"expired"`
	WinRate           float64                           `json:"win_rate"`
	Expectancy        float64                           `json:"expectancy"`
	ProfitFactor      float64                           `json:"profit_factor"`
	ExpiredRate       float64                           `json:"expired_rate"`
	LossStreak        int                               `json:"loss_streak"`
	PaperDrawdown     float64                           `json:"paper_drawdown"`
	Flags             []string                          `json:"flags,omitempty"`
	RegimeStats       []paperCandidateRegimeReviewStats `json:"regime_stats,omitempty"`
	RegimeFlags       []string                          `json:"regime_flags,omitempty"`
	GraduationReady   bool                              `json:"graduation_ready"`
	GraduationReason  string                            `json:"graduation_reason,omitempty"`
	Applied           bool                              `json:"applied,omitempty"`
}

type paperCandidateRegimeReviewStats struct {
	Regime             string  `json:"regime"`
	TotalPredictions   int     `json:"total_predictions"`
	ActivePredictions  int     `json:"active_predictions"`
	Evaluated          int     `json:"evaluated"`
	Decisive           int     `json:"decisive"`
	Right              int     `json:"right"`
	Wrong              int     `json:"wrong"`
	Expired            int     `json:"expired"`
	WinRate            float64 `json:"win_rate"`
	Expectancy         float64 `json:"expectancy"`
	ProfitFactor       float64 `json:"profit_factor"`
	ExpiredRate        float64 `json:"expired_rate"`
	LossStreak         int     `json:"loss_streak"`
	PaperDrawdown      float64 `json:"paper_drawdown"`
	ForwardAllowed     bool    `json:"forward_allowed"`
	ForwardBlockReason string  `json:"forward_block_reason,omitempty"`
}

type paperCandidateReviewOptions struct {
	Days              int
	MinEvaluated      int
	MinDecisive       int
	MinExpectancy     float64
	MinPF             float64
	MaxExpiredRate    float64
	MaxLossStreak     int
	MaxDDMultiple     float64
	MinRegimeDecisive int
	Graduate          paperCandidateGraduationOptions
	Apply             bool
}

type paperCandidateGraduationOptions struct {
	MinEvaluated   int
	MinDecisive    int
	MinExpectancy  float64
	MinPF          float64
	MaxExpiredRate float64
	MaxDDMultiple  float64
}

func newPaperCandidateReviewCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "candidate-review",
		Short: "Review paper candidates and optionally pause underperformers",
		Long: `Review promoted paper candidates against their forward paper outcomes.

The command links predictions by setup name candidate:<candidate_id>, computes
paper expectancy, profit factor, expiry rate, loss streak, and drawdown, then
flags candidates that should be paused. It is report-only unless --apply is set.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPaperCandidateReview(cmd, app)
		},
	}
	cmd.Flags().String("symbol", "", "Filter by symbol")
	cmd.Flags().String("strategy", "", "Filter by strategy")
	cmd.Flags().String("status", models.PaperCandidateStatusActive, "Candidate status to review")
	cmd.Flags().Int("days", 90, "Number of recent days to review")
	cmd.Flags().Int("limit", 100, "Maximum candidates to review")
	cmd.Flags().Int("min-evaluated", 10, "Minimum evaluated predictions before expiry-rate demotion")
	cmd.Flags().Int("min-decisive", 5, "Minimum RIGHT/WRONG predictions before edge demotion")
	cmd.Flags().Float64("min-expectancy", 0, "Minimum decisive expectancy percent")
	cmd.Flags().Float64("min-profit-factor", 1.0, "Minimum decisive paper profit factor")
	cmd.Flags().Float64("max-expired-rate", 60, "Maximum expired percentage after min-evaluated")
	cmd.Flags().Int("max-loss-streak", 5, "Maximum consecutive WRONG predictions before pause")
	cmd.Flags().Float64("max-dd-multiple", 1.5, "Pause if paper drawdown exceeds historical drawdown times this multiple")
	cmd.Flags().Int("min-regime-decisive", 3, "Minimum decisive predictions before flagging a weak forward regime")
	cmd.Flags().Int("graduate-min-evaluated", 30, "Minimum evaluated predictions for graduation readiness")
	cmd.Flags().Int("graduate-min-decisive", 20, "Minimum decisive predictions for graduation readiness")
	cmd.Flags().Float64("graduate-min-expectancy", 0.2, "Minimum decisive expectancy percent for graduation readiness")
	cmd.Flags().Float64("graduate-min-profit-factor", 1.2, "Minimum paper profit factor for graduation readiness")
	cmd.Flags().Float64("graduate-max-expired-rate", 40, "Maximum expired percentage for graduation readiness")
	cmd.Flags().Float64("graduate-max-dd-multiple", 1.0, "Graduation paper drawdown must not exceed historical drawdown times this multiple")
	cmd.Flags().Bool("apply", false, "Apply PAUSED status to candidates that fail review")
	return cmd
}

func runPaperCandidateReview(cmd *cobra.Command, app *App) error {
	output := NewOutput(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if app.Store == nil {
		return fmt.Errorf("paper candidate store is not available")
	}

	symbol, _ := cmd.Flags().GetString("symbol")
	strategy, _ := cmd.Flags().GetString("strategy")
	status, _ := cmd.Flags().GetString("status")
	limit, _ := cmd.Flags().GetInt("limit")
	days, _ := cmd.Flags().GetInt("days")
	minEvaluated, _ := cmd.Flags().GetInt("min-evaluated")
	minDecisive, _ := cmd.Flags().GetInt("min-decisive")
	minExpectancy, _ := cmd.Flags().GetFloat64("min-expectancy")
	minPF, _ := cmd.Flags().GetFloat64("min-profit-factor")
	maxExpiredRate, _ := cmd.Flags().GetFloat64("max-expired-rate")
	maxLossStreak, _ := cmd.Flags().GetInt("max-loss-streak")
	maxDDMultiple, _ := cmd.Flags().GetFloat64("max-dd-multiple")
	minRegimeDecisive, _ := cmd.Flags().GetInt("min-regime-decisive")
	graduateMinEvaluated, _ := cmd.Flags().GetInt("graduate-min-evaluated")
	graduateMinDecisive, _ := cmd.Flags().GetInt("graduate-min-decisive")
	graduateMinExpectancy, _ := cmd.Flags().GetFloat64("graduate-min-expectancy")
	graduateMinPF, _ := cmd.Flags().GetFloat64("graduate-min-profit-factor")
	graduateMaxExpiredRate, _ := cmd.Flags().GetFloat64("graduate-max-expired-rate")
	graduateMaxDDMultiple, _ := cmd.Flags().GetFloat64("graduate-max-dd-multiple")
	apply, _ := cmd.Flags().GetBool("apply")

	candidates, err := app.Store.GetPaperCandidates(ctx, models.PaperCandidateFilter{
		Symbol:   strings.ToUpper(strings.TrimSpace(symbol)),
		Strategy: strings.ToLower(strings.TrimSpace(strategy)),
		Status:   strings.ToUpper(strings.TrimSpace(status)),
		Limit:    limit,
	})
	if err != nil {
		return err
	}

	results, err := reviewPaperCandidates(ctx, app.Store, candidates, paperCandidateReviewOptions{
		Days:              days,
		MinEvaluated:      minEvaluated,
		MinDecisive:       minDecisive,
		MinExpectancy:     minExpectancy,
		MinPF:             minPF,
		MaxExpiredRate:    maxExpiredRate,
		MaxLossStreak:     maxLossStreak,
		MaxDDMultiple:     maxDDMultiple,
		MinRegimeDecisive: minRegimeDecisive,
		Graduate: paperCandidateGraduationOptions{
			MinEvaluated:   graduateMinEvaluated,
			MinDecisive:    graduateMinDecisive,
			MinExpectancy:  graduateMinExpectancy,
			MinPF:          graduateMinPF,
			MaxExpiredRate: graduateMaxExpiredRate,
			MaxDDMultiple:  graduateMaxDDMultiple,
		},
		Apply: apply,
	})
	if err != nil {
		return err
	}
	if output.IsJSON() {
		return output.JSON(results)
	}
	displayPaperCandidateReviewResults(output, results, apply)
	return nil
}

func reviewPaperCandidates(ctx context.Context, dataStore store.DataStore, candidates []models.PaperCandidate, opts paperCandidateReviewOptions) ([]paperCandidateReviewResult, error) {
	if opts.MinEvaluated <= 0 {
		opts.MinEvaluated = 10
	}
	if opts.MinDecisive <= 0 {
		opts.MinDecisive = 5
	}
	if opts.MinPF <= 0 {
		opts.MinPF = 1
	}
	if opts.MaxExpiredRate <= 0 {
		opts.MaxExpiredRate = 60
	}
	if opts.MaxLossStreak <= 0 {
		opts.MaxLossStreak = 5
	}
	if opts.MaxDDMultiple <= 0 {
		opts.MaxDDMultiple = 1.5
	}
	if opts.MinRegimeDecisive <= 0 {
		opts.MinRegimeDecisive = 3
	}
	opts.Graduate = normalizeGraduationOptions(opts.Graduate)

	results := make([]paperCandidateReviewResult, 0, len(candidates))
	for _, candidate := range candidates {
		filter := store.PaperPredictionFilter{
			SetupName: fmt.Sprintf("candidate:%s", candidate.ID),
		}
		if opts.Days > 0 {
			filter.StartDate = time.Now().AddDate(0, 0, -opts.Days)
			filter.EndDate = time.Now()
		}
		predictions, err := dataStore.GetPaperPredictions(ctx, filter)
		if err != nil {
			return nil, err
		}
		result := buildPaperCandidateReview(candidate, predictions, opts)
		if opts.Apply && result.Action == "PAUSE" && candidate.Status != models.PaperCandidateStatusPaused {
			candidate.Status = models.PaperCandidateStatusPaused
			candidate.Reason = appendCandidateReviewReason(candidate.Reason, result.Flags)
			if err := dataStore.SavePaperCandidate(ctx, &candidate); err != nil {
				return nil, err
			}
			result.Applied = true
			result.Status = candidate.Status
		}
		results = append(results, result)
	}
	return results, nil
}

func buildPaperCandidateReview(candidate models.PaperCandidate, predictions []models.PaperPrediction, opts paperCandidateReviewOptions) paperCandidateReviewResult {
	opts.Graduate = normalizeGraduationOptions(opts.Graduate)
	result := paperCandidateReviewResult{
		CandidateID: candidate.ID,
		Symbol:      candidate.Symbol,
		Strategy:    candidate.Strategy,
		Variant:     candidate.ParamVariant,
		Status:      candidate.Status,
		Action:      "KEEP",
		Reason:      "insufficient_forward_evidence",
	}
	stats := calculatePaperCandidateOutcomeStats(predictions)
	result.TotalPredictions = stats.total
	result.ActivePredictions = stats.active
	result.Evaluated = stats.evaluated
	result.Decisive = stats.decisive
	result.Right = stats.right
	result.Wrong = stats.wrong
	result.Expired = stats.expired
	result.WinRate = stats.winRate
	result.Expectancy = stats.expectancy
	result.ProfitFactor = stats.profitFactor
	result.ExpiredRate = stats.expiredRate
	result.LossStreak = stats.lossStreak
	result.PaperDrawdown = stats.maxDrawdown
	result.RegimeStats = buildPaperCandidateRegimeStats(predictions, opts)
	for _, regime := range result.RegimeStats {
		if !regime.ForwardAllowed {
			result.RegimeFlags = append(result.RegimeFlags, regime.Regime+": "+regime.ForwardBlockReason)
		}
	}

	if result.Evaluated == 0 {
		return result
	}
	result.Reason = "forward_evidence_ok"
	if result.Decisive >= opts.MinDecisive {
		if result.Expectancy < opts.MinExpectancy {
			result.Flags = append(result.Flags, fmt.Sprintf("expectancy %.2f < %.2f", result.Expectancy, opts.MinExpectancy))
		}
		if result.ProfitFactor < opts.MinPF {
			result.Flags = append(result.Flags, fmt.Sprintf("profit_factor %.2f < %.2f", result.ProfitFactor, opts.MinPF))
		}
	}
	if result.Evaluated >= opts.MinEvaluated && result.ExpiredRate > opts.MaxExpiredRate {
		result.Flags = append(result.Flags, fmt.Sprintf("expired_rate %.1f%% > %.1f%%", result.ExpiredRate, opts.MaxExpiredRate))
	}
	if result.LossStreak >= opts.MaxLossStreak {
		result.Flags = append(result.Flags, fmt.Sprintf("loss_streak %d >= %d", result.LossStreak, opts.MaxLossStreak))
	}
	if candidate.MaxDrawdownPct > 0 && result.PaperDrawdown > candidate.MaxDrawdownPct*opts.MaxDDMultiple {
		result.Flags = append(result.Flags, fmt.Sprintf("paper_dd %.2f%% > historical_dd %.2f%% x %.2f", result.PaperDrawdown, candidate.MaxDrawdownPct, opts.MaxDDMultiple))
	}
	if len(result.Flags) > 0 {
		result.Action = "PAUSE"
		result.Reason = strings.Join(result.Flags, "; ")
		result.GraduationReason = "blocked_by_demotion_flags"
		return result
	}
	ready, reason := paperCandidateGraduationReadiness(candidate, result, opts.Graduate)
	result.GraduationReady = ready
	result.GraduationReason = reason
	if ready {
		result.Action = "READY"
		result.Reason = "graduation_ready"
	}
	return result
}

func normalizeGraduationOptions(opts paperCandidateGraduationOptions) paperCandidateGraduationOptions {
	if opts.MinEvaluated <= 0 {
		opts.MinEvaluated = 30
	}
	if opts.MinDecisive <= 0 {
		opts.MinDecisive = 20
	}
	if opts.MinPF <= 0 {
		opts.MinPF = 1.2
	}
	if opts.MaxExpiredRate <= 0 {
		opts.MaxExpiredRate = 40
	}
	if opts.MaxDDMultiple <= 0 {
		opts.MaxDDMultiple = 1
	}
	return opts
}

func paperCandidateGraduationReadiness(candidate models.PaperCandidate, result paperCandidateReviewResult, opts paperCandidateGraduationOptions) (bool, string) {
	reasons := make([]string, 0)
	if result.Evaluated < opts.MinEvaluated {
		reasons = append(reasons, fmt.Sprintf("evaluated %d < %d", result.Evaluated, opts.MinEvaluated))
	}
	if result.Decisive < opts.MinDecisive {
		reasons = append(reasons, fmt.Sprintf("decisive %d < %d", result.Decisive, opts.MinDecisive))
	}
	if result.Expectancy < opts.MinExpectancy {
		reasons = append(reasons, fmt.Sprintf("expectancy %.2f < %.2f", result.Expectancy, opts.MinExpectancy))
	}
	if result.ProfitFactor < opts.MinPF {
		reasons = append(reasons, fmt.Sprintf("profit_factor %.2f < %.2f", result.ProfitFactor, opts.MinPF))
	}
	if result.ExpiredRate > opts.MaxExpiredRate {
		reasons = append(reasons, fmt.Sprintf("expired_rate %.1f%% > %.1f%%", result.ExpiredRate, opts.MaxExpiredRate))
	}
	if candidate.MaxDrawdownPct > 0 && result.PaperDrawdown > candidate.MaxDrawdownPct*opts.MaxDDMultiple {
		reasons = append(reasons, fmt.Sprintf("paper_dd %.2f%% > historical_dd %.2f%% x %.2f", result.PaperDrawdown, candidate.MaxDrawdownPct, opts.MaxDDMultiple))
	}
	if len(reasons) > 0 {
		return false, strings.Join(reasons, "; ")
	}
	return true, "meets_graduation_thresholds"
}

type paperCandidateOutcomeStats struct {
	total        int
	active       int
	evaluated    int
	decisive     int
	right        int
	wrong        int
	expired      int
	winRate      float64
	expectancy   float64
	profitFactor float64
	expiredRate  float64
	lossStreak   int
	maxDrawdown  float64
}

func calculatePaperCandidateOutcomeStats(predictions []models.PaperPrediction) paperCandidateOutcomeStats {
	stats := paperCandidateOutcomeStats{total: len(predictions)}
	sort.Slice(predictions, func(i, j int) bool {
		return predictions[i].CreatedAt.Before(predictions[j].CreatedAt)
	})

	var decisivePnL float64
	var grossProfit float64
	var grossLoss float64
	var cumulative float64
	var peak float64
	currentLossStreak := 0
	for _, prediction := range predictions {
		if !prediction.Evaluated {
			stats.active++
			continue
		}
		stats.evaluated++
		cumulative += prediction.PnLPercent
		if cumulative > peak {
			peak = cumulative
		}
		drawdown := peak - cumulative
		if drawdown > stats.maxDrawdown {
			stats.maxDrawdown = drawdown
		}

		switch strings.ToUpper(prediction.Outcome) {
		case "RIGHT":
			stats.right++
			stats.decisive++
			decisivePnL += prediction.PnLPercent
			if prediction.PnLPercent > 0 {
				grossProfit += prediction.PnLPercent
			}
			currentLossStreak = 0
		case "WRONG":
			stats.wrong++
			stats.decisive++
			decisivePnL += prediction.PnLPercent
			if prediction.PnLPercent < 0 {
				grossLoss += math.Abs(prediction.PnLPercent)
			}
			currentLossStreak++
			if currentLossStreak > stats.lossStreak {
				stats.lossStreak = currentLossStreak
			}
		case "EXPIRED":
			stats.expired++
		}
	}
	if stats.decisive > 0 {
		stats.winRate = float64(stats.right) / float64(stats.decisive) * 100
		stats.expectancy = decisivePnL / float64(stats.decisive)
	}
	if stats.evaluated > 0 {
		stats.expiredRate = float64(stats.expired) / float64(stats.evaluated) * 100
	}
	switch {
	case grossLoss > 0:
		stats.profitFactor = grossProfit / grossLoss
	case grossProfit > 0:
		stats.profitFactor = 999
	default:
		stats.profitFactor = 0
	}
	return stats
}

func buildPaperCandidateRegimeStats(predictions []models.PaperPrediction, opts paperCandidateReviewOptions) []paperCandidateRegimeReviewStats {
	byRegime := make(map[string][]models.PaperPrediction)
	for _, prediction := range predictions {
		regime := paperPredictionRegime(prediction)
		if regime == "" {
			regime = "unknown"
		}
		byRegime[regime] = append(byRegime[regime], prediction)
	}
	result := make([]paperCandidateRegimeReviewStats, 0, len(byRegime))
	for regime, regimePredictions := range byRegime {
		stats := calculatePaperCandidateOutcomeStats(regimePredictions)
		item := paperCandidateRegimeReviewStats{
			Regime:             regime,
			TotalPredictions:   stats.total,
			ActivePredictions:  stats.active,
			Evaluated:          stats.evaluated,
			Decisive:           stats.decisive,
			Right:              stats.right,
			Wrong:              stats.wrong,
			Expired:            stats.expired,
			WinRate:            stats.winRate,
			Expectancy:         stats.expectancy,
			ProfitFactor:       stats.profitFactor,
			ExpiredRate:        stats.expiredRate,
			LossStreak:         stats.lossStreak,
			PaperDrawdown:      stats.maxDrawdown,
			ForwardAllowed:     true,
			ForwardBlockReason: "",
		}
		reasons := make([]string, 0)
		if item.Decisive >= opts.MinRegimeDecisive {
			if item.Expectancy < opts.MinExpectancy {
				reasons = append(reasons, fmt.Sprintf("expectancy %.2f < %.2f", item.Expectancy, opts.MinExpectancy))
			}
			if item.ProfitFactor < opts.MinPF {
				reasons = append(reasons, fmt.Sprintf("profit_factor %.2f < %.2f", item.ProfitFactor, opts.MinPF))
			}
		}
		if item.LossStreak >= opts.MaxLossStreak {
			reasons = append(reasons, fmt.Sprintf("loss_streak %d >= %d", item.LossStreak, opts.MaxLossStreak))
		}
		if len(reasons) > 0 {
			item.ForwardAllowed = false
			item.ForwardBlockReason = strings.Join(reasons, "; ")
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Decisive == result[j].Decisive {
			return result[i].Regime < result[j].Regime
		}
		return result[i].Decisive > result[j].Decisive
	})
	return result
}

func paperPredictionRegime(prediction models.PaperPrediction) string {
	for _, gate := range prediction.Gates {
		if strings.EqualFold(gate.Name, "regime_allowed") {
			return strings.ToLower(strings.TrimSpace(gate.Reason))
		}
	}
	return ""
}

func appendCandidateReviewReason(existing string, flags []string) string {
	reason := "auto_pause"
	if len(flags) > 0 {
		reason += ": " + strings.Join(flags, "; ")
	}
	existing = strings.TrimSpace(existing)
	if existing == "" {
		return reason
	}
	return existing + " | " + reason
}

func displayPaperCandidateReviewResults(output *Output, results []paperCandidateReviewResult, apply bool) {
	title := "Paper Candidate Review"
	if apply {
		title += " (apply)"
	}
	output.Bold(title)
	output.Println()
	if len(results) == 0 {
		output.Info("No paper candidates matched")
		return
	}
	table := NewTable(output, "Action", "Status", "Symbol", "Strategy", "Variant", "Eval", "Dec", "Win", "Exp", "PF", "Expect", "DD", "Grad", "Reason")
	for _, result := range results {
		graduation := "NO"
		if result.GraduationReady {
			graduation = "YES"
		}
		table.AddRow(
			result.Action,
			result.Status,
			result.Symbol,
			result.Strategy,
			result.Variant,
			fmt.Sprintf("%d", result.Evaluated),
			fmt.Sprintf("%d", result.Decisive),
			fmt.Sprintf("%.1f%%", result.WinRate),
			fmt.Sprintf("%.1f%%", result.ExpiredRate),
			fmt.Sprintf("%.2f", result.ProfitFactor),
			FormatPercent(result.Expectancy),
			FormatPercent(result.PaperDrawdown),
			graduation,
			result.Reason,
		)
	}
	table.Render()
	printPaperCandidateRegimeReview(output, results)
}

func printPaperCandidateRegimeReview(output *Output, results []paperCandidateReviewResult) {
	rows := make([]paperCandidateRegimeReviewStats, 0)
	for _, result := range results {
		for _, regime := range result.RegimeStats {
			if regime.Decisive == 0 && regime.Evaluated == 0 {
				continue
			}
			rows = append(rows, regime)
		}
	}
	if len(rows) == 0 {
		return
	}
	output.Println()
	output.Bold("Forward Regime Review")
	table := NewTable(output, "Regime", "Eval", "Dec", "Win", "Exp", "PF", "Expect", "DD", "Gate", "Reason")
	for _, regime := range rows {
		gate := "ALLOW"
		if !regime.ForwardAllowed {
			gate = "BLOCK"
		}
		table.AddRow(
			regime.Regime,
			fmt.Sprintf("%d", regime.Evaluated),
			fmt.Sprintf("%d", regime.Decisive),
			fmt.Sprintf("%.1f%%", regime.WinRate),
			fmt.Sprintf("%.1f%%", regime.ExpiredRate),
			fmt.Sprintf("%.2f", regime.ProfitFactor),
			FormatPercent(regime.Expectancy),
			FormatPercent(regime.PaperDrawdown),
			gate,
			regime.ForwardBlockReason,
		)
	}
	table.Render()
}
