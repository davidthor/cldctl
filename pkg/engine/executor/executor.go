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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/architect-io/arcctl/pkg/engine/planner"
	"github.com/architect-io/arcctl/pkg/graph"
	"github.com/architect-io/arcctl/pkg/iac"
	"github.com/architect-io/arcctl/pkg/schema/datacenter"
	v1 "github.com/architect-io/arcctl/pkg/schema/datacenter/v1"
	"github.com/architect-io/arcctl/pkg/state"
	"github.com/architect-io/arcctl/pkg/state/types"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
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

// ProgressEvent represents a progress update during execution.
type ProgressEvent struct {
	NodeID   string
	NodeName string
	NodeType string
	Status   string // "pending", "running", "completed", "failed", "skipped"
	Message  string
	Error    error
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
	graph        *graph.Graph // Store reference to graph for service port lookups
	stateMu      sync.Mutex   // Protects concurrent access to environment state
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

// resourceKey returns the type-qualified key for storing a resource in state.
// Format: "type.name" (e.g., "deployment.api", "database.main").
// This prevents collisions when different resource types share the same name.
func resourceKey(node *graph.Node) string {
	return string(node.Type) + "." + node.Name
}

// Execute runs an execution plan.
func (e *Executor) Execute(ctx context.Context, plan *planner.Plan, g *graph.Graph) (*ExecutionResult, error) {
	startTime := time.Now()

	// Store graph reference for service port lookups
	e.graph = g

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
	// Ensure Components map is initialized (might be nil if loaded from state)
	if envState.Components == nil {
		envState.Components = make(map[string]*types.ComponentState)
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

			// For observability nodes, enrich outputs with merged attributes
			if change.Node.Type == graph.NodeTypeObservability {
				e.enrichObservabilityOutputs(change.Node)
			}
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
		result = e.executeApply(ctx, change, envState)
	case planner.ActionDelete:
		result = e.executeDestroy(ctx, change, envState)
	case planner.ActionNoop:
		result.Success = true
	}

	result.Duration = time.Since(startTime)

	// Notify progress: completed or failed
	if e.options.OnProgress != nil && change.Node != nil {
		status := "completed"
		msg := ""
		if !result.Success {
			status = "failed"
			if result.Error != nil {
				msg = result.Error.Error()
			}
		}
		e.options.OnProgress(ProgressEvent{
			NodeID:   change.Node.ID,
			NodeName: change.Node.Name,
			NodeType: string(change.Node.Type),
			Status:   status,
			Message:  msg,
			Error:    result.Error,
		})
	}

	return result
}

func (e *Executor) executeApply(ctx context.Context, change *planner.ResourceChange, envState *types.EnvironmentState) *NodeResult {
	result := &NodeResult{
		NodeID: change.Node.ID,
		Action: change.Action,
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
		compState = &types.ComponentState{
			Name:      change.Node.Component,
			Resources: make(map[string]*types.ResourceState),
			Status:    types.ResourceStatusProvisioning,
			UpdatedAt: time.Now(),
		}
		// Record inter-component dependencies from the graph
		if e.graph != nil && e.graph.ComponentDependencies != nil {
			compState.Dependencies = e.graph.ComponentDependencies[change.Node.Component]
		}
		envState.Components[change.Node.Component] = compState
	}

	// Ensure resources map is initialized
	if compState.Resources == nil {
		compState.Resources = make(map[string]*types.ResourceState)
	}

	e.stateMu.Unlock()

	// Resolve ${{ }} component expressions in node inputs (e.g., ${{ builds.api.image }})
	// by looking at completed dependency nodes' outputs.
	e.resolveComponentExpressions(change.Node)

	// Find the matching hook from datacenter
	modulePath, moduleInputs, pluginName, err := e.findMatchingHook(change.Node, envState.Name)
	if err != nil {
		result.Error = fmt.Errorf("failed to find matching hook: %w", err)
		result.Success = false
		return result
	}

	// Get IaC plugin
	if pluginName == "" {
		pluginName = "native"
	}
	plugin, err := e.iacRegistry.Get(pluginName)
	if err != nil {
		result.Error = fmt.Errorf("failed to get IaC plugin %q: %w", pluginName, err)
		result.Success = false
		return result
	}

	// Build run options
	runOpts := iac.RunOptions{
		ModuleSource: modulePath,
		Inputs:       moduleInputs,
		Environment:  map[string]string{},
	}

	// Execute
	applyResult, err := plugin.Apply(ctx, runOpts)
	if err != nil {
		result.Error = fmt.Errorf("apply failed: %w", err)
		result.Success = false

		// Update resource state to failed (lock for state update)
		e.stateMu.Lock()
		compState.Resources[resourceKey(change.Node)] = &types.ResourceState{
			Component: change.Node.Component,
			Name:      change.Node.Name,
			Type:      string(change.Node.Type),
			Status:    types.ResourceStatusFailed,
			Inputs:    change.Node.Inputs,
			UpdatedAt: time.Now(),
		}
		e.stateMu.Unlock()

		return result
	}

	// Extract outputs
	outputs := make(map[string]interface{})
	for name, out := range applyResult.Outputs {
		outputs[name] = out.Value
	}
	result.Outputs = outputs
	result.Success = true

	// Update resource state including IaC state for cleanup (lock for state update)
	e.stateMu.Lock()
	compState.Resources[resourceKey(change.Node)] = &types.ResourceState{
		Component: change.Node.Component,
		Name:      change.Node.Name,
		Type:      string(change.Node.Type),
		Status:    types.ResourceStatusReady,
		Inputs:    change.Node.Inputs,
		Outputs:   outputs,
		IaCState:  applyResult.State, // Store IaC state for destroy
		UpdatedAt: time.Now(),
	}
	e.stateMu.Unlock()

	return result
}

// findMatchingHook finds the matching datacenter hook for a node and returns the module path, inputs, and plugin name.
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
	if os.Getenv("ARCCTL_DEBUG") != "" && e.options.Output != nil {
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
	default:
		return nil
	}
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

	// Set node context with inputs
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
	// Check for "==" comparisons
	if contains(when, "==") {
		parts := splitOnce(when, "==")
		if len(parts) == 2 {
			left := trimQuotes(e.resolveWhenExpr(parts[0], inputs))
			right := trimQuotes(parts[1])
			return left == right
		}
	}

	// Check for "!= null" patterns
	if contains(when, "!= null") || contains(when, "!=null") {
		inputName := extractInputName(when)
		if inputName != "" {
			val := inputs[inputName]
			return val != nil && val != ""
		}
	}

	// Default to true if we can't parse the condition
	return true
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
	networkName := getStringVar(dcVars, "network_name", "arcctl-local")
	host := getStringVar(dcVars, "host", "localhost")

	// Standard name format: ${environment.name}-${node.component}-${node.name}
	standardName := fmt.Sprintf("%s-%s-%s", envName, node.Component, node.Name)

	// Try to use the datacenter's input definitions first (if they were evaluated successfully)
	moduleInputDefs := module.Inputs()
	for inputName, exprStr := range moduleInputDefs {
		value := e.evaluateInputExpression(exprStr, node, envName, dcVars)
		if value != nil && value != "" {
			inputs[inputName] = value
		}
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

	case "build":
		setIfMissing(inputs, "context", node.Inputs["context"])
		setIfMissing(inputs, "dockerfile", node.Inputs["dockerfile"])
		setIfMissing(inputs, "target", node.Inputs["target"])
		setIfMissing(inputs, "args", node.Inputs["args"])
		setIfMissing(inputs, "tag", fmt.Sprintf("%s-%s-%s:local", envName, node.Component, node.Name))

	case "process":
		setIfMissing(inputs, "name", standardName)
		// Map srcPath from function to context for process module
		setIfMissing(inputs, "context", node.Inputs["srcPath"])
		// Try dev command first, then start, then generic command
		if cmd := node.Inputs["dev"]; cmd != nil {
			setIfMissing(inputs, "command", cmd)
		} else if cmd := node.Inputs["start"]; cmd != nil {
			setIfMissing(inputs, "command", cmd)
		} else {
			setIfMissing(inputs, "command", node.Inputs["command"])
		}
		setIfMissing(inputs, "framework", node.Inputs["framework"])

		// Inject PORT into environment
		// Priority: 1) node's own port property (for functions), 2) associated service's port
		env := getEnvironmentMap(node.Inputs["environment"])
		port := e.resolvePortForWorkload(node)
		if port > 0 {
			env["PORT"] = fmt.Sprintf("%d", port)
		}
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

		// Inject PORT into environment
		// Priority: 1) node's own port property (for functions), 2) associated service's port
		env := getEnvironmentMap(node.Inputs["environment"])
		port := e.resolvePortForWorkload(node)
		if port > 0 {
			env["PORT"] = fmt.Sprintf("%d", port)
		}
		// Auto-inject OTEL_* env vars if observability.inject is true
		e.injectOTelEnvironmentIfEnabled(env, node)
		inputs["environment"] = env
		inputs["port"] = port

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

// extractVersionFromType extracts the version part from a "type:version" string.
// For example, "postgres:^16" returns "^16", and "postgres" returns "".
func extractVersionFromType(typeInput interface{}) string {
	typeStr, ok := typeInput.(string)
	if !ok || typeStr == "" {
		return ""
	}
	parts := strings.SplitN(typeStr, ":", 2)
	if len(parts) > 1 {
		return parts[1]
	}
	return ""
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
func (e *Executor) resolveComponentExpressions(node *graph.Node) {
	if e.graph == nil {
		return
	}

	exprPattern := regexp.MustCompile(`\$\{\{\s*([^}]+)\s*\}\}`)

	resolveStr := func(strVal string) string {
		if !strings.Contains(strVal, "${{") {
			return strVal
		}
		return exprPattern.ReplaceAllStringFunc(strVal, func(match string) string {
			inner := match[3 : len(match)-2]
			inner = strings.TrimSpace(inner)
			parts := strings.Split(inner, ".")

			if len(parts) < 2 {
				return match
			}

			resourceType := parts[0]
			switch resourceType {
			case "builds":
				if len(parts) < 3 {
					return match
				}
				buildNodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeDockerBuild, parts[1])
				buildNode, ok := e.graph.Nodes[buildNodeID]
				if !ok || buildNode.Outputs == nil {
					return match
				}
				if val, ok := buildNode.Outputs[parts[2]]; ok {
					return fmt.Sprintf("%v", val)
				}

			case "databases":
				if len(parts) < 3 {
					return match
				}
				nodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeDatabase, parts[1])
				depNode, ok := e.graph.Nodes[nodeID]
				if !ok || depNode.Outputs == nil {
					return match
				}
				if val, ok := depNode.Outputs[parts[2]]; ok {
					return fmt.Sprintf("%v", val)
				}

			case "services":
				if len(parts) < 3 {
					return match
				}
				nodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeService, parts[1])
				depNode, ok := e.graph.Nodes[nodeID]
				if !ok || depNode.Outputs == nil {
					return match
				}
				if val, ok := depNode.Outputs[parts[2]]; ok {
					return fmt.Sprintf("%v", val)
				}

			case "buckets":
				if len(parts) < 3 {
					return match
				}
				nodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeBucket, parts[1])
				depNode, ok := e.graph.Nodes[nodeID]
				if !ok || depNode.Outputs == nil {
					return match
				}
				if val, ok := depNode.Outputs[parts[2]]; ok {
					return fmt.Sprintf("%v", val)
				}

			case "routes":
				if len(parts) < 3 {
					return match
				}
				nodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeRoute, parts[1])
				depNode, ok := e.graph.Nodes[nodeID]
				if !ok || depNode.Outputs == nil {
					return match
				}
				if val, ok := depNode.Outputs[parts[2]]; ok {
					return fmt.Sprintf("%v", val)
				}

			case "observability":
				// observability is a singleton per component
				obsNodeID := fmt.Sprintf("%s/%s/%s", node.Component, graph.NodeTypeObservability, "observability")
				obsNode, ok := e.graph.Nodes[obsNodeID]
				if !ok || obsNode.Outputs == nil {
					return "" // Resolve to empty string if no observability hook
				}
				prop := parts[1]
				if val, ok := obsNode.Outputs[prop]; ok {
					return fmt.Sprintf("%v", val)
				}
				return "" // Unknown property resolves to empty

			case "variables":
				if len(parts) < 2 {
					return match
				}
				if val, ok := node.Inputs["variables_"+parts[1]]; ok {
					return fmt.Sprintf("%v", val)
				}
			}

			return match
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
// Priority: 1) node's own port property (for functions), 2) associated service's port, 3) default 0
func (e *Executor) resolvePortForWorkload(node *graph.Node) int {
	// First, check if the node has its own port property (functions can specify this)
	if port, ok := node.Inputs["port"].(int); ok && port > 0 {
		return port
	}
	if port, ok := node.Inputs["port"].(float64); ok && port > 0 {
		return int(port)
	}

	// Fall back to looking up the associated service's port
	targetType := "deployment"
	if node.Type == graph.NodeTypeFunction {
		targetType = "function"
	}
	return e.lookupServicePortForTarget(node.Component, node.Name, targetType)
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

		// Found a matching service, get its port
		if port, ok := node.Inputs["port"].(int); ok {
			return port
		}
		if port, ok := node.Inputs["port"].(float64); ok {
			return int(port)
		}
	}

	return 0
}

// evaluateInputExpression evaluates a simple HCL-like expression string.
// This is a simplified evaluator that handles common patterns.
func (e *Executor) evaluateInputExpression(expr string, node *graph.Node, envName string, dcVars map[string]interface{}) interface{} {
	expr = trimSpace(expr)

	// Handle string interpolation ${...}
	if strings.Contains(expr, "${") {
		result := expr
		// Replace ${environment.name}
		result = strings.ReplaceAll(result, "${environment.name}", envName)
		// Replace ${node.name}
		result = strings.ReplaceAll(result, "${node.name}", node.Name)
		// Replace ${node.component}
		result = strings.ReplaceAll(result, "${node.component}", node.Component)
		// Replace ${node.type}
		result = strings.ReplaceAll(result, "${node.type}", string(node.Type))
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
		return result
	}

	// Handle direct references
	if hasPrefix(expr, "node.name") {
		return node.Name
	}
	if hasPrefix(expr, "node.component") {
		return node.Component
	}
	if hasPrefix(expr, "node.type") {
		return string(node.Type)
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
			return "arcctl-local"
		case "host":
			return "localhost"
		case "base_port":
			return 8000
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
	rKey := resourceKey(change.Node)
	resourceState := compState.Resources[rKey]
	if resourceState == nil {
		// Backward compatibility: try legacy name-only key
		resourceState = compState.Resources[change.Node.Name]
		if resourceState != nil {
			rKey = change.Node.Name
		}
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
	rKey = resourceKey(change.Node)
	if _, ok := compState.Resources[rKey]; ok {
		delete(compState.Resources, rKey)
	} else {
		delete(compState.Resources, change.Node.Name)
	}

	// If component has no more resources, remove it
	if len(compState.Resources) == 0 {
		delete(envState.Components, change.Node.Component)
	}

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
	// Ensure Components map is initialized (might be nil if loaded from state)
	if envState.Components == nil {
		envState.Components = make(map[string]*types.ComponentState)
	}

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
	if os.Getenv("ARCCTL_DEBUG") != "" {
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
		if ctx.Err() != nil {
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

				if os.Getenv("ARCCTL_DEBUG") != "" {
					fmt.Fprintf(os.Stderr, "[debug] Launching %s (deps satisfied)\n", id)
				}

				wg.Add(1)

				go func(c *planner.ResourceChange) {
					// Acquire semaphore (limits concurrency)
					select {
					case sem <- struct{}{}:
						// Got semaphore
					case <-ctx.Done():
						// Context cancelled while waiting for semaphore
						wg.Done()
						mu.Lock()
						delete(inFlight, c.Node.ID)
						failed[c.Node.ID] = true
						result.NodeResults[c.Node.ID] = &NodeResult{
							NodeID:  c.Node.ID,
							Action:  c.Action,
							Success: false,
							Error:   ctx.Err(),
						}
						result.Failed++
						result.Success = false
						mu.Unlock()
						nodeFinished <- struct{}{}
						return
					}
					defer func() { <-sem }()
					defer wg.Done()

					if os.Getenv("ARCCTL_DEBUG") != "" {
						fmt.Fprintf(os.Stderr, "[debug] Goroutine started for %s, calling executeChange\n", c.Node.ID)
					}

					nodeResult := e.executeChange(ctx, c, envState)

					if os.Getenv("ARCCTL_DEBUG") != "" {
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
						c.Node.Outputs = nodeResult.Outputs
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

	// Check if context was cancelled
	mu.Lock()
	if cancelled || ctx.Err() != nil {
		// Mark remaining pending nodes as cancelled
		for id, change := range pending {
			if !inFlight[id] && !completed[id] && !failed[id] {
				result.NodeResults[id] = &NodeResult{
					NodeID:  id,
					Action:  change.Action,
					Success: false,
					Error:   ctx.Err(),
				}
				result.Failed++
			}
		}
		result.Success = false
		result.Errors = append(result.Errors, ctx.Err())
		mu.Unlock()

		// Still save state and return
		envState.Status = types.EnvironmentStatusFailed
		envState.UpdatedAt = time.Now()
		_ = e.stateManager.SaveEnvironment(ctx, envState)
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Check for stuck nodes (dependency cycle or unresolvable deps)
	if len(pending) > 0 {
		if os.Getenv("ARCCTL_DEBUG") != "" {
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
