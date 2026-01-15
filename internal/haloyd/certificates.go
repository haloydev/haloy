package haloyd

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/logging"
)

const (
	refreshDebounceKey   = "certificate_refresh"
	refreshDebounceDelay = 5 * time.Second
	accountsDirName      = "accounts"
	combinedCertExt      = ".pem"
	keyCertExt           = ".key"
)

type CertificatesUser struct {
	Email        string
	Registration *registration.Resource
	privateKey   crypto.PrivateKey
}

func (u *CertificatesUser) GetEmail() string {
	return u.Email
}

func (u *CertificatesUser) GetRegistration() *registration.Resource {
	return u.Registration
}

func (u *CertificatesUser) GetPrivateKey() crypto.PrivateKey {
	return u.privateKey
}

type CertificatesClientManager struct {
	tlsStaging         bool
	keyManager         *CertificatesKeyManager
	clients            map[string]*lego.Client
	clientsMutex       sync.RWMutex
	sharedHTTPProvider *http01.ProviderServer
}

func NewCertificatesClientManager(
	certDir string,
	tlsStaging bool,
	httpProviderPort string,
) (*CertificatesClientManager, error) {
	keyDir := filepath.Join(certDir, accountsDirName)

	if err := os.MkdirAll(keyDir, constants.ModeDirPrivate); err != nil {
		return nil, fmt.Errorf("failed to create key directory '%s': %w", keyDir, err)
	}
	keyManager, err := NewCertificatesKeyManager(keyDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create key manager: %w", err)
	}

	httpProvider := http01.NewProviderServer("", httpProviderPort)

	return &CertificatesClientManager{
		tlsStaging:         tlsStaging,
		clients:            make(map[string]*lego.Client),
		keyManager:         keyManager,
		sharedHTTPProvider: httpProvider,
	}, nil
}

func (cm *CertificatesClientManager) LoadOrRegisterClient(email string) (*lego.Client, error) {
	cm.clientsMutex.RLock()
	client, ok := cm.clients[email]
	cm.clientsMutex.RUnlock()

	if ok {
		return client, nil
	}

	cm.clientsMutex.Lock()
	defer cm.clientsMutex.Unlock()

	// Check again in case another goroutine created it while we were waiting. Just to be safe.
	if client, ok := cm.clients[email]; ok {
		return client, nil
	}

	privateKey, err := cm.keyManager.LoadOrCreateKey(email)
	if err != nil {
		return nil, fmt.Errorf("failed to load/create user key: %w", err)
	}

	user := &CertificatesUser{
		Email:      email,
		privateKey: privateKey,
	}

	legoConfig := lego.NewConfig(user)
	if cm.tlsStaging {
		legoConfig.CADirURL = lego.LEDirectoryStaging
	} else {
		legoConfig.CADirURL = lego.LEDirectoryProduction
	}

	client, err = lego.NewClient(legoConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create lego client: %w", err)
	}

	// Configure HTTP challenge provider using a server that listens on port 8080
	// The proxy forwards /.well-known/acme-challenge/* requests to this server
	err = client.Challenge.SetHTTP01Provider(cm.sharedHTTPProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to set HTTP challenge provider: %w", err)
	}

	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return nil, fmt.Errorf("failed to register user: %w", err)
	}
	user.Registration = reg

	cm.clients[email] = client

	return client, nil
}

type CertificatesManagerConfig struct {
	CertDir          string
	HTTPProviderPort string
	TlsStaging       bool
}

type CertificatesDomain struct {
	Canonical string
	Aliases   []string
	Email     string
}

func (cm *CertificatesDomain) Validate() error {
	if cm.Canonical == "" {
		return fmt.Errorf("canonical domain cannot be empty")
	}

	if err := helpers.IsValidDomain(cm.Canonical); err != nil {
		return fmt.Errorf("invalid canonical domain '%s': %w", cm.Canonical, err)
	}

	if cm.Email == "" {
		return fmt.Errorf("email cannot be empty")
	}
	if !helpers.IsValidEmail(cm.Email) {
		return fmt.Errorf("invalid email format: %s", cm.Email)
	}

	for _, alias := range cm.Aliases {
		if alias == "" {
			return fmt.Errorf("alias cannot be empty")
		}
		if err := helpers.IsValidDomain(alias); err != nil {
			return fmt.Errorf("invalid alias '%s': %w", alias, err)
		}
	}
	return nil
}

type CertificatesManager struct {
	config        CertificatesManagerConfig
	checkMutex    sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	clientManager *CertificatesClientManager
	updateSignal  chan<- string // signal successful updates
	debouncer     *helpers.Debouncer
}

func NewCertificatesManager(config CertificatesManagerConfig, updateSignal chan<- string) (*CertificatesManager, error) {
	if err := os.MkdirAll(config.CertDir, constants.ModeDirPrivate); err != nil {
		return nil, fmt.Errorf("failed to create certificate directory: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	clientManager, err := NewCertificatesClientManager(config.CertDir, config.TlsStaging, config.HTTPProviderPort)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create client manager: %w", err)
	}

	m := &CertificatesManager{
		config:        config,
		ctx:           ctx,
		cancel:        cancel,
		clientManager: clientManager,
		updateSignal:  updateSignal,
		debouncer:     helpers.NewDebouncer(refreshDebounceDelay),
	}

	return m, nil
}

func (m *CertificatesManager) Stop() {
	m.cancel()
	m.debouncer.Stop() // Stop the debouncer to clean up any pending timers
}

func (cm *CertificatesManager) RefreshSync(logger *slog.Logger, domains []CertificatesDomain) error {
	_, err := cm.checkRenewals(logger, domains)
	if err != nil {
		return err
	}
	return nil
}

// Refresh is used for periodic refreshes of certificates.
func (cm *CertificatesManager) Refresh(logger *slog.Logger, domains []CertificatesDomain) {
	logger.Debug("Refresh requested for certificate manager, using debouncer.")

	refreshAction := func() {
		renewedDomains, err := cm.checkRenewals(logger, domains)
		if err != nil {
			logger.Error("Certificate refresh failed", "error", err)
			return
		}
		// Signal the update channel to reload certificates if any were renewed.
		if len(renewedDomains) > 0 {
			if cm.updateSignal != nil {
				cm.updateSignal <- "certificates_renewed"
			}
		}
	}

	cm.debouncer.Debounce(refreshDebounceKey, refreshAction)
}

func (cm *CertificatesManager) checkRenewals(logger *slog.Logger, domains []CertificatesDomain) (renewedDomains []CertificatesDomain, err error) {
	cm.checkMutex.Lock()
	defer func() {
		cm.checkMutex.Unlock()
	}()

	if len(domains) == 0 {
		return renewedDomains, nil
	}

	uniqueDomains := deduplicateDomains(domains)
	if len(uniqueDomains) != len(domains) {
		logger.Debug("Deduplicated certificate domains",
			"original", len(domains), "unique", len(uniqueDomains))
	}

	// Build the current desired state - only one entry per canonical domain
	currentState := make(map[string]CertificatesDomain)
	for _, domain := range uniqueDomains {
		if existing, exists := currentState[domain.Canonical]; exists {
			// Prefer the configuration with more aliases
			if len(domain.Aliases) > len(existing.Aliases) {
				logger.Debug("Using domain configuration with more aliases",
					"domain", domain.Canonical,
					"newAliases", domain.Aliases,
					"oldAliases", existing.Aliases)
				currentState[domain.Canonical] = domain
			} else {
				logger.Debug("Keeping existing domain configuration",
					"domain", domain.Canonical,
					"aliases", existing.Aliases)
			}
		} else {
			currentState[domain.Canonical] = domain
		}
	}

	for canonical, domain := range currentState {
		configChanged, err := cm.hasConfigurationChanged(logger, domain)
		if err != nil {
			logger.Error("Failed to check configuration", "domain", canonical, "error", err)
			continue
		}

		// Check if certificate needs renewal due to expiry
		needsRenewal, err := cm.needsRenewalDueToExpiry(logger, domain)
		if err != nil {
			logger.Error("Failed to check expiry", "domain", canonical, "error", err)
			// Treat error as needing renewal to be safe
			needsRenewal = true
		}

		// If configuration changed, clean up all related certificates first
		if configChanged {
			logger.Debug("Configuration changed, cleaning up existing certificates", "domain", canonical)
			if err := cm.cleanupDomainCertificates(canonical); err != nil {
				logger.Warn("Failed to cleanup certificates", "domain", canonical, "error", err)
				// Continue anyway, might still work
			}
		}

		// Obtain certificate if needed
		allDomains := []string{domain.Canonical}
		allDomains = append(allDomains, domain.Aliases...)
		if configChanged || needsRenewal {
			requestMessage := "Requesting new certificate"
			if len(allDomains) > 1 {
				requestMessage = "Requesting new certificates"
			}
			logger.Info(requestMessage,
				logging.AttrDomains, allDomains,
				"domain", canonical,
				"aliases", domain.Aliases)
			obtainedDomain, err := cm.obtainCertificate(domain)
			if err != nil {
				return renewedDomains, err
			}

			renewedDomains = append(renewedDomains, obtainedDomain)
			logger.Info("Obtained new certificate",
				logging.AttrDomains, allDomains,
				"domain", canonical,
				"aliases", domain.Aliases)
		} else {
			logger.Info("Certificate is valid",
				logging.AttrDomains, allDomains,
				"domain", canonical,
				"aliases", domain.Aliases)
		}
	}

	return renewedDomains, nil
}

// hasConfigurationChanged checks if the domain configuration has changed compared to existing certificate
func (cm *CertificatesManager) hasConfigurationChanged(logger *slog.Logger, domain CertificatesDomain) (bool, error) {
	combinedCertKeyPath := filepath.Join(cm.config.CertDir, domain.Canonical+combinedCertExt)

	// If certificate files don't exist, configuration has "changed" (need to create)
	if _, err := os.Stat(combinedCertKeyPath); os.IsNotExist(err) {
		logger.Debug("Certificate files don't exist, needs creation", "domain", domain.Canonical)
		return true, nil
	}

	certData, err := os.ReadFile(combinedCertKeyPath)
	if err != nil {
		logger.Debug("Cannot read certificate file, treating as changed", "domain", domain.Canonical)
		return true, nil
	}

	parsedCert, err := parseCertificate(certData)
	if err != nil {
		logger.Debug("Cannot parse certificate, treating as changed", "domain", domain.Canonical)
		return true, nil
	}

	requiredDomains := []string{domain.Canonical}
	requiredDomains = append(requiredDomains, domain.Aliases...)
	sort.Strings(requiredDomains)

	existingDomains := parsedCert.DNSNames
	sort.Strings(existingDomains)

	return !reflect.DeepEqual(requiredDomains, existingDomains), nil
}

// needsRenewalDueToExpiry checks if certificate needs renewal due to expiry
func (cm *CertificatesManager) needsRenewalDueToExpiry(logger *slog.Logger, domain CertificatesDomain) (bool, error) {
	certFilePath := filepath.Join(cm.config.CertDir, domain.Canonical+combinedCertExt)

	// If certificate doesn't exist, we need to obtain one
	certData, err := os.ReadFile(certFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil // File doesn't exist, need to obtain
		}
		return false, err
	}

	parsedCert, err := parseCertificate(certData)
	if err != nil {
		return true, nil
	}

	// Check if certificate expires within 30 days
	if time.Until(parsedCert.NotAfter) < 30*24*time.Hour {
		logger.Info("Certificate expires soon and needs renewal", "domain", domain.Canonical)
		return true, nil
	}

	return false, nil
}

// cleanupDomainCertificates removes all certificate files for a domain
func (cm *CertificatesManager) cleanupDomainCertificates(canonical string) error {
	combinedPath := filepath.Join(cm.config.CertDir, canonical+combinedCertExt)
	if err := os.Remove(combinedPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove combined certificate file %s: %w", combinedPath, err)
	}

	return nil
}

func (cm *CertificatesManager) validateDomain(domain string) error {
	// Check if domain resolves
	ips, err := net.LookupIP(domain)
	if err != nil {
		// Try to determine the specific issue
		errorMessage := cm.buildDomainErrorMessage(domain, err)
		return fmt.Errorf("\n\n%s", errorMessage)
	}

	// Additional check: ensure domain resolves to a reachable IP
	if len(ips) == 0 {
		return fmt.Errorf(`domain %s has no IP addresses assigned

Please add DNS records:
- A record: %s → YOUR_SERVER_IP
- Test with: dig A %s`, domain, domain, domain)
	}

	return nil
}

func (cm *CertificatesManager) buildDomainErrorMessage(domain string, originalErr error) string {
	errorStr := originalErr.Error()

	if strings.Contains(errorStr, "NXDOMAIN") || strings.Contains(errorStr, "no such host") {
		return fmt.Sprintf("Domain %s not found. Check if domain exists and DNS A record is configured.", domain)
	}

	if strings.Contains(errorStr, "timeout") {
		return fmt.Sprintf("DNS timeout for %s. Check network connectivity or try different DNS server.", domain)
	}

	return fmt.Sprintf("DNS resolution failed for %s. Verify domain exists and has proper DNS records.", domain)
}

func (m *CertificatesManager) obtainCertificate(managedDomain CertificatesDomain) (obtainedDomain CertificatesDomain, err error) {
	canonicalDomain := managedDomain.Canonical
	email := managedDomain.Email
	aliases := managedDomain.Aliases
	allDomains := append([]string{canonicalDomain}, aliases...)

	if err := m.validateDomain(canonicalDomain); err != nil {
		return obtainedDomain, fmt.Errorf("domain validation failed for %s: %w", canonicalDomain, err)
	}

	client, err := m.clientManager.LoadOrRegisterClient(email)
	if err != nil {
		return obtainedDomain, fmt.Errorf("failed to load or register ACME client for %s: %w", email, err)
	}

	request := certificate.ObtainRequest{
		Domains: allDomains, // Request cert for canonical + aliases
		Bundle:  true,       // Bundle intermediate certs
	}

	certificates, err := client.Certificate.Obtain(request)
	if err != nil {
		return obtainedDomain, fmt.Errorf("failed to obtain certificate for %s: %w", canonicalDomain, err)
	}
	err = m.saveCertificate(canonicalDomain, certificates)
	if err != nil {
		return obtainedDomain, fmt.Errorf("failed to save certificate for %s: %w", canonicalDomain, err)
	} else {
		obtainedDomain = CertificatesDomain{
			Canonical: canonicalDomain,
			Aliases:   aliases,
			Email:     email,
		}
	}

	return obtainedDomain, nil
}

func (m *CertificatesManager) saveCertificate(domain string, cert *certificate.Resource) error {
	combinedPath := filepath.Join(m.config.CertDir, domain+combinedCertExt)
	tmpPath := combinedPath + ".tmp"

	pemContent := bytes.Buffer{}

	pemContent.Write(cert.PrivateKey)
	if len(cert.PrivateKey) > 0 && cert.PrivateKey[len(cert.PrivateKey)-1] != '\n' {
		pemContent.WriteByte('\n')
	}

	pemContent.Write(cert.Certificate)
	if err := os.WriteFile(tmpPath, pemContent.Bytes(), constants.ModeFileSecret); err != nil {
		return fmt.Errorf("failed to save temporary combined certificate/key: %w", err)
	}

	// Atomic replace
	if err := os.Rename(tmpPath, combinedPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to atomically replace combined certificate/key: %w", err)
	}

	return nil
}

func (m *CertificatesManager) CleanupExpiredCertificates(logger *slog.Logger, domains []CertificatesDomain) {
	logger.Debug("Starting certificate cleanup check")

	files, err := os.ReadDir(m.config.CertDir)
	if err != nil {
		logger.Error("Failed to read certificates directory", "dir", m.config.CertDir, "error", err)
		return
	}

	deleted := 0

	managedDomainsMap := make(map[string]struct{}, len(domains))
	for _, domain := range domains { // Keys are canonical domains
		managedDomainsMap[domain.Canonical] = struct{}{}
	}

	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), combinedCertExt) {
			domain := strings.TrimSuffix(file.Name(), combinedCertExt)
			_, isManaged := managedDomainsMap[domain]
			combinedCertPath := filepath.Join(m.config.CertDir, file.Name())

			certData, err := os.ReadFile(combinedCertPath)
			if err != nil {
				if os.IsNotExist(err) && !isManaged {
					logger.Warn("Found orphaned PEM file for unmanaged domain (.crt missing). Deleting", "domain", domain)
					os.Remove(combinedCertPath)
					deleted++
				} else if !os.IsNotExist(err) {
					logger.Warn("Failed to read certificate file during cleanup", "file", combinedCertPath, "error", err)
				}
				continue
			}

			parsedCert, err := parseCertificate(certData)
			if err != nil {
				logger.Warn("Failed to parse certificate during cleanup", "file", combinedCertPath)
				continue
			}

			if time.Now().After(parsedCert.NotAfter) && !isManaged {
				logger.Debug("Deleting expired certificate files for unmanaged domain", "domain", domain)
				os.Remove(combinedCertPath)
				deleted++
			}
		}
	}

	logger.Debug("Certificate cleanup complete. Deleted expired/orphaned certificate sets for unmanaged domains")
}

// parseCertificate takes PEM encoded certificate data and returns the parsed x509.Certificate
func parseCertificate(certData []byte) (*x509.Certificate, error) {
	var block *pem.Block
	rest := certData
	for {
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("failed to parse certificate: %w", err)
			}
			return cert, nil
		}
	}
	return nil, fmt.Errorf("no CERTIFICATE PEM block found")
}

// CertificatesKeyManager handles private key operations for the ACME client
type CertificatesKeyManager struct {
	keyDir string
}

func NewCertificatesKeyManager(keyDir string) (*CertificatesKeyManager, error) {
	stat, err := os.Stat(keyDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("key directory '%s' does not exist; ensure init process has created it", keyDir)
		}
		return nil, fmt.Errorf("failed to stat key directory '%s': %w", keyDir, err)
	}
	if !stat.IsDir() {
		return nil, fmt.Errorf("key directory path '%s' is not a directory", keyDir)
	}

	return &CertificatesKeyManager{
		keyDir: keyDir,
	}, nil
}

func (km *CertificatesKeyManager) LoadOrCreateKey(email string) (crypto.PrivateKey, error) {
	// Sanitize email for filename
	filename := helpers.SanitizeString(email) + keyCertExt
	keyPath := filepath.Join(km.keyDir, filename)

	if _, err := os.Stat(keyPath); err == nil {
		return km.loadKey(keyPath)
	}

	return km.createKey(keyPath)
}

func (km *CertificatesKeyManager) loadKey(path string) (crypto.PrivateKey, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}

	keyBlock, _ := pem.Decode(keyBytes)
	if keyBlock == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	switch keyBlock.Type {
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(keyBlock.Bytes)
	default:
		return nil, fmt.Errorf("unsupported key type: %s", keyBlock.Type)
	}
}

func (km *CertificatesKeyManager) createKey(path string) (crypto.PrivateKey, error) {
	// Generate new ECDSA key (P-256 for good balance of security and performance)
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	pemBlock := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	}

	keyFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, constants.ModeFileSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to create key file: %w", err)
	}
	defer keyFile.Close()

	if err := pem.Encode(keyFile, pemBlock); err != nil {
		return nil, fmt.Errorf("failed to write key file: %w", err)
	}

	return privateKey, nil
}

func deduplicateDomains(domains []CertificatesDomain) []CertificatesDomain {
	seen := make(map[string]bool)
	var unique []CertificatesDomain

	for _, domain := range domains {
		if !seen[domain.Canonical] {
			seen[domain.Canonical] = true
			unique = append(unique, domain)
		}
	}

	return unique
}
