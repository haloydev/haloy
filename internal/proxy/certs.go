package proxy

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// CertManager manages TLS certificates for the proxy.
// It loads certificates from disk and reloads when explicitly told to via ReloadCertificates().
type CertManager struct {
	certDir string
	logger  *slog.Logger

	mu    sync.RWMutex
	certs map[string]*tls.Certificate // domain -> certificate
}

// NewCertManager creates a new certificate manager.
func NewCertManager(certDir string, logger *slog.Logger) (*CertManager, error) {
	cm := &CertManager{
		certDir: certDir,
		logger:  logger,
		certs:   make(map[string]*tls.Certificate),
	}

	// Initial load of certificates
	if err := cm.loadAllCertificates(); err != nil {
		return nil, fmt.Errorf("failed to load certificates: %w", err)
	}

	return cm, nil
}

// Stop stops the certificate manager (no-op, retained for interface compatibility).
func (cm *CertManager) Stop() {
	// No-op: fsnotify watcher has been removed.
	// Certificate reloading is now handled explicitly via ReloadCertificates().
}

// GetCertificate implements the tls.Config.GetCertificate callback.
// It returns the certificate for the given SNI hostname.
func (cm *CertManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	serverName := strings.ToLower(hello.ServerName)

	cm.mu.RLock()
	cert, ok := cm.certs[serverName]
	cm.mu.RUnlock()

	if ok {
		return cert, nil
	}

	// Try to load from disk (for user-supplied certs that may not be in cache)
	cert, err := cm.loadCertificate(serverName)
	if err == nil {
		cm.mu.Lock()
		cm.certs[serverName] = cert
		cm.mu.Unlock()
		return cert, nil
	}

	return nil, fmt.Errorf("no certificate found for %s", serverName)
}

// ReloadCertificates reloads all certificates from disk.
func (cm *CertManager) ReloadCertificates() error {
	cm.logger.Info("Reloading certificates")
	return cm.loadAllCertificates()
}

// loadAllCertificates loads all .pem files from the certificate directory.
func (cm *CertManager) loadAllCertificates() error {
	entries, err := os.ReadDir(cm.certDir)
	if err != nil {
		if os.IsNotExist(err) {
			cm.logger.Warn("Certificate directory does not exist", "dir", cm.certDir)
			return nil
		}
		return fmt.Errorf("failed to read certificate directory: %w", err)
	}

	newCerts := make(map[string]*tls.Certificate)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".pem") {
			continue
		}

		// Extract domain from filename (e.g., "example.com.pem" -> "example.com")
		domain := strings.TrimSuffix(name, ".pem")
		domain = strings.ToLower(domain)

		cert, err := cm.loadCertificate(domain)
		if err != nil {
			cm.logger.Warn("Failed to load certificate",
				"domain", domain,
				"error", err)
			continue
		}

		newCerts[domain] = cert
		cm.logger.Debug("Loaded certificate", "domain", domain)
	}

	cm.mu.Lock()
	cm.certs = newCerts
	cm.mu.Unlock()

	cm.logger.Info("Certificates loaded", "count", len(newCerts))
	return nil
}

// loadCertificate loads a certificate for a specific domain.
func (cm *CertManager) loadCertificate(domain string) (*tls.Certificate, error) {
	certPath := filepath.Join(cm.certDir, domain+".pem")

	// The .pem file contains both the private key and certificate (combined format)
	certData, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate file: %w", err)
	}

	cert, err := tls.X509KeyPair(certData, certData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return &cert, nil
}

// HasCertificate checks if a certificate exists for the given domain.
func (cm *CertManager) HasCertificate(domain string) bool {
	domain = strings.ToLower(domain)

	cm.mu.RLock()
	_, ok := cm.certs[domain]
	cm.mu.RUnlock()

	if ok {
		return true
	}

	// Check if file exists on disk
	certPath := filepath.Join(cm.certDir, domain+".pem")
	_, err := os.Stat(certPath)
	return err == nil
}

// CertificateCount returns the number of loaded certificates.
func (cm *CertManager) CertificateCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.certs)
}
