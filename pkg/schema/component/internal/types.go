// Package internal contains the canonical internal representation for components.
// All version-specific schemas transform to these types.
package internal

// InternalComponent is the canonical internal representation.
// All version-specific schemas transform to this type.
type InternalComponent struct {
	// Metadata
	Readme string // README content loaded from README.md if present

	// Resources
	Databases      []InternalDatabase
	Buckets        []InternalBucket
	EncryptionKeys []InternalEncryptionKey
	SMTP           []InternalSMTP
	Deployments    []InternalDeployment
	Functions      []InternalFunction
	Services       []InternalService
	Routes         []InternalRoute
	Cronjobs       []InternalCronjob

	// Configuration
	Variables    []InternalVariable
	Dependencies []InternalDependency
	Outputs      []InternalOutput

	// Source information
	SourceVersion string // Which schema version this came from
	SourcePath    string // Original file path
}

// InternalDatabase represents a database requirement.
type InternalDatabase struct {
	Name       string
	Type       string              // e.g., "postgres"
	Version    string              // e.g., "^15" (semver constraint)
	Migrations *InternalMigrations // Optional
}

// InternalMigrations represents database migration configuration.
type InternalMigrations struct {
	// Source form (mutually exclusive with Image)
	Build *InternalBuild

	// Compiled form (mutually exclusive with Build)
	Image string

	// Common fields
	Command     []string
	Environment map[string]string
}

// InternalBuild represents a container build configuration.
type InternalBuild struct {
	Context    string            // Build context directory
	Dockerfile string            // Dockerfile path within context
	Target     string            // Build target for multi-stage
	Args       map[string]string // Build arguments
}

// InternalBucket represents a blob storage requirement.
type InternalBucket struct {
	Name       string
	Type       string // e.g., "s3", "gcs", "azure-blob"
	Versioning bool
	Public     bool
}

// InternalEncryptionKey represents an encryption key requirement.
// Keys are discriminated by type: rsa, ecdsa, or symmetric.
type InternalEncryptionKey struct {
	Name  string
	Type  string // "rsa", "ecdsa", "symmetric"
	Bits  int    // For RSA: 2048, 3072, 4096
	Curve string // For ECDSA: P-256, P-384, P-521
	Bytes int    // For symmetric: key length in bytes
}

// InternalSMTP represents an SMTP email connection requirement.
type InternalSMTP struct {
	Name        string
	Description string // Optional description
}

// InternalDeployment represents a deployment workload.
type InternalDeployment struct {
	Name string

	// Image source (one of these is set)
	Image string          // Pre-built image reference
	Build *InternalBuild  // Build from source

	// Container configuration
	Command     []string
	Entrypoint  []string
	Environment map[string]Expression // Values may contain expressions

	// Resource allocation
	CPU      string
	Memory   string
	Replicas int

	// Advanced configuration
	Volumes        []InternalVolume
	LivenessProbe  *InternalProbe
	ReadinessProbe *InternalProbe
	Labels         map[string]string
}

// InternalFunction represents a serverless function.
type InternalFunction struct {
	Name string

	// Image source (one of these is set)
	Image string
	Build *InternalBuild

	// Function configuration
	Runtime     string // e.g., "nodejs:20", "python:3.11"
	Framework   string // e.g., "nextjs", "nuxt"
	Environment map[string]Expression

	// Resource allocation
	CPU     string
	Memory  string
	Timeout int // seconds
}

// InternalService represents internal service exposure for deployments.
// Note: Functions don't need services - routes can point directly to functions.
type InternalService struct {
	Name string

	// Target (one of these is set)
	Deployment string // Target deployment name
	URL        string // External URL (virtual service)

	// Configuration
	Port     int
	Protocol string // http, https, tcp, grpc
}

// InternalRoute represents external traffic routing configuration.
type InternalRoute struct {
	Name     string
	Type     string // "http" or "grpc"
	Internal bool   // VPC-only access

	// Full routing configuration
	Rules []InternalRouteRule

	// Simplified form (alternative to Rules)
	Service  string // Direct service reference
	Function string // Direct function reference
	Port     int
}

// InternalRouteRule represents a routing rule.
type InternalRouteRule struct {
	Name        string
	Matches     []InternalRouteMatch
	BackendRefs []InternalBackendRef
	Filters     []InternalRouteFilter
	Timeouts    *InternalTimeouts
}

// InternalRouteMatch represents route matching conditions.
type InternalRouteMatch struct {
	// HTTP matching
	Path        *InternalPathMatch
	Headers     []InternalHeaderMatch
	QueryParams []InternalQueryParamMatch
	Method      string // HTTP method

	// gRPC matching
	GRPCMethod *InternalGRPCMethodMatch
}

// InternalPathMatch represents path matching.
type InternalPathMatch struct {
	Type  string // PathPrefix, Exact, RegularExpression
	Value string
}

// InternalHeaderMatch represents header matching.
type InternalHeaderMatch struct {
	Name  string
	Type  string // Exact, RegularExpression
	Value string
}

// InternalQueryParamMatch represents query parameter matching.
type InternalQueryParamMatch struct {
	Name  string
	Type  string // Exact, RegularExpression
	Value string
}

// InternalGRPCMethodMatch represents gRPC method matching.
type InternalGRPCMethodMatch struct {
	Service string
	Method  string
}

// InternalBackendRef represents a backend reference with weight.
type InternalBackendRef struct {
	Service  string
	Function string
	Port     int
	Weight   int
}

// InternalRouteFilter represents request/response processing.
type InternalRouteFilter struct {
	Type                    string // RequestHeaderModifier, ResponseHeaderModifier, etc.
	RequestHeaderModifier   *InternalHeaderModifier
	ResponseHeaderModifier  *InternalHeaderModifier
	RequestRedirect         *InternalRedirect
	URLRewrite              *InternalURLRewrite
	RequestMirror           *InternalMirror
}

// InternalHeaderModifier modifies headers.
type InternalHeaderModifier struct {
	Add    []InternalHeaderValue
	Set    []InternalHeaderValue
	Remove []string
}

// InternalHeaderValue represents a header key-value pair.
type InternalHeaderValue struct {
	Name  string
	Value string
}

// InternalRedirect represents a redirect configuration.
type InternalRedirect struct {
	Scheme     string
	Hostname   string
	Port       int
	StatusCode int
}

// InternalURLRewrite represents URL rewriting.
type InternalURLRewrite struct {
	Hostname        string
	Path            *InternalPathModifier
}

// InternalPathModifier represents path modification.
type InternalPathModifier struct {
	Type               string // ReplaceFullPath, ReplacePrefixMatch
	ReplaceFullPath    string
	ReplacePrefixMatch string
}

// InternalMirror represents request mirroring.
type InternalMirror struct {
	Service string
	Port    int
}

// InternalTimeouts represents timeout configuration.
type InternalTimeouts struct {
	Request        string
	BackendRequest string
}

// InternalCronjob represents a scheduled task.
type InternalCronjob struct {
	Name string

	// Image source
	Image string
	Build *InternalBuild

	// Schedule
	Schedule string // Cron expression

	// Configuration
	Command     []string
	Environment map[string]Expression

	// Resource allocation
	CPU    string
	Memory string
}

// InternalVariable represents a configurable input.
type InternalVariable struct {
	Name        string
	Description string
	Default     interface{}
	Required    bool
	Sensitive   bool
}

// InternalDependency represents a dependency on another component.
type InternalDependency struct {
	Name      string
	Component string            // OCI reference or local path
	Variables map[string]string // Variable overrides
}

// InternalOutput represents an output value exposed to dependents.
// Dependents access outputs via dependencies.<name>.outputs.<output>.
type InternalOutput struct {
	Name        string
	Description string
	Value       Expression // Expression that resolves to the output value
	Sensitive   bool
}

// InternalVolume represents a volume mount.
type InternalVolume struct {
	MountPath string
	HostPath  string
	Name      string
	ReadOnly  bool
}

// InternalProbe represents a health check probe.
type InternalProbe struct {
	// HTTP probe
	Path string
	Port int

	// Exec probe
	Command []string

	// TCP probe
	TCPPort int

	// Timing
	InitialDelaySeconds int
	PeriodSeconds       int
	TimeoutSeconds      int
	SuccessThreshold    int
	FailureThreshold    int
}

// Expression represents a value that may contain expressions.
// This wraps string values that need expression evaluation.
type Expression struct {
	Raw        string // Original string value
	IsTemplate bool   // Whether this contains ${{ }} expressions
}

// NewExpression creates a new Expression from a string value.
func NewExpression(value string) Expression {
	return Expression{
		Raw:        value,
		IsTemplate: containsExpression(value),
	}
}

// containsExpression checks if a string contains ${{ }} expressions.
func containsExpression(s string) bool {
	return len(s) > 5 && contains(s, "${{") && contains(s, "}}")
}

// contains is a simple substring check.
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
