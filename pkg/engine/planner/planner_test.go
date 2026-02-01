package planner

import (
	"testing"

	"github.com/architect-io/arcctl/pkg/graph"
	"github.com/architect-io/arcctl/pkg/state/types"
)

func TestActionConstants(t *testing.T) {
	if ActionCreate != "create" {
		t.Errorf("ActionCreate: got %q", ActionCreate)
	}
	if ActionUpdate != "update" {
		t.Errorf("ActionUpdate: got %q", ActionUpdate)
	}
	if ActionReplace != "replace" {
		t.Errorf("ActionReplace: got %q", ActionReplace)
	}
	if ActionDelete != "delete" {
		t.Errorf("ActionDelete: got %q", ActionDelete)
	}
	if ActionNoop != "noop" {
		t.Errorf("ActionNoop: got %q", ActionNoop)
	}
}

func TestNewPlanner(t *testing.T) {
	p := NewPlanner()
	if p == nil {
		t.Fatal("NewPlanner returned nil")
	}
}

func TestPlanIsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		plan     Plan
		expected bool
	}{
		{
			name:     "empty plan",
			plan:     Plan{ToCreate: 0, ToUpdate: 0, ToDelete: 0},
			expected: true,
		},
		{
			name:     "plan with creates",
			plan:     Plan{ToCreate: 1, ToUpdate: 0, ToDelete: 0},
			expected: false,
		},
		{
			name:     "plan with updates",
			plan:     Plan{ToCreate: 0, ToUpdate: 1, ToDelete: 0},
			expected: false,
		},
		{
			name:     "plan with deletes",
			plan:     Plan{ToCreate: 0, ToUpdate: 0, ToDelete: 1},
			expected: false,
		},
		{
			name:     "plan with no-changes only",
			plan:     Plan{ToCreate: 0, ToUpdate: 0, ToDelete: 0, NoChange: 5},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.plan.IsEmpty()
			if result != tt.expected {
				t.Errorf("IsEmpty(): got %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestPlan_NewEnvironment(t *testing.T) {
	p := NewPlanner()

	// Create a graph with new resources
	g := graph.NewGraph("test-env", "test-dc")

	node1 := graph.NewNode(graph.NodeTypeDatabase, "api", "postgres")
	node1.SetInput("type", "postgres")
	node1.SetInput("size", "small")
	_ = g.AddNode(node1)

	node2 := graph.NewNode(graph.NodeTypeDeployment, "api", "main")
	node2.SetInput("image", "myapp:v1")
	node2.SetInput("replicas", 1)
	_ = g.AddNode(node2)

	_ = g.AddEdge(node2.ID, node1.ID)

	// Plan against nil current state (new environment)
	plan, err := p.Plan(g, nil)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if plan.Environment != "test-env" {
		t.Errorf("Environment: got %q, want %q", plan.Environment, "test-env")
	}
	if plan.Datacenter != "test-dc" {
		t.Errorf("Datacenter: got %q, want %q", plan.Datacenter, "test-dc")
	}
	if plan.ToCreate != 2 {
		t.Errorf("ToCreate: got %d, want %d", plan.ToCreate, 2)
	}
	if plan.ToUpdate != 0 {
		t.Errorf("ToUpdate: got %d, want %d", plan.ToUpdate, 0)
	}
	if plan.ToDelete != 0 {
		t.Errorf("ToDelete: got %d, want %d", plan.ToDelete, 0)
	}
	if len(plan.Changes) != 2 {
		t.Errorf("Changes count: got %d, want %d", len(plan.Changes), 2)
	}

	// All changes should be creates
	for _, change := range plan.Changes {
		if change.Action != ActionCreate {
			t.Errorf("Expected ActionCreate, got %s for %s", change.Action, change.Node.ID)
		}
	}
}

func TestPlan_NoChanges(t *testing.T) {
	p := NewPlanner()

	// Create a graph
	g := graph.NewGraph("test-env", "test-dc")

	node := graph.NewNode(graph.NodeTypeDatabase, "api", "postgres")
	node.SetInput("type", "postgres")
	_ = g.AddNode(node)

	// Planner looks up key as: component + "/" + type + "/" + name
	// So for component="api", type="database", name="postgres"
	// The key is "api/database/postgres"
	// This maps to Resources[type/name] in component state
	resKey := string(graph.NodeTypeDatabase) + "/" + "postgres"

	currentState := &types.EnvironmentState{
		Name: "test-env",
		Components: map[string]*types.ComponentState{
			"api": {
				Name: "api",
				Resources: map[string]*types.ResourceState{
					resKey: {
						Name:      "postgres",
						Type:      string(graph.NodeTypeDatabase),
						Component: "api",
						Inputs: map[string]interface{}{
							"type": "postgres",
						},
					},
				},
			},
		},
	}

	plan, err := p.Plan(g, currentState)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if plan.ToCreate != 0 {
		t.Errorf("ToCreate: got %d, want %d", plan.ToCreate, 0)
	}
	if plan.ToUpdate != 0 {
		t.Errorf("ToUpdate: got %d, want %d", plan.ToUpdate, 0)
	}
	if plan.NoChange != 1 {
		t.Errorf("NoChange: got %d, want %d", plan.NoChange, 1)
	}
	if !plan.IsEmpty() {
		t.Error("Plan should be empty (no creates/updates/deletes)")
	}
}

func TestPlan_Updates(t *testing.T) {
	p := NewPlanner()

	// Create a graph with updated inputs
	g := graph.NewGraph("test-env", "test-dc")

	node := graph.NewNode(graph.NodeTypeDeployment, "api", "main")
	node.SetInput("image", "myapp:v2") // Changed from v1
	node.SetInput("replicas", 3)       // Changed from 1
	_ = g.AddNode(node)

	// Planner looks up key as: component + "/" + type + "/" + name
	resKey := string(graph.NodeTypeDeployment) + "/" + "main"

	currentState := &types.EnvironmentState{
		Name: "test-env",
		Components: map[string]*types.ComponentState{
			"api": {
				Name: "api",
				Resources: map[string]*types.ResourceState{
					resKey: {
						Name:      "main",
						Type:      string(graph.NodeTypeDeployment),
						Component: "api",
						Inputs: map[string]interface{}{
							"image":    "myapp:v1",
							"replicas": 1,
						},
					},
				},
			},
		},
	}

	plan, err := p.Plan(g, currentState)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if plan.ToUpdate != 1 {
		t.Errorf("ToUpdate: got %d, want %d", plan.ToUpdate, 1)
	}

	// Find the update change (there might be more changes for deletions)
	var updateChange *ResourceChange
	for _, c := range plan.Changes {
		if c.Action == ActionUpdate {
			updateChange = c
			break
		}
	}

	if updateChange == nil {
		t.Fatal("Expected an update change")
	}

	if len(updateChange.PropertyChanges) != 2 {
		t.Errorf("PropertyChanges count: got %d, want %d", len(updateChange.PropertyChanges), 2)
	}
}

func TestPlan_Deletions(t *testing.T) {
	p := NewPlanner()

	// Create empty graph (no resources desired)
	g := graph.NewGraph("test-env", "test-dc")

	// Create current state with existing resource
	currentState := &types.EnvironmentState{
		Name: "test-env",
		Components: map[string]*types.ComponentState{
			"api": {
				Name: "api",
				Resources: map[string]*types.ResourceState{
					"old-resource": {
						Name:      "old-resource",
						Type:      string(graph.NodeTypeDeployment),
						Component: "api",
					},
				},
			},
		},
	}

	plan, err := p.Plan(g, currentState)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if plan.ToDelete != 1 {
		t.Errorf("ToDelete: got %d, want %d", plan.ToDelete, 1)
	}

	// Find the delete change
	var deleteChange *ResourceChange
	for _, c := range plan.Changes {
		if c.Action == ActionDelete {
			deleteChange = c
			break
		}
	}

	if deleteChange == nil {
		t.Fatal("Expected a delete change")
	}
	if deleteChange.Reason != "resource no longer defined" {
		t.Errorf("Reason: got %q", deleteChange.Reason)
	}
}

func TestPlanDestroy(t *testing.T) {
	p := NewPlanner()

	// Create a graph with resources
	g := graph.NewGraph("test-env", "test-dc")

	node1 := graph.NewNode(graph.NodeTypeDatabase, "api", "postgres")
	_ = g.AddNode(node1)

	node2 := graph.NewNode(graph.NodeTypeDeployment, "api", "main")
	_ = g.AddNode(node2)
	_ = g.AddEdge(node2.ID, node1.ID)

	// Create current state
	currentState := &types.EnvironmentState{
		Name: "test-env",
		Components: map[string]*types.ComponentState{
			"api": {
				Name: "api",
				Resources: map[string]*types.ResourceState{
					"postgres": {
						Name:      "postgres",
						Component: "api",
					},
					"main": {
						Name:      "main",
						Component: "api",
					},
				},
			},
		},
	}

	plan, err := p.PlanDestroy(g, currentState)
	if err != nil {
		t.Fatalf("PlanDestroy failed: %v", err)
	}

	if plan.ToDelete != 2 {
		t.Errorf("ToDelete: got %d, want %d", plan.ToDelete, 2)
	}

	// All changes should be deletes
	for _, change := range plan.Changes {
		if change.Action != ActionDelete {
			t.Errorf("Expected ActionDelete, got %s", change.Action)
		}
		if change.Reason != "destroying environment" {
			t.Errorf("Reason: got %q", change.Reason)
		}
	}
}

func TestPlanDestroy_EmptyState(t *testing.T) {
	p := NewPlanner()

	g := graph.NewGraph("test-env", "test-dc")
	node := graph.NewNode(graph.NodeTypeDatabase, "api", "postgres")
	_ = g.AddNode(node)

	// Plan destroy against nil state
	plan, err := p.PlanDestroy(g, nil)
	if err != nil {
		t.Fatalf("PlanDestroy failed: %v", err)
	}

	// No resources to delete
	if plan.ToDelete != 0 {
		t.Errorf("ToDelete: got %d, want %d", plan.ToDelete, 0)
	}
}

func TestCompareInputs(t *testing.T) {
	p := NewPlanner()

	t.Run("no changes", func(t *testing.T) {
		desired := map[string]interface{}{
			"key1": "value1",
			"key2": 42,
		}
		current := map[string]interface{}{
			"key1": "value1",
			"key2": 42,
		}

		changes := p.compareInputs(desired, current)
		if len(changes) != 0 {
			t.Errorf("Expected 0 changes, got %d", len(changes))
		}
	})

	t.Run("new key", func(t *testing.T) {
		desired := map[string]interface{}{
			"key1": "value1",
			"key2": "value2",
		}
		current := map[string]interface{}{
			"key1": "value1",
		}

		changes := p.compareInputs(desired, current)
		if len(changes) != 1 {
			t.Fatalf("Expected 1 change, got %d", len(changes))
		}
		if changes[0].Path != "key2" {
			t.Errorf("Path: got %q", changes[0].Path)
		}
		if changes[0].OldValue != nil {
			t.Errorf("OldValue should be nil")
		}
	})

	t.Run("changed value", func(t *testing.T) {
		desired := map[string]interface{}{
			"key1": "new-value",
		}
		current := map[string]interface{}{
			"key1": "old-value",
		}

		changes := p.compareInputs(desired, current)
		if len(changes) != 1 {
			t.Fatalf("Expected 1 change, got %d", len(changes))
		}
		if changes[0].OldValue != "old-value" {
			t.Errorf("OldValue: got %v", changes[0].OldValue)
		}
		if changes[0].NewValue != "new-value" {
			t.Errorf("NewValue: got %v", changes[0].NewValue)
		}
	})

	t.Run("removed key", func(t *testing.T) {
		desired := map[string]interface{}{}
		current := map[string]interface{}{
			"key1": "value1",
		}

		changes := p.compareInputs(desired, current)
		if len(changes) != 1 {
			t.Fatalf("Expected 1 change, got %d", len(changes))
		}
		if changes[0].NewValue != nil {
			t.Errorf("NewValue should be nil")
		}
	})
}

func TestDeepEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        interface{}
		b        interface{}
		expected bool
	}{
		{"both nil", nil, nil, true},
		{"a nil", nil, "value", false},
		{"b nil", "value", nil, false},
		{"same strings", "hello", "hello", true},
		{"different strings", "hello", "world", false},
		{"same ints", 42, 42, true},
		{"different ints", 42, 43, false},
		{"same slices", []int{1, 2, 3}, []int{1, 2, 3}, true},
		{"different slices", []int{1, 2, 3}, []int{1, 2, 4}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deepEqual(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("deepEqual(%v, %v): got %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestFormatChanges(t *testing.T) {
	t.Run("empty changes", func(t *testing.T) {
		result := FormatChanges(nil)
		if result != "no changes" {
			t.Errorf("Expected 'no changes', got %q", result)
		}
	})

	t.Run("with changes", func(t *testing.T) {
		changes := []PropertyChange{
			{Path: "image", OldValue: "v1", NewValue: "v2"},
			{Path: "replicas", OldValue: 1, NewValue: 3},
		}

		result := FormatChanges(changes)
		if result == "no changes" {
			t.Error("Should format changes")
		}
		if len(result) == 0 {
			t.Error("Result should not be empty")
		}
	})
}

func TestResourceChange(t *testing.T) {
	node := graph.NewNode(graph.NodeTypeDeployment, "api", "main")

	change := &ResourceChange{
		Node:   node,
		Action: ActionUpdate,
		CurrentState: &types.ResourceState{
			Name: "main",
		},
		Reason: "configuration changed",
		PropertyChanges: []PropertyChange{
			{Path: "image", OldValue: "v1", NewValue: "v2"},
		},
	}

	if change.Node != node {
		t.Error("Node not set correctly")
	}
	if change.Action != ActionUpdate {
		t.Errorf("Action: got %s", change.Action)
	}
	if change.CurrentState == nil {
		t.Error("CurrentState should not be nil")
	}
	if len(change.PropertyChanges) != 1 {
		t.Errorf("PropertyChanges count: got %d", len(change.PropertyChanges))
	}
}

func TestPropertyChange(t *testing.T) {
	change := PropertyChange{
		Path:     "spec.replicas",
		OldValue: 1,
		NewValue: 3,
	}

	if change.Path != "spec.replicas" {
		t.Errorf("Path: got %q", change.Path)
	}
	if change.OldValue != 1 {
		t.Errorf("OldValue: got %v", change.OldValue)
	}
	if change.NewValue != 3 {
		t.Errorf("NewValue: got %v", change.NewValue)
	}
}
