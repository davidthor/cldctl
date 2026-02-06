package graph

import (
	"testing"

	"github.com/architect-io/arcctl/pkg/schema/component"
)

// loadComponent is a test helper that parses a YAML component specification.
func loadComponent(t *testing.T, yaml string) component.Component {
	t.Helper()
	loader := component.NewLoader()
	comp, err := loader.LoadFromBytes([]byte(yaml), "/tmp/test/architect.yml")
	if err != nil {
		t.Fatalf("failed to load component: %v", err)
	}
	return comp
}

func TestBuilder_AddComponent_TaskFromMigration(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16
    migrations:
      image: my-app-migrations:latest
      command: ["npm", "run", "migrate"]
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Should have database node
	dbNode := g.GetNode("my-app/database/main")
	if dbNode == nil {
		t.Fatal("expected database node to exist")
	}
	if dbNode.Type != NodeTypeDatabase {
		t.Errorf("expected database node type, got %s", dbNode.Type)
	}

	// Should have task node (not migration node)
	taskNode := g.GetNode("my-app/task/main-migration")
	if taskNode == nil {
		t.Fatal("expected task node to exist")
	}
	if taskNode.Type != NodeTypeTask {
		t.Errorf("expected task node type, got %s", taskNode.Type)
	}

	// Task should depend on database
	hasDep := false
	for _, dep := range taskNode.DependsOn {
		if dep == dbNode.ID {
			hasDep = true
			break
		}
	}
	if !hasDep {
		t.Error("expected task node to depend on database node")
	}
}

func TestBuilder_TaskDependencyInsertion(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16
    migrations:
      image: my-app-migrations:latest
      command: ["npm", "run", "migrate"]

deployments:
  api:
    image: my-app:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"

functions:
  web:
    src:
      path: ./web
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	dbNode := g.GetNode("my-app/database/main")
	taskNode := g.GetNode("my-app/task/main-migration")
	deployNode := g.GetNode("my-app/deployment/api")
	fnNode := g.GetNode("my-app/function/web")

	if dbNode == nil || taskNode == nil || deployNode == nil || fnNode == nil {
		t.Fatal("expected all nodes to exist")
	}

	// Deployment should depend on task (not just database)
	deployDependsOnTask := false
	for _, dep := range deployNode.DependsOn {
		if dep == taskNode.ID {
			deployDependsOnTask = true
			break
		}
	}
	if !deployDependsOnTask {
		t.Error("expected deployment to depend on task node")
	}

	// Function should depend on task (not just database)
	fnDependsOnTask := false
	for _, dep := range fnNode.DependsOn {
		if dep == taskNode.ID {
			fnDependsOnTask = true
			break
		}
	}
	if !fnDependsOnTask {
		t.Error("expected function to depend on task node")
	}

	// Deployment should also depend on database directly
	deployDependsOnDB := false
	for _, dep := range deployNode.DependsOn {
		if dep == dbNode.ID {
			deployDependsOnDB = true
			break
		}
	}
	if !deployDependsOnDB {
		t.Error("expected deployment to also depend on database node")
	}

	// Task should depend on database
	taskDependsOnDB := false
	for _, dep := range taskNode.DependsOn {
		if dep == dbNode.ID {
			taskDependsOnDB = true
			break
		}
	}
	if !taskDependsOnDB {
		t.Error("expected task to depend on database node")
	}

	// Verify topological sort produces correct order: database -> task -> deployment/function
	sorted, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("unexpected topological sort error: %v", err)
	}

	nodeIndex := make(map[string]int)
	for i, n := range sorted {
		nodeIndex[n.ID] = i
	}

	if nodeIndex[dbNode.ID] >= nodeIndex[taskNode.ID] {
		t.Error("database should come before task in topological order")
	}
	if nodeIndex[taskNode.ID] >= nodeIndex[deployNode.ID] {
		t.Error("task should come before deployment in topological order")
	}
	if nodeIndex[taskNode.ID] >= nodeIndex[fnNode.ID] {
		t.Error("task should come before function in topological order")
	}
}

func TestBuilder_NoTaskNoDependencyInsertion(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	// Database WITHOUT migrations
	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16

deployments:
  api:
    image: my-app:latest
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// No task node should exist
	taskNodes := g.GetNodesByType(NodeTypeTask)
	if len(taskNodes) != 0 {
		t.Errorf("expected 0 task nodes, got %d", len(taskNodes))
	}

	// Deployment should depend directly on database
	deployNode := g.GetNode("my-app/deployment/api")
	if deployNode == nil {
		t.Fatal("expected deployment node to exist")
	}

	deployDependsOnDB := false
	for _, dep := range deployNode.DependsOn {
		if dep == "my-app/database/main" {
			deployDependsOnDB = true
			break
		}
	}
	if !deployDependsOnDB {
		t.Error("expected deployment to depend on database node")
	}
}

func TestBuilder_CronjobDependsOnTask(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
databases:
  main:
    type: postgres:^16
    migrations:
      image: my-app-migrations:latest
      command: ["npm", "run", "migrate"]

cronjobs:
  cleanup:
    image: my-app-cleanup:latest
    schedule: "0 2 * * *"
    command: ["npm", "run", "cleanup"]
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	taskNode := g.GetNode("my-app/task/main-migration")
	cronNode := g.GetNode("my-app/cronjob/cleanup")

	if taskNode == nil || cronNode == nil {
		t.Fatal("expected task and cronjob nodes to exist")
	}

	// Cronjob should depend on task
	cronDependsOnTask := false
	for _, dep := range cronNode.DependsOn {
		if dep == taskNode.ID {
			cronDependsOnTask = true
			break
		}
	}
	if !cronDependsOnTask {
		t.Error("expected cronjob to depend on task node")
	}
}
