variable "name" {
  description = "Ingress name"
  type        = string
}

variable "token" {
  description = "Vercel API token"
  type        = string
  sensitive   = true
}

variable "team_id" {
  description = "Vercel team ID (optional for personal accounts)"
  type        = string
  default     = ""
}

variable "project_id" {
  description = "Vercel project ID"
  type        = string
}

variable "rules" {
  description = "Routing rules"
  type        = any
  default     = null
}

variable "alias" {
  description = "Custom domain alias (e.g., app.example.com or staging.app.example.com)"
  type        = string
  default     = ""
}
