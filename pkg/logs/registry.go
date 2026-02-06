package logs

import "fmt"

// Factory is a function that creates a LogQuerier for a given endpoint URL.
type Factory func(endpoint string) (LogQuerier, error)

// registry maps query type names (e.g., "loki") to their factory functions.
// Adapters register themselves via init() using Register().
var registry = map[string]Factory{}

// Register adds a LogQuerier factory under the given name.
// Typically called from an adapter's init() function.
func Register(name string, factory Factory) {
	registry[name] = factory
}

// NewQuerier creates a LogQuerier for the given query type and endpoint.
// Returns an error if the query type is not registered.
func NewQuerier(queryType, endpoint string) (LogQuerier, error) {
	factory, ok := registry[queryType]
	if !ok {
		return nil, fmt.Errorf("unsupported log query type %q (registered types: %v)", queryType, registeredTypes())
	}
	return factory(endpoint)
}

// registeredTypes returns the names of all registered query types.
func registeredTypes() []string {
	types := make([]string, 0, len(registry))
	for name := range registry {
		types = append(types, name)
	}
	return types
}
