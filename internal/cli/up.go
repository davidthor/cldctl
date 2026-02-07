package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/architect-io/arcctl/pkg/engine"
	"github.com/architect-io/arcctl/pkg/engine/executor"
	"github.com/architect-io/arcctl/pkg/schema/component"
	"github.com/architect-io/arcctl/pkg/state"
	"github.com/architect-io/arcctl/pkg/state/types"
	"github.com/spf13/cobra"
)

// resourceID generates a unique ID for a resource.
func resourceID(component, resourceType, name string) string {
	return fmt.Sprintf("%s/%s/%s", component, resourceType, name)
}

func newUpCmd() *cobra.Command {
	var (
		file       string
		name       string
		datacenter string
		variables  []string
		varFile    string
		detach     bool
		noOpen     bool
		port       int
	)

	cmd := &cobra.Command{
		Use:   "up [path]",
		Short: "Deploy a component to a local environment",
		Long: `The up command provides a streamlined experience for local development.
It deploys your component with all its dependencies to an environment
using the specified datacenter.

The up command:
  1. Parses your architect.yml file
  2. Creates or uses an existing environment with the specified datacenter
  3. Provisions all required resources (databases, etc.) in parallel
  4. Builds and deploys your application
  5. Watches for file changes and auto-reloads (unless --detach)
  6. Exposes routes for local access

Examples:
  arcctl up ./my-app -d local
  arcctl up -d my-datacenter --name my-env
  arcctl up ./my-app -d local --var API_KEY=secret`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) > 0 {
				path = args[0]
			}

			// Get absolute path for reliable operations
			absPath, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("failed to resolve path: %w", err)
			}

			// Determine architect.yml location
			componentFile := file
			if componentFile == "" {
				// Check if path is a file or directory
				info, err := os.Stat(absPath)
				if err != nil {
					return fmt.Errorf("failed to access path: %w", err)
				}
				if info.IsDir() {
					// Look for architect.yml in the directory
					componentFile = filepath.Join(absPath, "architect.yml")
					if _, err := os.Stat(componentFile); os.IsNotExist(err) {
						componentFile = filepath.Join(absPath, "architect.yaml")
					}
				} else {
					// Path is a file, use it directly
					componentFile = absPath
					absPath = filepath.Dir(absPath)
				}
			}

			// Load the component
			loader := component.NewLoader()
			comp, err := loader.Load(componentFile)
			if err != nil {
				return fmt.Errorf("failed to load component: %w", err)
			}

			// Resolve datacenter
			dc, err := resolveDatacenter(datacenter)
			if err != nil {
				return err
			}

			// Determine environment name from flag or derive from directory name
			envName := name
			if envName == "" {
				dirName := filepath.Base(absPath)
				envName = fmt.Sprintf("%s-dev", dirName)
			}

			// Load variables from file if specified
			vars := make(map[string]string)
			if varFile != "" {
				data, err := os.ReadFile(varFile)
				if err != nil {
					return fmt.Errorf("failed to read var file: %w", err)
				}
				if err := upParseVarFile(data, vars); err != nil {
					return fmt.Errorf("failed to parse var file: %w", err)
				}
			}

			// Parse inline variables
			for _, v := range variables {
				parts := strings.SplitN(v, "=", 2)
				if len(parts) == 2 {
					vars[parts[0]] = parts[1]
				}
			}

			componentName := filepath.Base(absPath)

			fmt.Printf("Component: %s\n", componentName)
			fmt.Printf("Datacenter: %s\n", dc)
			fmt.Printf("Environment: %s\n", envName)
			fmt.Println()

			// Create state manager (always local for 'up')
			mgr, err := upCreateStateManager()
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Create cancellable context that responds to Ctrl+C
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Verify datacenter exists
			_, err = mgr.GetDatacenter(ctx, dc)
			if err != nil {
				return fmt.Errorf("datacenter %q not found: %w\nDeploy a datacenter first with: arcctl deploy datacenter <name> <path>", dc, err)
			}

			// Create the engine early for dependency resolution
			eng := createEngine(mgr)

			// Convert vars to interface{} map
			varsInterface := make(map[string]interface{})
			for k, v := range vars {
				varsInterface[k] = v
			}

			// Build initial component and variable maps
			componentsMap := map[string]string{componentName: componentFile}
			variablesMap := map[string]map[string]interface{}{componentName: varsInterface}

			// Resolve dependencies that are not yet deployed in the environment
			deps, err := eng.ResolveDependencies(ctx, engine.DeployOptions{
				Environment: envName,
				Datacenter:  dc,
				Components:  componentsMap,
				Variables:   variablesMap,
			})
			if err != nil {
				return fmt.Errorf("failed to resolve dependencies: %w", err)
			}

			// Handle dependency variables - prompt or error
			for i := range deps {
				dep := &deps[i]
				if len(dep.MissingVariables) > 0 {
					if !isInteractive() {
						var names []string
						for _, v := range dep.MissingVariables {
							names = append(names, v.Name())
						}
						return fmt.Errorf("cannot auto-deploy dependency %q: missing required variables: %s\nProvide values with --var or deploy the dependency manually first",
							dep.Name, strings.Join(names, ", "))
					}

					// Prompt for dependency variables
					fmt.Printf("Dependency %q requires the following variables:\n", dep.Name)
					fmt.Println()

					depVars := make(map[string]interface{})
					for _, v := range dep.MissingVariables {
						value, err := promptForVariable(v)
						if err != nil {
							return fmt.Errorf("failed to read variable %q for dependency %q: %w", v.Name(), dep.Name, err)
						}
						depVars[v.Name()] = value
					}
					fmt.Println()
					variablesMap[dep.Name] = depVars
				}

				// Add dependency to the components map
				componentsMap[dep.Name] = dep.LocalPath
			}

			// Display auto-deployed dependencies
			if len(deps) > 0 {
				fmt.Println("Dependencies to deploy:")
				for _, dep := range deps {
					fmt.Printf("  %s (%s)\n", dep.Name, dep.OCIRef)
				}
				fmt.Println()
			}

			// Track if we've started provisioning (for cleanup purposes)
			provisioningStarted := false

			// cleanupEnvironment handles full cleanup of the environment
			cleanupEnvironment := func(reason string) {
				if !provisioningStarted {
					return
				}

				fmt.Println()
				fmt.Printf("Cleaning up (%s)...\n", reason)

				// Use a fresh context since the original may be cancelled
				cleanupCtx := context.Background()

				// Use the engine to destroy the environment
				cleanupEng := createEngine(mgr)
				_, err := cleanupEng.Destroy(cleanupCtx, engine.DestroyOptions{
					Environment: envName,
					Datacenter:  dc,
					Output:      os.Stdout,
					DryRun:      false,
					AutoApprove: true,
				})
				if err != nil {
					// Fall back to direct cleanup if engine destroy fails
					_ = CleanupByEnvName(cleanupCtx, envName)
				}

				// Delete environment state
				if err := mgr.DeleteEnvironment(cleanupCtx, dc, envName); err != nil {
					fmt.Printf("Warning: failed to delete environment state: %v\n", err)
				}

				fmt.Println("Cleanup complete.")
			}

			// Set up signal handling for graceful shutdown during provisioning
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
			var cancelCount int
			go func() {
				for {
					<-sigChan
					cancelCount++
					if cancelCount == 1 {
						fmt.Println("\nInterrupted, cancelling... (press Ctrl+C again to force quit)")
						cancel()
					} else {
						fmt.Println("\nForce quitting...")
						// Force cleanup by environment name
						_ = CleanupByEnvName(context.Background(), envName)
						os.Exit(1)
					}
				}
			}()

			// Create or get environment
			_, err = mgr.GetEnvironment(ctx, dc, envName)
			if err != nil {
				env := &types.EnvironmentState{
					Name:       envName,
					Datacenter: dc,
					Status:     types.EnvironmentStatusPending,
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
					Components: make(map[string]*types.ComponentState),
				}

				if err := mgr.SaveEnvironment(ctx, dc, env); err != nil {
					return fmt.Errorf("failed to create environment: %w", err)
				}
			}

			// Mark that provisioning has started (for cleanup purposes)
			provisioningStarted = true

			// Create progress table
			progress := NewProgressTable(os.Stdout)

			// Add dependency component resources to progress table first
			for _, dep := range deps {
				addComponentToProgressTable(progress, dep.Name, dep.Component)
			}

			// Add primary component resources to progress table
			addComponentToProgressTable(progress, componentName, comp)

			// Print initial progress table
			progress.PrintInitial()

			// Create progress callback for engine updates
			onProgress := func(event executor.ProgressEvent) {
				var status ResourceStatus
				switch event.Status {
				case "running":
					status = StatusInProgress
				case "completed":
					status = StatusCompleted
				case "failed":
					status = StatusFailed
				case "skipped":
					status = StatusSkipped
				default:
					status = StatusPending
				}

				if event.Error != nil {
					progress.SetError(event.NodeID, event.Error)
				} else {
					progress.UpdateStatus(event.NodeID, status, event.Message)
				}
				progress.PrintUpdate(event.NodeID)
			}

			// Execute deployment using the engine with parallelism
			// Note: We pass nil for Output to suppress the plan summary which would
			// interfere with the progress table's ANSI cursor management
			result, err := eng.Deploy(ctx, engine.DeployOptions{
				Environment: envName,
				Datacenter:  dc,
				Components:  componentsMap,
				Variables:   variablesMap,
				Output:      nil, // Suppress plan summary - progress table handles display
				DryRun:      false,
				AutoApprove: true,
				Parallelism: defaultParallelism, // Enable parallel execution
				OnProgress:  onProgress,
			})

			if err != nil {
				fmt.Fprintln(os.Stderr)
				fmt.Fprintln(os.Stderr, "Some resources may have been provisioned before the failure.")
				fmt.Fprintf(os.Stderr, "Inspect the current state with:\n  arcctl inspect %s\n\n", envName)
				fmt.Fprintf(os.Stderr, "Clean up with:\n  arcctl destroy environment %s\n\n", envName)
				return fmt.Errorf("deployment failed: %w", err)
			}

			if !result.Success {
				// Don't clean up on failure — preserve state so the user
				// can inspect what succeeded and what failed.
				fmt.Fprintln(os.Stderr)
				fmt.Fprintln(os.Stderr, "Some resources failed to deploy. Successful resources are still running.")
				fmt.Fprintf(os.Stderr, "Inspect the current state with:\n  arcctl inspect %s\n\n", envName)
				fmt.Fprintf(os.Stderr, "Clean up with:\n  arcctl destroy environment %s\n\n", envName)
				if result.Execution != nil && len(result.Execution.Errors) > 0 {
					return fmt.Errorf("deployment failed: %v", result.Execution.Errors[0])
				}
				return fmt.Errorf("deployment failed")
			}

			// Check if interrupted during deployment
			if ctx.Err() != nil {
				cleanupEnvironment("interrupted")
				return ctx.Err()
			}

			// Print final progress summary
			progress.PrintFinalSummary()

			// Get route URLs from the deployed state
			routeURLs := make(map[string]string)
			basePort := port
			if basePort == 0 {
				basePort = 8080
			}

			// Try to get route URLs from state
			updatedEnv, err := mgr.GetEnvironment(ctx, dc, envName)
			if err == nil {
				if compState, ok := updatedEnv.Components[componentName]; ok {
					for resName, resState := range compState.Resources {
						if resState.Type == "route" {
							if url, ok := resState.Outputs["url"].(string); ok {
								routeURLs[resName] = url
							}
						}
					}
				}
			}

			// If no routes found from state, construct default URL
			if len(routeURLs) == 0 && len(comp.Routes()) > 0 {
				for _, route := range comp.Routes() {
					routeURLs[route.Name()] = fmt.Sprintf("http://localhost:%d", basePort)
					break
				}
			}

			if len(routeURLs) > 0 {
				var primaryURL string
				for _, url := range routeURLs {
					primaryURL = url
					break
				}
				fmt.Printf("\nApplication running at %s\n", primaryURL)

				// Open browser unless --no-open is specified
				if !noOpen && !detach {
					openBrowserURL(primaryURL)
				}
			}

			if detach {
				fmt.Println()
				fmt.Println("Running in background. To stop:")
				fmt.Printf("  arcctl destroy environment %s\n", envName)
			} else {
				fmt.Println()
				fmt.Println("Press Ctrl+C to stop...")

				// Wait for context cancellation (already set up above)
				<-ctx.Done()

				// Clean up everything and remove the environment
				cleanupEnvironment("interrupted")
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to architect.yml if not in default location")
	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Datacenter to use for provisioning (uses default if not set)")
	cmd.Flags().StringVarP(&name, "name", "n", "", "Environment name (default: auto-generated from component name)")
	cmd.Flags().StringArrayVar(&variables, "var", nil, "Set a component variable (key=value)")
	cmd.Flags().StringVar(&varFile, "var-file", "", "Load variables from a file")
	cmd.Flags().BoolVar(&detach, "detach", false, "Run in background (don't watch for changes)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't open browser to application URL")
	cmd.Flags().IntVar(&port, "port", 0, "Override the port for local access (default: 8080)")

	return cmd
}

// openBrowser is an alias for the shared openBrowserURL utility (in browser.go)
// kept here for backward compatibility within this file. New code should use openBrowserURL directly.

// Helper functions for up command

func upCreateStateManager() (state.Manager, error) {
	// Use config file defaults with no CLI overrides
	return createStateManagerWithConfig("", nil)
}

func upParseVarFile(data []byte, vars map[string]string) error {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, "\"'")
			vars[key] = value
		}
	}
	return nil
}
