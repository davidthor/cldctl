package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/architect-io/arcctl/pkg/schema/component"
	"github.com/architect-io/arcctl/pkg/state"
	"github.com/architect-io/arcctl/pkg/state/types"
	"github.com/spf13/cobra"
)

func newUpCmd() *cobra.Command {
	var (
		file      string
		name      string
		variables []string
		varFile   string
		detach    bool
		noOpen    bool
		port      int
	)

	cmd := &cobra.Command{
		Use:   "up [path]",
		Short: "Deploy a component to a local environment",
		Long: `The up command provides a streamlined experience for local development.
It deploys your component with all its dependencies to a local environment
with minimal configuration.

The up command:
  1. Parses your architect.yml file
  2. Creates a local development environment
  3. Provisions all required resources (databases, etc.)
  4. Builds and deploys your application
  5. Watches for file changes and auto-reloads (unless --detach)
  6. Exposes routes for local access`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) > 0 {
				path = args[0]
			}

			// Get absolute path for reliable operations
			absPath, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("failed to resolve path: %w", err)
			}

			// Determine architect.yml location
			componentFile := file
			if componentFile == "" {
				componentFile = filepath.Join(absPath, "architect.yml")
			}

			// Load the component
			loader := component.NewLoader()
			comp, err := loader.Load(componentFile)
			if err != nil {
				return fmt.Errorf("failed to load component: %w", err)
			}

			// Determine environment name from flag or derive from directory name
			envName := name
			if envName == "" {
				dirName := filepath.Base(absPath)
				envName = fmt.Sprintf("%s-dev", dirName)
			}

			// Load variables from file if specified
			vars := make(map[string]string)
			if varFile != "" {
				data, err := os.ReadFile(varFile)
				if err != nil {
					return fmt.Errorf("failed to read var file: %w", err)
				}
				if err := upParseVarFile(data, vars); err != nil {
					return fmt.Errorf("failed to parse var file: %w", err)
				}
			}

			// Parse inline variables
			for _, v := range variables {
				parts := strings.SplitN(v, "=", 2)
				if len(parts) == 2 {
					vars[parts[0]] = parts[1]
				}
			}

			componentName := filepath.Base(absPath)
			fmt.Printf("Component: %s\n", componentName)
			fmt.Printf("Environment: %s (local)\n", envName)
			fmt.Println()

			// Create state manager (always local for 'up')
			mgr, err := upCreateStateManager()
			if err != nil {
				return fmt.Errorf("failed to create state manager: %w", err)
			}

			ctx := context.Background()

			// Ensure local datacenter exists
			dcName := "local"
			dc, err := mgr.GetDatacenter(ctx, dcName)
			if err != nil {
				dc = &types.DatacenterState{
					Name:         dcName,
					Version:      "local",
					CreatedAt:    time.Now(),
					UpdatedAt:    time.Now(),
					Environments: []string{},
				}
				if err := mgr.SaveDatacenter(ctx, dc); err != nil {
					return fmt.Errorf("failed to create local datacenter: %w", err)
				}
			}

			// Create or get environment
			env, err := mgr.GetEnvironment(ctx, envName)
			if err != nil {
				env = &types.EnvironmentState{
					Name:       envName,
					Datacenter: dcName,
					Status:     types.EnvironmentStatusPending,
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
					Components: make(map[string]*types.ComponentState),
				}

				dc.Environments = append(dc.Environments, envName)
				dc.UpdatedAt = time.Now()
				if err := mgr.SaveDatacenter(ctx, dc); err != nil {
					return fmt.Errorf("failed to update datacenter: %w", err)
				}
			}

			// Initialize Docker provisioner
			basePort := port
			if basePort == 0 {
				basePort = 8080
			}
			provisioner := NewDockerProvisioner(envName, basePort)

			// Ensure Docker network exists
			fmt.Println("[network] Creating Docker network...")
			if err := provisioner.EnsureNetwork(ctx); err != nil {
				return fmt.Errorf("failed to create network: %w", err)
			}

			// Collect database connection info for deployments
			dbConnections := make(map[string]*DatabaseConnection)

			// Provision databases
			for _, db := range comp.Databases() {
				fmt.Printf("[provision] Creating database %q (%s)...\n", db.Name(), db.Type())
				conn, err := provisioner.ProvisionDatabase(ctx, db, componentName)
				if err != nil {
					return fmt.Errorf("failed to provision database %q: %w", db.Name(), err)
				}
				dbConnections[db.Name()] = conn
				fmt.Printf("[provision] Database %q ready at localhost:%d\n", db.Name(), conn.Port)
			}

			// Provision buckets (placeholder - would need MinIO or similar)
			for _, bucket := range comp.Buckets() {
				fmt.Printf("[provision] Bucket %q not yet implemented (skipping)\n", bucket.Name())
			}

			// Build and deploy containers (deployments and functions)
			var appPort int

			// Helper function to build environment variables
			buildEnv := func() map[string]string {
				env := make(map[string]string)
				// Add database URLs
				for dbName, conn := range dbConnections {
					containerDBHost := fmt.Sprintf("%s-%s-%s", envName, componentName, dbName)
					containerURL := fmt.Sprintf("postgres://app:%s@%s:5432/%s?sslmode=disable",
						conn.Password, containerDBHost, conn.Database)
					env["DATABASE_URL"] = containerURL
					env[fmt.Sprintf("DB_%s_URL", strings.ToUpper(dbName))] = containerURL
					env[fmt.Sprintf("DB_%s_HOST", strings.ToUpper(dbName))] = containerDBHost
					env[fmt.Sprintf("DB_%s_PORT", strings.ToUpper(dbName))] = "5432"
					env[fmt.Sprintf("DB_%s_USER", strings.ToUpper(dbName))] = conn.Username
					env[fmt.Sprintf("DB_%s_PASSWORD", strings.ToUpper(dbName))] = conn.Password
					env[fmt.Sprintf("DB_%s_NAME", strings.ToUpper(dbName))] = conn.Database
				}
				// Add user-provided variables
				for k, v := range vars {
					env[k] = v
				}
				return env
			}

			// Helper function to build and run a workload (deployment or function)
			buildAndRun := func(name string, build component.Build, image string, resourceType string) error {
				workloadEnv := buildEnv()

				var imageTag string
				if build != nil {
					buildCtx := build.Context()
					if !filepath.IsAbs(buildCtx) {
						buildCtx = filepath.Join(absPath, buildCtx)
					}

					dockerfile := ""
					explicitDockerfile := build.Dockerfile()
					if explicitDockerfile != "" && explicitDockerfile != "Dockerfile" {
						if !filepath.IsAbs(explicitDockerfile) {
							dockerfile = filepath.Join(buildCtx, explicitDockerfile)
						} else {
							dockerfile = explicitDockerfile
						}
					}

					// Collect build args (NEXT_PUBLIC_* vars need to be available at build time)
					buildArgs := make(map[string]string)
					for k, v := range vars {
						if strings.HasPrefix(k, "NEXT_PUBLIC_") {
							buildArgs[k] = v
						}
					}

					fmt.Printf("[build] Building %s %q from %s...\n", resourceType, name, buildCtx)
					var err error
					imageTag, err = provisioner.BuildImage(ctx, name, buildCtx, dockerfile, buildArgs)
					if err != nil {
						return fmt.Errorf("failed to build %s %q: %w", resourceType, name, err)
					}
					fmt.Printf("[build] Built image %s\n", imageTag)
				} else if image != "" {
					imageTag = image
				} else {
					fmt.Printf("[%s] %s %q has no build context or image (skipping)\n", resourceType, resourceType, name)
					return nil
				}

				containerPort := 3000 // Default for Next.js/Node apps
				ports := map[int]int{containerPort: 0}

				fmt.Printf("[%s] Starting %s %q...\n", resourceType, resourceType, name)
				_, hostPort, err := provisioner.RunContainer(ctx, name, imageTag, componentName, workloadEnv, ports)
				if err != nil {
					return fmt.Errorf("failed to run %s %q: %w", resourceType, name, err)
				}
				appPort = hostPort
				fmt.Printf("[%s] %s %q running on port %d\n", resourceType, resourceType, name, hostPort)
				return nil
			}

			// Process functions (preferred for Next.js apps)
			for _, fn := range comp.Functions() {
				if err := buildAndRun(fn.Name(), fn.Build(), fn.Image(), "function"); err != nil {
					return err
				}
			}

			// Process deployments
			for _, depl := range comp.Deployments() {
				if err := buildAndRun(depl.Name(), depl.Build(), depl.Image(), "deployment"); err != nil {
					return err
				}
			}

			// Expose routes
			routeURLs := make(map[string]string)
			for _, route := range comp.Routes() {
				url := fmt.Sprintf("http://localhost:%d", appPort)
				routeURLs[route.Name()] = url
				fmt.Printf("[expose] Route %q available at %s\n", route.Name(), url)
			}

			fmt.Println()
			if len(routeURLs) > 0 {
				var primaryURL string
				for _, url := range routeURLs {
					primaryURL = url
					break
				}
				fmt.Printf("Application running at %s\n", primaryURL)

				// Open browser unless --no-open is specified
				if !noOpen && !detach {
					openBrowser(primaryURL)
				}
			}

			// Update environment state
			env.Status = types.EnvironmentStatusReady
			env.UpdatedAt = time.Now()
			env.Variables = vars

			compState := &types.ComponentState{
				Name:       componentName,
				Version:    "local",
				Source:     path,
				Status:     types.ResourceStatusReady,
				Variables:  vars,
				DeployedAt: time.Now(),
				UpdatedAt:  time.Now(),
				Resources:  provisioner.ProvisionedResources(),
			}
			env.Components[componentName] = compState

			if err := mgr.SaveEnvironment(ctx, env); err != nil {
				return fmt.Errorf("failed to save environment state: %w", err)
			}

			if detach {
				fmt.Println()
				fmt.Println("Running in background. To stop:")
				fmt.Printf("  arcctl destroy environment %s\n", envName)
			} else {
				fmt.Println()
				fmt.Println("Press Ctrl+C to stop...")

				// Handle graceful shutdown
				sigChan := make(chan os.Signal, 1)
				signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
				<-sigChan

				fmt.Println()
				fmt.Println("Shutting down...")

				// Cleanup containers
				if err := CleanupByEnvName(ctx, envName); err != nil {
					fmt.Printf("Warning: failed to cleanup containers: %v\n", err)
				}

				// Update state
				env.Status = types.EnvironmentStatusPending
				env.UpdatedAt = time.Now()
				_ = mgr.SaveEnvironment(ctx, env)

				fmt.Println("Stopped.")
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to architect.yml if not in default location")
	cmd.Flags().StringVarP(&name, "name", "n", "", "Environment name (default: auto-generated from component name)")
	cmd.Flags().StringArrayVar(&variables, "var", nil, "Set a component variable (key=value)")
	cmd.Flags().StringVar(&varFile, "var-file", "", "Load variables from a file")
	cmd.Flags().BoolVarP(&detach, "detach", "d", false, "Run in background (don't watch for changes)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Don't open browser to application URL")
	cmd.Flags().IntVar(&port, "port", 0, "Override the port for local access (default: 8080)")

	return cmd
}

// openBrowser opens the default browser to the given URL.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return
	}
	_ = cmd.Start()
}

// Helper functions for up command

func upCreateStateManager() (state.Manager, error) {
	// Use config file defaults with no CLI overrides
	return createStateManagerWithConfig("", nil)
}

func upParseVarFile(data []byte, vars map[string]string) error {
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, "\"'")
			vars[key] = value
		}
	}
	return nil
}
