package health

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// ServiceHealth represents the health status of a service
type ServiceHealth struct {
	Name      string `json:"name"`
	Healthy   bool   `json:"healthy"`
	Message   string `json:"message,omitempty"`
	Latency   string `json:"latency,omitempty"`
	CheckedAt string `json:"checked_at"`
}

// HealthCheckResult contains the results of all health checks
type HealthCheckResult struct {
	AllHealthy bool            `json:"all_healthy"`
	Services   []ServiceHealth `json:"services"`
	Duration   string          `json:"duration"`
}

// CheckConfig contains configuration for health checks
type CheckConfig struct {
	Subdomain      string
	Timeout        time.Duration
	RetryInterval  time.Duration
	MaxRetries     int
	PostgresPort   int
	KongPort       int
	StudioPort     int
	UseExponential bool // Use exponential backoff
	// Services lists the container-name *suffixes* to verify. Each entry
	// becomes `{Subdomain}-{suffix}`. Empty = fall back to legacy triad
	// [postgres, kong, studio] for backwards compatibility with 1.24.0
	// supabyoi-authored compose templates.
	Services []string
	// PostgresContainer is the container-name suffix used for the
	// postgres `docker exec pg_isready` check. Defaults to "postgres"
	// when empty. v1.26.04 upstream uses "db".
	PostgresContainer string
}

// DefaultCheckConfig returns a default health check configuration
func DefaultCheckConfig(subdomain string) *CheckConfig {
	return &CheckConfig{
		Subdomain:         subdomain,
		Timeout:           60 * time.Second,
		RetryInterval:     2 * time.Second,
		MaxRetries:        30,
		PostgresPort:      5432,
		KongPort:          8000,
		StudioPort:        3000,
		UseExponential:    true,
		Services:          nil, // legacy fallback
		PostgresContainer: "postgres",
	}
}

// legacyServiceSuffixes is the hardcoded triad used when no Services
// list is supplied — matches 1.24.0 supabyoi-authored compose layouts
// which only name postgres, kong, and studio containers.
var legacyServiceSuffixes = []string{"postgres", "kong", "studio"}

// CheckAll performs all health checks with retries and exponential backoff
func CheckAll(config *CheckConfig) (*HealthCheckResult, error) {
	if config == nil {
		config = DefaultCheckConfig("")
	}

	startTime := time.Now()
	deadline := startTime.Add(config.Timeout)
	attempt := 0

	for time.Now().Before(deadline) && attempt < config.MaxRetries {
		attempt++

		// Run all health checks
		result := runHealthChecks(config)

		if result.AllHealthy {
			result.Duration = time.Since(startTime).String()
			return result, nil
		}

		// Calculate wait time (exponential backoff if enabled)
		waitTime := config.RetryInterval
		if config.UseExponential {
			waitTime = time.Duration(1<<uint(min(attempt-1, 6))) * time.Second
			if waitTime > 30*time.Second {
				waitTime = 30 * time.Second
			}
		}

		// Don't wait on the last attempt
		if attempt < config.MaxRetries && time.Now().Add(waitTime).Before(deadline) {
			time.Sleep(waitTime)
		}
	}

	// Final check to get detailed status
	result := runHealthChecks(config)
	result.Duration = time.Since(startTime).String()

	if !result.AllHealthy {
		return result, fmt.Errorf("health checks failed after %d attempts (%s)", attempt, result.Duration)
	}

	return result, nil
}

// runHealthChecks performs all health checks once
func runHealthChecks(config *CheckConfig) *HealthCheckResult {
	result := &HealthCheckResult{
		AllHealthy: true,
		Services:   make([]ServiceHealth, 0),
	}

	// Check 1: Container states
	suffixes := config.Services
	if len(suffixes) == 0 {
		suffixes = legacyServiceSuffixes
	}
	containerHealth := checkContainerStates(config.Subdomain, suffixes)
	result.Services = append(result.Services, containerHealth...)
	for _, svc := range containerHealth {
		if !svc.Healthy {
			result.AllHealthy = false
		}
	}

	// Check 2: PostgreSQL
	pgSuffix := config.PostgresContainer
	if pgSuffix == "" {
		pgSuffix = "postgres"
	}
	pgHealth := checkPostgres(config.Subdomain, pgSuffix, config.PostgresPort)
	result.Services = append(result.Services, pgHealth)
	if !pgHealth.Healthy {
		result.AllHealthy = false
	}

	// Check 3: Kong Gateway
	kongHealth := checkKongGateway(config.KongPort)
	result.Services = append(result.Services, kongHealth)
	if !kongHealth.Healthy {
		result.AllHealthy = false
	}

	// Check 4: Studio (optional, don't fail deployment if it's not ready)
	studioHealth := checkStudio(config.StudioPort)
	result.Services = append(result.Services, studioHealth)
	// Studio is optional, so we don't fail on it

	return result
}

// checkContainerStates verifies all expected containers are running.
// Each suffix is prefixed with `{subdomain}-` to form the container
// name, matching how compose_renderer rewrites upstream hardcoded
// names for multi-tenant isolation.
func checkContainerStates(subdomain string, suffixes []string) []ServiceHealth {
	services := []ServiceHealth{}

	containers := make([]string, 0, len(suffixes))
	for _, suffix := range suffixes {
		// Realtime needs the `realtime-dev.` prefix preserved for tenant
		// parsing — the realtime service extracts tenant ID before the first dot.
		if suffix == "realtime" {
			containers = append(containers, fmt.Sprintf("realtime-dev.%s-realtime", subdomain))
		} else {
			containers = append(containers, fmt.Sprintf("%s-%s", subdomain, suffix))
		}
	}

	for _, container := range containers {
		health := ServiceHealth{
			Name:      container,
			CheckedAt: time.Now().Format(time.RFC3339),
		}

		// Check if container is running
		cmd := exec.Command("sudo", "docker", "inspect", "-f", "{{.State.Running}}", container)
		output, err := cmd.Output()

		if err != nil {
			health.Healthy = false
			health.Message = fmt.Sprintf("Container not found or not accessible: %v", err)
			services = append(services, health)
			continue
		}

		running := strings.TrimSpace(string(output))
		if running == "true" {
			health.Healthy = true
			health.Message = "Container is running"
		} else {
			health.Healthy = false
			health.Message = "Container is not running"
		}

		services = append(services, health)
	}

	return services
}

// checkPostgres verifies PostgreSQL is accepting connections.
// containerSuffix is "postgres" for legacy 1.24.0 compose layouts and
// "db" for upstream v1.26.04 layouts.
func checkPostgres(subdomain, containerSuffix string, port int) ServiceHealth {
	health := ServiceHealth{
		Name:      "postgresql",
		CheckedAt: time.Now().Format(time.RFC3339),
	}

	startTime := time.Now()

	// Use docker exec to run pg_isready inside the container
	// This is more reliable than trying to connect from the host.
	containerName := fmt.Sprintf("%s-%s", subdomain, containerSuffix)
	cmd := exec.Command("sudo", "docker", "exec", "-i",
		containerName,
		"pg_isready", "-h", "localhost", "-p", fmt.Sprintf("%d", port))

	output, err := cmd.CombinedOutput()
	health.Latency = time.Since(startTime).String()

	if err != nil {
		health.Healthy = false
		health.Message = fmt.Sprintf("pg_isready failed: %s", string(output))
		return health
	}

	outputStr := string(output)
	if strings.Contains(outputStr, "accepting connections") {
		health.Healthy = true
		health.Message = "PostgreSQL is accepting connections"
	} else {
		health.Healthy = false
		health.Message = outputStr
	}

	return health
}

// checkKongGateway verifies Kong is responding
func checkKongGateway(port int) ServiceHealth {
	health := ServiceHealth{
		Name:      "kong-gateway",
		CheckedAt: time.Now().Format(time.RFC3339),
	}

	startTime := time.Now()

	// Try to connect to Kong's status endpoint
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	url := fmt.Sprintf("http://localhost:%d/", port)
	resp, err := client.Get(url)

	health.Latency = time.Since(startTime).String()

	if err != nil {
		health.Healthy = false
		health.Message = fmt.Sprintf("Kong not responding: %v", err)
		return health
	}
	defer resp.Body.Close()

	// Kong should respond with some HTTP status (even 404 is fine, means it's up)
	if resp.StatusCode > 0 {
		health.Healthy = true
		health.Message = fmt.Sprintf("Kong responding (HTTP %d)", resp.StatusCode)
	} else {
		health.Healthy = false
		health.Message = "Kong returned invalid response"
	}

	return health
}

// checkStudio verifies Studio is responding
func checkStudio(port int) ServiceHealth {
	health := ServiceHealth{
		Name:      "studio",
		CheckedAt: time.Now().Format(time.RFC3339),
	}

	startTime := time.Now()

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	url := fmt.Sprintf("http://localhost:%d/", port)
	resp, err := client.Get(url)

	health.Latency = time.Since(startTime).String()

	if err != nil {
		health.Healthy = false
		health.Message = fmt.Sprintf("Studio not responding: %v", err)
		return health
	}
	defer resp.Body.Close()

	if resp.StatusCode > 0 && resp.StatusCode < 500 {
		health.Healthy = true
		health.Message = fmt.Sprintf("Studio responding (HTTP %d)", resp.StatusCode)
	} else {
		health.Healthy = false
		health.Message = fmt.Sprintf("Studio error (HTTP %d)", resp.StatusCode)
	}

	return health
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
