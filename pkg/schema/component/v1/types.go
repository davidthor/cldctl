// Package v1 implements the v1 component schema.
package v1

import (
	"fmt"
)

// interfaceToString converts an interface{} value (int, float64, or string) to a string.
// Used for fields that support both integer literals and expression strings.
func interfaceToString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case int:
		return fmt.Sprintf("%d", val)
	case float64:
		return fmt.Sprintf("%d", int(val))
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}

// SchemaV1 represents the v1 component schema.
type SchemaV1 struct {
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
	Extends string `yaml:"extends,omitempty" json:"extends,omitempty"`

	Builds         map[string]BuildV1         `yaml:"builds,omitempty" json:"builds,omitempty"`
	Databases      map[string]DatabaseV1      `yaml:"databases,omitempty" json:"databases,omitempty"`
	Buckets        map[string]BucketV1        `yaml:"buckets,omitempty" json:"buckets,omitempty"`
	EncryptionKeys map[string]EncryptionKeyV1 `yaml:"encryptionKeys,omitempty" json:"encryptionKeys,omitempty"`
	SMTP           map[string]SMTPV1          `yaml:"smtp,omitempty" json:"smtp,omitempty"`
	Ports          map[string]PortV1          `yaml:"ports,omitempty" json:"ports,omitempty"`
	Deployments    map[string]DeploymentV1    `yaml:"deployments,omitempty" json:"deployments,omitempty"`
	Functions      map[string]FunctionV1      `yaml:"functions,omitempty" json:"functions,omitempty"`
	Services       map[string]ServiceV1       `yaml:"services,omitempty" json:"services,omitempty"`
	Routes         map[string]RouteV1         `yaml:"routes,omitempty" json:"routes,omitempty"`
	Cronjobs       map[string]CronjobV1       `yaml:"cronjobs,omitempty" json:"cronjobs,omitempty"`

	Observability *ObservabilityV1 `yaml:"observability,omitempty" json:"observability,omitempty"`

	Variables    map[string]VariableV1   `yaml:"variables,omitempty" json:"variables,omitempty"`
	Dependencies map[string]DependencyV1 `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	Outputs      map[string]OutputV1     `yaml:"outputs,omitempty" json:"outputs,omitempty"`
}

// ObservabilityV1 represents observability configuration in the v1 schema.
// Supports both boolean shorthand (true/false) and full object form.
// When enabled, the datacenter's observability hook provides OTel infrastructure
// and outputs are available via ${{ observability.endpoint }} expressions.
// When inject is true, the engine also auto-injects standard OTEL_* env vars
// into all workloads.
type ObservabilityV1 struct {
	Enabled    bool              `yaml:"-" json:"-"`                                       // Internal: tracks if observability is enabled
	Inject     *bool             `yaml:"inject,omitempty" json:"inject,omitempty"`         // Auto-inject OTEL_* env vars (default: false)
	Attributes map[string]string `yaml:"attributes,omitempty" json:"attributes,omitempty"` // Custom OTel resource attributes
}

// UnmarshalYAML supports both boolean shorthand (true/false) and full object form.
func (o *ObservabilityV1) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try boolean shorthand first
	var b bool
	if err := unmarshal(&b); err == nil {
		o.Enabled = b
		return nil
	}

	// Fall back to full object form
	type rawObservability ObservabilityV1
	var raw rawObservability
	if err := unmarshal(&raw); err != nil {
		return fmt.Errorf("observability must be a boolean or an object: %w", err)
	}
	*o = ObservabilityV1(raw)
	o.Enabled = true
	return nil
}

// DatabaseV1 represents a database in the v1 schema.
type DatabaseV1 struct {
	Type       string        `yaml:"type" json:"type"`
	Migrations *MigrationsV1 `yaml:"migrations,omitempty" json:"migrations,omitempty"`
}

// MigrationsV1 represents migrations in the v1 schema.
// Image and runtime are mutually exclusive. When neither is set, the datacenter
// decides how to execute (e.g., as a local process for development).
type MigrationsV1 struct {
	Image            string            `yaml:"image,omitempty" json:"image,omitempty"`
	Runtime          *RuntimeV1        `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Command          []string          `yaml:"command,omitempty" json:"command,omitempty"`
	Environment      map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`
	WorkingDirectory string            `yaml:"workingDirectory,omitempty" json:"workingDirectory,omitempty"`
}

// BuildV1 represents a build configuration in the v1 schema.
type BuildV1 struct {
	Context    string            `yaml:"context" json:"context"`
	Dockerfile string            `yaml:"dockerfile,omitempty" json:"dockerfile,omitempty"`
	Target     string            `yaml:"target,omitempty" json:"target,omitempty"`
	Args       map[string]string `yaml:"args,omitempty" json:"args,omitempty"`
}

// BucketV1 represents a bucket in the v1 schema.
type BucketV1 struct {
	Type       string `yaml:"type" json:"type"`
	Versioning bool   `yaml:"versioning,omitempty" json:"versioning,omitempty"`
	Public     bool   `yaml:"public,omitempty" json:"public,omitempty"`
}

// EncryptionKeyV1 represents an encryption key in the v1 schema.
// Keys are discriminated by type: rsa, ecdsa, or symmetric.
type EncryptionKeyV1 struct {
	Type  string `yaml:"type" json:"type"`                       // Required: rsa, ecdsa, symmetric
	Bits  int    `yaml:"bits,omitempty" json:"bits,omitempty"`   // For RSA: 2048, 3072, 4096 (default: 2048)
	Curve string `yaml:"curve,omitempty" json:"curve,omitempty"` // For ECDSA: P-256, P-384, P-521 (default: P-256)
	Bytes int    `yaml:"bytes,omitempty" json:"bytes,omitempty"` // For symmetric: key length in bytes (default: 32)
}

// SMTPV1 represents an SMTP email connection in the v1 schema.
// The declaration may be empty - the datacenter provisions the connection credentials.
type SMTPV1 struct {
	Description string `yaml:"description,omitempty" json:"description,omitempty"` // Optional description
}

// PortV1 represents a dynamic port allocation in the v1 schema.
// Ports are allocated by the engine (or a datacenter hook) and can be referenced
// in environment variables and service ports via ${{ ports.<name>.port }}.
// Supports boolean shorthand (true) for minimal declarations.
type PortV1 struct {
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// UnmarshalYAML supports both boolean shorthand (true) and full object form.
func (p *PortV1) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try boolean shorthand first
	var b bool
	if err := unmarshal(&b); err == nil {
		if !b {
			return fmt.Errorf("port must be true or an object (false is not valid)")
		}
		// true creates an empty PortV1
		return nil
	}

	// Fall back to full object form
	type rawPort PortV1
	var raw rawPort
	if err := unmarshal(&raw); err != nil {
		return fmt.Errorf("port must be true or an object with optional description: %w", err)
	}
	*p = PortV1(raw)
	return nil
}

// DeploymentV1 represents a deployment in the v1 schema.
// Image and runtime are optional. When neither is set, the datacenter decides
// how to execute the workload (e.g., as a host process for local development).
type DeploymentV1 struct {
	Image            string            `yaml:"image,omitempty" json:"image,omitempty"`
	Runtime          *RuntimeV1        `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Command          []string          `yaml:"command,omitempty" json:"command,omitempty"`
	Entrypoint       []string          `yaml:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	Environment      map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`
	WorkingDirectory string            `yaml:"workingDirectory,omitempty" json:"workingDirectory,omitempty"`
	CPU              string            `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	Memory           string            `yaml:"memory,omitempty" json:"memory,omitempty"`
	Replicas         int               `yaml:"replicas,omitempty" json:"replicas,omitempty"`
	Volumes          []VolumeV1        `yaml:"volumes,omitempty" json:"volumes,omitempty"`
	LivenessProbe    *ProbeV1          `yaml:"liveness_probe,omitempty" json:"liveness_probe,omitempty"`
	ReadinessProbe   *ProbeV1          `yaml:"readiness_probe,omitempty" json:"readiness_probe,omitempty"`
	Labels           map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// RuntimeV1 describes the runtime environment for a deployment.
// Supports both a string shorthand ("node:20") and a full object form.
// When present without an image, the datacenter can provision a VM or managed runtime.
type RuntimeV1 struct {
	Language string   `yaml:"language" json:"language"`                     // Required: language and version (e.g., "node:20", "python:^3.12")
	OS       string   `yaml:"os,omitempty" json:"os,omitempty"`             // Optional: target OS (default: linux)
	Arch     string   `yaml:"arch,omitempty" json:"arch,omitempty"`         // Optional: target architecture (default: datacenter's choice)
	Packages []string `yaml:"packages,omitempty" json:"packages,omitempty"` // Optional: system-level dependencies
	Setup    []string `yaml:"setup,omitempty" json:"setup,omitempty"`       // Optional: provisioning commands
}

// UnmarshalYAML supports both string shorthand ("node:20") and full object form.
func (r *RuntimeV1) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try string shorthand first
	var s string
	if err := unmarshal(&s); err == nil {
		r.Language = s
		return nil
	}

	// Fall back to full object form
	type rawRuntime RuntimeV1
	var raw rawRuntime
	if err := unmarshal(&raw); err != nil {
		return fmt.Errorf("runtime must be a string (e.g., \"node:20\") or an object with a language field: %w", err)
	}
	*r = RuntimeV1(raw)
	return nil
}

// FunctionV1 represents a function in the v1 schema.
// Functions use a discriminated union: either Src OR Container must be set (not both).
type FunctionV1 struct {
	// Discriminated union - exactly one must be set
	Src       *FunctionSourceV1    `yaml:"src,omitempty" json:"src,omitempty"`
	Container *FunctionContainerV1 `yaml:"container,omitempty" json:"container,omitempty"`

	// Common fields (valid for both src and container)
	// Port supports both integer literals (3000) and expression strings (${{ ports.web.port }}).
	Port        interface{}       `yaml:"port,omitempty" json:"port,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`
	CPU         string            `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	Memory      string            `yaml:"memory,omitempty" json:"memory,omitempty"`
	Timeout     int               `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// FunctionSourceV1 represents a source-based function configuration.
// Most fields are optional and can be inferred from project files.
type FunctionSourceV1 struct {
	Path      string `yaml:"path" json:"path"`                               // Required: path to source code
	Language  string `yaml:"language,omitempty" json:"language,omitempty"`   // e.g., "javascript", "typescript", "python", "go"
	Runtime   string `yaml:"runtime,omitempty" json:"runtime,omitempty"`     // e.g., "nodejs20.x", "python3.11" (for Lambda)
	Framework string `yaml:"framework,omitempty" json:"framework,omitempty"` // e.g., "nextjs", "fastapi", "express"
	Install   string `yaml:"install,omitempty" json:"install,omitempty"`     // e.g., "npm install", "pip install -r requirements.txt"
	Dev       string `yaml:"dev,omitempty" json:"dev,omitempty"`             // Development command, e.g., "npm run dev"
	Build     string `yaml:"build,omitempty" json:"build,omitempty"`         // Build command, e.g., "npm run build"
	Start     string `yaml:"start,omitempty" json:"start,omitempty"`         // Production start command
	Handler   string `yaml:"handler,omitempty" json:"handler,omitempty"`     // Lambda-style handler, e.g., "index.handler"
	Entry     string `yaml:"entry,omitempty" json:"entry,omitempty"`         // Entry point file
}

// FunctionContainerV1 represents a container-based function configuration.
// Either Build or Image must be set (not both).
type FunctionContainerV1 struct {
	Build *BuildV1 `yaml:"build,omitempty" json:"build,omitempty"` // Build from Dockerfile
	Image string   `yaml:"image,omitempty" json:"image,omitempty"` // Pre-built image reference
}

// ServiceV1 represents a service in the v1 schema.
// Services expose deployments for internal communication.
// Note: Functions don't need services - routes can point directly to functions.
// Port supports both integer literals (8080) and expression strings (${{ ports.api.port }}).
type ServiceV1 struct {
	Deployment string      `yaml:"deployment" json:"deployment"`
	URL        string      `yaml:"url,omitempty" json:"url,omitempty"`
	Port       interface{} `yaml:"-" json:"-"`                           // int or string; handled by custom UnmarshalYAML
	PortRaw    interface{} `yaml:"port,omitempty" json:"port,omitempty"` // raw YAML value
	Protocol   string      `yaml:"protocol,omitempty" json:"protocol,omitempty"`
}

// UnmarshalYAML handles port being either an int or a string expression.
func (s *ServiceV1) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type rawService struct {
		Deployment string      `yaml:"deployment"`
		URL        string      `yaml:"url,omitempty"`
		Port       interface{} `yaml:"port,omitempty"`
		Protocol   string      `yaml:"protocol,omitempty"`
	}
	var raw rawService
	if err := unmarshal(&raw); err != nil {
		return err
	}
	s.Deployment = raw.Deployment
	s.URL = raw.URL
	s.Protocol = raw.Protocol
	s.PortRaw = raw.Port
	s.Port = raw.Port
	return nil
}

// PortAsString returns the port value as a string (for expression handling).
func (s *ServiceV1) PortAsString() string {
	if s.Port == nil {
		return ""
	}
	switch v := s.Port.(type) {
	case int:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%d", int(v))
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

// PortAsString returns the function port value as a string (for expression handling).
func (f *FunctionV1) PortAsString() string {
	if f.Port == nil {
		return ""
	}
	switch v := f.Port.(type) {
	case int:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%d", int(v))
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

// RouteV1 represents a route in the v1 schema.
type RouteV1 struct {
	Type     string        `yaml:"type" json:"type"`
	Internal bool          `yaml:"internal,omitempty" json:"internal,omitempty"`
	Rules    []RouteRuleV1 `yaml:"rules,omitempty" json:"rules,omitempty"`

	// Simplified form
	Service  string `yaml:"service,omitempty" json:"service,omitempty"`
	Function string `yaml:"function,omitempty" json:"function,omitempty"`
}

// RouteRuleV1 represents a route rule in the v1 schema.
type RouteRuleV1 struct {
	Name        string          `yaml:"name,omitempty" json:"name,omitempty"`
	Matches     []RouteMatchV1  `yaml:"matches,omitempty" json:"matches,omitempty"`
	BackendRefs []BackendRefV1  `yaml:"backendRefs,omitempty" json:"backendRefs,omitempty"`
	Filters     []RouteFilterV1 `yaml:"filters,omitempty" json:"filters,omitempty"`
	Timeouts    *TimeoutsV1     `yaml:"timeouts,omitempty" json:"timeouts,omitempty"`
}

// RouteMatchV1 represents route match conditions in the v1 schema.
type RouteMatchV1 struct {
	Path        *PathMatchV1        `yaml:"path,omitempty" json:"path,omitempty"`
	Headers     []HeaderMatchV1     `yaml:"headers,omitempty" json:"headers,omitempty"`
	QueryParams []QueryParamMatchV1 `yaml:"queryParams,omitempty" json:"queryParams,omitempty"`
	Method      string              `yaml:"method,omitempty" json:"method,omitempty"`

	// gRPC matching (for grpc routes)
	GRPCMethod *GRPCMethodMatchV1 `yaml:"grpcMethod,omitempty" json:"grpcMethod,omitempty"`
}

// PathMatchV1 represents path matching in the v1 schema.
type PathMatchV1 struct {
	Type  string `yaml:"type" json:"type"`
	Value string `yaml:"value" json:"value"`
}

// HeaderMatchV1 represents header matching in the v1 schema.
type HeaderMatchV1 struct {
	Name  string `yaml:"name" json:"name"`
	Type  string `yaml:"type,omitempty" json:"type,omitempty"`
	Value string `yaml:"value" json:"value"`
}

// QueryParamMatchV1 represents query param matching in the v1 schema.
type QueryParamMatchV1 struct {
	Name  string `yaml:"name" json:"name"`
	Type  string `yaml:"type,omitempty" json:"type,omitempty"`
	Value string `yaml:"value" json:"value"`
}

// GRPCMethodMatchV1 represents gRPC method matching in the v1 schema.
type GRPCMethodMatchV1 struct {
	Service string `yaml:"service" json:"service"`
	Method  string `yaml:"method,omitempty" json:"method,omitempty"`
}

// BackendRefV1 represents a backend reference in the v1 schema.
type BackendRefV1 struct {
	Service  string `yaml:"service,omitempty" json:"service,omitempty"`
	Function string `yaml:"function,omitempty" json:"function,omitempty"`
	Weight   int    `yaml:"weight,omitempty" json:"weight,omitempty"`
}

// RouteFilterV1 represents a route filter in the v1 schema.
type RouteFilterV1 struct {
	Type                   string            `yaml:"type" json:"type"`
	RequestHeaderModifier  *HeaderModifierV1 `yaml:"requestHeaderModifier,omitempty" json:"requestHeaderModifier,omitempty"`
	ResponseHeaderModifier *HeaderModifierV1 `yaml:"responseHeaderModifier,omitempty" json:"responseHeaderModifier,omitempty"`
	RequestRedirect        *RedirectV1       `yaml:"requestRedirect,omitempty" json:"requestRedirect,omitempty"`
	URLRewrite             *URLRewriteV1     `yaml:"urlRewrite,omitempty" json:"urlRewrite,omitempty"`
	RequestMirror          *MirrorV1         `yaml:"requestMirror,omitempty" json:"requestMirror,omitempty"`
}

// HeaderModifierV1 represents header modification in the v1 schema.
type HeaderModifierV1 struct {
	Add    []HeaderValueV1 `yaml:"add,omitempty" json:"add,omitempty"`
	Set    []HeaderValueV1 `yaml:"set,omitempty" json:"set,omitempty"`
	Remove []string        `yaml:"remove,omitempty" json:"remove,omitempty"`
}

// HeaderValueV1 represents a header key-value in the v1 schema.
type HeaderValueV1 struct {
	Name  string `yaml:"name" json:"name"`
	Value string `yaml:"value" json:"value"`
}

// RedirectV1 represents a redirect in the v1 schema.
type RedirectV1 struct {
	Scheme     string `yaml:"scheme,omitempty" json:"scheme,omitempty"`
	Hostname   string `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	Port       int    `yaml:"port,omitempty" json:"port,omitempty"`
	StatusCode int    `yaml:"statusCode,omitempty" json:"statusCode,omitempty"`
}

// URLRewriteV1 represents URL rewriting in the v1 schema.
type URLRewriteV1 struct {
	Hostname string          `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	Path     *PathModifierV1 `yaml:"path,omitempty" json:"path,omitempty"`
}

// PathModifierV1 represents path modification in the v1 schema.
type PathModifierV1 struct {
	Type               string `yaml:"type" json:"type"`
	ReplaceFullPath    string `yaml:"replaceFullPath,omitempty" json:"replaceFullPath,omitempty"`
	ReplacePrefixMatch string `yaml:"replacePrefixMatch,omitempty" json:"replacePrefixMatch,omitempty"`
}

// MirrorV1 represents request mirroring in the v1 schema.
type MirrorV1 struct {
	Service string `yaml:"service" json:"service"`
}

// TimeoutsV1 represents timeout configuration in the v1 schema.
type TimeoutsV1 struct {
	Request        string `yaml:"request,omitempty" json:"request,omitempty"`
	BackendRequest string `yaml:"backendRequest,omitempty" json:"backendRequest,omitempty"`
}

// CronjobV1 represents a cronjob in the v1 schema.
type CronjobV1 struct {
	Image       string            `yaml:"image,omitempty" json:"image,omitempty"`
	Build       *BuildV1          `yaml:"build,omitempty" json:"build,omitempty"`
	Schedule    string            `yaml:"schedule" json:"schedule"`
	Command     []string          `yaml:"command,omitempty" json:"command,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`
	CPU         string            `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	Memory      string            `yaml:"memory,omitempty" json:"memory,omitempty"`
}

// VariableV1 represents a variable in the v1 schema.
type VariableV1 struct {
	Description string      `yaml:"description,omitempty" json:"description,omitempty"`
	Default     interface{} `yaml:"default,omitempty" json:"default,omitempty"`
	Required    bool        `yaml:"required,omitempty" json:"required,omitempty"`
	Sensitive   bool        `yaml:"sensitive,omitempty" json:"sensitive,omitempty"`
	Secret      bool        `yaml:"secret,omitempty" json:"secret,omitempty"` // Alias for sensitive
}

// OutputV1 represents an output value in the v1 schema.
// Outputs expose values to dependent components via dependencies.<name>.outputs.<output>.
type OutputV1 struct {
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Value       string `yaml:"value" json:"value"` // Expression that resolves to the output value
	Sensitive   bool   `yaml:"sensitive,omitempty" json:"sensitive,omitempty"`
}

// DependencyV1 represents a dependency on another component in the v1 schema.
// Supports both string shorthand (OCI reference) and full object form with
// source and optional fields.
type DependencyV1 struct {
	Source   string `yaml:"source" json:"source"`                         // OCI reference in repo:tag format
	Optional bool   `yaml:"optional,omitempty" json:"optional,omitempty"` // If true, dependency is not auto-deployed (default: false)
}

// UnmarshalYAML supports both string shorthand ("ghcr.io/org/app:v1") and full object form.
func (d *DependencyV1) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try string shorthand first
	var s string
	if err := unmarshal(&s); err == nil {
		d.Source = s
		return nil
	}

	// Fall back to full object form
	type rawDependency DependencyV1
	var raw rawDependency
	if err := unmarshal(&raw); err != nil {
		return fmt.Errorf("dependency must be a string (OCI reference) or an object with a source field: %w", err)
	}
	*d = DependencyV1(raw)
	return nil
}

// VolumeV1 represents a volume in the v1 schema.
type VolumeV1 struct {
	MountPath string `yaml:"mount_path" json:"mount_path"`
	HostPath  string `yaml:"host_path,omitempty" json:"host_path,omitempty"`
	Name      string `yaml:"name,omitempty" json:"name,omitempty"`
	ReadOnly  bool   `yaml:"read_only,omitempty" json:"read_only,omitempty"`
}

// ProbeV1 represents a probe in the v1 schema.
// Port and TCPPort support both integer literals and expression strings (${{ ports.*.port }}).
type ProbeV1 struct {
	Path                string      `yaml:"path,omitempty" json:"path,omitempty"`
	Port                interface{} `yaml:"-" json:"-"` // int or string; handled by custom UnmarshalYAML
	Command             []string    `yaml:"command,omitempty" json:"command,omitempty"`
	TCPPort             interface{} `yaml:"-" json:"-"` // int or string; handled by custom UnmarshalYAML
	InitialDelaySeconds int         `yaml:"initial_delay_seconds,omitempty" json:"initial_delay_seconds,omitempty"`
	PeriodSeconds       int         `yaml:"period_seconds,omitempty" json:"period_seconds,omitempty"`
	TimeoutSeconds      int         `yaml:"timeout_seconds,omitempty" json:"timeout_seconds,omitempty"`
	SuccessThreshold    int         `yaml:"success_threshold,omitempty" json:"success_threshold,omitempty"`
	FailureThreshold    int         `yaml:"failure_threshold,omitempty" json:"failure_threshold,omitempty"`
}

// UnmarshalYAML handles Port and TCPPort being either an int or a string expression.
func (p *ProbeV1) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type rawProbe struct {
		Path                string      `yaml:"path,omitempty"`
		Port                interface{} `yaml:"port,omitempty"`
		Command             []string    `yaml:"command,omitempty"`
		TCPPort             interface{} `yaml:"tcp_port,omitempty"`
		InitialDelaySeconds int         `yaml:"initial_delay_seconds,omitempty"`
		PeriodSeconds       int         `yaml:"period_seconds,omitempty"`
		TimeoutSeconds      int         `yaml:"timeout_seconds,omitempty"`
		SuccessThreshold    int         `yaml:"success_threshold,omitempty"`
		FailureThreshold    int         `yaml:"failure_threshold,omitempty"`
	}
	var raw rawProbe
	if err := unmarshal(&raw); err != nil {
		return err
	}
	p.Path = raw.Path
	p.Port = raw.Port
	p.Command = raw.Command
	p.TCPPort = raw.TCPPort
	p.InitialDelaySeconds = raw.InitialDelaySeconds
	p.PeriodSeconds = raw.PeriodSeconds
	p.TimeoutSeconds = raw.TimeoutSeconds
	p.SuccessThreshold = raw.SuccessThreshold
	p.FailureThreshold = raw.FailureThreshold
	return nil
}

// PortAsString returns the probe port value as a string (for expression handling).
func (p *ProbeV1) PortAsString() string {
	return interfaceToString(p.Port)
}

// TCPPortAsString returns the probe TCP port value as a string (for expression handling).
func (p *ProbeV1) TCPPortAsString() string {
	return interfaceToString(p.TCPPort)
}
