package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/davidthor/cldctl/pkg/errors"
	"github.com/davidthor/cldctl/pkg/schema/component"
	"github.com/davidthor/cldctl/pkg/schema/datacenter"
	"github.com/davidthor/cldctl/pkg/schema/environment"
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
		Aliases: []string{"comp", "comps", "components"},
		Short:   "Validate a component configuration",
		Long: `Validate a component configuration file without deploying.

Examples:
  cldctl validate component
  cldctl validate component ./my-app
  cldctl validate component -f custom-component.yml`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "cloud.component.yml"
			if len(args) > 0 {
				if strings.HasSuffix(args[0], ".yml") || strings.HasSuffix(args[0], ".yaml") {
					path = args[0]
				} else {
					path = filepath.Join(args[0], "cloud.component.yml")
				}
			}
			if file != "" {
				path = file
			}

			loader := component.NewLoader()
			if err := loader.Validate(path); err != nil {
				return formatValidationError(err)
			}

			fmt.Println("Component configuration is valid!")
			return nil
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to cloud.component.yml if not in default location")

	return cmd
}

// formatValidationError extracts and displays validation error details
func formatValidationError(err error) error {
	// Try to extract cldctl error with details
	var arcErr *errors.Error
	if e, ok := err.(*errors.Error); ok {
		arcErr = e
	} else {
		// Check wrapped errors
		unwrapped := err
		for unwrapped != nil {
			if e, ok := unwrapped.(*errors.Error); ok {
				arcErr = e
				break
			}
			if u, ok := unwrapped.(interface{ Unwrap() error }); ok {
				unwrapped = u.Unwrap()
			} else {
				break
			}
		}
	}

	if arcErr != nil && arcErr.Code == errors.ErrCodeValidation {
		// Extract validation error details
		if errList, ok := arcErr.Details["errors"].([]string); ok && len(errList) > 0 {
			var sb strings.Builder
			sb.WriteString("validation failed\n")
			sb.WriteString("\nValidation errors:\n")
			for _, e := range errList {
				sb.WriteString(fmt.Sprintf("  - %s\n", e))
			}
			return fmt.Errorf("%s", sb.String())
		}
	}

	return fmt.Errorf("validation failed: %w", err)
}

func newValidateDatacenterCmd() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:     "datacenter [path]",
		Aliases: []string{"dc", "dcs", "datacenters"},
		Short:   "Validate a datacenter configuration",
		Long: `Validate a datacenter configuration file without deploying.

Examples:
  cldctl validate datacenter
  cldctl validate datacenter ./my-datacenter
  cldctl validate datacenter -f custom-datacenter.hcl`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
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
				return formatValidationError(err)
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
		Aliases: []string{"env", "envs", "environments"},
		Short:   "Validate an environment configuration",
		Long: `Validate an environment configuration file without applying.

Examples:
  cldctl validate environment
  cldctl validate environment ./envs/staging.yml
  cldctl validate environment -f custom-environment.yml`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
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
				return formatValidationError(err)
			}

			fmt.Println("Environment configuration is valid!")
			return nil
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to environment.yml if not in default location")

	return cmd
}
