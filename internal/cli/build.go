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
	"github.com/architect-io/arcctl/pkg/schema/component"
	"github.com/architect-io/arcctl/pkg/schema/datacenter"
	"github.com/spf13/cobra"
)

func newBuildCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build artifacts",
		Long:  `Commands for building components and datacenters into OCI artifacts.`,
	}

	cmd.AddCommand(newBuildComponentCmd())
	cmd.AddCommand(newBuildDatacenterCmd())

	return cmd
}

func newBuildComponentCmd() *cobra.Command {
	var (
		tag          string
		artifactTags []string
		file         string
		platform     string
		noCache      bool
		dryRun       bool
	)

	cmd := &cobra.Command{
		Use:     "component [path]",
		Aliases: []string{"comp"},
		Short:   "Build a component into an OCI artifact",
		Long: `Build a component and its container images into OCI artifacts.

When building a component, arcctl creates multiple artifacts:
  - Root artifact containing the component configuration
  - Child artifacts for each deployment, function, cronjob, and migration`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) > 0 {
				path = args[0]
			}

			// Determine architect.yml location
			componentFile := file
			if componentFile == "" {
				componentFile = filepath.Join(path, "architect.yml")
			}

			// Load and validate the component
			loader := component.NewLoader()
			comp, err := loader.Load(componentFile)
			if err != nil {
				return fmt.Errorf("failed to load component: %w", err)
			}

			fmt.Printf("Building component from: %s\n\n", componentFile)

			// Determine child artifacts
			childArtifacts := make(map[string]string)
			baseRef := strings.TrimSuffix(tag, filepath.Ext(tag))
			tagPart := ""
			if idx := strings.LastIndex(tag, ":"); idx != -1 {
				baseRef = tag[:idx]
				tagPart = tag[idx:]
			}

			// Process deployments
			for _, depl := range comp.Deployments() {
				if depl.Build() != nil {
					childRef := fmt.Sprintf("%s-deployment-%s%s", baseRef, depl.Name(), tagPart)
					childArtifacts[fmt.Sprintf("deployments/%s", depl.Name())] = childRef
				}
			}

			// Process functions
			for _, fn := range comp.Functions() {
				if fn.Build() != nil {
					childRef := fmt.Sprintf("%s-function-%s%s", baseRef, fn.Name(), tagPart)
					childArtifacts[fmt.Sprintf("functions/%s", fn.Name())] = childRef
				}
			}

			// Process cronjobs
			for _, cj := range comp.Cronjobs() {
				if cj.Build() != nil {
					childRef := fmt.Sprintf("%s-cronjob-%s%s", baseRef, cj.Name(), tagPart)
					childArtifacts[fmt.Sprintf("cronjobs/%s", cj.Name())] = childRef
				}
			}

			// Process database migrations
			for _, db := range comp.Databases() {
				if db.Migrations() != nil && db.Migrations().Build() != nil {
					childRef := fmt.Sprintf("%s-migration-%s%s", baseRef, db.Name(), tagPart)
					childArtifacts[fmt.Sprintf("migrations/%s", db.Name())] = childRef
				}
			}

			// Apply artifact tag overrides
			for _, override := range artifactTags {
				parts := strings.SplitN(override, "=", 2)
				if len(parts) == 2 {
					childArtifacts[parts[0]] = parts[1]
				}
			}

			// Display child artifacts if any
			if len(childArtifacts) > 0 {
				fmt.Println("Child artifacts to build:")
				for resource, ref := range childArtifacts {
					fmt.Printf("  %-24s → %s\n", resource, ref)
				}
				fmt.Println()
			}

			if dryRun {
				fmt.Println("Dry run - no artifacts were built.")
				return nil
			}

			// Build child artifacts (container images)
			fmt.Println()
			for resource, ref := range childArtifacts {
				fmt.Printf("[build] Building %s...\n", resource)
				_ = ref
				_ = platform
				_ = noCache
				// TODO: Implement actual Docker build
			}

			// Build root artifact
			fmt.Printf("[build] Building root artifact...\n")

			ctx := context.Background()
			client := oci.NewClient()

			// Create artifact config
			config := &oci.ComponentConfig{
				SchemaVersion:  "v1",
				Readme:         comp.Readme(),
				ChildArtifacts: childArtifacts,
				BuildTime:      time.Now().UTC().Format(time.RFC3339),
			}

			// Build artifact from component directory
			artifact, err := client.BuildFromDirectory(ctx, path, oci.ArtifactTypeComponent, config)
			if err != nil {
				return fmt.Errorf("failed to build artifact: %w", err)
			}

			artifact.Reference = tag

			// Register in local registry
			reg, err := registry.NewRegistry()
			if err != nil {
				return fmt.Errorf("failed to create local registry: %w", err)
			}

			repo, tagPortion := registry.ParseReference(tag)
			var totalSize int64
			for _, layer := range artifact.Layers {
				totalSize += int64(len(layer.Data))
			}

			// Calculate cache path for the component
			homeDir, _ := os.UserHomeDir()
			cacheKey := strings.ReplaceAll(tag, "/", "_")
			cacheKey = strings.ReplaceAll(cacheKey, ":", "_")
			cachePath := filepath.Join(homeDir, ".arcctl", "cache", "components", cacheKey)

			entry := registry.ComponentEntry{
				Reference:  tag,
				Repository: repo,
				Tag:        tagPortion,
				Digest:     artifact.Digest,
				Source:     registry.SourceBuilt,
				Size:       totalSize,
				CreatedAt:  time.Now(),
				CachePath:  cachePath,
			}

			if err := reg.Add(entry); err != nil {
				return fmt.Errorf("failed to register component: %w", err)
			}

			fmt.Printf("[success] Built %s\n", tag)

			return nil
		},
	}

	cmd.Flags().StringVarP(&tag, "tag", "t", "", "Tag for the root component artifact (required)")
	cmd.Flags().StringArrayVar(&artifactTags, "artifact-tag", nil, "Override tag for a specific child artifact (name=repo:tag)")
	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to architect.yml if not in default location")
	cmd.Flags().StringVar(&platform, "platform", "", "Target platform (linux/amd64, linux/arm64)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "Disable build cache")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be built without building")
	_ = cmd.MarkFlagRequired("tag")

	return cmd
}

func newBuildDatacenterCmd() *cobra.Command {
	var (
		tag        string
		moduleTags []string
		file       string
		dryRun     bool
	)

	cmd := &cobra.Command{
		Use:     "datacenter [path]",
		Aliases: []string{"dc"},
		Short:   "Build a datacenter into an OCI artifact",
		Long: `Build a datacenter and its IaC modules into OCI artifacts.

When building a datacenter, arcctl bundles all IaC modules:
  - Root artifact containing the datacenter configuration
  - Module artifacts for each IaC module referenced`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) > 0 {
				path = args[0]
			}

			// Determine datacenter file location
			dcFile := file
			if dcFile == "" {
				// Check if path is a file or directory
				info, err := os.Stat(path)
				if err != nil {
					return fmt.Errorf("failed to access path: %w", err)
				}
				if info.IsDir() {
					// Look for datacenter file in directory
					// Try datacenter.dc first, then datacenter.hcl
					dcFile = filepath.Join(path, "datacenter.dc")
					if _, err := os.Stat(dcFile); os.IsNotExist(err) {
						dcFile = filepath.Join(path, "datacenter.hcl")
					}
				} else {
					dcFile = path
				}
			}

			// Load and validate the datacenter
			loader := datacenter.NewLoader()
			dc, err := loader.Load(dcFile)
			if err != nil {
				return fmt.Errorf("failed to load datacenter: %w", err)
			}

			fmt.Printf("Building datacenter: %s\n\n", filepath.Base(path))

			// Determine module artifacts
			moduleArtifacts := make(map[string]string)
			baseRef := strings.TrimSuffix(tag, filepath.Ext(tag))
			tagPart := ""
			if idx := strings.LastIndex(tag, ":"); idx != -1 {
				baseRef = tag[:idx]
				tagPart = tag[idx:]
			}

			// Process modules
			for _, mod := range dc.Modules() {
				if mod.Source() != "" && !strings.HasPrefix(mod.Source(), "oci://") {
					// Local module, needs to be built
					modRef := fmt.Sprintf("%s-module-%s%s", baseRef, mod.Name(), tagPart)
					moduleArtifacts[fmt.Sprintf("module/%s", mod.Name())] = modRef
				}
			}

			// Apply module tag overrides
			for _, override := range moduleTags {
				parts := strings.SplitN(override, "=", 2)
				if len(parts) == 2 {
					moduleArtifacts[parts[0]] = parts[1]
				}
			}

			// Display module artifacts if any
			if len(moduleArtifacts) > 0 {
				fmt.Println("Module artifacts to build:")
				for module, ref := range moduleArtifacts {
					fmt.Printf("  %-24s → %s\n", module, ref)
				}
				fmt.Println()
			}

			if dryRun {
				fmt.Println("Dry run - no artifacts were built.")
				return nil
			}

			// Build module artifacts
			fmt.Println()
			ctx := context.Background()

			// Create module builder
			moduleBuilder, err := createModuleBuilder()
			if err != nil {
				return fmt.Errorf("failed to create module builder: %w", err)
			}
			defer moduleBuilder.Close()

			// Collect all modules from datacenter and hooks
			allModules := collectAllModules(dc, path)

			for modulePath, ref := range moduleArtifacts {
				fmt.Printf("[build] Building %s...\n", modulePath)

				// Find the module source directory
				modInfo, ok := allModules[modulePath]
				if !ok {
					fmt.Printf("[warn] Module %s not found, skipping\n", modulePath)
					continue
				}

				// Build the module container image
				buildResult, err := moduleBuilder.Build(ctx, modInfo.sourceDir, modInfo.plugin, ref)
				if err != nil {
					return fmt.Errorf("failed to build module %s: %w", modulePath, err)
				}

				fmt.Printf("[success] Built %s (%s)\n", ref, buildResult.ModuleType)
			}

			// Build root artifact
			fmt.Printf("[build] Building root artifact...\n")
			client := oci.NewClient()

			// Create artifact config
			config := &oci.DatacenterConfig{
				SchemaVersion:   "v1",
				Name:            filepath.Base(path),
				ModuleArtifacts: moduleArtifacts,
				BuildTime:       time.Now().UTC().Format(time.RFC3339),
			}

			// Build artifact from datacenter directory
			artifact, err := client.BuildFromDirectory(ctx, path, oci.ArtifactTypeDatacenter, config)
			if err != nil {
				return fmt.Errorf("failed to build artifact: %w", err)
			}

			artifact.Reference = tag
			fmt.Printf("[success] Built %s\n", tag)

			return nil
		},
	}

	cmd.Flags().StringVarP(&tag, "tag", "t", "", "Tag for the root datacenter artifact (required)")
	cmd.Flags().StringArrayVar(&moduleTags, "module-tag", nil, "Override tag for a specific module (name=repo:tag)")
	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to datacenter.hcl if not in default location")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be built without building")
	_ = cmd.MarkFlagRequired("tag")

	return cmd
}
