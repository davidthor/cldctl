package v1

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// parseExpr is a test helper that parses an HCL expression string.
func parseExpr(t *testing.T, src string) hcl.Expression {
	t.Helper()
	expr, diags := hclsyntax.ParseExpression([]byte(src), "test.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		t.Fatalf("failed to parse expression %q: %s", src, diags.Error())
	}
	return expr
}

func TestExprToString_LiteralString(t *testing.T) {
	expr := parseExpr(t, `"hello"`)
	result := exprToString(expr)
	if result != "hello" {
		t.Errorf("expected %q, got %q", "hello", result)
	}
}

func TestExprToString_LiteralNumber(t *testing.T) {
	expr := parseExpr(t, `42`)
	result := exprToString(expr)
	if result != "42" {
		t.Errorf("expected %q, got %q", "42", result)
	}
}

func TestExprToString_LiteralBool(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"true", "true"},
		{"false", "false"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			expr := parseExpr(t, tt.input)
			result := exprToString(expr)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestExprToString_ComplexExpression(t *testing.T) {
	// Write a temporary HCL file so exprToString can read back source text
	tmpDir := t.TempDir()
	hclFile := filepath.Join(tmpDir, "test.hcl")
	hclContent := `module.postgres.url`
	if err := os.WriteFile(hclFile, []byte(hclContent), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	// Parse the expression with the real filename so Range() points to the file
	expr, diags := hclsyntax.ParseExpression([]byte(hclContent), hclFile, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		t.Fatalf("failed to parse expression: %s", diags.Error())
	}

	result := exprToString(expr)
	if result != "module.postgres.url" {
		t.Errorf("expected %q, got %q", "module.postgres.url", result)
	}
}

func TestExprToString_FileNotReadable(t *testing.T) {
	// Parse with a non-existent filename
	expr, diags := hclsyntax.ParseExpression(
		[]byte("module.postgres.url"),
		"/nonexistent/test.hcl",
		hcl.Pos{Line: 1, Column: 1},
	)
	if diags.HasErrors() {
		t.Fatalf("failed to parse expression: %s", diags.Error())
	}

	result := exprToString(expr)
	// Should fall back to placeholder since the file can't be read
	if result != "<expression>" {
		t.Errorf("expected %q, got %q", "<expression>", result)
	}
}

func TestCtyValueToString_Null(t *testing.T) {
	result := ctyValueToString(cty.NullVal(cty.String))
	if result != "" {
		t.Errorf("expected empty string for null, got %q", result)
	}
}

func TestCtyValueToString_Types(t *testing.T) {
	tests := []struct {
		name     string
		input    cty.Value
		expected string
	}{
		{"string", cty.StringVal("hello"), "hello"},
		{"number_int", cty.NumberIntVal(42), "42"},
		{"number_float", cty.NumberFloatVal(3.14), "3.14"},
		{"bool_true", cty.BoolVal(true), "true"},
		{"bool_false", cty.BoolVal(false), "false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ctyValueToString(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
