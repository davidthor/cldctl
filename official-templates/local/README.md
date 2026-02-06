# Local Docker Datacenter

A lightweight datacenter for local development using arcctl's native plugin. Optimized for fast startup with minimal overhead.

## Features

- **Native Plugin**: Uses arcctl's built-in native plugin instead of Terraform/Pulumi
- **Fast Development**: Deployments run as local processes without Docker builds
- **Process-based Functions**: Functions run as local processes for fast iteration
- **Docker Infrastructure**: Databases and supporting services run as Docker containers
- **No IaC Overhead**: Skips drift detection and state refresh for faster operations
- **Automatic Port Assignment**: Ports are automatically assigned to avoid conflicts
- **Dockerfile CMD Support**: Automatically extracts commands from Dockerfiles

## Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `network_name` | string | `arcctl-local` | Docker network for container communication |
| `host` | string | `localhost` | Host for service URLs |
| `base_port` | number | `8000` | Base port for auto-assignment |

## Hooks Supported

| Hook | Implementation |
|------|---------------|
| database (PostgreSQL) | Docker container (`postgres:16`) |
| database (MySQL) | Docker container (`mysql:8`) |
| database (Redis) | Docker container (`redis:7-alpine`) |
| databaseUser | Additional user credentials for existing databases |
| task | One-time Docker containers (e.g., migrations) |
| bucket | MinIO container (S3-compatible) |
| encryptionKey (RSA/ECDSA) | Generated locally (asymmetric) |
| encryptionKey (symmetric) | Generated locally (symmetric) |
| smtp | MailHog container (email capture with web UI) |
| secret | Stored locally in state |
| deployment (from source) | Local processes (no Docker build) |
| deployment (pre-built image) | Docker containers |
| function | Local processes |
| service | Port mapping lookup |
| route | nginx reverse proxy |
| ingress | nginx reverse proxy (alias for route) |
| cronjob | Suspended by default (manual trigger) |
| dockerBuild | Local image builds |
| observability | Grafana LGTM (Loki + Tempo + Prometheus) |

## Quick Start

```bash
# Build and push
arcctl dc build . -t ghcr.io/myorg/local:v1
arcctl dc push ghcr.io/myorg/local:v1

# Deploy datacenter
arcctl dc deploy local-dev --config ghcr.io/myorg/local:v1

# Create environment and deploy
arcctl env create my-env --datacenter local-dev
arcctl deploy ./my-app -e my-env
```

## Module Structure

```
local/
├── datacenter.dc            # Main datacenter configuration
├── modules/
│   ├── docker-postgres/     # PostgreSQL in Docker
│   ├── docker-mysql/        # MySQL in Docker
│   ├── docker-redis/        # Redis in Docker
│   ├── docker-bucket/       # MinIO for S3 storage
│   ├── docker-build/        # Local Docker image builds
│   ├── docker-deployment/   # Container deployments
│   ├── docker-exec/         # One-time container execution (migrations)
│   ├── docker-network/      # Docker network creation
│   ├── docker-otel-backend/ # Grafana LGTM observability stack
│   ├── docker-service/      # Service discovery
│   ├── encryption-key/      # RSA, ECDSA, and symmetric key generation
│   ├── local-cronjob/       # Suspended cronjob tracking
│   ├── local-route/         # nginx reverse proxy for routing/ingress
│   ├── local-secret/        # Local secret storage
│   ├── local-smtp/          # MailHog email testing
│   ├── process-deployment/  # Process-based deployments (no Docker)
│   └── process-function/    # Local process functions
└── README.md
```

## How It Works

### Native Plugin

The native plugin executes Docker and process commands directly, without going through Pulumi or OpenTofu:

1. **Apply**: Creates Docker containers/processes and stores outputs in state
2. **Destroy**: Stops containers/processes based on stored state
3. **No Drift Detection**: Trusts stored state, making operations faster

### Deployment Strategy

The datacenter supports **three deployment modes** depending on your component configuration:

#### 1. Source-Based Deployments (with `build` context)
Components with a `build` section first build a Docker image, then run it as a container.

#### 2. Image-Based Deployments (pre-built images)
Components using existing Docker images run them as containers directly.

#### 3. Process-Based Deployments (no image, for local dev)
Components without an image (and optionally with `runtime`) run as local processes for maximum development speed.

### Trade-offs

**Faster** than cloud datacenters because:
- No IaC tool initialization
- No Docker build wait time (for process-based)
- No cloud API calls
- No state refresh/drift detection

**Less robust** than cloud datacenters because:
- No drift detection (manual changes not reconciled)
- No resource locking
- Requires system dependencies installed locally
- Designed for single-user local development
