package backup

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

// Verify performs verification of a backup file
func Verify(config types.VerifyBackupConfig) types.VerifyBackupResult {
	startTime := time.Now()
	result := types.VerifyBackupResult{
		Success:       false,
		BackupID:      config.BackupID,
		VerifiedAt:    time.Now().UTC().Format(time.RFC3339),
		Checks:        []types.VerificationCheck{},
		OverallStatus: "failed",
	}

	// Step 1: Download backup
	check := checkStart("download_backup", "Downloading backup file for verification")

	backupFile := filepath.Join("/tmp", fmt.Sprintf("verify_%s_%d.sql.gz", config.BackupID, time.Now().Unix()))
	defer os.Remove(backupFile) // Clean up temp file

	if err := downloadFile(config.DownloadURL, backupFile); err != nil {
		result.Checks = append(result.Checks, checkFail(check, fmt.Sprintf("Download failed: %v", err)))
		result.ErrorMessage = fmt.Sprintf("Failed to download backup: %v", err)
		result.Duration = time.Since(startTime).String()
		return result
	}

	result.Checks = append(result.Checks, checkSuccess(check, "Backup downloaded successfully"))

	// Step 2: Verify file exists and has content
	check = checkStart("file_check", "Verifying file integrity")

	fileInfo, err := os.Stat(backupFile)
	if err != nil {
		result.Checks = append(result.Checks, checkFail(check, fmt.Sprintf("File stat failed: %v", err)))
		result.ErrorMessage = "Backup file not accessible"
		result.Duration = time.Since(startTime).String()
		return result
	}

	if fileInfo.Size() == 0 {
		result.Checks = append(result.Checks, checkFail(check, "Backup file is empty"))
		result.ErrorMessage = "Backup file has zero size"
		result.Duration = time.Since(startTime).String()
		return result
	}

	result.Checks = append(result.Checks, checkSuccess(check, fmt.Sprintf("File exists: %d bytes", fileInfo.Size())))

	// Step 3: Verify checksum (if provided)
	if config.ExpectedChecksum != "" {
		check = checkStart("checksum_verification", "Verifying backup checksum")

		actualChecksum, err := calculateFileChecksum(backupFile)
		if err != nil {
			result.Checks = append(result.Checks, checkFail(check, fmt.Sprintf("Checksum calculation failed: %v", err)))
			result.ErrorMessage = "Failed to calculate checksum"
			result.Duration = time.Since(startTime).String()
			return result
		}

		if actualChecksum != config.ExpectedChecksum {
			result.Checks = append(result.Checks, checkFail(check,
				fmt.Sprintf("Checksum mismatch: expected %s, got %s", config.ExpectedChecksum, actualChecksum)))
			result.ErrorMessage = "Checksum verification failed - backup may be corrupted"
			result.Duration = time.Since(startTime).String()
			return result
		}

		result.Checks = append(result.Checks, checkSuccess(check, "Checksum verified"))
	} else {
		result.Checks = append(result.Checks, types.VerificationCheck{
			Name:    "checksum_verification",
			Status:  "skipped",
			Message: "No expected checksum provided",
		})
	}

	// Step 4: Verify gzip integrity
	check = checkStart("gzip_integrity", "Testing gzip compression integrity")

	if err := verifyGzipIntegrity(backupFile); err != nil {
		result.Checks = append(result.Checks, checkFail(check, fmt.Sprintf("gzip integrity test failed: %v", err)))
		result.ErrorMessage = "Backup file is corrupted (gzip test failed)"
		result.Duration = time.Since(startTime).String()
		return result
	}

	result.Checks = append(result.Checks, checkSuccess(check, "gzip integrity verified"))

	// Step 5: Verify SQL header
	check = checkStart("sql_header", "Validating SQL dump format")

	if err := verifySQLHeader(backupFile); err != nil {
		result.Checks = append(result.Checks, checkFail(check, fmt.Sprintf("SQL validation failed: %v", err)))
		result.ErrorMessage = "Invalid SQL dump format"
		result.Duration = time.Since(startTime).String()
		return result
	}

	result.Checks = append(result.Checks, checkSuccess(check, "SQL dump format validated"))

	// Step 6: Deep verification (optional test restore)
	if config.DeepVerify {
		check = checkStart("deep_verify", "Performing test restore to temporary database")

		if err := performDeepVerification(backupFile, config.Subdomain, config.BaseDir); err != nil {
			result.Checks = append(result.Checks, checkFail(check, fmt.Sprintf("Test restore failed: %v", err)))
			result.ErrorMessage = "Deep verification failed - backup may not be restorable"
			result.OverallStatus = "partial" // Other checks passed, but deep verify failed
			result.Duration = time.Since(startTime).String()
			return result
		}

		result.Checks = append(result.Checks, checkSuccess(check, "Test restore successful"))
	} else {
		result.Checks = append(result.Checks, types.VerificationCheck{
			Name:    "deep_verify",
			Status:  "skipped",
			Message: "Deep verification not requested",
		})
	}

	// All checks passed
	result.Success = true
	result.OverallStatus = "verified"
	result.Duration = time.Since(startTime).String()
	return result
}

// verifyGzipIntegrity tests if the gzip file is valid
func verifyGzipIntegrity(filePath string) error {
	cmd := exec.Command("gzip", "-t", filePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gzip test failed: %v - %s", err, string(output))
	}
	return nil
}

// verifySQLHeader checks if the file contains a valid pg_dump SQL header
func verifySQLHeader(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	// Decompress gzip
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %v", err)
	}
	defer gzReader.Close()

	// Read first few lines
	scanner := bufio.NewScanner(gzReader)
	lineCount := 0
	foundPgDump := false

	for scanner.Scan() && lineCount < 50 {
		line := scanner.Text()
		lineCount++

		// Look for pg_dump signature in comments
		if strings.Contains(line, "PostgreSQL database dump") ||
			strings.Contains(line, "pg_dump") ||
			strings.Contains(line, "Dumped from database version") {
			foundPgDump = true
			break
		}

		// Check for SQL commands that indicate a valid dump
		if strings.HasPrefix(line, "CREATE ") ||
			strings.HasPrefix(line, "SET ") ||
			strings.HasPrefix(line, "SELECT pg_catalog.") {
			foundPgDump = true
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading SQL content: %v", err)
	}

	if !foundPgDump {
		return fmt.Errorf("file does not appear to be a valid PostgreSQL dump")
	}

	return nil
}

// performDeepVerification performs a test restore to a temporary database
func performDeepVerification(backupFile, subdomain, baseDir string) error {
	if baseDir == "" {
		baseDir = "/opt/supabase"
	}

	instanceDir := filepath.Join(baseDir, subdomain)
	composeFile := filepath.Join(instanceDir, "docker-compose.yml")

	// Check if instance exists (we need it to get database connection info)
	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		return fmt.Errorf("instance not found at %s (required for deep verification)", instanceDir)
	}

	// Get postgres container
	postgresContainer, err := getPostgresContainer(instanceDir)
	if err != nil {
		return fmt.Errorf("failed to get postgres container: %v", err)
	}

	// Create temporary test database
	testDBName := fmt.Sprintf("verify_test_%d", time.Now().Unix())

	// Create test database
	createCmd := exec.Command("docker", "exec", postgresContainer,
		"psql", "-U", "postgres", "-c", fmt.Sprintf("CREATE DATABASE %s;", testDBName))
	if output, err := createCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create test database: %v - %s", err, string(output))
	}

	// Ensure we clean up the test database
	defer func() {
		dropCmd := exec.Command("docker", "exec", postgresContainer,
			"psql", "-U", "postgres", "-c", fmt.Sprintf("DROP DATABASE IF EXISTS %s;", testDBName))
		dropCmd.Run() // Best effort cleanup
	}()

	// Restore backup to test database
	gunzipCmd := exec.Command("gunzip", "-c", backupFile)
	psqlCmd := exec.Command("docker", "exec", "-i", postgresContainer,
		"psql", "-U", "postgres", "-d", testDBName, "-q")

	gunzipPipe, err := gunzipCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create gunzip pipe: %v", err)
	}

	psqlCmd.Stdin = gunzipPipe
	var psqlErr strings.Builder
	psqlCmd.Stderr = &psqlErr

	if err := gunzipCmd.Start(); err != nil {
		return fmt.Errorf("failed to start gunzip: %v", err)
	}

	if err := psqlCmd.Start(); err != nil {
		return fmt.Errorf("failed to start psql: %v", err)
	}

	if err := gunzipCmd.Wait(); err != nil {
		return fmt.Errorf("gunzip failed: %v", err)
	}

	if err := psqlCmd.Wait(); err != nil {
		// psql may return non-zero even on successful restore due to warnings
		errOutput := psqlErr.String()
		if strings.Contains(errOutput, "ERROR") || strings.Contains(errOutput, "FATAL") {
			return fmt.Errorf("restore to test database failed: %v - %s", err, errOutput)
		}
	}

	// Run sanity checks on restored database
	if err := runSanityChecks(postgresContainer, testDBName); err != nil {
		return fmt.Errorf("sanity checks failed: %v", err)
	}

	return nil
}

// runSanityChecks performs basic validation on a restored database
func runSanityChecks(containerID, dbName string) error {
	// Check 1: Can connect to database
	connectCmd := exec.Command("docker", "exec", containerID,
		"psql", "-U", "postgres", "-d", dbName, "-c", "SELECT 1;")
	if output, err := connectCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("connection test failed: %v - %s", err, string(output))
	}

	// Check 2: Count tables (should have at least some tables from a real backup)
	countCmd := exec.Command("docker", "exec", containerID,
		"psql", "-U", "postgres", "-d", dbName, "-t", "-c",
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog', 'information_schema');")
	output, err := countCmd.Output()
	if err != nil {
		return fmt.Errorf("table count query failed: %v", err)
	}

	tableCount := strings.TrimSpace(string(output))
	if tableCount == "0" {
		return fmt.Errorf("restored database has no tables (may be empty backup)")
	}

	// Check 3: Verify key Supabase schemas exist (if this is a Supabase backup)
	schemaCmd := exec.Command("docker", "exec", containerID,
		"psql", "-U", "postgres", "-d", dbName, "-t", "-c",
		"SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name IN ('auth', 'storage', 'public');")
	output, err = schemaCmd.Output()
	if err != nil {
		// Schema check is optional - don't fail if it errors
		return nil
	}

	schemaCount := strings.TrimSpace(string(output))
	if schemaCount == "0" {
		// This might not be a Supabase backup, but that's okay
		// As long as it has tables, it's valid
		return nil
	}

	return nil
}

// Helper functions for verification checks

func checkStart(name, message string) types.VerificationCheck {
	return types.VerificationCheck{
		Name:    name,
		Status:  "in_progress",
		Message: message,
	}
}

func checkSuccess(check types.VerificationCheck, message string) types.VerificationCheck {
	return types.VerificationCheck{
		Name:    check.Name,
		Status:  "passed",
		Message: message,
	}
}

func checkFail(check types.VerificationCheck, message string) types.VerificationCheck {
	return types.VerificationCheck{
		Name:    check.Name,
		Status:  "failed",
		Message: message,
	}
}
