package handlers

import (
	"context"
	"encoding/json"
	"testing"
)

func TestLogsFetchInvalidParams(t *testing.T) {
	_, err := LogsFetch(context.Background(), json.RawMessage(`{bad`))
	if err == nil {
		t.Error("expected error for invalid JSON params")
	}
}

func TestLogsFetchInvalidContainerName(t *testing.T) {
	res, err := LogsFetch(context.Background(), json.RawMessage(`{"container":"app; rm -rf /"}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	r := res.(logsFetchResult)
	if r.Error == "" {
		t.Error("expected error payload for invalid container name")
	}
	if r.Lines != nil {
		t.Error("expected no lines for invalid container name")
	}
}

func TestLogsFetchDockerErrorNeverCrashes(t *testing.T) {
	// A container that does not exist (or no docker daemon) must yield a
	// structured error payload, never a returned error or a panic.
	res, err := LogsFetch(context.Background(), json.RawMessage(`{"container":"vmkit_test_nonexistent"}`))
	if err != nil {
		t.Fatalf("expected nil error on docker failure, got %v", err)
	}
	r := res.(logsFetchResult)
	if r.Error == "" {
		t.Error("expected error payload when docker logs fails")
	}
}

func TestLogsFetchTailClamp(t *testing.T) {
	// tail above maxLogsTail is clamped; tail <= 0 falls back to the default.
	// The docker call fails in CI, but the clamped tail is echoed in the result.
	res, _ := LogsFetch(context.Background(), json.RawMessage(`{"container":"vmkit_test_nonexistent","tail":99999}`))
	if got := res.(logsFetchResult).Tail; got != maxLogsTail {
		t.Errorf("tail not clamped: got %d, want %d", got, maxLogsTail)
	}

	res, _ = LogsFetch(context.Background(), json.RawMessage(`{"container":"vmkit_test_nonexistent","tail":0}`))
	if got := res.(logsFetchResult).Tail; got != defaultLogsTail {
		t.Errorf("tail default not applied: got %d, want %d", got, defaultLogsTail)
	}
}
