# Container-Based IaC Module Execution

This package implements container-based execution for IaC modules (Pulumi, OpenTofu). Modules are packaged as self-contained container images that bundle the IaC code with its runtime, eliminating the need for Pulumi or OpenTofu to be installed on the deployment host.

## Overview

The container approach provides several benefits:

1. **Portability**: Deploy hosts only need Docker, not individual IaC tools
2. **Version Isolation**: Each module can use its own IaC runtime version
3. **Reproducibility**: Identical execution environment across all hosts
4. **Security**: Modules run in isolated containers with controlled access

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        cldctl host                               │
│                                                                  │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐      │
│  │   Executor   │───▶│   Docker     │───▶│   Module     │      │
│  │              │    │   Engine     │    │   Container  │      │
│  └──────────────┘    └──────────────┘    └──────────────┘      │
│         │                                       │               │
│         │  input.json                           │               │
│         ▼                                       ▼               │
│  ┌──────────────┐                        ┌──────────────┐      │
│  │   Request    │                        │  Entrypoint  │      │
│  │   {action,   │                        │  (translates │      │
│  │    inputs}   │                        │   to IaC)    │      │
│  └──────────────┘                        └──────────────┘      │
│                                                 │               │
│                                                 ▼               │
│                                          ┌──────────────┐      │
│                                          │ Pulumi/Tofu  │      │
│                                          │   Runtime    │      │
│                                          └──────────────┘      │
└─────────────────────────────────────────────────────────────────┘
```

## Module Container Interface

### Input (JSON)

The executor writes a JSON request to `/workspace/input.json`:

```json
{
  "action": "apply",
  "inputs": {
    "name": "my-database",
    "version": "16"
  },
  "environment": {
    "AWS_REGION": "us-east-1"
  },
  "stack_name": "prod-api-database"
}
```

### Output (JSON)

The container writes a JSON response to `/workspace/output.json`:

```json
{
  "success": true,
  "action": "apply",
  "outputs": {
    "host": { "value": "db.example.com" },
    "port": { "value": 5432 },
    "url": { "value": "postgresql://..." }
  }
}
```

## Building Module Images

### Automatic Detection

The builder detects the module type from the source directory:

- **Pulumi**: Contains `Pulumi.yaml`
- **OpenTofu**: Contains `.tf` files

### Generated Dockerfiles

#### Pulumi (Node.js example)

```dockerfile
FROM pulumi/pulumi-nodejs:latest
WORKDIR /app
COPY . .
RUN npm ci --production
COPY --from=cldctl-entrypoint /cldctl-entrypoint /cldctl-entrypoint
ENTRYPOINT ["/cldctl-entrypoint"]
```

#### OpenTofu

```dockerfile
FROM ghcr.io/opentofu/opentofu:latest
WORKDIR /app
COPY . .
RUN tofu init -backend=false
COPY --from=cldctl-entrypoint /cldctl-entrypoint /cldctl-entrypoint
ENTRYPOINT ["/cldctl-entrypoint"]
```

## Usage

### Building a Module

```go
builder, _ := container.NewBuilder()
defer builder.Close()

result, err := builder.Build(ctx, container.BuildOptions{
    ModuleDir: "./modules/postgres",
    Tag:       "myregistry.io/modules/postgres:v1.0.0",
})
```

### Executing a Module

```go
executor, _ := container.NewExecutor()
defer executor.Close()

response, err := executor.Execute(ctx, container.ExecuteOptions{
    Image: "myregistry.io/modules/postgres:v1.0.0",
    Request: &container.ModuleRequest{
        Action: "apply",
        Inputs: map[string]interface{}{
            "name": "my-db",
        },
    },
    Credentials: map[string]string{
        "AWS_ACCESS_KEY_ID":     "...",
        "AWS_SECRET_ACCESS_KEY": "...",
    },
})
```

### Using the IaC Plugin

```go
// Register automatically on import
import _ "github.com/davidthor/cldctl/pkg/iac/container"

// Get plugin from registry
plugin, _ := iac.DefaultRegistry.Get("container")

// Execute via standard interface
result, err := plugin.Apply(ctx, iac.RunOptions{
    ModuleSource: "myregistry.io/modules/postgres:v1.0.0",
    Inputs: map[string]interface{}{
        "name": "my-db",
    },
})
```

## Supported Actions

| Action    | Description                            |
| --------- | -------------------------------------- |
| `preview` | Show planned changes without applying  |
| `apply`   | Create or update resources             |
| `destroy` | Remove all resources                   |
| `refresh` | Read current state from infrastructure |

## Cloud Provider Credentials

The executor automatically passes through common cloud credentials:

### AWS

- `AWS_ACCESS_KEY_ID`
- `AWS_SECRET_ACCESS_KEY`
- `AWS_SESSION_TOKEN`
- `AWS_REGION`

### GCP

- `GOOGLE_APPLICATION_CREDENTIALS`
- `GOOGLE_PROJECT`
- `GOOGLE_REGION`

### Azure

- `AZURE_SUBSCRIPTION_ID`
- `AZURE_TENANT_ID`
- `AZURE_CLIENT_ID`
- `AZURE_CLIENT_SECRET`

### Kubernetes

- `KUBECONFIG`

## Entrypoint Program

The `entrypoint/main.go` program runs inside the container and:

1. Reads the JSON request from `/workspace/input.json`
2. Detects whether to use Pulumi or OpenTofu
3. Translates inputs to the IaC tool's native format
4. Executes the requested action
5. Captures outputs and writes response to `/workspace/output.json`

## Building the Entrypoint

The entrypoint needs to be compiled and included in module images:

```bash
cd pkg/iac/container/entrypoint
GOOS=linux GOARCH=amd64 go build -o cldctl-entrypoint .
```

For production, this is built as a multi-architecture binary and published as a scratch image that module Dockerfiles copy from.
