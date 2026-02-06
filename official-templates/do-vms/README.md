# DigitalOcean Droplet VMs

Deploy portable cloud-native applications to DigitalOcean Droplets (VMs) with managed databases and Spaces storage. Container-based workloads run Docker on Droplets; runtime-based workloads install the language runtime directly.

## Services Used

- **DigitalOcean Droplets** - Virtual machines for all compute workloads
- **DigitalOcean Managed Databases** - PostgreSQL, MySQL, Redis
- **DigitalOcean Spaces** - S3-compatible object storage
- **DigitalOcean Load Balancer** - HTTP/HTTPS traffic routing
- **DigitalOcean VPC** - Private networking between Droplets
- **DigitalOcean DNS** - DNS record management
- **DigitalOcean Container Registry** - Private Docker image registry
- **External SMTP Relay** - Email delivery (e.g., SendGrid, Mailgun)

## Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `do_token` | string (sensitive) | — | DigitalOcean API token |
| `region` | string | `nyc3` | DigitalOcean region |
| `domain` | string | `app.example.com` | Base domain for environments |
| `ssh_key_fingerprint` | string | — | SSH key fingerprint for Droplet access |
| `droplet_size` | string | `s-1vcpu-1gb` | Default Droplet size for workloads |
| `registry` | string | `registry.digitalocean.com` | Container registry URL |
| `smtp_host` | string | `smtp.sendgrid.net` | External SMTP relay host |
| `smtp_port` | number | `587` | External SMTP relay port |
| `smtp_username` | string | `apikey` | External SMTP relay username |
| `smtp_password` | string (sensitive) | `""` | External SMTP relay password |

## Hooks Supported

| Hook | Implementation |
|------|---------------|
| Database | DigitalOcean Managed Database (postgres, mysql, redis) |
| Task | Temporary Droplet for one-time execution |
| Bucket | DigitalOcean Spaces (S3-compatible) |
| Encryption Key | Generated keys stored in state (RSA, ECDSA, symmetric) |
| SMTP | External relay via configurable variables |
| Database User | Additional users on managed databases |
| Secret | Stored in state |
| Deployment (container) | Docker on Droplets |
| Deployment (VM/runtime) | Language runtime on Droplets |
| Function | Long-running process on Droplet behind Caddy |
| Service | Internal DNS/VPC routing |
| Ingress | DigitalOcean Load Balancer + DNS |
| CronJob | Cron on dedicated Droplets |
| Docker Build | Build and push to DigitalOcean Container Registry |
| Observability | OTel Collector on dedicated Droplet (Grafana + Loki) |

## Quick Start

```bash
# Build and push the datacenter
arcctl build datacenter ./do-vms -t ghcr.io/myorg/do-vms:v1
arcctl push datacenter ghcr.io/myorg/do-vms:v1

# Deploy the datacenter
arcctl deploy datacenter do-vms ghcr.io/myorg/do-vms:v1 \
  --var do_token=$DO_TOKEN \
  --var ssh_key_fingerprint=$SSH_FINGERPRINT \
  --var domain=app.example.com

# Create an environment and deploy a component
arcctl create environment staging -d do-vms
arcctl deploy component ghcr.io/myorg/my-app:v1 -e staging
```
