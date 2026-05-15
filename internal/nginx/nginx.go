package nginx

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"
)

const (
	// Directory paths
	ConfigDir = "/etc/nginx"
	// Rate limiting zones
	AuthRateLimit    = "5r/s"  // Auth endpoints: 5 requests/second
	APIRateLimit     = "30r/s" // API endpoints: 30 requests/second
	StorageRateLimit = "10r/s" // Storage endpoints: 10 requests/second
)

// SupabaseSitesDir is where per-instance site configs (and their htpasswd
// files) live. It's a var, not a const, so tests can point it at a temp
// directory — production code never writes to it.
var SupabaseSitesDir = "/etc/nginx/supabase-sites"

// SiteConfig represents the configuration for a Supabase instance site
type SiteConfig struct {
	Subdomain      string
	Domain         string
	StudioDomain   string
	KongPort       int
	StudioPort     int
	TLSEnabled     bool
	CertPath       string
	KeyPath        string
	StudioCertPath string
	StudioKeyPath  string
	// StudioHtpasswdPath, if non-empty, makes the studio server block emit
	// `auth_basic` directives reading from this file. Set this to the path
	// returned by WriteHtpasswd. Leaving it empty keeps studio public, which
	// must NEVER happen in production — see bead supabyoi-f8w0.
	StudioHtpasswdPath string
}

// IsInstalled checks if Nginx is installed
func IsInstalled() bool {
	_, err := exec.LookPath("nginx")
	return err == nil
}

// Install installs and configures Nginx on the system
// This function is idempotent - safe to call multiple times
func Install(username string) error {
	// Check if already installed (idempotent)
	if IsInstalled() {
		// Already installed, just ensure configuration is correct
		if err := ensureConfiguration(username); err != nil {
			return fmt.Errorf("failed to ensure Nginx configuration: %w", err)
		}
		return nil
	}

	// Install Nginx via apt (Ubuntu/Debian)
	if err := installNginxPackage(); err != nil {
		return fmt.Errorf("failed to install Nginx package: %w", err)
	}

	// Enable and start Nginx service
	if err := enableNginxService(); err != nil {
		return fmt.Errorf("failed to enable Nginx service: %w", err)
	}

	// Setup configuration
	if err := ensureConfiguration(username); err != nil {
		return fmt.Errorf("failed to setup Nginx configuration: %w", err)
	}

	// Verify installation
	if !IsInstalled() {
		return fmt.Errorf("Nginx installation verification failed")
	}

	return nil
}

// installNginxPackage installs Nginx using apt
func installNginxPackage() error {
	// Update package lists
	updateCmd := exec.Command("apt-get", "update")
	if err := updateCmd.Run(); err != nil {
		return fmt.Errorf("apt-get update failed: %w", err)
	}

	// Install nginx
	installCmd := exec.Command("apt-get", "install", "-y", "nginx")
	var stdout, stderr bytes.Buffer
	installCmd.Stdout = &stdout
	installCmd.Stderr = &stderr

	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("apt-get install nginx failed: %w\nStdout: %s\nStderr: %s",
			err, stdout.String(), stderr.String())
	}

	return nil
}

// enableNginxService enables and starts the Nginx systemd service
func enableNginxService() error {
	// Enable Nginx service to start on boot
	enableCmd := exec.Command("systemctl", "enable", "nginx")
	if err := enableCmd.Run(); err != nil {
		return fmt.Errorf("failed to enable Nginx service: %w", err)
	}

	// Start Nginx service
	startCmd := exec.Command("systemctl", "start", "nginx")
	if err := startCmd.Run(); err != nil {
		return fmt.Errorf("failed to start Nginx service: %w", err)
	}

	return nil
}

// ensureConfiguration ensures Nginx is configured correctly for Supabase
func ensureConfiguration(username string) error {
	// Create supabase-sites directory
	if err := SetupSupabaseSitesDir(username); err != nil {
		return fmt.Errorf("failed to setup supabase-sites directory: %w", err)
	}

	// Ensure include directive in nginx.conf
	if err := EnsureIncludeDirective(); err != nil {
		return fmt.Errorf("failed to add include directive: %w", err)
	}

	// Configure rate limiting zones
	if err := ConfigureRateLimiting(); err != nil {
		return fmt.Errorf("failed to configure rate limiting: %w", err)
	}

	// Increase server_names_hash_bucket_size for long subdomain names
	if err := ConfigureServerNamesHashBucketSize(); err != nil {
		return fmt.Errorf("failed to configure server names hash bucket size: %w", err)
	}

	// Test configuration
	if err := TestConfig(); err != nil {
		return fmt.Errorf("Nginx configuration test failed: %w", err)
	}

	// Reload to apply changes
	if err := Reload(); err != nil {
		return fmt.Errorf("failed to reload Nginx: %w", err)
	}

	return nil
}

// SetupSupabaseSitesDir creates the supabase-sites directory with proper permissions
func SetupSupabaseSitesDir(username string) error {
	// Create directory
	if err := os.MkdirAll(SupabaseSitesDir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", SupabaseSitesDir, err)
	}

	// Set ownership to username so SFTP can write config files
	if username != "" && username != "root" {
		chownCmd := exec.Command("chown", fmt.Sprintf("%s:%s", username, username), SupabaseSitesDir)
		if err := chownCmd.Run(); err != nil {
			return fmt.Errorf("failed to set ownership on %s: %w", SupabaseSitesDir, err)
		}
	}

	return nil
}

// EnsureIncludeDirective adds the include directive to nginx.conf if not present
func EnsureIncludeDirective() error {
	nginxConf := ConfigDir + "/nginx.conf"

	// Check if include directive already exists
	grepCmd := exec.Command("grep", "-q", SupabaseSitesDir, nginxConf)
	if grepCmd.Run() == nil {
		// Already present
		return nil
	}

	// Add include directive after "http {" line
	includeLine := fmt.Sprintf("    include %s/*.conf;", SupabaseSitesDir)
	// Note: sed -i requires sudo to modify /etc/nginx/nginx.conf
	sedCmd := exec.Command("sudo", "sed", "-i", fmt.Sprintf("/http {/a\\    %s", includeLine), nginxConf)

	if err := sedCmd.Run(); err != nil {
		return fmt.Errorf("failed to add include directive: %w", err)
	}

	return nil
}

// ConfigureRateLimiting adds rate limiting zones to nginx.conf if not present
func ConfigureRateLimiting() error {
	nginxConf := ConfigDir + "/nginx.conf"

	// Check if rate limiting already configured
	grepCmd := exec.Command("grep", "-q", "limit_req_zone.*zone=supabyoi_auth", nginxConf)
	if grepCmd.Run() == nil {
		// Already configured
		return nil
	}

	// Add rate limiting zones after "http {" line
	rateLimitZones := fmt.Sprintf(`    # Supabyoi rate limiting zones
    limit_req_zone $binary_remote_addr zone=supabyoi_auth:10m rate=%s;
    limit_req_zone $binary_remote_addr zone=supabyoi_api:10m rate=%s;
    limit_req_zone $binary_remote_addr zone=supabyoi_storage:10m rate=%s;
`,
		AuthRateLimit, APIRateLimit, StorageRateLimit)

	// Read current nginx.conf
	content, err := os.ReadFile(nginxConf)
	if err != nil {
		return fmt.Errorf("failed to read nginx.conf: %w", err)
	}

	// Find "http {" and insert rate limiting zones after it
	httpBlock := "http {"
	newContent := strings.Replace(string(content), httpBlock, httpBlock+"\n"+rateLimitZones, 1)

	// Write back to nginx.conf (requires the agent to run with sudo)
	if err := os.WriteFile(nginxConf, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write nginx.conf: %w", err)
	}

	return nil
}

// ConfigureServerNamesHashBucketSize increases the hash bucket size to 128
// to accommodate longer subdomain names (e.g., tigress-d59146.supabyoi.com)
func ConfigureServerNamesHashBucketSize() error {
	nginxConf := ConfigDir + "/nginx.conf"

	// Check if already configured with our value (128)
	grepCmd := exec.Command("grep", "-q", "server_names_hash_bucket_size 128", nginxConf)
	if grepCmd.Run() == nil {
		return nil
	}

	// Read current nginx.conf
	content, err := os.ReadFile(nginxConf)
	if err != nil {
		return fmt.Errorf("failed to read nginx.conf: %w", err)
	}

	contentStr := string(content)

	// Remove any existing server_names_hash_bucket_size lines (commented or not)
	lines := strings.Split(contentStr, "\n")
	var filtered []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "server_names_hash_bucket_size") {
			continue
		}
		filtered = append(filtered, line)
	}
	contentStr = strings.Join(filtered, "\n")

	// Add after "http {" line
	httpBlock := "http {"
	newContent := strings.Replace(contentStr, httpBlock, httpBlock+"\n    server_names_hash_bucket_size 128;", 1)

	if err := os.WriteFile(nginxConf, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write nginx.conf: %w", err)
	}

	return nil
}

// CreateSiteConfig creates an Nginx site configuration for a Supabase instance
func CreateSiteConfig(config *SiteConfig) error {
	// Ensure rate limiting zones are configured in nginx.conf
	// These zones are required by the site config templates
	if err := ConfigureRateLimiting(); err != nil {
		return fmt.Errorf("failed to configure rate limiting: %w", err)
	}

	// Generate configuration content
	content, err := generateSiteConfigContent(config)
	if err != nil {
		return fmt.Errorf("failed to generate config content: %w", err)
	}

	// Write configuration file
	configPath := fmt.Sprintf("%s/%s.conf", SupabaseSitesDir, config.Subdomain)
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	// Test configuration
	if err := TestConfig(); err != nil {
		// Rollback on error
		os.Remove(configPath)
		return fmt.Errorf("configuration test failed, rolled back: %w", err)
	}

	// Reload Nginx to apply changes
	if err := Reload(); err != nil {
		return fmt.Errorf("failed to reload Nginx: %w", err)
	}

	return nil
}

// generateSiteConfigContent generates the Nginx configuration content
func generateSiteConfigContent(config *SiteConfig) (string, error) {
	tmpl := `# Supabase instance: {{.Subdomain}}
# Generated by vmkit-agent

# Rate-limited upstream for API (Kong)
upstream {{.Subdomain}}_api {
    server 127.0.0.1:{{.KongPort}} max_fails=3 fail_timeout=30s;
    keepalive 32;
}

# Upstream for Studio
upstream {{.Subdomain}}_studio {
    server 127.0.0.1:{{.StudioPort}} max_fails=3 fail_timeout=30s;
    keepalive 16;
}

# API server block
server {
{{if .TLSEnabled}}    listen 443 ssl http2;
    listen [::]:443 ssl http2;

    ssl_certificate {{.CertPath}};
    ssl_certificate_key {{.KeyPath}};

    # SSL configuration
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384;
    ssl_prefer_server_ciphers off;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 10m;
{{else}}    listen 80;
    listen [::]:80;
{{end}}

    server_name {{.Domain}};

    # Security headers
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;
{{if .TLSEnabled}}    add_header Strict-Transport-Security "max-age=63072000; includeSubDomains; preload" always;
{{end}}

    # Tighten default request-body read timeout (nginx default is 60s).
    # Caps exposure to slow-upload DoS while still generous for legit clients.
    client_body_timeout 30s;

    # Rate limiting - Auth endpoints (strict: 5r/s, burst 5)
    location /auth/v1/token {
        limit_req zone=supabyoi_auth burst=5 nodelay;
        limit_req_status 429;
        proxy_pass http://{{.Subdomain}}_api;
        include /etc/nginx/proxy_params;
    }

    location ~ ^/auth/v1/(signup|recover|otp) {
        limit_req zone=supabyoi_auth burst=3 nodelay;
        limit_req_status 429;
        proxy_pass http://{{.Subdomain}}_api;
        include /etc/nginx/proxy_params;
    }

    # Rate limiting - REST API (moderate: 30r/s, burst 50)
    location /rest/v1/ {
        # 25m is generous for text/jsonb upserts without enabling obvious abuse.
        # Storage uploads go through /storage/v1/ which allows 100m.
        client_max_body_size 25m;
        limit_req zone=supabyoi_api burst=50;
        limit_req_status 429;
        proxy_pass http://{{.Subdomain}}_api;
        include /etc/nginx/proxy_params;
    }

    # Rate limiting - Storage (moderate: 10r/s, burst 20)
    location /storage/v1/ {
        client_max_body_size 100m;
        limit_req zone=supabyoi_storage burst=20;
        limit_req_status 429;
        proxy_pass http://{{.Subdomain}}_api;
        include /etc/nginx/proxy_params;
    }

    # Other API endpoints (no rate limiting)
    location / {
        proxy_pass http://{{.Subdomain}}_api;
        include /etc/nginx/proxy_params;

        # WebSocket support for Realtime
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }

    # Custom 429 error response
    error_page 429 = @rate_limited;
    location @rate_limited {
        default_type application/json;
        return 429 '{"error": "too_many_requests", "message": "Rate limit exceeded. Please try again later."}';
    }
}

# Studio server block
server {
{{if .TLSEnabled}}    listen 443 ssl http2;
    listen [::]:443 ssl http2;

    ssl_certificate {{.StudioCertPath}};
    ssl_certificate_key {{.StudioKeyPath}};

    # SSL configuration
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384;
    ssl_prefer_server_ciphers off;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 10m;
{{else}}    listen 80;
    listen [::]:80;
{{end}}

    server_name {{.StudioDomain}};

    # Security headers
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;
{{if .TLSEnabled}}    add_header Strict-Transport-Security "max-age=63072000; includeSubDomains; preload" always;
{{end}}
    location / {
{{if .StudioHtpasswdPath}}        auth_basic "Supabase Studio";
        auth_basic_user_file {{.StudioHtpasswdPath}};
{{end}}        proxy_pass http://{{.Subdomain}}_studio;
        include /etc/nginx/proxy_params;

        # WebSocket support
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
{{if .TLSEnabled}}
# HTTP to HTTPS redirect
server {
    listen 80;
    listen [::]:80;
    server_name {{.Domain}} {{.StudioDomain}};

    location / {
        return 301 https://$host$request_uri;
    }
}
{{end}}`

	t, err := template.New("nginx").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	// Fall back to main cert for studio if not explicitly set
	studioCertPath := config.StudioCertPath
	if studioCertPath == "" {
		studioCertPath = config.CertPath
	}
	studioKeyPath := config.StudioKeyPath
	if studioKeyPath == "" {
		studioKeyPath = config.KeyPath
	}

	data := struct {
		Subdomain          string
		Domain             string
		StudioDomain       string
		KongPort           int
		StudioPort         int
		TLSEnabled         bool
		CertPath           string
		KeyPath            string
		StudioCertPath     string
		StudioKeyPath      string
		StudioHtpasswdPath string
	}{
		Subdomain:          config.Subdomain,
		Domain:             config.Domain,
		StudioDomain:       config.StudioDomain,
		KongPort:           config.KongPort,
		StudioPort:         config.StudioPort,
		TLSEnabled:         config.TLSEnabled,
		CertPath:           config.CertPath,
		KeyPath:            config.KeyPath,
		StudioCertPath:     studioCertPath,
		StudioKeyPath:      studioKeyPath,
		StudioHtpasswdPath: config.StudioHtpasswdPath,
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// RemoveSiteConfig removes the site configuration for a subdomain
func RemoveSiteConfig(subdomain string) error {
	configPath := fmt.Sprintf("%s/%s.conf", SupabaseSitesDir, subdomain)

	// Remove config file
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove config file: %w", err)
	}

	// Test configuration
	if err := TestConfig(); err != nil {
		return fmt.Errorf("configuration test failed after removal: %w", err)
	}

	// Reload Nginx
	if err := Reload(); err != nil {
		return fmt.Errorf("failed to reload Nginx: %w", err)
	}

	return nil
}

// GetVersion returns the installed Nginx version
func GetVersion() (string, error) {
	out, err := exec.Command("nginx", "-v").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get Nginx version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// TestConfig tests the Nginx configuration for syntax errors
func TestConfig() error {
	cmd := exec.Command("nginx", "-t")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("nginx -t failed: %w\nOutput: %s", err, stderr.String())
	}

	return nil
}

// Reload reloads Nginx configuration without dropping connections
func Reload() error {
	cmd := exec.Command("systemctl", "reload", "nginx")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl reload nginx failed: %w", err)
	}
	return nil
}

// Restart restarts the Nginx service
func Restart() error {
	cmd := exec.Command("systemctl", "restart", "nginx")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl restart nginx failed: %w", err)
	}
	return nil
}

// IsServiceRunning checks if the Nginx service is running
func IsServiceRunning() bool {
	cmd := exec.Command("systemctl", "is-active", "nginx")
	return cmd.Run() == nil
}
