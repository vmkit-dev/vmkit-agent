package cleanup

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

// Cleanup performs VM cleanup according to the specified level
func Cleanup(cfg *types.CleanupConfig) types.CleanupResult {
	result := types.CleanupResult{
		Success: true,
		Level:   string(cfg.Level),
		Steps:   []types.StepResult{},
	}

	// Determine which steps to run based on cleanup level
	steps := getCleanupSteps(cfg)

	for _, step := range steps {
		stepResult := executeStep(step)
		result.Steps = append(result.Steps, stepResult)

		if stepResult.Status == "failed" {
			result.Success = false
			result.FailedStep = stepResult.Name
			result.ErrorMessage = stepResult.Message
			break
		}
	}

	// Record restored settings for revert level
	if cfg.Level == types.CleanupRevert && result.Success {
		result.RestoredSSHPort = cfg.PreHardeningSSHPort
		result.RestoredSSHUser = cfg.PreHardeningSSHUser
		if result.RestoredSSHPort == 0 {
			result.RestoredSSHPort = 22 // Default SSH port
		}
		if result.RestoredSSHUser == "" {
			result.RestoredSSHUser = "root" // Default user
		}
	}

	return result
}

// cleanupStep represents a single cleanup operation
type cleanupStep struct {
	name    string
	command string
	args    []string
}

// getCleanupSteps returns the list of cleanup steps based on the level
func getCleanupSteps(cfg *types.CleanupConfig) []cleanupStep {
	var steps []cleanupStep

	// All levels: Remove Supabyoi directories and files
	steps = append(steps, []cleanupStep{
		{
			name:    "Remove Supabase instance directories",
			command: "sudo",
			args:    []string{"rm", "-rf", "/opt/supabase"},
		},
		{
			name:    "Remove Supabyoi agent directory",
			command: "sudo",
			args:    []string{"rm", "-rf", "/opt/supabyoi"},
		},
		{
			name:    "Remove Nginx Supabase site configurations",
			command: "sudo",
			args:    []string{"rm", "-rf", "/etc/nginx/supabase-sites"},
		},
		{
			name:    "Remove ACME challenge directory",
			command: "sudo",
			args:    []string{"rm", "-rf", "/var/www/acme-challenge"},
		},
		{
			name:    "Reload Nginx to remove site configurations",
			command: "sudo",
			args:    []string{"systemctl", "reload", "nginx"},
		},
	}...)

	// Full and Revert levels: Remove additional configs (user removal deferred to end)
	if cfg.Level == types.CleanupFull || cfg.Level == types.CleanupRevert {
		steps = append(steps, []cleanupStep{
			{
				name:    "Remove fail2ban Supabyoi configurations",
				command: "sudo",
				args:    []string{"rm", "-f", "/etc/fail2ban/jail.d/supabyoi.conf"},
			},
			{
				name:    "Restart fail2ban to apply changes",
				command: "sudo",
				args:    []string{"systemctl", "restart", "fail2ban"},
			},
		}...)
	}

	// Revert level: Restore original SSH settings
	if cfg.Level == types.CleanupRevert {
		// Restore SSH port
		sshPort := cfg.PreHardeningSSHPort
		if sshPort == 0 {
			sshPort = 22 // Default SSH port
		}

		sshUser := cfg.PreHardeningSSHUser
		if sshUser == "" {
			sshUser = "root" // Default user
		}

		steps = append(steps, []cleanupStep{
			{
				name:    "Remove Supabyoi SSH configuration",
				command: "sudo",
				args:    []string{"rm", "-f", "/etc/ssh/sshd_config.d/supabyoi.conf"},
			},
			{
				name:    fmt.Sprintf("Restore SSH port to %d in main config", sshPort),
				command: "sudo",
				args: []string{"sed", "-i",
					fmt.Sprintf("s/^Port .*/Port %d/", sshPort),
					"/etc/ssh/sshd_config"},
			},
		}...)

		// Restore password authentication if requested
		if cfg.RestorePasswordAuth {
			steps = append(steps, cleanupStep{
				name:    "Re-enable password authentication",
				command: "sudo",
				args: []string{"sed", "-i",
					"s/^PasswordAuthentication no/PasswordAuthentication yes/",
					"/etc/ssh/sshd_config"},
			})
		}

		// Re-enable root login (restore to original user)
		steps = append(steps, cleanupStep{
			name:    "Re-enable root/original user login",
			command: "sudo",
			args: []string{"sed", "-i",
				"s/^PermitRootLogin no/PermitRootLogin yes/",
				"/etc/ssh/sshd_config"},
		})

		// Restart SSH to apply changes
		steps = append(steps, cleanupStep{
			name:    "Restart SSH service to apply changes",
			command: "sudo",
			args:    []string{"systemctl", "restart", "sshd"},
		})

		// Disable firewall if requested
		if cfg.DisableFirewall {
			steps = append(steps, cleanupStep{
				name:    "Disable UFW firewall",
				command: "sudo",
				args:    []string{"ufw", "disable"},
			})
		}
	}

	// Full and Revert levels: Remove supabyoi user as the very last step.
	// This must be last because the SSH session runs as this user.
	// The error is ignorable since userdel fails when the user has active processes.
	if cfg.Level == types.CleanupFull || cfg.Level == types.CleanupRevert {
		steps = append(steps, cleanupStep{
			name:    "Remove supabyoi user",
			command: "sudo",
			args:    []string{"userdel", "-r", "supabyoi"},
		})
	}

	return steps
}

// executeStep runs a single cleanup step and returns the result
func executeStep(step cleanupStep) types.StepResult {
	startTime := time.Now()

	result := types.StepResult{
		Name:      step.name,
		Status:    "in_progress",
		StartTime: startTime.Format(time.RFC3339),
	}

	// Execute command
	cmd := exec.Command(step.command, step.args...)
	output, err := cmd.CombinedOutput()

	endTime := time.Now()
	result.EndTime = endTime.Format(time.RFC3339)
	result.Duration = endTime.Sub(startTime).String()

	if err != nil {
		// Some commands may fail if files don't exist - that's okay
		// Only fail on critical errors
		if !isIgnorableError(step.name, err, output) {
			result.Status = "failed"
			result.Message = fmt.Sprintf("Command failed: %v. Output: %s", err, string(output))
			return result
		}
	}

	result.Status = "completed"
	result.Message = "Successfully completed"

	return result
}

// isIgnorableError determines if an error can be safely ignored
func isIgnorableError(stepName string, err error, output []byte) bool {
	// File/directory removal errors when path doesn't exist
	if stepName == "Remove Supabase instance directories" ||
		stepName == "Remove Supabyoi agent directory" ||
		stepName == "Remove Nginx Supabase site configurations" ||
		stepName == "Remove ACME challenge directory" ||
		stepName == "Remove fail2ban Supabyoi configurations" ||
		stepName == "Remove Supabyoi SSH configuration" {
		// Directory doesn't exist is fine
		return true
	}

	// User deletion when user doesn't exist or is currently in use (SSH session)
	if stepName == "Remove supabyoi user" {
		outputStr := string(output)
		if err != nil && (contains(outputStr, "does not exist") || contains(outputStr, "no such user") ||
			contains(outputStr, "currently used by process") || contains(outputStr, "currently logged in")) {
			return true
		}
	}

	// Service restart when service doesn't exist or isn't running
	if stepName == "Restart fail2ban to apply changes" ||
		stepName == "Reload Nginx to remove site configurations" {
		outputStr := string(output)
		if contains(outputStr, "could not be found") || contains(outputStr, "not loaded") {
			return true
		}
	}

	return false
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
