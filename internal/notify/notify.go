// Package notify provides notification functionality for the trading application.
package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"zerodha-trader/internal/config"
	"zerodha-trader/internal/models"
)

// Notifier defines the interface for sending notifications.
type Notifier interface {
	Send(ctx context.Context, n Notification) error
	SendTrade(ctx context.Context, trade *models.Trade, decision *models.Decision) error
	SendAlert(ctx context.Context, alert *models.Alert, tick models.Tick) error
	SendDailySummary(ctx context.Context, summary *DailySummary) error
	SendError(ctx context.Context, err error, context string) error
}

// NotificationChannel defines the interface for a notification channel.
type NotificationChannel interface {
	Name() string
	Send(ctx context.Context, n Notification) error
	IsEnabled() bool
}

// Notification represents a notification message.
type Notification struct {
	Type      NotificationType
	Title     string
	Message   string
	Data      map[string]interface{}
	Timestamp time.Time
}

// NotificationType represents the type of notification.
type NotificationType string

const (
	NotificationTrade   NotificationType = "trade"
	NotificationAlert   NotificationType = "alert"
	NotificationError   NotificationType = "error"
	NotificationSummary NotificationType = "summary"
	NotificationInfo    NotificationType = "info"
)

// NotificationLevel represents the notification level filter.
type NotificationLevel string

const (
	LevelAll        NotificationLevel = "all"
	LevelTradesOnly NotificationLevel = "trades_only"
	LevelErrorsOnly NotificationLevel = "errors_only"
)

// DailySummary represents a daily trading summary.
type DailySummary struct {
	Date             string
	TotalTrades      int
	WinningTrades    int
	LosingTrades     int
	TotalPnL         float64
	WinRate          float64
	BestTrade        *TradeSummary
	WorstTrade       *TradeSummary
	TopPerformers    []string
	BottomPerformers []string
}

// TradeSummary represents a summary of a single trade.
type TradeSummary struct {
	Symbol     string
	Side       string
	PnL        float64
	PnLPercent float64
}


// MultiNotifier sends notifications to multiple channels.
type MultiNotifier struct {
	channels []NotificationChannel
	level    NotificationLevel
	mu       sync.RWMutex
}

// formatCurrency formats a currency value with Indian numbering.
func formatCurrency(amount float64) string {
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
	formatted := formatIndianNumber(intPart)

	result := "₹" + formatted + "." + decPart
	if negative {
		result = "-" + result
	}
	return result
}

// formatIndianNumber formats an integer string in Indian numbering system.
func formatIndianNumber(s string) string {
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

// NewMultiNotifier creates a new MultiNotifier with the given configuration.
func NewMultiNotifier(cfg *config.NotificationConfig) *MultiNotifier {
	mn := &MultiNotifier{
		channels: make([]NotificationChannel, 0),
		level:    NotificationLevel(cfg.Level),
	}

	if mn.level == "" {
		mn.level = LevelAll
	}

	// Add enabled channels
	if cfg.Webhook.Enabled {
		mn.channels = append(mn.channels, NewWebhookNotifier(cfg.Webhook))
	}
	if cfg.Telegram.Enabled {
		mn.channels = append(mn.channels, NewTelegramNotifier(cfg.Telegram))
	}
	if cfg.Email.Enabled {
		mn.channels = append(mn.channels, NewEmailNotifier(cfg.Email))
	}

	return mn
}

// AddChannel adds a notification channel.
func (mn *MultiNotifier) AddChannel(ch NotificationChannel) {
	mn.mu.Lock()
	defer mn.mu.Unlock()
	mn.channels = append(mn.channels, ch)
}

// shouldSend checks if a notification should be sent based on the level filter.
func (mn *MultiNotifier) shouldSend(notifType NotificationType) bool {
	switch mn.level {
	case LevelTradesOnly:
		return notifType == NotificationTrade
	case LevelErrorsOnly:
		return notifType == NotificationError
	default:
		return true
	}
}

// Send sends a notification to all enabled channels.
func (mn *MultiNotifier) Send(ctx context.Context, n Notification) error {
	if !mn.shouldSend(n.Type) {
		return nil
	}

	if n.Timestamp.IsZero() {
		n.Timestamp = time.Now()
	}

	mn.mu.RLock()
	channels := mn.channels
	mn.mu.RUnlock()

	var errs []string
	for _, ch := range channels {
		if ch.IsEnabled() {
			if err := ch.Send(ctx, n); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", ch.Name(), err))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("notification errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// SendTrade sends a trade notification.
func (mn *MultiNotifier) SendTrade(ctx context.Context, trade *models.Trade, decision *models.Decision) error {
	pnlSign := "+"
	if trade.PnL < 0 {
		pnlSign = ""
	}

	title := fmt.Sprintf("🔔 Trade Executed: %s %s", trade.Side, trade.Symbol)
	message := fmt.Sprintf(
		"Symbol: %s\nAction: %s\nQuantity: %d\nEntry: %s\nExit: %s\nP&L: %s%s (%.2f%%)",
		trade.Symbol,
		trade.Side,
		trade.Quantity,
		formatCurrency(trade.EntryPrice),
		formatCurrency(trade.ExitPrice),
		pnlSign,
		formatCurrency(trade.PnL),
		trade.PnLPercent,
	)

	if decision != nil && decision.Reasoning != "" {
		message += fmt.Sprintf("\n\nReasoning: %s", decision.Reasoning)
	}

	data := map[string]interface{}{
		"symbol":      trade.Symbol,
		"side":        trade.Side,
		"quantity":    trade.Quantity,
		"entry_price": trade.EntryPrice,
		"exit_price":  trade.ExitPrice,
		"pnl":         trade.PnL,
		"pnl_percent": trade.PnLPercent,
	}

	if decision != nil {
		data["confidence"] = decision.Confidence
		data["reasoning"] = decision.Reasoning
	}

	return mn.Send(ctx, Notification{
		Type:    NotificationTrade,
		Title:   title,
		Message: message,
		Data:    data,
	})
}

// SendAlert sends an alert notification.
func (mn *MultiNotifier) SendAlert(ctx context.Context, alert *models.Alert, tick models.Tick) error {
	var emoji string
	switch alert.Condition {
	case "above":
		emoji = "📈"
	case "below":
		emoji = "📉"
	default:
		emoji = "⚠️"
	}

	title := fmt.Sprintf("%s Alert Triggered: %s", emoji, alert.Symbol)
	message := fmt.Sprintf(
		"Symbol: %s\nCondition: Price %s %s\nCurrent Price: %s\nTriggered at: %s",
		alert.Symbol,
		alert.Condition,
		formatCurrency(alert.Price),
		formatCurrency(tick.LTP),
		time.Now().Format("15:04:05"),
	)

	return mn.Send(ctx, Notification{
		Type:    NotificationAlert,
		Title:   title,
		Message: message,
		Data: map[string]interface{}{
			"symbol":        alert.Symbol,
			"condition":     alert.Condition,
			"trigger_price": alert.Price,
			"current_price": tick.LTP,
		},
	})
}

// SendDailySummary sends a daily summary notification.
func (mn *MultiNotifier) SendDailySummary(ctx context.Context, summary *DailySummary) error {
	pnlEmoji := "📊"
	if summary.TotalPnL > 0 {
		pnlEmoji = "💰"
	} else if summary.TotalPnL < 0 {
		pnlEmoji = "📉"
	}

	title := fmt.Sprintf("%s Daily Summary - %s", pnlEmoji, summary.Date)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Total Trades: %d\n", summary.TotalTrades))
	sb.WriteString(fmt.Sprintf("Winning: %d | Losing: %d\n", summary.WinningTrades, summary.LosingTrades))
	sb.WriteString(fmt.Sprintf("Win Rate: %.1f%%\n", summary.WinRate))
	sb.WriteString(fmt.Sprintf("Total P&L: %s\n", formatCurrency(summary.TotalPnL)))

	if summary.BestTrade != nil {
		sb.WriteString(fmt.Sprintf("\n🏆 Best Trade: %s %s (+%s)",
			summary.BestTrade.Side, summary.BestTrade.Symbol,
			formatCurrency(summary.BestTrade.PnL)))
	}
	if summary.WorstTrade != nil {
		sb.WriteString(fmt.Sprintf("\n📉 Worst Trade: %s %s (%s)",
			summary.WorstTrade.Side, summary.WorstTrade.Symbol,
			formatCurrency(summary.WorstTrade.PnL)))
	}

	return mn.Send(ctx, Notification{
		Type:    NotificationSummary,
		Title:   title,
		Message: sb.String(),
		Data: map[string]interface{}{
			"date":           summary.Date,
			"total_trades":   summary.TotalTrades,
			"winning_trades": summary.WinningTrades,
			"losing_trades":  summary.LosingTrades,
			"total_pnl":      summary.TotalPnL,
			"win_rate":       summary.WinRate,
		},
	})
}

// SendError sends an error notification.
func (mn *MultiNotifier) SendError(ctx context.Context, err error, errContext string) error {
	title := "❌ Error Occurred"
	message := fmt.Sprintf("Context: %s\nError: %v\nTime: %s",
		errContext, err, time.Now().Format("15:04:05"))

	return mn.Send(ctx, Notification{
		Type:    NotificationError,
		Title:   title,
		Message: message,
		Data: map[string]interface{}{
			"context": errContext,
			"error":   err.Error(),
		},
	})
}


// WebhookNotifier sends notifications via HTTP webhook.
type WebhookNotifier struct {
	url     string
	enabled bool
	client  *http.Client
}

// NewWebhookNotifier creates a new WebhookNotifier.
func NewWebhookNotifier(cfg config.WebhookConfig) *WebhookNotifier {
	return &WebhookNotifier{
		url:     cfg.URL,
		enabled: cfg.Enabled && cfg.URL != "",
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Name returns the name of the notifier.
func (w *WebhookNotifier) Name() string {
	return "webhook"
}

// IsEnabled returns whether the notifier is enabled.
func (w *WebhookNotifier) IsEnabled() bool {
	return w.enabled
}

// Send sends a notification via webhook.
func (w *WebhookNotifier) Send(ctx context.Context, n Notification) error {
	if !w.enabled {
		return nil
	}

	payload := map[string]interface{}{
		"type":      n.Type,
		"title":     n.Title,
		"message":   n.Message,
		"data":      n.Data,
		"timestamp": n.Timestamp.Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ZerodhaTrader/1.0")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// TelegramNotifier sends notifications via Telegram bot.
type TelegramNotifier struct {
	botToken string
	chatID   string
	enabled  bool
	client   *http.Client
}

// NewTelegramNotifier creates a new TelegramNotifier.
func NewTelegramNotifier(cfg config.TelegramConfig) *TelegramNotifier {
	return &TelegramNotifier{
		botToken: cfg.BotToken,
		chatID:   cfg.ChatID,
		enabled:  cfg.Enabled && cfg.BotToken != "" && cfg.ChatID != "",
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Name returns the name of the notifier.
func (t *TelegramNotifier) Name() string {
	return "telegram"
}

// IsEnabled returns whether the notifier is enabled.
func (t *TelegramNotifier) IsEnabled() bool {
	return t.enabled
}

// Send sends a notification via Telegram.
func (t *TelegramNotifier) Send(ctx context.Context, n Notification) error {
	if !t.enabled {
		return nil
	}

	// Format message for Telegram (using HTML parse mode)
	text := fmt.Sprintf("<b>%s</b>\n\n%s", escapeHTML(n.Title), escapeHTML(n.Message))

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.botToken)

	payload := map[string]interface{}{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling telegram payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating telegram request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending telegram message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}

	return nil
}

// escapeHTML escapes HTML special characters for Telegram.
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}


// EmailNotifier sends notifications via email using SMTP.
type EmailNotifier struct {
	smtpHost string
	smtpPort int
	username string
	password string
	from     string
	to       string
	enabled  bool
}

// NewEmailNotifier creates a new EmailNotifier.
func NewEmailNotifier(cfg config.EmailConfig) *EmailNotifier {
	return &EmailNotifier{
		smtpHost: cfg.SMTPHost,
		smtpPort: cfg.SMTPPort,
		username: cfg.Username,
		password: cfg.Password,
		from:     cfg.From,
		to:       cfg.To,
		enabled:  cfg.Enabled && cfg.SMTPHost != "" && cfg.From != "" && cfg.To != "",
	}
}

// Name returns the name of the notifier.
func (e *EmailNotifier) Name() string {
	return "email"
}

// IsEnabled returns whether the notifier is enabled.
func (e *EmailNotifier) IsEnabled() bool {
	return e.enabled
}

// Send sends a notification via email.
func (e *EmailNotifier) Send(ctx context.Context, n Notification) error {
	if !e.enabled {
		return nil
	}

	// Build email message
	subject := n.Title
	body := n.Message

	// Add data as JSON if present
	if len(n.Data) > 0 {
		dataJSON, _ := json.MarshalIndent(n.Data, "", "  ")
		body += "\n\n---\nData:\n" + string(dataJSON)
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		e.from, e.to, subject, body)

	addr := fmt.Sprintf("%s:%d", e.smtpHost, e.smtpPort)

	var auth smtp.Auth
	if e.username != "" && e.password != "" {
		auth = smtp.PlainAuth("", e.username, e.password, e.smtpHost)
	}

	// Use TLS for secure connection
	if e.smtpPort == 465 {
		return e.sendWithTLS(addr, auth, msg)
	}

	// Use STARTTLS for port 587 or plain for others
	return smtp.SendMail(addr, auth, e.from, []string{e.to}, []byte(msg))
}

// sendWithTLS sends email using implicit TLS (port 465).
func (e *EmailNotifier) sendWithTLS(addr string, auth smtp.Auth, msg string) error {
	tlsConfig := &tls.Config{
		ServerName: e.smtpHost,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("TLS dial failed: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, e.smtpHost)
	if err != nil {
		return fmt.Errorf("creating SMTP client: %w", err)
	}
	defer client.Close()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth failed: %w", err)
		}
	}

	if err := client.Mail(e.from); err != nil {
		return fmt.Errorf("SMTP MAIL command failed: %w", err)
	}

	if err := client.Rcpt(e.to); err != nil {
		return fmt.Errorf("SMTP RCPT command failed: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA command failed: %w", err)
	}

	_, err = w.Write([]byte(msg))
	if err != nil {
		return fmt.Errorf("writing email body: %w", err)
	}

	err = w.Close()
	if err != nil {
		return fmt.Errorf("closing email body: %w", err)
	}

	return client.Quit()
}

// NoOpNotifier is a notifier that does nothing (for testing or disabled notifications).
type NoOpNotifier struct{}

// NewNoOpNotifier creates a new NoOpNotifier.
func NewNoOpNotifier() *NoOpNotifier {
	return &NoOpNotifier{}
}

// Send does nothing.
func (n *NoOpNotifier) Send(ctx context.Context, notif Notification) error {
	return nil
}

// SendTrade does nothing.
func (n *NoOpNotifier) SendTrade(ctx context.Context, trade *models.Trade, decision *models.Decision) error {
	return nil
}

// SendAlert does nothing.
func (n *NoOpNotifier) SendAlert(ctx context.Context, alert *models.Alert, tick models.Tick) error {
	return nil
}

// SendDailySummary does nothing.
func (n *NoOpNotifier) SendDailySummary(ctx context.Context, summary *DailySummary) error {
	return nil
}

// SendError does nothing.
func (n *NoOpNotifier) SendError(ctx context.Context, err error, context string) error {
	return nil
}
