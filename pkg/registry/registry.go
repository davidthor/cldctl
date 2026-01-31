// Package registry provides a local registry for tracking built and pulled components.
package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ComponentEntry represents a component stored in the local registry.
type ComponentEntry struct {
	// Reference is the OCI reference (e.g., ghcr.io/org/app:v1.0.0)
	Reference string `json:"reference"`

	// Repository is the repository portion (e.g., ghcr.io/org/app)
	Repository string `json:"repository"`

	// Tag is the tag portion (e.g., v1.0.0)
	Tag string `json:"tag"`

	// Digest is the content digest (sha256:...)
	Digest string `json:"digest,omitempty"`

	// Source indicates how the component was added (built, pulled)
	Source ComponentSource `json:"source"`

	// Size is the size in bytes of the component artifact
	Size int64 `json:"size"`

	// CreatedAt is when the component was added to the registry
	CreatedAt time.Time `json:"createdAt"`

	// CachePath is the local path where the component is cached
	CachePath string `json:"cachePath"`
}

// ComponentSource indicates how a component was added to the local registry.
type ComponentSource string

const (
	// SourceBuilt indicates the component was built locally
	SourceBuilt ComponentSource = "built"

	// SourcePulled indicates the component was pulled from a remote registry
	SourcePulled ComponentSource = "pulled"
)

// Registry provides access to the local component registry.
type Registry interface {
	// Add adds or updates a component in the registry
	Add(entry ComponentEntry) error

	// Remove removes a component from the registry
	Remove(reference string) error

	// Get retrieves a component by reference
	Get(reference string) (*ComponentEntry, error)

	// List returns all components in the registry
	List() ([]ComponentEntry, error)

	// Clear removes all components from the registry
	Clear() error
}

// registry implements the Registry interface using a JSON file.
type registry struct {
	mu       sync.RWMutex
	filePath string
}

// registryData is the structure stored in the registry file.
type registryData struct {
	Version    string           `json:"version"`
	Components []ComponentEntry `json:"components"`
}

// DefaultRegistryPath returns the default path for the local registry.
func DefaultRegistryPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".arcctl", "registry", "components.json"), nil
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

	return &registry{
		filePath: path,
	}, nil
}

func (r *registry) load() (*registryData, error) {
	data, err := os.ReadFile(r.filePath)
	if os.IsNotExist(err) {
		return &registryData{
			Version:    "v1",
			Components: []ComponentEntry{},
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read registry file: %w", err)
	}

	var reg registryData
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("failed to parse registry file: %w", err)
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

func (r *registry) Add(entry ComponentEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := r.load()
	if err != nil {
		return err
	}

	// Check if component already exists and update or add
	found := false
	for i, existing := range data.Components {
		if existing.Reference == entry.Reference {
			data.Components[i] = entry
			found = true
			break
		}
	}

	if !found {
		data.Components = append(data.Components, entry)
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

	// Filter out the component
	filtered := make([]ComponentEntry, 0, len(data.Components))
	for _, entry := range data.Components {
		if entry.Reference != reference {
			filtered = append(filtered, entry)
		}
	}

	data.Components = filtered
	return r.save(data)
}

func (r *registry) Get(reference string) (*ComponentEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	data, err := r.load()
	if err != nil {
		return nil, err
	}

	for _, entry := range data.Components {
		if entry.Reference == reference {
			return &entry, nil
		}
	}

	return nil, fmt.Errorf("component %q not found in local registry", reference)
}

func (r *registry) List() ([]ComponentEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	data, err := r.load()
	if err != nil {
		return nil, err
	}

	// Sort by created time (most recent first)
	sort.Slice(data.Components, func(i, j int) bool {
		return data.Components[i].CreatedAt.After(data.Components[j].CreatedAt)
	})

	return data.Components, nil
}

func (r *registry) Clear() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data := &registryData{
		Version:    "v1",
		Components: []ComponentEntry{},
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
