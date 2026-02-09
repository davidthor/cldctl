package graph

import (
	"testing"
)

func TestBuilder_AddComponent_PortNodes(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
ports:
  api:
    description: "API server port"

deployments:
  api:
    command: ["node", "server.js"]
    environment:
      PORT: ${{ ports.api.port }}
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Should have port node
	portNode := g.GetNode("my-app/port/api")
	if portNode == nil {
		t.Fatal("expected port node to exist")
	}
	if portNode.Type != NodeTypePort {
		t.Errorf("expected port node type, got %s", portNode.Type)
	}

	// Port node should have description in inputs
	desc, ok := portNode.Inputs["description"]
	if !ok {
		t.Error("expected description input")
	}
	if desc != "API server port" {
		t.Errorf("expected description 'API server port', got %v", desc)
	}

	// Deployment should depend on port node (because of ${{ ports.api.port }} expression)
	deployNode := g.GetNode("my-app/deployment/api")
	if deployNode == nil {
		t.Fatal("expected deployment node to exist")
	}
	hasDep := false
	for _, dep := range deployNode.DependsOn {
		if dep == portNode.ID {
			hasDep = true
			break
		}
	}
	if !hasDep {
		t.Errorf("expected deployment to depend on port node, deps: %v", deployNode.DependsOn)
	}
}

func TestBuilder_AddComponent_ServicePortExpressionDependency(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
ports:
  api: true

deployments:
  api:
    command: ["node", "server.js"]

services:
  api:
    deployment: api
    port: ${{ ports.api.port }}
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Port node should exist
	portNode := g.GetNode("my-app/port/api")
	if portNode == nil {
		t.Fatal("expected port node to exist")
	}

	// Service should depend on port node
	serviceNode := g.GetNode("my-app/service/api")
	if serviceNode == nil {
		t.Fatal("expected service node to exist")
	}
	hasDep := false
	for _, dep := range serviceNode.DependsOn {
		if dep == portNode.ID {
			hasDep = true
			break
		}
	}
	if !hasDep {
		t.Errorf("expected service to depend on port node, deps: %v", serviceNode.DependsOn)
	}
}

func TestBuilder_AddComponent_NoPortNodes(t *testing.T) {
	builder := NewBuilder("test-env", "test-dc")

	comp := loadComponent(t, `
deployments:
  inngest:
    command: ["npx", "inngest-cli@latest", "dev"]

services:
  inngest:
    deployment: inngest
    port: 8288
`)

	err := builder.AddComponent("my-app", comp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := builder.Build()

	// Should NOT have any port nodes
	for _, node := range g.Nodes {
		if node.Type == NodeTypePort {
			t.Errorf("expected no port nodes, found %s", node.ID)
		}
	}
}
