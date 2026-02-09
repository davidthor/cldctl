package v1

import (
	"testing"
)

func TestParser_ParseBytes(t *testing.T) {
	parser := NewParser()

	hcl := `
variable "network_name" {
  description = "Name of the Docker network"
  default     = "cldctl-local"
}

module "network" {
  plugin = "native"
  build  = "./modules/docker-network"
  inputs {
    name = variable.network_name
  }
}

environment {
  database {
    when = true

    module "postgres" {
      plugin = "native"
      build  = "./modules/docker-postgres"
      inputs {
        version = "15"
      }
    }

    outputs {
      url = "postgresql://localhost:5432/db"
    }
  }

  deployment {
    module "container" {
      plugin = "native"
      build  = "./modules/docker-deployment"
    }
  }
}
`

	schema, diags, err := parser.ParseBytes([]byte(hcl), "test.hcl")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// Check for non-fatal diagnostics
	for _, d := range diags {
		if d.Severity == 0 { // Error
			t.Logf("diagnostic: %s", d.Summary)
		}
	}

	// Check variables
	if len(schema.Variables) != 1 {
		t.Errorf("expected 1 variable, got %d", len(schema.Variables))
	}

	if len(schema.Variables) > 0 {
		v := schema.Variables[0]
		if v.Name != "network_name" {
			t.Errorf("expected variable name 'network_name', got %q", v.Name)
		}
		// Note: type = string in HCL evaluates to type keyword, not "string" literal
		// This is expected behavior - type constraints work differently in HCL
		if v.Description != "Name of the Docker network" {
			t.Errorf("expected description 'Name of the Docker network', got %q", v.Description)
		}
	}

	// Check modules
	if len(schema.Modules) != 1 {
		t.Errorf("expected 1 top-level module, got %d", len(schema.Modules))
	}

	if len(schema.Modules) > 0 {
		m := schema.Modules[0]
		if m.Name != "network" {
			t.Errorf("expected module name 'network', got %q", m.Name)
		}
		if m.Plugin != "native" {
			t.Errorf("expected plugin 'native', got %q", m.Plugin)
		}
	}

	// Check environment
	if schema.Environment == nil {
		t.Fatal("expected environment block")
	}

	if len(schema.Environment.DatabaseHooks) != 1 {
		t.Errorf("expected 1 database hook, got %d", len(schema.Environment.DatabaseHooks))
	}

	if len(schema.Environment.DeploymentHooks) != 1 {
		t.Errorf("expected 1 deployment hook, got %d", len(schema.Environment.DeploymentHooks))
	}
}

func TestParser_WithVariables(t *testing.T) {
	parser := NewParser().
		WithVariable("env_name", "production").
		WithVariable("region", "us-east-1")

	hcl := `
module "test" {
  plugin = "native"
  build  = "./modules/test"
  inputs {
    name = variable.env_name
  }
}
`

	schema, _, err := parser.ParseBytes([]byte(hcl), "test.hcl")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(schema.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(schema.Modules))
	}

	// The inputs should have been evaluated with the variable context
	m := schema.Modules[0]
	if m.InputsEvaluated == nil {
		t.Error("expected inputs to be evaluated")
	}

	// Check that the variable was evaluated
	if m.InputsEvaluated != nil {
		name, ok := m.InputsEvaluated["name"]
		if !ok {
			t.Error("expected 'name' input to be present")
		} else if name.AsString() != "production" {
			t.Errorf("expected name to be 'production', got %q", name.AsString())
		}
	}
}

func TestParser_WithEnvironment(t *testing.T) {
	parser := NewParser().
		WithEnvironment(&EnvironmentContext{
			Name:       "staging",
			Datacenter: "local",
		})

	hcl := `
module "namespace" {
  plugin = "native"
  build  = "./modules/namespace"
}
`

	_, _, err := parser.ParseBytes([]byte(hcl), "test.hcl")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
}

func TestParser_Empty(t *testing.T) {
	parser := NewParser()

	schema, _, err := parser.ParseBytes([]byte(""), "test.hcl")
	if err != nil {
		t.Fatalf("failed to parse empty: %v", err)
	}

	if len(schema.Variables) != 0 {
		t.Error("expected no variables")
	}

	if len(schema.Modules) != 0 {
		t.Error("expected no modules")
	}

	if schema.Environment != nil {
		t.Error("expected no environment")
	}
}

func TestParser_InvalidHCL(t *testing.T) {
	parser := NewParser()

	invalidHCL := `
this is not valid HCL {
  missing = closing brace
`

	_, _, err := parser.ParseBytes([]byte(invalidHCL), "test.hcl")
	if err == nil {
		t.Error("expected error for invalid HCL")
	}
}

func TestParser_HookWithError(t *testing.T) {
	parser := NewParser()

	hcl := `
environment {
  database {
    when = true
    error = "This database type is not supported."
  }
}
`

	schema, diags, err := parser.ParseBytes([]byte(hcl), "test.hcl")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// Should not have fatal diagnostics
	for _, d := range diags {
		if d.Severity == 0 {
			t.Fatalf("unexpected error diagnostic: %s", d.Summary)
		}
	}

	if schema.Environment == nil {
		t.Fatal("expected environment block")
	}

	if len(schema.Environment.DatabaseHooks) != 1 {
		t.Fatalf("expected 1 database hook, got %d", len(schema.Environment.DatabaseHooks))
	}

	hook := schema.Environment.DatabaseHooks[0]
	if hook.Error != "This database type is not supported." {
		t.Errorf("expected error message, got %q", hook.Error)
	}
	if hook.ErrorExpr == nil {
		t.Error("expected ErrorExpr to be set")
	}
}

func TestParser_HookErrorMutualExclusivity_ErrorAndModule(t *testing.T) {
	parser := NewParser()

	hcl := `
environment {
  database {
    error = "Not supported"

    module "pg" {
      plugin = "native"
      build  = "./modules/pg"
    }
  }
}
`

	_, diags, _ := parser.ParseBytes([]byte(hcl), "test.hcl")

	foundError := false
	for _, d := range diags {
		if d.Summary == "Invalid hook: 'error' and 'module' are mutually exclusive" {
			foundError = true
			break
		}
	}

	if !foundError {
		t.Error("expected diagnostic error for error + module mutual exclusivity")
	}
}

func TestParser_HookErrorMutualExclusivity_ErrorAndOutputs(t *testing.T) {
	parser := NewParser()

	hcl := `
environment {
  database {
    error = "Not supported"

    outputs {
      url = "something"
    }
  }
}
`

	_, diags, _ := parser.ParseBytes([]byte(hcl), "test.hcl")

	foundError := false
	for _, d := range diags {
		if d.Summary == "Invalid hook: 'error' and 'outputs' are mutually exclusive" {
			foundError = true
			break
		}
	}

	if !foundError {
		t.Error("expected diagnostic error for error + outputs mutual exclusivity")
	}
}

func TestParser_HookReachability_CatchAllNotLast(t *testing.T) {
	parser := NewParser()

	hcl := `
environment {
  database {
    module "default" {
      plugin = "native"
      build  = "./modules/default-db"
    }
  }

  database {
    when = true

    module "pg" {
      plugin = "native"
      build  = "./modules/pg"
    }
  }
}
`

	_, diags, _ := parser.ParseBytes([]byte(hcl), "test.hcl")

	foundError := false
	for _, d := range diags {
		if d.Summary == "Unreachable database hook(s)" {
			foundError = true
			break
		}
	}

	if !foundError {
		t.Error("expected diagnostic error for unreachable hooks after catch-all")
	}
}

func TestParser_HookReachability_CatchAllLast(t *testing.T) {
	parser := NewParser()

	hcl := `
environment {
  database {
    when = true

    module "pg" {
      plugin = "native"
      build  = "./modules/pg"
    }
  }

  database {
    error = "Unsupported database type."
  }
}
`

	_, diags, err := parser.ParseBytes([]byte(hcl), "test.hcl")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	for _, d := range diags {
		if d.Summary == "Unreachable database hook(s)" {
			t.Error("catch-all as last hook should not produce reachability error")
		}
	}
}

func TestParser_HookReachability_AllHooksHaveWhen(t *testing.T) {
	parser := NewParser()

	hcl := `
environment {
  database {
    when = true

    module "pg" {
      plugin = "native"
      build  = "./modules/pg"
    }
  }

  database {
    when = true

    module "mysql" {
      plugin = "native"
      build  = "./modules/mysql"
    }
  }
}
`

	_, diags, err := parser.ParseBytes([]byte(hcl), "test.hcl")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	for _, d := range diags {
		if d.Summary == "Unreachable database hook(s)" {
			t.Error("all hooks have when conditions, should not produce reachability error")
		}
	}
}

func TestParser_ExtendsImage(t *testing.T) {
	parser := NewParser()

	hcl := `
extends = {
  image = "ghcr.io/myorg/my-dc:v1"
}

environment {
  database {
    when = true
    module "pg" {
      plugin = "native"
      build  = "./modules/pg"
    }
    outputs {
      url = "test"
    }
  }
}
`

	schema, diags, err := parser.ParseBytes([]byte(hcl), "test.hcl")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	for _, d := range diags {
		if d.Severity == 0 {
			t.Fatalf("unexpected error diagnostic: %s - %s", d.Summary, d.Detail)
		}
	}

	if schema.Extends == nil {
		t.Fatal("expected extends block")
	}

	if schema.Extends.Image != "ghcr.io/myorg/my-dc:v1" {
		t.Errorf("expected image 'ghcr.io/myorg/my-dc:v1', got %q", schema.Extends.Image)
	}

	if schema.Extends.Path != "" {
		t.Errorf("expected empty path, got %q", schema.Extends.Path)
	}
}

func TestParser_ExtendsPath(t *testing.T) {
	parser := NewParser()

	hcl := `
extends = {
  path = "../base-datacenter"
}

environment {
  database {
    when = true
    error = "not supported"
  }
}
`

	schema, diags, err := parser.ParseBytes([]byte(hcl), "test.hcl")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	for _, d := range diags {
		if d.Severity == 0 {
			t.Fatalf("unexpected error diagnostic: %s - %s", d.Summary, d.Detail)
		}
	}

	if schema.Extends == nil {
		t.Fatal("expected extends block")
	}

	if schema.Extends.Path != "../base-datacenter" {
		t.Errorf("expected path '../base-datacenter', got %q", schema.Extends.Path)
	}

	if schema.Extends.Image != "" {
		t.Errorf("expected empty image, got %q", schema.Extends.Image)
	}
}

func TestParser_ExtendsMutualExclusivity(t *testing.T) {
	parser := NewParser()

	hcl := `
extends = {
  image = "ghcr.io/myorg/my-dc:v1"
  path  = "../base-datacenter"
}
`

	_, diags, _ := parser.ParseBytes([]byte(hcl), "test.hcl")

	foundError := false
	for _, d := range diags {
		if d.Summary == "Invalid extends: 'image' and 'path' are mutually exclusive" {
			foundError = true
			break
		}
	}

	if !foundError {
		t.Error("expected diagnostic error for extends with both image and path")
	}
}

func TestParser_ExtendsEmpty(t *testing.T) {
	parser := NewParser()

	hcl := `
extends = {}
`

	_, diags, _ := parser.ParseBytes([]byte(hcl), "test.hcl")

	foundError := false
	for _, d := range diags {
		if d.Summary == "Invalid extends: missing 'image' or 'path'" {
			foundError = true
			break
		}
	}

	if !foundError {
		t.Error("expected diagnostic error for empty extends")
	}
}

func TestParser_NoExtends(t *testing.T) {
	parser := NewParser()

	hcl := `
environment {
  deployment {
    module "deploy" {
      plugin = "native"
      build  = "./modules/deploy"
    }
  }
}
`

	schema, _, err := parser.ParseBytes([]byte(hcl), "test.hcl")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if schema.Extends != nil {
		t.Error("expected no extends block")
	}
}
