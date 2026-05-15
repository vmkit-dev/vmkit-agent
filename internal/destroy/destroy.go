package destroy

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vmkit-dev/vmkit-agent/internal/compose"
	"github.com/vmkit-dev/vmkit-agent/internal/nginx"
	"github.com/vmkit-dev/vmkit-agent/internal/openport"
	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

const (
	// DefaultBaseDir is the default directory for instances
	DefaultBaseDir = "/opt/supabase"
)

// Destroy removes a Supabase instance and cleans up all resources
// This function is idempotent - safe to call multiple times
func Destroy(cfg *types.DestroyConfig) types.DestroyResult {
	result := types.DestroyResult{
		Success:    true,
		InstanceID: cfg.InstanceID,
		Steps:      []types.StepResult{},
	}

	// Set default base dir if not provided
	baseDir := cfg.BaseDir
	if baseDir == "" {
		baseDir = DefaultBaseDir
	}

	instanceDir := filepath.Join(baseDir, cfg.Subdomain)

	// Construct domain names for TLS cert cleanup
	domain := cfg.Subdomain + ".supabyoi.com"
	studioDomain := "studio-" + cfg.Subdomain + ".supabyoi.com"

	// Define destruction steps
	steps := []destructionStep{
		{
			name:     "stop_containers",
			function: func() error { return stopContainers(cfg.Subdomain, baseDir) },
		},
		{
			name:     "remove_volumes",
			function: func() error { return removeVolumes(cfg.Subdomain, baseDir) },
		},
		{
			name:     "remove_nginx",
			function: func() error { return removeNginxConfig(cfg.Subdomain) },
		},
		{
			name:     "remove_tls_certs",
			function: func() error { return removeTLSCerts(domain, studioDomain) },
		},
		{
			name:     "cleanup_directory",
			function: func() error { return cleanupDirectory(instanceDir) },
		},
		{
			name:     "cleanup_orphans",
			function: func() error { return cleanupOrphans(cfg.Subdomain) },
		},
		{
			name:     "close_firewall_port",
			function: func() error { return closeFirewallPort(cfg.PostgresPort) },
		},
	}

	// Execute each step
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

	return result
}

// destructionStep represents a single destruction operation
type destructionStep struct {
	name     string
	function func() error
}

// executeStep runs a single destruction step and returns the result
func executeStep(step destructionStep) types.StepResult {
	startTime := time.Now()

	result := types.StepResult{
		Name:      step.name,
		Status:    "in_progress",
		StartTime: startTime.Format(time.RFC3339),
	}

	// Execute the step function
	err := step.function()

	endTime := time.Now()
	result.EndTime = endTime.Format(time.RFC3339)
	result.Duration = endTime.Sub(startTime).String()

	if err != nil {
		result.Status = "failed"
		result.Message = fmt.Sprintf("Failed: %v", err)
		return result
	}

	result.Status = "completed"
	result.Message = "Successfully completed"

	return result
}

// stopContainers stops all Docker containers for the instance
func stopContainers(subdomain, baseDir string) error {
	c := compose.New(subdomain, baseDir)
	ctx := context.Background()

	// Use Stop instead of Down to keep containers for volume removal
	if err := c.Stop(ctx); err != nil {
		// If directory doesn't exist or no containers, that's fine (idempotent)
		if isIgnorableError(err) {
			return nil
		}
		return fmt.Errorf("failed to stop containers: %w", err)
	}

	return nil
}

// removeVolumes removes Docker volumes using compose down -v
func removeVolumes(subdomain, baseDir string) error {
	instanceDir := filepath.Join(baseDir, subdomain)

	// Run docker compose down -v --remove-orphans to remove containers and volumes
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "sudo", "docker", "compose", "down", "-v", "--remove-orphans")
	cmd.Dir = instanceDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		// If compose file doesn't exist, that's fine (already cleaned up)
		if isIgnorableError(err) {
			return nil
		}
		return fmt.Errorf("failed to remove volumes: %w. Output: %s", err, string(output))
	}

	return nil
}

// removeNginxConfig removes the Nginx site configuration
func removeNginxConfig(subdomain string) error {
	if err := nginx.RemoveSiteConfig(subdomain); err != nil {
		// If config doesn't exist, that's fine (idempotent)
		if isIgnorableError(err) {
			return nil
		}
		return fmt.Errorf("failed to remove nginx config: %w", err)
	}

	return nil
}

// removeTLSCerts removes TLS certificates for the instance domains
func removeTLSCerts(domain, studioDomain string) error {
	certDirs := []string{
		filepath.Join("/etc/letsencrypt/live", domain),
		filepath.Join("/etc/letsencrypt/live", studioDomain),
	}

	for _, dir := range certDirs {
		cmd := exec.Command("sudo", "rm", "-rf", dir)
		output, err := cmd.CombinedOutput()
		if err != nil {
			if isIgnorableError(err) {
				continue
			}
			return fmt.Errorf("failed to remove TLS certs at %s: %w. Output: %s", dir, err, string(output))
		}
	}

	return nil
}

// cleanupDirectory removes the instance directory
func cleanupDirectory(instanceDir string) error {
	cmd := exec.Command("sudo", "rm", "-rf", instanceDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If directory doesn't exist, that's fine (idempotent)
		if isIgnorableError(err) {
			return nil
		}
		return fmt.Errorf("failed to remove directory: %w. Output: %s", err, string(output))
	}

	return nil
}

// closeFirewallPort removes every UFW ALLOW rule targeting the given TCP
// port, across all sources. Called on destroy so a shared VM doesn't
// accumulate stale rules for deleted instances' postgres listeners. Zero
// port is a signal from a pre-0.12.2 caller that it doesn't know the port —
// we skip rather than guess.
//
// Rules are deleted in reverse rule-number order so that UFW's renumbering
// doesn't shift entries out from under us.
func closeFirewallPort(port int) error {
	if port == 0 {
		return nil
	}
	ctx := context.Background()

	out, err := exec.CommandContext(ctx, "sudo", "ufw", "status", "numbered").CombinedOutput()
	if err != nil {
		// Missing UFW on this host is not fatal — treat like any other
		// "resource doesn't exist" teardown skip.
		if isIgnorableError(err) {
			return nil
		}
		return fmt.Errorf("ufw status failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	existing := openport.ParseUFWRulesForPort(string(out), port, "tcp")
	if len(existing) == 0 {
		return nil
	}

	// Sort rule numbers descending so deletes don't renumber later entries
	// out from under us. The parser returns them ascending.
	nums := make([]int, 0, len(existing))
	for _, r := range existing {
		nums = append(nums, r.RuleNumber)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(nums)))

	for _, n := range nums {
		rmOut, rmErr := exec.CommandContext(
			ctx, "sudo", "ufw", "--force", "delete", strconv.Itoa(n),
		).CombinedOutput()
		if rmErr != nil {
			return fmt.Errorf(
				"ufw delete %d (port %d/tcp) failed: %w (%s)",
				n, port, rmErr, strings.TrimSpace(string(rmOut)),
			)
		}
	}
	return nil
}

// cleanupOrphans cleans up any orphaned containers or networks
func cleanupOrphans(subdomain string) error {
	// Remove any containers with the subdomain in the name
	cmd := exec.Command("sudo", "docker", "ps", "-a", "--filter", fmt.Sprintf("name=%s", subdomain), "-q")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If docker command fails, that's okay - best effort cleanup
		return nil
	}

	containerIDs := string(output)
	if containerIDs != "" {
		rmCmd := exec.Command("sudo", "docker", "rm", "-f", containerIDs)
		_ = rmCmd.Run() // Best effort - ignore errors
	}

	// Prune orphaned networks (best effort)
	pruneCmd := exec.Command("sudo", "docker", "network", "prune", "-f")
	_ = pruneCmd.Run() // Best effort - ignore errors

	return nil
}

// isIgnorableError determines if an error can be safely ignored (idempotency)
func isIgnorableError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	// Common ignorable errors that indicate resource doesn't exist
	ignorablePatterns := []string{
		"no such file or directory",
		"does not exist",
		"not found",
		"no configuration file",
		"cannot find",
	}

	for _, pattern := range ignorablePatterns {
		if contains(errMsg, pattern) {
			return true
		}
	}

	return false
}

// contains checks if a string contains a substring
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
