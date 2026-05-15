package enabletls

import (
	"fmt"
	"time"

	"github.com/vmkit-dev/vmkit-agent/internal/nginx"
	"github.com/vmkit-dev/vmkit-agent/internal/tls"
	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

// EnableTLS enables TLS for an existing instance by writing Origin CA certificates
// and reconfiguring Nginx to use HTTPS
func EnableTLS(config *types.EnableTLSConfig) types.EnableTLSResult {
	result := types.EnableTLSResult{
		Success:    true,
		InstanceID: config.InstanceID,
		Steps:      []types.StepResult{},
	}

	certDir := tls.DefaultCertDir

	// Step 1: Write certificate for main domain (wildcard covers all subdomains)
	step := executeStep("tls_write_main", func() error {
		return tls.WriteCertificate(config.Domain, certDir, config.CertPEM, config.KeyPEM)
	})
	result.Steps = append(result.Steps, step)
	if step.Status == "failed" {
		result.Success = false
		result.FailedStep = "tls_write_main"
		result.ErrorMessage = step.Message
		return result
	}

	// Step 2: Write same certificate for studio domain (wildcard covers it)
	if config.StudioDomain != "" {
		step = executeStep("tls_write_studio", func() error {
			return tls.WriteCertificate(config.StudioDomain, certDir, config.CertPEM, config.KeyPEM)
		})
		result.Steps = append(result.Steps, step)
		if step.Status == "failed" {
			result.Success = false
			result.FailedStep = "tls_write_studio"
			result.ErrorMessage = step.Message
			return result
		}
	}

	// Step 3: Reconfigure Nginx to use HTTPS
	step = executeStep("nginx_https_config", func() error {
		return configureNginxHTTPS(config)
	})
	result.Steps = append(result.Steps, step)
	if step.Status == "failed" {
		result.Success = false
		result.FailedStep = "nginx_https_config"
		result.ErrorMessage = step.Message
		return result
	}

	return result
}

// configureNginxHTTPS reconfigures Nginx site to use HTTPS
func configureNginxHTTPS(config *types.EnableTLSConfig) error {
	kongPort := config.KongPort
	if kongPort == 0 {
		kongPort = 8000
	}
	studioPort := config.StudioPort
	if studioPort == 0 {
		studioPort = 3000
	}

	// Re-render the studio htpasswd so the upgraded HTTPS site keeps basic-auth
	// in place. Without these the new server block ships unauthenticated — see
	// bead supabyoi-f8w0.
	if config.DashboardUsername == "" || config.DashboardPassword == "" {
		return fmt.Errorf("dashboard_username and dashboard_password are required to gate studio basic-auth")
	}
	htpasswdPath, err := nginx.WriteHtpasswd(config.Subdomain, config.DashboardUsername, config.DashboardPassword)
	if err != nil {
		return fmt.Errorf("write studio htpasswd: %w", err)
	}

	siteConfig := &nginx.SiteConfig{
		Subdomain:          config.Subdomain,
		Domain:             config.Domain,
		StudioDomain:       config.StudioDomain,
		KongPort:           kongPort,
		StudioPort:         studioPort,
		TLSEnabled:         true,
		CertPath:           fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem", config.Domain),
		KeyPath:            fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem", config.Domain),
		StudioCertPath:     fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem", config.StudioDomain),
		StudioKeyPath:      fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem", config.StudioDomain),
		StudioHtpasswdPath: htpasswdPath,
	}

	return nginx.CreateSiteConfig(siteConfig)
}

// executeStep executes a step and tracks its result
func executeStep(name string, fn func() error) types.StepResult {
	startTime := time.Now()
	step := types.StepResult{
		Name:      name,
		Status:    "in_progress",
		StartTime: startTime.Format(time.RFC3339),
	}

	err := fn()
	endTime := time.Now()
	step.EndTime = endTime.Format(time.RFC3339)
	step.Duration = endTime.Sub(startTime).String()

	if err != nil {
		step.Status = "failed"
		step.Message = err.Error()
	} else {
		step.Status = "completed"
		step.Message = fmt.Sprintf("%s completed successfully", name)
	}

	return step
}
