package expression

import (
	"testing"
)

func TestEvaluator_ObservabilityEndpoint(t *testing.T) {
	parser := NewParser()
	evaluator := NewEvaluator()

	ctx := NewEvalContext()
	ctx.Observability = &ObservabilityOutputs{
		Endpoint:   "http://otel-collector:4318",
		Protocol:   "http/protobuf",
		Attributes: "deployment.environment=prod,service.namespace=my-app,team=payments",
	}

	tests := []struct {
		name    string
		input   string
		want    interface{}
		wantErr bool
	}{
		{
			name:  "observability endpoint",
			input: "${{ observability.endpoint }}",
			want:  "http://otel-collector:4318",
		},
		{
			name:  "observability protocol",
			input: "${{ observability.protocol }}",
			want:  "http/protobuf",
		},
		{
			name:  "observability attributes",
			input: "${{ observability.attributes }}",
			want:  "deployment.environment=prod,service.namespace=my-app,team=payments",
		},
		{
			name:  "observability concatenation",
			input: "Endpoint: ${{ observability.endpoint }}",
			want:  "Endpoint: http://otel-collector:4318",
		},
		{
			name:    "observability unknown property",
			input:   "${{ observability.unknown }}",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.Parse(tt.input)
			if err != nil {
				t.Fatalf("failed to parse: %v", err)
			}

			result, err := evaluator.Evaluate(expr, ctx)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.want {
				t.Errorf("got %v, want %v", result, tt.want)
			}
		})
	}
}

func TestEvaluator_ObservabilityNotConfigured(t *testing.T) {
	parser := NewParser()
	evaluator := NewEvaluator()

	ctx := NewEvalContext()
	// Observability is nil (not configured)

	expr, err := parser.Parse("${{ observability.endpoint }}")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	_, err = evaluator.Evaluate(expr, ctx)
	if err == nil {
		t.Error("expected error when observability is not configured")
	}
}
