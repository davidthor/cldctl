package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/davidthor/cldctl/pkg/state/types"
	"gopkg.in/yaml.v3"
)

// inspectEnvironmentState displays the state of an environment.
func inspectEnvironmentState(env *types.EnvironmentState, dc, outputFormat string) error {
	switch outputFormat {
	case "json":
		return marshalJSON(env)
	case "yaml":
		return marshalYAML(env)
	default:
		return printEnvironmentStateTable(env, dc)
	}
}

func printEnvironmentStateTable(env *types.EnvironmentState, dc string) error {
	fmt.Printf("Environment: %s\n", env.Name)
	fmt.Printf("Datacenter:  %s\n", dc)
	fmt.Printf("Status:      %s\n", env.Status)
	fmt.Printf("Created:     %s\n", env.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Updated:     %s\n", env.UpdatedAt.Format("2006-01-02 15:04:05"))

	if env.StatusReason != "" {
		fmt.Printf("Reason:      %s\n", env.StatusReason)
	}

	if len(env.Variables) > 0 {
		fmt.Println()
		fmt.Println("Variables:")
		for _, key := range sortedStringMapKeys(env.Variables) {
			fmt.Printf("  %-24s = %s\n", key, env.Variables[key])
		}
	}

	if len(env.Components) > 0 {
		fmt.Println()
		fmt.Println("Components:")
		fmt.Printf("  %-20s %-40s %-12s %s\n", "NAME", "SOURCE", "STATUS", "RESOURCES")
		for _, name := range sortedComponentMapKeys(env.Components) {
			comp := env.Components[name]
			fmt.Printf("  %-20s %-40s %-12s %d\n",
				name,
				truncateString(comp.Source, 40),
				comp.Status,
				len(comp.Resources),
			)
		}
	}

	// Collect and display URLs from routes
	type routeURL struct {
		component, route, url string
	}
	var urls []routeURL
	for compName, comp := range env.Components {
		for _, res := range comp.Resources {
			if res.Type == "route" {
				if url, ok := res.Outputs["url"].(string); ok {
					urls = append(urls, routeURL{compName, res.Name, url})
				}
			}
		}
	}
	if len(urls) > 0 {
		sort.Slice(urls, func(i, j int) bool {
			if urls[i].component != urls[j].component {
				return urls[i].component < urls[j].component
			}
			return urls[i].route < urls[j].route
		})
		fmt.Println()
		fmt.Println("URLs:")
		for _, u := range urls {
			fmt.Printf("  %s/%s: %s\n", u.component, u.route, u.url)
		}
	}

	fmt.Println()
	return nil
}

// inspectComponentState displays the state of a component.
func inspectComponentState(comp *types.ComponentState, dc, envName, outputFormat string) error {
	switch outputFormat {
	case "json":
		return marshalJSON(comp)
	case "yaml":
		return marshalYAML(comp)
	default:
		return printComponentStateTable(comp, dc, envName)
	}
}

func printComponentStateTable(comp *types.ComponentState, dc, envName string) error {
	fmt.Printf("Component:   %s\n", comp.Name)
	fmt.Printf("Environment: %s\n", envName)
	fmt.Printf("Datacenter:  %s\n", dc)
	fmt.Printf("Source:      %s\n", comp.Source)
	fmt.Printf("Status:      %s\n", comp.Status)
	fmt.Printf("Deployed:    %s\n", comp.DeployedAt.Format("2006-01-02 15:04:05"))

	if comp.StatusReason != "" {
		fmt.Printf("Reason:      %s\n", comp.StatusReason)
	}

	if len(comp.Variables) > 0 {
		fmt.Println()
		fmt.Println("Variables:")
		for _, key := range sortedStringMapKeys(comp.Variables) {
			fmt.Printf("  %-24s = %s\n", key, comp.Variables[key])
		}
	}

	if len(comp.Dependencies) > 0 {
		fmt.Println()
		fmt.Println("Dependencies:")
		for _, dep := range comp.Dependencies {
			fmt.Printf("  - %s\n", dep)
		}
	}

	if len(comp.Resources) > 0 {
		// Sort resources by type then name
		type resEntry struct {
			res *types.ResourceState
		}
		var entries []resEntry
		for _, res := range comp.Resources {
			entries = append(entries, resEntry{res})
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].res.Type != entries[j].res.Type {
				return entries[i].res.Type < entries[j].res.Type
			}
			return entries[i].res.Name < entries[j].res.Name
		})

		fmt.Println()
		fmt.Println("Resources:")
		fmt.Printf("  %-16s %-20s %-12s %s\n", "TYPE", "NAME", "STATUS", "DETAILS")
		for _, e := range entries {
			details := resourceSummary(e.res)
			fmt.Printf("  %-16s %-20s %-12s %s\n",
				e.res.Type,
				e.res.Name,
				e.res.Status,
				details,
			)
		}
	}

	fmt.Println()
	return nil
}

// inspectResourceState displays the state of a single resource.
func inspectResourceState(res *types.ResourceState, dc, envName, outputFormat string) error {
	switch outputFormat {
	case "json":
		return marshalJSON(res)
	case "yaml":
		return marshalYAML(res)
	default:
		return printResourceStateTable(res, dc, envName)
	}
}

func printResourceStateTable(res *types.ResourceState, dc, envName string) error {
	fmt.Printf("Resource:    %s\n", res.Name)
	fmt.Printf("Type:        %s\n", res.Type)
	fmt.Printf("Component:   %s\n", res.Component)
	fmt.Printf("Environment: %s\n", envName)
	fmt.Printf("Datacenter:  %s\n", dc)
	fmt.Printf("Status:      %s\n", res.Status)

	if res.StatusReason != "" {
		fmt.Printf("Reason:      %s\n", res.StatusReason)
	}

	if res.Hook != "" {
		fmt.Printf("Hook:        %s\n", res.Hook)
	}
	if res.Module != "" {
		fmt.Printf("Module:      %s\n", res.Module)
	}

	fmt.Printf("Created:     %s\n", res.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Updated:     %s\n", res.UpdatedAt.Format("2006-01-02 15:04:05"))

	// Separate environment variables from other inputs
	envVars := extractEnvVars(res.Inputs)
	otherInputs := extractNonEnvInputs(res.Inputs)

	if len(otherInputs) > 0 {
		fmt.Println()
		fmt.Println("Inputs:")
		for _, key := range sortedInterfaceMapKeys(otherInputs) {
			fmt.Printf("  %-24s %s\n", key+":", formatInputValue(otherInputs[key]))
		}
	}

	if len(envVars) > 0 {
		fmt.Println()
		fmt.Println("Environment Variables:")
		keys := sortedStringMapKeys(envVars)
		maxKeyLen := 0
		for _, key := range keys {
			if len(key) > maxKeyLen {
				maxKeyLen = len(key)
			}
		}
		for _, key := range keys {
			fmt.Printf("  %-*s = %s\n", maxKeyLen, key, envVars[key])
		}
	}

	if len(res.Outputs) > 0 {
		fmt.Println()
		fmt.Println("Outputs:")
		for _, key := range sortedInterfaceMapKeys(res.Outputs) {
			fmt.Printf("  %-24s %s\n", key+":", formatInputValue(res.Outputs[key]))
		}
	}

	fmt.Println()
	return nil
}

// findResource locates a resource within a component's resource map.
// If resourceType is empty, it matches by name only. If resourceType is set,
// it matches by both type and name.
func findResource(resources map[string]*types.ResourceState, name, resourceType string) (*types.ResourceState, error) {
	if len(resources) == 0 {
		return nil, fmt.Errorf("component has no resources")
	}

	// If type is specified, match by both type and name
	if resourceType != "" {
		for _, res := range resources {
			if res.Type == resourceType && res.Name == name {
				return res, nil
			}
		}

		// Not found with type qualifier - list available
		var available []string
		for _, res := range resources {
			available = append(available, res.Type+"/"+res.Name)
		}
		sort.Strings(available)
		return nil, fmt.Errorf("resource %s/%s not found\n\nAvailable resources:\n  %s",
			resourceType, name, strings.Join(available, "\n  "))
	}

	// Try exact map key match first
	if res, ok := resources[name]; ok {
		return res, nil
	}

	// Try matching by ResourceState.Name
	var matches []*types.ResourceState
	for _, res := range resources {
		if res.Name == name {
			matches = append(matches, res)
		}
	}

	if len(matches) == 1 {
		return matches[0], nil
	}

	if len(matches) > 1 {
		// Ambiguous - multiple types share the same name
		var qualifiedNames []string
		for _, m := range matches {
			qualifiedNames = append(qualifiedNames, m.Type+"/"+m.Name)
		}
		sort.Strings(qualifiedNames)
		return nil, fmt.Errorf("ambiguous resource %q matches multiple types; qualify with type:\n  %s",
			name, strings.Join(qualifiedNames, "\n  "))
	}

	// No match at all - list available resources
	var available []string
	for _, res := range resources {
		available = append(available, res.Name+" ("+res.Type+")")
	}
	sort.Strings(available)
	return nil, fmt.Errorf("resource %q not found\n\nAvailable resources:\n  %s",
		name, strings.Join(available, "\n  "))
}

// resourceSummary returns a brief detail string for a resource (used in component table view).
func resourceSummary(res *types.ResourceState) string {
	// For failed resources, show the failure reason
	if res.Status == types.ResourceStatusFailed && res.StatusReason != "" {
		return res.StatusReason
	}

	if res.Outputs == nil {
		return ""
	}
	if url, ok := res.Outputs["url"].(string); ok {
		return url
	}
	if host, ok := res.Outputs["host"].(string); ok {
		if port, ok := res.Outputs["port"]; ok {
			return fmt.Sprintf("%s:%v", host, port)
		}
		return host
	}
	return ""
}

// extractEnvVars pulls the "environment" key from resource inputs as a flat string map.
func extractEnvVars(inputs map[string]interface{}) map[string]string {
	result := make(map[string]string)
	env, ok := inputs["environment"]
	if !ok {
		return result
	}

	switch e := env.(type) {
	case map[string]interface{}:
		for k, v := range e {
			result[k] = fmt.Sprintf("%v", v)
		}
	case map[string]string:
		for k, v := range e {
			result[k] = v
		}
	}

	return result
}

// extractNonEnvInputs returns all inputs except "environment" and binary fields.
func extractNonEnvInputs(inputs map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range inputs {
		if k == "environment" || k == "iac_state" {
			continue
		}
		result[k] = v
	}
	return result
}

// formatInputValue formats an input value for display.
func formatInputValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case []interface{}:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = fmt.Sprintf("%v", item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]interface{}:
		data, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(data)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// marshalJSON outputs a value as indented JSON.
func marshalJSON(v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// marshalYAML outputs a value as YAML.
func marshalYAML(v interface{}) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}
	fmt.Print(string(data))
	return nil
}

// sortedStringMapKeys returns the keys of a map[string]string in sorted order.
func sortedStringMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedComponentMapKeys returns the keys of a ComponentState map in sorted order.
func sortedComponentMapKeys(m map[string]*types.ComponentState) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedInterfaceMapKeys returns the keys of a map[string]interface{} in sorted order.
func sortedInterfaceMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
