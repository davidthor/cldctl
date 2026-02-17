//go:build js && wasm

package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/davidthor/cldctl/pkg/graph"
	"github.com/davidthor/cldctl/pkg/graph/visual"
	"github.com/davidthor/cldctl/pkg/schema/component"
)

type result struct {
	Mermaid string   `json:"mermaid,omitempty"`
	Nodes   int      `json:"nodes"`
	Edges   int      `json:"edges"`
	Errors  []string `json:"errors,omitempty"`
}

func parseComponent(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return toJS(result{Errors: []string{"missing yaml argument"}})
	}

	yamlStr := args[0].String()
	compName := "app"
	if len(args) > 1 && args[1].String() != "" {
		compName = args[1].String()
	}

	loader := component.NewLoader()
	comp, err := loader.LoadFromBytes([]byte(yamlStr), "playground.yml")
	if err != nil {
		return toJS(result{Errors: []string{err.Error()}})
	}

	builder := graph.NewBuilder("", "")
	if err := builder.AddComponent(compName, comp); err != nil {
		return toJS(result{Errors: []string{err.Error()}})
	}
	g := builder.Build()

	mermaid, err := visual.RenderMermaid(g, visual.MermaidOptions{
		Direction: "TD",
		Title:     compName,
	})
	if err != nil {
		return toJS(result{Errors: []string{err.Error()}})
	}

	nodeCount := 0
	edgeCount := 0
	for _, n := range g.Nodes {
		nodeCount++
		edgeCount += len(n.DependsOn)
	}

	return toJS(result{
		Mermaid: mermaid,
		Nodes:   nodeCount,
		Edges:   edgeCount,
	})
}

func toJS(r result) interface{} {
	data, _ := json.Marshal(r)
	return js.ValueOf(string(data))
}

func main() {
	js.Global().Set("cldctlParseComponent", js.FuncOf(parseComponent))
	// Signal that the module is ready
	if cb := js.Global().Get("_cldctlReady"); !cb.IsUndefined() && !cb.IsNull() {
		cb.Invoke()
	}
	// Block forever
	select {}
}
