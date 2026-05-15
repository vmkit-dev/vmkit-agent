package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/vmkit-dev/vmkit-agent/internal/backup"
	"github.com/vmkit-dev/vmkit-agent/internal/cleanup"
	"github.com/vmkit-dev/vmkit-agent/internal/config"
	"github.com/vmkit-dev/vmkit-agent/internal/deploy"
	"github.com/vmkit-dev/vmkit-agent/internal/diagnose"
	"github.com/vmkit-dev/vmkit-agent/internal/destroy"
	"github.com/vmkit-dev/vmkit-agent/internal/enabletls"
	"github.com/vmkit-dev/vmkit-agent/internal/harden"
	"github.com/vmkit-dev/vmkit-agent/internal/health"
	"github.com/vmkit-dev/vmkit-agent/internal/instance"
	"github.com/vmkit-dev/vmkit-agent/internal/openport"
	"github.com/vmkit-dev/vmkit-agent/internal/resetvm"
	"github.com/vmkit-dev/vmkit-agent/internal/rotatecreds"
	"github.com/vmkit-dev/vmkit-agent/internal/upgrade"
	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

var Version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "version":
		fmt.Printf("vmkit-agent version %s\n", Version)
	case "deploy":
		handleDeploy()
	case "harden":
		handleHarden()
	case "cleanup":
		handleCleanup()
	case "destroy":
		handleDestroy()
	case "status":
		handleStatus()
	case "health":
		handleHealth()
	case "diagnose":
		handleDiagnose()
	case "start":
		handleStart()
	case "stop":
		handleStop()
	case "update":
		handleUpdate()
	case "backup":
		handleBackup()
	case "restore":
		handleRestore()
	case "verify":
		handleVerify()
	case "upgrade":
		handleUpgrade()
	case "enable-tls":
		handleEnableTLS()
	case "rotate-credentials":
		handleRotateCredentials()
	case "open-port":
		handleOpenPort()
	case "update-allowlist":
		handleUpdateAllowlist()
	case "reset-vm":
		handleResetVM()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`vmkit-agent - Supabase deployment agent

Usage:
  vmkit-agent <command> [options]

Commands:
  version              Show version
  deploy               Deploy a Supabase instance
  harden               Harden VM security (SSH, firewall, fail2ban)
  cleanup              Cleanup VM and optionally restore original settings
  destroy              Destroy an instance and clean up resources
  status               Check instance status (basic)
  health               Enhanced health check with diagnostics
  diagnose             Comprehensive instance troubleshooting
  start                Start an instance
  stop                 Stop an instance
  update               Update an instance
  backup               Backup an instance database and storage
  restore              Restore an instance from backup
  verify               Verify backup integrity and restorability
  upgrade              Upgrade instance to a new Supabase version
  enable-tls           Enable TLS for an existing instance
  rotate-credentials   Rotate instance credentials
  open-port            Open a firewall port (UFW)
  update-allowlist     Reconcile UFW source-IP allowlist for a port
  reset-vm             Restore a shared VM to just-hardened baseline (purge instances + UFW reset)

Options:
  --config FILE        Configuration file (required for deploy/update/harden/cleanup/destroy/backup/restore)
  --instance-id ID     Instance ID (required for status/start/stop/health)
  --subdomain NAME     Instance subdomain (optional, defaults to instance-id)
  --level LEVEL        Health check level: basic, deep, metrics, services, full (default: basic)

Examples:
  vmkit-agent deploy --config /tmp/deploy.json
  vmkit-agent harden --config /tmp/harden.json
  vmkit-agent cleanup --config /tmp/cleanup.json
  vmkit-agent destroy --config /tmp/destroy.json
  vmkit-agent status --instance-id abc-123
  vmkit-agent health --instance-id abc-123 --level full
  vmkit-agent health --subdomain my-instance --level deep
  vmkit-agent diagnose --instance-id abc-123 --subdomain my-instance
  vmkit-agent start --instance-id abc-123
  vmkit-agent backup --config /tmp/backup.json
  vmkit-agent restore --config /tmp/restore.json
  vmkit-agent verify --config /tmp/verify.json
  vmkit-agent upgrade --config /tmp/upgrade.json
  vmkit-agent enable-tls --config /tmp/tls.json
  vmkit-agent rotate-credentials --config /tmp/rotate.json
  vmkit-agent reset-vm --config /tmp/reset-vm.json`)
}

func handleDeploy() {
	var configFile string

	fs := flag.NewFlagSet("deploy", flag.ExitOnError)
	fs.StringVar(&configFile, "config", "", "Path to deployment configuration file")
	fs.Parse(os.Args[2:])

	if configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --config is required for deploy command\n")
		os.Exit(1)
	}

	// Load configuration
	cfg, err := config.Load(configFile)
	if err != nil {
		result := types.DeployResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to load config: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	// Execute deployment
	result := deploy.Deploy(cfg)

	// Output result as JSON
	json.NewEncoder(os.Stdout).Encode(result)

	if !result.Success {
		os.Exit(1)
	}
}

func handleStatus() {
	var instanceID, subdomain string

	fs := flag.NewFlagSet("status", flag.ExitOnError)
	fs.StringVar(&instanceID, "instance-id", "", "Instance ID")
	fs.StringVar(&subdomain, "subdomain", "", "Instance subdomain")
	fs.Parse(os.Args[2:])

	// Require either instance-id or subdomain
	if instanceID == "" && subdomain == "" {
		fmt.Fprintf(os.Stderr, "Error: --instance-id or --subdomain is required\n")
		os.Exit(1)
	}

	// If subdomain not provided, use instance-id as subdomain for simplicity
	if subdomain == "" {
		subdomain = instanceID
	}

	status, err := instance.Status(instanceID, subdomain)
	if err != nil {
		// Still output status with error state
		if status == nil {
			status = &types.InstanceStatus{
				InstanceID: instanceID,
				State:      "error",
			}
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}

	json.NewEncoder(os.Stdout).Encode(status)

	if status.State == "error" {
		os.Exit(1)
	}
}

func handleHealth() {
	var instanceID, subdomain, baseDir, levelStr string
	var timeout int

	fs := flag.NewFlagSet("health", flag.ExitOnError)
	fs.StringVar(&instanceID, "instance-id", "", "Instance ID")
	fs.StringVar(&subdomain, "subdomain", "", "Instance subdomain")
	fs.StringVar(&baseDir, "base-dir", "/opt/supabase", "Base directory for instances")
	fs.StringVar(&levelStr, "level", "basic", "Health check level: basic, deep, metrics, services, full")
	fs.IntVar(&timeout, "timeout", 30, "Timeout in seconds")
	fs.Parse(os.Args[2:])

	// Require either instance-id or subdomain
	if instanceID == "" && subdomain == "" {
		fmt.Fprintf(os.Stderr, "Error: --instance-id or --subdomain is required\n")
		os.Exit(1)
	}

	// If subdomain not provided, use instance-id as subdomain
	if subdomain == "" {
		subdomain = instanceID
	}

	// If instance-id not provided, use subdomain
	if instanceID == "" {
		instanceID = subdomain
	}

	// Parse health check level
	var level types.HealthCheckLevel
	switch levelStr {
	case "basic":
		level = types.HealthCheckBasic
	case "deep":
		level = types.HealthCheckDeep
	case "metrics":
		level = types.HealthCheckMetrics
	case "services":
		level = types.HealthCheckServices
	case "full":
		level = types.HealthCheckFull
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid level '%s'. Valid levels: basic, deep, metrics, services, full\n", levelStr)
		os.Exit(1)
	}

	// Create health check config
	config := &types.EnhancedHealthCheckConfig{
		InstanceID: instanceID,
		Subdomain:  subdomain,
		BaseDir:    baseDir,
		Level:      level,
		Timeout:    timeout,
	}

	// Perform health check
	result, err := health.CheckEnhanced(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		if result == nil {
			// Create minimal error result
			result = &types.EnhancedHealthCheckResult{
				InstanceID:    instanceID,
				OverallStatus: "unhealthy",
				Level:         levelStr,
			}
		}
	}

	// Output result as JSON
	json.NewEncoder(os.Stdout).Encode(result)

	if result.OverallStatus != "healthy" {
		os.Exit(1)
	}
}

func handleDiagnose() {
	var instanceID, subdomain, baseDir string
	var timeout int

	fs := flag.NewFlagSet("diagnose", flag.ExitOnError)
	fs.StringVar(&instanceID, "instance-id", "", "Instance ID")
	fs.StringVar(&subdomain, "subdomain", "", "Instance subdomain")
	fs.StringVar(&baseDir, "base-dir", "/opt/supabase", "Base directory for instances")
	fs.IntVar(&timeout, "timeout", 60, "Timeout in seconds")
	fs.Parse(os.Args[2:])

	if instanceID == "" && subdomain == "" {
		fmt.Fprintf(os.Stderr, "Error: --instance-id or --subdomain is required\n")
		os.Exit(1)
	}

	if subdomain == "" {
		subdomain = instanceID
	}
	if instanceID == "" {
		instanceID = subdomain
	}

	// Set version so diagnose package can report it
	diagnose.Version = Version

	config := &types.DiagnoseConfig{
		InstanceID: instanceID,
		Subdomain:  subdomain,
		BaseDir:    baseDir,
		Timeout:    timeout,
	}

	result, err := diagnose.Diagnose(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		if result == nil {
			result = &types.DiagnoseResult{
				InstanceID:    instanceID,
				OverallStatus: "fail",
				AgentVersion:  Version,
			}
		}
	}

	json.NewEncoder(os.Stdout).Encode(result)

	if result.OverallStatus != "pass" {
		os.Exit(1)
	}
}

func handleStart() {
	var instanceID, subdomain string

	fs := flag.NewFlagSet("start", flag.ExitOnError)
	fs.StringVar(&instanceID, "instance-id", "", "Instance ID")
	fs.StringVar(&subdomain, "subdomain", "", "Instance subdomain")
	fs.Parse(os.Args[2:])

	// Require either instance-id or subdomain
	if instanceID == "" && subdomain == "" {
		fmt.Fprintf(os.Stderr, "Error: --instance-id or --subdomain is required\n")
		os.Exit(1)
	}

	// If subdomain not provided, use instance-id as subdomain
	if subdomain == "" {
		subdomain = instanceID
	}

	err := instance.Start(subdomain)
	if err != nil {
		result := map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	result := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Instance %s started successfully", subdomain),
	}
	json.NewEncoder(os.Stdout).Encode(result)
}

func handleStop() {
	var instanceID, subdomain string

	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	fs.StringVar(&instanceID, "instance-id", "", "Instance ID")
	fs.StringVar(&subdomain, "subdomain", "", "Instance subdomain")
	fs.Parse(os.Args[2:])

	// Require either instance-id or subdomain
	if instanceID == "" && subdomain == "" {
		fmt.Fprintf(os.Stderr, "Error: --instance-id or --subdomain is required\n")
		os.Exit(1)
	}

	// If subdomain not provided, use instance-id as subdomain
	if subdomain == "" {
		subdomain = instanceID
	}

	err := instance.Stop(subdomain)
	if err != nil {
		result := map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	result := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Instance %s stopped successfully", subdomain),
	}
	json.NewEncoder(os.Stdout).Encode(result)
}

func handleUpdate() {
	var configFile string

	fs := flag.NewFlagSet("update", flag.ExitOnError)
	fs.StringVar(&configFile, "config", "", "Path to deployment configuration file")
	fs.Parse(os.Args[2:])

	if configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --config is required for update command\n")
		os.Exit(1)
	}

	// Load configuration
	cfg, err := config.Load(configFile)
	if err != nil {
		result := types.DeployResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to load config: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	// Update is essentially a re-deployment with the new config
	// The Deploy function is idempotent, so it will update what needs updating
	result := deploy.Deploy(cfg)

	// Output result as JSON
	json.NewEncoder(os.Stdout).Encode(result)

	if !result.Success {
		os.Exit(1)
	}
}

func handleHarden() {
	var configFile string

	fs := flag.NewFlagSet("harden", flag.ExitOnError)
	fs.StringVar(&configFile, "config", "", "Path to hardening configuration file")
	fs.Parse(os.Args[2:])

	if configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --config is required for harden command\n")
		os.Exit(1)
	}

	// Load hardening configuration
	data, err := os.ReadFile(configFile)
	if err != nil {
		result := types.HardenResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to read config file: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	var cfg types.HardenConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		result := types.HardenResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to parse config: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	// Execute hardening
	result := harden.Harden(&cfg)

	// Output result as JSON
	json.NewEncoder(os.Stdout).Encode(result)

	if !result.Success {
		os.Exit(1)
	}
}

func handleCleanup() {
	var configFile string

	fs := flag.NewFlagSet("cleanup", flag.ExitOnError)
	fs.StringVar(&configFile, "config", "", "Path to cleanup configuration file")
	fs.Parse(os.Args[2:])

	if configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --config is required for cleanup command\n")
		os.Exit(1)
	}

	// Load cleanup configuration
	data, err := os.ReadFile(configFile)
	if err != nil {
		result := types.CleanupResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to read config file: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	var cfg types.CleanupConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		result := types.CleanupResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to parse config: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	// Execute cleanup
	result := cleanup.Cleanup(&cfg)

	// Output result as JSON
	json.NewEncoder(os.Stdout).Encode(result)

	if !result.Success {
		os.Exit(1)
	}
}

func handleDestroy() {
	var configFile string

	fs := flag.NewFlagSet("destroy", flag.ExitOnError)
	fs.StringVar(&configFile, "config", "", "Path to destroy configuration file")
	fs.Parse(os.Args[2:])

	if configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --config is required for destroy command\n")
		os.Exit(1)
	}

	// Load destroy configuration
	data, err := os.ReadFile(configFile)
	if err != nil {
		result := types.DestroyResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to read config file: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	var cfg types.DestroyConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		result := types.DestroyResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to parse config: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	// Execute destroy
	result := destroy.Destroy(&cfg)

	// Output result as JSON
	json.NewEncoder(os.Stdout).Encode(result)

	if !result.Success {
		os.Exit(1)
	}
}

func handleBackup() {
	var configFile string

	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	fs.StringVar(&configFile, "config", "", "Path to backup configuration file")
	fs.Parse(os.Args[2:])

	if configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --config is required for backup command\n")
		os.Exit(1)
	}

	// Load backup configuration
	data, err := os.ReadFile(configFile)
	if err != nil {
		result := types.BackupResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to read config file: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	var cfg types.BackupConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		result := types.BackupResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to parse config: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	// Execute backup
	result := backup.Backup(cfg)

	// Output result as JSON
	json.NewEncoder(os.Stdout).Encode(result)

	if !result.Success {
		os.Exit(1)
	}
}

func handleRestore() {
	var configFile string

	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	fs.StringVar(&configFile, "config", "", "Path to restore configuration file")
	fs.Parse(os.Args[2:])

	if configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --config is required for restore command\n")
		os.Exit(1)
	}

	// Load restore configuration
	data, err := os.ReadFile(configFile)
	if err != nil {
		result := types.RestoreResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to read config file: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	var cfg types.RestoreConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		result := types.RestoreResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to parse config: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	// Execute restore
	result := backup.Restore(cfg)

	// Output result as JSON
	json.NewEncoder(os.Stdout).Encode(result)

	if !result.Success {
		os.Exit(1)
	}
}

func handleVerify() {
	var configFile string

	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	fs.StringVar(&configFile, "config", "", "Path to verify configuration file")
	fs.Parse(os.Args[2:])

	if configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --config is required for verify command\n")
		os.Exit(1)
	}

	// Load verify configuration
	data, err := os.ReadFile(configFile)
	if err != nil {
		result := types.VerifyBackupResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to read config file: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	var cfg types.VerifyBackupConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		result := types.VerifyBackupResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to parse config: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	// Execute verification
	result := backup.Verify(cfg)

	// Output result as JSON
	json.NewEncoder(os.Stdout).Encode(result)

	if !result.Success {
		os.Exit(1)
	}
}

func handleUpgrade() {
	var configFile string

	fs := flag.NewFlagSet("upgrade", flag.ExitOnError)
	fs.StringVar(&configFile, "config", "", "Path to upgrade configuration file")
	fs.Parse(os.Args[2:])

	if configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --config is required for upgrade command\n")
		os.Exit(1)
	}

	// Load upgrade configuration
	data, err := os.ReadFile(configFile)
	if err != nil {
		result := types.UpgradeResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to read config file: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	var cfg types.UpgradeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		result := types.UpgradeResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to parse config: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	// Execute upgrade
	result := upgrade.Upgrade(cfg)

	// Output result as JSON
	json.NewEncoder(os.Stdout).Encode(result)

	if !result.Success {
		os.Exit(1)
	}
}

func handleEnableTLS() {
	var configFile string

	fs := flag.NewFlagSet("enable-tls", flag.ExitOnError)
	fs.StringVar(&configFile, "config", "", "Path to TLS configuration file")
	fs.Parse(os.Args[2:])

	if configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --config is required for enable-tls command\n")
		os.Exit(1)
	}

	// Load TLS configuration
	data, err := os.ReadFile(configFile)
	if err != nil {
		result := types.EnableTLSResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to read config file: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	var cfg types.EnableTLSConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		result := types.EnableTLSResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to parse config: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	// Execute TLS enablement
	result := enabletls.EnableTLS(&cfg)

	// Output result as JSON
	json.NewEncoder(os.Stdout).Encode(result)

	if !result.Success {
		os.Exit(1)
	}
}

func handleRotateCredentials() {
	var configFile string

	fs := flag.NewFlagSet("rotate-credentials", flag.ExitOnError)
	fs.StringVar(&configFile, "config", "", "Path to rotation configuration file")
	fs.Parse(os.Args[2:])

	if configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --config is required for rotate-credentials command\n")
		os.Exit(1)
	}

	// Load rotation configuration
	data, err := os.ReadFile(configFile)
	if err != nil {
		result := types.RotateCredentialsResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to read config file: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	var cfg types.RotateCredentialsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		result := types.RotateCredentialsResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to parse config: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	// Execute credential rotation
	result := rotatecreds.RotateCredentials(&cfg)

	// Output result as JSON
	json.NewEncoder(os.Stdout).Encode(result)

	if !result.Success {
		os.Exit(1)
	}
}

func handleOpenPort() {
	var configFile string

	fs := flag.NewFlagSet("open-port", flag.ExitOnError)
	fs.StringVar(&configFile, "config", "", "Path to open-port configuration file")
	fs.Parse(os.Args[2:])

	if configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --config is required for open-port command\n")
		os.Exit(1)
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		result := types.OpenPortResult{
			ErrorMessage: fmt.Sprintf("Failed to read config file: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	var cfg types.OpenPortConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		result := types.OpenPortResult{
			ErrorMessage: fmt.Sprintf("Failed to parse config: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	result := openport.OpenPort(&cfg)
	json.NewEncoder(os.Stdout).Encode(result)

	if !result.Success {
		os.Exit(1)
	}
}

func handleResetVM() {
	var configFile string

	fs := flag.NewFlagSet("reset-vm", flag.ExitOnError)
	fs.StringVar(&configFile, "config", "", "Path to reset-vm configuration file")
	fs.Parse(os.Args[2:])

	var cfg types.ResetVMConfig
	if configFile != "" {
		data, err := os.ReadFile(configFile)
		if err != nil {
			result := types.ResetVMResult{
				ErrorMessage: fmt.Sprintf("Failed to read config file: %v", err),
				FailedStep:   "load_config",
			}
			json.NewEncoder(os.Stdout).Encode(result)
			os.Exit(1)
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			result := types.ResetVMResult{
				ErrorMessage: fmt.Sprintf("Failed to parse config: %v", err),
				FailedStep:   "load_config",
			}
			json.NewEncoder(os.Stdout).Encode(result)
			os.Exit(1)
		}
	}

	// An empty config is valid — defaults apply. Supports `vmkit-agent reset-vm`
	// with no args for ad-hoc operator use.
	result := resetvm.Reset(&cfg)
	json.NewEncoder(os.Stdout).Encode(result)

	if !result.Success {
		os.Exit(1)
	}
}

func handleUpdateAllowlist() {
	var configFile string

	fs := flag.NewFlagSet("update-allowlist", flag.ExitOnError)
	fs.StringVar(&configFile, "config", "", "Path to update-allowlist configuration file")
	fs.Parse(os.Args[2:])

	if configFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --config is required for update-allowlist command\n")
		os.Exit(1)
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		result := types.UpdateAllowlistResult{
			ErrorMessage: fmt.Sprintf("Failed to read config file: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	var cfg types.UpdateAllowlistConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		result := types.UpdateAllowlistResult{
			ErrorMessage: fmt.Sprintf("Failed to parse config: %v", err),
		}
		json.NewEncoder(os.Stdout).Encode(result)
		os.Exit(1)
	}

	result := openport.UpdateAllowlist(&cfg)
	json.NewEncoder(os.Stdout).Encode(result)

	if !result.Success {
		os.Exit(1)
	}
}
