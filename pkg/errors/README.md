# errors

Structured error types for cldctl with error codes, wrapping, and metadata support.

## Overview

The `errors` package provides a comprehensive error handling system for cldctl that supports:

- Error codes for categorization
- Error wrapping with cause chains
- Structured details/metadata
- Full integration with Go's error handling (`error` interface, `errors.Unwrap()`)

## Types

### Error

Base error type with code, message, cause, and details.

```go
type Error struct {
    Code    ErrorCode
    Message string
    Cause   error
    Details map[string]interface{}
}
```

### ErrorCode

Predefined error codes for common error categories.

```go
const (
    ErrCodeValidation  ErrorCode = "VALIDATION_ERROR"
    ErrCodeNotFound    ErrorCode = "NOT_FOUND"
    ErrCodeConflict    ErrorCode = "CONFLICT"
    ErrCodeLocked      ErrorCode = "STATE_LOCKED"
    ErrCodeBackend     ErrorCode = "BACKEND_ERROR"
    ErrCodeIaC         ErrorCode = "IAC_ERROR"
    ErrCodeTimeout     ErrorCode = "TIMEOUT"
    ErrCodePermission  ErrorCode = "PERMISSION_DENIED"
    ErrCodeParse       ErrorCode = "PARSE_ERROR"
    ErrCodeExpression  ErrorCode = "EXPRESSION_ERROR"
    ErrCodeOCI         ErrorCode = "OCI_ERROR"
    ErrCodeDocker      ErrorCode = "DOCKER_ERROR"
    ErrCodePlugin      ErrorCode = "PLUGIN_ERROR"
)
```

### LockInfo

State lock metadata for lock-related errors.

```go
type LockInfo struct {
    ID        string
    Path      string
    Who       string
    Operation string
    Created   time.Time
}
```

## Functions

### Creating Errors

```go
// Create a new error with a code and message
err := errors.New(errors.ErrCodeValidation, "invalid configuration")

// Wrap an existing error
err := errors.Wrap(errors.ErrCodeParse, "failed to parse component", originalErr)
```

### Specialized Constructors

```go
// Validation error with details
err := errors.ValidationError("invalid port number", map[string]interface{}{
    "field": "port",
    "value": -1,
    "min":   1,
    "max":   65535,
})

// Not found error
err := errors.NotFoundError("component", "my-api")
// Message: "component 'my-api' not found"

// State locked error
err := errors.StateLocked(errors.LockInfo{
    ID:        "abc123",
    Path:      "environments/production",
    Who:       "user@example.com",
    Operation: "deploy",
    Created:   time.Now(),
})

// Parse error
err := errors.ParseError("/path/to/component.yml", originalErr)

// Expression evaluation error
err := errors.ExpressionError("${{ invalid.ref }}", originalErr)

// Plugin execution error
err := errors.PluginError("opentofu", "apply", originalErr)

// Backend error
err := errors.BackendError("s3", "read", originalErr)
```

### Checking Error Codes

```go
// Check if an error has a specific code
if errors.Is(err, errors.ErrCodeNotFound) {
    // Handle not found
}

if errors.Is(err, errors.ErrCodeLocked) {
    // Handle locked state
}
```

## Methods

### Error Methods

```go
// Get the error message (implements error interface)
msg := err.Error()

// Get the underlying cause (for error unwrapping)
cause := err.Unwrap()

// Add multiple details
err = err.WithDetails(map[string]interface{}{
    "file":   "/path/to/file",
    "line":   42,
    "column": 10,
})

// Add a single detail
err = err.WithDetail("component", "my-api")
```

## Usage Patterns

### Basic Error Handling

```go
import "github.com/davidthor/cldctl/pkg/errors"

func loadComponent(path string) (*Component, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return nil, errors.NotFoundError("component file", path)
        }
        return nil, errors.Wrap(errors.ErrCodeParse, "failed to read component", err)
    }

    comp, err := parse(data)
    if err != nil {
        return nil, errors.ParseError(path, err)
    }

    return comp, nil
}
```

### Error Checking

```go
result, err := engine.Deploy(ctx, opts)
if err != nil {
    if errors.Is(err, errors.ErrCodeLocked) {
        fmt.Println("Another deployment is in progress")
        return
    }
    if errors.Is(err, errors.ErrCodeValidation) {
        fmt.Println("Configuration validation failed:", err)
        return
    }
    // Handle other errors
    return err
}
```

### Working with Error Details

```go
err := errors.ValidationError("invalid replica count", nil).
    WithDetail("field", "replicas").
    WithDetail("value", -1).
    WithDetail("constraint", "must be >= 0")

// Access details
if arcErr, ok := err.(*errors.Error); ok {
    fmt.Printf("Field: %s\n", arcErr.Details["field"])
    fmt.Printf("Value: %v\n", arcErr.Details["value"])
}
```

### Error Wrapping Chain

```go
// Original error
originalErr := fmt.Errorf("connection refused")

// Wrap at the backend level
backendErr := errors.BackendError("s3", "read", originalErr)

// Wrap at the state level
stateErr := errors.Wrap(errors.ErrCodeBackend, "failed to load state", backendErr)

// The full error chain is preserved
fmt.Println(stateErr.Error())
// Output: failed to load state: s3 backend error during read: connection refused

// Unwrap to get the cause
cause := stateErr.Unwrap()
```

## Integration with Go Errors

The `Error` type fully implements Go's error interface and supports the standard error wrapping pattern:

```go
import (
    "errors"
    arcerrors "github.com/davidthor/cldctl/pkg/errors"
)

// Use errors.Is for type checking
if errors.Is(err, arcerrors.ErrCodeNotFound) {
    // ...
}

// Use errors.Unwrap for cause chain traversal
for err != nil {
    fmt.Println(err.Error())
    err = errors.Unwrap(err)
}
```
