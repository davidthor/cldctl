package ciworkflow

import (
	"bytes"
	"fmt"
	"strings"
)

// GitLabCIGenerator generates GitLab CI pipeline YAML.
type GitLabCIGenerator struct{}

// NewGitLabCIGenerator creates a new GitLab CI generator.
func NewGitLabCIGenerator() *GitLabCIGenerator {
	return &GitLabCIGenerator{}
}

// DefaultOutputPath returns the conventional path for the pipeline.
func (g *GitLabCIGenerator) DefaultOutputPath() string {
	return ".gitlab-ci.yml"
}

// DefaultTeardownOutputPath returns the conventional path for the teardown pipeline.
func (g *GitLabCIGenerator) DefaultTeardownOutputPath() string {
	return ".gitlab-ci-teardown.yml"
}

// Generate produces a GitLab CI pipeline YAML file.
func (g *GitLabCIGenerator) Generate(w Workflow) ([]byte, error) {
	var buf bytes.Buffer

	// Header comment with setup instructions
	writeGitLabSetupComment(&buf, w)

	// Stages: derive from job ordering
	stages := deriveStages(w.Jobs)
	buf.WriteString("stages:\n")
	for _, stage := range stages {
		buf.WriteString(fmt.Sprintf("  - %s\n", stage))
	}
	buf.WriteString("\n")

	// Global variables
	if len(w.EnvVars) > 0 {
		buf.WriteString("variables:\n")
		keys := sortedMapKeys(w.EnvVars)
		for _, k := range keys {
			buf.WriteString(fmt.Sprintf("  %s: %s\n", k, w.EnvVars[k]))
		}
		buf.WriteString("\n")
	}

	// Job template (hidden job for reuse)
	installCmd := "curl -sSL https://get.cldctl.dev | sh"
	if w.InstallVersion != "" && w.InstallVersion != "latest" {
		installCmd = fmt.Sprintf("curl -sSL https://get.cldctl.dev | sh -s -- --version %s", w.InstallVersion)
	}
	buf.WriteString(".install-cldctl: &install-cldctl\n")
	buf.WriteString(fmt.Sprintf("  - %s\n", installCmd))
	buf.WriteString("\n")

	// Assign stages based on topological depth
	stageMap := assignStages(w.Jobs, stages)

	// Jobs
	for _, job := range w.Jobs {
		writeGitLabJob(&buf, job, stageMap[job.ID])
	}

	return buf.Bytes(), nil
}

// GenerateTeardown produces a GitLab CI teardown pipeline YAML file.
func (g *GitLabCIGenerator) GenerateTeardown(w Workflow) ([]byte, error) {
	if w.Mode != ModeEnvironment || len(w.TeardownJobs) == 0 {
		return nil, nil
	}

	var buf bytes.Buffer

	stages := deriveStages(w.TeardownJobs)
	buf.WriteString("stages:\n")
	for _, stage := range stages {
		buf.WriteString(fmt.Sprintf("  - %s\n", stage))
	}
	buf.WriteString("\n")

	// Minimal env vars
	buf.WriteString("variables:\n")
	if v, ok := w.EnvVars["ENVIRONMENT"]; ok {
		buf.WriteString(fmt.Sprintf("  ENVIRONMENT: %s\n", v))
	}
	if v, ok := w.EnvVars["DATACENTER"]; ok {
		buf.WriteString(fmt.Sprintf("  DATACENTER: %s\n", v))
	}
	buf.WriteString("\n")

	stageMap := assignStages(w.TeardownJobs, stages)

	for _, job := range w.TeardownJobs {
		writeGitLabJob(&buf, job, stageMap[job.ID])
	}

	return buf.Bytes(), nil
}

// writeGitLabJob writes a single job in GitLab CI format.
func writeGitLabJob(buf *bytes.Buffer, job Job, stage string) {
	buf.WriteString(fmt.Sprintf("%s:\n", job.ID))
	buf.WriteString(fmt.Sprintf("  stage: %s\n", stage))

	if len(job.DependsOn) > 0 {
		buf.WriteString("  needs:\n")
		for _, dep := range job.DependsOn {
			buf.WriteString(fmt.Sprintf("    - %s\n", dep))
		}
	}

	buf.WriteString("  image: ubuntu:latest\n")
	buf.WriteString("  script:\n")
	buf.WriteString("    - *install-cldctl\n")

	if len(job.Steps) > 0 {
		for _, step := range job.Steps {
			if step.Run != "" {
				buf.WriteString(fmt.Sprintf("    - %s\n", step.Run))
			}
		}
	} else if job.ApplyCommand != "" {
		buf.WriteString(fmt.Sprintf("    - >-\n      %s\n", job.ApplyCommand))
	}

	buf.WriteString("\n")
}

// writeGitLabSetupComment writes configuration instructions.
func writeGitLabSetupComment(buf *bytes.Buffer, w Workflow) {
	if len(w.Variables) == 0 {
		return
	}

	var secrets, vars []string
	for _, v := range w.Variables {
		if v.Sensitive {
			secrets = append(secrets, v.EnvName)
		} else if v.Default == "" {
			vars = append(vars, v.EnvName)
		}
	}

	if len(secrets) == 0 && len(vars) == 0 {
		return
	}

	buf.WriteString("# Configure these in Settings > CI/CD > Variables:\n")
	if len(secrets) > 0 {
		buf.WriteString(fmt.Sprintf("#   Protected/Masked: %s\n", strings.Join(secrets, ", ")))
	}
	if len(vars) > 0 {
		buf.WriteString(fmt.Sprintf("#   Variables: %s\n", strings.Join(vars, ", ")))
	}
	buf.WriteString("#\n")
	buf.WriteString("# Also ensure CLDCTL_ENVIRONMENT and CLDCTL_DATACENTER are configured.\n")
	buf.WriteString("\n")
}

// deriveStages creates stage names from the job DAG depth.
func deriveStages(jobs []Job) []string {
	if len(jobs) == 0 {
		return nil
	}

	depths := computeJobDepths(jobs)
	maxDepth := 0
	for _, d := range depths {
		if d > maxDepth {
			maxDepth = d
		}
	}

	stages := make([]string, maxDepth+1)
	for i := range stages {
		stages[i] = fmt.Sprintf("stage-%d", i)
	}
	return stages
}

// assignStages maps job IDs to their stage names based on depth.
func assignStages(jobs []Job, stages []string) map[string]string {
	depths := computeJobDepths(jobs)
	result := make(map[string]string, len(jobs))
	for _, job := range jobs {
		d := depths[job.ID]
		if d < len(stages) {
			result[job.ID] = stages[d]
		} else {
			result[job.ID] = stages[len(stages)-1]
		}
	}
	return result
}

// computeJobDepths returns the topological depth of each job.
func computeJobDepths(jobs []Job) map[string]int {
	depths := make(map[string]int, len(jobs))
	for _, job := range jobs {
		depths[job.ID] = 0
	}

	// Iteratively compute depths
	changed := true
	for changed {
		changed = false
		for _, job := range jobs {
			for _, dep := range job.DependsOn {
				if depDepth, ok := depths[dep]; ok {
					newDepth := depDepth + 1
					if newDepth > depths[job.ID] {
						depths[job.ID] = newDepth
						changed = true
					}
				}
			}
		}
	}
	return depths
}
