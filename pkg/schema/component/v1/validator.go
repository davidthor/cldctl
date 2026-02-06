package v1

import (
	"fmt"
	"strings"
)

// ValidationError represents a validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Validator validates v1 component schemas.
type Validator struct{}

// NewValidator creates a new v1 validator.
func NewValidator() *Validator {
	return &Validator{}
}

// Validate validates a v1 schema and returns all validation errors.
func (v *Validator) Validate(schema *SchemaV1) []ValidationError {
	var errs []ValidationError

	// Validate builds
	errs = append(errs, v.validateBuilds(schema.Builds)...)

	// Validate databases
	errs = append(errs, v.validateDatabases(schema.Databases)...)

	// Validate buckets
	errs = append(errs, v.validateBuckets(schema.Buckets)...)

	// Validate encryption keys
	errs = append(errs, v.validateEncryptionKeys(schema.EncryptionKeys)...)

	// Validate SMTP connections
	errs = append(errs, v.validateSMTP(schema.SMTP)...)

	// Validate deployments
	errs = append(errs, v.validateDeployments(schema.Deployments)...)

	// Validate functions
	errs = append(errs, v.validateFunctions(schema.Functions)...)

	// Validate services
	errs = append(errs, v.validateServices(schema.Services, schema.Deployments, schema.Functions)...)

	// Validate routes
	errs = append(errs, v.validateRoutes(schema.Routes, schema.Services, schema.Functions)...)

	// Validate cronjobs
	errs = append(errs, v.validateCronjobs(schema.Cronjobs)...)

	// Validate variables
	errs = append(errs, v.validateVariables(schema.Variables)...)

	// Validate dependencies
	errs = append(errs, v.validateDependencies(schema.Dependencies)...)

	return errs
}

func (v *Validator) validateDatabases(databases map[string]DatabaseV1) []ValidationError {
	var errs []ValidationError

	validTypes := []string{"postgres", "mysql", "mongodb", "redis", "mariadb", "cockroachdb", "clickhouse"}

	for name, db := range databases {
		if db.Type == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("databases.%s.type", name),
				Message: "type is required",
			})
		} else {
			// Parse type:version format
			parts := strings.SplitN(db.Type, ":", 2)
			dbType := parts[0]
			if !contains(validTypes, dbType) {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("databases.%s.type", name),
					Message: fmt.Sprintf("invalid database type %q, must be one of: %v", dbType, validTypes),
				})
			}
		}

		// Validate migrations
		if db.Migrations != nil {
			if db.Migrations.Build != nil && db.Migrations.Image != "" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("databases.%s.migrations", name),
					Message: "build and image are mutually exclusive",
				})
			}
			if db.Migrations.Build == nil && db.Migrations.Image == "" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("databases.%s.migrations", name),
					Message: "either build or image is required",
				})
			}
			if db.Migrations.Build != nil && db.Migrations.Build.Context == "" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("databases.%s.migrations.build.context", name),
					Message: "context is required for build",
				})
			}
		}
	}

	return errs
}

func (v *Validator) validateBuckets(buckets map[string]BucketV1) []ValidationError {
	var errs []ValidationError

	validTypes := []string{"s3", "gcs", "azure-blob"}

	for name, bucket := range buckets {
		if bucket.Type == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("buckets.%s.type", name),
				Message: "type is required",
			})
		} else if !contains(validTypes, bucket.Type) {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("buckets.%s.type", name),
				Message: fmt.Sprintf("invalid bucket type %q, must be one of: %v", bucket.Type, validTypes),
			})
		}
	}

	return errs
}

func (v *Validator) validateEncryptionKeys(encryptionKeys map[string]EncryptionKeyV1) []ValidationError {
	var errs []ValidationError

	validTypes := []string{"rsa", "ecdsa", "symmetric"}
	validRSABits := []int{2048, 3072, 4096}
	validECDSACurves := []string{"P-256", "P-384", "P-521"}

	for name, ek := range encryptionKeys {
		if ek.Type == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("encryptionKeys.%s.type", name),
				Message: "type is required",
			})
			continue
		}

		if !contains(validTypes, ek.Type) {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("encryptionKeys.%s.type", name),
				Message: fmt.Sprintf("invalid encryption key type %q, must be one of: %v", ek.Type, validTypes),
			})
			continue
		}

		// Validate type-specific fields
		switch ek.Type {
		case "rsa":
			if ek.Bits != 0 && !containsInt(validRSABits, ek.Bits) {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("encryptionKeys.%s.bits", name),
					Message: fmt.Sprintf("invalid RSA key size %d, must be one of: %v", ek.Bits, validRSABits),
				})
			}
			if ek.Curve != "" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("encryptionKeys.%s.curve", name),
					Message: "curve is not applicable for RSA keys",
				})
			}
			if ek.Bytes != 0 {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("encryptionKeys.%s.bytes", name),
					Message: "bytes is not applicable for RSA keys",
				})
			}

		case "ecdsa":
			if ek.Curve != "" && !contains(validECDSACurves, ek.Curve) {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("encryptionKeys.%s.curve", name),
					Message: fmt.Sprintf("invalid ECDSA curve %q, must be one of: %v", ek.Curve, validECDSACurves),
				})
			}
			if ek.Bits != 0 {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("encryptionKeys.%s.bits", name),
					Message: "bits is not applicable for ECDSA keys",
				})
			}
			if ek.Bytes != 0 {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("encryptionKeys.%s.bytes", name),
					Message: "bytes is not applicable for ECDSA keys",
				})
			}

		case "symmetric":
			if ek.Bytes != 0 && (ek.Bytes < 1 || ek.Bytes > 1024) {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("encryptionKeys.%s.bytes", name),
					Message: "bytes must be between 1 and 1024",
				})
			}
			if ek.Bits != 0 {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("encryptionKeys.%s.bits", name),
					Message: "bits is not applicable for symmetric keys",
				})
			}
			if ek.Curve != "" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("encryptionKeys.%s.curve", name),
					Message: "curve is not applicable for symmetric keys",
				})
			}
		}
	}

	return errs
}

func (v *Validator) validateSMTP(smtp map[string]SMTPV1) []ValidationError {
	// SMTP declarations require no validation - they can be empty objects
	// The datacenter provisions the connection credentials
	return nil
}

func (v *Validator) validateBuilds(builds map[string]BuildV1) []ValidationError {
	var errs []ValidationError

	for name, build := range builds {
		if build.Context == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("builds.%s.context", name),
				Message: "context is required for build",
			})
		}
	}

	return errs
}

func (v *Validator) validateDeployments(deployments map[string]DeploymentV1) []ValidationError {
	var errs []ValidationError

	validOS := []string{"linux", "windows"}
	validArch := []string{"amd64", "arm64"}

	for name, dep := range deployments {
		// Image is optional. When absent, the datacenter decides how to
		// execute (e.g., as a host process for local development).
		if dep.Replicas < 0 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("deployments.%s.replicas", name),
				Message: "replicas must be non-negative",
			})
		}

		// Validate runtime
		if dep.Runtime != nil {
			if dep.Runtime.Language == "" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("deployments.%s.runtime.language", name),
					Message: "language is required when runtime is specified",
				})
			}
			if dep.Runtime.OS != "" && !contains(validOS, dep.Runtime.OS) {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("deployments.%s.runtime.os", name),
					Message: fmt.Sprintf("invalid os %q, must be one of: %v", dep.Runtime.OS, validOS),
				})
			}
			if dep.Runtime.Arch != "" && !contains(validArch, dep.Runtime.Arch) {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("deployments.%s.runtime.arch", name),
					Message: fmt.Sprintf("invalid arch %q, must be one of: %v", dep.Runtime.Arch, validArch),
				})
			}
		}
	}

	return errs
}

func (v *Validator) validateFunctions(functions map[string]FunctionV1) []ValidationError {
	var errs []ValidationError

	for name, fn := range functions {
		// Validate discriminated union: exactly one of src or container must be set
		hasSrc := fn.Src != nil
		hasContainer := fn.Container != nil

		if !hasSrc && !hasContainer {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("functions.%s", name),
				Message: "either src or container is required",
			})
		}
		if hasSrc && hasContainer {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("functions.%s", name),
				Message: "src and container are mutually exclusive",
			})
		}

		// Validate src-based function
		if fn.Src != nil {
			if fn.Src.Path == "" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("functions.%s.src.path", name),
					Message: "path is required",
				})
			}
			// All other src fields are optional (can be inferred)
		}

		// Validate container-based function
		if fn.Container != nil {
			hasBuild := fn.Container.Build != nil
			hasImage := fn.Container.Image != ""

			if !hasBuild && !hasImage {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("functions.%s.container", name),
					Message: "either build or image is required",
				})
			}
			if hasBuild && hasImage {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("functions.%s.container", name),
					Message: "build and image are mutually exclusive",
				})
			}
			if fn.Container.Build != nil && fn.Container.Build.Context == "" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("functions.%s.container.build.context", name),
					Message: "context is required for build",
				})
			}
		}

		// Validate common fields
		if fn.Timeout < 0 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("functions.%s.timeout", name),
				Message: "timeout must be non-negative",
			})
		}
	}

	return errs
}

func (v *Validator) validateServices(services map[string]ServiceV1, deployments map[string]DeploymentV1, _ map[string]FunctionV1) []ValidationError {
	var errs []ValidationError

	for name, svc := range services {
		// Count how many targets are specified
		targets := 0
		if svc.Deployment != "" {
			targets++
		}
		if svc.URL != "" {
			targets++
		}

		if targets == 0 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("services.%s", name),
				Message: "deployment or url is required",
			})
		}
		if targets > 1 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("services.%s", name),
				Message: "deployment and url are mutually exclusive",
			})
		}

		// Validate deployment reference
		if svc.Deployment != "" {
			if _, ok := deployments[svc.Deployment]; !ok {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("services.%s.deployment", name),
					Message: fmt.Sprintf("deployment %q not found", svc.Deployment),
				})
			}
		}
	}

	return errs
}

func (v *Validator) validateRoutes(routes map[string]RouteV1, services map[string]ServiceV1, functions map[string]FunctionV1) []ValidationError {
	var errs []ValidationError

	validTypes := []string{"http", "grpc"}

	for name, route := range routes {
		if route.Type == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("routes.%s.type", name),
				Message: "type is required",
			})
		} else if !contains(validTypes, route.Type) {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("routes.%s.type", name),
				Message: fmt.Sprintf("invalid route type %q, must be one of: %v", route.Type, validTypes),
			})
		}

		// Check if using simplified or full form
		hasRules := len(route.Rules) > 0
		hasSimplified := route.Service != "" || route.Function != ""

		if hasRules && hasSimplified {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("routes.%s", name),
				Message: "rules and simplified form (service/function) are mutually exclusive",
			})
		}

		if !hasRules && !hasSimplified {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("routes.%s", name),
				Message: "either rules or service/function is required",
			})
		}

		// Validate simplified form references
		if route.Service != "" {
			if _, ok := services[route.Service]; !ok {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("routes.%s.service", name),
					Message: fmt.Sprintf("service %q not found", route.Service),
				})
			}
		}
		if route.Function != "" {
			if _, ok := functions[route.Function]; !ok {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("routes.%s.function", name),
					Message: fmt.Sprintf("function %q not found", route.Function),
				})
			}
		}

		// Validate backend references in rules
		for i, rule := range route.Rules {
			for j, backend := range rule.BackendRefs {
				if backend.Service == "" && backend.Function == "" {
					errs = append(errs, ValidationError{
						Field:   fmt.Sprintf("routes.%s.rules[%d].backendRefs[%d]", name, i, j),
						Message: "either service or function is required",
					})
				}
				if backend.Service != "" {
					if _, ok := services[backend.Service]; !ok {
						errs = append(errs, ValidationError{
							Field:   fmt.Sprintf("routes.%s.rules[%d].backendRefs[%d].service", name, i, j),
							Message: fmt.Sprintf("service %q not found", backend.Service),
						})
					}
				}
				if backend.Function != "" {
					if _, ok := functions[backend.Function]; !ok {
						errs = append(errs, ValidationError{
							Field:   fmt.Sprintf("routes.%s.rules[%d].backendRefs[%d].function", name, i, j),
							Message: fmt.Sprintf("function %q not found", backend.Function),
						})
					}
				}
			}
		}
	}

	return errs
}

func (v *Validator) validateCronjobs(cronjobs map[string]CronjobV1) []ValidationError {
	var errs []ValidationError

	for name, cj := range cronjobs {
		if cj.Schedule == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("cronjobs.%s.schedule", name),
				Message: "schedule is required",
			})
		}
		if cj.Image == "" && cj.Build == nil {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("cronjobs.%s", name),
				Message: "either image or build is required",
			})
		}
		if cj.Image != "" && cj.Build != nil {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("cronjobs.%s", name),
				Message: "image and build are mutually exclusive",
			})
		}
	}

	return errs
}

func (v *Validator) validateVariables(variables map[string]VariableV1) []ValidationError {
	var errs []ValidationError

	for name, variable := range variables {
		if variable.Required && variable.Default != nil {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("variables.%s", name),
				Message: "required variables should not have a default value",
			})
		}
	}

	return errs
}

func (v *Validator) validateDependencies(dependencies map[string]string) []ValidationError {
	var errs []ValidationError

	for name, component := range dependencies {
		if component == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("dependencies.%s", name),
				Message: "component reference is required",
			})
			continue
		}

		// Reject file path references - dependencies must be OCI references
		if isFilePath(component) {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("dependencies.%s", name),
				Message: "file path references are not allowed; use OCI registry references (e.g., ghcr.io/org/component:v1)",
			})
		}
	}

	return errs
}

// isFilePath checks if a reference looks like a local file path.
func isFilePath(ref string) bool {
	// Check for path prefixes
	if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") || strings.HasPrefix(ref, "/") {
		return true
	}

	// Check for YAML file extensions
	if strings.HasSuffix(ref, ".yml") || strings.HasSuffix(ref, ".yaml") {
		return true
	}

	return false
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func containsInt(slice []int, item int) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
