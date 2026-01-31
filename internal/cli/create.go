package cli

import (
	"context"
	"fmt"
	"time"

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
		Aliases: []string{"env"},
		Short:   "Create a new environment",
		Long: `Create a new environment.

Examples:
  arcctl create environment staging -d my-datacenter
  arcctl create environment production -d prod-dc --if-not-exists`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := args[0]
			ctx := context.Background()

			// Create state manager
			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Check if environment already exists
			existingEnv, err := mgr.GetEnvironment(ctx, envName)
			if err == nil && existingEnv != nil {
				if ifNotExists {
					fmt.Printf("Environment %q already exists, skipping creation.\n", envName)
					return nil
				}
				return fmt.Errorf("environment %q already exists", envName)
			}

			// Verify datacenter exists
			dc, err := mgr.GetDatacenter(ctx, datacenter)
			if err != nil {
				return fmt.Errorf("datacenter %q not found: %w", datacenter, err)
			}

			fmt.Printf("Environment: %s\n", envName)
			fmt.Printf("Datacenter:  %s\n", datacenter)
			fmt.Println()

			fmt.Printf("[create] Creating environment %q...\n", envName)

			// Create environment state
			envState := &types.EnvironmentState{
				Name:       envName,
				Datacenter: datacenter,
				Status:     types.EnvironmentStatusReady,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
				Components: make(map[string]*types.ComponentState),
			}

			if err := mgr.SaveEnvironment(ctx, envState); err != nil {
				return fmt.Errorf("failed to save environment state: %w", err)
			}

			// Update datacenter with environment reference
			dc.Environments = append(dc.Environments, envName)
			dc.UpdatedAt = time.Now()
			if err := mgr.SaveDatacenter(ctx, dc); err != nil {
				return fmt.Errorf("failed to update datacenter state: %w", err)
			}

			fmt.Printf("[success] Environment created successfully\n")

			return nil
		},
	}

	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Datacenter to use (required)")
	cmd.Flags().BoolVar(&ifNotExists, "if-not-exists", false, "Don't error if environment already exists")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")
	_ = cmd.MarkFlagRequired("datacenter")

	return cmd
}
