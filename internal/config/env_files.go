package config

import (
	"fmt"
	"path/filepath"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/joho/godotenv"
)

// LoadHaloydEnvFiles loads environment files for haloyd (daemon).
// Loads from current directory and /etc/haloy.
func LoadHaloydEnvFiles() {
	// Load base .env files
	_ = godotenv.Load(constants.ConfigEnvFileName)
	if configDir, err := HaloydConfigDir(); err == nil {
		_ = godotenv.Load(filepath.Join(configDir, constants.ConfigEnvFileName))
	}

	// Load .env.local files (overrides base values)
	_ = godotenv.Overload(constants.ConfigEnvLocalFileName)
	if configDir, err := HaloydConfigDir(); err == nil {
		_ = godotenv.Overload(filepath.Join(configDir, constants.ConfigEnvLocalFileName))
	}
}

// LoadHaloyEnvFiles loads environment files for haloy (client CLI).
// Loads from current directory and ~/.config/haloy.
func LoadHaloyEnvFiles() {
	// Load base .env files
	_ = godotenv.Load(constants.ConfigEnvFileName)
	if configDir, err := HaloyConfigDir(); err == nil {
		_ = godotenv.Load(filepath.Join(configDir, constants.ConfigEnvFileName))
	}

	// Load .env.local files (overrides base values)
	_ = godotenv.Overload(constants.ConfigEnvLocalFileName)
	if configDir, err := HaloyConfigDir(); err == nil {
		_ = godotenv.Overload(filepath.Join(configDir, constants.ConfigEnvLocalFileName))
	}
}

func LoadEnvFilesForTargets(targets []string) {
	for _, target := range targets {
		_ = godotenv.Load(fmt.Sprintf(".env.%s", target))
	}
}

// LoadEnvFilesFromDir loads .env and .env.local files from the specified directory.
// This is used to load env files from the config file's directory.
func LoadEnvFilesFromDir(dir string) {
	if dir == "" || dir == "." {
		return // Already loaded from current directory in LoadHaloyEnvFiles
	}

	// Load base .env file
	_ = godotenv.Load(filepath.Join(dir, constants.ConfigEnvFileName))

	// Load .env.local file (overrides base values)
	_ = godotenv.Overload(filepath.Join(dir, constants.ConfigEnvLocalFileName))
}
