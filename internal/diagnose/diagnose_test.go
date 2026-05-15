package diagnose

import (
	"testing"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

func TestDiagnoseNilConfig(t *testing.T) {
	result, err := Diagnose(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
	if result != nil {
		t.Fatal("expected nil result for nil config")
	}
}

func TestDiagnoseDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Diagnose() panicked: %v", r)
		}
	}()

	config := &types.DiagnoseConfig{
		InstanceID: "test-123",
		Subdomain:  "test-instance",
		BaseDir:    "/opt/supabase",
		Timeout:    10,
	}

	result, err := Diagnose(config)
	if err != nil {
		t.Fatalf("Diagnose() returned error: %v", err)
	}
	if result == nil {
		t.Fatal("Diagnose() returned nil result")
	}
}

func TestDiagnoseResultStructure(t *testing.T) {
	config := &types.DiagnoseConfig{
		InstanceID: "test-456",
		Subdomain:  "myapp",
		BaseDir:    "/opt/supabase",
		Timeout:    10,
	}

	result, err := Diagnose(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.InstanceID != "test-456" {
		t.Errorf("expected instance_id 'test-456', got '%s'", result.InstanceID)
	}

	if result.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}

	if result.Duration == "" {
		t.Error("expected non-empty duration")
	}

	if result.AgentVersion == "" {
		t.Error("expected non-empty agent_version")
	}

	if len(result.Checks) == 0 {
		t.Error("expected at least one diagnostic check")
	}

	validStatuses := map[string]bool{"pass": true, "warn": true, "fail": true}
	for i, check := range result.Checks {
		if check.Name == "" {
			t.Errorf("check %d has empty name", i)
		}
		if !validStatuses[check.Status] {
			t.Errorf("check %d (%s) has invalid status '%s'", i, check.Name, check.Status)
		}
		if check.Detail == "" {
			t.Errorf("check %d (%s) has empty detail", i, check.Name)
		}
	}
}

func TestDiagnoseOverallStatus(t *testing.T) {
	config := &types.DiagnoseConfig{
		InstanceID: "test-789",
		Subdomain:  "nonexistent-instance",
		BaseDir:    "/opt/supabase",
		Timeout:    10,
	}

	result, err := Diagnose(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	validStatuses := map[string]bool{"pass": true, "warn": true, "fail": true}
	if !validStatuses[result.OverallStatus] {
		t.Errorf("invalid overall_status '%s'", result.OverallStatus)
	}
}

func TestCheckExpectedNames(t *testing.T) {
	config := &types.DiagnoseConfig{
		InstanceID: "test",
		Subdomain:  "test",
		BaseDir:    "/opt/supabase",
		Timeout:    10,
	}

	result, err := Diagnose(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedNames := map[string]bool{
		"docker_daemon":        false,
		"containers":           false,
		"container_restarts":   false,
		"disk_usage":           false,
		"memory":               false,
		"cpu_load":             false,
		"nginx_config":         false,
		"tls_certs":            false,
		"docker_logs_errors":   false,
		"health_endpoint":      false,
		"postgres_connectivity": false,
		"backup_status":        false,
	}

	for _, check := range result.Checks {
		if _, ok := expectedNames[check.Name]; ok {
			expectedNames[check.Name] = true
		}
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("expected check '%s' not found in results", name)
		}
	}
}

func TestParseMemInfoValue(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"MemTotal:        4028416 kB", 4028416},
		{"MemAvailable:    2514176 kB", 2514176},
		{"", 0},
		{"BadLine", 0},
	}

	for _, tt := range tests {
		got := parseMemInfoValue(tt.input)
		if got != tt.expected {
			t.Errorf("parseMemInfoValue(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}
