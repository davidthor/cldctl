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

func TestEvaluator_DatabaseReadWriteEndpoints(t *testing.T) {
	parser := NewParser()
	evaluator := NewEvaluator()

	t.Run("explicit read/write endpoints", func(t *testing.T) {
		ctx := NewEvalContext()
		ctx.Databases["main"] = DatabaseOutputs{
			Host:     "primary.db.example.com",
			Port:     5432,
			Database: "mydb",
			Username: "admin",
			Password: "secret",
			URL:      "postgresql://admin:secret@primary.db.example.com:5432/mydb",
			Read: &DatabaseEndpoint{
				Host:     "replica.db.example.com",
				Port:     5433,
				Username: "readonly",
				Password: "readsecret",
				URL:      "postgresql://readonly:readsecret@replica.db.example.com:5433/mydb",
			},
			Write: &DatabaseEndpoint{
				Host:     "proxy.db.example.com",
				Port:     5434,
				Username: "writer",
				Password: "writesecret",
				URL:      "postgresql://writer:writesecret@proxy.db.example.com:5434/mydb",
			},
		}

		tests := []struct {
			name    string
			input   string
			want    interface{}
			wantErr bool
		}{
			// Top-level properties still work
			{
				name:  "top-level url",
				input: "${{ databases.main.url }}",
				want:  "postgresql://admin:secret@primary.db.example.com:5432/mydb",
			},
			{
				name:  "top-level host",
				input: "${{ databases.main.host }}",
				want:  "primary.db.example.com",
			},
			// Read endpoint
			{
				name:  "read url",
				input: "${{ databases.main.read.url }}",
				want:  "postgresql://readonly:readsecret@replica.db.example.com:5433/mydb",
			},
			{
				name:  "read host",
				input: "${{ databases.main.read.host }}",
				want:  "replica.db.example.com",
			},
			{
				name:  "read port",
				input: "${{ databases.main.read.port }}",
				want:  5433,
			},
			{
				name:  "read username",
				input: "${{ databases.main.read.username }}",
				want:  "readonly",
			},
			{
				name:  "read password",
				input: "${{ databases.main.read.password }}",
				want:  "readsecret",
			},
			// Write endpoint
			{
				name:  "write url",
				input: "${{ databases.main.write.url }}",
				want:  "postgresql://writer:writesecret@proxy.db.example.com:5434/mydb",
			},
			{
				name:  "write host",
				input: "${{ databases.main.write.host }}",
				want:  "proxy.db.example.com",
			},
			{
				name:  "write port",
				input: "${{ databases.main.write.port }}",
				want:  5434,
			},
			{
				name:  "write username",
				input: "${{ databases.main.write.username }}",
				want:  "writer",
			},
			{
				name:  "write password",
				input: "${{ databases.main.write.password }}",
				want:  "writesecret",
			},
			// Concatenation with read/write
			{
				name:  "concatenation with read url",
				input: "READ_URL=${{ databases.main.read.url }}",
				want:  "READ_URL=postgresql://readonly:readsecret@replica.db.example.com:5433/mydb",
			},
			// Errors
			{
				name:    "read without property",
				input:   "${{ databases.main.read }}",
				wantErr: true,
			},
			{
				name:    "write without property",
				input:   "${{ databases.main.write }}",
				wantErr: true,
			},
			{
				name:    "read unknown property",
				input:   "${{ databases.main.read.database }}",
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
	})

	t.Run("fallback to top-level when read/write is nil", func(t *testing.T) {
		ctx := NewEvalContext()
		ctx.Databases["main"] = DatabaseOutputs{
			Host:     "db.example.com",
			Port:     5432,
			Database: "mydb",
			Username: "admin",
			Password: "secret",
			URL:      "postgresql://admin:secret@db.example.com:5432/mydb",
			// Read and Write are nil - should fall back to top-level values
		}

		tests := []struct {
			name  string
			input string
			want  interface{}
		}{
			{
				name:  "read url falls back to top-level",
				input: "${{ databases.main.read.url }}",
				want:  "postgresql://admin:secret@db.example.com:5432/mydb",
			},
			{
				name:  "read host falls back to top-level",
				input: "${{ databases.main.read.host }}",
				want:  "db.example.com",
			},
			{
				name:  "read port falls back to top-level",
				input: "${{ databases.main.read.port }}",
				want:  5432,
			},
			{
				name:  "write url falls back to top-level",
				input: "${{ databases.main.write.url }}",
				want:  "postgresql://admin:secret@db.example.com:5432/mydb",
			},
			{
				name:  "write host falls back to top-level",
				input: "${{ databases.main.write.host }}",
				want:  "db.example.com",
			},
			{
				name:  "write port falls back to top-level",
				input: "${{ databases.main.write.port }}",
				want:  5432,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				expr, err := parser.Parse(tt.input)
				if err != nil {
					t.Fatalf("parse error: %v", err)
				}

				got, err := evaluator.Evaluate(expr, ctx)
				if err != nil {
					t.Fatalf("evaluate error: %v", err)
				}

				if got != tt.want {
					t.Errorf("expected %v (%T), got %v (%T)", tt.want, tt.want, got, got)
				}
			})
		}
	})

	t.Run("partial read/write (only read set)", func(t *testing.T) {
		ctx := NewEvalContext()
		ctx.Databases["main"] = DatabaseOutputs{
			Host:     "primary.db.example.com",
			Port:     5432,
			Username: "admin",
			Password: "secret",
			URL:      "postgresql://admin:secret@primary.db.example.com:5432/mydb",
			Read: &DatabaseEndpoint{
				Host:     "replica.db.example.com",
				Port:     5433,
				Username: "readonly",
				Password: "readsecret",
				URL:      "postgresql://readonly:readsecret@replica.db.example.com:5433/mydb",
			},
			// Write is nil - should fall back to top-level
		}

		readExpr, _ := parser.Parse("${{ databases.main.read.url }}")
		readGot, err := evaluator.Evaluate(readExpr, ctx)
		if err != nil {
			t.Fatalf("read evaluate error: %v", err)
		}
		if readGot != "postgresql://readonly:readsecret@replica.db.example.com:5433/mydb" {
			t.Errorf("read: expected replica URL, got %v", readGot)
		}

		writeExpr, _ := parser.Parse("${{ databases.main.write.url }}")
		writeGot, err := evaluator.Evaluate(writeExpr, ctx)
		if err != nil {
			t.Fatalf("write evaluate error: %v", err)
		}
		if writeGot != "postgresql://admin:secret@primary.db.example.com:5432/mydb" {
			t.Errorf("write: expected primary URL (fallback), got %v", writeGot)
		}
	})
}
