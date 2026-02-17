package visual

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/davidthor/cldctl/pkg/graph"
)

// RenderImage generates a PNG image from a dependency graph by rendering
// a Mermaid flowchart through mermaid-cli (mmdc).
//
// The mmdc binary must be available on $PATH. Install it with:
//
//	npm install -g @mermaid-js/mermaid-cli
//
// Returns the PNG image bytes or an error if mmdc is not installed.
func RenderImage(g *graph.Graph, opts ImageOptions) ([]byte, error) {
	// Generate Mermaid text first
	mermaidText, err := RenderMermaid(g, opts.MermaidOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to generate mermaid diagram: %w", err)
	}

	return RenderMermaidToImage(mermaidText, opts)
}

// RenderMermaidToImage converts raw Mermaid text into a PNG image using mmdc.
// This is exposed so callers who already have Mermaid text can render it directly.
func RenderMermaidToImage(mermaidText string, opts ImageOptions) ([]byte, error) {
	// Check if mmdc is available
	mmdcPath, err := exec.LookPath("mmdc")
	if err != nil {
		return nil, fmt.Errorf(
			"mermaid-cli (mmdc) is not installed or not on $PATH\n\n" +
				"Install it with:\n" +
				"  npm install -g @mermaid-js/mermaid-cli\n\n" +
				"Alternatively, use --type mermaid to get the raw diagram text,\n" +
				"which you can paste into any Mermaid renderer (e.g., mermaid.live).",
		)
	}

	// Create temp files for input/output
	tmpDir, err := os.MkdirTemp("", "cldctl-mermaid-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	inputFile := filepath.Join(tmpDir, "input.mmd")
	outputFile := filepath.Join(tmpDir, "output.png")

	if err := os.WriteFile(inputFile, []byte(mermaidText), 0644); err != nil {
		return nil, fmt.Errorf("failed to write mermaid input: %w", err)
	}

	// Build mmdc arguments
	args := []string{"-i", inputFile, "-o", outputFile, "-e", "png"}

	theme := opts.Theme
	if theme == "" {
		theme = "default"
	}
	args = append(args, "-t", theme)

	if opts.Width > 0 {
		args = append(args, "-w", fmt.Sprintf("%d", opts.Width))
	}
	if opts.Height > 0 {
		args = append(args, "-H", fmt.Sprintf("%d", opts.Height))
	}

	// Run mmdc
	cmd := exec.Command(mmdcPath, args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("mmdc failed: %w", err)
	}

	// Read the output
	data, err := os.ReadFile(outputFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read rendered image: %w", err)
	}

	return data, nil
}
