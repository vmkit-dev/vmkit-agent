package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vmkit-dev/vmkit-agent/internal/compose"
	"github.com/vmkit-dev/vmkit-agent/internal/docker"
	"github.com/vmkit-dev/vmkit-agent/internal/files"
	"github.com/vmkit-dev/vmkit-agent/internal/health"
	"github.com/vmkit-dev/vmkit-agent/internal/nginx"
	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

const (
	// DefaultBaseDir is the default base directory for Supabase instances
	DefaultBaseDir = "/opt/supabase"
)

// Deploy orchestrates the deployment of a Supabase instance
func Deploy(config *types.DeployConfig) *types.DeployResult {
	result := &types.DeployResult{
		Success:    false,
		InstanceID: config.InstanceID,
		Steps:      make([]types.StepResult, 0),
	}

	// Use default base dir if not specified
	if config.BaseDir == "" {
		config.BaseDir = DefaultBaseDir
	}

	instanceDir := filepath.Join(config.BaseDir, config.Subdomain)

	// Step 1: Ensure Docker is installed
	step := executeStep("docker_install", func() error {
		if docker.IsInstalled() {
			return nil // Already installed, idempotent
		}
		return docker.Install(config.VMUser)
	})
	result.Steps = append(result.Steps, step)
	if step.Status == "failed" {
		result.FailedStep = step.Name
		result.ErrorMessage = step.Message
		return result
	}

	// Step 2: Ensure Nginx is installed and configured
	step = executeStep("nginx_install", func() error {
		return nginx.Install(config.VMUser)
	})
	result.Steps = append(result.Steps, step)
	if step.Status == "failed" {
		result.FailedStep = step.Name
		result.ErrorMessage = step.Message
		return result
	}

	// Step 3: Create directory structure and write configuration files
	step = executeStep("directory_and_config_setup", func() error {
		// Create file manager
		fm := files.New(config.Subdomain, config.BaseDir, config.VMUser)

		// Create directory structure
		if err := fm.CreateDirectoryStructure(); err != nil {
			return fmt.Errorf("failed to create directory structure: %w", err)
		}

		// Write all configuration files
		if err := fm.WriteAllConfigFiles(config); err != nil {
			return fmt.Errorf("failed to write config files: %w", err)
		}

		// Validate that all files were created correctly
		if err := fm.Validate(); err != nil {
			return fmt.Errorf("config validation failed: %w", err)
		}

		return nil
	})
	result.Steps = append(result.Steps, step)
	if step.Status == "failed" {
		result.FailedStep = step.Name
		result.ErrorMessage = step.Message
		return result
	}

	// Step 5: Pull Docker images and start containers (with role setup)
	step = executeStep("docker_compose", func() error {
		return runDockerCompose(instanceDir, config.PostgresPassword, postgresService(config))
	})
	result.Steps = append(result.Steps, step)
	if step.Status == "failed" {
		result.FailedStep = step.Name
		result.ErrorMessage = step.Message
		return result
	}

	// Step 6: Wait for health checks
	step = executeStep("health_check", func() error {
		return waitForHealthy(config, 60*time.Second)
	})
	result.Steps = append(result.Steps, step)
	if step.Status == "failed" {
		result.FailedStep = step.Name
		result.ErrorMessage = step.Message
		return result
	}

	// Step 7: Configure Nginx site (HTTP initially)
	step = executeStep("nginx_config", func() error {
		return configureNginxSite(config, false)
	})
	result.Steps = append(result.Steps, step)
	if step.Status == "failed" {
		result.FailedStep = step.Name
		result.ErrorMessage = step.Message
		return result
	}

	// Note: TLS provisioning is handled separately via the enable-tls command
	// after DNS records are created. The deploy command only sets up HTTP.

	result.Success = true
	return result
}


// postgresService returns the docker compose service name hosting postgres.
// Legacy supabyoi-authored compose (v1.24.0) uses "postgres"; upstream
// v1.26.04 uses "db". Backend populates config.PostgresContainer — when empty
// we fall back to the legacy default for back-compat.
func postgresService(config *types.DeployConfig) string {
	if config.PostgresContainer != "" {
		return config.PostgresContainer
	}
	return "postgres"
}

// runDockerCompose pulls images, starts containers, sets up DB roles, and waits for healthy state
func runDockerCompose(instanceDir string, postgresPassword string, pgService string) error {
	instanceID := filepath.Base(instanceDir)
	baseDir := filepath.Dir(instanceDir)

	c := compose.New(instanceID, baseDir)
	ctx := context.Background()

	// Tear down stale containers from previous deployment attempts
	_ = c.Down(ctx)

	// Remove postgres data volume so init scripts run fresh on next startup.
	// The Supabase postgres image only runs docker-entrypoint-initdb.d scripts
	// when the data directory is empty. Stale data from failed deployments
	// prevents role creation (authenticator, etc.).
	dbDataDir := filepath.Join(instanceDir, "volumes", "db", "data")
	_ = os.RemoveAll(dbDataDir)

	if err := c.Validate(ctx); err != nil {
		return fmt.Errorf("docker-compose.yml validation failed: %w", err)
	}

	if err := c.Pull(ctx); err != nil {
		return fmt.Errorf("docker compose pull failed: %w", err)
	}

	// Phase 1: start postgres alone and block on our own healthcheck.
	// supabyoi-a49s: starting the full stack via `docker compose up -d` races
	// against compose v2's internal dep-wait — dependents (auth/kong/rest)
	// give up on postgres's `service_healthy` condition before the upstream
	// init scripts finish, and compose returns exit 1 even though postgres
	// becomes healthy moments later. Bringing postgres up solo and waiting
	// with our 300s deadline (see waitForPostgres) dodges that race entirely.
	if err := c.UpService(ctx, pgService); err != nil {
		return fmt.Errorf("docker compose up %s failed: %w", pgService, err)
	}
	if err := waitForPostgres(c, ctx, pgService, 300*time.Second); err != nil {
		return fmt.Errorf("postgres failed to start: %w", err)
	}

	// Phase 2: bring up the rest of the stack. postgres is already healthy,
	// so dependents' `service_healthy` waits resolve immediately.
	if err := c.Up(ctx); err != nil {
		return fmt.Errorf("docker compose up failed: %w", err)
	}

	// Set up database roles (idempotent - safe if init script already ran)
	if err := setupDatabaseRoles(instanceDir, postgresPassword, pgService); err != nil {
		return fmt.Errorf("database role setup failed: %w", err)
	}

	// Bring services back up (restarts any that crashed due to wrong passwords)
	// Using Up instead of Restart because docker compose restart causes
	// containers to disappear from "docker compose ps" output, breaking WaitForContainers.
	// Up -d is idempotent and recreates exited containers without the visibility gap.
	if err := c.Up(ctx); err != nil {
		return fmt.Errorf("docker compose up (post-role-setup) failed: %w", err)
	}
	time.Sleep(10 * time.Second)

	// Wait for all containers to be running (180s for services to restart with valid roles)
	if err := c.WaitForContainers(ctx, 180*time.Second); err != nil {
		// Collect diagnostic info about which containers failed
		statuses, _ := c.GetContainerStatus(ctx)
		var all []string
		for _, s := range statuses {
			all = append(all, fmt.Sprintf("%s=%s", s.Name, s.State))
		}
		// Get logs from failing containers
		logs, _ := c.Logs(ctx, 10)
		return fmt.Errorf("containers failed to start (%d total): [%s]; logs: %s",
			len(statuses), strings.Join(all, ", "), truncate(logs, 1000))
	}

	return nil
}

// waitForHealthy waits for services to be healthy using comprehensive health checks
func waitForHealthy(deployConfig *types.DeployConfig, timeout time.Duration) error {
	checkCfg := health.DefaultCheckConfig(deployConfig.Subdomain)
	checkCfg.Timeout = timeout
	// Use instance-allocated ports instead of hardcoded defaults.
	// With random port allocation (supabyoi-ioz5), kong/studio are no
	// longer on 8000/3000 — health probes to localhost must use the
	// actual published host ports.
	if deployConfig.KongPort > 0 {
		checkCfg.KongPort = deployConfig.KongPort
	}
	if deployConfig.StudioPort > 0 {
		checkCfg.StudioPort = deployConfig.StudioPort
	}
	// Propagate backend-supplied service list when present (v1.26.04+).
	// Empty list falls through to the legacy triad inside runHealthChecks.
	if len(deployConfig.Services) > 0 {
		checkCfg.Services = deployConfig.Services
	}
	if deployConfig.PostgresContainer != "" {
		checkCfg.PostgresContainer = deployConfig.PostgresContainer
	}

	result, err := health.CheckAll(checkCfg)
	if err != nil {
		// Include detailed service status in error message
		var unhealthyServices []string
		for _, svc := range result.Services {
			if !svc.Healthy {
				unhealthyServices = append(unhealthyServices, fmt.Sprintf("%s: %s", svc.Name, svc.Message))
			}
		}
		if len(unhealthyServices) > 0 {
			return fmt.Errorf("health checks failed: %s", strings.Join(unhealthyServices, "; "))
		}
		return err
	}

	return nil
}

// configureNginxSite configures Nginx site for the instance
func configureNginxSite(config *types.DeployConfig, tlsEnabled bool) error {
	// Use domain and studio_domain from config directly (already fully qualified)
	domain := config.Domain
	studioDomain := config.StudioDomain
	if studioDomain == "" {
		studioDomain = fmt.Sprintf("studio-%s", domain)
	}

	kongPort := config.KongPort
	if kongPort == 0 {
		kongPort = 8000
	}
	studioPort := config.StudioPort
	if studioPort == 0 {
		studioPort = 3000
	}

	// Studio is published on its own subdomain. Without basic auth this gives
	// anyone with the URL full Postgres-meta admin access — see bead supabyoi-f8w0.
	// Hard-fail rather than silently deploy an unauthenticated studio.
	if config.DashboardUsername == "" || config.DashboardPassword == "" {
		return fmt.Errorf("dashboard_username and dashboard_password are required to gate studio basic-auth")
	}
	htpasswdPath, err := nginx.WriteHtpasswd(config.Subdomain, config.DashboardUsername, config.DashboardPassword)
	if err != nil {
		return fmt.Errorf("write studio htpasswd: %w", err)
	}

	siteConfig := &nginx.SiteConfig{
		Subdomain:          config.Subdomain,
		Domain:             domain,
		StudioDomain:       studioDomain,
		KongPort:           kongPort,
		StudioPort:         studioPort,
		TLSEnabled:         tlsEnabled,
		StudioHtpasswdPath: htpasswdPath,
	}

	if tlsEnabled {
		siteConfig.CertPath = fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem", domain)
		siteConfig.KeyPath = fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem", domain)
		siteConfig.StudioCertPath = fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem", studioDomain)
		siteConfig.StudioKeyPath = fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem", studioDomain)
	}

	return nginx.CreateSiteConfig(siteConfig)
}


// waitForPostgres waits for postgres to accept connections AND for the Supabase
// init scripts to finish creating roles. pg_isready alone is not sufficient because
// the Supabase postgres image runs init scripts after postgres starts accepting
// connections, and those scripts create the roles we need (authenticator, etc.).
func waitForPostgres(c *compose.Compose, ctx context.Context, pgService string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	// Phase 1: wait for postgres to accept connections
	for time.Now().Before(deadline) {
		_, err := c.Exec(ctx, pgService, []string{"pg_isready", "-U", "postgres"})
		if err == nil {
			break
		}
		time.Sleep(3 * time.Second)
	}
	if time.Now().After(deadline) {
		return fmt.Errorf("postgres not accepting connections within %v", timeout)
	}

	// Phase 2: wait for Supabase init scripts to create the authenticator role
	for time.Now().Before(deadline) {
		out, err := c.Exec(ctx, pgService, []string{
			"psql", "-U", "postgres", "-d", "postgres", "-tAc",
			"SELECT 1 FROM pg_roles WHERE rolname='authenticator'",
		})
		if err == nil && strings.TrimSpace(out) == "1" {
			return nil
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("supabase roles not created within %v", timeout)
}

// setupDatabaseRoles creates and configures Supabase service roles
// This is idempotent - safe to run multiple times
func setupDatabaseRoles(instanceDir string, password string, pgService string) error {
	instanceID := filepath.Base(instanceDir)
	baseDir := filepath.Dir(instanceDir)

	c := compose.New(instanceID, baseDir)
	ctx := context.Background()

	// Only set passwords for existing roles - the Supabase postgres image
	// handles role creation, schema creation, and extension installation
	sql := fmt.Sprintf(`
ALTER USER authenticator WITH PASSWORD '%s';
ALTER USER supabase_auth_admin WITH PASSWORD '%s';
ALTER USER supabase_storage_admin WITH PASSWORD '%s';
ALTER USER supabase_admin WITH PASSWORD '%s';
GRANT anon TO authenticator;
GRANT authenticated TO authenticator;
GRANT service_role TO authenticator;
`, password, password, password, password)

	// Must use supabase_admin (superuser) because authenticator is a reserved role.
	// Need PGPASSWORD because supabase_admin requires password auth on local socket.
	_, err := c.ExecWithEnv(ctx, pgService, []string{"psql", "-U", "supabase_admin", "-d", "postgres", "-c", sql}, map[string]string{"PGPASSWORD": password})
	return err
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// stepProgress is the JSON structure written to stderr for real-time progress.
type stepProgress struct {
	Type     string `json:"type"`
	Step     string `json:"step"`
	Status   string `json:"status"`
	Time     string `json:"time"`
	Duration string `json:"duration,omitempty"`
	Message  string `json:"message,omitempty"`
}

// emitProgress writes a JSON progress line to stderr.
// Errors are silently ignored — progress is best-effort.
func emitProgress(p stepProgress) {
	data, err := json.Marshal(p)
	if err != nil {
		return
	}
	data = append(data, '\n')
	os.Stderr.Write(data)
}

// executeStep executes a deployment step and tracks its result.
// It emits JSON progress lines to stderr before and after execution.
func executeStep(name string, fn func() error) types.StepResult {
	startTime := time.Now()
	step := types.StepResult{
		Name:      name,
		Status:    "in_progress",
		StartTime: startTime.Format(time.RFC3339),
	}

	// Emit start progress
	emitProgress(stepProgress{
		Type:   "step_progress",
		Step:   name,
		Status: "in_progress",
		Time:   startTime.Format(time.RFC3339),
	})

	err := fn()
	endTime := time.Now()
	step.EndTime = endTime.Format(time.RFC3339)
	step.Duration = endTime.Sub(startTime).String()

	if err != nil {
		step.Status = "failed"
		step.Message = err.Error()
		emitProgress(stepProgress{
			Type:     "step_progress",
			Step:     name,
			Status:   "failed",
			Time:     endTime.Format(time.RFC3339),
			Duration: step.Duration,
			Message:  err.Error(),
		})
	} else {
		step.Status = "completed"
		step.Message = fmt.Sprintf("%s completed successfully", name)
		emitProgress(stepProgress{
			Type:     "step_progress",
			Step:     name,
			Status:   "completed",
			Time:     endTime.Format(time.RFC3339),
			Duration: step.Duration,
		})
	}

	return step
}
