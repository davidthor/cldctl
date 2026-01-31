package cli

import (
	"testing"
)

func TestNewCreateCmd(t *testing.T) {
	cmd := newCreateCmd()

	if cmd.Use != "create" {
		t.Errorf("expected use 'create', got '%s'", cmd.Use)
	}

	// Check that subcommands are registered
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Use] = true
	}

	expectedCommands := []string{
		"environment <name>",
	}

	for _, expected := range expectedCommands {
		if !subcommands[expected] {
			t.Errorf("expected subcommand '%s' not found", expected)
		}
	}
}

func TestCreateEnvironmentCmd_Flags(t *testing.T) {
	cmd := newCreateEnvironmentCmd()

	if cmd.Use != "environment <name>" {
		t.Errorf("expected use 'environment <name>', got '%s'", cmd.Use)
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

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "env" {
		t.Error("expected alias 'env'")
	}
}
