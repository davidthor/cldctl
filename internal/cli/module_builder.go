package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/architect-io/arcctl/pkg/iac/container"
	"github.com/architect-io/arcctl/pkg/schema/datacenter"
)

// moduleInfo holds information about a module for building.
type moduleInfo struct {
	sourceDir string
	plugin    string
}

// moduleBuilder wraps the container builder for CLI use.
type moduleBuilder struct {
	builder   *container.Builder
	baseImage string // shared provider base image tag (set after BuildProviderBase)
}

// createModuleBuilder creates a new module builder.
func createModuleBuilder() (*moduleBuilder, error) {
	b, err := container.NewBuilder()
	if err != nil {
		return nil, err
	}
	return &moduleBuilder{builder: b}, nil
}

// BuildProviderBase creates a shared Docker base image containing OpenTofu
// with all required providers pre-downloaded. This image is then used as the
// FROM base for every OpenTofu module build, so providers are downloaded only
// once instead of once per module.
func (m *moduleBuilder) BuildProviderBase(ctx context.Context, modules map[string]moduleInfo, baseTag string) error {
	// Collect only opentofu module directories (use clean names for tar structure)
	dirs := make(map[string]string)
	for name, mod := range modules {
		if mod.plugin == "opentofu" || mod.plugin == "terraform" {
			// Strip "module/" prefix so tar paths are flat: modules/<name>/
			cleanName := strings.TrimPrefix(name, "module/")
			dirs[cleanName] = mod.sourceDir
		}
	}
	if len(dirs) == 0 {
		return nil
	}

	fmt.Printf("[build] Building provider base image (%d modules)...\n", len(dirs))
	if err := m.builder.BuildProviderBase(ctx, dirs, baseTag, os.Stderr); err != nil {
		return fmt.Errorf("failed to build provider base image: %w", err)
	}
	m.baseImage = baseTag
	fmt.Printf("[success] Provider base image ready: %s\n\n", baseTag)
	return nil
}

// Build builds a module container image.
func (m *moduleBuilder) Build(ctx context.Context, sourceDir, plugin, tag string) (*container.BuildResult, error) {
	// Determine module type from plugin
	var moduleType container.ModuleType
	switch plugin {
	case "pulumi":
		moduleType = container.ModuleTypePulumi
	case "opentofu", "terraform":
		moduleType = container.ModuleTypeOpenTofu
	case "native":
		// Native modules don't need containerization - they use Docker SDK directly
		return &container.BuildResult{
			Image:      tag,
			ModuleType: "native",
		}, nil
	default:
		// Auto-detect from source
		moduleType = ""
	}

	return m.builder.Build(ctx, container.BuildOptions{
		ModuleDir:  sourceDir,
		ModuleType: moduleType,
		Tag:        tag,
		Output:     io.Discard, // Suppress verbose build output (errors still surface)
		BaseImage:  m.baseImage,
	})
}

// Push pushes a module container image to a remote registry using docker push.
// This relies on the Docker CLI being authenticated (e.g., via docker login).
func (m *moduleBuilder) Push(ctx context.Context, ref string) error {
	cmd := exec.CommandContext(ctx, "docker", "push", ref)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker push failed for %s: %w", ref, err)
	}
	return nil
}

// Close releases resources.
func (m *moduleBuilder) Close() error {
	return m.builder.Close()
}

// collectAllModules collects all modules from a datacenter configuration.
func collectAllModules(dc datacenter.Datacenter, dcPath string) map[string]moduleInfo {
	modules := make(map[string]moduleInfo)

	// Collect datacenter-level modules
	for _, mod := range dc.Modules() {
		if mod.Build() != "" {
			modulePath := fmt.Sprintf("module/%s", mod.Name())
			modules[modulePath] = moduleInfo{
				sourceDir: filepath.Join(dcPath, mod.Build()),
				plugin:    mod.Plugin(),
			}
		}
	}

	// Collect environment-level modules
	env := dc.Environment()
	if env != nil {
		for _, mod := range env.Modules() {
			if mod.Build() != "" {
				modulePath := fmt.Sprintf("module/%s", mod.Name())
				modules[modulePath] = moduleInfo{
					sourceDir: filepath.Join(dcPath, mod.Build()),
					plugin:    mod.Plugin(),
				}
			}
		}

		// Collect modules from all hook types
		collectHookModules(env.Hooks().Database(), modules, dcPath)
		collectHookModules(env.Hooks().DatabaseUser(), modules, dcPath)
		collectHookModules(env.Hooks().Task(), modules, dcPath)
		collectHookModules(env.Hooks().Bucket(), modules, dcPath)
		collectHookModules(env.Hooks().EncryptionKey(), modules, dcPath)
		collectHookModules(env.Hooks().SMTP(), modules, dcPath)
		collectHookModules(env.Hooks().Deployment(), modules, dcPath)
		collectHookModules(env.Hooks().Function(), modules, dcPath)
		collectHookModules(env.Hooks().Service(), modules, dcPath)
		collectHookModules(env.Hooks().Route(), modules, dcPath)
		collectHookModules(env.Hooks().Cronjob(), modules, dcPath)
		collectHookModules(env.Hooks().Secret(), modules, dcPath)
		collectHookModules(env.Hooks().DockerBuild(), modules, dcPath)
		collectHookModules(env.Hooks().Observability(), modules, dcPath)
	}

	return modules
}

// collectHookModules collects modules from hooks.
func collectHookModules(hooks []datacenter.Hook, modules map[string]moduleInfo, dcPath string) {
	for _, hook := range hooks {
		for _, mod := range hook.Modules() {
			if mod.Build() != "" {
				modulePath := fmt.Sprintf("module/%s", mod.Name())
				if _, exists := modules[modulePath]; !exists {
					modules[modulePath] = moduleInfo{
						sourceDir: filepath.Join(dcPath, mod.Build()),
						plugin:    mod.Plugin(),
					}
				}
			}
		}
	}
}
