package expression

import (
	"testing"
)

func TestEvaluator_Evaluate(t *testing.T) {
	parser := NewParser()
	evaluator := NewEvaluator()

	ctx := NewEvalContext()
	ctx.Databases["main"] = DatabaseOutputs{
		Host:     "localhost",
		Port:     5432,
		Database: "mydb",
		Username: "user",
		Password: "secret",
		URL:      "postgresql://user:secret@localhost:5432/mydb",
	}
	ctx.Services["api"] = ServiceOutputs{
		URL:      "http://api:8080",
		Host:     "api",
		Port:     8080,
		Protocol: "http",
	}
	ctx.Variables["log_level"] = "debug"

	tests := []struct {
		name    string
		input   string
		want    interface{}
		wantErr bool
	}{
		{
			name:  "literal string",
			input: "hello",
			want:  "hello",
		},
		{
			name:  "database url",
			input: "${{ databases.main.url }}",
			want:  "postgresql://user:secret@localhost:5432/mydb",
		},
		{
			name:  "database host",
			input: "${{ databases.main.host }}",
			want:  "localhost",
		},
		{
			name:  "database port",
			input: "${{ databases.main.port }}",
			want:  5432,
		},
		{
			name:  "service url",
			input: "${{ services.api.url }}",
			want:  "http://api:8080",
		},
		{
			name:  "variable",
			input: "${{ variables.log_level }}",
			want:  "debug",
		},
		{
			name:  "concatenation",
			input: "Host: ${{ databases.main.host }}",
			want:  "Host: localhost",
		},
		{
			name:    "unknown database",
			input:   "${{ databases.unknown.url }}",
			wantErr: true,
		},
		{
			name:    "unknown property",
			input:   "${{ databases.main.invalid }}",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.Parse(tt.input)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			got, err := evaluator.Evaluate(expr, ctx)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("evaluate error: %v", err)
			}

			if got != tt.want {
				t.Errorf("expected %v (%T), got %v (%T)", tt.want, tt.want, got, got)
			}
		})
	}
}

func TestEvaluator_BuildsExpression(t *testing.T) {
	parser := NewParser()
	evaluator := NewEvaluator()

	ctx := NewEvalContext()
	ctx.Builds["api"] = BuildOutputs{
		Image: "ghcr.io/org/api:v1",
	}

	tests := []struct {
		name    string
		input   string
		want    interface{}
		wantErr bool
	}{
		{
			name:  "builds image reference",
			input: "${{ builds.api.image }}",
			want:  "ghcr.io/org/api:v1",
		},
		{
			name:    "builds unknown name",
			input:   "${{ builds.unknown.image }}",
			wantErr: true,
		},
		{
			name:    "builds unknown property",
			input:   "${{ builds.api.tag }}",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.Parse(tt.input)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			got, err := evaluator.Evaluate(expr, ctx)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("expected %v (%T), got %v (%T)", tt.want, tt.want, got, got)
			}
		})
	}
}

func TestEvaluator_EvaluateString(t *testing.T) {
	parser := NewParser()
	evaluator := NewEvaluator()

	ctx := NewEvalContext()
	ctx.Databases["main"] = DatabaseOutputs{
		Port: 5432,
	}

	expr, _ := parser.Parse("${{ databases.main.port }}")
	got, err := evaluator.EvaluateString(expr, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != "5432" {
		t.Errorf("expected '5432', got %q", got)
	}
}
