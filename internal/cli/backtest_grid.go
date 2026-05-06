package cli

import (
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
	"zerodha-trader/internal/trading"
)

type backtestSetup struct {
	Name         string
	StopLoss     float64
	TakeProfit   float64
	TrailingStop float64
	AllowShort   bool
}

type backtestParamVariant struct {
	Name       string
	Parameter  map[string]interface{}
	ParameterS string
}

type backtestGridThresholds struct {
	MinTrades           int
	MinValidationTrades int
	MinProfitFactor     float64
	MinReturn           float64
	MinValidationReturn float64
	MaxDrawdown         float64
}

type backtestGridResult struct {
	Verdict              string                    `json:"verdict"`
	Reason               string                    `json:"reason,omitempty"`
	Symbol               string                    `json:"symbol"`
	Exchange             string                    `json:"exchange"`
	Strategy             string                    `json:"strategy"`
	Timeframe            string                    `json:"timeframe"`
	Days                 int                       `json:"days"`
	Setup                string                    `json:"setup"`
	ParamVariant         string                    `json:"param_variant"`
	Parameters           string                    `json:"parameters,omitempty"`
	Regime               string                    `json:"regime"`
	Candles              int                       `json:"candles"`
	SignalBars           int                       `json:"signal_bars"`
	BuySignals           int                       `json:"buy_signals"`
	SellSignals          int                       `json:"sell_signals"`
	HoldSignals          int                       `json:"hold_signals"`
	DirectionalSignals   int                       `json:"directional_signals"`
	SignalRatePct        float64                   `json:"signal_rate_pct"`
	TradeConversionPct   float64                   `json:"trade_conversion_pct"`
	LatestSignal         string                    `json:"latest_signal,omitempty"`
	SignalDiagnostic     string                    `json:"signal_diagnostic,omitempty"`
	Trades               int                       `json:"trades"`
	ValidationTrades     int                       `json:"validation_trades"`
	ReturnPct            float64                   `json:"return_pct"`
	AnnualizedReturnPct  float64                   `json:"annualized_return_pct"`
	ValidationReturnPct  float64                   `json:"validation_return_pct"`
	TrainReturnPct       float64                   `json:"train_return_pct"`
	WinRate              float64                   `json:"win_rate"`
	ProfitFactor         float64                   `json:"profit_factor"`
	Expectancy           float64                   `json:"expectancy"`
	MaxDrawdownPct       float64                   `json:"max_drawdown_pct"`
	SharpeRatio          float64                   `json:"sharpe_ratio"`
	Score                float64                   `json:"score"`
	CandidateScore       float64                   `json:"candidate_score,omitempty"`
	CandidateScoreReason string                    `json:"candidate_score_reason,omitempty"`
	ScoreComponents      candidateScoreComponents  `json:"score_components,omitempty"`
	EvidenceScore        float64                   `json:"evidence_score,omitempty"`
	EvidenceSentiment    string                    `json:"evidence_sentiment,omitempty"`
	EvidenceConfidence   float64                   `json:"evidence_confidence,omitempty"`
	EvidenceSources      int                       `json:"evidence_sources,omitempty"`
	EvidenceError        string                    `json:"evidence_error,omitempty"`
	Error                string                    `json:"error,omitempty"`
	StopLossPercent      float64                   `json:"stop_loss_percent,omitempty"`
	TakeProfitPercent    float64                   `json:"take_profit_percent,omitempty"`
	TrailingStopPercent  float64                   `json:"trailing_stop_percent,omitempty"`
	AllowShort           bool                      `json:"allow_short"`
	ExecutionTiming      string                    `json:"execution_timing"`
	SlippagePercent      float64                   `json:"slippage_percent"`
	CommissionPercent    float64                   `json:"commission_percent"`
	PartialFillsEnabled  bool                      `json:"partial_fills_enabled"`
	MaxFillVolumePercent float64                   `json:"max_fill_volume_percent,omitempty"`
	Regimes              []backtestRegimeTradeStat `json:"regimes,omitempty"`
}

type backtestRegimeTradeStat struct {
	Regime      string  `json:"regime"`
	Trades      int     `json:"trades"`
	Wins        int     `json:"wins"`
	WinRate     float64 `json:"win_rate"`
	TotalPnL    float64 `json:"total_pnl"`
	Expectancy  float64 `json:"expectancy"`
	AvgHoldBars float64 `json:"avg_hold_bars"`
}

func newBacktestGridCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "grid",
		Short: "Run a backtest grid with reliability checks",
		Long: `Run multiple strategy, setup, symbol, timeframe, and period combinations.

Each row includes full-period metrics plus a simple walk-forward check using the
first 70% of candles as train history and the last 30% as validation history.
Verdicts are meant to reject weak candidates before paper soak testing.`,
		Example: `  trader backtest grid --symbols RELIANCE,INFY,TCS --strategies supertrend,multi_indicator --periods 365,1095
  trader backtest grid --watchlist nifty50 --strategies supertrend --timeframes 15min --periods 30 --setups sl2tp4,short_sl2tp4
  trader backtest grid --symbols RELIANCE,HDFCBANK --output reports/backtest-grid.csv`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			symbolsFlag, _ := cmd.Flags().GetString("symbols")
			watchlistName, _ := cmd.Flags().GetString("watchlist")
			strategiesFlag, _ := cmd.Flags().GetString("strategies")
			timeframesFlag, _ := cmd.Flags().GetString("timeframes")
			periodsFlag, _ := cmd.Flags().GetString("periods")
			setupsFlag, _ := cmd.Flags().GetString("setups")
			paramGrid, _ := cmd.Flags().GetString("param-grid")
			exchange, _ := cmd.Flags().GetString("exchange")
			capital, _ := cmd.Flags().GetFloat64("capital")
			slippagePct, _ := cmd.Flags().GetFloat64("slippage")
			commissionPct, _ := cmd.Flags().GetFloat64("commission")
			executionTiming, _ := cmd.Flags().GetString("execution")
			partialFills, _ := cmd.Flags().GetBool("partial-fills")
			maxFillVolumePct, _ := cmd.Flags().GetFloat64("max-fill-volume")
			limit, _ := cmd.Flags().GetInt("limit")
			outputPath, _ := cmd.Flags().GetString("output")
			regimeReport, _ := cmd.Flags().GetBool("regime-report")
			regimeWindow, _ := cmd.Flags().GetInt("regime-window")
			promote, _ := cmd.Flags().GetBool("promote-paper-candidates")
			promoteVerdictsFlag, _ := cmd.Flags().GetString("promote-verdicts")
			candidateStatus, _ := cmd.Flags().GetString("candidate-status")
			minRegimeTrades, _ := cmd.Flags().GetInt("min-regime-trades")
			minCandidateScore, _ := cmd.Flags().GetFloat64("min-candidate-score")

			thresholds := backtestGridThresholds{}
			thresholds.MinTrades, _ = cmd.Flags().GetInt("min-trades")
			thresholds.MinValidationTrades, _ = cmd.Flags().GetInt("min-validation-trades")
			thresholds.MinProfitFactor, _ = cmd.Flags().GetFloat64("min-profit-factor")
			thresholds.MinReturn, _ = cmd.Flags().GetFloat64("min-return")
			thresholds.MinValidationReturn, _ = cmd.Flags().GetFloat64("min-validation-return")
			thresholds.MaxDrawdown, _ = cmd.Flags().GetFloat64("max-drawdown")

			symbols := parseCSVFlag(symbolsFlag)
			for i := range symbols {
				symbols[i] = strings.ToUpper(symbols[i])
			}
			if len(symbols) == 0 && watchlistName != "" {
				symbols = getPredefinedWatchlist(watchlistName, app, ctx)
			}
			if len(symbols) == 0 {
				return fmt.Errorf("provide --symbols or --watchlist")
			}

			strategies, err := parseGridStrategies(strategiesFlag)
			if err != nil {
				return err
			}
			timeframes := parseCSVFlag(timeframesFlag)
			if len(timeframes) == 0 {
				timeframes = []string{"1day"}
			}
			periods, err := parsePositiveInts(periodsFlag)
			if err != nil {
				return err
			}
			setups, err := parseBacktestSetups(setupsFlag)
			if err != nil {
				return err
			}
			paramGrid = strings.ToLower(strings.TrimSpace(paramGrid))
			if paramGrid == "" {
				paramGrid = "default"
			}
			if paramGrid != "default" && paramGrid != "research" {
				return fmt.Errorf("unknown param grid %q", paramGrid)
			}

			if app.Broker == nil {
				return fmt.Errorf("broker not configured")
			}

			if !output.IsJSON() {
				output.Bold("Backtest Grid")
				output.Printf("  Symbols:    %s\n", strings.Join(symbols, ", "))
				output.Printf("  Strategies: %s\n", strings.Join(strategies, ", "))
				output.Printf("  Timeframes: %s\n", strings.Join(timeframes, ", "))
				output.Printf("  Periods:    %s days\n", strings.Join(intsToStrings(periods), ", "))
				output.Printf("  Setups:     %s\n", strings.Join(setupNames(setups), ", "))
				output.Printf("  Param grid: %s\n", paramGrid)
				output.Println()
			}

			results := runBacktestGrid(ctx, app, backtestGridRunConfig{
				Symbols:               symbols,
				Strategies:            strategies,
				Timeframes:            timeframes,
				Periods:               periods,
				Setups:                setups,
				Exchange:              exchange,
				Capital:               capital,
				SlippagePct:           slippagePct,
				CommissionPct:         commissionPct,
				ExecutionTiming:       executionTiming,
				PartialFills:          partialFills,
				MaxFillVolumePct:      maxFillVolumePct,
				ParamGrid:             paramGrid,
				RegimeWindow:          regimeWindow,
				ReliabilityThresholds: thresholds,
			})
			applyEvidenceAwareCandidateScores(results, nil, nil)
			sortBacktestGridResults(results)

			if outputPath != "" {
				if err := writeBacktestGridCSV(outputPath, results); err != nil {
					return err
				}
				if !output.IsJSON() {
					output.Success("Wrote CSV: %s", outputPath)
					output.Println()
				}
			}
			var promoted []models.PaperCandidate
			if promote {
				if app.Store == nil {
					return fmt.Errorf("paper candidate store is not available")
				}
				promoteVerdicts := parseVerdictSet(promoteVerdictsFlag)
				promoted, err = promoteBacktestGridCandidates(ctx, app.Store, results, promoteVerdicts, strings.ToUpper(strings.TrimSpace(candidateStatus)), minRegimeTrades, minCandidateScore)
				if err != nil {
					return err
				}
				if !output.IsJSON() {
					output.Success("Promoted %d paper candidate(s)", len(promoted))
					output.Println()
				}
			}

			if output.IsJSON() {
				if promote {
					return output.JSON(map[string]interface{}{"results": results, "promoted": promoted})
				}
				return output.JSON(results)
			}
			displayBacktestGridResults(output, results, limit)
			if regimeReport {
				output.Println()
				displayBacktestRegimeSummary(output, results, limit)
			}
			return nil
		},
	}

	cmd.Flags().String("symbols", "", "Comma-separated symbols")
	cmd.Flags().String("watchlist", "", "Watchlist name (nifty50, banknifty, it, auto, pharma, fmcg, or custom)")
	cmd.Flags().String("strategies", "intraday_momentum,multi_indicator,donchian_breakout,supertrend", "Comma-separated strategies or 'all'")
	cmd.Flags().String("timeframes", "1day", "Comma-separated candle timeframes")
	cmd.Flags().String("periods", "365", "Comma-separated lookback periods in days")
	cmd.Flags().String("setups", "base,sl2tp4,short_sl2tp4", "Comma-separated setups (base, sl1tp2, sl2tp4, sl25tp5, trail2, short_base, short_sl1tp2, short_sl2tp4)")
	cmd.Flags().String("param-grid", "default", "Strategy parameter grid (default, research)")
	cmd.Flags().StringP("exchange", "e", "NSE", "Exchange (NSE, BSE)")
	cmd.Flags().Float64("capital", 1000000, "Starting capital")
	cmd.Flags().Float64("slippage", 0.1, "Slippage percentage")
	cmd.Flags().Float64("commission", 0.03, "Commission percentage")
	cmd.Flags().String("execution", "next_open", "Execution timing (next_open, same_close)")
	cmd.Flags().Bool("partial-fills", false, "Cap fills by candle volume")
	cmd.Flags().Float64("max-fill-volume", 10, "Maximum fill as percentage of candle volume when partial fills are enabled")
	cmd.Flags().Int("min-trades", 30, "Minimum full-period trades for PASS")
	cmd.Flags().Int("min-validation-trades", 5, "Minimum validation trades for PASS")
	cmd.Flags().Float64("min-profit-factor", 1.2, "Minimum full-period profit factor for PASS")
	cmd.Flags().Float64("min-return", 0, "Minimum full-period return percentage for PASS")
	cmd.Flags().Float64("min-validation-return", 0, "Minimum validation return percentage for PASS")
	cmd.Flags().Float64("max-drawdown", 10, "Maximum full-period drawdown percentage for PASS")
	cmd.Flags().Int("limit", 25, "Rows to print in table output (0 for all)")
	cmd.Flags().String("output", "", "Optional CSV output path")
	cmd.Flags().Bool("regime-report", false, "Print trade expectancy grouped by entry regime")
	cmd.Flags().Int("regime-window", 50, "Candles used to classify each trade entry regime")
	cmd.Flags().Bool("promote-paper-candidates", false, "Save eligible grid rows as controlled paper-soak candidates")
	cmd.Flags().String("promote-verdicts", "PASS", "Comma-separated verdicts eligible for promotion")
	cmd.Flags().String("candidate-status", models.PaperCandidateStatusActive, "Status for promoted candidates (ACTIVE, PAUSED)")
	cmd.Flags().Int("min-regime-trades", 2, "Minimum trades for a regime to become an allowed/blocked guardrail")
	cmd.Flags().Float64("min-candidate-score", 0, "Minimum composite candidate score for promotion; 0 disables")
	return cmd
}

type backtestGridRunConfig struct {
	Symbols               []string
	Strategies            []string
	Timeframes            []string
	Periods               []int
	Setups                []backtestSetup
	Exchange              string
	Capital               float64
	SlippagePct           float64
	CommissionPct         float64
	ExecutionTiming       string
	PartialFills          bool
	MaxFillVolumePct      float64
	ParamGrid             string
	RegimeWindow          int
	ReliabilityThresholds backtestGridThresholds
}

func runBacktestGrid(ctx context.Context, app *App, cfg backtestGridRunConfig) []backtestGridResult {
	var results []backtestGridResult
	engine := trading.NewBacktestEngine(nil)

	for _, symbol := range cfg.Symbols {
		for _, timeframe := range cfg.Timeframes {
			for _, days := range cfg.Periods {
				candles, _, err := app.getQualityHistorical(ctx, broker.HistoricalRequest{
					Symbol:    symbol,
					Exchange:  models.Exchange(cfg.Exchange),
					Timeframe: timeframe,
					From:      time.Now().AddDate(0, 0, -days),
					To:        time.Now(),
				}, 50, false)

				if err != nil {
					results = append(results, backtestGridResult{
						Verdict:   "REJECT",
						Reason:    "data_unavailable",
						Symbol:    symbol,
						Exchange:  cfg.Exchange,
						Timeframe: timeframe,
						Days:      days,
						Error:     err.Error(),
					})
					continue
				}

				regime := classifyBacktestRegime(candles)
				for _, strategy := range cfg.Strategies {
					paramVariants := strategyParameterVariants(strategy, cfg.ParamGrid)
					for _, variant := range paramVariants {
						for _, setup := range cfg.Setups {
							btConfig := trading.BacktestConfig{
								Symbol:               symbol,
								Timeframe:            timeframe,
								InitialCapital:       cfg.Capital,
								Strategy:             strategy,
								Parameters:           cloneParams(variant.Parameter),
								Slippage:             cfg.SlippagePct / 100,
								Commission:           cfg.CommissionPct / 100,
								ExecutionTiming:      cfg.ExecutionTiming,
								AllowPartialFills:    cfg.PartialFills,
								MaxFillVolumePercent: cfg.MaxFillVolumePct,
								StopLossPercent:      setup.StopLoss,
								TakeProfitPercent:    setup.TakeProfit,
								TrailingStopPercent:  setup.TrailingStop,
								AllowShort:           setup.AllowShort,
							}
							result := evaluateBacktestGridCandidate(ctx, engine, btConfig, candles, cfg, setup, variant, regime, days)
							results = append(results, result)
						}
					}
				}
			}
		}
	}
	return results
}

func evaluateBacktestGridCandidate(ctx context.Context, engine *trading.DefaultBacktestEngine, btConfig trading.BacktestConfig, candles []models.Candle, runCfg backtestGridRunConfig, setup backtestSetup, variant backtestParamVariant, regime string, days int) backtestGridResult {
	full, err := engine.RunOnCandles(ctx, btConfig, candles)
	base := backtestGridResult{
		Symbol:               btConfig.Symbol,
		Exchange:             runCfg.Exchange,
		Strategy:             btConfig.Strategy,
		Timeframe:            btConfig.Timeframe,
		Days:                 days,
		Setup:                setup.Name,
		ParamVariant:         variant.Name,
		Parameters:           variant.ParameterS,
		Regime:               regime,
		Candles:              len(candles),
		StopLossPercent:      setup.StopLoss,
		TakeProfitPercent:    setup.TakeProfit,
		TrailingStopPercent:  setup.TrailingStop,
		AllowShort:           setup.AllowShort,
		ExecutionTiming:      runCfg.ExecutionTiming,
		SlippagePercent:      runCfg.SlippagePct,
		CommissionPercent:    runCfg.CommissionPct,
		PartialFillsEnabled:  runCfg.PartialFills,
		MaxFillVolumePercent: runCfg.MaxFillVolumePct,
	}
	if err != nil {
		base.Verdict = "REJECT"
		base.Reason = "backtest_failed"
		base.Error = err.Error()
		return base
	}
	if diagnostic, err := engine.LatestSignalDiagnostic(btConfig, candles); err == nil {
		base.LatestSignal = diagnostic.Signal
		base.SignalDiagnostic = diagnostic.Reason
	}

	base.Trades = full.TotalTrades
	base.SignalBars = full.SignalActivity.EvaluatedBars
	base.BuySignals = full.SignalActivity.BuySignals
	base.SellSignals = full.SignalActivity.SellSignals
	base.HoldSignals = full.SignalActivity.HoldSignals
	base.DirectionalSignals = full.SignalActivity.DirectionalSignals
	base.SignalRatePct = full.SignalActivity.SignalRatePct
	base.TradeConversionPct = full.SignalActivity.TradeConversionPct
	base.ReturnPct = full.TotalReturn
	base.AnnualizedReturnPct = full.AnnualizedReturn
	base.WinRate = full.WinRate
	base.ProfitFactor = finiteMetric(full.ProfitFactor)
	base.Expectancy = full.Expectancy
	base.MaxDrawdownPct = full.MaxDrawdown
	base.SharpeRatio = finiteMetric(full.SharpeRatio)
	base.Regimes = classifyTradeRegimeStats(candles, full.Trades, runCfg.RegimeWindow)

	train, validation, splitOK := runWalkForward(ctx, engine, btConfig, candles)
	if splitOK {
		base.TrainReturnPct = train.TotalReturn
		base.ValidationReturnPct = validation.TotalReturn
		base.ValidationTrades = validation.TotalTrades
	} else {
		base.Reason = "validation_insufficient_data"
	}

	base.Verdict, base.Reason = reliabilityVerdict(base, runCfg.ReliabilityThresholds)
	base.Score = backtestReliabilityScore(base, runCfg.ReliabilityThresholds)
	return base
}

func runWalkForward(ctx context.Context, engine *trading.DefaultBacktestEngine, btConfig trading.BacktestConfig, candles []models.Candle) (*trading.BacktestResult, *trading.BacktestResult, bool) {
	split := int(math.Floor(float64(len(candles)) * 0.70))
	if split < 50 || len(candles)-split < 50 {
		return nil, nil, false
	}
	train, err := engine.RunOnCandles(ctx, btConfig, candles[:split])
	if err != nil {
		return nil, nil, false
	}
	validation, err := engine.RunOnCandles(ctx, btConfig, candles[split:])
	if err != nil {
		return nil, nil, false
	}
	return train, validation, true
}

func reliabilityVerdict(r backtestGridResult, t backtestGridThresholds) (string, string) {
	switch {
	case r.Trades == 0:
		return "REJECT", "no_trades"
	case r.Expectancy <= 0:
		return "REJECT", "negative_expectancy"
	case r.ProfitFactor < 1:
		return "REJECT", "profit_factor_below_1"
	case r.ReturnPct < t.MinReturn:
		return "REJECT", "return_below_min"
	case r.TrainReturnPct < t.MinReturn:
		return "REJECT", "train_return_below_min"
	case r.ValidationReturnPct < t.MinValidationReturn:
		return "REJECT", "validation_return_below_min"
	case r.MaxDrawdownPct > t.MaxDrawdown*1.5:
		return "REJECT", "drawdown_too_high"
	}

	var watch []string
	if r.Trades < t.MinTrades {
		watch = append(watch, "low_trade_count")
	}
	if r.ValidationTrades < t.MinValidationTrades {
		watch = append(watch, "low_validation_trades")
	}
	if r.ProfitFactor < t.MinProfitFactor {
		watch = append(watch, "profit_factor_watch")
	}
	if r.MaxDrawdownPct > t.MaxDrawdown {
		watch = append(watch, "drawdown_watch")
	}
	if len(watch) > 0 {
		return "WATCH", strings.Join(watch, ",")
	}
	return "PASS", ""
}

func backtestReliabilityScore(r backtestGridResult, t backtestGridThresholds) float64 {
	score := r.ValidationReturnPct*2 + r.TrainReturnPct + r.ReturnPct + math.Min(r.ProfitFactor, 5)*5 - r.MaxDrawdownPct
	if r.Trades >= t.MinTrades {
		score += 10
	} else {
		score += float64(r.Trades) / math.Max(float64(t.MinTrades), 1) * 10
	}
	if r.ValidationTrades >= t.MinValidationTrades {
		score += 10
	} else {
		score += float64(r.ValidationTrades) / math.Max(float64(t.MinValidationTrades), 1) * 10
	}
	switch r.Verdict {
	case "PASS":
		score += 1000
	case "WATCH":
		score += 100
	}
	return score
}

func classifyBacktestRegime(candles []models.Candle) string {
	if len(candles) < 2 || candles[0].Close <= 0 {
		return "unknown"
	}
	first := candles[0].Close
	last := candles[len(candles)-1].Close
	totalReturn := (last - first) / first * 100

	var absReturns []float64
	var trueRanges []float64
	for i := 1; i < len(candles); i++ {
		prev := candles[i-1].Close
		if prev > 0 {
			absReturns = append(absReturns, math.Abs((candles[i].Close-prev)/prev*100))
		}
		tr := math.Max(candles[i].High-candles[i].Low, math.Max(math.Abs(candles[i].High-prev), math.Abs(candles[i].Low-prev)))
		if candles[i].Close > 0 {
			trueRanges = append(trueRanges, tr/candles[i].Close*100)
		}
	}

	avgMove := average(absReturns)
	avgATR := average(trueRanges)
	volTag := ""
	if avgMove >= 1.5 || avgATR >= 3 {
		volTag = "_high_vol"
	}

	switch {
	case totalReturn >= 8:
		return "trend_up" + volTag
	case totalReturn <= -8:
		return "trend_down" + volTag
	default:
		return "range" + volTag
	}
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func parseGridStrategies(flag string) ([]string, error) {
	if strings.EqualFold(strings.TrimSpace(flag), "all") {
		return trading.AvailableStrategies(), nil
	}
	strategies := parseCSVFlag(flag)
	for i, strategy := range strategies {
		strategy = trading.NormalizeStrategyName(strategy)
		if _, ok := trading.DefaultStrategyRegistry().Definition(strategy); !ok {
			return nil, fmt.Errorf("unknown strategy %q", strategy)
		}
		strategies[i] = strategy
	}
	return strategies, nil
}

func parseBacktestSetups(flag string) ([]backtestSetup, error) {
	names := parseCSVFlag(flag)
	if len(names) == 0 {
		names = []string{"base"}
	}
	setups := make([]backtestSetup, 0, len(names))
	for _, name := range names {
		switch strings.ToLower(name) {
		case "base":
			setups = append(setups, backtestSetup{Name: "base", StopLoss: 3})
		case "sl1tp2":
			setups = append(setups, backtestSetup{Name: "sl1tp2", StopLoss: 1, TakeProfit: 2})
		case "sl2tp4":
			setups = append(setups, backtestSetup{Name: "sl2tp4", StopLoss: 2, TakeProfit: 4})
		case "sl25tp5":
			setups = append(setups, backtestSetup{Name: "sl25tp5", StopLoss: 2.5, TakeProfit: 5})
		case "trail2":
			setups = append(setups, backtestSetup{Name: "trail2", StopLoss: 3, TrailingStop: 2})
		case "short_base":
			setups = append(setups, backtestSetup{Name: "short_base", StopLoss: 3, AllowShort: true})
		case "short_sl1tp2":
			setups = append(setups, backtestSetup{Name: "short_sl1tp2", StopLoss: 1, TakeProfit: 2, AllowShort: true})
		case "short_sl2tp4":
			setups = append(setups, backtestSetup{Name: "short_sl2tp4", StopLoss: 2, TakeProfit: 4, AllowShort: true})
		default:
			return nil, fmt.Errorf("unknown setup %q", name)
		}
	}
	return setups, nil
}

func strategyParameterVariants(strategy, grid string) []backtestParamVariant {
	strategy = trading.NormalizeStrategyName(strategy)
	if strings.EqualFold(strings.TrimSpace(grid), "default") || strings.TrimSpace(grid) == "" {
		return []backtestParamVariant{newParamVariant("default", nil)}
	}

	switch strategy {
	case "intraday_momentum":
		return []backtestParamVariant{
			newParamVariant("micro_breakout", map[string]interface{}{"mode": "breakout", "lookback_period": 3, "ema_period": 8, "volume_period": 12, "volume_multiplier": 0.9, "min_move_pct": 0.03, "min_range_pct": 0.05}),
			newParamVariant("fast_breakout", map[string]interface{}{"mode": "breakout", "lookback_period": 5, "ema_period": 9, "volume_period": 16, "volume_multiplier": 1.0, "min_move_pct": 0.05, "min_range_pct": 0.08}),
			newParamVariant("hybrid_active", map[string]interface{}{"mode": "hybrid", "lookback_period": 5, "ema_period": 9, "volume_period": 16, "volume_multiplier": 1.0, "min_move_pct": 0.05, "min_range_pct": 0.08}),
			newParamVariant("continuation_active", map[string]interface{}{"mode": "continuation", "lookback_period": 3, "ema_period": 8, "volume_period": 12, "volume_multiplier": 0.9, "min_move_pct": 0.04, "min_range_pct": 0.06}),
			newParamVariant("confirmed_breakout", map[string]interface{}{"mode": "breakout", "lookback_period": 8, "ema_period": 13, "volume_period": 20, "volume_multiplier": 1.2, "min_move_pct": 0.08, "min_range_pct": 0.12, "require_volume": true}),
		}
	case "supertrend":
		return []backtestParamVariant{
			newParamVariant("atr7_mult2", map[string]interface{}{"atr_period": 7, "multiplier": 2.0}),
			newParamVariant("atr10_mult2", map[string]interface{}{"atr_period": 10, "multiplier": 2.0}),
			newParamVariant("atr10_mult3", map[string]interface{}{"atr_period": 10, "multiplier": 3.0}),
			newParamVariant("atr14_mult3", map[string]interface{}{"atr_period": 14, "multiplier": 3.0}),
			newParamVariant("atr14_mult4", map[string]interface{}{"atr_period": 14, "multiplier": 4.0}),
		}
	case "donchian_breakout":
		return []backtestParamVariant{
			newParamVariant("donchian10", map[string]interface{}{"period": 10}),
			newParamVariant("donchian20", map[string]interface{}{"period": 20}),
			newParamVariant("donchian30", map[string]interface{}{"period": 30}),
			newParamVariant("donchian55", map[string]interface{}{"period": 55}),
		}
	case "multi_indicator":
		return []backtestParamVariant{
			newParamVariant("balanced", map[string]interface{}{"short_period": 9, "long_period": 21, "rsi_min": 30.0, "rsi_max": 70.0, "adx_threshold": 20.0, "volume_multiplier": 1.2}),
			newParamVariant("fast", map[string]interface{}{"short_period": 5, "long_period": 13, "rsi_min": 28.0, "rsi_max": 72.0, "adx_threshold": 18.0, "volume_multiplier": 1.1}),
			newParamVariant("trend_strict", map[string]interface{}{"short_period": 9, "long_period": 21, "rsi_min": 35.0, "rsi_max": 65.0, "adx_threshold": 25.0, "volume_multiplier": 1.1, "require_adx": true}),
			newParamVariant("volume_strict", map[string]interface{}{"short_period": 9, "long_period": 21, "rsi_min": 30.0, "rsi_max": 70.0, "adx_threshold": 18.0, "volume_multiplier": 1.5, "require_volume": true}),
			newParamVariant("slow_quality", map[string]interface{}{"short_period": 13, "long_period": 34, "rsi_min": 35.0, "rsi_max": 65.0, "adx_threshold": 22.0, "volume_multiplier": 1.2}),
		}
	default:
		return []backtestParamVariant{newParamVariant("default", nil)}
	}
}

func newParamVariant(name string, params map[string]interface{}) backtestParamVariant {
	return backtestParamVariant{
		Name:       name,
		Parameter:  cloneParams(params),
		ParameterS: formatParameters(params),
	}
}

func cloneParams(params map[string]interface{}) map[string]interface{} {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(params))
	for key, value := range params {
		out[key] = value
	}
	return out
}

func formatParameters(params map[string]interface{}) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, params[key]))
	}
	return strings.Join(parts, ";")
}

func parseCSVFlag(flag string) []string {
	parts := strings.Split(flag, ",")
	values := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		key := strings.ToUpper(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		values = append(values, value)
	}
	return values
}

func parsePositiveInts(flag string) ([]int, error) {
	parts := parseCSVFlag(flag)
	if len(parts) == 0 {
		return []int{365}, nil
	}
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value <= 0 {
			return nil, fmt.Errorf("invalid positive integer %q", part)
		}
		values = append(values, value)
	}
	return values, nil
}

func displayBacktestGridResults(output *Output, results []backtestGridResult, limit int) {
	if limit <= 0 || limit > len(results) {
		limit = len(results)
	}
	table := NewTable(output, "Verdict", "Symbol", "Strategy", "Variant", "TF", "Days", "Setup", "CScore", "Ev", "Sig%", "B/S", "Latest", "Diag", "Ret", "Val", "Tr", "VTr", "PF", "DD", "Regime", "Reason")
	for i := 0; i < limit; i++ {
		r := results[i]
		verdict := r.Verdict
		switch r.Verdict {
		case "PASS":
			verdict = output.Green(r.Verdict)
		case "WATCH":
			verdict = output.Yellow(r.Verdict)
		default:
			verdict = output.Red(r.Verdict)
		}
		table.AddRow(
			verdict,
			r.Symbol,
			r.Strategy,
			r.ParamVariant,
			r.Timeframe,
			strconv.Itoa(r.Days),
			r.Setup,
			formatScoreOrDash(r.CandidateScore),
			formatScoreOrDash(r.EvidenceScore),
			fmt.Sprintf("%.2f%%", r.SignalRatePct),
			fmt.Sprintf("%d/%d", r.BuySignals, r.SellSignals),
			r.LatestSignal,
			r.SignalDiagnostic,
			fmt.Sprintf("%.2f%%", r.ReturnPct),
			fmt.Sprintf("%.2f%%", r.ValidationReturnPct),
			strconv.Itoa(r.Trades),
			strconv.Itoa(r.ValidationTrades),
			fmt.Sprintf("%.2f", r.ProfitFactor),
			fmt.Sprintf("%.1f%%", r.MaxDrawdownPct),
			r.Regime,
			r.Reason,
		)
	}
	table.Render()
	if limit < len(results) {
		output.Println()
		output.Dim("Showing %d of %d rows. Use --limit 0 to print all rows.", limit, len(results))
	}
}

func sortBacktestGridResults(results []backtestGridResult) {
	rank := map[string]int{"PASS": 0, "WATCH": 1, "REJECT": 2}
	sort.SliceStable(results, func(i, j int) bool {
		ri, rj := rank[results[i].Verdict], rank[results[j].Verdict]
		if ri != rj {
			return ri < rj
		}
		if results[i].CandidateScore != results[j].CandidateScore {
			return results[i].CandidateScore > results[j].CandidateScore
		}
		return results[i].Score > results[j].Score
	})
}

func classifyTradeRegimeStats(candles []models.Candle, trades []trading.BacktestTrade, window int) []backtestRegimeTradeStat {
	if window <= 1 {
		window = 50
	}
	stats := make(map[string]*backtestRegimeTradeStat)
	for _, trade := range trades {
		index := candleIndexAtOrAfter(candles, trade.EntryTime)
		if index < 0 {
			continue
		}
		start := index - window + 1
		if start < 0 {
			start = 0
		}
		regime := classifyBacktestRegime(candles[start : index+1])
		stat := stats[regime]
		if stat == nil {
			stat = &backtestRegimeTradeStat{Regime: regime}
			stats[regime] = stat
		}
		stat.Trades++
		if trade.PnL > 0 {
			stat.Wins++
		}
		stat.TotalPnL += trade.PnL
		stat.AvgHoldBars += float64(trade.HoldBars)
	}

	out := make([]backtestRegimeTradeStat, 0, len(stats))
	for _, stat := range stats {
		if stat.Trades > 0 {
			stat.WinRate = float64(stat.Wins) / float64(stat.Trades) * 100
			stat.Expectancy = stat.TotalPnL / float64(stat.Trades)
			stat.AvgHoldBars = stat.AvgHoldBars / float64(stat.Trades)
		}
		out = append(out, *stat)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Regime != out[j].Regime {
			return out[i].Regime < out[j].Regime
		}
		return out[i].Expectancy > out[j].Expectancy
	})
	return out
}

func candleIndexAtOrAfter(candles []models.Candle, timestamp time.Time) int {
	for i, candle := range candles {
		if !candle.Timestamp.Before(timestamp) {
			return i
		}
	}
	return -1
}

type backtestRegimeSummary struct {
	Regime       string
	Strategy     string
	Variant      string
	Setup        string
	Rows         int
	Trades       int
	Wins         int
	TotalPnL     float64
	AvgHoldBars  float64
	BestVerdict  string
	Expectancy   float64
	WinRate      float64
	RankingScore float64
}

func displayBacktestRegimeSummary(output *Output, results []backtestGridResult, limit int) {
	summary := aggregateBacktestRegimeSummary(results)
	if len(summary) == 0 {
		output.Dim("No regime trade stats to report.")
		return
	}
	if limit <= 0 || limit > len(summary) {
		limit = len(summary)
	}
	output.Bold("Regime Expectancy")
	table := NewTable(output, "Regime", "Strategy", "Variant", "Setup", "Rows", "Trades", "Win", "Expectancy", "Total P&L", "Avg Hold", "Best")
	for i := 0; i < limit; i++ {
		row := summary[i]
		table.AddRow(
			row.Regime,
			row.Strategy,
			row.Variant,
			row.Setup,
			strconv.Itoa(row.Rows),
			strconv.Itoa(row.Trades),
			fmt.Sprintf("%.1f%%", row.WinRate),
			FormatIndianCurrency(row.Expectancy),
			FormatIndianCurrency(row.TotalPnL),
			fmt.Sprintf("%.1f", row.AvgHoldBars),
			row.BestVerdict,
		)
	}
	table.Render()
	if limit < len(summary) {
		output.Println()
		output.Dim("Showing %d of %d regime rows. Use --limit 0 to print all rows.", limit, len(summary))
	}
}

func aggregateBacktestRegimeSummary(results []backtestGridResult) []backtestRegimeSummary {
	type key struct {
		regime   string
		strategy string
		variant  string
		setup    string
	}
	summaries := make(map[key]*backtestRegimeSummary)
	for _, result := range results {
		if result.Error != "" {
			continue
		}
		for _, regime := range result.Regimes {
			k := key{
				regime:   regime.Regime,
				strategy: result.Strategy,
				variant:  result.ParamVariant,
				setup:    result.Setup,
			}
			summary := summaries[k]
			if summary == nil {
				summary = &backtestRegimeSummary{
					Regime:      regime.Regime,
					Strategy:    result.Strategy,
					Variant:     result.ParamVariant,
					Setup:       result.Setup,
					BestVerdict: result.Verdict,
				}
				summaries[k] = summary
			}
			summary.Rows++
			summary.Trades += regime.Trades
			summary.Wins += regime.Wins
			summary.TotalPnL += regime.TotalPnL
			summary.AvgHoldBars += regime.AvgHoldBars * float64(regime.Trades)
			if verdictRank(result.Verdict) < verdictRank(summary.BestVerdict) {
				summary.BestVerdict = result.Verdict
			}
		}
	}

	out := make([]backtestRegimeSummary, 0, len(summaries))
	for _, summary := range summaries {
		if summary.Trades == 0 {
			continue
		}
		summary.WinRate = float64(summary.Wins) / float64(summary.Trades) * 100
		summary.Expectancy = summary.TotalPnL / float64(summary.Trades)
		summary.AvgHoldBars = summary.AvgHoldBars / float64(summary.Trades)
		summary.RankingScore = summary.Expectancy * math.Sqrt(float64(summary.Trades))
		if summary.Expectancy > 0 {
			if summary.BestVerdict == "PASS" {
				summary.RankingScore += 100000
			} else if summary.BestVerdict == "WATCH" {
				summary.RankingScore += 10000
			}
		}
		out = append(out, *summary)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RankingScore > out[j].RankingScore
	})
	return out
}

func verdictRank(verdict string) int {
	switch verdict {
	case "PASS":
		return 0
	case "WATCH":
		return 1
	default:
		return 2
	}
}

type paperCandidateStore interface {
	SavePaperCandidate(ctx context.Context, candidate *models.PaperCandidate) error
}

func promoteBacktestGridCandidates(ctx context.Context, dataStore paperCandidateStore, results []backtestGridResult, verdicts map[string]bool, status string, minRegimeTrades int, minCandidateScore float64) ([]models.PaperCandidate, error) {
	if status == "" {
		status = models.PaperCandidateStatusActive
	}
	if minRegimeTrades <= 0 {
		minRegimeTrades = 1
	}
	promoted := make([]models.PaperCandidate, 0)
	for _, result := range results {
		if result.Error != "" || !verdicts[result.Verdict] {
			continue
		}
		if minCandidateScore > 0 && result.CandidateScore < minCandidateScore {
			continue
		}
		candidate := paperCandidateFromGridResult(result, status, minRegimeTrades)
		if len(candidate.AllowedRegimes) == 0 {
			continue
		}
		if err := dataStore.SavePaperCandidate(ctx, &candidate); err != nil {
			return promoted, err
		}
		promoted = append(promoted, candidate)
	}
	return promoted, nil
}

func paperCandidateFromGridResult(result backtestGridResult, status string, minRegimeTrades int) models.PaperCandidate {
	allowed, blocked := regimeGuardrails(result.Regimes, minRegimeTrades)
	return models.PaperCandidate{
		ID:                  paperCandidateID(result),
		Status:              status,
		Symbol:              result.Symbol,
		Exchange:            result.Exchange,
		Strategy:            result.Strategy,
		ParamVariant:        result.ParamVariant,
		Parameters:          result.Parameters,
		Timeframe:           result.Timeframe,
		Setup:               result.Setup,
		Source:              "backtest_grid",
		Verdict:             result.Verdict,
		Reason:              result.Reason,
		Days:                result.Days,
		Candles:             result.Candles,
		SignalBars:          result.SignalBars,
		BuySignals:          result.BuySignals,
		SellSignals:         result.SellSignals,
		HoldSignals:         result.HoldSignals,
		DirectionalSignals:  result.DirectionalSignals,
		SignalRatePct:       result.SignalRatePct,
		TradeConversionPct:  result.TradeConversionPct,
		Trades:              result.Trades,
		ValidationTrades:    result.ValidationTrades,
		ReturnPct:           result.ReturnPct,
		TrainReturnPct:      result.TrainReturnPct,
		ValidationReturnPct: result.ValidationReturnPct,
		WinRate:             result.WinRate,
		ProfitFactor:        result.ProfitFactor,
		Expectancy:          result.Expectancy,
		MaxDrawdownPct:      result.MaxDrawdownPct,
		SharpeRatio:         result.SharpeRatio,
		CandidateScore:      result.CandidateScore,
		EvidenceScore:       result.EvidenceScore,
		EvidenceSentiment:   result.EvidenceSentiment,
		EvidenceConfidence:  result.EvidenceConfidence,
		EvidenceSources:     result.EvidenceSources,
		EvidenceError:       result.EvidenceError,
		ScoreReason:         result.CandidateScoreReason,
		StopLossPercent:     result.StopLossPercent,
		TakeProfitPercent:   result.TakeProfitPercent,
		TrailingStopPercent: result.TrailingStopPercent,
		AllowShort:          result.AllowShort,
		AllowedRegimes:      allowed,
		BlockedRegimes:      blocked,
		RegimeStats:         paperCandidateRegimeStats(result.Regimes),
		PromotedAt:          time.Now(),
		UpdatedAt:           time.Now(),
	}
}

func regimeGuardrails(regimes []backtestRegimeTradeStat, minTrades int) ([]string, []string) {
	allowed := make([]string, 0)
	blocked := make([]string, 0)
	for _, stat := range regimes {
		if stat.Trades < minTrades {
			continue
		}
		if stat.Expectancy > 0 {
			allowed = append(allowed, stat.Regime)
		} else {
			blocked = append(blocked, stat.Regime)
		}
	}
	sort.Strings(allowed)
	sort.Strings(blocked)
	return allowed, blocked
}

func paperCandidateRegimeStats(regimes []backtestRegimeTradeStat) []models.PaperCandidateRegimeStat {
	stats := make([]models.PaperCandidateRegimeStat, 0, len(regimes))
	for _, regime := range regimes {
		stats = append(stats, models.PaperCandidateRegimeStat{
			Regime:      regime.Regime,
			Trades:      regime.Trades,
			Wins:        regime.Wins,
			WinRate:     regime.WinRate,
			TotalPnL:    regime.TotalPnL,
			Expectancy:  regime.Expectancy,
			AvgHoldBars: regime.AvgHoldBars,
		})
	}
	return stats
}

func paperCandidateID(result backtestGridResult) string {
	parts := []string{
		result.Symbol,
		result.Exchange,
		result.Strategy,
		result.ParamVariant,
		result.Timeframe,
		result.Setup,
		strconv.Itoa(result.Days),
	}
	return strings.ToLower(sanitizeCandidateID(strings.Join(parts, "_")))
}

func sanitizeCandidateID(value string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func formatScoreOrDash(value float64) string {
	if value == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", value)
}

func parseVerdictSet(flag string) map[string]bool {
	values := parseCSVFlag(flag)
	if len(values) == 0 {
		values = []string{"PASS"}
	}
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[strings.ToUpper(value)] = true
	}
	return out
}

func writeBacktestGridCSV(path string, results []backtestGridResult) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	header := []string{"verdict", "reason", "symbol", "exchange", "strategy", "param_variant", "parameters", "timeframe", "days", "setup", "regime", "candles", "signal_bars", "buy_signals", "sell_signals", "hold_signals", "directional_signals", "signal_rate_pct", "trade_conversion_pct", "latest_signal", "signal_diagnostic", "trades", "validation_trades", "return_pct", "validation_return_pct", "train_return_pct", "win_rate", "profit_factor", "expectancy", "max_drawdown_pct", "sharpe_ratio", "score", "candidate_score", "candidate_score_reason", "evidence_score", "evidence_sentiment", "evidence_confidence", "evidence_sources", "evidence_error", "error"}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, r := range results {
		row := []string{
			r.Verdict,
			r.Reason,
			r.Symbol,
			r.Exchange,
			r.Strategy,
			r.ParamVariant,
			r.Parameters,
			r.Timeframe,
			strconv.Itoa(r.Days),
			r.Setup,
			r.Regime,
			strconv.Itoa(r.Candles),
			strconv.Itoa(r.SignalBars),
			strconv.Itoa(r.BuySignals),
			strconv.Itoa(r.SellSignals),
			strconv.Itoa(r.HoldSignals),
			strconv.Itoa(r.DirectionalSignals),
			formatCSVFloat(r.SignalRatePct),
			formatCSVFloat(r.TradeConversionPct),
			r.LatestSignal,
			r.SignalDiagnostic,
			strconv.Itoa(r.Trades),
			strconv.Itoa(r.ValidationTrades),
			formatCSVFloat(r.ReturnPct),
			formatCSVFloat(r.ValidationReturnPct),
			formatCSVFloat(r.TrainReturnPct),
			formatCSVFloat(r.WinRate),
			formatCSVFloat(r.ProfitFactor),
			formatCSVFloat(r.Expectancy),
			formatCSVFloat(r.MaxDrawdownPct),
			formatCSVFloat(r.SharpeRatio),
			formatCSVFloat(r.Score),
			formatCSVFloat(r.CandidateScore),
			r.CandidateScoreReason,
			formatCSVFloat(r.EvidenceScore),
			r.EvidenceSentiment,
			formatCSVFloat(r.EvidenceConfidence),
			strconv.Itoa(r.EvidenceSources),
			r.EvidenceError,
			r.Error,
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	return writer.Error()
}

func intsToStrings(values []int) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strconv.Itoa(value))
	}
	return out
}

func setupNames(setups []backtestSetup) []string {
	names := make([]string, 0, len(setups))
	for _, setup := range setups {
		names = append(names, setup.Name)
	}
	return names
}

func finiteMetric(value float64) float64 {
	if math.IsInf(value, 0) {
		return 999
	}
	if math.IsNaN(value) {
		return 0
	}
	return value
}

func formatCSVFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}
