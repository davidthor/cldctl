// Package datacenter provides parsing and validation for datacenter configurations.
package datacenter

import (
	"github.com/architect-io/arcctl/pkg/schema/datacenter/internal"
)

// Datacenter represents a parsed and validated datacenter configuration.
type Datacenter interface {
	// Variables
	Variables() []Variable

	// Modules (datacenter-level)
	Modules() []Module

	// Environment configuration
	Environment() Environment

	// Version information
	SchemaVersion() string

	// Source information
	SourcePath() string

	// Internal access (for engine use)
	Internal() *internal.InternalDatacenter
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
	DatabaseMigration() []Hook
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
}

// Hook represents a resource hook.
type Hook interface {
	When() string
	Modules() []Module
	Outputs() map[string]string
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
