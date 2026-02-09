// Package types defines the data structures for cldctl state.
package types

import (
	"time"
)

// DatacenterState represents the state of a datacenter.
type DatacenterState struct {
	// Metadata
	Name        string    `json:"name"`
	Version     string    `json:"version"`              // Tag/reference (e.g., "my-dc:latest", "ghcr.io/org/dc:v1")
	Source      string    `json:"source,omitempty"`      // Original source (filesystem path or OCI reference)
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Configuration
	Variables map[string]string `json:"variables,omitempty"`

	// Module states (datacenter-level modules)
	Modules map[string]*ModuleState `json:"modules,omitempty"`
}

// DatacenterComponentConfig represents a component declared at the datacenter level.
// Each declaration is stored as its own state file under datacenters/<dc>/components/.
// It stores the source and variable expressions so the engine can deploy the component
// into environments when needed as a dependency.
type DatacenterComponentConfig struct {
	Name      string            `json:"name"`                // Component registry address (e.g., "myorg/stripe")
	Source    string            `json:"source"`              // Version tag or file path
	Variables map[string]string `json:"variables,omitempty"` // HCL expression strings (evaluated at runtime)
}

// EnvironmentState represents the state of an environment.
type EnvironmentState struct {
	// Metadata
	Name        string    `json:"name"`
	Datacenter  string    `json:"datacenter"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Status
	Status       EnvironmentStatus `json:"status"`
	StatusReason string            `json:"status_reason,omitempty"`

	// Configuration from environment file
	Variables map[string]string `json:"variables,omitempty"`

	// Deployed components
	Components map[string]*ComponentState `json:"components,omitempty"`

	// Environment-level module states
	Modules map[string]*ModuleState `json:"modules,omitempty"`
}

// EnvironmentStatus represents the status of an environment.
type EnvironmentStatus string

const (
	EnvironmentStatusPending      EnvironmentStatus = "pending"
	EnvironmentStatusProvisioning EnvironmentStatus = "provisioning"
	EnvironmentStatusReady        EnvironmentStatus = "ready"
	EnvironmentStatusFailed       EnvironmentStatus = "failed"
	EnvironmentStatusDestroying   EnvironmentStatus = "destroying"
)

// ComponentState represents a deployed component's state.
type ComponentState struct {
	// Metadata
	Name        string    `json:"name"`
	Version     string    `json:"version"`     // OCI image tag or "local"
	Source      string    `json:"source"`      // OCI reference or local path
	DeployedAt  time.Time `json:"deployed_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Status
	Status       ResourceStatus `json:"status"`
	StatusReason string         `json:"status_reason,omitempty"`

	// Variables used for this deployment
	Variables map[string]string `json:"variables,omitempty"`

	// Dependencies lists the names of other components this component depends on.
	// Populated at deploy time from the component schema's dependency declarations.
	Dependencies []string `json:"dependencies,omitempty"`

	// Resource states (shared resources in multi-instance mode)
	Resources map[string]*ResourceState `json:"resources,omitempty"`

	// Instances maps instance names to their state for progressive delivery.
	// When nil, the component is in single-instance mode.
	// Per-instance resources live under InstanceState.Resources.
	Instances map[string]*InstanceState `json:"instances,omitempty"`
}

// InstanceState represents the state of a single weighted component instance.
type InstanceState struct {
	// Name is the instance identifier (e.g., "canary", "stable").
	Name string `json:"name"`

	// Source is the component image/path for this instance.
	Source string `json:"source"`

	// Weight is the traffic weight (0-100).
	Weight int `json:"weight"`

	// Variables are the variable overrides for this instance.
	Variables map[string]string `json:"variables,omitempty"`

	// DeployedAt records when this instance was created.
	DeployedAt time.Time `json:"deployed_at"`

	// Resources contains per-instance resource states.
	Resources map[string]*ResourceState `json:"resources,omitempty"`
}

// ResourceState represents a single resource's state.
type ResourceState struct {
	// Metadata
	Name       string    `json:"name"`
	Type       string    `json:"type"`       // database, bucket, deployment, function, service, route, cronjob
	Component  string    `json:"component"`  // Parent component name
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`

	// Hook/module that created this resource
	Hook   string `json:"hook,omitempty"`   // Hook type that created this resource
	Module string `json:"module,omitempty"` // Module name within hook

	// Resource inputs (normalized from component)
	Inputs map[string]interface{} `json:"inputs,omitempty"`

	// Resource outputs (from hook execution)
	Outputs map[string]interface{} `json:"outputs,omitempty"`

	// IaC state (serialized state from the plugin) - used for single-module hooks
	IaCState []byte `json:"iac_state,omitempty"`

	// Per-module IaC states for multi-module hooks.
	// Maps module name to its ModuleState (inputs, outputs, IaC state).
	// When a hook has multiple modules, each module's state is tracked separately
	// so they can be destroyed independently.
	ModuleStates map[string]*ModuleState `json:"module_states,omitempty"`

	// Status
	Status       ResourceStatus `json:"status"`
	StatusReason string         `json:"status_reason,omitempty"`
}

// ResourceStatus represents the status of a resource.
type ResourceStatus string

const (
	ResourceStatusPending     ResourceStatus = "pending"
	ResourceStatusProvisioning ResourceStatus = "provisioning"
	ResourceStatusReady       ResourceStatus = "ready"
	ResourceStatusFailed      ResourceStatus = "failed"
	ResourceStatusDeleting    ResourceStatus = "deleting"
	ResourceStatusDeleted     ResourceStatus = "deleted"
)

// ModuleState represents the state of an IaC module execution.
type ModuleState struct {
	// Metadata
	Name      string    `json:"name"`
	Plugin    string    `json:"plugin"`    // pulumi, opentofu, native
	Source    string    `json:"source"`    // OCI reference or local path
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Inputs used for this execution
	Inputs map[string]interface{} `json:"inputs,omitempty"`

	// Outputs from the module
	Outputs map[string]interface{} `json:"outputs,omitempty"`

	// IaC state (serialized state from the plugin)
	IaCState []byte `json:"iac_state,omitempty"`

	// Status
	Status       ModuleStatus `json:"status"`
	StatusReason string       `json:"status_reason,omitempty"`
}

// ModuleStatus represents the status of a module.
type ModuleStatus string

const (
	ModuleStatusPending  ModuleStatus = "pending"
	ModuleStatusApplying ModuleStatus = "applying"
	ModuleStatusReady    ModuleStatus = "ready"
	ModuleStatusFailed   ModuleStatus = "failed"
)

// EnvironmentRef is a lightweight reference to an environment.
type EnvironmentRef struct {
	Name       string    `json:"name"`
	Datacenter string    `json:"datacenter"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
