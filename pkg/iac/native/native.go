package native

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/davidthor/cldctl/pkg/iac"
)

func init() {
	iac.Register("native", func() (iac.Plugin, error) {
		return NewPlugin()
	})
}

// Plugin implements the IaC plugin interface for native execution.
type Plugin struct {
	docker  *DockerClient
	process *ProcessManager
}

// NewPlugin creates a new native plugin instance.
func NewPlugin() (*Plugin, error) {
	docker, err := NewDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &Plugin{
		docker:  docker,
		process: NewProcessManager(),
	}, nil
}

func (p *Plugin) Name() string {
	return "native"
}

func (p *Plugin) Apply(ctx context.Context, opts iac.RunOptions) (*iac.ApplyResult, error) {
	// Load module definition
	module, err := LoadModule(opts.ModuleSource)
	if err != nil {
		return nil, fmt.Errorf("failed to load module: %w", err)
	}

	// Load existing state (if any)
	var existingState *State
	if opts.StateReader != nil {
		existingState, err = p.loadState(opts.StateReader)
		if err != nil {
			return nil, fmt.Errorf("failed to load state: %w", err)
		}
	}

	// Resolve inputs
	resolvedInputs, err := p.resolveInputs(module.Inputs, opts.Inputs)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve inputs: %w", err)
	}

	// Apply each resource in order
	state := &State{
		ModulePath: opts.ModuleSource,
		Inputs:     resolvedInputs,
		Resources:  make(map[string]*ResourceState),
		Outputs:    make(map[string]interface{}),
	}

	// Build evaluation context
	evalCtx := &EvalContext{
		Inputs:    resolvedInputs,
		Resources: state.Resources,
	}

	for name, resource := range module.Resources {
		// Check for context cancellation before each resource
		if ctx.Err() != nil {
			p.rollback(ctx, state)
			return nil, ctx.Err()
		}

		resourceState, err := p.applyResource(ctx, name, resource, evalCtx, existingState)
		if err != nil {
			// Rollback on failure
			p.rollback(ctx, state)
			return nil, fmt.Errorf("failed to apply resource %s: %w", name, err)
		}
		state.Resources[name] = resourceState
		evalCtx.Resources = state.Resources
	}

	// Resolve outputs
	outputs := make(map[string]iac.OutputValue)
	for name, outputDef := range module.Outputs {
		value, err := evaluateExpression(outputDef.Value, evalCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate output %s: %w", name, err)
		}
		state.Outputs[name] = value
		outputs[name] = iac.OutputValue{
			Value:     value,
			Sensitive: outputDef.Sensitive,
		}
	}

	// Serialize state
	stateBytes, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize state: %w", err)
	}

	return &iac.ApplyResult{
		Outputs: outputs,
		State:   stateBytes,
	}, nil
}

func (p *Plugin) Destroy(ctx context.Context, opts iac.RunOptions) error {
	if opts.StateReader == nil {
		return nil // Nothing to destroy
	}

	state, err := p.loadState(opts.StateReader)
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// Destroy in reverse order
	for name, rs := range state.Resources {
		if destroyErr := p.destroyResource(ctx, name, rs); destroyErr != nil {
			// Log but continue destroying other resources
			if opts.Stderr != nil {
				fmt.Fprintf(opts.Stderr, "warning: failed to destroy %s: %v\n", name, destroyErr)
			}
		}
	}

	return nil
}

func (p *Plugin) Preview(ctx context.Context, opts iac.RunOptions) (*iac.PreviewResult, error) {
	// Native plugin doesn't support preview/drift detection
	// Return empty preview indicating all resources will be created/updated
	return &iac.PreviewResult{
		Changes: []iac.ResourceChange{},
	}, nil
}

func (p *Plugin) Refresh(ctx context.Context, opts iac.RunOptions) (*iac.RefreshResult, error) {
	// Native plugin trusts stored state, no refresh needed
	return &iac.RefreshResult{}, nil
}

func (p *Plugin) loadState(reader io.Reader) (*State, error) {
	var state State
	if err := json.NewDecoder(reader).Decode(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (p *Plugin) resolveInputs(defs map[string]InputDef, provided map[string]interface{}) (map[string]interface{}, error) {
	resolved := make(map[string]interface{})

	for name, def := range defs {
		if value, ok := provided[name]; ok {
			resolved[name] = value
		} else if def.Default != nil {
			resolved[name] = def.Default
		} else if def.Required {
			return nil, fmt.Errorf("required input %s not provided", name)
		}
	}

	return resolved, nil
}

func (p *Plugin) applyResource(ctx context.Context, name string, resource Resource, evalCtx *EvalContext, existing *State) (*ResourceState, error) {
	// Resolve properties with expressions
	props, err := resolveProperties(resource.Properties, evalCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve properties: %w", err)
	}

	switch resource.Type {
	case "docker:container":
		return p.applyDockerContainer(ctx, name, props, existing)
	case "docker:network":
		return p.applyDockerNetwork(ctx, name, props, existing)
	case "docker:volume":
		return p.applyDockerVolume(ctx, name, props, existing)
	case "docker:build":
		return p.applyDockerBuild(ctx, name, props, existing)
	case "process":
		return p.applyProcess(ctx, name, props, existing)
	case "exec":
		return p.applyExec(ctx, name, props)
	default:
		return nil, fmt.Errorf("unknown resource type: %s", resource.Type)
	}
}

func (p *Plugin) destroyResource(ctx context.Context, name string, rs *ResourceState) error {
	switch rs.Type {
	case "docker:container":
		if id, ok := rs.ID.(string); ok {
			return p.docker.RemoveContainer(ctx, id)
		}
	case "docker:network":
		if id, ok := rs.ID.(string); ok {
			return p.docker.RemoveNetwork(ctx, id)
		}
	case "docker:volume":
		if id, ok := rs.ID.(string); ok {
			return p.docker.RemoveVolume(ctx, id)
		}
	case "docker:build":
		// Optionally remove the built image
		if imageID, ok := rs.ID.(string); ok && imageID != "" {
			_ = p.docker.RemoveImage(ctx, imageID, false)
		}
		return nil
	case "process":
		if processName, ok := rs.ID.(string); ok {
			return p.process.StopProcess(processName, 10*time.Second)
		}
	case "exec":
		return nil // One-time execution, nothing to destroy
	}
	return nil
}

func (p *Plugin) rollback(ctx context.Context, state *State) {
	for name, rs := range state.Resources {
		_ = p.destroyResource(ctx, name, rs)
	}
}

func (p *Plugin) applyDockerContainer(ctx context.Context, name string, props map[string]interface{}, existing *State) (*ResourceState, error) {
	containerName := getString(props, "name")
	desiredImage := getString(props, "image")

	// Build desired options for comparison
	opts := ContainerOptions{
		Image:       desiredImage,
		Name:        containerName,
		Command:     getStringSlice(props, "command"),
		Entrypoint:  getStringSlice(props, "entrypoint"),
		Environment: getStringMap(props, "environment"),
		Ports:       getPortMappings(props, "ports"),
		Volumes:     getVolumeMounts(props, "volumes"),
		Network:     getString(props, "network"),
		Restart:     getString(props, "restart"),
		LogDriver:   getString(props, "log_driver"),
		LogOptions:  getStringMap(props, "log_options"),
	}

	// Check if container already exists and is running (from state)
	if existing != nil {
		if rs, ok := existing.Resources[name]; ok {
			if containerID, ok := rs.ID.(string); ok {
				running, err := p.docker.IsContainerRunning(ctx, containerID)
				if err == nil && running {
					// Check if container config matches what we want
					if p.docker.ContainerMatchesConfig(ctx, containerID, opts) {
						// Container still running with same config, reuse it
						return rs, nil
					}
				}
				// Container stopped, missing, or config changed - remove it
				_ = p.docker.RemoveContainer(ctx, containerID)
			}
		}
	}

	// Also check by container name in case state was lost but container exists
	// This handles orphaned containers from failed previous runs
	if containerName != "" {
		if existingID, _ := p.docker.GetContainerByName(ctx, containerName); existingID != "" {
			running, _ := p.docker.IsContainerRunning(ctx, existingID)
			if running && p.docker.ContainerMatchesConfig(ctx, existingID, opts) {
				// Existing container matches config, reuse it
				info, err := p.docker.InspectContainer(ctx, existingID)
				if err == nil {
					return &ResourceState{
						Type:       "docker:container",
						ID:         existingID,
						Properties: props,
						Outputs: map[string]interface{}{
							"container_id": existingID,
							"ports":        info.Ports,
						},
					}, nil
				}
			}
			// Config changed or container not running, remove it
			_ = p.docker.RemoveContainer(ctx, existingID)
		}
	}

	// Create and start container
	containerID, err := p.docker.RunContainer(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Get container info
	info, err := p.docker.InspectContainer(ctx, containerID)
	if err != nil {
		_ = p.docker.RemoveContainer(ctx, containerID)
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	return &ResourceState{
		Type:       "docker:container",
		ID:         containerID,
		Properties: props,
		Outputs: map[string]interface{}{
			"container_id": containerID,
			"ports":        info.Ports,
			"environment":  opts.Environment, // Include environment for dependent resources
			"name":         containerName,
		},
	}, nil
}

func (p *Plugin) applyDockerNetwork(ctx context.Context, name string, props map[string]interface{}, existing *State) (*ResourceState, error) {
	networkName := getString(props, "name")

	// Check if network already exists
	if existing != nil {
		if rs, ok := existing.Resources[name]; ok {
			if networkID, ok := rs.ID.(string); ok {
				exists, err := p.docker.NetworkExists(ctx, networkID)
				if err == nil && exists {
					return rs, nil
				}
			}
		}
	}

	networkID, err := p.docker.CreateNetwork(ctx, networkName)
	if err != nil {
		return nil, err
	}

	return &ResourceState{
		Type:       "docker:network",
		ID:         networkID,
		Properties: props,
		Outputs: map[string]interface{}{
			"network_id": networkID,
			"name":       networkName,
		},
	}, nil
}

func (p *Plugin) applyDockerVolume(ctx context.Context, name string, props map[string]interface{}, existing *State) (*ResourceState, error) {
	volumeName := getString(props, "name")

	// Check if volume already exists
	if existing != nil {
		if rs, ok := existing.Resources[name]; ok {
			if volumeID, ok := rs.ID.(string); ok {
				exists, err := p.docker.VolumeExists(ctx, volumeID)
				if err == nil && exists {
					return rs, nil
				}
			}
		}
	}

	volumeID, err := p.docker.CreateVolume(ctx, volumeName)
	if err != nil {
		return nil, err
	}

	return &ResourceState{
		Type:       "docker:volume",
		ID:         volumeID,
		Properties: props,
		Outputs: map[string]interface{}{
			"volume_id": volumeID,
			"name":      volumeName,
		},
	}, nil
}

func (p *Plugin) applyDockerBuild(ctx context.Context, name string, props map[string]interface{}, existing *State) (*ResourceState, error) {
	buildContext := getString(props, "context")
	dockerfile := getString(props, "dockerfile")
	target := getString(props, "target")

	// Make dockerfile path relative to build context if it's absolute
	if dockerfile != "" && filepath.IsAbs(dockerfile) && buildContext != "" {
		absContext, err := filepath.Abs(buildContext)
		if err == nil {
			if relPath, err := filepath.Rel(absContext, dockerfile); err == nil {
				dockerfile = relPath
			}
		}
	}

	// Get tags
	var tags []string
	if tagsVal, ok := props["tags"]; ok {
		if tagsSlice, ok := tagsVal.([]interface{}); ok {
			for _, t := range tagsSlice {
				if tagStr, ok := t.(string); ok {
					tags = append(tags, tagStr)
				}
			}
		} else if tagStr, ok := tagsVal.(string); ok {
			tags = append(tags, tagStr)
		}
	}

	// Get build args
	buildArgs := make(map[string]string)
	if argsVal, ok := props["args"]; ok {
		if argsMap, ok := argsVal.(map[string]interface{}); ok {
			for k, v := range argsMap {
				if vStr, ok := v.(string); ok {
					buildArgs[k] = vStr
				}
			}
		}
	}

	// Check cache setting
	noCache := false
	if cacheVal, ok := props["cache"]; ok {
		if cacheBool, ok := cacheVal.(bool); ok {
			noCache = !cacheBool
		}
	}

	// Build the image using the existing BuildOptions type
	opts := BuildOptions{
		Context:    buildContext,
		Dockerfile: dockerfile,
		Target:     target,
		Tags:       tags,
		BuildArgs:  buildArgs,
		NoCache:    noCache,
	}

	// Stream build output in debug mode
	if os.Getenv("CLDCTL_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[debug] Building image: context=%s, dockerfile=%s, tags=%v\n", buildContext, dockerfile, tags)
		opts.Stderr = os.Stderr
	}

	// Add timeout for builds (10 minutes default, configurable via env)
	buildTimeout := 10 * time.Minute
	if timeoutStr := os.Getenv("CLDCTL_BUILD_TIMEOUT"); timeoutStr != "" {
		if d, err := time.ParseDuration(timeoutStr); err == nil {
			buildTimeout = d
		}
	}

	buildCtx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()

	buildResult, err := p.docker.BuildImage(buildCtx, opts)
	if err != nil {
		if buildCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("build timed out after %v", buildTimeout)
		}
		if os.Getenv("CLDCTL_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "[debug] Build failed: %v\n", err)
		}
		return nil, err
	}

	if os.Getenv("CLDCTL_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[debug] Build succeeded: imageID=%s\n", buildResult.ImageID)
	}

	// Determine the primary tag for output
	primaryTag := ""
	if len(tags) > 0 {
		primaryTag = tags[0]
	}

	return &ResourceState{
		Type:       "docker:build",
		ID:         buildResult.ImageID,
		Properties: props,
		Outputs: map[string]interface{}{
			"id":    buildResult.ImageID,
			"image": primaryTag,
			"tags":  tags,
		},
	}, nil
}

func (p *Plugin) applyExec(ctx context.Context, name string, props map[string]interface{}) (*ResourceState, error) {
	command := getStringSlice(props, "command")
	workDir := getString(props, "working_dir")
	env := getStringMap(props, "environment")
	imageName := getString(props, "image")
	networkName := getString(props, "network")

	var output string
	var err error

	if imageName != "" {
		// Run in a Docker container
		output, err = p.docker.RunOneShot(ctx, RunOneShotOptions{
			Image:       imageName,
			Command:     command,
			Environment: env,
			Network:     networkName,
			WorkDir:     workDir,
		})
	} else {
		// Run on host
		output, err = p.docker.Exec(ctx, command, workDir, env)
	}

	if err != nil {
		return nil, err
	}

	return &ResourceState{
		Type:       "exec",
		ID:         name,
		Properties: props,
		Outputs: map[string]interface{}{
			"output": output,
		},
	}, nil
}

func (p *Plugin) applyProcess(ctx context.Context, name string, props map[string]interface{}, existing *State) (*ResourceState, error) {
	// Check if process already exists and is running
	processName := getString(props, "name")
	if existing != nil {
		if rs, ok := existing.Resources[name]; ok {
			if pName, ok := rs.ID.(string); ok && p.process.IsProcessRunning(pName) {
				// Process still running, reuse it
				return rs, nil
			}
		}
	}

	// Get environment variables first
	env := getStringMap(props, "environment")

	// Pre-assign PORT if set to "auto" (legacy compatibility)
	if portVal, ok := env["PORT"]; ok && portVal == "auto" {
		port, err := findAvailablePort()
		if err != nil {
			return nil, fmt.Errorf("failed to find available port: %w", err)
		}
		env["PORT"] = strconv.Itoa(port)
	}

	// Parse readiness check if present
	// Skip readiness check entirely when the endpoint port is 0 (no service exposed)
	var readiness *ReadinessCheck
	if readinessMap, ok := props["readiness"].(map[string]interface{}); ok {
		endpoint := getString(readinessMap, "endpoint")
		// Substitute PORT if present (legacy compatibility)
		if port, ok := env["PORT"]; ok {
			endpoint = strings.ReplaceAll(endpoint, "${self.environment.PORT}", port)
		}

		// Guard: skip readiness check if the endpoint references port 0
		if strings.Contains(endpoint, "localhost:0") || strings.Contains(endpoint, "localhost:0/") {
			readiness = nil
		} else {
			readiness = &ReadinessCheck{
				Type:     getString(readinessMap, "type"),
				Endpoint: endpoint,
				Interval: parseDuration(getString(readinessMap, "interval"), 2*time.Second),
				Timeout:  parseDuration(getString(readinessMap, "timeout"), 120*time.Second),
			}
		}
	}

	// Start the process
	opts := ProcessOptions{
		Name:        processName,
		WorkingDir:  getString(props, "working_dir"),
		Command:     getStringSlice(props, "command"),
		Environment: env,
		Readiness:   readiness,
	}

	info, err := p.process.StartProcess(ctx, opts)
	if err != nil {
		return nil, err
	}

	return &ResourceState{
		Type:       "process",
		ID:         processName,
		Properties: props,
		Outputs: map[string]interface{}{
			"pid":         info.PID,
			"environment": info.Environment,
		},
	}, nil
}

// Helper functions for type conversion

func getString(props map[string]interface{}, key string) string {
	if v, ok := props[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getStringSlice(props map[string]interface{}, key string) []string {
	if v, ok := props[key]; ok {
		switch val := v.(type) {
		case []interface{}:
			result := make([]string, len(val))
			for i, item := range val {
				result[i], _ = item.(string)
			}
			return result
		case []string:
			return val
		case string:
			// Handle string commands by splitting on spaces (e.g., "pnpm dev --filter=app")
			if val != "" {
				return strings.Fields(val)
			}
		}
	}
	return nil
}

func getStringMap(props map[string]interface{}, key string) map[string]string {
	if v, ok := props[key]; ok {
		switch m := v.(type) {
		case map[string]interface{}:
			result := make(map[string]string, len(m))
			for k, val := range m {
				result[k], _ = val.(string)
			}
			return result
		case map[string]string:
			return m
		}
	}
	return nil
}

func getPortMappings(props map[string]interface{}, key string) []PortMapping {
	if v, ok := props[key]; ok {
		if arr, ok := v.([]interface{}); ok {
			var result []PortMapping
			for _, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					pm := PortMapping{}
					if container, ok := m["container"].(int); ok {
						pm.ContainerPort = container
					}
					if host, ok := m["host"]; ok {
						if hostInt, ok := host.(int); ok {
							pm.HostPort = hostInt
						} else if hostStr, ok := host.(string); ok && hostStr == "auto" {
							pm.HostPort = 0 // Auto-assign
						}
					}
					result = append(result, pm)
				}
			}
			return result
		}
	}
	return nil
}

func getVolumeMounts(props map[string]interface{}, key string) []VolumeMount {
	if v, ok := props[key]; ok {
		if arr, ok := v.([]interface{}); ok {
			var result []VolumeMount
			for _, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					vm := VolumeMount{
						Name:   getString(m, "name"),
						Source: getString(m, "source"),
						Path:   getString(m, "path"),
					}
					result = append(result, vm)
				}
			}
			return result
		}
	}
	return nil
}

func getString2(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func parseDuration(s string, defaultDuration time.Duration) time.Duration {
	if s == "" {
		return defaultDuration
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultDuration
	}
	return d
}
