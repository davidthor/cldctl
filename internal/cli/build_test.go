package cli

import (
	"os"
	"path/filepath"
	"testing"
)

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

// Helper to create a temporary datacenter directory with datacenter.hcl
func createTempDatacenter(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "datacenter.hcl"), []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to create datacenter.hcl: %v", err)
	}
	return dir
}

func TestNewBuildCmd(t *testing.T) {
	cmd := newBuildCmd()

	if cmd.Use != "build" {
		t.Errorf("expected use 'build', got '%s'", cmd.Use)
	}

	// Check that subcommands are registered
	subcommands := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		subcommands[sub.Use] = true
	}

	expectedCommands := []string{
		"component [path]",
		"datacenter [path]",
	}

	for _, expected := range expectedCommands {
		if !subcommands[expected] {
			t.Errorf("expected subcommand '%s' not found", expected)
		}
	}
}

func TestBuildComponentCmd_Flags(t *testing.T) {
	cmd := newBuildComponentCmd()

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

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "comp" {
		t.Error("expected alias 'comp'")
	}
}

func TestBuildDatacenterCmd_Flags(t *testing.T) {
	cmd := newBuildDatacenterCmd()

	// Check required flags
	tagFlag := cmd.Flags().Lookup("tag")
	if tagFlag == nil {
		t.Error("expected --tag flag")
	}

	// Check optional flags
	flags := []string{"module-tag", "file", "dry-run"}
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

	// Check aliases
	if len(cmd.Aliases) == 0 || cmd.Aliases[0] != "dc" {
		t.Error("expected alias 'dc'")
	}
}
