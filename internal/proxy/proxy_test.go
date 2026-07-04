package proxy

import (
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

func TestConfigFindRoute(t *testing.T) {
	rb := NewRouteBuilder()
	rb.AddRoute("example.com", []string{"www.example.com", "alias.example.com"}, []Backend{{IP: "10.0.0.1", Port: "8080"}})
	rb.AddRoute("other.com", nil, []Backend{{IP: "10.0.0.2", Port: "8080"}})

	config, err := rb.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	tests := []struct {
		name      string
		host      string
		wantNil   bool
		wantCanon string
	}{
		{
			name:      "find by canonical domain",
			host:      "example.com",
			wantNil:   false,
			wantCanon: "example.com",
		},
		{
			name:      "find by alias",
			host:      "www.example.com",
			wantNil:   false,
			wantCanon: "example.com",
		},
		{
			name:      "find by second alias",
			host:      "alias.example.com",
			wantNil:   false,
			wantCanon: "example.com",
		},
		{
			name:      "case insensitive canonical",
			host:      "ExAmPlE.CoM",
			wantNil:   false,
			wantCanon: "example.com",
		},
		{
			name:    "unknown domain returns nil",
			host:    "unknown.com",
			wantNil: true,
		},
		{
			name:      "find other route",
			host:      "other.com",
			wantNil:   false,
			wantCanon: "other.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := config.FindRoute(tt.host)

			if tt.wantNil {
				if route != nil {
					t.Errorf("FindRoute() = %v, want nil", route)
				}
				return
			}

			if route == nil {
				t.Fatal("FindRoute() = nil, want non-nil")
			}

			if route.Canonical != tt.wantCanon {
				t.Errorf("FindRoute().Canonical = %q, want %q", route.Canonical, tt.wantCanon)
			}
		})
	}
}

func TestNextBackend_SingleBackend(t *testing.T) {
	route := &Route{
		Canonical: "example.com",
		Backends:  []Backend{{IP: "10.0.0.1", Port: "8080"}},
	}

	// Call multiple times - should always return the same backend
	for range 5 {
		backend := route.nextBackend()
		if backend.IP != "10.0.0.1" || backend.Port != "8080" {
			t.Errorf("nextBackend() = %v, want {10.0.0.1 8080}", backend)
		}
	}
}

func TestNextBackend_RoundRobin(t *testing.T) {
	route := &Route{
		Canonical: "example.com",
		Backends: []Backend{
			{IP: "10.0.0.1", Port: "8080"},
			{IP: "10.0.0.2", Port: "8080"},
			{IP: "10.0.0.3", Port: "8080"},
		},
	}

	// Expect round-robin order: 1, 2, 3, 1, 2, 3, ...
	expected := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.1", "10.0.0.2", "10.0.0.3"}

	for i, expectedIP := range expected {
		backend := route.nextBackend()
		if backend.IP != expectedIP {
			t.Errorf("nextBackend() call %d: got IP %s, want %s", i, backend.IP, expectedIP)
		}
	}
}

func TestNextBackend_IndependentRoutes(t *testing.T) {
	route1 := &Route{
		Canonical: "example1.com",
		Backends: []Backend{
			{IP: "10.0.0.1", Port: "8080"},
			{IP: "10.0.0.2", Port: "8080"},
		},
	}

	route2 := &Route{
		Canonical: "example2.com",
		Backends: []Backend{
			{IP: "10.0.1.1", Port: "8080"},
			{IP: "10.0.1.2", Port: "8080"},
		},
	}

	// First call to route1
	b1 := route1.nextBackend()
	if b1.IP != "10.0.0.1" {
		t.Errorf("route1 first call: got %s, want 10.0.0.1", b1.IP)
	}

	// First call to route2 - should still start at index 0
	b2 := route2.nextBackend()
	if b2.IP != "10.0.1.1" {
		t.Errorf("route2 first call: got %s, want 10.0.1.1", b2.IP)
	}

	// Second call to route1 - should continue its own sequence
	b1 = route1.nextBackend()
	if b1.IP != "10.0.0.2" {
		t.Errorf("route1 second call: got %s, want 10.0.0.2", b1.IP)
	}

	// Second call to route2 - should continue its own sequence
	b2 = route2.nextBackend()
	if b2.IP != "10.0.1.2" {
		t.Errorf("route2 second call: got %s, want 10.0.1.2", b2.IP)
	}
}

func TestNextBackend_Concurrent(t *testing.T) {
	route := &Route{
		Canonical: "example.com",
		Backends: []Backend{
			{IP: "10.0.0.1", Port: "8080"},
			{IP: "10.0.0.2", Port: "8080"},
		},
	}

	var wg sync.WaitGroup
	results := make(chan string, 100)

	// Spawn many goroutines to test concurrent access
	for range 100 {
		wg.Go(func() {
			backend := route.nextBackend()
			results <- backend.IP
		})
	}

	wg.Wait()
	close(results)

	// Count results - should be roughly 50/50 split
	counts := make(map[string]int)
	for ip := range results {
		counts[ip]++
	}

	// Just verify we got results for both backends (no crashes)
	if counts["10.0.0.1"] == 0 || counts["10.0.0.2"] == 0 {
		t.Errorf("Expected both backends to be selected, got: %v", counts)
	}
}

func TestExtractHost(t *testing.T) {
	tests := []struct {
		name     string
		hostPort string
		want     string
	}{
		{
			name:     "host without port",
			hostPort: "example.com",
			want:     "example.com",
		},
		{
			name:     "host with port",
			hostPort: "example.com:8080",
			want:     "example.com",
		},
		{
			name:     "host with standard https port",
			hostPort: "example.com:443",
			want:     "example.com",
		},
		{
			name:     "uppercase host normalized",
			hostPort: "EXAMPLE.COM",
			want:     "example.com",
		},
		{
			name:     "uppercase host with port normalized",
			hostPort: "EXAMPLE.COM:8080",
			want:     "example.com",
		},
		{
			name:     "ipv4 address",
			hostPort: "192.168.1.1:8080",
			want:     "192.168.1.1",
		},
		{
			name:     "ipv6 address with port",
			hostPort: "[::1]:8080",
			want:     "::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractHost(tt.hostPort)
			if got != tt.want {
				t.Errorf("extractHost(%q) = %q, want %q", tt.hostPort, got, tt.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := New(logger, nil)

	if p == nil {
		t.Fatal("New() returned nil")
	}

	config := p.GetConfig()
	if config == nil {
		t.Fatal("GetConfig() returned nil")
	}

	if config.RouteCount() != 0 {
		t.Errorf("initial RouteCount() = %d, want 0", config.RouteCount())
	}
}

type routeTableRecorder struct {
	config *Config
}

func (r *routeTableRecorder) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return nil, nil
}

func (r *routeTableRecorder) SetRouteTable(config *Config) {
	r.config = config
}

func TestUpdateConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	recorder := &routeTableRecorder{}
	p := New(logger, recorder)

	rb := NewRouteBuilder()
	rb.SetAPIDomain("api.example.com")
	rb.AddRoute("example.com", nil, []Backend{{IP: "10.0.0.1", Port: "8080"}})
	newConfig, err := rb.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	p.UpdateConfig(newConfig)

	got := p.GetConfig()
	if got != newConfig {
		t.Error("GetConfig() did not return the updated config")
	}

	if got.RouteCount() != 1 {
		t.Errorf("RouteCount() = %d, want 1", got.RouteCount())
	}

	if got.APIDomain() != "api.example.com" {
		t.Errorf("APIDomain() = %q, want %q", got.APIDomain(), "api.example.com")
	}

	// The route table must be forwarded to the cert loader.
	if recorder.config != newConfig {
		t.Error("UpdateConfig() did not forward the route table to the cert loader")
	}
}

func TestHTTPHandler_LocalhostAPIRequiresLoopback(t *testing.T) {
	apiBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "api")
	}))
	defer apiBackend.Close()

	backendURL, err := url.Parse(apiBackend.URL)
	if err != nil {
		t.Fatal(err)
	}
	backendHost, backendPort, err := net.SplitHostPort(backendURL.Host)
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := New(logger, nil)

	rb := NewRouteBuilder()
	rb.SetAPIBackend(backendHost, backendPort)
	cfg, err := rb.Build()
	if err != nil {
		t.Fatal(err)
	}
	p.UpdateConfig(cfg)

	handler := p.httpHandler()

	// A remote client spoofing Host: localhost must not reach the API.
	r := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	r.RemoteAddr = "203.0.113.9:44321"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusMovedPermanently {
		t.Errorf("remote request with Host localhost: status = %d, want %d (redirect, not API)", w.Code, http.StatusMovedPermanently)
	}

	// A genuine loopback connection gets the API.
	r = httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	r.RemoteAddr = "127.0.0.1:53422"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Body.String() != "api" {
		t.Errorf("loopback request with Host localhost: status = %d body = %q, want API response", w.Code, w.Body.String())
	}

	// IPv6 loopback works too.
	r = httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	r.RemoteAddr = "[::1]:53423"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("IPv6 loopback request: status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHTTPSHandler_APIDomainForwardsToBackend(t *testing.T) {
	var gotForwardedFor string
	apiBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotForwardedFor = r.Header.Get("X-Forwarded-For")
		io.WriteString(w, "api")
	}))
	defer apiBackend.Close()

	backendURL, err := url.Parse(apiBackend.URL)
	if err != nil {
		t.Fatal(err)
	}
	backendHost, backendPort, err := net.SplitHostPort(backendURL.Host)
	if err != nil {
		t.Fatal(err)
	}

	p := newTestProxy()
	rb := NewRouteBuilder()
	rb.SetAPIDomain("api.example.com")
	rb.SetAPIBackend(backendHost, backendPort)
	cfg, err := rb.Build()
	if err != nil {
		t.Fatal(err)
	}
	p.UpdateConfig(cfg)

	handler := p.httpsHandler()

	r := httptest.NewRequest(http.MethodGet, "https://api.example.com/v1/version", nil)
	r.RemoteAddr = "203.0.113.9:44321"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK || w.Body.String() != "api" {
		t.Fatalf("status = %d body = %q, want API backend response", w.Code, w.Body.String())
	}
	if gotForwardedFor != "203.0.113.9" {
		t.Errorf("X-Forwarded-For = %q, want %q (rate limiting needs the real client IP)", gotForwardedFor, "203.0.113.9")
	}
}

func TestHTTPSHandler_APIDomainWithoutBackendIs503(t *testing.T) {
	p := newTestProxy()
	rb := NewRouteBuilder()
	rb.SetAPIDomain("api.example.com")
	cfg, err := rb.Build()
	if err != nil {
		t.Fatal(err)
	}
	p.UpdateConfig(cfg)

	r := httptest.NewRequest(http.MethodGet, "https://api.example.com/v1/version", nil)
	w := httptest.NewRecorder()
	p.httpsHandler().ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d when no control plane backend is configured", w.Code, http.StatusServiceUnavailable)
	}
}

func TestProxyToBackend_DialFailover(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	}))
	defer backend.Close()

	liveURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	liveHost, livePort, err := net.SplitHostPort(liveURL.Host)
	if err != nil {
		t.Fatal(err)
	}

	// Reserve a port and close it so dialing it fails.
	deadListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadHost, deadPort, err := net.SplitHostPort(deadListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	deadListener.Close()

	p := newTestProxy()
	route := &Route{
		Canonical: "example.com",
		Backends: []Backend{
			{IP: deadHost, Port: deadPort},
			{IP: liveHost, Port: livePort},
		},
	}

	r := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	w := httptest.NewRecorder()
	p.proxyToBackend(w, r, route, time.Now())

	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Errorf("status = %d body = %q, want request to fail over to the live backend", w.Code, w.Body.String())
	}
}

type stubCertLoader struct{}

func (stubCertLoader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return nil, nil
}

func TestStart_BindErrorIsReturned(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()

	p := New(slog.New(slog.NewTextHandler(io.Discard, nil)), stubCertLoader{})
	if err := p.Start(occupied.Addr().String(), "127.0.0.1:0"); err == nil {
		p.Shutdown(t.Context())
		t.Fatal("Start() on an occupied port should return an error")
	}
}

func TestStartAndShutdown(t *testing.T) {
	p := New(slog.New(slog.NewTextHandler(io.Discard, nil)), stubCertLoader{})
	if err := p.Start("127.0.0.1:0", "127.0.0.1:0"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	select {
	case err := <-p.Err():
		t.Fatalf("unexpected fatal listener error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := p.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}
