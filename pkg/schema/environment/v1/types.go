// Package v1 implements the v1 environment schema.
package v1

// SchemaV1 represents the v1 environment schema.
type SchemaV1 struct {
	Version string `yaml:"version,omitempty" json:"version,omitempty"`

	// Optional environment name. When provided, used as the default environment
	// name by commands like `cldctl up -e`. Can be overridden with --name flag.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Environment-level variables resolved from env vars, dotenv files, or defaults
	Variables map[string]EnvironmentVariableV1 `yaml:"variables,omitempty" json:"variables,omitempty"`

	// Reusable values
	Locals map[string]interface{} `yaml:"locals,omitempty" json:"locals,omitempty"`

	// Component configurations
	Components map[string]ComponentConfigV1 `yaml:"components,omitempty" json:"components,omitempty"`
}

// EnvironmentVariableV1 represents a variable declaration in the v1 environment schema.
// Variables are resolved from (highest priority first): CLI --var flags, OS environment
// variables, dotenv file chain, then default values.
type EnvironmentVariableV1 struct {
	// Description of the variable's purpose
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Default value if not provided via env var or CLI
	Default interface{} `yaml:"default,omitempty" json:"default,omitempty"`

	// Whether a value is required (error if missing and no default)
	Required bool `yaml:"required,omitempty" json:"required,omitempty"`

	// Whether the value is sensitive (masked in output)
	Sensitive bool `yaml:"sensitive,omitempty" json:"sensitive,omitempty"`

	// Explicit OS environment variable name to read from.
	// If not set, defaults to UPPER_SNAKE_CASE of the variable name.
	Env string `yaml:"env,omitempty" json:"env,omitempty"`
}

// ComponentConfigV1 represents a component configuration in v1 schema.
// Exactly one of Path or Image must be set (at top level or within each instance).
type ComponentConfigV1 struct {
	// Path is a local file path to the component directory or file (e.g., "./path/to/component").
	// Mutually exclusive with Image.
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// Image is an OCI registry reference for the component (e.g., "ghcr.io/org/my-app:v1.0.0").
	// Mutually exclusive with Path.
	Image string `yaml:"image,omitempty" json:"image,omitempty"`

	// Variable values
	Variables map[string]interface{} `yaml:"variables,omitempty" json:"variables,omitempty"`

	// Port overrides per port name (maps port name to specific port number)
	Ports map[string]int `yaml:"ports,omitempty" json:"ports,omitempty"`

	// Scaling configuration per deployment
	Scaling map[string]ScalingConfigV1 `yaml:"scaling,omitempty" json:"scaling,omitempty"`

	// Function configuration per function
	Functions map[string]FunctionConfigV1 `yaml:"functions,omitempty" json:"functions,omitempty"`

	// Environment variable overrides per deployment
	Environment map[string]map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`

	// Route configuration per route
	Routes map[string]RouteConfigV1 `yaml:"routes,omitempty" json:"routes,omitempty"`

	// Instances defines weighted component instances for progressive delivery.
	// First element = newest instance (shared resources derive inputs from it).
	Instances []InstanceConfigV1 `yaml:"instances,omitempty" json:"instances,omitempty"`

	// Distinct lists resource patterns that should be per-instance instead of shared.
	// Format: "resourceType.resourceName" (e.g., "encryptionKey.signing").
	Distinct []string `yaml:"distinct,omitempty" json:"distinct,omitempty"`
}

// InstanceConfigV1 represents a weighted component instance in v1 schema.
type InstanceConfigV1 struct {
	// Name is the instance identifier (e.g., "canary", "stable").
	Name string `yaml:"name" json:"name"`

	// Source is the component image/path for this instance.
	Source string `yaml:"source" json:"source"`

	// Weight is the traffic weight (0-100) for this instance.
	Weight int `yaml:"weight" json:"weight"`

	// Variables are optional variable overrides for this instance.
	Variables map[string]interface{} `yaml:"variables,omitempty" json:"variables,omitempty"`
}

// ScalingConfigV1 represents scaling configuration in v1 schema.
type ScalingConfigV1 struct {
	Replicas    int    `yaml:"replicas,omitempty" json:"replicas,omitempty"`
	CPU         string `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	Memory      string `yaml:"memory,omitempty" json:"memory,omitempty"`
	MinReplicas int    `yaml:"min_replicas,omitempty" json:"min_replicas,omitempty"`
	MaxReplicas int    `yaml:"max_replicas,omitempty" json:"max_replicas,omitempty"`
}

// FunctionConfigV1 represents function configuration in v1 schema.
type FunctionConfigV1 struct {
	Regions []string `yaml:"regions,omitempty" json:"regions,omitempty"`
	Memory  string   `yaml:"memory,omitempty" json:"memory,omitempty"`
	Timeout int      `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// RouteConfigV1 represents route configuration in v1 schema.
type RouteConfigV1 struct {
	Hostnames []HostnameV1  `yaml:"hostnames,omitempty" json:"hostnames,omitempty"`
	TLS       *TLSConfigV1  `yaml:"tls,omitempty" json:"tls,omitempty"`
}

// HostnameV1 represents a hostname in v1 schema.
type HostnameV1 struct {
	Subdomain string `yaml:"subdomain,omitempty" json:"subdomain,omitempty"`
	Host      string `yaml:"host,omitempty" json:"host,omitempty"`
}

// TLSConfigV1 represents TLS configuration in v1 schema.
type TLSConfigV1 struct {
	Enabled    bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	SecretName string `yaml:"secretName,omitempty" json:"secretName,omitempty"`
}
