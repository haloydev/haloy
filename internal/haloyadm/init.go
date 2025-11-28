package haloyadm

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/haloydev/haloy/internal/apiclient"
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

func InitCmd() *cobra.Command {
	var skipServices bool
	var override bool
	var apiDomain string
	var acmeEmail string
	var devMode bool
	var debug bool
	var noLogs bool
	var localInstall bool
	var remoteInstall bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Haloy data directory and start core services",
		Long: fmt.Sprintf(
			`Initialize Haloy by creating the data directory structure and starting core services.

Installation modes:
  Default (system): Uses system directories (/etc/haloy, /var/lib/haloy) when running as root
  --local-install:  Forces user directories (~/.config/haloy, ~/.local/share/haloy)

This command will:
- Create the data directory (default: /var/lib/haloy)
- Create the config directory for haloyd (default: /etc/haloy)
- Create the Docker network for Haloy services
- Start HAProxy and haloyd containers (unless --no-services is used)

The data directory can be customized by setting the %s environment variable.`,
			constants.EnvVarDataDir,
		),
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

			// If we encounter an error, we will clean up any directories created so far.
			defer func() {
				if cleanupOnFailure && len(createdDirs) > 0 {
					cleanupDirectories(createdDirs)
				}
			}()

			// Check if Docker is installed and available in PATH.
			if _, err := exec.LookPath("docker"); err != nil {
				return fmt.Errorf("docker executable not found: %w\nPlease ensure Docker is installed and in your PATH.\nDownload from: https://www.docker.com/get-started", err)
			}

			dataDir, err := config.DataDir()
			if err != nil {
				return fmt.Errorf("failed to determine data directory: %w", err)
			}

			configDir, err := config.ConfigDir()
			if err != nil {
				return fmt.Errorf("failed to determine haloyd config directory: %w", err)
			}

			if err := validateAndPrepareDirectory(configDir, "Haloyd Config", override); err != nil {
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

			// Use createdDirs for cleanup if later steps fail
			if err := createConfigFiles(apiToken, apiDomain, acmeEmail, configDir); err != nil {
				return fmt.Errorf("failed to create config files: %w", err)
			}

			emptyDirs := []string{
				filepath.Base(constants.HAProxyConfigDir),
				filepath.Base(constants.DBDir),
			}
			if err := copyDataFiles(dataDir, emptyDirs); err != nil {
				return fmt.Errorf("failed to create configuration files: %w", err)
			}

			// Ensure default Docker network exists.
			if err := ensureNetwork(ctx); err != nil {
				ui.Info("You can manually create it with:")
				ui.Info("docker network create --driver bridge --attachable %s", constants.DockerNetwork)
				return fmt.Errorf("failed to ensure Docker network exists: %w", err)
			}

			successMsg := "Haloy initialized successfully!\n\n"
			successMsg += fmt.Sprintf("Data directory: %s\n", dataDir)
			successMsg += fmt.Sprintf("Config directory: %s\n", configDir)
			if apiDomain != "" {
				successMsg += fmt.Sprintf("API domain: %s\n", apiDomain)
			}
			ui.Success("%s", successMsg)

			cleanupOnFailure = false

			// Start the haloyd container and haproxy container, stream logs if requested.
			if !skipServices {
				ui.Info("Starting Haloy services...")
				if err := startServices(ctx, dataDir, configDir, devMode, override, debug); err != nil {
					return err
				}

				if !noLogs {
					apiURL := fmt.Sprintf("http://localhost:%s", constants.APIServerPort)
					api, err := apiclient.New(apiURL, apiToken)
					if err != nil {
						return fmt.Errorf("failed to create API client: %w", err)
					}
					ui.Info("Waiting for haloyd API to become available...")
					waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Second)
					defer waitCancel()
					if err := waitForAPI(waitCtx, api); err != nil {
						return fmt.Errorf("Haloyd API not available: %w", err)
					}

					ui.Info("Streaming haloyd initialization logs...")
					if err := streamHaloydInitLogs(ctx, api); err != nil {
						ui.Warn("Failed to stream haloyd initialization logs: %v", err)
						ui.Info("haloyd is starting in the background. Check logs with: docker logs haloyd")
					}
				}
			}

			// If remote install this is taken care of automatically
			if !remoteInstall {
				apiDomainMessage := "<server-url>"
				if apiDomain != "" {
					apiDomainMessage = apiDomain
				}
				ui.Info("You can now add this server to the haloy cli with:")
				ui.Info(" haloy server add %s %s", apiDomainMessage, apiToken)

			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&skipServices, "no-services", false, "Skip starting HAProxy and haloyd containers")
	cmd.Flags().BoolVar(&override, "override", false, "Remove and recreate existing data directory. Any existing haloyd or haproxy containers will be restarted.")
	cmd.Flags().StringVar(&apiDomain, "api-domain", "", "Domain for the haloyd API (e.g., api.yourserver.com)")
	cmd.Flags().StringVar(&acmeEmail, "acme-email", "", "Email address for Let's Encrypt certificate registration")
	cmd.Flags().BoolVar(&devMode, "dev", false, "Start in development mode using the local haloyd image")
	cmd.Flags().BoolVar(&debug, "debug", false, "Enable debug mode")
	cmd.Flags().BoolVar(&noLogs, "no-logs", false, "Don't stream haloyd initialization logs")
	cmd.Flags().BoolVar(&localInstall, "local-install", false, "Install in user directories instead of system directories")
	cmd.Flags().BoolVar(&remoteInstall, "remote-install", false, "Indicates that this is a remote install, usually used internally")

	return cmd
}

func copyDataFiles(dataDir string, emptyDirs []string) error {
	// First create empty directories
	for _, dir := range emptyDirs {
		dirPath := filepath.Join(dataDir, dir)
		if err := os.MkdirAll(dirPath, constants.ModeDirPrivate); err != nil {
			return fmt.Errorf("failed to create empty directory %s: %w", dirPath, err)
		}
	}

	// Copy static files from embedded filesystem to data directory.
	err := fs.WalkDir(embed.DataFS, "data", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("error walking embedded filesystem: %w", err)
		}

		// Compute the relative path based on the data directory.
		relPath, err := filepath.Rel("data", path)
		if err != nil {
			return fmt.Errorf("failed to determine relative path: %w", err)
		}

		targetPath := filepath.Join(dataDir, relPath)
		if d.IsDir() {
			if err := os.MkdirAll(targetPath, constants.ModeDirPrivate); err != nil {
				return err
			}
			if err := os.Chmod(targetPath, constants.ModeDirPrivate); err != nil {
				ui.Warn("failed to chmod %s: %v", targetPath, err)
			}
			return nil // continue walking; children will be visited next
		}

		// Skip template files - they'll be handled by copyConfigTemplateFiles
		if strings.Contains(path, "template") {
			return nil
		}

		data, err := embed.DataFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read embedded file %s: %w", path, err)
		}

		// Determine the file mode - make shell scripts executable
		fileMode := constants.ModeFileDefault
		if filepath.Ext(targetPath) == ".sh" {
			fileMode = constants.ModeFileExec
		}

		if err := os.WriteFile(targetPath, data, fileMode); err != nil {
			return fmt.Errorf("failed to write file %s: %w", targetPath, err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Now handle template files
	if err := copyConfigTemplateFiles(dataDir); err != nil {
		return fmt.Errorf("failed to copy template files: %w", err)
	}

	return nil
}

func copyConfigTemplateFiles(dataDir string) error {
	haproxyConfigTemplateData := embed.HAProxyTemplateData{
		HTTPFrontend:            "",
		HTTPSFrontend:           "",
		HTTPSFrontendUseBackend: "",
		Backends:                "",
	}

	haproxyConfigFile, err := renderTemplate(fmt.Sprintf("templates/%s", constants.HAProxyConfigFileName), haproxyConfigTemplateData)
	if err != nil {
		return fmt.Errorf("failed to build HAProxy template: %w", err)
	}

	haproxyConfigFilePath := filepath.Join(dataDir, constants.HAProxyConfigDir, constants.HAProxyConfigFileName)

	if err := os.WriteFile(haproxyConfigFilePath, haproxyConfigFile.Bytes(), constants.ModeFileDefault); err != nil {
		return fmt.Errorf("failed to write updated haproxy config file: %w", err)
	}

	return nil
}

func renderTemplate(templateFilePath string, templateData any) (bytes.Buffer, error) {
	var buf bytes.Buffer
	file, err := embed.TemplatesFS.ReadFile(templateFilePath)
	if err != nil {
		return buf, fmt.Errorf("failed to read embedded file: %w", err)
	}

	tmpl, err := template.New(templateFilePath).Parse(string(file))
	if err != nil {
		return buf, fmt.Errorf("failed to parse template: %w", err)
	}

	if err := tmpl.Execute(&buf, templateData); err != nil {
		return buf, fmt.Errorf("failed to execute template: %w", err)
	}
	return buf, nil
}

// createConfigFiles creates a .env file with the API token in the data directory
func createConfigFiles(apiToken, domain, acmeEmail, configDir string) error {
	if apiToken == "" {
		return fmt.Errorf("apiToken cannot be empty")
	}

	if configDir == "" {
		return fmt.Errorf("configDir cannot be empty")
	}
	envPath := filepath.Join(configDir, constants.ConfigEnvFileName)
	env := map[string]string{
		constants.EnvVarAPIToken: apiToken,
	}
	if err := godotenv.Write(env, envPath); err != nil {
		return fmt.Errorf("failed to write %s content: %w", constants.ConfigEnvFileName, err)
	}

	if err := os.Chmod(envPath, constants.ModeFileSecret); err != nil {
		return fmt.Errorf("failed to set %s file permissions: %w", constants.ConfigEnvFileName, err)
	}

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

func validateAndPrepareDirectory(dirPath, dirType string, overrideExisting bool) error {
	fileInfo, statErr := os.Stat(dirPath)
	if statErr == nil {
		if !fileInfo.IsDir() {
			return fmt.Errorf("%s directory path exists but is a file, not a directory: %s",
				strings.ToLower(dirType), dirPath)
		}
		if !overrideExisting {
			return fmt.Errorf("%s directory already exists: %s\nUse --override to overwrite",
				strings.ToLower(dirType), dirPath)
		}
		ui.Info("Removing existing %s directory: %s\n", strings.ToLower(dirType), dirPath)
		if err := os.RemoveAll(dirPath); err != nil {
			return fmt.Errorf("failed to remove existing %s directory %s: %w",
				strings.ToLower(dirType), dirPath, err)
		}
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("failed to access %s directory %s: %w",
			strings.ToLower(dirType), dirPath, statErr)
	}

	if err := os.MkdirAll(dirPath, constants.ModeDirPrivate); err != nil {
		return fmt.Errorf("failed to create %s directory %s: %w",
			strings.ToLower(dirType), dirPath, err)
	}
	return nil
}

func cleanupDirectories(dirs []string) {
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			ui.Warn("Failed to cleanup directory %s: %v", dir, err)
		}
	}
}
