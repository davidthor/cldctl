package component

// deepMerge merges override onto base using RFC 7396 (JSON Merge Patch) semantics:
//   - Map keys present in override: recursively merged
//   - Map keys absent in override: inherited from base
//   - Explicit nil in override: deletes the key from the result
//   - Scalar values in override: replace the base value
//   - Arrays in override: replace entirely (no element-wise merge)
func deepMerge(base, override map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(base))

	// Copy all base entries
	for k, v := range base {
		result[k] = v
	}

	// Apply override entries
	for k, overrideVal := range override {
		// Explicit null means delete the key (RFC 7396)
		if overrideVal == nil {
			delete(result, k)
			continue
		}

		baseVal, baseExists := result[k]

		overrideMap, overrideIsMap := toStringMap(overrideVal)
		baseMap, baseIsMap := toStringMap(baseVal)

		// If both are maps, merge recursively
		if baseExists && baseIsMap && overrideIsMap {
			result[k] = deepMerge(baseMap, overrideMap)
		} else {
			// Scalars, arrays, and type mismatches: override replaces base
			result[k] = overrideVal
		}
	}

	return result
}

// toStringMap attempts to convert a value to map[string]interface{}.
// YAML unmarshal may produce map[string]interface{} or map[interface{}]interface{}.
func toStringMap(v interface{}) (map[string]interface{}, bool) {
	if v == nil {
		return nil, false
	}

	switch m := v.(type) {
	case map[string]interface{}:
		return m, true
	case map[interface{}]interface{}:
		result := make(map[string]interface{}, len(m))
		for k, val := range m {
			key, ok := k.(string)
			if !ok {
				continue
			}
			result[key] = val
		}
		return result, true
	default:
		return nil, false
	}
}
