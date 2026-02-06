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
			fmt.Printf("Datacenter: %s\n", datacenter)
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
			dc, err := mgr.GetDatacenter(ctx, datacenter)
			if err != nil {
				return fmt.Errorf("datacenter %q not found: %w\nDeploy a datacenter first with: arcctl deploy datacenter <name> <path>", datacenter, err)
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
				eng := createEngine(mgr)
				_, err := eng.Destroy(cleanupCtx, engine.DestroyOptions{
					Environment: envName,
					Output:      os.Stdout,
					DryRun:      false,
					AutoApprove: true,
				})
				if err != nil {
					// Fall back to direct cleanup if engine destroy fails
					_ = CleanupByEnvName(cleanupCtx, envName)
				}

				// Delete environment state
				if err := mgr.DeleteEnvironment(cleanupCtx, envName); err != nil {
					fmt.Printf("Warning: failed to delete environment state: %v\n", err)
				}

				// Update datacenter to remove environment reference
				if dc, err := mgr.GetDatacenter(cleanupCtx, datacenter); err == nil {
					newEnvs := make([]string, 0, len(dc.Environments))
					for _, e := range dc.Environments {
						if e != envName {
							newEnvs = append(newEnvs, e)
						}
					}
					dc.Environments = newEnvs
					dc.UpdatedAt = time.Now()
					_ = mgr.SaveDatacenter(cleanupCtx, dc)
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
			env, err := mgr.GetEnvironment(ctx, envName)
			if err != nil {
				env = &types.EnvironmentState{
					Name:       envName,
					Datacenter: datacenter,
					Status:     types.EnvironmentStatusPending,
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
					Components: make(map[string]*types.ComponentState),
				}

				// Add to datacenter only if not already present (avoid duplicates)
				envExists := false
				for _, e := range dc.Environments {
					if e == envName {
						envExists = true
						break
					}
				}
				if !envExists {
					dc.Environments = append(dc.Environments, envName)
					dc.UpdatedAt = time.Now()
					if err := mgr.SaveDatacenter(ctx, dc); err != nil {
						return fmt.Errorf("failed to update datacenter: %w", err)
					}
				}

				if err := mgr.SaveEnvironment(ctx, env); err != nil {
					return fmt.Errorf("failed to create environment: %w", err)
				}
			}

			// Mark that provisioning has started (for cleanup purposes)
			provisioningStarted = true

			// Create progress table
			progress := NewProgressTable(os.Stdout)

			// Build dependency lists for progress tracking
			var dbDeps []string
			for _, db := range comp.Databases() {
				id := resourceID(componentName, "database", db.Name())
				progress.AddResource(id, db.Name(), "database", componentName, nil)
				dbDeps = append(dbDeps, id)
			}

			// Buckets have no dependencies
			for _, bucket := range comp.Buckets() {
				id := resourceID(componentName, "bucket", bucket.Name())
				progress.AddResource(id, bucket.Name(), "bucket", componentName, nil)
			}

			// Functions depend on databases (and their own builds if they have one)
			for _, fn := range comp.Functions() {
				fnDeps := dbDeps
				// Add build dependency if function has a container with build
				if fn.IsContainerBased() && fn.Container() != nil && fn.Container().Build() != nil {
					buildID := resourceID(componentName, "dockerBuild", fn.Name()+"-build")
					progress.AddResource(buildID, fn.Name()+"-build", "build", componentName, nil)
					fnDeps = append(fnDeps, buildID)
				}
				id := resourceID(componentName, "function", fn.Name())
				progress.AddResource(id, fn.Name(), "function", componentName, fnDeps)
			}

			// Add top-level build resources
			buildIDs := make(map[string]string)
			for _, build := range comp.Builds() {
				buildID := resourceID(componentName, "dockerBuild", build.Name())
				progress.AddResource(buildID, build.Name(), "build", componentName, nil)
				buildIDs[build.Name()] = buildID
			}

			// Deployments depend on databases (and any builds they reference via expressions)
			for _, depl := range comp.Deployments() {
				deplDeps := dbDeps
				// If deployment image references a build, add it as a dependency
				for buildName, buildID := range buildIDs {
					if strings.Contains(depl.Image(), fmt.Sprintf("builds.%s.", buildName)) {
						deplDeps = append(deplDeps, buildID)
					}
				}
				id := resourceID(componentName, "deployment", depl.Name())
				progress.AddResource(id, depl.Name(), "deployment", componentName, deplDeps)
			}

			// Services have no dependencies - they can be created in parallel with deployments
			// (In Kubernetes and similar platforms, a Service is a networking abstraction
			// that doesn't require the underlying pods to exist yet)
			for _, svc := range comp.Services() {
				id := resourceID(componentName, "service", svc.Name())
				progress.AddResource(id, svc.Name(), "service", componentName, nil)
			}

			// Routes have no dependencies - they can be created in parallel with everything
			// (Routes are ingress configuration that can exist before backends are ready)
			for _, route := range comp.Routes() {
				id := resourceID(componentName, "route", route.Name())
				progress.AddResource(id, route.Name(), "route", componentName, nil)
			}

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

			// Create the engine
			eng := createEngine(mgr)

			// Convert vars to interface{} map
			varsInterface := make(map[string]interface{})
			for k, v := range vars {
				varsInterface[k] = v
			}

			// Execute deployment using the engine with parallelism
			// Note: We pass nil for Output to suppress the plan summary which would
			// interfere with the progress table's ANSI cursor management
			result, err := eng.Deploy(ctx, engine.DeployOptions{
				Environment: envName,
				Datacenter:  datacenter,
				Components:  map[string]string{componentName: componentFile},
				Variables:   map[string]map[string]interface{}{componentName: varsInterface},
				Output:      nil, // Suppress plan summary - progress table handles display
				DryRun:      false,
				AutoApprove: true,
				Parallelism: defaultParallelism, // Enable parallel execution
				OnProgress:  onProgress,
			})

			if err != nil {
				cleanupEnvironment("deployment failed")
				return fmt.Errorf("deployment failed: %w", err)
			}

			if !result.Success {
				cleanupEnvironment("deployment failed")
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
			updatedEnv, err := mgr.GetEnvironment(ctx, envName)
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
	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Datacenter to use for provisioning (required)")
	cmd.Flags().StringVarP(&name, "name", "n", "", "Environment name (default: auto-generated from component name)")
	cmd.Flags().StringArrayVar(&variables, "var", nil, "Set a component variable (key=value)")
	cmd.Flags().StringVar(&varFile, "var-file", "", "Load variables from a file")
	cmd.Flags().BoolVar(&detach, "detach", false, "Run in background (don't watch for changes)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't open browser to application URL")
	cmd.Flags().IntVar(&port, "port", 0, "Override the port for local access (default: 8080)")
	_ = cmd.MarkFlagRequired("datacenter")

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
