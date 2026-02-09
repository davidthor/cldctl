// Package v1 implements the v1 datacenter schema.
package v1

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

// SchemaV1 represents the v1 datacenter schema.
type SchemaV1 struct {
	Version     string              `hcl:"version,optional"`
	Extends     *ExtendsBlockV1     `hcl:"-"` // Parsed manually from HCL (attribute syntax)
	Variables   []VariableBlockV1   `hcl:"variable,block"`
	Modules     []ModuleBlockV1     `hcl:"module,block"`
	Components  []ComponentBlockV1  `hcl:"-"` // Parsed manually from HCL
	Environment *EnvironmentBlockV1 `hcl:"environment,block"`
}

// ExtendsBlockV1 represents the extends attribute for datacenter inheritance.
// Exactly one of Image or Path must be set.
type ExtendsBlockV1 struct {
	Image string // OCI reference for deploy-time resolution
	Path  string // Local path for build-time resolution
}

// ComponentBlockV1 represents a datacenter-level component declaration.
// These components are deployed into environments on-demand when needed as dependencies.
type ComponentBlockV1 struct {
	Name          string         `hcl:"name,label"`
	Source        string         `hcl:"source"`
	VariablesExpr hcl.Expression `hcl:"-"` // Raw variables expression for runtime evaluation
	Remain        hcl.Body       `hcl:",remain"`
}

// VariableBlockV1 represents a variable block.
type VariableBlockV1 struct {
	Name         string         `hcl:"name,label"`
	Type         string         `hcl:"type,optional"`
	Description  string         `hcl:"description,optional"`
	Default      *hcl.Attribute `hcl:"default,optional"`
	DefaultValue cty.Value      `hcl:"-"` // Evaluated default value
	Sensitive    bool           `hcl:"sensitive,optional"`
}

// ModuleBlockV1 represents a module block.
type ModuleBlockV1 struct {
	Name            string               `hcl:"name,label"`
	Build           string               `hcl:"build,optional"`
	Source          string               `hcl:"source,optional"`
	Plugin          string               `hcl:"plugin,optional"`
	InputsExpr      hcl.Expression       `hcl:"-"`             // Raw inputs expression for runtime evaluation
	InputsEvaluated map[string]cty.Value `hcl:"-"`             // Evaluated inputs
	Environment     map[string]string    `hcl:"environment,optional"`
	When            string               `hcl:"when,optional"`
	WhenExpr        hcl.Expression       `hcl:"-"`             // Raw when expression for runtime evaluation
	Volumes         []VolumeBlockV1      `hcl:"volume,block"`
	Remain          hcl.Body             `hcl:",remain"`
}

// VolumeBlockV1 represents a volume mount block.
type VolumeBlockV1 struct {
	HostPath  string `hcl:"host_path"`
	MountPath string `hcl:"mount_path"`
	ReadOnly  bool   `hcl:"read_only,optional"`
}

// EnvironmentBlockV1 represents the environment block.
type EnvironmentBlockV1 struct {
	Modules                []ModuleBlockV1 `hcl:"module,block"`
	DatabaseHooks          []HookBlockV1   `hcl:"database,block"`
	TaskHooks              []HookBlockV1   `hcl:"task,block"`
	BucketHooks            []HookBlockV1   `hcl:"bucket,block"`
	EncryptionKeyHooks     []HookBlockV1   `hcl:"encryptionKey,block"`
	SMTPHooks              []HookBlockV1   `hcl:"smtp,block"`
	DatabaseUserHooks      []HookBlockV1   `hcl:"databaseUser,block"`
	DeploymentHooks        []HookBlockV1   `hcl:"deployment,block"`
	FunctionHooks          []HookBlockV1   `hcl:"function,block"`
	ServiceHooks           []HookBlockV1   `hcl:"service,block"`
	RouteHooks             []HookBlockV1   `hcl:"route,block"`
	CronjobHooks           []HookBlockV1   `hcl:"cronjob,block"`
	SecretHooks            []HookBlockV1   `hcl:"secret,block"`
	DockerBuildHooks       []HookBlockV1   `hcl:"dockerBuild,block"`
	ObservabilityHooks     []HookBlockV1   `hcl:"observability,block"`
	PortHooks              []HookBlockV1   `hcl:"port,block"`
	Remain                 hcl.Body        `hcl:",remain"`
}

// HookBlockV1 represents a resource hook block.
type HookBlockV1 struct {
	When              string          `hcl:"when,optional"`
	WhenExpr          hcl.Expression  `hcl:"-"` // Raw when expression for runtime evaluation
	Modules           []ModuleBlockV1 `hcl:"module,block"`
	OutputsExpr       hcl.Expression  `hcl:"-"` // Raw outputs expression for runtime evaluation (attribute syntax)
	OutputsAttrs      hcl.Attributes  `hcl:"-"` // Raw outputs attributes for runtime evaluation (block syntax)
	NestedOutputExprs map[string]hcl.Expression  `hcl:"-"` // Nested output objects (e.g., read = {...}, write = {...})
	Error             string          `hcl:"error,optional"` // Human-readable error message (mutually exclusive with modules/outputs)
	ErrorExpr         hcl.Expression  `hcl:"-"`              // Raw error expression for runtime interpolation
	Remain            hcl.Body        `hcl:",remain"`
}

// OutputsBlockV1 represents the outputs block in a hook.
type OutputsBlockV1 struct {
	Attributes hcl.Attributes `hcl:",remain"`
	Body       hcl.Body       `hcl:"-"` // Raw body for runtime evaluation
}
