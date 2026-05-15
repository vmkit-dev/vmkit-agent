package backup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

// Backup performs a backup of a Supabase instance
func Backup(config types.BackupConfig) types.BackupResult {
	result := types.BackupResult{
		Success:    false,
		InstanceID: config.InstanceID,
		Steps:      []types.StepResult{},
		Metadata: types.BackupMetadata{
			InstanceID: config.InstanceID,
			Subdomain:  config.Subdomain,
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			Status:     "in_progress",
		},
	}

	// Set default base directory if not specified
	if config.BaseDir == "" {
		config.BaseDir = "/opt/supabase"
	}

	instanceDir := filepath.Join(config.BaseDir, config.Subdomain)
	composeFile := filepath.Join(instanceDir, "docker-compose.yml")

	// Step 1: Validate instance
	step := stepStart("validate_instance", "Validating instance and checking prerequisites")
	result.Steps = append(result.Steps, step)

	// Check if instance directory exists
	if _, err := os.Stat(instanceDir); os.IsNotExist(err) {
		result.ErrorMessage = fmt.Sprintf("Instance directory not found: %s", instanceDir)
		result.FailedStep = "validate_instance"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	// Check if docker-compose.yml exists
	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		result.ErrorMessage = fmt.Sprintf("Docker Compose file not found: %s", composeFile)
		result.FailedStep = "validate_instance"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	// Check if Docker is available
	if !isDockerAvailable() {
		result.ErrorMessage = "Docker is not available"
		result.FailedStep = "validate_instance"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	// Get Postgres container ID
	postgresContainer, err := getPostgresContainer(instanceDir)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to get Postgres container: %v", err)
		result.FailedStep = "validate_instance"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	// Check disk space (require at least 1GB free)
	freeSpace, err := getFreeDiskSpace("/tmp")
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to check disk space: %v", err)
		result.FailedStep = "validate_instance"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}
	if freeSpace < 1024*1024*1024 { // 1GB in bytes
		result.ErrorMessage = fmt.Sprintf("Insufficient disk space: %d bytes available, need at least 1GB", freeSpace)
		result.FailedStep = "validate_instance"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	result.Steps[len(result.Steps)-1] = stepSuccess(step, "Instance validation successful")

	// Step 2: Create database dump
	step = stepStart("create_dump", "Creating database dump with pg_dump")
	result.Steps = append(result.Steps, step)

	backupFile := filepath.Join("/tmp", fmt.Sprintf("%s_backup_%d.sql.gz", config.Subdomain, time.Now().Unix()))
	defer os.Remove(backupFile) // Clean up temp file

	// Run pg_dump via docker exec and compress
	dumpCmd := exec.Command("docker", "exec", postgresContainer, "pg_dump", "-U", "postgres", "-d", "postgres")
	gzipCmd := exec.Command("gzip")

	// Pipe pg_dump output to gzip
	var errBuf bytes.Buffer
	dumpPipe, err := dumpCmd.StdoutPipe()
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to create dump pipe: %v", err)
		result.FailedStep = "create_dump"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	gzipCmd.Stdin = dumpPipe
	gzipCmd.Stderr = &errBuf

	outFile, err := os.Create(backupFile)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to create backup file: %v", err)
		result.FailedStep = "create_dump"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}
	defer outFile.Close()

	gzipCmd.Stdout = outFile

	// Start both commands
	if err := dumpCmd.Start(); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to start pg_dump: %v", err)
		result.FailedStep = "create_dump"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	if err := gzipCmd.Start(); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to start gzip: %v", err)
		result.FailedStep = "create_dump"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	// Wait for both commands to finish
	if err := dumpCmd.Wait(); err != nil {
		result.ErrorMessage = fmt.Sprintf("pg_dump failed: %v", err)
		result.FailedStep = "create_dump"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	if err := gzipCmd.Wait(); err != nil {
		result.ErrorMessage = fmt.Sprintf("gzip failed: %v - %s", err, errBuf.String())
		result.FailedStep = "create_dump"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	// Get backup file info
	fileInfo, err := os.Stat(backupFile)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to stat backup file: %v", err)
		result.FailedStep = "create_dump"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	result.Metadata.SizeBytes = fileInfo.Size()
	result.Steps[len(result.Steps)-1] = stepSuccess(step, fmt.Sprintf("Database dump created: %d bytes", result.Metadata.SizeBytes))

	// Step 3: Calculate checksum
	step = stepStart("calculate_checksum", "Calculating backup checksum")
	result.Steps = append(result.Steps, step)

	checksum, err := calculateFileChecksum(backupFile)
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to calculate checksum: %v", err)
		result.FailedStep = "calculate_checksum"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	result.Metadata.Checksum = checksum
	result.Steps[len(result.Steps)-1] = stepSuccess(step, fmt.Sprintf("Checksum: %s", checksum))

	// Step 4: Upload database backup
	step = stepStart("upload_database", "Uploading database backup to storage")
	result.Steps = append(result.Steps, step)

	if config.UploadURL == "" {
		result.Steps[len(result.Steps)-1] = stepSuccess(step, "Upload skipped (no upload_url configured, backup saved locally)")
	} else {
		if err := uploadFile(backupFile, config.UploadURL); err != nil {
			result.ErrorMessage = fmt.Sprintf("Failed to upload database backup: %v", err)
			result.FailedStep = "upload_database"
			result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
			return result
		}

		result.Steps[len(result.Steps)-1] = stepSuccess(step, "Database backup uploaded successfully")
	}

	// Step 5: Backup storage files (if enabled)
	if config.IncludeStorage && config.StorageUploadURL != "" {
		step = stepStart("backup_storage", "Backing up storage files")
		result.Steps = append(result.Steps, step)

		storageBackupFile := filepath.Join("/tmp", fmt.Sprintf("%s_storage_%d.tar.gz", config.Subdomain, time.Now().Unix()))
		defer os.Remove(storageBackupFile)

		// Get storage container
		storageContainer, err := getStorageContainer(instanceDir)
		if err == nil && storageContainer != "" {
			// Export storage data from container
			if err := exportStorageData(storageContainer, storageBackupFile); err != nil {
				// Don't fail the entire backup if storage export fails
				result.Steps[len(result.Steps)-1] = stepSuccess(step, fmt.Sprintf("Storage backup skipped: %v", err))
			} else {
				// Get storage backup file info
				storageInfo, err := os.Stat(storageBackupFile)
				if err == nil && storageInfo.Size() > 100 { // Only upload if there's meaningful content
					result.Metadata.StorageSizeBytes = storageInfo.Size()

					// Calculate storage checksum
					storageChecksum, err := calculateFileChecksum(storageBackupFile)
					if err == nil {
						result.Metadata.StorageChecksum = storageChecksum
					}

					// Upload storage backup
					if err := uploadFile(storageBackupFile, config.StorageUploadURL); err != nil {
						result.Steps[len(result.Steps)-1] = stepSuccess(step, fmt.Sprintf("Storage backup upload failed: %v", err))
					} else {
						result.Steps[len(result.Steps)-1] = stepSuccess(step, fmt.Sprintf("Storage backup uploaded: %d bytes", result.Metadata.StorageSizeBytes))
					}
				} else {
					result.Steps[len(result.Steps)-1] = stepSuccess(step, "Storage directory is empty, skipping")
				}
			}
		} else {
			result.Steps[len(result.Steps)-1] = stepSuccess(step, "Storage container not running, skipping")
		}
	}

	// Success
	result.Success = true
	result.Metadata.Status = "completed"
	result.BackupID = fmt.Sprintf("backup_%s_%d", config.Subdomain, time.Now().Unix())

	return result
}

// Restore performs a restore of a Supabase instance from backup
func Restore(config types.RestoreConfig) types.RestoreResult {
	result := types.RestoreResult{
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

	// Step 1: Validate instance
	step := stepStart("validate_instance", "Validating instance")
	result.Steps = append(result.Steps, step)

	if _, err := os.Stat(instanceDir); os.IsNotExist(err) {
		result.ErrorMessage = fmt.Sprintf("Instance directory not found: %s", instanceDir)
		result.FailedStep = "validate_instance"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		result.ErrorMessage = fmt.Sprintf("Docker Compose file not found: %s", composeFile)
		result.FailedStep = "validate_instance"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	result.Steps[len(result.Steps)-1] = stepSuccess(step, "Instance validated")

	// Step 2: Download backup
	step = stepStart("download_backup", "Downloading backup from storage")
	result.Steps = append(result.Steps, step)

	backupFile := filepath.Join("/tmp", fmt.Sprintf("%s_restore_%d.sql.gz", config.Subdomain, time.Now().Unix()))
	defer os.Remove(backupFile)

	if err := downloadFile(config.DownloadURL, backupFile); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to download backup: %v", err)
		result.FailedStep = "download_backup"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	result.Steps[len(result.Steps)-1] = stepSuccess(step, "Backup downloaded successfully")

	// Step 3: Verify checksum (if requested)
	if config.VerifyChecksum && config.ExpectedChecksum != "" {
		step = stepStart("verify_checksum", "Verifying backup integrity")
		result.Steps = append(result.Steps, step)

		checksum, err := calculateFileChecksum(backupFile)
		if err != nil {
			result.ErrorMessage = fmt.Sprintf("Failed to calculate checksum: %v", err)
			result.FailedStep = "verify_checksum"
			result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
			return result
		}

		if checksum != config.ExpectedChecksum {
			result.ErrorMessage = fmt.Sprintf("Checksum mismatch: expected %s, got %s", config.ExpectedChecksum, checksum)
			result.FailedStep = "verify_checksum"
			result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
			return result
		}

		result.Steps[len(result.Steps)-1] = stepSuccess(step, "Checksum verified")
	}

	// Step 4: Stop instance (if requested)
	if config.StopInstance {
		step = stepStart("stop_instance", "Stopping instance containers")
		result.Steps = append(result.Steps, step)

		stopCmd := exec.Command("docker", "compose", "-f", composeFile, "stop")
		if output, err := stopCmd.CombinedOutput(); err != nil {
			result.ErrorMessage = fmt.Sprintf("Failed to stop instance: %v - %s", err, string(output))
			result.FailedStep = "stop_instance"
			result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
			return result
		}

		result.Steps[len(result.Steps)-1] = stepSuccess(step, "Instance stopped")
	}

	// Step 5: Restore database
	step = stepStart("restore_database", "Restoring database from backup")
	result.Steps = append(result.Steps, step)

	// Get Postgres container (start it if needed)
	postgresContainer, err := getPostgresContainer(instanceDir)
	if err != nil {
		// Try to start the database container
		startCmd := exec.Command("docker", "compose", "-f", composeFile, "start", "postgres")
		if output, err := startCmd.CombinedOutput(); err != nil {
			result.ErrorMessage = fmt.Sprintf("Failed to start database container: %v - %s", err, string(output))
			result.FailedStep = "restore_database"
			result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
			return result
		}

		// Wait a bit for container to be ready
		time.Sleep(5 * time.Second)

		postgresContainer, err = getPostgresContainer(instanceDir)
		if err != nil {
			result.ErrorMessage = fmt.Sprintf("Failed to get Postgres container after start: %v", err)
			result.FailedStep = "restore_database"
			result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
			return result
		}
	}

	// Drop existing database and recreate
	dropCmd := exec.Command("docker", "exec", postgresContainer, "psql", "-U", "postgres", "-c", "DROP DATABASE IF EXISTS postgres;")
	if output, err := dropCmd.CombinedOutput(); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to drop database: %v - %s", err, string(output))
		result.FailedStep = "restore_database"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	createCmd := exec.Command("docker", "exec", postgresContainer, "psql", "-U", "postgres", "-c", "CREATE DATABASE postgres;")
	if output, err := createCmd.CombinedOutput(); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to create database: %v - %s", err, string(output))
		result.FailedStep = "restore_database"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	// Restore from backup
	gunzipCmd := exec.Command("gunzip", "-c", backupFile)
	psqlCmd := exec.Command("docker", "exec", "-i", postgresContainer, "psql", "-U", "postgres", "-d", "postgres")

	gunzipPipe, err := gunzipCmd.StdoutPipe()
	if err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to create gunzip pipe: %v", err)
		result.FailedStep = "restore_database"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	psqlCmd.Stdin = gunzipPipe
	var psqlErr bytes.Buffer
	psqlCmd.Stderr = &psqlErr

	if err := gunzipCmd.Start(); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to start gunzip: %v", err)
		result.FailedStep = "restore_database"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	if err := psqlCmd.Start(); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to start psql: %v", err)
		result.FailedStep = "restore_database"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	if err := gunzipCmd.Wait(); err != nil {
		result.ErrorMessage = fmt.Sprintf("gunzip failed: %v", err)
		result.FailedStep = "restore_database"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	if err := psqlCmd.Wait(); err != nil {
		// psql may return non-zero even on successful restore due to warnings
		// Only fail if stderr contains actual errors
		errOutput := psqlErr.String()
		if strings.Contains(errOutput, "ERROR") || strings.Contains(errOutput, "FATAL") {
			result.ErrorMessage = fmt.Sprintf("psql failed: %v - %s", err, errOutput)
			result.FailedStep = "restore_database"
			result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
			return result
		}
	}

	result.Steps[len(result.Steps)-1] = stepSuccess(step, "Database restored successfully")

	// Step 5b: Fix auth-schema permissions.
	// pg_dump carries RLS policies and grants that bind to the source cluster's
	// role OIDs; once restored here, supabase_auth_admin gets 'permission denied
	// for table users' and GoTrue 500s on every request. Re-grant + drop RLS on
	// auth.* restores a working baseline. Idempotent — safe on our own dumps too.
	// See docs/migration-rls-gotcha.md (supabyoi-ktyi).
	step = stepStart("fix_auth_permissions", "Restoring auth schema grants and disabling migrated RLS")
	result.Steps = append(result.Steps, step)

	if err := fixAuthPermissions(postgresContainer); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to fix auth permissions: %v", err)
		result.FailedStep = "fix_auth_permissions"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	result.Steps[len(result.Steps)-1] = stepSuccess(step, "Auth schema grants and RLS reset")

	// Step 6: Restore storage (if download URL provided)
	if config.StorageDownloadURL != "" {
		step = stepStart("restore_storage", "Restoring storage files")
		result.Steps = append(result.Steps, step)

		storageBackupFile := filepath.Join("/tmp", fmt.Sprintf("%s_storage_restore_%d.tar.gz", config.Subdomain, time.Now().Unix()))
		defer os.Remove(storageBackupFile)

		if err := downloadFile(config.StorageDownloadURL, storageBackupFile); err != nil {
			// Don't fail the entire restore if storage download fails
			result.Steps[len(result.Steps)-1] = stepSuccess(step, fmt.Sprintf("Storage restore skipped: %v", err))
		} else {
			storageContainer, err := getStorageContainer(instanceDir)
			if err == nil && storageContainer != "" {
				if err := importStorageData(storageContainer, storageBackupFile); err != nil {
					result.Steps[len(result.Steps)-1] = stepSuccess(step, fmt.Sprintf("Storage restore failed: %v", err))
				} else {
					result.Steps[len(result.Steps)-1] = stepSuccess(step, "Storage files restored successfully")
				}
			} else {
				result.Steps[len(result.Steps)-1] = stepSuccess(step, "Storage container not available, skipping")
			}
		}
	}

	// Step 7: Restart instance
	step = stepStart("restart_instance", "Restarting instance")
	result.Steps = append(result.Steps, step)

	restartCmd := exec.Command("docker", "compose", "-f", composeFile, "restart")
	if output, err := restartCmd.CombinedOutput(); err != nil {
		result.ErrorMessage = fmt.Sprintf("Failed to restart instance: %v - %s", err, string(output))
		result.FailedStep = "restart_instance"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	result.Steps[len(result.Steps)-1] = stepSuccess(step, "Instance restarted")

	// Step 8: Health check
	step = stepStart("health_check", "Performing health check")
	result.Steps = append(result.Steps, step)

	// Wait a bit for services to be ready
	time.Sleep(10 * time.Second)

	if !isInstanceHealthy(instanceDir) {
		result.ErrorMessage = "Health check failed after restore"
		result.FailedStep = "health_check"
		result.Steps[len(result.Steps)-1] = stepFail(step, result.ErrorMessage)
		return result
	}

	result.Steps[len(result.Steps)-1] = stepSuccess(step, "Health check passed")

	result.Success = true
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

func isDockerAvailable() bool {
	cmd := exec.Command("docker", "ps")
	return cmd.Run() == nil
}

func getPostgresContainer(instanceDir string) (string, error) {
	composeFile := filepath.Join(instanceDir, "docker-compose.yml")
	cmd := exec.Command("docker", "compose", "-f", composeFile, "ps", "-q", "postgres")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	containerID := strings.TrimSpace(string(output))
	if containerID == "" {
		return "", fmt.Errorf("postgres container not running")
	}

	return containerID, nil
}

func getStorageContainer(instanceDir string) (string, error) {
	composeFile := filepath.Join(instanceDir, "docker-compose.yml")
	cmd := exec.Command("docker", "compose", "-f", composeFile, "ps", "-q", "storage")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	containerID := strings.TrimSpace(string(output))
	return containerID, nil
}

func getFreeDiskSpace(path string) (uint64, error) {
	// Use df command to get free space
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

func calculateFileChecksum(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func uploadFile(filePath, uploadURL string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", uploadURL, file)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/gzip")
	req.ContentLength = fileInfo.Size()

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func downloadFile(downloadURL, destPath string) error {
	resp, err := http.Get(downloadURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download failed with status %d: %s", resp.StatusCode, string(body))
	}

	outFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, resp.Body)
	return err
}

func exportStorageData(containerID, destPath string) error {
	// Export storage directory from container as tar.gz
	cmd := exec.Command("docker", "cp", fmt.Sprintf("%s:/var/lib/storage", containerID), "-")
	gzipCmd := exec.Command("gzip")

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	gzipCmd.Stdin = pipe

	outFile, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	gzipCmd.Stdout = outFile

	if err := cmd.Start(); err != nil {
		return err
	}

	if err := gzipCmd.Start(); err != nil {
		return err
	}

	if err := cmd.Wait(); err != nil {
		return err
	}

	return gzipCmd.Wait()
}

func importStorageData(containerID, srcPath string) error {
	// Import storage data into container
	gunzipCmd := exec.Command("gunzip", "-c", srcPath)
	dockerCmd := exec.Command("docker", "cp", "-", fmt.Sprintf("%s:/var/lib/", containerID))

	pipe, err := gunzipCmd.StdoutPipe()
	if err != nil {
		return err
	}

	dockerCmd.Stdin = pipe

	if err := gunzipCmd.Start(); err != nil {
		return err
	}

	if err := dockerCmd.Start(); err != nil {
		return err
	}

	if err := gunzipCmd.Wait(); err != nil {
		return err
	}

	return dockerCmd.Wait()
}

// authFixupSQL returns the idempotent SQL that restores supabase_auth_admin
// grants on the auth schema and disables any RLS policies that were carried
// over from a foreign cluster's pg_dump.
func authFixupSQL() string {
	return `GRANT ALL ON SCHEMA auth TO supabase_auth_admin;
GRANT ALL ON ALL TABLES IN SCHEMA auth TO supabase_auth_admin;
GRANT ALL ON ALL SEQUENCES IN SCHEMA auth TO supabase_auth_admin;
DO $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'auth'
    LOOP
        EXECUTE 'ALTER TABLE auth.' || quote_ident(r.tablename) || ' DISABLE ROW LEVEL SECURITY';
    END LOOP;
END
$$;
`
}

// fixAuthPermissions runs authFixupSQL inside the instance's postgres container.
func fixAuthPermissions(postgresContainer string) error {
	cmd := exec.Command("docker", "exec", "-i", postgresContainer, "psql", "-U", "postgres", "-d", "postgres", "-v", "ON_ERROR_STOP=1")
	cmd.Stdin = strings.NewReader(authFixupSQL())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("psql failed: %v - %s", err, string(output))
	}
	return nil
}

func isInstanceHealthy(instanceDir string) bool {
	composeFile := filepath.Join(instanceDir, "docker-compose.yml")
	cmd := exec.Command("docker", "compose", "-f", composeFile, "ps", "--format", "json")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	// Check if all containers are running
	// This is a basic check - could be enhanced with actual health checks
	return len(output) > 0 && !strings.Contains(string(output), "\"State\":\"exited\"")
}
