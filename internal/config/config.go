package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

// Load reads and parses the deployment configuration from a file
func Load(path string) (*types.DeployConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config types.DeployConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Validate required fields
	if config.InstanceID == "" {
		return nil, fmt.Errorf("instance_id is required")
	}
	if config.Subdomain == "" {
		return nil, fmt.Errorf("subdomain is required")
	}
	if config.Domain == "" {
		return nil, fmt.Errorf("domain is required")
	}
	if config.PostgresPassword == "" {
		return nil, fmt.Errorf("postgres_password is required")
	}
	if config.JWTSecret == "" {
		return nil, fmt.Errorf("jwt_secret is required")
	}

	// Set defaults
	if config.BaseDir == "" {
		config.BaseDir = "/opt/supabase"
	}
	if config.KongPort == 0 {
		config.KongPort = 8000
	}
	if config.PostgresPort == 0 {
		config.PostgresPort = 5432
	}
	if config.StudioPort == 0 {
		config.StudioPort = 3000
	}

	return &config, nil
}
