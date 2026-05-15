package destroy

import (
	"testing"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

func TestDestroy(t *testing.T) {
	tests := []struct {
		name       string
		config     *types.DestroyConfig
		wantErr    bool
		checkSteps bool
	}{
		{
			name: "basic destroy",
			config: &types.DestroyConfig{
				InstanceID: "test-123",
				Subdomain:  "test-subdomain",
				BaseDir:    "/tmp/test-supabase",
			},
			wantErr:    false,
			checkSteps: true,
		},
		{
			name: "destroy with default base dir",
			config: &types.DestroyConfig{
				InstanceID: "test-456",
				Subdomain:  "another-subdomain",
				BaseDir:    "", // Should use default
			},
			wantErr:    false,
			checkSteps: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Destroy(tt.config)

			// Check if instance ID is set
			if result.InstanceID != tt.config.InstanceID {
				t.Errorf("InstanceID = %v, want %v", result.InstanceID, tt.config.InstanceID)
			}

			// Check if we have steps
			if tt.checkSteps && len(result.Steps) == 0 {
				t.Error("Expected steps in result, got none")
			}

			// For this test, we expect it to handle non-existent resources gracefully (idempotent)
			// So even though resources don't exist, it should succeed
			if tt.wantErr && result.Success {
				t.Error("Expected failure, got success")
			}
		})
	}
}

// closeFirewallPort unit: parse+diff logic is covered by openport's tests;
// here we pin the short-circuit so a pre-0.12.2 caller with no port info
// doesn't shell out to UFW (which would fail on hosts that have ufw
// disabled). See supabyoi-v3bm.
func TestCloseFirewallPort_ZeroPortIsNoOp(t *testing.T) {
	if err := closeFirewallPort(0); err != nil {
		t.Fatalf("closeFirewallPort(0) should be a no-op; got %v", err)
	}
}

func TestIsIgnorableError(t *testing.T) {
	tests := []struct {
		name    string
		errMsg  string
		want    bool
	}{
		{
			name:    "nil error",
			errMsg:  "",
			want:    false,
		},
		{
			name:    "file not found",
			errMsg:  "no such file or directory",
			want:    true,
		},
		{
			name:    "does not exist",
			errMsg:  "resource does not exist",
			want:    true,
		},
		{
			name:    "not found",
			errMsg:  "container not found",
			want:    true,
		},
		{
			name:    "no configuration file",
			errMsg:  "no configuration file provided",
			want:    true,
		},
		{
			name:    "cannot find",
			errMsg:  "cannot find the specified resource",
			want:    true,
		},
		{
			name:    "real error",
			errMsg:  "permission denied",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.errMsg != "" {
				err = &testError{msg: tt.errMsg}
			}

			got := isIgnorableError(err)
			if got != tt.want {
				t.Errorf("isIgnorableError() = %v, want %v", got, tt.want)
			}
		})
	}
}

// testError is a simple error type for testing
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
