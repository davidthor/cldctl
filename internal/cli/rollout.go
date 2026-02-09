package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/davidthor/cldctl/pkg/state/types"
	"github.com/spf13/cobra"
)

func newRolloutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollout",
		Short: "Manage progressive delivery rollouts",
		Long: `Commands for managing progressive delivery rollouts.

Progressive delivery allows deploying new versions of a component alongside
existing ones with weighted traffic splitting. Use these commands to control
the rollout lifecycle.

Workflow:
  1. Deploy a new instance:  cldctl deploy component my-app:v2 -e prod --instance canary --weight 10
  2. Check status:           cldctl rollout status my-app -e prod
  3. Increase traffic:       cldctl rollout set-weight my-app -e prod --instance canary --weight 50
  4. Promote (cleanup):      cldctl rollout promote my-app -e prod --instance canary
  
  At any point, rollback:   cldctl rollout rollback my-app -e prod --instance canary`,
	}

	cmd.AddCommand(newRolloutStatusCmd())
	cmd.AddCommand(newRolloutSetWeightCmd())
	cmd.AddCommand(newRolloutPromoteCmd())
	cmd.AddCommand(newRolloutRollbackCmd())

	return cmd
}

func newRolloutStatusCmd() *cobra.Command {
	var (
		environment   string
		datacenter    string
		outputFormat  string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "status <component>",
		Short: "Show rollout status for a component",
		Long: `Show the current rollout status including all instances, their weights,
sources, and health status.

Examples:
  cldctl rollout status my-app -e production
  cldctl rollout status my-app -e production -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			componentName := args[0]
			ctx := context.Background()

			dc, err := resolveDatacenter(datacenter)
			if err != nil {
				return err
			}

			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			envState, err := mgr.GetEnvironment(ctx, dc, environment)
			if err != nil {
				return fmt.Errorf("environment %q not found in datacenter %q: %w", environment, dc, err)
			}

			compState, ok := envState.Components[componentName]
			if !ok {
				return fmt.Errorf("component %q not found in environment %q", componentName, environment)
			}

			if outputFormat == "json" {
				return printRolloutStatusJSON(compState)
			}

			return printRolloutStatus(componentName, environment, dc, compState)
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Target datacenter")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "", "Output format (json)")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")
	_ = cmd.MarkFlagRequired("environment")

	return cmd
}

func printRolloutStatus(componentName, envName, dcName string, compState *types.ComponentState) error {
	fmt.Printf("Component:   %s\n", componentName)
	fmt.Printf("Environment: %s\n", envName)
	fmt.Printf("Datacenter:  %s\n", dcName)
	fmt.Printf("Status:      %s\n", compState.Status)
	fmt.Println()

			if len(compState.Instances) == 0 {
				// Single-instance mode
				fmt.Println("Mode: single-instance")
				fmt.Printf("Source: %s\n", compState.Source)
				fmt.Printf("Resources: %d\n", len(compState.Resources))
				return nil
			}

	fmt.Println("Mode: multi-instance (progressive delivery)")
	fmt.Println()

	// Sort instances for deterministic output
	var names []string
	for name := range compState.Instances {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Printf("%-15s %-8s %-40s %-12s %s\n", "INSTANCE", "WEIGHT", "SOURCE", "RESOURCES", "DEPLOYED")
	fmt.Printf("%-15s %-8s %-40s %-12s %s\n",
		strings.Repeat("-", 15), strings.Repeat("-", 8), strings.Repeat("-", 40),
		strings.Repeat("-", 12), strings.Repeat("-", 20))

	for _, name := range names {
		inst := compState.Instances[name]
		deployedAt := inst.DeployedAt.Format(time.RFC3339)
		if inst.DeployedAt.IsZero() {
			deployedAt = "unknown"
		}
		fmt.Printf("%-15s %-8s %-40s %-12d %s\n",
			inst.Name,
			fmt.Sprintf("%d%%", inst.Weight),
			inst.Source,
			len(inst.Resources),
			deployedAt,
		)
	}

	fmt.Println()
	fmt.Printf("Shared resources: %d\n", len(compState.Resources))

	return nil
}

func printRolloutStatusJSON(compState *types.ComponentState) error {
	data, err := json.MarshalIndent(compState, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

func newRolloutSetWeightCmd() *cobra.Command {
	var (
		environment   string
		datacenter    string
		instanceName  string
		weight        int
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "set-weight <component>",
		Short: "Update the traffic weight of an instance",
		Long: `Change the traffic weight of a named instance. This only updates routing
configuration -- no new infrastructure is created or destroyed.

Other instances' weights are automatically adjusted to sum to 100.

Examples:
  cldctl rollout set-weight my-app -e production --instance canary --weight 25
  cldctl rollout set-weight my-app -e production --instance canary --weight 100`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			componentName := args[0]
			ctx := context.Background()

			if instanceName == "" {
				return fmt.Errorf("--instance is required")
			}
			if weight < 0 || weight > 100 {
				return fmt.Errorf("--weight must be between 0 and 100")
			}

			dc, err := resolveDatacenter(datacenter)
			if err != nil {
				return err
			}

			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			envState, err := mgr.GetEnvironment(ctx, dc, environment)
			if err != nil {
				return fmt.Errorf("environment %q not found in datacenter %q: %w", environment, dc, err)
			}

			compState, ok := envState.Components[componentName]
			if !ok {
				return fmt.Errorf("component %q not found in environment %q", componentName, environment)
			}

			if len(compState.Instances) == 0 {
				return fmt.Errorf("component %q is in single-instance mode; deploy with --instance first", componentName)
			}

			inst, ok := compState.Instances[instanceName]
			if !ok {
				return fmt.Errorf("instance %q not found in component %q", instanceName, componentName)
			}

			// Calculate remaining weight for other instances
			oldWeight := inst.Weight
			remaining := 100 - weight
			totalOtherWeight := 0
			for name, other := range compState.Instances {
				if name != instanceName {
					totalOtherWeight += other.Weight
				}
			}

			// Adjust other instance weights proportionally
			for name, other := range compState.Instances {
				if name == instanceName {
					other.Weight = weight
				} else if totalOtherWeight > 0 {
					other.Weight = other.Weight * remaining / totalOtherWeight
				} else {
					other.Weight = remaining / (len(compState.Instances) - 1)
				}
			}

			compState.UpdatedAt = time.Now()
			envState.UpdatedAt = time.Now()

			if err := mgr.SaveEnvironment(ctx, dc, envState); err != nil {
				return fmt.Errorf("failed to save state: %w", err)
			}

			fmt.Printf("[success] Updated %s/%s weight: %d%% → %d%%\n", componentName, instanceName, oldWeight, weight)

			// Show all instances
			for name, other := range compState.Instances {
				marker := ""
				if name == instanceName {
					marker = " ← updated"
				}
				fmt.Printf("  %s: %d%%%s\n", name, other.Weight, marker)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Target datacenter")
	cmd.Flags().StringVar(&instanceName, "instance", "", "Instance name (required)")
	cmd.Flags().IntVar(&weight, "weight", -1, "New traffic weight (0-100, required)")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")
	_ = cmd.MarkFlagRequired("environment")
	_ = cmd.MarkFlagRequired("instance")
	_ = cmd.MarkFlagRequired("weight")

	return cmd
}

func newRolloutPromoteCmd() *cobra.Command {
	var (
		environment   string
		datacenter    string
		instanceName  string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "promote <component>",
		Short: "Promote an instance to be the sole version",
		Long: `Promote a named instance, destroying all other instances and collapsing
the component back to single-instance mode.

This is the final cleanup step after a successful progressive rollout.
The promoted instance's source becomes the component's canonical source,
its per-instance resources are moved to shared resources, and the Instances
map is removed from state.

Examples:
  cldctl rollout promote my-app -e production --instance canary`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			componentName := args[0]
			ctx := context.Background()

			if instanceName == "" {
				return fmt.Errorf("--instance is required")
			}

			dc, err := resolveDatacenter(datacenter)
			if err != nil {
				return err
			}

			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			envState, err := mgr.GetEnvironment(ctx, dc, environment)
			if err != nil {
				return fmt.Errorf("environment %q not found in datacenter %q: %w", environment, dc, err)
			}

			compState, ok := envState.Components[componentName]
			if !ok {
				return fmt.Errorf("component %q not found in environment %q", componentName, environment)
			}

			if len(compState.Instances) == 0 {
				return fmt.Errorf("component %q is already in single-instance mode", componentName)
			}

			promotedInst, ok := compState.Instances[instanceName]
			if !ok {
				return fmt.Errorf("instance %q not found in component %q", instanceName, componentName)
			}

			// Collapse to single-instance mode
			// 1. Move promoted instance's resources to component-level
			if compState.Resources == nil {
				compState.Resources = make(map[string]*types.ResourceState)
			}
			for key, res := range promotedInst.Resources {
				compState.Resources[key] = res
			}

			// 2. Update component source to promoted instance's source
			compState.Source = promotedInst.Source

			// 3. Remove all instances
			compState.Instances = nil

			compState.UpdatedAt = time.Now()
			envState.UpdatedAt = time.Now()

			if err := mgr.SaveEnvironment(ctx, dc, envState); err != nil {
				return fmt.Errorf("failed to save state: %w", err)
			}

			fmt.Printf("[success] Promoted instance %q of component %q\n", instanceName, componentName)
			fmt.Println("Component is now in single-instance mode.")
			fmt.Printf("Source: %s\n", compState.Source)

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Target datacenter")
	cmd.Flags().StringVar(&instanceName, "instance", "", "Instance to promote (required)")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")
	_ = cmd.MarkFlagRequired("environment")
	_ = cmd.MarkFlagRequired("instance")

	return cmd
}

func newRolloutRollbackCmd() *cobra.Command {
	var (
		environment   string
		datacenter    string
		instanceName  string
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "rollback <component>",
		Short: "Remove a named instance (rollback)",
		Long: `Remove a named instance, destroying its per-instance resources and
redistributing its traffic weight to remaining instances.

If only one instance remains after removal, the component collapses
back to single-instance mode.

Examples:
  cldctl rollout rollback my-app -e production --instance canary`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			componentName := args[0]
			ctx := context.Background()

			if instanceName == "" {
				return fmt.Errorf("--instance is required")
			}

			dc, err := resolveDatacenter(datacenter)
			if err != nil {
				return err
			}

			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			envState, err := mgr.GetEnvironment(ctx, dc, environment)
			if err != nil {
				return fmt.Errorf("environment %q not found in datacenter %q: %w", environment, dc, err)
			}

			compState, ok := envState.Components[componentName]
			if !ok {
				return fmt.Errorf("component %q not found in environment %q", componentName, environment)
			}

			if len(compState.Instances) == 0 {
				return fmt.Errorf("component %q is in single-instance mode; nothing to rollback", componentName)
			}

			if _, ok := compState.Instances[instanceName]; !ok {
				return fmt.Errorf("instance %q not found in component %q", instanceName, componentName)
			}

			// Remove the instance
			removedWeight := compState.Instances[instanceName].Weight
			delete(compState.Instances, instanceName)

			// Redistribute weight to remaining instances
			remaining := len(compState.Instances)
			if remaining > 0 {
				perInstance := removedWeight / remaining
				extra := removedWeight % remaining
				i := 0
				for _, inst := range compState.Instances {
					inst.Weight += perInstance
					if i < extra {
						inst.Weight++
					}
					i++
				}
			}

			// If only one instance remains, collapse to single-instance mode
			if remaining == 1 {
				for _, lastInst := range compState.Instances {
					lastInst.Weight = 100
					// Move resources to component level
					if compState.Resources == nil {
						compState.Resources = make(map[string]*types.ResourceState)
					}
					for key, res := range lastInst.Resources {
						compState.Resources[key] = res
					}
					compState.Source = lastInst.Source
				}
				compState.Instances = nil
				fmt.Printf("[success] Rolled back instance %q; component collapsed to single-instance mode\n", instanceName)
			} else {
				fmt.Printf("[success] Rolled back instance %q of component %q\n", instanceName, componentName)
				for name, inst := range compState.Instances {
					fmt.Printf("  %s: %d%%\n", name, inst.Weight)
				}
			}

			compState.UpdatedAt = time.Now()
			envState.UpdatedAt = time.Now()

			if err := mgr.SaveEnvironment(ctx, dc, envState); err != nil {
				return fmt.Errorf("failed to save state: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Target datacenter")
	cmd.Flags().StringVar(&instanceName, "instance", "", "Instance to remove (required)")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")
	_ = cmd.MarkFlagRequired("environment")
	_ = cmd.MarkFlagRequired("instance")

	return cmd
}
