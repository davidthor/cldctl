package graph

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/architect-io/arcctl/pkg/schema/component"
)

// Builder constructs a dependency graph from component specifications.
type Builder struct {
	graph *Graph
}

// NewBuilder creates a new graph builder.
func NewBuilder(environment, datacenter string) *Builder {
	return &Builder{
		graph: NewGraph(environment, datacenter),
	}
}

// AddComponent adds a component's resources to the graph.
// The componentName is provided externally since component specs no longer contain names.
func (b *Builder) AddComponent(componentName string, comp component.Component) error {
	// Get the component's base directory for resolving relative paths
	// This is crucial for OCI-pulled components where build contexts need to be
	// resolved relative to the extracted artifact location
	compDir := filepath.Dir(comp.SourcePath())

	// Add databases
	for _, db := range comp.Databases() {
		node := NewNode(NodeTypeDatabase, componentName, db.Name())
		// Pass the type in the same format as the component schema (e.g., "postgres:^16")
		typeStr := db.Type()
		if db.Version() != "" {
			typeStr = typeStr + ":" + db.Version()
		}
		node.SetInput("type", typeStr)

		// Add migration node if migrations defined
		if db.Migrations() != nil {
			migNode := NewNode(NodeTypeTask, componentName, db.Name()+"-migration")
			migNode.SetInput("database", db.Name())
			if db.Migrations().Image() != "" {
				migNode.SetInput("image", db.Migrations().Image())
			}
			migNode.SetInput("command", db.Migrations().Command())
			migNode.SetInput("environment", db.Migrations().Environment())

			// Migration depends on database
			migNode.AddDependency(node.ID)
			node.AddDependent(migNode.ID)

			// If migrations have a build, add docker build node
			if db.Migrations().Build() != nil {
				buildNode := NewNode(NodeTypeDockerBuild, componentName, db.Name()+"-migration-build")
				buildNode.SetInput("context", resolveBuildContext(compDir, db.Migrations().Build().Context()))
				buildNode.SetInput("dockerfile", resolveBuildContext(compDir, db.Migrations().Build().Dockerfile()))
				buildNode.SetInput("args", db.Migrations().Build().Args())

				migNode.AddDependency(buildNode.ID)
				buildNode.AddDependent(migNode.ID)

				_ = b.graph.AddNode(buildNode)
			}

			_ = b.graph.AddNode(migNode)
		}

		_ = b.graph.AddNode(node)
	}

	// Add top-level builds
	for _, build := range comp.Builds() {
		buildNode := NewNode(NodeTypeDockerBuild, componentName, build.Name())
		buildNode.SetInput("context", resolveBuildContext(compDir, build.Context()))
		buildNode.SetInput("dockerfile", resolveBuildContext(compDir, build.Dockerfile()))
		buildNode.SetInput("target", build.Target())
		buildNode.SetInput("args", build.Args())

		_ = b.graph.AddNode(buildNode)
	}

	// Add buckets
	for _, bucket := range comp.Buckets() {
		node := NewNode(NodeTypeBucket, componentName, bucket.Name())
		node.SetInput("type", bucket.Type())
		node.SetInput("versioning", bucket.Versioning())
		node.SetInput("public", bucket.Public())

		_ = b.graph.AddNode(node)
	}

	// Add deployments
	for _, deploy := range comp.Deployments() {
		node := NewNode(NodeTypeDeployment, componentName, deploy.Name())

		if deploy.Image() != "" {
			node.SetInput("image", deploy.Image())
		}
		if deploy.Runtime() != nil {
			rt := deploy.Runtime()
			runtimeMap := map[string]interface{}{
				"language": rt.Language(),
			}
			if rt.OS() != "" {
				runtimeMap["os"] = rt.OS()
			}
			if rt.Arch() != "" {
				runtimeMap["arch"] = rt.Arch()
			}
			if len(rt.Packages()) > 0 {
				runtimeMap["packages"] = rt.Packages()
			}
			if len(rt.Setup()) > 0 {
				runtimeMap["setup"] = rt.Setup()
			}
			node.SetInput("runtime", runtimeMap)
		}
		node.SetInput("command", deploy.Command())
		node.SetInput("entrypoint", deploy.Entrypoint())
		node.SetInput("environment", deploy.Environment())
		node.SetInput("cpu", deploy.CPU())
		node.SetInput("memory", deploy.Memory())
		node.SetInput("replicas", deploy.Replicas())
		node.SetInput("liveness_probe", deploy.LivenessProbe())

		// Set working directory: explicit value or default to component directory
		if deploy.WorkingDirectory() != "" {
			node.SetInput("workingDirectory", resolveBuildContext(compDir, deploy.WorkingDirectory()))
		} else {
			node.SetInput("workingDirectory", compDir)
		}

		// Parse environment for dependencies (deferred until all nodes are added)
		_ = b.graph.AddNode(node)
	}

	// Add functions
	for _, fn := range comp.Functions() {
		node := NewNode(NodeTypeFunction, componentName, fn.Name())

		// Set common fields
		node.SetInput("environment", fn.Environment())
		node.SetInput("cpu", fn.CPU())
		node.SetInput("memory", fn.Memory())
		node.SetInput("timeout", fn.Timeout())
		node.SetInput("port", fn.Port())

		// Handle discriminated union
		if fn.IsSourceBased() {
			src := fn.Src()
			// Resolve srcPath relative to component directory so processes run
			// in the correct location (important for OCI-pulled components too)
			node.SetInput("srcPath", resolveBuildContext(compDir, src.Path()))
			node.SetInput("language", src.Language())
			node.SetInput("runtime", src.Runtime())
			node.SetInput("framework", src.Framework())
			node.SetInput("install", src.Install())
			node.SetInput("dev", src.Dev())
			node.SetInput("build", src.Build())
			node.SetInput("start", src.Start())
			node.SetInput("handler", src.Handler())
			node.SetInput("entry", src.Entry())
		} else if fn.IsContainerBased() {
			container := fn.Container()
			if container.Image() != "" {
				node.SetInput("image", container.Image())
			}
			// If has build, add docker build node
			if container.Build() != nil {
				buildNode := NewNode(NodeTypeDockerBuild, componentName, fn.Name()+"-build")
				buildNode.SetInput("context", resolveBuildContext(compDir, container.Build().Context()))
				buildNode.SetInput("dockerfile", resolveBuildContext(compDir, container.Build().Dockerfile()))
				buildNode.SetInput("args", container.Build().Args())

				node.AddDependency(buildNode.ID)
				buildNode.AddDependent(node.ID)

				_ = b.graph.AddNode(buildNode)
			}
		}

		// Parse environment for dependencies (deferred until all nodes are added)
		_ = b.graph.AddNode(node)
	}

	// Add services (for deployments only - functions don't need services)
	// Note: Services do NOT depend on deployments - they can be created in parallel.
	// In Kubernetes and similar platforms, a Service is a stable networking abstraction
	// that routes to pods matching a selector. The pods don't need to exist yet.
	for _, svc := range comp.Services() {
		node := NewNode(NodeTypeService, componentName, svc.Name())
		node.SetInput("port", svc.Port())
		node.SetInput("protocol", svc.Protocol())

		// Record target info for the service hook (but no dependency)
		if svc.Deployment() != "" {
			node.SetInput("target", svc.Deployment())
			node.SetInput("targetType", "deployment")
		}

		_ = b.graph.AddNode(node)
	}

	// Add routes
	// Note: Routes do NOT depend on services/functions - they can be created in parallel.
	// Routes are ingress configuration that can exist before backends are ready.
	// This also avoids cycles when workloads reference their own route URLs in env vars.
	for _, route := range comp.Routes() {
		node := NewNode(NodeTypeRoute, componentName, route.Name())
		node.SetInput("type", route.Type())
		node.SetInput("internal", route.Internal())
		node.SetInput("rules", route.Rules())

		// Record target info for the route hook (but no dependency)
		if route.Service() != "" {
			node.SetInput("target", route.Service())
			node.SetInput("targetType", "service")
		} else if route.Function() != "" {
			node.SetInput("target", route.Function())
			node.SetInput("targetType", "function")
		}

		// Check rules form for target info
		for _, rule := range route.Rules() {
			for _, backend := range rule.BackendRefs() {
				if backend.Service() != "" {
					node.SetInput("target", backend.Service())
					node.SetInput("targetType", "service")
				}
				if backend.Function() != "" {
					node.SetInput("target", backend.Function())
					node.SetInput("targetType", "function")
				}
			}
		}

		_ = b.graph.AddNode(node)
	}

	// Add cronjobs
	for _, cron := range comp.Cronjobs() {
		node := NewNode(NodeTypeCronjob, componentName, cron.Name())

		if cron.Image() != "" {
			node.SetInput("image", cron.Image())
		}
		node.SetInput("schedule", cron.Schedule())
		node.SetInput("command", cron.Command())
		node.SetInput("environment", cron.Environment())
		node.SetInput("cpu", cron.CPU())
		node.SetInput("memory", cron.Memory())

		// If has build, add docker build node
		if cron.Build() != nil {
			buildNode := NewNode(NodeTypeDockerBuild, componentName, cron.Name()+"-build")
			buildNode.SetInput("context", resolveBuildContext(compDir, cron.Build().Context()))
			buildNode.SetInput("dockerfile", resolveBuildContext(compDir, cron.Build().Dockerfile()))
			buildNode.SetInput("args", cron.Build().Args())

			node.AddDependency(buildNode.ID)
			buildNode.AddDependent(node.ID)

			_ = b.graph.AddNode(buildNode)
		}

		_ = b.graph.AddNode(node)
	}

	// Second pass: Parse environment variables and other expression-capable fields for dependencies.
	// This must happen AFTER all nodes are added so we can set up bidirectional relationships.
	for _, deploy := range comp.Deployments() {
		nodeID := fmt.Sprintf("%s/%s/%s", componentName, NodeTypeDeployment, deploy.Name())
		node := b.graph.GetNode(nodeID)
		if node == nil {
			continue
		}
		for _, value := range deploy.Environment() {
			b.addEnvDependencies(componentName, node, value)
		}
		// Scan image field for expressions like ${{ builds.api.image }}
		if deploy.Image() != "" {
			b.addEnvDependencies(componentName, node, deploy.Image())
		}
	}

	for _, fn := range comp.Functions() {
		nodeID := fmt.Sprintf("%s/%s/%s", componentName, NodeTypeFunction, fn.Name())
		node := b.graph.GetNode(nodeID)
		if node == nil {
			continue
		}
		for _, value := range fn.Environment() {
			b.addEnvDependencies(componentName, node, value)
		}
	}

	for _, cron := range comp.Cronjobs() {
		nodeID := fmt.Sprintf("%s/%s/%s", componentName, NodeTypeCronjob, cron.Name())
		node := b.graph.GetNode(nodeID)
		if node == nil {
			continue
		}
		for _, value := range cron.Environment() {
			b.addEnvDependencies(componentName, node, value)
		}
	}

	return nil
}

// addEnvDependencies parses an environment variable value and adds dependencies
// with proper bidirectional relationships.
func (b *Builder) addEnvDependencies(componentName string, node *Node, value string) {
	deps := extractDependencies(value)
	for _, dep := range deps {
		depNodeID := b.resolveDepReference(componentName, dep)
		if depNodeID == "" {
			continue
		}
		// Only add dependency if target node exists
		depNode := b.graph.GetNode(depNodeID)
		if depNode == nil {
			continue
		}
		// Add bidirectional relationship
		node.AddDependency(depNodeID)
		depNode.AddDependent(node.ID)

		// Also depend on any task nodes that depend on this resource.
		// This ensures tasks (e.g., database migrations) complete before
		// workloads that consume the same resource start.
		for _, dependentID := range depNode.DependedOnBy {
			taskNode := b.graph.GetNode(dependentID)
			if taskNode != nil && taskNode.Type == NodeTypeTask {
				node.AddDependency(dependentID)
				taskNode.AddDependent(node.ID)
			}
		}
	}
}

// Build returns the completed graph.
func (b *Builder) Build() *Graph {
	return b.graph
}

// extractDependencies finds ${{ }} references in a string
func extractDependencies(value string) []string {
	re := regexp.MustCompile(`\$\{\{\s*([^}]+)\s*\}\}`)
	matches := re.FindAllStringSubmatch(value, -1)

	var deps []string
	for _, match := range matches {
		if len(match) > 1 {
			deps = append(deps, strings.TrimSpace(match[1]))
		}
	}
	return deps
}

// resolveDepReference converts a reference like "databases.main.url" to a node ID
func (b *Builder) resolveDepReference(componentName, ref string) string {
	parts := strings.Split(ref, ".")
	if len(parts) < 2 {
		return ""
	}

	resourceType := parts[0]
	resourceName := parts[1]

	var nodeType NodeType
	switch resourceType {
	case "databases":
		nodeType = NodeTypeDatabase
	case "buckets":
		nodeType = NodeTypeBucket
	case "services":
		nodeType = NodeTypeService
	case "routes":
		nodeType = NodeTypeRoute
	case "functions":
		nodeType = NodeTypeFunction
	case "builds":
		nodeType = NodeTypeDockerBuild
	default:
		return ""
	}

	return fmt.Sprintf("%s/%s/%s", componentName, nodeType, resourceName)
}

// resolveBuildContext resolves a build context path to an absolute path.
// This is important for OCI-pulled components where relative paths need to be
// resolved relative to the extracted artifact location, not the current working directory.
func resolveBuildContext(compDir, path string) string {
	if path == "" {
		return ""
	}

	// If already absolute, return as-is
	if filepath.IsAbs(path) {
		return path
	}

	// Resolve relative path against component directory
	return filepath.Join(compDir, path)
}
