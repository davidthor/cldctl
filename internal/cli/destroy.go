package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/architect-io/arcctl/pkg/engine"
	"github.com/spf13/cobra"
)

func newDestroyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Destroy resources",
		Long:  `Commands for destroying components, datacenters, and environments.`,
	}

	cmd.AddCommand(newDestroyComponentCmd())
	cmd.AddCommand(newDestroyDatacenterCmd())
	cmd.AddCommand(newDestroyEnvironmentCmd())

	return cmd
}

func newDestroyComponentCmd() *cobra.Command {
	var (
		environment   string
		datacenter    string
		autoApprove   bool
		force         bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:     "component <name>",
		Aliases: []string{"comp", "comps", "components"},
		Short:   "Destroy a deployed component",
		Long: `Destroy a deployed component and its resources.

If other components in the environment depend on this component, the destroy
will be blocked. Use --force to override this check.

Examples:
  arcctl destroy component my-app -e production
  arcctl destroy component api -e staging --auto-approve
  arcctl destroy component shared-db -e staging --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			componentName := args[0]
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

			// Get component state
			comp, err := mgr.GetComponent(ctx, dc, environment, componentName)
			if err != nil {
				return fmt.Errorf("component %q not found in environment %q: %w", componentName, environment, err)
			}

			// Display what will be destroyed
			fmt.Printf("Component:   %s\n", componentName)
			fmt.Printf("Environment: %s\n", environment)
			fmt.Printf("Datacenter:  %s\n", dc)
			fmt.Println()

			fmt.Println("The following resources will be destroyed:")
			fmt.Println()

			resourceCount := 0
			for _, res := range comp.Resources {
				fmt.Printf("  - %s %q (%s)\n", res.Type, res.Name, res.Status)
				resourceCount++
			}

			fmt.Println()
			fmt.Printf("Total: %d resources to destroy\n", resourceCount)
			fmt.Println()

			// Confirm unless --auto-approve is provided
			if !autoApprove {
				fmt.Print("Are you sure you want to destroy this component? [y/N]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "y" && response != "yes" {
					fmt.Println("Destroy cancelled.")
					return nil
				}
			}

			fmt.Println()
			fmt.Printf("[destroy] Destroying component %q...\n", componentName)

			// Create the engine
			eng := createEngine(mgr)

			// Execute destroy using the engine
			result, err := eng.DestroyComponent(ctx, engine.DestroyComponentOptions{
				Environment: environment,
				Datacenter:  dc,
				Component:   componentName,
				Output:      os.Stdout,
				DryRun:      false,
				AutoApprove: autoApprove,
				Force:       force,
			})
			if err != nil {
				return fmt.Errorf("destroy failed: %w", err)
			}

			if !result.Success {
				if result.Execution != nil && len(result.Execution.Errors) > 0 {
					return fmt.Errorf("destroy failed with %d errors: %v", len(result.Execution.Errors), result.Execution.Errors[0])
				}
				return fmt.Errorf("destroy failed")
			}

			// Display results
			if result.Execution != nil {
				fmt.Printf("\n[success] Component destroyed in %v\n", result.Duration.Round(time.Millisecond))
				fmt.Printf("  Deleted: %d resources\n", result.Execution.Deleted)
			} else {
				fmt.Printf("[success] Component destroyed successfully\n")
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Target datacenter (uses default if not set)")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "Force destroy even if other components depend on this one")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")
	_ = cmd.MarkFlagRequired("environment")

	return cmd
}

func newDestroyDatacenterCmd() *cobra.Command {
	var (
		autoApprove   bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:     "datacenter <name>",
		Aliases: []string{"dc", "dcs", "datacenters"},
		Short:   "Destroy a deployed datacenter",
		Long: `Destroy a datacenter and all its resources.

WARNING: This will destroy all environments in the datacenter. Use with caution.

Examples:
  arcctl destroy datacenter my-dc
  arcctl destroy datacenter prod-dc --auto-approve`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dcName := args[0]
			ctx := context.Background()

			// Create state manager
			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Get datacenter state
			dc, err := mgr.GetDatacenter(ctx, dcName)
			if err != nil {
				return fmt.Errorf("datacenter %q not found: %w", dcName, err)
			}

			// List environments under this datacenter
			envRefs, err := mgr.ListEnvironments(ctx, dcName)
			if err != nil {
				return fmt.Errorf("failed to list environments: %w", err)
			}

			// Display what will be destroyed
			fmt.Printf("Datacenter: %s\n", dcName)
			fmt.Printf("Source:     %s\n", dc.Version)
			fmt.Println()

			if len(envRefs) > 0 {
				fmt.Println("WARNING: The following environments will also be destroyed:")
				for _, env := range envRefs {
					fmt.Printf("  - %s\n", env.Name)
				}
				fmt.Println()
			}

			moduleCount := len(dc.Modules)
			fmt.Printf("This will destroy %d modules.\n", moduleCount)
			fmt.Println()

			// Confirm unless --auto-approve is provided
			if !autoApprove {
				fmt.Print("Are you sure you want to destroy this datacenter? [y/N]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "y" && response != "yes" {
					fmt.Println("Destroy cancelled.")
					return nil
				}
			}

			fmt.Println()

			// Destroy environments first
			for _, envRef := range envRefs {
				fmt.Printf("[destroy] Destroying environment %q...\n", envRef.Name)
				if err := mgr.DeleteEnvironment(ctx, dcName, envRef.Name); err != nil {
					return fmt.Errorf("failed to destroy environment %q: %w", envRef.Name, err)
				}
			}

			fmt.Printf("[destroy] Destroying datacenter %q...\n", dcName)

			// Delete datacenter state (this deletes everything under datacenters/<name>/)
			if err := mgr.DeleteDatacenter(ctx, dcName); err != nil {
				return fmt.Errorf("failed to delete datacenter state: %w", err)
			}

			fmt.Printf("[success] Datacenter removed successfully\n")
			fmt.Println()
			fmt.Println("Note: Infrastructure created by datacenter hooks was destroyed")
			fmt.Println("when the associated environments and components were removed.")

			return nil
		},
	}

	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}

func newDestroyEnvironmentCmd() *cobra.Command {
	var (
		datacenter    string
		autoApprove   bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:     "environment <name>",
		Aliases: []string{"env", "envs", "environments"},
		Short:   "Destroy an environment",
		Long: `Destroy an environment and all its resources.

WARNING: This will destroy all components and resources in the environment. Use with caution.

Examples:
  arcctl destroy environment staging
  arcctl destroy environment production --auto-approve`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := args[0]
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

			// Get environment state
			env, err := mgr.GetEnvironment(ctx, dc, envName)
			if err != nil {
				return fmt.Errorf("environment %q not found in datacenter %q: %w", envName, dc, err)
			}

			// Display what will be destroyed
			fmt.Printf("Environment: %s\n", envName)
			fmt.Printf("Datacenter:  %s\n", env.Datacenter)
			fmt.Println()

			componentCount := len(env.Components)
			resourceCount := 0
			for _, comp := range env.Components {
				resourceCount += len(comp.Resources)
			}

			fmt.Println("This will destroy:")
			fmt.Printf("  - %d components\n", componentCount)
			fmt.Printf("  - %d resources\n", resourceCount)
			fmt.Println()

			// Confirm unless --auto-approve is provided
			if !autoApprove {
				fmt.Print("Are you sure you want to destroy this environment? [y/N]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "y" && response != "yes" {
					fmt.Println("Destroy cancelled.")
					return nil
				}
			}

			fmt.Println()
			fmt.Printf("[destroy] Destroying environment resources...\n")

			// Use the engine to properly destroy all resources (components + env modules)
			eng := createEngine(mgr)
			if err := eng.DestroyEnvironment(ctx, dc, envName, os.Stdout, nil); err != nil {
				fmt.Printf("[warning] Engine-based destroy encountered errors: %v\n", err)
			}

			// Also do Docker cleanup as a safety net for any orphaned containers
			fmt.Printf("[destroy] Stopping any remaining containers...\n")
			if err := CleanupByEnvName(ctx, envName); err != nil {
				fmt.Printf("Warning: failed to cleanup containers: %v\n", err)
			}

			fmt.Printf("[destroy] Removing environment state...\n")

			// Delete environment state
			if err := mgr.DeleteEnvironment(ctx, dc, envName); err != nil {
				return fmt.Errorf("failed to delete environment state: %w", err)
			}

			fmt.Printf("[success] Environment destroyed successfully\n")

			return nil
		},
	}

	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Target datacenter (uses default if not set)")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}
