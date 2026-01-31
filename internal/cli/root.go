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
infrastructure templates that automatically provision resources.`,
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

	// Add subcommands
	rootCmd.AddCommand(newComponentCmd())
	rootCmd.AddCommand(newDatacenterCmd())
	rootCmd.AddCommand(newEnvironmentCmd())
	rootCmd.AddCommand(newUpCmd())
	rootCmd.AddCommand(newVersionCmd())

	// Add deploy as a top-level alias for 'component deploy'
	rootCmd.AddCommand(newDeployCmd())
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
