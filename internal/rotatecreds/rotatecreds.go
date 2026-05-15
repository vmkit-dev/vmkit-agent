package rotatecreds

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

// RotateCredentials rotates instance credentials and restarts affected containers
func RotateCredentials(config *types.RotateCredentialsConfig) types.RotateCredentialsResult {
	result := types.RotateCredentialsResult{
		InstanceID: config.InstanceID,
		Steps:      make([]types.StepResult, 0),
	}

	// Set default base directory
	baseDir := config.BaseDir
	if baseDir == "" {
		baseDir = "/opt/supabase"
	}

	instanceDir := filepath.Join(baseDir, config.InstanceID)
	envFilePath := filepath.Join(instanceDir, ".env")

	// Step 1: Validate instance directory exists
	step1 := startStep("validate_instance")
	if _, err := os.Stat(instanceDir); os.IsNotExist(err) {
		result.Steps = append(result.Steps, failStep(step1, fmt.Sprintf("Instance directory not found: %s", instanceDir)))
		result.ErrorMessage = fmt.Sprintf("Instance directory not found: %s", instanceDir)
		result.FailedStep = "validate_instance"
		return result
	}
	result.Steps = append(result.Steps, completeStep(step1, "Instance directory validated"))

	// Step 2: Validate .env file exists
	step2 := startStep("validate_env_file")
	if _, err := os.Stat(envFilePath); os.IsNotExist(err) {
		result.Steps = append(result.Steps, failStep(step2, fmt.Sprintf(".env file not found: %s", envFilePath)))
		result.ErrorMessage = fmt.Sprintf(".env file not found: %s", envFilePath)
		result.FailedStep = "validate_env_file"
		return result
	}
	result.Steps = append(result.Steps, completeStep(step2, ".env file validated"))

	// Step 3: Update credentials in .env file
	step3 := startStep("update_credentials")
	if err := updateEnvFile(envFilePath, config); err != nil {
		result.Steps = append(result.Steps, failStep(step3, fmt.Sprintf("Failed to update .env file: %v", err)))
		result.ErrorMessage = fmt.Sprintf("Failed to update .env file: %v", err)
		result.FailedStep = "update_credentials"
		return result
	}
	result.Steps = append(result.Steps, completeStep(step3, "Credentials updated in .env file"))

	// Step 4: Determine which containers need restart based on credential type
	step4 := startStep("restart_containers")
	containersToRestart := getContainersToRestart(config.CredentialType)
	if err := restartContainers(instanceDir, containersToRestart); err != nil {
		result.Steps = append(result.Steps, failStep(step4, fmt.Sprintf("Failed to restart containers: %v", err)))
		result.ErrorMessage = fmt.Sprintf("Failed to restart containers: %v", err)
		result.FailedStep = "restart_containers"
		return result
	}
	result.Steps = append(result.Steps, completeStep(step4, fmt.Sprintf("Restarted containers: %s", strings.Join(containersToRestart, ", "))))

	// Step 5: Verify containers are healthy
	step5 := startStep("verify_health")
	if err := verifyContainerHealth(instanceDir, containersToRestart); err != nil {
		result.Steps = append(result.Steps, failStep(step5, fmt.Sprintf("Health check failed: %v", err)))
		result.ErrorMessage = fmt.Sprintf("Health check failed: %v", err)
		result.FailedStep = "verify_health"
		return result
	}
	result.Steps = append(result.Steps, completeStep(step5, "All containers healthy"))

	result.Success = true
	return result
}

// updateEnvFile updates the .env file with new credentials based on credential type
func updateEnvFile(envFilePath string, config *types.RotateCredentialsConfig) error {
	// Read existing .env file
	file, err := os.Open(envFilePath)
	if err != nil {
		return fmt.Errorf("failed to open .env file: %w", err)
	}
	defer file.Close()

	// Parse existing environment variables
	envVars := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Parse KEY=VALUE
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			envVars[parts[0]] = parts[1]
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read .env file: %w", err)
	}

	// Update credentials based on credential type
	switch config.CredentialType {
	case "postgres":
		if config.PostgresPassword != "" {
			envVars["POSTGRES_PASSWORD"] = config.PostgresPassword
		}
	case "jwt":
		if config.JWTSecret != "" {
			envVars["JWT_SECRET"] = config.JWTSecret
		}
	case "api_keys":
		if config.PublishableKey != "" {
			envVars["PUBLISHABLE_KEY"] = config.PublishableKey
		}
		if config.SecretKey != "" {
			envVars["SECRET_KEY"] = config.SecretKey
		}
	case "all":
		if config.PostgresPassword != "" {
			envVars["POSTGRES_PASSWORD"] = config.PostgresPassword
		}
		if config.JWTSecret != "" {
			envVars["JWT_SECRET"] = config.JWTSecret
		}
		if config.PublishableKey != "" {
			envVars["PUBLISHABLE_KEY"] = config.PublishableKey
		}
		if config.SecretKey != "" {
			envVars["SECRET_KEY"] = config.SecretKey
		}
	default:
		return fmt.Errorf("unknown credential type: %s", config.CredentialType)
	}

	// Write updated .env file
	var content strings.Builder
	content.WriteString("# Supabase instance environment variables\n")

	// Write in a consistent order
	orderedKeys := []string{
		"INSTANCE_ID", "SUBDOMAIN", "DOMAIN",
		"POSTGRES_PASSWORD", "JWT_SECRET", "PUBLISHABLE_KEY", "SECRET_KEY", "PGSODIUM_KEY",
		"KONG_PORT", "POSTGRES_PORT", "STUDIO_PORT",
	}

	writtenKeys := make(map[string]bool)
	for _, key := range orderedKeys {
		if value, exists := envVars[key]; exists {
			content.WriteString(fmt.Sprintf("%s=%s\n", key, value))
			writtenKeys[key] = true
		}
	}

	// Write any remaining keys not in the ordered list
	for key, value := range envVars {
		if !writtenKeys[key] {
			content.WriteString(fmt.Sprintf("%s=%s\n", key, value))
		}
	}

	// Write to file
	if err := os.WriteFile(envFilePath, []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("failed to write .env file: %w", err)
	}

	return nil
}

// getContainersToRestart determines which containers need to be restarted based on credential type
func getContainersToRestart(credentialType string) []string {
	switch credentialType {
	case "postgres":
		return []string{"postgres"}
	case "jwt":
		// JWT secret affects auth services - restart Kong for now
		return []string{"kong"}
	case "api_keys":
		// API keys affect Kong gateway
		return []string{"kong"}
	case "all":
		// Restart all containers when rotating all credentials
		return []string{"postgres", "kong", "studio"}
	default:
		// Default to restarting all containers if unknown type
		return []string{"postgres", "kong", "studio"}
	}
}

// restartContainers restarts specified containers
func restartContainers(instanceDir string, containers []string) error {
	for _, container := range containers {
		cmd := exec.Command("sudo", "docker-compose", "restart", container)
		cmd.Dir = instanceDir
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to restart %s: %w (output: %s)", container, err, string(output))
		}
	}

	// Wait a moment for containers to stabilize
	time.Sleep(2 * time.Second)

	return nil
}

// verifyContainerHealth checks if containers are healthy after restart
func verifyContainerHealth(instanceDir string, containers []string) error {
	maxAttempts := 30
	checkInterval := 2 * time.Second

	for _, container := range containers {
		healthy := false
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			cmd := exec.Command("sudo", "docker-compose", "ps", container)
			cmd.Dir = instanceDir
			output, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("failed to check %s status: %w", container, err)
			}

			// Check if container is running and healthy
			outputStr := string(output)
			if strings.Contains(outputStr, "Up") || strings.Contains(outputStr, "healthy") {
				healthy = true
				break
			}

			if attempt < maxAttempts {
				time.Sleep(checkInterval)
			}
		}

		if !healthy {
			return fmt.Errorf("container %s did not become healthy after restart", container)
		}
	}

	return nil
}

// Helper functions for step tracking
func startStep(name string) types.StepResult {
	return types.StepResult{
		Name:      name,
		Status:    "in_progress",
		StartTime: time.Now().Format(time.RFC3339),
	}
}

func completeStep(step types.StepResult, message string) types.StepResult {
	endTime := time.Now().Format(time.RFC3339)
	step.Status = "completed"
	step.Message = message
	step.EndTime = endTime
	return step
}

func failStep(step types.StepResult, message string) types.StepResult {
	endTime := time.Now().Format(time.RFC3339)
	step.Status = "failed"
	step.Message = message
	step.EndTime = endTime
	return step
}
