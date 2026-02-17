package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/davidthor/cldctl/pkg/ciworkflow"
	"github.com/davidthor/cldctl/pkg/graph"
	"github.com/davidthor/cldctl/pkg/graph/visual"
	"github.com/davidthor/cldctl/pkg/schema/component"
	"github.com/davidthor/cldctl/pkg/schema/environment"
	"github.com/spf13/cobra"
)

func newGenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate CI/CD workflows and visualizations",
		Long:  `Commands for generating CI workflow files and graph visualizations from component and environment definitions.`,
	}

	cmd.AddCommand(newGenerateComponentCmd())
	cmd.AddCommand(newGenerateEnvironmentCmd())

	return cmd
}

// --- generate component workflow ---

func newGenerateComponentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "component",
		Short: "Generate from a component definition",
	}

	cmd.AddCommand(newGenerateComponentWorkflowCmd())
	return cmd
}

func newGenerateComponentWorkflowCmd() *cobra.Command {
	var (
		outputType     string
		outputPath     string
		componentTag   string
		installVersion string
	)

	cmd := &cobra.Command{
		Use:   "workflow <component-path>",
		Short: "Generate a CI workflow for deploying a component",
		Long: `Generates a CI/CD workflow file from a component's dependency graph.

The workflow distributes each resource in the dependency graph into a separate
CI job, respecting dependency ordering. Each job runs 'cldctl apply' to
provision a single resource.

Supported output types:
  github-actions  GitHub Actions workflow YAML
  gitlab-ci       GitLab CI pipeline YAML
  circleci        CircleCI pipeline YAML
  mermaid         Mermaid flowchart diagram (text)
  image           PNG image of the workflow graph (requires mermaid-cli)

Examples:
  cldctl generate component workflow ./my-app --type github-actions
  cldctl generate component workflow ./my-app --type gitlab-ci -o .gitlab-ci.yml
  cldctl generate component workflow ./my-app --type mermaid
  cldctl generate component workflow ./my-app --type image -o workflow.png`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			compPath := args[0]
			ot := ciworkflow.OutputType(outputType)

			// Validate output type
			if !isValidOutputType(ot) {
				return fmt.Errorf("invalid --type %q; valid values: %s",
					outputType, strings.Join(ciworkflow.ValidOutputTypes(), ", "))
			}

			// Load component
			loader := component.NewLoader()
			compFile := findComponentFile(compPath)
			if compFile == "" {
				return fmt.Errorf("no component file found at %s", compPath)
			}

			comp, err := loader.Load(compFile)
			if err != nil {
				return fmt.Errorf("failed to load component: %w", err)
			}

			// Derive component name from path
			absPath, _ := filepath.Abs(compPath)
			compName := filepath.Base(absPath)

			// Build the logical graph (no datacenter, no implicit nodes)
			builder := graph.NewBuilder("", "")
			if err := builder.AddComponent(compName, comp); err != nil {
				return fmt.Errorf("failed to build graph: %w", err)
			}
			g := builder.Build()

			// Handle visualization types directly
			if ot == ciworkflow.TypeMermaid || ot == ciworkflow.TypeImage {
				return handleVisualization(g, ot, outputPath, compName, false)
			}

			// Build workflow for CI types
			wf, err := buildComponentWorkflow(comp, compName, g, componentTag, installVersion)
			if err != nil {
				return fmt.Errorf("failed to build workflow: %w", err)
			}

			return generateAndWrite(ot, wf, outputPath)
		},
	}

	cmd.Flags().StringVarP(&outputType, "type", "t", "", "Output type (required): github-actions, gitlab-ci, circleci, mermaid, image")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output file path (defaults to type-specific path)")
	cmd.Flags().StringVar(&componentTag, "component-tag", "$COMPONENT_IMAGE", "OCI tag template for apply commands")
	cmd.Flags().StringVar(&installVersion, "install-version", "latest", "cldctl version to install in workflows")
	_ = cmd.MarkFlagRequired("type")

	return cmd
}

// --- generate environment workflow ---

func newGenerateEnvironmentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "environment",
		Short: "Generate from an environment definition",
		Aliases: []string{"env"},
	}

	cmd.AddCommand(newGenerateEnvironmentWorkflowCmd())
	return cmd
}

func newGenerateEnvironmentWorkflowCmd() *cobra.Command {
	var (
		outputType     string
		outputPath     string
		installVersion string
		teardown       bool
		teardownOutput string
	)

	cmd := &cobra.Command{
		Use:   "workflow <environment-path>",
		Short: "Generate CI workflows for an environment",
		Long: `Generates CI/CD workflow files for provisioning (and tearing down) an entire environment.

This builds a unified graph across all components in the environment config,
enabling cross-component dependencies to be resolved naturally. Each resource
becomes a separate CI job.

For CI types, a teardown workflow is also generated (disable with --teardown=false).

Supported output types:
  github-actions  GitHub Actions workflow YAML
  gitlab-ci       GitLab CI pipeline YAML
  circleci        CircleCI pipeline YAML
  mermaid         Mermaid flowchart diagram (text)
  image           PNG image of the workflow graph (requires mermaid-cli)

Examples:
  cldctl generate environment workflow ./environment.yml --type github-actions
  cldctl generate environment workflow ./envs/preview.yml --type github-actions -o .github/workflows/preview.yml
  cldctl generate environment workflow ./environment.yml --type mermaid
  cldctl generate environment workflow ./environment.yml --type image -o preview-workflow.png`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			envPath := args[0]
			ot := ciworkflow.OutputType(outputType)

			if !isValidOutputType(ot) {
				return fmt.Errorf("invalid --type %q; valid values: %s",
					outputType, strings.Join(ciworkflow.ValidOutputTypes(), ", "))
			}

			// Load environment config
			envLoader := environment.NewLoader()
			env, err := envLoader.Load(envPath)
			if err != nil {
				return fmt.Errorf("failed to load environment: %w", err)
			}

			// Load all components and build unified graph
			compLoader := component.NewLoader()
			builder := graph.NewBuilder("", "")

			var components []ciworkflow.ComponentRef
			compDeps := make(map[string][]string)

			for compName, compConfig := range env.Components() {
				var compFile string
				compRef := ciworkflow.ComponentRef{
					Name: compName,
				}

				if compConfig.Path() != "" {
					// Local component
					resolvedPath := compConfig.Path()
					if !filepath.IsAbs(resolvedPath) {
						envDir := filepath.Dir(envPath)
						resolvedPath = filepath.Join(envDir, resolvedPath)
					}
					compFile = findComponentFile(resolvedPath)
					if compFile == "" {
						return fmt.Errorf("no component file found for %s at %s", compName, resolvedPath)
					}
					compRef.Path = resolvedPath
					compRef.IsLocal = true
				} else if compConfig.Image() != "" {
					// OCI component - resolve from cache
					resolved, err := resolveComponentPath(compConfig.Image())
					if err != nil {
						return fmt.Errorf("failed to resolve component %s (%s): %w", compName, compConfig.Image(), err)
					}
					compFile = resolved
					compRef.Image = compConfig.Image()
				} else {
					return fmt.Errorf("component %s has neither path nor image", compName)
				}

				// Map variables
				compRef.Variables = make(map[string]string)
				for k, v := range compConfig.Variables() {
					compRef.Variables[k] = fmt.Sprintf("%v", v)
				}

				comp, err := compLoader.Load(compFile)
				if err != nil {
					return fmt.Errorf("failed to load component %s: %w", compName, err)
				}

				if err := builder.AddComponent(compName, comp); err != nil {
					return fmt.Errorf("failed to add component %s to graph: %w", compName, err)
				}

				// Record dependencies for teardown ordering
				for _, dep := range comp.Dependencies() {
					if !dep.Optional() {
						compDeps[compName] = append(compDeps[compName], dep.Name())
					}
				}

				components = append(components, compRef)
			}

			g := builder.Build()

			// Handle visualization types
			if ot == ciworkflow.TypeMermaid || ot == ciworkflow.TypeImage {
				return handleVisualization(g, ot, outputPath, "", true)
			}

			// Build environment workflow
			wf, err := buildEnvironmentWorkflow(env, components, g, compDeps, installVersion)
			if err != nil {
				return fmt.Errorf("failed to build workflow: %w", err)
			}

			// Generate and write deploy workflow
			if err := generateAndWrite(ot, wf, outputPath); err != nil {
				return err
			}

			// Generate teardown workflow if requested
			if teardown && ot.IsCIType() {
				gen := getGenerator(ot)
				if gen == nil {
					return nil
				}

				teardownBytes, err := gen.GenerateTeardown(wf)
				if err != nil {
					return fmt.Errorf("failed to generate teardown workflow: %w", err)
				}

				if teardownBytes != nil {
					tdPath := teardownOutput
					if tdPath == "" {
						tdPath = gen.DefaultTeardownOutputPath()
					}

					if err := writeOutput(tdPath, teardownBytes); err != nil {
						return err
					}
					fmt.Fprintf(os.Stderr, "Teardown workflow written to %s\n", tdPath)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputType, "type", "t", "", "Output type (required): github-actions, gitlab-ci, circleci, mermaid, image")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output file path")
	cmd.Flags().StringVar(&installVersion, "install-version", "latest", "cldctl version to install in workflows")
	cmd.Flags().BoolVar(&teardown, "teardown", true, "Generate teardown workflow (CI types only)")
	cmd.Flags().StringVar(&teardownOutput, "teardown-output", "", "Teardown workflow output path")
	_ = cmd.MarkFlagRequired("type")

	return cmd
}

// --- Shared helpers ---

func isValidOutputType(ot ciworkflow.OutputType) bool {
	for _, valid := range ciworkflow.ValidOutputTypes() {
		if string(ot) == valid {
			return true
		}
	}
	return false
}

func getGenerator(ot ciworkflow.OutputType) ciworkflow.Generator {
	switch ot {
	case ciworkflow.TypeGitHubActions:
		return ciworkflow.NewGitHubActionsGenerator()
	case ciworkflow.TypeGitLabCI:
		return ciworkflow.NewGitLabCIGenerator()
	case ciworkflow.TypeCircleCI:
		return ciworkflow.NewCircleCIGenerator()
	default:
		return nil
	}
}

func generateAndWrite(ot ciworkflow.OutputType, wf ciworkflow.Workflow, outputPath string) error {
	gen := getGenerator(ot)
	if gen == nil {
		return fmt.Errorf("unsupported CI type: %s", ot)
	}

	data, err := gen.Generate(wf)
	if err != nil {
		return fmt.Errorf("failed to generate workflow: %w", err)
	}

	outPath := outputPath
	if outPath == "" {
		outPath = gen.DefaultOutputPath()
	}

	if err := writeOutput(outPath, data); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Workflow written to %s\n", outPath)
	return nil
}

func handleVisualization(g *graph.Graph, ot ciworkflow.OutputType, outputPath string, title string, groupByComponent bool) error {
	opts := visual.MermaidOptions{
		GroupByComponent: groupByComponent,
		Direction:        "TD",
		Title:            title,
	}

	if ot == ciworkflow.TypeMermaid {
		text, err := visual.RenderMermaid(g, opts)
		if err != nil {
			return fmt.Errorf("failed to render mermaid: %w", err)
		}

		if outputPath != "" {
			if err := writeOutput(outputPath, []byte(text)); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Mermaid diagram written to %s\n", outputPath)
		} else {
			fmt.Print(text)
		}
		return nil
	}

	// Image output
	imgOpts := visual.ImageOptions{
		MermaidOptions: opts,
	}

	data, err := visual.RenderImage(g, imgOpts)
	if err != nil {
		return err
	}

	outPath := outputPath
	if outPath == "" {
		outPath = "workflow.png"
	}

	if err := writeOutput(outPath, data); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Image written to %s\n", outPath)
	return nil
}

func writeOutput(path string, data []byte) error {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	return os.WriteFile(path, data, 0644)
}

// buildComponentWorkflow constructs a Workflow from a single component's graph.
func buildComponentWorkflow(comp component.Component, compName string, g *graph.Graph, componentTag, installVersion string) (ciworkflow.Workflow, error) {
	// Build component ref
	compRef := ciworkflow.ComponentRef{
		Name:    compName,
		IsLocal: true,
		Variables: make(map[string]string),
	}

	// Extract variables
	var wfVars []ciworkflow.WorkflowVariable
	envVars := map[string]string{
		"COMPONENT_IMAGE": componentTag,
		"ENVIRONMENT":     "${{ vars.CLDCTL_ENVIRONMENT }}",
		"DATACENTER":      "${{ vars.CLDCTL_DATACENTER }}",
	}

	for _, v := range comp.Variables() {
		envName := strings.ToUpper(v.Name())
		wfVar := ciworkflow.WorkflowVariable{
			Name:        v.Name(),
			EnvName:     envName,
			Sensitive:   v.Sensitive(),
			Required:    v.Required(),
			Description: v.Description(),
		}
		if v.Default() != nil {
			wfVar.Default = fmt.Sprintf("%v", v.Default())
		}
		wfVars = append(wfVars, wfVar)

		// Map to workflow-level env var
		if v.Sensitive() {
			envVars[envName] = fmt.Sprintf("${{ secrets.%s }}", envName)
		} else if v.Default() != nil {
			envVars[envName] = fmt.Sprintf("%v", v.Default())
		} else {
			envVars[envName] = fmt.Sprintf("${{ vars.%s }}", envName)
		}

		// Variable flag for apply calls
		compRef.Variables[v.Name()] = "$" + envName
	}

	// Build jobs from graph
	jobs, err := ciworkflow.BuildJobs(g, ciworkflow.ModeComponent, []ciworkflow.ComponentRef{compRef})
	if err != nil {
		return ciworkflow.Workflow{}, fmt.Errorf("failed to build jobs: %w", err)
	}

	// Collect dependency names for check-dependencies job
	var depNames []string
	for _, dep := range comp.Dependencies() {
		if !dep.Optional() {
			depNames = append(depNames, dep.Name())
		}
	}

	// Prepend setup jobs
	var allJobs []ciworkflow.Job

	// check-dependencies job (if component has dependencies)
	if len(depNames) > 0 {
		checkJob := ciworkflow.Job{
			ID:   "check-dependencies",
			Name: "Check Dependencies",
		}
		for _, depName := range depNames {
			checkJob.Steps = append(checkJob.Steps, ciworkflow.Step{
				Name: fmt.Sprintf("Check dependency '%s'", depName),
				Run:  fmt.Sprintf("cldctl get component %s -e $ENVIRONMENT -d $DATACENTER", depName),
			})
		}
		allJobs = append(allJobs, checkJob)
	}

	// build-and-push job
	buildJob := ciworkflow.Job{
		ID:            "build-and-push",
		Name:          "Build & Push",
		NeedsCheckout: true,
		Steps: []ciworkflow.Step{
			{Name: "Build component", Run: "cldctl build component . -t $COMPONENT_IMAGE"},
			{Name: "Push component", Run: "cldctl push component $COMPONENT_IMAGE"},
		},
	}
	allJobs = append(allJobs, buildJob)

	// Set dependencies on resource jobs: they depend on build-and-push (and check-dependencies if present)
	setupJobIDs := []string{"build-and-push"}
	if len(depNames) > 0 {
		setupJobIDs = append([]string{"check-dependencies"}, setupJobIDs...)
	}

	for i := range jobs {
		if len(jobs[i].DependsOn) == 0 {
			jobs[i].DependsOn = setupJobIDs
		} else {
			// Ensure build-and-push is in the dependency list
			hasBuild := false
			for _, dep := range jobs[i].DependsOn {
				if dep == "build-and-push" {
					hasBuild = true
					break
				}
			}
			if !hasBuild {
				jobs[i].DependsOn = append(setupJobIDs, jobs[i].DependsOn...)
			}
		}
	}

	allJobs = append(allJobs, jobs...)

	wf := ciworkflow.Workflow{
		Name:           fmt.Sprintf("Deploy %s", compName),
		Mode:           ciworkflow.ModeComponent,
		Jobs:           allJobs,
		EnvVars:        envVars,
		Variables:      wfVars,
		Components:     []ciworkflow.ComponentRef{compRef},
		Dependencies:   depNames,
		InstallVersion: installVersion,
		ComponentTag:   componentTag,
	}

	return wf, nil
}

// buildEnvironmentWorkflow constructs a Workflow from an environment config.
func buildEnvironmentWorkflow(env environment.Environment, components []ciworkflow.ComponentRef, g *graph.Graph, compDeps map[string][]string, installVersion string) (ciworkflow.Workflow, error) {
	// Build environment-level env vars
	envVars := map[string]string{
		"ENVIRONMENT": "preview-${{ github.event.pull_request.number }}",
		"DATACENTER":  "${{ vars.CLDCTL_DATACENTER }}",
	}

	var wfVars []ciworkflow.WorkflowVariable
	for _, v := range env.Variables() {
		envName := strings.ToUpper(v.Name())
		wfVar := ciworkflow.WorkflowVariable{
			Name:        v.Name(),
			EnvName:     envName,
			Sensitive:   v.Sensitive(),
			Required:    v.Required(),
			Description: v.Description(),
		}
		if v.Default() != nil {
			wfVar.Default = fmt.Sprintf("%v", v.Default())
		}
		wfVars = append(wfVars, wfVar)

		if v.Sensitive() {
			envVars[envName] = fmt.Sprintf("${{ secrets.%s }}", envName)
		} else if v.Default() != nil {
			envVars[envName] = fmt.Sprintf("%v", v.Default())
		} else {
			envVars[envName] = fmt.Sprintf("${{ vars.%s }}", envName)
		}
	}

	// Build resource jobs
	jobs, err := ciworkflow.BuildJobs(g, ciworkflow.ModeEnvironment, components)
	if err != nil {
		return ciworkflow.Workflow{}, fmt.Errorf("failed to build jobs: %w", err)
	}

	// Prepend setup jobs
	var allJobs []ciworkflow.Job

	// create-environment job
	createEnvJob := ciworkflow.Job{
		ID:   "create-environment",
		Name: "Create Environment",
		Steps: []ciworkflow.Step{
			{Name: "Create environment", Run: "cldctl create environment $ENVIRONMENT -d $DATACENTER"},
		},
	}
	allJobs = append(allJobs, createEnvJob)

	// Per-component setup jobs (build-and-push for local, pull for OCI)
	var setupJobIDs []string
	setupJobIDs = append(setupJobIDs, "create-environment")

	for _, comp := range components {
		if comp.IsLocal {
			jobID := fmt.Sprintf("build-%s", sanitizeJobComponent(comp.Name))
			buildJob := ciworkflow.Job{
				ID:            jobID,
				Name:          fmt.Sprintf("Build %s", comp.Name),
				NeedsCheckout: true,
				Steps: []ciworkflow.Step{
					{Name: "Build component", Run: fmt.Sprintf("cldctl build component %s -t %s", comp.Path, comp.Name+":${{ github.sha }}")},
					{Name: "Push component", Run: fmt.Sprintf("cldctl push component %s", comp.Name+":${{ github.sha }}")},
				},
			}
			allJobs = append(allJobs, buildJob)
			setupJobIDs = append(setupJobIDs, jobID)
		} else if comp.Image != "" {
			jobID := fmt.Sprintf("pull-%s", sanitizeJobComponent(comp.Name))
			pullJob := ciworkflow.Job{
				ID:   jobID,
				Name: fmt.Sprintf("Pull %s", comp.Name),
				Steps: []ciworkflow.Step{
					{Name: fmt.Sprintf("Pull %s component", comp.Name), Run: fmt.Sprintf("cldctl pull component %s", comp.Image)},
				},
			}
			allJobs = append(allJobs, pullJob)
			setupJobIDs = append(setupJobIDs, jobID)
		}
	}

	// Make resource jobs depend on their component's setup job + create-environment
	for i := range jobs {
		compSetupID := ""
		for _, comp := range components {
			if comp.Name == jobs[i].Component {
				if comp.IsLocal {
					compSetupID = fmt.Sprintf("build-%s", sanitizeJobComponent(comp.Name))
				} else if comp.Image != "" {
					compSetupID = fmt.Sprintf("pull-%s", sanitizeJobComponent(comp.Name))
				}
				break
			}
		}

		if len(jobs[i].DependsOn) == 0 {
			deps := []string{"create-environment"}
			if compSetupID != "" {
				deps = append(deps, compSetupID)
			}
			jobs[i].DependsOn = deps
		}
	}

	allJobs = append(allJobs, jobs...)

	// Build teardown jobs
	teardownJobs := ciworkflow.BuildTeardownJobs(components, compDeps)

	wf := ciworkflow.Workflow{
		Name:           "Preview Deploy",
		Mode:           ciworkflow.ModeEnvironment,
		Jobs:           allJobs,
		EnvVars:        envVars,
		Variables:      wfVars,
		TeardownJobs:   teardownJobs,
		Components:     components,
		InstallVersion: installVersion,
	}

	return wf, nil
}

func sanitizeJobComponent(name string) string {
	r := strings.NewReplacer("/", "-", ".", "-", " ", "-")
	return r.Replace(name)
}
