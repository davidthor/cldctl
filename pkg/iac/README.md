# iac

Infrastructure-as-Code plugin framework for cldctl. Provides a unified interface for different IaC tools including native Docker execution, OpenTofu/Terraform, and Pulumi.

## Overview

The `iac` package provides:

- A common `Plugin` interface for IaC frameworks
- A registry system for managing plugin factories
- Built-in plugins for native execution, OpenTofu/Terraform, and Pulumi

## Package Structure

```
iac/
├── plugin.go       # Plugin interface and types
├── registry.go     # Plugin registry
├── native/         # Native Docker/exec plugin
├── opentofu/       # OpenTofu/Terraform plugin
└── pulumi/         # Pulumi plugin
```

## Plugin Interface

All IaC plugins implement a common interface:

```go
type Plugin interface {
    Name() string
    Preview(ctx context.Context, opts RunOptions) (*PreviewResult, error)
    Apply(ctx context.Context, opts RunOptions) (*ApplyResult, error)
    Destroy(ctx context.Context, opts RunOptions) (*ApplyResult, error)
    Refresh(ctx context.Context, opts RunOptions) (*RefreshResult, error)
}
```

## Types

### RunOptions

Configuration for plugin execution.

```go
type RunOptions struct {
    WorkDir       string
    Inputs        map[string]interface{}
    State         []byte
    Volumes       []VolumeMount
    Output        *output.Stream
    AutoApprove   bool
}
```

### Result Types

```go
type PreviewResult struct {
    Changes []ResourceChange
    Summary ChangeSummary
}

type ApplyResult struct {
    Success bool
    Outputs map[string]OutputValue
    State   []byte
}

type RefreshResult struct {
    Drift []ResourceDrift
    State []byte
}
```

### Change Actions

```go
const (
    ActionCreate  ChangeAction = "create"
    ActionUpdate  ChangeAction = "update"
    ActionDelete  ChangeAction = "delete"
    ActionReplace ChangeAction = "replace"
    ActionNoop    ChangeAction = "noop"
)
```

## Registry

The registry manages plugin factories and provides plugin instances.

```go
// Register a plugin factory
iac.Register("myplugin", func() (iac.Plugin, error) {
    return NewMyPlugin()
})

// Get a plugin instance
plugin, err := iac.Get("opentofu")

// List available plugins
plugins := iac.DefaultRegistry.List()
```

## Subpackages

### native

Native IaC plugin for Docker and process execution. Executes Docker containers, networks, volumes, and host commands directly without external IaC tools.

```go
import "github.com/davidthor/cldctl/pkg/iac/native"

// Create a native plugin
plugin, err := native.NewPlugin()

// Load a native module definition
module, err := native.LoadModule("./module.yml")

// Direct Docker client usage
docker, err := native.NewDockerClient()
err = docker.RunContainer(ctx, native.ContainerOptions{
    Name:  "my-container",
    Image: "nginx:latest",
    Ports: []native.PortMapping{{Host: 8080, Container: 80}},
})
```

**Supported Resource Types:**

- `docker:container` - Docker containers
- `docker:network` - Docker networks
- `docker:volume` - Docker volumes
- `exec` - One-time command execution

**Docker Client Methods:**

- `RunContainer()`, `InspectContainer()`, `IsContainerRunning()`, `RemoveContainer()`
- `CreateNetwork()`, `NetworkExists()`, `RemoveNetwork()`
- `CreateVolume()`, `VolumeExists()`, `RemoveVolume()`
- `Exec()` - Execute a command on the host
- `BuildImage()`, `PushImage()`, `TagImage()`, `RemoveImage()`

**Expression Support:**

```yaml
resources:
  - name: api
    type: docker:container
    properties:
      image: ${inputs.image}
      env:
        DATABASE_URL: ${resources.db.outputs.url}
```

### opentofu

IaC plugin for OpenTofu/Terraform. Wraps the `tofu` or `terraform` binary.

```go
import "github.com/davidthor/cldctl/pkg/iac/opentofu"

// Create a plugin (auto-detects tofu or terraform binary)
plugin, err := opentofu.NewPlugin("tofu")  // or "terraform"
```

**Features:**

- Auto-detects `tofu` or `terraform` binary
- Registers as both "opentofu" and "terraform" plugins
- Writes `terraform.tfvars.json` from inputs
- Handles initialization automatically
- Parses JSON plan output for preview
- Reads state from `terraform.tfstate`

### pulumi

IaC plugin for Pulumi. Wraps the `pulumi` binary.

```go
import "github.com/davidthor/cldctl/pkg/iac/pulumi"

// Create a Pulumi plugin
plugin, err := pulumi.NewPlugin()
```

**Features:**

- Stack management (auto-creates/selects stacks)
- Writes `Pulumi.<stack>.yaml` config files from inputs
- Stack name resolution from environment variables
- Parses JSON preview output
- Exports state via `pulumi stack export`
- Uses local backend by default

## Usage Example

```go
import (
    "github.com/davidthor/cldctl/pkg/iac"
    "github.com/davidthor/cldctl/pkg/output"
)

// Get a plugin
plugin, err := iac.Get("opentofu")
if err != nil {
    log.Fatal(err)
}

// Create output stream
stream := output.NewStream()
stream.AddHandler(output.NewConsoleHandler(output.ConsoleOptions{
    UseColors: true,
}))

// Preview changes
preview, err := plugin.Preview(ctx, iac.RunOptions{
    WorkDir: "./infrastructure",
    Inputs: map[string]interface{}{
        "region":   "us-west-2",
        "replicas": 3,
    },
    Output: stream,
})
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Changes: +%d ~%d -%d\n",
    preview.Summary.ToCreate,
    preview.Summary.ToUpdate,
    preview.Summary.ToDelete)

// Apply changes
result, err := plugin.Apply(ctx, iac.RunOptions{
    WorkDir:     "./infrastructure",
    Inputs:      inputs,
    Output:      stream,
    AutoApprove: true,
})
if err != nil {
    log.Fatal(err)
}

// Access outputs
for name, output := range result.Outputs {
    fmt.Printf("%s = %v\n", name, output.Value)
}
```

## Plugin Registration

All built-in plugins register themselves via `init()` functions:

```go
func init() {
    iac.Register("native", func() (iac.Plugin, error) {
        return native.NewPlugin()
    })
    iac.Register("opentofu", func() (iac.Plugin, error) {
        return opentofu.NewPlugin("tofu")
    })
    iac.Register("terraform", func() (iac.Plugin, error) {
        return opentofu.NewPlugin("terraform")
    })
    iac.Register("pulumi", func() (iac.Plugin, error) {
        return pulumi.NewPlugin()
    })
}
```

## Creating Custom Plugins

Implement the `Plugin` interface:

```go
type MyPlugin struct{}

func (p *MyPlugin) Name() string {
    return "myplugin"
}

func (p *MyPlugin) Preview(ctx context.Context, opts iac.RunOptions) (*iac.PreviewResult, error) {
    // Implement preview logic
}

func (p *MyPlugin) Apply(ctx context.Context, opts iac.RunOptions) (*iac.ApplyResult, error) {
    // Implement apply logic
}

func (p *MyPlugin) Destroy(ctx context.Context, opts iac.RunOptions) (*iac.ApplyResult, error) {
    // Implement destroy logic
}

func (p *MyPlugin) Refresh(ctx context.Context, opts iac.RunOptions) (*iac.RefreshResult, error) {
    // Implement refresh logic
}

// Register the plugin
func init() {
    iac.Register("myplugin", func() (iac.Plugin, error) {
        return &MyPlugin{}, nil
    })
}
```
