# GCP VM-based Datacenter

A VM-centric datacenter for Google Cloud Platform where all workloads run on Compute Engine instances. Container-based deployments install Docker on the VM, runtime-based deployments install the language runtime directly. Uses Cloud SQL for databases, GCS for storage, and Cloud Monitoring for observability.

## Services Used

- **Compute Engine** - VMs for all deployments, functions, and cronjobs (via OpenTofu)
- **Cloud SQL** - Managed PostgreSQL and MySQL databases
- **Memorystore** - Managed Redis instances
- **Google Cloud Storage** - Object storage with HMAC keys for S3-compatible access
- **Cloud KMS** - Encryption key management
- **Secret Manager** - Secure secret storage
- **Cloud Load Balancing** - HTTPS load balancing with custom domains
- **Cloud DNS** - DNS management and internal service discovery
- **Artifact Registry** - Docker image storage
- **Cloud Monitoring** - Observability via Ops Agent on VMs

## Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `gcp_project` | string | *required* | GCP project ID |
| `gcp_region` | string | `us-central1` | GCP region |
| `domain` | string | `app.example.com` | Base domain for environments |
| `registry` | string | *required* | Artifact Registry repository URL |
| `machine_type` | string | `e2-medium` | Default Compute Engine machine type |
| `ssh_key` | string | *required* | SSH public key for VM access |
| `smtp_host` | string | `smtp.sendgrid.net` | External SMTP relay host |
| `smtp_port` | number | `587` | External SMTP relay port |
| `smtp_username` | string | `apikey` | External SMTP relay username |
| `smtp_password` | string | *(empty)* | External SMTP relay password (sensitive) |

## Hooks Supported

| Hook | Implementation |
|------|---------------|
| database (PostgreSQL) | Cloud SQL for PostgreSQL |
| database (MySQL) | Cloud SQL for MySQL |
| database (Redis) | Memorystore for Redis |
| databaseUser | Cloud SQL user management |
| task | Temporary Compute Engine VM (via OpenTofu) |
| bucket | Google Cloud Storage with HMAC keys |
| encryptionKey (RSA/ECDSA) | Cloud KMS (asymmetric) |
| encryptionKey (symmetric) | Cloud KMS (symmetric) |
| smtp | External relay (configurable) |
| secret | Secret Manager |
| deployment (container) | Compute Engine + Docker (via OpenTofu) |
| deployment (VM/runtime) | Compute Engine + language runtime (via OpenTofu) |
| function | Compute Engine as long-running process behind LB (via OpenTofu) |
| service | Cloud DNS private zone (internal DNS) |
| ingress | Cloud Load Balancing |
| cronjob | Compute Engine + system cron (via OpenTofu) |
| dockerBuild | Artifact Registry + Cloud Build |
| observability | Cloud Monitoring Ops Agent on VMs |

## Quick Start

```bash
# Build and push
arcctl dc build . -t ghcr.io/myorg/gcp-vms:v1
arcctl dc push ghcr.io/myorg/gcp-vms:v1

# Deploy datacenter
arcctl dc deploy gcp-vms-prod \
  --config ghcr.io/myorg/gcp-vms:v1 \
  --var gcp_project=my-project \
  --var registry=us-central1-docker.pkg.dev/my-project/arcctl \
  --var ssh_key="$(cat ~/.ssh/id_rsa.pub)" \
  --var domain=app.example.com \
  --var smtp_password=$SMTP_PASSWORD

# Create environment and deploy
arcctl env create staging --datacenter gcp-vms-prod
arcctl deploy ghcr.io/myorg/my-app:v1 -e staging
```

## Prerequisites

1. A GCP project with billing enabled
2. APIs enabled: Compute Engine, Cloud SQL, Memorystore, Cloud Storage, Cloud KMS, Secret Manager, Cloud DNS, Cloud Load Balancing, Artifact Registry, Cloud Monitoring
3. An Artifact Registry Docker repository
4. An SSH key pair for VM access
5. (Optional) A custom domain configured in Cloud DNS
6. (Optional) An external SMTP provider (e.g., SendGrid, Mailgun)

## How It Works

### Container Deployments (image present)
When a component provides a Docker image, the module provisions a Compute Engine VM, installs Docker via a startup script, pulls the image from Artifact Registry, and runs it as a Docker container with a systemd service.

### Runtime Deployments (runtime present, no image)
When a component specifies a runtime (e.g., `node:20`), the module provisions a Compute Engine VM, installs the appropriate language runtime and system packages, runs setup commands, and starts the application as a systemd service.

### Functions
Since Compute Engine doesn't have native serverless support, functions are deployed as long-running processes on VMs behind the Cloud Load Balancer, providing stable HTTP endpoints.
