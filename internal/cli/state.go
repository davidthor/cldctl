package cli

import (
	"os"
	"strings"

	"github.com/davidthor/cldctl/pkg/state"
	"github.com/davidthor/cldctl/pkg/state/backend"
)

// Environment variable names for state backend configuration.
const (
	// EnvStateBackend sets the state backend type (local, s3, gcs, azurerm).
	EnvStateBackend = "CLDCTL_STATE_BACKEND"

	// EnvStatePrefix is the prefix for backend-specific config environment variables.
	// For example, CLDCTL_STATE_PATH sets the "path" config for the local backend,
	// CLDCTL_STATE_BUCKET sets the "bucket" config for S3/GCS backends.
	EnvStatePrefix = "CLDCTL_STATE_"
)

// createStateManagerWithConfig creates a state manager with the given backend type and config.
//
// Configuration precedence (highest to lowest):
//  1. CLI flags (--backend, --backend-config)
//  2. Environment variables (CLDCTL_STATE_BACKEND, CLDCTL_STATE_*)
//  3. Hardcoded defaults (local backend with ~/.cldctl/state)
func createStateManagerWithConfig(backendType string, backendConfig []string) (state.Manager, error) {
	// Start with hardcoded default
	effectiveBackend := "local"
	effectiveConfig := make(map[string]string)

	// Apply environment variables
	if envBackend := os.Getenv(EnvStateBackend); envBackend != "" {
		effectiveBackend = envBackend
	}

	// Check for backend-specific env vars (CLDCTL_STATE_PATH, CLDCTL_STATE_BUCKET, etc.)
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, EnvStatePrefix) && !strings.HasPrefix(env, EnvStateBackend) {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				// Convert CLDCTL_STATE_PATH to "path", CLDCTL_STATE_BUCKET to "bucket", etc.
				key := strings.ToLower(strings.TrimPrefix(parts[0], EnvStatePrefix))
				effectiveConfig[key] = parts[1]
			}
		}
	}

	// Apply CLI flags (highest priority)
	if backendType != "" {
		effectiveBackend = backendType
	}

	for _, c := range backendConfig {
		parts := strings.SplitN(c, "=", 2)
		if len(parts) == 2 {
			effectiveConfig[parts[0]] = parts[1]
		}
	}

	config := backend.Config{
		Type:   effectiveBackend,
		Config: effectiveConfig,
	}

	return state.NewManagerFromConfig(config)
}
