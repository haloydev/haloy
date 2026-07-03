package haloydcli

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
)

func TestAPIHealthCheckRequestDefaultsToLocalhost(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv(constants.EnvVarConfigDir, configDir)

	client, req, err := apiHealthCheckRequest()
	if err != nil {
		t.Fatalf("apiHealthCheckRequest() error = %v", err)
	}

	if req.URL.String() != "http://localhost/health" {
		t.Fatalf("request URL = %q, want http://localhost/health", req.URL.String())
	}
	if req.Method != http.MethodGet {
		t.Fatalf("method = %q, want GET", req.Method)
	}
	if client.Transport != nil {
		t.Fatalf("client.Transport = %T, want default transport", client.Transport)
	}
}

func TestAPIHealthCheckRequestUsesConfiguredAPIDomainViaLoopbackHTTPS(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		wantURL string
	}{
		{name: "domain", domain: "api.example.com", wantURL: "https://api.example.com/health"},
		{name: "uppercase domain", domain: "API.Example.COM", wantURL: "https://api.example.com/health"},
		{name: "scheme prefix stripped", domain: "https://api.example.com", wantURL: "https://api.example.com/health"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configDir := t.TempDir()
			t.Setenv(constants.EnvVarConfigDir, configDir)

			cfg := &config.HaloydConfig{
				API: config.HaloydAPIConfig{Domain: tt.domain},
			}
			if err := config.SaveHaloydConfig(cfg, filepath.Join(configDir, constants.HaloydConfigFileName)); err != nil {
				t.Fatalf("SaveHaloydConfig() error = %v", err)
			}

			client, req, err := apiHealthCheckRequest()
			if err != nil {
				t.Fatalf("apiHealthCheckRequest() error = %v", err)
			}

			if req.URL.String() != tt.wantURL {
				t.Fatalf("request URL = %q, want %q", req.URL.String(), tt.wantURL)
			}
			if _, ok := client.Transport.(*http.Transport); !ok {
				t.Fatalf("client.Transport = %T, want *http.Transport", client.Transport)
			}
		})
	}
}

func TestAPIHealthCheckRequestTreatsLocalDomainsAsLocalhost(t *testing.T) {
	tests := []struct {
		name   string
		domain string
	}{
		{name: "localhost", domain: "localhost"},
		{name: "uppercase localhost", domain: "LOCALHOST"},
		{name: "ipv4 loopback", domain: "127.0.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configDir := t.TempDir()
			t.Setenv(constants.EnvVarConfigDir, configDir)

			cfg := &config.HaloydConfig{
				API: config.HaloydAPIConfig{Domain: tt.domain},
			}
			if err := config.SaveHaloydConfig(cfg, filepath.Join(configDir, constants.HaloydConfigFileName)); err != nil {
				t.Fatalf("SaveHaloydConfig() error = %v", err)
			}

			client, req, err := apiHealthCheckRequest()
			if err != nil {
				t.Fatalf("apiHealthCheckRequest() error = %v", err)
			}

			if req.URL.Scheme != "http" {
				t.Fatalf("request URL = %q, want http scheme", req.URL.String())
			}
			if client.Transport != nil {
				t.Fatalf("client.Transport = %T, want default transport", client.Transport)
			}
		})
	}
}
