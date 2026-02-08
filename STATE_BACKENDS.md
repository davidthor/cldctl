# State Backend Implementation Guide

This document provides detailed guidance on implementing new state backends for cldctl.

## Overview

State backends are responsible for persisting cldctl state, including datacenter configurations, environment metadata, and IaC module states. The backend abstraction allows cldctl to work with various storage systems while maintaining consistent behavior.

## Backend Interface

Every state backend must implement the `Backend` interface:

```go
// pkg/state/backend/backend.go

package backend

import (
    "context"
    "io"
    "time"
)

// Backend defines the interface for state storage backends
type Backend interface {
    // Type returns the backend type identifier (e.g., "s3", "local", "gcs")
    Type() string

    // Read reads state data from the given path
    // Returns ErrNotFound if the path doesn't exist
    Read(ctx context.Context, path string) (io.ReadCloser, error)

    // Write writes state data to the given path
    // Creates parent directories/prefixes as needed
    Write(ctx context.Context, path string, data io.Reader) error

    // Delete removes state data at the given path
    // Returns nil if path doesn't exist (idempotent)
    Delete(ctx context.Context, path string) error

    // List lists state files under the given prefix
    // Returns relative paths from the prefix
    List(ctx context.Context, prefix string) ([]string, error)

    // Exists checks if a state file exists
    Exists(ctx context.Context, path string) (bool, error)

    // Lock acquires a lock for the given path
    // Blocks until lock is acquired or context is cancelled
    Lock(ctx context.Context, path string, info LockInfo) (Lock, error)
}

// ErrNotFound is returned when a requested state file doesn't exist
var ErrNotFound = errors.New("state not found")

// ErrLocked is returned when state is already locked
var ErrLocked = errors.New("state is locked")
```

## State Path Structure

Backends store state in a hierarchical structure:

```
<prefix>/
├── datacenters/
│   └── <datacenter-name>/
│       ├── datacenter.state.json      # Datacenter metadata
│       ├── modules/
│       │   ├── <module>.state.json    # IaC state per module
│       │   └── ...
│       └── environments/
│           └── <env-name>/
│               ├── environment.state.json
│               ├── modules/
│               │   └── <module>.state.json
│               └── resources/
│                   └── <component>/
│                       └── <resource>.state.json
```

## Implementing a New Backend

### Step 1: Create the Package

Create a new package under `pkg/state/backend/`:

```go
// pkg/state/backend/mybackend/mybackend.go

package mybackend

import (
    "context"
    "fmt"
    "io"

    "github.com/davidthor/cldctl/pkg/state/backend"
)

// Backend implements the state backend interface for MyStorage
type Backend struct {
    client     *MyStorageClient
    bucket     string
    prefix     string
}

// Config holds backend configuration
type Config struct {
    Endpoint string
    Bucket   string
    Prefix   string
    APIKey   string
}

// NewBackend creates a new backend instance
func NewBackend(config map[string]string) (backend.Backend, error) {
    cfg, err := parseConfig(config)
    if err != nil {
        return nil, fmt.Errorf("invalid configuration: %w", err)
    }

    client, err := createClient(cfg)
    if err != nil {
        return nil, fmt.Errorf("failed to create client: %w", err)
    }

    return &Backend{
        client: client,
        bucket: cfg.Bucket,
        prefix: cfg.Prefix,
    }, nil
}

func parseConfig(config map[string]string) (*Config, error) {
    cfg := &Config{
        Endpoint: config["endpoint"],
        Bucket:   config["bucket"],
        Prefix:   config["prefix"],
        APIKey:   config["api_key"],
    }

    if cfg.Bucket == "" {
        return nil, fmt.Errorf("bucket is required")
    }

    return cfg, nil
}
```

### Step 2: Implement Core Methods

#### Type()

```go
func (b *Backend) Type() string {
    return "mybackend"
}
```

#### Read()

```go
func (b *Backend) Read(ctx context.Context, path string) (io.ReadCloser, error) {
    fullPath := b.fullPath(path)

    obj, err := b.client.GetObject(ctx, b.bucket, fullPath)
    if err != nil {
        if isNotFoundError(err) {
            return nil, backend.ErrNotFound
        }
        return nil, fmt.Errorf("failed to read %s: %w", fullPath, err)
    }

    return obj.Body, nil
}

func (b *Backend) fullPath(path string) string {
    if b.prefix == "" {
        return path
    }
    return b.prefix + "/" + path
}
```

#### Write()

```go
func (b *Backend) Write(ctx context.Context, path string, data io.Reader) error {
    fullPath := b.fullPath(path)

    // Read all data to get content length (required by some storage APIs)
    content, err := io.ReadAll(data)
    if err != nil {
        return fmt.Errorf("failed to read data: %w", err)
    }

    err = b.client.PutObject(ctx, b.bucket, fullPath, bytes.NewReader(content), int64(len(content)))
    if err != nil {
        return fmt.Errorf("failed to write %s: %w", fullPath, err)
    }

    return nil
}
```

#### Delete()

```go
func (b *Backend) Delete(ctx context.Context, path string) error {
    fullPath := b.fullPath(path)

    err := b.client.DeleteObject(ctx, b.bucket, fullPath)
    if err != nil {
        // Ignore not found errors for idempotency
        if isNotFoundError(err) {
            return nil
        }
        return fmt.Errorf("failed to delete %s: %w", fullPath, err)
    }

    return nil
}
```

#### List()

```go
func (b *Backend) List(ctx context.Context, prefix string) ([]string, error) {
    fullPrefix := b.fullPath(prefix)

    objects, err := b.client.ListObjects(ctx, b.bucket, fullPrefix)
    if err != nil {
        return nil, fmt.Errorf("failed to list %s: %w", fullPrefix, err)
    }

    var paths []string
    prefixLen := len(b.prefix)
    if prefixLen > 0 {
        prefixLen++ // Account for separator
    }

    for _, obj := range objects {
        // Return paths relative to backend prefix
        relPath := obj.Key[prefixLen:]
        paths = append(paths, relPath)
    }

    return paths, nil
}
```

#### Exists()

```go
func (b *Backend) Exists(ctx context.Context, path string) (bool, error) {
    fullPath := b.fullPath(path)

    _, err := b.client.HeadObject(ctx, b.bucket, fullPath)
    if err != nil {
        if isNotFoundError(err) {
            return false, nil
        }
        return false, fmt.Errorf("failed to check %s: %w", fullPath, err)
    }

    return true, nil
}
```

### Step 3: Implement Locking

State locking prevents concurrent modifications. Implementation varies by storage system.

#### Option A: Native Locking (Preferred)

If your storage system supports locking (e.g., DynamoDB, etcd, Consul):

```go
func (b *Backend) Lock(ctx context.Context, path string, info backend.LockInfo) (backend.Lock, error) {
    lockPath := b.fullPath(path + ".lock")

    // Try to acquire lock
    lockData, err := json.Marshal(info)
    if err != nil {
        return nil, fmt.Errorf("failed to marshal lock info: %w", err)
    }

    // Use conditional write (create if not exists)
    err = b.client.CreateExclusive(ctx, b.bucket, lockPath, lockData)
    if err != nil {
        if isAlreadyExistsError(err) {
            // Read existing lock info
            existingLock, _ := b.readLockInfo(ctx, lockPath)
            return nil, &backend.LockError{
                Info: existingLock,
                Err:  backend.ErrLocked,
            }
        }
        return nil, fmt.Errorf("failed to acquire lock: %w", err)
    }

    return &myLock{
        backend: b,
        path:    lockPath,
        info:    info,
    }, nil
}

type myLock struct {
    backend *Backend
    path    string
    info    backend.LockInfo
}

func (l *myLock) ID() string {
    return l.info.ID
}

func (l *myLock) Unlock(ctx context.Context) error {
    return l.backend.client.DeleteObject(ctx, l.backend.bucket, l.path)
}

func (l *myLock) Info() backend.LockInfo {
    return l.info
}
```

#### Option B: Auxiliary Locking Service

If your storage doesn't support locking, use an auxiliary service:

```go
// Use DynamoDB for S3 locking (like Terraform)
type Backend struct {
    s3Client  *s3.Client
    lockTable *dynamodb.Client
    // ...
}

func (b *Backend) Lock(ctx context.Context, path string, info backend.LockInfo) (backend.Lock, error) {
    lockID := uuid.New().String()

    // Try to acquire lock in DynamoDB
    _, err := b.lockTable.PutItem(ctx, &dynamodb.PutItemInput{
        TableName: aws.String(b.lockTableName),
        Item: map[string]types.AttributeValue{
            "LockID":    &types.AttributeValueMemberS{Value: path},
            "Info":      &types.AttributeValueMemberS{Value: encodeLockInfo(info)},
            "CreatedAt": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", time.Now().Unix())},
        },
        ConditionExpression: aws.String("attribute_not_exists(LockID)"),
    })

    if err != nil {
        // Handle conditional check failure (lock exists)
        var condErr *types.ConditionalCheckFailedException
        if errors.As(err, &condErr) {
            existing, _ := b.readLock(ctx, path)
            return nil, &backend.LockError{Info: existing, Err: backend.ErrLocked}
        }
        return nil, fmt.Errorf("failed to acquire lock: %w", err)
    }

    return &dynamoLock{
        backend: b,
        path:    path,
        info:    info,
    }, nil
}
```

### Step 4: Register the Backend

Add your backend to the registry:

```go
// pkg/state/backend/registry.go

import (
    "github.com/davidthor/cldctl/pkg/state/backend/mybackend"
)

func init() {
    DefaultRegistry.Register("mybackend", mybackend.NewBackend)
}
```

### Step 5: Add Environment Variable Support

Document and implement environment variable parsing:

```go
// pkg/state/backend/mybackend/config.go

// Environment variables:
// CLDCTL_BACKEND_MYBACKEND_ENDPOINT
// CLDCTL_BACKEND_MYBACKEND_BUCKET
// CLDCTL_BACKEND_MYBACKEND_PREFIX
// CLDCTL_BACKEND_MYBACKEND_API_KEY

func parseConfig(config map[string]string) (*Config, error) {
    // Merge environment variables with explicit config
    // Explicit config takes precedence

    cfg := &Config{
        Endpoint: getConfigValue(config, "endpoint", "CLDCTL_BACKEND_MYBACKEND_ENDPOINT"),
        Bucket:   getConfigValue(config, "bucket", "CLDCTL_BACKEND_MYBACKEND_BUCKET"),
        Prefix:   getConfigValue(config, "prefix", "CLDCTL_BACKEND_MYBACKEND_PREFIX"),
        APIKey:   getConfigValue(config, "api_key", "CLDCTL_BACKEND_MYBACKEND_API_KEY"),
    }

    // Validation
    if cfg.Bucket == "" {
        return nil, fmt.Errorf("bucket is required (set via --backend-config bucket=... or CLDCTL_BACKEND_MYBACKEND_BUCKET)")
    }

    return cfg, nil
}

func getConfigValue(config map[string]string, key, envVar string) string {
    if v, ok := config[key]; ok && v != "" {
        return v
    }
    return os.Getenv(envVar)
}
```

## Testing Your Backend

### Unit Tests

Test each method independently:

```go
// pkg/state/backend/mybackend/mybackend_test.go

func TestBackend_Read(t *testing.T) {
    // Create mock client
    mockClient := &MockClient{
        Objects: map[string][]byte{
            "prefix/test/state.json": []byte(`{"version": 1}`),
        },
    }

    b := &Backend{
        client: mockClient,
        prefix: "prefix",
    }

    t.Run("existing file", func(t *testing.T) {
        reader, err := b.Read(context.Background(), "test/state.json")
        require.NoError(t, err)
        defer reader.Close()

        data, _ := io.ReadAll(reader)
        assert.JSONEq(t, `{"version": 1}`, string(data))
    })

    t.Run("not found", func(t *testing.T) {
        _, err := b.Read(context.Background(), "nonexistent.json")
        assert.ErrorIs(t, err, backend.ErrNotFound)
    })
}

func TestBackend_Write(t *testing.T) {
    mockClient := &MockClient{
        Objects: make(map[string][]byte),
    }

    b := &Backend{
        client: mockClient,
        prefix: "prefix",
    }

    data := []byte(`{"version": 2}`)
    err := b.Write(context.Background(), "test/state.json", bytes.NewReader(data))
    require.NoError(t, err)

    assert.Equal(t, data, mockClient.Objects["prefix/test/state.json"])
}

func TestBackend_Lock(t *testing.T) {
    mockClient := &MockClient{
        Objects: make(map[string][]byte),
    }

    b := &Backend{
        client: mockClient,
        prefix: "prefix",
    }

    ctx := context.Background()

    t.Run("acquire lock", func(t *testing.T) {
        lock, err := b.Lock(ctx, "test/state.json", backend.LockInfo{
            ID:        "lock-1",
            Who:       "test",
            Operation: "deploy",
        })
        require.NoError(t, err)
        defer lock.Unlock(ctx)

        assert.Equal(t, "lock-1", lock.ID())
    })

    t.Run("lock already held", func(t *testing.T) {
        // First lock
        lock1, _ := b.Lock(ctx, "test/state2.json", backend.LockInfo{ID: "lock-1"})

        // Second lock attempt should fail
        _, err := b.Lock(ctx, "test/state2.json", backend.LockInfo{ID: "lock-2"})
        assert.ErrorIs(t, err, backend.ErrLocked)

        lock1.Unlock(ctx)
    })
}
```

### Integration Tests

Test with real infrastructure:

```go
// pkg/state/backend/mybackend/mybackend_integration_test.go

//go:build integration

func TestBackend_Integration(t *testing.T) {
    // Skip if not configured
    bucket := os.Getenv("TEST_MYBACKEND_BUCKET")
    if bucket == "" {
        t.Skip("TEST_MYBACKEND_BUCKET not set")
    }

    b, err := NewBackend(map[string]string{
        "bucket": bucket,
        "prefix": "cldctl-test-" + randomString(8),
    })
    require.NoError(t, err)

    ctx := context.Background()
    testPath := "integration-test/state.json"
    testData := []byte(`{"test": true}`)

    // Cleanup
    defer b.Delete(ctx, testPath)

    // Test Write
    err = b.Write(ctx, testPath, bytes.NewReader(testData))
    require.NoError(t, err)

    // Test Exists
    exists, err := b.Exists(ctx, testPath)
    require.NoError(t, err)
    assert.True(t, exists)

    // Test Read
    reader, err := b.Read(ctx, testPath)
    require.NoError(t, err)
    data, _ := io.ReadAll(reader)
    reader.Close()
    assert.Equal(t, testData, data)

    // Test List
    paths, err := b.List(ctx, "integration-test/")
    require.NoError(t, err)
    assert.Contains(t, paths, "integration-test/state.json")

    // Test Delete
    err = b.Delete(ctx, testPath)
    require.NoError(t, err)

    exists, _ = b.Exists(ctx, testPath)
    assert.False(t, exists)
}
```

## Best Practices

### Error Handling

1. **Wrap errors with context**: Include the path and operation in error messages
2. **Use sentinel errors**: Return `backend.ErrNotFound` and `backend.ErrLocked` appropriately
3. **Handle transient failures**: Consider retry logic for network errors

```go
func (b *Backend) Read(ctx context.Context, path string) (io.ReadCloser, error) {
    var lastErr error
    for attempt := 0; attempt < 3; attempt++ {
        reader, err := b.doRead(ctx, path)
        if err == nil {
            return reader, nil
        }

        if !isRetryable(err) {
            return nil, err
        }

        lastErr = err
        time.Sleep(time.Duration(attempt*100) * time.Millisecond)
    }

    return nil, fmt.Errorf("failed after 3 attempts: %w", lastErr)
}
```

### Context Handling

1. **Respect context cancellation**: Check `ctx.Done()` in long operations
2. **Pass context to underlying clients**: Ensure cancellation propagates

```go
func (b *Backend) Write(ctx context.Context, path string, data io.Reader) error {
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }

    return b.client.PutObject(ctx, b.bucket, path, data)
}
```

### Resource Cleanup

1. **Return closeable readers**: Use `io.ReadCloser` for Read operations
2. **Clean up on errors**: Ensure partial writes don't leave orphaned data

```go
func (b *Backend) Write(ctx context.Context, path string, data io.Reader) error {
    tempPath := path + ".tmp"

    // Write to temp location
    err := b.client.PutObject(ctx, b.bucket, tempPath, data)
    if err != nil {
        return err
    }

    // Move to final location (atomic on most storage systems)
    err = b.client.CopyObject(ctx, b.bucket, tempPath, path)
    if err != nil {
        b.client.DeleteObject(ctx, b.bucket, tempPath)
        return err
    }

    return b.client.DeleteObject(ctx, b.bucket, tempPath)
}
```

### Lock Safety

1. **Use timeouts**: Don't hold locks indefinitely
2. **Handle stale locks**: Implement lock expiration or heartbeats
3. **Document locking behavior**: Explain how conflicts are resolved

```go
type LockInfo struct {
    ID        string    `json:"id"`
    Who       string    `json:"who"`
    Operation string    `json:"operation"`
    Created   time.Time `json:"created"`
    Expires   time.Time `json:"expires,omitempty"` // Optional expiration
}

func (b *Backend) Lock(ctx context.Context, path string, info backend.LockInfo) (backend.Lock, error) {
    // Check for stale locks
    existing, err := b.readLock(ctx, path)
    if err == nil && !existing.Expires.IsZero() && time.Now().After(existing.Expires) {
        // Force unlock stale lock
        b.forceUnlock(ctx, path)
    }

    // Proceed with normal locking
    // ...
}
```

## Existing Backend Implementations

Study these implementations for reference:

| Backend | Location                     | Notes                                        |
| ------- | ---------------------------- | -------------------------------------------- |
| Local   | `pkg/state/backend/local/`   | Simplest implementation, good starting point |
| S3      | `pkg/state/backend/s3/`      | Uses DynamoDB for locking                    |
| GCS     | `pkg/state/backend/gcs/`     | Uses GCS Object conditions                   |
| AzureRM | `pkg/state/backend/azurerm/` | Uses Azure Blob leases                       |
