package haloydcli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

const (
	apiTokenLength = 32 // bytes, results in 64 character hex string
)

func initCmd() *cobra.Command {
	var override bool
	var apiDomain string
	var acmeEmail string
	var dataDirFlag string
	var configDirFlag string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Haloy data directory and configuration",
		Long: `Initialize Haloy by creating the data directory structure and configuration.

This command will:
  - Create the data directory (default: /var/lib/haloy)
  - Create the config directory (default: /etc/haloy)
  - Generate an API token for authentication
  - Create the Docker network for containers

Use --data-dir and --config-dir to specify custom directories.
Environment variables HALOY_DATA_DIR and HALOY_CONFIG_DIR can also be used.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Determine directories - flags take priority, then env vars, then defaults
			dataDir := dataDirFlag
			if dataDir == "" {
				var err error
				dataDir, err = config.DataDir()
				if err != nil {
					return fmt.Errorf("failed to determine data directory: %w", err)
				}
			}

			configDir := configDirFlag
			if configDir == "" {
				var err error
				configDir, err = config.ConfigDir()
				if err != nil {
					return fmt.Errorf("failed to determine config directory: %w", err)
				}
			}

			var createdDirs []string
			var cleanupOnFailure bool = true

			defer func() {
				if cleanupOnFailure && len(createdDirs) > 0 {
					cleanupDirectories(createdDirs)
				}
			}()

			// Check if Docker is installed and available
			if _, err := exec.LookPath("docker"); err != nil {
				return fmt.Errorf("docker executable not found: %w\nPlease ensure Docker is installed and in your PATH.\nDownload from: https://www.docker.com/get-started", err)
			}

			// Check if Docker daemon is running
			dockerCheck := exec.CommandContext(ctx, "docker", "info")
			if err := dockerCheck.Run(); err != nil {
				return fmt.Errorf("Docker daemon is not running. Please start Docker and try again.")
			}

			if err := validateAndPrepareDirectory(configDir, "Config", override); err != nil {
				return err
			}
			createdDirs = append(createdDirs, configDir)

			if err := validateAndPrepareDirectory(dataDir, "Data", override); err != nil {
				return err
			}
			createdDirs = append(createdDirs, dataDir)

			apiToken, err := generateAPIToken()
			if err != nil {
				return fmt.Errorf("failed to generate API token: %w", err)
			}

			if err := createConfigFiles(apiToken, apiDomain, acmeEmail, configDir); err != nil {
				return fmt.Errorf("failed to create config files: %w", err)
			}

			// Create required subdirectories
			subdirs := []string{
				constants.DBDir,
				constants.CertStorageDir,
			}
			if err := createSubdirectories(dataDir, subdirs); err != nil {
				return fmt.Errorf("failed to create subdirectories: %w", err)
			}

			// Create Docker network
			if err := ensureDockerNetwork(ctx); err != nil {
				ui.Info("You can manually create it with:")
				ui.Info("docker network create --driver bridge --attachable %s", constants.DockerNetwork)
				return fmt.Errorf("failed to create Docker network: %w", err)
			}

			cleanupOnFailure = false

			ui.Success("Haloy initialized successfully!\n")
			ui.Info("Data directory: %s", dataDir)
			ui.Info("Config directory: %s", configDir)
			if apiDomain != "" {
				ui.Info("API domain: %s", apiDomain)
			}

			ui.Info("\nAPI Token: %s", apiToken)
			ui.Info("\nAdd this server to the haloy CLI with:")
			apiDomainMsg := "<server-url>"
			if apiDomain != "" {
				apiDomainMsg = apiDomain
			}
			ui.Info("  haloy server add %s %s", apiDomainMsg, apiToken)

			return nil
		},
	}

	cmd.Flags().BoolVar(&override, "override", false, "Remove and recreate existing directories")
	cmd.Flags().StringVar(&apiDomain, "api-domain", "", "Domain for the haloyd API (e.g., api.yourserver.com)")
	cmd.Flags().StringVar(&acmeEmail, "acme-email", "", "Email address for Let's Encrypt certificate registration")
	cmd.Flags().StringVar(&dataDirFlag, "data-dir", "", "Data directory path (default: /var/lib/haloy)")
	cmd.Flags().StringVar(&configDirFlag, "config-dir", "", "Config directory path (default: /etc/haloy)")

	return cmd
}

func generateAPIToken() (string, error) {
	bytes := make([]byte, apiTokenLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func createConfigFiles(apiToken, domain, acmeEmail, configDir string) error {
	if apiToken == "" {
		return fmt.Errorf("apiToken cannot be empty")
	}

	// Write .env file with API token
	envPath := filepath.Join(configDir, constants.ConfigEnvFileName)
	env := map[string]string{
		constants.EnvVarAPIToken: apiToken,
	}
	if err := godotenv.Write(env, envPath); err != nil {
		return fmt.Errorf("failed to write %s: %w", constants.ConfigEnvFileName, err)
	}
	if err := os.Chmod(envPath, constants.ModeFileSecret); err != nil {
		return fmt.Errorf("failed to set %s permissions: %w", constants.ConfigEnvFileName, err)
	}

	// Always write haloyd.yaml with defaults
	haloydConfig := &config.HaloydConfig{}
	haloydConfig.API.Domain = domain                // may be empty
	haloydConfig.Certificates.AcmeEmail = acmeEmail // may be empty
	haloydConfig.HealthMonitor = config.HealthMonitorConfig{
		// Enabled is nil by default, which means enabled (see IsEnabled())
		Interval: "15s",
		Fall:     3,
		Rise:     2,
		Timeout:  "5s",
	}

	if err := haloydConfig.Validate(); err != nil {
		return fmt.Errorf("invalid haloyd config: %w", err)
	}

	haloydConfigPath := filepath.Join(configDir, constants.HaloydConfigFileName)
	if err := config.SaveHaloydConfig(haloydConfig, haloydConfigPath); err != nil {
		return fmt.Errorf("failed to save haloyd config: %w", err)
	}

	return nil
}

func createSubdirectories(dataDir string, subdirs []string) error {
	for _, subdir := range subdirs {
		path := filepath.Join(dataDir, subdir)
		if err := os.MkdirAll(path, constants.ModeDirPrivate); err != nil {
			return fmt.Errorf("failed to create %s: %w", path, err)
		}
	}
	return nil
}

func ensureDockerNetwork(ctx interface{ Done() <-chan struct{} }) error {
	// Check if network exists
	checkCmd := exec.Command("docker", "network", "inspect", constants.DockerNetwork)
	if err := checkCmd.Run(); err == nil {
		return nil // Network exists
	}

	// Create network
	createCmd := exec.Command("docker", "network", "create",
		"--driver", "bridge",
		"--attachable",
		constants.DockerNetwork)

	if output, err := createCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create network: %w\nOutput: %s", err, output)
	}

	return nil
}

func validateAndPrepareDirectory(dirPath, dirType string, override bool) error {
	info, err := os.Stat(dirPath)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s path exists but is not a directory: %s", dirType, dirPath)
		}
		if !override {
			return fmt.Errorf("%s directory already exists: %s\nUse --override to recreate", dirType, dirPath)
		}
		ui.Info("Removing existing %s directory: %s", strings.ToLower(dirType), dirPath)
		if err := os.RemoveAll(dirPath); err != nil {
			return fmt.Errorf("failed to remove existing %s directory: %w", dirType, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to access %s directory: %w", dirType, err)
	}

	if err := os.MkdirAll(dirPath, constants.ModeDirPrivate); err != nil {
		return fmt.Errorf("failed to create %s directory: %w", dirType, err)
	}

	return nil
}

func cleanupDirectories(dirs []string) {
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			ui.Warn("Failed to cleanup %s: %v", dir, err)
		}
	}
}
