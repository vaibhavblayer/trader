// Package security provides credential encryption, audit logging, and security controls.
package security

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// AuditEventType represents the type of audit event.
type AuditEventType string

const (
	// Authentication events
	AuditLogin          AuditEventType = "LOGIN"
	AuditLogout         AuditEventType = "LOGOUT"
	AuditSessionExpired AuditEventType = "SESSION_EXPIRED"
	AuditAuthFailed     AuditEventType = "AUTH_FAILED"

	// Trading events
	AuditOrderPlaced    AuditEventType = "ORDER_PLACED"
	AuditOrderModified  AuditEventType = "ORDER_MODIFIED"
	AuditOrderCancelled AuditEventType = "ORDER_CANCELLED"
	AuditOrderExecuted  AuditEventType = "ORDER_EXECUTED"
	AuditOrderRejected  AuditEventType = "ORDER_REJECTED"

	// Position events
	AuditPositionOpened AuditEventType = "POSITION_OPENED"
	AuditPositionClosed AuditEventType = "POSITION_CLOSED"
	AuditPositionExited AuditEventType = "POSITION_EXITED"

	// AI decision events
	AuditAIDecision     AuditEventType = "AI_DECISION"
	AuditAIAutoExecute  AuditEventType = "AI_AUTO_EXECUTE"
	AuditAIManualReview AuditEventType = "AI_MANUAL_REVIEW"

	// Configuration events
	AuditConfigChanged AuditEventType = "CONFIG_CHANGED"
	AuditModeChanged   AuditEventType = "MODE_CHANGED"

	// Security events
	AuditCredentialAccess  AuditEventType = "CREDENTIAL_ACCESS"
	AuditReadOnlyViolation AuditEventType = "READ_ONLY_VIOLATION"
	AuditInputValidation   AuditEventType = "INPUT_VALIDATION"
)

// AuditEvent represents a single audit log entry.
type AuditEvent struct {
	Timestamp   time.Time              `json:"timestamp"`
	EventType   AuditEventType         `json:"event_type"`
	UserID      string                 `json:"user_id,omitempty"`
	Symbol      string                 `json:"symbol,omitempty"`
	OrderID     string                 `json:"order_id,omitempty"`
	Action      string                 `json:"action,omitempty"`
	Details     map[string]interface{} `json:"details,omitempty"`
	Success     bool                   `json:"success"`
	ErrorMsg    string                 `json:"error,omitempty"`
	IPAddress   string                 `json:"ip_address,omitempty"`
	SessionID   string                 `json:"session_id,omitempty"`
	RequestID   string                 `json:"request_id,omitempty"`
}

// AuditLogger handles audit logging for trading actions.
type AuditLogger struct {
	writer    *lumberjack.Logger
	mu        sync.Mutex
	sessionID string
	userID    string
}

// AuditConfig holds audit logger configuration.
type AuditConfig struct {
	LogDir     string
	MaxSize    int // megabytes
	MaxBackups int
	MaxAge     int // days
	Compress   bool
}

// DefaultAuditConfig returns the default audit configuration.
func DefaultAuditConfig() AuditConfig {
	home, _ := os.UserHomeDir()
	return AuditConfig{
		LogDir:     filepath.Join(home, ".config", "zerodha-trader", "audit"),
		MaxSize:    50,
		MaxBackups: 30,
		MaxAge:     365, // Keep audit logs for 1 year
		Compress:   true,
	}
}

// NewAuditLogger creates a new audit logger.
func NewAuditLogger(cfg AuditConfig) (*AuditLogger, error) {
	// Ensure audit directory exists with restricted permissions
	if err := os.MkdirAll(cfg.LogDir, 0700); err != nil {
		return nil, fmt.Errorf("creating audit directory: %w", err)
	}

	logPath := filepath.Join(cfg.LogDir, "audit.log")

	writer := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    cfg.MaxSize,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAge,
		Compress:   cfg.Compress,
	}

	return &AuditLogger{
		writer:    writer,
		sessionID: generateSessionID(),
	}, nil
}

// SetUserID sets the user ID for audit events.
func (al *AuditLogger) SetUserID(userID string) {
	al.mu.Lock()
	defer al.mu.Unlock()
	al.userID = userID
}

// Log logs an audit event.
func (al *AuditLogger) Log(ctx context.Context, event AuditEvent) error {
	al.mu.Lock()
	defer al.mu.Unlock()

	// Set common fields
	event.Timestamp = time.Now().UTC()
	event.SessionID = al.sessionID
	if event.UserID == "" {
		event.UserID = al.userID
	}

	// Get request ID from context if available
	if reqID, ok := ctx.Value("request_id").(string); ok {
		event.RequestID = reqID
	}

	// Serialize to JSON
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("serializing audit event: %w", err)
	}

	// Write with newline
	if _, err := al.writer.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing audit event: %w", err)
	}

	return nil
}

// LogLogin logs a login event.
func (al *AuditLogger) LogLogin(ctx context.Context, userID string, success bool, errorMsg string) error {
	return al.Log(ctx, AuditEvent{
		EventType: AuditLogin,
		UserID:    userID,
		Success:   success,
		ErrorMsg:  errorMsg,
	})
}

// LogLogout logs a logout event.
func (al *AuditLogger) LogLogout(ctx context.Context, userID string) error {
	return al.Log(ctx, AuditEvent{
		EventType: AuditLogout,
		UserID:    userID,
		Success:   true,
	})
}

// LogOrderPlaced logs an order placement event.
func (al *AuditLogger) LogOrderPlaced(ctx context.Context, orderID, symbol, side string, qty int, price float64, orderType, product string, success bool, errorMsg string) error {
	return al.Log(ctx, AuditEvent{
		EventType: AuditOrderPlaced,
		OrderID:   orderID,
		Symbol:    symbol,
		Action:    side,
		Success:   success,
		ErrorMsg:  errorMsg,
		Details: map[string]interface{}{
			"quantity":   qty,
			"price":      price,
			"order_type": orderType,
			"product":    product,
		},
	})
}

// LogOrderCancelled logs an order cancellation event.
func (al *AuditLogger) LogOrderCancelled(ctx context.Context, orderID, symbol string, success bool, errorMsg string) error {
	return al.Log(ctx, AuditEvent{
		EventType: AuditOrderCancelled,
		OrderID:   orderID,
		Symbol:    symbol,
		Success:   success,
		ErrorMsg:  errorMsg,
	})
}

// LogAIDecision logs an AI trading decision.
func (al *AuditLogger) LogAIDecision(ctx context.Context, symbol, action, reasoning string, confidence float64, autoExecuted bool) error {
	eventType := AuditAIDecision
	if autoExecuted {
		eventType = AuditAIAutoExecute
	}

	return al.Log(ctx, AuditEvent{
		EventType: eventType,
		Symbol:    symbol,
		Action:    action,
		Success:   true,
		Details: map[string]interface{}{
			"confidence":    confidence,
			"reasoning":     reasoning,
			"auto_executed": autoExecuted,
		},
	})
}

// LogReadOnlyViolation logs an attempt to perform a write operation in read-only mode.
func (al *AuditLogger) LogReadOnlyViolation(ctx context.Context, operation string) error {
	return al.Log(ctx, AuditEvent{
		EventType: AuditReadOnlyViolation,
		Action:    operation,
		Success:   false,
		ErrorMsg:  "operation blocked: read-only mode enabled",
	})
}

// LogInputValidation logs an input validation failure.
func (al *AuditLogger) LogInputValidation(ctx context.Context, field, value, reason string) error {
	return al.Log(ctx, AuditEvent{
		EventType: AuditInputValidation,
		Success:   false,
		ErrorMsg:  reason,
		Details: map[string]interface{}{
			"field": field,
			"value": MaskSensitive(value),
		},
	})
}

// Close closes the audit logger.
func (al *AuditLogger) Close() error {
	return al.writer.Close()
}

// generateSessionID generates a unique session ID.
func generateSessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
