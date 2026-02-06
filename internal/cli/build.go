package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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
		Aliases: []string{"comp", "comps", "components"},
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
				// Check if path is a file or directory
				info, err := os.Stat(path)
				if err != nil {
					return fmt.Errorf("failed to access path: %w", err)
				}
				if info.IsDir() {
					// Look for architect.yml in the directory
					componentFile = filepath.Join(path, "architect.yml")
					if _, err := os.Stat(componentFile); os.IsNotExist(err) {
						componentFile = filepath.Join(path, "architect.yaml")
					}
				} else {
					// Path is a file, use it directly
					componentFile = path
					path = filepath.Dir(path)
				}
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

			// Process top-level builds
			for _, build := range comp.Builds() {
				childRef := fmt.Sprintf("%s-build-%s%s", baseRef, build.Name(), tagPart)
				childArtifacts[fmt.Sprintf("builds/%s", build.Name())] = childRef
			}

			// Process functions (only container-based functions have builds)
			for _, fn := range comp.Functions() {
				if fn.IsContainerBased() && fn.Container().Build() != nil {
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

			ctx := context.Background()

			// Collect build info for each child artifact
			type buildInfo struct {
				context    string
				dockerfile string
				target     string
				args       map[string]string
			}
			childBuilds := make(map[string]buildInfo)

			// Collect build info from top-level builds
			for _, build := range comp.Builds() {
				key := fmt.Sprintf("builds/%s", build.Name())
				childBuilds[key] = buildInfo{
					context:    build.Context(),
					dockerfile: build.Dockerfile(),
					target:     build.Target(),
					args:       build.Args(),
				}
			}

			// Collect build info from functions (only container-based functions)
			for _, fn := range comp.Functions() {
				if fn.IsContainerBased() && fn.Container().Build() != nil {
					key := fmt.Sprintf("functions/%s", fn.Name())
					childBuilds[key] = buildInfo{
						context:    fn.Container().Build().Context(),
						dockerfile: fn.Container().Build().Dockerfile(),
						target:     fn.Container().Build().Target(),
						args:       fn.Container().Build().Args(),
					}
				}
			}

			// Collect build info from cronjobs
			for _, cj := range comp.Cronjobs() {
				if cj.Build() != nil {
					key := fmt.Sprintf("cronjobs/%s", cj.Name())
					childBuilds[key] = buildInfo{
						context:    cj.Build().Context(),
						dockerfile: cj.Build().Dockerfile(),
						target:     cj.Build().Target(),
						args:       cj.Build().Args(),
					}
				}
			}

			// Collect build info from migrations
			for _, db := range comp.Databases() {
				if db.Migrations() != nil && db.Migrations().Build() != nil {
					key := fmt.Sprintf("migrations/%s", db.Name())
					childBuilds[key] = buildInfo{
						context:    db.Migrations().Build().Context(),
						dockerfile: db.Migrations().Build().Dockerfile(),
						target:     db.Migrations().Build().Target(),
						args:       db.Migrations().Build().Args(),
					}
				}
			}

			// Build child artifacts (container images)
			fmt.Println()
			for resource, ref := range childArtifacts {
				fmt.Printf("[build] Building %s...\n", resource)

				build, ok := childBuilds[resource]
				if !ok {
					fmt.Printf("[warn] No build info found for %s, skipping\n", resource)
					continue
				}

				// Resolve build context relative to component directory
				buildContext := build.context
				if !filepath.IsAbs(buildContext) {
					buildContext = filepath.Join(path, buildContext)
				}

				// Build Docker image
				dockerArgs := []string{"build", "-t", ref}

				// Add dockerfile if specified
				if build.dockerfile != "" {
					dockerfilePath := build.dockerfile
					if !filepath.IsAbs(dockerfilePath) {
						dockerfilePath = filepath.Join(path, dockerfilePath)
					}
					dockerArgs = append(dockerArgs, "-f", dockerfilePath)
				}

				// Add build target if specified
				if build.target != "" {
					dockerArgs = append(dockerArgs, "--target", build.target)
				}

				// Add platform if specified
				if platform != "" {
					dockerArgs = append(dockerArgs, "--platform", platform)
				}

				// Add no-cache if specified
				if noCache {
					dockerArgs = append(dockerArgs, "--no-cache")
				}

				// Add build arguments
				for k, v := range build.args {
					dockerArgs = append(dockerArgs, "--build-arg", fmt.Sprintf("%s=%s", k, v))
				}

				// Add build context
				dockerArgs = append(dockerArgs, buildContext)

				// Execute docker build
				dockerCmd := exec.CommandContext(ctx, "docker", dockerArgs...)
				dockerCmd.Stdout = os.Stdout
				dockerCmd.Stderr = os.Stderr

				if err := dockerCmd.Run(); err != nil {
					return fmt.Errorf("failed to build %s: %w", resource, err)
				}

				fmt.Printf("[success] Built %s → %s\n", resource, ref)
			}

			// Build root artifact
			fmt.Printf("[build] Building root artifact...\n")

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
		push       bool
	)

	cmd := &cobra.Command{
		Use:     "datacenter [path]",
		Aliases: []string{"dc", "dcs", "datacenters"},
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

			// Determine datacenter file location and normalize path to directory
			dcFile := file
			dcDir := path
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
					// Path is a file; derive directory from it
					dcFile = path
					dcDir = filepath.Dir(path)
				}
			} else {
				// Explicit file provided; derive directory from it
				dcDir = filepath.Dir(dcFile)
			}

			// Load and validate the datacenter
			loader := datacenter.NewLoader()
			dc, err := loader.Load(dcFile)
			if err != nil {
				return fmt.Errorf("failed to load datacenter: %w", err)
			}

			fmt.Printf("Building datacenter: %s\n\n", filepath.Base(dcDir))

			// Collect all modules from datacenter, environment, and hooks
			allModules := collectAllModules(dc, dcDir)

			// Build the module artifact reference map from discovered modules
			moduleArtifacts := make(map[string]string)
			baseRef := tag
			tagPart := ""
			if idx := strings.LastIndex(tag, ":"); idx != -1 {
				baseRef = tag[:idx]
				tagPart = tag[idx:]
			}

			for modulePath := range allModules {
				// Generate OCI reference for this module (e.g., repo-module-name:tag)
				modName := strings.TrimPrefix(modulePath, "module/")
				modRef := fmt.Sprintf("%s-module-%s%s", baseRef, modName, tagPart)
				moduleArtifacts[modulePath] = modRef
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
				fmt.Printf("Module artifacts to build (%d):\n", len(moduleArtifacts))
				for module, ref := range moduleArtifacts {
					fmt.Printf("  %-24s → %s\n", module, ref)
				}
				fmt.Println()
			}

			if dryRun {
				fmt.Println("Dry run - no artifacts were built.")
				return nil
			}

			ctx := context.Background()

			// Create module builder
			moduleBuilder, err := createModuleBuilder()
			if err != nil {
				return fmt.Errorf("failed to create module builder: %w", err)
			}
			defer moduleBuilder.Close()

			// Build (and optionally push) each module
			for modulePath, ref := range moduleArtifacts {
				modInfo := allModules[modulePath]
				fmt.Printf("[build] Building %s...\n", modulePath)

				// Build the module container image
				buildResult, err := moduleBuilder.Build(ctx, modInfo.sourceDir, modInfo.plugin, ref)
				if err != nil {
					return fmt.Errorf("failed to build module %s: %w", modulePath, err)
				}

				fmt.Printf("[success] Built %s (%s)\n", ref, buildResult.ModuleType)

				// Push module image if --push flag is set (native modules are local-only)
				if push && buildResult.ModuleType != "native" {
					fmt.Printf("[push] Pushing module %s...\n", ref)
					if err := moduleBuilder.Push(ctx, ref); err != nil {
						return fmt.Errorf("failed to push module %s: %w", modulePath, err)
					}
					fmt.Printf("[success] Pushed %s\n", ref)
				}
			}

			// Build root artifact
			fmt.Printf("[build] Building root artifact...\n")
			client := oci.NewClient()

			// Create artifact config
			config := &oci.DatacenterConfig{
				SchemaVersion:   "v1",
				Name:            filepath.Base(dcDir),
				ModuleArtifacts: moduleArtifacts,
				BuildTime:       time.Now().UTC().Format(time.RFC3339),
			}

			// Build artifact from datacenter directory
			artifact, err := client.BuildFromDirectory(ctx, dcDir, oci.ArtifactTypeDatacenter, config)
			if err != nil {
				return fmt.Errorf("failed to build artifact: %w", err)
			}

			artifact.Reference = tag
			fmt.Printf("[success] Built %s\n", tag)

			// Push to remote registry if --push flag is set
			if push {
				fmt.Printf("[push] Pushing %s...\n", tag)
				if err := client.Push(ctx, artifact); err != nil {
					return fmt.Errorf("failed to push artifact: %w", err)
				}
				fmt.Printf("[success] Pushed %s\n", tag)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&tag, "tag", "t", "", "Tag for the root datacenter artifact (required)")
	cmd.Flags().StringArrayVar(&moduleTags, "module-tag", nil, "Override tag for a specific module (name=repo:tag)")
	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to datacenter.hcl if not in default location")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be built without building")
	cmd.Flags().BoolVar(&push, "push", false, "Push to registry after building")
	_ = cmd.MarkFlagRequired("tag")

	return cmd
}
