package cli

import (
	"encoding/json"
	"fmt"

	"github.com/davidthor/cldctl/pkg/registry"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newImagesCmd() *cobra.Command {
	var (
		outputFormat string
		typeFilter   string
	)

	cmd := &cobra.Command{
		Use:   "images",
		Short: "List locally cached artifacts",
		Long: `List all component and datacenter artifacts in the local cache.

This is similar to 'docker images' — it shows every artifact that has been
built locally or pulled from a remote registry.

Examples:
  cldctl images                          # List all artifacts
  cldctl images --type component         # Only components
  cldctl images --type datacenter        # Only datacenters
  cldctl images -o json                  # JSON output`,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := registry.NewRegistry()
			if err != nil {
				return fmt.Errorf("failed to open local registry: %w", err)
			}

			var entries []registry.ArtifactEntry

			switch typeFilter {
			case "component", "comp":
				entries, err = reg.ListByType(registry.TypeComponent)
			case "datacenter", "dc":
				entries, err = reg.ListByType(registry.TypeDatacenter)
			case "":
				entries, err = reg.List()
			default:
				return fmt.Errorf("unknown type %q (use 'component' or 'datacenter')", typeFilter)
			}
			if err != nil {
				return fmt.Errorf("failed to list artifacts: %w", err)
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
				// Table output
				if len(entries) == 0 {
					fmt.Println("No locally cached artifacts found.")
					fmt.Println()
				fmt.Println("Build a component:   cldctl build component -t <repo:tag> <path>")
				fmt.Println("Build a datacenter:  cldctl build datacenter -t <repo:tag> <path>")
				fmt.Println("Pull a component:    cldctl pull component <repo:tag>")
				fmt.Println("Pull a datacenter:   cldctl pull datacenter <repo:tag>")
					return nil
				}

				fmt.Printf("%-40s %-15s %-14s %-12s %-10s %-8s %s\n",
					"REPOSITORY", "TAG", "ARTIFACT ID", "TYPE", "SOURCE", "SIZE", "CREATED")
				for _, entry := range entries {
					fmt.Printf("%-40s %-15s %-14s %-12s %-10s %-8s %s\n",
						truncateString(entry.Repository, 40),
						truncateString(entry.Tag, 15),
						shortDigest(entry.Digest),
						entry.Type,
						entry.Source,
						formatSize(entry.Size),
						formatTimeAgo(entry.CreatedAt),
					)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format: table, json, yaml")
	cmd.Flags().StringVar(&typeFilter, "type", "", "Filter by artifact type: component, datacenter")

	return cmd
}
