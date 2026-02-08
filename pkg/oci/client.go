package oci

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// Client provides OCI registry operations.
type Client struct {
	auth authn.Keychain
}

// NewClient creates a new OCI client.
func NewClient() *Client {
	return &Client{
		auth: authn.DefaultKeychain,
	}
}

// Push pushes an artifact to the registry.
func (c *Client) Push(ctx context.Context, artifact *Artifact) error {
	ref, err := name.ParseReference(artifact.Reference)
	if err != nil {
		return fmt.Errorf("invalid reference: %w", err)
	}

	// Start with empty image
	img := empty.Image

	// Determine media types based on artifact type
	layerMediaType := MediaTypeComponentLayer
	switch artifact.Type {
	case ArtifactTypeDatacenter:
		layerMediaType = MediaTypeDatacenterLayer
	case ArtifactTypeModule:
		layerMediaType = MediaTypeModuleLayer
	}

	// Add layers
	for _, layer := range artifact.Layers {
		l := static.NewLayer(layer.Data, types.MediaType(layerMediaType))
		img, err = mutate.AppendLayers(img, l)
		if err != nil {
			return fmt.Errorf("failed to append layer: %w", err)
		}
	}

	// Push
	if err := remote.Write(ref, img, remote.WithAuthFromKeychain(c.auth), remote.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to push: %w", err)
	}

	return nil
}

// Pull pulls an artifact from the registry.
func (c *Client) Pull(ctx context.Context, reference string, destDir string) error {
	ref, err := name.ParseReference(reference)
	if err != nil {
		return fmt.Errorf("invalid reference: %w", err)
	}

	// Pull image
	img, err := remote.Image(ref, remote.WithAuthFromKeychain(c.auth), remote.WithContext(ctx))
	if err != nil {
		return registryError(reference, err)
	}

	// Extract layers
	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("failed to get layers: %w", err)
	}

	for _, layer := range layers {
		rc, err := layer.Uncompressed()
		if err != nil {
			return fmt.Errorf("failed to uncompress layer: %w", err)
		}

		if err := extractTar(rc, destDir); err != nil {
			rc.Close()
			return fmt.Errorf("failed to extract layer: %w", err)
		}
		rc.Close()
	}

	return nil
}

// PullConfig pulls only the config from an artifact.
func (c *Client) PullConfig(ctx context.Context, reference string) ([]byte, error) {
	ref, err := name.ParseReference(reference)
	if err != nil {
		return nil, fmt.Errorf("invalid reference: %w", err)
	}

	// Pull image
	img, err := remote.Image(ref, remote.WithAuthFromKeychain(c.auth), remote.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to pull: %w", err)
	}

	// Get config
	configFile, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("failed to get config: %w", err)
	}

	return json.Marshal(configFile)
}

// Exists checks if an artifact exists in the registry.
func (c *Client) Exists(ctx context.Context, reference string) (bool, error) {
	ref, err := name.ParseReference(reference)
	if err != nil {
		return false, fmt.Errorf("invalid reference: %w", err)
	}

	_, err = remote.Head(ref, remote.WithAuthFromKeychain(c.auth), remote.WithContext(ctx))
	if err != nil {
		// Check if it's a "not found" error
		return false, nil
	}

	return true, nil
}

// Tag adds a new tag to an existing artifact.
func (c *Client) Tag(ctx context.Context, srcRef, destRef string) error {
	src, err := name.ParseReference(srcRef)
	if err != nil {
		return fmt.Errorf("invalid source reference: %w", err)
	}

	dest, err := name.ParseReference(destRef)
	if err != nil {
		return fmt.Errorf("invalid destination reference: %w", err)
	}

	// Get the source image
	img, err := remote.Image(src, remote.WithAuthFromKeychain(c.auth), remote.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("failed to get source image: %w", err)
	}

	// Push with new tag
	if err := remote.Write(dest, img, remote.WithAuthFromKeychain(c.auth), remote.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to tag: %w", err)
	}

	return nil
}

// BuildFromDirectory builds an artifact from a directory.
func (c *Client) BuildFromDirectory(ctx context.Context, dir string, artifactType ArtifactType, config interface{}) (*Artifact, error) {
	// Create tar from directory
	f, err := os.CreateTemp("", "cldctl-build-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tarPath := f.Name()
	f.Close()
	defer os.Remove(tarPath)

	if err := createTarGz(dir, tarPath); err != nil {
		return nil, fmt.Errorf("failed to create tar: %w", err)
	}

	// Read tar data
	tarData, err := os.ReadFile(tarPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read tar: %w", err)
	}

	// Marshal config
	configData, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	return &Artifact{
		Type:   artifactType,
		Config: configData,
		Layers: []Layer{{
			Data: tarData,
		}},
	}, nil
}

// registryError translates OCI registry errors into user-friendly messages.
func registryError(reference string, err error) error {
	var transportErr *transport.Error
	if errors.As(err, &transportErr) {
		for _, diagnostic := range transportErr.Errors {
			switch diagnostic.Code {
			case transport.ManifestUnknownErrorCode:
				return fmt.Errorf("artifact not found: %s does not exist or the tag is invalid", reference)
			case transport.NameUnknownErrorCode:
				return fmt.Errorf("repository not found: %s does not exist in the registry", reference)
			case transport.UnauthorizedErrorCode:
				return fmt.Errorf("authentication required: you may need to log in to access %s", reference)
			case transport.DeniedErrorCode:
				return fmt.Errorf("access denied: you don't have permission to pull %s", reference)
			}
		}

		if transportErr.StatusCode == http.StatusNotFound {
			return fmt.Errorf("artifact not found: %s does not exist in the registry", reference)
		}
	}

	return fmt.Errorf("failed to pull: %w", err)
}

// extractTar extracts a tar archive to a directory.
func extractTar(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		target := filepath.Join(destDir, header.Name)

		// Check for directory traversal
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid tar path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("failed to write file: %w", err)
			}
			f.Close()
		}
	}

	return nil
}

// defaultExcludeDirs contains directories that should always be excluded from artifacts.
var defaultExcludeDirs = map[string]bool{
	".terraform":   true,
	".git":         true,
	"node_modules": true,
	".next":        true,
	"__pycache__":  true,
	".venv":        true,
	"venv":         true,
	".cache":       true,
	"dist":         true,
	"build":        true,
	".DS_Store":    true,
}

// shouldExclude checks if a path should be excluded from the archive.
func shouldExclude(relPath string, info os.FileInfo) bool {
	// Check each path component against exclude list
	parts := strings.Split(relPath, string(filepath.Separator))
	for _, part := range parts {
		if defaultExcludeDirs[part] {
			return true
		}
	}

	// Exclude hidden files/directories (except specific ones we might want)
	baseName := filepath.Base(relPath)
	if strings.HasPrefix(baseName, ".") && baseName != ".env.example" && baseName != ".dockerignore" {
		return true
	}

	return false
}

// createTarGz creates a tar.gz archive from a directory.
func createTarGz(srcDir, destPath string) error {
	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create archive file: %w", err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		// Skip the root directory
		if relPath == "." {
			return nil
		}

		// Check if path should be excluded
		if shouldExclude(relPath, info) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Handle symlinks - check if target is a directory
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return nil // Skip broken symlinks
			}

			// Resolve the link target
			if !filepath.IsAbs(linkTarget) {
				linkTarget = filepath.Join(filepath.Dir(path), linkTarget)
			}

			targetInfo, err := os.Stat(linkTarget)
			if err != nil {
				return nil // Skip symlinks to non-existent targets
			}

			// Skip symlinks to directories (they can cause issues and are usually build artifacts)
			if targetInfo.IsDir() {
				return nil
			}

			// For symlinks to files, create a symlink entry in the tar
			header, err := tar.FileInfoHeader(info, linkTarget)
			if err != nil {
				return fmt.Errorf("failed to create symlink header: %w", err)
			}
			header.Name = relPath

			return tw.WriteHeader(header)
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("failed to create header: %w", err)
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write header: %w", err)
		}

		// Write file content (only for regular files)
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("failed to open file: %w", err)
			}
			defer file.Close()

			if _, err := io.Copy(tw, file); err != nil {
				return fmt.Errorf("failed to copy file: %w", err)
			}
		}

		return nil
	})
}
