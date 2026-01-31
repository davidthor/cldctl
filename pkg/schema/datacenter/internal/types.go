// Package internal contains the canonical internal representation for datacenters.
package internal

// InternalDatacenter is the canonical internal representation.
type InternalDatacenter struct {
	// Variables
	Variables []InternalVariable

	// Datacenter-level modules
	Modules []InternalModule

	// Environment configuration
	Environment InternalEnvironment

	// Source information
	SourceVersion string
	SourcePath    string
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
	DatabaseMigration []InternalHook
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
}

// InternalHook represents a resource hook.
type InternalHook struct {
	When    string                  // Conditional expression
	Modules []InternalModule        // Modules to execute
	Outputs map[string]string       // Output mappings (HCL expressions)
}
