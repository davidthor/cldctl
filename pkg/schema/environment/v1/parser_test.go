package v1

import (
	"testing"
)

func TestParser_ParseBytes(t *testing.T) {
	parser := NewParser()

	yaml := `
locals:
  base_domain: example.com
  log_level: debug

components:
  # Local component - uses path
  api:
    path: ./api
    variables:
      log_level: debug
    scaling:
      main:
        replicas: 3
        cpu: "0.5"
        memory: "512Mi"
    routes:
      public:
        hostnames:
          - subdomain: api
        tls:
          enabled: true

  # OCI component - uses image
  registry.example.com/worker:
    image: registry.example.com/worker:v1.0.0
    variables:
      queue: redis
`

	schema, err := parser.ParseBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// Check locals
	if len(schema.Locals) != 2 {
		t.Errorf("expected 2 locals, got %d", len(schema.Locals))
	}

	if schema.Locals["base_domain"] != "example.com" {
		t.Errorf("expected base_domain 'example.com', got %v", schema.Locals["base_domain"])
	}

	// Check components
	if len(schema.Components) != 2 {
		t.Errorf("expected 2 components, got %d", len(schema.Components))
	}

	// Check api component (local path)
	api, ok := schema.Components["api"]
	if !ok {
		t.Fatal("expected 'api' component")
	}

	if api.Path != "./api" {
		t.Errorf("expected path './api', got %q", api.Path)
	}
	if api.Image != "" {
		t.Errorf("expected empty image for path-based component, got %q", api.Image)
	}

	if len(api.Scaling) != 1 {
		t.Errorf("expected 1 scaling config, got %d", len(api.Scaling))
	}

	if scaling, ok := api.Scaling["main"]; ok {
		if scaling.Replicas != 3 {
			t.Errorf("expected 3 replicas, got %d", scaling.Replicas)
		}
		if scaling.CPU != "0.5" {
			t.Errorf("expected CPU '0.5', got %q", scaling.CPU)
		}
	} else {
		t.Error("expected 'main' scaling config")
	}

	if len(api.Routes) != 1 {
		t.Errorf("expected 1 route config, got %d", len(api.Routes))
	}

	// Check worker component (OCI image reference)
	worker, ok := schema.Components["registry.example.com/worker"]
	if !ok {
		t.Fatal("expected 'registry.example.com/worker' component")
	}

	if worker.Image != "registry.example.com/worker:v1.0.0" {
		t.Errorf("expected image 'registry.example.com/worker:v1.0.0', got %q", worker.Image)
	}
	if worker.Path != "" {
		t.Errorf("expected empty path for image-based component, got %q", worker.Path)
	}
}

func TestParser_ParseBytes_Empty(t *testing.T) {
	parser := NewParser()

	schema, err := parser.ParseBytes([]byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(schema.Components) != 0 {
		t.Errorf("expected 0 components, got %d", len(schema.Components))
	}
}

func TestParser_ParseBytes_Invalid(t *testing.T) {
	parser := NewParser()

	invalidYAML := `
components:
  - invalid list format
`

	_, err := parser.ParseBytes([]byte(invalidYAML))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}
