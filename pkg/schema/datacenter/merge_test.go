package datacenter

import (
	"testing"

	"github.com/davidthor/cldctl/pkg/schema/datacenter/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeDatacenters_Variables(t *testing.T) {
	tests := []struct {
		name     string
		child    *internal.InternalDatacenter
		parent   *internal.InternalDatacenter
		expected []string // variable names in order
	}{
		{
			name: "child only",
			child: &internal.InternalDatacenter{
				SourceVersion: "v1",
				Variables: []internal.InternalVariable{
					{Name: "region", Type: "string"},
				},
			},
			parent:   &internal.InternalDatacenter{},
			expected: []string{"region"},
		},
		{
			name:  "parent only",
			child: &internal.InternalDatacenter{SourceVersion: "v1"},
			parent: &internal.InternalDatacenter{
				Variables: []internal.InternalVariable{
					{Name: "region", Type: "string"},
				},
			},
			expected: []string{"region"},
		},
		{
			name: "child wins on collision",
			child: &internal.InternalDatacenter{
				SourceVersion: "v1",
				Variables: []internal.InternalVariable{
					{Name: "region", Type: "string", Default: "us-west-2"},
				},
			},
			parent: &internal.InternalDatacenter{
				Variables: []internal.InternalVariable{
					{Name: "region", Type: "string", Default: "us-east-1"},
					{Name: "cluster", Type: "string"},
				},
			},
			expected: []string{"region", "cluster"},
		},
		{
			name: "union of disjoint variables",
			child: &internal.InternalDatacenter{
				SourceVersion: "v1",
				Variables: []internal.InternalVariable{
					{Name: "alpha"},
				},
			},
			parent: &internal.InternalDatacenter{
				Variables: []internal.InternalVariable{
					{Name: "beta"},
				},
			},
			expected: []string{"alpha", "beta"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := MergeDatacenters(tt.child, tt.parent)
			var names []string
			for _, v := range merged.Variables {
				names = append(names, v.Name)
			}
			assert.Equal(t, tt.expected, names)
		})
	}
}

func TestMergeDatacenters_ChildVariableValueWins(t *testing.T) {
	child := &internal.InternalDatacenter{
		SourceVersion: "v1",
		Variables: []internal.InternalVariable{
			{Name: "region", Type: "string", Default: "us-west-2"},
		},
	}
	parent := &internal.InternalDatacenter{
		Variables: []internal.InternalVariable{
			{Name: "region", Type: "string", Default: "us-east-1"},
		},
	}

	merged := MergeDatacenters(child, parent)
	require.Len(t, merged.Variables, 1)
	assert.Equal(t, "us-west-2", merged.Variables[0].Default)
}

func TestMergeDatacenters_Modules(t *testing.T) {
	child := &internal.InternalDatacenter{
		SourceVersion: "v1",
		Modules: []internal.InternalModule{
			{Name: "vpc", Plugin: "pulumi", Build: "./modules/vpc-v2"},
		},
	}
	parent := &internal.InternalDatacenter{
		Modules: []internal.InternalModule{
			{Name: "vpc", Plugin: "pulumi", Build: "./modules/vpc"},
			{Name: "dns", Plugin: "pulumi", Build: "./modules/dns"},
		},
	}

	merged := MergeDatacenters(child, parent)
	require.Len(t, merged.Modules, 2)
	assert.Equal(t, "vpc", merged.Modules[0].Name)
	assert.Equal(t, "./modules/vpc-v2", merged.Modules[0].Build) // child wins
	assert.Equal(t, "dns", merged.Modules[1].Name)
}

func TestMergeDatacenters_Components(t *testing.T) {
	child := &internal.InternalDatacenter{
		SourceVersion: "v1",
		Components: []internal.InternalDatacenterComponent{
			{Name: "myorg/stripe", Source: "v2"},
		},
	}
	parent := &internal.InternalDatacenter{
		Components: []internal.InternalDatacenterComponent{
			{Name: "myorg/stripe", Source: "v1"},
			{Name: "myorg/clerk", Source: "latest"},
		},
	}

	merged := MergeDatacenters(child, parent)
	require.Len(t, merged.Components, 2)
	assert.Equal(t, "myorg/stripe", merged.Components[0].Name)
	assert.Equal(t, "v2", merged.Components[0].Source) // child wins
	assert.Equal(t, "myorg/clerk", merged.Components[1].Name)
}

func TestMergeDatacenters_Hooks_PrependChild(t *testing.T) {
	child := &internal.InternalDatacenter{
		SourceVersion: "v1",
		Environment: internal.InternalEnvironment{
			Hooks: internal.InternalHooks{
				Database: []internal.InternalHook{
					{When: "postgres-override", Outputs: map[string]string{"url": "child-url"}},
				},
			},
		},
	}
	parent := &internal.InternalDatacenter{
		Environment: internal.InternalEnvironment{
			Hooks: internal.InternalHooks{
				Database: []internal.InternalHook{
					{When: "postgres", Outputs: map[string]string{"url": "parent-url"}},
					{When: "redis", Outputs: map[string]string{"url": "redis-url"}},
				},
			},
		},
	}

	merged := MergeDatacenters(child, parent)
	hooks := merged.Environment.Hooks.Database
	require.Len(t, hooks, 3)
	assert.Equal(t, "postgres-override", hooks[0].When) // child first
	assert.Equal(t, "postgres", hooks[1].When)           // parent second
	assert.Equal(t, "redis", hooks[2].When)              // parent third
}

func TestMergeDatacenters_Hooks_ChildCatchAllShadowsParent(t *testing.T) {
	child := &internal.InternalDatacenter{
		SourceVersion: "v1",
		Environment: internal.InternalEnvironment{
			Hooks: internal.InternalHooks{
				Database: []internal.InternalHook{
					{When: "postgres", Outputs: map[string]string{"url": "child-pg"}},
					{When: "", Error: "Child catch-all: unsupported"},
				},
			},
		},
	}
	parent := &internal.InternalDatacenter{
		Environment: internal.InternalEnvironment{
			Hooks: internal.InternalHooks{
				Database: []internal.InternalHook{
					{When: "redis", Outputs: map[string]string{"url": "parent-redis"}},
					{When: "", Error: "Parent catch-all: unsupported"},
				},
			},
		},
	}

	merged := MergeDatacenters(child, parent)
	hooks := merged.Environment.Hooks.Database
	require.Len(t, hooks, 3) // child postgres, child catch-all, parent redis (parent catch-all dropped)
	assert.Equal(t, "postgres", hooks[0].When)
	assert.Equal(t, "", hooks[1].When)
	assert.Equal(t, "Child catch-all: unsupported", hooks[1].Error)
	assert.Equal(t, "redis", hooks[2].When)
}

func TestMergeDatacenters_Hooks_OnlyParentCatchAll(t *testing.T) {
	child := &internal.InternalDatacenter{
		SourceVersion: "v1",
		Environment: internal.InternalEnvironment{
			Hooks: internal.InternalHooks{
				Database: []internal.InternalHook{
					{When: "postgres", Outputs: map[string]string{"url": "child-pg"}},
				},
			},
		},
	}
	parent := &internal.InternalDatacenter{
		Environment: internal.InternalEnvironment{
			Hooks: internal.InternalHooks{
				Database: []internal.InternalHook{
					{When: "redis", Outputs: map[string]string{"url": "parent-redis"}},
					{When: "", Error: "Parent catch-all"},
				},
			},
		},
	}

	merged := MergeDatacenters(child, parent)
	hooks := merged.Environment.Hooks.Database
	require.Len(t, hooks, 3) // child postgres, parent redis, parent catch-all (kept)
	assert.Equal(t, "postgres", hooks[0].When)
	assert.Equal(t, "redis", hooks[1].When)
	assert.Equal(t, "", hooks[2].When)
	assert.Equal(t, "Parent catch-all", hooks[2].Error)
}

func TestMergeDatacenters_Hooks_EmptyChild(t *testing.T) {
	child := &internal.InternalDatacenter{
		SourceVersion: "v1",
	}
	parent := &internal.InternalDatacenter{
		Environment: internal.InternalEnvironment{
			Hooks: internal.InternalHooks{
				Deployment: []internal.InternalHook{
					{When: "", Outputs: map[string]string{"id": "deploy-id"}},
				},
			},
		},
	}

	merged := MergeDatacenters(child, parent)
	hooks := merged.Environment.Hooks.Deployment
	require.Len(t, hooks, 1)
	assert.Equal(t, "", hooks[0].When)
}

func TestMergeDatacenters_Hooks_EmptyParent(t *testing.T) {
	child := &internal.InternalDatacenter{
		SourceVersion: "v1",
		Environment: internal.InternalEnvironment{
			Hooks: internal.InternalHooks{
				Service: []internal.InternalHook{
					{When: "", Outputs: map[string]string{"url": "svc-url"}},
				},
			},
		},
	}
	parent := &internal.InternalDatacenter{}

	merged := MergeDatacenters(child, parent)
	hooks := merged.Environment.Hooks.Service
	require.Len(t, hooks, 1)
}

func TestMergeDatacenters_ExtendsCleared(t *testing.T) {
	child := &internal.InternalDatacenter{
		SourceVersion: "v1",
		Extends:       &internal.InternalExtends{Image: "ghcr.io/org/dc:v1"},
	}
	parent := &internal.InternalDatacenter{}

	merged := MergeDatacenters(child, parent)
	assert.Nil(t, merged.Extends, "merged datacenter should have Extends = nil")
}

func TestMergeDatacenters_SourceInfoFromChild(t *testing.T) {
	child := &internal.InternalDatacenter{
		SourceVersion: "v1",
		SourcePath:    "/child/datacenter.dc",
	}
	parent := &internal.InternalDatacenter{
		SourceVersion: "v1",
		SourcePath:    "/parent/datacenter.dc",
	}

	merged := MergeDatacenters(child, parent)
	assert.Equal(t, "/child/datacenter.dc", merged.SourcePath)
	assert.Equal(t, "v1", merged.SourceVersion)
}

func TestMergeDatacenters_EnvironmentModules(t *testing.T) {
	child := &internal.InternalDatacenter{
		SourceVersion: "v1",
		Environment: internal.InternalEnvironment{
			Modules: []internal.InternalModule{
				{Name: "namespace", Plugin: "native", Build: "./modules/ns-v2"},
			},
		},
	}
	parent := &internal.InternalDatacenter{
		Environment: internal.InternalEnvironment{
			Modules: []internal.InternalModule{
				{Name: "namespace", Plugin: "native", Build: "./modules/ns"},
				{Name: "monitoring", Plugin: "pulumi", Build: "./modules/mon"},
			},
		},
	}

	merged := MergeDatacenters(child, parent)
	mods := merged.Environment.Modules
	require.Len(t, mods, 2)
	assert.Equal(t, "namespace", mods[0].Name)
	assert.Equal(t, "./modules/ns-v2", mods[0].Build) // child wins
	assert.Equal(t, "monitoring", mods[1].Name)
}

func TestMergeDatacenters_AllHookTypes(t *testing.T) {
	// Verify that all hook types are properly merged
	childHook := internal.InternalHook{When: "child", Outputs: map[string]string{"x": "y"}}
	parentHook := internal.InternalHook{When: "parent", Outputs: map[string]string{"a": "b"}}

	child := &internal.InternalDatacenter{
		SourceVersion: "v1",
		Environment: internal.InternalEnvironment{
			Hooks: internal.InternalHooks{
				Database:      []internal.InternalHook{childHook},
				Task:          []internal.InternalHook{childHook},
				Bucket:        []internal.InternalHook{childHook},
				EncryptionKey: []internal.InternalHook{childHook},
				SMTP:          []internal.InternalHook{childHook},
				DatabaseUser:  []internal.InternalHook{childHook},
				Deployment:    []internal.InternalHook{childHook},
				Function:      []internal.InternalHook{childHook},
				Service:       []internal.InternalHook{childHook},
				Route:         []internal.InternalHook{childHook},
				Cronjob:       []internal.InternalHook{childHook},
				Secret:        []internal.InternalHook{childHook},
				DockerBuild:   []internal.InternalHook{childHook},
				Observability: []internal.InternalHook{childHook},
				Port:          []internal.InternalHook{childHook},
			},
		},
	}
	parent := &internal.InternalDatacenter{
		Environment: internal.InternalEnvironment{
			Hooks: internal.InternalHooks{
				Database:      []internal.InternalHook{parentHook},
				Task:          []internal.InternalHook{parentHook},
				Bucket:        []internal.InternalHook{parentHook},
				EncryptionKey: []internal.InternalHook{parentHook},
				SMTP:          []internal.InternalHook{parentHook},
				DatabaseUser:  []internal.InternalHook{parentHook},
				Deployment:    []internal.InternalHook{parentHook},
				Function:      []internal.InternalHook{parentHook},
				Service:       []internal.InternalHook{parentHook},
				Route:         []internal.InternalHook{parentHook},
				Cronjob:       []internal.InternalHook{parentHook},
				Secret:        []internal.InternalHook{parentHook},
				DockerBuild:   []internal.InternalHook{parentHook},
				Observability: []internal.InternalHook{parentHook},
				Port:          []internal.InternalHook{parentHook},
			},
		},
	}

	merged := MergeDatacenters(child, parent)
	h := merged.Environment.Hooks

	// Each hook type should have 2 hooks: child first, parent second
	assert.Len(t, h.Database, 2)
	assert.Len(t, h.Task, 2)
	assert.Len(t, h.Bucket, 2)
	assert.Len(t, h.EncryptionKey, 2)
	assert.Len(t, h.SMTP, 2)
	assert.Len(t, h.DatabaseUser, 2)
	assert.Len(t, h.Deployment, 2)
	assert.Len(t, h.Function, 2)
	assert.Len(t, h.Service, 2)
	assert.Len(t, h.Route, 2)
	assert.Len(t, h.Cronjob, 2)
	assert.Len(t, h.Secret, 2)
	assert.Len(t, h.DockerBuild, 2)
	assert.Len(t, h.Observability, 2)
	assert.Len(t, h.Port, 2)

	// Verify child is first for all types
	assert.Equal(t, "child", h.Database[0].When)
	assert.Equal(t, "parent", h.Database[1].When)
}
