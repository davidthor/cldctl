package graph

import (
	"testing"
)

func TestBuilder_ObservabilityNode(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
observability: true

deployments:
  api:
    image: nginx:latest
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Should have observability node
	obsNode := g.GetNode("my-app/observability/observability")
	if obsNode == nil {
		t.Fatal("expected observability node to exist")
	}
	if obsNode.Type != NodeTypeObservability {
		t.Errorf("expected observability node type, got %s", obsNode.Type)
	}

	// inject should default to false with boolean shorthand
	if inject, ok := obsNode.Inputs["inject"].(bool); !ok || inject {
		t.Error("expected inject input to be false (default)")
	}
}

func TestBuilder_ObservabilityWorkloadDependency(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
observability: true

deployments:
  api:
    image: nginx:latest

functions:
  web:
    src:
      path: ./web

cronjobs:
  cleanup:
    image: cleanup:latest
    schedule: "0 2 * * *"
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	obsNode := g.GetNode("my-app/observability/observability")
	deployNode := g.GetNode("my-app/deployment/api")
	fnNode := g.GetNode("my-app/function/web")
	cronNode := g.GetNode("my-app/cronjob/cleanup")

	if obsNode == nil || deployNode == nil || fnNode == nil || cronNode == nil {
		t.Fatal("expected all nodes to exist")
	}

	assertDependsOn(t, deployNode, obsNode.ID, "deployment should depend on observability")
	assertDependsOn(t, fnNode, obsNode.ID, "function should depend on observability")
	assertDependsOn(t, cronNode, obsNode.ID, "cronjob should depend on observability")

	if len(obsNode.DependedOnBy) != 3 {
		t.Errorf("expected observability to have 3 dependents, got %d", len(obsNode.DependedOnBy))
	}
}

func TestBuilder_NoObservabilityNode_WhenOmitted(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
deployments:
  api:
    image: nginx:latest
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	obsNode := g.GetNode("my-app/observability/observability")
	if obsNode != nil {
		t.Error("expected no observability node when omitted")
	}
}

func TestBuilder_NoObservabilityNode_WhenDisabled(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
observability: false

deployments:
  api:
    image: nginx:latest
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	obsNode := g.GetNode("my-app/observability/observability")
	if obsNode != nil {
		t.Error("expected no observability node when disabled")
	}
}

func TestBuilder_ObservabilityTopologicalOrder(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
observability: true

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

	sorted, err := g.TopologicalSort()
	if err != nil {
		t.Fatalf("unexpected topological sort error: %v", err)
	}

	nodeIndex := make(map[string]int)
	for i, n := range sorted {
		nodeIndex[n.ID] = i
	}

	obsIdx := nodeIndex["my-app/observability/observability"]
	deployIdx := nodeIndex["my-app/deployment/api"]

	if obsIdx >= deployIdx {
		t.Error("observability should come before deployment in topological order")
	}
}

func TestBuilder_ObservabilityCustomAttributes(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
observability:
  inject: true
  attributes:
    team: payments

deployments:
  api:
    image: nginx:latest
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	obsNode := g.GetNode("my-app/observability/observability")
	if obsNode == nil {
		t.Fatal("expected observability node to exist")
	}

	// Check inject input
	if inject, ok := obsNode.Inputs["inject"].(bool); !ok || !inject {
		t.Error("expected inject to be true")
	}

	// Check attributes
	if attrs, ok := obsNode.Inputs["attributes"].(map[string]string); ok {
		if attrs["team"] != "payments" {
			t.Errorf("expected team=payments, got %s", attrs["team"])
		}
	} else {
		t.Error("expected attributes input to be map[string]string")
	}
}

// assertDependsOn is a test helper that checks if a node depends on the given nodeID.
func assertDependsOn(t *testing.T, node *Node, depID, msg string) {
	t.Helper()
	for _, dep := range node.DependsOn {
		if dep == depID {
			return
		}
	}
	t.Error(msg)
}
