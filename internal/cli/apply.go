package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/davidthor/cldctl/pkg/engine"
	"github.com/davidthor/cldctl/pkg/oci"
	"github.com/davidthor/cldctl/pkg/registry"
	"github.com/spf13/cobra"
)

func newApplyCmd() *cobra.Command {
	var (
		environment   string
		datacenter    string
		variables     []string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "apply <component-ref> <node-path>",
		Short: "Apply a single resource node from a component",
		Long: `Executes a single logical resource node from a component's dependency graph.

This command is designed for CI/CD workflows where each graph node runs as a
separate job. It builds the full graph, locates the target node, resolves
upstream dependency outputs from state, and executes only the target node.

The <component-ref> can be an OCI reference (e.g., ghcr.io/org/app:v1) or a
local path (e.g., ./my-app).

The <node-path> identifies the target node as type/name, matching the graph
node's Type and Name fields (e.g., database/main, deployment/api, dockerBuild/api).

Examples:
  cldctl apply ghcr.io/org/app:v1 database/main -e staging -d my-dc
  cldctl apply ghcr.io/org/app:v1 deployment/api -e staging -d my-dc
  cldctl apply ./my-app dockerBuild/api -e staging -d my-dc
  cldctl apply ghcr.io/org/app:v1 service/api -e staging -d my-dc --var api_key=$API_KEY`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			componentRef := args[0]
			nodePath := args[1]
			ctx := context.Background()

			// Resolve datacenter
			dc, err := resolveDatacenter(datacenter)
			if err != nil {
				return err
			}

			if environment == "" {
				return fmt.Errorf("environment is required for apply (use -e flag)")
			}

			// Create state manager
			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Parse variables
			vars := make(map[string]interface{})
			for _, v := range variables {
				parts := strings.SplitN(v, "=", 2)
				if len(parts) == 2 {
					vars[parts[0]] = parts[1]
				}
			}

			// Derive component name
			componentName := deriveComponentName(componentRef, false)

			// Resolve component path: local path or OCI reference
			componentPath, err := resolveComponentPath(componentRef)
			if err != nil {
				return fmt.Errorf("failed to resolve component: %w", err)
			}

			// Create engine and apply
			eng := createEngine(mgr)

			result, err := eng.ApplyNode(ctx, engine.ApplyNodeOptions{
				Environment:   environment,
				Datacenter:    dc,
				ComponentName: componentName,
				ComponentPath: componentPath,
				NodePath:      nodePath,
				Variables:     vars,
				Output:        os.Stdout,
			})
			if err != nil {
				return err
			}

			if result.Action == "noop" {
				fmt.Printf("No changes needed for %s\n", nodePath)
			} else {
				fmt.Printf("Successfully applied %s (%s) in %s\n", nodePath, result.Action, result.Duration.Round(100*1e6))
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Target datacenter")
	cmd.Flags().StringArrayVar(&variables, "var", nil, "Variable values (key=value)")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}

// resolveComponentPath resolves a component reference to a local filesystem path.
// The reference can be a local path or an OCI image reference.
func resolveComponentPath(ref string) (string, error) {
	// Check if this is a local path
	if strings.HasPrefix(ref, ".") || strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "~") {
		compFile := findComponentFile(ref)
		if compFile != "" {
			return compFile, nil
		}
		return "", fmt.Errorf("no component file found at %s", ref)
	}

	// Try local registry cache
	reg, err := registry.NewRegistry()
	if err != nil {
		return "", fmt.Errorf("failed to open local registry: %w", err)
	}

	entry, err := reg.Get(ref)
	if err == nil && entry != nil && entry.CachePath != "" {
		compFile := findComponentFile(entry.CachePath)
		if compFile != "" {
			return compFile, nil
		}
	}

	// Pull from remote
	client := oci.NewClient()
	compDir, err := registry.CachePathForRef(ref)
	if err != nil {
		return "", fmt.Errorf("failed to determine cache path: %w", err)
	}

	if err := client.Pull(context.Background(), ref, compDir); err != nil {
		return "", fmt.Errorf("failed to pull %s: %w", ref, err)
	}

	compFile := findComponentFile(compDir)
	if compFile == "" {
		return "", fmt.Errorf("no component file found in pulled artifact %s", ref)
	}

	return compFile, nil
}
