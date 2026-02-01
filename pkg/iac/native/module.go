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
	Properties map[string]interface{} `yaml:"properties"`
	DependsOn  []string               `yaml:"depends_on"`
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

	return &module, nil
}
