package resolver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectReferenceType(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		expected ReferenceType
	}{
		// Git references
		{
			name:     "git https reference",
			ref:      "git::https://github.com/org/repo.git",
			expected: ReferenceTypeGit,
		},
		{
			name:     "git https with subpath",
			ref:      "git::https://github.com/org/repo.git//components/api",
			expected: ReferenceTypeGit,
		},
		{
			name:     "git https with ref param",
			ref:      "git::https://github.com/org/repo.git//components/api?ref=v1.0.0",
			expected: ReferenceTypeGit,
		},
		{
			name:     "git ssh reference",
			ref:      "git::git@github.com:org/repo.git",
			expected: ReferenceTypeGit,
		},

		// Local path references (detected but rejected in validation)
		{
			name:     "relative path with ./",
			ref:      "./components/api",
			expected: ReferenceTypeLocal,
		},
		{
			name:     "relative path with ../",
			ref:      "../shared/component",
			expected: ReferenceTypeLocal,
		},
		{
			name:     "absolute path",
			ref:      "/home/user/components/api",
			expected: ReferenceTypeLocal,
		},
		{
			name:     "yml file extension",
			ref:      "component.yml",
			expected: ReferenceTypeLocal,
		},
		{
			name:     "yaml file extension",
			ref:      "cld.yaml",
			expected: ReferenceTypeLocal,
		},

		// OCI references
		{
			name:     "simple OCI reference",
			ref:      "ghcr.io/myorg/mycomponent:v1.0.0",
			expected: ReferenceTypeOCI,
		},
		{
			name:     "docker.io reference",
			ref:      "myorg/myapp:latest",
			expected: ReferenceTypeOCI,
		},
		{
			name:     "OCI with digest",
			ref:      "ghcr.io/org/app@sha256:abc123",
			expected: ReferenceTypeOCI,
		},
		{
			name:     "registry with port",
			ref:      "localhost:5000/myapp:v1",
			expected: ReferenceTypeOCI,
		},
		// Simple names now default to OCI (not local)
		{
			name:     "simple name defaults to OCI",
			ref:      "mycomponent",
			expected: ReferenceTypeOCI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectReferenceType(tt.ref)
			if result != tt.expected {
				t.Errorf("DetectReferenceType(%q): got %q, want %q", tt.ref, result, tt.expected)
			}
		})
	}
}

func TestDetectReferenceTypeWithLocal(t *testing.T) {
	// Create a temporary directory to test local existence detection
	tmpDir, err := os.MkdirTemp("", "detect-ref-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a test directory
	testDir := filepath.Join(tmpDir, "mycomponent")
	if err := os.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test dir: %v", err)
	}

	// Change to temp dir for the test
	originalDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(originalDir) }()

	t.Run("existing local directory", func(t *testing.T) {
		result := DetectReferenceTypeWithLocal("mycomponent")
		if result != ReferenceTypeLocal {
			t.Errorf("DetectReferenceTypeWithLocal(%q): got %q, want %q", "mycomponent", result, ReferenceTypeLocal)
		}
	})

	t.Run("non-existing path defaults to OCI", func(t *testing.T) {
		result := DetectReferenceTypeWithLocal("nonexistent")
		if result != ReferenceTypeOCI {
			t.Errorf("DetectReferenceTypeWithLocal(%q): got %q, want %q", "nonexistent", result, ReferenceTypeOCI)
		}
	})

	t.Run("explicit path prefix still detected", func(t *testing.T) {
		result := DetectReferenceTypeWithLocal("./mycomponent")
		if result != ReferenceTypeLocal {
			t.Errorf("DetectReferenceTypeWithLocal(%q): got %q, want %q", "./mycomponent", result, ReferenceTypeLocal)
		}
	})
}

func TestReferenceTypeConstants(t *testing.T) {
	if ReferenceTypeLocal != "local" {
		t.Errorf("ReferenceTypeLocal: got %q, want %q", ReferenceTypeLocal, "local")
	}
	if ReferenceTypeOCI != "oci" {
		t.Errorf("ReferenceTypeOCI: got %q, want %q", ReferenceTypeOCI, "oci")
	}
	if ReferenceTypeGit != "git" {
		t.Errorf("ReferenceTypeGit: got %q, want %q", ReferenceTypeGit, "git")
	}
}

func TestNewResolver(t *testing.T) {
	t.Run("with default options", func(t *testing.T) {
		resolver := NewResolver(Options{
			AllowLocal:  true,
			AllowRemote: true,
		})

		if resolver == nil {
			t.Fatal("NewResolver returned nil")
		}
	})

	t.Run("with custom cache dir", func(t *testing.T) {
		customDir := "/tmp/custom-cache"
		r := NewResolver(Options{
			CacheDir:    customDir,
			AllowLocal:  true,
			AllowRemote: false,
		})

		if r == nil {
			t.Fatal("NewResolver returned nil")
		}

		// The resolver was created with the custom cache dir
		// We can't easily inspect internal state, but we verify it doesn't crash
	})
}

func TestResolveLocal(t *testing.T) {
	// Create a temporary directory with a component
	tmpDir, err := os.MkdirTemp("", "resolver-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a valid component file
	componentContent := `name: test-component
version: v1
`
	componentPath := filepath.Join(tmpDir, "cld.yml")
	if err := os.WriteFile(componentPath, []byte(componentContent), 0644); err != nil {
		t.Fatalf("Failed to create component file: %v", err)
	}

	t.Run("resolve directory with cld.yml", func(t *testing.T) {
		resolver := NewResolver(Options{
			AllowLocal: true,
		})

		resolved, err := resolver.Resolve(context.Background(), tmpDir)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}

		if resolved.Type != ReferenceTypeLocal {
			t.Errorf("Type: got %q, want %q", resolved.Type, ReferenceTypeLocal)
		}
		if resolved.Path != componentPath {
			t.Errorf("Path: got %q, want %q", resolved.Path, componentPath)
		}
	})

	t.Run("resolve file directly", func(t *testing.T) {
		resolver := NewResolver(Options{
			AllowLocal: true,
		})

		resolved, err := resolver.Resolve(context.Background(), componentPath)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}

		if resolved.Path != componentPath {
			t.Errorf("Path: got %q, want %q", resolved.Path, componentPath)
		}
	})

	t.Run("local not allowed", func(t *testing.T) {
		resolver := NewResolver(Options{
			AllowLocal: false,
		})

		_, err := resolver.Resolve(context.Background(), tmpDir)
		if err == nil {
			t.Error("Expected error when local references not allowed")
		}
	})

	t.Run("directory without component file", func(t *testing.T) {
		emptyDir, err := os.MkdirTemp("", "resolver-empty-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(emptyDir)

		resolver := NewResolver(Options{
			AllowLocal: true,
		})

		_, err = resolver.Resolve(context.Background(), emptyDir)
		if err == nil {
			t.Error("Expected error for directory without cld.yml")
		}
	})

	t.Run("nonexistent path", func(t *testing.T) {
		resolver := NewResolver(Options{
			AllowLocal: true,
		})

		_, err := resolver.Resolve(context.Background(), "/nonexistent/path")
		if err == nil {
			t.Error("Expected error for nonexistent path")
		}
	})
}

func TestResolveAll(t *testing.T) {
	// Create temporary directories with components
	tmpDir1, err := os.MkdirTemp("", "resolver-test1-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir1)

	tmpDir2, err := os.MkdirTemp("", "resolver-test2-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir2)

	// Create component files
	componentContent := `name: test-component
version: v1
`
	_ = os.WriteFile(filepath.Join(tmpDir1, "cld.yml"), []byte(componentContent), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir2, "cld.yml"), []byte(componentContent), 0644)

	resolver := NewResolver(Options{
		AllowLocal: true,
	})

	refs := []string{tmpDir1, tmpDir2}
	results, err := resolver.ResolveAll(context.Background(), refs)
	if err != nil {
		t.Fatalf("ResolveAll failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}
}

func TestResolveOCI_NotAllowed(t *testing.T) {
	resolver := NewResolver(Options{
		AllowLocal:  true,
		AllowRemote: false,
	})

	_, err := resolver.Resolve(context.Background(), "ghcr.io/org/component:v1.0.0")
	if err == nil {
		t.Error("Expected error when remote references not allowed")
	}
}

func TestResolveGit_NotAllowed(t *testing.T) {
	resolver := NewResolver(Options{
		AllowLocal:  true,
		AllowRemote: false,
	})

	_, err := resolver.Resolve(context.Background(), "git::https://github.com/org/repo.git")
	if err == nil {
		t.Error("Expected error when remote references not allowed")
	}
}

func TestResolveGit_InvalidFormat(t *testing.T) {
	resolver := NewResolver(Options{
		AllowLocal:  true,
		AllowRemote: true,
	})

	// Test invalid git reference (missing ::)
	_, err := resolver.Resolve(context.Background(), "git:https://github.com/org/repo.git")
	if err == nil {
		t.Error("Expected error for invalid git reference format")
	}
}

func TestResolvedComponent(t *testing.T) {
	rc := ResolvedComponent{
		Reference: "./my-component",
		Type:      ReferenceTypeLocal,
		Path:      "/absolute/path/to/cld.yml",
		Version:   "v1.0.0",
		Digest:    "sha256:abc123",
		Metadata: map[string]string{
			"key": "value",
		},
	}

	if rc.Reference != "./my-component" {
		t.Errorf("Reference: got %q", rc.Reference)
	}
	if rc.Type != ReferenceTypeLocal {
		t.Errorf("Type: got %q", rc.Type)
	}
	if rc.Metadata["key"] != "value" {
		t.Error("Metadata not preserved")
	}
}
