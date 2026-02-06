package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	// ConfigKeyDefaultDatacenter is the viper/config key for the default datacenter.
	ConfigKeyDefaultDatacenter = "default_datacenter"

	// EnvDefaultDatacenter is the environment variable for the default datacenter.
	EnvDefaultDatacenter = "ARCCTL_DATACENTER"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration",
		Long:  `Get and set arcctl CLI configuration values stored in ~/.arcctl/config.yaml.`,
	}

	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigGetCmd())
	cmd.AddCommand(newConfigListCmd())

	return cmd
}

func newConfigSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Set a configuration value in ~/.arcctl/config.yaml.

Available keys:
  default-datacenter    The datacenter used when --datacenter/-d is not specified.

Examples:
  arcctl config set default-datacenter my-dc`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			value := args[1]

			// Normalize key names: allow dashes in CLI, store with underscores
			viperKey := normalizeConfigKey(key)

			switch viperKey {
			case ConfigKeyDefaultDatacenter:
				// valid
			default:
				return fmt.Errorf("unknown configuration key %q\n\nAvailable keys:\n  default-datacenter", key)
			}

			viper.Set(viperKey, value)
			if err := writeConfig(); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Printf("Set %s = %s\n", key, value)
			return nil
		},
	}

	return cmd
}

func newConfigGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Long: `Get a configuration value from ~/.arcctl/config.yaml.

Examples:
  arcctl config get default-datacenter`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			viperKey := normalizeConfigKey(key)

			value := viper.GetString(viperKey)
			if value == "" {
				fmt.Printf("%s is not set\n", key)
			} else {
				fmt.Println(value)
			}
			return nil
		},
	}

	return cmd
}

func newConfigListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all configuration values",
		Long:  `List all configuration values from ~/.arcctl/config.yaml.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dc := viper.GetString(ConfigKeyDefaultDatacenter)

			fmt.Println("Configuration:")
			if dc != "" {
				fmt.Printf("  default-datacenter = %s\n", dc)
			} else {
				fmt.Println("  (no values set)")
			}

			return nil
		},
	}

	return cmd
}

// resolveDatacenter resolves the datacenter name from multiple sources.
//
// Precedence (highest to lowest):
//  1. --datacenter/-d flag (explicit)
//  2. ARCCTL_DATACENTER environment variable
//  3. default_datacenter from ~/.arcctl/config.yaml
//  4. Error if none set
func resolveDatacenter(flagValue string) (string, error) {
	// 1. Explicit flag
	if flagValue != "" {
		return flagValue, nil
	}

	// 2. Environment variable
	if envVal := os.Getenv(EnvDefaultDatacenter); envVal != "" {
		return envVal, nil
	}

	// 3. Config file default
	if configVal := viper.GetString(ConfigKeyDefaultDatacenter); configVal != "" {
		return configVal, nil
	}

	// 4. Error
	return "", fmt.Errorf(
		"no datacenter specified\n\n" +
			"Specify a datacenter using one of:\n" +
			"  --datacenter/-d flag\n" +
			"  ARCCTL_DATACENTER environment variable\n" +
			"  arcctl config set default-datacenter <name>\n\n" +
			"Deploying a datacenter automatically sets the default.",
	)
}

// setDefaultDatacenter updates the default datacenter in the config file.
func setDefaultDatacenter(name string) error {
	viper.Set(ConfigKeyDefaultDatacenter, name)
	return writeConfig()
}

// writeConfig writes the current viper config to the config file.
func writeConfig() error {
	configPath := viper.ConfigFileUsed()
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		configDir := filepath.Join(home, ".arcctl")
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
		configPath = filepath.Join(configDir, "config.yaml")
	}

	return viper.WriteConfigAs(configPath)
}

// normalizeConfigKey converts CLI-style keys (with dashes) to viper-style keys (with underscores).
func normalizeConfigKey(key string) string {
	switch key {
	case "default-datacenter":
		return ConfigKeyDefaultDatacenter
	default:
		return key
	}
}
