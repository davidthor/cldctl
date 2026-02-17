package ciworkflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/davidthor/cldctl/pkg/graph"
)

// BuildJobs converts a topologically sorted graph into a list of CI jobs.
// In component mode, job IDs are "<type>-<name>".
// In environment mode, job IDs are "<component>--<type>-<name>" to avoid collisions.
func BuildJobs(g *graph.Graph, mode WorkflowMode, components []ComponentRef) ([]Job, error) {
	sorted, err := g.TopologicalSort()
	if err != nil {
		return nil, fmt.Errorf("failed to sort graph: %w", err)
	}

	// Build component image lookup
	compImages := make(map[string]string, len(components))
	compVars := make(map[string]map[string]string, len(components))
	for _, comp := range components {
		if comp.Image != "" {
			compImages[comp.Name] = comp.Image
		}
		compVars[comp.Name] = comp.Variables
	}

	// Build a mapping from graph node ID to job ID
	nodeToJob := make(map[string]string, len(sorted))
	for _, node := range sorted {
		nodeToJob[node.ID] = makeJobID(node, mode)
	}

	var jobs []Job
	for _, node := range sorted {
		jobID := nodeToJob[node.ID]

		// Build dependency list (map graph edges to job IDs)
		var dependsOn []string
		depsSeen := make(map[string]bool)
		for _, depNodeID := range node.DependsOn {
			if depJobID, ok := nodeToJob[depNodeID]; ok && !depsSeen[depJobID] {
				dependsOn = append(dependsOn, depJobID)
				depsSeen[depJobID] = true
			}
		}
		sort.Strings(dependsOn)

		// Build var flags for this component
		var varFlags []string
		if vars, ok := compVars[node.Component]; ok {
			varFlags = buildVarFlags(vars)
		}

		// Build apply command
		imageRef := compImages[node.Component]
		if imageRef == "" {
			imageRef = "$COMPONENT_IMAGE"
		}
		applyCmd := fmt.Sprintf("cldctl apply %s %s/%s -e $ENVIRONMENT -d $DATACENTER",
			imageRef, node.Type, node.Name)
		for _, vf := range varFlags {
			applyCmd += fmt.Sprintf(" --var %s", vf)
		}

		job := Job{
			ID:            jobID,
			Name:          makeJobName(node, mode),
			Component:     node.Component,
			NodeType:      string(node.Type),
			NodeName:      node.Name,
			DependsOn:     dependsOn,
			VarFlags:      varFlags,
			NeedsCheckout: node.Type == graph.NodeTypeDockerBuild,
			ApplyCommand:  applyCmd,
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}

// BuildTeardownJobs creates teardown jobs for environment mode.
// Components are destroyed in reverse dependency order.
func BuildTeardownJobs(components []ComponentRef, dependencies map[string][]string) []Job {
	// Reverse topological sort of components based on their dependencies
	ordered := topSortComponents(components, dependencies)

	// Reverse: destroy dependents before dependencies
	for i, j := 0, len(ordered)-1; i < j; i, j = i+1, j-1 {
		ordered[i], ordered[j] = ordered[j], ordered[i]
	}

	var jobs []Job

	// Destroy components job
	destroyJob := Job{
		ID:   "destroy-components",
		Name: "Destroy Components",
	}
	for _, comp := range ordered {
		destroyJob.Steps = append(destroyJob.Steps, Step{
			Name: fmt.Sprintf("Destroy %s", comp.Name),
			Run:  fmt.Sprintf("cldctl destroy component %s -e $ENVIRONMENT -d $DATACENTER --force", comp.Name),
		})
	}
	jobs = append(jobs, destroyJob)

	// Destroy environment job
	jobs = append(jobs, Job{
		ID:        "destroy-environment",
		Name:      "Destroy Environment",
		DependsOn: []string{"destroy-components"},
		Steps: []Step{
			{
				Name: "Destroy environment",
				Run:  "cldctl destroy environment $ENVIRONMENT -d $DATACENTER",
			},
		},
	})

	return jobs
}

// makeJobID creates a unique job ID from a graph node.
func makeJobID(node *graph.Node, mode WorkflowMode) string {
	base := fmt.Sprintf("%s-%s", node.Type, node.Name)
	if mode == ModeEnvironment {
		return fmt.Sprintf("%s--%s", sanitizeJobID(node.Component), base)
	}
	return base
}

// makeJobName creates a human-readable job name from a graph node.
func makeJobName(node *graph.Node, mode WorkflowMode) string {
	base := fmt.Sprintf("Apply %s/%s", node.Type, node.Name)
	if mode == ModeEnvironment {
		return fmt.Sprintf("%s %s", node.Component, base[len("Apply "):])
	}
	return base
}

// sanitizeJobID makes a component name safe for use in job IDs.
func sanitizeJobID(name string) string {
	r := strings.NewReplacer("/", "-", ".", "-", " ", "-")
	return r.Replace(name)
}

// buildVarFlags creates --var flag values from a variable map.
// Static values are inlined; env var references use $ENV_VAR syntax.
func buildVarFlags(vars map[string]string) []string {
	if len(vars) == 0 {
		return nil
	}

	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	flags := make([]string, 0, len(vars))
	for _, k := range keys {
		v := vars[k]
		flags = append(flags, fmt.Sprintf("%s=%s", k, v))
	}
	return flags
}

// topSortComponents sorts components in dependency order.
func topSortComponents(components []ComponentRef, dependencies map[string][]string) []ComponentRef {
	// Build name-to-component map
	byName := make(map[string]ComponentRef, len(components))
	for _, comp := range components {
		byName[comp.Name] = comp
	}

	// Compute in-degree
	inDegree := make(map[string]int)
	for _, comp := range components {
		if _, ok := inDegree[comp.Name]; !ok {
			inDegree[comp.Name] = 0
		}
		for _, dep := range dependencies[comp.Name] {
			if _, exists := byName[dep]; exists {
				inDegree[comp.Name]++
			}
		}
	}

	// Kahn's algorithm
	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue)

	// Build reverse edges (dependency -> dependents)
	reverseEdges := make(map[string][]string)
	for name, deps := range dependencies {
		for _, dep := range deps {
			if _, exists := byName[dep]; exists {
				reverseEdges[dep] = append(reverseEdges[dep], name)
			}
		}
	}

	var result []ComponentRef
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		result = append(result, byName[name])

		for _, dependent := range reverseEdges[name] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
				sort.Strings(queue)
			}
		}
	}

	// If not all components were processed (cycle), append remaining
	if len(result) < len(components) {
		processed := make(map[string]bool, len(result))
		for _, c := range result {
			processed[c.Name] = true
		}
		for _, comp := range components {
			if !processed[comp.Name] {
				result = append(result, comp)
			}
		}
	}

	return result
}
