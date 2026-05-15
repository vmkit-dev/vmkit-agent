package diagnose

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

// Version is set by the main package at startup
var Version = "unknown"

// Diagnose runs comprehensive diagnostics on the instance
func Diagnose(config *types.DiagnoseConfig) (*types.DiagnoseResult, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	startTime := time.Now()
	result := &types.DiagnoseResult{
		InstanceID:    config.InstanceID,
		OverallStatus: "pass",
		Checks:        []types.DiagnosticCheck{},
		Timestamp:     time.Now().Format(time.RFC3339),
		AgentVersion:  Version,
	}

	checks := []func() types.DiagnosticCheck{
		checkDockerDaemon,
		func() types.DiagnosticCheck { return checkContainers(config.Subdomain) },
		func() types.DiagnosticCheck { return checkContainerRestarts(config.Subdomain) },
		checkDiskUsage,
		checkMemory,
		checkCPU,
		checkNginxConfig,
		func() types.DiagnosticCheck { return checkTLSCerts(config.Subdomain) },
		func() types.DiagnosticCheck { return checkDockerLogs(config.Subdomain) },
		func() types.DiagnosticCheck { return checkHealthEndpoint() },
		func() types.DiagnosticCheck { return checkPostgresConnectivity(config.Subdomain) },
		func() types.DiagnosticCheck { return checkBackupStatus(config.Subdomain, config.BaseDir) },
	}

	for _, checkFn := range checks {
		check := checkFn()
		result.Checks = append(result.Checks, check)

		switch check.Status {
		case "fail":
			result.OverallStatus = "fail"
		case "warn":
			if result.OverallStatus == "pass" {
				result.OverallStatus = "warn"
			}
		}
	}

	result.Duration = time.Since(startTime).String()
	return result, nil
}

func checkDockerDaemon() types.DiagnosticCheck {
	start := time.Now()

	cmd := exec.Command("sudo", "docker", "info", "--format", "{{.ServerVersion}}")
	output, err := cmd.Output()
	duration := time.Since(start).String()

	if err != nil {
		return types.DiagnosticCheck{
			Name:     "docker_daemon",
			Status:   "fail",
			Detail:   "Docker daemon not running",
			Duration: duration,
		}
	}

	version := strings.TrimSpace(string(output))
	return types.DiagnosticCheck{
		Name:     "docker_daemon",
		Status:   "pass",
		Detail:   fmt.Sprintf("Docker %s running", version),
		Duration: duration,
	}
}

func checkContainers(subdomain string) types.DiagnosticCheck {
	start := time.Now()

	expectedContainers := []string{
		fmt.Sprintf("%s-postgres", subdomain),
		fmt.Sprintf("%s-kong", subdomain),
		fmt.Sprintf("%s-studio", subdomain),
		fmt.Sprintf("%s-auth", subdomain),
		fmt.Sprintf("%s-rest", subdomain),
		fmt.Sprintf("%s-realtime", subdomain),
		fmt.Sprintf("%s-storage", subdomain),
	}

	running := 0
	for _, name := range expectedContainers {
		cmd := exec.Command("sudo", "docker", "inspect", name, "--format", "{{.State.Running}}")
		output, err := cmd.Output()
		if err == nil && strings.TrimSpace(string(output)) == "true" {
			running++
		}
	}

	duration := time.Since(start).String()
	total := len(expectedContainers)

	if running == total {
		return types.DiagnosticCheck{
			Name:     "containers",
			Status:   "pass",
			Detail:   fmt.Sprintf("%d/%d running", running, total),
			Duration: duration,
		}
	}

	status := "warn"
	if running == 0 {
		status = "fail"
	}
	return types.DiagnosticCheck{
		Name:     "containers",
		Status:   status,
		Detail:   fmt.Sprintf("%d/%d running", running, total),
		Duration: duration,
	}
}

func checkContainerRestarts(subdomain string) types.DiagnosticCheck {
	start := time.Now()

	containers := []string{
		fmt.Sprintf("%s-postgres", subdomain),
		fmt.Sprintf("%s-kong", subdomain),
		fmt.Sprintf("%s-studio", subdomain),
		fmt.Sprintf("%s-auth", subdomain),
		fmt.Sprintf("%s-rest", subdomain),
		fmt.Sprintf("%s-realtime", subdomain),
		fmt.Sprintf("%s-storage", subdomain),
	}

	var restarted []string
	for _, name := range containers {
		cmd := exec.Command("sudo", "docker", "inspect", name, "--format", "{{.RestartCount}}")
		output, err := cmd.Output()
		if err != nil {
			continue
		}
		count, err := strconv.Atoi(strings.TrimSpace(string(output)))
		if err != nil {
			continue
		}
		if count > 0 {
			// Extract short name (remove subdomain prefix)
			short := strings.TrimPrefix(name, subdomain+"-")
			restarted = append(restarted, fmt.Sprintf("%s(%d)", short, count))
		}
	}

	duration := time.Since(start).String()

	if len(restarted) == 0 {
		return types.DiagnosticCheck{
			Name:     "container_restarts",
			Status:   "pass",
			Detail:   "no restarts detected",
			Duration: duration,
		}
	}

	return types.DiagnosticCheck{
		Name:     "container_restarts",
		Status:   "warn",
		Detail:   fmt.Sprintf("restarts: %s", strings.Join(restarted, ", ")),
		Duration: duration,
	}
}

func checkDiskUsage() types.DiagnosticCheck {
	start := time.Now()

	cmd := exec.Command("df", "--output=pcent,size,used", "/")
	output, err := cmd.Output()
	duration := time.Since(start).String()

	if err != nil {
		return types.DiagnosticCheck{
			Name:     "disk_usage",
			Status:   "warn",
			Detail:   fmt.Sprintf("could not check: %v", err),
			Duration: duration,
		}
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 2 {
		return types.DiagnosticCheck{
			Name:     "disk_usage",
			Status:   "warn",
			Detail:   "unexpected df output",
			Duration: duration,
		}
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 3 {
		return types.DiagnosticCheck{
			Name:     "disk_usage",
			Status:   "warn",
			Detail:   "unexpected df format",
			Duration: duration,
		}
	}

	pctStr := strings.TrimSuffix(fields[0], "%")
	pct, _ := strconv.Atoi(pctStr)
	totalKB, _ := strconv.ParseInt(fields[1], 10, 64)
	usedKB, _ := strconv.ParseInt(fields[2], 10, 64)

	detail := fmt.Sprintf("%d%% used (%dGB/%dGB)", pct, usedKB/(1024*1024), totalKB/(1024*1024))

	status := "pass"
	if pct >= 95 {
		status = "fail"
	} else if pct >= 80 {
		status = "warn"
	}

	return types.DiagnosticCheck{
		Name:     "disk_usage",
		Status:   status,
		Detail:   detail,
		Duration: duration,
	}
}

func checkMemory() types.DiagnosticCheck {
	start := time.Now()

	data, err := os.ReadFile("/proc/meminfo")
	duration := time.Since(start).String()

	if err != nil {
		return types.DiagnosticCheck{
			Name:     "memory",
			Status:   "warn",
			Detail:   fmt.Sprintf("could not read /proc/meminfo: %v", err),
			Duration: duration,
		}
	}

	var memTotal, memAvailable int64
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			memTotal = parseMemInfoValue(line)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			memAvailable = parseMemInfoValue(line)
		}
	}

	if memTotal == 0 {
		return types.DiagnosticCheck{
			Name:     "memory",
			Status:   "warn",
			Detail:   "could not parse memory info",
			Duration: duration,
		}
	}

	usedKB := memTotal - memAvailable
	pct := int(float64(usedKB) / float64(memTotal) * 100)

	detail := fmt.Sprintf("%d%% used (%dMB/%dMB)", pct, usedKB/1024, memTotal/1024)

	status := "pass"
	if pct >= 95 {
		status = "fail"
	} else if pct >= 80 {
		status = "warn"
	}

	return types.DiagnosticCheck{
		Name:     "memory",
		Status:   status,
		Detail:   detail,
		Duration: duration,
	}
}

func parseMemInfoValue(line string) int64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	val, _ := strconv.ParseInt(fields[1], 10, 64)
	return val
}

func checkCPU() types.DiagnosticCheck {
	start := time.Now()

	data, err := os.ReadFile("/proc/loadavg")
	duration := time.Since(start).String()

	if err != nil {
		return types.DiagnosticCheck{
			Name:     "cpu_load",
			Status:   "warn",
			Detail:   fmt.Sprintf("could not read /proc/loadavg: %v", err),
			Duration: duration,
		}
	}

	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return types.DiagnosticCheck{
			Name:     "cpu_load",
			Status:   "warn",
			Detail:   "could not parse load average",
			Duration: duration,
		}
	}

	load1m, _ := strconv.ParseFloat(fields[0], 64)
	nproc := runtime.NumCPU()

	detail := fmt.Sprintf("1m avg %.2f (%d cores)", load1m, nproc)

	status := "pass"
	if load1m > float64(nproc)*2 {
		status = "fail"
	} else if load1m > float64(nproc) {
		status = "warn"
	}

	return types.DiagnosticCheck{
		Name:     "cpu_load",
		Status:   status,
		Detail:   detail,
		Duration: duration,
	}
}

func checkNginxConfig() types.DiagnosticCheck {
	start := time.Now()

	cmd := exec.Command("sudo", "nginx", "-t")
	output, err := cmd.CombinedOutput()
	duration := time.Since(start).String()

	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return types.DiagnosticCheck{
			Name:     "nginx_config",
			Status:   "fail",
			Detail:   detail,
			Duration: duration,
		}
	}

	return types.DiagnosticCheck{
		Name:     "nginx_config",
		Status:   "pass",
		Detail:   "configuration valid",
		Duration: duration,
	}
}

func checkTLSCerts(subdomain string) types.DiagnosticCheck {
	start := time.Now()

	certPath := fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem", subdomain)

	cmd := exec.Command("sudo", "openssl", "x509", "-enddate", "-noout", "-in", certPath)
	output, err := cmd.Output()
	duration := time.Since(start).String()

	if err != nil {
		return types.DiagnosticCheck{
			Name:     "tls_certs",
			Status:   "warn",
			Detail:   fmt.Sprintf("could not read cert: %v", err),
			Duration: duration,
		}
	}

	// Parse "notAfter=Mar 15 12:00:00 2026 GMT"
	line := strings.TrimSpace(string(output))
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return types.DiagnosticCheck{
			Name:     "tls_certs",
			Status:   "warn",
			Detail:   "could not parse cert expiry",
			Duration: duration,
		}
	}

	expiry, err := time.Parse("Jan  2 15:04:05 2006 GMT", parts[1])
	if err != nil {
		// Try single-digit day format
		expiry, err = time.Parse("Jan 2 15:04:05 2006 GMT", parts[1])
		if err != nil {
			return types.DiagnosticCheck{
				Name:     "tls_certs",
				Status:   "warn",
				Detail:   fmt.Sprintf("could not parse date: %s", parts[1]),
				Duration: duration,
			}
		}
	}

	daysLeft := int(time.Until(expiry).Hours() / 24)
	detail := fmt.Sprintf("expires in %d days", daysLeft)

	status := "pass"
	if daysLeft < 7 {
		status = "fail"
	} else if daysLeft < 30 {
		status = "warn"
	}

	return types.DiagnosticCheck{
		Name:     "tls_certs",
		Status:   status,
		Detail:   detail,
		Duration: duration,
	}
}

func checkDockerLogs(subdomain string) types.DiagnosticCheck {
	start := time.Now()

	containers := []string{
		fmt.Sprintf("%s-postgres", subdomain),
		fmt.Sprintf("%s-kong", subdomain),
		fmt.Sprintf("%s-auth", subdomain),
		fmt.Sprintf("%s-rest", subdomain),
		fmt.Sprintf("%s-realtime", subdomain),
		fmt.Sprintf("%s-storage", subdomain),
	}

	var withErrors []string
	for _, name := range containers {
		cmd := exec.Command("sudo", "docker", "logs", "--tail", "50", "--since", "1h", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			continue
		}
		logStr := strings.ToLower(string(output))
		if strings.Contains(logStr, "error") || strings.Contains(logStr, "fatal") || strings.Contains(logStr, "panic") {
			short := strings.TrimPrefix(name, subdomain+"-")
			withErrors = append(withErrors, short)
		}
	}

	duration := time.Since(start).String()

	if len(withErrors) == 0 {
		return types.DiagnosticCheck{
			Name:     "docker_logs_errors",
			Status:   "pass",
			Detail:   "no recent errors in logs",
			Duration: duration,
		}
	}

	return types.DiagnosticCheck{
		Name:     "docker_logs_errors",
		Status:   "warn",
		Detail:   fmt.Sprintf("recent errors in: %s", strings.Join(withErrors, ", ")),
		Duration: duration,
	}
}

func checkHealthEndpoint() types.DiagnosticCheck {
	start := time.Now()

	cmd := exec.Command("curl", "-sf", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "5", "http://localhost:8000/")
	output, err := cmd.Output()
	duration := time.Since(start).String()

	httpCode := strings.TrimSpace(string(output))

	if err != nil && httpCode == "" {
		return types.DiagnosticCheck{
			Name:     "health_endpoint",
			Status:   "fail",
			Detail:   "Kong gateway not responding",
			Duration: duration,
		}
	}

	// Kong returns various codes; any response means it's alive
	if httpCode != "" {
		return types.DiagnosticCheck{
			Name:     "health_endpoint",
			Status:   "pass",
			Detail:   fmt.Sprintf("Kong responding (HTTP %s)", httpCode),
			Duration: duration,
		}
	}

	return types.DiagnosticCheck{
		Name:     "health_endpoint",
		Status:   "fail",
		Detail:   "no response from Kong",
		Duration: duration,
	}
}

func checkPostgresConnectivity(subdomain string) types.DiagnosticCheck {
	start := time.Now()
	containerName := fmt.Sprintf("%s-postgres", subdomain)

	cmd := exec.Command("sudo", "docker", "exec", "-i", containerName,
		"pg_isready", "-h", "localhost", "-p", "5432")
	output, err := cmd.CombinedOutput()
	duration := time.Since(start).String()

	if err != nil {
		return types.DiagnosticCheck{
			Name:     "postgres_connectivity",
			Status:   "fail",
			Detail:   fmt.Sprintf("not ready: %s", strings.TrimSpace(string(output))),
			Duration: duration,
		}
	}

	return types.DiagnosticCheck{
		Name:     "postgres_connectivity",
		Status:   "pass",
		Detail:   "accepting connections",
		Duration: duration,
	}
}

func checkBackupStatus(subdomain, baseDir string) types.DiagnosticCheck {
	start := time.Now()

	backupDir := fmt.Sprintf("%s/%s/backups", baseDir, subdomain)

	// Check if backup directory exists and has recent files
	cmd := exec.Command("sudo", "find", backupDir, "-name", "*.sql.gz", "-mtime", "-7", "-type", "f")
	output, err := cmd.Output()
	duration := time.Since(start).String()

	if err != nil {
		return types.DiagnosticCheck{
			Name:     "backup_status",
			Status:   "warn",
			Detail:   "no backup directory found",
			Duration: duration,
		}
	}

	files := strings.TrimSpace(string(output))
	if files == "" {
		return types.DiagnosticCheck{
			Name:     "backup_status",
			Status:   "warn",
			Detail:   "no backups in last 7 days",
			Duration: duration,
		}
	}

	count := len(strings.Split(files, "\n"))
	return types.DiagnosticCheck{
		Name:     "backup_status",
		Status:   "pass",
		Detail:   fmt.Sprintf("%d backup(s) in last 7 days", count),
		Duration: duration,
	}
}
