package harden

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

const (
	backupDir          = "/var/backups/supabyoi"
	sshdConfigDir      = "/etc/ssh/sshd_config.d"
	sshdConfigFile     = "/etc/ssh/sshd_config.d/supabyoi.conf"
	rollbackScriptPath = "/usr/local/bin/supabyoi-rollback-hardening.sh"
)

// runCmd executes a command with a hard timeout. Returns error if the command
// fails or the timeout elapses (context.DeadlineExceeded wraps the error).
func runCmd(timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}

// runCmdOutput executes a command with a hard timeout and captures stdout.
func runCmdOutput(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

// Harden executes VM hardening with comprehensive safety checks and rollback
func Harden(cfg *types.HardenConfig) types.HardenResult {
	result := types.HardenResult{
		Success: false,
		Steps:   []types.StepResult{},
	}

	// Step 1: Pre-flight checks
	step := startStep("Pre-flight Safety Checks")
	if err := preflightChecks(cfg); err != nil {
		step.Status = "failed"
		step.Message = err.Error()
		result.Steps = append(result.Steps, finishStep(step))
		result.ErrorMessage = err.Error()
		result.FailedStep = step.Name
		return result
	}
	step.Status = "completed"
	step.Message = "All safety checks passed"
	result.Steps = append(result.Steps, finishStep(step))

	// Step 2: Create backups
	step = startStep("Create Configuration Backups")
	if err := createBackups(); err != nil {
		step.Status = "failed"
		step.Message = err.Error()
		result.Steps = append(result.Steps, finishStep(step))
		result.ErrorMessage = err.Error()
		result.FailedStep = step.Name
		return result
	}
	step.Status = "completed"
	step.Message = "Configuration files backed up successfully"
	result.Steps = append(result.Steps, finishStep(step))

	// Step 3: User management
	step = startStep("Configure System User")
	if err := configureUser(cfg); err != nil {
		step.Status = "failed"
		step.Message = err.Error()
		result.Steps = append(result.Steps, finishStep(step))
		result.ErrorMessage = err.Error()
		result.FailedStep = step.Name
		return result
	}
	step.Status = "completed"
	step.Message = fmt.Sprintf("User %s configured with sudo access", cfg.VMUser)
	result.Steps = append(result.Steps, finishStep(step))

	// Step 4: SSH configuration
	step = startStep("Configure SSH Security")
	if err := configureSSH(cfg); err != nil {
		step.Status = "failed"
		step.Message = err.Error()
		result.Steps = append(result.Steps, finishStep(step))
		result.ErrorMessage = err.Error()
		result.FailedStep = step.Name
		// Rollback on SSH configuration failure
		rollback(&result)
		return result
	}
	step.Status = "completed"
	step.Message = fmt.Sprintf("SSH configured on port %d with key-based auth only", cfg.SSHPort)
	result.Steps = append(result.Steps, finishStep(step))

	// Step 5: Restart SSH and test connection
	step = startStep("Restart SSH and Validate Connection")
	if err := restartAndTestSSH(cfg); err != nil {
		step.Status = "failed"
		step.Message = err.Error()
		result.Steps = append(result.Steps, finishStep(step))
		result.ErrorMessage = err.Error()
		result.FailedStep = step.Name
		// Critical: rollback on SSH connection test failure
		rollback(&result)
		return result
	}
	step.Status = "completed"
	step.Message = "SSH service restarted and connection verified"
	result.Steps = append(result.Steps, finishStep(step))

	// Step 6: Firewall configuration (if enabled)
	if cfg.EnableFirewall {
		step = startStep("Configure UFW Firewall")
		if err := configureFirewall(cfg); err != nil {
			step.Status = "failed"
			step.Message = err.Error()
			result.Steps = append(result.Steps, finishStep(step))
			result.ErrorMessage = err.Error()
			result.FailedStep = step.Name
			// Rollback on firewall failure
			rollback(&result)
			return result
		}
		step.Status = "completed"
		step.Message = "UFW firewall configured and enabled"
		result.Steps = append(result.Steps, finishStep(step))
	}

	// Step 7: Install fail2ban (if enabled)
	if cfg.EnableFail2Ban {
		step = startStep("Install and Configure Fail2Ban")
		if err := installFail2Ban(cfg); err != nil {
			step.Status = "failed"
			step.Message = err.Error()
			result.Steps = append(result.Steps, finishStep(step))
			result.ErrorMessage = err.Error()
			result.FailedStep = step.Name
			return result
		}
		step.Status = "completed"
		step.Message = "Fail2Ban installed and configured"
		result.Steps = append(result.Steps, finishStep(step))
	}

	// Step 8: Enable automatic updates (if enabled)
	if cfg.EnableAutoUpdates {
		step = startStep("Enable Automatic Security Updates")
		if err := enableAutoUpdates(); err != nil {
			step.Status = "failed"
			step.Message = err.Error()
			result.Steps = append(result.Steps, finishStep(step))
			result.ErrorMessage = err.Error()
			result.FailedStep = step.Name
			return result
		}
		step.Status = "completed"
		step.Message = "Automatic security updates enabled"
		result.Steps = append(result.Steps, finishStep(step))
	}

	// Step 9: Create emergency rollback script
	step = startStep("Create Emergency Rollback Script")
	if err := createRollbackScript(cfg); err != nil {
		step.Status = "failed"
		step.Message = err.Error()
		result.Steps = append(result.Steps, finishStep(step))
		// Non-critical failure - log but continue
		step.Status = "completed"
		step.Message = "Warning: Could not create rollback script: " + err.Error()
	} else {
		step.Status = "completed"
		step.Message = "Emergency rollback script created at " + rollbackScriptPath
	}
	result.Steps = append(result.Steps, finishStep(step))

	// Success!
	result.Success = true
	return result
}

// buildAuthorizedKeys assembles the authorized_keys file content from the
// primary key and any extras, validating each extra entry's prefix.
// Extras with surrounding whitespace are trimmed; blank entries are skipped.
func buildAuthorizedKeys(primary string, extras []string) (string, error) {
	var b strings.Builder
	b.WriteString(primary)
	b.WriteString("\n")
	for _, extra := range extras {
		trimmed := strings.TrimSpace(extra)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "ssh-") {
			return "", fmt.Errorf("invalid extra SSH public key (must start with 'ssh-'): %q", trimmed)
		}
		b.WriteString(trimmed)
		b.WriteString("\n")
	}
	return b.String(), nil
}

// preflightChecks validates the environment before making changes
func preflightChecks(cfg *types.HardenConfig) error {
	// Check 1: Verify we're running as root or with sudo
	if os.Geteuid() != 0 {
		return fmt.Errorf("hardening must run as root or with sudo")
	}

	// Check 2: Verify SSH public key is provided and valid
	if cfg.SSHPublicKey == "" {
		return fmt.Errorf("SSH public key is required")
	}
	if !strings.HasPrefix(cfg.SSHPublicKey, "ssh-") {
		return fmt.Errorf("invalid SSH public key format")
	}

	// Check 3: Verify SSH port is reasonable (allow well-known ports like 22)
	if cfg.SSHPort < 1 || cfg.SSHPort > 65535 {
		return fmt.Errorf("SSH port must be between 1 and 65535")
	}

	// Check 5: Verify OS compatibility (Ubuntu/Debian)
	if _, err := os.Stat("/etc/debian_version"); err != nil {
		return fmt.Errorf("currently only Ubuntu/Debian systems are supported")
	}

	// Check 6: Verify VM user is set
	if cfg.VMUser == "" {
		cfg.VMUser = "supabyoi" // default
	}

	// Check 7: Ensure backup directory can be created
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return fmt.Errorf("cannot create backup directory: %v", err)
	}

	return nil
}

// createBackups backs up critical configuration files
func createBackups() error {
	timestamp := time.Now().Format("20060102-150405")

	filesToBackup := []string{
		"/etc/ssh/sshd_config",
		"/root/.ssh/authorized_keys",
	}

	for _, file := range filesToBackup {
		if _, err := os.Stat(file); err != nil {
			continue // Skip if file doesn't exist
		}

		backupPath := filepath.Join(backupDir, fmt.Sprintf("%s.%s", filepath.Base(file), timestamp))

		if err := runCmd(10*time.Second, "cp", "-p", file, backupPath); err != nil {
			return fmt.Errorf("failed to backup %s: %v", file, err)
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

func finishStep(step types.StepResult) types.StepResult {
	step.EndTime = time.Now().Format(time.RFC3339)

	start, _ := time.Parse(time.RFC3339, step.StartTime)
	end, _ := time.Parse(time.RFC3339, step.EndTime)
	duration := end.Sub(start)
	step.Duration = fmt.Sprintf("%.2fs", duration.Seconds())

	return step
}

// configureUser creates the VM user and sets up sudo access
func configureUser(cfg *types.HardenConfig) error {
	// Check if user already exists
	userExists := runCmd(5*time.Second, "id", "-u", cfg.VMUser) == nil

	if !userExists {
		if err := runCmd(10*time.Second, "useradd", "-m", "-s", "/bin/bash", cfg.VMUser); err != nil {
			return fmt.Errorf("failed to create user: %v", err)
		}
	}

	// Add user to sudo and docker groups. Cloud-init installs Docker before
	// starting the agent, so the docker group must exist by harden time.
	if err := runCmd(10*time.Second, "usermod", "-aG", "sudo", cfg.VMUser); err != nil {
		return fmt.Errorf("failed to add user to sudo group: %v", err)
	}
	if err := runCmd(10*time.Second, "usermod", "-aG", "docker", cfg.VMUser); err != nil {
		return fmt.Errorf("failed to add user to docker group (is Docker installed?): %v", err)
	}
	// Verify the group membership is visible in /etc/group immediately.
	out, err := runCmdOutput(5*time.Second, "groups", cfg.VMUser)
	if err != nil || !strings.Contains(string(out), "docker") {
		return fmt.Errorf("usermod -aG docker succeeded but %s is not in docker group (groups: %s)", cfg.VMUser, strings.TrimSpace(string(out)))
	}

	// Configure passwordless sudo
	sudoersContent := fmt.Sprintf("%s ALL=(ALL) NOPASSWD:ALL\n", cfg.VMUser)
	sudoersFile := fmt.Sprintf("/etc/sudoers.d/%s", cfg.VMUser)

	if err := os.WriteFile(sudoersFile, []byte(sudoersContent), 0440); err != nil {
		return fmt.Errorf("failed to configure sudo: %v", err)
	}

	// Verify sudoers file syntax
	if err := runCmd(5*time.Second, "visudo", "-c", "-f", sudoersFile); err != nil {
		os.Remove(sudoersFile)
		return fmt.Errorf("invalid sudoers configuration: %v", err)
	}

	// Set up SSH directory for user
	sshDir := fmt.Sprintf("/home/%s/.ssh", cfg.VMUser)
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return fmt.Errorf("failed to create .ssh directory: %v", err)
	}

	// Write authorized_keys: primary key first, then any extras (e.g. the
	// operator's debug key). Without extras, hardening strips every key
	// attached to the cloud-provider server record except the one Supabyoi
	// registered, which locks operators out of their own VM post-harden.
	authorizedKeysFile := filepath.Join(sshDir, "authorized_keys")
	content, err := buildAuthorizedKeys(cfg.SSHPublicKey, cfg.ExtraAuthorizedKeys)
	if err != nil {
		return err
	}
	if err := os.WriteFile(authorizedKeysFile, []byte(content), 0600); err != nil {
		return fmt.Errorf("failed to write authorized_keys: %v", err)
	}

	// Set correct ownership
	if err := runCmd(10*time.Second, "chown", "-R", fmt.Sprintf("%s:%s", cfg.VMUser, cfg.VMUser), sshDir); err != nil {
		return fmt.Errorf("failed to set ownership: %v", err)
	}

	// Test sudo capability
	if err := runCmd(10*time.Second, "sudo", "-u", cfg.VMUser, "sudo", "-n", "true"); err != nil {
		return fmt.Errorf("user cannot sudo without password: %v", err)
	}

	return nil
}

// configureSSH updates SSH configuration for enhanced security
func configureSSH(cfg *types.HardenConfig) error {
	// Ensure sshd_config.d directory exists
	if err := os.MkdirAll(sshdConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create sshd config directory: %v", err)
	}

	// Generate SSH configuration.
	// ChallengeResponseAuthentication was renamed to KbdInteractiveAuthentication
	// in OpenSSH 9.x (Ubuntu 24.04+). Both names are accepted in 8.x, but 9.x
	// treats the old name as an error that causes `sshd -t` to exit 255.
	sshdConfig := fmt.Sprintf(`# VMKit SSH Security Configuration
# Generated on %s

Port %d
PermitRootLogin no
PasswordAuthentication no
PubkeyAuthentication yes
KbdInteractiveAuthentication no
UsePAM yes
`, time.Now().Format(time.RFC3339), cfg.SSHPort)

	// Write configuration file
	if err := os.WriteFile(sshdConfigFile, []byte(sshdConfig), 0644); err != nil {
		return fmt.Errorf("failed to write SSH config: %v", err)
	}

	// Test SSH configuration syntax. sshd -t requires /run/sshd to exist
	// (privilege separation dir) even in test mode; on a fresh VM it doesn't
	// exist until sshd has run once. Create it before the test, then remove
	// it afterwards so we don't leave unexpected state.
	_ = os.MkdirAll("/run/sshd", 0755)
	sshdCtx, sshdCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer sshdCancel()
	sshdCmd := exec.CommandContext(sshdCtx, "/usr/sbin/sshd", "-t")
	if out, err := sshdCmd.CombinedOutput(); err != nil {
		os.Remove(sshdConfigFile)
		return fmt.Errorf("invalid SSH configuration (%v): %s", err, strings.TrimSpace(string(out)))
	}

	return nil
}

// restartAndTestSSH restarts the SSH service and validates connectivity
func restartAndTestSSH(cfg *types.HardenConfig) error {
	// Determine SSH service name by checking which unit file exists.
	// Ubuntu uses "ssh.service"; RHEL/CentOS uses "sshd.service".
	// Using is-active would misdetect Ubuntu 24.04 where only ssh.socket is
	// active (not ssh.service), causing us to fall through to the non-existent
	// sshd.service unit and fail with exit status 1 on systemctl enable.
	serviceName := "ssh"
	if _, err := runCmdOutput(5*time.Second, "systemctl", "cat", "ssh.service"); err != nil {
		serviceName = "sshd"
	}

	// CRITICAL FIX for SSH port change issue (supabyoi-6yn2, supabyoi-mka6)
	// Modern Ubuntu (20.04+) uses systemd socket activation which listens on port 22
	// The socket is hardcoded in /usr/lib/systemd/system/ssh.socket to listen on port 22
	// Even after changing sshd_config Port directive, the socket takes precedence
	// and SSH continues listening on port 22, causing validation to fail
	// Solution: Disable socket activation and run SSH service directly
	socketName := serviceName + ".socket"
	if runCmd(5*time.Second, "systemctl", "is-active", socketName) == nil {
		// Socket is active - must disable it to allow port change

		// Step 1: Stop the socket (30s timeout — systemd can be slow)
		if err := runCmd(30*time.Second, "systemctl", "stop", socketName); err != nil {
			return fmt.Errorf("failed to stop %s (required for port change): %v", socketName, err)
		}

		// Step 2: Disable the socket (prevent auto-restart on boot)
		if err := runCmd(15*time.Second, "systemctl", "disable", socketName); err != nil {
			return fmt.Errorf("failed to disable %s (required for port change): %v", socketName, err)
		}

		// Step 3: Verify socket is actually inactive
		time.Sleep(500 * time.Millisecond) // Brief delay for systemd to update
		if runCmd(5*time.Second, "systemctl", "is-active", socketName) == nil {
			// Socket is still active - this should not happen!
			return fmt.Errorf("%s is still active after stop/disable - systemd may be auto-restarting it", socketName)
		}
	}

	// Ensure SSH service is enabled to start on boot (now that socket is disabled)
	if err := runCmd(15*time.Second, "systemctl", "enable", serviceName); err != nil {
		return fmt.Errorf("failed to enable %s service: %v", serviceName, err)
	}

	// Restart SSH service (30s: systemd may need time to stop old process first)
	if err := runCmd(30*time.Second, "systemctl", "restart", serviceName); err != nil {
		return fmt.Errorf("failed to restart SSH service: %v", err)
	}

	// Wait for SSH to be ready (up to 10s)
	maxAttempts := 10
	for i := 0; i < maxAttempts; i++ {
		if runCmd(5*time.Second, "systemctl", "is-active", serviceName) == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// Verify SSH is listening on the new port
	output, err := runCmdOutput(10*time.Second, "ss", "-tuln")
	if err != nil {
		return fmt.Errorf("failed to check listening ports: %v", err)
	}

	portStr := fmt.Sprintf(":%d", cfg.SSHPort)
	if !strings.Contains(string(output), portStr) {
		return fmt.Errorf("SSH is not listening on port %d", cfg.SSHPort)
	}

	// Test connection using nc (already has a 5s wall-clock limit via timeout(1))
	if err := runCmd(10*time.Second, "timeout", "5", "nc", "-z", "localhost", fmt.Sprintf("%d", cfg.SSHPort)); err != nil {
		return fmt.Errorf("cannot connect to SSH on port %d: %v", cfg.SSHPort, err)
	}

	return nil
}

// configureFirewall sets up UFW firewall with necessary rules
func configureFirewall(cfg *types.HardenConfig) error {
	// Check if UFW is installed
	if runCmd(5*time.Second, "which", "ufw") != nil {
		// Install UFW
		runCmd(60*time.Second, "apt-get", "update") // Best effort

		if err := runCmd(120*time.Second, "apt-get", "install", "-y", "ufw"); err != nil {
			return fmt.Errorf("failed to install UFW: %v", err)
		}
	}

	// CRITICAL: Add SSH port rule BEFORE enabling UFW
	if err := runCmd(15*time.Second, "ufw", "allow", fmt.Sprintf("%d/tcp", cfg.SSHPort)); err != nil {
		return fmt.Errorf("failed to allow SSH port: %v", err)
	}

	// Allow HTTP and HTTPS (best effort)
	runCmd(15*time.Second, "ufw", "allow", "80/tcp")
	runCmd(15*time.Second, "ufw", "allow", "443/tcp")

	// Set default policies
	if err := runCmd(15*time.Second, "ufw", "default", "deny", "incoming"); err != nil {
		return fmt.Errorf("failed to set default deny: %v", err)
	}

	if err := runCmd(15*time.Second, "ufw", "default", "allow", "outgoing"); err != nil {
		return fmt.Errorf("failed to set default allow outgoing: %v", err)
	}

	// Enable UFW (non-interactive)
	if err := runCmd(30*time.Second, "ufw", "--force", "enable"); err != nil {
		return fmt.Errorf("failed to enable UFW: %v", err)
	}

	// Verify UFW is active
	output, err := runCmdOutput(10*time.Second, "ufw", "status")
	if err != nil {
		return fmt.Errorf("failed to check UFW status: %v", err)
	}

	if !strings.Contains(string(output), "Status: active") {
		return fmt.Errorf("UFW is not active after enabling")
	}

	// Verify SSH port is allowed
	if !strings.Contains(string(output), fmt.Sprintf("%d/tcp", cfg.SSHPort)) {
		return fmt.Errorf("SSH port %d is not allowed in UFW", cfg.SSHPort)
	}

	return nil
}

// installFail2Ban installs and configures fail2ban
func installFail2Ban(cfg *types.HardenConfig) error {
	// Check if fail2ban is already installed
	if runCmd(5*time.Second, "which", "fail2ban-client") != nil {
		// Install fail2ban
		runCmd(60*time.Second, "apt-get", "update") // Best effort

		if err := runCmd(120*time.Second, "apt-get", "install", "-y", "fail2ban"); err != nil {
			return fmt.Errorf("failed to install fail2ban: %v", err)
		}
	}

	// Create custom jail configuration for SSH
	jailConfig := fmt.Sprintf(`[sshd]
enabled = true
port = %d
filter = sshd
logpath = /var/log/auth.log
maxretry = 3
bantime = 3600
findtime = 600
`, cfg.SSHPort)

	jailFile := "/etc/fail2ban/jail.d/supabyoi-sshd.conf"
	if err := os.WriteFile(jailFile, []byte(jailConfig), 0644); err != nil {
		return fmt.Errorf("failed to write fail2ban config: %v", err)
	}

	// Restart fail2ban
	if err := runCmd(30*time.Second, "systemctl", "restart", "fail2ban"); err != nil {
		return fmt.Errorf("failed to restart fail2ban: %v", err)
	}

	// Enable fail2ban
	if err := runCmd(15*time.Second, "systemctl", "enable", "fail2ban"); err != nil {
		return fmt.Errorf("failed to enable fail2ban: %v", err)
	}

	return nil
}

// enableAutoUpdates configures automatic security updates
func enableAutoUpdates() error {
	// Install unattended-upgrades if not present
	if runCmd(5*time.Second, "which", "unattended-upgrade") != nil {
		runCmd(60*time.Second, "apt-get", "update") // Best effort

		if err := runCmd(120*time.Second, "apt-get", "install", "-y", "unattended-upgrades"); err != nil {
			return fmt.Errorf("failed to install unattended-upgrades: %v", err)
		}
	}

	// Enable automatic updates
	autoUpgradeConfig := `APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
`
	configFile := "/etc/apt/apt.conf.d/20auto-upgrades"
	if err := os.WriteFile(configFile, []byte(autoUpgradeConfig), 0644); err != nil {
		return fmt.Errorf("failed to write auto-upgrade config: %v", err)
	}

	// Configure to only install security updates
	unattendedConfig := `Unattended-Upgrade::Allowed-Origins {
    "${distro_id}:${distro_codename}-security";
};
Unattended-Upgrade::AutoFixInterruptedDpkg "true";
Unattended-Upgrade::MinimalSteps "true";
`
	configFile = "/etc/apt/apt.conf.d/50unattended-upgrades"

	// Only write if it doesn't exist or append security origins
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		if err := os.WriteFile(configFile, []byte(unattendedConfig), 0644); err != nil {
			return fmt.Errorf("failed to write unattended-upgrades config: %v", err)
		}
	}

	return nil
}

// createRollbackScript creates an emergency rollback script
func createRollbackScript(cfg *types.HardenConfig) error {
	script := fmt.Sprintf(`#!/bin/bash
# Supabyoi VM Hardening Emergency Rollback Script
# This script should be run from console access if you get locked out
# Usage: sudo %s

set -e

echo "=== Supabyoi VM Hardening Rollback ==="
echo "This will restore SSH configuration from backups"
echo ""

BACKUP_DIR="%s"

# Find latest backups
LATEST_SSHD=$(ls -t $BACKUP_DIR/sshd_config.* 2>/dev/null | head -1)

if [ -z "$LATEST_SSHD" ]; then
    echo "ERROR: No backup files found in $BACKUP_DIR"
    exit 1
fi

echo "Found backups:"
echo "  SSH config: $LATEST_SSHD"
echo ""

# Restore sshd_config
echo "Restoring SSH configuration..."
cp -f "$LATEST_SSHD" /etc/ssh/sshd_config

# Remove custom config
if [ -f "%s" ]; then
    echo "Removing custom SSH config..."
    rm -f "%s"
fi

# Temporarily enable root login on port 22
echo "Temporarily enabling root login on port 22..."
sed -i 's/^#*PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config
sed -i 's/^#*Port.*/Port 22/' /etc/ssh/sshd_config

# Restart SSH
echo "Restarting SSH service..."
systemctl restart ssh 2>/dev/null || systemctl restart sshd

# Disable UFW if active
if command -v ufw >/dev/null 2>&1; then
    echo "Disabling UFW firewall..."
    ufw --force disable
fi

echo ""
echo "=== Rollback Complete ==="
echo "You should now be able to connect via:"
echo "  ssh root@<server-ip> -p 22"
echo ""
echo "IMPORTANT: Re-run hardening with correct configuration"
exit 0
`, rollbackScriptPath, backupDir, sshdConfigFile, sshdConfigFile)

	if err := os.WriteFile(rollbackScriptPath, []byte(script), 0755); err != nil {
		return fmt.Errorf("failed to write rollback script: %v", err)
	}

	return nil
}

// rollback attempts to restore the system to its previous state
func rollback(result *types.HardenResult) {
	result.RollbackApplied = true

	var rollbackSteps []string

	// Find the most recent backups
	files, err := os.ReadDir(backupDir)
	if err != nil {
		result.RollbackDetails = fmt.Sprintf("Failed to read backup directory: %v", err)
		return
	}

	// Restore sshd_config
	var latestSSHDConfig string
	for i := len(files) - 1; i >= 0; i-- {
		if strings.HasPrefix(files[i].Name(), "sshd_config.") {
			latestSSHDConfig = filepath.Join(backupDir, files[i].Name())
			break
		}
	}

	if latestSSHDConfig != "" {
		if runCmd(10*time.Second, "cp", "-f", latestSSHDConfig, "/etc/ssh/sshd_config") == nil {
			rollbackSteps = append(rollbackSteps, "Restored /etc/ssh/sshd_config")
		}
	}

	// Remove supabyoi.conf if it exists
	if _, err := os.Stat(sshdConfigFile); err == nil {
		os.Remove(sshdConfigFile)
		rollbackSteps = append(rollbackSteps, "Removed "+sshdConfigFile)
	}

	// Re-enable socket activation if it was disabled
	socketName := "ssh.socket"
	if runCmd(15*time.Second, "systemctl", "enable", socketName) == nil {
		rollbackSteps = append(rollbackSteps, "Re-enabled SSH socket activation")
	}

	// Stop the direct service and let socket take over
	if runCmd(30*time.Second, "systemctl", "stop", "ssh") == nil {
		if runCmd(15*time.Second, "systemctl", "start", socketName) == nil {
			rollbackSteps = append(rollbackSteps, "Restored SSH socket activation")
		}
	} else {
		// Try sshd
		runCmd(30*time.Second, "systemctl", "stop", "sshd")
		runCmd(15*time.Second, "systemctl", "enable", "sshd.socket")
		if runCmd(15*time.Second, "systemctl", "start", "sshd.socket") == nil {
			rollbackSteps = append(rollbackSteps, "Restored SSH socket activation (sshd)")
		}
	}

	// Disable UFW if it was enabled
	if runCmd(15*time.Second, "ufw", "--force", "disable") == nil {
		rollbackSteps = append(rollbackSteps, "Disabled UFW firewall")
	}

	if len(rollbackSteps) > 0 {
		result.RollbackDetails = "Rollback completed: " + strings.Join(rollbackSteps, "; ")
	} else {
		result.RollbackDetails = "Rollback attempted but no steps could be completed"
	}
}
