// Package graph provides dependency graph construction and traversal for cldctl.
package graph

import (
	"fmt"
)

// NodeType identifies the type of a graph node.
type NodeType string

const (
	NodeTypeDatabase      NodeType = "database"
	NodeTypeBucket        NodeType = "bucket"
	NodeTypeEncryptionKey NodeType = "encryptionKey"
	NodeTypeSMTP          NodeType = "smtp"
	NodeTypeDeployment    NodeType = "deployment"
	NodeTypeFunction      NodeType = "function"
	NodeTypeService       NodeType = "service"
	NodeTypeRoute         NodeType = "route"
	NodeTypeCronjob       NodeType = "cronjob"
	NodeTypeSecret        NodeType = "secret"
	NodeTypeDockerBuild   NodeType = "dockerBuild"
	NodeTypeTask          NodeType = "task"
	NodeTypeObservability  NodeType = "observability"
	NodeTypePort           NodeType = "port"
	NodeTypeDatabaseUser   NodeType = "databaseUser"
	NodeTypeNetworkPolicy  NodeType = "networkPolicy"
)

// NodeInstance holds instance context for per-instance nodes in progressive delivery.
type NodeInstance struct {
	// Name is the instance identifier (e.g., "canary", "stable", "default").
	Name string

	// Weight is the traffic weight (0-100) for this instance.
	Weight int
}

// Node represents a resource in the dependency graph.
type Node struct {
	// Unique identifier within the graph
	ID string

	// Type of resource
	Type NodeType

	// Component this node belongs to
	Component string

	// Name of the resource within the component
	Name string

	// Input values for this node
	Inputs map[string]interface{}

	// Outputs produced by this node (populated after execution)
	Outputs map[string]interface{}

	// Dependencies - IDs of nodes this node depends on
	DependsOn []string

	// Dependents - IDs of nodes that depend on this node
	DependedOnBy []string

	// State tracking
	State NodeState

	// Instance holds the instance context for per-instance nodes.
	// Nil for shared nodes in multi-instance mode and for single-instance mode.
	Instance *NodeInstance

	// Instances holds all instance metadata for shared nodes (e.g., routes).
	// This lets shared hooks see all instances and their weights for traffic splitting.
	Instances []NodeInstance
}

// NodeState tracks the execution state of a node.
type NodeState string

const (
	NodeStatePending   NodeState = "pending"
	NodeStateRunning   NodeState = "running"
	NodeStateCompleted NodeState = "completed"
	NodeStateFailed    NodeState = "failed"
	NodeStateSkipped   NodeState = "skipped"
)

// NewNode creates a new graph node.
func NewNode(nodeType NodeType, component, name string) *Node {
	return &Node{
		ID:           fmt.Sprintf("%s/%s/%s", component, nodeType, name),
		Type:         nodeType,
		Component:    component,
		Name:         name,
		Inputs:       make(map[string]interface{}),
		Outputs:      make(map[string]interface{}),
		DependsOn:    []string{},
		DependedOnBy: []string{},
		State:        NodeStatePending,
	}
}

// NewInstanceNode creates a node qualified by an instance name.
// The node ID includes the instance: component/instance/type/name.
func NewInstanceNode(nodeType NodeType, component, instanceName string, weight int, name string) *Node {
	return &Node{
		ID:        fmt.Sprintf("%s/%s/%s/%s", component, instanceName, nodeType, name),
		Type:      nodeType,
		Component: component,
		Name:      name,
		Inputs:    make(map[string]interface{}),
		Outputs:   make(map[string]interface{}),
		DependsOn: []string{},
		DependedOnBy: []string{},
		State:     NodeStatePending,
		Instance:  &NodeInstance{Name: instanceName, Weight: weight},
	}
}

// IsPerInstanceType returns true if the given node type is per-instance by default.
func IsPerInstanceType(t NodeType) bool {
	switch t {
	case NodeTypeDeployment, NodeTypeFunction, NodeTypeService, NodeTypeCronjob, NodeTypeDockerBuild, NodeTypePort:
		return true
	default:
		return false
	}
}

// IsWorkloadType returns true if the given node type is a workload that can consume
// databases and services (deployment, function, cronjob, task).
func IsWorkloadType(t NodeType) bool {
	switch t {
	case NodeTypeDeployment, NodeTypeFunction, NodeTypeCronjob, NodeTypeTask:
		return true
	default:
		return false
	}
}

// IsSharedType returns true if the given node type is shared by default.
func IsSharedType(t NodeType) bool {
	return !IsPerInstanceType(t)
}

// AddDependency adds a dependency to this node.
func (n *Node) AddDependency(nodeID string) {
	for _, dep := range n.DependsOn {
		if dep == nodeID {
			return // Already exists
		}
	}
	n.DependsOn = append(n.DependsOn, nodeID)
}

// AddDependent adds a dependent to this node.
func (n *Node) AddDependent(nodeID string) {
	for _, dep := range n.DependedOnBy {
		if dep == nodeID {
			return // Already exists
		}
	}
	n.DependedOnBy = append(n.DependedOnBy, nodeID)
}

// SetInput sets an input value.
func (n *Node) SetInput(key string, value interface{}) {
	n.Inputs[key] = value
}

// SetOutput sets an output value.
func (n *Node) SetOutput(key string, value interface{}) {
	n.Outputs[key] = value
}

// IsReady returns true if all dependencies are completed.
func (n *Node) IsReady(graph *Graph) bool {
	if n.State != NodeStatePending {
		return false
	}

	for _, depID := range n.DependsOn {
		dep := graph.GetNode(depID)
		if dep == nil || dep.State != NodeStateCompleted {
			return false
		}
	}

	return true
}
