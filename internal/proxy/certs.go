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
	"sync/atomic"
	"time"
)

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

	// routes is the current routing snapshot, used to resolve aliases to
	// canonical domains and to restrict disk lookups to known domains.
	routes atomic.Pointer[Config]
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

// SetRouteTable updates the routing snapshot used for alias resolution and
// known-host checks. Proxy.UpdateConfig calls this automatically.
func (cm *CertManager) SetRouteTable(config *Config) {
	cm.routes.Store(config)
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

// validCertHostname reports whether name is a plausible DNS hostname. The SNI
// value is attacker-controlled and used to build certificate file paths, so
// anything that could escape the certificate directory must be rejected.
func validCertHostname(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' {
				return false
			}
		}
	}
	return true
}

// GetCertificate implements the tls.Config.GetCertificate callback.
// It returns the certificate for the given SNI hostname.
func (cm *CertManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	serverName := strings.ToLower(hello.ServerName)
	if !validCertHostname(serverName) {
		// No SNI (scanners/bots) or a malformed name: return the default
		// self-signed cert. The request is rejected at the HTTP handler level.
		return cm.defaultCert, nil
	}

	routes := cm.routes.Load()
	known := routes == nil || routes.IsKnownHost(serverName)
	if !known {
		return cm.defaultCert, nil
	}

	if cert, ok := cm.getCachedCertificate(serverName); ok {
		return cert, nil
	}

	// Only touch disk for domains we actually route, so scanner traffic with
	// random SNI values stays away from the filesystem. A nil route table
	// (not set yet) is treated as permissive.
	if cert, err := cm.loadAndCacheCertificate(serverName); err == nil {
		return cert, nil
	}

	// Aliases are covered by their canonical domain's certificate.
	if routes != nil {
		if canonical, ok := routes.ResolveCanonical(serverName); ok && canonical != serverName {
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

	// Return default cert for unknown domains - request will be rejected at HTTP layer
	return cm.defaultCert, nil
}

// ReloadCertificates reloads all certificates from disk.
func (cm *CertManager) ReloadCertificates() error {
	cm.logger.Info("Reloading certificates")
	return cm.loadAllCertificates()
}

// CertCount returns the number of certificates currently cached.
func (cm *CertManager) CertCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.certs)
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
