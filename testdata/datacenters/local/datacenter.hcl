variable "network_name" {
  type        = string
  description = "Name of the Docker network"
  default     = "arcctl-local"
}

module "network" {
  plugin = "native"
  build  = "./modules/docker-network"
  inputs = {
    name = variable.network_name
  }
}

environment {
  module "namespace" {
    plugin = "native"
    build  = "./modules/docker-network"
    inputs = {
      name = "${environment.name}-network"
    }
  }

  database {
    when = element(split(":", node.inputs.type), 0) == "postgres"

    module "postgres" {
      plugin = "native"
      build  = "./modules/docker-postgres"
      inputs = {
        name     = "${environment.name}-${node.component}--${node.name}"
        version  = try(element(split(":", node.inputs.type), 1), null)
        database = node.name
        network  = module.namespace.network_id
      }
    }

    outputs = {
      host     = module.postgres.host
      port     = module.postgres.port
      database = module.postgres.database
      username = module.postgres.username
      password = module.postgres.password
      url      = module.postgres.url
    }
  }

  database {
    when = element(split(":", node.inputs.type), 0) == "redis"

    module "redis" {
      plugin = "native"
      build  = "./modules/docker-redis"
      inputs = {
        name    = "${environment.name}-${node.component}--${node.name}"
        network = module.namespace.network_id
      }
    }

    outputs = {
      host = module.redis.host
      port = module.redis.port
      url  = module.redis.url
    }
  }

  deployment {
    module "container" {
      plugin = "native"
      build  = "./modules/docker-deployment"
      inputs = {
        name        = "${environment.name}-${node.component}--${node.name}"
        image       = node.inputs.image
        command     = node.inputs.command
        entrypoint  = node.inputs.entrypoint
        environment = node.inputs.environment
        ports       = node.inputs.ports
        network     = module.namespace.network_id
        replicas    = node.inputs.replicas
      }
    }

    outputs = {
      id = module.container.container_id
    }
  }

  service {
    module "service" {
      plugin = "native"
      build  = "./modules/docker-service"
      inputs = {
        name       = "${environment.name}-${node.component}--${node.name}"
        target     = node.inputs.target
        port       = node.inputs.port
        network    = module.namespace.network_id
      }
    }

    outputs = {
      host = module.service.host
      port = module.service.port
      url  = module.service.url
    }
  }

  ingress {
    module "ingress" {
      plugin = "native"
      build  = "./modules/docker-ingress"
      inputs = {
        name    = "${environment.name}-${node.component}--${node.name}"
        rules   = node.inputs.rules
        network = module.namespace.network_id
      }
    }

    outputs = {
      url   = module.ingress.url
      hosts = module.ingress.hosts
    }
  }

  dockerBuild {
    module "build" {
      plugin = "native"
      build  = "./modules/docker-build"
      inputs = {
        context    = node.inputs.context
        dockerfile = node.inputs.dockerfile
        tag        = node.inputs.tag
        args       = node.inputs.args
      }
      volumes = [
        {
          host_path  = "/var/run/docker.sock"
          mount_path = "/var/run/docker.sock"
          read_only  = false
        }
      ]
    }

    outputs = {
      image = module.build.image
    }
  }
}
