package tls

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// DefaultCertDir is the default directory for storing certificates
	DefaultCertDir = "/etc/letsencrypt"
	// RenewalThreshold is when to renew certs (30 days before expiry)
	RenewalThreshold = 30 * 24 * time.Hour
)

// CertificateInfo contains certificate details
type CertificateInfo struct {
	Domain       string    `json:"domain"`
	ExpiryDate   time.Time `json:"expiry_date"`
	DaysLeft     int       `json:"days_left"`
	NeedsRenewal bool      `json:"needs_renewal"`
	Issuer       string    `json:"issuer"`
}

// WriteCertificate writes cert and key PEM data to the standard cert directory.
// Creates /etc/letsencrypt/live/{domain}/fullchain.pem and privkey.pem with 0600 perms.
func WriteCertificate(domain, certDir, certPEM, keyPEM string) error {
	certPath := certificatePath(domain, certDir)
	if err := os.MkdirAll(certPath, 0755); err != nil {
		return fmt.Errorf("failed to create certificate directory %s: %w", certPath, err)
	}

	files := map[string]string{
		"fullchain.pem": certPEM,
		"privkey.pem":   keyPEM,
	}

	for filename, data := range files {
		path := filepath.Join(certPath, filename)
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			return fmt.Errorf("failed to write %s: %w", filename, err)
		}
	}

	return nil
}

// IsProvisioned checks if a TLS certificate exists for a domain
func IsProvisioned(domain string) bool {
	return IsProvisionedInDir(domain, DefaultCertDir)
}

// IsProvisionedInDir checks if a certificate exists in a specific directory
func IsProvisionedInDir(domain, certDir string) bool {
	certPath := certificatePath(domain, certDir)
	certFile := filepath.Join(certPath, "fullchain.pem")
	keyFile := filepath.Join(certPath, "privkey.pem")

	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		return false
	}
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		return false
	}

	return true
}

// GetCertificateInfo retrieves information about an existing certificate
func GetCertificateInfo(domain, certDir string) (*CertificateInfo, error) {
	certPath := certificatePath(domain, certDir)
	certFile := filepath.Join(certPath, "fullchain.pem")

	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	now := time.Now()
	daysLeft := int(cert.NotAfter.Sub(now).Hours() / 24)
	needsRenewal := cert.NotAfter.Sub(now) < RenewalThreshold

	info := &CertificateInfo{
		Domain:       domain,
		ExpiryDate:   cert.NotAfter,
		DaysLeft:     daysLeft,
		NeedsRenewal: needsRenewal,
		Issuer:       cert.Issuer.CommonName,
	}

	return info, nil
}

// certificatePath returns the path to certificate directory for a domain
func certificatePath(domain, certDir string) string {
	return filepath.Join(certDir, "live", domain)
}
