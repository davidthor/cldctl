package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/architect-io/arcctl/pkg/registry"
	"github.com/architect-io/arcctl/pkg/state/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List resources",
		Long:    `Commands for listing components, datacenters, and environments.`,
	}

	cmd.AddCommand(newListComponentCmd())
	cmd.AddCommand(newListDatacenterCmd())
	cmd.AddCommand(newListEnvironmentCmd())

	return cmd
}

func newListComponentCmd() *cobra.Command {
	var (
		environment   string
		outputFormat  string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:     "component",
		Aliases: []string{"comp", "components"},
		Short:   "List components",
		Long: `List components available locally or deployed to an environment.

Without the --environment flag, lists all locally built or pulled components
(similar to 'docker images').

With the --environment flag, lists all components deployed to that environment.

Examples:
  arcctl list component                    # List local components
  arcctl list component -e production      # List deployed components`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// If no environment specified, list local components
			if environment == "" {
				return listLocalComponents(outputFormat)
			}

			// Otherwise, list deployed components
			// Create state manager
			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Get environment state
			envState, err := mgr.GetEnvironment(ctx, environment)
			if err != nil {
				return fmt.Errorf("failed to get environment: %w", err)
			}

			// Handle output format
			switch outputFormat {
			case "json":
				data, err := json.MarshalIndent(envState.Components, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(data))
			case "yaml":
				data, err := yaml.Marshal(envState.Components)
				if err != nil {
					return fmt.Errorf("failed to marshal YAML: %w", err)
				}
				fmt.Println(string(data))
			default:
				// Table format
				fmt.Printf("Environment: %s\n", environment)
				fmt.Printf("Datacenter:  %s\n\n", envState.Datacenter)

				if len(envState.Components) == 0 {
					fmt.Println("No components deployed.")
					return nil
				}

				fmt.Printf("%-16s %-40s %-10s %-10s %s\n", "NAME", "SOURCE", "VERSION", "STATUS", "RESOURCES")
				for name, comp := range envState.Components {
					resourceCount := len(comp.Resources)
					fmt.Printf("%-16s %-40s %-10s %-10s %d\n",
						name,
						truncateString(comp.Source, 40),
						comp.Version,
						comp.Status,
						resourceCount,
					)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (lists deployed components)")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format: table, json, yaml")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}

// listLocalComponents lists all components in the local registry.
func listLocalComponents(outputFormat string) error {
	reg, err := registry.NewRegistry()
	if err != nil {
		return fmt.Errorf("failed to create local registry: %w", err)
	}

	entries, err := reg.List()
	if err != nil {
		return fmt.Errorf("failed to list components: %w", err)
	}

	switch outputFormat {
	case "json":
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(data))
	case "yaml":
		data, err := yaml.Marshal(entries)
		if err != nil {
			return fmt.Errorf("failed to marshal YAML: %w", err)
		}
		fmt.Println(string(data))
	default:
		// Table format (similar to docker images)
		if len(entries) == 0 {
			fmt.Println("No local components found.")
			fmt.Println()
			fmt.Println("Build a component:  arcctl build component -t <repo:tag> <path>")
			fmt.Println("Pull a component:   arcctl pull component <repo:tag>")
			return nil
		}

		fmt.Printf("%-40s %-15s %-12s %-8s %s\n", "REPOSITORY", "TAG", "SOURCE", "SIZE", "CREATED")
		for _, entry := range entries {
			fmt.Printf("%-40s %-15s %-12s %-8s %s\n",
				truncateString(entry.Repository, 40),
				truncateString(entry.Tag, 15),
				entry.Source,
				formatSize(entry.Size),
				formatTimeAgo(entry.CreatedAt),
			)
		}
	}

	return nil
}

func newListDatacenterCmd() *cobra.Command {
	var (
		outputFormat  string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:     "datacenter",
		Aliases: []string{"dc", "datacenters"},
		Short:   "List deployed datacenters",
		Long: `List all deployed datacenters.

Examples:
  arcctl list datacenter
  arcctl list datacenter -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Create state manager
			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// List datacenters
			dcNames, err := mgr.ListDatacenters(ctx)
			if err != nil {
				return fmt.Errorf("failed to list datacenters: %w", err)
			}

			// Load full datacenter states
			var datacenters []*types.DatacenterState
			for _, name := range dcNames {
				dc, err := mgr.GetDatacenter(ctx, name)
				if err != nil {
					continue // Skip datacenters that can't be read
				}
				datacenters = append(datacenters, dc)
			}

			// Handle output format
			switch outputFormat {
			case "json":
				data, err := json.MarshalIndent(datacenters, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(data))
			case "yaml":
				data, err := yaml.Marshal(datacenters)
				if err != nil {
					return fmt.Errorf("failed to marshal YAML: %w", err)
				}
				fmt.Println(string(data))
			default:
				// Table format
				if len(datacenters) == 0 {
					fmt.Println("No datacenters deployed.")
					return nil
				}

				fmt.Printf("%-18s %-45s %s\n", "NAME", "SOURCE", "ENVIRONMENTS")
				for _, dc := range datacenters {
					envs := strings.Join(dc.Environments, ", ")
					if len(envs) > 30 {
						envs = envs[:27] + "..."
					}
					fmt.Printf("%-18s %-45s %s\n",
						dc.Name,
						truncateString(dc.Version, 45),
						envs,
					)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format: table, json, yaml")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}

func newListEnvironmentCmd() *cobra.Command {
	var (
		datacenter    string
		outputFormat  string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:     "environment",
		Aliases: []string{"env", "environments"},
		Short:   "List environments",
		Long: `List all environments.

Examples:
  arcctl list environment
  arcctl list environment -d my-datacenter
  arcctl list environment -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Create state manager
			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// List environments
			envRefs, err := mgr.ListEnvironments(ctx)
			if err != nil {
				return fmt.Errorf("failed to list environments: %w", err)
			}

			// Filter by datacenter if specified
			if datacenter != "" {
				filtered := make([]types.EnvironmentRef, 0)
				for _, ref := range envRefs {
					if ref.Datacenter == datacenter {
						filtered = append(filtered, ref)
					}
				}
				envRefs = filtered
			}

			// Handle output format
			switch outputFormat {
			case "json":
				data, err := json.MarshalIndent(envRefs, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(data))
			case "yaml":
				data, err := yaml.Marshal(envRefs)
				if err != nil {
					return fmt.Errorf("failed to marshal YAML: %w", err)
				}
				fmt.Println(string(data))
			default:
				// Table format
				if len(envRefs) == 0 {
					fmt.Println("No environments found.")
					return nil
				}

				fmt.Printf("%-16s %-20s %-12s %s\n", "NAME", "DATACENTER", "COMPONENTS", "CREATED")
				for _, ref := range envRefs {
					// Get full environment state for component count
					env, err := mgr.GetEnvironment(ctx, ref.Name)
					componentCount := 0
					if err == nil {
						componentCount = len(env.Components)
					}
					fmt.Printf("%-16s %-20s %-12d %s\n",
						ref.Name,
						ref.Datacenter,
						componentCount,
						ref.CreatedAt.Format("2006-01-02"),
					)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Filter by datacenter")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format: table, json, yaml")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}

// Helper functions for list commands

// truncateString truncates a string to a maximum length.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// formatSize formats a size in bytes to a human-readable string.
func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1fGB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1fMB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1fKB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

// formatTimeAgo formats a time as a human-readable relative time.
func formatTimeAgo(t time.Time) string {
	duration := time.Since(t)

	switch {
	case duration < time.Minute:
		return "just now"
	case duration < time.Hour:
		mins := int(duration.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case duration < 24*time.Hour:
		hours := int(duration.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case duration < 7*24*time.Hour:
		days := int(duration.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	case duration < 30*24*time.Hour:
		weeks := int(duration.Hours() / 24 / 7)
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	default:
		months := int(duration.Hours() / 24 / 30)
		if months == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	}
}
