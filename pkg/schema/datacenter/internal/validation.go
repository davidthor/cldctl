package internal

import (
	"fmt"
	"sort"
	"strings"
)

// RequiredHookOutputs maps each hook type to the flat output keys that
// non-error hooks MUST declare in their outputs block. This is used at
// build time (during validation/build commands) to catch misconfigured
// hooks before deployment. Missing outputs otherwise surface as cryptic
// unresolved ${{ }} expressions at deploy time.
//
// Notes:
//   - database: username and password are optional because not all engines
//     require credentials (e.g., Redis).
//   - encryptionKey: outputs vary by algorithm (RSA/ECDSA vs symmetric),
//     so we only require the common set (none — validated at runtime).
//   - port: the hook is optional (engine has a built-in fallback).
var RequiredHookOutputs = map[string][]string{
	"database":      {"host", "port", "url"},
	"bucket":        {"endpoint", "bucket", "accessKeyId", "secretAccessKey"},
	"smtp":          {"host", "port", "username", "password"},
	"deployment":    {"id"},
	"function":      {"id", "endpoint"},
	"service":       {"host", "port", "url"},
	"route":         {"url", "host", "port"},
	"task":          {"id", "status"},
	"observability": {"endpoint", "protocol"},
}

// ValidateHookOutputs checks that every non-error hook for the given type
// declares all required output keys. It inspects both the flat Outputs map
// and the NestedOutputs map (which covers nested objects like read/write).
// Returns nil if all hooks are valid.
func ValidateHookOutputs(hookType string, hooks []InternalHook) []error {
	required, ok := RequiredHookOutputs[hookType]
	if !ok {
		return nil // No required outputs defined for this type
	}

	var errs []error
	for i, hook := range hooks {
		// Error hooks don't produce outputs — skip them.
		if hook.Error != "" {
			continue
		}

		// No modules means no output (this could be a partial/extends hook).
		if len(hook.Modules) == 0 {
			continue
		}

		// Collect all declared output keys (flat + nested top-level keys).
		declared := make(map[string]bool)
		for k := range hook.Outputs {
			declared[k] = true
		}
		for k := range hook.NestedOutputs {
			declared[k] = true
		}

		var missing []string
		for _, key := range required {
			if !declared[key] {
				missing = append(missing, key)
			}
		}

		if len(missing) > 0 {
			have := make([]string, 0, len(declared))
			for k := range declared {
				have = append(have, k)
			}
			sort.Strings(have)

			haveStr := "none"
			if len(have) > 0 {
				haveStr = strings.Join(have, ", ")
			}

			label := hookType
			if hook.When != "" {
				label = fmt.Sprintf("%s (when %s)", hookType, hook.When)
			} else if len(hooks) > 1 {
				label = fmt.Sprintf("%s[%d]", hookType, i)
			}

			errs = append(errs, fmt.Errorf(
				"%s hook is missing required outputs: %s (have: %s)",
				label,
				strings.Join(missing, ", "),
				haveStr,
			))
		}
	}

	return errs
}
