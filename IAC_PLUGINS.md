# IaC Plugin Implementation Guide

This document provides detailed guidance on implementing new Infrastructure-as-Code (IaC) plugins for cldctl.

## Overview

IaC plugins enable cldctl to execute infrastructure modules written in different frameworks. Each plugin handles the translation between cldctl's execution model and the specific IaC tool's CLI and state format.

## Plugin Interface

Every IaC plugin must implement the `Plugin` interface:

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

    // Preview generates a preview of changes without applying
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
    // ModuleSource is the OCI image reference or local path to the module
    ModuleSource string

    // Inputs are the values passed to the module
    Inputs map[string]interface{}

    // StateReader provides existing state (nil for new deployments)
    StateReader io.Reader

    // StateWriter receives the updated state after apply
    StateWriter io.Writer

    // WorkDir is the working directory for execution
    WorkDir string

    // Environment contains environment variables for the execution
    Environment map[string]string

    // Volumes are volume mounts needed by the module (e.g., Docker socket)
    Volumes []VolumeMount

    // Stdout/Stderr for command output
    Stdout io.Writer
    Stderr io.Writer
}

// VolumeMount defines a volume mount for module execution
type VolumeMount struct {
    HostPath  string
    MountPath string
    ReadOnly  bool
}

// PreviewResult contains the result of a preview operation
type PreviewResult struct {
    Changes []ResourceChange
    Summary ChangeSummary
}

// ResourceChange describes a planned change to a resource
type ResourceChange struct {
    ResourceID   string
    ResourceType string
    Action       ChangeAction   // Create, Update, Delete, Replace
    Before       interface{}    // Current state (nil for create)
    After        interface{}    // Planned state (nil for delete)
    Diff         []PropertyDiff // Property-level changes
}

// ChangeAction indicates the type of change
type ChangeAction string

const (
    ActionCreate  ChangeAction = "create"
    ActionUpdate  ChangeAction = "update"
    ActionDelete  ChangeAction = "delete"
    ActionReplace ChangeAction = "replace"
    ActionNoop    ChangeAction = "noop"
)

// ChangeSummary summarizes planned changes
type ChangeSummary struct {
    Create  int
    Update  int
    Delete  int
    Replace int
}

// ApplyResult contains the result of an apply operation
type ApplyResult struct {
    Outputs map[string]OutputValue
    State   []byte // Serialized state for persistence
}

// OutputValue represents a module output
type OutputValue struct {
    Value     interface{}
    Sensitive bool
}

// RefreshResult contains the result of a refresh operation
type RefreshResult struct {
    State   []byte
    Drifts  []ResourceDrift
}

// ResourceDrift describes drift between state and actual infrastructure
type ResourceDrift struct {
    ResourceID   string
    ResourceType string
    Diffs        []PropertyDiff
}
```

## Implementing a New Plugin

### Step 1: Create the Plugin Package

Create a new package under `pkg/iac/`:

```go
// pkg/iac/myiac/myiac.go

package myiac

import (
    "context"
    "fmt"

    "github.com/davidthor/cldctl/pkg/iac"
)

// Plugin implements the IaC plugin interface for MyIaC
type Plugin struct {
    // Plugin-specific configuration
    cliPath string
}

// NewPlugin creates a new MyIaC plugin instance
func NewPlugin() *Plugin {
    return &Plugin{
        cliPath: "myiac", // Default CLI path
    }
}

func (p *Plugin) Name() string {
    return "myiac"
}
```

### Step 2: Implement Module Preparation

Modules come either from OCI images or local paths. The plugin must prepare a working environment:

```go
// pkg/iac/myiac/prepare.go

package myiac

import (
    "context"
    "os"
    "path/filepath"

    "github.com/davidthor/cldctl/pkg/oci"
)

// prepareModule sets up the module for execution
// Returns working directory and cleanup function
func (p *Plugin) prepareModule(ctx context.Context, opts iac.RunOptions) (string, func(), error) {
    if isOCIReference(opts.ModuleSource) {
        return p.prepareFromOCI(ctx, opts.ModuleSource)
    }
    return p.prepareFromLocal(ctx, opts.ModuleSource)
}

func (p *Plugin) prepareFromOCI(ctx context.Context, ref string) (string, func(), error) {
    // Create temp directory
    workDir, err := os.MkdirTemp("", "cldctl-myiac-*")
    if err != nil {
        return "", nil, fmt.Errorf("failed to create temp dir: %w", err)
    }

    cleanup := func() {
        os.RemoveAll(workDir)
    }

    // Pull OCI artifact
    client := oci.NewClient()
    if err := client.Pull(ctx, ref, workDir); err != nil {
        cleanup()
        return "", nil, fmt.Errorf("failed to pull module %s: %w", ref, err)
    }

    return workDir, cleanup, nil
}

func (p *Plugin) prepareFromLocal(ctx context.Context, path string) (string, func(), error) {
    absPath, err := filepath.Abs(path)
    if err != nil {
        return "", nil, fmt.Errorf("failed to resolve path: %w", err)
    }

    // Verify directory exists
    info, err := os.Stat(absPath)
    if err != nil {
        return "", nil, fmt.Errorf("module path not found: %w", err)
    }
    if !info.IsDir() {
        return "", nil, fmt.Errorf("module path is not a directory: %s", absPath)
    }

    // No cleanup needed for local paths
    return absPath, func() {}, nil
}
```

### Step 3: Implement Preview

```go
// pkg/iac/myiac/preview.go

package myiac

import (
    "bytes"
    "context"
    "encoding/json"
    "os/exec"

    "github.com/davidthor/cldctl/pkg/iac"
)

func (p *Plugin) Preview(ctx context.Context, opts iac.RunOptions) (*iac.PreviewResult, error) {
    workDir, cleanup, err := p.prepareModule(ctx, opts)
    if err != nil {
        return nil, err
    }
    defer cleanup()

    // Write inputs
    if err := p.writeInputs(workDir, opts.Inputs); err != nil {
        return nil, fmt.Errorf("failed to write inputs: %w", err)
    }

    // Import existing state if provided
    if opts.StateReader != nil {
        if err := p.importState(ctx, workDir, opts.StateReader); err != nil {
            return nil, fmt.Errorf("failed to import state: %w", err)
        }
    }

    // Run preview command
    var stdout, stderr bytes.Buffer
    cmd := exec.CommandContext(ctx, p.cliPath, "preview", "--json")
    cmd.Dir = workDir
    cmd.Env = p.buildEnvironment(opts.Environment)
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    if err := cmd.Run(); err != nil {
        return nil, fmt.Errorf("preview failed: %s\n%s", err, stderr.String())
    }

    // Parse preview output
    return p.parsePreviewOutput(stdout.Bytes())
}

func (p *Plugin) parsePreviewOutput(data []byte) (*iac.PreviewResult, error) {
    // Parse the IaC tool's JSON output format
    var raw struct {
        Changes []struct {
            ID     string      `json:"id"`
            Type   string      `json:"type"`
            Action string      `json:"action"`
            Before interface{} `json:"before"`
            After  interface{} `json:"after"`
        } `json:"changes"`
    }

    if err := json.Unmarshal(data, &raw); err != nil {
        return nil, fmt.Errorf("failed to parse preview output: %w", err)
    }

    result := &iac.PreviewResult{
        Changes: make([]iac.ResourceChange, len(raw.Changes)),
    }

    for i, c := range raw.Changes {
        action := p.mapAction(c.Action)
        result.Changes[i] = iac.ResourceChange{
            ResourceID:   c.ID,
            ResourceType: c.Type,
            Action:       action,
            Before:       c.Before,
            After:        c.After,
        }

        switch action {
        case iac.ActionCreate:
            result.Summary.Create++
        case iac.ActionUpdate:
            result.Summary.Update++
        case iac.ActionDelete:
            result.Summary.Delete++
        case iac.ActionReplace:
            result.Summary.Replace++
        }
    }

    return result, nil
}
```

### Step 4: Implement Apply

```go
// pkg/iac/myiac/apply.go

package myiac

import (
    "bytes"
    "context"
    "encoding/json"
    "io"
    "os"
    "os/exec"
    "path/filepath"

    "github.com/davidthor/cldctl/pkg/iac"
)

func (p *Plugin) Apply(ctx context.Context, opts iac.RunOptions) (*iac.ApplyResult, error) {
    workDir, cleanup, err := p.prepareModule(ctx, opts)
    if err != nil {
        return nil, err
    }
    defer cleanup()

    // Write inputs
    if err := p.writeInputs(workDir, opts.Inputs); err != nil {
        return nil, fmt.Errorf("failed to write inputs: %w", err)
    }

    // Import existing state
    if opts.StateReader != nil {
        if err := p.importState(ctx, workDir, opts.StateReader); err != nil {
            return nil, fmt.Errorf("failed to import state: %w", err)
        }
    }

    // Run apply command
    cmd := exec.CommandContext(ctx, p.cliPath, "apply", "--auto-approve")
    cmd.Dir = workDir
    cmd.Env = p.buildEnvironment(opts.Environment)
    cmd.Stdout = opts.Stdout
    cmd.Stderr = opts.Stderr

    if err := cmd.Run(); err != nil {
        return nil, fmt.Errorf("apply failed: %w", err)
    }

    // Get outputs
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

func (p *Plugin) writeInputs(workDir string, inputs map[string]interface{}) error {
    // Write inputs in the format expected by the IaC tool
    // This varies by tool (e.g., terraform.tfvars, pulumi config, CDK context)

    inputFile := filepath.Join(workDir, "inputs.json")
    data, err := json.MarshalIndent(inputs, "", "  ")
    if err != nil {
        return err
    }

    return os.WriteFile(inputFile, data, 0644)
}

func (p *Plugin) getOutputs(ctx context.Context, workDir string) (map[string]iac.OutputValue, error) {
    var stdout bytes.Buffer
    cmd := exec.CommandContext(ctx, p.cliPath, "output", "--json")
    cmd.Dir = workDir
    cmd.Stdout = &stdout

    if err := cmd.Run(); err != nil {
        return nil, err
    }

    var raw map[string]struct {
        Value     interface{} `json:"value"`
        Sensitive bool        `json:"sensitive"`
    }

    if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
        return nil, err
    }

    outputs := make(map[string]iac.OutputValue)
    for name, o := range raw {
        outputs[name] = iac.OutputValue{
            Value:     o.Value,
            Sensitive: o.Sensitive,
        }
    }

    return outputs, nil
}

func (p *Plugin) importState(ctx context.Context, workDir string, reader io.Reader) error {
    // Write state to the location expected by the IaC tool
    stateFile := filepath.Join(workDir, "state.json") // Adjust for tool

    data, err := io.ReadAll(reader)
    if err != nil {
        return err
    }

    return os.WriteFile(stateFile, data, 0644)
}

func (p *Plugin) exportState(ctx context.Context, workDir string) ([]byte, error) {
    // Read state from the IaC tool's state location
    stateFile := filepath.Join(workDir, "state.json") // Adjust for tool
    return os.ReadFile(stateFile)
}
```

### Step 5: Implement Destroy

```go
// pkg/iac/myiac/destroy.go

package myiac

import (
    "context"
    "os/exec"

    "github.com/davidthor/cldctl/pkg/iac"
)

func (p *Plugin) Destroy(ctx context.Context, opts iac.RunOptions) error {
    workDir, cleanup, err := p.prepareModule(ctx, opts)
    if err != nil {
        return err
    }
    defer cleanup()

    // Import existing state
    if opts.StateReader != nil {
        if err := p.importState(ctx, workDir, opts.StateReader); err != nil {
            return fmt.Errorf("failed to import state: %w", err)
        }
    } else {
        return fmt.Errorf("state required for destroy operation")
    }

    // Run destroy command
    cmd := exec.CommandContext(ctx, p.cliPath, "destroy", "--auto-approve")
    cmd.Dir = workDir
    cmd.Env = p.buildEnvironment(opts.Environment)
    cmd.Stdout = opts.Stdout
    cmd.Stderr = opts.Stderr

    if err := cmd.Run(); err != nil {
        return fmt.Errorf("destroy failed: %w", err)
    }

    return nil
}
```

### Step 6: Implement Refresh

```go
// pkg/iac/myiac/refresh.go

package myiac

import (
    "context"
    "os/exec"

    "github.com/davidthor/cldctl/pkg/iac"
)

func (p *Plugin) Refresh(ctx context.Context, opts iac.RunOptions) (*iac.RefreshResult, error) {
    workDir, cleanup, err := p.prepareModule(ctx, opts)
    if err != nil {
        return nil, err
    }
    defer cleanup()

    // Import existing state
    if opts.StateReader != nil {
        if err := p.importState(ctx, workDir, opts.StateReader); err != nil {
            return nil, fmt.Errorf("failed to import state: %w", err)
        }
    }

    // Run refresh command
    cmd := exec.CommandContext(ctx, p.cliPath, "refresh")
    cmd.Dir = workDir
    cmd.Env = p.buildEnvironment(opts.Environment)
    cmd.Stdout = opts.Stdout
    cmd.Stderr = opts.Stderr

    if err := cmd.Run(); err != nil {
        return nil, fmt.Errorf("refresh failed: %w", err)
    }

    // Export refreshed state
    state, err := p.exportState(ctx, workDir)
    if err != nil {
        return nil, err
    }

    // Compare states to detect drift (optional)
    // ...

    return &iac.RefreshResult{
        State: state,
    }, nil
}
```

### Step 7: Register the Plugin

```go
// pkg/iac/registry.go

import (
    "github.com/davidthor/cldctl/pkg/iac/myiac"
)

func init() {
    DefaultRegistry.Register("pulumi", pulumi.NewPlugin)
    DefaultRegistry.Register("opentofu", opentofu.NewPlugin)
    DefaultRegistry.Register("myiac", myiac.NewPlugin)  // Add new plugin
}
```

### Step 8: Update Datacenter Schema

Allow the plugin in module definitions:

```go
// pkg/schema/datacenter/v1/validator.go

var validPlugins = []string{"pulumi", "opentofu", "myiac"}
```

## Plugin Configuration

### Environment Variables

Plugins receive environment variables through `RunOptions.Environment`. Handle tool-specific configuration:

```go
func (p *Plugin) buildEnvironment(extra map[string]string) []string {
    env := os.Environ()

    // Add plugin-specific variables
    env = append(env, "MYIAC_NON_INTERACTIVE=true")

    // Add user-provided variables
    for k, v := range extra {
        env = append(env, fmt.Sprintf("%s=%s", k, v))
    }

    return env
}
```

### Volume Mounts

Some modules need access to host resources (e.g., Docker socket for building images):

```go
func (p *Plugin) Apply(ctx context.Context, opts iac.RunOptions) (*iac.ApplyResult, error) {
    // Handle volume mounts if running in container
    if p.runInContainer {
        return p.applyInContainer(ctx, opts)
    }
    return p.applyDirect(ctx, opts)
}

func (p *Plugin) applyInContainer(ctx context.Context, opts iac.RunOptions) (*iac.ApplyResult, error) {
    args := []string{
        "run", "--rm",
        "-v", fmt.Sprintf("%s:/workspace", opts.WorkDir),
    }

    // Add volume mounts
    for _, v := range opts.Volumes {
        mode := "rw"
        if v.ReadOnly {
            mode = "ro"
        }
        args = append(args, "-v", fmt.Sprintf("%s:%s:%s", v.HostPath, v.MountPath, mode))
    }

    args = append(args, p.containerImage, "apply", "--auto-approve")

    cmd := exec.CommandContext(ctx, "docker", args...)
    // ...
}
```

## State Management

### State Format

Each IaC tool has its own state format. The plugin is responsible for:

1. **Importing state**: Converting cldctl's stored state to the tool's format
2. **Exporting state**: Converting the tool's state for cldctl storage
3. **State versioning**: Handling state format changes across tool versions

```go
// Pulumi uses JSON state files with a specific structure
type PulumiState struct {
    Version int                    `json:"version"`
    Stack   string                 `json:"stack"`
    Outputs map[string]interface{} `json:"outputs"`
    Resources []PulumiResource     `json:"resources"`
}

// OpenTofu/Terraform uses a different JSON structure
type TofuState struct {
    Version          int            `json:"version"`
    TerraformVersion string         `json:"terraform_version"`
    Serial           int64          `json:"serial"`
    Outputs          map[string]TofuOutput `json:"outputs"`
    Resources        []TofuResource `json:"resources"`
}
```

### State Locking

cldctl handles state locking at the backend level, but plugins should handle in-process locking:

```go
func (p *Plugin) Apply(ctx context.Context, opts iac.RunOptions) (*iac.ApplyResult, error) {
    // The state backend already holds a lock
    // But ensure we don't run concurrent applies in-process

    p.mu.Lock()
    defer p.mu.Unlock()

    // ...
}
```

## Error Handling

### Structured Errors

Return detailed errors for debugging:

```go
type ApplyError struct {
    Phase      string // "init", "plan", "apply"
    Resource   string // Resource that failed (if known)
    Message    string
    Stdout     string
    Stderr     string
    ExitCode   int
}

func (e *ApplyError) Error() string {
    return fmt.Sprintf("apply failed during %s: %s", e.Phase, e.Message)
}

func (p *Plugin) Apply(ctx context.Context, opts iac.RunOptions) (*iac.ApplyResult, error) {
    // ...

    if err := cmd.Run(); err != nil {
        exitCode := 1
        if exitErr, ok := err.(*exec.ExitError); ok {
            exitCode = exitErr.ExitCode()
        }

        return nil, &ApplyError{
            Phase:    "apply",
            Message:  err.Error(),
            Stderr:   stderr.String(),
            ExitCode: exitCode,
        }
    }

    // ...
}
```

### Partial Failures

Handle cases where apply partially succeeds:

```go
func (p *Plugin) Apply(ctx context.Context, opts iac.RunOptions) (*iac.ApplyResult, error) {
    // ...

    err := cmd.Run()

    // Even on error, try to export state (may have partial changes)
    state, stateErr := p.exportState(ctx, workDir)

    if err != nil {
        // Return both the error and any state that was created
        return &iac.ApplyResult{
            State:        state, // May be partial
            PartialError: err,
        }, nil
    }

    // ...
}
```

## Testing

### Unit Tests

Mock CLI execution:

```go
func TestPlugin_Apply(t *testing.T) {
    // Create mock CLI
    mockCLI := createMockCLI(t, map[string]mockResponse{
        "apply": {
            stdout: `{"outputs": {"url": {"value": "https://example.com"}}}`,
            exit:   0,
        },
    })

    p := &Plugin{cliPath: mockCLI}

    result, err := p.Apply(context.Background(), iac.RunOptions{
        ModuleSource: "testdata/simple-module",
        Inputs:       map[string]interface{}{"name": "test"},
    })

    require.NoError(t, err)
    assert.Equal(t, "https://example.com", result.Outputs["url"].Value)
}
```

### Integration Tests

Test with real IaC tool:

```go
//go:build integration

func TestPlugin_Apply_Integration(t *testing.T) {
    if _, err := exec.LookPath("myiac"); err != nil {
        t.Skip("myiac CLI not installed")
    }

    p := NewPlugin()

    result, err := p.Apply(context.Background(), iac.RunOptions{
        ModuleSource: "testdata/null-resource",
        Inputs:       map[string]interface{}{},
    })

    require.NoError(t, err)
    assert.NotNil(t, result.State)

    // Cleanup
    p.Destroy(context.Background(), iac.RunOptions{
        ModuleSource: "testdata/null-resource",
        StateReader:  bytes.NewReader(result.State),
    })
}
```

## Existing Plugin Implementations

Study these implementations for reference:

| Plugin   | Location            | Notes                                         |
| -------- | ------------------- | --------------------------------------------- |
| Pulumi   | `pkg/iac/pulumi/`   | Handles Pulumi stacks, config, and JSON state |
| OpenTofu | `pkg/iac/opentofu/` | Terraform-compatible, handles providers       |

## Module Container Format

When modules are built into OCI artifacts, they should follow this structure:

```
/
├── module/                    # Module code
│   ├── main.tf               # For OpenTofu
│   ├── index.ts              # For Pulumi TypeScript
│   └── ...
├── metadata.json             # Module metadata
└── entrypoint.sh             # Optional entrypoint script
```

Metadata format:

```json
{
  "plugin": "opentofu",
  "version": "1.0.0",
  "inputs": {
    "name": { "type": "string", "required": true },
    "region": { "type": "string", "default": "us-east-1" }
  },
  "outputs": {
    "url": { "type": "string" },
    "id": { "type": "string" }
  }
}
```
