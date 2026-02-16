terraform {
  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.0"
    }
  }
}

locals {
  name = replace(try(var.name, "ingress"), "/[^a-zA-Z0-9-]/", "-")
}

resource "kubernetes_ingress_v1" "this" {
  metadata {
    name      = local.name
    namespace = var.namespace

    annotations = {
      "kubernetes.io/ingress.class"               = "alb"
      "alb.ingress.kubernetes.io/scheme"          = "internet-facing"
      "alb.ingress.kubernetes.io/target-type"     = "ip"
      "alb.ingress.kubernetes.io/listen-ports"    = "[{\"HTTPS\":443}]"
      "alb.ingress.kubernetes.io/certificate-arn" = var.certificate_arn
      "alb.ingress.kubernetes.io/ssl-policy"      = "ELBSecurityPolicy-TLS13-1-2-2021-06"
    }

    labels = {
      "app.kubernetes.io/managed-by" = "cldctl"
    }
  }

  spec {
    rule {
      host = var.domain

      http {
        path {
          path      = "/"
          path_type = "Prefix"

          backend {
            service {
              name = var.target
              port {
                number = try(var.port, 80)
              }
            }
          }
        }
      }
    }

    tls {
      hosts = [var.domain]
    }
  }
}
