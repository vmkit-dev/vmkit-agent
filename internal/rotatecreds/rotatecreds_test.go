package rotatecreds

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

func TestGetContainersToRestart(t *testing.T) {
	tests := []struct {
		name           string
		credentialType string
		expected       []string
	}{
		{
			name:           "postgres credentials only",
			credentialType: "postgres",
			expected:       []string{"postgres"},
		},
		{
			name:           "jwt credentials only",
			credentialType: "jwt",
			expected:       []string{"kong"},
		},
		{
			name:           "api_keys credentials only",
			credentialType: "api_keys",
			expected:       []string{"kong"},
		},
		{
			name:           "all credentials",
			credentialType: "all",
			expected:       []string{"postgres", "kong", "studio"},
		},
		{
			name:           "unknown type defaults to all",
			credentialType: "unknown",
			expected:       []string{"postgres", "kong", "studio"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getContainersToRestart(tt.credentialType)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d containers, got %d", len(tt.expected), len(result))
			}
			for i, container := range result {
				if container != tt.expected[i] {
					t.Errorf("expected container %s at index %d, got %s", tt.expected[i], i, container)
				}
			}
		})
	}
}

func TestUpdateEnvFile(t *testing.T) {
	// Create temporary test directory
	tmpDir, err := os.MkdirTemp("", "rotatecreds-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test .env file
	envFilePath := filepath.Join(tmpDir, ".env")
	initialContent := `# Supabase instance environment variables
INSTANCE_ID=test-123
SUBDOMAIN=test
DOMAIN=test.supabyoi.com
POSTGRES_PASSWORD=old_password
JWT_SECRET=old_jwt_secret
PUBLISHABLE_KEY=old_anon_key
SECRET_KEY=old_service_key
KONG_PORT=8000
POSTGRES_PORT=5432
STUDIO_PORT=3000
`
	if err := os.WriteFile(envFilePath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("failed to write test .env file: %v", err)
	}

	tests := []struct {
		name           string
		config         *types.RotateCredentialsConfig
		expectedVars   map[string]string
		unexpectedVars map[string]string
	}{
		{
			name: "rotate postgres password only",
			config: &types.RotateCredentialsConfig{
				InstanceID:       "test-123",
				CredentialType:   "postgres",
				PostgresPassword: "new_postgres_password",
			},
			expectedVars: map[string]string{
				"POSTGRES_PASSWORD": "new_postgres_password",
				"JWT_SECRET":        "old_jwt_secret", // Should not change
			},
		},
		{
			name: "rotate jwt secret only",
			config: &types.RotateCredentialsConfig{
				InstanceID:     "test-123",
				CredentialType: "jwt",
				JWTSecret:      "new_jwt_secret",
			},
			expectedVars: map[string]string{
				"JWT_SECRET":        "new_jwt_secret",
				"POSTGRES_PASSWORD": "old_password", // Should not change
			},
		},
		{
			name: "rotate api keys only",
			config: &types.RotateCredentialsConfig{
				InstanceID:     "test-123",
				CredentialType: "api_keys",
				PublishableKey: "new_anon_key",
				SecretKey:      "new_service_key",
			},
			expectedVars: map[string]string{
				"PUBLISHABLE_KEY":          "new_anon_key",
				"SECRET_KEY":  "new_service_key",
				"POSTGRES_PASSWORD": "old_password", // Should not change
			},
		},
		{
			name: "rotate all credentials",
			config: &types.RotateCredentialsConfig{
				InstanceID:       "test-123",
				CredentialType:   "all",
				PostgresPassword: "all_new_password",
				JWTSecret:        "all_new_jwt",
				PublishableKey:   "all_new_anon",
				SecretKey:        "all_new_service",
			},
			expectedVars: map[string]string{
				"POSTGRES_PASSWORD": "all_new_password",
				"JWT_SECRET":        "all_new_jwt",
				"PUBLISHABLE_KEY":          "all_new_anon",
				"SECRET_KEY":  "all_new_service",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset .env file before each test
			if err := os.WriteFile(envFilePath, []byte(initialContent), 0644); err != nil {
				t.Fatalf("failed to reset .env file: %v", err)
			}

			// Update env file
			if err := updateEnvFile(envFilePath, tt.config); err != nil {
				t.Fatalf("updateEnvFile failed: %v", err)
			}

			// Read updated file
			content, err := os.ReadFile(envFilePath)
			if err != nil {
				t.Fatalf("failed to read updated .env file: %v", err)
			}

			contentStr := string(content)

			// Verify expected variables are present with correct values
			for key, expectedValue := range tt.expectedVars {
				expectedLine := key + "=" + expectedValue
				if !contains(contentStr, expectedLine) {
					t.Errorf("expected to find '%s' in .env file, but it was not found or had wrong value", expectedLine)
				}
			}

			// Verify instance metadata is preserved
			if !contains(contentStr, "INSTANCE_ID=test-123") {
				t.Error("INSTANCE_ID should be preserved")
			}
			if !contains(contentStr, "SUBDOMAIN=test") {
				t.Error("SUBDOMAIN should be preserved")
			}
		})
	}
}

func TestUpdateEnvFileUnknownType(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rotatecreds-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	envFilePath := filepath.Join(tmpDir, ".env")
	initialContent := `INSTANCE_ID=test-123
POSTGRES_PASSWORD=old_password
`
	if err := os.WriteFile(envFilePath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("failed to write test .env file: %v", err)
	}

	config := &types.RotateCredentialsConfig{
		InstanceID:       "test-123",
		CredentialType:   "unknown_type",
		PostgresPassword: "new_password",
	}

	err = updateEnvFile(envFilePath, config)
	if err == nil {
		t.Error("expected error for unknown credential type, got nil")
	}
	if err != nil && !contains(err.Error(), "unknown credential type") {
		t.Errorf("expected 'unknown credential type' error, got: %v", err)
	}
}

func TestRotateCredentialsConfig(t *testing.T) {
	// Test that the config struct can be properly initialized
	config := &types.RotateCredentialsConfig{
		InstanceID:       "test-instance",
		CredentialType:   "all",
		PostgresPassword: "new_pass",
		JWTSecret:        "new_jwt",
		PublishableKey:   "new_anon",
		SecretKey:        "new_service",
		BaseDir:          "/opt/supabase",
	}

	if config.InstanceID != "test-instance" {
		t.Error("InstanceID not set correctly")
	}
	if config.CredentialType != "all" {
		t.Error("CredentialType not set correctly")
	}
	if config.BaseDir != "/opt/supabase" {
		t.Error("BaseDir not set correctly")
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
