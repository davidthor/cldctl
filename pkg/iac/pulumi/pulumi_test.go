package pulumi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/davidthor/cldctl/pkg/iac"
	"gopkg.in/yaml.v3"
)

func TestPlugin_Name(t *testing.T) {
	p := &Plugin{
		pulumiPath: "/usr/bin/pulumi",
	}

	if p.Name() != "pulumi" {
		t.Errorf("expected 'pulumi', got %q", p.Name())
	}
}

func TestMapPulumiAction(t *testing.T) {
	tests := []struct {
		name     string
		op       string
		expected iac.ChangeAction
	}{
		{
			name:     "create action",
			op:       "create",
			expected: iac.ActionCreate,
		},
		{
			name:     "update action",
			op:       "update",
			expected: iac.ActionUpdate,
		},
		{
			name:     "delete action",
			op:       "delete",
			expected: iac.ActionDelete,
		},
		{
			name:     "replace action",
			op:       "replace",
			expected: iac.ActionReplace,
		},
		{
			name:     "same action (no-op)",
			op:       "same",
			expected: iac.ActionNoop,
		},
		{
			name:     "empty action",
			op:       "",
			expected: iac.ActionNoop,
		},
		{
			name:     "unknown action",
			op:       "unknown",
			expected: iac.ActionNoop,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := mapPulumiAction(tc.op)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestGetStackName(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		expected string
	}{
		{
			name: "PULUMI_STACK set",
			env: map[string]string{
				"PULUMI_STACK": "production",
			},
			expected: "production",
		},
		{
			name: "ENVIRONMENT set",
			env: map[string]string{
				"ENVIRONMENT": "staging",
			},
			expected: "staging",
		},
		{
			name: "PULUMI_STACK takes precedence",
			env: map[string]string{
				"PULUMI_STACK": "production",
				"ENVIRONMENT":  "staging",
			},
			expected: "production",
		},
		{
			name:     "default to dev",
			env:      map[string]string{},
			expected: "dev",
		},
		{
			name:     "nil env defaults to dev",
			env:      nil,
			expected: "dev",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := getStackName(tc.env)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestHasEnv(t *testing.T) {
	tests := []struct {
		name     string
		env      []string
		key      string
		expected bool
	}{
		{
			name:     "key exists",
			env:      []string{"PATH=/usr/bin", "HOME=/home/user"},
			key:      "PATH",
			expected: true,
		},
		{
			name:     "key not found",
			env:      []string{"PATH=/usr/bin", "HOME=/home/user"},
			key:      "MISSING",
			expected: false,
		},
		{
			name:     "empty env",
			env:      []string{},
			key:      "PATH",
			expected: false,
		},
		{
			name:     "nil env",
			env:      nil,
			key:      "PATH",
			expected: false,
		},
		{
			name:     "partial key match is false",
			env:      []string{"MYPATH=/usr/bin"},
			key:      "PATH",
			expected: false,
		},
		{
			name:     "exact key match",
			env:      []string{"PATH=/usr/bin", "MYPATH=/opt/bin"},
			key:      "PATH",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := hasEnv(tc.env, tc.key)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestPlugin_WriteConfig(t *testing.T) {
	p := &Plugin{}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pulumi-config-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	stackName := "test-stack"
	inputs := map[string]interface{}{
		"string_var": "test-value",
		"number_var": 42,
		"bool_var":   true,
	}

	err = p.writeConfig(tmpDir, stackName, inputs)
	if err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Verify file was created
	configFile := filepath.Join(tmpDir, "Pulumi.test-stack.yaml")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		t.Error("expected config file to be created")
	}

	// Read and verify content
	content, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	if len(content) == 0 {
		t.Error("expected non-empty config content")
	}
}

func TestPlugin_WriteConfig_EmptyInputs(t *testing.T) {
	p := &Plugin{}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pulumi-config-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with empty inputs
	err = p.writeConfig(tmpDir, "test-stack", map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error for empty inputs: %v", err)
	}

	// Verify file was NOT created
	configFile := filepath.Join(tmpDir, "Pulumi.test-stack.yaml")
	if _, err := os.Stat(configFile); !os.IsNotExist(err) {
		t.Error("expected no config file for empty inputs")
	}
}

func TestPlugin_WriteConfig_NilInputs(t *testing.T) {
	p := &Plugin{}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "pulumi-config-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with nil inputs
	err = p.writeConfig(tmpDir, "test-stack", nil)
	if err != nil {
		t.Fatalf("unexpected error for nil inputs: %v", err)
	}

	// Verify file was NOT created
	configFile := filepath.Join(tmpDir, "Pulumi.test-stack.yaml")
	if _, err := os.Stat(configFile); !os.IsNotExist(err) {
		t.Error("expected no config file for nil inputs")
	}
}

func TestPlugin_ParsePreviewOutput(t *testing.T) {
	p := &Plugin{}

	// Test with valid preview output
	output := `{
		"steps": [
			{"op": "create", "urn": "urn:pulumi:dev::project::aws:s3/bucket:Bucket::my-bucket", "type": "aws:s3/bucket:Bucket"},
			{"op": "update", "urn": "urn:pulumi:dev::project::aws:ec2/instance:Instance::my-instance", "type": "aws:ec2/instance:Instance"},
			{"op": "delete", "urn": "urn:pulumi:dev::project::aws:rds/instance:Instance::old-db", "type": "aws:rds/instance:Instance"},
			{"op": "replace", "urn": "urn:pulumi:dev::project::aws:lambda/function:Function::my-func", "type": "aws:lambda/function:Function"},
			{"op": "same", "urn": "urn:pulumi:dev::project::aws:vpc:Vpc::my-vpc", "type": "aws:vpc:Vpc"}
		],
		"changeSummary": {"create": 1, "update": 1, "delete": 1, "replace": 1}
	}`

	result, err := p.parsePreviewOutput(output)
	if err != nil {
		t.Fatalf("failed to parse preview output: %v", err)
	}

	// 4 changes (excluding "same" which is no-op)
	if len(result.Changes) != 4 {
		t.Errorf("expected 4 changes, got %d", len(result.Changes))
	}

	if result.Summary.Create != 1 {
		t.Errorf("expected 1 create, got %d", result.Summary.Create)
	}
	if result.Summary.Update != 1 {
		t.Errorf("expected 1 update, got %d", result.Summary.Update)
	}
	if result.Summary.Delete != 1 {
		t.Errorf("expected 1 delete, got %d", result.Summary.Delete)
	}
	if result.Summary.Replace != 1 {
		t.Errorf("expected 1 replace, got %d", result.Summary.Replace)
	}
}

func TestPlugin_ParsePreviewOutput_Empty(t *testing.T) {
	p := &Plugin{}

	result, err := p.parsePreviewOutput("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Changes) != 0 {
		t.Errorf("expected 0 changes, got %d", len(result.Changes))
	}
}

func TestPlugin_ParsePreviewOutput_InvalidJSON(t *testing.T) {
	p := &Plugin{}

	// Should handle invalid JSON gracefully
	result, err := p.parsePreviewOutput("not valid json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return empty result for invalid JSON
	if len(result.Changes) != 0 {
		t.Errorf("expected 0 changes for invalid JSON, got %d", len(result.Changes))
	}
}

func TestPlugin_ParsePreviewOutput_NoSteps(t *testing.T) {
	p := &Plugin{}

	output := `{
		"steps": [],
		"changeSummary": {}
	}`

	result, err := p.parsePreviewOutput(output)
	if err != nil {
		t.Fatalf("failed to parse preview output: %v", err)
	}

	if len(result.Changes) != 0 {
		t.Errorf("expected 0 changes, got %d", len(result.Changes))
	}
}

func TestState_Struct(t *testing.T) {
	state := State{
		StackName:   "dev",
		ProjectName: "my-project",
		Outputs: map[string]interface{}{
			"endpoint": "https://example.com",
			"port":     8080,
		},
		Resources: []ResourceState{
			{
				URN:  "urn:pulumi:dev::project::aws:s3/bucket:Bucket::my-bucket",
				Type: "aws:s3/bucket:Bucket",
				ID:   "my-bucket-12345",
				Properties: map[string]interface{}{
					"bucketName": "my-bucket-12345",
				},
			},
		},
	}

	if state.StackName != "dev" {
		t.Errorf("expected StackName='dev', got %q", state.StackName)
	}
	if state.ProjectName != "my-project" {
		t.Errorf("expected ProjectName='my-project', got %q", state.ProjectName)
	}
	if len(state.Outputs) != 2 {
		t.Errorf("expected 2 outputs, got %d", len(state.Outputs))
	}
	if len(state.Resources) != 1 {
		t.Errorf("expected 1 resource, got %d", len(state.Resources))
	}
}

func TestResourceState_Struct(t *testing.T) {
	rs := ResourceState{
		URN:  "urn:pulumi:dev::project::aws:ec2/instance:Instance::my-instance",
		Type: "aws:ec2/instance:Instance",
		ID:   "i-12345",
		Properties: map[string]interface{}{
			"instanceType": "t2.micro",
			"ami":          "ami-12345",
			"tags": map[string]interface{}{
				"Name": "my-instance",
			},
		},
	}

	if rs.URN != "urn:pulumi:dev::project::aws:ec2/instance:Instance::my-instance" {
		t.Errorf("unexpected URN: %q", rs.URN)
	}
	if rs.Type != "aws:ec2/instance:Instance" {
		t.Errorf("expected Type='aws:ec2/instance:Instance', got %q", rs.Type)
	}
	if rs.ID != "i-12345" {
		t.Errorf("expected ID='i-12345', got %q", rs.ID)
	}
	if rs.Properties["instanceType"] != "t2.micro" {
		t.Errorf("expected instanceType='t2.micro', got %v", rs.Properties["instanceType"])
	}
}

func TestPlugin_Interface(t *testing.T) {
	// Verify Plugin implements iac.Plugin interface
	var _ iac.Plugin = (*Plugin)(nil)
}

func TestYamlEncoder_Marshal(t *testing.T) {
	// Test the yaml encoder fallback to JSON
	data := map[string]interface{}{
		"key":    "value",
		"number": 42,
	}

	result, err := yaml.Marshal(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) == 0 {
		t.Error("expected non-empty result")
	}
}
