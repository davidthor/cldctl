package v1

import (
	"fmt"
	"regexp"
	"strings"
)

// expressionPattern matches ${{ ... }} expressions.
var validatorExprPattern = regexp.MustCompile(`\$\{\{\s*(.+?)\s*\}\}`)

// ValidationError represents a validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Validator validates v1 environment schemas.
type Validator struct{}

// NewValidator creates a new v1 environment validator.
func NewValidator() *Validator {
	return &Validator{}
}

// Validate validates an environment schema.
func (v *Validator) Validate(schema *SchemaV1) []ValidationError {
	var errors []ValidationError

	// Validate variable declarations
	for name, variable := range schema.Variables {
		varErrors := v.validateVariable(name, variable)
		errors = append(errors, varErrors...)
	}

	// Validate components
	for name, comp := range schema.Components {
		compErrors := v.validateComponent(name, comp)
		errors = append(errors, compErrors...)

		// Validate that ${{ variables.* }} references point to declared variables
		refErrors := v.validateVariableReferences(name, comp, schema.Variables, schema.Locals)
		errors = append(errors, refErrors...)
	}

	// Validate locals don't contain reserved keys
	for key := range schema.Locals {
		if isReservedLocalKey(key) {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("locals.%s", key),
				Message: "reserved key name",
			})
		}
	}

	return errors
}

func (v *Validator) validateVariable(name string, variable EnvironmentVariableV1) []ValidationError {
	var errors []ValidationError
	prefix := fmt.Sprintf("variables.%s", name)

	// Required variables should not have a default value
	if variable.Required && variable.Default != nil {
		errors = append(errors, ValidationError{
			Field:   prefix,
			Message: "required variables should not have a default value",
		})
	}

	return errors
}

// validateVariableReferences checks that ${{ variables.* }} and ${{ locals.* }}
// expressions in component variable values reference declared names.
func (v *Validator) validateVariableReferences(compName string, comp ComponentConfigV1, vars map[string]EnvironmentVariableV1, locals map[string]interface{}) []ValidationError {
	var errors []ValidationError

	for key, val := range comp.Variables {
		str, ok := val.(string)
		if !ok {
			continue
		}

		matches := validatorExprPattern.FindAllStringSubmatch(str, -1)
		for _, match := range matches {
			expr := strings.TrimSpace(match[1])
			parts := strings.SplitN(expr, ".", 2)
			if len(parts) != 2 {
				continue
			}

			namespace := parts[0]
			refName := parts[1]

			switch namespace {
			case "variables":
				if _, ok := vars[refName]; !ok {
					errors = append(errors, ValidationError{
						Field:   fmt.Sprintf("components.%s.variables.%s", compName, key),
						Message: fmt.Sprintf("references undefined variable %q", refName),
					})
				}
			case "locals":
				if _, ok := locals[refName]; !ok && locals != nil {
					errors = append(errors, ValidationError{
						Field:   fmt.Sprintf("components.%s.variables.%s", compName, key),
						Message: fmt.Sprintf("references undefined local %q", refName),
					})
				}
			}
		}
	}

	return errors
}

func (v *Validator) validateComponent(name string, comp ComponentConfigV1) []ValidationError {
	var errors []ValidationError
	prefix := fmt.Sprintf("components.%s", name)

	if len(comp.Instances) > 0 {
		// Multi-instance mode: path/image are optional at top level
		// (each instance must have a source)
		errors = append(errors, v.validateInstances(prefix, comp)...)
	} else {
		// Single-instance mode: exactly one of path or image must be set
		if comp.Path == "" && comp.Image == "" {
			errors = append(errors, ValidationError{
				Field:   prefix,
				Message: "either path or image is required",
			})
		}
		if comp.Path != "" && comp.Image != "" {
			errors = append(errors, ValidationError{
				Field:   prefix,
				Message: "path and image are mutually exclusive",
			})
		}
	}

	// Validate scaling configs
	for deployName, scaling := range comp.Scaling {
		scalingErrors := v.validateScaling(fmt.Sprintf("%s.scaling.%s", prefix, deployName), scaling)
		errors = append(errors, scalingErrors...)
	}

	// Validate function configs
	for funcName, funcConfig := range comp.Functions {
		funcErrors := v.validateFunction(fmt.Sprintf("%s.functions.%s", prefix, funcName), funcConfig)
		errors = append(errors, funcErrors...)
	}

	// Validate route configs
	for routeName, routeConfig := range comp.Routes {
		routeErrors := v.validateRoute(fmt.Sprintf("%s.routes.%s", prefix, routeName), routeConfig)
		errors = append(errors, routeErrors...)
	}

	return errors
}

func (v *Validator) validateInstances(prefix string, comp ComponentConfigV1) []ValidationError {
	var errors []ValidationError

	if len(comp.Instances) == 0 {
		return errors
	}

	// Check for duplicate instance names
	namesSeen := make(map[string]bool)
	totalWeight := 0

	for i, inst := range comp.Instances {
		instPrefix := fmt.Sprintf("%s.instances[%d]", prefix, i)

		// Name is required
		if inst.Name == "" {
			errors = append(errors, ValidationError{
				Field:   instPrefix + ".name",
				Message: "instance name is required",
			})
		} else if namesSeen[inst.Name] {
			errors = append(errors, ValidationError{
				Field:   instPrefix + ".name",
				Message: fmt.Sprintf("duplicate instance name %q", inst.Name),
			})
		}
		namesSeen[inst.Name] = true

		// Source is required
		if inst.Source == "" {
			errors = append(errors, ValidationError{
				Field:   instPrefix + ".source",
				Message: "instance source is required",
			})
		}

		// Weight must be 0-100
		if inst.Weight < 0 || inst.Weight > 100 {
			errors = append(errors, ValidationError{
				Field:   instPrefix + ".weight",
				Message: "weight must be between 0 and 100",
			})
		}

		totalWeight += inst.Weight
	}

	// Total weight should not exceed 100
	if totalWeight > 100 {
		errors = append(errors, ValidationError{
			Field:   prefix + ".instances",
			Message: fmt.Sprintf("total instance weights (%d) exceed 100", totalWeight),
		})
	}

	return errors
}

func (v *Validator) validateScaling(prefix string, scaling ScalingConfigV1) []ValidationError {
	var errors []ValidationError

	// Replicas must be non-negative
	if scaling.Replicas < 0 {
		errors = append(errors, ValidationError{
			Field:   prefix + ".replicas",
			Message: "replicas must be non-negative",
		})
	}

	// Min/max replicas validation
	if scaling.MinReplicas > 0 && scaling.MaxReplicas > 0 {
		if scaling.MinReplicas > scaling.MaxReplicas {
			errors = append(errors, ValidationError{
				Field:   prefix,
				Message: "min_replicas cannot be greater than max_replicas",
			})
		}
	}

	// CPU format validation
	if scaling.CPU != "" && !isValidResourceQuantity(scaling.CPU) {
		errors = append(errors, ValidationError{
			Field:   prefix + ".cpu",
			Message: "invalid CPU format",
		})
	}

	// Memory format validation
	if scaling.Memory != "" && !isValidResourceQuantity(scaling.Memory) {
		errors = append(errors, ValidationError{
			Field:   prefix + ".memory",
			Message: "invalid memory format",
		})
	}

	return errors
}

func (v *Validator) validateFunction(prefix string, funcConfig FunctionConfigV1) []ValidationError {
	var errors []ValidationError

	// Timeout must be positive
	if funcConfig.Timeout < 0 {
		errors = append(errors, ValidationError{
			Field:   prefix + ".timeout",
			Message: "timeout must be non-negative",
		})
	}

	// Memory format validation
	if funcConfig.Memory != "" && !isValidResourceQuantity(funcConfig.Memory) {
		errors = append(errors, ValidationError{
			Field:   prefix + ".memory",
			Message: "invalid memory format",
		})
	}

	return errors
}

func (v *Validator) validateRoute(prefix string, routeConfig RouteConfigV1) []ValidationError {
	var errors []ValidationError

	// Each hostname must have either subdomain or host, but not both
	for i, hostname := range routeConfig.Hostnames {
		if hostname.Subdomain == "" && hostname.Host == "" {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("%s.hostnames[%d]", prefix, i),
				Message: "hostname must have either subdomain or host",
			})
		}
		if hostname.Subdomain != "" && hostname.Host != "" {
			errors = append(errors, ValidationError{
				Field:   fmt.Sprintf("%s.hostnames[%d]", prefix, i),
				Message: "hostname cannot have both subdomain and host",
			})
		}
	}

	return errors
}

// isReservedLocalKey checks if a key is reserved.
func isReservedLocalKey(key string) bool {
	reserved := []string{"environment", "datacenter", "component", "node"}
	for _, r := range reserved {
		if key == r {
			return true
		}
	}
	return false
}

// isValidResourceQuantity validates a Kubernetes-style resource quantity.
func isValidResourceQuantity(s string) bool {
	// Simple validation - accepts numbers with optional suffix
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}

	// Check for valid suffixes
	validSuffixes := []string{"", "m", "k", "M", "G", "T", "P", "E", "Ki", "Mi", "Gi", "Ti", "Pi", "Ei"}

	// Find numeric part
	numEnd := 0
	for i, c := range s {
		if (c >= '0' && c <= '9') || c == '.' {
			numEnd = i + 1
		} else {
			break
		}
	}

	if numEnd == 0 {
		return false
	}

	suffix := s[numEnd:]
	for _, vs := range validSuffixes {
		if suffix == vs {
			return true
		}
	}

	return false
}
