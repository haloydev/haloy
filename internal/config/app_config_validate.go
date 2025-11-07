package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/haloydev/haloy/internal/helpers"
)

func (tc *TargetConfig) Validate(format string) error {
	if tc.Name == "" {
		return errors.New("app 'name' is required")
	}

	if tc.Server == "" {
		return errors.New("server is required")
	}

	if !isValidAppName(tc.Name) {
		return fmt.Errorf("invalid app name '%s'; must contain only alphanumeric characters, hyphens, and underscores", tc.Name)
	}

	if tc.Image != nil && tc.ImageKey != "" {
		return fmt.Errorf("cannot specify both 'image' and 'imageRef' in target config")
	}

	if tc.Image != nil {
		if err := tc.Image.Validate(format); err != nil {
			return fmt.Errorf("invalid image: %w", err)
		}
	}

	if tc.DeploymentStrategy != "" {
		validStrategies := []DeploymentStrategy{DeploymentStrategyRolling, DeploymentStrategyReplace}
		if !slices.Contains(validStrategies, tc.DeploymentStrategy) {
			return fmt.Errorf("deployment_strategy must be 'rolling' or 'replace', got '%s'", tc.DeploymentStrategy)
		}
	}

	if len(tc.Domains) > 0 {
		for _, domain := range tc.Domains {
			if err := domain.Validate(); err != nil {
				return err
			}
		}
	}

	if tc.ACMEEmail != "" && !helpers.IsValidEmail(tc.ACMEEmail) {
		return fmt.Errorf("%s is invalid '%s'; must be a valid email address", GetFieldNameForFormat(TargetConfig{}, "ACMEEmail", format), tc.ACMEEmail)
	}

	for j, envVar := range tc.Env {
		if err := envVar.Validate(format); err != nil {
			return fmt.Errorf("env[%d]: %w", j, err)
		}
	}

	for _, volume := range tc.Volumes {
		// Expected format: /host/path:/container/path[:options] or volume-name:/container/path[:options]
		parts := strings.Split(volume, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return fmt.Errorf("invalid volume mapping '%s'; expected 'host-path:/container/path[:options]'", volume)
		}

		hostPath := strings.TrimSpace(parts[0])
		if hostPath == "" {
			return fmt.Errorf("volume host path cannot be empty in '%s'", volume)
		}

		// Check if this is a filesystem bind mount (not a named volume)
		// Named volumes don't contain path separators and don't start with '.'
		if strings.Contains(hostPath, "/") || strings.HasPrefix(hostPath, ".") {
			// This appears to be a filesystem path, require it to be absolute
			if !filepath.IsAbs(hostPath) {
				return fmt.Errorf("volume host path '%s' in '%s' must be absolute when using filesystem bind mounts. Relative paths don't work when the daemon runs in a container", hostPath, volume)
			}
		}

		// Container path must be absolute
		containerPath := strings.TrimSpace(parts[1])
		if !filepath.IsAbs(containerPath) {
			return fmt.Errorf("volume container path '%s' in '%s' is not an absolute path", containerPath, volume)
		}
	}

	if tc.HealthCheckPath != "" {
		if tc.HealthCheckPath[0] != '/' {
			return fmt.Errorf("%s must start with a slash", GetFieldNameForFormat(TargetConfig{}, "HealthCheckPath", format))
		}
	}

	if tc.Replicas != nil {
		if int(*tc.Replicas) < 1 {
			return errors.New("replicas must be at least 1")
		}
	}

	return nil
}

func isValidAppName(name string) bool {
	// Only allow alphanumeric, hyphens, and underscores
	// Must start with alphanumeric character
	// This is to satisfy docker container name restrictions
	matched, err := regexp.MatchString(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`, name)
	if err != nil {
		return false
	}
	return matched
}
