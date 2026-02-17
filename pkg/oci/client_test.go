package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient()
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
	if client.auth == nil {
		t.Error("NewClient() returned client with nil auth")
	}
}

func TestExtractTar(t *testing.T) {
	// Create a tar archive in memory
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Add a directory
	dirHeader := &tar.Header{
		Name:     "testdir/",
		Mode:     0755,
		Typeflag: tar.TypeDir,
	}
	if err := tw.WriteHeader(dirHeader); err != nil {
		t.Fatalf("Failed to write directory header: %v", err)
	}

	// Add a file
	content := []byte("Hello, World!")
	fileHeader := &tar.Header{
		Name:     "testdir/hello.txt",
		Mode:     0644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(fileHeader); err != nil {
		t.Fatalf("Failed to write file header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("Failed to write file content: %v", err)
	}

	// Add another file at root
	rootContent := []byte("Root file content")
	rootHeader := &tar.Header{
		Name:     "root.txt",
		Mode:     0644,
		Size:     int64(len(rootContent)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(rootHeader); err != nil {
		t.Fatalf("Failed to write root file header: %v", err)
	}
	if _, err := tw.Write(rootContent); err != nil {
		t.Fatalf("Failed to write root file content: %v", err)
	}

	tw.Close()

	// Create temp directory for extraction
	destDir, err := os.MkdirTemp("", "oci-test-extract-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(destDir)

	// Extract
	if err := extractTar(&buf, destDir); err != nil {
		t.Fatalf("extractTar failed: %v", err)
	}

	// Verify directory was created
	dirPath := filepath.Join(destDir, "testdir")
	info, err := os.Stat(dirPath)
	if err != nil {
		t.Errorf("Directory not created: %v", err)
	} else if !info.IsDir() {
		t.Errorf("Expected directory, got file")
	}

	// Verify file was created with correct content
	filePath := filepath.Join(destDir, "testdir", "hello.txt")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Errorf("File not created: %v", err)
	} else if string(data) != "Hello, World!" {
		t.Errorf("File content: got %q, want %q", string(data), "Hello, World!")
	}

	// Verify root file
	rootPath := filepath.Join(destDir, "root.txt")
	rootData, err := os.ReadFile(rootPath)
	if err != nil {
		t.Errorf("Root file not created: %v", err)
	} else if string(rootData) != "Root file content" {
		t.Errorf("Root file content: got %q, want %q", string(rootData), "Root file content")
	}
}

func TestExtractTarDirectoryTraversal(t *testing.T) {
	// Create a tar archive with a path traversal attempt
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Try to write a file outside the destination
	maliciousHeader := &tar.Header{
		Name:     "../../../etc/passwd",
		Mode:     0644,
		Size:     4,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(maliciousHeader); err != nil {
		t.Fatalf("Failed to write malicious header: %v", err)
	}
	if _, err := tw.Write([]byte("evil")); err != nil {
		t.Fatalf("Failed to write malicious content: %v", err)
	}
	tw.Close()

	// Create temp directory
	destDir, err := os.MkdirTemp("", "oci-test-traversal-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(destDir)

	// Extraction should fail
	err = extractTar(&buf, destDir)
	if err == nil {
		t.Error("extractTar should have failed for path traversal attempt")
	}
}

func TestCreateTarGz(t *testing.T) {
	// Create a source directory with files
	srcDir, err := os.MkdirTemp("", "oci-test-src-*")
	if err != nil {
		t.Fatalf("Failed to create source dir: %v", err)
	}
	defer os.RemoveAll(srcDir)

	// Create some files
	if err := os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("File 1 content"), 0644); err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}

	subDir := filepath.Join(srcDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(subDir, "file2.txt"), []byte("File 2 content"), 0644); err != nil {
		t.Fatalf("Failed to create file2: %v", err)
	}

	// Create tar.gz
	tarPath := filepath.Join(os.TempDir(), "oci-test-archive.tar.gz")
	defer os.Remove(tarPath)

	if err := createTarGz(srcDir, tarPath); err != nil {
		t.Fatalf("createTarGz failed: %v", err)
	}

	// Verify the archive exists and can be read
	f, err := os.Open(tarPath)
	if err != nil {
		t.Fatalf("Failed to open created archive: %v", err)
	}
	defer f.Close()

	// Read and decompress
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gr.Close()

	// Read tar contents
	tr := tar.NewReader(gr)
	files := make(map[string]bool)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to read tar: %v", err)
		}
		files[header.Name] = true
	}

	// Verify expected files are in the archive
	expectedFiles := []string{"file1.txt", "subdir", "subdir/file2.txt"}
	for _, expected := range expectedFiles {
		if !files[expected] {
			t.Errorf("Expected file %q not found in archive", expected)
		}
	}
}

func TestCreateTarGzAndExtract(t *testing.T) {
	// Create source directory
	srcDir, err := os.MkdirTemp("", "oci-test-roundtrip-src-*")
	if err != nil {
		t.Fatalf("Failed to create source dir: %v", err)
	}
	defer os.RemoveAll(srcDir)

	// Create test files
	testFiles := map[string]string{
		"config.yaml":       "name: test\nversion: v1",
		"src/main.go":       "package main\n\nfunc main() {}",
		"src/lib/helper.go": "package lib\n\nfunc Helper() {}",
		"docs/README.md":    "# Documentation",
	}

	for path, content := range testFiles {
		fullPath := filepath.Join(srcDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory for %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create %s: %v", path, err)
		}
	}

	// Create archive
	tarPath := filepath.Join(os.TempDir(), "oci-test-roundtrip.tar.gz")
	defer os.Remove(tarPath)

	if err := createTarGz(srcDir, tarPath); err != nil {
		t.Fatalf("createTarGz failed: %v", err)
	}

	// Extract to new directory
	destDir, err := os.MkdirTemp("", "oci-test-roundtrip-dest-*")
	if err != nil {
		t.Fatalf("Failed to create dest dir: %v", err)
	}
	defer os.RemoveAll(destDir)

	// Open and decompress
	f, err := os.Open(tarPath)
	if err != nil {
		t.Fatalf("Failed to open archive: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gr.Close()

	if err := extractTar(gr, destDir); err != nil {
		t.Fatalf("extractTar failed: %v", err)
	}

	// Verify all files were extracted correctly
	for path, expectedContent := range testFiles {
		fullPath := filepath.Join(destDir, path)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("Failed to read extracted %s: %v", path, err)
			continue
		}
		if string(data) != expectedContent {
			t.Errorf("Content mismatch for %s: got %q, want %q", path, string(data), expectedContent)
		}
	}
}

func TestBuildFromDirectory(t *testing.T) {
	// Create source directory
	srcDir, err := os.MkdirTemp("", "oci-test-build-*")
	if err != nil {
		t.Fatalf("Failed to create source dir: %v", err)
	}
	defer os.RemoveAll(srcDir)

	// Create test component structure
	if err := os.WriteFile(filepath.Join(srcDir, "cld.yml"), []byte("name: test-component\nversion: v1"), 0644); err != nil {
		t.Fatalf("Failed to create cld.yml: %v", err)
	}

	client := NewClient()

	config := ComponentConfig{
		SchemaVersion: "v1",
		Readme:        "# Test Component\n\nA test component for unit testing.",
	}

	artifact, err := client.BuildFromDirectory(context.TODO(), srcDir, ArtifactTypeComponent, config)
	if err != nil {
		t.Fatalf("BuildFromDirectory failed: %v", err)
	}

	// Verify artifact
	if artifact.Type != ArtifactTypeComponent {
		t.Errorf("Artifact type: got %q, want %q", artifact.Type, ArtifactTypeComponent)
	}

	if len(artifact.Layers) != 1 {
		t.Errorf("Expected 1 layer, got %d", len(artifact.Layers))
	}

	if len(artifact.Layers[0].Data) == 0 {
		t.Error("Layer data is empty")
	}

	// Verify config was serialized
	var parsedConfig ComponentConfig
	if err := json.Unmarshal(artifact.Config, &parsedConfig); err != nil {
		t.Errorf("Failed to parse config: %v", err)
	}
	if parsedConfig.SchemaVersion != "v1" {
		t.Errorf("Config schema version: got %q, want %q", parsedConfig.SchemaVersion, "v1")
	}
}

func TestBuildFromDirectoryDatacenter(t *testing.T) {
	srcDir, err := os.MkdirTemp("", "oci-test-build-dc-*")
	if err != nil {
		t.Fatalf("Failed to create source dir: %v", err)
	}
	defer os.RemoveAll(srcDir)

	if err := os.WriteFile(filepath.Join(srcDir, "datacenter.hcl"), []byte("# datacenter config"), 0644); err != nil {
		t.Fatalf("Failed to create datacenter.hcl: %v", err)
	}

	client := NewClient()

	config := DatacenterConfig{
		SchemaVersion: "v1",
		Name:          "test-datacenter",
	}

	artifact, err := client.BuildFromDirectory(context.TODO(), srcDir, ArtifactTypeDatacenter, config)
	if err != nil {
		t.Fatalf("BuildFromDirectory failed: %v", err)
	}

	if artifact.Type != ArtifactTypeDatacenter {
		t.Errorf("Artifact type: got %q, want %q", artifact.Type, ArtifactTypeDatacenter)
	}
}

func TestBuildFromDirectoryModule(t *testing.T) {
	srcDir, err := os.MkdirTemp("", "oci-test-build-mod-*")
	if err != nil {
		t.Fatalf("Failed to create source dir: %v", err)
	}
	defer os.RemoveAll(srcDir)

	if err := os.WriteFile(filepath.Join(srcDir, "module.yml"), []byte("# module config"), 0644); err != nil {
		t.Fatalf("Failed to create module.yml: %v", err)
	}

	client := NewClient()

	config := ModuleConfig{
		Plugin: "opentofu",
		Name:   "test-module",
		Inputs: map[string]string{
			"region": "string",
		},
		Outputs: map[string]string{
			"vpc_id": "string",
		},
	}

	artifact, err := client.BuildFromDirectory(context.TODO(), srcDir, ArtifactTypeModule, config)
	if err != nil {
		t.Fatalf("BuildFromDirectory failed: %v", err)
	}

	if artifact.Type != ArtifactTypeModule {
		t.Errorf("Artifact type: got %q, want %q", artifact.Type, ArtifactTypeModule)
	}

	var parsedConfig ModuleConfig
	if err := json.Unmarshal(artifact.Config, &parsedConfig); err != nil {
		t.Errorf("Failed to parse config: %v", err)
	}
	if parsedConfig.Plugin != "opentofu" {
		t.Errorf("Config plugin: got %q, want %q", parsedConfig.Plugin, "opentofu")
	}
}

func TestConfigStructs(t *testing.T) {
	t.Run("ComponentConfig JSON roundtrip", func(t *testing.T) {
		original := ComponentConfig{
			SchemaVersion:  "v1",
			Readme:         "# My Component\n\nA test component for CI/CD.",
			ChildArtifacts: map[string]string{"web": "ghcr.io/org/web:v1"},
			SourceHash:     "abc123",
			BuildTime:      "2024-01-01T00:00:00Z",
		}

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Failed to marshal: %v", err)
		}

		var parsed ComponentConfig
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if parsed.Readme != original.Readme {
			t.Errorf("Readme mismatch: got %q, want %q", parsed.Readme, original.Readme)
		}
		if parsed.ChildArtifacts["web"] != original.ChildArtifacts["web"] {
			t.Errorf("ChildArtifacts mismatch")
		}
	})

	t.Run("DatacenterConfig JSON roundtrip", func(t *testing.T) {
		original := DatacenterConfig{
			SchemaVersion:   "v1",
			Name:            "aws-datacenter",
			ModuleArtifacts: map[string]string{"vpc": "ghcr.io/org/vpc:v1"},
			SourceHash:      "def456",
			BuildTime:       "2024-01-01T00:00:00Z",
		}

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Failed to marshal: %v", err)
		}

		var parsed DatacenterConfig
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if parsed.Name != original.Name {
			t.Errorf("Name mismatch: got %q, want %q", parsed.Name, original.Name)
		}
	})

	t.Run("ModuleConfig JSON roundtrip", func(t *testing.T) {
		original := ModuleConfig{
			Plugin:     "pulumi",
			Name:       "database-module",
			Inputs:     map[string]string{"size": "string", "region": "string"},
			Outputs:    map[string]string{"endpoint": "string", "port": "number"},
			SourceHash: "ghi789",
			BuildTime:  "2024-01-01T00:00:00Z",
		}

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Failed to marshal: %v", err)
		}

		var parsed ModuleConfig
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("Failed to unmarshal: %v", err)
		}

		if parsed.Plugin != original.Plugin {
			t.Errorf("Plugin mismatch: got %q, want %q", parsed.Plugin, original.Plugin)
		}
		if len(parsed.Inputs) != len(original.Inputs) {
			t.Errorf("Inputs count mismatch: got %d, want %d", len(parsed.Inputs), len(original.Inputs))
		}
	})
}
