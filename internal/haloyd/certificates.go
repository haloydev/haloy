package haloyd

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/logging"
	"golang.org/x/crypto/acme"
)

const (
	refreshDebounceKey   = "certificate_refresh"
	refreshDebounceDelay = 5 * time.Second
	accountsDirName      = "accounts"
	combinedCertExt      = ".pem"
	accountFileName      = "account.json"

	// ACME directory URLs
	letsEncryptProduction = "https://acme-v02.api.letsencrypt.org/directory"
	letsEncryptStaging    = "https://acme-staging-v02.api.letsencrypt.org/directory"
)

// ChallengeServer handles HTTP-01 ACME challenges
type ChallengeServer struct {
	mu         sync.RWMutex
	challenges map[string]string // token -> keyAuth
	server     *http.Server
	port       string
}

// NewChallengeServer creates a new HTTP-01 challenge server
func NewChallengeServer(port string) *ChallengeServer {
	cs := &ChallengeServer{
		challenges: make(map[string]string),
		port:       port,
	}
	cs.server = &http.Server{
		Addr:    "127.0.0.1:" + port,
		Handler: cs,
	}
	return cs
}

// Start begins listening for ACME challenges
func (cs *ChallengeServer) Start() error {
	go func() {
		if err := cs.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log error but don't crash - challenges will fail if server isn't running
		}
	}()
	return nil
}

// Stop shuts down the challenge server
func (cs *ChallengeServer) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return cs.server.Shutdown(ctx)
}

// SetChallenge registers a challenge token and its key authorization
func (cs *ChallengeServer) SetChallenge(token, keyAuth string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.challenges[token] = keyAuth
}

// ClearChallenge removes a challenge token
func (cs *ChallengeServer) ClearChallenge(token string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.challenges, token)
}

// ServeHTTP handles HTTP-01 challenge requests
func (cs *ChallengeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Expected path: /.well-known/acme-challenge/{token}
	prefix := "/.well-known/acme-challenge/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}

	token := strings.TrimPrefix(r.URL.Path, prefix)
	if token == "" {
		http.NotFound(w, r)
		return
	}

	cs.mu.RLock()
	keyAuth, ok := cs.challenges[token]
	cs.mu.RUnlock()

	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(keyAuth))
}

// ACMEAccount represents a stored ACME account
type ACMEAccount struct {
	URL        string `json:"url"`
	PrivateKey []byte `json:"private_key"` // PEM encoded
}

// ACMEClientManager manages ACME client and account
type ACMEClientManager struct {
	client      *acme.Client
	account     *acme.Account
	accountPath string
	certDir     string
	staging     bool
	mu          sync.Mutex
	privateKey  crypto.PrivateKey
	initialized bool
}

// NewACMEClientManager creates a new ACME client manager
func NewACMEClientManager(certDir string, staging bool) (*ACMEClientManager, error) {
	accountDir := filepath.Join(certDir, accountsDirName)
	if err := os.MkdirAll(accountDir, constants.ModeDirPrivate); err != nil {
		return nil, fmt.Errorf("failed to create account directory: %w", err)
	}

	return &ACMEClientManager{
		certDir:     certDir,
		accountPath: filepath.Join(accountDir, accountFileName),
		staging:     staging,
	}, nil
}

// GetClient returns the ACME client, initializing it if necessary
func (m *ACMEClientManager) GetClient(ctx context.Context) (*acme.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.initialized {
		return m.client, nil
	}

	if err := m.loadOrCreateAccount(ctx); err != nil {
		return nil, err
	}

	m.initialized = true
	return m.client, nil
}

func (m *ACMEClientManager) loadOrCreateAccount(ctx context.Context) error {
	directoryURL := letsEncryptProduction
	if m.staging {
		directoryURL = letsEncryptStaging
	}

	// Try to load existing account
	data, err := os.ReadFile(m.accountPath)
	if err == nil {
		var stored ACMEAccount
		if err := json.Unmarshal(data, &stored); err == nil && stored.URL != "" {
			// Parse the stored private key
			block, _ := pem.Decode(stored.PrivateKey)
			if block != nil {
				privateKey, err := x509.ParseECPrivateKey(block.Bytes)
				if err == nil {
					m.privateKey = privateKey
					m.client = &acme.Client{
						Key:          privateKey,
						DirectoryURL: directoryURL,
					}
					m.account = &acme.Account{URI: stored.URL}
					return nil
				}
			}
		}
	}

	// Create new account
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate account key: %w", err)
	}
	m.privateKey = privateKey

	m.client = &acme.Client{
		Key:          privateKey,
		DirectoryURL: directoryURL,
	}

	// Register with ACME server (no email required)
	account, err := m.client.Register(ctx, &acme.Account{}, acme.AcceptTOS)
	if err != nil {
		return fmt.Errorf("failed to register ACME account: %w", err)
	}
	m.account = account

	// Save account for future use
	keyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})

	stored := ACMEAccount{
		URL:        account.URI,
		PrivateKey: pemBlock,
	}

	data, err = json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal account: %w", err)
	}

	if err := os.WriteFile(m.accountPath, data, constants.ModeFileSecret); err != nil {
		return fmt.Errorf("failed to save account: %w", err)
	}

	return nil
}

// ObtainCertificate obtains a certificate for the given domains using HTTP-01 challenge
func (m *ACMEClientManager) ObtainCertificate(ctx context.Context, domains []string, challengeServer *ChallengeServer) (certPEM, keyPEM []byte, err error) {
	client, err := m.GetClient(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get ACME client: %w", err)
	}

	// Create order for the domains
	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(domains...))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create order: %w", err)
	}

	// Complete authorizations
	for _, authURL := range order.AuthzURLs {
		auth, err := client.GetAuthorization(ctx, authURL)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get authorization: %w", err)
		}

		if auth.Status == acme.StatusValid {
			continue // Already authorized
		}

		// Find HTTP-01 challenge
		var challenge *acme.Challenge
		for _, c := range auth.Challenges {
			if c.Type == "http-01" {
				challenge = c
				break
			}
		}
		if challenge == nil {
			return nil, nil, fmt.Errorf("no HTTP-01 challenge found for %s", auth.Identifier.Value)
		}

		// Get key authorization
		keyAuth, err := client.HTTP01ChallengeResponse(challenge.Token)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get challenge response: %w", err)
		}

		// Set up challenge response
		challengeServer.SetChallenge(challenge.Token, keyAuth)
		defer challengeServer.ClearChallenge(challenge.Token)

		// Accept the challenge
		if _, err := client.Accept(ctx, challenge); err != nil {
			return nil, nil, fmt.Errorf("failed to accept challenge: %w", err)
		}

		// Wait for authorization to be valid
		if _, err := client.WaitAuthorization(ctx, authURL); err != nil {
			return nil, nil, wrapAuthorizationError(auth.Identifier.Value, err)
		}
	}

	// Generate certificate private key
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate certificate key: %w", err)
	}

	// Create CSR
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: domains[0]},
		DNSNames: domains,
	}, certKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create CSR: %w", err)
	}

	// Wait for order to be ready
	order, err = client.WaitOrder(ctx, order.URI)
	if err != nil {
		return nil, nil, fmt.Errorf("failed waiting for order: %w", err)
	}

	// Finalize the order
	derCerts, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to finalize order: %w", err)
	}

	// Encode certificate chain
	var certBuf bytes.Buffer
	for _, derCert := range derCerts {
		pem.Encode(&certBuf, &pem.Block{
			Type:  "CERTIFICATE",
			Bytes: derCert,
		})
	}

	// Encode private key
	keyBytes, err := x509.MarshalECPrivateKey(certKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal certificate key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})

	return certBuf.Bytes(), keyPEM, nil
}

type CertificatesManagerConfig struct {
	CertDir          string
	HTTPProviderPort string
	TlsStaging       bool
}

type CertificatesDomain struct {
	Canonical string
	Aliases   []string
}

func (cm *CertificatesDomain) Validate() error {
	if cm.Canonical == "" {
		return fmt.Errorf("canonical domain cannot be empty")
	}

	if err := helpers.IsValidDomain(cm.Canonical); err != nil {
		return fmt.Errorf("invalid canonical domain '%s': %w", cm.Canonical, err)
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
	config          CertificatesManagerConfig
	checkMutex      sync.Mutex
	ctx             context.Context
	cancel          context.CancelFunc
	clientManager   *ACMEClientManager
	challengeServer *ChallengeServer
	updateSignal    chan<- string // signal successful updates
	debouncer       *helpers.Debouncer
}

func NewCertificatesManager(config CertificatesManagerConfig, updateSignal chan<- string) (*CertificatesManager, error) {
	if err := os.MkdirAll(config.CertDir, constants.ModeDirPrivate); err != nil {
		return nil, fmt.Errorf("failed to create certificate directory: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	clientManager, err := NewACMEClientManager(config.CertDir, config.TlsStaging)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create ACME client manager: %w", err)
	}

	challengeServer := NewChallengeServer(config.HTTPProviderPort)
	if err := challengeServer.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start challenge server: %w", err)
	}

	m := &CertificatesManager{
		config:          config,
		ctx:             ctx,
		cancel:          cancel,
		clientManager:   clientManager,
		challengeServer: challengeServer,
		updateSignal:    updateSignal,
		debouncer:       helpers.NewDebouncer(refreshDebounceDelay),
	}

	return m, nil
}

func (m *CertificatesManager) Stop() {
	m.cancel()
	m.debouncer.Stop()
	m.challengeServer.Stop()
}

func (cm *CertificatesManager) RefreshSync(logger *slog.Logger, domains []CertificatesDomain) error {
	renewedDomains, err := cm.checkRenewals(logger, domains)
	if err != nil {
		return err
	}
	if len(renewedDomains) > 0 && cm.updateSignal != nil {
		cm.updateSignal <- "certificates_renewed"
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

	// Check if staging/production mode changed
	// Staging certs have "(STAGING)" in the issuer name
	isStagingCert := strings.Contains(parsedCert.Issuer.String(), "(STAGING)")
	if isStagingCert != cm.config.TlsStaging {
		if cm.config.TlsStaging {
			logger.Debug("Production cert exists but staging mode enabled, needs new staging cert", "domain", domain.Canonical)
		} else {
			logger.Debug("Staging cert exists but production mode enabled, needs new production cert", "domain", domain.Canonical)
		}
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

	// Check if domain points to this server's IP
	domainIP, err := helpers.GetARecord(domain)
	if err != nil {
		return fmt.Errorf("domain %s has no A record (IPv4): %w", domain, err)
	}

	serverIP, err := helpers.GetExternalIP()
	if err != nil {
		// Can't determine server IP, skip this check
		return nil
	}

	if !domainIP.Equal(serverIP) {
		return fmt.Errorf(`domain %s points to %s but this server's IP is %s

Please update your DNS:
- A record: %s → %s
- Test with: dig A %s`, domain, domainIP, serverIP, domain, serverIP, domain)
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

func wrapAuthorizationError(domain string, err error) error {
	errStr := err.Error()

	if strings.Contains(errStr, "521") {
		return fmt.Errorf(`authorization failed for %s: origin server unreachable (521)

This error means Let's Encrypt could not reach your server.
If using Cloudflare proxy (orange cloud), the origin IP may be incorrect.
Otherwise, ensure your DNS A record points to this server's IP.

Original error: %w`, domain, err)
	}

	if strings.Contains(errStr, "403") || strings.Contains(errStr, "unauthorized") {
		return fmt.Errorf(`authorization failed for %s: domain verification failed

Let's Encrypt could not verify domain ownership.
Ensure the domain's A record points to this server's IP address.

Original error: %w`, domain, err)
	}

	return fmt.Errorf("authorization failed for %s: %w", domain, err)
}

func (m *CertificatesManager) obtainCertificate(managedDomain CertificatesDomain) (obtainedDomain CertificatesDomain, err error) {
	canonicalDomain := managedDomain.Canonical
	aliases := managedDomain.Aliases
	allDomains := append([]string{canonicalDomain}, aliases...)

	for _, domain := range allDomains {
		if err := m.validateDomain(domain); err != nil {
			return obtainedDomain, fmt.Errorf("domain validation failed for %s: %w", domain, err)
		}
	}

	certPEM, keyPEM, err := m.clientManager.ObtainCertificate(m.ctx, allDomains, m.challengeServer)
	if err != nil {
		return obtainedDomain, fmt.Errorf("failed to obtain certificate for %s: %w", canonicalDomain, err)
	}

	if err := m.saveCertificate(canonicalDomain, keyPEM, certPEM); err != nil {
		return obtainedDomain, fmt.Errorf("failed to save certificate for %s: %w", canonicalDomain, err)
	}

	obtainedDomain = CertificatesDomain{
		Canonical: canonicalDomain,
		Aliases:   aliases,
	}

	return obtainedDomain, nil
}

func (m *CertificatesManager) saveCertificate(domain string, keyPEM, certPEM []byte) error {
	combinedPath := filepath.Join(m.config.CertDir, domain+combinedCertExt)
	tmpPath := combinedPath + ".tmp"

	pemContent := bytes.Buffer{}

	pemContent.Write(keyPEM)
	if len(keyPEM) > 0 && keyPEM[len(keyPEM)-1] != '\n' {
		pemContent.WriteByte('\n')
	}

	pemContent.Write(certPEM)
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
