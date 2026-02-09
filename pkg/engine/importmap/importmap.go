// Package importmap provides types and parsing for cldctl import mapping files.
//
// Mapping files describe how existing cloud resources map to IaC module-internal
// addresses so that `cldctl import` can adopt them into state.
package importmap

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ResourceMapping maps an IaC-internal resource address to a cloud resource ID.
type ResourceMapping struct {
	// Address is the IaC resource address (e.g., "aws_db_instance.main")
	Address string `yaml:"address"`

	// ID is the real cloud resource ID (e.g., "mydb-instance-123")
	ID string `yaml:"id"`
}

// ComponentMapping describes all resource mappings for a single component.
type ComponentMapping struct {
	// Resources maps resource keys (e.g., "database.main") to their IaC mappings
	Resources map[string][]ResourceMapping `yaml:"resources"`
}

// EnvironmentComponentMapping extends ComponentMapping with source and variables.
type EnvironmentComponentMapping struct {
	// Source is the OCI reference or path for the component artifact
	Source string `yaml:"source"`

	// Variables for the component
	Variables map[string]string `yaml:"variables,omitempty"`

	// Resources maps resource keys (e.g., "database.main") to their IaC mappings
	Resources map[string][]ResourceMapping `yaml:"resources"`
}

// EnvironmentMapping describes all component and resource mappings for an
// environment-level import.
type EnvironmentMapping struct {
	// Components maps component names to their import configuration
	Components map[string]EnvironmentComponentMapping `yaml:"components"`
}

// DatacenterMapping describes import mappings for datacenter root-level modules.
// Used by `deploy datacenter --import-file` to import existing infrastructure
// atomically during the first deploy.
type DatacenterMapping struct {
	// Modules maps module names to their IaC resource mappings
	Modules map[string][]ResourceMapping `yaml:"modules"`
}

// ParseDatacenterMapping reads and parses a datacenter-level import mapping file.
func ParseDatacenterMapping(path string) (*DatacenterMapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read mapping file: %w", err)
	}

	var mapping DatacenterMapping
	if err := yaml.Unmarshal(data, &mapping); err != nil {
		return nil, fmt.Errorf("failed to parse mapping file: %w", err)
	}

	if len(mapping.Modules) == 0 {
		return nil, fmt.Errorf("mapping file contains no module mappings")
	}

	for modName, mappings := range mapping.Modules {
		for i, m := range mappings {
			if m.Address == "" {
				return nil, fmt.Errorf("module %s mapping[%d]: address is required", modName, i)
			}
			if m.ID == "" {
				return nil, fmt.Errorf("module %s mapping[%d]: id is required", modName, i)
			}
		}
	}

	return &mapping, nil
}

// ParseComponentMapping reads and parses a component-level mapping file.
func ParseComponentMapping(path string) (*ComponentMapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read mapping file: %w", err)
	}

	var mapping ComponentMapping
	if err := yaml.Unmarshal(data, &mapping); err != nil {
		return nil, fmt.Errorf("failed to parse mapping file: %w", err)
	}

	// Validate
	if len(mapping.Resources) == 0 {
		return nil, fmt.Errorf("mapping file contains no resource mappings")
	}

	for key, mappings := range mapping.Resources {
		for i, m := range mappings {
			if m.Address == "" {
				return nil, fmt.Errorf("resource %s mapping[%d]: address is required", key, i)
			}
			if m.ID == "" {
				return nil, fmt.Errorf("resource %s mapping[%d]: id is required", key, i)
			}
		}
	}

	return &mapping, nil
}

// ParseEnvironmentMapping reads and parses an environment-level mapping file.
func ParseEnvironmentMapping(path string) (*EnvironmentMapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read mapping file: %w", err)
	}

	var mapping EnvironmentMapping
	if err := yaml.Unmarshal(data, &mapping); err != nil {
		return nil, fmt.Errorf("failed to parse mapping file: %w", err)
	}

	// Validate
	if len(mapping.Components) == 0 {
		return nil, fmt.Errorf("mapping file contains no component mappings")
	}

	for compName, comp := range mapping.Components {
		if comp.Source == "" {
			return nil, fmt.Errorf("component %s: source is required", compName)
		}
		for key, mappings := range comp.Resources {
			for i, m := range mappings {
				if m.Address == "" {
					return nil, fmt.Errorf("component %s resource %s mapping[%d]: address is required", compName, key, i)
				}
				if m.ID == "" {
					return nil, fmt.Errorf("component %s resource %s mapping[%d]: id is required", compName, key, i)
				}
			}
		}
	}

	return &mapping, nil
}

// ParseMapFlags parses --map flag values into ResourceMappings.
// Each flag value is in "address=id" format.
func ParseMapFlags(flags []string) ([]ResourceMapping, error) {
	var mappings []ResourceMapping
	for _, flag := range flags {
		// Split on first '=' only (IDs may contain '=')
		idx := -1
		for i, c := range flag {
			if c == '=' {
				idx = i
				break
			}
		}
		if idx <= 0 {
			return nil, fmt.Errorf("invalid --map value %q: expected format address=id", flag)
		}

		address := flag[:idx]
		id := flag[idx+1:]
		if id == "" {
			return nil, fmt.Errorf("invalid --map value %q: id is empty", flag)
		}

		mappings = append(mappings, ResourceMapping{
			Address: address,
			ID:      id,
		})
	}
	return mappings, nil
}
