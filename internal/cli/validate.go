package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/architect-io/arcctl/pkg/schema/component"
	"github.com/architect-io/arcctl/pkg/schema/datacenter"
	"github.com/architect-io/arcctl/pkg/schema/environment"
	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate configurations",
		Long:  `Commands for validating component, datacenter, and environment configurations.`,
	}

	cmd.AddCommand(newValidateComponentCmd())
	cmd.AddCommand(newValidateDatacenterCmd())
	cmd.AddCommand(newValidateEnvironmentCmd())

	return cmd
}

func newValidateComponentCmd() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:     "component [path]",
		Aliases: []string{"comp"},
		Short:   "Validate a component configuration",
		Long: `Validate a component configuration file without deploying.

Examples:
  arcctl validate component
  arcctl validate component ./my-app
  arcctl validate component -f custom-component.yml`,
		Args: cobra.MaximumNArgs(1),
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

func newValidateDatacenterCmd() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:     "datacenter [path]",
		Aliases: []string{"dc"},
		Short:   "Validate a datacenter configuration",
		Long: `Validate a datacenter configuration file without deploying.

Examples:
  arcctl validate datacenter
  arcctl validate datacenter ./my-datacenter
  arcctl validate datacenter -f custom-datacenter.hcl`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var dcPath string
			if file != "" {
				dcPath = file
			} else if len(args) > 0 {
				dcPath = args[0]
			} else {
				dcPath = "."
			}

			// Check if path is a file or directory
			info, err := os.Stat(dcPath)
			if err != nil {
				return fmt.Errorf("failed to access path: %w", err)
			}

			dcFile := dcPath
			if info.IsDir() {
				// Look for datacenter file in directory
				// Try datacenter.dc first, then datacenter.hcl
				dcFile = filepath.Join(dcPath, "datacenter.dc")
				if _, err := os.Stat(dcFile); os.IsNotExist(err) {
					dcFile = filepath.Join(dcPath, "datacenter.hcl")
				}
			}

			loader := datacenter.NewLoader()
			if err := loader.Validate(dcFile); err != nil {
				return fmt.Errorf("validation failed: %w", err)
			}

			fmt.Println("Datacenter configuration is valid!")
			return nil
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to datacenter.hcl if not in default location")

	return cmd
}

func newValidateEnvironmentCmd() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:     "environment [path]",
		Aliases: []string{"env"},
		Short:   "Validate an environment configuration",
		Long: `Validate an environment configuration file without applying.

Examples:
  arcctl validate environment
  arcctl validate environment ./envs/staging.yml
  arcctl validate environment -f custom-environment.yml`,
		Args: cobra.MaximumNArgs(1),
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
