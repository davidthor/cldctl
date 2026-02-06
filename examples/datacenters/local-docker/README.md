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

## Resource Implementations

| Resource | Implementation |
|----------|----------------|
| PostgreSQL | Docker container (`postgres:16`) |
| MySQL | Docker container (`mysql:8`) |
| Redis | Docker container (`redis:7-alpine`) |
| Buckets | MinIO container (S3-compatible) |
| Encryption Keys | Generated locally (RSA, ECDSA, symmetric) |
| SMTP | MailHog container (email capture with web UI) |
| **Deployments (from source)** | **Local processes (no Docker build)** |
| **Deployments (pre-built)** | **Docker containers (existing images)** |
| Functions | Local processes |
| Services | Port mapping lookup |
| Routes | nginx reverse proxy |
| Cronjobs | Suspended by default (manual trigger) |
| Docker Builds | Local image builds (if needed) |
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

### Deployment Strategy

The datacenter supports **two deployment modes** depending on your component configuration:

#### 1. Source-Based Deployments (Fast Development)

**For maximum development speed**, components with `build.context` run as **local processes**:

1. **No Docker Build**: Skips time-consuming image builds
2. **Direct Execution**: Runs commands directly in the build context directory
3. **Dockerfile CMD Extraction**: Automatically extracts the `CMD` from your Dockerfile
4. **Command Override**: Can override with explicit `command` in component definition

**Requirements**:
- System dependencies (Node.js, Python, etc.) must be installed locally
- Build context must contain source code ready to run

**Example**:
```yaml
# architect.yml - Process-based local development
deployments:
  api:
    command: ["npm", "run", "dev"]
    environment:
      DATABASE_URL: ${{ databases.main.url }}
```

Or with a Docker build for production:
```yaml
# architect.prod.yml - Extends dev base with Docker builds
extends: architect.yml

builds:
  api:
    context: .
    dockerfile: Dockerfile

deployments:
  api:
    image: ${{ builds.api.image }}
    command: ["npm", "start"]
```

The datacenter will:
1. Use the component directory as the working directory (or `workingDirectory` if set)
2. Run the command directly as a local process
3. Auto-assign a PORT environment variable

#### 2. Image-Based Deployments (Pre-Built Images)

For components using **pre-built Docker images** (e.g., third-party services like Zookeeper), the datacenter runs them as **Docker containers**:

**Example**:
```yaml
# architect.yml - Pre-built image
deployments:
  zookeeper:
    image: zookeeper:3.9
    environment:
      ZOO_TICK_TIME: "2000"
```

The datacenter will:
1. Pull the Docker image if not present
2. Run it as a Docker container
3. Connect it to the local network
4. No build step required

### State Management

Despite being lightweight, state IS persisted:
- Enables dependency wiring between components
- Tracks process IDs and container IDs for cleanup
- Stores outputs (ports, credentials) for other resources

### Trade-offs

**Faster** than cloud datacenters because:
- No IaC tool initialization
- No Docker build wait time
- No cloud API calls
- No state refresh/drift detection

**Less robust** than cloud datacenters because:
- No drift detection (manual changes not reconciled)
- No resource locking
- Requires system dependencies installed locally
- Designed for single-user local development

## Module Structure

```
local-docker/
├── datacenter.dc            # Main datacenter configuration
├── modules/
│   ├── docker-postgres/     # PostgreSQL in Docker
│   ├── docker-mysql/        # MySQL in Docker
│   ├── docker-redis/        # Redis in Docker
│   ├── process-deployment/  # ⭐ NEW: Process-based deployments (no Docker build)
│   ├── docker-deployment/   # (Deprecated) Container deployments
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
