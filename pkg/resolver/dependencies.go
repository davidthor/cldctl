package resolver

import (
	"context"
	"fmt"

	"github.com/davidthor/cldctl/pkg/schema/component"
)

// DependencyResolver resolves component dependencies recursively.
type DependencyResolver struct {
	resolver Resolver
	loader   component.Loader
	resolved map[string]ResolvedDependency
	visiting map[string]bool
}

// ResolvedDependency represents a resolved dependency.
type ResolvedDependency struct {
	// Name is the dependency name
	Name string

	// Component is the resolved component
	Component ResolvedComponent

	// LoadedComponent is the parsed component
	LoadedComponent component.Component

	// Dependencies are the transitive dependencies
	Dependencies []ResolvedDependency

	// Variables are the variables to pass to the component
	Variables map[string]string

	// Depth is the dependency depth (0 for root)
	Depth int
}

// DependencyGraph represents the full dependency graph.
type DependencyGraph struct {
	// Root is the root component
	Root ResolvedDependency

	// All contains all resolved dependencies by name
	All map[string]ResolvedDependency

	// Order is the topologically sorted order for deployment
	Order []string
}

// NewDependencyResolver creates a new dependency resolver.
func NewDependencyResolver(resolver Resolver) *DependencyResolver {
	return &DependencyResolver{
		resolver: resolver,
		loader:   component.NewLoader(),
		resolved: make(map[string]ResolvedDependency),
		visiting: make(map[string]bool),
	}
}

// Resolve resolves a component and all its dependencies.
func (r *DependencyResolver) Resolve(ctx context.Context, ref string, variables map[string]string) (*DependencyGraph, error) {
	// Reset state
	r.resolved = make(map[string]ResolvedDependency)
	r.visiting = make(map[string]bool)

	// Resolve root component
	root, err := r.resolveWithDeps(ctx, "root", ref, variables, 0)
	if err != nil {
		return nil, err
	}

	// Build dependency graph
	graph := &DependencyGraph{
		Root: root,
		All:  r.resolved,
	}

	// Topological sort for deployment order
	graph.Order = r.topologicalSort()

	return graph, nil
}

func (r *DependencyResolver) resolveWithDeps(ctx context.Context, name, ref string, variables map[string]string, depth int) (ResolvedDependency, error) {
	// Check if already resolved
	if resolved, ok := r.resolved[name]; ok {
		return resolved, nil
	}

	// Check for circular dependencies
	if r.visiting[ref] {
		return ResolvedDependency{}, fmt.Errorf("circular dependency detected: %s", ref)
	}
	r.visiting[ref] = true
	defer func() { delete(r.visiting, ref) }()

	// Resolve component reference
	resolved, err := r.resolver.Resolve(ctx, ref)
	if err != nil {
		return ResolvedDependency{}, fmt.Errorf("failed to resolve %s: %w", ref, err)
	}

	// Load component
	comp, err := r.loader.Load(resolved.Path)
	if err != nil {
		return ResolvedDependency{}, fmt.Errorf("failed to load %s: %w", resolved.Path, err)
	}

	// Create resolved dependency
	resolvedDep := ResolvedDependency{
		Name:            name,
		Component:       resolved,
		LoadedComponent: comp,
		Variables:       variables,
		Depth:           depth,
		Dependencies:    []ResolvedDependency{},
	}

	// Resolve transitive dependencies
	for _, dep := range comp.Dependencies() {
		depResolved, err := r.resolveWithDeps(ctx, dep.Name(), dep.Component(), nil, depth+1)
		if err != nil {
			return ResolvedDependency{}, fmt.Errorf("failed to resolve dependency %s: %w", dep.Name(), err)
		}

		resolvedDep.Dependencies = append(resolvedDep.Dependencies, depResolved)
	}

	// Cache result
	r.resolved[name] = resolvedDep

	return resolvedDep, nil
}

func (r *DependencyResolver) topologicalSort() []string {
	var order []string
	visited := make(map[string]bool)

	var visit func(name string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true

		if dep, ok := r.resolved[name]; ok {
			for _, d := range dep.Dependencies {
				visit(d.Name)
			}
		}

		order = append(order, name)
	}

	for name := range r.resolved {
		visit(name)
	}

	return order
}

// ValidateDependencies validates that all dependencies can be satisfied.
func (r *DependencyResolver) ValidateDependencies(ctx context.Context, ref string) error {
	_, err := r.Resolve(ctx, ref, nil)
	return err
}

// FlattenDependencies returns a flat list of all dependencies.
func (g *DependencyGraph) FlattenDependencies() []ResolvedDependency {
	deps := make([]ResolvedDependency, 0, len(g.Order))
	for _, name := range g.Order {
		if dep, ok := g.All[name]; ok {
			deps = append(deps, dep)
		}
	}
	return deps
}

// GetDependency retrieves a specific dependency by name.
func (g *DependencyGraph) GetDependency(name string) (ResolvedDependency, bool) {
	dep, ok := g.All[name]
	return dep, ok
}

// HasCircularDependencies checks if there are any circular dependencies.
func (g *DependencyGraph) HasCircularDependencies() bool {
	visited := make(map[string]int) // 0: unvisited, 1: visiting, 2: visited

	var hasCycle func(name string) bool
	hasCycle = func(name string) bool {
		if visited[name] == 1 {
			return true // Back edge found
		}
		if visited[name] == 2 {
			return false
		}

		visited[name] = 1

		if dep, ok := g.All[name]; ok {
			for _, d := range dep.Dependencies {
				if hasCycle(d.Name) {
					return true
				}
			}
		}

		visited[name] = 2
		return false
	}

	for name := range g.All {
		if hasCycle(name) {
			return true
		}
	}

	return false
}

// GetDeploymentOrder returns the order in which components should be deployed.
func (g *DependencyGraph) GetDeploymentOrder() []string {
	return g.Order
}

// GetDestroyOrder returns the order in which components should be destroyed (reverse of deploy).
func (g *DependencyGraph) GetDestroyOrder() []string {
	order := make([]string, len(g.Order))
	for i, name := range g.Order {
		order[len(g.Order)-1-i] = name
	}
	return order
}
