package inference

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// PackageJSON represents the structure of a package.json file.
type PackageJSON struct {
	Name            string            `json:"name"`
	Main            string            `json:"main"`
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	PackageManager  string            `json:"packageManager"` // e.g., "pnpm@8.0.0"
	Engines         struct {
		Node string `json:"node"`
	} `json:"engines"`
}

// JavaScriptDetector detects JavaScript projects.
type JavaScriptDetector struct{}

func (d *JavaScriptDetector) Name() string  { return "javascript" }
func (d *JavaScriptDetector) Priority() int { return 50 } // Lower priority than TypeScript

func (d *JavaScriptDetector) Detect(projectPath string) bool {
	// Check for package.json without tsconfig.json
	hasPackageJSON := FileExistsInProject(projectPath, "package.json")
	hasTsConfig := FileExistsInProject(projectPath, "tsconfig.json")
	return hasPackageJSON && !hasTsConfig
}

// TypeScriptDetector detects TypeScript projects.
type TypeScriptDetector struct{}

func (d *TypeScriptDetector) Name() string  { return "typescript" }
func (d *TypeScriptDetector) Priority() int { return 100 } // Higher priority - check before JavaScript

func (d *TypeScriptDetector) Detect(projectPath string) bool {
	// Check for package.json WITH tsconfig.json
	hasPackageJSON := FileExistsInProject(projectPath, "package.json")
	hasTsConfig := FileExistsInProject(projectPath, "tsconfig.json")
	return hasPackageJSON && hasTsConfig
}

// JavaScriptInferrer infers configuration from JavaScript/TypeScript projects.
// Used for both JavaScript and TypeScript since they share package.json.
type JavaScriptInferrer struct{}

func (i *JavaScriptInferrer) Language() string { return "javascript" }

func (i *JavaScriptInferrer) Infer(projectPath string) (*ProjectInfo, error) {
	info := &ProjectInfo{
		Dependencies:    make(map[string]string),
		DevDependencies: make(map[string]string),
		Scripts:         make(map[string]string),
	}

	// Parse package.json
	pkgPath := filepath.Join(projectPath, "package.json")
	pkg, err := parsePackageJSON(pkgPath)
	if err != nil {
		return info, nil // Return empty info if can't parse
	}

	// Copy dependencies
	for k, v := range pkg.Dependencies {
		info.Dependencies[k] = v
	}
	for k, v := range pkg.DevDependencies {
		info.DevDependencies[k] = v
	}
	for k, v := range pkg.Scripts {
		info.Scripts[k] = v
	}

	// Detect TypeScript
	if FileExistsInProject(projectPath, "tsconfig.json") {
		info.Language = "typescript"
	} else {
		info.Language = "javascript"
	}

	// Detect package manager (pass pkg to check packageManager field first)
	info.PackageManager = detectJSPackageManager(projectPath, pkg)
	info.InstallCommand = getInstallCommand(info.PackageManager)

	// Extract commands from scripts
	if _, ok := pkg.Scripts["dev"]; ok {
		info.DevCommand = runScript(info.PackageManager, "dev")
	}
	if _, ok := pkg.Scripts["build"]; ok {
		info.BuildCommand = runScript(info.PackageManager, "build")
	}
	if _, ok := pkg.Scripts["start"]; ok {
		info.StartCommand = runScript(info.PackageManager, "start")
	}

	// Extract runtime version from engines
	if pkg.Engines.Node != "" {
		info.Runtime = parseNodeRuntime(pkg.Engines.Node)
	}

	// Entry point
	if pkg.Main != "" {
		info.Entry = pkg.Main
	}

	return info, nil
}

// TypeScriptInferrer is an alias for JavaScriptInferrer since they share package.json.
type TypeScriptInferrer struct {
	JavaScriptInferrer
}

func (i *TypeScriptInferrer) Language() string { return "typescript" }

// parsePackageJSON reads and parses a package.json file.
func parsePackageJSON(path string) (*PackageJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var pkg PackageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}

	return &pkg, nil
}

// detectJSPackageManager detects which JS package manager is used.
// It checks in order of priority:
// 1. packageManager field in package.json (Corepack standard)
// 2. Lock files (pnpm-lock.yaml, yarn.lock, bun.lockb, package-lock.json)
// 3. Default to npm
func detectJSPackageManager(projectPath string, pkg *PackageJSON) string {
	// First, check the packageManager field in package.json
	// Format is typically "pnpm@8.0.0", "yarn@4.0.0", etc.
	if pkg != nil && pkg.PackageManager != "" {
		pm := strings.ToLower(pkg.PackageManager)
		switch {
		case strings.HasPrefix(pm, "pnpm"):
			return "pnpm"
		case strings.HasPrefix(pm, "yarn"):
			return "yarn"
		case strings.HasPrefix(pm, "bun"):
			return "bun"
		case strings.HasPrefix(pm, "npm"):
			return "npm"
		}
	}

	// Fall back to lock file detection
	switch {
	case FileExistsInProject(projectPath, "pnpm-lock.yaml"):
		return "pnpm"
	case FileExistsInProject(projectPath, "yarn.lock"):
		return "yarn"
	case FileExistsInProject(projectPath, "bun.lockb"):
		return "bun"
	default:
		return "npm"
	}
}

// getInstallCommand returns the install command for a package manager.
func getInstallCommand(pm string) string {
	switch pm {
	case "pnpm":
		return "pnpm install"
	case "yarn":
		return "yarn install"
	case "bun":
		return "bun install"
	default:
		return "npm install"
	}
}

// runScript returns the command to run a script with the given package manager.
func runScript(pm, script string) string {
	switch pm {
	case "pnpm":
		return "pnpm run " + script
	case "yarn":
		return "yarn " + script
	case "bun":
		return "bun run " + script
	default:
		return "npm run " + script
	}
}

// parseNodeRuntime converts a Node.js version constraint to a Lambda-style runtime string.
func parseNodeRuntime(version string) string {
	// Extract major version number from constraints like ">=18", "^20", "20.x", etc.
	re := regexp.MustCompile(`(\d+)`)
	matches := re.FindStringSubmatch(version)
	if len(matches) > 1 {
		major := matches[1]
		return "nodejs" + major + ".x"
	}
	return ""
}

// JS Framework Detectors

// NextJSDetector detects Next.js projects.
type NextJSDetector struct{}

func (d *NextJSDetector) Language() string { return "typescript" } // Also works for javascript
func (d *NextJSDetector) Name() string     { return "nextjs" }

func (d *NextJSDetector) Detect(projectPath string, info *ProjectInfo) bool {
	// Check for next.config.js/mjs/ts or next in dependencies
	if AnyFileExists(projectPath, "next.config.js", "next.config.mjs", "next.config.ts") {
		return true
	}
	allDeps := MergeDeps(info.Dependencies, info.DevDependencies)
	return HasDependency(allDeps, "next")
}

func (d *NextJSDetector) Defaults() *FrameworkDefaults {
	return &FrameworkDefaults{
		Framework: "nextjs",
		Language:  "typescript",
		Dev:       "next dev",
		Build:     "next build",
		Start:     "next start",
		Port:      3000,
	}
}

// ExpressDetector detects Express.js projects.
type ExpressDetector struct{}

func (d *ExpressDetector) Language() string { return "javascript" }
func (d *ExpressDetector) Name() string     { return "express" }

func (d *ExpressDetector) Detect(projectPath string, info *ProjectInfo) bool {
	allDeps := MergeDeps(info.Dependencies, info.DevDependencies)
	return HasDependency(allDeps, "express")
}

func (d *ExpressDetector) Defaults() *FrameworkDefaults {
	return &FrameworkDefaults{
		Framework: "express",
		Language:  "javascript",
		Dev:       "node --watch src/index.js",
		Start:     "node src/index.js",
		Port:      3000,
	}
}

// NuxtDetector detects Nuxt.js projects.
type NuxtDetector struct{}

func (d *NuxtDetector) Language() string { return "typescript" }
func (d *NuxtDetector) Name() string     { return "nuxt" }

func (d *NuxtDetector) Detect(projectPath string, info *ProjectInfo) bool {
	if AnyFileExists(projectPath, "nuxt.config.js", "nuxt.config.ts") {
		return true
	}
	allDeps := MergeDeps(info.Dependencies, info.DevDependencies)
	return HasDependency(allDeps, "nuxt")
}

func (d *NuxtDetector) Defaults() *FrameworkDefaults {
	return &FrameworkDefaults{
		Framework: "nuxt",
		Language:  "typescript",
		Dev:       "nuxt dev",
		Build:     "nuxt build",
		Start:     "nuxt start",
		Port:      3000,
	}
}

// RemixDetector detects Remix projects.
type RemixDetector struct{}

func (d *RemixDetector) Language() string { return "typescript" }
func (d *RemixDetector) Name() string     { return "remix" }

func (d *RemixDetector) Detect(projectPath string, info *ProjectInfo) bool {
	allDeps := MergeDeps(info.Dependencies, info.DevDependencies)
	return HasAnyDependency(allDeps, "@remix-run/node", "@remix-run/react", "@remix-run/dev")
}

func (d *RemixDetector) Defaults() *FrameworkDefaults {
	return &FrameworkDefaults{
		Framework: "remix",
		Language:  "typescript",
		Dev:       "remix dev",
		Build:     "remix build",
		Start:     "remix-serve build",
		Port:      3000,
	}
}

// HonoDetector detects Hono projects.
type HonoDetector struct{}

func (d *HonoDetector) Language() string { return "typescript" }
func (d *HonoDetector) Name() string     { return "hono" }

func (d *HonoDetector) Detect(projectPath string, info *ProjectInfo) bool {
	allDeps := MergeDeps(info.Dependencies, info.DevDependencies)
	return HasDependency(allDeps, "hono")
}

func (d *HonoDetector) Defaults() *FrameworkDefaults {
	return &FrameworkDefaults{
		Framework: "hono",
		Language:  "typescript",
		Port:      3000,
	}
}

// NestJSDetector detects NestJS projects.
type NestJSDetector struct{}

func (d *NestJSDetector) Language() string { return "typescript" }
func (d *NestJSDetector) Name() string     { return "nestjs" }

func (d *NestJSDetector) Detect(projectPath string, info *ProjectInfo) bool {
	allDeps := MergeDeps(info.Dependencies, info.DevDependencies)
	return HasDependency(allDeps, "@nestjs/core")
}

func (d *NestJSDetector) Defaults() *FrameworkDefaults {
	return &FrameworkDefaults{
		Framework: "nestjs",
		Language:  "typescript",
		Dev:       "nest start --watch",
		Build:     "nest build",
		Start:     "node dist/main",
		Port:      3000,
	}
}

// RegisterJavaScript registers JavaScript/TypeScript support with the registry.
func RegisterJavaScript(r *Registry) {
	// Language detectors and inferrers
	r.RegisterLanguage(&TypeScriptDetector{}, &TypeScriptInferrer{})
	r.RegisterLanguage(&JavaScriptDetector{}, &JavaScriptInferrer{})

	// Framework detectors (for both JS and TS)
	for _, lang := range []string{"javascript", "typescript"} {
		r.frameworks[lang] = append(r.frameworks[lang],
			&NextJSDetector{},
			&NuxtDetector{},
			&RemixDetector{},
			&ExpressDetector{},
			&HonoDetector{},
			&NestJSDetector{},
		)
	}
}
