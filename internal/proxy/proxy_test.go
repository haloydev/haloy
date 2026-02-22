package proxy

import (
	"log/slog"
	"os"
	"sync"
	"testing"
)

func TestFindRoute(t *testing.T) {
	config := &Config{
		Routes: map[string]*Route{
			"example.com": {
				Canonical: "example.com",
				Aliases:   []string{"www.example.com", "alias.example.com"},
				Backends:  []Backend{{IP: "10.0.0.1", Port: "8080"}},
			},
			"other.com": {
				Canonical: "other.com",
				Aliases:   nil,
				Backends:  []Backend{{IP: "10.0.0.2", Port: "8080"}},
			},
		},
	}

	p := &Proxy{}

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
			route := p.findRoute(config, tt.host)

			if tt.wantNil {
				if route != nil {
					t.Errorf("findRoute() = %v, want nil", route)
				}
				return
			}

			if route == nil {
				t.Fatal("findRoute() = nil, want non-nil")
			}

			if route.Canonical != tt.wantCanon {
				t.Errorf("findRoute().Canonical = %q, want %q", route.Canonical, tt.wantCanon)
			}
		})
	}
}

func TestSelectBackend_SingleBackend(t *testing.T) {
	p := &Proxy{
		rrIndexes: make(map[string]uint32),
	}

	route := &Route{
		Canonical: "example.com",
		Backends:  []Backend{{IP: "10.0.0.1", Port: "8080"}},
	}

	// Call multiple times - should always return the same backend
	for i := 0; i < 5; i++ {
		backend := p.selectBackend(route)
		if backend.IP != "10.0.0.1" || backend.Port != "8080" {
			t.Errorf("selectBackend() = %v, want {10.0.0.1 8080}", backend)
		}
	}
}

func TestSelectBackend_RoundRobin(t *testing.T) {
	p := &Proxy{
		rrIndexes: make(map[string]uint32),
	}

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
		backend := p.selectBackend(route)
		if backend.IP != expectedIP {
			t.Errorf("selectBackend() call %d: got IP %s, want %s", i, backend.IP, expectedIP)
		}
	}
}

func TestSelectBackend_IndependentRoutes(t *testing.T) {
	p := &Proxy{
		rrIndexes: make(map[string]uint32),
	}

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
	b1 := p.selectBackend(route1)
	if b1.IP != "10.0.0.1" {
		t.Errorf("route1 first call: got %s, want 10.0.0.1", b1.IP)
	}

	// First call to route2 - should still start at index 0
	b2 := p.selectBackend(route2)
	if b2.IP != "10.0.1.1" {
		t.Errorf("route2 first call: got %s, want 10.0.1.1", b2.IP)
	}

	// Second call to route1 - should continue its own sequence
	b1 = p.selectBackend(route1)
	if b1.IP != "10.0.0.2" {
		t.Errorf("route1 second call: got %s, want 10.0.0.2", b1.IP)
	}

	// Second call to route2 - should continue its own sequence
	b2 = p.selectBackend(route2)
	if b2.IP != "10.0.1.2" {
		t.Errorf("route2 second call: got %s, want 10.0.1.2", b2.IP)
	}
}

func TestSelectBackend_Concurrent(t *testing.T) {
	p := &Proxy{
		rrIndexes: make(map[string]uint32),
	}

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
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			backend := p.selectBackend(route)
			results <- backend.IP
		}()
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
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	p := New(logger, nil, nil)

	if p == nil {
		t.Fatal("New() returned nil")
	}

	config := p.GetConfig()
	if config == nil {
		t.Fatal("GetConfig() returned nil")
	}

	if config.Routes == nil {
		t.Error("initial config.Routes is nil")
	}
}

func TestUpdateConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	p := New(logger, nil, nil)

	newConfig := &Config{
		Routes: map[string]*Route{
			"example.com": {Canonical: "example.com"},
		},
		APIDomain: "api.example.com",
	}

	p.UpdateConfig(newConfig)

	got := p.GetConfig()
	if got != newConfig {
		t.Error("GetConfig() did not return the updated config")
	}

	if len(got.Routes) != 1 {
		t.Errorf("len(Routes) = %d, want 1", len(got.Routes))
	}

	if got.APIDomain != "api.example.com" {
		t.Errorf("APIDomain = %q, want %q", got.APIDomain, "api.example.com")
	}
}
