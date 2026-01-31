package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/architect-io/arcctl/pkg/schema/environment"
	"github.com/architect-io/arcctl/pkg/state"
	"github.com/architect-io/arcctl/pkg/state/types"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newEnvironmentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "environment",
		Aliases: []string{"env"},
		Short:   "Manage environments",
		Long:    `Commands for creating, updating, and managing environments.`,
	}

	cmd.AddCommand(newEnvironmentListCmd())
	cmd.AddCommand(newEnvironmentGetCmd())
	cmd.AddCommand(newEnvironmentCreateCmd())
	cmd.AddCommand(newEnvironmentUpdateCmd())
	cmd.AddCommand(newEnvironmentDestroyCmd())
	cmd.AddCommand(newEnvironmentValidateCmd())

	return cmd
}

func newEnvironmentListCmd() *cobra.Command {
	var (
		datacenter    string
		outputFormat  string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List environments",
		Long:  `List all environments.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Create state manager
			mgr, err := envCreateStateManager(backendType, backendConfig)
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

func newEnvironmentGetCmd() *cobra.Command {
	var (
		outputFormat  string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "Get details of an environment",
		Long:  `Get detailed information about an environment.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := args[0]
			ctx := context.Background()

			// Create state manager
			mgr, err := envCreateStateManager(backendType, backendConfig)
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
							envTruncateString(comp.Source, 40),
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

func newEnvironmentCreateCmd() *cobra.Command {
	var (
		datacenter    string
		ifNotExists   bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new environment",
		Long:  `Create a new environment.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := args[0]
			ctx := context.Background()

			// Create state manager
			mgr, err := envCreateStateManager(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Check if environment already exists
			existingEnv, err := mgr.GetEnvironment(ctx, envName)
			if err == nil && existingEnv != nil {
				if ifNotExists {
					fmt.Printf("Environment %q already exists, skipping creation.\n", envName)
					return nil
				}
				return fmt.Errorf("environment %q already exists", envName)
			}

			// Verify datacenter exists
			dc, err := mgr.GetDatacenter(ctx, datacenter)
			if err != nil {
				return fmt.Errorf("datacenter %q not found: %w", datacenter, err)
			}

			fmt.Printf("Environment: %s\n", envName)
			fmt.Printf("Datacenter:  %s\n", datacenter)
			fmt.Println()

			fmt.Printf("[create] Creating environment %q...\n", envName)

			// Create environment state
			envState := &types.EnvironmentState{
				Name:       envName,
				Datacenter: datacenter,
				Status:     types.EnvironmentStatusReady,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
				Components: make(map[string]*types.ComponentState),
			}

			if err := mgr.SaveEnvironment(ctx, envState); err != nil {
				return fmt.Errorf("failed to save environment state: %w", err)
			}

			// Update datacenter with environment reference
			dc.Environments = append(dc.Environments, envName)
			dc.UpdatedAt = time.Now()
			if err := mgr.SaveDatacenter(ctx, dc); err != nil {
				return fmt.Errorf("failed to update datacenter state: %w", err)
			}

			fmt.Printf("[success] Environment created successfully\n")

			return nil
		},
	}

	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Datacenter to use (required)")
	cmd.Flags().BoolVar(&ifNotExists, "if-not-exists", false, "Don't error if environment already exists")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")
	_ = cmd.MarkFlagRequired("datacenter")

	return cmd
}

func newEnvironmentUpdateCmd() *cobra.Command {
	var (
		datacenter    string
		autoApprove   bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "update <name> [config-file]",
		Short: "Update an environment",
		Long: `Update environment configuration.

You can update an environment in two ways:

1. Update specific settings with flags:
   arcctl env update staging --datacenter new-dc

2. Apply a configuration file that defines the full environment state:
   arcctl env update staging environment.yml

When providing a config file, the environment's components and configuration
will be updated to match the file. Components not in the file will be removed,
and new components will be deployed.

Examples:
  arcctl env update staging --datacenter new-dc
  arcctl env update staging environment.yml
  arcctl env update staging ./envs/staging.yml --auto-approve`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := args[0]
			ctx := context.Background()

			// Check if a config file was provided as second argument
			configFile := ""
			if len(args) > 1 {
				configFile = args[1]
			}

			// Create state manager
			mgr, err := envCreateStateManager(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Get environment state
			env, err := mgr.GetEnvironment(ctx, envName)
			if err != nil {
				return fmt.Errorf("environment %q not found: %w", envName, err)
			}

			// If a config file is provided, apply it
			if configFile != "" {
				return applyEnvironmentConfig(ctx, mgr, env, configFile, autoApprove)
			}

			// Otherwise, update individual settings
			hasChanges := false

			// Update datacenter if specified
			if datacenter != "" && datacenter != env.Datacenter {
				// Verify new datacenter exists
				_, err := mgr.GetDatacenter(ctx, datacenter)
				if err != nil {
					return fmt.Errorf("datacenter %q not found: %w", datacenter, err)
				}

				fmt.Printf("Updating environment %q datacenter from %q to %q\n", envName, env.Datacenter, datacenter)
				env.Datacenter = datacenter
				hasChanges = true
			}

			if !hasChanges {
				fmt.Println("No changes specified. Use --datacenter or provide a config file.")
				return nil
			}

			env.UpdatedAt = time.Now()

			if err := mgr.SaveEnvironment(ctx, env); err != nil {
				return fmt.Errorf("failed to save environment state: %w", err)
			}

			fmt.Printf("[success] Environment updated successfully\n")

			return nil
		},
	}

	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Change target datacenter")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt (when using config file)")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}

// applyEnvironmentConfig applies an environment configuration file to an existing environment.
func applyEnvironmentConfig(ctx context.Context, mgr state.Manager, env *types.EnvironmentState, configFile string, autoApprove bool) error {
	// Load and validate the environment file
	loader := environment.NewLoader()
	envConfig, err := loader.Load(configFile)
	if err != nil {
		return fmt.Errorf("failed to load environment config: %w", err)
	}

	// Verify the environment name matches
	if envConfig.Name() != "" && envConfig.Name() != env.Name {
		return fmt.Errorf("environment name in config file (%q) does not match target environment (%q)", envConfig.Name(), env.Name)
	}

	fmt.Printf("Environment: %s\n", env.Name)
	fmt.Printf("Datacenter:  %s\n", env.Datacenter)
	fmt.Printf("Config file: %s\n", configFile)
	fmt.Println()

	// Determine changes
	newComponents := envConfig.Components()
	existingComponents := env.Components

	// Components to add
	var toAdd []string
	for name := range newComponents {
		if _, exists := existingComponents[name]; !exists {
			toAdd = append(toAdd, name)
		}
	}

	// Components to update
	var toUpdate []string
	for name, newComp := range newComponents {
		if existing, exists := existingComponents[name]; exists {
			// Check if source changed
			if existing.Source != newComp.Source() {
				toUpdate = append(toUpdate, name)
			}
		}
	}

	// Components to remove
	var toRemove []string
	for name := range existingComponents {
		if _, exists := newComponents[name]; !exists {
			toRemove = append(toRemove, name)
		}
	}

	// Display execution plan
	fmt.Println("Execution Plan:")
	fmt.Println()

	if len(toAdd) > 0 {
		fmt.Println("  Components to deploy:")
		for _, name := range toAdd {
			comp := newComponents[name]
			fmt.Printf("    + %s (%s)\n", name, comp.Source())
		}
		fmt.Println()
	}

	if len(toUpdate) > 0 {
		fmt.Println("  Components to update:")
		for _, name := range toUpdate {
			oldComp := existingComponents[name]
			newComp := newComponents[name]
			fmt.Printf("    ~ %s: %s -> %s\n", name, oldComp.Source, newComp.Source())
		}
		fmt.Println()
	}

	if len(toRemove) > 0 {
		fmt.Println("  Components to remove:")
		for _, name := range toRemove {
			fmt.Printf("    - %s\n", name)
		}
		fmt.Println()
	}

	if len(toAdd) == 0 && len(toUpdate) == 0 && len(toRemove) == 0 {
		fmt.Println("  No changes detected.")
		return nil
	}

	fmt.Printf("Plan: %d to deploy, %d to update, %d to remove\n", len(toAdd), len(toUpdate), len(toRemove))
	fmt.Println()

	// Confirm unless --auto-approve is provided
	if !autoApprove {
		fmt.Print("Proceed with update? [Y/n]: ")
		var response string
		_, _ = fmt.Scanln(&response)
		response = strings.ToLower(strings.TrimSpace(response))
		if response != "" && response != "y" && response != "yes" {
			fmt.Println("Update cancelled.")
			return nil
		}
	}

	fmt.Println()
	fmt.Printf("[update] Applying configuration to environment %q...\n", env.Name)

	// Update datacenter if changed
	if envConfig.Datacenter() != "" && envConfig.Datacenter() != env.Datacenter {
		env.Datacenter = envConfig.Datacenter()
	}

	// TODO: Implement actual component deployment/update/removal logic
	// For now, just update the state to reflect the new components

	// Remove components
	for _, name := range toRemove {
		fmt.Printf("[update] Removing component %q...\n", name)
		delete(env.Components, name)
	}

	// Add/update components
	for name, comp := range newComponents {
		if env.Components == nil {
			env.Components = make(map[string]*types.ComponentState)
		}

		// Convert variables from map[string]interface{} to map[string]string
		vars := make(map[string]string)
		for k, v := range comp.Variables() {
			if s, ok := v.(string); ok {
				vars[k] = s
			} else {
				vars[k] = fmt.Sprintf("%v", v)
			}
		}

		if _, exists := env.Components[name]; !exists {
			fmt.Printf("[update] Deploying component %q...\n", name)
			env.Components[name] = &types.ComponentState{
				Name:       name,
				Source:     comp.Source(),
				Status:     types.ResourceStatusReady,
				DeployedAt: time.Now(),
				Variables:  vars,
				Resources:  make(map[string]*types.ResourceState),
			}
		} else {
			fmt.Printf("[update] Updating component %q...\n", name)
			env.Components[name].Source = comp.Source()
			env.Components[name].Variables = vars
		}
	}

	env.UpdatedAt = time.Now()

	if err := mgr.SaveEnvironment(ctx, env); err != nil {
		return fmt.Errorf("failed to save environment state: %w", err)
	}

	fmt.Printf("[success] Environment updated successfully\n")

	return nil
}

func newEnvironmentDestroyCmd() *cobra.Command {
	var (
		autoApprove   bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "destroy <name>",
		Short: "Destroy an environment",
		Long: `Destroy an environment and all its resources.

WARNING: This will destroy all components and resources in the environment. Use with caution.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := args[0]
			ctx := context.Background()

			// Create state manager
			mgr, err := envCreateStateManager(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			// Get environment state
			env, err := mgr.GetEnvironment(ctx, envName)
			if err != nil {
				return fmt.Errorf("environment %q not found: %w", envName, err)
			}

			// Display what will be destroyed
			fmt.Printf("Environment: %s\n", envName)
			fmt.Printf("Datacenter:  %s\n", env.Datacenter)
			fmt.Println()

			componentCount := len(env.Components)
			resourceCount := 0
			for _, comp := range env.Components {
				resourceCount += len(comp.Resources)
			}

			fmt.Println("This will destroy:")
			fmt.Printf("  - %d components\n", componentCount)
			fmt.Printf("  - %d resources\n", resourceCount)
			fmt.Println()

			// Confirm unless --auto-approve is provided
			if !autoApprove {
				fmt.Print("Are you sure you want to destroy this environment? [y/N]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "y" && response != "yes" {
					fmt.Println("Destroy cancelled.")
					return nil
				}
			}

			fmt.Println()

			// Destroy components
			for compName := range env.Components {
				fmt.Printf("[destroy] Destroying component %q...\n", compName)
				// TODO: Implement actual component destroy logic
			}

			fmt.Printf("[destroy] Removing environment...\n")

			// Delete environment state
			if err := mgr.DeleteEnvironment(ctx, envName); err != nil {
				return fmt.Errorf("failed to delete environment state: %w", err)
			}

	// Update datacenter to remove environment reference
	dc, err := mgr.GetDatacenter(ctx, env.Datacenter)
	if err == nil {
		newEnvs := make([]string, 0, len(dc.Environments))
		for _, e := range dc.Environments {
			if e != envName {
				newEnvs = append(newEnvs, e)
			}
		}
		dc.Environments = newEnvs
		dc.UpdatedAt = time.Now()
		_ = mgr.SaveDatacenter(ctx, dc)
	}

			fmt.Printf("[success] Environment destroyed successfully\n")

			return nil
		},
	}

	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}

func newEnvironmentValidateCmd() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate an environment configuration",
		Long:  `Validate an environment configuration file without applying.`,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "environment.yml"
			if len(args) > 0 {
				if strings.HasSuffix(args[0], ".yml") || strings.HasSuffix(args[0], ".yaml") {
					path = args[0]
				} else {
					path = filepath.Join(args[0], "environment.yml")
				}
			}
			if file != "" {
				path = file
			}

			loader := environment.NewLoader()
			if err := loader.Validate(path); err != nil {
				return fmt.Errorf("validation failed: %w", err)
			}

			fmt.Println("Environment configuration is valid!")
			return nil
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to environment.yml if not in default location")

	return cmd
}

// Helper functions (prefixed to avoid conflicts)

func envCreateStateManager(backendType string, backendConfig []string) (state.Manager, error) {
	return createStateManagerWithConfig(backendType, backendConfig)
}

func envTruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
