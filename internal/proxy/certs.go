package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DomainResolver resolves alias domains to canonical domains.
type DomainResolver interface {
	ResolveCanonical(domain string) (string, bool)
}

// CertManager manages TLS certificates for the proxy.
// It loads certificates from disk and reloads when explicitly told to via ReloadCertificates().
type CertManager struct {
	certDir string
	logger  *slog.Logger

	mu    sync.RWMutex
	certs map[string]*tls.Certificate // domain -> certificate

	// defaultCert is a self-signed certificate returned for connections without SNI.
	// This prevents TLS handshake errors from being logged for scanner/bot traffic.
	defaultCert *tls.Certificate

	resolverMu sync.RWMutex
	resolver   DomainResolver
}

// NewCertManager creates a new certificate manager.
func NewCertManager(certDir string, logger *slog.Logger) (*CertManager, error) {
	cm := &CertManager{
		certDir: certDir,
		logger:  logger,
		certs:   make(map[string]*tls.Certificate),
	}

	// Generate default self-signed certificate for connections without SNI
	defaultCert, err := generateSelfSignedCert()
	if err != nil {
		return nil, fmt.Errorf("failed to generate default certificate: %w", err)
	}
	cm.defaultCert = defaultCert

	// Initial load of certificates
	if err := cm.loadAllCertificates(); err != nil {
		return nil, fmt.Errorf("failed to load certificates: %w", err)
	}

	return cm, nil
}

// generateSelfSignedCert creates a self-signed certificate for use when no SNI is provided.
func generateSelfSignedCert() (*tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Haloy Default"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, err
	}

	cert := &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
	}

	return cert, nil
}

// Stop stops the certificate manager (no-op, retained for interface compatibility).
func (cm *CertManager) Stop() {
	// No-op: fsnotify watcher has been removed.
	// Certificate reloading is now handled explicitly via ReloadCertificates().
}

// SetDomainResolver sets the resolver used for alias lookups.
func (cm *CertManager) SetDomainResolver(resolver DomainResolver) {
	cm.resolverMu.Lock()
	cm.resolver = resolver
	cm.resolverMu.Unlock()
}

func (cm *CertManager) resolveCanonical(domain string) (string, bool) {
	cm.resolverMu.RLock()
	resolver := cm.resolver
	cm.resolverMu.RUnlock()

	if resolver == nil {
		return "", false
	}

	return resolver.ResolveCanonical(domain)
}

func (cm *CertManager) getCachedCertificate(domain string) (*tls.Certificate, bool) {
	cm.mu.RLock()
	cert, ok := cm.certs[domain]
	cm.mu.RUnlock()
	return cert, ok
}

func (cm *CertManager) loadAndCacheCertificate(domain string) (*tls.Certificate, error) {
	cert, err := cm.loadCertificate(domain)
	if err != nil {
		return nil, err
	}

	cm.mu.Lock()
	cm.certs[domain] = cert
	cm.mu.Unlock()

	return cert, nil
}

// wildcardDomain returns a one-level wildcard domain for the provided hostname.
func wildcardDomain(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) < 3 {
		return ""
	}
	return "*." + strings.Join(parts[1:], ".")
}

// GetCertificate implements the tls.Config.GetCertificate callback.
// It returns the certificate for the given SNI hostname.
func (cm *CertManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	serverName := strings.ToLower(hello.ServerName)
	if serverName == "" {
		// Return default self-signed cert for connections without SNI (scanners/bots).
		// The request will be rejected with 404 at the HTTP handler level.
		return cm.defaultCert, nil
	}

	if cert, ok := cm.getCachedCertificate(serverName); ok {
		return cert, nil
	}

	if cert, err := cm.loadAndCacheCertificate(serverName); err == nil {
		return cert, nil
	}

	if canonical, ok := cm.resolveCanonical(serverName); ok {
		canonical = strings.ToLower(canonical)
		if canonical != "" && canonical != serverName {
			if cert, ok := cm.getCachedCertificate(canonical); ok {
				return cert, nil
			}
			if cert, err := cm.loadAndCacheCertificate(canonical); err == nil {
				return cert, nil
			}
		}
	}

	if wildcard := wildcardDomain(serverName); wildcard != "" {
		if cert, ok := cm.getCachedCertificate(wildcard); ok {
			return cert, nil
		}
		if cert, err := cm.loadAndCacheCertificate(wildcard); err == nil {
			return cert, nil
		}
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
