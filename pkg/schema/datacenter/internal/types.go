// Package internal contains the canonical internal representation for datacenters.
package internal

// InternalDatacenter is the canonical internal representation.
type InternalDatacenter struct {
	// Variables
	Variables []InternalVariable

	// Datacenter-level modules
	Modules []InternalModule

	// Datacenter-level component declarations.
	// These components are deployed into environments on-demand when needed
	// as dependencies by other components.
	Components []InternalDatacenterComponent

	// Environment configuration
	Environment InternalEnvironment

	// Source information
	SourceVersion string
	SourcePath    string
}

// InternalDatacenterComponent represents a component declared at the datacenter level.
// It provides source and variable configuration so the component can be automatically
// deployed into environments when referenced as a dependency.
type InternalDatacenterComponent struct {
	Name      string            // Registry address (e.g., "questra/stripe")
	Source    string            // Version tag (e.g., "latest") or file path
	Variables map[string]string // HCL expression strings (evaluated at runtime with datacenter variables)
}

// InternalVariable represents a datacenter variable.
type InternalVariable struct {
	Name        string
	Type        string
	Description string
	Default     interface{}
	Required    bool
	Sensitive   bool
}

// InternalModule represents an IaC module.
type InternalModule struct {
	Name string

	// Source (one is set)
	Build  string // Local path for source form
	Source string // OCI reference for compiled form

	// Configuration
	Plugin      string                 // "pulumi", "opentofu", "native"
	Inputs      map[string]string      // Input values (may be HCL expressions)
	Environment map[string]string      // Environment variables
	When        string                 // Conditional expression

	// Volume mounts
	Volumes []InternalVolumeMount
}

// InternalVolumeMount represents a volume mount for module execution.
type InternalVolumeMount struct {
	HostPath  string
	MountPath string
	ReadOnly  bool
}

// InternalEnvironment represents environment-level configuration.
type InternalEnvironment struct {
	Modules []InternalModule
	Hooks   InternalHooks
}

// InternalHooks contains resource hooks.
type InternalHooks struct {
	Database          []InternalHook
	Task              []InternalHook
	Bucket            []InternalHook
	EncryptionKey     []InternalHook
	SMTP              []InternalHook
	DatabaseUser      []InternalHook
	Deployment        []InternalHook
	Function          []InternalHook
	Service           []InternalHook
	Route             []InternalHook
	Cronjob           []InternalHook
	Secret            []InternalHook
	DockerBuild       []InternalHook
	Observability     []InternalHook
	Port              []InternalHook
}

// InternalHook represents a resource hook.
type InternalHook struct {
	When          string                        // Conditional expression
	Modules       []InternalModule              // Modules to execute
	Outputs       map[string]string             // Output mappings (HCL expressions)
	NestedOutputs map[string]map[string]string  // Nested output objects (e.g., read/write sub-objects for database hooks)
	Error         string                        // Human-readable error message (mutually exclusive with Modules/Outputs)
}
