package ciworkflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitLabCIGenerator_Generate(t *testing.T) {
	gen := NewGitLabCIGenerator()

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
			"ENVIRONMENT": "staging",
		},
	}

	data, err := gen.Generate(wf)
	require.NoError(t, err)

	output := string(data)
	assert.Contains(t, output, "stages:")
	assert.Contains(t, output, "stage-0")
	assert.Contains(t, output, "variables:")
	assert.Contains(t, output, "ENVIRONMENT:")
	assert.Contains(t, output, "database-main:")
	assert.Contains(t, output, "deployment-api:")
	assert.Contains(t, output, "needs:")
	assert.Contains(t, output, "*install-cldctl")
	assert.Contains(t, output, "image: ubuntu:latest")
}

func TestGitLabCIGenerator_DefaultOutputPath(t *testing.T) {
	gen := NewGitLabCIGenerator()
	assert.Equal(t, ".gitlab-ci.yml", gen.DefaultOutputPath())
}

func TestGitLabCIGenerator_GenerateTeardown(t *testing.T) {
	gen := NewGitLabCIGenerator()

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
			"ENVIRONMENT": "preview-123",
			"DATACENTER":  "my-dc",
		},
	}

	data, err := gen.GenerateTeardown(wf)
	require.NoError(t, err)
	require.NotNil(t, data)

	output := string(data)
	assert.Contains(t, output, "destroy-components:")
	assert.Contains(t, output, "destroy-environment:")
	assert.Contains(t, output, "ENVIRONMENT:")
}

func TestGitLabCI_ComputeJobDepths(t *testing.T) {
	jobs := []Job{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	}

	depths := computeJobDepths(jobs)
	assert.Equal(t, 0, depths["a"])
	assert.Equal(t, 1, depths["b"])
	assert.Equal(t, 2, depths["c"])
}

func TestGitLabCI_DeriveStages(t *testing.T) {
	jobs := []Job{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	}

	stages := deriveStages(jobs)
	assert.Equal(t, []string{"stage-0", "stage-1", "stage-2"}, stages)
}

func TestGitLabCI_DeriveStages_Empty(t *testing.T) {
	assert.Nil(t, deriveStages(nil))
}
