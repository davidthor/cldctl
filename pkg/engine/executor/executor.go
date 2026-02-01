// Package executor runs execution plans.
package executor

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/architect-io/arcctl/pkg/engine/planner"
	"github.com/architect-io/arcctl/pkg/graph"
	"github.com/architect-io/arcctl/pkg/iac"
	"github.com/architect-io/arcctl/pkg/state"
	"github.com/architect-io/arcctl/pkg/state/types"
)

// ExecutionResult contains the results of an execution.
type ExecutionResult struct {
	Success   bool
	Duration  time.Duration
	Created   int
	Updated   int
	Deleted   int
	Failed    int
	Errors    []error
	NodeResults map[string]*NodeResult
}

// NodeResult contains the result of executing a single node.
type NodeResult struct {
	NodeID   string
	Action   planner.Action
	Success  bool
	Duration time.Duration
	Error    error
	Outputs  map[string]interface{}
}

// Options configures the executor.
type Options struct {
	// Parallelism is the max number of concurrent operations
	Parallelism int

	// Output writer for progress
	Output io.Writer

	// DryRun skips actual execution
	DryRun bool

	// StopOnError stops execution on first error
	StopOnError bool
}

// DefaultOptions returns default executor options.
func DefaultOptions() Options {
	return Options{
		Parallelism: 10,
		StopOnError: true,
	}
}

// Executor runs execution plans.
type Executor struct {
	stateManager state.Manager
	iacRegistry  *iac.Registry
	options      Options
}

// NewExecutor creates a new executor.
func NewExecutor(stateManager state.Manager, iacRegistry *iac.Registry, options Options) *Executor {
	if options.Parallelism <= 0 {
		options.Parallelism = 10
	}
	return &Executor{
		stateManager: stateManager,
		iacRegistry:  iacRegistry,
		options:      options,
	}
}

// Execute runs an execution plan.
func (e *Executor) Execute(ctx context.Context, plan *planner.Plan, g *graph.Graph) (*ExecutionResult, error) {
	startTime := time.Now()

	result := &ExecutionResult{
		Success:     true,
		NodeResults: make(map[string]*NodeResult),
	}

	if plan.IsEmpty() {
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Get or create environment state
	envState, err := e.stateManager.GetEnvironment(ctx, plan.Environment)
	if err != nil {
		// Create new state if it doesn't exist
		envState = &types.EnvironmentState{
			Name:       plan.Environment,
			Datacenter: plan.Datacenter,
			Components: make(map[string]*types.ComponentState),
			Status:     types.EnvironmentStatusProvisioning,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
	}

	// Execute changes in order
	for _, change := range plan.Changes {
		if ctx.Err() != nil {
			result.Success = false
			result.Errors = append(result.Errors, ctx.Err())
			break
		}

		// Check if dependencies are satisfied
		if change.Node != nil && !e.areDependenciesSatisfied(change.Node, g, result) {
			nodeResult := &NodeResult{
				NodeID:  change.Node.ID,
				Action:  change.Action,
				Success: false,
				Error:   fmt.Errorf("dependencies not satisfied"),
			}
			result.NodeResults[change.Node.ID] = nodeResult
			result.Failed++
			result.Success = false

			if e.options.StopOnError {
				break
			}
			continue
		}

		nodeResult := e.executeChange(ctx, change, envState)
		if change.Node != nil {
			result.NodeResults[change.Node.ID] = nodeResult
		}

		switch change.Action {
		case planner.ActionCreate:
			if nodeResult.Success {
				result.Created++
			} else {
				result.Failed++
			}
		case planner.ActionUpdate, planner.ActionReplace:
			if nodeResult.Success {
				result.Updated++
			} else {
				result.Failed++
			}
		case planner.ActionDelete:
			if nodeResult.Success {
				result.Deleted++
			} else {
				result.Failed++
			}
		}

		if !nodeResult.Success {
			result.Success = false
			result.Errors = append(result.Errors, nodeResult.Error)

			if e.options.StopOnError {
				break
			}
		}

		// Update node in graph with outputs
		if nodeResult.Success && change.Node != nil && nodeResult.Outputs != nil {
			change.Node.Outputs = nodeResult.Outputs
			change.Node.State = graph.NodeStateCompleted
		} else if change.Node != nil {
			change.Node.State = graph.NodeStateFailed
		}
	}

	// Update environment status
	if result.Success {
		envState.Status = types.EnvironmentStatusReady
	} else {
		envState.Status = types.EnvironmentStatusFailed
	}
	envState.UpdatedAt = time.Now()

	// Save state
	if err := e.stateManager.SaveEnvironment(ctx, envState); err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("failed to save state: %w", err))
	}

	result.Duration = time.Since(startTime)
	return result, nil
}

func (e *Executor) areDependenciesSatisfied(node *graph.Node, g *graph.Graph, result *ExecutionResult) bool {
	for _, depID := range node.DependsOn {
		depResult, exists := result.NodeResults[depID]
		if !exists {
			return false
		}
		if !depResult.Success {
			return false
		}
	}
	return true
}

func (e *Executor) executeChange(ctx context.Context, change *planner.ResourceChange, envState *types.EnvironmentState) *NodeResult {
	startTime := time.Now()

	result := &NodeResult{
		Action: change.Action,
	}

	if change.Node != nil {
		result.NodeID = change.Node.ID
	}

	// Handle dry run
	if e.options.DryRun {
		result.Success = true
		result.Duration = time.Since(startTime)
		return result
	}

	switch change.Action {
	case planner.ActionCreate, planner.ActionUpdate, planner.ActionReplace:
		result = e.executeApply(ctx, change, envState)
	case planner.ActionDelete:
		result = e.executeDestroy(ctx, change, envState)
	case planner.ActionNoop:
		result.Success = true
	}

	result.Duration = time.Since(startTime)
	return result
}

func (e *Executor) executeApply(ctx context.Context, change *planner.ResourceChange, envState *types.EnvironmentState) *NodeResult {
	result := &NodeResult{
		NodeID: change.Node.ID,
		Action: change.Action,
	}

	// Get or create component state
	compState := envState.Components[change.Node.Component]
	if compState == nil {
		compState = &types.ComponentState{
			Name:      change.Node.Component,
			Resources: make(map[string]*types.ResourceState),
			Status:    types.ResourceStatusProvisioning,
			UpdatedAt: time.Now(),
		}
		envState.Components[change.Node.Component] = compState
	}

	// Get IaC plugin (default to native for now)
	plugin, err := e.iacRegistry.Get("native")
	if err != nil {
		result.Error = fmt.Errorf("failed to get IaC plugin: %w", err)
		result.Success = false
		return result
	}

	// Build run options
	runOpts := iac.RunOptions{
		ModulePath:  string(change.Node.Type),
		Inputs:      change.Node.Inputs,
		Environment: map[string]string{},
	}

	// Add outputs from dependencies
	// (In a real implementation, we'd resolve the dependency outputs here)

	// Execute
	applyResult, err := plugin.Apply(ctx, runOpts)
	if err != nil {
		result.Error = fmt.Errorf("apply failed: %w", err)
		result.Success = false

		// Update resource state to failed
		compState.Resources[change.Node.Name] = &types.ResourceState{
			Component: change.Node.Component,
			Name:      change.Node.Name,
			Type:      string(change.Node.Type),
			Status:    types.ResourceStatusFailed,
			Inputs:    change.Node.Inputs,
			UpdatedAt: time.Now(),
		}

		return result
	}

	// Extract outputs
	outputs := make(map[string]interface{})
	for name, out := range applyResult.Outputs {
		outputs[name] = out.Value
	}
	result.Outputs = outputs
	result.Success = true

	// Update resource state
	compState.Resources[change.Node.Name] = &types.ResourceState{
		Component: change.Node.Component,
		Name:      change.Node.Name,
		Type:      string(change.Node.Type),
		Status:    types.ResourceStatusReady,
		Inputs:    change.Node.Inputs,
		Outputs:   outputs,
		UpdatedAt: time.Now(),
	}

	return result
}

func (e *Executor) executeDestroy(ctx context.Context, change *planner.ResourceChange, envState *types.EnvironmentState) *NodeResult {
	result := &NodeResult{
		Action: change.Action,
	}

	if change.Node != nil {
		result.NodeID = change.Node.ID
	}

	// Get component state
	compState := envState.Components[change.Node.Component]
	if compState == nil {
		// Nothing to destroy
		result.Success = true
		return result
	}

	// Get IaC plugin
	plugin, err := e.iacRegistry.Get("native")
	if err != nil {
		result.Error = fmt.Errorf("failed to get IaC plugin: %w", err)
		result.Success = false
		return result
	}

	// Build run options
	runOpts := iac.RunOptions{
		ModulePath: string(change.Node.Type),
		Inputs:     change.Node.Inputs,
	}

	// Execute destroy
	if err := plugin.Destroy(ctx, runOpts); err != nil {
		result.Error = fmt.Errorf("destroy failed: %w", err)
		result.Success = false
		return result
	}

	result.Success = true

	// Remove resource from state
	delete(compState.Resources, change.Node.Name)

	// If component has no more resources, remove it
	if len(compState.Resources) == 0 {
		delete(envState.Components, change.Node.Component)
	}

	return result
}

// ExecuteParallel executes independent operations in parallel.
func (e *Executor) ExecuteParallel(ctx context.Context, plan *planner.Plan, g *graph.Graph) (*ExecutionResult, error) {
	startTime := time.Now()

	result := &ExecutionResult{
		Success:     true,
		NodeResults: make(map[string]*NodeResult),
	}

	if plan.IsEmpty() {
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Get or create environment state
	envState, err := e.stateManager.GetEnvironment(ctx, plan.Environment)
	if err != nil {
		envState = &types.EnvironmentState{
			Name:       plan.Environment,
			Datacenter: plan.Datacenter,
			Components: make(map[string]*types.ComponentState),
			Status:     types.EnvironmentStatusProvisioning,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
	}

	// Create work queue
	var mu sync.Mutex
	sem := make(chan struct{}, e.options.Parallelism)
	var wg sync.WaitGroup

	// Index changes by node ID
	changeByNode := make(map[string]*planner.ResourceChange)
	for _, change := range plan.Changes {
		if change.Node != nil {
			changeByNode[change.Node.ID] = change
		}
	}

	// Process nodes as they become ready
	pending := make(map[string]*planner.ResourceChange)
	for _, change := range plan.Changes {
		if change.Node != nil {
			pending[change.Node.ID] = change
		}
	}

	completed := make(map[string]bool)
	failed := make(map[string]bool)

	for len(pending) > 0 {
		// Find ready nodes
		var ready []*planner.ResourceChange
		for id, change := range pending {
			if change.Node == nil {
				continue
			}

			isReady := true
			for _, depID := range change.Node.DependsOn {
				if failed[depID] {
					// Dependency failed, skip this node
					mu.Lock()
					result.NodeResults[id] = &NodeResult{
						NodeID:  id,
						Action:  change.Action,
						Success: false,
						Error:   fmt.Errorf("dependency %s failed", depID),
					}
					result.Failed++
					result.Success = false
					mu.Unlock()
					delete(pending, id)
					failed[id] = true
					isReady = false
					break
				}
				if !completed[depID] {
					isReady = false
					break
				}
			}

			if isReady {
				ready = append(ready, change)
			}
		}

		if len(ready) == 0 && len(pending) > 0 {
			// No ready nodes but still pending - deadlock or all failed
			break
		}

		// Execute ready nodes in parallel
		for _, change := range ready {
			delete(pending, change.Node.ID)

			wg.Add(1)
			sem <- struct{}{}

			go func(c *planner.ResourceChange) {
				defer wg.Done()
				defer func() { <-sem }()

				nodeResult := e.executeChange(ctx, c, envState)

				mu.Lock()
				result.NodeResults[c.Node.ID] = nodeResult

				switch c.Action {
				case planner.ActionCreate:
					if nodeResult.Success {
						result.Created++
					} else {
						result.Failed++
					}
				case planner.ActionUpdate, planner.ActionReplace:
					if nodeResult.Success {
						result.Updated++
					} else {
						result.Failed++
					}
				case planner.ActionDelete:
					if nodeResult.Success {
						result.Deleted++
					} else {
						result.Failed++
					}
				}

				if nodeResult.Success {
					completed[c.Node.ID] = true
					c.Node.Outputs = nodeResult.Outputs
					c.Node.State = graph.NodeStateCompleted
				} else {
					failed[c.Node.ID] = true
					result.Success = false
					result.Errors = append(result.Errors, nodeResult.Error)
					c.Node.State = graph.NodeStateFailed
				}
				mu.Unlock()
			}(change)
		}

		wg.Wait()
	}

	// Update environment status
	if result.Success {
		envState.Status = types.EnvironmentStatusReady
	} else {
		envState.Status = types.EnvironmentStatusFailed
	}
	envState.UpdatedAt = time.Now()

	// Save state
	if err := e.stateManager.SaveEnvironment(ctx, envState); err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("failed to save state: %w", err))
	}

	result.Duration = time.Since(startTime)
	return result, nil
}
