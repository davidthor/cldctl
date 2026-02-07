package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/architect-io/arcctl/pkg/engine"
	"github.com/architect-io/arcctl/pkg/engine/executor"
	"github.com/architect-io/arcctl/pkg/schema/environment"
	"github.com/architect-io/arcctl/pkg/state"
	"github.com/architect-io/arcctl/pkg/state/types"
	"github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update resources",
		Long:  `Commands for updating existing resources.`,
	}

	cmd.AddCommand(newUpdateEnvironmentCmd())

	return cmd
}

func newUpdateEnvironmentCmd() *cobra.Command {
	var (
		datacenter    string
		autoApprove   bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:     "environment <name> [config-file]",
		Aliases: []string{"env", "envs", "environments"},
		Short:   "Update an environment",
		Long: `Update environment configuration.

You can update an environment in two ways:

1. Update specific settings with flags:
   arcctl update environment staging --datacenter new-dc

2. Apply a configuration file that defines the full environment state:
   arcctl update environment staging environment.yml

When providing a config file, the environment's components and configuration
will be updated to match the file. Components not in the file will be removed,
and new components will be deployed.

Examples:
  arcctl update environment staging --datacenter new-dc
  arcctl update environment staging environment.yml
  arcctl update environment staging ./envs/staging.yml --auto-approve`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := args[0]
			ctx := context.Background()

			// Check if a config file was provided as second argument
			configFile := ""
			if len(args) > 1 {
				configFile = args[1]
			}

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

			// Get environment state
			env, err := mgr.GetEnvironment(ctx, dc, envName)
			if err != nil {
				return fmt.Errorf("environment %q not found in datacenter %q: %w", envName, dc, err)
			}

			// If a config file is provided, apply it
			if configFile != "" {
				return applyEnvironmentConfig(ctx, mgr, dc, env, configFile, autoApprove)
			}

			// Otherwise, update individual settings
			// Note: datacenter migration is not supported through this command
			// since environments are now nested under datacenters
			fmt.Println("No changes specified. Provide a config file to update environment components.")
			return nil
		},
	}

	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Target datacenter (uses default if not set)")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt (when using config file)")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}

// applyEnvironmentConfig applies an environment configuration file to an existing environment.
func applyEnvironmentConfig(ctx context.Context, mgr state.Manager, dc string, env *types.EnvironmentState, configFile string, autoApprove bool) error {
	// Load and validate the environment file
	loader := environment.NewLoader()
	envConfig, err := loader.Load(configFile)
	if err != nil {
		return fmt.Errorf("failed to load environment config: %w", err)
	}

	// Note: Name is a CLI parameter, not part of the config file

	fmt.Printf("Environment: %s\n", env.Name)
	fmt.Printf("Datacenter:  %s\n", env.Datacenter)
	fmt.Printf("Config file: %s\n", configFile)
	fmt.Println()

	// Determine changes
	newComponents := envConfig.Components()
	existingComponents := env.Components

	// Components to add
	var toAdd []string
	for name := range newComponents {
		if _, exists := existingComponents[name]; !exists {
			toAdd = append(toAdd, name)
		}
	}

	// Components to update
	var toUpdate []string
	for name, newComp := range newComponents {
		if existing, exists := existingComponents[name]; exists {
			// Check if source changed
			if existing.Source != newComp.Source() {
				toUpdate = append(toUpdate, name)
			}
		}
	}

	// Components to remove
	var toRemove []string
	for name := range existingComponents {
		if _, exists := newComponents[name]; !exists {
			toRemove = append(toRemove, name)
		}
	}

	// Display execution plan
	fmt.Println("Execution Plan:")
	fmt.Println()

	if len(toAdd) > 0 {
		fmt.Println("  Components to deploy:")
		for _, name := range toAdd {
			comp := newComponents[name]
			fmt.Printf("    + %s (%s)\n", name, comp.Source())
		}
		fmt.Println()
	}

	if len(toUpdate) > 0 {
		fmt.Println("  Components to update:")
		for _, name := range toUpdate {
			oldComp := existingComponents[name]
			newComp := newComponents[name]
			fmt.Printf("    ~ %s: %s -> %s\n", name, oldComp.Source, newComp.Source())
		}
		fmt.Println()
	}

	if len(toRemove) > 0 {
		fmt.Println("  Components to remove:")
		for _, name := range toRemove {
			fmt.Printf("    - %s\n", name)
		}
		fmt.Println()
	}

	if len(toAdd) == 0 && len(toUpdate) == 0 && len(toRemove) == 0 {
		fmt.Println("  No changes detected.")
		return nil
	}

	fmt.Printf("Plan: %d to deploy, %d to update, %d to remove\n", len(toAdd), len(toUpdate), len(toRemove))
	fmt.Println()

	// Confirm unless --auto-approve is provided
	if !autoApprove {
		fmt.Print("Proceed with update? [Y/n]: ")
		var response string
		_, _ = fmt.Scanln(&response)
		response = strings.ToLower(strings.TrimSpace(response))
		if response != "" && response != "y" && response != "yes" {
			fmt.Println("Update cancelled.")
			return nil
		}
	}

	fmt.Println()
	fmt.Printf("[update] Applying configuration to environment %q...\n", env.Name)

	// Create the engine
	eng := createEngine(mgr)

	// Re-execute environment-scoped modules to reconcile with any datacenter changes
	envResult, err := eng.DeployEnvironment(ctx, engine.DeployEnvironmentOptions{
		Datacenter:  dc,
		Environment: env.Name,
		Output:      os.Stdout,
		Parallelism: defaultParallelism,
	})
	if err != nil {
		fmt.Printf("[warning] Failed to reconcile environment modules: %v\n", err)
	} else if envResult.Success && len(envResult.ModuleOutputs) > 0 {
		fmt.Printf("[success] %d environment module(s) reconciled\n", len(envResult.ModuleOutputs))
	}

	// Create progress callback for component deployments
	onProgress := func(event executor.ProgressEvent) {
		switch event.Status {
		case "running":
			fmt.Printf("  [%s] %s (%s): provisioning...\n", event.NodeType, event.NodeName, event.NodeID)
		case "completed":
			fmt.Printf("  [%s] %s: ready\n", event.NodeType, event.NodeName)
		case "failed":
			errMsg := "unknown error"
			if event.Error != nil {
				errMsg = event.Error.Error()
			}
			fmt.Printf("  [%s] %s: failed (%s)\n", event.NodeType, event.NodeName, errMsg)
		}
	}

	// Track any errors
	var updateErrors []error

	// Remove components first (reverse order for clean teardown)
	for _, name := range toRemove {
		fmt.Printf("[update] Removing component %q...\n", name)

		result, err := eng.DestroyComponent(ctx, engine.DestroyComponentOptions{
			Environment: env.Name,
			Datacenter:  dc,
			Component:   name,
			Output:      os.Stdout,
			DryRun:      false,
			AutoApprove: true, // Already confirmed above
		})
		if err != nil {
			fmt.Printf("  Warning: failed to destroy component %q: %v\n", name, err)
			updateErrors = append(updateErrors, err)
			continue
		}

		if result.Success {
			fmt.Printf("  Removed %d resources\n", result.Execution.Deleted)
		}
	}

	// Add/update components
	for name, comp := range newComponents {
		// Convert variables from map[string]interface{} to map[string]interface{}
		vars := comp.Variables()

		_, isNew := existingComponents[name]
		if isNew {
			fmt.Printf("[update] Updating component %q...\n", name)
		} else {
			fmt.Printf("[update] Deploying component %q...\n", name)
		}

		// Deploy the component
		result, err := eng.Deploy(ctx, engine.DeployOptions{
			Environment: env.Name,
			Datacenter:  dc,
			Components:  map[string]string{name: comp.Source()},
			Variables:   map[string]map[string]interface{}{name: vars},
			Output:      os.Stdout,
			DryRun:      false,
			AutoApprove: true, // Already confirmed above
			Parallelism: defaultParallelism,
			OnProgress:  onProgress,
		})
		if err != nil {
			fmt.Printf("  Warning: failed to deploy component %q: %v\n", name, err)
			updateErrors = append(updateErrors, err)
			continue
		}

		if result.Success && result.Execution != nil {
			fmt.Printf("  Created: %d, Updated: %d\n", result.Execution.Created, result.Execution.Updated)
		}
	}

	// Report final status
	if len(updateErrors) > 0 {
		fmt.Printf("\n[warning] Environment update completed with %d errors\n", len(updateErrors))
		return fmt.Errorf("environment update completed with errors")
	}

	fmt.Printf("\n[success] Environment updated successfully\n")

	return nil
}
