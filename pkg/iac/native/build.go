package native

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
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/moby/go-archive"
)

// BuildOptions configures a Docker image build.
type BuildOptions struct {
	// Context is the build context directory
	Context string

	// Dockerfile is the path to the Dockerfile (relative to context)
	Dockerfile string

	// Tags are the image tags to apply
	Tags []string

	// BuildArgs are build-time variables
	BuildArgs map[string]string

	// Target is the build target stage
	Target string

	// Platform is the target platform (e.g., "linux/amd64")
	Platform string

	// NoCache disables build cache
	NoCache bool

	// Pull always pulls base images
	Pull bool

	// Labels to apply to the image
	Labels map[string]string

	// Output writers for build logs
	Stdout io.Writer
	Stderr io.Writer
}

// BuildResult contains the result of a Docker build.
type BuildResult struct {
	// ImageID is the built image ID
	ImageID string

	// Tags are the tags applied to the image
	Tags []string

	// Digest is the image digest (if pushed)
	Digest string

	// Size is the image size in bytes
	Size int64
}

// BuildImage builds a Docker image from a Dockerfile.
func (d *DockerClient) BuildImage(ctx context.Context, opts BuildOptions) (*BuildResult, error) {
	// Create build context tar
	contextTar, err := d.createBuildContext(opts.Context, opts.Dockerfile)
	if err != nil {
		return nil, fmt.Errorf("failed to create build context: %w", err)
	}
	defer contextTar.Close()

	// Prepare build options
	dockerfile := opts.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	buildArgs := make(map[string]*string)
	for k, v := range opts.BuildArgs {
		val := v
		buildArgs[k] = &val
	}

	buildOpts := build.ImageBuildOptions{
		Dockerfile:  dockerfile,
		Tags:        opts.Tags,
		BuildArgs:   buildArgs,
		Target:      opts.Target,
		NoCache:     opts.NoCache,
		PullParent:  opts.Pull,
		Labels:      opts.Labels,
		Remove:      true,
		ForceRemove: true,
	}

	if opts.Platform != "" {
		buildOpts.Platform = opts.Platform
	}

	// Build image
	response, err := d.client.ImageBuild(ctx, contextTar, buildOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to start build: %w", err)
	}
	defer response.Body.Close()

	// Process build output
	imageID, err := d.processBuildOutput(response.Body, opts.Stdout, opts.Stderr)
	if err != nil {
		return nil, fmt.Errorf("build failed: %w", err)
	}

	// Get image info
	info, err := d.client.ImageInspect(ctx, imageID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect image: %w", err)
	}

	return &BuildResult{
		ImageID: imageID,
		Tags:    opts.Tags,
		Size:    info.Size,
	}, nil
}

// createBuildContext creates a tar archive of the build context.
func (d *DockerClient) createBuildContext(contextPath, dockerfile string) (io.ReadCloser, error) {
	// Use Docker's archive package for efficient context creation
	contextPath, err := filepath.Abs(contextPath)
	if err != nil {
		return nil, err
	}

	// Check if context exists
	info, err := os.Stat(contextPath)
	if err != nil {
		return nil, fmt.Errorf("build context not found: %w", err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("build context must be a directory")
	}

	// Create tar with exclusions
	excludes := []string{
		".git",
		".gitignore",
		".dockerignore",
		"node_modules",
		"__pycache__",
		".venv",
		"venv",
	}

	// Read .dockerignore if present
	dockerignorePath := filepath.Join(contextPath, ".dockerignore")
	if data, err := os.ReadFile(dockerignorePath); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				excludes = append(excludes, line)
			}
		}
	}

	return archive.TarWithOptions(contextPath, &archive.TarOptions{
		ExcludePatterns: excludes,
	})
}

// processBuildOutput reads the build output and extracts the image ID.
func (d *DockerClient) processBuildOutput(reader io.Reader, stdout, stderr io.Writer) (string, error) {
	var imageID string
	decoder := json.NewDecoder(reader)

	for {
		var msg struct {
			Stream      string `json:"stream"`
			Status      string `json:"status"`
			Error       string `json:"error"`
			ErrorDetail struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
			Aux struct {
				ID string `json:"ID"`
			} `json:"aux"`
		}

		if err := decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}

		// Check for errors
		if msg.Error != "" {
			errMsg := msg.Error
			if msg.ErrorDetail.Message != "" {
				errMsg = msg.ErrorDetail.Message
			}
			return "", fmt.Errorf("%s", errMsg)
		}

		// Write stream output
		if msg.Stream != "" && stdout != nil {
			stdout.Write([]byte(msg.Stream))
		}

		// Capture image ID from aux
		if msg.Aux.ID != "" {
			imageID = msg.Aux.ID
		}

		// Try to extract image ID from stream
		if strings.HasPrefix(msg.Stream, "Successfully built ") {
			imageID = strings.TrimSpace(strings.TrimPrefix(msg.Stream, "Successfully built "))
		}
	}

	if imageID == "" {
		return "", fmt.Errorf("build completed but no image ID found")
	}

	return imageID, nil
}

// PushImage pushes an image to a registry.
func (d *DockerClient) PushImage(ctx context.Context, imageName string, authConfig registry.AuthConfig) (string, error) {
	// Encode auth config
	encodedAuth, err := encodeAuthConfig(authConfig)
	if err != nil {
		return "", fmt.Errorf("failed to encode auth: %w", err)
	}

	// Push image
	response, err := d.client.ImagePush(ctx, imageName, image.PushOptions{
		RegistryAuth: encodedAuth,
	})
	if err != nil {
		return "", fmt.Errorf("failed to push image: %w", err)
	}
	defer response.Close()

	// Process push output
	digest, err := d.processPushOutput(response)
	if err != nil {
		return "", fmt.Errorf("push failed: %w", err)
	}

	return digest, nil
}

// processPushOutput reads the push output and extracts the digest.
func (d *DockerClient) processPushOutput(reader io.Reader) (string, error) {
	var digest string
	decoder := json.NewDecoder(reader)

	for {
		var msg struct {
			Status      string `json:"status"`
			Error       string `json:"error"`
			ErrorDetail struct {
				Message string `json:"message"`
			} `json:"errorDetail"`
			Aux struct {
				Tag    string `json:"Tag"`
				Digest string `json:"Digest"`
				Size   int64  `json:"Size"`
			} `json:"aux"`
		}

		if err := decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}

		// Check for errors
		if msg.Error != "" {
			errMsg := msg.Error
			if msg.ErrorDetail.Message != "" {
				errMsg = msg.ErrorDetail.Message
			}
			return "", fmt.Errorf("%s", errMsg)
		}

		// Capture digest from aux
		if msg.Aux.Digest != "" {
			digest = msg.Aux.Digest
		}
	}

	return digest, nil
}

// TagImage tags an image with a new tag.
func (d *DockerClient) TagImage(ctx context.Context, sourceImage, targetImage string) error {
	return d.client.ImageTag(ctx, sourceImage, targetImage)
}

// RemoveImage removes a Docker image.
func (d *DockerClient) RemoveImage(ctx context.Context, imageID string, force bool) error {
	_, err := d.client.ImageRemove(ctx, imageID, image.RemoveOptions{
		Force:         force,
		PruneChildren: true,
	})
	return err
}

// encodeAuthConfig encodes auth configuration for the Docker API.
func encodeAuthConfig(authConfig registry.AuthConfig) (string, error) {
	authBytes, err := json.Marshal(authConfig)
	if err != nil {
		return "", err
	}

	buf := &bytes.Buffer{}
	encoder := json.NewEncoder(buf)
	if err := encoder.Encode(authBytes); err != nil {
		return "", err
	}

	return strings.TrimSpace(buf.String()), nil
}

// createTarFromDirectory creates a tar archive from a directory.
func createTarFromDirectory(srcPath string) (io.ReadCloser, error) { //nolint:unused
	srcPath, err := filepath.Abs(srcPath)
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	tw := tar.NewWriter(pw)

	go func() {
		defer pw.Close()
		defer tw.Close()

		err := filepath.Walk(srcPath, func(file string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Skip directories
			if fi.IsDir() {
				return nil
			}

			// Create tar header
			relPath, err := filepath.Rel(srcPath, file)
			if err != nil {
				return err
			}

			header, err := tar.FileInfoHeader(fi, "")
			if err != nil {
				return err
			}
			header.Name = relPath

			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			// Write file content
			f, err := os.Open(file)
			if err != nil {
				return err
			}
			defer f.Close()

			_, err = io.Copy(tw, f)
			return err
		})

		if err != nil {
			pw.CloseWithError(err)
		}
	}()

	return pr, nil
}
