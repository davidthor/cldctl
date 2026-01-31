# Registry Package

The registry package provides a local registry for tracking built and pulled component artifacts. This is similar to how `docker images` tracks locally available Docker images.

## Overview

The local registry stores metadata about components that have been:
- **Built locally** using `arcctl component build`
- **Pulled manually** using `arcctl component pull`
- **Pulled implicitly** during deploy operations

## Storage Location

The registry is stored as a JSON file at:
```
~/.arcctl/registry/components.json
```

## Usage

```go
import "github.com/architect-io/arcctl/pkg/registry"

// Create a registry instance
reg, err := registry.NewRegistry()
if err != nil {
    return err
}

// Add a component
entry := registry.ComponentEntry{
    Reference:  "ghcr.io/org/app:v1.0.0",
    Repository: "ghcr.io/org/app",
    Tag:        "v1.0.0",
    Source:     registry.SourceBuilt,
    Size:       1024,
    CreatedAt:  time.Now(),
    CachePath:  "/path/to/cache",
}
err = reg.Add(entry)

// List all components
entries, err := reg.List()

// Get a specific component
entry, err := reg.Get("ghcr.io/org/app:v1.0.0")

// Remove a component
err = reg.Remove("ghcr.io/org/app:v1.0.0")
```

## Component Entry Fields

| Field | Description |
|-------|-------------|
| `Reference` | Full OCI reference (e.g., `ghcr.io/org/app:v1.0.0`) |
| `Repository` | Repository portion (e.g., `ghcr.io/org/app`) |
| `Tag` | Tag portion (e.g., `v1.0.0`) |
| `Digest` | Content digest (sha256:...) |
| `Source` | How the component was added (`built` or `pulled`) |
| `Size` | Size in bytes |
| `CreatedAt` | When the component was added |
| `CachePath` | Local filesystem path to the cached component |

## Thread Safety

The registry implementation is thread-safe and uses atomic writes to prevent corruption.
