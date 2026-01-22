package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/haloydev/haloy/internal/helpers"
)

func TestHaloydConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  HaloydConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid empty config",
			config:  HaloydConfig{},
			wantErr: false,
		},
		{
			name: "valid config with domain",
			config: HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: "api.example.com"},
			},
			wantErr: false,
		},
		{
			name: "invalid domain format",
			config: HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: "invalid domain"},
			},
			wantErr: true,
			errMsg:  "invalid domain format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error but got none")
				} else if tt.errMsg != "" && !helpers.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, expected to contain %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestHaloydConfig_Normalize(t *testing.T) {
	tests := []struct {
		name   string
		config HaloydConfig
	}{
		{
			name:   "empty config",
			config: HaloydConfig{},
		},
		{
			name: "config with values",
			config: HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: "api.example.com"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.Normalize()
			if result != &tt.config {
				t.Errorf("Normalize() should return the same config instance")
			}
			// Currently Normalize() doesn't modify anything, but test is here for future changes
		})
	}
}

func TestLoadHaloydConfig(t *testing.T) {
	// Create temporary directory for test files
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		content     string
		extension   string
		expectError bool
		expected    *HaloydConfig
	}{
		{
			name: "load valid yaml config",
			content: `api:
  domain: api.example.com
`,
			extension: ".yaml",
			expected: &HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: "api.example.com"},
			},
		},
		{
			name: "load valid json config",
			content: `{
  "api": {
    "domain": "api.example.com"
  }
}`,
			extension: ".json",
			expected: &HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: "api.example.com"},
			},
		},
		{
			name:        "non-existent file returns nil",
			content:     "",
			extension:   ".yaml",
			expectError: false,
			expected:    nil,
		},
		{
			name: "load minimal config",
			content: `api:
  domain: ""
`,
			extension: ".yaml",
			expected: &HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: ""},
			},
		},
		{
			name: "invalid yaml format",
			content: `api:
  domain: api.example.com
    invalid_indent: value
`,
			extension:   ".yaml",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var path string
			if tt.content != "" {
				path = filepath.Join(tempDir, "haloyd"+tt.extension)
				err := os.WriteFile(path, []byte(tt.content), 0o644)
				if err != nil {
					t.Fatalf("Failed to create test file: %v", err)
				}
			} else {
				path = filepath.Join(tempDir, "nonexistent.yaml")
			}

			result, err := LoadHaloydConfig(path)

			if tt.expectError {
				if err == nil {
					t.Errorf("LoadHaloydConfig() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("LoadHaloydConfig() unexpected error = %v", err)
				}
				if tt.expected == nil {
					if result != nil {
						t.Errorf("LoadHaloydConfig() expected nil result, got %v", result)
					}
				} else {
					if result == nil {
						t.Errorf("LoadHaloydConfig() expected non-nil result, got nil")
					} else {
						if result.API.Domain != tt.expected.API.Domain {
							t.Errorf("LoadHaloydConfig() API.Domain = %s, expected %s",
								result.API.Domain, tt.expected.API.Domain)
						}
					}
				}
			}
		})
	}
}

func TestSaveHaloydConfig(t *testing.T) {
	// Create temporary directory for test files
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		config      HaloydConfig
		extension   string
		expectError bool
	}{
		{
			name: "save yaml config",
			config: HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: "api.example.com"},
			},
			extension: ".yaml",
		},
		{
			name: "save json config",
			config: HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: "api.example.com"},
			},
			extension: ".json",
		},
		{
			name:      "save empty config",
			config:    HaloydConfig{},
			extension: ".yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(tempDir, "haloyd"+tt.extension)
			err := SaveHaloydConfig(&tt.config, path)

			if tt.expectError {
				if err == nil {
					t.Errorf("SaveHaloydConfig() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("SaveHaloydConfig() unexpected error = %v", err)
				}

				// Verify the file was created and can be loaded back
				loaded, err := LoadHaloydConfig(path)
				if err != nil {
					t.Errorf("SaveHaloydConfig() file could not be loaded back: %v", err)
				}
				if loaded.API.Domain != tt.config.API.Domain {
					t.Errorf("SaveHaloydConfig() loaded API.Domain = %s, expected %s",
						loaded.API.Domain, tt.config.API.Domain)
				}
			}
		})
	}
}

func TestHealthMonitorConfig_IsEnabled(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name     string
		config   HealthMonitorConfig
		expected bool
	}{
		{
			name:     "nil Enabled defaults to true",
			config:   HealthMonitorConfig{Enabled: nil},
			expected: true,
		},
		{
			name:     "explicit true",
			config:   HealthMonitorConfig{Enabled: &trueVal},
			expected: true,
		},
		{
			name:     "explicit false",
			config:   HealthMonitorConfig{Enabled: &falseVal},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.IsEnabled()
			if result != tt.expected {
				t.Errorf("IsEnabled() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestHealthMonitorConfig_GetMethods(t *testing.T) {
	t.Run("GetInterval", func(t *testing.T) {
		// Empty string returns default
		config := HealthMonitorConfig{Interval: ""}
		if got := config.GetInterval(); got != 15*time.Second {
			t.Errorf("GetInterval() = %v, expected %v", got, 15*time.Second)
		}

		// Valid string returns parsed value
		config = HealthMonitorConfig{Interval: "30s"}
		if got := config.GetInterval(); got != 30*time.Second {
			t.Errorf("GetInterval() = %v, expected %v", got, 30*time.Second)
		}

		// Invalid string returns default
		config = HealthMonitorConfig{Interval: "invalid"}
		if got := config.GetInterval(); got != 15*time.Second {
			t.Errorf("GetInterval() = %v, expected %v", got, 15*time.Second)
		}
	})

	t.Run("GetTimeout", func(t *testing.T) {
		// Empty string returns default
		config := HealthMonitorConfig{Timeout: ""}
		if got := config.GetTimeout(); got != 5*time.Second {
			t.Errorf("GetTimeout() = %v, expected %v", got, 5*time.Second)
		}

		// Valid string returns parsed value
		config = HealthMonitorConfig{Timeout: "10s"}
		if got := config.GetTimeout(); got != 10*time.Second {
			t.Errorf("GetTimeout() = %v, expected %v", got, 10*time.Second)
		}
	})

	t.Run("GetFall", func(t *testing.T) {
		// Zero returns default
		config := HealthMonitorConfig{Fall: 0}
		if got := config.GetFall(); got != 3 {
			t.Errorf("GetFall() = %v, expected %v", got, 3)
		}

		// Negative returns default
		config = HealthMonitorConfig{Fall: -1}
		if got := config.GetFall(); got != 3 {
			t.Errorf("GetFall() = %v, expected %v", got, 3)
		}

		// Positive returns value
		config = HealthMonitorConfig{Fall: 5}
		if got := config.GetFall(); got != 5 {
			t.Errorf("GetFall() = %v, expected %v", got, 5)
		}
	})

	t.Run("GetRise", func(t *testing.T) {
		// Zero returns default
		config := HealthMonitorConfig{Rise: 0}
		if got := config.GetRise(); got != 2 {
			t.Errorf("GetRise() = %v, expected %v", got, 2)
		}

		// Positive returns value
		config = HealthMonitorConfig{Rise: 4}
		if got := config.GetRise(); got != 4 {
			t.Errorf("GetRise() = %v, expected %v", got, 4)
		}
	})
}

func TestLoadHaloydConfig_HealthMonitor(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("health_monitor not specified defaults to enabled", func(t *testing.T) {
		content := `api:
  domain: api.example.com
`
		path := filepath.Join(tempDir, "no_health_monitor.yaml")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		config, err := LoadHaloydConfig(path)
		if err != nil {
			t.Fatalf("LoadHaloydConfig() error = %v", err)
		}

		// Enabled should be nil (not specified), IsEnabled() should return true
		if config.HealthMonitor.Enabled != nil {
			t.Errorf("Expected Enabled to be nil, got %v", *config.HealthMonitor.Enabled)
		}
		if !config.HealthMonitor.IsEnabled() {
			t.Error("IsEnabled() should return true when not specified")
		}
	})

	t.Run("health_monitor.enabled explicitly false", func(t *testing.T) {
		content := `health_monitor:
  enabled: false
`
		path := filepath.Join(tempDir, "disabled.yaml")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		config, err := LoadHaloydConfig(path)
		if err != nil {
			t.Fatalf("LoadHaloydConfig() error = %v", err)
		}

		if config.HealthMonitor.Enabled == nil {
			t.Error("Expected Enabled to be non-nil")
		} else if *config.HealthMonitor.Enabled != false {
			t.Errorf("Expected Enabled to be false, got %v", *config.HealthMonitor.Enabled)
		}
		if config.HealthMonitor.IsEnabled() {
			t.Error("IsEnabled() should return false when explicitly disabled")
		}
	})

	t.Run("health_monitor.enabled explicitly true", func(t *testing.T) {
		content := `health_monitor:
  enabled: true
  interval: "30s"
  fall: 5
  rise: 3
  timeout: "10s"
`
		path := filepath.Join(tempDir, "enabled.yaml")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		config, err := LoadHaloydConfig(path)
		if err != nil {
			t.Fatalf("LoadHaloydConfig() error = %v", err)
		}

		if !config.HealthMonitor.IsEnabled() {
			t.Error("IsEnabled() should return true when explicitly enabled")
		}
		if config.HealthMonitor.GetInterval() != 30*time.Second {
			t.Errorf("GetInterval() = %v, expected 30s", config.HealthMonitor.GetInterval())
		}
		if config.HealthMonitor.GetFall() != 5 {
			t.Errorf("GetFall() = %v, expected 5", config.HealthMonitor.GetFall())
		}
		if config.HealthMonitor.GetRise() != 3 {
			t.Errorf("GetRise() = %v, expected 3", config.HealthMonitor.GetRise())
		}
		if config.HealthMonitor.GetTimeout() != 10*time.Second {
			t.Errorf("GetTimeout() = %v, expected 10s", config.HealthMonitor.GetTimeout())
		}
	})
}
