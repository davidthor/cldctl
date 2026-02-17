package ciworkflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubActionsGenerator_Generate_ComponentMode(t *testing.T) {
	gen := NewGitHubActionsGenerator()

	wf := Workflow{
		Name: "Deploy my-app",
		Mode: ModeComponent,
		Jobs: []Job{
			{
				ID:           "database-main",
				Name:         "Apply database/main",
				NodeType:     "database",
				NodeName:     "main",
				ApplyCommand: "cldctl apply $COMPONENT_IMAGE database/main -e $ENVIRONMENT -d $DATACENTER",
			},
			{
				ID:           "deployment-api",
				Name:         "Apply deployment/api",
				NodeType:     "deployment",
				NodeName:     "api",
				DependsOn:    []string{"database-main"},
				ApplyCommand: "cldctl apply $COMPONENT_IMAGE deployment/api -e $ENVIRONMENT -d $DATACENTER",
			},
		},
		EnvVars: map[string]string{
			"COMPONENT_IMAGE": "ghcr.io/org/app:latest",
			"ENVIRONMENT":     "${{ vars.CLDCTL_ENVIRONMENT }}",
			"DATACENTER":      "${{ vars.CLDCTL_DATACENTER }}",
		},
		InstallVersion: "latest",
	}

	data, err := gen.Generate(wf)
	require.NoError(t, err)

	output := string(data)

	assert.Contains(t, output, "name: Deploy my-app")
	assert.Contains(t, output, "push:")
	assert.Contains(t, output, "branches: [main]")
	assert.Contains(t, output, "COMPONENT_IMAGE:")
	assert.Contains(t, output, "database-main:")
	assert.Contains(t, output, "deployment-api:")
	assert.Contains(t, output, "needs: [database-main]")
	assert.Contains(t, output, "Install cldctl")
	assert.Contains(t, output, "curl -sSL https://get.cldctl.dev | sh")
}

func TestGitHubActionsGenerator_Generate_EnvironmentMode(t *testing.T) {
	gen := NewGitHubActionsGenerator()

	wf := Workflow{
		Name: "Preview Deploy",
		Mode: ModeEnvironment,
		Jobs: []Job{
			{
				ID:   "create-environment",
				Name: "Create Environment",
				Steps: []Step{
					{Name: "Create environment", Run: "cldctl create environment $ENVIRONMENT -d $DATACENTER"},
				},
			},
		},
		EnvVars: map[string]string{
			"ENVIRONMENT": "preview-${{ github.event.pull_request.number }}",
			"DATACENTER":  "${{ vars.CLDCTL_DATACENTER }}",
		},
	}

	data, err := gen.Generate(wf)
	require.NoError(t, err)

	output := string(data)
	assert.Contains(t, output, "pull_request:")
	assert.Contains(t, output, "types: [opened, synchronize]")
}

func TestGitHubActionsGenerator_GenerateTeardown(t *testing.T) {
	gen := NewGitHubActionsGenerator()

	wf := Workflow{
		Name: "Preview Deploy",
		Mode: ModeEnvironment,
		TeardownJobs: []Job{
			{
				ID:   "destroy-components",
				Name: "Destroy Components",
				Steps: []Step{
					{Name: "Destroy my-app", Run: "cldctl destroy component my-app -e $ENVIRONMENT -d $DATACENTER --force"},
				},
			},
			{
				ID:        "destroy-environment",
				Name:      "Destroy Environment",
				DependsOn: []string{"destroy-components"},
				Steps: []Step{
					{Name: "Destroy environment", Run: "cldctl destroy environment $ENVIRONMENT -d $DATACENTER"},
				},
			},
		},
		EnvVars: map[string]string{
			"ENVIRONMENT": "preview-${{ github.event.pull_request.number }}",
			"DATACENTER":  "${{ vars.CLDCTL_DATACENTER }}",
		},
	}

	data, err := gen.GenerateTeardown(wf)
	require.NoError(t, err)
	require.NotNil(t, data)

	output := string(data)
	assert.Contains(t, output, "Preview Teardown")
	assert.Contains(t, output, "types: [closed]")
	assert.Contains(t, output, "destroy-components:")
	assert.Contains(t, output, "destroy-environment:")
}

func TestGitHubActionsGenerator_GenerateTeardown_ComponentMode_ReturnsNil(t *testing.T) {
	gen := NewGitHubActionsGenerator()
	data, err := gen.GenerateTeardown(Workflow{Mode: ModeComponent})
	assert.NoError(t, err)
	assert.Nil(t, data)
}

func TestGitHubActionsGenerator_SetupComment(t *testing.T) {
	gen := NewGitHubActionsGenerator()

	wf := Workflow{
		Name: "Deploy",
		Mode: ModeComponent,
		Jobs: []Job{},
		Variables: []WorkflowVariable{
			{Name: "api_key", EnvName: "API_KEY", Sensitive: true, Required: true, Description: "API key"},
			{Name: "region", EnvName: "REGION", Sensitive: false, Required: true},
		},
		EnvVars: map[string]string{},
	}

	data, err := gen.Generate(wf)
	require.NoError(t, err)

	output := string(data)
	assert.Contains(t, output, "Secrets:")
	assert.Contains(t, output, "API_KEY")
	assert.Contains(t, output, "Variables:")
	assert.Contains(t, output, "REGION")
}

func TestGitHubActionsGenerator_NeedsCheckout(t *testing.T) {
	gen := NewGitHubActionsGenerator()

	wf := Workflow{
		Name: "Deploy",
		Mode: ModeComponent,
		Jobs: []Job{
			{
				ID:            "dockerBuild-api",
				Name:          "Apply dockerBuild/api",
				NodeType:      "dockerBuild",
				NodeName:      "api",
				NeedsCheckout: true,
				ApplyCommand:  "cldctl apply $COMPONENT_IMAGE dockerBuild/api -e $ENVIRONMENT -d $DATACENTER",
			},
		},
		EnvVars: map[string]string{},
	}

	data, err := gen.Generate(wf)
	require.NoError(t, err)

	output := string(data)
	assert.Contains(t, output, "actions/checkout@v4")
}

func TestGitHubActionsGenerator_CustomInstallVersion(t *testing.T) {
	gen := NewGitHubActionsGenerator()

	wf := Workflow{
		Name: "Deploy",
		Mode: ModeComponent,
		Jobs: []Job{
			{
				ID:           "database-main",
				Name:         "Apply database/main",
				NodeType:     "database",
				NodeName:     "main",
				ApplyCommand: "cldctl apply $COMPONENT_IMAGE database/main -e $ENVIRONMENT -d $DATACENTER",
			},
		},
		EnvVars:        map[string]string{},
		InstallVersion: "v1.2.3",
	}

	data, err := gen.Generate(wf)
	require.NoError(t, err)

	output := string(data)
	assert.Contains(t, output, "--version v1.2.3")
}

func TestGitHubActionsGenerator_DefaultOutputPath(t *testing.T) {
	gen := NewGitHubActionsGenerator()
	assert.Equal(t, ".github/workflows/deploy.yml", gen.DefaultOutputPath())
}

func TestGitHubActionsGenerator_DefaultTeardownOutputPath(t *testing.T) {
	gen := NewGitHubActionsGenerator()
	assert.Equal(t, ".github/workflows/teardown.yml", gen.DefaultTeardownOutputPath())
}

func TestGitHubActionsGenerator_MultilineApplyCommand(t *testing.T) {
	gen := NewGitHubActionsGenerator()

	wf := Workflow{
		Name: "Deploy",
		Mode: ModeComponent,
		Jobs: []Job{
			{
				ID:           "database-main",
				Name:         "Apply database/main",
				NodeType:     "database",
				NodeName:     "main",
				ApplyCommand: "cldctl apply $COMPONENT_IMAGE database/main -e $ENVIRONMENT -d $DATACENTER --var key=$VALUE",
			},
		},
		EnvVars: map[string]string{},
	}

	data, err := gen.Generate(wf)
	require.NoError(t, err)

	output := string(data)
	// The >- YAML multiline format should be used
	assert.True(t, strings.Contains(output, "run: >-"), "should use YAML multiline format")
}
