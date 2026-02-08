# Contributing to cldctl

This guide explains how to contribute new features to cldctl, including adding new resource types to components, new hooks to datacenters, new state backends, and new IaC plugins.

## Table of Contents

1. [Development Setup](#development-setup)
2. [Project Conventions](#project-conventions)
3. [Adding New Component Resource Types](#adding-new-component-resource-types)
4. [Adding New Datacenter Hooks](#adding-new-datacenter-hooks)
5. [Adding New State Backends](#adding-new-state-backends)
6. [Adding New IaC Plugins](#adding-new-iac-plugins)
7. [Testing Guidelines](#testing-guidelines)
8. [Documentation Requirements](#documentation-requirements)

---

## Development Setup

### Prerequisites

- Go 1.22 or later
- Docker (for building and testing containers)
- Make

### Getting Started

```bash
# Clone the repository
git clone https://github.com/davidthor/cldctl.git
cd cldctl

# Install dependencies
go mod download

# Build the CLI
make build

# Run tests
make test

# Run linter
make lint
```

### Project Layout

```
cldctl/
├── cmd/cldctl/          # CLI entry point
├── internal/            # Private packages (CLI implementation)
├── pkg/                 # Public packages (can be imported externally)
│   ├── schema/          # Configuration parsing
│   ├── state/           # State management
│   ├── engine/          # Execution engine
│   ├── iac/             # IaC plugins
│   └── oci/             # OCI artifact management
└── testdata/            # Test fixtures
```

---

## Project Conventions

### Code Style

- Follow [Effective Go](https://go.dev/doc/effective_go) guidelines
- Use `gofmt` for formatting (enforced by CI)
- Use `golangci-lint` for static analysis

### Naming Conventions

| Item            | Convention              | Example                       |
| --------------- | ----------------------- | ----------------------------- |
| Packages        | lowercase, single word  | `schema`, `state`, `engine`   |
| Interfaces      | Descriptive nouns       | `Backend`, `Plugin`, `Loader` |
| Implementations | Prefixed with type      | `S3Backend`, `PulumiPlugin`   |
| Test files      | `*_test.go`             | `parser_test.go`              |
| Test functions  | `Test<Function>_<Case>` | `TestParser_ValidComponent`   |

### Error Handling

- Use the custom error types in `pkg/errors`
- Wrap errors with context using `fmt.Errorf("context: %w", err)`
- Include actionable information in error messages

```go
// Good
return nil, errors.ValidationError(
    "invalid database type",
    map[string]interface{}{
        "type":      dbType,
        "supported": []string{"postgres", "mysql", "mongodb", "redis"},
    },
)

// Bad
return nil, fmt.Errorf("bad type")
```

### Interface Design

- Define interfaces where they are used, not where they are implemented
- Keep interfaces small and focused
- Use composition over large interfaces

```go
// Good - small, focused interfaces
type StateReader interface {
    Read(ctx context.Context, path string) (io.ReadCloser, error)
}

type StateWriter interface {
    Write(ctx context.Context, path string, data io.Reader) error
}

type Backend interface {
    StateReader
    StateWriter
    // Additional methods...
}

// Bad - monolithic interface
type Backend interface {
    // 20+ methods...
}
```

---

## Adding New Component Resource Types

This section describes how to add a new resource type to the component specification (e.g., adding `queues` for message queue support).

### Step 1: Define the Internal Representation

Add the internal type in `pkg/schema/component/internal/types.go`:

```go
// pkg/schema/component/internal/types.go

// InternalQueue represents a message queue requirement
type InternalQueue struct {
    Name       string
    Type       string   // e.g., "sqs", "rabbitmq", "kafka"
    MaxRetries int
    DLQ        bool     // Dead letter queue enabled
}

// Add to InternalComponent
type InternalComponent struct {
    // ... existing fields
    Queues []InternalQueue
}
```

### Step 2: Add Public Interface Methods

Update the public interface in `pkg/schema/component/component.go`:

```go
// pkg/schema/component/component.go

// Queue represents a message queue requirement
type Queue interface {
    Name() string
    Type() string
    MaxRetries() int
    DeadLetterQueue() bool
}

// Update Component interface
type Component interface {
    // ... existing methods
    Queues() []Queue
}
```

### Step 3: Add Schema Types for Each Version

Add the external schema type in the appropriate version package:

```go
// pkg/schema/component/v1/types.go

// QueueV1 represents a queue in the v1 schema
type QueueV1 struct {
    Type       string `yaml:"type"`
    MaxRetries int    `yaml:"maxRetries,omitempty"`
    DLQ        bool   `yaml:"dlq,omitempty"`
}

// Update SchemaV1
type SchemaV1 struct {
    // ... existing fields
    Queues map[string]QueueV1 `yaml:"queues,omitempty"`
}
```

### Step 4: Update the Transformer

Add transformation logic:

```go
// pkg/schema/component/v1/transformer.go

func (t *Transformer) Transform(v1 *SchemaV1) (*internal.InternalComponent, error) {
    ic := &internal.InternalComponent{
        // ... existing transformations
    }

    // Transform queues
    for name, q := range v1.Queues {
        iq, err := t.transformQueue(name, q)
        if err != nil {
            return nil, fmt.Errorf("queue %s: %w", name, err)
        }
        ic.Queues = append(ic.Queues, iq)
    }

    return ic, nil
}

func (t *Transformer) transformQueue(name string, q QueueV1) (internal.InternalQueue, error) {
    // Validate queue type
    validTypes := []string{"sqs", "rabbitmq", "kafka"}
    if !contains(validTypes, q.Type) {
        return internal.InternalQueue{}, fmt.Errorf(
            "invalid queue type %q, must be one of: %v", q.Type, validTypes,
        )
    }

    return internal.InternalQueue{
        Name:       name,
        Type:       q.Type,
        MaxRetries: q.MaxRetries,
        DLQ:        q.DLQ,
    }, nil
}
```

### Step 5: Add Validation Rules

Update the validator:

```go
// pkg/schema/component/v1/validator.go

func (v *Validator) validateQueues(queues map[string]QueueV1) []ValidationError {
    var errs []ValidationError

    for name, q := range queues {
        if q.Type == "" {
            errs = append(errs, ValidationError{
                Field:   fmt.Sprintf("queues.%s.type", name),
                Message: "type is required",
            })
        }

        if q.MaxRetries < 0 {
            errs = append(errs, ValidationError{
                Field:   fmt.Sprintf("queues.%s.maxRetries", name),
                Message: "maxRetries must be non-negative",
            })
        }
    }

    return errs
}
```

### Step 6: Add Expression Outputs

Update the expression evaluator to support queue references:

```go
// pkg/engine/expression/evaluator.go

// QueueOutputs contains outputs from a provisioned queue
type QueueOutputs struct {
    URL       string
    ARN       string
    Name      string
    Region    string
}

// Add to EvalContext
type EvalContext struct {
    // ... existing fields
    Queues map[string]QueueOutputs
}

func (e *Evaluator) evaluateReference(ref ReferenceSegment, ctx *EvalContext) (interface{}, error) {
    switch ref.Path[0] {
    // ... existing cases
    case "queues":
        return e.resolveQueue(ref.Path[1:], ctx.Queues)
    }
}

func (e *Evaluator) resolveQueue(path []string, queues map[string]QueueOutputs) (interface{}, error) {
    if len(path) < 2 {
        return nil, fmt.Errorf("invalid queue reference: need name and property")
    }

    name := path[0]
    prop := path[1]

    q, ok := queues[name]
    if !ok {
        return nil, fmt.Errorf("queue %q not found", name)
    }

    switch prop {
    case "url":
        return q.URL, nil
    case "arn":
        return q.ARN, nil
    case "name":
        return q.Name, nil
    case "region":
        return q.Region, nil
    default:
        return nil, fmt.Errorf("unknown queue property: %s", prop)
    }
}
```

### Step 7: Update the Execution Graph

Add queue nodes to the dependency graph:

```go
// pkg/engine/graph/resolver.go

func (r *Resolver) resolveComponent(comp component.Component) (*Graph, error) {
    g := NewGraph()

    // ... existing resource resolution

    // Add queue nodes
    for _, q := range comp.Queues() {
        node := &Node{
            ID:   fmt.Sprintf("queue/%s", q.Name()),
            Type: NodeTypeQueue,
            Inputs: map[string]interface{}{
                "name":       q.Name(),
                "queueType":  q.Type(),
                "maxRetries": q.MaxRetries(),
                "dlq":        q.DeadLetterQueue(),
            },
        }
        g.AddNode(node)
    }

    return g, nil
}
```

### Step 8: Add Tests

Create comprehensive tests:

```go
// pkg/schema/component/v1/parser_test.go

func TestParser_Parse_Queues(t *testing.T) {
    input := `
name: queue-test
queues:
  orders:
    type: sqs
    maxRetries: 3
    dlq: true
  notifications:
    type: rabbitmq
`

    p := NewParser()
    result, err := p.ParseBytes([]byte(input))

    require.NoError(t, err)
    assert.Len(t, result.Queues, 2)
    assert.Equal(t, "sqs", result.Queues["orders"].Type)
    assert.Equal(t, 3, result.Queues["orders"].MaxRetries)
    assert.True(t, result.Queues["orders"].DLQ)
}

func TestValidator_Queues_InvalidType(t *testing.T) {
    input := `
name: queue-test
queues:
  orders:
    type: invalid-type
`

    p := NewParser()
    _, err := p.ParseBytes([]byte(input))

    require.Error(t, err)
    assert.Contains(t, err.Error(), "invalid queue type")
}
```

### Step 9: Document the New Resource

Update the PRD and add examples:

```yaml
# Example in PRD or documentation
queues:
  orders:
    type: sqs # Required: sqs, rabbitmq, kafka
    maxRetries: 3 # Optional: max delivery attempts
    dlq: true # Optional: enable dead letter queue
```

---

## Adding New Datacenter Hooks

This section describes how to add a new hook type to the datacenter specification.

### Step 1: Define Hook Inputs and Outputs

Document the contract in `pkg/schema/datacenter/internal/types.go`:

```go
// pkg/schema/datacenter/internal/types.go

// QueueHookInputs defines inputs for the queue hook
type QueueHookInputs struct {
    Name       string `json:"name"`
    QueueType  string `json:"queueType"`
    MaxRetries int    `json:"maxRetries"`
    DLQ        bool   `json:"dlq"`
}

// QueueHookOutputs defines required outputs from the queue hook
type QueueHookOutputs struct {
    URL    string `json:"url"`
    ARN    string `json:"arn"`
    Name   string `json:"name"`
    Region string `json:"region"`
}
```

### Step 2: Add Hook to Datacenter Schema

```go
// pkg/schema/datacenter/v1/types.go

// EnvironmentBlockV1 contains environment-level configuration
type EnvironmentBlockV1 struct {
    Modules    []ModuleBlockV1          `hcl:"module,block"`
    Database   []HookBlockV1            `hcl:"database,block"`
    Deployment []HookBlockV1            `hcl:"deployment,block"`
    // Add new hook
    Queue      []HookBlockV1            `hcl:"queue,block"`
}
```

### Step 3: Update the Executor

Add hook execution logic:

```go
// pkg/engine/executor/hooks.go

func (e *Executor) executeQueueHook(
    ctx context.Context,
    env string,
    node *graph.Node,
    datacenter datacenter.Datacenter,
) (*QueueResult, error) {
    // Find matching hook
    hook := datacenter.FindQueueHook(node.Inputs)
    if hook == nil {
        return nil, fmt.Errorf("no queue hook matches inputs: %v", node.Inputs)
    }

    // Execute hook modules
    var outputs map[string]interface{}
    for _, mod := range hook.Modules() {
        result, err := e.executeModule(ctx, mod, node.Inputs)
        if err != nil {
            return nil, fmt.Errorf("module %s failed: %w", mod.Name(), err)
        }
        outputs = mergeOutputs(outputs, result.Outputs)
    }

    // Validate required outputs
    if err := validateQueueOutputs(outputs); err != nil {
        return nil, fmt.Errorf("hook outputs invalid: %w", err)
    }

    return &QueueResult{
        URL:    outputs["url"].(string),
        ARN:    outputs["arn"].(string),
        Name:   outputs["name"].(string),
        Region: outputs["region"].(string),
    }, nil
}

func validateQueueOutputs(outputs map[string]interface{}) error {
    required := []string{"url", "name"}
    for _, key := range required {
        if _, ok := outputs[key]; !ok {
            return fmt.Errorf("missing required output: %s", key)
        }
    }
    return nil
}
```

### Step 4: Add Example Hook Implementation

Provide an example in documentation:

```hcl
# Example queue hook for AWS SQS
environment {
  queue {
    when = node.inputs.queueType == "sqs"

    module "sqs_queue" {
      build = "./modules/sqs-queue"
      inputs = {
        name        = "${environment.name}-${node.component}--${node.name}"
        max_retries = node.inputs.maxRetries
        enable_dlq  = node.inputs.dlq
        region      = variable.region
      }
    }

    outputs = {
      url    = module.sqs_queue.queue_url
      arn    = module.sqs_queue.queue_arn
      name   = module.sqs_queue.queue_name
      region = variable.region
    }
  }
}
```

---

## Adding New State Backends

This section describes how to implement a new state backend.

### Step 1: Create Backend Package

Create a new package under `pkg/state/backend/`:

```go
// pkg/state/backend/consul/consul.go

package consul

import (
    "context"
    "fmt"
    "io"
    "path"

    "github.com/hashicorp/consul/api"
    "github.com/davidthor/cldctl/pkg/state/backend"
)

// Backend implements the state backend interface for Consul KV
type Backend struct {
    client *api.Client
    prefix string
}

// NewBackend creates a new Consul backend
func NewBackend(config map[string]string) (backend.Backend, error) {
    // Validate configuration
    address := config["address"]
    if address == "" {
        address = "localhost:8500"
    }

    // Create Consul client
    consulConfig := api.DefaultConfig()
    consulConfig.Address = address

    if token := config["token"]; token != "" {
        consulConfig.Token = token
    }

    client, err := api.NewClient(consulConfig)
    if err != nil {
        return nil, fmt.Errorf("failed to create Consul client: %w", err)
    }

    return &Backend{
        client: client,
        prefix: config["prefix"],
    }, nil
}

func (b *Backend) Type() string {
    return "consul"
}

func (b *Backend) Read(ctx context.Context, statePath string) (io.ReadCloser, error) {
    key := path.Join(b.prefix, statePath)

    pair, _, err := b.client.KV().Get(key, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to read from Consul: %w", err)
    }

    if pair == nil {
        return nil, backend.ErrNotFound
    }

    return io.NopCloser(bytes.NewReader(pair.Value)), nil
}

func (b *Backend) Write(ctx context.Context, statePath string, data io.Reader) error {
    key := path.Join(b.prefix, statePath)

    value, err := io.ReadAll(data)
    if err != nil {
        return fmt.Errorf("failed to read data: %w", err)
    }

    _, err = b.client.KV().Put(&api.KVPair{
        Key:   key,
        Value: value,
    }, nil)
    if err != nil {
        return fmt.Errorf("failed to write to Consul: %w", err)
    }

    return nil
}

func (b *Backend) Delete(ctx context.Context, statePath string) error {
    key := path.Join(b.prefix, statePath)

    _, err := b.client.KV().Delete(key, nil)
    if err != nil {
        return fmt.Errorf("failed to delete from Consul: %w", err)
    }

    return nil
}

func (b *Backend) List(ctx context.Context, prefix string) ([]string, error) {
    key := path.Join(b.prefix, prefix)

    pairs, _, err := b.client.KV().List(key, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to list from Consul: %w", err)
    }

    var paths []string
    for _, pair := range pairs {
        // Strip the backend prefix to return relative paths
        relPath := strings.TrimPrefix(pair.Key, b.prefix+"/")
        paths = append(paths, relPath)
    }

    return paths, nil
}

func (b *Backend) Exists(ctx context.Context, statePath string) (bool, error) {
    key := path.Join(b.prefix, statePath)

    pair, _, err := b.client.KV().Get(key, nil)
    if err != nil {
        return false, fmt.Errorf("failed to check existence in Consul: %w", err)
    }

    return pair != nil, nil
}

func (b *Backend) Lock(ctx context.Context, statePath string, info backend.LockInfo) (backend.Lock, error) {
    key := path.Join(b.prefix, statePath, ".lock")

    lock, err := b.client.LockOpts(&api.LockOptions{
        Key:   key,
        Value: encodeLockInfo(info),
    })
    if err != nil {
        return nil, fmt.Errorf("failed to create lock: %w", err)
    }

    _, err = lock.Lock(nil)
    if err != nil {
        return nil, fmt.Errorf("failed to acquire lock: %w", err)
    }

    return &consulLock{
        lock: lock,
        info: info,
    }, nil
}

type consulLock struct {
    lock *api.Lock
    info backend.LockInfo
}

func (l *consulLock) ID() string {
    return l.info.ID
}

func (l *consulLock) Unlock(ctx context.Context) error {
    return l.lock.Unlock()
}

func (l *consulLock) Info() backend.LockInfo {
    return l.info
}
```

### Step 2: Register the Backend

Add to the registry initialization:

```go
// pkg/state/backend/registry.go

import (
    "github.com/davidthor/cldctl/pkg/state/backend/consul"
)

func init() {
    DefaultRegistry.Register("local", local.NewBackend)
    DefaultRegistry.Register("s3", s3.NewBackend)
    DefaultRegistry.Register("gcs", gcs.NewBackend)
    DefaultRegistry.Register("azurerm", azurerm.NewBackend)
    DefaultRegistry.Register("consul", consul.NewBackend)  // Add new backend
}
```

### Step 3: Add Environment Variable Support

Document the environment variable pattern:

```go
// Environment variables for Consul backend:
// CLDCTL_BACKEND=consul
// CLDCTL_BACKEND_CONSUL_ADDRESS=localhost:8500
// CLDCTL_BACKEND_CONSUL_TOKEN=<token>
// CLDCTL_BACKEND_CONSUL_PREFIX=cldctl/state
```

### Step 4: Write Tests

```go
// pkg/state/backend/consul/consul_test.go

func TestBackend_ReadWrite(t *testing.T) {
    // Skip if no Consul available
    client, err := api.NewClient(api.DefaultConfig())
    if err != nil {
        t.Skip("Consul not available")
    }

    backend, err := NewBackend(map[string]string{
        "prefix": "cldctl-test",
    })
    require.NoError(t, err)

    ctx := context.Background()
    testData := []byte(`{"test": "data"}`)

    // Write
    err = backend.Write(ctx, "test/state.json", bytes.NewReader(testData))
    require.NoError(t, err)

    // Read
    reader, err := backend.Read(ctx, "test/state.json")
    require.NoError(t, err)
    defer reader.Close()

    data, err := io.ReadAll(reader)
    require.NoError(t, err)
    assert.Equal(t, testData, data)

    // Cleanup
    backend.Delete(ctx, "test/state.json")
}
```

---

## Adding New IaC Plugins

This section describes how to implement a new IaC plugin (e.g., for AWS CDK).

### Step 1: Create Plugin Package

```go
// pkg/iac/awscdk/awscdk.go

package awscdk

import (
    "context"
    "encoding/json"
    "fmt"
    "os/exec"

    "github.com/davidthor/cldctl/pkg/iac"
)

// Plugin implements the IaC plugin interface for AWS CDK
type Plugin struct {
    language string // typescript, python, go, java, csharp
}

func NewPlugin() *Plugin {
    return &Plugin{
        language: "typescript",
    }
}

func (p *Plugin) Name() string {
    return "awscdk"
}

func (p *Plugin) Preview(ctx context.Context, opts iac.RunOptions) (*iac.PreviewResult, error) {
    workDir, cleanup, err := p.prepareModule(ctx, opts)
    if err != nil {
        return nil, err
    }
    defer cleanup()

    // Write inputs as CDK context
    if err := p.writeContext(workDir, opts.Inputs); err != nil {
        return nil, err
    }

    // Run cdk diff
    cmd := exec.CommandContext(ctx, "cdk", "diff", "--json")
    cmd.Dir = workDir
    cmd.Env = p.buildEnvironment(opts.Environment)

    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("cdk diff failed: %w", err)
    }

    return p.parsePreviewOutput(output)
}

func (p *Plugin) Apply(ctx context.Context, opts iac.RunOptions) (*iac.ApplyResult, error) {
    workDir, cleanup, err := p.prepareModule(ctx, opts)
    if err != nil {
        return nil, err
    }
    defer cleanup()

    // Write inputs as CDK context
    if err := p.writeContext(workDir, opts.Inputs); err != nil {
        return nil, err
    }

    // Import existing state if provided
    if opts.StateReader != nil {
        if err := p.importState(ctx, workDir, opts.StateReader); err != nil {
            return nil, err
        }
    }

    // Run cdk deploy
    cmd := exec.CommandContext(ctx, "cdk", "deploy", "--require-approval", "never", "--outputs-file", "outputs.json")
    cmd.Dir = workDir
    cmd.Env = p.buildEnvironment(opts.Environment)
    cmd.Stdout = opts.Stdout
    cmd.Stderr = opts.Stderr

    if err := cmd.Run(); err != nil {
        return nil, fmt.Errorf("cdk deploy failed: %w", err)
    }

    // Read outputs
    outputs, err := p.readOutputs(workDir)
    if err != nil {
        return nil, err
    }

    // Export state
    state, err := p.exportState(ctx, workDir)
    if err != nil {
        return nil, err
    }

    return &iac.ApplyResult{
        Outputs: outputs,
        State:   state,
    }, nil
}

func (p *Plugin) Destroy(ctx context.Context, opts iac.RunOptions) error {
    workDir, cleanup, err := p.prepareModule(ctx, opts)
    if err != nil {
        return err
    }
    defer cleanup()

    // Import state
    if opts.StateReader != nil {
        if err := p.importState(ctx, workDir, opts.StateReader); err != nil {
            return err
        }
    }

    // Run cdk destroy
    cmd := exec.CommandContext(ctx, "cdk", "destroy", "--force")
    cmd.Dir = workDir
    cmd.Env = p.buildEnvironment(opts.Environment)
    cmd.Stdout = opts.Stdout
    cmd.Stderr = opts.Stderr

    return cmd.Run()
}

func (p *Plugin) Refresh(ctx context.Context, opts iac.RunOptions) (*iac.RefreshResult, error) {
    // CDK doesn't have a direct refresh command
    // We can synthesize and compare with CloudFormation state
    // Implementation depends on specific requirements
    return nil, fmt.Errorf("refresh not implemented for AWS CDK")
}

func (p *Plugin) prepareModule(ctx context.Context, opts iac.RunOptions) (string, func(), error) {
    // If module source is an OCI reference, pull it
    // Set up working directory with CDK project structure
    // Return working directory and cleanup function
    // ...
}

func (p *Plugin) writeContext(workDir string, inputs map[string]interface{}) error {
    // Write inputs to cdk.context.json
    contextFile := filepath.Join(workDir, "cdk.context.json")
    data, err := json.MarshalIndent(inputs, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(contextFile, data, 0644)
}
```

### Step 2: Register the Plugin

```go
// pkg/iac/registry.go

import (
    "github.com/davidthor/cldctl/pkg/iac/awscdk"
)

func init() {
    DefaultRegistry.Register("pulumi", pulumi.NewPlugin)
    DefaultRegistry.Register("opentofu", opentofu.NewPlugin)
    DefaultRegistry.Register("awscdk", awscdk.NewPlugin)  // Add new plugin
}
```

### Step 3: Update Datacenter Schema

Allow the new plugin in module definitions:

```go
// pkg/schema/datacenter/v1/validator.go

var validPlugins = []string{"pulumi", "opentofu", "awscdk"}

func (v *Validator) validateModule(mod ModuleBlockV1) []ValidationError {
    var errs []ValidationError

    if mod.Plugin != "" && !contains(validPlugins, mod.Plugin) {
        errs = append(errs, ValidationError{
            Field:   "plugin",
            Message: fmt.Sprintf("invalid plugin %q, must be one of: %v", mod.Plugin, validPlugins),
        })
    }

    return errs
}
```

---

## Testing Guidelines

### Unit Tests

- Test each function in isolation
- Use table-driven tests for multiple cases
- Mock external dependencies

```go
func TestParser_Parse(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    *Result
        wantErr bool
    }{
        {
            name:  "valid input",
            input: "...",
            want:  &Result{...},
        },
        {
            name:    "invalid input",
            input:   "...",
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

### Integration Tests

- Use build tags to separate from unit tests
- Require real infrastructure (Docker, cloud services)
- Clean up resources after tests

```go
//go:build integration

func TestBackend_Integration(t *testing.T) {
    // Test with real backend
}
```

### Test Coverage

- Aim for 80%+ coverage on core packages
- Focus on critical paths and error handling
- Don't test trivial getters/setters

---

## Documentation Requirements

### Code Documentation

- Document all exported types, functions, and methods
- Include examples in doc comments where helpful

```go
// Backend defines the interface for state storage backends.
// Implementations must be safe for concurrent use.
//
// Example usage:
//
//     backend, err := s3.NewBackend(map[string]string{
//         "bucket": "my-state-bucket",
//         "region": "us-east-1",
//     })
//     if err != nil {
//         return err
//     }
//
//     data, err := backend.Read(ctx, "path/to/state.json")
type Backend interface {
    // ...
}
```

### User Documentation

When adding new features, update:

1. **PRD.md** - Add specification details
2. **README.md** - Update feature list
3. **Examples** - Add example configurations in `testdata/`

### Changelog

Add entries to CHANGELOG.md following [Keep a Changelog](https://keepachangelog.com/):

```markdown
## [Unreleased]

### Added

- Queue resource type for message queue support (#123)
- Consul state backend (#124)
- AWS CDK plugin (#125)

### Changed

- Improved error messages for validation failures

### Fixed

- State locking race condition on S3 backend
```

---

## Pull Request Process

1. **Create a feature branch** from `main`
2. **Make changes** following the guidelines above
3. **Write tests** for new functionality
4. **Update documentation** as needed
5. **Run checks locally**:
   ```bash
   make lint
   make test
   make build
   ```
6. **Create PR** with clear description
7. **Address review feedback**
8. **Squash and merge** when approved

### PR Title Convention

Use conventional commit format:

- `feat: add queue resource type`
- `fix: resolve state locking race condition`
- `docs: update contributing guide`
- `refactor: simplify expression parser`
- `test: add integration tests for S3 backend`
