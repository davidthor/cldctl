package cli

import (
	"context"
	"fmt"

	"github.com/architect-io/arcctl/pkg/state/types"
	"github.com/spf13/cobra"
)

func newObservabilityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "observability",
		Aliases: []string{"obs"},
		Short:   "Observability commands",
		Long:    `Commands for interacting with the observability infrastructure.`,
	}

	cmd.AddCommand(newObservabilityDashboardCmd())

	return cmd
}

func newObservabilityDashboardCmd() *cobra.Command {
	var (
		environment   string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Open the observability dashboard",
		Long: `Open the observability dashboard in your default browser.

The dashboard URL comes from the datacenter's observability hook outputs.
For the local Docker datacenter this opens Grafana; other datacenters
may provide CloudWatch, SigNoz, or any other observability UI.

Examples:
  arcctl observability dashboard -e staging
  arcctl obs dashboard -e production`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Create state manager
			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Load environment state
			envState, err := mgr.GetEnvironment(ctx, environment)
			if err != nil {
				return fmt.Errorf("failed to get environment %q: %w", environment, err)
			}

			// Find the observability resource
			dashboardURL, err := findDashboardURL(envState)
			if err != nil {
				return err
			}

			fmt.Printf("Opening observability dashboard: %s\n", dashboardURL)
			openBrowserURL(dashboardURL)

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
	_ = cmd.MarkFlagRequired("environment")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}

// findDashboardURL searches the environment state for an observability resource
// and returns its dashboard_url output.
func findDashboardURL(envState *types.EnvironmentState) (string, error) {
	for _, comp := range envState.Components {
		for _, res := range comp.Resources {
			if res.Type == "observability" && res.Status == types.ResourceStatusReady {
				dashboardURL, _ := res.Outputs["dashboard_url"].(string)

				if dashboardURL == "" {
					return "", fmt.Errorf(
						"observability resource found but no dashboard_url output.\n"+
							"The datacenter's observability hook must include dashboard_url in its outputs.",
					)
				}

				return dashboardURL, nil
			}
		}
	}

	return "", fmt.Errorf(
		"no observability resource found in environment %q.\n"+
			"Components must declare 'observability: true' and the datacenter\n"+
			"must provide an observability hook to use this command.",
		envState.Name,
	)
}
