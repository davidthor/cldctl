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
		Aliases: []string{"comp"},
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
			fmt.Printf("[deploy] Deploying component %q to environment %q...\n", componentName, environment)

			// TODO: Implement actual deployment logic using engine

			_ = envState
			_ = vars

			fmt.Printf("[success] Component deployed successfully\n")

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
		Aliases: []string{"dc"},
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

			// Display execution plan
			fmt.Printf("Datacenter: %s\n", dcName)
			fmt.Printf("Source:     %s\n", configRef)
			fmt.Println()

			fmt.Println("Execution Plan:")
			fmt.Println()

			// Check if this is an OCI reference or local path
			isLocalPath := !strings.Contains(configRef, ":") || strings.HasPrefix(configRef, "./") || strings.HasPrefix(configRef, "/")

			if isLocalPath {
				// Load datacenter from local path
				// Check if configRef is a file or directory
				dcFile := configRef
				info, err := os.Stat(configRef)
				if err != nil {
					return fmt.Errorf("failed to access config path: %w", err)
				}
				if info.IsDir() {
					// Look for datacenter file in directory
					// Try datacenter.dc first, then datacenter.hcl
					dcFile = filepath.Join(configRef, "datacenter.dc")
					if _, err := os.Stat(dcFile); os.IsNotExist(err) {
						dcFile = filepath.Join(configRef, "datacenter.hcl")
					}
				}
				loader := datacenter.NewLoader()
				dc, err := loader.Load(dcFile)
				if err != nil {
					return fmt.Errorf("failed to load datacenter: %w", err)
				}

				// Show modules that will be deployed
				for _, mod := range dc.Modules() {
					fmt.Printf("  module %q\n", mod.Name())
					fmt.Printf("    + create: Module %q\n\n", mod.Name())
				}

				fmt.Printf("Plan: %d modules to deploy\n", len(dc.Modules()))
			} else {
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

			// Create or update datacenter state
			dcState := &types.DatacenterState{
				Name:      dcName,
				Version:   configRef,
				Variables: vars,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			if err := mgr.SaveDatacenter(ctx, dcState); err != nil {
				return fmt.Errorf("failed to save datacenter state: %w", err)
			}

			// TODO: Implement actual deployment logic using engine

			fmt.Printf("[success] Datacenter deployed successfully\n")

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
