// Package main implements the arcctl module entrypoint.
// This program runs inside containerized IaC modules and translates
// between arcctl's JSON interface and the IaC tool's native interface.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ModuleRequest represents the input from arcctl.
type ModuleRequest struct {
	Action      string                 `json:"action"`
	Inputs      map[string]interface{} `json:"inputs"`
	State       map[string]interface{} `json:"state,omitempty"`
	Environment map[string]string      `json:"environment,omitempty"`
	StackName   string                 `json:"stack_name,omitempty"`
	Backend     *BackendConfig         `json:"backend,omitempty"`
}

// BackendConfig for state storage.
type BackendConfig struct {
	Type   string            `json:"type"`
	Config map[string]string `json:"config"`
}

// ModuleResponse represents the output to arcctl.
type ModuleResponse struct {
	Success bool                   `json:"success"`
	Action  string                 `json:"action"`
	Outputs map[string]OutputValue `json:"outputs,omitempty"`
	State   map[string]interface{} `json:"state,omitempty"`
	Changes []ResourceChange       `json:"changes,omitempty"`
	Error   string                 `json:"error,omitempty"`
	Logs    string                 `json:"logs,omitempty"`
}

// OutputValue represents a module output.
type OutputValue struct {
	Value     interface{} `json:"value"`
	Sensitive bool        `json:"sensitive,omitempty"`
}

// ResourceChange describes a change.
type ResourceChange struct {
	Resource string                 `json:"resource"`
	Action   string                 `json:"action"`
	Before   map[string]interface{} `json:"before,omitempty"`
	After    map[string]interface{} `json:"after,omitempty"`
}

func main() {
	inputFile := flag.String("input", "/workspace/input.json", "Input JSON file path")
	outputFile := flag.String("output", "/workspace/output.json", "Output JSON file path")
	flag.Parse()

	// Read request
	data, err := os.ReadFile(*inputFile)
	if err != nil {
		writeError(*outputFile, "", fmt.Sprintf("failed to read input: %v", err))
		os.Exit(1)
	}

	var request ModuleRequest
	if err := json.Unmarshal(data, &request); err != nil {
		writeError(*outputFile, "", fmt.Sprintf("failed to parse input: %v", err))
		os.Exit(1)
	}

	// Set environment variables
	for k, v := range request.Environment {
		os.Setenv(k, v)
	}

	// Detect which IaC tool to use
	tool := detectTool()

	var response *ModuleResponse
	switch tool {
	case "pulumi":
		response, err = executePulumi(&request)
	case "tofu":
		response, err = executeOpenTofu(&request)
	default:
		writeError(*outputFile, request.Action, fmt.Sprintf("unknown tool: %s", tool))
		os.Exit(1)
	}

	if err != nil {
		writeError(*outputFile, request.Action, err.Error())
		os.Exit(1)
	}

	// Write response
	responseData, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		writeError(*outputFile, request.Action, fmt.Sprintf("failed to marshal response: %v", err))
		os.Exit(1)
	}

	if err := os.WriteFile(*outputFile, responseData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write output: %v\n", err)
		os.Exit(1)
	}

	if !response.Success {
		os.Exit(1)
	}
}

func writeError(outputFile, action, errMsg string) {
	response := ModuleResponse{
		Success: false,
		Action:  action,
		Error:   errMsg,
	}
	data, _ := json.MarshalIndent(response, "", "  ")
	_ = os.WriteFile(outputFile, data, 0644)
}

func detectTool() string {
	// Check for Pulumi
	if _, err := os.Stat("/app/Pulumi.yaml"); err == nil {
		return "pulumi"
	}

	// Check for OpenTofu/Terraform
	entries, _ := os.ReadDir("/app")
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tf") {
			return "tofu"
		}
	}

	return "unknown"
}

func executePulumi(request *ModuleRequest) (*ModuleResponse, error) {
	// Write inputs as Pulumi config
	stackName := request.StackName
	if stackName == "" {
		stackName = "default"
	}

	// Create or select stack
	cmd := exec.Command("pulumi", "stack", "select", "--create", stackName)
	cmd.Dir = "/app"
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to select stack: %s", string(out))
	}

	// Set config from inputs
	if err := setPulumiConfig("/app", request.Inputs, execCommand); err != nil {
		return nil, err
	}

	var logs bytes.Buffer
	var response *ModuleResponse

	switch request.Action {
	case "preview":
		cmd := exec.Command("pulumi", "preview", "--json")
		cmd.Dir = "/app"
		cmd.Env = os.Environ()
		cmd.Stdout = &logs
		cmd.Stderr = &logs

		if err := cmd.Run(); err != nil {
			return &ModuleResponse{
				Success: false,
				Action:  request.Action,
				Error:   fmt.Sprintf("preview failed: %v", err),
				Logs:    logs.String(),
			}, nil
		}

		// Parse preview output for changes
		response = &ModuleResponse{
			Success: true,
			Action:  request.Action,
			Changes: parsePreviewOutput(logs.String()),
			Logs:    logs.String(),
		}

	case "apply":
		cmd := exec.Command("pulumi", "up", "--yes", "--json")
		cmd.Dir = "/app"
		cmd.Env = os.Environ()
		cmd.Stdout = &logs
		cmd.Stderr = &logs

		if err := cmd.Run(); err != nil {
			return &ModuleResponse{
				Success: false,
				Action:  request.Action,
				Error:   fmt.Sprintf("apply failed: %v", err),
				Logs:    logs.String(),
			}, nil
		}

		// Get outputs
		outputs, err := getPulumiOutputs()
		if err != nil {
			return &ModuleResponse{
				Success: false,
				Action:  request.Action,
				Error:   fmt.Sprintf("failed to get outputs: %v", err),
				Logs:    logs.String(),
			}, nil
		}

		response = &ModuleResponse{
			Success: true,
			Action:  request.Action,
			Outputs: outputs,
			Logs:    logs.String(),
		}

	case "destroy":
		cmd := exec.Command("pulumi", "destroy", "--yes", "--json")
		cmd.Dir = "/app"
		cmd.Env = os.Environ()
		cmd.Stdout = &logs
		cmd.Stderr = &logs

		if err := cmd.Run(); err != nil {
			return &ModuleResponse{
				Success: false,
				Action:  request.Action,
				Error:   fmt.Sprintf("destroy failed: %v", err),
				Logs:    logs.String(),
			}, nil
		}

		response = &ModuleResponse{
			Success: true,
			Action:  request.Action,
			Logs:    logs.String(),
		}

	default:
		return nil, fmt.Errorf("unsupported action: %s", request.Action)
	}

	return response, nil
}

// commandRunner executes an external command and returns its combined output.
// This is an abstraction to allow testing without real exec calls.
type commandRunner func(dir string, name string, args ...string) ([]byte, error)

// execCommand is the default command runner using os/exec.
func execCommand(dir string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	return cmd.CombinedOutput()
}

// setPulumiConfig sets Pulumi config values from inputs using the provided command runner.
func setPulumiConfig(dir string, inputs map[string]interface{}, runner commandRunner) error {
	for key, value := range inputs {
		valueStr := fmt.Sprintf("%v", value)
		out, err := runner(dir, "pulumi", "config", "set", key, valueStr)
		if err != nil {
			return fmt.Errorf("failed to set pulumi config %q: %s", key, string(out))
		}
	}
	return nil
}

func getPulumiOutputs() (map[string]OutputValue, error) {
	cmd := exec.Command("pulumi", "stack", "output", "--json")
	cmd.Dir = "/app"
	cmd.Env = os.Environ()

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var rawOutputs map[string]interface{}
	if err := json.Unmarshal(output, &rawOutputs); err != nil {
		return nil, err
	}

	outputs := make(map[string]OutputValue)
	for k, v := range rawOutputs {
		outputs[k] = OutputValue{Value: v}
	}

	return outputs, nil
}

func parsePreviewOutput(output string) []ResourceChange {
	// Simplified parsing - in production would parse Pulumi's JSON output
	return nil
}

func executeOpenTofu(request *ModuleRequest) (*ModuleResponse, error) {
	// Write inputs as tfvars
	tfvarsPath := "/app/terraform.tfvars.json"
	tfvarsData, err := json.MarshalIndent(request.Inputs, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal inputs: %w", err)
	}
	if err := os.WriteFile(tfvarsPath, tfvarsData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write tfvars: %w", err)
	}

	// Initialize if needed
	if _, err := os.Stat(filepath.Join("/app", ".terraform")); os.IsNotExist(err) {
		cmd := exec.Command("tofu", "init")
		cmd.Dir = "/app"
		cmd.Env = os.Environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("init failed: %s", string(out))
		}
	}

	var logs bytes.Buffer
	var response *ModuleResponse

	switch request.Action {
	case "preview":
		cmd := exec.Command("tofu", "plan", "-json", "-out=/workspace/plan.tfplan")
		cmd.Dir = "/app"
		cmd.Env = os.Environ()
		cmd.Stdout = &logs
		cmd.Stderr = &logs

		if err := cmd.Run(); err != nil {
			return &ModuleResponse{
				Success: false,
				Action:  request.Action,
				Error:   fmt.Sprintf("plan failed: %v", err),
				Logs:    logs.String(),
			}, nil
		}

		response = &ModuleResponse{
			Success: true,
			Action:  request.Action,
			Changes: parseTofuPlanOutput(logs.String()),
			Logs:    logs.String(),
		}

	case "apply":
		cmd := exec.Command("tofu", "apply", "-auto-approve", "-json")
		cmd.Dir = "/app"
		cmd.Env = os.Environ()
		cmd.Stdout = &logs
		cmd.Stderr = &logs

		if err := cmd.Run(); err != nil {
			return &ModuleResponse{
				Success: false,
				Action:  request.Action,
				Error:   fmt.Sprintf("apply failed: %v", err),
				Logs:    logs.String(),
			}, nil
		}

		// Get outputs
		outputs, err := getTofuOutputs()
		if err != nil {
			return &ModuleResponse{
				Success: false,
				Action:  request.Action,
				Error:   fmt.Sprintf("failed to get outputs: %v", err),
				Logs:    logs.String(),
			}, nil
		}

		response = &ModuleResponse{
			Success: true,
			Action:  request.Action,
			Outputs: outputs,
			Logs:    logs.String(),
		}

	case "destroy":
		cmd := exec.Command("tofu", "destroy", "-auto-approve", "-json")
		cmd.Dir = "/app"
		cmd.Env = os.Environ()
		cmd.Stdout = &logs
		cmd.Stderr = &logs

		if err := cmd.Run(); err != nil {
			return &ModuleResponse{
				Success: false,
				Action:  request.Action,
				Error:   fmt.Sprintf("destroy failed: %v", err),
				Logs:    logs.String(),
			}, nil
		}

		response = &ModuleResponse{
			Success: true,
			Action:  request.Action,
			Logs:    logs.String(),
		}

	default:
		return nil, fmt.Errorf("unsupported action: %s", request.Action)
	}

	return response, nil
}

func getTofuOutputs() (map[string]OutputValue, error) {
	cmd := exec.Command("tofu", "output", "-json")
	cmd.Dir = "/app"
	cmd.Env = os.Environ()

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var rawOutputs map[string]struct {
		Value     interface{} `json:"value"`
		Type      interface{} `json:"type"`
		Sensitive bool        `json:"sensitive"`
	}
	if err := json.Unmarshal(output, &rawOutputs); err != nil {
		return nil, err
	}

	outputs := make(map[string]OutputValue)
	for k, v := range rawOutputs {
		outputs[k] = OutputValue{
			Value:     v.Value,
			Sensitive: v.Sensitive,
		}
	}

	return outputs, nil
}

func parseTofuPlanOutput(output string) []ResourceChange {
	// Simplified parsing - in production would parse OpenTofu's JSON output
	return nil
}
