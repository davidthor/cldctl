# Clerk Authentication Component

A configuration passthrough component for [Clerk.com](https://clerk.com) - a modern authentication platform.

## Overview

Clerk is a SaaS authentication service (no self-hosted option). This component centralizes Clerk configuration across your environment, allowing multiple applications to share consistent auth settings.

**Note:** Clerk automatically infers the domain from the publishable key, so only the API keys are required.

### Why Use This Pattern?

1. **Single Source of Truth**: Configure Clerk credentials once, use everywhere
2. **Environment Separation**: Different Clerk instances for staging vs production
3. **Dependency Visibility**: Clearly shows which apps use Clerk authentication
4. **Consistent Configuration**: Ensures all apps use the same Clerk instance

## Configuration

### Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `publishable_key` | Yes | Clerk publishable key (`pk_test_...` or `pk_live_...`) |
| `secret_key` | Yes | Clerk secret key (`sk_test_...` or `sk_live_...`) - **sensitive** |
| `webhook_secret` | No | Webhook signing secret - **sensitive** |

### Outputs

| Output | Description |
|--------|-------------|
| `publishable_key` | Publishable key for frontend SDKs |
| `secret_key` | Secret key for backend verification (**sensitive**) |
| `webhook_secret` | Webhook signing secret (**sensitive**) |

## Usage

### 1. Environment Configuration

Configure Clerk in your `environment.yml`:

```yaml
# environment.yml
components:
  clerk:
    source: ghcr.io/myorg/clerk:v1
    variables:
      publishable_key: pk_live_xxxxxxxxxxxxx
      secret_key: ${{ secrets.clerk_secret_key }}
      webhook_secret: ${{ secrets.clerk_webhook_secret }}

  my-app:
    source: ghcr.io/myorg/my-app:v1
```

### 2. Dependent Application

In your application's `cld.yml`, declare Clerk as a dependency and access outputs:

```yaml
# my-app/cld.yml

dependencies:
  clerk:
    component: clerk

builds:
  api:
    context: ./api

deployments:
  api:
    image: ${{ builds.api.image }}
    environment:
      CLERK_SECRET_KEY: ${{ dependencies.clerk.outputs.secret_key }}
      CLERK_WEBHOOK_SECRET: ${{ dependencies.clerk.outputs.webhook_secret }}

functions:
  web:
    build:
      context: ./web
    framework: nextjs
    environment:
      NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY: ${{ dependencies.clerk.outputs.publishable_key }}
      # App-specific settings configured here, not in the Clerk component
      NEXT_PUBLIC_CLERK_SIGN_IN_URL: /sign-in
      NEXT_PUBLIC_CLERK_SIGN_UP_URL: /sign-up

# Services are only needed for deployments
services:
  api:
    deployment: api
    port: 3000

# Routes can point directly to functions
routes:
  main:
    type: http
    rules:
      - name: api
        matches:
          - path:
              type: PathPrefix
              value: /api
        backendRefs:
          - service: api
            port: 3000

      - name: web
        matches:
          - path:
              type: PathPrefix
              value: /
        backendRefs:
          - function: web  # Direct reference to function
```

## Design Notes

### Configuration Passthrough Pattern

This component demonstrates the "configuration passthrough" pattern:

1. No deployments, services, or infrastructure
2. Variables capture Clerk credentials
3. Outputs expose those values to dependent components
4. Apps access via `${{ dependencies.clerk.outputs.<field> }}`

### Test vs Live Mode

Use separate environments for test and live Clerk keys:

```yaml
# environments/staging/environment.yml
components:
  clerk:
    source: ../components/clerk
    variables:
      publishable_key: pk_test_xxx
      secret_key: ${{ secrets.clerk_secret_key_test }}

# environments/production/environment.yml
components:
  clerk:
    source: ghcr.io/myorg/clerk:v1
    variables:
      publishable_key: pk_live_xxx
      secret_key: ${{ secrets.clerk_secret_key_live }}
```
