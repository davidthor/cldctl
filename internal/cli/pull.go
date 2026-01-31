package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/architect-io/arcctl/pkg/oci"
	"github.com/architect-io/arcctl/pkg/registry"
	"github.com/spf13/cobra"
)

func newPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull artifacts from registry",
		Long:  `Commands for pulling artifacts from OCI registries.`,
	}

	cmd.AddCommand(newPullComponentCmd())

	return cmd
}

func newPullComponentCmd() *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:     "component <repo:tag>",
		Aliases: []string{"comp"},
		Short:   "Pull a component artifact from an OCI registry",
		Long: `Pull a component artifact from an OCI registry to the local cache.

This command downloads the component artifact and registers it in the local
component registry. The component can then be used for deployment or inspection.

Examples:
  arcctl pull component ghcr.io/myorg/myapp:v1.0.0
  arcctl pull component docker.io/library/nginx:latest`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reference := args[0]
			ctx := context.Background()

			if !quiet {
				fmt.Printf("Pulling component: %s\n", reference)
			}

			client := oci.NewClient()

			// Create cache directory for this component
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}

			cacheKey := strings.ReplaceAll(reference, "/", "_")
			cacheKey = strings.ReplaceAll(cacheKey, ":", "_")
			componentDir := filepath.Join(homeDir, ".arcctl", "cache", "components", cacheKey)

			// Remove old cache if exists
			if _, err := os.Stat(componentDir); err == nil {
				if !quiet {
					fmt.Printf("[pull] Removing existing cache...\n")
				}
				os.RemoveAll(componentDir)
			}

			// Create cache directory
			if err := os.MkdirAll(componentDir, 0755); err != nil {
				return fmt.Errorf("failed to create cache directory: %w", err)
			}

			if !quiet {
				fmt.Printf("[pull] Downloading %s...\n", reference)
			}

			// Pull the component
			if err := client.Pull(ctx, reference, componentDir); err != nil {
				return fmt.Errorf("failed to pull component: %w", err)
			}

			// Calculate size
			var totalSize int64
			err = filepath.Walk(componentDir, func(_ string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() {
					totalSize += info.Size()
				}
				return nil
			})
			if err != nil {
				totalSize = 0 // Non-fatal, just won't have accurate size
			}

			// Get digest if available
			digest := ""
			configData, err := client.PullConfig(ctx, reference)
			if err == nil && len(configData) > 0 {
				digest = fmt.Sprintf("sha256:%x", configData)[:71] + "..."
			}

			// Register in local registry
			reg, err := registry.NewRegistry()
			if err != nil {
				return fmt.Errorf("failed to create local registry: %w", err)
			}

			repo, tag := registry.ParseReference(reference)
			entry := registry.ComponentEntry{
				Reference:  reference,
				Repository: repo,
				Tag:        tag,
				Digest:     digest,
				Source:     registry.SourcePulled,
				Size:       totalSize,
				CreatedAt:  time.Now(),
				CachePath:  componentDir,
			}

			if err := reg.Add(entry); err != nil {
				return fmt.Errorf("failed to register component: %w", err)
			}

			if !quiet {
				fmt.Printf("[success] Pulled %s\n", reference)
				fmt.Printf("  Cache: %s\n", componentDir)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Suppress output")

	return cmd
}
