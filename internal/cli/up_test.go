package cli

import (
	"strings"
	"testing"
)

func TestNewUpCmd(t *testing.T) {
	cmd := newUpCmd()

	if cmd.Use != "up" {
		t.Errorf("expected use 'up', got '%s'", cmd.Use)
	}

	// Check that the command has no aliases
	if len(cmd.Aliases) != 0 {
		t.Error("expected no aliases for up command")
	}

	// Verify the command accepts no positional arguments
	if cmd.Args == nil {
		t.Error("expected Args validator")
	}
}

func TestUpCmd_Flags(t *testing.T) {
	cmd := newUpCmd()

	// Check flags including required datacenter
	flags := []string{"component", "environment", "datacenter", "name", "var", "var-file", "detach", "no-open", "port"}
	for _, flagName := range flags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}

	// Check shorthands
	if cmd.Flags().ShorthandLookup("c") == nil {
		t.Error("expected -c shorthand for --component")
	}
	if cmd.Flags().ShorthandLookup("e") == nil {
		t.Error("expected -e shorthand for --environment")
	}
	if cmd.Flags().ShorthandLookup("n") == nil {
		t.Error("expected -n shorthand for --name")
	}
	if cmd.Flags().ShorthandLookup("d") == nil {
		t.Error("expected -d shorthand for --datacenter")
	}
}

func TestUpCmd_DatacenterFlag(t *testing.T) {
	cmd := newUpCmd()

	// Check that datacenter flag exists
	dcFlag := cmd.Flags().Lookup("datacenter")
	if dcFlag == nil {
		t.Fatal("expected --datacenter flag")
	}

	// Datacenter is no longer marked as required because it can be
	// resolved from the CLDCTL_DATACENTER env var or default_datacenter config
	annotations := dcFlag.Annotations
	if _, ok := annotations["cobra_annotation_bash_completion_one_required_flag"]; ok {
		t.Error("--datacenter should NOT be marked as required (resolved via flag, env var, or config)")
	}
}

func TestUpCmd_FlagDefaults(t *testing.T) {
	cmd := newUpCmd()

	// Check default values
	detachFlag := cmd.Flags().Lookup("detach")
	if detachFlag.DefValue != "false" {
		t.Errorf("expected detach default 'false', got '%s'", detachFlag.DefValue)
	}

	noOpenFlag := cmd.Flags().Lookup("no-open")
	if noOpenFlag.DefValue != "false" {
		t.Errorf("expected no-open default 'false', got '%s'", noOpenFlag.DefValue)
	}

	portFlag := cmd.Flags().Lookup("port")
	if portFlag.DefValue != "0" {
		t.Errorf("expected port default '0', got '%s'", portFlag.DefValue)
	}
}

func TestUpCmd_LongDescription(t *testing.T) {
	cmd := newUpCmd()

	if cmd.Long == "" {
		t.Error("expected long description")
	}

	expectedPhrases := []string{
		"local development",
		"cloud.component.yml",
		"cloud.environment.yml",
		"provisions all required resources",
		"file changes",
	}

	for _, phrase := range expectedPhrases {
		if !strings.Contains(strings.ToLower(cmd.Long), strings.ToLower(phrase)) {
			t.Errorf("expected long description to contain '%s'", phrase)
		}
	}
}

func TestUpParseVarFile(t *testing.T) {
	content := `
# Environment variables
API_KEY=secret123
DEBUG=true
DATABASE_URL="postgresql://localhost/db"
`
	vars := make(map[string]string)
	err := upParseVarFile([]byte(content), vars)
	if err != nil {
		t.Fatalf("upParseVarFile failed: %v", err)
	}

	expected := map[string]string{
		"API_KEY":      "secret123",
		"DEBUG":        "true",
		"DATABASE_URL": "postgresql://localhost/db",
	}

	for key, expectedValue := range expected {
		if vars[key] != expectedValue {
			t.Errorf("vars[%q] = %q, expected %q", key, vars[key], expectedValue)
		}
	}
}

func TestUpParseVarFile_EmptyFile(t *testing.T) {
	vars := make(map[string]string)
	err := upParseVarFile([]byte(""), vars)
	if err != nil {
		t.Fatalf("upParseVarFile failed: %v", err)
	}

	if len(vars) != 0 {
		t.Errorf("expected empty vars, got %d", len(vars))
	}
}

func TestUpParseVarFile_OnlyComments(t *testing.T) {
	content := `
# This is a comment
# Another comment

`
	vars := make(map[string]string)
	err := upParseVarFile([]byte(content), vars)
	if err != nil {
		t.Fatalf("upParseVarFile failed: %v", err)
	}

	if len(vars) != 0 {
		t.Errorf("expected empty vars, got %d", len(vars))
	}
}

func TestUpParseVarFile_QuotedValues(t *testing.T) {
	content := `
DOUBLE_QUOTED="value with spaces"
SINGLE_QUOTED='another value'
NO_QUOTES=simple
`
	vars := make(map[string]string)
	err := upParseVarFile([]byte(content), vars)
	if err != nil {
		t.Fatalf("upParseVarFile failed: %v", err)
	}

	expected := map[string]string{
		"DOUBLE_QUOTED": "value with spaces",
		"SINGLE_QUOTED": "another value",
		"NO_QUOTES":     "simple",
	}

	for key, expectedValue := range expected {
		if vars[key] != expectedValue {
			t.Errorf("vars[%q] = %q, expected %q", key, vars[key], expectedValue)
		}
	}
}

func TestUpParseVarFile_TrimWhitespace(t *testing.T) {
	content := `
  KEY1  =  value1  
KEY2=   value2
`
	vars := make(map[string]string)
	err := upParseVarFile([]byte(content), vars)
	if err != nil {
		t.Fatalf("upParseVarFile failed: %v", err)
	}

	// The key should be trimmed
	if _, ok := vars["KEY1"]; !ok {
		t.Error("expected KEY1 to be present")
	}

	// The value should be trimmed
	if vars["KEY1"] != "value1" {
		t.Errorf("expected 'value1', got '%s'", vars["KEY1"])
	}

	if vars["KEY2"] != "value2" {
		t.Errorf("expected 'value2', got '%s'", vars["KEY2"])
	}
}

func TestUpCmd_VariableFlags(t *testing.T) {
	cmd := newUpCmd()

	// The var flag should accept multiple values (array)
	varFlag := cmd.Flags().Lookup("var")
	if varFlag == nil {
		t.Fatal("expected --var flag")
	}

	// Check it's a string array type
	if varFlag.Value.Type() != "stringArray" {
		t.Errorf("expected stringArray type for --var, got %s", varFlag.Value.Type())
	}
}

func TestUpCmd_ComponentFlag(t *testing.T) {
	cmd := newUpCmd()

	compFlag := cmd.Flags().Lookup("component")
	if compFlag == nil {
		t.Fatal("expected --component flag")
	}

	// Default should be empty
	if compFlag.DefValue != "" {
		t.Errorf("expected empty default for --component, got '%s'", compFlag.DefValue)
	}
}

func TestUpCmd_EnvironmentFlag(t *testing.T) {
	cmd := newUpCmd()

	envFlag := cmd.Flags().Lookup("environment")
	if envFlag == nil {
		t.Fatal("expected --environment flag")
	}

	// Default should be empty
	if envFlag.DefValue != "" {
		t.Errorf("expected empty default for --environment, got '%s'", envFlag.DefValue)
	}
}

func TestUpCmd_NameFlag(t *testing.T) {
	cmd := newUpCmd()

	nameFlag := cmd.Flags().Lookup("name")
	if nameFlag == nil {
		t.Fatal("expected --name flag")
	}

	// Default should be empty (auto-generated)
	if nameFlag.DefValue != "" {
		t.Errorf("expected empty default for --name, got '%s'", nameFlag.DefValue)
	}
}
