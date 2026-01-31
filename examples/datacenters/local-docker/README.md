# Local Docker Datacenter

A lightweight datacenter for local development using arcctl's native plugin. Optimized for fast startup with minimal overhead.

## Features

- **Native Plugin**: Uses arcctl's built-in native plugin instead of Terraform/Pulumi
- **Docker-based**: Databases and deployments run as Docker containers
- **Process-based Functions**: Functions run as local processes for fast iteration
- **No IaC Overhead**: Skips drift detection and state refresh for faster operations
- **Automatic Port Assignment**: Ports are automatically assigned to avoid conflicts

## Resource Implementations

| Resource | Implementation |
|----------|----------------|
| PostgreSQL | Docker container (`postgres:16`) |
| MySQL | Docker container (`mysql:8`) |
| Redis | Docker container (`redis:7-alpine`) |
| Buckets | MinIO container (S3-compatible) |
| Encryption Keys | Generated locally (RSA, ECDSA, symmetric) |
| SMTP | MailHog container (email capture with web UI) |
| Deployments | Docker containers |
| Functions | Local processes |
| Services | Port mapping lookup |
| Routes | nginx reverse proxy |
| Cronjobs | Suspended by default (manual trigger) |
| Docker Builds | Local image builds |
| Migrations | One-time Docker containers |

## Usage

### With arcctl up (Recommended for Local Dev)

```bash
# From your component directory
arcctl up --datacenter local-docker

# Or set as default datacenter
export ARCCTL_DATACENTER=local-docker
arcctl up
```

### Manual Environment Management

```bash
# Deploy the datacenter
arcctl dc deploy local-dev ./local-docker

# Create an environment
arcctl env create my-env --datacenter local-dev

# Deploy a component
arcctl component deploy my-app -e my-env --config ./my-app
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `network_name` | `arcctl-local` | Docker network for container communication |
| `host` | `localhost` | Host for service URLs |
| `base_port` | `8000` | Base port for auto-assignment |

## How It Works

### Native Plugin

The native plugin executes Docker and process commands directly, without going through Pulumi or OpenTofu:

1. **Apply**: Creates Docker containers/processes and stores outputs in state
2. **Destroy**: Stops containers/processes based on stored state
3. **No Drift Detection**: Trusts stored state, making operations faster

### State Management

Despite being lightweight, state IS persisted:
- Enables dependency wiring between components
- Tracks container IDs for cleanup
- Stores outputs (ports, credentials) for other resources

### Trade-offs

**Faster** than cloud datacenters because:
- No IaC tool initialization
- No cloud API calls
- No state refresh/drift detection

**Less robust** than cloud datacenters because:
- No drift detection (manual changes not reconciled)
- No resource locking
- Designed for single-user local development

## Module Structure

```
local-docker/
├── datacenter.dc            # Main datacenter configuration
├── modules/
│   ├── docker-postgres/     # PostgreSQL in Docker
│   ├── docker-mysql/        # MySQL in Docker
│   ├── docker-redis/        # Redis in Docker
│   ├── docker-deployment/   # Container deployments
│   ├── docker-bucket/       # MinIO for S3 storage
│   ├── docker-service/      # Service discovery
│   ├── docker-network/      # Docker network creation
│   ├── docker-exec/         # One-time container execution (migrations)
│   ├── docker-build/        # Local Docker image builds
│   ├── encryption-key/      # RSA, ECDSA, and symmetric key generation
│   ├── local-smtp/          # MailHog email testing
│   ├── local-route/         # nginx reverse proxy for routing
│   ├── local-cronjob/       # Suspended cronjob tracking
│   └── process-function/    # Local process functions
└── README.md
```

## Example

```bash
# Start a Next.js app with PostgreSQL locally
cd my-nextjs-app
arcctl up --datacenter local-docker

# Output:
# ✓ Database main (postgres) running on localhost:54321
# ✓ Function web running on localhost:3000
# ✓ Route main available at http://localhost:3000
#
# Press Ctrl+C to stop
```
