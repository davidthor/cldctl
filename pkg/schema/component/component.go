// Package component provides parsing and validation for component configurations.
package component

import (
	"github.com/architect-io/arcctl/pkg/schema/component/internal"
)

// Component represents a parsed and validated component configuration.
// This is the public interface used throughout arcctl.
type Component interface {
	// Metadata
	Readme() string // README content loaded from README.md if present

	// Resources
	Databases() []Database
	Buckets() []Bucket
	EncryptionKeys() []EncryptionKey
	SMTP() []SMTPConnection
	Deployments() []Deployment
	Functions() []Function
	Services() []Service
	Routes() []Route
	Cronjobs() []Cronjob

	// Configuration
	Variables() []Variable
	Dependencies() []Dependency
	Outputs() []Output

	// Version information
	SchemaVersion() string

	// Source information
	SourcePath() string

	// Serialization
	ToYAML() ([]byte, error)
	ToJSON() ([]byte, error)

	// Internal access (for engine use)
	Internal() *internal.InternalComponent
}

// Database represents a database requirement.
type Database interface {
	Name() string
	Type() string
	Version() string
	Migrations() Migrations
}

// Migrations represents database migration configuration.
type Migrations interface {
	Image() string
	Build() Build
	Command() []string
	Environment() map[string]string
}

// Build represents a container build configuration.
type Build interface {
	Context() string
	Dockerfile() string
	Target() string
	Args() map[string]string
}

// Bucket represents a blob storage requirement.
type Bucket interface {
	Name() string
	Type() string
	Versioning() bool
	Public() bool
}

// EncryptionKey represents an encryption key requirement.
type EncryptionKey interface {
	Name() string
	Type() string  // rsa, ecdsa, symmetric
	Bits() int     // For RSA keys
	Curve() string // For ECDSA keys
	Bytes() int    // For symmetric keys
}

// SMTPConnection represents an SMTP email connection requirement.
type SMTPConnection interface {
	Name() string
	Description() string
}

// Deployment represents a deployment workload.
type Deployment interface {
	Name() string
	Image() string
	Build() Build
	Command() []string
	Entrypoint() []string
	Environment() map[string]string
	CPU() string
	Memory() string
	Replicas() int
	Volumes() []Volume
	LivenessProbe() Probe
	ReadinessProbe() Probe
}

// Function represents a serverless function.
type Function interface {
	Name() string
	Image() string
	Build() Build
	Runtime() string
	Framework() string
	Environment() map[string]string
	CPU() string
	Memory() string
	Timeout() int
}

// Service represents internal service exposure for deployments.
// Note: Functions don't need services - routes can point directly to functions.
type Service interface {
	Name() string
	Deployment() string
	URL() string
	Port() int
	Protocol() string
}

// Route represents external traffic routing.
type Route interface {
	Name() string
	Type() string
	Internal() bool
	Rules() []RouteRule
	Service() string
	Function() string
	Port() int
}

// RouteRule represents a routing rule.
type RouteRule interface {
	Name() string
	Matches() []RouteMatch
	BackendRefs() []BackendRef
	Filters() []RouteFilter
	Timeouts() Timeouts
}

// RouteMatch represents route matching conditions.
type RouteMatch interface {
	Path() PathMatch
	Headers() []HeaderMatch
	QueryParams() []QueryParamMatch
	Method() string
	GRPCMethod() GRPCMethodMatch
}

// PathMatch represents path matching.
type PathMatch interface {
	Type() string
	Value() string
}

// HeaderMatch represents header matching.
type HeaderMatch interface {
	Name() string
	Type() string
	Value() string
}

// QueryParamMatch represents query parameter matching.
type QueryParamMatch interface {
	Name() string
	Type() string
	Value() string
}

// GRPCMethodMatch represents gRPC method matching.
type GRPCMethodMatch interface {
	Service() string
	Method() string
}

// BackendRef represents a backend reference.
type BackendRef interface {
	Service() string
	Function() string
	Port() int
	Weight() int
}

// RouteFilter represents request/response processing.
type RouteFilter interface {
	Type() string
}

// Timeouts represents timeout configuration.
type Timeouts interface {
	Request() string
	BackendRequest() string
}

// Cronjob represents a scheduled task.
type Cronjob interface {
	Name() string
	Image() string
	Build() Build
	Schedule() string
	Command() []string
	Environment() map[string]string
	CPU() string
	Memory() string
}

// Variable represents a configurable input.
type Variable interface {
	Name() string
	Description() string
	Default() interface{}
	Required() bool
	Sensitive() bool
}

// Dependency represents a dependency on another component.
type Dependency interface {
	Name() string
	Component() string
	Variables() map[string]string
}

// Output represents an output value exposed to dependents.
// Dependents access outputs via dependencies.<name>.outputs.<output>.
type Output interface {
	Name() string
	Description() string
	Value() string // Expression string
	Sensitive() bool
}

// Volume represents a volume mount.
type Volume interface {
	MountPath() string
	HostPath() string
	Name() string
	ReadOnly() bool
}

// Probe represents a health check probe.
type Probe interface {
	Path() string
	Port() int
	Command() []string
	TCPPort() int
	InitialDelaySeconds() int
	PeriodSeconds() int
	TimeoutSeconds() int
	SuccessThreshold() int
	FailureThreshold() int
}

// Loader loads and parses component configurations.
type Loader interface {
	// Load parses a component from the given path
	Load(path string) (Component, error)

	// LoadFromBytes parses a component from raw bytes
	LoadFromBytes(data []byte, sourcePath string) (Component, error)

	// Validate validates a component without fully parsing
	Validate(path string) error
}
