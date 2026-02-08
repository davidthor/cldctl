# cldctl Architecture Documentation

This document outlines the implementation architecture for cldctl, a CLI tool designed to help developers create and deploy cloud-native applications in a portable fashion.

## Table of Contents

1. [Overview](#overview)
2. [Project Structure](#project-structure)
3. [Core Packages](#core-packages)
4. [Specification Versioning](#specification-versioning)
5. [State Management Framework](#state-management-framework)
6. [CLI Command Architecture](#cli-command-architecture)
7. [IaC Plugin System](#iac-plugin-system)
8. [Expression Engine](#expression-engine)
9. [OCI Artifact Management](#oci-artifact-management)
10. [Error Handling Strategy](#error-handling-strategy)

---

## Overview

cldctl is implemented in Go for performance, cross-platform compatibility, and ease of distribution as a single binary. The architecture follows Go best practices with clear separation of concerns, interface-driven design for extensibility, and comprehensive testing support.

### Design Principles

1. **Interface-Driven Design**: Core abstractions are defined as interfaces, enabling extensibility and testability
2. **Versioned Specifications**: Component and datacenter schemas support multiple versions with forward/backward compatibility
3. **Pluggable Backends**: State management, IaC execution, and OCI operations are implemented as pluggable providers
4. **Separation of Concerns**: Clear boundaries between parsing, planning, execution, and state management
5. **Fail-Fast Validation**: Comprehensive validation at parse time to catch errors early

---

## Project Structure

```
cldctl/
├── cmd/
│   └── cldctl/
│       └── main.go                    # CLI entry point
├── internal/
│   ├── cli/                           # CLI command implementations
│   │   ├── root.go
│   │   ├── component/
│   │   │   ├── build.go
│   │   │   ├── tag.go
│   │   │   ├── push.go
│   │   │   ├── deploy.go
│   │   │   ├── destroy.go
│   │   │   ├── list.go
│   │   │   └── get.go
│   │   ├── datacenter/
│   │   │   ├── build.go
│   │   │   ├── tag.go
│   │   │   ├── push.go
│   │   │   └── deploy.go
│   │   ├── environment/
│   │   │   ├── list.go
│   │   │   ├── get.go
│   │   │   ├── create.go
│   │   │   ├── update.go
│   │   │   └── destroy.go
│   │   ├── logs.go                    # cldctl logs command
│   │   ├── observability.go           # cldctl observability dashboard command
│   │   ├── browser.go                 # Shared browser-opening utility
│   │   └── up/
│   │       └── up.go
│   ├── config/                        # Configuration loading
│   │   └── config.go
│   └── ui/                            # Terminal UI helpers
│       ├── progress.go
│       ├── prompt.go
│       └── table.go
├── pkg/
│   ├── schema/                        # Schema parsing (public API)
│   │   ├── component/
│   │   │   ├── component.go           # Public interface
│   │   │   ├── loader.go              # Version-detecting loader
│   │   │   ├── v1/                    # Version 1 implementation
│   │   │   │   ├── schema.go
│   │   │   │   ├── types.go
│   │   │   │   ├── parser.go
│   │   │   │   ├── validator.go
│   │   │   │   └── transformer.go
│   │   │   └── internal/              # Internal representations
│   │   │       └── types.go
│   │   ├── datacenter/
│   │   │   ├── datacenter.go          # Public interface
│   │   │   ├── loader.go
│   │   │   ├── v1/
│   │   │   │   ├── schema.go
│   │   │   │   ├── types.go
│   │   │   │   ├── parser.go
│   │   │   │   ├── validator.go
│   │   │   │   └── transformer.go
│   │   │   └── internal/
│   │   │       └── types.go
│   │   └── environment/
│   │       ├── environment.go         # Public interface
│   │       ├── loader.go
│   │       ├── v1/
│   │       │   ├── schema.go
│   │       │   ├── types.go
│   │       │   ├── parser.go
│   │       │   ├── validator.go
│   │       │   └── transformer.go
│   │       └── internal/
│   │           └── types.go
│   ├── state/                         # State management framework
│   │   ├── state.go                   # Core interfaces
│   │   ├── manager.go                 # State manager implementation
│   │   ├── lock.go                    # Locking interfaces
│   │   ├── backend/                   # Backend implementations
│   │   │   ├── backend.go             # Backend interface
│   │   │   ├── local/
│   │   │   │   └── local.go
│   │   │   ├── s3/
│   │   │   │   └── s3.go
│   │   │   ├── gcs/
│   │   │   │   └── gcs.go
│   │   │   └── azurerm/
│   │   │       └── azurerm.go
│   │   └── types/                     # State data structures
│   │       ├── datacenter.go
│   │       ├── environment.go
│   │       └── resource.go
│   ├── engine/                        # Core execution engine
│   │   ├── graph/                     # Dependency graph
│   │   │   ├── graph.go
│   │   │   ├── node.go
│   │   │   └── resolver.go
│   │   ├── planner/                   # Execution planning
│   │   │   ├── planner.go
│   │   │   ├── diff.go
│   │   │   └── plan.go
│   │   ├── executor/                  # Plan execution
│   │   │   ├── executor.go
│   │   │   ├── hooks.go
│   │   │   └── runner.go
│   │   └── expression/                # Expression evaluation
│   │       ├── expression.go
│   │       ├── parser.go
│   │       ├── evaluator.go
│   │       └── functions.go
│   ├── iac/                           # IaC plugin framework
│   │   ├── plugin.go                  # Plugin interface
│   │   ├── registry.go                # Plugin registry
│   │   ├── pulumi/
│   │   │   ├── pulumi.go
│   │   │   ├── runner.go
│   │   │   └── state.go
│   │   └── opentofu/
│   │       ├── opentofu.go
│   │       ├── runner.go
│   │       └── state.go
│   ├── logs/                          # Log query plugin system
│   │   ├── querier.go                 # LogQuerier interface & types
│   │   ├── registry.go                # Backend registry
│   │   ├── multiplexer.go             # Output formatting & label construction
│   │   └── loki/                      # Loki query adapter
│   │       └── loki.go
│   ├── oci/                           # OCI artifact management
│   │   ├── client.go                  # OCI client interface
│   │   ├── artifact.go                # Artifact types
│   │   ├── builder.go                 # Build orchestration
│   │   ├── pusher.go                  # Push operations
│   │   └── puller.go                  # Pull operations
│   └── docker/                        # Docker operations
│       ├── client.go
│       ├── builder.go
│       └── registry.go
├── testdata/                          # Test fixtures
│   ├── components/
│   ├── datacenters/
│   └── environments/
├── go.mod
├── go.sum
└── Makefile
```

### Directory Rationale

| Directory   | Purpose                                                          |
| ----------- | ---------------------------------------------------------------- |
| `cmd/`      | CLI entry points (minimal code, just wiring)                     |
| `internal/` | Private implementation details (CLI, config, UI)                 |
| `pkg/`      | Public packages that can be imported by external tools           |
| `testdata/` | Test fixtures for component, datacenter, and environment parsing |

---

## Core Packages

### `pkg/schema` - Specification Parsing

The schema package provides parsing, validation, and transformation for all three configuration types. Each schema type follows the same pattern:

```go
// pkg/schema/component/component.go

package component

// Component represents a parsed and validated component configuration.
// This is the internal representation used throughout cldctl.
type Component interface {
    // Metadata
    Name() string
    Description() string

    // Resources
    Databases() []Database
    Buckets() []Bucket
    Deployments() []Deployment
    Functions() []Function
    Services() []Service
    Routes() []Route
    Cronjobs() []Cronjob

    // Configuration
    Variables() []Variable
    Dependencies() []Dependency

    // Version information
    SchemaVersion() string

    // Serialization
    ToYAML() ([]byte, error)
    ToJSON() ([]byte, error)
}

// Loader loads and parses component configurations
type Loader interface {
    // Load parses a component from the given path
    Load(path string) (Component, error)

    // LoadFromBytes parses a component from raw bytes
    LoadFromBytes(data []byte) (Component, error)

    // Validate validates a component without fully parsing
    Validate(path string) error
}

// NewLoader creates a new component loader that auto-detects schema version
func NewLoader() Loader {
    return &versionDetectingLoader{
        parsers: map[string]Parser{
            "v1": v1.NewParser(),
            // Future versions added here
        },
    }
}
```

### `pkg/state` - State Management Framework

The state package provides an extensible framework for managing cldctl state across different backends.

```go
// pkg/state/state.go

package state

import "context"

// StateManager provides high-level state operations
type StateManager interface {
    // Datacenter operations
    GetDatacenter(ctx context.Context, name string) (*DatacenterState, error)
    SaveDatacenter(ctx context.Context, state *DatacenterState) error
    DeleteDatacenter(ctx context.Context, name string) error

    // Environment operations
    ListEnvironments(ctx context.Context) ([]EnvironmentRef, error)
    GetEnvironment(ctx context.Context, name string) (*EnvironmentState, error)
    SaveEnvironment(ctx context.Context, state *EnvironmentState) error
    DeleteEnvironment(ctx context.Context, name string) error

    // Resource operations
    GetResource(ctx context.Context, env, component, resource string) (*ResourceState, error)
    SaveResource(ctx context.Context, env string, state *ResourceState) error
    DeleteResource(ctx context.Context, env, component, resource string) error

    // Locking
    Lock(ctx context.Context, scope LockScope) (Lock, error)

    // Backend info
    Backend() Backend
}
```

### `pkg/engine` - Execution Engine

The engine package orchestrates the deployment process, from parsing configurations to executing IaC modules.

```go
// pkg/engine/executor/executor.go

package executor

// Executor orchestrates the deployment process
type Executor interface {
    // Deploy deploys or updates a component in an environment
    Deploy(ctx context.Context, opts DeployOptions) (*DeployResult, error)

    // Destroy removes a component from an environment
    Destroy(ctx context.Context, opts DestroyOptions) (*DestroyResult, error)

    // Plan generates an execution plan without applying changes
    Plan(ctx context.Context, opts PlanOptions) (*Plan, error)
}

// DeployOptions configures a deployment operation
type DeployOptions struct {
    Environment   string
    ComponentName string
    Config        component.Component  // Parsed component
    Variables     map[string]string
    Targets       []string            // Optional: specific resources to target
    AutoApprove   bool
}
```

---

## Specification Versioning

The versioning system allows cldctl to support multiple versions of component and datacenter specifications simultaneously. This is critical for backward compatibility as specifications evolve.

### Version Detection

Each configuration file can optionally declare its schema version. If not specified, the loader infers the version based on the file structure.

```yaml
# cloud.component.yml
version: v1 # Optional, defaults to latest stable
name: my-app
# ...
```

```hcl
# datacenter.dc
version = "v1"  # Optional, defaults to latest stable

variable "region" {
  # ...
}
```

### Internal Representation

External schemas are transformed into internal representations. This decouples the external format from internal processing:

```go
// pkg/schema/component/internal/types.go

package internal

// InternalComponent is the canonical internal representation
// All version-specific parsers transform to this type
type InternalComponent struct {
    Name        string
    Description string

    Databases   []InternalDatabase
    Buckets     []InternalBucket
    Deployments []InternalDeployment
    Functions   []InternalFunction
    Services    []InternalService
    Routes      []InternalRoute
    Cronjobs    []InternalCronjob

    Variables    []InternalVariable
    Dependencies []InternalDependency

    SourceVersion string  // Original schema version
}

// InternalDatabase represents a database requirement
type InternalDatabase struct {
    Name       string
    Type       string  // e.g., "postgres"
    Version    string  // e.g., "^15"
    Migrations *InternalMigrations
}

// InternalDeployment represents a deployment workload
type InternalDeployment struct {
    Name        string
    Image       string              // Set if using pre-built image
    Build       *InternalBuild      // Set if building from source
    Command     []string
    Entrypoint  []string
    Environment map[string]string   // May contain expressions
    CPU         string
    Memory      string
    Replicas    int
    Volumes     []InternalVolume
    Probes      InternalProbes
}
```

### Version-Specific Transformers

Each schema version has a transformer that converts from the external format to the internal representation:

```go
// pkg/schema/component/v1/transformer.go

package v1

import "github.com/davidthor/cldctl/pkg/schema/component/internal"

// Transformer converts v1 schema to internal representation
type Transformer struct{}

func (t *Transformer) Transform(v1 *SchemaV1) (*internal.InternalComponent, error) {
    ic := &internal.InternalComponent{
        Name:          v1.Name,
        Description:   v1.Description,
        SourceVersion: "v1",
    }

    // Transform databases
    for name, db := range v1.Databases {
        idb, err := t.transformDatabase(name, db)
        if err != nil {
            return nil, fmt.Errorf("database %s: %w", name, err)
        }
        ic.Databases = append(ic.Databases, idb)
    }

    // Transform other resource types...

    return ic, nil
}

func (t *Transformer) transformDatabase(name string, db DatabaseV1) (internal.InternalDatabase, error) {
    dbType, version, err := parseTypeVersion(db.Type)
    if err != nil {
        return internal.InternalDatabase{}, err
    }

    return internal.InternalDatabase{
        Name:       name,
        Type:       dbType,
        Version:    version,
        Migrations: t.transformMigrations(db.Migrations),
    }, nil
}
```

### Adding New Schema Versions

When the specification evolves, add a new version package:

```go
// pkg/schema/component/v2/schema.go

package v2

// SchemaV2 represents the v2 component schema
type SchemaV2 struct {
    Version     string `yaml:"version"`
    Name        string `yaml:"name"`
    Description string `yaml:"description,omitempty"`

    // New in v2: resource groups for organization
    ResourceGroups map[string]ResourceGroupV2 `yaml:"resourceGroups,omitempty"`

    // Existing resources (may have updated structure)
    Databases   map[string]DatabaseV2   `yaml:"databases,omitempty"`
    // ...
}
```

Then register the new parser in the loader:

```go
// pkg/schema/component/loader.go

func NewLoader() Loader {
    return &versionDetectingLoader{
        parsers: map[string]Parser{
            "v1": v1.NewParser(),
            "v2": v2.NewParser(),  // Add new version
        },
        defaultVersion: "v1",  // Keep v1 as default for compatibility
    }
}
```

---

## State Management Framework

The state management framework is designed to be extensible, allowing new backends to be added without modifying core logic.

### Backend Interface

```go
// pkg/state/backend/backend.go

package backend

import (
    "context"
    "io"
)

// Backend defines the interface for state storage backends
type Backend interface {
    // Type returns the backend type identifier (e.g., "s3", "local")
    Type() string

    // Read reads state data from the given path
    Read(ctx context.Context, path string) (io.ReadCloser, error)

    // Write writes state data to the given path
    Write(ctx context.Context, path string, data io.Reader) error

    // Delete removes state data at the given path
    Delete(ctx context.Context, path string) error

    // List lists state files under the given prefix
    List(ctx context.Context, prefix string) ([]string, error)

    // Exists checks if a state file exists
    Exists(ctx context.Context, path string) (bool, error)

    // Lock acquires a lock for the given path
    Lock(ctx context.Context, path string, info LockInfo) (Lock, error)
}

// BackendConfig holds configuration for creating a backend
type BackendConfig struct {
    Type   string            // Backend type
    Config map[string]string // Backend-specific configuration
}

// Lock represents an acquired lock
type Lock interface {
    // ID returns the lock identifier
    ID() string

    // Unlock releases the lock
    Unlock(ctx context.Context) error

    // Info returns lock metadata
    Info() LockInfo
}

// LockInfo contains metadata about a lock
type LockInfo struct {
    ID        string
    Path      string
    Who       string    // User or CI job identity
    Operation string    // What operation holds the lock
    Created   time.Time
}
```

### Backend Registry

```go
// pkg/state/backend/registry.go

package backend

import (
    "fmt"
    "sync"
)

// Factory creates a backend from configuration
type Factory func(config map[string]string) (Backend, error)

// Registry manages backend factories
type Registry struct {
    mu        sync.RWMutex
    factories map[string]Factory
}

// DefaultRegistry is the global backend registry
var DefaultRegistry = &Registry{
    factories: make(map[string]Factory),
}

// Register adds a backend factory to the registry
func (r *Registry) Register(backendType string, factory Factory) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.factories[backendType] = factory
}

// Create instantiates a backend from configuration
func (r *Registry) Create(config BackendConfig) (Backend, error) {
    r.mu.RLock()
    factory, ok := r.factories[config.Type]
    r.mu.RUnlock()

    if !ok {
        return nil, fmt.Errorf("unknown backend type: %s", config.Type)
    }

    return factory(config.Config)
}

// Initialize default backends
func init() {
    DefaultRegistry.Register("local", local.NewBackend)
    DefaultRegistry.Register("s3", s3.NewBackend)
    DefaultRegistry.Register("gcs", gcs.NewBackend)
    DefaultRegistry.Register("azurerm", azurerm.NewBackend)
}
```

### S3 Backend Implementation

```go
// pkg/state/backend/s3/s3.go

package s3

import (
    "context"
    "fmt"
    "io"
    "path"

    "github.com/aws/aws-sdk-go-v2/service/s3"
    "github.com/davidthor/cldctl/pkg/state/backend"
)

// Backend implements the state backend interface for S3-compatible storage
type Backend struct {
    client *s3.Client
    bucket string
    prefix string
    region string
}

// NewBackend creates a new S3 backend
func NewBackend(config map[string]string) (backend.Backend, error) {
    bucket, ok := config["bucket"]
    if !ok {
        return nil, fmt.Errorf("s3 backend requires 'bucket' configuration")
    }

    region := config["region"]
    if region == "" {
        region = "us-east-1"
    }

    // Create AWS config and S3 client
    client, err := createS3Client(config)
    if err != nil {
        return nil, fmt.Errorf("failed to create S3 client: %w", err)
    }

    return &Backend{
        client: client,
        bucket: bucket,
        prefix: config["key"],
        region: region,
    }, nil
}

func (b *Backend) Type() string {
    return "s3"
}

func (b *Backend) Read(ctx context.Context, statePath string) (io.ReadCloser, error) {
    key := path.Join(b.prefix, statePath)

    output, err := b.client.GetObject(ctx, &s3.GetObjectInput{
        Bucket: &b.bucket,
        Key:    &key,
    })
    if err != nil {
        return nil, fmt.Errorf("failed to read state from s3://%s/%s: %w", b.bucket, key, err)
    }

    return output.Body, nil
}

func (b *Backend) Write(ctx context.Context, statePath string, data io.Reader) error {
    key := path.Join(b.prefix, statePath)

    _, err := b.client.PutObject(ctx, &s3.PutObjectInput{
        Bucket: &b.bucket,
        Key:    &key,
        Body:   data,
    })
    if err != nil {
        return fmt.Errorf("failed to write state to s3://%s/%s: %w", b.bucket, key, err)
    }

    return nil
}

func (b *Backend) Lock(ctx context.Context, statePath string, info backend.LockInfo) (backend.Lock, error) {
    // Implement DynamoDB-based locking for S3
    // Similar to Terraform's S3 backend locking strategy
    // ...
}
```

### Adding a New Backend

To add a new state backend (e.g., for Consul):

1. Create a new package under `pkg/state/backend/`:

```go
// pkg/state/backend/consul/consul.go

package consul

import "github.com/davidthor/cldctl/pkg/state/backend"

type Backend struct {
    // Consul-specific fields
}

func NewBackend(config map[string]string) (backend.Backend, error) {
    // Implementation
}

func (b *Backend) Type() string {
    return "consul"
}

// Implement remaining interface methods...
```

2. Register the backend in an init function or explicitly:

```go
// In your init or setup code
backend.DefaultRegistry.Register("consul", consul.NewBackend)
```

---

## CLI Command Architecture

The CLI uses [Cobra](https://github.com/spf13/cobra) for command structure and [Viper](https://github.com/spf13/viper) for configuration management.

### Command Structure

```go
// internal/cli/root.go

package cli

import (
    "github.com/spf13/cobra"
    "github.com/spf13/viper"
)

func NewRootCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "cldctl",
        Short: "Deploy cloud-native applications anywhere",
        Long:  `cldctl is a CLI tool for deploying portable cloud applications.`,
    }

    // Global flags
    cmd.PersistentFlags().String("backend", "local", "State backend type")
    cmd.PersistentFlags().StringArray("backend-config", nil, "Backend configuration (key=value)")

    // Bind to viper for env var support
    viper.BindPFlag("backend", cmd.PersistentFlags().Lookup("backend"))
    viper.SetEnvPrefix("CLDCTL")
    viper.AutomaticEnv()

    // Add subcommands
    cmd.AddCommand(
        NewComponentCmd(),
        NewDatacenterCmd(),
        NewEnvironmentCmd(),
        NewUpCmd(),
    )

    // Add aliases
    dcCmd := NewDatacenterCmd()
    dcCmd.Use = "dc"
    dcCmd.Aliases = []string{"datacenter"}
    cmd.AddCommand(dcCmd)

    envCmd := NewEnvironmentCmd()
    envCmd.Use = "env"
    envCmd.Aliases = []string{"environment"}
    cmd.AddCommand(envCmd)

    return cmd
}
```

### Component Deploy Command Example

```go
// internal/cli/component/deploy.go

package component

import (
    "context"
    "fmt"

    "github.com/spf13/cobra"
    "github.com/davidthor/cldctl/internal/ui"
    "github.com/davidthor/cldctl/pkg/engine/executor"
    "github.com/davidthor/cldctl/pkg/schema/component"
    "github.com/davidthor/cldctl/pkg/state"
)

func NewDeployCmd() *cobra.Command {
    var (
        environment string
        configRef   string
        variables   []string
        varFile     string
        autoApprove bool
        targets     []string
    )

    cmd := &cobra.Command{
        Use:   "deploy <name>",
        Short: "Deploy a component to an environment",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()
            componentName := args[0]

            // Load state manager from backend config
            stateManager, err := state.NewManagerFromFlags(cmd)
            if err != nil {
                return fmt.Errorf("failed to initialize state: %w", err)
            }

            // Parse component configuration
            loader := component.NewLoader()
            comp, err := loadComponent(loader, configRef)
            if err != nil {
                return fmt.Errorf("failed to load component: %w", err)
            }

            // Parse variables
            vars, err := parseVariables(variables, varFile)
            if err != nil {
                return fmt.Errorf("failed to parse variables: %w", err)
            }

            // Create executor
            exec := executor.New(stateManager)

            // Generate plan
            plan, err := exec.Plan(ctx, executor.PlanOptions{
                Environment:   environment,
                ComponentName: componentName,
                Config:        comp,
                Variables:     vars,
                Targets:       targets,
            })
            if err != nil {
                return fmt.Errorf("failed to create plan: %w", err)
            }

            // Display plan
            ui.DisplayPlan(plan)

            // Prompt for confirmation unless auto-approve
            if !autoApprove {
                confirmed, err := ui.Confirm("Proceed with deployment?")
                if err != nil {
                    return err
                }
                if !confirmed {
                    fmt.Println("Deployment cancelled.")
                    return nil
                }
            }

            // Execute deployment
            result, err := exec.Deploy(ctx, executor.DeployOptions{
                Environment:   environment,
                ComponentName: componentName,
                Config:        comp,
                Variables:     vars,
                Targets:       targets,
                AutoApprove:   autoApprove,
            })
            if err != nil {
                return fmt.Errorf("deployment failed: %w", err)
            }

            // Display results
            ui.DisplayDeployResult(result)

            return nil
        },
    }

    cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
    cmd.Flags().StringVarP(&configRef, "config", "c", "", "Component config (path or OCI image)")
    cmd.Flags().StringArrayVar(&variables, "var", nil, "Set variable (key=value)")
    cmd.Flags().StringVar(&varFile, "var-file", "", "Load variables from file")
    cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation")
    cmd.Flags().StringArrayVar(&targets, "target", nil, "Target specific resources")

    cmd.MarkFlagRequired("environment")
    cmd.MarkFlagRequired("config")

    return cmd
}
```

---

## IaC Plugin System

The IaC plugin system allows cldctl to execute modules written in different Infrastructure-as-Code frameworks.

### Plugin Interface

```go
// pkg/iac/plugin.go

package iac

import (
    "context"
    "io"
)

// Plugin defines the interface for IaC framework plugins
type Plugin interface {
    // Name returns the plugin identifier (e.g., "pulumi", "opentofu")
    Name() string

    // Preview generates a preview of changes
    Preview(ctx context.Context, opts RunOptions) (*PreviewResult, error)

    // Apply applies the module and returns outputs
    Apply(ctx context.Context, opts RunOptions) (*ApplyResult, error)

    // Destroy destroys resources created by the module
    Destroy(ctx context.Context, opts RunOptions) error

    // Refresh refreshes state without applying changes
    Refresh(ctx context.Context, opts RunOptions) (*RefreshResult, error)
}

// RunOptions configures a plugin execution
type RunOptions struct {
    // Module configuration
    ModuleSource string            // OCI image reference or local path
    ModulePath   string            // Path within module if local

    // Inputs and outputs
    Inputs  map[string]interface{} // Input values

    // State management
    StateReader io.Reader          // Existing state (nil for new)
    StateWriter io.Writer          // Where to write new state

    // Execution environment
    WorkDir     string             // Working directory
    Environment map[string]string  // Environment variables
    Volumes     []VolumeMount      // Volume mounts (for dockerBuild, etc.)

    // Output handling
    Stdout io.Writer
    Stderr io.Writer
}

// ApplyResult contains the result of an apply operation
type ApplyResult struct {
    Outputs map[string]OutputValue
    State   []byte  // Serialized state
}

// OutputValue represents a module output
type OutputValue struct {
    Value     interface{}
    Sensitive bool
}
```

### Pulumi Plugin Implementation

```go
// pkg/iac/pulumi/pulumi.go

package pulumi

import (
    "context"
    "encoding/json"
    "fmt"
    "os/exec"

    "github.com/davidthor/cldctl/pkg/iac"
)

// Plugin implements the IaC plugin interface for Pulumi
type Plugin struct {
    // Configuration
    runtime string // e.g., "nodejs", "python", "go"
}

func NewPlugin() *Plugin {
    return &Plugin{}
}

func (p *Plugin) Name() string {
    return "pulumi"
}

func (p *Plugin) Apply(ctx context.Context, opts iac.RunOptions) (*iac.ApplyResult, error) {
    // Prepare the module environment
    workDir, cleanup, err := p.prepareModule(ctx, opts)
    if err != nil {
        return nil, fmt.Errorf("failed to prepare module: %w", err)
    }
    defer cleanup()

    // Write inputs as Pulumi config
    if err := p.writeConfig(workDir, opts.Inputs); err != nil {
        return nil, fmt.Errorf("failed to write config: %w", err)
    }

    // Import existing state if provided
    if opts.StateReader != nil {
        if err := p.importState(ctx, workDir, opts.StateReader); err != nil {
            return nil, fmt.Errorf("failed to import state: %w", err)
        }
    }

    // Run pulumi up
    cmd := exec.CommandContext(ctx, "pulumi", "up", "--yes", "--json")
    cmd.Dir = workDir
    cmd.Env = p.buildEnvironment(opts.Environment)
    cmd.Stdout = opts.Stdout
    cmd.Stderr = opts.Stderr

    if err := cmd.Run(); err != nil {
        return nil, fmt.Errorf("pulumi up failed: %w", err)
    }

    // Extract outputs
    outputs, err := p.getOutputs(ctx, workDir)
    if err != nil {
        return nil, fmt.Errorf("failed to get outputs: %w", err)
    }

    // Export state
    state, err := p.exportState(ctx, workDir)
    if err != nil {
        return nil, fmt.Errorf("failed to export state: %w", err)
    }

    return &iac.ApplyResult{
        Outputs: outputs,
        State:   state,
    }, nil
}

func (p *Plugin) prepareModule(ctx context.Context, opts iac.RunOptions) (string, func(), error) {
    // If module source is an OCI reference, pull it
    // If local path, use directly
    // Return working directory and cleanup function
    // ...
}
```

### Native Plugin Implementation

The native plugin provides lightweight resource provisioning without external IaC tooling, optimized for local development and ephemeral environments.

```go
// pkg/iac/native/native.go

package native

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/davidthor/cldctl/pkg/iac"
    "github.com/davidthor/cldctl/pkg/iac/native/docker"
    "github.com/davidthor/cldctl/pkg/iac/native/process"
)

// Plugin implements the IaC plugin interface for native execution
type Plugin struct {
    dockerClient *docker.Client
    procManager  *process.Manager
}

func NewPlugin() (*Plugin, error) {
    dockerClient, err := docker.NewClient()
    if err != nil {
        return nil, fmt.Errorf("failed to create docker client: %w", err)
    }

    return &Plugin{
        dockerClient: dockerClient,
        procManager:  process.NewManager(),
    }, nil
}

func (p *Plugin) Name() string {
    return "native"
}

func (p *Plugin) Apply(ctx context.Context, opts iac.RunOptions) (*iac.ApplyResult, error) {
    // Load module definition
    module, err := p.loadModule(opts.ModuleSource)
    if err != nil {
        return nil, fmt.Errorf("failed to load module: %w", err)
    }

    // Load existing state (if any)
    var existingState *State
    if opts.StateReader != nil {
        existingState, err = p.loadState(opts.StateReader)
        if err != nil {
            return nil, fmt.Errorf("failed to load state: %w", err)
        }
    }

    // Resolve inputs with expressions
    resolvedInputs, err := p.resolveInputs(module.Inputs, opts.Inputs)
    if err != nil {
        return nil, fmt.Errorf("failed to resolve inputs: %w", err)
    }

    // Apply each resource
    state := &State{Resources: make(map[string]*ResourceState)}
    for name, resource := range module.Resources {
        resourceState, err := p.applyResource(ctx, name, resource, resolvedInputs, existingState)
        if err != nil {
            // Rollback on failure
            p.rollback(ctx, state)
            return nil, fmt.Errorf("failed to apply resource %s: %w", name, err)
        }
        state.Resources[name] = resourceState
    }

    // Resolve outputs
    outputs, err := p.resolveOutputs(module.Outputs, state, resolvedInputs)
    if err != nil {
        return nil, fmt.Errorf("failed to resolve outputs: %w", err)
    }

    // Serialize state
    stateBytes, err := json.Marshal(state)
    if err != nil {
        return nil, fmt.Errorf("failed to serialize state: %w", err)
    }

    return &iac.ApplyResult{
        Outputs: outputs,
        State:   stateBytes,
    }, nil
}

func (p *Plugin) applyResource(ctx context.Context, name string, resource Resource, inputs map[string]interface{}, existing *State) (*ResourceState, error) {
    switch resource.Type {
    case "docker:container":
        return p.applyDockerContainer(ctx, name, resource, inputs, existing)
    case "docker:network":
        return p.applyDockerNetwork(ctx, name, resource, inputs, existing)
    case "docker:volume":
        return p.applyDockerVolume(ctx, name, resource, inputs, existing)
    case "process":
        return p.applyProcess(ctx, name, resource, inputs, existing)
    case "exec":
        return p.applyExec(ctx, name, resource, inputs)
    default:
        return nil, fmt.Errorf("unknown resource type: %s", resource.Type)
    }
}

func (p *Plugin) applyDockerContainer(ctx context.Context, name string, resource Resource, inputs map[string]interface{}, existing *State) (*ResourceState, error) {
    props := resource.Properties

    // Check if container already exists and is running
    if existing != nil {
        if rs, ok := existing.Resources[name]; ok {
            if containerID, ok := rs.ID.(string); ok {
                running, err := p.dockerClient.IsRunning(ctx, containerID)
                if err == nil && running {
                    // Container still running, reuse it
                    return rs, nil
                }
            }
        }
    }

    // Resolve port mappings
    ports, err := p.resolvePorts(props.Ports)
    if err != nil {
        return nil, err
    }

    // Create and start container
    containerID, err := p.dockerClient.Run(ctx, docker.RunOptions{
        Image:       props.Image,
        Name:        props.Name,
        Command:     props.Command,
        Entrypoint:  props.Entrypoint,
        Environment: props.Environment,
        Ports:       ports,
        Volumes:     props.Volumes,
        Network:     props.Network,
        Healthcheck: props.Healthcheck,
        Restart:     props.Restart,
    })
    if err != nil {
        return nil, err
    }

    // Wait for health check if configured
    if props.Healthcheck != nil {
        if err := p.dockerClient.WaitHealthy(ctx, containerID); err != nil {
            p.dockerClient.Remove(ctx, containerID)
            return nil, fmt.Errorf("container failed health check: %w", err)
        }
    }

    return &ResourceState{
        Type:       "docker:container",
        ID:         containerID,
        Properties: props,
        Outputs: map[string]interface{}{
            "container_id": containerID,
            "ports":        ports,
        },
    }, nil
}

// Destroy removes all resources managed by the native plugin
func (p *Plugin) Destroy(ctx context.Context, opts iac.RunOptions) error {
    if opts.StateReader == nil {
        return nil // Nothing to destroy
    }

    state, err := p.loadState(opts.StateReader)
    if err != nil {
        return fmt.Errorf("failed to load state: %w", err)
    }

    // Destroy in reverse order
    for name, rs := range state.Resources {
        if err := p.destroyResource(ctx, name, rs); err != nil {
            // Log but continue destroying other resources
            fmt.Fprintf(opts.Stderr, "warning: failed to destroy %s: %v\n", name, err)
        }
    }

    return nil
}

func (p *Plugin) destroyResource(ctx context.Context, name string, rs *ResourceState) error {
    switch rs.Type {
    case "docker:container":
        return p.dockerClient.Remove(ctx, rs.ID.(string))
    case "docker:network":
        return p.dockerClient.RemoveNetwork(ctx, rs.ID.(string))
    case "docker:volume":
        return p.dockerClient.RemoveVolume(ctx, rs.ID.(string))
    case "process":
        return p.procManager.Stop(rs.ID.(int))
    case "exec":
        return nil // One-time execution, nothing to destroy
    default:
        return fmt.Errorf("unknown resource type: %s", rs.Type)
    }
}

// Preview is a no-op for native plugin (no drift detection)
func (p *Plugin) Preview(ctx context.Context, opts iac.RunOptions) (*iac.PreviewResult, error) {
    // Native plugin doesn't support preview/drift detection
    // Return empty preview indicating all resources will be created/updated
    return &iac.PreviewResult{
        Changes: []iac.ResourceChange{},
    }, nil
}

// Refresh is a no-op for native plugin (no state refresh)
func (p *Plugin) Refresh(ctx context.Context, opts iac.RunOptions) (*iac.RefreshResult, error) {
    // Native plugin trusts stored state, no refresh needed
    return &iac.RefreshResult{}, nil
}
```

### Native Module Definition Format

Native modules use a YAML-based declarative format:

```go
// pkg/iac/native/module.go

package native

// Module represents a native module definition
type Module struct {
    Plugin    string                    `yaml:"plugin"`    // Must be "native"
    Type      string                    `yaml:"type"`      // Primary resource type hint
    Inputs    map[string]InputDef       `yaml:"inputs"`
    Resources map[string]Resource       `yaml:"resources"`
    Outputs   map[string]OutputDef      `yaml:"outputs"`
}

// InputDef defines a module input
type InputDef struct {
    Type        string      `yaml:"type"`
    Required    bool        `yaml:"required"`
    Default     interface{} `yaml:"default"`
    Description string      `yaml:"description"`
    Sensitive   bool        `yaml:"sensitive"`
}

// Resource defines a native resource
type Resource struct {
    Type       string                 `yaml:"type"`
    Properties map[string]interface{} `yaml:"properties"`
    DependsOn  []string               `yaml:"depends_on"`
}

// OutputDef defines a module output
type OutputDef struct {
    Value       string `yaml:"value"`     // Expression to evaluate
    Description string `yaml:"description"`
    Sensitive   bool   `yaml:"sensitive"`
}

// State represents the persisted state of native resources
type State struct {
    Resources map[string]*ResourceState `json:"resources"`
}

// ResourceState represents a single resource's state
type ResourceState struct {
    Type       string                 `json:"type"`
    ID         interface{}            `json:"id"`
    Properties map[string]interface{} `json:"properties"`
    Outputs    map[string]interface{} `json:"outputs"`
}
```

### Plugin Selection

The plugin registry selects the appropriate plugin based on the module's `plugin` property:

```go
// pkg/iac/registry.go

func (r *Registry) Get(name string) (Plugin, error) {
    switch name {
    case "pulumi", "":  // Default to Pulumi
        return pulumi.NewPlugin(), nil
    case "opentofu":
        return opentofu.NewPlugin(), nil
    case "native":
        return native.NewPlugin()
    default:
        return nil, fmt.Errorf("unknown plugin: %s", name)
    }
}
```

---

## Expression Engine

The expression engine evaluates `${{ }}` expressions in component configurations.

### Expression Parser

```go
// pkg/engine/expression/parser.go

package expression

import (
    "fmt"
    "regexp"
    "strings"
)

// Expression represents a parsed expression
type Expression struct {
    Raw      string       // Original expression text
    Segments []Segment    // Parsed segments
}

// Segment is part of an expression
type Segment interface {
    segment()
}

// LiteralSegment is a literal string
type LiteralSegment struct {
    Value string
}

func (LiteralSegment) segment() {}

// ReferenceSegment is a reference to a value
type ReferenceSegment struct {
    Path   []string      // e.g., ["databases", "main", "url"]
    Pipe   []PipeFunc    // Optional pipe functions
}

func (ReferenceSegment) segment() {}

// PipeFunc represents a pipe function call
type PipeFunc struct {
    Name string
    Args []string
}

// Parser parses expression strings
type Parser struct {
    expressionPattern *regexp.Regexp
}

func NewParser() *Parser {
    return &Parser{
        expressionPattern: regexp.MustCompile(`\$\{\{\s*(.+?)\s*\}\}`),
    }
}

// Parse parses a string that may contain expressions
func (p *Parser) Parse(input string) (*Expression, error) {
    expr := &Expression{Raw: input}

    matches := p.expressionPattern.FindAllStringSubmatchIndex(input, -1)
    if len(matches) == 0 {
        // No expressions, just a literal
        expr.Segments = []Segment{LiteralSegment{Value: input}}
        return expr, nil
    }

    lastEnd := 0
    for _, match := range matches {
        // Add literal segment before this expression
        if match[0] > lastEnd {
            expr.Segments = append(expr.Segments, LiteralSegment{
                Value: input[lastEnd:match[0]],
            })
        }

        // Parse the expression content
        exprContent := input[match[2]:match[3]]
        ref, err := p.parseReference(exprContent)
        if err != nil {
            return nil, fmt.Errorf("invalid expression %q: %w", exprContent, err)
        }
        expr.Segments = append(expr.Segments, ref)

        lastEnd = match[1]
    }

    // Add trailing literal if any
    if lastEnd < len(input) {
        expr.Segments = append(expr.Segments, LiteralSegment{
            Value: input[lastEnd:],
        })
    }

    return expr, nil
}

func (p *Parser) parseReference(content string) (ReferenceSegment, error) {
    // Parse reference path and optional pipe functions
    // e.g., "databases.main.url" or "dependents.*.routes.*.url | join \",\""

    parts := strings.Split(content, "|")
    pathStr := strings.TrimSpace(parts[0])

    ref := ReferenceSegment{
        Path: strings.Split(pathStr, "."),
    }

    // Parse pipe functions
    for i := 1; i < len(parts); i++ {
        pipeStr := strings.TrimSpace(parts[i])
        pf, err := p.parsePipeFunc(pipeStr)
        if err != nil {
            return ref, err
        }
        ref.Pipe = append(ref.Pipe, pf)
    }

    return ref, nil
}
```

### Expression Evaluator

```go
// pkg/engine/expression/evaluator.go

package expression

import (
    "fmt"
    "strings"
)

// EvalContext provides values for expression evaluation
type EvalContext struct {
    Databases    map[string]DatabaseOutputs
    Buckets      map[string]BucketOutputs
    Services     map[string]ServiceOutputs
    Routes       map[string]RouteOutputs
    Variables    map[string]interface{}
    Dependencies map[string]DependencyOutputs
    Dependents   map[string]DependentOutputs
}

// Evaluator evaluates parsed expressions
type Evaluator struct {
    functions map[string]PipeFuncImpl
}

func NewEvaluator() *Evaluator {
    return &Evaluator{
        functions: map[string]PipeFuncImpl{
            "join":   joinFunc,
            "first":  firstFunc,
            "length": lengthFunc,
            "map":    mapFunc,
            "where":  whereFunc,
        },
    }
}

// Evaluate evaluates an expression in the given context
func (e *Evaluator) Evaluate(expr *Expression, ctx *EvalContext) (interface{}, error) {
    if len(expr.Segments) == 1 {
        if lit, ok := expr.Segments[0].(LiteralSegment); ok {
            return lit.Value, nil
        }
    }

    var result strings.Builder
    for _, seg := range expr.Segments {
        switch s := seg.(type) {
        case LiteralSegment:
            result.WriteString(s.Value)
        case ReferenceSegment:
            val, err := e.evaluateReference(s, ctx)
            if err != nil {
                return nil, err
            }
            result.WriteString(fmt.Sprintf("%v", val))
        }
    }

    return result.String(), nil
}

func (e *Evaluator) evaluateReference(ref ReferenceSegment, ctx *EvalContext) (interface{}, error) {
    // Navigate the path to get the value
    var value interface{}
    var err error

    switch ref.Path[0] {
    case "databases":
        value, err = e.resolveDatabase(ref.Path[1:], ctx.Databases)
    case "buckets":
        value, err = e.resolveBucket(ref.Path[1:], ctx.Buckets)
    case "services":
        value, err = e.resolveService(ref.Path[1:], ctx.Services)
    case "routes":
        value, err = e.resolveRoute(ref.Path[1:], ctx.Routes)
    case "variables":
        value, err = e.resolveVariable(ref.Path[1:], ctx.Variables)
    case "dependencies":
        value, err = e.resolveDependency(ref.Path[1:], ctx.Dependencies)
    case "dependents":
        value, err = e.resolveDependents(ref.Path[1:], ctx.Dependents)
    default:
        return nil, fmt.Errorf("unknown reference type: %s", ref.Path[0])
    }

    if err != nil {
        return nil, err
    }

    // Apply pipe functions
    for _, pf := range ref.Pipe {
        fn, ok := e.functions[pf.Name]
        if !ok {
            return nil, fmt.Errorf("unknown pipe function: %s", pf.Name)
        }
        value, err = fn(value, pf.Args)
        if err != nil {
            return nil, fmt.Errorf("pipe function %s failed: %w", pf.Name, err)
        }
    }

    return value, nil
}
```

---

## OCI Artifact Management

The OCI package handles building, pushing, and pulling component and datacenter artifacts.

### Artifact Types

```go
// pkg/oci/artifact.go

package oci

// ArtifactType identifies the type of OCI artifact
type ArtifactType string

const (
    ArtifactTypeComponent  ArtifactType = "component"
    ArtifactTypeDatacenter ArtifactType = "datacenter"
    ArtifactTypeModule     ArtifactType = "module"
)

// Artifact represents an OCI artifact
type Artifact struct {
    Type      ArtifactType
    Reference string                 // OCI reference (repo:tag)
    Manifest  *ArtifactManifest
    Config    []byte                 // Artifact configuration
    Layers    []Layer
}

// ArtifactManifest describes artifact contents
type ArtifactManifest struct {
    SchemaVersion int
    MediaType     string
    Config        Descriptor
    Layers        []Descriptor
    Annotations   map[string]string
}

// Layer represents a layer in the artifact
type Layer struct {
    MediaType string
    Digest    string
    Size      int64
    Data      []byte
}
```

### Component Builder

```go
// pkg/oci/builder.go

package oci

import (
    "context"
    "fmt"

    "github.com/davidthor/cldctl/pkg/docker"
    "github.com/davidthor/cldctl/pkg/schema/component"
)

// ComponentBuilder builds component OCI artifacts
type ComponentBuilder struct {
    docker *docker.Client
    oci    *Client
}

// BuildOptions configures a component build
type BuildOptions struct {
    ComponentPath string
    Tag           string
    ArtifactTags  map[string]string  // Override child artifact tags
    Platform      string             // Target platform
    NoCache       bool
}

// BuildResult contains the results of a build
type BuildResult struct {
    RootArtifact  string
    ChildArtifacts map[string]string // name -> reference
}

// Build builds a component and all its child artifacts
func (b *ComponentBuilder) Build(ctx context.Context, opts BuildOptions) (*BuildResult, error) {
    // Load component
    loader := component.NewLoader()
    comp, err := loader.Load(opts.ComponentPath)
    if err != nil {
        return nil, fmt.Errorf("failed to load component: %w", err)
    }

    result := &BuildResult{
        RootArtifact:   opts.Tag,
        ChildArtifacts: make(map[string]string),
    }

    // Build child artifacts (container images)

    // Build deployments
    for _, dep := range comp.Deployments() {
        if dep.Build() != nil {
            childTag := b.childArtifactTag(opts.Tag, "deployment", dep.Name(), opts.ArtifactTags)
            if err := b.buildContainer(ctx, dep.Build(), childTag, opts); err != nil {
                return nil, fmt.Errorf("failed to build deployment %s: %w", dep.Name(), err)
            }
            result.ChildArtifacts["deployment/"+dep.Name()] = childTag
        }
    }

    // Build functions
    for _, fn := range comp.Functions() {
        if fn.Build() != nil {
            childTag := b.childArtifactTag(opts.Tag, "function", fn.Name(), opts.ArtifactTags)
            if err := b.buildContainer(ctx, fn.Build(), childTag, opts); err != nil {
                return nil, fmt.Errorf("failed to build function %s: %w", fn.Name(), err)
            }
            result.ChildArtifacts["function/"+fn.Name()] = childTag
        }
    }

    // Build migrations
    for _, db := range comp.Databases() {
        if db.Migrations() != nil && db.Migrations().Build() != nil {
            childTag := b.childArtifactTag(opts.Tag, "migration", db.Name(), opts.ArtifactTags)
            if err := b.buildContainer(ctx, db.Migrations().Build(), childTag, opts); err != nil {
                return nil, fmt.Errorf("failed to build migration %s: %w", db.Name(), err)
            }
            result.ChildArtifacts["migration/"+db.Name()] = childTag
        }
    }

    // Build cronjobs
    for _, cj := range comp.Cronjobs() {
        if cj.Build() != nil {
            childTag := b.childArtifactTag(opts.Tag, "cronjob", cj.Name(), opts.ArtifactTags)
            if err := b.buildContainer(ctx, cj.Build(), childTag, opts); err != nil {
                return nil, fmt.Errorf("failed to build cronjob %s: %w", cj.Name(), err)
            }
            result.ChildArtifacts["cronjob/"+cj.Name()] = childTag
        }
    }

    // Create compiled component configuration
    compiledConfig := b.createCompiledConfig(comp, result.ChildArtifacts)

    // Build root artifact
    if err := b.buildRootArtifact(ctx, compiledConfig, opts.Tag); err != nil {
        return nil, fmt.Errorf("failed to build root artifact: %w", err)
    }

    return result, nil
}

func (b *ComponentBuilder) childArtifactTag(rootTag, artifactType, name string, overrides map[string]string) string {
    key := artifactType + "/" + name
    if override, ok := overrides[key]; ok {
        return override
    }

    // Default naming convention: <root-repo>-<type>-<name>:<root-tag>
    ref, _ := ParseReference(rootTag)
    return fmt.Sprintf("%s-%s-%s:%s", ref.Repository, artifactType, name, ref.Tag)
}
```

---

## Error Handling Strategy

cldctl uses structured errors throughout for better debugging and user feedback.

### Error Types

```go
// pkg/errors/errors.go

package errors

import (
    "fmt"
)

// ErrorCode identifies specific error conditions
type ErrorCode string

const (
    ErrCodeValidation   ErrorCode = "VALIDATION_ERROR"
    ErrCodeNotFound     ErrorCode = "NOT_FOUND"
    ErrCodeConflict     ErrorCode = "CONFLICT"
    ErrCodeLocked       ErrorCode = "STATE_LOCKED"
    ErrCodeBackend      ErrorCode = "BACKEND_ERROR"
    ErrCodeIaC          ErrorCode = "IAC_ERROR"
    ErrCodeTimeout      ErrorCode = "TIMEOUT"
    ErrCodePermission   ErrorCode = "PERMISSION_DENIED"
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

// Constructors for common errors

func ValidationError(message string, details map[string]interface{}) *Error {
    return &Error{
        Code:    ErrCodeValidation,
        Message: message,
        Details: details,
    }
}

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
```

### Error Handling in Commands

```go
// internal/cli/component/deploy.go

func handleDeployError(err error) {
    var arcErr *errors.Error
    if errors.As(err, &arcErr) {
        switch arcErr.Code {
        case errors.ErrCodeLocked:
            fmt.Fprintf(os.Stderr, "Error: State is locked\n\n")
            fmt.Fprintf(os.Stderr, "  Lock ID:    %s\n", arcErr.Details["lock_id"])
            fmt.Fprintf(os.Stderr, "  Locked by:  %s\n", arcErr.Details["locked_by"])
            fmt.Fprintf(os.Stderr, "  Operation:  %s\n", arcErr.Details["operation"])
            fmt.Fprintf(os.Stderr, "\nUse --force-unlock to break the lock (use with caution).\n")
            os.Exit(1)

        case errors.ErrCodeValidation:
            fmt.Fprintf(os.Stderr, "Error: Validation failed\n\n")
            fmt.Fprintf(os.Stderr, "  %s\n", arcErr.Message)
            if details, ok := arcErr.Details["errors"].([]string); ok {
                for _, d := range details {
                    fmt.Fprintf(os.Stderr, "  - %s\n", d)
                }
            }
            os.Exit(1)

        default:
            fmt.Fprintf(os.Stderr, "Error: %s\n", arcErr.Message)
            os.Exit(1)
        }
    }

    // Generic error
    fmt.Fprintf(os.Stderr, "Error: %v\n", err)
    os.Exit(1)
}
```

---

## Testing Strategy

### Unit Tests

Each package has comprehensive unit tests:

```go
// pkg/schema/component/v1/parser_test.go

package v1

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestParser_Parse(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        wantName string
        wantErr  bool
    }{
        {
            name: "basic component",
            input: `
name: my-app
databases:
  main:
    type: postgres:^15
`,
            wantName: "my-app",
            wantErr:  false,
        },
        {
            name:    "missing name",
            input:   `databases: {}`,
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            p := NewParser()
            result, err := p.ParseBytes([]byte(tt.input))

            if tt.wantErr {
                assert.Error(t, err)
                return
            }

            require.NoError(t, err)
            assert.Equal(t, tt.wantName, result.Name)
        })
    }
}
```

### Integration Tests

Integration tests use test fixtures:

```go
// pkg/engine/executor/executor_integration_test.go

//go:build integration

package executor

import (
    "context"
    "testing"

    "github.com/davidthor/cldctl/pkg/state/backend/local"
)

func TestExecutor_Deploy_Integration(t *testing.T) {
    // Set up test backend
    tmpDir := t.TempDir()
    backend, _ := local.NewBackend(map[string]string{"path": tmpDir})

    // Load test component
    comp, _ := component.NewLoader().Load("testdata/components/basic")

    // Create executor
    exec := New(state.NewManager(backend))

    // Deploy
    result, err := exec.Deploy(context.Background(), DeployOptions{
        Environment:   "test",
        ComponentName: "basic",
        Config:        comp,
    })

    require.NoError(t, err)
    assert.NotEmpty(t, result.Resources)
}
```

---

## Next Steps

See the following companion documents for additional details:

- [CONTRIBUTING.md](./CONTRIBUTING.md) - How to contribute new features
- [SPEC_VERSIONING.md](./SPEC_VERSIONING.md) - Detailed specification versioning guide
- [STATE_BACKENDS.md](./STATE_BACKENDS.md) - Guide to implementing new state backends
- [IAC_PLUGINS.md](./IAC_PLUGINS.md) - Guide to implementing new IaC plugins
