package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// logsFetchParams is the request payload for the logs.fetch RPC.
//
// vk-9itz: EnvironmentID is optional — when present, resolveContainer uses it
// as a primary filter (label=vmkit.environment_id=<id>) so two co-located
// environments with the same role can be distinguished. Older containers
// provisioned before the label was stamped are still found via the role-only
// fallback.
type logsFetchParams struct {
	Container     string `json:"container"`
	Tail          int    `json:"tail"`
	EnvironmentID string `json:"environment_id,omitempty"`
}

// logsFetchResult is the success response for the logs.fetch RPC.
// On a docker failure, Error is set and Lines is nil; the RPC itself still
// succeeds so the backend can surface the message rather than see a crash.
type logsFetchResult struct {
	Lines     []string `json:"lines,omitempty"`
	Container string   `json:"container,omitempty"`
	Tail      int      `json:"tail,omitempty"`
	Error     string   `json:"error,omitempty"`
}

const (
	defaultLogsTail = 200
	maxLogsTail     = 1000
)

// containerNameRe restricts container names to a safe character set. exec.Command
// does not invoke a shell, but validating the name keeps the surface defensive.
var containerNameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// envIDRe restricts the environment_id filter value to UUID-shaped strings so a
// caller can't smuggle additional --filter args by stuffing whitespace or `=`
// into the value. Backend only ever passes a UUID here; tighter than necessary
// is fine.
var envIDRe = regexp.MustCompile(`^[a-zA-Z0-9-]{1,64}$`)

// resolveContainer maps a logical container name to the actual running container
// name on the host.  Kamal names containers with a hash suffix
// ({service}-{role}-{dest}-{hash}), so callers can't know the exact name.
//
// Resolution order:
//  1. Exact name match — returned immediately if running.
//  2. vk-9itz: env-scoped role match — only when envID is set and valid:
//     docker ps --filter label=vmkit.environment_id=<id> --filter label=role=<role>.
//     Wins when two environments co-exist on the same VM; falls through if
//     the container predates the vmkit.environment_id label.
//  3. Kamal role-label lookup: docker ps --filter label=role={name}.
//     "app" (the default sentinel) is treated as an alias for "web".
//  4. For the "app" sentinel only: first running Kamal-managed container
//     (label=service, any role) as a last resort.
func resolveContainer(ctx context.Context, requested, envID string) string {
	// 1. Exact name match.
	chk := exec.CommandContext(ctx, "sudo", "docker", "ps", "--filter",
		"name="+requested, "--filter", "status=running", "--format", "{{.Names}}")
	out, err := chk.Output()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if strings.TrimSpace(line) == requested {
				return requested
			}
		}
	}

	// Role label used for both env-scoped and legacy role-only lookups below.
	roleLabel := requested
	if requested == "app" {
		roleLabel = "web"
	}

	// 2. vk-9itz: env-scoped role match. Skipped when envID is missing or
	//    rejected by the safe-char regex so an unexpected value can't be
	//    weaponised as an additional --filter argument.
	if envID != "" && envIDRe.MatchString(envID) {
		envOut, err := exec.CommandContext(ctx, "sudo", "docker", "ps",
			"--filter", "label=vmkit.environment_id="+envID,
			"--filter", "label=role="+roleLabel,
			"--filter", "status=running",
			"--format", "{{.Names}}").Output()
		if err == nil {
			for _, name := range strings.Split(strings.TrimSpace(string(envOut)), "\n") {
				name = strings.TrimSpace(name)
				if name != "" && name != "kamal-proxy" {
					return name
				}
			}
		}
		// Fall through to legacy resolution if no env-tagged container exists
		// (older deploys predate the label stamp).
	}

	// 3. Kamal role-label lookup.  "app" is an alias for the primary "web" role.
	roleOut, err := exec.CommandContext(ctx, "sudo", "docker", "ps",
		"--filter", "label=role="+roleLabel,
		"--filter", "status=running",
		"--format", "{{.Names}}").Output()
	if err == nil {
		for _, name := range strings.Split(strings.TrimSpace(string(roleOut)), "\n") {
			name = strings.TrimSpace(name)
			if name != "" && name != "kamal-proxy" {
				return name
			}
		}
	}

	// 4. Fallback for the "app" sentinel only: first running Kamal container
	//    regardless of role.  For explicit role names we let the requested value
	//    pass through so the caller gets a clear "No such container" from Docker.
	if requested == "app" {
		listOut, err := exec.CommandContext(ctx, "sudo", "docker", "ps",
			"--filter", "label=service",
			"--filter", "status=running",
			"--format", "{{.Names}}").Output()
		if err == nil {
			for _, name := range strings.Split(strings.TrimSpace(string(listOut)), "\n") {
				name = strings.TrimSpace(name)
				if name != "" && name != "kamal-proxy" {
					return name
				}
			}
		}
	}

	return requested
}

// LogsFetch handles the logs.fetch RPC: it runs `docker logs` for a container
// and returns the trailing lines. Docker errors (missing container, daemon
// down) are returned as a structured error payload, never a daemon crash.
func LogsFetch(ctx context.Context, params json.RawMessage) (any, error) {
	var p logsFetchParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	container := p.Container
	if container == "" {
		container = "app"
	}
	if !containerNameRe.MatchString(container) {
		return logsFetchResult{Error: fmt.Sprintf("invalid container name: %q", container)}, nil
	}

	// Resolve logical names ("app", "web", "worker", …) to the actual Kamal
	// container name.  Applied unconditionally so non-default names work too.
	// vk-9itz: thread the optional environment_id through so multi-environment
	// VMs disambiguate by label rather than by docker's listing order.
	container = resolveContainer(ctx, container, p.EnvironmentID)

	tail := p.Tail
	if tail <= 0 {
		tail = defaultLogsTail
	}
	if tail > maxLogsTail {
		tail = maxLogsTail
	}

	cmd := exec.CommandContext(ctx, "sudo", "docker", "logs", "--tail", strconv.Itoa(tail), container)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return logsFetchResult{Container: container, Tail: tail, Error: msg}, nil
	}

	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = []string{}
	}
	return logsFetchResult{Lines: lines, Container: container, Tail: tail}, nil
}
