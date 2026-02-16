output "url" {
  description = "Ingress URL"
  value       = local.url
}

output "hosts" {
  description = "List of hosts served by this ingress"
  value       = [local.host]
}

output "host" {
  description = "Primary host"
  value       = local.host
}

output "port" {
  description = "Ingress port (always 443 for Vercel)"
  value       = 443
}
