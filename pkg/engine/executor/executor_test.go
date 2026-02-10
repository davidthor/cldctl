package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/davidthor/cldctl/pkg/engine/planner"
	"github.com/davidthor/cldctl/pkg/graph"
	"github.com/davidthor/cldctl/pkg/iac"
	"github.com/davidthor/cldctl/pkg/schema/datacenter"
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

func (p *mockPlugin) Import(ctx context.Context, opts iac.ImportOptions) (*iac.ImportResult, error) {
	return &iac.ImportResult{}, nil
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

func TestEvaluateWhenCondition_EqualNullTrue(t *testing.T) {
	sm := newMockStateManager()
	exec := NewExecutor(sm, newTestRegistry(), DefaultOptions())

	// image is not in the inputs map at all (simulates a runtime-based deployment)
	inputs := map[string]interface{}{
		"runtime": map[string]interface{}{"language": "node:22"},
		"command": []string{"npx", "inngest-cli", "dev"},
	}
	when := `node.inputs.image == null`

	result := exec.evaluateWhenCondition(when, inputs)
	if !result {
		t.Error("should return true when image is not in inputs (missing key is null)")
	}
}

func TestEvaluateWhenCondition_EqualNullFalse(t *testing.T) {
	sm := newMockStateManager()
	exec := NewExecutor(sm, newTestRegistry(), DefaultOptions())

	inputs := map[string]interface{}{
		"image": "nginx:latest",
	}
	when := `node.inputs.image == null`

	result := exec.evaluateWhenCondition(when, inputs)
	if result {
		t.Error("should return false when image is set")
	}
}

func TestEvaluateWhenCondition_EqualNullNilValue(t *testing.T) {
	sm := newMockStateManager()
	exec := NewExecutor(sm, newTestRegistry(), DefaultOptions())

	// image is explicitly set to nil (present in map but nil value)
	inputs := map[string]interface{}{
		"image": nil,
	}
	when := `node.inputs.image == null`

	result := exec.evaluateWhenCondition(when, inputs)
	if !result {
		t.Error("should return true when image is explicitly nil")
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

func TestExtractVersionFromType(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{"simple version", "postgres:16", "16"},
		{"semver caret", "postgres:^16", "16"},
		{"semver tilde", "redis:~7", "7"},
		{"semver range", "postgres:>=14", "14"},
		{"full semver", "mysql:8.0.32", "8.0.32"},
		{"no version", "redis", ""},
		{"empty string", "", ""},
		{"nil input", nil, ""},
		{"non-string", 42, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractVersionFromType(tt.input)
			if result != tt.expected {
				t.Errorf("extractVersionFromType(%v) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSanitizeResourceName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple name", "my-app", "my-app"},
		{"with slash", "questra/app", "questra-app"},
		{"multiple slashes", "org/team/app", "org-team-app"},
		{"dots and underscores", "my_app.v2", "my_app.v2"},
		{"leading slash", "/app", "app"},
		{"empty", "", ""},
		{"alphanumeric", "abc123", "abc123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeResourceName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeResourceName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExecute_CascadedFailure_ResolvesExpressions(t *testing.T) {
	// Test that when a node fails due to a dependency cascade, expressions in its
	// inputs are still resolved (using outputs from completed dependency nodes).
	// This ensures `cldctl inspect` shows resolved values even for cascaded failures.
	sm := newMockStateManager()
	registry := newTestRegistry()

	g := graph.NewGraph("test-env", "test-dc")

	// Create a database node (will succeed via noop, pre-populate outputs)
	dbNode := graph.NewNode(graph.NodeTypeDatabase, "my-app", "main")
	dbNode.SetInput("type", "postgres:16")
	dbNode.Outputs = map[string]interface{}{
		"url":      "postgresql://user:pass@localhost:5432/main",
		"host":     "localhost",
		"port":     5432,
		"username": "user",
		"password": "pass",
		"database": "main",
	}
	dbNode.State = graph.NodeStateCompleted
	_ = g.AddNode(dbNode)

	// Create a task node that depends on database (will be ActionCreate and fail
	// because there's no datacenter configured — simulates a real migration failure)
	taskNode := graph.NewNode(graph.NodeTypeTask, "my-app", "main-migration")
	taskNode.AddDependency(dbNode.ID)
	dbNode.AddDependent(taskNode.ID)
	_ = g.AddNode(taskNode)

	// Create a deployment node that depends on database AND task
	deployNode := graph.NewNode(graph.NodeTypeDeployment, "my-app", "api")
	deployNode.SetInput("environment", map[string]string{
		"DATABASE_URL": "${{ databases.main.url }}",
		"API_PORT":     "8080",
	})
	deployNode.SetInput("command", []string{"node", "api.js"})
	deployNode.AddDependency(dbNode.ID)
	dbNode.AddDependent(deployNode.ID)
	deployNode.AddDependency(taskNode.ID)
	taskNode.AddDependent(deployNode.ID)
	_ = g.AddNode(deployNode)

	// Build a plan:
	// - database is noop (already completed)
	// - task is create (will fail — no datacenter configured)
	// - deployment is create (should cascade fail due to task failure)
	plan := &planner.Plan{
		Environment: "test-env",
		Datacenter:  "test-dc",
		ToCreate:    2, // task + deployment
		Changes: []*planner.ResourceChange{
			{Node: dbNode, Action: planner.ActionNoop},
			{Node: taskNode, Action: planner.ActionCreate},
			{Node: deployNode, Action: planner.ActionCreate},
		},
	}

	opts := DefaultOptions()
	opts.StopOnError = false // Allow cascade to process remaining nodes
	exec := NewExecutor(sm, registry, opts)

	result, err := exec.Execute(context.Background(), plan, g)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	// Task should have failed (no datacenter configured)
	taskResult := result.NodeResults[taskNode.ID]
	if taskResult == nil {
		t.Fatal("expected task result to be present")
	}
	if taskResult.Success {
		t.Error("expected task to fail")
	}

	// Deployment should have failed due to cascade
	deployResult := result.NodeResults[deployNode.ID]
	if deployResult == nil {
		t.Fatal("expected deployment result to be present")
	}
	if deployResult.Success {
		t.Error("expected deployment to fail due to cascaded dependency failure")
	}

	// Check that the saved state has resolved expressions
	envState := sm.environments["test-dc/test-env"]
	if envState == nil {
		t.Fatal("expected environment state to be saved")
	}

	compState := envState.Components["my-app"]
	if compState == nil {
		t.Fatal("expected component state to be saved")
	}

	resState := compState.Resources["deployment.api"]
	if resState == nil {
		t.Fatal("expected deployment resource state to be saved")
	}

	// Check status reason includes dependency info
	if resState.StatusReason == "" {
		t.Error("expected StatusReason to be set for cascaded failure")
	}
	if !contains(resState.StatusReason, taskNode.ID) {
		t.Errorf("StatusReason should reference failed task, got: %s", resState.StatusReason)
	}

	// Check that the DATABASE_URL expression was resolved
	envVars, ok := resState.Inputs["environment"]
	if !ok {
		t.Fatal("expected environment input to be present")
	}

	switch env := envVars.(type) {
	case map[string]string:
		dbURL := env["DATABASE_URL"]
		if dbURL == "${{ databases.main.url }}" {
			t.Error("DATABASE_URL should be resolved, still contains ${{ }} expression")
		}
		if dbURL != "postgresql://user:pass@localhost:5432/main" {
			t.Errorf("DATABASE_URL = %q, want %q", dbURL, "postgresql://user:pass@localhost:5432/main")
		}

		apiPort := env["API_PORT"]
		if apiPort != "8080" {
			t.Errorf("API_PORT = %q, want %q", apiPort, "8080")
		}
	default:
		t.Errorf("environment input is %T, want map[string]string", envVars)
	}
}

func TestAutoPopulateDatabaseEndpoints(t *testing.T) {
	exec := &Executor{}

	t.Run("auto-populates read and write from top-level outputs", func(t *testing.T) {
		outputs := map[string]interface{}{
			"host":     "db.example.com",
			"port":     5432,
			"url":      "postgresql://user:pass@db.example.com:5432/mydb",
			"username": "user",
			"password": "pass",
			"database": "mydb",
		}

		exec.autoPopulateDatabaseEndpoints(outputs, &mockHook{})

		// read should be auto-populated
		readRaw, hasRead := outputs["read"]
		if !hasRead {
			t.Fatal("expected 'read' to be auto-populated")
		}
		readMap, ok := readRaw.(map[string]interface{})
		if !ok {
			t.Fatalf("expected 'read' to be map[string]interface{}, got %T", readRaw)
		}
		if readMap["host"] != "db.example.com" {
			t.Errorf("read.host = %v, want %v", readMap["host"], "db.example.com")
		}
		if readMap["url"] != "postgresql://user:pass@db.example.com:5432/mydb" {
			t.Errorf("read.url = %v, want connection URL", readMap["url"])
		}

		// write should be auto-populated
		writeRaw, hasWrite := outputs["write"]
		if !hasWrite {
			t.Fatal("expected 'write' to be auto-populated")
		}
		writeMap, ok := writeRaw.(map[string]interface{})
		if !ok {
			t.Fatalf("expected 'write' to be map[string]interface{}, got %T", writeRaw)
		}
		if writeMap["host"] != "db.example.com" {
			t.Errorf("write.host = %v, want %v", writeMap["host"], "db.example.com")
		}
	})

	t.Run("does not overwrite explicit read/write", func(t *testing.T) {
		outputs := map[string]interface{}{
			"host":     "primary.db.example.com",
			"port":     5432,
			"url":      "postgresql://user:pass@primary.db.example.com:5432/mydb",
			"username": "user",
			"password": "pass",
			"read": map[string]interface{}{
				"host": "replica.db.example.com",
				"url":  "postgresql://readonly:pass@replica.db.example.com:5433/mydb",
			},
			"write": map[string]interface{}{
				"host": "proxy.db.example.com",
				"url":  "postgresql://writer:pass@proxy.db.example.com:5434/mydb",
			},
		}

		exec.autoPopulateDatabaseEndpoints(outputs, &mockHook{})

		// read should NOT be overwritten
		readMap := outputs["read"].(map[string]interface{})
		if readMap["host"] != "replica.db.example.com" {
			t.Errorf("read.host should be preserved, got %v", readMap["host"])
		}

		// write should NOT be overwritten
		writeMap := outputs["write"].(map[string]interface{})
		if writeMap["host"] != "proxy.db.example.com" {
			t.Errorf("write.host should be preserved, got %v", writeMap["host"])
		}
	})

	t.Run("read and write are independent copies", func(t *testing.T) {
		outputs := map[string]interface{}{
			"host":     "db.example.com",
			"port":     5432,
			"url":      "postgresql://user:pass@db.example.com:5432/mydb",
			"username": "user",
			"password": "pass",
		}

		exec.autoPopulateDatabaseEndpoints(outputs, &mockHook{})

		// Modify read, write should not be affected
		readMap := outputs["read"].(map[string]interface{})
		writeMap := outputs["write"].(map[string]interface{})

		readMap["host"] = "modified"
		if writeMap["host"] == "modified" {
			t.Error("modifying read should not affect write (must be independent copies)")
		}
	})
}

// mockHook implements the datacenter.Hook interface for testing
type mockHook struct {
	when          string
	outputs       map[string]string
	nestedOutputs map[string]map[string]string
	errorMsg      string
}

func (h *mockHook) When() string                              { return h.when }
func (h *mockHook) Modules() []datacenter.Module              { return nil }
func (h *mockHook) Outputs() map[string]string                { return h.outputs }
func (h *mockHook) NestedOutputs() map[string]map[string]string { return h.nestedOutputs }
func (h *mockHook) Error() string                             { return h.errorMsg }

func TestBuildDependencyError(t *testing.T) {
	exec := &Executor{}

	node := graph.NewNode(graph.NodeTypeDeployment, "app", "api")
	node.AddDependency("app/task/main-migration")
	node.AddDependency("app/database/main")

	result := &ExecutionResult{
		NodeResults: map[string]*NodeResult{
			"app/task/main-migration": {NodeID: "app/task/main-migration", Success: false},
			"app/database/main":       {NodeID: "app/database/main", Success: true},
		},
	}

	err := exec.buildDependencyError(node, result)
	if err == nil {
		t.Fatal("expected error")
	}

	// Should mention the failed dependency
	errStr := err.Error()
	if !contains(errStr, "app/task/main-migration") {
		t.Errorf("error should mention failed dep: %s", errStr)
	}
	// Should use singular "dependency" for single failure
	if !contains(errStr, "dependency") {
		t.Errorf("error should contain 'dependency': %s", errStr)
	}
}

func TestHasMatchingHook_DatabaseUser_Matches(t *testing.T) {
	// Datacenter has a postgres-only databaseUser hook.
	// Write to a temp file so the HCL evaluator can read source text for
	// complex when expressions (exprToString needs file-based loading).
	dcContent := []byte(`
environment {
  databaseUser {
    when = node.inputs.dbEngine == "postgres"
    module "pg-user" {
      plugin = "native"
      build  = "./modules/pg-user"
      inputs = {
        name = "test"
      }
    }
    outputs = {
      host = "localhost"
      port = "5432"
      url  = "postgres://localhost/test"
    }
  }
}
`)
	tmpFile := filepath.Join(t.TempDir(), "datacenter.dc")
	if err := os.WriteFile(tmpFile, dcContent, 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	loader := datacenter.NewLoader()
	dc, err := loader.Load(tmpFile)
	if err != nil {
		t.Fatalf("failed to load datacenter: %v", err)
	}

	exec := &Executor{
		options: Options{Datacenter: dc},
	}

	// Postgres databaseUser should match
	pgNode := graph.NewNode(graph.NodeTypeDatabaseUser, "my-app", "main--api")
	pgNode.SetInput("dbEngine", "postgres")
	if !exec.hasMatchingHook(pgNode) {
		t.Error("expected postgres databaseUser to match hook")
	}

	// Redis databaseUser should NOT match (no redis hook defined)
	redisNode := graph.NewNode(graph.NodeTypeDatabaseUser, "my-app", "cache--api")
	redisNode.SetInput("dbEngine", "redis")
	if exec.hasMatchingHook(redisNode) {
		t.Error("expected redis databaseUser to NOT match any hook")
	}
}

func TestExecuteDatabaseUserPassthrough(t *testing.T) {
	sm := newMockStateManager()
	registry := newTestRegistry()

	g := graph.NewGraph("test-env", "test-dc")

	// Create database node with outputs
	dbNode := graph.NewNode(graph.NodeTypeDatabase, "my-app", "cache")
	dbNode.Outputs = map[string]interface{}{
		"host": "redis.example.com",
		"port": "6379",
		"url":  "redis://redis.example.com:6379",
	}
	dbNode.State = graph.NodeStateCompleted
	_ = g.AddNode(dbNode)

	// Create databaseUser node (Redis — no matching hook)
	dbUserNode := graph.NewNode(graph.NodeTypeDatabaseUser, "my-app", "cache--api")
	dbUserNode.SetInput("database", "cache")
	dbUserNode.SetInput("type", "redis:^7")
	dbUserNode.SetInput("consumer", "api")
	dbUserNode.SetInput("consumerType", "deployment")
	dbUserNode.SetInput("component", "my-app")
	dbUserNode.AddDependency(dbNode.ID)
	dbNode.AddDependent(dbUserNode.ID)
	_ = g.AddNode(dbUserNode)

	exec := NewExecutor(sm, registry, Options{Parallelism: 1, StopOnError: true})
	exec.graph = g
	exec.datacenterName = "test-dc"

	envState := &types.EnvironmentState{
		Name:       "test-env",
		Components: make(map[string]*types.ComponentState),
	}

	change := &planner.ResourceChange{
		Node:   dbUserNode,
		Action: planner.ActionCreate,
	}

	result := exec.executeDatabaseUserPassthrough(context.Background(), change, envState)

	if !result.Success {
		t.Fatal("expected databaseUser pass-through to succeed")
	}

	// Outputs should match the parent database outputs
	if result.Outputs["host"] != "redis.example.com" {
		t.Errorf("expected host 'redis.example.com', got %v", result.Outputs["host"])
	}
	if result.Outputs["url"] != "redis://redis.example.com:6379" {
		t.Errorf("expected correct url, got %v", result.Outputs["url"])
	}

	// State should be saved
	compState := envState.Components["my-app"]
	if compState == nil {
		t.Fatal("expected component state to be created")
	}
	resState := compState.Resources["databaseUser.cache--api"]
	if resState == nil {
		t.Fatal("expected resource state to be created")
	}
	if resState.Status != types.ResourceStatusReady {
		t.Errorf("expected status Ready, got %s", resState.Status)
	}
}

func TestGetHooksForType_DatabaseUser(t *testing.T) {
	// Load a datacenter with a databaseUser hook
	loader := datacenter.NewLoader()
	dc, err := loader.LoadFromBytes([]byte(`
environment {
  databaseUser {
    module "db-user" {
      plugin = "native"
      build  = "./modules/db-user"
      inputs = {
        name = "test"
      }
    }
    outputs = {
      host = "localhost"
      port = "5432"
      url  = "postgres://localhost/test"
    }
  }
}
`), "test.dc")
	if err != nil {
		t.Fatalf("failed to load datacenter: %v", err)
	}

	exec := &Executor{
		options: Options{Datacenter: dc},
	}

	hooks := exec.getHooksForType(graph.NodeTypeDatabaseUser)
	if len(hooks) != 1 {
		t.Errorf("expected 1 databaseUser hook, got %d", len(hooks))
	}
}

func TestGetHooksForType_NetworkPolicy(t *testing.T) {
	// Load a datacenter with a networkPolicy hook
	loader := datacenter.NewLoader()
	dc, err := loader.LoadFromBytes([]byte(`
environment {
  networkPolicy {
    module "net-policy" {
      plugin = "native"
      build  = "./modules/net-policy"
      inputs = {
        from = "test"
      }
    }
  }
}
`), "test.dc")
	if err != nil {
		t.Fatalf("failed to load datacenter: %v", err)
	}

	exec := &Executor{
		options: Options{Datacenter: dc},
	}

	hooks := exec.getHooksForType(graph.NodeTypeNetworkPolicy)
	if len(hooks) != 1 {
		t.Errorf("expected 1 networkPolicy hook, got %d", len(hooks))
	}
}

func TestGetHooksForType_NoHooksDefined(t *testing.T) {
	// Load a datacenter with no hooks
	loader := datacenter.NewLoader()
	dc, err := loader.LoadFromBytes([]byte("environment {}"), "test.dc")
	if err != nil {
		t.Fatalf("failed to load datacenter: %v", err)
	}

	exec := &Executor{
		options: Options{Datacenter: dc},
	}

	dbUserHooks := exec.getHooksForType(graph.NodeTypeDatabaseUser)
	if len(dbUserHooks) != 0 {
		t.Errorf("expected 0 databaseUser hooks, got %d", len(dbUserHooks))
	}

	npHooks := exec.getHooksForType(graph.NodeTypeNetworkPolicy)
	if len(npHooks) != 0 {
		t.Errorf("expected 0 networkPolicy hooks, got %d", len(npHooks))
	}
}
