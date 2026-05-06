package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

type paperSoakLoopOptions struct {
	Run         paperSoakRunOptions
	Interval    time.Duration
	MaxRuns     int
	MarketHours bool
	WindowStart string
	WindowEnd   string
	OnceNow     bool
}

type paperSoakLoopSkip struct {
	Timestamp time.Time `json:"timestamp"`
	Reason    string    `json:"reason"`
}

type paperSoakLoopError struct {
	Timestamp time.Time `json:"timestamp"`
	Error     string    `json:"error"`
}

type paperSoakLoopReport struct {
	GeneratedAt time.Time            `json:"generated_at"`
	StartedAt   time.Time            `json:"started_at"`
	FinishedAt  time.Time            `json:"finished_at"`
	Interval    string               `json:"interval"`
	MaxRuns     int                  `json:"max_runs"`
	Attempts    int                  `json:"attempts"`
	MarketHours bool                 `json:"market_hours"`
	WindowStart string               `json:"window_start,omitempty"`
	WindowEnd   string               `json:"window_end,omitempty"`
	Stopped     bool                 `json:"stopped"`
	Runs        []paperSoakRunReport `json:"runs"`
	Skipped     []paperSoakLoopSkip  `json:"skipped,omitempty"`
	Errors      []paperSoakLoopError `json:"errors,omitempty"`
}

func newPaperSoakLoopCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "soak-loop",
		Short: "Run candidate-aware paper soak cycles repeatedly",
		Long: `Run the same candidate-aware paper soak cycle as paper soak-run on a repeatable interval.

Use this for local supervised soak sessions. For detached operation, use the
daemon's paper-soak mode so heartbeat, pause/resume, and kill switch state are
persisted with the rest of daemon control.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPaperSoakLoop(cmd, app)
		},
	}
	addPaperSoakRunFlags(cmd)
	cmd.Flags().String("interval", "15m", "Delay between paper soak attempts")
	cmd.Flags().Int("max-runs", 0, "Maximum soak attempts before exiting; 0 runs until stopped")
	cmd.Flags().Bool("market-hours", true, "Only run inside the configured intraday market-hours window")
	cmd.Flags().String("start", "09:15", "Market-hours window start in HH:MM local time")
	cmd.Flags().String("end", "15:30", "Market-hours window end in HH:MM local time")
	cmd.Flags().Bool("once-now", false, "Run the first cycle immediately, even outside market hours")
	return cmd
}

func runPaperSoakLoop(cmd *cobra.Command, app *App) error {
	output := NewOutput(cmd)
	if app.Store == nil {
		return fmt.Errorf("paper store is not available")
	}
	if app.Broker == nil {
		return fmt.Errorf("broker not configured")
	}

	runOpts, err := paperSoakRunOptionsFromFlags(cmd, "cli_loop")
	if err != nil {
		return err
	}
	intervalFlag, _ := cmd.Flags().GetString("interval")
	interval, err := time.ParseDuration(intervalFlag)
	if err != nil {
		return fmt.Errorf("invalid interval %q: %w", intervalFlag, err)
	}
	if interval <= 0 {
		return fmt.Errorf("interval must be greater than zero")
	}
	maxRuns, _ := cmd.Flags().GetInt("max-runs")
	marketHours, _ := cmd.Flags().GetBool("market-hours")
	windowStart, _ := cmd.Flags().GetString("start")
	windowEnd, _ := cmd.Flags().GetString("end")
	onceNow, _ := cmd.Flags().GetBool("once-now")
	if marketHours {
		if _, err := withinPaperSoakLoopWindow(time.Now(), windowStart, windowEnd); err != nil {
			return err
		}
	}

	runOpts.Command = paperSoakLoopCommandSummary(runOpts, interval, maxRuns, marketHours, windowStart, windowEnd, onceNow)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !output.IsJSON() {
		output.Info("Starting paper soak loop every %s", interval)
		if marketHours {
			output.Printf("  Window:   %s-%s local time\n", windowStart, windowEnd)
		}
		if maxRuns > 0 {
			output.Printf("  Max Runs: %d\n", maxRuns)
		}
		output.Println()
	}
	report := executePaperSoakLoop(ctx, app, paperSoakLoopOptions{
		Run:         runOpts,
		Interval:    interval,
		MaxRuns:     maxRuns,
		MarketHours: marketHours,
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
		OnceNow:     onceNow,
	})
	if output.IsJSON() {
		return output.JSON(report)
	}
	displayPaperSoakLoopReport(output, report)
	return nil
}

func executePaperSoakLoop(ctx context.Context, app *App, opts paperSoakLoopOptions) paperSoakLoopReport {
	startedAt := time.Now()
	report := paperSoakLoopReport{
		GeneratedAt: startedAt,
		StartedAt:   startedAt,
		Interval:    opts.Interval.String(),
		MaxRuns:     opts.MaxRuns,
		MarketHours: opts.MarketHours,
		WindowStart: opts.WindowStart,
		WindowEnd:   opts.WindowEnd,
		Runs:        []paperSoakRunReport{},
	}

	firstAttempt := true
	for {
		now := time.Now()
		shouldRun := true
		if opts.MarketHours {
			inWindow, err := withinPaperSoakLoopWindow(now, opts.WindowStart, opts.WindowEnd)
			if err != nil {
				report.Errors = append(report.Errors, paperSoakLoopError{Timestamp: now, Error: err.Error()})
				break
			}
			shouldRun = inWindow || (opts.OnceNow && firstAttempt)
		}

		if shouldRun {
			report.Attempts++
			runOpts := opts.Run
			run, err := executePaperSoakRun(ctx, app, runOpts)
			if err != nil {
				report.Errors = append(report.Errors, paperSoakLoopError{Timestamp: now, Error: err.Error()})
			} else {
				report.Runs = append(report.Runs, run)
			}
			if opts.MaxRuns > 0 && report.Attempts >= opts.MaxRuns {
				break
			}
		} else {
			report.Skipped = append(report.Skipped, paperSoakLoopSkip{
				Timestamp: now,
				Reason:    fmt.Sprintf("outside market-hours window %s-%s", opts.WindowStart, opts.WindowEnd),
			})
		}
		firstAttempt = false

		timer := time.NewTimer(opts.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			report.Stopped = true
			report.FinishedAt = time.Now()
			return report
		case <-timer.C:
		}
	}
	report.FinishedAt = time.Now()
	return report
}

func withinPaperSoakLoopWindow(now time.Time, start string, end string) (bool, error) {
	startMinutes, err := parsePaperSoakLoopClock(start)
	if err != nil {
		return false, fmt.Errorf("invalid start time %q: %w", start, err)
	}
	endMinutes, err := parsePaperSoakLoopClock(end)
	if err != nil {
		return false, fmt.Errorf("invalid end time %q: %w", end, err)
	}
	currentMinutes := now.Hour()*60 + now.Minute()
	if startMinutes <= endMinutes {
		return currentMinutes >= startMinutes && currentMinutes <= endMinutes, nil
	}
	return currentMinutes >= startMinutes || currentMinutes <= endMinutes, nil
}

func parsePaperSoakLoopClock(value string) (int, error) {
	value = strings.TrimSpace(value)
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, err
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

func paperSoakLoopCommandSummary(opts paperSoakRunOptions, interval time.Duration, maxRuns int, marketHours bool, start string, end string, onceNow bool) string {
	parts := []string{"paper soak-loop"}
	if strings.TrimSpace(opts.Symbol) != "" {
		parts = append(parts, "--symbol "+strings.ToUpper(strings.TrimSpace(opts.Symbol)))
	}
	if strings.TrimSpace(opts.Strategy) != "" {
		parts = append(parts, "--strategy "+strings.TrimSpace(opts.Strategy))
	}
	parts = append(parts, "--regime-mode "+opts.RegimeMode)
	parts = append(parts, "--interval "+interval.String())
	if maxRuns > 0 {
		parts = append(parts, fmt.Sprintf("--max-runs %d", maxRuns))
	}
	if !marketHours {
		parts = append(parts, "--market-hours=false")
	} else {
		parts = append(parts, "--start "+start, "--end "+end)
	}
	if onceNow {
		parts = append(parts, "--once-now")
	}
	if opts.DryRun {
		parts = append(parts, "--dry-run")
	}
	return strings.Join(parts, " ")
}

func displayPaperSoakLoopReport(output *Output, report paperSoakLoopReport) {
	title := "Paper Soak Loop"
	if report.Stopped {
		title += " (stopped)"
	}
	output.Bold(title)
	output.Println()
	output.Printf("  Interval:  %s\n", report.Interval)
	output.Printf("  Attempts:  %d\n", report.Attempts)
	output.Printf("  Runs:      %d completed\n", len(report.Runs))
	output.Printf("  Skipped:   %d\n", len(report.Skipped))
	output.Printf("  Errors:    %d\n", len(report.Errors))
	if report.MarketHours {
		output.Printf("  Window:    %s-%s local time\n", report.WindowStart, report.WindowEnd)
	}
	if len(report.Runs) > 0 {
		last := report.Runs[len(report.Runs)-1]
		output.Println()
		output.Printf("  Last Run:  %s | %d candidates | %d predicted | %d blocked | %d no-signal | readiness %s\n",
			last.FinishedAt.Format(time.RFC3339), last.CandidatesChecked, last.PredictionsCreated, last.Blocked, last.NoSignal, emptyDash(last.ReadinessDecision))
	}
	if len(report.Errors) > 0 {
		output.Println()
		for _, loopErr := range report.Errors {
			output.Warning("%s: %s", loopErr.Timestamp.Format(time.RFC3339), loopErr.Error)
		}
	}
}
