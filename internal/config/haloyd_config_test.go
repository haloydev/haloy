package config

import (
	"os"
	"path/filepath"
	"testing"

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
			name: "valid config with domain and email",
			config: HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: "api.example.com"},
				Certificates: struct {
					AcmeEmail string `json:"acmeEmail" yaml:"acme_email" toml:"acme_email"`
				}{AcmeEmail: "admin@example.com"},
			},
			wantErr: false,
		},
		{
			name: "valid config with only email",
			config: HaloydConfig{
				Certificates: struct {
					AcmeEmail string `json:"acmeEmail" yaml:"acme_email" toml:"acme_email"`
				}{AcmeEmail: "admin@example.com"},
			},
			wantErr: false,
		},
		{
			name: "invalid domain format",
			config: HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: "invalid domain"},
				Certificates: struct {
					AcmeEmail string `json:"acmeEmail" yaml:"acme_email" toml:"acme_email"`
				}{AcmeEmail: "admin@example.com"},
			},
			wantErr: true,
			errMsg:  "invalid domain format",
		},
		{
			name: "invalid email format",
			config: HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: "api.example.com"},
				Certificates: struct {
					AcmeEmail string `json:"acmeEmail" yaml:"acme_email" toml:"acme_email"`
				}{AcmeEmail: "not-an-email"},
			},
			wantErr: true,
			errMsg:  "invalid acme-email format",
		},
		{
			name: "domain without email",
			config: HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: "api.example.com"},
			},
			wantErr: true,
			errMsg:  "acmeEmail is required when domain is specified",
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
				Certificates: struct {
					AcmeEmail string `json:"acmeEmail" yaml:"acme_email" toml:"acme_email"`
				}{AcmeEmail: "admin@example.com"},
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
certificates:
  acme_email: admin@example.com
`,
			extension: ".yaml",
			expected: &HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: "api.example.com"},
				Certificates: struct {
					AcmeEmail string `json:"acmeEmail" yaml:"acme_email" toml:"acme_email"`
				}{AcmeEmail: "admin@example.com"},
			},
		},
		{
			name: "load valid json config",
			content: `{
  "api": {
    "domain": "api.example.com"
  },
  "certificates": {
    "acmeEmail": "admin@example.com"
  }
}`,
			extension: ".json",
			expected: &HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: "api.example.com"},
				Certificates: struct {
					AcmeEmail string `json:"acmeEmail" yaml:"acme_email" toml:"acme_email"`
				}{AcmeEmail: "admin@example.com"},
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
certificates:
  acme_email: ""
`,
			extension: ".yaml",
			expected: &HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: ""},
				Certificates: struct {
					AcmeEmail string `json:"acmeEmail" yaml:"acme_email" toml:"acme_email"`
				}{AcmeEmail: ""},
			},
		},
		{
			name: "invalid yaml format",
			content: `api:
  domain: api.example.com
    invalid_indent: value
certificates:
  acme_email: admin@example.com
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
						if result.Certificates.AcmeEmail != tt.expected.Certificates.AcmeEmail {
							t.Errorf("LoadHaloydConfig() Certificates.AcmeEmail = %s, expected %s",
								result.Certificates.AcmeEmail, tt.expected.Certificates.AcmeEmail)
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
				Certificates: struct {
					AcmeEmail string `json:"acmeEmail" yaml:"acme_email" toml:"acme_email"`
				}{AcmeEmail: "admin@example.com"},
			},
			extension: ".yaml",
		},
		{
			name: "save json config",
			config: HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: "api.example.com"},
				Certificates: struct {
					AcmeEmail string `json:"acmeEmail" yaml:"acme_email" toml:"acme_email"`
				}{AcmeEmail: "admin@example.com"},
			},
			extension: ".json",
		},
		{
			name:      "save empty config",
			config:    HaloydConfig{},
			extension: ".yaml",
		},
		{
			name: "save config with only domain",
			config: HaloydConfig{
				API: struct {
					Domain string `json:"domain" yaml:"domain" toml:"domain"`
				}{Domain: "api.example.com"},
			},
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
				if loaded.Certificates.AcmeEmail != tt.config.Certificates.AcmeEmail {
					t.Errorf("SaveHaloydConfig() loaded Certificates.AcmeEmail = %s, expected %s",
						loaded.Certificates.AcmeEmail, tt.config.Certificates.AcmeEmail)
				}
			}
		})
	}
}
