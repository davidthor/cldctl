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

	"github.com/architect-io/arcctl/pkg/engine"
	"github.com/architect-io/arcctl/pkg/engine/executor"
	"github.com/architect-io/arcctl/pkg/oci"
	"github.com/architect-io/arcctl/pkg/registry"
	"github.com/architect-io/arcctl/pkg/schema/component"
	"github.com/architect-io/arcctl/pkg/schema/datacenter"
	"github.com/architect-io/arcctl/pkg/state/types"
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
		variables     []string
		varFile       string
		autoApprove   bool
		targets       []string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:     "component <source>",
		Aliases: []string{"comp", "comps", "components"},
		Short:   "Deploy a component to an environment",
		Long: `Deploy a component to an environment.

The source can be specified as either:
  - An OCI image reference (e.g., ghcr.io/myorg/myapp:v1.0.0)
  - A local directory containing an architect.yml file
  - A path to an architect.yml file directly

When deploying from local source, arcctl will build container images as needed.

In interactive mode (when not running in CI), you will be prompted to enter
values for any required variables that were not provided via --var or --var-file.

Examples:
  arcctl deploy component ./my-app -e production
  arcctl deploy component ./my-app/architect.yml -e staging
  arcctl deploy component ghcr.io/myorg/myapp:v1.0.0 -e production
  arcctl deploy component ./my-app -e production --var api_key=secret123`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]
			ctx := context.Background()

			// Create state manager
			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Verify environment exists
			envState, err := mgr.GetEnvironment(ctx, environment)
			if err != nil {
				return fmt.Errorf("environment %q not found: %w", environment, err)
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

			// Check if this is an OCI reference or local path
			isLocalPath := !strings.Contains(source, ":") || strings.HasPrefix(source, "./") || strings.HasPrefix(source, "/")

			// Derive component name from source
			componentName := deriveComponentName(source, isLocalPath)

			// Load component to get variable definitions
			var comp component.Component
			if isLocalPath {
				// Determine the path to architect.yml
				componentFile := source
				if !strings.HasSuffix(source, ".yml") && !strings.HasSuffix(source, ".yaml") {
					componentFile = filepath.Join(source, "architect.yml")
				}

				// Load component from local path
				loader := component.NewLoader()
				comp, err = loader.Load(componentFile)
				if err != nil {
					return fmt.Errorf("failed to load component: %w", err)
				}
			}

			// Prompt for missing variables if running interactively
			if comp != nil && isInteractive() {
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
			if comp != nil {
				missingRequired := getMissingRequiredVariables(comp, vars)
				if len(missingRequired) > 0 {
					var names []string
					for _, v := range missingRequired {
						names = append(names, v.Name())
					}
					return fmt.Errorf("missing required variables: %s\nUse --var or --var-file to provide values, or run interactively", strings.Join(names, ", "))
				}
			}

			// Display execution plan
			fmt.Printf("Component:   %s\n", componentName)
			fmt.Printf("Environment: %s\n", environment)
			fmt.Printf("Source:      %s\n", source)
			fmt.Println()

			fmt.Println("Execution Plan:")
			fmt.Println()

			if comp != nil {
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
			} else {
				fmt.Println("  (resources will be determined from OCI artifact)")
			}

			fmt.Println()

			// Handle targets filter
			_ = targets

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

			// Create the engine
			eng := createEngine(mgr)

			// Convert vars to interface{} map
			varsInterface := make(map[string]interface{})
			for k, v := range vars {
				varsInterface[k] = v
			}

			// Determine component path
			componentPath := source
			if isLocalPath && !strings.HasSuffix(source, ".yml") && !strings.HasSuffix(source, ".yaml") {
				componentPath = filepath.Join(source, "architect.yml")
			}

			// Build progress table from component resources
			progress := NewProgressTable(os.Stdout)

			if comp != nil {
				// Build dependency graph for progress display
				var dbDeps []string
				for _, db := range comp.Databases() {
					id := fmt.Sprintf("%s/database/%s", componentName, db.Name())
					progress.AddResource(id, db.Name(), "database", componentName, nil)
					dbDeps = append(dbDeps, id)
				}

				for _, bucket := range comp.Buckets() {
					id := fmt.Sprintf("%s/bucket/%s", componentName, bucket.Name())
					progress.AddResource(id, bucket.Name(), "bucket", componentName, nil)
				}

				for _, fn := range comp.Functions() {
					id := fmt.Sprintf("%s/function/%s", componentName, fn.Name())
					progress.AddResource(id, fn.Name(), "function", componentName, dbDeps)
				}

				for _, depl := range comp.Deployments() {
					id := fmt.Sprintf("%s/deployment/%s", componentName, depl.Name())
					progress.AddResource(id, depl.Name(), "deployment", componentName, dbDeps)
				}

				// Services have no dependencies - they can be created in parallel with deployments
				for _, svc := range comp.Services() {
					id := fmt.Sprintf("%s/service/%s", componentName, svc.Name())
					progress.AddResource(id, svc.Name(), "service", componentName, nil)
				}

				// Routes have no dependencies - they can be created in parallel
				for _, route := range comp.Routes() {
					id := fmt.Sprintf("%s/route/%s", componentName, route.Name())
					progress.AddResource(id, route.Name(), "route", componentName, nil)
				}

				// Print initial progress table
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
				Datacenter:  envState.Datacenter,
				Components:  map[string]string{componentName: componentPath},
				Variables:   map[string]map[string]interface{}{componentName: varsInterface},
				Output:      os.Stdout,
				DryRun:      false,
				AutoApprove: autoApprove,
				Parallelism: defaultParallelism,
				OnProgress:  onProgress,
			})
			if err != nil {
				return fmt.Errorf("deployment failed: %w", err)
			}

			if !result.Success {
				if result.Execution != nil && len(result.Execution.Errors) > 0 {
					return fmt.Errorf("deployment failed with %d errors: %v", len(result.Execution.Errors), result.Execution.Errors[0])
				}
				return fmt.Errorf("deployment failed")
			}

			// Print final summary
			progress.PrintFinalSummary()

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
	cmd.Flags().StringArrayVar(&variables, "var", nil, "Set variable (key=value)")
	cmd.Flags().StringVar(&varFile, "var-file", "", "Load variables from file")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().StringArrayVar(&targets, "target", nil, "Target specific resource (repeatable)")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")
	_ = cmd.MarkFlagRequired("environment")

	return cmd
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
		Use:     "datacenter <name> <config>",
		Aliases: []string{"dc", "dcs", "datacenters"},
		Short:   "Deploy a datacenter",
		Long: `Deploy or update a datacenter.

Arguments:
  name    Name for the deployed datacenter
  config  Datacenter config: OCI image reference or local path

Examples:
  arcctl deploy datacenter my-dc ./datacenter
  arcctl deploy datacenter prod-dc ghcr.io/myorg/dc:v1.0.0`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dcName := args[0]
			configRef := args[1]
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

			// Check if this is an OCI reference or local path
			isLocalPath := !strings.Contains(configRef, ":") || strings.HasPrefix(configRef, "./") || strings.HasPrefix(configRef, "/")

			// Determine the tag and source for this datacenter
			tag := configRef // For OCI refs, the tag IS the reference
			source := configRef
			if isLocalPath {
				absPath, err := filepath.Abs(configRef)
				if err != nil {
					return fmt.Errorf("failed to resolve absolute path: %w", err)
				}
				source = absPath
				tag = fmt.Sprintf("%s:latest", dcName)
			}

			// Display execution plan
			fmt.Printf("Datacenter: %s\n", dcName)
			fmt.Printf("Source:     %s\n", source)
			fmt.Printf("Tag:        %s\n", tag)
			fmt.Println()

			// Load datacenter to show execution plan
			var dc datacenter.Datacenter
			var dcDir string
			var allModules map[string]moduleInfo

			if isLocalPath {
				// Load datacenter from local path
				dcFile := source
				dcDir = source
				info, err := os.Stat(source)
				if err != nil {
					return fmt.Errorf("failed to access config path: %w", err)
				}
				if info.IsDir() {
					dcFile = filepath.Join(source, "datacenter.dc")
					if _, err := os.Stat(dcFile); os.IsNotExist(err) {
						dcFile = filepath.Join(source, "datacenter.hcl")
					}
				} else {
					dcDir = filepath.Dir(source)
				}
				loader := datacenter.NewLoader()
				dc, err = loader.Load(dcFile)
				if err != nil {
					return fmt.Errorf("failed to load datacenter: %w", err)
				}

				// Collect all modules
				allModules = collectAllModules(dc, dcDir)

				fmt.Println("Build Plan:")
				if len(allModules) > 0 {
					fmt.Printf("  %d module(s) to build:\n", len(allModules))
					for modulePath, modInfo := range allModules {
						fmt.Printf("    %-24s (%s)\n", modulePath, modInfo.plugin)
					}
				} else {
					fmt.Println("  No modules to build.")
				}
			} else {
				fmt.Println("Build Plan:")
				fmt.Println("  (modules will be determined from OCI artifact)")
			}

			fmt.Println()

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

			if isLocalPath {
				// Build module Docker images
				if len(allModules) > 0 {
					moduleBuilder, err := createModuleBuilder()
					if err != nil {
						return fmt.Errorf("failed to create module builder: %w", err)
					}
					defer moduleBuilder.Close()

					// Compute module artifact tags
					baseRef := tag
					tagPart := ""
					if idx := strings.LastIndex(tag, ":"); idx != -1 {
						baseRef = tag[:idx]
						tagPart = tag[idx:]
					}

					for modulePath, modInfo := range allModules {
						modName := strings.TrimPrefix(modulePath, "module/")
						modRef := fmt.Sprintf("%s-module-%s%s", baseRef, modName, tagPart)

						fmt.Printf("[build] Building %s...\n", modulePath)
						buildResult, err := moduleBuilder.Build(ctx, modInfo.sourceDir, modInfo.plugin, modRef)
						if err != nil {
							return fmt.Errorf("failed to build module %s: %w", modulePath, err)
						}
						fmt.Printf("[success] Built %s (%s)\n", modRef, buildResult.ModuleType)
					}
				}

				// Snapshot the datacenter source to a stable cache directory.
				// This makes the deployed datacenter immutable — source changes
				// won't affect it until explicitly re-deployed.
				cacheDir, err := registry.CachePathForRef(tag)
				if err != nil {
					return fmt.Errorf("failed to compute cache path: %w", err)
				}

				// Remove old cache if present
				os.RemoveAll(cacheDir)
				if err := os.MkdirAll(cacheDir, 0755); err != nil {
					return fmt.Errorf("failed to create cache directory: %w", err)
				}

				if err := copyDirectory(dcDir, cacheDir); err != nil {
					return fmt.Errorf("failed to snapshot datacenter source: %w", err)
				}

				// Register in unified artifact registry
				reg, err := registry.NewRegistry()
				if err != nil {
					return fmt.Errorf("failed to create local registry: %w", err)
				}

				repo, tagPortion := registry.ParseReference(tag)
				dcEntry := registry.ArtifactEntry{
					Reference:  tag,
					Repository: repo,
					Tag:        tagPortion,
					Type:       registry.TypeDatacenter,
					Source:     registry.SourceBuilt,
					CreatedAt:  time.Now(),
					CachePath:  cacheDir,
				}
				if err := reg.Add(dcEntry); err != nil {
					return fmt.Errorf("failed to register datacenter: %w", err)
				}

				fmt.Printf("[success] Datacenter cached at %s\n", cacheDir)
			} else {
				// For OCI references, verify the artifact exists
				client := oci.NewClient()
				exists, err := client.Exists(ctx, configRef)
				if err != nil {
					return fmt.Errorf("failed to check artifact: %w", err)
				}
				if !exists {
					return fmt.Errorf("artifact %s not found in registry", configRef)
				}
			}

			// Preserve existing environment references on update
			existingDC, _ := mgr.GetDatacenter(ctx, dcName)
			var existingEnvs []string
			if existingDC != nil {
				existingEnvs = existingDC.Environments
			}

			// Save datacenter state
			dcState := &types.DatacenterState{
				Name:         dcName,
				Version:      tag,
				Source:        source,
				Variables:    vars,
				Modules:      make(map[string]*types.ModuleState),
				Environments: existingEnvs,
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			}

			if err := mgr.SaveDatacenter(ctx, dcState); err != nil {
				return fmt.Errorf("failed to save datacenter state: %w", err)
			}

			fmt.Printf("[success] Datacenter %q deployed as %s\n", dcName, tag)
			fmt.Println()
			fmt.Println("The datacenter is now available for use with environments.")
			fmt.Println("Infrastructure modules will be executed when components are deployed.")

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
		// If it's a file path (architect.yml), get the parent directory
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
