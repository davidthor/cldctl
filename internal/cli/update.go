package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

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
		Aliases: []string{"env"},
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

			// Create state manager
			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Get environment state
			env, err := mgr.GetEnvironment(ctx, envName)
			if err != nil {
				return fmt.Errorf("environment %q not found: %w", envName, err)
			}

			// If a config file is provided, apply it
			if configFile != "" {
				return applyEnvironmentConfig(ctx, mgr, env, configFile, autoApprove)
			}

			// Otherwise, update individual settings
			hasChanges := false

			// Update datacenter if specified
			if datacenter != "" && datacenter != env.Datacenter {
				// Verify new datacenter exists
				_, err := mgr.GetDatacenter(ctx, datacenter)
				if err != nil {
					return fmt.Errorf("datacenter %q not found: %w", datacenter, err)
				}

				fmt.Printf("Updating environment %q datacenter from %q to %q\n", envName, env.Datacenter, datacenter)
				env.Datacenter = datacenter
				hasChanges = true
			}

			if !hasChanges {
				fmt.Println("No changes specified. Use --datacenter or provide a config file.")
				return nil
			}

			env.UpdatedAt = time.Now()

			if err := mgr.SaveEnvironment(ctx, env); err != nil {
				return fmt.Errorf("failed to save environment state: %w", err)
			}

			fmt.Printf("[success] Environment updated successfully\n")

			return nil
		},
	}

	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Change target datacenter")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt (when using config file)")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}

// applyEnvironmentConfig applies an environment configuration file to an existing environment.
func applyEnvironmentConfig(ctx context.Context, mgr state.Manager, env *types.EnvironmentState, configFile string, autoApprove bool) error {
	// Load and validate the environment file
	loader := environment.NewLoader()
	envConfig, err := loader.Load(configFile)
	if err != nil {
		return fmt.Errorf("failed to load environment config: %w", err)
	}

	// Verify the environment name matches
	if envConfig.Name() != "" && envConfig.Name() != env.Name {
		return fmt.Errorf("environment name in config file (%q) does not match target environment (%q)", envConfig.Name(), env.Name)
	}

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

	// Update datacenter if changed
	if envConfig.Datacenter() != "" && envConfig.Datacenter() != env.Datacenter {
		env.Datacenter = envConfig.Datacenter()
	}

	// TODO: Implement actual component deployment/update/removal logic
	// For now, just update the state to reflect the new components

	// Remove components
	for _, name := range toRemove {
		fmt.Printf("[update] Removing component %q...\n", name)
		delete(env.Components, name)
	}

	// Add/update components
	for name, comp := range newComponents {
		if env.Components == nil {
			env.Components = make(map[string]*types.ComponentState)
		}

		// Convert variables from map[string]interface{} to map[string]string
		vars := make(map[string]string)
		for k, v := range comp.Variables() {
			if s, ok := v.(string); ok {
				vars[k] = s
			} else {
				vars[k] = fmt.Sprintf("%v", v)
			}
		}

		if _, exists := env.Components[name]; !exists {
			fmt.Printf("[update] Deploying component %q...\n", name)
			env.Components[name] = &types.ComponentState{
				Name:       name,
				Source:     comp.Source(),
				Status:     types.ResourceStatusReady,
				DeployedAt: time.Now(),
				Variables:  vars,
				Resources:  make(map[string]*types.ResourceState),
			}
		} else {
			fmt.Printf("[update] Updating component %q...\n", name)
			env.Components[name].Source = comp.Source()
			env.Components[name].Variables = vars
		}
	}

	env.UpdatedAt = time.Now()

	if err := mgr.SaveEnvironment(ctx, env); err != nil {
		return fmt.Errorf("failed to save environment state: %w", err)
	}

	fmt.Printf("[success] Environment updated successfully\n")

	return nil
}
