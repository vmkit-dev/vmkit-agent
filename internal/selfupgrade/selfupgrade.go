// Package selfupgrade replaces the running vmkit-agent binary with a freshly
// downloaded one and restarts the systemd service. It backs the vm.upgrade
// RPC (vk-phz): VMs provisioned before a given release run an old agent that
// lacks newer handlers, and this is the path that brings them current without
// reprovisioning.
package selfupgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultService is the systemd unit restarted after a successful swap.
const DefaultService = "vmkit-agent"

const (
	restartDelay    = 3 * time.Second
	downloadTimeout = 5 * time.Minute
)

// Config is the vm.upgrade RPC payload.
type Config struct {
	// BinaryURL is where to fetch the new agent binary. A literal "{arch}"
	// token is replaced with the running GOARCH (amd64 / arm64) so the
	// backend can hand out one templated URL for both Hetzner VM shapes.
	BinaryURL string `json:"binary_url"`
	// SHA256, when non-empty, must match the downloaded binary (hex digest).
	SHA256 string `json:"sha256,omitempty"`
	// Version is an informational label for the target release.
	Version string `json:"version,omitempty"`
	// Service overrides the systemd unit to restart; defaults to DefaultService.
	Service string `json:"service,omitempty"`
}

// Result is the vm.upgrade RPC response.
type Result struct {
	Success    bool   `json:"success"`
	OldVersion string `json:"old_version,omitempty"`
	NewVersion string `json:"new_version,omitempty"`
	BinaryURL  string `json:"binary_url,omitempty"`
	Message    string `json:"message,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Options carries dependencies that tests override; the zero value is the
// production configuration.
type Options struct {
	// CurrentVersion is reported back as Result.OldVersion.
	CurrentVersion string
	// ExecutablePath overrides the running-binary path. Default: os.Executable().
	ExecutablePath string
	// HTTPClient downloads the binary. Default: a client with downloadTimeout.
	HTTPClient *http.Client
	// Restart is invoked after a successful swap. Default: a goroutine that
	// waits restartDelay (so the RPC response flushes first) then runs
	// `systemctl restart <service>`. Tests inject a recorder.
	Restart func(service string)
}

// Run downloads the binary, verifies it, atomically replaces the running
// executable, and triggers a service restart. Every failure path returns a
// Result with Success=false and Error set rather than panicking, so the
// daemon can surface the message over the WS uplink.
func Run(cfg Config, opts Options) Result {
	res := Result{OldVersion: opts.CurrentVersion}

	url := strings.ReplaceAll(cfg.BinaryURL, "{arch}", runtime.GOARCH)
	res.BinaryURL = url
	if url == "" {
		return fail(res, "binary_url is required")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fail(res, fmt.Sprintf("binary_url must be an http(s) URL: %q", url))
	}

	execPath := opts.ExecutablePath
	if execPath == "" {
		p, err := os.Executable()
		if err != nil {
			return fail(res, fmt.Sprintf("cannot resolve running binary path: %v", err))
		}
		// Resolve symlinks so the rename targets the real file, not a link.
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			p = resolved
		}
		execPath = p
	}

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: downloadTimeout}
	}

	// Download to a sibling temp file so the final swap is an atomic rename
	// on the same filesystem.
	tmpPath := execPath + ".new"
	if err := download(client, url, tmpPath, cfg.SHA256); err != nil {
		os.Remove(tmpPath)
		return fail(res, err.Error())
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return fail(res, fmt.Sprintf("chmod new binary: %v", err))
	}
	// Atomic replace. Linux keeps the old inode open for the live process,
	// so overwriting our own executable is safe — the restart picks up the
	// new file.
	if err := os.Rename(tmpPath, execPath); err != nil {
		os.Remove(tmpPath)
		return fail(res, fmt.Sprintf("replace running binary: %v", err))
	}

	service := cfg.Service
	if service == "" {
		service = DefaultService
	}
	restart := opts.Restart
	if restart == nil {
		restart = scheduleSystemctlRestart
	}
	restart(service)

	res.Success = true
	res.NewVersion = cfg.Version
	if res.NewVersion == "" {
		res.NewVersion = "latest"
	}
	res.Message = fmt.Sprintf("binary replaced at %s; restarting %s", execPath, service)
	return res
}

// download fetches url into dst and, when wantSHA is set, fails on a digest
// mismatch.
func download(client *http.Client, url, dst, wantSHA string) error {
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("create temp file %s: %w", dst, err)
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("write binary: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close temp file: %w", closeErr)
	}
	if n == 0 {
		return fmt.Errorf("downloaded binary is empty")
	}
	if wantSHA != "" {
		got := hex.EncodeToString(h.Sum(nil))
		if !strings.EqualFold(got, wantSHA) {
			return fmt.Errorf("sha256 mismatch: want %s, got %s", wantSHA, got)
		}
	}
	return nil
}

// scheduleSystemctlRestart restarts the service after restartDelay. The delay
// lets the daemon flush the vm.upgrade RPC response before systemd kills this
// process. Start() (not Run()) is used because the restart terminates us.
func scheduleSystemctlRestart(service string) {
	go func() {
		time.Sleep(restartDelay)
		_ = exec.Command("systemctl", "restart", service).Start()
	}()
}

func fail(res Result, msg string) Result {
	res.Success = false
	res.Error = msg
	return res
}
