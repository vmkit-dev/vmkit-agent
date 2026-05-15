package nginx

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"os"
)

// HtpasswdPathFor returns the path nginx reads basic-auth credentials from
// for a given instance subdomain. One file per site, sibling of the main
// site .conf, locked down to root.
func HtpasswdPathFor(subdomain string) string {
	return fmt.Sprintf("%s/%s.htpasswd", SupabaseSitesDir, subdomain)
}

// WriteHtpasswd writes a single-user htpasswd file using nginx's built-in
// {SHA} format (SHA-1 + base64). We deliberately avoid bcrypt to keep the
// agent dependency-free; the protected password is a 192-bit random token
// (secrets.token_urlsafe(24) on the Python side) so unsalted SHA-1 is fine
// against the online-guessing threat we're defending against here.
//
// File is written 0644 because nginx worker processes run as www-data (or
// nginx, depending on distro) and need read access to verify supplied
// credentials. 0600 makes nginx return 500 on credential-bearing requests
// instead of 401 — the file contains a hash of a 192-bit token, not the
// password itself, so 0644 is consistent with standard apache htpasswd
// permissions and doesn't materially weaken the gate.
//
// Returns the path written.
func WriteHtpasswd(subdomain, username, password string) (string, error) {
	if username == "" || password == "" {
		return "", fmt.Errorf("username and password are both required")
	}

	if err := os.MkdirAll(SupabaseSitesDir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", SupabaseSitesDir, err)
	}

	sum := sha1.Sum([]byte(password))
	encoded := base64.StdEncoding.EncodeToString(sum[:])
	line := fmt.Sprintf("%s:{SHA}%s\n", username, encoded)

	path := HtpasswdPathFor(subdomain)
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	// os.WriteFile only applies the mode when it CREATES the file; if the
	// file already existed (e.g. an earlier agent version wrote 0600) the
	// old mode sticks. Chmod explicitly so an upgrade fixes the permissions
	// on the next run.
	if err := os.Chmod(path, 0o644); err != nil {
		return "", fmt.Errorf("chmod %s: %w", path, err)
	}
	return path, nil
}

// RemoveHtpasswd deletes the per-site htpasswd file. Idempotent.
func RemoveHtpasswd(subdomain string) error {
	path := HtpasswdPathFor(subdomain)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}
