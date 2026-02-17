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

### With OAuth Authentication

```yaml
# environment.yml
name: production
datacenter: my-datacenter

components:
  formbricks:
    source: examples/components/formbricks
    variables:
      # Google OAuth
      google_client_id: "your-google-client-id"
      google_client_secret: "your-google-client-secret"
      # GitHub OAuth
      github_id: "your-github-client-id"
      github_secret: "your-github-client-secret"
```

### Production Configuration

```yaml
# environment.yml
name: production
datacenter: aws-ecs

components:
  formbricks:
    source: examples/components/formbricks
    variables:
      log_level: "warn"
      audit_log_enabled: "1"
      privacy_url: "https://example.com/privacy"
      terms_url: "https://example.com/terms"
      default_brand_color: "#4f46e5"
```

## Configuration

### Dynamic URL Injection

The application URLs (`WEBAPP_URL`, `NEXTAUTH_URL`, `PUBLIC_URL`) are automatically injected from the declared route using `${{ routes.public.url }}`. This means the correct URL is determined at deployment time based on your datacenter's route configuration.

### Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `log_level` | Log level (debug, info, warn, error, fatal) | `info` |
| `default_brand_color` | Default survey brand color (hex) | `#64748b` |
| `session_max_age` | Session expiration in seconds | `86400` (24 hours) |

### Feature Flags

| Variable | Description | Default |
|----------|-------------|---------|
| `email_auth_disabled` | Disable email/password auth | `0` |
| `email_verification_disabled` | Disable email verification | `0` |
| `password_reset_disabled` | Disable password reset | `0` |
| `invite_disabled` | Disable user invitations | `0` |
| `rate_limiting_disabled` | Disable rate limiting | `0` |
| `audit_log_enabled` | Enable audit logging | `0` |

### OAuth Configuration

| Variable | Description | Required |
|----------|-------------|----------|
| `google_client_id` | Google OAuth client ID | No |
| `google_client_secret` | Google OAuth client secret | No |
| `github_id` | GitHub OAuth client ID | No |
| `github_secret` | GitHub OAuth client secret | No |

### Legal Pages

| Variable | Description |
|----------|-------------|
| `privacy_url` | Link to privacy policy |
| `terms_url` | Link to terms of service |
| `imprint_url` | Link to legal imprint |

## Resources Provisioned

### Databases

- **PostgreSQL 15**: Primary database for surveys, responses, users, and configuration
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

1. Open the configured `webapp_url` in your browser
2. Complete the setup wizard to create your first admin account
3. Start creating surveys!

## Upgrading

To upgrade Formbricks, update the image tag in `cld.yml`:

```yaml
deployments:
  app:
    image: ghcr.io/formbricks/formbricks:v3.6.1  # Update version here
```

Check the [migration guide](https://formbricks.com/docs/self-hosting/advanced/migration) for version-specific upgrade steps.

## Troubleshooting

### Common Issues

1. **Container fails to start**: Ensure PostgreSQL and Redis are healthy before the app starts
2. **File uploads not working**: Verify S3/MinIO bucket permissions and endpoint URL
3. **Emails not sending**: Check SMTP configuration and credentials
4. **OAuth not working**: Verify callback URLs match your `webapp_url`

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
