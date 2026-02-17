// Package visual provides reusable graph visualization utilities.
// It operates directly on *graph.Graph and has no dependency on CI workflow types,
// making it suitable for use by any feature that needs to render dependency graphs
// (e.g., CLI generate commands, playground/inspector features).
package visual

import (
	"fmt"
	"sort"
	"strings"

	"github.com/davidthor/cldctl/pkg/graph"
)

// MermaidOptions controls how a graph is rendered to a Mermaid flowchart.
type MermaidOptions struct {
	// GroupByComponent uses subgraphs to group nodes by component name.
	GroupByComponent bool

	// Direction is the flowchart direction: "TD" (top-down) or "LR" (left-right).
	// Defaults to "TD" if empty.
	Direction string

	// Title is an optional diagram title rendered as a comment.
	Title string

	// SetupJobs are additional synthetic nodes prepended to the diagram.
	// They appear before graph nodes and can declare dependencies.
	// This is useful for CI contexts where build/check jobs precede resource nodes.
	SetupJobs []SetupJob

	// SetupJobDependents maps a setup job ID to the graph node IDs that depend on it.
	// If empty, root nodes (nodes with no in-graph dependencies) depend on all setup jobs.
	SetupJobDependents map[string][]string
}

// SetupJob represents a synthetic node to prepend to the diagram.
type SetupJob struct {
	// ID is the unique identifier for this node in the diagram.
	ID string

	// Label is the human-readable label displayed in the node.
	Label string

	// DependsOn lists IDs of other setup jobs this one depends on.
	DependsOn []string
}

// ImageOptions extends MermaidOptions with image rendering settings.
type ImageOptions struct {
	MermaidOptions

	// Width is the PNG width in pixels. 0 means auto.
	Width int

	// Height is the PNG height in pixels. 0 means auto.
	Height int

	// Theme is the Mermaid theme (default, dark, forest, neutral).
	// Defaults to "default" if empty.
	Theme string
}

// RenderMermaid generates a Mermaid flowchart string from a dependency graph.
// The output can be embedded in Markdown, rendered by mermaid-cli, or displayed
// in any tool that supports Mermaid syntax.
func RenderMermaid(g *graph.Graph, opts MermaidOptions) (string, error) {
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

	var b strings.Builder

	if opts.Title != "" {
		b.WriteString(fmt.Sprintf("---\ntitle: %s\n---\n", opts.Title))
	}

	b.WriteString(fmt.Sprintf("flowchart %s\n", direction))

	// Build a set of all graph node IDs for quick lookup
	graphNodeIDs := make(map[string]bool, len(sorted))
	for _, node := range sorted {
		graphNodeIDs[node.ID] = true
	}

	// Build a mapping from graph node ID to display ID (sanitized for Mermaid)
	nodeDisplayID := make(map[string]string, len(sorted))
	for _, node := range sorted {
		nodeDisplayID[node.ID] = sanitizeMermaidID(node)
	}

	// Render setup jobs first
	setupJobIDs := make(map[string]bool, len(opts.SetupJobs))
	for _, sj := range opts.SetupJobs {
		setupJobIDs[sj.ID] = true
		b.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", sj.ID, escapeMermaidLabel(sj.Label)))
	}

	if len(opts.SetupJobs) > 0 {
		b.WriteString("\n")
	}

	// Determine root graph nodes (no in-graph dependencies)
	rootNodes := findRootNodes(sorted)

	if opts.GroupByComponent {
		renderGrouped(&b, sorted, nodeDisplayID, opts, setupJobIDs, rootNodes)
	} else {
		renderFlat(&b, sorted, nodeDisplayID, opts, setupJobIDs, rootNodes)
	}

	return b.String(), nil
}

// renderFlat renders all nodes without component subgraphs.
func renderFlat(b *strings.Builder, sorted []*graph.Node, displayIDs map[string]string, opts MermaidOptions, setupJobIDs map[string]bool, rootNodes map[string]bool) {
	// Declare nodes
	for _, node := range sorted {
		did := displayIDs[node.ID]
		label := nodeLabel(node)
		b.WriteString(fmt.Sprintf("    %s[\"%s\"]\n", did, escapeMermaidLabel(label)))
	}

	b.WriteString("\n")

	// Render setup job edges
	renderSetupEdges(b, opts, displayIDs, setupJobIDs, rootNodes)

	// Render graph edges
	renderGraphEdges(b, sorted, displayIDs)
}

// renderGrouped renders nodes grouped by component using Mermaid subgraphs.
func renderGrouped(b *strings.Builder, sorted []*graph.Node, displayIDs map[string]string, opts MermaidOptions, setupJobIDs map[string]bool, rootNodes map[string]bool) {
	// Group nodes by component
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

	// Render each component as a subgraph
	for _, comp := range componentOrder {
		nodes := componentNodes[comp]
		subgraphID := sanitizeSubgraphID(comp)
		b.WriteString(fmt.Sprintf("    subgraph %s [\"%s\"]\n", subgraphID, escapeMermaidLabel(comp)))
		for _, node := range nodes {
			did := displayIDs[node.ID]
			label := nodeLabel(node)
			b.WriteString(fmt.Sprintf("        %s[\"%s\"]\n", did, escapeMermaidLabel(label)))
		}
		b.WriteString("    end\n\n")
	}

	// Render setup job edges
	renderSetupEdges(b, opts, displayIDs, setupJobIDs, rootNodes)

	// Render graph edges
	renderGraphEdges(b, sorted, displayIDs)
}

// renderSetupEdges renders edges from setup jobs to graph nodes.
func renderSetupEdges(b *strings.Builder, opts MermaidOptions, displayIDs map[string]string, setupJobIDs map[string]bool, rootNodes map[string]bool) {
	// Edges between setup jobs
	for _, sj := range opts.SetupJobs {
		for _, dep := range sj.DependsOn {
			b.WriteString(fmt.Sprintf("    %s --> %s\n", dep, sj.ID))
		}
	}

	if len(opts.SetupJobs) == 0 {
		return
	}

	if len(opts.SetupJobDependents) > 0 {
		// Explicit mapping from setup jobs to their dependents
		for sjID, dependentIDs := range opts.SetupJobDependents {
			for _, depID := range dependentIDs {
				if did, ok := displayIDs[depID]; ok {
					b.WriteString(fmt.Sprintf("    %s --> %s\n", sjID, did))
				}
			}
		}
	} else {
		// Default: all root graph nodes depend on all setup jobs
		sortedRoots := sortedKeys(rootNodes)
		for _, sj := range opts.SetupJobs {
			for _, rootID := range sortedRoots {
				if did, ok := displayIDs[rootID]; ok {
					b.WriteString(fmt.Sprintf("    %s --> %s\n", sj.ID, did))
				}
			}
		}
	}

	if len(opts.SetupJobs) > 0 {
		b.WriteString("\n")
	}
}

// renderGraphEdges renders dependency edges between graph nodes.
func renderGraphEdges(b *strings.Builder, sorted []*graph.Node, displayIDs map[string]string) {
	for _, node := range sorted {
		did := displayIDs[node.ID]
		// Sort dependencies for deterministic output
		deps := make([]string, len(node.DependsOn))
		copy(deps, node.DependsOn)
		sort.Strings(deps)

		for _, depID := range deps {
			if depDID, ok := displayIDs[depID]; ok {
				b.WriteString(fmt.Sprintf("    %s --> %s\n", depDID, did))
			}
		}
	}
}

// findRootNodes returns node IDs that have no in-graph dependencies.
func findRootNodes(sorted []*graph.Node) map[string]bool {
	// Build the set of all node IDs in this graph
	allIDs := make(map[string]bool, len(sorted))
	for _, n := range sorted {
		allIDs[n.ID] = true
	}

	roots := make(map[string]bool)
	for _, n := range sorted {
		isRoot := true
		for _, dep := range n.DependsOn {
			if allIDs[dep] {
				isRoot = false
				break
			}
		}
		if isRoot {
			roots[n.ID] = true
		}
	}
	return roots
}

// sanitizeMermaidID creates a Mermaid-safe node identifier from a graph node.
// Graph node IDs are like "component/type/name" — we convert slashes to dashes.
func sanitizeMermaidID(node *graph.Node) string {
	return strings.ReplaceAll(node.ID, "/", "--")
}

// sanitizeSubgraphID creates a safe subgraph identifier from a component name.
func sanitizeSubgraphID(component string) string {
	r := strings.NewReplacer("/", "_", "-", "_", ".", "_", " ", "_")
	return "sg_" + r.Replace(component)
}

// nodeLabel creates a human-readable label for a graph node.
// Format: "type/name" (without the component prefix).
func nodeLabel(node *graph.Node) string {
	return fmt.Sprintf("%s/%s", node.Type, node.Name)
}

// escapeMermaidLabel escapes characters that have special meaning in Mermaid labels.
func escapeMermaidLabel(s string) string {
	s = strings.ReplaceAll(s, `"`, `#quot;`)
	return s
}

// sortedKeys returns sorted keys from a map.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
