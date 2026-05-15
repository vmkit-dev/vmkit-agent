package upgrade

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vmkit-dev/vmkit-agent/internal/backup"
	"github.com/vmkit-dev/vmkit-agent/internal/compose"
	"github.com/vmkit-dev/vmkit-agent/internal/health"
	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

// Version registry maps version strings to their component versions
var versionRegistry = map[string]types.VersionInfo{
	"1.23.0": {
		Postgres: "supabase/postgres:15.1.0.117",
		Kong:     "kong:2.8.1",
		Studio:   "supabase/studio:20231123-64a766a",
	},
	"1.24.0": {
		Postgres: "supabase/postgres:15.1.1.118",
		Kong:     "kong:2.8.1",
		Studio:   "supabase/studio:20240101-a1b2c3d",
	},
	// Add more versions as needed
}

// Upgrade performs a production-grade upgrade of a Supabase instance
func Upgrade(config types.UpgradeConfig) types.UpgradeResult {
	result := types.UpgradeResult{
		Success:    false,
		InstanceID: config.InstanceID,
		Steps:      []types.StepResult{},
	}

	// Set default base directory if not specified
	if config.BaseDir == "" {
		config.BaseDir = "/opt/supabase"
	}

	instanceDir := filepath.Join(config.BaseDir, config.Subdomain)
	composeFile := filepath.Join(instanceDir, "docker-compose.yml")
	composeBackupFile := filepath.Join(instanceDir, "docker-compose.yml.backup")

	// Step 1: Pre-upgrade validation
	step := stepStart("pre_upgrade_validation", "Validating upgrade prerequisites")
	result.Steps = append(result.Steps, step)

	// Check if instance directory exists
	if _, err := os.Stat(instanceDir); os.IsNotExist(err) {
		result.ErrorMessage = fmt.Sprintf("Instance directory not found: %s", instanceDir)
		result.FailedStep = "pre_upgrade_validation"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	// Validate version upgrade path
	if err := validateVersionUpgrade(config.CurrentVersion, config.TargetVersion); err != nil {
		result.ErrorMessage = err.Error()
		result.FailedStep = "pre_upgrade_validation"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	// Check disk space (require at least 5GB for image pulls)
	freeSpace, err := getFreeDiskSpace("/var/lib/docker")
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to check disk space: %v", err)
		result.FailedStep = "pre_upgrade_validation"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}
	if freeSpace < 5*1024*1024*1024 { // 5GB in bytes
		result.ErrorMessage = fmt.Sprintf("Insufficient disk space: %d bytes available, need at least 5GB", freeSpace)
		result.FailedStep = "pre_upgrade_validation"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	// Check current instance health
	healthConfig := health.DefaultCheckConfig(config.Subdomain)
	ctx := context.Background()
	healthResult, err := health.CheckAll(healthConfig)
	if err != nil || !healthResult.AllHealthy {
		result.ErrorMessage = "Instance is not healthy before upgrade - fix issues before upgrading"
		result.FailedStep = "pre_upgrade_validation"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	result.Steps[len(result.Steps)-1] = stepSuccess(step, fmt.Sprintf("Validation passed: %s -> %s", config.CurrentVersion, config.TargetVersion))

	// Step 2: Create backup (if enabled)
	var backupID string
	if config.BackupBeforeUpgrade && config.BackupUploadURL != "" {
		step = stepStart("create_backup", "Creating pre-upgrade backup")
		result.Steps = append(result.Steps, step)

		backupConfig := types.BackupConfig{
			InstanceID:       config.InstanceID,
			Subdomain:        config.Subdomain,
			BaseDir:          config.BaseDir,
			UploadURL:        config.BackupUploadURL,
			StorageUploadURL: config.StorageUploadURL,
			IncludeStorage:   true,
		}

		backupResult := backup.Backup(backupConfig)
		if !backupResult.Success {
			result.ErrorMessage = fmt.Sprintf("Pre-upgrade backup failed: %s", backupResult.ErrorMessage)
			result.FailedStep = "create_backup"
			result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
			return result
		}

		backupID = backupResult.BackupID
		result.BackupID = backupID
		result.Steps[len(result.Steps)-1] = stepSuccess(step, fmt.Sprintf("Backup created: %s", backupID))
	}

	// Step 3: Backup current docker-compose.yml
	step = stepStart("backup_configuration", "Backing up current configuration")
	result.Steps = append(result.Steps, step)

	if err := copyFile(composeFile, composeBackupFile); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to backup docker-compose.yml: %v", err)
		result.FailedStep = "backup_configuration"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	result.Steps[len(result.Steps)-1] = stepSuccess(step, "Configuration backed up")

	// Step 4: Update docker-compose.yml
	step = stepStart("update_configuration", "Updating docker-compose.yml with new versions")
	result.Steps = append(result.Steps, step)

	if config.ComposeContent != "" {
		// Backend provided pre-rendered compose — write it directly
		if err := os.WriteFile(composeFile, []byte(config.ComposeContent), 0644); err != nil {
			result.ErrorMessage = fmt.Sprintf("Failed to write docker-compose.yml: %v", err)
			result.FailedStep = "update_configuration"
			result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
			if config.RollbackOnFailure {
				copyFile(composeBackupFile, composeFile)
			}
			return result
		}
		// Write .env if provided
		if config.EnvContent != "" {
			envFile := filepath.Join(instanceDir, ".env")
			if err := os.WriteFile(envFile, []byte(config.EnvContent), 0644); err != nil {
				result.ErrorMessage = fmt.Sprintf("Failed to write .env: %v", err)
				result.FailedStep = "update_configuration"
				result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
				if config.RollbackOnFailure {
					copyFile(composeBackupFile, composeFile)
				}
				return result
			}
		}
		result.Steps[len(result.Steps)-1] = stepSuccess(step, "Configuration updated from backend-rendered templates")
	} else {
		// Legacy path: use hardcoded version registry
		targetVersionInfo, ok := versionRegistry[config.TargetVersion]
		if !ok {
			result.ErrorMessage = fmt.Sprintf("Unknown target version: %s", config.TargetVersion)
			result.FailedStep = "update_configuration"
			result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
			return result
		}

		// Pull images from registry
		imagesToPull := []string{
			targetVersionInfo.Postgres,
			targetVersionInfo.Kong,
			targetVersionInfo.Studio,
		}
		for _, image := range imagesToPull {
			pullCmd := exec.Command("docker", "pull", image)
			if output, err := pullCmd.CombinedOutput(); err != nil {
				result.ErrorMessage = fmt.Sprintf("Failed to pull image %s: %v - %s", image, err, string(output))
				result.FailedStep = "update_configuration"
				result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
				if config.RollbackOnFailure {
					copyFile(composeBackupFile, composeFile)
				}
				return result
			}
		}

		if err := updateDockerCompose(composeFile, targetVersionInfo, config.Subdomain); err != nil {
			result.ErrorMessage = fmt.Sprintf("Failed to update docker-compose.yml: %v", err)
			result.FailedStep = "update_configuration"
			result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
			if config.RollbackOnFailure {
				copyFile(composeBackupFile, composeFile)
			}
			return result
		}
		result.Steps[len(result.Steps)-1] = stepSuccess(step, "Configuration updated via version registry")
	}

	// Step 6: Rolling restart
	step = stepStart("rolling_restart", "Performing rolling restart of services")
	result.Steps = append(result.Steps, step)

	c := compose.New(config.Subdomain, config.BaseDir)

	// Stop containers gracefully
	if err := c.Stop(ctx); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to stop containers: %v", err)
		result.FailedStep = "rolling_restart"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)

		// Attempt rollback
		if config.RollbackOnFailure {
			result.RollbackApplied = true
			rollbackUpgrade(composeBackupFile, composeFile, c, ctx)
		}
		return result
	}

	// Start with new images
	if err := c.Up(ctx); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to start containers with new images: %v", err)
		result.FailedStep = "rolling_restart"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)

		// Attempt rollback
		if config.RollbackOnFailure {
			result.RollbackApplied = true
			rollbackUpgrade(composeBackupFile, composeFile, c, ctx)
		}
		return result
	}

	result.Steps[len(result.Steps)-1] = stepSuccess(step, "Containers restarted with new versions")

	// Step 7: Wait for containers to stabilize
	step = stepStart("stabilization_wait", "Waiting for containers to stabilize")
	result.Steps = append(result.Steps, step)

	time.Sleep(10 * time.Second)

	result.Steps[len(result.Steps)-1] = stepSuccess(step, "Containers stabilized")

	// Step 8: Post-upgrade health check
	step = stepStart("post_upgrade_health_check", "Verifying upgraded instance health")
	result.Steps = append(result.Steps, step)

	// Give services more time to be fully ready
	time.Sleep(15 * time.Second)

	healthResult, err = health.CheckAll(healthConfig)
	if err != nil || !healthResult.AllHealthy {
		result.ErrorMessage = "Health check failed after upgrade"
		result.FailedStep = "post_upgrade_health_check"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)

		// Attempt rollback
		if config.RollbackOnFailure {
			result.RollbackApplied = true
			rollbackUpgrade(composeBackupFile, composeFile, c, ctx)
			result.ErrorMessage += " - rollback applied"
		}
		return result
	}

	result.Steps[len(result.Steps)-1] = stepSuccess(step, "Health check passed")

	// Step 9: Cleanup backup file
	os.Remove(composeBackupFile)

	// Success
	result.Success = true
	result.UpgradedFrom = config.CurrentVersion
	result.UpgradedTo = config.TargetVersion

	return result
}

// Helper functions

func stepStart(name, message string) types.StepResult {
	return types.StepResult{
		Name:      name,
		Status:    "in_progress",
		Message:   message,
		StartTime: time.Now().UTC().Format(time.RFC3339),
	}
}

func stepSuccess(step types.StepResult, message string) types.StepResult {
	endTime := time.Now().UTC()
	startTime, _ := time.Parse(time.RFC3339, step.StartTime)
	duration := endTime.Sub(startTime)

	return types.StepResult{
		Name:      step.Name,
		Status:    "completed",
		Message:   message,
		StartTime: step.StartTime,
		EndTime:   endTime.Format(time.RFC3339),
		Duration:  duration.String(),
	}
}

func stepFail(step types.StepResult, message string) types.StepResult {
	endTime := time.Now().UTC()
	startTime, _ := time.Parse(time.RFC3339, step.StartTime)
	duration := endTime.Sub(startTime)

	return types.StepResult{
		Name:      step.Name,
		Status:    "failed",
		Message:   message,
		StartTime: step.StartTime,
		EndTime:   endTime.Format(time.RFC3339),
		Duration:  duration.String(),
	}
}

func validateVersionUpgrade(currentVersion, targetVersion string) error {
	// Check if both versions exist in registry
	if _, ok := versionRegistry[currentVersion]; !ok {
		return fmt.Errorf("unknown current version: %s", currentVersion)
	}
	if _, ok := versionRegistry[targetVersion]; !ok {
		return fmt.Errorf("unknown target version: %s", targetVersion)
	}

	// Prevent downgrades (simple version comparison)
	if compareVersions(targetVersion, currentVersion) <= 0 {
		return fmt.Errorf("downgrade not allowed: %s -> %s (use --allow-downgrade flag if intentional)", currentVersion, targetVersion)
	}

	return nil
}

// compareVersions returns:
// -1 if v1 < v2
//  0 if v1 == v2
//  1 if v1 > v2
func compareVersions(v1, v2 string) int {
	// Simple string comparison for now
	// In production, use proper semver parsing
	if v1 == v2 {
		return 0
	}
	if v1 > v2 {
		return 1
	}
	return -1
}

func getFreeDiskSpace(path string) (uint64, error) {
	cmd := exec.Command("df", "-B1", path)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("unexpected df output")
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return 0, fmt.Errorf("unexpected df output format")
	}

	var freeSpace uint64
	fmt.Sscanf(fields[3], "%d", &freeSpace)
	return freeSpace, nil
}

func copyFile(src, dst string) error {
	cmd := exec.Command("cp", src, dst)
	return cmd.Run()
}

func updateDockerCompose(composeFile string, versionInfo types.VersionInfo, subdomain string) error {
	// Read current docker-compose.yml
	content, err := os.ReadFile(composeFile)
	if err != nil {
		return err
	}

	updated := string(content)

	// Update image versions using simple string replacement
	// In production, use proper YAML parsing
	updated = replaceImageVersion(updated, "postgres", versionInfo.Postgres)
	updated = replaceImageVersion(updated, "kong", versionInfo.Kong)
	updated = replaceImageVersion(updated, "studio", versionInfo.Studio)

	// Write updated content
	return os.WriteFile(composeFile, []byte(updated), 0644)
}

func replaceImageVersion(content, serviceName, newImage string) string {
	// Find the service section and replace its image
	// This is a simple implementation - production should use YAML parsing
	lines := strings.Split(content, "\n")
	inService := false

	for i, line := range lines {
		// Check if we're entering the service section
		if strings.Contains(line, serviceName+":") && strings.HasPrefix(strings.TrimSpace(line), serviceName+":") {
			inService = true
			continue
		}

		// If we're in the service and find an image line, replace it
		if inService && strings.Contains(line, "image:") {
			indent := getIndent(line)
			lines[i] = indent + "image: " + newImage
			inService = false
		}

		// Exit service section if we hit another top-level service
		if inService && strings.HasPrefix(line, "  ") && strings.Contains(line, ":") && !strings.HasPrefix(strings.TrimSpace(line), "-") {
			if !strings.HasPrefix(line, "    ") {
				inService = false
			}
		}
	}

	return strings.Join(lines, "\n")
}

func getIndent(line string) string {
	for i, ch := range line {
		if ch != ' ' {
			return line[:i]
		}
	}
	return ""
}

func rollbackUpgrade(backupFile, composeFile string, c *compose.Compose, ctx context.Context) error {
	// Stop current containers
	c.Stop(ctx)

	// Restore previous docker-compose.yml
	if err := copyFile(backupFile, composeFile); err != nil {
		return err
	}

	// Start containers with previous version
	if err := c.Up(ctx); err != nil {
		return err
	}

	return nil
}

// GetCurrentVersion reads the current version from docker-compose.yml
func GetCurrentVersion(subdomain, baseDir string) (string, error) {
	composeFile := filepath.Join(baseDir, subdomain, "docker-compose.yml")

	content, err := os.ReadFile(composeFile)
	if err != nil {
		return "", err
	}

	// Parse postgres image to determine version
	// This is a simple heuristic - in production, store version in metadata
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.Contains(line, "image:") && strings.Contains(line, "supabase/postgres") {
			// Extract version from image tag
			for version, versionInfo := range versionRegistry {
				if strings.Contains(line, versionInfo.Postgres) {
					return version, nil
				}
			}
		}
	}

	return "unknown", fmt.Errorf("could not determine version from docker-compose.yml")
}
