package haloyadm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

func APIDomainCmd() *cobra.Command {
	var devMode bool
	var debug bool

	cmd := &cobra.Command{
		Use:   "domain <url> <email>",
		Short: "Set the API domain",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("not enough arguments: expected 2 (domain URL and email), got %d\n\nUsage:\n  %s\n", len(args), cmd.UseLine())
			}
			if len(args) > 2 {
				return fmt.Errorf("too many arguments: expected 2 (domain URL and email), got %d\n\nUsage:\n  %s\n", len(args), cmd.UseLine())
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()

			if err := checkDirectoryAccess(RequiredAccess{
				Config: true,
				Data:   true,
			}); err != nil {
				ui.Error("%s", err)
				return
			}

			url := args[0]
			email := args[1]

			if url == "" {
				ui.Error("Domain URL cannot be empty")
				return
			}

			if email == "" {
				ui.Error("Email cannot be empty")
				return
			}

			normalizedURL, err := helpers.NormalizeServerURL(url)
			if err != nil {
				ui.Error("Invalid domain URL: %v", err)
				return
			}

			if err := helpers.IsValidDomain(normalizedURL); err != nil {
				ui.Error("Invalid domain URL: %v", err)
				return
			}

			if !helpers.IsValidEmail(email) {
				ui.Error("Invalid email format: %s", email)
				return
			}

			configDir, err := config.ConfigDir()
			if err != nil {
				ui.Error("Failed to determine config directory: %v\n", err)
				return
			}

			haloydConfigPath := filepath.Join(configDir, constants.HaloydConfigFileName)
			haloydConfig, err := config.LoadHaloydConfig(haloydConfigPath)
			if err != nil {
				ui.Error("Failed to load haloyd configuration: %v\n", err)
				return
			}

			if haloydConfig == nil {
				haloydConfig = &config.HaloydConfig{}
			}

			// Set the API domain and email in the haloyd configuration
			haloydConfig.API.Domain = normalizedURL
			haloydConfig.Certificates.AcmeEmail = email

			// Save the updated haloyd configuration
			if err := config.SaveHaloydConfig(haloydConfig, haloydConfigPath); err != nil {
				ui.Error("Failed to save haloyd configuration: %v\n", err)
				return
			}

			ui.Info("Updated configuration:")
			ui.Info("  Domain: %s", normalizedURL)
			ui.Info("  Email: %s", email)
			ui.Info("Restarting haloyd...")

			dataDir, err := config.DataDir()
			if err != nil {
				ui.Error("Failed to determine data directory: %v\n", err)
				return
			}

			haloydExists, err := containerExists(ctx, config.HaloydLabelRole)
			if err != nil {
				ui.Error("Failed to determine if haloyd is already running, check out the logs with docker logs haloyd")
				return
			}

			if haloydExists {
				if err := stopContainer(ctx, config.HaloydLabelRole); err != nil {
					ui.Error("failed to stop existing haloyd: %s", err)
				}
			}

			if err := startHaloyd(ctx, dataDir, configDir, devMode, debug); err != nil {
				ui.Error("%s", err)
				return
			}

			apiToken := os.Getenv(constants.EnvVarAPIToken)
			if apiToken == "" {
				ui.Error("Failed to get API token")
				return
			}
			apiURL := fmt.Sprintf("http://localhost:%s", constants.APIServerPort)
			api, err := apiclient.New(apiURL, apiToken)
			if err != nil {
				ui.Error("Failed to create API client: %v", err)
				return
			}
			if err := streamHaloydInitLogs(ctx, api); err != nil {
				ui.Warn("Failed to stream haloyd initialization logs: %v", err)
				ui.Info("haloyd is starting in the background. Check logs with: docker logs haloyd")
			}

			ui.Success("API domain and email set successfully")
		},
	}

	cmd.Flags().BoolVar(&devMode, "dev", false, "Start in development mode using the local haloyd image")
	cmd.Flags().BoolVar(&debug, "debug", false, "Enable debug mode")

	return cmd
}

func APITokenCmd() *cobra.Command {
	var raw bool
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Reveal API token",
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir, err := config.ConfigDir()
			if err != nil {
				if raw {
					fmt.Fprintln(os.Stderr, err)
				} else {
					ui.Error("Failed to determine config directory: %v\n", err)
				}
				return err
			}

			envFile := filepath.Join(configDir, constants.ConfigEnvFileName)
			env, err := godotenv.Read(envFile)
			if err != nil {
				if raw {
					fmt.Fprintln(os.Stderr, err)
				} else {
					ui.Error("Failed to read environment variables from %s: %v", envFile, err)
				}
				return err
			}

			token, exists := env[constants.EnvVarAPIToken]
			if !exists || token == "" {
				err := fmt.Errorf("API token not found in %s", envFile)
				if raw {
					fmt.Fprintln(os.Stderr, err)
				} else {
					ui.Error("API token not found in %s", envFile)
				}
				return err
			}

			if raw {
				fmt.Print(token)
			} else {
				ui.Info("API token: %s\n", token)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "Output only the token value")
	return cmd
}

func APIURLCmd() *cobra.Command {
	var raw bool
	cmd := &cobra.Command{
		Use:   "url",
		Short: "Show API URL",
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir, err := config.ConfigDir()
			if err != nil {
				if raw {
					fmt.Fprintln(os.Stderr, err)
				} else {
					ui.Error("Failed to determine config directory: %v\n", err)
				}
				return err
			}

			configFilePath := filepath.Join(configDir, constants.HaloydConfigFileName)
			haloydConfig, err := config.LoadHaloydConfig(configFilePath)
			if err != nil {
				if raw {
					fmt.Fprintln(os.Stderr, err)
				} else {
					ui.Error("Failed to load configuration file: %v", err)
				}
				return err
			}

			if haloydConfig == nil || haloydConfig.API.Domain == "" {
				err := fmt.Errorf("API URL not found")
				if raw {
					fmt.Fprintln(os.Stderr, err)
				} else {
					ui.Error("API URL not found in %s", configFilePath)
				}
				return err
			}

			if raw {
				fmt.Print(haloydConfig.API.Domain)
			} else {
				ui.Info("API URL: %s\n", haloydConfig.API.Domain)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "Output only the URL value")
	return cmd
}

const (
	newTokenTimeout = 1 * time.Minute
)

func APINewTokenCmd() *cobra.Command {
	var devMode bool
	var debug bool
	cmd := &cobra.Command{
		Use:   "generate-token",
		Short: "Generate a new API token and restart the haloyd",
		Run: func(cmd *cobra.Command, args []string) {
			ctx, cancel := context.WithTimeout(context.Background(), newTokenTimeout)
			defer cancel()

			token, err := generateAPIToken()
			if err != nil {
				ui.Error("Failed to generate API token: %v\n", err)
				return
			}
			dataDir, err := config.DataDir()
			if err != nil {
				ui.Error("Failed to determine data directory: %v\n", err)
				return
			}
			configDir, err := config.ConfigDir()
			if err != nil {
				ui.Error("Failed to determine config directory: %v\n", err)
				return
			}

			envFile := filepath.Join(configDir, constants.ConfigEnvFileName)
			env, err := godotenv.Read(envFile)
			if err != nil {
				ui.Error("Failed to read environment variables from %s: %v", envFile, err)
				return
			}
			env[constants.EnvVarAPIToken] = token
			if err := godotenv.Write(env, envFile); err != nil {
				ui.Error("Failed to write environment variables to %s: %v", envFile, err)
				return
			}

			// Restart haloyd
			if err := stopContainer(ctx, config.HaloydLabelRole); err != nil {
				ui.Error("Failed to stop haloyd container: %v", err)
				return
			}
			if err := startHaloyd(ctx, dataDir, configDir, devMode, debug); err != nil {
				ui.Error("Failed to restart haloyd: %v", err)
				return
			}

			ui.Success("Generated new API token and restarted haloyd")
			ui.Info("New API token: %s\n", token)
		},
	}
	cmd.Flags().BoolVar(&devMode, "dev", false, "Restart in development mode using the local haloyd image")
	cmd.Flags().BoolVar(&debug, "debug", false, "Restart haloyd in debug mode")
	return cmd
}

func APICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api",
		Short: "API related commands",
	}

	cmd.AddCommand(APIDomainCmd())
	cmd.AddCommand(APITokenCmd())
	cmd.AddCommand(APINewTokenCmd())
	cmd.AddCommand(APIURLCmd())

	return cmd
}
