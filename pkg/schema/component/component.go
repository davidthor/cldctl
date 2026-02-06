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

	// Build artifacts
	Builds() []ComponentBuild

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

	// Observability
	Observability() Observability

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

// ComponentBuild represents a top-level named Docker build configuration.
// Deployments reference the built image via ${{ builds.<name>.image }}.
type ComponentBuild interface {
	Name() string
	Context() string
	Dockerfile() string
	Target() string
	Args() map[string]string
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
// Image is optional. When absent, the datacenter decides how to execute
// (e.g., as a host process for local development).
type Deployment interface {
	Name() string
	Image() string
	Runtime() Runtime
	Command() []string
	Entrypoint() []string
	Environment() map[string]string
	WorkingDirectory() string
	CPU() string
	Memory() string
	Replicas() int
	Volumes() []Volume
	LivenessProbe() Probe
	ReadinessProbe() Probe
}

// Runtime describes the runtime environment for a deployment.
// When present without an image, the datacenter can provision a VM or managed runtime.
type Runtime interface {
	Language() string  // Required: language and version (e.g., "node:20", "python:^3.12")
	OS() string        // Optional: target OS (default: linux)
	Arch() string      // Optional: target architecture
	Packages() []string // Optional: system-level dependencies
	Setup() []string    // Optional: provisioning commands
}

// Function represents a serverless function.
// Uses a discriminated union: either Src() or Container() returns non-nil (not both).
type Function interface {
	Name() string

	// Discriminated union - exactly one returns non-nil
	Src() FunctionSource
	Container() FunctionContainer

	// Common configuration
	Port() int
	Environment() map[string]string
	CPU() string
	Memory() string
	Timeout() int

	// IsSourceBased returns true if this is a source-based function
	IsSourceBased() bool
	// IsContainerBased returns true if this is a container-based function
	IsContainerBased() bool
}

// FunctionSource represents source-based function configuration.
// Most fields are optional and can be inferred from project files.
type FunctionSource interface {
	Path() string      // Required: path to source code
	Language() string  // e.g., "javascript", "typescript", "python", "go"
	Runtime() string   // e.g., "nodejs20.x", "python3.11" (for Lambda)
	Framework() string // e.g., "nextjs", "fastapi", "express"
	Install() string   // Dependency installation command
	Dev() string       // Development server command
	Build() string     // Production build command
	Start() string     // Production start command
	Handler() string   // Lambda-style handler (e.g., "index.handler")
	Entry() string     // Entry point file
}

// FunctionContainer represents container-based function configuration.
// Either Build() or Image() returns non-empty (not both).
type FunctionContainer interface {
	Build() Build  // Build from Dockerfile (nil if using image)
	Image() string // Pre-built image reference (empty if using build)
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

// Observability represents the observability configuration for a component.
// Returns nil if observability is not configured or explicitly disabled.
type Observability interface {
	Inject() bool                // Auto-inject OTEL_* env vars into workloads
	Attributes() map[string]string // Custom OTel resource attributes
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
// The Component value is a repo:tag reference (tag is optional).
// Tag supports semver expressions like "^1" or "~2.0".
type Dependency interface {
	Name() string
	Component() string
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
