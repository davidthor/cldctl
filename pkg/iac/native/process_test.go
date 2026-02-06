package native

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDockerfileCmd_JSONFormat(t *testing.T) {
	// Create a temp Dockerfile
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")

	dockerfile := `FROM node:18
WORKDIR /app
COPY package.json .
RUN npm install
CMD ["npm", "start"]
`
	err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644)
	require.NoError(t, err)

	cmd, err := ParseDockerfileCmd(dockerfilePath)
	require.NoError(t, err)
	assert.Equal(t, []string{"npm", "start"}, cmd)
}

func TestParseDockerfileCmd_ShellFormat(t *testing.T) {
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")

	dockerfile := `FROM node:18
WORKDIR /app
CMD npm run dev
`
	err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644)
	require.NoError(t, err)

	cmd, err := ParseDockerfileCmd(dockerfilePath)
	require.NoError(t, err)
	assert.Equal(t, []string{"/bin/sh", "-c", "npm run dev"}, cmd)
}

func TestParseDockerfileCmd_LastCmdWins(t *testing.T) {
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")

	dockerfile := `FROM node:18
CMD ["npm", "install"]
CMD ["npm", "start"]
`
	err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644)
	require.NoError(t, err)

	cmd, err := ParseDockerfileCmd(dockerfilePath)
	require.NoError(t, err)
	assert.Equal(t, []string{"npm", "start"}, cmd)
}

func TestParseDockerfileCmd_NoCmdFound(t *testing.T) {
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")

	dockerfile := `FROM node:18
WORKDIR /app
`
	err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644)
	require.NoError(t, err)

	_, err = ParseDockerfileCmd(dockerfilePath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no CMD instruction found")
}

func TestParseDockerfileCmd_WithComments(t *testing.T) {
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")

	dockerfile := `FROM node:18
# This is a comment
# CMD ["npm", "test"]
CMD ["npm", "start"]
`
	err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644)
	require.NoError(t, err)

	cmd, err := ParseDockerfileCmd(dockerfilePath)
	require.NoError(t, err)
	assert.Equal(t, []string{"npm", "start"}, cmd)
}

func TestExtractDockerfileCmdFromContext(t *testing.T) {
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")

	dockerfile := `FROM python:3.11
CMD ["python", "app.py"]
`
	err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644)
	require.NoError(t, err)

	cmd, err := ExtractDockerfileCmdFromContext(tmpDir, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"python", "app.py"}, cmd)
}

func TestExtractDockerfileCmdFromContext_CustomDockerfile(t *testing.T) {
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile.dev")

	dockerfile := `FROM node:18
CMD ["npm", "run", "dev"]
`
	err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644)
	require.NoError(t, err)

	cmd, err := ExtractDockerfileCmdFromContext(tmpDir, "Dockerfile.dev")
	require.NoError(t, err)
	assert.Equal(t, []string{"npm", "run", "dev"}, cmd)
}

func TestEvaluateFunction_Coalesce(t *testing.T) {
	ctx := &EvalContext{
		Inputs: map[string]interface{}{
			"command": []interface{}{"npm", "start"},
			"empty":   "",
		},
	}

	// Test coalesce with valid value
	result, err := evaluateFunction("coalesce(inputs.command, inputs.empty)", ctx)
	require.NoError(t, err)
	assert.Equal(t, []interface{}{"npm", "start"}, result)

	// Test coalesce with empty string falling back
	result, err = evaluateFunction("coalesce(inputs.empty, inputs.command)", ctx)
	require.NoError(t, err)
	assert.Equal(t, []interface{}{"npm", "start"}, result)
}

func TestEvaluateFunction_DockerfileCmd(t *testing.T) {
	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")

	dockerfile := `FROM node:18
CMD ["node", "server.js"]
`
	err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644)
	require.NoError(t, err)

	ctx := &EvalContext{
		Inputs: map[string]interface{}{
			"context": tmpDir,
		},
	}

	result, err := evaluateFunction("dockerfile_cmd(inputs.context)", ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"node", "server.js"}, result)
}

func newTestProcessManager() *ProcessManager {
	return &ProcessManager{
		processes: make(map[string]*managedProcess),
	}
}

func TestWaitForReady_TCPSuccess(t *testing.T) {
	// Start a TCP listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	pm := newTestProcessManager()
	readiness := &ReadinessCheck{
		Type:     "tcp",
		Endpoint: listener.Addr().String(),
		Interval: 50 * time.Millisecond,
		Timeout:  2 * time.Second,
	}

	err = pm.waitForReady(context.Background(), readiness)
	assert.NoError(t, err)
}

func TestWaitForReady_TCPTimeout(t *testing.T) {
	// Use a port that nothing is listening on
	pm := newTestProcessManager()
	readiness := &ReadinessCheck{
		Type:     "tcp",
		Endpoint: "127.0.0.1:1", // Port 1 is unlikely to be open
		Interval: 50 * time.Millisecond,
		Timeout:  200 * time.Millisecond,
	}

	err := pm.waitForReady(context.Background(), readiness)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "did not become ready")
}

func TestWaitForReady_TCPContextCancelled(t *testing.T) {
	pm := newTestProcessManager()
	readiness := &ReadinessCheck{
		Type:     "tcp",
		Endpoint: "127.0.0.1:1",
		Interval: 50 * time.Millisecond,
		Timeout:  10 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := pm.waitForReady(ctx, readiness)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestWaitForReady_HTTPSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	pm := newTestProcessManager()
	readiness := &ReadinessCheck{
		Type:     "http",
		Endpoint: server.URL,
		Interval: 50 * time.Millisecond,
		Timeout:  2 * time.Second,
	}

	err := pm.waitForReady(context.Background(), readiness)
	assert.NoError(t, err)
}

func TestWaitForReady_HTTPTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	pm := newTestProcessManager()
	readiness := &ReadinessCheck{
		Type:     "http",
		Endpoint: server.URL,
		Interval: 50 * time.Millisecond,
		Timeout:  200 * time.Millisecond,
	}

	err := pm.waitForReady(context.Background(), readiness)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "did not become ready")
}

func TestWaitForReady_UnsupportedType(t *testing.T) {
	pm := newTestProcessManager()
	readiness := &ReadinessCheck{
		Type:     "exec",
		Endpoint: "echo hello",
		Interval: 50 * time.Millisecond,
		Timeout:  200 * time.Millisecond,
	}

	err := pm.waitForReady(context.Background(), readiness)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported readiness check type")
}

func TestSplitFunctionArgs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single arg",
			input:    "inputs.name",
			expected: []string{"inputs.name"},
		},
		{
			name:     "two args",
			input:    "inputs.command, inputs.default",
			expected: []string{"inputs.command", "inputs.default"},
		},
		{
			name:     "three args",
			input:    "a, b, c",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "nested function",
			input:    "coalesce(a, b), c",
			expected: []string{"coalesce(a, b)", "c"},
		},
		{
			name:     "empty",
			input:    "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitFunctionArgs(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
