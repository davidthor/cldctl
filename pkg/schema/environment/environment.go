// Package environment provides parsing and validation for environment configurations.
package environment

import (
	"github.com/davidthor/cldctl/pkg/schema/environment/internal"
)

// Environment represents a parsed and validated environment configuration.
type Environment interface {
	// Name returns the optional environment name from the config file.
	// Returns empty string if not specified.
	Name() string

	// Variables returns environment-level variable declarations
	Variables() map[string]EnvironmentVariable

	// Locals
	Locals() map[string]interface{}

	// Components
	Components() map[string]ComponentConfig

	// Version information
	SchemaVersion() string

	// Source information
	SourcePath() string

	// Internal access (for engine use)
	Internal() *internal.InternalEnvironment
}

// EnvironmentVariable represents an environment-level variable declaration.
type EnvironmentVariable interface {
	// Name returns the variable name
	Name() string

	// Description returns a human-readable description
	Description() string

	// Default returns the default value, or nil if none
	Default() interface{}

	// Required returns true if the variable must be provided
	Required() bool

	// Sensitive returns true if the value should be masked
	Sensitive() bool

	// Env returns the explicit OS env var name, or empty string for auto-mapping
	Env() string
}

// ComponentConfig represents a component's configuration within an environment.
// Exactly one of Path or Image will be set.
type ComponentConfig interface {
	// Path returns the local file path to the component, or empty string if using an image.
	Path() string

	// Image returns the OCI registry reference, or empty string if using a path.
	Image() string

	// Variable values
	Variables() map[string]interface{}

	// Port overrides (maps port name to specific port number)
	Ports() map[string]int

	// Scaling configurations per deployment
	Scaling() map[string]ScalingConfig

	// Function configurations per function
	Functions() map[string]FunctionConfig

	// Environment variable overrides per deployment
	Environment() map[string]map[string]string

	// Route configurations per route
	Routes() map[string]RouteConfig
}

// ScalingConfig represents scaling configuration for a deployment.
type ScalingConfig interface {
	Replicas() int
	CPU() string
	Memory() string
	MinReplicas() int
	MaxReplicas() int
}

// FunctionConfig represents configuration for a serverless function.
type FunctionConfig interface {
	Regions() []string
	Memory() string
	Timeout() int
}

// RouteConfig represents route configuration.
type RouteConfig interface {
	Hostnames() []Hostname
	TLS() TLSConfig
}

// Hostname represents a hostname configuration.
type Hostname interface {
	Subdomain() string
	Host() string
}

// TLSConfig represents TLS configuration.
type TLSConfig interface {
	Enabled() bool
	SecretName() string
}

// Loader loads and parses environment configurations.
type Loader interface {
	// Load parses an environment from the given path
	Load(path string) (Environment, error)

	// LoadFromBytes parses an environment from raw bytes
	LoadFromBytes(data []byte, sourcePath string) (Environment, error)

	// Validate validates an environment without fully parsing
	Validate(path string) error
}
