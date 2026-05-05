// Package security provides credential encryption, audit logging, and security controls.
package security

import (
	"context"
	"fmt"
	"sync"
)

// OperationType represents the type of operation.
type OperationType string

const (
	// Read operations
	OpRead OperationType = "READ"

	// Write operations (blocked in read-only mode)
	OpPlaceOrder      OperationType = "PLACE_ORDER"
	OpModifyOrder     OperationType = "MODIFY_ORDER"
	OpCancelOrder     OperationType = "CANCEL_ORDER"
	OpExitPosition    OperationType = "EXIT_POSITION"
	OpPlaceGTT        OperationType = "PLACE_GTT"
	OpModifyGTT       OperationType = "MODIFY_GTT"
	OpCancelGTT       OperationType = "CANCEL_GTT"
	OpSavePlan        OperationType = "SAVE_PLAN"
	OpModifyPlan      OperationType = "MODIFY_PLAN"
	OpExecutePlan     OperationType = "EXECUTE_PLAN"
	OpSaveAlert       OperationType = "SAVE_ALERT"
	OpModifyWatchlist OperationType = "MODIFY_WATCHLIST"
	OpAutoTrade       OperationType = "AUTO_TRADE"
	OpModifyConfig    OperationType = "MODIFY_CONFIG"
)

// ReadOnlyError represents an error when attempting a write operation in read-only mode.
type ReadOnlyError struct {
	Operation OperationType
}

func (e *ReadOnlyError) Error() string {
	return fmt.Sprintf("operation %s blocked: read-only mode is enabled", e.Operation)
}

// AccessController manages read-only mode and operation permissions.
type AccessController struct {
	readOnly    bool
	profile     string
	auditLogger *AuditLogger
	mu          sync.RWMutex
}

// NewAccessController creates a new access controller.
func NewAccessController(readOnly bool, auditLogger *AuditLogger) *AccessController {
	return &AccessController{
		readOnly:    readOnly,
		auditLogger: auditLogger,
	}
}

// NewProfileAccessController creates an access controller with an explicit safety profile.
func NewProfileAccessController(profile string, readOnly bool, auditLogger *AuditLogger) *AccessController {
	return &AccessController{
		readOnly:    readOnly,
		profile:     profile,
		auditLogger: auditLogger,
	}
}

// IsReadOnly returns whether read-only mode is enabled.
func (ac *AccessController) IsReadOnly() bool {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.readOnly
}

// SetReadOnly sets the read-only mode.
func (ac *AccessController) SetReadOnly(readOnly bool) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.readOnly = readOnly
}

// CheckPermission checks if an operation is allowed.
func (ac *AccessController) CheckPermission(ctx context.Context, op OperationType) error {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	if err := checkProfilePermission(ac.profile, op); err != nil {
		if ac.auditLogger != nil {
			ac.auditLogger.LogReadOnlyViolation(ctx, string(op))
		}
		return err
	}

	if !ac.readOnly {
		return nil
	}

	// Check if operation is a write operation
	if isWriteOperation(op) {
		// Log the violation
		if ac.auditLogger != nil {
			ac.auditLogger.LogReadOnlyViolation(ctx, string(op))
		}
		return &ReadOnlyError{Operation: op}
	}

	return nil
}

// ProfileError represents an operation blocked by the active safety profile.
type ProfileError struct {
	Profile   string
	Operation OperationType
}

func (e *ProfileError) Error() string {
	return fmt.Sprintf("operation %s blocked by safety profile %s", e.Operation, e.Profile)
}

func checkProfilePermission(profile string, op OperationType) error {
	if profile == "" {
		return nil
	}
	switch profile {
	case "backtest":
		if isWriteOperation(op) {
			return &ProfileError{Profile: profile, Operation: op}
		}
	case "live-readonly":
		if isWriteOperation(op) && op != OpModifyConfig {
			return &ProfileError{Profile: profile, Operation: op}
		}
	case "paper", "live-trading":
		return nil
	default:
		if isWriteOperation(op) {
			return &ProfileError{Profile: profile, Operation: op}
		}
	}
	return nil
}

// MustCheckPermission checks permission and panics if denied.
// Use this for critical paths where permission denial should never happen.
func (ac *AccessController) MustCheckPermission(ctx context.Context, op OperationType) {
	if err := ac.CheckPermission(ctx, op); err != nil {
		panic(err)
	}
}

// isWriteOperation returns true if the operation modifies state.
func isWriteOperation(op OperationType) bool {
	switch op {
	case OpPlaceOrder, OpModifyOrder, OpCancelOrder,
		OpExitPosition, OpPlaceGTT, OpModifyGTT, OpCancelGTT,
		OpSavePlan, OpModifyPlan, OpExecutePlan, OpSaveAlert,
		OpModifyWatchlist, OpAutoTrade, OpModifyConfig:
		return true
	default:
		return false
	}
}

// WriteOperations returns a list of all write operations.
func WriteOperations() []OperationType {
	return []OperationType{
		OpPlaceOrder,
		OpModifyOrder,
		OpCancelOrder,
		OpExitPosition,
		OpPlaceGTT,
		OpModifyGTT,
		OpCancelGTT,
		OpSavePlan,
		OpModifyPlan,
		OpExecutePlan,
		OpSaveAlert,
		OpModifyWatchlist,
		OpAutoTrade,
		OpModifyConfig,
	}
}

// OperationDescription returns a human-readable description of an operation.
func OperationDescription(op OperationType) string {
	switch op {
	case OpRead:
		return "Read data"
	case OpPlaceOrder:
		return "Place order"
	case OpModifyOrder:
		return "Modify order"
	case OpCancelOrder:
		return "Cancel order"
	case OpExitPosition:
		return "Exit position"
	case OpPlaceGTT:
		return "Place GTT order"
	case OpModifyGTT:
		return "Modify GTT order"
	case OpCancelGTT:
		return "Cancel GTT order"
	case OpSavePlan:
		return "Save trade plan"
	case OpModifyPlan:
		return "Modify trade plan"
	case OpExecutePlan:
		return "Execute trade plan"
	case OpSaveAlert:
		return "Save alert"
	case OpModifyWatchlist:
		return "Modify watchlist"
	case OpAutoTrade:
		return "Autonomous trading"
	case OpModifyConfig:
		return "Modify configuration"
	default:
		return string(op)
	}
}
