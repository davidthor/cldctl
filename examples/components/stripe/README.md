# Stripe Payment Platform Component

A configuration passthrough component for [Stripe](https://stripe.com) - a comprehensive payment processing platform.

## Overview

Stripe is a SaaS payment platform (no self-hosted option). This component centralizes Stripe configuration across your environment, allowing multiple applications to share consistent payment settings.

### Why Use This Pattern?

1. **Single Source of Truth**: Configure Stripe credentials once, use everywhere
2. **Environment Separation**: Different Stripe accounts for test vs live mode
3. **Dependency Visibility**: Clearly shows which apps handle payments
4. **Consistent Configuration**: Ensures all apps use the same payment settings
5. **Multi-Service Payments**: Backend handles charges, frontend handles checkout, webhooks process events

## Configuration

### Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `publishable_key` | Yes | Stripe publishable key (`pk_test_...` or `pk_live_...`) |
| `secret_key` | Yes | Stripe secret key (`sk_test_...` or `sk_live_...`) - **sensitive** |
| `webhook_secret` | No | Primary webhook signing secret (`whsec_...`) - **sensitive** |
| `webhook_secret_connect` | No | Connect webhook signing secret - **sensitive** |
| `api_version` | No | Pin to specific Stripe API version (e.g., `2024-12-18`) |
| `connect_client_id` | No | OAuth client ID for Stripe Connect |
| `customer_portal_enabled` | No | Enable Customer Portal (default: `true`) |
| `payment_methods` | No | Enabled payment methods (default: `card`) |
| `default_currency` | No | Default currency code (default: `usd`) |
| `test_mode` | No | Whether using test keys (default: `false`) |

### Outputs

All key variables are exposed as outputs for dependent components:

| Output | Description |
|--------|-------------|
| `publishable_key` | Publishable key for frontend SDKs (Stripe.js) |
| `secret_key` | Secret key for backend API calls (**sensitive**) |
| `webhook_secret` | Primary webhook signing secret (**sensitive**) |
| `webhook_secret_connect` | Connect webhook signing secret (**sensitive**) |
| `api_version` | Stripe API version |
| `connect_client_id` | Stripe Connect OAuth client ID |
| `default_currency` | Default currency for payments |
| `test_mode` | Whether using test mode |

## Usage

### 1. Environment Configuration

Configure Stripe in your `environment.yml`:

```yaml
# environment.yml
name: production
datacenter: ghcr.io/myorg/aws-datacenter:v1

components:
  # Deploy Stripe configuration component
  stripe:
    component: ghcr.io/myorg/stripe:v1
    variables:
      publishable_key: pk_live_xxxxxxxxxxxxx
      secret_key: ${{ secrets.stripe_secret_key }}
      webhook_secret: ${{ secrets.stripe_webhook_secret }}
      api_version: "2024-12-18"
      default_currency: usd

  # Your e-commerce application
  shop:
    component: ghcr.io/myorg/shop:v1
```

### 2. Dependent Application

In your application's `architect.yml`, declare Stripe as a dependency and access outputs:

```yaml
# shop/architect.yml

dependencies:
  stripe:
    component: ghcr.io/myorg/stripe:v1

deployments:
  api:
    build:
      context: ./api
    environment:
      # Access Stripe configuration via dependency outputs
      STRIPE_PUBLISHABLE_KEY: ${{ dependencies.stripe.outputs.publishable_key }}
      STRIPE_SECRET_KEY: ${{ dependencies.stripe.outputs.secret_key }}
      STRIPE_WEBHOOK_SECRET: ${{ dependencies.stripe.outputs.webhook_secret }}
      STRIPE_API_VERSION: ${{ dependencies.stripe.outputs.api_version }}

  webhook-handler:
    build:
      context: ./webhooks
    environment:
      STRIPE_SECRET_KEY: ${{ dependencies.stripe.outputs.secret_key }}
      STRIPE_WEBHOOK_SECRET: ${{ dependencies.stripe.outputs.webhook_secret }}

functions:
  web:
    build:
      context: ./web
    framework: nextjs
    environment:
      # Frontend only needs publishable key
      NEXT_PUBLIC_STRIPE_PUBLISHABLE_KEY: ${{ dependencies.stripe.outputs.publishable_key }}

# Services are only needed for deployments
services:
  api:
    deployment: api
    port: 3000

  webhooks:
    deployment: webhook-handler
    port: 3001

# Routes can point directly to functions
routes:
  main:
    type: http
    rules:
      - name: webhooks
        matches:
          - path:
              type: Exact
              value: /webhooks/stripe
        backendRefs:
          - service: webhooks
            port: 3001

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
          - function: web
```

### 3. Next.js + Stripe Example

For a Next.js app using `@stripe/stripe-js` and `stripe`:

```yaml
# nextjs-shop/architect.yml

dependencies:
  stripe:
    component: ghcr.io/myorg/stripe:v1

functions:
  web:
    build:
      context: .
    framework: nextjs
    environment:
      # Server-side
      STRIPE_SECRET_KEY: ${{ dependencies.stripe.outputs.secret_key }}
      STRIPE_WEBHOOK_SECRET: ${{ dependencies.stripe.outputs.webhook_secret }}
      # Client-side (prefixed with NEXT_PUBLIC_)
      NEXT_PUBLIC_STRIPE_PUBLISHABLE_KEY: ${{ dependencies.stripe.outputs.publishable_key }}

# Routes can point directly to functions
routes:
  main:
    type: http
    function: web
```

### 4. Microservices with Shared Stripe

For architectures with separate payment and order services:

```yaml
# environment.yml
components:
  stripe:
    component: ghcr.io/myorg/stripe:v1
    variables:
      publishable_key: pk_live_xxx
      secret_key: ${{ secrets.stripe_secret_key }}
      webhook_secret: ${{ secrets.stripe_webhook_secret }}

  # Payment service handles Stripe interactions
  payments:
    component: ghcr.io/myorg/payments:v1
    # Automatically gets stripe outputs via dependency

  # Order service needs to create payment intents
  orders:
    component: ghcr.io/myorg/orders:v1
    # Automatically gets stripe outputs via dependency

  # Frontend needs publishable key for Stripe.js
  storefront:
    component: ghcr.io/myorg/storefront:v1
    # Automatically gets stripe outputs via dependency
```

## Webhook Configuration

Configure webhooks in the Stripe Dashboard under **Developers → Webhooks**.

Common webhook events to handle:
- `checkout.session.completed` - Successful checkout
- `payment_intent.succeeded` - Payment confirmed
- `payment_intent.payment_failed` - Payment failed
- `customer.subscription.created` - New subscription
- `customer.subscription.updated` - Subscription changed
- `customer.subscription.deleted` - Subscription cancelled
- `invoice.paid` - Invoice payment received
- `invoice.payment_failed` - Invoice payment failed

## Design Notes

### Configuration Passthrough Pattern

This component demonstrates the "configuration passthrough" pattern where:

1. The component has no deployments, services, or infrastructure
2. Variables capture all necessary configuration
3. Outputs expose those values to dependent components
4. Dependent apps access values via `${{ dependencies.<name>.outputs.<field> }}`

This pattern is ideal for SaaS integrations where you want to:
- Centralize credentials management
- Ensure consistent configuration across services
- Make dependencies explicit and visible

### Test vs Live Mode

Use separate environments for test and live Stripe keys:

```yaml
# environments/staging/environment.yml
components:
  stripe:
    variables:
      publishable_key: pk_test_xxx
      secret_key: ${{ secrets.stripe_secret_key_test }}
      test_mode: "true"

# environments/production/environment.yml
components:
  stripe:
    variables:
      publishable_key: pk_live_xxx
      secret_key: ${{ secrets.stripe_secret_key_live }}
      test_mode: "false"
```

### Stripe Connect

For marketplace/platform architectures using Stripe Connect:

```yaml
stripe:
  component: ghcr.io/myorg/stripe:v1
  variables:
    publishable_key: pk_live_xxx
    secret_key: ${{ secrets.stripe_secret_key }}
    webhook_secret: ${{ secrets.stripe_webhook_secret }}
    webhook_secret_connect: ${{ secrets.stripe_webhook_secret_connect }}
    connect_client_id: ca_xxx
```

Your platform can then onboard connected accounts and process payments on their behalf.
