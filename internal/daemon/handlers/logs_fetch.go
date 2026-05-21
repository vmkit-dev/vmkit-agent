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
type logsFetchParams struct {
	Container string `json:"container"`
	Tail      int    `json:"tail"`
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

// resolveContainer returns the container name to use. When the caller passes
// "app" (the default) and no container with that exact name exists, we fall
// back to the first running Kamal app container on the host — identified by
// the label=service filter that Kamal stamps on every managed container,
// excluding kamal-proxy itself. This lets the MCP tool work without the caller
// knowing the Kamal service name ({repo}-{dest}-web-{hash}).
func resolveContainer(ctx context.Context, requested string) string {
	// Check if the exact container exists and is running.
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

	// Exact name not running — fall back to first running Kamal app container.
	list := exec.CommandContext(ctx, "sudo", "docker", "ps",
		"--filter", "label=service",
		"--filter", "status=running",
		"--format", "{{.Names}}")
	out, err = list.Output()
	if err != nil {
		return requested
	}
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name = strings.TrimSpace(name)
		if name != "" && name != "kamal-proxy" {
			return name
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

	// When the caller uses the default "app" sentinel, resolve to the actual
	// running Kamal container (whose name includes the service + dest + hash).
	if container == "app" {
		container = resolveContainer(ctx, container)
	}

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
