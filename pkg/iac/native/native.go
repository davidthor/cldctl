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
	"sync"
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

	// portRegistry maps "containerName:containerPort" → hostPort.
	// Populated when Docker containers are created or found running,
	// used by resolve_to_localhost to rewrite container-network URLs
	// to localhost URLs for host-based processes.
	portRegistry   map[string]int
	portRegistryMu sync.Mutex
}

// NewPlugin creates a new native plugin instance.
func NewPlugin() (*Plugin, error) {
	docker, err := NewDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &Plugin{
		docker:       docker,
		process:      NewProcessManager(),
		portRegistry: make(map[string]int),
	}, nil
}

// registerContainerPorts records port mappings from a Docker container so that
// process-based workloads can resolve container-network URLs to localhost.
func (p *Plugin) registerContainerPorts(containerName string, ports []interface{}) {
	p.portRegistryMu.Lock()
	defer p.portRegistryMu.Unlock()
	for _, port := range ports {
		if pm, ok := port.(map[string]interface{}); ok {
			containerPort := 0
			hostPort := 0
			if v, ok := pm["container"]; ok {
				containerPort = toInt(v)
			}
			if v, ok := pm["host"]; ok {
				hostPort = toInt(v)
			}
			if containerPort > 0 && hostPort > 0 {
				key := fmt.Sprintf("%s:%d", containerName, containerPort)
				p.portRegistry[key] = hostPort
			}
		}
	}
}

// resolveContainerRefsToLocalhost replaces Docker container-name:port references
// in environment variable values with localhost:hostPort using the port registry.
// This is the inverse of resolve_localhost: it converts Docker-network URLs to
// host-accessible URLs for workloads running directly on the host.
func (p *Plugin) resolveContainerRefsToLocalhost(env map[string]string) map[string]string {
	p.portRegistryMu.Lock()
	defer p.portRegistryMu.Unlock()

	result := make(map[string]string, len(env))
	for k, v := range env {
		for containerPortKey, hostPort := range p.portRegistry {
			v = strings.ReplaceAll(v, containerPortKey, fmt.Sprintf("localhost:%d", hostPort))
		}
		result[k] = v
	}
	return result
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

	// Iterate resources in YAML declaration order (ResourceOrder) so that
	// sequentially-declared resources execute top-to-bottom. Fall back to
	// unordered map iteration when ResourceOrder is empty (legacy modules).
	resourceNames := module.ResourceOrder
	if len(resourceNames) == 0 {
		resourceNames = make([]string, 0, len(module.Resources))
		for name := range module.Resources {
			resourceNames = append(resourceNames, name)
		}
	}

	for _, name := range resourceNames {
		resource, ok := module.Resources[name]
		if !ok {
			continue
		}

		// Check for context cancellation before each resource
		if ctx.Err() != nil {
			p.rollback(ctx, state)
			return nil, ctx.Err()
		}

		// Evaluate when condition — skip resource if condition is false
		if resource.When != "" {
			condResult, err := evaluateExpression(resource.When, evalCtx)
			if err != nil {
				p.rollback(ctx, state)
				return nil, fmt.Errorf("failed to evaluate when condition for resource %s: %w", name, err)
			}

			if !isTruthy(condResult) {
				continue
			}
		}

		resourceState, err := p.applyResource(ctx, name, resource, evalCtx, existingState, opts.Stdout, opts.Stderr, opts.OnProgress)
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

func (p *Plugin) Import(ctx context.Context, opts iac.ImportOptions) (*iac.ImportResult, error) {
	// Native plugin supports importing Docker containers, networks, and volumes
	// by inspecting them with Docker commands.
	outputs := make(map[string]iac.OutputValue)
	var imported []string

	state := &State{
		ModulePath: opts.ModuleSource,
		Inputs:     opts.Inputs,
		Resources:  make(map[string]*ResourceState),
		Outputs:    make(map[string]interface{}),
	}

	for _, mapping := range opts.Mappings {
		// Determine resource type from the address or by probing Docker
		// The address should follow native resource naming: "container", "network", "volume"
		resourceType := ""
		switch {
		case strings.Contains(mapping.Address, "container"):
			resourceType = "docker:container"
		case strings.Contains(mapping.Address, "network"):
			resourceType = "docker:network"
		case strings.Contains(mapping.Address, "volume"):
			resourceType = "docker:volume"
		default:
			// Try to inspect as container first
			info, err := p.docker.InspectContainer(ctx, mapping.ID)
			if err == nil && info != nil {
				resourceType = "docker:container"
			} else {
				return nil, fmt.Errorf("cannot determine resource type for %s; native import supports docker containers, networks, and volumes", mapping.Address)
			}
		}

		switch resourceType {
		case "docker:container":
			info, err := p.docker.InspectContainer(ctx, mapping.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to inspect container %s: %w", mapping.ID, err)
			}
			rs := &ResourceState{
				Type: "docker:container",
				ID:   mapping.ID,
				Properties: map[string]interface{}{
					"name": info.Name,
				},
				Outputs: map[string]interface{}{
					"container_id": mapping.ID,
					"name":         info.Name,
					"ports":        info.Ports,
				},
			}
			state.Resources[mapping.Address] = rs
			for k, v := range rs.Outputs {
				outputs[k] = iac.OutputValue{Value: v}
			}
			imported = append(imported, mapping.Address)

		case "docker:network":
			// Verify network exists
			exists, err := p.docker.NetworkExists(ctx, mapping.ID)
			if err != nil || !exists {
				return nil, fmt.Errorf("network %s not found: %w", mapping.ID, err)
			}
			rs := &ResourceState{
				Type: "docker:network",
				ID:   mapping.ID,
				Outputs: map[string]interface{}{
					"network_id": mapping.ID,
				},
			}
			state.Resources[mapping.Address] = rs
			imported = append(imported, mapping.Address)

		case "docker:volume":
			exists, err := p.docker.VolumeExists(ctx, mapping.ID)
			if err != nil || !exists {
				return nil, fmt.Errorf("volume %s not found: %w", mapping.ID, err)
			}
			rs := &ResourceState{
				Type: "docker:volume",
				ID:   mapping.ID,
				Outputs: map[string]interface{}{
					"volume_id": mapping.ID,
				},
			}
			state.Resources[mapping.Address] = rs
			imported = append(imported, mapping.Address)
		}
	}

	// Serialize state
	stateBytes, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize state: %w", err)
	}

	return &iac.ImportResult{
		Outputs:           outputs,
		State:             stateBytes,
		ImportedResources: imported,
	}, nil
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

// resolveDestroyCommand evaluates all expressions in a DestroyCommand using the
// current eval context and returns a fully resolved command ready for state persistence.
func resolveDestroyCommand(dc *DestroyCommand, evalCtx *EvalContext) (*ResolvedDestroyCommand, error) {
	resolved := &ResolvedDestroyCommand{}

	// Resolve command list
	for _, item := range dc.Command {
		val, err := resolveValue(item, evalCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve destroy command element: %w", err)
		}
		resolved.Command = append(resolved.Command, fmt.Sprintf("%v", val))
	}

	// Resolve image
	if dc.Image != "" {
		val, err := evaluateExpression(dc.Image, evalCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve destroy image: %w", err)
		}
		resolved.Image = fmt.Sprintf("%v", val)
	}

	// Resolve network
	if dc.Network != "" {
		val, err := evaluateExpression(dc.Network, evalCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve destroy network: %w", err)
		}
		resolved.Network = fmt.Sprintf("%v", val)
	}

	// Resolve working directory
	if dc.WorkDir != "" {
		val, err := evaluateExpression(dc.WorkDir, evalCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve destroy working_dir: %w", err)
		}
		resolved.WorkDir = fmt.Sprintf("%v", val)
	}

	// Resolve environment variables
	if len(dc.Environment) > 0 {
		resolved.Environment = make(map[string]string, len(dc.Environment))
		for k, v := range dc.Environment {
			val, err := resolveValue(v, evalCtx)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve destroy env var %s: %w", k, err)
			}
			resolved.Environment[k] = fmt.Sprintf("%v", val)
		}
	}

	return resolved, nil
}

func (p *Plugin) applyResource(ctx context.Context, name string, resource Resource, evalCtx *EvalContext, existing *State, stdout, stderr io.Writer, onProgress func(string)) (*ResourceState, error) {
	// Resolve properties with expressions
	props, err := resolveProperties(resource.Properties, evalCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve properties: %w", err)
	}

	var rs *ResourceState
	switch resource.Type {
	case "docker:container":
		rs, err = p.applyDockerContainer(ctx, name, props, existing, onProgress)
	case "docker:network":
		rs, err = p.applyDockerNetwork(ctx, name, props, existing)
	case "docker:volume":
		rs, err = p.applyDockerVolume(ctx, name, props, existing)
	case "docker:build":
		rs, err = p.applyDockerBuild(ctx, name, props, existing, stdout, stderr)
	case "process":
		rs, err = p.applyProcess(ctx, name, props, existing, stdout, stderr)
	case "exec":
		rs, err = p.applyExec(ctx, name, props)
	case "crypto:rsa_key":
		rs, err = p.applyCryptoRSAKey(name, props)
	case "crypto:ecdsa_key":
		rs, err = p.applyCryptoECDSAKey(name, props)
	case "crypto:symmetric_key":
		rs, err = p.applyCryptoSymmetricKey(name, props)
	default:
		return nil, fmt.Errorf("unknown resource type: %s", resource.Type)
	}

	if err != nil {
		return nil, err
	}

	// If the resource defines a destroy command, resolve its expressions now
	// and persist in state so it's available during teardown.
	if resource.Destroy != nil {
		resolved, resolveErr := resolveDestroyCommand(resource.Destroy, evalCtx)
		if resolveErr != nil {
			return nil, fmt.Errorf("failed to resolve destroy command: %w", resolveErr)
		}
		rs.DestroyCmd = resolved
	}

	return rs, nil
}

func (p *Plugin) destroyResource(ctx context.Context, name string, rs *ResourceState) error {
	// If the resource has a custom destroy command, execute it instead of
	// (or in addition to) the default teardown for this resource type.
	if rs.DestroyCmd != nil && len(rs.DestroyCmd.Command) > 0 {
		if err := p.executeDestroyCommand(ctx, rs.DestroyCmd); err != nil {
			return fmt.Errorf("destroy command failed: %w", err)
		}
	}

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
		return nil // One-time execution, nothing to destroy (unless destroy cmd handled above)
	}
	return nil
}

// executeDestroyCommand runs a resolved destroy command, either on the host
// or inside a Docker container (when Image is set).
func (p *Plugin) executeDestroyCommand(ctx context.Context, cmd *ResolvedDestroyCommand) error {
	if cmd.Image != "" {
		_, err := p.docker.RunOneShot(ctx, RunOneShotOptions{
			Image:       cmd.Image,
			Command:     cmd.Command,
			Environment: cmd.Environment,
			Network:     cmd.Network,
			WorkDir:     cmd.WorkDir,
		})
		return err
	}

	_, err := p.docker.Exec(ctx, cmd.Command, cmd.WorkDir, cmd.Environment)
	return err
}

func (p *Plugin) rollback(ctx context.Context, state *State) {
	for name, rs := range state.Resources {
		_ = p.destroyResource(ctx, name, rs)
	}
}

func (p *Plugin) applyDockerContainer(ctx context.Context, name string, props map[string]interface{}, existing *State, onProgress func(string)) (*ResourceState, error) {
	containerName := getString(props, "name")
	desiredImage := getString(props, "image")

	// Build desired options for comparison
	opts := ContainerOptions{
		Image:            desiredImage,
		Name:             containerName,
		Command:          getStringSlice(props, "command"),
		Entrypoint:       getStringSlice(props, "entrypoint"),
		Environment:      getStringMap(props, "environment"),
		Ports:            getPortMappings(props, "ports"),
		Volumes:          getVolumeMounts(props, "volumes"),
		Network:          getString(props, "network"),
		Restart:          getString(props, "restart"),
		LogDriver:        getString(props, "log_driver"),
		LogOptions:       getStringMap(props, "log_options"),
		Healthcheck:      getHealthcheck(props, "healthcheck"),
		ExtraHosts:       getStringSlice(props, "extra_hosts"),
		ResolveLocalhost: getBool(props, "resolve_localhost"),
		Wait:             getBool(props, "wait"),
		OnProgress:       onProgress,
	}


	// Check if container already exists and is running (from state)
	if existing != nil {
		if rs, ok := existing.Resources[name]; ok {
			if containerID, ok := rs.ID.(string); ok {
				running, err := p.docker.IsContainerRunning(ctx, containerID)
				if err == nil && running {
					// Check if container config matches what we want
					if p.docker.ContainerMatchesConfig(ctx, containerID, opts) {
						// Register port mappings from existing container for resolve_to_localhost
						if ports, ok := rs.Outputs["ports"].([]interface{}); ok {
							p.registerContainerPorts(containerName, ports)
						}
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
					portsArray := buildPortsArray(opts.Ports, info.Ports)
					p.registerContainerPorts(containerName, portsArray)
					return &ResourceState{
						Type:       "docker:container",
						ID:         existingID,
						Properties: props,
						Outputs: map[string]interface{}{
							"container_id": existingID,
							"ports":        portsArray,
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

	portsArray := buildPortsArray(opts.Ports, info.Ports)
	p.registerContainerPorts(containerName, portsArray)

	return &ResourceState{
		Type:       "docker:container",
		ID:         containerID,
		Properties: props,
		Outputs: map[string]interface{}{
			"container_id": containerID,
			"ports":        portsArray,
			"environment":  opts.Environment, // Include environment for dependent resources
			"name":         containerName,
		},
	}, nil
}

// buildPortsArray converts Docker inspect ports into an ordered array matching the
// original port declarations. This is critical because module outputs reference ports
// by array index (e.g., ${resources.container.ports[0].host}), so the order must be
// deterministic and match the YAML declaration order.
//
// declaredPorts provides the declaration order; inspectPorts provides the actual
// host port assignments from Docker.
func buildPortsArray(declaredPorts []PortMapping, inspectPorts map[string]int) []interface{} {
	var result []interface{}
	for _, pm := range declaredPorts {
		protocol := pm.Protocol
		if protocol == "" {
			protocol = "tcp"
		}
		key := fmt.Sprintf("%d/%s", pm.ContainerPort, protocol)
		hostPort := inspectPorts[key]
		result = append(result, map[string]interface{}{
			"container": pm.ContainerPort,
			"host":      hostPort,
		})
	}
	return result
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

func (p *Plugin) applyDockerBuild(ctx context.Context, name string, props map[string]interface{}, existing *State, stdout, stderr io.Writer) (*ResourceState, error) {
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

	// Pass through output writers for log capture
	opts.Stdout = stdout
	opts.Stderr = stderr

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
		// Resolve Docker container-network URLs to localhost for host-based execution
		if getBool(props, "resolve_to_localhost") {
			env = p.resolveContainerRefsToLocalhost(env)
		}
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

func (p *Plugin) applyProcess(ctx context.Context, name string, props map[string]interface{}, existing *State, stdout, stderr io.Writer) (*ResourceState, error) {
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

	// Resolve Docker container-network URLs to localhost for host-based processes
	if getBool(props, "resolve_to_localhost") {
		env = p.resolveContainerRefsToLocalhost(env)
	}

	// Parse readiness check if present
	// Skip readiness check entirely when the endpoint port is 0 (no service exposed)
	var readiness *ReadinessCheck
	if readinessMap, ok := props["readiness"].(map[string]interface{}); ok {
		endpoint := getString(readinessMap, "endpoint")

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
		Stdout:      stdout,
		Stderr:      stderr,
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

// isTruthy determines whether a value returned by evaluateExpression is "true".
// It handles booleans, the strings "true"/"false", and nil.
func isTruthy(v interface{}) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true"
	default:
		return false
	}
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

func getBool(props map[string]interface{}, key string) bool {
	if v, ok := props[key]; ok {
		switch b := v.(type) {
		case bool:
			return b
		case string:
			return b == "true"
		}
	}
	return false
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
			// Run string commands through a shell so that variable expansion,
			// piping, and other shell syntax work correctly. This matches
			// the behavior of Docker CMD strings and npm scripts.
			if val != "" {
				return []string{"sh", "-c", val}
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
				if s, ok := val.(string); ok {
					result[k] = s
				} else if val != nil {
					result[k] = fmt.Sprintf("%v", val)
				}
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
					pm.ContainerPort = toInt(m["container"])
					pm.HostPort = toInt(m["host"])
					result = append(result, pm)
				}
			}
			return result
		}
	}
	return nil
}

// toInt converts various numeric types and string representations to int.
func toInt(v interface{}) int {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
	}
	return 0
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

func getHealthcheck(props map[string]interface{}, key string) *Healthcheck {
	v, ok := props[key]
	if !ok || v == nil {
		return nil
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}

	hc := &Healthcheck{
		Command:     getStringSlice(m, "command"),
		Interval:    getString(m, "interval"),
		Timeout:     getString(m, "timeout"),
		StartPeriod: getString(m, "start_period"),
	}

	if retries, ok := m["retries"]; ok {
		switch r := retries.(type) {
		case int:
			hc.Retries = r
		case float64:
			hc.Retries = int(r)
		}
	}

	if len(hc.Command) == 0 {
		return nil
	}
	return hc
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
