# n8n Component

This component deploys [n8n](https://n8n.io/), an open-source workflow automation tool that helps you connect apps and services to automate tasks without writing code.

## Features

- **400+ Integrations**: Connect to popular apps like Slack, Google Sheets, GitHub, and more
- **Visual Workflow Builder**: Create workflows with a drag-and-drop interface
- **Code When Needed**: Use JavaScript or Python for custom logic
- **Self-Hosted**: Full control over your data and infrastructure
- **Webhook Support**: Trigger workflows from external services
- **Scheduled Workflows**: Run automations on a schedule
- **AI Capabilities**: Built-in AI nodes for LLMs, embeddings, and agents

## Architecture

This component deploys:

| Service | Description |
|---------|-------------|
| **app** | Main n8n application (Node.js) |
| **postgres** | PostgreSQL database for workflows and credentials |

## Quick Start

### Basic Deployment

```yaml
# environment.yml
name: production
datacenter: my-datacenter

components:
  n8n:
    source: examples/components/n8n
```

### With Custom Timezone

```yaml
# environment.yml
name: production
datacenter: my-datacenter

components:
  n8n:
    source: examples/components/n8n
    variables:
      timezone: "America/New_York"
```

### Production Configuration

```yaml
# environment.yml
name: production
datacenter: aws-ecs

components:
  n8n:
    source: examples/components/n8n
    variables:
      timezone: "UTC"
      log_level: "warn"
      executions_data_max_age: "168"  # 7 days
      secure_cookie: "true"
      diagnostics_enabled: "false"
```

## Configuration

### Dynamic URL Injection

The application URLs (`N8N_EDITOR_BASE_URL`) are automatically injected from the declared route using `${{ routes.public.url }}`. This means the correct URL is determined at deployment time based on your datacenter's route configuration.

### Core Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `timezone` | Timezone (e.g., America/New_York) | `UTC` |
| `webhook_url` | Public URL for webhooks | Auto from route |
| `log_level` | Log level (debug, info, warn, error) | `info` |
| `secure_cookie` | Use secure cookies (HTTPS) | `true` |

### Execution Settings

| Variable | Description | Default |
|----------|-------------|---------|
| `executions_data_prune` | Enable auto-pruning of old executions | `true` |
| `executions_data_max_age` | Max age of executions in hours | `336` (14 days) |
| `executions_data_save_on_error` | Save execution data on error | `all` |
| `executions_data_save_on_success` | Save execution data on success | `all` |
| `executions_data_save_manual` | Save manual execution data | `true` |

### Feature Toggles

| Variable | Description | Default |
|----------|-------------|---------|
| `user_management_disabled` | Disable user management (single user) | `false` |
| `templates_enabled` | Enable workflow templates | `true` |
| `diagnostics_enabled` | Enable telemetry | `true` |
| `version_notifications_enabled` | Enable version notifications | `true` |

### Workflow Settings

| Variable | Description | Default |
|----------|-------------|---------|
| `workflow_caller_policy_default` | Default workflow caller policy | `workflowsFromSameOwner` |

## Resources Provisioned

### Databases

- **PostgreSQL 15**: Primary database for workflows, credentials, and execution data

### Security

The following encryption key is automatically generated:

- `encryption-key`: Encrypts credentials and sensitive workflow data

## Exposed Services

| Service | Port | Protocol | Description |
|---------|------|----------|-------------|
| `app` | 5678 | HTTP | Main n8n application |

## Routes

| Route | Path | Service | Description |
|-------|------|---------|-------------|
| `public` | `/` | app:5678 | Editor UI |
| `public` | `/webhook/*` | app:5678 | Production webhooks |
| `public` | `/webhook-test/*` | app:5678 | Test webhooks |
| `public` | `/api/*` | app:5678 | REST API |
| `public` | `/healthz` | app:5678 | Health check |

## System Requirements

Per the [official documentation](https://docs.n8n.io/hosting/scaling/performance-benchmarking/):

- **Minimum**: 1 vCPU, 2 GB RAM
- **Recommended**: 2 vCPU, 4 GB RAM (for production workloads)

## Accessing n8n

After deployment:

1. Open the configured URL in your browser
2. Create your first admin account (owner)
3. Start building workflows!

## Upgrading

To upgrade n8n, update the image tag in `cld.yml`:

```yaml
deployments:
  app:
    image: docker.n8n.io/n8nio/n8n:2.4.7  # Update version here
```

Check the [release notes](https://docs.n8n.io/release-notes/) for version-specific changes.

## Queue Mode (Scaling)

For high-volume production deployments, n8n supports queue mode with Redis. To enable queue mode, you would need to:

1. Add a Redis database
2. Configure worker processes
3. Set up proper scaling

See the [scaling documentation](https://docs.n8n.io/hosting/scaling/queue-mode/) for details.

## Troubleshooting

### Common Issues

1. **Webhooks not working**: Ensure `webhook_url` is set to your public URL, or verify the route URL is accessible
2. **Workflows not executing**: Check PostgreSQL connectivity and logs
3. **Credentials not saving**: Verify the encryption key is properly configured
4. **Slow performance**: Consider increasing CPU/memory or enabling queue mode

### Health Check

The application exposes a `/healthz` endpoint for monitoring:

```bash
curl https://your-n8n-url/healthz
```

### View Logs

Check the deployment logs for errors:

```bash
cldctl logs n8n --deployment app
```

## Links

- [n8n Documentation](https://docs.n8n.io/)
- [Docker Installation Guide](https://docs.n8n.io/hosting/installation/docker/)
- [Environment Variables Reference](https://docs.n8n.io/hosting/configuration/environment-variables/)
- [GitHub Repository](https://github.com/n8n-io/n8n)
- [Community Forum](https://community.n8n.io/)
- [Workflow Templates](https://n8n.io/workflows/)
