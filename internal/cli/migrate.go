package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"

	"github.com/architect-io/arcctl/pkg/state/backend"
	"github.com/architect-io/arcctl/pkg/state/types"
	"github.com/spf13/cobra"
)

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migration utilities",
		Long:  `Commands for migrating arcctl state and configuration.`,
	}

	cmd.AddCommand(newMigrateStateCmd())

	return cmd
}

func newMigrateStateCmd() *cobra.Command {
	var (
		autoApprove   bool
		backendType   string
		backendConfig []string
	)

	cmd := &cobra.Command{
		Use:   "state",
		Short: "Migrate state from flat to nested hierarchy",
		Long: `Migrate environment state from the old flat layout to the new
datacenter-nested layout.

Old layout:
  environments/<env>/environment.state.json
  environments/<env>/components/<comp>/component.state.json

New layout:
  datacenters/<dc>/environments/<env>/environment.state.json
  datacenters/<dc>/environments/<env>/components/<comp>/component.state.json

This command reads each environment's state to determine its datacenter,
then copies all files to the new path and removes the old files.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Create state manager backend directly (we need raw access)
			b, err := createBackend(backendType, backendConfig)
			if err != nil {
				return fmt.Errorf("failed to create backend: %w", err)
			}

			// Check if old-style state exists
			oldPaths, err := b.List(ctx, "environments/")
			if err != nil {
				return fmt.Errorf("failed to list old state: %w", err)
			}

			if len(oldPaths) == 0 {
				fmt.Println("No old-style environment state found. Nothing to migrate.")
				return nil
			}

			// Find all environments in the old layout
			envNames := make(map[string]bool)
			for _, p := range oldPaths {
				parts := splitMigratePath(p)
				// Path format: environments/<name>/...
				if len(parts) >= 2 {
					envNames[parts[1]] = true
				}
			}

			// Read each environment to find its datacenter
			type envMigration struct {
				name       string
				datacenter string
			}
			var migrations []envMigration

			for name := range envNames {
				envStatePath := path.Join("environments", name, "environment.state.json")
				reader, err := b.Read(ctx, envStatePath)
				if err != nil {
					fmt.Printf("Warning: could not read %s, skipping: %v\n", envStatePath, err)
					continue
				}

				var envState types.EnvironmentState
				if err := json.NewDecoder(reader).Decode(&envState); err != nil {
					reader.Close()
					fmt.Printf("Warning: could not decode %s, skipping: %v\n", envStatePath, err)
					continue
				}
				reader.Close()

				if envState.Datacenter == "" {
					fmt.Printf("Warning: environment %q has no datacenter field, skipping\n", name)
					continue
				}

				migrations = append(migrations, envMigration{
					name:       name,
					datacenter: envState.Datacenter,
				})
			}

			if len(migrations) == 0 {
				fmt.Println("No environments with datacenter references found. Nothing to migrate.")
				return nil
			}

			// Display migration plan
			fmt.Println("State Migration Plan:")
			fmt.Println()
			for _, m := range migrations {
				fmt.Printf("  %s -> datacenters/%s/environments/%s/\n", m.name, m.datacenter, m.name)
			}
			fmt.Println()
			fmt.Printf("Total: %d environments to migrate\n", len(migrations))
			fmt.Println()

			// Confirm
			if !autoApprove {
				fmt.Print("Proceed with migration? [y/N]: ")
				var response string
				_, _ = fmt.Scanln(&response)
				if response != "y" && response != "yes" {
					fmt.Println("Migration cancelled.")
					return nil
				}
			}

			fmt.Println()

			// Execute migration
			var migrated, errCount int
			for _, m := range migrations {
				fmt.Printf("[migrate] Moving environment %q to datacenter %q...\n", m.name, m.datacenter)

				oldPrefix := path.Join("environments", m.name)
				newPrefix := path.Join("datacenters", m.datacenter, "environments", m.name)

				// List all files under this environment
				envFiles, err := b.List(ctx, oldPrefix+"/")
				if err != nil {
					fmt.Printf("  Error listing files: %v\n", err)
					errCount++
					continue
				}

				allCopied := true
				for _, oldPath := range envFiles {
					// Compute new path
					relPath := oldPath[len(oldPrefix):]
					newPath := newPrefix + relPath

					// Copy file
					reader, err := b.Read(ctx, oldPath)
					if err != nil {
						fmt.Printf("  Error reading %s: %v\n", oldPath, err)
						allCopied = false
						continue
					}

					data, err := io.ReadAll(reader)
					reader.Close()
					if err != nil {
						fmt.Printf("  Error reading %s: %v\n", oldPath, err)
						allCopied = false
						continue
					}

					if err := b.Write(ctx, newPath, bytes.NewReader(data)); err != nil {
						fmt.Printf("  Error writing %s: %v\n", newPath, err)
						allCopied = false
						continue
					}
				}

				// Only delete old files if all were successfully copied
				if allCopied {
					for _, oldPath := range envFiles {
						if err := b.Delete(ctx, oldPath); err != nil {
							fmt.Printf("  Warning: failed to delete old file %s: %v\n", oldPath, err)
						}
					}
					migrated++
					fmt.Printf("  Migrated %d files\n", len(envFiles))
				} else {
					errCount++
					fmt.Printf("  Error: some files could not be copied, skipping cleanup\n")
				}
			}

			fmt.Println()
			if errCount > 0 {
				fmt.Printf("[warning] Migration completed with %d errors (%d/%d environments migrated)\n", errCount, migrated, len(migrations))
				return fmt.Errorf("migration completed with errors")
			}

			fmt.Printf("[success] Migration completed: %d environments migrated\n", migrated)
			return nil
		},
	}

	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&backendType, "backend", "", "State backend type")
	cmd.Flags().StringArrayVar(&backendConfig, "backend-config", nil, "Backend configuration (key=value)")

	return cmd
}

// createBackend creates a raw backend from type and config flags.
func createBackend(backendType string, backendConfigFlags []string) (backend.Backend, error) {
	// Reuse the same config resolution logic as createStateManagerWithConfig
	mgr, err := createStateManagerWithConfig(backendType, backendConfigFlags)
	if err != nil {
		return nil, err
	}
	return mgr.Backend(), nil
}

// splitMigratePath splits a path into its components.
func splitMigratePath(p string) []string {
	var parts []string
	for p != "" && p != "." && p != "/" {
		dir, file := path.Split(p)
		if file != "" {
			parts = append([]string{file}, parts...)
		}
		p = path.Clean(dir)
	}
	return parts
}
