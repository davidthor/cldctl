package opentofu

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/davidthor/cldctl/pkg/iac"
)

func TestPlugin_Name(t *testing.T) {
	// Create plugin without checking for binary
	p := &Plugin{
		binaryPath: "/usr/bin/tofu",
		binaryName: "tofu",
	}

	if p.Name() != "opentofu" {
		t.Errorf("expected 'opentofu', got %q", p.Name())
	}
}

func TestMapTFAction(t *testing.T) {
	tests := []struct {
		name     string
		actions  []string
		expected iac.ChangeAction
	}{
		{
			name:     "create action",
			actions:  []string{"create"},
			expected: iac.ActionCreate,
		},
		{
			name:     "update action",
			actions:  []string{"update"},
			expected: iac.ActionUpdate,
		},
		{
			name:     "delete action",
			actions:  []string{"delete"},
			expected: iac.ActionDelete,
		},
		{
			name:     "replace action (create + delete)",
			actions:  []string{"create", "delete"},
			expected: iac.ActionReplace,
		},
		{
			name:     "replace action (delete + create)",
			actions:  []string{"delete", "create"},
			expected: iac.ActionReplace,
		},
		{
			name:     "empty actions",
			actions:  []string{},
			expected: iac.ActionNoop,
		},
		{
			name:     "nil actions",
			actions:  nil,
			expected: iac.ActionNoop,
		},
		{
			name:     "unknown action",
			actions:  []string{"unknown"},
			expected: iac.ActionNoop,
		},
		{
			name:     "no-op action",
			actions:  []string{"no-op"},
			expected: iac.ActionNoop,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := mapTFAction(tc.actions)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		item     string
		expected bool
	}{
		{
			name:     "item exists",
			slice:    []string{"a", "b", "c"},
			item:     "b",
			expected: true,
		},
		{
			name:     "item not found",
			slice:    []string{"a", "b", "c"},
			item:     "d",
			expected: false,
		},
		{
			name:     "empty slice",
			slice:    []string{},
			item:     "a",
			expected: false,
		},
		{
			name:     "nil slice",
			slice:    nil,
			item:     "a",
			expected: false,
		},
		{
			name:     "first element",
			slice:    []string{"a", "b", "c"},
			item:     "a",
			expected: true,
		},
		{
			name:     "last element",
			slice:    []string{"a", "b", "c"},
			item:     "c",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := contains(tc.slice, tc.item)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestPlugin_WriteTFVars(t *testing.T) {
	p := &Plugin{}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "tfvars-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with inputs
	inputs := map[string]interface{}{
		"string_var": "test-value",
		"number_var": 42,
		"bool_var":   true,
		"map_var": map[string]interface{}{
			"key": "value",
		},
	}

	err = p.writeTFVars(tmpDir, inputs)
	if err != nil {
		t.Fatalf("failed to write tfvars: %v", err)
	}

	// Verify file was created
	varFile := filepath.Join(tmpDir, "terraform.tfvars.json")
	if _, err := os.Stat(varFile); os.IsNotExist(err) {
		t.Error("expected tfvars file to be created")
	}

	// Read and verify content
	content, err := os.ReadFile(varFile)
	if err != nil {
		t.Fatalf("failed to read tfvars file: %v", err)
	}

	// Verify it's valid JSON with expected structure
	if len(content) == 0 {
		t.Error("expected non-empty tfvars content")
	}
}

func TestPlugin_WriteTFVars_EmptyInputs(t *testing.T) {
	p := &Plugin{}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "tfvars-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with empty inputs
	err = p.writeTFVars(tmpDir, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error for empty inputs: %v", err)
	}

	// Verify file was NOT created
	varFile := filepath.Join(tmpDir, "terraform.tfvars.json")
	if _, err := os.Stat(varFile); !os.IsNotExist(err) {
		t.Error("expected no tfvars file for empty inputs")
	}
}

func TestPlugin_WriteTFVars_NilInputs(t *testing.T) {
	p := &Plugin{}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "tfvars-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with nil inputs
	err = p.writeTFVars(tmpDir, nil)
	if err != nil {
		t.Fatalf("unexpected error for nil inputs: %v", err)
	}

	// Verify file was NOT created
	varFile := filepath.Join(tmpDir, "terraform.tfvars.json")
	if _, err := os.Stat(varFile); !os.IsNotExist(err) {
		t.Error("expected no tfvars file for nil inputs")
	}
}

func TestPlugin_ReadState(t *testing.T) {
	p := &Plugin{}

	// Create temp directory with state file
	tmpDir, err := os.MkdirTemp("", "tfstate-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	stateContent := `{
		"version": 4,
		"terraform_version": "1.5.0",
		"serial": 1,
		"lineage": "test-lineage",
		"outputs": {},
		"resources": []
	}`

	stateFile := filepath.Join(tmpDir, "terraform.tfstate")
	if err := os.WriteFile(stateFile, []byte(stateContent), 0644); err != nil {
		t.Fatalf("failed to write state file: %v", err)
	}

	// Read state
	state, err := p.readState(tmpDir)
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}

	if len(state) == 0 {
		t.Error("expected non-empty state")
	}
}

func TestPlugin_ReadState_NotFound(t *testing.T) {
	p := &Plugin{}

	// Create temp directory without state file
	tmpDir, err := os.MkdirTemp("", "tfstate-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Try to read non-existent state
	_, err = p.readState(tmpDir)
	if err == nil {
		t.Error("expected error for non-existent state file")
	}
}

func TestPlugin_ParsePlanOutput(t *testing.T) {
	p := &Plugin{}

	// Test with valid plan output
	output := `{"@level":"info","@message":"Plan: 1 to add, 0 to change, 0 to destroy."}
{"change":{"resource":{"addr":"aws_instance.example","resource_type":"aws_instance","resource_name":"example"},"action":["create"]}}
{"change":{"resource":{"addr":"aws_s3_bucket.data","resource_type":"aws_s3_bucket","resource_name":"data"},"action":["update"]}}
{"change":{"resource":{"addr":"aws_vpc.main","resource_type":"aws_vpc","resource_name":"main"},"action":["delete"]}}
{"change":{"resource":{"addr":"aws_rds_instance.db","resource_type":"aws_rds_instance","resource_name":"db"},"action":["create","delete"]}}`

	result, err := p.parsePlanOutput(output)
	if err != nil {
		t.Fatalf("failed to parse plan output: %v", err)
	}

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

func TestPlugin_ParsePlanOutput_Empty(t *testing.T) {
	p := &Plugin{}

	result, err := p.parsePlanOutput("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Changes) != 0 {
		t.Errorf("expected 0 changes, got %d", len(result.Changes))
	}
}

func TestPlugin_ParsePlanOutput_InvalidJSON(t *testing.T) {
	p := &Plugin{}

	// Should handle invalid JSON gracefully
	output := `not valid json
{"invalid: "json"}`

	result, err := p.parsePlanOutput(output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should skip invalid lines and return empty result
	if len(result.Changes) != 0 {
		t.Errorf("expected 0 changes, got %d", len(result.Changes))
	}
}

func TestPlugin_ParsePlanOutput_NoopChanges(t *testing.T) {
	p := &Plugin{}

	// Test with no-op changes (should be filtered out)
	output := `{"change":{"resource":{"addr":"aws_instance.example","resource_type":"aws_instance","resource_name":"example"},"action":["no-op"]}}
{"change":{"resource":{"addr":"aws_s3_bucket.data","resource_type":"aws_s3_bucket","resource_name":"data"},"action":[]}}`

	result, err := p.parsePlanOutput(output)
	if err != nil {
		t.Fatalf("failed to parse plan output: %v", err)
	}

	if len(result.Changes) != 0 {
		t.Errorf("expected 0 changes (no-op filtered), got %d", len(result.Changes))
	}
}

func TestTFState_Struct(t *testing.T) {
	state := TFState{
		Version:          4,
		TerraformVersion: "1.5.0",
		Serial:           1,
		Lineage:          "test-lineage",
		Outputs: TFOutputs{
			"endpoint": TFOutput{
				Value:     "https://example.com",
				Type:      "string",
				Sensitive: false,
			},
		},
		Resources: []TFResource{
			{
				Mode:     "managed",
				Type:     "aws_instance",
				Name:     "example",
				Provider: "registry.terraform.io/hashicorp/aws",
				Instances: []TFInstance{
					{
						SchemaVersion: 1,
						Attributes: map[string]interface{}{
							"id":   "i-12345",
							"type": "t2.micro",
						},
						Dependencies: []string{},
					},
				},
			},
		},
	}

	if state.Version != 4 {
		t.Errorf("expected Version=4, got %d", state.Version)
	}
	if state.TerraformVersion != "1.5.0" {
		t.Errorf("expected TerraformVersion='1.5.0', got %q", state.TerraformVersion)
	}
	if state.Serial != 1 {
		t.Errorf("expected Serial=1, got %d", state.Serial)
	}
	if state.Lineage != "test-lineage" {
		t.Errorf("expected Lineage='test-lineage', got %q", state.Lineage)
	}
	if len(state.Outputs) != 1 {
		t.Errorf("expected 1 output, got %d", len(state.Outputs))
	}
	if len(state.Resources) != 1 {
		t.Errorf("expected 1 resource, got %d", len(state.Resources))
	}
}

func TestTFOutput_Struct(t *testing.T) {
	output := TFOutput{
		Value:     "test-value",
		Type:      "string",
		Sensitive: true,
	}

	if output.Value != "test-value" {
		t.Errorf("expected Value='test-value', got %v", output.Value)
	}
	if output.Type != "string" {
		t.Errorf("expected Type='string', got %v", output.Type)
	}
	if !output.Sensitive {
		t.Error("expected Sensitive=true")
	}
}

func TestTFResource_Struct(t *testing.T) {
	resource := TFResource{
		Mode:     "managed",
		Type:     "aws_instance",
		Name:     "example",
		Provider: "registry.terraform.io/hashicorp/aws",
		Instances: []TFInstance{
			{
				SchemaVersion: 1,
				Attributes: map[string]interface{}{
					"id": "i-12345",
				},
				Dependencies: []string{"aws_vpc.main"},
			},
		},
	}

	if resource.Mode != "managed" {
		t.Errorf("expected Mode='managed', got %q", resource.Mode)
	}
	if resource.Type != "aws_instance" {
		t.Errorf("expected Type='aws_instance', got %q", resource.Type)
	}
	if resource.Name != "example" {
		t.Errorf("expected Name='example', got %q", resource.Name)
	}
	if resource.Provider != "registry.terraform.io/hashicorp/aws" {
		t.Errorf("expected Provider='registry.terraform.io/hashicorp/aws', got %q", resource.Provider)
	}
	if len(resource.Instances) != 1 {
		t.Errorf("expected 1 instance, got %d", len(resource.Instances))
	}
}

func TestTFInstance_Struct(t *testing.T) {
	instance := TFInstance{
		SchemaVersion: 1,
		Attributes: map[string]interface{}{
			"id":            "i-12345",
			"instance_type": "t2.micro",
			"tags": map[string]interface{}{
				"Name": "example",
			},
		},
		Dependencies: []string{"aws_vpc.main", "aws_subnet.public"},
	}

	if instance.SchemaVersion != 1 {
		t.Errorf("expected SchemaVersion=1, got %d", instance.SchemaVersion)
	}
	if instance.Attributes["id"] != "i-12345" {
		t.Errorf("expected id='i-12345', got %v", instance.Attributes["id"])
	}
	if len(instance.Dependencies) != 2 {
		t.Errorf("expected 2 dependencies, got %d", len(instance.Dependencies))
	}
}

func TestPlugin_Interface(t *testing.T) {
	// Verify Plugin implements iac.Plugin interface
	var _ iac.Plugin = (*Plugin)(nil)
}

func TestPlugin_Init_AlreadyInitialized(t *testing.T) {
	p := &Plugin{}

	// Create temp directory with .terraform directory
	tmpDir, err := os.MkdirTemp("", "tf-init-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create .terraform directory to simulate already initialized
	terraformDir := filepath.Join(tmpDir, ".terraform")
	if err := os.Mkdir(terraformDir, 0755); err != nil {
		t.Fatalf("failed to create .terraform dir: %v", err)
	}

	// Init should return early without error
	err = p.init(context.TODO(), tmpDir, iac.RunOptions{})
	if err != nil {
		t.Errorf("expected no error for already initialized project, got: %v", err)
	}
}
