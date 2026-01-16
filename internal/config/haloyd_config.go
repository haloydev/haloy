package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"gopkg.in/yaml.v3"
)

type HaloydConfig struct {
	API struct {
		Domain string `json:"domain" yaml:"domain" toml:"domain"`
	} `json:"api" yaml:"api" toml:"api"`
	Certificates struct {
		AcmeEmail string `json:"acmeEmail" yaml:"acme_email" toml:"acme_email"`
	} `json:"certificates" yaml:"certificates" toml:"certificates"`
	HealthMonitor HealthMonitorConfig `json:"health_monitor" yaml:"health_monitor" toml:"health_monitor"`
}

// HealthMonitorConfig holds configuration for continuous health monitoring.
type HealthMonitorConfig struct {
	Enabled  *bool  `json:"enabled" yaml:"enabled" toml:"enabled"`    // nil means enabled (default)
	Interval string `json:"interval" yaml:"interval" toml:"interval"` // e.g., "15s"
	Fall     int    `json:"fall" yaml:"fall" toml:"fall"`             // Mark unhealthy after N failures
	Rise     int    `json:"rise" yaml:"rise" toml:"rise"`             // Mark healthy after N successes
	Timeout  string `json:"timeout" yaml:"timeout" toml:"timeout"`    // Per-check timeout, e.g., "5s"
}

// IsEnabled returns whether health monitoring is enabled.
// Defaults to true if not explicitly set.
func (c *HealthMonitorConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true // Enabled by default
	}
	return *c.Enabled
}

// GetInterval parses the interval string and returns the duration.
// Returns the default of 15s if not set or invalid.
func (c *HealthMonitorConfig) GetInterval() time.Duration {
	if c.Interval == "" {
		return 15 * time.Second
	}
	d, err := time.ParseDuration(c.Interval)
	if err != nil {
		return 15 * time.Second
	}
	return d
}

// GetTimeout parses the timeout string and returns the duration.
// Returns the default of 5s if not set or invalid.
func (c *HealthMonitorConfig) GetTimeout() time.Duration {
	if c.Timeout == "" {
		return 5 * time.Second
	}
	d, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return 5 * time.Second
	}
	return d
}

// GetFall returns the fall threshold, defaulting to 3 if not set.
func (c *HealthMonitorConfig) GetFall() int {
	if c.Fall <= 0 {
		return 3
	}
	return c.Fall
}

// GetRise returns the rise threshold, defaulting to 2 if not set.
func (c *HealthMonitorConfig) GetRise() int {
	if c.Rise <= 0 {
		return 2
	}
	return c.Rise
}

// Normalize sets default values for HaloydConfig
func (mc *HaloydConfig) Normalize() *HaloydConfig {
	// Add any defaults if needed in the future
	return mc
}

func (mc *HaloydConfig) Validate() error {
	if mc.API.Domain != "" {
		if err := helpers.IsValidDomain(mc.API.Domain); err != nil {
			return fmt.Errorf("invalid domain format: %w", err)
		}
	}

	if mc.Certificates.AcmeEmail != "" && !helpers.IsValidEmail(mc.Certificates.AcmeEmail) {
		return fmt.Errorf("invalid acme-email format: %s", mc.Certificates.AcmeEmail)
	}

	if mc.API.Domain != "" && mc.Certificates.AcmeEmail == "" {
		return fmt.Errorf("acmeEmail is required when domain is specified")
	}

	return nil
}

func LoadHaloydConfig(path string) (*HaloydConfig, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	format, err := GetConfigFormat(path)
	if err != nil {
		return nil, err
	}

	parser, err := GetConfigParser(format)
	if err != nil {
		return nil, err
	}

	k := koanf.New(".")
	if err := k.Load(file.Provider(path), parser); err != nil {
		return nil, fmt.Errorf("failed to load haloyd config file: %w", err)
	}

	var haloydConfig HaloydConfig
	if err := k.UnmarshalWithConf("", &haloydConfig, koanf.UnmarshalConf{Tag: format}); err != nil {
		return nil, fmt.Errorf("failed to unmarshal haloyd config: %w", err)
	}
	return &haloydConfig, nil
}

func SaveHaloydConfig(config *HaloydConfig, path string) error {
	ext := filepath.Ext(path)
	var data []byte
	var err error

	switch ext {
	case ".json":
		data, err = json.MarshalIndent(config, "", "  ")
	default: // yaml
		data, err = yaml.Marshal(config)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(path, data, constants.ModeFileDefault)
}
