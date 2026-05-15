package docker

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// IsInstalled checks if Docker is installed and docker compose is available
func IsInstalled() bool {
	// Check if docker command exists
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}

	// Check if docker compose is available (either plugin or standalone)
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		return false
	}

	return true
}

// Install installs Docker on the system (Ubuntu/Debian)
// This function is idempotent - safe to call multiple times
func Install(username string) error {
	// Check if already installed (idempotent)
	if IsInstalled() {
		// Already installed, just ensure user is in docker group
		if username != "" && username != "root" {
			return ensureUserInDockerGroup(username)
		}
		return nil
	}

	// Install Docker using official installation script
	if err := installDocker(); err != nil {
		return fmt.Errorf("failed to install Docker: %w", err)
	}

	// Enable and start Docker service
	if err := enableDockerService(); err != nil {
		return fmt.Errorf("failed to enable Docker service: %w", err)
	}

	// Add user to docker group for non-root access
	if username != "" && username != "root" {
		if err := ensureUserInDockerGroup(username); err != nil {
			return fmt.Errorf("failed to add user to docker group: %w", err)
		}
	}

	// Verify installation
	if !IsInstalled() {
		return fmt.Errorf("Docker installation verification failed")
	}

	return nil
}

// installDocker installs Docker using the official get.docker.com script
func installDocker() error {
	// Download and execute Docker installation script
	// Using official Docker installation script is the recommended way
	cmd := exec.Command("sh", "-c", "curl -fsSL https://get.docker.com | sh")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Docker installation script failed: %w\nStdout: %s\nStderr: %s",
			err, stdout.String(), stderr.String())
	}

	return nil
}

// enableDockerService enables and starts the Docker systemd service
func enableDockerService() error {
	// Enable Docker service to start on boot
	enableCmd := exec.Command("systemctl", "enable", "docker")
	if err := enableCmd.Run(); err != nil {
		return fmt.Errorf("failed to enable Docker service: %w", err)
	}

	// Start Docker service
	startCmd := exec.Command("systemctl", "start", "docker")
	if err := startCmd.Run(); err != nil {
		return fmt.Errorf("failed to start Docker service: %w", err)
	}

	return nil
}

// ensureUserInDockerGroup adds the specified user to the docker group
// This is idempotent - safe to call even if user is already in the group
func ensureUserInDockerGroup(username string) error {
	// Check if user is already in docker group
	groupsCmd := exec.Command("groups", username)
	output, err := groupsCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check user groups: %w", err)
	}

	groups := string(output)
	if strings.Contains(groups, "docker") {
		// User already in docker group
		return nil
	}

	// Add user to docker group
	usermodCmd := exec.Command("usermod", "-aG", "docker", username)
	if err := usermodCmd.Run(); err != nil {
		return fmt.Errorf("failed to add user %s to docker group: %w", username, err)
	}

	return nil
}

// GetVersion returns the installed Docker version
func GetVersion() (string, error) {
	out, err := exec.Command("docker", "--version").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get Docker version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// GetComposeVersion returns the installed Docker Compose version
func GetComposeVersion() (string, error) {
	out, err := exec.Command("docker", "compose", "version").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get Docker Compose version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// IsServiceRunning checks if the Docker daemon is running
func IsServiceRunning() bool {
	// Try to connect to Docker daemon
	cmd := exec.Command("docker", "info")
	cmd.Stderr = nil // Suppress error output
	return cmd.Run() == nil
}

// WaitForDaemon waits for the Docker daemon to be ready
func WaitForDaemon() error {
	// Simple check - in production might want exponential backoff
	if !IsServiceRunning() {
		return fmt.Errorf("Docker daemon is not running")
	}
	return nil
}
