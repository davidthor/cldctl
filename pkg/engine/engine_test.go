package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/architect-io/arcctl/pkg/engine/executor"
	"github.com/architect-io/arcctl/pkg/engine/planner"
	"github.com/architect-io/arcctl/pkg/iac"
	"github.com/architect-io/arcctl/pkg/state"
	"github.com/architect-io/arcctl/pkg/state/backend"
	"github.com/architect-io/arcctl/pkg/state/types"
)

// mockStateManager implements state.Manager for testing
type mockStateManager struct {
	environments map[string]*types.EnvironmentState
	saveErr      error
	getErr       error
}

func newMockStateManager() *mockStateManager {
	return &mockStateManager{
		environments: make(map[string]*types.EnvironmentState),
	}
}

func (m *mockStateManager) GetEnvironment(ctx context.Context, name string) (*types.EnvironmentState, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if env, ok := m.environments[name]; ok {
		return env, nil
	}
	return nil, backend.ErrNotFound
}

func (m *mockStateManager) SaveEnvironment(ctx context.Context, s *types.EnvironmentState) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.environments[s.Name] = s
	return nil
}

func (m *mockStateManager) ListEnvironments(ctx context.Context) ([]types.EnvironmentRef, error) {
	var refs []types.EnvironmentRef
	for name := range m.environments {
		refs = append(refs, types.EnvironmentRef{Name: name})
	}
	return refs, nil
}

func (m *mockStateManager) DeleteEnvironment(ctx context.Context, name string) error {
	delete(m.environments, name)
	return nil
}

func (m *mockStateManager) GetDatacenter(ctx context.Context, name string) (*types.DatacenterState, error) {
	return nil, nil
}

func (m *mockStateManager) SaveDatacenter(ctx context.Context, s *types.DatacenterState) error {
	return nil
}

func (m *mockStateManager) DeleteDatacenter(ctx context.Context, name string) error {
	return nil
}

func (m *mockStateManager) ListDatacenters(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockStateManager) GetComponent(ctx context.Context, env, name string) (*types.ComponentState, error) {
	return nil, nil
}

func (m *mockStateManager) SaveComponent(ctx context.Context, env string, s *types.ComponentState) error {
	return nil
}

func (m *mockStateManager) DeleteComponent(ctx context.Context, env, name string) error {
	return nil
}

func (m *mockStateManager) GetResource(ctx context.Context, env, comp, name string) (*types.ResourceState, error) {
	return nil, nil
}

func (m *mockStateManager) SaveResource(ctx context.Context, env, comp string, s *types.ResourceState) error {
	return nil
}

func (m *mockStateManager) DeleteResource(ctx context.Context, env, comp, name string) error {
	return nil
}

func (m *mockStateManager) Lock(ctx context.Context, scope state.LockScope) (backend.Lock, error) {
	return nil, nil
}

func (m *mockStateManager) Backend() backend.Backend {
	return nil
}

func TestNewEngine(t *testing.T) {
	sm := newMockStateManager()
	registry := iac.DefaultRegistry

	engine := NewEngine(sm, registry)

	if engine == nil {
		t.Fatal("NewEngine returned nil")
	}
	if engine.stateManager == nil {
		t.Error("stateManager is nil")
	}
	if engine.iacRegistry == nil {
		t.Error("iacRegistry is nil")
	}
	if engine.compLoader == nil {
		t.Error("compLoader is nil")
	}
	if engine.envLoader == nil {
		t.Error("envLoader is nil")
	}
}

func TestDeployOptions(t *testing.T) {
	opts := DeployOptions{
		Environment: "production",
		Datacenter:  "aws-us-east",
		Components: map[string]string{
			"api": "./components/api",
			"web": "./components/web",
		},
		Variables: map[string]map[string]interface{}{
			"api": {"replicas": 3},
		},
		DryRun:      true,
		AutoApprove: false,
		Parallelism: 5,
	}

	if opts.Environment != "production" {
		t.Errorf("Environment: got %q", opts.Environment)
	}
	if opts.Datacenter != "aws-us-east" {
		t.Errorf("Datacenter: got %q", opts.Datacenter)
	}
	if len(opts.Components) != 2 {
		t.Errorf("Components count: got %d", len(opts.Components))
	}
	if !opts.DryRun {
		t.Error("DryRun should be true")
	}
	if opts.Parallelism != 5 {
		t.Errorf("Parallelism: got %d", opts.Parallelism)
	}
}

func TestDeployResult(t *testing.T) {
	result := &DeployResult{
		Success: true,
		Plan: &planner.Plan{
			Environment: "test",
			ToCreate:    2,
		},
		Execution: &executor.ExecutionResult{
			Success: true,
			Created: 2,
		},
		Duration: 5 * time.Second,
	}

	if !result.Success {
		t.Error("Success should be true")
	}
	if result.Plan.ToCreate != 2 {
		t.Errorf("Plan.ToCreate: got %d", result.Plan.ToCreate)
	}
	if result.Execution.Created != 2 {
		t.Errorf("Execution.Created: got %d", result.Execution.Created)
	}
	if result.Duration != 5*time.Second {
		t.Errorf("Duration: got %v", result.Duration)
	}
}

func TestDestroyOptions(t *testing.T) {
	var buf bytes.Buffer
	opts := DestroyOptions{
		Environment: "staging",
		Output:      &buf,
		DryRun:      true,
		AutoApprove: false,
	}

	if opts.Environment != "staging" {
		t.Errorf("Environment: got %q", opts.Environment)
	}
	if opts.Output == nil {
		t.Error("Output should not be nil")
	}
	if !opts.DryRun {
		t.Error("DryRun should be true")
	}
}

func TestDestroyResult(t *testing.T) {
	result := &DestroyResult{
		Success: true,
		Plan: &planner.Plan{
			Environment: "test",
			ToDelete:    5,
		},
		Execution: &executor.ExecutionResult{
			Success: true,
			Deleted: 5,
		},
		Duration: 10 * time.Second,
	}

	if !result.Success {
		t.Error("Success should be true")
	}
	if result.Plan.ToDelete != 5 {
		t.Errorf("Plan.ToDelete: got %d", result.Plan.ToDelete)
	}
	if result.Execution.Deleted != 5 {
		t.Errorf("Execution.Deleted: got %d", result.Execution.Deleted)
	}
}

func TestPrintPlanSummary(t *testing.T) {
	sm := newMockStateManager()
	registry := iac.DefaultRegistry
	engine := NewEngine(sm, registry)

	t.Run("empty plan", func(t *testing.T) {
		var buf bytes.Buffer
		plan := &planner.Plan{
			Environment: "test-env",
			Datacenter:  "test-dc",
			ToCreate:    0,
			ToUpdate:    0,
			ToDelete:    0,
		}

		engine.printPlanSummary(&buf, plan)

		output := buf.String()
		if !bytes.Contains([]byte(output), []byte("No changes required")) {
			t.Errorf("Expected 'No changes required' in output, got: %s", output)
		}
	})

	t.Run("plan with changes", func(t *testing.T) {
		var buf bytes.Buffer
		plan := &planner.Plan{
			Environment: "test-env",
			Datacenter:  "test-dc",
			ToCreate:    2,
			ToUpdate:    1,
			ToDelete:    1,
			NoChange:    3,
			Changes: []*planner.ResourceChange{
				{Action: planner.ActionCreate, Node: nil},
				{Action: planner.ActionUpdate, Node: nil},
				{Action: planner.ActionDelete, Node: nil},
			},
		}

		engine.printPlanSummary(&buf, plan)

		output := buf.String()
		if !bytes.Contains([]byte(output), []byte("Environment: test-env")) {
			t.Errorf("Expected 'Environment: test-env' in output, got: %s", output)
		}
		if !bytes.Contains([]byte(output), []byte("2 to create")) {
			t.Errorf("Expected '2 to create' in output, got: %s", output)
		}
	})
}

func TestPrintDestroyPlanSummary(t *testing.T) {
	sm := newMockStateManager()
	registry := iac.DefaultRegistry
	engine := NewEngine(sm, registry)

	t.Run("empty destroy plan", func(t *testing.T) {
		var buf bytes.Buffer
		plan := &planner.Plan{
			Environment: "test-env",
			ToDelete:    0,
		}

		engine.printDestroyPlanSummary(&buf, plan)

		output := buf.String()
		if !bytes.Contains([]byte(output), []byte("No resources to destroy")) {
			t.Errorf("Expected 'No resources to destroy' in output, got: %s", output)
		}
	})

	t.Run("destroy plan with resources", func(t *testing.T) {
		var buf bytes.Buffer
		plan := &planner.Plan{
			Environment: "test-env",
			ToDelete:    3,
			Changes: []*planner.ResourceChange{
				{Action: planner.ActionDelete, Node: nil},
				{Action: planner.ActionDelete, Node: nil},
				{Action: planner.ActionDelete, Node: nil},
			},
		}

		engine.printDestroyPlanSummary(&buf, plan)

		output := buf.String()
		if !bytes.Contains([]byte(output), []byte("3 resources to destroy")) {
			t.Errorf("Expected '3 resources to destroy' in output, got: %s", output)
		}
	})
}

func TestDestroy_EnvironmentNotFound(t *testing.T) {
	sm := newMockStateManager()
	registry := iac.DefaultRegistry
	engine := NewEngine(sm, registry)

	opts := DestroyOptions{
		Environment: "nonexistent",
	}

	_, err := engine.Destroy(context.Background(), opts)
	if err == nil {
		t.Error("Expected error for nonexistent environment")
	}
}

// mockOCIClient implements OCIClient for testing.
type mockOCIClient struct {
	pullFn       func(ctx context.Context, reference string, destDir string) error
	pullConfigFn func(ctx context.Context, reference string) ([]byte, error)
	existsFn     func(ctx context.Context, reference string) (bool, error)
}

func (m *mockOCIClient) Pull(ctx context.Context, reference string, destDir string) error {
	if m.pullFn != nil {
		return m.pullFn(ctx, reference, destDir)
	}
	return nil
}

func (m *mockOCIClient) PullConfig(ctx context.Context, reference string) ([]byte, error) {
	if m.pullConfigFn != nil {
		return m.pullConfigFn(ctx, reference)
	}
	return []byte("test-config"), nil
}

func (m *mockOCIClient) Exists(ctx context.Context, reference string) (bool, error) {
	if m.existsFn != nil {
		return m.existsFn(ctx, reference)
	}
	return true, nil
}

// minimalDatacenterHCL is a minimal valid datacenter configuration for testing.
const minimalDatacenterHCL = `
environment {
  deployment {
    module "container" {
      plugin = "native"
      build  = "./modules/test"
      inputs = {
        name = node.name
      }
    }

    outputs = {
      id = module.container.id
    }
  }
}
`

func TestLoadDatacenterConfig_LocalPath(t *testing.T) {
	tmpDir := t.TempDir()
	dcFile := filepath.Join(tmpDir, "datacenter.hcl")
	if err := os.WriteFile(dcFile, []byte(minimalDatacenterHCL), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	sm := newMockStateManager()
	eng := NewEngine(sm, iac.DefaultRegistry)

	dc, err := eng.loadDatacenterConfig(dcFile)
	if err != nil {
		t.Fatalf("loadDatacenterConfig failed: %v", err)
	}
	if dc == nil {
		t.Fatal("expected non-nil datacenter")
	}
}

func TestLoadDatacenterConfig_LocalDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	dcFile := filepath.Join(tmpDir, "datacenter.dc")
	if err := os.WriteFile(dcFile, []byte(minimalDatacenterHCL), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	sm := newMockStateManager()
	eng := NewEngine(sm, iac.DefaultRegistry)

	dc, err := eng.loadDatacenterConfig(tmpDir)
	if err != nil {
		t.Fatalf("loadDatacenterConfig failed: %v", err)
	}
	if dc == nil {
		t.Fatal("expected non-nil datacenter")
	}
}

func TestLoadDatacenterConfig_LocalDirectoryHCL(t *testing.T) {
	tmpDir := t.TempDir()
	// Only write datacenter.hcl, not datacenter.dc
	dcFile := filepath.Join(tmpDir, "datacenter.hcl")
	if err := os.WriteFile(dcFile, []byte(minimalDatacenterHCL), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	sm := newMockStateManager()
	eng := NewEngine(sm, iac.DefaultRegistry)

	dc, err := eng.loadDatacenterConfig(tmpDir)
	if err != nil {
		t.Fatalf("loadDatacenterConfig failed: %v", err)
	}
	if dc == nil {
		t.Fatal("expected non-nil datacenter")
	}
}

func TestLoadDatacenterConfig_LocalPathNotFound(t *testing.T) {
	sm := newMockStateManager()
	eng := NewEngine(sm, iac.DefaultRegistry)

	_, err := eng.loadDatacenterConfig("/nonexistent/path/datacenter.hcl")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestLoadDatacenterConfig_OCIReference(t *testing.T) {
	tmpDir := t.TempDir()

	sm := newMockStateManager()
	eng := NewEngine(sm, iac.DefaultRegistry)
	eng.cacheDir = tmpDir

	ociMock := &mockOCIClient{
		pullFn: func(ctx context.Context, reference string, destDir string) error {
			// Simulate pulling by writing a datacenter file
			return os.WriteFile(filepath.Join(destDir, "datacenter.dc"), []byte(minimalDatacenterHCL), 0644)
		},
	}
	eng.ociClient = ociMock

	dc, err := eng.loadDatacenterConfig("ghcr.io/myorg/mydc:v1")
	if err != nil {
		t.Fatalf("loadDatacenterConfig OCI failed: %v", err)
	}
	if dc == nil {
		t.Fatal("expected non-nil datacenter from OCI")
	}
}

func TestLoadDatacenterConfig_OCIReferenceCached(t *testing.T) {
	tmpDir := t.TempDir()

	sm := newMockStateManager()
	eng := NewEngine(sm, iac.DefaultRegistry)
	eng.cacheDir = tmpDir

	pullCount := 0
	ociMock := &mockOCIClient{
		pullFn: func(ctx context.Context, reference string, destDir string) error {
			pullCount++
			return os.WriteFile(filepath.Join(destDir, "datacenter.dc"), []byte(minimalDatacenterHCL), 0644)
		},
		pullConfigFn: func(ctx context.Context, reference string) ([]byte, error) {
			return []byte("same-digest"), nil
		},
		existsFn: func(ctx context.Context, reference string) (bool, error) {
			return true, nil
		},
	}
	eng.ociClient = ociMock

	// First call - should pull
	_, err := eng.loadDatacenterConfig("ghcr.io/myorg/mydc:v1")
	if err != nil {
		t.Fatalf("first load failed: %v", err)
	}
	if pullCount != 1 {
		t.Fatalf("expected 1 pull, got %d", pullCount)
	}

	// Second call - should use cache (digest matches)
	_, err = eng.loadDatacenterConfig("ghcr.io/myorg/mydc:v1")
	if err != nil {
		t.Fatalf("second load failed: %v", err)
	}
	if pullCount != 1 {
		t.Fatalf("expected still 1 pull (cached), got %d", pullCount)
	}
}

func TestLoadDatacenterConfig_OCIReferenceNoFile(t *testing.T) {
	tmpDir := t.TempDir()

	sm := newMockStateManager()
	eng := NewEngine(sm, iac.DefaultRegistry)
	eng.cacheDir = tmpDir

	ociMock := &mockOCIClient{
		pullFn: func(ctx context.Context, reference string, destDir string) error {
			// Pull succeeds but no datacenter file is created
			return nil
		},
	}
	eng.ociClient = ociMock

	_, err := eng.loadDatacenterConfig("ghcr.io/myorg/mydc:v1")
	if err == nil {
		t.Fatal("expected error when no datacenter file in artifact")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("no datacenter.dc or datacenter.hcl")) {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestLoadDatacenterConfig_OCIPullFailure(t *testing.T) {
	tmpDir := t.TempDir()

	sm := newMockStateManager()
	eng := NewEngine(sm, iac.DefaultRegistry)
	eng.cacheDir = tmpDir

	ociMock := &mockOCIClient{
		pullFn: func(ctx context.Context, reference string, destDir string) error {
			return fmt.Errorf("network error: connection refused")
		},
	}
	eng.ociClient = ociMock

	_, err := eng.loadDatacenterConfig("ghcr.io/myorg/mydc:v1")
	if err == nil {
		t.Fatal("expected error on pull failure")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("failed to pull datacenter")) {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestDestroy_DryRun(t *testing.T) {
	sm := newMockStateManager()
	// Pre-populate with an environment
	sm.environments["test-env"] = &types.EnvironmentState{
		Name:       "test-env",
		Datacenter: "test-dc",
		Components: map[string]*types.ComponentState{
			"api": {
				Name: "api",
				Resources: map[string]*types.ResourceState{
					"main": {
						Name:      "main",
						Type:      "deployment",
						Component: "api",
					},
				},
			},
		},
	}

	registry := iac.DefaultRegistry
	engine := NewEngine(sm, registry)

	var buf bytes.Buffer
	opts := DestroyOptions{
		Environment: "test-env",
		Output:      &buf,
		DryRun:      true,
	}

	result, err := engine.Destroy(context.Background(), opts)
	if err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}

	if !result.Success {
		t.Error("Dry run should succeed")
	}

	// Environment should still exist after dry run
	if _, exists := sm.environments["test-env"]; !exists {
		t.Error("Environment should still exist after dry run")
	}
}
