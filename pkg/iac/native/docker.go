package native

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

// resolveLocalhostRe matches "localhost" only when used as a standalone hostname,
// not as part of a subdomain (e.g., "app.localhost" should NOT be replaced).
// It matches "localhost" preceded by "://", "@", or start-of-string — but not by
// a dot or alphanumeric character.
var resolveLocalhostRe = regexp.MustCompile(`(^|[/@])localhost\b`)

// replaceLocalhostWithDockerHost replaces standalone "localhost" references with
// "host.docker.internal" while preserving subdomain patterns like "app.localhost".
func replaceLocalhostWithDockerHost(s string) string {
	return resolveLocalhostRe.ReplaceAllStringFunc(s, func(match string) string {
		prefix := match[:len(match)-len("localhost")]
		return prefix + "host.docker.internal"
	})
}

// DockerClient wraps the Docker SDK client.
type DockerClient struct {
	client *client.Client
}

// ContainerOptions defines options for creating a container.
type ContainerOptions struct {
	Image            string
	Name             string
	Command          []string
	Entrypoint       []string
	Environment      map[string]string
	Ports            []PortMapping
	Volumes          []VolumeMount
	Network          string
	Restart          string
	Healthcheck      *Healthcheck
	LogDriver        string            // Docker logging driver (e.g., "fluentd", "json-file")
	LogOptions       map[string]string // Options for the logging driver
	ExtraHosts       []string          // Additional /etc/hosts entries (e.g., "host.docker.internal:host-gateway")
	ResolveLocalhost bool              // Replace "localhost" in env var values with "host.docker.internal"
	Wait             bool              // Wait for container to exit before returning (for one-shot tasks)
	OnProgress       func(string)      // Optional callback for sub-status updates (e.g., "pulling image...", "health check 5/30")
}

// PortMapping defines a port mapping.
type PortMapping struct {
	ContainerPort int
	HostPort      int
	Protocol      string
}

// VolumeMount defines a volume mount.
type VolumeMount struct {
	Name   string
	Source string
	Path   string
}

// Healthcheck defines a health check.
type Healthcheck struct {
	Command     []string
	Interval    string
	Timeout     string
	Retries     int
	StartPeriod string
}

// ContainerInfo contains container information.
type ContainerInfo struct {
	ID    string
	Name  string
	Ports map[string]int
}

// NewDockerClient creates a new Docker client.
func NewDockerClient() (*DockerClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &DockerClient{client: cli}, nil
}

// reportProgress calls the OnProgress callback if set.
func reportProgress(cb func(string), msg string) {
	if cb != nil {
		cb(msg)
	}
}

// dockerPullProgress represents a single line from the Docker image pull JSON stream.
type dockerPullProgress struct {
	Status         string `json:"status"`
	ID             string `json:"id"`
	ProgressDetail struct {
		Current int64 `json:"current"`
		Total   int64 `json:"total"`
	} `json:"progressDetail"`
}

// pullImageWithProgress pulls a Docker image and reports download progress via the callback.
func (d *DockerClient) pullImageWithProgress(ctx context.Context, imageName string, onProgress func(string)) error {
	reader, err := d.client.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image %s: %w", imageName, err)
	}
	defer reader.Close()

	if onProgress == nil {
		_, _ = io.Copy(io.Discard, reader)
		return nil
	}

	// Parse the Docker JSON progress stream to report layer-level progress.
	// Each line is a JSON object with status, id, and progressDetail.
	layerTotal := make(map[string]int64)   // layer ID -> total bytes
	layerDone := make(map[string]int64)    // layer ID -> downloaded bytes
	layerComplete := make(map[string]bool) // layer ID -> fully downloaded

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		var p dockerPullProgress
		if err := json.Unmarshal(scanner.Bytes(), &p); err != nil {
			continue
		}

		if p.ID == "" {
			continue
		}

		switch p.Status {
		case "Downloading":
			if p.ProgressDetail.Total > 0 {
				layerTotal[p.ID] = p.ProgressDetail.Total
				layerDone[p.ID] = p.ProgressDetail.Current
			}
		case "Download complete", "Pull complete", "Already exists":
			layerComplete[p.ID] = true
			if t, ok := layerTotal[p.ID]; ok {
				layerDone[p.ID] = t
			}
		case "Pulling fs layer", "Waiting":
			if _, exists := layerTotal[p.ID]; !exists {
				layerTotal[p.ID] = 0
			}
		}

		// Compute aggregate progress
		totalLayers := len(layerTotal)
		if totalLayers == 0 {
			continue
		}

		completedLayers := 0
		for id := range layerTotal {
			if layerComplete[id] {
				completedLayers++
			}
		}

		var totalBytes, doneBytes int64
		for id, t := range layerTotal {
			totalBytes += t
			doneBytes += layerDone[id]
		}

		if totalBytes > 0 {
			pct := int(float64(doneBytes) / float64(totalBytes) * 100)
			onProgress(fmt.Sprintf("pulling image… %d%% (%d/%d layers)", pct, completedLayers, totalLayers))
		} else {
			onProgress(fmt.Sprintf("pulling image… (%d/%d layers)", completedLayers, totalLayers))
		}
	}

	return nil
}

// RunContainer creates and starts a container.
func (d *DockerClient) RunContainer(ctx context.Context, opts ContainerOptions) (string, error) {
	// Check if image exists locally first
	imageExists := false
	_, err := d.client.ImageInspect(ctx, opts.Image)
	if err == nil {
		imageExists = true
	}

	// Only pull if image doesn't exist locally
	if !imageExists {
		reportProgress(opts.OnProgress, "pulling image…")
		if err := d.pullImageWithProgress(ctx, opts.Image, opts.OnProgress); err != nil {
			return "", err
		}
	}

	// Build environment slice, optionally replacing standalone localhost with host.docker.internal
	env := make([]string, 0, len(opts.Environment))
	for k, v := range opts.Environment {
		if opts.ResolveLocalhost {
			v = replaceLocalhostWithDockerHost(v)
		}
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Build port bindings
	exposedPorts := nat.PortSet{}
	portBindings := nat.PortMap{}
	for _, pm := range opts.Ports {
		protocol := pm.Protocol
		if protocol == "" {
			protocol = "tcp"
		}
		port := nat.Port(fmt.Sprintf("%d/%s", pm.ContainerPort, protocol))
		exposedPorts[port] = struct{}{}

		hostPort := ""
		if pm.HostPort > 0 {
			hostPort = fmt.Sprintf("%d", pm.HostPort)
		}
		portBindings[port] = []nat.PortBinding{{HostPort: hostPort}}
	}

	// Build volume binds
	var binds []string
	for _, vm := range opts.Volumes {
		source := vm.Source
		if source == "" {
			source = vm.Name
		}
		binds = append(binds, fmt.Sprintf("%s:%s", source, vm.Path))
	}

	// Create container config
	config := &container.Config{
		Image:        opts.Image,
		Env:          env,
		Cmd:          opts.Command,
		Entrypoint:   opts.Entrypoint,
		ExposedPorts: exposedPorts,
	}

	// For wait-mode containers (tasks), enable attach so we can stream
	// stdout/stderr lines as progress regardless of the log driver.
	if opts.Wait {
		config.AttachStdout = true
		config.AttachStderr = true
	}

	// Apply healthcheck to container config if provided
	if opts.Healthcheck != nil && len(opts.Healthcheck.Command) > 0 {
		hc := &container.HealthConfig{
			Test: append([]string{"CMD-SHELL"}, strings.Join(opts.Healthcheck.Command, " ")),
		}
		if opts.Healthcheck.Interval != "" {
			if d, err := time.ParseDuration(opts.Healthcheck.Interval); err == nil {
				hc.Interval = d
			}
		}
		if opts.Healthcheck.Timeout != "" {
			if d, err := time.ParseDuration(opts.Healthcheck.Timeout); err == nil {
				hc.Timeout = d
			}
		}
		if opts.Healthcheck.Retries > 0 {
			hc.Retries = opts.Healthcheck.Retries
		}
		if opts.Healthcheck.StartPeriod != "" {
			if d, err := time.ParseDuration(opts.Healthcheck.StartPeriod); err == nil {
				hc.StartPeriod = d
			}
		}
		config.Healthcheck = hc
	}

	hostConfig := &container.HostConfig{
		PortBindings: portBindings,
		Binds:        binds,
		ExtraHosts:   opts.ExtraHosts,
	}

	if opts.ResolveLocalhost {
		// Ensure host.docker.internal is resolvable inside the container
		hasHostGateway := false
		for _, h := range hostConfig.ExtraHosts {
			if strings.HasPrefix(h, "host.docker.internal:") {
				hasHostGateway = true
				break
			}
		}
		if !hasHostGateway {
			hostConfig.ExtraHosts = append(hostConfig.ExtraHosts, "host.docker.internal:host-gateway")
		}
	}

	if opts.Restart != "" {
		hostConfig.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyMode(opts.Restart)}
	}

	if opts.LogDriver != "" {
		hostConfig.LogConfig = container.LogConfig{
			Type:   opts.LogDriver,
			Config: opts.LogOptions,
		}
	}

	networkConfig := &network.NetworkingConfig{}
	if opts.Network != "" {
		networkConfig.EndpointsConfig = map[string]*network.EndpointSettings{
			opts.Network: {},
		}
	}

	// Create container
	reportProgress(opts.OnProgress, "starting container…")
	resp, err := d.client.ContainerCreate(ctx, config, hostConfig, networkConfig, nil, opts.Name)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	// For wait-mode containers (tasks), attach to stdout/stderr BEFORE starting
	// so we capture all output from the very beginning. The attach stream provides
	// direct access to the container's stdio regardless of the log driver.
	var attachResp dockertypes.HijackedResponse
	var attachOk bool
	if opts.Wait {
		ar, err := d.client.ContainerAttach(ctx, resp.ID, container.AttachOptions{
			Stream: true,
			Stdout: true,
			Stderr: true,
		})
		if err == nil {
			attachResp = ar
			attachOk = true
			go streamAttachOutput(ar.Reader, opts.OnProgress)
		}
	}

	// Start container
	if err := d.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		if attachOk {
			attachResp.Close()
		}
		_ = d.client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	// If a healthcheck is configured, wait for the container to become healthy
	// before returning. This ensures downstream resources (e.g. databaseUser)
	// can safely connect to the service inside the container.
	if opts.Healthcheck != nil && len(opts.Healthcheck.Command) > 0 {
		if err := d.waitForHealthy(ctx, resp.ID, opts.Healthcheck, opts.OnProgress); err != nil {
			if attachOk {
				attachResp.Close()
			}
			_ = d.client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
			return "", fmt.Errorf("container failed health check: %w", err)
		}
	}

	// If Wait is true, block until the container exits (for one-shot tasks like migrations)
	if opts.Wait {
		statusCh, errCh := d.client.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
		var waitErr error
		select {
		case err := <-errCh:
			if err != nil {
				waitErr = fmt.Errorf("error waiting for container: %w", err)
			}
		case status := <-statusCh:
			if status.StatusCode != 0 {
				waitErr = fmt.Errorf("container exited with code %d", status.StatusCode)
			}
		case <-ctx.Done():
			waitErr = ctx.Err()
		}

		if attachOk {
			attachResp.Close()
		}
		if waitErr != nil {
			return resp.ID, waitErr
		}
	}

	return resp.ID, nil
}

// waitForHealthy polls the container's health status until it reports "healthy",
// using the healthcheck's interval and retries to determine the polling cadence
// and timeout. If the container exits or the context is cancelled, it returns
// an error immediately. The onProgress callback (may be nil) receives periodic
// sub-status messages such as "health check 5/30".
func (d *DockerClient) waitForHealthy(ctx context.Context, containerID string, hc *Healthcheck, onProgress func(string)) error {
	interval := 2 * time.Second
	if hc.Interval != "" {
		if parsed, err := time.ParseDuration(hc.Interval); err == nil {
			interval = parsed
		}
	}

	retries := 30 // default max retries
	if hc.Retries > 0 {
		retries = hc.Retries
	}

	// Respect start_period: wait before counting health check failures
	startDelay := 500 * time.Millisecond
	if hc.StartPeriod != "" {
		if parsed, err := time.ParseDuration(hc.StartPeriod); err == nil && parsed > startDelay {
			startDelay = parsed
		}
	}

	reportProgress(onProgress, fmt.Sprintf("health check 0/%d", retries))

	select {
	case <-time.After(startDelay):
	case <-ctx.Done():
		return ctx.Err()
	}

	for i := 0; i < retries; i++ {
		reportProgress(onProgress, fmt.Sprintf("health check %d/%d", i+1, retries))

		info, err := d.client.ContainerInspect(ctx, containerID)
		if err != nil {
			return fmt.Errorf("failed to inspect container: %w", err)
		}

		// If the container has stopped, there's no point waiting
		if info.State != nil && !info.State.Running {
			if logs := d.tailContainerLogs(ctx, containerID, "50"); logs != "" {
				return fmt.Errorf("container exited while waiting for health check (exit code %d):\n%s", info.State.ExitCode, logs)
			}
			return fmt.Errorf("container exited while waiting for health check (exit code %d)", info.State.ExitCode)
		}

		// Check the health status reported by Docker
		if info.State != nil && info.State.Health != nil {
			switch info.State.Health.Status {
			case "healthy":
				return nil
			case "unhealthy":
				// Gather the last health check log entry
				lastLog := ""
				if len(info.State.Health.Log) > 0 {
					last := info.State.Health.Log[len(info.State.Health.Log)-1]
					lastLog = strings.TrimSpace(last.Output)
				}

				// Fetch container application logs for debugging context
				containerLogs := d.tailContainerLogs(ctx, containerID, "50")

				msg := "container reported unhealthy"
				if lastLog != "" {
					msg += ": " + lastLog
				}
				if containerLogs != "" {
					msg += "\n\nContainer logs:\n" + containerLogs
				}
				return fmt.Errorf("%s", msg)
			}
			// "starting" — keep waiting
		}

		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Build an informative error that includes both health-check output
	// (the command Docker ran) and the container's application logs.
	msg := fmt.Sprintf("timed out after %d health check attempts", retries)

	// Try to surface the last health-check command output (e.g. "curl: not found")
	info, inspectErr := d.client.ContainerInspect(ctx, containerID)
	if inspectErr == nil && info.State != nil && info.State.Health != nil && len(info.State.Health.Log) > 0 {
		last := info.State.Health.Log[len(info.State.Health.Log)-1]
		if hcOutput := strings.TrimSpace(last.Output); hcOutput != "" {
			msg += fmt.Sprintf("\n\nLast health check output (exit code %d):\n%s", last.ExitCode, hcOutput)
		} else if last.ExitCode != 0 {
			msg += fmt.Sprintf("\n\nLast health check exited with code %d (no output)", last.ExitCode)
		}
	}

	if logs := d.tailContainerLogs(ctx, containerID, "50"); logs != "" {
		msg += "\n\nContainer logs:\n" + logs
	}

	return fmt.Errorf("%s", msg)
}

// tailContainerLogs fetches the last N lines of a container's stdout/stderr.
// It returns an empty string (and no error) if logs cannot be retrieved.
func (d *DockerClient) tailContainerLogs(ctx context.Context, containerID string, lines string) string {
	logs, err := d.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       lines,
	})
	if err != nil || logs == nil {
		return ""
	}
	defer logs.Close()

	output, err := io.ReadAll(logs)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// streamAttachOutput reads lines from a Docker container attach stream and
// reports the latest non-empty line via onProgress. The attach reader provides
// direct access to the container's stdout/stderr regardless of the log driver,
// so this works even when fluentd or other non-local log drivers are configured.
//
// The Docker multiplexed stream uses an 8-byte header per frame:
//
//	[stream_type(1)][0][0][0][payload_size(4 big-endian)]
//
// This function uses stdcopy-compatible header parsing to extract clean text.
func streamAttachOutput(reader io.Reader, onProgress func(string)) {
	if onProgress == nil || reader == nil {
		return
	}

	header := make([]byte, 8)
	for {
		// Read the 8-byte Docker multiplex header
		_, err := io.ReadFull(reader, header)
		if err != nil {
			return // stream closed (container exited) or error
		}

		// Payload size is big-endian uint32 in bytes 4-7
		size := uint32(header[4])<<24 | uint32(header[5])<<16 | uint32(header[6])<<8 | uint32(header[7])
		if size == 0 || size > 1<<20 {
			continue // skip empty or suspiciously large frames
		}

		payload := make([]byte, size)
		_, err = io.ReadFull(reader, payload)
		if err != nil {
			return
		}

		// Extract the last non-empty line from this frame
		line := lastNonEmptyLine(string(payload))
		if line != "" {
			reportProgress(onProgress, line)
		}
	}
}

// lastNonEmptyLine returns the last non-empty, printable line from text.
func lastNonEmptyLine(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	// Take the last line
	if idx := strings.LastIndex(text, "\n"); idx >= 0 {
		text = strings.TrimSpace(text[idx+1:])
	}

	// Strip non-printable characters
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if r >= 32 && r != 127 {
			b.WriteRune(r)
		}
	}

	return b.String()
}

// InspectContainer returns information about a container.
func (d *DockerClient) InspectContainer(ctx context.Context, containerID string) (*ContainerInfo, error) {
	info, err := d.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, err
	}

	ports := make(map[string]int)
	for port, bindings := range info.NetworkSettings.Ports {
		if len(bindings) > 0 {
			var hostPort int
			_, _ = fmt.Sscanf(bindings[0].HostPort, "%d", &hostPort)
			ports[string(port)] = hostPort
		}
	}

	return &ContainerInfo{
		ID:    info.ID,
		Name:  info.Name,
		Ports: ports,
	}, nil
}

// IsContainerRunning checks if a container is running.
func (d *DockerClient) IsContainerRunning(ctx context.Context, containerID string) (bool, error) {
	info, err := d.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return false, err
	}
	return info.State.Running, nil
}

// GetContainerByName finds a container by name and returns its ID.
// Returns empty string if not found.
func (d *DockerClient) GetContainerByName(ctx context.Context, name string) (string, error) {
	containers, err := d.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", "^/"+name+"$")),
	})
	if err != nil {
		return "", err
	}
	for _, c := range containers {
		for _, n := range c.Names {
			if n == "/"+name || n == name {
				return c.ID, nil
			}
		}
	}
	return "", nil
}

// ContainerMatchesConfig checks if a running container matches the desired configuration.
// Returns true if the container can be reused (image matches, etc.).
func (d *DockerClient) ContainerMatchesConfig(ctx context.Context, containerID string, opts ContainerOptions) bool {
	info, err := d.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return false
	}

	// Check if image matches (compare by image ID for accuracy)
	// The container stores the full image ID, we need to resolve the desired image
	desiredImageInfo, err := d.client.ImageInspect(ctx, opts.Image)
	if err != nil {
		// Can't inspect desired image, assume mismatch
		return false
	}

	if info.Image != desiredImageInfo.ID {
		// Image has changed
		return false
	}

	// Check environment variables
	currentEnv := make(map[string]string)
	for _, e := range info.Config.Env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			currentEnv[parts[0]] = parts[1]
		}
	}
	for k, v := range opts.Environment {
		if currentEnv[k] != v {
			return false
		}
	}

	// Check network
	if opts.Network != "" {
		found := false
		for netName := range info.NetworkSettings.Networks {
			if netName == opts.Network {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Note: We don't check ports here because dynamically-assigned host ports would always differ.
	// The image and env check is usually sufficient for local development.

	return true
}

// RemoveContainer stops and removes a container.
func (d *DockerClient) RemoveContainer(ctx context.Context, containerID string) error {
	return d.client.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force:         true,
		RemoveVolumes: false,
	})
}

// CreateNetwork creates a Docker network.
func (d *DockerClient) CreateNetwork(ctx context.Context, name string) (string, error) {
	// Check if network already exists
	networks, err := d.client.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return "", err
	}

	for _, n := range networks {
		if n.Name == name {
			return n.ID, nil
		}
	}

	resp, err := d.client.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: "bridge",
	})
	if err != nil {
		return "", fmt.Errorf("failed to create network: %w", err)
	}

	return resp.ID, nil
}

// NetworkExists checks if a network exists.
func (d *DockerClient) NetworkExists(ctx context.Context, networkID string) (bool, error) {
	_, err := d.client.NetworkInspect(ctx, networkID, network.InspectOptions{})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// RemoveNetwork removes a Docker network.
func (d *DockerClient) RemoveNetwork(ctx context.Context, networkID string) error {
	return d.client.NetworkRemove(ctx, networkID)
}

// CreateVolume creates a Docker volume.
func (d *DockerClient) CreateVolume(ctx context.Context, name string) (string, error) {
	// Check if volume already exists
	volumes, err := d.client.VolumeList(ctx, volume.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return "", err
	}

	for _, v := range volumes.Volumes {
		if v.Name == name {
			return v.Name, nil
		}
	}

	vol, err := d.client.VolumeCreate(ctx, volume.CreateOptions{
		Name: name,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create volume: %w", err)
	}

	return vol.Name, nil
}

// VolumeExists checks if a volume exists.
func (d *DockerClient) VolumeExists(ctx context.Context, volumeName string) (bool, error) {
	_, err := d.client.VolumeInspect(ctx, volumeName)
	if err != nil {
		if strings.Contains(err.Error(), "no such volume") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// RemoveVolume removes a Docker volume.
func (d *DockerClient) RemoveVolume(ctx context.Context, volumeName string) error {
	return d.client.VolumeRemove(ctx, volumeName, false)
}

// Exec executes a command on the host.
func (d *DockerClient) Exec(ctx context.Context, command []string, workDir string, env map[string]string) (string, error) {
	if len(command) == 0 {
		return "", fmt.Errorf("command is required")
	}

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	if workDir != "" {
		cmd.Dir = workDir
	}

	// Inherit parent process environment, then overlay task-specific vars
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(output))
		if outStr != "" {
			return outStr, fmt.Errorf("command failed: %w\n%s", err, outStr)
		}
		return "", fmt.Errorf("command failed: %w", err)
	}

	return string(output), nil
}

// RunOneShotOptions defines options for a one-shot container.
type RunOneShotOptions struct {
	Image            string
	Command          []string
	Environment      map[string]string
	Network          string
	WorkDir          string
	ResolveLocalhost bool // Replace "localhost" in env var values with "host.docker.internal"
}

// RunOneShot runs a command in a temporary Docker container and returns the output.
func (d *DockerClient) RunOneShot(ctx context.Context, opts RunOneShotOptions) (string, error) {
	if opts.Image == "" {
		return "", fmt.Errorf("image is required")
	}

	// Pull image first
	reader, err := d.client.ImagePull(ctx, opts.Image, image.PullOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to pull image: %w", err)
	}
	defer reader.Close()
	_, _ = io.Copy(io.Discard, reader)

	// Build environment variables
	var envList []string
	for k, v := range opts.Environment {
		if opts.ResolveLocalhost {
			v = replaceLocalhostWithDockerHost(v)
		}
		envList = append(envList, fmt.Sprintf("%s=%s", k, v))
	}

	// Create container config
	config := &container.Config{
		Image: opts.Image,
		Cmd:   opts.Command,
		Env:   envList,
	}
	if opts.WorkDir != "" {
		config.WorkingDir = opts.WorkDir
	}

	// Create host config
	hostConfig := &container.HostConfig{}
	if opts.ResolveLocalhost {
		hostConfig.ExtraHosts = []string{"host.docker.internal:host-gateway"}
	}

	// Create network config
	var networkConfig *network.NetworkingConfig
	if opts.Network != "" {
		networkConfig = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				opts.Network: {},
			},
		}
	}

	// Create container
	resp, err := d.client.ContainerCreate(ctx, config, hostConfig, networkConfig, nil, "")
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}
	containerID := resp.ID

	// Ensure cleanup
	defer func() {
		_ = d.client.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
	}()

	// Start container
	if err := d.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("failed to start container: %w", err)
	}

	// Wait for container to finish
	statusCh, errCh := d.client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return "", fmt.Errorf("error waiting for container: %w", err)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			// Get logs for error message
			logs, _ := d.client.ContainerLogs(ctx, containerID, container.LogsOptions{
				ShowStdout: true,
				ShowStderr: true,
			})
			if logs != nil {
				output, _ := io.ReadAll(logs)
				logs.Close()
				return string(output), fmt.Errorf("container exited with code %d: %s", status.StatusCode, string(output))
			}
			return "", fmt.Errorf("container exited with code %d", status.StatusCode)
		}
	}

	// Get logs
	logs, err := d.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}
	defer logs.Close()

	output, err := io.ReadAll(logs)
	if err != nil {
		return "", fmt.Errorf("failed to read logs: %w", err)
	}

	return string(output), nil
}
