package component

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeepMerge_ScalarOverride(t *testing.T) {
	base := map[string]interface{}{
		"name": "base-app",
		"port": 3000,
	}
	override := map[string]interface{}{
		"name": "override-app",
	}

	result := deepMerge(base, override)

	assert.Equal(t, "override-app", result["name"])
	assert.Equal(t, 3000, result["port"]) // Inherited from base
}

func TestDeepMerge_NestedMapMerge(t *testing.T) {
	base := map[string]interface{}{
		"deployments": map[string]interface{}{
			"api": map[string]interface{}{
				"command": []string{"npm", "run", "dev"},
				"environment": map[string]interface{}{
					"DATABASE_URL": "${{ databases.main.url }}",
					"NODE_ENV":     "development",
				},
			},
		},
	}
	override := map[string]interface{}{
		"deployments": map[string]interface{}{
			"api": map[string]interface{}{
				"image":   "${{ builds.api.image }}",
				"command": []string{"npm", "start"},
			},
		},
	}

	result := deepMerge(base, override)

	deployments := result["deployments"].(map[string]interface{})
	api := deployments["api"].(map[string]interface{})

	assert.Equal(t, "${{ builds.api.image }}", api["image"])
	assert.Equal(t, []string{"npm", "start"}, api["command"]) // Replaced
	env := api["environment"].(map[string]interface{})
	assert.Equal(t, "${{ databases.main.url }}", env["DATABASE_URL"]) // Inherited
	assert.Equal(t, "development", env["NODE_ENV"])                   // Inherited
}

func TestDeepMerge_NullDeletion(t *testing.T) {
	base := map[string]interface{}{
		"deployments": map[string]interface{}{
			"api": map[string]interface{}{
				"command": []string{"npm", "run", "dev"},
				"volumes": []interface{}{"/data:/data"},
			},
		},
	}
	override := map[string]interface{}{
		"deployments": map[string]interface{}{
			"api": map[string]interface{}{
				"volumes": nil, // Explicit null deletes volumes
			},
		},
	}

	result := deepMerge(base, override)

	deployments := result["deployments"].(map[string]interface{})
	api := deployments["api"].(map[string]interface{})

	assert.Equal(t, []string{"npm", "run", "dev"}, api["command"]) // Inherited
	_, hasVolumes := api["volumes"]
	assert.False(t, hasVolumes, "volumes should be deleted by null override")
}

func TestDeepMerge_ArrayReplacement(t *testing.T) {
	base := map[string]interface{}{
		"command": []interface{}{"npm", "run", "dev"},
	}
	override := map[string]interface{}{
		"command": []interface{}{"npm", "start"},
	}

	result := deepMerge(base, override)

	assert.Equal(t, []interface{}{"npm", "start"}, result["command"])
}

func TestDeepMerge_EmptyOverride(t *testing.T) {
	base := map[string]interface{}{
		"name": "app",
		"port": 3000,
	}
	override := map[string]interface{}{}

	result := deepMerge(base, override)

	assert.Equal(t, "app", result["name"])
	assert.Equal(t, 3000, result["port"])
}

func TestDeepMerge_EmptyBase(t *testing.T) {
	base := map[string]interface{}{}
	override := map[string]interface{}{
		"name": "app",
	}

	result := deepMerge(base, override)

	assert.Equal(t, "app", result["name"])
}

func TestDeepMerge_NewTopLevelKeys(t *testing.T) {
	base := map[string]interface{}{
		"databases": map[string]interface{}{
			"main": map[string]interface{}{"type": "postgres:^16"},
		},
	}
	override := map[string]interface{}{
		"builds": map[string]interface{}{
			"api": map[string]interface{}{"context": "."},
		},
	}

	result := deepMerge(base, override)

	assert.NotNil(t, result["databases"])
	assert.NotNil(t, result["builds"])
}

func TestDeepMerge_DoesNotMutateInputs(t *testing.T) {
	base := map[string]interface{}{
		"name": "base",
	}
	override := map[string]interface{}{
		"name": "override",
	}

	_ = deepMerge(base, override)

	assert.Equal(t, "base", base["name"], "base should not be mutated")
	assert.Equal(t, "override", override["name"], "override should not be mutated")
}

// Test extends resolution with real files

func TestResolveExtends_Simple(t *testing.T) {
	dir := t.TempDir()

	// Write base file
	base := `databases:
  main:
    type: postgres:^16
deployments:
  api:
    command: ["npm", "run", "dev"]
    environment:
      DATABASE_URL: "${{ databases.main.url }}"
services:
  api:
    deployment: api
    port: 3000
`
	err := os.WriteFile(filepath.Join(dir, "cld.yml"), []byte(base), 0644)
	require.NoError(t, err)

	// Write extending file
	ext := `extends: ./cld.yml
builds:
  api:
    context: .
deployments:
  api:
    image: "${{ builds.api.image }}"
    command: ["npm", "start"]
`
	extPath := filepath.Join(dir, "cld.prod.yml")
	err = os.WriteFile(extPath, []byte(ext), 0644)
	require.NoError(t, err)

	// Load the extending file
	loader := NewLoader()
	comp, err := loader.Load(extPath)
	require.NoError(t, err)

	// Verify merged result
	assert.Len(t, comp.Databases(), 1, "should inherit databases from base")
	assert.Equal(t, "postgres", comp.Databases()[0].Type())

	assert.Len(t, comp.Builds(), 1, "should have builds from override")
	assert.Equal(t, "api", comp.Builds()[0].Name())
	assert.Equal(t, ".", comp.Builds()[0].Context())

	assert.Len(t, comp.Deployments(), 1)
	assert.Equal(t, "${{ builds.api.image }}", comp.Deployments()[0].Image())
	assert.Equal(t, []string{"npm", "start"}, comp.Deployments()[0].Command())

	// Environment should be inherited from base
	env := comp.Deployments()[0].Environment()
	assert.Equal(t, "${{ databases.main.url }}", env["DATABASE_URL"])

	assert.Len(t, comp.Services(), 1, "should inherit services from base")
}

func TestResolveExtends_ChainedExtends(t *testing.T) {
	dir := t.TempDir()

	// Write grandparent (base of base)
	grandparent := `databases:
  main:
    type: postgres:^16
deployments:
  api:
    command: ["echo", "hello"]
    environment:
      NODE_ENV: base
`
	err := os.WriteFile(filepath.Join(dir, "base.yml"), []byte(grandparent), 0644)
	require.NoError(t, err)

	// Write parent (extends grandparent)
	parent := `extends: ./base.yml
deployments:
  api:
    command: ["npm", "run", "dev"]
    environment:
      NODE_ENV: development
`
	err = os.WriteFile(filepath.Join(dir, "dev.yml"), []byte(parent), 0644)
	require.NoError(t, err)

	// Write child (extends parent)
	child := `extends: ./dev.yml
builds:
  api:
    context: .
deployments:
  api:
    image: "${{ builds.api.image }}"
    command: ["npm", "start"]
    environment:
      NODE_ENV: production
`
	childPath := filepath.Join(dir, "prod.yml")
	err = os.WriteFile(childPath, []byte(child), 0644)
	require.NoError(t, err)

	// Load the leaf
	loader := NewLoader()
	comp, err := loader.Load(childPath)
	require.NoError(t, err)

	assert.Len(t, comp.Databases(), 1, "should inherit databases from grandparent")
	assert.Len(t, comp.Builds(), 1, "should have builds from child")
	assert.Equal(t, "${{ builds.api.image }}", comp.Deployments()[0].Image())
	assert.Equal(t, []string{"npm", "start"}, comp.Deployments()[0].Command())
	assert.Equal(t, "production", comp.Deployments()[0].Environment()["NODE_ENV"])
}

func TestResolveExtends_CircularReference(t *testing.T) {
	dir := t.TempDir()

	// a.yml extends b.yml
	err := os.WriteFile(filepath.Join(dir, "a.yml"), []byte(`extends: ./b.yml
deployments:
  api:
    command: ["npm", "start"]
`), 0644)
	require.NoError(t, err)

	// b.yml extends a.yml (circular!)
	err = os.WriteFile(filepath.Join(dir, "b.yml"), []byte(`extends: ./a.yml
databases:
  main:
    type: postgres:^16
`), 0644)
	require.NoError(t, err)

	loader := NewLoader()
	_, err = loader.Load(filepath.Join(dir, "a.yml"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circular")
}

func TestResolveExtends_MissingBase(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(filepath.Join(dir, "app.yml"), []byte(`extends: ./nonexistent.yml
deployments:
  api:
    command: ["npm", "start"]
`), 0644)
	require.NoError(t, err)

	loader := NewLoader()
	_, err = loader.Load(filepath.Join(dir, "app.yml"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read base component")
}

func TestResolveExtends_NoExtends(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(filepath.Join(dir, "app.yml"), []byte(`deployments:
  api:
    command: ["npm", "start"]
services:
  api:
    deployment: api
    port: 3000
`), 0644)
	require.NoError(t, err)

	loader := NewLoader()
	comp, err := loader.Load(filepath.Join(dir, "app.yml"))
	require.NoError(t, err)

	assert.Len(t, comp.Deployments(), 1)
	assert.Equal(t, []string{"npm", "start"}, comp.Deployments()[0].Command())
}
