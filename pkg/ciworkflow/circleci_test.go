package ciworkflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircleCIGenerator_Generate(t *testing.T) {
	gen := NewCircleCIGenerator()

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
		EnvVars: map[string]string{},
	}

	data, err := gen.Generate(wf)
	require.NoError(t, err)

	output := string(data)
	assert.Contains(t, output, "version: 2.1")
	assert.Contains(t, output, "commands:")
	assert.Contains(t, output, "install-cldctl:")
	assert.Contains(t, output, "jobs:")
	assert.Contains(t, output, "database-main:")
	assert.Contains(t, output, "deployment-api:")
	assert.Contains(t, output, "workflows:")
	assert.Contains(t, output, "requires:")
	assert.Contains(t, output, "cimg/base:current")
}

func TestCircleCIGenerator_DefaultOutputPath(t *testing.T) {
	gen := NewCircleCIGenerator()
	assert.Equal(t, ".circleci/config.yml", gen.DefaultOutputPath())
}

func TestCircleCIGenerator_NeedsCheckout(t *testing.T) {
	gen := NewCircleCIGenerator()

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
	assert.Contains(t, output, "- checkout")
}

func TestCircleCIGenerator_GenerateTeardown(t *testing.T) {
	gen := NewCircleCIGenerator()

	wf := Workflow{
		Mode: ModeEnvironment,
		TeardownJobs: []Job{
			{
				ID:   "destroy-components",
				Name: "Destroy Components",
				Steps: []Step{
					{Name: "Destroy app", Run: "cldctl destroy component app -e $ENVIRONMENT -d $DATACENTER --force"},
				},
			},
		},
		EnvVars: map[string]string{},
	}

	data, err := gen.GenerateTeardown(wf)
	require.NoError(t, err)
	require.NotNil(t, data)

	output := string(data)
	assert.Contains(t, output, "version: 2.1")
	assert.Contains(t, output, "destroy-components:")
	assert.Contains(t, output, "workflows:")
	assert.Contains(t, output, "teardown:")
}

func TestCircleCIGenerator_GenerateTeardown_ComponentMode_ReturnsNil(t *testing.T) {
	gen := NewCircleCIGenerator()
	data, err := gen.GenerateTeardown(Workflow{Mode: ModeComponent})
	assert.NoError(t, err)
	assert.Nil(t, data)
}

func TestSanitizeCircleCIID(t *testing.T) {
	assert.Equal(t, "deploy-my-app", sanitizeCircleCIID("Deploy my-app"))
	assert.Equal(t, "preview-deploy", sanitizeCircleCIID("Preview Deploy"))
}
