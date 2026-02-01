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
		node.SetInput("databaseType", db.Type())
		node.SetInput("databaseVersion", extractVersion(db.Type()))

		// Add migration node if migrations defined
		if db.Migrations() != nil {
			migNode := NewNode(NodeTypeMigration, componentName, db.Name()+"-migration")
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
		node.SetInput("command", deploy.Command())
		node.SetInput("entrypoint", deploy.Entrypoint())
		node.SetInput("environment", deploy.Environment())
		node.SetInput("cpu", deploy.CPU())
		node.SetInput("memory", deploy.Memory())
		node.SetInput("replicas", deploy.Replicas())

		// If has build, add docker build node
		if deploy.Build() != nil {
			buildNode := NewNode(NodeTypeDockerBuild, componentName, deploy.Name()+"-build")
			buildNode.SetInput("context", resolveBuildContext(compDir, deploy.Build().Context()))
			buildNode.SetInput("dockerfile", resolveBuildContext(compDir, deploy.Build().Dockerfile()))
			buildNode.SetInput("target", deploy.Build().Target())
			buildNode.SetInput("args", deploy.Build().Args())

			node.AddDependency(buildNode.ID)
			buildNode.AddDependent(node.ID)

			_ = b.graph.AddNode(buildNode)
		}

		// Parse environment for dependencies
		for _, value := range deploy.Environment() {
			deps := extractDependencies(value)
			for _, dep := range deps {
				depNodeID := b.resolveDepReference(componentName, dep)
				if depNodeID != "" {
					node.AddDependency(depNodeID)
				}
			}
		}

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
			node.SetInput("srcPath", src.Path())
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

		// Parse environment for dependencies
		for _, value := range fn.Environment() {
			deps := extractDependencies(value)
			for _, dep := range deps {
				depNodeID := b.resolveDepReference(componentName, dep)
				if depNodeID != "" {
					node.AddDependency(depNodeID)
				}
			}
		}

		_ = b.graph.AddNode(node)
	}

	// Add services (for deployments only - functions don't need services)
	for _, svc := range comp.Services() {
		node := NewNode(NodeTypeService, componentName, svc.Name())
		node.SetInput("port", svc.Port())
		node.SetInput("protocol", svc.Protocol())

		// Service depends on its deployment
		if svc.Deployment() != "" {
			targetID := fmt.Sprintf("%s/deployment/%s", componentName, svc.Deployment())
			node.AddDependency(targetID)
			if target := b.graph.GetNode(targetID); target != nil {
				target.AddDependent(node.ID)
			}
			node.SetInput("target", svc.Deployment())
			node.SetInput("targetType", "deployment")
		}

		_ = b.graph.AddNode(node)
	}

	// Add routes
	for _, route := range comp.Routes() {
		node := NewNode(NodeTypeRoute, componentName, route.Name())
		node.SetInput("type", route.Type())
		node.SetInput("internal", route.Internal())
		node.SetInput("rules", route.Rules())

		// Routes can depend on services or functions
		// Check simplified form first
		if route.Service() != "" {
			targetID := fmt.Sprintf("%s/service/%s", componentName, route.Service())
			node.AddDependency(targetID)
			if target := b.graph.GetNode(targetID); target != nil {
				target.AddDependent(node.ID)
			}
			node.SetInput("target", route.Service())
			node.SetInput("targetType", "service")
		} else if route.Function() != "" {
			targetID := fmt.Sprintf("%s/function/%s", componentName, route.Function())
			node.AddDependency(targetID)
			if target := b.graph.GetNode(targetID); target != nil {
				target.AddDependent(node.ID)
			}
			node.SetInput("target", route.Function())
			node.SetInput("targetType", "function")
		}

		// Check rules form
		for _, rule := range route.Rules() {
			for _, backend := range rule.BackendRefs() {
				if backend.Service() != "" {
					targetID := fmt.Sprintf("%s/service/%s", componentName, backend.Service())
					node.AddDependency(targetID)
					if target := b.graph.GetNode(targetID); target != nil {
						target.AddDependent(node.ID)
					}
				}
				if backend.Function() != "" {
					targetID := fmt.Sprintf("%s/function/%s", componentName, backend.Function())
					node.AddDependency(targetID)
					if target := b.graph.GetNode(targetID); target != nil {
						target.AddDependent(node.ID)
					}
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

		// Parse environment for dependencies
		for _, value := range cron.Environment() {
			deps := extractDependencies(value)
			for _, dep := range deps {
				depNodeID := b.resolveDepReference(componentName, dep)
				if depNodeID != "" {
					node.AddDependency(depNodeID)
				}
			}
		}

		_ = b.graph.AddNode(node)
	}

	return nil
}

// Build returns the completed graph.
func (b *Builder) Build() *Graph {
	return b.graph
}

// extractVersion extracts version from type string like "postgres:^15"
func extractVersion(typeStr string) string {
	parts := strings.Split(typeStr, ":")
	if len(parts) > 1 {
		return strings.TrimPrefix(parts[1], "^")
	}
	return ""
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
