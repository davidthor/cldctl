package visual

import (
	"strings"
	"testing"

	"github.com/davidthor/cldctl/pkg/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildTestGraph() *graph.Graph {
	g := graph.NewGraph("staging", "my-dc")

	dbNode := graph.NewNode(graph.NodeTypeDatabase, "my-app", "main")
	deployNode := graph.NewNode(graph.NodeTypeDeployment, "my-app", "api")
	svcNode := graph.NewNode(graph.NodeTypeService, "my-app", "api")
	routeNode := graph.NewNode(graph.NodeTypeRoute, "my-app", "main")

	_ = g.AddNode(dbNode)
	_ = g.AddNode(deployNode)
	_ = g.AddNode(svcNode)
	_ = g.AddNode(routeNode)

	_ = g.AddEdge(deployNode.ID, dbNode.ID)
	_ = g.AddEdge(svcNode.ID, deployNode.ID)
	_ = g.AddEdge(routeNode.ID, svcNode.ID)

	return g
}

func buildMultiComponentGraph() *graph.Graph {
	g := graph.NewGraph("staging", "my-dc")

	authDB := graph.NewNode(graph.NodeTypeDatabase, "auth", "main")
	authDeploy := graph.NewNode(graph.NodeTypeDeployment, "auth", "api")

	appDB := graph.NewNode(graph.NodeTypeDatabase, "my-app", "main")
	appDeploy := graph.NewNode(graph.NodeTypeDeployment, "my-app", "api")

	_ = g.AddNode(authDB)
	_ = g.AddNode(authDeploy)
	_ = g.AddNode(appDB)
	_ = g.AddNode(appDeploy)

	_ = g.AddEdge(authDeploy.ID, authDB.ID)
	_ = g.AddEdge(appDeploy.ID, appDB.ID)
	_ = g.AddEdge(appDeploy.ID, authDeploy.ID)

	return g
}

func TestRenderMermaid_NilGraph(t *testing.T) {
	_, err := RenderMermaid(nil, MermaidOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestRenderMermaid_EmptyGraph(t *testing.T) {
	g := graph.NewGraph("staging", "my-dc")
	result, err := RenderMermaid(g, MermaidOptions{})
	require.NoError(t, err)
	assert.Contains(t, result, "flowchart TD")
}

func TestRenderMermaid_SimpleGraph(t *testing.T) {
	g := buildTestGraph()
	result, err := RenderMermaid(g, MermaidOptions{})
	require.NoError(t, err)

	assert.Contains(t, result, "flowchart TD")
	assert.Contains(t, result, "database/main")
	assert.Contains(t, result, "deployment/api")
	assert.Contains(t, result, "service/api")
	assert.Contains(t, result, "route/main")
	assert.Contains(t, result, "-->")
}

func TestRenderMermaid_WithDirection(t *testing.T) {
	g := buildTestGraph()
	result, err := RenderMermaid(g, MermaidOptions{Direction: "LR"})
	require.NoError(t, err)
	assert.Contains(t, result, "flowchart LR")
}

func TestRenderMermaid_WithTitle(t *testing.T) {
	g := buildTestGraph()
	result, err := RenderMermaid(g, MermaidOptions{Title: "My Workflow"})
	require.NoError(t, err)
	assert.Contains(t, result, "title: My Workflow")
}

func TestRenderMermaid_GroupByComponent(t *testing.T) {
	g := buildMultiComponentGraph()
	result, err := RenderMermaid(g, MermaidOptions{GroupByComponent: true})
	require.NoError(t, err)

	assert.Contains(t, result, "subgraph")
	assert.Contains(t, result, `"auth"`)
	assert.Contains(t, result, `"my-app"`)
	assert.Contains(t, result, "end")
}

func TestRenderMermaid_WithSetupJobs(t *testing.T) {
	g := buildTestGraph()
	opts := MermaidOptions{
		SetupJobs: []SetupJob{
			{ID: "build-and-push", Label: "Build & Push"},
		},
	}

	result, err := RenderMermaid(g, opts)
	require.NoError(t, err)

	assert.Contains(t, result, `build-and-push["Build & Push"]`)
	assert.Contains(t, result, "build-and-push -->")
}

func TestRenderMermaid_SetupJobDependencies(t *testing.T) {
	g := buildTestGraph()
	opts := MermaidOptions{
		SetupJobs: []SetupJob{
			{ID: "check-deps", Label: "Check Dependencies"},
			{ID: "build", Label: "Build", DependsOn: []string{"check-deps"}},
		},
	}

	result, err := RenderMermaid(g, opts)
	require.NoError(t, err)

	assert.Contains(t, result, "check-deps --> build")
}

func TestRenderMermaid_DeterministicOutput(t *testing.T) {
	// Run multiple times and verify same output
	g := buildTestGraph()
	opts := MermaidOptions{}

	var results []string
	for i := 0; i < 5; i++ {
		result, err := RenderMermaid(g, opts)
		require.NoError(t, err)
		results = append(results, result)
	}

	for i := 1; i < len(results); i++ {
		assert.Equal(t, results[0], results[i], "output should be deterministic")
	}
}

func TestNodeLabel(t *testing.T) {
	tests := []struct {
		name     string
		nodeType graph.NodeType
		nodeName string
		expected string
	}{
		{"database", graph.NodeTypeDatabase, "main", "database/main"},
		{"deployment", graph.NodeTypeDeployment, "api", "deployment/api"},
		{"dockerBuild", graph.NodeTypeDockerBuild, "web", "dockerBuild/web"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := graph.NewNode(tt.nodeType, "comp", tt.nodeName)
			assert.Equal(t, tt.expected, nodeLabel(node))
		})
	}
}

func TestSanitizeMermaidID(t *testing.T) {
	node := graph.NewNode(graph.NodeTypeDatabase, "my-app", "main")
	// ID is "my-app/database/main"
	result := sanitizeMermaidID(node)
	assert.Equal(t, "my-app--database--main", result)
	assert.False(t, strings.Contains(result, "/"))
}

func TestEscapeMermaidLabel(t *testing.T) {
	assert.Equal(t, `hello #quot;world#quot;`, escapeMermaidLabel(`hello "world"`))
	assert.Equal(t, "simple", escapeMermaidLabel("simple"))
}
