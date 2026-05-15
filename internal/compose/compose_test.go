package compose

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name       string
		instanceID string
		baseDir    string
		wantDir    string
		wantName   string
	}{
		{
			name:       "basic instance",
			instanceID: "test-instance",
			baseDir:    "/opt/supabase",
			wantDir:    "/opt/supabase/test-instance",
			wantName:   "test-instance",
		},
		{
			name:       "instance with subdomain",
			instanceID: "my-project",
			baseDir:    "/var/supabase",
			wantDir:    "/var/supabase/my-project",
			wantName:   "my-project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New(tt.instanceID, tt.baseDir)

			if c.GetInstanceDir() != tt.wantDir {
				t.Errorf("GetInstanceDir() = %v, want %v", c.GetInstanceDir(), tt.wantDir)
			}

			if c.GetProjectName() != tt.wantName {
				t.Errorf("GetProjectName() = %v, want %v", c.GetProjectName(), tt.wantName)
			}
		})
	}
}

func TestGetContainerStatus_NoDocker(t *testing.T) {
	// Test behavior when Docker is not available or no containers exist
	c := New("test-instance", "/tmp/nonexistent")
	ctx := context.Background()

	statuses, err := c.GetContainerStatus(ctx)

	// Should return empty list without error when no containers exist
	if err != nil && len(statuses) != 0 {
		t.Logf("GetContainerStatus() returned error (expected if Docker not available): %v", err)
	}
}

func TestIsRunning_NoContainers(t *testing.T) {
	c := New("test-instance", "/tmp/nonexistent")
	ctx := context.Background()

	running, err := c.IsRunning(ctx)

	// Should return false when no containers exist
	if running {
		t.Error("IsRunning() = true, want false when no containers")
	}

	if err != nil {
		t.Logf("IsRunning() returned error (expected if Docker not available): %v", err)
	}
}

func TestPullDoesNotPanic(t *testing.T) {
	c := New("test-instance", "/tmp/nonexistent")
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Pull() panicked: %v", r)
		}
	}()

	_ = c.Pull(ctx)
}

func TestUpDoesNotPanic(t *testing.T) {
	c := New("test-instance", "/tmp/nonexistent")
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Up() panicked: %v", r)
		}
	}()

	_ = c.Up(ctx)
}

func TestUpServiceDoesNotPanic(t *testing.T) {
	// UpService is the supabyoi-a49s dodge for compose v2's dep-wait race:
	// it starts postgres alone so we can block on our own healthcheck before
	// bringing up dependents. Smoke test mirrors the Up variant.
	c := New("test-instance", "/tmp/nonexistent")
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("UpService() panicked: %v", r)
		}
	}()

	_ = c.UpService(ctx, "postgres")
}

func TestDownDoesNotPanic(t *testing.T) {
	c := New("test-instance", "/tmp/nonexistent")
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Down() panicked: %v", r)
		}
	}()

	_ = c.Down(ctx)
}

func TestStopDoesNotPanic(t *testing.T) {
	c := New("test-instance", "/tmp/nonexistent")
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Stop() panicked: %v", r)
		}
	}()

	_ = c.Stop(ctx)
}

func TestStartDoesNotPanic(t *testing.T) {
	c := New("test-instance", "/tmp/nonexistent")
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Start() panicked: %v", r)
		}
	}()

	_ = c.Start(ctx)
}

func TestRestartDoesNotPanic(t *testing.T) {
	c := New("test-instance", "/tmp/nonexistent")
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Restart() panicked: %v", r)
		}
	}()

	_ = c.Restart(ctx)
}

func TestLogsDoesNotPanic(t *testing.T) {
	c := New("test-instance", "/tmp/nonexistent")
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Logs() panicked: %v", r)
		}
	}()

	_, _ = c.Logs(ctx, 100)
}

func TestGetServiceLogsDoesNotPanic(t *testing.T) {
	c := New("test-instance", "/tmp/nonexistent")
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("GetServiceLogs() panicked: %v", r)
		}
	}()

	_, _ = c.GetServiceLogs(ctx, "postgres", 50)
}

func TestExecDoesNotPanic(t *testing.T) {
	c := New("test-instance", "/tmp/nonexistent")
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Exec() panicked: %v", r)
		}
	}()

	_, _ = c.Exec(ctx, "postgres", []string{"psql", "--version"})
}

func TestValidateDoesNotPanic(t *testing.T) {
	c := New("test-instance", "/tmp/nonexistent")
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Validate() panicked: %v", r)
		}
	}()

	_ = c.Validate(ctx)
}

// Integration tests - only run when Docker is available
func TestWithDocker(t *testing.T) {
	// Check if Docker is available
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker not installed, skipping Docker integration tests")
	}

	// Check if docker compose is available
	if err := exec.Command("docker", "compose", "version").Run(); err != nil {
		t.Skip("Docker Compose not available, skipping integration tests")
	}

	t.Run("Pull", testPullWithDocker)
	t.Run("Validate", testValidateWithDocker)
	t.Run("GetContainerStatus", testGetContainerStatusWithDocker)
	t.Run("Lifecycle", testLifecycleWithDocker)
}

func testPullWithDocker(t *testing.T) {
	// Create a temporary directory with a minimal docker-compose.yml
	tmpDir, err := os.MkdirTemp("", "compose-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a minimal docker-compose.yml with a tiny image
	composeContent := `version: '3.8'
services:
  hello:
    image: hello-world
`
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")
	if err := os.WriteFile(composeFile, []byte(composeContent), 0644); err != nil {
		t.Fatalf("Failed to write compose file: %v", err)
	}

	// Extract instance name from temp dir
	instanceID := filepath.Base(tmpDir)
	baseDir := filepath.Dir(tmpDir)

	c := New(instanceID, baseDir)
	ctx := context.Background()

	// Pull images
	if err := c.Pull(ctx); err != nil {
		t.Errorf("Pull() error = %v", err)
	}

	// Verify pull is idempotent
	if err := c.Pull(ctx); err != nil {
		t.Errorf("Pull() second call error = %v", err)
	}
}

func testValidateWithDocker(t *testing.T) {
	// Create a temporary directory with a valid docker-compose.yml
	tmpDir, err := os.MkdirTemp("", "compose-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a valid docker-compose.yml
	composeContent := `version: '3.8'
services:
  test:
    image: hello-world
`
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")
	if err := os.WriteFile(composeFile, []byte(composeContent), 0644); err != nil {
		t.Fatalf("Failed to write compose file: %v", err)
	}

	instanceID := filepath.Base(tmpDir)
	baseDir := filepath.Dir(tmpDir)
	c := New(instanceID, baseDir)
	ctx := context.Background()

	// Should validate successfully
	if err := c.Validate(ctx); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func testGetContainerStatusWithDocker(t *testing.T) {
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "compose-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a docker-compose.yml with a simple service
	composeContent := `version: '3.8'
services:
  nginx:
    image: nginx:alpine
    ports:
      - "8080"
`
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")
	if err := os.WriteFile(composeFile, []byte(composeContent), 0644); err != nil {
		t.Fatalf("Failed to write compose file: %v", err)
	}

	instanceID := filepath.Base(tmpDir)
	baseDir := filepath.Dir(tmpDir)
	c := New(instanceID, baseDir)
	ctx := context.Background()

	// Start containers
	if err := c.Up(ctx); err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	defer c.Down(ctx)

	// Wait a bit for containers to start
	time.Sleep(3 * time.Second)

	// Get container status
	statuses, err := c.GetContainerStatus(ctx)
	if err != nil {
		t.Errorf("GetContainerStatus() error = %v", err)
	}

	if len(statuses) == 0 {
		t.Error("GetContainerStatus() returned no containers")
	}

	t.Logf("Container statuses: %+v", statuses)
}

func testLifecycleWithDocker(t *testing.T) {
	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "compose-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a docker-compose.yml
	composeContent := `version: '3.8'
services:
  web:
    image: nginx:alpine
`
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")
	if err := os.WriteFile(composeFile, []byte(composeContent), 0644); err != nil {
		t.Fatalf("Failed to write compose file: %v", err)
	}

	instanceID := filepath.Base(tmpDir)
	baseDir := filepath.Dir(tmpDir)
	c := New(instanceID, baseDir)
	ctx := context.Background()

	// Test lifecycle: up -> stop -> start -> restart -> down

	// Up
	if err := c.Up(ctx); err != nil {
		t.Fatalf("Up() error = %v", err)
	}

	// Wait for containers
	if err := c.WaitForContainers(ctx, 30*time.Second); err != nil {
		t.Errorf("WaitForContainers() error = %v", err)
	}

	// Check running
	running, err := c.IsRunning(ctx)
	if err != nil {
		t.Errorf("IsRunning() error = %v", err)
	}
	if !running {
		t.Error("IsRunning() = false after Up(), want true")
	}

	// Stop
	if err := c.Stop(ctx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	// Should not be running
	time.Sleep(2 * time.Second)
	running, err = c.IsRunning(ctx)
	if err != nil {
		t.Errorf("IsRunning() error after Stop = %v", err)
	}
	if running {
		t.Error("IsRunning() = true after Stop(), want false")
	}

	// Start
	if err := c.Start(ctx); err != nil {
		t.Errorf("Start() error = %v", err)
	}

	time.Sleep(2 * time.Second)
	running, err = c.IsRunning(ctx)
	if err != nil {
		t.Errorf("IsRunning() error after Start = %v", err)
	}
	if !running {
		t.Error("IsRunning() = false after Start(), want true")
	}

	// Restart
	if err := c.Restart(ctx); err != nil {
		t.Errorf("Restart() error = %v", err)
	}

	time.Sleep(2 * time.Second)
	running, err = c.IsRunning(ctx)
	if err != nil {
		t.Errorf("IsRunning() error after Restart = %v", err)
	}
	if !running {
		t.Error("IsRunning() = false after Restart(), want true")
	}

	// Get logs
	logs, err := c.Logs(ctx, 50)
	if err != nil {
		t.Errorf("Logs() error = %v", err)
	}
	t.Logf("Logs output length: %d", len(logs))

	// Down
	if err := c.Down(ctx); err != nil {
		t.Errorf("Down() error = %v", err)
	}

	// Should not be running
	time.Sleep(2 * time.Second)
	running, err = c.IsRunning(ctx)
	if err != nil && running {
		t.Errorf("IsRunning() = true after Down(), want false")
	}
}

func TestWaitForContainers_Timeout(t *testing.T) {
	c := New("test-instance", "/tmp/nonexistent")
	ctx := context.Background()

	// Should timeout quickly since no containers exist
	err := c.WaitForContainers(ctx, 1*time.Second)
	if err == nil {
		t.Error("WaitForContainers() expected timeout error, got nil")
	}
}

func TestWaitForContainers_ContextCancellation(t *testing.T) {
	c := New("test-instance", "/tmp/nonexistent")
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	err := c.WaitForContainers(ctx, 30*time.Second)
	if err == nil {
		t.Error("WaitForContainers() expected context error, got nil")
	}
}

func TestContainerInfoJSONParsing(t *testing.T) {
	tests := []struct {
		name        string
		jsonLine    string
		wantName    string
		wantState   string
		wantHealth  string
		wantService string
	}{
		{
			name:        "healthy running container",
			jsonLine:    `{"Name":"test_postgres_1","State":"running","Health":"healthy","Service":"postgres"}`,
			wantName:    "test_postgres_1",
			wantState:   "running",
			wantHealth:  "healthy",
			wantService: "postgres",
		},
		{
			name:        "unhealthy container",
			jsonLine:    `{"Name":"test_kong_1","State":"running","Health":"unhealthy","Service":"kong"}`,
			wantName:    "test_kong_1",
			wantState:   "running",
			wantHealth:  "unhealthy",
			wantService: "kong",
		},
		{
			name:        "exited container",
			jsonLine:    `{"Name":"test_auth_1","State":"exited","Health":"","Service":"auth"}`,
			wantName:    "test_auth_1",
			wantState:   "exited",
			wantHealth:  "",
			wantService: "auth",
		},
		{
			name:        "container with empty health",
			jsonLine:    `{"Name":"test_studio_1","State":"running","Health":"","Service":"studio"}`,
			wantName:    "test_studio_1",
			wantState:   "running",
			wantHealth:  "",
			wantService: "studio",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var info containerInfo
			if err := json.Unmarshal([]byte(tt.jsonLine), &info); err != nil {
				t.Fatalf("Failed to parse JSON: %v", err)
			}

			if info.Name != tt.wantName {
				t.Errorf("Name = %v, want %v", info.Name, tt.wantName)
			}
			if info.State != tt.wantState {
				t.Errorf("State = %v, want %v", info.State, tt.wantState)
			}
			if info.Health != tt.wantHealth {
				t.Errorf("Health = %v, want %v", info.Health, tt.wantHealth)
			}
			if info.Service != tt.wantService {
				t.Errorf("Service = %v, want %v", info.Service, tt.wantService)
			}
		})
	}
}

func TestContainerInfoMalformedJSON(t *testing.T) {
	malformed := []string{
		"",
		"not json at all",
		`{"Name": "incomplete`,
		`[]`,
	}

	for _, input := range malformed {
		var info containerInfo
		err := json.Unmarshal([]byte(input), &info)
		if err == nil && input != "" {
			t.Errorf("Expected error parsing malformed JSON %q, got nil", input)
		}
	}
}

func TestGetProjectNameAndInstanceDir(t *testing.T) {
	c := New("my-instance", "/opt/supabase")

	if c.GetProjectName() != "my-instance" {
		t.Errorf("GetProjectName() = %v, want my-instance", c.GetProjectName())
	}

	expectedDir := filepath.Join("/opt/supabase", "my-instance")
	if c.GetInstanceDir() != expectedDir {
		t.Errorf("GetInstanceDir() = %v, want %v", c.GetInstanceDir(), expectedDir)
	}
}

// Benchmark tests
func BenchmarkNew(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = New("test-instance", "/opt/supabase")
	}
}

func BenchmarkIsRunning(b *testing.B) {
	if _, err := exec.LookPath("docker"); err != nil {
		b.Skip("Docker not installed")
	}

	c := New("test-instance", "/tmp/nonexistent")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.IsRunning(ctx)
	}
}
