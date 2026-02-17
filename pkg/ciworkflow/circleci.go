package ciworkflow

import (
	"bytes"
	"fmt"
	"strings"
)

// CircleCIGenerator generates CircleCI pipeline YAML.
type CircleCIGenerator struct{}

// NewCircleCIGenerator creates a new CircleCI generator.
func NewCircleCIGenerator() *CircleCIGenerator {
	return &CircleCIGenerator{}
}

// DefaultOutputPath returns the conventional path for the pipeline.
func (g *CircleCIGenerator) DefaultOutputPath() string {
	return ".circleci/config.yml"
}

// DefaultTeardownOutputPath returns the conventional path for teardown.
func (g *CircleCIGenerator) DefaultTeardownOutputPath() string {
	return ".circleci/teardown.yml"
}

// Generate produces a CircleCI pipeline YAML file.
func (g *CircleCIGenerator) Generate(w Workflow) ([]byte, error) {
	var buf bytes.Buffer

	// Header comment
	writeCircleCISetupComment(&buf, w)

	buf.WriteString("version: 2.1\n\n")

	// Commands (reusable steps)
	installCmd := "curl -sSL https://get.cldctl.dev | sh"
	if w.InstallVersion != "" && w.InstallVersion != "latest" {
		installCmd = fmt.Sprintf("curl -sSL https://get.cldctl.dev | sh -s -- --version %s", w.InstallVersion)
	}

	buf.WriteString("commands:\n")
	buf.WriteString("  install-cldctl:\n")
	buf.WriteString("    steps:\n")
	buf.WriteString("      - run:\n")
	buf.WriteString("          name: Install cldctl\n")
	buf.WriteString(fmt.Sprintf("          command: %s\n", installCmd))
	buf.WriteString("\n")

	// Jobs
	buf.WriteString("jobs:\n")
	for _, job := range w.Jobs {
		writeCircleCIJob(&buf, job)
	}

	// Workflows
	buf.WriteString("workflows:\n")
	workflowID := sanitizeCircleCIID(w.Name)
	buf.WriteString(fmt.Sprintf("  %s:\n", workflowID))
	buf.WriteString("    jobs:\n")
	for _, job := range w.Jobs {
		if len(job.DependsOn) == 0 {
			buf.WriteString(fmt.Sprintf("      - %s\n", job.ID))
		} else {
			buf.WriteString(fmt.Sprintf("      - %s:\n", job.ID))
			buf.WriteString("          requires:\n")
			for _, dep := range job.DependsOn {
				buf.WriteString(fmt.Sprintf("            - %s\n", dep))
			}
		}
	}

	return buf.Bytes(), nil
}

// GenerateTeardown produces a CircleCI teardown pipeline YAML file.
func (g *CircleCIGenerator) GenerateTeardown(w Workflow) ([]byte, error) {
	if w.Mode != ModeEnvironment || len(w.TeardownJobs) == 0 {
		return nil, nil
	}

	var buf bytes.Buffer

	buf.WriteString("version: 2.1\n\n")

	installCmd := "curl -sSL https://get.cldctl.dev | sh"
	if w.InstallVersion != "" && w.InstallVersion != "latest" {
		installCmd = fmt.Sprintf("curl -sSL https://get.cldctl.dev | sh -s -- --version %s", w.InstallVersion)
	}

	buf.WriteString("commands:\n")
	buf.WriteString("  install-cldctl:\n")
	buf.WriteString("    steps:\n")
	buf.WriteString("      - run:\n")
	buf.WriteString("          name: Install cldctl\n")
	buf.WriteString(fmt.Sprintf("          command: %s\n", installCmd))
	buf.WriteString("\n")

	buf.WriteString("jobs:\n")
	for _, job := range w.TeardownJobs {
		writeCircleCIJob(&buf, job)
	}

	buf.WriteString("workflows:\n")
	buf.WriteString("  teardown:\n")
	buf.WriteString("    jobs:\n")
	for _, job := range w.TeardownJobs {
		if len(job.DependsOn) == 0 {
			buf.WriteString(fmt.Sprintf("      - %s\n", job.ID))
		} else {
			buf.WriteString(fmt.Sprintf("      - %s:\n", job.ID))
			buf.WriteString("          requires:\n")
			for _, dep := range job.DependsOn {
				buf.WriteString(fmt.Sprintf("            - %s\n", dep))
			}
		}
	}

	return buf.Bytes(), nil
}

// writeCircleCIJob writes a single job in CircleCI format.
func writeCircleCIJob(buf *bytes.Buffer, job Job) {
	buf.WriteString(fmt.Sprintf("  %s:\n", job.ID))
	buf.WriteString("    docker:\n")
	buf.WriteString("      - image: cimg/base:current\n")
	buf.WriteString("    steps:\n")

	if job.NeedsCheckout {
		buf.WriteString("      - checkout\n")
	}

	buf.WriteString("      - install-cldctl\n")

	if len(job.Steps) > 0 {
		for _, step := range job.Steps {
			if step.Run != "" {
				buf.WriteString("      - run:\n")
				buf.WriteString(fmt.Sprintf("          name: %s\n", step.Name))
				buf.WriteString(fmt.Sprintf("          command: %s\n", step.Run))
			}
		}
	} else if job.ApplyCommand != "" {
		buf.WriteString("      - run:\n")
		buf.WriteString(fmt.Sprintf("          name: Apply %s/%s\n", job.NodeType, job.NodeName))
		buf.WriteString(fmt.Sprintf("          command: >-\n            %s\n", job.ApplyCommand))
	}

	buf.WriteString("\n")
}

// writeCircleCISetupComment writes configuration instructions.
func writeCircleCISetupComment(buf *bytes.Buffer, w Workflow) {
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

	buf.WriteString("# Configure these in Project Settings > Environment Variables:\n")
	if len(secrets) > 0 {
		buf.WriteString(fmt.Sprintf("#   Secrets: %s\n", strings.Join(secrets, ", ")))
	}
	if len(vars) > 0 {
		buf.WriteString(fmt.Sprintf("#   Variables: %s\n", strings.Join(vars, ", ")))
	}
	buf.WriteString("#\n")
	buf.WriteString("# Also ensure CLDCTL_ENVIRONMENT and CLDCTL_DATACENTER are configured.\n")
	buf.WriteString("\n")
}

// sanitizeCircleCIID makes a workflow name safe for YAML keys.
func sanitizeCircleCIID(name string) string {
	r := strings.NewReplacer(" ", "-", "/", "-", ".", "-")
	return strings.ToLower(r.Replace(name))
}
