package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/davidthor/cldctl/pkg/state/types"
)

func TestFindObservabilityQueryConfig_Found(t *testing.T) {
	envState := &types.EnvironmentState{
		Name: "test-env",
		Components: map[string]*types.ComponentState{
			"my-app": {
				Resources: map[string]*types.ResourceState{
					"observability.observability": {
						Type:   "observability",
						Status: types.ResourceStatusReady,
						Outputs: map[string]interface{}{
							"query_type":     "loki",
							"query_endpoint": "http://localhost:3100",
							"dashboard_url":  "http://localhost:3000",
						},
					},
				},
			},
		},
	}

	queryType, queryEndpoint, err := findObservabilityQueryConfig(envState)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if queryType != "loki" {
		t.Errorf("expected query_type=loki, got %q", queryType)
	}
	if queryEndpoint != "http://localhost:3100" {
		t.Errorf("expected query_endpoint=http://localhost:3100, got %q", queryEndpoint)
	}
}

func TestFindObservabilityQueryConfig_NotFound(t *testing.T) {
	envState := &types.EnvironmentState{
		Name: "test-env",
		Components: map[string]*types.ComponentState{
			"my-app": {
				Resources: map[string]*types.ResourceState{
					"database.main": {
						Type:   "database",
						Status: types.ResourceStatusReady,
					},
				},
			},
		},
	}

	_, _, err := findObservabilityQueryConfig(envState)
	if err == nil {
		t.Fatal("expected error when no observability resource found")
	}
}

func TestFindObservabilityQueryConfig_MissingOutputs(t *testing.T) {
	envState := &types.EnvironmentState{
		Name: "test-env",
		Components: map[string]*types.ComponentState{
			"my-app": {
				Resources: map[string]*types.ResourceState{
					"observability.observability": {
						Type:    "observability",
						Status:  types.ResourceStatusReady,
						Outputs: map[string]interface{}{
							// Missing query_type and query_endpoint
						},
					},
				},
			},
		},
	}

	_, _, err := findObservabilityQueryConfig(envState)
	if err == nil {
		t.Fatal("expected error when outputs are missing")
	}
}

func TestFindObservabilityQueryConfig_NotReady(t *testing.T) {
	envState := &types.EnvironmentState{
		Name: "test-env",
		Components: map[string]*types.ComponentState{
			"my-app": {
				Resources: map[string]*types.ResourceState{
					"observability.observability": {
						Type:   "observability",
						Status: types.ResourceStatusProvisioning,
						Outputs: map[string]interface{}{
							"query_type":     "loki",
							"query_endpoint": "http://localhost:3100",
						},
					},
				},
			},
		},
	}

	_, _, err := findObservabilityQueryConfig(envState)
	if err == nil {
		t.Fatal("expected error when observability resource is not ready")
	}
}

func TestFindObservabilityConfig_Found(t *testing.T) {
	envState := &types.EnvironmentState{
		Name: "test-env",
		Components: map[string]*types.ComponentState{
			"my-app": {
				Resources: map[string]*types.ResourceState{
					"observability.observability": {
						Type:   "observability",
						Status: types.ResourceStatusReady,
						Outputs: map[string]interface{}{
							"dashboard_url": "http://localhost:3000",
						},
					},
				},
			},
		},
	}

	cfg, err := findObservabilityConfig(envState)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DashboardURL != "http://localhost:3000" {
		t.Errorf("expected http://localhost:3000, got %q", cfg.DashboardURL)
	}
}

func TestFindObservabilityConfig_NotFound(t *testing.T) {
	envState := &types.EnvironmentState{
		Name:       "test-env",
		Components: map[string]*types.ComponentState{},
	}

	_, err := findObservabilityConfig(envState)
	if err == nil {
		t.Fatal("expected error when no observability resource found")
	}
}

func TestFindObservabilityConfig_MissingURL(t *testing.T) {
	envState := &types.EnvironmentState{
		Name: "test-env",
		Components: map[string]*types.ComponentState{
			"my-app": {
				Resources: map[string]*types.ResourceState{
					"observability.observability": {
						Type:    "observability",
						Status:  types.ResourceStatusReady,
						Outputs: map[string]interface{}{},
					},
				},
			},
		},
	}

	_, err := findObservabilityConfig(envState)
	if err == nil {
		t.Fatal("expected error when dashboard_url is missing")
	}
}

func TestBuildDashboardURL_Loki(t *testing.T) {
	cfg := &observabilityConfig{
		DashboardURL:  "http://localhost:64829",
		QueryType:     "loki",
		QueryEndpoint: "http://localhost:64830",
	}

	result := buildDashboardURL(cfg, "twenty-dev")

	// Should produce a Grafana Explore URL with Loki query
	if !strings.Contains(result, "http://localhost:64829/explore") {
		t.Errorf("expected Grafana Explore URL, got %q", result)
	}
	if !strings.Contains(result, "schemaVersion=1") {
		t.Errorf("expected schemaVersion parameter, got %q", result)
	}
	if !strings.Contains(result, "deployment_environment") {
		t.Errorf("expected deployment_environment in query, got %q", result)
	}
	if !strings.Contains(result, "twenty-dev") {
		t.Errorf("expected environment name in query, got %q", result)
	}
}

func TestBuildDashboardURL_UnknownQueryType(t *testing.T) {
	cfg := &observabilityConfig{
		DashboardURL: "https://cloudwatch.aws.amazon.com/dashboard/my-env",
		QueryType:    "cloudwatch",
	}

	result := buildDashboardURL(cfg, "production")

	// Should fall back to the base dashboard URL
	if result != "https://cloudwatch.aws.amazon.com/dashboard/my-env" {
		t.Errorf("expected base dashboard URL, got %q", result)
	}
}

func TestBuildDashboardURL_NoQueryType(t *testing.T) {
	cfg := &observabilityConfig{
		DashboardURL: "http://localhost:3000",
	}

	result := buildDashboardURL(cfg, "staging")

	// Should fall back to the base dashboard URL
	if result != "http://localhost:3000" {
		t.Errorf("expected base dashboard URL, got %q", result)
	}
}

func TestBuildGrafanaExploreURL_TrailingSlash(t *testing.T) {
	result := buildGrafanaExploreURL("http://localhost:3000/", "my-env")

	// Should not have double slashes
	if strings.Contains(result, "//explore") {
		t.Errorf("expected no double slash before /explore, got %q", result)
	}
	if !strings.Contains(result, "http://localhost:3000/explore") {
		t.Errorf("expected clean URL, got %q", result)
	}
}

func TestParseSince_Duration(t *testing.T) {
	before := time.Now()
	result, err := parseSince("5m")
	after := time.Now()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Result should be ~5 minutes ago
	expected := before.Add(-5 * time.Minute)
	if result.Before(expected.Add(-time.Second)) || result.After(after.Add(-5*time.Minute).Add(time.Second)) {
		t.Errorf("parseSince(5m) = %v, expected ~%v", result, expected)
	}
}

func TestParseSince_RFC3339(t *testing.T) {
	result, err := parseSince("2025-01-15T10:30:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	if !result.Equal(expected) {
		t.Errorf("parseSince(RFC3339) = %v, expected %v", result, expected)
	}
}

func TestParseSince_Invalid(t *testing.T) {
	_, err := parseSince("not-a-time")
	if err == nil {
		t.Fatal("expected error for invalid since value")
	}
}
