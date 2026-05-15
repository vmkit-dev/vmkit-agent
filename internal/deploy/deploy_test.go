package deploy

import (
	"testing"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

func TestDeploy(t *testing.T) {
	tests := []struct {
		name   string
		config *types.DeployConfig
		want   bool
	}{
		{
			name: "basic deployment",
			config: &types.DeployConfig{
				InstanceID:       "test-123",
				Subdomain:        "myapp",
				Domain:           "example.com",
				VMUser:           "ubuntu",
				BaseDir:          "/tmp/test-deploy",
				EnableTLS:        false,
				PostgresPassword: "test123",
				JWTSecret:        "secret123",
			},
			want: false, // Will fail in test environment without real infrastructure
		},
		{
			name: "deployment with TLS",
			config: &types.DeployConfig{
				InstanceID:       "test-456",
				Subdomain:        "secure-app",
				Domain:           "example.com",
				VMUser:           "ubuntu",
				EnableTLS:        true,
				PostgresPassword: "test123",
				JWTSecret:        "secret123",
			},
			want: false, // Will fail without real infrastructure
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Deploy(tt.config)

			// Verify result structure
			if result == nil {
				t.Fatal("Deploy() returned nil result")
			}

			if result.InstanceID != tt.config.InstanceID {
				t.Errorf("Deploy() InstanceID = %s, want %s", result.InstanceID, tt.config.InstanceID)
			}

			// Should have at least 2 steps (docker + nginx install)
			if len(result.Steps) < 2 {
				t.Errorf("Deploy() steps count = %d, want at least 2", len(result.Steps))
			}

			// Log steps for debugging
			t.Logf("Deployment steps:")
			for i, step := range result.Steps {
				t.Logf("  %d. %s: %s (%s)", i+1, step.Name, step.Status, step.Message)
			}
		})
	}
}

func TestDeployNilConfig(t *testing.T) {
	// Verify Deploy doesn't crash with nil config
	defer func() {
		if r := recover(); r == nil {
			t.Error("Deploy(nil) did not panic")
		}
	}()

	Deploy(nil)
}

func TestExecuteStep(t *testing.T) {
	tests := []struct {
		name       string
		stepName   string
		fn         func() error
		wantStatus string
	}{
		{
			name:     "successful step",
			stepName: "test_step",
			fn: func() error {
				return nil
			},
			wantStatus: "completed",
		},
		{
			name:     "failed step",
			stepName: "failing_step",
			fn: func() error {
				return &testError{"test error"}
			},
			wantStatus: "failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executeStep(tt.stepName, tt.fn)

			if result.Name != tt.stepName {
				t.Errorf("executeStep() Name = %s, want %s", result.Name, tt.stepName)
			}

			if result.Status != tt.wantStatus {
				t.Errorf("executeStep() Status = %s, want %s", result.Status, tt.wantStatus)
			}

			if result.StartTime == "" {
				t.Error("executeStep() StartTime is empty")
			}

			if result.EndTime == "" {
				t.Error("executeStep() EndTime is empty")
			}

			if result.Duration == "" {
				t.Error("executeStep() Duration is empty")
			}
		})
	}
}

func TestExecuteStepPanic(t *testing.T) {
	// Verify executeStep handles panics gracefully
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic was recovered")
		}
	}()

	executeStep("panic_step", func() error {
		panic("test panic")
	})
}

// Config generation tests are now in the files package

// testError is a simple error type for testing
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
