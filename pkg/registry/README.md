# Registry Package

The registry package provides a unified local registry for tracking built and pulled artifacts (both components and datacenters). This is similar to how `docker images` tracks locally available Docker images.

## Overview

The local registry stores metadata about artifacts that have been:
- **Built locally** using `arcctl build component` or `arcctl build datacenter`
- **Pulled manually** using `arcctl pull component` or `arcctl pull datacenter`
- **Pulled implicitly** during deploy operations (engine auto-pull)
- **Cached from source** during `arcctl deploy datacenter` from a local path

## Storage Location

The registry is stored as a JSON file at:
```
~/.arcctl/registry/artifacts.json
```

Artifact content is cached at:
```
~/.arcctl/cache/artifacts/<cache-key>/
```

### Migration

If `artifacts.json` doesn't exist but a legacy `components.json` does in the same directory, the registry automatically migrates entries on first load.

## Usage

```go
import "github.com/architect-io/arcctl/pkg/registry"

// Create a registry instance
reg, err := registry.NewRegistry()
if err != nil {
    return err
}

// Add an artifact
entry := registry.ArtifactEntry{
    Reference:  "ghcr.io/org/app:v1.0.0",
    Repository: "ghcr.io/org/app",
    Tag:        "v1.0.0",
    Type:       registry.TypeComponent,
    Source:     registry.SourceBuilt,
    Size:       1024,
    CreatedAt:  time.Now(),
    CachePath:  "/path/to/cache",
}
err = reg.Add(entry)

// List all artifacts
entries, err := reg.List()

// List only components
components, err := reg.ListByType(registry.TypeComponent)

// List only datacenters
datacenters, err := reg.ListByType(registry.TypeDatacenter)

// Get a specific artifact
entry, err := reg.Get("ghcr.io/org/app:v1.0.0")

// Remove an artifact
err = reg.Remove("ghcr.io/org/app:v1.0.0")

// Compute cache path for a reference
cachePath, err := registry.CachePathForRef("ghcr.io/org/app:v1.0.0")
```

## Artifact Entry Fields

| Field | Description |
|-------|-------------|
| `Reference` | Full OCI reference (e.g., `ghcr.io/org/app:v1.0.0`) |
| `Repository` | Repository portion (e.g., `ghcr.io/org/app`) |
| `Tag` | Tag portion (e.g., `v1.0.0`) |
| `Type` | Artifact type: `component` or `datacenter` |
| `Digest` | Content digest (sha256:...) |
| `Source` | How the artifact was added (`built` or `pulled`) |
| `Size` | Size in bytes |
| `CreatedAt` | When the artifact was added |
| `CachePath` | Local filesystem path to the cached artifact |

## Thread Safety

The registry implementation is thread-safe and uses atomic writes to prevent corruption.
