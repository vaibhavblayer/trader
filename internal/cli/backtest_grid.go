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

type backtestGridThresholds struct {
	MinTrades           int
	MinValidationTrades int
	MinProfitFactor     float64
	MinReturn           float64
	MinValidationReturn float64
	MaxDrawdown         float64
}

type backtestGridResult struct {
	Verdict              string  `json:"verdict"`
	Reason               string  `json:"reason,omitempty"`
	Symbol               string  `json:"symbol"`
	Exchange             string  `json:"exchange"`
	Strategy             string  `json:"strategy"`
	Timeframe            string  `json:"timeframe"`
	Days                 int     `json:"days"`
	Setup                string  `json:"setup"`
	Regime               string  `json:"regime"`
	Candles              int     `json:"candles"`
	Trades               int     `json:"trades"`
	ValidationTrades     int     `json:"validation_trades"`
	ReturnPct            float64 `json:"return_pct"`
	AnnualizedReturnPct  float64 `json:"annualized_return_pct"`
	ValidationReturnPct  float64 `json:"validation_return_pct"`
	TrainReturnPct       float64 `json:"train_return_pct"`
	WinRate              float64 `json:"win_rate"`
	ProfitFactor         float64 `json:"profit_factor"`
	Expectancy           float64 `json:"expectancy"`
	MaxDrawdownPct       float64 `json:"max_drawdown_pct"`
	SharpeRatio          float64 `json:"sharpe_ratio"`
	Score                float64 `json:"score"`
	Error                string  `json:"error,omitempty"`
	StopLossPercent      float64 `json:"stop_loss_percent,omitempty"`
	TakeProfitPercent    float64 `json:"take_profit_percent,omitempty"`
	TrailingStopPercent  float64 `json:"trailing_stop_percent,omitempty"`
	AllowShort           bool    `json:"allow_short"`
	ExecutionTiming      string  `json:"execution_timing"`
	SlippagePercent      float64 `json:"slippage_percent"`
	CommissionPercent    float64 `json:"commission_percent"`
	PartialFillsEnabled  bool    `json:"partial_fills_enabled"`
	MaxFillVolumePercent float64 `json:"max_fill_volume_percent,omitempty"`
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
			exchange, _ := cmd.Flags().GetString("exchange")
			capital, _ := cmd.Flags().GetFloat64("capital")
			slippagePct, _ := cmd.Flags().GetFloat64("slippage")
			commissionPct, _ := cmd.Flags().GetFloat64("commission")
			executionTiming, _ := cmd.Flags().GetString("execution")
			partialFills, _ := cmd.Flags().GetBool("partial-fills")
			maxFillVolumePct, _ := cmd.Flags().GetFloat64("max-fill-volume")
			limit, _ := cmd.Flags().GetInt("limit")
			outputPath, _ := cmd.Flags().GetString("output")

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
				ReliabilityThresholds: thresholds,
			})
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

			if output.IsJSON() {
				return output.JSON(results)
			}
			displayBacktestGridResults(output, results, limit)
			return nil
		},
	}

	cmd.Flags().String("symbols", "", "Comma-separated symbols")
	cmd.Flags().String("watchlist", "", "Watchlist name (nifty50, banknifty, it, auto, pharma, fmcg, or custom)")
	cmd.Flags().String("strategies", "multi_indicator,donchian_breakout,supertrend", "Comma-separated strategies or 'all'")
	cmd.Flags().String("timeframes", "1day", "Comma-separated candle timeframes")
	cmd.Flags().String("periods", "365", "Comma-separated lookback periods in days")
	cmd.Flags().String("setups", "base,sl2tp4,short_sl2tp4", "Comma-separated setups (base, sl2tp4, sl25tp5, trail2, short_base, short_sl2tp4)")
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
					for _, setup := range cfg.Setups {
						btConfig := trading.BacktestConfig{
							Symbol:               symbol,
							Timeframe:            timeframe,
							InitialCapital:       cfg.Capital,
							Strategy:             strategy,
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
						result := evaluateBacktestGridCandidate(ctx, engine, btConfig, candles, cfg, setup, regime, days)
						results = append(results, result)
					}
				}
			}
		}
	}
	return results
}

func evaluateBacktestGridCandidate(ctx context.Context, engine *trading.DefaultBacktestEngine, btConfig trading.BacktestConfig, candles []models.Candle, runCfg backtestGridRunConfig, setup backtestSetup, regime string, days int) backtestGridResult {
	full, err := engine.RunOnCandles(ctx, btConfig, candles)
	base := backtestGridResult{
		Symbol:               btConfig.Symbol,
		Exchange:             runCfg.Exchange,
		Strategy:             btConfig.Strategy,
		Timeframe:            btConfig.Timeframe,
		Days:                 days,
		Setup:                setup.Name,
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

	base.Trades = full.TotalTrades
	base.ReturnPct = full.TotalReturn
	base.AnnualizedReturnPct = full.AnnualizedReturn
	base.WinRate = full.WinRate
	base.ProfitFactor = finiteMetric(full.ProfitFactor)
	base.Expectancy = full.Expectancy
	base.MaxDrawdownPct = full.MaxDrawdown
	base.SharpeRatio = finiteMetric(full.SharpeRatio)

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
		case "sl2tp4":
			setups = append(setups, backtestSetup{Name: "sl2tp4", StopLoss: 2, TakeProfit: 4})
		case "sl25tp5":
			setups = append(setups, backtestSetup{Name: "sl25tp5", StopLoss: 2.5, TakeProfit: 5})
		case "trail2":
			setups = append(setups, backtestSetup{Name: "trail2", StopLoss: 3, TrailingStop: 2})
		case "short_base":
			setups = append(setups, backtestSetup{Name: "short_base", StopLoss: 3, AllowShort: true})
		case "short_sl2tp4":
			setups = append(setups, backtestSetup{Name: "short_sl2tp4", StopLoss: 2, TakeProfit: 4, AllowShort: true})
		default:
			return nil, fmt.Errorf("unknown setup %q", name)
		}
	}
	return setups, nil
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
	table := NewTable(output, "Verdict", "Symbol", "Strategy", "TF", "Days", "Setup", "Ret", "Val", "Tr", "VTr", "PF", "DD", "Regime", "Reason")
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
			r.Timeframe,
			strconv.Itoa(r.Days),
			r.Setup,
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
		return results[i].Score > results[j].Score
	})
}

func writeBacktestGridCSV(path string, results []backtestGridResult) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	header := []string{"verdict", "reason", "symbol", "exchange", "strategy", "timeframe", "days", "setup", "regime", "candles", "trades", "validation_trades", "return_pct", "validation_return_pct", "train_return_pct", "win_rate", "profit_factor", "expectancy", "max_drawdown_pct", "sharpe_ratio", "score", "error"}
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
			r.Timeframe,
			strconv.Itoa(r.Days),
			r.Setup,
			r.Regime,
			strconv.Itoa(r.Candles),
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
