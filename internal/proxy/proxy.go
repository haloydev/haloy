package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
)

// Backend represents a backend server that can receive traffic.
type Backend struct {
	IP   string
	Port string
}

// Route represents a domain route configuration.
type Route struct {
	Canonical string
	Aliases   []string
	Backends  []Backend
}

// Config holds the proxy configuration.
type Config struct {
	// Routes maps canonical domains (lowercase) to their route configurations.
	Routes map[string]*Route
	// APIDomain is the domain for the haloy API (lowercase).
	APIDomain string
}

// Proxy is an HTTP reverse proxy with TLS termination and host-based routing.
type Proxy struct {
	config     atomic.Pointer[Config]
	certLoader CertLoader
	apiHandler http.Handler
	logger     *slog.Logger

	httpServer  *http.Server
	httpsServer *http.Server

	// Transport for backend connections with connection pooling
	transport *http.Transport

	// For graceful shutdown
	shutdownMu sync.Mutex
	isShutdown bool

	// Round-robin load balancing state
	rrMu      sync.Mutex
	rrIndexes map[string]uint32 // canonical domain -> next backend index
}

// CertLoader is an interface for loading TLS certificates.
type CertLoader interface {
	GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error)
}

// New creates a new Proxy instance.
func New(logger *slog.Logger, certLoader CertLoader, apiHandler http.Handler) *Proxy {
	p := &Proxy{
		logger:     logger,
		certLoader: certLoader,
		apiHandler: apiHandler,
		transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
		rrIndexes: make(map[string]uint32),
	}

	// Initialize with empty config
	p.config.Store(&Config{
		Routes: make(map[string]*Route),
	})

	return p
}

// UpdateConfig atomically updates the proxy configuration.
func (p *Proxy) UpdateConfig(config *Config) {
	p.config.Store(config)
	p.logger.Info("Proxy configuration updated",
		"routes", len(config.Routes),
		"api_domain", config.APIDomain)
}

// GetConfig returns the current proxy configuration.
func (p *Proxy) GetConfig() *Config {
	return p.config.Load()
}

// selectBackend picks the next backend using round-robin selection.
func (p *Proxy) selectBackend(route *Route) Backend {
	if len(route.Backends) == 1 {
		return route.Backends[0]
	}

	p.rrMu.Lock()
	index := p.rrIndexes[route.Canonical]
	p.rrIndexes[route.Canonical] = index + 1
	p.rrMu.Unlock()

	return route.Backends[index%uint32(len(route.Backends))]
}

// Start starts both HTTP and HTTPS servers.
func (p *Proxy) Start(httpAddr, httpsAddr string) error {
	p.logger.Info("Starting proxy", "http_addr", httpAddr, "https_addr", httpsAddr)

	// Create HTTP server (redirects to HTTPS, handles ACME challenges)
	p.httpServer = &http.Server{
		Addr:              httpAddr,
		Handler:           p.httpHandler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Create HTTPS server with TLS
	tlsConfig := &tls.Config{
		GetCertificate: p.certLoader.GetCertificate,
		NextProtos:     []string{"h2", "http/1.1"},
		MinVersion:     tls.VersionTLS12,
	}

	p.httpsServer = &http.Server{
		Addr:              httpsAddr,
		Handler:           p.httpsHandler(),
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 2)

	// Start HTTP server in goroutine
	go func() {
		p.logger.Info("HTTP server listening", "addr", httpAddr)
		if err := p.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			p.logger.Error("HTTP server error", "error", err)
			errCh <- fmt.Errorf("HTTP server: %w", err)
		}
	}()

	// Start HTTPS server in goroutine
	go func() {
		p.logger.Info("HTTPS server listening", "addr", httpsAddr)
		// ListenAndServeTLS with empty cert/key paths uses GetCertificate from TLSConfig
		if err := p.httpsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			p.logger.Error("HTTPS server error", "error", err)
			errCh <- fmt.Errorf("HTTPS server: %w", err)
		}
	}()

	// Give servers a moment to start and check for immediate errors
	select {
	case err := <-errCh:
		return err
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

// Shutdown gracefully shuts down both HTTP and HTTPS servers.
func (p *Proxy) Shutdown(ctx context.Context) error {
	p.shutdownMu.Lock()
	if p.isShutdown {
		p.shutdownMu.Unlock()
		return nil
	}
	p.isShutdown = true
	p.shutdownMu.Unlock()

	p.logger.Info("Shutting down proxy...")

	var errs []error

	if p.httpServer != nil {
		if err := p.httpServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("HTTP server shutdown: %w", err))
		}
	}

	if p.httpsServer != nil {
		if err := p.httpsServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("HTTPS server shutdown: %w", err))
		}
	}

	p.transport.CloseIdleConnections()

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	p.logger.Info("Proxy shutdown complete")
	return nil
}

// httpHandler handles HTTP requests (port 80).
// It redirects to HTTPS except for ACME challenges and localhost API access.
// For known routes, it redirects directly to the canonical domain.
func (p *Proxy) httpHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow ACME challenges through
		if strings.HasPrefix(r.URL.Path, "/.well-known/acme-challenge/") {
			p.handleACMEChallenge(w, r)
			return
		}

		// Get host without port
		host := extractHost(r.Host)

		// Always serve API over HTTP for localhost (local development)
		if helpers.IsLocalhost(host) {
			p.apiHandler.ServeHTTP(w, r)
			return
		}

		// Determine redirect target (default: same host for unknown domains)
		targetHost := host

		config := p.config.Load()

		// Check if this is the API domain
		if config.APIDomain != "" && host == config.APIDomain {
			targetHost = config.APIDomain
		} else if route := p.findRoute(config, host); route != nil {
			// Redirect to canonical domain
			targetHost = route.Canonical
		}

		httpsURL := &url.URL{
			Scheme:   "https",
			Host:     targetHost,
			Path:     r.URL.Path,
			RawQuery: r.URL.RawQuery,
		}

		http.Redirect(w, r, httpsURL.String(), http.StatusMovedPermanently)
	})
}

// httpsHandler handles HTTPS requests (port 443).
func (p *Proxy) httpsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// Allow ACME challenges through (in case of direct HTTPS requests)
		if strings.HasPrefix(r.URL.Path, "/.well-known/acme-challenge/") {
			p.handleACMEChallenge(w, r)
			return
		}

		// Get host without port
		host := extractHost(r.Host)

		config := p.config.Load()

		// Check if this is the API domain - route internally
		if config.APIDomain != "" && host == config.APIDomain {
			p.apiHandler.ServeHTTP(w, r)
			return
		}

		// Find matching route
		route := p.findRoute(config, host)
		if route == nil {
			p.logRequest(r, http.StatusNotFound, time.Since(startTime))
			p.serveErrorPage(w, http.StatusNotFound, "Not Found")
			return
		}

		// Check if this is an alias that should redirect to canonical
		if host != strings.ToLower(route.Canonical) {
			canonicalURL := &url.URL{
				Scheme:   "https",
				Host:     route.Canonical,
				Path:     r.URL.Path,
				RawQuery: r.URL.RawQuery,
			}
			p.logRequest(r, http.StatusMovedPermanently, time.Since(startTime))
			http.Redirect(w, r, canonicalURL.String(), http.StatusMovedPermanently)
			return
		}

		// Check for WebSocket upgrade
		if isWebSocketUpgrade(r) {
			p.handleWebSocket(w, r, route, startTime)
			return
		}

		// Select a backend (simple round-robin would go here, for now just use first)
		if len(route.Backends) == 0 {
			p.logRequest(r, http.StatusBadGateway, time.Since(startTime))
			p.serveErrorPage(w, http.StatusBadGateway, "No backends available")
			return
		}

		backend := p.selectBackend(route)
		backendAddr := net.JoinHostPort(backend.IP, backend.Port)

		p.proxyToBackend(w, r, backendAddr, startTime)
	})
}

// findRoute finds a route for the given host (checking canonical and aliases).
func (p *Proxy) findRoute(config *Config, host string) *Route {
	if route, ok := config.Routes[host]; ok {
		return route
	}

	// check aliases
	for _, route := range config.Routes {
		for _, alias := range route.Aliases {
			if strings.ToLower(alias) == host {
				return route
			}
		}
	}

	return nil
}

// proxyToBackend proxies the request to a backend server.
func (p *Proxy) proxyToBackend(w http.ResponseWriter, r *http.Request, backendAddr string, startTime time.Time) {
	targetURL := &url.URL{
		Scheme: "http",
		Host:   backendAddr,
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(targetURL)
			pr.SetXForwarded()
			pr.Out.Host = r.Host
		},
		Transport:     p.transport,
		FlushInterval: -1, // Flush immediately for streaming
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			p.logger.Error("Proxy error",
				"host", r.Host,
				"path", r.URL.Path,
				"backend", backendAddr,
				"error", err)
			p.logRequest(r, http.StatusBadGateway, time.Since(startTime))
			p.serveErrorPage(w, http.StatusBadGateway, "Backend unavailable")
		},
		ModifyResponse: func(resp *http.Response) error {
			p.logRequest(r, resp.StatusCode, time.Since(startTime))
			return nil
		},
	}

	proxy.ServeHTTP(w, r)
}

// handleACMEChallenge forwards ACME challenges to the certificate manager's HTTP-01 server.
func (p *Proxy) handleACMEChallenge(w http.ResponseWriter, r *http.Request) {
	targetURL := &url.URL{
		Scheme: "http",
		Host:   "127.0.0.1:" + constants.CertificatesHTTPProviderPort,
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(targetURL)
			pr.Out.Host = r.Host
		},
		Transport: p.transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// ACME challenge not available - this is expected when no challenge is pending
			http.Error(w, "ACME challenge not found", http.StatusNotFound)
		},
	}

	proxy.ServeHTTP(w, r)
}

// serveErrorPage serves a simple error page.
func (p *Proxy) serveErrorPage(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>%d %s</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
               display: flex; justify-content: center; align-items: center;
               height: 100vh; margin: 0; background: #f5f5f5; }
        .container { text-align: center; }
        h1 { color: #333; font-size: 72px; margin: 0; }
        p { color: #666; font-size: 24px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>%d</h1>
        <p>%s</p>
    </div>
</body>
</html>`, statusCode, message, statusCode, message)
}

// logRequest logs an HTTP request in structured JSON format.
func (p *Proxy) logRequest(r *http.Request, statusCode int, duration time.Duration) {
	p.logger.Info("request",
		"method", r.Method,
		"host", r.Host,
		"path", r.URL.Path,
		"status", statusCode,
		"duration_ms", duration.Milliseconds(),
		"remote_addr", r.RemoteAddr,
		"user_agent", r.UserAgent(),
	)
}

// extractHost extracts the hostname from a host:port string and lowercases it.
func extractHost(hostPort string) string {
	host := hostPort
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		host = h
	}
	return strings.ToLower(host)
}
