package cli

import (
	"testing"
)

func TestNewGetCmd(t *testing.T) {
	cmd := newGetCmd()

	if cmd.Use != "get" {
		t.Errorf("expected use 'get', got '%s'", cmd.Use)
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

func TestGetComponentCmd_Flags(t *testing.T) {
	cmd := newGetComponentCmd()

	if cmd.Use != "component <name>" {
		t.Errorf("expected use 'component <name>', got '%s'", cmd.Use)
	}

	// Check flags
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

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "comp" {
		t.Error("expected alias 'comp'")
	}
}

func TestGetDatacenterCmd_Flags(t *testing.T) {
	cmd := newGetDatacenterCmd()

	if cmd.Use != "datacenter <name>" {
		t.Errorf("expected use 'datacenter <name>', got '%s'", cmd.Use)
	}

	// Check flags
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

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "dc" {
		t.Error("expected alias 'dc'")
	}
}

func TestGetEnvironmentCmd_Flags(t *testing.T) {
	cmd := newGetEnvironmentCmd()

	if cmd.Use != "environment <name>" {
		t.Errorf("expected use 'environment <name>', got '%s'", cmd.Use)
	}

	// Check flags
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

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "env" {
		t.Error("expected alias 'env'")
	}
}
