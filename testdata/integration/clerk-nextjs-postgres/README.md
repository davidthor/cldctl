# Clerk + Next.js + PostgreSQL Integration Test

This integration test validates the full deployment flow of a Next.js application with Clerk authentication and PostgreSQL database using cldctl.

## Overview

The test deploys:

- **Clerk component** - Configuration passthrough for Clerk authentication credentials
- **Next.js application** - With protected API routes and database connectivity
- **PostgreSQL database** - For data persistence

## Prerequisites

1. **Docker** - Required for the local datacenter
2. **Clerk account** - Get credentials from [Clerk Dashboard](https://dashboard.clerk.com)
3. **Go 1.21+** - For running the tests
4. **pnpm** - For Node.js dependency management

## Configuration

### Required Environment Variables

| Variable                | Description           | Example       |
| ----------------------- | --------------------- | ------------- |
| `CLERK_PUBLISHABLE_KEY` | Clerk publishable key | `pk_test_...` |
| `CLERK_SECRET_KEY`      | Clerk secret key      | `sk_test_...` |

**Note:** Clerk automatically infers the domain from the publishable key, so `CLERK_DOMAIN` is not required.

### Optional Environment Variables

| Variable        | Description           | Default                |
| --------------- | --------------------- | ---------------------- |
| `CLDCTL_BINARY` | Path to cldctl binary | Auto-detected or built |
| `TEST_TIMEOUT`  | Maximum test duration | `5m`                   |

## Running the Tests

### From Repository Root

```bash
# Set Clerk credentials
export CLERK_PUBLISHABLE_KEY="pk_test_..."
export CLERK_SECRET_KEY="sk_test_..."

# Run integration tests
make test-integration
```

### Direct Go Command

```bash
go test -tags=integration -v -timeout=10m ./testdata/integration/...
```

### Manual Deployment

To deploy manually using cldctl (name and datacenter are CLI flags):

```bash
# From repository root
cldctl update environment clerk-test \
  -d ./official-templates/local \
  ./testdata/integration/clerk-nextjs-postgres/environment.yml
```

### Validation Only (No Clerk Credentials Needed)

```bash
# Test that configuration files are valid
go test -tags=integration -v -run "Validation" ./testdata/integration/...
```

## Test Structure

### Files

```
clerk-nextjs-postgres/
├── cloud.component.yml    # Component definition
├── environment.yml        # Environment configuration
├── app/                   # Next.js application
│   ├── package.json
│   ├── Dockerfile
│   ├── src/
│   │   ├── middleware.ts  # Clerk route protection
│   │   └── app/
│   │       ├── layout.tsx # Root layout with ClerkProvider
│   │       ├── page.tsx   # Home page
│   │       └── api/
│   │           ├── health/route.ts    # Public health check
│   │           └── protected/route.ts # Auth-protected route
└── README.md
```

### Test Cases

1. **TestClerkNextJSPostgres** - Full integration test
   - Deploys environment with `cldctl env update`
   - Waits for application to be healthy
   - Tests public health endpoint
   - Tests protected endpoint returns 401 without auth
   - Tests protected endpoint authentication flow
   - Cleans up environment

2. **TestClerkNextJSPostgres_EnvironmentValidation** - File validation
   - Verifies all required files exist

3. **TestClerkNextJSPostgres_ComponentValidation** - Component validation
   - Runs `cldctl component validate`

## API Endpoints

### GET /api/health (Public)

Returns application and database health status.

**Response (200 OK):**

```json
{
  "status": "healthy",
  "database": "connected",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### GET /api/protected (Requires Authentication)

Returns user info and database query result.

**Response (401 Unauthorized):**

```json
{
  "error": "Unauthorized",
  "message": "Authentication required"
}
```

**Response (200 OK with valid Clerk session):**

```json
{
  "success": true,
  "userId": "user_abc123",
  "dbConnected": true,
  "serverTime": "2024-01-15T10:30:00Z",
  "message": "Protected route accessed successfully"
}
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    environment.yml                       │
│  - clerk component (credentials)                         │
│  - clerk-nextjs-app component                           │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│                 local datacenter                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │  PostgreSQL  │  │  Next.js App │  │ Docker       │  │
│  │  Container   │◄─┤  Container   │  │ Network      │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│                      Clerk API                           │
│  (Authentication verification)                           │
└─────────────────────────────────────────────────────────┘
```

## Troubleshooting

### Test Skipped

If you see "Skipping integration test", ensure all required environment variables are set.

### Deployment Fails

1. Check Docker is running: `docker info`
2. Check cldctl is built: `make build`
3. Review deployment logs in test output

### Health Check Fails

The application may take 1-2 minutes to build and start. The test polls every 5 seconds with a default timeout of 5 minutes.

### Database Connection Errors

Ensure the PostgreSQL container is running and the `DATABASE_URL` environment variable is correctly injected.

## Development

### Local Testing Without cldctl

```bash
cd app

# Install dependencies
pnpm install

# Set environment variables
export DATABASE_URL="postgresql://user:pass@localhost:5432/testdb"
export NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY="pk_test_..."
export CLERK_SECRET_KEY="sk_test_..."

# Run development server
pnpm dev
```

### Building the Docker Image

```bash
cd app
docker build -t clerk-nextjs-test .
```
