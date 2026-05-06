package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"zerodha-trader/internal/agents"
	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
)

type discoveryResearchReport struct {
	Discovered     []ScanResult                       `json:"discovered"`
	DiscoveryStats symbolDiscoveryStats               `json:"discovery_stats"`
	Evidence       map[string]candidateEvidenceResult `json:"evidence,omitempty"`
	Grid           []backtestGridResult               `json:"grid"`
	Promoted       []models.PaperCandidate            `json:"promoted,omitempty"`
	CandidateRun   []candidateRunResult               `json:"candidate_run,omitempty"`
}

type symbolDiscoveryStats struct {
	Universe      int    `json:"universe"`
	Fetched       int    `json:"fetched"`
	Matched       int    `json:"matched"`
	Selected      int    `json:"selected"`
	FetchErrors   int    `json:"fetch_errors"`
	ThinHistory   int    `json:"thin_history"`
	StaleCandles  int    `json:"stale_candles"`
	Filtered      int    `json:"filtered"`
	Timeframe     string `json:"timeframe"`
	Days          int    `json:"days"`
	MinCandles    int    `json:"min_candles"`
	MaxCandleAge  string `json:"max_candle_age,omitempty"`
	LastDataError string `json:"last_data_error,omitempty"`
}

type symbolDiscoveryOptions struct {
	Exchange     string
	Index        string
	Watchlist    string
	Preset       string
	Limit        int
	SortBy       string
	Timeframe    string
	Days         int
	MinCandles   int
	MaxCandleAge time.Duration
	RSIBelow     float64
	RSIAbove     float64
	VolumeAbove  float64
	MinATR       float64
	MinChange    float64
	MinPrice     float64
	MaxPrice     float64
	Gainers      bool
	Losers       bool
}

func newBacktestDiscoverCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Scan top symbols, run research grid, and promote candidates",
		Long: `Run the complete discovery-to-research workflow.

The command scans a symbol universe, feeds discovered symbols into the backtest
research grid, promotes eligible PASS/WATCH rows into paper candidates, and can
immediately dry-run the promoted candidates through regime guardrails.`,
		Example: `  trader backtest discover --index banknifty --scan-sort change --scan-limit 8
  trader backtest discover --index fno --scan-timeframe 15minute --scan-days 5 --scan-min-candles 80 --scan-max-age 45m --scan-preset volatile
  trader backtest discover --index nifty50 --scan-preset volatile --scan-limit 10 --promote-paper-candidates --candidate-run
  trader backtest discover --watchlist default --strategies supertrend,multi_indicator --param-grid research`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			if app.Broker == nil {
				return fmt.Errorf("broker not configured")
			}

			exchange, _ := cmd.Flags().GetString("exchange")
			index, _ := cmd.Flags().GetString("index")
			watchlist, _ := cmd.Flags().GetString("watchlist")
			scanPreset, _ := cmd.Flags().GetString("scan-preset")
			scanLimit, _ := cmd.Flags().GetInt("scan-limit")
			scanSort, _ := cmd.Flags().GetString("scan-sort")
			scanTimeframe, _ := cmd.Flags().GetString("scan-timeframe")
			scanDays, _ := cmd.Flags().GetInt("scan-days")
			scanMinCandles, _ := cmd.Flags().GetInt("scan-min-candles")
			scanMaxAge, _ := cmd.Flags().GetDuration("scan-max-age")
			rsiBelow, _ := cmd.Flags().GetFloat64("rsi-below")
			rsiAbove, _ := cmd.Flags().GetFloat64("rsi-above")
			volumeAbove, _ := cmd.Flags().GetFloat64("volume-above")
			minATR, _ := cmd.Flags().GetFloat64("min-atr")
			minChange, _ := cmd.Flags().GetFloat64("min-change")
			minPrice, _ := cmd.Flags().GetFloat64("min-price")
			maxPrice, _ := cmd.Flags().GetFloat64("max-price")
			gainers, _ := cmd.Flags().GetBool("gainers")
			losers, _ := cmd.Flags().GetBool("losers")

			strategiesFlag, _ := cmd.Flags().GetString("strategies")
			timeframesFlag, _ := cmd.Flags().GetString("timeframes")
			periodsFlag, _ := cmd.Flags().GetString("periods")
			setupsFlag, _ := cmd.Flags().GetString("setups")
			paramGrid, _ := cmd.Flags().GetString("param-grid")
			capital, _ := cmd.Flags().GetFloat64("capital")
			slippagePct, _ := cmd.Flags().GetFloat64("slippage")
			commissionPct, _ := cmd.Flags().GetFloat64("commission")
			executionTiming, _ := cmd.Flags().GetString("execution")
			partialFills, _ := cmd.Flags().GetBool("partial-fills")
			maxFillVolumePct, _ := cmd.Flags().GetFloat64("max-fill-volume")
			gridLimit, _ := cmd.Flags().GetInt("grid-limit")
			regimeWindow, _ := cmd.Flags().GetInt("regime-window")
			regimeReport, _ := cmd.Flags().GetBool("regime-report")
			promote, _ := cmd.Flags().GetBool("promote-paper-candidates")
			promoteVerdictsFlag, _ := cmd.Flags().GetString("promote-verdicts")
			candidateStatus, _ := cmd.Flags().GetString("candidate-status")
			minRegimeTrades, _ := cmd.Flags().GetInt("min-regime-trades")
			minCandidateScore, _ := cmd.Flags().GetFloat64("min-candidate-score")
			evidenceResearch, _ := cmd.Flags().GetBool("evidence-research")
			evidenceDomainsFlag, _ := cmd.Flags().GetString("evidence-domains")
			evidenceLive, _ := cmd.Flags().GetBool("evidence-live")
			candidateRun, _ := cmd.Flags().GetBool("candidate-run")
			candidateRunDry, _ := cmd.Flags().GetBool("candidate-run-dry")
			candidateRunDays, _ := cmd.Flags().GetInt("candidate-run-days")
			candidateRunWindow, _ := cmd.Flags().GetString("candidate-run-window")
			candidateRunRegimeModeFlag, _ := cmd.Flags().GetString("candidate-run-regime-mode")

			thresholds := backtestGridThresholds{}
			thresholds.MinTrades, _ = cmd.Flags().GetInt("min-trades")
			thresholds.MinValidationTrades, _ = cmd.Flags().GetInt("min-validation-trades")
			thresholds.MinProfitFactor, _ = cmd.Flags().GetFloat64("min-profit-factor")
			thresholds.MinReturn, _ = cmd.Flags().GetFloat64("min-return")
			thresholds.MinValidationReturn, _ = cmd.Flags().GetFloat64("min-validation-return")
			thresholds.MaxDrawdown, _ = cmd.Flags().GetFloat64("max-drawdown")

			discovered, discoveryStats, err := discoverResearchSymbols(ctx, app, symbolDiscoveryOptions{
				Exchange:     exchange,
				Index:        index,
				Watchlist:    watchlist,
				Preset:       scanPreset,
				Limit:        scanLimit,
				SortBy:       scanSort,
				Timeframe:    scanTimeframe,
				Days:         scanDays,
				MinCandles:   scanMinCandles,
				MaxCandleAge: scanMaxAge,
				RSIBelow:     rsiBelow,
				RSIAbove:     rsiAbove,
				VolumeAbove:  volumeAbove,
				MinATR:       minATR,
				MinChange:    minChange,
				MinPrice:     minPrice,
				MaxPrice:     maxPrice,
				Gainers:      gainers,
				Losers:       losers,
			})
			if err != nil {
				return err
			}
			if len(discovered) == 0 {
				if output.IsJSON() {
					return output.JSON(discoveryResearchReport{
						Discovered:     []ScanResult{},
						DiscoveryStats: discoveryStats,
						Grid:           []backtestGridResult{},
					})
				}
				output.Info("No symbols discovered")
				displayDiscoveryStats(output, discoveryStats)
				return nil
			}

			symbols := scanSymbols(discovered)
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
				paramGrid = "research"
			}
			if paramGrid != "default" && paramGrid != "research" {
				return fmt.Errorf("unknown param grid %q", paramGrid)
			}

			if !output.IsJSON() {
				output.Bold("Discovery")
				displayScanResults(output, discovered)
				output.Println()
				output.Bold("Research Grid")
				output.Printf("  Symbols:    %s\n", strings.Join(symbols, ", "))
				output.Printf("  Strategies: %s\n", strings.Join(strategies, ", "))
				output.Printf("  Timeframes: %s\n", strings.Join(timeframes, ", "))
				output.Printf("  Periods:    %s days\n", strings.Join(intsToStrings(periods), ", "))
				output.Printf("  Setups:     %s\n", strings.Join(setupNames(setups), ", "))
				output.Printf("  Param grid: %s\n", paramGrid)
				if evidenceResearch {
					output.Printf("  Evidence:   OpenAI web research\n")
				}
				output.Println()
			}

			grid := runBacktestGrid(ctx, app, backtestGridRunConfig{
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
			evidence := map[string]candidateEvidenceResult{}
			if evidenceResearch {
				if app.Research == nil {
					return fmt.Errorf("OpenAI Responses research client is not configured")
				}
				evidence = fetchCandidateEvidence(ctx, app.Research, discovered, exchange, parseDomainList(evidenceDomainsFlag), evidenceLive)
			}
			applyEvidenceAwareCandidateScores(grid, discovered, evidence)
			sortBacktestGridResults(grid)

			var promoted []models.PaperCandidate
			if promote {
				if app.Store == nil {
					return fmt.Errorf("paper candidate store is not available")
				}
				promoted, err = promoteBacktestGridCandidates(ctx, app.Store, grid, parseVerdictSet(promoteVerdictsFlag), strings.ToUpper(strings.TrimSpace(candidateStatus)), minRegimeTrades, minCandidateScore)
				if err != nil {
					return err
				}
			}

			var candidateResults []candidateRunResult
			if candidateRun {
				candidateRunRegimeMode, err := parseCandidateRegimeMode(candidateRunRegimeModeFlag)
				if err != nil {
					return err
				}
				window, err := time.ParseDuration(candidateRunWindow)
				if err != nil {
					return fmt.Errorf("invalid candidate-run-window %q: %w", candidateRunWindow, err)
				}
				candidateResults = runPaperCandidates(ctx, app, promoted, candidateRunOptions{
					Days:         candidateRunDays,
					MinCandles:   80,
					RegimeWindow: regimeWindow,
					RegimeMode:   candidateRunRegimeMode,
					TimeWindow:   window,
					DryRun:       candidateRunDry,
				})
			}

			report := discoveryResearchReport{
				Discovered:     discovered,
				DiscoveryStats: discoveryStats,
				Evidence:       evidence,
				Grid:           grid,
				Promoted:       promoted,
				CandidateRun:   candidateResults,
			}
			if output.IsJSON() {
				return output.JSON(report)
			}

			displayBacktestGridResults(output, grid, gridLimit)
			if regimeReport {
				output.Println()
				displayBacktestRegimeSummary(output, grid, gridLimit)
			}
			if promote {
				output.Println()
				output.Success("Promoted %d paper candidate(s)", len(promoted))
				if len(promoted) > 0 {
					displayPaperCandidateSummary(output, promoted)
				}
			}
			if candidateRun {
				output.Println()
				displayCandidateRunResults(output, candidateResults, candidateRunDry)
			}
			return nil
		},
	}

	cmd.Flags().String("index", "banknifty", "Index universe to scan")
	cmd.Flags().String("watchlist", "", "Watchlist to scan when --index is empty")
	cmd.Flags().String("scan-preset", "", "Scan preset (momentum, oversold, overbought, breakout, reversal, movers, volatile)")
	cmd.Flags().Int("scan-limit", 8, "Maximum discovered symbols to research")
	cmd.Flags().String("scan-sort", "change", "Discovery sort by price, change, rsi, volume, atr, age")
	cmd.Flags().String("scan-timeframe", "1day", "Discovery candle timeframe, e.g. 5minute, 15minute, 1day")
	cmd.Flags().Int("scan-days", 30, "Discovery lookback period in calendar days")
	cmd.Flags().Int("scan-min-candles", 15, "Minimum candles required before a symbol can be discovered")
	cmd.Flags().Duration("scan-max-age", 0, "Maximum age of the latest discovery candle, e.g. 30m; 0 disables freshness filtering")
	cmd.Flags().Float64("rsi-below", 0, "Discovery RSI below threshold")
	cmd.Flags().Float64("rsi-above", 0, "Discovery RSI above threshold")
	cmd.Flags().Float64("volume-above", 0, "Discovery volume multiple above average")
	cmd.Flags().Float64("min-atr", 0, "Discovery minimum ATR percentage")
	cmd.Flags().Float64("min-change", 0, "Discovery minimum absolute change percentage")
	cmd.Flags().Float64("min-price", 0, "Discovery minimum stock price")
	cmd.Flags().Float64("max-price", 0, "Discovery maximum stock price")
	cmd.Flags().Bool("gainers", false, "Discovery gainers only")
	cmd.Flags().Bool("losers", false, "Discovery losers only")
	cmd.Flags().StringP("exchange", "e", "NSE", "Exchange (NSE, BSE)")

	cmd.Flags().String("strategies", "intraday_momentum,multi_indicator,donchian_breakout,supertrend", "Comma-separated strategies or 'all'")
	cmd.Flags().String("timeframes", "1day", "Comma-separated candle timeframes")
	cmd.Flags().String("periods", "1095", "Comma-separated lookback periods in days")
	cmd.Flags().String("setups", "sl2tp4,short_sl2tp4", "Comma-separated setups")
	cmd.Flags().String("param-grid", "research", "Strategy parameter grid (default, research)")
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
	cmd.Flags().Int("grid-limit", 25, "Rows to print from the research grid")
	cmd.Flags().Bool("regime-report", false, "Print trade expectancy grouped by entry regime")
	cmd.Flags().Int("regime-window", 50, "Candles used to classify regimes")
	cmd.Flags().Bool("promote-paper-candidates", false, "Save eligible grid rows as controlled paper-soak candidates")
	cmd.Flags().String("promote-verdicts", "PASS", "Comma-separated verdicts eligible for promotion")
	cmd.Flags().String("candidate-status", models.PaperCandidateStatusActive, "Status for promoted candidates")
	cmd.Flags().Int("min-regime-trades", 2, "Minimum trades for a regime guardrail")
	cmd.Flags().Float64("min-candidate-score", 0, "Minimum evidence-aware candidate score for promotion; 0 disables")
	cmd.Flags().Bool("evidence-research", false, "Fetch cited OpenAI web evidence for discovered symbols and include it in candidate scoring")
	cmd.Flags().String("evidence-domains", "moneycontrol.com,nseindia.com,bseindia.com", "Comma-separated allowed domains for evidence research")
	cmd.Flags().Bool("evidence-live", true, "Allow live external web access for evidence research")
	cmd.Flags().Bool("candidate-run", false, "Run newly promoted candidates through paper guardrails")
	cmd.Flags().Bool("candidate-run-dry", true, "Candidate-run dry run mode")
	cmd.Flags().Int("candidate-run-days", 180, "Candidate-run historical lookback days")
	cmd.Flags().String("candidate-run-regime-mode", regimeModeStrict, "Candidate-run regime guardrail mode: strict, allow-unknown, or explore")
	cmd.Flags().String("candidate-run-window", "24h", "Candidate paper prediction evaluation window")
	return cmd
}

func discoverResearchSymbols(ctx context.Context, app *App, opts symbolDiscoveryOptions) ([]ScanResult, symbolDiscoveryStats, error) {
	opts, err := normalizeDiscoveryOptions(opts)
	if err != nil {
		return nil, symbolDiscoveryStats{}, err
	}
	symbols, err := discoveryUniverse(ctx, app, opts)
	if err != nil {
		return nil, symbolDiscoveryStats{}, err
	}
	stats := symbolDiscoveryStats{
		Universe:   len(symbols),
		Timeframe:  opts.Timeframe,
		Days:       opts.Days,
		MinCandles: opts.MinCandles,
	}
	if opts.MaxCandleAge > 0 {
		stats.MaxCandleAge = opts.MaxCandleAge.String()
	}
	now := time.Now()
	results := make([]ScanResult, 0, len(symbols))
	for _, symbol := range symbols {
		candles, err := app.Broker.GetHistorical(ctx, broker.HistoricalRequest{
			Symbol:    symbol,
			Exchange:  models.Exchange(opts.Exchange),
			Timeframe: opts.Timeframe,
			From:      now.AddDate(0, 0, -opts.Days),
			To:        now,
		})
		if err != nil {
			stats.FetchErrors++
			stats.LastDataError = fmt.Sprintf("%s: %v", symbol, err)
			continue
		}
		if len(candles) < opts.MinCandles {
			stats.ThinHistory++
			continue
		}
		stats.Fetched++
		result, rejectReason, ok := scanResultFromCandlesWithReason(symbol, candles, opts)
		if ok {
			results = append(results, result)
			continue
		}
		switch rejectReason {
		case scanRejectStale:
			stats.StaleCandles++
		case scanRejectThin:
			stats.ThinHistory++
		default:
			stats.Filtered++
		}
	}
	sortScanResults(results, opts.SortBy)
	stats.Matched = len(results)
	if opts.Limit > 0 && len(results) > opts.Limit {
		results = results[:opts.Limit]
	}
	stats.Selected = len(results)
	return results, stats, nil
}

func fetchCandidateEvidence(ctx context.Context, research agents.ResearchEvidenceClient, discovered []ScanResult, exchange string, domains []string, live bool) map[string]candidateEvidenceResult {
	out := make(map[string]candidateEvidenceResult, len(discovered))
	for _, candidate := range discovered {
		symbol := strings.ToUpper(strings.TrimSpace(candidate.Symbol))
		if symbol == "" {
			continue
		}
		report, err := research.ResearchSymbol(ctx, agents.ResearchEvidenceRequest{
			Symbol:         symbol,
			Exchange:       exchange,
			CurrentPrice:   candidate.LTP,
			AllowedDomains: domains,
			LiveWebAccess:  live,
		})
		result := candidateEvidenceResult{Report: report}
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Score = scoreResearchEvidence(result)
		}
		out[symbol] = result
	}
	return out
}

func normalizeDiscoveryOptions(opts symbolDiscoveryOptions) (symbolDiscoveryOptions, error) {
	opts = applyDiscoveryPreset(opts)
	opts.Exchange = strings.ToUpper(strings.TrimSpace(opts.Exchange))
	if opts.Exchange == "" {
		opts.Exchange = "NSE"
	}
	opts.Timeframe = strings.TrimSpace(opts.Timeframe)
	if opts.Timeframe == "" {
		opts.Timeframe = "1day"
	}
	if opts.Days <= 0 {
		return opts, fmt.Errorf("scan-days must be positive")
	}
	if opts.MinCandles <= 0 {
		return opts, fmt.Errorf("scan-min-candles must be positive")
	}
	if opts.MaxCandleAge < 0 {
		return opts, fmt.Errorf("scan-max-age cannot be negative")
	}
	return opts, nil
}

func displayDiscoveryStats(output *Output, stats symbolDiscoveryStats) {
	maxAge := "disabled"
	if stats.MaxCandleAge != "" {
		maxAge = stats.MaxCandleAge
	}
	output.Printf("  Universe: %d | fetched: %d | matched: %d | selected: %d\n", stats.Universe, stats.Fetched, stats.Matched, stats.Selected)
	output.Printf("  Scan: %s over %d days | min candles: %d | max age: %s\n", stats.Timeframe, stats.Days, stats.MinCandles, maxAge)
	output.Printf("  Rejected: fetch_errors=%d thin_history=%d stale_candles=%d filtered=%d\n",
		stats.FetchErrors, stats.ThinHistory, stats.StaleCandles, stats.Filtered)
	if stats.LastDataError != "" {
		output.Dim("Last data error: %s", stats.LastDataError)
	}
}

func applyDiscoveryPreset(opts symbolDiscoveryOptions) symbolDiscoveryOptions {
	switch strings.ToLower(strings.TrimSpace(opts.Preset)) {
	case "momentum":
		if opts.RSIAbove == 0 {
			opts.RSIAbove = 60
		}
		if opts.VolumeAbove == 0 {
			opts.VolumeAbove = 1.5
		}
	case "oversold":
		if opts.RSIBelow == 0 {
			opts.RSIBelow = 30
		}
	case "overbought":
		if opts.RSIAbove == 0 {
			opts.RSIAbove = 70
		}
	case "breakout":
		if opts.VolumeAbove == 0 {
			opts.VolumeAbove = 2
		}
	case "reversal":
		if opts.RSIBelow == 0 {
			opts.RSIBelow = 35
		}
		if opts.VolumeAbove == 0 {
			opts.VolumeAbove = 1.5
		}
	case "movers":
		if opts.MinChange == 0 {
			opts.MinChange = 2
		}
		if opts.VolumeAbove == 0 {
			opts.VolumeAbove = 1.2
		}
		if opts.SortBy == "" {
			opts.SortBy = "change"
		}
	case "volatile":
		if opts.MinATR == 0 {
			opts.MinATR = 2
		}
		if opts.VolumeAbove == 0 {
			opts.VolumeAbove = 1
		}
		if opts.SortBy == "" {
			opts.SortBy = "atr"
		}
	}
	return opts
}

func discoveryUniverse(ctx context.Context, app *App, opts symbolDiscoveryOptions) ([]string, error) {
	if opts.Index != "" {
		symbols := getIndexConstituents(opts.Index)
		if len(symbols) == 0 {
			return nil, fmt.Errorf("unknown index: %s", opts.Index)
		}
		return symbols, nil
	}
	watchlist := opts.Watchlist
	if watchlist == "" {
		watchlist = "default"
	}
	if predefined := getPredefinedWatchlist(watchlist, app, ctx); len(predefined) > 0 {
		return predefined, nil
	}
	return []string{"RELIANCE", "TCS", "INFY", "HDFCBANK", "ICICIBANK", "SBIN", "BHARTIARTL", "ITC", "KOTAKBANK", "LT"}, nil
}

const (
	scanRejectThin   = "thin_history"
	scanRejectStale  = "stale_candle"
	scanRejectFilter = "filter"
)

func scanResultFromCandles(symbol string, candles []models.Candle, opts symbolDiscoveryOptions) (ScanResult, bool) {
	result, _, ok := scanResultFromCandlesWithReason(symbol, candles, opts)
	return result, ok
}

func scanResultFromCandlesWithReason(symbol string, candles []models.Candle, opts symbolDiscoveryOptions) (ScanResult, string, bool) {
	if len(candles) == 0 || (opts.MinCandles > 0 && len(candles) < opts.MinCandles) {
		return ScanResult{}, scanRejectThin, false
	}
	last := candles[len(candles)-1]
	lastCandleAge := time.Since(last.Timestamp)
	if opts.MaxCandleAge > 0 && lastCandleAge > opts.MaxCandleAge {
		return ScanResult{}, scanRejectStale, false
	}
	closes := make([]float64, len(candles))
	highs := make([]float64, len(candles))
	lows := make([]float64, len(candles))
	volumes := make([]int64, len(candles))
	for i, candle := range candles {
		closes[i] = candle.Close
		highs[i] = candle.High
		lows[i] = candle.Low
		volumes[i] = candle.Volume
	}
	currentPrice := closes[len(closes)-1]
	if opts.MinPrice > 0 && currentPrice < opts.MinPrice {
		return ScanResult{}, scanRejectFilter, false
	}
	if opts.MaxPrice > 0 && currentPrice > opts.MaxPrice {
		return ScanResult{}, scanRejectFilter, false
	}
	rsi := calculateRSI(closes, 14)
	avgVolume := int64(0)
	if len(volumes) >= 20 {
		for i := len(volumes) - 20; i < len(volumes)-1; i++ {
			avgVolume += volumes[i]
		}
		avgVolume /= 19
	}
	volRatio := 1.0
	if avgVolume > 0 {
		volRatio = float64(volumes[len(volumes)-1]) / float64(avgVolume)
	}
	change := 0.0
	if len(closes) >= 2 && closes[len(closes)-2] > 0 {
		change = ((closes[len(closes)-1] - closes[len(closes)-2]) / closes[len(closes)-2]) * 100
	}
	atr := calculateATR(highs, lows, closes, 14)
	atrPct := 0.0
	if currentPrice > 0 {
		atrPct = atr / currentPrice * 100
	}
	dayRange := 0.0
	if last.Low > 0 {
		dayRange = (last.High - last.Low) / last.Low * 100
	}
	if opts.RSIBelow > 0 && rsi >= opts.RSIBelow {
		return ScanResult{}, scanRejectFilter, false
	}
	if opts.RSIAbove > 0 && rsi <= opts.RSIAbove {
		return ScanResult{}, scanRejectFilter, false
	}
	if opts.VolumeAbove > 0 && volRatio < opts.VolumeAbove {
		return ScanResult{}, scanRejectFilter, false
	}
	if opts.MinATR > 0 && atrPct < opts.MinATR {
		return ScanResult{}, scanRejectFilter, false
	}
	if opts.MinChange > 0 && change < opts.MinChange && change > -opts.MinChange {
		return ScanResult{}, scanRejectFilter, false
	}
	if opts.Gainers && change <= 0 {
		return ScanResult{}, scanRejectFilter, false
	}
	if opts.Losers && change >= 0 {
		return ScanResult{}, scanRejectFilter, false
	}
	signal := "NEUTRAL"
	if rsi < 30 {
		signal = "OVERSOLD"
	} else if rsi > 70 {
		signal = "OVERBOUGHT"
	} else if volRatio > 2 {
		signal = "HIGH VOLUME"
	} else if atrPct > 3 {
		signal = "VOLATILE"
	}
	lastCandleAt := last.Timestamp
	return ScanResult{
		Symbol:               symbol,
		Timeframe:            opts.Timeframe,
		Candles:              len(candles),
		LastCandleAt:         &lastCandleAt,
		LastCandleAgeMinutes: int64(lastCandleAge.Round(time.Minute) / time.Minute),
		LTP:                  currentPrice,
		Change:               change,
		RSI:                  rsi,
		Volume:               volRatio,
		ATRPct:               atrPct,
		DayRange:             dayRange,
		Signal:               signal,
	}, "", true
}

func sortScanResults(results []ScanResult, sortBy string) {
	switch sortBy {
	case "price":
		sort.Slice(results, func(i, j int) bool { return results[i].LTP > results[j].LTP })
	case "rsi":
		sort.Slice(results, func(i, j int) bool { return results[i].RSI < results[j].RSI })
	case "volume":
		sort.Slice(results, func(i, j int) bool { return results[i].Volume > results[j].Volume })
	case "atr", "volatile":
		sort.Slice(results, func(i, j int) bool { return results[i].ATRPct > results[j].ATRPct })
	case "age", "freshness":
		sort.Slice(results, func(i, j int) bool { return results[i].LastCandleAgeMinutes < results[j].LastCandleAgeMinutes })
	default:
		sort.Slice(results, func(i, j int) bool { return absFloat(results[i].Change) > absFloat(results[j].Change) })
	}
}

func scanSymbols(results []ScanResult) []string {
	symbols := make([]string, 0, len(results))
	for _, result := range results {
		symbols = append(symbols, result.Symbol)
	}
	return symbols
}

func displayPaperCandidateSummary(output *Output, candidates []models.PaperCandidate) {
	table := NewTable(output, "Status", "Symbol", "Strategy", "Variant", "TF", "Setup", "CScore", "Ev", "Sig%", "B/S", "Ret", "Val", "Allowed", "Blocked")
	for _, candidate := range candidates {
		table.AddRow(
			candidate.Status,
			candidate.Symbol,
			candidate.Strategy,
			candidate.ParamVariant,
			candidate.Timeframe,
			candidate.Setup,
			formatScoreOrDash(candidate.CandidateScore),
			formatScoreOrDash(candidate.EvidenceScore),
			fmt.Sprintf("%.2f%%", candidate.SignalRatePct),
			fmt.Sprintf("%d/%d", candidate.BuySignals, candidate.SellSignals),
			FormatPercent(candidate.ReturnPct),
			FormatPercent(candidate.ValidationReturnPct),
			strings.Join(candidate.AllowedRegimes, ","),
			strings.Join(candidate.BlockedRegimes, ","),
		)
	}
	table.Render()
}
