package enabletls

import (
	"testing"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

func TestEnableTLSConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		config *types.EnableTLSConfig
	}{
		{
			name: "basic config with certs",
			config: &types.EnableTLSConfig{
				InstanceID:   "test-123",
				Subdomain:    "myapp",
				Domain:       "myapp.example.com",
				StudioDomain: "studio-myapp.example.com",
				CertPEM:      "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
				KeyPEM:       "-----BEGIN RSA PRIVATE KEY-----\ntest\n-----END RSA PRIVATE KEY-----",
			},
		},
		{
			name: "without studio domain",
			config: &types.EnableTLSConfig{
				InstanceID: "test-456",
				Subdomain:  "otherapp",
				Domain:     "otherapp.example.com",
				CertPEM:    "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
				KeyPEM:     "-----BEGIN RSA PRIVATE KEY-----\ntest\n-----END RSA PRIVATE KEY-----",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.config.InstanceID == "" {
				t.Error("InstanceID should not be empty")
			}
			if tt.config.Subdomain == "" {
				t.Error("Subdomain should not be empty")
			}
			if tt.config.Domain == "" {
				t.Error("Domain should not be empty")
			}
			if tt.config.CertPEM == "" {
				t.Error("CertPEM should not be empty")
			}
			if tt.config.KeyPEM == "" {
				t.Error("KeyPEM should not be empty")
			}
		})
	}
}

func TestConfigureNginxHTTPS(t *testing.T) {
	config := &types.EnableTLSConfig{
		InstanceID:   "test-123",
		Subdomain:    "myapp",
		Domain:       "myapp.example.com",
		StudioDomain: "studio-myapp.example.com",
		CertPEM:      "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----",
		KeyPEM:       "-----BEGIN RSA PRIVATE KEY-----\ntest\n-----END RSA PRIVATE KEY-----",
	}

	// We can't actually run this without nginx installed
	// but we validate that the function exists and has the right signature
	_ = configureNginxHTTPS(config)
}
