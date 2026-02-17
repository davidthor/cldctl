//go:build js && wasm

package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/davidthor/cldctl/pkg/graph"
	"github.com/davidthor/cldctl/pkg/graph/visual"
	"github.com/davidthor/cldctl/pkg/schema/component"
	"github.com/davidthor/cldctl/pkg/schema/datacenter/v1"
	"github.com/davidthor/cldctl/pkg/schema/environment"
)

// componentResult is the JSON shape returned by cldctlParseComponent.
type componentResult struct {
	Mermaid string   `json:"mermaid,omitempty"`
	Nodes   int      `json:"nodes"`
	Edges   int      `json:"edges"`
	Errors  []string `json:"errors,omitempty"`
}

// environmentResult is the JSON shape returned by cldctlParseEnvironment.
type environmentResult struct {
	Mermaid    string   `json:"mermaid,omitempty"`
	Nodes      int      `json:"nodes"`
	Edges      int      `json:"edges"`
	Components []string `json:"components,omitempty"`
	Errors     []string `json:"errors,omitempty"`
}

// infrastructureResult is the JSON shape returned by cldctlParseInfrastructure.
type infrastructureResult struct {
	Mermaid  string                   `json:"mermaid,omitempty"`
	Nodes    int                      `json:"nodes"`
	Modules  int                      `json:"modules"`
	Mappings []map[string]interface{} `json:"mappings,omitempty"`
	Errors   []string                 `json:"errors,omitempty"`
}

// parseComponent handles: cldctlParseComponent(yaml, name) -> JSON
func parseComponent(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return toJSON(componentResult{Errors: []string{"missing yaml argument"}})
	}

	yamlStr := args[0].String()
	compName := "app"
	if len(args) > 1 && args[1].String() != "" {
		compName = args[1].String()
	}

	loader := component.NewLoader()
	comp, err := loader.LoadFromBytes([]byte(yamlStr), "playground.yml")
	if err != nil {
		return toJSON(componentResult{Errors: []string{err.Error()}})
	}

	builder := graph.NewBuilder("", "")
	if err := builder.AddComponent(compName, comp); err != nil {
		return toJSON(componentResult{Errors: []string{err.Error()}})
	}
	g := builder.Build()

	mermaid, err := visual.RenderMermaid(g, visual.MermaidOptions{
		Direction: "TD",
		Title:     compName,
	})
	if err != nil {
		return toJSON(componentResult{Errors: []string{err.Error()}})
	}

	nodes, edges := countGraph(g)
	return toJSON(componentResult{
		Mermaid: mermaid,
		Nodes:   nodes,
		Edges:   edges,
	})
}

// parseEnvironment handles: cldctlParseEnvironment(envYaml, componentYamls) -> JSON
func parseEnvironment(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return toJSON(environmentResult{Errors: []string{"expected 2 arguments: envYaml, componentYamls (JSON map)"}})
	}

	envYaml := args[0].String()
	componentYamls, err := parseJSMap(args[1].String())
	if err != nil {
		return toJSON(environmentResult{Errors: []string{"componentYamls must be a JSON map of name->yaml: " + err.Error()}})
	}

	envLoader := environment.NewLoader()
	env, err := envLoader.LoadFromBytes([]byte(envYaml), "playground.yml")
	if err != nil {
		return toJSON(environmentResult{Errors: []string{err.Error()}})
	}

	compLoader := component.NewLoader()
	builder := graph.NewBuilder("playground", "")

	var errs []string
	var compNames []string

	for name, compConfig := range env.Components() {
		if compConfig.Path() != "" {
			errs = append(errs, "component "+name+": path-based component references are not supported in the playground; provide the component YAML inline using image references")
			continue
		}

		yamlContent, ok := componentYamls[name]
		if !ok {
			errs = append(errs, "component "+name+": no YAML provided in componentYamls map")
			continue
		}

		comp, err := compLoader.LoadFromBytes([]byte(yamlContent), name+".yml")
		if err != nil {
			errs = append(errs, "component "+name+": "+err.Error())
			continue
		}

		if err := builder.AddComponent(name, comp); err != nil {
			errs = append(errs, "component "+name+": "+err.Error())
			continue
		}
		compNames = append(compNames, name)
	}

	if len(errs) > 0 && len(compNames) == 0 {
		return toJSON(environmentResult{Errors: errs})
	}

	g := builder.Build()

	mermaid, err := visual.RenderMermaid(g, visual.MermaidOptions{
		Direction:        "TD",
		GroupByComponent: true,
		Title:            "Environment",
	})
	if err != nil {
		return toJSON(environmentResult{Errors: append(errs, err.Error())})
	}

	nodes, edges := countGraph(g)
	return toJSON(environmentResult{
		Mermaid:    mermaid,
		Nodes:      nodes,
		Edges:      edges,
		Components: compNames,
		Errors:     errs,
	})
}

// parseInfrastructure handles: cldctlParseInfrastructure(datacenterHCL, mode, yaml, componentYamls, variables) -> JSON
func parseInfrastructure(this js.Value, args []js.Value) interface{} {
	if len(args) < 3 {
		return toJSON(infrastructureResult{Errors: []string{"expected at least 3 arguments: datacenterHCL, mode, yaml"}})
	}

	dcHCL := args[0].String()
	mode := args[1].String()
	yamlStr := args[2].String()

	var componentYamls map[string]string
	if len(args) > 3 && args[3].String() != "" {
		var err error
		componentYamls, err = parseJSMap(args[3].String())
		if err != nil {
			return toJSON(infrastructureResult{Errors: []string{"componentYamls must be a JSON map: " + err.Error()}})
		}
	}

	var variables map[string]interface{}
	if len(args) > 4 && args[4].String() != "" {
		if err := json.Unmarshal([]byte(args[4].String()), &variables); err != nil {
			return toJSON(infrastructureResult{Errors: []string{"variables must be a JSON object: " + err.Error()}})
		}
	}

	// Parse the datacenter HCL using the v1 parser directly so we get HookBlockV1
	// objects with WhenExpr for runtime evaluation.
	parser := v1.NewParser()
	schema, _, err := parser.ParseBytes([]byte(dcHCL), "playground.dc")
	if err != nil {
		return toJSON(infrastructureResult{Errors: []string{"datacenter parse error: " + err.Error()}})
	}
	if schema.Environment == nil {
		return toJSON(infrastructureResult{Errors: []string{"datacenter has no environment block"}})
	}

	// Build the resource graph from the component/environment YAML
	g, compNames, errs := buildResourceGraph(mode, yamlStr, componentYamls)
	if g == nil {
		return toJSON(infrastructureResult{Errors: errs})
	}
	_ = compNames

	// Match each resource node to a datacenter hook
	eval := v1.NewEvaluator()
	if variables != nil {
		eval.SetVariables(variables)
	}
	eval.SetEnvironmentContext("playground", "", "", "")

	sorted, err := g.TopologicalSort()
	if err != nil {
		return toJSON(infrastructureResult{Errors: append(errs, "graph sort error: "+err.Error())})
	}

	var matches []visual.HookMatch
	uniqueModules := make(map[string]bool)

	for _, node := range sorted {
		hooks := getHooksForType(schema.Environment, node.Type)
		if len(hooks) == 0 {
			continue
		}

		eval.SetNodeContext(string(node.Type), node.Name, node.Component, node.Inputs)

		matched, err := eval.FindMatchingHook(hooks)
		if err != nil {
			errs = append(errs, "hook evaluation error for "+node.ID+": "+err.Error())
			continue
		}

		if matched == nil {
			continue
		}

		hm := visual.HookMatch{
			NodeID:    node.ID,
			NodeType:  string(node.Type),
			NodeName:  node.Name,
			Component: node.Component,
		}

		if matched.IsError {
			hm.IsError = true
			hm.Error = matched.ErrorMessage
		} else {
			for _, mod := range matched.Modules {
				hm.Modules = append(hm.Modules, mod.Name)
				uniqueModules[mod.Name] = true
			}
		}

		matches = append(matches, hm)
	}

	mermaid, err := visual.RenderInfrastructureMermaid(g, matches, visual.InfrastructureOptions{
		Direction:        "TD",
		GroupByComponent: mode == "environment",
		Title:            "Infrastructure",
	})
	if err != nil {
		return toJSON(infrastructureResult{Errors: append(errs, err.Error())})
	}

	nodes, _ := countGraph(g)
	return toJSON(infrastructureResult{
		Mermaid:  mermaid,
		Nodes:    nodes,
		Modules:  len(uniqueModules),
		Mappings: visual.BuildHookMatchSummary(matches),
		Errors:   errs,
	})
}

// buildResourceGraph constructs a graph from component or environment YAML.
func buildResourceGraph(mode, yamlStr string, componentYamls map[string]string) (*graph.Graph, []string, []string) {
	compLoader := component.NewLoader()

	switch mode {
	case "component":
		comp, err := compLoader.LoadFromBytes([]byte(yamlStr), "playground.yml")
		if err != nil {
			return nil, nil, []string{err.Error()}
		}
		builder := graph.NewBuilder("playground", "")
		if err := builder.AddComponent("app", comp); err != nil {
			return nil, nil, []string{err.Error()}
		}
		return builder.Build(), []string{"app"}, nil

	case "environment":
		envLoader := environment.NewLoader()
		env, err := envLoader.LoadFromBytes([]byte(yamlStr), "playground.yml")
		if err != nil {
			return nil, nil, []string{err.Error()}
		}

		builder := graph.NewBuilder("playground", "")
		var errs []string
		var compNames []string

		for name, compConfig := range env.Components() {
			if compConfig.Path() != "" {
				errs = append(errs, "component "+name+": path-based component references are not supported in the playground; provide the component YAML inline using image references")
				continue
			}

			yamlContent, ok := componentYamls[name]
			if !ok {
				errs = append(errs, "component "+name+": no YAML provided in componentYamls map")
				continue
			}

			comp, err := compLoader.LoadFromBytes([]byte(yamlContent), name+".yml")
			if err != nil {
				errs = append(errs, "component "+name+": "+err.Error())
				continue
			}

			if err := builder.AddComponent(name, comp); err != nil {
				errs = append(errs, "component "+name+": "+err.Error())
				continue
			}
			compNames = append(compNames, name)
		}

		if len(compNames) == 0 {
			return nil, nil, errs
		}
		return builder.Build(), compNames, errs

	default:
		return nil, nil, []string{"mode must be 'component' or 'environment'"}
	}
}

// getHooksForType returns the v1 hook slice for a given node type.
func getHooksForType(env *v1.EnvironmentBlockV1, nodeType graph.NodeType) []v1.HookBlockV1 {
	switch nodeType {
	case graph.NodeTypeDatabase:
		return env.DatabaseHooks
	case graph.NodeTypeBucket:
		return env.BucketHooks
	case graph.NodeTypeEncryptionKey:
		return env.EncryptionKeyHooks
	case graph.NodeTypeSMTP:
		return env.SMTPHooks
	case graph.NodeTypeDeployment:
		return env.DeploymentHooks
	case graph.NodeTypeFunction:
		return env.FunctionHooks
	case graph.NodeTypeService:
		return env.ServiceHooks
	case graph.NodeTypeRoute:
		return env.RouteHooks
	case graph.NodeTypeCronjob:
		return env.CronjobHooks
	case graph.NodeTypeSecret:
		return env.SecretHooks
	case graph.NodeTypeDockerBuild:
		return env.DockerBuildHooks
	case graph.NodeTypeTask:
		return env.TaskHooks
	case graph.NodeTypeObservability:
		return env.ObservabilityHooks
	case graph.NodeTypePort:
		return env.PortHooks
	case graph.NodeTypeDatabaseUser:
		return env.DatabaseUserHooks
	case graph.NodeTypeNetworkPolicy:
		return env.NetworkPolicyHooks
	default:
		return nil
	}
}

// countGraph returns the number of nodes and edges in a graph.
func countGraph(g *graph.Graph) (int, int) {
	nodes := 0
	edges := 0
	for _, n := range g.Nodes {
		nodes++
		edges += len(n.DependsOn)
	}
	return nodes, edges
}

// parseJSMap parses a JSON string into a map[string]string.
func parseJSMap(s string) (map[string]string, error) {
	var m map[string]string
	err := json.Unmarshal([]byte(s), &m)
	return m, err
}

// toJSON serializes any value to a JSON string and returns it as a JS value.
func toJSON(v interface{}) interface{} {
	data, _ := json.Marshal(v)
	return js.ValueOf(string(data))
}

func main() {
	js.Global().Set("cldctlParseComponent", js.FuncOf(parseComponent))
	js.Global().Set("cldctlParseEnvironment", js.FuncOf(parseEnvironment))
	js.Global().Set("cldctlParseInfrastructure", js.FuncOf(parseInfrastructure))

	// Signal readiness
	if cb := js.Global().Get("_cldctlReady"); !cb.IsUndefined() && !cb.IsNull() {
		cb.Invoke()
	}

	// Block forever
	select {}
}
