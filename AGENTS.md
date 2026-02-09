# AI Agent Instructions for cldctl

This file provides guidance for AI coding assistants working on the cldctl codebase.

## Project Overview

cldctl is a Go CLI tool for deploying portable cloud-native applications. The architecture separates concerns between:

1. **Components** (`cloud.component.yml`) - Developer-focused application definitions
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
cldctl uses an action-first command structure: `cldctl <action> <resource> [args] [flags]`

```bash
# Build commands (tag is optional; omit -t to identify by digest)
cldctl build component ./my-app -t ghcr.io/myorg/app:v1
cldctl build component ./my-app                           # digest-only
cldctl build datacenter ./dc -t ghcr.io/myorg/dc:v1
cldctl build datacenter ./dc                              # digest-only

# Deploy commands
cldctl deploy component myorg/myapp:v1 -e production -d my-datacenter
cldctl deploy component myorg/stripe:latest -d my-dc --var key=secret  # datacenter-level component (no -e)
cldctl deploy datacenter local davidthor/local-datacenter
cldctl deploy datacenter prod-dc ghcr.io/myorg/dc:v1.0.0
cldctl deploy datacenter prod-dc ghcr.io/myorg/dc:v1.0.0 --import-file import.yml  # adopt existing infra during deploy

# Environment management
cldctl create environment staging -d my-datacenter
cldctl list environment -d my-datacenter
cldctl update environment staging environment.yml -d my-datacenter
cldctl destroy environment staging -d my-datacenter

# Resource management
cldctl list component -e staging -d my-datacenter
cldctl get component my-app -e production -d my-datacenter
cldctl destroy component my-app -e staging -d my-datacenter
cldctl destroy component shared-db -e staging -d my-datacenter --force  # Override dependency check
cldctl destroy component myorg/stripe -d my-datacenter                  # Remove datacenter-level component (no -e)

# CLI configuration
cldctl config set default_datacenter my-datacenter  # Set default datacenter
cldctl config get default_datacenter                 # Get default datacenter
cldctl config list                                   # List all config values

# State migration (from old flat structure to new nested hierarchy)
cldctl migrate state

# Artifact management
cldctl images                                              # List all cached artifacts
cldctl images --type component                             # Filter by type
cldctl tag component ghcr.io/myorg/app:v1 ghcr.io/myorg/app:latest
cldctl push component ghcr.io/myorg/app:v1
cldctl pull component ghcr.io/myorg/app:v1
cldctl pull datacenter docker.io/davidthor/startup-datacenter:latest

# Validation
cldctl validate component ./my-app
cldctl validate datacenter ./dc
cldctl validate environment ./env.yml

# Inspect deployed state
cldctl inspect staging                               # Environment details
cldctl inspect staging/my-app                        # Component details
cldctl inspect staging/my-app/api                    # Resource details (inputs, env vars, outputs)
cldctl inspect staging/my-app/deployment/api         # Disambiguate by type
cldctl inspect staging/my-app/api -o json            # JSON output

# Inspect component topology (not deployed state)
cldctl inspect component ./my-app                    # Visualize resource graph
cldctl inspect component ./my-app --expand           # Include dependencies

# Audit templates (not deployed state — for building import mapping files)
cldctl audit datacenter ./my-dc                      # Show hooks, modules, variables
cldctl audit datacenter ghcr.io/myorg/dc:v1 --modules  # Show IaC resource addresses for import
cldctl audit component ./my-app                      # Show resource keys and dependencies

# Local development (up command)
cldctl up                                         # Auto-detect cloud.component.yml or cloud.environment.yml in CWD
cldctl up -c ./my-app -d local                    # Component mode: deploy single component
cldctl up -e cloud.environment.yml -d local       # Environment mode: deploy all components from env file
cldctl up -c ./my-app --name my-feature -d local  # Named environment
cldctl up -e ./envs/dev.yml --var key=secret      # Environment mode with variable overrides
cldctl up --detach                                # Run in background

# Import existing infrastructure
cldctl import resource my-app database.main -d prod-dc -e production \
  --map "aws_db_instance.main=mydb-123" --map "aws_security_group.db=sg-abc"
cldctl import component my-app -d prod-dc -e production \
  --source ghcr.io/myorg/app:v1.0.0 --mapping import-my-app.yml
cldctl import environment production -d prod-dc --mapping import-production.yml
cldctl import datacenter prod-dc --module vpc \
  --map "aws_vpc.main=vpc-0abc123" --map "aws_subnet.public[0]=subnet-xyz789"

# Logs and observability
cldctl logs -e staging -d my-datacenter           # All logs in the environment
cldctl logs -e staging my-app                     # Logs from one component (uses default DC)
cldctl logs -e staging my-app/deployment          # All deployments in a component
cldctl logs -e staging my-app/deployment/api      # A specific deployment
cldctl logs -e staging -f                         # Stream logs in real-time
cldctl logs -e staging --since 5m                 # Logs from the last 5 minutes
cldctl observability dashboard -e staging         # Open observability UI in browser
```

Aliases: `comp` for `component`, `dc` for `datacenter`, `env` for `environment`, `ls` for `list`, `obs` for `observability`

### Datacenter Resolution

All environment-scoped commands require a datacenter to be specified. The datacenter is resolved in this order:
1. `--datacenter / -d` flag on the command
2. `CLDCTL_DATACENTER` environment variable
3. `default_datacenter` in `~/.cldctl/config.yaml` (auto-set on `cldctl deploy datacenter`)

This means after deploying a datacenter once, subsequent commands can omit `-d`:
```bash
cldctl deploy datacenter my-dc my-dc:latest    # auto-sets default_datacenter
cldctl create environment staging            # uses my-dc from config
cldctl deploy component myapp:latest -e staging  # uses my-dc from config
```

### Automatic Dependency Deployment

When deploying a component that declares `dependencies` in its `cloud.component.yml`, cldctl automatically resolves and deploys any dependency components not already present in the target environment. This applies to both `deploy component` and `up` commands.

- Dependencies are resolved **transitively** (dependency of a dependency is also deployed).
- Dependencies already deployed in the environment are **skipped** (not updated).
- **Optional dependencies** (`optional: true`) are never auto-deployed. Their outputs are available if the dependency is already present in the environment, but cldctl does not pull or deploy them automatically.
- If a dependency has required variables without defaults:
  - **Interactive mode**: the user is prompted for values.
  - **CI / `--auto-approve`**: the command errors with a message listing the missing variables.
- Circular dependencies are detected and produce an error.
- Dependency components are pulled from their OCI registry references, cached locally, and registered in the unified artifact registry.
- Destroy protection prevents destroying a component that other deployed components depend on (use `--force` to override). Optional dependencies do not participate in destroy protection.

### Key Directories
| Path | Purpose |
|------|---------|
| `cmd/cldctl/` | CLI entry point |
| `internal/cli/` | Cobra command implementations |
| `pkg/schema/` | YAML/HCL config parsing with versioned schemas |
| `pkg/state/backend/` | Pluggable state backends (local, s3, gcs, azurerm) |
| `pkg/engine/` | Execution engine (graph, planner, executor, expressions, import) |
| `pkg/iac/` | IaC plugins (native, pulumi, opentofu) |
| `pkg/logs/` | Log query plugin system (querier interface, Loki adapter) |
| `pkg/errors/` | Structured error types |
| `testdata/` | Test fixtures |
| `examples/` | Example component configurations |
| `official-templates/` | Official datacenter templates (local, startup, do-k8s, do-app-platform, do-vms, aws-ecs, aws-lambda, aws-k8s, aws-vms, gcp-cloud-run, gcp-k8s, gcp-vms) |

## Component Authoring (cloud.component.yml)

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
# cloud.component.yml - Next.js apps should use functions
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
# cloud.component.yml - For long-running services with Docker builds
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
# cloud.component.yml (dev base - process-based, no Docker)
deployments:
  api:
    command: ["npm", "run", "dev"]
    workingDirectory: ./backend  # optional, defaults to cloud.component.yml dir
    environment:
      DATABASE_URL: ${{ databases.main.url }}
```

```yaml
# cloud.component.prod.yml (production - extends dev, adds Docker build)
extends: ./cloud.component.yml

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

### Dynamic Ports

Use the `ports` block to request dynamically allocated ports. Applications reference these via `${{ ports.<name>.port }}` expressions. This is opt-in — fixed-port applications don't need it.

```yaml
# Dynamic port allocation
ports:
  api:
    description: "Port for the API server"

deployments:
  api:
    command: ["node", "server.js"]
    environment:
      PORT: ${{ ports.api.port }}

services:
  api:
    deployment: api
    port: ${{ ports.api.port }}
```

```yaml
# Fixed-port apps work without ports block
deployments:
  inngest:
    command: ["npx", "inngest-cli@latest", "dev"]

services:
  inngest:
    deployment: inngest
    port: 8288
```

Ports support boolean shorthand (`ports: { api: true }`) and an optional `description` field.

Port allocation priority: environment override > datacenter hook > built-in deterministic hash fallback.

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
- `databases.<name>.read.url|host|port|username|password` (read endpoint; falls back to top-level if not set by datacenter)
- `databases.<name>.write.url|host|port|username|password` (write endpoint; falls back to top-level if not set by datacenter)
- `buckets.<name>.endpoint|bucket|accessKeyId|secretAccessKey`
- `encryptionKeys.<name>.privateKey|publicKey|privateKeyBase64|publicKeyBase64|key|keyBase64`
- `smtp.<name>.host|port|username|password`
- `ports.<name>.port` (dynamically allocated port number)
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

### Datacenter-Level Components

Datacenters can declare components at the top level that are automatically deployed into environments when needed as dependencies. This is useful for shared credential pass-through components (Stripe, Clerk, Google Cloud, etc.):

```hcl
variable "stripe_secret_key" {
  type      = string
  sensitive = true
}

# Deployed into environments on-demand when a component depends on it
component "myorg/stripe" {
  source = "latest"
  variables = {
    secret_key = variable.stripe_secret_key
  }
}
```

- `source` (required): Version tag or file path
- `variables` (optional): Map of variable values; can reference `variable.*` and `module.*.*`
- Components are **not** deployed at the datacenter level -- they are deployed into individual environments when another component declares them as a dependency
- Datacenter component variables take priority over interactive prompts but not over explicitly provided values (from environment config files or CLI flags)
- Component declarations are stored as individual state files (`datacenters/<dc>/components/<name>.state.json`), separate from the datacenter template state, so re-deploying a datacenter template does not remove previously registered components
- Components can also be managed via CLI: `cldctl deploy component <image> -d <dc>` (no `-e` flag) and `cldctl destroy component <name> -d <dc>`

### Hook Types & Required Outputs
| Hook | Required Outputs |
|------|-----------------|
| `database` | `host`, `port`, `url`, `username`, `password`; optional nested: `read` { `host`, `port`, `url`, `username`, `password` }, `write` { `host`, `port`, `url`, `username`, `password` } (auto-populated from top-level if omitted) |
| `bucket` | `endpoint`, `bucket`, `accessKeyId`, `secretAccessKey` |
| `encryptionKey` | RSA/ECDSA: `privateKey`, `publicKey`, `privateKeyBase64`, `publicKeyBase64`; Symmetric: `key`, `keyBase64` |
| `smtp` | `host`, `port`, `username`, `password` |
| `deployment` | `id` |
| `function` | `id`, `endpoint` |
| `service` | `host`, `port`, `url` |
| `route` | `url`, `host`, `port` |
| `task` | `id`, `status` |
| `observability` | `endpoint`, `protocol`, `attributes`; optional: `query_type`, `query_endpoint`, `dashboard_url` |
| `port` | `port` (optional hook — engine has built-in deterministic fallback) |

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

## Environment Files

Environment files (`environment.yml`) define which components to deploy and how they're configured. They support a `variables` block for declaring secrets and configuration that are resolved from OS environment variables and `.env` files.

### Environment Variables

```yaml
variables:
  clerk_secret_key:
    description: "Clerk secret key"
    required: true
    sensitive: true
  posthog_debug:
    description: "PostHog debug mode"
    default: "false"
  google_project_id:
    required: true
    env: GOOGLE_CLOUD_PROJECT  # explicit env var name override

components:
  my-app/clerk:
    image: my-app/clerk:latest
    variables:
      secret_key: ${{ variables.clerk_secret_key }}
```

Resolution priority (highest first): CLI `--var` flags > OS env vars > dotenv files > defaults.

By default, `clerk_secret_key` looks up env var `CLERK_SECRET_KEY` (uppercased). Use the `env` field to override.

### Dotenv File Chain (loaded from CWD)

1. `.env` (base)
2. `.env.local` (local overrides)
3. `.env.{name}` (environment-specific, e.g., `.env.staging`)
4. `.env.{name}.local` (environment-specific local overrides)

### Expression Resolution

Environment files support `${{ variables.* }}` and `${{ locals.* }}` expressions in component variable values. These are resolved before passing to the engine.

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
| `docs/guides/components/` | Step-by-step component authoring guides (Next.js, microservices, dependency deployment, etc.) |
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
