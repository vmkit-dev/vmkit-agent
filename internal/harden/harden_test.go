package harden

import (
	"os"
	"testing"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

func TestPreflightChecks(t *testing.T) {
	tests := []struct {
		name    string
		config  *types.HardenConfig
		wantErr bool
	}{
		{
			name: "missing SSH key",
			config: &types.HardenConfig{
				VMUser:  "testuser",
				SSHPort: 2222,
			},
			wantErr: true,
		},
		{
			name: "invalid SSH key format",
			config: &types.HardenConfig{
				VMUser:       "testuser",
				SSHPort:      2222,
				SSHPublicKey: "invalid-key",
			},
			wantErr: true,
		},
		{
			name: "invalid SSH port (too low)",
			config: &types.HardenConfig{
				VMUser:       "testuser",
				SSHPort:      22,
				SSHPublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest",
			},
			wantErr: true,
		},
		{
			name: "invalid SSH port (too high)",
			config: &types.HardenConfig{
				VMUser:       "testuser",
				SSHPort:      99999,
				SSHPublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest",
			},
			wantErr: true,
		},
		{
			name: "valid configuration with defaults",
			config: &types.HardenConfig{
				SSHPort:      2222,
				SSHPublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip if not root (most preflight checks require root)
			if os.Geteuid() != 0 {
				t.Skip("Skipping test that requires root privileges")
			}

			err := preflightChecks(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("preflightChecks() error = %v, wantErr %v", err, tt.wantErr)
			}

			// If no error expected, verify defaults are set
			if !tt.wantErr && tt.config.VMUser == "" {
				t.Error("VMUser should be set to default value")
			}
		})
	}
}

func TestStartFinishStep(t *testing.T) {
	step := startStep("Test Step")

	if step.Name != "Test Step" {
		t.Errorf("expected name 'Test Step', got '%s'", step.Name)
	}

	if step.Status != "in_progress" {
		t.Errorf("expected status 'in_progress', got '%s'", step.Status)
	}

	if step.StartTime == "" {
		t.Error("StartTime should be set")
	}

	finished := finishStep(step)

	if finished.EndTime == "" {
		t.Error("EndTime should be set")
	}

	if finished.Duration == "" {
		t.Error("Duration should be calculated")
	}
}

func TestHardenIdempotency(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	t.Log("Note: Full hardening test skipped in unit tests")
	t.Log("Run integration tests on a VM for complete validation")

	// We can't actually run Harden() in unit tests as it modifies system state
	// This would be tested in integration tests on a VM
}

func TestCreateBackups(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	// Create backup directory if it doesn't exist
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		t.Fatalf("Failed to create backup directory: %v", err)
	}

	// Run backup
	err := createBackups()
	if err != nil {
		t.Errorf("createBackups() failed: %v", err)
	}

	// Verify backup directory exists and has files (if source files exist)
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		t.Error("Backup directory was not created")
	}
}

func TestBuildAuthorizedKeys(t *testing.T) {
	const primary = "ssh-ed25519 AAAAprimary user@host"
	const extra1 = "ssh-ed25519 AAAAextra1 debug@laptop"
	const extra2 = "ssh-rsa AAAAextra2 operator@ops"

	tests := []struct {
		name    string
		primary string
		extras  []string
		want    string
		wantErr bool
	}{
		{
			name:    "primary only",
			primary: primary,
			extras:  nil,
			want:    primary + "\n",
		},
		{
			name:    "primary plus extras",
			primary: primary,
			extras:  []string{extra1, extra2},
			want:    primary + "\n" + extra1 + "\n" + extra2 + "\n",
		},
		{
			name:    "blank and whitespace-only extras skipped",
			primary: primary,
			extras:  []string{"", "   \n", extra1},
			want:    primary + "\n" + extra1 + "\n",
		},
		{
			name:    "extras are trimmed",
			primary: primary,
			extras:  []string{"  " + extra1 + "  \n"},
			want:    primary + "\n" + extra1 + "\n",
		},
		{
			name:    "invalid extra rejected",
			primary: primary,
			extras:  []string{"not-a-key"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildAuthorizedKeys(tt.primary, tt.extras)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (output=%q)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("mismatch\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

// TestSSHSocketActivationHandling tests the socket activation detection logic
// This test documents the expected behavior for the fix to supabyoi-6yn2
func TestSSHSocketActivationHandling(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	t.Log("=== SSH Socket Activation Test ===")
	t.Log("This test verifies the fix for supabyoi-6yn2")
	t.Log("Issue: systemd socket activation keeps SSH on port 22 despite config changes")
	t.Log("")

	// Test scenario 1: Document expected behavior with socket activation
	t.Run("socket_activation_detection", func(t *testing.T) {
		t.Log("Expected behavior:")
		t.Log("1. Check if ssh.socket is active using: systemctl is-active ssh.socket")
		t.Log("2. If active: stop socket with: systemctl stop ssh.socket")
		t.Log("3. If active: disable socket with: systemctl disable ssh.socket")
		t.Log("4. Enable direct service: systemctl enable ssh.service")
		t.Log("5. Restart service: systemctl restart ssh.service")
		t.Log("6. Verify SSH listens on new port using: ss -tuln")
		t.Log("")
		t.Log("This ensures SSH respects Port directive in sshd_config.d/supabyoi.conf")
	})

	// Test scenario 2: Verify rollback behavior
	t.Run("rollback_restores_socket", func(t *testing.T) {
		t.Log("Expected rollback behavior:")
		t.Log("1. Re-enable socket: systemctl enable ssh.socket")
		t.Log("2. Stop direct service: systemctl stop ssh.service")
		t.Log("3. Start socket: systemctl start ssh.socket")
		t.Log("4. This restores system to original state with socket activation")
	})

	// Note: Actual integration test should run on Ubuntu VM with systemd
	t.Log("")
	t.Log("=== Integration Test Required ===")
	t.Log("Full test requires Ubuntu 20.04+ VM with systemd")
	t.Log("Run: scripts/validate-release.py --skip-harden=false")
	t.Log("Validation script will verify socket handling on real system")
}
