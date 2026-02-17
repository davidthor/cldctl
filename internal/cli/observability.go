package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/davidthor/cldctl/pkg/state/types"
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
		datacenter    string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Open the observability dashboard",
		Long: `Open the observability dashboard in your default browser.

The dashboard URL comes from the datacenter's observability hook outputs.
For the local Docker datacenter this opens Grafana's Explore page with a
Loki query pre-filtered to the environment. Other datacenters may provide
CloudWatch, SigNoz, or any other observability UI.

Examples:
  cldctl observability dashboard -e staging
  cldctl obs dashboard -e production`,
		RunE: func(cmd *cobra.Command, args []string) error {
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

			// Load environment state
			envState, err := mgr.GetEnvironment(ctx, dc, environment)
			if err != nil {
				return fmt.Errorf("failed to get environment %q: %w", environment, err)
			}

			// Find the observability resource and build the best possible URL
			obsConfig, err := findObservabilityConfig(envState)
			if err != nil {
				return err
			}

			dashboardURL := buildDashboardURL(obsConfig, environment)

			fmt.Printf("Opening observability dashboard: %s\n", dashboardURL)
			openBrowserURL(dashboardURL)

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
	_ = cmd.MarkFlagRequired("environment")
	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Target datacenter (uses default if not set)")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}

// observabilityConfig holds the outputs from the observability resource
// needed to construct a deep-linked dashboard URL.
type observabilityConfig struct {
	DashboardURL  string
	QueryType     string
	QueryEndpoint string
}

// findObservabilityConfig searches the environment state for an observability
// resource and returns its relevant outputs.
func findObservabilityConfig(envState *types.EnvironmentState) (*observabilityConfig, error) {
	for _, comp := range envState.Components {
		for _, res := range comp.Resources {
			if res.Type == "observability" && res.Status == types.ResourceStatusReady {
				dashboardURL, _ := res.Outputs["dashboard_url"].(string)

				if dashboardURL == "" {
					return nil, fmt.Errorf(
						"observability resource found but no dashboard_url output.\n" +
							"The datacenter's observability hook must include dashboard_url in its outputs.",
					)
				}

				queryType, _ := res.Outputs["query_type"].(string)
				queryEndpoint, _ := res.Outputs["query_endpoint"].(string)

				return &observabilityConfig{
					DashboardURL:  dashboardURL,
					QueryType:     queryType,
					QueryEndpoint: queryEndpoint,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf(
		"no observability resource found in environment %q.\n"+
			"Components must declare 'observability: true' and the datacenter\n"+
			"must provide an observability hook to use this command.",
		envState.Name,
	)
}

// buildDashboardURL constructs a deep-linked dashboard URL filtered to the
// given environment. When the query backend is Loki (Grafana), it builds a
// Grafana Explore URL with a pre-populated LogQL query. For unknown backends,
// it falls back to the base dashboard URL.
func buildDashboardURL(cfg *observabilityConfig, environment string) string {
	if cfg.QueryType == "loki" {
		return buildGrafanaExploreURL(cfg.DashboardURL, environment)
	}
	return cfg.DashboardURL
}

// buildGrafanaExploreURL constructs a Grafana Explore page URL that opens Loki
// with a LogQL query filtered to the specified environment. This uses Grafana's
// schemaVersion=1 URL format compatible with Grafana 9+.
func buildGrafanaExploreURL(grafanaBaseURL string, environment string) string {
	baseURL := strings.TrimRight(grafanaBaseURL, "/")

	// Build the LogQL query matching the same label format used by cldctl logs
	logQL := fmt.Sprintf(`{deployment_environment=%q}`, environment)

	// Construct the Grafana Explore panes JSON structure
	panes := map[string]interface{}{
		"left": map[string]interface{}{
			"datasource": "loki",
			"queries": []map[string]interface{}{
				{
					"refId": "A",
					"expr":  logQL,
				},
			},
			"range": map[string]string{
				"from": "now-1h",
				"to":   "now",
			},
		},
	}

	panesJSON, err := json.Marshal(panes)
	if err != nil {
		// Fall back to base URL if JSON encoding fails
		return baseURL
	}

	params := url.Values{}
	params.Set("schemaVersion", "1")
	params.Set("panes", string(panesJSON))
	params.Set("orgId", "1")

	return fmt.Sprintf("%s/explore?%s", baseURL, params.Encode())
}
