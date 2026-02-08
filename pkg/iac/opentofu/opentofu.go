// Package opentofu implements an IaC plugin for OpenTofu/Terraform.
package opentofu

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
)

func init() {
	// Register both opentofu and terraform names
	iac.Register("opentofu", func() (iac.Plugin, error) {
		return NewPlugin("tofu")
	})
	iac.Register("terraform", func() (iac.Plugin, error) {
		return NewPlugin("terraform")
	})
}

// Plugin implements the IaC plugin interface for OpenTofu/Terraform.
type Plugin struct {
	// binaryPath is the path to the tofu/terraform binary
	binaryPath string
	// binaryName is "tofu" or "terraform"
	binaryName string
}

// NewPlugin creates a new OpenTofu/Terraform plugin instance.
func NewPlugin(binaryName string) (*Plugin, error) {
	// Try to find the binary
	binaryPath, err := exec.LookPath(binaryName)
	if err != nil {
		// Try alternative binary name
		if binaryName == "tofu" {
			binaryPath, err = exec.LookPath("terraform")
			if err == nil {
				binaryName = "terraform"
			}
		} else {
			binaryPath, err = exec.LookPath("tofu")
			if err == nil {
				binaryName = "tofu"
			}
		}
		if err != nil {
			return nil, fmt.Errorf("neither tofu nor terraform binary found: %w", err)
		}
	}

	return &Plugin{
		binaryPath: binaryPath,
		binaryName: binaryName,
	}, nil
}

func (p *Plugin) Name() string {
	return "opentofu"
}

// TFState represents Terraform/OpenTofu state.
type TFState struct {
	Version          int         `json:"version"`
	TerraformVersion string      `json:"terraform_version"`
	Serial           int         `json:"serial"`
	Lineage          string      `json:"lineage"`
	Outputs          TFOutputs   `json:"outputs"`
	Resources        []TFResource `json:"resources"`
}

// TFOutputs represents Terraform outputs.
type TFOutputs map[string]TFOutput

// TFOutput represents a single Terraform output.
type TFOutput struct {
	Value     interface{} `json:"value"`
	Type      interface{} `json:"type"`
	Sensitive bool        `json:"sensitive"`
}

// TFResource represents a Terraform resource.
type TFResource struct {
	Mode      string       `json:"mode"`
	Type      string       `json:"type"`
	Name      string       `json:"name"`
	Provider  string       `json:"provider"`
	Instances []TFInstance `json:"instances"`
}

// TFInstance represents a resource instance.
type TFInstance struct {
	SchemaVersion int                    `json:"schema_version"`
	Attributes    map[string]interface{} `json:"attributes"`
	Dependencies  []string               `json:"dependencies"`
}

func (p *Plugin) Apply(ctx context.Context, opts iac.RunOptions) (*iac.ApplyResult, error) {
	workDir := opts.WorkDir
	if workDir == "" {
		workDir = opts.ModuleSource
	}

	// Write tfvars file from inputs
	if err := p.writeTFVars(workDir, opts.Inputs); err != nil {
		return nil, fmt.Errorf("failed to write tfvars: %w", err)
	}

	// Initialize if needed
	if err := p.init(ctx, workDir, opts); err != nil {
		return nil, fmt.Errorf("init failed: %w", err)
	}

	// Run apply
	args := []string{
		"apply",
		"-auto-approve",
		"-json",
		"-input=false",
	}

	// Add var file if exists
	varFile := filepath.Join(workDir, "terraform.tfvars.json")
	if _, err := os.Stat(varFile); err == nil {
		args = append(args, "-var-file=terraform.tfvars.json")
	}

	output, err := p.runTF(ctx, workDir, args, opts)
	if err != nil {
		return nil, fmt.Errorf("apply failed: %w\nOutput: %s", err, output)
	}

	// Read outputs
	outputs, err := p.getOutputs(ctx, workDir, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to get outputs: %w", err)
	}

	// Read state
	stateBytes, err := p.readState(workDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read state: %w", err)
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

	// Write tfvars file from inputs
	if err := p.writeTFVars(workDir, opts.Inputs); err != nil {
		return fmt.Errorf("failed to write tfvars: %w", err)
	}

	// Initialize if needed
	if err := p.init(ctx, workDir, opts); err != nil {
		return fmt.Errorf("init failed: %w", err)
	}

	// Run destroy
	args := []string{
		"destroy",
		"-auto-approve",
		"-input=false",
	}

	// Add var file if exists
	varFile := filepath.Join(workDir, "terraform.tfvars.json")
	if _, err := os.Stat(varFile); err == nil {
		args = append(args, "-var-file=terraform.tfvars.json")
	}

	output, err := p.runTF(ctx, workDir, args, opts)
	if err != nil {
		return fmt.Errorf("destroy failed: %w\nOutput: %s", err, output)
	}

	return nil
}

func (p *Plugin) Preview(ctx context.Context, opts iac.RunOptions) (*iac.PreviewResult, error) {
	workDir := opts.WorkDir
	if workDir == "" {
		workDir = opts.ModuleSource
	}

	// Write tfvars file from inputs
	if err := p.writeTFVars(workDir, opts.Inputs); err != nil {
		return nil, fmt.Errorf("failed to write tfvars: %w", err)
	}

	// Initialize if needed
	if err := p.init(ctx, workDir, opts); err != nil {
		return nil, fmt.Errorf("init failed: %w", err)
	}

	// Run plan with JSON output
	args := []string{
		"plan",
		"-json",
		"-input=false",
	}

	// Add var file if exists
	varFile := filepath.Join(workDir, "terraform.tfvars.json")
	if _, err := os.Stat(varFile); err == nil {
		args = append(args, "-var-file=terraform.tfvars.json")
	}

	output, err := p.runTF(ctx, workDir, args, opts)
	if err != nil {
		return nil, fmt.Errorf("plan failed: %w", err)
	}

	return p.parsePlanOutput(output)
}

func (p *Plugin) Refresh(ctx context.Context, opts iac.RunOptions) (*iac.RefreshResult, error) {
	workDir := opts.WorkDir
	if workDir == "" {
		workDir = opts.ModuleSource
	}

	// Initialize if needed
	if err := p.init(ctx, workDir, opts); err != nil {
		return nil, fmt.Errorf("init failed: %w", err)
	}

	// Run refresh
	args := []string{
		"refresh",
		"-json",
		"-input=false",
	}

	_, err := p.runTF(ctx, workDir, args, opts)
	if err != nil {
		return nil, fmt.Errorf("refresh failed: %w", err)
	}

	// Read state
	stateBytes, err := p.readState(workDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read state: %w", err)
	}

	return &iac.RefreshResult{
		State:  stateBytes,
		Drifts: []iac.ResourceDrift{}, // Would parse from refresh output
	}, nil
}

func (p *Plugin) init(ctx context.Context, workDir string, opts iac.RunOptions) error {
	// Check if already initialized
	if _, err := os.Stat(filepath.Join(workDir, ".terraform")); err == nil {
		return nil
	}

	args := []string{"init", "-input=false"}
	_, err := p.runTF(ctx, workDir, args, opts)
	return err
}

func (p *Plugin) writeTFVars(workDir string, inputs map[string]interface{}) error {
	if len(inputs) == 0 {
		return nil
	}

	varFile := filepath.Join(workDir, "terraform.tfvars.json")
	data, err := json.MarshalIndent(inputs, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(varFile, data, 0644)
}

func (p *Plugin) getOutputs(ctx context.Context, workDir string, opts iac.RunOptions) (map[string]iac.OutputValue, error) {
	args := []string{"output", "-json"}

	output, err := p.runTF(ctx, workDir, args, opts)
	if err != nil {
		// No outputs is fine
		return map[string]iac.OutputValue{}, nil
	}

	var tfOutputs TFOutputs
	if err := json.Unmarshal([]byte(output), &tfOutputs); err != nil {
		return nil, err
	}

	outputs := make(map[string]iac.OutputValue)
	for k, v := range tfOutputs {
		outputs[k] = iac.OutputValue{
			Value:     v.Value,
			Sensitive: v.Sensitive,
		}
	}

	return outputs, nil
}

func (p *Plugin) readState(workDir string) ([]byte, error) {
	stateFile := filepath.Join(workDir, "terraform.tfstate")
	return os.ReadFile(stateFile)
}

func (p *Plugin) parsePlanOutput(output string) (*iac.PreviewResult, error) {
	result := &iac.PreviewResult{
		Changes: []iac.ResourceChange{},
	}

	// Parse line-delimited JSON output
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		var msg struct {
			Type    string `json:"@level"`
			Message string `json:"@message"`
			Change  *struct {
				Resource struct {
					Addr string `json:"addr"`
					Type string `json:"resource_type"`
					Name string `json:"resource_name"`
				} `json:"resource"`
				Action []string `json:"action"`
			} `json:"change"`
		}

		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		if msg.Change != nil {
			action := mapTFAction(msg.Change.Action)
			if action == iac.ActionNoop {
				continue
			}

			result.Changes = append(result.Changes, iac.ResourceChange{
				ResourceID:   msg.Change.Resource.Addr,
				ResourceType: msg.Change.Resource.Type,
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
	}

	return result, nil
}

func mapTFAction(actions []string) iac.ChangeAction {
	if len(actions) == 0 {
		return iac.ActionNoop
	}

	if contains(actions, "create") && contains(actions, "delete") {
		return iac.ActionReplace
	}
	if contains(actions, "create") {
		return iac.ActionCreate
	}
	if contains(actions, "update") {
		return iac.ActionUpdate
	}
	if contains(actions, "delete") {
		return iac.ActionDelete
	}
	return iac.ActionNoop
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (p *Plugin) runTF(ctx context.Context, workDir string, args []string, opts iac.RunOptions) (string, error) {
	cmd := exec.CommandContext(ctx, p.binaryPath, args...)
	cmd.Dir = workDir

	// Set up environment
	cmd.Env = os.Environ()
	for k, v := range opts.Environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Disable interactive prompts
	cmd.Env = append(cmd.Env, "TF_INPUT=0")
	cmd.Env = append(cmd.Env, "TF_IN_AUTOMATION=1")

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

// Ensure we implement the Plugin interface
var _ iac.Plugin = (*Plugin)(nil)
