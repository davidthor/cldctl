package cli

import (
	"os"
	"testing"
	"time"
)

func TestNewDeployCmd(t *testing.T) {
	cmd := newDeployCmd()

	if cmd.Use != "deploy" {
		t.Errorf("expected use 'deploy', got '%s'", cmd.Use)
	}

	// Check that subcommands are registered
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Use] = true
	}

	expectedCommands := []string{
		"component <source>",
		"datacenter <name> <config>",
	}

	for _, expected := range expectedCommands {
		if !subcommands[expected] {
			t.Errorf("expected subcommand '%s' not found", expected)
		}
	}
}

func TestDeployComponentCmd_Flags(t *testing.T) {
	cmd := newDeployComponentCmd()

	if cmd.Use != "component <source>" {
		t.Errorf("expected use 'component <source>', got '%s'", cmd.Use)
	}

	// Check required flags
	if cmd.Flags().Lookup("environment") == nil {
		t.Error("expected --environment flag")
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

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "comp" {
		t.Error("expected alias 'comp'")
	}
}

func TestDeployDatacenterCmd_Flags(t *testing.T) {
	cmd := newDeployDatacenterCmd()

	if cmd.Use != "datacenter <name> <config>" {
		t.Errorf("expected use 'datacenter <name> <config>', got '%s'", cmd.Use)
	}

	// Check optional flags
	optionalFlags := []string{"var", "var-file", "auto-approve", "backend", "backend-config"}
	for _, flagName := range optionalFlags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "dc" {
		t.Error("expected alias 'dc'")
	}
}

func TestDeriveComponentName(t *testing.T) {
	tests := []struct {
		source      string
		isLocalPath bool
		expected    string
	}{
		// Local paths
		{"./my-app", true, "my-app"},
		{"./my-app/", true, "my-app"},
		{"/home/user/projects/my-app", true, "my-app"},
		{"./my-app/architect.yml", true, "my-app"},
		{"./my-app/architect.yaml", true, "my-app"},
		{"my-component", true, "my-component"},

		// OCI references
		{"ghcr.io/myorg/myapp:v1.0.0", false, "myapp"},
		{"docker.io/library/nginx:latest", false, "nginx"},
		{"myregistry.com/team/service:sha-abc123", false, "service"},
		{"ghcr.io/org/repo@sha256:abcd1234", false, "repo"},
		{"nginx:latest", false, "nginx"},
		{"myapp", false, "myapp"},
	}

	for _, test := range tests {
		result := deriveComponentName(test.source, test.isLocalPath)
		if result != test.expected {
			t.Errorf("deriveComponentName(%q, %v) = %q, expected %q",
				test.source, test.isLocalPath, result, test.expected)
		}
	}
}

func TestIsInteractive_CIEnvVars(t *testing.T) {
	// Save original env vars
	originalCI := os.Getenv("CI")
	originalGitHub := os.Getenv("GITHUB_ACTIONS")

	// Cleanup
	defer func() {
		if originalCI != "" {
			os.Setenv("CI", originalCI)
		} else {
			os.Unsetenv("CI")
		}
		if originalGitHub != "" {
			os.Setenv("GITHUB_ACTIONS", originalGitHub)
		} else {
			os.Unsetenv("GITHUB_ACTIONS")
		}
	}()

	// Test with CI=true
	os.Setenv("CI", "true")
	if isInteractive() {
		t.Error("isInteractive() should return false when CI=true")
	}

	// Test with GITHUB_ACTIONS=true
	os.Unsetenv("CI")
	os.Setenv("GITHUB_ACTIONS", "true")
	if isInteractive() {
		t.Error("isInteractive() should return false when GITHUB_ACTIONS=true")
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
