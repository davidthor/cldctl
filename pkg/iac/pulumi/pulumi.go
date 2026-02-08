// Package pulumi implements an IaC plugin for Pulumi.
package pulumi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/davidthor/cldctl/pkg/iac"
	"gopkg.in/yaml.v3"
)

func init() {
	iac.Register("pulumi", func() (iac.Plugin, error) {
		return NewPlugin()
	})
}

// Plugin implements the IaC plugin interface for Pulumi.
type Plugin struct {
	// pulumiPath is the path to the pulumi binary
	pulumiPath string
}

// NewPlugin creates a new Pulumi plugin instance.
func NewPlugin() (*Plugin, error) {
	// Find pulumi binary
	pulumiPath, err := exec.LookPath("pulumi")
	if err != nil {
		return nil, fmt.Errorf("pulumi binary not found: %w", err)
	}

	return &Plugin{
		pulumiPath: pulumiPath,
	}, nil
}

func (p *Plugin) Name() string {
	return "pulumi"
}

// State represents Pulumi stack state.
type State struct {
	StackName   string                 `json:"stack_name"`
	ProjectName string                 `json:"project_name"`
	Outputs     map[string]interface{} `json:"outputs"`
	Resources   []ResourceState        `json:"resources"`
}

// ResourceState represents a Pulumi resource state.
type ResourceState struct {
	URN        string                 `json:"urn"`
	Type       string                 `json:"type"`
	ID         string                 `json:"id"`
	Properties map[string]interface{} `json:"properties"`
}

func (p *Plugin) Apply(ctx context.Context, opts iac.RunOptions) (*iac.ApplyResult, error) {
	workDir := opts.WorkDir
	if workDir == "" {
		workDir = opts.ModuleSource
	}

	// Ensure we have a stack
	stackName := getStackName(opts.Environment)

	// Write Pulumi config from inputs
	if err := p.writeConfig(workDir, stackName, opts.Inputs); err != nil {
		return nil, fmt.Errorf("failed to write config: %w", err)
	}

	// Initialize stack if needed
	if err := p.ensureStack(ctx, workDir, stackName, opts); err != nil {
		return nil, fmt.Errorf("failed to ensure stack: %w", err)
	}

	// Run pulumi up
	args := []string{
		"up",
		"--yes",
		"--stack", stackName,
		"--json",
		"--non-interactive",
	}

	output, err := p.runPulumi(ctx, workDir, args, opts)
	if err != nil {
		return nil, fmt.Errorf("pulumi up failed: %w\nOutput: %s", err, output)
	}

	// Get outputs
	outputs, err := p.getOutputs(ctx, workDir, stackName, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get outputs: %w", err)
	}

	// Export state
	stateBytes, err := p.exportState(ctx, workDir, stackName, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to export state: %w", err)
	}

	return &iac.ApplyResult{
		Outputs: outputs,
		State:   stateBytes,
	}, nil
}

func (p *Plugin) Destroy(ctx context.Context, opts iac.RunOptions) error {
	workDir := opts.WorkDir
	if workDir == "" {
		workDir = opts.ModuleSource
	}

	stackName := getStackName(opts.Environment)

	// Run pulumi destroy
	args := []string{
		"destroy",
		"--yes",
		"--stack", stackName,
		"--non-interactive",
	}

	output, err := p.runPulumi(ctx, workDir, args, opts)
	if err != nil {
		return fmt.Errorf("pulumi destroy failed: %w\nOutput: %s", err, output)
	}

	// Remove stack
	removeArgs := []string{
		"stack", "rm", stackName,
		"--yes",
		"--force",
	}
	_, _ = p.runPulumi(ctx, workDir, removeArgs, opts) // Ignore errors on cleanup

	return nil
}

func (p *Plugin) Preview(ctx context.Context, opts iac.RunOptions) (*iac.PreviewResult, error) {
	workDir := opts.WorkDir
	if workDir == "" {
		workDir = opts.ModuleSource
	}

	stackName := getStackName(opts.Environment)

	// Write config
	if err := p.writeConfig(workDir, stackName, opts.Inputs); err != nil {
		return nil, fmt.Errorf("failed to write config: %w", err)
	}

	// Ensure stack exists
	if err := p.ensureStack(ctx, workDir, stackName, opts); err != nil {
		return nil, fmt.Errorf("failed to ensure stack: %w", err)
	}

	// Run pulumi preview
	args := []string{
		"preview",
		"--stack", stackName,
		"--json",
		"--non-interactive",
	}

	output, err := p.runPulumi(ctx, workDir, args, opts)
	if err != nil {
		return nil, fmt.Errorf("pulumi preview failed: %w", err)
	}

	// Parse preview output
	return p.parsePreviewOutput(output)
}

func (p *Plugin) Refresh(ctx context.Context, opts iac.RunOptions) (*iac.RefreshResult, error) {
	workDir := opts.WorkDir
	if workDir == "" {
		workDir = opts.ModuleSource
	}

	stackName := getStackName(opts.Environment)

	// Run pulumi refresh
	args := []string{
		"refresh",
		"--yes",
		"--stack", stackName,
		"--json",
		"--non-interactive",
	}

	output, err := p.runPulumi(ctx, workDir, args, opts)
	if err != nil {
		return nil, fmt.Errorf("pulumi refresh failed: %w\nOutput: %s", err, output)
	}

	// Export state after refresh
	stateBytes, err := p.exportState(ctx, workDir, stackName, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to export state: %w", err)
	}

	return &iac.RefreshResult{
		State:  stateBytes,
		Drifts: []iac.ResourceDrift{}, // Would parse from refresh output
	}, nil
}

func (p *Plugin) ensureStack(ctx context.Context, workDir, stackName string, opts iac.RunOptions) error {
	// Check if stack exists
	listArgs := []string{"stack", "ls", "--json"}
	output, err := p.runPulumi(ctx, workDir, listArgs, opts)
	if err != nil {
		// Stack list failed, try to init
		initArgs := []string{"stack", "init", stackName, "--non-interactive"}
		_, err := p.runPulumi(ctx, workDir, initArgs, opts)
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			return err
		}
		return nil
	}

	// Parse stack list
	var stacks []struct {
		Name    string `json:"name"`
		Current bool   `json:"current"`
	}
	if err := json.Unmarshal([]byte(output), &stacks); err == nil {
		for _, s := range stacks {
			if s.Name == stackName || strings.HasSuffix(s.Name, "/"+stackName) {
				// Stack exists, select it
				selectArgs := []string{"stack", "select", stackName}
				_, _ = p.runPulumi(ctx, workDir, selectArgs, opts)
				return nil
			}
		}
	}

	// Stack doesn't exist, create it
	initArgs := []string{"stack", "init", stackName, "--non-interactive"}
	_, err = p.runPulumi(ctx, workDir, initArgs, opts)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	return nil
}

func (p *Plugin) writeConfig(workDir, stackName string, inputs map[string]interface{}) error {
	if len(inputs) == 0 {
		return nil
	}

	// Create Pulumi.<stack>.yaml config file
	configPath := filepath.Join(workDir, fmt.Sprintf("Pulumi.%s.yaml", stackName))

	config := map[string]interface{}{
		"config": inputs,
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

func (p *Plugin) getOutputs(ctx context.Context, workDir, stackName string, opts iac.RunOptions) (map[string]iac.OutputValue, error) {
	args := []string{
		"stack", "output",
		"--stack", stackName,
		"--json",
	}

	output, err := p.runPulumi(ctx, workDir, args, opts)
	if err != nil {
		return nil, err
	}

	var rawOutputs map[string]interface{}
	if err := json.Unmarshal([]byte(output), &rawOutputs); err != nil {
		return nil, err
	}

	outputs := make(map[string]iac.OutputValue)
	for k, v := range rawOutputs {
		outputs[k] = iac.OutputValue{
			Value:     v,
			Sensitive: false, // Would need to parse from stack config
		}
	}

	return outputs, nil
}

func (p *Plugin) exportState(ctx context.Context, workDir, stackName string, opts iac.RunOptions) ([]byte, error) {
	args := []string{
		"stack", "export",
		"--stack", stackName,
	}

	output, err := p.runPulumi(ctx, workDir, args, opts)
	if err != nil {
		return nil, err
	}

	return []byte(output), nil
}

func (p *Plugin) parsePreviewOutput(output string) (*iac.PreviewResult, error) {
	// Parse Pulumi's JSON preview output
	var previewData struct {
		Steps []struct {
			Op       string `json:"op"`
			URN      string `json:"urn"`
			Provider string `json:"provider"`
			Type     string `json:"type"`
		} `json:"steps"`
		ChangeSummary map[string]int `json:"changeSummary"`
	}

	if err := json.Unmarshal([]byte(output), &previewData); err != nil {
		// Return empty result if parsing fails
		return &iac.PreviewResult{}, nil
	}

	result := &iac.PreviewResult{
		Changes: make([]iac.ResourceChange, 0, len(previewData.Steps)),
	}

	for _, step := range previewData.Steps {
		action := mapPulumiAction(step.Op)
		if action == iac.ActionNoop {
			continue
		}

		result.Changes = append(result.Changes, iac.ResourceChange{
			ResourceID:   step.URN,
			ResourceType: step.Type,
			Action:       action,
		})

		switch action {
		case iac.ActionCreate:
			result.Summary.Create++
		case iac.ActionUpdate:
			result.Summary.Update++
		case iac.ActionDelete:
			result.Summary.Delete++
		case iac.ActionReplace:
			result.Summary.Replace++
		}
	}

	return result, nil
}

func mapPulumiAction(op string) iac.ChangeAction {
	switch op {
	case "create":
		return iac.ActionCreate
	case "update":
		return iac.ActionUpdate
	case "delete":
		return iac.ActionDelete
	case "replace":
		return iac.ActionReplace
	case "same":
		return iac.ActionNoop
	default:
		return iac.ActionNoop
	}
}

func (p *Plugin) runPulumi(ctx context.Context, workDir string, args []string, opts iac.RunOptions) (string, error) {
	cmd := exec.CommandContext(ctx, p.pulumiPath, args...)
	cmd.Dir = workDir

	// Set up environment
	cmd.Env = os.Environ()
	for k, v := range opts.Environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Use local backend by default if no backend is configured
	if !hasEnv(cmd.Env, "PULUMI_BACKEND_URL") {
		cmd.Env = append(cmd.Env, "PULUMI_BACKEND_URL=file://~/.pulumi")
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Also stream to provided writers if available
	if opts.Stdout != nil {
		cmd.Stdout = io.MultiWriter(&stdout, opts.Stdout)
	}
	if opts.Stderr != nil {
		cmd.Stderr = io.MultiWriter(&stderr, opts.Stderr)
	}

	err := cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("%w: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

func getStackName(env map[string]string) string {
	if stack := env["PULUMI_STACK"]; stack != "" {
		return stack
	}
	if env := env["ENVIRONMENT"]; env != "" {
		return env
	}
	return "dev"
}

func hasEnv(env []string, key string) bool {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

// Ensure we implement the Plugin interface
var _ iac.Plugin = (*Plugin)(nil)
