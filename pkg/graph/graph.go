package graph

import (
	"fmt"
	"sort"
)

// Graph represents a dependency graph of resources.
type Graph struct {
	// All nodes in the graph
	Nodes map[string]*Node

	// Environment name
	Environment string

	// Datacenter name
	Datacenter string
}

// NewGraph creates a new empty graph.
func NewGraph(environment, datacenter string) *Graph {
	return &Graph{
		Nodes:       make(map[string]*Node),
		Environment: environment,
		Datacenter:  datacenter,
	}
}

// AddNode adds a node to the graph.
func (g *Graph) AddNode(node *Node) error {
	if _, exists := g.Nodes[node.ID]; exists {
		return fmt.Errorf("node %s already exists", node.ID)
	}
	g.Nodes[node.ID] = node
	return nil
}

// GetNode returns a node by ID.
func (g *Graph) GetNode(id string) *Node {
	return g.Nodes[id]
}

// AddEdge adds a dependency edge from dependent to dependency.
func (g *Graph) AddEdge(dependentID, dependencyID string) error {
	dependent := g.GetNode(dependentID)
	if dependent == nil {
		return fmt.Errorf("dependent node %s not found", dependentID)
	}

	dependency := g.GetNode(dependencyID)
	if dependency == nil {
		return fmt.Errorf("dependency node %s not found", dependencyID)
	}

	dependent.AddDependency(dependencyID)
	dependency.AddDependent(dependentID)

	return nil
}

// TopologicalSort returns nodes in topological order (dependencies first).
func (g *Graph) TopologicalSort() ([]*Node, error) {
	// Kahn's algorithm
	inDegree := make(map[string]int)
	for id := range g.Nodes {
		inDegree[id] = 0
	}

	for _, node := range g.Nodes {
		for _, depID := range node.DependsOn {
			inDegree[node.ID]++
			_ = depID // dependency contributes to dependent's in-degree
		}
	}

	// Recount properly
	for id := range g.Nodes {
		inDegree[id] = len(g.Nodes[id].DependsOn)
	}

	// Start with nodes that have no dependencies
	var queue []string
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	// Sort queue for deterministic order
	sort.Strings(queue)

	var result []*Node
	for len(queue) > 0 {
		// Pop first element
		nodeID := queue[0]
		queue = queue[1:]

		node := g.Nodes[nodeID]
		result = append(result, node)

		// Reduce in-degree of dependents
		for _, dependentID := range node.DependedOnBy {
			inDegree[dependentID]--
			if inDegree[dependentID] == 0 {
				queue = append(queue, dependentID)
				// Re-sort for determinism
				sort.Strings(queue)
			}
		}
	}

	// Check for cycles
	if len(result) != len(g.Nodes) {
		return nil, fmt.Errorf("dependency cycle detected")
	}

	return result, nil
}

// ReverseTopologicalSort returns nodes in reverse order (dependents first).
func (g *Graph) ReverseTopologicalSort() ([]*Node, error) {
	sorted, err := g.TopologicalSort()
	if err != nil {
		return nil, err
	}

	// Reverse the slice
	for i, j := 0, len(sorted)-1; i < j; i, j = i+1, j-1 {
		sorted[i], sorted[j] = sorted[j], sorted[i]
	}

	return sorted, nil
}

// GetReadyNodes returns all nodes that are ready to execute.
func (g *Graph) GetReadyNodes() []*Node {
	var ready []*Node
	for _, node := range g.Nodes {
		if node.IsReady(g) {
			ready = append(ready, node)
		}
	}
	return ready
}

// GetNodesByType returns all nodes of a specific type.
func (g *Graph) GetNodesByType(nodeType NodeType) []*Node {
	var nodes []*Node
	for _, node := range g.Nodes {
		if node.Type == nodeType {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

// GetNodesByComponent returns all nodes belonging to a component.
func (g *Graph) GetNodesByComponent(component string) []*Node {
	var nodes []*Node
	for _, node := range g.Nodes {
		if node.Component == component {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

// AllCompleted returns true if all nodes are completed.
func (g *Graph) AllCompleted() bool {
	for _, node := range g.Nodes {
		if node.State != NodeStateCompleted && node.State != NodeStateSkipped {
			return false
		}
	}
	return true
}

// HasFailed returns true if any node has failed.
func (g *Graph) HasFailed() bool {
	for _, node := range g.Nodes {
		if node.State == NodeStateFailed {
			return true
		}
	}
	return false
}
