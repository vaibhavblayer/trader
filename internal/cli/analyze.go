// Package cli provides the command-line interface for the trading application.
package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// addAnalysisCommands adds analysis commands.
// Requirements: 4, 5, 6, 20, 35
func addAnalysisCommands(rootCmd *cobra.Command, app *App) {
	rootCmd.AddCommand(newAnalyzeCmd(app))
	rootCmd.AddCommand(newSignalCmd(app))
	rootCmd.AddCommand(newMTFCmd(app))
	rootCmd.AddCommand(newScanCmd(app))
	rootCmd.AddCommand(newResearchCmd(app))
}

func newAnalyzeCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analyze <symbol>",
		Short: "Full technical analysis for a symbol",
		Long: `Perform comprehensive technical analysis including:
- Trend indicators (SMA, EMA, MACD, ADX, SuperTrend)
- Momentum indicators (RSI, Stochastic, CCI)
- Volatility indicators (Bollinger Bands, ATR)
- Volume analysis (VWAP, OBV)
- Support/Resistance levels
- Chart patterns
- Candlestick patterns`,
		Example: `  trader analyze RELIANCE
  trader analyze INFY --timeframe 15min
  trader analyze TCS --detailed`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			symbol := strings.ToUpper(args[0])
			timeframe, _ := cmd.Flags().GetString("timeframe")
			detailed, _ := cmd.Flags().GetBool("detailed")

			output.Info("Analyzing %s on %s timeframe...", symbol, timeframe)
			output.Println()

			// This would perform actual analysis using the analysis package
			// For now, display placeholder structure
			analysis := AnalysisResult{
				Symbol:    symbol,
				Timeframe: timeframe,
				LTP:       2450.50,
				Trend: TrendAnalysis{
					Direction: "BULLISH",
					Strength:  "STRONG",
					ADX:       32.5,
					SMA20:     2420.30,
					SMA50:     2380.15,
					EMA20:     2435.20,
					MACD:      15.30,
					MACDSignal: 12.50,
					SuperTrend: 2380.00,
					SuperTrendDir: "UP",
				},
				Momentum: MomentumAnalysis{
					RSI:        62.5,
					RSISignal:  "NEUTRAL",
					StochK:     75.2,
					StochD:     70.8,
					StochSignal: "OVERBOUGHT",
					CCI:        85.3,
				},
				Volatility: VolatilityAnalysis{
					ATR:        45.30,
					ATRPercent: 1.85,
					BBUpper:    2520.50,
					BBMiddle:   2450.00,
					BBLower:    2379.50,
					BBWidth:    5.75,
				},
				Volume: VolumeAnalysis{
					Current:    1250000,
					Average:    980000,
					Ratio:      1.28,
					VWAP:       2445.80,
					OBVTrend:   "UP",
				},
				Levels: LevelAnalysis{
					NearestSupport:    2400.00,
					NearestResistance: 2500.00,
					PivotPoint:        2440.00,
					R1:                2480.00,
					R2:                2520.00,
					S1:                2400.00,
					S2:                2360.00,
				},
				Patterns: []PatternInfo{
					{Name: "Bullish Engulfing", Type: "Candlestick", Direction: "BULLISH", Strength: 0.75},
					{Name: "Ascending Triangle", Type: "Chart", Direction: "BULLISH", Strength: 0.65, Completion: 0.80},
				},
			}

			if output.IsJSON() {
				return output.JSON(analysis)
			}

			return displayAnalysis(output, analysis, detailed)
		},
	}

	cmd.Flags().StringP("timeframe", "t", "1day", "Timeframe for analysis")
	cmd.Flags().Bool("detailed", false, "Show detailed analysis")

	return cmd
}

// Analysis result structures
type AnalysisResult struct {
	Symbol     string
	Timeframe  string
	LTP        float64
	Trend      TrendAnalysis
	Momentum   MomentumAnalysis
	Volatility VolatilityAnalysis
	Volume     VolumeAnalysis
	Levels     LevelAnalysis
	Patterns   []PatternInfo
}

type TrendAnalysis struct {
	Direction     string
	Strength      string
	ADX           float64
	SMA20         float64
	SMA50         float64
	EMA20         float64
	MACD          float64
	MACDSignal    float64
	SuperTrend    float64
	SuperTrendDir string
}

type MomentumAnalysis struct {
	RSI         float64
	RSISignal   string
	StochK      float64
	StochD      float64
	StochSignal string
	CCI         float64
}

type VolatilityAnalysis struct {
	ATR        float64
	ATRPercent float64
	BBUpper    float64
	BBMiddle   float64
	BBLower    float64
	BBWidth    float64
}

type VolumeAnalysis struct {
	Current  int64
	Average  int64
	Ratio    float64
	VWAP     float64
	OBVTrend string
}

type LevelAnalysis struct {
	NearestSupport    float64
	NearestResistance float64
	PivotPoint        float64
	R1, R2, R3        float64
	S1, S2, S3        float64
}

type PatternInfo struct {
	Name       string
	Type       string
	Direction  string
	Strength   float64
	Completion float64
}

func displayAnalysis(output *Output, a AnalysisResult, detailed bool) error {
	// Header
	output.Bold("%s Technical Analysis", a.Symbol)
	output.Printf("  LTP: %s  Timeframe: %s\n", output.BoldText(FormatPrice(a.LTP)), a.Timeframe)
	output.Println()

	// Trend
	trendColor := ColorGreen
	if a.Trend.Direction == "BEARISH" {
		trendColor = ColorRed
	} else if a.Trend.Direction == "SIDEWAYS" {
		trendColor = ColorYellow
	}
	output.Bold("Trend")
	output.Printf("  Direction: %s  Strength: %s\n",
		output.ColoredString(trendColor, a.Trend.Direction),
		a.Trend.Strength)
	output.Printf("  ADX: %.1f  SuperTrend: %s %.2f\n",
		a.Trend.ADX,
		a.Trend.SuperTrendDir,
		a.Trend.SuperTrend)
	if detailed {
		output.Printf("  SMA(20): %.2f  SMA(50): %.2f  EMA(20): %.2f\n",
			a.Trend.SMA20, a.Trend.SMA50, a.Trend.EMA20)
		output.Printf("  MACD: %.2f  Signal: %.2f\n",
			a.Trend.MACD, a.Trend.MACDSignal)
	}
	output.Println()

	// Momentum
	output.Bold("Momentum")
	rsiColor := ColorYellow
	if a.Momentum.RSI > 70 {
		rsiColor = ColorRed
	} else if a.Momentum.RSI < 30 {
		rsiColor = ColorGreen
	}
	output.Printf("  RSI(14): %s (%s)\n",
		output.ColoredString(rsiColor, fmt.Sprintf("%.1f", a.Momentum.RSI)),
		a.Momentum.RSISignal)
	output.Printf("  Stochastic: %%K=%.1f %%D=%.1f (%s)\n",
		a.Momentum.StochK, a.Momentum.StochD, a.Momentum.StochSignal)
	if detailed {
		output.Printf("  CCI: %.1f\n", a.Momentum.CCI)
	}
	output.Println()

	// Volatility
	output.Bold("Volatility")
	output.Printf("  ATR: %.2f (%.2f%%)\n", a.Volatility.ATR, a.Volatility.ATRPercent)
	output.Printf("  Bollinger Bands: %.2f / %.2f / %.2f (Width: %.2f%%)\n",
		a.Volatility.BBLower, a.Volatility.BBMiddle, a.Volatility.BBUpper, a.Volatility.BBWidth)
	output.Println()

	// Volume
	output.Bold("Volume")
	volColor := ColorYellow
	if a.Volume.Ratio > 1.5 {
		volColor = ColorGreen
	} else if a.Volume.Ratio < 0.5 {
		volColor = ColorRed
	}
	output.Printf("  Current: %s  Avg: %s  Ratio: %s\n",
		FormatVolume(a.Volume.Current),
		FormatVolume(a.Volume.Average),
		output.ColoredString(volColor, fmt.Sprintf("%.2fx", a.Volume.Ratio)))
	output.Printf("  VWAP: %.2f  OBV Trend: %s\n", a.Volume.VWAP, a.Volume.OBVTrend)
	output.Println()

	// Levels
	output.Bold("Key Levels")
	output.Printf("  Support:    %s (%.2f%% away)\n",
		output.Green(FormatPrice(a.Levels.NearestSupport)),
		((a.LTP-a.Levels.NearestSupport)/a.LTP)*100)
	output.Printf("  Resistance: %s (%.2f%% away)\n",
		output.Red(FormatPrice(a.Levels.NearestResistance)),
		((a.Levels.NearestResistance-a.LTP)/a.LTP)*100)
	if detailed {
		output.Printf("  Pivot: %.2f  R1: %.2f  R2: %.2f  S1: %.2f  S2: %.2f\n",
			a.Levels.PivotPoint, a.Levels.R1, a.Levels.R2, a.Levels.S1, a.Levels.S2)
	}
	output.Println()

	// Patterns
	if len(a.Patterns) > 0 {
		output.Bold("Patterns Detected")
		for _, p := range a.Patterns {
			dirColor := ColorGreen
			if p.Direction == "BEARISH" {
				dirColor = ColorRed
			}
			strength := fmt.Sprintf("%.0f%%", p.Strength*100)
			if p.Completion > 0 {
				output.Printf("  %s %s (%s) - Strength: %s, Completion: %.0f%%\n",
					output.ColoredString(dirColor, "●"),
					p.Name, p.Type, strength, p.Completion*100)
			} else {
				output.Printf("  %s %s (%s) - Strength: %s\n",
					output.ColoredString(dirColor, "●"),
					p.Name, p.Type, strength)
			}
		}
	}

	return nil
}

func newSignalCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "signal <symbol>",
		Short: "Get composite signal score for a symbol",
		Long: `Calculate and display composite signal score combining multiple indicators.

Score ranges from -100 (strong sell) to +100 (strong buy).
Includes recommendation: STRONG BUY, BUY, WEAK BUY, NEUTRAL, WEAK SELL, SELL, STRONG SELL`,
		Example: `  trader signal RELIANCE
  trader signal INFY --timeframe 15min`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			symbol := strings.ToUpper(args[0])
			timeframe, _ := cmd.Flags().GetString("timeframe")

			// This would calculate actual signal score
			signal := SignalResult{
				Symbol:         symbol,
				Timeframe:      timeframe,
				Score:          65.5,
				Recommendation: "BUY",
				Components: map[string]float64{
					"RSI":        70.0,
					"MACD":       80.0,
					"Stochastic": 60.0,
					"SuperTrend": 100.0,
					"ADX":        50.0,
					"MA Cross":   40.0,
				},
				VolumeConfirm: true,
			}

			if output.IsJSON() {
				return output.JSON(signal)
			}

			return displaySignal(output, signal)
		},
	}

	cmd.Flags().StringP("timeframe", "t", "1day", "Timeframe for analysis")

	return cmd
}

type SignalResult struct {
	Symbol         string
	Timeframe      string
	Score          float64
	Recommendation string
	Components     map[string]float64
	VolumeConfirm  bool
}

func displaySignal(output *Output, s SignalResult) error {
	output.Bold("%s Signal Score", s.Symbol)
	output.Println()

	// Score visualization
	scoreColor := ColorYellow
	if s.Score > 50 {
		scoreColor = ColorGreen
	} else if s.Score < -50 {
		scoreColor = ColorRed
	}

	// Create score bar
	barWidth := 40
	normalized := (s.Score + 100) / 200 // 0 to 1
	pos := int(normalized * float64(barWidth))

	bar := strings.Repeat("░", pos) + "█" + strings.Repeat("░", barWidth-pos-1)
	output.Printf("  -100 [%s] +100\n", bar)
	output.Printf("  Score: %s\n", output.ColoredString(scoreColor, fmt.Sprintf("%.1f", s.Score)))
	output.Println()

	// Recommendation
	output.Printf("  Recommendation: %s\n", output.Recommendation(s.Recommendation))
	if s.VolumeConfirm {
		output.Printf("  Volume: %s Confirmed\n", output.Green("✓"))
	} else {
		output.Printf("  Volume: %s Not Confirmed\n", output.Yellow("⚠"))
	}
	output.Println()

	// Components
	output.Bold("Component Scores")
	for name, score := range s.Components {
		compColor := ColorYellow
		if score > 50 {
			compColor = ColorGreen
		} else if score < -50 {
			compColor = ColorRed
		}
		compBar := createBar(int(score+100), 200, 20)
		output.Printf("  %-12s %s %s\n", name, compBar, output.ColoredString(compColor, fmt.Sprintf("%+.0f", score)))
	}

	return nil
}

func newMTFCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mtf <symbol>",
		Short: "Multi-timeframe analysis",
		Long: `Analyze a symbol across multiple timeframes (5min, 15min, 1hour, 1day).

Shows trend alignment and confluence across timeframes.`,
		Example: `  trader mtf RELIANCE
  trader mtf INFY --detailed`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			symbol := strings.ToUpper(args[0])

			// This would perform actual MTF analysis
			mtf := MTFResult{
				Symbol: symbol,
				Timeframes: []TimeframeAnalysis{
					{Timeframe: "5min", Trend: "BULLISH", RSI: 58.5, MACD: "BULLISH", SuperTrend: "UP"},
					{Timeframe: "15min", Trend: "BULLISH", RSI: 62.3, MACD: "BULLISH", SuperTrend: "UP"},
					{Timeframe: "1hour", Trend: "BULLISH", RSI: 55.8, MACD: "NEUTRAL", SuperTrend: "UP"},
					{Timeframe: "1day", Trend: "BULLISH", RSI: 48.2, MACD: "BULLISH", SuperTrend: "UP"},
				},
				Confluence:  "HIGH",
				Alignment:   4,
				TotalFrames: 4,
			}

			if output.IsJSON() {
				return output.JSON(mtf)
			}

			return displayMTF(output, mtf)
		},
	}

	cmd.Flags().Bool("detailed", false, "Show detailed analysis per timeframe")

	return cmd
}

type MTFResult struct {
	Symbol      string
	Timeframes  []TimeframeAnalysis
	Confluence  string
	Alignment   int
	TotalFrames int
}

type TimeframeAnalysis struct {
	Timeframe  string
	Trend      string
	RSI        float64
	MACD       string
	SuperTrend string
}

func displayMTF(output *Output, mtf MTFResult) error {
	output.Bold("%s Multi-Timeframe Analysis", mtf.Symbol)
	output.Println()

	// Confluence summary
	confColor := ColorYellow
	if mtf.Confluence == "HIGH" {
		confColor = ColorGreen
	} else if mtf.Confluence == "LOW" {
		confColor = ColorRed
	}
	output.Printf("  Confluence: %s (%d/%d timeframes aligned)\n",
		output.ColoredString(confColor, mtf.Confluence),
		mtf.Alignment, mtf.TotalFrames)
	output.Println()

	// Timeframe table
	table := NewTable(output, "Timeframe", "Trend", "RSI", "MACD", "SuperTrend")
	for _, tf := range mtf.Timeframes {
		trendColor := ColorGreen
		if tf.Trend == "BEARISH" {
			trendColor = ColorRed
		} else if tf.Trend == "SIDEWAYS" {
			trendColor = ColorYellow
		}

		stColor := ColorGreen
		if tf.SuperTrend == "DOWN" {
			stColor = ColorRed
		}

		table.AddRow(
			tf.Timeframe,
			output.ColoredString(trendColor, tf.Trend),
			fmt.Sprintf("%.1f", tf.RSI),
			tf.MACD,
			output.ColoredString(stColor, tf.SuperTrend),
		)
	}
	table.Render()

	return nil
}

func newScanCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan stocks based on technical criteria",
		Long: `Scan stocks based on technical filters.

Supports filters for RSI, volume, gaps, and more.
Can use preset screeners or custom filters.`,
		Example: `  trader scan --rsi-below 30
  trader scan --volume-above 2 --gap-up 2
  trader scan --preset momentum
  trader scan --preset breakout`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			_ = ctx // Would be used for actual scanning

			preset, _ := cmd.Flags().GetString("preset")
			rsiBelow, _ := cmd.Flags().GetFloat64("rsi-below")
			rsiAbove, _ := cmd.Flags().GetFloat64("rsi-above")
			volumeAbove, _ := cmd.Flags().GetFloat64("volume-above")
			gapUp, _ := cmd.Flags().GetFloat64("gap-up")
			gapDown, _ := cmd.Flags().GetFloat64("gap-down")

			output.Info("Scanning stocks...")
			if preset != "" {
				output.Printf("  Preset: %s\n", preset)
			}
			if rsiBelow > 0 {
				output.Printf("  RSI < %.0f\n", rsiBelow)
			}
			if rsiAbove > 0 {
				output.Printf("  RSI > %.0f\n", rsiAbove)
			}
			if volumeAbove > 0 {
				output.Printf("  Volume > %.1fx avg\n", volumeAbove)
			}
			if gapUp > 0 {
				output.Printf("  Gap Up > %.1f%%\n", gapUp)
			}
			if gapDown > 0 {
				output.Printf("  Gap Down > %.1f%%\n", gapDown)
			}
			output.Println()

			// This would perform actual scanning
			results := []ScanResult{
				{Symbol: "RELIANCE", LTP: 2450.50, Change: 2.5, RSI: 28.5, Volume: 2.3, Signal: "OVERSOLD"},
				{Symbol: "INFY", LTP: 1520.30, Change: 3.2, RSI: 25.8, Volume: 1.8, Signal: "OVERSOLD"},
				{Symbol: "TCS", LTP: 3450.00, Change: 1.8, RSI: 32.1, Volume: 1.5, Signal: "NEUTRAL"},
			}

			if output.IsJSON() {
				return output.JSON(results)
			}

			return displayScanResults(output, results)
		},
	}

	cmd.Flags().String("preset", "", "Use preset screener (momentum, value, breakout, reversal)")
	cmd.Flags().Float64("rsi-below", 0, "RSI below threshold")
	cmd.Flags().Float64("rsi-above", 0, "RSI above threshold")
	cmd.Flags().Float64("volume-above", 0, "Volume multiple above average")
	cmd.Flags().Float64("gap-up", 0, "Gap up percentage")
	cmd.Flags().Float64("gap-down", 0, "Gap down percentage")
	cmd.Flags().String("watchlist", "", "Scan specific watchlist")

	return cmd
}

type ScanResult struct {
	Symbol string
	LTP    float64
	Change float64
	RSI    float64
	Volume float64
	Signal string
}

func displayScanResults(output *Output, results []ScanResult) error {
	output.Bold("Scan Results")
	output.Printf("  Found %d stocks\n\n", len(results))

	table := NewTable(output, "Symbol", "LTP", "Change", "RSI", "Volume", "Signal")
	for _, r := range results {
		table.AddRow(
			r.Symbol,
			FormatPrice(r.LTP),
			output.ColoredString(output.PnLColor(r.Change), FormatPercent(r.Change)),
			fmt.Sprintf("%.1f", r.RSI),
			fmt.Sprintf("%.1fx", r.Volume),
			r.Signal,
		)
	}
	table.Render()

	output.Println()
	output.Dim("Tip: Use 'trader analyze <symbol>' for detailed analysis")

	return nil
}

func newResearchCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "research <symbol>",
		Short: "AI-powered research report",
		Long: `Generate AI-powered research report including:
- Company fundamentals
- Sector analysis
- News sentiment
- Analyst ratings
- Price targets`,
		Example: `  trader research RELIANCE
  trader research INFY --detailed`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			symbol := strings.ToUpper(args[0])

			output.Info("Generating research report for %s...", symbol)
			output.Println()

			// This would generate actual research using AI agents
			research := ResearchResult{
				Symbol:        symbol,
				CompanyName:   "Reliance Industries Ltd",
				Sector:        "Energy",
				Industry:      "Oil & Gas Refining",
				MarketCap:     1850000,
				PE:            25.5,
				PB:            2.8,
				ROE:           12.5,
				DebtToEquity:  0.45,
				DividendYield: 0.35,
				AnalystRating: "BUY",
				AvgTarget:     2650.00,
				Highlights: []string{
					"Strong retail and digital business growth",
					"Expanding renewable energy portfolio",
					"Consistent cash flow generation",
				},
				Risks: []string{
					"Oil price volatility",
					"Regulatory changes in telecom",
					"High capex requirements",
				},
			}

			if output.IsJSON() {
				return output.JSON(research)
			}

			return displayResearch(output, research)
		},
	}

	cmd.Flags().Bool("detailed", false, "Show detailed research")

	return cmd
}

type ResearchResult struct {
	Symbol        string
	CompanyName   string
	Sector        string
	Industry      string
	MarketCap     float64
	PE            float64
	PB            float64
	ROE           float64
	DebtToEquity  float64
	DividendYield float64
	AnalystRating string
	AvgTarget     float64
	Highlights    []string
	Risks         []string
}

func displayResearch(output *Output, r ResearchResult) error {
	output.Bold("%s - %s", r.Symbol, r.CompanyName)
	output.Printf("  %s > %s\n", r.Sector, r.Industry)
	output.Println()

	output.Bold("Fundamentals")
	output.Printf("  Market Cap:    %s\n", FormatCompact(r.MarketCap*10000000))
	output.Printf("  P/E Ratio:     %.2f\n", r.PE)
	output.Printf("  P/B Ratio:     %.2f\n", r.PB)
	output.Printf("  ROE:           %.2f%%\n", r.ROE)
	output.Printf("  Debt/Equity:   %.2f\n", r.DebtToEquity)
	output.Printf("  Div Yield:     %.2f%%\n", r.DividendYield)
	output.Println()

	output.Bold("Analyst View")
	ratingColor := ColorYellow
	if r.AnalystRating == "BUY" || r.AnalystRating == "STRONG BUY" {
		ratingColor = ColorGreen
	} else if r.AnalystRating == "SELL" || r.AnalystRating == "STRONG SELL" {
		ratingColor = ColorRed
	}
	output.Printf("  Rating:        %s\n", output.ColoredString(ratingColor, r.AnalystRating))
	output.Printf("  Avg Target:    %s\n", FormatIndianCurrency(r.AvgTarget))
	output.Println()

	output.Bold("Key Highlights")
	for _, h := range r.Highlights {
		output.Printf("  %s %s\n", output.Green("✓"), h)
	}
	output.Println()

	output.Bold("Key Risks")
	for _, risk := range r.Risks {
		output.Printf("  %s %s\n", output.Yellow("⚠"), risk)
	}

	return nil
}
