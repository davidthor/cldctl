# GitHub Copilot Instructions for arcctl

## Project Overview

arcctl is a Go CLI tool for deploying portable cloud-native applications. It uses a three-tier architecture:

- **Components** (`architect.yml`): Developer-focused application bundles describing what an app needs
- **Datacenters** (`datacenter.dc`): Platform engineer infrastructure templates defining how resources are provisioned
- **Environments**: Deployed instances of components in datacenters

## Repository Structure

```
arcctl/
├── cmd/arcctl/          # CLI entry point
├── internal/cli/        # CLI commands (Cobra)
├── pkg/
│   ├── schema/          # Config parsing (component, datacenter, environment)
│   │   ├── component/   # architect.yml parsing
│   │   │   ├── v1/      # Version-specific schema
│   │   │   └── internal/ # Internal representation
│   │   └── datacenter/  # datacenter.dc parsing
│   ├── state/           # State management with pluggable backends
│   │   └── backend/     # local, s3, gcs, azurerm
│   ├── engine/          # Execution engine
│   │   ├── graph/       # Dependency graph
│   │   ├── planner/     # Execution planning
│   │   ├── executor/    # Plan execution
│   │   └── expression/  # ${{ }} expression evaluation
│   ├── iac/             # IaC plugins (native, pulumi, opentofu)
│   └── oci/             # OCI artifact management
├── testdata/            # Test fixtures
└── examples/            # Example components and datacenters
```

## Code Conventions

### Go Style
- Follow Effective Go guidelines
- Use `gofmt` for formatting, `golangci-lint` for linting
- Packages: lowercase single words
- Interfaces: descriptive nouns (`Backend`, `Plugin`, `Loader`)
- Test functions: `Test<Function>_<Case>`

### Error Handling
Use structured errors from `pkg/errors`:

```go
// Good
return nil, errors.ValidationError("invalid database type", map[string]interface{}{
    "type": dbType,
    "supported": []string{"postgres", "mysql"},
})

// Bad
return nil, fmt.Errorf("bad type")
```

### Interface Design
Keep interfaces small and define them where used:

```go
type StateReader interface {
    Read(ctx context.Context, path string) (io.ReadCloser, error)
}
```

## Component Configuration (architect.yml)

Components use YAML with `${{ }}` expressions.

### Functions vs Deployments

Use **functions** for:
- **Next.js applications** (always use `framework: nextjs`)
- Serverless workloads with variable traffic
- Applications that benefit from scale-to-zero

Use **deployments** for:
- Long-running services (workers, background processors)
- Stateful applications requiring persistent connections

### Next.js Application (Recommended)

```yaml
# Next.js apps should use functions, not deployments
# Routes can point directly to functions - no service wrapper needed
databases:
  main:
    type: postgres:^16

functions:
  web:
    build:
      context: .
    framework: nextjs
    environment:
      DATABASE_URL: ${{ databases.main.url }}
    memory: "1024Mi"
    timeout: 30

routes:
  main:
    type: http
    function: web
```

### Traditional Deployment

```yaml
# For long-running services like workers or APIs
builds:
  api:
    context: ./api

deployments:
  api:
    image: ${{ builds.api.image }}
    environment:
      DATABASE_URL: ${{ databases.main.url }}
      LOG_LEVEL: ${{ variables.log_level }}
    cpu: "0.5"
    memory: "512Mi"
    replicas: 2

services:
  api:
    deployment: api
    port: 8080

routes:
  main:
    type: http
    rules:
      - matches:
          - path:
              type: PathPrefix
              value: /api
        backendRefs:
          - service: api

variables:
  log_level:
    default: "info"
```

### Dev/Prod with Extends

```yaml
# architect.yml (dev base - runs as a local process)
deployments:
  api:
    command: ["npm", "run", "dev"]
    environment:
      DATABASE_URL: ${{ databases.main.url }}
```

```yaml
# architect.prod.yml (production - adds Docker build)
extends: architect.yml

builds:
  api:
    context: ./api

deployments:
  api:
    image: ${{ builds.api.image }}
    command: ["npm", "start"]
```

### VM-based Deployment with Runtime

Use `runtime` to deploy on VMs (EC2, Droplets, GCE). Supports string shorthand or full object:

```yaml
deployments:
  worker:
    runtime: node:20                    # String shorthand
    command: ["node", "dist/worker.js"]
```

```yaml
deployments:
  worker:
    runtime:
      language: node:20          # Required
      os: linux                  # Optional (linux, windows)
      arch: amd64                # Optional (amd64, arm64)
      packages: [ffmpeg]         # Optional system deps
      setup: [npm ci --production] # Optional provisioning commands
    command: ["node", "dist/worker.js"]
```

### Expression References
- `builds.<name>.image`
- `databases.<name>.url|host|port|username|password`
- `buckets.<name>.endpoint|bucket|accessKeyId|secretAccessKey`
- `services.<name>.url|host|port`
- `variables.<name>`
- `dependencies.<name>.<output>`

## Datacenter Configuration (datacenter.dc)

Datacenters use HCL with hooks for each resource type:

```hcl
variable "region" {
  type    = string
  default = "us-east-1"
}

module "network" {
  plugin = "native"
  build  = "./modules/network"
  inputs = {
    name = variable.network_name
  }
}

environment {
  database {
    when = element(split(":", node.inputs.type), 0) == "postgres"
    
    module "postgres" {
      plugin = "native"
      build  = "./modules/docker-postgres"
      inputs = {
        name    = "${environment.name}-${node.component}-${node.name}"
        version = coalesce(try(element(split(":", node.inputs.type), 1), null), "16")
      }
    }
    
    outputs = {
      host     = module.postgres.host
      port     = module.postgres.port
      url      = module.postgres.url
      username = module.postgres.username
      password = module.postgres.password
    }
  }
  
  deployment {
    module "container" {
      plugin = "native"
      build  = "./modules/docker-deployment"
      inputs = merge(node.inputs, {
        name    = "${environment.name}-${node.component}-${node.name}"
        network = variable.network_name
      })
    }
    
    outputs = {
      id = module.container.container_id
    }
  }
}
```

### Hook Types
- `database` - Databases (postgres, mysql, redis)
- `task` - One-shot jobs (e.g., migrations)
- `bucket` - Object storage
- `deployment` - Container workloads
- `function` - Serverless functions
- `service` - Internal networking
- `ingress` - External routing
- `dockerBuild` - Image building
- `cronjob` - Scheduled tasks

### Expression Context in Hooks
- `variable.<name>` - Datacenter variables
- `environment.name` - Environment name
- `node.name` - Resource name
- `node.component` - Component name
- `node.inputs.<field>` - Resource inputs
- `module.<name>.<output>` - Module outputs

## Adding New Features

### New Component Resource Type
1. Add internal type: `pkg/schema/component/internal/types.go`
2. Add public interface: `pkg/schema/component/component.go`
3. Add v1 schema: `pkg/schema/component/v1/types.go`
4. Update transformer: `pkg/schema/component/v1/transformer.go`
5. Add validation: `pkg/schema/component/v1/validator.go`
6. Update expression evaluator: `pkg/engine/expression/evaluator.go`

### New Datacenter Hook
1. Define inputs/outputs: `pkg/schema/datacenter/internal/types.go`
2. Add to schema: `pkg/schema/datacenter/v1/types.go`
3. Update executor: `pkg/engine/executor/hooks.go`

### New State Backend
1. Create package: `pkg/state/backend/<name>/<name>.go`
2. Implement `Backend` interface
3. Register in `pkg/state/backend/registry.go`

### New IaC Plugin
1. Create package: `pkg/iac/<name>/<name>.go`
2. Implement `Plugin` interface
3. Register in `pkg/iac/registry.go`

## Testing

```bash
make build    # Build CLI
make test     # Run unit tests
make lint     # Run linter
```

Use table-driven tests with testify:

```go
func TestParser_Parse(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    *Result
        wantErr bool
    }{
        {"valid", "...", &Result{}, false},
        {"invalid", "...", nil, true},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := Parse(tt.input)
            if tt.wantErr {
                assert.Error(t, err)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.want, result)
        })
    }
}
```

## Key Documentation

- `ARCHITECTURE.md` - System architecture details
- `CONTRIBUTING.md` - Contribution guidelines
- `SPEC_VERSIONING.md` - Schema versioning strategy
- `STATE_BACKENDS.md` - State backend implementation guide
- `IAC_PLUGINS.md` - IaC plugin implementation guide
- `docs/` - User documentation (Mintlify)
