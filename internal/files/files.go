package files

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

// postgresUIDRegex matches `uid=N` / `gid=N` output from `id` inside the
// postgres container, so we can detect the UID/GID the image uses at runtime
// instead of hardcoding it.
var postgresUIDRegex = regexp.MustCompile(`uid=(\d+).*gid=(\d+)`)

const (
	// DefaultFileMode is the default permission for config files (644)
	DefaultFileMode = 0644
	// DefaultDirMode is the default permission for directories (755)
	DefaultDirMode = 0755
)

// Manager handles directory structure and file operations for instances
type Manager struct {
	baseDir    string
	instanceID string
	username   string
}

// New creates a new file manager for an instance
func New(instanceID, baseDir, username string) *Manager {
	return &Manager{
		baseDir:    baseDir,
		instanceID: instanceID,
		username:   username,
	}
}

// GetInstanceDir returns the full path to the instance directory
func (m *Manager) GetInstanceDir() string {
	return filepath.Join(m.baseDir, m.instanceID)
}

// CreateDirectoryStructure creates the complete directory structure for an instance
// This is idempotent - safe to call multiple times
func (m *Manager) CreateDirectoryStructure() error {
	instanceDir := m.GetInstanceDir()

	// Define the directory structure. v1.26.04 upstream compose adds
	// volumes/db/init (for 7 upstream init SQL scripts), volumes/pooler
	// (for pooler.exs), and volumes/functions (for edge-function source).
	// Extra dirs are harmless on v1.24.0 deploys.
	dirs := []string{
		instanceDir,
		filepath.Join(instanceDir, "volumes"),
		filepath.Join(instanceDir, "volumes", "db"),
		filepath.Join(instanceDir, "volumes", "db", "data"),
		filepath.Join(instanceDir, "volumes", "db", "init"),
		filepath.Join(instanceDir, "volumes", "storage"),
		filepath.Join(instanceDir, "volumes", "api"),
		filepath.Join(instanceDir, "volumes", "snippets"),
		filepath.Join(instanceDir, "volumes", "logs"),
		filepath.Join(instanceDir, "volumes", "pooler"),
		filepath.Join(instanceDir, "volumes", "functions"),
	}

	// Create each directory
	for _, dir := range dirs {
		// Check if directory already exists (idempotent)
		if dirExists(dir) {
			continue
		}

		// Create with sudo for permission
		cmd := exec.Command("sudo", "mkdir", "-p", dir)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Set ownership to user for SFTP access. The postgres image is empty
	// here because CreateDirectoryStructure runs before config files exist;
	// postgres uid restoration happens later in WritePostgresSSLCerts /
	// post-config ownership passes.
	if err := m.SetOwnership(""); err != nil {
		return fmt.Errorf("failed to set ownership: %w", err)
	}

	return nil
}

// SetOwnership sets the correct ownership on the instance directory.
// It chowns the instance dir to the VM user for SFTP access, but preserves
// postgres ownership on volumes/db/data/ which the container needs. The
// postgres uid/gid is detected from the image when provided; when empty
// (e.g. during initial directory creation, before the image is known),
// the db data dir is left alone and chowned later alongside the SSL certs.
func (m *Manager) SetOwnership(postgresImage string) error {
	if m.username == "" || m.username == "root" {
		return nil // Skip for root or empty username
	}

	instanceDir := m.GetInstanceDir()
	userGroup := fmt.Sprintf("%s:%s", m.username, m.username)

	// Set ownership recursively on the instance directory
	chownCmd := exec.Command("sudo", "chown", "-R", userGroup, instanceDir)
	if err := chownCmd.Run(); err != nil {
		return fmt.Errorf("failed to set ownership on %s: %w", instanceDir, err)
	}

	// Restore postgres ownership on db data directory using the image's
	// actual uid/gid (differs between postgres 15.1.x and 15.8.x).
	dbDataDir := filepath.Join(instanceDir, "volumes", "db", "data")
	if dirExists(dbDataDir) && postgresImage != "" {
		uidGid, err := GetPostgresUIDGID(postgresImage)
		if err != nil {
			return fmt.Errorf("failed to detect postgres uid/gid for %s: %w", dbDataDir, err)
		}
		restoreCmd := exec.Command("sudo", "chown", "-R", uidGid, dbDataDir)
		if err := restoreCmd.Run(); err != nil {
			return fmt.Errorf("failed to restore postgres ownership on %s: %w", dbDataDir, err)
		}
	}

	return nil
}

// WriteDockerCompose writes the docker-compose.yml file using the
// backend-rendered content. The agent no longer generates compose itself.
func (m *Manager) WriteDockerCompose(config *types.DeployConfig) error {
	if config.ComposeContent == "" {
		return fmt.Errorf("compose_content is required (backend must render the docker-compose.yml)")
	}
	path := filepath.Join(m.GetInstanceDir(), "docker-compose.yml")

	if err := m.writeFile(path, config.ComposeContent, DefaultFileMode); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	return nil
}

// WriteEnvFile writes the .env file using the backend-rendered content.
func (m *Manager) WriteEnvFile(config *types.DeployConfig) error {
	if config.EnvContent == "" {
		return fmt.Errorf("env_content is required (backend must render the .env file)")
	}
	path := filepath.Join(m.GetInstanceDir(), ".env")

	if err := m.writeFile(path, config.EnvContent, DefaultFileMode); err != nil {
		return fmt.Errorf("failed to write .env: %w", err)
	}

	return nil
}

// WriteKongConfig writes the kong.yml configuration and entrypoint script.
// Uses pre-rendered content from backend if available, otherwise falls back to local generation.
func (m *Manager) WriteKongConfig(config *types.DeployConfig) error {
	var content string
	if config.KongContent != "" {
		content = config.KongContent
	} else {
		content = generateKongConfig(config)
	}
	path := filepath.Join(m.GetInstanceDir(), "volumes", "api", "kong.yml")

	if err := m.writeFile(path, content, DefaultFileMode); err != nil {
		return fmt.Errorf("failed to write kong.yml: %w", err)
	}

	entrypoint := generateKongEntrypoint()
	entrypointPath := filepath.Join(m.GetInstanceDir(), "volumes", "api", "kong-entrypoint.sh")

	if err := m.writeFile(entrypointPath, entrypoint, 0755); err != nil {
		return fmt.Errorf("failed to write kong-entrypoint.sh: %w", err)
	}

	return nil
}

// WritePgHbaConfig writes the pg_hba.conf file for PostgreSQL
func (m *Manager) WritePgHbaConfig() error {
	content := generatePgHbaConfig()
	path := filepath.Join(m.GetInstanceDir(), "volumes", "db", "pg_hba.conf")

	if err := m.writeFile(path, content, DefaultFileMode); err != nil {
		return fmt.Errorf("failed to write pg_hba.conf: %w", err)
	}

	return nil
}

// WriteInitSQL writes the database initialization SQL script
func (m *Manager) WriteInitSQL() error {
	content := generateInitSQL()
	path := filepath.Join(m.GetInstanceDir(), "volumes", "db", "init", "roles.sh")

	if err := m.writeFile(path, content, DefaultFileMode); err != nil {
		return fmt.Errorf("failed to write roles.sql: %w", err)
	}

	return nil
}

// WriteInitSQLFiles writes the upstream Supabase init SQL files supplied by
// the backend (v1.26.04+). Each key is a filename like `roles.sql` or
// `_supabase.sql` and the value is the file content. Files are written
// directly under `volumes/db/` with mode 0644.
//
// Path note: the upstream compose uses explicit per-file bind mounts that
// map each `./volumes/db/<name>.sql` on the host to a specific target under
// `/docker-entrypoint-initdb.d/{init-scripts,migrations}/<prefix>-<name>.sql`
// inside the container (with numeric prefixes defining execution order).
// If any source file is missing, docker silently auto-creates it as an
// empty *directory*, which then surfaces inside the container where psql
// fails with "Is a directory" mid-migrate.sh. We therefore write files
// exactly where the compose expects to read them — no `init/` subdir.
func (m *Manager) WriteInitSQLFiles(initFiles map[string]string) error {
	if len(initFiles) == 0 {
		return nil
	}
	dbDir := filepath.Join(m.GetInstanceDir(), "volumes", "db")
	for name, content := range initFiles {
		if strings.Contains(name, "/") || strings.Contains(name, "..") {
			return fmt.Errorf("invalid init file name %q: must not contain path separators", name)
		}
		path := filepath.Join(dbDir, name)
		// A previous deploy attempt may have left an auto-created directory
		// at this path (docker's "missing source" behavior on bind mounts).
		// Remove it so the retry can write a real file. Using RemoveAll via
		// sudo because the VM user may not own the docker-created dir.
		if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
			if err := exec.Command("sudo", "rm", "-rf", path).Run(); err != nil {
				return fmt.Errorf("failed to remove stale directory at %s: %w", path, err)
			}
		}
		if err := m.writeFile(path, content, DefaultFileMode); err != nil {
			return fmt.Errorf("failed to write init file %s: %w", name, err)
		}
	}
	return nil
}

// WriteVolumeConfigFiles writes arbitrary volume-mounted config files
// supplied by the backend. Keys are relative paths under the instance
// directory and must start with `volumes/` (e.g. `volumes/logs/vector.yml`,
// `volumes/pooler/pooler.exs`). v1.26.04 uses this for upstream compose
// bind-mounts that are not init SQL.
func (m *Manager) WriteVolumeConfigFiles(configFiles map[string]string) error {
	if len(configFiles) == 0 {
		return nil
	}
	instanceDir := m.GetInstanceDir()
	for relPath, content := range configFiles {
		if !strings.HasPrefix(relPath, "volumes/") || strings.Contains(relPath, "..") {
			return fmt.Errorf("invalid volume config path %q: must start with 'volumes/' and not contain '..'", relPath)
		}
		path := filepath.Join(instanceDir, relPath)
		if err := m.writeFile(path, content, DefaultFileMode); err != nil {
			return fmt.Errorf("failed to write volume config %s: %w", relPath, err)
		}
	}
	return nil
}

// GetPostgresUIDGID inspects a postgres image to determine the uid/gid that
// the `postgres` user maps to in that image. Returns "<uid>:<gid>" suitable
// for `chown`. Historically supabyoi hardcoded 101:102, but supabase/postgres
// 15.8.x shipped uid 105/gid 106, which broke SSL cert ownership. See bead
// supabyoi-tv64 prereq 1.
//
// Side effect: `docker run` will pull the image if it isn't present on the
// host. That's fine because `docker compose up` would pull it next anyway.
func GetPostgresUIDGID(image string) (string, error) {
	if strings.TrimSpace(image) == "" {
		return "", fmt.Errorf("postgres image is empty")
	}

	cmd := exec.Command("sudo", "docker", "run", "--rm", "--entrypoint", "id", image, "postgres")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to inspect postgres uid in image %s: %w (output: %s)", image, err, strings.TrimSpace(string(out)))
	}

	matches := postgresUIDRegex.FindStringSubmatch(string(out))
	if len(matches) != 3 {
		return "", fmt.Errorf("could not parse uid/gid from `id` output: %s", strings.TrimSpace(string(out)))
	}
	return fmt.Sprintf("%s:%s", matches[1], matches[2]), nil
}

// WritePostgresSSLCerts writes TLS certificate and key for Postgres SSL.
// The key file is set to mode 0600 and owned by the postgres container user,
// whose uid/gid is detected dynamically from the postgres image (because it
// differs between 15.1.x and 15.8.x image series).
func (m *Manager) WritePostgresSSLCerts(certPEM, keyPEM, postgresImage string) error {
	certDir := filepath.Join(m.GetInstanceDir(), "volumes", "db", "certs")

	// Create certs directory
	if err := exec.Command("sudo", "mkdir", "-p", certDir).Run(); err != nil {
		return fmt.Errorf("failed to create certs directory: %w", err)
	}

	certPath := filepath.Join(certDir, "server.crt")
	keyPath := filepath.Join(certDir, "server.key")

	if err := m.writeFile(certPath, certPEM, 0644); err != nil {
		return fmt.Errorf("failed to write server.crt: %w", err)
	}

	if err := m.writeFile(keyPath, keyPEM, 0600); err != nil {
		return fmt.Errorf("failed to write server.key: %w", err)
	}

	uidGid, err := GetPostgresUIDGID(postgresImage)
	if err != nil {
		return fmt.Errorf("failed to detect postgres uid/gid: %w", err)
	}

	if err := exec.Command("sudo", "chown", "-R", uidGid, certDir).Run(); err != nil {
		return fmt.Errorf("failed to set cert ownership to %s: %w", uidGid, err)
	}

	return nil
}

// WriteAllConfigFiles writes all configuration files for an instance
func (m *Manager) WriteAllConfigFiles(config *types.DeployConfig) error {
	// Write docker-compose.yml
	if err := m.WriteDockerCompose(config); err != nil {
		return err
	}

	// Write .env file
	if err := m.WriteEnvFile(config); err != nil {
		return err
	}

	// Write kong.yml
	if err := m.WriteKongConfig(config); err != nil {
		return err
	}

	// Write pg_hba.conf
	if err := m.WritePgHbaConfig(); err != nil {
		return err
	}

	// Write init SQL scripts. For v1.26.04+ the backend supplies the
	// upstream 7-file bundle via InitSQLFiles; for legacy v1.24.0 it's
	// empty and we fall back to the bundled roles.sh password-setter.
	if len(config.InitSQLFiles) > 0 {
		if err := m.WriteInitSQLFiles(config.InitSQLFiles); err != nil {
			return err
		}
	} else {
		if err := m.WriteInitSQL(); err != nil {
			return err
		}
	}

	// Write extra volume-mounted config files (vector.yml, pooler.exs, ...)
	// for upstream compose bind-mounts. No-op for legacy deployments.
	if err := m.WriteVolumeConfigFiles(config.VolumeConfigFiles); err != nil {
		return err
	}

	// Write Postgres SSL certificates if provided
	if config.CertPEM != "" && config.KeyPEM != "" {
		if err := m.WritePostgresSSLCerts(config.CertPEM, config.KeyPEM, config.PostgresImage); err != nil {
			return err
		}
	}

	return nil
}

// writeFile writes content to a file with specified permissions
func (m *Manager) writeFile(path, content string, mode os.FileMode) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if !dirExists(dir) {
		if err := os.MkdirAll(dir, DefaultDirMode); err != nil {
			return fmt.Errorf("failed to create parent directory %s: %w", dir, err)
		}
	}

	// Write file
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		return fmt.Errorf("failed to write file %s: %w", path, err)
	}

	return nil
}

// dirExists checks if a directory exists
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// FileExists checks if a file exists
func FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// generateKongConfig generates the kong.yml configuration.
// Uses $VARIABLE placeholders resolved by kong-entrypoint.sh at container start.
func generateKongConfig(config *types.DeployConfig) string {
	realtimeHost := fmt.Sprintf("realtime-dev.%s-realtime", config.Subdomain)
	return fmt.Sprintf(`_format_version: "2.1"
_transform: true

consumers:
  - username: DASHBOARD
  - username: anon
    keyauth_credentials:
      - key: $SUPABASE_ANON_KEY
      - key: $SUPABASE_PUBLISHABLE_KEY
  - username: service_role
    keyauth_credentials:
      - key: $SUPABASE_SERVICE_KEY
      - key: $SUPABASE_SECRET_KEY

acls:
  - consumer: anon
    group: anon
  - consumer: service_role
    group: admin

basicauth_credentials:
  - consumer: DASHBOARD
    username: '$DASHBOARD_USERNAME'
    password: '$DASHBOARD_PASSWORD'

services:
  ## Open Auth routes
  - name: auth-v1-open
    url: http://auth:9999/verify
    routes:
      - name: auth-v1-open
        strip_path: true
        paths:
          - /auth/v1/verify
    plugins:
      - name: cors

  - name: auth-v1-open-callback
    url: http://auth:9999/callback
    routes:
      - name: auth-v1-open-callback
        strip_path: true
        paths:
          - /auth/v1/callback
    plugins:
      - name: cors

  - name: auth-v1-open-authorize
    url: http://auth:9999/authorize
    routes:
      - name: auth-v1-open-authorize
        strip_path: true
        paths:
          - /auth/v1/authorize
    plugins:
      - name: cors

  - name: auth-v1-open-health
    url: http://auth:9999/health
    routes:
      - name: auth-v1-open-health
        strip_path: true
        paths:
          - /auth/v1/health
    plugins:
      - name: cors

  ## Secure Auth routes
  - name: auth-v1
    url: http://auth:9999/
    routes:
      - name: auth-v1-all
        strip_path: true
        paths:
          - /auth/v1/
    plugins:
      - name: cors
      - name: key-auth
        config:
          hide_credentials: false
      - name: request-transformer
        config:
          add:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
          replace:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
            - anon

  ## Secure PostgREST routes
  - name: rest-v1
    url: http://rest:3000/
    routes:
      - name: rest-v1-all
        strip_path: true
        paths:
          - /rest/v1/
    plugins:
      - name: cors
      - name: key-auth
        config:
          hide_credentials: false
      - name: request-transformer
        config:
          add:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
          replace:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
            - anon

  ## Secure GraphQL routes
  - name: graphql-v1
    url: http://rest:3000/rpc/graphql
    routes:
      - name: graphql-v1-all
        strip_path: true
        paths:
          - /graphql/v1
    plugins:
      - name: cors
      - name: key-auth
        config:
          hide_credentials: false
      - name: request-transformer
        config:
          add:
            headers:
              - "Content-Profile: graphql_public"
              - "Authorization: $LUA_AUTH_EXPR"
          replace:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
            - anon

  ## Secure Realtime routes
  - name: realtime-v1-ws
    url: http://%[1]s:4000/socket
    protocol: ws
    routes:
      - name: realtime-v1-ws
        strip_path: true
        paths:
          - /realtime/v1/
    plugins:
      - name: cors
      - name: key-auth
        config:
          hide_credentials: false
      - name: request-transformer
        config:
          add:
            headers:
              - "x-api-key:$LUA_RT_WS_EXPR"
          replace:
            querystring:
              - "apikey:$LUA_RT_WS_EXPR"
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
            - anon

  - name: realtime-v1-rest
    url: http://%[1]s:4000/api
    routes:
      - name: realtime-v1-rest
        strip_path: true
        paths:
          - /realtime/v1/api/
    plugins:
      - name: cors
      - name: key-auth
        config:
          hide_credentials: false
      - name: request-transformer
        config:
          add:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
          replace:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin
            - anon

  ## Edge Functions routes
  - name: functions-v1
    url: http://functions:9000/
    routes:
      - name: functions-v1-all
        strip_path: true
        paths:
          - /functions/v1/
    plugins:
      - name: cors

  ## Storage routes (no key-auth — S3 presigned URLs don't carry apikey header)
  - name: storage-v1
    url: http://storage:5000/
    routes:
      - name: storage-v1-all
        strip_path: true
        paths:
          - /storage/v1/
    plugins:
      - name: cors
      - name: request-transformer
        config:
          add:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
          replace:
            headers:
              - "Authorization: $LUA_AUTH_EXPR"
      - name: post-function
        config:
          access:
            - |
              local auth = kong.request.get_header("authorization")
              if auth == nil or auth == "" or auth:find("^%%%%s*$") then
                kong.service.request.clear_header("authorization")
              end

  ## Secure Database routes
  - name: meta
    url: http://meta:8080/
    routes:
      - name: meta-all
        strip_path: true
        paths:
          - /pg/
    plugins:
      - name: key-auth
        config:
          hide_credentials: false
      - name: acl
        config:
          hide_groups_header: true
          allow:
            - admin

  ## Protected Dashboard — catch all remaining routes
  - name: dashboard
    url: http://studio:3000/
    routes:
      - name: dashboard-all
        strip_path: true
        paths:
          - /
    plugins:
      - name: cors
      - name: basic-auth
        config:
          hide_credentials: true
`, realtimeHost)
}

// generateKongEntrypoint generates the kong-entrypoint.sh script.
// This script builds Lua expressions for request-transformer and performs
// environment variable substitution in the declarative config.
func generateKongEntrypoint() string {
	return `#!/bin/bash
# Custom entrypoint for Kong that builds Lua expressions for request-transformer
# and performs environment variable substitution in the declarative config.

# Legacy API keys -> pass apikey through unchanged
export LUA_AUTH_EXPR="\$((headers.authorization ~= nil and headers.authorization:sub(1, 10) ~= 'Bearer sb_' and headers.authorization) or headers.apikey)"
export LUA_RT_WS_EXPR="\$(query_params.apikey)"

# Substitute environment variables in the Kong declarative config.
awk '{
  result = ""
  rest = $0
  while (match(rest, /\$[A-Za-z_][A-Za-z_0-9]*/)) {
    varname = substr(rest, RSTART + 1, RLENGTH - 1)
    if (varname in ENVIRON) {
      result = result substr(rest, 1, RSTART - 1) ENVIRON[varname]
    } else {
      result = result substr(rest, 1, RSTART + RLENGTH - 1)
    }
    rest = substr(rest, RSTART + RLENGTH)
  }
  print result rest
}' /home/kong/temp.yml > "$KONG_DECLARATIVE_CONFIG"

# Remove empty key-auth credentials (unconfigured opaque keys)
sed -i '/^[[:space:]]*- key:[[:space:]]*$/d' "$KONG_DECLARATIVE_CONFIG"

exec /entrypoint.sh kong docker-start
`
}

// generatePgHbaConfig generates the pg_hba.conf configuration
func generatePgHbaConfig() string {
	return `# PostgreSQL Client Authentication Configuration File
# TYPE  DATABASE        USER            ADDRESS                 METHOD

# Allow local connections
local   all             all                                     trust

# IPv4 local connections
host    all             all             127.0.0.1/32            trust

# IPv4 connections from Docker network
host    all             all             172.16.0.0/12           md5

# IPv6 local connections
host    all             all             ::1/128                 trust

# External connections via SSL (direct Postgres access)
hostssl all             all             0.0.0.0/0               md5
hostssl all             all             ::/0                    md5

# Allow replication connections
local   replication     all                                     trust
host    replication     all             127.0.0.1/32            trust
host    replication     all             ::1/128                 trust
`
}

// generateInitSQL generates a shell script to set service role passwords on first boot.
// The Supabase postgres image handles role creation, schemas, and extensions.
// This script only sets passwords so services can authenticate.
func generateInitSQL() string {
	return `#!/bin/bash
set -e

psql -v ON_ERROR_STOP=1 --username "supabase_admin" --dbname "$POSTGRES_DB" <<-EOSQL
  ALTER USER authenticator WITH PASSWORD '$POSTGRES_PASSWORD';
  ALTER USER supabase_auth_admin WITH PASSWORD '$POSTGRES_PASSWORD';
  ALTER USER supabase_storage_admin WITH PASSWORD '$POSTGRES_PASSWORD';
  ALTER USER supabase_admin WITH PASSWORD '$POSTGRES_PASSWORD';
  GRANT anon TO authenticator;
  GRANT authenticated TO authenticator;
  GRANT service_role TO authenticator;
EOSQL
`
}

// Validate checks if all required configuration files exist
func (m *Manager) Validate() error {
	instanceDir := m.GetInstanceDir()

	requiredFiles := []string{
		filepath.Join(instanceDir, "docker-compose.yml"),
		filepath.Join(instanceDir, ".env"),
		filepath.Join(instanceDir, "volumes", "api", "kong.yml"),
	}

	for _, file := range requiredFiles {
		if !FileExists(file) {
			return fmt.Errorf("required file missing: %s", file)
		}
	}

	return nil
}

// Clean removes the instance directory and all contents
// This is a destructive operation - use with caution
func (m *Manager) Clean() error {
	instanceDir := m.GetInstanceDir()

	// Check if directory exists
	if !dirExists(instanceDir) {
		return nil // Already clean (idempotent)
	}

	// Remove directory recursively with sudo
	cmd := exec.Command("sudo", "rm", "-rf", instanceDir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove instance directory: %w", err)
	}

	return nil
}
