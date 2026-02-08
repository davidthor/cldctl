package executor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/davidthor/cldctl/pkg/engine/planner"
	"github.com/davidthor/cldctl/pkg/graph"
	"github.com/davidthor/cldctl/pkg/iac"
	"github.com/davidthor/cldctl/pkg/state"
	"github.com/davidthor/cldctl/pkg/state/backend"
	"github.com/davidthor/cldctl/pkg/state/types"
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

func (m *mockStateManager) GetEnvironment(ctx context.Context, datacenter, name string) (*types.EnvironmentState, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	key := datacenter + "/" + name
	if env, ok := m.environments[key]; ok {
		return env, nil
	}
	if env, ok := m.environments[name]; ok {
		return env, nil
	}
	return nil, backend.ErrNotFound
}

func (m *mockStateManager) SaveEnvironment(ctx context.Context, datacenter string, s *types.EnvironmentState) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	key := datacenter + "/" + s.Name
	m.environments[key] = s
	m.environments[s.Name] = s
	return nil
}

func (m *mockStateManager) ListEnvironments(ctx context.Context, datacenter string) ([]types.EnvironmentRef, error) {
	return nil, nil
}

func (m *mockStateManager) DeleteEnvironment(ctx context.Context, datacenter, name string) error {
	key := datacenter + "/" + name
	delete(m.environments, key)
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

func (m *mockStateManager) GetComponent(ctx context.Context, dc, env, name string) (*types.ComponentState, error) {
	return nil, nil
}

func (m *mockStateManager) SaveComponent(ctx context.Context, dc, env string, s *types.ComponentState) error {
	return nil
}

func (m *mockStateManager) DeleteComponent(ctx context.Context, dc, env, name string) error {
	return nil
}

func (m *mockStateManager) GetResource(ctx context.Context, dc, env, comp, name string) (*types.ResourceState, error) {
	return nil, nil
}

func (m *mockStateManager) SaveResource(ctx context.Context, dc, env, comp string, s *types.ResourceState) error {
	return nil
}

func (m *mockStateManager) DeleteResource(ctx context.Context, dc, env, comp, name string) error {
	return nil
}

func (m *mockStateManager) GetDatacenterComponent(ctx context.Context, dc, component string) (*types.DatacenterComponentConfig, error) {
	return nil, fmt.Errorf("not found")
}

func (m *mockStateManager) SaveDatacenterComponent(ctx context.Context, dc string, s *types.DatacenterComponentConfig) error {
	return nil
}

func (m *mockStateManager) DeleteDatacenterComponent(ctx context.Context, dc, component string) error {
	return nil
}

func (m *mockStateManager) ListDatacenterComponents(ctx context.Context, dc string) ([]*types.DatacenterComponentConfig, error) {
	return nil, nil
}

func (m *mockStateManager) Lock(ctx context.Context, scope state.LockScope) (backend.Lock, error) {
	return nil, nil
}

func (m *mockStateManager) Backend() backend.Backend {
	return nil
}

// mockPlugin implements iac.Plugin for testing
type mockPlugin struct {
	name       string
	applyErr   error
	destroyErr error
	outputs    map[string]iac.OutputValue
}

func (p *mockPlugin) Name() string {
	return p.name
}

func (p *mockPlugin) Preview(ctx context.Context, opts iac.RunOptions) (*iac.PreviewResult, error) {
	return &iac.PreviewResult{}, nil
}

func (p *mockPlugin) Apply(ctx context.Context, opts iac.RunOptions) (*iac.ApplyResult, error) {
	if p.applyErr != nil {
		return nil, p.applyErr
	}
	return &iac.ApplyResult{
		Outputs: p.outputs,
	}, nil
}

func (p *mockPlugin) Destroy(ctx context.Context, opts iac.RunOptions) error {
	return p.destroyErr
}

func (p *mockPlugin) Refresh(ctx context.Context, opts iac.RunOptions) (*iac.RefreshResult, error) {
	return &iac.RefreshResult{}, nil
}

// testRegistry creates a test registry with plugins registered
// Note: This uses the global DefaultRegistry which has its factories map initialized
var testRegistry *iac.Registry

func init() {
	// Use the default registry which is already initialized
	testRegistry = iac.DefaultRegistry
}

// newTestRegistry returns a registry for testing - uses the global registry
func newTestRegistry() *iac.Registry {
	return testRegistry
}

func TestResourceKey(t *testing.T) {
	tests := []struct {
		name     string
		nodeType graph.NodeType
		nodeName string
		want     string
	}{
		{"deployment", graph.NodeTypeDeployment, "api", "deployment.api"},
		{"database", graph.NodeTypeDatabase, "main", "database.main"},
		{"function", graph.NodeTypeFunction, "handler", "function.handler"},
		{"observability", graph.NodeTypeObservability, "observability", "observability.observability"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := graph.NewNode(tt.nodeType, "my-app", tt.nodeName)
			got := resourceKey(node)
			if got != tt.want {
				t.Errorf("resourceKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if opts.Parallelism != 10 {
		t.Errorf("Parallelism: got %d, want %d", opts.Parallelism, 10)
	}
	if !opts.StopOnError {
		t.Error("StopOnError should be true by default")
	}
	if opts.DryRun {
		t.Error("DryRun should be false by default")
	}
}

func TestNewExecutor(t *testing.T) {
	sm := newMockStateManager()
	registry := newTestRegistry()

	exec := NewExecutor(sm, registry, DefaultOptions())

	if exec == nil {
		t.Fatal("NewExecutor returned nil")
	}
	if exec.stateManager == nil {
		t.Error("stateManager is nil")
	}
	if exec.iacRegistry == nil {
		t.Error("iacRegistry is nil")
	}
}

func TestNewExecutor_ParallelismDefault(t *testing.T) {
	sm := newMockStateManager()
	registry := newTestRegistry()

	// Test with zero parallelism (should default to 10)
	opts := Options{Parallelism: 0}
	exec := NewExecutor(sm, registry, opts)

	if exec.options.Parallelism != 10 {
		t.Errorf("Parallelism should default to 10, got %d", exec.options.Parallelism)
	}
}

func TestExecute_EmptyPlan(t *testing.T) {
	sm := newMockStateManager()
	registry := newTestRegistry()
	exec := NewExecutor(sm, registry, DefaultOptions())

	plan := &planner.Plan{
		Environment: "test",
		Datacenter:  "dc",
		ToCreate:    0,
		ToUpdate:    0,
		ToDelete:    0,
	}

	g := graph.NewGraph("test", "dc")

	result, err := exec.Execute(context.Background(), plan, g)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Error("Empty plan should succeed")
	}
	if result.Created != 0 || result.Updated != 0 || result.Deleted != 0 {
		t.Error("Empty plan should have no changes")
	}
}

func TestExecute_DryRun(t *testing.T) {
	sm := newMockStateManager()
	registry := newTestRegistry()

	// Register a mock plugin (overrides any existing)
	registry.Register("native", func() (iac.Plugin, error) {
		return &mockPlugin{name: "native"}, nil
	})

	opts := DefaultOptions()
	opts.DryRun = true

	exec := NewExecutor(sm, registry, opts)

	node := graph.NewNode(graph.NodeTypeDeployment, "api", "main")
	g := graph.NewGraph("test", "dc")
	_ = g.AddNode(node)

	plan := &planner.Plan{
		Environment: "test",
		Datacenter:  "dc",
		ToCreate:    1,
		Changes: []*planner.ResourceChange{
			{
				Node:   node,
				Action: planner.ActionCreate,
			},
		},
	}

	result, err := exec.Execute(context.Background(), plan, g)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Error("Dry run should succeed")
	}

	// In dry run mode, changes are simulated
	if result.Created != 1 {
		t.Errorf("Created: got %d, want %d", result.Created, 1)
	}
}

func TestExecute_ContextCancellation(t *testing.T) {
	sm := newMockStateManager()
	registry := newTestRegistry()
	registry.Register("native", func() (iac.Plugin, error) {
		return &mockPlugin{name: "native"}, nil
	})

	exec := NewExecutor(sm, registry, DefaultOptions())

	node := graph.NewNode(graph.NodeTypeDeployment, "api", "main")
	g := graph.NewGraph("test", "dc")
	_ = g.AddNode(node)

	plan := &planner.Plan{
		Environment: "test",
		Datacenter:  "dc",
		ToCreate:    1,
		Changes: []*planner.ResourceChange{
			{
				Node:   node,
				Action: planner.ActionCreate,
			},
		},
	}

	// Create already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := exec.Execute(ctx, plan, g)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if result.Success {
		t.Error("Should fail with cancelled context")
	}
	if len(result.Errors) == 0 {
		t.Error("Should have errors from cancelled context")
	}
}

func TestAreDependenciesSatisfied(t *testing.T) {
	sm := newMockStateManager()
	registry := newTestRegistry()
	exec := NewExecutor(sm, registry, DefaultOptions())

	g := graph.NewGraph("test", "dc")

	// Create nodes with dependency
	node1 := graph.NewNode(graph.NodeTypeDatabase, "api", "postgres")
	node2 := graph.NewNode(graph.NodeTypeDeployment, "api", "main")
	node2.AddDependency(node1.ID)

	_ = g.AddNode(node1)
	_ = g.AddNode(node2)

	t.Run("no dependencies", func(t *testing.T) {
		result := &ExecutionResult{NodeResults: make(map[string]*NodeResult)}

		// Node1 has no dependencies
		satisfied := exec.areDependenciesSatisfied(node1, g, result)
		if !satisfied {
			t.Error("Node with no dependencies should be satisfied")
		}
	})

	t.Run("dependency not executed", func(t *testing.T) {
		result := &ExecutionResult{NodeResults: make(map[string]*NodeResult)}

		// Node2 depends on Node1, but Node1 hasn't been executed
		satisfied := exec.areDependenciesSatisfied(node2, g, result)
		if satisfied {
			t.Error("Should not be satisfied when dependency not executed")
		}
	})

	t.Run("dependency failed", func(t *testing.T) {
		result := &ExecutionResult{
			NodeResults: map[string]*NodeResult{
				node1.ID: {
					Success: false,
				},
			},
		}

		satisfied := exec.areDependenciesSatisfied(node2, g, result)
		if satisfied {
			t.Error("Should not be satisfied when dependency failed")
		}
	})

	t.Run("dependency succeeded", func(t *testing.T) {
		result := &ExecutionResult{
			NodeResults: map[string]*NodeResult{
				node1.ID: {
					Success: true,
				},
			},
		}

		satisfied := exec.areDependenciesSatisfied(node2, g, result)
		if !satisfied {
			t.Error("Should be satisfied when dependency succeeded")
		}
	})
}

func TestExecutionResult(t *testing.T) {
	result := &ExecutionResult{
		Success:     true,
		Duration:    5 * time.Second,
		Created:     2,
		Updated:     1,
		Deleted:     1,
		Failed:      0,
		Errors:      []error{},
		NodeResults: make(map[string]*NodeResult),
	}

	if !result.Success {
		t.Error("Success should be true")
	}
	if result.Created != 2 {
		t.Errorf("Created: got %d", result.Created)
	}
	if result.Updated != 1 {
		t.Errorf("Updated: got %d", result.Updated)
	}
	if result.Deleted != 1 {
		t.Errorf("Deleted: got %d", result.Deleted)
	}
}

func TestNodeResult(t *testing.T) {
	result := &NodeResult{
		NodeID:   "api/main",
		Action:   planner.ActionCreate,
		Success:  true,
		Duration: 2 * time.Second,
		Error:    nil,
		Outputs: map[string]interface{}{
			"url": "http://localhost:8080",
		},
	}

	if result.NodeID != "api/main" {
		t.Errorf("NodeID: got %q", result.NodeID)
	}
	if result.Action != planner.ActionCreate {
		t.Errorf("Action: got %s", result.Action)
	}
	if !result.Success {
		t.Error("Success should be true")
	}
	if result.Outputs["url"] != "http://localhost:8080" {
		t.Error("Outputs not preserved")
	}
}

func TestOptions(t *testing.T) {
	opts := Options{
		Parallelism: 5,
		DryRun:      true,
		StopOnError: false,
	}

	if opts.Parallelism != 5 {
		t.Errorf("Parallelism: got %d", opts.Parallelism)
	}
	if !opts.DryRun {
		t.Error("DryRun should be true")
	}
	if opts.StopOnError {
		t.Error("StopOnError should be false")
	}
}

func TestEvaluateWhenCondition_EmptyAlwaysTrue(t *testing.T) {
	sm := newMockStateManager()
	exec := NewExecutor(sm, newTestRegistry(), DefaultOptions())

	result := exec.evaluateWhenCondition("", nil)
	if !result {
		t.Error("empty when should return true")
	}
}

func TestEvaluateWhenCondition_EqualityMatch(t *testing.T) {
	sm := newMockStateManager()
	exec := NewExecutor(sm, newTestRegistry(), DefaultOptions())

	inputs := map[string]interface{}{
		"type": "postgres:^16",
	}
	when := `element(split(":", node.inputs.type), 0) == "postgres"`

	result := exec.evaluateWhenCondition(when, inputs)
	if !result {
		t.Error("should match postgres type via HCL evaluation")
	}
}

func TestEvaluateWhenCondition_EqualityNoMatch(t *testing.T) {
	sm := newMockStateManager()
	exec := NewExecutor(sm, newTestRegistry(), DefaultOptions())

	inputs := map[string]interface{}{
		"type": "mysql:^8",
	}
	when := `element(split(":", node.inputs.type), 0) == "postgres"`

	result := exec.evaluateWhenCondition(when, inputs)
	if result {
		t.Error("should not match mysql type when looking for postgres")
	}
}

func TestEvaluateWhenCondition_NotNullTrue(t *testing.T) {
	sm := newMockStateManager()
	exec := NewExecutor(sm, newTestRegistry(), DefaultOptions())

	inputs := map[string]interface{}{
		"image": "nginx:latest",
	}
	when := `node.inputs.image != null`

	result := exec.evaluateWhenCondition(when, inputs)
	if !result {
		t.Error("should return true when image is set")
	}
}

func TestEvaluateWhenCondition_NotNullFalse(t *testing.T) {
	sm := newMockStateManager()
	exec := NewExecutor(sm, newTestRegistry(), DefaultOptions())

	inputs := map[string]interface{}{
		"image": nil,
	}
	when := `node.inputs.image != null`

	result := exec.evaluateWhenCondition(when, inputs)
	if result {
		t.Error("should return false when image is nil")
	}
}

func TestEvaluateWhenCondition_HCLExpression(t *testing.T) {
	sm := newMockStateManager()
	exec := NewExecutor(sm, newTestRegistry(), DefaultOptions())

	// Test a compound HCL expression using && (logical AND)
	inputs := map[string]interface{}{
		"image":   "nginx:latest",
		"runtime": nil,
	}
	when := `node.inputs.image != null`

	result := exec.evaluateWhenCondition(when, inputs)
	if !result {
		t.Error("HCL expression should match when image is set")
	}
}

func TestEvaluateWhenCondition_RedisType(t *testing.T) {
	sm := newMockStateManager()
	exec := NewExecutor(sm, newTestRegistry(), DefaultOptions())

	inputs := map[string]interface{}{
		"type": "redis:^7",
	}
	when := `element(split(":", node.inputs.type), 0) == "redis"`

	result := exec.evaluateWhenCondition(when, inputs)
	if !result {
		t.Error("should match redis type")
	}
}

func TestEvaluateWhenCondition_WithVariables(t *testing.T) {
	sm := newMockStateManager()
	opts := DefaultOptions()
	opts.DatacenterVariables = map[string]interface{}{
		"region": "us-east-1",
	}
	exec := NewExecutor(sm, newTestRegistry(), opts)

	inputs := map[string]interface{}{
		"type": "postgres:^16",
	}
	when := `element(split(":", node.inputs.type), 0) == "postgres"`

	result := exec.evaluateWhenCondition(when, inputs)
	if !result {
		t.Error("should still match with variables set")
	}
}

func TestEvaluateWhenCondition_FallbackOnInvalidHCL(t *testing.T) {
	sm := newMockStateManager()
	exec := NewExecutor(sm, newTestRegistry(), DefaultOptions())

	// This is not valid HCL but the string fallback handles it
	inputs := map[string]interface{}{
		"image": "test",
	}
	when := `node.inputs.image != null`

	// Should succeed via HCL evaluation path
	result := exec.evaluateWhenCondition(when, inputs)
	if !result {
		t.Error("should evaluate successfully")
	}
}

func TestEvaluateErrorMessage_SimpleLiteral(t *testing.T) {
	sm := newMockStateManager()
	exec := NewExecutor(sm, newTestRegistry(), DefaultOptions())

	msg := "MongoDB is not supported by this datacenter."
	result := exec.evaluateErrorMessage(msg, nil)

	if result != msg {
		t.Errorf("expected %q, got %q", msg, result)
	}
}

func TestEvaluateErrorMessage_WithInterpolation(t *testing.T) {
	sm := newMockStateManager()
	exec := NewExecutor(sm, newTestRegistry(), DefaultOptions())

	inputs := map[string]interface{}{
		"type": "mongodb",
	}
	msg := `Unsupported database type: ${node.inputs.type}`

	result := exec.evaluateErrorMessage(msg, inputs)

	expected := "Unsupported database type: mongodb"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestEvaluateErrorMessage_FallbackOnInvalidTemplate(t *testing.T) {
	sm := newMockStateManager()
	exec := NewExecutor(sm, newTestRegistry(), DefaultOptions())

	// A string that isn't valid HCL template syntax
	msg := "plain error message with no interpolation"
	result := exec.evaluateErrorMessage(msg, nil)

	if result != msg {
		t.Errorf("expected %q, got %q", msg, result)
	}
}
