package datacenter

import (
	"github.com/davidthor/cldctl/pkg/schema/datacenter/internal"
)

// MergeDatacenters merges a child datacenter with a parent datacenter, producing
// a fully-resolved datacenter. The merge semantics are:
//
//   - Variables: Union; child wins on name collision
//   - Root modules: Union; child wins on name collision
//   - Components: Union; child wins on name collision
//   - Environment modules: Union; child wins on name collision
//   - Hooks (per type): Prepend child hooks before parent hooks. If both have
//     catch-alls (hook without a 'when' condition), only the child's catch-all
//     is kept (it shadows the parent's).
//
// The merged result has Extends set to nil (fully resolved).
func MergeDatacenters(child, parent *internal.InternalDatacenter) *internal.InternalDatacenter {
	merged := &internal.InternalDatacenter{
		Extends:       nil, // Fully resolved
		SourceVersion: child.SourceVersion,
		SourcePath:    child.SourcePath,
	}

	// Merge variables: start with parent, overlay child (child wins on name collision)
	merged.Variables = mergeVariables(child.Variables, parent.Variables)

	// Merge root modules: start with parent, overlay child (child wins on name collision)
	merged.Modules = mergeModules(child.Modules, parent.Modules)

	// Merge components: start with parent, overlay child (child wins on name collision)
	merged.Components = mergeComponents(child.Components, parent.Components)

	// Merge environment
	merged.Environment = mergeEnvironment(child.Environment, parent.Environment)

	return merged
}

// mergeVariables merges child and parent variables. Child wins on name collision.
func mergeVariables(child, parent []internal.InternalVariable) []internal.InternalVariable {
	// Build index of child variables by name
	childIndex := make(map[string]bool, len(child))
	for _, v := range child {
		childIndex[v.Name] = true
	}

	// Start with child variables
	result := make([]internal.InternalVariable, len(child))
	copy(result, child)

	// Add parent variables that are not overridden by child
	for _, v := range parent {
		if !childIndex[v.Name] {
			result = append(result, v)
		}
	}

	return result
}

// mergeModules merges child and parent modules. Child wins on name collision.
func mergeModules(child, parent []internal.InternalModule) []internal.InternalModule {
	childIndex := make(map[string]bool, len(child))
	for _, m := range child {
		childIndex[m.Name] = true
	}

	result := make([]internal.InternalModule, len(child))
	copy(result, child)

	for _, m := range parent {
		if !childIndex[m.Name] {
			result = append(result, m)
		}
	}

	return result
}

// mergeComponents merges child and parent datacenter components. Child wins on name collision.
func mergeComponents(child, parent []internal.InternalDatacenterComponent) []internal.InternalDatacenterComponent {
	childIndex := make(map[string]bool, len(child))
	for _, c := range child {
		childIndex[c.Name] = true
	}

	result := make([]internal.InternalDatacenterComponent, len(child))
	copy(result, child)

	for _, c := range parent {
		if !childIndex[c.Name] {
			result = append(result, c)
		}
	}

	return result
}

// mergeEnvironment merges child and parent environment configurations.
func mergeEnvironment(child, parent internal.InternalEnvironment) internal.InternalEnvironment {
	merged := internal.InternalEnvironment{}

	// Merge environment modules
	merged.Modules = mergeModules(child.Modules, parent.Modules)

	// Merge hooks per type
	merged.Hooks = mergeHooks(child.Hooks, parent.Hooks)

	return merged
}

// mergeHooks merges all hook types between child and parent.
func mergeHooks(child, parent internal.InternalHooks) internal.InternalHooks {
	return internal.InternalHooks{
		Database:      mergeHookSlice(child.Database, parent.Database),
		Task:          mergeHookSlice(child.Task, parent.Task),
		Bucket:        mergeHookSlice(child.Bucket, parent.Bucket),
		EncryptionKey: mergeHookSlice(child.EncryptionKey, parent.EncryptionKey),
		SMTP:          mergeHookSlice(child.SMTP, parent.SMTP),
		DatabaseUser:  mergeHookSlice(child.DatabaseUser, parent.DatabaseUser),
		Deployment:    mergeHookSlice(child.Deployment, parent.Deployment),
		Function:      mergeHookSlice(child.Function, parent.Function),
		Service:       mergeHookSlice(child.Service, parent.Service),
		Route:         mergeHookSlice(child.Route, parent.Route),
		Cronjob:       mergeHookSlice(child.Cronjob, parent.Cronjob),
		Secret:        mergeHookSlice(child.Secret, parent.Secret),
		DockerBuild:   mergeHookSlice(child.DockerBuild, parent.DockerBuild),
		Observability:  mergeHookSlice(child.Observability, parent.Observability),
		Port:           mergeHookSlice(child.Port, parent.Port),
		NetworkPolicy:  mergeHookSlice(child.NetworkPolicy, parent.NetworkPolicy),
	}
}

// mergeHookSlice merges child and parent hooks for a single hook type.
// Child hooks are prepended before parent hooks (child hooks are higher priority
// in the waterfall evaluation). If both have catch-alls (no 'when' condition),
// only the child's catch-all is kept.
func mergeHookSlice(child, parent []internal.InternalHook) []internal.InternalHook {
	if len(child) == 0 {
		return parent
	}
	if len(parent) == 0 {
		return child
	}

	// Check if child has a catch-all (hook without 'when')
	childHasCatchAll := false
	for _, h := range child {
		if h.When == "" {
			childHasCatchAll = true
			break
		}
	}

	// Build merged result: child hooks first, then parent hooks
	result := make([]internal.InternalHook, 0, len(child)+len(parent))
	result = append(result, child...)

	// Add parent hooks, but drop parent's catch-all if child has one
	for _, h := range parent {
		if childHasCatchAll && h.When == "" {
			// Skip parent catch-all -- child's catch-all shadows it
			continue
		}
		result = append(result, h)
	}

	return result
}
