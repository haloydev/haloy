package config

import (
	"fmt"
	"path/filepath"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/joho/godotenv"
)

// LoadEnvFiles attempts to load .env files from various locations
// Does not return an error - just tries to load what it can find
func LoadEnvFiles(targets []string) {
	_ = godotenv.Load(constants.ConfigEnvFileName)
	for _, target := range targets {
		_ = godotenv.Load(fmt.Sprintf(".env.%s", target))
	}

	if configDir, err := ConfigDir(); err == nil {
		configEnvPath := filepath.Join(configDir, constants.ConfigEnvFileName)
		_ = godotenv.Load(configEnvPath)
	}
}
