package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
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

	// next holds the round-robin backend index for this route.
	next atomic.Uint32
}

// nextBackend picks the next backend using round-robin selection.
func (r *Route) nextBackend() Backend {
	if len(r.Backends) == 1 {
		return r.Backends[0]
	}
	index := r.next.Add(1) - 1
	return r.Backends[int(index)%len(r.Backends)]
}

// Config is an immutable, validated routing snapshot. Build one with
// RouteBuilder; the zero value routes nothing.
type Config struct {
	// routes maps canonical domains (lowercase) to their route configurations.
	routes map[string]*Route
	// hosts is a flat lookup index mapping every canonical domain and alias
	// (lowercase) to its route.
	hosts map[string]*Route
	// apiDomain is the domain for the haloy API (lowercase).
	apiDomain string
	// apiBackend is the control plane's API listener; the zero value means no
	// control plane is reachable and API traffic is answered with 503.
	apiBackend Backend
}

// FindRoute returns the route for the given host (canonical or alias), or nil.
func (c *Config) FindRoute(host string) *Route {
	return c.hosts[strings.ToLower(host)]
}

// APIDomain returns the domain for the haloy API (lowercase).
func (c *Config) APIDomain() string {
	return c.apiDomain
}

// APIBackend returns the control plane's API listener address and whether one
// is configured.
func (c *Config) APIBackend() (Backend, bool) {
	return c.apiBackend, c.apiBackend != Backend{}
}

// RouteCount returns the number of routes (canonical domains).
func (c *Config) RouteCount() int {
	return len(c.routes)
}

// IsKnownHost reports whether the host is a routed domain (canonical or alias)
// or the API domain.
func (c *Config) IsKnownHost(host string) bool {
	host = strings.ToLower(host)
	if host == "" {
		return false
	}
	if c.apiDomain != "" && host == c.apiDomain {
		return true
	}
	_, ok := c.hosts[host]
	return ok
}

// ResolveCanonical resolves a domain (canonical or alias) to its canonical domain.
func (c *Config) ResolveCanonical(domain string) (string, bool) {
	if route := c.FindRoute(domain); route != nil {
		return route.Canonical, true
	}
	return "", false
}

// Proxy is an HTTP reverse proxy with TLS termination and host-based routing.
type Proxy struct {
	config     atomic.Pointer[Config]
	certLoader CertLoader
	logger     *slog.Logger

	httpServer  *http.Server
	httpsServer *http.Server

	// fatalCh receives listener errors that occur after Start returned.
	fatalCh chan error

	// Transport for backend connections with connection pooling
	transport *http.Transport

	// For graceful shutdown
	shutdownMu sync.Mutex
	isShutdown bool

	// Active hijacked WebSocket tunnels, tracked so Shutdown can drain them.
	wsMu    sync.Mutex
	wsConns map[net.Conn]struct{}
	wsWg    sync.WaitGroup
}

// CertLoader is an interface for loading TLS certificates.
type CertLoader interface {
	GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error)
}

// New creates a new Proxy instance.
func New(logger *slog.Logger, certLoader CertLoader) *Proxy {
	p := &Proxy{
		logger:     logger,
		certLoader: certLoader,
		fatalCh:    make(chan error, 2),
		transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		wsConns: make(map[net.Conn]struct{}),
	}

	// Initialize with empty config
	p.config.Store(&Config{
		routes: make(map[string]*Route),
		hosts:  make(map[string]*Route),
	})

	return p
}

// UpdateConfig atomically updates the proxy configuration. If the cert loader
// uses routing information (alias resolution, known-host checks), the new
// snapshot is forwarded to it as well.
func (p *Proxy) UpdateConfig(config *Config) {
	if config == nil {
		return
	}
	p.config.Store(config)
	if ra, ok := p.certLoader.(interface{ SetRouteTable(*Config) }); ok {
		ra.SetRouteTable(config)
	}
	p.logger.Info("Proxy configuration updated",
		"routes", config.RouteCount(),
		"api_domain", config.APIDomain())
}

// GetConfig returns the current proxy configuration.
func (p *Proxy) GetConfig() *Config {
	return p.config.Load()
}

// Start binds the HTTP and HTTPS listeners and starts serving. A bind failure
// is returned immediately; errors after that are delivered on Err().
func (p *Proxy) Start(httpAddr, httpsAddr string) error {
	p.logger.Info("Starting proxy", "http_addr", httpAddr, "https_addr", httpsAddr)

	httpListener, err := net.Listen("tcp", httpAddr)
	if err != nil {
		return fmt.Errorf("HTTP listener: %w", err)
	}

	httpsListener, err := net.Listen("tcp", httpsAddr)
	if err != nil {
		httpListener.Close()
		return fmt.Errorf("HTTPS listener: %w", err)
	}

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
		ErrorLog:          log.New(io.Discard, "", 0),
	}

	go func() {
		p.logger.Info("HTTP server listening", "addr", httpAddr)
		if err := p.httpServer.Serve(httpListener); err != nil && err != http.ErrServerClosed {
			p.logger.Error("HTTP server error", "error", err)
			p.fatalCh <- fmt.Errorf("HTTP server: %w", err)
		}
	}()

	go func() {
		p.logger.Info("HTTPS server listening", "addr", httpsAddr)
		if err := p.httpsServer.ServeTLS(httpsListener, "", ""); err != nil && err != http.ErrServerClosed {
			p.logger.Error("HTTPS server error", "error", err)
			p.fatalCh <- fmt.Errorf("HTTPS server: %w", err)
		}
	}()

	return nil
}

// Err returns a channel that receives fatal listener errors occurring after
// Start returned. A value on this channel means the proxy is no longer
// serving traffic and the process should exit.
func (p *Proxy) Err() <-chan error {
	return p.fatalCh
}

// Shutdown gracefully shuts down both HTTP and HTTPS servers, then waits for
// active WebSocket tunnels to drain. Tunnels still open when ctx expires are
// force-closed.
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

	// Hijacked WebSocket connections are not tracked by http.Server.Shutdown,
	// so drain them separately.
	wsDone := make(chan struct{})
	go func() {
		p.wsWg.Wait()
		close(wsDone)
	}()

	select {
	case <-wsDone:
	case <-ctx.Done():
		p.wsMu.Lock()
		open := len(p.wsConns) / 2
		for conn := range p.wsConns {
			conn.Close()
		}
		p.wsMu.Unlock()
		<-wsDone
		errs = append(errs, fmt.Errorf("force-closed %d websocket tunnel(s): %w", open, ctx.Err()))
	}

	p.transport.CloseIdleConnections()

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	p.logger.Info("Proxy shutdown complete")
	return nil
}

// trackWebSocket registers a hijacked tunnel's connections for shutdown
// draining. It returns false if the proxy is already shutting down.
func (p *Proxy) trackWebSocket(conns ...net.Conn) bool {
	p.shutdownMu.Lock()
	defer p.shutdownMu.Unlock()
	if p.isShutdown {
		return false
	}
	p.wsWg.Add(1)
	p.wsMu.Lock()
	for _, conn := range conns {
		p.wsConns[conn] = struct{}{}
	}
	p.wsMu.Unlock()
	return true
}

func (p *Proxy) untrackWebSocket(conns ...net.Conn) {
	p.wsMu.Lock()
	for _, conn := range conns {
		delete(p.wsConns, conn)
	}
	p.wsMu.Unlock()
	p.wsWg.Done()
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

		// Always serve API over HTTP for localhost (local development).
		// The Host header is client-controlled, so also require the connection
		// to actually come from loopback.
		if helpers.IsLocalhost(host) && isLoopbackAddr(r.RemoteAddr) {
			p.proxyToAPIBackend(w, r, time.Now())
			return
		}

		// Determine redirect target (default: same host for unknown domains)
		targetHost := host

		config := p.config.Load()

		// Check if this is the API domain
		if config.APIDomain() != "" && host == config.APIDomain() {
			targetHost = config.APIDomain()
		} else if route := config.FindRoute(host); route != nil {
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

		// Check if this is the API domain - forward to the control plane
		if config.APIDomain() != "" && host == config.APIDomain() {
			p.proxyToAPIBackend(w, r, startTime)
			return
		}

		// Find matching route
		route := config.FindRoute(host)
		if route == nil {
			p.serveErrorPage(w, http.StatusNotFound, "Not Found")
			return
		}

		// Check if this is an alias that should redirect to canonical
		if host != route.Canonical {
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

		if len(route.Backends) == 0 {
			p.logRequest(r, http.StatusBadGateway, time.Since(startTime))
			p.serveErrorPage(w, http.StatusBadGateway, "No healthy backends available for this application")
			return
		}

		p.proxyToBackend(w, r, route, startTime)
	})
}

// proxyToBackend proxies the request to one of the route's backends. If the
// dial fails and the route has other backends, the request is retried once on
// the next backend; a dial error means no bytes were sent, so the request is
// safe to replay.
func (p *Proxy) proxyToBackend(w http.ResponseWriter, r *http.Request, route *Route, startTime time.Time) {
	maxAttempts := 1
	if len(route.Backends) > 1 {
		maxAttempts = 2
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		backend := route.nextBackend()
		backendAddr := net.JoinHostPort(backend.IP, backend.Port)

		targetURL := &url.URL{
			Scheme: "http",
			Host:   backendAddr,
		}

		var retryErr error

		proxy := &httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(targetURL)
				pr.SetXForwarded()
				pr.Out.Header.Del("X-Real-IP")
				pr.Out.Host = r.Host
			},
			Transport:     p.transport,
			FlushInterval: -1, // Flush immediately for streaming
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				if attempt < maxAttempts && isDialError(err) && r.Context().Err() == nil {
					retryErr = err
					return
				}
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
		if retryErr == nil {
			return
		}
		p.logger.Warn("Backend dial failed, retrying with next backend",
			"host", r.Host,
			"backend", backendAddr,
			"error", retryErr)
	}
}

// proxyToAPIBackend forwards API traffic to the control plane's loopback
// listener. There is exactly one backend, so no retry; if the control plane
// is down (e.g. mid-upgrade) the client gets 503 and can retry.
func (p *Proxy) proxyToAPIBackend(w http.ResponseWriter, r *http.Request, startTime time.Time) {
	backend, ok := p.config.Load().APIBackend()
	if !ok {
		p.logRequest(r, http.StatusServiceUnavailable, time.Since(startTime))
		p.serveErrorPage(w, http.StatusServiceUnavailable, "Control plane unavailable")
		return
	}

	targetURL := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(backend.IP, backend.Port),
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(targetURL)
			pr.SetXForwarded()
			pr.Out.Header.Del("X-Real-IP")
			pr.Out.Host = r.Host
		},
		Transport:     p.transport,
		FlushInterval: -1, // API streams deploy logs via SSE
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			p.logger.Error("API proxy error",
				"host", r.Host,
				"path", r.URL.Path,
				"backend", targetURL.Host,
				"error", err)
			p.logRequest(r, http.StatusServiceUnavailable, time.Since(startTime))
			p.serveErrorPage(w, http.StatusServiceUnavailable, "Control plane unavailable")
		},
		ModifyResponse: func(resp *http.Response) error {
			p.logRequest(r, resp.StatusCode, time.Since(startTime))
			return nil
		},
	}

	proxy.ServeHTTP(w, r)
}

// isDialError reports whether err came from dialing the backend, meaning no
// part of the request was sent.
func isDialError(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
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
			pr.Out.Header.Del("X-Real-IP")
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
	p.logger.Info(
		"request",
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

// isLoopbackAddr reports whether the remote address is a loopback IP.
func isLoopbackAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
