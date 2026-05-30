package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// containersStopParams is the request payload for the containers.stop RPC.
type containersStopParams struct {
	EnvironmentID string `json:"environment_id"`
}

// containersStopResult is the success response for the containers.stop RPC.
type containersStopResult struct {
	Stopped []string `json:"stopped"`
	Errors  []string `json:"errors,omitempty"`
}

// ContainersStop handles the containers.stop RPC: it stops and removes all
// Docker containers labelled vmkit.environment_id=<id>. Called by the backend
// when an environment is deleted so orphaned containers don't keep running.
//
// vk-rl0u: dispatch is best-effort — the caller (backend delete_environment)
// proceeds with the DB delete regardless of the outcome here.
func ContainersStop(ctx context.Context, params json.RawMessage) (any, error) {
	var p containersStopParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if !envIDRe.MatchString(p.EnvironmentID) {
		return nil, fmt.Errorf("invalid environment_id: %q", p.EnvironmentID)
	}

	// Find all containers with this environment label (running or stopped).
	listOut, err := exec.CommandContext(ctx, "sudo", "docker", "ps", "-a",
		"--filter", "label=vmkit.environment_id="+p.EnvironmentID,
		"--format", "{{.Names}}").Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps failed: %w", err)
	}

	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(listOut)), "\n") {
		name := strings.TrimSpace(line)
		if name != "" {
			names = append(names, name)
		}
	}

	if len(names) == 0 {
		return containersStopResult{Stopped: []string{}}, nil
	}

	var stopped []string
	var errs []string

	for _, name := range names {
		// Stop the container (no-op if already stopped).
		stopCmd := exec.CommandContext(ctx, "sudo", "docker", "stop", name)
		if out, err := stopCmd.CombinedOutput(); err != nil {
			errs = append(errs, fmt.Sprintf("stop %s: %s", name, strings.TrimSpace(string(out))))
			continue
		}

		// Remove the container.
		rmCmd := exec.CommandContext(ctx, "sudo", "docker", "rm", name)
		if out, err := rmCmd.CombinedOutput(); err != nil {
			errs = append(errs, fmt.Sprintf("rm %s: %s", name, strings.TrimSpace(string(out))))
			continue
		}

		stopped = append(stopped, name)
	}

	return containersStopResult{Stopped: stopped, Errors: errs}, nil
}
