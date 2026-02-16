output "url" {
  description = "Ingress URL"
  value       = "https://${var.domain}"
}

output "hosts" {
  description = "Ingress hosts"
  value       = [var.domain]
}

output "host" {
  description = "Ingress host"
  value       = var.domain
}

output "port" {
  description = "Ingress port"
  value       = 443
}
