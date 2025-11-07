package config

import (
	"testing"

	"github.com/haloydev/haloy/internal/helpers"
)

func TestClientConfig_AddServer(t *testing.T) {
	tests := []struct {
		name        string
		initial     ClientConfig
		url         string
		tokenEnv    string
		force       bool
		expectError bool
		errMsg      string
		expected    map[string]ServerConfig
	}{
		{
			name:     "add new server to empty config",
			initial:  ClientConfig{},
			url:      "https://api.example.com",
			tokenEnv: "API_TOKEN",
			force:    false,
			expected: map[string]ServerConfig{
				"api.example.com": {TokenEnv: "API_TOKEN"},
			},
		},
		{
			name: "add new server to existing config",
			initial: ClientConfig{
				Servers: map[string]ServerConfig{
					"existing.com": {TokenEnv: "EXISTING_TOKEN"},
				},
			},
			url:      "new.example.com",
			tokenEnv: "NEW_TOKEN",
			force:    false,
			expected: map[string]ServerConfig{
				"existing.com":    {TokenEnv: "EXISTING_TOKEN"},
				"new.example.com": {TokenEnv: "NEW_TOKEN"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.initial
			err := config.AddServer(tt.url, tt.tokenEnv, tt.force)

			if tt.expectError {
				if err == nil {
					t.Errorf("AddServer() expected error but got none")
				} else if tt.errMsg != "" && !helpers.Contains(err.Error(), tt.errMsg) {
					t.Errorf("AddServer() error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("AddServer() unexpected error = %v", err)
				}
				if len(config.Servers) != len(tt.expected) {
					t.Errorf("AddServer() got %d servers, expected %d", len(config.Servers), len(tt.expected))
				}
				for url, server := range tt.expected {
					if actual, exists := config.Servers[url]; !exists {
						t.Errorf("AddServer() expected server %s not found", url)
					} else if actual.TokenEnv != server.TokenEnv {
						t.Errorf("AddServer() server %s TokenEnv = %s, expected %s", url, actual.TokenEnv, server.TokenEnv)
					}
				}
			}
		})
	}
}

func TestClientConfig_ListServers(t *testing.T) {
	tests := []struct {
		name     string
		config   ClientConfig
		expected []string
	}{
		{
			name:     "empty config",
			config:   ClientConfig{},
			expected: []string{},
		},
		{
			name: "single server",
			config: ClientConfig{
				Servers: map[string]ServerConfig{
					"https://api.example.com": {TokenEnv: "API_TOKEN"},
				},
			},
			expected: []string{"https://api.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.ListServers()
			if len(result) != len(tt.expected) {
				t.Errorf("ListServers() got %d servers, expected %d", len(result), len(tt.expected))
			}
			for i, server := range tt.expected {
				if i < len(result) && result[i] != server {
					t.Errorf("ListServers() [%d] = %s, expected %s", i, result[i], server)
				}
			}
		})
	}
}
