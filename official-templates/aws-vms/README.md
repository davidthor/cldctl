# AWS EC2 (VMs)

VM-based AWS template. All deployments run on EC2 instances. Container-based deployments install Docker on EC2 and run containers. Runtime-based deployments install the language runtime directly. Functions deploy as long-running processes behind ALB.

## Services Used

- **Amazon EC2** - Virtual machines for all compute
- **Amazon RDS** - Managed PostgreSQL and MySQL databases
- **Amazon ElastiCache** - Managed Redis
- **Amazon S3** - Object storage
- **AWS SES** - Transactional email (SMTP)
- **AWS KMS** - Symmetric encryption key management
- **AWS Secrets Manager** - Asymmetric key pair storage and secrets
- **Amazon CloudWatch** - Logs, metrics, and CloudWatch agent
- **Application Load Balancer** - HTTP/HTTPS routing
- **Amazon Route53** - DNS management
- **Amazon ECR** - Container image registry

## Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `aws_region` | string | `us-east-1` | AWS region |
| `vpc_id` | string | *(required)* | VPC ID for deployment |
| `domain` | string | `app.example.com` | Base domain for environments |
| `hosted_zone_id` | string | *(required)* | Route53 hosted zone ID |
| `certificate_arn` | string | *(required)* | ACM certificate ARN for TLS |
| `ssh_key_pair` | string | *(required)* | EC2 key pair name for SSH access |
| `default_instance_type` | string | `t3.small` | Default EC2 instance type |
| `ses_identity_arn` | string | `""` | AWS SES verified identity ARN |
| `ses_smtp_username` | string | `""` | AWS SES SMTP username |
| `ses_smtp_password` | string | `""` | AWS SES SMTP password (sensitive) |

## Hooks Supported

| Hook | Implementation |
|------|---------------|
| database | Amazon RDS (PostgreSQL, MySQL) |
| task | EC2 one-time tasks via SSM Run Command |
| bucket | Amazon S3 |
| encryptionKey (RSA/ECDSA) | AWS Secrets Manager (asymmetric) |
| encryptionKey (symmetric) | AWS KMS (symmetric) |
| smtp | AWS SES (SMTP interface) |
| deployment (container) | EC2 + Docker (install Docker, run container) |
| deployment (VM/runtime) | EC2 + language runtime (install runtime directly) |
| function | EC2 long-running process behind ALB |
| service | ALB target group routing |
| ingress | ALB listener rules |
| cronjob | Cron on dedicated EC2 instances |
| databaseUser | RDS user management |
| secret | AWS Secrets Manager |
| dockerBuild | Amazon ECR (build & push) |
| observability | CloudWatch agent on EC2 |

## Quick Start

```bash
# Build and push the datacenter
arcctl dc build . -t ghcr.io/myorg/aws-vms:v1
arcctl dc push ghcr.io/myorg/aws-vms:v1

# Deploy the datacenter
arcctl dc deploy aws-vms-prod \
  --config ghcr.io/myorg/aws-vms:v1 \
  --var vpc_id=vpc-12345 \
  --var domain=app.example.com \
  --var hosted_zone_id=Z123456 \
  --var certificate_arn=arn:aws:acm:us-east-1:123456:certificate/abc \
  --var ssh_key_pair=my-key-pair

# Create an environment and deploy a component
arcctl env create staging --datacenter aws-vms-prod
arcctl deploy ghcr.io/myorg/my-app:v1 -e staging
```

## How It Works

### Container-Based Deployments

When a component has a Docker image (`node.inputs.image != null`):
1. An EC2 instance is provisioned via OpenTofu
2. Docker is installed via user data script
3. The container image is pulled from ECR
4. The container runs as a systemd service
5. The instance registers with the ALB target group

### Runtime-Based Deployments

When a component has a runtime (`node.inputs.runtime != null`):
1. An EC2 instance is provisioned via OpenTofu
2. The specified language runtime is installed (e.g., Node.js 20, Python 3.12)
3. System packages are installed if specified
4. Setup commands are executed
5. The application runs as a systemd service

### Functions on EC2

Functions are deployed as long-running HTTP server processes on EC2 instances:
1. An EC2 instance is provisioned
2. The function handler runs as a persistent process
3. The ALB routes traffic to the instance
4. Auto Scaling Groups can scale instances based on load
