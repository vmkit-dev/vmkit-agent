package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		baseDir    string
		username   string
		wantDir    string
	}{
		{
			name:       "basic instance",
			instanceID: "test-instance",
			baseDir:    "/opt/supabase",
			username:   "supabyoi",
			wantDir:    "/opt/supabase/test-instance",
		},
		{
			name:       "instance with different base",
			instanceID: "my-project",
			baseDir:    "/var/supabase",
			username:   "ubuntu",
			wantDir:    "/var/supabase/my-project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(tt.instanceID, tt.baseDir, tt.username)

			if m.GetInstanceDir() != tt.wantDir {
				t.Errorf("GetInstanceDir() = %v, want %v", m.GetInstanceDir(), tt.wantDir)
			}

			if m.instanceID != tt.instanceID {
				t.Errorf("instanceID = %v, want %v", m.instanceID, tt.instanceID)
			}

			if m.baseDir != tt.baseDir {
				t.Errorf("baseDir = %v, want %v", m.baseDir, tt.baseDir)
			}

			if m.username != tt.username {
				t.Errorf("username = %v, want %v", m.username, tt.username)
			}
		})
	}
}

func TestFileExists(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test-file-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "existing file",
			path: tmpFile.Name(),
			want: true,
		},
		{
			name: "nonexistent file",
			path: "/nonexistent/path/to/file.txt",
			want: false,
		},
		{
			name: "directory not a file",
			path: os.TempDir(),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FileExists(tt.path)
			if got != tt.want {
				t.Errorf("FileExists(%s) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestDirExists(t *testing.T) {
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "test-dir-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test-file-*")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "existing directory",
			path: tmpDir,
			want: true,
		},
		{
			name: "nonexistent directory",
			path: "/nonexistent/path/to/dir",
			want: false,
		},
		{
			name: "file not a directory",
			path: tmpFile.Name(),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dirExists(tt.path)
			if got != tt.want {
				t.Errorf("dirExists(%s) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestGeneratePgHbaConfigHasSSLRules(t *testing.T) {
	content := generatePgHbaConfig()

	sslStrings := []string{
		"hostssl all",
		"0.0.0.0/0",
		"::/0",
	}
	for _, expected := range sslStrings {
		if !strings.Contains(content, expected) {
			t.Errorf("generatePgHbaConfig() missing SSL rule: %s", expected)
		}
	}
}

func TestGenerateKongConfig(t *testing.T) {
	config := &types.DeployConfig{
		InstanceID:     "test-123",
		Subdomain:      "myproject",
		PublishableKey: "anon-key-123",
		SecretKey:      "service-role-key-456",
	}

	content := generateKongConfig(config)

	// Check that content includes expected elements
	expectedStrings := []string{
		"_format_version:",
		"_transform: true",
		"services:",
		"auth-v1-open",
		"auth-v1-open-callback",
		"auth-v1-open-authorize",
		"auth-v1-open-health",
		"/auth/v1/health",
		"auth-v1-all",
		"rest-v1-all",
		"storage-v1-all",
		"realtime-v1-ws",
		"graphql-v1-all",
		"meta-all",
		"dashboard-all",
		"/auth/v1/verify",
		"/auth/v1/callback",
		"/auth/v1/authorize",
		"/auth/v1/",
		"/rest/v1/",
		"/storage/v1/",
		"/realtime/v1/",
		"/graphql/v1",
		"/pg/",
		"strip_path: true",
		"key-auth",
		"hide_credentials: false",
		"consumers:",
		"username: anon",
		"username: service_role",
		"username: DASHBOARD",
		"$SUPABASE_ANON_KEY",
		"$SUPABASE_SERVICE_KEY",
		"$DASHBOARD_USERNAME",
		"$DASHBOARD_PASSWORD",
		"acls:",
		"group: anon",
		"group: admin",
		"basicauth_credentials:",
		"basic-auth",
		"request-transformer",
		"$LUA_AUTH_EXPR",
		"post-function",
		"cors",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(content, expected) {
			t.Errorf("generateKongConfig() missing expected string: %s", expected)
		}
	}

	// Verify no direct postgres route (security)
	if strings.Contains(content, "postgres:5432") {
		t.Error("generateKongConfig() should not expose direct postgres access")
	}

	// Verify no hardcoded API keys (should use $VARIABLE placeholders)
	if strings.Contains(content, "anon-key-123") {
		t.Error("generateKongConfig() should not contain hardcoded API keys")
	}
}

func TestGenerateKongEntrypoint(t *testing.T) {
	content := generateKongEntrypoint()

	expectedStrings := []string{
		"#!/bin/bash",
		"LUA_AUTH_EXPR",
		"LUA_RT_WS_EXPR",
		"KONG_DECLARATIVE_CONFIG",
		"/home/kong/temp.yml",
		"exec /entrypoint.sh kong docker-start",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(content, expected) {
			t.Errorf("generateKongEntrypoint() missing expected string: %s", expected)
		}
	}
}

func TestGeneratePgHbaConfig(t *testing.T) {
	content := generatePgHbaConfig()

	// Check that content includes expected elements
	expectedStrings := []string{
		"PostgreSQL Client Authentication",
		"local   all             all",
		"host    all             all             127.0.0.1/32",
		"172.16.0.0/12",
		"replication",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(content, expected) {
			t.Errorf("generatePgHbaConfig() missing expected string: %s", expected)
		}
	}
}

func TestWriteFile(t *testing.T) {
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "files-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m := New("test-instance", tmpDir, "testuser")

	tests := []struct {
		name    string
		path    string
		content string
		mode    os.FileMode
		wantErr bool
	}{
		{
			name:    "write simple file",
			path:    filepath.Join(tmpDir, "test.txt"),
			content: "test content",
			mode:    0644,
			wantErr: false,
		},
		{
			name:    "write file in nested directory",
			path:    filepath.Join(tmpDir, "nested", "dir", "file.txt"),
			content: "nested content",
			mode:    0644,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := m.writeFile(tt.path, tt.content, tt.mode)

			if (err != nil) != tt.wantErr {
				t.Errorf("writeFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil {
				// Verify file was written
				if !FileExists(tt.path) {
					t.Error("writeFile() file not created")
					return
				}

				// Verify content
				readContent, err := os.ReadFile(tt.path)
				if err != nil {
					t.Errorf("Failed to read written file: %v", err)
					return
				}

				if string(readContent) != tt.content {
					t.Errorf("File content = %v, want %v", string(readContent), tt.content)
				}
			}
		})
	}
}

func TestWriteDockerCompose(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "files-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m := New("test-instance", tmpDir, "testuser")

	// Create instance directory first
	if err := os.MkdirAll(m.GetInstanceDir(), 0755); err != nil {
		t.Fatalf("Failed to create instance dir: %v", err)
	}

	config := &types.DeployConfig{
		InstanceID:       "test-123",
		Subdomain:        "myproject",
		Domain:           "example.com",
		PostgresPassword: "secret",
		JWTSecret:        "jwt-secret",
		ComposeContent:   "services:\n  postgres:\n    container_name: myproject-postgres\n",
	}

	err = m.WriteDockerCompose(config)
	if err != nil {
		t.Errorf("WriteDockerCompose() error = %v", err)
		return
	}

	// Verify file exists
	path := filepath.Join(m.GetInstanceDir(), "docker-compose.yml")
	if !FileExists(path) {
		t.Error("WriteDockerCompose() file not created")
	}

	// Verify content
	content, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("Failed to read docker-compose.yml: %v", err)
		return
	}

	if !strings.Contains(string(content), "myproject-postgres") {
		t.Error("docker-compose.yml missing expected content")
	}
}

func TestWriteDockerComposeRequiresContent(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "files-test-*")
	defer os.RemoveAll(tmpDir)
	m := New("test-instance", tmpDir, "testuser")
	_ = os.MkdirAll(m.GetInstanceDir(), 0755)

	err := m.WriteDockerCompose(&types.DeployConfig{Subdomain: "x"})
	if err == nil {
		t.Error("expected error when ComposeContent is empty")
	}
}

func TestWriteEnvFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "files-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m := New("test-instance", tmpDir, "testuser")

	// Create instance directory first
	if err := os.MkdirAll(m.GetInstanceDir(), 0755); err != nil {
		t.Fatalf("Failed to create instance dir: %v", err)
	}

	config := &types.DeployConfig{
		InstanceID:       "test-123",
		Subdomain:        "myproject",
		Domain:           "example.com",
		PostgresPassword: "secret",
		JWTSecret:        "jwt-secret",
		EnvContent:       "INSTANCE_ID=test-123\nSUBDOMAIN=myproject\n",
	}

	err = m.WriteEnvFile(config)
	if err != nil {
		t.Errorf("WriteEnvFile() error = %v", err)
		return
	}

	// Verify file exists
	path := filepath.Join(m.GetInstanceDir(), ".env")
	if !FileExists(path) {
		t.Error("WriteEnvFile() file not created")
	}

	// Verify content
	content, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("Failed to read .env: %v", err)
		return
	}

	if !strings.Contains(string(content), "INSTANCE_ID=test-123") {
		t.Error(".env missing expected content")
	}
}

func TestWriteEnvFileRequiresContent(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "files-test-*")
	defer os.RemoveAll(tmpDir)
	m := New("test-instance", tmpDir, "testuser")
	_ = os.MkdirAll(m.GetInstanceDir(), 0755)

	err := m.WriteEnvFile(&types.DeployConfig{Subdomain: "x"})
	if err == nil {
		t.Error("expected error when EnvContent is empty")
	}
}

func TestWriteKongConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "files-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m := New("test-instance", tmpDir, "testuser")

	// Create directory structure
	apiDir := filepath.Join(m.GetInstanceDir(), "volumes", "api")
	if err := os.MkdirAll(apiDir, 0755); err != nil {
		t.Fatalf("Failed to create api dir: %v", err)
	}

	config := &types.DeployConfig{
		InstanceID: "test-123",
		Subdomain:  "myproject",
	}

	err = m.WriteKongConfig(config)
	if err != nil {
		t.Errorf("WriteKongConfig() error = %v", err)
		return
	}

	// Verify file exists
	path := filepath.Join(apiDir, "kong.yml")
	if !FileExists(path) {
		t.Error("WriteKongConfig() file not created")
	}
}

func TestWritePgHbaConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "files-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m := New("test-instance", tmpDir, "testuser")

	// Create directory structure
	dbDir := filepath.Join(m.GetInstanceDir(), "volumes", "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		t.Fatalf("Failed to create db dir: %v", err)
	}

	err = m.WritePgHbaConfig()
	if err != nil {
		t.Errorf("WritePgHbaConfig() error = %v", err)
		return
	}

	// Verify file exists
	path := filepath.Join(dbDir, "pg_hba.conf")
	if !FileExists(path) {
		t.Error("WritePgHbaConfig() file not created")
	}
}

func TestWriteAllConfigFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "files-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m := New("test-instance", tmpDir, "testuser")

	// Create directory structure
	instanceDir := m.GetInstanceDir()
	if err := os.MkdirAll(filepath.Join(instanceDir, "volumes", "api"), 0755); err != nil {
		t.Fatalf("Failed to create api dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(instanceDir, "volumes", "db"), 0755); err != nil {
		t.Fatalf("Failed to create db dir: %v", err)
	}

	config := &types.DeployConfig{
		InstanceID:       "test-123",
		Subdomain:        "myproject",
		Domain:           "example.com",
		PostgresPassword: "secret",
		JWTSecret:        "jwt-secret",
		ComposeContent:   "services:\n  postgres: {}\n",
		EnvContent:       "INSTANCE_ID=test-123\n",
	}

	err = m.WriteAllConfigFiles(config)
	if err != nil {
		t.Errorf("WriteAllConfigFiles() error = %v", err)
		return
	}

	// Verify all files were created
	expectedFiles := []string{
		filepath.Join(instanceDir, "docker-compose.yml"),
		filepath.Join(instanceDir, ".env"),
		filepath.Join(instanceDir, "volumes", "api", "kong.yml"),
		filepath.Join(instanceDir, "volumes", "db", "pg_hba.conf"),
	}

	for _, file := range expectedFiles {
		if !FileExists(file) {
			t.Errorf("WriteAllConfigFiles() missing file: %s", file)
		}
	}
}

func TestWriteInitSQLFilesWritesAllUpstreamFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "files-init-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m := New("test-instance", tmpDir, "testuser")
	initDir := filepath.Join(m.GetInstanceDir(), "volumes", "db")
	if err := os.MkdirAll(initDir, 0755); err != nil {
		t.Fatalf("mkdir init dir: %v", err)
	}

	files := map[string]string{
		"_supabase.sql": "-- supabase\n",
		"jwt.sql":       "-- jwt\n",
		"logs.sql":      "-- logs\n",
		"pooler.sql":    "-- pooler\n",
		"realtime.sql":  "-- realtime\n",
		"roles.sql":     "-- roles\n",
		"webhooks.sql":  "-- webhooks\n",
	}

	if err := m.WriteInitSQLFiles(files); err != nil {
		t.Fatalf("WriteInitSQLFiles: %v", err)
	}

	for name, want := range files {
		p := filepath.Join(initDir, name)
		got, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("missing init file %s: %v", name, err)
			continue
		}
		if string(got) != want {
			t.Errorf("init file %s content mismatch: got %q want %q", name, got, want)
		}
	}
}

func TestWriteInitSQLFilesEmptyIsNoop(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "files-init-empty-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m := New("test-instance", tmpDir, "testuser")
	if err := m.WriteInitSQLFiles(nil); err != nil {
		t.Errorf("WriteInitSQLFiles(nil) should be no-op, got: %v", err)
	}
	if err := m.WriteInitSQLFiles(map[string]string{}); err != nil {
		t.Errorf("WriteInitSQLFiles(empty) should be no-op, got: %v", err)
	}
}

func TestWriteInitSQLFilesRejectsPathSeparators(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "files-init-bad-*")
	defer os.RemoveAll(tmpDir)
	m := New("test-instance", tmpDir, "testuser")
	cases := []string{"sub/roles.sql", "../etc/passwd", "..roles.sql"}
	for _, bad := range cases {
		err := m.WriteInitSQLFiles(map[string]string{bad: "x"})
		if err == nil {
			t.Errorf("WriteInitSQLFiles(%q) should reject path traversal", bad)
		}
	}
}

func TestWriteVolumeConfigFilesWritesUpstreamFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "files-vol-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m := New("test-instance", tmpDir, "testuser")
	cfg := map[string]string{
		"volumes/logs/vector.yml":   "api:\n  enabled: true\n",
		"volumes/pooler/pooler.exs": "{:ok, _} = Application.ensure_all_started(:supavisor)\n",
	}

	if err := m.WriteVolumeConfigFiles(cfg); err != nil {
		t.Fatalf("WriteVolumeConfigFiles: %v", err)
	}

	for rel, want := range cfg {
		p := filepath.Join(m.GetInstanceDir(), rel)
		got, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("missing volume config %s: %v", rel, err)
			continue
		}
		if string(got) != want {
			t.Errorf("volume config %s mismatch", rel)
		}
	}
}

func TestWriteVolumeConfigFilesRejectsInvalidPaths(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "files-vol-bad-*")
	defer os.RemoveAll(tmpDir)
	m := New("test-instance", tmpDir, "testuser")
	cases := []string{"/etc/passwd", "config/foo.yml", "volumes/../etc/passwd"}
	for _, bad := range cases {
		err := m.WriteVolumeConfigFiles(map[string]string{bad: "x"})
		if err == nil {
			t.Errorf("WriteVolumeConfigFiles(%q) should reject invalid path", bad)
		}
	}
}

func TestWriteAllConfigFilesPrefersUpstreamInitFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "files-all-upstream-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m := New("test-instance", tmpDir, "testuser")
	instanceDir := m.GetInstanceDir()
	for _, d := range []string{
		filepath.Join(instanceDir, "volumes", "api"),
		filepath.Join(instanceDir, "volumes", "db", "init"),
		filepath.Join(instanceDir, "volumes", "logs"),
		filepath.Join(instanceDir, "volumes", "pooler"),
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	config := &types.DeployConfig{
		InstanceID:       "test-123",
		Subdomain:        "acme",
		PostgresPassword: "secret",
		ComposeContent:   "services: {}\n",
		EnvContent:       "X=1\n",
		InitSQLFiles: map[string]string{
			"roles.sql":     "-- upstream roles\n",
			"_supabase.sql": "-- supabase\n",
		},
		VolumeConfigFiles: map[string]string{
			"volumes/logs/vector.yml": "vector: true\n",
		},
	}
	if err := m.WriteAllConfigFiles(config); err != nil {
		t.Fatalf("WriteAllConfigFiles: %v", err)
	}

	// Legacy roles.sh must NOT be written when upstream init files are supplied.
	legacy := filepath.Join(instanceDir, "volumes", "db", "init", "roles.sh")
	if FileExists(legacy) {
		t.Errorf("legacy roles.sh should not be written when InitSQLFiles is populated")
	}
	// Upstream files must exist directly under volumes/db/ (see WriteInitSQLFiles).
	for name := range config.InitSQLFiles {
		if !FileExists(filepath.Join(instanceDir, "volumes", "db", name)) {
			t.Errorf("missing upstream init file %s", name)
		}
	}
	// Volume config file must exist.
	if !FileExists(filepath.Join(instanceDir, "volumes", "logs", "vector.yml")) {
		t.Error("missing volumes/logs/vector.yml")
	}
}

func TestGetPostgresUIDGIDRejectsEmptyImage(t *testing.T) {
	if _, err := GetPostgresUIDGID(""); err == nil {
		t.Error("GetPostgresUIDGID(\"\") should return an error")
	}
	if _, err := GetPostgresUIDGID("   "); err == nil {
		t.Error("GetPostgresUIDGID(whitespace) should return an error")
	}
}

func TestPostgresUIDRegexParsesKnownImageSeries(t *testing.T) {
	// Representative `id` outputs for the two supabase/postgres image
	// series that have been seen in the wild. This regression guards
	// the parser against future `id` format drift without requiring a
	// live docker daemon.
	cases := []struct {
		name       string
		idOutput   string
		wantUIDGID string
	}{
		{
			name:       "postgres 15.1.x (legacy)",
			idOutput:   "uid=101(postgres) gid=102(postgres) groups=102(postgres),101(ssl-cert)",
			wantUIDGID: "101:102",
		},
		{
			name:       "postgres 15.8.x",
			idOutput:   "uid=105(postgres) gid=106(postgres) groups=106(postgres)",
			wantUIDGID: "105:106",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matches := postgresUIDRegex.FindStringSubmatch(tc.idOutput)
			if len(matches) != 3 {
				t.Fatalf("regex failed to match %q", tc.idOutput)
			}
			got := matches[1] + ":" + matches[2]
			if got != tc.wantUIDGID {
				t.Errorf("got %s, want %s", got, tc.wantUIDGID)
			}
		})
	}
}

func TestPostgresUIDRegexRejectsGarbage(t *testing.T) {
	garbage := []string{
		"",
		"postgres",
		"uid=postgres gid=postgres",
		"error: no such user",
	}
	for _, g := range garbage {
		if postgresUIDRegex.MatchString(g) {
			t.Errorf("regex should not match %q", g)
		}
	}
}

func TestValidate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "files-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m := New("test-instance", tmpDir, "testuser")

	// Test validation fails when files don't exist
	err = m.Validate()
	if err == nil {
		t.Error("Validate() should fail when files don't exist")
	}

	// Create directory structure and files
	instanceDir := m.GetInstanceDir()
	os.MkdirAll(filepath.Join(instanceDir, "volumes", "api"), 0755)

	config := &types.DeployConfig{
		InstanceID:     "test-123",
		Subdomain:      "myproject",
		Domain:         "example.com",
		ComposeContent: "services: {}\n",
		EnvContent:     "INSTANCE_ID=test-123\n",
	}

	m.WriteDockerCompose(config)
	m.WriteEnvFile(config)
	m.WriteKongConfig(config)

	// Test validation succeeds when all required files exist
	err = m.Validate()
	if err != nil {
		t.Errorf("Validate() error = %v, expected nil after writing files", err)
	}
}

func TestCreateDirectoryStructure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "files-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m := New("test-instance", tmpDir, "")

	// CreateDirectoryStructure uses sudo, which requires root privileges
	// In a test environment, we just verify it doesn't panic
	err = m.CreateDirectoryStructure()
	if err != nil {
		// Expected to fail in test environment without sudo
		t.Logf("CreateDirectoryStructure() error = %v (expected without sudo)", err)
	}

	// If we somehow have sudo access, verify directories
	if err == nil {
		expectedDirs := []string{
			m.GetInstanceDir(),
			filepath.Join(m.GetInstanceDir(), "volumes"),
			filepath.Join(m.GetInstanceDir(), "volumes", "db"),
			filepath.Join(m.GetInstanceDir(), "volumes", "db", "data"),
			filepath.Join(m.GetInstanceDir(), "volumes", "storage"),
			filepath.Join(m.GetInstanceDir(), "volumes", "api"),
			filepath.Join(m.GetInstanceDir(), "volumes", "logs"),
		}

		for _, dir := range expectedDirs {
			if !dirExists(dir) {
				t.Errorf("CreateDirectoryStructure() missing directory: %s", dir)
			}
		}
	}
}

func TestClean(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "files-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	m := New("test-instance", tmpDir, "")

	// Create instance directory manually (without sudo)
	instanceDir := m.GetInstanceDir()
	if err := os.MkdirAll(instanceDir, 0755); err != nil {
		t.Fatalf("Failed to create instance dir: %v", err)
	}

	// Verify directory exists
	if !dirExists(instanceDir) {
		t.Fatal("Instance directory should exist before Clean()")
	}

	// Clean directory (uses sudo, may fail in test environment)
	err = m.Clean()
	if err != nil {
		t.Logf("Clean() error = %v (expected without sudo)", err)
		// Manually clean up for test
		os.RemoveAll(instanceDir)
		return
	}

	// If Clean succeeded, verify directory was removed
	if dirExists(instanceDir) {
		t.Error("Clean() should remove instance directory")
	}

	// Test idempotency - should not fail when called again
	err = m.Clean()
	if err != nil {
		t.Logf("Clean() idempotency error = %v (expected without sudo)", err)
	}
}

// Benchmark tests
