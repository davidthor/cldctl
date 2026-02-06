# AWS Lambda (Serverless)

Serverless-first AWS template. All compute runs on AWS Lambda with API Gateway for routing, RDS for databases, S3 for storage, and SES for email.

## Services Used

- **AWS Lambda** - Serverless functions and container-based deployments
- **Amazon API Gateway** - HTTP API routing
- **Amazon CloudFront** - CDN and edge caching
- **Amazon RDS** - Managed PostgreSQL and MySQL databases
- **Amazon ElastiCache** - Managed Redis
- **Amazon S3** - Object storage
- **AWS SES** - Transactional email (SMTP)
- **AWS KMS** - Symmetric encryption key management
- **AWS Secrets Manager** - Asymmetric key pair storage and secrets
- **Amazon CloudWatch** - Logs, metrics, Lambda Insights
- **Amazon EventBridge** - Scheduled Lambda invocations (cronjobs)
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
| `ses_identity_arn` | string | `""` | AWS SES verified identity ARN |
| `ses_smtp_username` | string | `""` | AWS SES SMTP username |
| `ses_smtp_password` | string | `""` | AWS SES SMTP password (sensitive) |

## Hooks Supported

| Hook | Implementation |
|------|---------------|
| database | Amazon RDS (PostgreSQL, MySQL) |
| task | Lambda one-time invocations |
| bucket | Amazon S3 |
| encryptionKey (RSA/ECDSA) | AWS Secrets Manager (asymmetric) |
| encryptionKey (symmetric) | AWS KMS (symmetric) |
| smtp | AWS SES (SMTP interface) |
| deployment (container) | Lambda container images + API Gateway |
| deployment (VM/runtime) | **Not supported** (serverless only) |
| function | AWS Lambda |
| service | API Gateway internal integrations |
| ingress | API Gateway routes + CloudFront |
| cronjob | EventBridge scheduled Lambda |
| databaseUser | RDS user management |
| secret | AWS Secrets Manager |
| dockerBuild | Amazon ECR (build & push) |
| observability | CloudWatch Lambda Insights |

## Quick Start

```bash
# Build and push the datacenter
arcctl dc build . -t ghcr.io/myorg/aws-lambda:v1
arcctl dc push ghcr.io/myorg/aws-lambda:v1

# Deploy the datacenter
arcctl dc deploy aws-serverless \
  --config ghcr.io/myorg/aws-lambda:v1 \
  --var vpc_id=vpc-12345 \
  --var domain=app.example.com \
  --var hosted_zone_id=Z123456 \
  --var certificate_arn=arn:aws:acm:us-east-1:123456:certificate/abc

# Create an environment and deploy a component
arcctl env create staging --datacenter aws-serverless
arcctl deploy ghcr.io/myorg/my-app:v1 -e staging
```

## Limitations

- **No VM/runtime deployments**: This template is serverless-only. Use `aws-ecs` or `aws-vms` for workloads requiring VM-based deployments.
- **Lambda timeout**: Maximum execution time is 15 minutes per invocation.
- **Lambda container size**: Container images must be under 10 GB.
- **Cold starts**: Lambda functions may experience cold start latency.
