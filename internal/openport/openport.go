package openport

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

// Executor abstracts shelling out to UFW so the reconcile logic can be
// unit-tested without touching a real firewall.
type Executor interface {
	Run(name string, args ...string) ([]byte, error)
}

type defaultExecutor struct{}

func (defaultExecutor) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func normalizeProtocol(p string) string {
	if p == "" {
		return "tcp"
	}
	return p
}

// OpenPort opens a firewall port using UFW. When cfg.SourceCIDRs is non-empty
// the agent reconciles per-source rules; otherwise it writes a single blanket
// ALLOW rule (pre-0.12.0 behavior).
func OpenPort(cfg *types.OpenPortConfig) *types.OpenPortResult {
	protocol := normalizeProtocol(cfg.Protocol)
	result := &types.OpenPortResult{
		Port:     cfg.Port,
		Protocol: protocol,
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		result.ErrorMessage = fmt.Sprintf("invalid port number: %d", cfg.Port)
		return result
	}
	if err := ensureUFWInstalled(defaultExecutor{}); err != nil {
		result.ErrorMessage = err.Error()
		return result
	}
	allowed, err := reconcile(defaultExecutor{}, cfg.Port, protocol, cfg.SourceCIDRs)
	if err != nil {
		result.ErrorMessage = err.Error()
		return result
	}
	result.AllowedCIDRs = allowed
	result.Success = true
	return result
}

// UpdateAllowlist reconciles UFW rules for a previously-opened port to match
// the supplied source CIDRs. This is the post-deploy hook the backend calls
// when the user edits the allowlist from the dashboard.
func UpdateAllowlist(cfg *types.UpdateAllowlistConfig) *types.UpdateAllowlistResult {
	protocol := normalizeProtocol(cfg.Protocol)
	result := &types.UpdateAllowlistResult{
		Port:     cfg.Port,
		Protocol: protocol,
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		result.ErrorMessage = fmt.Sprintf("invalid port number: %d", cfg.Port)
		return result
	}
	if err := ensureUFWInstalled(defaultExecutor{}); err != nil {
		result.ErrorMessage = err.Error()
		return result
	}

	before, err := listRulesForPort(defaultExecutor{}, cfg.Port, protocol)
	if err != nil {
		result.ErrorMessage = err.Error()
		return result
	}
	toDelete, toAdd := DiffAllowlist(before, cfg.SourceCIDRs)
	beforeByNum := map[int]string{}
	for _, r := range before {
		beforeByNum[r.RuleNumber] = r.Source
	}
	for _, n := range toDelete {
		if src, ok := beforeByNum[n]; ok {
			result.RemovedCIDRs = append(result.RemovedCIDRs, src)
		}
	}
	result.AddedCIDRs = append(result.AddedCIDRs, toAdd...)

	allowed, err := reconcile(defaultExecutor{}, cfg.Port, protocol, cfg.SourceCIDRs)
	if err != nil {
		result.ErrorMessage = err.Error()
		return result
	}
	result.AllowedCIDRs = allowed
	result.Success = true
	return result
}

func ensureUFWInstalled(exe Executor) error {
	if _, err := exe.Run("which", "ufw"); err != nil {
		return fmt.Errorf("UFW is not installed")
	}
	return nil
}

func listRulesForPort(exe Executor, port int, protocol string) ([]ExistingRule, error) {
	out, err := exe.Run("ufw", "status", "numbered")
	if err != nil {
		return nil, fmt.Errorf("failed to read UFW status: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return ParseUFWRulesForPort(string(out), port, protocol), nil
}

// reconcile deletes obsolete rules and adds the missing ones so that UFW ends
// up with exactly `desired` sources for this port. Returns the desired set as
// written (so the caller can echo it back to the backend).
func reconcile(exe Executor, port int, protocol string, desired []string) ([]string, error) {
	existing, err := listRulesForPort(exe, port, protocol)
	if err != nil {
		return nil, err
	}
	toDelete, toAdd := DiffAllowlist(existing, desired)

	for _, ruleNum := range toDelete {
		out, err := exe.Run("ufw", "--force", "delete", strconv.Itoa(ruleNum))
		if err != nil {
			return nil, fmt.Errorf(
				"failed to delete UFW rule %d: %v (%s)",
				ruleNum, err, strings.TrimSpace(string(out)),
			)
		}
	}
	for _, cidr := range toAdd {
		args := FormatAddRule(port, protocol, cidr)
		out, err := exe.Run("ufw", args...)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to allow %s on %d/%s: %v (%s)",
				cidr, port, protocol, err, strings.TrimSpace(string(out)),
			)
		}
	}

	// Summarize what's in place now — re-read to avoid drift from concurrent
	// edits on the VM.
	after, err := listRulesForPort(exe, port, protocol)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var allowed []string
	for _, r := range after {
		if _, ok := seen[r.Source]; ok {
			continue
		}
		seen[r.Source] = struct{}{}
		allowed = append(allowed, r.Source)
	}
	return allowed, nil
}
