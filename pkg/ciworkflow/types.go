// Package ciworkflow generates CI/CD workflow files from cldctl dependency graphs.
// It supports multiple CI providers (GitHub Actions, GitLab CI, CircleCI) and
// two modes: single-component workflows and full-environment workflows.
package ciworkflow

// OutputType identifies the type of output to generate.
type OutputType string

const (
	TypeGitHubActions OutputType = "github-actions"
	TypeGitLabCI      OutputType = "gitlab-ci"
	TypeCircleCI      OutputType = "circleci"
	TypeMermaid       OutputType = "mermaid"
	TypeImage         OutputType = "image"
)

// ValidOutputTypes returns all valid output type values.
func ValidOutputTypes() []string {
	return []string{
		string(TypeGitHubActions),
		string(TypeGitLabCI),
		string(TypeCircleCI),
		string(TypeMermaid),
		string(TypeImage),
	}
}

// IsCIType returns true if the output type is a CI provider (not visualization).
func (t OutputType) IsCIType() bool {
	switch t {
	case TypeGitHubActions, TypeGitLabCI, TypeCircleCI:
		return true
	default:
		return false
	}
}

// WorkflowMode distinguishes between component and environment workflows.
type WorkflowMode string

const (
	ModeComponent   WorkflowMode = "component"
	ModeEnvironment WorkflowMode = "environment"
)

// Workflow is the intermediate representation of a CI workflow.
// CI provider generators consume this to produce provider-specific YAML.
type Workflow struct {
	// Name is the workflow display name (e.g., "Deploy my-app").
	Name string

	// Mode indicates component or environment workflow.
	Mode WorkflowMode

	// Jobs is the ordered list of jobs in the deploy workflow.
	Jobs []Job

	// EnvVars are workflow-level environment variables.
	// Keys are env var names, values are the expressions/references
	// (e.g., "${{ secrets.API_KEY }}" for GitHub Actions).
	EnvVars map[string]string

	// Variables are the extracted variable declarations from the component
	// or environment config, used to generate setup comments.
	Variables []WorkflowVariable

	// TeardownJobs holds jobs for the teardown workflow (environment mode only).
	TeardownJobs []Job

	// Components lists all components included in the workflow.
	Components []ComponentRef

	// Dependencies lists cross-component dependency names to check (component mode).
	Dependencies []string

	// InstallVersion is the cldctl version to install in CI jobs.
	InstallVersion string

	// ComponentTag is the OCI tag template for apply commands (component mode).
	ComponentTag string
}

// WorkflowVariable represents a variable extracted from component/environment config.
// The generator maps these to CI-native secrets (sensitive) or variables (non-sensitive).
type WorkflowVariable struct {
	// Name is the variable name as declared in config (e.g., "api_key").
	Name string

	// EnvName is the uppercased env var name for CI (e.g., "API_KEY").
	EnvName string

	// Sensitive indicates the variable should be stored as a CI secret.
	Sensitive bool

	// Required indicates the variable must be set.
	Required bool

	// Default is the default value (empty if none).
	Default string

	// Description is a human-readable description for setup comments.
	Description string
}

// ComponentRef describes a component included in the workflow.
type ComponentRef struct {
	// Name is the component name (e.g., "my-app", "auth").
	Name string

	// Image is the OCI image reference (empty if local).
	Image string

	// Path is the local filesystem path (empty if OCI).
	Path string

	// IsLocal indicates this component needs build-and-push.
	IsLocal bool

	// Variables maps variable names to their values for this component.
	// Values may be env var references (e.g., "$API_KEY") or static values.
	Variables map[string]string
}

// Job represents a single CI job in the workflow.
type Job struct {
	// ID is the unique job identifier (e.g., "database-main", "auth--deployment-api").
	ID string

	// Name is the human-readable job name.
	Name string

	// Component is which component this job belongs to.
	Component string

	// NodeType is the graph node type (e.g., "database", "deployment").
	NodeType string

	// NodeName is the resource name within the component.
	NodeName string

	// DependsOn lists job IDs this job depends on.
	DependsOn []string

	// Steps contains the job's execution steps.
	Steps []Step

	// VarFlags are --var flags for the apply call
	// (e.g., ["secret_key=$SECRET_KEY", "log_level=debug"]).
	VarFlags []string

	// NeedsCheckout indicates the job needs source code (e.g., dockerBuild nodes).
	NeedsCheckout bool

	// ApplyCommand is the full cldctl apply command for this job.
	// Empty for non-apply jobs (setup, teardown).
	ApplyCommand string
}

// Step represents a single step within a CI job.
type Step struct {
	// Name is the step display name.
	Name string

	// Run is the shell command to execute.
	Run string

	// Uses is a CI action reference (GitHub Actions specific).
	Uses string

	// With contains action inputs (GitHub Actions specific).
	With map[string]string
}

// Generator is the interface for CI provider-specific workflow generators.
type Generator interface {
	// Generate produces the deploy workflow file content.
	Generate(w Workflow) ([]byte, error)

	// GenerateTeardown produces the teardown workflow file content (environment mode).
	GenerateTeardown(w Workflow) ([]byte, error)

	// DefaultOutputPath returns the conventional output path for this provider.
	DefaultOutputPath() string

	// DefaultTeardownOutputPath returns the conventional teardown output path.
	DefaultTeardownOutputPath() string
}
