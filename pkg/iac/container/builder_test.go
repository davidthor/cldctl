package container

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratePulumiDockerfile_NodeJS(t *testing.T) {
	// Create temp directory with Pulumi Node.js module
	tmpDir, err := os.MkdirTemp("", "pulumi-nodejs-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write Pulumi.yaml
	pulumiYaml := `name: test-module
runtime: nodejs
`
	if err := os.WriteFile(filepath.Join(tmpDir, "Pulumi.yaml"), []byte(pulumiYaml), 0644); err != nil {
		t.Fatalf("Failed to write Pulumi.yaml: %v", err)
	}

	// Write package.json
	packageJson := `{"name": "test-module", "dependencies": {}}`
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJson), 0644); err != nil {
		t.Fatalf("Failed to write package.json: %v", err)
	}

	dockerfile, err := generatePulumiDockerfile(tmpDir)
	if err != nil {
		t.Fatalf("generatePulumiDockerfile failed: %v", err)
	}

	// Verify Dockerfile content
	if !strings.Contains(dockerfile, "pulumi/pulumi-nodejs") {
		t.Error("Expected nodejs base image")
	}
	if !strings.Contains(dockerfile, "npm ci") {
		t.Error("Expected npm install for package.json")
	}
}

func TestGeneratePulumiDockerfile_Python(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pulumi-python-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pulumiYaml := `name: test-module
runtime: python
`
	if err := os.WriteFile(filepath.Join(tmpDir, "Pulumi.yaml"), []byte(pulumiYaml), 0644); err != nil {
		t.Fatalf("Failed to write Pulumi.yaml: %v", err)
	}

	// Write requirements.txt
	requirements := `pulumi>=3.0.0
pulumi-aws>=6.0.0
`
	if err := os.WriteFile(filepath.Join(tmpDir, "requirements.txt"), []byte(requirements), 0644); err != nil {
		t.Fatalf("Failed to write requirements.txt: %v", err)
	}

	dockerfile, err := generatePulumiDockerfile(tmpDir)
	if err != nil {
		t.Fatalf("generatePulumiDockerfile failed: %v", err)
	}

	if !strings.Contains(dockerfile, "pulumi/pulumi-python") {
		t.Error("Expected python base image")
	}
	if !strings.Contains(dockerfile, "pip install") {
		t.Error("Expected pip install for requirements.txt")
	}
}

func TestGeneratePulumiDockerfile_Go(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pulumi-go-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	pulumiYaml := `name: test-module
runtime: go
`
	if err := os.WriteFile(filepath.Join(tmpDir, "Pulumi.yaml"), []byte(pulumiYaml), 0644); err != nil {
		t.Fatalf("Failed to write Pulumi.yaml: %v", err)
	}

	// Write go.mod
	goMod := `module test-module

go 1.21
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}

	dockerfile, err := generatePulumiDockerfile(tmpDir)
	if err != nil {
		t.Fatalf("generatePulumiDockerfile failed: %v", err)
	}

	if !strings.Contains(dockerfile, "pulumi/pulumi-go") {
		t.Error("Expected go base image")
	}
	if !strings.Contains(dockerfile, "go mod download") {
		t.Error("Expected go mod download")
	}
}

func TestGenerateOpenTofuDockerfile(t *testing.T) {
	// Test without base image (fallback: downloads from scratch)
	dockerfile, err := generateOpenTofuDockerfile("")
	if err != nil {
		t.Fatalf("generateOpenTofuDockerfile failed: %v", err)
	}

	if !strings.Contains(dockerfile, "opentofu/opentofu") {
		t.Error("Expected opentofu base image reference")
	}
	if !strings.Contains(dockerfile, "tofu init") {
		t.Error("Expected tofu init")
	}

	// Test with base image (fast path: extends provider base)
	dockerfileWithBase, err := generateOpenTofuDockerfile("my-provider-base:latest")
	if err != nil {
		t.Fatalf("generateOpenTofuDockerfile with base failed: %v", err)
	}

	if !strings.Contains(dockerfileWithBase, "FROM my-provider-base:latest") {
		t.Error("Expected FROM base image")
	}
	if !strings.Contains(dockerfileWithBase, "tofu init") {
		t.Error("Expected tofu init")
	}
}

func TestGenerateDockerfile_UnsupportedType(t *testing.T) {
	_, err := generateDockerfile("unsupported", "/tmp", "")
	if err == nil {
		t.Error("Expected error for unsupported module type")
	}
}

func TestFileExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "file-exists-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	existingFile := filepath.Join(tmpDir, "exists.txt")
	if err := os.WriteFile(existingFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	if !fileExists(existingFile) {
		t.Error("fileExists returned false for existing file")
	}

	if fileExists(filepath.Join(tmpDir, "nonexistent.txt")) {
		t.Error("fileExists returned true for nonexistent file")
	}
}

func TestCreateBuildContext(t *testing.T) {
	// Create temp directory with some files
	tmpDir, err := os.MkdirTemp("", "build-context-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create files
	if err := os.WriteFile(filepath.Join(tmpDir, "main.tf"), []byte("resource {}"), 0644); err != nil {
		t.Fatalf("Failed to create main.tf: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "variables.tf"), []byte("variable {}"), 0644); err != nil {
		t.Fatalf("Failed to create variables.tf: %v", err)
	}

	// Create a hidden file that should be skipped
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte("*.bak"), 0644); err != nil {
		t.Fatalf("Failed to create .gitignore: %v", err)
	}

	// Create node_modules directory that should be skipped
	nodeModules := filepath.Join(tmpDir, "node_modules")
	if err := os.MkdirAll(nodeModules, 0755); err != nil {
		t.Fatalf("Failed to create node_modules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nodeModules, "test.js"), []byte(""), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	dockerfile := "FROM alpine\nRUN echo hello"
	reader, err := createBuildContext(tmpDir, dockerfile)
	if err != nil {
		t.Fatalf("createBuildContext failed: %v", err)
	}

	// Verify we got a reader
	if reader == nil {
		t.Fatal("createBuildContext returned nil reader")
	}
}
