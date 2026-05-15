package health

import (
	"testing"
	"time"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

func TestCheckEnhanced_NilConfig(t *testing.T) {
	_, err := CheckEnhanced(nil)
	if err == nil {
		t.Error("Expected error with nil config")
	}
}

func TestCheckEnhanced_BasicLevel(t *testing.T) {
	config := &types.EnhancedHealthCheckConfig{
		InstanceID: "test-instance",
		Subdomain:  "test-nonexistent",
		BaseDir:    "/opt/supabase",
		Level:      types.HealthCheckBasic,
		Timeout:    10,
	}

	result, err := CheckEnhanced(config)
	if err != nil {
		t.Fatalf("CheckEnhanced failed: %v", err)
	}

	if result == nil {
		t.Fatal("Result should not be nil")
	}

	if result.InstanceID != "test-instance" {
		t.Errorf("InstanceID = %s, want test-instance", result.InstanceID)
	}

	if result.Level != "basic" {
		t.Errorf("Level = %s, want basic", result.Level)
	}

	if result.Timestamp == "" {
		t.Error("Timestamp should not be empty")
	}

	if result.Duration == "" {
		t.Error("Duration should not be empty")
	}

	// In test environment, containers won't exist
	if result.OverallStatus == "healthy" {
		t.Error("Should not be healthy without running containers")
	}

	if len(result.Containers) == 0 {
		t.Error("Should have checked containers")
	}

	if len(result.Checks) == 0 {
		t.Error("Should have performed basic checks")
	}
}

func TestCheckEnhanced_DeepLevel(t *testing.T) {
	config := &types.EnhancedHealthCheckConfig{
		InstanceID: "test-instance",
		Subdomain:  "test-nonexistent",
		BaseDir:    "/opt/supabase",
		Level:      types.HealthCheckDeep,
		Timeout:    10,
	}

	result, err := CheckEnhanced(config)
	if err != nil {
		t.Fatalf("CheckEnhanced failed: %v", err)
	}

	// Deep level should include more checks
	basicCheckCount := 3  // postgres, kong, studio
	deepCheckCount := 5   // database query, REST, auth, storage, realtime
	minExpectedChecks := basicCheckCount + deepCheckCount

	if len(result.Checks) < minExpectedChecks {
		t.Errorf("Deep level should have at least %d checks, got %d", minExpectedChecks, len(result.Checks))
	}

	// Verify some deep checks are present
	hasDeepChecks := false
	for _, check := range result.Checks {
		if check.Name == "database_query_execution" ||
		   check.Name == "rest_api_endpoint" ||
		   check.Name == "auth_endpoint" {
			hasDeepChecks = true
			break
		}
	}

	if !hasDeepChecks {
		t.Error("Deep level should include deep diagnostic checks")
	}
}

func TestCheckEnhanced_MetricsLevel(t *testing.T) {
	config := &types.EnhancedHealthCheckConfig{
		InstanceID: "test-instance",
		Subdomain:  "test-nonexistent",
		BaseDir:    "/opt/supabase",
		Level:      types.HealthCheckMetrics,
		Timeout:    10,
	}

	result, err := CheckEnhanced(config)
	if err != nil {
		t.Fatalf("CheckEnhanced failed: %v", err)
	}

	// Metrics level should attempt to collect metrics
	// Even if containers aren't running, metrics should be attempted
	t.Logf("Metrics level result: %d containers, %d checks", len(result.Containers), len(result.Checks))
}

func TestCheckEnhanced_ServicesLevel(t *testing.T) {
	config := &types.EnhancedHealthCheckConfig{
		InstanceID: "test-instance",
		Subdomain:  "test-nonexistent",
		BaseDir:    "/opt/supabase",
		Level:      types.HealthCheckServices,
		Timeout:    10,
	}

	result, err := CheckEnhanced(config)
	if err != nil {
		t.Fatalf("CheckEnhanced failed: %v", err)
	}

	// Services level should include service diagnostics
	if len(result.Services) == 0 {
		t.Error("Services level should include service diagnostics")
	}
}

func TestCheckEnhanced_FullLevel(t *testing.T) {
	config := &types.EnhancedHealthCheckConfig{
		InstanceID: "test-instance",
		Subdomain:  "test-nonexistent",
		BaseDir:    "/opt/supabase",
		Level:      types.HealthCheckFull,
		Timeout:    15,
	}

	result, err := CheckEnhanced(config)
	if err != nil {
		t.Fatalf("CheckEnhanced failed: %v", err)
	}

	// Full level should include all types of checks
	if len(result.Checks) < 8 {
		t.Errorf("Full level should have many checks, got %d", len(result.Checks))
	}

	if len(result.Services) == 0 {
		t.Error("Full level should include service diagnostics")
	}

	// Containers should be checked
	if len(result.Containers) == 0 {
		t.Error("Full level should check containers")
	}
}

func TestCheckContainerStatus(t *testing.T) {
	containers, err := checkContainerStatus("test-nonexistent")

	if err != nil {
		t.Fatalf("checkContainerStatus failed: %v", err)
	}

	// Should check 7 containers (postgres, kong, studio, auth, rest, realtime, storage)
	if len(containers) != 7 {
		t.Errorf("Expected 7 containers, got %d", len(containers))
	}

	// Verify each container has required fields
	for i, container := range containers {
		if container.Name == "" {
			t.Errorf("Container %d has empty name", i)
		}
		if container.Status == "" {
			t.Errorf("Container %d has empty status", i)
		}
		// In test environment, containers won't exist
		if container.Status != "not_found" && container.Status != "stopped" {
			t.Logf("Unexpected container status: %s", container.Status)
		}
	}
}

func TestCheckSingleContainer(t *testing.T) {
	status := checkSingleContainer("nonexistent-container")

	if status.Name != "nonexistent-container" {
		t.Errorf("Name = %s, want nonexistent-container", status.Name)
	}

	if status.Status != "not_found" {
		t.Errorf("Status should be not_found for nonexistent container, got %s", status.Status)
	}
}

func TestCheckPostgresConnectivity(t *testing.T) {
	success, message, duration := checkPostgresConnectivity("test-nonexistent")

	// Should fail in test environment
	if success {
		t.Error("Should not succeed without running PostgreSQL")
	}

	if message == "" {
		t.Error("Message should not be empty")
	}

	if duration < 0 {
		t.Error("Duration should not be negative")
	}
}

func TestCheckDatabaseQuery(t *testing.T) {
	success, message, duration := checkDatabaseQuery("test-nonexistent")

	// Should fail in test environment
	if success {
		t.Error("Should not succeed without running database")
	}

	if message == "" {
		t.Error("Message should not be empty")
	}

	if duration < 0 {
		t.Error("Duration should not be negative")
	}
}

func TestCheckKongGatewayHealth(t *testing.T) {
	success, message, duration := checkKongGatewayHealth(8000)

	// May or may not succeed depending on environment
	t.Logf("Kong health: success=%v, message=%s, duration=%dms", success, message, duration)

	if message == "" {
		t.Error("Message should not be empty")
	}

	if duration < 0 {
		t.Error("Duration should not be negative")
	}
}

func TestCheckHTTPEndpoint(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		serviceName string
	}{
		{"localhost", "http://localhost:9999", "TestService"},
		{"invalid", "http://invalid-host-that-does-not-exist:8000", "InvalidService"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			success, message, duration := checkHTTPEndpoint(tt.url, tt.serviceName)

			// Should fail for non-existent endpoints
			if success && tt.name == "invalid" {
				t.Error("Should not succeed for invalid endpoint")
			}

			if message == "" {
				t.Error("Message should not be empty")
			}

			if duration < 0 {
				t.Error("Duration should not be negative")
			}
		})
	}
}

func TestCheckPostgresDiagnostics(t *testing.T) {
	diag := checkPostgresDiagnostics("test-nonexistent")

	if diag.Name != "postgresql" {
		t.Errorf("Name = %s, want postgresql", diag.Name)
	}

	// Should be degraded or unhealthy without running database
	if diag.Status == "healthy" {
		t.Error("Should not be healthy without running database")
	}

	if diag.Details == nil {
		t.Error("Details should not be nil")
	}

	if diag.Errors == nil {
		t.Error("Errors should not be nil")
	}
}

func TestCheckKongDiagnostics(t *testing.T) {
	diag := checkKongDiagnostics()

	if diag.Name != "kong" {
		t.Errorf("Name = %s, want kong", diag.Name)
	}

	if diag.Details == nil {
		t.Error("Details should not be nil")
	}

	if diag.Errors == nil {
		t.Error("Errors should not be nil")
	}

	// Status depends on whether Kong is running
	t.Logf("Kong diagnostics: status=%s, errors=%v", diag.Status, diag.Errors)
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"minutes", 45 * time.Minute, "45m"},
		{"hours and minutes", 3*time.Hour + 20*time.Minute, "3h 20m"},
		{"days", 2*24*time.Hour + 5*time.Hour + 30*time.Minute, "2d 5h 30m"},
		{"just over a day", 25 * time.Hour, "1d 1h 0m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("formatDuration(%v) = %s, want %s", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestParseMemorySize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		{"megabytes", "512MiB", 512},
		{"gigabytes", "2GiB", 2048},
		{"megabytes MB", "256MB", 256},
		{"gigabytes GB", "1GB", 1024},
		{"with spaces", " 128MiB ", 128},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseMemorySize(tt.input)
			if result != tt.expected {
				t.Errorf("parseMemorySize(%s) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseNetworkSize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected float64
	}{
		{"megabytes", "100MB", 100.0},
		{"gigabytes", "2GB", 2048.0},
		{"kilobytes", "500KB", 0.5},
		{"bytes", "1000B", 0.001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseNetworkSize(tt.input)
			if result != tt.expected {
				t.Errorf("parseNetworkSize(%s) = %f, want %f", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGetContainerMetrics(t *testing.T) {
	metrics := getContainerMetrics("nonexistent-container")

	if metrics == nil {
		t.Fatal("Metrics should not be nil")
	}

	if metrics.Name != "nonexistent-container" {
		t.Errorf("Name = %s, want nonexistent-container", metrics.Name)
	}

	// Metrics should be zero for nonexistent container
	t.Logf("Container metrics: CPU=%.2f%%, Memory=%dMB", metrics.CPUPercent, metrics.MemoryMB)
}

func TestEnhancedHealthCheckResult_Structure(t *testing.T) {
	// Test that we can create and populate the result structure
	result := &types.EnhancedHealthCheckResult{
		InstanceID:    "test-123",
		OverallStatus: "healthy",
		Timestamp:     time.Now().Format(time.RFC3339),
		Containers: []types.EnhancedContainerStatus{
			{
				Name:     "test-postgres",
				Status:   "running",
				Health:   "healthy",
				Uptime:   "2d 5h 30m",
				Restarts: 0,
				Metrics: &types.ContainerMetrics{
					Name:       "test-postgres",
					CPUPercent: 15.5,
					MemoryMB:   512,
				},
			},
		},
		Services: []types.ServiceDiagnostics{
			{
				Name:           "postgresql",
				Status:         "healthy",
				ResponseTimeMS: 10,
				Details: map[string]string{
					"connections": "5",
				},
			},
		},
		Checks: []types.HealthCheck{
			{
				Name:       "postgres_connectivity",
				Status:     "passed",
				Message:    "OK",
				DurationMS: 5,
			},
		},
		Duration: "500ms",
		Level:    "full",
	}

	if result.InstanceID != "test-123" {
		t.Error("InstanceID not set correctly")
	}

	if len(result.Containers) != 1 {
		t.Error("Containers not set correctly")
	}

	if len(result.Services) != 1 {
		t.Error("Services not set correctly")
	}

	if len(result.Checks) != 1 {
		t.Error("Checks not set correctly")
	}

	if result.Containers[0].Metrics == nil {
		t.Error("Container metrics should not be nil")
	}
}
