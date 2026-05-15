package health

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

// CheckEnhanced performs enhanced health checks based on the specified level
func CheckEnhanced(config *types.EnhancedHealthCheckConfig) (*types.EnhancedHealthCheckResult, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	startTime := time.Now()
	result := &types.EnhancedHealthCheckResult{
		InstanceID:    config.InstanceID,
		Timestamp:     time.Now().Format(time.RFC3339),
		OverallStatus: "healthy",
		Containers:    []types.EnhancedContainerStatus{},
		Services:      []types.ServiceDiagnostics{},
		Checks:        []types.HealthCheck{},
		Level:         string(config.Level),
	}

	// Step 1: Check container status (all levels)
	containers, err := checkContainerStatus(config.Subdomain)
	if err != nil {
		result.OverallStatus = "unhealthy"
		result.Checks = append(result.Checks, types.HealthCheck{
			Name:    "container_check",
			Status:  "failed",
			Message: err.Error(),
		})
	} else {
		result.Containers = containers
		for _, c := range containers {
			if c.Status != "running" || (c.Health != "" && c.Health != "healthy") {
				result.OverallStatus = "degraded"
			}
		}
	}

	// Step 2: Basic connectivity checks (all levels)
	if err := performBasicChecks(config, result); err != nil {
		if result.OverallStatus == "healthy" {
			result.OverallStatus = "degraded"
		}
	}

	// Step 3: Deep diagnostics (deep, full levels)
	if config.Level == types.HealthCheckDeep || config.Level == types.HealthCheckFull {
		if err := performDeepChecks(config, result); err != nil {
			result.OverallStatus = "degraded"
		}
	}

	// Step 4: Metrics collection (metrics, full levels)
	if config.Level == types.HealthCheckMetrics || config.Level == types.HealthCheckFull {
		if err := collectMetrics(config, result); err != nil {
			// Metrics collection failure is not critical
			result.Checks = append(result.Checks, types.HealthCheck{
				Name:    "metrics_collection",
				Status:  "warning",
				Message: fmt.Sprintf("Metrics collection partially failed: %v", err),
			})
		}
	}

	// Step 5: Service-specific diagnostics (services, full levels)
	if config.Level == types.HealthCheckServices || config.Level == types.HealthCheckFull {
		if err := performServiceDiagnostics(config, result); err != nil {
			result.OverallStatus = "degraded"
		}
	}

	result.Duration = time.Since(startTime).String()
	return result, nil
}

// checkContainerStatus returns detailed container status information
func checkContainerStatus(subdomain string) ([]types.EnhancedContainerStatus, error) {
	containers := []types.EnhancedContainerStatus{}

	// Expected containers
	containerNames := []string{
		fmt.Sprintf("%s-postgres", subdomain),
		fmt.Sprintf("%s-kong", subdomain),
		fmt.Sprintf("%s-studio", subdomain),
		fmt.Sprintf("%s-auth", subdomain),
		fmt.Sprintf("%s-rest", subdomain),
		fmt.Sprintf("%s-realtime", subdomain),
		fmt.Sprintf("%s-storage", subdomain),
	}

	for _, name := range containerNames {
		status := checkSingleContainer(name)
		containers = append(containers, status)
	}

	return containers, nil
}

// checkSingleContainer checks the status of a single container
func checkSingleContainer(name string) types.EnhancedContainerStatus {
	status := types.EnhancedContainerStatus{
		Name:     name,
		Status:   "stopped",
		Health:   "",
		Uptime:   "",
		Restarts: 0,
	}

	// Check if container exists and is running
	cmd := exec.Command("sudo", "docker", "inspect", name,
		"--format", "{{.State.Running}}|{{.State.Health.Status}}|{{.State.StartedAt}}|{{.RestartCount}}")
	output, err := cmd.Output()
	if err != nil {
		status.Status = "not_found"
		return status
	}

	parts := strings.Split(strings.TrimSpace(string(output)), "|")
	if len(parts) >= 4 {
		if parts[0] == "true" {
			status.Status = "running"
		}
		if parts[1] != "" && parts[1] != "<no value>" {
			status.Health = parts[1]
		}
		// Calculate uptime
		if startedAt, err := time.Parse(time.RFC3339, parts[2]); err == nil {
			uptime := time.Since(startedAt)
			status.Uptime = formatDuration(uptime)
		}
		if restarts, err := strconv.Atoi(parts[3]); err == nil {
			status.Restarts = restarts
		}
	}

	return status
}

// performBasicChecks performs basic connectivity checks
func performBasicChecks(config *types.EnhancedHealthCheckConfig, result *types.EnhancedHealthCheckResult) error {
	checks := []struct {
		name     string
		checkFn  func() (bool, string, int64)
	}{
		{"postgres_connectivity", func() (bool, string, int64) {
			return checkPostgresConnectivity(config.Subdomain)
		}},
		{"kong_gateway", func() (bool, string, int64) {
			return checkKongGatewayHealth(8000)
		}},
		{"studio_accessibility", func() (bool, string, int64) {
			return checkHTTPEndpoint("http://localhost:3000", "Studio")
		}},
	}

	hasFailures := false
	for _, check := range checks {
		success, message, duration := check.checkFn()
		status := "passed"
		if !success {
			status = "failed"
			hasFailures = true
		}

		result.Checks = append(result.Checks, types.HealthCheck{
			Name:       check.name,
			Status:     status,
			Message:    message,
			DurationMS: duration,
		})
	}

	if hasFailures {
		return fmt.Errorf("some basic checks failed")
	}
	return nil
}

// performDeepChecks performs deep diagnostic checks
func performDeepChecks(config *types.EnhancedHealthCheckConfig, result *types.EnhancedHealthCheckResult) error {
	checks := []struct {
		name     string
		checkFn  func() (bool, string, int64)
	}{
		{"database_query_execution", func() (bool, string, int64) {
			return checkDatabaseQuery(config.Subdomain)
		}},
		{"rest_api_endpoint", func() (bool, string, int64) {
			return checkHTTPEndpoint("http://localhost:8000/rest/v1/", "REST API")
		}},
		{"auth_endpoint", func() (bool, string, int64) {
			return checkHTTPEndpoint("http://localhost:8000/auth/v1/health", "Auth")
		}},
		{"storage_endpoint", func() (bool, string, int64) {
			return checkHTTPEndpoint("http://localhost:8000/storage/v1/", "Storage")
		}},
		{"realtime_endpoint", func() (bool, string, int64) {
			return checkHTTPEndpoint("http://localhost:8000/realtime/v1/", "Realtime")
		}},
	}

	hasFailures := false
	for _, check := range checks {
		success, message, duration := check.checkFn()
		status := "passed"
		if !success {
			status = "failed"
			hasFailures = true
		}

		result.Checks = append(result.Checks, types.HealthCheck{
			Name:       check.name,
			Status:     status,
			Message:    message,
			DurationMS: duration,
		})
	}

	if hasFailures {
		return fmt.Errorf("some deep checks failed")
	}
	return nil
}

// collectMetrics collects performance metrics for containers
func collectMetrics(config *types.EnhancedHealthCheckConfig, result *types.EnhancedHealthCheckResult) error {
	for i := range result.Containers {
		if result.Containers[i].Status == "running" {
			metrics := getContainerMetrics(result.Containers[i].Name)
			result.Containers[i].Metrics = metrics
		}
	}
	return nil
}

// getContainerMetrics retrieves resource usage metrics for a container
func getContainerMetrics(containerName string) *types.ContainerMetrics {
	metrics := &types.ContainerMetrics{
		Name: containerName,
	}

	// Get container stats
	cmd := exec.Command("sudo", "docker", "stats", containerName, "--no-stream", "--format",
		"{{.CPUPerc}}|{{.MemUsage}}|{{.NetIO}}")
	output, err := cmd.Output()
	if err != nil {
		return metrics
	}

	parts := strings.Split(strings.TrimSpace(string(output)), "|")
	if len(parts) >= 3 {
		// Parse CPU percentage
		cpuStr := strings.TrimSuffix(parts[0], "%")
		if cpu, err := strconv.ParseFloat(cpuStr, 64); err == nil {
			metrics.CPUPercent = cpu
		}

		// Parse memory usage (format: "100MiB / 2GiB")
		memParts := strings.Split(parts[1], " / ")
		if len(memParts) >= 1 {
			memUsage := parseMemorySize(memParts[0])
			metrics.MemoryMB = memUsage
		}

		// Parse network I/O (format: "1.2MB / 3.4MB")
		netParts := strings.Split(parts[2], " / ")
		if len(netParts) >= 2 {
			metrics.NetworkRxMB = parseNetworkSize(netParts[0])
			metrics.NetworkTxMB = parseNetworkSize(netParts[1])
		}
	}

	return metrics
}

// performServiceDiagnostics performs service-specific diagnostics
func performServiceDiagnostics(config *types.EnhancedHealthCheckConfig, result *types.EnhancedHealthCheckResult) error {
	// PostgreSQL diagnostics
	pgDiag := checkPostgresDiagnostics(config.Subdomain)
	result.Services = append(result.Services, pgDiag)

	// Kong diagnostics
	kongDiag := checkKongDiagnostics()
	result.Services = append(result.Services, kongDiag)

	return nil
}

// checkPostgresConnectivity checks if PostgreSQL is accepting connections
func checkPostgresConnectivity(subdomain string) (bool, string, int64) {
	start := time.Now()
	containerName := fmt.Sprintf("%s-postgres", subdomain)

	cmd := exec.Command("sudo", "docker", "exec", "-i", containerName,
		"pg_isready", "-h", "localhost", "-p", "5432")
	output, err := cmd.CombinedOutput()
	duration := time.Since(start).Milliseconds()

	if err != nil {
		return false, fmt.Sprintf("PostgreSQL not ready: %s", string(output)), duration
	}

	if strings.Contains(string(output), "accepting connections") {
		return true, "PostgreSQL accepting connections", duration
	}

	return false, string(output), duration
}

// checkDatabaseQuery executes a simple query to verify database functionality
func checkDatabaseQuery(subdomain string) (bool, string, int64) {
	start := time.Now()
	containerName := fmt.Sprintf("%s-postgres", subdomain)

	cmd := exec.Command("sudo", "docker", "exec", "-i", containerName,
		"psql", "-U", "postgres", "-c", "SELECT 1;")
	output, err := cmd.CombinedOutput()
	duration := time.Since(start).Milliseconds()

	if err != nil {
		return false, fmt.Sprintf("Query execution failed: %s", string(output)), duration
	}

	return true, "Database query executed successfully", duration
}

// checkKongGatewayHealth checks Kong gateway health
func checkKongGatewayHealth(port int) (bool, string, int64) {
	start := time.Now()
	client := &http.Client{Timeout: 5 * time.Second}

	url := fmt.Sprintf("http://localhost:%d/", port)
	resp, err := client.Get(url)
	duration := time.Since(start).Milliseconds()

	if err != nil {
		return false, fmt.Sprintf("Kong not responding: %v", err), duration
	}
	defer resp.Body.Close()

	if resp.StatusCode > 0 {
		return true, fmt.Sprintf("Kong responding (HTTP %d)", resp.StatusCode), duration
	}

	return false, "Kong returned invalid response", duration
}

// checkHTTPEndpoint checks if an HTTP endpoint is responding
func checkHTTPEndpoint(url, serviceName string) (bool, string, int64) {
	start := time.Now()
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(url)
	duration := time.Since(start).Milliseconds()

	if err != nil {
		return false, fmt.Sprintf("%s not responding: %v", serviceName, err), duration
	}
	defer resp.Body.Close()

	if resp.StatusCode < 500 {
		return true, fmt.Sprintf("%s responding (HTTP %d)", serviceName, resp.StatusCode), duration
	}

	return false, fmt.Sprintf("%s error (HTTP %d)", serviceName, resp.StatusCode), duration
}

// checkPostgresDiagnostics performs PostgreSQL-specific diagnostics
func checkPostgresDiagnostics(subdomain string) types.ServiceDiagnostics {
	diag := types.ServiceDiagnostics{
		Name:    "postgresql",
		Status:  "healthy",
		Details: make(map[string]string),
		Errors:  []string{},
	}

	containerName := fmt.Sprintf("%s-postgres", subdomain)
	start := time.Now()

	// Get connection count
	cmd := exec.Command("sudo", "docker", "exec", "-i", containerName,
		"psql", "-U", "postgres", "-t", "-c",
		"SELECT count(*) FROM pg_stat_activity;")
	if output, err := cmd.Output(); err == nil {
		diag.Details["active_connections"] = strings.TrimSpace(string(output))
	} else {
		diag.Errors = append(diag.Errors, fmt.Sprintf("Failed to get connection count: %v", err))
		diag.Status = "degraded"
	}

	// Get database size
	cmd = exec.Command("sudo", "docker", "exec", "-i", containerName,
		"psql", "-U", "postgres", "-t", "-c",
		"SELECT pg_size_pretty(pg_database_size('postgres'));")
	if output, err := cmd.Output(); err == nil {
		diag.Details["database_size"] = strings.TrimSpace(string(output))
	}

	diag.ResponseTimeMS = time.Since(start).Milliseconds()
	return diag
}

// checkKongDiagnostics performs Kong-specific diagnostics
func checkKongDiagnostics() types.ServiceDiagnostics {
	diag := types.ServiceDiagnostics{
		Name:    "kong",
		Status:  "healthy",
		Details: make(map[string]string),
		Errors:  []string{},
	}

	start := time.Now()
	client := &http.Client{Timeout: 5 * time.Second}

	// Check Kong admin API status
	resp, err := client.Get("http://localhost:8001/status")
	if err != nil {
		diag.Status = "unhealthy"
		diag.Errors = append(diag.Errors, fmt.Sprintf("Admin API not responding: %v", err))
		return diag
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		var statusData map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&statusData); err == nil {
			if server, ok := statusData["server"].(map[string]interface{}); ok {
				if conns, ok := server["connections_active"].(float64); ok {
					diag.Details["active_connections"] = fmt.Sprintf("%.0f", conns)
				}
			}
		}
	} else {
		diag.Status = "degraded"
		diag.Errors = append(diag.Errors, fmt.Sprintf("Admin API returned HTTP %d", resp.StatusCode))
	}

	diag.ResponseTimeMS = time.Since(start).Milliseconds()
	return diag
}

// Helper functions

func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func parseMemorySize(sizeStr string) int64 {
	sizeStr = strings.TrimSpace(sizeStr)
	var multiplier int64 = 1

	if strings.HasSuffix(sizeStr, "GiB") || strings.HasSuffix(sizeStr, "GB") {
		multiplier = 1024
		sizeStr = strings.TrimSuffix(strings.TrimSuffix(sizeStr, "GiB"), "GB")
	} else if strings.HasSuffix(sizeStr, "MiB") || strings.HasSuffix(sizeStr, "MB") {
		multiplier = 1
		sizeStr = strings.TrimSuffix(strings.TrimSuffix(sizeStr, "MiB"), "MB")
	} else if strings.HasSuffix(sizeStr, "KiB") || strings.HasSuffix(sizeStr, "KB") {
		multiplier = 1
		sizeStr = strings.TrimSuffix(strings.TrimSuffix(sizeStr, "KiB"), "KB")
		multiplier = 1024
	}

	size, err := strconv.ParseFloat(sizeStr, 64)
	if err != nil {
		return 0
	}

	return int64(size * float64(multiplier))
}

func parseNetworkSize(sizeStr string) float64 {
	sizeStr = strings.TrimSpace(sizeStr)
	var multiplier float64 = 1

	if strings.HasSuffix(sizeStr, "GB") {
		multiplier = 1024
		sizeStr = strings.TrimSuffix(sizeStr, "GB")
	} else if strings.HasSuffix(sizeStr, "MB") {
		multiplier = 1
		sizeStr = strings.TrimSuffix(sizeStr, "MB")
	} else if strings.HasSuffix(sizeStr, "KB") {
		multiplier = 0.001
		sizeStr = strings.TrimSuffix(sizeStr, "KB")
	} else if strings.HasSuffix(sizeStr, "B") {
		multiplier = 0.000001
		sizeStr = strings.TrimSuffix(sizeStr, "B")
	}

	size, err := strconv.ParseFloat(sizeStr, 64)
	if err != nil {
		return 0
	}

	return size * multiplier
}
