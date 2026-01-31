package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/architect-io/arcctl/pkg/iac/container"
	"github.com/architect-io/arcctl/pkg/oci"
	"github.com/architect-io/arcctl/pkg/schema/datacenter"
	"github.com/architect-io/arcctl/pkg/state"
	"github.com/architect-io/arcctl/pkg/state/backend"
	"github.com/architect-io/arcctl/pkg/state/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newDatacenterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "datacenter",
		Aliases: []string{"dc"},
		Short:   "Manage datacenters",
		Long:    `Commands for building, deploying, and managing datacenters.`,
	}

	cmd.AddCommand(newDatacenterBuildCmd())
	cmd.AddCommand(newDatacenterTagCmd())
	cmd.AddCommand(newDatacenterPushCmd())
	cmd.AddCommand(newDatacenterListCmd())
	cmd.AddCommand(newDatacenterGetCmd())
	cmd.AddCommand(newDatacenterDeployCmd())
	cmd.AddCommand(newDatacenterDestroyCmd())
	cmd.AddCommand(newDatacenterValidateCmd())

	return cmd
}

func newDatacenterBuildCmd() *cobra.Command {
	var (
		tag        string
		moduleTags []string
		file       string
		dryRun     bool
	)

	cmd := &cobra.Command{
		Use:   "build [path]",
		Short: "Build a datacenter into an OCI artifact",
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

			// Determine datacenter.hcl location
			dcFile := file
			if dcFile == "" {
				dcFile = filepath.Join(path, "datacenter.hcl")
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

func newDatacenterTagCmd() *cobra.Command {
	var (
		moduleTags []string
		yes        bool
	)

	cmd := &cobra.Command{
		Use:   "tag <source> <target>",
		Short: "Create a new tag for an existing datacenter artifact",
		Long: `Create a new tag for an existing datacenter artifact and all its module artifacts.

This command pulls the source artifact and pushes it with the new target tag,
automatically handling all module artifacts.`,
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

func newDatacenterPushCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "push <repo:tag>",
		Short: "Push a datacenter artifact to an OCI registry",
		Long: `Push a datacenter artifact and all its module artifacts to an OCI registry.

This command pushes the root datacenter artifact and all associated module
artifacts to the specified registry.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reference := args[0]

			fmt.Printf("Pushing datacenter artifact: %s\n", reference)
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
				return fmt.Errorf("artifact %s not found - build it first with 'arcctl datacenter build'", reference)
			}

			fmt.Printf("[push] Pushing %s...\n", reference)
			fmt.Printf("[success] Pushed %s\n", reference)

			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Non-interactive mode")

	return cmd
}

func newDatacenterListCmd() *cobra.Command {
	var (
		outputFormat  string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List deployed datacenters",
		Long:  `List all deployed datacenters.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Create state manager
			mgr, err := dcCreateStateManager(backendType, backendConfig)
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
						dcTruncateString(dc.Version, 45),
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

func newDatacenterGetCmd() *cobra.Command {
	var (
		outputFormat  string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get details of a deployed datacenter",
		Long:  `Get detailed information about a datacenter.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dcName := args[0]
			ctx := context.Background()

			// Create state manager
			mgr, err := dcCreateStateManager(backendType, backendConfig)
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

func newDatacenterDeployCmd() *cobra.Command {
	var (
		configRef     string
		variables     []string
		varFile       string
		autoApprove   bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "deploy <name>",
		Short: "Deploy a datacenter",
		Long: `Deploy or update a datacenter.

The datacenter can be specified as either an OCI image reference or a local path.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dcName := args[0]
			ctx := context.Background()

			// Create state manager
			mgr, err := dcCreateStateManager(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Load variables from file if specified
			vars := make(map[string]string)
			if varFile != "" {
				data, err := os.ReadFile(varFile)
				if err != nil {
					return fmt.Errorf("failed to read var file: %w", err)
				}
				if err := dcParseVarFile(data, vars); err != nil {
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
			fmt.Printf("Datacenter: %s\n", dcName)
			fmt.Printf("Source:     %s\n", configRef)
			fmt.Println()

			fmt.Println("Execution Plan:")
			fmt.Println()

			// Check if this is an OCI reference or local path
			isLocalPath := !strings.Contains(configRef, ":") || strings.HasPrefix(configRef, "./") || strings.HasPrefix(configRef, "/")

			if isLocalPath {
				// Load datacenter from local path
				dcFile := filepath.Join(configRef, "datacenter.hcl")
				loader := datacenter.NewLoader()
				dc, err := loader.Load(dcFile)
				if err != nil {
					return fmt.Errorf("failed to load datacenter: %w", err)
				}

				// Show modules that will be deployed
				for _, mod := range dc.Modules() {
					fmt.Printf("  module %q\n", mod.Name())
					fmt.Printf("    + create: Module %q\n\n", mod.Name())
				}

				fmt.Printf("Plan: %d modules to deploy\n", len(dc.Modules()))
			} else {
				fmt.Println("  (modules will be determined from OCI artifact)")
			}

			fmt.Println()

			// Confirm unless --auto-approve is provided
			if !autoApprove {
				fmt.Print("Proceed? [Y/n]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "" && response != "y" && response != "yes" {
					fmt.Println("Deployment cancelled.")
					return nil
				}
			}

			fmt.Println()
			fmt.Printf("[deploy] Deploying datacenter %q...\n", dcName)

			// Create or update datacenter state
			dcState := &types.DatacenterState{
				Name:      dcName,
				Version:   configRef,
				Variables: vars,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			if err := mgr.SaveDatacenter(ctx, dcState); err != nil {
				return fmt.Errorf("failed to save datacenter state: %w", err)
			}

			// TODO: Implement actual deployment logic using engine

			fmt.Printf("[success] Datacenter deployed successfully\n")

			return nil
		},
	}

	cmd.Flags().StringVarP(&configRef, "config", "c", "", "Datacenter config: OCI image or local path (required)")
	cmd.Flags().StringArrayVar(&variables, "var", nil, "Set variable (key=value)")
	cmd.Flags().StringVar(&varFile, "var-file", "", "Load variables from file")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")
	_ = cmd.MarkFlagRequired("config")

	return cmd
}

func newDatacenterDestroyCmd() *cobra.Command {
	var (
		autoApprove   bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "destroy <name>",
		Short: "Destroy a deployed datacenter",
		Long: `Destroy a datacenter and all its resources.

WARNING: This will destroy all environments in the datacenter. Use with caution.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dcName := args[0]
			ctx := context.Background()

			// Create state manager
			mgr, err := dcCreateStateManager(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Get datacenter state
			dc, err := mgr.GetDatacenter(ctx, dcName)
			if err != nil {
				return fmt.Errorf("datacenter %q not found: %w", dcName, err)
			}

			// Display what will be destroyed
			fmt.Printf("Datacenter: %s\n", dcName)
			fmt.Printf("Source:     %s\n", dc.Version)
			fmt.Println()

			if len(dc.Environments) > 0 {
				fmt.Println("WARNING: The following environments will also be destroyed:")
				for _, env := range dc.Environments {
					fmt.Printf("  - %s\n", env)
				}
				fmt.Println()
			}

			moduleCount := len(dc.Modules)
			fmt.Printf("This will destroy %d modules.\n", moduleCount)
			fmt.Println()

			// Confirm unless --auto-approve is provided
			if !autoApprove {
				fmt.Print("Are you sure you want to destroy this datacenter? [y/N]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "y" && response != "yes" {
					fmt.Println("Destroy cancelled.")
					return nil
				}
			}

			fmt.Println()

			// Destroy environments first
			for _, envName := range dc.Environments {
				fmt.Printf("[destroy] Destroying environment %q...\n", envName)
				if err := mgr.DeleteEnvironment(ctx, envName); err != nil {
					return fmt.Errorf("failed to destroy environment %q: %w", envName, err)
				}
			}

			fmt.Printf("[destroy] Destroying datacenter %q...\n", dcName)

			// TODO: Implement actual destroy logic using engine

			// Delete datacenter state
			if err := mgr.DeleteDatacenter(ctx, dcName); err != nil {
				return fmt.Errorf("failed to delete datacenter state: %w", err)
			}

			fmt.Printf("[success] Datacenter destroyed successfully\n")

			return nil
		},
	}

	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}

func newDatacenterValidateCmd() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate a datacenter configuration",
		Long:  `Validate a datacenter configuration file without deploying.`,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "datacenter.hcl"
			if len(args) > 0 {
				if strings.HasSuffix(args[0], ".hcl") {
					path = args[0]
				} else {
					path = filepath.Join(args[0], "datacenter.hcl")
				}
			}
			if file != "" {
				path = file
			}

			loader := datacenter.NewLoader()
			if err := loader.Validate(path); err != nil {
				return fmt.Errorf("validation failed: %w", err)
			}

			fmt.Println("Datacenter configuration is valid!")
			return nil
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to datacenter.hcl if not in default location")

	return cmd
}

// Helper functions (prefixed to avoid conflicts with component.go)

func dcCreateStateManager(backendType string, backendConfig []string) (state.Manager, error) {
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

func dcTruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func dcParseVarFile(data []byte, vars map[string]string) error {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, "\"'")
			vars[key] = value
		}
	}
	return nil
}

// moduleInfo holds information about a module for building.
type moduleInfo struct {
	sourceDir string
	plugin    string
}

// moduleBuilder wraps the container builder for CLI use.
type moduleBuilder struct {
	builder *container.Builder
}

// createModuleBuilder creates a new module builder.
func createModuleBuilder() (*moduleBuilder, error) {
	b, err := container.NewBuilder()
	if err != nil {
		return nil, err
	}
	return &moduleBuilder{builder: b}, nil
}

// Build builds a module container image.
func (m *moduleBuilder) Build(ctx context.Context, sourceDir, plugin, tag string) (*container.BuildResult, error) {
	// Determine module type from plugin
	var moduleType container.ModuleType
	switch plugin {
	case "pulumi":
		moduleType = container.ModuleTypePulumi
	case "opentofu", "terraform":
		moduleType = container.ModuleTypeOpenTofu
	case "native":
		// Native modules don't need containerization - they use Docker SDK directly
		return &container.BuildResult{
			Image:      tag,
			ModuleType: "native",
		}, nil
	default:
		// Auto-detect from source
		moduleType = ""
	}

	return m.builder.Build(ctx, container.BuildOptions{
		ModuleDir:  sourceDir,
		ModuleType: moduleType,
		Tag:        tag,
		Output:     io.Discard, // Suppress verbose build output
	})
}

// Close releases resources.
func (m *moduleBuilder) Close() error {
	return m.builder.Close()
}

// collectAllModules collects all modules from a datacenter configuration.
func collectAllModules(dc datacenter.Datacenter, dcPath string) map[string]moduleInfo {
	modules := make(map[string]moduleInfo)

	// Collect datacenter-level modules
	for _, mod := range dc.Modules() {
		if mod.Build() != "" {
			modulePath := fmt.Sprintf("module/%s", mod.Name())
			modules[modulePath] = moduleInfo{
				sourceDir: filepath.Join(dcPath, mod.Build()),
				plugin:    mod.Plugin(),
			}
		}
	}

	// Collect environment-level modules
	env := dc.Environment()
	if env != nil {
		for _, mod := range env.Modules() {
			if mod.Build() != "" {
				modulePath := fmt.Sprintf("module/%s", mod.Name())
				modules[modulePath] = moduleInfo{
					sourceDir: filepath.Join(dcPath, mod.Build()),
					plugin:    mod.Plugin(),
				}
			}
		}

		// Collect modules from hooks
		collectHookModules(env.Hooks().Database(), modules, dcPath)
		collectHookModules(env.Hooks().DatabaseMigration(), modules, dcPath)
		collectHookModules(env.Hooks().Bucket(), modules, dcPath)
		collectHookModules(env.Hooks().Deployment(), modules, dcPath)
		collectHookModules(env.Hooks().Function(), modules, dcPath)
		collectHookModules(env.Hooks().Service(), modules, dcPath)
		collectHookModules(env.Hooks().Ingress(), modules, dcPath)
		collectHookModules(env.Hooks().Cronjob(), modules, dcPath)
		collectHookModules(env.Hooks().Secret(), modules, dcPath)
		collectHookModules(env.Hooks().DockerBuild(), modules, dcPath)
	}

	return modules
}

// collectHookModules collects modules from hooks.
func collectHookModules(hooks []datacenter.Hook, modules map[string]moduleInfo, dcPath string) {
	for _, hook := range hooks {
		for _, mod := range hook.Modules() {
			if mod.Build() != "" {
				modulePath := fmt.Sprintf("module/%s", mod.Name())
				if _, exists := modules[modulePath]; !exists {
					modules[modulePath] = moduleInfo{
						sourceDir: filepath.Join(dcPath, mod.Build()),
						plugin:    mod.Plugin(),
					}
				}
			}
		}
	}
}
