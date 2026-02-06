package state

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/architect-io/arcctl/pkg/state/backend"
	"github.com/architect-io/arcctl/pkg/state/backend/local"
	"github.com/architect-io/arcctl/pkg/state/types"
)

func TestNewManager(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-manager-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	b, err := local.NewBackend(map[string]string{"path": tmpDir})
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}

	m := NewManager(b)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}

	if m.Backend() != b {
		t.Error("Backend() should return the provided backend")
	}
}

func TestNewManagerFromConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-manager-config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := backend.Config{
		Type:   "local",
		Config: map[string]string{"path": tmpDir},
	}

	m, err := NewManagerFromConfig(config)
	if err != nil {
		t.Fatalf("NewManagerFromConfig failed: %v", err)
	}

	if m == nil {
		t.Fatal("NewManagerFromConfig returned nil")
	}
}

func TestNewManagerFromConfig_InvalidBackend(t *testing.T) {
	config := backend.Config{
		Type: "invalid",
	}

	_, err := NewManagerFromConfig(config)
	if err == nil {
		t.Error("Expected error for invalid backend type")
	}
}

func TestLockScope(t *testing.T) {
	scope := LockScope{
		Datacenter:  "aws-us-east",
		Environment: "production",
		Component:   "api",
		Operation:   "deploy",
		Who:         "user@example.com",
	}

	if scope.Datacenter != "aws-us-east" {
		t.Errorf("Datacenter: got %q", scope.Datacenter)
	}
	if scope.Environment != "production" {
		t.Errorf("Environment: got %q", scope.Environment)
	}
	if scope.Component != "api" {
		t.Errorf("Component: got %q", scope.Component)
	}
	if scope.Operation != "deploy" {
		t.Errorf("Operation: got %q", scope.Operation)
	}
	if scope.Who != "user@example.com" {
		t.Errorf("Who: got %q", scope.Who)
	}
}

// Helper to create a test manager with a local backend
func createTestManager(t *testing.T) (Manager, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	b, err := local.NewBackend(map[string]string{"path": tmpDir})
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create backend: %v", err)
	}

	cleanup := func() {
		os.RemoveAll(tmpDir)
	}

	return NewManager(b), cleanup
}

func TestDatacenterOperations(t *testing.T) {
	m, cleanup := createTestManager(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("save and get datacenter", func(t *testing.T) {
		state := &types.DatacenterState{
			Name:      "aws-us-east",
			Version:   "v1.0.0",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Variables: map[string]string{
				"region": "us-east-1",
			},
		}

		err := m.SaveDatacenter(ctx, state)
		if err != nil {
			t.Fatalf("SaveDatacenter failed: %v", err)
		}

		retrieved, err := m.GetDatacenter(ctx, "aws-us-east")
		if err != nil {
			t.Fatalf("GetDatacenter failed: %v", err)
		}

		if retrieved.Name != "aws-us-east" {
			t.Errorf("Name: got %q, want %q", retrieved.Name, "aws-us-east")
		}
		if retrieved.Version != "v1.0.0" {
			t.Errorf("Version: got %q", retrieved.Version)
		}
		if retrieved.Variables["region"] != "us-east-1" {
			t.Error("Variables not preserved")
		}
	})

	t.Run("list datacenters", func(t *testing.T) {
		// Save another datacenter
		state := &types.DatacenterState{
			Name:    "gcp-us-central",
			Version: "v1.0.0",
		}
		_ = m.SaveDatacenter(ctx, state)

		names, err := m.ListDatacenters(ctx)
		if err != nil {
			t.Fatalf("ListDatacenters failed: %v", err)
		}

		if len(names) < 2 {
			t.Errorf("Expected at least 2 datacenters, got %d", len(names))
		}
	})

	t.Run("delete datacenter", func(t *testing.T) {
		err := m.DeleteDatacenter(ctx, "aws-us-east")
		if err != nil {
			t.Fatalf("DeleteDatacenter failed: %v", err)
		}

		_, err = m.GetDatacenter(ctx, "aws-us-east")
		if err == nil {
			t.Error("Expected error getting deleted datacenter")
		}
	})

	t.Run("get nonexistent datacenter", func(t *testing.T) {
		_, err := m.GetDatacenter(ctx, "nonexistent")
		if err == nil {
			t.Error("Expected error for nonexistent datacenter")
		}
	})
}

func TestEnvironmentOperations(t *testing.T) {
	m, cleanup := createTestManager(t)
	defer cleanup()

	ctx := context.Background()
	dc := "aws-us-east"

	// Create datacenter first
	_ = m.SaveDatacenter(ctx, &types.DatacenterState{Name: dc})

	t.Run("save and get environment", func(t *testing.T) {
		now := time.Now()
		state := &types.EnvironmentState{
			Name:       "production",
			Datacenter: dc,
			Status:     types.EnvironmentStatusReady,
			Components: map[string]*types.ComponentState{
				"api": {
					Name:   "api",
					Status: types.ResourceStatusReady,
				},
			},
			CreatedAt: now,
			UpdatedAt: now,
		}

		err := m.SaveEnvironment(ctx, dc, state)
		if err != nil {
			t.Fatalf("SaveEnvironment failed: %v", err)
		}

		retrieved, err := m.GetEnvironment(ctx, dc, "production")
		if err != nil {
			t.Fatalf("GetEnvironment failed: %v", err)
		}

		if retrieved.Name != "production" {
			t.Errorf("Name: got %q, want %q", retrieved.Name, "production")
		}
		if retrieved.Datacenter != dc {
			t.Errorf("Datacenter: got %q", retrieved.Datacenter)
		}
		if len(retrieved.Components) != 1 {
			t.Errorf("Expected 1 component, got %d", len(retrieved.Components))
		}
	})

	t.Run("list environments", func(t *testing.T) {
		// Save another environment
		state := &types.EnvironmentState{
			Name:       "staging",
			Datacenter: dc,
		}
		_ = m.SaveEnvironment(ctx, dc, state)

		refs, err := m.ListEnvironments(ctx, dc)
		if err != nil {
			t.Fatalf("ListEnvironments failed: %v", err)
		}

		if len(refs) < 2 {
			t.Errorf("Expected at least 2 environments, got %d", len(refs))
		}

		// Verify refs contain expected data
		found := false
		for _, ref := range refs {
			if ref.Name == "production" {
				found = true
				if ref.Datacenter != dc {
					t.Errorf("Datacenter in ref: got %q", ref.Datacenter)
				}
			}
		}
		if !found {
			t.Error("Expected to find 'production' in refs")
		}
	})

	t.Run("environments scoped to datacenter", func(t *testing.T) {
		// Create another datacenter with its own environment
		dc2 := "gcp-us-central"
		_ = m.SaveDatacenter(ctx, &types.DatacenterState{Name: dc2})
		_ = m.SaveEnvironment(ctx, dc2, &types.EnvironmentState{
			Name:       "staging",
			Datacenter: dc2,
		})

		// Listing environments for dc should not include dc2's environments
		refs, err := m.ListEnvironments(ctx, dc)
		if err != nil {
			t.Fatalf("ListEnvironments failed: %v", err)
		}

		for _, ref := range refs {
			if ref.Datacenter != dc {
				t.Errorf("Found environment from wrong datacenter: %q (expected %q)", ref.Datacenter, dc)
			}
		}

		// Both datacenters can have "staging" without collision
		env1, err := m.GetEnvironment(ctx, dc, "staging")
		if err != nil {
			t.Fatalf("GetEnvironment dc failed: %v", err)
		}
		env2, err := m.GetEnvironment(ctx, dc2, "staging")
		if err != nil {
			t.Fatalf("GetEnvironment dc2 failed: %v", err)
		}
		if env1.Datacenter == env2.Datacenter {
			t.Error("Environments should belong to different datacenters")
		}
	})

	t.Run("delete environment", func(t *testing.T) {
		err := m.DeleteEnvironment(ctx, dc, "staging")
		if err != nil {
			t.Fatalf("DeleteEnvironment failed: %v", err)
		}

		_, err = m.GetEnvironment(ctx, dc, "staging")
		if err == nil {
			t.Error("Expected error getting deleted environment")
		}
	})

	t.Run("get nonexistent environment", func(t *testing.T) {
		_, err := m.GetEnvironment(ctx, dc, "nonexistent")
		if err == nil {
			t.Error("Expected error for nonexistent environment")
		}
	})
}

func TestComponentOperations(t *testing.T) {
	m, cleanup := createTestManager(t)
	defer cleanup()

	ctx := context.Background()
	dc := "test-dc"

	// First create datacenter and environment
	_ = m.SaveDatacenter(ctx, &types.DatacenterState{Name: dc})
	envState := &types.EnvironmentState{
		Name:       "test-env",
		Datacenter: dc,
	}
	_ = m.SaveEnvironment(ctx, dc, envState)

	t.Run("save and get component", func(t *testing.T) {
		state := &types.ComponentState{
			Name:   "api",
			Status: types.ResourceStatusReady,
			Resources: map[string]*types.ResourceState{
				"main": {
					Name:   "main",
					Type:   "deployment",
					Status: types.ResourceStatusReady,
				},
			},
			UpdatedAt: time.Now(),
		}

		err := m.SaveComponent(ctx, dc, "test-env", state)
		if err != nil {
			t.Fatalf("SaveComponent failed: %v", err)
		}

		retrieved, err := m.GetComponent(ctx, dc, "test-env", "api")
		if err != nil {
			t.Fatalf("GetComponent failed: %v", err)
		}

		if retrieved.Name != "api" {
			t.Errorf("Name: got %q, want %q", retrieved.Name, "api")
		}
		if len(retrieved.Resources) != 1 {
			t.Errorf("Expected 1 resource, got %d", len(retrieved.Resources))
		}
	})

	t.Run("delete component", func(t *testing.T) {
		err := m.DeleteComponent(ctx, dc, "test-env", "api")
		if err != nil {
			t.Fatalf("DeleteComponent failed: %v", err)
		}

		_, err = m.GetComponent(ctx, dc, "test-env", "api")
		if err == nil {
			t.Error("Expected error getting deleted component")
		}
	})

	t.Run("get nonexistent component", func(t *testing.T) {
		_, err := m.GetComponent(ctx, dc, "test-env", "nonexistent")
		if err == nil {
			t.Error("Expected error for nonexistent component")
		}
	})
}

func TestResourceOperations(t *testing.T) {
	m, cleanup := createTestManager(t)
	defer cleanup()

	ctx := context.Background()
	dc := "test-dc"

	// Create datacenter, environment and component
	_ = m.SaveDatacenter(ctx, &types.DatacenterState{Name: dc})
	_ = m.SaveEnvironment(ctx, dc, &types.EnvironmentState{Name: "test-env", Datacenter: dc})
	_ = m.SaveComponent(ctx, dc, "test-env", &types.ComponentState{Name: "api"})

	t.Run("save and get resource", func(t *testing.T) {
		state := &types.ResourceState{
			Name:      "main",
			Type:      "deployment",
			Component: "api",
			Status:    types.ResourceStatusReady,
			Inputs: map[string]interface{}{
				"image":    "myapp:v1",
				"replicas": 3,
			},
			Outputs: map[string]interface{}{
				"url": "http://api.example.com",
			},
			UpdatedAt: time.Now(),
		}

		err := m.SaveResource(ctx, dc, "test-env", "api", state)
		if err != nil {
			t.Fatalf("SaveResource failed: %v", err)
		}

		// GetResource now uses type-qualified key
		retrieved, err := m.GetResource(ctx, dc, "test-env", "api", "deployment.main")
		if err != nil {
			t.Fatalf("GetResource failed: %v", err)
		}

		if retrieved.Name != "main" {
			t.Errorf("Name: got %q, want %q", retrieved.Name, "main")
		}
		if retrieved.Type != "deployment" {
			t.Errorf("Type: got %q", retrieved.Type)
		}
		if retrieved.Status != types.ResourceStatusReady {
			t.Errorf("Status: got %q", retrieved.Status)
		}
		if retrieved.Inputs["image"] != "myapp:v1" {
			t.Error("Inputs not preserved correctly")
		}
	})

	t.Run("delete resource", func(t *testing.T) {
		err := m.DeleteResource(ctx, dc, "test-env", "api", "deployment.main")
		if err != nil {
			t.Fatalf("DeleteResource failed: %v", err)
		}

		_, err = m.GetResource(ctx, dc, "test-env", "api", "deployment.main")
		if err == nil {
			t.Error("Expected error getting deleted resource")
		}
	})

	t.Run("get nonexistent resource", func(t *testing.T) {
		_, err := m.GetResource(ctx, dc, "test-env", "api", "nonexistent")
		if err == nil {
			t.Error("Expected error for nonexistent resource")
		}
	})
}

func TestPathHelpers(t *testing.T) {
	tests := []struct {
		name     string
		fn       func() string
		expected string
	}{
		{
			name:     "datacenterPath",
			fn:       func() string { return datacenterPath("aws-east") },
			expected: "datacenters/aws-east/datacenter.state.json",
		},
		{
			name:     "environmentPath",
			fn:       func() string { return environmentPath("aws-east", "production") },
			expected: "datacenters/aws-east/environments/production/environment.state.json",
		},
		{
			name:     "componentPath",
			fn:       func() string { return componentPath("aws-east", "prod", "api") },
			expected: "datacenters/aws-east/environments/prod/components/api/component.state.json",
		},
		{
			name:     "resourcePath",
			fn:       func() string { return resourcePath("aws-east", "prod", "api", "main") },
			expected: "datacenters/aws-east/environments/prod/components/api/resources/main.state.json",
		},
		{
			name:     "resourcePath type-qualified",
			fn:       func() string { return resourcePath("aws-east", "prod", "api", "deployment.main") },
			expected: "datacenters/aws-east/environments/prod/components/api/resources/deployment.main.state.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.fn()
			if result != tt.expected {
				t.Errorf("got %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSplitPath(t *testing.T) {
	tests := []struct {
		path     string
		expected []string
	}{
		{"datacenters/aws/environments/prod/environment.state.json", []string{"datacenters", "aws", "environments", "prod", "environment.state.json"}},
		{"datacenters/aws/datacenter.state.json", []string{"datacenters", "aws", "datacenter.state.json"}},
		{"", []string{}},
		{"single", []string{"single"}},
		{"a/b/c/d", []string{"a", "b", "c", "d"}},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := splitPath(tt.path)
			if len(result) != len(tt.expected) {
				t.Errorf("len: got %d, want %d", len(result), len(tt.expected))
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("part %d: got %q, want %q", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestLocking(t *testing.T) {
	m, cleanup := createTestManager(t)
	defer cleanup()

	ctx := context.Background()

	scope := LockScope{
		Datacenter:  "test-dc",
		Environment: "test-env",
		Component:   "api",
		Operation:   "deploy",
		Who:         "test-user",
	}

	lock, err := m.Lock(ctx, scope)
	if err != nil {
		t.Fatalf("Lock failed: %v", err)
	}

	// Lock should be returned
	if lock != nil {
		err = lock.Unlock(ctx)
		if err != nil {
			t.Errorf("Unlock failed: %v", err)
		}
	}
}

func TestLocking_EnvironmentOnly(t *testing.T) {
	m, cleanup := createTestManager(t)
	defer cleanup()

	ctx := context.Background()

	scope := LockScope{
		Datacenter:  "test-dc",
		Environment: "test-env",
		Operation:   "destroy",
		Who:         "test-user",
	}

	lock, err := m.Lock(ctx, scope)
	if err != nil {
		t.Fatalf("Lock failed: %v", err)
	}

	if lock != nil {
		_ = lock.Unlock(ctx)
	}
}
