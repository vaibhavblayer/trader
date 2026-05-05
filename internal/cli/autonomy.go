package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/models"
	"zerodha-trader/internal/store"
)

const (
	autonomyPhasePaperSoak    = "paper-soak"
	autonomyPhaseLiveReadOnly = "live-readonly"
	autonomyPhaseLiveTrading  = "live-trading"
)

type autonomyReadinessOptions struct {
	Days                  int
	Symbol                string
	Phase                 string
	MinDecisive           int
	MinReviewedTrades     int
	MinWinRate            float64
	MinExpectancy         float64
	MaxSlippageBp         float64
	MaxRejectionRate      float64
	MaxMissingLinkRate    float64
	AllowLiveTradingCheck bool
}

func newAutonomyCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "autonomy",
		Short: "Autonomy readiness and paper soak workflows",
		Long:  "Run go/no-go checks for autonomous operation and summarize paper soak progress.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAutonomyReadiness(cmd, app)
		},
	}

	readinessCmd := &cobra.Command{
		Use:   "readiness",
		Short: "Show autonomous operation readiness",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAutonomyReadiness(cmd, app)
		},
	}
	addAutonomyReadinessFlags(readinessCmd)
	addAutonomyReadinessFlags(cmd)

	soakCmd := &cobra.Command{
		Use:   "soak",
		Short: "Paper soak testing workflow",
		Long:  "Plan and report autonomous paper-trading soak tests using readiness checks as guardrails.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPaperSoakReport(cmd, app)
		},
	}
	soakPlanCmd := &cobra.Command{
		Use:   "plan",
		Short: "Show paper soak preflight and run command",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPaperSoakPlan(cmd, app)
		},
	}
	soakStatusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show current paper soak status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPaperSoakReport(cmd, app)
		},
	}
	soakReportCmd := &cobra.Command{
		Use:   "report",
		Short: "Show paper soak results",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPaperSoakReport(cmd, app)
		},
	}
	addPaperSoakFlags(soakCmd)
	addPaperSoakFlags(soakPlanCmd)
	addPaperSoakFlags(soakStatusCmd)
	addPaperSoakFlags(soakReportCmd)
	soakCmd.AddCommand(soakPlanCmd, soakStatusCmd, soakReportCmd)

	cmd.AddCommand(readinessCmd, soakCmd)
	return cmd
}

func addAutonomyReadinessFlags(cmd *cobra.Command) {
	cmd.Flags().Int("days", 30, "Number of recent days to evaluate")
	cmd.Flags().String("symbol", "", "Filter by symbol")
	cmd.Flags().String("phase", autonomyPhasePaperSoak, "Readiness phase (paper-soak, live-readonly, live-trading)")
	cmd.Flags().Int("min-decisive", 20, "Minimum decisive paper predictions expected")
	cmd.Flags().Int("min-reviewed-trades", 5, "Minimum post-trade reviews expected")
	cmd.Flags().Float64("min-win-rate", 50, "Minimum paper prediction win rate")
	cmd.Flags().Float64("min-expectancy", 0, "Minimum expectancy per decisive prediction")
	cmd.Flags().Float64("max-slippage-bp", 50, "Maximum average adverse slippage in basis points")
	cmd.Flags().Float64("max-rejection-rate", 10, "Maximum execution rejection rate")
	cmd.Flags().Float64("max-missing-link-rate", 20, "Maximum post-trade missing-link rate")
	cmd.Flags().Bool("allow-live-trading-check", false, "Allow live-trading readiness to report READY when all gates pass")
}

func addPaperSoakFlags(cmd *cobra.Command) {
	cmd.Flags().Int("days", 20, "Number of recent days to summarize")
	cmd.Flags().String("symbols", "RELIANCE,INFY,TCS,HDFCBANK,ICICIBANK", "Comma-separated symbols for soak plan")
	cmd.Flags().String("symbol", "", "Filter reports by one symbol")
	cmd.Flags().String("watchlist", "", "Watchlist name for soak plan")
	cmd.Flags().String("window", "24h", "Candidate paper prediction evaluation window for soak plan")
	cmd.Flags().Float64("threshold", 65, "Prediction confidence threshold for soak plan")
	cmd.Flags().Int("interval", 60, "Analysis interval seconds for soak plan")
	cmd.Flags().Int("min-decisive", 20, "Minimum decisive paper predictions expected")
	cmd.Flags().Int("min-reviewed-trades", 5, "Minimum post-trade reviews expected")
	cmd.Flags().Float64("min-win-rate", 50, "Minimum paper prediction win rate")
	cmd.Flags().Float64("min-expectancy", 0, "Minimum expectancy per decisive prediction")
	cmd.Flags().Float64("max-slippage-bp", 50, "Maximum average adverse slippage in basis points")
	cmd.Flags().Float64("max-rejection-rate", 10, "Maximum execution rejection rate")
	cmd.Flags().Float64("max-missing-link-rate", 20, "Maximum post-trade missing-link rate")
}

func readinessOptionsFromFlags(cmd *cobra.Command) autonomyReadinessOptions {
	days, _ := cmd.Flags().GetInt("days")
	symbol, _ := cmd.Flags().GetString("symbol")
	phase, _ := cmd.Flags().GetString("phase")
	minDecisive, _ := cmd.Flags().GetInt("min-decisive")
	minReviewed, _ := cmd.Flags().GetInt("min-reviewed-trades")
	minWinRate, _ := cmd.Flags().GetFloat64("min-win-rate")
	minExpectancy, _ := cmd.Flags().GetFloat64("min-expectancy")
	maxSlippage, _ := cmd.Flags().GetFloat64("max-slippage-bp")
	maxRejection, _ := cmd.Flags().GetFloat64("max-rejection-rate")
	maxMissing, _ := cmd.Flags().GetFloat64("max-missing-link-rate")
	allowLive, _ := cmd.Flags().GetBool("allow-live-trading-check")
	return autonomyReadinessOptions{
		Days:                  days,
		Symbol:                strings.ToUpper(strings.TrimSpace(symbol)),
		Phase:                 normalizeAutonomyPhase(phase),
		MinDecisive:           minDecisive,
		MinReviewedTrades:     minReviewed,
		MinWinRate:            minWinRate,
		MinExpectancy:         minExpectancy,
		MaxSlippageBp:         maxSlippage,
		MaxRejectionRate:      maxRejection,
		MaxMissingLinkRate:    maxMissing,
		AllowLiveTradingCheck: allowLive,
	}
}

func paperSoakReadinessOptionsFromFlags(cmd *cobra.Command) autonomyReadinessOptions {
	opts := readinessOptionsFromFlags(cmd)
	opts.Phase = autonomyPhasePaperSoak
	opts.AllowLiveTradingCheck = false
	return opts
}

func normalizeAutonomyPhase(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "", "paper", "paper-soak", "soak":
		return autonomyPhasePaperSoak
	case "live-readonly", "readonly", "read-only":
		return autonomyPhaseLiveReadOnly
	case "live-trading", "live", "trading":
		return autonomyPhaseLiveTrading
	default:
		return autonomyPhasePaperSoak
	}
}

func runAutonomyReadiness(cmd *cobra.Command, app *App) error {
	output := NewOutput(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	report, err := buildAutonomyReadinessReport(ctx, app, readinessOptionsFromFlags(cmd))
	if err != nil {
		output.Error("Failed to build autonomy readiness report: %v", err)
		return err
	}
	if output.IsJSON() {
		return output.JSON(report)
	}
	printAutonomyReadiness(output, report)
	return nil
}

func buildAutonomyReadinessReport(ctx context.Context, app *App, opts autonomyReadinessOptions) (*models.AutonomyReadinessReport, error) {
	now := time.Now()
	report := &models.AutonomyReadinessReport{
		GeneratedAt: now,
		Phase:       opts.Phase,
		Symbol:      opts.Symbol,
		Decision:    models.AutonomyDecisionReady,
		Checks:      []models.AutonomyReadinessCheck{},
		Reasons:     []string{},
	}
	if opts.Days > 0 {
		report.StartDate = now.AddDate(0, 0, -opts.Days)
		report.EndDate = now
	}
	if opts.MinDecisive <= 0 {
		opts.MinDecisive = 1
	}
	if opts.MaxSlippageBp <= 0 {
		opts.MaxSlippageBp = 50
	}
	if opts.MaxRejectionRate <= 0 {
		opts.MaxRejectionRate = 10
	}
	if opts.MaxMissingLinkRate <= 0 {
		opts.MaxMissingLinkRate = 20
	}

	configReady := app != nil && app.Config != nil
	addReadinessCheck(report, "config", statusFromBool(configReady), "configuration loaded", nil)
	storeReady := app != nil && app.Store != nil
	addReadinessCheck(report, "store", statusFromBool(storeReady), "SQLite store initialized", nil)
	if app != nil && app.Config != nil {
		report.Summary.TradingMode = app.Config.Trading.Mode
		report.Summary.SafetyProfile = app.Config.SafetyProfile()
		report.Summary.AutonomousMode = app.Config.Agents.AutonomousMode
		addSafetyProfileCheck(report, opts, app.Config.Trading.Mode, app.Config.SafetyProfile())
		capabilities := app.Config.SafetyCapabilities()
		switch opts.Phase {
		case autonomyPhaseLiveReadOnly:
			if capabilities.BrokerOrders || capabilities.AutoTrade || capabilities.LLMOrderAuthority {
				addReadinessCheck(report, "live_readonly_boundary", models.AutonomyReadinessFail, "live-readonly requires broker orders, auto trading, and LLM order authority disabled", map[string]interface{}{"broker_orders": capabilities.BrokerOrders, "auto_trade": capabilities.AutoTrade, "llm_order_authority": capabilities.LLMOrderAuthority})
			} else {
				addReadinessCheck(report, "live_readonly_boundary", models.AutonomyReadinessPass, "live-readonly boundary blocks all order authority", map[string]interface{}{"broker_orders": capabilities.BrokerOrders, "auto_trade": capabilities.AutoTrade, "llm_order_authority": capabilities.LLMOrderAuthority})
			}
		case autonomyPhaseLiveTrading:
			if !capabilities.AutoTrade {
				addReadinessCheck(report, "auto_trade_capability", models.AutonomyReadinessFail, "safety profile does not permit autonomous live order placement", nil)
			} else if capabilities.LLMOrderAuthority {
				addReadinessCheck(report, "llm_order_boundary", models.AutonomyReadinessFail, "LLM direct order authority must stay disabled for live trading", nil)
			} else {
				addReadinessCheck(report, "auto_trade_capability", models.AutonomyReadinessPass, "safety profile capability is compatible with requested phase", map[string]interface{}{"auto_trade": capabilities.AutoTrade})
			}
		default:
			addReadinessCheck(report, "auto_trade_capability", models.AutonomyReadinessPass, "safety profile capability is compatible with requested phase", map[string]interface{}{"auto_trade": capabilities.AutoTrade})
		}
	}
	llmReady := app != nil && app.LLMClient != nil
	addReadinessCheck(report, "llm_client", statusForPhaseEvidence(opts.Phase, llmReady), "LLM client configured for explanation/review/paper prediction workflows", nil)
	brokerReady := app != nil && app.Broker != nil
	addReadinessCheck(report, "broker", statusForPhaseEvidence(opts.Phase, brokerReady), "broker or paper broker initialized", nil)

	if !storeReady {
		finalizeReadinessDecision(report)
		return report, nil
	}

	state, err := loadDaemonStateOrDefault(ctx, app)
	if err != nil {
		addReadinessCheck(report, "daemon_state", models.AutonomyReadinessFail, fmt.Sprintf("daemon state unavailable: %v", err), nil)
	} else {
		report.Summary.KillSwitchActive = state.KillSwitchActive
		report.Summary.DaemonStatus = string(state.Status)
		if state.KillSwitchActive {
			reason := state.KillSwitchReason
			if reason == "" {
				reason = "kill switch active"
			}
			addReadinessCheck(report, "kill_switch", models.AutonomyReadinessFail, reason, nil)
		} else {
			addReadinessCheck(report, "kill_switch", models.AutonomyReadinessPass, "kill switch clear", nil)
		}
		if state.StopRequested || state.Status == models.DaemonStatusStopRequested {
			addReadinessCheck(report, "daemon_state", models.AutonomyReadinessFail, "daemon stop requested", nil)
		} else if state.Paused || state.Status == models.DaemonStatusPaused {
			addReadinessCheck(report, "daemon_state", models.AutonomyReadinessWarn, "daemon is paused", map[string]interface{}{"status": state.Status})
		} else {
			addReadinessCheck(report, "daemon_state", models.AutonomyReadinessPass, "daemon control state permits execution", map[string]interface{}{"status": state.Status})
		}
	}

	filter := store.PaperPredictionFilter{
		Symbol:    opts.Symbol,
		StartDate: report.StartDate,
		EndDate:   report.EndDate,
	}
	predictions, err := app.Store.GetPaperPredictions(ctx, filter)
	if err != nil {
		addReadinessCheck(report, "paper_prediction_calibration", models.AutonomyReadinessFail, fmt.Sprintf("paper prediction report unavailable: %v", err), nil)
	} else {
		trustedPredictions, exploratoryPredictions := splitTrustedPaperCandidatePredictions(predictions)
		paperStats := calculatePaperCandidateOutcomeStats(trustedPredictions)
		report.Summary.PaperPredictions = paperStats.total
		report.Summary.PaperDecisive = paperStats.decisive
		report.Summary.PaperWinRate = paperStats.winRate
		report.Summary.PaperExpectancy = paperStats.expectancy
		addSamplePerformanceCheckWithDetails(report, opts, "paper_prediction_calibration", paperStats.decisive, paperStats.winRate, paperStats.expectancy, map[string]interface{}{
			"trusted_predictions":     paperStats.total,
			"exploratory_predictions": len(exploratoryPredictions),
		})
	}

	calibrationReport, err := app.Store.GetHistoricalCalibrationReport(ctx, filter)
	if err != nil {
		addReadinessCheck(report, "historical_calibration", models.AutonomyReadinessFail, fmt.Sprintf("historical calibration unavailable: %v", err), nil)
	} else {
		report.Summary.CalibrationDecisive = calibrationReport.Decisive
		report.Summary.CalibrationExpectancy = calibrationReport.Expectancy
		addSamplePerformanceCheck(report, opts, "historical_calibration", calibrationReport.Decisive, calibrationReport.WinRate, calibrationReport.Expectancy)
	}

	execReport, err := app.Store.GetExecutionQualityReport(ctx, models.ExecutionQualityFilter{
		Symbol:          opts.Symbol,
		StartDate:       report.StartDate,
		EndDate:         report.EndDate,
		SlippageAlertBp: opts.MaxSlippageBp,
		Limit:           20,
	})
	if err != nil {
		addReadinessCheck(report, "execution_quality", models.AutonomyReadinessFail, fmt.Sprintf("execution quality unavailable: %v", err), nil)
	} else {
		report.Summary.ExecutionOrders = execReport.TotalOrders
		report.Summary.ExecutionFillRate = execReport.FillRate
		report.Summary.ExecutionRejectionRate = execReport.RejectionRate
		report.Summary.ExecutionAvgSlippageBp = execReport.AvgSlippageBp
		addExecutionQualityCheck(report, opts, execReport)
	}

	paperOnly := true
	reviewReport, err := app.Store.GetPostTradeReviewReport(ctx, models.PostTradeReviewFilter{
		Symbol:    opts.Symbol,
		StartDate: report.StartDate,
		EndDate:   report.EndDate,
		IsPaper:   &paperOnly,
		Limit:     1000,
	})
	if err != nil {
		addReadinessCheck(report, "post_trade_review", models.AutonomyReadinessFail, fmt.Sprintf("post-trade review unavailable: %v", err), nil)
	} else {
		report.Summary.ReviewedTrades = reviewReport.ReviewedTrades
		report.Summary.PostTradeAvgPnLPercent = reviewReport.AvgPnLPercent
		if reviewReport.ReviewedTrades > 0 {
			report.Summary.MissingPredictionRate = float64(reviewReport.MissingPrediction) / float64(reviewReport.ReviewedTrades) * 100
			report.Summary.MissingExecutionRate = float64(reviewReport.MissingExecution) / float64(reviewReport.ReviewedTrades) * 100
		}
		addPostTradeReviewCheck(report, opts, reviewReport)
	}

	if opts.Phase == autonomyPhaseLiveTrading && !opts.AllowLiveTradingCheck {
		addReadinessCheck(report, "live_trading_confirmation", models.AutonomyReadinessFail, "live-trading readiness requires --allow-live-trading-check and proven paper results", nil)
	}

	finalizeReadinessDecision(report)
	return report, nil
}

func statusFromBool(ok bool) models.AutonomyReadinessStatus {
	if ok {
		return models.AutonomyReadinessPass
	}
	return models.AutonomyReadinessFail
}

func statusForPhaseEvidence(phase string, ok bool) models.AutonomyReadinessStatus {
	if ok {
		return models.AutonomyReadinessPass
	}
	if phase == autonomyPhasePaperSoak {
		return models.AutonomyReadinessWarn
	}
	return models.AutonomyReadinessFail
}

func addSafetyProfileCheck(report *models.AutonomyReadinessReport, opts autonomyReadinessOptions, mode, profile string) {
	status := models.AutonomyReadinessPass
	message := "safety profile matches requested phase"
	switch opts.Phase {
	case autonomyPhasePaperSoak:
		if profile != "paper" && profile != "backtest" {
			status = models.AutonomyReadinessWarn
			message = "paper soak should run under paper or backtest safety profile"
		}
	case autonomyPhaseLiveReadOnly:
		if profile != "live-readonly" {
			status = models.AutonomyReadinessFail
			message = "live-readonly phase requires live-readonly safety profile"
		}
	case autonomyPhaseLiveTrading:
		if profile != "live-trading" {
			status = models.AutonomyReadinessFail
			message = "live-trading phase requires live-trading safety profile"
		}
	}
	addReadinessCheck(report, "safety_profile", status, message, map[string]interface{}{"mode": mode, "profile": profile, "phase": opts.Phase})
}

func addSamplePerformanceCheck(report *models.AutonomyReadinessReport, opts autonomyReadinessOptions, name string, decisive int, winRate, expectancy float64) {
	addSamplePerformanceCheckWithDetails(report, opts, name, decisive, winRate, expectancy, nil)
}

func addSamplePerformanceCheckWithDetails(report *models.AutonomyReadinessReport, opts autonomyReadinessOptions, name string, decisive int, winRate, expectancy float64, details map[string]interface{}) {
	status := models.AutonomyReadinessPass
	message := "sample performance meets thresholds"
	if decisive < opts.MinDecisive {
		status = missingEvidenceStatus(opts.Phase)
		message = fmt.Sprintf("need at least %d decisive samples, got %d", opts.MinDecisive, decisive)
	} else if winRate < opts.MinWinRate {
		status = models.AutonomyReadinessFail
		message = fmt.Sprintf("win rate %.1f%% is below %.1f%%", winRate, opts.MinWinRate)
	} else if expectancy < opts.MinExpectancy {
		status = models.AutonomyReadinessFail
		message = fmt.Sprintf("expectancy %.2f%% is below %.2f%%", expectancy, opts.MinExpectancy)
	}
	if details == nil {
		details = make(map[string]interface{})
	}
	details["decisive"] = decisive
	details["win_rate"] = winRate
	details["expectancy"] = expectancy
	addReadinessCheck(report, name, status, message, details)
}

func missingEvidenceStatus(phase string) models.AutonomyReadinessStatus {
	if phase == autonomyPhasePaperSoak {
		return models.AutonomyReadinessWarn
	}
	return models.AutonomyReadinessFail
}

func addExecutionQualityCheck(report *models.AutonomyReadinessReport, opts autonomyReadinessOptions, execReport *models.ExecutionQualityReport) {
	status := models.AutonomyReadinessPass
	message := "execution quality meets thresholds"
	if execReport.TotalOrders == 0 {
		if opts.Phase == autonomyPhaseLiveReadOnly {
			status = models.AutonomyReadinessPass
			message = "execution samples are not required for live-readonly monitoring"
		} else {
			status = missingEvidenceStatus(opts.Phase)
			message = "no execution samples found"
		}
	} else if execReport.RejectionRate > opts.MaxRejectionRate {
		status = models.AutonomyReadinessFail
		message = fmt.Sprintf("rejection rate %.1f%% exceeds %.1f%%", execReport.RejectionRate, opts.MaxRejectionRate)
	} else if execReport.AvgSlippageBp > opts.MaxSlippageBp {
		status = models.AutonomyReadinessFail
		message = fmt.Sprintf("average slippage %.1f bp exceeds %.1f bp", execReport.AvgSlippageBp, opts.MaxSlippageBp)
	}
	addReadinessCheck(report, "execution_quality", status, message, map[string]interface{}{"orders": execReport.TotalOrders, "fill_rate": execReport.FillRate, "rejection_rate": execReport.RejectionRate, "avg_slippage_bp": execReport.AvgSlippageBp})
}

func addPostTradeReviewCheck(report *models.AutonomyReadinessReport, opts autonomyReadinessOptions, review *models.PostTradeReviewReport) {
	status := models.AutonomyReadinessPass
	message := "post-trade review links meet thresholds"
	missingPredictionRate := 0.0
	missingExecutionRate := 0.0
	if review.ReviewedTrades > 0 {
		missingPredictionRate = float64(review.MissingPrediction) / float64(review.ReviewedTrades) * 100
		missingExecutionRate = float64(review.MissingExecution) / float64(review.ReviewedTrades) * 100
	}
	if review.ReviewedTrades < opts.MinReviewedTrades {
		if opts.Phase == autonomyPhaseLiveReadOnly {
			status = models.AutonomyReadinessPass
			message = "post-trade reviews are not required for live-readonly monitoring"
		} else {
			status = missingEvidenceStatus(opts.Phase)
			message = fmt.Sprintf("need at least %d reviewed trades, got %d", opts.MinReviewedTrades, review.ReviewedTrades)
		}
	} else if missingPredictionRate > opts.MaxMissingLinkRate {
		status = models.AutonomyReadinessFail
		message = fmt.Sprintf("missing prediction links %.1f%% exceeds %.1f%%", missingPredictionRate, opts.MaxMissingLinkRate)
	} else if missingExecutionRate > opts.MaxMissingLinkRate {
		status = models.AutonomyReadinessFail
		message = fmt.Sprintf("missing execution links %.1f%% exceeds %.1f%%", missingExecutionRate, opts.MaxMissingLinkRate)
	}
	addReadinessCheck(report, "post_trade_review", status, message, map[string]interface{}{"reviewed_trades": review.ReviewedTrades, "avg_pnl_percent": review.AvgPnLPercent, "missing_prediction_rate": missingPredictionRate, "missing_execution_rate": missingExecutionRate})
}

func addReadinessCheck(report *models.AutonomyReadinessReport, name string, status models.AutonomyReadinessStatus, message string, details map[string]interface{}) {
	report.Checks = append(report.Checks, models.AutonomyReadinessCheck{
		Name:    name,
		Status:  status,
		Message: message,
		Details: details,
	})
	if status == models.AutonomyReadinessFail {
		report.Reasons = append(report.Reasons, name+": "+message)
	}
}

func finalizeReadinessDecision(report *models.AutonomyReadinessReport) {
	decision := models.AutonomyDecisionReady
	for _, check := range report.Checks {
		if check.Status == models.AutonomyReadinessFail {
			decision = models.AutonomyDecisionBlocked
			break
		}
		if check.Status == models.AutonomyReadinessWarn && decision != models.AutonomyDecisionBlocked {
			decision = models.AutonomyDecisionWarn
		}
	}
	report.Decision = decision
}

func printAutonomyReadiness(output *Output, report *models.AutonomyReadinessReport) {
	output.Bold("Autonomy Readiness: %s", report.Decision)
	output.Printf("  Phase: %s\n", report.Phase)
	if !report.StartDate.IsZero() {
		output.Printf("  Period: %s to %s\n", report.StartDate.Format("2006-01-02"), report.EndDate.Format("2006-01-02"))
	}
	if report.Symbol != "" {
		output.Printf("  Symbol: %s\n", report.Symbol)
	}
	output.Println()

	output.Bold("Summary")
	output.Printf("  Safety:      mode=%s profile=%s autonomous=%s\n", report.Summary.TradingMode, report.Summary.SafetyProfile, report.Summary.AutonomousMode)
	output.Printf("  Paper:       %d predictions | %d decisive | win %.1f%% | expectancy %.2f%%\n",
		report.Summary.PaperPredictions, report.Summary.PaperDecisive, report.Summary.PaperWinRate, report.Summary.PaperExpectancy)
	output.Printf("  Execution:   %d orders | fill %.1f%% | reject %.1f%% | slip %.1f bp\n",
		report.Summary.ExecutionOrders, report.Summary.ExecutionFillRate, report.Summary.ExecutionRejectionRate, report.Summary.ExecutionAvgSlippageBp)
	output.Printf("  Review:      %d trades | avg %s | missing pred %.1f%% | missing exec %.1f%%\n",
		report.Summary.ReviewedTrades, FormatPercent(report.Summary.PostTradeAvgPnLPercent), report.Summary.MissingPredictionRate, report.Summary.MissingExecutionRate)
	output.Println()

	output.Bold("Checks")
	table := NewTable(output, "Check", "Status", "Message")
	for _, check := range report.Checks {
		table.AddRow(check.Name, string(check.Status), check.Message)
	}
	table.Render()
	if len(report.Reasons) > 0 {
		output.Println()
		output.Bold("Blocking Reasons")
		for _, reason := range report.Reasons {
			output.Printf("  - %s\n", reason)
		}
	}
}

func runPaperSoakPlan(cmd *cobra.Command, app *App) error {
	output := NewOutput(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := buildPaperSoakReport(ctx, app, paperSoakReadinessOptionsFromFlags(cmd), paperSoakCommandFromFlags(cmd))
	if err != nil {
		return err
	}
	if output.IsJSON() {
		return output.JSON(report)
	}
	output.Bold("Paper Soak Plan")
	output.Printf("  Readiness: %s\n", report.Readiness.Decision)
	output.Printf("  Command:   %s\n", report.RecommendedCommand)
	output.Println()
	printAutonomyReadiness(output, report.Readiness)
	return nil
}

func runPaperSoakReport(cmd *cobra.Command, app *App) error {
	output := NewOutput(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := buildPaperSoakReport(ctx, app, paperSoakReadinessOptionsFromFlags(cmd), paperSoakCommandFromFlags(cmd))
	if err != nil {
		return err
	}
	if output.IsJSON() {
		return output.JSON(report)
	}
	output.Bold("Paper Soak Status")
	output.Printf("  Readiness:   %s\n", report.Readiness.Decision)
	output.Printf("  Sessions:    %d daemon events\n", report.Sessions)
	output.Printf("  Decisions:   %d daemon readiness failures | %d kill-switch events\n", report.ReadinessFailures, report.KillSwitchEvents)
	output.Printf("  Predictions: %d total | %d decisive | win %.1f%% | expectancy %.2f%% | avg P&L %.2f%%\n",
		report.Predictions, report.Decisive, report.WinRate, report.Expectancy, report.AvgPnLPercent)
	output.Printf("  Execution:   %d orders\n", report.ExecutionOrders)
	output.Printf("  Reviews:     %d trades\n", report.ReviewedTrades)
	output.Println()
	output.Bold("Run Command")
	output.Printf("  %s\n", report.RecommendedCommand)
	if report.Readiness.Decision == models.AutonomyDecisionBlocked {
		output.Println()
		output.Warning("Paper soak should not start until blocking readiness checks are fixed.")
	}
	return nil
}

func buildPaperSoakReport(ctx context.Context, app *App, opts autonomyReadinessOptions, command string) (*models.PaperSoakReport, error) {
	readiness, err := buildAutonomyReadinessReport(ctx, app, opts)
	if err != nil {
		return nil, err
	}
	report := &models.PaperSoakReport{
		GeneratedAt:        time.Now(),
		StartDate:          readiness.StartDate,
		EndDate:            readiness.EndDate,
		Symbol:             opts.Symbol,
		Readiness:          readiness,
		RecommendedCommand: command,
	}
	report.Predictions = readiness.Summary.PaperPredictions
	report.Decisive = readiness.Summary.PaperDecisive
	report.WinRate = readiness.Summary.PaperWinRate
	report.Expectancy = readiness.Summary.PaperExpectancy
	report.ExecutionOrders = readiness.Summary.ExecutionOrders
	report.ReviewedTrades = readiness.Summary.ReviewedTrades
	report.AvgPnLPercent = readiness.Summary.PostTradeAvgPnLPercent
	if app == nil || app.Store == nil {
		return report, nil
	}
	events, err := app.Store.GetDaemonEvents(ctx, 1000)
	if err == nil {
		for _, event := range events {
			if !report.StartDate.IsZero() && event.Timestamp.Before(report.StartDate) {
				continue
			}
			if !report.EndDate.IsZero() && event.Timestamp.After(report.EndDate) {
				continue
			}
			report.Sessions++
			if strings.Contains(strings.ToUpper(event.Type), "KILL_SWITCH") {
				report.KillSwitchEvents++
			}
			if strings.Contains(strings.ToUpper(event.Type), "READINESS") && event.Status == models.DaemonStatusPaused {
				report.ReadinessFailures++
			}
		}
	}
	return report, nil
}

func paperSoakCommandFromFlags(cmd *cobra.Command) string {
	symbols, _ := cmd.Flags().GetString("symbols")
	window, _ := cmd.Flags().GetString("window")
	minDecisive, _ := cmd.Flags().GetInt("min-decisive")
	minReviewed, _ := cmd.Flags().GetInt("min-reviewed-trades")
	minWinRate, _ := cmd.Flags().GetFloat64("min-win-rate")
	minExpectancy, _ := cmd.Flags().GetFloat64("min-expectancy")

	parts := []string{
		"trader", "paper", "soak-run",
		"--window", window,
		"--limit", "100",
		"--min-decisive", fmt.Sprintf("%d", minDecisive),
		"--min-reviewed-trades", fmt.Sprintf("%d", minReviewed),
		"--min-win-rate", fmt.Sprintf("%.0f", minWinRate),
		"--min-expectancy", fmt.Sprintf("%.2f", minExpectancy),
	}
	symbolList := parseAutonomySoakSymbols(symbols)
	if len(symbolList) == 1 {
		parts = append(parts, "--symbol", symbolList[0])
	}
	return strings.Join(parts, " ")
}

func parseAutonomySoakSymbols(symbols string) []string {
	result := make([]string, 0)
	for _, symbol := range strings.Split(symbols, ",") {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol != "" {
			result = append(result, symbol)
		}
	}
	return result
}
