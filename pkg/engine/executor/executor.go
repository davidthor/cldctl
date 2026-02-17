// Package executor runs execution plans.
package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/davidthor/cldctl/pkg/engine/planner"
	arcerrors "github.com/davidthor/cldctl/pkg/errors"
	"github.com/davidthor/cldctl/pkg/graph"
	"github.com/davidthor/cldctl/pkg/iac"
	"github.com/davidthor/cldctl/pkg/names"
	"github.com/davidthor/cldctl/pkg/schema/datacenter"
	v1 "github.com/davidthor/cldctl/pkg/schema/datacenter/v1"
	"github.com/davidthor/cldctl/pkg/state"
	"github.com/davidthor/cldctl/pkg/state/types"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// ExecutionResult contains the results of an execution.
type ExecutionResult struct {
	Success     bool
	Duration    time.Duration
	Created     int
	Updated     int
	Deleted     int
	Failed      int
	Errors      []error
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

// ProgressEvent represents a progress update during execution.
type ProgressEvent struct {
	NodeID   string
	NodeName string
	NodeType string
	Status   string // "pending", "running", "completed", "failed", "skipped"
	Message  string
	Error    error
	// Logs contains captured stdout/stderr output from the resource execution.
	// Populated on failure for error diagnostics.
	Logs string
}

// ProgressCallback is called when resource status changes.
type ProgressCallback func(event ProgressEvent)

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

	// OnProgress is called when resource status changes
	OnProgress ProgressCallback

	// Datacenter is the parsed datacenter configuration (required for hook execution)
	Datacenter datacenter.Datacenter

	// DatacenterVariables are the resolved datacenter variables
	DatacenterVariables map[string]interface{}

	// ComponentSources maps component name to its source path/OCI reference.
	// Used to populate ComponentState.Source for re-deploy reconstruction.
	ComponentSources map[string]string

	// ComponentVariables maps component name to its deployment variables.
	// Used to populate ComponentState.Variables for re-deploy reconstruction.
	ComponentVariables map[string]map[string]interface{}

	// ComponentPorts maps component name to port name to specific port number.
	// Environment-level port overrides take priority over datacenter hooks and
	// the built-in deterministic port allocator.
	ComponentPorts map[string]map[string]int

	// ComponentRoutes maps component name to route name to route override.
	// Environment-level route overrides (subdomain, pathPrefix) are injected
	// into route node inputs by buildModuleInputs.
	ComponentRoutes map[string]map[string]RouteOverride
}

// RouteOverride holds environment-level overrides for a single route.
type RouteOverride struct {
	Subdomain  string
	PathPrefix string
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
	stateManager   state.Manager
	iacRegistry    *iac.Registry
	options        Options
	graph          *graph.Graph // Store reference to graph for service port lookups
	stateMu        sync.Mutex   // Protects concurrent access to environment state
	datacenterName string       // Set at execution start for incremental state saves
}

// saveStateLocked flushes the in-memory environment state to the backend so that
// other processes (e.g., `cldctl inspect`) can observe progress in real time.
// MUST be called while holding e.stateMu. Uses a background context so that
// saves complete even when the deployment context has been cancelled.
func (e *Executor) saveStateLocked(envState *types.EnvironmentState) {
	saveCtx := context.Background()
	_ = e.stateManager.SaveEnvironment(saveCtx, e.datacenterName, envState)
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

// newComponentState creates a new ComponentState for a component, populating
// metadata from executor options (source, variables) and graph (dependencies).
func (e *Executor) newComponentState(componentName string) *types.ComponentState {
	cs := &types.ComponentState{
		Name:      componentName,
		Resources: make(map[string]*types.ResourceState),
		Status:    types.ResourceStatusProvisioning,
		UpdatedAt: time.Now(),
	}
	if e.graph != nil && e.graph.ComponentDependencies != nil {
		cs.Dependencies = e.graph.ComponentDependencies[componentName]
	}
	if e.options.ComponentSources != nil {
		cs.Source = e.options.ComponentSources[componentName]
	}
	if e.options.ComponentVariables != nil {
		if vars, ok := e.options.ComponentVariables[componentName]; ok {
			strVars := make(map[string]string, len(vars))
			for k, v := range vars {
				strVars[k] = fmt.Sprintf("%v", v)
			}
			cs.Variables = strVars
		}
	}
	return cs
}

// resourceKey returns the type-qualified key for storing a resource in state.
// Format: "type.name" (e.g., "deployment.api", "database.main").
// This prevents collisions when different resource types share the same name.
func resourceKey(node *graph.Node) string {
	return string(node.Type) + "." + node.Name
}

// getResourceMap returns the correct resource map for a node: either the shared
// component resources or the per-instance resources. If the node has an Instance,
// the resource is stored under ComponentState.Instances[instanceName].Resources.
// Otherwise, it's stored under ComponentState.Resources.
func (e *Executor) getResourceMap(compState *types.ComponentState, node *graph.Node) map[string]*types.ResourceState {
	if node.Instance == nil {
		// Shared resource
		if compState.Resources == nil {
			compState.Resources = make(map[string]*types.ResourceState)
		}
		return compState.Resources
	}

	// Per-instance resource
	if compState.Instances == nil {
		compState.Instances = make(map[string]*types.InstanceState)
	}
	inst, ok := compState.Instances[node.Instance.Name]
	if !ok {
		inst = &types.InstanceState{
			Name:       node.Instance.Name,
			Weight:     node.Instance.Weight,
			Resources:  make(map[string]*types.ResourceState),
			DeployedAt: time.Now(),
		}
		compState.Instances[node.Instance.Name] = inst
	}
	if inst.Resources == nil {
		inst.Resources = make(map[string]*types.ResourceState)
	}
	return inst.Resources
}

// computeComponentStatuses derives each component's status from its child resources.
// Call this before the final state save so that component-level status is accurate.
func computeComponentStatuses(envState *types.EnvironmentState) {
	for _, comp := range envState.Components {
		if len(comp.Resources) == 0 {
			continue
		}
		allReady := true
		anyFailed := false
		for _, res := range comp.Resources {
			switch res.Status {
			case types.ResourceStatusFailed:
				anyFailed = true
				allReady = false
			case types.ResourceStatusReady:
				// still potentially allReady
			default:
				allReady = false
			}
		}
		if anyFailed {
			comp.Status = types.ResourceStatusFailed
		} else if allReady {
			comp.Status = types.ResourceStatusReady
		} else {
			comp.Status = types.ResourceStatusProvisioning
		}
		comp.UpdatedAt = time.Now()
	}
}

// resolveAndStoreComponentOutputs resolves component-level output expressions
// and stores the results on each ComponentState. This enables dependents to
// access outputs from pass-through components (e.g., credential wrappers) that
// have no infrastructure resources but expose variables as outputs.
//
// Output expressions can reference:
//   - ${{ variables.<name> }}       → component deployment variables
//   - ${{ databases.<name>.<key> }} → resource outputs (resolved via graph nodes)
//   - ${{ services.<name>.<key> }}  → resource outputs
//   - Other standard component expressions
func (e *Executor) resolveAndStoreComponentOutputs(envState *types.EnvironmentState) {
	if e.graph == nil || e.graph.ComponentOutputExprs == nil {
		return
	}

	exprPattern := regexp.MustCompile(`\$\{\{\s*([^}]+)\s*\}\}`)

	for compName, outputExprs := range e.graph.ComponentOutputExprs {
		// Get component variables
		compVars := make(map[string]interface{})
		if e.options.ComponentVariables != nil {
			if vars, ok := e.options.ComponentVariables[compName]; ok {
				compVars = vars
			}
		}

		resolved := make(map[string]interface{}, len(outputExprs))
		for outName, expr := range outputExprs {
			val := exprPattern.ReplaceAllStringFunc(expr, func(match string) string {
				inner := match[3 : len(match)-2]
				inner = strings.TrimSpace(inner)
				parts := strings.Split(inner, ".")

				if len(parts) < 2 {
					return match
				}

				switch parts[0] {
				case "variables":
					varName := parts[1]
					if v, ok := compVars[varName]; ok {
						return fmt.Sprintf("%v", v)
					}
					return ""

				case "databases", "services", "buckets", "routes", "ports":
					// Look up resource output from graph
					if len(parts) < 3 {
						return ""
					}
					var nodeType graph.NodeType
					switch parts[0] {
					case "databases":
						nodeType = graph.NodeTypeDatabase
					case "services":
						nodeType = graph.NodeTypeService
					case "buckets":
						nodeType = graph.NodeTypeBucket
					case "routes":
						nodeType = graph.NodeTypeRoute
					case "ports":
						nodeType = graph.NodeTypePort
					}
					nodeID := fmt.Sprintf("%s/%s/%s", compName, nodeType, parts[1])
					if n, ok := e.graph.Nodes[nodeID]; ok && n.Outputs != nil {
						if v, ok := n.Outputs[parts[2]]; ok {
							return fmt.Sprintf("%v", v)
						}
					}
					return ""

				default:
					return ""
				}
			})
			resolved[outName] = val
		}

		// Store on the ComponentState
		if envState.Components != nil {
			if compState, ok := envState.Components[compName]; ok {
				compState.Outputs = resolved
			}
		}

		// Also ensure components with outputs but no resources still get state entries
		if envState.Components[compName] == nil {
			cs := e.newComponentState(compName)
			cs.Outputs = resolved
			cs.Status = types.ResourceStatusReady
			cs.DeployedAt = time.Now()
			envState.Components[compName] = cs
		}
	}
}

// Execute runs an execution plan.
func (e *Executor) Execute(ctx context.Context, plan *planner.Plan, g *graph.Graph) (*ExecutionResult, error) {
	startTime := time.Now()

	// Store graph reference for service port lookups
	e.graph = g
	e.datacenterName = plan.Datacenter

	result := &ExecutionResult{
		Success:     true,
		NodeResults: make(map[string]*NodeResult),
	}

	if plan.IsEmpty() {
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Get or create environment state
	envState, err := e.stateManager.GetEnvironment(ctx, plan.Datacenter, plan.Environment)
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
	// Ensure Components map is initialized (might be nil if loaded from state)
	if envState.Components == nil {
		envState.Components = make(map[string]*types.ComponentState)
	}

	// Mark as provisioning and flush so that inspect can see progress immediately
	envState.Status = types.EnvironmentStatusProvisioning
	envState.UpdatedAt = time.Now()
	_ = e.stateManager.SaveEnvironment(ctx, plan.Datacenter, envState)

	// Execute changes in order
	for _, change := range plan.Changes {
		if ctx.Err() != nil {
			result.Success = false
			result.Errors = append(result.Errors, ctx.Err())
			break
		}

		// Check if dependencies are satisfied
		if change.Node != nil && !e.areDependenciesSatisfied(change.Node, g, result) {
			depErr := e.buildDependencyError(change.Node, result)
			nodeResult := &NodeResult{
				NodeID:  change.Node.ID,
				Action:  change.Action,
				Success: false,
				Error:   depErr,
			}
			result.NodeResults[change.Node.ID] = nodeResult
			result.Failed++
			result.Success = false

			// Resolve expressions before saving state so that `cldctl inspect`
			// shows resolved values (e.g., database URLs) even for cascaded failures.
			e.resolveComponentExpressions(change.Node, envState)

			// Persist the dependency failure to state so `cldctl inspect` shows it
			e.stateMu.Lock()
			if envState.Components == nil {
				envState.Components = make(map[string]*types.ComponentState)
			}
			compState := envState.Components[change.Node.Component]
			if compState == nil {
				compState = e.newComponentState(change.Node.Component)
				envState.Components[change.Node.Component] = compState
			}
			if compState.Resources == nil {
				compState.Resources = make(map[string]*types.ResourceState)
			}
			compState.Resources[resourceKey(change.Node)] = &types.ResourceState{
				Component:    change.Node.Component,
				Name:         change.Node.Name,
				Type:         string(change.Node.Type),
				Status:       types.ResourceStatusFailed,
				StatusReason: depErr.Error(),
				Inputs:       change.Node.Inputs,
				UpdatedAt:    time.Now(),
			}
			e.saveStateLocked(envState)
			e.stateMu.Unlock()

			// Fire progress event for the dependency failure
			if e.options.OnProgress != nil {
				e.options.OnProgress(ProgressEvent{
					NodeID:   change.Node.ID,
					NodeName: change.Node.Name,
					NodeType: string(change.Node.Type),
					Status:   "failed",
					Message:  depErr.Error(),
					Error:    depErr,
				})
			}

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

		// Update node in graph with outputs and state
		if change.Node != nil {
			if nodeResult.Success {
				if nodeResult.Outputs != nil {
					change.Node.Outputs = nodeResult.Outputs
				}
				change.Node.State = graph.NodeStateCompleted

				// For observability nodes, enrich outputs with merged attributes
				if change.Node.Type == graph.NodeTypeObservability {
					e.enrichObservabilityOutputs(change.Node)
				}
			} else {
				change.Node.State = graph.NodeStateFailed
			}
		}
	}

	// Compute component statuses from child resources
	computeComponentStatuses(envState)

	// Resolve component-level output expressions (e.g., pass-through components
	// that expose variables as outputs for dependents).
	e.resolveAndStoreComponentOutputs(envState)

	// Update environment status
	if result.Success {
		envState.Status = types.EnvironmentStatusReady
	} else {
		envState.Status = types.EnvironmentStatusFailed
	}
	envState.UpdatedAt = time.Now()

	// Save state
	if err := e.stateManager.SaveEnvironment(ctx, plan.Datacenter, envState); err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("failed to save state: %w", err))
	}

	result.Duration = time.Since(startTime)
	return result, nil
}

// buildDependencyError creates a specific error message identifying which dependencies failed.
func (e *Executor) buildDependencyError(node *graph.Node, result *ExecutionResult) error {
	var failedDeps []string
	for _, depID := range node.DependsOn {
		depResult, exists := result.NodeResults[depID]
		if !exists || !depResult.Success {
			failedDeps = append(failedDeps, depID)
		}
	}
	if len(failedDeps) == 1 {
		return fmt.Errorf("dependency %s failed", failedDeps[0])
	}
	return fmt.Errorf("dependencies failed: %s", strings.Join(failedDeps, ", "))
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

	// Create a per-node log buffer to capture plugin output (build logs, process
	// output, etc.). On failure the captured output is included in the progress
	// event so the caller can display it for error diagnostics.
	var logBuf bytes.Buffer

	// Notify progress: starting
	if e.options.OnProgress != nil && change.Node != nil {
		e.options.OnProgress(ProgressEvent{
			NodeID:   change.Node.ID,
			NodeName: change.Node.Name,
			NodeType: string(change.Node.Type),
			Status:   "running",
			Message:  fmt.Sprintf("%s %s", change.Action, change.Node.Name),
		})
	}

	// Handle dry run
	if e.options.DryRun {
		result.Success = true
		result.Duration = time.Since(startTime)
		return result
	}

	switch change.Action {
	case planner.ActionCreate, planner.ActionUpdate, planner.ActionReplace:
		result = e.executeApply(ctx, change, envState, &logBuf)
	case planner.ActionDelete:
		result = e.executeDestroy(ctx, change, envState)
	case planner.ActionNoop:
		result.Success = true
		// Load existing outputs from state so downstream expression resolution works.
		if change.CurrentState != nil && change.CurrentState.Outputs != nil {
			result.Outputs = change.CurrentState.Outputs
		}
	}

	result.Duration = time.Since(startTime)

	// Notify progress: completed or failed.
	// If the context was cancelled (StopOnError), report "cancelled" instead
	// of the verbose underlying error (e.g., Docker socket context canceled).
	if e.options.OnProgress != nil && change.Node != nil {
		status := "completed"
		msg := ""
		progressErr := result.Error
		capturedLogs := ""
		if !result.Success {
			status = "failed"
			if ctx.Err() != nil {
				progressErr = fmt.Errorf("cancelled")
				msg = "cancelled"
			} else if result.Error != nil {
				msg = result.Error.Error()
			}
			// Include captured logs on failure for error diagnostics
			capturedLogs = logBuf.String()
		}
		e.options.OnProgress(ProgressEvent{
			NodeID:   change.Node.ID,
			NodeName: change.Node.Name,
			NodeType: string(change.Node.Type),
			Status:   status,
			Message:  msg,
			Error:    progressErr,
			Logs:     capturedLogs,
		})
	}

	return result
}

func (e *Executor) executeApply(ctx context.Context, change *planner.ResourceChange, envState *types.EnvironmentState, logBuf *bytes.Buffer) *NodeResult {
	result := &NodeResult{
		NodeID: change.Node.ID,
		Action: change.Action,
	}

	// Resolve ${{ }} component expressions in node inputs (e.g., ${{ builds.api.image }},
	// ${{ dependencies.*.outputs.* }}, ${{ variables.* }}) BEFORE saving state so that
	// inspect shows resolved values even while the resource is still provisioning.
	e.resolveComponentExpressions(change.Node, envState)

	// Dump the resolved node configuration when debug mode is active so
	// operators can inspect resource inputs even if the environment is
	// auto-cleaned after a failure (where `inspect` would not be available).
	if os.Getenv("CLDCTL_DEBUG") != "" {
		debugDumpNodeConfig(change)
	}

	// Port nodes use a special allocation flow: env override > datacenter hook > built-in fallback
	if change.Node.Type == graph.NodeTypePort {
		return e.executePortAllocation(ctx, change, envState)
	}

	// Implicit nodes (databaseUser, networkPolicy) are only created when the datacenter
	// defines at least one hook of that type. However, a specific node might not match
	// any hook's `when` condition (e.g., a Redis database when only a Postgres databaseUser
	// hook exists). In that case, fall back to default behavior instead of erroring.
	if change.Node.Type == graph.NodeTypeDatabaseUser && !e.hasMatchingHook(change.Node) {
		return e.executeDatabaseUserPassthrough(ctx, change, envState)
	}
	if change.Node.Type == graph.NodeTypeNetworkPolicy && !e.hasMatchingHook(change.Node) {
		return e.executeNetworkPolicyNoop(ctx, change, envState)
	}

	// For databaseUser nodes, inject the parent database's resolved host and port
	// into the node inputs so the datacenter hook can forward them to the module.
	// The database node has already completed (it's a dependency), so its outputs
	// are available. This allows the databaseUser module to construct correct
	// connection URLs even when the database uses dynamically allocated ports.
	if change.Node.Type == graph.NodeTypeDatabaseUser {
		e.injectParentDatabaseOutputs(change.Node)
	}

	// Lock for state initialization
	e.stateMu.Lock()

	// Ensure environment state maps are initialized
	if envState.Components == nil {
		envState.Components = make(map[string]*types.ComponentState)
	}

	// Get or create component state
	compState := envState.Components[change.Node.Component]
	if compState == nil {
		compState = e.newComponentState(change.Node.Component)
		envState.Components[change.Node.Component] = compState
	}

	// Determine where to store the resource: per-instance or shared
	resMap := e.getResourceMap(compState, change.Node)

	// Save a "provisioning" entry immediately so that `cldctl inspect` can see
	// in-progress resources before plugin.Apply returns (which may block for a
	// long time, e.g. readiness checks on dev-server processes).
	resMap[resourceKey(change.Node)] = &types.ResourceState{
		Component: change.Node.Component,
		Name:      change.Node.Name,
		Type:      string(change.Node.Type),
		Status:    types.ResourceStatusProvisioning,
		Inputs:    change.Node.Inputs,
		UpdatedAt: time.Now(),
	}
	e.saveStateLocked(envState)

	e.stateMu.Unlock()

	// Build a sub-status progress callback that forwards messages from the
	// IaC plugin back to the CLI progress table as intermediate "running" events.
	var hookOnProgress func(string)
	if e.options.OnProgress != nil && change.Node != nil {
		hookOnProgress = func(msg string) {
			e.options.OnProgress(ProgressEvent{
				NodeID:   change.Node.ID,
				NodeName: change.Node.Name,
				NodeType: string(change.Node.Type),
				Status:   "running",
				Message:  msg,
			})
		}
	}

	// Find the matching hook from datacenter and execute all its modules
	hookResult, err := e.executeHookModules(ctx, change.Node, envState.Name, compState, logBuf, hookOnProgress)
	if err != nil {
		result.Error = fmt.Errorf("failed to execute hook: %w", err)
		result.Success = false

		// Update resource state to failed (lock for state update)
		e.stateMu.Lock()
		resMap := e.getResourceMap(compState, change.Node)
		resMap[resourceKey(change.Node)] = &types.ResourceState{
			Component:    change.Node.Component,
			Name:         change.Node.Name,
			Type:         string(change.Node.Type),
			Status:       types.ResourceStatusFailed,
			StatusReason: result.Error.Error(),
			Inputs:       change.Node.Inputs,
			UpdatedAt:    time.Now(),
		}
		e.saveStateLocked(envState)
		e.stateMu.Unlock()

		return result
	}

	result.Outputs = hookResult.Outputs
	result.Success = true

	// Update resource state including IaC state for cleanup (lock for state update)
	e.stateMu.Lock()
	resourceState := &types.ResourceState{
		Component: change.Node.Component,
		Name:      change.Node.Name,
		Type:      string(change.Node.Type),
		Status:    types.ResourceStatusReady,
		Inputs:    change.Node.Inputs,
		Outputs:   hookResult.Outputs,
		UpdatedAt: time.Now(),
	}
	// For single-module hooks, store IaC state in the legacy field for backward compatibility.
	// For multi-module hooks, store per-module states.
	if len(hookResult.ModuleStates) == 1 {
		for _, ms := range hookResult.ModuleStates {
			resourceState.IaCState = ms.IaCState
		}
	} else if len(hookResult.ModuleStates) > 1 {
		resourceState.ModuleStates = hookResult.ModuleStates
	}
	resMapFinal := e.getResourceMap(compState, change.Node)
	resMapFinal[resourceKey(change.Node)] = resourceState
	e.saveStateLocked(envState)
	e.stateMu.Unlock()

	return result
}

// hookExecutionResult contains the combined results of executing all modules in a hook.
type hookExecutionResult struct {
	Outputs      map[string]interface{}
	ModuleStates map[string]*types.ModuleState
}

// executeHookModules finds the matching hook, executes ALL its modules (not just the first),
// allows cross-module references in inputs, evaluates hook outputs including nested objects,
// and auto-populates read/write fallback outputs for database hooks.
// onProgress (may be nil) forwards sub-status messages from plugins to the caller.
func (e *Executor) executeHookModules(ctx context.Context, node *graph.Node, envName string, compState *types.ComponentState, logBuf *bytes.Buffer, onProgress func(string)) (*hookExecutionResult, error) {
	dc := e.options.Datacenter
	if dc == nil {
		return nil, fmt.Errorf("no datacenter configuration provided")
	}

	// Get hooks for this node type
	hooks := e.getHooksForType(node.Type)
	if len(hooks) == 0 {
		return nil, fmt.Errorf("no hooks defined for resource type %s in datacenter (source: %s)", node.Type, dc.SourcePath())
	}

	// Find the first matching hook based on 'when' condition
	var matchedHook datacenter.Hook
	for _, hook := range hooks {
		when := hook.When()
		matches := e.evaluateWhenCondition(when, node.Inputs)
		if matches {
			matchedHook = hook
			break
		}
	}

	if matchedHook == nil {
		return nil, fmt.Errorf("no matching hook found for %s (inputs: %v)", node.Type, node.Inputs)
	}

	// Check if the matched hook is an error hook (rejects the resource)
	if errMsg := matchedHook.Error(); errMsg != "" {
		evaluatedMsg := e.evaluateErrorMessage(errMsg, node.Inputs)
		return nil, arcerrors.DatacenterHookError(
			string(node.Type),
			node.Component,
			node.Name,
			evaluatedMsg,
		)
	}

	modules := matchedHook.Modules()
	if len(modules) == 0 {
		return nil, fmt.Errorf("hook has no modules defined for %s", node.Type)
	}

	// Resolve datacenter path for module paths
	dcPath := dc.SourcePath()
	dcDir := filepath.Dir(dcPath)

	// Execute all modules, accumulating outputs for cross-module references
	// moduleOutputs maps module name -> output name -> value
	moduleOutputs := make(map[string]map[string]interface{})
	moduleStates := make(map[string]*types.ModuleState)

	for _, module := range modules {
		// Check module's when condition (if any)
		moduleWhen := module.When()
		if moduleWhen != "" && !e.evaluateWhenCondition(moduleWhen, node.Inputs) {
			continue
		}

		// Resolve module path
		modulePath := module.Build()
		if modulePath == "" {
			modulePath = module.Source()
		}

		if os.Getenv("CLDCTL_DEBUG") != "" && e.options.Output != nil {
			fmt.Fprintf(e.options.Output, "  [debug] Node %s: executing module %s (dcDir=%s, build=%q, source=%q)\n",
				node.ID, module.Name(), dcDir, module.Build(), module.Source())
		}

		if modulePath != "" && !filepath.IsAbs(modulePath) {
			modulePath = filepath.Join(dcDir, modulePath)
		}

		if modulePath == "" {
			return nil, fmt.Errorf("module %s has no build or source path", module.Name())
		}

		// Build module inputs, resolving cross-module references (module.<name>.<output>)
		inputs := e.buildModuleInputsWithCrossRef(module, node, envName, moduleOutputs)

		// Get IaC plugin
		pluginName := module.Plugin()
		if pluginName == "" {
			pluginName = "native"
		}
		plugin, err := e.iacRegistry.Get(pluginName)
		if err != nil {
			return nil, fmt.Errorf("failed to get IaC plugin %q: %w", pluginName, err)
		}

		// Execute — pipe plugin output into the per-node log buffer so it can be
		// included in error diagnostics instead of being printed to stdout.
		runOpts := iac.RunOptions{
			ModuleSource: modulePath,
			Inputs:       inputs,
			Environment:  map[string]string{},
			Stdout:       logBuf,
			Stderr:       logBuf,
			OnProgress:   onProgress,
		}

		applyResult, err := plugin.Apply(ctx, runOpts)
		if err != nil {
			// Log resource configuration on failure for debugging
			if os.Getenv("CLDCTL_DEBUG") != "" {
				fmt.Fprintf(os.Stderr, "\n[debug] Resource %s/%s module %s failed — configuration:\n", node.Type, node.Name, module.Name())
				fmt.Fprintf(os.Stderr, "  Component:        %s\n", node.Component)
				fmt.Fprintf(os.Stderr, "  Module:           %s (plugin: %s)\n", modulePath, pluginName)
			}
			return nil, fmt.Errorf("module %s apply failed: %w", module.Name(), err)
		}

		// Collect module outputs
		modOutputs := make(map[string]interface{})
		for name, out := range applyResult.Outputs {
			modOutputs[name] = out.Value
		}
		moduleOutputs[module.Name()] = modOutputs

		// Track per-module state
		moduleStates[module.Name()] = &types.ModuleState{
			Name:     module.Name(),
			Plugin:   pluginName,
			Source:   modulePath,
			Inputs:   inputs,
			Outputs:  modOutputs,
			IaCState: applyResult.State,
			Status:   types.ModuleStatusReady,
		}
	}

	// Evaluate hook-level outputs using module outputs
	outputs := e.evaluateHookOutputs(matchedHook, moduleOutputs, node, envName)

	// Auto-populate read/write nested outputs for database hooks
	if node.Type == graph.NodeTypeDatabase {
		e.autoPopulateDatabaseEndpoints(outputs, matchedHook)
	}

	// Validate that the hook produced all required outputs for this resource type.
	// Missing outputs lead to unresolved ${{ }} expressions downstream which are
	// very difficult to diagnose, so we fail early with a clear message.
	if err := validateHookOutputs(node.Type, outputs); err != nil {
		return nil, fmt.Errorf("datacenter hook for %s/%s produced incomplete outputs: %w", node.Type, node.Name, err)
	}

	return &hookExecutionResult{
		Outputs:      outputs,
		ModuleStates: moduleStates,
	}, nil
}

// buildModuleInputsWithCrossRef builds inputs for a module, resolving cross-module references
// (module.<name>.<output>) from previously executed modules' outputs.
func (e *Executor) buildModuleInputsWithCrossRef(module datacenter.Module, node *graph.Node, envName string, moduleOutputs map[string]map[string]interface{}) map[string]interface{} {
	// Start with the standard input building
	inputs := e.buildModuleInputs(module, node, envName)

	// Resolve cross-module references in input values
	dcVars := e.options.DatacenterVariables
	if dcVars == nil {
		dcVars = make(map[string]interface{})
	}

	for key, value := range inputs {
		if strVal, ok := value.(string); ok {
			resolved := e.resolveCrossModuleRefs(strVal, moduleOutputs, node, envName, dcVars)
			if resolved != strVal {
				inputs[key] = resolved
			}
		}
	}

	// Also check the raw module input definitions for cross-module refs that
	// buildModuleInputs may have stored as expression strings
	moduleInputDefs := module.Inputs()
	for inputName, exprStr := range moduleInputDefs {
		if strings.Contains(exprStr, "module.") {
			resolved := e.resolveCrossModuleRefs(exprStr, moduleOutputs, node, envName, dcVars)
			if resolved != exprStr {
				inputs[inputName] = resolved
			}
		}
	}

	return inputs
}

// resolveCrossModuleRefs resolves module.<name>.<output> references in an expression string
// using the accumulated outputs from previously executed modules.
func (e *Executor) resolveCrossModuleRefs(expr string, moduleOutputs map[string]map[string]interface{}, node *graph.Node, envName string, dcVars map[string]interface{}) string {
	if !strings.Contains(expr, "module.") {
		return expr
	}

	// Handle ${module.<name>.<output>} interpolation patterns
	result := expr
	for modName, modOutputs := range moduleOutputs {
		for outName, outVal := range modOutputs {
			ref := fmt.Sprintf("module.%s.%s", modName, outName)
			interpolatedRef := fmt.Sprintf("${module.%s.%s}", modName, outName)

			// Replace ${module.x.y} interpolation
			if strings.Contains(result, interpolatedRef) {
				result = strings.ReplaceAll(result, interpolatedRef, fmt.Sprintf("%v", outVal))
			}

			// Replace bare module.x.y references (when the whole value is a reference)
			if result == ref {
				result = fmt.Sprintf("%v", outVal)
			}
		}
	}

	return result
}

// evaluateHookOutputs evaluates the hook's output expressions using accumulated module outputs.
// For expressions like "module.postgres.url", it looks up the value from moduleOutputs.
// Also handles nested output objects (e.g., read = { ... }, write = { ... }).
func (e *Executor) evaluateHookOutputs(hook datacenter.Hook, moduleOutputs map[string]map[string]interface{}, node *graph.Node, envName string) map[string]interface{} {
	outputs := make(map[string]interface{})

	dcVars := e.options.DatacenterVariables
	if dcVars == nil {
		dcVars = make(map[string]interface{})
	}

	// Evaluate flat outputs
	for name, exprStr := range hook.Outputs() {
		value := e.resolveOutputExpression(exprStr, moduleOutputs, node, envName, dcVars)
		if value != nil {
			outputs[name] = value
		}
	}

	// Evaluate nested outputs (e.g., read = { ... }, write = { ... })
	nestedOutputs := hook.NestedOutputs()
	for name, nested := range nestedOutputs {
		nestedMap := make(map[string]interface{})
		for nk, nv := range nested {
			value := e.resolveOutputExpression(nv, moduleOutputs, node, envName, dcVars)
			if value != nil {
				nestedMap[nk] = value
			}
		}
		if len(nestedMap) > 0 {
			outputs[name] = nestedMap
		}
	}

	return outputs
}

// resolveOutputExpression resolves a hook output expression, handling module references,
// string interpolation, and node/environment references.
func (e *Executor) resolveOutputExpression(exprStr string, moduleOutputs map[string]map[string]interface{}, node *graph.Node, envName string, dcVars map[string]interface{}) interface{} {
	// First try to resolve cross-module references
	resolved := e.resolveCrossModuleRefs(exprStr, moduleOutputs, node, envName, dcVars)

	// If fully resolved (no more module. references), return the value
	if resolved != exprStr {
		return resolved
	}

	// Fall back to the standard input expression evaluator for non-module references
	return e.evaluateInputExpression(exprStr, node, envName, dcVars)
}

// autoPopulateDatabaseEndpoints ensures that database outputs have read/write sub-objects.
// If the datacenter hook didn't explicitly set read/write outputs, they are auto-populated
// by mirroring the top-level flat outputs. This ensures backwards compatibility.
func (e *Executor) autoPopulateDatabaseEndpoints(outputs map[string]interface{}, hook datacenter.Hook) {
	// Build a default endpoint from top-level outputs
	defaultEndpoint := map[string]interface{}{}
	for _, key := range []string{"host", "port", "url", "username", "password"} {
		if val, ok := outputs[key]; ok {
			defaultEndpoint[key] = val
		}
	}

	// Auto-populate "read" if not explicitly set
	if _, hasRead := outputs["read"]; !hasRead {
		if len(defaultEndpoint) > 0 {
			readCopy := make(map[string]interface{}, len(defaultEndpoint))
			for k, v := range defaultEndpoint {
				readCopy[k] = v
			}
			outputs["read"] = readCopy
		}
	}

	// Auto-populate "write" if not explicitly set
	if _, hasWrite := outputs["write"]; !hasWrite {
		if len(defaultEndpoint) > 0 {
			writeCopy := make(map[string]interface{}, len(defaultEndpoint))
			for k, v := range defaultEndpoint {
				writeCopy[k] = v
			}
			outputs["write"] = writeCopy
		}
	}
}

// requiredHookOutputs maps each resource type to the output keys that the
// datacenter hook MUST produce at deploy time. Missing outputs cause unresolved
// ${{ }} expressions downstream, so we validate eagerly and fail with a clear
// message.
//
// This mirrors internal.RequiredHookOutputs but uses graph.NodeType keys and
// operates on resolved map[string]interface{} values (vs expression strings at
// build time).
var requiredHookOutputs = map[graph.NodeType][]string{
	graph.NodeTypeDatabase:      {"host", "port", "url"},
	graph.NodeTypeBucket:        {"endpoint", "bucket", "accessKeyId", "secretAccessKey"},
	graph.NodeTypeSMTP:          {"host", "port", "username", "password"},
	graph.NodeTypeDeployment:    {"id"},
	graph.NodeTypeFunction:      {"id", "endpoint"},
	graph.NodeTypeService:       {"host", "port", "url"},
	graph.NodeTypeRoute:         {"url", "host", "port"},
	graph.NodeTypeTask:          {"id", "status"},
	graph.NodeTypeObservability: {"endpoint", "protocol"},
	graph.NodeTypeDatabaseUser:  {"host", "port", "url"},
	// database: username and password are optional (not all engines require
	// credentials, e.g., Redis).
	// encryptionKey: outputs vary by algorithm (RSA vs symmetric) — validated separately if needed.
	// port: hook is optional (engine has built-in fallback), so no required outputs here.
	// networkPolicy: no outputs (fire-and-forget leaf node).
}

// validateHookOutputs checks that the hook outputs contain all required keys
// for the given resource type. Returns a descriptive error listing the missing
// keys, or nil if all required outputs are present.
func validateHookOutputs(nodeType graph.NodeType, outputs map[string]interface{}) error {
	required, ok := requiredHookOutputs[nodeType]
	if !ok {
		return nil // No required outputs defined for this type
	}

	var missing []string
	for _, key := range required {
		if _, exists := outputs[key]; !exists {
			missing = append(missing, key)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf(
			"hook for %s is missing required outputs: %s (have: %s). "+
				"Check the datacenter template's outputs block for this hook",
			nodeType,
			strings.Join(missing, ", "),
			func() string {
				keys := make([]string, 0, len(outputs))
				for k := range outputs {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				if len(keys) == 0 {
					return "none"
				}
				return strings.Join(keys, ", ")
			}(),
		)
	}

	return nil
}

// findMatchingHook finds the matching datacenter hook for a node and returns the module path, inputs, and plugin name.
// NOTE: This method is retained for backward compatibility with single-module execution paths
// (e.g., port allocation). For multi-module execution, use executeHookModules instead.
func (e *Executor) findMatchingHook(node *graph.Node, envName string) (modulePath string, inputs map[string]interface{}, pluginName string, err error) {
	dc := e.options.Datacenter
	if dc == nil {
		return "", nil, "", fmt.Errorf("no datacenter configuration provided")
	}

	// Get hooks for this node type
	hooks := e.getHooksForType(node.Type)
	if len(hooks) == 0 {
		return "", nil, "", fmt.Errorf("no hooks defined for resource type %s in datacenter (source: %s)", node.Type, dc.SourcePath())
	}

	// Find the first matching hook based on 'when' condition
	var matchedHook datacenter.Hook
	for _, hook := range hooks {
		when := hook.When()
		matches := e.evaluateWhenCondition(when, node.Inputs)
		if matches {
			matchedHook = hook
			break
		}
	}

	if matchedHook == nil {
		return "", nil, "", fmt.Errorf("no matching hook found for %s (inputs: %v)", node.Type, node.Inputs)
	}

	// Check if the matched hook is an error hook (rejects the resource)
	if errMsg := matchedHook.Error(); errMsg != "" {
		evaluatedMsg := e.evaluateErrorMessage(errMsg, node.Inputs)
		return "", nil, "", arcerrors.DatacenterHookError(
			string(node.Type),
			node.Component,
			node.Name,
			evaluatedMsg,
		)
	}

	// Get the first module from the hook
	modules := matchedHook.Modules()
	if len(modules) == 0 {
		return "", nil, "", fmt.Errorf("hook has no modules defined for %s", node.Type)
	}

	module := modules[0]

	// Resolve module path relative to datacenter source
	dcPath := dc.SourcePath()
	dcDir := filepath.Dir(dcPath)
	modulePath = module.Build()
	if modulePath == "" {
		modulePath = module.Source()
	}

	// Debug output for troubleshooting (only when env var is set)
	if os.Getenv("CLDCTL_DEBUG") != "" && e.options.Output != nil {
		fmt.Fprintf(e.options.Output, "  [debug] Node %s: dcPath=%s, dcDir=%s, moduleName=%s, moduleBuild=%q, moduleSource=%q\n",
			node.ID, dcPath, dcDir, module.Name(), module.Build(), module.Source())
	}

	if modulePath != "" && !filepath.IsAbs(modulePath) {
		modulePath = filepath.Join(dcDir, modulePath)
	}

	if modulePath == "" {
		return "", nil, "", fmt.Errorf("module %s has no build or source path", module.Name())
	}

	// Build module inputs by evaluating expressions in the hook's module inputs
	inputs = e.buildModuleInputs(module, node, envName)

	return modulePath, inputs, module.Plugin(), nil
}

// getHooksForType returns the datacenter hooks for a given node type.
func (e *Executor) getHooksForType(nodeType graph.NodeType) []datacenter.Hook {
	dc := e.options.Datacenter
	if dc == nil || dc.Environment() == nil {
		return nil
	}

	hooks := dc.Environment().Hooks()
	if hooks == nil {
		return nil
	}

	switch nodeType {
	case graph.NodeTypeDatabase:
		return hooks.Database()
	case graph.NodeTypeBucket:
		return hooks.Bucket()
	case graph.NodeTypeDeployment:
		return hooks.Deployment()
	case graph.NodeTypeFunction:
		return hooks.Function()
	case graph.NodeTypeService:
		return hooks.Service()
	case graph.NodeTypeRoute:
		return hooks.Route()
	case graph.NodeTypeCronjob:
		return hooks.Cronjob()
	case graph.NodeTypeDockerBuild:
		return hooks.DockerBuild()
	case graph.NodeTypeTask:
		return hooks.Task()
	case graph.NodeTypeEncryptionKey:
		return hooks.EncryptionKey()
	case graph.NodeTypeSMTP:
		return hooks.SMTP()
	case graph.NodeTypeObservability:
		return hooks.Observability()
	case graph.NodeTypePort:
		return hooks.Port()
	case graph.NodeTypeDatabaseUser:
		return hooks.DatabaseUser()
	case graph.NodeTypeNetworkPolicy:
		return hooks.NetworkPolicy()
	default:
		return nil
	}
}

// hasMatchingHook returns true if at least one datacenter hook of the node's type
// matches the node's inputs (evaluating `when` conditions). This is used for
// implicit node types (databaseUser, networkPolicy) to decide whether to execute
// the hook pipeline or fall back to default behavior.
func (e *Executor) hasMatchingHook(node *graph.Node) bool {
	hooks := e.getHooksForType(node.Type)
	for _, hook := range hooks {
		if e.evaluateWhenCondition(hook.When(), node.Inputs) {
			return true
		}
	}
	return false
}

// evaluateWhenCondition evaluates a 'when' condition string against node inputs.
// It first attempts full HCL expression evaluation via the v1 Evaluator. If that
// fails (e.g. due to an unparseable expression), it falls back to simplified
// string-based matching for common patterns.
func (e *Executor) evaluateWhenCondition(when string, inputs map[string]interface{}) bool {
	if when == "" {
		return true // No condition means always match
	}

	// Try full HCL expression evaluation first
	if result, err := e.evaluateWhenHCL(when, inputs); err == nil {
		return result
	}

	// Fall back to simplified string matching for patterns that can't be parsed as HCL
	return e.evaluateWhenStringFallback(when, inputs)
}

// evaluateWhenHCL parses the when string as an HCL expression and evaluates it
// with the full v1 Evaluator context (node inputs, environment, variables, etc.).
func (e *Executor) evaluateWhenHCL(when string, inputs map[string]interface{}) (bool, error) {
	expr, diags := hclsyntax.ParseExpression([]byte(when), "when.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return false, fmt.Errorf("failed to parse when expression: %s", diags.Error())
	}

	eval := v1.NewEvaluator()

	// Set node context with inputs (including instance context injected by buildModuleInputs)
	eval.SetNodeContext("", "", "", inputs)

	// Set datacenter variables if available
	if e.options.DatacenterVariables != nil {
		eval.SetVariables(e.options.DatacenterVariables)
	}

	return eval.EvaluateWhen(expr)
}

// evaluateWhenStringFallback provides legacy string-based matching for when conditions
// that cannot be parsed as HCL expressions.
func (e *Executor) evaluateWhenStringFallback(when string, inputs map[string]interface{}) bool {
	// Check for "!= null" patterns (before generic "==" check)
	if contains(when, "!= null") || contains(when, "!=null") {
		inputName := extractInputName(when)
		if inputName != "" {
			val := inputs[inputName]
			return val != nil && val != ""
		}
	}

	// Check for "== null" patterns (before generic "==" check)
	// Handles conditions like: node.inputs.image == null
	if contains(when, "== null") || contains(when, "==null") {
		inputName := extractInputName(when)
		if inputName != "" {
			val := inputs[inputName]
			return val == nil || val == ""
		}
	}

	// Check for "==" comparisons
	if contains(when, "==") {
		parts := splitOnce(when, "==")
		if len(parts) == 2 {
			left := trimQuotes(e.resolveWhenExpr(parts[0], inputs))
			right := trimQuotes(parts[1])
			return left == right
		}
	}

	// Default to true if we can't parse the condition
	return true
}

// evaluateErrorMessage evaluates a hook error message, resolving any HCL interpolations
// like ${node.inputs.type} using the node's inputs. Falls back to the raw string
// if HCL evaluation fails.
func (e *Executor) evaluateErrorMessage(errorMsg string, inputs map[string]interface{}) string {
	// Try full HCL template evaluation first
	expr, diags := hclsyntax.ParseTemplate([]byte(errorMsg), "error.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return errorMsg // Return raw string if it can't be parsed as a template
	}

	eval := v1.NewEvaluator()
	eval.SetNodeContext("", "", "", inputs)

	if e.options.DatacenterVariables != nil {
		eval.SetVariables(e.options.DatacenterVariables)
	}

	evaluated, err := eval.EvaluateErrorMessage(expr)
	if err != nil {
		return errorMsg // Return raw string if evaluation fails
	}

	return evaluated
}

// resolveWhenExpr resolves the left-hand side of a when condition comparison.
// Handles direct input references (node.inputs.X) and function call expressions
// like element(split(":", node.inputs.type), 0).
func (e *Executor) resolveWhenExpr(expr string, inputs map[string]interface{}) string {
	expr = trimSpace(expr)

	// Check if expression is a function call
	if strings.Contains(expr, "(") {
		result := e.evaluateExprFunctions(expr, inputs)
		if s, ok := result.(string); ok {
			return s
		}
		if result != nil {
			return fmt.Sprintf("%v", result)
		}
		return ""
	}

	// Fall back to direct input reference
	return extractInputRef(expr, inputs)
}

// evaluateExprFunctions evaluates function call expressions like:
//   - split(":", "postgres:^16") -> ["postgres", "^16"]
//   - element(split(":", node.inputs.type), 0) -> "postgres"
//   - try(element(split(":", node.inputs.type), 1), null) -> "^16" or nil
func (e *Executor) evaluateExprFunctions(expr string, inputs map[string]interface{}) interface{} {
	expr = trimSpace(expr)

	// Handle element(list, index)
	if strings.HasPrefix(expr, "element(") && strings.HasSuffix(expr, ")") {
		inner := expr[8 : len(expr)-1] // strip "element(" and ")"
		args := splitFuncArgs(inner)
		if len(args) == 2 {
			listVal := e.evaluateExprFunctions(trimSpace(args[0]), inputs)
			indexStr := trimSpace(args[1])
			index, _ := strconv.Atoi(indexStr)

			if list, ok := listVal.([]string); ok && index >= 0 && index < len(list) {
				return list[index]
			}
			return nil
		}
	}

	// Handle split(separator, string)
	if strings.HasPrefix(expr, "split(") && strings.HasSuffix(expr, ")") {
		inner := expr[6 : len(expr)-1] // strip "split(" and ")"
		args := splitFuncArgs(inner)
		if len(args) == 2 {
			sep := trimQuotes(trimSpace(args[0]))
			strVal := e.evaluateExprFunctions(trimSpace(args[1]), inputs)
			if s, ok := strVal.(string); ok {
				return strings.Split(s, sep)
			}
		}
	}

	// Handle try(expr, fallback)
	if strings.HasPrefix(expr, "try(") && strings.HasSuffix(expr, ")") {
		inner := expr[4 : len(expr)-1] // strip "try(" and ")"
		args := splitFuncArgs(inner)
		if len(args) >= 1 {
			result := e.evaluateExprFunctions(trimSpace(args[0]), inputs)
			if result != nil {
				return result
			}
			if len(args) >= 2 {
				fallback := trimSpace(args[1])
				if fallback == "null" {
					return nil
				}
				return trimQuotes(fallback)
			}
		}
		return nil
	}

	// Handle coalesce(a, b, ...)
	if strings.HasPrefix(expr, "coalesce(") && strings.HasSuffix(expr, ")") {
		inner := expr[9 : len(expr)-1]
		args := splitFuncArgs(inner)
		for _, arg := range args {
			val := e.evaluateExprFunctions(trimSpace(arg), inputs)
			if val != nil && val != "" {
				return val
			}
		}
		return nil
	}

	// Handle node.inputs.X references
	if strings.HasPrefix(expr, "node.inputs.") {
		inputName := expr[12:]
		if val, ok := inputs[inputName]; ok {
			if s, ok := val.(string); ok {
				return s
			}
			if val != nil {
				return fmt.Sprintf("%v", val)
			}
		}
		return nil
	}

	// Handle quoted strings
	if len(expr) >= 2 && expr[0] == '"' && expr[len(expr)-1] == '"' {
		return expr[1 : len(expr)-1]
	}

	// Handle null
	if expr == "null" {
		return nil
	}

	return expr
}

// splitFuncArgs splits function arguments respecting nested parentheses and quoted strings.
func splitFuncArgs(s string) []string {
	var args []string
	var current strings.Builder
	parenDepth := 0
	inQuotes := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' && (i == 0 || s[i-1] != '\\') {
			inQuotes = !inQuotes
			current.WriteByte(c)
		} else if inQuotes {
			current.WriteByte(c)
		} else if c == '(' {
			parenDepth++
			current.WriteByte(c)
		} else if c == ')' {
			parenDepth--
			current.WriteByte(c)
		} else if c == ',' && parenDepth == 0 {
			args = append(args, current.String())
			current.Reset()
		} else {
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

// buildModuleInputs builds the inputs for a module based on the hook's input expressions.
// Since the datacenter's HCL expressions can't be evaluated at parse time (they contain runtime references),
// we build the inputs directly based on standard patterns for each module type.
func (e *Executor) buildModuleInputs(module datacenter.Module, node *graph.Node, envName string) map[string]interface{} {
	inputs := make(map[string]interface{})

	// Get datacenter variables with defaults
	dcVars := e.options.DatacenterVariables
	if dcVars == nil {
		dcVars = make(map[string]interface{})
	}

	// Get defaults for common datacenter variables
	networkName := getStringVar(dcVars, "network_name", "cldctl-local")
	host := getStringVar(dcVars, "host", "localhost")

	// Standard name format: ${environment.name}-${node.component}-${node.name}
	// Sanitize component name for use in Docker resource names (slashes are not allowed)
	safeComponent := sanitizeResourceName(node.Component)
	standardName := fmt.Sprintf("%s-%s-%s", envName, safeComponent, node.Name)

	// Try to use the datacenter's input definitions first (if they were evaluated successfully)
	moduleInputDefs := module.Inputs()
	for inputName, exprStr := range moduleInputDefs {
		value := e.evaluateInputExpression(exprStr, node, envName, dcVars)
		if value != nil && value != "" {
			inputs[inputName] = value
		}
	}

	// Inject instance context into inputs so datacenter modules can access it
	if node.Instance != nil {
		setIfMissing(inputs, "instance_name", node.Instance.Name)
		setIfMissing(inputs, "instance_weight", node.Instance.Weight)
	} else {
		setIfMissing(inputs, "instance_name", "default")
		setIfMissing(inputs, "instance_weight", 100)
	}

	// Inject instances list for shared nodes (e.g., route hooks for traffic splitting)
	if len(node.Instances) > 0 {
		instancesList := make([]map[string]interface{}, len(node.Instances))
		for i, inst := range node.Instances {
			instancesList[i] = map[string]interface{}{
				"name":   inst.Name,
				"weight": inst.Weight,
			}
		}
		setIfMissing(inputs, "instances", instancesList)
	}

	// Build inputs based on module type with standard mappings
	// These are the common inputs expected by local datacenter modules
	moduleName := module.Name()

	switch moduleName {
	case "postgres", "mysql":
		inputs["name"] = standardName
		inputs["database"] = node.Name
		inputs["network"] = networkName
		// Assign a port based on hash of the name (for local dev)
		inputs["port"] = 5432 + hashCode(standardName)%100
		if v := extractVersionFromType(node.Inputs["type"]); v != "" {
			inputs["version"] = v
		}

	case "redis":
		inputs["name"] = standardName
		inputs["network"] = networkName
		// Assign a port based on hash of the name (for local dev)
		inputs["port"] = 6379 + hashCode(standardName)%100
		if v := extractVersionFromType(node.Inputs["type"]); v != "" {
			inputs["version"] = v
		}

	case "minio":
		setIfMissing(inputs, "name", standardName)
		setIfMissing(inputs, "network", networkName)
		setIfMissing(inputs, "versioning", node.Inputs["versioning"])
		setIfMissing(inputs, "public", node.Inputs["public"])

	case "service":
		setIfMissing(inputs, "name", node.Name)
		setIfMissing(inputs, "target", node.Inputs["target"])
		setIfMissing(inputs, "target_type", node.Inputs["targetType"])
		setIfMissing(inputs, "port", node.Inputs["port"])
		setIfMissing(inputs, "protocol", node.Inputs["protocol"])
		setIfMissing(inputs, "host", host)

	case "route":
		setIfMissing(inputs, "name", standardName)
		setIfMissing(inputs, "type", node.Inputs["type"])
		setIfMissing(inputs, "rules", node.Inputs["rules"])
		setIfMissing(inputs, "internal", node.Inputs["internal"])
		setIfMissing(inputs, "host", host)

		// Inject subdomain and path_prefix from environment route config or generate defaults.
		// Priority: environment-level override > deterministic default.
		subdomain := ""
		pathPrefix := "/"
		if e.options.ComponentRoutes != nil {
			if compRoutes, ok := e.options.ComponentRoutes[node.Component]; ok {
				if ro, ok := compRoutes[node.Name]; ok {
					subdomain = ro.Subdomain
					pathPrefix = ro.PathPrefix
				}
			}
		}
		if subdomain == "" {
			subdomain = names.Generate(envName, node.Component, node.Name)
		}
		if pathPrefix == "" {
			pathPrefix = "/"
		}
		setIfMissing(inputs, "subdomain", subdomain)
		setIfMissing(inputs, "path_prefix", pathPrefix)

		// Resolve upstream port for the route's target service/function.
		// This allows datacenter route hooks to know the upstream endpoint
		// without needing to reference other node outputs.
		if target, ok := node.Inputs["target"].(string); ok && target != "" {
			var upstreamPort int
			targetType, _ := node.Inputs["targetType"].(string)
			switch targetType {
			case "service":
				// Look up the service node's declared port from the component schema.
				// The service node may execute in parallel with this route, so its
				// port input may still be an unresolved expression. Handle both
				// resolved (int/float64) and unresolved (${{ }}) cases.
				svcNodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeService, target)
				if svcNode, ok := e.graph.Nodes[svcNodeID]; ok {
					upstreamPort = toIntSafe(svcNode.Inputs["port"])
					if upstreamPort == 0 {
						if portStr, ok := svcNode.Inputs["port"].(string); ok && strings.Contains(portStr, "${{") {
							upstreamPort = e.resolvePortFromExpression(portStr, node.Component)
						}
					}
				}
			case "function":
				// Look up the function node and resolve its port (from schema or associated service)
				fnNodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeFunction, target)
				if fnNode, ok := e.graph.Nodes[fnNodeID]; ok {
					upstreamPort = e.resolvePortForWorkload(fnNode)
				}
			}
			if upstreamPort > 0 {
				setIfMissing(inputs, "upstream_port", upstreamPort)
			}
		}

	case "build":
		setIfMissing(inputs, "context", node.Inputs["context"])
		setIfMissing(inputs, "dockerfile", node.Inputs["dockerfile"])
		setIfMissing(inputs, "target", node.Inputs["target"])
		setIfMissing(inputs, "args", node.Inputs["args"])
		setIfMissing(inputs, "tag", fmt.Sprintf("%s-%s-%s:local", envName, safeComponent, node.Name))

	case "process":
		setIfMissing(inputs, "name", standardName)
		// Map source path to context for process module.
		// Functions use "srcPath"; deployments use "workingDirectory".
		if node.Inputs["srcPath"] != nil {
			setIfMissing(inputs, "context", node.Inputs["srcPath"])
		} else {
			setIfMissing(inputs, "context", node.Inputs["workingDirectory"])
		}
		// Try dev command first, then start, then generic command
		if cmd := node.Inputs["dev"]; cmd != nil {
			setIfMissing(inputs, "command", cmd)
		} else if cmd := node.Inputs["start"]; cmd != nil {
			setIfMissing(inputs, "command", cmd)
		} else {
			setIfMissing(inputs, "command", node.Inputs["command"])
		}
		setIfMissing(inputs, "runtime", node.Inputs["runtime"])
		setIfMissing(inputs, "framework", node.Inputs["framework"])

		// Resolve port for readiness check (but do NOT inject PORT into environment --
		// applications opt in via the ports resource and ${{ ports.<name>.port }})
		env := getEnvironmentMap(node.Inputs["environment"])
		port := e.resolvePortForWorkload(node)
		// Auto-inject OTEL_* env vars if observability.inject is true
		e.injectOTelEnvironmentIfEnabled(env, node)
		inputs["environment"] = env
		inputs["port"] = port

	case "container":
		setIfMissing(inputs, "name", standardName)
		// First try direct image input, then look for build dependency output
		if node.Inputs["image"] != nil {
			setIfMissing(inputs, "image", node.Inputs["image"])
		} else {
			// Look for image from build dependency
			buildImage := e.getBuildImageForNode(node)
			if buildImage != "" {
				setIfMissing(inputs, "image", buildImage)
			}
		}
		setIfMissing(inputs, "command", node.Inputs["command"])
		setIfMissing(inputs, "entrypoint", node.Inputs["entrypoint"])
		setIfMissing(inputs, "network", networkName)
		setIfMissing(inputs, "cpu", node.Inputs["cpu"])
		setIfMissing(inputs, "memory", node.Inputs["memory"])
		setIfMissing(inputs, "liveness_probe", node.Inputs["liveness_probe"])

		// Resolve port for readiness check (but do NOT inject PORT into environment --
		// applications opt in via the ports resource and ${{ ports.<name>.port }})
		env := getEnvironmentMap(node.Inputs["environment"])
		port := e.resolvePortForWorkload(node)
		// Auto-inject OTEL_* env vars if observability.inject is true
		e.injectOTelEnvironmentIfEnabled(env, node)
		inputs["environment"] = env
		inputs["port"] = port

	case "task":
		// Docker-based task execution (e.g., database migrations with an image)
		setIfMissing(inputs, "name", standardName)
		if node.Inputs["image"] != nil {
			setIfMissing(inputs, "image", node.Inputs["image"])
		} else {
			buildImage := e.getBuildImageForNode(node)
			if buildImage != "" {
				setIfMissing(inputs, "image", buildImage)
			}
		}
		setIfMissing(inputs, "command", node.Inputs["command"])
		setIfMissing(inputs, "network", networkName)
		setIfMissing(inputs, "environment", node.Inputs["environment"])

	case "process_task":
		// Process-based task execution (e.g., database migrations with runtime or bare process)
		setIfMissing(inputs, "name", standardName)
		setIfMissing(inputs, "command", node.Inputs["command"])
		setIfMissing(inputs, "environment", node.Inputs["environment"])
		setIfMissing(inputs, "context", node.Inputs["workingDirectory"])
		setIfMissing(inputs, "runtime", node.Inputs["runtime"])

	default:
		// For unknown modules, pass node inputs directly
		for k, v := range node.Inputs {
			setIfMissing(inputs, k, v)
		}
	}

	return inputs
}

func getStringVar(vars map[string]interface{}, key, defaultValue string) string {
	if v, ok := vars[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultValue
}

func setIfMissing(m map[string]interface{}, key string, value interface{}) {
	if _, exists := m[key]; !exists && value != nil {
		m[key] = value
	}
}

// toIntSafe converts an interface{} to int, handling int, float64, and string types.
// Returns 0 if the value is nil or cannot be converted.
func toIntSafe(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case int64:
		return int(val)
	case string:
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
	}
	return 0
}

// getEnvironmentMap extracts an environment map from node inputs, handling various types.
func getEnvironmentMap(value interface{}) map[string]string {
	result := make(map[string]string)
	if value == nil {
		return result
	}

	switch v := value.(type) {
	case map[string]string:
		for k, val := range v {
			result[k] = val
		}
	case map[string]interface{}:
		for k, val := range v {
			if s, ok := val.(string); ok {
				result[k] = s
			} else {
				result[k] = fmt.Sprintf("%v", val)
			}
		}
	}
	return result
}

// enrichObservabilityOutputs computes merged attributes after an observability node completes.
// It combines: auto-generated attributes (service.namespace, deployment.environment)
// + datacenter hook attributes (from outputs) + component attributes (from inputs).
// Component attributes override datacenter attributes, which override auto-generated ones.
// The result is stored as a comma-separated "key=value" string in the node's "attributes" output.
func (e *Executor) enrichObservabilityOutputs(node *graph.Node) {
	if node.Outputs == nil {
		node.Outputs = make(map[string]interface{})
	}

	// Start with auto-generated attributes (lowest priority)
	merged := make(map[string]string)
	merged["service.namespace"] = node.Component
	if e.graph != nil && e.graph.Environment != "" {
		merged["deployment.environment"] = e.graph.Environment
	}

	// Layer on datacenter-provided attributes (from hook outputs)
	if dcAttrsRaw, ok := node.Outputs["attributes"]; ok {
		switch dcAttrs := dcAttrsRaw.(type) {
		case map[string]interface{}:
			for k, v := range dcAttrs {
				merged[k] = fmt.Sprintf("%v", v)
			}
		case map[string]string:
			for k, v := range dcAttrs {
				merged[k] = v
			}
		case string:
			// Handle pre-formatted "key=value,key=value" string from datacenter
			for _, pair := range strings.Split(dcAttrs, ",") {
				pair = strings.TrimSpace(pair)
				if eqIdx := strings.Index(pair, "="); eqIdx > 0 {
					merged[strings.TrimSpace(pair[:eqIdx])] = strings.TrimSpace(pair[eqIdx+1:])
				}
			}
		}
	}

	// Layer on component-declared attributes (highest priority, from node inputs)
	if compAttrs, ok := node.Inputs["attributes"].(map[string]string); ok {
		for k, v := range compAttrs {
			merged[k] = v
		}
	} else if compAttrs, ok := node.Inputs["attributes"].(map[string]interface{}); ok {
		for k, v := range compAttrs {
			merged[k] = fmt.Sprintf("%v", v)
		}
	}

	// Format as comma-separated key=value string
	var pairs []string
	for k, v := range merged {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
	}

	// Sort for deterministic output
	sortStrings(pairs)
	node.Outputs["attributes"] = strings.Join(pairs, ",")
}

// sortStrings sorts a slice of strings in place (simple insertion sort to avoid importing sort).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// injectOTelEnvironmentIfEnabled checks whether the component's observability config
// has inject=true. If so, it looks up the completed observability node and merges
// standard OTEL_* environment variables into the workload's env map.
// Component-declared env vars always take precedence (no overwrite).
func (e *Executor) injectOTelEnvironmentIfEnabled(env map[string]string, node *graph.Node) {
	if e.graph == nil {
		return
	}

	// Find the observability node for this component
	obsNodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeObservability, "observability")
	obsNode := e.graph.GetNode(obsNodeID)
	if obsNode == nil || obsNode.State != graph.NodeStateCompleted {
		return
	}

	// Check the inject flag -- only auto-inject when the component opted in
	inject, _ := obsNode.Inputs["inject"].(bool)
	if !inject {
		return
	}

	// Extract outputs from the observability hook
	endpoint, _ := obsNode.Outputs["endpoint"].(string)
	protocol, _ := obsNode.Outputs["protocol"].(string)

	if endpoint == "" {
		return // No endpoint means the hook didn't produce useful output
	}

	// Auto-generate service name from component and workload name
	serviceName := fmt.Sprintf("%s-%s", node.Component, node.Name)

	// Use pre-merged attributes from enrichObservabilityOutputs (includes datacenter +
	// component + auto-generated attributes like service.namespace, deployment.environment)
	mergedAttrs, _ := obsNode.Outputs["attributes"].(string)

	// Append service.type for the specific workload (deployment, function, database, etc.)
	// so log queries can filter by resource type.
	nodeTypeAttr := fmt.Sprintf("service.type=%s", string(node.Type))
	if mergedAttrs != "" {
		mergedAttrs = mergedAttrs + "," + nodeTypeAttr
	} else {
		mergedAttrs = nodeTypeAttr
	}

	// Inject standard OTEL env vars (without overwriting component-declared vars).
	// All exporters are set to "otlp". To disable a specific signal, component authors
	// can explicitly set e.g. OTEL_METRICS_EXPORTER: none in their environment block.
	otelSetIfMissing(env, "OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	if protocol != "" {
		otelSetIfMissing(env, "OTEL_EXPORTER_OTLP_PROTOCOL", protocol)
	}
	otelSetIfMissing(env, "OTEL_SERVICE_NAME", serviceName)
	otelSetIfMissing(env, "OTEL_LOGS_EXPORTER", "otlp")
	otelSetIfMissing(env, "OTEL_TRACES_EXPORTER", "otlp")
	otelSetIfMissing(env, "OTEL_METRICS_EXPORTER", "otlp")
	if mergedAttrs != "" {
		otelSetIfMissing(env, "OTEL_RESOURCE_ATTRIBUTES", mergedAttrs)
	}
}

// otelSetIfMissing sets an environment variable only if it's not already set.
func otelSetIfMissing(env map[string]string, key, value string) {
	if _, exists := env[key]; !exists {
		env[key] = value
	}
}

// debugDumpNodeConfig writes a human-readable dump of the node's resolved
// configuration to stderr. This is only called when CLDCTL_DEBUG is set.
func debugDumpNodeConfig(change *planner.ResourceChange) {
	node := change.Node
	fmt.Fprintf(os.Stderr, "\n[debug] ─── %s %s/%s/%s ───\n", change.Action, node.Component, node.Type, node.Name)

	if len(node.Inputs) == 0 {
		fmt.Fprintf(os.Stderr, "[debug]   (no inputs)\n")
		return
	}

	// Sort keys for deterministic output.
	keys := make([]string, 0, len(node.Inputs))
	for k := range node.Inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := node.Inputs[k]
		switch val := v.(type) {
		case map[string]string:
			if len(val) == 0 {
				continue
			}
			fmt.Fprintf(os.Stderr, "[debug]   %s:\n", k)
			subKeys := make([]string, 0, len(val))
			for sk := range val {
				subKeys = append(subKeys, sk)
			}
			sort.Strings(subKeys)
			for _, sk := range subKeys {
				fmt.Fprintf(os.Stderr, "[debug]     %s: %s\n", sk, val[sk])
			}
		case map[string]interface{}:
			if len(val) == 0 {
				continue
			}
			fmt.Fprintf(os.Stderr, "[debug]   %s:\n", k)
			subKeys := make([]string, 0, len(val))
			for sk := range val {
				subKeys = append(subKeys, sk)
			}
			sort.Strings(subKeys)
			for _, sk := range subKeys {
				fmt.Fprintf(os.Stderr, "[debug]     %s: %v\n", sk, val[sk])
			}
		default:
			if v == nil {
				continue
			}
			fmt.Fprintf(os.Stderr, "[debug]   %s: %v\n", k, v)
		}
	}
}

// extractVersionFromType extracts the version part from a "type:version" string.
// For example, "postgres:^16" returns "^16", and "postgres" returns "".
func extractVersionFromType(typeInput interface{}) string {
	typeStr, ok := typeInput.(string)
	if !ok || typeStr == "" {
		return ""
	}
	parts := strings.SplitN(typeStr, ":", 2)
	if len(parts) > 1 {
		version := parts[1]
		// Strip semver range prefixes (^, ~, >=, <=, =) so the version can
		// be used directly as a Docker image tag (e.g., "^16" → "16").
		version = strings.TrimLeft(version, "^~>=<!")
		return version
	}
	return ""
}

// sanitizeResourceName replaces characters that are invalid in Docker resource names
// (container names, volume names, etc.) with hyphens. Docker allows [a-zA-Z0-9][a-zA-Z0-9_.-].
// This is primarily needed when component names contain slashes (e.g., "questra/app").
func sanitizeResourceName(name string) string {
	var b strings.Builder
	for i, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteRune(c)
		case c == '_' || c == '.' || c == '-':
			b.WriteRune(c)
		default:
			// Replace invalid characters (like '/') with '-'
			// Avoid leading hyphen
			if i > 0 {
				b.WriteRune('-')
			}
		}
	}
	return b.String()
}

// executePortAllocation handles port node allocation with a three-tier priority:
// 1. Environment override (from ComponentPorts)
// 2. Datacenter port hook (if defined)
// 3. Built-in deterministic hash-based fallback
func (e *Executor) executePortAllocation(ctx context.Context, change *planner.ResourceChange, envState *types.EnvironmentState) *NodeResult {
	result := &NodeResult{
		NodeID: change.Node.ID,
		Action: change.Action,
	}

	componentName := change.Node.Component
	portName := change.Node.Name

	var allocatedPort int
	var allocated bool

	// Priority 1: Environment override
	if e.options.ComponentPorts != nil {
		if compPorts, ok := e.options.ComponentPorts[componentName]; ok {
			if port, ok := compPorts[portName]; ok {
				allocatedPort = port
				allocated = true
			}
		}
	}

	// Priority 2: Datacenter port hook
	if !allocated {
		hooks := e.getHooksForType(graph.NodeTypePort)
		if len(hooks) > 0 {
			// Dispatch to the hook like any other resource type
			modulePath, moduleInputs, pluginName, err := e.findMatchingHook(change.Node, envState.Name)
			if err == nil {
				if pluginName == "" {
					pluginName = "native"
				}
				plugin, err := e.iacRegistry.Get(pluginName)
				if err == nil {
					runOpts := iac.RunOptions{
						ModuleSource: modulePath,
						Inputs:       moduleInputs,
						Environment:  map[string]string{},
					}
					applyResult, err := plugin.Apply(ctx, runOpts)
					if err == nil && applyResult != nil {
						if portVal, ok := applyResult.Outputs["port"]; ok {
							switch v := portVal.Value.(type) {
							case int:
								allocatedPort = v
								allocated = true
							case float64:
								allocatedPort = int(v)
								allocated = true
							case string:
								if p, err := strconv.Atoi(v); err == nil {
									allocatedPort = p
									allocated = true
								}
							}
						}
					}
				}
			}
		}
	}

	// Priority 3: Built-in deterministic hash fallback
	if !allocated {
		allocatedPort = stablePortForNode(envState.Name, componentName, portName)
	}

	// Save to state
	outputs := map[string]interface{}{
		"port": allocatedPort,
	}

	e.stateMu.Lock()
	if envState.Components == nil {
		envState.Components = make(map[string]*types.ComponentState)
	}
	compState := envState.Components[componentName]
	if compState == nil {
		compState = e.newComponentState(componentName)
		envState.Components[componentName] = compState
	}
	if compState.Resources == nil {
		compState.Resources = make(map[string]*types.ResourceState)
	}
	compState.Resources[resourceKey(change.Node)] = &types.ResourceState{
		Component: componentName,
		Name:      portName,
		Type:      string(change.Node.Type),
		Status:    types.ResourceStatusReady,
		Inputs:    change.Node.Inputs,
		Outputs:   outputs,
		UpdatedAt: time.Now(),
	}
	e.saveStateLocked(envState)
	e.stateMu.Unlock()

	result.Success = true
	result.Outputs = outputs
	return result
}

// injectParentDatabaseOutputs copies the parent database node's host and port
// outputs into the databaseUser node's inputs. This bridges the gap between the
// database (which knows its actual allocated port) and the databaseUser hook
// (which needs the port to construct correct connection URLs). The database
// node is a dependency of the databaseUser, so it has already completed.
func (e *Executor) injectParentDatabaseOutputs(node *graph.Node) {
	if e.graph == nil {
		return
	}
	dbName, _ := node.Inputs["database"].(string)
	if dbName == "" {
		return
	}
	dbNodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeDatabase, dbName)
	dbNode := e.graph.GetNode(dbNodeID)
	if dbNode == nil || dbNode.Outputs == nil {
		return
	}
	if host, ok := dbNode.Outputs["host"]; ok {
		node.SetInput("host", host)
	}
	if port, ok := dbNode.Outputs["port"]; ok {
		node.SetInput("port", port)
	}
}

// executeDatabaseUserPassthrough handles databaseUser nodes when no matching hook
// exists for the specific database type (e.g., a Redis database when only a Postgres
// databaseUser hook is defined). It copies the parent database node's outputs so that
// consumers receive the same connection credentials as a direct database reference.
func (e *Executor) executeDatabaseUserPassthrough(ctx context.Context, change *planner.ResourceChange, envState *types.EnvironmentState) *NodeResult {
	result := &NodeResult{
		NodeID: change.Node.ID,
		Action: change.Action,
	}

	// Find the parent database node from the graph
	outputs := make(map[string]interface{})
	if e.graph != nil {
		dbName, _ := change.Node.Inputs["database"].(string)
		if dbName != "" {
			dbNodeID := fmt.Sprintf("%s/%s/%s", change.Node.Component, graph.NodeTypeDatabase, dbName)
			dbNode := e.graph.GetNode(dbNodeID)
			if dbNode != nil && dbNode.Outputs != nil {
				for k, v := range dbNode.Outputs {
					outputs[k] = v
				}
			}
		}
	}

	// Save to state
	e.stateMu.Lock()
	if envState.Components == nil {
		envState.Components = make(map[string]*types.ComponentState)
	}
	compState := envState.Components[change.Node.Component]
	if compState == nil {
		compState = e.newComponentState(change.Node.Component)
		envState.Components[change.Node.Component] = compState
	}
	resMap := e.getResourceMap(compState, change.Node)
	resMap[resourceKey(change.Node)] = &types.ResourceState{
		Component: change.Node.Component,
		Name:      change.Node.Name,
		Type:      string(change.Node.Type),
		Status:    types.ResourceStatusReady,
		Inputs:    change.Node.Inputs,
		Outputs:   outputs,
		UpdatedAt: time.Now(),
	}
	e.saveStateLocked(envState)
	e.stateMu.Unlock()

	result.Success = true
	result.Outputs = outputs
	return result
}

// executeNetworkPolicyNoop handles networkPolicy nodes when no matching hook exists
// for the specific workload-service pair. It completes the node immediately with no
// outputs or side effects.
func (e *Executor) executeNetworkPolicyNoop(ctx context.Context, change *planner.ResourceChange, envState *types.EnvironmentState) *NodeResult {
	result := &NodeResult{
		NodeID: change.Node.ID,
		Action: change.Action,
	}

	outputs := make(map[string]interface{})

	// Save to state
	e.stateMu.Lock()
	if envState.Components == nil {
		envState.Components = make(map[string]*types.ComponentState)
	}
	compState := envState.Components[change.Node.Component]
	if compState == nil {
		compState = e.newComponentState(change.Node.Component)
		envState.Components[change.Node.Component] = compState
	}
	resMap := e.getResourceMap(compState, change.Node)
	resMap[resourceKey(change.Node)] = &types.ResourceState{
		Component: change.Node.Component,
		Name:      change.Node.Name,
		Type:      string(change.Node.Type),
		Status:    types.ResourceStatusReady,
		Inputs:    change.Node.Inputs,
		Outputs:   outputs,
		UpdatedAt: time.Now(),
	}
	e.saveStateLocked(envState)
	e.stateMu.Unlock()

	result.Success = true
	result.Outputs = outputs
	return result
}

// stablePortForNode produces a deterministic port from env/component/port names.
// Uses the same hashCode helper already used for database port offsets.
func stablePortForNode(envName, componentName, portName string) int {
	key := fmt.Sprintf("%s/%s/%s", envName, componentName, portName)
	h := hashCode(key)
	// Map into range [10000, 60000) -- avoids privileged ports and common dev ports
	return 10000 + (h % 50000)
}

func hashCode(s string) int {
	h := 0
	for _, c := range s {
		h = 31*h + int(c)
	}
	if h < 0 {
		h = -h
	}
	return h
}

// resolveComponentExpressions resolves ${{ }} component expressions in a node's inputs
// by looking at completed dependency nodes' outputs. Handles expressions like:
//   - ${{ builds.api.image }}      → dockerBuild node output
//   - ${{ databases.main.url }}    → database node output
//   - ${{ services.api.host }}     → service node output
//   - ${{ observability.endpoint }} → observability node output
//
// Also recurses into nested maps (e.g., environment map) to resolve expressions there.
// envState is used to look up cross-component dependency outputs (dependencies.<name>.outputs.<key>).
func (e *Executor) resolveComponentExpressions(node *graph.Node, envState *types.EnvironmentState) {
	if e.graph == nil {
		return
	}

	// Resolve component variables from executor options
	compVars := make(map[string]interface{})
	if e.options.ComponentVariables != nil {
		if vars, ok := e.options.ComponentVariables[node.Component]; ok {
			compVars = vars
		}
	}

	exprPattern := regexp.MustCompile(`\$\{\{\s*([^}]+)\s*\}\}`)

	// applyPipeFuncs processes pipe functions (e.g., "| default 'fallback'")
	// on a resolved string value. Supported functions: default.
	applyPipeFuncs := func(value string, pipeFuncs []string) string {
		for _, pipeStr := range pipeFuncs {
			fields := strings.Fields(strings.TrimSpace(pipeStr))
			if len(fields) == 0 {
				continue
			}
			funcName := fields[0]
			switch funcName {
			case "default":
				if len(fields) >= 2 && value == "" {
					value = strings.Trim(fields[1], `"'`)
				}
			}
		}
		return value
	}

	resolveStr := func(strVal string) string {
		if !strings.Contains(strVal, "${{") {
			return strVal
		}
		return exprPattern.ReplaceAllStringFunc(strVal, func(match string) string {
			inner := match[3 : len(match)-2]
			inner = strings.TrimSpace(inner)

			// Split pipe functions from the reference path.
			// E.g., "smtp.email.username | default 'unused'" → ref="smtp.email.username", pipes=["default 'unused'"]
			pipeParts := strings.Split(inner, "|")
			refStr := strings.TrimSpace(pipeParts[0])
			var pipeFuncs []string
			for _, p := range pipeParts[1:] {
				pipeFuncs = append(pipeFuncs, strings.TrimSpace(p))
			}

			parts := strings.Split(refStr, ".")

			if len(parts) < 2 {
				return match // Malformed expression — preserve as-is
			}

			// debugUnresolved emits a debug-level warning when an expression cannot
			// be resolved. The expression resolves to "" so applications receive an
			// empty string instead of a literal "${{ ... }}" value.
			debugUnresolved := func(reason string) string {
				if os.Getenv("CLDCTL_DEBUG") != "" {
					fmt.Fprintf(os.Stderr, "[debug] unresolved expression %s in %s/%s: %s\n",
						refStr, node.Type, node.Name, reason)
				}
				return ""
			}

			// Resolve the reference, then apply any pipe functions.
			resolved := func() string {

			resourceType := parts[0]
			switch resourceType {
			case "builds":
				if len(parts) < 3 {
					return debugUnresolved("malformed builds expression (expected builds.<name>.<output>)")
				}
				buildNodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeDockerBuild, parts[1])
				buildNode, ok := e.graph.Nodes[buildNodeID]
				if !ok || buildNode.Outputs == nil {
					return debugUnresolved(fmt.Sprintf("build %q not found or has no outputs", parts[1]))
				}
				if val, ok := buildNode.Outputs[parts[2]]; ok {
					return fmt.Sprintf("%v", val)
				}
				return debugUnresolved(fmt.Sprintf("build %q has no output %q", parts[1], parts[2]))

			case "databases":
				if len(parts) < 3 {
					return debugUnresolved("malformed databases expression (expected databases.<name>.<output>)")
				}
				dbName := parts[1]

				// Resolve the parent database node (always needed as a fallback source).
				dbNodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeDatabase, dbName)
				dbNode, dbOK := e.graph.Nodes[dbNodeID]

				// If the datacenter defines a databaseUser hook, resolve through the
				// interposed databaseUser node first for per-consumer credentials.
				dbUserNodeID := fmt.Sprintf("%s/%s/%s--%s", node.Component, graph.NodeTypeDatabaseUser, dbName, node.Name)
				depNode, ok := e.graph.Nodes[dbUserNodeID]
				if !ok || depNode == nil || depNode.Outputs == nil {
					// No databaseUser node — resolve directly from the database node
					if !dbOK || dbNode.Outputs == nil {
						return debugUnresolved(fmt.Sprintf("database %q not found or has no outputs", dbName))
					}
					depNode = dbNode
				}
				// Handle read/write sub-objects: databases.<name>.read.<prop> / databases.<name>.write.<prop>
				if (parts[2] == "read" || parts[2] == "write") && len(parts) >= 4 {
					if nested, ok := depNode.Outputs[parts[2]]; ok {
						if nestedMap, ok := nested.(map[string]interface{}); ok {
							if val, ok := nestedMap[parts[3]]; ok {
								return fmt.Sprintf("%v", val)
							}
						}
					}
					// Fallback to top-level output when read/write is not explicitly set
					if val, ok := depNode.Outputs[parts[3]]; ok {
						return fmt.Sprintf("%v", val)
					}
					// Per-field fallback: if the databaseUser node doesn't have this field,
					// try the parent database node (e.g., host/port come from the database).
					if depNode.Type == graph.NodeTypeDatabaseUser && dbOK && dbNode.Outputs != nil {
						if nested, ok := dbNode.Outputs[parts[2]]; ok {
							if nestedMap, ok := nested.(map[string]interface{}); ok {
								if val, ok := nestedMap[parts[3]]; ok {
									return fmt.Sprintf("%v", val)
								}
							}
						}
						if val, ok := dbNode.Outputs[parts[3]]; ok {
							return fmt.Sprintf("%v", val)
						}
					}
					return debugUnresolved(fmt.Sprintf("database %q has no output %s.%s", dbName, parts[2], parts[3]))
				}
				if val, ok := depNode.Outputs[parts[2]]; ok {
					return fmt.Sprintf("%v", val)
				}
				// Per-field fallback: if the databaseUser node doesn't have this field,
				// try the parent database node (e.g., host/port come from the database
				// while username/password/url come from the databaseUser hook).
				if depNode.Type == graph.NodeTypeDatabaseUser && dbOK && dbNode.Outputs != nil {
					if val, ok := dbNode.Outputs[parts[2]]; ok {
						return fmt.Sprintf("%v", val)
					}
				}
				return debugUnresolved(fmt.Sprintf("database %q has no output %q", dbName, parts[2]))

			case "services":
				if len(parts) < 3 {
					return debugUnresolved("malformed services expression (expected services.<name>.<output>)")
				}
				nodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeService, parts[1])
				depNode, ok := e.graph.Nodes[nodeID]
				if !ok || depNode.Outputs == nil {
					return debugUnresolved(fmt.Sprintf("service %q not found or has no outputs", parts[1]))
				}
				if val, ok := depNode.Outputs[parts[2]]; ok {
					return fmt.Sprintf("%v", val)
				}
				return debugUnresolved(fmt.Sprintf("service %q has no output %q", parts[1], parts[2]))

			case "buckets":
				if len(parts) < 3 {
					return debugUnresolved("malformed buckets expression (expected buckets.<name>.<output>)")
				}
				nodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeBucket, parts[1])
				depNode, ok := e.graph.Nodes[nodeID]
				if !ok || depNode.Outputs == nil {
					return debugUnresolved(fmt.Sprintf("bucket %q not found or has no outputs", parts[1]))
				}
				if val, ok := depNode.Outputs[parts[2]]; ok {
					return fmt.Sprintf("%v", val)
				}
				return debugUnresolved(fmt.Sprintf("bucket %q has no output %q", parts[1], parts[2]))

			case "routes":
				if len(parts) < 3 {
					return debugUnresolved("malformed routes expression (expected routes.<name>.<output>)")
				}
				nodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeRoute, parts[1])
				depNode, ok := e.graph.Nodes[nodeID]
				if !ok || depNode.Outputs == nil {
					return debugUnresolved(fmt.Sprintf("route %q not found or has no outputs", parts[1]))
				}
				if val, ok := depNode.Outputs[parts[2]]; ok {
					return fmt.Sprintf("%v", val)
				}
				return debugUnresolved(fmt.Sprintf("route %q has no output %q", parts[1], parts[2]))

			case "ports":
				if len(parts) < 3 {
					return debugUnresolved("malformed ports expression (expected ports.<name>.<output>)")
				}
				nodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypePort, parts[1])
				depNode, ok := e.graph.Nodes[nodeID]
				if !ok || depNode.Outputs == nil {
					return debugUnresolved(fmt.Sprintf("port %q not found or has no outputs", parts[1]))
				}
				if val, ok := depNode.Outputs[parts[2]]; ok {
					return fmt.Sprintf("%v", val)
				}
				return debugUnresolved(fmt.Sprintf("port %q has no output %q", parts[1], parts[2]))

			case "observability":
				// observability is a singleton per component
				obsNodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeObservability, "observability")
				obsNode, ok := e.graph.Nodes[obsNodeID]
				if !ok || obsNode.Outputs == nil {
					return "" // No observability hook — resolve silently
				}
				prop := parts[1]
				if val, ok := obsNode.Outputs[prop]; ok {
					return fmt.Sprintf("%v", val)
				}
				return "" // Unknown observability property — resolve silently

			case "variables":
				// Resolve from component deployment variables
				if len(parts) < 2 {
					return debugUnresolved("malformed variables expression (expected variables.<name>)")
				}
				varName := parts[1]
				if val, ok := compVars[varName]; ok {
					return fmt.Sprintf("%v", val)
				}
				// Fallback: check if stored as variables_<name> in node inputs
				if val, ok := node.Inputs["variables_"+varName]; ok {
					return fmt.Sprintf("%v", val)
				}
				return debugUnresolved(fmt.Sprintf("variable %q not provided", varName))

			case "encryptionKeys":
				if len(parts) < 3 {
					return debugUnresolved("malformed encryptionKeys expression (expected encryptionKeys.<name>.<output>)")
				}
				nodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeEncryptionKey, parts[1])
				depNode, ok := e.graph.Nodes[nodeID]
				if !ok || depNode.Outputs == nil {
					return debugUnresolved(fmt.Sprintf("encryptionKey %q not found or has no outputs", parts[1]))
				}
				if val, ok := depNode.Outputs[parts[2]]; ok {
					return fmt.Sprintf("%v", val)
				}
				return debugUnresolved(fmt.Sprintf("encryptionKey %q has no output %q", parts[1], parts[2]))

			case "smtp":
				if len(parts) < 3 {
					return debugUnresolved("malformed smtp expression (expected smtp.<name>.<output>)")
				}
				nodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeSMTP, parts[1])
				depNode, ok := e.graph.Nodes[nodeID]
				if !ok || depNode.Outputs == nil {
					return debugUnresolved(fmt.Sprintf("smtp %q not found or has no outputs", parts[1]))
				}
				if val, ok := depNode.Outputs[parts[2]]; ok {
					return fmt.Sprintf("%v", val)
				}
				return debugUnresolved(fmt.Sprintf("smtp %q has no output %q", parts[1], parts[2]))

			case "dependencies":
				// Resolve cross-component dependency outputs.
				// Format: dependencies.<depAlias>.outputs.<outputKey>
				// or:     dependencies.<depAlias>.<outputKey>
				if len(parts) < 3 {
					return debugUnresolved("malformed dependencies expression (expected dependencies.<name>.outputs.<key>)")
				}
				depAlias := parts[1]

				// Resolve the dependency alias to the actual component name.
				// E.g., "clerk" → "questra/clerk" via DependencyTargets.
				targetComp := depAlias
				if e.graph.DependencyTargets != nil {
					if targets, ok := e.graph.DependencyTargets[node.Component]; ok {
						if tc, ok := targets[depAlias]; ok {
							targetComp = tc
						}
					}
				}

				// Determine the output key (handle both with and without "outputs" segment)
				var outputKey string
				if len(parts) >= 4 && parts[2] == "outputs" {
					outputKey = parts[3]
				} else {
					outputKey = parts[2]
				}

				// Try 1: look up component-level outputs from the graph
				// (for pass-through components with outputs but no resources,
				// resolved during the current session)
				if e.graph.ComponentOutputExprs != nil {
					if outExprs, ok := e.graph.ComponentOutputExprs[targetComp]; ok {
						if exprStr, ok := outExprs[outputKey]; ok {
							// Resolve the output expression inline using the
							// dependency component's variables
							depVars := make(map[string]interface{})
							if e.options.ComponentVariables != nil {
								if vars, ok := e.options.ComponentVariables[targetComp]; ok {
									depVars = vars
								}
							}
							resolved := exprPattern.ReplaceAllStringFunc(exprStr, func(m string) string {
								innerM := m[3 : len(m)-2]
								innerM = strings.TrimSpace(innerM)
								mParts := strings.Split(innerM, ".")
								if len(mParts) >= 2 && mParts[0] == "variables" {
									if v, ok := depVars[mParts[1]]; ok {
										return fmt.Sprintf("%v", v)
									}
								}
								return ""
							})
							return resolved
						}
					}
				}

				// Try 2: look up component-level outputs from environment state
				// (for components deployed in a previous session)
				if envState != nil {
					if depComp, ok := envState.Components[targetComp]; ok {
						// Check component-level outputs first
						if depComp.Outputs != nil {
							if val, ok := depComp.Outputs[outputKey]; ok {
								return fmt.Sprintf("%v", val)
							}
						}
						// Fall back to resource-level outputs
						for _, res := range depComp.Resources {
							if res.Outputs != nil {
								if val, ok := res.Outputs[outputKey]; ok {
									return fmt.Sprintf("%v", val)
								}
							}
						}
					}
				}

				// Try 3: look up resource-level outputs from graph nodes
				// (for components deployed in the same session with resources)
				for _, graphNode := range e.graph.Nodes {
					if graphNode.Component == targetComp && graphNode.Outputs != nil {
						if val, ok := graphNode.Outputs[outputKey]; ok {
							return fmt.Sprintf("%v", val)
						}
					}
				}

				// Dependency not found or doesn't expose this output key.
				// For optional dependencies this is expected — resolve silently to "".
				// For required dependencies emit a debug warning.
				isOptional := e.graph.OptionalDependencies != nil &&
					e.graph.OptionalDependencies[node.Component] != nil &&
					e.graph.OptionalDependencies[node.Component][depAlias]
				if isOptional {
					return "" // Silently resolve optional dep to empty string
				}
				return debugUnresolved(fmt.Sprintf("dependency %q (component %q) output %q not available", depAlias, targetComp, outputKey))

			default:
				return debugUnresolved(fmt.Sprintf("unknown expression prefix %q", resourceType))
			}

			}() // end resolved func
			return applyPipeFuncs(resolved, pipeFuncs)
		})
	}

	for key, value := range node.Inputs {
		switch v := value.(type) {
		case string:
			node.Inputs[key] = resolveStr(v)
		case map[string]string:
			resolved := make(map[string]string, len(v))
			for k, val := range v {
				resolved[k] = resolveStr(val)
			}
			node.Inputs[key] = resolved
		case map[string]interface{}:
			resolved := make(map[string]interface{}, len(v))
			for k, val := range v {
				if s, ok := val.(string); ok {
					resolved[k] = resolveStr(s)
				} else {
					resolved[k] = val
				}
			}
			node.Inputs[key] = resolved
		}
	}
}

// getBuildImageForNode looks up the built image from build dependencies.
// For deployments that have a dockerBuild dependency, this returns the image produced by that build.
func (e *Executor) getBuildImageForNode(node *graph.Node) string {
	if e.graph == nil {
		return ""
	}

	// Look through dependencies for a dockerBuild node
	for _, depID := range node.DependsOn {
		depNode, ok := e.graph.Nodes[depID]
		if !ok {
			continue
		}

		// Check if this dependency is a dockerBuild node
		if depNode.Type == graph.NodeTypeDockerBuild {
			// Get the image from the build node's outputs
			if depNode.Outputs != nil {
				if image, ok := depNode.Outputs["image"].(string); ok && image != "" {
					return image
				}
			}
		}
	}

	return ""
}

// resolvePortForWorkload determines the port a deployment or function should listen on.
// Priority: 1) node's own port property, 2) unresolved port expression lookup,
// 3) associated service's port.
func (e *Executor) resolvePortForWorkload(node *graph.Node) int {
	// First, check if the node has its own port property.
	// After expression resolution, the port may be an int, float64, or string.
	if port := toIntSafe(node.Inputs["port"]); port > 0 {
		return port
	}

	// If the port is an unresolved expression (e.g., the node hasn't been processed
	// yet), try to resolve it by looking up the referenced port node's outputs directly.
	// This happens when a route resolves upstream_port for a function that hasn't
	// had its expressions resolved yet (because the function depends on the route).
	if portStr, ok := node.Inputs["port"].(string); ok && strings.Contains(portStr, "${{") {
		if port := e.resolvePortFromExpression(portStr, node.Component); port > 0 {
			return port
		}
	}

	// Fall back to looking up the associated service's port
	targetType := "deployment"
	if node.Type == graph.NodeTypeFunction {
		targetType = "function"
	}
	if port := e.lookupServicePortForTarget(node.Component, node.Name, targetType); port > 0 {
		return port
	}

	// Last resort: check the PORT environment variable. Functions and deployments
	// often set PORT=${{ ports.<name>.port }} without declaring an explicit port
	// property. After expression resolution, the env var holds the allocated port.
	if env := node.Inputs["environment"]; env != nil {
		switch envMap := env.(type) {
		case map[string]string:
			if portStr, ok := envMap["PORT"]; ok {
				if port := toIntSafe(portStr); port > 0 {
					return port
				}
				// Handle unresolved expression
				if strings.Contains(portStr, "${{") {
					if port := e.resolvePortFromExpression(portStr, node.Component); port > 0 {
						return port
					}
				}
			}
		case map[string]interface{}:
			if portVal, ok := envMap["PORT"]; ok {
				if port := toIntSafe(portVal); port > 0 {
					return port
				}
				if portStr, ok := portVal.(string); ok && strings.Contains(portStr, "${{") {
					if port := e.resolvePortFromExpression(portStr, node.Component); port > 0 {
						return port
					}
				}
			}
		}
	}

	return 0
}

// resolvePortFromExpression resolves a ${{ ports.<name>.port }} expression by looking
// up the port node's outputs directly from the graph. This is used when we need to
// determine a node's port before its expressions have been resolved (e.g., when a
// route handler looks up a function's port, but the function hasn't been processed yet).
func (e *Executor) resolvePortFromExpression(expr string, component string) int {
	if e.graph == nil {
		return 0
	}
	// Extract: "${{ ports.NAME.port }}" -> NAME
	trimmed := strings.TrimSpace(expr)
	trimmed = strings.TrimPrefix(trimmed, "${{")
	trimmed = strings.TrimSuffix(trimmed, "}}")
	trimmed = strings.TrimSpace(trimmed)
	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 || parts[0] != "ports" || parts[2] != "port" {
		return 0
	}
	portName := parts[1]
	portNodeID := fmt.Sprintf("%s/%s/%s", component, graph.NodeTypePort, portName)
	if portNode, ok := e.graph.Nodes[portNodeID]; ok && portNode.Outputs != nil {
		if val, ok := portNode.Outputs["port"]; ok {
			return toIntSafe(val)
		}
	}
	return 0
}

// lookupServicePortForTarget finds a service that references the given target (deployment/function name)
// and returns its port. Returns 0 if no service is found.
func (e *Executor) lookupServicePortForTarget(componentName, targetName, targetType string) int {
	if e.graph == nil {
		return 0
	}

	// Scan all nodes looking for services that reference this target
	for _, node := range e.graph.Nodes {
		if node.Type != graph.NodeTypeService {
			continue
		}
		if node.Component != componentName {
			continue
		}

		// Check if this service targets our deployment/function
		target, ok := node.Inputs["target"].(string)
		if !ok || target != targetName {
			continue
		}

		nodeTargetType, _ := node.Inputs["targetType"].(string)
		if nodeTargetType != "" && nodeTargetType != targetType {
			continue
		}

		// Found a matching service, get its port (may be int, float64, or string after expression resolution)
		if port := toIntSafe(node.Inputs["port"]); port > 0 {
			return port
		}
		// If the port is an unresolved expression, resolve it from the port node
		if portStr, ok := node.Inputs["port"].(string); ok && strings.Contains(portStr, "${{") {
			if port := e.resolvePortFromExpression(portStr, node.Component); port > 0 {
				return port
			}
		}
	}

	return 0
}

// evaluateInputExpression evaluates a simple HCL-like expression string.
// This is a simplified evaluator that handles common patterns.
func (e *Executor) evaluateInputExpression(expr string, node *graph.Node, envName string, dcVars map[string]interface{}) interface{} {
	expr = trimSpace(expr)

	// Handle static HCL map literals (no interpolation needed)
	if strings.HasPrefix(expr, "{") && strings.HasSuffix(expr, "}") && !strings.Contains(expr, "${") {
		if parsed := parseHCLMapLiteral(expr); parsed != nil {
			return parsed
		}
	}

	// Handle string interpolation ${...}
	if strings.Contains(expr, "${") {
		result := expr
		// Replace ${environment.name}
		result = strings.ReplaceAll(result, "${environment.name}", envName)
		// Replace ${node.name}
		result = strings.ReplaceAll(result, "${node.name}", node.Name)
		// Replace ${node.component} - sanitize for use in resource names
		result = strings.ReplaceAll(result, "${node.component}", sanitizeResourceName(node.Component))
		// Replace ${node.type}
		result = strings.ReplaceAll(result, "${node.type}", string(node.Type))
		// Replace ${node.instance.name} and ${node.instance.weight}
		if node.Instance != nil {
			result = strings.ReplaceAll(result, "${node.instance.name}", node.Instance.Name)
			result = strings.ReplaceAll(result, "${node.instance.weight}", fmt.Sprintf("%d", node.Instance.Weight))
		} else {
			// Single-instance mode defaults
			result = strings.ReplaceAll(result, "${node.instance.name}", "default")
			result = strings.ReplaceAll(result, "${node.instance.weight}", "100")
		}
		// Replace ${node.inputs.*}
		for k, v := range node.Inputs {
			if s, ok := v.(string); ok {
				result = strings.ReplaceAll(result, "${node.inputs."+k+"}", s)
			}
		}
		// Replace ${variable.*}
		for k, v := range dcVars {
			if s, ok := v.(string); ok {
				result = strings.ReplaceAll(result, "${variable."+k+"}", s)
			}
		}

		// After interpolation, check if the result is an HCL-style map literal
		// (e.g., inline map inputs from the datacenter stored as source text).
		// Parse it back into a Go map so downstream consumers get the correct type.
		trimmedResult := strings.TrimSpace(result)
		if strings.HasPrefix(trimmedResult, "{") && strings.HasSuffix(trimmedResult, "}") {
			if parsed := parseHCLMapLiteral(trimmedResult); parsed != nil {
				return parsed
			}
		}

		return result
	}

	// Handle direct references
	if hasPrefix(expr, "node.name") {
		return node.Name
	}
	if hasPrefix(expr, "node.component") {
		return sanitizeResourceName(node.Component)
	}
	if hasPrefix(expr, "node.type") {
		return string(node.Type)
	}
	if hasPrefix(expr, "node.instance.name") {
		if node.Instance != nil {
			return node.Instance.Name
		}
		return "default"
	}
	if hasPrefix(expr, "node.instance.weight") {
		if node.Instance != nil {
			return node.Instance.Weight
		}
		return 100
	}
	if hasPrefix(expr, "node.inputs.") {
		inputName := expr[12:] // len("node.inputs.")
		if val, ok := node.Inputs[inputName]; ok {
			return val
		}
		return nil
	}
	if hasPrefix(expr, "environment.name") {
		return envName
	}
	if hasPrefix(expr, "variable.") {
		varName := expr[9:] // len("variable.")
		if val, ok := dcVars[varName]; ok {
			return val
		}
		// Return default for common variables
		switch varName {
		case "network_name":
			return "cldctl-local"
		case "host":
			return "localhost"
		}
		return nil
	}

	// Handle element(list, index) function
	if hasPrefix(expr, "element(") && hasSuffix(expr, ")") {
		inner := expr[8 : len(expr)-1]
		args := splitFuncArgs(inner)
		if len(args) == 2 {
			listVal := e.evaluateInputExpression(trimSpace(args[0]), node, envName, dcVars)
			indexStr := trimSpace(args[1])
			index, _ := strconv.Atoi(indexStr)

			if list, ok := listVal.([]string); ok && index >= 0 && index < len(list) {
				return list[index]
			}
			return nil
		}
	}

	// Handle split(separator, string) function
	if hasPrefix(expr, "split(") && hasSuffix(expr, ")") {
		inner := expr[6 : len(expr)-1]
		args := splitFuncArgs(inner)
		if len(args) == 2 {
			sep := trimQuotes(trimSpace(args[0]))
			strVal := e.evaluateInputExpression(trimSpace(args[1]), node, envName, dcVars)
			if s, ok := strVal.(string); ok {
				return strings.Split(s, sep)
			}
		}
		return nil
	}

	// Handle try(expr, fallback) function
	if hasPrefix(expr, "try(") && hasSuffix(expr, ")") {
		inner := expr[4 : len(expr)-1]
		args := splitFuncArgs(inner)
		if len(args) >= 1 {
			result := e.evaluateInputExpression(trimSpace(args[0]), node, envName, dcVars)
			if result != nil {
				return result
			}
			if len(args) >= 2 {
				fallback := trimSpace(args[1])
				if fallback == "null" {
					return nil
				}
				return e.evaluateInputExpression(fallback, node, envName, dcVars)
			}
		}
		return nil
	}

	// Handle coalesce function: coalesce(value1, value2)
	if hasPrefix(expr, "coalesce(") && hasSuffix(expr, ")") {
		inner := expr[9 : len(expr)-1] // strip "coalesce(" and ")"
		parts := splitCoalesce(inner)
		for _, part := range parts {
			part = trimSpace(part)
			val := e.evaluateInputExpression(part, node, envName, dcVars)
			if val != nil && val != "" {
				return val
			}
		}
		return nil
	}

	// Handle quoted strings
	if len(expr) >= 2 && expr[0] == '"' && expr[len(expr)-1] == '"' {
		return expr[1 : len(expr)-1]
	}

	// Handle booleans
	if expr == "true" {
		return true
	}
	if expr == "false" {
		return false
	}

	// Return as-is for other expressions (numbers, etc.)
	return expr
}

// Helper functions for expression evaluation
func hasPrefix(s, prefix string) bool {
	return strings.HasPrefix(s, prefix)
}

func hasSuffix(s, suffix string) bool {
	return strings.HasSuffix(s, suffix)
}

func splitCoalesce(s string) []string {
	var parts []string
	var current string
	parenDepth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '(' {
			parenDepth++
			current += string(c)
		} else if c == ')' {
			parenDepth--
			current += string(c)
		} else if c == ',' && parenDepth == 0 {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// Helper functions for simple expression parsing
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func splitOnce(s, sep string) []string {
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			return []string{s[:i], s[i+len(sep):]}
		}
	}
	return []string{s}
}

func trimQuotes(s string) string {
	s = trimSpace(s)
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"') {
		return s[1 : len(s)-1]
	}
	return s
}

// parseHCLMapLiteral parses an HCL-style map literal string into a Go map.
// Handles the format produced when inline map literals from datacenter HCL
// are stored as source text and later interpolated:
//
//	{
//	  key = "value"
//	  key-with-hyphens = "other"
//	}
//
// Returns nil if the string is not a valid HCL map literal.
func parseHCLMapLiteral(s string) map[string]interface{} {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return nil
	}

	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return make(map[string]interface{})
	}

	result := make(map[string]interface{})
	lines := strings.Split(inner, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		eqIdx := strings.Index(line, "=")
		if eqIdx < 0 {
			return nil
		}

		key := strings.TrimSpace(line[:eqIdx])
		val := strings.TrimSpace(line[eqIdx+1:])

		// Strip quotes from value
		if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
			val = val[1 : len(val)-1]
		}

		result[key] = val
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

func extractInputRef(expr string, inputs map[string]interface{}) string {
	expr = trimSpace(expr)
	// Handle "node.inputs.<name>" pattern
	if len(expr) > 12 && expr[:12] == "node.inputs." {
		inputName := expr[12:]
		if val, ok := inputs[inputName]; ok {
			if s, ok := val.(string); ok {
				return s
			}
		}
	}
	return expr
}

func extractInputName(expr string) string {
	expr = trimSpace(expr)
	// Handle "node.inputs.<name> != null" pattern
	if len(expr) > 12 && expr[:12] == "node.inputs." {
		rest := expr[12:]
		// Find where the input name ends
		for i, c := range rest {
			if c == ' ' || c == '!' || c == '=' {
				return rest[:i]
			}
		}
		return rest
	}
	return ""
}

func (e *Executor) executeDestroy(ctx context.Context, change *planner.ResourceChange, envState *types.EnvironmentState) *NodeResult {
	result := &NodeResult{
		Action: change.Action,
	}

	if change.Node != nil {
		result.NodeID = change.Node.ID
	}

	// Lock for state access
	e.stateMu.Lock()

	// Ensure environment state maps are initialized
	if envState.Components == nil {
		// Nothing to destroy if no components
		e.stateMu.Unlock()
		result.Success = true
		return result
	}

	// Get component state
	compState := envState.Components[change.Node.Component]
	if compState == nil {
		// Nothing to destroy
		e.stateMu.Unlock()
		result.Success = true
		return result
	}

	// Get resource state to retrieve IaC state (try type-qualified key first, fall back to legacy name-only key)
	resourceState := compState.Resources[resourceKey(change.Node)]
	if resourceState == nil {
		// Backward compatibility: try legacy name-only key
		resourceState = compState.Resources[change.Node.Name]
	}

	e.stateMu.Unlock()

	// Get IaC plugin
	plugin, err := e.iacRegistry.Get("native")
	if err != nil {
		result.Error = fmt.Errorf("failed to get IaC plugin: %w", err)
		result.Success = false
		return result
	}

	// Build run options with state reader if we have stored state
	runOpts := iac.RunOptions{
		ModulePath: string(change.Node.Type),
		Inputs:     change.Node.Inputs,
	}

	// Pass the stored IaC state so the plugin knows what to destroy
	if resourceState != nil && len(resourceState.IaCState) > 0 {
		runOpts.StateReader = bytes.NewReader(resourceState.IaCState)
	}

	// Execute destroy
	if err := plugin.Destroy(ctx, runOpts); err != nil {
		result.Error = fmt.Errorf("destroy failed: %w", err)
		result.Success = false
		return result
	}

	result.Success = true

	// Lock for state cleanup
	e.stateMu.Lock()

	// Remove resource from state (try type-qualified key first, fall back to legacy)
	rKey := resourceKey(change.Node)
	if _, ok := compState.Resources[rKey]; ok {
		delete(compState.Resources, rKey)
	} else {
		delete(compState.Resources, change.Node.Name)
	}

	// If component has no more resources, remove it
	if len(compState.Resources) == 0 {
		delete(envState.Components, change.Node.Component)
	}

	e.saveStateLocked(envState)
	e.stateMu.Unlock()

	return result
}

// ExecuteParallel executes independent operations in parallel.
// Uses a reactive approach: nodes start as soon as their specific dependencies
// complete, rather than waiting for an entire batch to finish. This prevents
// fast-completing nodes (like routes) from being blocked by slow nodes (like
// docker builds) when they share the same dependency level.
func (e *Executor) ExecuteParallel(ctx context.Context, plan *planner.Plan, g *graph.Graph) (*ExecutionResult, error) {
	startTime := time.Now()

	// Store graph reference for service port lookups
	e.graph = g
	e.datacenterName = plan.Datacenter

	result := &ExecutionResult{
		Success:     true,
		NodeResults: make(map[string]*NodeResult),
	}

	if plan.IsEmpty() {
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Get or create environment state
	envState, err := e.stateManager.GetEnvironment(ctx, plan.Datacenter, plan.Environment)
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
	// Ensure Components map is initialized (might be nil if loaded from state)
	if envState.Components == nil {
		envState.Components = make(map[string]*types.ComponentState)
	}

	// Mark as provisioning and flush so that inspect can see progress immediately
	envState.Status = types.EnvironmentStatusProvisioning
	envState.UpdatedAt = time.Now()
	_ = e.stateManager.SaveEnvironment(ctx, plan.Datacenter, envState)

	// Create a derived context so StopOnError can cancel in-flight operations
	// (e.g., Docker builds, image pulls) for fast termination and cleanup.
	execCtx, execCancel := context.WithCancel(ctx)
	defer execCancel()

	// Concurrency control
	var mu sync.Mutex
	sem := make(chan struct{}, e.options.Parallelism)
	var wg sync.WaitGroup

	// Track node states
	pending := make(map[string]*planner.ResourceChange)
	for _, change := range plan.Changes {
		if change.Node != nil {
			pending[change.Node.ID] = change
		}
	}

	completed := make(map[string]bool)
	failed := make(map[string]bool)
	inFlight := make(map[string]bool)

	// Channel to signal that a node has finished (triggers re-evaluation of ready nodes)
	nodeFinished := make(chan struct{}, len(pending))

	// Debug: show all nodes and their dependencies
	if os.Getenv("CLDCTL_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[debug] All nodes and dependencies:\n")
		for id, change := range pending {
			fmt.Fprintf(os.Stderr, "[debug]   %s -> %v\n", id, change.Node.DependsOn)
		}
	}

	// Track if context was cancelled
	var cancelled bool

	// findAndLaunchReady finds nodes whose dependencies are all met and launches them.
	// Must be called with mu held. Uses two-step declaration for recursive self-reference.
	var findAndLaunchReady func()
	findAndLaunchReady = func() {
		// Don't launch new nodes if context is cancelled
		if execCtx.Err() != nil {
			cancelled = true
			return
		}

		// First pass: cascade failures to nodes whose dependencies failed
		for {
			cascaded := false
			for id, change := range pending {
				if inFlight[id] {
					continue
				}
				for _, depID := range change.Node.DependsOn {
					if failed[depID] {
						depErr := fmt.Errorf("dependency %s failed", depID)
						result.NodeResults[id] = &NodeResult{
							NodeID:  id,
							Action:  change.Action,
							Success: false,
							Error:   depErr,
						}
						result.Failed++
						result.Success = false
						delete(pending, id)
						failed[id] = true
						cascaded = true

						// Resolve expressions before saving state so that `cldctl inspect`
						// shows resolved values (e.g., database URLs) even for cascaded failures.
						// Dependencies that succeeded will have their outputs available in the graph.
						e.resolveComponentExpressions(change.Node, envState)

						// Persist the cascaded failure to state so `cldctl inspect` shows it
						e.stateMu.Lock()
						if envState.Components == nil {
							envState.Components = make(map[string]*types.ComponentState)
						}
						compState := envState.Components[change.Node.Component]
						if compState == nil {
							compState = e.newComponentState(change.Node.Component)
							envState.Components[change.Node.Component] = compState
						}
						if compState.Resources == nil {
							compState.Resources = make(map[string]*types.ResourceState)
						}
						compState.Resources[resourceKey(change.Node)] = &types.ResourceState{
							Component:    change.Node.Component,
							Name:         change.Node.Name,
							Type:         string(change.Node.Type),
							Status:       types.ResourceStatusFailed,
							StatusReason: depErr.Error(),
							Inputs:       change.Node.Inputs,
							UpdatedAt:    time.Now(),
						}
						e.saveStateLocked(envState)
						e.stateMu.Unlock()

						// Notify progress callback about the failure
						if e.options.OnProgress != nil {
							e.options.OnProgress(ProgressEvent{
								NodeID:   change.Node.ID,
								NodeName: change.Node.Name,
								NodeType: string(change.Node.Type),
								Status:   "failed",
								Message:  depErr.Error(),
								Error:    depErr,
							})
						}
						break
					}
				}
			}
			if !cascaded {
				break
			}
		}

		// If StopOnError is set and any node has failed, don't launch new work.
		// Mark all remaining pending (non-in-flight) nodes as failed so the
		// executor terminates quickly once in-flight goroutines finish.
		if e.options.StopOnError && len(failed) > 0 {
			for id, change := range pending {
				if inFlight[id] {
					continue
				}
				stopErr := fmt.Errorf("deployment stopped: a previous resource failed")
				result.NodeResults[id] = &NodeResult{
					NodeID:  id,
					Action:  change.Action,
					Success: false,
					Error:   stopErr,
				}
				result.Failed++
				result.Success = false
				delete(pending, id)
				failed[id] = true

				// Resolve expressions and persist so inspect shows useful data
				e.resolveComponentExpressions(change.Node, envState)

				e.stateMu.Lock()
				if envState.Components == nil {
					envState.Components = make(map[string]*types.ComponentState)
				}
				compState := envState.Components[change.Node.Component]
				if compState == nil {
					compState = e.newComponentState(change.Node.Component)
					envState.Components[change.Node.Component] = compState
				}
				if compState.Resources == nil {
					compState.Resources = make(map[string]*types.ResourceState)
				}
				compState.Resources[resourceKey(change.Node)] = &types.ResourceState{
					Component:    change.Node.Component,
					Name:         change.Node.Name,
					Type:         string(change.Node.Type),
					Status:       types.ResourceStatusFailed,
					StatusReason: stopErr.Error(),
					Inputs:       change.Node.Inputs,
					UpdatedAt:    time.Now(),
				}
				e.saveStateLocked(envState)
				e.stateMu.Unlock()

				if e.options.OnProgress != nil {
					e.options.OnProgress(ProgressEvent{
						NodeID:   change.Node.ID,
						NodeName: change.Node.Name,
						NodeType: string(change.Node.Type),
						Status:   "failed",
						Message:  stopErr.Error(),
						Error:    stopErr,
					})
				}
			}
			// Cancel the execution context so in-flight operations (Docker
			// builds, image pulls, etc.) are interrupted immediately.
			execCancel()
			return
		}

		// Second pass: find and launch ready nodes
		for id, change := range pending {
			if inFlight[id] {
				continue
			}

			isReady := true
			for _, depID := range change.Node.DependsOn {
				if !completed[depID] {
					isReady = false
					break
				}
			}

			if isReady {
				inFlight[id] = true

				if os.Getenv("CLDCTL_DEBUG") != "" {
					fmt.Fprintf(os.Stderr, "[debug] Launching %s (deps satisfied)\n", id)
				}

				wg.Add(1)

				go func(c *planner.ResourceChange) {
					// Acquire semaphore (limits concurrency)
					select {
					case sem <- struct{}{}:
						// Got semaphore
					case <-execCtx.Done():
						// Context cancelled (user interrupt or StopOnError)
						wg.Done()
						mu.Lock()
						delete(inFlight, c.Node.ID)
						failed[c.Node.ID] = true
						result.NodeResults[c.Node.ID] = &NodeResult{
							NodeID:  c.Node.ID,
							Action:  c.Action,
							Success: false,
							Error:   fmt.Errorf("cancelled"),
						}
						result.Failed++
						result.Success = false
						mu.Unlock()
						nodeFinished <- struct{}{}
						return
					}
					defer func() { <-sem }()
					defer wg.Done()

					if os.Getenv("CLDCTL_DEBUG") != "" {
						fmt.Fprintf(os.Stderr, "[debug] Goroutine started for %s, calling executeChange\n", c.Node.ID)
					}

					nodeResult := e.executeChange(execCtx, c, envState)

					// If this node failed because StopOnError cancelled the
					// execution context (not a user Ctrl+C), use a clean error.
					if !nodeResult.Success && execCtx.Err() != nil && ctx.Err() == nil {
						nodeResult.Error = fmt.Errorf("cancelled")
					}

					if os.Getenv("CLDCTL_DEBUG") != "" {
						fmt.Fprintf(os.Stderr, "[debug] executeChange completed for %s, success=%v\n", c.Node.ID, nodeResult.Success)
					}

					mu.Lock()
					result.NodeResults[c.Node.ID] = nodeResult
					delete(pending, c.Node.ID)
					delete(inFlight, c.Node.ID)

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
						if nodeResult.Outputs != nil {
							c.Node.Outputs = nodeResult.Outputs
						}
						c.Node.State = graph.NodeStateCompleted

						// For observability nodes, enrich outputs with merged attributes
						if c.Node.Type == graph.NodeTypeObservability {
							e.enrichObservabilityOutputs(c.Node)
						}
					} else {
						failed[c.Node.ID] = true
						result.Success = false
						result.Errors = append(result.Errors, nodeResult.Error)
						c.Node.State = graph.NodeStateFailed
					}

					// Trigger re-evaluation: new nodes may now be ready
					findAndLaunchReady()
					mu.Unlock()

					// Signal completion for the drain loop
					nodeFinished <- struct{}{}
				}(change)
			}
		}
	}

	// Initial launch
	mu.Lock()
	findAndLaunchReady()
	mu.Unlock()

	// Wait for all goroutines to complete
	wg.Wait()

	// Check if execution was stopped (user interrupt or StopOnError)
	mu.Lock()
	if cancelled || execCtx.Err() != nil {
		// Mark remaining pending nodes
		for id, change := range pending {
			if !inFlight[id] && !completed[id] && !failed[id] {
				result.NodeResults[id] = &NodeResult{
					NodeID:  id,
					Action:  change.Action,
					Success: false,
					Error:   fmt.Errorf("cancelled"),
				}
				result.Failed++
			}
		}
		result.Success = false
		if ctx.Err() != nil {
			result.Errors = append(result.Errors, ctx.Err())
		}
		mu.Unlock()

		// Still save state and return
		computeComponentStatuses(envState)
		e.resolveAndStoreComponentOutputs(envState)
		envState.Status = types.EnvironmentStatusFailed
		envState.UpdatedAt = time.Now()
		_ = e.stateManager.SaveEnvironment(ctx, plan.Datacenter, envState)
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Check for stuck nodes (dependency cycle or unresolvable deps)
	if len(pending) > 0 {
		if os.Getenv("CLDCTL_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[debug] Deadlock detected! Pending nodes:\n")
			for id, change := range pending {
				fmt.Fprintf(os.Stderr, "[debug]   %s depends on: %v\n", id, change.Node.DependsOn)
			}
			fmt.Fprintf(os.Stderr, "[debug] Completed nodes: %v\n", completed)
		}
		// Mark remaining nodes as failed
		for id, change := range pending {
			if !inFlight[id] {
				depErr := fmt.Errorf("node stuck: dependencies could not be resolved")
				result.NodeResults[id] = &NodeResult{
					NodeID:  id,
					Action:  change.Action,
					Success: false,
					Error:   depErr,
				}
				result.Failed++
				result.Success = false
			}
		}
	}
	mu.Unlock()

	// Compute component statuses from child resources
	computeComponentStatuses(envState)

	// Resolve component-level output expressions (e.g., pass-through components
	// that expose variables as outputs for dependents).
	e.resolveAndStoreComponentOutputs(envState)

	// Update environment status
	if result.Success {
		envState.Status = types.EnvironmentStatusReady
	} else {
		envState.Status = types.EnvironmentStatusFailed
	}
	envState.UpdatedAt = time.Now()

	// Save state
	if err := e.stateManager.SaveEnvironment(ctx, plan.Datacenter, envState); err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("failed to save state: %w", err))
	}

	result.Duration = time.Since(startTime)
	return result, nil
}
