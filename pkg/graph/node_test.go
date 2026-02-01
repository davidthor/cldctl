package graph

import (
	"testing"
)

func TestNewNode(t *testing.T) {
	node := NewNode(NodeTypeDatabase, "my-app", "main")

	expectedID := "my-app/database/main"
	if node.ID != expectedID {
		t.Errorf("expected ID %q, got %q", expectedID, node.ID)
	}

	if node.Type != NodeTypeDatabase {
		t.Errorf("expected type %s, got %s", NodeTypeDatabase, node.Type)
	}

	if node.Component != "my-app" {
		t.Errorf("expected component 'my-app', got %q", node.Component)
	}

	if node.Name != "main" {
		t.Errorf("expected name 'main', got %q", node.Name)
	}

	if node.State != NodeStatePending {
		t.Errorf("expected state %s, got %s", NodeStatePending, node.State)
	}
}

func TestNode_AddDependency(t *testing.T) {
	node := NewNode(NodeTypeDeployment, "app", "api")

	node.AddDependency("dep1")
	node.AddDependency("dep2")
	node.AddDependency("dep1") // Duplicate

	if len(node.DependsOn) != 2 {
		t.Errorf("expected 2 dependencies, got %d", len(node.DependsOn))
	}
}

func TestNode_AddDependent(t *testing.T) {
	node := NewNode(NodeTypeDatabase, "app", "main")

	node.AddDependent("dep1")
	node.AddDependent("dep2")
	node.AddDependent("dep1") // Duplicate

	if len(node.DependedOnBy) != 2 {
		t.Errorf("expected 2 dependents, got %d", len(node.DependedOnBy))
	}
}

func TestNode_SetInput(t *testing.T) {
	node := NewNode(NodeTypeDatabase, "app", "main")

	node.SetInput("type", "postgres")
	node.SetInput("version", "15")

	if node.Inputs["type"] != "postgres" {
		t.Error("expected type input to be set")
	}

	if node.Inputs["version"] != "15" {
		t.Error("expected version input to be set")
	}
}

func TestNode_SetOutput(t *testing.T) {
	node := NewNode(NodeTypeDatabase, "app", "main")

	node.SetOutput("url", "postgres://localhost:5432/db")
	node.SetOutput("host", "localhost")

	if node.Outputs["url"] != "postgres://localhost:5432/db" {
		t.Error("expected url output to be set")
	}

	if node.Outputs["host"] != "localhost" {
		t.Error("expected host output to be set")
	}
}

func TestNode_IsReady(t *testing.T) {
	g := NewGraph("env", "dc")

	db := NewNode(NodeTypeDatabase, "app", "main")
	deploy := NewNode(NodeTypeDeployment, "app", "api")

	_ = g.AddNode(db)
	_ = g.AddNode(deploy)

	deploy.AddDependency(db.ID)
	db.AddDependent(deploy.ID)

	// db should be ready (no dependencies)
	if !db.IsReady(g) {
		t.Error("db should be ready")
	}

	// deploy should not be ready (db not completed)
	if deploy.IsReady(g) {
		t.Error("deploy should not be ready")
	}

	// Mark db as completed
	db.State = NodeStateCompleted

	// Now deploy should be ready
	if !deploy.IsReady(g) {
		t.Error("deploy should be ready after db completed")
	}

	// Mark deploy as running
	deploy.State = NodeStateRunning

	// Running nodes are not "ready"
	if deploy.IsReady(g) {
		t.Error("running node should not be ready")
	}
}
