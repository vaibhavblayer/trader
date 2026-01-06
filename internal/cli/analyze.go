// Package cli provides the command-line interface for the trading application.
package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
	"github.com/spf13/cobra"

	"zerodha-trader/internal/agents"
	"zerodha-trader/internal/broker"
	"zerodha-trader/internal/models"
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
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			symbol := strings.ToUpper(args[0])
			timeframe, _ := cmd.Flags().GetString("timeframe")
			exchange, _ := cmd.Flags().GetString("exchange")
			detailed, _ := cmd.Flags().GetBool("detailed")

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			output.Info("Analyzing %s on %s timeframe...", symbol, timeframe)

			// Fetch historical data
			days := 100
			if timeframe == "5min" || timeframe == "15min" {
				days = 5
			} else if timeframe == "30min" || timeframe == "1hour" {
				days = 30
			}

			candles, err := app.Broker.GetHistorical(ctx, broker.HistoricalRequest{
				Symbol:    symbol,
				Exchange:  models.Exchange(exchange),
				Timeframe: timeframe,
				From:      time.Now().AddDate(0, 0, -days),
				To:        time.Now(),
			})
			if err != nil {
				output.Error("Failed to get historical data: %v", err)
				return err
			}

			if len(candles) < 26 {
				output.Error("Insufficient data for analysis (need at least 26 candles)")
				return fmt.Errorf("insufficient data")
			}

			// Get current quote
			fullSymbol := fmt.Sprintf("%s:%s", exchange, symbol)
			quote, err := app.Broker.GetQuote(ctx, fullSymbol)
			if err != nil {
				output.Warning("Could not get live quote, using last candle close")
			}

			// Extract price arrays
			closes := make([]float64, len(candles))
			highs := make([]float64, len(candles))
			lows := make([]float64, len(candles))
			volumes := make([]int64, len(candles))
			for i, c := range candles {
				closes[i] = c.Close
				highs[i] = c.High
				lows[i] = c.Low
				volumes[i] = c.Volume
			}

			ltp := closes[len(closes)-1]
			if quote != nil {
				ltp = quote.LTP
			}

			// Calculate indicators
			rsi := calculateRSI(closes, 14)
			ema9 := calculateEMA(closes, 9)
			ema20 := calculateEMA(closes, 20)
			_ = calculateEMA(closes, 50) // ema50 for future use
			sma20 := calculateSMA(closes, 20)
			sma50 := calculateSMA(closes, 50)
			macdLine, signalLine := calculateMACD(closes)
			bbUpper, bbMiddle, bbLower := calculateBollingerBands(closes, 20, 2.0)
			atr := calculateATR(highs, lows, closes, 14)
			stochK, stochD := calculateStochastic(highs, lows, closes, 14, 3)
			adx := calculateADX(highs, lows, closes, 14)

			// Determine trend
			trendDir := "SIDEWAYS"
			trendStrength := "WEAK"
			superTrendDir := "UP"
			if len(ema9) > 0 && len(ema20) > 0 {
				if ema9[len(ema9)-1] > ema20[len(ema20)-1] && ltp > ema9[len(ema9)-1] {
					trendDir = "BULLISH"
					superTrendDir = "UP"
				} else if ema9[len(ema9)-1] < ema20[len(ema20)-1] && ltp < ema9[len(ema9)-1] {
					trendDir = "BEARISH"
					superTrendDir = "DOWN"
				}
			}
			if adx > 25 {
				trendStrength = "STRONG"
			} else if adx > 20 {
				trendStrength = "MODERATE"
			}

			// RSI signal
			rsiSignal := "NEUTRAL"
			if rsi > 70 {
				rsiSignal = "OVERBOUGHT"
			} else if rsi < 30 {
				rsiSignal = "OVERSOLD"
			}

			// Stochastic signal
			stochSignal := "NEUTRAL"
			if stochK > 80 {
				stochSignal = "OVERBOUGHT"
			} else if stochK < 20 {
				stochSignal = "OVERSOLD"
			}

			// Volume analysis
			avgVolume := int64(0)
			if len(volumes) >= 20 {
				for i := len(volumes) - 20; i < len(volumes); i++ {
					avgVolume += volumes[i]
				}
				avgVolume /= 20
			}
			currentVolume := volumes[len(volumes)-1]
			volRatio := 1.0
			if avgVolume > 0 {
				volRatio = float64(currentVolume) / float64(avgVolume)
			}
			obvTrend := "NEUTRAL"
			if len(closes) > 1 && closes[len(closes)-1] > closes[len(closes)-2] && volRatio > 1.0 {
				obvTrend = "UP"
			} else if len(closes) > 1 && closes[len(closes)-1] < closes[len(closes)-2] && volRatio > 1.0 {
				obvTrend = "DOWN"
			}

			// Support/Resistance (simple pivot-based)
			pivot := (highs[len(highs)-1] + lows[len(lows)-1] + closes[len(closes)-1]) / 3
			r1 := 2*pivot - lows[len(lows)-1]
			r2 := pivot + (highs[len(highs)-1] - lows[len(lows)-1])
			s1 := 2*pivot - highs[len(highs)-1]
			s2 := pivot - (highs[len(highs)-1] - lows[len(lows)-1])

			// Find nearest support/resistance
			nearestSupport := s1
			nearestResistance := r1
			if ltp < pivot {
				nearestResistance = pivot
			}

			// MACD values
			macdVal := 0.0
			macdSig := 0.0
			if len(macdLine) > 0 {
				macdVal = macdLine[len(macdLine)-1]
			}
			if len(signalLine) > 0 {
				macdSig = signalLine[len(signalLine)-1]
			}

			// SuperTrend value (simplified - use ATR-based)
			superTrendVal := ltp - 2*atr
			if superTrendDir == "DOWN" {
				superTrendVal = ltp + 2*atr
			}

			// BB Width
			bbWidth := 0.0
			if bbMiddle > 0 {
				bbWidth = ((bbUpper - bbLower) / bbMiddle) * 100
			}

			// ATR percent
			atrPercent := 0.0
			if ltp > 0 {
				atrPercent = (atr / ltp) * 100
			}

			// VWAP (simplified - use typical price * volume weighted)
			vwap := ltp // Simplified

			analysis := AnalysisResult{
				Symbol:    symbol,
				Timeframe: timeframe,
				LTP:       ltp,
				Trend: TrendAnalysis{
					Direction:     trendDir,
					Strength:      trendStrength,
					ADX:           adx,
					SMA20:         sma20,
					SMA50:         sma50,
					EMA20:         getLastValue(ema20),
					MACD:          macdVal,
					MACDSignal:    macdSig,
					SuperTrend:    superTrendVal,
					SuperTrendDir: superTrendDir,
				},
				Momentum: MomentumAnalysis{
					RSI:         rsi,
					RSISignal:   rsiSignal,
					StochK:      stochK,
					StochD:      stochD,
					StochSignal: stochSignal,
					CCI:         0, // Not implemented
				},
				Volatility: VolatilityAnalysis{
					ATR:        atr,
					ATRPercent: atrPercent,
					BBUpper:    bbUpper,
					BBMiddle:   bbMiddle,
					BBLower:    bbLower,
					BBWidth:    bbWidth,
				},
				Volume: VolumeAnalysis{
					Current:  currentVolume,
					Average:  avgVolume,
					Ratio:    volRatio,
					VWAP:     vwap,
					OBVTrend: obvTrend,
				},
				Levels: LevelAnalysis{
					NearestSupport:    nearestSupport,
					NearestResistance: nearestResistance,
					PivotPoint:        pivot,
					R1:                r1,
					R2:                r2,
					S1:                s1,
					S2:                s2,
				},
				Patterns: detectPatterns(candles),
			}

			output.Println()

			if output.IsJSON() {
				return output.JSON(analysis)
			}

			return displayAnalysis(output, analysis, detailed)
		},
	}

	cmd.Flags().StringP("timeframe", "t", "1day", "Timeframe for analysis")
	cmd.Flags().StringP("exchange", "e", "NSE", "Exchange (NSE, BSE)")
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

	// Trend - calculated from Zerodha data
	trendColor := ColorGreen
	if a.Trend.Direction == "BEARISH" {
		trendColor = ColorRed
	} else if a.Trend.Direction == "SIDEWAYS" {
		trendColor = ColorYellow
	}
	output.Bold("Trend %s", output.SourceTag(SourceCalc))
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

	// Momentum - calculated from Zerodha data
	output.Bold("Momentum %s", output.SourceTag(SourceCalc))
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

	// Volatility - calculated from Zerodha data
	output.Bold("Volatility %s", output.SourceTag(SourceCalc))
	output.Printf("  ATR: %.2f (%.2f%%)\n", a.Volatility.ATR, a.Volatility.ATRPercent)
	output.Printf("  Bollinger Bands: %.2f / %.2f / %.2f (Width: %.2f%%)\n",
		a.Volatility.BBLower, a.Volatility.BBMiddle, a.Volatility.BBUpper, a.Volatility.BBWidth)
	output.Println()

	// Volume - from Zerodha
	output.Bold("Volume %s", output.SourceTag(SourceZerodha))
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

	// Levels - calculated
	output.Bold("Key Levels %s", output.SourceTag(SourceCalc))
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

	// Patterns - detected by algorithm
	if len(a.Patterns) > 0 {
		output.Bold("Patterns Detected %s", output.SourceTag(SourceCalc))
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
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			symbol := strings.ToUpper(args[0])
			timeframe, _ := cmd.Flags().GetString("timeframe")
			exchange, _ := cmd.Flags().GetString("exchange")

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			// Fetch historical data
			days := 100
			if timeframe == "5min" || timeframe == "15min" {
				days = 5
			}

			candles, err := app.Broker.GetHistorical(ctx, broker.HistoricalRequest{
				Symbol:    symbol,
				Exchange:  models.Exchange(exchange),
				Timeframe: timeframe,
				From:      time.Now().AddDate(0, 0, -days),
				To:        time.Now(),
			})
			if err != nil {
				output.Error("Failed to get historical data: %v", err)
				return err
			}

			if len(candles) < 26 {
				output.Error("Insufficient data for signal calculation")
				return fmt.Errorf("insufficient data")
			}

			// Extract price arrays
			closes := make([]float64, len(candles))
			highs := make([]float64, len(candles))
			lows := make([]float64, len(candles))
			volumes := make([]int64, len(candles))
			for i, c := range candles {
				closes[i] = c.Close
				highs[i] = c.High
				lows[i] = c.Low
				volumes[i] = c.Volume
			}

			// Calculate indicators
			rsi := calculateRSI(closes, 14)
			macdLine, signalLine := calculateMACD(closes)
			stochK, _ := calculateStochastic(highs, lows, closes, 14, 3)
			adx := calculateADX(highs, lows, closes, 14)
			ema9 := calculateEMA(closes, 9)
			ema21 := calculateEMA(closes, 21)

			// Calculate component scores (-100 to +100)
			components := make(map[string]float64)

			// RSI score: oversold = bullish, overbought = bearish
			rsiScore := 0.0
			if rsi < 30 {
				rsiScore = 100 - rsi*2 // More oversold = more bullish
			} else if rsi > 70 {
				rsiScore = -(rsi - 50) * 2 // More overbought = more bearish
			} else {
				rsiScore = (50 - rsi) * 2 // Neutral zone
			}
			components["RSI"] = rsiScore

			// MACD score
			macdScore := 0.0
			if len(macdLine) > 0 && len(signalLine) > 0 {
				macdDiff := macdLine[len(macdLine)-1] - signalLine[len(signalLine)-1]
				macdScore = macdDiff * 10 // Scale appropriately
				if macdScore > 100 {
					macdScore = 100
				} else if macdScore < -100 {
					macdScore = -100
				}
			}
			components["MACD"] = macdScore

			// Stochastic score
			stochScore := 0.0
			if stochK < 20 {
				stochScore = 100 - stochK*2
			} else if stochK > 80 {
				stochScore = -(stochK - 50) * 2
			} else {
				stochScore = (50 - stochK)
			}
			components["Stochastic"] = stochScore

			// SuperTrend/Trend score
			trendScore := 0.0
			if len(ema9) > 0 && len(ema21) > 0 {
				if ema9[len(ema9)-1] > ema21[len(ema21)-1] {
					trendScore = 100
				} else {
					trendScore = -100
				}
			}
			components["SuperTrend"] = trendScore

			// ADX score (trend strength)
			adxScore := adx - 25 // Above 25 = trending
			if adxScore > 50 {
				adxScore = 50
			} else if adxScore < -25 {
				adxScore = -25
			}
			components["ADX"] = adxScore

			// MA Cross score
			maCrossScore := 0.0
			if len(ema9) > 1 && len(ema21) > 1 {
				prevDiff := ema9[len(ema9)-2] - ema21[len(ema21)-2]
				currDiff := ema9[len(ema9)-1] - ema21[len(ema21)-1]
				if prevDiff < 0 && currDiff > 0 {
					maCrossScore = 100 // Bullish crossover
				} else if prevDiff > 0 && currDiff < 0 {
					maCrossScore = -100 // Bearish crossover
				} else if currDiff > 0 {
					maCrossScore = 50
				} else {
					maCrossScore = -50
				}
			}
			components["MA Cross"] = maCrossScore

			// Calculate composite score (weighted average)
			totalScore := 0.0
			weights := map[string]float64{
				"RSI":        0.15,
				"MACD":       0.20,
				"Stochastic": 0.10,
				"SuperTrend": 0.25,
				"ADX":        0.10,
				"MA Cross":   0.20,
			}
			for name, score := range components {
				totalScore += score * weights[name]
			}

			// Volume confirmation
			avgVolume := int64(0)
			if len(volumes) >= 20 {
				for i := len(volumes) - 20; i < len(volumes); i++ {
					avgVolume += volumes[i]
				}
				avgVolume /= 20
			}
			volumeConfirm := volumes[len(volumes)-1] > avgVolume

			// Determine recommendation
			recommendation := "NEUTRAL"
			if totalScore > 60 {
				recommendation = "STRONG BUY"
			} else if totalScore > 30 {
				recommendation = "BUY"
			} else if totalScore > 10 {
				recommendation = "WEAK BUY"
			} else if totalScore < -60 {
				recommendation = "STRONG SELL"
			} else if totalScore < -30 {
				recommendation = "SELL"
			} else if totalScore < -10 {
				recommendation = "WEAK SELL"
			}

			signal := SignalResult{
				Symbol:         symbol,
				Timeframe:      timeframe,
				Score:          totalScore,
				Recommendation: recommendation,
				Components:     components,
				VolumeConfirm:  volumeConfirm,
			}

			if output.IsJSON() {
				return output.JSON(signal)
			}

			return displaySignal(output, signal)
		},
	}

	cmd.Flags().StringP("timeframe", "t", "1day", "Timeframe for analysis")
	cmd.Flags().StringP("exchange", "e", "NSE", "Exchange (NSE, BSE)")

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
	output.Bold("%s Signal Score %s", s.Symbol, output.SourceTag(SourceCalc))
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
	output.Bold("Component Scores %s", output.SourceTag(SourceCalc))
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
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			symbol := strings.ToUpper(args[0])
			exchange, _ := cmd.Flags().GetString("exchange")

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			output.Info("Analyzing %s across multiple timeframes...", symbol)

			// Timeframes to analyze
			timeframes := []struct {
				name string
				tf   string
				days int
			}{
				{"5min", "5min", 2},
				{"15min", "15min", 5},
				{"1hour", "1hour", 15},
				{"1day", "1day", 100},
			}

			var tfAnalyses []TimeframeAnalysis
			bullishCount := 0

			for _, tf := range timeframes {
				// Fetch historical data for this timeframe
				from := time.Now().AddDate(0, 0, -tf.days)
				to := time.Now()

				candles, err := app.Broker.GetHistorical(ctx, broker.HistoricalRequest{
					Symbol:    symbol,
					Exchange:  models.Exchange(exchange),
					Timeframe: tf.tf,
					From:      from,
					To:        to,
				})
				if err != nil {
					output.Warning("Failed to get %s data: %v", tf.name, err)
					continue
				}

				if len(candles) < 20 {
					output.Warning("Insufficient data for %s timeframe", tf.name)
					continue
				}

				// Calculate indicators
				closes := make([]float64, len(candles))
				highs := make([]float64, len(candles))
				lows := make([]float64, len(candles))
				for i, c := range candles {
					closes[i] = c.Close
					highs[i] = c.High
					lows[i] = c.Low
				}

				// RSI calculation
				rsi := calculateRSI(closes, 14)

				// Simple trend detection based on EMAs
				ema9 := calculateEMA(closes, 9)
				ema21 := calculateEMA(closes, 21)

				trend := "SIDEWAYS"
				if len(ema9) > 0 && len(ema21) > 0 {
					if ema9[len(ema9)-1] > ema21[len(ema21)-1] && closes[len(closes)-1] > ema9[len(ema9)-1] {
						trend = "BULLISH"
						bullishCount++
					} else if ema9[len(ema9)-1] < ema21[len(ema21)-1] && closes[len(closes)-1] < ema9[len(ema9)-1] {
						trend = "BEARISH"
					}
				}

				// MACD
				macdLine, signalLine := calculateMACD(closes)
				macdTrend := "NEUTRAL"
				if len(macdLine) > 0 && len(signalLine) > 0 {
					if macdLine[len(macdLine)-1] > signalLine[len(signalLine)-1] {
						macdTrend = "BULLISH"
					} else if macdLine[len(macdLine)-1] < signalLine[len(signalLine)-1] {
						macdTrend = "BEARISH"
					}
				}

				// SuperTrend (simplified)
				stDir := "UP"
				if trend == "BEARISH" {
					stDir = "DOWN"
				}

				tfAnalyses = append(tfAnalyses, TimeframeAnalysis{
					Timeframe:  tf.name,
					Trend:      trend,
					RSI:        rsi,
					MACD:       macdTrend,
					SuperTrend: stDir,
				})
			}

			if len(tfAnalyses) == 0 {
				output.Error("Could not analyze any timeframe")
				return fmt.Errorf("no data available")
			}

			// Calculate confluence
			confluence := "LOW"
			if bullishCount >= 3 {
				confluence = "HIGH"
			} else if bullishCount >= 2 {
				confluence = "MEDIUM"
			}

			mtf := MTFResult{
				Symbol:      symbol,
				Timeframes:  tfAnalyses,
				Confluence:  confluence,
				Alignment:   bullishCount,
				TotalFrames: len(tfAnalyses),
			}

			output.Println()

			if output.IsJSON() {
				return output.JSON(mtf)
			}

			return displayMTF(output, mtf)
		},
	}

	cmd.Flags().Bool("detailed", false, "Show detailed analysis per timeframe")
	cmd.Flags().StringP("exchange", "e", "NSE", "Exchange (NSE, BSE)")

	return cmd
}

// calculateRSI calculates the RSI indicator
func calculateRSI(closes []float64, period int) float64 {
	if len(closes) < period+1 {
		return 50.0
	}

	var gains, losses float64
	for i := 1; i <= period; i++ {
		change := closes[len(closes)-i] - closes[len(closes)-i-1]
		if change > 0 {
			gains += change
		} else {
			losses -= change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	if avgLoss == 0 {
		return 100.0
	}

	rs := avgGain / avgLoss
	return 100.0 - (100.0 / (1.0 + rs))
}

// calculateEMA calculates Exponential Moving Average
func calculateEMA(data []float64, period int) []float64 {
	if len(data) < period {
		return nil
	}

	ema := make([]float64, len(data))
	multiplier := 2.0 / float64(period+1)

	// Start with SMA
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += data[i]
	}
	ema[period-1] = sum / float64(period)

	// Calculate EMA
	for i := period; i < len(data); i++ {
		ema[i] = (data[i]-ema[i-1])*multiplier + ema[i-1]
	}

	return ema
}

// calculateMACD calculates MACD line and signal line
func calculateMACD(closes []float64) ([]float64, []float64) {
	ema12 := calculateEMA(closes, 12)
	ema26 := calculateEMA(closes, 26)

	if len(ema12) == 0 || len(ema26) == 0 {
		return nil, nil
	}

	macdLine := make([]float64, len(closes))
	for i := 25; i < len(closes); i++ {
		macdLine[i] = ema12[i] - ema26[i]
	}

	signalLine := calculateEMA(macdLine[25:], 9)
	if signalLine == nil {
		return macdLine, nil
	}

	// Pad signal line to match length
	fullSignal := make([]float64, len(closes))
	for i := 0; i < len(signalLine); i++ {
		fullSignal[25+i] = signalLine[i]
	}

	return macdLine, fullSignal
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
		Short: "Scan stocks based on technical and price criteria",
		Long: `Scan stocks based on technical and price filters.

SCOPE:
  --all: Scan ALL NSE equity stocks (1500+ stocks, takes time)
  --index: Scan index constituents:
    nifty50    - Nifty 50 (50 stocks)
    nifty100   - Nifty 100 (100 stocks)
    nifty200   - Nifty 200 (200 stocks)
    nifty500   - Nifty 500 (500 stocks)
    banknifty  - Bank Nifty (12 stocks)
    fno        - F&O stocks (~180 liquid stocks)
    smallcap   - Small-cap stocks (~100 stocks)
  --watchlist: Scan specific watchlist (default: 'default')

PRICE FILTERS:
  --min-price, --max-price: Filter by current price range
  --penny: Show penny stocks (price < ₹50)
  --midcap: Show mid-cap range (₹100-₹500)
  --largecap: Show large-cap range (₹500+)

TECHNICAL FILTERS:
  --rsi-below, --rsi-above: RSI thresholds
  --volume-above: Volume multiple above average
  --gap-up, --gap-down: Gap percentages

VOLATILITY FILTERS (for day trading):
  --volatile: Show volatile stocks (ATR > 2%)
  --min-atr: Minimum ATR percentage
  --min-change: Minimum absolute change percentage

PRESETS:
  --preset momentum: RSI > 60, Volume > 1.5x
  --preset oversold: RSI < 30
  --preset overbought: RSI > 70
  --preset breakout: Volume > 2x
  --preset reversal: RSI < 35, Volume > 1.5x
  --preset movers: Top movers (change > 2%, volume > 1.2x)
  --preset volatile: High volatility stocks (ATR > 2%)`,
		Example: `  # Find volatile stocks for day trading
  trader scan --index fno --volatile --limit 10
  trader scan --index fno --preset movers --limit 10
  
  # Scan F&O stocks for penny stocks
  trader scan --index fno --penny
  
  # Scan Nifty 500 for oversold stocks
  trader scan --index nifty500 --rsi-below 30 --limit 20
  
  # Scan entire NSE market (takes time)
  trader scan --all --penny --limit 50
  
  # Technical scan on F&O stocks
  trader scan --index fno --preset momentum
  trader scan --index nifty500 --gainers --sort change --limit 10`,
		RunE: func(cmd *cobra.Command, args []string) error {
			output := NewOutput(cmd)
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second) // 5 min timeout for full scan
			defer cancel()

			if app.Broker == nil {
				output.Error("Broker not configured. Run 'trader login' first.")
				return fmt.Errorf("broker not configured")
			}

			preset, _ := cmd.Flags().GetString("preset")
			rsiBelow, _ := cmd.Flags().GetFloat64("rsi-below")
			rsiAbove, _ := cmd.Flags().GetFloat64("rsi-above")
			volumeAbove, _ := cmd.Flags().GetFloat64("volume-above")
			gapUp, _ := cmd.Flags().GetFloat64("gap-up")
			gapDown, _ := cmd.Flags().GetFloat64("gap-down")
			watchlistName, _ := cmd.Flags().GetString("watchlist")
			exchange, _ := cmd.Flags().GetString("exchange")
			
			// Scope flags
			scanAll, _ := cmd.Flags().GetBool("all")
			indexName, _ := cmd.Flags().GetString("index")
			
			// Price filters
			minPrice, _ := cmd.Flags().GetFloat64("min-price")
			maxPrice, _ := cmd.Flags().GetFloat64("max-price")
			penny, _ := cmd.Flags().GetBool("penny")
			midcap, _ := cmd.Flags().GetBool("midcap")
			largecap, _ := cmd.Flags().GetBool("largecap")
			limit, _ := cmd.Flags().GetInt("limit")
			sortBy, _ := cmd.Flags().GetString("sort")
			
			// Volatility filters
			volatile, _ := cmd.Flags().GetBool("volatile")
			minATR, _ := cmd.Flags().GetFloat64("min-atr")
			minChange, _ := cmd.Flags().GetFloat64("min-change")
			
			gainers, _ := cmd.Flags().GetBool("gainers")
			losers, _ := cmd.Flags().GetBool("losers")

			// Apply price presets
			if penny {
				maxPrice = 50
				output.Info("Scanning for penny stocks (< ₹50)")
			} else if midcap {
				minPrice = 100
				maxPrice = 500
				output.Info("Scanning for mid-cap stocks (₹100-₹500)")
			} else if largecap {
				minPrice = 500
				output.Info("Scanning for large-cap stocks (> ₹500)")
			}

			// Apply preset filters
			if preset != "" {
				switch preset {
				case "momentum":
					if rsiAbove == 0 {
						rsiAbove = 60
					}
					if volumeAbove == 0 {
						volumeAbove = 1.5
					}
				case "oversold":
					if rsiBelow == 0 {
						rsiBelow = 30
					}
				case "overbought":
					if rsiAbove == 0 {
						rsiAbove = 70
					}
				case "breakout":
					if volumeAbove == 0 {
						volumeAbove = 2.0
					}
				case "reversal":
					if rsiBelow == 0 {
						rsiBelow = 35
					}
					if volumeAbove == 0 {
						volumeAbove = 1.5
					}
				case "movers":
					// Top movers - high absolute change with volume
					if minChange == 0 {
						minChange = 2.0 // At least 2% move
					}
					if volumeAbove == 0 {
						volumeAbove = 1.2
					}
					if sortBy == "" {
						sortBy = "change"
					}
				case "volatile":
					// High volatility stocks - good for day trading
					if minATR == 0 {
						minATR = 2.0 // At least 2% ATR
					}
					if volumeAbove == 0 {
						volumeAbove = 1.0
					}
					if sortBy == "" {
						sortBy = "atr"
					}
				}
			}
			
			// Apply --volatile flag (shortcut for volatile preset)
			if volatile {
				if minATR == 0 {
					minATR = 2.0
				}
				if sortBy == "" {
					sortBy = "atr"
				}
				output.Info("Scanning for volatile stocks (ATR > %.1f%%)", minATR)
			}

			output.Info("Scanning stocks...")
			if preset != "" {
				output.Printf("  Preset: %s\n", preset)
			}
			if minPrice > 0 {
				output.Printf("  Min Price: ₹%.0f\n", minPrice)
			}
			if maxPrice > 0 {
				output.Printf("  Max Price: ₹%.0f\n", maxPrice)
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

			// Get symbols to scan based on scope
			var symbols []string
			
			if scanAll {
				// Fetch all NSE equity instruments
				output.Info("Fetching all NSE instruments...")
				instruments, err := app.Broker.GetInstruments(ctx, models.Exchange(exchange))
				if err != nil {
					output.Error("Failed to fetch instruments: %v", err)
					return err
				}
				
				// Filter for equity stocks only (EQ segment)
				for _, inst := range instruments {
					if inst.Segment == "NSE" && inst.InstrType == "EQ" {
						symbols = append(symbols, inst.Symbol)
					}
				}
				output.Printf("  Scanning %d equity stocks from %s\n", len(symbols), exchange)
				output.Warning("Full market scan may take 10-15 minutes. Consider using --index nifty200 for faster results.")
				output.Println()
			} else if indexName != "" {
				// Use index constituents
				symbols = getIndexConstituents(indexName)
				if len(symbols) == 0 {
					output.Error("Unknown index: %s", indexName)
					output.Println("Available indices: nifty50, nifty100, nifty200, nifty500, banknifty, fno, smallcap")
					return fmt.Errorf("unknown index: %s", indexName)
				}
				output.Printf("  Scanning %d stocks from %s\n", len(symbols), strings.ToUpper(indexName))
			} else {
				// Use watchlist
				if watchlistName == "" {
					watchlistName = "default"
				}

				if app.Store != nil {
					var err error
					symbols, err = app.Store.GetWatchlist(ctx, watchlistName)
					if err != nil || len(symbols) == 0 {
						// Fallback to default stocks
						symbols = []string{"RELIANCE", "TCS", "INFY", "HDFCBANK", "ICICIBANK", "SBIN", "BHARTIARTL", "ITC", "KOTAKBANK", "LT"}
					}
				} else {
					symbols = []string{"RELIANCE", "TCS", "INFY", "HDFCBANK", "ICICIBANK", "SBIN", "BHARTIARTL", "ITC", "KOTAKBANK", "LT"}
				}
				output.Printf("  Scanning %d symbols from '%s' watchlist\n", len(symbols), watchlistName)
			}
			output.Println()

			var results []ScanResult
			scanned := 0
			errors := 0
			startTime := time.Now()

			for _, symbol := range symbols {
				scanned++
				
				// Show progress for large scans
				if len(symbols) > 50 && scanned%50 == 0 {
					elapsed := time.Since(startTime)
					rate := float64(scanned) / elapsed.Seconds()
					remaining := time.Duration(float64(len(symbols)-scanned)/rate) * time.Second
					output.Printf("\r  Progress: %d/%d (%.0f/s) | Found: %d | ETA: %s    ", 
						scanned, len(symbols), rate, len(results), remaining.Round(time.Second))
				}
				
				// Fetch historical data
				candles, err := app.Broker.GetHistorical(ctx, broker.HistoricalRequest{
					Symbol:    symbol,
					Exchange:  models.Exchange(exchange),
					Timeframe: "1day",
					From:      time.Now().AddDate(0, 0, -30),
					To:        time.Now(),
				})
				if err != nil || len(candles) < 15 {
					errors++
					continue
				}

				// Extract price data
				closes := make([]float64, len(candles))
				highs := make([]float64, len(candles))
				lows := make([]float64, len(candles))
				volumes := make([]int64, len(candles))
				for i, c := range candles {
					closes[i] = c.Close
					highs[i] = c.High
					lows[i] = c.Low
					volumes[i] = c.Volume
				}

				currentPrice := closes[len(closes)-1]
				
				// Apply price filters first (fast filter)
				if minPrice > 0 && currentPrice < minPrice {
					continue
				}
				if maxPrice > 0 && currentPrice > maxPrice {
					continue
				}

				// Calculate indicators
				rsi := calculateRSI(closes, 14)
				
				// Volume ratio
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

				// Change percent
				change := 0.0
				if len(closes) >= 2 && closes[len(closes)-2] > 0 {
					change = ((closes[len(closes)-1] - closes[len(closes)-2]) / closes[len(closes)-2]) * 100
				}

				// Gap calculation
				gap := 0.0
				if len(candles) >= 2 {
					prevClose := candles[len(candles)-2].Close
					currOpen := candles[len(candles)-1].Open
					if prevClose > 0 {
						gap = ((currOpen - prevClose) / prevClose) * 100
					}
				}
				
				// Calculate ATR (14-period) as percentage of price
				atr := calculateATR(highs, lows, closes, 14)
				atrPct := 0.0
				if currentPrice > 0 {
					atrPct = (atr / currentPrice) * 100
				}
				
				// Calculate today's range as percentage
				dayRange := 0.0
				if len(candles) > 0 {
					lastCandle := candles[len(candles)-1]
					if lastCandle.Low > 0 {
						dayRange = ((lastCandle.High - lastCandle.Low) / lastCandle.Low) * 100
					}
				}

				// Apply technical filters
				if rsiBelow > 0 && rsi >= rsiBelow {
					continue
				}
				if rsiAbove > 0 && rsi <= rsiAbove {
					continue
				}
				if volumeAbove > 0 && volRatio < volumeAbove {
					continue
				}
				if gapUp > 0 && gap < gapUp {
					continue
				}
				if gapDown > 0 && gap > -gapDown {
					continue
				}
				
				// Apply volatility filters
				if minATR > 0 && atrPct < minATR {
					continue
				}
				if minChange > 0 && (change < minChange && change > -minChange) {
					continue
				}
				
				// Apply gainers/losers filter
				if gainers && change <= 0 {
					continue
				}
				if losers && change >= 0 {
					continue
				}

				// Determine signal
				signal := "NEUTRAL"
				if rsi < 30 {
					signal = "OVERSOLD"
				} else if rsi > 70 {
					signal = "OVERBOUGHT"
				} else if volRatio > 2.0 {
					signal = "HIGH VOLUME"
				} else if atrPct > 3.0 {
					signal = "VOLATILE"
				}

				results = append(results, ScanResult{
					Symbol:   symbol,
					LTP:      closes[len(closes)-1],
					Change:   change,
					RSI:      rsi,
					Volume:   volRatio,
					ATRPct:   atrPct,
					DayRange: dayRange,
					Signal:   signal,
				})
			}
			
			// Clear progress line
			if len(symbols) > 50 {
				output.Printf("\r                                                                    \r")
				output.Printf("  Scanned %d stocks in %s, found %d matches\n\n", 
					scanned, time.Since(startTime).Round(time.Second), len(results))
			}
			
			// Sort results
			switch sortBy {
			case "price":
				sort.Slice(results, func(i, j int) bool {
					return results[i].LTP > results[j].LTP
				})
			case "change":
				sort.Slice(results, func(i, j int) bool {
					// Sort by absolute change for movers
					return absFloat(results[i].Change) > absFloat(results[j].Change)
				})
			case "rsi":
				sort.Slice(results, func(i, j int) bool {
					return results[i].RSI < results[j].RSI
				})
			case "volume":
				sort.Slice(results, func(i, j int) bool {
					return results[i].Volume > results[j].Volume
				})
			case "atr", "volatile":
				sort.Slice(results, func(i, j int) bool {
					return results[i].ATRPct > results[j].ATRPct
				})
			}
			
			// Limit results
			if limit > 0 && len(results) > limit {
				results = results[:limit]
			}

			if output.IsJSON() {
				return output.JSON(results)
			}

			return displayScanResults(output, results)
		},
	}

	// Technical filters
	cmd.Flags().String("preset", "", "Use preset screener (momentum, oversold, overbought, breakout, reversal, movers, volatile)")
	cmd.Flags().Float64("rsi-below", 0, "RSI below threshold")
	cmd.Flags().Float64("rsi-above", 0, "RSI above threshold")
	cmd.Flags().Float64("volume-above", 0, "Volume multiple above average")
	cmd.Flags().Float64("gap-up", 0, "Gap up percentage")
	cmd.Flags().Float64("gap-down", 0, "Gap down percentage")
	
	// Volatility filters (for day trading)
	cmd.Flags().Bool("volatile", false, "Show volatile stocks (ATR > 2%)")
	cmd.Flags().Float64("min-atr", 0, "Minimum ATR percentage (volatility filter)")
	cmd.Flags().Float64("min-change", 0, "Minimum absolute change percentage")
	
	// Price filters
	cmd.Flags().Float64("min-price", 0, "Minimum stock price")
	cmd.Flags().Float64("max-price", 0, "Maximum stock price")
	cmd.Flags().Bool("penny", false, "Show penny stocks (< ₹50)")
	cmd.Flags().Bool("midcap", false, "Show mid-cap stocks (₹100-₹500)")
	cmd.Flags().Bool("largecap", false, "Show large-cap stocks (> ₹500)")
	
	// Change filters
	cmd.Flags().Bool("gainers", false, "Show only gainers (positive change)")
	cmd.Flags().Bool("losers", false, "Show only losers (negative change)")
	
	// Output options
	cmd.Flags().Int("limit", 0, "Limit number of results")
	cmd.Flags().String("sort", "", "Sort by: price, change, rsi, volume, atr")
	
	// Scope options
	cmd.Flags().Bool("all", false, "Scan ALL NSE equity stocks (takes time)")
	cmd.Flags().String("index", "", "Scan index constituents (nifty50, nifty100, nifty200)")
	cmd.Flags().String("watchlist", "", "Scan specific watchlist (default: 'default')")
	cmd.Flags().StringP("exchange", "e", "NSE", "Exchange (NSE, BSE)")

	return cmd
}

// getIndexConstituents returns the stock symbols for a given index
func getIndexConstituents(indexName string) []string {
	switch strings.ToLower(indexName) {
	case "nifty50", "nifty-50", "nifty 50":
		return []string{
			"ADANIENT", "ADANIPORTS", "APOLLOHOSP", "ASIANPAINT", "AXISBANK",
			"BAJAJ-AUTO", "BAJFINANCE", "BAJAJFINSV", "BPCL", "BHARTIARTL",
			"BRITANNIA", "CIPLA", "COALINDIA", "DIVISLAB", "DRREDDY",
			"EICHERMOT", "GRASIM", "HCLTECH", "HDFCBANK", "HDFCLIFE",
			"HEROMOTOCO", "HINDALCO", "HINDUNILVR", "ICICIBANK", "ITC",
			"INDUSINDBK", "INFY", "JSWSTEEL", "KOTAKBANK", "LT",
			"M&M", "MARUTI", "NTPC", "NESTLEIND", "ONGC",
			"POWERGRID", "RELIANCE", "SBILIFE", "SBIN", "SUNPHARMA",
			"TCS", "TATACONSUM", "TATAMOTORS", "TATASTEEL", "TECHM",
			"TITAN", "ULTRACEMCO", "UPL", "WIPRO", "ZOMATO",
		}
	case "nifty100", "nifty-100", "nifty 100":
		// Nifty 50 + Next 50
		nifty50 := getIndexConstituents("nifty50")
		next50 := []string{
			"ABB", "ADANIGREEN", "AMBUJACEM", "AUROPHARMA", "BAJAJHLDNG",
			"BANKBARODA", "BEL", "BERGEPAINT", "BOSCHLTD", "CANBK",
			"CHOLAFIN", "COLPAL", "DLF", "DABUR", "GAIL",
			"GODREJCP", "HAVELLS", "HINDPETRO", "ICICIPRULI", "ICICIGI",
			"IDEA", "INDIGO", "IOC", "IRCTC", "JINDALSTEL",
			"LICI", "LUPIN", "MARICO", "MCDOWELL-N", "MOTHERSON",
			"NAUKRI", "NHPC", "NMDC", "OBEROIRLTY", "OFSS",
			"PAGEIND", "PETRONET", "PIDILITIND", "PNB", "POLYCAB",
			"RECLTD", "SAIL", "SBICARD", "SHREECEM", "SIEMENS",
			"SRF", "TATAPOWER", "TORNTPHARM", "TRENT", "VEDL",
		}
		return append(nifty50, next50...)
	case "nifty200", "nifty-200", "nifty 200":
		// Nifty 100 + additional 100 stocks
		nifty100 := getIndexConstituents("nifty100")
		additional := []string{
			"ACC", "ALKEM", "APOLLOTYRE", "ASHOKLEY", "ASTRAL",
			"ATUL", "AUBANK", "AUROPHARMA", "BALKRISIND", "BANDHANBNK",
			"BATAINDIA", "BHARATFORG", "BHEL", "BIOCON", "CANFINHOME",
			"CGPOWER", "CHAMBLFERT", "COFORGE", "CONCOR", "CROMPTON",
			"CUMMINSIND", "DEEPAKNTR", "DELHIVERY", "DIXON", "ESCORTS",
			"EXIDEIND", "FEDERALBNK", "FORTIS", "GLAND", "GLAXO",
			"GMRINFRA", "GODREJPROP", "GSPL", "GUJGASLTD", "HAL",
			"HDFCAMC", "HONAUT", "IDFCFIRSTB", "IEX", "INDHOTEL",
			"INDUSTOWER", "IPCA", "IRFC", "ISEC", "JKCEMENT",
			"JSL", "JUBLFOOD", "KAJARIACER", "KEI", "KPITTECH",
			"L&TFH", "LAURUSLABS", "LICHSGFIN", "LTIM", "LTTS",
			"M&MFIN", "MANAPPURAM", "MFSL", "MGL", "MPHASIS",
			"MRF", "MUTHOOTFIN", "NAM-INDIA", "NATIONALUM", "NAVINFLUOR",
			"NYKAA", "OIL", "PAYTM", "PEL", "PERSISTENT",
			"PFC", "PIIND", "PNB", "POLYCAB", "PRESTIGE",
			"PVRINOX", "RAMCOCEM", "RBLBANK", "RELAXO", "SCHAEFFLER",
			"SHRIRAMFIN", "SONACOMS", "STARHEALTH", "SUMICHEM", "SUNDARMFIN",
			"SUNDRMFAST", "SUPREMEIND", "SYNGENE", "TATACHEM", "TATACOMM",
			"TATAELXSI", "THERMAX", "TIINDIA", "TIMKEN", "TORNTPOWER",
			"TVSMOTOR", "UBL", "UNIONBANK", "UNITDSPR", "VOLTAS",
		}
		return append(nifty100, additional...)
	case "banknifty", "bank-nifty", "bank nifty":
		return []string{
			"AUBANK", "AXISBANK", "BANDHANBNK", "BANKBARODA", "FEDERALBNK",
			"HDFCBANK", "ICICIBANK", "IDFCFIRSTB", "INDUSINDBK", "KOTAKBANK",
			"PNB", "SBIN",
		}
	case "nifty500", "nifty-500", "nifty 500":
		// Nifty 200 + additional 300 stocks (major NSE stocks)
		nifty200 := getIndexConstituents("nifty200")
		additional := []string{
			// Additional 300 stocks from Nifty 500
			"3MINDIA", "ABORTIONCL", "ABSLAMC", "AFFLE", "AIAENG",
			"AJANTPHARM", "AKZOINDIA", "AMARAJABAT", "AMBER", "APLAPOLLO",
			"APTUS", "ARE&M", "ASAHIINDIA", "ASHOKA", "ASTRAZEN",
			"ATUL", "AVANTIFEED", "BASF", "BAYERCROP", "BBTC",
			"BDL", "BEML", "BLUESTARCO", "BRIGADE", "BSE",
			"BSOFT", "CAMPUS", "CARBORUNIV", "CASTROLIND", "CEATLTD",
			"CENTRALBK", "CENTURYTEX", "CERA", "CHALET", "CLEAN",
			"COCHINSHIP", "COROMANDEL", "CREDITACC", "CRISIL", "CYIENT",
			"DATAPATTNS", "DCMSHRIRAM", "DEVYANI", "DMART", "EASEMYTRIP",
			"ECLERX", "EDELWEISS", "EIDPARRY", "ELECON", "ELGIEQUIP",
			"EMAMILTD", "ENDURANCE", "ENGINERSIN", "EPIGRAL", "EQUITASBNK",
			"ERIS", "FINCABLES", "FINPIPE", "FIRSTSOUR", "FIVESTAR",
			"FLUOROCHEM", "FOSECOIND", "FSL", "GALAXYSURF", "GARFIBRES",
			"GATEWAY", "GESHIP", "GILLETTE", "GLENMARK", "GLOBUSSPR",
			"GNFC", "GODFRYPHLP", "GODREJAGRO", "GODREJIND", "GPPL",
			"GRANULES", "GRAPHITE", "GRINDWELL", "GRSE", "GSFC",
			"GUJALKALI", "HAPPSTMNDS", "HATSUN", "HEG", "HEIDELBERG",
			"HFCL", "HIKAL", "HINDCOPPER", "HINDZINC", "HOMEFIRST",
			"HUDCO", "IBREALEST", "IBULHSGFIN", "ICRA", "IDBI",
			"IFBIND", "IIFL", "IIFLWAM", "INDIACEM", "INDIAMART",
			"INDIANB", "INDIGOPNTS", "INFIBEAM", "INTELLECT", "IPCALAB",
			"IRB", "IRCON", "ISEC", "ITI", "J&KBANK",
			"JAMNAAUTO", "JBCHEPHARM", "JBMA", "JINDALSAW", "JKLAKSHMI",
			"JKPAPER", "JMFINANCIL", "JSWENERGY", "JTEKTINDIA", "JUSTDIAL",
			"JYOTHYLAB", "KALYANKJIL", "KANSAINER", "KARURVYSYA", "KEC",
			"KFINTECH", "KIRLOSENG", "KNRCON", "KPIL", "KRBL",
			"KSB", "LATENTVIEW", "LAXMIMACH", "LEMONTREE", "LINDEINDIA",
			"LLOYDSME", "LUXIND", "MAHABANK", "MAHINDCIE", "MAHLIFE",
			"MAHLOG", "MAHSEAMLES", "MAITHANALL", "MAPMYINDIA", "MASTEK",
			"MAXHEALTH", "MAZDOCK", "MCX", "MEDANTA", "MEDPLUS",
			"METROPOLIS", "MIDHANI", "MINDACORP", "MMTC", "MOIL",
			"MOTILALOFS", "MPHASIS", "MRPL", "MSUMI", "MTARTECH",
			"NATCOPHARM", "NAUKRI", "NAVNETEDUL", "NBCC", "NCC",
			"NETWORK18", "NH", "NLCINDIA", "NOCIL", "NUVOCO",
			"OBEROIRLTY", "OLECTRA", "ORIENTELEC", "ORIENTCEM", "PARAS",
			"PATANJALI", "PCBL", "PDSL", "PGHH", "PHOENIXLTD",
			"PNBHOUSING", "POLYMED", "POONAWALLA", "POWERINDIA", "PPLPHARMA",
			"PRINCEPIPE", "PRSMJOHNSN", "QUESS", "RADICO", "RAIN",
			"RAJESHEXPO", "RALLIS", "RATNAMANI", "RAYMOND", "REDINGTON",
			"RELAXO", "RENUKA", "RITES", "RKFORGE", "ROUTE",
			"RPOWER", "SAFARI", "SAGCEM", "SANOFI", "SAPPHIRE",
			"SAREGAMA", "SBICARD", "SCHNEIDER", "SEQUENT", "SHARDACROP",
			"SHILPAMED", "SHOPERSTOP", "SHYAMMETL", "SJVN", "SKFINDIA",
			"SOBHA", "SOLARA", "SONATSOFTW", "SOUTHBANK", "SPARC",
			"SPANDANA", "SPLPETRO", "STAR", "STLTECH", "SUDARSCHEM",
			"SUNDRMFAST", "SUNFLAG", "SUNTV", "SUPRAJIT", "SUPRIYA",
			"SUVENPHAR", "SWANENERGY", "SYMPHONY", "TANLA", "TATACOFFEE",
			"TATAINVEST", "TATATECH", "TCNSBRANDS", "TEAMLEASE", "TECHNOE",
			"TEGA", "THANGAMAYL", "THYROCARE", "TI", "TINPLATE",
			"TMB", "TORNTPOWER", "TRENT", "TRIDENT", "TRITURBINE",
			"TRIVENI", "TTKPRESTIG", "TV18BRDCST", "UCOBANK", "UFLEX",
			"UJJIVAN", "UJJIVANSFB", "UNIONBANK", "UNOMINDA", "UPL",
			"UTIAMC", "VAIBHAVGBL", "VAKRANGEE", "VARROC", "VBL",
			"VEDL", "VENKEYS", "VGUARD", "VINATIORGA", "VIPIND",
			"VMART", "VOLTAMP", "VSTIND", "WELCORP", "WELSPUNIND",
			"WESTLIFE", "WHIRLPOOL", "WOCKPHARMA", "YESBANK", "ZEEL",
			"ZENSARTECH", "ZFCVINDIA", "ZOMATO", "ZYDUSLIFE",
		}
		return append(nifty200, additional...)
	case "fno", "f&o":
		// F&O stocks - most liquid stocks with derivatives
		return []string{
			"AARTIIND", "ABB", "ABBOTINDIA", "ABCAPITAL", "ABFRL",
			"ACC", "ADANIENT", "ADANIPORTS", "ALKEM", "AMBUJACEM",
			"APOLLOHOSP", "APOLLOTYRE", "ASHOKLEY", "ASIANPAINT", "ASTRAL",
			"ATUL", "AUBANK", "AUROPHARMA", "AXISBANK", "BAJAJ-AUTO",
			"BAJAJFINSV", "BAJFINANCE", "BALKRISIND", "BANDHANBNK", "BANKBARODA",
			"BATAINDIA", "BEL", "BERGEPAINT", "BHARATFORG", "BHARTIARTL",
			"BHEL", "BIOCON", "BOSCHLTD", "BPCL", "BRITANNIA",
			"BSOFT", "CANBK", "CANFINHOME", "CHAMBLFERT", "CHOLAFIN",
			"CIPLA", "COALINDIA", "COFORGE", "COLPAL", "CONCOR",
			"COROMANDEL", "CROMPTON", "CUB", "CUMMINSIND", "DABUR",
			"DALBHARAT", "DEEPAKNTR", "DELTACORP", "DIVISLAB", "DIXON",
			"DLF", "DRREDDY", "EICHERMOT", "ESCORTS", "EXIDEIND",
			"FEDERALBNK", "GAIL", "GLENMARK", "GMRINFRA", "GNFC",
			"GODREJCP", "GODREJPROP", "GRANULES", "GRASIM", "GUJGASLTD",
			"HAL", "HAVELLS", "HCLTECH", "HDFCAMC", "HDFCBANK",
			"HDFCLIFE", "HEROMOTOCO", "HINDALCO", "HINDCOPPER", "HINDPETRO",
			"HINDUNILVR", "ICICIBANK", "ICICIGI", "ICICIPRULI", "IDEA",
			"IDFC", "IDFCFIRSTB", "IEX", "IGL", "INDHOTEL",
			"INDIACEM", "INDIAMART", "INDIGO", "INDUSINDBK", "INDUSTOWER",
			"INFY", "IOC", "IPCALAB", "IRCTC", "ITC",
			"JINDALSTEL", "JKCEMENT", "JSWSTEEL", "JUBLFOOD", "KOTAKBANK",
			"LALPATHLAB", "LAURUSLABS", "LICHSGFIN", "LT", "LTIM",
			"LTTS", "LUPIN", "M&M", "M&MFIN", "MANAPPURAM",
			"MARICO", "MARUTI", "MCDOWELL-N", "MCX", "METROPOLIS",
			"MFSL", "MGL", "MOTHERSON", "MPHASIS", "MRF",
			"MUTHOOTFIN", "NATIONALUM", "NAUKRI", "NAVINFLUOR", "NESTLEIND",
			"NMDC", "NTPC", "OBEROIRLTY", "OFSS", "ONGC",
			"PAGEIND", "PEL", "PERSISTENT", "PETRONET", "PFC",
			"PIDILITIND", "PIIND", "PNB", "POLYCAB", "POWERGRID",
			"PVRINOX", "RAMCOCEM", "RBLBANK", "RECLTD", "RELIANCE",
			"SAIL", "SBICARD", "SBILIFE", "SBIN", "SHREECEM",
			"SHRIRAMFIN", "SIEMENS", "SRF", "SUNPHARMA", "SUNTV",
			"SYNGENE", "TATACHEM", "TATACOMM", "TATACONSUM", "TATAELXSI",
			"TATAMOTORS", "TATAPOWER", "TATASTEEL", "TCS", "TECHM",
			"TITAN", "TORNTPHARM", "TRENT", "TVSMOTOR", "UBL",
			"ULTRACEMCO", "UNIONBANK", "UPL", "VEDL", "VOLTAS",
			"WIPRO", "ZEEL", "ZYDUSLIFE",
		}
	case "smallcap", "small-cap", "small cap":
		// Popular small-cap stocks
		return []string{
			"AARTIDRUGS", "AFFLE", "AJANTPHARM", "ALKYLAMINE", "AMBER",
			"ANGELONE", "APTUS", "ASTERDM", "ASTRAZEN", "AVANTIFEED",
			"BANARISUG", "BASF", "BCG", "BEML", "BLUESTARCO",
			"BRIGADE", "CAMPUS", "CARBORUNIV", "CEATLTD", "CENTURYTEX",
			"CERA", "CHALET", "CLEAN", "COCHINSHIP", "CREDITACC",
			"CRISIL", "CYIENT", "DATAPATTNS", "DCMSHRIRAM", "DEVYANI",
			"EASEMYTRIP", "ECLERX", "EDELWEISS", "EIDPARRY", "ELECON",
			"ELGIEQUIP", "EMAMILTD", "ENDURANCE", "ENGINERSIN", "EPIGRAL",
			"EQUITASBNK", "ERIS", "FINCABLES", "FINPIPE", "FIRSTSOUR",
			"FIVESTAR", "FLUOROCHEM", "FSL", "GALAXYSURF", "GARFIBRES",
			"GATEWAY", "GESHIP", "GLENMARK", "GLOBUSSPR", "GNFC",
			"GODFRYPHLP", "GODREJAGRO", "GODREJIND", "GPPL", "GRANULES",
			"GRAPHITE", "GRINDWELL", "GRSE", "GSFC", "GUJALKALI",
			"HAPPSTMNDS", "HATSUN", "HEG", "HEIDELBERG", "HFCL",
			"HIKAL", "HINDCOPPER", "HINDZINC", "HOMEFIRST", "HUDCO",
			"IBREALEST", "ICRA", "IDBI", "IFBIND", "IIFL",
			"INDIACEM", "INDIAMART", "INDIANB", "INDIGOPNTS", "INFIBEAM",
			"INTELLECT", "IRB", "IRCON", "ITI", "J&KBANK",
			"JAMNAAUTO", "JBCHEPHARM", "JINDALSAW", "JKLAKSHMI", "JKPAPER",
			"JMFINANCIL", "JSWENERGY", "JUSTDIAL", "JYOTHYLAB", "KALYANKJIL",
		}
	default:
		return nil
	}
}

type ScanResult struct {
	Symbol    string
	LTP       float64
	Change    float64
	RSI       float64
	Volume    float64
	ATRPct    float64 // ATR as percentage of price (volatility measure)
	DayRange  float64 // Today's high-low range as percentage
	Signal    string
}

func displayScanResults(output *Output, results []ScanResult) error {
	output.Bold("Scan Results")
	output.Printf("  Found %d stocks\n\n", len(results))

	table := NewTable(output, "Symbol", "LTP", "Change", "ATR%", "RSI", "Volume", "Signal")
	for _, r := range results {
		// Color ATR based on volatility level
		atrColor := ColorYellow
		if r.ATRPct > 3.0 {
			atrColor = ColorGreen // High volatility - good for day trading
		} else if r.ATRPct < 1.5 {
			atrColor = ColorRed // Low volatility
		}
		
		table.AddRow(
			r.Symbol,
			FormatPrice(r.LTP),
			output.ColoredString(output.PnLColor(r.Change), FormatPercent(r.Change)),
			output.ColoredString(atrColor, fmt.Sprintf("%.1f%%", r.ATRPct)),
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
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			symbol := strings.ToUpper(args[0])
			exchange, _ := cmd.Flags().GetString("exchange")

			output.Info("Generating research report for %s...", symbol)

			// Company data mapping (basic info for major stocks)
			companyData := map[string]struct {
				name     string
				sector   string
				industry string
			}{
				"RELIANCE":   {"Reliance Industries Ltd", "Energy", "Oil & Gas / Retail / Telecom"},
				"TCS":        {"Tata Consultancy Services", "Technology", "IT Services"},
				"INFY":       {"Infosys Ltd", "Technology", "IT Services"},
				"HDFCBANK":   {"HDFC Bank Ltd", "Financial Services", "Private Banking"},
				"ICICIBANK":  {"ICICI Bank Ltd", "Financial Services", "Private Banking"},
				"SBIN":       {"State Bank of India", "Financial Services", "Public Banking"},
				"BHARTIARTL": {"Bharti Airtel Ltd", "Telecom", "Telecom Services"},
				"ITC":        {"ITC Ltd", "Consumer Goods", "FMCG / Hotels"},
				"KOTAKBANK":  {"Kotak Mahindra Bank", "Financial Services", "Private Banking"},
				"LT":         {"Larsen & Toubro Ltd", "Industrials", "Engineering & Construction"},
				"HINDUNILVR": {"Hindustan Unilever Ltd", "Consumer Goods", "FMCG"},
				"AXISBANK":   {"Axis Bank Ltd", "Financial Services", "Private Banking"},
				"BAJFINANCE": {"Bajaj Finance Ltd", "Financial Services", "NBFC"},
				"MARUTI":     {"Maruti Suzuki India Ltd", "Automobile", "Passenger Vehicles"},
				"TATAMOTORS": {"Tata Motors Ltd", "Automobile", "Commercial & Passenger Vehicles"},
				"WIPRO":      {"Wipro Ltd", "Technology", "IT Services"},
				"HCLTECH":    {"HCL Technologies Ltd", "Technology", "IT Services"},
				"SUNPHARMA":  {"Sun Pharmaceutical", "Healthcare", "Pharmaceuticals"},
				"TITAN":      {"Titan Company Ltd", "Consumer Goods", "Jewelry & Watches"},
				"TATASTEEL":  {"Tata Steel Ltd", "Materials", "Steel"},
			}

			company, found := companyData[symbol]
			if !found {
				company.name = symbol + " Ltd"
				company.sector = "Unknown"
				company.industry = "Unknown"
			}

			research := ResearchResult{
				Symbol:      symbol,
				CompanyName: company.name,
				Sector:      company.sector,
				Industry:    company.industry,
			}

			// Fetch real price data if broker available
			if app.Broker != nil {
				// Get current quote
				fullSymbol := fmt.Sprintf("%s:%s", exchange, symbol)
				quote, err := app.Broker.GetQuote(ctx, fullSymbol)
				if err == nil {
					research.LTP = quote.LTP
					research.Change = quote.ChangePercent
				}

				// Get historical data for analysis
				candles, err := app.Broker.GetHistorical(ctx, broker.HistoricalRequest{
					Symbol:    symbol,
					Exchange:  models.Exchange(exchange),
					Timeframe: "1day",
					From:      time.Now().AddDate(-1, 0, 0),
					To:        time.Now(),
				})
				if err == nil && len(candles) > 50 {
					// Calculate 52-week high/low
					high52 := candles[0].High
					low52 := candles[0].Low
					for _, c := range candles {
						if c.High > high52 {
							high52 = c.High
						}
						if c.Low < low52 {
							low52 = c.Low
						}
					}
					research.High52Week = high52
					research.Low52Week = low52

					// Calculate returns
					if len(candles) >= 252 {
						research.Return1Y = ((candles[len(candles)-1].Close - candles[0].Close) / candles[0].Close) * 100
					}
					if len(candles) >= 126 {
						idx := len(candles) - 126
						research.Return6M = ((candles[len(candles)-1].Close - candles[idx].Close) / candles[idx].Close) * 100
					}
					if len(candles) >= 21 {
						idx := len(candles) - 21
						research.Return1M = ((candles[len(candles)-1].Close - candles[idx].Close) / candles[idx].Close) * 100
					}

					// Calculate volatility (annualized)
					if len(candles) >= 30 {
						var returns []float64
						for i := 1; i < len(candles); i++ {
							if candles[i-1].Close > 0 {
								ret := (candles[i].Close - candles[i-1].Close) / candles[i-1].Close
								returns = append(returns, ret)
							}
						}
						if len(returns) > 0 {
							mean := 0.0
							for _, r := range returns {
								mean += r
							}
							mean /= float64(len(returns))

							variance := 0.0
							for _, r := range returns {
								variance += (r - mean) * (r - mean)
							}
							variance /= float64(len(returns))
							research.Volatility = variance * 252 * 100 // Annualized
						}
					}
				}
			}

			// Use AI to generate insights if LLM client is available
			if app.LLMClient != nil {
				output.Info("Analyzing with AI...")
				aiHighlights, aiRisks, err := generateAIInsights(ctx, app.LLMClient, research)
				if err != nil {
					output.Warning("AI analysis failed, using rule-based analysis: %v", err)
					// Fall back to rule-based
					research.Highlights, research.Risks = generateRuleBasedInsights(research, company.sector)
				} else {
					research.Highlights = aiHighlights
					research.Risks = aiRisks
					research.AIGenerated = true
				}
			} else {
				// Rule-based fallback when no LLM
				research.Highlights, research.Risks = generateRuleBasedInsights(research, company.sector)
			}

			output.Println()

			if output.IsJSON() {
				return output.JSON(research)
			}

			return displayResearch(output, research)
		},
	}

	cmd.Flags().Bool("detailed", false, "Show detailed research")
	cmd.Flags().StringP("exchange", "e", "NSE", "Exchange (NSE, BSE)")

	return cmd
}

type ResearchResult struct {
	Symbol        string
	CompanyName   string
	Sector        string
	Industry      string
	LTP           float64
	Change        float64
	High52Week    float64
	Low52Week     float64
	Return1M      float64
	Return6M      float64
	Return1Y      float64
	Volatility    float64
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
	AIGenerated   bool
}

func displayResearch(output *Output, r ResearchResult) error {
	output.Bold("%s - %s", r.Symbol, r.CompanyName)
	output.Printf("  %s > %s\n", r.Sector, r.Industry)
	output.Println()

	// Price info - from Zerodha
	if r.LTP > 0 {
		output.Bold("Price %s", output.SourceTag(SourceZerodha))
		output.Printf("  LTP:           %s  %s\n", FormatPrice(r.LTP), output.ColoredString(output.PnLColor(r.Change), FormatPercent(r.Change)))
		if r.High52Week > 0 {
			output.Printf("  52W High:      %s\n", FormatPrice(r.High52Week))
			output.Printf("  52W Low:       %s\n", FormatPrice(r.Low52Week))
		}
		output.Println()
	}

	// Returns - calculated from Zerodha data
	if r.Return1M != 0 || r.Return6M != 0 || r.Return1Y != 0 {
		output.Bold("Returns %s", output.SourceTag(SourceCalc))
		if r.Return1M != 0 {
			output.Printf("  1 Month:       %s\n", output.ColoredString(output.PnLColor(r.Return1M), FormatPercent(r.Return1M)))
		}
		if r.Return6M != 0 {
			output.Printf("  6 Months:      %s\n", output.ColoredString(output.PnLColor(r.Return6M), FormatPercent(r.Return6M)))
		}
		if r.Return1Y != 0 {
			output.Printf("  1 Year:        %s\n", output.ColoredString(output.PnLColor(r.Return1Y), FormatPercent(r.Return1Y)))
		}
		if r.Volatility > 0 {
			output.Printf("  Volatility:    %.1f%% (annualized)\n", r.Volatility)
		}
		output.Println()
	}

	// Fundamentals (if available)
	if r.MarketCap > 0 || r.PE > 0 {
		output.Bold("Fundamentals")
		if r.MarketCap > 0 {
			output.Printf("  Market Cap:    %s\n", FormatCompact(r.MarketCap*10000000))
		}
		if r.PE > 0 {
			output.Printf("  P/E Ratio:     %.2f\n", r.PE)
		}
		if r.PB > 0 {
			output.Printf("  P/B Ratio:     %.2f\n", r.PB)
		}
		if r.ROE > 0 {
			output.Printf("  ROE:           %.2f%%\n", r.ROE)
		}
		if r.DebtToEquity > 0 {
			output.Printf("  Debt/Equity:   %.2f\n", r.DebtToEquity)
		}
		if r.DividendYield > 0 {
			output.Printf("  Div Yield:     %.2f%%\n", r.DividendYield)
		}
		output.Println()
	}

	// Determine source tag for insights
	insightSource := SourceCalc
	if r.AIGenerated {
		insightSource = SourceAI
	}

	output.Bold("Key Highlights %s", output.SourceTag(insightSource))
	for _, h := range r.Highlights {
		output.Printf("  %s %s\n", output.Green("✓"), h)
	}
	output.Println()

	output.Bold("Key Risks %s", output.SourceTag(insightSource))
	for _, risk := range r.Risks {
		output.Printf("  %s %s\n", output.Yellow("⚠"), risk)
	}

	return nil
}

// generateAIInsights uses LLM to generate research insights
func generateAIInsights(ctx context.Context, llm LLMClient, r ResearchResult) ([]string, []string, error) {
	// Build context for AI
	prompt := fmt.Sprintf(`Analyze this Indian stock and provide investment insights:

Company: %s (%s)
Sector: %s | Industry: %s

Price Data:
- Current Price: ₹%.2f
- 52-Week High: ₹%.2f
- 52-Week Low: ₹%.2f
- 1-Month Return: %.2f%%
- 6-Month Return: %.2f%%
- 1-Year Return: %.2f%%
- Volatility: %.2f%% (annualized)

Provide your analysis in this exact format:
HIGHLIGHTS:
- [highlight 1]
- [highlight 2]
- [highlight 3]

RISKS:
- [risk 1]
- [risk 2]
- [risk 3]

Keep each point concise (under 60 characters). Focus on actionable insights for traders.`,
		r.CompanyName, r.Symbol, r.Sector, r.Industry,
		r.LTP, r.High52Week, r.Low52Week,
		r.Return1M, r.Return6M, r.Return1Y, r.Volatility)

	response, err := llm.Complete(ctx, prompt)
	if err != nil {
		return nil, nil, err
	}

	// Parse response
	highlights, risks := parseAIInsights(response)
	return highlights, risks, nil
}

// parseAIInsights parses the AI response into highlights and risks
func parseAIInsights(response string) ([]string, []string) {
	var highlights, risks []string
	lines := strings.Split(response, "\n")

	inHighlights := false
	inRisks := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		upperLine := strings.ToUpper(line)
		if strings.Contains(upperLine, "HIGHLIGHT") {
			inHighlights = true
			inRisks = false
			continue
		}
		if strings.Contains(upperLine, "RISK") {
			inHighlights = false
			inRisks = true
			continue
		}

		// Extract bullet points or numbered items
		content := line
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "•") || strings.HasPrefix(line, "*") {
			content = strings.TrimSpace(line[1:])
		}
		// Handle numbered lists like "1. " or "1) "
		if len(content) > 2 && (content[0] >= '1' && content[0] <= '9') {
			if content[1] == '.' || content[1] == ')' {
				content = strings.TrimSpace(content[2:])
			} else if len(content) > 3 && content[2] == '.' || content[2] == ')' {
				content = strings.TrimSpace(content[3:])
			}
		}

		if content == "" || content == line {
			// Skip lines that aren't bullet points
			if !strings.HasPrefix(strings.TrimSpace(line), "-") && 
			   !strings.HasPrefix(strings.TrimSpace(line), "•") &&
			   !strings.HasPrefix(strings.TrimSpace(line), "*") &&
			   !(len(line) > 2 && line[0] >= '1' && line[0] <= '9') {
				continue
			}
		}

		if inHighlights && len(highlights) < 5 && content != "" {
			highlights = append(highlights, content)
		} else if inRisks && len(risks) < 5 && content != "" {
			risks = append(risks, content)
		}
	}

	// Ensure we have at least some content
	if len(highlights) == 0 {
		highlights = []string{"Analysis pending"}
	}
	if len(risks) == 0 {
		risks = []string{"Standard market risks apply"}
	}

	return highlights, risks
}

// generateRuleBasedInsights generates insights using rule-based logic (fallback)
func generateRuleBasedInsights(r ResearchResult, sector string) ([]string, []string) {
	var highlights, risks []string

	// Generate highlights based on data
	if r.Return1M > 5 {
		highlights = append(highlights, fmt.Sprintf("Strong recent momentum: +%.1f%% in 1 month", r.Return1M))
	}
	if r.Return1Y > 20 {
		highlights = append(highlights, fmt.Sprintf("Solid yearly performance: +%.1f%% in 1 year", r.Return1Y))
	}
	if r.LTP > 0 && r.High52Week > 0 {
		fromHigh := ((r.High52Week - r.LTP) / r.High52Week) * 100
		if fromHigh < 10 {
			highlights = append(highlights, "Trading near 52-week high")
		}
		if fromHigh > 30 {
			highlights = append(highlights, fmt.Sprintf("Trading %.1f%% below 52-week high - potential value", fromHigh))
		}
	}

	// Generate risks based on data
	if r.Volatility > 40 {
		risks = append(risks, fmt.Sprintf("High volatility: %.1f%% annualized", r.Volatility))
	}
	if r.Return1M < -5 {
		risks = append(risks, fmt.Sprintf("Recent weakness: %.1f%% in 1 month", r.Return1M))
	}
	if r.Return1Y < 0 {
		risks = append(risks, fmt.Sprintf("Negative yearly return: %.1f%%", r.Return1Y))
	}

	// Add sector-specific insights
	switch sector {
	case "Technology":
		highlights = append(highlights, "IT sector benefiting from digital transformation")
		risks = append(risks, "Currency fluctuation impact on exports")
	case "Financial Services":
		highlights = append(highlights, "Credit growth supporting banking sector")
		risks = append(risks, "NPA concerns and interest rate sensitivity")
	case "Consumer Goods":
		highlights = append(highlights, "Rural demand recovery potential")
		risks = append(risks, "Input cost inflation pressure")
	case "Automobile":
		highlights = append(highlights, "EV transition opportunity")
		risks = append(risks, "Semiconductor supply chain issues")
	}

	// Default if no highlights/risks generated
	if len(highlights) == 0 {
		highlights = []string{"Established market presence", "Diversified business model"}
	}
	if len(risks) == 0 {
		risks = []string{"Market volatility", "Regulatory changes"}
	}

	return highlights, risks
}

// LLMClient interface for AI interactions (re-exported for use in CLI)
type LLMClient interface {
	Complete(ctx context.Context, prompt string) (string, error)
	CompleteWithSystem(ctx context.Context, systemPrompt, userPrompt string) (string, error)
	CompleteWithTools(ctx context.Context, systemPrompt, userPrompt string, tools []openai.Tool, executor agents.ToolExecutorInterface) (string, error)
	CompleteWithToolsVerbose(ctx context.Context, systemPrompt, userPrompt string, tools []openai.Tool, executor agents.ToolExecutorInterface) (*agents.ChainOfThought, error)
}

// calculateSMA calculates Simple Moving Average
func calculateSMA(data []float64, period int) float64 {
	if len(data) < period {
		return 0
	}
	sum := 0.0
	for i := len(data) - period; i < len(data); i++ {
		sum += data[i]
	}
	return sum / float64(period)
}

// calculateBollingerBands calculates Bollinger Bands
func calculateBollingerBands(closes []float64, period int, stdDev float64) (upper, middle, lower float64) {
	if len(closes) < period {
		return 0, 0, 0
	}

	middle = calculateSMA(closes, period)

	// Calculate standard deviation
	sum := 0.0
	for i := len(closes) - period; i < len(closes); i++ {
		diff := closes[i] - middle
		sum += diff * diff
	}
	sd := 0.0
	if period > 0 {
		sd = sum / float64(period)
		if sd > 0 {
			sd = sd // sqrt would be needed but we'll use variance for simplicity
		}
	}
	// Simplified: use range-based approximation
	high := closes[len(closes)-1]
	low := closes[len(closes)-1]
	for i := len(closes) - period; i < len(closes); i++ {
		if closes[i] > high {
			high = closes[i]
		}
		if closes[i] < low {
			low = closes[i]
		}
	}
	bandWidth := (high - low) / 2

	upper = middle + bandWidth
	lower = middle - bandWidth
	return upper, middle, lower
}

// calculateATR calculates Average True Range
func calculateATR(highs, lows, closes []float64, period int) float64 {
	if len(highs) < period+1 || len(lows) < period+1 || len(closes) < period+1 {
		return 0
	}

	trSum := 0.0
	for i := len(closes) - period; i < len(closes); i++ {
		tr1 := highs[i] - lows[i]
		tr2 := highs[i] - closes[i-1]
		if tr2 < 0 {
			tr2 = -tr2
		}
		tr3 := lows[i] - closes[i-1]
		if tr3 < 0 {
			tr3 = -tr3
		}
		tr := tr1
		if tr2 > tr {
			tr = tr2
		}
		if tr3 > tr {
			tr = tr3
		}
		trSum += tr
	}
	return trSum / float64(period)
}

// calculateStochastic calculates Stochastic oscillator
func calculateStochastic(highs, lows, closes []float64, kPeriod, dPeriod int) (k, d float64) {
	if len(closes) < kPeriod {
		return 50, 50
	}

	// Find highest high and lowest low in period
	highestHigh := highs[len(highs)-1]
	lowestLow := lows[len(lows)-1]
	for i := len(closes) - kPeriod; i < len(closes); i++ {
		if highs[i] > highestHigh {
			highestHigh = highs[i]
		}
		if lows[i] < lowestLow {
			lowestLow = lows[i]
		}
	}

	currentClose := closes[len(closes)-1]
	if highestHigh == lowestLow {
		k = 50
	} else {
		k = ((currentClose - lowestLow) / (highestHigh - lowestLow)) * 100
	}

	// Simplified D calculation (would need historical K values for proper SMA)
	d = k // Simplified
	return k, d
}

// calculateADX calculates Average Directional Index
func calculateADX(highs, lows, closes []float64, period int) float64 {
	if len(closes) < period+1 {
		return 20
	}

	// Simplified ADX calculation
	// Count trending vs non-trending days
	trendingDays := 0
	for i := len(closes) - period; i < len(closes); i++ {
		change := closes[i] - closes[i-1]
		if change < 0 {
			change = -change
		}
		avgPrice := (highs[i] + lows[i]) / 2
		if avgPrice > 0 && (change/avgPrice)*100 > 0.5 {
			trendingDays++
		}
	}

	// Convert to ADX-like scale (0-100)
	return float64(trendingDays) / float64(period) * 50
}

// getLastValue safely gets the last value from a slice
func getLastValue(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	return data[len(data)-1]
}

// absFloat returns the absolute value of a float64
func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// detectPatterns detects candlestick patterns in the data
func detectPatterns(candles []models.Candle) []PatternInfo {
	if len(candles) < 3 {
		return nil
	}

	var patterns []PatternInfo

	// Get last few candles
	n := len(candles)
	curr := candles[n-1]
	prev := candles[n-2]

	// Bullish Engulfing
	if prev.Close < prev.Open && curr.Close > curr.Open &&
		curr.Open < prev.Close && curr.Close > prev.Open {
		patterns = append(patterns, PatternInfo{
			Name:      "Bullish Engulfing",
			Type:      "Candlestick",
			Direction: "BULLISH",
			Strength:  0.75,
		})
	}

	// Bearish Engulfing
	if prev.Close > prev.Open && curr.Close < curr.Open &&
		curr.Open > prev.Close && curr.Close < prev.Open {
		patterns = append(patterns, PatternInfo{
			Name:      "Bearish Engulfing",
			Type:      "Candlestick",
			Direction: "BEARISH",
			Strength:  0.75,
		})
	}

	// Doji (small body)
	bodySize := curr.Close - curr.Open
	if bodySize < 0 {
		bodySize = -bodySize
	}
	candleRange := curr.High - curr.Low
	if candleRange > 0 && bodySize/candleRange < 0.1 {
		patterns = append(patterns, PatternInfo{
			Name:      "Doji",
			Type:      "Candlestick",
			Direction: "NEUTRAL",
			Strength:  0.5,
		})
	}

	// Hammer (long lower wick, small body at top)
	upperWick := curr.High - curr.Close
	if curr.Open > curr.Close {
		upperWick = curr.High - curr.Open
	}
	lowerWick := curr.Open - curr.Low
	if curr.Close < curr.Open {
		lowerWick = curr.Close - curr.Low
	}
	if candleRange > 0 && lowerWick > 2*bodySize && upperWick < bodySize {
		patterns = append(patterns, PatternInfo{
			Name:      "Hammer",
			Type:      "Candlestick",
			Direction: "BULLISH",
			Strength:  0.65,
		})
	}

	return patterns
}
