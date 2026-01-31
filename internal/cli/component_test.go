package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// Helper function to execute a command and capture output
func executeCommand(root *cobra.Command, args ...string) (output string, err error) { //nolint:unused //nolint:unused
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)

	err = root.Execute()
	return buf.String(), err
}

// Helper to create a temporary component directory with architect.yml
func createTempComponent(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "architect.yml"), []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create architect.yml: %v", err)
	}
	return dir
}

func TestNewComponentCmd(t *testing.T) {
	cmd := newComponentCmd()

	if cmd.Use != "component" {
		t.Errorf("expected use 'component', got '%s'", cmd.Use)
	}

	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "comp" {
		t.Error("expected alias 'comp'")
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
		"pull <repo:tag>",
		"list",
		"get <name>",
		"deploy <name>",
		"destroy <name>",
		"validate [path]",
	}

	for _, expected := range expectedCommands {
		if !subcommands[expected] {
			t.Errorf("expected subcommand '%s' not found", expected)
		}
	}
}

func TestComponentBuildCmd_Flags(t *testing.T) {
	cmd := newComponentBuildCmd()

	// Check required flags
	tagFlag := cmd.Flags().Lookup("tag")
	if tagFlag == nil {
		t.Error("expected --tag flag")
	}

	// Check optional flags
	flags := []string{"artifact-tag", "file", "platform", "no-cache", "dry-run"}
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

func TestComponentTagCmd_Flags(t *testing.T) {
	cmd := newComponentTagCmd()

	if cmd.Use != "tag <source> <target>" {
		t.Errorf("expected use 'tag <source> <target>', got '%s'", cmd.Use)
	}

	// Check flags
	if cmd.Flags().Lookup("artifact-tag") == nil {
		t.Error("expected --artifact-tag flag")
	}
	if cmd.Flags().Lookup("yes") == nil {
		t.Error("expected --yes flag")
	}
	if cmd.Flags().ShorthandLookup("y") == nil {
		t.Error("expected -y shorthand for --yes")
	}
}

func TestComponentPushCmd_Flags(t *testing.T) {
	cmd := newComponentPushCmd()

	if cmd.Use != "push <repo:tag>" {
		t.Errorf("expected use 'push <repo:tag>', got '%s'", cmd.Use)
	}

	if cmd.Flags().Lookup("yes") == nil {
		t.Error("expected --yes flag")
	}
}

func TestComponentListCmd_Flags(t *testing.T) {
	cmd := newComponentListCmd()

	// Check optional flags (environment is now optional)
	flags := []string{"environment", "output", "backend", "backend-config"}
	for _, flagName := range flags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}

	// Check shorthands
	if cmd.Flags().ShorthandLookup("e") == nil {
		t.Error("expected -e shorthand for --environment")
	}
	if cmd.Flags().ShorthandLookup("o") == nil {
		t.Error("expected -o shorthand for --output")
	}
}

func TestComponentPullCmd_Flags(t *testing.T) {
	cmd := newComponentPullCmd()

	if cmd.Use != "pull <repo:tag>" {
		t.Errorf("expected use 'pull <repo:tag>', got '%s'", cmd.Use)
	}

	// Check flags
	if cmd.Flags().Lookup("quiet") == nil {
		t.Error("expected --quiet flag")
	}
	if cmd.Flags().ShorthandLookup("q") == nil {
		t.Error("expected -q shorthand for --quiet")
	}
}

func TestComponentGetCmd_Flags(t *testing.T) {
	cmd := newComponentGetCmd()

	if cmd.Use != "get <name>" {
		t.Errorf("expected use 'get <name>', got '%s'", cmd.Use)
	}

	// Check flags
	flags := []string{"environment", "output", "backend", "backend-config"}
	for _, flagName := range flags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}
}

func TestComponentDeployCmd_Flags(t *testing.T) {
	cmd := newComponentDeployCmd()

	if cmd.Use != "deploy <name>" {
		t.Errorf("expected use 'deploy <name>', got '%s'", cmd.Use)
	}

	// Check required flags
	requiredFlags := []string{"environment", "config"}
	for _, flagName := range requiredFlags {
		flag := cmd.Flags().Lookup(flagName)
		if flag == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}

	// Check optional flags
	optionalFlags := []string{"var", "var-file", "auto-approve", "target", "backend", "backend-config"}
	for _, flagName := range optionalFlags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}

	// Check shorthands
	if cmd.Flags().ShorthandLookup("e") == nil {
		t.Error("expected -e shorthand for --environment")
	}
	if cmd.Flags().ShorthandLookup("c") == nil {
		t.Error("expected -c shorthand for --config")
	}
}

func TestComponentDestroyCmd_Flags(t *testing.T) {
	cmd := newComponentDestroyCmd()

	if cmd.Use != "destroy <name>" {
		t.Errorf("expected use 'destroy <name>', got '%s'", cmd.Use)
	}

	// Check flags
	flags := []string{"environment", "auto-approve", "target", "backend", "backend-config"}
	for _, flagName := range flags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}
}

func TestComponentValidateCmd_Flags(t *testing.T) {
	cmd := newComponentValidateCmd()

	if !strings.HasPrefix(cmd.Use, "validate") {
		t.Errorf("expected use to start with 'validate', got '%s'", cmd.Use)
	}

	if cmd.Flags().Lookup("file") == nil {
		t.Error("expected --file flag")
	}
}

func TestComponentValidateCmd_ValidComponent(t *testing.T) {
	componentYAML := `
name: test-app
description: Test application

deployments:
  api:
    image: nginx:latest
`
	dir := createTempComponent(t, componentYAML)

	cmd := newComponentValidateCmd()
	cmd.SetArgs([]string{dir})

	// The command should execute without error for valid component
	err := cmd.Execute()
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestComponentValidateCmd_InvalidComponent(t *testing.T) {
	// Create an invalid component file
	dir := t.TempDir()
	invalidYAML := `
this is not: valid yaml: [
`
	err := os.WriteFile(filepath.Join(dir, "architect.yml"), []byte(invalidYAML), 0644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	cmd := newComponentValidateCmd()
	cmd.SetArgs([]string{dir})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err = cmd.Execute()
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestComponentValidateCmd_NonExistentFile(t *testing.T) {
	cmd := newComponentValidateCmd()
	cmd.SetArgs([]string{"/nonexistent/path"})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestTruncateString(t *testing.T) {
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
		result := truncateString(test.input, test.maxLen)
		if result != test.expected {
			t.Errorf("truncateString(%q, %d) = %q, expected %q",
				test.input, test.maxLen, result, test.expected)
		}
	}
}

func TestParseVarFile(t *testing.T) {
	content := `
# This is a comment
KEY1=value1
KEY2="quoted value"
KEY3='single quoted'

# Another comment
EMPTY=
SPACES =  value with spaces  
`
	vars := make(map[string]string)
	err := parseVarFile([]byte(content), vars)
	if err != nil {
		t.Fatalf("parseVarFile failed: %v", err)
	}

	expected := map[string]string{
		"KEY1":   "value1",
		"KEY2":   "quoted value",
		"KEY3":   "single quoted",
		"EMPTY":  "",
		"SPACES": "value with spaces",
	}

	for key, expectedValue := range expected {
		if vars[key] != expectedValue {
			t.Errorf("vars[%q] = %q, expected %q", key, vars[key], expectedValue)
		}
	}
}

func TestParseVarFile_EmptyFile(t *testing.T) {
	vars := make(map[string]string)
	err := parseVarFile([]byte(""), vars)
	if err != nil {
		t.Fatalf("parseVarFile failed: %v", err)
	}

	if len(vars) != 0 {
		t.Errorf("expected empty vars, got %d", len(vars))
	}
}

func TestParseVarFile_OnlyComments(t *testing.T) {
	content := `
# Comment 1
# Comment 2
`
	vars := make(map[string]string)
	err := parseVarFile([]byte(content), vars)
	if err != nil {
		t.Fatalf("parseVarFile failed: %v", err)
	}

	if len(vars) != 0 {
		t.Errorf("expected empty vars, got %d", len(vars))
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0B"},
		{500, "500B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1048576, "1.0MB"},
		{1572864, "1.5MB"},
		{1073741824, "1.0GB"},
		{1610612736, "1.5GB"},
	}

	for _, test := range tests {
		result := formatSize(test.input)
		if result != test.expected {
			t.Errorf("formatSize(%d) = %q, expected %q", test.input, result, test.expected)
		}
	}
}

func TestFormatTimeAgo(t *testing.T) {
	now := time.Now()

	tests := []struct {
		input    time.Time
		contains string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5 minutes ago"},
		{now.Add(-1 * time.Minute), "1 minute ago"},
		{now.Add(-2 * time.Hour), "2 hours ago"},
		{now.Add(-1 * time.Hour), "1 hour ago"},
		{now.Add(-3 * 24 * time.Hour), "3 days ago"},
		{now.Add(-1 * 24 * time.Hour), "1 day ago"},
		{now.Add(-2 * 7 * 24 * time.Hour), "2 weeks ago"},
		{now.Add(-1 * 7 * 24 * time.Hour), "1 week ago"},
		{now.Add(-60 * 24 * time.Hour), "2 months ago"},
		{now.Add(-35 * 24 * time.Hour), "1 month ago"},
	}

	for _, test := range tests {
		result := formatTimeAgo(test.input)
		if result != test.contains {
			t.Errorf("formatTimeAgo(%v) = %q, expected %q", test.input, result, test.contains)
		}
	}
}
