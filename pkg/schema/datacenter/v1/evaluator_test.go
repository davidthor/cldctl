package v1

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

func TestEvaluator_EvaluateWhen(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		node     *NodeContext
		expected bool
	}{
		{
			name:     "simple true",
			expr:     "true",
			expected: true,
		},
		{
			name:     "simple false",
			expr:     "false",
			expected: false,
		},
		{
			name: "node input equals",
			expr: "node.inputs.type == \"postgres\"",
			node: &NodeContext{
				Type:      "database",
				Name:      "main",
				Component: "app",
				Inputs: map[string]cty.Value{
					"type": cty.StringVal("postgres"),
				},
			},
			expected: true,
		},
		{
			name: "node input not equals",
			expr: "node.inputs.type == \"mysql\"",
			node: &NodeContext{
				Type:      "database",
				Name:      "main",
				Component: "app",
				Inputs: map[string]cty.Value{
					"type": cty.StringVal("postgres"),
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval := NewEvaluator()
			if tt.node != nil {
				eval.ctx.Node = tt.node
			}

			expr, diags := hclsyntax.ParseExpression([]byte(tt.expr), "test.hcl", hcl.Pos{})
			if diags.HasErrors() {
				t.Fatalf("failed to parse expression: %s", diags.Error())
			}

			result, err := eval.EvaluateWhen(expr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestEvaluator_SetNodeContext(t *testing.T) {
	eval := NewEvaluator()

	inputs := map[string]interface{}{
		"image":    "nginx:latest",
		"replicas": 3,
	}

	eval.SetNodeContext("deployment", "api", "my-app", inputs)

	if eval.ctx.Node == nil {
		t.Fatal("expected node context to be set")
	}

	if eval.ctx.Node.Type != "deployment" {
		t.Errorf("expected type 'deployment', got %q", eval.ctx.Node.Type)
	}

	if eval.ctx.Node.Name != "api" {
		t.Errorf("expected name 'api', got %q", eval.ctx.Node.Name)
	}

	if eval.ctx.Node.Component != "my-app" {
		t.Errorf("expected component 'my-app', got %q", eval.ctx.Node.Component)
	}
}

func TestEvaluator_AddModuleOutputs(t *testing.T) {
	eval := NewEvaluator()

	outputs := map[string]interface{}{
		"host": "localhost",
		"port": 5432,
		"url":  "postgresql://localhost:5432/db",
	}

	eval.AddModuleOutputs("postgres", outputs)

	if len(eval.ctx.Modules) != 1 {
		t.Errorf("expected 1 module, got %d", len(eval.ctx.Modules))
	}
}

func TestEvaluator_SetEnvironmentContext(t *testing.T) {
	eval := NewEvaluator()

	eval.SetEnvironmentContext("staging", "local", "account-123", "us-east-1")

	if eval.ctx.Environment == nil {
		t.Fatal("expected environment context to be set")
	}

	if eval.ctx.Environment.Name != "staging" {
		t.Errorf("expected name 'staging', got %q", eval.ctx.Environment.Name)
	}

	if eval.ctx.Environment.Datacenter != "local" {
		t.Errorf("expected datacenter 'local', got %q", eval.ctx.Environment.Datacenter)
	}
}

func TestEvaluator_SetVariables(t *testing.T) {
	eval := NewEvaluator()

	vars := map[string]interface{}{
		"network_name": "my-network",
		"port":         8080,
	}

	eval.SetVariables(vars)

	if len(eval.ctx.Variables) != 2 {
		t.Errorf("expected 2 variables, got %d", len(eval.ctx.Variables))
	}
}

func TestEvalContext_Clone(t *testing.T) {
	ctx := NewEvalContext()
	ctx.WithVariable("foo", "bar")
	ctx.WithEnvironment(&EnvironmentContext{Name: "test"})

	cloned := ctx.Clone()

	// Modify original
	ctx.WithVariable("baz", "qux")

	// Cloned should not have the new variable
	if len(cloned.Variables) != 1 {
		t.Errorf("expected 1 variable in clone, got %d", len(cloned.Variables))
	}

	// But should have the original
	if _, ok := cloned.Variables["foo"]; !ok {
		t.Error("expected 'foo' variable in clone")
	}

	// Environment should be shared (pointer)
	if cloned.Environment != ctx.Environment {
		t.Error("expected environment to be shared")
	}
}

func TestEvaluator_EvaluateErrorMessage(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		node     *NodeContext
		expected string
	}{
		{
			name:     "simple string literal",
			expr:     `"MongoDB is not supported."`,
			expected: "MongoDB is not supported.",
		},
		{
			name: "interpolation with node input",
			expr: `"Unsupported type: ${node.inputs.type}"`,
			node: &NodeContext{
				Type:      "database",
				Name:      "main",
				Component: "app",
				Inputs: map[string]cty.Value{
					"type": cty.StringVal("mongodb"),
				},
			},
			expected: "Unsupported type: mongodb",
		},
		{
			name: "interpolation with multiple inputs",
			expr: `"Component ${node.component} requested unsupported database ${node.inputs.type}"`,
			node: &NodeContext{
				Type:      "database",
				Name:      "main",
				Component: "my-app",
				Inputs: map[string]cty.Value{
					"type": cty.StringVal("mongodb"),
				},
			},
			expected: "Component my-app requested unsupported database mongodb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval := NewEvaluator()
			if tt.node != nil {
				eval.ctx.Node = tt.node
			}

			expr, diags := hclsyntax.ParseExpression([]byte(tt.expr), "test.hcl", hcl.Pos{Line: 1, Column: 1})
			if diags.HasErrors() {
				// Try parsing as template (for interpolated strings)
				expr, diags = hclsyntax.ParseTemplate([]byte(tt.expr), "test.hcl", hcl.Pos{Line: 1, Column: 1})
				if diags.HasErrors() {
					t.Fatalf("failed to parse expression: %s", diags.Error())
				}
			}

			result, err := eval.EvaluateErrorMessage(expr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestEvaluator_EvaluateHook_ErrorHook(t *testing.T) {
	eval := NewEvaluator()
	eval.ctx.Node = &NodeContext{
		Type:      "database",
		Name:      "main",
		Component: "app",
		Inputs: map[string]cty.Value{
			"type": cty.StringVal("mongodb"),
		},
	}

	// Parse the error expression
	errorExpr, diags := hclsyntax.ParseExpression(
		[]byte(`"MongoDB is not supported."`),
		"test.hcl",
		hcl.Pos{Line: 1, Column: 1},
	)
	if diags.HasErrors() {
		t.Fatalf("failed to parse error expression: %s", diags.Error())
	}

	hook := &HookBlockV1{
		ErrorExpr: errorExpr,
		Error:     "MongoDB is not supported.",
	}

	result, err := eval.EvaluateHook(hook)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if !result.IsError {
		t.Error("expected IsError to be true")
	}

	if result.ErrorMessage != "MongoDB is not supported." {
		t.Errorf("expected error message 'MongoDB is not supported.', got %q", result.ErrorMessage)
	}
}

func TestEvaluator_EvaluateHook_ErrorHookSkippedWhenNoMatch(t *testing.T) {
	eval := NewEvaluator()
	eval.ctx.Node = &NodeContext{
		Type:      "database",
		Name:      "main",
		Component: "app",
		Inputs: map[string]cty.Value{
			"type": cty.StringVal("postgres"),
		},
	}

	// when condition that won't match
	whenExpr, diags := hclsyntax.ParseExpression(
		[]byte(`node.inputs.type == "mongodb"`),
		"test.hcl",
		hcl.Pos{Line: 1, Column: 1},
	)
	if diags.HasErrors() {
		t.Fatalf("failed to parse when expression: %s", diags.Error())
	}

	errorExpr, diags := hclsyntax.ParseExpression(
		[]byte(`"MongoDB not supported."`),
		"test.hcl",
		hcl.Pos{Line: 1, Column: 1},
	)
	if diags.HasErrors() {
		t.Fatalf("failed to parse error expression: %s", diags.Error())
	}

	hook := &HookBlockV1{
		WhenExpr:  whenExpr,
		ErrorExpr: errorExpr,
		Error:     "MongoDB not supported.",
	}

	result, err := eval.EvaluateHook(hook)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != nil {
		t.Error("expected nil result when when condition doesn't match")
	}
}

func TestToCtyValue(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		check func(cty.Value) bool
	}{
		{
			name:  "string",
			input: "hello",
			check: func(v cty.Value) bool { return v.Type() == cty.String && v.AsString() == "hello" },
		},
		{
			name:  "int",
			input: 42,
			check: func(v cty.Value) bool { return v.Type() == cty.Number },
		},
		{
			name:  "bool",
			input: true,
			check: func(v cty.Value) bool { return v.Type() == cty.Bool && v.True() },
		},
		{
			name:  "string slice",
			input: []string{"a", "b"},
			check: func(v cty.Value) bool { return v.Type().IsListType() && v.LengthInt() == 2 },
		},
		{
			name:  "map",
			input: map[string]interface{}{"key": "value"},
			check: func(v cty.Value) bool { return v.Type().IsObjectType() },
		},
		{
			name:  "nil",
			input: nil,
			check: func(v cty.Value) bool { return v.IsNull() },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toCtyValue(tt.input)
			if !tt.check(result) {
				t.Errorf("check failed for input %v, got %v", tt.input, result.GoString())
			}
		})
	}
}

func TestFromCtyValue(t *testing.T) {
	tests := []struct {
		name     string
		input    cty.Value
		expected interface{}
	}{
		{
			name:     "string",
			input:    cty.StringVal("hello"),
			expected: "hello",
		},
		{
			name:     "int",
			input:    cty.NumberIntVal(42),
			expected: int64(42),
		},
		{
			name:     "bool",
			input:    cty.True,
			expected: true,
		},
		{
			name:     "null",
			input:    cty.NullVal(cty.String),
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fromCtyValue(tt.input)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
