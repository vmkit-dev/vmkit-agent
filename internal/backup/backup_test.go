package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

func TestBackupValidation(t *testing.T) {
	tests := []struct {
		name          string
		config        types.BackupConfig
		shouldFail    bool
		expectedError string
	}{
		{
			name: "missing instance directory",
			config: types.BackupConfig{
				InstanceID:  "test-instance",
				Subdomain:   "nonexistent",
				BaseDir:     "/tmp/test-backup",
				UploadURL:   "https://example.com/upload",
			},
			shouldFail:    true,
			expectedError: "Instance directory not found",
		},
		{
			name: "missing upload URL",
			config: types.BackupConfig{
				InstanceID: "test-instance",
				Subdomain:  "test",
				BaseDir:    "/tmp/test-backup",
				UploadURL:  "",
			},
			shouldFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Backup(tt.config)

			if tt.shouldFail {
				if result.Success {
					t.Error("Expected backup to fail, but it succeeded")
				}
				if tt.expectedError != "" && result.ErrorMessage == "" {
					t.Error("Expected error message but got none")
				}
			} else {
				if !result.Success {
					t.Errorf("Expected backup to succeed, but it failed: %s", result.ErrorMessage)
				}
			}
		})
	}
}

func TestRestoreValidation(t *testing.T) {
	tests := []struct {
		name          string
		config        types.RestoreConfig
		shouldFail    bool
		expectedError string
	}{
		{
			name: "missing instance directory",
			config: types.RestoreConfig{
				InstanceID:  "test-instance",
				Subdomain:   "nonexistent",
				BaseDir:     "/tmp/test-restore",
				DownloadURL: "https://example.com/backup.sql.gz",
			},
			shouldFail:    true,
			expectedError: "Instance directory not found",
		},
		{
			name: "missing download URL",
			config: types.RestoreConfig{
				InstanceID:  "test-instance",
				Subdomain:   "test",
				BaseDir:     "/tmp/test-restore",
				DownloadURL: "",
			},
			shouldFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Restore(tt.config)

			if tt.shouldFail {
				if result.Success {
					t.Error("Expected restore to fail, but it succeeded")
				}
				if tt.expectedError != "" && result.ErrorMessage == "" {
					t.Error("Expected error message but got none")
				}
			} else {
				if !result.Success {
					t.Errorf("Expected restore to succeed, but it failed: %s", result.ErrorMessage)
				}
			}
		})
	}
}

func TestCalculateFileChecksum(t *testing.T) {
	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	content := []byte("test content for checksum")
	if err := os.WriteFile(testFile, content, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	checksum, err := calculateFileChecksum(testFile)
	if err != nil {
		t.Fatalf("Failed to calculate checksum: %v", err)
	}

	// Verify checksum format
	if len(checksum) == 0 {
		t.Error("Checksum is empty")
	}

	if checksum[:7] != "sha256:" {
		t.Error("Checksum does not start with 'sha256:'")
	}

	// Calculate again and verify it's consistent
	checksum2, err := calculateFileChecksum(testFile)
	if err != nil {
		t.Fatalf("Failed to calculate checksum second time: %v", err)
	}

	if checksum != checksum2 {
		t.Error("Checksums are not consistent")
	}
}

// TestAuthFixupSQL verifies the SQL emitted after a restore to re-grant
// supabase_auth_admin on the auth schema and drop any RLS policies carried
// over from a foreign pg_dump. Regression: supabyoi-ktyi. The SQL must be
// idempotent — ran on every restore, including re-restores of our own dumps.
func TestAuthFixupSQL(t *testing.T) {
	sql := authFixupSQL()

	mustContain := []string{
		"GRANT ALL ON SCHEMA auth TO supabase_auth_admin",
		"GRANT ALL ON ALL TABLES IN SCHEMA auth TO supabase_auth_admin",
		"GRANT ALL ON ALL SEQUENCES IN SCHEMA auth TO supabase_auth_admin",
		"schemaname = 'auth'",
		"DISABLE ROW LEVEL SECURITY",
		"quote_ident(r.tablename)",
	}
	for _, want := range mustContain {
		if !strings.Contains(sql, want) {
			t.Errorf("authFixupSQL missing %q\nfull SQL:\n%s", want, sql)
		}
	}

	// The DO block must stay inside an anonymous PL/pgSQL block so psql
	// parses it correctly even when piped on stdin.
	if !strings.Contains(sql, "DO $$") || !strings.Contains(sql, "END\n$$;") {
		t.Errorf("authFixupSQL DO block malformed, got:\n%s", sql)
	}
}

func TestIsDockerAvailable(t *testing.T) {
	// This test will succeed if Docker is installed, fail otherwise
	// It's more of an integration test
	available := isDockerAvailable()
	t.Logf("Docker available: %v", available)
}

func TestGetFreeDiskSpace(t *testing.T) {
	// df -B1 is Linux-only; skip on macOS/other platforms
	if _, err := os.Stat("/proc"); os.IsNotExist(err) {
		t.Skip("Skipping: df -B1 not available on this platform (Linux only)")
	}

	space, err := getFreeDiskSpace("/tmp")
	if err != nil {
		t.Fatalf("Failed to get disk space: %v", err)
	}

	if space == 0 {
		t.Error("Free disk space is reported as 0")
	}

	t.Logf("Free disk space in /tmp: %d bytes", space)
}
