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
var containerNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

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
