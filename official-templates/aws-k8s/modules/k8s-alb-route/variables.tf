variable "namespace" {
  description = "Kubernetes namespace"
  type        = string
}

variable "kubeconfig" {
  description = "Kubeconfig for cluster access"
  type        = string
  sensitive   = true
}

variable "domain" {
  description = "Domain name for the ingress"
  type        = string
}

variable "certificate_arn" {
  description = "ACM certificate ARN"
  type        = string
}

variable "name" {
  description = "Ingress name"
  type        = string
  default     = "ingress"
}

variable "target" {
  description = "Target service name"
  type        = string
  default     = ""
}

variable "target_type" {
  description = "Target type"
  type        = string
  default     = "service"
}

variable "port" {
  description = "Service port"
  type        = number
  default     = 80
}
