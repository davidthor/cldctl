// Package cli implements the cldctl CLI commands.
package cli

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	// Import state backends to register them via init()
	_ "github.com/davidthor/cldctl/pkg/state/backend/azurerm"
	_ "github.com/davidthor/cldctl/pkg/state/backend/gcs"
	_ "github.com/davidthor/cldctl/pkg/state/backend/local"
	_ "github.com/davidthor/cldctl/pkg/state/backend/s3"

	// Import log query adapters to register them via init()
	_ "github.com/davidthor/cldctl/pkg/logs/loki"
)

var (
	cfgFile string
)

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:   "cldctl",
	Short: "Deploy cloud-native applications anywhere",
	Long: `cldctl is a CLI tool for deploying portable cloud applications.

It enables developers to describe cloud applications without learning 
infrastructure-as-code, while platform engineers create reusable 
infrastructure templates that automatically provision resources.

Command Structure:
  cldctl <action> <resource> [arguments] [flags]

Examples:
  cldctl build component ./my-app -t ghcr.io/myorg/app:v1
  cldctl deploy component ghcr.io/myorg/app:v1 -e production
  cldctl create environment staging -d my-datacenter
  cldctl list environment
  cldctl destroy component my-app -e staging`,
	SilenceErrors: true,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cldctl/config.yaml)")
	rootCmd.PersistentFlags().String("backend", "local", "State backend type (local, s3, gcs)")
	rootCmd.PersistentFlags().StringArray("backend-config", nil, "Backend configuration (key=value)")

	// Bind to viper
	_ = viper.BindPFlag("backend", rootCmd.PersistentFlags().Lookup("backend"))
	viper.SetEnvPrefix("CLDCTL")
	viper.AutomaticEnv()

	// Add action-based commands (new inverted syntax)
	rootCmd.AddCommand(newBuildCmd())
	rootCmd.AddCommand(newDeployCmd())
	rootCmd.AddCommand(newDestroyCmd())
	rootCmd.AddCommand(newListCmd())
	rootCmd.AddCommand(newGetCmd())
	rootCmd.AddCommand(newCreateCmd())
	rootCmd.AddCommand(newUpdateCmd())
	rootCmd.AddCommand(newTagCmd())
	rootCmd.AddCommand(newPushCmd())
	rootCmd.AddCommand(newPullCmd())
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.AddCommand(newInspectCmd())

	// Keep the up command and version command
	rootCmd.AddCommand(newUpCmd())
	rootCmd.AddCommand(newVersionCmd())

	// Configuration commands
	rootCmd.AddCommand(newConfigCmd())

	// Artifact cache commands
	rootCmd.AddCommand(newImagesCmd())

	// Migration commands
	rootCmd.AddCommand(newMigrateCmd())

	// Observability commands
	rootCmd.AddCommand(newLogsCmd())
	rootCmd.AddCommand(newObservabilityCmd())
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		// Search for config in home directory
		home, err := os.UserHomeDir()
		if err == nil {
			viper.AddConfigPath(home + "/.cldctl")
			viper.SetConfigName("config")
			viper.SetConfigType("yaml")
		}
	}

	// Read config file if it exists
	_ = viper.ReadInConfig()
}
