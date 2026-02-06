// Package state provides state management for arcctl.
package state

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"

	"github.com/architect-io/arcctl/pkg/state/backend"
	"github.com/architect-io/arcctl/pkg/state/types"
)

// Manager provides high-level state operations.
type Manager interface {
	// Datacenter operations
	GetDatacenter(ctx context.Context, name string) (*types.DatacenterState, error)
	SaveDatacenter(ctx context.Context, state *types.DatacenterState) error
	DeleteDatacenter(ctx context.Context, name string) error
	ListDatacenters(ctx context.Context) ([]string, error)

	// Environment operations (datacenter-scoped)
	ListEnvironments(ctx context.Context, datacenter string) ([]types.EnvironmentRef, error)
	GetEnvironment(ctx context.Context, datacenter, name string) (*types.EnvironmentState, error)
	SaveEnvironment(ctx context.Context, datacenter string, state *types.EnvironmentState) error
	DeleteEnvironment(ctx context.Context, datacenter, name string) error

	// Component operations (datacenter-scoped)
	GetComponent(ctx context.Context, dc, env, component string) (*types.ComponentState, error)
	SaveComponent(ctx context.Context, dc, env string, state *types.ComponentState) error
	DeleteComponent(ctx context.Context, dc, env, component string) error

	// Resource operations (datacenter-scoped)
	GetResource(ctx context.Context, dc, env, component, resource string) (*types.ResourceState, error)
	SaveResource(ctx context.Context, dc, env, component string, state *types.ResourceState) error
	DeleteResource(ctx context.Context, dc, env, component, resource string) error

	// Locking
	Lock(ctx context.Context, scope LockScope) (backend.Lock, error)

	// Backend info
	Backend() backend.Backend
}

// LockScope defines what to lock.
type LockScope struct {
	Datacenter  string
	Environment string
	Component   string
	Operation   string
	Who         string
}

// manager implements the Manager interface.
type manager struct {
	backend backend.Backend
}

// NewManager creates a new state manager with the given backend.
func NewManager(b backend.Backend) Manager {
	return &manager{backend: b}
}

// NewManagerFromConfig creates a new state manager from backend configuration.
func NewManagerFromConfig(config backend.Config) (Manager, error) {
	b, err := backend.Create(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create backend: %w", err)
	}
	return NewManager(b), nil
}

func (m *manager) Backend() backend.Backend {
	return m.backend
}

// Datacenter operations

func (m *manager) GetDatacenter(ctx context.Context, name string) (*types.DatacenterState, error) {
	p := datacenterPath(name)
	return readJSON[types.DatacenterState](ctx, m.backend, p)
}

func (m *manager) SaveDatacenter(ctx context.Context, state *types.DatacenterState) error {
	p := datacenterPath(state.Name)
	return writeJSON(ctx, m.backend, p, state)
}

func (m *manager) DeleteDatacenter(ctx context.Context, name string) error {
	// Delete all state under the datacenter (including environments)
	paths, err := m.backend.List(ctx, path.Join("datacenters", name))
	if err != nil {
		return err
	}

	for _, p := range paths {
		if err := m.backend.Delete(ctx, p); err != nil {
			return fmt.Errorf("failed to delete %s: %w", p, err)
		}
	}

	return nil
}

func (m *manager) ListDatacenters(ctx context.Context) ([]string, error) {
	paths, err := m.backend.List(ctx, "datacenters/")
	if err != nil {
		return nil, err
	}

	// Extract datacenter names from paths
	names := make(map[string]bool)
	for _, p := range paths {
		// Path format: datacenters/<name>/datacenter.state.json
		// or: datacenters/<name>/environments/...
		parts := splitPath(p)
		if len(parts) >= 2 {
			names[parts[1]] = true
		}
	}

	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	return result, nil
}

// Environment operations

func (m *manager) ListEnvironments(ctx context.Context, datacenter string) ([]types.EnvironmentRef, error) {
	prefix := path.Join("datacenters", datacenter, "environments") + "/"
	paths, err := m.backend.List(ctx, prefix)
	if err != nil {
		return nil, err
	}

	// Extract environment names from paths
	// Path format: datacenters/<dc>/environments/<name>/environment.state.json
	names := make(map[string]bool)
	for _, p := range paths {
		parts := splitPath(p)
		if len(parts) >= 4 {
			names[parts[3]] = true
		}
	}

	var refs []types.EnvironmentRef
	for name := range names {
		state, err := m.GetEnvironment(ctx, datacenter, name)
		if err != nil {
			continue // Skip environments that can't be read
		}
		refs = append(refs, types.EnvironmentRef{
			Name:       state.Name,
			Datacenter: state.Datacenter,
			CreatedAt:  state.CreatedAt,
			UpdatedAt:  state.UpdatedAt,
		})
	}

	return refs, nil
}

func (m *manager) GetEnvironment(ctx context.Context, datacenter, name string) (*types.EnvironmentState, error) {
	p := environmentPath(datacenter, name)
	return readJSON[types.EnvironmentState](ctx, m.backend, p)
}

func (m *manager) SaveEnvironment(ctx context.Context, datacenter string, state *types.EnvironmentState) error {
	p := environmentPath(datacenter, state.Name)
	return writeJSON(ctx, m.backend, p, state)
}

func (m *manager) DeleteEnvironment(ctx context.Context, datacenter, name string) error {
	// Delete all state under the environment
	paths, err := m.backend.List(ctx, path.Join("datacenters", datacenter, "environments", name))
	if err != nil {
		return err
	}

	for _, p := range paths {
		if err := m.backend.Delete(ctx, p); err != nil {
			return fmt.Errorf("failed to delete %s: %w", p, err)
		}
	}

	return nil
}

// Component operations

func (m *manager) GetComponent(ctx context.Context, dc, env, component string) (*types.ComponentState, error) {
	p := componentPath(dc, env, component)
	return readJSON[types.ComponentState](ctx, m.backend, p)
}

func (m *manager) SaveComponent(ctx context.Context, dc, env string, state *types.ComponentState) error {
	p := componentPath(dc, env, state.Name)
	return writeJSON(ctx, m.backend, p, state)
}

func (m *manager) DeleteComponent(ctx context.Context, dc, env, component string) error {
	// Delete all state under the component
	paths, err := m.backend.List(ctx, path.Join("datacenters", dc, "environments", env, "components", component))
	if err != nil {
		return err
	}

	for _, p := range paths {
		if err := m.backend.Delete(ctx, p); err != nil {
			return fmt.Errorf("failed to delete %s: %w", p, err)
		}
	}

	return nil
}

// Resource operations

func (m *manager) GetResource(ctx context.Context, dc, env, component, resource string) (*types.ResourceState, error) {
	p := resourcePath(dc, env, component, resource)
	return readJSON[types.ResourceState](ctx, m.backend, p)
}

func (m *manager) SaveResource(ctx context.Context, dc, env, component string, state *types.ResourceState) error {
	// Use type-qualified key for the file path to avoid collisions
	key := state.Type + "." + state.Name
	p := resourcePath(dc, env, component, key)
	return writeJSON(ctx, m.backend, p, state)
}

func (m *manager) DeleteResource(ctx context.Context, dc, env, component, resource string) error {
	p := resourcePath(dc, env, component, resource)
	return m.backend.Delete(ctx, p)
}

// Locking

func (m *manager) Lock(ctx context.Context, scope LockScope) (backend.Lock, error) {
	lockPath := path.Join("datacenters", scope.Datacenter, "environments", scope.Environment)
	if scope.Component != "" {
		lockPath = path.Join(lockPath, scope.Component)
	}

	info := backend.LockInfo{
		Who:       scope.Who,
		Operation: scope.Operation,
	}

	return m.backend.Lock(ctx, lockPath, info)
}

// Path helpers

func datacenterPath(name string) string {
	return path.Join("datacenters", name, "datacenter.state.json")
}

func environmentPath(dc, name string) string {
	return path.Join("datacenters", dc, "environments", name, "environment.state.json")
}

func componentPath(dc, env, component string) string {
	return path.Join("datacenters", dc, "environments", env, "components", component, "component.state.json")
}

func resourcePath(dc, env, component, resource string) string {
	return path.Join("datacenters", dc, "environments", env, "components", component, "resources", resource+".state.json")
}

func splitPath(p string) []string {
	var parts []string
	for p != "" && p != "." && p != "/" {
		dir, file := path.Split(p)
		if file != "" {
			parts = append([]string{file}, parts...)
		}
		p = path.Clean(dir)
	}
	return parts
}

// JSON helpers

func readJSON[T any](ctx context.Context, b backend.Backend, p string) (*T, error) {
	reader, err := b.Read(ctx, p)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var result T
	if err := json.NewDecoder(reader).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	return &result, nil
}

func writeJSON(ctx context.Context, b backend.Backend, p string, data interface{}) error {
	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	return b.Write(ctx, p, bytes.NewReader(content))
}

// Ensure manager imports are used
var _ io.Reader = (*bytes.Reader)(nil)
