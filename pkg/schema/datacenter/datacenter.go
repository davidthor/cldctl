// Package datacenter provides parsing and validation for datacenter configurations.
package datacenter

import (
	"github.com/davidthor/cldctl/pkg/schema/datacenter/internal"
)

// Datacenter represents a parsed and validated datacenter configuration.
type Datacenter interface {
	// Extends returns the extends metadata, or nil if not extending.
	Extends() *Extends

	// Variables
	Variables() []Variable

	// Modules (datacenter-level)
	Modules() []Module

	// Components (datacenter-level component declarations)
	Components() []DatacenterComponent

	// Environment configuration
	Environment() Environment

	// Version information
	SchemaVersion() string

	// Source information
	SourcePath() string

	// Internal access (for engine use)
	Internal() *internal.InternalDatacenter
}

// Extends represents datacenter inheritance metadata.
type Extends struct {
	Image string // OCI reference (deploy-time resolution)
	Path  string // Local path (build-time resolution)
}

// DatacenterComponent represents a component declared at the datacenter level.
// These components are deployed into environments on-demand when needed as
// dependencies by other components.
type DatacenterComponent interface {
	// Name returns the registry address (e.g., "questra/stripe")
	Name() string
	// Source returns the version tag (e.g., "latest") or file path
	Source() string
	// Variables returns HCL expression strings for runtime evaluation
	Variables() map[string]string
}

// Variable represents a datacenter variable.
type Variable interface {
	Name() string
	Type() string
	Description() string
	Default() interface{}
	Required() bool
	Sensitive() bool
}

// Module represents an IaC module configuration.
type Module interface {
	Name() string
	Build() string
	Source() string
	Plugin() string
	Inputs() map[string]string
	Environment() map[string]string
	When() string
	Volumes() []VolumeMount
}

// VolumeMount represents a volume mount.
type VolumeMount interface {
	HostPath() string
	MountPath() string
	ReadOnly() bool
}

// Environment represents environment-level configuration.
type Environment interface {
	Modules() []Module
	Hooks() Hooks
}

// Hooks provides access to resource hooks.
type Hooks interface {
	Database() []Hook
	Task() []Hook
	Bucket() []Hook
	EncryptionKey() []Hook
	SMTP() []Hook
	DatabaseUser() []Hook
	Deployment() []Hook
	Function() []Hook
	Service() []Hook
	Route() []Hook
	Cronjob() []Hook
	Secret() []Hook
	DockerBuild() []Hook
	Observability() []Hook
	Port() []Hook
}

// Hook represents a resource hook.
type Hook interface {
	When() string
	Modules() []Module
	Outputs() map[string]string
	NestedOutputs() map[string]map[string]string
	Error() string
}

// Loader loads and parses datacenter configurations.
type Loader interface {
	// Load parses a datacenter from the given path
	Load(path string) (Datacenter, error)

	// LoadFromBytes parses a datacenter from raw bytes
	LoadFromBytes(data []byte, sourcePath string) (Datacenter, error)

	// Validate validates a datacenter without fully parsing
	Validate(path string) error
}
