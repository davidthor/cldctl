package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuilds_Validation(t *testing.T) {
	v := NewValidator()

	tests := []struct {
		name       string
		schema     *SchemaV1
		wantErrors int
	}{
		{
			name: "valid build with context",
			schema: &SchemaV1{
				Builds: map[string]BuildV1{
					"api": {Context: "."},
				},
			},
			wantErrors: 0,
		},
		{
			name: "valid build with all fields",
			schema: &SchemaV1{
				Builds: map[string]BuildV1{
					"api": {
						Context:    "./api",
						Dockerfile: "Dockerfile.prod",
						Target:     "production",
						Args:       map[string]string{"NODE_ENV": "production"},
					},
				},
			},
			wantErrors: 0,
		},
		{
			name: "build missing context",
			schema: &SchemaV1{
				Builds: map[string]BuildV1{
					"api": {},
				},
			},
			wantErrors: 1,
		},
		{
			name: "multiple builds with one missing context",
			schema: &SchemaV1{
				Builds: map[string]BuildV1{
					"api":    {Context: "."},
					"worker": {},
				},
			},
			wantErrors: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := v.Validate(tt.schema)
			assert.Len(t, errs, tt.wantErrors)
		})
	}
}

func TestBuilds_ParseAndTransform(t *testing.T) {
	parser := NewParser()
	transformer := NewTransformer()

	yaml := []byte(`
builds:
  api:
    context: .
    dockerfile: Dockerfile.prod
    target: production
    args:
      NODE_ENV: production
deployments:
  api:
    image: "${{ builds.api.image }}"
    command: ["npm", "start"]
services:
  api:
    deployment: api
    port: 3000
`)

	schema, err := parser.ParseBytes(yaml)
	require.NoError(t, err)

	assert.Len(t, schema.Builds, 1)
	assert.Equal(t, ".", schema.Builds["api"].Context)
	assert.Equal(t, "Dockerfile.prod", schema.Builds["api"].Dockerfile)
	assert.Equal(t, "production", schema.Builds["api"].Target)
	assert.Equal(t, "production", schema.Builds["api"].Args["NODE_ENV"])

	ic, err := transformer.Transform(schema)
	require.NoError(t, err)

	assert.Len(t, ic.Builds, 1)
	assert.Equal(t, "api", ic.Builds[0].Name)
	assert.Equal(t, ".", ic.Builds[0].Context)
	assert.Equal(t, "Dockerfile.prod", ic.Builds[0].Dockerfile)
	assert.Equal(t, "production", ic.Builds[0].Target)

	assert.Len(t, ic.Deployments, 1)
	assert.Equal(t, "${{ builds.api.image }}", ic.Deployments[0].Image)
	assert.Equal(t, []string{"npm", "start"}, ic.Deployments[0].Command)
}

func TestDeployment_WithoutImageOrBuild(t *testing.T) {
	parser := NewParser()
	transformer := NewTransformer()
	validator := NewValidator()

	yaml := []byte(`
deployments:
  api:
    command: ["npm", "run", "dev"]
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
    workingDirectory: ./backend
`)

	schema, err := parser.ParseBytes(yaml)
	require.NoError(t, err)

	errs := validator.Validate(schema)
	assert.Len(t, errs, 0, "deployment without image should be valid")

	ic, err := transformer.Transform(schema)
	require.NoError(t, err)

	assert.Len(t, ic.Deployments, 1)
	assert.Equal(t, "", ic.Deployments[0].Image)
	assert.Equal(t, "./backend", ic.Deployments[0].WorkingDirectory)
	assert.Equal(t, []string{"npm", "run", "dev"}, ic.Deployments[0].Command)
}

func TestBuilds_DefaultDockerfile(t *testing.T) {
	parser := NewParser()
	transformer := NewTransformer()

	yaml := []byte(`
builds:
  api:
    context: .
`)

	schema, err := parser.ParseBytes(yaml)
	require.NoError(t, err)

	ic, err := transformer.Transform(schema)
	require.NoError(t, err)

	assert.Len(t, ic.Builds, 1)
	assert.Equal(t, "Dockerfile", ic.Builds[0].Dockerfile, "should default to 'Dockerfile'")
}
