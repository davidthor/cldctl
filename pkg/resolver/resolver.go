// Package resolver provides component and dependency resolution.
package resolver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/davidthor/cldctl/pkg/oci"
	"github.com/davidthor/cldctl/pkg/schema/component"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// Resolver resolves component references to loadable sources.
type Resolver interface {
	// Resolve resolves a component reference to a loadable path
	Resolve(ctx context.Context, ref string) (ResolvedComponent, error)

	// ResolveAll resolves multiple component references
	ResolveAll(ctx context.Context, refs []string) ([]ResolvedComponent, error)
}

// ResolvedComponent represents a resolved component reference.
type ResolvedComponent struct {
	// Reference is the original reference
	Reference string

	// Type is the reference type (local, oci, git)
	Type ReferenceType

	// Path is the local path to the component
	Path string

	// Version is the resolved version (tag, commit, etc.)
	Version string

	// Digest is the content digest (for OCI)
	Digest string

	// Metadata contains additional resolution info
	Metadata map[string]string
}

// ReferenceType indicates the type of component reference.
type ReferenceType string

const (
	// ReferenceTypeLocal is a local filesystem path
	ReferenceTypeLocal ReferenceType = "local"

	// ReferenceTypeOCI is an OCI registry reference
	ReferenceTypeOCI ReferenceType = "oci"

	// ReferenceTypeGit is a git repository reference
	ReferenceTypeGit ReferenceType = "git"
)

// resolver implements the Resolver interface.
type resolver struct {
	ociClient   *oci.Client
	loader      component.Loader
	cacheDir    string
	allowLocal  bool
	allowRemote bool
}

// Options configures the resolver.
type Options struct {
	// CacheDir is the directory to cache downloaded components
	CacheDir string

	// AllowLocal allows resolving local filesystem paths
	AllowLocal bool

	// AllowRemote allows resolving remote references (OCI, git)
	AllowRemote bool

	// OCIClient is the OCI registry client
	OCIClient *oci.Client
}

// NewResolver creates a new component resolver.
func NewResolver(opts Options) Resolver {
	cacheDir := opts.CacheDir
	if cacheDir == "" {
		homeDir, _ := os.UserHomeDir()
		cacheDir = filepath.Join(homeDir, ".cldctl", "cache", "components")
	}

	return &resolver{
		ociClient:   opts.OCIClient,
		loader:      component.NewLoader(),
		cacheDir:    cacheDir,
		allowLocal:  opts.AllowLocal,
		allowRemote: opts.AllowRemote,
	}
}

func (r *resolver) Resolve(ctx context.Context, ref string) (ResolvedComponent, error) {
	// Use local-aware detection only when local references are allowed (CLI usage)
	// This enables commands like "cldctl up mycomponent" to work with local directories
	var refType ReferenceType
	if r.allowLocal {
		refType = DetectReferenceTypeWithLocal(ref)
	} else {
		refType = DetectReferenceType(ref)
	}

	switch refType {
	case ReferenceTypeLocal:
		return r.resolveLocal(ctx, ref)
	case ReferenceTypeOCI:
		return r.resolveOCI(ctx, ref)
	case ReferenceTypeGit:
		return r.resolveGit(ctx, ref)
	default:
		return ResolvedComponent{}, fmt.Errorf("unknown reference type: %s", ref)
	}
}

func (r *resolver) ResolveAll(ctx context.Context, refs []string) ([]ResolvedComponent, error) {
	results := make([]ResolvedComponent, 0, len(refs))

	for _, ref := range refs {
		resolved, err := r.Resolve(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve %s: %w", ref, err)
		}
		results = append(results, resolved)
	}

	return results, nil
}

func (r *resolver) resolveLocal(ctx context.Context, ref string) (ResolvedComponent, error) {
	if !r.allowLocal {
		return ResolvedComponent{}, fmt.Errorf("local references not allowed")
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(ref)
	if err != nil {
		return ResolvedComponent{}, fmt.Errorf("failed to resolve path: %w", err)
	}

	// Check if it's a directory or file
	info, err := os.Stat(absPath)
	if err != nil {
		return ResolvedComponent{}, fmt.Errorf("path not found: %w", err)
	}

	// If directory, look for cloud.component.yml
	if info.IsDir() {
		componentFile := filepath.Join(absPath, "cloud.component.yml")
		if _, err := os.Stat(componentFile); err != nil {
			componentFile = filepath.Join(absPath, "cloud.component.yaml")
			if _, err := os.Stat(componentFile); err != nil {
				return ResolvedComponent{}, fmt.Errorf("no cloud.component.yml found in %s", absPath)
			}
		}
		absPath = componentFile
	}

	// Validate it's a valid component
	if err := r.loader.Validate(absPath); err != nil {
		return ResolvedComponent{}, fmt.Errorf("invalid component: %w", err)
	}

	return ResolvedComponent{
		Reference: ref,
		Type:      ReferenceTypeLocal,
		Path:      absPath,
		Metadata:  map[string]string{},
	}, nil
}

func (r *resolver) resolveOCI(ctx context.Context, ref string) (ResolvedComponent, error) {
	if !r.allowRemote {
		return ResolvedComponent{}, fmt.Errorf("remote references not allowed")
	}

	if r.ociClient == nil {
		r.ociClient = oci.NewClient()
	}

	// Parse OCI reference
	ociRef, err := oci.ParseReference(ref)
	if err != nil {
		return ResolvedComponent{}, fmt.Errorf("invalid OCI reference: %w", err)
	}

	// Create cache directory for this component
	cacheKey := strings.ReplaceAll(ref, "/", "_")
	cacheKey = strings.ReplaceAll(cacheKey, ":", "_")
	componentDir := filepath.Join(r.cacheDir, cacheKey)
	digestFile := filepath.Join(componentDir, ".digest")

	// Check if already cached
	componentFile := filepath.Join(componentDir, "cloud.component.yml")
	if _, err := os.Stat(componentFile); err == nil {
		// Cache exists - check if we need to update by comparing digests
		needsUpdate := false

		// Only check for updates if the reference doesn't include a specific digest
		if ociRef.Digest == "" {
			// Read cached digest
			cachedDigest, _ := os.ReadFile(digestFile)

			// Check if remote has a different digest
			exists, _ := r.ociClient.Exists(ctx, ref)
			if exists {
				// Pull config to get current digest (lightweight operation)
				remoteConfig, err := r.ociClient.PullConfig(ctx, ref)
				if err == nil && len(remoteConfig) > 0 {
					// Simple digest comparison using config hash
					remoteDigest := fmt.Sprintf("%x", remoteConfig)
					if string(cachedDigest) != "" && string(cachedDigest) != remoteDigest {
						needsUpdate = true
					}
				}
			}
		}

		if !needsUpdate {
			return ResolvedComponent{
				Reference: ref,
				Type:      ReferenceTypeOCI,
				Path:      componentFile,
				Version:   ociRef.Tag,
				Digest:    ociRef.Digest,
				Metadata:  map[string]string{"cached": "true"},
			}, nil
		}

		// Remove old cache before re-pulling
		os.RemoveAll(componentDir)
	}

	// Pull component from registry
	if err := os.MkdirAll(componentDir, 0755); err != nil {
		return ResolvedComponent{}, fmt.Errorf("failed to create cache directory: %w", err)
	}

	if err := r.ociClient.Pull(ctx, ref, componentDir); err != nil {
		return ResolvedComponent{}, fmt.Errorf("failed to pull component: %w", err)
	}

	// Store digest for future cache validation
	remoteConfig, err := r.ociClient.PullConfig(ctx, ref)
	if err == nil && len(remoteConfig) > 0 {
		remoteDigest := fmt.Sprintf("%x", remoteConfig)
		_ = os.WriteFile(digestFile, []byte(remoteDigest), 0644)
	}

	// Find cloud.component.yml in pulled content
	componentFile = filepath.Join(componentDir, "cloud.component.yml")
	if _, err := os.Stat(componentFile); err != nil {
		componentFile = filepath.Join(componentDir, "cloud.component.yaml")
		if _, err := os.Stat(componentFile); err != nil {
			return ResolvedComponent{}, fmt.Errorf("no cloud.component.yml found in pulled artifact")
		}
	}

	return ResolvedComponent{
		Reference: ref,
		Type:      ReferenceTypeOCI,
		Path:      componentFile,
		Version:   ociRef.Tag,
		Digest:    ociRef.Digest,
		Metadata:  map[string]string{},
	}, nil
}

func (r *resolver) resolveGit(ctx context.Context, ref string) (ResolvedComponent, error) {
	if !r.allowRemote {
		return ResolvedComponent{}, fmt.Errorf("remote references not allowed")
	}

	// Parse git reference
	// Format: git::https://github.com/org/repo.git//path?ref=branch
	parts := strings.SplitN(ref, "::", 2)
	if len(parts) != 2 {
		return ResolvedComponent{}, fmt.Errorf("invalid git reference format")
	}

	gitURL := parts[1]
	subPath := ""
	gitRef := "main"

	// Extract subpath
	if idx := strings.Index(gitURL, "//"); idx != -1 {
		subPath = gitURL[idx+2:]
		gitURL = gitURL[:idx]

		// Extract query params (ref)
		if idx := strings.Index(subPath, "?"); idx != -1 {
			query := subPath[idx+1:]
			subPath = subPath[:idx]

			for _, param := range strings.Split(query, "&") {
				kv := strings.SplitN(param, "=", 2)
				if len(kv) == 2 && kv[0] == "ref" {
					gitRef = kv[1]
				}
			}
		}
	}

	// Create cache directory
	cacheKey := strings.ReplaceAll(gitURL, "/", "_")
	cacheKey = strings.ReplaceAll(cacheKey, ":", "_")
	cacheKey = strings.ReplaceAll(cacheKey, ".", "_")
	repoDir := filepath.Join(r.cacheDir, "git", cacheKey, gitRef)

	// Clone or update repo
	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		if err := r.gitClone(ctx, gitURL, gitRef, repoDir); err != nil {
			return ResolvedComponent{}, fmt.Errorf("failed to clone repository: %w", err)
		}
	}

	// Find component file
	componentDir := repoDir
	if subPath != "" {
		componentDir = filepath.Join(repoDir, subPath)
	}

	componentFile := filepath.Join(componentDir, "cloud.component.yml")
	if _, err := os.Stat(componentFile); err != nil {
		componentFile = filepath.Join(componentDir, "cloud.component.yaml")
		if _, err := os.Stat(componentFile); err != nil {
			return ResolvedComponent{}, fmt.Errorf("no cloud.component.yml found at %s", componentDir)
		}
	}

	return ResolvedComponent{
		Reference: ref,
		Type:      ReferenceTypeGit,
		Path:      componentFile,
		Version:   gitRef,
		Metadata: map[string]string{
			"repository": gitURL,
			"subpath":    subPath,
		},
	}, nil
}

func (r *resolver) gitClone(ctx context.Context, url, ref, dest string) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	// Clone options with depth 1 for shallow clone
	cloneOpts := &git.CloneOptions{
		URL:           url,
		Depth:         1,
		SingleBranch:  true,
		ReferenceName: plumbing.NewBranchReferenceName(ref),
	}

	// Try cloning as a branch first
	_, err := git.PlainCloneContext(ctx, dest, false, cloneOpts)
	if err != nil {
		// If branch clone fails, try as a tag
		cloneOpts.ReferenceName = plumbing.NewTagReferenceName(ref)
		_, err = git.PlainCloneContext(ctx, dest, false, cloneOpts)
		if err != nil {
			return fmt.Errorf("git clone failed: %w", err)
		}
	}

	return nil
}

// DetectReferenceType determines the type of a component reference.
// For dependency resolution, file paths are not supported - use DetectReferenceTypeWithLocal
// if you need to resolve local paths (e.g., for CLI commands).
func DetectReferenceType(ref string) ReferenceType {
	// Git references start with "git::"
	if strings.HasPrefix(ref, "git::") {
		return ReferenceTypeGit
	}

	// Local path prefixes - still detect them so we can provide good error messages
	if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") || strings.HasPrefix(ref, "/") {
		return ReferenceTypeLocal
	}

	// YAML file extensions indicate local paths
	if strings.HasSuffix(ref, ".yml") || strings.HasSuffix(ref, ".yaml") {
		return ReferenceTypeLocal
	}

	// Default to OCI for everything else (including simple names like "mycomponent")
	return ReferenceTypeOCI
}

// DetectReferenceTypeWithLocal determines the type of a component reference,
// including checking if the path exists locally. This is used by CLI commands
// that need to resolve local component paths.
func DetectReferenceTypeWithLocal(ref string) ReferenceType {
	// First check using the standard detection
	refType := DetectReferenceType(ref)
	if refType != ReferenceTypeOCI {
		return refType
	}

	// For OCI-looking refs, also check if they exist locally
	// This allows CLI commands like "cldctl up mycomponent" to work
	// when there's a local directory named "mycomponent"
	if _, err := os.Stat(ref); err == nil {
		return ReferenceTypeLocal
	}

	return ReferenceTypeOCI
}
