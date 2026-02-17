package visual

import (
	"testing"

	"github.com/davidthor/cldctl/pkg/graph"
	"github.com/stretchr/testify/assert"
)

func TestRenderImage_NilGraph(t *testing.T) {
	_, err := RenderImage(nil, ImageOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestRenderImage_MissingMmdc(t *testing.T) {
	// This test will likely fail to find mmdc in most test environments,
	// which is the expected behavior - we want to verify the error message.
	g := graph.NewGraph("staging", "my-dc")
	_ = g.AddNode(graph.NewNode(graph.NodeTypeDatabase, "app", "main"))

	_, err := RenderImage(g, ImageOptions{})
	if err != nil {
		// Either mmdc is not installed (expected) or some other error
		assert.Contains(t, err.Error(), "mmdc")
	}
	// If mmdc IS installed, the test passes (it rendered successfully)
}

func TestRenderMermaidToImage_MissingMmdc(t *testing.T) {
	_, err := RenderMermaidToImage("flowchart TD\n    A --> B", ImageOptions{})
	if err != nil {
		assert.Contains(t, err.Error(), "mmdc")
	}
}
