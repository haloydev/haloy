package config

import (
	"os"
	"path/filepath"
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

// DataDir returns the Haloy data directory.
// Uses HALOY_DATA_DIR env var if set, otherwise defaults to /var/lib/haloy.
func DataDir() (string, error) {
	if envPath, ok := os.LookupEnv(constants.EnvVarDataDir); ok && envPath != "" {
		return expandPath(envPath)
	}
	return constants.SystemDataDir, nil
}

// HaloydConfigDir returns the configuration directory for haloyd (daemon).
// Uses HALOY_CONFIG_DIR env var if set, otherwise defaults to /etc/haloy.
func HaloydConfigDir() (string, error) {
	if envPath, ok := os.LookupEnv(constants.EnvVarConfigDir); ok && envPath != "" {
		return expandPath(envPath)
	}
	return constants.DefaultHaloydConfigDir, nil
}

// HaloyConfigDir returns the configuration directory for haloy (client CLI).
// Uses HALOY_CONFIG_DIR env var if set, otherwise defaults to ~/.config/haloy.
func HaloyConfigDir() (string, error) {
	if envPath, ok := os.LookupEnv(constants.EnvVarConfigDir); ok && envPath != "" {
		return expandPath(envPath)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, constants.DefaultHaloyConfigDir), nil
}

// BinDir returns the directory where haloy binaries are installed.
// Defaults to /usr/local/bin.
func BinDir() (string, error) {
	return constants.SystemBinDir, nil
}
