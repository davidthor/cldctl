# GCP Kubernetes (GKE) Datacenter

A Kubernetes datacenter for Google Cloud Platform using GKE for container workloads, Knative for serverless functions, Cloud SQL for databases, and Compute Engine for VM-based deployments.

## Services Used

- **Google Kubernetes Engine (GKE)** - Managed Kubernetes for container deployments
- **Knative Serving** - Serverless functions on GKE (scale-to-zero)
- **Cloud SQL** - Managed PostgreSQL and MySQL databases
- **Memorystore** - Managed Redis instances
- **Google Cloud Storage** - Object storage with HMAC keys for S3-compatible access
- **Compute Engine** - VMs for runtime-based deployments (via OpenTofu)
- **Cloud KMS** - Encryption key management
- **Secret Manager** - Secure secret storage
- **Cloud DNS** - DNS management
- **GKE Gateway API** - HTTP routing and ingress
- **Artifact Registry** - Docker image storage
- **Cloud Monitoring** - Observability via OTel collector on GKE

## Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `gcp_project` | string | *required* | GCP project ID |
| `gcp_region` | string | `us-central1` | GCP region |
| `cluster_name` | string | *required* | GKE cluster name |
| `domain` | string | `app.example.com` | Base domain for environments |
| `registry` | string | *required* | Artifact Registry repository URL |
| `ssh_key` | string | *(empty)* | SSH public key for Compute Engine VM access |
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
| task | Kubernetes Jobs |
| bucket | Google Cloud Storage with HMAC keys |
| encryptionKey (RSA/ECDSA) | Cloud KMS (asymmetric) |
| encryptionKey (symmetric) | Cloud KMS (symmetric) |
| smtp | External relay (configurable) |
| secret | Secret Manager |
| deployment (container) | GKE Deployments |
| deployment (VM/runtime) | Compute Engine via OpenTofu |
| function | Knative Serving on GKE |
| service | Kubernetes ClusterIP Services |
| ingress | GKE Gateway API + HTTPRoute |
| cronjob | Kubernetes CronJobs |
| dockerBuild | Artifact Registry + Cloud Build |
| observability | Cloud Monitoring + OTel collector on GKE |

## Quick Start

```bash
# Build and push
arcctl dc build . -t ghcr.io/myorg/gcp-k8s:v1
arcctl dc push ghcr.io/myorg/gcp-k8s:v1

# Deploy datacenter
arcctl dc deploy gcp-k8s-prod \
  --config ghcr.io/myorg/gcp-k8s:v1 \
  --var gcp_project=my-project \
  --var cluster_name=prod-cluster \
  --var registry=us-central1-docker.pkg.dev/my-project/arcctl \
  --var domain=app.example.com \
  --var smtp_password=$SMTP_PASSWORD

# Create environment and deploy
arcctl env create staging --datacenter gcp-k8s-prod
arcctl deploy ghcr.io/myorg/my-app:v1 -e staging
```

## Prerequisites

1. A GCP project with billing enabled
2. APIs enabled: Kubernetes Engine, Cloud SQL, Memorystore, Cloud Storage, Compute Engine, Cloud KMS, Secret Manager, Cloud DNS, Artifact Registry, Cloud Monitoring
3. An Artifact Registry Docker repository
4. (Optional) Knative Serving installed on the GKE cluster for function support
5. (Optional) An SSH key pair for Compute Engine VM access
6. (Optional) A custom domain configured in Cloud DNS
7. (Optional) An external SMTP provider (e.g., SendGrid, Mailgun)

## Knative Setup

For serverless functions, install Knative Serving on your GKE cluster:

```bash
kubectl apply -f https://github.com/knative/serving/releases/download/knative-v1.14.0/serving-crds.yaml
kubectl apply -f https://github.com/knative/serving/releases/download/knative-v1.14.0/serving-core.yaml
```
