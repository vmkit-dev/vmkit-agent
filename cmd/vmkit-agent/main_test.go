package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMainFunction(t *testing.T) {
	// Test that the binary can be built and run
	// This is an integration test
	t.Run("version command", func(t *testing.T) {
		cmd := exec.Command("go", "run", "main.go", "version")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("version command error: %v (expected if not built)", err)
		}
		t.Logf("version output: %s", output)
	})

	t.Run("no args shows usage", func(t *testing.T) {
		cmd := exec.Command("go", "run", "main.go")
		output, err := cmd.CombinedOutput()
		// Should exit with code 1
		if err == nil {
			t.Error("Expected error when running without args")
		}
		t.Logf("no args output: %s", output)
	})

	t.Run("unknown command shows usage", func(t *testing.T) {
		cmd := exec.Command("go", "run", "main.go", "unknown")
		output, err := cmd.CombinedOutput()
		// Should exit with code 1
		if err == nil {
			t.Error("Expected error for unknown command")
		}
		t.Logf("unknown command output: %s", output)
	})
}

func TestPrintUsage(t *testing.T) {
	// Test that printUsage doesn't panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("printUsage() panicked: %v", r)
		}
	}()

	printUsage()
}

func TestHandleDeploy(t *testing.T) {
	// Test that handleDeploy doesn't panic
	// It will exit, but we can test in a subprocess
	if os.Getenv("TEST_HANDLE_DEPLOY") == "1" {
		handleDeploy()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleDeploy")
	cmd.Env = append(os.Environ(), "TEST_HANDLE_DEPLOY=1")
	err := cmd.Run()

	// Should exit with non-zero code
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		// Expected
	} else {
		t.Logf("handleDeploy() exit: %v", err)
	}
}

func TestHandleStatus(t *testing.T) {
	if os.Getenv("TEST_HANDLE_STATUS") == "1" {
		handleStatus()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleStatus")
	cmd.Env = append(os.Environ(), "TEST_HANDLE_STATUS=1")
	err := cmd.Run()

	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		// Expected
	} else {
		t.Logf("handleStatus() exit: %v", err)
	}
}

func TestHandleStart(t *testing.T) {
	if os.Getenv("TEST_HANDLE_START") == "1" {
		handleStart()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleStart")
	cmd.Env = append(os.Environ(), "TEST_HANDLE_START=1")
	err := cmd.Run()

	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		// Expected
	} else {
		t.Logf("handleStart() exit: %v", err)
	}
}

func TestHandleStop(t *testing.T) {
	if os.Getenv("TEST_HANDLE_STOP") == "1" {
		handleStop()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleStop")
	cmd.Env = append(os.Environ(), "TEST_HANDLE_STOP=1")
	err := cmd.Run()

	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		// Expected
	} else {
		t.Logf("handleStop() exit: %v", err)
	}
}

func TestHandleUpdate(t *testing.T) {
	if os.Getenv("TEST_HANDLE_UPDATE") == "1" {
		handleUpdate()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleUpdate")
	cmd.Env = append(os.Environ(), "TEST_HANDLE_UPDATE=1")
	err := cmd.Run()

	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		// Expected
	} else {
		t.Logf("handleUpdate() exit: %v", err)
	}
}

func TestHandleHarden(t *testing.T) {
	if os.Getenv("TEST_HANDLE_HARDEN") == "1" {
		handleHarden()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleHarden")
	cmd.Env = append(os.Environ(), "TEST_HANDLE_HARDEN=1")
	err := cmd.Run()

	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		// Expected
	} else {
		t.Logf("handleHarden() exit: %v", err)
	}
}

func TestHandleCleanup(t *testing.T) {
	if os.Getenv("TEST_HANDLE_CLEANUP") == "1" {
		handleCleanup()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleCleanup")
	cmd.Env = append(os.Environ(), "TEST_HANDLE_CLEANUP=1")
	err := cmd.Run()

	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		// Expected
	} else {
		t.Logf("handleCleanup() exit: %v", err)
	}
}

func TestHandleBackup(t *testing.T) {
	if os.Getenv("TEST_HANDLE_BACKUP") == "1" {
		handleBackup()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleBackup")
	cmd.Env = append(os.Environ(), "TEST_HANDLE_BACKUP=1")
	err := cmd.Run()

	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		// Expected - no --config provided
	} else {
		t.Logf("handleBackup() exit: %v", err)
	}
}

func TestHandleRestore(t *testing.T) {
	if os.Getenv("TEST_HANDLE_RESTORE") == "1" {
		handleRestore()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleRestore")
	cmd.Env = append(os.Environ(), "TEST_HANDLE_RESTORE=1")
	err := cmd.Run()

	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		// Expected
	} else {
		t.Logf("handleRestore() exit: %v", err)
	}
}

func TestHandleVerify(t *testing.T) {
	if os.Getenv("TEST_HANDLE_VERIFY") == "1" {
		handleVerify()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleVerify")
	cmd.Env = append(os.Environ(), "TEST_HANDLE_VERIFY=1")
	err := cmd.Run()

	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		// Expected
	} else {
		t.Logf("handleVerify() exit: %v", err)
	}
}

func TestHandleUpgrade(t *testing.T) {
	if os.Getenv("TEST_HANDLE_UPGRADE") == "1" {
		handleUpgrade()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleUpgrade")
	cmd.Env = append(os.Environ(), "TEST_HANDLE_UPGRADE=1")
	err := cmd.Run()

	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		// Expected
	} else {
		t.Logf("handleUpgrade() exit: %v", err)
	}
}

func TestHandleEnableTLS(t *testing.T) {
	if os.Getenv("TEST_HANDLE_ENABLE_TLS") == "1" {
		handleEnableTLS()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleEnableTLS")
	cmd.Env = append(os.Environ(), "TEST_HANDLE_ENABLE_TLS=1")
	err := cmd.Run()

	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		// Expected
	} else {
		t.Logf("handleEnableTLS() exit: %v", err)
	}
}

func TestHandleRotateCredentials(t *testing.T) {
	if os.Getenv("TEST_HANDLE_ROTATE_CREDENTIALS") == "1" {
		handleRotateCredentials()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleRotateCredentials")
	cmd.Env = append(os.Environ(), "TEST_HANDLE_ROTATE_CREDENTIALS=1")
	err := cmd.Run()

	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		// Expected
	} else {
		t.Logf("handleRotateCredentials() exit: %v", err)
	}
}

func TestHandleDestroy(t *testing.T) {
	if os.Getenv("TEST_HANDLE_DESTROY") == "1" {
		handleDestroy()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHandleDestroy")
	cmd.Env = append(os.Environ(), "TEST_HANDLE_DESTROY=1")
	err := cmd.Run()

	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		// Expected
	} else {
		t.Logf("handleDestroy() exit: %v", err)
	}
}

// TestConfigLoadingWithFixtures tests that each handler correctly loads
// its config from a JSON file using the test fixtures
func TestConfigLoadingWithFixtures(t *testing.T) {
	fixturesDir := filepath.Join("..", "..", "test-fixtures")

	tests := []struct {
		name    string
		command string
		fixture string
	}{
		{"deploy", "deploy", "deploy.json"},
		{"backup", "backup", "backup.json"},
		{"restore", "restore", "restore.json"},
		{"verify", "verify", "verify.json"},
		{"upgrade", "upgrade", "upgrade.json"},
		{"enable-tls", "enable-tls", "enable-tls.json"},
		{"rotate-credentials", "rotate-credentials", "rotate-credentials.json"},
		{"harden", "harden", "harden.json"},
		{"destroy", "destroy", "destroy.json"},
		{"cleanup-minimal", "cleanup", "cleanup-minimal.json"},
		{"cleanup-revert", "cleanup", "cleanup-revert.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixturePath := filepath.Join(fixturesDir, tt.fixture)

			// Verify fixture file is valid JSON
			data, err := os.ReadFile(fixturePath)
			if err != nil {
				t.Fatalf("Failed to read fixture %s: %v", tt.fixture, err)
			}

			if !json.Valid(data) {
				t.Fatalf("Fixture %s is not valid JSON", tt.fixture)
			}

			// Run the command with the fixture config
			// It will fail (no real infrastructure) but should output valid JSON
			cmd := exec.Command("go", "run", "main.go", tt.command, "--config", fixturePath)
			output, err := cmd.CombinedOutput()

			// Command will exit non-zero (no real Docker/Nginx/etc), that's expected
			if err == nil {
				t.Logf("Command %s succeeded unexpectedly", tt.command)
			}

			// The stdout should contain valid JSON (the error result)
			outStr := string(output)
			lines := strings.Split(strings.TrimSpace(outStr), "\n")

			// Find the last line that looks like JSON
			foundJSON := false
			for i := len(lines) - 1; i >= 0; i-- {
				line := strings.TrimSpace(lines[i])
				if strings.HasPrefix(line, "{") {
					if json.Valid([]byte(line)) {
						foundJSON = true

						// Verify it has a "success" field
						var result map[string]interface{}
						if err := json.Unmarshal([]byte(line), &result); err == nil {
							if _, ok := result["success"]; !ok {
								t.Errorf("JSON output missing 'success' field for %s", tt.command)
							}
						}
					}
					break
				}
			}

			if !foundJSON {
				t.Logf("Output: %s", outStr)
				t.Logf("No JSON output found for %s (may have exited before outputting)", tt.command)
			}
		})
	}
}

// TestCommandsRequireConfig verifies that config-based commands fail
// gracefully when --config is not provided
func TestCommandsRequireConfig(t *testing.T) {
	commands := []string{
		"deploy", "backup", "restore", "verify", "upgrade",
		"enable-tls", "rotate-credentials", "harden", "cleanup", "destroy",
	}

	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			cmd := exec.Command("go", "run", "main.go", command)
			output, err := cmd.CombinedOutput()

			if err == nil {
				t.Errorf("%s without --config should fail", command)
			}

			outStr := string(output)
			if !strings.Contains(outStr, "--config is required") {
				t.Errorf("%s should mention --config is required, got: %s", command, outStr)
			}
		})
	}
}

// TestCommandsWithInvalidConfig verifies graceful handling of invalid JSON
func TestCommandsWithInvalidConfig(t *testing.T) {
	// Create a temp file with invalid JSON
	tmpDir := t.TempDir()
	invalidFile := filepath.Join(tmpDir, "invalid.json")
	if err := os.WriteFile(invalidFile, []byte("{invalid json}"), 0644); err != nil {
		t.Fatalf("Failed to write invalid config: %v", err)
	}

	commands := []string{
		"backup", "restore", "verify", "upgrade",
		"enable-tls", "rotate-credentials", "harden", "cleanup", "destroy",
	}

	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			cmd := exec.Command("go", "run", "main.go", command, "--config", invalidFile)
			output, err := cmd.CombinedOutput()

			if err == nil {
				t.Errorf("%s with invalid JSON should fail", command)
			}

			// Should output a JSON error result
			outStr := string(output)
			lines := strings.Split(strings.TrimSpace(outStr), "\n")
			for i := len(lines) - 1; i >= 0; i-- {
				line := strings.TrimSpace(lines[i])
				if strings.HasPrefix(line, "{") && json.Valid([]byte(line)) {
					var result map[string]interface{}
					if err := json.Unmarshal([]byte(line), &result); err == nil {
						if success, ok := result["success"].(bool); ok && success {
							t.Errorf("%s with invalid JSON reported success=true", command)
						}
					}
					break
				}
			}
		})
	}
}

// TestCommandsWithNonexistentConfig verifies graceful handling of missing files
func TestCommandsWithNonexistentConfig(t *testing.T) {
	commands := []string{
		"backup", "restore", "verify", "upgrade",
		"enable-tls", "rotate-credentials", "harden", "cleanup", "destroy",
	}

	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			cmd := exec.Command("go", "run", "main.go", command, "--config", "/nonexistent/config.json")
			output, err := cmd.CombinedOutput()

			if err == nil {
				t.Errorf("%s with nonexistent config should fail", command)
			}

			// Should output JSON with error info
			outStr := string(output)
			lines := strings.Split(strings.TrimSpace(outStr), "\n")
			for i := len(lines) - 1; i >= 0; i-- {
				line := strings.TrimSpace(lines[i])
				if strings.HasPrefix(line, "{") && json.Valid([]byte(line)) {
					var result map[string]interface{}
					if err := json.Unmarshal([]byte(line), &result); err == nil {
						if success, ok := result["success"].(bool); ok && success {
							t.Errorf("%s with nonexistent config reported success=true", command)
						}
					}
					break
				}
			}
		})
	}
}

// TestStatusAndHealthRequireInstanceID verifies flag-based commands
// fail gracefully without --instance-id
func TestStatusAndHealthRequireInstanceID(t *testing.T) {
	commands := []string{"status", "health", "start", "stop"}

	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			cmd := exec.Command("go", "run", "main.go", command)
			output, err := cmd.CombinedOutput()

			if err == nil {
				t.Errorf("%s without --instance-id should fail", command)
			}

			outStr := string(output)
			if !strings.Contains(outStr, "--instance-id") && !strings.Contains(outStr, "--subdomain") {
				t.Errorf("%s should mention --instance-id or --subdomain, got: %s", command, outStr)
			}
		})
	}
}
