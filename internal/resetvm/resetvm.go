// Package resetvm restores a shared VM to its just-hardened baseline so the
// e2e fleet can reuse the same host across runs without paying the ~8-min
// harden cost on every iteration. It does NOT re-harden — the agent binary,
// hardened user, SSH settings, and cron are left alone. See supabyoi-88d9.
package resetvm

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vmkit-dev/vmkit-agent/internal/destroy"
	"github.com/vmkit-dev/vmkit-agent/internal/openport"
	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

const (
	// DefaultBaseDir is the default directory housing deployed instances.
	DefaultBaseDir = "/opt/supabase"
	// ConfigDir is where the backend drops ephemeral JSON configs between
	// agent invocations. Cleaned on reset.
	ConfigDir = "/tmp"
	// BackupEnvDir is where harden writes backup cron env files.
	BackupEnvDir = "/etc/supabyoi"
)

// DefaultAllowedPorts is the UFW baseline when the caller passes no
// allowed_ports list. Matches a plain (non-hardened) SSH+HTTP/S baseline.
// Callers should pass an explicit list that includes the live SSH port if
// harden moved it off :22 — otherwise the operator risks locking themselves out.
var DefaultAllowedPorts = []int{22, 80, 443}

// Reset runs every reset step, aggregating results. Unlike Destroy it does
// NOT bail on the first failing step — a half-broken instance should not
// prevent the rest of the VM from being cleaned.
func Reset(cfg *types.ResetVMConfig) types.ResetVMResult {
	baseDir := cfg.BaseDir
	if baseDir == "" {
		baseDir = DefaultBaseDir
	}
	allowed := cfg.AllowedPorts
	if len(allowed) == 0 {
		allowed = append([]int(nil), DefaultAllowedPorts...)
	}

	result := types.ResetVMResult{Success: true}

	// 1. Tear down every /opt/supabase/<subdomain>/ via destroy.Destroy.
	subs, listErr := listInstanceSubdirs(baseDir)
	step := runStep("purge_instances", func() error {
		if listErr != nil {
			return listErr
		}
		var failed []string
		for _, sub := range subs {
			dr := destroy.Destroy(&types.DestroyConfig{
				InstanceID:   sub, // synthetic — destroy only echoes it in its response
				Subdomain:    sub,
				BaseDir:      baseDir,
				PostgresPort: 0, // UFW is handled by the reset_ufw step below
			})
			if !dr.Success {
				failed = append(failed, fmt.Sprintf("%s: %s", sub, dr.ErrorMessage))
			}
		}
		if len(failed) > 0 {
			return fmt.Errorf("destroy failed for: %s", strings.Join(failed, "; "))
		}
		return nil
	})
	if len(subs) == 0 {
		step.Message = "no instance dirs found"
	} else if step.Status == "completed" {
		step.Message = fmt.Sprintf("purged %d instance(s)", len(subs))
	}
	result.Steps = append(result.Steps, step)
	result.PurgedInstances = subs
	recordFailure(&result, step)

	// 2. Prune orphan docker networks and volumes.
	step = runStep("prune_docker", pruneDocker)
	result.Steps = append(result.Steps, step)
	recordFailure(&result, step)

	// 3. Reset UFW back to allowed_ports baseline.
	var removedRules []int
	step = runStep("reset_ufw", func() error {
		nums, err := resetUFW(allowed)
		if err != nil {
			return err
		}
		removedRules = nums
		return nil
	})
	if step.Status == "completed" {
		step.Message = fmt.Sprintf("deleted %d rule(s)", len(removedRules))
	}
	result.Steps = append(result.Steps, step)
	result.RemovedUFWRules = removedRules
	recordFailure(&result, step)

	// 4. Scrub stray config files from /tmp and /etc/supabyoi.
	var removedFiles []string
	step = runStep("cleanup_configs", func() error {
		removed, err := cleanupStaleConfigs()
		if err != nil {
			return err
		}
		removedFiles = removed
		return nil
	})
	if step.Status == "completed" {
		step.Message = fmt.Sprintf("removed %d file(s)", len(removedFiles))
	}
	result.Steps = append(result.Steps, step)
	result.RemovedConfigFiles = removedFiles
	recordFailure(&result, step)

	return result
}

func recordFailure(r *types.ResetVMResult, step types.StepResult) {
	if step.Status != "failed" {
		return
	}
	r.Success = false
	if r.FailedStep == "" {
		r.FailedStep = step.Name
		r.ErrorMessage = step.Message
	}
}

// runStep wraps a function as a StepResult with timing + status.
func runStep(name string, fn func() error) types.StepResult {
	start := time.Now()
	err := fn()
	end := time.Now()
	sr := types.StepResult{
		Name:      name,
		StartTime: start.Format(time.RFC3339),
		EndTime:   end.Format(time.RFC3339),
		Duration:  end.Sub(start).String(),
	}
	if err != nil {
		sr.Status = "failed"
		sr.Message = fmt.Sprintf("Failed: %v", err)
		return sr
	}
	sr.Status = "completed"
	sr.Message = "Successfully completed"
	return sr
}

// listInstanceSubdirs returns subdomains found as direct subdirectories of
// BaseDir. Missing BaseDir is treated as "nothing to do".
func listInstanceSubdirs(baseDir string) ([]string, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read base dir %s: %w", baseDir, err)
	}
	var subs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		subs = append(subs, name)
	}
	sort.Strings(subs)
	return subs, nil
}

// pruneDocker runs `docker network prune` and `docker volume prune` (both -f).
// Missing Docker is not fatal — reset-vm is a best-effort sweep.
func pruneDocker() error {
	ctx := context.Background()
	for _, target := range []string{"network", "volume"} {
		out, err := exec.CommandContext(
			ctx, "sudo", "docker", target, "prune", "-f",
		).CombinedOutput()
		if err != nil {
			if strings.Contains(string(out), "command not found") ||
				strings.Contains(err.Error(), "executable file not found") {
				continue
			}
			return fmt.Errorf("docker %s prune: %w (%s)", target, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// resetUFW removes every UFW ALLOW rule whose port is not in allowedPorts.
// Rules are deleted in descending rule-number order to avoid UFW renumbering
// the list out from under us. Returns the (pre-delete) rule numbers removed.
func resetUFW(allowedPorts []int) ([]int, error) {
	ctx := context.Background()
	out, err := exec.CommandContext(ctx, "sudo", "ufw", "status", "numbered").CombinedOutput()
	if err != nil {
		if strings.Contains(err.Error(), "executable file not found") {
			return nil, nil
		}
		return nil, fmt.Errorf("ufw status: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	rules := openport.ParseAllUFWAllowRules(string(out))
	toDelete := PlanUFWReset(rules, allowedPorts)
	if len(toDelete) == 0 {
		return nil, nil
	}

	for _, n := range toDelete {
		rmOut, rmErr := exec.CommandContext(
			ctx, "sudo", "ufw", "--force", "delete", strconv.Itoa(n),
		).CombinedOutput()
		if rmErr != nil {
			return nil, fmt.Errorf(
				"ufw delete %d: %w (%s)",
				n, rmErr, strings.TrimSpace(string(rmOut)),
			)
		}
	}
	return toDelete, nil
}

// PlanUFWReset returns the rule numbers to delete so that only rules for
// allowedPorts remain. Sorted descending so the caller can `ufw delete` in
// order without renumbering shifting the remaining ones. Exported for tests.
func PlanUFWReset(rules []openport.ExistingRule, allowedPorts []int) []int {
	allowed := make(map[int]struct{}, len(allowedPorts))
	for _, p := range allowedPorts {
		allowed[p] = struct{}{}
	}
	var toDelete []int
	for _, r := range rules {
		if _, ok := allowed[r.Port]; ok {
			continue
		}
		toDelete = append(toDelete, r.RuleNumber)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(toDelete)))
	return toDelete
}

// cleanupStaleConfigs removes ephemeral JSON configs the backend drops in
// /tmp between agent invocations, plus any /etc/supabyoi/backup-*.env env
// files harden left behind. Missing dirs are fine. Returns the sorted list
// of paths actually removed.
func cleanupStaleConfigs() ([]string, error) {
	patterns := []string{
		filepath.Join(ConfigDir, "supabyoi-*.json"),
		// Backend uploads configs with command-specific names too — sweep them.
		filepath.Join(ConfigDir, "deploy-*.json"),
		filepath.Join(ConfigDir, "destroy-*.json"),
		filepath.Join(ConfigDir, "backup-*.json"),
		filepath.Join(ConfigDir, "restore-*.json"),
		filepath.Join(ConfigDir, "harden-*.json"),
		filepath.Join(ConfigDir, "cleanup-*.json"),
		filepath.Join(ConfigDir, "verify-*.json"),
		filepath.Join(ConfigDir, "upgrade-*.json"),
		filepath.Join(ConfigDir, "rotate-*.json"),
		filepath.Join(ConfigDir, "open-port-*.json"),
		filepath.Join(ConfigDir, "update-allowlist-*.json"),
		filepath.Join(ConfigDir, "reset-vm-*.json"),
		filepath.Join(BackupEnvDir, "backup-*.env"),
	}
	var removed []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("glob %s: %w", pattern, err)
		}
		for _, path := range matches {
			// /etc/supabyoi is root-owned and /tmp entries are agent-created
			// (root) so sudo is needed regardless.
			cmd := exec.Command("sudo", "rm", "-f", path)
			if out, err := cmd.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("rm %s: %w (%s)", path, err, strings.TrimSpace(string(out)))
			}
			removed = append(removed, path)
		}
	}
	sort.Strings(removed)
	return removed, nil
}
