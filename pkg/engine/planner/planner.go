// Package planner generates execution plans from dependency graphs.
package planner

import (
	"fmt"

	"github.com/davidthor/cldctl/pkg/graph"
	"github.com/davidthor/cldctl/pkg/state/types"
)

// Action represents the type of operation to perform.
type Action string

const (
	ActionCreate  Action = "create"
	ActionUpdate  Action = "update"
	ActionReplace Action = "replace"
	ActionDelete  Action = "delete"
	ActionNoop    Action = "noop"
)

// ResourceChange describes a planned change to a resource.
type ResourceChange struct {
	// Node being changed
	Node *graph.Node

	// Action to take
	Action Action

	// Current state (nil if creating)
	CurrentState *types.ResourceState

	// Reason for the change
	Reason string

	// Property changes (for updates)
	PropertyChanges []PropertyChange
}

// PropertyChange describes a change to a property.
type PropertyChange struct {
	Path     string
	OldValue interface{}
	NewValue interface{}
}

// Plan represents an execution plan.
type Plan struct {
	// Environment being modified
	Environment string

	// Datacenter being used
	Datacenter string

	// Changes to make, in execution order
	Changes []*ResourceChange

	// Summary
	ToCreate int
	ToUpdate int
	ToDelete int
	NoChange int
}

// IsEmpty returns true if there are no changes.
func (p *Plan) IsEmpty() bool {
	return p.ToCreate == 0 && p.ToUpdate == 0 && p.ToDelete == 0
}

// PlanOptions configures planning behavior.
type PlanOptions struct {
	// ForceUpdate converts Noop actions to Update, used when datacenter config
	// changes and all resources need re-evaluation against new hooks.
	ForceUpdate bool
}

// Planner generates execution plans.
type Planner struct {
	options PlanOptions
}

// NewPlanner creates a new planner.
func NewPlanner() *Planner {
	return &Planner{}
}

// NewPlannerWithOptions creates a new planner with options.
func NewPlannerWithOptions(opts PlanOptions) *Planner {
	return &Planner{options: opts}
}

// Plan creates an execution plan by comparing desired state (graph) with current state.
func (p *Planner) Plan(g *graph.Graph, currentState *types.EnvironmentState) (*Plan, error) {
	plan := &Plan{
		Environment: g.Environment,
		Datacenter:  g.Datacenter,
	}

	// Get nodes in topological order
	sortedNodes, err := g.TopologicalSort()
	if err != nil {
		return nil, err
	}

	// Track which resources exist in current state
	existingResources := make(map[string]*types.ResourceState)
	if currentState != nil {
		for compName, compState := range currentState.Components {
			for resName, resState := range compState.Resources {
				key := compName + "/" + resName
				existingResources[key] = resState
			}
		}
	}

	// Plan changes for each node
	processedIDs := make(map[string]bool)
	for _, node := range sortedNodes {
		change := p.planNodeChange(node, existingResources)
		plan.Changes = append(plan.Changes, change)
		processedIDs[node.ID] = true

		switch change.Action {
		case ActionCreate:
			plan.ToCreate++
		case ActionUpdate, ActionReplace:
			plan.ToUpdate++
		case ActionDelete:
			plan.ToDelete++
		case ActionNoop:
			plan.NoChange++
		}
	}

	// Plan deletions for resources that exist but aren't in the graph
	for key, resState := range existingResources {
		if !processedIDs[key] {
			change := &ResourceChange{
				Node: &graph.Node{
					ID:        key,
					Component: resState.Component,
					Name:      resState.Name,
				},
				Action:       ActionDelete,
				CurrentState: resState,
				Reason:       "resource no longer defined",
			}
			plan.Changes = append(plan.Changes, change)
			plan.ToDelete++
		}
	}

	return plan, nil
}

// PlanDestroy creates a plan to destroy all resources.
func (p *Planner) PlanDestroy(g *graph.Graph, currentState *types.EnvironmentState) (*Plan, error) {
	plan := &Plan{
		Environment: g.Environment,
		Datacenter:  g.Datacenter,
	}

	// Get nodes in reverse topological order (dependents first)
	sortedNodes, err := g.ReverseTopologicalSort()
	if err != nil {
		return nil, err
	}

	for _, node := range sortedNodes {
		// Find current state for this resource
		var currentResState *types.ResourceState
		if currentState != nil {
			if compState, ok := currentState.Components[node.Component]; ok {
				currentResState = compState.Resources[node.Name]
			}
		}

		if currentResState != nil {
			change := &ResourceChange{
				Node:         node,
				Action:       ActionDelete,
				CurrentState: currentResState,
				Reason:       "destroying environment",
			}
			plan.Changes = append(plan.Changes, change)
			plan.ToDelete++
		}
	}

	return plan, nil
}

func (p *Planner) planNodeChange(node *graph.Node, existingResources map[string]*types.ResourceState) *ResourceChange {
	// Look for existing resource
	existingKey := node.Component + "/" + string(node.Type) + "/" + node.Name
	existing := existingResources[existingKey]

	// Also check with just the node ID
	if existing == nil {
		existing = existingResources[node.ID]
	}

	change := &ResourceChange{
		Node:         node,
		CurrentState: existing,
	}

	if existing == nil {
		// New resource
		change.Action = ActionCreate
		change.Reason = "resource does not exist"
		return change
	}

	// Compare inputs to detect changes
	changes := p.compareInputs(node.Inputs, existing.Inputs)
	if len(changes) > 0 {
		change.Action = ActionUpdate
		change.PropertyChanges = changes
		change.Reason = "resource configuration changed"
		return change
	}

	// No input changes detected, but ForceUpdate converts noop to update
	if p.options.ForceUpdate {
		change.Action = ActionUpdate
		change.Reason = "force update (datacenter configuration changed)"
		return change
	}

	// No changes needed
	change.Action = ActionNoop
	change.Reason = "resource is up to date"
	return change
}

func (p *Planner) compareInputs(desired, current map[string]interface{}) []PropertyChange {
	var changes []PropertyChange

	// Check for new or changed values
	for key, desiredVal := range desired {
		currentVal, exists := current[key]
		if !exists {
			changes = append(changes, PropertyChange{
				Path:     key,
				OldValue: nil,
				NewValue: desiredVal,
			})
		} else if !deepEqual(desiredVal, currentVal) {
			changes = append(changes, PropertyChange{
				Path:     key,
				OldValue: currentVal,
				NewValue: desiredVal,
			})
		}
	}

	// Check for removed values
	for key, currentVal := range current {
		if _, exists := desired[key]; !exists {
			changes = append(changes, PropertyChange{
				Path:     key,
				OldValue: currentVal,
				NewValue: nil,
			})
		}
	}

	return changes
}

// deepEqual compares two values for equality.
func deepEqual(a, b interface{}) bool {
	// Simple equality check - in production would use reflect.DeepEqual
	// but need to handle special cases
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// For slices, maps, compare string representations
	// This is simplified - production code would do proper deep comparison
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// FormatChanges formats property changes as a string.
func FormatChanges(changes []PropertyChange) string {
	if len(changes) == 0 {
		return "no changes"
	}

	result := ""
	for _, c := range changes {
		result += fmt.Sprintf("  %s: %v -> %v\n", c.Path, c.OldValue, c.NewValue)
	}
	return result
}
