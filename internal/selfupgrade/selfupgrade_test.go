package selfupgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// newBinaryServer serves payload at any path, recording the last path hit.
func newBinaryServer(t *testing.T, payload []byte, status int) (*httptest.Server, *string) {
	t.Helper()
	var lastPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Write(payload)
	}))
	t.Cleanup(srv.Close)
	return srv, &lastPath
}

// stageOldBinary writes a placeholder "running binary" into a temp dir and
// returns its path.
func stageOldBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vmkit-agent")
	if err := os.WriteFile(path, []byte("OLD BINARY"), 0o755); err != nil {
		t.Fatalf("stage old binary: %v", err)
	}
	return path
}

func TestRunReplacesBinaryAndRestarts(t *testing.T) {
	payload := []byte("NEW BINARY CONTENT")
	srv, _ := newBinaryServer(t, payload, http.StatusOK)
	execPath := stageOldBinary(t)

	var restarted string
	res := Run(
		Config{BinaryURL: srv.URL + "/vmkit-agent-linux-amd64", Version: "v1.2.3"},
		Options{
			CurrentVersion: "v1.0.0",
			ExecutablePath: execPath,
			Restart:        func(svc string) { restarted = svc },
		},
	)

	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	got, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatalf("read swapped binary: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("binary not swapped: got %q", got)
	}
	if restarted != DefaultService {
		t.Errorf("restart service = %q, want %q", restarted, DefaultService)
	}
	if res.OldVersion != "v1.0.0" || res.NewVersion != "v1.2.3" {
		t.Errorf("versions: old=%q new=%q", res.OldVersion, res.NewVersion)
	}
	// The temp download file must not linger next to the binary.
	if _, err := os.Stat(execPath + ".new"); !os.IsNotExist(err) {
		t.Errorf("temp .new file was not cleaned up")
	}
}

func TestRunSubstitutesArch(t *testing.T) {
	srv, lastPath := newBinaryServer(t, []byte("bin"), http.StatusOK)
	execPath := stageOldBinary(t)

	res := Run(
		Config{BinaryURL: srv.URL + "/vmkit-agent-linux-{arch}"},
		Options{ExecutablePath: execPath, Restart: func(string) {}},
	)
	if !res.Success {
		t.Fatalf("expected success, got: %s", res.Error)
	}
	want := "/vmkit-agent-linux-" + runtime.GOARCH
	if *lastPath != want {
		t.Errorf("requested path = %q, want %q", *lastPath, want)
	}
	if !strings.HasSuffix(res.BinaryURL, want) {
		t.Errorf("Result.BinaryURL = %q, want suffix %q", res.BinaryURL, want)
	}
}

func TestRunVerifiesSHA256(t *testing.T) {
	payload := []byte("verified payload")
	sum := sha256.Sum256(payload)
	srv, _ := newBinaryServer(t, payload, http.StatusOK)

	t.Run("match", func(t *testing.T) {
		res := Run(
			Config{BinaryURL: srv.URL + "/b", SHA256: hex.EncodeToString(sum[:])},
			Options{ExecutablePath: stageOldBinary(t), Restart: func(string) {}},
		)
		if !res.Success {
			t.Errorf("expected success on matching digest, got: %s", res.Error)
		}
	})

	t.Run("mismatch", func(t *testing.T) {
		execPath := stageOldBinary(t)
		res := Run(
			Config{BinaryURL: srv.URL + "/b", SHA256: "deadbeef"},
			Options{ExecutablePath: execPath, Restart: func(string) {
				t.Error("restart must not run on a digest mismatch")
			}},
		)
		if res.Success {
			t.Error("expected failure on digest mismatch")
		}
		// The running binary must be left untouched.
		if got, _ := os.ReadFile(execPath); string(got) != "OLD BINARY" {
			t.Errorf("binary was swapped despite digest mismatch: %q", got)
		}
	})
}

func TestRunHTTPError(t *testing.T) {
	srv, _ := newBinaryServer(t, nil, http.StatusNotFound)
	res := Run(
		Config{BinaryURL: srv.URL + "/missing"},
		Options{ExecutablePath: stageOldBinary(t), Restart: func(string) {
			t.Error("restart must not run when the download fails")
		}},
	)
	if res.Success {
		t.Error("expected failure on HTTP 404")
	}
}

func TestRunRejectsBadURL(t *testing.T) {
	for _, url := range []string{"", "ftp://example.com/agent", "/local/path"} {
		res := Run(Config{BinaryURL: url}, Options{ExecutablePath: stageOldBinary(t)})
		if res.Success {
			t.Errorf("expected failure for url %q", url)
		}
	}
}

func TestRunCustomService(t *testing.T) {
	srv, _ := newBinaryServer(t, []byte("bin"), http.StatusOK)
	var restarted string
	res := Run(
		Config{BinaryURL: srv.URL + "/b", Service: "vmkit-agent-staging"},
		Options{ExecutablePath: stageOldBinary(t), Restart: func(s string) { restarted = s }},
	)
	if !res.Success {
		t.Fatalf("expected success, got: %s", res.Error)
	}
	if restarted != "vmkit-agent-staging" {
		t.Errorf("restart service = %q, want vmkit-agent-staging", restarted)
	}
}
