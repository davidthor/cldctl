package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/architect-io/arcctl/pkg/engine"
	"github.com/architect-io/arcctl/pkg/state/types"
	"github.com/spf13/cobra"
)

func newCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create resources",
		Long:  `Commands for creating new resources.`,
	}

	cmd.AddCommand(newCreateEnvironmentCmd())

	return cmd
}

func newCreateEnvironmentCmd() *cobra.Command {
	var (
		datacenter    string
		ifNotExists   bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:     "environment <name>",
		Aliases: []string{"env", "envs", "environments"},
		Short:   "Create a new environment",
		Long: `Create a new environment.

Examples:
  arcctl create environment staging -d my-datacenter
  arcctl create environment production -d prod-dc --if-not-exists`,
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

			// Check if environment already exists
			existingEnv, err := mgr.GetEnvironment(ctx, dc, envName)
			if err == nil && existingEnv != nil {
				if ifNotExists {
					fmt.Printf("Environment %q already exists, skipping creation.\n", envName)
					return nil
				}
				return fmt.Errorf("environment %q already exists in datacenter %q", envName, dc)
			}

			// Verify datacenter exists
			_, err = mgr.GetDatacenter(ctx, dc)
			if err != nil {
				return fmt.Errorf("datacenter %q not found: %w", dc, err)
			}

			fmt.Printf("Environment: %s\n", envName)
			fmt.Printf("Datacenter:  %s\n", dc)
			fmt.Println()

			fmt.Printf("[create] Creating environment %q...\n", envName)

			// Create environment state
			envState := &types.EnvironmentState{
				Name:       envName,
				Datacenter: dc,
				Status:     types.EnvironmentStatusReady,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
				Components: make(map[string]*types.ComponentState),
			}

			if err := mgr.SaveEnvironment(ctx, dc, envState); err != nil {
				return fmt.Errorf("failed to save environment state: %w", err)
			}

			// Execute environment-scoped modules (namespaces, security groups, etc.)
			eng := createEngine(mgr)
			envResult, err := eng.DeployEnvironment(ctx, engine.DeployEnvironmentOptions{
				Datacenter:  dc,
				Environment: envName,
				Output:      os.Stdout,
				Parallelism: defaultParallelism,
			})
			if err != nil {
				fmt.Printf("[warning] Failed to provision environment modules: %v\n", err)
				fmt.Printf("Environment was created but some infrastructure may not be ready.\n")
			} else if envResult.Success && len(envResult.ModuleOutputs) > 0 {
				fmt.Printf("[success] %d environment module(s) provisioned\n", len(envResult.ModuleOutputs))
			}

			fmt.Printf("[success] Environment created successfully\n")

			return nil
		},
	}

	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Datacenter to use (uses default if not set)")
	cmd.Flags().BoolVar(&ifNotExists, "if-not-exists", false, "Don't error if environment already exists")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}
