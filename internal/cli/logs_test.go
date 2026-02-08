package cli

import (
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

func TestFindDashboardURL_Found(t *testing.T) {
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

	url, err := findDashboardURL(envState)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "http://localhost:3000" {
		t.Errorf("expected http://localhost:3000, got %q", url)
	}
}

func TestFindDashboardURL_NotFound(t *testing.T) {
	envState := &types.EnvironmentState{
		Name: "test-env",
		Components: map[string]*types.ComponentState{},
	}

	_, err := findDashboardURL(envState)
	if err == nil {
		t.Fatal("expected error when no observability resource found")
	}
}

func TestFindDashboardURL_MissingURL(t *testing.T) {
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

	_, err := findDashboardURL(envState)
	if err == nil {
		t.Fatal("expected error when dashboard_url is missing")
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
