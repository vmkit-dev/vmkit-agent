// +build integration

package deploy

import (
	"os"
	"os/exec"
	"testing"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

// TestFullDeployFlow tests the complete deployment workflow
// This requires Docker and Nginx to be installed on the system
func TestFullDeployFlow(t *testing.T) {
	// Check prerequisites
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker not installed, skipping integration test")
	}
	if _, err := exec.LookPath("nginx"); err != nil {
		t.Skip("Nginx not installed, skipping integration test")
	}
	if os.Geteuid() != 0 {
		t.Skip("Integration test requires root privileges")
	}

	// Create test configuration
	config := &types.DeployConfig{
		InstanceID:       "integration-test",
		Subdomain:        "test-deploy",
		Domain:           "test-deploy.local",
		BaseDir:          "/tmp/supabase-integration-test",
		VMUser:           "root",
		EnableTLS:        false,
		PostgresPassword: "test-password",
		JWTSecret:        "test-jwt-secret",
	}

	// Run deployment
	result := Deploy(config)

	// Verify result structure
	if result == nil {
		t.Fatal("Deploy() returned nil result")
	}

	if result.InstanceID != config.InstanceID {
		t.Errorf("Result InstanceID = %s, want %s", result.InstanceID, config.InstanceID)
	}

	// Check steps
	if len(result.Steps) == 0 {
		t.Error("Deploy() returned no steps")
	}

	t.Logf("Deployment result: Success=%v, Steps=%d", result.Success, len(result.Steps))

	// Log all steps
	for i, step := range result.Steps {
		t.Logf("Step %d: %s - %s (%s)", i+1, step.Name, step.Status, step.Message)
	}

	// Verify critical steps completed
	stepNames := make(map[string]bool)
	for _, step := range result.Steps {
		stepNames[step.Name] = step.Status == "completed"
	}

	if !stepNames["docker_install"] {
		t.Error("Docker installation step did not complete successfully")
	}
	if !stepNames["nginx_install"] {
		t.Error("Nginx installation step did not complete successfully")
	}

	// If deployment failed, show why
	if !result.Success {
		t.Logf("Deployment failed at step: %s", result.FailedStep)
		t.Logf("Error: %s", result.ErrorMessage)
	}
}

// TestDeployIdempotency verifies that running deploy twice produces the same result
func TestDeployIdempotency(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker not installed")
	}
	if _, err := exec.LookPath("nginx"); err != nil {
		t.Skip("Nginx not installed")
	}

	config := &types.DeployConfig{
		InstanceID:       "idempotency-test",
		Subdomain:        "test-idem",
		Domain:           "test-idem.local",
		BaseDir:          "/tmp/supabase-idempotency-test",
		VMUser:           "root",
		EnableTLS:        false,
		PostgresPassword: "test-password",
		JWTSecret:        "test-jwt-secret",
	}

	// First deployment
	result1 := Deploy(config)
	if result1 == nil {
		t.Fatal("First deploy returned nil")
	}

	t.Logf("First deployment: Success=%v, Steps=%d", result1.Success, len(result1.Steps))

	// Second deployment (should be idempotent)
	result2 := Deploy(config)
	if result2 == nil {
		t.Fatal("Second deploy returned nil")
	}

	t.Logf("Second deployment: Success=%v, Steps=%d", result2.Success, len(result2.Steps))

	// Both deployments should have the same number of steps
	if len(result1.Steps) != len(result2.Steps) {
		t.Errorf("Step count mismatch: first=%d, second=%d", len(result1.Steps), len(result2.Steps))
	}

	// Both should succeed (or fail at the same step)
	if result1.Success != result2.Success {
		t.Errorf("Success mismatch: first=%v, second=%v", result1.Success, result2.Success)
	}

	// If both failed, they should fail at the same step
	if !result1.Success && !result2.Success {
		if result1.FailedStep != result2.FailedStep {
			t.Errorf("Failed at different steps: first=%s, second=%s", result1.FailedStep, result2.FailedStep)
		}
	}
}

// TestDeployWithInvalidConfig tests error handling
func TestDeployWithInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *types.DeployConfig
	}{
		{
			name: "empty instance ID",
			config: &types.DeployConfig{
				InstanceID: "",
				Subdomain:  "test",
				Domain:     "test.local",
			},
		},
		{
			name: "empty subdomain",
			config: &types.DeployConfig{
				InstanceID: "test",
				Subdomain:  "",
				Domain:     "test.local",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Deploy(tt.config)
			// Should not panic, even with invalid config
			// May succeed or fail depending on validation
			t.Logf("Result for %s: Success=%v", tt.name, result.Success)
		})
	}
}
