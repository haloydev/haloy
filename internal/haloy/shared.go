package haloy

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/oklog/ulid"
)

func createDeploymentID() string {
	entropy := ulid.Monotonic(rand.New(rand.NewSource(time.Now().UnixNano())), 0)
	id := ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
	return strings.ToLower(id)
}

func getToken(targetConfig *config.TargetConfig, url string) (string, error) {
	if targetConfig != nil && targetConfig.APIToken != nil && targetConfig.APIToken.Value != "" {
		return targetConfig.APIToken.Value, nil
	}

	configDir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	clientConfigPath := filepath.Join(configDir, constants.ClientConfigFileName)
	clientConfig, err := config.LoadClientConfig(clientConfigPath)
	if err != nil {
		return "", err
	}

	if clientConfig == nil {
		return "", fmt.Errorf("no client configuration found. Run: haloy server add <url> <token>")
	}

	normalizedURL, err := helpers.NormalizeServerURL(url)
	if err != nil {
		return "", err
	}

	serverConfig, exists := clientConfig.Servers[normalizedURL]
	if !exists {
		return "", fmt.Errorf("server %s not configured. Run: haloy server add %s <token>", normalizedURL, normalizedURL)
	}

	token := os.Getenv(serverConfig.TokenEnv)
	if token == "" {
		return "", fmt.Errorf("token not found for server %s. Please set environment variable: %s", normalizedURL, serverConfig.TokenEnv)
	}

	return token, nil
}
