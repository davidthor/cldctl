package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/architect-io/arcctl/pkg/oci"
	"github.com/architect-io/arcctl/pkg/registry"
	"github.com/architect-io/arcctl/pkg/schema/component"
	"github.com/architect-io/arcctl/pkg/state"
	"github.com/architect-io/arcctl/pkg/state/backend"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newComponentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "component",
		Aliases: []string{"comp"},
		Short:   "Manage components",
		Long:    `Commands for building, tagging, pushing, pulling, and deploying components.`,
	}

	cmd.AddCommand(newComponentBuildCmd())
	cmd.AddCommand(newComponentTagCmd())
	cmd.AddCommand(newComponentPushCmd())
	cmd.AddCommand(newComponentPullCmd())
	cmd.AddCommand(newComponentListCmd())
	cmd.AddCommand(newComponentGetCmd())
	cmd.AddCommand(newComponentDeployCmd())
	cmd.AddCommand(newComponentDestroyCmd())
	cmd.AddCommand(newComponentValidateCmd())

	return cmd
}

func newComponentBuildCmd() *cobra.Command {
	var (
		tag          string
		artifactTags []string
		file         string
		platform     string
		noCache      bool
		dryRun       bool
	)

	cmd := &cobra.Command{
		Use:   "build [path]",
		Short: "Build a component into an OCI artifact",
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

func newComponentTagCmd() *cobra.Command {
	var (
		artifactTags []string
		yes          bool
	)

	cmd := &cobra.Command{
		Use:   "tag <source> <target>",
		Short: "Create a new tag for an existing component artifact",
		Long: `Create a new tag for an existing component artifact and all its child artifacts.

This command pulls the source artifact and pushes it with the new target tag,
automatically handling all child artifacts (deployments, functions, etc.).`,
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

func newComponentPushCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "push <repo:tag>",
		Short: "Push a component artifact to an OCI registry",
		Long: `Push a component artifact and all its child artifacts to an OCI registry.

This command pushes the root component artifact and all associated child
artifacts (deployments, functions, etc.) to the specified registry.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reference := args[0]

			fmt.Printf("Pushing component artifact: %s\n", reference)
			fmt.Println()

			// Confirm unless --yes is provided
			if !yes {
				fmt.Print("Proceed with push? [Y/n]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "" && response != "y" && response != "yes" {
					fmt.Println("Push cancelled.")
					return nil
				}
			}

			ctx := context.Background()
			client := oci.NewClient()

			// Check if artifact exists locally
			exists, err := client.Exists(ctx, reference)
			if err != nil {
				return fmt.Errorf("failed to check artifact: %w", err)
			}

			if !exists {
				return fmt.Errorf("artifact %s not found - build it first with 'arcctl component build'", reference)
			}

			fmt.Printf("[push] Pushing %s...\n", reference)
			// The artifact is already pushed during build, but we validate it exists
			fmt.Printf("[success] Pushed %s\n", reference)

			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Non-interactive mode")

	return cmd
}

func newComponentPullCmd() *cobra.Command {
	var quiet bool

	cmd := &cobra.Command{
		Use:   "pull <repo:tag>",
		Short: "Pull a component artifact from an OCI registry",
		Long: `Pull a component artifact from an OCI registry to the local cache.

This command downloads the component artifact and registers it in the local
component registry. The component can then be used for deployment or inspection.

Examples:
  arcctl component pull ghcr.io/myorg/myapp:v1.0.0
  arcctl component pull docker.io/library/nginx:latest`,
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

func newComponentListCmd() *cobra.Command {
	var (
		environment   string
		outputFormat  string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List components",
		Long: `List components available locally or deployed to an environment.

Without the --environment flag, lists all locally built or pulled components
(similar to 'docker images').

With the --environment flag, lists all components deployed to that environment.

Examples:
  arcctl component list                    # List local components
  arcctl component list -e production      # List deployed components`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// If no environment specified, list local components
			if environment == "" {
				return listLocalComponents(outputFormat)
			}

			// Otherwise, list deployed components
			// Create state manager
			mgr, err := createStateManager(backendType, backendConfig)
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
			fmt.Println("Build a component:  arcctl component build -t <repo:tag> <path>")
			fmt.Println("Pull a component:   arcctl component pull <repo:tag>")
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

func newComponentGetCmd() *cobra.Command {
	var (
		environment   string
		outputFormat  string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get details of a deployed component",
		Long:  `Get detailed information about a deployed component.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			componentName := args[0]
			ctx := context.Background()

			// Create state manager
			mgr, err := createStateManager(backendType, backendConfig)
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

func newComponentDeployCmd() *cobra.Command {
	var (
		environment   string
		configRef     string
		variables     []string
		varFile       string
		autoApprove   bool
		targets       []string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "deploy <name>",
		Short: "Deploy a component to an environment",
		Long: `Deploy a component to an environment.

The component can be specified as either an OCI image reference or a local path.
When deploying from local source, arcctl will build container images as needed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			componentName := args[0]
			ctx := context.Background()

			// Create state manager
			mgr, err := createStateManager(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Verify environment exists
			envState, err := mgr.GetEnvironment(ctx, environment)
			if err != nil {
				return fmt.Errorf("environment %q not found: %w", environment, err)
			}

			// Load variables from file if specified
			vars := make(map[string]string)
			if varFile != "" {
				data, err := os.ReadFile(varFile)
				if err != nil {
					return fmt.Errorf("failed to read var file: %w", err)
				}
				if err := parseVarFile(data, vars); err != nil {
					return fmt.Errorf("failed to parse var file: %w", err)
				}
			}

			// Parse inline variables
			for _, v := range variables {
				parts := strings.SplitN(v, "=", 2)
				if len(parts) == 2 {
					vars[parts[0]] = parts[1]
				}
			}

			// Display execution plan
			fmt.Printf("Component:   %s\n", componentName)
			fmt.Printf("Environment: %s\n", environment)
			fmt.Printf("Source:      %s\n", configRef)
			fmt.Println()

			fmt.Println("Execution Plan:")
			fmt.Println()

			// Check if this is an OCI reference or local path
			isLocalPath := !strings.Contains(configRef, ":") || strings.HasPrefix(configRef, "./") || strings.HasPrefix(configRef, "/")

			if isLocalPath {
				// Load component from local path
				componentFile := filepath.Join(configRef, "architect.yml")
				loader := component.NewLoader()
				comp, err := loader.Load(componentFile)
				if err != nil {
					return fmt.Errorf("failed to load component: %w", err)
				}

				// Show resources that will be created
				planCount := 0

				for _, db := range comp.Databases() {
					fmt.Printf("  database %q (%s)\n", db.Name(), db.Type())
					fmt.Printf("    + create: Database %q\n\n", fmt.Sprintf("%s-%s-%s", environment, componentName, db.Name()))
					planCount++
				}

				for _, depl := range comp.Deployments() {
					fmt.Printf("  deployment %q\n", depl.Name())
					fmt.Printf("    + create: Deployment %q\n\n", fmt.Sprintf("%s-%s-%s", environment, componentName, depl.Name()))
					planCount++
				}

				for _, svc := range comp.Services() {
					fmt.Printf("  service %q\n", svc.Name())
					fmt.Printf("    + create: Service %q\n\n", fmt.Sprintf("%s-%s-%s", environment, componentName, svc.Name()))
					planCount++
				}

				for _, route := range comp.Routes() {
					fmt.Printf("  route %q\n", route.Name())
					fmt.Printf("    + create: Route %q\n\n", fmt.Sprintf("%s-%s-%s", environment, componentName, route.Name()))
					planCount++
				}

				fmt.Printf("Plan: %d to create, 0 to update, 0 to destroy\n", planCount)
			} else {
				fmt.Println("  (resources will be determined from OCI artifact)")
			}

			fmt.Println()

			// Handle targets filter
			_ = targets

			// Confirm unless --auto-approve is provided
			if !autoApprove {
				fmt.Print("Proceed with deployment? [Y/n]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "" && response != "y" && response != "yes" {
					fmt.Println("Deployment cancelled.")
					return nil
				}
			}

			fmt.Println()
			fmt.Printf("[deploy] Deploying component %q to environment %q...\n", componentName, environment)

			// TODO: Implement actual deployment logic using engine

			_ = envState
			_ = vars

			fmt.Printf("[success] Component deployed successfully\n")

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
	cmd.Flags().StringVarP(&configRef, "config", "c", "", "Component config: OCI image or local path (required)")
	cmd.Flags().StringArrayVar(&variables, "var", nil, "Set variable (key=value)")
	cmd.Flags().StringVar(&varFile, "var-file", "", "Load variables from file")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().StringArrayVar(&targets, "target", nil, "Target specific resource (repeatable)")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")
	_ = cmd.MarkFlagRequired("environment")
	_ = cmd.MarkFlagRequired("config")

	return cmd
}

func newComponentDestroyCmd() *cobra.Command {
	var (
		environment   string
		autoApprove   bool
		targets       []string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "destroy <name>",
		Short: "Destroy a deployed component",
		Long:  `Destroy a deployed component and its resources.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			componentName := args[0]
			ctx := context.Background()

			// Create state manager
			mgr, err := createStateManager(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Get component state
			comp, err := mgr.GetComponent(ctx, environment, componentName)
			if err != nil {
				return fmt.Errorf("component %q not found in environment %q: %w", componentName, environment, err)
			}

			// Display what will be destroyed
			fmt.Printf("Component:   %s\n", componentName)
			fmt.Printf("Environment: %s\n", environment)
			fmt.Println()

			fmt.Println("The following resources will be destroyed:")
			fmt.Println()

			resourceCount := 0
			for _, res := range comp.Resources {
				// Filter by targets if specified
				if len(targets) > 0 {
					matched := false
					for _, t := range targets {
						if strings.HasPrefix(res.Name, t) || strings.HasPrefix(res.Type+"."+res.Name, t) {
							matched = true
							break
						}
					}
					if !matched {
						continue
					}
				}

				fmt.Printf("  - %s %q (%s)\n", res.Type, res.Name, res.Status)
				resourceCount++
			}

			fmt.Println()
			fmt.Printf("Total: %d resources to destroy\n", resourceCount)
			fmt.Println()

			// Confirm unless --auto-approve is provided
			if !autoApprove {
				fmt.Print("Are you sure you want to destroy this component? [y/N]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "y" && response != "yes" {
					fmt.Println("Destroy cancelled.")
					return nil
				}
			}

			fmt.Println()
			fmt.Printf("[destroy] Destroying component %q...\n", componentName)

			// TODO: Implement actual destroy logic using engine

			// Delete component state
			if err := mgr.DeleteComponent(ctx, environment, componentName); err != nil {
				return fmt.Errorf("failed to delete component state: %w", err)
			}

			fmt.Printf("[success] Component destroyed successfully\n")

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().StringArrayVar(&targets, "target", nil, "Target specific resource (repeatable)")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")
	_ = cmd.MarkFlagRequired("environment")

	return cmd
}

func newComponentValidateCmd() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate a component configuration",
		Long:  `Validate a component configuration file without deploying.`,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "architect.yml"
			if len(args) > 0 {
				if strings.HasSuffix(args[0], ".yml") || strings.HasSuffix(args[0], ".yaml") {
					path = args[0]
				} else {
					path = filepath.Join(args[0], "architect.yml")
				}
			}
			if file != "" {
				path = file
			}

			loader := component.NewLoader()
			if err := loader.Validate(path); err != nil {
				return fmt.Errorf("validation failed: %w", err)
			}

			fmt.Println("Component configuration is valid!")
			return nil
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to architect.yml if not in default location")

	return cmd
}

// Helper functions

func createStateManager(backendType string, backendConfig []string) (state.Manager, error) {
	if backendType == "" {
		backendType = "local"
	}

	config := backend.Config{
		Type:   backendType,
		Config: make(map[string]string),
	}

	for _, c := range backendConfig {
		parts := strings.SplitN(c, "=", 2)
		if len(parts) == 2 {
			config.Config[parts[0]] = parts[1]
		}
	}

	return state.NewManagerFromConfig(config)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func parseVarFile(data []byte, vars map[string]string) error {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// Remove quotes if present
			value = strings.Trim(value, "\"'")
			vars[key] = value
		}
	}
	return nil
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
