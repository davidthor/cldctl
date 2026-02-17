package ciworkflow

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// GitHubActionsGenerator generates GitHub Actions workflow YAML.
type GitHubActionsGenerator struct{}

// NewGitHubActionsGenerator creates a new GitHub Actions generator.
func NewGitHubActionsGenerator() *GitHubActionsGenerator {
	return &GitHubActionsGenerator{}
}

// DefaultOutputPath returns the conventional path for the deploy workflow.
func (g *GitHubActionsGenerator) DefaultOutputPath() string {
	return ".github/workflows/deploy.yml"
}

// DefaultTeardownOutputPath returns the conventional path for the teardown workflow.
func (g *GitHubActionsGenerator) DefaultTeardownOutputPath() string {
	return ".github/workflows/teardown.yml"
}

// Generate produces a GitHub Actions deploy workflow YAML file.
func (g *GitHubActionsGenerator) Generate(w Workflow) ([]byte, error) {
	var buf bytes.Buffer

	// Header comment with setup instructions
	writeSetupComment(&buf, w)

	// Workflow name
	buf.WriteString(fmt.Sprintf("name: %s\n", w.Name))

	// Trigger
	if w.Mode == ModeEnvironment {
		buf.WriteString("on:\n")
		buf.WriteString("  pull_request:\n")
		buf.WriteString("    types: [opened, synchronize]\n")
	} else {
		buf.WriteString("on:\n")
		buf.WriteString("  push:\n")
		buf.WriteString("    branches: [main]\n")
	}
	buf.WriteString("\n")

	// Workflow-level env vars
	if len(w.EnvVars) > 0 {
		buf.WriteString("env:\n")
		keys := sortedMapKeys(w.EnvVars)
		for _, k := range keys {
			buf.WriteString(fmt.Sprintf("  %s: %s\n", k, w.EnvVars[k]))
		}
		buf.WriteString("\n")
	}

	// Jobs
	buf.WriteString("jobs:\n")

	for _, job := range w.Jobs {
		writeGitHubJob(&buf, job, w.InstallVersion)
	}

	return buf.Bytes(), nil
}

// GenerateTeardown produces a GitHub Actions teardown workflow YAML file.
func (g *GitHubActionsGenerator) GenerateTeardown(w Workflow) ([]byte, error) {
	if w.Mode != ModeEnvironment || len(w.TeardownJobs) == 0 {
		return nil, nil
	}

	var buf bytes.Buffer

	// Name
	teardownName := strings.Replace(w.Name, "Deploy", "Teardown", 1)
	if teardownName == w.Name {
		teardownName = w.Name + " - Teardown"
	}
	buf.WriteString(fmt.Sprintf("name: %s\n", teardownName))

	// Trigger
	buf.WriteString("on:\n")
	buf.WriteString("  pull_request:\n")
	buf.WriteString("    types: [closed]\n")
	buf.WriteString("\n")

	// Env vars (subset: just ENVIRONMENT and DATACENTER)
	buf.WriteString("env:\n")
	if v, ok := w.EnvVars["ENVIRONMENT"]; ok {
		buf.WriteString(fmt.Sprintf("  ENVIRONMENT: %s\n", v))
	}
	if v, ok := w.EnvVars["DATACENTER"]; ok {
		buf.WriteString(fmt.Sprintf("  DATACENTER: %s\n", v))
	}
	buf.WriteString("\n")

	// Jobs
	buf.WriteString("jobs:\n")

	for _, job := range w.TeardownJobs {
		writeGitHubJob(&buf, job, w.InstallVersion)
	}

	return buf.Bytes(), nil
}

// writeGitHubJob writes a single job in GitHub Actions YAML format.
func writeGitHubJob(buf *bytes.Buffer, job Job, installVersion string) {
	buf.WriteString(fmt.Sprintf("  %s:\n", job.ID))
	buf.WriteString(fmt.Sprintf("    name: %s\n", job.Name))
	if len(job.DependsOn) > 0 {
		buf.WriteString(fmt.Sprintf("    needs: [%s]\n", strings.Join(job.DependsOn, ", ")))
	}
	buf.WriteString("    runs-on: ubuntu-latest\n")
	buf.WriteString("    steps:\n")

	// Checkout step (if needed)
	if job.NeedsCheckout {
		buf.WriteString("      - uses: actions/checkout@v4\n")
	}

	// Install cldctl step
	installCmd := "curl -sSL https://get.cldctl.dev | sh"
	if installVersion != "" && installVersion != "latest" {
		installCmd = fmt.Sprintf("curl -sSL https://get.cldctl.dev | sh -s -- --version %s", installVersion)
	}
	buf.WriteString("      - name: Install cldctl\n")
	buf.WriteString(fmt.Sprintf("        run: %s\n", installCmd))

	// Custom steps (for teardown jobs that have explicit steps)
	if len(job.Steps) > 0 {
		for _, step := range job.Steps {
			if step.Uses != "" {
				buf.WriteString(fmt.Sprintf("      - uses: %s\n", step.Uses))
				if len(step.With) > 0 {
					buf.WriteString("        with:\n")
					for k, v := range step.With {
						buf.WriteString(fmt.Sprintf("          %s: %s\n", k, v))
					}
				}
			} else if step.Run != "" {
				buf.WriteString(fmt.Sprintf("      - name: %s\n", step.Name))
				buf.WriteString(fmt.Sprintf("        run: %s\n", step.Run))
			}
		}
	} else if job.ApplyCommand != "" {
		// Apply command step
		stepName := fmt.Sprintf("Apply %s/%s", job.NodeType, job.NodeName)
		buf.WriteString(fmt.Sprintf("      - name: %s\n", stepName))
		buf.WriteString(fmt.Sprintf("        run: >-\n          %s\n", job.ApplyCommand))
	}

	buf.WriteString("\n")
}

// writeSetupComment writes a comment block describing required CI configuration.
func writeSetupComment(buf *bytes.Buffer, w Workflow) {
	if len(w.Variables) == 0 {
		return
	}

	var secrets, vars []string
	for _, v := range w.Variables {
		if v.Sensitive {
			desc := v.EnvName
			if v.Description != "" {
				desc += " (" + v.Description + ")"
			}
			secrets = append(secrets, desc)
		} else if v.Default == "" {
			desc := v.EnvName
			if v.Description != "" {
				desc += " (" + v.Description + ")"
			}
			vars = append(vars, desc)
		}
	}

	if len(secrets) == 0 && len(vars) == 0 {
		return
	}

	buf.WriteString("# Configure these in Settings > Secrets and variables > Actions:\n")
	if len(secrets) > 0 {
		buf.WriteString(fmt.Sprintf("#   Secrets: %s\n", strings.Join(secrets, ", ")))
	}
	if len(vars) > 0 {
		buf.WriteString(fmt.Sprintf("#   Variables: %s\n", strings.Join(vars, ", ")))
	}
	buf.WriteString("#\n")
	buf.WriteString("# Also ensure CLDCTL_ENVIRONMENT and CLDCTL_DATACENTER are configured\n")
	buf.WriteString("# as repository variables, or set via the datacenter default config.\n")
	buf.WriteString("\n")
}

// sortedMapKeys returns sorted keys from a string map.
func sortedMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
