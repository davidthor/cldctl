package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/davidthor/cldctl/pkg/engine"
	"github.com/davidthor/cldctl/pkg/engine/executor"
	"github.com/davidthor/cldctl/pkg/engine/planner"
	"github.com/davidthor/cldctl/pkg/envfile"
	"github.com/davidthor/cldctl/pkg/logs"
	"github.com/davidthor/cldctl/pkg/schema/component"
	"github.com/davidthor/cldctl/pkg/schema/environment"
	"github.com/davidthor/cldctl/pkg/state"
	"github.com/davidthor/cldctl/pkg/state/types"
	"github.com/spf13/cobra"

	// Register log query adapters for post-deployment log streaming
	_ "github.com/davidthor/cldctl/pkg/logs/loki"
)

// resourceID generates a unique ID for a resource.
func resourceID(component, resourceType, name string) string {
	return fmt.Sprintf("%s/%s/%s", component, resourceType, name)
}

// upMode determines which mode the up command should run in.
type upMode int

const (
	upModeComponent   upMode = iota
	upModeEnvironment
)

func newUpCmd() *cobra.Command {
	var (
		componentFile string
		envFile       string
		name          string
		datacenter    string
		variables     []string
		varFile       string
		detach        bool
		noOpen        bool
		port          int
	)

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Deploy a component or environment to a local environment",
		Long: `The up command provides a streamlined experience for local development.
It deploys your component or environment with all dependencies to an
environment using the specified datacenter.

You can specify either a component or an environment file:
  -c, --component   Path to a component file or directory
  -e, --environment Path to an environment file

If neither flag is provided, the command auto-detects by looking for
cloud.component.yml or cloud.environment.yml in the current directory.

The up command:
  1. Parses your cloud.component.yml or cloud.environment.yml file
  2. Creates or uses an existing environment with the specified datacenter
  3. Provisions all required resources (databases, etc.) in parallel
  4. Builds and deploys your application(s)
  5. Watches for file changes and auto-reloads (unless --detach)
  6. Exposes routes for local access

Examples:
  # Component mode (single component)
  cldctl up -c ./my-app -d local
  cldctl up -c ./my-app -d local --var API_KEY=secret

  # Environment mode (multi-component)
  cldctl up -e cloud.environment.yml -d local
  cldctl up -e ./envs/dev.yml -d my-datacenter

  # Auto-detect mode (looks for cloud.component.yml or cloud.environment.yml in CWD)
  cldctl up -d local`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate mutually exclusive flags
			if componentFile != "" && envFile != "" {
				return fmt.Errorf("flags -c/--component and -e/--environment are mutually exclusive")
			}

			// Determine mode
			mode, resolvedPath, err := resolveUpMode(componentFile, envFile)
			if err != nil {
				return err
			}

			// Resolve datacenter
			dc, err := resolveDatacenter(datacenter)
			if err != nil {
				return err
			}

			// Parse CLI variable overrides
			cliVars := make(map[string]string)
			if varFile != "" {
				data, err := os.ReadFile(varFile)
				if err != nil {
					return fmt.Errorf("failed to read var file: %w", err)
				}
				if err := upParseVarFile(data, cliVars); err != nil {
					return fmt.Errorf("failed to parse var file: %w", err)
				}
			}
			for _, v := range variables {
				parts := strings.SplitN(v, "=", 2)
				if len(parts) == 2 {
					cliVars[parts[0]] = parts[1]
				}
			}

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
				return fmt.Errorf("datacenter %q not found: %w\nDeploy a datacenter first with: cldctl deploy datacenter <name> <path>", dc, err)
			}

			// Build component/variable maps and loaded components based on mode
			var (
				componentsMap map[string]string
				variablesMap  map[string]map[string]interface{}
				envName       string
				loadedComps   map[string]component.Component // for progress table
			)

			switch mode {
			case upModeComponent:
				componentsMap, variablesMap, envName, loadedComps, err = prepareComponentMode(ctx, resolvedPath, name, cliVars, dc, mgr)
			case upModeEnvironment:
				componentsMap, variablesMap, envName, loadedComps, err = prepareEnvironmentMode(resolvedPath, name, cliVars, dc)
			}
			if err != nil {
				return err
			}

			fmt.Printf("Datacenter: %s\n", dc)
			fmt.Printf("Environment: %s\n", envName)
			fmt.Println()

			// Create the engine
			eng := createEngine(mgr)

			// Set up signal handling for graceful shutdown during provisioning
			provisioningStarted := false
			cleanupEnvironment := makeCleanupFunc(&provisioningStarted, mgr, envName, dc)

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

			// Create progress table (populated from the real plan via OnPlan callback)
			progress := NewProgressTable(os.Stdout)

			// OnPlan populates the progress table from the real execution plan
			// so that dependency information is accurate and complete.
			onPlan := func(plan *planner.Plan) {
				populateProgressFromPlan(progress, plan)
				progress.PrintInitial()
			}

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

			// Capture logs from failed resources for error diagnostics
			if event.Logs != "" {
				progress.SetLogs(event.NodeID, event.Logs)
			}

			progress.PrintUpdate(event.NodeID)
		}

			// Execute deployment
			result, err := eng.Deploy(ctx, engine.DeployOptions{
				Environment: envName,
				Datacenter:  dc,
				Components:  componentsMap,
				Variables:   variablesMap,
				Output:      nil, // Suppress plan summary - progress table handles display
				DryRun:      false,
				AutoApprove: true,
				Parallelism: defaultParallelism,
				OnProgress:  onProgress,
				OnPlan:      onPlan,
			})

			// Always print the final progress summary so the user sees a clear
			// success/failure report with resource counts and error details.
			progress.PrintFinalSummary()

			if err != nil {
				cleanupEnvironment()
				// Include the underlying error so the user can diagnose failures
				// that occur before the execution plan is created (e.g., component
				// loading or datacenter config errors).
				return fmt.Errorf("deployment failed: %w", err)
			}

			if !result.Success {
				cleanupEnvironment()
				return fmt.Errorf("deployment failed")
			}

			// Check if interrupted during deployment
			if ctx.Err() != nil {
				cleanupEnvironment()
				return ctx.Err()
			}

			// Get route URLs from all deployed components
			routeURLs := collectRouteURLs(ctx, mgr, dc, envName, componentsMap, loadedComps, port)

			if len(routeURLs) > 0 {
				var primaryURL string
				for _, url := range routeURLs {
					primaryURL = url
					break
				}
				fmt.Printf("\nApplication running at %s\n", primaryURL)

				if !noOpen && !detach {
					openBrowserURL(primaryURL)
				}
			}

			if detach {
				fmt.Println()
				fmt.Println("Running in background. To stop:")
				fmt.Printf("  cldctl destroy environment %s\n", envName)
			} else {
				streaming := false

				// Check if observability/log streaming is available
				envState, envErr := mgr.GetEnvironment(ctx, dc, envName)
				if envErr == nil {
					queryType, queryEndpoint, obsErr := findObservabilityQueryConfig(envState)
					if obsErr == nil && isInteractive() {
						// Observability is available — ask user if they want to stream logs
						fmt.Println()
						fmt.Print("Stream logs for this environment? [Y/n]: ")
						var response string
						_, _ = fmt.Scanln(&response)
						response = strings.ToLower(strings.TrimSpace(response))

						if response == "" || response == "y" || response == "yes" {
							querier, qErr := logs.NewQuerier(queryType, queryEndpoint)
							if qErr == nil {
								fmt.Fprintf(os.Stderr, "\nStreaming logs (Ctrl+C to stop)...\n\n")
								stream, sErr := querier.Tail(ctx, logs.QueryOptions{
									Environment: envName,
								})
								if sErr == nil {
									streaming = true
									muxOpts := logs.MultiplexOptions{
										ShowTimestamps: false,
										NoColor:        false,
									}
									// FormatStream blocks until context cancels (Ctrl+C)
									_ = logs.FormatStream(os.Stdout, stream, muxOpts)
									stream.Close()
								}
							}
						} else {
							fmt.Println()
							fmt.Printf("You can stream logs at any time with: cldctl logs -e %s -f\n", envName)
							fmt.Printf("Or open the observability dashboard with: cldctl obs dashboard -e %s\n", envName)
						}
					}
				}

				if !streaming {
					fmt.Println()
					fmt.Println("Press Ctrl+C to stop...")
					<-ctx.Done()
				}

				cleanupEnvironment()
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&componentFile, "component", "c", "", "Path to component file or directory")
	cmd.Flags().StringVarP(&envFile, "environment", "e", "", "Path to environment file")
	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Datacenter to use for provisioning (uses default if not set)")
	cmd.Flags().StringVarP(&name, "name", "n", "", "Environment name (default: auto-generated)")
	cmd.Flags().StringArrayVar(&variables, "var", nil, "Set a variable (key=value)")
	cmd.Flags().StringVar(&varFile, "var-file", "", "Load variables from a file")
	cmd.Flags().BoolVar(&detach, "detach", false, "Run in background (don't watch for changes)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't open browser to application URL")
	cmd.Flags().IntVar(&port, "port", 0, "Override the port for local access (default: 8080)")

	return cmd
}

// resolveUpMode determines the mode and resolved file path for the up command.
func resolveUpMode(componentFile, envFile string) (upMode, string, error) {
	if componentFile != "" {
		absPath, err := filepath.Abs(componentFile)
		if err != nil {
			return 0, "", fmt.Errorf("failed to resolve component path: %w", err)
		}
		return upModeComponent, absPath, nil
	}

	if envFile != "" {
		absPath, err := filepath.Abs(envFile)
		if err != nil {
			return 0, "", fmt.Errorf("failed to resolve environment path: %w", err)
		}
		return upModeEnvironment, absPath, nil
	}

	// Auto-detect from CWD
	cwd, err := os.Getwd()
	if err != nil {
		return 0, "", fmt.Errorf("failed to get working directory: %w", err)
	}

	// Check for component files first
	for _, filename := range []string{"cloud.component.yml", "cloud.component.yaml"} {
		candidate := filepath.Join(cwd, filename)
		if _, err := os.Stat(candidate); err == nil {
			return upModeComponent, candidate, nil
		}
	}

	// Check for environment files
	for _, filename := range []string{"cloud.environment.yml", "cloud.environment.yaml"} {
		candidate := filepath.Join(cwd, filename)
		if _, err := os.Stat(candidate); err == nil {
			return upModeEnvironment, candidate, nil
		}
	}

	return 0, "", fmt.Errorf("no component or environment file found in current directory\nCreate a cloud.component.yml or cloud.environment.yml, or specify one with -c or -e")
}

// prepareComponentMode loads a single component and resolves its dependencies,
// returning the maps needed for engine.Deploy.
func prepareComponentMode(
	ctx context.Context,
	resolvedPath string,
	nameFlag string,
	cliVars map[string]string,
	dc string,
	mgr state.Manager,
) (
	componentsMap map[string]string,
	variablesMap map[string]map[string]interface{},
	envName string,
	loadedComps map[string]component.Component,
	err error,
) {
	// Determine the component file path
	componentFile := resolvedPath
	absDir := resolvedPath

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return nil, nil, "", nil, fmt.Errorf("failed to access path: %w", err)
	}
	if info.IsDir() {
		componentFile = filepath.Join(resolvedPath, "cloud.component.yml")
		if _, err := os.Stat(componentFile); os.IsNotExist(err) {
			componentFile = filepath.Join(resolvedPath, "cloud.component.yaml")
		}
	} else {
		absDir = filepath.Dir(resolvedPath)
	}

	// Load the component
	loader := component.NewLoader()
	comp, err := loader.Load(componentFile)
	if err != nil {
		return nil, nil, "", nil, fmt.Errorf("failed to load component: %w", err)
	}

	// Determine environment name
	envName = nameFlag
	if envName == "" {
		dirName := filepath.Base(absDir)
		envName = fmt.Sprintf("%s-dev", dirName)
	}

	// Convert CLI vars to interface map
	varsInterface := make(map[string]interface{})
	for k, v := range cliVars {
		varsInterface[k] = v
	}

	componentName := filepath.Base(absDir)
	componentsMap = map[string]string{componentName: componentFile}
	variablesMap = map[string]map[string]interface{}{componentName: varsInterface}
	loadedComps = map[string]component.Component{componentName: comp}

	fmt.Printf("Component: %s\n", componentName)

	// Resolve dependencies
	eng := createEngine(mgr)
	deps, err := eng.ResolveDependencies(ctx, engine.DeployOptions{
		Environment: envName,
		Datacenter:  dc,
		Components:  componentsMap,
		Variables:   variablesMap,
	})
	if err != nil {
		return nil, nil, "", nil, fmt.Errorf("failed to resolve dependencies: %w", err)
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
				return nil, nil, "", nil, fmt.Errorf("cannot auto-deploy dependency %q: missing required variables: %s\nProvide values with --var or deploy the dependency manually first",
					dep.Name, strings.Join(names, ", "))
			}

			fmt.Printf("Dependency %q requires the following variables:\n", dep.Name)
			fmt.Println()

			depVars := make(map[string]interface{})
			for _, v := range dep.MissingVariables {
				value, err := promptForVariable(v)
				if err != nil {
					return nil, nil, "", nil, fmt.Errorf("failed to read variable %q for dependency %q: %w", v.Name(), dep.Name, err)
				}
				depVars[v.Name()] = value
			}
			fmt.Println()
			variablesMap[dep.Name] = depVars
		}

		componentsMap[dep.Name] = dep.LocalPath
		loadedComps[dep.Name] = dep.Component
	}

	if len(deps) > 0 {
		fmt.Println("Dependencies to deploy:")
		for _, dep := range deps {
			fmt.Printf("  %s (%s)\n", dep.Name, dep.OCIRef)
		}
		fmt.Println()
	}

	return componentsMap, variablesMap, envName, loadedComps, nil
}

// prepareEnvironmentMode loads an environment file, resolves variables,
// and builds the component/variable maps needed for engine.Deploy.
func prepareEnvironmentMode(
	resolvedPath string,
	nameFlag string,
	cliVars map[string]string,
	dc string,
) (
	componentsMap map[string]string,
	variablesMap map[string]map[string]interface{},
	envName string,
	loadedComps map[string]component.Component,
	err error,
) {
	// Load the environment file
	envLoader := environment.NewLoader()
	envConfig, err := envLoader.Load(resolvedPath)
	if err != nil {
		return nil, nil, "", nil, fmt.Errorf("failed to load environment config: %w", err)
	}

	// Determine environment name: --name flag > config file name > directory-based default
	envName = nameFlag
	if envName == "" {
		envName = envConfig.Name()
	}
	if envName == "" {
		dirName := filepath.Base(filepath.Dir(resolvedPath))
		envName = fmt.Sprintf("%s-dev", dirName)
	}

	// Load dotenv file chain from the current working directory (consistent with
	// `update environment`). The .env files live alongside the user's project,
	// not necessarily alongside the environment file.
	envDir := filepath.Dir(resolvedPath)
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, "", nil, fmt.Errorf("failed to get working directory: %w", err)
	}
	dotenvVars, err := envfile.Load(cwd, envName)
	if err != nil {
		return nil, nil, "", nil, fmt.Errorf("failed to load .env files: %w", err)
	}

	// Resolve environment-level variables and substitute expressions
	if err := environment.ResolveVariables(envConfig.Internal(), environment.ResolveOptions{
		CLIVars:    cliVars,
		DotenvVars: dotenvVars,
		EnvName:    envName,
	}); err != nil {
		return nil, nil, "", nil, fmt.Errorf("failed to resolve environment variables: %w", err)
	}

	// Build component and variable maps from the environment config
	envComponents := envConfig.Components()
	componentsMap = make(map[string]string, len(envComponents))
	variablesMap = make(map[string]map[string]interface{}, len(envComponents))
	loadedComps = make(map[string]component.Component, len(envComponents))

	compLoader := component.NewLoader()

	for compName, compConfig := range envComponents {
		var source string
		if compConfig.Path() != "" {
			// Local path: resolve relative to the environment file directory
			source = filepath.Join(envDir, compConfig.Path())
		} else {
			// OCI image reference
			source = compConfig.Image()
		}

		componentsMap[compName] = source
		variablesMap[compName] = compConfig.Variables()

		// Load component for the progress table
		// For local paths, load directly; for OCI references the engine handles pulling
		if compConfig.Path() != "" {
			comp, err := compLoader.Load(source)
			if err != nil {
				return nil, nil, "", nil, fmt.Errorf("failed to load component %q from %s: %w", compName, source, err)
			}
			loadedComps[compName] = comp
		}
	}

	return componentsMap, variablesMap, envName, loadedComps, nil
}

// makeCleanupFunc creates the cleanup function used during shutdown.
// The cleanup runs quietly — no plan or per-resource progress is printed.
// Only errors (if any) and the final "Cleanup complete." line are shown.
func makeCleanupFunc(provisioningStarted *bool, mgr state.Manager, envName, dc string) func() {
	return func() {
		if !*provisioningStarted {
			return
		}

		fmt.Println()
		fmt.Println("Cleaning up...")

		cleanupCtx := context.Background()

		cleanupEng := createEngine(mgr)
		_, err := cleanupEng.Destroy(cleanupCtx, engine.DestroyOptions{
			Environment: envName,
			Datacenter:  dc,
			Output:      io.Discard,
			DryRun:      false,
			AutoApprove: true,
		})
		if err != nil {
			fmt.Printf("Warning: cleanup encountered an error: %v\n", err)
			_ = CleanupByEnvName(cleanupCtx, envName)
		}

		if err := mgr.DeleteEnvironment(cleanupCtx, dc, envName); err != nil {
			fmt.Printf("Warning: failed to delete environment state: %v\n", err)
		}

		fmt.Println("Cleanup complete.")
	}
}

// collectRouteURLs gathers route URLs from the deployed environment state.
func collectRouteURLs(
	ctx context.Context,
	mgr state.Manager,
	dc, envName string,
	componentsMap map[string]string,
	loadedComps map[string]component.Component,
	portOverride int,
) map[string]string {
	routeURLs := make(map[string]string)
	basePort := portOverride
	if basePort == 0 {
		basePort = 8080
	}

	// Try to get route URLs from state for all components
	updatedEnv, err := mgr.GetEnvironment(ctx, dc, envName)
	if err == nil {
		for compName := range componentsMap {
			if compState, ok := updatedEnv.Components[compName]; ok {
				for resName, resState := range compState.Resources {
					if resState.Type == "route" {
						if url, ok := resState.Outputs["url"].(string); ok {
							routeURLs[fmt.Sprintf("%s/%s", compName, resName)] = url
						}
					}
				}
			}
		}
	}

	// If no routes found from state, construct default URLs from loaded components
	if len(routeURLs) == 0 {
		for compName, comp := range loadedComps {
			for _, route := range comp.Routes() {
				routeURLs[fmt.Sprintf("%s/%s", compName, route.Name())] = fmt.Sprintf("http://localhost:%d", basePort)
				break
			}
		}
	}

	return routeURLs
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
