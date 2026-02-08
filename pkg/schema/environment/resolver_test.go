package environment

import (
	"os"
	"testing"

	"github.com/davidthor/cldctl/pkg/schema/environment/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveVariables_CLIOverride(t *testing.T) {
	env := &internal.InternalEnvironment{
		Variables: map[string]internal.InternalEnvironmentVariable{
			"api_key": {
				Name:     "api_key",
				Required: true,
			},
		},
		Components: map[string]internal.InternalComponentConfig{
			"my-app": {
				Variables: map[string]interface{}{
					"key": "${{ variables.api_key }}",
				},
			},
		},
	}

	err := ResolveVariables(env, ResolveOptions{
		CLIVars: map[string]string{"api_key": "cli-value"},
	})
	require.NoError(t, err)
	assert.Equal(t, "cli-value", env.Components["my-app"].Variables["key"])
}

func TestResolveVariables_OSEnvVar(t *testing.T) {
	// Set an OS env var
	os.Setenv("MY_SECRET", "from-env")
	defer os.Unsetenv("MY_SECRET")

	env := &internal.InternalEnvironment{
		Variables: map[string]internal.InternalEnvironmentVariable{
			"my_secret": {
				Name:     "my_secret",
				Required: true,
			},
		},
		Components: map[string]internal.InternalComponentConfig{
			"app": {
				Variables: map[string]interface{}{
					"secret": "${{ variables.my_secret }}",
				},
			},
		},
	}

	err := ResolveVariables(env, ResolveOptions{})
	require.NoError(t, err)
	assert.Equal(t, "from-env", env.Components["app"].Variables["secret"])
}

func TestResolveVariables_ExplicitEnvField(t *testing.T) {
	os.Setenv("GOOGLE_CLOUD_PROJECT", "my-project")
	defer os.Unsetenv("GOOGLE_CLOUD_PROJECT")

	env := &internal.InternalEnvironment{
		Variables: map[string]internal.InternalEnvironmentVariable{
			"project_id": {
				Name:     "project_id",
				Required: true,
				Env:      "GOOGLE_CLOUD_PROJECT",
			},
		},
		Components: map[string]internal.InternalComponentConfig{
			"vertex": {
				Variables: map[string]interface{}{
					"project": "${{ variables.project_id }}",
				},
			},
		},
	}

	err := ResolveVariables(env, ResolveOptions{})
	require.NoError(t, err)
	assert.Equal(t, "my-project", env.Components["vertex"].Variables["project"])
}

func TestResolveVariables_DotenvFallback(t *testing.T) {
	env := &internal.InternalEnvironment{
		Variables: map[string]internal.InternalEnvironmentVariable{
			"api_key": {
				Name:     "api_key",
				Required: true,
			},
		},
		Components: map[string]internal.InternalComponentConfig{
			"app": {
				Variables: map[string]interface{}{
					"key": "${{ variables.api_key }}",
				},
			},
		},
	}

	err := ResolveVariables(env, ResolveOptions{
		DotenvVars: map[string]string{"API_KEY": "from-dotenv"},
	})
	require.NoError(t, err)
	assert.Equal(t, "from-dotenv", env.Components["app"].Variables["key"])
}

func TestResolveVariables_DefaultValue(t *testing.T) {
	env := &internal.InternalEnvironment{
		Variables: map[string]internal.InternalEnvironmentVariable{
			"debug": {
				Name:    "debug",
				Default: "false",
			},
		},
		Components: map[string]internal.InternalComponentConfig{
			"app": {
				Variables: map[string]interface{}{
					"debug_mode": "${{ variables.debug }}",
				},
			},
		},
	}

	err := ResolveVariables(env, ResolveOptions{})
	require.NoError(t, err)
	assert.Equal(t, "false", env.Components["app"].Variables["debug_mode"])
}

func TestResolveVariables_MissingRequired(t *testing.T) {
	env := &internal.InternalEnvironment{
		Variables: map[string]internal.InternalEnvironmentVariable{
			"api_key": {
				Name:     "api_key",
				Required: true,
			},
		},
		Components: map[string]internal.InternalComponentConfig{},
	}

	err := ResolveVariables(env, ResolveOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_key")
	assert.Contains(t, err.Error(), "missing required")
}

func TestResolveVariables_PriorityOrder(t *testing.T) {
	// CLI > OS env > dotenv > default
	os.Setenv("PRIORITY_TEST", "from-os")
	defer os.Unsetenv("PRIORITY_TEST")

	env := &internal.InternalEnvironment{
		Variables: map[string]internal.InternalEnvironmentVariable{
			"priority_test": {
				Name:    "priority_test",
				Default: "from-default",
			},
		},
		Components: map[string]internal.InternalComponentConfig{
			"app": {
				Variables: map[string]interface{}{
					"val": "${{ variables.priority_test }}",
				},
			},
		},
	}

	// CLI wins over all
	err := ResolveVariables(env, ResolveOptions{
		CLIVars:    map[string]string{"priority_test": "from-cli"},
		DotenvVars: map[string]string{"PRIORITY_TEST": "from-dotenv"},
	})
	require.NoError(t, err)
	assert.Equal(t, "from-cli", env.Components["app"].Variables["val"])
}

func TestResolveVariables_LocalsExpression(t *testing.T) {
	env := &internal.InternalEnvironment{
		Variables: map[string]internal.InternalEnvironmentVariable{},
		Locals: map[string]interface{}{
			"log_level": "debug",
		},
		Components: map[string]internal.InternalComponentConfig{
			"app": {
				Variables: map[string]interface{}{
					"level": "${{ locals.log_level }}",
				},
			},
		},
	}

	err := ResolveVariables(env, ResolveOptions{})
	require.NoError(t, err)
	assert.Equal(t, "debug", env.Components["app"].Variables["level"])
}

func TestResolveVariables_MixedExpressions(t *testing.T) {
	env := &internal.InternalEnvironment{
		Variables: map[string]internal.InternalEnvironmentVariable{
			"host": {
				Name:    "host",
				Default: "api.example.com",
			},
		},
		Locals: map[string]interface{}{
			"protocol": "https",
		},
		Components: map[string]internal.InternalComponentConfig{
			"app": {
				Variables: map[string]interface{}{
					"url": "${{ locals.protocol }}://${{ variables.host }}",
				},
			},
		},
	}

	err := ResolveVariables(env, ResolveOptions{})
	require.NoError(t, err)
	assert.Equal(t, "https://api.example.com", env.Components["app"].Variables["url"])
}

func TestResolveVariables_NonStringPassthrough(t *testing.T) {
	env := &internal.InternalEnvironment{
		Variables: map[string]internal.InternalEnvironmentVariable{},
		Components: map[string]internal.InternalComponentConfig{
			"app": {
				Variables: map[string]interface{}{
					"count":   42,
					"enabled": true,
				},
			},
		},
	}

	err := ResolveVariables(env, ResolveOptions{})
	require.NoError(t, err)
	assert.Equal(t, 42, env.Components["app"].Variables["count"])
	assert.Equal(t, true, env.Components["app"].Variables["enabled"])
}

func TestResolveVariables_UndefinedVariable(t *testing.T) {
	env := &internal.InternalEnvironment{
		Variables: map[string]internal.InternalEnvironmentVariable{},
		Components: map[string]internal.InternalComponentConfig{
			"app": {
				Variables: map[string]interface{}{
					"key": "${{ variables.nonexistent }}",
				},
			},
		},
	}

	err := ResolveVariables(env, ResolveOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
}

func TestResolveVariables_NoVariablesBlock(t *testing.T) {
	// Environment with no variables block at all -- should work fine
	env := &internal.InternalEnvironment{
		Variables: map[string]internal.InternalEnvironmentVariable{},
		Components: map[string]internal.InternalComponentConfig{
			"app": {
				Variables: map[string]interface{}{
					"static": "just a string",
				},
			},
		},
	}

	err := ResolveVariables(env, ResolveOptions{})
	require.NoError(t, err)
	assert.Equal(t, "just a string", env.Components["app"].Variables["static"])
}
