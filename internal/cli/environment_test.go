package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Helper to create a temporary environment file
func createTempEnvironment(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "environment.yml"), []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create environment.yml: %v", err)
	}
	return filepath.Join(dir, "environment.yml")
}

func TestNewEnvironmentCmd(t *testing.T) {
	cmd := newEnvironmentCmd()

	if cmd.Use != "environment" {
		t.Errorf("expected use 'environment', got '%s'", cmd.Use)
	}

	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "env" {
		t.Error("expected alias 'env'")
	}

	// Check that all subcommands are registered
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Use] = true
	}

	expectedCommands := []string{
		"list",
		"get <name>",
		"create <name>",
		"update <name> [config-file]",
		"destroy <name>",
		"validate [path]",
	}

	for _, expected := range expectedCommands {
		if !subcommands[expected] {
			t.Errorf("expected subcommand '%s' not found", expected)
		}
	}
}

func TestEnvironmentListCmd_Flags(t *testing.T) {
	cmd := newEnvironmentListCmd()

	if cmd.Use != "list" {
		t.Errorf("expected use 'list', got '%s'", cmd.Use)
	}

	// Check optional flags
	flags := []string{"datacenter", "output", "backend", "backend-config"}
	for _, flagName := range flags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}

	// Check shorthands
	if cmd.Flags().ShorthandLookup("d") == nil {
		t.Error("expected -d shorthand for --datacenter")
	}
	if cmd.Flags().ShorthandLookup("o") == nil {
		t.Error("expected -o shorthand for --output")
	}
}

func TestEnvironmentGetCmd_Flags(t *testing.T) {
	cmd := newEnvironmentGetCmd()

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

func TestEnvironmentCreateCmd_Flags(t *testing.T) {
	cmd := newEnvironmentCreateCmd()

	if cmd.Use != "create <name>" {
		t.Errorf("expected use 'create <name>', got '%s'", cmd.Use)
	}

	// Check required flags
	dcFlag := cmd.Flags().Lookup("datacenter")
	if dcFlag == nil {
		t.Error("expected --datacenter flag")
	}

	// Check optional flags
	optionalFlags := []string{"if-not-exists", "backend", "backend-config"}
	for _, flagName := range optionalFlags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}

	// Check shorthands
	if cmd.Flags().ShorthandLookup("d") == nil {
		t.Error("expected -d shorthand for --datacenter")
	}
}

func TestEnvironmentUpdateCmd_Flags(t *testing.T) {
	cmd := newEnvironmentUpdateCmd()

	if cmd.Use != "update <name> [config-file]" {
		t.Errorf("expected use 'update <name> [config-file]', got '%s'", cmd.Use)
	}

	// Check flags
	flags := []string{"datacenter", "auto-approve", "backend", "backend-config"}
	for _, flagName := range flags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}

	// Check shorthands
	if cmd.Flags().ShorthandLookup("d") == nil {
		t.Error("expected -d shorthand for --datacenter")
	}
}

func TestEnvironmentDestroyCmd_Flags(t *testing.T) {
	cmd := newEnvironmentDestroyCmd()

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

func TestEnvironmentValidateCmd_Flags(t *testing.T) {
	cmd := newEnvironmentValidateCmd()

	if !strings.HasPrefix(cmd.Use, "validate") {
		t.Errorf("expected use to start with 'validate', got '%s'", cmd.Use)
	}

	if cmd.Flags().Lookup("file") == nil {
		t.Error("expected --file flag")
	}
}

func TestEnvironmentValidateCmd_ValidEnvironment(t *testing.T) {
	envYAML := `
name: staging
datacenter: aws-production

components:
  ghcr.io/myorg/web-app:
    source: v1.0.0
    variables:
      log_level: debug
`
	envFile := createTempEnvironment(t, envYAML)

	cmd := newEnvironmentValidateCmd()
	cmd.SetArgs([]string{envFile})

	// The command should execute without error for valid environment
	err := cmd.Execute()
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestEnvironmentValidateCmd_InvalidEnvironment(t *testing.T) {
	// Create an invalid environment file
	dir := t.TempDir()
	invalidYAML := `
invalid: yaml: [
`
	err := os.WriteFile(filepath.Join(dir, "environment.yml"), []byte(invalidYAML), 0644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	cmd := newEnvironmentValidateCmd()
	cmd.SetArgs([]string{filepath.Join(dir, "environment.yml")})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err = cmd.Execute()
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestEnvironmentValidateCmd_NonExistentFile(t *testing.T) {
	cmd := newEnvironmentValidateCmd()
	cmd.SetArgs([]string{"/nonexistent/path/environment.yml"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestEnvTruncateString(t *testing.T) {
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
		result := envTruncateString(test.input, test.maxLen)
		if result != test.expected {
			t.Errorf("envTruncateString(%q, %d) = %q, expected %q",
				test.input, test.maxLen, result, test.expected)
		}
	}
}

func TestEnvironmentValidateCmd_WithDirectory(t *testing.T) {
	envYAML := `
name: staging
datacenter: aws-production
`
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "environment.yml"), []byte(envYAML), 0644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	cmd := newEnvironmentValidateCmd()
	cmd.SetArgs([]string{dir})

	// The command should execute without error for valid environment
	err = cmd.Execute()
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestEnvironmentValidateCmd_WithFileFlag(t *testing.T) {
	envYAML := `
name: staging
datacenter: aws-production
`
	dir := t.TempDir()
	customFile := filepath.Join(dir, "custom-env.yml")
	err := os.WriteFile(customFile, []byte(envYAML), 0644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	cmd := newEnvironmentValidateCmd()
	cmd.SetArgs([]string{"--file", customFile})

	// The command should execute without error for valid environment
	err = cmd.Execute()
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}
