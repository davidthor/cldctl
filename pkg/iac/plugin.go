// Package iac provides the Infrastructure-as-Code plugin framework.
package iac

import (
	"context"
	"io"
)

// Plugin defines the interface for IaC framework plugins.
type Plugin interface {
	// Name returns the plugin identifier (e.g., "pulumi", "opentofu", "native")
	Name() string

	// Preview generates a preview of changes without applying
	Preview(ctx context.Context, opts RunOptions) (*PreviewResult, error)

	// Apply applies the module and returns outputs
	Apply(ctx context.Context, opts RunOptions) (*ApplyResult, error)

	// Destroy destroys resources created by the module
	Destroy(ctx context.Context, opts RunOptions) error

	// Refresh refreshes state without applying changes
	Refresh(ctx context.Context, opts RunOptions) (*RefreshResult, error)

	// Import adopts existing cloud resources into the module's state.
	// Each ImportMapping maps an IaC-internal resource address to a real cloud
	// resource ID. After importing, the plugin extracts outputs from the
	// resulting state so cldctl can record them.
	Import(ctx context.Context, opts ImportOptions) (*ImportResult, error)
}

// ImportMapping maps an IaC resource address to a cloud resource ID.
type ImportMapping struct {
	// Address is the IaC-module-internal resource address (e.g., "aws_db_instance.main")
	Address string

	// ID is the real cloud resource ID (e.g., "mydb-instance-123")
	ID string
}

// ImportOptions configures an import operation.
type ImportOptions struct {
	// ModuleSource is the OCI image reference or local path to the module
	ModuleSource string

	// ModulePath is the path within the module (for local modules)
	ModulePath string

	// Inputs are the values passed to the module
	Inputs map[string]interface{}

	// Mappings are the resource address to cloud ID mappings
	Mappings []ImportMapping

	// WorkDir is the working directory for execution
	WorkDir string

	// Environment contains environment variables for the execution
	Environment map[string]string

	// Stdout/Stderr for command output
	Stdout io.Writer
	Stderr io.Writer
}

// ImportResult contains the result of an import operation.
type ImportResult struct {
	// Outputs extracted from the imported state
	Outputs map[string]OutputValue

	// State is the serialized IaC state after import
	State []byte

	// ImportedResources lists the addresses that were successfully imported
	ImportedResources []string
}

// RunOptions configures a plugin execution.
type RunOptions struct {
	// ModuleSource is the OCI image reference or local path to the module
	ModuleSource string

	// ModulePath is the path within the module (for local modules)
	ModulePath string

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

	// OnProgress reports sub-status updates during long-running operations
	// (e.g., "pulling image...", "health check 5/30"). May be nil.
	OnProgress func(message string)
}

// VolumeMount defines a volume mount for module execution.
type VolumeMount struct {
	HostPath  string
	MountPath string
	ReadOnly  bool
}

// PreviewResult contains the result of a preview operation.
type PreviewResult struct {
	Changes []ResourceChange
	Summary ChangeSummary
}

// ResourceChange describes a planned change to a resource.
type ResourceChange struct {
	ResourceID   string
	ResourceType string
	Action       ChangeAction // Create, Update, Delete, Replace
	Before       interface{}  // Current state (nil for create)
	After        interface{}  // Planned state (nil for delete)
	Diff         []PropertyDiff
}

// PropertyDiff describes a change to a property.
type PropertyDiff struct {
	Path      string
	OldValue  interface{}
	NewValue  interface{}
	Sensitive bool
}

// ChangeAction indicates the type of change.
type ChangeAction string

const (
	ActionCreate  ChangeAction = "create"
	ActionUpdate  ChangeAction = "update"
	ActionDelete  ChangeAction = "delete"
	ActionReplace ChangeAction = "replace"
	ActionNoop    ChangeAction = "noop"
)

// ChangeSummary summarizes planned changes.
type ChangeSummary struct {
	Create  int
	Update  int
	Delete  int
	Replace int
}

// ApplyResult contains the result of an apply operation.
type ApplyResult struct {
	Outputs map[string]OutputValue
	State   []byte // Serialized state for persistence

	// PartialError is set if apply partially succeeded
	PartialError error
}

// OutputValue represents a module output.
type OutputValue struct {
	Value     interface{}
	Sensitive bool
}

// RefreshResult contains the result of a refresh operation.
type RefreshResult struct {
	State  []byte
	Drifts []ResourceDrift
}

// ResourceDrift describes drift between state and actual infrastructure.
type ResourceDrift struct {
	ResourceID   string
	ResourceType string
	Diffs        []PropertyDiff
}
