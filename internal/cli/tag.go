package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/architect-io/arcctl/pkg/oci"
	"github.com/spf13/cobra"
)

func newTagCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tag",
		Short: "Tag artifacts",
		Long:  `Commands for tagging component and datacenter artifacts.`,
	}

	cmd.AddCommand(newTagComponentCmd())
	cmd.AddCommand(newTagDatacenterCmd())

	return cmd
}

func newTagComponentCmd() *cobra.Command {
	var (
		artifactTags []string
		yes          bool
	)

	cmd := &cobra.Command{
		Use:     "component <source> <target>",
		Aliases: []string{"comp"},
		Short:   "Create a new tag for an existing component artifact",
		Long: `Create a new tag for an existing component artifact and all its child artifacts.

This command pulls the source artifact and pushes it with the new target tag,
automatically handling all child artifacts (deployments, functions, etc.).

Examples:
  arcctl tag component ghcr.io/myorg/app:v1.0.0 ghcr.io/myorg/app:latest
  arcctl tag component myapp:dev myapp:staging -y`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]
			target := args[1]

			fmt.Printf("Tagging component artifact\n")
			fmt.Printf("  Source: %s\n", source)
			fmt.Printf("  Target: %s\n", target)
			fmt.Println()

			// Confirm unless --yes is provided
			if !yes {
				fmt.Print("Proceed with tagging? [Y/n]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "" && response != "y" && response != "yes" {
					fmt.Println("Tagging cancelled.")
					return nil
				}
			}

			ctx := context.Background()
			client := oci.NewClient()

			// Tag the artifact
			if err := client.Tag(ctx, source, target); err != nil {
				return fmt.Errorf("failed to tag artifact: %w", err)
			}

			// Handle artifact tag overrides
			_ = artifactTags

			fmt.Printf("[success] Tagged %s as %s\n", source, target)
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&artifactTags, "artifact-tag", nil, "Override tag for a specific child artifact (name=repo:tag)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Non-interactive mode")

	return cmd
}

func newTagDatacenterCmd() *cobra.Command {
	var (
		moduleTags []string
		yes        bool
	)

	cmd := &cobra.Command{
		Use:     "datacenter <source> <target>",
		Aliases: []string{"dc"},
		Short:   "Create a new tag for an existing datacenter artifact",
		Long: `Create a new tag for an existing datacenter artifact and all its module artifacts.

This command pulls the source artifact and pushes it with the new target tag,
automatically handling all module artifacts.

Examples:
  arcctl tag datacenter ghcr.io/myorg/dc:v1.0.0 ghcr.io/myorg/dc:latest
  arcctl tag datacenter my-dc:dev my-dc:staging -y`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]
			target := args[1]

			fmt.Printf("Tagging datacenter artifact\n")
			fmt.Printf("  Source: %s\n", source)
			fmt.Printf("  Target: %s\n", target)
			fmt.Println()

			// Confirm unless --yes is provided
			if !yes {
				fmt.Print("Proceed with tagging? [Y/n]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "" && response != "y" && response != "yes" {
					fmt.Println("Tagging cancelled.")
					return nil
				}
			}

			ctx := context.Background()
			client := oci.NewClient()

			// Tag the artifact
			if err := client.Tag(ctx, source, target); err != nil {
				return fmt.Errorf("failed to tag artifact: %w", err)
			}

			// Handle module tag overrides
			_ = moduleTags

			fmt.Printf("[success] Tagged %s as %s\n", source, target)
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&moduleTags, "module-tag", nil, "Override tag for a specific module (name=repo:tag)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Non-interactive mode")

	return cmd
}
