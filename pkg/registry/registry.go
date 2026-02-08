// Package registry provides a local registry for tracking built and pulled artifacts.
package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ArtifactType identifies the type of artifact in the registry.
type ArtifactType string

const (
	// TypeComponent identifies a component artifact.
	TypeComponent ArtifactType = "component"

	// TypeDatacenter identifies a datacenter artifact.
	TypeDatacenter ArtifactType = "datacenter"
)

// ArtifactEntry represents an artifact stored in the local registry.
type ArtifactEntry struct {
	// Reference is the full tag (e.g., ghcr.io/org/app:v1.0.0, my-dc:latest)
	Reference string `json:"reference"`

	// Repository is the repository portion (e.g., ghcr.io/org/app)
	Repository string `json:"repository"`

	// Tag is the tag portion (e.g., v1.0.0, latest)
	Tag string `json:"tag"`

	// Type identifies the artifact type (component or datacenter)
	Type ArtifactType `json:"type"`

	// Digest is the content digest (sha256:...)
	Digest string `json:"digest,omitempty"`

	// Size is the size in bytes of the artifact
	Size int64 `json:"size"`

	// CreatedAt is when the artifact was added to the registry
	CreatedAt time.Time `json:"createdAt"`

	// CachePath is the local path where the artifact is cached
	CachePath string `json:"cachePath"`
}

// ---- Backward-compatible type aliases ----

// ComponentEntry is an alias for ArtifactEntry (backward compatibility).
type ComponentEntry = ArtifactEntry

// Registry provides access to the local artifact registry.
type Registry interface {
	// Add adds or updates an artifact in the registry.
	Add(entry ArtifactEntry) error

	// Remove removes an artifact from the registry by reference.
	Remove(reference string) error

	// Get retrieves an artifact by reference.
	Get(reference string) (*ArtifactEntry, error)

	// List returns all artifacts in the registry.
	List() ([]ArtifactEntry, error)

	// ListByType returns artifacts filtered by type.
	ListByType(artifactType ArtifactType) ([]ArtifactEntry, error)

	// Clear removes all artifacts from the registry.
	Clear() error
}

// registry implements the Registry interface using a JSON file.
type registry struct {
	mu       sync.RWMutex
	filePath string
}

// registryData is the structure stored in the registry file.
type registryData struct {
	Version   string          `json:"version"`
	Artifacts []ArtifactEntry `json:"artifacts"`
}

// DefaultRegistryPath returns the default path for the local registry.
func DefaultRegistryPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".cldctl", "registry", "artifacts.json"), nil
}

// DefaultCachePath returns the default base path for the artifact cache.
func DefaultCachePath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".cldctl", "cache", "artifacts"), nil
}

// CacheKey converts an OCI-style reference into a filesystem-safe key.
func CacheKey(reference string) string {
	key := strings.ReplaceAll(reference, "/", "_")
	key = strings.ReplaceAll(key, ":", "_")
	return key
}

// CachePathForRef returns the full cache directory path for an artifact reference.
func CachePathForRef(reference string) (string, error) {
	base, err := DefaultCachePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, CacheKey(reference)), nil
}

// NewRegistry creates a new registry with the default path.
func NewRegistry() (Registry, error) {
	path, err := DefaultRegistryPath()
	if err != nil {
		return nil, err
	}
	return NewRegistryWithPath(path)
}

// NewRegistryWithPath creates a new registry with a custom path.
func NewRegistryWithPath(path string) (Registry, error) {
	// Ensure the directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create registry directory: %w", err)
	}

	r := &registry{filePath: path}

	// Migrate from legacy components.json if needed
	if err := r.migrateIfNeeded(); err != nil {
		// Non-fatal: log and continue with empty registry
		fmt.Fprintf(os.Stderr, "warning: failed to migrate legacy registry: %v\n", err)
	}

	return r, nil
}

// migrateIfNeeded checks for legacy components.json and migrates to the new format.
func (r *registry) migrateIfNeeded() error {
	// Only migrate when using the default path convention
	if _, err := os.Stat(r.filePath); err == nil {
		return nil // artifacts.json already exists, nothing to migrate
	}

	// Check for legacy components.json in the same directory
	legacyPath := filepath.Join(filepath.Dir(r.filePath), "components.json")
	legacyData, err := os.ReadFile(legacyPath)
	if err != nil {
		return nil // No legacy file, nothing to migrate
	}

	// Parse legacy format
	var legacy struct {
		Version    string `json:"version"`
		Components []struct {
			Reference  string    `json:"reference"`
			Repository string    `json:"repository"`
			Tag        string    `json:"tag"`
			Digest     string    `json:"digest,omitempty"`
			Size       int64     `json:"size"`
			CreatedAt  time.Time `json:"createdAt"`
			CachePath  string    `json:"cachePath"`
		} `json:"components"`
	}
	if err := json.Unmarshal(legacyData, &legacy); err != nil {
		return fmt.Errorf("failed to parse legacy registry: %w", err)
	}

	// Convert to new format
	data := &registryData{
		Version:   "v2",
		Artifacts: make([]ArtifactEntry, 0, len(legacy.Components)),
	}
	for _, c := range legacy.Components {
		data.Artifacts = append(data.Artifacts, ArtifactEntry{
			Reference:  c.Reference,
			Repository: c.Repository,
			Tag:        c.Tag,
			Type:       TypeComponent,
			Digest:     c.Digest,
			Size:       c.Size,
			CreatedAt:  c.CreatedAt,
			CachePath:  c.CachePath,
		})
	}

	// Save new format
	if err := r.save(data); err != nil {
		return fmt.Errorf("failed to save migrated registry: %w", err)
	}

	return nil
}

func (r *registry) load() (*registryData, error) {
	data, err := os.ReadFile(r.filePath)
	if os.IsNotExist(err) {
		return &registryData{
			Version:   "v2",
			Artifacts: []ArtifactEntry{},
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read registry file: %w", err)
	}

	var reg registryData
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("failed to parse registry file: %w", err)
	}

	// Handle v1 format (had "components" key instead of "artifacts")
	if reg.Artifacts == nil {
		var v1 struct {
			Components []ArtifactEntry `json:"components"`
		}
		if err := json.Unmarshal(data, &v1); err == nil && len(v1.Components) > 0 {
			reg.Artifacts = v1.Components
			// Default type to component for entries migrated inline
			for i := range reg.Artifacts {
				if reg.Artifacts[i].Type == "" {
					reg.Artifacts[i].Type = TypeComponent
				}
			}
		}
	}

	return &reg, nil
}

func (r *registry) save(data *registryData) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal registry data: %w", err)
	}

	// Write to temp file first for atomic write
	tempFile := r.filePath + ".tmp"
	if err := os.WriteFile(tempFile, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write registry file: %w", err)
	}

	// Rename for atomic update
	if err := os.Rename(tempFile, r.filePath); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to update registry file: %w", err)
	}

	return nil
}

func (r *registry) Add(entry ArtifactEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := r.load()
	if err != nil {
		return err
	}

	// Check if artifact already exists and update or add
	found := false
	for i, existing := range data.Artifacts {
		if existing.Reference == entry.Reference {
			data.Artifacts[i] = entry
			found = true
			break
		}
	}

	if !found {
		data.Artifacts = append(data.Artifacts, entry)
	}

	return r.save(data)
}

func (r *registry) Remove(reference string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := r.load()
	if err != nil {
		return err
	}

	filtered := make([]ArtifactEntry, 0, len(data.Artifacts))
	for _, entry := range data.Artifacts {
		if entry.Reference != reference {
			filtered = append(filtered, entry)
		}
	}

	data.Artifacts = filtered
	return r.save(data)
}

func (r *registry) Get(reference string) (*ArtifactEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	data, err := r.load()
	if err != nil {
		return nil, err
	}

	for _, entry := range data.Artifacts {
		if entry.Reference == reference {
			return &entry, nil
		}
	}

	return nil, fmt.Errorf("artifact %q not found in local registry", reference)
}

func (r *registry) List() ([]ArtifactEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	data, err := r.load()
	if err != nil {
		return nil, err
	}

	// Sort by created time (most recent first)
	sort.Slice(data.Artifacts, func(i, j int) bool {
		return data.Artifacts[i].CreatedAt.After(data.Artifacts[j].CreatedAt)
	})

	return data.Artifacts, nil
}

func (r *registry) ListByType(artifactType ArtifactType) ([]ArtifactEntry, error) {
	all, err := r.List()
	if err != nil {
		return nil, err
	}

	filtered := make([]ArtifactEntry, 0)
	for _, entry := range all {
		if entry.Type == artifactType {
			filtered = append(filtered, entry)
		}
	}
	return filtered, nil
}

func (r *registry) Clear() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data := &registryData{
		Version:   "v2",
		Artifacts: []ArtifactEntry{},
	}

	return r.save(data)
}

// ParseReference extracts repository and tag from a full OCI reference.
func ParseReference(ref string) (repository, tag string) {
	// Handle digest references (e.g., repo:tag@sha256:abc123)
	// Only treat @ as a digest separator if it's followed by a hash algorithm
	// This allows scoped package names like @clerk/clerk:latest
	if idx := findLastIndex(ref, "@"); idx > 0 {
		afterAt := ref[idx+1:]
		// Check if this looks like a digest (starts with sha256:, sha512:, etc.)
		if len(afterAt) > 0 && (hasPrefix(afterAt, "sha256:") || hasPrefix(afterAt, "sha512:")) {
			ref = ref[:idx]
		}
	}

	// Find the tag separator
	if idx := findLastIndex(ref, ":"); idx != -1 {
		// Make sure this isn't a port number (has / after it)
		afterColon := ref[idx+1:]
		if !containsSlash(afterColon) {
			return ref[:idx], afterColon
		}
	}

	return ref, "latest"
}

func hasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return s[:len(prefix)] == prefix
}

func findLastIndex(s string, substr string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if string(s[i]) == substr {
			return i
		}
	}
	return -1
}

func containsSlash(s string) bool {
	for _, c := range s {
		if c == '/' {
			return true
		}
	}
	return false
}
