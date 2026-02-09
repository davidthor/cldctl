package container

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/davidthor/cldctl/pkg/iac"
)

func init() {
	// Register container-based plugins
	iac.Register("container", func() (iac.Plugin, error) {
		return NewPlugin()
	})
	iac.Register("container-pulumi", func() (iac.Plugin, error) {
		return NewPluginWithType(ModuleTypePulumi)
	})
	iac.Register("container-opentofu", func() (iac.Plugin, error) {
		return NewPluginWithType(ModuleTypeOpenTofu)
	})
}

// Plugin implements the IaC plugin interface using containerized modules.
type Plugin struct {
	executor   *Executor
	moduleType ModuleType
}

// NewPlugin creates a new container-based IaC plugin.
func NewPlugin() (*Plugin, error) {
	executor, err := NewExecutor()
	if err != nil {
		return nil, err
	}
	return &Plugin{executor: executor}, nil
}

// NewPluginWithType creates a plugin for a specific module type.
func NewPluginWithType(moduleType ModuleType) (*Plugin, error) {
	executor, err := NewExecutor()
	if err != nil {
		return nil, err
	}
	return &Plugin{
		executor:   executor,
		moduleType: moduleType,
	}, nil
}

// Name returns the plugin name.
func (p *Plugin) Name() string {
	if p.moduleType != "" {
		return fmt.Sprintf("container-%s", p.moduleType)
	}
	return "container"
}

// Apply executes a module to create/update resources.
func (p *Plugin) Apply(ctx context.Context, opts iac.RunOptions) (*iac.ApplyResult, error) {
	response, err := p.executeModule(ctx, "apply", opts)
	if err != nil {
		return nil, err
	}

	if !response.Success {
		return nil, fmt.Errorf("apply failed: %s", response.Error)
	}

	// Convert response to ApplyResult
	result := &iac.ApplyResult{
		Outputs: make(map[string]iac.OutputValue),
	}

	for name, output := range response.Outputs {
		result.Outputs[name] = iac.OutputValue{
			Value:     output.Value,
			Sensitive: output.Sensitive,
		}
	}

	return result, nil
}

// Preview shows what changes would be made.
func (p *Plugin) Preview(ctx context.Context, opts iac.RunOptions) (*iac.PreviewResult, error) {
	response, err := p.executeModule(ctx, "preview", opts)
	if err != nil {
		return nil, err
	}

	if !response.Success {
		return nil, fmt.Errorf("preview failed: %s", response.Error)
	}

	// Convert response to PreviewResult
	result := &iac.PreviewResult{
		Changes: make([]iac.ResourceChange, 0, len(response.Changes)),
	}

	for _, change := range response.Changes {
		result.Changes = append(result.Changes, iac.ResourceChange{
			ResourceID: change.Resource,
			Action:     iac.ChangeAction(change.Action),
			Before:     change.Before,
			After:      change.After,
		})
	}

	return result, nil
}

// Destroy removes resources.
func (p *Plugin) Destroy(ctx context.Context, opts iac.RunOptions) error {
	response, err := p.executeModule(ctx, "destroy", opts)
	if err != nil {
		return err
	}

	if !response.Success {
		return fmt.Errorf("destroy failed: %s", response.Error)
	}

	return nil
}

// Refresh reads the current state.
func (p *Plugin) Refresh(ctx context.Context, opts iac.RunOptions) (*iac.RefreshResult, error) {
	response, err := p.executeModule(ctx, "refresh", opts)
	if err != nil {
		return nil, err
	}

	if !response.Success {
		return nil, fmt.Errorf("refresh failed: %s", response.Error)
	}

	// Parse drift from response changes
	// During refresh, any changes detected indicate drift between state and actual infrastructure
	drifts := parseDriftsFromChanges(response.Changes)

	return &iac.RefreshResult{
		State:  nil, // State managed by container
		Drifts: drifts,
	}, nil
}

// parseDriftsFromChanges converts module response changes to ResourceDrift objects.
// During a refresh, changes indicate drift between expected state and actual infrastructure.
func parseDriftsFromChanges(changes []ResourceChange) []iac.ResourceDrift {
	var drifts []iac.ResourceDrift

	for _, change := range changes {
		// Skip no-op changes - they indicate no drift
		if change.Action == "no-op" || change.Action == "" {
			continue
		}

		// Extract resource type from resource ID if possible
		// Resource IDs are typically in format "type.name" or similar
		resourceType := ""
		if parts := strings.SplitN(change.Resource, ".", 2); len(parts) > 1 {
			resourceType = parts[0]
		}

		// Convert before/after differences to property diffs
		diffs := computePropertyDiffs(change.Before, change.After)

		drifts = append(drifts, iac.ResourceDrift{
			ResourceID:   change.Resource,
			ResourceType: resourceType,
			Diffs:        diffs,
		})
	}

	return drifts
}

// computePropertyDiffs computes property-level differences between before and after states.
func computePropertyDiffs(before, after map[string]interface{}) []iac.PropertyDiff {
	var diffs []iac.PropertyDiff

	// Track all keys from both maps
	allKeys := make(map[string]bool)
	for k := range before {
		allKeys[k] = true
	}
	for k := range after {
		allKeys[k] = true
	}

	// Compare each property
	for key := range allKeys {
		oldVal, hasOld := before[key]
		newVal, hasNew := after[key]

		// Skip if values are equal
		if hasOld && hasNew && fmt.Sprintf("%v", oldVal) == fmt.Sprintf("%v", newVal) {
			continue
		}

		// Check if property might be sensitive (common patterns)
		sensitive := isSensitiveKey(key)

		diffs = append(diffs, iac.PropertyDiff{
			Path:      key,
			OldValue:  oldVal,
			NewValue:  newVal,
			Sensitive: sensitive,
		})
	}

	return diffs
}

// isSensitiveKey checks if a key name suggests sensitive data.
func isSensitiveKey(key string) bool {
	sensitivePatterns := []string{
		"password", "secret", "key", "token", "credential",
		"private", "auth", "api_key", "apikey", "access_key",
	}
	keyLower := strings.ToLower(key)
	for _, pattern := range sensitivePatterns {
		if strings.Contains(keyLower, pattern) {
			return true
		}
	}
	return false
}

// Import delegates to the containerized module's import entrypoint.
func (p *Plugin) Import(ctx context.Context, opts iac.ImportOptions) (*iac.ImportResult, error) {
	// Build the request with import action and mappings
	mappingInputs := make([]map[string]string, 0, len(opts.Mappings))
	for _, m := range opts.Mappings {
		mappingInputs = append(mappingInputs, map[string]string{
			"address": m.Address,
			"id":      m.ID,
		})
	}

	inputs := make(map[string]interface{})
	for k, v := range opts.Inputs {
		inputs[k] = v
	}
	inputs["__import_mappings"] = mappingInputs

	runOpts := iac.RunOptions{
		ModuleSource: opts.ModuleSource,
		ModulePath:   opts.ModulePath,
		Inputs:       inputs,
		WorkDir:      opts.WorkDir,
		Environment:  opts.Environment,
		Stdout:       opts.Stdout,
		Stderr:       opts.Stderr,
	}

	response, err := p.executeModule(ctx, "import", runOpts)
	if err != nil {
		return nil, err
	}

	if !response.Success {
		return nil, fmt.Errorf("import failed: %s", response.Error)
	}

	result := &iac.ImportResult{
		Outputs: make(map[string]iac.OutputValue),
	}

	for name, output := range response.Outputs {
		result.Outputs[name] = iac.OutputValue{
			Value:     output.Value,
			Sensitive: output.Sensitive,
		}
	}

	for _, m := range opts.Mappings {
		result.ImportedResources = append(result.ImportedResources, m.Address)
	}

	return result, nil
}

// executeModule runs a containerized module.
func (p *Plugin) executeModule(ctx context.Context, action string, opts iac.RunOptions) (*ModuleResponse, error) {
	// Determine the container image
	image := opts.ModuleSource
	if !isContainerImage(image) {
		return nil, fmt.Errorf("module source must be a container image reference: %s", image)
	}

	// Create work directory for this execution
	workDir, err := os.MkdirTemp("", "cldctl-module-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create work dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	// Build the request
	request := &ModuleRequest{
		Action:      action,
		Inputs:      opts.Inputs,
		Environment: opts.Environment,
		StackName:   generateStackName(opts),
	}

	// State is passed via StateReader if available
	// The container handles state internally via its backend configuration

	// Execute the module
	response, err := p.executor.Execute(ctx, ExecuteOptions{
		Image:       image,
		Request:     request,
		WorkDir:     workDir,
		Credentials: extractCredentials(opts.Environment),
		Stdout:      opts.Stdout,
		Stderr:      opts.Stderr,
	})

	if err != nil {
		return nil, err
	}

	return response, nil
}

// isContainerImage checks if a string looks like a container image reference.
func isContainerImage(ref string) bool {
	// Check for common registry patterns
	if strings.Contains(ref, "/") {
		// Has path component, likely a registry reference
		return true
	}
	if strings.Contains(ref, ":") && !strings.HasPrefix(ref, "/") && !strings.HasPrefix(ref, ".") {
		// Has tag, likely a registry reference
		return true
	}
	// Check for well-known registries
	registries := []string{"docker.io", "ghcr.io", "gcr.io", "registry.", "localhost:"}
	for _, reg := range registries {
		if strings.HasPrefix(ref, reg) {
			return true
		}
	}
	return false
}

// generateStackName creates a unique stack name for state isolation.
func generateStackName(opts iac.RunOptions) string {
	parts := []string{}

	if env, ok := opts.Environment["CLDCTL_ENVIRONMENT"]; ok && env != "" {
		parts = append(parts, env)
	}

	if comp, ok := opts.Environment["CLDCTL_COMPONENT"]; ok && comp != "" {
		parts = append(parts, comp)
	}

	// Extract resource name from module path if available
	if opts.ModulePath != "" {
		base := filepath.Base(opts.ModulePath)
		parts = append(parts, base)
	}

	if len(parts) == 0 {
		return "default"
	}

	return strings.Join(parts, "-")
}

// extractCredentials extracts cloud provider credentials from environment.
func extractCredentials(env map[string]string) map[string]string {
	creds := make(map[string]string)

	// AWS credentials
	awsKeys := []string{
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
		"AWS_REGION",
		"AWS_DEFAULT_REGION",
		"AWS_PROFILE",
	}
	for _, key := range awsKeys {
		if val, ok := env[key]; ok {
			creds[key] = val
		} else if val := os.Getenv(key); val != "" {
			creds[key] = val
		}
	}

	// GCP credentials
	gcpKeys := []string{
		"GOOGLE_APPLICATION_CREDENTIALS",
		"GOOGLE_PROJECT",
		"GOOGLE_REGION",
		"GOOGLE_ZONE",
		"CLOUDSDK_CORE_PROJECT",
	}
	for _, key := range gcpKeys {
		if val, ok := env[key]; ok {
			creds[key] = val
		} else if val := os.Getenv(key); val != "" {
			creds[key] = val
		}
	}

	// Azure credentials
	azureKeys := []string{
		"AZURE_SUBSCRIPTION_ID",
		"AZURE_TENANT_ID",
		"AZURE_CLIENT_ID",
		"AZURE_CLIENT_SECRET",
		"ARM_SUBSCRIPTION_ID",
		"ARM_TENANT_ID",
		"ARM_CLIENT_ID",
		"ARM_CLIENT_SECRET",
	}
	for _, key := range azureKeys {
		if val, ok := env[key]; ok {
			creds[key] = val
		} else if val := os.Getenv(key); val != "" {
			creds[key] = val
		}
	}

	// Kubernetes credentials
	k8sKeys := []string{
		"KUBECONFIG",
	}
	for _, key := range k8sKeys {
		if val, ok := env[key]; ok {
			creds[key] = val
		} else if val := os.Getenv(key); val != "" {
			creds[key] = val
		}
	}

	return creds
}
