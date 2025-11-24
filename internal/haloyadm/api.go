package haloyadm

import (
	"context"
	"errors"
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
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if err := checkDirectoryAccess(RequiredAccess{
				Config: true,
				Data:   true,
			}); err != nil {
				return err
			}

			url := args[0]
			email := args[1]

			if url == "" {
				return errors.New("domain URL cannot be empty")
			}

			if email == "" {
				return errors.New("email cannot be empty")
			}

			normalizedURL, err := helpers.NormalizeServerURL(url)
			if err != nil {
				return fmt.Errorf("invalid domain URL: %w", err)
			}

			if err := helpers.IsValidDomain(normalizedURL); err != nil {
				return fmt.Errorf("invalid domain URL: %w", err)
			}

			if !helpers.IsValidEmail(email) {
				return fmt.Errorf("invalid email format: %s", email)
			}

			configDir, err := config.ConfigDir()
			if err != nil {
				return fmt.Errorf("failed to determine config directory: %w", err)
			}

			haloydConfigPath := filepath.Join(configDir, constants.HaloydConfigFileName)
			haloydConfig, err := config.LoadHaloydConfig(haloydConfigPath)
			if err != nil {
				return fmt.Errorf("failed to load haloyd configuration: %w", err)
			}

			if haloydConfig == nil {
				haloydConfig = &config.HaloydConfig{}
			}

			// Set the API domain and email in the haloyd configuration
			haloydConfig.API.Domain = normalizedURL
			haloydConfig.Certificates.AcmeEmail = email

			// Save the updated haloyd configuration
			if err := config.SaveHaloydConfig(haloydConfig, haloydConfigPath); err != nil {
				return fmt.Errorf("failed to save haloyd configuration: %w", err)
			}

			ui.Info("Updated configuration:")
			ui.Info("  Domain: %s", normalizedURL)
			ui.Info("  Email: %s", email)
			ui.Info("Restarting haloyd...")

			dataDir, err := config.DataDir()
			if err != nil {
				return fmt.Errorf("failed to determine data directory: %w", err)
			}

			haloydExists, err := containerExists(ctx, config.HaloydLabelRole)
			if err != nil {
				return errors.New("failed to determine if haloyd is already running, check out the logs with docker logs haloyd")
			}

			if haloydExists {
				if err := stopContainer(ctx, config.HaloydLabelRole); err != nil {
					ui.Warn("failed to stop existing haloyd: %s", err)
				}
			}

			if err := startHaloyd(ctx, dataDir, configDir, devMode, debug); err != nil {
				return err
			}

			apiToken := os.Getenv(constants.EnvVarAPIToken)
			if apiToken == "" {
				return errors.New("failed to get API token")
			}
			apiURL := fmt.Sprintf("http://localhost:%s", constants.APIServerPort)
			api, err := apiclient.New(apiURL, apiToken)
			if err != nil {
				return fmt.Errorf("failed to create API client: %w", err)
			}
			if err := streamHaloydInitLogs(ctx, api); err != nil {
				ui.Warn("Failed to stream haloyd initialization logs: %v", err)
				ui.Info("haloyd is starting in the background. Check logs with: docker logs haloyd")
			}

			ui.Success("API domain and email set successfully")
			return nil
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
				return fmt.Errorf("failed to determine config directory: %w", err)
			}

			envFile := filepath.Join(configDir, constants.ConfigEnvFileName)
			env, err := godotenv.Read(envFile)
			if err != nil {
				return fmt.Errorf("failed to read environment variables from %s: %w", envFile, err)
			}

			token, exists := env[constants.EnvVarAPIToken]
			if !exists || token == "" {
				return fmt.Errorf("API token not found in %s", envFile)
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
				return fmt.Errorf("failed to determine config directory: %w", err)
			}

			configFilePath := filepath.Join(configDir, constants.HaloydConfigFileName)
			haloydConfig, err := config.LoadHaloydConfig(configFilePath)
			if err != nil {
				return fmt.Errorf("failed to load configuration file: %w", err)
			}

			if haloydConfig == nil || haloydConfig.API.Domain == "" {
				return fmt.Errorf("API URL not found in %s", configFilePath)
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
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), newTokenTimeout)
			defer cancel()

			token, err := generateAPIToken()
			if err != nil {
				return fmt.Errorf("failed to generate API token: %w", err)
			}
			dataDir, err := config.DataDir()
			if err != nil {
				return fmt.Errorf("failed to determine data directory: %w", err)
			}
			configDir, err := config.ConfigDir()
			if err != nil {
				return fmt.Errorf("failed to determine config directory: %w", err)
			}

			envFile := filepath.Join(configDir, constants.ConfigEnvFileName)
			env, err := godotenv.Read(envFile)
			if err != nil {
				return fmt.Errorf("failed to read environment variables from %s: %w", envFile, err)
			}
			env[constants.EnvVarAPIToken] = token
			if err := godotenv.Write(env, envFile); err != nil {
				return fmt.Errorf("failed to write environment variables to %s: %w", envFile, err)
			}

			// Restart haloyd
			if err := stopContainer(ctx, config.HaloydLabelRole); err != nil {
				return fmt.Errorf("failed to stop haloyd container: %w", err)
			}
			if err := startHaloyd(ctx, dataDir, configDir, devMode, debug); err != nil {
				return fmt.Errorf("failed to restart haloyd: %w", err)
			}

			ui.Success("Generated new API token and restarted haloyd")
			ui.Info("New API token: %s\n", token)
			return nil
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
