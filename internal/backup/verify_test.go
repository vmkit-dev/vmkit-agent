package backup

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

func TestVerifyGzipIntegrity(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		compress  bool
		expectErr bool
	}{
		{
			name:      "valid gzip",
			content:   "-- PostgreSQL database dump\nSELECT 1;\n",
			compress:  true,
			expectErr: false,
		},
		{
			name:      "invalid gzip (not compressed)",
			content:   "not compressed data",
			compress:  false,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file
			tmpFile := filepath.Join(os.TempDir(), "test_verify.sql.gz")
			defer os.Remove(tmpFile)

			if tt.compress {
				// Create valid gzip file
				f, err := os.Create(tmpFile)
				if err != nil {
					t.Fatalf("Failed to create temp file: %v", err)
				}

				gzWriter := gzip.NewWriter(f)
				_, err = gzWriter.Write([]byte(tt.content))
				if err != nil {
					t.Fatalf("Failed to write gzip content: %v", err)
				}
				gzWriter.Close()
				f.Close()
			} else {
				// Create invalid (uncompressed) file
				err := os.WriteFile(tmpFile, []byte(tt.content), 0644)
				if err != nil {
					t.Fatalf("Failed to write temp file: %v", err)
				}
			}

			err := verifyGzipIntegrity(tmpFile)
			if (err != nil) != tt.expectErr {
				t.Errorf("verifyGzipIntegrity() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

func TestVerifySQLHeader(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		expectErr bool
	}{
		{
			name: "valid pg_dump header",
			content: `--
-- PostgreSQL database dump
--

-- Dumped from database version 15.1
SET statement_timeout = 0;
`,
			expectErr: false,
		},
		{
			name: "valid with CREATE statement",
			content: `SET statement_timeout = 0;
SET lock_timeout = 0;
CREATE TABLE public.users (
    id integer NOT NULL
);
`,
			expectErr: false,
		},
		{
			name: "invalid - not a SQL dump",
			content: `This is not a SQL dump
Just some random text
Nothing SQL-related here
`,
			expectErr: true,
		},
		{
			name:      "invalid - empty file",
			content:   "",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp gzipped file
			tmpFile := filepath.Join(os.TempDir(), "test_sql_header.sql.gz")
			defer os.Remove(tmpFile)

			f, err := os.Create(tmpFile)
			if err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}

			gzWriter := gzip.NewWriter(f)
			_, err = gzWriter.Write([]byte(tt.content))
			if err != nil {
				t.Fatalf("Failed to write gzip content: %v", err)
			}
			gzWriter.Close()
			f.Close()

			err = verifySQLHeader(tmpFile)
			if (err != nil) != tt.expectErr {
				t.Errorf("verifySQLHeader() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

func TestCheckHelpers(t *testing.T) {
	// Test checkStart
	check := checkStart("test_check", "Testing check start")
	if check.Name != "test_check" {
		t.Errorf("checkStart() Name = %s, want test_check", check.Name)
	}
	if check.Status != "in_progress" {
		t.Errorf("checkStart() Status = %s, want in_progress", check.Status)
	}
	if check.Message != "Testing check start" {
		t.Errorf("checkStart() Message = %s, want 'Testing check start'", check.Message)
	}

	// Test checkSuccess
	successCheck := checkSuccess(check, "Check passed")
	if successCheck.Status != "passed" {
		t.Errorf("checkSuccess() Status = %s, want passed", successCheck.Status)
	}
	if successCheck.Message != "Check passed" {
		t.Errorf("checkSuccess() Message = %s, want 'Check passed'", successCheck.Message)
	}

	// Test checkFail
	failCheck := checkFail(check, "Check failed")
	if failCheck.Status != "failed" {
		t.Errorf("checkFail() Status = %s, want failed", failCheck.Status)
	}
	if failCheck.Message != "Check failed" {
		t.Errorf("checkFail() Message = %s, want 'Check failed'", failCheck.Message)
	}
}

func TestVerifyBackupResult_Structure(t *testing.T) {
	// Test that we can create and populate a VerifyBackupResult
	result := types.VerifyBackupResult{
		Success:    true,
		BackupID:   "test-backup-123",
		VerifiedAt: "2026-02-14T10:00:00Z",
		Checks: []types.VerificationCheck{
			{
				Name:     "download_backup",
				Status:   "passed",
				Message:  "Backup downloaded",
				Duration: "500ms",
			},
			{
				Name:    "checksum_verification",
				Status:  "passed",
				Message: "Checksum verified",
			},
		},
		OverallStatus: "verified",
		Duration:      "2s",
	}

	if !result.Success {
		t.Error("Success should be true")
	}

	if result.BackupID != "test-backup-123" {
		t.Errorf("BackupID = %s, want test-backup-123", result.BackupID)
	}

	if len(result.Checks) != 2 {
		t.Errorf("Checks count = %d, want 2", len(result.Checks))
	}

	if result.OverallStatus != "verified" {
		t.Errorf("OverallStatus = %s, want verified", result.OverallStatus)
	}
}

func TestVerifyBackupConfig_Structure(t *testing.T) {
	// Test that we can create a VerifyBackupConfig
	config := types.VerifyBackupConfig{
		BackupID:         "backup-123",
		DownloadURL:      "https://example.com/backup.sql.gz",
		ExpectedChecksum: "sha256:abcdef123456",
		DeepVerify:       true,
		Subdomain:        "test-instance",
		BaseDir:          "/opt/supabase",
	}

	if config.BackupID != "backup-123" {
		t.Errorf("BackupID = %s, want backup-123", config.BackupID)
	}

	if !config.DeepVerify {
		t.Error("DeepVerify should be true")
	}

	if config.Subdomain != "test-instance" {
		t.Errorf("Subdomain = %s, want test-instance", config.Subdomain)
	}
}

func TestRunSanityChecks(t *testing.T) {
	// This test requires Docker to be available
	// In CI/test environments without Docker, this will fail gracefully
	err := runSanityChecks("nonexistent-container", "test_db")

	// We expect an error since the container doesn't exist
	if err == nil {
		t.Error("Expected error for nonexistent container")
	}

	// Verify error message is informative
	if err != nil {
		errMsg := err.Error()
		if errMsg == "" {
			t.Error("Error message should not be empty")
		}
		t.Logf("Expected error for nonexistent container: %v", err)
	}
}
