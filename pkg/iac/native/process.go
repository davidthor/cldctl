package native

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ProcessOptions defines options for running a process.
type ProcessOptions struct {
	Name        string
	WorkingDir  string
	Command     []string
	Environment map[string]string
	// Readiness check configuration
	Readiness *ReadinessCheck
	// Graceful stop configuration
	GracefulStop *GracefulStop
	// Stdout receives process stdout. If nil, output is discarded.
	Stdout io.Writer
	// Stderr receives process stderr. If nil, output is discarded.
	Stderr io.Writer
}

// ReadinessCheck defines a process readiness check.
type ReadinessCheck struct {
	Type     string        // "http" or "tcp"
	Endpoint string        // For HTTP: full URL, for TCP: host:port
	Interval time.Duration // How often to check
	Timeout  time.Duration // Total time to wait for ready
}

// GracefulStop defines graceful shutdown configuration.
type GracefulStop struct {
	Signal  string        // Signal name (e.g., "SIGTERM")
	Timeout time.Duration // Time to wait before SIGKILL
}

// ProcessInfo contains information about a running process.
type ProcessInfo struct {
	PID         int
	Name        string
	Command     []string
	Environment map[string]string
	WorkingDir  string
}

// ProcessManager manages local processes.
type ProcessManager struct {
	processes map[string]*managedProcess
	mu        sync.RWMutex
}

type managedProcess struct {
	cmd  *exec.Cmd
	info *ProcessInfo
	done chan error
}

// NewProcessManager creates a new process manager.
func NewProcessManager() *ProcessManager {
	return &ProcessManager{
		processes: make(map[string]*managedProcess),
	}
}

// StartProcess starts a new process.
func (pm *ProcessManager) StartProcess(ctx context.Context, opts ProcessOptions) (*ProcessInfo, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Check if process already running
	if mp, exists := pm.processes[opts.Name]; exists {
		if mp.cmd.Process != nil {
			// Check if still running
			if err := mp.cmd.Process.Signal(syscall.Signal(0)); err == nil {
				return mp.info, nil
			}
		}
		delete(pm.processes, opts.Name)
	}

	if len(opts.Command) == 0 {
		return nil, fmt.Errorf("command is required")
	}

	// Prepare environment
	env := os.Environ()
	for k, v := range opts.Environment {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Create command
	cmd := exec.CommandContext(ctx, opts.Command[0], opts.Command[1:]...)
	cmd.Dir = opts.WorkingDir
	cmd.Env = env

	// Set up output capture
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start process: %w", err)
	}

	// Stream output to configured writers (or discard if nil)
	stdoutWriter := opts.Stdout
	if stdoutWriter == nil {
		stdoutWriter = io.Discard
	}
	stderrWriter := opts.Stderr
	if stderrWriter == nil {
		stderrWriter = io.Discard
	}
	go streamOutput(stdoutPipe, fmt.Sprintf("[%s] ", opts.Name), stdoutWriter)
	go streamOutput(stderrPipe, fmt.Sprintf("[%s] [ERROR] ", opts.Name), stderrWriter)

	// Track completion
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	info := &ProcessInfo{
		PID:         cmd.Process.Pid,
		Name:        opts.Name,
		Command:     opts.Command,
		Environment: opts.Environment,
		WorkingDir:  opts.WorkingDir,
	}

	pm.processes[opts.Name] = &managedProcess{
		cmd:  cmd,
		info: info,
		done: done,
	}

	// Wait for readiness if configured
	if opts.Readiness != nil {
		if err := pm.waitForReady(ctx, opts.Readiness); err != nil {
			_ = pm.stopProcessLocked(opts.Name, 5*time.Second) // Best effort cleanup; ignore error
			return nil, fmt.Errorf("process failed readiness check: %w", err)
		}
	}

	return info, nil
}

// StopProcess stops a running process.
func (pm *ProcessManager) StopProcess(name string, timeout time.Duration) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	return pm.stopProcessLocked(name, timeout)
}

// stopProcessLocked stops a running process. Caller must hold pm.mu.
func (pm *ProcessManager) stopProcessLocked(name string, timeout time.Duration) error {
	mp, exists := pm.processes[name]
	if !exists {
		return nil // Already stopped
	}

	if mp.cmd.Process == nil {
		delete(pm.processes, name)
		return nil
	}

	// Try graceful shutdown
	if err := mp.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Process might already be dead
		delete(pm.processes, name)
		return nil
	}

	// Wait for process to exit
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-mp.done:
		delete(pm.processes, name)
		return nil
	case <-timer.C:
		// Force kill
		if err := mp.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill process: %w", err)
		}
		delete(pm.processes, name)
		return nil
	}
}

// GetProcessInfo returns information about a running process.
func (pm *ProcessManager) GetProcessInfo(name string) (*ProcessInfo, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	mp, exists := pm.processes[name]
	if !exists {
		return nil, fmt.Errorf("process not found: %s", name)
	}

	return mp.info, nil
}

// IsProcessRunning checks if a process is running.
func (pm *ProcessManager) IsProcessRunning(name string) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	mp, exists := pm.processes[name]
	if !exists {
		return false
	}

	if mp.cmd.Process == nil {
		return false
	}

	// Check if process is still alive
	err := mp.cmd.Process.Signal(syscall.Signal(0))
	return err == nil
}

// StopAllWithPrefix stops all processes whose names start with the given prefix.
// This is used for environment cleanup to stop all processes for an environment.
func (pm *ProcessManager) StopAllWithPrefix(prefix string, timeout time.Duration) {
	pm.mu.Lock()
	// Collect process names to stop (can't modify map while iterating)
	var toStop []string
	for name := range pm.processes {
		if strings.HasPrefix(name, prefix) {
			toStop = append(toStop, name)
		}
	}
	pm.mu.Unlock()

	// Stop each process
	for _, name := range toStop {
		_ = pm.StopProcess(name, timeout)
	}
}

// StopAll stops all managed processes.
func (pm *ProcessManager) StopAll(timeout time.Duration) {
	pm.mu.Lock()
	var toStop []string
	for name := range pm.processes {
		toStop = append(toStop, name)
	}
	pm.mu.Unlock()

	for _, name := range toStop {
		_ = pm.StopProcess(name, timeout)
	}
}

// waitForReady waits for the process to become ready.
func (pm *ProcessManager) waitForReady(ctx context.Context, readiness *ReadinessCheck) error {
	switch readiness.Type {
	case "http":
		return pm.waitForReadyHTTP(ctx, readiness)
	case "tcp":
		return pm.waitForReadyTCP(ctx, readiness)
	default:
		return fmt.Errorf("unsupported readiness check type: %q (supported: http, tcp)", readiness.Type)
	}
}

// waitForReadyHTTP waits for an HTTP endpoint to return a 2xx/3xx status.
func (pm *ProcessManager) waitForReadyHTTP(ctx context.Context, readiness *ReadinessCheck) error {
	deadline := time.Now().Add(readiness.Timeout)
	ticker := time.NewTicker(readiness.Interval)
	defer ticker.Stop()

	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			resp, err := client.Get(readiness.Endpoint)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 400 {
					return nil
				}
			}
		}
	}

	return fmt.Errorf("process did not become ready within %v", readiness.Timeout)
}

// waitForReadyTCP waits for a TCP endpoint to accept connections.
func (pm *ProcessManager) waitForReadyTCP(ctx context.Context, readiness *ReadinessCheck) error {
	deadline := time.Now().Add(readiness.Timeout)
	ticker := time.NewTicker(readiness.Interval)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", readiness.Endpoint, 2*time.Second)
			if err == nil {
				conn.Close()
				return nil
			}
		}
	}

	return fmt.Errorf("process did not become ready within %v", readiness.Timeout)
}

// streamOutput streams process output to the given writer with a prefix.
func streamOutput(r io.Reader, prefix string, w io.Writer) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fmt.Fprintf(w, "%s%s\n", prefix, scanner.Text())
	}
}

// findAvailablePort finds an available port to use.
func findAvailablePort() (int, error) {
	// Try to bind to port 0 to let the OS assign an available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("failed to find available port: %w", err)
	}
	defer listener.Close()

	// Get the assigned port
	addr := listener.Addr().(*net.TCPAddr)
	return addr.Port, nil
}

// ParseDockerfileCmd parses a Dockerfile and extracts the CMD instruction.
func ParseDockerfileCmd(dockerfilePath string) ([]string, error) {
	data, err := os.ReadFile(dockerfilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Dockerfile: %w", err)
	}

	// Parse Dockerfile line by line
	lines := strings.Split(string(data), "\n")
	var cmd []string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Look for CMD instruction (last one wins)
		if strings.HasPrefix(strings.ToUpper(line), "CMD") {
			cmdLine := strings.TrimSpace(line[3:])

			// Parse JSON array format: CMD ["npm", "start"]
			if strings.HasPrefix(cmdLine, "[") {
				cmdLine = strings.Trim(cmdLine, "[]")
				parts := strings.Split(cmdLine, ",")
				cmd = make([]string, 0, len(parts))
				for _, part := range parts {
					part = strings.TrimSpace(part)
					part = strings.Trim(part, `"'`)
					if part != "" {
						cmd = append(cmd, part)
					}
				}
			} else {
				// Shell form: CMD npm start
				// This needs to be wrapped in a shell
				cmd = []string{"/bin/sh", "-c", cmdLine}
			}
		}
	}

	if len(cmd) == 0 {
		return nil, fmt.Errorf("no CMD instruction found in Dockerfile")
	}

	return cmd, nil
}

// ExtractDockerfileCmdFromContext extracts CMD from a Dockerfile in the build context.
func ExtractDockerfileCmdFromContext(contextPath, dockerfilePath string) ([]string, error) {
	if dockerfilePath == "" {
		dockerfilePath = "Dockerfile"
	}

	fullPath := filepath.Join(contextPath, dockerfilePath)
	return ParseDockerfileCmd(fullPath)
}
