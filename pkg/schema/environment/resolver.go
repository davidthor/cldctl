package environment

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/davidthor/cldctl/pkg/schema/environment/internal"
)

// expressionPattern matches ${{ ... }} expressions in string values.
var expressionPattern = regexp.MustCompile(`\$\{\{\s*(.+?)\s*\}\}`)

// ResolveOptions configures how environment variables are resolved.
type ResolveOptions struct {
	// CLIVars are variable overrides from --var flags (highest priority).
	CLIVars map[string]string

	// DotenvVars are values loaded from the dotenv file chain.
	DotenvVars map[string]string

	// EnvName is the environment name, used in error messages.
	EnvName string
}

// ResolveVariables resolves all declared environment variables using the priority chain:
//  1. CLI --var overrides
//  2. OS environment variables (auto-mapped UPPER_SNAKE_CASE or explicit env field)
//  3. Dotenv file values (same key matching as OS env)
//  4. Default value from the variable declaration
//  5. Error if required and no value found
//
// After resolving variables, it substitutes ${{ variables.* }} and ${{ locals.* }}
// expressions in all component variable values.
func ResolveVariables(env *internal.InternalEnvironment, opts ResolveOptions) error {
	// Step 1: Resolve declared variable values
	resolved, err := resolveVariableValues(env.Variables, opts)
	if err != nil {
		return err
	}

	// Step 2: Substitute expressions in component configs
	return resolveComponentExpressions(env, resolved)
}

// resolveVariableValues resolves each declared variable to a concrete value.
func resolveVariableValues(vars map[string]InternalEnvironmentVariable, opts ResolveOptions) (map[string]interface{}, error) {
	resolved := make(map[string]interface{}, len(vars))
	var missing []string

	for name, v := range vars {
		value, found := resolveOneVariable(name, v, opts)
		if found {
			resolved[name] = value
		} else if v.Required {
			missing = append(missing, name)
		}
		// If not required and not found, the variable is simply absent (nil)
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %s\nProvide values via OS environment variables, a .env file, or --var flags", strings.Join(missing, ", "))
	}

	return resolved, nil
}

// resolveOneVariable attempts to resolve a single variable through the priority chain.
func resolveOneVariable(name string, v InternalEnvironmentVariable, opts ResolveOptions) (interface{}, bool) {
	// Determine the env var key to look up
	envKey := v.Env
	if envKey == "" {
		envKey = strings.ToUpper(name)
	}

	// Priority 1: CLI --var override
	if opts.CLIVars != nil {
		if val, ok := opts.CLIVars[name]; ok {
			return val, true
		}
	}

	// Priority 2: OS environment variable
	if val, ok := os.LookupEnv(envKey); ok {
		return val, true
	}

	// Priority 3: Dotenv file value
	if opts.DotenvVars != nil {
		if val, ok := opts.DotenvVars[envKey]; ok {
			return val, true
		}
	}

	// Priority 4: Default value
	if v.Default != nil {
		return v.Default, true
	}

	return nil, false
}

// InternalEnvironmentVariable is a type alias for use in the resolver.
// This avoids a circular import with the internal package.
type InternalEnvironmentVariable = internal.InternalEnvironmentVariable

// resolveComponentExpressions walks all component variable values and substitutes
// ${{ variables.* }} and ${{ locals.* }} expressions with resolved values.
func resolveComponentExpressions(env *internal.InternalEnvironment, resolved map[string]interface{}) error {
	for compName, comp := range env.Components {
		if comp.Variables == nil {
			continue
		}

		resolvedVars := make(map[string]interface{}, len(comp.Variables))
		for key, val := range comp.Variables {
			resolvedVal, err := resolveValue(val, resolved, env.Locals, compName, key)
			if err != nil {
				return err
			}
			resolvedVars[key] = resolvedVal
		}
		comp.Variables = resolvedVars
		env.Components[compName] = comp
	}
	return nil
}

// resolveValue resolves ${{ }} expressions in a single value.
// Supports string values containing expressions, and passes through non-string values.
func resolveValue(val interface{}, variables map[string]interface{}, locals map[string]interface{}, compName, key string) (interface{}, error) {
	str, ok := val.(string)
	if !ok {
		return val, nil
	}

	// Check if the entire value is a single expression (return typed value)
	if match := expressionPattern.FindStringSubmatch(str); match != nil && match[0] == str {
		return resolveExpression(strings.TrimSpace(match[1]), variables, locals, compName, key)
	}

	// Otherwise do string interpolation (multiple expressions or mixed content)
	result := expressionPattern.ReplaceAllStringFunc(str, func(expr string) string {
		match := expressionPattern.FindStringSubmatch(expr)
		if match == nil {
			return expr
		}
		resolved, err := resolveExpression(strings.TrimSpace(match[1]), variables, locals, compName, key)
		if err != nil {
			return expr // Leave unresolved on error (will be caught by validation)
		}
		return fmt.Sprintf("%v", resolved)
	})

	return result, nil
}

// resolveExpression resolves a single expression path like "variables.foo" or "locals.bar".
func resolveExpression(expr string, variables map[string]interface{}, locals map[string]interface{}, compName, key string) (interface{}, error) {
	parts := strings.SplitN(expr, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("components.%s.variables.%s: invalid expression ${{ %s }}", compName, key, expr)
	}

	namespace := parts[0]
	name := parts[1]

	switch namespace {
	case "variables":
		if val, ok := variables[name]; ok {
			return val, nil
		}
		return nil, fmt.Errorf("components.%s.variables.%s: undefined variable ${{ variables.%s }}", compName, key, name)

	case "locals":
		if locals == nil {
			return nil, fmt.Errorf("components.%s.variables.%s: no locals defined, cannot resolve ${{ locals.%s }}", compName, key, name)
		}
		if val, ok := locals[name]; ok {
			return val, nil
		}
		return nil, fmt.Errorf("components.%s.variables.%s: undefined local ${{ locals.%s }}", compName, key, name)

	default:
		return nil, fmt.Errorf("components.%s.variables.%s: unsupported expression namespace %q in ${{ %s }}", compName, key, namespace, expr)
	}
}
