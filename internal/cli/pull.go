package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/davidthor/cldctl/pkg/oci"
	"github.com/davidthor/cldctl/pkg/registry"
	"github.com/spf13/cobra"
)

func newPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull artifacts from registry",
		Long:  `Commands for pulling artifacts from OCI registries.`,
	}

	cmd.AddCommand(newPullComponentCmd())
	cmd.AddCommand(newPullDatacenterCmd())

	return cmd
}

func newPullComponentCmd() *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:     "component <repo:tag>",
		Aliases: []string{"comp", "comps", "components"},
		Short:   "Pull a component artifact from an OCI registry",
		Long: `Pull a component artifact from an OCI registry to the local cache.

This command downloads the component artifact and registers it in the local
artifact registry. The component can then be used for deployment or inspection.

Examples:
  cldctl pull component ghcr.io/myorg/myapp:v1.0.0
  cldctl pull component docker.io/library/nginx:latest`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			reference := args[0]
			ctx := context.Background()

			if !quiet {
				fmt.Printf("Pulling component: %s\n", reference)
			}

			client := oci.NewClient()

			// Create cache directory for this artifact
			componentDir, err := registry.CachePathForRef(reference)
			if err != nil {
				return fmt.Errorf("failed to compute cache path: %w", err)
			}

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
			entry := registry.ArtifactEntry{
				Reference:  reference,
				Repository: repo,
				Tag:        tag,
				Type:       registry.TypeComponent,
				Digest:     digest,
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

func newPullDatacenterCmd() *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:     "datacenter <repo:tag>",
		Aliases: []string{"dc", "dcs", "datacenters"},
		Short:   "Pull a datacenter artifact from an OCI registry",
		Long: `Pull a datacenter artifact from an OCI registry to the local cache.

This command downloads the datacenter artifact and registers it in the local
artifact registry. The datacenter can then be used for deployment.

Examples:
  cldctl pull datacenter docker.io/davidthor/startup-datacenter:latest
  cldctl pull datacenter ghcr.io/myorg/my-dc:v1.0.0`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			reference := args[0]
			ctx := context.Background()

			if !quiet {
				fmt.Printf("Pulling datacenter: %s\n", reference)
			}

			client := oci.NewClient()

			// Create cache directory for this artifact
			dcDir, err := registry.CachePathForRef(reference)
			if err != nil {
				return fmt.Errorf("failed to compute cache path: %w", err)
			}

			// Remove old cache if exists
			if _, err := os.Stat(dcDir); err == nil {
				if !quiet {
					fmt.Printf("[pull] Removing existing cache...\n")
				}
				os.RemoveAll(dcDir)
			}

			// Create cache directory
			if err := os.MkdirAll(dcDir, 0755); err != nil {
				return fmt.Errorf("failed to create cache directory: %w", err)
			}

			if !quiet {
				fmt.Printf("[pull] Downloading %s...\n", reference)
			}

			// Pull the datacenter
			if err := client.Pull(ctx, reference, dcDir); err != nil {
				return fmt.Errorf("failed to pull datacenter: %w", err)
			}

			// Calculate size
			var totalSize int64
			err = filepath.Walk(dcDir, func(_ string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() {
					totalSize += info.Size()
				}
				return nil
			})
			if err != nil {
				totalSize = 0
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
			entry := registry.ArtifactEntry{
				Reference:  reference,
				Repository: repo,
				Tag:        tag,
				Type:       registry.TypeDatacenter,
				Digest:     digest,
				Size:       totalSize,
				CreatedAt:  time.Now(),
				CachePath:  dcDir,
			}

			if err := reg.Add(entry); err != nil {
				return fmt.Errorf("failed to register datacenter: %w", err)
			}

			if !quiet {
				fmt.Printf("[success] Pulled %s\n", reference)
				fmt.Printf("  Cache: %s\n", dcDir)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Suppress output")

	return cmd
}
