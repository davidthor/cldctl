# DigitalOcean Kubernetes

Deploy portable cloud-native applications to DigitalOcean Kubernetes (DOKS) with managed databases, Spaces storage, and full observability.

## Services Used

- **DigitalOcean Kubernetes (DOKS)** - Managed Kubernetes cluster with autoscaling
- **DigitalOcean Managed Databases** - PostgreSQL, MySQL, Redis
- **DigitalOcean Spaces** - S3-compatible object storage
- **DigitalOcean Droplets** - VM-based deployments (runtime workloads)
- **DigitalOcean DNS** - DNS record management
- **Knative Serving** - Serverless functions on Kubernetes
- **NGINX Ingress + cert-manager** - TLS-terminated HTTP routing
- **OpenTelemetry Collector** - Observability (logs, traces, metrics)
- **External SMTP Relay** - Email delivery (e.g., SendGrid, Mailgun)

## Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `do_token` | string (sensitive) | — | DigitalOcean API token |
| `region` | string | `nyc3` | DigitalOcean region |
| `cluster_name` | string | — | Kubernetes cluster name |
| `domain` | string | `app.example.com` | Base domain for environments |
| `registry` | string | `registry.digitalocean.com` | Container registry URL |
| `ssh_key_fingerprint` | string | `""` | SSH key fingerprint for Droplet access |
| `smtp_host` | string | `smtp.sendgrid.net` | External SMTP relay host |
| `smtp_port` | number | `587` | External SMTP relay port |
| `smtp_username` | string | `apikey` | External SMTP relay username |
| `smtp_password` | string (sensitive) | `""` | External SMTP relay password |

## Hooks Supported

| Hook | Implementation |
|------|---------------|
| Database | DigitalOcean Managed Database (postgres, mysql, redis) |
| Task | Kubernetes Jobs (one-time execution) |
| Bucket | DigitalOcean Spaces (S3-compatible) |
| Encryption Key | Generated keys stored as Kubernetes Secrets (RSA, ECDSA, symmetric) |
| SMTP | External relay via configurable variables |
| Database User | Additional users on managed databases |
| Secret | Kubernetes Secrets |
| Deployment (container) | Kubernetes Deployments |
| Deployment (VM/runtime) | DigitalOcean Droplets via OpenTofu |
| Function | Knative Serving (serverless on K8s) |
| Service | Kubernetes Services (ClusterIP) |
| Ingress | Kubernetes HTTPRoute + NGINX Gateway |
| CronJob | Kubernetes CronJobs |
| Docker Build | Build and push to DigitalOcean Container Registry |
| Observability | Self-hosted OTel Collector + DigitalOcean Monitoring |

## Quick Start

```bash
# Build and push the datacenter
arcctl build datacenter ./do-k8s -t ghcr.io/myorg/do-k8s-dc:v1
arcctl push datacenter ghcr.io/myorg/do-k8s-dc:v1

# Deploy the datacenter
arcctl deploy datacenter do-production ghcr.io/myorg/do-k8s-dc:v1 \
  --var cluster_name=prod-cluster \
  --var domain=app.example.com \
  --var do_token=$DO_TOKEN

# Create an environment and deploy a component
arcctl create environment staging -d do-production
arcctl deploy component ghcr.io/myorg/my-app:v1 -e staging
```
