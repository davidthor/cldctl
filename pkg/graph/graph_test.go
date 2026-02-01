package graph

import (
	"testing"
)

func TestNewGraph(t *testing.T) {
	g := NewGraph("test-env", "test-dc")

	if g.Environment != "test-env" {
		t.Errorf("expected environment 'test-env', got %q", g.Environment)
	}

	if g.Datacenter != "test-dc" {
		t.Errorf("expected datacenter 'test-dc', got %q", g.Datacenter)
	}

	if len(g.Nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(g.Nodes))
	}
}

func TestGraph_AddNode(t *testing.T) {
	g := NewGraph("env", "dc")
	node := NewNode(NodeTypeDatabase, "app", "main")

	err := g.AddNode(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(g.Nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(g.Nodes))
	}

	// Adding duplicate should fail
	err = g.AddNode(node)
	if err == nil {
		t.Error("expected error for duplicate node")
	}
}

func TestGraph_AddEdge(t *testing.T) {
	g := NewGraph("env", "dc")

	db := NewNode(NodeTypeDatabase, "app", "main")
	deploy := NewNode(NodeTypeDeployment, "app", "api")

	_ = g.AddNode(db)
	_ = g.AddNode(deploy)

	// Add edge: deploy depends on db
	err := g.AddEdge(deploy.ID, db.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deploy.DependsOn) != 1 {
		t.Errorf("expected 1 dependency, got %d", len(deploy.DependsOn))
	}

	if len(db.DependedOnBy) != 1 {
		t.Errorf("expected 1 dependent, got %d", len(db.DependedOnBy))
	}

	// Edge to non-existent node should fail
	err = g.AddEdge(deploy.ID, "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent node")
	}
}

func TestGraph_TopologicalSort(t *testing.T) {
	g := NewGraph("env", "dc")

	// Create nodes: A -> B -> C
	a := NewNode(NodeTypeDatabase, "app", "a")
	b := NewNode(NodeTypeDeployment, "app", "b")
	c := NewNode(NodeTypeService, "app", "c")

	_ = g.AddNode(a)
	_ = g.AddNode(b)
	_ = g.AddNode(c)

	_ = g.AddEdge(b.ID, a.ID) // B depends on A
	_ = g.AddEdge(c.ID, b.ID) // C depends on B

	sorted, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sorted) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(sorted))
	}

	// A should come before B, B should come before C
	aIdx, bIdx, cIdx := -1, -1, -1
	for i, n := range sorted {
		switch n.ID {
		case a.ID:
			aIdx = i
		case b.ID:
			bIdx = i
		case c.ID:
			cIdx = i
		}
	}

	if aIdx > bIdx {
		t.Error("A should come before B")
	}
	if bIdx > cIdx {
		t.Error("B should come before C")
	}
}

func TestGraph_TopologicalSort_Cycle(t *testing.T) {
	g := NewGraph("env", "dc")

	// Create cycle: A -> B -> A
	a := NewNode(NodeTypeDatabase, "app", "a")
	b := NewNode(NodeTypeDeployment, "app", "b")

	_ = g.AddNode(a)
	_ = g.AddNode(b)

	_ = g.AddEdge(a.ID, b.ID) // A depends on B
	_ = g.AddEdge(b.ID, a.ID) // B depends on A (creates cycle)

	_, err := g.TopologicalSort()
	if err == nil {
		t.Error("expected error for cycle")
	}
}

func TestGraph_ReverseTopologicalSort(t *testing.T) {
	g := NewGraph("env", "dc")

	a := NewNode(NodeTypeDatabase, "app", "a")
	b := NewNode(NodeTypeDeployment, "app", "b")
	c := NewNode(NodeTypeService, "app", "c")

	_ = g.AddNode(a)
	_ = g.AddNode(b)
	_ = g.AddNode(c)

	_ = g.AddEdge(b.ID, a.ID)
	_ = g.AddEdge(c.ID, b.ID)

	sorted, err := g.ReverseTopologicalSort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// C should come first (dependents before dependencies)
	if sorted[0].ID != c.ID {
		t.Errorf("expected C first, got %s", sorted[0].ID)
	}
}

func TestGraph_GetReadyNodes(t *testing.T) {
	g := NewGraph("env", "dc")

	a := NewNode(NodeTypeDatabase, "app", "a")
	b := NewNode(NodeTypeDeployment, "app", "b")
	c := NewNode(NodeTypeService, "app", "c")

	_ = g.AddNode(a)
	_ = g.AddNode(b)
	_ = g.AddNode(c)

	_ = g.AddEdge(b.ID, a.ID)
	_ = g.AddEdge(c.ID, b.ID)

	// Initially only A should be ready (no dependencies)
	ready := g.GetReadyNodes()
	if len(ready) != 1 {
		t.Errorf("expected 1 ready node, got %d", len(ready))
	}
	if ready[0].ID != a.ID {
		t.Errorf("expected node A to be ready")
	}

	// Mark A as completed
	a.State = NodeStateCompleted

	// Now B should be ready
	ready = g.GetReadyNodes()
	if len(ready) != 1 {
		t.Errorf("expected 1 ready node, got %d", len(ready))
	}
	if ready[0].ID != b.ID {
		t.Errorf("expected node B to be ready")
	}
}

func TestGraph_GetNodesByType(t *testing.T) {
	g := NewGraph("env", "dc")

	_ = g.AddNode(NewNode(NodeTypeDatabase, "app", "db1"))
	_ = g.AddNode(NewNode(NodeTypeDatabase, "app", "db2"))
	_ = g.AddNode(NewNode(NodeTypeDeployment, "app", "deploy"))

	dbs := g.GetNodesByType(NodeTypeDatabase)
	if len(dbs) != 2 {
		t.Errorf("expected 2 databases, got %d", len(dbs))
	}

	deploys := g.GetNodesByType(NodeTypeDeployment)
	if len(deploys) != 1 {
		t.Errorf("expected 1 deployment, got %d", len(deploys))
	}
}

func TestGraph_GetNodesByComponent(t *testing.T) {
	g := NewGraph("env", "dc")

	_ = g.AddNode(NewNode(NodeTypeDatabase, "app1", "db"))
	_ = g.AddNode(NewNode(NodeTypeDeployment, "app1", "deploy"))
	_ = g.AddNode(NewNode(NodeTypeDatabase, "app2", "db"))

	app1Nodes := g.GetNodesByComponent("app1")
	if len(app1Nodes) != 2 {
		t.Errorf("expected 2 nodes for app1, got %d", len(app1Nodes))
	}

	app2Nodes := g.GetNodesByComponent("app2")
	if len(app2Nodes) != 1 {
		t.Errorf("expected 1 node for app2, got %d", len(app2Nodes))
	}
}
