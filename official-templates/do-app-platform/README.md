# DigitalOcean App Platform

Deploy portable cloud-native applications to DigitalOcean App Platform — a fully managed PaaS with automatic scaling, TLS, and zero-downtime deploys.

## Services Used

- **DigitalOcean App Platform** - PaaS for deployments, functions, and scheduled jobs
- **DigitalOcean Managed Databases** - PostgreSQL, MySQL, Redis
- **DigitalOcean Spaces** - S3-compatible object storage
- **DigitalOcean Container Registry** - Private Docker image registry
- **External SMTP Relay** - Email delivery (e.g., SendGrid, Mailgun)

## Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `do_token` | string (sensitive) | — | DigitalOcean API token |
| `region` | string | `nyc` | DigitalOcean region |
| `domain` | string | `""` | Custom domain for applications |
| `registry` | string | `registry.digitalocean.com` | Container registry URL |
| `smtp_host` | string | `smtp.sendgrid.net` | External SMTP relay host |
| `smtp_port` | number | `587` | External SMTP relay port |
| `smtp_username` | string | `apikey` | External SMTP relay username |
| `smtp_password` | string (sensitive) | `""` | External SMTP relay password |

## Hooks Supported

| Hook | Implementation |
|------|---------------|
| Database | DigitalOcean Managed Database (postgres, mysql, redis) |
| Task | App Platform Jobs (one-time execution) |
| Bucket | DigitalOcean Spaces (S3-compatible) |
| Encryption Key | Generated keys stored as app secrets (RSA, ECDSA, symmetric) |
| SMTP | External relay via configurable variables |
| Database User | Additional users on managed databases |
| Secret | App Platform encrypted environment variables |
| Deployment (container) | App Platform Services |
| Function | App Platform Functions |
| Service | App Platform internal routing |
| Ingress | App Platform domain routing (automatic TLS) |
| CronJob | App Platform scheduled jobs |
| Docker Build | Build and push to DigitalOcean Container Registry |
| Observability | App Platform built-in monitoring + OTel collector |

**Not supported:** VM/runtime-based deployments (App Platform is PaaS only).

## Quick Start

```bash
# Build and push the datacenter
arcctl build datacenter ./do-app-platform -t ghcr.io/myorg/do-app-platform:v1
arcctl push datacenter ghcr.io/myorg/do-app-platform:v1

# Deploy the datacenter
arcctl deploy datacenter do-paas ghcr.io/myorg/do-app-platform:v1 \
  --var do_token=$DO_TOKEN \
  --var domain=app.example.com

# Create an environment and deploy a component
arcctl create environment staging -d do-paas
arcctl deploy component ghcr.io/myorg/my-app:v1 -e staging
```
