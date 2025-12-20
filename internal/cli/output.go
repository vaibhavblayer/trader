// Package cli provides the command-line interface for the trading application.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Color codes for terminal output
const (
	ColorReset   = "\033[0m"
	ColorRed     = "\033[31m"
	ColorGreen   = "\033[32m"
	ColorYellow  = "\033[33m"
	ColorBlue    = "\033[34m"
	ColorMagenta = "\033[35m"
	ColorCyan    = "\033[36m"
	ColorWhite   = "\033[37m"
	ColorBold    = "\033[1m"
	ColorDim     = "\033[2m"
)

// Output handles formatted output for the CLI.
type Output struct {
	writer       io.Writer
	jsonMode     bool
	colorEnabled bool
}

// NewOutput creates a new Output instance.
func NewOutput(cmd *cobra.Command) *Output {
	jsonMode, _ := cmd.Flags().GetBool("json")
	return &Output{
		writer:       cmd.OutOrStdout(),
		jsonMode:     jsonMode,
		colorEnabled: !jsonMode && isTerminal(),
	}
}

// isTerminal checks if stdout is a terminal.
func isTerminal() bool {
	fileInfo, _ := os.Stdout.Stat()
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}

// IsJSON returns true if JSON output mode is enabled.
func (o *Output) IsJSON() bool {
	return o.jsonMode
}

// JSON outputs data as JSON.
func (o *Output) JSON(data interface{}) error {
	encoder := json.NewEncoder(o.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// Print prints a message.
func (o *Output) Print(format string, args ...interface{}) {
	fmt.Fprintf(o.writer, format, args...)
}

// Println prints a message with newline.
func (o *Output) Println(args ...interface{}) {
	fmt.Fprintln(o.writer, args...)
}

// Printf prints a formatted message.
func (o *Output) Printf(format string, args ...interface{}) {
	fmt.Fprintf(o.writer, format, args...)
}

// Success prints a success message in green.
func (o *Output) Success(format string, args ...interface{}) {
	o.colored(ColorGreen, format, args...)
}

// Error prints an error message in red.
func (o *Output) Error(format string, args ...interface{}) {
	o.colored(ColorRed, format, args...)
}

// Warning prints a warning message in yellow.
func (o *Output) Warning(format string, args ...interface{}) {
	o.colored(ColorYellow, format, args...)
}

// Info prints an info message in cyan.
func (o *Output) Info(format string, args ...interface{}) {
	o.colored(ColorCyan, format, args...)
}

// Bold prints a bold message.
func (o *Output) Bold(format string, args ...interface{}) {
	o.colored(ColorBold, format, args...)
}

// Dim prints a dimmed message.
func (o *Output) Dim(format string, args ...interface{}) {
	o.colored(ColorDim, format, args...)
}

// Bullish prints a bullish indicator in green.
func (o *Output) Bullish(format string, args ...interface{}) {
	o.colored(ColorGreen, format, args...)
}

// Bearish prints a bearish indicator in red.
func (o *Output) Bearish(format string, args ...interface{}) {
	o.colored(ColorRed, format, args...)
}

// colored prints a colored message.
func (o *Output) colored(color, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if o.colorEnabled {
		fmt.Fprintf(o.writer, "%s%s%s\n", color, msg, ColorReset)
	} else {
		fmt.Fprintln(o.writer, msg)
	}
}

// ColoredString returns a colored string without newline.
func (o *Output) ColoredString(color, text string) string {
	if o.colorEnabled {
		return color + text + ColorReset
	}
	return text
}

// Green returns green colored text.
func (o *Output) Green(text string) string {
	return o.ColoredString(ColorGreen, text)
}

// Red returns red colored text.
func (o *Output) Red(text string) string {
	return o.ColoredString(ColorRed, text)
}

// Yellow returns yellow colored text.
func (o *Output) Yellow(text string) string {
	return o.ColoredString(ColorYellow, text)
}

// Cyan returns cyan colored text.
func (o *Output) Cyan(text string) string {
	return o.ColoredString(ColorCyan, text)
}

// BoldText returns bold text.
func (o *Output) BoldText(text string) string {
	return o.ColoredString(ColorBold, text)
}

// DimText returns dimmed text.
func (o *Output) DimText(text string) string {
	return o.ColoredString(ColorDim, text)
}

// Source indicator constants
const (
	SourceZerodha = "ZERODHA"
	SourceAI      = "AI"
	SourceLocal   = "LOCAL"
	SourceCalc    = "CALC"
)

// SourceTag returns a formatted source indicator tag.
func (o *Output) SourceTag(source string) string {
	var color string
	var icon string
	switch source {
	case SourceZerodha:
		color = ColorCyan
		icon = "📡"
	case SourceAI:
		color = ColorMagenta
		icon = "🤖"
	case SourceLocal:
		color = ColorBlue
		icon = "💾"
	case SourceCalc:
		color = ColorYellow
		icon = "📊"
	default:
		color = ColorDim
		icon = "•"
	}
	if o.colorEnabled {
		return fmt.Sprintf("%s[%s%s%s]", icon, color, source, ColorReset)
	}
	return fmt.Sprintf("[%s]", source)
}

// SourceLine prints a line with source indicator.
func (o *Output) SourceLine(source, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(o.writer, "  %s %s\n", o.SourceTag(source), msg)
}

// PnLColor returns the appropriate color for P&L.
func (o *Output) PnLColor(pnl float64) string {
	if pnl > 0 {
		return ColorGreen
	} else if pnl < 0 {
		return ColorRed
	}
	return ColorWhite
}

// FormatPnL formats P&L with color.
func (o *Output) FormatPnL(pnl float64) string {
	formatted := FormatIndianCurrency(pnl)
	if pnl > 0 {
		formatted = "+" + formatted
	}
	return o.ColoredString(o.PnLColor(pnl), formatted)
}

// FormatPercent formats percentage with color.
func (o *Output) FormatPercent(pct float64) string {
	sign := ""
	if pct > 0 {
		sign = "+"
	}
	formatted := fmt.Sprintf("%s%.2f%%", sign, pct)
	return o.ColoredString(o.PnLColor(pct), formatted)
}

// Table represents a simple table for output.
type Table struct {
	headers []string
	rows    [][]string
	output  *Output
}

// NewTable creates a new table.
func NewTable(output *Output, headers ...string) *Table {
	return &Table{
		headers: headers,
		rows:    make([][]string, 0),
		output:  output,
	}
}

// AddRow adds a row to the table.
func (t *Table) AddRow(cells ...string) {
	t.rows = append(t.rows, cells)
}

// Render renders the table.
func (t *Table) Render() {
	if len(t.headers) == 0 {
		return
	}

	// Calculate column widths
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = len(stripANSI(h))
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i < len(widths) {
				cellLen := len(stripANSI(cell))
				if cellLen > widths[i] {
					widths[i] = cellLen
				}
			}
		}
	}

	// Print header
	t.printRow(t.headers, widths, true)
	t.printSeparator(widths)

	// Print rows
	for _, row := range t.rows {
		t.printRow(row, widths, false)
	}
}

func (t *Table) printRow(cells []string, widths []int, isHeader bool) {
	var parts []string
	for i, cell := range cells {
		if i < len(widths) {
			padding := widths[i] - len(stripANSI(cell))
			if padding < 0 {
				padding = 0
			}
			padded := cell + strings.Repeat(" ", padding)
			if isHeader && t.output.colorEnabled {
				padded = ColorBold + padded + ColorReset
			}
			parts = append(parts, padded)
		}
	}
	t.output.Println(strings.Join(parts, "  "))
}

func (t *Table) printSeparator(widths []int) {
	var parts []string
	for _, w := range widths {
		parts = append(parts, strings.Repeat("─", w))
	}
	sep := strings.Join(parts, "──")
	if t.output.colorEnabled {
		sep = ColorDim + sep + ColorReset
	}
	t.output.Println(sep)
}

// stripANSI removes ANSI escape codes from a string.
func stripANSI(s string) string {
	result := s
	// Remove common ANSI escape sequences
	escapes := []string{
		ColorReset, ColorRed, ColorGreen, ColorYellow,
		ColorBlue, ColorMagenta, ColorCyan, ColorWhite,
		ColorBold, ColorDim,
	}
	for _, esc := range escapes {
		result = strings.ReplaceAll(result, esc, "")
	}
	return result
}

// Box draws a box around content.
func (o *Output) Box(title string, content []string) {
	maxLen := len(title)
	for _, line := range content {
		lineLen := len(stripANSI(line))
		if lineLen > maxLen {
			maxLen = lineLen
		}
	}

	width := maxLen + 4
	border := strings.Repeat("─", width-2)

	if o.colorEnabled {
		o.Printf("%s┌%s┐%s\n", ColorDim, border, ColorReset)
		o.Printf("%s│%s %s%s%s%s │%s\n", ColorDim, ColorReset, ColorBold, title, strings.Repeat(" ", width-4-len(title)), ColorDim, ColorReset)
		o.Printf("%s├%s┤%s\n", ColorDim, border, ColorReset)
		for _, line := range content {
			padding := width - 4 - len(stripANSI(line))
			o.Printf("%s│%s %s%s %s│%s\n", ColorDim, ColorReset, line, strings.Repeat(" ", padding), ColorDim, ColorReset)
		}
		o.Printf("%s└%s┘%s\n", ColorDim, border, ColorReset)
	} else {
		o.Printf("+%s+\n", border)
		o.Printf("| %s%s |\n", title, strings.Repeat(" ", width-4-len(title)))
		o.Printf("+%s+\n", border)
		for _, line := range content {
			padding := width - 4 - len(stripANSI(line))
			o.Printf("| %s%s |\n", line, strings.Repeat(" ", padding))
		}
		o.Printf("+%s+\n", border)
	}
}

// Spinner represents a simple spinner for async operations.
type Spinner struct {
	frames  []string
	current int
	message string
	output  *Output
	done    chan bool
}

// NewSpinner creates a new spinner.
func NewSpinner(output *Output, message string) *Spinner {
	return &Spinner{
		frames:  []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		message: message,
		output:  output,
		done:    make(chan bool),
	}
}

// Progress prints a progress indicator.
func (o *Output) Progress(current, total int, message string) {
	pct := float64(current) / float64(total) * 100
	barWidth := 30
	filled := int(float64(barWidth) * float64(current) / float64(total))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	o.Printf("\r%s [%s] %.0f%% ", message, bar, pct)
	if current == total {
		o.Println()
	}
}

// MarketStatus prints market status with appropriate color.
func (o *Output) MarketStatus(status string) string {
	switch status {
	case "OPEN":
		return o.Green("● OPEN")
	case "CLOSED":
		return o.Red("● CLOSED")
	case "PRE_OPEN":
		return o.Yellow("● PRE-OPEN")
	case "MIS_SQUAREOFF_WARNING":
		return o.Yellow("⚠ MIS SQUAREOFF")
	default:
		return status
	}
}

// Recommendation prints a recommendation with appropriate color.
func (o *Output) Recommendation(rec string) string {
	switch rec {
	case "STRONG_BUY":
		return o.Green("📈 STRONG BUY")
	case "BUY":
		return o.Green("↑ BUY")
	case "WEAK_BUY":
		return o.Green("↗ WEAK BUY")
	case "NEUTRAL":
		return o.Yellow("→ NEUTRAL")
	case "WEAK_SELL":
		return o.Red("↘ WEAK SELL")
	case "SELL":
		return o.Red("↓ SELL")
	case "STRONG_SELL":
		return o.Red("📉 STRONG SELL")
	default:
		return rec
	}
}
