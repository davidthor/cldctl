# GCP Cloud Run Datacenter

A serverless datacenter for Google Cloud Platform using Cloud Run for compute, Cloud SQL for databases, GCS for storage, and Cloud Monitoring for observability. Optimized for scale-to-zero workloads with minimal infrastructure management.

## Services Used

- **Cloud Run** - Serverless container deployments and functions (scale-to-zero)
- **Cloud SQL** - Managed PostgreSQL and MySQL databases
- **Memorystore** - Managed Redis instances
- **Google Cloud Storage** - Object storage with HMAC keys for S3-compatible access
- **Cloud KMS** - Encryption key management
- **Secret Manager** - Secure secret storage
- **Cloud Load Balancing** - HTTPS load balancing with custom domains
- **Cloud DNS** - DNS management
- **Cloud Scheduler** - Cron job scheduling
- **Artifact Registry** - Docker image storage
- **Cloud Monitoring / Logging / Trace** - Observability via OpenTelemetry

## Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `gcp_project` | string | *required* | GCP project ID |
| `gcp_region` | string | `us-central1` | GCP region |
| `domain` | string | `app.example.com` | Base domain for environments |
| `registry` | string | *required* | Artifact Registry repository URL |
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
| task | Cloud Run Jobs (one-time execution) |
| bucket | Google Cloud Storage with HMAC keys |
| encryptionKey (RSA/ECDSA) | Cloud KMS (asymmetric) |
| encryptionKey (symmetric) | Cloud KMS (symmetric) |
| smtp | External relay (configurable) |
| secret | Secret Manager |
| deployment (container) | Cloud Run services |
| function | Cloud Run services (scale-to-zero) |
| service | Cloud Run internal service discovery |
| ingress | Cloud Load Balancing |
| cronjob | Cloud Scheduler |
| dockerBuild | Artifact Registry + Cloud Build |
| observability | Cloud Monitoring + OTel collector |

> **Note**: This template does **not** support VM/runtime-based deployments. Use [gcp-k8s](../gcp-k8s/) or [gcp-vms](../gcp-vms/) for workloads that require the `runtime` property.

## Quick Start

```bash
# Build and push
arcctl dc build . -t ghcr.io/myorg/gcp-cloud-run:v1
arcctl dc push ghcr.io/myorg/gcp-cloud-run:v1

# Deploy datacenter
arcctl dc deploy gcp-prod \
  --config ghcr.io/myorg/gcp-cloud-run:v1 \
  --var gcp_project=my-project \
  --var registry=us-central1-docker.pkg.dev/my-project/arcctl \
  --var domain=app.example.com \
  --var smtp_password=$SMTP_PASSWORD

# Create environment and deploy
arcctl env create staging --datacenter gcp-prod
arcctl deploy ghcr.io/myorg/my-app:v1 -e staging
```

## Prerequisites

1. A GCP project with billing enabled
2. APIs enabled: Cloud Run, Cloud SQL, Memorystore, Cloud Storage, Cloud KMS, Secret Manager, Cloud Scheduler, Artifact Registry, Cloud Monitoring
3. An Artifact Registry Docker repository
4. (Optional) A custom domain configured in Cloud DNS
5. (Optional) An external SMTP provider (e.g., SendGrid, Mailgun)
