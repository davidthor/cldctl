# arcctl Product Requirements Document

## Overview

arcctl is a command-line tool designed to help developers create and deploy cloud-native applications in a portable fashion—write once, deploy anywhere. The tool bridges the gap between application development and infrastructure management by separating concerns into three distinct configuration paradigms:

1. **Components** - Developer-focused application bundles
2. **Datacenters** - Platform engineer-focused infrastructure templates
3. **Environments** - Collaboration-focused deployment configurations

This separation enables true self-service: developers can create environments and deploy applications without infrastructure knowledge, while platform engineers maintain control over how infrastructure behaves.

---

## Goals

### Primary Goals

1. Enable developers to describe cloud applications without learning infrastructure-as-code (IaC)
2. Enable platform engineers to create reusable infrastructure templates that automatically provision resources based on application needs
3. Provide portable application bundles that can run in any datacenter without modification
4. Automate dependency management, service discovery, and credential generation
5. Support on-demand ephemeral environments for development and testing

### Non-Goals (for initial version)

1. Replacing existing IaC tools (arcctl is an abstraction layer on top of IaC)
2. Managing non-application infrastructure (VPCs, base clusters, etc. are handled by datacenter modules)
3. Providing a hosted platform (arcctl is a CLI tool with local or remote state)

---

## Target Users

### Developers

- Want to focus on application code, not infrastructure
- Need to run their applications locally and in cloud environments
- Want to create test/preview environments easily
- Need to integrate with other teams' services without complex setup

### Platform Engineers / DevOps

- Responsible for cloud infrastructure and security
- Want to provide self-service capabilities to developers
- Need to enforce organizational standards and policies
- Want to manage infrastructure changes without coordinating with every application team

---

## Core Concepts

### The Contract

Components and Datacenters communicate through a resource contract. When a component is deployed:

1. arcctl extracts all cloud resources from the component (databases, services, deployments, etc.)
2. Each resource is matched against hooks defined in the datacenter
3. The datacenter fulfills each resource request using its configured IaC modules
4. Output values (URLs, credentials, etc.) are passed back to dependent resources

This contract allows components to be portable across any datacenter that implements the required resource hooks.

---

## File Specifications

### 1. Component File (`architect.yml`)

The component file describes an application bundle using a docker-compose-like syntax. Developers define what their application needs without specifying how infrastructure should be provisioned.

#### Purpose

- Define application workloads (deployments, cronjobs)
- Declare database requirements
- Expose internal services and external routes (Gateway API pattern)
- Specify dependencies on other components
- Define configurable variables

#### File Location

- Default: `architect.yml` in the project root
- Can be specified explicitly via CLI

#### Top-Level Structure

```yaml
# Component metadata
name: string # Required. Unique identifier for the component
description: string # Optional. Human-readable description

# Application resources
databases: map<string, Database>
buckets: map<string, Bucket>
deployments: map<string, Deployment>
functions: map<string, Function>
services: map<string, Service>
routes: map<string, Route>
cronjobs: map<string, Cronjob>

# Configuration
variables: map<string, Variable>
dependencies: map<string, Dependency>
```

#### Resource Types

##### Databases

Declare database requirements. The datacenter determines how databases are provisioned (shared cluster, dedicated instance, etc.).

```yaml
databases:
  main:
    type: postgres:^15 # Required. Database type (postgres, mysql, mongodb, redis, etc.), with optional semvar constraint for database version that matches package.json character use
```

**Database with migrations/seeding:**

Databases can optionally include migration or seeding configuration. This allows you to define a container that runs database migrations or seeds data after the database is provisioned.

```yaml
databases:
  main:
    type: postgres:^15

    # Option 1: Build from source (for development and CI builds)
    migrations:
      build:
        context: ./database        # Required. Build context directory for the migration container
        dockerfile: Dockerfile     # Optional. Defaults to "Dockerfile"
      command: ["npm", "run", "migrate"]  # Optional. Command to run migrations (defaults to container entrypoint)
      environment:                 # Optional. Additional environment variables for migration container
        MIGRATION_MODE: "up"
      
  analytics:
    type: postgres:^15
    
    # Option 2: Use pre-built image (for compiled components or shared migration images)
    migrations:
      image: myregistry.io/myorg/analytics-migrations:v1.0.0
      command: ["python", "seed.py", "--env", "production"]
```

**Note:** When a component is built using `arcctl build component`, any `build:` blocks are compiled into `image:` references pointing to the built child artifacts. This allows the compiled component to be portable and self-contained.

**Database properties:**

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `type` | string | required | Database type with optional semver constraint (`postgres:^15`, `mysql:^8`, etc.) |
| `migrations` | object | - | Optional migration/seeding configuration |
| `migrations.build` | object | - | Build configuration for migration container (mutually exclusive with `image`) |
| `migrations.build.context` | string | required | Build context directory |
| `migrations.build.dockerfile` | string | `"Dockerfile"` | Path to Dockerfile within context |
| `migrations.image` | string | - | Pre-built migration container image (mutually exclusive with `build`) |
| `migrations.command` | string[] | - | Command to run migrations (overrides container default) |
| `migrations.environment` | map | - | Additional environment variables for migration container |

**Notes:**

- The migration container automatically receives `DATABASE_URL`, `DATABASE_HOST`, `DATABASE_PORT`, `DATABASE_NAME`, `DATABASE_USER`, and `DATABASE_PASSWORD` environment variables
- Migrations run as a one-time job after the database is provisioned and before dependent deployments start
- If migrations fail, the deployment is halted and marked as failed
- The datacenter controls how migration jobs are executed (Kubernetes Job, ECS Task, local Docker, etc.)

**Supported database types:**

- `postgres`
- `mysql`
- `mongodb`
- `redis`
- (extensible based on datacenter support)

##### Buckets

Declare blob storage requirements. The datacenter determines how storage is provisioned (AWS S3, MinIO, Cloudflare R2, DigitalOcean Spaces, etc.).

```yaml
buckets:
  uploads:
    type: s3 # Required. Storage type (s3, gcs, azure-blob)

  media:
    type: s3
    versioning: true # Optional. Enable object versioning (default: false)
    public: false # Optional. Allow public read access (default: false)
```

**Supported bucket types:**

- `s3` - S3-compatible storage (AWS S3, MinIO, R2, DigitalOcean Spaces, etc.)
- `gcs` - Google Cloud Storage (planned)
- `azure-blob` - Azure Blob Storage (planned)

**Bucket properties:**

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `type` | string | required | Storage type (`s3`, `gcs`, `azure-blob`) |
| `versioning` | boolean | `false` | Enable object versioning |
| `public` | boolean | `false` | Allow public read access to objects |

**Bucket outputs available via expressions:**

- `${{ buckets.<name>.endpoint }}` - S3-compatible endpoint URL
- `${{ buckets.<name>.bucket }}` - Bucket name
- `${{ buckets.<name>.region }}` - Bucket region (if applicable)
- `${{ buckets.<name>.accessKeyId }}` - Access key ID for authentication
- `${{ buckets.<name>.secretAccessKey }}` - Secret access key for authentication

**Example usage:**

```yaml
deployments:
  api:
    build:
      context: ./api
    environment:
      S3_ENDPOINT: ${{ buckets.uploads.endpoint }}
      S3_BUCKET: ${{ buckets.uploads.bucket }}
      S3_REGION: ${{ buckets.uploads.region }}
      S3_ACCESS_KEY_ID: ${{ buckets.uploads.accessKeyId }}
      S3_SECRET_ACCESS_KEY: ${{ buckets.uploads.secretAccessKey }}
```

##### Encryption Keys

Declare cryptographic key requirements. The datacenter is responsible for generating and managing these keys securely. This resource type supports asymmetric key pairs (RSA, ECDSA) for signing/verification, symmetric keys for encryption, and random salts for key derivation.

```yaml
encryptionKeys:
  # RSA key pair for webhook signatures
  webhook-signing:
    type: rsa
    bits: 2048  # Optional: 2048, 3072, 4096 (default: 2048)

  # ECDSA key pair for JWT signing
  jwt-signing:
    type: ecdsa
    curve: P-256  # Optional: P-256, P-384, P-521 (default: P-256)

  # Symmetric key for encryption, secrets, or salts
  session-key:
    type: symmetric
    bytes: 32  # Key length in bytes (default: 32)
```

**Supported key types:**

| Type | Description | Use Cases |
|------|-------------|-----------|
| `rsa` | RSA asymmetric key pair | Webhook signatures, JWT signing, encryption |
| `ecdsa` | ECDSA asymmetric key pair | JWT signing, digital signatures |
| `symmetric` | Random symmetric key/secret | Session encryption, AES keys, secret tokens, salts, key derivation |

**Encryption key properties:**

| Property | Type | Default | Applies To | Description |
|----------|------|---------|------------|-------------|
| `type` | string | required | all | Key type (`rsa`, `ecdsa`, `symmetric`) |
| `bits` | number | `2048` | rsa | RSA key size (2048, 3072, or 4096) |
| `curve` | string | `P-256` | ecdsa | ECDSA curve (P-256, P-384, or P-521) |
| `bytes` | number | `32` | symmetric | Key length in bytes |

**Encryption key outputs available via expressions:**

For asymmetric keys (`rsa`, `ecdsa`):
- `${{ encryptionKeys.<name>.privateKey }}` - Private key in PEM format
- `${{ encryptionKeys.<name>.publicKey }}` - Public key in PEM format
- `${{ encryptionKeys.<name>.privateKeyBase64 }}` - Private key PEM, base64-encoded
- `${{ encryptionKeys.<name>.publicKeyBase64 }}` - Public key PEM, base64-encoded

For symmetric keys and salts (`symmetric`, `salt`):
- `${{ encryptionKeys.<name>.key }}` - Key/salt as hex-encoded string
- `${{ encryptionKeys.<name>.keyBase64 }}` - Key/salt as base64-encoded string

**Example usage:**

```yaml
encryptionKeys:
  rsa-key:
    type: rsa
    bits: 2048
  
  session-secret:
    type: symmetric
    bytes: 64

  encryption-salt:
    type: symmetric
    bytes: 32

deployments:
  api:
    build:
      context: ./api
    environment:
      # RSA key for webhook signatures (base64-encoded for env var safety)
      RSA_PRIVATE_KEY: ${{ encryptionKeys.rsa-key.privateKeyBase64 }}
      # Symmetric key for session encryption (hex format)
      SECRET_KEY_BASE: ${{ encryptionKeys.session-secret.key }}
      # Salt for key derivation (also uses symmetric type)
      ENCRYPTION_SALT: ${{ encryptionKeys.encryption-salt.keyBase64 }}
```

##### SMTP

Declare email sending requirements. The datacenter is responsible for provisioning SMTP credentials, which may be fulfilled by various email providers (AWS SES, SendGrid, Mailgun, Postmark, or self-hosted SMTP servers).

```yaml
smtp:
  email: {}  # Empty object - datacenter provisions credentials
  
  # Or with optional description
  notifications:
    description: "For transactional emails"
```

**SMTP properties:**

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `description` | string | optional | Human-readable description of the SMTP connection |

**SMTP outputs available via expressions:**

- `${{ smtp.<name>.host }}` - SMTP server hostname
- `${{ smtp.<name>.port }}` - SMTP server port (typically 587 or 465)
- `${{ smtp.<name>.username }}` - Authentication username
- `${{ smtp.<name>.password }}` - Authentication password

**Example usage:**

```yaml
smtp:
  notifications: {}

deployments:
  api:
    build:
      context: ./api
    environment:
      SMTP_HOST: ${{ smtp.notifications.host }}
      SMTP_PORT: ${{ smtp.notifications.port }}
      SMTP_USERNAME: ${{ smtp.notifications.username }}
      SMTP_PASSWORD: ${{ smtp.notifications.password }}
```

##### Deployments

Define long-running application workloads (e.g., backend APIs, monolithic applications, microservices).

```yaml
deployments:
  api:
    image: node:18 # Image to run (mutually exclusive with build)
    build: # Build configuration (mutually exclusive with image)
      context: ./api # Build context directory
      dockerfile: Dockerfile # Optional. Defaults to "Dockerfile"
    command: ["npm", "start"] # Optional. Override container command
    entrypoint: ["/bin/sh"] # Optional. Override container entrypoint
    environment: # Environment variables
      DATABASE_URL: ${{ databases.main.url }}
      LOG_LEVEL: ${{ variables.log_level }}
    cpu: "0.5" # Optional. CPU allocation (cores)
    memory: "512Mi" # Optional. Memory allocation
    replicas: 1 # Optional. Default replica count
    volumes: # Optional. Volume mounts
      - mount_path: /data
        host_path: ./data # For local development
    liveness_probe: # Optional. Health check configuration
      path: /health
      port: 8080
      initial_delay_seconds: 10
      period_seconds: 30
    readiness_probe: # Optional. Readiness check
      path: /ready
      port: 8080
```

##### Functions

Define serverless functions for modern JavaScript frameworks (e.g., Next.js, Nuxt, SvelteKit) and event-driven workloads. Functions are auto-scaling, stateless compute units suitable for platforms like Vercel, AWS Lambda, Google Cloud Run, etc.

```yaml
functions:
  web:
    build:
      context: ./web # Build context directory
      dockerfile: Dockerfile # Optional. Custom Dockerfile
    runtime: nodejs:20 # Optional. Runtime specification (nodejs:20, python:3.11, go:1.21, etc.)
    framework: nextjs # Optional. Framework hint for specialized providers (nextjs, nuxt, sveltekit, remix)
    environment: # Environment variables
      DATABASE_URL: ${{ databases.main.url }}
      API_URL: ${{ services.api.url }}
    cpu: "1" # Optional. CPU allocation per invocation
    memory: "1024Mi" # Optional. Memory allocation per invocation
    timeout: 60 # Optional. Timeout in seconds (default varies by platform)
```

**Function properties:**

- `build` - Build configuration (mutually exclusive with `image`)
- `image` - Pre-built container image (mutually exclusive with `build`)
- `runtime` - Runtime/language specification (nodejs:20, python:3.11, go:1.21, etc.)
- `framework` - Optional framework hint for providers with framework-specific optimizations (e.g., Vercel for Next.js)
- `environment` - Environment variables
- `cpu` - CPU allocation per invocation
- `memory` - Memory allocation per invocation
- `timeout` - Maximum execution time in seconds

**Function outputs available via expressions:**

- `${{ functions.<name>.url }}` - Public URL to the function (typically assigned by route or platform)

**Notes:**

- Most serverless providers (Lambda, Cloud Run) only need `runtime` to specify the language/version
- The `framework` field is optional and primarily useful for specialized providers like Vercel that have framework-specific optimizations
- Infrastructure concerns like regions are configured in the environment or datacenter, not the component

##### Services

Expose deployments for internal traffic within the environment.

> **Note:** Functions don't need services. Routes can point directly to functions. Services are only needed for deployments (long-running processes) that require internal service discovery.

```yaml
services:
  api:
    deployment: api # Target deployment name
    port: 8080 # Port the deployment listens on
    protocol: http # Optional. Protocol (http, https, tcp, grpc)

  # External/virtual service (for migration or egress)
  legacy-api:
    url: https://legacy.example.com
```

**Service outputs available via expressions:**

- `${{ services.<name>.url }}` - Full URL to the service
- `${{ services.<name>.host }}` - Hostname only
- `${{ services.<name>.port }}` - Port number

##### Routes

Define external traffic routing rules using the Gateway API pattern. Routes specify **application-level** routing logic (matching, backends, filters), while infrastructure concerns (gateways, TLS, hostnames) are handled by the datacenter and environment.

**HTTP Routes**

Route HTTP traffic with expressive matching capabilities:

```yaml
routes:
  api:
    type: http # Route type: http or grpc
    internal: false # Optional. If true, route is only accessible within the VPC (default: false)
    rules:
      - name: v1-api # Optional. Rule name for identification
        matches: # Match conditions (OR logic between items)
          - path:
              type: PathPrefix # PathPrefix, Exact, or RegularExpression
              value: /api/v1
            headers: # Optional. Header matching (AND with path)
              - name: x-api-version
                type: Exact # Exact or RegularExpression
                value: "1"
            method: GET # Optional. HTTP method filter
          - path:
              type: PathPrefix
              value: /v1
        backendRefs: # Target backends with optional weights
          - service: api # Target service name
            port: 8080 # Optional. Defaults to service port
            weight: 90 # Optional. Traffic weight (default: 1)
          - service: api-canary
            port: 8080
            weight: 10
        filters: # Optional. Request/response processing
          - type: RequestHeaderModifier
            requestHeaderModifier:
              add:
                - name: x-request-id
                  value: ${{ uuid() }}
              set:
                - name: x-backend-version
                  value: v1
              remove:
                - x-internal-header
        timeouts: # Optional. Timeout configuration
          request: 30s # Total request timeout
          backendRequest: 10s # Per-backend request timeout

      - name: v2-api
        matches:
          - path:
              type: PathPrefix
              value: /api/v2
            queryParams: # Optional. Query parameter matching
              - name: version
                type: Exact
                value: "2"
        backendRefs:
          - service: api-v2
            port: 8080

      - name: legacy-redirect
        matches:
          - path:
              type: Exact
              value: /old-endpoint
        filters:
          - type: RequestRedirect
            requestRedirect:
              scheme: https
              hostname: new-api.example.com
              port: 443
              statusCode: 301
```

**gRPC Routes**

Route gRPC traffic with service/method matching:

```yaml
routes:
  grpc-api:
    type: grpc
    rules:
      - name: user-service
        matches:
          - method:
              service: com.example.UserService # gRPC service name
              method: GetUser # Optional. Specific method (omit for all methods)
            headers:
              - name: x-tenant-id
                type: Exact
                value: "acme"
        backendRefs:
          - service: user-grpc
            port: 50051
            weight: 100

      - name: all-services
        matches:
          - method:
              service: com.example.* # Wildcard matching
        backendRefs:
          - service: grpc-gateway
            port: 50051
```

**Simplified Routes**

For simple use cases, routes support a shorthand syntax:

```yaml
routes:
  web:
    type: http
    service: frontend # Shorthand: routes all traffic to this service
    port: 3000

  api:
    type: http
    function: api-handler # Route to a function
```

**Route Properties**

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `type` | string | required | Route type: `http` or `grpc` |
| `internal` | boolean | `false` | If true, route is only accessible within the VPC (for admin panels, internal APIs) |
| `rules` | RouteRule[] | required | Array of routing rules |

**Route Match Types**

| Match Type | Description | Supported In |
|------------|-------------|--------------|
| `path` | URL path matching (PathPrefix, Exact, RegularExpression) | HTTP |
| `headers` | HTTP header matching | HTTP, gRPC |
| `queryParams` | Query parameter matching | HTTP |
| `method` (HTTP) | HTTP method (GET, POST, etc.) | HTTP |
| `method` (gRPC) | gRPC service/method matching | gRPC |

**Route Filter Types**

| Filter Type | Description |
|-------------|-------------|
| `RequestHeaderModifier` | Add, set, or remove request headers |
| `ResponseHeaderModifier` | Add, set, or remove response headers |
| `RequestRedirect` | Redirect requests to a different URL |
| `URLRewrite` | Rewrite the request URL path or hostname |
| `RequestMirror` | Mirror requests to another backend |

**Route outputs:**

- `${{ routes.<name>.url }}` - Public URL assigned by the datacenter/environment
- `${{ routes.<name>.hosts }}` - Array of assigned hostnames

##### Cronjobs

Define scheduled tasks.

```yaml
cronjobs:
  daily-report:
    schedule: "0 3 * * *" # Required. Cron expression
    image: node:18 # Image to run (or use build)
    build:
      context: ./jobs
    command: ["npm", "run", "report"]
    environment:
      API_URL: ${{ services.api.url }}
    cpu: "0.25"
    memory: "256Mi"
```

##### Variables

Define configurable inputs for the component.

```yaml
variables:
  log_level:
    description: "Application log level"
    default: "info"
    required: false

  api_key:
    description: "External API key"
    required: true
    sensitive: true # Marked as sensitive
```

**Variable outputs:**

- `${{ variables.<name> }}` - Variable value

##### Dependencies

Declare dependencies on other components.

```yaml
dependencies:
  auth:
    component: registry.example.com/org/auth-service:v1.0.0
    variables:
      log_level: debug

  payments:
    component: ./path/to/local/component # Local path for development
```

**Dependency outputs:**

- `${{ dependencies.<name>.services.<service>.url }}` - Service URL from dependency
- `${{ dependencies.<name>.routes.<route>.url }}` - Route URL from dependency

##### Dependents

Access information about upstream components that depend on this component. This is useful for scenarios like dynamic CORS configuration, where an API needs to know which frontends will call it.

**Dependent outputs:**

- `${{ dependents.<name>.services.<service>.url }}` - Service URL from a specific dependent
- `${{ dependents.<name>.routes.<route>.url }}` - Route URL from a specific dependent
- `${{ dependents.*.routes.*.url }}` - Array of all route URLs from all dependents (wildcard syntax)

**Example - Dynamic CORS from dependent frontends:**

```yaml
deployments:
  api:
    environment:
      # Collect all route URLs from components that depend on this API
      CORS_ORIGINS: ${{ dependents.*.routes.*.url | join "," }}
```

#### Expression Syntax

Components use a template expression syntax to reference values from other resources:

```
${{ <resource_type>.<resource_name>.<property> }}
```

**Examples:**

```yaml
environment:
  DATABASE_URL: ${{ databases.main.url }}
  AUTH_SERVICE_URL: ${{ dependencies.auth.services.api.url }}
  PUBLIC_URL: ${{ routes.main.url }}
  LOG_LEVEL: ${{ variables.log_level }}
```

#### Complete Example

```yaml
name: my-app
description: A sample web application with Next.js frontend

variables:
  log_level:
    default: "info"
  stripe_key:
    required: true
    secret: true

databases:
  main:
    type: postgres:^15
    migrations:
      build:
        context: ./database/migrations
      command: ["npm", "run", "migrate:up"]
  cache:
    type: redis

buckets:
  uploads:
    type: s3
    versioning: true

deployments:
  api:
    build:
      context: ./api
    environment:
      DATABASE_URL: ${{ databases.main.url }}
      REDIS_URL: ${{ databases.cache.url }}
      LOG_LEVEL: ${{ variables.log_level }}
      STRIPE_KEY: ${{ variables.stripe_key }}
      S3_ENDPOINT: ${{ buckets.uploads.endpoint }}
      S3_BUCKET: ${{ buckets.uploads.bucket }}
      S3_ACCESS_KEY_ID: ${{ buckets.uploads.accessKeyId }}
      S3_SECRET_ACCESS_KEY: ${{ buckets.uploads.secretAccessKey }}
    cpu: "0.5"
    memory: "512Mi"
    liveness_probe:
      path: /health
      port: 8080

  worker:
    build:
      context: ./worker
    environment:
      DATABASE_URL: ${{ databases.main.url }}
      REDIS_URL: ${{ databases.cache.url }}

functions:
  web:
    build:
      context: ./web
    framework: nextjs
    environment:
      DATABASE_URL: ${{ databases.main.url }}
      NEXT_PUBLIC_API_URL: ${{ services.api.url }}
    memory: "1024Mi"
    timeout: 60

services:
  api:
    deployment: api
    port: 8080

  web:
    function: web

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
            port: 8080
      - name: web
        matches:
          - path:
              type: PathPrefix
              value: /
        backendRefs:
          - service: web

cronjobs:
  cleanup:
    schedule: "0 0 * * *"
    build:
      context: ./jobs
    command: ["node", "cleanup.js"]
    environment:
      DATABASE_URL: ${{ databases.main.url }}

dependencies:
  auth:
    component: registry.example.com/org/auth:latest
```

---

### 2. Datacenter File (`datacenter.dc`)

The datacenter file defines infrastructure templates and rules for how application resources should be provisioned. It acts as the bridge between application requirements and actual cloud infrastructure. Datacenters are written in **arcctl's HCL-based configuration language**, inspired by Terraform and OpenTofu syntax, making it familiar to platform engineers and DevOps practitioners while providing arcctl-specific constructs for resource hooks and module orchestration.

#### Purpose

- Define IaC modules for provisioning infrastructure
- Configure resource hooks to fulfill component requests
- Manage environment-level resources (namespaces, DNS zones)
- Set datacenter-wide variables and policies

#### File Location

- Default: `datacenter.dc` in the project root
- Can be specified explicitly via CLI

#### Top-Level Structure

```hcl
# Variables for datacenter configuration
variable "cluster_name" {
  description = "Kubernetes cluster name"
  type        = string
}

variable "region" {
  description = "Cloud region"
  type        = string
  default     = "us-east-1"
}

# Datacenter-level modules (shared across all environments)
module "k8s" {
  build = "./k8s-cluster"
  inputs = {
    name   = "${datacenter.name}-cluster"
    region = variable.region
  }
}

# Environment configuration with modules and resource hooks
environment {
  module "namespace" {
    build = "./k8s-namespace"
    inputs = {
      name       = environment.name
      kubeconfig = module.k8s.kubeconfig
    }
  }

  # Resource hooks
  database {
    # ...
  }

  deployment {
    # ...
  }
}
```

#### Modules

Modules are containers for IaC templates. Supported frameworks:

- **Pulumi** (default)
- **OpenTofu** (Terraform-compatible)
- **CloudFormation** (planned)

```hcl
# Datacenter-level module (lives/dies with datacenter lifecycle)
module "k8s" {
  build = "./k8s-cluster"  # Local path to build from
  # OR
  source = "registry.example.com/org/k8s-cluster:v1.0.0"  # Pre-built image

  plugin = "pulumi"  # IaC framework (pulumi, opentofu)

  inputs = {
    name   = "${datacenter.name}-cluster"
    region = variable.region
    vpc_id = module.vpc.id
    "digitalocean:token" = variable.do_token
  }
}

# OpenTofu module example
module "vpc" {
  plugin = "opentofu"
  build  = "./vpc-module"
  inputs = {
    name = var.vpc_name
  }
}
```

**Module properties:**

- `build` - Local directory containing IaC code (source form, mutually exclusive with `source`)
- `source` - Pre-built module image reference (compiled form, mutually exclusive with `build`)
- `plugin` - IaC framework: `pulumi` (default), `opentofu`, or `native`
- `inputs` - Map of values passed to the module
- `when` - Conditional expression for when to invoke the module

**Note:** When a datacenter is built using `arcctl build datacenter`, any `build` properties are compiled into `source` references pointing to the built module artifacts. The compiled datacenter can then be pushed to a registry and used without access to the original source code.

#### The Native Plugin

The `native` plugin provides a lightweight alternative to Pulumi and OpenTofu for scenarios where external IaC tooling overhead is undesirable. It's ideal for:

- **Local development** - Fast startup without IaC tool initialization
- **Ephemeral environments** - Quick provisioning of short-lived resources
- **Simple operations** - Docker containers, processes, shell commands

Unlike Pulumi or OpenTofu, the native plugin:
- Executes commands directly without external tooling
- Stores outputs in arcctl's state (enabling dependency wiring)
- Skips drift detection and state refresh (trusts stored state)
- Has no initialization overhead

Native modules define resources using a declarative YAML format instead of IaC code:

```yaml
# modules/docker-postgres/module.yml
plugin: native
type: docker

inputs:
  name:
    type: string
    required: true
  version:
    type: string
    default: "16"
  database:
    type: string
    required: true

resources:
  container:
    type: docker:container
    properties:
      image: "postgres:${inputs.version}"
      name: "${inputs.name}"
      environment:
        POSTGRES_DB: "${inputs.database}"
        POSTGRES_USER: "app"
        POSTGRES_PASSWORD: "${random_password(16)}"
      ports:
        - container: 5432
          host: auto
      volumes:
        - name: "${inputs.name}-data"
          path: /var/lib/postgresql/data
      healthcheck:
        command: ["pg_isready", "-U", "app"]
        interval: 5s
        retries: 5

outputs:
  host:
    value: "localhost"
  port:
    value: "${resources.container.ports[0].host}"
  database:
    value: "${inputs.database}"
  username:
    value: "app"
  password:
    value: "${resources.container.environment.POSTGRES_PASSWORD}"
    sensitive: true
  url:
    value: "postgres://app:${resources.container.environment.POSTGRES_PASSWORD}@localhost:${resources.container.ports[0].host}/${inputs.database}"
    sensitive: true
```

**Native Resource Types:**

| Type | Description |
|------|-------------|
| `docker:container` | Run a Docker container |
| `docker:network` | Create a Docker network |
| `docker:volume` | Create a Docker volume |
| `process` | Run a local process |
| `exec` | Execute a one-time command |

##### docker:container Properties

| Property | Type | Description |
|----------|------|-------------|
| `image` | string | Docker image to run |
| `name` | string | Container name |
| `command` | list | Override container command |
| `entrypoint` | list | Override container entrypoint |
| `environment` | map | Environment variables |
| `ports` | list | Port mappings with `container` and `host` (or `auto`) |
| `volumes` | list | Volume mounts with `name`/`source` and `path` |
| `network` | string | Docker network to join |
| `healthcheck` | object | Health check configuration |
| `restart` | string | Restart policy (`no`, `always`, `on-failure`) |

##### process Properties

| Property | Type | Description |
|----------|------|-------------|
| `command` | list | Command to execute |
| `working_dir` | string | Working directory |
| `environment` | map | Environment variables |
| `readiness` | object | Readiness check (http, tcp, or exec) |
| `graceful_stop` | object | Graceful shutdown configuration |

##### Using Native Modules in Hooks

```hcl
environment {
  database {
    when = node.inputs.databaseType == "postgres"
    
    module "postgres" {
      plugin = "native"
      build  = "./modules/docker-postgres"
      inputs = {
        name     = "${environment.name}-${node.name}"
        version  = node.inputs.databaseVersion
        database = node.name
      }
    }
    
    outputs = {
      host     = module.postgres.host
      port     = module.postgres.port
      database = module.postgres.database
      url      = module.postgres.url
    }
  }
  
  deployment {
    module "container" {
      plugin = "native"
      build  = "./modules/docker-deployment"
      inputs = {
        name        = "${environment.name}-${node.component}-${node.name}"
        image       = node.inputs.image
        environment = node.inputs.environment
        ports       = node.inputs.ports
      }
    }
    
    outputs = {
      id = module.container.container_id
    }
  }
}
```

##### Native Plugin Lifecycle

**Apply:**
1. Parse module definition
2. Resolve input expressions
3. Create/update resources directly (docker run, process start)
4. Store resource IDs and outputs in state
5. Return outputs for dependency wiring

**Destroy:**
1. Read resource IDs from state
2. Stop/remove resources directly (docker rm, process kill)
3. Clean up state

**Key difference from Pulumi/OpenTofu:** No state refresh or drift detection. The native plugin trusts that state is accurate, making operations much faster but less robust for long-lived environments.

#### Environment Configuration

The `environment` block defines modules and hooks that respond to environment and resource lifecycle events.

```hcl
environment {
  # Modules invoked once per environment
  module "namespace" {
    build = "./modules/namespace"
    inputs = {
      name = environment.name
    }
  }

  # Gateway infrastructure (created once per environment)
  module "gateway" {
    build = "./modules/gateway"
    when  = length(environment.resources.routes) > 0
    inputs = {
      name       = "${environment.name}-gateway"
      namespace  = environment.name
      domain     = "${environment.name}.${variable.domain}"
      tls_issuer = "letsencrypt-prod"
    }
  }

  module "postgres_cluster" {
    build = "./modules/postgres"
    when  = contains(environment.nodes.*.inputs.databaseType, "postgres")
    inputs = {
      namespace = environment.name
    }
  }

  # Resource hooks (see below)
  database {
    # ...
  }

  deployment {
    # ...
  }

  service {
    # ...
  }

  ingress {
    # ...
  }

  cronjob {
    # ...
  }

  secret {
    # ...
  }

  databaseUser {
    # ...
  }

  dockerBuild {
    # ...
  }
}
```

#### Resource Hooks

Hooks define how each resource type from components gets fulfilled. Each hook has access to the resource inputs (`node.inputs`) and must provide the required outputs. Hooks are declared directly inside the `environment` block using the resource type as the block name.

##### Database Hook

```hcl
environment {
  database {
    when = node.inputs.databaseType == "postgres"

    module "db_cluster" {
      build = "./modules/database"
      inputs = {
        name    = node.name
        type    = node.inputs.databaseType
        version = node.inputs.databaseVersion
      }
    }

    outputs = {
      host     = module.db_cluster.host
      port     = module.db_cluster.port
      database = module.db_cluster.database
      url      = module.db_cluster.url
    }
  }
}
```

**Inputs (available via `node.inputs`):**
| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Resource identifier |
| `databaseType` | string | Database type (postgres, mysql, etc.) |
| `databaseVersion` | string | Requested version |
| `migrations` | object | Optional migration/seeding configuration (see below) |

**Migration inputs (available via `node.inputs.migrations`):**
| Field | Type | Description |
|-------|------|-------------|
| `image` | string | Built migration container image (from dockerBuild hook) |
| `command` | string[] | Command to run migrations |
| `environment` | map | Additional environment variables for migration container |

**Required Outputs:**
| Field | Type | Description |
|-------|------|-------------|
| `host` | string | Database hostname |
| `port` | number | Database port |
| `database` | string | Database name |
| `url` | string | Full connection URL |

##### Database Migration Hook

When a database has migrations configured, the datacenter receives a `databaseMigration` resource after the database is provisioned. This hook is responsible for running the migration container.

```hcl
environment {
  databaseMigration {
    module "migration_job" {
      build = "./modules/k8s-job"
      inputs = {
        name        = "${node.component}--${node.name}--migration"
        namespace   = module.namespace.id
        kubeconfig  = module.k8s.kubeconfig
        image       = node.inputs.image
        command     = node.inputs.command
        environment = merge(node.inputs.environment, {
          DATABASE_URL      = node.inputs.databaseUrl
          DATABASE_HOST     = node.inputs.databaseHost
          DATABASE_PORT     = node.inputs.databasePort
          DATABASE_NAME     = node.inputs.databaseName
          DATABASE_USER     = node.inputs.databaseUser
          DATABASE_PASSWORD = node.inputs.databasePassword
        })
      }
    }

    outputs = {
      id     = module.migration_job.job_id
      status = module.migration_job.status
    }
  }
}
```

**Inputs (available via `node.inputs`):**
| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Migration job name |
| `image` | string | Built migration container image |
| `command` | string[] | Command to run migrations |
| `environment` | map | Additional environment variables from component |
| `databaseUrl` | string | Full database connection URL |
| `databaseHost` | string | Database hostname |
| `databasePort` | number | Database port |
| `databaseName` | string | Database name |
| `databaseUser` | string | Database username |
| `databasePassword` | string | Database password |

**Required Outputs:**
| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique migration job identifier |
| `status` | string | Job completion status (success, failed) |

**Notes:**

- The migration job runs as a one-time task (Kubernetes Job, ECS Task, etc.)
- arcctl waits for the migration job to complete before starting dependent deployments
- If the migration fails, the deployment is halted
- The datacenter may implement retry logic or rollback strategies

##### Bucket Hook

```hcl
environment {
  bucket {
    module "s3_bucket" {
      build = "./modules/s3-bucket"
      inputs = {
        name       = node.name
        type       = node.inputs.type
        versioning = node.inputs.versioning
        public     = node.inputs.public
      }
    }

    outputs = {
      endpoint        = module.s3_bucket.endpoint
      bucket          = module.s3_bucket.bucket_name
      region          = module.s3_bucket.region
      accessKeyId     = module.s3_bucket.access_key_id
      secretAccessKey = module.s3_bucket.secret_access_key
    }
  }
}
```

**Inputs (available via `node.inputs`):**
| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Resource identifier |
| `type` | string | Storage type (s3, gcs, azure-blob) |
| `versioning` | boolean | Enable object versioning |
| `public` | boolean | Allow public read access |

**Required Outputs:**
| Field | Type | Description |
|-------|------|-------------|
| `endpoint` | string | S3-compatible endpoint URL |
| `bucket` | string | Bucket name |
| `region` | string | Bucket region |
| `accessKeyId` | string | Access key ID |
| `secretAccessKey` | string | Secret access key |

##### Encryption Key Hook

The encryption key hook generates cryptographic keys when components declare encryption key requirements. It supports multiple key types (RSA, ECDSA, symmetric, salt) and must return the appropriate outputs based on the key type.

```hcl
environment {
  encryptionKey {
    when = node.inputs.type == "rsa"

    module "rsa_key" {
      build = "./modules/rsa-keygen"
      inputs = {
        name = "${environment.name}-${node.component}-${node.name}"
        bits = node.inputs.bits
      }
    }

    outputs = {
      privateKey       = module.rsa_key.private_key_pem
      publicKey        = module.rsa_key.public_key_pem
      privateKeyBase64 = module.rsa_key.private_key_base64
      publicKeyBase64  = module.rsa_key.public_key_base64
    }
  }

  encryptionKey {
    when = node.inputs.type == "symmetric"

    module "random_key" {
      build = "./modules/random-keygen"
      inputs = {
        name  = "${environment.name}-${node.component}-${node.name}"
        bytes = node.inputs.bytes
      }
    }

    outputs = {
      key       = module.random_key.hex
      keyBase64 = module.random_key.base64
    }
  }
}
```

**Inputs (available via `node.inputs`):**
| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Key identifier |
| `type` | string | Key type (rsa, ecdsa, symmetric) |
| `bits` | number | RSA key size in bits (when type=rsa) |
| `curve` | string | ECDSA curve name (when type=ecdsa) |
| `bytes` | number | Key length in bytes (when type=symmetric) |

**Required Outputs (for RSA and ECDSA):**
| Field | Type | Description |
|-------|------|-------------|
| `privateKey` | string | Private key in PEM format |
| `publicKey` | string | Public key in PEM format |
| `privateKeyBase64` | string | Private key PEM, base64-encoded |
| `publicKeyBase64` | string | Public key PEM, base64-encoded |

**Required Outputs (for symmetric):**
| Field | Type | Description |
|-------|------|-------------|
| `key` | string | Key as hex-encoded string |
| `keyBase64` | string | Key as base64-encoded string |

**Notes:**

- Keys should be generated deterministically or stored in state to ensure consistency across applies
- For production datacenters, consider integrating with secret managers (AWS Secrets Manager, HashiCorp Vault, etc.)
- The datacenter may use conditional hooks (`when`) to handle different key types with specialized modules
- Symmetric keys are cryptographically secure random values suitable for encryption keys, secrets, or salts

##### SMTP Hook

The SMTP hook provisions email sending capabilities when components declare SMTP requirements. This can be fulfilled by various email service providers (AWS SES, SendGrid, Mailgun, Postmark) or self-hosted SMTP servers.

```hcl
environment {
  smtp {
    module "ses" {
      plugin = "pulumi"
      build  = "./modules/aws-ses"
      inputs = {
        name   = "${environment.name}-${node.component}-${node.name}"
        domain = variable.email_domain
      }
    }

    outputs = {
      host     = module.ses.smtp_host
      port     = module.ses.smtp_port
      username = module.ses.smtp_username
      password = module.ses.smtp_password
    }
  }
}
```

**Inputs (available via `node.inputs`):**
| Field | Type | Description |
|-------|------|-------------|
| `name` | string | SMTP connection identifier |
| `description` | string | Optional description from the component |

**Required Outputs:**
| Field | Type | Description |
|-------|------|-------------|
| `host` | string | SMTP server hostname |
| `port` | number | SMTP server port (typically 587 or 465) |
| `username` | string | SMTP authentication username |
| `password` | string | SMTP authentication password |

**Notes:**

- For production datacenters, consider using managed email services (AWS SES, SendGrid) for deliverability and compliance
- The datacenter may require domain verification before allowing email sending
- Credentials should be stored securely and rotated according to organizational policies
- The "from" address is controlled by the application when sending emails, not by the SMTP connection

##### Database User Hook

Generated automatically when multiple applications access the same database.

```hcl
environment {
  databaseUser {
    when = node.inputs.databaseType == "postgres"

    module "pg_user" {
      build = "./modules/db-user"
      inputs = {
        root_user = node.inputs.username
        root_pass = node.inputs.password
        database  = node.inputs.database
        username  = node.inputs.requestedUsername
      }
    }

    outputs = {
      username = module.pg_user.username
      password = module.pg_user.password
      url      = module.pg_user.url
    }
  }
}
```

**Inputs (available via `node.inputs`):**
| Field | Type | Description |
|-------|------|-------------|
| `database` | string | Target database name |
| `username` | string | Requested username |

**Required Outputs:**
| Field | Type | Description |
|-------|------|-------------|
| `username` | string | Created username |
| `password` | string | Created password |
| `url` | string | Connection URL with credentials |

##### Deployment Hook

```hcl
environment {
  deployment {
    module "deployment" {
      build = "./modules/k8s-deployment"
      inputs = merge(node.inputs, {
        namespace  = module.namespace.id
        kubeconfig = module.k8s.kubeconfig
      })
    }

    outputs = {
      id = module.deployment.deployment_id
    }
  }
}
```

**Inputs (available via `node.inputs`):**
| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Deployment name |
| `image` | string | Container image |
| `command` | string[] | Container command |
| `entrypoint` | string[] | Container entrypoint |
| `environment` | map | Environment variables |
| `cpu` | string | CPU allocation |
| `memory` | string | Memory allocation |
| `replicas` | number | Replica count |
| `volumes` | Volume[] | Volume mounts |
| `liveness_probe` | Probe | Liveness configuration |
| `readiness_probe` | Probe | Readiness configuration |
| `labels` | map | Resource labels |

**Required Outputs:**
| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique deployment identifier |

##### Function Hook

```hcl
environment {
  function {
    module "serverless_function" {
      build = "./modules/serverless-function"
      inputs = merge(node.inputs, {
        region = variable.region
      })
    }

    outputs = {
      id       = module.serverless_function.function_id
      endpoint = module.serverless_function.endpoint
    }
  }
}
```

**Inputs (available via `node.inputs`):**
| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Function name |
| `image` | string | Container image |
| `runtime` | string | Runtime/language specification |
| `framework` | string | Optional framework hint |
| `environment` | map | Environment variables |
| `cpu` | string | CPU allocation per invocation |
| `memory` | string | Memory allocation per invocation |
| `timeout` | number | Maximum execution time (seconds) |

**Required Outputs:**
| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique function identifier |
| `endpoint` | string | Internal endpoint for the function |

**Notes:**
- Datacenters may deploy functions to different platforms (Vercel, Lambda, Cloud Run, etc.)
- Most providers only need `runtime` (e.g., nodejs:20, python:3.11); `framework` is optional for specialized providers
- Infrastructure details like regions are configured in the environment/datacenter, not passed from the component
- Functions are stateless and auto-scaling by design

##### Service Hook

```hcl
environment {
  service {
    module "service" {
      build = "./modules/k8s-service"
      inputs = merge(node.inputs, {
        name       = "${node.component}--${node.name}"
        namespace  = module.namespace.id
        kubeconfig = module.k8s.kubeconfig
      })
    }

    outputs = {
      protocol = node.inputs.protocol != null ? node.inputs.protocol : "http"
      host     = module.service.host
      port     = module.service.port
      url      = "${node.inputs.protocol}://${module.service.host}:${module.service.port}"
    }
  }
}
```

**Inputs (available via `node.inputs`):**
| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Service name |
| `target` | string | Target deployment or function ID |
| `target_type` | string | Type of target (deployment or function) |
| `port` | number | Service port |
| `protocol` | string | Protocol (http, tcp, grpc) |

**Required Outputs:**
| Field | Type | Description |
|-------|------|-------------|
| `host` | string | Service hostname |
| `port` | number | Service port |
| `url` | string | Full service URL |

##### Route Hook (Ingress)

Routes are processed using the Gateway API pattern. The datacenter receives the full route specification and is responsible for creating the appropriate gateway resources (HTTPRoute, GRPCRoute, Gateway, etc.). In HCL, this is typically handled by the `ingress` hook.

```hcl
environment {
  ingress {
    module "ingress_rule" {
      build = "./modules/ingress-rule"
      inputs = merge(node.inputs, {
        name             = "${node.component}--${node.name}"
        namespace        = module.namespace.id
        kubeconfig       = module.k8s.kubeconfig
        dns_zone         = variable.dns_zone
        ingress_class    = module.nginx_controller.ingress_class_name
      })
    }

    module "dns_record" {
      build = "./modules/dns-record"
      environment = {
        DIGITALOCEAN_TOKEN = variable.do_token
      }
      inputs = {
        domain    = variable.dns_zone
        type      = "A"
        value     = module.ingress_rule.load_balancer_ip
        subdomain = node.inputs.subdomain
      }
    }

    outputs = {
      protocol  = module.ingress_rule.protocol
      host      = module.ingress_rule.host
      port      = module.ingress_rule.port
      url       = module.ingress_rule.url
      path      = module.ingress_rule.path
      subdomain = node.inputs.subdomain
      dns_zone  = node.inputs.dns_zone
    }
  }
}
```

**Inputs (from component via `node.inputs`):**
| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Route name |
| `type` | string | Route type: `http` or `grpc` |
| `internal` | boolean | Whether route should be VPC-only |
| `rules` | RouteRule[] | Array of routing rules (see below) |

**Inputs (from environment/datacenter):**
| Field | Type | Description |
|-------|------|-------------|
| `hostnames` | string[] | Hostnames assigned by environment config |
| `gateway_name` | string | Gateway to attach routes to |
| `domain` | string | Base domain for the environment |
| `tls_secret` | string | TLS certificate secret reference |

**RouteRule Structure:**
| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Optional rule name |
| `matches` | RouteMatch[] | Array of match conditions (OR logic) |
| `backendRefs` | BackendRef[] | Target backends with weights |
| `filters` | RouteFilter[] | Optional request/response filters |
| `timeouts` | Timeouts | Optional timeout configuration |

**RouteMatch Structure (HTTP):**
| Field | Type | Description |
|-------|------|-------------|
| `path` | PathMatch | Path matching (type: PathPrefix, Exact, RegularExpression) |
| `headers` | HeaderMatch[] | Optional header matching |
| `queryParams` | QueryParamMatch[] | Optional query parameter matching |
| `method` | string | Optional HTTP method |

**RouteMatch Structure (gRPC):**
| Field | Type | Description |
|-------|------|-------------|
| `method` | GRPCMethodMatch | gRPC service/method matching |
| `headers` | HeaderMatch[] | Optional header matching |

**BackendRef Structure:**
| Field | Type | Description |
|-------|------|-------------|
| `service` | string | Target service name |
| `function` | string | Target function name (alternative to service) |
| `port` | number | Target port |
| `weight` | number | Traffic weight (default: 1) |

**RouteFilter Types:**
| Type | Description |
|------|-------------|
| `RequestHeaderModifier` | Modify request headers (add, set, remove) |
| `ResponseHeaderModifier` | Modify response headers (add, set, remove) |
| `RequestRedirect` | Redirect to different URL (scheme, hostname, port, statusCode) |
| `URLRewrite` | Rewrite URL path or hostname |
| `RequestMirror` | Mirror traffic to another backend |

**Required Outputs:**
| Field | Type | Description |
|-------|------|-------------|
| `url` | string | Primary public URL |
| `hosts` | string[] | Array of assigned hostnames |

##### Cronjob Hook

```hcl
environment {
  cronjob {
    module "cronjob" {
      build = "./modules/k8s-cronjob"
      inputs = merge(node.inputs, {
        namespace  = module.namespace.id
        kubeconfig = module.k8s.kubeconfig
      })
    }

    outputs = {
      id = module.cronjob.cronjob_id
    }
  }
}
```

**Inputs (available via `node.inputs`):**
| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Cronjob name |
| `schedule` | string | Cron expression |
| `image` | string | Container image |
| `command` | string[] | Container command |
| `environment` | map | Environment variables |

**Required Outputs:**
| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique cronjob identifier |

##### Secret Hook

```hcl
environment {
  secret {
    module "secret" {
      build  = "./modules/secret"
      plugin = "opentofu"
      inputs = {
        filename = "${variable.secrets_dir}--${environment.name}--${node.component}--${node.name}.json"
        content  = node.inputs.data
      }
    }

    outputs = {
      data = node.inputs.data
    }
  }
}
```

##### Docker Build Hook

Used when deploying components from local source (not pre-built OCI artifacts). This hook is responsible for building container images AND pushing them to a registry accessible by the target environment.

**Why This Hook Matters:**

When deploying from local source to a remote cluster (e.g., Kubernetes on AWS), the cluster needs to pull images from a registry it can access. The `dockerBuild` hook allows datacenter authors to:

1. Build container images from local source
2. Tag images with the appropriate registry prefix
3. Push images to a registry the target environment can access
4. Return the final image reference for use by deployment hooks

**Basic Example (Local Development):**

For local development environments (e.g., Docker Compose, local K8s), images may not need to be pushed:

```hcl
environment {
  dockerBuild {
    module "build" {
      build = "./modules/docker-build-local"

      volume {
        host_path  = "/var/run/docker.sock"
        mount_path = "/var/run/docker.sock"
      }

      # merge passes through standard inputs (context, dockerfile, args, target)
      inputs = merge(node.inputs, {
        image = "${node.component}--${node.name}:${node.inputs.hash}"
      })
    }

    outputs = {
      image = module.build.image
    }
  }
}
```

**Remote Registry Example (Production):**

For remote environments, images must be pushed to a registry. Use `environment.name` to organize images per-environment:

```hcl
environment {
  dockerBuild {
    module "build_and_push" {
      build = "./modules/docker-build-push"

      volume {
        host_path  = "/var/run/docker.sock"
        mount_path = "/var/run/docker.sock"
      }

      # merge passes through standard inputs (context, dockerfile, args, target)
      # while allowing you to override or add custom fields
      inputs = merge(node.inputs, {
        # Use environment.name to organize images per-environment
        # Examples of resulting image tags:
        #   - staging:    registry.io/staging/web-app--api:abc123
        #   - production: registry.io/production/web-app--api:abc123
        #   - preview-42: registry.io/preview-42/web-app--api:abc123
        image = "${variable.registry_url}/${environment.name}/${node.component}--${node.name}:${node.inputs.hash}"
        
        # Registry authentication
        registry_url      = variable.registry_url
        registry_username = variable.registry_username
        registry_password = variable.registry_password
        
        # Push after build
        push = true
      })
    }

    outputs = {
      image = module.build_and_push.image
    }
  }
}
```

**Using `environment.name` for Image Organization:**

The `environment.name` variable is available in the `dockerBuild` hook and can be used to:

- **Namespace images per environment**: `${registry}/${environment.name}/${component}:${tag}`
- **Use environment-specific registries**: Different ECR repos, GCR projects, etc.
- **Isolate preview environments**: Each PR gets its own image namespace

```hcl
# Option 1: Environment as path segment
image = "${variable.registry_url}/${environment.name}/${node.component}--${node.name}:${node.inputs.hash}"
# Result: registry.io/staging/web-app--api:abc123

# Option 2: Environment as tag prefix
image = "${variable.registry_url}/${node.component}--${node.name}:${environment.name}-${node.inputs.hash}"
# Result: registry.io/web-app--api:staging-abc123

# Option 3: Environment-specific repository
image = "${variable.registry_url}/${environment.name}-${node.component}--${node.name}:${node.inputs.hash}"
# Result: registry.io/staging-web-app--api:abc123
```

**AWS ECR Example:**

```hcl
variable "aws_account_id" {
  type = string
}

variable "aws_region" {
  type    = string
  default = "us-east-1"
}

environment {
  dockerBuild {
    module "ecr_build" {
      build = "./modules/docker-build-ecr"

      inputs = merge(node.inputs, {
        # ECR repository URL pattern using environment.name for isolation
        # Examples:
        #   - staging:    123456789.dkr.ecr.us-east-1.amazonaws.com/staging/web-app--api:abc123
        #   - production: 123456789.dkr.ecr.us-east-1.amazonaws.com/production/web-app--api:abc123
        #   - preview-42: 123456789.dkr.ecr.us-east-1.amazonaws.com/preview-42/web-app--api:abc123
        image = "${variable.aws_account_id}.dkr.ecr.${variable.aws_region}.amazonaws.com/${environment.name}/${node.component}--${node.name}:${node.inputs.hash}"
        
        # ECR-specific
        aws_region     = variable.aws_region
        aws_account_id = variable.aws_account_id
        
        # Create repository if it doesn't exist (useful for preview environments)
        create_repository = true
      })
    }

    outputs = {
      image = module.ecr_build.image
    }
  }
}
```

**Inputs (available via `node.inputs`):**

| Field | Type | Description |
|-------|------|-------------|
| `context` | string | Build context path (local filesystem path) |
| `dockerfile` | string | Dockerfile path within context |
| `target` | string | Build target (for multi-stage builds) |
| `args` | map | Build arguments |
| `hash` | string | Content hash of the build context (for cache-busting tags) |

**Required Outputs:**

| Field | Type | Description |
|-------|------|-------------|
| `image` | string | Final image reference (must be pullable by the target environment) |

**Notes:**

- The `image` output must be a reference that the target environment can pull
- For remote clusters, this means the image must be pushed to an accessible registry
- The datacenter author controls the registry URL, authentication, and tagging strategy
- The `hash` input can be used to create unique, content-addressable tags
- If deploying from a pre-built component artifact (OCI image), this hook is not invoked—the artifact's embedded image references are used directly

#### Variables

Datacenter-level variables for configuration. Variables are declared using HCL `variable` blocks.

```hcl
variable "cluster_name" {
  description = "Kubernetes cluster name"
  type        = string
}

variable "region" {
  description = "Cloud region"
  type        = string
  default     = "us-east-1"
}

variable "domain" {
  description = "Base domain for environments"
  type        = string
  default     = "dev.example.com"
}

variable "do_token" {
  description = "DigitalOcean API token"
  type        = string
  sensitive   = true
}
```

Variables can be referenced anywhere in the datacenter using `variable.<name>`:

```hcl
module "k8s" {
  build = "./k8s-cluster"
  inputs = {
    name   = variable.cluster_name
    region = variable.region
  }
}
```

#### Expression Context

Datacenter expressions use HCL interpolation syntax and have access to:

- `variable.<name>` - Datacenter variables
- `datacenter.name` - Datacenter name
- `environment.name` - Current environment name
- `environment.nodes` - Array of all nodes/resources in the environment
- `environment.nodes.*.type` - Array of node types (for filtering)
- `node.name` - Current resource name (in hooks)
- `node.component` - Component name the resource belongs to (in hooks)
- `node.inputs.<field>` - Resource input values (in hooks)
- `module.<name>.<output>` - Module output values

**HCL Functions:**

```hcl
# String interpolation
name = "${datacenter.name}-cluster"

# Conditional expressions
when = node.inputs.databaseType == "postgres"

# Contains function for array checking
when = contains(environment.nodes.*.type, "database")

# Merge maps
inputs = merge(node.inputs, {
  namespace = module.namespace.id
})

# Length function
when = length(environment.resources.routes) > 0
```

#### Complete Example

```hcl
# Datacenter: AWS Production on EKS
# File: datacenter.dc

variable "cluster_name" {
  description = "Kubernetes cluster name"
  type        = string
}

variable "region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "domain" {
  description = "Base domain for environments"
  type        = string
  default     = "prod.example.com"
}

variable "aws_account_id" {
  description = "AWS account ID for ECR"
  type        = string
}

variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

# Datacenter-level module: shared EKS cluster
module "k8s" {
  build = "./modules/eks-cluster"
  inputs = {
    name   = "${datacenter.name}-cluster"
    region = variable.region
  }
}

environment {
  # Environment-level modules
  module "namespace" {
    build = "./modules/k8s-namespace"
    inputs = {
      name       = environment.name
      kubeconfig = module.k8s.kubeconfig
    }
  }

  module "dns_zone" {
    build = "./modules/route53-zone"
    inputs = {
      name = "${environment.name}.${variable.domain}"
    }
  }

  module "nginx_controller" {
    when  = contains(environment.nodes.*.type, "ingress")
    build = "./modules/nginx-controller"
    inputs = {
      name       = "${datacenter.name}-nginx-controller"
      kubeconfig = module.k8s.kubeconfig
    }
  }

  module "postgres_cluster" {
    when  = contains(environment.nodes.*.type, "database") && contains(environment.nodes.*.inputs.databaseType, "postgres")
    build = "./modules/rds-postgres"
    inputs = {
      name                 = "${datacenter.name}-database"
      database_type        = "pg"
      database_version     = 15
      region               = variable.region
      "digitalocean:token" = variable.do_token
    }
  }

  # Resource hooks
  database {
    when = node.inputs.databaseType == "postgres"

    module "db_cluster" {
      build = "./modules/rds-database"
      inputs = {
        name    = node.name
        type    = node.inputs.databaseType
        version = node.inputs.databaseVersion
      }
    }

    outputs = {
      host     = module.db_cluster.endpoint
      port     = module.db_cluster.port
      database = module.db_cluster.database_name
      url      = module.db_cluster.connection_url
    }
  }

  bucket {
    module "s3_bucket" {
      build = "./modules/s3-bucket"
      inputs = {
        name       = node.name
        versioning = node.inputs.versioning
        public     = node.inputs.public
        region     = variable.region
      }
    }

    outputs = {
      endpoint        = module.s3_bucket.endpoint
      bucket          = module.s3_bucket.bucket_name
      region          = module.s3_bucket.region
      accessKeyId     = module.s3_bucket.access_key_id
      secretAccessKey = module.s3_bucket.secret_access_key
    }
  }

  deployment {
    module "deployment" {
      build = "./modules/k8s-deployment"
      inputs = merge(node.inputs, {
        namespace  = module.namespace.id
        kubeconfig = module.k8s.kubeconfig
      })
    }

    outputs = {
      id = module.deployment.deployment_id
    }
  }

  function {
    module "lambda" {
      build = "./modules/lambda-function"
      inputs = merge(node.inputs, {
        region = variable.region
      })
    }

    outputs = {
      id       = module.lambda.function_id
      endpoint = module.lambda.function_url
    }
  }

  service {
    module "service" {
      build = "./modules/k8s-service"
      inputs = merge(node.inputs, {
        name       = "${node.component}--${node.name}"
        namespace  = module.namespace.id
        kubeconfig = module.k8s.kubeconfig
      })
    }

    outputs = {
      host = module.service.cluster_ip
      port = module.service.port
      url  = "http://${module.service.cluster_ip}:${module.service.port}"
    }
  }

  ingress {
    module "ingress_rule" {
      build = "./modules/ingress-rule"
      inputs = merge(node.inputs, {
        name             = "${node.component}--${node.name}"
        namespace        = module.namespace.id
        kubeconfig       = module.k8s.kubeconfig
        dns_zone         = variable.domain
        ingress_class    = module.nginx_controller.ingress_class_name
      })
    }

    module "dns_record" {
      build = "./modules/dns-record"
      inputs = {
        domain    = variable.domain
        type      = "A"
        value     = module.ingress_rule.load_balancer_ip
        subdomain = node.inputs.subdomain
      }
    }

    outputs = {
      protocol  = module.ingress_rule.protocol
      host      = module.ingress_rule.host
      port      = module.ingress_rule.port
      url       = module.ingress_rule.url
      subdomain = node.inputs.subdomain
      dns_zone  = variable.domain
    }
  }

  cronjob {
    module "cronjob" {
      build = "./modules/k8s-cronjob"
      inputs = merge(node.inputs, {
        namespace  = module.namespace.id
        kubeconfig = module.k8s.kubeconfig
      })
    }

    outputs = {
      id = module.cronjob.cronjob_id
    }
  }

  secret {
    module "secret" {
      build  = "./modules/secret"
      plugin = "opentofu"
      inputs = {
        filename = "${variable.secrets_dir}--${environment.name}--${node.component}--${node.name}.json"
        content  = node.inputs.data
      }
    }

    outputs = {
      data = node.inputs.data
    }
  }

  dockerBuild {
    module "build_and_push" {
      build = "./modules/docker-build-ecr"
      
      volume {
        host_path  = "/var/run/docker.sock"
        mount_path = "/var/run/docker.sock"
      }
      
      inputs = merge(node.inputs, {
        # Tag for ECR so the EKS cluster can pull
        image = "${variable.aws_account_id}.dkr.ecr.${variable.aws_region}.amazonaws.com/${environment.name}/${node.component}--${node.name}:${node.inputs.hash}"
        
        aws_region        = variable.aws_region
        aws_account_id    = variable.aws_account_id
        create_repository = true
      })
    }

    outputs = {
      image = module.build_and_push.image
    }
  }
}
```

---

### 3. Environment File (`environment.yml`)

The environment file defines the desired state of a single environment, including which components to deploy and their configurations.

#### Purpose

- Specify which components should be deployed
- Configure component variables per environment
- Override scaling rules and resource allocations
- Configure route hostnames and TLS settings
- Set environment-specific settings

#### File Location

- Can be named anything, typically `environment.yml` or `<env-name>.yml`
- Specified explicitly via CLI

#### Top-Level Structure

```yaml
# Environment metadata
name: string # Optional. Environment name (can be set via CLI)
datacenter: string # Optional. Target datacenter (can be set via CLI)

# Reusable values
locals: map<string, any>

# Component configurations
components: map<string, ComponentConfig>
```

#### Component Configuration

Each component in the environment can be configured with variables, scaling rules, and more.

```yaml
components:
  my-app:
    # Component source (required)
    component: registry.example.com/org/my-app:v1.0.0
    # OR for local development
    component: ./path/to/component

    # Variable values
    variables:
      log_level: "debug"
      stripe_key: ${{ locals.stripe_key }}

    # Scaling configuration (for deployments)
    scaling:
      api:                      # Deployment name
        replicas: 3
        cpu: "1"
        memory: "1Gi"
      worker:
        replicas: 2

    # Function configuration (for serverless functions)
    functions:
      web:                      # Function name
        regions: ["us-east-1", "us-west-2"]  # Multi-region deployment
        memory: "2048Mi"        # Override memory
        timeout: 120            # Override timeout

    # Environment variable overrides
    environment:
      api:                      # Deployment name
        FEATURE_FLAG: "true"

    # Route configuration (hostname assignment, TLS, etc.)
    routes:
      main:                     # Route name from component
        hostnames:              # Assign hostnames to this route
          - subdomain: app      # Results in app.<env-domain>
          - host: app.example.com  # Explicit hostname
        tls:
          enabled: true         # Enable TLS (default based on datacenter)
          secretName: app-tls   # Optional. Custom TLS secret
```

#### Locals

Define reusable values within the environment file.

```yaml
locals:
  domain: "staging.example.com"
  stripe_key: "sk_test_..."
  database_size: "db.t3.medium"

components:
  my-app:
    component: registry.example.com/org/my-app:v1.0.0
    variables:
      api_domain: ${{ locals.domain }}
      stripe_key: ${{ locals.stripe_key }}
```

#### Complete Example

```yaml
name: staging
datacenter: aws-production

locals:
  base_domain: "staging.example.com"
  stripe_key: "sk_test_abc123"
  log_level: "debug"

components:
  auth-service:
    component: registry.example.com/org/auth:v2.1.0
    variables:
      jwt_secret: ${{ locals.jwt_secret }}
      log_level: ${{ locals.log_level }}
    scaling:
      api:
        replicas: 2
        cpu: "0.5"
        memory: "512Mi"
    routes:
      public:
        hostnames:
          - subdomain: auth

  web-app:
    component: registry.example.com/org/web-app:v1.5.0
    variables:
      stripe_key: ${{ locals.stripe_key }}
      log_level: ${{ locals.log_level }}
    scaling:
      api:
        replicas: 2
    functions:
      frontend:
        regions: ["us-east-1", "us-west-2"]
        memory: "2048Mi"
    environment:
      frontend:
        NEXT_PUBLIC_API_URL: https://api.${{ locals.base_domain }}
    routes:
      main:
        hostnames:
          - subdomain: www
          - subdomain: api
        tls:
          enabled: true

  worker:
    component: registry.example.com/org/worker:v1.2.0
    variables:
      log_level: "info"
    scaling:
      processor:
        replicas: 5
        cpu: "1"
        memory: "2Gi"
```

---

## User Experience

### Developer Workflow

1. **Write**: Create an `architect.yml` file describing the application
2. **Build**: Build the component into a portable OCI artifact
3. **Test**: Create ephemeral environments for testing
4. **Deploy**: Deploy to shared environments (staging, production)

#### Local Development

Developers can spin up their entire application stack locally:

- All dependencies are automatically deployed
- Services are automatically discovered and connected
- Hot reload support for rapid iteration

#### Ephemeral Environments

Developers can create on-demand environments that:

- Automatically deploy all component dependencies
- Clean up when the terminal session ends
- Support custom naming for collaboration

#### CI/CD Integration

Components can be built and deployed in CI pipelines:

- Build component artifacts
- Deploy to preview environments on pull requests
- Promote to staging/production on merge

### Platform Engineer Workflow

1. **Write**: Create a `datacenter.yml` file defining infrastructure rules
2. **Build**: Build the datacenter and its modules
3. **Apply**: Apply changes to create/update the datacenter
4. **Monitor**: Observe environments and resources

#### Infrastructure Control

Platform engineers maintain control over:

- Which cloud resources are provisioned
- How applications are deployed (Kubernetes, ECS, etc.)
- Security policies and network rules
- Credential management and rotation

#### Self-Service Enablement

The datacenter framework allows platform engineers to:

- Offer standardized infrastructure to developers
- Enforce organizational policies automatically
- Scale infrastructure without application changes

---

## CLI Commands

### Overview

arcctl provides CLI commands for building, tagging, and pushing components and datacenters as OCI-compliant artifacts. Both components and datacenters are bundled into OCI artifacts that can be stored in any OCI-compatible registry (Docker Hub, GitHub Container Registry, AWS ECR, etc.).

### Command Structure

arcctl uses an intuitive action-first command structure:

```
arcctl <action> <resource> [arguments] [flags]
```

This pattern makes commands more natural to type (e.g., `arcctl create environment` instead of `arcctl create environment`).

### Command Aliases

For convenience, arcctl supports shorthand aliases for commonly used commands:

| Resource | Alias |
|----------|-------|
| `component` | `comp` |
| `datacenter` | `dc` |
| `environment` | `env` |
| `list` | `ls` |

**Examples:**

```bash
# These are equivalent:
arcctl build datacenter . -t ghcr.io/myorg/dc:v1.0.0
arcctl build dc . -t ghcr.io/myorg/dc:v1.0.0

# These are equivalent:
arcctl list environment
arcctl ls env

# These are equivalent:
arcctl create environment staging --if-not-exists
arcctl create env staging --if-not-exists
```

### Artifact Structure

When building a component or datacenter, arcctl produces multiple OCI artifacts:

1. **Root Artifact**: The main component/datacenter manifest and configuration
2. **Child Artifacts**: Supporting artifacts referenced by the root artifact
   - For components: Container images for deployments, functions, cronjobs, and migrations
   - For datacenters: IaC module images (Pulumi/OpenTofu templates bundled as executable containers)

During the build process, source references (like `build:` blocks) are compiled into image references pointing to the built child artifacts.

### Build Commands

#### `arcctl build component`

Build a component and its container images into OCI artifacts.

**Synopsis:**

```bash
arcctl build component <path> -t <repo:tag> [options]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `<path>` | Path to the component directory (containing `architect.yml`) |

**Options:**

| Option | Description |
|--------|-------------|
| `-t, --tag <repo:tag>` | Tag for the root component artifact (required) |
| `--artifact-tag <name=repo:tag>` | Override tag for a specific child artifact (can be specified multiple times) |
| `-f, --file <path>` | Path to architect.yml if not in the default location |
| `--platform <platform>` | Target platform for container builds (e.g., `linux/amd64`, `linux/arm64`) |
| `--no-cache` | Disable build cache for container builds |
| `--yes, -y` | Non-interactive mode: skip confirmation prompts (for CI/CD) |
| `--dry-run` | Show what would be built without actually building |

**Child Artifact Naming Convention:**

When building a component with tag `myregistry.io/myorg/myapp:v1.0.0`, child artifacts are automatically named using this convention:

```
<root-repo>-<child-type>-<child-name>:<root-tag>
```

For example:
- Root: `myregistry.io/myorg/myapp:v1.0.0`
- API deployment: `myregistry.io/myorg/myapp-deployment-api:v1.0.0`
- Worker deployment: `myregistry.io/myorg/myapp-deployment-worker:v1.0.0`
- Web function: `myregistry.io/myorg/myapp-function-web:v1.0.0`
- DB migrations: `myregistry.io/myorg/myapp-migration-main:v1.0.0`
- Cleanup cronjob: `myregistry.io/myorg/myapp-cronjob-cleanup:v1.0.0`

**Interactive Mode (default):**

In interactive mode, arcctl displays all artifacts that will be created and prompts for confirmation:

```
$ arcctl build component . -t myregistry.io/myorg/myapp:v1.0.0

Building component: my-app

The following artifacts will be created:

  Root Artifact:
    myregistry.io/myorg/myapp:v1.0.0

  Child Artifacts:
    deployments/api      → myregistry.io/myorg/myapp-deployment-api:v1.0.0
    deployments/worker   → myregistry.io/myorg/myapp-deployment-worker:v1.0.0
    functions/web        → myregistry.io/myorg/myapp-function-web:v1.0.0
    databases/main       → myregistry.io/myorg/myapp-migration-main:v1.0.0
      (migrations)
    cronjobs/cleanup     → myregistry.io/myorg/myapp-cronjob-cleanup:v1.0.0

Proceed with build? [Y/n]: 
```

**Non-Interactive Mode (CI/CD):**

With `--yes` or `-y`, arcctl skips confirmation and logs the artifact names:

```
$ arcctl build component . -t myregistry.io/myorg/myapp:v1.0.0 --yes

Building component: my-app
[info] Root artifact: myregistry.io/myorg/myapp:v1.0.0
[info] Child artifact: myregistry.io/myorg/myapp-deployment-api:v1.0.0
[info] Child artifact: myregistry.io/myorg/myapp-deployment-worker:v1.0.0
[info] Child artifact: myregistry.io/myorg/myapp-function-web:v1.0.0
[info] Child artifact: myregistry.io/myorg/myapp-migration-main:v1.0.0
[info] Child artifact: myregistry.io/myorg/myapp-cronjob-cleanup:v1.0.0
[build] Building deployment/api...
[build] Building deployment/worker...
...
[success] Component built successfully
```

**Overriding Child Artifact Tags:**

Use `--artifact-tag` to override specific child artifacts:

```bash
# Override the API deployment image tag
arcctl build component . \
  -t myregistry.io/myorg/myapp:v1.0.0 \
  --artifact-tag deployment/api=myregistry.io/myorg/custom-api:latest

# Override multiple artifacts
arcctl build component . \
  -t myregistry.io/myorg/myapp:v1.0.0 \
  --artifact-tag deployment/api=myregistry.io/myorg/api:v2.0.0 \
  --artifact-tag migration/main=myregistry.io/myorg/migrations:v1.5.0
```

**Example:**

```bash
# Basic build
arcctl build component . -t ghcr.io/myorg/myapp:v1.0.0

# Build with platform specification
arcctl build component . -t ghcr.io/myorg/myapp:v1.0.0 --platform linux/amd64

# CI/CD build (non-interactive)
arcctl build component . -t ghcr.io/myorg/myapp:$CI_COMMIT_SHA --yes

# Build from specific file
arcctl build component ./services/api -f architect.production.yml -t ghcr.io/myorg/api:v1.0.0
```

### Tag Commands

#### `arcctl tag component`

Create a new tag for an existing component artifact and all its child artifacts.

**Synopsis:**

```bash
arcctl tag component <source> <target> [options]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `<source>` | Source component artifact reference |
| `<target>` | Target tag for the component |

**Options:**

| Option | Description |
|--------|-------------|
| `--artifact-tag <name=repo:tag>` | Override tag for a specific child artifact |
| `--yes, -y` | Non-interactive mode: skip confirmation prompts |

**Behavior:**

When tagging a component, arcctl re-tags both the root artifact and all child artifacts, maintaining the naming convention:

```bash
$ arcctl tag component myregistry.io/myorg/myapp:v1.0.0 myregistry.io/myorg/myapp:latest

Tagging component artifacts:

  Root Artifact:
    myregistry.io/myorg/myapp:v1.0.0 → myregistry.io/myorg/myapp:latest

  Child Artifacts:
    myregistry.io/myorg/myapp-deployment-api:v1.0.0 → myregistry.io/myorg/myapp-deployment-api:latest
    myregistry.io/myorg/myapp-deployment-worker:v1.0.0 → myregistry.io/myorg/myapp-deployment-worker:latest
    myregistry.io/myorg/myapp-function-web:v1.0.0 → myregistry.io/myorg/myapp-function-web:latest

Proceed? [Y/n]:
```

**Example:**

```bash
# Tag a version as latest
arcctl tag component ghcr.io/myorg/myapp:v1.0.0 ghcr.io/myorg/myapp:latest

# Tag for promotion to production
arcctl tag component ghcr.io/myorg/myapp:v1.0.0 ghcr.io/myorg/myapp:production

# CI/CD tagging (non-interactive)
arcctl tag component ghcr.io/myorg/myapp:$CI_COMMIT_SHA ghcr.io/myorg/myapp:$CI_BRANCH_NAME --yes
```

### Push Commands

#### `arcctl push component`

Push a component artifact and all its child artifacts to an OCI registry.

**Synopsis:**

```bash
arcctl push component <repo:tag> [options]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `<repo:tag>` | Component artifact reference to push |

**Options:**

| Option | Description |
|--------|-------------|
| `--yes, -y` | Non-interactive mode: skip confirmation prompts |

**Behavior:**

Pushes the root component artifact and all referenced child artifacts to the registry:

```bash
$ arcctl push component myregistry.io/myorg/myapp:v1.0.0

Pushing component artifacts:

  Root Artifact:
    myregistry.io/myorg/myapp:v1.0.0

  Child Artifacts:
    myregistry.io/myorg/myapp-deployment-api:v1.0.0
    myregistry.io/myorg/myapp-deployment-worker:v1.0.0
    myregistry.io/myorg/myapp-function-web:v1.0.0
    myregistry.io/myorg/myapp-migration-main:v1.0.0

Proceed? [Y/n]:

[push] Pushing myregistry.io/myorg/myapp-deployment-api:v1.0.0...
[push] Pushing myregistry.io/myorg/myapp-deployment-worker:v1.0.0...
[push] Pushing myregistry.io/myorg/myapp-function-web:v1.0.0...
[push] Pushing myregistry.io/myorg/myapp-migration-main:v1.0.0...
[push] Pushing myregistry.io/myorg/myapp:v1.0.0...
[success] Component pushed successfully
```

**Example:**

```bash
# Push to registry
arcctl push component ghcr.io/myorg/myapp:v1.0.0

# CI/CD push (non-interactive)
arcctl push component ghcr.io/myorg/myapp:$CI_COMMIT_SHA --yes
```

### List Commands

#### `arcctl list component`

List all components deployed to an environment.

**Synopsis:**

```bash
arcctl list component --environment <name> [backend-options]
```

**Options:**

| Option | Description |
|--------|-------------|
| `-e, --environment <name>` | Target environment name (required) |
| `--backend <type>` | State backend type (default: `local`) |
| `--backend-config <key=value>` | Backend-specific configuration |
| `--output <format>` | Output format: `table` (default), `json`, `yaml` |

**Example:**

```bash
# List components in an environment
arcctl list component --environment staging

# Using shorthand flag
arcctl list component -e staging

# With S3 backend
arcctl list component -e production \
  --backend s3 \
  --backend-config bucket=my-arcctl-state \
  --backend-config region=us-east-1

# Output as JSON
arcctl list component -e staging --output json
```

**Output:**

```
$ arcctl list component -e staging

Environment: staging
Datacenter:  aws-production

NAME           SOURCE                               VERSION   STATUS    RESOURCES
auth-service   ghcr.io/myorg/auth:v2.1.0           v2.1.0    running   4
web-app        ghcr.io/myorg/web-app:v1.5.0        v1.5.0    running   6
worker         ghcr.io/myorg/worker:v1.2.0         v1.2.0    running   2
```

### Get Commands

#### `arcctl get component`

Get detailed information about a deployed component.

**Synopsis:**

```bash
arcctl get component <name> --environment <name> [backend-options]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `<name>` | Component name (as deployed to the environment) |

**Options:**

| Option | Description |
|--------|-------------|
| `-e, --environment <name>` | Target environment name (required) |
| `--backend <type>` | State backend type (default: `local`) |
| `--backend-config <key=value>` | Backend-specific configuration |
| `--output <format>` | Output format: `table` (default), `json`, `yaml` |

**Example:**

```bash
# Get component details
arcctl get component web-app --environment staging

# Using shorthand
arcctl get component web-app -e staging

# Output as JSON for scripting
arcctl get component web-app -e staging --output json
```

**Output:**

```
$ arcctl get component web-app -e staging

Component:   web-app
Environment: staging
Datacenter:  aws-production
Source:      ghcr.io/myorg/web-app:v1.5.0
Status:      running
Deployed:    2026-01-28 14:22:00

Variables:
  log_level    = "debug"
  stripe_key   = <sensitive>

Resources:
  TYPE        NAME        STATUS       DETAILS
  database    main        provisioned  postgres:15, 10GB
  database    cache       provisioned  redis:7
  bucket      uploads     provisioned  s3, versioning enabled
  deployment  api         running      2/2 replicas
  service     api         active       http://web-app--api:8080
  route       main        active       https://staging.example.com
```

### Deploy Commands

#### `arcctl deploy component`

Deploy a component to an environment.

**Synopsis:**

```bash
arcctl deploy component <source> --environment <name> [options]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `<name>` | Name for the component deployment (used to identify the component in this environment) |

**Options:**

| Option | Description |
|--------|-------------|
| `-e, --environment <name>` | Target environment name (required) |
| `-c, --config <config-ref>` | Component configuration reference (required). Local path or OCI image |
| `--var <key=value>` | Set a component variable (can be specified multiple times) |
| `--var-file <path>` | Load variables from a `.dcvars` file |
| `--auto-approve` | Skip interactive approval of the execution plan |
| `--target <resource>` | Target a specific resource (can be specified multiple times) |
| `--backend <type>` | State backend type (default: `local`) |
| `--backend-config <key=value>` | Backend-specific configuration |

**Config Reference Formats:**

The `--config` flag accepts two formats:

1. **OCI image reference**: A built component artifact in `repo:tag` format
   ```bash
   arcctl deploy component ghcr.io/myorg/web-app:v1.5.0 -e staging
   ```
   When deploying from an OCI image, the pre-built container images embedded in the artifact are used directly.

2. **Local path**: Path to a directory containing `architect.yml` (for development)
   ```bash
   arcctl deploy component ./services/web-app -e staging
   ```
   When deploying from local source, arcctl invokes the datacenter's `dockerBuild` hook to build and push container images. The datacenter author controls where images are pushed (e.g., ECR, GCR, Docker Hub) so the target environment can pull them. See [Docker Build Hook](#docker-build-hook) for details.

**Behavior:**

1. **Resolve**: arcctl reads the datacenter and environment state
2. **Plan**: Analyzes the component config and current state to determine changes
3. **Approve**: In interactive mode, displays the execution plan and prompts for confirmation
4. **Apply**: Deploys resources via datacenter hooks, updating component state as each completes

```
$ arcctl deploy component ghcr.io/myorg/web-app:v1.5.0 -e staging

Component:   web-app
Environment: staging
Datacenter:  aws-production
Source:      ghcr.io/myorg/web-app:v1.5.0

Resolving datacenter resources...
Planning component deployment...

Execution Plan:

  database "main" (postgres:^15)
    + create: RDS instance "staging-web-app-main"
    
  database "cache" (redis)
    + create: ElastiCache cluster "staging-web-app-cache"

  bucket "uploads"
    + create: S3 bucket "staging-web-app-uploads"

  deployment "api"
    + create: EKS deployment "staging-web-app-api"
    
  service "api"
    + create: EKS service "staging-web-app-api"
    
  route "main"
    + create: Ingress rule "staging-web-app-main"

Plan: 6 to create, 0 to update, 0 to destroy

Proceed with deployment? [Y/n]:

[deploy] Creating database "main"...
[deploy] Creating database "cache"...
[deploy] Creating bucket "uploads"...
[deploy] Creating deployment "api"...
[deploy] Creating service "api"...
[deploy] Creating route "main"...
[success] Component deployed successfully

  route/main: https://staging.example.com
```

**Example:**

```bash
# Deploy from OCI image
arcctl deploy component ghcr.io/myorg/web-app:v1.5.0 -e staging

# Deploy from local source (development)
arcctl deploy component ./services/web-app -e staging

# Deploy with variables
arcctl deploy component ghcr.io/myorg/web-app:v1.5.0 -e staging \
  --var log_level=debug \
  --var-file ./vars/staging.dcvars

# Deploy with auto-approval (CI/CD)
arcctl deploy component ghcr.io/myorg/web-app:v1.5.0 -e staging \
  --auto-approve

# Deploy with S3 backend
arcctl deploy component ghcr.io/myorg/web-app:v1.5.0 -e staging \
  --backend s3 \
  --backend-config bucket=my-arcctl-state \
  --backend-config region=us-east-1

# Deploy targeting a specific resource
arcctl deploy component ghcr.io/myorg/web-app:v1.5.0 -e staging \
  --target deployment.api

# Deploy via environment variables
export ARCCTL_BACKEND=s3
export ARCCTL_BACKEND_S3_BUCKET=my-arcctl-state
export ARCCTL_BACKEND_S3_REGION=us-east-1
arcctl deploy component ghcr.io/myorg/web-app:v1.5.0 -e production --auto-approve
```

**Updating a Component:**

Running `deploy component` with the same name updates the existing deployment:

```bash
# Update to a new version
arcctl deploy component ghcr.io/myorg/web-app:v1.6.0 -e staging

# The plan shows what changes
Execution Plan:

  deployment "api"
    ~ update: image "ghcr.io/myorg/web-app-deployment-api:v1.5.0" → "ghcr.io/myorg/web-app-deployment-api:v1.6.0"

Plan: 0 to create, 1 to update, 0 to destroy
```

### Destroy Commands

#### `arcctl destroy component`

Remove a component from an environment and destroy all its resources.

**Synopsis:**

```bash
arcctl destroy component <name> --environment <name> [options]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `<name>` | Component name to destroy |

**Options:**

| Option | Description |
|--------|-------------|
| `-e, --environment <name>` | Target environment name (required) |
| `--auto-approve` | Skip interactive confirmation |
| `--backend <type>` | State backend type (default: `local`) |
| `--backend-config <key=value>` | Backend-specific configuration |

**Behavior:**

1. Reads the component state from the environment
2. Plans destruction of all resources in reverse dependency order
3. Prompts for confirmation (unless `--auto-approve`)
4. Destroys all resources and removes component from environment state

```
$ arcctl destroy component web-app -e staging

Component:   web-app
Environment: staging
Datacenter:  aws-production

Planning component destruction...

The following resources will be destroyed:

  - destroy: route/main
  - destroy: service/api
  - destroy: deployment/api
  - destroy: bucket/uploads
  - destroy: database/cache
  - destroy: database/main

Plan: 0 to create, 0 to update, 6 to destroy

This action cannot be undone. All data in databases and buckets will be lost.

Type the component name to confirm: web-app

[destroy] Destroying route "main"...
[destroy] Destroying service "api"...
[destroy] Destroying deployment "api"...
[destroy] Destroying bucket "uploads"...
[destroy] Destroying database "cache"...
[destroy] Destroying database "main"...
[destroy] Removing component from environment state...
[success] Component destroyed successfully
```

**Example:**

```bash
# Interactive destroy
arcctl destroy component web-app -e staging

# Auto-approve (CI/CD cleanup)
arcctl destroy component web-app -e preview-123 --auto-approve

# With S3 backend
arcctl destroy component web-app -e staging \
  --backend s3 \
  --backend-config bucket=my-arcctl-state \
  --backend-config region=us-east-1

# Via environment variables
export ARCCTL_BACKEND=s3
export ARCCTL_BACKEND_S3_BUCKET=my-arcctl-state
export ARCCTL_BACKEND_S3_REGION=us-east-1
arcctl destroy component web-app -e preview-123 --auto-approve
```

### Datacenter Build Commands

#### `arcctl build datacenter`

Build a datacenter and its IaC modules into OCI artifacts.

**Synopsis:**

```bash
arcctl build datacenter <path> -t <repo:tag> [options]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `<path>` | Path to the datacenter directory (containing `datacenter.dc`) |

**Options:**

| Option | Description |
|--------|-------------|
| `-t, --tag <repo:tag>` | Tag for the root datacenter artifact (required) |
| `--artifact-tag <name=repo:tag>` | Override tag for a specific module artifact (can be specified multiple times) |
| `-f, --file <path>` | Path to datacenter.dc if not in the default location |
| `--yes, -y` | Non-interactive mode: skip confirmation prompts (for CI/CD) |
| `--dry-run` | Show what would be built without actually building |

**Child Artifact Naming Convention:**

Datacenter modules are compiled into executable container images that bundle the IaC templates (Pulumi programs, OpenTofu modules) with their runtime. When building a datacenter with tag `myregistry.io/myorg/aws-prod:v1.0.0`, module artifacts are named:

```
<root-repo>-module-<module-name>:<root-tag>
```

For example:
- Root: `myregistry.io/myorg/aws-prod:v1.0.0`
- K8s cluster module: `myregistry.io/myorg/aws-prod-module-k8s:v1.0.0`
- Namespace module: `myregistry.io/myorg/aws-prod-module-namespace:v1.0.0`
- Postgres module: `myregistry.io/myorg/aws-prod-module-postgres_cluster:v1.0.0`
- Deployment module: `myregistry.io/myorg/aws-prod-module-deployment:v1.0.0`

**Module Image Structure:**

Each module is built into a container image that:
1. Contains the IaC templates (Pulumi program or OpenTofu module)
2. Includes the appropriate runtime (Pulumi CLI, OpenTofu CLI)
3. Exposes a standard interface for arcctl to invoke with inputs and receive outputs
4. Is self-contained and executable without external dependencies (except cloud credentials)

**Interactive Mode (default):**

```
$ arcctl build datacenter . -t myregistry.io/myorg/aws-prod:v1.0.0

Building datacenter: aws-production

The following artifacts will be created:

  Root Artifact:
    myregistry.io/myorg/aws-prod:v1.0.0

  Module Artifacts:
    module "k8s"              → myregistry.io/myorg/aws-prod-module-k8s:v1.0.0
    module "namespace"        → myregistry.io/myorg/aws-prod-module-namespace:v1.0.0
    module "postgres_cluster" → myregistry.io/myorg/aws-prod-module-postgres_cluster:v1.0.0
    module "deployment"       → myregistry.io/myorg/aws-prod-module-deployment:v1.0.0
    module "service"          → myregistry.io/myorg/aws-prod-module-service:v1.0.0
    module "ingress_rule"     → myregistry.io/myorg/aws-prod-module-ingress_rule:v1.0.0
    module "dns_record"       → myregistry.io/myorg/aws-prod-module-dns_record:v1.0.0

Proceed with build? [Y/n]:
```

**Non-Interactive Mode (CI/CD):**

```
$ arcctl build datacenter . -t myregistry.io/myorg/aws-prod:v1.0.0 --yes

Building datacenter: aws-production
[info] Root artifact: myregistry.io/myorg/aws-prod:v1.0.0
[info] Module artifact: myregistry.io/myorg/aws-prod-module-k8s:v1.0.0
[info] Module artifact: myregistry.io/myorg/aws-prod-module-namespace:v1.0.0
[info] Module artifact: myregistry.io/myorg/aws-prod-module-postgres_cluster:v1.0.0
...
[build] Building module/k8s...
[build] Building module/namespace...
...
[success] Datacenter built successfully
```

**Overriding Module Artifact Tags:**

```bash
# Override a specific module image
arcctl build datacenter . \
  -t myregistry.io/myorg/aws-prod:v1.0.0 \
  --artifact-tag module/k8s=myregistry.io/myorg/shared-k8s-module:v2.0.0

# Use shared modules from another registry
arcctl build datacenter . \
  -t myregistry.io/myorg/aws-prod:v1.0.0 \
  --artifact-tag module/postgres_cluster=myregistry.io/shared-postgres:latest \
  --artifact-tag module/redis=myregistry.io/shared-redis:latest
```

**Example:**

```bash
# Basic build
arcctl build datacenter . -t ghcr.io/myorg/aws-prod:v1.0.0

# CI/CD build (non-interactive) - using shorthand
arcctl build dc . -t ghcr.io/myorg/aws-prod:$CI_COMMIT_SHA --yes

# Build from specific file
arcctl build datacenter ./datacenters/aws -f datacenter.production.dc -t ghcr.io/myorg/aws-prod:v1.0.0
```

### Datacenter Tag Commands

#### `arcctl tag datacenter`

Create a new tag for an existing datacenter artifact and all its module artifacts.

**Synopsis:**

```bash
arcctl tag datacenter <source> <target> [options]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `<source>` | Source datacenter artifact reference |
| `<target>` | Target tag for the datacenter |

**Options:**

| Option | Description |
|--------|-------------|
| `--artifact-tag <name=repo:tag>` | Override tag for a specific module artifact |
| `--yes, -y` | Non-interactive mode: skip confirmation prompts |

**Example:**

```bash
# Tag a version as latest
arcctl tag datacenter ghcr.io/myorg/aws-prod:v1.0.0 ghcr.io/myorg/aws-prod:latest

# Tag for promotion
arcctl tag datacenter ghcr.io/myorg/aws-prod:v1.0.0 ghcr.io/myorg/aws-prod:stable --yes
```

### Datacenter Push Commands

#### `arcctl push datacenter`

Push a datacenter artifact and all its module artifacts to an OCI registry.

**Synopsis:**

```bash
arcctl push datacenter <repo:tag> [options]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `<repo:tag>` | Datacenter artifact reference to push |

**Options:**

| Option | Description |
|--------|-------------|
| `--yes, -y` | Non-interactive mode: skip confirmation prompts |

**Example:**

```bash
# Push to registry
arcctl push datacenter ghcr.io/myorg/aws-prod:v1.0.0

# CI/CD push (non-interactive)
arcctl push datacenter ghcr.io/myorg/aws-prod:$CI_COMMIT_SHA --yes
```

### Datacenter Deploy Commands

#### `arcctl deploy datacenter`

Create a new datacenter or update an existing one by applying a datacenter configuration. This command provisions or updates the datacenter-level infrastructure (shared resources like Kubernetes clusters, VPCs, etc.) defined in the datacenter config.

**Synopsis:**

```bash
arcctl deploy datacenter <name> <config> [options]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `<name>` | Name for the datacenter (used to identify the datacenter in state and subsequent commands) |

**Options:**

| Option | Description |
|--------|-------------|
| `-c, --config <config-ref>` | Datacenter configuration reference (required). Can be a local path or OCI image reference |
| `--var <key=value>` | Set a datacenter variable (can be specified multiple times) |
| `--var-file <path>` | Load variables from a `.dcvars` file (HCL syntax) |
| `--auto-approve` | Skip interactive approval of the execution plan |
| `--refresh-only` | Only refresh state, don't apply changes |
| `--destroy` | Destroy the datacenter and all its resources |
| `--target <module>` | Target a specific module for deployment (can be specified multiple times) |
| `--backend <type>` | State backend type: `local` (default), `s3`, `gcs`, `azurerm` |
| `--backend-config <key=value>` | Backend-specific configuration (can be specified multiple times) |

**State Backend Configuration:**

State backend can be configured via command flags or environment variables, similar to Terraform. Environment variables use the pattern `ARCCTL_BACKEND_<BACKEND>_<KEY>`:

```bash
# Local state (default) - stores in current working directory
arcctl deploy datacenter my-dc ./dc
# State written to: .arcctl/my-dc/

# Custom local state path via flag
arcctl deploy datacenter my-dc ./dc \
  --backend local \
  --backend-config path=./custom/state/path

# Custom local state path via env var
export ARCCTL_BACKEND=local
export ARCCTL_BACKEND_LOCAL_PATH=./custom/state/path
arcctl deploy datacenter my-dc ./dc

# S3 backend via flags
arcctl deploy datacenter my-dc ./dc \
  --backend s3 \
  --backend-config bucket=my-arcctl-state \
  --backend-config region=us-east-1 \
  --backend-config key=datacenters/my-dc

# S3 backend via environment variables
export ARCCTL_BACKEND=s3
export ARCCTL_BACKEND_S3_BUCKET=my-arcctl-state
export ARCCTL_BACKEND_S3_REGION=us-east-1
export ARCCTL_BACKEND_S3_KEY=datacenters/my-dc
arcctl deploy datacenter my-dc ./dc
```

**Environment Variable Pattern:**

| Environment Variable | Description |
|---------------------|-------------|
| `ARCCTL_BACKEND` | Backend type (`local`, `s3`, `gcs`, `azurerm`) |
| `ARCCTL_BACKEND_<BACKEND>_<KEY>` | Backend-specific config (e.g., `ARCCTL_BACKEND_S3_BUCKET`) |

**Backend Configuration Options:**

| Backend | Config Key | Environment Variable | Description |
|---------|-----------|---------------------|-------------|
| `local` | `path` | `ARCCTL_BACKEND_LOCAL_PATH` | State directory path (default: `.arcctl/`) |
| `s3` | `bucket` | `ARCCTL_BACKEND_S3_BUCKET` | S3 bucket name (required) |
| `s3` | `region` | `ARCCTL_BACKEND_S3_REGION` | AWS region (required) |
| `s3` | `key` | `ARCCTL_BACKEND_S3_KEY` | State file key/path prefix |
| `s3` | `endpoint` | `ARCCTL_BACKEND_S3_ENDPOINT` | Custom S3 endpoint (for MinIO, R2, etc.) |
| `s3` | `access_key` | `ARCCTL_BACKEND_S3_ACCESS_KEY` or `AWS_ACCESS_KEY_ID` | AWS access key |
| `s3` | `secret_key` | `ARCCTL_BACKEND_S3_SECRET_KEY` or `AWS_SECRET_ACCESS_KEY` | AWS secret key |
| `gcs` | `bucket` | `ARCCTL_BACKEND_GCS_BUCKET` | GCS bucket name (required) |
| `gcs` | `prefix` | `ARCCTL_BACKEND_GCS_PREFIX` | State file prefix |
| `azurerm` | `container_name` | `ARCCTL_BACKEND_AZURERM_CONTAINER_NAME` | Azure container name (required) |
| `azurerm` | `storage_account` | `ARCCTL_BACKEND_AZURERM_STORAGE_ACCOUNT` | Azure storage account (required) |

**Config Reference Formats:**

The `--config` flag accepts two formats:

1. **Local path**: Path to a directory containing `datacenter.dc`
   ```bash
   arcctl deploy datacenter my-dc ./datacenters/aws-prod
   ```

2. **OCI image reference**: A built datacenter artifact in `repo:tag` format
   ```bash
   arcctl deploy datacenter my-dc ghcr.io/myorg/aws-prod:v1.0.0
   ```

**Behavior:**

1. **Plan Phase**: arcctl analyzes the datacenter configuration and current state to determine what changes need to be made
2. **Approval Phase**: In interactive mode, displays the execution plan and prompts for confirmation
3. **Apply Phase**: Executes each datacenter-level module in dependency order, updating state as each completes

```
$ arcctl deploy datacenter aws-production ghcr.io/myorg/aws-prod:v1.0.0

Datacenter: aws-production
Config: ghcr.io/myorg/aws-prod:v1.0.0

Planning datacenter deployment...

Execution Plan:

  module "k8s" (pulumi)
    + create: aws:eks:Cluster "aws-production-cluster"
    + create: aws:eks:NodeGroup "aws-production-nodes"
    ~ update: aws:iam:Role "eks-cluster-role"

  module "vpc" (opentofu)
    + create: aws_vpc.main
    + create: aws_subnet.public[0]
    + create: aws_subnet.public[1]

Plan: 5 to create, 1 to update, 0 to destroy

Proceed with deployment? [Y/n]:

[deploy] Deploying module "vpc"...
[deploy] Deploying module "k8s"...
[success] Datacenter deployed successfully

Outputs:
  cluster_endpoint = "https://XXXXX.eks.amazonaws.com"
  kubeconfig       = <sensitive>
```

**Variable Files (`.dcvars` format):**

Variable files use HCL syntax for consistency with datacenter templates:

```hcl
# vars/production.dcvars

region       = "us-east-1"
cluster_name = "production"
domain       = "prod.example.com"

# Complex values are supported
node_pools = {
  default = {
    instance_type = "m5.large"
    min_size      = 3
    max_size      = 10
  }
  gpu = {
    instance_type = "p3.2xlarge"
    min_size      = 0
    max_size      = 4
  }
}

# Lists
availability_zones = ["us-east-1a", "us-east-1b", "us-east-1c"]
```

**Variable Precedence:**

Variables are resolved in the following order (later sources override earlier):

1. Default values in `datacenter.dc`
2. Values from `--var-file`
3. Values from `--var` flags
4. Environment variables (prefixed with `ARCCTL_VAR_`)

```bash
# Using multiple variable sources
arcctl deploy datacenter my-dc \
  --config ./datacenters/aws-prod \
  --var-file ./vars/production.dcvars \
  --var region=us-west-2 \
  --var cluster_name=prod-cluster
```

**Example:**

```bash
# Deploy from local config
arcctl deploy datacenter my-datacenter ./datacenters/aws-prod

# Deploy from OCI artifact
arcctl deploy datacenter my-datacenter ghcr.io/myorg/aws-prod:v1.0.0

# Deploy with variables
arcctl deploy datacenter my-datacenter \
  --config ghcr.io/myorg/aws-prod:v1.0.0 \
  --var region=us-east-1 \
  --var cluster_name=production

# Deploy with auto-approval (CI/CD)
arcctl deploy datacenter my-datacenter \
  --config ghcr.io/myorg/aws-prod:v1.0.0 \
  --var-file ./vars/production.dcvars \
  --auto-approve

# Destroy a datacenter
arcctl deploy datacenter my-datacenter \
  --config ghcr.io/myorg/aws-prod:v1.0.0 \
  --destroy \
  --auto-approve

# Update a specific module only
arcctl deploy datacenter my-datacenter \
  --config ghcr.io/myorg/aws-prod:v1.0.0 \
  --target module.k8s
```

### Environment Commands (`environment` | `env`)

Environments are deployment targets that live within a datacenter. Each environment has its own isolated state, but the datacenter's state backend manages all environment state references. Since the state backend is scoped to a single datacenter, you don't need to specify the datacenter name—it's implicit from the backend configuration.

### Environment Commands

#### `arcctl list environment`

List all environments in the datacenter.

**Synopsis:**

```bash
arcctl list environment [backend-options]
```

**Options:**

| Option | Description |
|--------|-------------|
| `--backend <type>` | State backend type (default: `local`) |
| `--backend-config <key=value>` | Backend-specific configuration |
| `--output <format>` | Output format: `table` (default), `json`, `yaml` |

**Example:**

```bash
# List environments using local state
arcctl list environment

# List environments using S3 backend
arcctl list environment \
  --backend s3 \
  --backend-config bucket=my-arcctl-state \
  --backend-config region=us-east-1

# List environments via env vars
export ARCCTL_BACKEND=s3
export ARCCTL_BACKEND_S3_BUCKET=my-arcctl-state
export ARCCTL_BACKEND_S3_REGION=us-east-1
arcctl list environment

# Output as JSON
arcctl list environment --output json
```

**Output:**

```
$ arcctl list environment

Datacenter: aws-production

ENVIRONMENT   STATUS    COMPONENTS   CREATED              LAST DEPLOYED
staging       healthy   3            2026-01-15 10:30:00  2026-01-28 14:22:00
production    healthy   3            2026-01-10 09:00:00  2026-01-29 08:15:00
preview-123   healthy   2            2026-01-29 11:00:00  2026-01-29 11:05:00
```

#### `arcctl get environment`

Get detailed information about a specific environment.

**Synopsis:**

```bash
arcctl get environment <name> [backend-options]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `<name>` | Environment name |

**Options:**

| Option | Description |
|--------|-------------|
| `--backend <type>` | State backend type (default: `local`) |
| `--backend-config <key=value>` | Backend-specific configuration |
| `--output <format>` | Output format: `table` (default), `json`, `yaml` |

**Example:**

```bash
# Get environment details
arcctl get environment staging

# Get as JSON for scripting
arcctl get environment staging --output json

# With S3 backend
arcctl get environment staging \
  --backend s3 \
  --backend-config bucket=my-arcctl-state \
  --backend-config region=us-east-1
```

**Output:**

```
$ arcctl get environment staging --datacenter aws-production

Environment: staging
Datacenter:  aws-production
Status:      healthy
Created:     2026-01-15 10:30:00
Last Deploy: 2026-01-28 14:22:00

Components:
  NAME          VERSION                              STATUS    REPLICAS
  auth-service  ghcr.io/myorg/auth:v2.1.0           running   2/2
  web-app       ghcr.io/myorg/web-app:v1.5.0        running   2/2
  worker        ghcr.io/myorg/worker:v1.2.0         running   5/5

Resources:
  TYPE        NAME              STATUS
  database    auth-service/main provisioned
  database    web-app/main      provisioned
  database    web-app/cache     provisioned
  bucket      web-app/uploads   provisioned
  route       auth-service/api  https://auth.staging.example.com
  route       web-app/main      https://staging.example.com
```

#### `arcctl create environment`

Create a new environment in the datacenter.

**Synopsis:**

```bash
arcctl create environment <name> [options]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `<name>` | Environment name (must be unique within the datacenter) |

**Options:**

| Option | Description |
|--------|-------------|
| `--if-not-exists` | Exit successfully if environment already exists (useful for CI/CD) |
| `--backend <type>` | State backend type (default: `local`) |
| `--backend-config <key=value>` | Backend-specific configuration |

**Behavior:**

Creates an empty environment entry in the datacenter state. The environment is ready to have components deployed to it after creation.

```
$ arcctl create environment staging

Environment: staging
Datacenter:  aws-production (from state backend)

[create] Creating environment "staging"...
[success] Environment created successfully
```

**Example:**

```bash
# Create a new environment
arcctl create environment staging

# Create with S3 backend
arcctl create environment staging \
  --backend s3 \
  --backend-config bucket=my-arcctl-state \
  --backend-config region=us-east-1

# CI/CD: Create if not exists (idempotent) - using shorthand
arcctl create env preview-$PR_NUMBER --if-not-exists

# Via environment variables
export ARCCTL_BACKEND=s3
export ARCCTL_BACKEND_S3_BUCKET=my-arcctl-state
export ARCCTL_BACKEND_S3_REGION=us-east-1
arcctl create env preview-123 --if-not-exists
```

**CI/CD Usage:**

The `--if-not-exists` flag is particularly useful for preview environments in CI pipelines:

```yaml
# GitHub Actions example
- name: Create Preview Environment
  run: |
    arcctl create environment preview-${{ github.event.pull_request.number }} \
      --if-not-exists \
      --backend s3 \
      --backend-config bucket=${{ secrets.STATE_BUCKET }} \
      --backend-config region=us-east-1
```

When `--if-not-exists` is set and the environment already exists:
- The command exits with code 0 (success)
- A message is logged: `Environment "preview-123" already exists, skipping creation`

#### `arcctl update environment`

Update an existing environment's properties.

**Synopsis:**

```bash
arcctl update environment <name> [options]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `<name>` | Current environment name |

**Options:**

| Option | Description |
|--------|-------------|
| `--name <new-name>` | Rename the environment |
| `--backend <type>` | State backend type (default: `local`) |
| `--backend-config <key=value>` | Backend-specific configuration |

**Behavior:**

Updates environment metadata. Currently, the only editable property is the environment name.

```
$ arcctl update environment staging --name production

Environment: staging → production
Datacenter:  aws-production (from state backend)

[update] Renaming environment "staging" to "production"...
[success] Environment updated successfully
```

**Example:**

```bash
# Rename an environment
arcctl update environment staging --name production

# Rename with S3 backend
arcctl update environment staging \
  --name production \
  --backend s3 \
  --backend-config bucket=my-arcctl-state \
  --backend-config region=us-east-1
```

**Notes:**

- Renaming an environment updates all state references but does not affect deployed resources
- Deployed components continue running; only the environment identifier changes

#### `arcctl destroy environment`

Destroy an environment and all its resources.

**Synopsis:**

```bash
arcctl destroy environment <name> [options]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `<name>` | Environment name to destroy |

**Options:**

| Option | Description |
|--------|-------------|
| `--auto-approve` | Skip interactive confirmation |
| `--backend <type>` | State backend type (default: `local`) |
| `--backend-config <key=value>` | Backend-specific configuration |

**Behavior:**

1. Reads the environment state from the datacenter backend
2. Plans destruction of all resources in reverse dependency order
3. Prompts for confirmation (unless `--auto-approve`)
4. Destroys all resources and removes environment state

```
$ arcctl destroy environment preview-123

Environment: preview-123
Datacenter:  aws-production (from state backend)

Planning environment destruction...

The following resources will be destroyed:

  Component: web-app
    - destroy: route/main
    - destroy: service/api
    - destroy: deployment/api
    - destroy: database/main

  Component: auth-service
    - destroy: route/api
    - destroy: service/api
    - destroy: deployment/api
    - destroy: database/main

Plan: 0 to create, 0 to update, 8 to destroy

This action cannot be undone. All data in databases and buckets will be lost.

Type the environment name to confirm: preview-123

[destroy] Destroying web-app resources...
[destroy] Destroying auth-service resources...
[destroy] Removing environment state...
[success] Environment destroyed successfully
```

**Example:**

```bash
# Interactive destroy (using local state backend)
arcctl destroy environment preview-123

# Auto-approve (CI/CD cleanup) - using shorthand
arcctl env destroy preview-123 --auto-approve

# With S3 backend
arcctl destroy environment preview-123 \
  --backend s3 \
  --backend-config bucket=my-arcctl-state \
  --backend-config region=us-east-1

# Via environment variables
export ARCCTL_BACKEND=s3
export ARCCTL_BACKEND_S3_BUCKET=my-arcctl-state
export ARCCTL_BACKEND_S3_REGION=us-east-1
arcctl destroy environment preview-123 --auto-approve
```

### Environment State Management

Each state backend is scoped to a single datacenter. This means you don't need to specify the datacenter name when running environment commands—the backend configuration implicitly identifies which datacenter you're working with.

When you run environment commands, arcctl:

1. **Connects to the state backend** using the provided `--backend` and `--backend-config` options (or environment variables)
2. **Reads the datacenter state** to get the datacenter name, shared resource outputs (cluster endpoints, etc.), and environment references
3. **Reads/writes environment state** at the location managed by the datacenter state

This design ensures:
- One state backend = one datacenter (simple mental model)
- All state (datacenter and environments) lives in a single backend location
- The datacenter state serves as the "index" for all environments
- Environment state files are automatically created, updated, and deleted by arcctl
- No separate backend configuration is needed per-environment
- No need to specify `--datacenter` on every command

**State Location:**

Environment state is stored relative to the datacenter state:

```
<backend>/
  datacenters/
    <datacenter-name>/
      datacenter.state.json           # Datacenter state (contains env references)
      environments/
        <env-name>/
          environment.state.json      # Environment metadata and resource index
          modules/
            <component>--<resource>.state.json  # Per-resource IaC state
```

**Datacenter State References:**

The datacenter state file tracks all environments:

```json
{
  "version": 1,
  "datacenter": {
    "name": "aws-production",
    "config": "ghcr.io/myorg/aws-prod:v1.0.0"
  },
  "environments": {
    "staging": {
      "state_path": "environments/staging/",
      "created_at": "2026-01-15T10:30:00Z",
      "last_deployed_at": "2026-01-28T14:22:00Z",
      "config": "./environments/staging.yml"
    },
    "production": {
      "state_path": "environments/production/",
      "created_at": "2026-01-10T09:00:00Z",
      "last_deployed_at": "2026-01-29T08:15:00Z",
      "config": "ghcr.io/myorg/prod-env:v2.0.0"
    }
  }
}
```

### Datacenter State Management

arcctl manages state for datacenters and environments similarly to Terraform and Pulumi, tracking the mapping between configuration and real infrastructure resources.

#### State Structure

Datacenter state is organized hierarchically:

```
<state-backend>/
  datacenters/
    <datacenter-name>/
      datacenter.state.json      # Root datacenter state (umbrella)
      modules/
        k8s.state.json           # State for "k8s" module (Pulumi/Tofu state)
        vpc.state.json           # State for "vpc" module
        ...
      environments/
        <env-name>/
          environment.state.json # Environment-level state
          modules/
            namespace.state.json # State for environment modules
            ...
          resources/
            <component>/
              <resource>.state.json  # Per-resource state
```

#### Datacenter State (`datacenter.state.json`)

The root datacenter state file contains:

- Datacenter metadata (name, config reference, version)
- References to all module states
- Datacenter-level outputs
- Lock information

```json
{
  "version": 1,
  "datacenter": {
    "name": "aws-production",
    "config": "ghcr.io/myorg/aws-prod:v1.0.0",
    "variables": {
      "region": "us-east-1",
      "cluster_name": "production"
    }
  },
  "modules": {
    "k8s": {
      "plugin": "pulumi",
      "state_file": "modules/k8s.state.json",
      "outputs": {
        "cluster_endpoint": "https://...",
        "kubeconfig": "<sensitive>"
      }
    },
    "vpc": {
      "plugin": "opentofu",
      "state_file": "modules/vpc.state.json",
      "outputs": {
        "vpc_id": "vpc-123456",
        "subnet_ids": ["subnet-a", "subnet-b"]
      }
    }
  },
  "outputs": {
    "cluster_endpoint": "https://...",
    "region": "us-east-1"
  },
  "environments": ["staging", "production"],
  "lock": null
}
```

#### Module State Files

Each module's state file contains the native state format for the IaC plugin:

- **Pulumi modules**: Contains Pulumi stack state (JSON format)
- **OpenTofu modules**: Contains Terraform/OpenTofu state (JSON format)

These state files are managed directly by the underlying IaC tool and encapsulated by arcctl.

#### Environment State Isolation

Each environment has its own state files, separate from the datacenter and other environments. This provides:

1. **Parallel operations**: Multiple environments can be modified simultaneously without blocking each other
2. **Isolation**: A failed operation in one environment doesn't affect others
3. **Independent lifecycle**: Environments can be created and destroyed without impacting siblings

```bash
# Concurrent operations are safe because state is isolated
arcctl apply ./staging.yml &
arcctl apply ./production.yml &
wait
```

#### State Locking

arcctl implements state locking to prevent concurrent modifications:

- **Datacenter-level lock**: Acquired when deploying datacenter-level modules
- **Environment-level lock**: Acquired when deploying to a specific environment

Locks are advisory and include:
- Lock holder identity (user, hostname, or CI job ID)
- Timestamp
- Operation being performed

```
$ arcctl deploy datacenter my-dc ./dc

Error: State is locked

  Lock ID:    abc123
  Locked by:  user@hostname
  Locked at:  2026-01-29T10:30:00Z
  Operation:  datacenter deploy

Use --force-unlock to break the lock (use with caution).
```

#### State Backends

State backend is configured per-command via `--backend` and `--backend-config` flags or environment variables (similar to Terraform). Environment variables follow the pattern `ARCCTL_BACKEND_<BACKEND>_<KEY>`. If no backend is specified, state is stored locally in the current working directory.

**Local State (default):**

```bash
# State stored in .arcctl/<datacenter-name>/
arcctl deploy datacenter my-dc ./dc

# Custom local path via flags
arcctl deploy datacenter my-dc ./dc \
  --backend local \
  --backend-config path=/var/lib/arcctl/state

# Custom local path via env vars
export ARCCTL_BACKEND=local
export ARCCTL_BACKEND_LOCAL_PATH=/var/lib/arcctl/state
arcctl deploy datacenter my-dc ./dc
```

**Remote State via Flags:**

```bash
# S3 backend
arcctl deploy datacenter my-dc ./dc \
  --backend s3 \
  --backend-config bucket=my-arcctl-state \
  --backend-config region=us-east-1
```

**Remote State via Environment Variables:**

```bash
export ARCCTL_BACKEND=s3
export ARCCTL_BACKEND_S3_BUCKET=my-arcctl-state
export ARCCTL_BACKEND_S3_REGION=us-east-1

arcctl deploy datacenter my-dc ./dc
```

Supported backends:
- `local` - Local filesystem (default, stores in `.arcctl/`)
- `s3` - S3-compatible storage (AWS S3, MinIO, DigitalOcean Spaces, Cloudflare R2)
- `gcs` - Google Cloud Storage
- `azurerm` - Azure Blob Storage

### Compiled Artifact Schemas

When a component or datacenter is built, source references (`build:` blocks) are transformed into image references (`image:`) pointing to the compiled child artifacts. This section describes the schema differences between source and compiled forms.

#### Component: Source vs Compiled Form

**Source Form (architect.yml - developer writes this):**

```yaml
name: my-app

databases:
  main:
    type: postgres:^15
    migrations:
      build:
        context: ./database
        dockerfile: Dockerfile.migrations
      command: ["npm", "run", "migrate"]

deployments:
  api:
    build:
      context: ./api
      dockerfile: Dockerfile
    environment:
      DATABASE_URL: ${{ databases.main.url }}
```

**Compiled Form (stored in OCI artifact):**

```yaml
name: my-app

databases:
  main:
    type: postgres:^15
    migrations:
      image: myregistry.io/myorg/myapp-migration-main:v1.0.0
      command: ["npm", "run", "migrate"]

deployments:
  api:
    image: myregistry.io/myorg/myapp-deployment-api:v1.0.0
    environment:
      DATABASE_URL: ${{ databases.main.url }}
```

#### Datacenter: Source vs Compiled Form

**Source Form (datacenter.dc - platform engineer writes this):**

```hcl
module "k8s" {
  build = "./modules/k8s-cluster"
  inputs = {
    name   = "${datacenter.name}-cluster"
    region = variable.region
  }
}

environment {
  module "namespace" {
    build = "./modules/k8s-namespace"
    inputs = {
      name = environment.name
    }
  }
}
```

**Compiled Form (stored in OCI artifact):**

```hcl
module "k8s" {
  source = "myregistry.io/myorg/aws-prod-module-k8s:v1.0.0"
  inputs = {
    name   = "${datacenter.name}-cluster"
    region = variable.region
  }
}

environment {
  module "namespace" {
    source = "myregistry.io/myorg/aws-prod-module-namespace:v1.0.0"
    inputs = {
      name = environment.name
    }
  }
}
```

#### Schema Updates for Migrations

To support both source and compiled forms, the migrations schema accepts either `build` or `image`:

**Database Migrations Properties (updated):**

| Property | Type | Description |
|----------|------|-------------|
| `migrations.build` | object | Build configuration for migration container (source form, mutually exclusive with `image`) |
| `migrations.build.context` | string | Build context directory |
| `migrations.build.dockerfile` | string | Path to Dockerfile within context (default: `"Dockerfile"`) |
| `migrations.image` | string | Pre-built migration container image (compiled form, mutually exclusive with `build`) |
| `migrations.command` | string[] | Command to run migrations (overrides container default) |
| `migrations.environment` | map | Additional environment variables for migration container |

**Example - Using pre-built migration image:**

```yaml
databases:
  main:
    type: postgres:^15
    migrations:
      image: myregistry.io/myorg/migrations:v1.0.0
      command: ["npm", "run", "migrate"]
```

### Ephemeral Environment Command (`up`)

These commands provide a simplified workflow for creating and managing ephemeral environments, ideal for local development and testing.

#### `arcctl up`

Create and run an ephemeral environment. By default, the command stays attached and destroys the environment on Ctrl+C.

**Synopsis:**

```bash
arcctl up [config-ref] [options]
```

**Arguments:**

| Argument | Description |
|----------|-------------|
| `[config-ref]` | Optional. Component image, local component path, or local environment path. Defaults to current directory |

**Options:**

| Option | Description |
|--------|-------------|
| `-d, --detach` | Detach after environment is ready (leave running in background) |
| `-n, --name <name>` | Environment name (default: auto-generated from directory name + random suffix) |
| `--var <key=value>` | Set a variable (can be specified multiple times) |
| `--var-file <path>` | Load variables from a `.dcvars` file |
| `--backend <type>` | State backend type (default: `local`) |
| `--backend-config <key=value>` | Backend-specific configuration |
| `--timeout <duration>` | Timeout for environment to become ready (default: `10m`) |

**Config Detection:**

When no `config-ref` is provided, arcctl searches the current directory for (in order):

1. `environment.yml` or `environment.yaml` - Environment configuration
2. `architect.yml` or `architect.yaml` - Component configuration

If a component config is found, arcctl creates a minimal environment with just that component.

**Default Filenames:**

| File | Purpose |
|------|---------|
| `environment.yml` | Environment configuration (multiple components) |
| `architect.yml` | Component configuration (single component) |

**Behavior (Attached Mode - Default):**

1. **Detect/Parse**: Find and parse the configuration
2. **Create Environment**: Create an ephemeral environment with auto-generated name
3. **Deploy**: Deploy components and show progress
4. **Display Status**: Show environment status and route URLs
5. **Watch**: Keep running and show status updates
6. **Cleanup on Exit**: Destroy environment when user presses Ctrl+C

```
$ arcctl up

Detecting configuration...
Found: ./architect.yml (component: web-app)

Creating ephemeral environment: web-app-a1b2c3
Datacenter: aws-production (from state backend)

[deploy] Creating database "main"...
[deploy] Creating deployment "api"...
[deploy] Creating service "api"...
[deploy] Creating route "main"...

Environment ready! (took 2m 34s)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Environment: web-app-a1b2c3
Status:      running

Routes:
  main → https://web-app-a1b2c3.dev.example.com

Components:
  web-app    running    4 resources

Press Ctrl+C to stop and destroy the environment...
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

^C

Shutting down...
[destroy] Destroying route "main"...
[destroy] Destroying service "api"...
[destroy] Destroying deployment "api"...
[destroy] Destroying database "main"...
[destroy] Removing environment...

Environment destroyed. Goodbye!
```

**Behavior (Detached Mode - `-d`):**

With `-d`, arcctl exits after the environment is ready, leaving it running:

```
$ arcctl up -d

Detecting configuration...
Found: ./architect.yml (component: web-app)

Creating ephemeral environment: web-app-a1b2c3
Datacenter: aws-production (from state backend)

[deploy] Creating database "main"...
[deploy] Creating deployment "api"...
[deploy] Creating service "api"...
[deploy] Creating route "main"...

Environment ready! (took 2m 34s)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Environment: web-app-a1b2c3
Status:      running

Routes:
  main → https://web-app-a1b2c3.dev.example.com

Run 'arcctl env destroy web-app-a1b2c3' to destroy the environment.
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

**Examples:**

```bash
# Auto-detect config in current directory
arcctl up

# Specify a component image
arcctl up ghcr.io/myorg/web-app:v1.5.0

# Specify a local component path
arcctl up ./services/web-app

# Specify an environment config
arcctl up ./environments/dev.yml

# Run detached (background)
arcctl up -d

# Custom environment name
arcctl up --name my-test-env

# With variables
arcctl up --var log_level=debug --var feature_flag=true

# With S3 backend (for shared/team environments)
arcctl up -d \
  --backend s3 \
  --backend-config bucket=my-arcctl-state \
  --backend-config region=us-east-1
```

#### Environment Naming

By default, ephemeral environments are named using the pattern:

```
<base-name>-<random-suffix>
```

Where:
- `base-name` is derived from the directory name or component name
- `random-suffix` is a 6-character alphanumeric string

Examples:
- `web-app-a1b2c3`
- `my-project-x7y8z9`
- `api-service-m3n4o5`

Custom names can be specified with `--name`:

```bash
arcctl up --name my-feature-test
```

### CI/CD Structured Output

Traditional IaC tools hide their entire execution workflow inside a single `apply` command, making it difficult to see progress in CI systems. arcctl provides rich, structured output that integrates with CI system features for better visibility—without requiring multiple jobs or complex workflow generation.

#### Design Philosophy

Rather than trying to map infrastructure resources to CI jobs (which is impractical due to dynamic plans and CI system limitations), arcctl runs as a single CI job but outputs structured, CI-aware progress information that each platform can render optimally.

**Why not separate CI jobs per resource?**

1. **Plans are dynamic** - The execution plan changes based on current state, datacenter changes, and component dependencies. A pre-generated workflow would be immediately stale.
2. **Depth limitations** - CI systems don't support dynamic job creation. You'd need to pre-define N wave jobs, limiting dependency depth.
3. **State complexity** - Passing outputs between jobs (e.g., VPC ID to EKS) requires artifact management and adds failure modes.

#### CI Environment Auto-Detection

arcctl automatically detects the CI environment and adjusts its output format accordingly:

| Environment Variable | Detected CI System |
|---------------------|-------------------|
| `GITHUB_ACTIONS=true` | GitHub Actions |
| `GITLAB_CI=true` | GitLab CI |
| `CIRCLECI=true` | CircleCI |
| `TF_BUILD=true` | Azure Pipelines |
| `BUILDKITE=true` | Buildkite |
| `JENKINS_URL` set | Jenkins |
| None detected | Plain terminal output |

Override auto-detection with `--ci <system>` or disable CI formatting with `--ci none`.

#### Output Formats

**`--output` Flag Options:**

| Format | Description |
|--------|-------------|
| `auto` | Auto-detect CI system and use native formatting (default) |
| `json` | Machine-readable JSON for custom tooling |
| `plain` | Plain text with no CI-specific formatting |

#### GitHub Actions Output

arcctl uses GitHub Actions' native features for structured output:

- **Collapsible groups** (`::group::` / `::endgroup::`) for each wave and resource
- **Job summaries** (written to `$GITHUB_STEP_SUMMARY`) with deployment graph
- **Annotations** (`::error::` / `::warning::`) for failures and drift detection
- **Problem matchers** for parsing resource errors

**Example Output:**

```
::group::📋 Execution Plan
┌─────────────────────────────────────────────────────────────┐
│ Datacenter: aws-prod                                        │
│ Operation:  deploy                                          │
│ Changes:    +3 create, ~1 update, -0 destroy                │
└─────────────────────────────────────────────────────────────┘

Wave 0 (parallel):
  + module.vpc        create
  + module.iam        create

Wave 1 (parallel):
  + module.eks        create    (depends: vpc, iam)
  ~ module.rds        update    (depends: vpc)

Wave 2:
  + module.nginx      create    (depends: eks)
::endgroup::

::group::🌊 Wave 0 - Deploying 2 resources in parallel
  ├─ module.vpc ........ ✓ created (45s)
  └─ module.iam ........ ✓ created (12s)
::endgroup::

::group::🌊 Wave 1 - Deploying 2 resources in parallel
  ├─ module.eks ........ ✓ created (8m 23s)
  └─ module.rds ........ ✓ updated (2m 15s)
::endgroup::

::group::🌊 Wave 2 - Deploying 1 resource
  └─ module.nginx ...... ✓ created (34s)
::endgroup::

::notice::Deployment complete: 3 created, 1 updated in 11m 09s
```

**Job Summary (Markdown):**

arcctl writes a deployment summary to `$GITHUB_STEP_SUMMARY`:

```markdown
## 🚀 Deployment Summary

**Datacenter:** aws-prod  
**Duration:** 11m 09s  
**Result:** ✅ Success

### Execution Graph

```mermaid
graph LR
    vpc[vpc ✓] --> eks[eks ✓]
    iam[iam ✓] --> eks
    vpc --> rds[rds ✓]
    eks --> nginx[nginx ✓]
```

### Resources

| Resource | Action | Duration | Status |
|----------|--------|----------|--------|
| module.vpc | create | 45s | ✓ |
| module.iam | create | 12s | ✓ |
| module.eks | create | 8m 23s | ✓ |
| module.rds | update | 2m 15s | ✓ |
| module.nginx | create | 34s | ✓ |
```

#### GitLab CI Output

arcctl uses GitLab CI's collapsible sections and artifacts:

- **Collapsible sections** (`\e[0Ksection_start` / `section_end`) for waves
- **Job artifacts** with deployment reports (JUnit XML for resource status)
- **Merge request comments** (via CI variables) with deployment summary

**Example Output:**

```
section_start:1234567890:plan[collapsed=false]
\r\e[0K📋 Execution Plan
┌─────────────────────────────────────────────────────────────┐
│ Datacenter: aws-prod                                        │
│ Changes:    +3 create, ~1 update, -0 destroy                │
└─────────────────────────────────────────────────────────────┘
section_end:1234567890:plan

section_start:1234567891:wave_0[collapsed=false]
\r\e[0K🌊 Wave 0 - Deploying 2 resources in parallel
  ├─ module.vpc ........ ✓ created (45s)
  └─ module.iam ........ ✓ created (12s)
section_end:1234567891:wave_0

section_start:1234567892:wave_1[collapsed=true]
\r\e[0K🌊 Wave 1 - Deploying 2 resources in parallel
  ├─ module.eks ........ ✓ created (8m 23s)
  └─ module.rds ........ ✓ updated (2m 15s)
section_end:1234567892:wave_1
```

#### CircleCI Output

CircleCI lacks native grouping, so arcctl uses clear visual separators and writes detailed artifacts:

**Example Output:**

```
════════════════════════════════════════════════════════════════
📋 EXECUTION PLAN
════════════════════════════════════════════════════════════════
Datacenter: aws-prod
Changes:    +3 create, ~1 update, -0 destroy

Wave 0: module.vpc, module.iam
Wave 1: module.eks, module.rds
Wave 2: module.nginx
════════════════════════════════════════════════════════════════

────────────────────────────────────────────────────────────────
🌊 WAVE 0 - Deploying 2 resources in parallel
────────────────────────────────────────────────────────────────
  ├─ module.vpc ........ ✓ created (45s)
  └─ module.iam ........ ✓ created (12s)

────────────────────────────────────────────────────────────────
🌊 WAVE 1 - Deploying 2 resources in parallel
────────────────────────────────────────────────────────────────
  ├─ module.eks ........ ✓ created (8m 23s)
  └─ module.rds ........ ✓ updated (2m 15s)
```

**Artifacts:** arcctl writes `deployment-report.json` and `deployment-report.xml` (JUnit format) for CircleCI's test results UI.

#### Azure Pipelines Output

arcctl uses Azure Pipelines' logging commands:

- **Collapsible groups** (`##[group]` / `##[endgroup]`)
- **Task commands** (`##[section]`, `##vso[task.logissue]`)
- **Build timeline** updates via REST API (optional, requires PAT)

**Example Output:**

```
##[group]📋 Execution Plan
Datacenter: aws-prod
Changes:    +3 create, ~1 update, -0 destroy
##[endgroup]

##[group]🌊 Wave 0 - Deploying 2 resources in parallel
##[section]Deploying module.vpc
  module.vpc ........ ✓ created (45s)
##[section]Deploying module.iam
  module.iam ........ ✓ created (12s)
##[endgroup]
```

#### Buildkite Output

arcctl uses Buildkite's annotation API and collapsible sections:

- **Collapsible sections** (`---` and `+++` markers)
- **Annotations** (via `buildkite-agent annotate`) for summary
- **Metadata** (via `buildkite-agent meta-data set`) for downstream steps

**Example Output:**

```
+++ 📋 Execution Plan
Datacenter: aws-prod
Changes:    +3 create, ~1 update, -0 destroy

--- 🌊 Wave 0
  ├─ module.vpc ........ ✓ created (45s)
  └─ module.iam ........ ✓ created (12s)

--- 🌊 Wave 1
  ├─ module.eks ........ ✓ created (8m 23s)
  └─ module.rds ........ ✓ updated (2m 15s)

+++ ✅ Deployment Complete
3 created, 1 updated in 11m 09s
```

#### JSON Output for Custom Tooling

Use `--output json` for machine-readable output that can power custom dashboards, notifications, or integrations:

```bash
arcctl dc deploy my-dc ./dc --output json
```

**Output Structure:**

```json
{
  "version": 1,
  "operation": "deploy",
  "target": {
    "type": "datacenter",
    "name": "my-dc",
    "config": "ghcr.io/myorg/aws-prod:v1.0.0"
  },
  "plan": {
    "waves": [
      {
        "index": 0,
        "resources": [
          {"id": "module.vpc", "name": "vpc", "type": "module", "action": "create", "dependencies": []},
          {"id": "module.iam", "name": "iam", "type": "module", "action": "create", "dependencies": []}
        ]
      },
      {
        "index": 1,
        "resources": [
          {"id": "module.eks", "name": "eks", "type": "module", "action": "create", "dependencies": ["module.vpc", "module.iam"]},
          {"id": "module.rds", "name": "rds", "type": "module", "action": "update", "dependencies": ["module.vpc"]}
        ]
      }
    ],
    "summary": {"create": 3, "update": 1, "destroy": 0, "total_waves": 3}
  },
  "execution": {
    "started_at": "2026-01-29T10:30:00Z",
    "completed_at": "2026-01-29T10:41:09Z",
    "duration_seconds": 669,
    "resources": [
      {"id": "module.vpc", "action": "create", "status": "success", "duration_seconds": 45, "wave": 0},
      {"id": "module.iam", "action": "create", "status": "success", "duration_seconds": 12, "wave": 0},
      {"id": "module.eks", "action": "create", "status": "success", "duration_seconds": 503, "wave": 1},
      {"id": "module.rds", "action": "update", "status": "success", "duration_seconds": 135, "wave": 1},
      {"id": "module.nginx", "action": "create", "status": "success", "duration_seconds": 34, "wave": 2}
    ]
  },
  "result": {
    "status": "success",
    "created": 3,
    "updated": 1,
    "destroyed": 0,
    "failed": 0
  }
}
```

#### Plan-Only Mode

Use `--plan` to generate the execution plan without applying changes:

```bash
# View plan in terminal
arcctl dc deploy my-dc ./dc --plan

# Export plan as JSON
arcctl dc deploy my-dc ./dc --plan --output json > plan.json
```

This is useful for:
- **PR comments** - Show what would change before merging
- **Approval workflows** - Review plan before applying
- **Custom orchestration** - Parse plan and implement custom execution logic

#### Workflow Examples

**GitHub Actions with Plan/Apply:**

```yaml
name: Deploy Infrastructure

on:
  push:
    branches: [main]
  pull_request:

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Setup arcctl
        uses: architect-io/setup-arcctl@v1
      
      # On PRs, show plan only
      - name: Plan
        if: github.event_name == 'pull_request'
        run: arcctl dc deploy my-dc ./dc --plan
      
      # On main, apply changes
      - name: Deploy
        if: github.ref == 'refs/heads/main'
        run: arcctl dc deploy my-dc ./dc --auto-approve
        env:
          ARCCTL_BACKEND: s3
          ARCCTL_BACKEND_S3_BUCKET: ${{ secrets.STATE_BUCKET }}
          ARCCTL_BACKEND_S3_REGION: us-east-1
```

**GitLab CI with Environments:**

```yaml
stages:
  - plan
  - deploy

plan:
  stage: plan
  script:
    - arcctl dc deploy my-dc ./dc --plan
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"

deploy:
  stage: deploy
  script:
    - arcctl dc deploy my-dc ./dc --auto-approve
  environment:
    name: production
  rules:
    - if: $CI_COMMIT_BRANCH == "main"
```

#### Benefits

1. **Visibility** - See wave-by-wave progress with collapsible sections
2. **Native integration** - Uses each CI system's native features (no workarounds)
3. **Rich summaries** - Mermaid diagrams, tables, and timing in job summaries
4. **Machine-readable** - JSON output for custom integrations
5. **No workflow generation** - Plans are dynamic; output adapts automatically
6. **Parallelization** - arcctl handles parallel execution within waves internally

### CI/CD Integration Examples

#### GitHub Actions

```yaml
name: Build and Push Component

on:
  push:
    tags:
      - 'v*'

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Login to GitHub Container Registry
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      
      - name: Install arcctl
        run: |
          curl -sSL https://get.arcctl.dev | sh
      
      - name: Build Component
        run: |
          arcctl component build . \
            -t ghcr.io/${{ github.repository }}:${{ github.ref_name }} \
            --yes
      
      - name: Push Component
        run: |
          arcctl component push ghcr.io/${{ github.repository }}:${{ github.ref_name }} --yes
      
      - name: Tag as Latest
        if: startsWith(github.ref, 'refs/tags/v')
        run: |
          arcctl component tag \
            ghcr.io/${{ github.repository }}:${{ github.ref_name }} \
            ghcr.io/${{ github.repository }}:latest \
            --yes
          arcctl component push ghcr.io/${{ github.repository }}:latest --yes
```

#### GitLab CI

```yaml
build-component:
  stage: build
  image: arcctl/cli:latest
  script:
    - docker login -u $CI_REGISTRY_USER -p $CI_REGISTRY_PASSWORD $CI_REGISTRY
    - arcctl component build . -t $CI_REGISTRY_IMAGE:$CI_COMMIT_SHA --yes
    - arcctl component push $CI_REGISTRY_IMAGE:$CI_COMMIT_SHA --yes
  only:
    - main
    - tags

tag-release:
  stage: release
  image: arcctl/cli:latest
  script:
    - docker login -u $CI_REGISTRY_USER -p $CI_REGISTRY_PASSWORD $CI_REGISTRY
    - arcctl component tag $CI_REGISTRY_IMAGE:$CI_COMMIT_SHA $CI_REGISTRY_IMAGE:$CI_COMMIT_TAG --yes
    - arcctl component push $CI_REGISTRY_IMAGE:$CI_COMMIT_TAG --yes
  only:
    - tags
```

#### Datacenter Deployment with Remote State (GitHub Actions)

```yaml
name: Deploy Datacenter

on:
  push:
    branches: [main]
    paths:
      - 'datacenters/**'

jobs:
  deploy:
    runs-on: ubuntu-latest
    env:
      # State backend configuration via environment variables
      ARCCTL_BACKEND: s3
      ARCCTL_BACKEND_S3_BUCKET: ${{ secrets.STATE_BUCKET }}
      ARCCTL_BACKEND_S3_REGION: us-east-1
    steps:
      - uses: actions/checkout@v4
      
      - name: Configure AWS Credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          aws-access-key-id: ${{ secrets.AWS_ACCESS_KEY_ID }}
          aws-secret-access-key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          aws-region: us-east-1
      
      - name: Install arcctl
        run: curl -sSL https://get.arcctl.dev | sh
      
      - name: Deploy Datacenter
        run: |
          arcctl deploy datacenter production \
            --config ./datacenters/aws-prod \
            --var-file ./vars/production.dcvars \
            --auto-approve
```

---

## State Management

arcctl uses persisted state to track infrastructure resources, similar to Terraform and Pulumi. State enables arcctl to map configuration to real resources, plan changes, and manage the full lifecycle of datacenters and environments.

For detailed state structure and CLI commands, see [Datacenter State Management](#datacenter-state-management) in the CLI Commands section.

### What State Tracks

- **Datacenters**: Configuration reference, variables, module outputs
- **Modules**: Native IaC state (Pulumi or OpenTofu format) encapsulated per module
- **Environments**: Deployed components, resource outputs, relationships
- **Resources**: Individual resource state within environments

### State Isolation

State is organized to allow parallel operations:

- **Datacenter-level state**: Umbrella state for shared infrastructure (clusters, VPCs)
- **Environment-level state**: Separate state files per environment, enabling concurrent deployments

### State Backends

State backend is configured per-command via flags (`--backend`, `--backend-config`) or environment variables (`ARCCTL_BACKEND`, `ARCCTL_BACKEND_*`), similar to Terraform.

**Local State** (default):

- State files stored in `.arcctl/` (current working directory)
- Suitable for individual use and development
- Path can be customized with `--backend-config path=<path>`

**Remote State**:

- Configured via `--backend` flag or `ARCCTL_BACKEND` environment variable
- Stored in cloud storage (S3, GCS, Azure)
- Enables team collaboration
- Required for CI/CD pipelines
- Supports state locking for safe concurrent access

Supported backends:

- `local` - Local filesystem (default)
- `s3` - S3-compatible storage (AWS S3, DigitalOcean Spaces, MinIO, R2)
- `gcs` - Google Cloud Storage
- `azurerm` - Azure Blob Storage

---

## Security Considerations

### Credential Management

- Components declare database/service dependencies
- Datacenters provision unique credentials per application
- Credentials are injected at deploy time
- Support for secret rotation policies

### Network Security

- arcctl understands the relationship between resources
- Datacenters can configure zero-trust networking
- Only resources that should communicate can resolve each other

### Secret Handling

- Variables marked as `secret: true` are treated as sensitive
- Secrets are stored using the datacenter's secret hook
- Secrets are never logged or displayed in plain text

---

## Portability

### Vertical Portability

The same component can run in:

- Local development environments
- CI/CD preview environments
- Staging environments
- Production environments

### Horizontal Portability

Components can move between:

- Different cloud providers (AWS, GCP, Azure)
- Different orchestrators (Kubernetes, ECS)
- Different datacenters within the same provider

This is achieved because components don't contain infrastructure details—only application requirements that any datacenter can fulfill.

---

## Appendix

### Supported Database Types

| Type       | Description |
| ---------- | ----------- |
| `postgres` | PostgreSQL  |
| `mysql`    | MySQL       |
| `mongodb`  | MongoDB     |
| `redis`    | Redis       |

(Extensible based on datacenter implementation)

### Supported Bucket Types

| Type         | Description                                                      |
| ------------ | ---------------------------------------------------------------- |
| `s3`         | S3-compatible storage (AWS S3, MinIO, R2, DigitalOcean Spaces)   |
| `gcs`        | Google Cloud Storage (planned)                                   |
| `azure-blob` | Azure Blob Storage (planned)                                     |

(Extensible based on datacenter implementation)

### Supported IaC Frameworks

| Framework      | Status    |
| -------------- | --------- |
| Pulumi         | Supported |
| OpenTofu       | Supported |
| CloudFormation | Planned   |

### HCL Expression Functions (Datacenter)

| Function | Description              | Example                                       |
| -------- | ------------------------ | --------------------------------------------- |
| `contains` | Check if array contains value | `contains(environment.nodes.*.type, "database")` |
| `length` | Get array length         | `length(environment.resources.routes) > 0`    |
| `merge`  | Merge two maps           | `merge(node.inputs, { namespace = "default" })` |
| `coalesce` | Return first non-null value | `coalesce(node.inputs.port, 8080)` |
| `format` | Format string            | `format("%s-%s", environment.name, node.name)` |

### Component Expression Syntax

Components continue to use the `${{ }}` expression syntax for referencing values:

| Function | Description              | Example                                       |
| -------- | ------------------------ | --------------------------------------------- |
| `where`  | Filter array by property | `${{ resources \| where "type" "postgres" }}` |
| `length` | Get array length         | `${{ resources \| length }}`                  |
| `join`   | Join array with delimiter | `${{ dependents.*.routes.*.url \| join "," }}` |
| `first`  | Get first element of array | `${{ dependents.*.routes.*.url \| first }}` |
| `map`    | Extract property from array | `${{ dependents.* \| map "name" }}` |

**Wildcard Syntax:**

The `*` wildcard can be used to expand arrays in expressions:

- `${{ dependents.*.routes.*.url }}` - Expands to an array of all route URLs from all dependents
- `${{ dependencies.*.services.api.url }}` - Expands to an array of API service URLs from all dependencies

---

## Open Questions

1. Should we support additional IaC frameworks beyond Pulumi and OpenTofu?
2. What is the minimum set of resource hooks a datacenter must implement?
3. Should components support custom resource types for datacenter-specific features?
4. How should we handle breaking changes to the component/datacenter contract?
5. What is the versioning strategy for component and datacenter schemas?
6. How should route priority/precedence be handled when multiple routes match the same request? (e.g., follow Gateway API merging rules: most specific match wins, then longest path prefix, then alphabetical by route name)
7. Should we support TCPRoute and UDPRoute types for non-HTTP workloads?
8. How should cross-component route conflicts be detected and resolved? (e.g., two components claiming the same hostname/path)
9. What OCI artifact format should be used for component/datacenter root artifacts? (e.g., custom media types, annotations, manifest structure)
10. How should registry authentication be handled for `arcctl push component` and `arcctl push datacenter`? (e.g., Docker credential helpers, environment variables, config file)
11. Should we support multi-platform builds for container child artifacts (e.g., linux/amd64 + linux/arm64)?
12. How should state migration be handled when upgrading arcctl versions or changing state backends?
13. Should we support state import for existing infrastructure not originally created by arcctl?
14. How should sensitive values in state be encrypted? (e.g., at-rest encryption, integration with cloud KMS)

---

## Revision History

| Version | Date       | Author | Changes                                              |
| ------- | ---------- | ------ | ---------------------------------------------------- |
| 0.13    | 2026-01-29 | -      | Redesigned CI/CD output: structured, CI-aware output instead of workflow generation |
| 0.12    | 2026-01-29 | -      | Added arcctl up command for ephemeral environments |
| 0.11    | 2026-01-29 | -      | Enhanced dockerBuild hook to support remote registry push for local source deployments |
| 0.10    | 2026-01-29 | -      | Added component deployment commands (list, get, deploy, destroy) with --environment flag |
| 0.9     | 2026-01-29 | -      | Added environment CLI commands (list, get, create, update, destroy) |
| 0.8     | 2026-01-29 | -      | Added datacenter deploy command and state management documentation |
| 0.7     | 2026-01-29 | -      | Added CLI commands for build, tag, and push (components and datacenters) |
| 0.6     | 2026-01-29 | -      | Added database migrations/seeding support with build context |
| 0.5     | 2026-01-29 | -      | Added dependents expression syntax for upstream component references |
| 0.4     | 2026-01-29 | -      | Added buckets resource type for blob storage (S3)   |
| 0.3     | 2026-01-29 | -      | Replaced ingresses with Gateway API-style routes    |
| 0.2     | 2026-01-29 | -      | Added functions resource type for serverless apps   |
| 0.1     | 2026-01-29 | -      | Initial draft                                        |
