package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

// Compose handles Docker Compose operations for a Supabase instance
type Compose struct {
	instanceDir string
	projectName string
}

// New creates a new Compose instance for managing a specific instance
func New(instanceID string, baseDir string) *Compose {
	instanceDir := filepath.Join(baseDir, instanceID)
	return &Compose{
		instanceDir: instanceDir,
		projectName: instanceID,
	}
}

// Pull pulls all images defined in docker-compose.yml
// This is idempotent - safe to call multiple times
func (c *Compose) Pull(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "sudo", "docker", "compose", "pull")
	cmd.Dir = c.instanceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull images: %w\nStdout: %s\nStderr: %s",
			err, stdout.String(), stderr.String())
	}

	return nil
}

// Up starts all containers defined in docker-compose.yml
// Uses -d flag to run in detached mode
// This is idempotent - safe to call on already-running containers
func (c *Compose) Up(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "sudo", "docker", "compose", "up", "-d", "--remove-orphans")
	cmd.Dir = c.instanceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start containers: %w\nStdout: %s\nStderr: %s",
			err, stdout.String(), stderr.String())
	}

	return nil
}

// UpService starts a single named service (and anything it implicitly depends
// on that is already running) without touching the rest of the stack. Used on
// first boot to bring postgres up alone so we can wait on its healthcheck
// with our own deadline, dodging compose v2's short internal dep-wait.
//
// See supabyoi-a49s: `docker compose up -d` for the full stack races when
// dependents declare `depends_on.condition: service_healthy` on a postgres
// that is still running upstream init scripts. Compose's exit-1 on that race
// kills the deploy even though postgres becomes healthy moments later.
func (c *Compose) UpService(ctx context.Context, service string) error {
	cmd := exec.CommandContext(ctx, "sudo", "docker", "compose", "up", "-d", "--no-deps", service)
	cmd.Dir = c.instanceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start service %s: %w\nStdout: %s\nStderr: %s",
			service, err, stdout.String(), stderr.String())
	}
	return nil
}

// Down stops and removes all containers, networks
// This is idempotent - safe to call on already-stopped containers
func (c *Compose) Down(ctx context.Context) error {
	return c.down(ctx, false)
}

// DownWithVolumes stops containers and removes volumes
// This ensures init scripts re-run on next startup
func (c *Compose) DownWithVolumes(ctx context.Context) error {
	return c.down(ctx, true)
}

func (c *Compose) down(ctx context.Context, removeVolumes bool) error {
	args := []string{"docker", "compose", "down"}
	if removeVolumes {
		args = append(args, "-v")
	}
	cmd := exec.CommandContext(ctx, "sudo", args...)
	cmd.Dir = c.instanceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop containers: %w\nStdout: %s\nStderr: %s",
			err, stdout.String(), stderr.String())
	}

	return nil
}

// Stop stops all containers without removing them
// This is idempotent - safe to call on already-stopped containers
func (c *Compose) Stop(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "sudo", "docker", "compose", "stop")
	cmd.Dir = c.instanceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop containers: %w\nStdout: %s\nStderr: %s",
			err, stdout.String(), stderr.String())
	}

	return nil
}

// Start starts all stopped containers
// This is idempotent - safe to call on already-running containers
func (c *Compose) Start(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "sudo", "docker", "compose", "start")
	cmd.Dir = c.instanceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start containers: %w\nStdout: %s\nStderr: %s",
			err, stdout.String(), stderr.String())
	}

	return nil
}

// Restart restarts all containers
// This is idempotent
func (c *Compose) Restart(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "sudo", "docker", "compose", "restart")
	cmd.Dir = c.instanceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restart containers: %w\nStdout: %s\nStderr: %s",
			err, stdout.String(), stderr.String())
	}

	return nil
}

// containerInfo represents the JSON output from docker compose ps
type containerInfo struct {
	Name    string `json:"Name"`
	State   string `json:"State"`
	Health  string `json:"Health"`
	Service string `json:"Service"`
}

// GetContainerStatus returns the status of all containers
func (c *Compose) GetContainerStatus(ctx context.Context) ([]types.ContainerStatus, error) {
	cmd := exec.CommandContext(ctx, "sudo", "docker", "compose", "ps", "--format", "json")
	cmd.Dir = c.instanceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// If compose file doesn't exist or no containers, return empty list
		if strings.Contains(stderr.String(), "no configuration file") ||
			strings.Contains(stderr.String(), "no such file") {
			return []types.ContainerStatus{}, nil
		}
		return nil, fmt.Errorf("failed to get container status: %w\nStderr: %s",
			err, stderr.String())
	}

	// Parse JSON output
	output := stdout.String()
	if output == "" {
		return []types.ContainerStatus{}, nil
	}

	// Docker compose ps --format json returns one JSON object per line
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var statuses []types.ContainerStatus

	for _, line := range lines {
		if line == "" {
			continue
		}

		var info containerInfo
		if err := json.Unmarshal([]byte(line), &info); err != nil {
			// Skip malformed lines
			continue
		}

		status := types.ContainerStatus{
			Name:   info.Service,
			State:  strings.ToLower(info.State),
			Health: strings.ToLower(info.Health),
		}

		statuses = append(statuses, status)
	}

	return statuses, nil
}

// IsRunning checks if all containers are running
func (c *Compose) IsRunning(ctx context.Context) (bool, error) {
	statuses, err := c.GetContainerStatus(ctx)
	if err != nil {
		return false, err
	}

	if len(statuses) == 0 {
		return false, nil
	}

	// Check if all containers are running
	for _, status := range statuses {
		if status.State != "running" {
			return false, nil
		}
	}

	return true, nil
}

// WaitForContainers waits for all containers to be running
// Returns error if containers don't start within timeout
func (c *Compose) WaitForContainers(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		running, err := c.IsRunning(ctx)
		if err != nil {
			return err
		}

		if running {
			return nil
		}

		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			// Continue waiting
		}
	}

	return fmt.Errorf("containers did not start within %v", timeout)
}

// Logs returns the logs from all containers
func (c *Compose) Logs(ctx context.Context, tail int) (string, error) {
	args := []string{"docker", "compose", "logs"}
	if tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", tail))
	}

	cmd := exec.CommandContext(ctx, "sudo", args...)
	cmd.Dir = c.instanceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// If no containers exist, return empty logs instead of error
		if strings.Contains(stderr.String(), "no configuration file") {
			return "", nil
		}
		return "", fmt.Errorf("failed to get logs: %w\nStderr: %s",
			err, stderr.String())
	}

	return stdout.String(), nil
}

// GetServiceLogs returns logs for a specific service
func (c *Compose) GetServiceLogs(ctx context.Context, service string, tail int) (string, error) {
	args := []string{"docker", "compose", "logs", service}
	if tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", tail))
	}

	cmd := exec.CommandContext(ctx, "sudo", args...)
	cmd.Dir = c.instanceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to get logs for service %s: %w\nStderr: %s",
			service, err, stderr.String())
	}

	return stdout.String(), nil
}

// Exec executes a command in a running container
func (c *Compose) Exec(ctx context.Context, service string, command []string) (string, error) {
	args := append([]string{"docker", "compose", "exec", "-T", service}, command...)

	cmd := exec.CommandContext(ctx, "sudo", args...)
	cmd.Dir = c.instanceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to execute command in %s: %w\nStderr: %s",
			service, err, stderr.String())
	}

	return stdout.String(), nil
}

// ExecWithEnv runs a command in a service container with extra environment variables.
func (c *Compose) ExecWithEnv(ctx context.Context, service string, command []string, env map[string]string) (string, error) {
	args := []string{"docker", "compose", "exec", "-T"}
	for k, v := range env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, service)
	args = append(args, command...)

	cmd := exec.CommandContext(ctx, "sudo", args...)
	cmd.Dir = c.instanceDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to execute command in %s: %w\nStderr: %s",
			service, err, stderr.String())
	}

	return stdout.String(), nil
}

// GetProjectName returns the compose project name
func (c *Compose) GetProjectName() string {
	return c.projectName
}

// GetInstanceDir returns the instance directory path
func (c *Compose) GetInstanceDir() string {
	return c.instanceDir
}

// Validate checks if the docker-compose.yml file exists and is valid
func (c *Compose) Validate(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "sudo", "docker", "compose", "config", "--quiet")
	cmd.Dir = c.instanceDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker-compose.yml validation failed: %w\nStderr: %s",
			err, stderr.String())
	}

	return nil
}
