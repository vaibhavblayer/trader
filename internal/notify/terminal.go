// Package notify provides notification functionality for the trading application.
package notify

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"zerodha-trader/internal/models"
)

// TerminalNotificationType represents the type of terminal notification.
type TerminalNotificationType int

// formatCurrencyTerminal formats a currency value with Indian numbering.
func formatCurrencyTerminal(amount float64) string {
	negative := amount < 0
	if negative {
		amount = -amount
	}

	// Format with 2 decimal places
	str := fmt.Sprintf("%.2f", amount)
	parts := strings.Split(str, ".")
	intPart := parts[0]
	decPart := parts[1]

	// Apply Indian numbering system
	formatted := formatIndianNumberTerminal(intPart)

	result := "₹" + formatted + "." + decPart
	if negative {
		result = "-" + result
	}
	return result
}

// formatIndianNumberTerminal formats an integer string in Indian numbering system.
func formatIndianNumberTerminal(s string) string {
	n := len(s)
	if n <= 3 {
		return s
	}

	// First group of 3 from right
	result := s[n-3:]
	s = s[:n-3]

	// Then groups of 2
	for len(s) > 0 {
		if len(s) >= 2 {
			result = s[len(s)-2:] + "," + result
			s = s[:len(s)-2]
		} else {
			result = s + "," + result
			s = ""
		}
	}

	return result
}

const (
	TerminalNotifyEntry TerminalNotificationType = iota
	TerminalNotifyStopLoss
	TerminalNotifyTarget
	TerminalNotifyAlert
	TerminalNotifyTrade
	TerminalNotifyError
	TerminalNotifyInfo
)

// TerminalNotification represents a notification to be displayed in the terminal.
type TerminalNotification struct {
	Type         TerminalNotificationType
	Symbol       string
	Message      string
	CurrentPrice float64
	TriggerPrice float64
	Distance     float64 // Percentage distance from trigger
	Timestamp    time.Time
	Priority     int // Higher = more important
	Action       string
}

// TerminalNotifier handles real-time terminal notifications.
type TerminalNotifier struct {
	notifications chan TerminalNotification
	handlers      []TerminalNotificationHandler
	mu            sync.RWMutex
	enabled       bool
	bellEnabled   bool
	colorEnabled  bool
	voiceEnabled  bool
}

// TerminalNotificationHandler is a function that handles terminal notifications.
type TerminalNotificationHandler func(n TerminalNotification)

// NewTerminalNotifier creates a new TerminalNotifier.
func NewTerminalNotifier(bufferSize int) *TerminalNotifier {
	if bufferSize <= 0 {
		bufferSize = 100
	}
	return &TerminalNotifier{
		notifications: make(chan TerminalNotification, bufferSize),
		handlers:      make([]TerminalNotificationHandler, 0),
		enabled:       true,
		bellEnabled:   true,
		colorEnabled:  true,
		voiceEnabled:  true,
	}
}

// SetBellEnabled enables or disables the terminal bell.
func (tn *TerminalNotifier) SetBellEnabled(enabled bool) {
	tn.mu.Lock()
	defer tn.mu.Unlock()
	tn.bellEnabled = enabled
}

// SetColorEnabled enables or disables colored output.
func (tn *TerminalNotifier) SetColorEnabled(enabled bool) {
	tn.mu.Lock()
	defer tn.mu.Unlock()
	tn.colorEnabled = enabled
}

// SetVoiceEnabled enables or disables voice notifications.
func (tn *TerminalNotifier) SetVoiceEnabled(enabled bool) {
	tn.mu.Lock()
	defer tn.mu.Unlock()
	tn.voiceEnabled = enabled
}

// SetEnabled enables or disables the notifier.
func (tn *TerminalNotifier) SetEnabled(enabled bool) {
	tn.mu.Lock()
	defer tn.mu.Unlock()
	tn.enabled = enabled
}

// AddHandler adds a notification handler.
func (tn *TerminalNotifier) AddHandler(handler TerminalNotificationHandler) {
	tn.mu.Lock()
	defer tn.mu.Unlock()
	tn.handlers = append(tn.handlers, handler)
}

// Notify sends a notification to the terminal.
func (tn *TerminalNotifier) Notify(n TerminalNotification) {
	tn.mu.RLock()
	enabled := tn.enabled
	tn.mu.RUnlock()

	if !enabled {
		return
	}

	if n.Timestamp.IsZero() {
		n.Timestamp = time.Now()
	}

	select {
	case tn.notifications <- n:
	default:
		// Buffer full, drop oldest notification
		select {
		case <-tn.notifications:
		default:
		}
		tn.notifications <- n
	}
}

// Start starts processing notifications.
func (tn *TerminalNotifier) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case n := <-tn.notifications:
				tn.processNotification(n)
			}
		}
	}()
}

// processNotification processes a single notification.
func (tn *TerminalNotifier) processNotification(n TerminalNotification) {
	tn.mu.RLock()
	handlers := tn.handlers
	bellEnabled := tn.bellEnabled
	voiceEnabled := tn.voiceEnabled
	tn.mu.RUnlock()

	// Ring bell for important notifications
	if bellEnabled && n.Priority > 0 {
		fmt.Print("\a") // Terminal bell
	}

	// Voice notification for important alerts (macOS)
	if voiceEnabled && n.Priority >= 2 {
		go speakNotification(n)
	}

	// Call all registered handlers
	for _, handler := range handlers {
		handler(n)
	}
}

// speakNotification uses macOS 'say' command for voice notifications (non-blocking).
func speakNotification(n TerminalNotification) {
	var text string
	switch n.Type {
	case TerminalNotifyEntry:
		text = fmt.Sprintf("%s approaching entry at %.0f", n.Symbol, n.TriggerPrice)
	case TerminalNotifyStopLoss:
		text = fmt.Sprintf("Warning! %s approaching stop loss at %.0f", n.Symbol, n.TriggerPrice)
	case TerminalNotifyTarget:
		text = fmt.Sprintf("%s approaching target at %.0f. Consider booking profits", n.Symbol, n.TriggerPrice)
	case TerminalNotifyAlert:
		text = fmt.Sprintf("Alert! %s", n.Message)
	case TerminalNotifyTrade:
		text = fmt.Sprintf("Trade executed. %s", n.Message)
	case TerminalNotifyError:
		text = fmt.Sprintf("Error! %s", n.Message)
	default:
		return // Don't speak info notifications
	}
	exec.Command("say", text).Start() // Non-blocking, uses default Siri voice
}

// GetNotificationChannel returns the notification channel for custom processing.
func (tn *TerminalNotifier) GetNotificationChannel() <-chan TerminalNotification {
	return tn.notifications
}


// NotifyPlanEntry sends a notification when price approaches a planned entry level.
func (tn *TerminalNotifier) NotifyPlanEntry(plan *models.TradePlan, tick models.Tick) {
	distance := ((tick.LTP - plan.EntryPrice) / plan.EntryPrice) * 100

	tn.Notify(TerminalNotification{
		Type:         TerminalNotifyEntry,
		Symbol:       plan.Symbol,
		Message:      fmt.Sprintf("Price approaching entry level"),
		CurrentPrice: tick.LTP,
		TriggerPrice: plan.EntryPrice,
		Distance:     distance,
		Priority:     2,
		Action:       fmt.Sprintf("Consider %s at %s", plan.Side, formatCurrencyTerminal(plan.EntryPrice)),
	})
}

// NotifyPlanStopLoss sends a notification when price approaches stop-loss level.
func (tn *TerminalNotifier) NotifyPlanStopLoss(plan *models.TradePlan, tick models.Tick) {
	distance := ((tick.LTP - plan.StopLoss) / plan.StopLoss) * 100

	tn.Notify(TerminalNotification{
		Type:         TerminalNotifyStopLoss,
		Symbol:       plan.Symbol,
		Message:      fmt.Sprintf("⚠️ Price approaching STOP-LOSS"),
		CurrentPrice: tick.LTP,
		TriggerPrice: plan.StopLoss,
		Distance:     distance,
		Priority:     3, // High priority
		Action:       "Review position and consider exit",
	})
}

// NotifyPlanTarget sends a notification when price approaches target level.
func (tn *TerminalNotifier) NotifyPlanTarget(plan *models.TradePlan, tick models.Tick, targetNum int, targetPrice float64) {
	distance := ((tick.LTP - targetPrice) / targetPrice) * 100

	tn.Notify(TerminalNotification{
		Type:         TerminalNotifyTarget,
		Symbol:       plan.Symbol,
		Message:      fmt.Sprintf("🎯 Price approaching Target %d", targetNum),
		CurrentPrice: tick.LTP,
		TriggerPrice: targetPrice,
		Distance:     distance,
		Priority:     2,
		Action:       fmt.Sprintf("Consider booking profits at %s", formatCurrencyTerminal(targetPrice)),
	})
}

// NotifyAlert sends a notification when an alert is triggered.
func (tn *TerminalNotifier) NotifyAlert(alert *models.Alert, tick models.Tick) {
	var emoji string
	switch alert.Condition {
	case "above":
		emoji = "📈"
	case "below":
		emoji = "📉"
	default:
		emoji = "🔔"
	}

	tn.Notify(TerminalNotification{
		Type:         TerminalNotifyAlert,
		Symbol:       alert.Symbol,
		Message:      fmt.Sprintf("%s Alert: Price %s %s", emoji, alert.Condition, formatCurrencyTerminal(alert.Price)),
		CurrentPrice: tick.LTP,
		TriggerPrice: alert.Price,
		Priority:     2,
	})
}

// NotifyTrade sends a notification for a trade execution.
func (tn *TerminalNotifier) NotifyTrade(trade *models.Trade) {
	var emoji string
	if trade.PnL >= 0 {
		emoji = "✅"
	} else {
		emoji = "❌"
	}

	tn.Notify(TerminalNotification{
		Type:         TerminalNotifyTrade,
		Symbol:       trade.Symbol,
		Message:      fmt.Sprintf("%s Trade executed: %s %d @ %s", emoji, trade.Side, trade.Quantity, formatCurrencyTerminal(trade.EntryPrice)),
		CurrentPrice: trade.EntryPrice,
		Priority:     2,
	})
}

// NotifyError sends an error notification.
func (tn *TerminalNotifier) NotifyError(err error, context string) {
	tn.Notify(TerminalNotification{
		Type:     TerminalNotifyError,
		Message:  fmt.Sprintf("❌ Error in %s: %v", context, err),
		Priority: 3,
	})
}

// NotifyInfo sends an informational notification.
func (tn *TerminalNotifier) NotifyInfo(message string) {
	tn.Notify(TerminalNotification{
		Type:     TerminalNotifyInfo,
		Message:  fmt.Sprintf("ℹ️ %s", message),
		Priority: 1,
	})
}

// FormatNotification formats a notification for terminal display.
func FormatNotification(n TerminalNotification, colorEnabled bool) string {
	var sb strings.Builder

	// Timestamp
	timestamp := n.Timestamp.Format("15:04:05")

	// Type indicator and color
	var typeIndicator, color, resetColor string
	if colorEnabled {
		resetColor = "\033[0m"
	}

	switch n.Type {
	case TerminalNotifyEntry:
		typeIndicator = "📥 ENTRY"
		if colorEnabled {
			color = "\033[36m" // Cyan
		}
	case TerminalNotifyStopLoss:
		typeIndicator = "🛑 STOP-LOSS"
		if colorEnabled {
			color = "\033[31m" // Red
		}
	case TerminalNotifyTarget:
		typeIndicator = "🎯 TARGET"
		if colorEnabled {
			color = "\033[32m" // Green
		}
	case TerminalNotifyAlert:
		typeIndicator = "🔔 ALERT"
		if colorEnabled {
			color = "\033[33m" // Yellow
		}
	case TerminalNotifyTrade:
		typeIndicator = "💹 TRADE"
		if colorEnabled {
			color = "\033[35m" // Magenta
		}
	case TerminalNotifyError:
		typeIndicator = "❌ ERROR"
		if colorEnabled {
			color = "\033[31m" // Red
		}
	case TerminalNotifyInfo:
		typeIndicator = "ℹ️  INFO"
		if colorEnabled {
			color = "\033[37m" // White
		}
	}

	// Build notification string
	sb.WriteString(fmt.Sprintf("%s[%s] %s%s", color, timestamp, typeIndicator, resetColor))

	if n.Symbol != "" {
		sb.WriteString(fmt.Sprintf(" | %s", n.Symbol))
	}

	sb.WriteString(fmt.Sprintf(" | %s", n.Message))

	if n.CurrentPrice > 0 && n.TriggerPrice > 0 {
		sb.WriteString(fmt.Sprintf(" | LTP: %s → Trigger: %s (%.2f%%)",
			formatCurrencyTerminal(n.CurrentPrice),
			formatCurrencyTerminal(n.TriggerPrice),
			n.Distance))
	}

	if n.Action != "" {
		sb.WriteString(fmt.Sprintf("\n    → %s", n.Action))
	}

	return sb.String()
}

// DefaultTerminalHandler returns a default handler that prints to stdout.
func DefaultTerminalHandler(colorEnabled bool) TerminalNotificationHandler {
	return func(n TerminalNotification) {
		fmt.Println(FormatNotification(n, colorEnabled))
	}
}

// NotificationOverlay represents a non-blocking notification overlay for watch mode.
type NotificationOverlay struct {
	notifications []TerminalNotification
	maxVisible    int
	mu            sync.RWMutex
	ttl           time.Duration
}

// NewNotificationOverlay creates a new notification overlay.
func NewNotificationOverlay(maxVisible int, ttl time.Duration) *NotificationOverlay {
	if maxVisible <= 0 {
		maxVisible = 5
	}
	if ttl <= 0 {
		ttl = 10 * time.Second
	}
	return &NotificationOverlay{
		notifications: make([]TerminalNotification, 0, maxVisible),
		maxVisible:    maxVisible,
		ttl:           ttl,
	}
}

// Add adds a notification to the overlay.
func (no *NotificationOverlay) Add(n TerminalNotification) {
	no.mu.Lock()
	defer no.mu.Unlock()

	// Remove expired notifications
	now := time.Now()
	active := make([]TerminalNotification, 0, len(no.notifications))
	for _, notif := range no.notifications {
		if now.Sub(notif.Timestamp) < no.ttl {
			active = append(active, notif)
		}
	}
	no.notifications = active

	// Add new notification
	no.notifications = append(no.notifications, n)

	// Keep only maxVisible notifications
	if len(no.notifications) > no.maxVisible {
		no.notifications = no.notifications[len(no.notifications)-no.maxVisible:]
	}
}

// GetVisible returns the currently visible notifications.
func (no *NotificationOverlay) GetVisible() []TerminalNotification {
	no.mu.RLock()
	defer no.mu.RUnlock()

	// Filter expired notifications
	now := time.Now()
	visible := make([]TerminalNotification, 0, len(no.notifications))
	for _, n := range no.notifications {
		if now.Sub(n.Timestamp) < no.ttl {
			visible = append(visible, n)
		}
	}
	return visible
}

// Clear clears all notifications.
func (no *NotificationOverlay) Clear() {
	no.mu.Lock()
	defer no.mu.Unlock()
	no.notifications = no.notifications[:0]
}

// Render renders the overlay as a string for display.
func (no *NotificationOverlay) Render(colorEnabled bool) string {
	visible := no.GetVisible()
	if len(visible) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("┌─ Notifications ─────────────────────────────────────────────────────────┐\n")

	for _, n := range visible {
		line := FormatNotification(n, colorEnabled)
		// Truncate long lines
		if len(line) > 75 {
			line = line[:72] + "..."
		}
		sb.WriteString(fmt.Sprintf("│ %-75s │\n", line))
	}

	sb.WriteString("└─────────────────────────────────────────────────────────────────────────┘")
	return sb.String()
}

// PlanMonitorNotifier monitors trade plans and sends notifications.
type PlanMonitorNotifier struct {
	terminalNotifier *TerminalNotifier
	thresholdPercent float64 // Distance threshold to trigger notification
	notifiedLevels   map[string]map[string]bool // symbol -> level -> notified
	mu               sync.RWMutex
}

// NewPlanMonitorNotifier creates a new PlanMonitorNotifier.
func NewPlanMonitorNotifier(tn *TerminalNotifier, thresholdPercent float64) *PlanMonitorNotifier {
	if thresholdPercent <= 0 {
		thresholdPercent = 1.0 // Default 1% threshold
	}
	return &PlanMonitorNotifier{
		terminalNotifier: tn,
		thresholdPercent: thresholdPercent,
		notifiedLevels:   make(map[string]map[string]bool),
	}
}

// CheckPlan checks a trade plan against current price and sends notifications.
func (pm *PlanMonitorNotifier) CheckPlan(plan *models.TradePlan, tick models.Tick) {
	pm.mu.Lock()
	if pm.notifiedLevels[plan.Symbol] == nil {
		pm.notifiedLevels[plan.Symbol] = make(map[string]bool)
	}
	notified := pm.notifiedLevels[plan.Symbol]
	pm.mu.Unlock()

	// Check entry level
	if !notified["entry"] && plan.EntryPrice > 0 {
		distance := absPercent(tick.LTP, plan.EntryPrice)
		if distance <= pm.thresholdPercent {
			pm.terminalNotifier.NotifyPlanEntry(plan, tick)
			pm.markNotified(plan.Symbol, "entry")
		}
	}

	// Check stop-loss level
	if !notified["sl"] && plan.StopLoss > 0 {
		distance := absPercent(tick.LTP, plan.StopLoss)
		if distance <= pm.thresholdPercent {
			pm.terminalNotifier.NotifyPlanStopLoss(plan, tick)
			pm.markNotified(plan.Symbol, "sl")
		}
	}

	// Check target levels
	targets := []struct {
		price float64
		key   string
		num   int
	}{
		{plan.Target1, "t1", 1},
		{plan.Target2, "t2", 2},
		{plan.Target3, "t3", 3},
	}

	for _, t := range targets {
		if !notified[t.key] && t.price > 0 {
			distance := absPercent(tick.LTP, t.price)
			if distance <= pm.thresholdPercent {
				pm.terminalNotifier.NotifyPlanTarget(plan, tick, t.num, t.price)
				pm.markNotified(plan.Symbol, t.key)
			}
		}
	}
}

// markNotified marks a level as notified.
func (pm *PlanMonitorNotifier) markNotified(symbol, level string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.notifiedLevels[symbol] == nil {
		pm.notifiedLevels[symbol] = make(map[string]bool)
	}
	pm.notifiedLevels[symbol][level] = true
}

// ResetNotifications resets notification state for a symbol.
func (pm *PlanMonitorNotifier) ResetNotifications(symbol string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.notifiedLevels, symbol)
}

// ResetAllNotifications resets all notification states.
func (pm *PlanMonitorNotifier) ResetAllNotifications() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.notifiedLevels = make(map[string]map[string]bool)
}

// absPercent calculates the absolute percentage difference between two values.
func absPercent(current, target float64) float64 {
	if target == 0 {
		return 100
	}
	diff := ((current - target) / target) * 100
	if diff < 0 {
		return -diff
	}
	return diff
}
