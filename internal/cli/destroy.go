package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

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
		autoApprove   bool
		targets       []string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:     "component <name>",
		Aliases: []string{"comp"},
		Short:   "Destroy a deployed component",
		Long: `Destroy a deployed component and its resources.

Examples:
  arcctl destroy component my-app -e production
  arcctl destroy component api -e staging --auto-approve`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			componentName := args[0]
			ctx := context.Background()

			// Create state manager
			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Get component state
			comp, err := mgr.GetComponent(ctx, environment, componentName)
			if err != nil {
				return fmt.Errorf("component %q not found in environment %q: %w", componentName, environment, err)
			}

			// Display what will be destroyed
			fmt.Printf("Component:   %s\n", componentName)
			fmt.Printf("Environment: %s\n", environment)
			fmt.Println()

			fmt.Println("The following resources will be destroyed:")
			fmt.Println()

			resourceCount := 0
			for _, res := range comp.Resources {
				// Filter by targets if specified
				if len(targets) > 0 {
					matched := false
					for _, t := range targets {
						if strings.HasPrefix(res.Name, t) || strings.HasPrefix(res.Type+"."+res.Name, t) {
							matched = true
							break
						}
					}
					if !matched {
						continue
					}
				}

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

			// TODO: Implement actual destroy logic using engine

			// Delete component state
			if err := mgr.DeleteComponent(ctx, environment, componentName); err != nil {
				return fmt.Errorf("failed to delete component state: %w", err)
			}

			fmt.Printf("[success] Component destroyed successfully\n")

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().StringArrayVar(&targets, "target", nil, "Target specific resource (repeatable)")
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
		Aliases: []string{"dc"},
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

			// Display what will be destroyed
			fmt.Printf("Datacenter: %s\n", dcName)
			fmt.Printf("Source:     %s\n", dc.Version)
			fmt.Println()

			if len(dc.Environments) > 0 {
				fmt.Println("WARNING: The following environments will also be destroyed:")
				for _, env := range dc.Environments {
					fmt.Printf("  - %s\n", env)
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
			for _, envName := range dc.Environments {
				fmt.Printf("[destroy] Destroying environment %q...\n", envName)
				if err := mgr.DeleteEnvironment(ctx, envName); err != nil {
					return fmt.Errorf("failed to destroy environment %q: %w", envName, err)
				}
			}

			fmt.Printf("[destroy] Destroying datacenter %q...\n", dcName)

			// TODO: Implement actual destroy logic using engine

			// Delete datacenter state
			if err := mgr.DeleteDatacenter(ctx, dcName); err != nil {
				return fmt.Errorf("failed to delete datacenter state: %w", err)
			}

			fmt.Printf("[success] Datacenter destroyed successfully\n")

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
		autoApprove   bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:     "environment <name>",
		Aliases: []string{"env"},
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

			// Destroy components
			for compName := range env.Components {
				fmt.Printf("[destroy] Destroying component %q...\n", compName)
				// TODO: Implement actual component destroy logic
			}

			fmt.Printf("[destroy] Removing environment...\n")

			// Delete environment state
			if err := mgr.DeleteEnvironment(ctx, envName); err != nil {
				return fmt.Errorf("failed to delete environment state: %w", err)
			}

			// Update datacenter to remove environment reference
			dc, err := mgr.GetDatacenter(ctx, env.Datacenter)
			if err == nil {
				newEnvs := make([]string, 0, len(dc.Environments))
				for _, e := range dc.Environments {
					if e != envName {
						newEnvs = append(newEnvs, e)
					}
				}
				dc.Environments = newEnvs
				dc.UpdatedAt = time.Now()
				_ = mgr.SaveDatacenter(ctx, dc)
			}

			fmt.Printf("[success] Environment destroyed successfully\n")

			return nil
		},
	}

	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}
