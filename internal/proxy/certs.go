package proxy

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// CertManager manages TLS certificates for the proxy.
// It loads certificates from disk and supports hot-reloading.
type CertManager struct {
	certDir string
	logger  *slog.Logger

	mu    sync.RWMutex
	certs map[string]*tls.Certificate // domain -> certificate

	watcher  *fsnotify.Watcher
	stopChan chan struct{}
}

// NewCertManager creates a new certificate manager.
func NewCertManager(certDir string, logger *slog.Logger) (*CertManager, error) {
	cm := &CertManager{
		certDir:  certDir,
		logger:   logger,
		certs:    make(map[string]*tls.Certificate),
		stopChan: make(chan struct{}),
	}

	// Initial load of certificates
	if err := cm.loadAllCertificates(); err != nil {
		return nil, fmt.Errorf("failed to load certificates: %w", err)
	}

	return cm, nil
}

// StartWatching starts watching the certificate directory for changes.
func (cm *CertManager) StartWatching() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	cm.watcher = watcher

	if err := watcher.Add(cm.certDir); err != nil {
		watcher.Close()
		return fmt.Errorf("failed to watch certificate directory: %w", err)
	}

	go cm.watchLoop()

	cm.logger.Info("Certificate watcher started", "dir", cm.certDir)
	return nil
}

// Stop stops the certificate manager and its file watcher.
func (cm *CertManager) Stop() {
	close(cm.stopChan)
	if cm.watcher != nil {
		cm.watcher.Close()
	}
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

// watchLoop watches for file system changes in the certificate directory.
func (cm *CertManager) watchLoop() {
	for {
		select {
		case <-cm.stopChan:
			return

		case event, ok := <-cm.watcher.Events:
			if !ok {
				return
			}

			// Only handle .pem files
			if !strings.HasSuffix(event.Name, ".pem") {
				continue
			}

			// Handle create, write, and remove events
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove) != 0 {
				cm.logger.Debug("Certificate file changed",
					"file", event.Name,
					"op", event.Op.String())

				// Reload all certificates on any change
				// This is simpler than tracking individual changes
				if err := cm.loadAllCertificates(); err != nil {
					cm.logger.Error("Failed to reload certificates", "error", err)
				}
			}

		case err, ok := <-cm.watcher.Errors:
			if !ok {
				return
			}
			cm.logger.Error("Certificate watcher error", "error", err)
		}
	}
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
