package haloydcli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/embed"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

const (
	apiTokenLength = 32 // bytes, results in 64 character hex string
)

func newInitCmd() *cobra.Command {
	var override bool
	var apiDomain string
	var acmeEmail string
	var localInstall bool
	var noSystemd bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Haloy data directory and configuration",
		Long: fmt.Sprintf(`Initialize Haloy by creating the data directory structure and configuration.

Installation modes:
  Default (system): Uses system directories (/etc/haloy, /var/lib/haloy) when running as root
  --local-install:  Forces user directories (~/.config/haloy, ~/.local/share/haloy)

This command will:
  - Create the data directory (default: /var/lib/haloy)
  - Create the config directory for haloyd (default: /etc/haloy)
  - Generate an API token for authentication
  - Create the Docker network for containers
  - Install the systemd service (unless --no-systemd)

The data directory can be customized by setting the %s environment variable.`,
			constants.EnvVarDataDir),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if localInstall {
				os.Setenv(constants.EnvVarSystemInstall, "false")
			}

			if !config.IsSystemMode() {
				ui.Info("Installing in local mode (user directories)")
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

			dataDir, err := config.DataDir()
			if err != nil {
				return fmt.Errorf("failed to determine data directory: %w", err)
			}

			configDir, err := config.ConfigDir()
			if err != nil {
				return fmt.Errorf("failed to determine config directory: %w", err)
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

			// Copy error page files
			if err := copyEmbeddedDataFiles(dataDir); err != nil {
				return fmt.Errorf("failed to copy data files: %w", err)
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

			// Install systemd service if running as root and not disabled
			if !noSystemd && config.IsSystemMode() {
				ui.Info("\nInstalling systemd service...")
				if err := installSystemdService(); err != nil {
					ui.Warn("Failed to install systemd service: %v", err)
					ui.Info("You can start haloyd manually with: haloyd serve")
				} else {
					ui.Success("Systemd service installed!")
					ui.Info("Start with: systemctl start haloyd")
					ui.Info("Enable on boot: systemctl enable haloyd")
				}
			}

			ui.Info("\nYou can add this server to the haloy CLI with:")
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
	cmd.Flags().BoolVar(&localInstall, "local-install", false, "Install in user directories instead of system directories")
	cmd.Flags().BoolVar(&noSystemd, "no-systemd", false, "Don't install systemd service")

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

	// Write haloyd.yaml if domain is specified
	if domain != "" {
		haloydConfig := &config.HaloydConfig{}
		haloydConfig.API.Domain = domain
		haloydConfig.Certificates.AcmeEmail = acmeEmail

		if err := haloydConfig.Validate(); err != nil {
			return fmt.Errorf("invalid haloyd config: %w", err)
		}

		haloydConfigPath := filepath.Join(configDir, constants.HaloydConfigFileName)
		if err := config.SaveHaloydConfig(haloydConfig, haloydConfigPath); err != nil {
			return fmt.Errorf("failed to save haloyd config: %w", err)
		}
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

func copyEmbeddedDataFiles(dataDir string) error {
	return fs.WalkDir(embed.DataFS, "data", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel("data", path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dataDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, constants.ModeDirPrivate)
		}

		// Skip HAProxy-related files
		if strings.Contains(path, "haproxy") {
			return nil
		}

		data, err := embed.DataFS.ReadFile(path)
		if err != nil {
			return err
		}

		fileMode := constants.ModeFileDefault
		if filepath.Ext(targetPath) == ".sh" {
			fileMode = constants.ModeFileExec
		}

		return os.WriteFile(targetPath, data, fileMode)
	})
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

func installSystemdService() error {
	serviceContent := `[Unit]
Description=Haloy Daemon
After=network-online.target docker.service
Requires=docker.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/haloyd serve
Restart=always
RestartSec=5
Environment=HALOY_DATA_DIR=/var/lib/haloy
Environment=HALOY_CONFIG_DIR=/etc/haloy

[Install]
WantedBy=multi-user.target
`

	servicePath := "/etc/systemd/system/haloyd.service"
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	// Reload systemd
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	return nil
}
