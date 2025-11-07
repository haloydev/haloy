package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/haloydev/haloy/internal/constants"
)

// expandPath handles tilde expansion for paths
func expandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[2:])
	}
	return path, nil
}

// DataDir returns the Haloy data directory
// Should work for code run in containers and host filesystem.
// System install: /var/lib/haloy
// User install: ~/.local/share/haloy
func DataDir() (string, error) {
	if envPath, ok := os.LookupEnv(constants.EnvVarDataDir); ok && envPath != "" {
		return expandPath(envPath)
	}

	if !IsSystemMode() {
		return expandPath(constants.UserDataDir)
	}

	return constants.SystemDataDir, nil
}

// ConfigDir returns the configuration directory for haloy/haloyadm
// System mode: /etc/haloy
// User mode: ~/.config/haloy
func ConfigDir() (string, error) {
	// Environment variable override takes priority
	if envPath, ok := os.LookupEnv(constants.EnvVarConfigDir); ok && envPath != "" {
		expandedPath, err := expandPath(envPath)
		if err != nil {
			return "", err
		}
		return expandedPath, nil
	}

	// System mode detection (haloyadm)
	if IsSystemMode() {
		return constants.SystemConfigDir, nil
	}

	// User mode fallback
	expandedPath, err := expandPath(constants.UserConfigDir)
	if err != nil {
		return "", err
	}
	return expandedPath, nil
}

func IsSystemMode() bool {
	// Check explicit override first
	if systemInstall := os.Getenv(constants.EnvVarSystemInstall); systemInstall != "" {
		return systemInstall == "true"
	}

	// On Windows, default to user mode (system mode requires explicit setting)
	if runtime.GOOS == "windows" {
		return false
	}

	// Default to true (system mode) unless running as non-root user
	return os.Geteuid() == 0
}
