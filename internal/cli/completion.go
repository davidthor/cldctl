package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newCompletionCmd())
}

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for cldctl.

To load completions:

Bash:
  $ source <(cldctl completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ cldctl completion bash > /etc/bash_completion.d/cldctl
  # macOS:
  $ cldctl completion bash > $(brew --prefix)/etc/bash_completion.d/cldctl

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ cldctl completion zsh > "${fpath[1]}/_cldctl"

  # You will need to start a new shell for this setup to take effect.

Fish:
  $ cldctl completion fish | source

  # To load completions for each session, execute once:
  $ cldctl completion fish > ~/.config/fish/completions/cldctl.fish

PowerShell:
  PS> cldctl completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> cldctl completion powershell > cldctl.ps1
  # and source this file from your PowerShell profile.
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletionV2(os.Stdout, true)
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				return rootCmd.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unknown shell: %s", args[0])
			}
		},
	}

	return cmd
}

// registerCompletions adds custom completion functions.
func registerCompletions() { //nolint:unused
	// Component name completion
	_ = rootCmd.RegisterFlagCompletionFunc("component", completeComponentNames)

	// Datacenter name completion
	_ = rootCmd.RegisterFlagCompletionFunc("datacenter", completeDatacenterNames)

	// Environment name completion
	_ = rootCmd.RegisterFlagCompletionFunc("environment", completeEnvironmentNames)

	// Backend type completion
	_ = rootCmd.RegisterFlagCompletionFunc("backend", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"local", "s3", "gcs"}, cobra.ShellCompDirectiveNoFileComp
	})
}

// completeComponentNames returns component names for completion.
func completeComponentNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) { //nolint:unused
	// In a real implementation, this would list components from the state or local files
	components := []string{}

	// Try to find local cloud.component.yml files
	if files, err := findComponentFiles("."); err == nil {
		components = append(components, files...)
	}

	return components, cobra.ShellCompDirectiveNoFileComp
}

// completeDatacenterNames returns datacenter names for completion.
func completeDatacenterNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) { //nolint:unused
	// In a real implementation, this would list datacenters from the state
	datacenters := []string{}

	// Try to find local datacenter.hcl files
	if files, err := findDatacenterFiles("."); err == nil {
		datacenters = append(datacenters, files...)
	}

	return datacenters, cobra.ShellCompDirectiveNoFileComp
}

// completeEnvironmentNames returns environment names for completion.
func completeEnvironmentNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) { //nolint:unused
	// In a real implementation, this would list environments from the state
	environments := []string{}

	// Try to find local environment.yml files
	if files, err := findEnvironmentFiles("."); err == nil {
		environments = append(environments, files...)
	}

	return environments, cobra.ShellCompDirectiveNoFileComp
}

// findComponentFiles finds cloud.component.yml files in a directory.
func findComponentFiles(dir string) ([]string, error) { //nolint:unused
	var files []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.Name() == "cloud.component.yml" || entry.Name() == "cloud.component.yaml" {
			files = append(files, dir)
		}
		if entry.IsDir() {
			subFiles, err := findComponentFiles(dir + "/" + entry.Name())
			if err == nil {
				files = append(files, subFiles...)
			}
		}
	}

	return files, nil
}

// findDatacenterFiles finds datacenter.hcl files in a directory.
func findDatacenterFiles(dir string) ([]string, error) { //nolint:unused
	var files []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.Name() == "datacenter.hcl" {
			files = append(files, dir)
		}
		if entry.IsDir() {
			subFiles, err := findDatacenterFiles(dir + "/" + entry.Name())
			if err == nil {
				files = append(files, subFiles...)
			}
		}
	}

	return files, nil
}

// findEnvironmentFiles finds environment.yml files in a directory.
func findEnvironmentFiles(dir string) ([]string, error) { //nolint:unused
	var files []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.Name() == "environment.yml" || entry.Name() == "environment.yaml" {
			files = append(files, dir)
		}
		if entry.IsDir() {
			subFiles, err := findEnvironmentFiles(dir + "/" + entry.Name())
			if err == nil {
				files = append(files, subFiles...)
			}
		}
	}

	return files, nil
}
