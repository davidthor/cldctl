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
  logs: true
  traces: false
  metrics: true
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
	if schema.Observability.Logs == nil || !*schema.Observability.Logs {
		t.Error("expected logs to be true")
	}
	if schema.Observability.Traces == nil || *schema.Observability.Traces {
		t.Error("expected traces to be false")
	}
	if schema.Observability.Metrics == nil || !*schema.Observability.Metrics {
		t.Error("expected metrics to be true")
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

	trueVal := true
	schema := &SchemaV1{
		Observability: &ObservabilityV1{
			Enabled: true,
			Logs:    &trueVal,
		},
	}

	ic, err := transformer.Transform(schema)
	if err != nil {
		t.Fatalf("unexpected transform error: %v", err)
	}

	if ic.Observability == nil {
		t.Fatal("expected observability to be set")
	}
	if !ic.Observability.Logs {
		t.Error("expected logs to be true")
	}
	if !ic.Observability.Traces {
		t.Error("expected traces to default to true")
	}
	if !ic.Observability.Metrics {
		t.Error("expected metrics to default to true")
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

func TestTransformer_ObservabilityCustomSignals(t *testing.T) {
	transformer := NewTransformer()

	trueVal := true
	falseVal := false
	schema := &SchemaV1{
		Observability: &ObservabilityV1{
			Enabled: true,
			Logs:    &trueVal,
			Traces:  &falseVal,
			Metrics: &falseVal,
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
	if !ic.Observability.Logs {
		t.Error("expected logs to be true")
	}
	if ic.Observability.Traces {
		t.Error("expected traces to be false")
	}
	if ic.Observability.Metrics {
		t.Error("expected metrics to be false")
	}
	if ic.Observability.Attributes["team"] != "platform" {
		t.Errorf("expected team=platform, got %s", ic.Observability.Attributes["team"])
	}
}

func TestParser_ParseBytes_ObservabilityWithInject(t *testing.T) {
	parser := &Parser{}

	yaml := `
deployments:
  api:
    image: nginx:latest

observability:
  inject: true
  logs: true
  traces: true
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
}

func TestTransformer_ObservabilityInjectTrue(t *testing.T) {
	transformer := NewTransformer()

	trueVal := true
	schema := &SchemaV1{
		Observability: &ObservabilityV1{
			Enabled: true,
			Inject:  &trueVal,
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
