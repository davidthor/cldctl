package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Helper to create a temporary datacenter directory with datacenter.hcl
func createTempDatacenter(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "datacenter.hcl"), []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create datacenter.hcl: %v", err)
	}
	return dir
}

func TestNewDatacenterCmd(t *testing.T) {
	cmd := newDatacenterCmd()

	if cmd.Use != "datacenter" {
		t.Errorf("expected use 'datacenter', got '%s'", cmd.Use)
	}

	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "dc" {
		t.Error("expected alias 'dc'")
	}

	// Check that all subcommands are registered
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Use] = true
	}

	expectedCommands := []string{
		"build [path]",
		"tag <source> <target>",
		"push <repo:tag>",
		"list",
		"get <name>",
		"deploy <name> <config>",
		"destroy <name>",
		"validate [path]",
	}

	for _, expected := range expectedCommands {
		if !subcommands[expected] {
			t.Errorf("expected subcommand '%s' not found", expected)
		}
	}
}

func TestDatacenterBuildCmd_Flags(t *testing.T) {
	cmd := newDatacenterBuildCmd()

	// Check required flags
	tagFlag := cmd.Flags().Lookup("tag")
	if tagFlag == nil {
		t.Error("expected --tag flag")
	}

	// Check optional flags
	flags := []string{"module-tag", "file", "dry-run"}
	for _, flagName := range flags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}

	// Check shorthand
	if cmd.Flags().ShorthandLookup("t") == nil {
		t.Error("expected -t shorthand for --tag")
	}
	if cmd.Flags().ShorthandLookup("f") == nil {
		t.Error("expected -f shorthand for --file")
	}
}

func TestDatacenterTagCmd_Flags(t *testing.T) {
	cmd := newDatacenterTagCmd()

	if cmd.Use != "tag <source> <target>" {
		t.Errorf("expected use 'tag <source> <target>', got '%s'", cmd.Use)
	}

	// Check flags
	if cmd.Flags().Lookup("module-tag") == nil {
		t.Error("expected --module-tag flag")
	}
	if cmd.Flags().Lookup("yes") == nil {
		t.Error("expected --yes flag")
	}
	if cmd.Flags().ShorthandLookup("y") == nil {
		t.Error("expected -y shorthand for --yes")
	}
}

func TestDatacenterPushCmd_Flags(t *testing.T) {
	cmd := newDatacenterPushCmd()

	if cmd.Use != "push <repo:tag>" {
		t.Errorf("expected use 'push <repo:tag>', got '%s'", cmd.Use)
	}

	if cmd.Flags().Lookup("yes") == nil {
		t.Error("expected --yes flag")
	}
}

func TestDatacenterListCmd_Flags(t *testing.T) {
	cmd := newDatacenterListCmd()

	if cmd.Use != "list" {
		t.Errorf("expected use 'list', got '%s'", cmd.Use)
	}

	// Check optional flags
	flags := []string{"output", "backend", "backend-config"}
	for _, flagName := range flags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}

	// Check shorthands
	if cmd.Flags().ShorthandLookup("o") == nil {
		t.Error("expected -o shorthand for --output")
	}
}

func TestDatacenterGetCmd_Flags(t *testing.T) {
	cmd := newDatacenterGetCmd()

	if cmd.Use != "get <name>" {
		t.Errorf("expected use 'get <name>', got '%s'", cmd.Use)
	}

	// Check flags
	flags := []string{"output", "backend", "backend-config"}
	for _, flagName := range flags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}
}

func TestDatacenterDeployCmd_Flags(t *testing.T) {
	cmd := newDatacenterDeployCmd()

	if cmd.Use != "deploy <name> <config>" {
		t.Errorf("expected use 'deploy <name> <config>', got '%s'", cmd.Use)
	}

	// Check optional flags
	optionalFlags := []string{"var", "var-file", "auto-approve", "backend", "backend-config"}
	for _, flagName := range optionalFlags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}
}

func TestDatacenterDestroyCmd_Flags(t *testing.T) {
	cmd := newDatacenterDestroyCmd()

	if cmd.Use != "destroy <name>" {
		t.Errorf("expected use 'destroy <name>', got '%s'", cmd.Use)
	}

	// Check flags
	flags := []string{"auto-approve", "backend", "backend-config"}
	for _, flagName := range flags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}
}

func TestDatacenterValidateCmd_Flags(t *testing.T) {
	cmd := newDatacenterValidateCmd()

	if !strings.HasPrefix(cmd.Use, "validate") {
		t.Errorf("expected use to start with 'validate', got '%s'", cmd.Use)
	}

	if cmd.Flags().Lookup("file") == nil {
		t.Error("expected --file flag")
	}
}

func TestDatacenterValidateCmd_ValidDatacenter(t *testing.T) {
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

	cmd := newDatacenterValidateCmd()
	cmd.SetArgs([]string{dir})

	// The command should execute without error for valid datacenter
	err := cmd.Execute()
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestDatacenterValidateCmd_InvalidDatacenter(t *testing.T) {
	// Create an invalid datacenter file
	dir := t.TempDir()
	invalidHCL := `
this is not valid HCL {{{
`
	err := os.WriteFile(filepath.Join(dir, "datacenter.hcl"), []byte(invalidHCL), 0644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	cmd := newDatacenterValidateCmd()
	cmd.SetArgs([]string{dir})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err = cmd.Execute()
	if err == nil {
		t.Error("expected error for invalid HCL")
	}
}

func TestDatacenterValidateCmd_NonExistentFile(t *testing.T) {
	cmd := newDatacenterValidateCmd()
	cmd.SetArgs([]string{"/nonexistent/path"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestDcTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a long string", 10, "this is..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
		{"", 5, ""},
	}

	for _, test := range tests {
		result := dcTruncateString(test.input, test.maxLen)
		if result != test.expected {
			t.Errorf("dcTruncateString(%q, %d) = %q, expected %q",
				test.input, test.maxLen, result, test.expected)
		}
	}
}

func TestDcParseVarFile(t *testing.T) {
	content := `
# This is a comment
REGION=us-east-1
CLUSTER_NAME="production"
ENVIRONMENT='staging'

# Empty line above
EMPTY=
`
	vars := make(map[string]string)
	err := dcParseVarFile([]byte(content), vars)
	if err != nil {
		t.Fatalf("dcParseVarFile failed: %v", err)
	}

	expected := map[string]string{
		"REGION":       "us-east-1",
		"CLUSTER_NAME": "production",
		"ENVIRONMENT":  "staging",
		"EMPTY":        "",
	}

	for key, expectedValue := range expected {
		if vars[key] != expectedValue {
			t.Errorf("vars[%q] = %q, expected %q", key, vars[key], expectedValue)
		}
	}
}

func TestDcParseVarFile_EmptyFile(t *testing.T) {
	vars := make(map[string]string)
	err := dcParseVarFile([]byte(""), vars)
	if err != nil {
		t.Fatalf("dcParseVarFile failed: %v", err)
	}

	if len(vars) != 0 {
		t.Errorf("expected empty vars, got %d", len(vars))
	}
}
