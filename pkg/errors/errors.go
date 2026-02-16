// Package errors provides structured error types for cldctl.
package errors

import (
	"fmt"
	"time"
)

// ErrorCode identifies specific error conditions
type ErrorCode string

const (
	ErrCodeValidation     ErrorCode = "VALIDATION_ERROR"
	ErrCodeNotFound       ErrorCode = "NOT_FOUND"
	ErrCodeConflict       ErrorCode = "CONFLICT"
	ErrCodeLocked         ErrorCode = "STATE_LOCKED"
	ErrCodeBackend        ErrorCode = "BACKEND_ERROR"
	ErrCodeIaC            ErrorCode = "IAC_ERROR"
	ErrCodeTimeout        ErrorCode = "TIMEOUT"
	ErrCodePermission     ErrorCode = "PERMISSION_DENIED"
	ErrCodeParse          ErrorCode = "PARSE_ERROR"
	ErrCodeExpression     ErrorCode = "EXPRESSION_ERROR"
	ErrCodeOCI            ErrorCode = "OCI_ERROR"
	ErrCodeDocker         ErrorCode = "DOCKER_ERROR"
	ErrCodePlugin         ErrorCode = "PLUGIN_ERROR"
	ErrCodeDatacenterHook ErrorCode = "DATACENTER_HOOK_ERROR"
)

// Error is the base error type for cldctl
type Error struct {
	Code    ErrorCode
	Message string
	Cause   error
	Details map[string]interface{}
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

// New creates a new error with the given code and message
func New(code ErrorCode, message string) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Details: make(map[string]interface{}),
	}
}

// Wrap creates a new error wrapping an existing error
func Wrap(code ErrorCode, message string, cause error) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Cause:   cause,
		Details: make(map[string]interface{}),
	}
}

// WithDetails adds details to an error
func (e *Error) WithDetails(details map[string]interface{}) *Error {
	for k, v := range details {
		e.Details[k] = v
	}
	return e
}

// WithDetail adds a single detail to an error
func (e *Error) WithDetail(key string, value interface{}) *Error {
	e.Details[key] = value
	return e
}

// ValidationError creates a validation error
func ValidationError(message string, details map[string]interface{}) *Error {
	return &Error{
		Code:    ErrCodeValidation,
		Message: message,
		Details: details,
	}
}

// NotFoundError creates a not found error
func NotFoundError(resourceType, name string) *Error {
	return &Error{
		Code:    ErrCodeNotFound,
		Message: fmt.Sprintf("%s %q not found", resourceType, name),
		Details: map[string]interface{}{
			"resource_type": resourceType,
			"name":          name,
		},
	}
}

// LockInfo contains metadata about a lock
type LockInfo struct {
	ID        string
	Path      string
	Who       string
	Operation string
	Created   time.Time
}

// StateLocked creates a state locked error
func StateLocked(lockInfo LockInfo) *Error {
	return &Error{
		Code:    ErrCodeLocked,
		Message: "state is locked",
		Details: map[string]interface{}{
			"lock_id":   lockInfo.ID,
			"locked_by": lockInfo.Who,
			"operation": lockInfo.Operation,
			"created":   lockInfo.Created,
		},
	}
}

// ParseError creates a parse error
func ParseError(filePath string, err error) *Error {
	return &Error{
		Code:    ErrCodeParse,
		Message: fmt.Sprintf("failed to parse %s", filePath),
		Cause:   err,
		Details: map[string]interface{}{
			"file": filePath,
		},
	}
}

// ExpressionError creates an expression evaluation error
func ExpressionError(expression string, err error) *Error {
	return &Error{
		Code:    ErrCodeExpression,
		Message: fmt.Sprintf("failed to evaluate expression: %s", expression),
		Cause:   err,
		Details: map[string]interface{}{
			"expression": expression,
		},
	}
}

// PluginError creates a plugin execution error
func PluginError(plugin string, operation string, err error) *Error {
	return &Error{
		Code:    ErrCodePlugin,
		Message: fmt.Sprintf("plugin %s failed during %s", plugin, operation),
		Cause:   err,
		Details: map[string]interface{}{
			"plugin":    plugin,
			"operation": operation,
		},
	}
}

// BackendError creates a backend error
func BackendError(backend string, operation string, err error) *Error {
	return &Error{
		Code:    ErrCodeBackend,
		Message: fmt.Sprintf("backend %s failed during %s", backend, operation),
		Cause:   err,
		Details: map[string]interface{}{
			"backend":   backend,
			"operation": operation,
		},
	}
}

// DatacenterHookError creates an error for a datacenter hook that explicitly
// rejects a resource configuration (e.g., unsupported database type).
func DatacenterHookError(hookType, component, resource, message string) *Error {
	return &Error{
		Code:    ErrCodeDatacenterHook,
		Message: message,
		Details: map[string]interface{}{
			"hook_type": hookType,
			"component": component,
			"resource":  resource,
		},
	}
}

// Is checks if the error matches the given code
func Is(err error, code ErrorCode) bool {
	if e, ok := err.(*Error); ok {
		return e.Code == code
	}
	return false
}
