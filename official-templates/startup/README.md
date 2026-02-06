# Startup Datacenter

A managed-services datacenter for startups and small teams. Uses best-in-class managed services to minimize infrastructure overhead: Vercel for compute, Neon for PostgreSQL, Upstash for Redis, Vercel Blob for storage, and Resend for transactional email.

This template is designed for a full production workflow: a single datacenter hosts production, staging, and unlimited preview environments with built-in database branching and per-environment DNS aliases.

## Services Used

- **Vercel** - Serverless Functions, Blob Storage, Cron Jobs, Routing
- **Neon** - Managed PostgreSQL (serverless, branching)
- **Upstash** - Managed Redis (serverless, per-request pricing)
- **Resend** - Transactional email (SMTP)

## Environment Tiers

Environment behavior is determined by `environment.name`:

| Environment Name | Tier | Neon Branch | Vercel Target | DNS Alias |
|------------------|------|-------------|---------------|-----------|
| `production` | Production | main (no branch) | `production` | `app.example.com` |
| `staging` | Staging | branch from main | `preview` | `staging.app.example.com` |
| anything else | Preview | branch from staging | `preview` | `<name>.app.example.com` |

### Neon Branching Model

```
main (production)
  └── staging
        ├── preview-42
        ├── preview-108
        └── preview-271
```

Databases with the same component/name share the same Neon database across environments. Each non-production environment creates a **branch** (copy-on-write), not a separate database. Preview branches fork from staging, so they always have the latest staging data.

### Vercel Project Sharing

All environments deploy to a **single Vercel project**. Each environment uses:
- **Environment targets**: `production` for the production environment, `preview` for all others
- **Aliases**: Per-environment DNS aliases (e.g., `preview-42.app.example.com`) for isolated routing

## Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `vercel_token` | string | *(required, sensitive)* | Vercel API token |
| `vercel_team_id` | string | `""` | Vercel team ID (optional for personal accounts) |
| `vercel_project_name` | string | *(required)* | Vercel project name (shared across all environments) |
| `neon_api_key` | string | *(required, sensitive)* | Neon API key |
| `neon_project_id` | string | *(required)* | Neon project ID (shared across all environments) |
| `upstash_api_key` | string | *(required, sensitive)* | Upstash API key |
| `upstash_email` | string | *(required)* | Upstash account email |
| `resend_api_key` | string | *(required, sensitive)* | Resend API key |
| `domain` | string | `""` | Base domain for routing (e.g., `app.example.com`) |
| `region` | string | `iad1` | Primary deployment region |

## Hooks Supported

| Hook | Implementation |
|------|---------------|
| database (PostgreSQL) | Neon managed database with tier-based branching |
| database (Redis) | Upstash managed Redis (per-environment) |
| databaseUser | Neon additional user credentials |
| task | Vercel Serverless Functions (one-time) |
| bucket | Vercel Blob Storage |
| encryptionKey (RSA/ECDSA) | Generated asymmetric keys |
| encryptionKey (symmetric) | Generated symmetric keys |
| smtp | Resend transactional email |
| secret | Vercel environment variables (scoped to tier) |
| deployment | Vercel Serverless Functions with environment targets |
| function | Vercel Serverless Functions with environment targets |
| service | Vercel internal service routing |
| ingress | Vercel routing with per-environment aliases |
| route | Vercel routing (alias for ingress) |
| cronjob | Vercel Cron Jobs |
| dockerBuild | Build and push to Vercel registry |

## Quick Start

```bash
# Build and push
arcctl dc build . -t ghcr.io/myorg/startup:v1
arcctl dc push ghcr.io/myorg/startup:v1

# Deploy datacenter (one datacenter for all environments)
arcctl dc deploy startup \
  --config ghcr.io/myorg/startup:v1 \
  --var vercel_token=$VERCEL_TOKEN \
  --var vercel_project_name=my-app \
  --var neon_api_key=$NEON_API_KEY \
  --var neon_project_id=$NEON_PROJECT_ID \
  --var upstash_api_key=$UPSTASH_API_KEY \
  --var upstash_email=$UPSTASH_EMAIL \
  --var resend_api_key=$RESEND_API_KEY \
  --var domain=app.example.com

# Create environments
arcctl env create production --datacenter startup
arcctl env create staging --datacenter startup

# Deploy to production
arcctl deploy ghcr.io/myorg/my-app:v1 -e production

# Deploy to staging (Neon branches from main)
arcctl deploy ghcr.io/myorg/my-app:v1 -e staging

# Create a preview environment for a PR (Neon branches from staging)
arcctl env create preview-42 --datacenter startup
arcctl deploy ghcr.io/myorg/my-app:pr-42 -e preview-42

# Tear down preview when PR is merged
arcctl destroy environment preview-42
```

## Notes

- **No MySQL support**: Neon only supports PostgreSQL. Use Upstash for Redis. If you need MySQL, consider using the AWS or DigitalOcean datacenters.
- **No VM-based deployments**: This datacenter is fully serverless. Container-based deployments are adapted to Vercel Serverless Functions. The `runtime` property is not supported.
- **Neon branching**: Preview branches fork from staging (not production), so they always start with the latest staging data without affecting production.
- **Vercel regions**: The `region` variable controls where serverless functions execute. Use Vercel's region codes (e.g., `iad1`, `sfo1`, `lhr1`).
- **Preview cleanup**: When you destroy a preview environment, the Neon branch and Vercel alias are removed. The shared Neon project and Vercel project remain intact.
