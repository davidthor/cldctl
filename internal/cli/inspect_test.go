package cli

import (
	"testing"

	"github.com/davidthor/cldctl/pkg/graph"
	"github.com/davidthor/cldctl/pkg/resolver"
	"github.com/davidthor/cldctl/pkg/state/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInspectCmd(t *testing.T) {
	cmd := newInspectCmd()

	assert.Contains(t, cmd.Use, "inspect")
	assert.Contains(t, cmd.Short, "Inspect")

	// Should have RunE for state inspection
	assert.NotNil(t, cmd.RunE)

	// Should have component subcommand
	subcommands := cmd.Commands()
	assert.Len(t, subcommands, 1)
	assert.Equal(t, "component [path|image]", subcommands[0].Use)

	// Should have state-related flags
	assert.NotNil(t, cmd.Flags().Lookup("datacenter"))
	assert.NotNil(t, cmd.Flags().Lookup("output"))
	assert.NotNil(t, cmd.Flags().Lookup("backend"))
	assert.NotNil(t, cmd.Flags().Lookup("backend-config"))

	// Check shorthands
	assert.NotNil(t, cmd.Flags().ShorthandLookup("d"))
	assert.NotNil(t, cmd.Flags().ShorthandLookup("o"))
}

func TestInspectComponentCmd_Flags(t *testing.T) {
	cmd := newInspectComponentCmd()

	// Check expand flag
	expandFlag := cmd.Flags().Lookup("expand")
	assert.NotNil(t, expandFlag)
	assert.Equal(t, "false", expandFlag.DefValue)

	// Check file flag
	fileFlag := cmd.Flags().Lookup("file")
	assert.NotNil(t, fileFlag)
	assert.Equal(t, "f", fileFlag.Shorthand)
}

func TestFindResource(t *testing.T) {
	resources := map[string]*types.ResourceState{
		"database.main": {
			Name: "main",
			Type: "database",
		},
		"deployment.api": {
			Name: "api",
			Type: "deployment",
		},
		"service.api": {
			Name: "api",
			Type: "service",
		},
		"route.main": {
			Name: "main",
			Type: "route",
		},
	}

	tests := []struct {
		name         string
		query        string
		resourceType string
		wantName     string
		wantType     string
		wantErr      bool
		errContains  string
	}{
		{
			name:     "exact map key match",
			query:    "database.main",
			wantName: "main",
			wantType: "database",
		},
		{
			name:         "type-qualified match",
			query:        "api",
			resourceType: "deployment",
			wantName:     "api",
			wantType:     "deployment",
		},
		{
			name:         "type-qualified match - service",
			query:        "api",
			resourceType: "service",
			wantName:     "api",
			wantType:     "service",
		},
		{
			name:        "ambiguous name match",
			query:       "api",
			wantErr:     true,
			errContains: "ambiguous",
		},
		{
			name:        "not found",
			query:       "nonexistent",
			wantErr:     true,
			errContains: "not found",
		},
		{
			name:         "type-qualified not found",
			query:        "nonexistent",
			resourceType: "deployment",
			wantErr:      true,
			errContains:  "not found",
		},
		{
			name:     "unique name match",
			query:    "route.main",
			wantName: "main",
			wantType: "route",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := findResource(resources, tt.query, tt.resourceType)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantName, res.Name)
				assert.Equal(t, tt.wantType, res.Type)
			}
		})
	}
}

func TestFindResource_EmptyResources(t *testing.T) {
	_, err := findResource(map[string]*types.ResourceState{}, "api", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no resources")
}

func TestExtractEnvVars(t *testing.T) {
	tests := []struct {
		name   string
		inputs map[string]interface{}
		want   map[string]string
	}{
		{
			name:   "no environment key",
			inputs: map[string]interface{}{"image": "myimage:latest"},
			want:   map[string]string{},
		},
		{
			name: "map[string]interface{} env vars",
			inputs: map[string]interface{}{
				"environment": map[string]interface{}{
					"DATABASE_URL": "postgres://localhost/db",
					"PORT":         "8080",
				},
			},
			want: map[string]string{
				"DATABASE_URL": "postgres://localhost/db",
				"PORT":         "8080",
			},
		},
		{
			name: "map[string]string env vars",
			inputs: map[string]interface{}{
				"environment": map[string]string{
					"NODE_ENV": "production",
				},
			},
			want: map[string]string{
				"NODE_ENV": "production",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractEnvVars(tt.inputs)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractNonEnvInputs(t *testing.T) {
	inputs := map[string]interface{}{
		"image":       "myimage:latest",
		"cpu":         "512m",
		"environment": map[string]interface{}{"PORT": "8080"},
		"iac_state":   []byte("binary data"),
	}

	got := extractNonEnvInputs(inputs)
	assert.Contains(t, got, "image")
	assert.Contains(t, got, "cpu")
	assert.NotContains(t, got, "environment")
	assert.NotContains(t, got, "iac_state")
}

func TestFormatInputValue(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  string
	}{
		{
			name:  "string",
			input: "hello",
			want:  "hello",
		},
		{
			name:  "array",
			input: []interface{}{"node", "server.js"},
			want:  "[node, server.js]",
		},
		{
			name:  "number",
			input: 42,
			want:  "42",
		},
		{
			name:  "map",
			input: map[string]interface{}{"key": "value"},
			want:  `{"key":"value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatInputValue(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResourceSummary(t *testing.T) {
	tests := []struct {
		name string
		res  *types.ResourceState
		want string
	}{
		{
			name: "route with url",
			res: &types.ResourceState{
				Type:    "route",
				Outputs: map[string]interface{}{"url": "https://example.com"},
			},
			want: "https://example.com",
		},
		{
			name: "service with host and port",
			res: &types.ResourceState{
				Type:    "service",
				Outputs: map[string]interface{}{"host": "api.internal", "port": 8080},
			},
			want: "api.internal:8080",
		},
		{
			name: "resource with no outputs",
			res: &types.ResourceState{
				Type: "deployment",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resourceSummary(tt.res)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractComponentName(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		resolved resolver.ResolvedComponent
		want     string
	}{
		{
			name: "OCI reference with tag",
			ref:  "ghcr.io/myorg/myapp:v1.0.0",
			resolved: resolver.ResolvedComponent{
				Type: resolver.ReferenceTypeOCI,
			},
			want: "myapp",
		},
		{
			name: "OCI reference without tag",
			ref:  "ghcr.io/myorg/myapp",
			resolved: resolver.ResolvedComponent{
				Type: resolver.ReferenceTypeOCI,
			},
			want: "myapp",
		},
		{
			name: "local directory path",
			ref:  "./my-component",
			resolved: resolver.ResolvedComponent{
				Type: resolver.ReferenceTypeLocal,
				Path: "/Users/test/my-component/cloud.component.yml",
			},
			want: "my-component",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractComponentName(tt.ref, tt.resolved)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatNodeID(t *testing.T) {
	tests := []struct {
		name      string
		nodeType  graph.NodeType
		component string
		nodeName  string
		want      string
	}{
		{
			name:      "database node",
			nodeType:  graph.NodeTypeDatabase,
			component: "myapp",
			nodeName:  "main",
			want:      "[DB] myapp/main",
		},
		{
			name:      "deployment node",
			nodeType:  graph.NodeTypeDeployment,
			component: "myapp",
			nodeName:  "api",
			want:      "[DP] myapp/api",
		},
		{
			name:      "function node",
			nodeType:  graph.NodeTypeFunction,
			component: "myapp",
			nodeName:  "web",
			want:      "[FN] myapp/web",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := graph.NewNode(tt.nodeType, tt.component, tt.nodeName)
			got := formatNodeID(node)
			assert.Equal(t, tt.want, got)
		})
	}
}
