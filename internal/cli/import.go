package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/davidthor/cldctl/pkg/engine"
	"github.com/davidthor/cldctl/pkg/engine/importmap"
	"github.com/davidthor/cldctl/pkg/iac"
	"github.com/spf13/cobra"
)

// Ensure time is used (imported for Duration formatting in import summary)
var _ = time.Millisecond

func newImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import existing cloud resources into cldctl state",
		Long: `Commands for importing existing cloud resources into cldctl state.

Import allows you to adopt already-running infrastructure so that future
deploys manage it alongside other resources. This is similar to
'terraform import' — the operator has existing infrastructure and wants
cldctl to manage it.

Prerequisites:
  - A datacenter must be deployed (for hook matching)
  - An environment must exist (for environment-scoped imports)
  - Component artifacts must be available (for component/environment imports)

Import does NOT provision new resources — it records existing cloud
resource state in cldctl's state backend.`,
	}

	cmd.AddCommand(newImportResourceCmd())
	cmd.AddCommand(newImportComponentCmd())
	cmd.AddCommand(newImportEnvironmentCmd())
	cmd.AddCommand(newImportDatacenterCmd())

	return cmd
}

func newImportResourceCmd() *cobra.Command {
	var (
		datacenter    string
		environment   string
		mapFlags      []string
		autoApprove   bool
		force         bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "resource <component> <resource-key>",
		Short: "Import a single resource into cldctl state",
		Long: `Import an existing cloud resource into cldctl state by providing
the mapping from IaC-module-internal resource addresses to real cloud
resource IDs.

The resource-key uses the type-qualified format: database.main,
deployment.api, service.api, etc.

The --map flag provides the mapping from IaC resource addresses to
cloud resource IDs. Multiple --map flags can be provided for modules
that manage multiple IaC resources.

Examples:
  # Simple single-resource module
  cldctl import resource my-app deployment.api \
    -d prod-dc -e production \
    --map "aws_ecs_service.main=arn:aws:ecs:us-east-1:123:service/my-cluster/api"

  # Multi-resource module (e.g., database with security group)
  cldctl import resource my-app database.main \
    -d prod-dc -e production \
    --map "aws_db_instance.main=mydb-instance-123" \
    --map "aws_security_group.db=sg-abc456"`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			componentName := args[0]
			resourceKey := args[1]
			ctx := context.Background()

			// Resolve datacenter
			dc, err := resolveDatacenter(datacenter)
			if err != nil {
				return err
			}

			// Parse --map flags
			mappings, err := importmap.ParseMapFlags(mapFlags)
			if err != nil {
				return err
			}
			if len(mappings) == 0 {
				return fmt.Errorf("at least one --map flag is required")
			}

			// Convert to IaC mappings
			iacMappings := make([]iac.ImportMapping, 0, len(mappings))
			for _, m := range mappings {
				iacMappings = append(iacMappings, iac.ImportMapping{
					Address: m.Address,
					ID:      m.ID,
				})
			}

			// Create state manager and engine
			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}
			eng := createEngine(mgr)

			// Show preview
			fmt.Printf("Import plan:\n\n")
			fmt.Printf("  Component:   %s\n", componentName)
			fmt.Printf("  Resource:    %s\n", resourceKey)
			fmt.Printf("  Environment: %s\n", environment)
			fmt.Printf("  Datacenter:  %s\n", dc)
			fmt.Printf("\n  Mappings:\n")
			for _, m := range mappings {
				fmt.Printf("    %s = %s\n", m.Address, m.ID)
			}
			fmt.Println()

			// Confirm
			if !autoApprove && isInteractive() {
				fmt.Print("Proceed with import? [y/N]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "y" && response != "yes" {
					fmt.Println("Import cancelled.")
					return nil
				}
				fmt.Println()
			}

			// Execute import
			result, err := eng.ImportResource(ctx, engine.ImportResourceOptions{
				Datacenter:  dc,
				Environment: environment,
				Component:   componentName,
				ResourceKey: resourceKey,
				Mappings:    iacMappings,
				Output:      os.Stdout,
				Force:       force,
			})
			if err != nil {
				return fmt.Errorf("import failed: %w", err)
			}

			if !result.Success {
				return fmt.Errorf("import completed with errors")
			}

			fmt.Println()
			fmt.Printf("[success] Imported %s into %s/%s\n", resourceKey, environment, componentName)

			// Show verification results
			if len(result.Drifts) > 0 {
				fmt.Printf("\n  Warning: %d drift(s) detected after import. Run 'cldctl deploy' to reconcile.\n", len(result.Drifts))
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Target datacenter (uses default if not set)")
	cmd.Flags().StringArrayVar(&mapFlags, "map", nil, "Resource mapping (address=cloud-id)")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing resource state")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	_ = cmd.MarkFlagRequired("environment")

	return cmd
}

func newImportComponentCmd() *cobra.Command {
	var (
		datacenter    string
		environment   string
		source        string
		mappingFile   string
		variables     []string
		autoApprove   bool
		force         bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "component <component-name>",
		Short: "Import all resources for a component",
		Long: `Import all resources for a component using a mapping file that describes
how each resource maps to existing cloud infrastructure.

The --source flag specifies the component artifact (OCI reference or local
path), identical to what 'deploy component' uses.

The --mapping flag points to a YAML file describing resource-to-cloud-ID
mappings:

  resources:
    database.main:
      - address: aws_db_instance.main
        id: "mydb-instance-123"
      - address: aws_security_group.db
        id: "sg-abc456"
    deployment.api:
      - address: aws_ecs_service.main
        id: "arn:aws:ecs:..."

Examples:
  cldctl import component my-app \
    -d prod-dc -e production \
    --source ghcr.io/myorg/app:v1.0.0 \
    --mapping import-my-app.yml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			componentName := args[0]
			ctx := context.Background()

			// Resolve datacenter
			dc, err := resolveDatacenter(datacenter)
			if err != nil {
				return err
			}

			// Parse mapping file
			mapping, err := importmap.ParseComponentMapping(mappingFile)
			if err != nil {
				return fmt.Errorf("failed to parse mapping file: %w", err)
			}

			// Parse variables
			vars := make(map[string]interface{})
			for _, v := range variables {
				parts := strings.SplitN(v, "=", 2)
				if len(parts) == 2 {
					vars[parts[0]] = parts[1]
				}
			}

			// Create state manager and engine
			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}
			eng := createEngine(mgr)

			// Show preview
			fmt.Printf("Import plan for component %q:\n\n", componentName)
			fmt.Printf("  Source:      %s\n", source)
			fmt.Printf("  Environment: %s\n", environment)
			fmt.Printf("  Datacenter:  %s\n", dc)
			fmt.Printf("\n  Resources:\n")
			for key, mappings := range mapping.Resources {
				fmt.Printf("    + %s (import %d IaC resource(s))\n", key, len(mappings))
			}
			fmt.Printf("\n  %d resource(s) to import.\n\n", len(mapping.Resources))

			// Confirm
			if !autoApprove && isInteractive() {
				fmt.Print("Proceed with import? [y/N]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "y" && response != "yes" {
					fmt.Println("Import cancelled.")
					return nil
				}
				fmt.Println()
			}

			// Execute import
			result, err := eng.ImportComponent(ctx, engine.ImportComponentOptions{
				Datacenter:  dc,
				Environment: environment,
				Component:   componentName,
				Source:      source,
				Variables:   vars,
				Mapping:     mapping,
				Output:      os.Stdout,
				Force:       force,
				AutoApprove: autoApprove,
			})
			if err != nil {
				return fmt.Errorf("import failed: %w", err)
			}

			// Print summary
			fmt.Println()
			successCount := 0
			failCount := 0
			driftCount := 0
			for _, res := range result.Resources {
				if res.Success {
					successCount++
					driftCount += len(res.Drifts)
				} else {
					failCount++
				}
			}

			fmt.Printf("Import complete: %d imported, %d failed", successCount, failCount)
			if driftCount > 0 {
				fmt.Printf(", %d with drift", driftCount)
			}
			fmt.Printf(" (%s)\n", result.Duration.Round(100*time.Millisecond))

			if !result.Success {
				return fmt.Errorf("import completed with %d error(s)", failCount)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&environment, "environment", "e", "", "Target environment (required)")
	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Target datacenter (uses default if not set)")
	cmd.Flags().StringVar(&source, "source", "", "Component artifact reference (required)")
	cmd.Flags().StringVar(&mappingFile, "mapping", "", "Path to mapping YAML file (required)")
	cmd.Flags().StringArrayVar(&variables, "var", nil, "Set variable (key=value)")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing state")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	_ = cmd.MarkFlagRequired("environment")
	_ = cmd.MarkFlagRequired("source")
	_ = cmd.MarkFlagRequired("mapping")

	return cmd
}

func newImportEnvironmentCmd() *cobra.Command {
	var (
		datacenter    string
		mappingFile   string
		autoApprove   bool
		force         bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "environment <environment-name>",
		Short: "Import multiple components into an environment",
		Long: `Import multiple components and their resources into an environment
using a mapping file that describes all components, their sources, and
resource-to-cloud-ID mappings.

The mapping file format:

  components:
    my-app:
      source: ghcr.io/myorg/app:v1.0.0
      variables:
        log_level: info
      resources:
        database.main:
          - address: aws_db_instance.main
            id: "mydb-instance-123"
        deployment.api:
          - address: aws_ecs_service.main
            id: "arn:aws:ecs:..."
    my-api:
      source: ghcr.io/myorg/api:v2.0.0
      resources:
        deployment.worker:
          - address: aws_ecs_service.main
            id: "arn:aws:ecs:..."

If the environment does not exist, it will be created automatically.

Examples:
  cldctl import environment production \
    -d prod-dc \
    --mapping import-production.yml`,
		Aliases: []string{"env"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			envName := args[0]
			ctx := context.Background()

			// Resolve datacenter
			dc, err := resolveDatacenter(datacenter)
			if err != nil {
				return err
			}

			// Parse mapping file
			mapping, err := importmap.ParseEnvironmentMapping(mappingFile)
			if err != nil {
				return fmt.Errorf("failed to parse mapping file: %w", err)
			}

			// Create state manager and engine
			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}
			eng := createEngine(mgr)

			// Show preview
			fmt.Printf("Import plan for environment %q:\n\n", envName)
			totalResources := 0
			for compName, comp := range mapping.Components {
				resCount := len(comp.Resources)
				totalResources += resCount
				fmt.Printf("  Component: %s (%s)\n", compName, comp.Source)
				for key, mappings := range comp.Resources {
					fmt.Printf("    + %s (import %d IaC resource(s))\n", key, len(mappings))
				}
				fmt.Println()
			}
			fmt.Printf("  %d component(s), %d resource(s) to import.\n\n", len(mapping.Components), totalResources)

			// Confirm
			if !autoApprove && isInteractive() {
				fmt.Print("Proceed with import? [y/N]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "y" && response != "yes" {
					fmt.Println("Import cancelled.")
					return nil
				}
				fmt.Println()
			}

			// Execute import
			result, err := eng.ImportEnvironment(ctx, engine.ImportEnvironmentOptions{
				Datacenter:  dc,
				Environment: envName,
				Mapping:     mapping,
				Output:      os.Stdout,
				Force:       force,
				AutoApprove: autoApprove,
			})
			if err != nil {
				return fmt.Errorf("import failed: %w", err)
			}

			// Print summary
			fmt.Println()
			successComps := 0
			failComps := 0
			for _, comp := range result.Components {
				if comp.Success {
					successComps++
				} else {
					failComps++
				}
			}

			fmt.Printf("Import complete: %d component(s) imported, %d failed (%s)\n",
				successComps, failComps, result.Duration.Round(100*time.Millisecond))

			if !result.Success {
				return fmt.Errorf("import completed with errors")
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&datacenter, "datacenter", "d", "", "Target datacenter (uses default if not set)")
	cmd.Flags().StringVar(&mappingFile, "mapping", "", "Path to environment mapping YAML file (required)")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing state")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	_ = cmd.MarkFlagRequired("mapping")

	return cmd
}

func newImportDatacenterCmd() *cobra.Command {
	var (
		moduleName    string
		mapFlags      []string
		autoApprove   bool
		force         bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "datacenter <datacenter-name>",
		Short: "Import existing infrastructure into a datacenter module",
		Long: `Import existing cloud infrastructure into a datacenter-level module's
state. This is used for root-level modules defined in the datacenter
configuration (e.g., a shared VPC, networking layer).

The --module flag specifies which datacenter module to import into.
The --map flags provide resource address to cloud ID mappings.

Examples:
  cldctl import datacenter prod-dc \
    --module vpc \
    --map "aws_vpc.main=vpc-0abc123" \
    --map "aws_subnet.public[0]=subnet-xyz789"`,
		Aliases: []string{"dc"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			dcName := args[0]
			ctx := context.Background()

			// Parse --map flags
			mappings, err := importmap.ParseMapFlags(mapFlags)
			if err != nil {
				return err
			}
			if len(mappings) == 0 {
				return fmt.Errorf("at least one --map flag is required")
			}

			// Convert to IaC mappings
			iacMappings := make([]iac.ImportMapping, 0, len(mappings))
			for _, m := range mappings {
				iacMappings = append(iacMappings, iac.ImportMapping{
					Address: m.Address,
					ID:      m.ID,
				})
			}

			// Create state manager and engine
			mgr, err := createStateManagerWithConfig(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}
			eng := createEngine(mgr)

			// Show preview
			fmt.Printf("Import plan for datacenter module:\n\n")
			fmt.Printf("  Datacenter: %s\n", dcName)
			fmt.Printf("  Module:     %s\n", moduleName)
			fmt.Printf("\n  Mappings:\n")
			for _, m := range mappings {
				fmt.Printf("    %s = %s\n", m.Address, m.ID)
			}
			fmt.Println()

			// Confirm
			if !autoApprove && isInteractive() {
				fmt.Print("Proceed with import? [y/N]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "y" && response != "yes" {
					fmt.Println("Import cancelled.")
					return nil
				}
				fmt.Println()
			}

			// Execute import
			result, err := eng.ImportDatacenterModule(ctx, engine.ImportDatacenterModuleOptions{
				Datacenter: dcName,
				Module:     moduleName,
				Mappings:   iacMappings,
				Output:     os.Stdout,
				Force:      force,
			})
			if err != nil {
				return fmt.Errorf("import failed: %w", err)
			}

			if !result.Success {
				return fmt.Errorf("import completed with errors")
			}

			fmt.Println()
			fmt.Printf("[success] Imported %d resource(s) into datacenter module %q\n",
				len(result.ImportedResources), moduleName)

			return nil
		},
	}

	cmd.Flags().StringVar(&moduleName, "module", "", "Datacenter module name (required)")
	cmd.Flags().StringArrayVar(&mapFlags, "map", nil, "Resource mapping (address=cloud-id)")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing module state")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	_ = cmd.MarkFlagRequired("module")

	return cmd
}
