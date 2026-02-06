package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockRunnerSuccess(dir string, name string, args ...string) ([]byte, error) {
	return []byte("ok"), nil
}

func mockRunnerFailure(dir string, name string, args ...string) ([]byte, error) {
	return []byte("error: invalid config key"), fmt.Errorf("exit status 1")
}

func TestSetPulumiConfig_Success(t *testing.T) {
	inputs := map[string]interface{}{
		"region":   "us-east-1",
		"replicas": 3,
	}

	err := setPulumiConfig("/app", inputs, mockRunnerSuccess)
	assert.NoError(t, err)
}

func TestSetPulumiConfig_Failure(t *testing.T) {
	inputs := map[string]interface{}{
		"badkey": "value",
	}

	err := setPulumiConfig("/app", inputs, mockRunnerFailure)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to set pulumi config")
	assert.Contains(t, err.Error(), "badkey")
}

func TestSetPulumiConfig_EmptyInputs(t *testing.T) {
	callCount := 0
	countingRunner := func(dir string, name string, args ...string) ([]byte, error) {
		callCount++
		return nil, nil
	}

	err := setPulumiConfig("/app", map[string]interface{}{}, countingRunner)
	assert.NoError(t, err)
	assert.Equal(t, 0, callCount, "should not run any commands for empty inputs")
}
