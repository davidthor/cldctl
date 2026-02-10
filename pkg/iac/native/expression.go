package native

import (
	"fmt"
	"os"
	"strings"
)

// EvalContext provides values for expression evaluation.
type EvalContext struct {
	Inputs    map[string]interface{}
	Resources map[string]*ResourceState
}

// evaluateExpression evaluates a simple expression string.
// Supports: ${inputs.name}, ${resources.name.outputs.field},
// and expressions with nested braces like ${merge(a, { KEY: 'val' })}.
func evaluateExpression(expr string, ctx *EvalContext) (interface{}, error) {
	// If no expressions, return as-is
	if !strings.Contains(expr, "${") {
		return expr, nil
	}

	// Find all ${...} spans using brace-depth tracking (the simple regex
	// \$\{[^}]+\} breaks when expressions contain nested braces such as
	// inline map literals like { KEY: 'value' }).
	spans := findExpressionSpans(expr)

	if len(spans) == 0 {
		return expr, nil
	}

	// If the entire string is a single expression, return the actual value (preserving type)
	if len(spans) == 1 && spans[0][0] == 0 && spans[0][1] == len(expr) {
		trimmed := expr[2 : len(expr)-1] // strip ${ and }
		return resolveReference(trimmed, ctx)
	}

	// Otherwise, substitute expressions in the string
	var result strings.Builder
	lastEnd := 0
	for _, span := range spans {
		// Append literal text before this expression
		result.WriteString(expr[lastEnd:span[0]])

		// Extract and resolve the expression body
		body := expr[span[0]+2 : span[1]-1] // strip ${ and }
		value, err := resolveReference(body, ctx)
		if err != nil {
			result.WriteString(expr[span[0]:span[1]]) // keep original on error
		} else {
			result.WriteString(fmt.Sprintf("%v", value))
		}
		lastEnd = span[1]
	}
	// Append any trailing literal text
	result.WriteString(expr[lastEnd:])

	return result.String(), nil
}

// findExpressionSpans locates all ${...} expression boundaries in s,
// correctly handling nested braces. Each returned [2]int is [start, end)
// where start is the index of '$' and end is one past the closing '}'.
func findExpressionSpans(s string) [][2]int {
	var spans [][2]int

	for i := 0; i < len(s)-1; i++ {
		if s[i] != '$' || s[i+1] != '{' {
			continue
		}

		// Track brace depth starting after the opening '{'
		depth := 1
		j := i + 2
		inSingle := false
		inDouble := false

		for j < len(s) && depth > 0 {
			c := s[j]
			switch {
			case c == '\'' && !inDouble:
				inSingle = !inSingle
			case c == '"' && !inSingle:
				inDouble = !inDouble
			case c == '{' && !inSingle && !inDouble:
				depth++
			case c == '}' && !inSingle && !inDouble:
				depth--
			}
			j++
		}

		if depth == 0 {
			spans = append(spans, [2]int{i, j})
			i = j - 1 // advance past this expression
		}
	}

	return spans
}

// resolveReference resolves a dotted reference like "inputs.name" or "resources.container.outputs.port"
func resolveReference(ref string, ctx *EvalContext) (interface{}, error) {
	parts := strings.Split(strings.TrimSpace(ref), ".")
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty reference")
	}

	// Handle inline map literals like { KEY: 'value', OTHER: 'data' }
	trimmedRef := strings.TrimSpace(ref)
	if strings.HasPrefix(trimmedRef, "{") && strings.HasSuffix(trimmedRef, "}") {
		return parseMapLiteral(trimmedRef)
	}

	switch parts[0] {
	case "inputs":
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid input reference: %s", ref)
		}
		return navigatePath(ctx.Inputs, parts[1:])

	case "resources":
		if len(parts) < 3 {
			return nil, fmt.Errorf("invalid resource reference: %s", ref)
		}
		resourceName := parts[1]
		resource, ok := ctx.Resources[resourceName]
		if !ok {
			return nil, fmt.Errorf("resource not found: %s", resourceName)
		}

		// Handle resources.name.outputs.field or resources.name.properties.field
		if parts[2] == "outputs" {
			return navigatePath(resource.Outputs, parts[3:])
		} else if parts[2] == "properties" {
			return navigatePath(resource.Properties, parts[3:])
		} else if parts[2] == "id" {
			return resource.ID, nil
		}

		// Shorthand: resources.name.field -> try outputs first, then properties
		// This allows expressions like ${resources.proxy.ports[0].host} instead of
		// ${resources.proxy.outputs.ports[0].host}
		if resource.Outputs != nil {
			result, err := navigatePath(resource.Outputs, parts[2:])
			if err == nil && result != nil {
				return result, nil
			}
		}
		if resource.Properties != nil {
			result, err := navigatePath(resource.Properties, parts[2:])
			if err == nil && result != nil {
				return result, nil
			}
		}
		return nil, fmt.Errorf("property %s not found in resource %s", parts[2], resourceName)

	default:
		// Try as a function call
		return evaluateFunction(ref, ctx)
	}
}

// parseMapLiteral parses an inline map literal like { KEY: 'value', OTHER: 'val2' }
// into a map[string]interface{}. Supports single-quoted, double-quoted, and unquoted values.
func parseMapLiteral(s string) (map[string]interface{}, error) {
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return make(map[string]interface{}), nil
	}

	result := make(map[string]interface{})

	// Split on commas, respecting quoted strings
	pairs := splitMapPairs(inner)
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		// Split key: value (first colon only)
		idx := strings.Index(pair, ":")
		if idx < 0 {
			return nil, fmt.Errorf("invalid map pair (missing ':'): %s", pair)
		}

		key := strings.TrimSpace(pair[:idx])
		value := strings.TrimSpace(pair[idx+1:])

		// Strip surrounding quotes from key and value
		key = stripQuotes(key)
		value = stripQuotes(value)

		result[key] = value
	}

	return result, nil
}

// splitMapPairs splits a string by commas at the top level, respecting quoted strings.
func splitMapPairs(s string) []string {
	var pairs []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			current.WriteByte(c)
		case c == '"' && !inSingle:
			inDouble = !inDouble
			current.WriteByte(c)
		case c == ',' && !inSingle && !inDouble:
			pairs = append(pairs, current.String())
			current.Reset()
		default:
			current.WriteByte(c)
		}
	}
	if current.Len() > 0 {
		pairs = append(pairs, current.String())
	}
	return pairs
}

// navigatePath navigates a path through nested maps and arrays.
// Supports array indexing like "ports[0]" or "items[2].name".
// Returns nil (not error) for missing keys to support optional values.
func navigatePath(data interface{}, path []string) (interface{}, error) {
	if len(path) == 0 {
		return data, nil
	}

	current := data
	for _, key := range path {
		// Check for array indexing: key[index]
		if idx := strings.Index(key, "["); idx != -1 {
			// Split into key and index parts
			baseKey := key[:idx]
			indexPart := key[idx:]

			// First navigate to the base key if it's not empty
			if baseKey != "" {
				var err error
				current, err = navigatePath(current, []string{baseKey})
				if err != nil || current == nil {
					return nil, err
				}
			}

			// Parse index from [N] format
			if !strings.HasSuffix(indexPart, "]") {
				return nil, fmt.Errorf("invalid array index: %s", key)
			}
			indexStr := indexPart[1 : len(indexPart)-1]
			index := 0
			if _, err := fmt.Sscanf(indexStr, "%d", &index); err != nil {
				return nil, fmt.Errorf("invalid array index: %s", indexStr)
			}

			// Access the array element
			switch arr := current.(type) {
			case []interface{}:
				if index < 0 || index >= len(arr) {
					return nil, nil // Out of bounds, return nil
				}
				current = arr[index]
			case []map[string]interface{}:
				if index < 0 || index >= len(arr) {
					return nil, nil
				}
				current = arr[index]
			default:
				return nil, fmt.Errorf("cannot index into %T", current)
			}
			continue
		}

		switch v := current.(type) {
		case map[string]interface{}:
			var ok bool
			current, ok = v[key]
			if !ok {
				// Return nil for missing keys (supports optional inputs)
				return nil, nil
			}
		case map[string]string:
			val, ok := v[key]
			if !ok {
				// Return nil for missing keys (supports optional inputs)
				return nil, nil
			}
			current = val
		case nil:
			// If we hit nil, the path doesn't exist
			return nil, nil
		default:
			return nil, fmt.Errorf("cannot navigate into %T", current)
		}
	}

	return current, nil
}

// evaluateFunction evaluates a function call like "random_password(16)"
func evaluateFunction(expr string, ctx *EvalContext) (interface{}, error) {
	// Parse function name and arguments
	openParen := strings.Index(expr, "(")
	if openParen == -1 {
		return nil, fmt.Errorf("unknown function or reference: %s", expr)
	}

	funcName := strings.TrimSpace(expr[:openParen])
	argsStr := strings.TrimSpace(expr[openParen+1:])
	if !strings.HasSuffix(argsStr, ")") {
		return nil, fmt.Errorf("invalid function call: %s", expr)
	}
	argsStr = argsStr[:len(argsStr)-1]

	switch funcName {
	case "random_password":
		return generateRandomString(16), nil

	case "coalesce":
		// Return first non-empty value
		args := splitFunctionArgs(argsStr)
		for _, arg := range args {
			val, err := resolveReference(arg, ctx)
			if err == nil && val != nil {
				// Check if value is non-empty
				switch v := val.(type) {
				case string:
					if v != "" {
						return v, nil
					}
				case []interface{}:
					if len(v) > 0 {
						return v, nil
					}
				default:
					return v, nil
				}
			}
		}
		return nil, nil

	case "dockerfile_cmd":
		// Extract CMD from Dockerfile
		args := splitFunctionArgs(argsStr)
		if len(args) < 1 {
			return nil, fmt.Errorf("dockerfile_cmd requires at least 1 argument")
		}

		// Resolve context path
		contextPath, err := resolveReference(args[0], ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve context path: %w", err)
		}

		// Resolve dockerfile path (optional)
		dockerfilePath := "Dockerfile"
		if len(args) > 1 {
			dfPath, err := resolveReference(args[1], ctx)
			if err == nil {
				if dfPathStr, ok := dfPath.(string); ok {
					dockerfilePath = dfPathStr
				}
			}
		}

		contextStr, ok := contextPath.(string)
		if !ok {
			return nil, fmt.Errorf("context path must be a string")
		}

		cmd, err := ExtractDockerfileCmdFromContext(contextStr, dockerfilePath)
		if err != nil {
			// Log the error for debugging but return nil so coalesce can fall back
			fmt.Fprintf(os.Stderr, "Warning: Failed to extract CMD from Dockerfile: %v\n", err)
			return nil, nil
		}
		return cmd, nil

	case "lookup_port":
		// lookup_port(target, port) - Look up the host port for a container port mapping.
		// Falls back to returning the port argument if no mapping is found.
		args := splitFunctionArgs(argsStr)
		if len(args) < 2 {
			return nil, fmt.Errorf("lookup_port requires 2 arguments (target, port)")
		}

		// Resolve the port argument (may be a quoted string or a reference)
		portArg := strings.TrimSpace(args[1])
		portVal, err := resolveReference(portArg, ctx)
		if err != nil {
			portVal = stripQuotes(portArg)
		}

		// Resolve the target argument (may be a quoted string or a reference)
		targetArg := strings.TrimSpace(args[0])
		target, _ := resolveReference(targetArg, ctx)
		if target == nil {
			target = stripQuotes(targetArg)
		}

		if targetName, ok := target.(string); ok && ctx != nil && ctx.Resources != nil {
			if resource, ok := ctx.Resources[targetName]; ok && resource.Outputs != nil {
				if ports, ok := resource.Outputs["ports"]; ok {
					portStr := fmt.Sprintf("%v", portVal)
					if hostPort := lookupPortMapping(ports, portStr); hostPort != nil {
						return hostPort, nil
					}
				}
			}
		}

		// Fall back to returning the port argument as-is
		return portVal, nil

	case "jsonencode":
		// jsonencode(value) - Encode a value as JSON
		args := splitFunctionArgs(argsStr)
		if len(args) < 1 {
			return nil, fmt.Errorf("jsonencode requires 1 argument")
		}
		val, err := resolveReference(args[0], ctx)
		if err != nil {
			// Try as literal string
			return args[0], nil
		}
		// For simple cases, just return the value as string
		return fmt.Sprintf("%v", val), nil

	case "random_string":
		// Alias for random_password
		return generateRandomString(16), nil

	case "merge":
		// merge(map1, map2) - Merge two maps, map2 values override map1
		args := splitFunctionArgs(argsStr)
		if len(args) < 2 {
			return nil, fmt.Errorf("merge requires 2 arguments")
		}

		// Resolve both arguments
		val1, err1 := resolveReference(args[0], ctx)
		val2, err2 := resolveReference(args[1], ctx)

		result := make(map[string]interface{})

		// Add values from first map if it's a map
		if err1 == nil {
			if m1, ok := val1.(map[string]interface{}); ok {
				for k, v := range m1 {
					result[k] = v
				}
			}
		}

		// Override/add values from second map
		if err2 == nil {
			if m2, ok := val2.(map[string]interface{}); ok {
				for k, v := range m2 {
					result[k] = v
				}
			}
		}

		return result, nil

	case "framework_command":
		// framework_command(framework) - Return the appropriate start command for a framework
		args := splitFunctionArgs(argsStr)
		if len(args) < 1 {
			return nil, fmt.Errorf("framework_command requires 1 argument")
		}

		framework, err := resolveReference(args[0], ctx)
		if err != nil {
			framework = args[0]
		}

		frameworkStr, ok := framework.(string)
		if !ok {
			return []string{"npm", "start"}, nil
		}

		// Return appropriate command based on framework
		switch strings.ToLower(frameworkStr) {
		case "nextjs", "next":
			return []string{"npm", "run", "dev"}, nil
		case "react", "create-react-app":
			return []string{"npm", "start"}, nil
		case "vue", "nuxt":
			return []string{"npm", "run", "dev"}, nil
		case "express", "node":
			return []string{"npm", "start"}, nil
		case "fastapi", "flask", "django":
			return []string{"python", "-m", "uvicorn", "main:app", "--reload"}, nil
		case "go", "golang":
			return []string{"go", "run", "."}, nil
		default:
			return []string{"npm", "start"}, nil
		}

	default:
		return nil, fmt.Errorf("unknown function: %s", funcName)
	}
}

// splitFunctionArgs splits function arguments by commas (simplified).
func splitFunctionArgs(argsStr string) []string {
	if argsStr == "" {
		return nil
	}

	var args []string
	var current strings.Builder
	depth := 0

	for _, ch := range argsStr {
		switch ch {
		case '(':
			depth++
			current.WriteRune(ch)
		case ')':
			depth--
			current.WriteRune(ch)
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(current.String()))
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}

	if current.Len() > 0 {
		args = append(args, strings.TrimSpace(current.String()))
	}

	return args
}

// stripQuotes removes surrounding quotes from a string literal.
func stripQuotes(s string) string {
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}

// lookupPortMapping finds a host port in a port mapping.
// Supports array format [{container: 80, host: 55123}] and legacy map format {"80/tcp": 55123}.
// Tries exact match first, then appends "/tcp" suffix for map formats.
func lookupPortMapping(ports interface{}, portSpec string) interface{} {
	switch portMap := ports.(type) {
	case []interface{}:
		// Array format: [{container: 80, host: 55123}, ...]
		for _, item := range portMap {
			if m, ok := item.(map[string]interface{}); ok {
				containerPort := fmt.Sprintf("%v", m["container"])
				if containerPort == portSpec {
					return m["host"]
				}
			}
		}
	case map[string]interface{}:
		if hostPort, ok := portMap[portSpec]; ok {
			return hostPort
		}
		if !strings.Contains(portSpec, "/") {
			if hostPort, ok := portMap[portSpec+"/tcp"]; ok {
				return hostPort
			}
		}
	case map[string]int:
		if hostPort, ok := portMap[portSpec]; ok {
			return hostPort
		}
		if !strings.Contains(portSpec, "/") {
			if hostPort, ok := portMap[portSpec+"/tcp"]; ok {
				return hostPort
			}
		}
	}
	return nil
}

// generateRandomString generates a random alphanumeric string.
func generateRandomString(length int) string {
	// Simplified random string generation
	// In production, use crypto/rand
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = chars[i%len(chars)]
	}
	return string(result)
}

// resolveProperties resolves all expressions in a properties map.
func resolveProperties(props map[string]interface{}, ctx *EvalContext) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	for key, value := range props {
		resolved, err := resolveValue(value, ctx)
		if err != nil {
			return nil, fmt.Errorf("property %s: %w", key, err)
		}
		// Skip nil values (optional inputs that weren't provided)
		if resolved != nil {
			result[key] = resolved
		}
	}

	return result, nil
}

// resolveValue recursively resolves expressions in a value.
func resolveValue(value interface{}, ctx *EvalContext) (interface{}, error) {
	switch v := value.(type) {
	case string:
		return evaluateExpression(v, ctx)

	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, val := range v {
			resolved, err := resolveValue(val, ctx)
			if err != nil {
				return nil, err
			}
			result[k] = resolved
		}
		return result, nil

	case []interface{}:
		result := make([]interface{}, len(v))
		for i, val := range v {
			resolved, err := resolveValue(val, ctx)
			if err != nil {
				return nil, err
			}
			result[i] = resolved
		}
		return result, nil

	default:
		return value, nil
	}
}
