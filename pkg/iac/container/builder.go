package container

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/client"
)

// Builder builds container images for IaC modules.
type Builder struct {
	dockerClient *client.Client
}

// NewBuilder creates a new module builder.
func NewBuilder() (*Builder, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &Builder{dockerClient: cli}, nil
}

// BuildOptions configures module image building.
type BuildOptions struct {
	// ModuleDir is the directory containing the IaC module
	ModuleDir string

	// ModuleType is the IaC framework (auto-detected if empty)
	ModuleType ModuleType

	// Tag is the image tag
	Tag string

	// Output for build logs
	Output io.Writer

	// BaseImage is an optional pre-built base image (with providers pre-downloaded).
	// Used by OpenTofu modules to avoid re-downloading providers for every module.
	BaseImage string
}

// BuildResult contains the result of a module build.
type BuildResult struct {
	// Image is the built image tag
	Image string

	// Digest is the image digest
	Digest string

	// ModuleType is the detected/specified module type
	ModuleType ModuleType
}

// Build builds a container image for an IaC module.
func (b *Builder) Build(ctx context.Context, opts BuildOptions) (*BuildResult, error) {
	// Detect module type if not specified
	moduleType := opts.ModuleType
	if moduleType == "" {
		detected, err := DetectModuleType(opts.ModuleDir)
		if err != nil {
			return nil, fmt.Errorf("failed to detect module type: %w", err)
		}
		moduleType = detected
	}

	// Generate Dockerfile
	dockerfile, err := generateDockerfile(moduleType, opts.ModuleDir, opts.BaseImage)
	if err != nil {
		return nil, fmt.Errorf("failed to generate dockerfile: %w", err)
	}

	// Create build context (tar archive)
	buildContext, err := createBuildContext(opts.ModuleDir, dockerfile)
	if err != nil {
		return nil, fmt.Errorf("failed to create build context: %w", err)
	}

	// Build the image
	result, err := b.buildImage(ctx, buildContext, opts.Tag, opts.Output)
	if err != nil {
		return nil, err
	}

	return &BuildResult{
		Image:      opts.Tag,
		Digest:     result.id,
		ModuleType: moduleType,
	}, nil
}

// BuildProviderBase builds a Docker base image containing OpenTofu with all
// providers pre-downloaded. Each module directory is temporarily copied in
// and initialized, populating a shared TF_PLUGIN_CACHE_DIR. This means
// providers are only downloaded once across all modules in a template.
func (b *Builder) BuildProviderBase(ctx context.Context, moduleDirectories map[string]string, tag string, output io.Writer) error {
	// Generate Dockerfile
	var df strings.Builder
	df.WriteString(`FROM ghcr.io/opentofu/opentofu:minimal AS tofu

FROM alpine:3.20

COPY --from=tofu /usr/local/bin/tofu /usr/local/bin/tofu
RUN apk add --no-cache git curl ca-certificates

ENV TF_PLUGIN_CACHE_DIR=/usr/share/tofu/providers
RUN mkdir -p $TF_PLUGIN_CACHE_DIR

# Copy all module source files
COPY modules/ /tmp/modules/

# Initialize each module to populate the shared provider cache, then clean up
RUN set -e; for dir in /tmp/modules/*/; do \
      echo "--- Caching providers for $(basename "$dir") ---"; \
      cd "$dir" && tofu init -backend=false 2>&1 && cd /; \
    done && rm -rf /tmp/modules
`)

	dockerfileBytes := []byte(df.String())

	// Build tar context: Dockerfile + modules/<name>/<files>
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Add Dockerfile
	if err := writeTarFile(tw, "Dockerfile", dockerfileBytes); err != nil {
		return fmt.Errorf("failed to write dockerfile to tar: %w", err)
	}

	// Add each module directory under modules/<name>/
	for name, dir := range moduleDirectories {
		prefix := filepath.Join("modules", name)
		if err := addDirToTar(tw, dir, prefix); err != nil {
			return fmt.Errorf("failed to add module %s to build context: %w", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("failed to close tar: %w", err)
	}

	_, err := b.buildImage(ctx, &buf, tag, output)
	return err
}

// buildImageResult holds metadata returned by buildImage.
type buildImageResult struct {
	id string
}

// buildImage is the shared Docker image build routine. It streams the Docker
// JSON output, captures the last N lines for error context, and returns the
// image digest on success.
func (b *Builder) buildImage(ctx context.Context, buildContext io.Reader, tag string, output io.Writer) (*buildImageResult, error) {
	buildResp, err := b.dockerClient.ImageBuild(ctx, buildContext, build.ImageBuildOptions{
		Tags:       []string{tag},
		Dockerfile: "Dockerfile",
		Remove:     true,
		Platform:   "linux/amd64",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to build image: %w", err)
	}
	defer buildResp.Body.Close()

	if output == nil {
		output = io.Discard
	}

	// Keep a rolling buffer of recent stream lines for error context
	const contextLines = 25
	recentLines := make([]string, 0, contextLines)

	decoder := json.NewDecoder(buildResp.Body)
	var msg struct {
		Stream string `json:"stream"`
		Error  string `json:"error"`
		Aux    struct {
			ID string `json:"ID"`
		} `json:"aux"`
	}

	for {
		if err := decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to decode build output: %w", err)
		}

		if msg.Stream != "" {
			fmt.Fprint(output, msg.Stream)
			line := strings.TrimRight(msg.Stream, "\n")
			if line != "" {
				if len(recentLines) >= contextLines {
					recentLines = recentLines[1:]
				}
				recentLines = append(recentLines, line)
			}
		}

		if msg.Error != "" {
			context := strings.Join(recentLines, "\n")
			return nil, fmt.Errorf("build error: %s\n\nBuild output (last %d lines):\n%s",
				msg.Error, len(recentLines), context)
		}
	}

	return &buildImageResult{id: msg.Aux.ID}, nil
}

// Close releases resources.
func (b *Builder) Close() error {
	return b.dockerClient.Close()
}

// generateDockerfile generates a Dockerfile for the given module type.
func generateDockerfile(moduleType ModuleType, moduleDir string, baseImage string) (string, error) {
	switch moduleType {
	case ModuleTypePulumi:
		return generatePulumiDockerfile(moduleDir)
	case ModuleTypeOpenTofu:
		return generateOpenTofuDockerfile(baseImage)
	default:
		return "", fmt.Errorf("unsupported module type: %s", moduleType)
	}
}

// generatePulumiDockerfile generates a Dockerfile for a Pulumi module.
func generatePulumiDockerfile(moduleDir string) (string, error) {
	// Read Pulumi.yaml to detect runtime
	pulumiYaml := filepath.Join(moduleDir, "Pulumi.yaml")
	data, err := os.ReadFile(pulumiYaml)
	if err != nil {
		return "", fmt.Errorf("failed to read Pulumi.yaml: %w", err)
	}

	// Simple runtime detection from Pulumi.yaml
	runtime := "nodejs" // default
	content := string(data)
	if strings.Contains(content, "runtime: python") || strings.Contains(content, "runtime:\n  name: python") {
		runtime = "python"
	} else if strings.Contains(content, "runtime: go") || strings.Contains(content, "runtime:\n  name: go") {
		runtime = "go"
	} else if strings.Contains(content, "runtime: dotnet") || strings.Contains(content, "runtime:\n  name: dotnet") {
		runtime = "dotnet"
	}

	// Check if package.json or requirements.txt exists
	hasPackageJson := fileExists(filepath.Join(moduleDir, "package.json"))
	hasRequirements := fileExists(filepath.Join(moduleDir, "requirements.txt"))
	hasGoMod := fileExists(filepath.Join(moduleDir, "go.mod"))

	var dockerfile strings.Builder

	dockerfile.WriteString(`# Auto-generated Dockerfile for Pulumi module
# This image bundles the Pulumi CLI with the module code

`)

	switch runtime {
	case "nodejs":
		dockerfile.WriteString(`FROM pulumi/pulumi-nodejs:latest

WORKDIR /app

# Copy module files
COPY . .

`)
		if hasPackageJson {
			dockerfile.WriteString(`# Install dependencies
RUN npm ci --production
`)
		}

	case "python":
		dockerfile.WriteString(`FROM pulumi/pulumi-python:latest

WORKDIR /app

# Copy module files
COPY . .

`)
		if hasRequirements {
			dockerfile.WriteString(`# Install dependencies
RUN pip install -r requirements.txt
`)
		}

	case "go":
		dockerfile.WriteString(`FROM pulumi/pulumi-go:latest

WORKDIR /app

# Copy module files
COPY . .

`)
		if hasGoMod {
			dockerfile.WriteString(`# Download dependencies
RUN go mod download

# Build the module
RUN go build -o /app/module .
`)
		}

	case "dotnet":
		dockerfile.WriteString(`FROM pulumi/pulumi-dotnet:latest

WORKDIR /app

# Copy module files
COPY . .

# Restore dependencies
RUN dotnet restore
`)
	}

	// Set entrypoint to the Pulumi CLI
	dockerfile.WriteString(`
ENTRYPOINT ["pulumi"]
`)

	return dockerfile.String(), nil
}

// generateOpenTofuDockerfile generates a Dockerfile for an OpenTofu module.
// If baseImage is provided, the module extends the pre-built provider base
// (which already has tofu + all providers cached). Otherwise it builds from
// scratch using a multi-stage build per OpenTofu 1.10+ requirements.
func generateOpenTofuDockerfile(baseImage string) (string, error) {
	if baseImage != "" {
		// Fast path: extend the provider base image (providers already cached)
		return fmt.Sprintf(`# Auto-generated Dockerfile for OpenTofu module
# Extends pre-built provider base image for fast builds

FROM %s

WORKDIR /app

# Copy module files
COPY . .

# Initialize (providers are found in TF_PLUGIN_CACHE_DIR — no download needed)
RUN tofu init -backend=false

ENTRYPOINT ["tofu"]
`, baseImage), nil
	}

	// Fallback: build from scratch (downloads providers from registry)
	return `# Auto-generated Dockerfile for OpenTofu module
# Uses multi-stage build per OpenTofu 1.10+ requirements

FROM ghcr.io/opentofu/opentofu:minimal AS tofu

FROM alpine:3.20

# Install the tofu binary from the minimal image
COPY --from=tofu /usr/local/bin/tofu /usr/local/bin/tofu

# Install common utilities needed by providers
RUN apk add --no-cache git curl ca-certificates

WORKDIR /app

# Copy module files
COPY . .

# Initialize the module (download providers and lock versions)
RUN tofu init -backend=false

ENTRYPOINT ["tofu"]
`, nil
}

// createBuildContext creates a tar archive for the Docker build context.
func createBuildContext(moduleDir string, dockerfile string) (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Add Dockerfile
	dockerfileBytes := []byte(dockerfile)
	if err := tw.WriteHeader(&tar.Header{
		Name: "Dockerfile",
		Mode: 0644,
		Size: int64(len(dockerfileBytes)),
	}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(dockerfileBytes); err != nil {
		return nil, err
	}

	// Walk the module directory and add files
	err := filepath.Walk(moduleDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden files/directories and common excludes
		name := info.Name()
		if strings.HasPrefix(name, ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip node_modules, __pycache__, etc.
		if info.IsDir() {
			if name == "node_modules" || name == "__pycache__" || name == ".terraform" || name == ".pulumi" {
				return filepath.SkipDir
			}
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(moduleDir, path)
		if err != nil {
			return err
		}

		// Read file
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Add to tar
		header := &tar.Header{
			Name: relPath,
			Mode: int64(info.Mode()),
			Size: int64(len(data)),
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if _, err := tw.Write(data); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}

	return &buf, nil
}

// writeTarFile writes a single file entry to a tar writer.
func writeTarFile(tw *tar.Writer, name string, data []byte) error {
	if err := tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0644,
		Size: int64(len(data)),
	}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// addDirToTar walks a directory and adds all files to a tar writer under
// the given prefix. Hidden files/directories and common build artifacts
// are excluded.
func addDirToTar(tw *tar.Writer, srcDir string, prefix string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		name := info.Name()
		if strings.HasPrefix(name, ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			if name == "node_modules" || name == "__pycache__" || name == ".terraform" || name == ".pulumi" {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return writeTarFile(tw, filepath.Join(prefix, relPath), data)
	})
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
