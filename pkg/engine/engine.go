// Package engine provides the core orchestration for arcctl deployments.
package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/architect-io/arcctl/pkg/engine/executor"
	"github.com/architect-io/arcctl/pkg/engine/planner"
	"github.com/architect-io/arcctl/pkg/graph"
	"github.com/architect-io/arcctl/pkg/iac"
	"github.com/architect-io/arcctl/pkg/oci"
	"github.com/architect-io/arcctl/pkg/registry"
	"github.com/architect-io/arcctl/pkg/schema/component"
	"github.com/architect-io/arcctl/pkg/schema/datacenter"
	"github.com/architect-io/arcctl/pkg/schema/environment"
	"github.com/architect-io/arcctl/pkg/state"
	"github.com/architect-io/arcctl/pkg/state/types"
)

// OCIClient defines the interface for OCI registry operations needed by the engine.
type OCIClient interface {
	Pull(ctx context.Context, reference string, destDir string) error
	PullConfig(ctx context.Context, reference string) ([]byte, error)
	Exists(ctx context.Context, reference string) (bool, error)
}

// Engine orchestrates component deployments.
type Engine struct {
	stateManager state.Manager
	iacRegistry  *iac.Registry
	compLoader   component.Loader
	envLoader    environment.Loader
	dcLoader     datacenter.Loader
	ociClient    OCIClient
}

// NewEngine creates a new deployment engine.
func NewEngine(stateManager state.Manager, iacRegistry *iac.Registry) *Engine {
	return &Engine{
		stateManager: stateManager,
		iacRegistry:  iacRegistry,
		compLoader:   component.NewLoader(),
		envLoader:    environment.NewLoader(),
		dcLoader:     datacenter.NewLoader(),
		ociClient:    oci.NewClient(),
	}
}

// DeployDatacenterOptions configures a datacenter deployment operation.
type DeployDatacenterOptions struct {
	// Datacenter name
	Datacenter string

	// Output writer for progress
	Output io.Writer

	// OnProgress is called when module status changes
	OnProgress executor.ProgressCallback

	// Parallelism for parallel execution
	Parallelism int

	// DryRun only plans without executing
	DryRun bool
}

// DeployDatacenterResult contains the results of a datacenter deployment.
type DeployDatacenterResult struct {
	Success  bool
	Duration time.Duration
	// Outputs from root-level modules keyed by module name
	ModuleOutputs map[string]map[string]interface{}
}

// DeployEnvironmentOptions configures an environment module deployment.
type DeployEnvironmentOptions struct {
	// Datacenter name
	Datacenter string

	// Environment name
	Environment string

	// Output writer for progress
	Output io.Writer

	// OnProgress is called when module status changes
	OnProgress executor.ProgressCallback

	// Parallelism for parallel execution
	Parallelism int

	// DryRun only plans without executing
	DryRun bool
}

// DeployEnvironmentResult contains the results of an environment module deployment.
type DeployEnvironmentResult struct {
	Success  bool
	Duration time.Duration
	// Outputs from environment-scoped modules keyed by module name
	ModuleOutputs map[string]map[string]interface{}
}

// DeployOptions configures a deployment operation.
type DeployOptions struct {
	// Environment name
	Environment string

	// Datacenter name
	Datacenter string

	// Components to deploy (by name to config path)
	Components map[string]string

	// Variables to pass to components
	Variables map[string]map[string]interface{}

	// Output writer for progress
	Output io.Writer

	// DryRun only plans without executing
	DryRun bool

	// AutoApprove skips confirmation
	AutoApprove bool

	// Parallelism for parallel execution
	Parallelism int

	// OnProgress is called when resource status changes
	OnProgress executor.ProgressCallback

	// ForceUpdate converts Noop actions to Update, used when datacenter config
	// changes and all resources need re-evaluation against new hooks.
	ForceUpdate bool
}

// DeployResult contains the results of a deployment.
type DeployResult struct {
	Success   bool
	Plan      *planner.Plan
	Execution *executor.ExecutionResult
	Duration  time.Duration
}

// Deploy deploys components to an environment.
func (e *Engine) Deploy(ctx context.Context, opts DeployOptions) (*DeployResult, error) {
	startTime := time.Now()

	result := &DeployResult{}

	// Load datacenter configuration
	dcState, err := e.stateManager.GetDatacenter(ctx, opts.Datacenter)
	if err != nil {
		return nil, fmt.Errorf("datacenter %q not found: %w", opts.Datacenter, err)
	}

	// Load the datacenter schema from the stored path
	// The Version field contains the source path or OCI reference
	dcPath := dcState.Version
	if dcPath == "" {
		return nil, fmt.Errorf("datacenter %q has no source path configured", opts.Datacenter)
	}

	// Load the datacenter configuration
	dc, err := e.loadDatacenterConfig(dcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load datacenter configuration: %w", err)
	}

	// Build dependency graph
	builder := graph.NewBuilder(opts.Environment, opts.Datacenter)

	for compName, compPath := range opts.Components {
		// Load component
		comp, err := e.compLoader.Load(compPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load component %s: %w", compName, err)
		}

		// Add to graph - component name comes from the deployment mapping
		if err := builder.AddComponent(compName, comp); err != nil {
			return nil, fmt.Errorf("failed to add component %s to graph: %w", compName, err)
		}
	}

	g := builder.Build()

	// Get current state
	currentState, _ := e.stateManager.GetEnvironment(ctx, opts.Datacenter, opts.Environment)

	// Create plan
	planOpts := planner.PlanOptions{
		ForceUpdate: opts.ForceUpdate,
	}
	p := planner.NewPlannerWithOptions(planOpts)
	plan, err := p.Plan(g, currentState)
	if err != nil {
		return nil, fmt.Errorf("failed to create plan: %w", err)
	}

	result.Plan = plan

	// Print plan summary
	if opts.Output != nil {
		e.printPlanSummary(opts.Output, plan)
	}

	// If dry run or no changes, return here
	if opts.DryRun || plan.IsEmpty() {
		result.Success = plan.IsEmpty() || opts.DryRun
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Build datacenter variables map
	dcVars := make(map[string]interface{})
	for k, v := range dcState.Variables {
		dcVars[k] = v
	}

	// Execute plan
	execOpts := executor.Options{
		Parallelism:         opts.Parallelism,
		Output:              opts.Output,
		DryRun:              false,
		StopOnError:         true,
		OnProgress:          opts.OnProgress,
		Datacenter:          dc,
		DatacenterVariables: dcVars,
		ComponentSources:    opts.Components,
		ComponentVariables:  opts.Variables,
	}

	exec := executor.NewExecutor(e.stateManager, e.iacRegistry, execOpts)

	var execResult *executor.ExecutionResult
	if opts.Parallelism > 1 {
		execResult, err = exec.ExecuteParallel(ctx, plan, g)
	} else {
		execResult, err = exec.Execute(ctx, plan, g)
	}

	if err != nil {
		return nil, fmt.Errorf("execution failed: %w", err)
	}

	result.Execution = execResult
	result.Success = execResult.Success
	result.Duration = time.Since(startTime)

	return result, nil
}

// loadDatacenterConfig loads a datacenter configuration from a path or OCI reference.
// Resolution order: local filesystem path → unified artifact registry → remote OCI pull.
func (e *Engine) loadDatacenterConfig(ref string) (datacenter.Datacenter, error) {
	// Check if this is a local filesystem path
	isLocalPath := strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "../")
	if !isLocalPath {
		// Also treat paths without ":" as local (but not bare names like "mydc:latest")
		if !strings.Contains(ref, ":") {
			if _, err := os.Stat(ref); err == nil {
				isLocalPath = true
			}
		}
	}

	if isLocalPath {
		// Resolve to absolute path
		absPath := ref
		if !filepath.IsAbs(ref) {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, fmt.Errorf("failed to get working directory: %w", err)
			}
			absPath = filepath.Join(cwd, ref)
		}

		info, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to access datacenter path: %w", err)
		}

		dcFile := absPath
		if info.IsDir() {
			dcFile = filepath.Join(absPath, "datacenter.dc")
			if _, err := os.Stat(dcFile); os.IsNotExist(err) {
				dcFile = filepath.Join(absPath, "datacenter.hcl")
			}
		}

		return e.dcLoader.Load(dcFile)
	}

	// Not a local path — check the unified artifact registry first (like docker run).
	reg, err := registry.NewRegistry()
	if err == nil {
		entry, err := reg.Get(ref)
		if err == nil && entry.CachePath != "" {
			// Found in local registry — verify cache still exists on disk
			dcFile := findDatacenterFile(entry.CachePath)
			if dcFile != "" {
				return e.dcLoader.Load(dcFile)
			}
			// Cache directory is gone; fall through to remote pull
		}
	}

	// Not in local registry — pull from remote OCI registry
	return e.loadDatacenterFromOCI(context.Background(), ref)
}

// loadDatacenterFromOCI pulls a datacenter artifact from a remote OCI registry,
// caches it locally, registers it in the unified artifact registry, and loads it.
func (e *Engine) loadDatacenterFromOCI(ctx context.Context, ref string) (datacenter.Datacenter, error) {
	dcDir, err := registry.CachePathForRef(ref)
	if err != nil {
		return nil, fmt.Errorf("failed to compute cache path: %w", err)
	}

	// Remove any stale cache
	os.RemoveAll(dcDir)

	// Pull from registry
	if err := os.MkdirAll(dcDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	if err := e.ociClient.Pull(ctx, ref, dcDir); err != nil {
		os.RemoveAll(dcDir)
		return nil, fmt.Errorf("failed to pull datacenter from registry: %w", err)
	}

	// Find the datacenter file in pulled content
	dcFile := findDatacenterFile(dcDir)
	if dcFile == "" {
		os.RemoveAll(dcDir)
		return nil, fmt.Errorf("no datacenter.dc or datacenter.hcl found in pulled artifact: %s", ref)
	}

	// Calculate size
	var totalSize int64
	_ = filepath.Walk(dcDir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})

	// Get digest if available
	digest := ""
	remoteConfig, err := e.ociClient.PullConfig(ctx, ref)
	if err == nil && len(remoteConfig) > 0 {
		digest = fmt.Sprintf("sha256:%x", remoteConfig)
		if len(digest) > 71 {
			digest = digest[:71] + "..."
		}
	}

	// Register in unified artifact registry
	reg, regErr := registry.NewRegistry()
	if regErr == nil {
		repo, tag := registry.ParseReference(ref)
		entry := registry.ArtifactEntry{
			Reference:  ref,
			Repository: repo,
			Tag:        tag,
			Type:       registry.TypeDatacenter,
			Digest:     digest,
			Source:     registry.SourcePulled,
			Size:       totalSize,
			CreatedAt:  time.Now(),
			CachePath:  dcDir,
		}
		_ = reg.Add(entry)
	}

	return e.dcLoader.Load(dcFile)
}

// findDatacenterFile looks for a datacenter config file in the given directory.
// Returns the path to the file if found, or empty string if not.
func findDatacenterFile(dir string) string {
	dcFile := filepath.Join(dir, "datacenter.dc")
	if _, err := os.Stat(dcFile); err == nil {
		return dcFile
	}
	hclFile := filepath.Join(dir, "datacenter.hcl")
	if _, err := os.Stat(hclFile); err == nil {
		return hclFile
	}
	return ""
}

// DestroyOptions configures a destroy operation.
type DestroyOptions struct {
	// Environment name
	Environment string

	// Datacenter name
	Datacenter string

	// Output writer for progress
	Output io.Writer

	// DryRun only plans without executing
	DryRun bool

	// AutoApprove skips confirmation
	AutoApprove bool
}

// DestroyResult contains the results of a destroy operation.
type DestroyResult struct {
	Success   bool
	Plan      *planner.Plan
	Execution *executor.ExecutionResult
	Duration  time.Duration
}

// Destroy destroys an environment.
func (e *Engine) Destroy(ctx context.Context, opts DestroyOptions) (*DestroyResult, error) {
	startTime := time.Now()

	result := &DestroyResult{}

	// Get current state
	currentState, err := e.stateManager.GetEnvironment(ctx, opts.Datacenter, opts.Environment)
	if err != nil {
		return nil, fmt.Errorf("environment %s not found in datacenter %s", opts.Environment, opts.Datacenter)
	}

	// Build graph from current state
	g := graph.NewGraph(opts.Environment, opts.Datacenter)

	for compName, compState := range currentState.Components {
		for resName, resState := range compState.Resources {
			node := graph.NewNode(graph.NodeType(resState.Type), compName, resName)
			node.Inputs = resState.Inputs
			node.Outputs = resState.Outputs
			_ = g.AddNode(node)
		}
	}

	// Create destroy plan
	p := planner.NewPlanner()
	plan, err := p.PlanDestroy(g, currentState)
	if err != nil {
		return nil, fmt.Errorf("failed to create destroy plan: %w", err)
	}

	result.Plan = plan

	// Print plan summary
	if opts.Output != nil {
		e.printDestroyPlanSummary(opts.Output, plan)
	}

	// If dry run, return here
	if opts.DryRun || plan.IsEmpty() {
		result.Success = plan.IsEmpty() || opts.DryRun
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Execute plan
	execOpts := executor.Options{
		Parallelism: 1,
		Output:      opts.Output,
		DryRun:      false,
		StopOnError: true,
	}

	exec := executor.NewExecutor(e.stateManager, e.iacRegistry, execOpts)
	execResult, err := exec.Execute(ctx, plan, g)
	if err != nil {
		return nil, fmt.Errorf("destroy failed: %w", err)
	}

	result.Execution = execResult
	result.Success = execResult.Success
	result.Duration = time.Since(startTime)

	// Delete environment state if successful
	if result.Success {
		if err := e.stateManager.DeleteEnvironment(ctx, opts.Datacenter, opts.Environment); err != nil {
			// Log but don't fail
			fmt.Fprintf(opts.Output, "Warning: failed to delete environment state: %v\n", err)
		}
	}

	return result, nil
}

// DestroyComponentOptions configures a component destroy operation.
type DestroyComponentOptions struct {
	// Environment name
	Environment string

	// Datacenter name
	Datacenter string

	// Component name to destroy
	Component string

	// Output writer for progress
	Output io.Writer

	// DryRun only plans without executing
	DryRun bool

	// AutoApprove skips confirmation
	AutoApprove bool

	// Force allows destroying a component even if other components depend on it
	Force bool
}

// FindDependents returns the names of components in the environment that depend
// on the given component. This is used to prevent destroying a component that
// other components rely on.
func FindDependents(envState *types.EnvironmentState, targetComponent string) []string {
	var dependents []string
	for compName, compState := range envState.Components {
		if compName == targetComponent {
			continue
		}
		for _, dep := range compState.Dependencies {
			if dep == targetComponent {
				dependents = append(dependents, compName)
				break
			}
		}
	}
	sort.Strings(dependents)
	return dependents
}

// DestroyComponent destroys a single component within an environment.
func (e *Engine) DestroyComponent(ctx context.Context, opts DestroyComponentOptions) (*DestroyResult, error) {
	startTime := time.Now()

	result := &DestroyResult{}

	// Get current environment state
	currentState, err := e.stateManager.GetEnvironment(ctx, opts.Datacenter, opts.Environment)
	if err != nil {
		return nil, fmt.Errorf("environment %s not found in datacenter %s", opts.Environment, opts.Datacenter)
	}

	// Get component state
	compState, ok := currentState.Components[opts.Component]
	if !ok {
		return nil, fmt.Errorf("component %s not found in environment %s", opts.Component, opts.Environment)
	}

	// Check for dependents unless --force is used
	if !opts.Force {
		dependents := FindDependents(currentState, opts.Component)
		if len(dependents) > 0 {
			return nil, fmt.Errorf(
				"cannot destroy component %q because the following components depend on it: %s\n"+
					"Destroy those components first, or use --force to override",
				opts.Component, strings.Join(dependents, ", "),
			)
		}
	}

	// Build graph from component state only
	g := graph.NewGraph(opts.Environment, opts.Datacenter)

	for resName, resState := range compState.Resources {
		node := graph.NewNode(graph.NodeType(resState.Type), opts.Component, resName)
		node.Inputs = resState.Inputs
		node.Outputs = resState.Outputs
		_ = g.AddNode(node)
	}

	// Create destroy plan for just this component
	p := planner.NewPlanner()
	plan, err := p.PlanDestroy(g, currentState)
	if err != nil {
		return nil, fmt.Errorf("failed to create destroy plan: %w", err)
	}

	result.Plan = plan

	// Print plan summary
	if opts.Output != nil {
		e.printDestroyPlanSummary(opts.Output, plan)
	}

	// If dry run, return here
	if opts.DryRun || plan.IsEmpty() {
		result.Success = plan.IsEmpty() || opts.DryRun
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Execute plan
	execOpts := executor.Options{
		Parallelism: 1,
		Output:      opts.Output,
		DryRun:      false,
		StopOnError: true,
	}

	exec := executor.NewExecutor(e.stateManager, e.iacRegistry, execOpts)
	execResult, err := exec.Execute(ctx, plan, g)
	if err != nil {
		return nil, fmt.Errorf("destroy failed: %w", err)
	}

	result.Execution = execResult
	result.Success = execResult.Success
	result.Duration = time.Since(startTime)

	// Delete component state if successful (but not the entire environment)
	if result.Success {
		if err := e.stateManager.DeleteComponent(ctx, opts.Datacenter, opts.Environment, opts.Component); err != nil {
			if opts.Output != nil {
				fmt.Fprintf(opts.Output, "Warning: failed to delete component state: %v\n", err)
			}
		}
	}

	return result, nil
}

// DeployFromEnvironmentFile deploys from an environment.yml file.
func (e *Engine) DeployFromEnvironmentFile(ctx context.Context, envPath string, opts DeployOptions) (*DeployResult, error) {
	// Load environment file
	env, err := e.envLoader.Load(envPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load environment file: %w", err)
	}

	// Note: Name and Datacenter are CLI parameters, not part of the config file
	// They must be provided via opts.Environment and opts.Datacenter

	// Build components map from environment
	if opts.Components == nil {
		opts.Components = make(map[string]string)
	}
	if opts.Variables == nil {
		opts.Variables = make(map[string]map[string]interface{})
	}

	for name, compConfig := range env.Components() {
		// The key is the registry address, source is the version tag or file path
		// If source starts with "./" or "../" or "/", it's a file path, otherwise it's a version tag
		source := compConfig.Source()
		if isFilePath(source) {
			opts.Components[name] = source
		} else {
			// Combine registry address (key) with version tag to get full OCI reference
			opts.Components[name] = name + ":" + source
		}
		opts.Variables[name] = compConfig.Variables()
	}

	return e.Deploy(ctx, opts)
}

func (e *Engine) printPlanSummary(w io.Writer, plan *planner.Plan) {
	fmt.Fprintf(w, "\nPlan Summary:\n")
	fmt.Fprintf(w, "  Environment: %s\n", plan.Environment)
	fmt.Fprintf(w, "  Datacenter:  %s\n", plan.Datacenter)
	fmt.Fprintf(w, "\n")

	if plan.IsEmpty() {
		fmt.Fprintf(w, "No changes required.\n")
		return
	}

	fmt.Fprintf(w, "Changes:\n")
	for _, change := range plan.Changes {
		if change.Action == planner.ActionNoop {
			continue
		}

		actionSymbol := "?"
		switch change.Action {
		case planner.ActionCreate:
			actionSymbol = "+"
		case planner.ActionUpdate:
			actionSymbol = "~"
		case planner.ActionDelete:
			actionSymbol = "-"
		case planner.ActionReplace:
			actionSymbol = "±"
		}

		nodeID := "(unknown)"
		if change.Node != nil {
			nodeID = change.Node.ID
		}

		fmt.Fprintf(w, "  %s %s\n", actionSymbol, nodeID)
	}

	fmt.Fprintf(w, "\nSummary: %d to create, %d to update, %d to delete, %d unchanged\n",
		plan.ToCreate, plan.ToUpdate, plan.ToDelete, plan.NoChange)
}

func (e *Engine) printDestroyPlanSummary(w io.Writer, plan *planner.Plan) {
	fmt.Fprintf(w, "\nDestroy Plan:\n")
	fmt.Fprintf(w, "  Environment: %s\n", plan.Environment)
	fmt.Fprintf(w, "\n")

	if plan.IsEmpty() {
		fmt.Fprintf(w, "No resources to destroy.\n")
		return
	}

	fmt.Fprintf(w, "Resources to destroy:\n")
	for _, change := range plan.Changes {
		nodeID := "(unknown)"
		if change.Node != nil {
			nodeID = change.Node.ID
		}
		fmt.Fprintf(w, "  - %s\n", nodeID)
	}

	fmt.Fprintf(w, "\nTotal: %d resources to destroy\n", plan.ToDelete)
}

// DeployDatacenter provisions root-level modules defined in the datacenter and
// reconciles all existing environments. This is called by `arcctl deploy datacenter`.
func (e *Engine) DeployDatacenter(ctx context.Context, opts DeployDatacenterOptions) (*DeployDatacenterResult, error) {
	startTime := time.Now()
	result := &DeployDatacenterResult{
		ModuleOutputs: make(map[string]map[string]interface{}),
	}

	// Load datacenter state
	dcState, err := e.stateManager.GetDatacenter(ctx, opts.Datacenter)
	if err != nil {
		return nil, fmt.Errorf("datacenter %q not found: %w", opts.Datacenter, err)
	}

	// Load the datacenter configuration
	dcPath := dcState.Version
	if dcPath == "" {
		return nil, fmt.Errorf("datacenter %q has no source path configured", opts.Datacenter)
	}
	dc, err := e.loadDatacenterConfig(dcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load datacenter configuration: %w", err)
	}

	// Build datacenter variables map
	dcVars := make(map[string]interface{})
	for k, v := range dcState.Variables {
		dcVars[k] = v
	}
	// Fill in defaults from the schema for any unset variables
	for _, v := range dc.Variables() {
		if _, ok := dcVars[v.Name()]; !ok && v.Default() != nil {
			dcVars[v.Name()] = v.Default()
		}
	}

	// Phase 1: Provision root-level modules
	rootModules := dc.Modules()
	if len(rootModules) > 0 {
		if opts.Output != nil {
			fmt.Fprintf(opts.Output, "\nProvisioning %d root-level module(s)...\n", len(rootModules))
		}

		// Ensure Modules map is initialized
		if dcState.Modules == nil {
			dcState.Modules = make(map[string]*types.ModuleState)
		}

		for _, mod := range rootModules {
			modName := mod.Name()

			// Evaluate when condition
			if mod.When() != "" {
				// Root modules with when conditions are skipped if condition is false
				// For now, root modules typically don't have when conditions
				continue
			}

			if opts.OnProgress != nil {
				opts.OnProgress(executor.ProgressEvent{
					NodeID:   "root/module/" + modName,
					NodeName: modName,
					NodeType: "module",
					Status:   "running",
					Message:  "Provisioning root module...",
				})
			}

			// Resolve module path
			dcDir := filepath.Dir(dc.SourcePath())
			modulePath := mod.Build()
			if modulePath == "" {
				modulePath = mod.Source()
			}
			if modulePath != "" && !filepath.IsAbs(modulePath) {
				modulePath = filepath.Join(dcDir, modulePath)
			}

			// Build module inputs by substituting variable references
			inputs := make(map[string]interface{})
			for inputName, exprStr := range mod.Inputs() {
				inputs[inputName] = evaluateModuleExpression(exprStr, dcVars, nil, nil)
			}

			// Get IaC plugin
			pluginName := mod.Plugin()
			if pluginName == "" {
				pluginName = "native"
			}
			plugin, err := e.iacRegistry.Get(pluginName)
			if err != nil {
				return nil, fmt.Errorf("failed to get IaC plugin %q for module %s: %w", pluginName, modName, err)
			}

			// Check for existing state (for updates)
			existingMod := dcState.Modules[modName]

			runOpts := iac.RunOptions{
				ModuleSource: modulePath,
				Inputs:       inputs,
				Environment:  map[string]string{},
			}
			if existingMod != nil && existingMod.IaCState != nil {
				// TODO: Pass existing state via StateReader for incremental updates
				_ = existingMod
			}

			// Mark as applying
			dcState.Modules[modName] = &types.ModuleState{
				Name:      modName,
				Plugin:    pluginName,
				Source:    modulePath,
				Inputs:    inputs,
				Status:    types.ModuleStatusApplying,
				UpdatedAt: time.Now(),
			}
			dcState.UpdatedAt = time.Now()
			_ = e.stateManager.SaveDatacenter(ctx, dcState)

			// Apply
			applyResult, err := plugin.Apply(ctx, runOpts)
			if err != nil {
				dcState.Modules[modName].Status = types.ModuleStatusFailed
				dcState.Modules[modName].StatusReason = err.Error()
				dcState.UpdatedAt = time.Now()
				_ = e.stateManager.SaveDatacenter(ctx, dcState)

				if opts.OnProgress != nil {
					opts.OnProgress(executor.ProgressEvent{
						NodeID:   "root/module/" + modName,
						NodeName: modName,
						NodeType: "module",
						Status:   "failed",
						Error:    err,
					})
				}

				result.Success = false
				result.Duration = time.Since(startTime)
				return result, fmt.Errorf("failed to apply root module %s: %w", modName, err)
			}

			// Store outputs
			outputs := make(map[string]interface{})
			for name, out := range applyResult.Outputs {
				outputs[name] = out.Value
			}
			result.ModuleOutputs[modName] = outputs

			dcState.Modules[modName] = &types.ModuleState{
				Name:      modName,
				Plugin:    pluginName,
				Source:    modulePath,
				Inputs:    inputs,
				Outputs:   outputs,
				IaCState:  applyResult.State,
				Status:    types.ModuleStatusReady,
				UpdatedAt: time.Now(),
			}
			dcState.UpdatedAt = time.Now()
			_ = e.stateManager.SaveDatacenter(ctx, dcState)

			if opts.OnProgress != nil {
				opts.OnProgress(executor.ProgressEvent{
					NodeID:   "root/module/" + modName,
					NodeName: modName,
					NodeType: "module",
					Status:   "completed",
					Message:  "Root module ready",
				})
			}

			if opts.Output != nil {
				fmt.Fprintf(opts.Output, "  [success] Module %q provisioned\n", modName)
			}
		}
	}

	// Phase 2: Reconcile existing environments
	envs, err := e.stateManager.ListEnvironments(ctx, opts.Datacenter)
	if err == nil && len(envs) > 0 {
		if opts.Output != nil {
			fmt.Fprintf(opts.Output, "\nReconciling %d environment(s)...\n", len(envs))
		}

		for _, envRef := range envs {
			envState, err := e.stateManager.GetEnvironment(ctx, opts.Datacenter, envRef.Name)
			if err != nil {
				if opts.Output != nil {
					fmt.Fprintf(opts.Output, "  [warning] Could not load environment %q: %v\n", envRef.Name, err)
				}
				continue
			}

			// Re-deploy environment-scoped modules
			envResult, err := e.DeployEnvironment(ctx, DeployEnvironmentOptions{
				Datacenter:  opts.Datacenter,
				Environment: envRef.Name,
				Output:      opts.Output,
				OnProgress:  opts.OnProgress,
				Parallelism: opts.Parallelism,
			})
			if err != nil {
				if opts.Output != nil {
					fmt.Fprintf(opts.Output, "  [warning] Failed to reconcile env modules for %q: %v\n", envRef.Name, err)
				}
			}
			_ = envResult

			// Re-deploy each component with force update
			if len(envState.Components) > 0 {
				components := make(map[string]string)
				variables := make(map[string]map[string]interface{})

				for compName, compState := range envState.Components {
					if compState.Source != "" {
						components[compName] = compState.Source
					}
					if compState.Variables != nil {
						vars := make(map[string]interface{})
						for k, v := range compState.Variables {
							vars[k] = v
						}
						variables[compName] = vars
					}
				}

				if len(components) > 0 {
					if opts.Output != nil {
						fmt.Fprintf(opts.Output, "  Reconciling %d component(s) in %q...\n", len(components), envRef.Name)
					}

					deployResult, err := e.Deploy(ctx, DeployOptions{
						Environment: envRef.Name,
						Datacenter:  opts.Datacenter,
						Components:  components,
						Variables:   variables,
						Output:      opts.Output,
						Parallelism: opts.Parallelism,
						AutoApprove: true,
						OnProgress:  opts.OnProgress,
						ForceUpdate: true,
					})
					if err != nil {
						if opts.Output != nil {
							fmt.Fprintf(opts.Output, "  [warning] Failed to reconcile components in %q: %v\n", envRef.Name, err)
						}
					} else if deployResult.Success {
						if opts.Output != nil {
							fmt.Fprintf(opts.Output, "  [success] Components in %q reconciled\n", envRef.Name)
						}
					}
				}
			}
		}
	}

	result.Success = true
	result.Duration = time.Since(startTime)
	return result, nil
}

// DeployEnvironment provisions environment-scoped modules (modules inside the
// `environment {}` block but outside hooks). Called by `create environment`,
// `update environment`, and as part of datacenter reconciliation.
func (e *Engine) DeployEnvironment(ctx context.Context, opts DeployEnvironmentOptions) (*DeployEnvironmentResult, error) {
	startTime := time.Now()
	result := &DeployEnvironmentResult{
		ModuleOutputs: make(map[string]map[string]interface{}),
	}

	// Load datacenter state
	dcState, err := e.stateManager.GetDatacenter(ctx, opts.Datacenter)
	if err != nil {
		return nil, fmt.Errorf("datacenter %q not found: %w", opts.Datacenter, err)
	}

	// Load the datacenter configuration
	dcPath := dcState.Version
	if dcPath == "" {
		return nil, fmt.Errorf("datacenter %q has no source path configured", opts.Datacenter)
	}
	dc, err := e.loadDatacenterConfig(dcPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load datacenter configuration: %w", err)
	}

	envBlock := dc.Environment()
	if envBlock == nil {
		result.Success = true
		result.Duration = time.Since(startTime)
		return result, nil
	}

	envModules := envBlock.Modules()
	if len(envModules) == 0 {
		result.Success = true
		result.Duration = time.Since(startTime)
		return result, nil
	}

	// Build datacenter variables map
	dcVars := make(map[string]interface{})
	for k, v := range dcState.Variables {
		dcVars[k] = v
	}
	for _, v := range dc.Variables() {
		if _, ok := dcVars[v.Name()]; !ok && v.Default() != nil {
			dcVars[v.Name()] = v.Default()
		}
	}

	// Collect root module outputs for cross-module references
	rootOutputs := make(map[string]map[string]interface{})
	if dcState.Modules != nil {
		for name, mod := range dcState.Modules {
			if mod.Outputs != nil {
				rootOutputs[name] = mod.Outputs
			}
		}
	}

	// Load environment state
	envState, err := e.stateManager.GetEnvironment(ctx, opts.Datacenter, opts.Environment)
	if err != nil {
		return nil, fmt.Errorf("environment %q not found in datacenter %q: %w", opts.Environment, opts.Datacenter, err)
	}

	// Ensure Modules map is initialized
	if envState.Modules == nil {
		envState.Modules = make(map[string]*types.ModuleState)
	}

	if opts.Output != nil {
		fmt.Fprintf(opts.Output, "  Provisioning %d environment module(s) for %q...\n", len(envModules), opts.Environment)
	}

	for _, mod := range envModules {
		modName := mod.Name()

		if opts.OnProgress != nil {
			opts.OnProgress(executor.ProgressEvent{
				NodeID:   fmt.Sprintf("env/%s/module/%s", opts.Environment, modName),
				NodeName: modName,
				NodeType: "module",
				Status:   "running",
				Message:  "Provisioning environment module...",
			})
		}

		// Resolve module path
		dcDir := filepath.Dir(dc.SourcePath())
		modulePath := mod.Build()
		if modulePath == "" {
			modulePath = mod.Source()
		}
		if modulePath != "" && !filepath.IsAbs(modulePath) {
			modulePath = filepath.Join(dcDir, modulePath)
		}

		// Build module inputs by substituting variable and environment references
		inputs := make(map[string]interface{})
		for inputName, exprStr := range mod.Inputs() {
			inputs[inputName] = evaluateModuleExpression(exprStr, dcVars, rootOutputs, map[string]string{
				"environment.name": opts.Environment,
			})
		}

		// Get IaC plugin
		pluginName := mod.Plugin()
		if pluginName == "" {
			pluginName = "native"
		}
		plugin, err := e.iacRegistry.Get(pluginName)
		if err != nil {
			return nil, fmt.Errorf("failed to get IaC plugin %q for module %s: %w", pluginName, modName, err)
		}

		runOpts := iac.RunOptions{
			ModuleSource: modulePath,
			Inputs:       inputs,
			Environment:  map[string]string{},
		}

		// Mark as applying
		envState.Modules[modName] = &types.ModuleState{
			Name:      modName,
			Plugin:    pluginName,
			Source:    modulePath,
			Inputs:    inputs,
			Status:    types.ModuleStatusApplying,
			UpdatedAt: time.Now(),
		}
		envState.UpdatedAt = time.Now()
		_ = e.stateManager.SaveEnvironment(ctx, opts.Datacenter, envState)

		// Apply
		applyResult, err := plugin.Apply(ctx, runOpts)
		if err != nil {
			envState.Modules[modName].Status = types.ModuleStatusFailed
			envState.Modules[modName].StatusReason = err.Error()
			envState.UpdatedAt = time.Now()
			_ = e.stateManager.SaveEnvironment(ctx, opts.Datacenter, envState)

			if opts.OnProgress != nil {
				opts.OnProgress(executor.ProgressEvent{
					NodeID:   fmt.Sprintf("env/%s/module/%s", opts.Environment, modName),
					NodeName: modName,
					NodeType: "module",
					Status:   "failed",
					Error:    err,
				})
			}

			result.Success = false
			result.Duration = time.Since(startTime)
			return result, fmt.Errorf("failed to apply environment module %s: %w", modName, err)
		}

		// Store outputs
		outputs := make(map[string]interface{})
		for name, out := range applyResult.Outputs {
			outputs[name] = out.Value
		}
		result.ModuleOutputs[modName] = outputs

		envState.Modules[modName] = &types.ModuleState{
			Name:      modName,
			Plugin:    pluginName,
			Source:    modulePath,
			Inputs:    inputs,
			Outputs:   outputs,
			IaCState:  applyResult.State,
			Status:    types.ModuleStatusReady,
			UpdatedAt: time.Now(),
		}
		envState.UpdatedAt = time.Now()
		_ = e.stateManager.SaveEnvironment(ctx, opts.Datacenter, envState)

		if opts.OnProgress != nil {
			opts.OnProgress(executor.ProgressEvent{
				NodeID:   fmt.Sprintf("env/%s/module/%s", opts.Environment, modName),
				NodeName: modName,
				NodeType: "module",
				Status:   "completed",
				Message:  "Environment module ready",
			})
		}

		if opts.Output != nil {
			fmt.Fprintf(opts.Output, "    [success] Module %q provisioned\n", modName)
		}
	}

	result.Success = true
	result.Duration = time.Since(startTime)
	return result, nil
}

// DestroyEnvironment destroys environment-scoped modules and all component
// resources for an environment using the engine. Called by `destroy environment`.
func (e *Engine) DestroyEnvironment(ctx context.Context, datacenterName, envName string, output io.Writer, onProgress executor.ProgressCallback) error {
	// Load datacenter state
	dcState, err := e.stateManager.GetDatacenter(ctx, datacenterName)
	if err != nil {
		return fmt.Errorf("datacenter %q not found: %w", datacenterName, err)
	}

	// Load environment state
	envState, err := e.stateManager.GetEnvironment(ctx, datacenterName, envName)
	if err != nil {
		return fmt.Errorf("environment %q not found: %w", envName, err)
	}

	// Load datacenter config for plugin resolution
	dc, err := e.loadDatacenterConfig(dcState.Version)
	if err != nil {
		// If we can't load the config, we can still try destroying from stored state
		if output != nil {
			fmt.Fprintf(output, "  [warning] Could not load datacenter config: %v\n", err)
		}
	}

	// Phase 1: Destroy all component resources using eng.DestroyComponent
	if envState.Components != nil {
		for compName := range envState.Components {
			if output != nil {
				fmt.Fprintf(output, "  Destroying component %q...\n", compName)
			}
			_, err := e.DestroyComponent(ctx, DestroyComponentOptions{
				Datacenter:  datacenterName,
				Environment: envName,
				Component:   compName,
				Output:      output,
				Force:       true, // Force since we're destroying the whole environment
				AutoApprove: true,
			})
			if err != nil {
				if output != nil {
					fmt.Fprintf(output, "  [warning] Failed to destroy component %q: %v\n", compName, err)
				}
			}
		}
	}

	// Phase 2: Destroy environment-scoped modules (in reverse order)
	if len(envState.Modules) > 0 {
		if output != nil {
			fmt.Fprintf(output, "  Destroying %d environment module(s)...\n", len(envState.Modules))
		}

		for modName, modState := range envState.Modules {
			if modState.Status == types.ModuleStatusFailed || modState.Plugin == "" {
				continue
			}

			pluginName := modState.Plugin
			plugin, err := e.iacRegistry.Get(pluginName)
			if err != nil {
				if output != nil {
					fmt.Fprintf(output, "  [warning] Could not get plugin %q for module %s: %v\n", pluginName, modName, err)
				}
				continue
			}

			runOpts := iac.RunOptions{
				ModuleSource: modState.Source,
				Inputs:       modState.Inputs,
				Environment:  map[string]string{},
			}

			if err := plugin.Destroy(ctx, runOpts); err != nil {
				if output != nil {
					fmt.Fprintf(output, "  [warning] Failed to destroy module %s: %v\n", modName, err)
				}
			} else {
				if output != nil {
					fmt.Fprintf(output, "  [success] Module %q destroyed\n", modName)
				}
			}
		}
	}

	_ = dc // Used for future expression-aware destroy

	return nil
}

// evaluateModuleExpression evaluates a simple expression string used in datacenter
// module inputs. Supports ${variable.*}, ${environment.name}, and ${module.*.*} references.
func evaluateModuleExpression(expr string, dcVars map[string]interface{}, moduleOutputs map[string]map[string]interface{}, extras map[string]string) interface{} {
	expr = strings.TrimSpace(expr)

	// Handle string interpolation ${...}
	if strings.Contains(expr, "${") {
		result := expr
		// Replace ${variable.*}
		for k, v := range dcVars {
			if s, ok := v.(string); ok {
				result = strings.ReplaceAll(result, "${variable."+k+"}", s)
			} else {
				result = strings.ReplaceAll(result, "${variable."+k+"}", fmt.Sprintf("%v", v))
			}
		}
		// Replace extras (e.g., ${environment.name})
		for k, v := range extras {
			result = strings.ReplaceAll(result, "${"+k+"}", v)
		}
		// Replace ${module.*.*}
		for modName, outputs := range moduleOutputs {
			for outName, outVal := range outputs {
				if s, ok := outVal.(string); ok {
					result = strings.ReplaceAll(result, "${module."+modName+"."+outName+"}", s)
				} else {
					result = strings.ReplaceAll(result, "${module."+modName+"."+outName+"}", fmt.Sprintf("%v", outVal))
				}
			}
		}
		return result
	}

	// Handle direct variable references
	if strings.HasPrefix(expr, "variable.") {
		varName := expr[len("variable."):]
		if v, ok := dcVars[varName]; ok {
			return v
		}
		return expr
	}

	// Handle direct extras
	if v, ok := extras[expr]; ok {
		return v
	}

	// Handle module output references
	if strings.HasPrefix(expr, "module.") && moduleOutputs != nil {
		parts := strings.SplitN(expr[len("module."):], ".", 2)
		if len(parts) == 2 {
			if outputs, ok := moduleOutputs[parts[0]]; ok {
				if v, ok := outputs[parts[1]]; ok {
					return v
				}
			}
		}
	}

	return expr
}

// isFilePath checks if a source string is a file path (starts with "./", "../", or "/").
func isFilePath(source string) bool {
	return strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") || strings.HasPrefix(source, "/")
}

// findComponentFile looks for a component config file in the given directory.
// Returns the path to the file if found, or empty string if not.
func findComponentFile(dir string) string {
	ymlFile := filepath.Join(dir, "architect.yml")
	if _, err := os.Stat(ymlFile); err == nil {
		return ymlFile
	}
	yamlFile := filepath.Join(dir, "architect.yaml")
	if _, err := os.Stat(yamlFile); err == nil {
		return yamlFile
	}
	return ""
}

// loadComponentConfig resolves a component OCI reference to a local file path.
// Resolution order: unified artifact registry (local cache) → remote OCI pull.
// Returns the local path to the architect.yml file.
func (e *Engine) loadComponentConfig(ctx context.Context, ref string) (string, error) {
	// Check the unified artifact registry first (like docker run).
	reg, err := registry.NewRegistry()
	if err == nil {
		entry, err := reg.Get(ref)
		if err == nil && entry.CachePath != "" {
			compFile := findComponentFile(entry.CachePath)
			if compFile != "" {
				return compFile, nil
			}
			// Cache directory is gone; fall through to remote pull
		}
	}

	// Not in local registry — pull from remote OCI registry
	return e.loadComponentFromOCI(ctx, ref)
}

// loadComponentFromOCI pulls a component artifact from a remote OCI registry,
// caches it locally, registers it in the unified artifact registry, and returns
// the local path to the architect.yml file.
func (e *Engine) loadComponentFromOCI(ctx context.Context, ref string) (string, error) {
	compDir, err := registry.CachePathForRef(ref)
	if err != nil {
		return "", fmt.Errorf("failed to compute cache path: %w", err)
	}

	// Remove any stale cache
	os.RemoveAll(compDir)

	// Pull from registry
	if err := os.MkdirAll(compDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	if err := e.ociClient.Pull(ctx, ref, compDir); err != nil {
		os.RemoveAll(compDir)
		return "", fmt.Errorf("failed to pull component from registry: %w", err)
	}

	// Find the component file in pulled content
	compFile := findComponentFile(compDir)
	if compFile == "" {
		os.RemoveAll(compDir)
		return "", fmt.Errorf("no architect.yml or architect.yaml found in pulled artifact: %s", ref)
	}

	// Calculate size
	var totalSize int64
	_ = filepath.Walk(compDir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return nil
	})

	// Get digest if available
	digest := ""
	remoteConfig, err := e.ociClient.PullConfig(ctx, ref)
	if err == nil && len(remoteConfig) > 0 {
		digest = fmt.Sprintf("sha256:%x", remoteConfig)
		if len(digest) > 71 {
			digest = digest[:71] + "..."
		}
	}

	// Register in unified artifact registry
	reg, regErr := registry.NewRegistry()
	if regErr == nil {
		repo, tag := registry.ParseReference(ref)
		entry := registry.ArtifactEntry{
			Reference:  ref,
			Repository: repo,
			Tag:        tag,
			Type:       registry.TypeComponent,
			Digest:     digest,
			Source:     registry.SourcePulled,
			Size:       totalSize,
			CreatedAt:  time.Now(),
			CachePath:  compDir,
		}
		_ = reg.Add(entry)
	}

	return compFile, nil
}

// DependencyInfo describes a component dependency that needs to be deployed.
type DependencyInfo struct {
	// Name is the dependency name (key in the dependencies map).
	Name string

	// OCIRef is the OCI reference for the dependency component.
	OCIRef string

	// LocalPath is the resolved local file path to the architect.yml.
	LocalPath string

	// Component is the loaded component schema.
	Component component.Component

	// MissingVariables lists required variables that have no default and were not provided.
	MissingVariables []component.Variable
}

// ResolveDependencies discovers component dependencies that are not yet deployed
// to the target environment. It recursively walks the full transitive dependency
// tree, pulling OCI artifacts as needed, and returns a list of DependencyInfo
// for every dependency that needs to be deployed.
//
// Components already present in the environment state are skipped (not updated).
// Circular dependencies are detected and result in an error.
func (e *Engine) ResolveDependencies(ctx context.Context, opts DeployOptions) ([]DependencyInfo, error) {
	// Get current environment state to check which components already exist
	envState, _ := e.stateManager.GetEnvironment(ctx, opts.Datacenter, opts.Environment)

	// Track what's already being deployed (primary components) or already in the environment
	deployed := make(map[string]bool)
	if envState != nil {
		for compName := range envState.Components {
			deployed[compName] = true
		}
	}
	for compName := range opts.Components {
		deployed[compName] = true
	}

	// Track components being visited for cycle detection
	visiting := make(map[string]bool)
	// Track resolved dependencies to avoid duplicates
	resolved := make(map[string]*DependencyInfo)
	// Maintain insertion order for deterministic output
	var orderedNames []string

	// resolveRecursive walks the dependency tree depth-first
	var resolveRecursive func(comp component.Component, parentName string) error
	resolveRecursive = func(comp component.Component, parentName string) error {
		for _, dep := range comp.Dependencies() {
			depName := dep.Name()
			depRef := dep.Component()

			// Skip if already deployed in the environment or being deployed in this batch
			if deployed[depName] {
				continue
			}

			// Skip if already resolved as a dependency
			if _, ok := resolved[depName]; ok {
				continue
			}

			// Cycle detection
			if visiting[depName] {
				return fmt.Errorf("circular dependency detected: %s -> %s", parentName, depName)
			}
			visiting[depName] = true

			// Pull and load the dependency component
			localPath, err := e.loadComponentConfig(ctx, depRef)
			if err != nil {
				return fmt.Errorf("failed to resolve dependency %q (%s): %w", depName, depRef, err)
			}

			depComp, err := e.compLoader.Load(localPath)
			if err != nil {
				return fmt.Errorf("failed to load dependency %q from %s: %w", depName, localPath, err)
			}

			// Determine missing required variables (no default, not provided)
			providedVars := opts.Variables[depName]
			var missingVars []component.Variable
			for _, v := range depComp.Variables() {
				if v.Default() != nil {
					continue
				}
				if providedVars != nil {
					if _, ok := providedVars[v.Name()]; ok {
						continue
					}
				}
				if v.Required() {
					missingVars = append(missingVars, v)
				}
			}

			info := &DependencyInfo{
				Name:             depName,
				OCIRef:           depRef,
				LocalPath:        localPath,
				Component:        depComp,
				MissingVariables: missingVars,
			}
			resolved[depName] = info
			orderedNames = append(orderedNames, depName)
			deployed[depName] = true

			// Recurse into the dependency's own dependencies
			if err := resolveRecursive(depComp, depName); err != nil {
				return err
			}

			delete(visiting, depName)
		}
		return nil
	}

	// Walk dependencies for each primary component
	for compName, compPath := range opts.Components {
		comp, err := e.compLoader.Load(compPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load component %s: %w", compName, err)
		}
		if err := resolveRecursive(comp, compName); err != nil {
			return nil, err
		}
	}

	// Build ordered result
	result := make([]DependencyInfo, 0, len(orderedNames))
	for _, name := range orderedNames {
		result = append(result, *resolved[name])
	}
	return result, nil
}
