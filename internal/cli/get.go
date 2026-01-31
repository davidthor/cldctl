package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get resource details",
		Long:  `Commands for getting details of components, datacenters, and environments.`,
	}

	cmd.AddCommand(newGetComponentCmd())
	cmd.AddCommand(newGetDatacenterCmd())
	cmd.AddCommand(newGetEnvironmentCmd())

	return cmd
}

func newGetComponentCmd() *cobra.Command {
	var (
		environment   string
		outputFormat  string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:     "component <name>",
		Aliases: []string{"comp"},
		Short:   "Get details of a deployed component",
		Long: `Get detailed information about a deployed component.

Examples:
  arcctl get component my-app -e production
  arcctl get component api -e staging -o json`,
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
				return fmt.Errorf("failed to get component: %w", err)
			}

			// Get environment state for datacenter info
			envState, err := mgr.GetEnvironment(ctx, environment)
			if err != nil {
				return fmt.Errorf("failed to get environment: %w", err)
			}

			// Handle output format
			switch outputFormat {
			case "json":
				data, err := json.MarshalIndent(comp, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(data))
			case "yaml":
				data, err := yaml.Marshal(comp)
				if err != nil {
					return fmt.Errorf("failed to marshal YAML: %w", err)
				}
				fmt.Println(string(data))
			default:
				// Table format
				fmt.Printf("Component:   %s\n", comp.Name)
				fmt.Printf("Environment: %s\n", environment)
				fmt.Printf("Datacenter:  %s\n", envState.Datacenter)
				fmt.Printf("Source:      %s\n", comp.Source)
				fmt.Printf("Status:      %s\n", comp.Status)
				fmt.Printf("Deployed:    %s\n", comp.DeployedAt.Format("2006-01-02 15:04:05"))
				fmt.Println()

				if len(comp.Variables) > 0 {
					fmt.Println("Variables:")
					for key, value := range comp.Variables {
						// Mask sensitive values
						displayValue := value
						if strings.Contains(strings.ToLower(key), "secret") ||
							strings.Contains(strings.ToLower(key), "password") ||
							strings.Contains(strings.ToLower(key), "key") {
							displayValue = "<sensitive>"
						}
						fmt.Printf("  %-16s = %q\n", key, displayValue)
					}
					fmt.Println()
				}

				if len(comp.Resources) > 0 {
					fmt.Println("Resources:")
					fmt.Printf("  %-12s %-16s %-12s %s\n", "TYPE", "NAME", "STATUS", "DETAILS")
					for _, res := range comp.Resources {
						details := ""
						if res.Outputs != nil {
							// Extract some key outputs for display
							if url, ok := res.Outputs["url"].(string); ok {
								details = url
							}
						}
						fmt.Printf("  %-12s %-16s %-12s %s\n",
							res.Type,
							res.Name,
							res.Status,
							details,
						)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format: table, json, yaml")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")
	_ = cmd.MarkFlagRequired("environment")

	return cmd
}

func newGetDatacenterCmd() *cobra.Command {
	var (
		outputFormat  string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:     "datacenter <name>",
		Aliases: []string{"dc"},
		Short:   "Get details of a deployed datacenter",
		Long: `Get detailed information about a datacenter.

Examples:
  arcctl get datacenter my-dc
  arcctl get datacenter prod-dc -o json`,
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

			// Handle output format
			switch outputFormat {
			case "json":
				data, err := json.MarshalIndent(dc, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(data))
			case "yaml":
				data, err := yaml.Marshal(dc)
				if err != nil {
					return fmt.Errorf("failed to marshal YAML: %w", err)
				}
				fmt.Println(string(data))
			default:
				// Table format
				fmt.Printf("Datacenter: %s\n", dc.Name)
				fmt.Printf("Source:     %s\n", dc.Version)
				fmt.Printf("Deployed:   %s\n", dc.CreatedAt.Format("2006-01-02 15:04:05"))
				fmt.Println()

				if len(dc.Variables) > 0 {
					fmt.Println("Variables:")
					for key, value := range dc.Variables {
						fmt.Printf("  %-16s = %q\n", key, value)
					}
					fmt.Println()
				}

				if len(dc.Modules) > 0 {
					fmt.Println("Modules:")
					fmt.Printf("  %-16s %-12s %s\n", "NAME", "STATUS", "RESOURCES")
					for name, mod := range dc.Modules {
						resourceCount := 0
						if mod.Outputs != nil {
							resourceCount = len(mod.Outputs)
						}
						fmt.Printf("  %-16s %-12s %d\n", name, mod.Status, resourceCount)
					}
					fmt.Println()
				}

				if len(dc.Environments) > 0 {
					fmt.Println("Environments:")
					fmt.Printf("  %-16s %-12s %s\n", "NAME", "COMPONENTS", "CREATED")
					for _, envName := range dc.Environments {
						env, err := mgr.GetEnvironment(ctx, envName)
						if err != nil {
							continue
						}
						fmt.Printf("  %-16s %-12d %s\n",
							envName,
							len(env.Components),
							env.CreatedAt.Format("2006-01-02"),
						)
					}
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

func newGetEnvironmentCmd() *cobra.Command {
	var (
		outputFormat  string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:     "environment <name>",
		Aliases: []string{"env"},
		Short:   "Get details of an environment",
		Long: `Get detailed information about an environment.

Examples:
  arcctl get environment staging
  arcctl get environment production -o json`,
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

			// Handle output format
			switch outputFormat {
			case "json":
				data, err := json.MarshalIndent(env, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(data))
			case "yaml":
				data, err := yaml.Marshal(env)
				if err != nil {
					return fmt.Errorf("failed to marshal YAML: %w", err)
				}
				fmt.Println(string(data))
			default:
				// Table format
				fmt.Printf("Environment: %s\n", env.Name)
				fmt.Printf("Datacenter:  %s\n", env.Datacenter)
				fmt.Printf("Created:     %s\n", env.CreatedAt.Format("2006-01-02 15:04:05"))
				fmt.Printf("Status:      %s\n", env.Status)
				fmt.Println()

				if len(env.Components) > 0 {
					fmt.Println("Components:")
					fmt.Printf("  %-16s %-40s %-12s %s\n", "NAME", "SOURCE", "STATUS", "RESOURCES")
					for name, comp := range env.Components {
						fmt.Printf("  %-16s %-40s %-12s %d\n",
							name,
							truncateString(comp.Source, 40),
							comp.Status,
							len(comp.Resources),
						)
					}
					fmt.Println()
				}

				// Collect URLs from components
				var urls []struct {
					component string
					route     string
					url       string
				}
				for compName, comp := range env.Components {
					for resName, res := range comp.Resources {
						if res.Type == "route" || res.Type == "ingress" {
							if url, ok := res.Outputs["url"].(string); ok {
								urls = append(urls, struct {
									component string
									route     string
									url       string
								}{compName, resName, url})
							}
						}
					}
				}
				if len(urls) > 0 {
					fmt.Println("URLs:")
					for _, u := range urls {
						fmt.Printf("  %s/%s: %s\n", u.component, u.route, u.url)
					}
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
