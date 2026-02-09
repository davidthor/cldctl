package executor

import (
	"strings"
	"testing"

	"github.com/davidthor/cldctl/pkg/graph"
	"github.com/davidthor/cldctl/pkg/schema/datacenter"
)

// --- enrichObservabilityOutputs tests ---

func TestEnrichObservabilityOutputs_MergesAllSources(t *testing.T) {
	g := graph.NewGraph("staging", "test-dc")

	obsNode := graph.NewNode(graph.NodeTypeObservability, "my-app", "observability")
	obsNode.SetInput("attributes", map[string]string{
		"team": "payments",
		"tier": "critical",
	})
	obsNode.Outputs = map[string]interface{}{
		"endpoint": "http://otel-collector:4318",
		"attributes": map[string]interface{}{
			"cloud.provider": "aws",
			"cloud.region":   "us-east-1",
		},
	}
	_ = g.AddNode(obsNode)

	executor := &Executor{graph: g}
	executor.enrichObservabilityOutputs(obsNode)

	attrs, ok := obsNode.Outputs["attributes"].(string)
	if !ok {
		t.Fatalf("expected attributes to be a string, got %T", obsNode.Outputs["attributes"])
	}

	assertContains(t, attrs, "service.namespace=my-app")
	assertContains(t, attrs, "deployment.environment=staging")
	assertContains(t, attrs, "cloud.provider=aws")
	assertContains(t, attrs, "cloud.region=us-east-1")
	assertContains(t, attrs, "team=payments")
	assertContains(t, attrs, "tier=critical")
}

func TestEnrichObservabilityOutputs_ComponentOverridesDC(t *testing.T) {
	g := graph.NewGraph("prod", "test-dc")

	obsNode := graph.NewNode(graph.NodeTypeObservability, "my-app", "observability")
	obsNode.SetInput("attributes", map[string]string{
		"team": "component-team",
	})
	obsNode.Outputs = map[string]interface{}{
		"endpoint": "http://otel-collector:4318",
		"attributes": map[string]string{
			"team":           "dc-team",
			"cloud.provider": "gcp",
		},
	}
	_ = g.AddNode(obsNode)

	executor := &Executor{graph: g}
	executor.enrichObservabilityOutputs(obsNode)

	attrs := obsNode.Outputs["attributes"].(string)
	assertContains(t, attrs, "team=component-team")
	assertContains(t, attrs, "cloud.provider=gcp")
	assertNotContains(t, attrs, "team=dc-team")
}

func TestEnrichObservabilityOutputs_NoDCAttributes(t *testing.T) {
	g := graph.NewGraph("dev", "test-dc")

	obsNode := graph.NewNode(graph.NodeTypeObservability, "my-app", "observability")
	obsNode.SetInput("attributes", map[string]string{"team": "backend"})
	obsNode.Outputs = map[string]interface{}{
		"endpoint": "http://otel-collector:4318",
		"protocol": "http/protobuf",
	}
	_ = g.AddNode(obsNode)

	executor := &Executor{graph: g}
	executor.enrichObservabilityOutputs(obsNode)

	attrs := obsNode.Outputs["attributes"].(string)
	assertContains(t, attrs, "service.namespace=my-app")
	assertContains(t, attrs, "deployment.environment=dev")
	assertContains(t, attrs, "team=backend")
}

func TestEnrichObservabilityOutputs_NoComponentAttributes(t *testing.T) {
	g := graph.NewGraph("prod", "test-dc")

	obsNode := graph.NewNode(graph.NodeTypeObservability, "my-app", "observability")
	obsNode.Outputs = map[string]interface{}{
		"endpoint": "http://otel-collector:4318",
		"attributes": map[string]string{
			"cloud.region": "eu-west-1",
		},
	}
	_ = g.AddNode(obsNode)

	executor := &Executor{graph: g}
	executor.enrichObservabilityOutputs(obsNode)

	attrs := obsNode.Outputs["attributes"].(string)
	assertContains(t, attrs, "cloud.region=eu-west-1")
	assertContains(t, attrs, "service.namespace=my-app")
}

func TestEnrichObservabilityOutputs_DCStringAttributes(t *testing.T) {
	g := graph.NewGraph("prod", "test-dc")

	obsNode := graph.NewNode(graph.NodeTypeObservability, "my-app", "observability")
	obsNode.Outputs = map[string]interface{}{
		"endpoint":   "http://otel-collector:4318",
		"attributes": "cloud.provider=aws,cloud.region=us-east-1",
	}
	_ = g.AddNode(obsNode)

	executor := &Executor{graph: g}
	executor.enrichObservabilityOutputs(obsNode)

	attrs := obsNode.Outputs["attributes"].(string)
	assertContains(t, attrs, "cloud.provider=aws")
	assertContains(t, attrs, "cloud.region=us-east-1")
}

func TestEnrichObservabilityOutputs_SortedDeterministic(t *testing.T) {
	g := graph.NewGraph("prod", "test-dc")

	obsNode := graph.NewNode(graph.NodeTypeObservability, "my-app", "observability")
	obsNode.SetInput("attributes", map[string]string{"team": "payments"})
	obsNode.Outputs = map[string]interface{}{
		"endpoint":   "http://otel-collector:4318",
		"attributes": map[string]string{"cloud.region": "us-east-1"},
	}
	_ = g.AddNode(obsNode)

	executor := &Executor{graph: g}
	executor.enrichObservabilityOutputs(obsNode)

	attrs := obsNode.Outputs["attributes"].(string)
	parts := strings.Split(attrs, ",")
	for i := 1; i < len(parts); i++ {
		if parts[i] < parts[i-1] {
			t.Errorf("attributes not sorted: %s comes after %s", parts[i], parts[i-1])
		}
	}
}

// --- resolveComponentExpressions tests ---

func TestResolveComponentExpressions_ObservabilityEndpoint(t *testing.T) {
	g := graph.NewGraph("test-env", "test-dc")

	obsNode := graph.NewNode(graph.NodeTypeObservability, "my-app", "observability")
	obsNode.SetOutput("endpoint", "http://otel-collector:4318")
	obsNode.SetOutput("protocol", "http/protobuf")
	obsNode.SetOutput("attributes", "service.namespace=my-app,deployment.environment=test-env,team=payments")
	obsNode.State = graph.NodeStateCompleted
	_ = g.AddNode(obsNode)

	deployNode := graph.NewNode(graph.NodeTypeDeployment, "my-app", "api")
	deployNode.SetInput("environment", map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": "${{ observability.endpoint }}",
		"OTEL_EXPORTER_OTLP_PROTOCOL": "${{ observability.protocol }}",
		"OTEL_RESOURCE_ATTRIBUTES":    "${{ observability.attributes }}",
		"DATABASE_URL":                "postgresql://localhost/mydb",
	})
	_ = g.AddNode(deployNode)

	executor := &Executor{graph: g}
	executor.resolveComponentExpressions(deployNode, nil)

	env, ok := deployNode.Inputs["environment"].(map[string]string)
	if !ok {
		t.Fatalf("expected environment to be map[string]string")
	}

	assertEnvVar(t, env, "OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector:4318")
	assertEnvVar(t, env, "OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	assertEnvVar(t, env, "OTEL_RESOURCE_ATTRIBUTES", "service.namespace=my-app,deployment.environment=test-env,team=payments")
	assertEnvVar(t, env, "DATABASE_URL", "postgresql://localhost/mydb")
}

func TestResolveComponentExpressions_ObservabilityNotConfigured(t *testing.T) {
	g := graph.NewGraph("test-env", "test-dc")

	deployNode := graph.NewNode(graph.NodeTypeDeployment, "my-app", "api")
	deployNode.SetInput("environment", map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": "${{ observability.endpoint }}",
	})
	_ = g.AddNode(deployNode)

	executor := &Executor{graph: g}
	executor.resolveComponentExpressions(deployNode, nil)

	env := deployNode.Inputs["environment"].(map[string]string)
	assertEnvVar(t, env, "OTEL_EXPORTER_OTLP_ENDPOINT", "")
}

func TestResolveComponentExpressions_StringConcatenation(t *testing.T) {
	g := graph.NewGraph("test-env", "test-dc")

	obsNode := graph.NewNode(graph.NodeTypeObservability, "my-app", "observability")
	obsNode.SetOutput("endpoint", "http://otel-collector:4318")
	obsNode.State = graph.NodeStateCompleted
	_ = g.AddNode(obsNode)

	deployNode := graph.NewNode(graph.NodeTypeDeployment, "my-app", "api")
	deployNode.SetInput("some_url", "endpoint=${{ observability.endpoint }}/v1/traces")
	_ = g.AddNode(deployNode)

	executor := &Executor{graph: g}
	executor.resolveComponentExpressions(deployNode, nil)

	resolved := deployNode.Inputs["some_url"].(string)
	if resolved != "endpoint=http://otel-collector:4318/v1/traces" {
		t.Errorf("expected concatenated string, got %s", resolved)
	}
}

func TestResolveComponentExpressions_MapStringInterface(t *testing.T) {
	g := graph.NewGraph("test-env", "test-dc")

	obsNode := graph.NewNode(graph.NodeTypeObservability, "my-app", "observability")
	obsNode.SetOutput("endpoint", "http://otel-collector:4318")
	obsNode.State = graph.NodeStateCompleted
	_ = g.AddNode(obsNode)

	deployNode := graph.NewNode(graph.NodeTypeDeployment, "my-app", "api")
	deployNode.SetInput("environment", map[string]interface{}{
		"OTEL_ENDPOINT": "${{ observability.endpoint }}",
		"PORT":          8080,
	})
	_ = g.AddNode(deployNode)

	executor := &Executor{graph: g}
	executor.resolveComponentExpressions(deployNode, nil)

	env := deployNode.Inputs["environment"].(map[string]interface{})
	if env["OTEL_ENDPOINT"] != "http://otel-collector:4318" {
		t.Errorf("expected endpoint resolved, got %v", env["OTEL_ENDPOINT"])
	}
	if env["PORT"] != 8080 {
		t.Errorf("expected PORT preserved as int, got %v", env["PORT"])
	}
}

// --- injectOTelEnvironmentIfEnabled tests ---

func TestInjectOTelEnvironmentIfEnabled_InjectTrue(t *testing.T) {
	g := graph.NewGraph("test-env", "test-dc")

	obsNode := graph.NewNode(graph.NodeTypeObservability, "my-app", "observability")
	obsNode.SetInput("inject", true)
	obsNode.SetInput("attributes", map[string]string{"team": "backend"})
	obsNode.Outputs = map[string]interface{}{
		"endpoint": "http://otel-collector:4318",
		"protocol": "http/protobuf",
	}
	obsNode.State = graph.NodeStateCompleted

	executor := &Executor{graph: g}
	_ = g.AddNode(obsNode)
	executor.enrichObservabilityOutputs(obsNode)

	deployNode := graph.NewNode(graph.NodeTypeDeployment, "my-app", "api")
	_ = g.AddNode(deployNode)

	env := map[string]string{
		"DATABASE_URL": "postgresql://localhost/mydb",
	}
	executor.injectOTelEnvironmentIfEnabled(env, deployNode)

	assertEnvVar(t, env, "OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector:4318")
	assertEnvVar(t, env, "OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	assertEnvVar(t, env, "OTEL_SERVICE_NAME", "my-app-api")
	assertEnvVar(t, env, "OTEL_LOGS_EXPORTER", "otlp")
	assertEnvVar(t, env, "OTEL_TRACES_EXPORTER", "otlp")
	assertEnvVar(t, env, "OTEL_METRICS_EXPORTER", "otlp")

	attrs := env["OTEL_RESOURCE_ATTRIBUTES"]
	assertContains(t, attrs, "service.namespace=my-app")
	assertContains(t, attrs, "deployment.environment=test-env")
	assertContains(t, attrs, "team=backend")
	assertContains(t, attrs, "service.type=deployment")

	assertEnvVar(t, env, "DATABASE_URL", "postgresql://localhost/mydb")
}

func TestInjectOTelEnvironmentIfEnabled_InjectFalse(t *testing.T) {
	g := graph.NewGraph("test-env", "test-dc")

	obsNode := graph.NewNode(graph.NodeTypeObservability, "my-app", "observability")
	obsNode.SetInput("inject", false)
	obsNode.SetOutput("endpoint", "http://otel-collector:4318")
	obsNode.SetOutput("protocol", "http/protobuf")
	obsNode.State = graph.NodeStateCompleted
	_ = g.AddNode(obsNode)

	deployNode := graph.NewNode(graph.NodeTypeDeployment, "my-app", "api")
	_ = g.AddNode(deployNode)

	executor := &Executor{graph: g}

	env := map[string]string{"DATABASE_URL": "postgresql://localhost/mydb"}
	executor.injectOTelEnvironmentIfEnabled(env, deployNode)

	otelKeys := []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_SERVICE_NAME",
		"OTEL_LOGS_EXPORTER",
		"OTEL_TRACES_EXPORTER",
		"OTEL_METRICS_EXPORTER",
		"OTEL_RESOURCE_ATTRIBUTES",
	}
	for _, key := range otelKeys {
		if _, exists := env[key]; exists {
			t.Errorf("%s should NOT be set when inject=false", key)
		}
	}
}

func TestInjectOTelEnvironmentIfEnabled_NoOverwrite(t *testing.T) {
	g := graph.NewGraph("test-env", "test-dc")

	obsNode := graph.NewNode(graph.NodeTypeObservability, "my-app", "observability")
	obsNode.SetInput("inject", true)
	obsNode.Outputs = map[string]interface{}{
		"endpoint": "http://otel-collector:4318",
		"protocol": "http/protobuf",
	}
	obsNode.State = graph.NodeStateCompleted
	_ = g.AddNode(obsNode)

	executor := &Executor{graph: g}
	executor.enrichObservabilityOutputs(obsNode)

	deployNode := graph.NewNode(graph.NodeTypeDeployment, "my-app", "api")
	_ = g.AddNode(deployNode)

	// Component author explicitly overrides some vars
	env := map[string]string{
		"OTEL_SERVICE_NAME":           "custom-name",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://custom:4318",
		"OTEL_METRICS_EXPORTER":       "none", // opt out of metrics
	}
	executor.injectOTelEnvironmentIfEnabled(env, deployNode)

	assertEnvVar(t, env, "OTEL_SERVICE_NAME", "custom-name")
	assertEnvVar(t, env, "OTEL_EXPORTER_OTLP_ENDPOINT", "http://custom:4318")
	assertEnvVar(t, env, "OTEL_METRICS_EXPORTER", "none") // preserved
	assertEnvVar(t, env, "OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	assertEnvVar(t, env, "OTEL_LOGS_EXPORTER", "otlp")
	assertEnvVar(t, env, "OTEL_TRACES_EXPORTER", "otlp")
}

func TestInjectOTelEnvironmentIfEnabled_NoObservabilityNode(t *testing.T) {
	g := graph.NewGraph("test-env", "test-dc")

	deployNode := graph.NewNode(graph.NodeTypeDeployment, "my-app", "api")
	_ = g.AddNode(deployNode)

	executor := &Executor{graph: g}

	env := map[string]string{}
	executor.injectOTelEnvironmentIfEnabled(env, deployNode)

	if len(env) != 0 {
		t.Errorf("expected empty env when no observability node, got %d vars", len(env))
	}
}

func TestInjectOTelEnvironmentIfEnabled_WithDCAttributes(t *testing.T) {
	g := graph.NewGraph("prod", "test-dc")

	obsNode := graph.NewNode(graph.NodeTypeObservability, "my-app", "observability")
	obsNode.SetInput("inject", true)
	obsNode.SetInput("attributes", map[string]string{"team": "payments"})
	obsNode.Outputs = map[string]interface{}{
		"endpoint":   "http://otel-collector:4318",
		"attributes": map[string]string{"cloud.provider": "aws"},
	}
	obsNode.State = graph.NodeStateCompleted
	_ = g.AddNode(obsNode)

	executor := &Executor{graph: g}
	executor.enrichObservabilityOutputs(obsNode)

	deployNode := graph.NewNode(graph.NodeTypeDeployment, "my-app", "api")
	_ = g.AddNode(deployNode)

	env := map[string]string{}
	executor.injectOTelEnvironmentIfEnabled(env, deployNode)

	attrs := env["OTEL_RESOURCE_ATTRIBUTES"]
	assertContains(t, attrs, "team=payments")
	assertContains(t, attrs, "cloud.provider=aws")
	assertContains(t, attrs, "deployment.environment=prod")
	assertContains(t, attrs, "service.namespace=my-app")
	assertContains(t, attrs, "service.type=deployment")
}

// --- Utility function tests ---

func TestSortStrings(t *testing.T) {
	s := []string{"c", "a", "b"}
	sortStrings(s)
	if s[0] != "a" || s[1] != "b" || s[2] != "c" {
		t.Errorf("expected [a b c], got %v", s)
	}
}

// --- Datacenter hook tests ---

func TestGetHooksForType_Observability(t *testing.T) {
	dc := loadTestDatacenter(t)
	if dc == nil {
		t.Skip("skipping: test datacenter not available")
	}

	executor := &Executor{
		options: Options{
			Datacenter: dc,
		},
	}

	hooks := executor.getHooksForType(graph.NodeTypeObservability)
	if len(hooks) == 0 {
		t.Error("expected at least 1 observability hook from test datacenter")
	}
}

func TestGetHooksForType_ObservabilityNilDatacenter(t *testing.T) {
	executor := &Executor{
		options: Options{
			Datacenter: nil,
		},
	}

	hooks := executor.getHooksForType(graph.NodeTypeObservability)
	if hooks != nil {
		t.Error("expected nil hooks when datacenter is nil")
	}
}

// --- Test helpers ---

func assertEnvVar(t *testing.T, env map[string]string, key, expected string) {
	t.Helper()
	if val, ok := env[key]; !ok {
		t.Errorf("expected %s to be set", key)
	} else if val != expected {
		t.Errorf("expected %s=%s, got %s", key, expected, val)
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected %q to contain %q", haystack, needle)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("expected %q NOT to contain %q", haystack, needle)
	}
}

func loadTestDatacenter(t *testing.T) datacenter.Datacenter {
	t.Helper()
	loader := datacenter.NewLoader()
	dc, err := loader.Load("../../../official-templates/local/datacenter.dc")
	if err != nil {
		return nil
	}
	return dc
}
