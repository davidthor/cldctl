# Specification Versioning Guide

This document describes the versioning strategy for component and datacenter specifications in cldctl.

## Overview

cldctl uses a versioned specification system that allows:

1. **Backward compatibility**: Old configuration files continue to work with new versions of cldctl
2. **Forward evolution**: The specification can evolve to support new features
3. **Clear migration paths**: Users can upgrade configurations at their own pace
4. **Internal stability**: Core logic works with a stable internal representation

## Versioning Philosophy

### Version Numbering

Specifications use semantic versioning with the format `v<major>`:

- **v1, v2, v3, etc.**: Major versions that may include breaking changes
- Minor/patch changes within a major version are backward compatible

### Version Declaration

Configuration files can optionally declare their version:

**Component (YAML):**

```yaml
version: v1 # Optional, defaults to latest stable
name: my-app
# ...
```

**Datacenter (HCL):**

```hcl
version = "v1"  # Optional, defaults to latest stable

variable "region" {
  # ...
}
```

### Version Detection

When no version is specified, cldctl uses heuristics to detect the version:

1. Check for version-specific fields or syntax
2. Fall back to the latest stable version
3. Emit a warning recommending explicit version declaration

## Architecture

### External vs Internal Representations

The versioning system uses a two-layer approach:

```
┌─────────────────────────────────────────────────────────────┐
│                    External Layer                            │
├──────────────┬──────────────┬──────────────┬────────────────┤
│   v1 Schema  │   v2 Schema  │   v3 Schema  │   Future...    │
│   (YAML/HCL) │   (YAML/HCL) │   (YAML/HCL) │                │
└──────┬───────┴──────┬───────┴──────┬───────┴────────────────┘
       │              │              │
       ▼              ▼              ▼
┌──────────────────────────────────────────────────────────────┐
│                    Transformers                               │
│   v1Transformer    v2Transformer    v3Transformer            │
└──────────────────────────┬───────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────┐
│                Internal Representation                        │
│   InternalComponent / InternalDatacenter                     │
│   (Stable, version-agnostic)                                 │
└──────────────────────────────────────────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────────────┐
│                    Core Logic                                 │
│   Graph Builder, Planner, Executor, Expression Evaluator     │
└──────────────────────────────────────────────────────────────┘
```

### Benefits

1. **Core stability**: Engine code only deals with internal types
2. **Version isolation**: Changes to one version don't affect others
3. **Easy additions**: New versions add packages, don't modify existing ones
4. **Testing simplicity**: Internal representation has comprehensive tests

## Internal Representation

### Component Internal Types

```go
// pkg/schema/component/internal/types.go

package internal

// InternalComponent is the canonical internal representation.
// All version-specific schemas transform to this type.
type InternalComponent struct {
    // Metadata
    Name        string
    Description string

    // Resources
    Databases    []InternalDatabase
    Buckets      []InternalBucket
    Deployments  []InternalDeployment
    Functions    []InternalFunction
    Services     []InternalService
    Routes       []InternalRoute
    Cronjobs     []InternalCronjob

    // Configuration
    Variables    []InternalVariable
    Dependencies []InternalDependency

    // Source information
    SourceVersion string  // Which schema version this came from
    SourcePath    string  // Original file path
}

// InternalDatabase represents a database requirement.
// This structure is stable across all schema versions.
type InternalDatabase struct {
    Name       string
    Type       string              // e.g., "postgres"
    Version    string              // e.g., "^15" (semver constraint)
    Migrations *InternalMigrations // Optional
}

// InternalMigrations represents database migration configuration.
type InternalMigrations struct {
    // Source form (mutually exclusive with Image)
    Build *InternalBuild

    // Compiled form (mutually exclusive with Build)
    Image string

    // Common fields
    Command     []string
    Environment map[string]string
}

// InternalBuild represents a container build configuration.
type InternalBuild struct {
    Context    string            // Build context directory
    Dockerfile string            // Dockerfile path within context
    Target     string            // Build target for multi-stage
    Args       map[string]string // Build arguments
}

// InternalDeployment represents a deployment workload.
type InternalDeployment struct {
    Name        string

    // Image source (one of these is set)
    Image       string          // Pre-built image reference
    Build       *InternalBuild  // Build from source

    // Container configuration
    Command     []string
    Entrypoint  []string
    Environment map[string]Expression  // Values may contain expressions

    // Resource allocation
    CPU         string
    Memory      string
    Replicas    int

    // Advanced configuration
    Volumes        []InternalVolume
    LivenessProbe  *InternalProbe
    ReadinessProbe *InternalProbe
}

// InternalRoute represents a routing configuration.
type InternalRoute struct {
    Name     string
    Type     string           // "http" or "grpc"
    Internal bool             // VPC-only access
    Rules    []InternalRouteRule

    // Simplified form (alternative to Rules)
    Service  string           // Direct service reference
    Function string           // Direct function reference
    Port     int
}

// InternalRouteRule represents a routing rule.
type InternalRouteRule struct {
    Name        string
    Matches     []InternalRouteMatch
    BackendRefs []InternalBackendRef
    Filters     []InternalRouteFilter
    Timeouts    *InternalTimeouts
}

// Expression represents a value that may contain expressions.
// This wraps string values that need expression evaluation.
type Expression struct {
    Raw        string   // Original string value
    IsTemplate bool     // Whether this contains ${{ }} expressions
}
```

### Datacenter Internal Types

```go
// pkg/schema/datacenter/internal/types.go

package internal

// InternalDatacenter is the canonical internal representation.
type InternalDatacenter struct {
    // Variables
    Variables []InternalVariable

    // Datacenter-level modules
    Modules []InternalModule

    // Environment configuration
    Environment InternalEnvironment

    // Source information
    SourceVersion string
    SourcePath    string
}

// InternalModule represents an IaC module.
type InternalModule struct {
    Name   string

    // Source (one is set)
    Build  string  // Local path for source form
    Source string  // OCI reference for compiled form

    // Configuration
    Plugin  string              // "pulumi", "opentofu", etc.
    Inputs  map[string]HCLExpr  // Input values (may be HCL expressions)
    When    HCLExpr             // Conditional expression

    // Volume mounts
    Volumes []InternalVolumeMount
}

// InternalEnvironment represents environment-level configuration.
type InternalEnvironment struct {
    Modules []InternalModule
    Hooks   InternalHooks
}

// InternalHooks contains resource hooks.
type InternalHooks struct {
    Database          []InternalHook
    DatabaseMigration []InternalHook
    Bucket            []InternalHook
    DatabaseUser      []InternalHook
    Deployment        []InternalHook
    Function          []InternalHook
    Service           []InternalHook
    Route             []InternalHook
    Cronjob           []InternalHook
    Secret            []InternalHook
    DockerBuild       []InternalHook
}

// InternalHook represents a resource hook.
type InternalHook struct {
    When    HCLExpr              // Conditional
    Modules []InternalModule     // Modules to execute
    Outputs map[string]HCLExpr   // Output mappings
}

// HCLExpr wraps an HCL expression that needs evaluation.
type HCLExpr struct {
    Raw string  // Original HCL expression
}
```

## Version-Specific Schemas

### V1 Component Schema

```go
// pkg/schema/component/v1/types.go

package v1

// SchemaV1 represents the v1 component schema.
type SchemaV1 struct {
    Version     string `yaml:"version,omitempty"`
    Name        string `yaml:"name"`
    Description string `yaml:"description,omitempty"`

    Databases    map[string]DatabaseV1   `yaml:"databases,omitempty"`
    Buckets      map[string]BucketV1     `yaml:"buckets,omitempty"`
    Deployments  map[string]DeploymentV1 `yaml:"deployments,omitempty"`
    Functions    map[string]FunctionV1   `yaml:"functions,omitempty"`
    Services     map[string]ServiceV1    `yaml:"services,omitempty"`
    Routes       map[string]RouteV1      `yaml:"routes,omitempty"`
    Cronjobs     map[string]CronjobV1    `yaml:"cronjobs,omitempty"`

    Variables    map[string]VariableV1   `yaml:"variables,omitempty"`
    Dependencies map[string]DependencyV1 `yaml:"dependencies,omitempty"`
}

// DatabaseV1 represents a database in v1 schema.
type DatabaseV1 struct {
    Type       string       `yaml:"type"`
    Migrations *MigrationsV1 `yaml:"migrations,omitempty"`
}

// MigrationsV1 represents migrations in v1 schema.
type MigrationsV1 struct {
    Build       *BuildV1          `yaml:"build,omitempty"`
    Image       string            `yaml:"image,omitempty"`
    Command     []string          `yaml:"command,omitempty"`
    Environment map[string]string `yaml:"environment,omitempty"`
}

// DeploymentV1 represents a deployment in v1 schema.
type DeploymentV1 struct {
    Image          string              `yaml:"image,omitempty"`
    Build          *BuildV1            `yaml:"build,omitempty"`
    Command        []string            `yaml:"command,omitempty"`
    Entrypoint     []string            `yaml:"entrypoint,omitempty"`
    Environment    map[string]string   `yaml:"environment,omitempty"`
    CPU            string              `yaml:"cpu,omitempty"`
    Memory         string              `yaml:"memory,omitempty"`
    Replicas       int                 `yaml:"replicas,omitempty"`
    Volumes        []VolumeV1          `yaml:"volumes,omitempty"`
    LivenessProbe  *ProbeV1            `yaml:"liveness_probe,omitempty"`
    ReadinessProbe *ProbeV1            `yaml:"readiness_probe,omitempty"`
}

// RouteV1 represents a route in v1 schema.
type RouteV1 struct {
    Type     string        `yaml:"type"`
    Internal bool          `yaml:"internal,omitempty"`
    Rules    []RouteRuleV1 `yaml:"rules,omitempty"`

    // Simplified form
    Service  string `yaml:"service,omitempty"`
    Function string `yaml:"function,omitempty"`
    Port     int    `yaml:"port,omitempty"`
}
```

### V1 Transformer

```go
// pkg/schema/component/v1/transformer.go

package v1

import (
    "fmt"
    "strings"

    "github.com/davidthor/cldctl/pkg/schema/component/internal"
)

// Transformer converts v1 schema to internal representation.
type Transformer struct{}

func NewTransformer() *Transformer {
    return &Transformer{}
}

// Transform converts a v1 schema to the internal representation.
func (t *Transformer) Transform(v1 *SchemaV1) (*internal.InternalComponent, error) {
    ic := &internal.InternalComponent{
        Name:          v1.Name,
        Description:   v1.Description,
        SourceVersion: "v1",
    }

    // Transform databases
    for name, db := range v1.Databases {
        idb, err := t.transformDatabase(name, db)
        if err != nil {
            return nil, fmt.Errorf("database %s: %w", name, err)
        }
        ic.Databases = append(ic.Databases, idb)
    }

    // Transform buckets
    for name, b := range v1.Buckets {
        ib := t.transformBucket(name, b)
        ic.Buckets = append(ic.Buckets, ib)
    }

    // Transform deployments
    for name, dep := range v1.Deployments {
        idep, err := t.transformDeployment(name, dep)
        if err != nil {
            return nil, fmt.Errorf("deployment %s: %w", name, err)
        }
        ic.Deployments = append(ic.Deployments, idep)
    }

    // Transform functions
    for name, fn := range v1.Functions {
        ifn, err := t.transformFunction(name, fn)
        if err != nil {
            return nil, fmt.Errorf("function %s: %w", name, err)
        }
        ic.Functions = append(ic.Functions, ifn)
    }

    // Transform services
    for name, svc := range v1.Services {
        isvc := t.transformService(name, svc)
        ic.Services = append(ic.Services, isvc)
    }

    // Transform routes
    for name, rt := range v1.Routes {
        irt, err := t.transformRoute(name, rt)
        if err != nil {
            return nil, fmt.Errorf("route %s: %w", name, err)
        }
        ic.Routes = append(ic.Routes, irt)
    }

    // Transform cronjobs
    for name, cj := range v1.Cronjobs {
        icj, err := t.transformCronjob(name, cj)
        if err != nil {
            return nil, fmt.Errorf("cronjob %s: %w", name, err)
        }
        ic.Cronjobs = append(ic.Cronjobs, icj)
    }

    // Transform variables
    for name, v := range v1.Variables {
        iv := t.transformVariable(name, v)
        ic.Variables = append(ic.Variables, iv)
    }

    // Transform dependencies
    for name, d := range v1.Dependencies {
        id := t.transformDependency(name, d)
        ic.Dependencies = append(ic.Dependencies, id)
    }

    return ic, nil
}

func (t *Transformer) transformDatabase(name string, db DatabaseV1) (internal.InternalDatabase, error) {
    dbType, version, err := parseTypeVersion(db.Type)
    if err != nil {
        return internal.InternalDatabase{}, err
    }

    idb := internal.InternalDatabase{
        Name:    name,
        Type:    dbType,
        Version: version,
    }

    if db.Migrations != nil {
        idb.Migrations = &internal.InternalMigrations{
            Image:       db.Migrations.Image,
            Command:     db.Migrations.Command,
            Environment: db.Migrations.Environment,
        }

        if db.Migrations.Build != nil {
            idb.Migrations.Build = &internal.InternalBuild{
                Context:    db.Migrations.Build.Context,
                Dockerfile: defaultString(db.Migrations.Build.Dockerfile, "Dockerfile"),
            }
        }
    }

    return idb, nil
}

func (t *Transformer) transformDeployment(name string, dep DeploymentV1) (internal.InternalDeployment, error) {
    idep := internal.InternalDeployment{
        Name:       name,
        Image:      dep.Image,
        Command:    dep.Command,
        Entrypoint: dep.Entrypoint,
        CPU:        dep.CPU,
        Memory:     dep.Memory,
        Replicas:   defaultInt(dep.Replicas, 1),
    }

    if dep.Build != nil {
        idep.Build = &internal.InternalBuild{
            Context:    dep.Build.Context,
            Dockerfile: defaultString(dep.Build.Dockerfile, "Dockerfile"),
            Target:     dep.Build.Target,
            Args:       dep.Build.Args,
        }
    }

    // Transform environment with expression detection
    idep.Environment = make(map[string]internal.Expression)
    for k, v := range dep.Environment {
        idep.Environment[k] = internal.Expression{
            Raw:        v,
            IsTemplate: strings.Contains(v, "${{"),
        }
    }

    // Transform volumes
    for _, vol := range dep.Volumes {
        idep.Volumes = append(idep.Volumes, internal.InternalVolume{
            MountPath: vol.MountPath,
            HostPath:  vol.HostPath,
        })
    }

    // Transform probes
    if dep.LivenessProbe != nil {
        idep.LivenessProbe = t.transformProbe(dep.LivenessProbe)
    }
    if dep.ReadinessProbe != nil {
        idep.ReadinessProbe = t.transformProbe(dep.ReadinessProbe)
    }

    return idep, nil
}

// parseTypeVersion parses "postgres:^15" into ("postgres", "^15")
func parseTypeVersion(typeSpec string) (string, string, error) {
    parts := strings.SplitN(typeSpec, ":", 2)
    if len(parts) == 1 {
        return parts[0], "", nil
    }
    return parts[0], parts[1], nil
}

func defaultString(val, def string) string {
    if val == "" {
        return def
    }
    return val
}

func defaultInt(val, def int) int {
    if val == 0 {
        return def
    }
    return val
}
```

## Adding a New Schema Version

When the specification needs breaking changes, create a new version:

### Step 1: Create Version Package

```go
// pkg/schema/component/v2/schema.go

package v2

// SchemaV2 represents the v2 component schema.
type SchemaV2 struct {
    Version string `yaml:"version"`  // Now required in v2
    Name    string `yaml:"name"`
    // ...

    // New in v2: Resource groups for organization
    ResourceGroups map[string]ResourceGroupV2 `yaml:"resourceGroups,omitempty"`

    // Changed in v2: databases becomes compute.databases
    Compute ComputeV2 `yaml:"compute"`
}

// ResourceGroupV2 allows organizing resources.
type ResourceGroupV2 struct {
    Description string                `yaml:"description"`
    Databases   map[string]DatabaseV2 `yaml:"databases,omitempty"`
    // ...
}
```

### Step 2: Create Transformer

```go
// pkg/schema/component/v2/transformer.go

package v2

import "github.com/davidthor/cldctl/pkg/schema/component/internal"

type Transformer struct{}

func (t *Transformer) Transform(v2 *SchemaV2) (*internal.InternalComponent, error) {
    ic := &internal.InternalComponent{
        Name:          v2.Name,
        SourceVersion: "v2",
    }

    // Handle v2-specific transformations
    // Resource groups are flattened into the internal representation
    for _, group := range v2.ResourceGroups {
        for name, db := range group.Databases {
            idb, err := t.transformDatabase(name, db)
            if err != nil {
                return nil, err
            }
            ic.Databases = append(ic.Databases, idb)
        }
    }

    // Also handle top-level compute resources (if allowed in v2)
    // ...

    return ic, nil
}
```

### Step 3: Add Version Detection

```go
// pkg/schema/component/loader.go

func (l *versionDetectingLoader) detectVersion(data []byte) (string, error) {
    // First, try to parse the version field
    var versionOnly struct {
        Version string `yaml:"version"`
    }
    if err := yaml.Unmarshal(data, &versionOnly); err == nil && versionOnly.Version != "" {
        return versionOnly.Version, nil
    }

    // Heuristic detection
    var probe struct {
        ResourceGroups interface{} `yaml:"resourceGroups"`
        Compute        interface{} `yaml:"compute"`
    }
    yaml.Unmarshal(data, &probe)

    // v2 uses resourceGroups or compute blocks
    if probe.ResourceGroups != nil || probe.Compute != nil {
        return "v2", nil
    }

    // Default to v1
    return "v1", nil
}
```

### Step 4: Register in Loader

```go
// pkg/schema/component/loader.go

func NewLoader() Loader {
    return &versionDetectingLoader{
        parsers: map[string]Parser{
            "v1": v1.NewParser(),
            "v2": v2.NewParser(),
        },
        transformers: map[string]Transformer{
            "v1": v1.NewTransformer(),
            "v2": v2.NewTransformer(),
        },
        defaultVersion: "v1",  // Keep v1 as default for compatibility
    }
}
```

## Migration Tooling

### Schema Upgrade Command

Provide tooling to upgrade configurations:

```bash
# Check current version and suggest upgrades
cldctl component check-version ./cld.yml

# Upgrade to latest version
cldctl component upgrade ./cld.yml --to v2

# Preview upgrade without modifying file
cldctl component upgrade ./cld.yml --to v2 --dry-run
```

### Upgrade Implementation

```go
// internal/cli/component/upgrade.go

func upgradeComponent(path, targetVersion string, dryRun bool) error {
    // Load with current version
    loader := component.NewLoader()
    comp, err := loader.Load(path)
    if err != nil {
        return err
    }

    currentVersion := comp.SchemaVersion()
    if currentVersion == targetVersion {
        fmt.Printf("Component is already at version %s\n", targetVersion)
        return nil
    }

    // Get the target version serializer
    serializer, err := getSerializer(targetVersion)
    if err != nil {
        return fmt.Errorf("unsupported target version: %s", targetVersion)
    }

    // Serialize internal representation to target version
    output, warnings, err := serializer.Serialize(comp)
    if err != nil {
        return fmt.Errorf("upgrade failed: %w", err)
    }

    // Print warnings about lossy conversions
    for _, w := range warnings {
        fmt.Fprintf(os.Stderr, "Warning: %s\n", w)
    }

    if dryRun {
        fmt.Println(string(output))
        return nil
    }

    // Backup original
    backupPath := path + ".bak"
    if err := copyFile(path, backupPath); err != nil {
        return fmt.Errorf("failed to create backup: %w", err)
    }

    // Write upgraded file
    if err := os.WriteFile(path, output, 0644); err != nil {
        return fmt.Errorf("failed to write upgraded file: %w", err)
    }

    fmt.Printf("Upgraded %s from %s to %s\n", path, currentVersion, targetVersion)
    fmt.Printf("Backup saved to %s\n", backupPath)

    return nil
}
```

## Deprecation Policy

### Deprecation Timeline

1. **Announce**: Deprecate version in release notes
2. **Warn**: Emit warnings when deprecated version is used
3. **Remove**: Remove support after 2 major cldctl releases

### Deprecation Warnings

```go
// pkg/schema/component/loader.go

var deprecatedVersions = map[string]string{
    "v1": "v1 is deprecated and will be removed in cldctl 3.0. Run 'cldctl component upgrade' to migrate to v2.",
}

func (l *versionDetectingLoader) Load(path string) (Component, error) {
    version, err := l.detectVersion(data)
    if err != nil {
        return nil, err
    }

    // Emit deprecation warning
    if warning, deprecated := deprecatedVersions[version]; deprecated {
        fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
    }

    // Continue loading...
}
```

## Best Practices

### When to Create a New Version

Create a new major version when:

1. **Removing fields**: A field is being removed without replacement
2. **Changing semantics**: A field's meaning changes significantly
3. **Restructuring**: Major reorganization of the schema
4. **New required fields**: Adding required fields with no default

### Avoid New Versions For

1. **Adding optional fields**: Can be done within existing version
2. **Adding new resource types**: Extend existing version
3. **Changing defaults**: Document the change, keep version

### Internal Representation Stability

1. **Additive only**: Only add fields to internal types
2. **Use optional fields**: New fields should be pointers or have zero-value defaults
3. **Avoid renames**: Keep field names stable

```go
// Good: Adding a new optional field
type InternalDeployment struct {
    // Existing fields...

    // New in cldctl 2.1
    Sidecars []InternalSidecar  // Zero value is empty slice
}

// Bad: Renaming a field (breaks existing code)
type InternalDeployment struct {
    // Renamed from LivenessProbe - DON'T DO THIS
    HealthCheck *InternalProbe
}
```
