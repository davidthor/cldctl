package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/davidthor/cldctl/pkg/logs"
	"github.com/davidthor/cldctl/pkg/state/types"
	"github.com/spf13/cobra"

	// Register log query adapters
	_ "github.com/davidthor/cldctl/pkg/logs/loki"
)

func newLogsCmd() *cobra.Command {
	var (
		environment    string
		datacenter     string
		follow         bool
		tail           int
		since          string
		showTimestamps bool
		noColor        bool
		backendType    string
		backendConfig  []string
	)

	cmd := &cobra.Command{
		Use:   "logs [component[/type[/name]]]",
		Short: "View logs from an environment",
		Long: `View and stream logs from workloads in a deployed environment.

Logs are retrieved from the OTel backend provisioned by the datacenter's
observability hook. Components must have observability enabled and the
datacenter must provide an observability hook with query outputs.

Scope:
  cldctl logs -e staging                          # All logs in the environment
  cldctl logs -e staging my-app                   # Logs from one component
  cldctl logs -e staging my-app/deployment        # All deployments in a component
  cldctl logs -e staging my-app/deployment/api    # A specific deployment

Streaming:
  cldctl logs -e staging -f                       # Follow new logs in real-time
  cldctl logs -e staging my-app -f                # Follow one component

Filtering:
  cldctl logs -e staging --since 5m               # Logs from the last 5 minutes
  cldctl logs -e staging -n 50                    # Last 50 lines`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()

			// Resolve datacenter
			dc, err := resolveDatacenter(datacenter)
			if err != nil {
				return err
			}

			// Parse the optional component[/type[/name]] argument
			var component, resourceType, workload string
			if len(args) > 0 {
				parts := strings.SplitN(args[0], "/", 3)
				component = parts[0]
				if len(parts) >= 2 {
					resourceType = parts[1]
				}
				if len(parts) == 3 {
					workload = parts[2]
				}
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

			// Find the observability resource
			queryType, queryEndpoint, err := findObservabilityQueryConfig(envState)
			if err != nil {
				return err
			}

			// Create the log querier
			querier, err := logs.NewQuerier(queryType, queryEndpoint)
			if err != nil {
				return fmt.Errorf("failed to create log querier: %w", err)
			}

			// Build query options
			opts := logs.QueryOptions{
				Environment:  environment,
				Component:    component,
				ResourceType: resourceType,
				Limit:        tail,
			}

			// If name is specified, build the full service name (component-name)
			if workload != "" && component != "" {
				opts.Workload = component + "-" + workload
			}

			// Parse --since flag
			if since != "" {
				sinceTime, err := parseSince(since)
				if err != nil {
					return fmt.Errorf("invalid --since value %q: %w", since, err)
				}
				opts.Since = sinceTime
			}

			muxOpts := logs.MultiplexOptions{
				ShowTimestamps: showTimestamps,
				NoColor:        noColor,
			}

			if follow {
				// Streaming mode
				stream, err := querier.Tail(ctx, opts)
				if err != nil {
					return fmt.Errorf("failed to start log stream: %w", err)
				}
				defer stream.Close()

				fmt.Fprintf(os.Stderr, "Streaming logs from environment %q (Ctrl+C to stop)...\n", environment)

				if err := logs.FormatStream(os.Stdout, stream, muxOpts); err != nil {
					return fmt.Errorf("log stream error: %w", err)
				}
			} else {
				// Historical query mode
				result, err := querier.Query(ctx, opts)
				if err != nil {
					return fmt.Errorf("failed to query logs: %w", err)
				}

				if len(result.Entries) == 0 {
					fmt.Fprintln(os.Stderr, "No logs found for the specified scope and time range.")
					return nil
				}

				logs.FormatQueryResult(os.Stdout, result, muxOpts)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
	_ = cmd.MarkFlagRequired("environment")
	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Target datacenter (uses default if not set)")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Stream logs in real-time")
	cmd.Flags().IntVarP(&tail, "tail", "n", 100, "Number of recent lines to show")
	cmd.Flags().StringVar(&since, "since", "", "Show logs since duration or timestamp (e.g., 5m, 1h, 2025-01-01T00:00:00Z)")
	cmd.Flags().BoolVar(&showTimestamps, "timestamps", false, "Show timestamps")
	cmd.Flags().BoolVar(&noColor, "no-color", false, "Disable colored prefixes")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}

// findObservabilityQueryConfig searches the environment state for an observability
// resource and returns its query_type and query_endpoint outputs.
func findObservabilityQueryConfig(envState *types.EnvironmentState) (string, string, error) {
	for _, comp := range envState.Components {
		for _, res := range comp.Resources {
			if res.Type == "observability" && res.Status == types.ResourceStatusReady {
				queryType, _ := res.Outputs["query_type"].(string)
				queryEndpoint, _ := res.Outputs["query_endpoint"].(string)

				if queryType == "" || queryEndpoint == "" {
					return "", "", fmt.Errorf(
						"observability resource found but missing query outputs (query_type=%q, query_endpoint=%q).\n"+
							"The datacenter's observability hook must include query_type and query_endpoint in its outputs.\n"+
							"See: cldctl observability dashboard -e %s",
						queryType, queryEndpoint, envState.Name,
					)
				}

				return queryType, queryEndpoint, nil
			}
		}
	}

	return "", "", fmt.Errorf(
		"no observability resource found in environment %q.\n"+
			"To use 'cldctl logs', components must declare 'observability: true' (or inject: true)\n"+
			"and the datacenter must provide an observability hook with query outputs.\n\n"+
			"Example component:\n"+
			"  observability:\n"+
			"    inject: true\n"+
			"    attributes:\n"+
			"      team: backend",
		envState.Name,
	)
}

// parseSince parses a duration string (e.g., "5m", "1h") or an RFC3339 timestamp.
func parseSince(s string) (time.Time, error) {
	// Try as a duration first
	d, err := time.ParseDuration(s)
	if err == nil {
		return time.Now().Add(-d), nil
	}

	// Try as RFC3339 timestamp
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("must be a duration (e.g., 5m, 1h) or RFC3339 timestamp")
}
