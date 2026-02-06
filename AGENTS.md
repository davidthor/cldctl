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
# Build commands (tag is optional; omit -t to identify by digest)
arcctl build component ./my-app -t ghcr.io/myorg/app:v1
arcctl build component ./my-app                           # digest-only
arcctl build datacenter ./dc -t ghcr.io/myorg/dc:v1
arcctl build datacenter ./dc                              # digest-only

# Deploy commands
arcctl deploy component ./my-app -e production -d my-datacenter
arcctl deploy datacenter my-dc ./datacenter

# Environment management
arcctl create environment staging -d my-datacenter
arcctl list environment -d my-datacenter
arcctl update environment staging environment.yml -d my-datacenter
arcctl destroy environment staging -d my-datacenter

# Resource management
arcctl list component -e staging -d my-datacenter
arcctl get component my-app -e production -d my-datacenter
arcctl destroy component my-app -e staging -d my-datacenter
arcctl destroy component shared-db -e staging -d my-datacenter --force  # Override dependency check

# CLI configuration
arcctl config set default_datacenter my-datacenter  # Set default datacenter
arcctl config get default_datacenter                 # Get default datacenter
arcctl config list                                   # List all config values

# State migration (from old flat structure to new nested hierarchy)
arcctl migrate state

# Artifact management
arcctl images                                              # List all cached artifacts
arcctl images --type component                             # Filter by type
arcctl tag component ghcr.io/myorg/app:v1 ghcr.io/myorg/app:latest
arcctl push component ghcr.io/myorg/app:v1
arcctl pull component ghcr.io/myorg/app:v1
arcctl pull datacenter docker.io/davidthor/startup-datacenter:latest

# Validation
arcctl validate component ./my-app
arcctl validate datacenter ./dc
arcctl validate environment ./env.yml

# Logs and observability
arcctl logs -e staging -d my-datacenter           # All logs in the environment
arcctl logs -e staging my-app                     # Logs from one component (uses default DC)
arcctl logs -e staging my-app/deployment          # All deployments in a component
arcctl logs -e staging my-app/deployment/api      # A specific deployment
arcctl logs -e staging -f                         # Stream logs in real-time
arcctl logs -e staging --since 5m                 # Logs from the last 5 minutes
arcctl observability dashboard -e staging         # Open observability UI in browser
```

Aliases: `comp` for `component`, `dc` for `datacenter`, `env` for `environment`, `ls` for `list`, `obs` for `observability`

### Datacenter Resolution

All environment-scoped commands require a datacenter to be specified. The datacenter is resolved in this order:
1. `--datacenter / -d` flag on the command
2. `ARCCTL_DATACENTER` environment variable
3. `default_datacenter` in `~/.arcctl/config.yaml` (auto-set on `arcctl deploy datacenter`)

This means after deploying a datacenter once, subsequent commands can omit `-d`:
```bash
arcctl deploy datacenter my-dc ./datacenter  # auto-sets default_datacenter
arcctl create environment staging            # uses my-dc from config
arcctl deploy component ./my-app -e staging  # uses my-dc from config
```

### Key Directories
| Path | Purpose |
|------|---------|
| `cmd/arcctl/` | CLI entry point |
| `internal/cli/` | Cobra command implementations |
| `pkg/schema/` | YAML/HCL config parsing with versioned schemas |
| `pkg/state/backend/` | Pluggable state backends (local, s3, gcs, azurerm) |
| `pkg/engine/` | Execution engine (graph, planner, executor, expressions) |
| `pkg/iac/` | IaC plugins (native, pulumi, opentofu) |
| `pkg/logs/` | Log query plugin system (querier interface, Loki adapter) |
| `pkg/errors/` | Structured error types |
| `testdata/` | Test fixtures |
| `examples/` | Example component configurations |
| `official-templates/` | Official datacenter templates (local, startup, do-k8s, do-app-platform, do-vms, aws-ecs, aws-lambda, aws-k8s, aws-vms, gcp-cloud-run, gcp-k8s, gcp-vms) |

## Component Authoring (architect.yml)

Components describe application requirements using YAML with `${{ }}` expressions.
Component names are determined by the OCI tag at build time (e.g., `ghcr.io/org/my-app:v1`).
If a `README.md` exists in the component directory, it's bundled into the artifact for documentation.

### Functions vs Deployments

Use **functions** for:
- **Next.js applications** (always use `framework: nextjs`)
- Serverless workloads with variable traffic
- Applications that benefit from scale-to-zero
- Request/response oriented services

Use **deployments** for:
- Long-running services (workers, background processors)
- Stateful applications requiring persistent connections
- Applications with specific replica requirements

### Next.js Application (Recommended Pattern)

```yaml
# architect.yml - Next.js apps should use functions
# Routes can point directly to functions - no service wrapper needed
databases:
  main:
    type: postgres:^16

functions:
  web:
    src:
      path: .
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

### Traditional Deployment Pattern

```yaml
# architect.yml - For long-running services with Docker builds
builds:
  api:
    context: ./api

deployments:
  api:
    image: ${{ builds.api.image }}
    environment:
      DATABASE_URL: ${{ databases.main.url }}

services:
  api:
    deployment: api
    port: 8080
```

### Dev/Prod with Extends

```yaml
# architect.yml (dev base - process-based, no Docker)
deployments:
  api:
    command: ["npm", "run", "dev"]
    workingDirectory: ./backend  # optional, defaults to architect.yml dir
    environment:
      DATABASE_URL: ${{ databases.main.url }}
```

```yaml
# architect.prod.yml (production - extends dev, adds Docker build)
extends: ./architect.yml

builds:
  api:
    context: .

deployments:
  api:
    image: ${{ builds.api.image }}
    command: ["npm", "start"]
```

### VM-based Deployment with Runtime

Use `runtime` to declare language and system requirements for VM-based deployments.
Datacenters handle provisioning the actual VM (EC2, Droplet, GCE, etc.).

```yaml
# String shorthand
deployments:
  worker:
    runtime: node:20
    command: ["node", "dist/worker.js"]
```

```yaml
# Full object form with system dependencies
deployments:
  worker:
    runtime:
      language: node:20          # Required. Language and version
      os: linux                  # Optional. Default: linux (linux, windows)
      arch: amd64                # Optional. Default: datacenter's choice (amd64, arm64)
      packages:                  # Optional. System-level dependencies
        - ffmpeg
        - imagemagick
      setup:                     # Optional. Provisioning commands
        - npm ci --production
    command: ["node", "dist/worker.js"]
    cpu: "2"
    memory: "4Gi"
    replicas: 5
```

### Observability (OpenTelemetry)

Components can declare observability preferences using the optional `observability` block.
When a datacenter provides an `observability` hook, the hook's outputs become available
via expressions so component authors can wire them into workload environment variables.

There are two modes: **expression-only** (default) and **auto-inject**.

```yaml
# Boolean shorthand - enable with all defaults (expression-only)
observability: true

# Full object form - customize attributes and injection mode
observability:
  inject: false     # default: false (expression-only mode)
  attributes:       # custom OTel resource attributes
    team: payments
    tier: critical

# Auto-inject mode - engine injects OTEL_* env vars into all workloads
observability:
  inject: true

# Disable entirely
observability: false

# Omit = enabled with defaults when datacenter supports it
```

#### Expression-Only Mode (default, `inject: false`)

Component authors explicitly reference observability outputs in their environment.
This gives full control over which OTEL env vars are set and what values they use:

```yaml
observability:
  attributes:
    team: backend

deployments:
  api:
    image: ${{ builds.api.image }}
    environment:
      OTEL_EXPORTER_OTLP_ENDPOINT: ${{ observability.endpoint }}
      OTEL_EXPORTER_OTLP_PROTOCOL: ${{ observability.protocol }}
      OTEL_RESOURCE_ATTRIBUTES: ${{ observability.attributes }}
      OTEL_SERVICE_NAME: my-api
```

The `${{ observability.attributes }}` expression returns a comma-separated `key=value`
string that merges three sources (later values override earlier ones):
1. **Auto-generated**: `service.namespace=<component>`, `deployment.environment=<env>`
2. **Datacenter hook** `attributes` output (e.g., `cloud.provider=aws,cloud.region=us-east-1`)
3. **Component** `attributes` (from the `observability` block)

#### Auto-Inject Mode (`inject: true`)

When `inject: true`, the engine automatically injects standard OTEL_* environment
variables into all workloads (deployments, functions, cronjobs). This is convenient
for teams that want convention-based configuration with minimal boilerplate:

```yaml
observability:
  inject: true
  attributes:
    team: backend

deployments:
  api:
    image: ${{ builds.api.image }}
    environment:
      DATABASE_URL: ${{ databases.main.url }}
      # No need to set OTEL_* vars -- they're injected automatically
```

Auto-injected variables (never overwrites component-declared values):

| Variable | Value |
|----------|-------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | From datacenter hook output |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | From datacenter hook output |
| `OTEL_SERVICE_NAME` | `<component>-<workload>` |
| `OTEL_LOGS_EXPORTER` | `otlp` |
| `OTEL_TRACES_EXPORTER` | `otlp` |
| `OTEL_METRICS_EXPORTER` | `otlp` |
| `OTEL_RESOURCE_ATTRIBUTES` | Merged attributes (auto-generated + datacenter + component) |

Component-declared env vars always take precedence -- the engine never overwrites
a value the component author explicitly set.

### Available Expression References
- `builds.<name>.image` (built Docker image)
- `databases.<name>.url|host|port|username|password|database`
- `buckets.<name>.endpoint|bucket|accessKeyId|secretAccessKey`
- `encryptionKeys.<name>.privateKey|publicKey|privateKeyBase64|publicKeyBase64|key|keyBase64`
- `smtp.<name>.host|port|username|password`
- `services.<name>.url|host|port`
- `observability.endpoint|protocol|attributes` (OTel config; attributes merges datacenter + component + auto-generated)
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
    when = element(split(":", node.inputs.type), 0) == "postgres"
    
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
| `observability` | `endpoint`, `protocol`, `attributes`; optional: `query_type`, `query_endpoint`, `dashboard_url` |

### Error Hooks

Hooks can reject unsupported configurations with the `error` attribute instead of provisioning resources. When a hook's `when` condition matches and it has `error`, deployment is blocked with the error message.

```hcl
# Reject specific types
database {
  when = element(split(":", node.inputs.type), 0) == "mongodb"
  error = "MongoDB is not supported. Use postgres or redis instead."
}

# Catch-all (no when = always matches, must be last)
database {
  error = "Unsupported database type '${node.inputs.type}'. Supported: postgres, redis."
}
```

Rules:
- `error` is mutually exclusive with `module` blocks and `outputs`
- Error messages support HCL interpolation (`${node.inputs.type}`, `${node.component}`, etc.)
- Hooks are evaluated in source order; first match wins
- A hook without `when` must be the last of its type (subsequent hooks are unreachable)

### Hook Evaluation Order

**Only one hook per resource type is executed for a given resource.** Hooks use waterfall-style evaluation: they are checked top-to-bottom in source order, and the **first** hook whose `when` condition matches wins. All remaining hooks of that type are skipped entirely for that resource. This is like a switch/case or if/else-if chain -- order matters. A hook without a `when` condition always matches and acts as a catch-all (must be last).

### Hook Expression Context
- `variable.<name>` - Datacenter variables
- `environment.name` - Current environment name
- `node.name|type|component|inputs.<field>` - Current resource info
- `module.<name>.<output>` - Module outputs

For `observability` hooks, `node.inputs` includes: `inject`, `attributes`

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
7. Create new reference page in `docs/components/<resource>.mdx`
8. Add to navigation in `docs/docs.json`
9. Update `docs/components/overview.mdx` with the new resource type
10. Add examples in `examples/components/` if applicable
11. Update `AGENTS.md` (Available Expression References, Component Authoring sections)
12. Update `.cursor/rules/components.mdc` with the new resource type patterns

### New Datacenter Hook
1. `pkg/schema/datacenter/internal/types.go` - Define inputs/outputs
2. `pkg/schema/datacenter/v1/types.go` - Add to environment block
3. `pkg/engine/executor/hooks.go` - Implement execution
4. Create new reference page in `docs/datacenters/<hook>-hook.mdx`
5. Add to navigation in `docs/docs.json`
6. Update `docs/datacenters/overview.mdx` with the new hook type
7. Add or update official templates in `official-templates/` if applicable
8. Update `AGENTS.md` (Hook Types & Required Outputs, Datacenter Authoring sections)
9. Update `.cursor/rules/datacenters.mdc` with the new hook type

### New CLI Command
1. Create command in `internal/cli/`
2. Register in parent command
3. Create new reference page in `docs/cli/<action>/<resource>.mdx`
4. Add to navigation in `docs/docs.json`
5. Update `docs/cli/overview.mdx` with the new command
6. Update `AGENTS.md` CLI Command Structure section

### New State Backend
1. Create `pkg/state/backend/<name>/<name>.go`
2. Implement `Backend` interface (Read, Write, Delete, List, Exists, Lock)
3. Register in `pkg/state/backend/registry.go`
4. Update `docs/advanced/state-backends.mdx`

### New IaC Plugin
1. Create `pkg/iac/<name>/<name>.go`
2. Implement `Plugin` interface (Preview, Apply, Destroy, Refresh)
3. Register in `pkg/iac/registry.go`
4. Update `docs/advanced/iac-plugins.mdx`

## Documentation

- `ARCHITECTURE.md` - Detailed system architecture
- `CONTRIBUTING.md` - Contribution guidelines with examples
- `SPEC_VERSIONING.md` - Schema versioning strategy
- `docs/` - User documentation (Mintlify format)

### Documentation Structure

| Directory | Content |
|-----------|---------|
| `docs/components/` | Component schema reference (one page per resource type: databases, deployments, functions, etc.) |
| `docs/datacenters/` | Datacenter schema reference (one page per hook type, plus expressions, variables, modules) |
| `docs/cli/` | CLI command reference (one page per command, organized by action) |
| `docs/environments/` | Environment configuration reference |
| `docs/guides/components/` | Step-by-step component authoring guides (Next.js, microservices, etc.) |
| `docs/guides/datacenters/` | Step-by-step datacenter authoring guides (local Docker, AWS ECS, etc.) |
| `docs/concepts/` | High-level conceptual documentation |
| `examples/` | Example component configurations |
| `official-templates/` | Official datacenter templates (12 templates across Local, Startup/Vercel, DigitalOcean, AWS, GCP) |

### Keeping Documentation In Sync (IMPORTANT)

**Whenever you make changes to the component schema, datacenter schema, or CLI commands, you MUST also update all relevant documentation.** Documentation changes are not optional — they are part of completing the feature or fix.

#### Component Schema Changes
When modifying anything in `pkg/schema/component/`:
1. Update the relevant reference page(s) in `docs/components/` (e.g., adding a field to deployments → update `docs/components/deployments.mdx`)
2. Update any affected guides in `docs/guides/components/` that demonstrate the changed feature
3. Update example configurations in `examples/components/` if they use the changed feature
4. Update `AGENTS.md` sections (Component Authoring, Available Expression References) if the change affects component authoring guidance or expressions
5. Update `.cursor/rules/components.mdc` if the change affects component authoring patterns

#### Datacenter Schema Changes
When modifying anything in `pkg/schema/datacenter/`:
1. Update the relevant hook reference page(s) in `docs/datacenters/` (e.g., adding an output to the database hook → update `docs/datacenters/database-hook.mdx`)
2. Update any affected guides in `docs/guides/datacenters/` that demonstrate the changed feature
3. Update official templates in `official-templates/` if they use the changed feature
4. Update `AGENTS.md` sections (Datacenter Authoring, Hook Types & Required Outputs) if the change affects datacenter authoring guidance
5. Update `.cursor/rules/datacenters.mdc` if the change affects datacenter authoring patterns

#### CLI Command Changes
When modifying CLI commands in `internal/cli/`:
1. Update the relevant command reference page in `docs/cli/` (e.g., adding a flag to `deploy component` → update `docs/cli/deploy/component.mdx`)
2. Update `docs/cli/overview.mdx` if the change affects the command listing or general CLI usage
3. Update any guides in `docs/guides/` that reference the changed command
4. Update `AGENTS.md` CLI Command Structure section if the change affects the command listing

#### Environment Schema Changes
When modifying anything in `pkg/schema/environment/`:
1. Update the relevant reference page(s) in `docs/environments/`
2. Update any affected guides that demonstrate environment configuration

#### General Rules
- If you add a new resource type or hook, create a new reference page in the appropriate `docs/` subdirectory
- If you add a new CLI command, create a new reference page in `docs/cli/` and add it to `docs/docs.json`
- Always check `docs/docs.json` to ensure navigation is updated when adding or removing pages
- Update `examples/` when adding features that benefit from example configurations
- When in doubt, search `docs/` for mentions of the feature you're changing to find all pages that may need updates
