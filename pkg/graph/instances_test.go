package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInstanceNode(t *testing.T) {
	node := NewInstanceNode(NodeTypeDeployment, "my-app", "canary", 10, "api")

	assert.Equal(t, "my-app/canary/deployment/api", node.ID)
	assert.Equal(t, NodeTypeDeployment, node.Type)
	assert.Equal(t, "my-app", node.Component)
	assert.Equal(t, "api", node.Name)
	assert.NotNil(t, node.Instance)
	assert.Equal(t, "canary", node.Instance.Name)
	assert.Equal(t, 10, node.Instance.Weight)
	assert.Equal(t, NodeStatePending, node.State)
}

func TestIsPerInstanceType(t *testing.T) {
	tests := []struct {
		nodeType NodeType
		expected bool
	}{
		{NodeTypeDeployment, true},
		{NodeTypeFunction, true},
		{NodeTypeService, true},
		{NodeTypeCronjob, true},
		{NodeTypeDockerBuild, true},
		{NodeTypePort, true},
		{NodeTypeDatabase, false},
		{NodeTypeBucket, false},
		{NodeTypeEncryptionKey, false},
		{NodeTypeSMTP, false},
		{NodeTypeRoute, false},
		{NodeTypeTask, false},
		{NodeTypeObservability, false},
		{NodeTypeSecret, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.nodeType), func(t *testing.T) {
			assert.Equal(t, tt.expected, IsPerInstanceType(tt.nodeType))
			assert.Equal(t, !tt.expected, IsSharedType(tt.nodeType))
		})
	}
}

func TestNewInstanceNode_IDFormat(t *testing.T) {
	tests := []struct {
		name       string
		nodeType   NodeType
		component  string
		instance   string
		weight     int
		nodeName   string
		expectedID string
	}{
		{
			name:       "deployment canary",
			nodeType:   NodeTypeDeployment,
			component:  "my-app",
			instance:   "canary",
			weight:     10,
			nodeName:   "api",
			expectedID: "my-app/canary/deployment/api",
		},
		{
			name:       "service stable",
			nodeType:   NodeTypeService,
			component:  "my-app",
			instance:   "stable",
			weight:     90,
			nodeName:   "web",
			expectedID: "my-app/stable/service/web",
		},
		{
			name:       "port with instance",
			nodeType:   NodeTypePort,
			component:  "my-app",
			instance:   "green",
			weight:     0,
			nodeName:   "http",
			expectedID: "my-app/green/port/http",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := NewInstanceNode(tt.nodeType, tt.component, tt.instance, tt.weight, tt.nodeName)
			assert.Equal(t, tt.expectedID, node.ID)
			assert.NotNil(t, node.Instance)
			assert.Equal(t, tt.instance, node.Instance.Name)
			assert.Equal(t, tt.weight, node.Instance.Weight)
		})
	}
}

func TestNodeInstance_SharedNodesHaveInstancesList(t *testing.T) {
	g := NewGraph("staging", "local")

	// Create a shared route node with instances metadata
	routeNode := NewNode(NodeTypeRoute, "my-app", "main")
	routeNode.Instances = []NodeInstance{
		{Name: "canary", Weight: 10},
		{Name: "stable", Weight: 90},
	}

	err := g.AddNode(routeNode)
	require.NoError(t, err)

	// Verify the node was added correctly
	fetched := g.GetNode("my-app/route/main")
	require.NotNil(t, fetched)
	assert.Nil(t, fetched.Instance)
	assert.Len(t, fetched.Instances, 2)
	assert.Equal(t, "canary", fetched.Instances[0].Name)
	assert.Equal(t, 10, fetched.Instances[0].Weight)
	assert.Equal(t, "stable", fetched.Instances[1].Name)
	assert.Equal(t, 90, fetched.Instances[1].Weight)
}

func TestInstanceAndSharedNodesCoexist(t *testing.T) {
	g := NewGraph("prod", "my-dc")

	// Shared database
	db := NewNode(NodeTypeDatabase, "my-app", "main")
	db.SetInput("type", "postgres:^16")
	require.NoError(t, g.AddNode(db))

	// Per-instance deployments
	deployCanary := NewInstanceNode(NodeTypeDeployment, "my-app", "canary", 10, "api")
	deployCanary.SetInput("image", "my-app:v2")
	require.NoError(t, g.AddNode(deployCanary))

	deployStable := NewInstanceNode(NodeTypeDeployment, "my-app", "stable", 90, "api")
	deployStable.SetInput("image", "my-app:v1")
	require.NoError(t, g.AddNode(deployStable))

	// Wire dependencies: both deployments depend on shared database
	deployCanary.AddDependency(db.ID)
	db.AddDependent(deployCanary.ID)
	deployStable.AddDependency(db.ID)
	db.AddDependent(deployStable.ID)

	// Verify graph structure
	assert.Len(t, g.Nodes, 3)

	// Verify topological sort works
	sorted, err := g.TopologicalSort()
	require.NoError(t, err)
	assert.Len(t, sorted, 3)

	// Database should come first (no dependencies)
	assert.Equal(t, db.ID, sorted[0].ID)
}
