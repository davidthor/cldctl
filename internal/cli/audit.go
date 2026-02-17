package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	nativepkg "github.com/davidthor/cldctl/pkg/iac/native"
	"github.com/davidthor/cldctl/pkg/registry"
	"github.com/davidthor/cldctl/pkg/schema/component"
	"github.com/davidthor/cldctl/pkg/schema/datacenter"
	"github.com/spf13/cobra"
)

func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Audit component and datacenter templates (without deploying)",
		Long: `Examine component and datacenter templates to see their structure,
hooks, modules, and IaC resource addresses — without deploying anything.

Unlike 'inspect' (which examines deployed state), 'audit' reads the
template configuration directly from a local path or cached OCI image.

This is particularly useful when building import mapping files: use
'audit datacenter --modules' to discover the IaC resource addresses
you need.`,
	}

	cmd.AddCommand(newAuditDatacenterCmd())
	cmd.AddCommand(newAuditComponentCmd())

	return cmd
}

func newAuditDatacenterCmd() *cobra.Command {
	var showModules bool

	cmd := &cobra.Command{
		Use:     "datacenter [path|image]",
		Aliases: []string{"dc"},
		Short:   "Audit a datacenter template's hooks, modules, and IaC resource addresses",
		Long: `Examine a datacenter template (from a local path or cached OCI image) to
see its variables, hooks, and modules.

Use --modules to list the IaC resource addresses defined in each module.
This is essential for building import mapping files — it tells you which
addresses to use in --import-file YAML or --map flags.

Examples:
  # Show hooks, modules, and variables
  cldctl audit datacenter ./my-datacenter
  cldctl audit datacenter ghcr.io/myorg/dc:v1.0.0

  # Show IaC resource addresses for import mapping files
  cldctl audit datacenter ./my-datacenter --modules
  cldctl audit datacenter ghcr.io/cldctl/aws-ecs:latest --modules`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := "."
			if len(args) > 0 {
				ref = args[0]
			}

			// Resolve to a local path
			dcFile, err := resolveDatacenterFile(ref)
			if err != nil {
				return fmt.Errorf("failed to resolve datacenter %q: %w", ref, err)
			}

			// Load the datacenter config
			loader := datacenter.NewLoader()
			dc, err := loader.Load(dcFile)
			if err != nil {
				return fmt.Errorf("failed to load datacenter: %w", err)
			}

			if showModules {
				return printDatacenterModuleAddresses(dc)
			}

			return printDatacenterOverview(dc)
		},
	}

	cmd.Flags().BoolVar(&showModules, "modules", false, "Show IaC resource addresses for each module (for import mapping)")

	return cmd
}

func newAuditComponentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "component [path|image]",
		Aliases: []string{"comp"},
		Short:   "Audit a component template's resources and dependencies",
		Long: `Examine a component template (from a local path or cached OCI image) to
see its resource keys — the identifiers you need for import mapping files.

This shows every resource the component declares (databases, deployments,
services, routes, etc.) with the type-qualified keys used in import mappings.

Examples:
  cldctl audit component ./my-app
  cldctl audit component ghcr.io/myorg/app:v1.0.0`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := "."
			if len(args) > 0 {
				ref = args[0]
			}

			// Resolve the component reference
			compFile, err := resolveComponentFile(ref)
			if err != nil {
				return fmt.Errorf("failed to resolve component %q: %w", ref, err)
			}

			loader := component.NewLoader()
			comp, err := loader.Load(compFile)
			if err != nil {
				return fmt.Errorf("failed to load component: %w", err)
			}

			return printComponentResourceKeys(comp)
		},
	}

	return cmd
}

// resolveComponentFile resolves a component reference to a local file path.
func resolveComponentFile(ref string) (string, error) {
	if strings.HasPrefix(ref, ".") || strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "~") {
		absPath := ref
		if !filepath.IsAbs(ref) {
			cwd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			absPath = filepath.Join(cwd, ref)
		}

		info, err := os.Stat(absPath)
		if err != nil {
			return "", fmt.Errorf("path not found: %s", absPath)
		}

		if info.IsDir() {
			for _, name := range []string{"cld.yml", "cld.yaml"} {
				f := filepath.Join(absPath, name)
				if _, err := os.Stat(f); err == nil {
					return f, nil
				}
			}
			return "", fmt.Errorf("no cld.yml found in %s", absPath)
		}

		return absPath, nil
	}

	// Try loading from local artifact cache
	reg, err := registry.NewRegistry()
	if err != nil {
		return "", fmt.Errorf("failed to open registry: %w", err)
	}

	entry, err := reg.Get(ref)
	if err != nil || entry == nil || entry.CachePath == "" {
		return "", fmt.Errorf("component %q not found in local cache; try: cldctl pull component %s", ref, ref)
	}

	for _, name := range []string{"cld.yml", "cld.yaml"} {
		f := filepath.Join(entry.CachePath, name)
		if _, err := os.Stat(f); err == nil {
			return f, nil
		}
	}

	return "", fmt.Errorf("no cld.yml found in cached artifact for %s", ref)
}

// printComponentResourceKeys lists all resource keys a component defines.
func printComponentResourceKeys(comp component.Component) error {
	fmt.Println()
	fmt.Println("Component Resource Keys")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println()
	fmt.Println("Use these keys in import mapping files (resources section).")
	fmt.Println()

	type resEntry struct {
		key  string
		info string
	}
	var entries []resEntry

	for _, db := range comp.Databases() {
		entries = append(entries, resEntry{
			key:  "database." + db.Name(),
			info: db.Type(),
		})
	}
	for _, b := range comp.Buckets() {
		entries = append(entries, resEntry{
			key:  "bucket." + b.Name(),
			info: b.Type(),
		})
	}
	for _, d := range comp.Deployments() {
		info := ""
		if d.Image() != "" {
			info = "image=" + d.Image()
		}
		entries = append(entries, resEntry{
			key:  "deployment." + d.Name(),
			info: info,
		})
	}
	for _, f := range comp.Functions() {
		entries = append(entries, resEntry{
			key:  "function." + f.Name(),
			info: "",
		})
	}
	for _, s := range comp.Services() {
		entries = append(entries, resEntry{
			key:  "service." + s.Name(),
			info: fmt.Sprintf("port=%s", s.Port()),
		})
	}
	for _, r := range comp.Routes() {
		entries = append(entries, resEntry{
			key:  "route." + r.Name(),
			info: r.Type(),
		})
	}
	for _, c := range comp.Cronjobs() {
		entries = append(entries, resEntry{
			key:  "cronjob." + c.Name(),
			info: c.Schedule(),
		})
	}

	if len(entries) == 0 {
		fmt.Println("  (no resources defined)")
	} else {
		for _, e := range entries {
			if e.info != "" {
				fmt.Printf("  %-30s %s\n", e.key, e.info)
			} else {
				fmt.Printf("  %s\n", e.key)
			}
		}
	}

	// Dependencies
	deps := comp.Dependencies()
	if len(deps) > 0 {
		fmt.Println()
		fmt.Println("Dependencies:")
		for _, dep := range deps {
			optional := ""
			if dep.Optional() {
				optional = " (optional)"
			}
			fmt.Printf("  %-30s %s%s\n", dep.Name(), dep.Component(), optional)
		}
	}

	// Variables
	vars := comp.Variables()
	if len(vars) > 0 {
		fmt.Println()
		fmt.Println("Variables:")
		for _, v := range vars {
			required := ""
			if v.Required() {
				required = " [required]"
			}
			fmt.Printf("  %-30s%s\n", v.Name(), required)
		}
	}

	fmt.Println()
	return nil
}

// resolveDatacenterFile resolves a datacenter reference to a local file path.
// Handles local paths (directories or files) and OCI references (from local cache).
func resolveDatacenterFile(ref string) (string, error) {
	// Check if this is a local filesystem path
	if strings.HasPrefix(ref, ".") || strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "~") {
		absPath := ref
		if !filepath.IsAbs(ref) {
			cwd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			absPath = filepath.Join(cwd, ref)
		}

		info, err := os.Stat(absPath)
		if err != nil {
			return "", fmt.Errorf("path not found: %s", absPath)
		}

		if info.IsDir() {
			for _, name := range []string{"datacenter.dc", "datacenter.hcl"} {
				dcFile := filepath.Join(absPath, name)
				if _, err := os.Stat(dcFile); err == nil {
					return dcFile, nil
				}
			}
			return "", fmt.Errorf("no datacenter.dc or datacenter.hcl found in %s", absPath)
		}

		return absPath, nil
	}

	// Try loading from local artifact cache
	reg, err := registry.NewRegistry()
	if err != nil {
		return "", fmt.Errorf("failed to open registry: %w", err)
	}

	entry, err := reg.Get(ref)
	if err != nil || entry == nil || entry.CachePath == "" {
		return "", fmt.Errorf("datacenter %q not found in local cache; try: cldctl pull datacenter %s", ref, ref)
	}

	for _, name := range []string{"datacenter.dc", "datacenter.hcl"} {
		dcFile := filepath.Join(entry.CachePath, name)
		if _, err := os.Stat(dcFile); err == nil {
			return dcFile, nil
		}
	}

	return "", fmt.Errorf("no datacenter.dc or datacenter.hcl found in cached artifact for %s", ref)
}

// printDatacenterOverview shows a summary of the datacenter template.
func printDatacenterOverview(dc datacenter.Datacenter) error {
	fmt.Println()
	fmt.Println("Datacenter Template")
	fmt.Println(strings.Repeat("=", 60))

	vars := dc.Variables()
	if len(vars) > 0 {
		fmt.Printf("\n  Variables (%d):\n", len(vars))
		for _, v := range vars {
			required := ""
			if v.Required() {
				required = " [required]"
			}
			defVal := ""
			if v.Default() != nil {
				defVal = fmt.Sprintf(" (default: %v)", v.Default())
			}
			fmt.Printf("    %-25s %s%s%s\n", v.Name(), v.Type(), required, defVal)
		}
	}

	rootMods := dc.Modules()
	if len(rootMods) > 0 {
		fmt.Printf("\n  Root Modules (%d):\n", len(rootMods))
		for _, mod := range rootMods {
			plugin := mod.Plugin()
			if plugin == "" {
				plugin = "native"
			}
			fmt.Printf("    %-25s plugin=%s\n", mod.Name(), plugin)
		}
	}

	dcComps := dc.Components()
	if len(dcComps) > 0 {
		fmt.Printf("\n  Component Declarations (%d):\n", len(dcComps))
		for _, comp := range dcComps {
			fmt.Printf("    %-25s source=%s\n", comp.Name(), comp.Source())
		}
	}

	env := dc.Environment()
	if env != nil {
		hooks := env.Hooks()
		if hooks != nil {
			printHookSummary("database", hooks.Database())
			printHookSummary("bucket", hooks.Bucket())
			printHookSummary("deployment", hooks.Deployment())
			printHookSummary("function", hooks.Function())
			printHookSummary("service", hooks.Service())
			printHookSummary("route", hooks.Route())
			printHookSummary("cronjob", hooks.Cronjob())
			printHookSummary("encryptionKey", hooks.EncryptionKey())
			printHookSummary("smtp", hooks.SMTP())
			printHookSummary("dockerBuild", hooks.DockerBuild())
			printHookSummary("observability", hooks.Observability())
			printHookSummary("port", hooks.Port())
			printHookSummary("task", hooks.Task())
		}

		envMods := env.Modules()
		if len(envMods) > 0 {
			fmt.Printf("\n  Environment Modules (%d):\n", len(envMods))
			for _, mod := range envMods {
				plugin := mod.Plugin()
				if plugin == "" {
					plugin = "native"
				}
				fmt.Printf("    %-25s plugin=%s\n", mod.Name(), plugin)
			}
		}
	}

	fmt.Println()
	return nil
}

func printHookSummary(hookType string, hooks []datacenter.Hook) {
	if len(hooks) == 0 {
		return
	}

	fmt.Printf("\n  Hook: %s (%d variant(s)):\n", hookType, len(hooks))
	for _, hook := range hooks {
		when := hook.When()
		if when == "" {
			when = "(catch-all)"
		}
		if hook.Error() != "" {
			fmt.Printf("    when: %-30s  -> ERROR: %s\n", when, hook.Error())
			continue
		}
		for _, mod := range hook.Modules() {
			plugin := mod.Plugin()
			if plugin == "" {
				plugin = "native"
			}
			fmt.Printf("    when: %-30s  -> module %q (plugin=%s)\n", when, mod.Name(), plugin)
		}
	}
}

// printDatacenterModuleAddresses lists IaC resource addresses for each module.
func printDatacenterModuleAddresses(dc datacenter.Datacenter) error {
	dcDir := filepath.Dir(dc.SourcePath())

	fmt.Println()
	fmt.Println("IaC Resource Addresses")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println()
	fmt.Println("Use these addresses in import mapping files (--import-file)")
	fmt.Println("or --map flags to map existing cloud resources to IaC state.")

	rootMods := dc.Modules()
	if len(rootMods) > 0 {
		fmt.Println()
		fmt.Println("Root Modules")
		fmt.Println(strings.Repeat("-", 60))

		for _, mod := range rootMods {
			printModuleAddresses(mod, dcDir)
		}
	}

	env := dc.Environment()
	if env != nil {
		hooks := env.Hooks()
		if hooks != nil {
			printHookModuleAddresses("database", hooks.Database(), dcDir)
			printHookModuleAddresses("bucket", hooks.Bucket(), dcDir)
			printHookModuleAddresses("deployment", hooks.Deployment(), dcDir)
			printHookModuleAddresses("function", hooks.Function(), dcDir)
			printHookModuleAddresses("service", hooks.Service(), dcDir)
			printHookModuleAddresses("route", hooks.Route(), dcDir)
			printHookModuleAddresses("cronjob", hooks.Cronjob(), dcDir)
			printHookModuleAddresses("encryptionKey", hooks.EncryptionKey(), dcDir)
			printHookModuleAddresses("smtp", hooks.SMTP(), dcDir)
			printHookModuleAddresses("dockerBuild", hooks.DockerBuild(), dcDir)
			printHookModuleAddresses("observability", hooks.Observability(), dcDir)
			printHookModuleAddresses("task", hooks.Task(), dcDir)
		}

		envMods := env.Modules()
		if len(envMods) > 0 {
			fmt.Println()
			fmt.Println("Environment Modules")
			fmt.Println(strings.Repeat("-", 60))
			for _, mod := range envMods {
				printModuleAddresses(mod, dcDir)
			}
		}
	}

	fmt.Println()
	return nil
}

func printHookModuleAddresses(hookType string, hooks []datacenter.Hook, dcDir string) {
	if len(hooks) == 0 {
		return
	}

	fmt.Println()
	fmt.Printf("Hook: %s\n", hookType)
	fmt.Println(strings.Repeat("-", 60))

	for _, hook := range hooks {
		if hook.Error() != "" {
			continue
		}

		when := hook.When()
		if when != "" {
			fmt.Printf("  when: %s\n", when)
		}

		for _, mod := range hook.Modules() {
			printModuleAddresses(mod, dcDir)
		}
	}
}

func printModuleAddresses(mod datacenter.Module, dcDir string) {
	plugin := mod.Plugin()
	if plugin == "" {
		plugin = "native"
	}

	modPath := mod.Build()
	if modPath == "" {
		modPath = mod.Source()
	}
	if modPath != "" && !filepath.IsAbs(modPath) {
		modPath = filepath.Join(dcDir, modPath)
	}

	fmt.Printf("\n  module %q (plugin=%s)\n", mod.Name(), plugin)

	if modPath == "" {
		fmt.Println("    (no source path — addresses not discoverable)")
		return
	}

	switch plugin {
	case "native":
		discoverNativeAddresses(modPath)
	case "opentofu", "terraform":
		discoverTerraformAddresses(modPath)
	default:
		fmt.Printf("    source: %s\n", modPath)
		fmt.Println("    (address discovery not supported for this plugin type)")
	}
}

func discoverNativeAddresses(modPath string) {
	module, err := nativepkg.LoadModule(modPath)
	if err != nil {
		fmt.Printf("    (could not load module: %v)\n", err)
		return
	}

	if len(module.Resources) == 0 {
		fmt.Println("    (no resources defined)")
		return
	}

	fmt.Println("    Resources:")
	names := make([]string, 0, len(module.Resources))
	for name := range module.Resources {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		res := module.Resources[name]
		fmt.Printf("      %-30s type=%s\n", name, res.Type)
	}
}

func discoverTerraformAddresses(modPath string) {
	entries, err := os.ReadDir(modPath)
	if err != nil {
		fmt.Printf("    (could not read module directory: %v)\n", err)
		return
	}

	type tfResource struct {
		typeName string
		name     string
	}

	var resources []tfResource

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tf") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(modPath, entry.Name()))
		if err != nil {
			continue
		}

		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "resource ") {
				continue
			}

			parts := strings.Fields(line)
			if len(parts) >= 3 {
				typeName := strings.Trim(parts[1], "\"")
				resName := strings.Trim(parts[2], "\"")
				resources = append(resources, tfResource{typeName, resName})
			}
		}
	}

	if len(resources) == 0 {
		fmt.Println("    (no Terraform resources found)")
		return
	}

	fmt.Println("    Resources (use as --map addresses):")
	for _, res := range resources {
		fmt.Printf("      %s.%s\n", res.typeName, res.name)
	}
}
