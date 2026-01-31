// Package cli implements the arcctl CLI commands.
package cli

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	// Import state backends to register them via init()
	_ "github.com/architect-io/arcctl/pkg/state/backend/azurerm"
	_ "github.com/architect-io/arcctl/pkg/state/backend/gcs"
	_ "github.com/architect-io/arcctl/pkg/state/backend/local"
	_ "github.com/architect-io/arcctl/pkg/state/backend/s3"
)

var (
	cfgFile string
)

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:   "arcctl",
	Short: "Deploy cloud-native applications anywhere",
	Long: `arcctl is a CLI tool for deploying portable cloud applications.

It enables developers to describe cloud applications without learning 
infrastructure-as-code, while platform engineers create reusable 
infrastructure templates that automatically provision resources.

Command Structure:
  arcctl <action> <resource> [arguments] [flags]

Examples:
  arcctl build component ./my-app -t ghcr.io/myorg/app:v1
  arcctl deploy component ./my-app -e production
  arcctl create environment staging -d my-datacenter
  arcctl list environment
  arcctl destroy component my-app -e staging`,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.arcctl/config.yaml)")
	rootCmd.PersistentFlags().String("backend", "local", "State backend type (local, s3, gcs)")
	rootCmd.PersistentFlags().StringArray("backend-config", nil, "Backend configuration (key=value)")

	// Bind to viper
	_ = viper.BindPFlag("backend", rootCmd.PersistentFlags().Lookup("backend"))
	viper.SetEnvPrefix("ARCCTL")
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

	// Keep the up command and version command
	rootCmd.AddCommand(newUpCmd())
	rootCmd.AddCommand(newVersionCmd())
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		// Search for config in home directory
		home, err := os.UserHomeDir()
		if err == nil {
			viper.AddConfigPath(home + "/.arcctl")
			viper.SetConfigName("config")
			viper.SetConfigType("yaml")
		}
	}

	// Read config file if it exists
	_ = viper.ReadInConfig()
}
