package types

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstanceState_JSONRoundTrip(t *testing.T) {
	original := &ComponentState{
		Name:   "my-app",
		Source: "my-app:v1",
		Status: ResourceStatusReady,
		Resources: map[string]*ResourceState{
			"database.main": {
				Name:   "main",
				Type:   "database",
				Status: ResourceStatusReady,
			},
		},
		Instances: map[string]*InstanceState{
			"canary": {
				Name:       "canary",
				Source:      "my-app:v2",
				Weight:     10,
				DeployedAt: time.Now().Truncate(time.Second),
				Resources: map[string]*ResourceState{
					"deployment.api": {
						Name:   "api",
						Type:   "deployment",
						Status: ResourceStatusReady,
					},
				},
			},
			"stable": {
				Name:       "stable",
				Source:      "my-app:v1",
				Weight:     90,
				DeployedAt: time.Now().Add(-24 * time.Hour).Truncate(time.Second),
				Resources: map[string]*ResourceState{
					"deployment.api": {
						Name:   "api",
						Type:   "deployment",
						Status: ResourceStatusReady,
					},
				},
			},
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded ComponentState
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.Name, decoded.Name)
	assert.Equal(t, original.Source, decoded.Source)
	assert.Len(t, decoded.Resources, 1)
	assert.Len(t, decoded.Instances, 2)

	canary := decoded.Instances["canary"]
	assert.Equal(t, "canary", canary.Name)
	assert.Equal(t, "my-app:v2", canary.Source)
	assert.Equal(t, 10, canary.Weight)
	assert.Len(t, canary.Resources, 1)

	stable := decoded.Instances["stable"]
	assert.Equal(t, "stable", stable.Name)
	assert.Equal(t, 90, stable.Weight)
}

func TestComponentState_BackwardCompatible(t *testing.T) {
	// Old-format JSON without instances should still work
	jsonData := `{
		"name": "my-app",
		"version": "v1",
		"source": "my-app:v1",
		"deployed_at": "2026-01-01T00:00:00Z",
		"updated_at": "2026-01-01T00:00:00Z",
		"status": "ready",
		"resources": {
			"database.main": {
				"name": "main",
				"type": "database",
				"component": "my-app",
				"status": "ready"
			}
		}
	}`

	var state ComponentState
	err := json.Unmarshal([]byte(jsonData), &state)
	require.NoError(t, err)

	assert.Equal(t, "my-app", state.Name)
	assert.Nil(t, state.Instances)
	assert.Len(t, state.Resources, 1)
}

func TestInstanceState_NilInstancesInJSON(t *testing.T) {
	state := &ComponentState{
		Name:      "my-app",
		Source:    "my-app:v1",
		Resources: map[string]*ResourceState{},
	}

	data, err := json.Marshal(state)
	require.NoError(t, err)

	// Instances should be omitted from JSON when nil
	assert.NotContains(t, string(data), "instances")
}
