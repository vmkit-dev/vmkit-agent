package types

// DeployConfig represents the deployment configuration
type DeployConfig struct {
	InstanceID       string            `json:"instance_id"`
	Subdomain        string            `json:"subdomain"`
	Domain           string            `json:"domain"`
	StudioDomain     string            `json:"studio_domain,omitempty"`
	BaseDir          string            `json:"base_dir"`
	EnableTLS        bool              `json:"enable_tls"`
	VMUser           string            `json:"vm_user"`              // VM username for docker group
	KongPort         int               `json:"kong_port,omitempty"`  // Default: 8000
	PostgresPort     int               `json:"postgres_port,omitempty"` // Default: 5432
	StudioPort       int               `json:"studio_port,omitempty"`  // Default: 3000
	PostgresPassword string            `json:"postgres_password"`
	JWTSecret        string            `json:"jwt_secret"`
	PublishableKey   string            `json:"publishable_key"`      // Anon key
	SecretKey        string            `json:"secret_key"`           // Service role key
	PgsodiumKey        string            `json:"pgsodium_key,omitempty"`
	DashboardUsername  string            `json:"dashboard_username,omitempty"`  // Kong basic-auth username for Studio
	DashboardPassword  string            `json:"dashboard_password,omitempty"`  // Kong basic-auth password for Studio
	IsFirstInstance    bool              `json:"is_first_instance,omitempty"`
	CertPEM          string            `json:"cert_pem,omitempty"`          // TLS certificate for Postgres SSL
	KeyPEM           string            `json:"key_pem,omitempty"`           // TLS private key for Postgres SSL
	PostgresImage    string            `json:"postgres_image,omitempty"`    // Postgres image ref, e.g. "supabase/postgres:15.8.1.020". Used to detect postgres UID for SSL cert ownership.
	ComposeContent   string            `json:"compose_content,omitempty"`   // Pre-rendered docker-compose.yml from backend
	EnvContent       string            `json:"env_content,omitempty"`       // Pre-rendered .env file from backend
	KongContent      string            `json:"kong_content,omitempty"`      // Pre-rendered kong.yml from backend
	// Services lists the expected container-name *suffixes* to health-check,
	// e.g. ["db","kong","studio","auth","rest","realtime","storage",
	// "imgproxy","meta","functions","analytics","vector","supavisor"].
	// The agent prefixes each with Subdomain to get the container name
	// (`{Subdomain}-{suffix}`). Populated by backend based on the target
	// Supabase version — v1.26.04 lists all 13 upstream services; older
	// versions leave this empty and the agent falls back to its legacy
	// hardcoded [postgres, kong, studio] triad.
	Services         []string          `json:"services,omitempty"`
	// PostgresContainer is the explicit postgres container name suffix
	// used by checkPostgres for `docker exec pg_isready`. Defaults to
	// "postgres" (legacy 1.24.0 supabyoi-authored compose). v1.26.04
	// upstream uses "db".
	PostgresContainer string           `json:"postgres_container,omitempty"`
	// InitSQLFiles maps filename -> SQL/shell content for
	// docker-entrypoint-initdb.d scripts. v1.26.04 ships 7 upstream files
	// (_supabase.sql, jwt.sql, logs.sql, pooler.sql, realtime.sql,
	// roles.sql, webhooks.sql). When empty, the agent falls back to its
	// legacy roles.sh for v1.24.0 back-compat.
	InitSQLFiles     map[string]string `json:"init_sql_files,omitempty"`
	// VolumeConfigFiles maps relative path (under instance dir) -> file
	// content for extra volume-mounted configs the upstream compose
	// bind-mounts but are not init SQL. v1.26.04 ships volumes/logs/vector.yml
	// and volumes/pooler/pooler.exs. Keys must be relative paths starting
	// with "volumes/".
	VolumeConfigFiles map[string]string `json:"volume_config_files,omitempty"`
	// SupabaseExtras is the bundled-secrets map fetched from Vault for
	// v1.26.04 (RS256 keys, logflare tokens, supavisor tenant id, etc.).
	// The backend substitutes these into the rendered compose/env before
	// sending, so the agent itself doesn't consume them — the field exists
	// solely to keep the JSON-schema contract honest when the backend
	// chooses to echo the bundle for debugging or future agent-side use.
	SupabaseExtras   map[string]string `json:"supabase_extras,omitempty"`
	Environment      map[string]string `json:"environment"`
}

// DeployResult represents the result of a deployment operation
type DeployResult struct {
	Success      bool              `json:"success"`
	InstanceID   string            `json:"instance_id"`
	Steps        []StepResult      `json:"steps"`
	ErrorMessage string            `json:"error_message,omitempty"`
	FailedStep   string            `json:"failed_step,omitempty"`
}

// StepResult represents the result of a deployment step
type StepResult struct {
	Name      string `json:"name"`
	Status    string `json:"status"` // in_progress, completed, failed
	Message   string `json:"message,omitempty"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time,omitempty"`
	Duration  string `json:"duration,omitempty"`
}

// InstanceStatus represents the current status of an instance
type InstanceStatus struct {
	InstanceID     string              `json:"instance_id"`
	State          string              `json:"state"` // running, stopped, error
	Containers     []ContainerStatus   `json:"containers"`
	HealthChecks   map[string]bool     `json:"health_checks"`
	LastChecked    string              `json:"last_checked"`
}

// ContainerStatus represents the status of a Docker container
type ContainerStatus struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	Health  string `json:"health,omitempty"`
	Uptime  string `json:"uptime,omitempty"`
}

// HardenConfig represents the VM hardening configuration
type HardenConfig struct {
	VMUser              string   `json:"vm_user"`                         // User to create (default: supabyoi)
	SSHPort             int      `json:"ssh_port"`                        // New SSH port (default: 2222)
	SSHPublicKey        string   `json:"ssh_public_key"`                  // Primary SSH public key for the new user
	ExtraAuthorizedKeys []string `json:"extra_authorized_keys,omitempty"` // Extra pubkeys to append to authorized_keys (operator debug keys)
	EnableFirewall      bool     `json:"enable_firewall"`                 // Enable UFW firewall
	EnableFail2Ban      bool     `json:"enable_fail2ban"`                 // Enable fail2ban
	EnableAutoUpdates   bool     `json:"enable_auto_updates"`             // Enable automatic security updates
}

// HardenResult represents the result of a VM hardening operation
type HardenResult struct {
	Success         bool         `json:"success"`
	Steps           []StepResult `json:"steps"`
	ErrorMessage    string       `json:"error_message,omitempty"`
	FailedStep      string       `json:"failed_step,omitempty"`
	RollbackApplied bool         `json:"rollback_applied,omitempty"`
	RollbackDetails string       `json:"rollback_details,omitempty"`
}

// CleanupLevel represents the level of cleanup to perform
type CleanupLevel string

const (
	CleanupMinimal CleanupLevel = "minimal" // Remove Supabyoi files only
	CleanupFull    CleanupLevel = "full"    // Remove files + user + configs
	CleanupRevert  CleanupLevel = "revert"  // Full cleanup + restore original SSH settings
)

// CleanupConfig represents the VM cleanup configuration
type CleanupConfig struct {
	Level                CleanupLevel `json:"level"`                            // Cleanup level: minimal, full, revert
	PreHardeningSSHPort  int          `json:"pre_hardening_ssh_port,omitempty"` // Original SSH port (for revert)
	PreHardeningSSHUser  string       `json:"pre_hardening_ssh_user,omitempty"` // Original SSH user (for revert)
	RestorePasswordAuth  bool         `json:"restore_password_auth"`            // Re-enable password auth (for revert)
	DisableFirewall      bool         `json:"disable_firewall"`                 // Disable UFW firewall (for revert)
}

// CleanupResult represents the result of a VM cleanup operation
type CleanupResult struct {
	Success         bool         `json:"success"`
	Level           string       `json:"level"`
	Steps           []StepResult `json:"steps"`
	ErrorMessage    string       `json:"error_message,omitempty"`
	FailedStep      string       `json:"failed_step,omitempty"`
	RestoredSSHPort int          `json:"restored_ssh_port,omitempty"` // SSH port after cleanup
	RestoredSSHUser string       `json:"restored_ssh_user,omitempty"` // SSH user after cleanup
}

// DestroyConfig represents the instance destruction configuration
type DestroyConfig struct {
	InstanceID string `json:"instance_id"`
	Subdomain  string `json:"subdomain"`
	BaseDir    string `json:"base_dir"` // Base directory for instances (default: /opt/supabase)
	// PostgresPort tells the agent to also remove all UFW ALLOW rules for
	// this port on this VM (any source). Zero means "don't touch the
	// firewall" for back-compat with pre-0.12.2 callers. See supabyoi-v3bm.
	PostgresPort int `json:"postgres_port,omitempty"`
}

// DestroyResult represents the result of an instance destruction operation
type DestroyResult struct {
	Success      bool         `json:"success"`
	InstanceID   string       `json:"instance_id"`
	Steps        []StepResult `json:"steps"`
	ErrorMessage string       `json:"error_message,omitempty"`
	FailedStep   string       `json:"failed_step,omitempty"`
}

// BackupConfig represents the backup configuration
type BackupConfig struct {
	InstanceID       string `json:"instance_id"`
	Subdomain        string `json:"subdomain"`
	BaseDir          string `json:"base_dir"`           // Base directory for instances (default: /opt/supabase)
	UploadURL        string `json:"upload_url"`         // Pre-signed URL for database backup upload
	StorageUploadURL string `json:"storage_upload_url"` // Pre-signed URL for storage backup upload
	IncludeStorage   bool   `json:"include_storage"`    // Whether to backup storage files
}

// BackupMetadata represents backup metadata
type BackupMetadata struct {
	BackupID            string `json:"backup_id"`
	InstanceID          string `json:"instance_id"`
	Subdomain           string `json:"subdomain"`
	SupabaseVersion     string `json:"supabase_version"`
	Timestamp           string `json:"timestamp"`
	SizeBytes           int64  `json:"size_bytes"`
	Checksum            string `json:"checksum"`
	StorageSizeBytes    int64  `json:"storage_size_bytes,omitempty"`
	StorageChecksum     string `json:"storage_checksum,omitempty"`
	Status              string `json:"status"`
}

// BackupResult represents the result of a backup operation
type BackupResult struct {
	Success      bool           `json:"success"`
	BackupID     string         `json:"backup_id,omitempty"`
	InstanceID   string         `json:"instance_id"`
	Steps        []StepResult   `json:"steps"`
	Metadata     BackupMetadata `json:"metadata,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	FailedStep   string         `json:"failed_step,omitempty"`
}

// RestoreConfig represents the restore configuration
type RestoreConfig struct {
	InstanceID       string `json:"instance_id"`
	Subdomain        string `json:"subdomain"`
	BaseDir          string `json:"base_dir"`           // Base directory for instances (default: /opt/supabase)
	DownloadURL      string `json:"download_url"`       // Pre-signed URL for database backup download
	StorageDownloadURL string `json:"storage_download_url,omitempty"` // Pre-signed URL for storage backup download
	StopInstance     bool   `json:"stop_instance"`      // Whether to stop instance before restore
	VerifyChecksum   bool   `json:"verify_checksum"`    // Whether to verify backup integrity
	ExpectedChecksum string `json:"expected_checksum,omitempty"` // Expected checksum for verification
}

// RestoreResult represents the result of a restore operation
type RestoreResult struct {
	Success      bool         `json:"success"`
	InstanceID   string       `json:"instance_id"`
	Steps        []StepResult `json:"steps"`
	ErrorMessage string       `json:"error_message,omitempty"`
	FailedStep   string       `json:"failed_step,omitempty"`
}

// BackupListConfig represents the configuration for listing backups
type BackupListConfig struct {
	InstanceID string `json:"instance_id"`
	Subdomain  string `json:"subdomain"`
	BaseDir    string `json:"base_dir"`
}

// BackupInfo represents information about a single backup
type BackupInfo struct {
	Filename      string `json:"filename"`
	SizeBytes     int64  `json:"size_bytes"`
	Timestamp     string `json:"timestamp"`
	Path          string `json:"path"`
}

// UpgradeConfig represents the configuration for upgrading an instance
type UpgradeConfig struct {
	InstanceID          string `json:"instance_id"`
	Subdomain           string `json:"subdomain"`
	BaseDir             string `json:"base_dir"`                     // Base directory for instances (default: /opt/supabase)
	CurrentVersion      string `json:"current_version"`              // Current Supabase version
	TargetVersion       string `json:"target_version"`               // Target Supabase version
	BackupBeforeUpgrade bool   `json:"backup_before_upgrade"`        // Create backup before upgrade
	RollbackOnFailure   bool   `json:"rollback_on_failure"`          // Rollback to previous version on failure
	BackupUploadURL     string `json:"backup_upload_url,omitempty"`  // Pre-signed URL for backup upload
	StorageUploadURL    string `json:"storage_upload_url,omitempty"` // Pre-signed URL for storage backup
	ComposeContent      string `json:"compose_content,omitempty"`    // Pre-rendered docker-compose.yml from backend
	EnvContent          string `json:"env_content,omitempty"`        // Pre-rendered .env file from backend
}

// UpgradeResult represents the result of an upgrade operation
type UpgradeResult struct {
	Success         bool           `json:"success"`
	InstanceID      string         `json:"instance_id"`
	UpgradedFrom    string         `json:"upgraded_from,omitempty"`
	UpgradedTo      string         `json:"upgraded_to,omitempty"`
	Steps           []StepResult   `json:"steps"`
	RollbackApplied bool           `json:"rollback_applied"`
	BackupID        string         `json:"backup_id,omitempty"`
	ErrorMessage    string         `json:"error_message,omitempty"`
	FailedStep      string         `json:"failed_step,omitempty"`
}

// VersionInfo represents version information for Supabase components
type VersionInfo struct {
	Postgres string `json:"postgres"`
	Kong     string `json:"kong"`
	Studio   string `json:"studio"`
	GoTrue   string `json:"gotrue,omitempty"`
	PostgREST string `json:"postgrest,omitempty"`
	Realtime string `json:"realtime,omitempty"`
	Storage  string `json:"storage,omitempty"`
}

// VerifyBackupConfig represents the configuration for verifying a backup
type VerifyBackupConfig struct {
	BackupID         string `json:"backup_id"`
	DownloadURL      string `json:"download_url"`          // Pre-signed URL to download backup
	ExpectedChecksum string `json:"expected_checksum"`     // Expected SHA256 checksum
	DeepVerify       bool   `json:"deep_verify"`           // Perform deep verification (test restore)
	Subdomain        string `json:"subdomain,omitempty"`   // Required for deep verify
	BaseDir          string `json:"base_dir,omitempty"`    // Base directory (default: /opt/supabase)
}

// VerificationCheck represents a single verification check result
type VerificationCheck struct {
	Name     string `json:"name"`
	Status   string `json:"status"` // passed, failed, skipped
	Message  string `json:"message"`
	Duration string `json:"duration,omitempty"`
}

// VerifyBackupResult represents the result of a backup verification
type VerifyBackupResult struct {
	Success       bool                `json:"success"`
	BackupID      string              `json:"backup_id"`
	VerifiedAt    string              `json:"verified_at"`
	Checks        []VerificationCheck `json:"checks"`
	OverallStatus string              `json:"overall_status"` // verified, failed, partial
	ErrorMessage  string              `json:"error_message,omitempty"`
	Duration      string              `json:"duration"`
}

// HealthCheckLevel represents the depth of health checks to perform
type HealthCheckLevel string

const (
	HealthCheckBasic    HealthCheckLevel = "basic"    // Basic container and service status
	HealthCheckDeep     HealthCheckLevel = "deep"     // Deep diagnostics with endpoint checks
	HealthCheckMetrics  HealthCheckLevel = "metrics"  // Include performance metrics
	HealthCheckServices HealthCheckLevel = "services" // Service-specific diagnostics
	HealthCheckFull     HealthCheckLevel = "full"     // All checks combined
)

// EnhancedHealthCheckConfig represents configuration for enhanced health checks
type EnhancedHealthCheckConfig struct {
	InstanceID string           `json:"instance_id"`
	Subdomain  string           `json:"subdomain"`
	BaseDir    string           `json:"base_dir"`
	Level      HealthCheckLevel `json:"level"` // basic, deep, metrics, services, full
	Timeout    int              `json:"timeout"` // Timeout in seconds
}

// ContainerMetrics represents resource usage metrics for a container
type ContainerMetrics struct {
	Name         string  `json:"name"`
	CPUPercent   float64 `json:"cpu_percent"`
	MemoryMB     int64   `json:"memory_mb"`
	MemoryPercent float64 `json:"memory_percent"`
	DiskMB       int64   `json:"disk_mb,omitempty"`
	NetworkRxMB  float64 `json:"network_rx_mb,omitempty"`
	NetworkTxMB  float64 `json:"network_tx_mb,omitempty"`
}

// ServiceDiagnostics represents service-specific diagnostic information
type ServiceDiagnostics struct {
	Name           string            `json:"name"`
	Status         string            `json:"status"` // healthy, degraded, unhealthy
	ResponseTimeMS int64             `json:"response_time_ms,omitempty"`
	Details        map[string]string `json:"details,omitempty"`
	Errors         []string          `json:"errors,omitempty"`
}

// HealthCheck represents a single health check result
type HealthCheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"` // passed, failed, warning
	Message    string `json:"message"`
	DurationMS int64  `json:"duration_ms"`
}

// EnhancedContainerStatus represents detailed container status
type EnhancedContainerStatus struct {
	Name     string  `json:"name"`
	Status   string  `json:"status"` // running, stopped, error
	Health   string  `json:"health,omitempty"` // healthy, unhealthy, starting
	Uptime   string  `json:"uptime,omitempty"`
	Restarts int     `json:"restarts"`
	Metrics  *ContainerMetrics `json:"metrics,omitempty"`
}

// EnhancedHealthCheckResult represents comprehensive health check results
type EnhancedHealthCheckResult struct {
	InstanceID     string                     `json:"instance_id"`
	OverallStatus  string                     `json:"overall_status"` // healthy, degraded, unhealthy
	Timestamp      string                     `json:"timestamp"`
	Containers     []EnhancedContainerStatus  `json:"containers"`
	Services       []ServiceDiagnostics       `json:"services,omitempty"`
	Checks         []HealthCheck              `json:"checks"`
	Duration       string                     `json:"duration"`
	Level          string                     `json:"level"` // What level of checks was performed
}

// EnableTLSConfig represents the configuration for enabling TLS on an instance
type EnableTLSConfig struct {
	InstanceID   string `json:"instance_id"`
	Subdomain    string `json:"subdomain"`
	Domain       string `json:"domain"`
	StudioDomain string `json:"studio_domain,omitempty"`
	BaseDir      string `json:"base_dir,omitempty"`      // Base directory for instances (default: /opt/supabase)
	CertPEM      string `json:"cert_pem"`                // PEM-encoded certificate from Origin CA
	KeyPEM       string `json:"key_pem"`                 // PEM-encoded private key
	KongPort     int    `json:"kong_port,omitempty"`     // Default: 8000
	StudioPort   int    `json:"studio_port,omitempty"`   // Default: 3000
	// Studio basic-auth credentials. When both are set the agent regenerates
	// the per-instance htpasswd file and the studio nginx server block enforces
	// basic auth. Without these, studio is publicly accessible — see bead supabyoi-f8w0.
	DashboardUsername string `json:"dashboard_username,omitempty"`
	DashboardPassword string `json:"dashboard_password,omitempty"`
}

// EnableTLSResult represents the result of enabling TLS
type EnableTLSResult struct {
	Success      bool         `json:"success"`
	InstanceID   string       `json:"instance_id"`
	Steps        []StepResult `json:"steps"`
	ErrorMessage string       `json:"error_message,omitempty"`
	FailedStep   string       `json:"failed_step,omitempty"`
}

// RotateCredentialsConfig represents the configuration for rotating credentials
type RotateCredentialsConfig struct {
	InstanceID       string `json:"instance_id"`
	CredentialType   string `json:"credential_type"`   // postgres, jwt, api_keys, or all
	PostgresPassword string `json:"postgres_password"` // New postgres password
	JWTSecret        string `json:"jwt_secret"`        // New JWT secret
	PublishableKey   string `json:"publishable_key"`   // New anon/publishable key
	SecretKey        string `json:"secret_key"`        // New service role key
	BaseDir          string `json:"base_dir,omitempty"` // Base directory for instances (default: /opt/supabase)
}

// RotateCredentialsResult represents the result of a credential rotation
type RotateCredentialsResult struct {
	Success      bool         `json:"success"`
	InstanceID   string       `json:"instance_id"`
	Steps        []StepResult `json:"steps"`
	ErrorMessage string       `json:"error_message,omitempty"`
	FailedStep   string       `json:"failed_step,omitempty"`
}

// DiagnoseConfig represents the configuration for running diagnostics
type DiagnoseConfig struct {
	InstanceID string `json:"instance_id"`
	Subdomain  string `json:"subdomain"`
	BaseDir    string `json:"base_dir"`
	Timeout    int    `json:"timeout"`
}

// DiagnosticCheck represents a single diagnostic check result
type DiagnosticCheck struct {
	Name     string `json:"name"`
	Status   string `json:"status"` // pass, warn, fail
	Detail   string `json:"detail"`
	Duration string `json:"duration,omitempty"`
}

// DiagnoseResult represents the result of a diagnose operation
type DiagnoseResult struct {
	InstanceID    string            `json:"instance_id"`
	OverallStatus string            `json:"overall_status"` // pass, warn, fail
	Checks        []DiagnosticCheck `json:"checks"`
	Timestamp     string            `json:"timestamp"`
	Duration      string            `json:"duration"`
	AgentVersion  string            `json:"agent_version"`
}

// OpenPortConfig represents the configuration for opening a firewall port
type OpenPortConfig struct {
	Port     int    `json:"port"`              // Port number to open
	Protocol string `json:"protocol,omitempty"` // tcp or udp (default: tcp)
	// SourceCIDRs narrows the UFW rule to the given sources. When empty or
	// missing the agent writes a single ALLOW-FROM-ANYWHERE rule, matching
	// pre-0.12.0 behavior. When provided the agent reconciles the rules so
	// only those sources are allowed (supabyoi-g6cd phase 3).
	SourceCIDRs []string `json:"source_cidrs,omitempty"`
}

// OpenPortResult represents the result of opening a firewall port
type OpenPortResult struct {
	Success      bool     `json:"success"`
	Port         int      `json:"port"`
	Protocol     string   `json:"protocol"`
	AllowedCIDRs []string `json:"allowed_cidrs,omitempty"` // Rules actually written
	ErrorMessage string   `json:"error_message,omitempty"`
}

// UpdateAllowlistConfig narrows the source IPs allowed to reach a port that
// was previously opened. The agent reconciles UFW rules for the port so that
// only SourceCIDRs are allowed; existing rules for the port that aren't in
// the list are removed. Empty SourceCIDRs means "publicly reachable" and
// writes a single ALLOW-FROM-ANYWHERE rule.
type UpdateAllowlistConfig struct {
	Port        int      `json:"port"`
	Protocol    string   `json:"protocol,omitempty"`
	SourceCIDRs []string `json:"source_cidrs"`
}

// UpdateAllowlistResult reports the outcome of an allowlist reconcile.
type UpdateAllowlistResult struct {
	Success      bool     `json:"success"`
	Port         int      `json:"port"`
	Protocol     string   `json:"protocol"`
	AllowedCIDRs []string `json:"allowed_cidrs,omitempty"` // Rules in place after reconcile
	AddedCIDRs   []string `json:"added_cidrs,omitempty"`
	RemovedCIDRs []string `json:"removed_cidrs,omitempty"`
	ErrorMessage string   `json:"error_message,omitempty"`
}

// ResetVMConfig drives the reset-vm command, which restores a shared VM to
// its just-hardened baseline: every deployed instance under BaseDir is purged,
// Docker state pruned, UFW rules outside AllowedPorts deleted, and stray
// config files (/tmp/supabyoi-*.json, /etc/supabyoi/backup-*.env) removed.
// Idempotent and safe to re-run. Does NOT re-harden — the agent binary, the
// hardened user, SSH settings, and cron are left alone.
//
// Callers should include the live SSH port in AllowedPorts (hardening typically
// moves SSH off :22 to 2222), otherwise a blind default risks locking the
// operator out. When AllowedPorts is empty the agent falls back to [22, 80, 443].
type ResetVMConfig struct {
	BaseDir      string `json:"base_dir,omitempty"`      // Default: /opt/supabase
	AllowedPorts []int  `json:"allowed_ports,omitempty"` // UFW ports to keep (default: [22, 80, 443])
}

// ResetVMResult reports what was purged during a reset.
type ResetVMResult struct {
	Success            bool         `json:"success"`
	Steps              []StepResult `json:"steps"`
	PurgedInstances    []string     `json:"purged_instances,omitempty"`    // Subdomains under BaseDir that were torn down
	RemovedUFWRules    []int        `json:"removed_ufw_rules,omitempty"`   // UFW rule numbers deleted (as they were at parse time)
	RemovedConfigFiles []string     `json:"removed_config_files,omitempty"` // Paths of stray config files removed
	ErrorMessage       string       `json:"error_message,omitempty"`
	FailedStep         string       `json:"failed_step,omitempty"`
}
