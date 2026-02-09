package engine

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/davidthor/cldctl/pkg/engine/importmap"
	"github.com/davidthor/cldctl/pkg/graph"
	"github.com/davidthor/cldctl/pkg/iac"
	"github.com/davidthor/cldctl/pkg/schema/datacenter"
	"github.com/davidthor/cldctl/pkg/state/types"
)

// ImportResourceOptions configures a single resource import.
type ImportResourceOptions struct {
	// Datacenter name
	Datacenter string

	// Environment name
	Environment string

	// Component name
	Component string

	// ResourceKey is the type-qualified resource key (e.g., "database.main")
	ResourceKey string

	// Mappings are the IaC address to cloud ID mappings
	Mappings []iac.ImportMapping

	// Output writer for progress
	Output io.Writer

	// Force overwrites existing resource state
	Force bool
}

// ImportResourceResult contains the results of a resource import.
type ImportResourceResult struct {
	Success           bool
	ResourceKey       string
	ImportedResources []string
	Outputs           map[string]interface{}
	Drifts            []iac.ResourceDrift
}

// ImportComponentOptions configures a component-level import.
type ImportComponentOptions struct {
	// Datacenter name
	Datacenter string

	// Environment name
	Environment string

	// Component name
	Component string

	// Source is the OCI reference or local path for the component
	Source string

	// Variables for the component
	Variables map[string]interface{}

	// Mapping contains the resource-to-cloud-ID mappings
	Mapping *importmap.ComponentMapping

	// Output writer for progress
	Output io.Writer

	// Force overwrites existing resource state
	Force bool

	// AutoApprove skips confirmation
	AutoApprove bool
}

// ImportComponentResult contains the results of a component import.
type ImportComponentResult struct {
	Success   bool
	Component string
	Resources []ImportResourceResult
	Duration  time.Duration
}

// ImportEnvironmentOptions configures an environment-level import.
type ImportEnvironmentOptions struct {
	// Datacenter name
	Datacenter string

	// Environment name
	Environment string

	// Mapping contains the component and resource mappings
	Mapping *importmap.EnvironmentMapping

	// Output writer for progress
	Output io.Writer

	// Force overwrites existing resource state
	Force bool

	// AutoApprove skips confirmation
	AutoApprove bool
}

// ImportEnvironmentResult contains the results of an environment import.
type ImportEnvironmentResult struct {
	Success    bool
	Components []ImportComponentResult
	Duration   time.Duration
}

// ImportDatacenterModuleOptions configures a datacenter module import.
type ImportDatacenterModuleOptions struct {
	// Datacenter name
	Datacenter string

	// Module name
	Module string

	// Mappings are the IaC address to cloud ID mappings
	Mappings []iac.ImportMapping

	// Output writer for progress
	Output io.Writer

	// Force overwrites existing module state
	Force bool
}

// ImportDatacenterModuleResult contains the results of a datacenter module import.
type ImportDatacenterModuleResult struct {
	Success           bool
	Module            string
	ImportedResources []string
	Outputs           map[string]interface{}
}

// ImportEnvironmentModuleOptions configures an environment-scoped module import.
type ImportEnvironmentModuleOptions struct {
	// Datacenter name
	Datacenter string

	// Environment name
	Environment string

	// Module name (must match a module in the datacenter's environment block)
	Module string

	// Mappings are the IaC address to cloud ID mappings
	Mappings []iac.ImportMapping

	// Output writer for progress
	Output io.Writer

	// Force overwrites existing module state
	Force bool
}

// ImportEnvironmentModuleResult contains the results of an environment module import.
type ImportEnvironmentModuleResult struct {
	Success           bool
	Module            string
	ImportedResources []string
	Outputs           map[string]interface{}
}

// ImportResource imports a single existing cloud resource into cldctl state.
func (e *Engine) ImportResource(ctx context.Context, opts ImportResourceOptions) (*ImportResourceResult, error) {
	result := &ImportResourceResult{
		ResourceKey: opts.ResourceKey,
	}

	// Load datacenter configuration
	dcState, err := e.stateManager.GetDatacenter(ctx, opts.Datacenter)
	if err != nil {
		return nil, fmt.Errorf("datacenter %q not found: %w", opts.Datacenter, err)
	}

	dc, err := e.loadDatacenterConfig(dcState.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to load datacenter configuration: %w", err)
	}

	// Get environment state
	envState, err := e.stateManager.GetEnvironment(ctx, opts.Datacenter, opts.Environment)
	if err != nil {
		return nil, fmt.Errorf("environment %q not found in datacenter %q: %w", opts.Environment, opts.Datacenter, err)
	}

	// Check for existing resource state
	if !opts.Force {
		if compState, ok := envState.Components[opts.Component]; ok {
			if _, ok := compState.Resources[opts.ResourceKey]; ok {
				return nil, fmt.Errorf("resource %s already exists in component %s (use --force to overwrite)", opts.ResourceKey, opts.Component)
			}
		}
	}

	// Parse the resource key into type and name
	resType, resName, err := parseResourceKey(opts.ResourceKey)
	if err != nil {
		return nil, err
	}

	// Build datacenter variables
	dcVars := make(map[string]interface{})
	for k, v := range dcState.Variables {
		dcVars[k] = v
	}

	// Find matching hook for this resource type
	node := &graph.Node{
		ID:        fmt.Sprintf("%s/%s/%s", opts.Component, resType, resName),
		Type:      graph.NodeType(resType),
		Name:      resName,
		Component: opts.Component,
		Inputs:    make(map[string]interface{}),
	}

	modulePath, moduleInputs, pluginName, err := findMatchingHookForImport(dc, node, opts.Environment, dcVars)
	if err != nil {
		return nil, fmt.Errorf("failed to find matching hook for %s: %w", opts.ResourceKey, err)
	}

	// Get IaC plugin
	if pluginName == "" {
		pluginName = "native"
	}
	plugin, err := e.iacRegistry.Get(pluginName)
	if err != nil {
		return nil, fmt.Errorf("failed to get IaC plugin %q: %w", pluginName, err)
	}

	// Run import
	importOpts := iac.ImportOptions{
		ModuleSource: modulePath,
		Inputs:       moduleInputs,
		Mappings:     opts.Mappings,
		Environment:  map[string]string{},
	}

	if opts.Output != nil {
		fmt.Fprintf(opts.Output, "  Importing %s (%d IaC resource(s))...\n", opts.ResourceKey, len(opts.Mappings))
	}

	importResult, err := plugin.Import(ctx, importOpts)
	if err != nil {
		return nil, fmt.Errorf("import failed for %s: %w", opts.ResourceKey, err)
	}

	// Extract outputs
	outputs := make(map[string]interface{})
	for name, out := range importResult.Outputs {
		outputs[name] = out.Value
	}

	result.Outputs = outputs
	result.ImportedResources = importResult.ImportedResources

	// Save resource state
	if envState.Components == nil {
		envState.Components = make(map[string]*types.ComponentState)
	}

	compState := envState.Components[opts.Component]
	if compState == nil {
		compState = &types.ComponentState{
			Name:      opts.Component,
			Resources: make(map[string]*types.ResourceState),
			Status:    types.ResourceStatusReady,
			UpdatedAt: time.Now(),
		}
		envState.Components[opts.Component] = compState
	}
	if compState.Resources == nil {
		compState.Resources = make(map[string]*types.ResourceState)
	}

	compState.Resources[opts.ResourceKey] = &types.ResourceState{
		Component: opts.Component,
		Name:      resName,
		Type:      resType,
		Status:    types.ResourceStatusReady,
		Outputs:   outputs,
		IaCState:  importResult.State,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	compState.UpdatedAt = time.Now()

	envState.UpdatedAt = time.Now()
	if err := e.stateManager.SaveEnvironment(ctx, opts.Datacenter, envState); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	// Post-import verification via refresh
	if opts.Output != nil {
		fmt.Fprintf(opts.Output, "  Verifying import...\n")
	}

	refreshResult, err := plugin.Refresh(ctx, iac.RunOptions{
		ModuleSource: modulePath,
		Inputs:       moduleInputs,
		Environment:  map[string]string{},
	})
	if err == nil && refreshResult != nil {
		result.Drifts = refreshResult.Drifts
	}

	result.Success = true

	if opts.Output != nil {
		if len(result.Drifts) > 0 {
			fmt.Fprintf(opts.Output, "  %s: imported with %d drift(s) detected\n", opts.ResourceKey, len(result.Drifts))
		} else {
			fmt.Fprintf(opts.Output, "  %s: imported successfully (no drift)\n", opts.ResourceKey)
		}
	}

	return result, nil
}

// ImportComponent imports all resources for a component from existing cloud infrastructure.
func (e *Engine) ImportComponent(ctx context.Context, opts ImportComponentOptions) (*ImportComponentResult, error) {
	startTime := time.Now()
	result := &ImportComponentResult{
		Component: opts.Component,
	}

	// Validate that the component source can be resolved
	if !isLocalPath(opts.Source) {
		// Resolve from OCI to verify the artifact exists
		if _, err := e.loadComponentConfig(ctx, opts.Source); err != nil {
			return nil, fmt.Errorf("failed to load component %q: %w", opts.Source, err)
		}
	}

	// Get existing environment state to check for conflicts
	envState, err := e.stateManager.GetEnvironment(ctx, opts.Datacenter, opts.Environment)
	if err != nil {
		return nil, fmt.Errorf("environment %q not found: %w", opts.Environment, err)
	}

	// Check for existing component state
	if !opts.Force {
		if _, ok := envState.Components[opts.Component]; ok {
			return nil, fmt.Errorf("component %q already exists in environment %q (use --force to overwrite)", opts.Component, opts.Environment)
		}
	}

	if opts.Output != nil {
		fmt.Fprintf(opts.Output, "\nImporting component %q into environment %q\n", opts.Component, opts.Environment)
		fmt.Fprintf(opts.Output, "  Source: %s\n", opts.Source)
		fmt.Fprintf(opts.Output, "  Resources to import: %d\n\n", len(opts.Mapping.Resources))
	}

	// Initialize component state
	if envState.Components == nil {
		envState.Components = make(map[string]*types.ComponentState)
	}

	compState := &types.ComponentState{
		Name:      opts.Component,
		Source:    opts.Source,
		Resources: make(map[string]*types.ResourceState),
		Status:    types.ResourceStatusProvisioning,
		UpdatedAt: time.Now(),
	}
	if opts.Variables != nil {
		strVars := make(map[string]string, len(opts.Variables))
		for k, v := range opts.Variables {
			strVars[k] = fmt.Sprintf("%v", v)
		}
		compState.Variables = strVars
	}
	envState.Components[opts.Component] = compState

	// Import each resource
	allSuccess := true
	for resourceKey, mappings := range opts.Mapping.Resources {
		iacMappings := make([]iac.ImportMapping, 0, len(mappings))
		for _, m := range mappings {
			iacMappings = append(iacMappings, iac.ImportMapping{
				Address: m.Address,
				ID:      m.ID,
			})
		}

		resResult, err := e.ImportResource(ctx, ImportResourceOptions{
			Datacenter:  opts.Datacenter,
			Environment: opts.Environment,
			Component:   opts.Component,
			ResourceKey: resourceKey,
			Mappings:    iacMappings,
			Output:      opts.Output,
			Force:       true, // Force since we already checked at component level
		})
		if err != nil {
			if opts.Output != nil {
				fmt.Fprintf(opts.Output, "  [error] %s: %v\n", resourceKey, err)
			}
			allSuccess = false
			result.Resources = append(result.Resources, ImportResourceResult{
				ResourceKey: resourceKey,
				Success:     false,
			})
			continue
		}
		result.Resources = append(result.Resources, *resResult)
	}

	// Update component status
	if allSuccess {
		compState.Status = types.ResourceStatusReady
	} else {
		compState.Status = types.ResourceStatusFailed
	}
	compState.UpdatedAt = time.Now()

	envState.UpdatedAt = time.Now()
	if err := e.stateManager.SaveEnvironment(ctx, opts.Datacenter, envState); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	result.Success = allSuccess
	result.Duration = time.Since(startTime)
	return result, nil
}

// ImportEnvironment imports multiple components into an environment from existing infrastructure.
func (e *Engine) ImportEnvironment(ctx context.Context, opts ImportEnvironmentOptions) (*ImportEnvironmentResult, error) {
	startTime := time.Now()
	result := &ImportEnvironmentResult{}

	// Ensure environment exists (create if it doesn't)
	envState, err := e.stateManager.GetEnvironment(ctx, opts.Datacenter, opts.Environment)
	if err != nil {
		// Create the environment
		envState = &types.EnvironmentState{
			Name:       opts.Environment,
			Datacenter: opts.Datacenter,
			Components: make(map[string]*types.ComponentState),
			Status:     types.EnvironmentStatusProvisioning,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		if err := e.stateManager.SaveEnvironment(ctx, opts.Datacenter, envState); err != nil {
			return nil, fmt.Errorf("failed to create environment state: %w", err)
		}
	}

	if opts.Output != nil {
		fmt.Fprintf(opts.Output, "\nImporting %d component(s) into environment %q\n\n", len(opts.Mapping.Components), opts.Environment)
	}

	allSuccess := true
	for compName, compMapping := range opts.Mapping.Components {
		// Build variables
		vars := make(map[string]interface{})
		for k, v := range compMapping.Variables {
			vars[k] = v
		}

		// Build component mapping
		compMap := &importmap.ComponentMapping{
			Resources: compMapping.Resources,
		}

		compResult, err := e.ImportComponent(ctx, ImportComponentOptions{
			Datacenter:  opts.Datacenter,
			Environment: opts.Environment,
			Component:   compName,
			Source:      compMapping.Source,
			Variables:   vars,
			Mapping:     compMap,
			Output:      opts.Output,
			Force:       opts.Force,
			AutoApprove: opts.AutoApprove,
		})
		if err != nil {
			if opts.Output != nil {
				fmt.Fprintf(opts.Output, "\n  [error] Component %q: %v\n", compName, err)
			}
			allSuccess = false
			result.Components = append(result.Components, ImportComponentResult{
				Component: compName,
				Success:   false,
			})
			continue
		}
		result.Components = append(result.Components, *compResult)
		if !compResult.Success {
			allSuccess = false
		}
	}

	// Update environment status
	if allSuccess {
		envState.Status = types.EnvironmentStatusReady
	} else {
		envState.Status = types.EnvironmentStatusFailed
	}
	envState.UpdatedAt = time.Now()
	_ = e.stateManager.SaveEnvironment(ctx, opts.Datacenter, envState)

	result.Success = allSuccess
	result.Duration = time.Since(startTime)
	return result, nil
}

// ImportDatacenterModule imports existing infrastructure into a datacenter-level module's state.
func (e *Engine) ImportDatacenterModule(ctx context.Context, opts ImportDatacenterModuleOptions) (*ImportDatacenterModuleResult, error) {
	result := &ImportDatacenterModuleResult{
		Module: opts.Module,
	}

	// Load datacenter state
	dcState, err := e.stateManager.GetDatacenter(ctx, opts.Datacenter)
	if err != nil {
		return nil, fmt.Errorf("datacenter %q not found: %w", opts.Datacenter, err)
	}

	// Load datacenter configuration
	dc, err := e.loadDatacenterConfig(dcState.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to load datacenter configuration: %w", err)
	}

	// Find the module in the datacenter configuration
	var targetModule datacenter.Module
	for _, mod := range dc.Modules() {
		if mod.Name() == opts.Module {
			targetModule = mod
			break
		}
	}
	if targetModule == nil {
		return nil, fmt.Errorf("module %q not found in datacenter configuration", opts.Module)
	}

	// Check for existing module state
	if !opts.Force && dcState.Modules != nil {
		if existing, ok := dcState.Modules[opts.Module]; ok {
			if existing.Status == types.ModuleStatusReady {
				return nil, fmt.Errorf("module %q already has state (use --force to overwrite)", opts.Module)
			}
		}
	}

	// Resolve module path
	dcDir := filepath.Dir(dc.SourcePath())
	modulePath := targetModule.Build()
	if modulePath == "" {
		modulePath = targetModule.Source()
	}
	if modulePath != "" && !filepath.IsAbs(modulePath) {
		modulePath = filepath.Join(dcDir, modulePath)
	}

	// Get IaC plugin
	pluginName := targetModule.Plugin()
	if pluginName == "" {
		pluginName = "native"
	}
	plugin, err := e.iacRegistry.Get(pluginName)
	if err != nil {
		return nil, fmt.Errorf("failed to get IaC plugin %q: %w", pluginName, err)
	}

	// Build module inputs
	dcVars := make(map[string]interface{})
	for k, v := range dcState.Variables {
		dcVars[k] = v
	}
	inputs := make(map[string]interface{})
	for inputName, exprStr := range targetModule.Inputs() {
		inputs[inputName] = evaluateModuleExpression(exprStr, dcVars, nil, nil)
	}

	if opts.Output != nil {
		fmt.Fprintf(opts.Output, "Importing %d resource(s) into datacenter module %q...\n", len(opts.Mappings), opts.Module)
	}

	// Run import
	importOpts := iac.ImportOptions{
		ModuleSource: modulePath,
		Inputs:       inputs,
		Mappings:     opts.Mappings,
		Environment:  map[string]string{},
	}

	importResult, err := plugin.Import(ctx, importOpts)
	if err != nil {
		return nil, fmt.Errorf("import failed for module %q: %w", opts.Module, err)
	}

	// Extract outputs
	outputs := make(map[string]interface{})
	for name, out := range importResult.Outputs {
		outputs[name] = out.Value
	}
	result.Outputs = outputs
	result.ImportedResources = importResult.ImportedResources

	// Save module state
	if dcState.Modules == nil {
		dcState.Modules = make(map[string]*types.ModuleState)
	}

	dcState.Modules[opts.Module] = &types.ModuleState{
		Name:      opts.Module,
		Plugin:    pluginName,
		Source:    modulePath,
		Inputs:    inputs,
		Outputs:   outputs,
		IaCState:  importResult.State,
		Status:    types.ModuleStatusReady,
		UpdatedAt: time.Now(),
	}
	dcState.UpdatedAt = time.Now()

	if err := e.stateManager.SaveDatacenter(ctx, dcState); err != nil {
		return nil, fmt.Errorf("failed to save datacenter state: %w", err)
	}

	result.Success = true

	if opts.Output != nil {
		fmt.Fprintf(opts.Output, "  [success] Module %q imported (%d resource(s))\n", opts.Module, len(importResult.ImportedResources))
	}

	return result, nil
}

// ImportEnvironmentModule imports existing infrastructure into an environment-scoped module's state.
// Environment-scoped modules are defined inside the `environment {}` block of a datacenter
// configuration (outside hooks). They are per-environment shared resources like VPCs or namespaces.
func (e *Engine) ImportEnvironmentModule(ctx context.Context, opts ImportEnvironmentModuleOptions) (*ImportEnvironmentModuleResult, error) {
	result := &ImportEnvironmentModuleResult{
		Module: opts.Module,
	}

	// Load datacenter state
	dcState, err := e.stateManager.GetDatacenter(ctx, opts.Datacenter)
	if err != nil {
		return nil, fmt.Errorf("datacenter %q not found: %w", opts.Datacenter, err)
	}

	// Load datacenter configuration
	dc, err := e.loadDatacenterConfig(dcState.Version)
	if err != nil {
		return nil, fmt.Errorf("failed to load datacenter configuration: %w", err)
	}

	// Find the module in the environment block
	envBlock := dc.Environment()
	if envBlock == nil {
		return nil, fmt.Errorf("datacenter has no environment block")
	}

	var targetModule datacenter.Module
	for _, mod := range envBlock.Modules() {
		if mod.Name() == opts.Module {
			targetModule = mod
			break
		}
	}
	if targetModule == nil {
		return nil, fmt.Errorf("environment module %q not found in datacenter configuration", opts.Module)
	}

	// Get or create environment state
	envState, err := e.stateManager.GetEnvironment(ctx, opts.Datacenter, opts.Environment)
	if err != nil {
		// Create the environment if it doesn't exist
		envState = &types.EnvironmentState{
			Name:       opts.Environment,
			Datacenter: opts.Datacenter,
			Components: make(map[string]*types.ComponentState),
			Modules:    make(map[string]*types.ModuleState),
			Status:     types.EnvironmentStatusReady,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
	}

	// Check for existing module state
	if !opts.Force && envState.Modules != nil {
		if existing, ok := envState.Modules[opts.Module]; ok {
			if existing.Status == types.ModuleStatusReady {
				return nil, fmt.Errorf("environment module %q already has state in %q (use --force to overwrite)", opts.Module, opts.Environment)
			}
		}
	}

	// Resolve module path
	dcDir := filepath.Dir(dc.SourcePath())
	modulePath := targetModule.Build()
	if modulePath == "" {
		modulePath = targetModule.Source()
	}
	if modulePath != "" && !filepath.IsAbs(modulePath) {
		modulePath = filepath.Join(dcDir, modulePath)
	}

	// Get IaC plugin
	pluginName := targetModule.Plugin()
	if pluginName == "" {
		pluginName = "native"
	}
	plugin, err := e.iacRegistry.Get(pluginName)
	if err != nil {
		return nil, fmt.Errorf("failed to get IaC plugin %q: %w", pluginName, err)
	}

	// Build module inputs
	dcVars := make(map[string]interface{})
	for k, v := range dcState.Variables {
		dcVars[k] = v
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

	inputs := make(map[string]interface{})
	for inputName, exprStr := range targetModule.Inputs() {
		inputs[inputName] = evaluateModuleExpression(exprStr, dcVars, rootOutputs, map[string]string{
			"environment.name": opts.Environment,
		})
	}

	if opts.Output != nil {
		fmt.Fprintf(opts.Output, "  Importing %d resource(s) into environment module %q for %q...\n",
			len(opts.Mappings), opts.Module, opts.Environment)
	}

	// Run import
	importOpts := iac.ImportOptions{
		ModuleSource: modulePath,
		Inputs:       inputs,
		Mappings:     opts.Mappings,
		Environment:  map[string]string{},
	}

	importResult, err := plugin.Import(ctx, importOpts)
	if err != nil {
		return nil, fmt.Errorf("import failed for environment module %q: %w", opts.Module, err)
	}

	// Extract outputs
	outputs := make(map[string]interface{})
	for name, out := range importResult.Outputs {
		outputs[name] = out.Value
	}
	result.Outputs = outputs
	result.ImportedResources = importResult.ImportedResources

	// Save module state to environment
	if envState.Modules == nil {
		envState.Modules = make(map[string]*types.ModuleState)
	}

	envState.Modules[opts.Module] = &types.ModuleState{
		Name:      opts.Module,
		Plugin:    pluginName,
		Source:    modulePath,
		Inputs:    inputs,
		Outputs:   outputs,
		IaCState:  importResult.State,
		Status:    types.ModuleStatusReady,
		UpdatedAt: time.Now(),
	}
	envState.UpdatedAt = time.Now()

	if err := e.stateManager.SaveEnvironment(ctx, opts.Datacenter, envState); err != nil {
		return nil, fmt.Errorf("failed to save environment state: %w", err)
	}

	result.Success = true

	if opts.Output != nil {
		fmt.Fprintf(opts.Output, "  [success] Environment module %q imported for %q (%d resource(s))\n",
			opts.Module, opts.Environment, len(importResult.ImportedResources))
	}

	return result, nil
}

// parseResourceKey splits a type-qualified resource key (e.g., "database.main") into type and name.
func parseResourceKey(key string) (resType, resName string, err error) {
	for i, c := range key {
		if c == '.' {
			return key[:i], key[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid resource key %q: expected format type.name (e.g., database.main)", key)
}

// isLocalPath checks if a reference looks like a local filesystem path.
func isLocalPath(ref string) bool {
	return len(ref) > 0 && (ref[0] == '/' || ref[0] == '.' || ref[0] == '~')
}

// findMatchingHookForImport finds the matching datacenter hook for a resource node.
// This is a simplified version of the executor's findMatchingHook that doesn't
// require a full executor context.
func findMatchingHookForImport(dc datacenter.Datacenter, node *graph.Node, envName string, dcVars map[string]interface{}) (modulePath string, inputs map[string]interface{}, pluginName string, err error) {
	if dc == nil || dc.Environment() == nil {
		return "", nil, "", fmt.Errorf("no datacenter configuration provided")
	}

	hooks := dc.Environment().Hooks()
	if hooks == nil {
		return "", nil, "", fmt.Errorf("no hooks defined in datacenter")
	}

	// Get hooks for this node type
	var typeHooks []datacenter.Hook
	switch node.Type {
	case graph.NodeTypeDatabase:
		typeHooks = hooks.Database()
	case graph.NodeTypeBucket:
		typeHooks = hooks.Bucket()
	case graph.NodeTypeDeployment:
		typeHooks = hooks.Deployment()
	case graph.NodeTypeFunction:
		typeHooks = hooks.Function()
	case graph.NodeTypeService:
		typeHooks = hooks.Service()
	case graph.NodeTypeRoute:
		typeHooks = hooks.Route()
	case graph.NodeTypeCronjob:
		typeHooks = hooks.Cronjob()
	case graph.NodeTypeDockerBuild:
		typeHooks = hooks.DockerBuild()
	case graph.NodeTypeEncryptionKey:
		typeHooks = hooks.EncryptionKey()
	case graph.NodeTypeSMTP:
		typeHooks = hooks.SMTP()
	case graph.NodeTypeObservability:
		typeHooks = hooks.Observability()
	case graph.NodeTypePort:
		typeHooks = hooks.Port()
	default:
		return "", nil, "", fmt.Errorf("unsupported resource type: %s", node.Type)
	}

	if len(typeHooks) == 0 {
		return "", nil, "", fmt.Errorf("no hooks defined for resource type %s", node.Type)
	}

	// Find first matching hook (waterfall evaluation).
	// For import, we use the first available hook since we don't have
	// full node inputs to evaluate when conditions.
	var matchedHook datacenter.Hook
	if len(typeHooks) > 0 {
		matchedHook = typeHooks[0]
	}

	if matchedHook == nil {
		return "", nil, "", fmt.Errorf("no matching hook found for %s", node.Type)
	}

	// Check for error hooks
	if errMsg := matchedHook.Error(); errMsg != "" {
		return "", nil, "", fmt.Errorf("hook rejects this resource: %s", errMsg)
	}

	// Get the first module
	modules := matchedHook.Modules()
	if len(modules) == 0 {
		return "", nil, "", fmt.Errorf("hook has no modules defined for %s", node.Type)
	}

	module := modules[0]

	// Resolve module path
	dcDir := filepath.Dir(dc.SourcePath())
	modulePath = module.Build()
	if modulePath == "" {
		modulePath = module.Source()
	}
	if modulePath != "" && !filepath.IsAbs(modulePath) {
		modulePath = filepath.Join(dcDir, modulePath)
	}

	// Build basic inputs
	inputs = make(map[string]interface{})
	for inputName, exprStr := range module.Inputs() {
		inputs[inputName] = evaluateModuleExpression(exprStr, dcVars, nil, map[string]string{
			"environment.name": envName,
		})
	}

	return modulePath, inputs, module.Plugin(), nil
}
