package executor

import (
	"testing"
)

func TestStablePortForNode_Deterministic(t *testing.T) {
	// Same inputs should always produce the same port
	port1 := stablePortForNode("staging", "my-app", "api")
	port2 := stablePortForNode("staging", "my-app", "api")

	if port1 != port2 {
		t.Errorf("expected deterministic port, got %d and %d", port1, port2)
	}
}

func TestStablePortForNode_DifferentNames(t *testing.T) {
	// Different port names should produce different ports
	portApi := stablePortForNode("staging", "my-app", "api")
	portAdmin := stablePortForNode("staging", "my-app", "admin")

	if portApi == portAdmin {
		t.Errorf("expected different ports for different names, got %d for both", portApi)
	}
}

func TestStablePortForNode_DifferentEnvironments(t *testing.T) {
	// Different environments should produce different ports
	portStaging := stablePortForNode("staging", "my-app", "api")
	portProd := stablePortForNode("production", "my-app", "api")

	if portStaging == portProd {
		t.Errorf("expected different ports for different environments, got %d for both", portStaging)
	}
}

func TestStablePortForNode_Range(t *testing.T) {
	// Port should be in the range [10000, 60000)
	testCases := []struct {
		env       string
		component string
		port      string
	}{
		{"staging", "my-app", "api"},
		{"production", "my-app", "api"},
		{"dev", "another-app", "web"},
		{"test", "service", "grpc"},
	}

	for _, tc := range testCases {
		port := stablePortForNode(tc.env, tc.component, tc.port)
		if port < 10000 || port >= 60000 {
			t.Errorf("port %d out of range [10000, 60000) for %s/%s/%s", port, tc.env, tc.component, tc.port)
		}
	}
}
