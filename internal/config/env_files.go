package config

import (
	"fmt"
	"path/filepath"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/joho/godotenv"
)

func LoadEnvFiles() {
	_ = godotenv.Load(constants.ConfigEnvFileName)

	if configDir, err := ConfigDir(); err == nil {
		configEnvPath := filepath.Join(configDir, constants.ConfigEnvFileName)
		_ = godotenv.Load(configEnvPath)
	}
}

func LoadEnvFilesForTargets(targets []string) {
	for _, target := range targets {
		_ = godotenv.Load(fmt.Sprintf(".env.%s", target))
	}
}
