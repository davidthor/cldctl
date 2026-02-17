package visual

import (
	"strings"
	"testing"

	"github.com/davidthor/cldctl/pkg/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildInfraTestGraph() *graph.Graph {
	g := graph.NewGraph("staging", "my-dc")

	dbNode := graph.NewNode(graph.NodeTypeDatabase, "my-app", "main")
	dbNode.Inputs = map[string]interface{}{"type": "postgres:16"}

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

func TestRenderInfrastructureMermaid_NilGraph(t *testing.T) {
	_, err := RenderInfrastructureMermaid(nil, nil, InfrastructureOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestRenderInfrastructureMermaid_EmptyGraph(t *testing.T) {
	g := graph.NewGraph("staging", "my-dc")
	result, err := RenderInfrastructureMermaid(g, nil, InfrastructureOptions{})
	require.NoError(t, err)
	assert.Contains(t, result, "flowchart TD")
	assert.Contains(t, result, "Application Resources")
}

func TestRenderInfrastructureMermaid_BasicMapping(t *testing.T) {
	g := buildInfraTestGraph()
	matches := []HookMatch{
		{
			NodeID:    "my-app/database/main",
			NodeType:  "database",
			NodeName:  "main",
			Component: "my-app",
			HookIndex: 0,
			Modules:   []string{"docker-postgres"},
		},
		{
			NodeID:    "my-app/deployment/api",
			NodeType:  "deployment",
			NodeName:  "api",
			Component: "my-app",
			HookIndex: 0,
			Modules:   []string{"docker-deployment"},
		},
		{
			NodeID:    "my-app/service/api",
			NodeType:  "service",
			NodeName:  "api",
			Component: "my-app",
			HookIndex: 0,
			Modules:   []string{"docker-service"},
		},
		{
			NodeID:    "my-app/route/main",
			NodeType:  "route",
			NodeName:  "main",
			Component: "my-app",
			HookIndex: 0,
			Modules:   []string{"local-route"},
		},
	}

	result, err := RenderInfrastructureMermaid(g, matches, InfrastructureOptions{})
	require.NoError(t, err)

	// Should have two main subgraphs
	assert.Contains(t, result, "Application Resources")
	assert.Contains(t, result, "Infrastructure Modules")

	// Resource nodes with labels
	assert.Contains(t, result, "database/main (postgres:16)")
	assert.Contains(t, result, "deployment/api")
	assert.Contains(t, result, "service/api")
	assert.Contains(t, result, "route/main")

	// Module nodes
	assert.Contains(t, result, "docker-postgres")
	assert.Contains(t, result, "docker-deployment")
	assert.Contains(t, result, "docker-service")
	assert.Contains(t, result, "local-route")

	// Edges
	assert.Contains(t, result, "-->")
}

func TestRenderInfrastructureMermaid_WithTitle(t *testing.T) {
	g := buildInfraTestGraph()
	result, err := RenderInfrastructureMermaid(g, nil, InfrastructureOptions{
		Title: "Infrastructure Mapping",
	})
	require.NoError(t, err)
	assert.Contains(t, result, "title: Infrastructure Mapping")
}

func TestRenderInfrastructureMermaid_DirectionLR(t *testing.T) {
	g := buildInfraTestGraph()
	result, err := RenderInfrastructureMermaid(g, nil, InfrastructureOptions{
		Direction: "LR",
	})
	require.NoError(t, err)
	assert.Contains(t, result, "flowchart LR")
}

func TestRenderInfrastructureMermaid_ErrorHook(t *testing.T) {
	g := graph.NewGraph("staging", "my-dc")

	dbNode := graph.NewNode(graph.NodeTypeDatabase, "my-app", "main")
	dbNode.Inputs = map[string]interface{}{"type": "mongodb:7"}
	_ = g.AddNode(dbNode)

	matches := []HookMatch{
		{
			NodeID:    "my-app/database/main",
			NodeType:  "database",
			NodeName:  "main",
			Component: "my-app",
			HookIndex: 0,
			IsError:   true,
			Error:     "MongoDB is not supported",
		},
	}

	result, err := RenderInfrastructureMermaid(g, matches, InfrastructureOptions{})
	require.NoError(t, err)

	// Error nodes render with the errorNode class
	assert.Contains(t, result, ":::errorNode")
	assert.Contains(t, result, "ERROR: MongoDB is not supported")
	assert.Contains(t, result, "-.-x")

	// Should NOT have a modules subgraph since the only match is an error
	assert.NotContains(t, result, "Infrastructure Modules")
}

func TestRenderInfrastructureMermaid_GroupByComponent(t *testing.T) {
	g := graph.NewGraph("staging", "my-dc")

	authDB := graph.NewNode(graph.NodeTypeDatabase, "auth", "main")
	authDB.Inputs = map[string]interface{}{"type": "postgres:16"}
	appDB := graph.NewNode(graph.NodeTypeDatabase, "my-app", "main")
	appDB.Inputs = map[string]interface{}{"type": "postgres:16"}

	_ = g.AddNode(authDB)
	_ = g.AddNode(appDB)

	matches := []HookMatch{
		{
			NodeID:    "auth/database/main",
			NodeType:  "database",
			NodeName:  "main",
			Component: "auth",
			HookIndex: 0,
			Modules:   []string{"docker-postgres"},
		},
		{
			NodeID:    "my-app/database/main",
			NodeType:  "database",
			NodeName:  "main",
			Component: "my-app",
			HookIndex: 0,
			Modules:   []string{"docker-postgres"},
		},
	}

	result, err := RenderInfrastructureMermaid(g, matches, InfrastructureOptions{
		GroupByComponent: true,
	})
	require.NoError(t, err)

	// Should have nested subgraphs for each component within resources
	assert.Contains(t, result, `"auth"`)
	assert.Contains(t, result, `"my-app"`)
}

func TestRenderInfrastructureMermaid_MultiModuleHook(t *testing.T) {
	g := graph.NewGraph("staging", "my-dc")

	deployNode := graph.NewNode(graph.NodeTypeDeployment, "my-app", "api")
	_ = g.AddNode(deployNode)

	matches := []HookMatch{
		{
			NodeID:    "my-app/deployment/api",
			NodeType:  "deployment",
			NodeName:  "api",
			Component: "my-app",
			HookIndex: 0,
			Modules:   []string{"k8s-deployment", "k8s-hpa"},
		},
	}

	result, err := RenderInfrastructureMermaid(g, matches, InfrastructureOptions{})
	require.NoError(t, err)

	// Both modules should appear
	assert.Contains(t, result, "k8s-deployment")
	assert.Contains(t, result, "k8s-hpa")

	// Two edges from the single resource to two modules
	// Count the --> occurrences involving the resource
	edgeCount := 0
	for _, line := range splitLines(result) {
		if contains(line, "res_") && contains(line, "mod_") && contains(line, "-->") {
			edgeCount++
		}
	}
	assert.Equal(t, 2, edgeCount, "should have 2 resource-to-module edges")
}

func TestRenderInfrastructureMermaid_NoMatches(t *testing.T) {
	g := buildInfraTestGraph()
	result, err := RenderInfrastructureMermaid(g, nil, InfrastructureOptions{})
	require.NoError(t, err)

	// Resources should still render
	assert.Contains(t, result, "Application Resources")
	assert.Contains(t, result, "database/main")
	// No modules subgraph
	assert.NotContains(t, result, "Infrastructure Modules")
}

func TestRenderInfrastructureMermaid_DeterministicOutput(t *testing.T) {
	g := buildInfraTestGraph()
	matches := []HookMatch{
		{
			NodeID:    "my-app/database/main",
			NodeType:  "database",
			NodeName:  "main",
			Component: "my-app",
			Modules:   []string{"docker-postgres"},
		},
		{
			NodeID:    "my-app/deployment/api",
			NodeType:  "deployment",
			NodeName:  "api",
			Component: "my-app",
			Modules:   []string{"docker-deployment"},
		},
	}

	var results []string
	for i := 0; i < 5; i++ {
		result, err := RenderInfrastructureMermaid(g, matches, InfrastructureOptions{})
		require.NoError(t, err)
		results = append(results, result)
	}

	for i := 1; i < len(results); i++ {
		assert.Equal(t, results[0], results[i], "output should be deterministic")
	}
}

func TestBuildHookMatchSummary(t *testing.T) {
	matches := []HookMatch{
		{
			NodeID:    "my-app/database/main",
			NodeType:  "database",
			NodeName:  "main",
			Component: "my-app",
			HookIndex: 0,
			Modules:   []string{"docker-postgres"},
		},
		{
			NodeID:    "my-app/database/cache",
			NodeType:  "database",
			NodeName:  "cache",
			Component: "my-app",
			HookIndex: 1,
			IsError:   true,
			Error:     "Redis not supported",
		},
	}

	summary := BuildHookMatchSummary(matches)
	require.Len(t, summary, 2)

	// First entry should have modules
	assert.Equal(t, "my-app/database/main", summary[0]["nodeId"])
	assert.Equal(t, []string{"docker-postgres"}, summary[0]["modules"])

	// Second entry should have error
	assert.Equal(t, "my-app/database/cache", summary[1]["nodeId"])
	assert.Equal(t, true, summary[1]["isError"])
	assert.Equal(t, "Redis not supported", summary[1]["error"])
}

func TestInfraModuleKey(t *testing.T) {
	node := graph.NewNode(graph.NodeTypeDatabase, "my-app", "main")
	key := infraModuleKey(node, "docker-postgres")
	assert.Equal(t, "my-app/database/main/docker-postgres", key)
}

func TestInfraSanitizeID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"res_my-app/database/main", "res_my_app_database_main"},
		{"mod_my.app/db:16", "mod_my_app_db_16"},
	}

	for _, tt := range tests {
		result := infraSanitizeID(tt.input)
		assert.Equal(t, tt.expected, result)
	}
}

// helpers
func splitLines(s string) []string {
	return strings.Split(s, "\n")
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
