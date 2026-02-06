# AWS ECS Fargate

Deploy portable applications to AWS ECS Fargate with managed AWS services for databases, storage, email, encryption, and observability.

## Services Used

- **AWS ECS Fargate** - Serverless container orchestration
- **Amazon RDS** - Managed PostgreSQL and MySQL databases
- **Amazon ElastiCache** - Managed Redis
- **Amazon S3** - Object storage
- **AWS Lambda** - Serverless functions
- **AWS SES** - Transactional email (SMTP)
- **AWS KMS** - Symmetric encryption key management
- **AWS Secrets Manager** - Asymmetric key pair storage
- **Amazon CloudWatch** - Logs, metrics, and dashboards
- **AWS X-Ray** - Distributed tracing
- **Application Load Balancer** - HTTP/HTTPS routing
- **Amazon Route53** - DNS management
- **Amazon ECR** - Container image registry
- **Amazon EventBridge** - Scheduled tasks (cronjobs)

## Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `aws_region` | string | `us-east-1` | AWS region |
| `vpc_id` | string | *(required)* | VPC ID for deployment |
| `cluster_name` | string | *(required)* | ECS cluster name |
| `domain` | string | `app.example.com` | Base domain for environments |
| `hosted_zone_id` | string | *(required)* | Route53 hosted zone ID |
| `certificate_arn` | string | *(required)* | ACM certificate ARN for TLS |
| `ses_identity_arn` | string | `""` | AWS SES verified identity ARN |
| `ses_smtp_username` | string | `""` | AWS SES SMTP username (IAM access key) |
| `ses_smtp_password` | string | `""` | AWS SES SMTP password (IAM secret, sensitive) |

## Hooks Supported

| Hook | Implementation |
|------|---------------|
| database | Amazon RDS (PostgreSQL, MySQL) |
| task | ECS Fargate one-time tasks (FARGATE_SPOT) |
| bucket | Amazon S3 |
| encryptionKey (RSA/ECDSA) | AWS Secrets Manager (asymmetric key pairs) |
| encryptionKey (symmetric) | AWS KMS (symmetric keys) |
| smtp | AWS SES (SMTP interface) |
| deployment (container) | ECS Fargate services |
| deployment (VM/runtime) | EC2 instances via OpenTofu |
| function | AWS Lambda |
| service | AWS Cloud Map (service discovery) |
| ingress | ALB listener rules |
| cronjob | Amazon EventBridge scheduled ECS tasks |
| databaseUser | RDS user management |
| secret | AWS Secrets Manager |
| dockerBuild | Amazon ECR (build & push) |
| observability | OTel Collector on ECS -> CloudWatch + X-Ray |

## Quick Start

```bash
# Build and push the datacenter
arcctl dc build . -t ghcr.io/myorg/aws-ecs:v1
arcctl dc push ghcr.io/myorg/aws-ecs:v1

# Deploy the datacenter
arcctl dc deploy aws-prod \
  --config ghcr.io/myorg/aws-ecs:v1 \
  --var cluster_name=prod-cluster \
  --var vpc_id=vpc-12345 \
  --var domain=app.example.com \
  --var hosted_zone_id=Z123456 \
  --var certificate_arn=arn:aws:acm:us-east-1:123456:certificate/abc

# Create an environment and deploy a component
arcctl env create staging --datacenter aws-prod
arcctl deploy ghcr.io/myorg/my-app:v1 -e staging
```
