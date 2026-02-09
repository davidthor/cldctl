# schema

Schema parsing, validation, and transformation for cldctl configuration files. Supports components, datacenters, and environments with versioned schemas.

## Overview

The `schema` package provides:

- Parsing configuration files (YAML for components/environments, HCL for datacenters)
- Version detection and handling
- Validation with detailed error reporting
- Transformation to canonical internal representations
- Expression support for dynamic values

## Package Structure

```
schema/
├── component/     # Component configuration (YAML)
│   ├── internal/  # Internal types
│   └── v1/        # V1 schema implementation
├── datacenter/    # Datacenter configuration (HCL)
│   ├── internal/  # Internal types
│   └── v1/        # V1 schema implementation
└── environment/   # Environment configuration (YAML)
    ├── internal/  # Internal types
    └── v1/        # V1 schema implementation
```

## Common Patterns

All schema types follow the same architecture:

1. **Version detection**: Automatic detection with fallback to v1
2. **Three-stage pipeline**: Parse → Validate → Transform
3. **Internal representation**: Version-specific schemas transform to canonical internal types
4. **Wrapper pattern**: Internal types wrapped by public interfaces

## component

Parses and validates component configurations that define application resources.

### Usage

```go
import "github.com/davidthor/cldctl/pkg/schema/component"

// Create a loader
loader := component.NewLoader()

// Load a component
comp, err := loader.Load("./component.yml")
if err != nil {
    log.Fatal(err)
}

// Access component data
fmt.Println("Name:", comp.Name())
fmt.Println("Databases:", len(comp.Databases()))
fmt.Println("Services:", len(comp.Services()))

// Validate without loading
err = loader.Validate("./component.yml")
```

### Component Interface

```go
type Component interface {
    Name() string
    Description() string
    Databases() []Database
    Buckets() []Bucket
    Deployments() []Deployment
    Functions() []Function
    Services() []Service
    Routes() []Route
    Cronjobs() []Cronjob
    Variables() []Variable
    Dependencies() []Dependency
    SchemaVersion() string
    SourcePath() string
    ToYAML() ([]byte, error)
    ToJSON() ([]byte, error)
    Internal() *internal.InternalComponent
}
```

### Resource Interfaces

- `Database` - Database resources
- `Bucket` - Storage bucket resources
- `Port` - Dynamic port allocations (opt-in, referenced via `${{ ports.<name>.port }}`)
- `Deployment` - Container deployments
- `Function` - Serverless functions
- `Service` - Internal service endpoints (port supports int or expression string)
- `Route` - HTTP routing rules
- `Cronjob` - Scheduled jobs
- `Variable` - Input variables
- `Dependency` - Component dependencies

### Expression Support

Components support `${{ }}` expressions for dynamic values:

```yaml
name: my-api

databases:
  main:
    type: postgres

deployments:
  api:
    image: myapp:latest
    environment:
      DATABASE_URL: ${{ databases.main.url }}
      API_KEY: ${{ variables.api_key }}
```

## datacenter

Parses and validates datacenter configurations that define infrastructure modules and hooks.

### Usage

```go
import "github.com/davidthor/cldctl/pkg/schema/datacenter"

// Create a loader
loader := datacenter.NewLoader()

// Load a datacenter
dc, err := loader.Load("./datacenter.hcl")
if err != nil {
    log.Fatal(err)
}

// Access datacenter data
fmt.Println("Variables:", len(dc.Variables()))
fmt.Println("Modules:", len(dc.Modules()))
```

### Datacenter Interface

```go
type Datacenter interface {
    Variables() []Variable
    Modules() []Module
    Environment() Environment
    SchemaVersion() string
    SourcePath() string
    Internal() *internal.InternalDatacenter
}
```

### HCL Evaluation

Datacenters use HCL with runtime evaluation:

```go
import "github.com/davidthor/cldctl/pkg/schema/datacenter/v1"

// Create parser with context
parser := v1.NewParser()
parser = parser.WithVariable("region", "us-west-2")
parser = parser.WithEnvironment(&v1.EnvironmentContext{
    Name: "production",
})

// Parse with context
schema, err := parser.Parse("./datacenter.hcl")

// Create evaluator for runtime evaluation
evaluator := v1.NewEvaluator()
evaluator.SetVariables(variables)
evaluator.SetEnvironmentContext(envCtx)

// Evaluate a hook
evaluated, err := evaluator.EvaluateHook(hook)
```

### Expression Namespaces

HCL expressions have access to:

- `variable.*` - Datacenter variables
- `environment.*` - Environment context
- `node.*` - Current resource node context
- `module.*` - Module outputs

### Example Datacenter

```hcl
variable "region" {
  type    = string
  default = "us-west-2"
}

module "vpc" {
  source = "./modules/vpc"
  plugin = "opentofu"

  inputs = {
    region = variable.region
    cidr   = "10.0.0.0/16"
  }
}

environment {
  database "postgres" {
    module = "rds"

    inputs = {
      vpc_id = module.vpc.outputs.vpc_id
      name   = node.name
    }

    outputs = {
      host     = module.rds.outputs.endpoint
      port     = 5432
      database = node.name
      username = module.rds.outputs.username
      password = module.rds.outputs.password
      url      = "postgresql://${module.rds.outputs.username}:${module.rds.outputs.password}@${module.rds.outputs.endpoint}:5432/${node.name}"
    }
  }
}
```

## environment

Parses and validates environment configurations that define how components are deployed.

### Usage

```go
import "github.com/davidthor/cldctl/pkg/schema/environment"

// Create a loader
loader := environment.NewLoader()

// Load an environment
env, err := loader.Load("./environment.yml")
if err != nil {
    log.Fatal(err)
}

// Access environment data
fmt.Println("Name:", env.Name())
fmt.Println("Datacenter:", env.Datacenter())
fmt.Println("Components:", len(env.Components()))
```

### Environment Interface

```go
type Environment interface {
    Name() string
    Datacenter() string
    Locals() map[string]interface{}
    Components() []ComponentConfig
    SchemaVersion() string
    SourcePath() string
    Internal() *internal.InternalEnvironment
}
```

### ComponentConfig Interface

```go
type ComponentConfig interface {
    Name() string
    Source() string
    Variables() map[string]string
    Scaling() ScalingConfig
    Functions() map[string]FunctionConfig
    Routes() map[string]RouteConfig
}
```

### Example Environment

```yaml
name: production
datacenter: aws-us-east

locals:
  domain: example.com
  replicas: 3

components:
  api:
    source: ./components/api
    variables:
      domain: ${{ locals.domain }}
    scaling:
      api:
        replicas: ${{ locals.replicas }}

  web:
    source: ghcr.io/myorg/web:v1.0.0
    variables:
      api_url: https://api.${{ locals.domain }}
    routes:
      main:
        hostnames:
          - host: ${{ locals.domain }}
            tls:
              enabled: true
```

## Validation

All loaders provide validation with detailed errors:

```go
// Validate a component
err := componentLoader.Validate("./component.yml")
if err != nil {
    // Error includes file, line, and field information
    fmt.Println(err)
}

// V1 validators return structured errors
validator := v1.NewValidator()
errors := validator.Validate(schema)
for _, e := range errors {
    fmt.Printf("%s: %s (field: %s)\n", e.Path, e.Message, e.Field)
}
```

## Internal Types

Each schema type has internal types for canonical representation:

```go
import "github.com/davidthor/cldctl/pkg/schema/component/internal"

// Access internal representation
internal := comp.Internal()

// Work with internal types directly
for _, db := range internal.Databases {
    fmt.Println(db.Name, db.Type)
}
```

### Expression Type

For components, the internal package provides an Expression type:

```go
import "github.com/davidthor/cldctl/pkg/schema/component/internal"

// Create an expression
expr := internal.NewExpression("${{ variables.name }}")

// Check if it's a literal or has expressions
if expr.IsLiteral() {
    fmt.Println("Static value:", expr.Value())
}
```

## Schema Versioning

- Default version: `"v1"` (used when not specified)
- Version is auto-detected from schema content
- Future versions can be added as new subpackages
