package nginx

import (
	"os"
	"os/exec"
	"testing"
)

func TestIsInstalled(t *testing.T) {
	// Just verify the function doesn't panic
	// Result depends on whether Nginx is installed
	_ = IsInstalled()
}

func TestGetVersion(t *testing.T) {
	// Check if Nginx is available
	if _, err := exec.LookPath("nginx"); err != nil {
		t.Skip("Nginx not installed, skipping version test")
	}

	version, err := GetVersion()
	if err != nil {
		t.Fatalf("GetVersion() error = %v", err)
	}

	if version == "" {
		t.Error("GetVersion() returned empty string")
	}

	t.Logf("Nginx version: %s", version)
}

func TestIsServiceRunning(t *testing.T) {
	// Check if Nginx is available
	if _, err := exec.LookPath("nginx"); err != nil {
		t.Skip("Nginx not installed, skipping service test")
	}

	// Just verify the function doesn't panic
	running := IsServiceRunning()
	t.Logf("Nginx service running: %v", running)
}

func TestGenerateSiteConfigContent(t *testing.T) {
	tests := []struct {
		name    string
		config  *SiteConfig
		wantErr bool
	}{
		{
			name: "HTTP configuration",
			config: &SiteConfig{
				Subdomain:    "testapp",
				Domain:       "testapp.supabyoi.com",
				StudioDomain: "studio-testapp.supabyoi.com",
				KongPort:     8000,
				StudioPort:   3000,
				TLSEnabled:   false,
			},
			wantErr: false,
		},
		{
			name: "HTTPS configuration",
			config: &SiteConfig{
				Subdomain:    "testapp",
				Domain:       "testapp.supabyoi.com",
				StudioDomain: "studio-testapp.supabyoi.com",
				KongPort:     8000,
				StudioPort:   3000,
				TLSEnabled:   true,
				CertPath:     "/etc/letsencrypt/live/testapp.supabyoi.com/fullchain.pem",
				KeyPath:      "/etc/letsencrypt/live/testapp.supabyoi.com/privkey.pem",
			},
			wantErr: false,
		},
		{
			name: "Custom ports",
			config: &SiteConfig{
				Subdomain:    "customapp",
				Domain:       "customapp.supabyoi.com",
				StudioDomain: "studio-customapp.supabyoi.com",
				KongPort:     9000,
				StudioPort:   4000,
				TLSEnabled:   false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := generateSiteConfigContent(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("generateSiteConfigContent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if content == "" {
					t.Error("generateSiteConfigContent() returned empty string")
				}

				// Verify content contains expected elements
				if !contains(content, tt.config.Subdomain) {
					t.Errorf("Config missing subdomain: %s", tt.config.Subdomain)
				}
				if !contains(content, tt.config.Domain) {
					t.Errorf("Config missing domain: %s", tt.config.Domain)
				}
				if !contains(content, tt.config.StudioDomain) {
					t.Errorf("Config missing studio domain: %s", tt.config.StudioDomain)
				}

				// Verify TLS configuration
				if tt.config.TLSEnabled {
					if !contains(content, "ssl_certificate") {
						t.Error("HTTPS config missing ssl_certificate directive")
					}
					if !contains(content, "443 ssl") {
						t.Error("HTTPS config missing 443 ssl listen directive")
					}
					// Verify TLS 1.2+ enforcement
					if !contains(content, "ssl_protocols TLSv1.2 TLSv1.3") {
						t.Error("HTTPS config missing TLS 1.2/1.3 protocol enforcement")
					}
					// Verify modern cipher suite
					if !contains(content, "ECDHE-ECDSA-AES128-GCM-SHA256") {
						t.Error("HTTPS config missing modern cipher suite")
					}
					// Verify HSTS header
					if !contains(content, "Strict-Transport-Security") {
						t.Error("HTTPS config missing HSTS header")
					}
					if !contains(content, "max-age=63072000") {
						t.Error("HSTS header missing max-age directive")
					}
					// Verify HTTP-to-HTTPS redirect block
					if !contains(content, "return 301 https://") {
						t.Error("HTTPS config missing HTTP-to-HTTPS redirect")
					}
				} else {
					if !contains(content, "listen 80") {
						t.Error("HTTP config missing listen 80 directive")
					}
					// Verify no HSTS on HTTP-only config
					if contains(content, "Strict-Transport-Security") {
						t.Error("HTTP config should not have HSTS header")
					}
					// Verify no redirect on HTTP-only config
					if contains(content, "return 301 https://") {
						t.Error("HTTP config should not have HTTPS redirect")
					}
				}

				// Verify rate limiting
				if !contains(content, "limit_req zone=supabyoi_auth") {
					t.Error("Config missing auth rate limiting")
				}
				if !contains(content, "limit_req zone=supabyoi_api") {
					t.Error("Config missing API rate limiting")
				}
				if !contains(content, "limit_req zone=supabyoi_storage") {
					t.Error("Config missing storage rate limiting")
				}
				if !contains(content, "client_max_body_size 100m") {
					t.Error("Config missing storage upload size limit")
				}

				// Verify upstream configuration
				if !contains(content, "upstream "+tt.config.Subdomain+"_api") {
					t.Error("Config missing API upstream")
				}
				if !contains(content, "upstream "+tt.config.Subdomain+"_studio") {
					t.Error("Config missing Studio upstream")
				}

				t.Logf("Generated config length: %d bytes", len(content))
			}
		})
	}
}

func TestSiteConfigRoundtrip(t *testing.T) {
	// This test requires root privileges and an actual Nginx installation
	if os.Geteuid() != 0 {
		t.Skip("Skipping test that requires root privileges")
	}

	if _, err := exec.LookPath("nginx"); err != nil {
		t.Skip("Nginx not installed, skipping roundtrip test")
	}

	config := &SiteConfig{
		Subdomain:    "test-roundtrip",
		Domain:       "test-roundtrip.supabyoi.com",
		StudioDomain: "studio-test-roundtrip.supabyoi.com",
		KongPort:     8000,
		StudioPort:   3000,
		TLSEnabled:   false,
	}

	// Create site config
	if err := CreateSiteConfig(config); err != nil {
		t.Fatalf("CreateSiteConfig() error = %v", err)
	}

	// Verify file exists
	configPath := SupabaseSitesDir + "/test-roundtrip.conf"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Config file was not created")
	}

	// Clean up
	if err := RemoveSiteConfig("test-roundtrip"); err != nil {
		t.Errorf("RemoveSiteConfig() error = %v", err)
	}

	// Verify file removed
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Error("Config file was not removed")
	}
}

func TestInstallIdempotency(t *testing.T) {
	// This test verifies that Install is idempotent
	// It requires Nginx to be already installed and write access to /etc/nginx

	if _, err := exec.LookPath("nginx"); err != nil {
		t.Skip("Nginx not installed, skipping idempotency test")
	}

	// Skip if we don't have write permission to /etc/nginx (e.g. CI without sudo)
	if err := os.MkdirAll("/etc/nginx/supabase-sites", 0755); err != nil {
		t.Skip("No write access to /etc/nginx, skipping idempotency test")
	}

	// Call Install on an already-installed system
	// Should return without error (idempotent)
	err := Install("root")
	if err != nil {
		t.Errorf("Install() on already-installed system returned error: %v", err)
	}
}

func TestEnsureIncludeDirective(t *testing.T) {
	// This test requires root privileges
	if os.Geteuid() != 0 {
		t.Skip("Skipping test that requires root privileges")
	}

	if _, err := exec.LookPath("nginx"); err != nil {
		t.Skip("Nginx not installed")
	}

	// Test is idempotent - should work even if already present
	err := EnsureIncludeDirective()
	if err != nil {
		t.Errorf("EnsureIncludeDirective() error = %v", err)
	}

	// Call again - should be idempotent
	err = EnsureIncludeDirective()
	if err != nil {
		t.Errorf("EnsureIncludeDirective() second call error = %v", err)
	}
}

func TestConfigureRateLimiting(t *testing.T) {
	// This test requires root privileges
	if os.Geteuid() != 0 {
		t.Skip("Skipping test that requires root privileges")
	}

	if _, err := exec.LookPath("nginx"); err != nil {
		t.Skip("Nginx not installed")
	}

	// Test is idempotent - should work even if already configured
	err := ConfigureRateLimiting()
	if err != nil {
		t.Errorf("ConfigureRateLimiting() error = %v", err)
	}

	// Call again - should be idempotent
	err = ConfigureRateLimiting()
	if err != nil {
		t.Errorf("ConfigureRateLimiting() second call error = %v", err)
	}
}

func TestTestConfig(t *testing.T) {
	// Check if Nginx is available
	if _, err := exec.LookPath("nginx"); err != nil {
		t.Skip("Nginx not installed, skipping config test")
	}

	// Test the current nginx configuration
	err := TestConfig()
	if err != nil {
		t.Logf("TestConfig() error = %v (expected if nginx not properly configured)", err)
	}
}

func TestGenerateSiteConfigSecurityHeaders(t *testing.T) {
	config := &SiteConfig{
		Subdomain:    "secure-app",
		Domain:       "secure-app.supabyoi.com",
		StudioDomain: "studio-secure-app.supabyoi.com",
		KongPort:     8000,
		StudioPort:   3000,
		TLSEnabled:   false,
	}

	content, err := generateSiteConfigContent(config)
	if err != nil {
		t.Fatalf("generateSiteConfigContent() error = %v", err)
	}

	securityHeaders := []string{
		"X-Frame-Options",
		"X-Content-Type-Options",
		"X-XSS-Protection",
	}

	for _, header := range securityHeaders {
		if !contains(content, header) {
			t.Errorf("Config missing security header: %s", header)
		}
	}
}

func TestGenerateSiteConfigWebSocket(t *testing.T) {
	config := &SiteConfig{
		Subdomain:    "ws-app",
		Domain:       "ws-app.supabyoi.com",
		StudioDomain: "studio-ws-app.supabyoi.com",
		KongPort:     8000,
		StudioPort:   3000,
		TLSEnabled:   false,
	}

	content, err := generateSiteConfigContent(config)
	if err != nil {
		t.Fatalf("generateSiteConfigContent() error = %v", err)
	}

	wsDirectives := []string{
		"proxy_http_version 1.1",
		"Upgrade $http_upgrade",
		`Connection "upgrade"`,
	}

	for _, directive := range wsDirectives {
		if !contains(content, directive) {
			t.Errorf("Config missing WebSocket directive: %s", directive)
		}
	}
}

func TestGenerateSiteConfigTLSSettings(t *testing.T) {
	config := &SiteConfig{
		Subdomain:    "tls-app",
		Domain:       "tls-app.supabyoi.com",
		StudioDomain: "studio-tls-app.supabyoi.com",
		KongPort:     8000,
		StudioPort:   3000,
		TLSEnabled:   true,
		CertPath:     "/etc/letsencrypt/live/tls-app.supabyoi.com/fullchain.pem",
		KeyPath:      "/etc/letsencrypt/live/tls-app.supabyoi.com/privkey.pem",
	}

	content, err := generateSiteConfigContent(config)
	if err != nil {
		t.Fatalf("generateSiteConfigContent() error = %v", err)
	}

	tlsDirectives := []string{
		"ssl_protocols TLSv1.2 TLSv1.3",
		"ssl_prefer_server_ciphers off",
		"ssl_session_cache",
		config.CertPath,
		config.KeyPath,
	}

	for _, directive := range tlsDirectives {
		if !contains(content, directive) {
			t.Errorf("TLS config missing directive: %s", directive)
		}
	}

	// Should have listen 443 ssl for the main server block
	if !contains(content, "listen 443 ssl") {
		t.Error("TLS config should have 'listen 443 ssl' directive")
	}

	// Should have HTTP to HTTPS redirect block with listen 80
	if !contains(content, "return 301 https://") {
		t.Error("TLS config should have HTTP to HTTPS redirect")
	}
}

func TestGenerateSiteConfigACMEChallenge(t *testing.T) {
	config := &SiteConfig{
		Subdomain:    "acme-app",
		Domain:       "acme-app.supabyoi.com",
		StudioDomain: "studio-acme-app.supabyoi.com",
		KongPort:     8000,
		StudioPort:   3000,
		TLSEnabled:   false,
	}

	content, err := generateSiteConfigContent(config)
	if err != nil {
		t.Fatalf("generateSiteConfigContent() error = %v", err)
	}

	if contains(content, ".well-known/acme-challenge") {
		t.Error("Config should not contain ACME challenge location (removed in Origin CA migration)")
	}
}

func TestGenerateSiteConfigRateLimitZones(t *testing.T) {
	config := &SiteConfig{
		Subdomain:    "rl-app",
		Domain:       "rl-app.supabyoi.com",
		StudioDomain: "studio-rl-app.supabyoi.com",
		KongPort:     8000,
		StudioPort:   3000,
		TLSEnabled:   false,
	}

	content, err := generateSiteConfigContent(config)
	if err != nil {
		t.Fatalf("generateSiteConfigContent() error = %v", err)
	}

	// Verify all three rate limiting zones are referenced
	rateLimitZones := []string{
		"limit_req zone=supabyoi_auth",
		"limit_req zone=supabyoi_api",
		"limit_req zone=supabyoi_storage",
	}

	for _, zone := range rateLimitZones {
		if !contains(content, zone) {
			t.Errorf("Config missing rate limit zone: %s", zone)
		}
	}

	// Verify 429 error handling
	if !contains(content, "429") {
		t.Error("Config missing 429 rate limit response handling")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && s != "" && substr != "" &&
		   s != substr && (s == substr || len(s) >= len(substr) &&
		   (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		    len(s) > len(substr) && findInString(s, substr)))
}

func findInString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestInstall(t *testing.T) {
	tests := []struct {
		name     string
		username string
		wantErr  bool
	}{
		{
			name:     "install with valid username",
			username: "testuser",
			wantErr:  false, // May fail if not root, but shouldn't panic
		},
		{
			name:     "install with empty username",
			username: "",
			wantErr:  false, // Should handle gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: This will likely fail if not running as root
			// or if Nginx is already installed, but it tests the code path
			err := Install(tt.username)

			// We just verify it doesn't panic
			t.Logf("Install(%q) returned error: %v", tt.username, err)
		})
	}
}

func TestInstallDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Install() panicked: %v", r)
		}
	}()

	_ = Install("testuser")
}

func TestCreateSiteConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  *SiteConfig
		wantErr bool
	}{
		{
			name: "valid HTTP config",
			config: &SiteConfig{
				Subdomain:    "test",
				Domain:       "test.supabyoi.com",
				StudioDomain: "studio-test.supabyoi.com",
				KongPort:     8000,
				StudioPort:   3000,
				TLSEnabled:   false,
			},
			wantErr: false, // Will fail if no permissions, but tests logic
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CreateSiteConfig(tt.config)
			// Just verify it doesn't panic
			t.Logf("CreateSiteConfig() returned error: %v", err)
		})
	}
}

func TestCreateSiteConfigNilReturnsError(t *testing.T) {
	// CreateSiteConfig(nil) should return an error (from ConfigureRateLimiting
	// failing before nil dereference) or panic - either is acceptable
	defer func() {
		if r := recover(); r != nil {
			t.Logf("CreateSiteConfig(nil) panicked as expected: %v", r)
		}
	}()

	err := CreateSiteConfig(nil)
	if err == nil {
		t.Error("CreateSiteConfig(nil) should return an error or panic")
	}
}

func TestRemoveSiteConfig(t *testing.T) {
	tests := []struct {
		name      string
		subdomain string
		wantErr   bool
	}{
		{
			name:      "remove existing config",
			subdomain: "test",
			wantErr:   false, // May fail if no permissions
		},
		{
			name:      "remove with empty subdomain",
			subdomain: "",
			wantErr:   false, // Should handle gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RemoveSiteConfig(tt.subdomain)
			t.Logf("RemoveSiteConfig(%q) returned error: %v", tt.subdomain, err)
		})
	}
}

func TestSetupSupabaseSitesDir(t *testing.T) {
	// Test that function doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SetupSupabaseSitesDir() panicked: %v", r)
		}
	}()

	_ = SetupSupabaseSitesDir("testuser")
}

func TestGetVersionDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("GetVersion() panicked: %v", r)
		}
	}()

	_, _ = GetVersion()
}

func TestReload(t *testing.T) {
	// Test that function doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Reload() panicked: %v", r)
		}
	}()

	_ = Reload()
}

func TestRestart(t *testing.T) {
	// Test that function doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Restart() panicked: %v", r)
		}
	}()

	_ = Restart()
}

func TestGenerateSiteConfigNoAuthGate(t *testing.T) {
	config := &SiteConfig{
		Subdomain:    "noauth-app",
		Domain:       "noauth-app.supabyoi.com",
		StudioDomain: "studio-noauth-app.supabyoi.com",
		KongPort:     8000,
		StudioPort:   3000,
		TLSEnabled:   false,
	}

	content, err := generateSiteConfigContent(config)
	if err != nil {
		t.Fatalf("generateSiteConfigContent() error = %v", err)
	}

	// Auth gate should NOT be present (Kong handles auth now)
	forbiddenDirectives := []string{
		"auth_request",
		"/auth-check",
		"@login_redirect",
		"@access_denied",
		"validate-studio-access",
	}

	for _, directive := range forbiddenDirectives {
		if contains(content, directive) {
			t.Errorf("Config should NOT contain old auth directive: %s", directive)
		}
	}

	// Studio proxy should still work
	if !contains(content, "proxy_pass http://noauth-app_studio") {
		t.Error("Config should still have Studio proxy_pass")
	}
}

// TestGenerateSiteConfigStudioBasicAuth verifies that when StudioHtpasswdPath
// is set, the studio server block emits auth_basic directives — and when it
// isn't, no directive is emitted (so the absence is obvious in tests).
func TestGenerateSiteConfigStudioBasicAuth(t *testing.T) {
	withAuth := &SiteConfig{
		Subdomain:          "gated",
		Domain:             "gated.supabyoi.com",
		StudioDomain:       "studio-gated.supabyoi.com",
		KongPort:           8000,
		StudioPort:         3000,
		TLSEnabled:         false,
		StudioHtpasswdPath: "/etc/nginx/supabase-sites/gated.htpasswd",
	}
	content, err := generateSiteConfigContent(withAuth)
	if err != nil {
		t.Fatalf("generateSiteConfigContent() error = %v", err)
	}
	if !contains(content, `auth_basic "Supabase Studio"`) {
		t.Error("studio block should emit auth_basic when StudioHtpasswdPath is set")
	}
	if !contains(content, "auth_basic_user_file /etc/nginx/supabase-sites/gated.htpasswd") {
		t.Error("studio block should reference the htpasswd path")
	}

	withoutAuth := *withAuth
	withoutAuth.StudioHtpasswdPath = ""
	content2, err := generateSiteConfigContent(&withoutAuth)
	if err != nil {
		t.Fatalf("generateSiteConfigContent() error = %v", err)
	}
	if contains(content2, "auth_basic") {
		t.Error("studio block must NOT emit auth_basic when StudioHtpasswdPath is empty")
	}
}

// TestWriteHtpasswdRoundtrip writes a file via the public helper and reads it
// back to verify the {SHA} encoding nginx expects.
func TestWriteHtpasswdRoundtrip(t *testing.T) {
	// Redirect SupabaseSitesDir at a temp dir for this test so it works in
	// any environment, including CI where /etc/nginx isn't writable.
	original := SupabaseSitesDir
	SupabaseSitesDir = t.TempDir()
	t.Cleanup(func() { SupabaseSitesDir = original })

	path, err := WriteHtpasswd("test-roundtrip", "supabase", "hunter2-very-long-test-password")
	if err != nil {
		t.Fatalf("WriteHtpasswd: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	line := string(data)
	if !contains(line, "supabase:{SHA}") {
		t.Errorf("expected {SHA} format, got %q", line)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("expected 0644 (nginx worker must read it), got %o", info.Mode().Perm())
	}
}

// TestWriteHtpasswdRequiresBoth checks the empty-input guard.
func TestWriteHtpasswdRequiresBoth(t *testing.T) {
	if _, err := WriteHtpasswd("x", "", "p"); err == nil {
		t.Error("empty username should error")
	}
	if _, err := WriteHtpasswd("x", "u", ""); err == nil {
		t.Error("empty password should error")
	}
}

// Benchmark tests
func BenchmarkIsInstalled(b *testing.B) {
	for i := 0; i < b.N; i++ {
		IsInstalled()
	}
}

func BenchmarkIsServiceRunning(b *testing.B) {
	if _, err := exec.LookPath("nginx"); err != nil {
		b.Skip("Nginx not installed")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsServiceRunning()
	}
}

func BenchmarkGenerateSiteConfigContent(b *testing.B) {
	config := &SiteConfig{
		Subdomain:    "benchapp",
		Domain:       "benchapp.supabyoi.com",
		StudioDomain: "studio-benchapp.supabyoi.com",
		KongPort:     8000,
		StudioPort:   3000,
		TLSEnabled:   false,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = generateSiteConfigContent(config)
	}
}
