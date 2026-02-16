// Package native implements a native IaC plugin for Docker and process execution.
package native

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Module represents a native module definition.
type Module struct {
	Plugin    string               `yaml:"plugin"` // Must be "native"
	Type      string               `yaml:"type"`   // Primary resource type hint
	Inputs    map[string]InputDef  `yaml:"inputs"`
	Resources map[string]Resource  `yaml:"resources"`
	Outputs   map[string]OutputDef `yaml:"outputs"`

	// ResourceOrder preserves the YAML declaration order of resources.
	// The Apply loop iterates in this order so that sequentially-declared
	// resources execute top-to-bottom (matching author expectations).
	ResourceOrder []string `yaml:"-"`
}

// InputDef defines a module input.
type InputDef struct {
	Type        string      `yaml:"type"`
	Required    bool        `yaml:"required"`
	Default     interface{} `yaml:"default"`
	Description string      `yaml:"description"`
	Sensitive   bool        `yaml:"sensitive"`
}

// Resource defines a native resource.
type Resource struct {
	Type       string                 `yaml:"type"`
	When       string                 `yaml:"when,omitempty"`
	Properties map[string]interface{} `yaml:"properties"`
	DependsOn  []string               `yaml:"depends_on"`
	Destroy    *DestroyCommand        `yaml:"destroy,omitempty"`
}

// DestroyCommand defines a command to run when a resource is destroyed.
// The command is resolved at apply time (expressions like ${inputs.*} are
// evaluated) and persisted in state so it's available during teardown.
type DestroyCommand struct {
	Command     []interface{}          `yaml:"command"`               // Command to execute
	Image       string                 `yaml:"image,omitempty"`       // If set, run in a Docker container
	Network     string                 `yaml:"network,omitempty"`     // Docker network (when using image)
	WorkDir     string                 `yaml:"working_dir,omitempty"` // Working directory
	Environment map[string]interface{} `yaml:"environment,omitempty"` // Environment variables
}

// OutputDef defines a module output.
type OutputDef struct {
	Value       string `yaml:"value"` // Expression to evaluate
	Description string `yaml:"description"`
	Sensitive   bool   `yaml:"sensitive"`
}

// State represents the persisted state of native resources.
type State struct {
	ModulePath string                    `json:"module_path"`
	Inputs     map[string]interface{}    `json:"inputs"`
	Resources  map[string]*ResourceState `json:"resources"`
	Outputs    map[string]interface{}    `json:"outputs"`
}

// ResourceState represents a single resource's state.
type ResourceState struct {
	Type       string                 `json:"type"`
	ID         interface{}            `json:"id"`
	Properties map[string]interface{} `json:"properties"`
	Outputs    map[string]interface{} `json:"outputs"`
	// DestroyCmd holds a resolved destroy command to execute during teardown.
	// Nil if the resource has no custom destroy behaviour.
	DestroyCmd *ResolvedDestroyCommand `json:"destroy_cmd,omitempty"`
}

// ResolvedDestroyCommand is the state-persisted form of a destroy command
// with all expressions already evaluated.
type ResolvedDestroyCommand struct {
	Command     []string          `json:"command"`
	Image       string            `json:"image,omitempty"`
	Network     string            `json:"network,omitempty"`
	WorkDir     string            `json:"working_dir,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
}

// LoadModule loads a native module definition from a path.
func LoadModule(path string) (*Module, error) {
	// Try module.yml first, then module.yaml
	files := []string{
		path + "/module.yml",
		path + "/module.yaml",
		path,
	}

	var data []byte
	var err error
	for _, f := range files {
		data, err = os.ReadFile(f)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read module: %w", err)
	}

	var module Module
	if err := yaml.Unmarshal(data, &module); err != nil {
		return nil, fmt.Errorf("failed to parse module: %w", err)
	}

	if module.Plugin != "" && module.Plugin != "native" {
		return nil, fmt.Errorf("invalid plugin type: expected 'native', got '%s'", module.Plugin)
	}

	// Extract resource key ordering from the raw YAML tree.
	// Go maps don't preserve insertion order, so we parse the YAML node
	// tree to discover the declaration order of resource keys.
	module.ResourceOrder = extractResourceOrder(data)

	return &module, nil
}

// extractResourceOrder parses raw YAML bytes and returns the resource keys
// in their declaration order. Falls back to an empty slice on parse errors.
func extractResourceOrder(data []byte) []string {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil || root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil
	}
	mapping := root.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return nil
	}

	// Find the "resources" key in the top-level mapping
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		keyNode := mapping.Content[i]
		valNode := mapping.Content[i+1]
		if keyNode.Value == "resources" && valNode.Kind == yaml.MappingNode {
			order := make([]string, 0, len(valNode.Content)/2)
			for j := 0; j < len(valNode.Content)-1; j += 2 {
				order = append(order, valNode.Content[j].Value)
			}
			return order
		}
	}
	return nil
}
