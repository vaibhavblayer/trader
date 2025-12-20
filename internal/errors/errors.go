// Package errors provides custom error types for domain-specific errors.
package errors

import (
	"errors"
	"fmt"
)

// Standard sentinel errors
var (
	ErrNotAuthenticated   = errors.New("not authenticated")
	ErrSessionExpired     = errors.New("session expired")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrMarketClosed       = errors.New("market is closed")
	ErrInsufficientFunds  = errors.New("insufficient funds")
	ErrInvalidOrder       = errors.New("invalid order")
	ErrOrderRejected      = errors.New("order rejected")
	ErrPositionNotFound   = errors.New("position not found")
	ErrSymbolNotFound     = errors.New("symbol not found")
	ErrRateLimited        = errors.New("rate limited")
	ErrConnectionFailed   = errors.New("connection failed")
	ErrTimeout            = errors.New("operation timed out")
	ErrConfigInvalid      = errors.New("invalid configuration")
	ErrDataNotFound       = errors.New("data not found")
	ErrDatabaseError      = errors.New("database error")
	ErrReadOnlyMode       = errors.New("operation blocked: read-only mode enabled")
	ErrInputValidation    = errors.New("input validation failed")
	ErrCredentialAccess   = errors.New("credential access denied")
)

// BrokerError represents an error from the broker API.
type BrokerError struct {
	Code    string
	Message string
	Err     error
}

func (e *BrokerError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("broker error [%s]: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("broker error [%s]: %s", e.Code, e.Message)
}

func (e *BrokerError) Unwrap() error {
	return e.Err
}

// NewBrokerError creates a new BrokerError.
func NewBrokerError(code, message string, err error) *BrokerError {
	return &BrokerError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// OrderError represents an error related to order operations.
type OrderError struct {
	OrderID string
	Symbol  string
	Action  string
	Reason  string
	Err     error
}

func (e *OrderError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("order error [%s] %s %s: %s: %v", e.OrderID, e.Action, e.Symbol, e.Reason, e.Err)
	}
	return fmt.Sprintf("order error [%s] %s %s: %s", e.OrderID, e.Action, e.Symbol, e.Reason)
}

func (e *OrderError) Unwrap() error {
	return e.Err
}

// NewOrderError creates a new OrderError.
func NewOrderError(orderID, symbol, action, reason string, err error) *OrderError {
	return &OrderError{
		OrderID: orderID,
		Symbol:  symbol,
		Action:  action,
		Reason:  reason,
		Err:     err,
	}
}

// ValidationError represents a validation error.
type ValidationError struct {
	Field   string
	Value   interface{}
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s (%v): %s", e.Field, e.Value, e.Message)
}

// NewValidationError creates a new ValidationError.
func NewValidationError(field string, value interface{}, message string) *ValidationError {
	return &ValidationError{
		Field:   field,
		Value:   value,
		Message: message,
	}
}

// AgentError represents an error from an AI agent.
type AgentError struct {
	AgentName string
	Operation string
	Err       error
}

func (e *AgentError) Error() string {
	return fmt.Sprintf("agent error [%s] %s: %v", e.AgentName, e.Operation, e.Err)
}

func (e *AgentError) Unwrap() error {
	return e.Err
}

// NewAgentError creates a new AgentError.
func NewAgentError(agentName, operation string, err error) *AgentError {
	return &AgentError{
		AgentName: agentName,
		Operation: operation,
		Err:       err,
	}
}

// DataError represents a data-related error.
type DataError struct {
	DataType string
	Symbol   string
	Message  string
	Err      error
}

func (e *DataError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("data error [%s] %s: %s: %v", e.DataType, e.Symbol, e.Message, e.Err)
	}
	return fmt.Sprintf("data error [%s] %s: %s", e.DataType, e.Symbol, e.Message)
}

func (e *DataError) Unwrap() error {
	return e.Err
}

// NewDataError creates a new DataError.
func NewDataError(dataType, symbol, message string, err error) *DataError {
	return &DataError{
		DataType: dataType,
		Symbol:   symbol,
		Message:  message,
		Err:      err,
	}
}

// RiskError represents a risk management error.
type RiskError struct {
	Rule    string
	Current float64
	Limit   float64
	Message string
}

func (e *RiskError) Error() string {
	return fmt.Sprintf("risk violation [%s]: %s (current: %.2f, limit: %.2f)", e.Rule, e.Message, e.Current, e.Limit)
}

// NewRiskError creates a new RiskError.
func NewRiskError(rule string, current, limit float64, message string) *RiskError {
	return &RiskError{
		Rule:    rule,
		Current: current,
		Limit:   limit,
		Message: message,
	}
}

// Wrap wraps an error with additional context.
func Wrap(err error, message string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", message, err)
}

// Wrapf wraps an error with formatted context.
func Wrapf(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}

// Is reports whether any error in err's chain matches target.
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As finds the first error in err's chain that matches target.
func As(err error, target interface{}) bool {
	return errors.As(err, target)
}


// SecurityError represents a security-related error.
type SecurityError struct {
	Operation string
	Reason    string
	Err       error
}

func (e *SecurityError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("security error [%s]: %s: %v", e.Operation, e.Reason, e.Err)
	}
	return fmt.Sprintf("security error [%s]: %s", e.Operation, e.Reason)
}

func (e *SecurityError) Unwrap() error {
	return e.Err
}

// NewSecurityError creates a new SecurityError.
func NewSecurityError(operation, reason string, err error) *SecurityError {
	return &SecurityError{
		Operation: operation,
		Reason:    reason,
		Err:       err,
	}
}
