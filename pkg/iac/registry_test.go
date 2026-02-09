package iac

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
)

// mockPlugin is a test implementation of the Plugin interface.
type mockPlugin struct {
	name string
}

func (m *mockPlugin) Name() string {
	return m.name
}

func (m *mockPlugin) Preview(ctx context.Context, opts RunOptions) (*PreviewResult, error) {
	return &PreviewResult{}, nil
}

func (m *mockPlugin) Apply(ctx context.Context, opts RunOptions) (*ApplyResult, error) {
	return &ApplyResult{}, nil
}

func (m *mockPlugin) Destroy(ctx context.Context, opts RunOptions) error {
	return nil
}

func (m *mockPlugin) Refresh(ctx context.Context, opts RunOptions) (*RefreshResult, error) {
	return &RefreshResult{}, nil
}

func (m *mockPlugin) Import(ctx context.Context, opts ImportOptions) (*ImportResult, error) {
	return &ImportResult{}, nil
}

func TestRegistry_Register(t *testing.T) {
	r := &Registry{
		factories: make(map[string]Factory),
	}

	// Register a plugin
	r.Register("test", func() (Plugin, error) {
		return &mockPlugin{name: "test"}, nil
	})

	// Verify it was registered
	if _, ok := r.factories["test"]; !ok {
		t.Error("expected plugin 'test' to be registered")
	}
}

func TestRegistry_Get(t *testing.T) {
	r := &Registry{
		factories: make(map[string]Factory),
	}

	// Register a plugin
	r.Register("test", func() (Plugin, error) {
		return &mockPlugin{name: "test"}, nil
	})

	// Get the plugin
	plugin, err := r.Get("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plugin.Name() != "test" {
		t.Errorf("expected plugin name 'test', got %q", plugin.Name())
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := &Registry{
		factories: make(map[string]Factory),
	}

	// Try to get a non-existent plugin
	_, err := r.Get("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent plugin")
	}

	if err.Error() != "unknown plugin: nonexistent" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRegistry_Get_FactoryError(t *testing.T) {
	r := &Registry{
		factories: make(map[string]Factory),
	}

	// Register a factory that returns an error
	r.Register("failing", func() (Plugin, error) {
		return nil, errors.New("factory error")
	})

	// Get should return the factory error
	_, err := r.Get("failing")
	if err == nil {
		t.Error("expected error from factory")
	}

	if err.Error() != "factory error" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRegistry_List(t *testing.T) {
	r := &Registry{
		factories: make(map[string]Factory),
	}

	// Register multiple plugins
	r.Register("alpha", func() (Plugin, error) { return &mockPlugin{name: "alpha"}, nil })
	r.Register("beta", func() (Plugin, error) { return &mockPlugin{name: "beta"}, nil })
	r.Register("gamma", func() (Plugin, error) { return &mockPlugin{name: "gamma"}, nil })

	// List plugins
	names := r.List()

	// Sort for consistent comparison
	sort.Strings(names)

	expected := []string{"alpha", "beta", "gamma"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d plugins, got %d", len(expected), len(names))
	}

	for i, name := range names {
		if name != expected[i] {
			t.Errorf("expected name %q at index %d, got %q", expected[i], i, name)
		}
	}
}

func TestRegistry_List_Empty(t *testing.T) {
	r := &Registry{
		factories: make(map[string]Factory),
	}

	names := r.List()
	if len(names) != 0 {
		t.Errorf("expected empty list, got %d items", len(names))
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := &Registry{
		factories: make(map[string]Factory),
	}

	var wg sync.WaitGroup
	numGoroutines := 100

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r.Register("test", func() (Plugin, error) {
				return &mockPlugin{name: "test"}, nil
			})
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = r.Get("test")
			r.List()
		}()
	}

	wg.Wait()
}

func TestRegistry_RegisterOverwrite(t *testing.T) {
	r := &Registry{
		factories: make(map[string]Factory),
	}

	// Register initial plugin
	r.Register("test", func() (Plugin, error) {
		return &mockPlugin{name: "original"}, nil
	})

	// Overwrite with new plugin
	r.Register("test", func() (Plugin, error) {
		return &mockPlugin{name: "replacement"}, nil
	})

	// Get should return the replacement
	plugin, err := r.Get("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plugin.Name() != "replacement" {
		t.Errorf("expected plugin name 'replacement', got %q", plugin.Name())
	}
}

func TestDefaultRegistry_Register(t *testing.T) {
	// Save original state and restore after test
	originalFactories := DefaultRegistry.factories
	DefaultRegistry.factories = make(map[string]Factory)
	defer func() {
		DefaultRegistry.factories = originalFactories
	}()

	// Use package-level Register function
	Register("default-test", func() (Plugin, error) {
		return &mockPlugin{name: "default-test"}, nil
	})

	// Use package-level Get function
	plugin, err := Get("default-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if plugin.Name() != "default-test" {
		t.Errorf("expected plugin name 'default-test', got %q", plugin.Name())
	}
}

func TestDefaultRegistry_Get_NotFound(t *testing.T) {
	// Save original state and restore after test
	originalFactories := DefaultRegistry.factories
	DefaultRegistry.factories = make(map[string]Factory)
	defer func() {
		DefaultRegistry.factories = originalFactories
	}()

	_, err := Get("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent plugin")
	}
}
