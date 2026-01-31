package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewValidateCmd(t *testing.T) {
	cmd := newValidateCmd()

	if cmd.Use != "validate" {
		t.Errorf("expected use 'validate', got '%s'", cmd.Use)
	}

	// Check that subcommands are registered
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Use] = true
	}

	expectedCommands := []string{
		"component [path]",
		"datacenter [path]",
		"environment [path]",
	}

	for _, expected := range expectedCommands {
		if !subcommands[expected] {
			t.Errorf("expected subcommand '%s' not found", expected)
		}
	}
}

func TestValidateComponentCmd_Flags(t *testing.T) {
	cmd := newValidateComponentCmd()

	if !strings.HasPrefix(cmd.Use, "component") {
		t.Errorf("expected use to start with 'component', got '%s'", cmd.Use)
	}

	if cmd.Flags().Lookup("file") == nil {
		t.Error("expected --file flag")
	}

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "comp" {
		t.Error("expected alias 'comp'")
	}
}

func TestValidateComponentCmd_ValidComponent(t *testing.T) {
	componentYAML := `
name: test-app
description: Test application

deployments:
  api:
    image: nginx:latest
`
	dir := createTempComponent(t, componentYAML)

	cmd := newValidateComponentCmd()
	cmd.SetArgs([]string{dir})

	// The command should execute without error for valid component
	err := cmd.Execute()
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateComponentCmd_InvalidComponent(t *testing.T) {
	// Create an invalid component file
	dir := t.TempDir()
	invalidYAML := `
this is not: valid yaml: [
`
	err := os.WriteFile(filepath.Join(dir, "architect.yml"), []byte(invalidYAML), 0644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	cmd := newValidateComponentCmd()
	cmd.SetArgs([]string{dir})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err = cmd.Execute()
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestValidateComponentCmd_NonExistentFile(t *testing.T) {
	cmd := newValidateComponentCmd()
	cmd.SetArgs([]string{"/nonexistent/path"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestValidateDatacenterCmd_Flags(t *testing.T) {
	cmd := newValidateDatacenterCmd()

	if !strings.HasPrefix(cmd.Use, "datacenter") {
		t.Errorf("expected use to start with 'datacenter', got '%s'", cmd.Use)
	}

	if cmd.Flags().Lookup("file") == nil {
		t.Error("expected --file flag")
	}

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "dc" {
		t.Error("expected alias 'dc'")
	}
}

func TestValidateDatacenterCmd_ValidDatacenter(t *testing.T) {
	// Use an empty but valid datacenter HCL
	datacenterHCL := `
variable "network_name" {
  type        = "string"
  description = "Name of the network"
  default     = "default-network"
}

environment {
}
`
	dir := createTempDatacenter(t, datacenterHCL)

	cmd := newValidateDatacenterCmd()
	cmd.SetArgs([]string{dir})

	// The command should execute without error for valid datacenter
	err := cmd.Execute()
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateDatacenterCmd_InvalidDatacenter(t *testing.T) {
	// Create an invalid datacenter file
	dir := t.TempDir()
	invalidHCL := `
this is not valid HCL {{{
`
	err := os.WriteFile(filepath.Join(dir, "datacenter.hcl"), []byte(invalidHCL), 0644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	cmd := newValidateDatacenterCmd()
	cmd.SetArgs([]string{dir})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err = cmd.Execute()
	if err == nil {
		t.Error("expected error for invalid HCL")
	}
}

func TestValidateDatacenterCmd_NonExistentFile(t *testing.T) {
	cmd := newValidateDatacenterCmd()
	cmd.SetArgs([]string{"/nonexistent/path"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestValidateEnvironmentCmd_Flags(t *testing.T) {
	cmd := newValidateEnvironmentCmd()

	if !strings.HasPrefix(cmd.Use, "environment") {
		t.Errorf("expected use to start with 'environment', got '%s'", cmd.Use)
	}

	if cmd.Flags().Lookup("file") == nil {
		t.Error("expected --file flag")
	}

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "env" {
		t.Error("expected alias 'env'")
	}
}

func TestValidateEnvironmentCmd_ValidEnvironment(t *testing.T) {
	envYAML := `
name: staging
datacenter: aws-production

components:
  ghcr.io/myorg/web-app:
    source: v1.0.0
    variables:
      log_level: debug
`
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "environment.yml"), []byte(envYAML), 0644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	cmd := newValidateEnvironmentCmd()
	cmd.SetArgs([]string{filepath.Join(dir, "environment.yml")})

	// The command should execute without error for valid environment
	err = cmd.Execute()
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateEnvironmentCmd_InvalidEnvironment(t *testing.T) {
	// Create an invalid environment file
	dir := t.TempDir()
	invalidYAML := `
invalid: yaml: [
`
	err := os.WriteFile(filepath.Join(dir, "environment.yml"), []byte(invalidYAML), 0644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	cmd := newValidateEnvironmentCmd()
	cmd.SetArgs([]string{filepath.Join(dir, "environment.yml")})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err = cmd.Execute()
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestValidateEnvironmentCmd_NonExistentFile(t *testing.T) {
	cmd := newValidateEnvironmentCmd()
	cmd.SetArgs([]string{"/nonexistent/path/environment.yml"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}
