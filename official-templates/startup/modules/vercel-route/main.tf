terraform {
  required_providers {
    vercel = {
      source  = "vercel/vercel"
      version = "~> 2.0"
    }
  }
}

provider "vercel" {
  api_token = var.token
  team      = var.team_id != "" ? var.team_id : null
}

# Create a project domain alias for external routing.
# Vercel handles TLS termination, CDN, and edge routing automatically.
resource "vercel_project_domain" "this" {
  count = var.alias != "" ? 1 : 0

  project_id = var.project_id
  domain     = var.alias
}

locals {
  # Use the alias domain if provided, otherwise use the Vercel-generated domain
  host = var.alias != "" ? var.alias : "${var.project_id}.vercel.app"
  url  = "https://${local.host}"
}
