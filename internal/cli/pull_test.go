package cli

import (
	"testing"
)

func TestNewPullCmd(t *testing.T) {
	cmd := newPullCmd()

	if cmd.Use != "pull" {
		t.Errorf("expected use 'pull', got '%s'", cmd.Use)
	}

	// Check that subcommands are registered
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Use] = true
	}

	expectedCommands := []string{
		"component <repo:tag>",
	}

	for _, expected := range expectedCommands {
		if !subcommands[expected] {
			t.Errorf("expected subcommand '%s' not found", expected)
		}
	}
}

func TestPullComponentCmd_Flags(t *testing.T) {
	cmd := newPullComponentCmd()

	if cmd.Use != "component <repo:tag>" {
		t.Errorf("expected use 'component <repo:tag>', got '%s'", cmd.Use)
	}

	// Check flags
	if cmd.Flags().Lookup("quiet") == nil {
		t.Error("expected --quiet flag")
	}
	if cmd.Flags().ShorthandLookup("q") == nil {
		t.Error("expected -q shorthand for --quiet")
	}

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "comp" {
		t.Error("expected alias 'comp'")
	}
}
