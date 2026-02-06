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
	p := planner.NewPlanner()
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

// isFilePath checks if a source string is a file path (starts with "./", "../", or "/").
func isFilePath(source string) bool {
	return strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") || strings.HasPrefix(source, "/")
}
