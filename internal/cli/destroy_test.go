package cli

import (
	"testing"
)

func TestNewDestroyCmd(t *testing.T) {
	cmd := newDestroyCmd()

	if cmd.Use != "destroy" {
		t.Errorf("expected use 'destroy', got '%s'", cmd.Use)
	}

	// Check that subcommands are registered
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Use] = true
	}

	expectedCommands := []string{
		"component <name>",
		"datacenter <name>",
		"environment <name>",
	}

	for _, expected := range expectedCommands {
		if !subcommands[expected] {
			t.Errorf("expected subcommand '%s' not found", expected)
		}
	}
}

func TestDestroyComponentCmd_Flags(t *testing.T) {
	cmd := newDestroyComponentCmd()

	if cmd.Use != "component <name>" {
		t.Errorf("expected use 'component <name>', got '%s'", cmd.Use)
	}

	// Check flags
	flags := []string{"environment", "auto-approve", "target", "backend", "backend-config"}
	for _, flagName := range flags {
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

func TestDestroyDatacenterCmd_Flags(t *testing.T) {
	cmd := newDestroyDatacenterCmd()

	if cmd.Use != "datacenter <name>" {
		t.Errorf("expected use 'datacenter <name>', got '%s'", cmd.Use)
	}

	// Check flags
	flags := []string{"auto-approve", "backend", "backend-config"}
	for _, flagName := range flags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "dc" {
		t.Error("expected alias 'dc'")
	}
}

func TestDestroyEnvironmentCmd_Flags(t *testing.T) {
	cmd := newDestroyEnvironmentCmd()

	if cmd.Use != "environment <name>" {
		t.Errorf("expected use 'environment <name>', got '%s'", cmd.Use)
	}

	// Check flags
	flags := []string{"auto-approve", "backend", "backend-config"}
	for _, flagName := range flags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("expected --%s flag", flagName)
		}
	}

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "env" {
		t.Error("expected alias 'env'")
	}
}
