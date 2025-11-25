package haloy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/apitypes"
	"github.com/haloydev/haloy/internal/appconfigloader"
	"github.com/haloydev/haloy/internal/config"
	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/helpers"
	"github.com/haloydev/haloy/internal/ui"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func ServerCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage Haloy servers",
		Long:  "Add, remove, and manage connections to Haloy servers",
	}

	cmd.AddCommand(ServerAddCmd())
	cmd.AddCommand(ServerDeleteCmd())
	cmd.AddCommand(ServerListCmd())
	cmd.AddCommand(ServerVersionCmd(configPath, flags))

	return cmd
}

func ServerAddCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "add <url> <token>",
		Short: "Add a new Haloy server",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				ui.Error("Error: You must provide a <url> and a <token> to add a server.\n")
				ui.Info("%s", cmd.UsageString())
				return fmt.Errorf("requires at least 2 arg(s), only received %d", len(args))
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			url := args[0]
			token := strings.Join(args[1:], " ")

			if url == "" {
				ui.Error("URL is required")
				return
			}

			if token == "" {
				ui.Error("Token is required")
				return
			}

			normalizedURL, err := helpers.NormalizeServerURL(url)
			if err != nil {
				ui.Error("Invalid URL: %v", err)
				return
			}

			if err := helpers.IsValidDomain(normalizedURL); err != nil {
				ui.Error("Invalid domain: %v", err)
				return
			}

			configDir, err := config.ConfigDir()
			if err != nil {
				ui.Error("Failed to get config dir: %v", err)
				return
			}

			if err = helpers.EnsureDir(configDir); err != nil {
				ui.Error("Failed to create config dir: %v", err)
				return
			}

			envFile := filepath.Join(configDir, constants.ConfigEnvFileName)

			tokenEnv := generateTokenEnvName(normalizedURL)

			env, err := godotenv.Read(envFile)
			if err != nil {
				if os.IsNotExist(err) {
					// Create empty map if file doesn't exist
					env = make(map[string]string)
				} else {
					ui.Error("Failed to read env file: %v", err)
					return
				}
			}
			env[tokenEnv] = token
			if err := godotenv.Write(env, envFile); err != nil {
				ui.Error("Failed to write env file: %v", err)
				return
			}

			clientConfigPath := filepath.Join(configDir, constants.ClientConfigFileName)
			clientConfig, err := config.LoadClientConfig(clientConfigPath)
			if err != nil {
				ui.Error("Failed to load client config: %v", err)
				return
			}

			if clientConfig == nil {
				clientConfig = &config.ClientConfig{}
			}

			if err := clientConfig.AddServer(normalizedURL, tokenEnv, force); err != nil {
				ui.Error("Failed to add server: %v", err)
				return
			}

			if err := config.SaveClientConfig(clientConfig, clientConfigPath); err != nil {
				ui.Error("Failed to save client config: %v", err)
				return
			}

			ui.Success("Server %s added successfully", normalizedURL)
			ui.Info("API token stored as: %s", tokenEnv)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force overwrite if server already exists")

	return cmd
}

func generateTokenEnvName(url string) string {
	return fmt.Sprintf("HALOY_API_TOKEN_%s", strings.ToUpper(helpers.SanitizeString(url)))
}

func ServerDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <url>",
		Short: "Delete a Haloy server",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			url := args[0]

			if url == "" {
				ui.Error("URL is required")
				return
			}

			normalizedURL, err := helpers.NormalizeServerURL(url)
			if err != nil {
				ui.Error("Invalid URL: %v", err)
				return
			}

			configDir, err := config.ConfigDir()
			if err != nil {
				ui.Error("Failed to get config dir: %v", err)
				return
			}

			clientConfigPath := filepath.Join(configDir, constants.ClientConfigFileName)
			clientConfig, err := config.LoadClientConfig(clientConfigPath)
			if err != nil {
				ui.Error("Failed to load client config: %v", err)
				return
			}

			if clientConfig == nil {
				ui.Error("No config file found in %s", clientConfigPath)
				return
			}

			if len(clientConfig.Servers) == 0 {
				ui.Error("No servers found in client config")
				return
			}

			serverConfig, exists := clientConfig.Servers[normalizedURL]
			if !exists {
				ui.Error("Server %s not found in config", normalizedURL)
				return
			}

			envFile := filepath.Join(configDir, constants.ConfigEnvFileName)
			env, _ := godotenv.Read(envFile)
			if _, exists := env[serverConfig.TokenEnv]; exists {
				delete(env, serverConfig.TokenEnv)
				if err := godotenv.Write(env, envFile); err != nil {
					ui.Warn("Failed to write env file: %v", err)
					ui.Info("Please remove the token %s from %s manually", serverConfig.TokenEnv, envFile)
				}
			}

			if err := clientConfig.DeleteServer(normalizedURL); err != nil {
				ui.Error("Failed to delete server: %v", err)
				return
			}

			if err := config.SaveClientConfig(clientConfig, clientConfigPath); err != nil {
				ui.Error("Failed to save client config: %v", err)
				return
			}

			ui.Success("Server %s deleted successfully", normalizedURL)
		},
	}
	return cmd
}

func ServerListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all Haloy servers",
		Run: func(cmd *cobra.Command, args []string) {
			configDir, err := config.ConfigDir()
			if err != nil {
				ui.Error("Failed to get config dir: %v", err)
				return
			}

			clientConfigPath := filepath.Join(configDir, constants.ClientConfigFileName)
			clientConfig, err := config.LoadClientConfig(clientConfigPath)
			if err != nil {
				ui.Error("Failed to load client config: %v", err)
				return
			}

			if clientConfig == nil {
				ui.Error("No config file found in %s", clientConfigPath)
				return
			}

			servers := clientConfig.Servers
			if len(servers) == 0 {
				ui.Info("No Haloy servers found")
				return
			}

			ui.Info("List of servers:")
			headers := []string{"URL", "ENV VAR", "ENV VAR EXISTS"}
			rows := make([][]string, 0, len(servers))
			for url, config := range servers {
				tokenExists := "⚠️ no"
				token := os.Getenv(config.TokenEnv)
				if token != "" {
					tokenExists = "✅ yes"
				}
				rows = append(rows, []string{url, config.TokenEnv, tokenExists})
			}

			ui.Table(headers, rows)
		},
	}
	return cmd
}

func ServerVersionCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var serverFlag string

	cmd := &cobra.Command{
		Use:   "version <url>",
		Short: "Check server version",
		Long:  "Check the haloyd and HAProxy version running on a specific server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if serverFlag != "" {
				version, err := getServerVersion(ctx, nil, serverFlag, "")
				if err != nil {
					return err
				}
				ui.Info("haloyd version: %s", version.Version)
				return nil
			}

			rawAppConfig, format, err := appconfigloader.Load(ctx, *configPath, flags.targets, flags.all)
			if err != nil {
				return fmt.Errorf("unable to load config: %w", err)
			}

			targets, err := appconfigloader.ExtractTargets(rawAppConfig, format)
			if err != nil {
				return err
			}

			g, ctx := errgroup.WithContext(ctx)
			for _, target := range targets {
				g.Go(func() error {
					prefix := ""
					if len(targets) > 1 {
						prefix = target.TargetName
					}
					version, err := getServerVersion(ctx, &target, target.Server, prefix)
					if err != nil {
						return err
					}

					if version.Version != constants.Version {
						ui.Warn("haloy version %s does not match haloyd (server) version %s", constants.Version, version.Version)
					}
					ui.Info("haloyd version: %s", version.Version)
					return nil
				})
			}

			return g.Wait()
		},
	}
	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Get version for specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Get version for all targets")
	return cmd
}

func getServerVersion(ctx context.Context, targetConfig *config.TargetConfig, targetServer, prefix string) (*apitypes.VersionResponse, error) {
	token, err := getToken(targetConfig, targetServer)
	if err != nil {
		return nil, &PrefixedError{Err: fmt.Errorf("unable to get token: %w", err), Prefix: prefix}
	}

	api, err := apiclient.New(targetServer, token)
	if err != nil {
		return nil, &PrefixedError{Err: fmt.Errorf("unable to create API client: %w", err), Prefix: prefix}
	}

	var response apitypes.VersionResponse
	if err := api.Get(ctx, "version", &response); err != nil {
		return nil, &PrefixedError{Err: fmt.Errorf("failed to get version from API: %w", err), Prefix: prefix}
	}
	return &response, nil
}
