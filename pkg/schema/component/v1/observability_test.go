package v1

import (
	"testing"
)

func TestParser_ParseBytes_ObservabilityBoolTrue(t *testing.T) {
	parser := &Parser{}

	yaml := `
deployments:
  api:
    image: nginx:latest

observability: true
`

	schema, err := parser.ParseBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if schema.Observability == nil {
		t.Fatal("expected observability to be set")
	}
	if !schema.Observability.Enabled {
		t.Error("expected observability to be enabled")
	}
}

func TestParser_ParseBytes_ObservabilityBoolFalse(t *testing.T) {
	parser := &Parser{}

	yaml := `
deployments:
  api:
    image: nginx:latest

observability: false
`

	schema, err := parser.ParseBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if schema.Observability == nil {
		t.Fatal("expected observability to be set")
	}
	if schema.Observability.Enabled {
		t.Error("expected observability to be disabled")
	}
}

func TestParser_ParseBytes_ObservabilityFullObject(t *testing.T) {
	parser := &Parser{}

	yaml := `
deployments:
  api:
    image: nginx:latest

observability:
  inject: true
  attributes:
    team: payments
    tier: critical
`

	schema, err := parser.ParseBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if schema.Observability == nil {
		t.Fatal("expected observability to be set")
	}
	if !schema.Observability.Enabled {
		t.Error("expected observability to be enabled")
	}
	if schema.Observability.Inject == nil || !*schema.Observability.Inject {
		t.Error("expected inject to be true")
	}
	if len(schema.Observability.Attributes) != 2 {
		t.Errorf("expected 2 attributes, got %d", len(schema.Observability.Attributes))
	}
	if schema.Observability.Attributes["team"] != "payments" {
		t.Errorf("expected team=payments, got %s", schema.Observability.Attributes["team"])
	}
}

func TestParser_ParseBytes_ObservabilityOmitted(t *testing.T) {
	parser := &Parser{}

	yaml := `
deployments:
  api:
    image: nginx:latest
`

	schema, err := parser.ParseBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if schema.Observability != nil {
		t.Error("expected observability to be nil when omitted")
	}
}

func TestTransformer_ObservabilityDefaults(t *testing.T) {
	transformer := NewTransformer()

	schema := &SchemaV1{
		Observability: &ObservabilityV1{
			Enabled: true,
		},
	}

	ic, err := transformer.Transform(schema)
	if err != nil {
		t.Fatalf("unexpected transform error: %v", err)
	}

	if ic.Observability == nil {
		t.Fatal("expected observability to be set")
	}
	if ic.Observability.Inject {
		t.Error("expected inject to default to false")
	}
}

func TestTransformer_ObservabilityDisabled(t *testing.T) {
	transformer := NewTransformer()

	schema := &SchemaV1{
		Observability: &ObservabilityV1{
			Enabled: false,
		},
	}

	ic, err := transformer.Transform(schema)
	if err != nil {
		t.Fatalf("unexpected transform error: %v", err)
	}

	if ic.Observability != nil {
		t.Error("expected observability to be nil when disabled")
	}
}

func TestTransformer_ObservabilityWithAttributes(t *testing.T) {
	transformer := NewTransformer()

	trueVal := true
	schema := &SchemaV1{
		Observability: &ObservabilityV1{
			Enabled: true,
			Inject:  &trueVal,
			Attributes: map[string]string{
				"team": "platform",
			},
		},
	}

	ic, err := transformer.Transform(schema)
	if err != nil {
		t.Fatalf("unexpected transform error: %v", err)
	}

	if ic.Observability == nil {
		t.Fatal("expected observability to be set")
	}
	if !ic.Observability.Inject {
		t.Error("expected inject to be true")
	}
	if ic.Observability.Attributes["team"] != "platform" {
		t.Errorf("expected team=platform, got %s", ic.Observability.Attributes["team"])
	}
}

func TestTransformer_ObservabilityInjectDefaultsFalse(t *testing.T) {
	transformer := NewTransformer()

	schema := &SchemaV1{
		Observability: &ObservabilityV1{
			Enabled: true,
			// inject not set -- should default to false
		},
	}

	ic, err := transformer.Transform(schema)
	if err != nil {
		t.Fatalf("unexpected transform error: %v", err)
	}

	if ic.Observability == nil {
		t.Fatal("expected observability to be set")
	}
	if ic.Observability.Inject {
		t.Error("expected inject to default to false")
	}
}

func TestValidator_ObservabilityValid(t *testing.T) {
	validator := NewValidator()

	schema := &SchemaV1{
		Observability: &ObservabilityV1{
			Enabled: true,
		},
	}

	errs := validator.Validate(schema)
	if len(errs) != 0 {
		t.Errorf("expected no validation errors, got %d: %v", len(errs), errs)
	}
}
