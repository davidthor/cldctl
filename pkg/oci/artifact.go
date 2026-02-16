// Package oci provides OCI artifact management for cldctl.
package oci

import (
	"strings"
)

// ArtifactType identifies the type of OCI artifact.
type ArtifactType string

const (
	ArtifactTypeComponent  ArtifactType = "component"
	ArtifactTypeDatacenter ArtifactType = "datacenter"
	ArtifactTypeModule     ArtifactType = "module"
)

// MediaTypes for cldctl artifacts.
const (
	MediaTypeComponentConfig  = "application/vnd.architect.component.config.v1+json"
	MediaTypeComponentLayer   = "application/vnd.architect.component.layer.v1.tar+gzip"
	MediaTypeDatacenterConfig = "application/vnd.architect.datacenter.config.v1+json"
	MediaTypeDatacenterLayer  = "application/vnd.architect.datacenter.layer.v1.tar+gzip"
	MediaTypeModuleConfig     = "application/vnd.architect.module.config.v1+json"
	MediaTypeModuleLayer      = "application/vnd.architect.module.layer.v1.tar+gzip"
)

// Artifact represents an OCI artifact.
type Artifact struct {
	Type        ArtifactType
	Reference   string // OCI reference (repo:tag)
	Digest      string // Content digest
	Config      []byte // Artifact configuration
	Layers      []Layer
	Annotations map[string]string
}

// Layer represents a layer in the artifact.
type Layer struct {
	MediaType   string
	Digest      string
	Size        int64
	Data        []byte
	Annotations map[string]string
}

// Reference represents a parsed OCI reference.
type Reference struct {
	Registry   string // e.g., "docker.io", "ghcr.io"
	Repository string // e.g., "library/nginx", "myorg/myapp"
	Tag        string // e.g., "latest", "v1.0.0"
	Digest     string // e.g., "sha256:abc123..."
}

// ParseReference parses an OCI reference string.
func ParseReference(ref string) (*Reference, error) {
	result := &Reference{}

	// Check for digest
	if idx := strings.Index(ref, "@"); idx != -1 {
		result.Digest = ref[idx+1:]
		ref = ref[:idx]
	}

	// Check for tag
	if idx := strings.LastIndex(ref, ":"); idx != -1 {
		// Make sure this isn't a port number
		afterColon := ref[idx+1:]
		if !strings.Contains(afterColon, "/") {
			result.Tag = afterColon
			ref = ref[:idx]
		}
	}

	// Default tag
	if result.Tag == "" && result.Digest == "" {
		result.Tag = "latest"
	}

	// Parse registry and repository
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) == 1 {
		// No registry, assume docker.io
		result.Registry = "docker.io"
		result.Repository = "library/" + parts[0]
	} else if strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") || parts[0] == "localhost" {
		// Has registry
		result.Registry = parts[0]
		result.Repository = parts[1]
	} else {
		// No registry, assume docker.io
		result.Registry = "docker.io"
		result.Repository = ref
	}

	return result, nil
}

// String returns the full reference string.
func (r *Reference) String() string {
	result := r.Registry + "/" + r.Repository
	if r.Tag != "" {
		result += ":" + r.Tag
	}
	if r.Digest != "" {
		result += "@" + r.Digest
	}
	return result
}

// ComponentConfig represents the configuration stored in a component artifact.
type ComponentConfig struct {
	SchemaVersion  string            `json:"schemaVersion"`
	Readme         string            `json:"readme,omitempty"`         // README content bundled at build time
	ChildArtifacts map[string]string `json:"childArtifacts,omitempty"` // Resource type -> OCI reference
	SourceHash     string            `json:"sourceHash,omitempty"`
	BuildTime      string            `json:"buildTime,omitempty"`
}

// DatacenterConfig represents the configuration stored in a datacenter artifact.
type DatacenterConfig struct {
	SchemaVersion   string            `json:"schemaVersion"`
	Name            string            `json:"name"`
	ModuleArtifacts map[string]string `json:"moduleArtifacts,omitempty"` // Module name -> OCI reference
	ExtendsImage    string            `json:"extendsImage,omitempty"`    // Parent datacenter OCI reference (deploy-time resolution)
	SourceHash      string            `json:"sourceHash,omitempty"`
	BuildTime       string            `json:"buildTime,omitempty"`
}

// ModuleConfig represents the configuration stored in a module artifact.
type ModuleConfig struct {
	Plugin     string            `json:"plugin"` // pulumi, opentofu, native
	Name       string            `json:"name"`
	Inputs     map[string]string `json:"inputs,omitempty"`  // Input schema summary
	Outputs    map[string]string `json:"outputs,omitempty"` // Output schema summary
	SourceHash string            `json:"sourceHash,omitempty"`
	BuildTime  string            `json:"buildTime,omitempty"`
}
