package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/architect-io/arcctl/pkg/schema/component"
	"github.com/architect-io/arcctl/pkg/state/types"
)

// DockerProvisioner handles local Docker-based resource provisioning.
type DockerProvisioner struct {
	envName       string
	networkName   string
	basePort      int
	nextPort      int
	containerIDs  []string
	resources     map[string]*types.ResourceState
}

// NewDockerProvisioner creates a new Docker provisioner for local development.
func NewDockerProvisioner(envName string, basePort int) *DockerProvisioner {
	return &DockerProvisioner{
		envName:     envName,
		networkName: fmt.Sprintf("arcctl-%s", envName),
		basePort:    basePort,
		nextPort:    basePort,
		resources:   make(map[string]*types.ResourceState),
	}
}

// ProvisionedResources returns the resource states after provisioning.
func (p *DockerProvisioner) ProvisionedResources() map[string]*types.ResourceState {
	return p.resources
}

// ContainerIDs returns all container IDs created during provisioning.
func (p *DockerProvisioner) ContainerIDs() []string {
	return p.containerIDs
}

// EnsureNetwork creates the Docker network if it doesn't exist.
func (p *DockerProvisioner) EnsureNetwork(ctx context.Context) error {
	// Check if network exists
	cmd := exec.CommandContext(ctx, "docker", "network", "inspect", p.networkName)
	if err := cmd.Run(); err == nil {
		return nil // Network already exists
	}

	// Create network
	cmd = exec.CommandContext(ctx, "docker", "network", "create", p.networkName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create network: %s: %w", string(output), err)
	}

	return nil
}

// ProvisionDatabase creates a PostgreSQL container for the given database.
func (p *DockerProvisioner) ProvisionDatabase(ctx context.Context, db component.Database, componentName string) (*DatabaseConnection, error) {
	dbType := strings.Split(db.Type(), ":")[0] // Extract type without version

	switch dbType {
	case "postgres":
		return p.provisionPostgres(ctx, db, componentName)
	case "redis":
		return p.provisionRedis(ctx, db, componentName)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}

// DatabaseConnection holds connection details for a provisioned database.
type DatabaseConnection struct {
	Host     string
	Port     int
	Username string
	Password string
	Database string
	URL      string
}

func (p *DockerProvisioner) provisionPostgres(ctx context.Context, db component.Database, componentName string) (*DatabaseConnection, error) {
	containerName := fmt.Sprintf("%s-%s-%s", p.envName, componentName, db.Name())
	password := generatePassword(16)
	dbName := strings.ReplaceAll(db.Name(), "-", "_")
	hostPort := p.nextPort
	p.nextPort++

	// Check if container already exists and is running
	cmd := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", containerName)
	output, err := cmd.Output()
	if err == nil && strings.TrimSpace(string(output)) == "true" {
		// Container is running, get its port
		cmd = exec.CommandContext(ctx, "docker", "port", containerName, "5432")
		output, err = cmd.Output()
		if err == nil {
			parts := strings.Split(strings.TrimSpace(string(output)), ":")
			if len(parts) == 2 {
				hostPort, _ = strconv.Atoi(parts[1])
			}
		}
		// Get the password from environment
		cmd = exec.CommandContext(ctx, "docker", "inspect", "-f", "{{range .Config.Env}}{{println .}}{{end}}", containerName)
		output, err = cmd.Output()
		if err == nil {
			for _, line := range strings.Split(string(output), "\n") {
				if strings.HasPrefix(line, "POSTGRES_PASSWORD=") {
					password = strings.TrimPrefix(line, "POSTGRES_PASSWORD=")
					break
				}
			}
		}

		conn := &DatabaseConnection{
			Host:     "localhost",
			Port:     hostPort,
			Username: "app",
			Password: password,
			Database: dbName,
			URL:      fmt.Sprintf("postgres://app:%s@localhost:%d/%s?sslmode=disable", password, hostPort, dbName),
		}

		p.resources[fmt.Sprintf("database/%s", db.Name())] = &types.ResourceState{
			Name:      db.Name(),
			Type:      "database",
			Component: componentName,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Status:    types.ResourceStatusReady,
			Outputs: map[string]interface{}{
				"host":         conn.Host,
				"port":         conn.Port,
				"username":     conn.Username,
				"password":     conn.Password,
				"database":     conn.Database,
				"url":          conn.URL,
				"container_id": containerName,
			},
		}

		return conn, nil
	}

	// Remove existing container if it exists but isn't running
	_ = exec.CommandContext(ctx, "docker", "rm", "-f", containerName).Run()

	// Run PostgreSQL container
	args := []string{
		"run", "-d",
		"--name", containerName,
		"--network", p.networkName,
		"-e", "POSTGRES_USER=app",
		"-e", fmt.Sprintf("POSTGRES_PASSWORD=%s", password),
		"-e", fmt.Sprintf("POSTGRES_DB=%s", dbName),
		"-p", fmt.Sprintf("%d:5432", hostPort),
		"--health-cmd", "pg_isready -U app",
		"--health-interval", "5s",
		"--health-timeout", "5s",
		"--health-retries", "5",
		"postgres:16-alpine",
	}

	cmd = exec.CommandContext(ctx, "docker", args...)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to start PostgreSQL: %s: %w", string(output), err)
	}

	containerID := strings.TrimSpace(string(output))
	p.containerIDs = append(p.containerIDs, containerID)

	// Wait for PostgreSQL to be healthy
	if err := p.waitForHealthy(ctx, containerName, 60*time.Second); err != nil {
		return nil, fmt.Errorf("PostgreSQL failed to become healthy: %w", err)
	}

	conn := &DatabaseConnection{
		Host:     "localhost",
		Port:     hostPort,
		Username: "app",
		Password: password,
		Database: dbName,
		URL:      fmt.Sprintf("postgres://app:%s@localhost:%d/%s?sslmode=disable", password, hostPort, dbName),
	}

	p.resources[fmt.Sprintf("database/%s", db.Name())] = &types.ResourceState{
		Name:      db.Name(),
		Type:      "database",
		Component: componentName,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Status:    types.ResourceStatusReady,
		Outputs: map[string]interface{}{
			"host":         conn.Host,
			"port":         conn.Port,
			"username":     conn.Username,
			"password":     conn.Password,
			"database":     conn.Database,
			"url":          conn.URL,
			"container_id": containerID,
		},
	}

	return conn, nil
}

func (p *DockerProvisioner) provisionRedis(ctx context.Context, db component.Database, componentName string) (*DatabaseConnection, error) {
	containerName := fmt.Sprintf("%s-%s-%s", p.envName, componentName, db.Name())
	hostPort := p.nextPort
	p.nextPort++

	// Check if container already exists and is running
	cmd := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", containerName)
	output, err := cmd.Output()
	if err == nil && strings.TrimSpace(string(output)) == "true" {
		// Get its port
		cmd = exec.CommandContext(ctx, "docker", "port", containerName, "6379")
		output, err = cmd.Output()
		if err == nil {
			parts := strings.Split(strings.TrimSpace(string(output)), ":")
			if len(parts) == 2 {
				hostPort, _ = strconv.Atoi(parts[1])
			}
		}

		conn := &DatabaseConnection{
			Host: "localhost",
			Port: hostPort,
			URL:  fmt.Sprintf("redis://localhost:%d", hostPort),
		}

		p.resources[fmt.Sprintf("database/%s", db.Name())] = &types.ResourceState{
			Name:      db.Name(),
			Type:      "database",
			Component: componentName,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Status:    types.ResourceStatusReady,
			Outputs: map[string]interface{}{
				"host":         conn.Host,
				"port":         conn.Port,
				"url":          conn.URL,
				"container_id": containerName,
			},
		}

		return conn, nil
	}

	// Remove existing container
	_ = exec.CommandContext(ctx, "docker", "rm", "-f", containerName).Run()

	// Run Redis container
	args := []string{
		"run", "-d",
		"--name", containerName,
		"--network", p.networkName,
		"-p", fmt.Sprintf("%d:6379", hostPort),
		"--health-cmd", "redis-cli ping",
		"--health-interval", "5s",
		"--health-timeout", "5s",
		"--health-retries", "5",
		"redis:7-alpine",
	}

	cmd = exec.CommandContext(ctx, "docker", args...)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to start Redis: %s: %w", string(output), err)
	}

	containerID := strings.TrimSpace(string(output))
	p.containerIDs = append(p.containerIDs, containerID)

	// Wait for Redis to be healthy
	if err := p.waitForHealthy(ctx, containerName, 30*time.Second); err != nil {
		return nil, fmt.Errorf("Redis failed to become healthy: %w", err)
	}

	conn := &DatabaseConnection{
		Host: "localhost",
		Port: hostPort,
		URL:  fmt.Sprintf("redis://localhost:%d", hostPort),
	}

	p.resources[fmt.Sprintf("database/%s", db.Name())] = &types.ResourceState{
		Name:      db.Name(),
		Type:      "database",
		Component: componentName,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Status:    types.ResourceStatusReady,
		Outputs: map[string]interface{}{
			"host":         conn.Host,
			"port":         conn.Port,
			"url":          conn.URL,
			"container_id": containerID,
		},
	}

	return conn, nil
}

// BuildImage builds a Docker image from the given build context.
func (p *DockerProvisioner) BuildImage(ctx context.Context, name string, buildContext string, dockerfile string, buildArgs map[string]string) (string, error) {
	imageTag := fmt.Sprintf("arcctl-%s-%s:latest", p.envName, name)

	// Build the docker command with absolute path to context
	args := []string{"build", "-t", imageTag}
	if dockerfile != "" {
		args = append(args, "-f", dockerfile)
	}

	// Add build arguments
	for k, v := range buildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}

	// Use the absolute build context path
	args = append(args, buildContext)

	cmd := exec.CommandContext(ctx, "docker", args...)
	// Run from the parent directory of the build context for relative path resolution
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to build image: %s: %w", string(output), err)
	}

	return imageTag, nil
}

// RunContainer runs a container from the given image.
func (p *DockerProvisioner) RunContainer(ctx context.Context, name string, image string, componentName string, env map[string]string, ports map[int]int) (string, int, error) {
	containerName := fmt.Sprintf("%s-%s-%s", p.envName, componentName, name)

	// Check if container already exists
	cmd := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", containerName)
	output, err := cmd.Output()
	if err == nil {
		// Container exists
		if strings.TrimSpace(string(output)) == "true" {
			// Get the host port
			for containerPort := range ports {
				cmd = exec.CommandContext(ctx, "docker", "port", containerName, fmt.Sprintf("%d", containerPort))
				output, err = cmd.Output()
				if err == nil {
					parts := strings.Split(strings.TrimSpace(string(output)), ":")
					if len(parts) == 2 {
						hostPort, _ := strconv.Atoi(parts[1])
						return containerName, hostPort, nil
					}
				}
			}
		}
		// Container exists but not running, remove it
		_ = exec.CommandContext(ctx, "docker", "rm", "-f", containerName).Run()
	}

	args := []string{
		"run", "-d",
		"--name", containerName,
		"--network", p.networkName,
	}

	// Add environment variables
	for k, v := range env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Add port mappings
	var exposedPort int
	for containerPort, hostPort := range ports {
		if hostPort == 0 {
			hostPort = p.nextPort
			p.nextPort++
		}
		args = append(args, "-p", fmt.Sprintf("%d:%d", hostPort, containerPort))
		exposedPort = hostPort
	}

	args = append(args, image)

	cmd = exec.CommandContext(ctx, "docker", args...)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return "", 0, fmt.Errorf("failed to run container: %s: %w", string(output), err)
	}

	containerID := strings.TrimSpace(string(output))
	p.containerIDs = append(p.containerIDs, containerID)

	p.resources[fmt.Sprintf("deployment/%s", name)] = &types.ResourceState{
		Name:      name,
		Type:      "deployment",
		Component: componentName,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Status:    types.ResourceStatusReady,
		Outputs: map[string]interface{}{
			"container_id": containerID,
			"port":         exposedPort,
		},
	}

	return containerID, exposedPort, nil
}

// Cleanup removes all containers created by this provisioner.
func (p *DockerProvisioner) Cleanup(ctx context.Context) error {
	for _, containerID := range p.containerIDs {
		cmd := exec.CommandContext(ctx, "docker", "rm", "-f", containerID)
		_ = cmd.Run() // Ignore errors
	}
	return nil
}

// CleanupByEnvName removes all containers for a given environment.
func CleanupByEnvName(ctx context.Context, envName string) error {
	// List all containers with the environment prefix
	prefix := fmt.Sprintf("%s-", envName)
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--filter", fmt.Sprintf("name=%s", prefix), "-q")
	output, err := cmd.Output()
	if err != nil {
		return nil // No containers found
	}

	containerIDs := strings.Fields(string(output))
	for _, id := range containerIDs {
		_ = exec.CommandContext(ctx, "docker", "rm", "-f", id).Run()
	}

	// Remove network
	networkName := fmt.Sprintf("arcctl-%s", envName)
	_ = exec.CommandContext(ctx, "docker", "network", "rm", networkName).Run()

	return nil
}

func (p *DockerProvisioner) waitForHealthy(ctx context.Context, containerName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Health.Status}}", containerName)
		output, err := cmd.Output()
		if err == nil {
			status := strings.TrimSpace(string(output))
			if status == "healthy" {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting for container to become healthy")
}

func generatePassword(length int) string {
	bytes := make([]byte, length/2)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
