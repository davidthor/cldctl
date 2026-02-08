package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_AddAndGet(t *testing.T) {
	tempDir := t.TempDir()
	regPath := filepath.Join(tempDir, "registry.json")

	reg, err := NewRegistryWithPath(regPath)
	require.NoError(t, err)

	entry := ArtifactEntry{
		Reference:  "ghcr.io/org/app:v1.0.0",
		Repository: "ghcr.io/org/app",
		Tag:        "v1.0.0",
		Type:       TypeComponent,
		Digest:     "sha256:abc123",
		Size:       1024,
		CreatedAt:  time.Now(),
		CachePath:  "/tmp/cache/app",
	}

	err = reg.Add(entry)
	require.NoError(t, err)

	got, err := reg.Get("ghcr.io/org/app:v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, entry.Reference, got.Reference)
	assert.Equal(t, entry.Repository, got.Repository)
	assert.Equal(t, entry.Tag, got.Tag)
	assert.Equal(t, TypeComponent, got.Type)
}

func TestRegistry_AddUpdatesExisting(t *testing.T) {
	tempDir := t.TempDir()
	regPath := filepath.Join(tempDir, "registry.json")

	reg, err := NewRegistryWithPath(regPath)
	require.NoError(t, err)

	entry1 := ArtifactEntry{
		Reference:  "ghcr.io/org/app:v1.0.0",
		Repository: "ghcr.io/org/app",
		Tag:        "v1.0.0",
		Type:       TypeComponent,
		Size:       1024,
		CreatedAt:  time.Now(),
	}

	err = reg.Add(entry1)
	require.NoError(t, err)

	// Update with new entry
	entry2 := ArtifactEntry{
		Reference:  "ghcr.io/org/app:v1.0.0",
		Repository: "ghcr.io/org/app",
		Tag:        "v1.0.0",
		Type:       TypeComponent,
		Size:       2048,
		CreatedAt:  time.Now(),
	}

	err = reg.Add(entry2)
	require.NoError(t, err)

	// Should only have one entry
	entries, err := reg.List()
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, int64(2048), entries[0].Size)
}

func TestRegistry_Remove(t *testing.T) {
	tempDir := t.TempDir()
	regPath := filepath.Join(tempDir, "registry.json")

	reg, err := NewRegistryWithPath(regPath)
	require.NoError(t, err)

	entry := ArtifactEntry{
		Reference:  "ghcr.io/org/app:v1.0.0",
		Repository: "ghcr.io/org/app",
		Tag:        "v1.0.0",
		Type:       TypeComponent,
		CreatedAt:  time.Now(),
	}

	err = reg.Add(entry)
	require.NoError(t, err)

	err = reg.Remove("ghcr.io/org/app:v1.0.0")
	require.NoError(t, err)

	_, err = reg.Get("ghcr.io/org/app:v1.0.0")
	assert.Error(t, err)
}

func TestRegistry_List(t *testing.T) {
	tempDir := t.TempDir()
	regPath := filepath.Join(tempDir, "registry.json")

	reg, err := NewRegistryWithPath(regPath)
	require.NoError(t, err)

	// Add multiple entries with different times
	now := time.Now()
	entries := []ArtifactEntry{
		{
			Reference:  "ghcr.io/org/app:v1.0.0",
			Repository: "ghcr.io/org/app",
			Tag:        "v1.0.0",
			Type:       TypeComponent,
			CreatedAt:  now.Add(-2 * time.Hour),
		},
		{
			Reference:  "ghcr.io/org/app:v2.0.0",
			Repository: "ghcr.io/org/app",
			Tag:        "v2.0.0",
			Type:       TypeComponent,
			CreatedAt:  now.Add(-1 * time.Hour),
		},
		{
			Reference:  "docker.io/library/nginx:latest",
			Repository: "docker.io/library/nginx",
			Tag:        "latest",
			Type:       TypeComponent,
			CreatedAt:  now,
		},
	}

	for _, e := range entries {
		err = reg.Add(e)
		require.NoError(t, err)
	}

	list, err := reg.List()
	require.NoError(t, err)
	assert.Len(t, list, 3)

	// Should be sorted by created time (most recent first)
	assert.Equal(t, "docker.io/library/nginx:latest", list[0].Reference)
	assert.Equal(t, "ghcr.io/org/app:v2.0.0", list[1].Reference)
	assert.Equal(t, "ghcr.io/org/app:v1.0.0", list[2].Reference)
}

func TestRegistry_ListByType(t *testing.T) {
	tempDir := t.TempDir()
	regPath := filepath.Join(tempDir, "registry.json")

	reg, err := NewRegistryWithPath(regPath)
	require.NoError(t, err)

	now := time.Now()
	entries := []ArtifactEntry{
		{Reference: "myapp:v1", Repository: "myapp", Tag: "v1", Type: TypeComponent, CreatedAt: now},
		{Reference: "my-dc:latest", Repository: "my-dc", Tag: "latest", Type: TypeDatacenter, CreatedAt: now},
		{Reference: "otherapp:v2", Repository: "otherapp", Tag: "v2", Type: TypeComponent, CreatedAt: now},
	}
	for _, e := range entries {
		require.NoError(t, reg.Add(e))
	}

	components, err := reg.ListByType(TypeComponent)
	require.NoError(t, err)
	assert.Len(t, components, 2)

	datacenters, err := reg.ListByType(TypeDatacenter)
	require.NoError(t, err)
	assert.Len(t, datacenters, 1)
	assert.Equal(t, "my-dc:latest", datacenters[0].Reference)
}

func TestRegistry_Clear(t *testing.T) {
	tempDir := t.TempDir()
	regPath := filepath.Join(tempDir, "registry.json")

	reg, err := NewRegistryWithPath(regPath)
	require.NoError(t, err)

	entry := ArtifactEntry{
		Reference:  "ghcr.io/org/app:v1.0.0",
		Repository: "ghcr.io/org/app",
		Tag:        "v1.0.0",
		Type:       TypeComponent,
		CreatedAt:  time.Now(),
	}

	err = reg.Add(entry)
	require.NoError(t, err)

	err = reg.Clear()
	require.NoError(t, err)

	list, err := reg.List()
	require.NoError(t, err)
	assert.Len(t, list, 0)
}

func TestRegistry_GetNotFound(t *testing.T) {
	tempDir := t.TempDir()
	regPath := filepath.Join(tempDir, "registry.json")

	reg, err := NewRegistryWithPath(regPath)
	require.NoError(t, err)

	_, err = reg.Get("nonexistent:tag")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRegistry_PersistenceAcrossInstances(t *testing.T) {
	tempDir := t.TempDir()
	regPath := filepath.Join(tempDir, "registry.json")

	// First instance
	reg1, err := NewRegistryWithPath(regPath)
	require.NoError(t, err)

	entry := ArtifactEntry{
		Reference:  "ghcr.io/org/app:v1.0.0",
		Repository: "ghcr.io/org/app",
		Tag:        "v1.0.0",
		Type:       TypeComponent,
		CreatedAt:  time.Now(),
	}

	err = reg1.Add(entry)
	require.NoError(t, err)

	// Second instance should see the same data
	reg2, err := NewRegistryWithPath(regPath)
	require.NoError(t, err)

	got, err := reg2.Get("ghcr.io/org/app:v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, entry.Reference, got.Reference)
}

func TestRegistry_CreatesDirectory(t *testing.T) {
	tempDir := t.TempDir()
	regPath := filepath.Join(tempDir, "nested", "dir", "registry.json")

	reg, err := NewRegistryWithPath(regPath)
	require.NoError(t, err)

	entry := ArtifactEntry{
		Reference:  "test:v1",
		Repository: "test",
		Tag:        "v1",
		Type:       TypeComponent,
		CreatedAt:  time.Now(),
	}

	err = reg.Add(entry)
	require.NoError(t, err)

	// File should exist
	_, err = os.Stat(regPath)
	require.NoError(t, err)
}

func TestRegistry_MigrateLegacy(t *testing.T) {
	tempDir := t.TempDir()

	// Write a legacy components.json
	legacyPath := filepath.Join(tempDir, "components.json")
	legacy := map[string]interface{}{
		"version": "v1",
		"components": []map[string]interface{}{
			{
				"reference":  "ghcr.io/org/app:v1",
				"repository": "ghcr.io/org/app",
				"tag":        "v1",
				"source":     "built",
				"size":       1024,
				"createdAt":  time.Now().Format(time.RFC3339Nano),
				"cachePath":  "/tmp/cache",
			},
		},
	}
	data, _ := json.Marshal(legacy)
	require.NoError(t, os.WriteFile(legacyPath, data, 0644))

	// Create registry at the new path — should auto-migrate
	newPath := filepath.Join(tempDir, "artifacts.json")
	reg, err := NewRegistryWithPath(newPath)
	require.NoError(t, err)

	entries, err := reg.List()
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "ghcr.io/org/app:v1", entries[0].Reference)
	assert.Equal(t, TypeComponent, entries[0].Type)
}

func TestCacheKey(t *testing.T) {
	assert.Equal(t, "ghcr.io_org_app_v1", CacheKey("ghcr.io/org/app:v1"))
	assert.Equal(t, "local_latest", CacheKey("local:latest"))
	assert.Equal(t, "myapp_dev", CacheKey("myapp:dev"))
}

func TestParseReference(t *testing.T) {
	tests := []struct {
		input          string
		wantRepository string
		wantTag        string
	}{
		{"ghcr.io/org/app:v1.0.0", "ghcr.io/org/app", "v1.0.0"},
		{"docker.io/library/nginx:latest", "docker.io/library/nginx", "latest"},
		{"myapp:dev", "myapp", "dev"},
		{"ghcr.io/org/app", "ghcr.io/org/app", "latest"},
		{"localhost:5000/myapp:v1", "localhost:5000/myapp", "v1"},
		{"ghcr.io/org/app@sha256:abc123", "ghcr.io/org/app", "latest"},
		{"ghcr.io/org/app:v1@sha256:abc123", "ghcr.io/org/app", "v1"},
		// Scoped package names (npm-style)
		{"@clerk/clerk:latest", "@clerk/clerk", "latest"},
		{"@myorg/myapp:v1.0.0", "@myorg/myapp", "v1.0.0"},
		{"@scope/package", "@scope/package", "latest"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			repo, tag := ParseReference(tt.input)
			assert.Equal(t, tt.wantRepository, repo)
			assert.Equal(t, tt.wantTag, tag)
		})
	}
}
