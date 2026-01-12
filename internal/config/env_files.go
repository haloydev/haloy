package config

import (
	"fmt"
	"path/filepath"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/joho/godotenv"
)

func LoadEnvFiles() {
	// Load base .env files
	_ = godotenv.Load(constants.ConfigEnvFileName)
	if configDir, err := ConfigDir(); err == nil {
		_ = godotenv.Load(filepath.Join(configDir, constants.ConfigEnvFileName))
	}

	// Load .env.local files (overrides base values)
	_ = godotenv.Overload(constants.ConfigEnvLocalFileName)
	if configDir, err := ConfigDir(); err == nil {
		_ = godotenv.Overload(filepath.Join(configDir, constants.ConfigEnvLocalFileName))
	}
}

func LoadEnvFilesForTargets(targets []string) {
	for _, target := range targets {
		_ = godotenv.Load(fmt.Sprintf(".env.%s", target))
	}
}
