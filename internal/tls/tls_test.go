package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteCertificate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tls-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	domain := "test.example.com"
	certPEM := "-----BEGIN CERTIFICATE-----\ntest cert data\n-----END CERTIFICATE-----"
	keyPEM := "-----BEGIN RSA PRIVATE KEY-----\ntest key data\n-----END RSA PRIVATE KEY-----"

	err = WriteCertificate(domain, tmpDir, certPEM, keyPEM)
	if err != nil {
		t.Fatalf("WriteCertificate failed: %v", err)
	}

	// Verify files were created
	certPath := filepath.Join(tmpDir, "live", domain, "fullchain.pem")
	keyPath := filepath.Join(tmpDir, "live", domain, "privkey.pem")

	certData, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("Failed to read cert file: %v", err)
	}
	if string(certData) != certPEM {
		t.Errorf("Cert content mismatch: got %q, want %q", string(certData), certPEM)
	}

	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("Failed to read key file: %v", err)
	}
	if string(keyData) != keyPEM {
		t.Errorf("Key content mismatch: got %q, want %q", string(keyData), keyPEM)
	}

	// Verify file permissions are 0600
	info, _ := os.Stat(certPath)
	if info.Mode().Perm() != 0600 {
		t.Errorf("Cert file permissions = %o, want 0600", info.Mode().Perm())
	}

	info, _ = os.Stat(keyPath)
	if info.Mode().Perm() != 0600 {
		t.Errorf("Key file permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestWriteCertificateIdempotent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tls-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	domain := "test.example.com"
	certPEM := "cert1"
	keyPEM := "key1"

	// Write once
	err = WriteCertificate(domain, tmpDir, certPEM, keyPEM)
	if err != nil {
		t.Fatalf("First WriteCertificate failed: %v", err)
	}

	// Write again with different content (overwrite)
	certPEM2 := "cert2"
	keyPEM2 := "key2"
	err = WriteCertificate(domain, tmpDir, certPEM2, keyPEM2)
	if err != nil {
		t.Fatalf("Second WriteCertificate failed: %v", err)
	}

	// Verify latest content
	certPath := filepath.Join(tmpDir, "live", domain, "fullchain.pem")
	certData, _ := os.ReadFile(certPath)
	if string(certData) != certPEM2 {
		t.Errorf("Expected updated cert content")
	}
}

func TestIsProvisioned(t *testing.T) {
	if IsProvisioned("nonexistent.example.com") {
		t.Error("IsProvisioned should return false for non-existent domain")
	}
}

func TestIsProvisionedInDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tls-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	domain := "test.example.com"

	if IsProvisionedInDir(domain, tmpDir) {
		t.Error("IsProvisionedInDir should return false for non-existent cert")
	}

	// Write certs
	err = WriteCertificate(domain, tmpDir, "cert", "key")
	if err != nil {
		t.Fatalf("WriteCertificate failed: %v", err)
	}

	if !IsProvisionedInDir(domain, tmpDir) {
		t.Error("IsProvisionedInDir should return true after writing certs")
	}
}

func TestGetCertificateInfo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tls-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	domain := "test.example.com"
	cert := createTestCertificate(t, domain)

	err = WriteCertificate(domain, tmpDir, string(cert), "dummy-key")
	if err != nil {
		t.Fatalf("WriteCertificate failed: %v", err)
	}

	info, err := GetCertificateInfo(domain, tmpDir)
	if err != nil {
		t.Fatalf("GetCertificateInfo failed: %v", err)
	}

	if info.Domain != domain {
		t.Errorf("Domain = %s, want %s", info.Domain, domain)
	}

	if info.DaysLeft < 0 {
		t.Errorf("DaysLeft should be positive, got %d", info.DaysLeft)
	}

	if info.ExpiryDate.IsZero() {
		t.Error("ExpiryDate should not be zero")
	}
}

func TestGetCertificateInfoNonExistent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tls-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	_, err = GetCertificateInfo("nonexistent.example.com", tmpDir)
	if err == nil {
		t.Error("GetCertificateInfo should return error for non-existent cert")
	}
}

func TestCertificatePath(t *testing.T) {
	domain := "test.example.com"
	certDir := "/etc/letsencrypt"

	expected := "/etc/letsencrypt/live/test.example.com"
	got := certificatePath(domain, certDir)

	if got != expected {
		t.Errorf("certificatePath = %s, want %s", got, expected)
	}
}

// createTestCertificate creates a self-signed certificate for testing
func createTestCertificate(t *testing.T, domain string) []byte {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(90 * 24 * time.Hour)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: domain,
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,
		KeyUsage:  x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	return certPEM
}
