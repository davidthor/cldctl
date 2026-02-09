package expression

import (
	"testing"
)

func TestEvaluator_ResolvePorts(t *testing.T) {
	parser := NewParser()
	evaluator := NewEvaluator()

	ctx := NewEvalContext()
	ctx.Ports["api"] = PortOutputs{Port: 12345}

	tests := []struct {
		name    string
		input   string
		want    interface{}
		wantErr bool
	}{
		{
			name:  "port reference",
			input: "${{ ports.api.port }}",
			want:  12345,
		},
		{
			name:    "unknown port",
			input:   "${{ ports.unknown.port }}",
			wantErr: true,
		},
		{
			name:    "unknown port property",
			input:   "${{ ports.api.host }}",
			wantErr: true,
		},
		{
			name:  "port in template",
			input: "http://localhost:${{ ports.api.port }}/health",
			want:  "http://localhost:12345/health",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := parser.Parse(tt.input)
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}

			result, err := evaluator.Evaluate(parsed, ctx)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("evaluate failed: %v", err)
			}

			if result != tt.want {
				t.Errorf("got %v (%T), want %v (%T)", result, result, tt.want, tt.want)
			}
		})
	}
}
