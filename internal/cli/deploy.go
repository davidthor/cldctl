package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/davidthor/cldctl/pkg/engine"
	"github.com/davidthor/cldctl/pkg/engine/executor"
	"github.com/davidthor/cldctl/pkg/engine/planner"
	"github.com/davidthor/cldctl/pkg/oci"
	"github.com/davidthor/cldctl/pkg/registry"
	"github.com/davidthor/cldctl/pkg/schema/component"
	"github.com/davidthor/cldctl/pkg/state"
	"github.com/davidthor/cldctl/pkg/state/types"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newDeployCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy resources",
		Long:  `Commands for deploying components and datacenters.`,
	}

	cmd.AddCommand(newDeployComponentCmd())
	cmd.AddCommand(newDeployDatacenterCmd())

	return cmd
}

func newDeployComponentCmd() *cobra.Command {
	var (
		environment   string
		datacenter    string
		variables     []string
		varFile       string
		autoApprove   bool
		targets       []string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:     "component <image>",
		Aliases: []string{"comp", "comps", "components"},
		Short:   "Deploy a component to an environment or register it with a datacenter",
		Long: `Deploy a component to an environment, or register it as a datacenter-level
component declaration when --environment is omitted.

The image must be a reference to a component artifact in the local cache
(built with 'cldctl build component' or pulled with 'cldctl pull component').
If the image is not cached locally, it will be pulled from the remote registry
automatically.

When -e is provided, the component is deployed into the target environment
with full resource provisioning.

When -e is omitted, the component is registered as a datacenter-level
declaration. Datacenter components are automatically deployed into
environments when needed as dependencies by other components.

In interactive mode (when not running in CI), you will be prompted to enter
values for any required variables that were not provided via --var or --var-file.

Examples:
  cldctl deploy component ghcr.io/myorg/myapp:v1.0.0 -e production
  cldctl deploy component myapp:latest -e staging -d my-dc
  cldctl deploy component ghcr.io/myorg/myapp:v1.0.0 -e production --var api_key=secret123
  cldctl deploy component myorg/stripe:latest -d my-dc --var key=sk_live_xxx`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			imageRef := args[0]
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

			// If no environment specified, register as datacenter-level component
			if environment == "" {
				return deployDatacenterComponent(ctx, mgr, dc, imageRef, variables, varFile)
			}

			// Verify environment exists
			envState, err := mgr.GetEnvironment(ctx, dc, environment)
			if err != nil {
				return fmt.Errorf("environment %q not found in datacenter %q: %w", environment, dc, err)
			}

			// Load variables from file if specified
			vars := make(map[string]string)
			if varFile != "" {
				data, err := os.ReadFile(varFile)
				if err != nil {
					return fmt.Errorf("failed to read var file: %w", err)
				}
				if err := parseVarFile(data, vars); err != nil {
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

			// Derive component name from image reference
			componentName := deriveComponentName(imageRef, false)

			// Resolve image: load from local cache or pull from remote
			reg, err := registry.NewRegistry()
			if err != nil {
				return fmt.Errorf("failed to open local registry: %w", err)
			}

			var componentPath string
			entry, err := reg.Get(imageRef)
			if err == nil && entry != nil && entry.CachePath != "" {
				// Found in local cache — find the component file
				compFile := findComponentFile(entry.CachePath)
				if compFile != "" {
					componentPath = compFile
				}
			}

			if componentPath == "" {
				// Not in local cache or cache is stale — pull from remote
				fmt.Printf("[pull] Downloading %s...\n", imageRef)
				client := oci.NewClient()

				compDir, err := registry.CachePathForRef(imageRef)
				if err != nil {
					return fmt.Errorf("failed to compute cache path: %w", err)
				}

				os.RemoveAll(compDir)
				if err := os.MkdirAll(compDir, 0755); err != nil {
					return fmt.Errorf("failed to create cache directory: %w", err)
				}

				if err := client.Pull(ctx, imageRef, compDir); err != nil {
					os.RemoveAll(compDir)
					return fmt.Errorf("failed to pull component: %w", err)
				}

				// Calculate size
				var totalSize int64
				_ = filepath.Walk(compDir, func(_ string, info os.FileInfo, walkErr error) error {
					if walkErr != nil {
						return walkErr
					}
					if !info.IsDir() {
						totalSize += info.Size()
					}
					return nil
				})

				// Register in local cache
				repo, tagPortion := registry.ParseReference(imageRef)
				compEntry := registry.ArtifactEntry{
					Reference:  imageRef,
					Repository: repo,
					Tag:        tagPortion,
					Type:       registry.TypeComponent,
					Size:       totalSize,
					CreatedAt:  time.Now(),
					CachePath:  compDir,
				}
				if err := reg.Add(compEntry); err != nil {
					return fmt.Errorf("failed to register component: %w", err)
				}

				compFile := findComponentFile(compDir)
				if compFile == "" {
					return fmt.Errorf("no cloud.component.yml found in artifact %s", imageRef)
				}
				componentPath = compFile
				fmt.Printf("[pull] Cached %s\n", imageRef)
			}

			// Load component from resolved cache path for variable prompts and plan display
			var comp component.Component
			loader := component.NewLoader()
			comp, err = loader.Load(componentPath)
			if err != nil {
				return fmt.Errorf("failed to load component: %w", err)
			}

			// Prompt for missing variables if running interactively
			if isInteractive() {
				missingVars := getMissingVariables(comp, vars)
				if len(missingVars) > 0 {
					fmt.Println("The following variables need values:")
					fmt.Println()

					for _, v := range missingVars {
						value, err := promptForVariable(v)
						if err != nil {
							return fmt.Errorf("failed to read variable %q: %w", v.Name(), err)
						}
						vars[v.Name()] = value
					}
					fmt.Println()
				}
			}

			// Validate all required variables are set
			missingRequired := getMissingRequiredVariables(comp, vars)
			if len(missingRequired) > 0 {
				var names []string
				for _, v := range missingRequired {
					names = append(names, v.Name())
				}
				return fmt.Errorf("missing required variables: %s\nUse --var or --var-file to provide values, or run interactively", strings.Join(names, ", "))
			}

			// Create the engine early so we can use it for dependency resolution
			eng := createEngine(mgr)

			// Convert vars to interface{} map
			varsInterface := make(map[string]interface{})
			for k, v := range vars {
				varsInterface[k] = v
			}

			// Build initial component and variable maps
			componentsMap := map[string]string{componentName: componentPath}
			variablesMap := map[string]map[string]interface{}{componentName: varsInterface}

			// Resolve dependencies that are not yet deployed in the environment
			deps, err := eng.ResolveDependencies(ctx, engine.DeployOptions{
				Environment: environment,
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
					if !isInteractive() || autoApprove {
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

			// Display execution plan
			fmt.Printf("Component:   %s\n", componentName)
			fmt.Printf("Environment: %s\n", environment)
			fmt.Printf("Datacenter:  %s\n", dc)
			fmt.Printf("Image:       %s\n", imageRef)
			fmt.Println()

			fmt.Println("Execution Plan:")
			fmt.Println()

			// Show resources that will be created
			planCount := 0

			for _, db := range comp.Databases() {
				fmt.Printf("  database %q (%s)\n", db.Name(), db.Type())
				fmt.Printf("    + create: Database %q\n\n", fmt.Sprintf("%s-%s-%s", environment, componentName, db.Name()))
				planCount++
			}

			for _, depl := range comp.Deployments() {
				fmt.Printf("  deployment %q\n", depl.Name())
				fmt.Printf("    + create: Deployment %q\n\n", fmt.Sprintf("%s-%s-%s", environment, componentName, depl.Name()))
				planCount++
			}

			for _, svc := range comp.Services() {
				fmt.Printf("  service %q\n", svc.Name())
				fmt.Printf("    + create: Service %q\n\n", fmt.Sprintf("%s-%s-%s", environment, componentName, svc.Name()))
				planCount++
			}

			for _, route := range comp.Routes() {
				fmt.Printf("  route %q\n", route.Name())
				fmt.Printf("    + create: Route %q\n\n", fmt.Sprintf("%s-%s-%s", environment, componentName, route.Name()))
				planCount++
			}

			fmt.Printf("Plan: %d to create, 0 to update, 0 to destroy\n", planCount)
			fmt.Println()

			// Handle targets filter
			_ = targets
			_ = envState

			// Confirm unless --auto-approve is provided
			if !autoApprove && isInteractive() {
				fmt.Print("Proceed with deployment? [Y/n]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "" && response != "y" && response != "yes" {
					fmt.Println("Deployment cancelled.")
					return nil
				}
			}

			fmt.Println()

			// Build progress table (populated from the real plan via OnPlan callback)
			progress := NewProgressTable(os.Stdout)

			// OnPlan populates the progress table from the real execution plan
			onPlan := func(plan *planner.Plan) {
				populateProgressFromPlan(progress, plan)
				progress.PrintInitial()
			}

			// Create progress callback
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

			// Execute deployment using the engine
			result, err := eng.Deploy(ctx, engine.DeployOptions{
				Environment: environment,
				Datacenter:  dc,
				Components:  componentsMap,
				Variables:   variablesMap,
				Output:      os.Stdout,
				DryRun:      false,
				AutoApprove: autoApprove,
				Parallelism: defaultParallelism,
				OnProgress:  onProgress,
				OnPlan:      onPlan,
			})
			// Always print the final progress summary so the user sees a clear
			// success/failure report with resource counts and error details.
			progress.PrintFinalSummary()

			if err != nil {
				return fmt.Errorf("deployment failed: %w", err)
			}

			if !result.Success {
				if result.Execution != nil && len(result.Execution.Errors) > 0 {
					return fmt.Errorf("deployment failed with %d errors: %v", len(result.Execution.Errors), result.Execution.Errors[0])
				}
				return fmt.Errorf("deployment failed")
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (omit to register as datacenter component)")
	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Target datacenter (uses default if not set)")
	cmd.Flags().StringArrayVar(&variables, "var", nil, "Set variable (key=value)")
	cmd.Flags().StringVar(&varFile, "var-file", "", "Load variables from file")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().StringArrayVar(&targets, "target", nil, "Target specific resource (repeatable)")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}

// deployDatacenterComponent registers a component declaration at the datacenter level.
// The component is not deployed immediately -- it is stored so the engine can
// automatically deploy it into environments when needed as a dependency.
func deployDatacenterComponent(ctx context.Context, mgr state.Manager, dc, imageRef string, variables []string, varFile string) error {
	// Verify the datacenter exists
	_, err := mgr.GetDatacenter(ctx, dc)
	if err != nil {
		return fmt.Errorf("datacenter %q not found: %w", dc, err)
	}

	// Parse the image reference into component name and version tag.
	// For OCI references like "myorg/stripe:latest", the name is "myorg/stripe" and source is "latest".
	parts := strings.SplitN(imageRef, ":", 2)
	componentName := parts[0]
	componentSource := "latest"
	if len(parts) == 2 {
		componentSource = parts[1]
	}

	// Load variables from file if specified
	vars := make(map[string]string)
	if varFile != "" {
		data, err := os.ReadFile(varFile)
		if err != nil {
			return fmt.Errorf("failed to read var file: %w", err)
		}
		if err := parseVarFile(data, vars); err != nil {
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

	// Save the datacenter component state
	compConfig := &types.DatacenterComponentConfig{
		Name:      componentName,
		Source:    componentSource,
		Variables: vars,
	}

	if err := mgr.SaveDatacenterComponent(ctx, dc, compConfig); err != nil {
		return fmt.Errorf("failed to save datacenter component: %w", err)
	}

	fmt.Printf("[success] Registered component %q in datacenter %q\n", componentName, dc)
	fmt.Printf("  Source: %s\n", componentSource)
	if len(vars) > 0 {
		fmt.Printf("  Variables: %d\n", len(vars))
	}
	fmt.Println()
	fmt.Println("This component will be automatically deployed into environments when")
	fmt.Println("needed as a dependency by other components.")

	return nil
}

func newDeployDatacenterCmd() *cobra.Command {
	var (
		variables     []string
		varFile       string
		autoApprove   bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:     "datacenter <name> <image>",
		Aliases: []string{"dc", "dcs", "datacenters"},
		Short:   "Deploy a datacenter",
		Long: `Deploy or update a datacenter from a built or pulled image.

The image must be a reference to a datacenter artifact in the local cache
(built with 'cldctl build datacenter' or pulled with 'cldctl pull datacenter').
If the image is not cached locally, it will be pulled from the remote registry
automatically.

Arguments:
  name    Name for the deployed datacenter
  image   Datacenter image reference (e.g., my-dc:latest, davidthor/local-datacenter)

Examples:
  cldctl deploy datacenter local davidthor/local-datacenter
  cldctl deploy datacenter prod-dc ghcr.io/myorg/dc:v1.0.0
  cldctl deploy datacenter my-dc my-dc:latest`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			dcName := args[0]
			imageRef := args[1]
			ctx := context.Background()

			// Create state manager
			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Load variables from file if specified
			vars := make(map[string]string)
			if varFile != "" {
				data, err := os.ReadFile(varFile)
				if err != nil {
					return fmt.Errorf("failed to read var file: %w", err)
				}
				if err := parseVarFile(data, vars); err != nil {
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

			// Display execution plan
			fmt.Printf("Datacenter: %s\n", dcName)
			fmt.Printf("Image:      %s\n", imageRef)
			fmt.Println()

			// Resolve the image: check local cache first, then pull from remote
			reg, err := registry.NewRegistry()
			if err != nil {
				return fmt.Errorf("failed to open local registry: %w", err)
			}

			entry, err := reg.Get(imageRef)
			if err != nil || entry == nil || entry.CachePath == "" {
				// Not in local cache — pull from remote
				fmt.Printf("[pull] Downloading %s...\n", imageRef)
				client := oci.NewClient()

				dcDir, err := registry.CachePathForRef(imageRef)
				if err != nil {
					return fmt.Errorf("failed to compute cache path: %w", err)
				}

				os.RemoveAll(dcDir)
				if err := os.MkdirAll(dcDir, 0755); err != nil {
					return fmt.Errorf("failed to create cache directory: %w", err)
				}

				if err := client.Pull(ctx, imageRef, dcDir); err != nil {
					os.RemoveAll(dcDir)
					return fmt.Errorf("failed to pull datacenter: %w", err)
				}

				// Calculate size
				var totalSize int64
				_ = filepath.Walk(dcDir, func(_ string, info os.FileInfo, walkErr error) error {
					if walkErr != nil {
						return walkErr
					}
					if !info.IsDir() {
						totalSize += info.Size()
					}
					return nil
				})

				// Register in local cache
				repo, tagPortion := registry.ParseReference(imageRef)
				dcEntry := registry.ArtifactEntry{
					Reference:  imageRef,
					Repository: repo,
					Tag:        tagPortion,
					Type:       registry.TypeDatacenter,
					Size:       totalSize,
					CreatedAt:  time.Now(),
					CachePath:  dcDir,
				}
				if err := reg.Add(dcEntry); err != nil {
					return fmt.Errorf("failed to register datacenter: %w", err)
				}

				fmt.Printf("[pull] Cached %s\n", imageRef)
			} else {
				// Verify cached content still exists on disk
				dcFile := findDatacenterFile(entry.CachePath)
				if dcFile == "" {
					return fmt.Errorf("cached artifact for %s is missing from disk; try: cldctl pull datacenter %s", imageRef, imageRef)
				}
				fmt.Printf("[cache] Using locally cached %s\n", imageRef)
			}

			// Confirm unless --auto-approve is provided
			if !autoApprove {
				fmt.Print("Proceed? [Y/n]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "" && response != "y" && response != "yes" {
					fmt.Println("Deployment cancelled.")
					return nil
				}
			}

			fmt.Println()
			fmt.Printf("[deploy] Deploying datacenter %q...\n", dcName)

			// Save datacenter state
			dcState := &types.DatacenterState{
				Name:      dcName,
				Version:   imageRef,
				Source:    imageRef,
				Variables: vars,
				Modules:   make(map[string]*types.ModuleState),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			if err := mgr.SaveDatacenter(ctx, dcState); err != nil {
				return fmt.Errorf("failed to save datacenter state: %w", err)
			}

			// Auto-set default datacenter in CLI config
			if err := setDefaultDatacenter(dcName); err != nil {
				// Non-fatal: warn but don't fail the deploy
				fmt.Printf("Warning: failed to set default datacenter in config: %v\n", err)
			} else {
				fmt.Printf("[config] Default datacenter set to %q\n", dcName)
			}

			// Execute root-level modules and reconcile environments
			eng := createEngine(mgr)

			dcResult, err := eng.DeployDatacenter(ctx, engine.DeployDatacenterOptions{
				Datacenter:  dcName,
				Output:      os.Stdout,
				Parallelism: defaultParallelism,
			})
			if err != nil {
				return fmt.Errorf("failed to provision datacenter infrastructure: %w", err)
			}
			if !dcResult.Success {
				return fmt.Errorf("datacenter infrastructure provisioning failed")
			}

			fmt.Printf("[success] Datacenter %q deployed from %s\n", dcName, imageRef)
			fmt.Println()
			fmt.Println("The datacenter is now available for use with environments.")

			return nil
		},
	}

	cmd.Flags().StringArrayVar(&variables, "var", nil, "Set variable (key=value)")
	cmd.Flags().StringVar(&varFile, "var-file", "", "Load variables from file")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}

// Helper functions used by deploy commands - these are duplicated from component.go
// to avoid circular dependencies. In a future refactor, these could be moved to a
// shared helpers package.

// deriveComponentName extracts a component name from the source.
// For local paths, it uses the directory name.
// For OCI references, it uses the repository name without the registry prefix.
func deriveComponentName(source string, isLocalPath bool) string {
	if isLocalPath {
		// Remove trailing slashes
		source = strings.TrimRight(source, "/")
		// If it's a file path (cloud.component.yml), get the parent directory
		if strings.HasSuffix(source, ".yml") || strings.HasSuffix(source, ".yaml") {
			source = filepath.Dir(source)
		}
		// Get the base directory name
		name := filepath.Base(source)
		// Handle "." case
		if name == "." {
			absPath, err := filepath.Abs(source)
			if err == nil {
				name = filepath.Base(absPath)
			}
		}
		return name
	}

	// OCI reference: extract repository name
	// e.g., ghcr.io/myorg/myapp:v1.0.0 -> myapp
	// e.g., docker.io/library/nginx:latest -> nginx
	ref := source

	// Remove tag/digest
	if idx := strings.LastIndex(ref, ":"); idx != -1 {
		ref = ref[:idx]
	}
	if idx := strings.LastIndex(ref, "@"); idx != -1 {
		ref = ref[:idx]
	}

	// Get the last path segment (repository name)
	if idx := strings.LastIndex(ref, "/"); idx != -1 {
		ref = ref[idx+1:]
	}

	return ref
}

// findComponentFile looks for a component config file in the given directory.
// Returns the path to the file if found, or empty string if not.
func findComponentFile(dir string) string {
	ymlFile := filepath.Join(dir, "cloud.component.yml")
	if _, err := os.Stat(ymlFile); err == nil {
		return ymlFile
	}
	yamlFile := filepath.Join(dir, "cloud.component.yaml")
	if _, err := os.Stat(yamlFile); err == nil {
		return yamlFile
	}
	return ""
}

// isInteractive returns true if the CLI is running in an interactive terminal
// and not in a CI environment.
func isInteractive() bool {
	// Check if stdin is a terminal
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}

	// Check for common CI environment variables
	ciEnvVars := []string{
		"CI",
		"CONTINUOUS_INTEGRATION",
		"GITHUB_ACTIONS",
		"GITLAB_CI",
		"CIRCLECI",
		"TRAVIS",
		"JENKINS_URL",
		"BUILDKITE",
		"DRONE",
		"TEAMCITY_VERSION",
		"TF_BUILD",           // Azure DevOps
		"BITBUCKET_BUILD_NUMBER",
		"CODEBUILD_BUILD_ID", // AWS CodeBuild
	}

	for _, env := range ciEnvVars {
		if os.Getenv(env) != "" {
			return false
		}
	}

	return true
}

// getMissingVariables returns variables that need values (required without defaults,
// or all variables without values if running interactively).
func getMissingVariables(comp component.Component, providedVars map[string]string) []component.Variable {
	var missing []component.Variable

	for _, v := range comp.Variables() {
		// Skip if already provided
		if _, ok := providedVars[v.Name()]; ok {
			continue
		}

		// Skip if has a default value
		if v.Default() != nil {
			continue
		}

		// Include if required
		if v.Required() {
			missing = append(missing, v)
		}
	}

	return missing
}

// getMissingRequiredVariables returns required variables that don't have values.
func getMissingRequiredVariables(comp component.Component, providedVars map[string]string) []component.Variable {
	var missing []component.Variable

	for _, v := range comp.Variables() {
		if !v.Required() {
			continue
		}

		// Check if provided
		if _, ok := providedVars[v.Name()]; ok {
			continue
		}

		// Check if has default
		if v.Default() != nil {
			continue
		}

		missing = append(missing, v)
	}

	return missing
}

// promptForVariable prompts the user to enter a value for a variable.
func promptForVariable(v component.Variable) (string, error) {
	// Show variable name and description
	prompt := fmt.Sprintf("  %s", v.Name())
	if v.Description() != "" {
		prompt += fmt.Sprintf(" (%s)", v.Description())
	}
	if v.Required() {
		prompt += " [required]"
	}
	prompt += ": "

	fmt.Print(prompt)

	// Read input - use password-style input for sensitive variables
	if v.Sensitive() {
		// Read password without echo
		bytePassword, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println() // Add newline after hidden input
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(bytePassword)), nil
	}

	// Regular input
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(input), nil
}

// parseVarFile parses a variable file with KEY=value format.
func parseVarFile(data []byte, vars map[string]string) error {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// Remove quotes if present
			value = strings.Trim(value, "\"'")
			vars[key] = value
		}
	}
	return nil
}

// populateProgressFromPlan populates a progress table from the real execution plan,
// using the actual dependency graph instead of a simplified approximation.
func populateProgressFromPlan(progress *ProgressTable, plan *planner.Plan) {
	for _, change := range plan.Changes {
		node := change.Node
		progress.AddResource(node.ID, node.Name, string(node.Type), node.Component, node.DependsOn)
	}
}

// copyDirectory copies the contents of srcDir into destDir, excluding
// hidden files/directories and common build artifacts. destDir must
// already exist.
func copyDirectory(srcDir, destDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden files/directories
		name := info.Name()
		if strings.HasPrefix(name, ".") && path != srcDir {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip common build artifacts
		if info.IsDir() {
			switch name {
			case "node_modules", "__pycache__", ".terraform", ".pulumi":
				return filepath.SkipDir
			}
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(destDir, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(destPath, data, info.Mode())
	})
}
