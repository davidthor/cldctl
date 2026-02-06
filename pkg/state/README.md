# state

State management for arcctl. Provides storage and retrieval of datacenters, environments, components, and resources with support for multiple backends.

## Overview

The `state` package provides:

- High-level state management API
- Multiple storage backends (local, S3, GCS, Azure Blob Storage)
- Distributed locking for safe concurrent access
- Hierarchical state organization

## Package Structure

```
state/
├── manager.go     # High-level state manager
├── types/         # State data structures
└── backend/       # Storage backend implementations
    ├── azurerm/   # Azure Blob Storage
    ├── gcs/       # Google Cloud Storage
    ├── local/     # Local filesystem
    └── s3/        # AWS S3 (and compatible)
```

## Manager

The manager provides high-level operations for state management.

### Creating a Manager

```go
import (
    "github.com/architect-io/arcctl/pkg/state"
    "github.com/architect-io/arcctl/pkg/state/backend"
)

// From a backend instance
localBackend, _ := backend.Create(backend.Config{
    Type: "local",
    Config: map[string]string{
        "path": "~/.arcctl/state",
    },
})
manager := state.NewManager(localBackend)

// From configuration
manager, err := state.NewManagerFromConfig(backend.Config{
    Type: "s3",
    Config: map[string]string{
        "bucket": "my-arcctl-state",
        "region": "us-west-2",
    },
})
```

### Manager Interface

Environments are scoped under datacenters, so environment, component, and resource
operations require a `datacenter` parameter. This allows environments with the same
name to exist on different datacenters without collisions.

```go
type Manager interface {
    // Datacenter operations
    GetDatacenter(ctx context.Context, name string) (*types.DatacenterState, error)
    SaveDatacenter(ctx context.Context, state *types.DatacenterState) error
    DeleteDatacenter(ctx context.Context, name string) error
    ListDatacenters(ctx context.Context) ([]string, error)
    
    // Environment operations (datacenter-scoped)
    ListEnvironments(ctx context.Context, datacenter string) ([]types.EnvironmentRef, error)
    GetEnvironment(ctx context.Context, datacenter, name string) (*types.EnvironmentState, error)
    SaveEnvironment(ctx context.Context, datacenter string, state *types.EnvironmentState) error
    DeleteEnvironment(ctx context.Context, datacenter, name string) error
    
    // Component operations (datacenter-scoped)
    GetComponent(ctx context.Context, dc, env, name string) (*types.ComponentState, error)
    SaveComponent(ctx context.Context, dc, env string, state *types.ComponentState) error
    DeleteComponent(ctx context.Context, dc, env, name string) error
    
    // Resource operations (datacenter-scoped)
    GetResource(ctx context.Context, dc, env, comp, name string) (*types.ResourceState, error)
    SaveResource(ctx context.Context, dc, env, comp string, state *types.ResourceState) error
    DeleteResource(ctx context.Context, dc, env, comp, name string) error
    
    // Locking
    Lock(ctx context.Context, scope LockScope) (backend.Lock, error)
    
    // Backend access
    Backend() backend.Backend
}
```

### Locking

```go
// Acquire a lock before modifying state
lock, err := manager.Lock(ctx, state.LockScope{
    Datacenter:  "aws-us-east",
    Environment: "production",
    Component:   "api",
    Operation:   "deploy",
    Who:         "user@example.com",
})
if err != nil {
    log.Fatal(err)
}
defer lock.Unlock(ctx)

// Perform state modifications...
```

## State Types

### DatacenterState

```go
type DatacenterState struct {
    Name         string
    Version      string
    CreatedAt    time.Time
    UpdatedAt    time.Time
    Variables    map[string]string
    Modules      map[string]ModuleState
}
```

Note: Environments are children of datacenters in the state hierarchy. Use
`ListEnvironments(ctx, datacenter)` to discover environments for a datacenter.

### EnvironmentState

```go
type EnvironmentState struct {
    Name         string
    Datacenter   string
    CreatedAt    time.Time
    UpdatedAt    time.Time
    Status       EnvironmentStatus
    StatusReason string
    Variables    map[string]string
    Components   map[string]ComponentState
    Modules      map[string]ModuleState
}

const (
    EnvironmentStatusPending      EnvironmentStatus = "pending"
    EnvironmentStatusProvisioning EnvironmentStatus = "provisioning"
    EnvironmentStatusReady        EnvironmentStatus = "ready"
    EnvironmentStatusFailed       EnvironmentStatus = "failed"
    EnvironmentStatusDestroying   EnvironmentStatus = "destroying"
)
```

### ComponentState

```go
type ComponentState struct {
    Name         string
    Version      string
    Source       string
    DeployedAt   time.Time
    UpdatedAt    time.Time
    Status       string
    StatusReason string
    Variables    map[string]string
    Resources    map[string]ResourceState
}
```

### ResourceState

```go
type ResourceState struct {
    Name         string
    Type         string
    Component    string
    CreatedAt    time.Time
    UpdatedAt    time.Time
    Hook         string
    Module       string
    Inputs       map[string]interface{}
    Outputs      map[string]interface{}
    IaCState     []byte
    Status       ResourceStatus
    StatusReason string
}

const (
    ResourceStatusPending      ResourceStatus = "pending"
    ResourceStatusProvisioning ResourceStatus = "provisioning"
    ResourceStatusReady        ResourceStatus = "ready"
    ResourceStatusFailed       ResourceStatus = "failed"
    ResourceStatusDeleting     ResourceStatus = "deleting"
    ResourceStatusDeleted      ResourceStatus = "deleted"
)
```

### ModuleState

```go
type ModuleState struct {
    Name         string
    Plugin       string
    Source       string
    CreatedAt    time.Time
    UpdatedAt    time.Time
    Inputs       map[string]interface{}
    Outputs      map[string]interface{}
    IaCState     []byte
    Status       ModuleStatus
    StatusReason string
}
```

## Backends

### Backend Interface

```go
type Backend interface {
    Type() string
    Read(ctx context.Context, path string) (io.ReadCloser, error)
    Write(ctx context.Context, path string, data io.Reader) error
    Delete(ctx context.Context, path string) error
    List(ctx context.Context, prefix string) ([]string, error)
    Exists(ctx context.Context, path string) (bool, error)
    Lock(ctx context.Context, path string, info LockInfo) (Lock, error)
}
```

### Local Backend

Stores state on the local filesystem.

```go
import "github.com/architect-io/arcctl/pkg/state/backend/local"

backend, err := local.NewBackend(map[string]string{
    "path": "~/.arcctl/state",  // Optional, default: ~/.arcctl/state
})
```

### S3 Backend

Stores state in AWS S3 or compatible services (MinIO, R2).

```go
import "github.com/architect-io/arcctl/pkg/state/backend/s3"

backend, err := s3.NewBackend(map[string]string{
    "bucket":           "my-arcctl-state",  // Required
    "region":           "us-west-2",        // Optional, default: us-east-1
    "key":              "arcctl/",          // Optional prefix
    "endpoint":         "...",              // Optional, for S3-compatible services
    "access_key":       "...",              // Optional, uses default credentials
    "secret_key":       "...",              // Optional
    "force_path_style": "true",             // Optional, for MinIO
})
```

### GCS Backend

Stores state in Google Cloud Storage.

```go
import "github.com/architect-io/arcctl/pkg/state/backend/gcs"

backend, err := gcs.NewBackend(map[string]string{
    "bucket":           "my-arcctl-state",  // Required
    "prefix":           "arcctl/",          // Optional
    "credentials":      "/path/to/key.json", // Optional
    "credentials_json": "...",              // Optional, inline JSON
    "endpoint":         "...",              // Optional, for emulator
})
defer backend.Close()
```

### Azure Blob Storage Backend

Stores state in Azure Blob Storage.

```go
import "github.com/architect-io/arcctl/pkg/state/backend/azurerm"

backend, err := azurerm.NewBackend(map[string]string{
    "storage_account_name": "myaccount",    // Required
    "container_name":       "arcctl-state", // Required
    "key":                  "arcctl/",      // Optional prefix
    "access_key":           "...",          // Optional
    "sas_token":            "...",          // Optional
    "connection_string":    "...",          // Optional
    "endpoint":             "...",          // Optional, for Azurite
})
```

### Backend Registry

```go
import "github.com/architect-io/arcctl/pkg/state/backend"

// Create a backend from configuration
b, err := backend.Create(backend.Config{
    Type: "s3",
    Config: map[string]string{
        "bucket": "my-state",
    },
})

// List available backends
backends := backend.DefaultRegistry.List()
// ["local", "s3", "gcs", "azurerm"]

// Register a custom backend
backend.Register("custom", func(config map[string]string) (backend.Backend, error) {
    return NewCustomBackend(config)
})
```

## State Path Structure

Environments are nested under their parent datacenter in the state hierarchy.
This allows the same environment name to exist on different datacenters without collisions.

```
datacenters/<datacenter>/datacenter.state.json
datacenters/<datacenter>/modules/<module>.state.json
datacenters/<datacenter>/environments/<env>/environment.state.json
datacenters/<datacenter>/environments/<env>/modules/<module>.state.json
datacenters/<datacenter>/environments/<env>/resources/<component>/<resource>.state.json
```

Use `arcctl migrate state` to migrate from the old flat structure
(`environments/<name>/...`) to the new nested structure.

## Locking

All backends implement distributed locking:

- Stale lock detection (1 hour timeout)
- Lock metadata (who, operation, timestamp)
- UUID-based lock IDs

```go
// Lock information
type LockInfo struct {
    ID        string
    Path      string
    Who       string
    Operation string
    Created   time.Time
    Expires   time.Time
}

// Lock interface
type Lock interface {
    ID() string
    Unlock(ctx context.Context) error
    Info() LockInfo
}

// Lock error with info about existing lock
type LockError struct {
    Info LockInfo
    Err  error
}
```

## Example: Full Workflow

```go
import (
    "context"
    "time"
    "github.com/architect-io/arcctl/pkg/state"
    "github.com/architect-io/arcctl/pkg/state/backend"
    "github.com/architect-io/arcctl/pkg/state/types"
)

func main() {
    ctx := context.Background()
    
    // Create manager with S3 backend
    manager, err := state.NewManagerFromConfig(backend.Config{
        Type: "s3",
        Config: map[string]string{
            "bucket": "my-arcctl-state",
            "region": "us-west-2",
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    
    datacenter := "aws-us-east"
    
    // Acquire lock
    lock, err := manager.Lock(ctx, state.LockScope{
        Datacenter:  datacenter,
        Environment: "production",
        Operation:   "deploy",
        Who:         "ci-pipeline",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer lock.Unlock(ctx)
    
    // Get or create environment state (scoped to datacenter)
    env, err := manager.GetEnvironment(ctx, datacenter, "production")
    if err == backend.ErrNotFound {
        env = &types.EnvironmentState{
            Name:       "production",
            Datacenter: datacenter,
            CreatedAt:  time.Now(),
            Status:     types.EnvironmentStatusPending,
        }
    }
    
    // Update state
    env.Status = types.EnvironmentStatusProvisioning
    env.UpdatedAt = time.Now()
    
    err = manager.SaveEnvironment(ctx, datacenter, env)
    if err != nil {
        log.Fatal(err)
    }
    
    // Save component state (scoped to datacenter + environment)
    compState := &types.ComponentState{
        Name:       "api",
        Version:    "v1.0.0",
        Source:     "ghcr.io/myorg/api:v1.0.0",
        DeployedAt: time.Now(),
        Status:     "ready",
    }
    
    err = manager.SaveComponent(ctx, datacenter, "production", compState)
    if err != nil {
        log.Fatal(err)
    }
}
```

## Error Handling

```go
import "github.com/architect-io/arcctl/pkg/state/backend"

// Check for not found
if err == backend.ErrNotFound {
    // State doesn't exist
}

// Check for lock conflict
if lockErr, ok := err.(*backend.LockError); ok {
    fmt.Printf("Locked by %s since %v\n", 
        lockErr.Info.Who, 
        lockErr.Info.Created)
}
```
