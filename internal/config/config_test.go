package config

import (
	"os"
	"testing"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name       string
		configJSON string
		wantErr    bool
		errMsg     string
		validate   func(t *testing.T, config *types.DeployConfig)
	}{
		{
			name: "valid minimal config",
			configJSON: `{
				"instance_id": "test-123",
				"subdomain": "myapp",
				"domain": "myapp.supabyoi.com",
				"postgres_password": "secret123",
				"jwt_secret": "jwt-secret-min-32-chars-long-xxx"
			}`,
			wantErr: false,
			validate: func(t *testing.T, config *types.DeployConfig) {
				if config.InstanceID != "test-123" {
					t.Errorf("InstanceID = %s, want test-123", config.InstanceID)
				}
				if config.Subdomain != "myapp" {
					t.Errorf("Subdomain = %s, want myapp", config.Subdomain)
				}
				if config.Domain != "myapp.supabyoi.com" {
					t.Errorf("Domain = %s, want myapp.supabyoi.com", config.Domain)
				}
				if config.PostgresPassword != "secret123" {
					t.Errorf("PostgresPassword = %s, want secret123", config.PostgresPassword)
				}
				// Test defaults
				if config.BaseDir != "/opt/supabase" {
					t.Errorf("BaseDir = %s, want /opt/supabase", config.BaseDir)
				}
				if config.KongPort != 8000 {
					t.Errorf("KongPort = %d, want 8000", config.KongPort)
				}
				if config.PostgresPort != 5432 {
					t.Errorf("PostgresPort = %d, want 5432", config.PostgresPort)
				}
				if config.StudioPort != 3000 {
					t.Errorf("StudioPort = %d, want 3000", config.StudioPort)
				}
			},
		},
		{
			name: "valid full config",
			configJSON: `{
				"instance_id": "full-456",
				"subdomain": "fullapp",
				"domain": "fullapp.supabyoi.com",
				"base_dir": "/custom/path",
				"vm_user": "admin",
				"enable_tls": true,
				"postgres_password": "secret",
				"jwt_secret": "jwt-secret-value",
				"publishable_key": "anon-key",
				"secret_key": "service-role-key",
				"kong_port": 9000,
				"postgres_port": 6543,
				"studio_port": 4000,
				"environment": {
					"CUSTOM_VAR": "custom-value"
				}
			}`,
			wantErr: false,
			validate: func(t *testing.T, config *types.DeployConfig) {
				if config.InstanceID != "full-456" {
					t.Errorf("InstanceID = %s, want full-456", config.InstanceID)
				}
				if config.BaseDir != "/custom/path" {
					t.Errorf("BaseDir = %s, want /custom/path", config.BaseDir)
				}
				if config.VMUser != "admin" {
					t.Errorf("VMUser = %s, want admin", config.VMUser)
				}
				if !config.EnableTLS {
					t.Error("EnableTLS should be true")
				}
				if config.PostgresPassword != "secret" {
					t.Errorf("PostgresPassword = %s, want secret", config.PostgresPassword)
				}
				if config.JWTSecret != "jwt-secret-value" {
					t.Errorf("JWTSecret = %s, want jwt-secret-value", config.JWTSecret)
				}
				if config.PublishableKey != "anon-key" {
					t.Errorf("PublishableKey = %s, want anon-key", config.PublishableKey)
				}
				if config.SecretKey != "service-role-key" {
					t.Errorf("SecretKey = %s, want service-role-key", config.SecretKey)
				}
				if config.KongPort != 9000 {
					t.Errorf("KongPort = %d, want 9000", config.KongPort)
				}
				if config.Environment["CUSTOM_VAR"] != "custom-value" {
					t.Error("Environment CUSTOM_VAR not set correctly")
				}
			},
		},
		{
			name: "missing instance_id",
			configJSON: `{
				"subdomain": "myapp",
				"domain": "myapp.supabyoi.com",
				"postgres_password": "secret",
				"jwt_secret": "jwt"
			}`,
			wantErr: true,
			errMsg:  "instance_id is required",
		},
		{
			name: "missing subdomain",
			configJSON: `{
				"instance_id": "test-123",
				"domain": "myapp.supabyoi.com",
				"postgres_password": "secret",
				"jwt_secret": "jwt"
			}`,
			wantErr: true,
			errMsg:  "subdomain is required",
		},
		{
			name: "missing domain",
			configJSON: `{
				"instance_id": "test-123",
				"subdomain": "myapp",
				"postgres_password": "secret",
				"jwt_secret": "jwt"
			}`,
			wantErr: true,
			errMsg:  "domain is required",
		},
		{
			name: "missing postgres_password",
			configJSON: `{
				"instance_id": "test-123",
				"subdomain": "myapp",
				"domain": "myapp.supabyoi.com",
				"jwt_secret": "jwt"
			}`,
			wantErr: true,
			errMsg:  "postgres_password is required",
		},
		{
			name: "missing jwt_secret",
			configJSON: `{
				"instance_id": "test-123",
				"subdomain": "myapp",
				"domain": "myapp.supabyoi.com",
				"postgres_password": "secret"
			}`,
			wantErr: true,
			errMsg:  "jwt_secret is required",
		},
		{
			name:       "invalid JSON",
			configJSON: `{"instance_id": "test"`,
			wantErr:    true,
			errMsg:     "failed to parse config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file
			tmpfile, err := os.CreateTemp("", "config-*.json")
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(tmpfile.Name())

			if _, err := tmpfile.Write([]byte(tt.configJSON)); err != nil {
				t.Fatal(err)
			}
			if err := tmpfile.Close(); err != nil {
				t.Fatal(err)
			}

			// Test Load
			config, err := Load(tmpfile.Name())

			if tt.wantErr {
				if err == nil {
					t.Error("Load() expected error, got nil")
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Load() error = %v, want error containing %s", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Load() unexpected error = %v", err)
					return
				}
				if tt.validate != nil {
					tt.validate(t, config)
				}
			}
		})
	}
}

func TestLoadNonexistentFile(t *testing.T) {
	_, err := Load("/nonexistent/config.json")
	if err == nil {
		t.Error("Load() with nonexistent file should return error")
	}
	if !contains(err.Error(), "failed to read config file") {
		t.Errorf("Load() error = %v, want error containing 'failed to read config file'", err)
	}
}

func TestLoadEmptyFile(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "empty-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	_, err = Load(tmpfile.Name())
	if err == nil {
		t.Error("Load() with empty file should return error")
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findInString(s, substr))
}

func findInString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
