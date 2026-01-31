package cli

import (
	"testing"
)

func TestNewUpdateCmd(t *testing.T) {
	cmd := newUpdateCmd()

	if cmd.Use != "update" {
		t.Errorf("expected use 'update', got '%s'", cmd.Use)
	}

	// Check that subcommands are registered
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Use] = true
	}

	expectedCommands := []string{
		"environment <name> [config-file]",
	}

	for _, expected := range expectedCommands {
		if !subcommands[expected] {
			t.Errorf("expected subcommand '%s' not found", expected)
		}
	}
}

func TestUpdateEnvironmentCmd_Flags(t *testing.T) {
	cmd := newUpdateEnvironmentCmd()

	if cmd.Use != "environment <name> [config-file]" {
		t.Errorf("expected use 'environment <name> [config-file]', got '%s'", cmd.Use)
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

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "env" {
		t.Error("expected alias 'env'")
	}
}
