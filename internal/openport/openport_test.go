package openport

import (
	"testing"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

func TestOpenPort_InvalidPort(t *testing.T) {
	tests := []struct {
		name string
		port int
	}{
		{"zero port", 0},
		{"negative port", -1},
		{"port too high", 70000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &types.OpenPortConfig{Port: tt.port}
			result := OpenPort(cfg)
			if result.Success {
				t.Error("expected failure for invalid port")
			}
			if result.ErrorMessage == "" {
				t.Error("expected error message for invalid port")
			}
		})
	}
}

func TestOpenPort_DefaultProtocol(t *testing.T) {
	// Can't actually test UFW without root, but verify protocol defaults
	cfg := &types.OpenPortConfig{Port: 5432}
	result := OpenPort(cfg)
	// Will fail because UFW is not installed in test env, but protocol should be set
	if result.Protocol != "tcp" {
		t.Errorf("expected protocol 'tcp', got '%s'", result.Protocol)
	}
	if result.Port != 5432 {
		t.Errorf("expected port 5432, got %d", result.Port)
	}
}
