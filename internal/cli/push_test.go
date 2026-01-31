package cli

import (
	"testing"
)

func TestNewPushCmd(t *testing.T) {
	cmd := newPushCmd()

	if cmd.Use != "push" {
		t.Errorf("expected use 'push', got '%s'", cmd.Use)
	}

	// Check that subcommands are registered
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Use] = true
	}

	expectedCommands := []string{
		"component <repo:tag>",
		"datacenter <repo:tag>",
	}

	for _, expected := range expectedCommands {
		if !subcommands[expected] {
			t.Errorf("expected subcommand '%s' not found", expected)
		}
	}
}

func TestPushComponentCmd_Flags(t *testing.T) {
	cmd := newPushComponentCmd()

	if cmd.Use != "component <repo:tag>" {
		t.Errorf("expected use 'component <repo:tag>', got '%s'", cmd.Use)
	}

	if cmd.Flags().Lookup("yes") == nil {
		t.Error("expected --yes flag")
	}

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "comp" {
		t.Error("expected alias 'comp'")
	}
}

func TestPushDatacenterCmd_Flags(t *testing.T) {
	cmd := newPushDatacenterCmd()

	if cmd.Use != "datacenter <repo:tag>" {
		t.Errorf("expected use 'datacenter <repo:tag>', got '%s'", cmd.Use)
	}

	if cmd.Flags().Lookup("yes") == nil {
		t.Error("expected --yes flag")
	}

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "dc" {
		t.Error("expected alias 'dc'")
	}
}
