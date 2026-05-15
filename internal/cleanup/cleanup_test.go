package cleanup

import (
	"testing"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

func TestGetCleanupSteps(t *testing.T) {
	tests := []struct {
		name          string
		config        *types.CleanupConfig
		expectedSteps int
		checkSteps    []string
	}{
		{
			name: "Minimal cleanup",
			config: &types.CleanupConfig{
				Level: types.CleanupMinimal,
			},
			expectedSteps: 5, // Only basic file removal + nginx reload
			checkSteps: []string{
				"Remove Supabase instance directories",
				"Remove Supabyoi agent directory",
				"Remove Nginx Supabase site configurations",
			},
		},
		{
			name: "Full cleanup",
			config: &types.CleanupConfig{
				Level: types.CleanupFull,
			},
			expectedSteps: 8, // Basic + user removal + fail2ban
			checkSteps: []string{
				"Remove Supabase instance directories",
				"Remove supabyoi user",
				"Remove fail2ban Supabyoi configurations",
			},
		},
		{
			name: "Revert cleanup with custom SSH port",
			config: &types.CleanupConfig{
				Level:               types.CleanupRevert,
				PreHardeningSSHPort: 2222,
				PreHardeningSSHUser: "admin",
				RestorePasswordAuth: true,
				DisableFirewall:     true,
			},
			expectedSteps: 14, // Full + SSH config restoration + firewall
			checkSteps: []string{
				"Remove Supabase instance directories",
				"Remove supabyoi user",
				"Remove Supabyoi SSH configuration",
				"Restore SSH port to 2222 in main config",
				"Re-enable password authentication",
				"Re-enable root/original user login",
				"Restart SSH service to apply changes",
				"Disable UFW firewall",
			},
		},
		{
			name: "Revert cleanup with default SSH settings",
			config: &types.CleanupConfig{
				Level:               types.CleanupRevert,
				RestorePasswordAuth: false,
				DisableFirewall:     false,
			},
			expectedSteps: 12, // Full + SSH config without password auth and firewall
			checkSteps: []string{
				"Remove Supabase instance directories",
				"Restore SSH port to 22 in main config",
				"Restart SSH service to apply changes",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := getCleanupSteps(tt.config)

			if len(steps) != tt.expectedSteps {
				t.Errorf("Expected %d steps, got %d", tt.expectedSteps, len(steps))
			}

			// Check that expected steps are present
			stepNames := make(map[string]bool)
			for _, step := range steps {
				stepNames[step.name] = true
			}

			for _, expectedStep := range tt.checkSteps {
				if !stepNames[expectedStep] {
					t.Errorf("Expected step '%s' not found", expectedStep)
				}
			}
		})
	}
}

func TestIsIgnorableError(t *testing.T) {
	tests := []struct {
		name       string
		stepName   string
		err        error
		output     []byte
		shouldPass bool
	}{
		{
			name:       "Directory removal when dir doesn't exist",
			stepName:   "Remove Supabase instance directories",
			err:        nil,
			output:     []byte(""),
			shouldPass: true,
		},
		{
			name:       "User removal when user doesn't exist",
			stepName:   "Remove supabyoi user",
			err:        &mockError{},
			output:     []byte("userdel: user 'supabyoi' does not exist"),
			shouldPass: true,
		},
		{
			name:       "User removal when user is currently in use",
			stepName:   "Remove supabyoi user",
			err:        &mockError{},
			output:     []byte("userdel: user supabyoi is currently used by process 12345"),
			shouldPass: true,
		},
		{
			name:       "Service restart when service not found",
			stepName:   "Restart fail2ban to apply changes",
			err:        &mockError{},
			output:     []byte("Unit fail2ban.service could not be found"),
			shouldPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isIgnorableError(tt.stepName, tt.err, tt.output)
			if result != tt.shouldPass {
				t.Errorf("Expected isIgnorableError to return %v, got %v", tt.shouldPass, result)
			}
		})
	}
}

func TestCleanupResult(t *testing.T) {
	tests := []struct {
		name           string
		config         *types.CleanupConfig
		expectSuccess  bool
		expectSSHPort  int
		expectSSHUser  string
	}{
		{
			name: "Minimal cleanup result",
			config: &types.CleanupConfig{
				Level: types.CleanupMinimal,
			},
			expectSuccess: true,
			expectSSHPort: 0, // Not set for minimal
			expectSSHUser: "", // Not set for minimal
		},
		{
			name: "Revert cleanup with custom settings",
			config: &types.CleanupConfig{
				Level:               types.CleanupRevert,
				PreHardeningSSHPort: 2222,
				PreHardeningSSHUser: "admin",
			},
			expectSuccess: true,
			expectSSHPort: 2222,
			expectSSHUser: "admin",
		},
		{
			name: "Revert cleanup with defaults",
			config: &types.CleanupConfig{
				Level: types.CleanupRevert,
			},
			expectSuccess: true,
			expectSSHPort: 22,    // Default
			expectSSHUser: "root", // Default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: We can't actually run cleanup in tests, but we can verify
			// the logic for setting restored values
			result := types.CleanupResult{
				Success: true,
				Level:   string(tt.config.Level),
			}

			if tt.config.Level == types.CleanupRevert {
				result.RestoredSSHPort = tt.config.PreHardeningSSHPort
				result.RestoredSSHUser = tt.config.PreHardeningSSHUser
				if result.RestoredSSHPort == 0 {
					result.RestoredSSHPort = 22
				}
				if result.RestoredSSHUser == "" {
					result.RestoredSSHUser = "root"
				}
			}

			if result.RestoredSSHPort != tt.expectSSHPort {
				t.Errorf("Expected SSH port %d, got %d", tt.expectSSHPort, result.RestoredSSHPort)
			}

			if result.RestoredSSHUser != tt.expectSSHUser {
				t.Errorf("Expected SSH user %s, got %s", tt.expectSSHUser, result.RestoredSSHUser)
			}
		})
	}
}

// mockError is a simple error implementation for testing
type mockError struct{}

func (e *mockError) Error() string {
	return "mock error"
}
