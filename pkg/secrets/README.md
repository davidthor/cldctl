# secrets

Secret management for cldctl with a pluggable provider system. Supports multiple backends including environment variables, AWS Secrets Manager, HashiCorp Vault, and file-based storage.

## Overview

The `secrets` package provides:

- Unified interface for secret retrieval and storage
- Multiple backend providers
- Priority-based lookup across providers
- Caching to reduce provider calls
- Secret reference resolution in configuration data

## Providers

### Available Providers

| Provider | Description           | Read | Write | Delete |
| -------- | --------------------- | ---- | ----- | ------ |
| `env`    | Environment variables | ✓    | ✗     | ✗      |
| `file`   | In-memory map         | ✓    | ✓     | ✓      |
| `aws`    | AWS Secrets Manager   | ✓    | ✓     | ✓      |
| `vault`  | HashiCorp Vault       | ✓    | ✓     | ✓      |

### Provider Interface

```go
type Provider interface {
    Name() string
    Get(ctx context.Context, key string) (string, error)
    GetBatch(ctx context.Context, keys []string) (map[string]string, error)
    List(ctx context.Context, prefix string) ([]string, error)
    Set(ctx context.Context, key, value string) error
    Delete(ctx context.Context, key string) error
}
```

## Manager

The manager coordinates multiple providers with priority ordering.

### Creating a Manager

```go
import "github.com/davidthor/cldctl/pkg/secrets"

// Default manager (includes EnvProvider)
manager := secrets.DefaultManager()

// Empty manager
manager := secrets.NewManager()
```

### Registering Providers

```go
// Environment variable provider
manager.RegisterProvider(secrets.NewEnvProvider())

// With custom prefix
manager.RegisterProvider(secrets.NewEnvProviderWithPrefix("MYAPP_SECRET_"))

// File-based provider
manager.RegisterProvider(secrets.NewFileProvider(map[string]string{
    "api-key": "secret-value",
}))

// AWS Secrets Manager
awsProvider, err := secrets.NewAWSProvider(ctx, secrets.AWSConfig{
    Region: "us-west-2",
    Prefix: "myapp/",
})
manager.RegisterProvider(awsProvider)

// HashiCorp Vault
vaultProvider, err := secrets.NewVaultProvider(secrets.VaultConfig{
    Address:   "https://vault.example.com",
    Token:     os.Getenv("VAULT_TOKEN"),
    Namespace: "myapp",
    MountPath: "secret",
})
manager.RegisterProvider(vaultProvider)
```

### Setting Priority

```go
// Set the order in which providers are checked
manager.SetPriority([]string{"vault", "aws", "env"})
```

### Retrieving Secrets

```go
// Get from first provider that has the secret
value, err := manager.Get(ctx, "database-password")
if err == secrets.ErrSecretNotFound {
    log.Fatal("Secret not found in any provider")
}

// Get from a specific provider
value, err := manager.GetFromProvider(ctx, "vault", "database-password")

// Get multiple secrets concurrently
secrets, err := manager.GetBatch(ctx, []string{
    "database-password",
    "api-key",
    "jwt-secret",
})
```

### Resolving Secret References

Resolve `${secret:...}` references in configuration data:

```go
config := map[string]interface{}{
    "database": map[string]interface{}{
        "host":     "localhost",
        "password": "${secret:database-password}",
    },
    "api": map[string]interface{}{
        "key": "${secret:vault:api-key}",  // Specific provider
    },
}

resolved, err := manager.ResolveSecrets(ctx, config)
// resolved["database"]["password"] = "actual-password-value"
```

### Caching

```go
// Clear the secret cache
manager.ClearCache()
```

## Providers

### EnvProvider

Reads secrets from environment variables.

```go
provider := secrets.NewEnvProvider()
// Default prefix: CLDCTL_SECRET_

provider := secrets.NewEnvProviderWithPrefix("MYAPP_")
// Reads MYAPP_DATABASE_PASSWORD for key "database-password"
```

Key transformation:

- Converts to uppercase
- Replaces hyphens with underscores
- Applies prefix

Example: `database-password` → `CLDCTL_SECRET_DATABASE_PASSWORD`

### FileProvider

In-memory secret storage, useful for testing or local development.

```go
provider := secrets.NewFileProvider(map[string]string{
    "api-key":     "test-key",
    "db-password": "test-password",
})

// Supports write and delete
provider.Set(ctx, "new-secret", "value")
provider.Delete(ctx, "old-secret")
```

### AWSProvider

Reads secrets from AWS Secrets Manager.

```go
provider, err := secrets.NewAWSProvider(ctx, secrets.AWSConfig{
    Region:          "us-west-2",
    AccessKeyID:     "AKIA...",        // Optional, uses default credentials
    SecretAccessKey: "...",            // Optional
    Prefix:          "myapp/prod/",    // Optional prefix for all keys
    Endpoint:        "...",            // Optional, for LocalStack
})
```

Features:

- Supports JSON secrets with field extraction: `secret-name#field`
- Uses AWS SDK v2 with default credential chain
- Supports custom endpoints for testing

### VaultProvider

Reads secrets from HashiCorp Vault (KV v2 engine).

```go
provider, err := secrets.NewVaultProvider(secrets.VaultConfig{
    Address:   "https://vault.example.com",
    Token:     os.Getenv("VAULT_TOKEN"),
    Namespace: "myapp",           // Optional, for Enterprise
    MountPath: "secret",          // Default: "secret"
})
```

Features:

- KV v2 engine support
- Supports JSON secrets with field extraction: `secret-name#field`
- Reads token from `VAULT_TOKEN` env var or token file
- Namespace support for Vault Enterprise

## Example: Full Setup

```go
import (
    "context"
    "os"
    "github.com/davidthor/cldctl/pkg/secrets"
)

func main() {
    ctx := context.Background()

    // Create manager
    manager := secrets.NewManager()

    // Add providers
    manager.RegisterProvider(secrets.NewEnvProvider())

    if os.Getenv("AWS_REGION") != "" {
        aws, _ := secrets.NewAWSProvider(ctx, secrets.AWSConfig{
            Region: os.Getenv("AWS_REGION"),
            Prefix: "cldctl/",
        })
        manager.RegisterProvider(aws)
    }

    if os.Getenv("VAULT_ADDR") != "" {
        vault, _ := secrets.NewVaultProvider(secrets.VaultConfig{
            Address: os.Getenv("VAULT_ADDR"),
            Token:   os.Getenv("VAULT_TOKEN"),
        })
        manager.RegisterProvider(vault)
    }

    // Set priority (vault first, then aws, then env)
    manager.SetPriority([]string{"vault", "aws", "env"})

    // Retrieve secrets
    dbPassword, err := manager.Get(ctx, "database-password")
    if err != nil {
        log.Fatal(err)
    }

    // Resolve secret references in config
    config := loadConfig()
    resolved, err := manager.ResolveSecrets(ctx, config)
    if err != nil {
        log.Fatal(err)
    }
}
```

## Secret Reference Syntax

```
${secret:key}           - Get from any provider (priority order)
${secret:provider:key}  - Get from specific provider
```

Examples:

```yaml
database:
  password: ${secret:db-password}

api:
  key: ${secret:vault:api-key}
  aws_key: ${secret:aws:credentials#access_key}
```

## Error Handling

```go
value, err := manager.Get(ctx, "missing-secret")
if err == secrets.ErrSecretNotFound {
    // Secret not found in any provider
}
```

## Thread Safety

- Manager uses mutexes for concurrent access
- Cache is thread-safe
- Batch retrieval uses goroutines for parallel fetching
