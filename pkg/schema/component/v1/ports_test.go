package v1

import (
	"testing"
)

func TestParser_ParseBytes_PortsFullObject(t *testing.T) {
	parser := &Parser{}

	yaml := `
ports:
  api:
    description: "Port for the API server"
  admin:
    description: "Port for the admin panel"

deployments:
  api:
    command: ["node", "server.js"]
    environment:
      PORT: ${{ ports.api.port }}
`

	schema, err := parser.ParseBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(schema.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(schema.Ports))
	}

	apiPort, ok := schema.Ports["api"]
	if !ok {
		t.Fatal("expected port 'api' to exist")
	}
	if apiPort.Description != "Port for the API server" {
		t.Errorf("expected description 'Port for the API server', got %q", apiPort.Description)
	}

	adminPort, ok := schema.Ports["admin"]
	if !ok {
		t.Fatal("expected port 'admin' to exist")
	}
	if adminPort.Description != "Port for the admin panel" {
		t.Errorf("expected description 'Port for the admin panel', got %q", adminPort.Description)
	}
}

func TestParser_ParseBytes_PortsBoolShorthand(t *testing.T) {
	parser := &Parser{}

	yaml := `
ports:
  api: true

deployments:
  api:
    command: ["node", "server.js"]
`

	schema, err := parser.ParseBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(schema.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(schema.Ports))
	}

	apiPort, ok := schema.Ports["api"]
	if !ok {
		t.Fatal("expected port 'api' to exist")
	}
	if apiPort.Description != "" {
		t.Errorf("expected empty description, got %q", apiPort.Description)
	}
}

func TestParser_ParseBytes_NoPorts(t *testing.T) {
	parser := &Parser{}

	yaml := `
deployments:
  inngest:
    command: ["npx", "inngest-cli@latest", "dev"]

services:
  inngest:
    deployment: inngest
    port: 8288
`

	schema, err := parser.ParseBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(schema.Ports) != 0 {
		t.Errorf("expected no ports, got %d", len(schema.Ports))
	}
}

func TestParser_ParseBytes_ServicePortExpression(t *testing.T) {
	parser := &Parser{}

	yaml := `
ports:
  api: true

deployments:
  api:
    command: ["node", "server.js"]

services:
  api:
    deployment: api
    port: ${{ ports.api.port }}
`

	schema, err := parser.ParseBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	svc, ok := schema.Services["api"]
	if !ok {
		t.Fatal("expected service 'api' to exist")
	}

	portStr := svc.PortAsString()
	if portStr != "${{ ports.api.port }}" {
		t.Errorf("expected port expression '${{ ports.api.port }}', got %q", portStr)
	}
}

func TestParser_ParseBytes_ServicePortInteger(t *testing.T) {
	parser := &Parser{}

	yaml := `
deployments:
  api:
    command: ["node", "server.js"]

services:
  api:
    deployment: api
    port: 8080
`

	schema, err := parser.ParseBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	svc, ok := schema.Services["api"]
	if !ok {
		t.Fatal("expected service 'api' to exist")
	}

	portStr := svc.PortAsString()
	if portStr != "8080" {
		t.Errorf("expected port '8080', got %q", portStr)
	}
}

func TestTransformer_TransformPorts(t *testing.T) {
	transformer := NewTransformer()

	schema := &SchemaV1{
		Ports: map[string]PortV1{
			"api":   {Description: "API port"},
			"admin": {Description: "Admin port"},
		},
	}

	ic, err := transformer.Transform(schema)
	if err != nil {
		t.Fatalf("failed to transform: %v", err)
	}

	if len(ic.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(ic.Ports))
	}

	// Find api port
	found := false
	for _, p := range ic.Ports {
		if p.Name == "api" {
			found = true
			if p.Description != "API port" {
				t.Errorf("expected description 'API port', got %q", p.Description)
			}
		}
	}
	if !found {
		t.Error("expected port 'api' in internal component")
	}
}

func TestTransformer_TransformServicePortExpression(t *testing.T) {
	transformer := NewTransformer()

	portExpr := "${{ ports.api.port }}"
	schema := &SchemaV1{
		Ports: map[string]PortV1{
			"api": {},
		},
		Deployments: map[string]DeploymentV1{
			"api": {
				Command: []string{"node", "server.js"},
			},
		},
		Services: map[string]ServiceV1{
			"api": {
				Deployment: "api",
				Port:       portExpr,
			},
		},
	}

	ic, err := transformer.Transform(schema)
	if err != nil {
		t.Fatalf("failed to transform: %v", err)
	}

	if len(ic.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(ic.Services))
	}

	svc := ic.Services[0]
	if svc.Port.Raw != portExpr {
		t.Errorf("expected port expression %q, got %q", portExpr, svc.Port.Raw)
	}
	if !svc.Port.IsTemplate {
		t.Error("expected port to be an expression template")
	}
}
