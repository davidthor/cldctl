package visual

import (
	"fmt"
	"sort"
	"strings"

	"github.com/davidthor/cldctl/pkg/graph"
)

// HookMatch describes how a datacenter hook matched a resource node and
// which IaC modules it would instantiate.
type HookMatch struct {
	// NodeID is the graph node ID (e.g., "my-app/database/main").
	NodeID string

	// NodeType is the resource type (e.g., "database").
	NodeType string

	// NodeName is the resource name (e.g., "main").
	NodeName string

	// Component is the owning component name.
	Component string

	// HookIndex is the ordinal position of the matched hook (0-based).
	HookIndex int

	// Modules lists the module names from the matched hook.
	Modules []string

	// IsError indicates the matched hook is an error hook that rejects the resource.
	IsError bool

	// Error is the human-readable error message (only set when IsError is true).
	Error string
}

// InfrastructureOptions controls how the two-layer infrastructure diagram is rendered.
type InfrastructureOptions struct {
	// Title is an optional diagram title.
	Title string

	// GroupByComponent groups resource nodes by component using subgraphs.
	GroupByComponent bool

	// Direction is the flowchart direction ("TD" or "LR"). Defaults to "TD".
	Direction string
}

// RenderInfrastructureMermaid generates a two-layer Mermaid flowchart showing
// application resources on top and the infrastructure modules they trigger below.
// Edges connect each resource to the modules its matched hook would create.
func RenderInfrastructureMermaid(g *graph.Graph, matches []HookMatch, opts InfrastructureOptions) (string, error) {
	if g == nil {
		return "", fmt.Errorf("graph is nil")
	}

	direction := opts.Direction
	if direction == "" {
		direction = "TD"
	}

	sorted, err := g.TopologicalSort()
	if err != nil {
		return "", fmt.Errorf("failed to sort graph: %w", err)
	}

	// Index matches by node ID for fast lookup
	matchByNode := make(map[string]*HookMatch, len(matches))
	for i := range matches {
		matchByNode[matches[i].NodeID] = &matches[i]
	}

	// Collect unique modules across all matches (deduplicated)
	type moduleEntry struct {
		name    string
		sources []string // node IDs that trigger this module
	}
	moduleMap := make(map[string]*moduleEntry)
	var moduleOrder []string

	for _, node := range sorted {
		m, ok := matchByNode[node.ID]
		if !ok || m.IsError {
			continue
		}
		for _, modName := range m.Modules {
			// Module IDs are scoped to the resource to allow the same module name
			// to appear in different hooks (e.g., "postgres" in two database hooks).
			modKey := infraModuleKey(node, modName)
			if _, exists := moduleMap[modKey]; !exists {
				moduleMap[modKey] = &moduleEntry{name: modName}
				moduleOrder = append(moduleOrder, modKey)
			}
			moduleMap[modKey].sources = append(moduleMap[modKey].sources, node.ID)
		}
	}

	var b strings.Builder

	if opts.Title != "" {
		b.WriteString(fmt.Sprintf("---\ntitle: %s\n---\n", opts.Title))
	}

	b.WriteString(fmt.Sprintf("flowchart %s\n", direction))

	// --- Resource layer ---
	b.WriteString("    subgraph resources [\"Application Resources\"]\n")

	if opts.GroupByComponent {
		renderInfraResourcesGrouped(&b, sorted, matchByNode)
	} else {
		renderInfraResourcesFlat(&b, sorted, matchByNode)
	}

	b.WriteString("    end\n\n")

	// --- Module layer ---
	if len(moduleOrder) > 0 {
		b.WriteString("    subgraph modules [\"Infrastructure Modules\"]\n")
		for _, modKey := range moduleOrder {
			entry := moduleMap[modKey]
			modDisplayID := infraSanitizeID("mod_" + modKey)
			b.WriteString(fmt.Sprintf("        %s[\"%s\"]\n", modDisplayID, escapeMermaidLabel(entry.name)))
		}
		b.WriteString("    end\n\n")
	}

	// --- Error nodes (outside both subgraphs) ---
	hasErrors := false
	for _, node := range sorted {
		m, ok := matchByNode[node.ID]
		if !ok || !m.IsError {
			continue
		}
		if !hasErrors {
			hasErrors = true
		}
		errID := infraSanitizeID("err_" + node.ID)
		resID := infraSanitizeID("res_" + node.ID)
		errLabel := fmt.Sprintf("ERROR: %s", m.Error)
		b.WriteString(fmt.Sprintf("    %s[\"%s\"]:::errorNode\n", errID, escapeMermaidLabel(errLabel)))
		b.WriteString(fmt.Sprintf("    %s -.-x %s\n", resID, errID))
	}

	if hasErrors {
		b.WriteString("\n")
	}

	// --- Resource-to-module edges ---
	for _, node := range sorted {
		m, ok := matchByNode[node.ID]
		if !ok || m.IsError {
			continue
		}
		resID := infraSanitizeID("res_" + node.ID)
		for _, modName := range m.Modules {
			modKey := infraModuleKey(node, modName)
			modDisplayID := infraSanitizeID("mod_" + modKey)
			b.WriteString(fmt.Sprintf("    %s --> %s\n", resID, modDisplayID))
		}
	}

	return b.String(), nil
}

// renderInfraResourcesFlat renders resource nodes without component grouping.
func renderInfraResourcesFlat(b *strings.Builder, sorted []*graph.Node, matches map[string]*HookMatch) {
	for _, node := range sorted {
		resID := infraSanitizeID("res_" + node.ID)
		label := infraResourceLabel(node, matches)
		b.WriteString(fmt.Sprintf("        %s[\"%s\"]\n", resID, escapeMermaidLabel(label)))
	}
}

// renderInfraResourcesGrouped renders resource nodes grouped by component.
func renderInfraResourcesGrouped(b *strings.Builder, sorted []*graph.Node, matches map[string]*HookMatch) {
	componentNodes := make(map[string][]*graph.Node)
	var componentOrder []string
	seen := make(map[string]bool)
	for _, node := range sorted {
		comp := node.Component
		if !seen[comp] {
			seen[comp] = true
			componentOrder = append(componentOrder, comp)
		}
		componentNodes[comp] = append(componentNodes[comp], node)
	}

	for _, comp := range componentOrder {
		nodes := componentNodes[comp]
		subID := infraSanitizeID("res_sg_" + comp)
		b.WriteString(fmt.Sprintf("        subgraph %s [\"%s\"]\n", subID, escapeMermaidLabel(comp)))
		for _, node := range nodes {
			resID := infraSanitizeID("res_" + node.ID)
			label := infraResourceLabel(node, matches)
			b.WriteString(fmt.Sprintf("            %s[\"%s\"]\n", resID, escapeMermaidLabel(label)))
		}
		b.WriteString("        end\n")
	}
}

// infraResourceLabel builds a label for a resource node, including type info
// from inputs when available (e.g., "database/main (postgres:16)").
func infraResourceLabel(node *graph.Node, matches map[string]*HookMatch) string {
	base := fmt.Sprintf("%s/%s", node.Type, node.Name)
	if node.Type == graph.NodeTypeDatabase {
		if t, ok := node.Inputs["type"]; ok && t != "" {
			return fmt.Sprintf("%s (%v)", base, t)
		}
	}
	return base
}

// infraModuleKey creates a unique key for a module instance scoped to its resource node.
func infraModuleKey(node *graph.Node, moduleName string) string {
	return fmt.Sprintf("%s/%s/%s/%s", node.Component, string(node.Type), node.Name, moduleName)
}

// infraSanitizeID makes a string safe for use as a Mermaid node ID.
func infraSanitizeID(s string) string {
	r := strings.NewReplacer("/", "_", "-", "_", ".", "_", " ", "_", ":", "_")
	return r.Replace(s)
}

// BuildHookMatchSummary returns a structured summary of hook matches for use
// by the WASM module or other callers that need the raw mapping data.
func BuildHookMatchSummary(matches []HookMatch) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(matches))
	for _, m := range matches {
		entry := map[string]interface{}{
			"nodeId":    m.NodeID,
			"nodeType":  m.NodeType,
			"nodeName":  m.NodeName,
			"component": m.Component,
			"hookIndex": m.HookIndex,
		}
		if m.IsError {
			entry["isError"] = true
			entry["error"] = m.Error
		} else {
			mods := make([]string, len(m.Modules))
			copy(mods, m.Modules)
			sort.Strings(mods)
			entry["modules"] = mods
		}
		result = append(result, entry)
	}
	return result
}
