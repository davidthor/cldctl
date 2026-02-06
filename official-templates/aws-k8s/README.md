# AWS EKS (Kubernetes)

Deploy portable applications to Amazon EKS with managed AWS services for databases, storage, email, encryption, and observability.

## Services Used

- **Amazon EKS** - Managed Kubernetes
- **Amazon RDS** - Managed PostgreSQL and MySQL databases
- **Amazon ElastiCache** - Managed Redis
- **Amazon S3** - Object storage
- **AWS SES** - Transactional email (SMTP)
- **AWS KMS** - Symmetric encryption key management
- **AWS Secrets Manager** - Asymmetric key pair storage and secrets
- **Amazon CloudWatch** - Logs, metrics, and Container Insights
- **AWS X-Ray** - Distributed tracing
- **AWS ALB Ingress Controller** - Kubernetes-native load balancing
- **Amazon Route53** - DNS management
- **Amazon ECR** - Container image registry
- **Knative Serving** - Serverless functions on Kubernetes
- **Amazon EC2** - VM-based deployments (runtime workloads)

## Variables

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `aws_region` | string | `us-east-1` | AWS region |
| `vpc_id` | string | *(required)* | VPC ID for deployment |
| `cluster_name` | string | *(required)* | EKS cluster name |
| `domain` | string | `app.example.com` | Base domain for environments |
| `hosted_zone_id` | string | *(required)* | Route53 hosted zone ID |
| `certificate_arn` | string | *(required)* | ACM certificate ARN for TLS |
| `ses_identity_arn` | string | `""` | AWS SES verified identity ARN |
| `ses_smtp_username` | string | `""` | AWS SES SMTP username |
| `ses_smtp_password` | string | `""` | AWS SES SMTP password (sensitive) |
| `ssh_key_pair` | string | `""` | EC2 key pair for VM deployments |

## Hooks Supported

| Hook | Implementation |
|------|---------------|
| database | Amazon RDS (PostgreSQL, MySQL) |
| task | Kubernetes Jobs |
| bucket | Amazon S3 |
| encryptionKey (RSA/ECDSA) | AWS Secrets Manager (asymmetric) |
| encryptionKey (symmetric) | AWS KMS (symmetric) |
| smtp | AWS SES (SMTP interface) |
| deployment (container) | Kubernetes Deployments on EKS |
| deployment (VM/runtime) | EC2 instances via OpenTofu |
| function | Knative Serving on EKS |
| service | Kubernetes Services |
| ingress | AWS ALB Ingress Controller |
| cronjob | Kubernetes CronJobs |
| databaseUser | RDS user management |
| secret | AWS Secrets Manager |
| dockerBuild | Amazon ECR (build & push) |
| observability | OTel Collector DaemonSet -> CloudWatch + X-Ray |

## Quick Start

```bash
# Build and push the datacenter
arcctl dc build . -t ghcr.io/myorg/aws-k8s:v1
arcctl dc push ghcr.io/myorg/aws-k8s:v1

# Deploy the datacenter
arcctl dc deploy aws-k8s-prod \
  --config ghcr.io/myorg/aws-k8s:v1 \
  --var cluster_name=prod-eks \
  --var vpc_id=vpc-12345 \
  --var domain=app.example.com \
  --var hosted_zone_id=Z123456 \
  --var certificate_arn=arn:aws:acm:us-east-1:123456:certificate/abc

# Create an environment and deploy a component
arcctl env create staging --datacenter aws-k8s-prod
arcctl deploy ghcr.io/myorg/my-app:v1 -e staging
```

## Prerequisites

- An existing EKS cluster with the [AWS Load Balancer Controller](https://docs.aws.amazon.com/eks/latest/userguide/aws-load-balancer-controller.html) installed
- [Knative Serving](https://knative.dev/docs/install/) installed on the cluster for serverless functions
- VPC with private subnets for RDS and ElastiCache
- ACM certificate for your domain
- Route53 hosted zone for DNS
