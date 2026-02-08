# resolver

Component resolution for cldctl. Resolves component references from local filesystem, OCI registries, and Git repositories. Handles dependency resolution and topological sorting.

## Overview

The `resolver` package provides:

- Multi-source component resolution (local, OCI, Git)
- Caching of remote components
- Recursive dependency resolution with cycle detection
- Topological sorting for deployment/destruction order
- Variable passing to dependencies

## Types

### ReferenceType

Component reference types.

```go
const (
    ReferenceTypeLocal ReferenceType = "local"  // Local filesystem path
    ReferenceTypeOCI   ReferenceType = "oci"    // OCI registry reference
    ReferenceTypeGit   ReferenceType = "git"    // Git repository reference
)
```

### ResolvedComponent

Resolved component reference.

```go
type ResolvedComponent struct {
    Reference string            // Original reference
    Type      ReferenceType     // Reference type (local, oci, git)
    Path      string            // Local path to component
    Version   string            // Resolved version (tag, commit, etc.)
    Digest    string            // Content digest (for OCI)
    Metadata  map[string]string // Additional resolution info
}
```

### Options

Resolver configuration.

```go
type Options struct {
    CacheDir    string       // Directory to cache downloaded components
    AllowLocal  bool         // Allow resolving local filesystem paths
    AllowRemote bool         // Allow resolving remote references (OCI, git)
    OCIClient   *oci.Client  // OCI registry client
}
```

### ResolvedDependency

Resolved dependency with loaded component.

```go
type ResolvedDependency struct {
    Name            string               // Dependency name
    Component       ResolvedComponent    // Resolved component
    LoadedComponent component.Component  // Parsed component
    Dependencies    []ResolvedDependency // Transitive dependencies
    Variables       map[string]string    // Variables to pass to component
    Depth           int                  // Dependency depth (0 for root)
}
```

### DependencyGraph

Full dependency graph.

```go
type DependencyGraph struct {
    Root  ResolvedDependency            // Root component
    All   map[string]ResolvedDependency // All resolved dependencies by name
    Order []string                      // Topologically sorted order
}
```

## Resolver

### Creating a Resolver

```go
import "github.com/davidthor/cldctl/pkg/resolver"

// Create with default options
r := resolver.NewResolver(resolver.Options{
    AllowLocal:  true,
    AllowRemote: true,
})

// With custom cache directory
r := resolver.NewResolver(resolver.Options{
    CacheDir:    "/path/to/cache",
    AllowLocal:  true,
    AllowRemote: true,
    OCIClient:   oci.NewClient(),
})
```

### Resolving Components

```go
// Resolve a single component
resolved, err := r.Resolve(ctx, "./my-component")
if err != nil {
    log.Fatal(err)
}

fmt.Println(resolved.Path)    // Local path to component
fmt.Println(resolved.Type)    // "local"
fmt.Println(resolved.Version) // Version if available

// Resolve multiple components
components, err := r.ResolveAll(ctx, []string{
    "./api",
    "ghcr.io/myorg/shared:v1.0.0",
    "git::https://github.com/org/repo.git//components/web?ref=main",
})
```

### Detecting Reference Types

```go
refType := resolver.DetectReferenceType("./my-component")
// Returns: ReferenceTypeLocal

refType := resolver.DetectReferenceType("ghcr.io/myorg/component:v1.0.0")
// Returns: ReferenceTypeOCI

refType := resolver.DetectReferenceType("git::https://github.com/org/repo.git//path")
// Returns: ReferenceTypeGit
```

## Dependency Resolver

### Creating a Dependency Resolver

```go
import "github.com/davidthor/cldctl/pkg/resolver"

r := resolver.NewResolver(resolver.Options{
    AllowLocal:  true,
    AllowRemote: true,
})

depResolver := resolver.NewDependencyResolver(r)
```

### Resolving Dependencies

```go
// Resolve a component and all its dependencies
graph, err := depResolver.Resolve(ctx, "./my-app", map[string]string{
    "environment": "production",
})
if err != nil {
    log.Fatal(err)
}

// Get deployment order (dependencies first)
for _, name := range graph.GetDeploymentOrder() {
    dep, _ := graph.GetDependency(name)
    fmt.Printf("Deploy: %s\n", dep.Name)
}

// Get destruction order (dependents first)
for _, name := range graph.GetDestroyOrder() {
    fmt.Printf("Destroy: %s\n", name)
}
```

### Validating Dependencies

```go
// Check if all dependencies can be satisfied
err := depResolver.ValidateDependencies(ctx, "./my-app")
if err != nil {
    log.Fatal("Missing dependencies:", err)
}
```

### Working with the Dependency Graph

```go
// Get all dependencies as a flat list
deps := graph.FlattenDependencies()

// Get a specific dependency
dep, found := graph.GetDependency("shared-database")
if found {
    fmt.Println(dep.LoadedComponent.Name())
}

// Check for circular dependencies
if graph.HasCircularDependencies() {
    log.Fatal("Circular dependency detected")
}
```

## Reference Format Examples

### Local References (CLI only)

Local references are supported for CLI commands (e.g., `cldctl up ./my-component`) but are **not allowed** in component dependency specifications. For dependencies, use OCI or Git references instead.

```
./components/api
../shared/component
/absolute/path/to/component
./component.yml
```

### OCI References

```
ghcr.io/myorg/component:v1.0.0
docker.io/library/nginx:latest
registry.example.com/org/component@sha256:abc123...
myorg/component:latest  (defaults to docker.io)
```

### Git References

```
git::https://github.com/org/repo.git//path/to/component?ref=main
git::https://github.com/org/repo.git//components/web?ref=v1.0.0
git::git@github.com:org/repo.git//component?ref=feature-branch
```

## Caching

Remote components are cached locally to avoid repeated downloads:

- Default cache directory: `~/.cldctl/cache/components`
- OCI artifacts are extracted to cache
- Git repositories are cloned to cache
- Cache is keyed by reference and version/digest

## Example: Full Workflow

```go
import (
    "context"
    "fmt"
    "github.com/davidthor/cldctl/pkg/resolver"
    "github.com/davidthor/cldctl/pkg/oci"
)

func main() {
    ctx := context.Background()

    // Create resolver
    r := resolver.NewResolver(resolver.Options{
        CacheDir:    "~/.cldctl/cache/components",
        AllowLocal:  true,
        AllowRemote: true,
        OCIClient:   oci.NewClient(),
    })

    // Create dependency resolver
    depResolver := resolver.NewDependencyResolver(r)

    // Resolve component with dependencies
    graph, err := depResolver.Resolve(ctx, "./my-app", map[string]string{
        "env": "prod",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Print deployment order
    fmt.Println("Deployment order:")
    for i, name := range graph.GetDeploymentOrder() {
        dep, _ := graph.GetDependency(name)
        fmt.Printf("  %d. %s (%s)\n", i+1, name, dep.Component.Type)
    }

    // Access loaded components
    for name, dep := range graph.All {
        comp := dep.LoadedComponent
        fmt.Printf("\nComponent: %s\n", name)
        fmt.Printf("  Databases: %d\n", len(comp.Databases()))
        fmt.Printf("  Services: %d\n", len(comp.Services()))
        fmt.Printf("  Deployments: %d\n", len(comp.Deployments()))
    }
}
```

## Error Handling

The resolver returns detailed errors for common issues:

- Component not found
- Invalid reference format
- Network/registry errors
- Circular dependency detection
- Missing transitive dependencies
