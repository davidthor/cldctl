package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/davidthor/cldctl/pkg/errors"
	"github.com/davidthor/cldctl/pkg/graph"
	"github.com/davidthor/cldctl/pkg/resolver"
	"github.com/davidthor/cldctl/pkg/schema/component"
	"github.com/spf13/cobra"
)

func newInspectCmd() *cobra.Command {
	var (
		datacenter    string
		outputFormat  string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "inspect [environment[/component[/resource]]]",
		Short: "Inspect deployed state or visualize component topology",
		Long: `Inspect deployed resources by providing a slash-separated path:

  cldctl inspect <environment>                           Show environment details
  cldctl inspect <environment>/<component>               Show component details
  cldctl inspect <environment>/<component>/<resource>    Show resource details

Resources can be qualified with type if the name is ambiguous:
  cldctl inspect staging/my-app/deployment/api

To visualize a component's topology instead, use:
  cldctl inspect component ./my-app

Examples:
  # Inspect an environment
  cldctl inspect staging

  # Inspect a component within an environment
  cldctl inspect staging/my-app

  # Inspect a specific resource to see its environment variables
  cldctl inspect staging/my-app/api

  # Disambiguate resources with the same name across types
  cldctl inspect staging/my-app/deployment/api

  # Output as JSON or YAML
  cldctl inspect staging/my-app/api -o json`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}

			ctx := context.Background()

			// Resolve datacenter
			dc, err := resolveDatacenter(datacenter)
			if err != nil {
				return err
			}

			// Create state manager
			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Parse path: environment[/component[/resource]]
			pathArg := strings.Trim(args[0], "/")
			parts := strings.Split(pathArg, "/")

			// All paths require the environment state. Components and resources
			// are stored inline within the environment state (not as separate files),
			// so we always load the environment and extract from there.
			envName := parts[0]
			env, err := mgr.GetEnvironment(ctx, dc, envName)
			if err != nil {
				return fmt.Errorf("environment %q not found in datacenter %q: %w", envName, dc, err)
			}

			switch len(parts) {
			case 1:
				// Environment only
				return inspectEnvironmentState(env, dc, outputFormat)

			case 2:
				// Environment/component
				comp, ok := env.Components[parts[1]]
				if !ok {
					var available []string
					for name := range env.Components {
						available = append(available, name)
					}
					if len(available) == 0 {
						return fmt.Errorf("component %q not found in environment %q (no components deployed)", parts[1], envName)
					}
					return fmt.Errorf("component %q not found in environment %q\n\nAvailable components:\n  %s",
						parts[1], envName, strings.Join(available, "\n  "))
				}
				return inspectComponentState(comp, dc, envName, outputFormat)

			case 3:
				// Environment/component/resource (match by name)
				comp, ok := env.Components[parts[1]]
				if !ok {
					return fmt.Errorf("component %q not found in environment %q", parts[1], envName)
				}
				res, err := findResource(comp.Resources, parts[2], "")
				if err != nil {
					return err
				}
				return inspectResourceState(res, dc, envName, outputFormat)

			case 4:
				// Environment/component/type/name
				comp, ok := env.Components[parts[1]]
				if !ok {
					return fmt.Errorf("component %q not found in environment %q", parts[1], envName)
				}
				res, err := findResource(comp.Resources, parts[3], parts[2])
				if err != nil {
					return err
				}
				return inspectResourceState(res, dc, envName, outputFormat)

			default:
				return fmt.Errorf("invalid path %q: expected environment[/component[/resource]]", args[0])
			}
		},
	}

	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Target datacenter (uses default if not set)")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format: table, json, yaml")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	cmd.AddCommand(newInspectComponentCmd())

	return cmd
}

func newInspectComponentCmd() *cobra.Command {
	var (
		expand bool
		file   string
	)

	cmd := &cobra.Command{
		Use:     "component [path|image]",
		Aliases: []string{"comp", "comps", "components"},
		Short:   "Visualize a component's topology as an ASCII graph",
		Long: `Inspect a component and display its resource topology as an ASCII graph.

The graph shows all nodes (databases, deployments, functions, services, routes, etc.)
and the dependency edges between them.

Examples:
  # Inspect a local component
  cldctl inspect component ./my-app
  cldctl inspect component -f cloud.component.yml

  # Inspect an OCI component image
  cldctl inspect component ghcr.io/myorg/app:v1

  # Expand to include dependency component nodes
  cldctl inspect component ./my-app --expand`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Determine the component reference
			ref := "."
			if len(args) > 0 {
				ref = args[0]
			}
			if file != "" {
				ref = file
			}

			// Create resolver
			res := resolver.NewResolver(resolver.Options{
				AllowLocal:  true,
				AllowRemote: true,
			})

			// Create dependency resolver for expanded mode
			depResolver := resolver.NewDependencyResolver(res)

			if expand {
				// Resolve with all dependencies
				depGraph, err := depResolver.Resolve(ctx, ref, nil)
				if err != nil {
					return formatResolveError(err)
				}

				// Build expanded graph
				return printExpandedTopology(depGraph)
			}

			// Non-expanded mode: just show the root component
			resolved, err := res.Resolve(ctx, ref)
			if err != nil {
				return formatResolveError(err)
			}

			loader := component.NewLoader()
			comp, err := loader.Load(resolved.Path)
			if err != nil {
				return formatLoadError(err)
			}

			// Determine component name from reference
			componentName := extractComponentName(ref, resolved)

			return printComponentTopology(componentName, comp)
		},
	}

	cmd.Flags().BoolVar(&expand, "expand", false, "Expand dependency components to show their nodes")
	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to cloud.component.yml if not in default location")

	return cmd
}

// formatResolveError formats resolution errors to show validation details
func formatResolveError(err error) error {
	return formatErrorWithDetails(err, "failed to resolve component")
}

// formatLoadError formats load errors to show validation details
func formatLoadError(err error) error {
	return formatErrorWithDetails(err, "failed to load component")
}

// formatErrorWithDetails extracts and displays validation error details
func formatErrorWithDetails(err error, prefix string) error {
	// Try to extract cldctl error with details
	var arcErr *errors.Error
	if e, ok := err.(*errors.Error); ok {
		arcErr = e
	} else {
		// Check wrapped errors
		unwrapped := err
		for unwrapped != nil {
			if e, ok := unwrapped.(*errors.Error); ok {
				arcErr = e
				break
			}
			if u, ok := unwrapped.(interface{ Unwrap() error }); ok {
				unwrapped = u.Unwrap()
			} else {
				break
			}
		}
	}

	if arcErr != nil && arcErr.Code == errors.ErrCodeValidation {
		// Extract validation error details
		if errList, ok := arcErr.Details["errors"].([]string); ok && len(errList) > 0 {
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("%s: validation failed\n", prefix))
			sb.WriteString("\nValidation errors:\n")
			for _, e := range errList {
				sb.WriteString(fmt.Sprintf("  - %s\n", e))
			}
			return fmt.Errorf("%s", sb.String())
		}
	}

	return fmt.Errorf("%s: %w", prefix, err)
}

// extractComponentName derives a display name from the reference
func extractComponentName(ref string, resolved resolver.ResolvedComponent) string {
	// For local paths, use the directory name
	if resolved.Type == resolver.ReferenceTypeLocal {
		dir := filepath.Dir(resolved.Path)
		return filepath.Base(dir)
	}

	// For OCI refs, use the image name (without registry and tag)
	if resolved.Type == resolver.ReferenceTypeOCI {
		// Extract name from ghcr.io/org/name:tag -> name
		parts := strings.Split(ref, "/")
		name := parts[len(parts)-1]
		// Remove tag
		if idx := strings.Index(name, ":"); idx != -1 {
			name = name[:idx]
		}
		return name
	}

	return "component"
}

// printComponentTopology prints a single component's topology
func printComponentTopology(name string, comp component.Component) error {
	// Build graph for this component
	builder := graph.NewBuilder("", "")
	if err := builder.AddComponent(name, comp); err != nil {
		return fmt.Errorf("failed to build graph: %w", err)
	}
	g := builder.Build()

	// Print header
	fmt.Printf("\nComponent: %s\n", name)
	fmt.Println(strings.Repeat("=", 60))

	// Get all nodes (sorted by ID for deterministic output)
	nodes := getSortedNodes(g)

	// Group nodes by type for display
	nodesByType := make(map[graph.NodeType][]*graph.Node)
	for _, node := range nodes {
		nodesByType[node.Type] = append(nodesByType[node.Type], node)
	}

	// Print nodes grouped by type
	printNodesByType(nodesByType)

	// Print dependency edges
	fmt.Println("\nDependency Graph:")
	fmt.Println(strings.Repeat("-", 60))
	printDependencyGraph(nodes)

	// Print external dependencies (other components)
	if len(comp.Dependencies()) > 0 {
		fmt.Println("\nExternal Dependencies:")
		fmt.Println(strings.Repeat("-", 60))
		for _, dep := range comp.Dependencies() {
			fmt.Printf("  -> %s (%s)\n", dep.Name(), dep.Component())
		}
	}

	fmt.Println()
	return nil
}

// getSortedNodes returns all nodes from a graph sorted by ID for deterministic output
func getSortedNodes(g *graph.Graph) []*graph.Node {
	nodes := make([]*graph.Node, 0, len(g.Nodes))
	for _, node := range g.Nodes {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	return nodes
}

// printExpandedTopology prints the full expanded topology including dependencies
func printExpandedTopology(depGraph *resolver.DependencyGraph) error {
	// Build combined graph from all components
	builder := graph.NewBuilder("", "")

	// Process in deployment order (dependencies first)
	for _, name := range depGraph.Order {
		dep, ok := depGraph.All[name]
		if !ok {
			continue
		}

		compName := dep.Name
		if compName == "root" {
			// Use a better name for the root component
			compName = extractComponentName(dep.Component.Reference, dep.Component)
		}

		if err := builder.AddComponent(compName, dep.LoadedComponent); err != nil {
			return fmt.Errorf("failed to add component %s to graph: %w", compName, err)
		}
	}

	g := builder.Build()

	// Print header
	fmt.Printf("\nExpanded Component Topology\n")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("Components: %d\n", len(depGraph.Order))

	// Get all nodes sorted by ID
	nodes := getSortedNodes(g)

	// Group by component, then by type
	nodesByComponent := make(map[string]map[graph.NodeType][]*graph.Node)
	for _, node := range nodes {
		if nodesByComponent[node.Component] == nil {
			nodesByComponent[node.Component] = make(map[graph.NodeType][]*graph.Node)
		}
		nodesByComponent[node.Component][node.Type] = append(nodesByComponent[node.Component][node.Type], node)
	}

	// Print each component's nodes
	for _, name := range depGraph.Order {
		compName := name
		if compName == "root" {
			dep := depGraph.All[name]
			compName = extractComponentName(dep.Component.Reference, dep.Component)
		}

		if nodesByType, ok := nodesByComponent[compName]; ok {
			fmt.Printf("\n[%s]\n", compName)
			fmt.Println(strings.Repeat("-", 40))
			printNodesByType(nodesByType)
		}
	}

	// Print full dependency graph
	fmt.Println("\nFull Dependency Graph:")
	fmt.Println(strings.Repeat("-", 60))
	printDependencyGraph(nodes)

	fmt.Println()
	return nil
}

// printNodesByType prints nodes organized by type
func printNodesByType(nodesByType map[graph.NodeType][]*graph.Node) {
	// Define display order for node types
	typeOrder := []graph.NodeType{
		graph.NodeTypeDatabase,
		graph.NodeTypeBucket,
		graph.NodeTypeEncryptionKey,
		graph.NodeTypeSMTP,
		graph.NodeTypeDockerBuild,
		graph.NodeTypeDeployment,
		graph.NodeTypeFunction,
		graph.NodeTypeTask,
		graph.NodeTypeService,
		graph.NodeTypeCronjob,
		graph.NodeTypeRoute,
	}

	typeSymbols := map[graph.NodeType]string{
		graph.NodeTypeDatabase:      "[DB]",
		graph.NodeTypeBucket:        "[S3]",
		graph.NodeTypeEncryptionKey: "[EK]",
		graph.NodeTypeSMTP:          "[SM]",
		graph.NodeTypeDockerBuild:   "[BL]",
		graph.NodeTypeDeployment:    "[DP]",
		graph.NodeTypeFunction:      "[FN]",
		graph.NodeTypeTask:          "[TK]",
		graph.NodeTypeService:       "[SV]",
		graph.NodeTypeCronjob:       "[CJ]",
		graph.NodeTypeRoute:         "[RT]",
		graph.NodeTypeSecret:        "[SC]",
	}

	typeNames := map[graph.NodeType]string{
		graph.NodeTypeDatabase:      "Databases",
		graph.NodeTypeBucket:        "Buckets",
		graph.NodeTypeEncryptionKey: "Encryption Keys",
		graph.NodeTypeSMTP:          "SMTP",
		graph.NodeTypeDockerBuild:   "Docker Builds",
		graph.NodeTypeDeployment:    "Deployments",
		graph.NodeTypeFunction:      "Functions",
		graph.NodeTypeTask:          "Tasks",
		graph.NodeTypeService:       "Services",
		graph.NodeTypeCronjob:       "Cronjobs",
		graph.NodeTypeRoute:         "Routes",
		graph.NodeTypeSecret:        "Secrets",
	}

	for _, nodeType := range typeOrder {
		nodes, ok := nodesByType[nodeType]
		if !ok || len(nodes) == 0 {
			continue
		}

		symbol := typeSymbols[nodeType]
		typeName := typeNames[nodeType]
		fmt.Printf("\n  %s %s:\n", symbol, typeName)

		for _, node := range nodes {
			// Show node with key inputs
			info := getNodeInfo(node)
			fmt.Printf("      %s %s\n", symbol, node.Name)
			if info != "" {
				fmt.Printf("         %s\n", info)
			}
		}
	}
}

// getNodeInfo returns a brief info string for a node
func getNodeInfo(node *graph.Node) string {
	var parts []string

	switch node.Type {
	case graph.NodeTypeDatabase:
		if dbType, ok := node.Inputs["type"].(string); ok {
			parts = append(parts, dbType)
		}
	case graph.NodeTypeDeployment:
		if image, ok := node.Inputs["image"].(string); ok {
			parts = append(parts, fmt.Sprintf("image=%s", image))
		}
		if replicas, ok := node.Inputs["replicas"].(int); ok {
			parts = append(parts, fmt.Sprintf("replicas=%d", replicas))
		}
	case graph.NodeTypeFunction:
		if framework, ok := node.Inputs["framework"].(string); ok && framework != "" {
			parts = append(parts, fmt.Sprintf("framework=%s", framework))
		}
		if runtime, ok := node.Inputs["runtime"].(string); ok && runtime != "" {
			parts = append(parts, fmt.Sprintf("runtime=%s", runtime))
		}
	case graph.NodeTypeService:
		if port, ok := node.Inputs["port"].(int); ok {
			parts = append(parts, fmt.Sprintf("port=%d", port))
		}
	case graph.NodeTypeRoute:
		if routeType, ok := node.Inputs["type"].(string); ok {
			parts = append(parts, routeType)
		}
	case graph.NodeTypeCronjob:
		if schedule, ok := node.Inputs["schedule"].(string); ok {
			parts = append(parts, fmt.Sprintf("schedule=%s", schedule))
		}
	case graph.NodeTypeDockerBuild:
		if ctx, ok := node.Inputs["context"].(string); ok && ctx != "" {
			// Shorten context path
			if len(ctx) > 30 {
				ctx = "..." + ctx[len(ctx)-27:]
			}
			parts = append(parts, fmt.Sprintf("context=%s", ctx))
		}
	}

	return strings.Join(parts, ", ")
}

// graphNodeInfo holds adjacency information for graph rendering
type graphNodeInfo struct {
	node       *graph.Node
	dependsOn  []string
	dependedBy []string
}

// printDependencyGraph prints the dependency relationships as ASCII art
func printDependencyGraph(nodes []*graph.Node) {
	if len(nodes) == 0 {
		fmt.Println("  (no nodes)")
		return
	}

	// Build complete adjacency info
	// Since DependedOnBy may not be complete, rebuild it from DependsOn
	nodeMap := make(map[string]*graphNodeInfo)
	for _, n := range nodes {
		nodeMap[n.ID] = &graphNodeInfo{
			node:       n,
			dependsOn:  n.DependsOn,
			dependedBy: []string{},
		}
	}

	// Rebuild DependedOnBy from DependsOn
	for _, n := range nodes {
		for _, depID := range n.DependsOn {
			if depNode, ok := nodeMap[depID]; ok {
				depNode.dependedBy = append(depNode.dependedBy, n.ID)
			}
		}
	}

	// Sort dependedBy lists for deterministic output
	for _, info := range nodeMap {
		sort.Strings(info.dependedBy)
	}

	// Find root nodes (no dependencies)
	var roots []*graph.Node
	for _, n := range nodes {
		if len(n.DependsOn) == 0 {
			roots = append(roots, n)
		}
	}

	// Sort roots for deterministic output
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].ID < roots[j].ID
	})

	// Track printed nodes to avoid duplicates
	printed := make(map[string]bool)

	// Print tree from each root
	for i, root := range roots {
		if i > 0 {
			fmt.Println()
		}
		printNodeTree(root, nodeMap, printed, "", true)
	}

	// Print any nodes with dependencies that weren't reachable from roots
	// (handles cross-component dependencies)
	for _, n := range nodes {
		if !printed[n.ID] && len(n.DependsOn) > 0 {
			fmt.Println()
			printNodeTree(n, nodeMap, printed, "", true)
		}
	}
}

// printNodeTree recursively prints a node and its dependents as a tree
func printNodeTree(node *graph.Node, nodeMap map[string]*graphNodeInfo, printed map[string]bool, prefix string, isLast bool) {
	if printed[node.ID] {
		// Show reference to already printed node
		connector := "├──"
		if isLast {
			connector = "└──"
		}
		fmt.Printf("%s%s (%s) [see above]\n", prefix, connector, formatNodeID(node))
		return
	}

	printed[node.ID] = true

	// Print this node
	connector := "├──"
	if isLast {
		connector = "└──"
	}
	if prefix == "" {
		// Root node
		fmt.Printf("  %s\n", formatNodeID(node))
	} else {
		fmt.Printf("%s%s %s\n", prefix, connector, formatNodeID(node))
	}

	// Get dependents (nodes that depend on this one)
	info := nodeMap[node.ID]
	if info == nil {
		return
	}

	dependents := info.dependedBy
	if len(dependents) == 0 {
		return
	}

	// Sort dependents for deterministic output
	sort.Strings(dependents)

	// Calculate new prefix
	var newPrefix string
	if prefix == "" {
		newPrefix = "  "
	} else if isLast {
		newPrefix = prefix + "    "
	} else {
		newPrefix = prefix + "│   "
	}

	// Print each dependent
	for i, depID := range dependents {
		if depNode, ok := nodeMap[depID]; ok {
			printNodeTree(depNode.node, nodeMap, printed, newPrefix, i == len(dependents)-1)
		}
	}
}

// formatNodeID formats a node ID for display
func formatNodeID(node *graph.Node) string {
	typeSymbols := map[graph.NodeType]string{
		graph.NodeTypeDatabase:      "[DB]",
		graph.NodeTypeBucket:        "[S3]",
		graph.NodeTypeEncryptionKey: "[EK]",
		graph.NodeTypeSMTP:          "[SM]",
		graph.NodeTypeDockerBuild:   "[BL]",
		graph.NodeTypeDeployment:    "[DP]",
		graph.NodeTypeFunction:      "[FN]",
		graph.NodeTypeTask:          "[TK]",
		graph.NodeTypeService:       "[SV]",
		graph.NodeTypeCronjob:       "[CJ]",
		graph.NodeTypeRoute:         "[RT]",
		graph.NodeTypeSecret:        "[SC]",
	}

	symbol := typeSymbols[node.Type]
	if symbol == "" {
		symbol = "[??]"
	}

	// Format: [TYPE] component/name
	return fmt.Sprintf("%s %s/%s", symbol, node.Component, node.Name)
}
