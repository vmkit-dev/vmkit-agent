package instance

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vmkit-dev/vmkit-agent/internal/compose"
	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

const (
	// DefaultBaseDir is the default directory for instances
	DefaultBaseDir = "/opt/supabase"
)

// Status retrieves the current status of an instance
func Status(instanceID, subdomain string) (*types.InstanceStatus, error) {
	if subdomain == "" {
		return nil, fmt.Errorf("subdomain is required")
	}

	status := &types.InstanceStatus{
		InstanceID:   instanceID,
		Containers:   make([]types.ContainerStatus, 0),
		HealthChecks: make(map[string]bool),
		LastChecked:  time.Now().Format(time.RFC3339),
	}

	// Create compose client
	c := compose.New(subdomain, DefaultBaseDir)
	ctx := context.Background()

	// Get container statuses using compose package
	containers, err := c.GetContainerStatus(ctx)
	if err != nil {
		status.State = "error"
		return status, fmt.Errorf("failed to get container statuses: %w", err)
	}

	status.Containers = containers

	// Determine overall state
	if len(containers) == 0 {
		status.State = "stopped"
	} else {
		allRunning := true
		anyRunning := false

		for _, container := range containers {
			if container.State == "running" {
				anyRunning = true
			} else {
				allRunning = false
			}

			// Set health check status
			status.HealthChecks[container.Name] = container.State == "running"
		}

		if allRunning {
			status.State = "running"
		} else if anyRunning {
			status.State = "degraded"
		} else {
			status.State = "stopped"
		}
	}

	return status, nil
}

// Start starts a stopped instance
func Start(subdomain string) error {
	if subdomain == "" {
		return fmt.Errorf("subdomain is required")
	}

	instanceDir := filepath.Join(DefaultBaseDir, subdomain)

	// Check if docker-compose.yml exists
	composeFile := filepath.Join(instanceDir, "docker-compose.yml")
	if !fileExists(composeFile) {
		return fmt.Errorf("instance not found: %s", subdomain)
	}

	// Create compose client and start containers
	c := compose.New(subdomain, DefaultBaseDir)
	ctx := context.Background()

	if err := c.Start(ctx); err != nil {
		return fmt.Errorf("failed to start instance: %w", err)
	}

	return nil
}

// Stop stops a running instance
func Stop(subdomain string) error {
	if subdomain == "" {
		return fmt.Errorf("subdomain is required")
	}

	instanceDir := filepath.Join(DefaultBaseDir, subdomain)

	// Check if docker-compose.yml exists
	composeFile := filepath.Join(instanceDir, "docker-compose.yml")
	if !fileExists(composeFile) {
		return fmt.Errorf("instance not found: %s", subdomain)
	}

	// Create compose client and stop containers
	c := compose.New(subdomain, DefaultBaseDir)
	ctx := context.Background()

	if err := c.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop instance: %w", err)
	}

	return nil
}

// Restart restarts a running instance
func Restart(subdomain string) error {
	if subdomain == "" {
		return fmt.Errorf("subdomain is required")
	}

	instanceDir := filepath.Join(DefaultBaseDir, subdomain)

	// Check if docker-compose.yml exists
	composeFile := filepath.Join(instanceDir, "docker-compose.yml")
	if !fileExists(composeFile) {
		return fmt.Errorf("instance not found: %s", subdomain)
	}

	// Create compose client and restart containers
	c := compose.New(subdomain, DefaultBaseDir)
	ctx := context.Background()

	if err := c.Restart(ctx); err != nil {
		return fmt.Errorf("failed to restart instance: %w", err)
	}

	return nil
}


// fileExists checks if a file exists
func fileExists(path string) bool {
	cmd := exec.Command("test", "-f", path)
	return cmd.Run() == nil
}

// Logs retrieves logs for an instance or specific container
func Logs(subdomain, containerName string, tail int, follow bool) error {
	if subdomain == "" {
		return fmt.Errorf("subdomain is required")
	}

	instanceDir := filepath.Join(DefaultBaseDir, subdomain)

	args := []string{"docker", "compose", "logs"}

	if tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", tail))
	}

	if follow {
		args = append(args, "-f")
	}

	if containerName != "" {
		// Map service names (postgres, kong, studio) to container names
		serviceName := strings.TrimPrefix(containerName, subdomain+"-")
		args = append(args, serviceName)
	}

	cmd := exec.Command("sudo", args...)
	cmd.Dir = instanceDir
	cmd.Stdout = nil // Will be handled by caller
	cmd.Stderr = nil // Will be handled by caller

	return cmd.Run()
}
