package container

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestModuleRequest_Marshal(t *testing.T) {
	request := &ModuleRequest{
		Action: "apply",
		Inputs: map[string]interface{}{
			"name":    "my-db",
			"version": "16",
			"port":    5432,
		},
		Environment: map[string]string{
			"AWS_REGION": "us-east-1",
		},
		StackName: "prod-api-database",
	}

	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ModuleRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Action != "apply" {
		t.Errorf("Action: got %q, want %q", decoded.Action, "apply")
	}
	if decoded.Inputs["name"] != "my-db" {
		t.Error("Inputs not preserved")
	}
	if decoded.Environment["AWS_REGION"] != "us-east-1" {
		t.Error("Environment not preserved")
	}
}

func TestModuleResponse_Marshal(t *testing.T) {
	response := &ModuleResponse{
		Success: true,
		Action:  "apply",
		Outputs: map[string]OutputValue{
			"host": {Value: "db.example.com"},
			"port": {Value: 5432},
			"url":  {Value: "postgresql://...", Sensitive: true},
		},
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ModuleResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if !decoded.Success {
		t.Error("Success should be true")
	}
	if decoded.Outputs["host"].Value != "db.example.com" {
		t.Error("Outputs not preserved")
	}
	if !decoded.Outputs["url"].Sensitive {
		t.Error("Sensitive flag not preserved")
	}
}

func TestModuleResponse_Error(t *testing.T) {
	response := &ModuleResponse{
		Success: false,
		Action:  "apply",
		Error:   "failed to create database: connection refused",
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ModuleResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Success {
		t.Error("Success should be false")
	}
	if decoded.Error == "" {
		t.Error("Error message should be preserved")
	}
}

func TestResourceChange_Marshal(t *testing.T) {
	change := ResourceChange{
		Resource: "aws:rds/instance:Instance::my-db",
		Action:   "create",
		After: map[string]interface{}{
			"engine":         "postgres",
			"engine_version": "16",
			"instance_class": "db.t3.micro",
		},
	}

	data, err := json.Marshal(change)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ResourceChange
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Action != "create" {
		t.Errorf("Action: got %q", decoded.Action)
	}
	if decoded.After["engine"] != "postgres" {
		t.Error("After state not preserved")
	}
}

func TestBackendConfig_Marshal(t *testing.T) {
	config := &BackendConfig{
		Type: "s3",
		Config: map[string]string{
			"bucket": "my-state-bucket",
			"key":    "terraform.tfstate",
			"region": "us-east-1",
		},
	}

	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded BackendConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Type != "s3" {
		t.Errorf("Type: got %q", decoded.Type)
	}
	if decoded.Config["bucket"] != "my-state-bucket" {
		t.Error("Config not preserved")
	}
}

func TestModuleTypeConstants(t *testing.T) {
	if ModuleTypePulumi != "pulumi" {
		t.Errorf("ModuleTypePulumi: got %q", ModuleTypePulumi)
	}
	if ModuleTypeOpenTofu != "opentofu" {
		t.Errorf("ModuleTypeOpenTofu: got %q", ModuleTypeOpenTofu)
	}
}

func TestDetectModuleType_Pulumi(t *testing.T) {
	// Create temp directory with Pulumi.yaml
	tmpDir, err := os.MkdirTemp("", "module-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write Pulumi.yaml
	pulumiYaml := `name: test-module
runtime: nodejs
`
	if err := os.WriteFile(filepath.Join(tmpDir, "Pulumi.yaml"), []byte(pulumiYaml), 0644); err != nil {
		t.Fatalf("Failed to write Pulumi.yaml: %v", err)
	}

	moduleType, err := DetectModuleType(tmpDir)
	if err != nil {
		t.Fatalf("DetectModuleType failed: %v", err)
	}

	if moduleType != ModuleTypePulumi {
		t.Errorf("Expected Pulumi, got %q", moduleType)
	}
}

func TestDetectModuleType_OpenTofu(t *testing.T) {
	// Create temp directory with .tf file
	tmpDir, err := os.MkdirTemp("", "module-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write main.tf
	mainTf := `resource "aws_instance" "example" {
  ami           = "ami-12345678"
  instance_type = "t2.micro"
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.tf"), []byte(mainTf), 0644); err != nil {
		t.Fatalf("Failed to write main.tf: %v", err)
	}

	moduleType, err := DetectModuleType(tmpDir)
	if err != nil {
		t.Fatalf("DetectModuleType failed: %v", err)
	}

	if moduleType != ModuleTypeOpenTofu {
		t.Errorf("Expected OpenTofu, got %q", moduleType)
	}
}

func TestDetectModuleType_Unknown(t *testing.T) {
	// Create empty temp directory
	tmpDir, err := os.MkdirTemp("", "module-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	_, err = DetectModuleType(tmpDir)
	if err == nil {
		t.Error("Expected error for unknown module type")
	}
}

func TestOutputValue(t *testing.T) {
	tests := []struct {
		name      string
		output    OutputValue
		sensitive bool
	}{
		{
			name:      "simple value",
			output:    OutputValue{Value: "hello"},
			sensitive: false,
		},
		{
			name:      "sensitive value",
			output:    OutputValue{Value: "secret123", Sensitive: true},
			sensitive: true,
		},
		{
			name:      "complex value",
			output:    OutputValue{Value: map[string]interface{}{"key": "value"}},
			sensitive: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.output.Sensitive != tt.sensitive {
				t.Errorf("Sensitive: got %v, want %v", tt.output.Sensitive, tt.sensitive)
			}
		})
	}
}
