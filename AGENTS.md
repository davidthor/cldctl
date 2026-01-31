# AI Agent Instructions for arcctl

This file provides guidance for AI coding assistants working on the arcctl codebase.

## Project Overview

arcctl is a Go CLI tool for deploying portable cloud-native applications. The architecture separates concerns between:

1. **Components** (`architect.yml`) - Developer-focused application definitions
2. **Datacenters** (`datacenter.dc`) - Platform engineer infrastructure templates  
3. **Environments** - Deployed instances combining components with datacenters

## Quick Reference

### Build & Test Commands
```bash
make build    # Build the CLI binary
make test     # Run unit tests
make lint     # Run golangci-lint
go mod tidy   # Clean up dependencies
```

### CLI Command Structure
arcctl uses an action-first command structure: `arcctl <action> <resource> [args] [flags]`

```bash
# Build commands
arcctl build component ./my-app -t ghcr.io/myorg/app:v1
arcctl build datacenter ./dc -t ghcr.io/myorg/dc:v1

# Deploy commands
arcctl deploy component ./my-app -e production
arcctl deploy datacenter my-dc ./datacenter

# Environment management
arcctl create environment staging -d my-datacenter
arcctl list environment
arcctl update environment staging environment.yml
arcctl destroy environment staging

# Resource management
arcctl list component -e staging
arcctl get component my-app -e production
arcctl destroy component my-app -e staging

# Artifact management
arcctl tag component ghcr.io/myorg/app:v1 ghcr.io/myorg/app:latest
arcctl push component ghcr.io/myorg/app:v1
arcctl pull component ghcr.io/myorg/app:v1

# Validation
arcctl validate component ./my-app
arcctl validate datacenter ./dc
arcctl validate environment ./env.yml
```

Aliases: `comp` for `component`, `dc` for `datacenter`, `env` for `environment`, `ls` for `list`

### Key Directories
| Path | Purpose |
|------|---------|
| `cmd/arcctl/` | CLI entry point |
| `internal/cli/` | Cobra command implementations |
| `pkg/schema/` | YAML/HCL config parsing with versioned schemas |
| `pkg/state/backend/` | Pluggable state backends (local, s3, gcs, azurerm) |
| `pkg/engine/` | Execution engine (graph, planner, executor, expressions) |
| `pkg/iac/` | IaC plugins (native, pulumi, opentofu) |
| `pkg/errors/` | Structured error types |
| `testdata/` | Test fixtures |
| `examples/` | Example configurations |

## Component Authoring (architect.yml)

Components describe application requirements using YAML with `${{ }}` expressions.
Component names are determined by the OCI tag at build time (e.g., `ghcr.io/org/my-app:v1`).
If a `README.md` exists in the component directory, it's bundled into the artifact for documentation.

```yaml
# architect.yml
databases:
  main:
    type: postgres:^15

deployments:
  api:
    build:
      context: ./api
    environment:
      DATABASE_URL: ${{ databases.main.url }}

services:
  api:
    deployment: api
    port: 8080
```

### Available Expression References
- `databases.<name>.url|host|port|username|password|database`
- `buckets.<name>.endpoint|bucket|accessKeyId|secretAccessKey`
- `encryptionKeys.<name>.privateKey|publicKey|privateKeyBase64|publicKeyBase64|key|keyBase64`
- `smtp.<name>.host|port|username|password`
- `services.<name>.url|host|port`
- `variables.<name>`
- `dependencies.<name>.<output>`
- `dependents.*.<output>` (for dependent components)

## Datacenter Authoring (datacenter.dc)

Datacenters define infrastructure using HCL with hooks for each resource type:

```hcl
variable "region" {
  type    = string
  default = "us-east-1"
}

environment {
  database {
    when = node.inputs.databaseType == "postgres"
    
    module "postgres" {
      plugin = "native"
      build  = "./modules/docker-postgres"
      inputs = {
        name = "${environment.name}-${node.component}-${node.name}"
      }
    }
    
    outputs = {
      url = module.postgres.url
    }
  }
}
```

### Hook Types & Required Outputs
| Hook | Required Outputs |
|------|-----------------|
| `database` | `host`, `port`, `url`, `username`, `password` |
| `bucket` | `endpoint`, `bucket`, `accessKeyId`, `secretAccessKey` |
| `encryptionKey` | RSA/ECDSA: `privateKey`, `publicKey`, `privateKeyBase64`, `publicKeyBase64`; Symmetric: `key`, `keyBase64` |
| `smtp` | `host`, `port`, `username`, `password` |
| `deployment` | `id` |
| `function` | `id`, `endpoint` |
| `service` | `host`, `port`, `url` |
| `route` | `url`, `host`, `port` |

### Hook Expression Context
- `variable.<name>` - Datacenter variables
- `environment.name` - Current environment name
- `node.name|component|inputs.<field>` - Current resource info
- `module.<name>.<output>` - Module outputs

## Go Code Conventions

### Error Handling
Use structured errors from `pkg/errors`:
```go
return errors.ValidationError("invalid type", map[string]interface{}{
    "type": value,
    "supported": []string{"a", "b"},
})
```

### Interface Design
Define small interfaces where used:
```go
type StateReader interface {
    Read(ctx context.Context, path string) (io.ReadCloser, error)
}
```

### Testing Pattern
Use table-driven tests with testify:
```go
func TestParse(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr bool
    }{
        {"valid", "...", false},
        {"invalid", "...", true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := Parse(tt.input)
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

## Adding New Features

### New Component Resource Type
1. `pkg/schema/component/internal/types.go` - Add internal type
2. `pkg/schema/component/component.go` - Add public interface
3. `pkg/schema/component/v1/types.go` - Add v1 schema type
4. `pkg/schema/component/v1/transformer.go` - Add transformation
5. `pkg/schema/component/v1/validator.go` - Add validation
6. `pkg/engine/expression/evaluator.go` - Add expression support

### New Datacenter Hook
1. `pkg/schema/datacenter/internal/types.go` - Define inputs/outputs
2. `pkg/schema/datacenter/v1/types.go` - Add to environment block
3. `pkg/engine/executor/hooks.go` - Implement execution

### New State Backend
1. Create `pkg/state/backend/<name>/<name>.go`
2. Implement `Backend` interface (Read, Write, Delete, List, Exists, Lock)
3. Register in `pkg/state/backend/registry.go`

### New IaC Plugin
1. Create `pkg/iac/<name>/<name>.go`
2. Implement `Plugin` interface (Preview, Apply, Destroy, Refresh)
3. Register in `pkg/iac/registry.go`

## Documentation

- `ARCHITECTURE.md` - Detailed system architecture
- `CONTRIBUTING.md` - Contribution guidelines with examples
- `SPEC_VERSIONING.md` - Schema versioning strategy
- `docs/` - User documentation (Mintlify format)
