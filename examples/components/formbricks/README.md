# Formbricks Component

This component deploys [Formbricks](https://formbricks.com/), an open-source Experience Management platform and Qualtrics/Typeform alternative for collecting user feedback through surveys.

## Features

- **In-app Surveys**: Trigger surveys based on user behavior and actions
- **Link Surveys**: Share surveys via links for broad distribution
- **Website Surveys**: Embed surveys on any website
- **User Targeting**: Target specific user segments with precision
- **No-code Builder**: Create surveys without writing code
- **Integrations**: Connect with Slack, Notion, Airtable, and more

## Architecture

This component deploys:

| Service | Description |
|---------|-------------|
| **app** | Main Formbricks application (Next.js) |
| **postgres** | PostgreSQL database for persistent storage |
| **redis** | Redis for caching, rate limiting, and audit logs |
| **uploads** | S3-compatible bucket for file uploads |
| **smtp** | Email service for notifications and invitations |

## Quick Start

### Basic Deployment

```yaml
# environment.yml
name: production
datacenter: my-datacenter

components:
  formbricks:
    source: examples/components/formbricks
```

The public URL is automatically injected from the declared route - no manual URL configuration needed.

### With Custom Email Settings

```yaml
# environment.yml
name: production
datacenter: my-datacenter

components:
  formbricks:
    source: examples/components/formbricks
    variables:
      mail_from: "noreply@example.com"
      mail_from_name: "My Company Surveys"
      email_verification_disabled: "0"
      password_reset_disabled: "0"
```

### Adding Optional Features via Environment Overrides

Formbricks supports many optional features (OAuth, legal pages, branding, audit logging, etc.) that can be enabled by passing additional environment variables directly to the deployment. See the [Environment Variables Reference](https://formbricks.com/docs/self-hosting/configuration/environment-variables) for the full list.

```yaml
# environment.yml - example with OAuth and legal pages
components:
  formbricks:
    source: examples/components/formbricks
    environment:
      GOOGLE_CLIENT_ID: "your-google-client-id"
      GOOGLE_CLIENT_SECRET: "your-google-client-secret"
      GITHUB_ID: "your-github-client-id"
      GITHUB_SECRET: "your-github-client-secret"
      PRIVACY_URL: "https://example.com/privacy"
      TERMS_URL: "https://example.com/terms"
      DEFAULT_BRAND_COLOR: "#4f46e5"
      AUDIT_LOG_ENABLED: "1"
```

## Configuration

### Dynamic URL Injection

The application URLs (`WEBAPP_URL`, `NEXTAUTH_URL`) are automatically injected from the declared route using `${{ routes.public.url }}`. This means the correct URL is determined at deployment time based on your datacenter's route configuration.

### Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `log_level` | Log level (debug, info, warn, error, fatal) | `info` |
| `mail_from` | Email address for outgoing emails | `noreply@formbricks.local` |
| `mail_from_name` | Display name for outgoing emails | `Formbricks` |
| `email_verification_disabled` | Disable email verification (requires SMTP) | `1` |
| `password_reset_disabled` | Disable password reset (requires SMTP) | `1` |

### Optional Environment Variables

These can be passed via environment file overrides (not set by default to avoid Zod validation errors from empty strings):

| Variable | Description |
|----------|-------------|
| `EMAIL_AUTH_DISABLED` | Disable email/password auth (set to `1`) |
| `INVITE_DISABLED` | Disable user invitations (set to `1`) |
| `RATE_LIMITING_DISABLED` | Disable rate limiting (set to `1`) |
| `DEFAULT_BRAND_COLOR` | Default survey brand color (hex, e.g. `#64748b`) |
| `SESSION_MAX_AGE` | Session expiration in seconds (default `86400`) |
| `AUDIT_LOG_ENABLED` | Enable audit logging (set to `1`, requires Redis) |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | Google OAuth for SSO |
| `GITHUB_ID` / `GITHUB_SECRET` | GitHub OAuth for SSO |
| `PRIVACY_URL` | Link to privacy policy |
| `TERMS_URL` | Link to terms of service |
| `IMPRINT_URL` | Link to legal imprint |
| `IMPRINT_ADDRESS` | Address for legal imprint |

## Resources Provisioned

### Databases

- **PostgreSQL 15**: Primary database for surveys, responses, users, and configuration (requires pgvector extension)
- **Redis 7**: Caching, rate limiting, and audit log storage

### Storage

- **S3 Bucket**: File uploads (survey images, response attachments)

### Security

The following encryption keys are automatically generated:

- `nextauth-secret`: Session signing and encryption
- `encryption-key`: Data encryption and audit log hashing
- `cron-secret`: Securing internal cron job endpoints

## Exposed Services

| Service | Port | Protocol | Description |
|---------|------|----------|-------------|
| `app` | 3000 | HTTP | Main application |

## Routes

| Route | Path | Service |
|-------|------|---------|
| `public` | `/` | app:3000 |

## System Requirements

Per the [official documentation](https://formbricks.com/docs/self-hosting/overview):

- **Minimum**: 1 vCPU, 2 GB RAM, 8 GB SSD
- **Recommended**: 2 vCPU, 4 GB RAM, 20 GB SSD

## Accessing Formbricks

After deployment:

1. Open the route URL in your browser
2. Complete the setup wizard to create your first admin account
3. Start creating surveys!

## Upgrading

To upgrade Formbricks, update the image tag in `cld.yml`:

```yaml
deployments:
  app:
    image: ghcr.io/formbricks/formbricks:4.6.1  # Update version here
```

Check the [migration guide](https://formbricks.com/docs/self-hosting/advanced/migration) for version-specific upgrade steps.

## Troubleshooting

### Common Issues

1. **Container fails to start**: Ensure PostgreSQL and Redis are healthy before the app starts
2. **File uploads not working**: Verify S3/MinIO bucket permissions and endpoint URL
3. **Emails not sending**: Check SMTP configuration and credentials
4. **OAuth not working**: Verify callback URLs match your route URL

### Health Check

The application exposes a `/health` endpoint for monitoring:

```bash
curl https://your-formbricks-url/health
```

## Links

- [Formbricks Documentation](https://formbricks.com/docs)
- [Self-Hosting Guide](https://formbricks.com/docs/self-hosting/overview)
- [Environment Variables Reference](https://formbricks.com/docs/self-hosting/configuration/environment-variables)
- [GitHub Repository](https://github.com/formbricks/formbricks)
- [GitHub Discussions](https://github.com/formbricks/formbricks/discussions)
