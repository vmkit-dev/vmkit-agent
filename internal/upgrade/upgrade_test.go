package upgrade

import (
	"testing"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

func TestValidateVersionUpgrade(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion string
		targetVersion  string
		shouldFail     bool
	}{
		{
			name:           "valid upgrade",
			currentVersion: "1.23.0",
			targetVersion:  "1.24.0",
			shouldFail:     false,
		},
		{
			name:           "downgrade not allowed",
			currentVersion: "1.24.0",
			targetVersion:  "1.23.0",
			shouldFail:     true,
		},
		{
			name:           "same version",
			currentVersion: "1.23.0",
			targetVersion:  "1.23.0",
			shouldFail:     true,
		},
		{
			name:           "unknown current version",
			currentVersion: "1.99.0",
			targetVersion:  "1.24.0",
			shouldFail:     true,
		},
		{
			name:           "unknown target version",
			currentVersion: "1.23.0",
			targetVersion:  "1.99.0",
			shouldFail:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateVersionUpgrade(tt.currentVersion, tt.targetVersion)

			if tt.shouldFail {
				if err == nil {
					t.Error("Expected validation to fail, but it succeeded")
				}
			} else {
				if err != nil {
					t.Errorf("Expected validation to succeed, but got error: %v", err)
				}
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name     string
		v1       string
		v2       string
		expected int
	}{
		{
			name:     "v1 greater than v2",
			v1:       "1.24.0",
			v2:       "1.23.0",
			expected: 1,
		},
		{
			name:     "v1 less than v2",
			v1:       "1.23.0",
			v2:       "1.24.0",
			expected: -1,
		},
		{
			name:     "v1 equals v2",
			v1:       "1.23.0",
			v2:       "1.23.0",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compareVersions(tt.v1, tt.v2)
			if result != tt.expected {
				t.Errorf("compareVersions(%s, %s) = %d, want %d", tt.v1, tt.v2, result, tt.expected)
			}
		})
	}
}

func TestReplaceImageVersion(t *testing.T) {
	dockerCompose := `version: '3.8'

services:
  postgres:
    image: supabase/postgres:15.1.0.117
    container_name: test-postgres
    restart: unless-stopped

  kong:
    image: kong:2.8.1
    container_name: test-kong
    restart: unless-stopped`

	tests := []struct {
		name        string
		serviceName string
		newImage    string
		expected    string
	}{
		{
			name:        "replace postgres image",
			serviceName: "postgres",
			newImage:    "supabase/postgres:15.1.1.118",
			expected:    "supabase/postgres:15.1.1.118",
		},
		{
			name:        "replace kong image",
			serviceName: "kong",
			newImage:    "kong:2.8.2",
			expected:    "kong:2.8.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := replaceImageVersion(dockerCompose, tt.serviceName, tt.newImage)
			if !containsImage(result, tt.expected) {
				t.Errorf("replaceImageVersion did not update image to %s", tt.expected)
			}
		})
	}
}

func containsImage(content, image string) bool {
	return len(content) > 0 && len(image) > 0 && containsSubstring(content, image)
}

func containsSubstring(content, substring string) bool {
	return len(content) >= len(substring) && findSubstring(content, substring)
}

func findSubstring(content, substring string) bool {
	for i := 0; i <= len(content)-len(substring); i++ {
		if content[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}

func TestUpgradeValidation(t *testing.T) {
	// Test upgrade validation with minimal config
	config := types.UpgradeConfig{
		InstanceID:          "test-instance",
		Subdomain:           "nonexistent",
		BaseDir:             "/tmp/test-upgrade",
		CurrentVersion:      "1.23.0",
		TargetVersion:       "1.24.0",
		BackupBeforeUpgrade: false,
		RollbackOnFailure:   false,
	}

	result := Upgrade(config)

	// Should fail because instance directory doesn't exist
	if result.Success {
		t.Error("Expected upgrade to fail for nonexistent instance, but it succeeded")
	}

	if result.FailedStep == "" {
		t.Error("Expected failed_step to be set")
	}

	if result.ErrorMessage == "" {
		t.Error("Expected error_message to be set")
	}
}
