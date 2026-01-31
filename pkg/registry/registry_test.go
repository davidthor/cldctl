package registry

import (
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

	entry := ComponentEntry{
		Reference:  "ghcr.io/org/app:v1.0.0",
		Repository: "ghcr.io/org/app",
		Tag:        "v1.0.0",
		Digest:     "sha256:abc123",
		Source:     SourceBuilt,
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
	assert.Equal(t, entry.Source, got.Source)
}

func TestRegistry_AddUpdatesExisting(t *testing.T) {
	tempDir := t.TempDir()
	regPath := filepath.Join(tempDir, "registry.json")

	reg, err := NewRegistryWithPath(regPath)
	require.NoError(t, err)

	entry1 := ComponentEntry{
		Reference:  "ghcr.io/org/app:v1.0.0",
		Repository: "ghcr.io/org/app",
		Tag:        "v1.0.0",
		Source:     SourcePulled,
		Size:       1024,
		CreatedAt:  time.Now(),
	}

	err = reg.Add(entry1)
	require.NoError(t, err)

	// Update with new entry
	entry2 := ComponentEntry{
		Reference:  "ghcr.io/org/app:v1.0.0",
		Repository: "ghcr.io/org/app",
		Tag:        "v1.0.0",
		Source:     SourceBuilt,
		Size:       2048,
		CreatedAt:  time.Now(),
	}

	err = reg.Add(entry2)
	require.NoError(t, err)

	// Should only have one entry
	entries, err := reg.List()
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, SourceBuilt, entries[0].Source)
	assert.Equal(t, int64(2048), entries[0].Size)
}

func TestRegistry_Remove(t *testing.T) {
	tempDir := t.TempDir()
	regPath := filepath.Join(tempDir, "registry.json")

	reg, err := NewRegistryWithPath(regPath)
	require.NoError(t, err)

	entry := ComponentEntry{
		Reference:  "ghcr.io/org/app:v1.0.0",
		Repository: "ghcr.io/org/app",
		Tag:        "v1.0.0",
		Source:     SourceBuilt,
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
	entries := []ComponentEntry{
		{
			Reference:  "ghcr.io/org/app:v1.0.0",
			Repository: "ghcr.io/org/app",
			Tag:        "v1.0.0",
			Source:     SourceBuilt,
			CreatedAt:  now.Add(-2 * time.Hour),
		},
		{
			Reference:  "ghcr.io/org/app:v2.0.0",
			Repository: "ghcr.io/org/app",
			Tag:        "v2.0.0",
			Source:     SourcePulled,
			CreatedAt:  now.Add(-1 * time.Hour),
		},
		{
			Reference:  "docker.io/library/nginx:latest",
			Repository: "docker.io/library/nginx",
			Tag:        "latest",
			Source:     SourcePulled,
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

func TestRegistry_Clear(t *testing.T) {
	tempDir := t.TempDir()
	regPath := filepath.Join(tempDir, "registry.json")

	reg, err := NewRegistryWithPath(regPath)
	require.NoError(t, err)

	entry := ComponentEntry{
		Reference:  "ghcr.io/org/app:v1.0.0",
		Repository: "ghcr.io/org/app",
		Tag:        "v1.0.0",
		Source:     SourceBuilt,
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

	entry := ComponentEntry{
		Reference:  "ghcr.io/org/app:v1.0.0",
		Repository: "ghcr.io/org/app",
		Tag:        "v1.0.0",
		Source:     SourceBuilt,
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

	entry := ComponentEntry{
		Reference:  "test:v1",
		Repository: "test",
		Tag:        "v1",
		Source:     SourceBuilt,
		CreatedAt:  time.Now(),
	}

	err = reg.Add(entry)
	require.NoError(t, err)

	// File should exist
	_, err = os.Stat(regPath)
	require.NoError(t, err)
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
