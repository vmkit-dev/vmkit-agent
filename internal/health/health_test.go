package health

import (
	"testing"
	"time"
)

func TestDefaultCheckConfig(t *testing.T) {
	config := DefaultCheckConfig("test-instance")

	if config.Subdomain != "test-instance" {
		t.Errorf("Subdomain = %s, want test-instance", config.Subdomain)
	}

	if config.Timeout != 60*time.Second {
		t.Errorf("Timeout = %v, want 60s", config.Timeout)
	}

	if config.RetryInterval != 2*time.Second {
		t.Errorf("RetryInterval = %v, want 2s", config.RetryInterval)
	}

	if config.MaxRetries != 30 {
		t.Errorf("MaxRetries = %d, want 30", config.MaxRetries)
	}

	if !config.UseExponential {
		t.Error("UseExponential should be true by default")
	}

	if config.PostgresPort != 5432 {
		t.Errorf("PostgresPort = %d, want 5432", config.PostgresPort)
	}

	if config.KongPort != 8000 {
		t.Errorf("KongPort = %d, want 8000", config.KongPort)
	}

	if config.StudioPort != 3000 {
		t.Errorf("StudioPort = %d, want 3000", config.StudioPort)
	}
}

func TestCheckConfigNil(t *testing.T) {
	// CheckAll should handle nil config gracefully
	result, err := CheckAll(nil)

	// Should return an error since services won't be running in test environment
	if err == nil {
		t.Error("Expected error when checking non-existent services")
	}

	if result == nil {
		t.Fatal("Result should not be nil even on error")
	}

	if result.AllHealthy {
		t.Error("AllHealthy should be false when services are not running")
	}

	if len(result.Services) == 0 {
		t.Error("Should have checked at least some services")
	}
}

func TestRunHealthChecks(t *testing.T) {
	config := DefaultCheckConfig("nonexistent")

	result := runHealthChecks(config)

	if result == nil {
		t.Fatal("runHealthChecks returned nil")
	}

	// In test environment without running containers, health checks should fail
	if result.AllHealthy {
		t.Error("Expected AllHealthy to be false in test environment")
	}

	if len(result.Services) == 0 {
		t.Error("Should have performed some health checks")
	}

	// Verify each service has required fields
	for i, svc := range result.Services {
		if svc.Name == "" {
			t.Errorf("Service %d has empty name", i)
		}
		if svc.CheckedAt == "" {
			t.Errorf("Service %d has empty CheckedAt", i)
		}
		if svc.Message == "" {
			t.Errorf("Service %d has empty Message", i)
		}
	}
}

func TestCheckContainerStates(t *testing.T) {
	services := checkContainerStates("test-nonexistent", legacyServiceSuffixes)

	// Should check 3 containers (postgres, kong, studio)
	if len(services) != 3 {
		t.Errorf("Expected 3 container checks, got %d", len(services))
	}

	// All should be unhealthy in test environment
	for _, svc := range services {
		if svc.Healthy {
			t.Errorf("Service %s should not be healthy in test environment", svc.Name)
		}
		if svc.Name == "" {
			t.Error("Service name should not be empty")
		}
		if svc.Message == "" {
			t.Error("Service message should not be empty")
		}
	}
}

func TestCheckPostgres(t *testing.T) {
	health := checkPostgres("test-app", "postgres", 5432)

	if health.Name != "postgresql" {
		t.Errorf("Name = %s, want postgresql", health.Name)
	}

	if health.CheckedAt == "" {
		t.Error("CheckedAt should not be empty")
	}

	// Should be unhealthy in test environment
	if health.Healthy {
		t.Error("Should not be healthy in test environment")
	}

	if health.Message == "" {
		t.Error("Message should not be empty")
	}

	if health.Latency == "" {
		t.Error("Latency should not be empty")
	}
}

func TestCheckKongGateway(t *testing.T) {
	health := checkKongGateway(8000)

	if health.Name != "kong-gateway" {
		t.Errorf("Name = %s, want kong-gateway", health.Name)
	}

	if health.CheckedAt == "" {
		t.Error("CheckedAt should not be empty")
	}

	// Health status depends on whether Kong is running
	// Just verify the check completes and returns valid data
	t.Logf("Kong health check: Healthy=%v, Message=%s", health.Healthy, health.Message)

	if health.Message == "" {
		t.Error("Message should not be empty")
	}

	if health.Latency == "" {
		t.Error("Latency should not be empty")
	}
}

func TestCheckStudio(t *testing.T) {
	health := checkStudio(3000)

	if health.Name != "studio" {
		t.Errorf("Name = %s, want studio", health.Name)
	}

	if health.CheckedAt == "" {
		t.Error("CheckedAt should not be empty")
	}

	// Health status depends on whether Studio is running
	// Just verify the check completes and returns valid data
	t.Logf("Studio health check: Healthy=%v, Message=%s", health.Healthy, health.Message)

	if health.Message == "" {
		t.Error("Message should not be empty")
	}

	if health.Latency == "" {
		t.Error("Latency should not be empty")
	}
}

func TestMinFunction(t *testing.T) {
	tests := []struct {
		name string
		a    int
		b    int
		want int
	}{
		{"both positive", 5, 10, 5},
		{"both negative", -5, -10, -10},
		{"mixed", -5, 10, -5},
		{"equal", 5, 5, 5},
		{"zero and positive", 0, 5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := min(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("min(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestHealthCheckResultStructure(t *testing.T) {
	// Test that we can create and populate a HealthCheckResult
	result := &HealthCheckResult{
		AllHealthy: true,
		Services: []ServiceHealth{
			{
				Name:      "test-service",
				Healthy:   true,
				Message:   "OK",
				Latency:   "100ms",
				CheckedAt: time.Now().Format(time.RFC3339),
			},
		},
		Duration: "1s",
	}

	if !result.AllHealthy {
		t.Error("AllHealthy should be true")
	}

	if len(result.Services) != 1 {
		t.Errorf("Services count = %d, want 1", len(result.Services))
	}

	if result.Duration != "1s" {
		t.Errorf("Duration = %s, want 1s", result.Duration)
	}
}

func TestExponentialBackoff(t *testing.T) {
	config := &CheckConfig{
		Subdomain:      "test",
		Timeout:        5 * time.Second,
		RetryInterval:  1 * time.Second,
		MaxRetries:     3,
		UseExponential: true,
	}

	// This will fail but we're testing that it doesn't panic
	// and completes within reasonable time
	start := time.Now()
	_, err := CheckAll(config)
	duration := time.Since(start)

	if err == nil {
		t.Error("Expected error when services are not running")
	}

	// Should complete within timeout + some overhead
	if duration > 10*time.Second {
		t.Errorf("CheckAll took too long: %v", duration)
	}

	// Should take at least some time for retries
	if duration < 2*time.Second {
		t.Errorf("CheckAll completed too quickly, may not be retrying: %v", duration)
	}
}
