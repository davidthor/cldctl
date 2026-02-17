package ciworkflow

import (
	"testing"

	"github.com/davidthor/cldctl/pkg/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildTestGraph() *graph.Graph {
	g := graph.NewGraph("staging", "my-dc")

	dbNode := graph.NewNode(graph.NodeTypeDatabase, "my-app", "main")
	buildNode := graph.NewNode(graph.NodeTypeDockerBuild, "my-app", "api")
	deployNode := graph.NewNode(graph.NodeTypeDeployment, "my-app", "api")
	svcNode := graph.NewNode(graph.NodeTypeService, "my-app", "api")
	routeNode := graph.NewNode(graph.NodeTypeRoute, "my-app", "main")

	_ = g.AddNode(dbNode)
	_ = g.AddNode(buildNode)
	_ = g.AddNode(deployNode)
	_ = g.AddNode(svcNode)
	_ = g.AddNode(routeNode)

	_ = g.AddEdge(deployNode.ID, dbNode.ID)
	_ = g.AddEdge(deployNode.ID, buildNode.ID)
	_ = g.AddEdge(svcNode.ID, deployNode.ID)
	_ = g.AddEdge(routeNode.ID, svcNode.ID)

	return g
}

func buildMultiComponentGraph() *graph.Graph {
	g := graph.NewGraph("staging", "my-dc")

	authDB := graph.NewNode(graph.NodeTypeDatabase, "auth", "main")
	authDeploy := graph.NewNode(graph.NodeTypeDeployment, "auth", "api")
	authSvc := graph.NewNode(graph.NodeTypeService, "auth", "api")

	appDB := graph.NewNode(graph.NodeTypeDatabase, "my-app", "main")
	appDeploy := graph.NewNode(graph.NodeTypeDeployment, "my-app", "api")

	_ = g.AddNode(authDB)
	_ = g.AddNode(authDeploy)
	_ = g.AddNode(authSvc)
	_ = g.AddNode(appDB)
	_ = g.AddNode(appDeploy)

	_ = g.AddEdge(authDeploy.ID, authDB.ID)
	_ = g.AddEdge(authSvc.ID, authDeploy.ID)
	_ = g.AddEdge(appDeploy.ID, appDB.ID)
	_ = g.AddEdge(appDeploy.ID, authSvc.ID)

	return g
}

func TestBuildJobs_ComponentMode(t *testing.T) {
	g := buildTestGraph()
	components := []ComponentRef{{Name: "my-app"}}

	jobs, err := BuildJobs(g, ModeComponent, components)
	require.NoError(t, err)
	assert.Len(t, jobs, 5)

	// Check job IDs follow component mode naming
	jobIDs := make(map[string]bool)
	for _, job := range jobs {
		jobIDs[job.ID] = true
	}

	assert.True(t, jobIDs["database-main"])
	assert.True(t, jobIDs["dockerBuild-api"])
	assert.True(t, jobIDs["deployment-api"])
	assert.True(t, jobIDs["service-api"])
	assert.True(t, jobIDs["route-main"])
}

func TestBuildJobs_ComponentMode_Dependencies(t *testing.T) {
	g := buildTestGraph()
	components := []ComponentRef{{Name: "my-app"}}

	jobs, err := BuildJobs(g, ModeComponent, components)
	require.NoError(t, err)

	jobMap := make(map[string]Job)
	for _, j := range jobs {
		jobMap[j.ID] = j
	}

	// deployment-api depends on database-main and dockerBuild-api
	deployJob := jobMap["deployment-api"]
	assert.Contains(t, deployJob.DependsOn, "database-main")
	assert.Contains(t, deployJob.DependsOn, "dockerBuild-api")

	// service-api depends on deployment-api
	svcJob := jobMap["service-api"]
	assert.Contains(t, svcJob.DependsOn, "deployment-api")

	// route-main depends on service-api
	routeJob := jobMap["route-main"]
	assert.Contains(t, routeJob.DependsOn, "service-api")
}

func TestBuildJobs_ComponentMode_DockerBuildCheckout(t *testing.T) {
	g := buildTestGraph()
	components := []ComponentRef{{Name: "my-app"}}

	jobs, err := BuildJobs(g, ModeComponent, components)
	require.NoError(t, err)

	jobMap := make(map[string]Job)
	for _, j := range jobs {
		jobMap[j.ID] = j
	}

	assert.True(t, jobMap["dockerBuild-api"].NeedsCheckout)
	assert.False(t, jobMap["database-main"].NeedsCheckout)
	assert.False(t, jobMap["deployment-api"].NeedsCheckout)
}

func TestBuildJobs_EnvironmentMode(t *testing.T) {
	g := buildMultiComponentGraph()
	components := []ComponentRef{
		{Name: "auth", Image: "ghcr.io/org/auth:latest"},
		{Name: "my-app", Path: "."},
	}

	jobs, err := BuildJobs(g, ModeEnvironment, components)
	require.NoError(t, err)
	assert.Len(t, jobs, 5)

	// Check environment mode naming: component--type-name
	jobIDs := make(map[string]bool)
	for _, job := range jobs {
		jobIDs[job.ID] = true
	}

	assert.True(t, jobIDs["auth--database-main"])
	assert.True(t, jobIDs["auth--deployment-api"])
	assert.True(t, jobIDs["auth--service-api"])
	assert.True(t, jobIDs["my-app--database-main"])
	assert.True(t, jobIDs["my-app--deployment-api"])
}

func TestBuildJobs_EnvironmentMode_CrossComponentDeps(t *testing.T) {
	g := buildMultiComponentGraph()
	components := []ComponentRef{
		{Name: "auth"},
		{Name: "my-app"},
	}

	jobs, err := BuildJobs(g, ModeEnvironment, components)
	require.NoError(t, err)

	jobMap := make(map[string]Job)
	for _, j := range jobs {
		jobMap[j.ID] = j
	}

	// my-app deployment depends on auth service (cross-component)
	appDeploy := jobMap["my-app--deployment-api"]
	assert.Contains(t, appDeploy.DependsOn, "auth--service-api")
}

func TestBuildJobs_VarFlags(t *testing.T) {
	g := graph.NewGraph("staging", "my-dc")
	_ = g.AddNode(graph.NewNode(graph.NodeTypeDatabase, "my-app", "main"))

	components := []ComponentRef{{
		Name: "my-app",
		Variables: map[string]string{
			"api_key":   "$API_KEY",
			"log_level": "debug",
		},
	}}

	jobs, err := BuildJobs(g, ModeComponent, components)
	require.NoError(t, err)
	require.Len(t, jobs, 1)

	assert.Contains(t, jobs[0].VarFlags, "api_key=$API_KEY")
	assert.Contains(t, jobs[0].VarFlags, "log_level=debug")
}

func TestBuildJobs_ApplyCommand(t *testing.T) {
	g := graph.NewGraph("staging", "my-dc")
	_ = g.AddNode(graph.NewNode(graph.NodeTypeDatabase, "my-app", "main"))

	components := []ComponentRef{{
		Name:  "my-app",
		Image: "ghcr.io/org/app:v1",
	}}

	jobs, err := BuildJobs(g, ModeComponent, components)
	require.NoError(t, err)
	require.Len(t, jobs, 1)

	assert.Contains(t, jobs[0].ApplyCommand, "cldctl apply ghcr.io/org/app:v1 database/main")
	assert.Contains(t, jobs[0].ApplyCommand, "-e $ENVIRONMENT")
	assert.Contains(t, jobs[0].ApplyCommand, "-d $DATACENTER")
}

func TestBuildTeardownJobs(t *testing.T) {
	components := []ComponentRef{
		{Name: "auth"},
		{Name: "my-app"},
	}
	deps := map[string][]string{
		"my-app": {"auth"},
	}

	jobs := BuildTeardownJobs(components, deps)
	require.Len(t, jobs, 2)

	assert.Equal(t, "destroy-components", jobs[0].ID)
	assert.Equal(t, "destroy-environment", jobs[1].ID)
	assert.Contains(t, jobs[1].DependsOn, "destroy-components")

	// Verify destroy order: my-app first (depends on auth), then auth
	require.Len(t, jobs[0].Steps, 2)
	assert.Contains(t, jobs[0].Steps[0].Run, "my-app")
	assert.Contains(t, jobs[0].Steps[1].Run, "auth")
}

func TestMakeJobID_ComponentMode(t *testing.T) {
	node := graph.NewNode(graph.NodeTypeDatabase, "my-app", "main")
	assert.Equal(t, "database-main", makeJobID(node, ModeComponent))
}

func TestMakeJobID_EnvironmentMode(t *testing.T) {
	node := graph.NewNode(graph.NodeTypeDatabase, "my-app", "main")
	assert.Equal(t, "my-app--database-main", makeJobID(node, ModeEnvironment))
}

func TestBuildVarFlags_Sorted(t *testing.T) {
	vars := map[string]string{
		"zebra":  "z",
		"alpha":  "a",
		"middle": "m",
	}

	flags := buildVarFlags(vars)
	require.Len(t, flags, 3)
	assert.Equal(t, "alpha=a", flags[0])
	assert.Equal(t, "middle=m", flags[1])
	assert.Equal(t, "zebra=z", flags[2])
}

func TestBuildVarFlags_Empty(t *testing.T) {
	assert.Nil(t, buildVarFlags(nil))
	assert.Nil(t, buildVarFlags(map[string]string{}))
}
