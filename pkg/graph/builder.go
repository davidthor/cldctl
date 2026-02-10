package graph

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/davidthor/cldctl/pkg/schema/component"
)

// ImplicitNodeFilter is a predicate that decides whether an implicit node should
// be created for a given set of node inputs. The builder calls this before
// creating each databaseUser or networkPolicy node. Returning true means "create
// the node"; false means "skip it".
type ImplicitNodeFilter func(inputs map[string]interface{}) bool

// Builder constructs a dependency graph from component specifications.
type Builder struct {
	graph *Graph

	// databaseUserFilter, when non-nil, is called to decide whether a
	// databaseUser node should be created for a specific database+consumer pair.
	// The inputs map contains "type", "database", "consumer", etc.
	// If nil, no databaseUser nodes are created.
	databaseUserFilter ImplicitNodeFilter

	// networkPolicyFilter, when non-nil, is called to decide whether a
	// networkPolicy node should be created for a specific workload+service pair.
	// If nil, no networkPolicy nodes are created.
	networkPolicyFilter ImplicitNodeFilter
}

// NewBuilder creates a new graph builder.
func NewBuilder(environment, datacenter string) *Builder {
	return &Builder{
		graph: NewGraph(environment, datacenter),
	}
}

// EnableImplicitNodes configures which implicit node types the builder should
// generate. Pass true for each hook type that the datacenter defines.
// This is a convenience wrapper that creates pass-all filters when true.
// For fine-grained control over which nodes are created, use
// SetDatabaseUserFilter / SetNetworkPolicyFilter instead.
func (b *Builder) EnableImplicitNodes(databaseUser, networkPolicy bool) {
	if databaseUser {
		b.databaseUserFilter = func(_ map[string]interface{}) bool { return true }
	}
	if networkPolicy {
		b.networkPolicyFilter = func(_ map[string]interface{}) bool { return true }
	}
}

// SetDatabaseUserFilter sets a filter that determines whether a databaseUser
// implicit node should be created for a given database+consumer pair. The filter
// receives the prospective node's inputs (including "type" from the database).
// If the filter returns false, no node is created and the workload depends
// directly on the database.
func (b *Builder) SetDatabaseUserFilter(fn ImplicitNodeFilter) {
	b.databaseUserFilter = fn
}

// SetNetworkPolicyFilter sets a filter that determines whether a networkPolicy
// implicit node should be created for a given workload+service pair.
func (b *Builder) SetNetworkPolicyFilter(fn ImplicitNodeFilter) {
	b.networkPolicyFilter = fn
}

// AddComponent adds a component's resources to the graph.
// The componentName is provided externally since component specs no longer contain names.
func (b *Builder) AddComponent(componentName string, comp component.Component) error {
	// Record inter-component dependencies.
	// Required (non-optional) dependencies create hard edges for destroy protection
	// and execution ordering. Optional dependencies are tracked separately so the
	// expression resolver can silently return empty strings for them.
	// DependencyTargets maps dep alias → target component name for expression resolution.
	if deps := comp.Dependencies(); len(deps) > 0 {
		var depNames []string
		for _, dep := range deps {
			// Record the dep alias → target component name mapping.
			// dep.Component() is the OCI reference (e.g., "questra/clerk:latest");
			// strip the tag to get the component name used in the environment.
			targetComp := dep.Component()
			if idx := strings.LastIndex(targetComp, ":"); idx > 0 {
				targetComp = targetComp[:idx]
			}
			if b.graph.DependencyTargets == nil {
				b.graph.DependencyTargets = make(map[string]map[string]string)
			}
			if b.graph.DependencyTargets[componentName] == nil {
				b.graph.DependencyTargets[componentName] = make(map[string]string)
			}
			b.graph.DependencyTargets[componentName][dep.Name()] = targetComp

			if dep.Optional() {
				if b.graph.OptionalDependencies == nil {
					b.graph.OptionalDependencies = make(map[string]map[string]bool)
				}
				if b.graph.OptionalDependencies[componentName] == nil {
					b.graph.OptionalDependencies[componentName] = make(map[string]bool)
				}
				b.graph.OptionalDependencies[componentName][dep.Name()] = true
			} else {
				depNames = append(depNames, dep.Name())
			}
		}
		if len(depNames) > 0 {
			if b.graph.ComponentDependencies == nil {
				b.graph.ComponentDependencies = make(map[string][]string)
			}
			b.graph.ComponentDependencies[componentName] = depNames
		}
	}

	// Record component-level output expressions so the executor can resolve
	// them after all resources are deployed. This is essential for pass-through
	// components (e.g., credential wrappers) that have outputs but no resources.
	if outputs := comp.Outputs(); len(outputs) > 0 {
		if b.graph.ComponentOutputExprs == nil {
			b.graph.ComponentOutputExprs = make(map[string]map[string]string)
		}
		outMap := make(map[string]string, len(outputs))
		for _, out := range outputs {
			outMap[out.Name()] = out.Value()
		}
		b.graph.ComponentOutputExprs[componentName] = outMap
	}

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
			if db.Migrations().Runtime() != nil {
				rt := db.Migrations().Runtime()
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
				migNode.SetInput("runtime", runtimeMap)
			}
			migNode.SetInput("command", db.Migrations().Command())
			migNode.SetInput("environment", db.Migrations().Environment())

			// Set working directory: explicit value or default to component directory
			if db.Migrations().WorkingDirectory() != "" {
				migNode.SetInput("workingDirectory", resolveBuildContext(compDir, db.Migrations().WorkingDirectory()))
			} else {
				migNode.SetInput("workingDirectory", compDir)
			}

			// Migration depends on database
			migNode.AddDependency(node.ID)
			node.AddDependent(migNode.ID)

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

	// Add ports (no dependencies - they are depended on by workloads/services via expressions)
	for _, p := range comp.Ports() {
		node := NewNode(NodeTypePort, componentName, p.Name())
		node.SetInput("description", p.Description())

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
	// Routes are external routing configuration that can exist before backends are ready.
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

	// Add observability node if component has observability configured.
	// All workload nodes (deployments, functions, cronjobs) will depend on this node
	// so that OTel configuration is resolved before workloads start.
	var obsNodeID string
	if comp.Observability() != nil {
		obsNode := NewNode(NodeTypeObservability, componentName, "observability")
		obsNode.SetInput("inject", comp.Observability().Inject())
		if attrs := comp.Observability().Attributes(); len(attrs) > 0 {
			obsNode.SetInput("attributes", attrs)
		}
		_ = b.graph.AddNode(obsNode)
		obsNodeID = obsNode.ID
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
		// Make workload depend on observability node so OTel config is resolved first
		if obsNodeID != "" {
			obsNode := b.graph.GetNode(obsNodeID)
			if obsNode != nil {
				node.AddDependency(obsNodeID)
				obsNode.AddDependent(node.ID)
			}
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
		// Make workload depend on observability node so OTel config is resolved first
		if obsNodeID != "" {
			obsNode := b.graph.GetNode(obsNodeID)
			if obsNode != nil {
				node.AddDependency(obsNodeID)
				obsNode.AddDependent(node.ID)
			}
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
		// Make workload depend on observability node so OTel config is resolved first
		if obsNodeID != "" {
			obsNode := b.graph.GetNode(obsNodeID)
			if obsNode != nil {
				node.AddDependency(obsNodeID)
				obsNode.AddDependent(node.ID)
			}
		}
	}

	// Scan service port fields for expression dependencies (e.g., ${{ ports.api.port }})
	for _, svc := range comp.Services() {
		nodeID := fmt.Sprintf("%s/%s/%s", componentName, NodeTypeService, svc.Name())
		node := b.graph.GetNode(nodeID)
		if node == nil {
			continue
		}
		if svc.Port() != "" {
			b.addEnvDependencies(componentName, node, svc.Port())
		}
	}

	return nil
}

// addEnvDependencies parses an environment variable value and adds dependencies
// with proper bidirectional relationships.
// When a workload references a database, a databaseUser node is interposed.
// When a workload references a service, a networkPolicy leaf node is created.
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

		// Implicit databaseUser interposition: when a workload references a database
		// and the datacenter defines a matching databaseUser hook, inject a
		// databaseUser node between the database and the consumer. The filter
		// checks hook when-clauses against the database type so that non-matching
		// databases (e.g. redis when only a postgres hook exists) connect directly.
		if depNode.Type == NodeTypeDatabase && IsWorkloadType(node.Type) && b.shouldCreateDatabaseUser(depNode, node) {
			dbUserNode := b.getOrCreateDatabaseUserNode(componentName, depNode, node)
			// Consumer depends on databaseUser instead of database directly
			node.AddDependency(dbUserNode.ID)
			dbUserNode.AddDependent(node.ID)

			// Also depend on any task nodes that depend on the database.
			for _, dependentID := range depNode.DependedOnBy {
				taskNode := b.graph.GetNode(dependentID)
				if taskNode != nil && taskNode.Type == NodeTypeTask {
					node.AddDependency(dependentID)
					taskNode.AddDependent(node.ID)
				}
			}
			continue
		}

		// Add bidirectional relationship
		node.AddDependency(depNodeID)
		depNode.AddDependent(node.ID)

		// Implicit networkPolicy creation: when a workload references a service
		// and the datacenter defines a matching networkPolicy hook, create a
		// networkPolicy leaf node capturing the from/to relationship.
		if depNode.Type == NodeTypeService && IsWorkloadType(node.Type) && b.shouldCreateNetworkPolicy(node, depNode) {
			b.getOrCreateNetworkPolicyNode(componentName, node, depNode)
		}

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

// shouldCreateDatabaseUser checks whether a databaseUser implicit node should be
// created for the given database→consumer pair. It builds the prospective node
// inputs and passes them to the databaseUserFilter. Returns false when no filter
// is set (no hooks) or when the filter rejects the inputs (no matching hook).
func (b *Builder) shouldCreateDatabaseUser(dbNode, consumerNode *Node) bool {
	if b.databaseUserFilter == nil {
		return false
	}
	// Build the same inputs that getOrCreateDatabaseUserNode would set,
	// so the filter can evaluate when-clauses against the database type.
	inputs := map[string]interface{}{
		"database":     dbNode.Name,
		"type":         dbNode.Inputs["type"],
		"consumer":     consumerNode.Name,
		"consumerType": string(consumerNode.Type),
	}
	return b.databaseUserFilter(inputs)
}

// shouldCreateNetworkPolicy checks whether a networkPolicy implicit node should
// be created for the given workload→service pair.
func (b *Builder) shouldCreateNetworkPolicy(fromNode, toServiceNode *Node) bool {
	if b.networkPolicyFilter == nil {
		return false
	}
	inputs := map[string]interface{}{
		"from":     fromNode.Name,
		"fromType": string(fromNode.Type),
		"to":       toServiceNode.Name,
		"toType":   string(toServiceNode.Type),
	}
	return b.networkPolicyFilter(inputs)
}

// getOrCreateDatabaseUserNode returns (or creates) a databaseUser node for the given
// database-consumer pair. The databaseUser node depends on the database. The datacenter's
// databaseUser hook provisions per-consumer credentials for each node.
// Only called when shouldCreateDatabaseUser returns true.
// Naming convention: {dbName}--{consumerName}
func (b *Builder) getOrCreateDatabaseUserNode(componentName string, dbNode, consumerNode *Node) *Node {
	dbUserName := dbNode.Name + "--" + consumerNode.Name
	dbUserID := fmt.Sprintf("%s/%s/%s", componentName, NodeTypeDatabaseUser, dbUserName)

	existing := b.graph.GetNode(dbUserID)
	if existing != nil {
		return existing
	}

	dbUserNode := NewNode(NodeTypeDatabaseUser, componentName, dbUserName)
	dbUserNode.SetInput("database", dbNode.Name)
	dbUserNode.SetInput("type", dbNode.Inputs["type"])
	dbUserNode.SetInput("consumer", consumerNode.Name)
	dbUserNode.SetInput("consumerType", string(consumerNode.Type))
	dbUserNode.SetInput("component", componentName)

	// databaseUser depends on the database
	dbUserNode.AddDependency(dbNode.ID)
	dbNode.AddDependent(dbUserNode.ID)

	_ = b.graph.AddNode(dbUserNode)
	return dbUserNode
}

// getOrCreateNetworkPolicyNode returns (or creates) a networkPolicy leaf node for the
// given workload-to-service relationship. The networkPolicy depends on both the workload
// and the service. Nothing depends on it — it is a fire-and-forget side-effect node.
// Naming convention: {fromWorkload}--{toService}
func (b *Builder) getOrCreateNetworkPolicyNode(componentName string, fromNode, toServiceNode *Node) *Node {
	npName := fromNode.Name + "--" + toServiceNode.Name
	npID := fmt.Sprintf("%s/%s/%s", componentName, NodeTypeNetworkPolicy, npName)

	existing := b.graph.GetNode(npID)
	if existing != nil {
		return existing
	}

	npNode := NewNode(NodeTypeNetworkPolicy, componentName, npName)
	npNode.SetInput("from", fromNode.Name)
	npNode.SetInput("fromType", string(fromNode.Type))
	npNode.SetInput("fromComponent", componentName)
	npNode.SetInput("to", toServiceNode.Name)
	npNode.SetInput("toComponent", toServiceNode.Component)

	// Extract port from the service node inputs
	if port, ok := toServiceNode.Inputs["port"]; ok {
		npNode.SetInput("port", port)
	}

	// networkPolicy depends on both the workload and the service
	npNode.AddDependency(fromNode.ID)
	fromNode.AddDependent(npNode.ID)
	npNode.AddDependency(toServiceNode.ID)
	toServiceNode.AddDependent(npNode.ID)

	_ = b.graph.AddNode(npNode)
	return npNode
}

// InstanceInfo describes a component instance for multi-instance graph building.
type InstanceInfo struct {
	Name   string
	Weight int
}

// AddComponentWithInstances adds a component's resources to the graph in multi-instance mode.
// Per-instance resource types are duplicated for each instance (with instance-qualified IDs).
// Shared resource types create a single node that derives inputs from the newest (first) instance.
// The `distinct` list promotes specific shared resources to per-instance.
func (b *Builder) AddComponentWithInstances(componentName string, comp component.Component, instances []InstanceInfo, distinct []string) error {
	// Build a set of distinct resource patterns for quick lookup
	distinctSet := make(map[string]bool)
	for _, d := range distinct {
		distinctSet[d] = true
	}

	// Helper: check if a resource should be per-instance
	isDistinct := func(nodeType NodeType, resourceName string) bool {
		key := string(nodeType) + "." + resourceName
		return distinctSet[key]
	}

	// Record inter-component dependencies (same as single-instance).
	if deps := comp.Dependencies(); len(deps) > 0 {
		var depNames []string
		for _, dep := range deps {
			targetComp := dep.Component()
			if idx := strings.LastIndex(targetComp, ":"); idx > 0 {
				targetComp = targetComp[:idx]
			}
			if b.graph.DependencyTargets == nil {
				b.graph.DependencyTargets = make(map[string]map[string]string)
			}
			if b.graph.DependencyTargets[componentName] == nil {
				b.graph.DependencyTargets[componentName] = make(map[string]string)
			}
			b.graph.DependencyTargets[componentName][dep.Name()] = targetComp

			if dep.Optional() {
				if b.graph.OptionalDependencies == nil {
					b.graph.OptionalDependencies = make(map[string]map[string]bool)
				}
				if b.graph.OptionalDependencies[componentName] == nil {
					b.graph.OptionalDependencies[componentName] = make(map[string]bool)
				}
				b.graph.OptionalDependencies[componentName][dep.Name()] = true
			} else {
				depNames = append(depNames, dep.Name())
			}
		}
		if len(depNames) > 0 {
			if b.graph.ComponentDependencies == nil {
				b.graph.ComponentDependencies = make(map[string][]string)
			}
			b.graph.ComponentDependencies[componentName] = depNames
		}
	}

	// Record component-level output expressions (same as single-instance).
	if outputs := comp.Outputs(); len(outputs) > 0 {
		if b.graph.ComponentOutputExprs == nil {
			b.graph.ComponentOutputExprs = make(map[string]map[string]string)
		}
		outMap := make(map[string]string, len(outputs))
		for _, out := range outputs {
			outMap[out.Name()] = out.Value()
		}
		b.graph.ComponentOutputExprs[componentName] = outMap
	}

	compDir := filepath.Dir(comp.SourcePath())

	// Convert instances to NodeInstance for shared node metadata
	nodeInstances := make([]NodeInstance, len(instances))
	for i, inst := range instances {
		nodeInstances[i] = NodeInstance(inst)
	}

	// === SHARED RESOURCES ===
	// These are created once, using the first (newest) instance's definition.

	// Add databases (shared by default)
	for _, db := range comp.Databases() {
		if isDistinct(NodeTypeDatabase, db.Name()) {
			// Per-instance: create one per instance
			for _, inst := range instances {
				node := NewInstanceNode(NodeTypeDatabase, componentName, inst.Name, inst.Weight, db.Name())
				typeStr := db.Type()
				if db.Version() != "" {
					typeStr = typeStr + ":" + db.Version()
				}
				node.SetInput("type", typeStr)
				node.Instances = nodeInstances
				_ = b.graph.AddNode(node)
			}
		} else {
			node := NewNode(NodeTypeDatabase, componentName, db.Name())
			typeStr := db.Type()
			if db.Version() != "" {
				typeStr = typeStr + ":" + db.Version()
			}
			node.SetInput("type", typeStr)
			node.Instances = nodeInstances
			_ = b.graph.AddNode(node)

			// Add migration tasks (shared)
			if db.Migrations() != nil {
				migNode := NewNode(NodeTypeTask, componentName, db.Name()+"-migration")
				migNode.SetInput("database", db.Name())
				if db.Migrations().Image() != "" {
					migNode.SetInput("image", db.Migrations().Image())
				}
				migNode.SetInput("command", db.Migrations().Command())
				migNode.SetInput("environment", db.Migrations().Environment())
				if db.Migrations().WorkingDirectory() != "" {
					migNode.SetInput("workingDirectory", resolveBuildContext(compDir, db.Migrations().WorkingDirectory()))
				} else {
					migNode.SetInput("workingDirectory", compDir)
				}
				migNode.AddDependency(node.ID)
				node.AddDependent(migNode.ID)
				migNode.Instances = nodeInstances
				_ = b.graph.AddNode(migNode)
			}
		}
	}

	// Add buckets (shared by default)
	for _, bucket := range comp.Buckets() {
		node := NewNode(NodeTypeBucket, componentName, bucket.Name())
		node.SetInput("type", bucket.Type())
		node.SetInput("versioning", bucket.Versioning())
		node.SetInput("public", bucket.Public())
		node.Instances = nodeInstances
		_ = b.graph.AddNode(node)
	}

	// Add observability (shared)
	var obsNodeID string
	if comp.Observability() != nil {
		obsNode := NewNode(NodeTypeObservability, componentName, "observability")
		obsNode.SetInput("inject", comp.Observability().Inject())
		if attrs := comp.Observability().Attributes(); len(attrs) > 0 {
			obsNode.SetInput("attributes", attrs)
		}
		obsNode.Instances = nodeInstances
		_ = b.graph.AddNode(obsNode)
		obsNodeID = obsNode.ID
	}

	// === PER-INSTANCE RESOURCES ===
	// These are duplicated for each instance.

	for _, inst := range instances {
		// Add builds per instance
		for _, build := range comp.Builds() {
			buildNode := NewInstanceNode(NodeTypeDockerBuild, componentName, inst.Name, inst.Weight, build.Name())
			buildNode.SetInput("context", resolveBuildContext(compDir, build.Context()))
			buildNode.SetInput("dockerfile", resolveBuildContext(compDir, build.Dockerfile()))
			buildNode.SetInput("target", build.Target())
			buildNode.SetInput("args", build.Args())
			_ = b.graph.AddNode(buildNode)
		}

		// Add ports per instance
		for _, p := range comp.Ports() {
			node := NewInstanceNode(NodeTypePort, componentName, inst.Name, inst.Weight, p.Name())
			node.SetInput("description", p.Description())
			_ = b.graph.AddNode(node)
		}

		// Add deployments per instance
		for _, deploy := range comp.Deployments() {
			node := NewInstanceNode(NodeTypeDeployment, componentName, inst.Name, inst.Weight, deploy.Name())
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
			if deploy.WorkingDirectory() != "" {
				node.SetInput("workingDirectory", resolveBuildContext(compDir, deploy.WorkingDirectory()))
			} else {
				node.SetInput("workingDirectory", compDir)
			}
			_ = b.graph.AddNode(node)
		}

		// Add functions per instance
		for _, fn := range comp.Functions() {
			node := NewInstanceNode(NodeTypeFunction, componentName, inst.Name, inst.Weight, fn.Name())
			node.SetInput("environment", fn.Environment())
			node.SetInput("cpu", fn.CPU())
			node.SetInput("memory", fn.Memory())
			node.SetInput("timeout", fn.Timeout())
			node.SetInput("port", fn.Port())
			if fn.IsSourceBased() {
				src := fn.Src()
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
				if container.Build() != nil {
					buildNode := NewInstanceNode(NodeTypeDockerBuild, componentName, inst.Name, inst.Weight, fn.Name()+"-build")
					buildNode.SetInput("context", resolveBuildContext(compDir, container.Build().Context()))
					buildNode.SetInput("dockerfile", resolveBuildContext(compDir, container.Build().Dockerfile()))
					buildNode.SetInput("args", container.Build().Args())
					node.AddDependency(buildNode.ID)
					buildNode.AddDependent(node.ID)
					_ = b.graph.AddNode(buildNode)
				}
			}
			_ = b.graph.AddNode(node)
		}

		// Add services per instance
		for _, svc := range comp.Services() {
			node := NewInstanceNode(NodeTypeService, componentName, inst.Name, inst.Weight, svc.Name())
			node.SetInput("port", svc.Port())
			node.SetInput("protocol", svc.Protocol())
			if svc.Deployment() != "" {
				node.SetInput("target", svc.Deployment())
				node.SetInput("targetType", "deployment")
			}
			_ = b.graph.AddNode(node)
		}

		// Add cronjobs per instance
		for _, cron := range comp.Cronjobs() {
			node := NewInstanceNode(NodeTypeCronjob, componentName, inst.Name, inst.Weight, cron.Name())
			if cron.Image() != "" {
				node.SetInput("image", cron.Image())
			}
			node.SetInput("schedule", cron.Schedule())
			node.SetInput("command", cron.Command())
			node.SetInput("environment", cron.Environment())
			node.SetInput("cpu", cron.CPU())
			node.SetInput("memory", cron.Memory())
			if cron.Build() != nil {
				buildNode := NewInstanceNode(NodeTypeDockerBuild, componentName, inst.Name, inst.Weight, cron.Name()+"-build")
				buildNode.SetInput("context", resolveBuildContext(compDir, cron.Build().Context()))
				buildNode.SetInput("dockerfile", resolveBuildContext(compDir, cron.Build().Dockerfile()))
				buildNode.SetInput("args", cron.Build().Args())
				node.AddDependency(buildNode.ID)
				buildNode.AddDependent(node.ID)
				_ = b.graph.AddNode(buildNode)
			}
			_ = b.graph.AddNode(node)
		}
	}

	// === ROUTES (shared) ===
	// Routes depend on ALL instances' service nodes for traffic correlation.
	for _, route := range comp.Routes() {
		node := NewNode(NodeTypeRoute, componentName, route.Name())
		node.SetInput("type", route.Type())
		node.SetInput("internal", route.Internal())
		node.SetInput("rules", route.Rules())
		node.Instances = nodeInstances

		if route.Service() != "" {
			node.SetInput("target", route.Service())
			node.SetInput("targetType", "service")
		} else if route.Function() != "" {
			node.SetInput("target", route.Function())
			node.SetInput("targetType", "function")
		}

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

	// === Second pass: wire dependencies ===
	for _, inst := range instances {
		for _, deploy := range comp.Deployments() {
			nodeID := fmt.Sprintf("%s/%s/%s/%s", componentName, inst.Name, NodeTypeDeployment, deploy.Name())
			node := b.graph.GetNode(nodeID)
			if node == nil {
				continue
			}
			for _, value := range deploy.Environment() {
				b.addInstanceEnvDependencies(componentName, inst.Name, node, value)
			}
			if deploy.Image() != "" {
				b.addInstanceEnvDependencies(componentName, inst.Name, node, deploy.Image())
			}
			if obsNodeID != "" {
				obsNode := b.graph.GetNode(obsNodeID)
				if obsNode != nil {
					node.AddDependency(obsNodeID)
					obsNode.AddDependent(node.ID)
				}
			}
		}

		for _, fn := range comp.Functions() {
			nodeID := fmt.Sprintf("%s/%s/%s/%s", componentName, inst.Name, NodeTypeFunction, fn.Name())
			node := b.graph.GetNode(nodeID)
			if node == nil {
				continue
			}
			for _, value := range fn.Environment() {
				b.addInstanceEnvDependencies(componentName, inst.Name, node, value)
			}
			if obsNodeID != "" {
				obsNode := b.graph.GetNode(obsNodeID)
				if obsNode != nil {
					node.AddDependency(obsNodeID)
					obsNode.AddDependent(node.ID)
				}
			}
		}

		for _, cron := range comp.Cronjobs() {
			nodeID := fmt.Sprintf("%s/%s/%s/%s", componentName, inst.Name, NodeTypeCronjob, cron.Name())
			node := b.graph.GetNode(nodeID)
			if node == nil {
				continue
			}
			for _, value := range cron.Environment() {
				b.addInstanceEnvDependencies(componentName, inst.Name, node, value)
			}
			if obsNodeID != "" {
				obsNode := b.graph.GetNode(obsNodeID)
				if obsNode != nil {
					node.AddDependency(obsNodeID)
					obsNode.AddDependent(node.ID)
				}
			}
		}

		// Scan service port fields for dependencies
		for _, svc := range comp.Services() {
			nodeID := fmt.Sprintf("%s/%s/%s/%s", componentName, inst.Name, NodeTypeService, svc.Name())
			node := b.graph.GetNode(nodeID)
			if node == nil {
				continue
			}
			if svc.Port() != "" {
				b.addInstanceEnvDependencies(componentName, inst.Name, node, svc.Port())
			}
		}
	}

	return nil
}

// addInstanceEnvDependencies resolves dependencies for instance-qualified nodes.
// It first looks for per-instance dependencies (instance-qualified IDs),
// then falls back to shared resources (non-instance-qualified IDs).
// Also injects databaseUser and networkPolicy implicit nodes.
func (b *Builder) addInstanceEnvDependencies(componentName, instanceName string, node *Node, value string) {
	deps := extractDependencies(value)
	for _, dep := range deps {
		// First try instance-qualified ID
		depNodeID := b.resolveInstanceDepReference(componentName, instanceName, dep)
		if depNodeID == "" {
			continue
		}
		depNode := b.graph.GetNode(depNodeID)
		if depNode == nil {
			// Fall back to shared (non-instance-qualified) ID
			depNodeID = b.resolveDepReference(componentName, dep)
			if depNodeID == "" {
				continue
			}
			depNode = b.graph.GetNode(depNodeID)
			if depNode == nil {
				continue
			}
		}

		// Implicit databaseUser interposition for multi-instance mode
		if depNode.Type == NodeTypeDatabase && IsWorkloadType(node.Type) && b.shouldCreateDatabaseUser(depNode, node) {
			dbUserNode := b.getOrCreateDatabaseUserNode(componentName, depNode, node)
			node.AddDependency(dbUserNode.ID)
			dbUserNode.AddDependent(node.ID)

			for _, dependentID := range depNode.DependedOnBy {
				taskNode := b.graph.GetNode(dependentID)
				if taskNode != nil && taskNode.Type == NodeTypeTask {
					node.AddDependency(dependentID)
					taskNode.AddDependent(node.ID)
				}
			}
			continue
		}

		node.AddDependency(depNodeID)
		depNode.AddDependent(node.ID)

		// Implicit networkPolicy creation for multi-instance mode
		if depNode.Type == NodeTypeService && IsWorkloadType(node.Type) && b.shouldCreateNetworkPolicy(node, depNode) {
			b.getOrCreateNetworkPolicyNode(componentName, node, depNode)
		}

		// Also depend on tasks
		for _, dependentID := range depNode.DependedOnBy {
			taskNode := b.graph.GetNode(dependentID)
			if taskNode != nil && taskNode.Type == NodeTypeTask {
				node.AddDependency(dependentID)
				taskNode.AddDependent(node.ID)
			}
		}
	}
}

// resolveInstanceDepReference converts a reference to an instance-qualified node ID.
func (b *Builder) resolveInstanceDepReference(componentName, instanceName, ref string) string {
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
	case "ports":
		nodeType = NodeTypePort
	case "observability":
		return fmt.Sprintf("%s/%s/%s", componentName, NodeTypeObservability, "observability")
	default:
		return ""
	}

	// Per-instance types use instance-qualified IDs
	if IsPerInstanceType(nodeType) {
		return fmt.Sprintf("%s/%s/%s/%s", componentName, instanceName, nodeType, resourceName)
	}
	// Shared types use non-instance-qualified IDs
	return fmt.Sprintf("%s/%s/%s", componentName, nodeType, resourceName)
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
	case "ports":
		nodeType = NodeTypePort
	case "observability":
		// Observability is a singleton per component, always named "observability"
		return fmt.Sprintf("%s/%s/%s", componentName, NodeTypeObservability, "observability")
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
