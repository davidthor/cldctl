# oci

OCI artifact management for cldctl. Handles building, pushing, and pulling artifacts to/from OCI registries.

## Overview

The `oci` package provides:

- Building artifacts from directories
- Pushing/pulling artifacts to/from OCI registries
- Parsing and managing OCI references
- Support for component, datacenter, and module artifact types

## Types

### Artifact

Represents an OCI artifact.

```go
type Artifact struct {
    Type        ArtifactType
    Reference   string                 // OCI reference (repo:tag)
    Digest      string                 // Content digest
    Config      []byte                 // Artifact configuration
    Layers      []Layer
    Annotations map[string]string
}
```

### Layer

Represents a layer in an artifact.

```go
type Layer struct {
    MediaType   string
    Digest      string
    Size        int64
    Data        []byte
    Annotations map[string]string
}
```

### Reference

Parsed OCI reference.

```go
type Reference struct {
    Registry   string  // e.g., "docker.io", "ghcr.io"
    Repository string  // e.g., "library/nginx", "myorg/myapp"
    Tag        string  // e.g., "latest", "v1.0.0"
    Digest     string  // e.g., "sha256:abc123..."
}
```

### Artifact Types

```go
const (
    ArtifactTypeComponent  ArtifactType = "component"
    ArtifactTypeDatacenter ArtifactType = "datacenter"
    ArtifactTypeModule     ArtifactType = "module"
)
```

### Configuration Types

```go
type ComponentConfig struct {
    SchemaVersion  string
    Name           string
    Description    string
    ChildArtifacts map[string]string  // Resource type → OCI reference
    SourceHash     string
    BuildTime      string
}

type DatacenterConfig struct {
    SchemaVersion   string
    Name            string
    ModuleArtifacts map[string]string  // Module name → OCI reference
    SourceHash      string
    BuildTime       string
}

type ModuleConfig struct {
    Plugin     string              // "pulumi", "opentofu", or "native"
    Name       string
    Inputs     map[string]string   // Input schema summary
    Outputs    map[string]string   // Output schema summary
    SourceHash string
    BuildTime  string
}
```

## Client

### Creating a Client

```go
import "github.com/davidthor/cldctl/pkg/oci"

// Create a client with default keychain authentication
client := oci.NewClient()
```

### Parsing References

```go
ref, err := oci.ParseReference("ghcr.io/myorg/mycomponent:v1.0.0")
if err != nil {
    log.Fatal(err)
}

fmt.Println(ref.Registry)   // ghcr.io
fmt.Println(ref.Repository) // myorg/mycomponent
fmt.Println(ref.Tag)        // v1.0.0
fmt.Println(ref.String())   // ghcr.io/myorg/mycomponent:v1.0.0
```

### Building Artifacts

```go
// Build a component artifact from a directory
artifact, err := client.BuildFromDirectory(ctx, "./my-component", oci.ArtifactTypeComponent, oci.ComponentConfig{
    SchemaVersion: "v1",
    Name:          "my-component",
    Description:   "My awesome component",
})
if err != nil {
    log.Fatal(err)
}

// Set the reference for pushing
artifact.Reference = "ghcr.io/myorg/my-component:v1.0.0"
```

### Pushing Artifacts

```go
err := client.Push(ctx, artifact)
if err != nil {
    log.Fatal(err)
}

fmt.Println("Pushed:", artifact.Reference)
fmt.Println("Digest:", artifact.Digest)
```

### Pulling Artifacts

```go
// Pull an artifact and extract to a directory
err := client.Pull(ctx, "ghcr.io/myorg/my-component:v1.0.0", "./output")
if err != nil {
    log.Fatal(err)
}

// Pull only the config (without layers)
configData, err := client.PullConfig(ctx, "ghcr.io/myorg/my-component:v1.0.0")
if err != nil {
    log.Fatal(err)
}

var config oci.ComponentConfig
json.Unmarshal(configData, &config)
```

### Checking Existence

```go
exists, err := client.Exists(ctx, "ghcr.io/myorg/my-component:v1.0.0")
if err != nil {
    log.Fatal(err)
}

if exists {
    fmt.Println("Artifact exists")
}
```

### Tagging Artifacts

```go
// Tag an existing artifact with a new reference
err := client.Tag(ctx,
    "ghcr.io/myorg/my-component:v1.0.0",
    "ghcr.io/myorg/my-component:latest",
)
if err != nil {
    log.Fatal(err)
}
```

## Media Types

Custom media types for cldctl artifacts:

```go
const (
    // Component artifacts
    MediaTypeComponentConfig = "application/vnd.architect.component.config.v1+json"
    MediaTypeComponentLayer  = "application/vnd.architect.component.layer.v1.tar+gzip"

    // Datacenter artifacts
    MediaTypeDatacenterConfig = "application/vnd.architect.datacenter.config.v1+json"
    MediaTypeDatacenterLayer  = "application/vnd.architect.datacenter.layer.v1.tar+gzip"

    // Module artifacts
    MediaTypeModuleConfig = "application/vnd.architect.module.config.v1+json"
    MediaTypeModuleLayer  = "application/vnd.architect.module.layer.v1.tar+gzip"
)
```

## Authentication

The client uses the default keychain for authentication, which supports:

- Docker config (`~/.docker/config.json`)
- Credential helpers (e.g., `docker-credential-osxkeychain`)
- Environment variables (`DOCKER_CONFIG`)
- Platform-specific credential stores

## Example: Full Workflow

```go
import (
    "context"
    "github.com/davidthor/cldctl/pkg/oci"
)

func main() {
    ctx := context.Background()
    client := oci.NewClient()

    // Build artifact
    artifact, err := client.BuildFromDirectory(ctx, "./my-component",
        oci.ArtifactTypeComponent,
        oci.ComponentConfig{
            SchemaVersion: "v1",
            Name:          "api",
            Description:   "API component",
        },
    )
    if err != nil {
        log.Fatal(err)
    }

    // Push to registry
    artifact.Reference = "ghcr.io/myorg/api:v1.0.0"
    err = client.Push(ctx, artifact)
    if err != nil {
        log.Fatal(err)
    }

    // Tag as latest
    err = client.Tag(ctx,
        "ghcr.io/myorg/api:v1.0.0",
        "ghcr.io/myorg/api:latest",
    )
    if err != nil {
        log.Fatal(err)
    }

    // Pull to another location
    err = client.Pull(ctx, "ghcr.io/myorg/api:v1.0.0", "./downloaded")
    if err != nil {
        log.Fatal(err)
    }
}
```

## Security Notes

- Tar extraction includes directory traversal protection
- Uses secure connections (HTTPS) by default
- Supports both digest and tag-based references
- Validates content digests on pull
