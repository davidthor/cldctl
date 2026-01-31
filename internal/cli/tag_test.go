package cli

import (
	"testing"
)

func TestNewTagCmd(t *testing.T) {
	cmd := newTagCmd()

	if cmd.Use != "tag" {
		t.Errorf("expected use 'tag', got '%s'", cmd.Use)
	}

	// Check that subcommands are registered
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Use] = true
	}

	expectedCommands := []string{
		"component <source> <target>",
		"datacenter <source> <target>",
	}

	for _, expected := range expectedCommands {
		if !subcommands[expected] {
			t.Errorf("expected subcommand '%s' not found", expected)
		}
	}
}

func TestTagComponentCmd_Flags(t *testing.T) {
	cmd := newTagComponentCmd()

	if cmd.Use != "component <source> <target>" {
		t.Errorf("expected use 'component <source> <target>', got '%s'", cmd.Use)
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

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "comp" {
		t.Error("expected alias 'comp'")
	}
}

func TestTagDatacenterCmd_Flags(t *testing.T) {
	cmd := newTagDatacenterCmd()

	if cmd.Use != "datacenter <source> <target>" {
		t.Errorf("expected use 'datacenter <source> <target>', got '%s'", cmd.Use)
	}

	// Check flags
	if cmd.Flags().Lookup("module-tag") == nil {
		t.Error("expected --module-tag flag")
	}
	if cmd.Flags().Lookup("yes") == nil {
		t.Error("expected --yes flag")
	}
	if cmd.Flags().ShorthandLookup("y") == nil {
		t.Error("expected -y shorthand for --yes")
	}

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "dc" {
		t.Error("expected alias 'dc'")
	}
}
