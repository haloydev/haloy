package haloy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/haloydev/haloy/internal/apiclient"
	"github.com/haloydev/haloy/internal/apitypes"
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
	cmd.AddCommand(ServerRegistryCmd(configPath, flags))
	cmd.AddCommand(ServerLogsCmd(configPath, flags))
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
		RunE: func(cmd *cobra.Command, args []string) error {
			url := args[0]
			token := strings.Join(args[1:], " ")
			return addServerURL(url, token, force)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force overwrite if server already exists")

	return cmd
}

func addServerURL(url, token string, force bool) error {
	if url == "" {
		return errors.New("URL is required")
	}

	if token == "" {
		return errors.New("token is required")
	}

	normalizedURL, err := helpers.NormalizeServerURL(url)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if err := helpers.IsValidDomain(normalizedURL); err != nil {
		return fmt.Errorf("invalid domain: %w", err)
	}

	configDir, err := config.HaloyConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config dir: %w", err)
	}

	if err = helpers.EnsureDir(configDir); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	envFile := filepath.Join(configDir, constants.ConfigEnvFileName)

	tokenEnv := generateTokenEnvName(normalizedURL)

	env, err := godotenv.Read(envFile)
	if err != nil {
		if os.IsNotExist(err) {
			env = make(map[string]string)
		} else {
			return fmt.Errorf("failed to read env file: %w", err)
		}
	}
	env[tokenEnv] = token
	if err := godotenv.Write(env, envFile); err != nil {
		return fmt.Errorf("failed to write env file: %w", err)
	}

	clientConfigPath := filepath.Join(configDir, constants.ClientConfigFileName)
	clientConfig, err := config.LoadClientConfig(clientConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load client config: %w", err)
	}

	if clientConfig == nil {
		clientConfig = &config.ClientConfig{}
	}

	if err := clientConfig.AddServer(normalizedURL, tokenEnv, force); err != nil {
		return fmt.Errorf("failed to add server: %w", err)
	}

	if err := config.SaveClientConfig(clientConfig, clientConfigPath); err != nil {
		return fmt.Errorf("failed to save client config: %w", err)
	}

	ui.Success("Server %s added successfully", normalizedURL)
	ui.Info("API token stored as: %s", tokenEnv)

	return nil
}

func generateTokenEnvName(url string) string {
	return fmt.Sprintf("%s_%s", constants.EnvVarAPIToken, strings.ToUpper(helpers.SanitizeString(url)))
}

func ServerDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <url>",
		Short: "Delete a Haloy server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			url := args[0]

			if url == "" {
				return errors.New("URL is required")
			}

			normalizedURL, err := helpers.NormalizeServerURL(url)
			if err != nil {
				return fmt.Errorf("invalid URL: %w", err)
			}

			configDir, err := config.HaloyConfigDir()
			if err != nil {
				return fmt.Errorf("failed to get config dir: %w", err)
			}

			clientConfigPath := filepath.Join(configDir, constants.ClientConfigFileName)
			clientConfig, err := config.LoadClientConfig(clientConfigPath)
			if err != nil {
				return fmt.Errorf("failed to load client config: %w", err)
			}

			if clientConfig == nil {
				return fmt.Errorf("no config file found in %s", clientConfigPath)
			}

			if len(clientConfig.Servers) == 0 {
				return errors.New("no servers found in client config")
			}

			serverConfig, exists := clientConfig.Servers[normalizedURL]
			if !exists {
				return fmt.Errorf("server %s not found in config", normalizedURL)
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
				return fmt.Errorf("failed to delete server: %w", err)
			}

			if err := config.SaveClientConfig(clientConfig, clientConfigPath); err != nil {
				return fmt.Errorf("failed to save client config: %w", err)
			}

			ui.Success("Server %s deleted successfully", normalizedURL)

			return nil
		},
	}
	return cmd
}

func ServerListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all Haloy servers",
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir, err := config.HaloyConfigDir()
			if err != nil {
				return fmt.Errorf("failed to get config dir: %w", err)
			}

			clientConfigPath := filepath.Join(configDir, constants.ClientConfigFileName)
			clientConfig, err := config.LoadClientConfig(clientConfigPath)
			if err != nil {
				return fmt.Errorf("failed to load client config: %w", err)
			}

			if clientConfig == nil {
				return fmt.Errorf("no config file found in %s", clientConfigPath)
			}

			servers := clientConfig.Servers
			if len(servers) == 0 {
				return errors.New("no Haloy servers found")
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

			return nil
		},
	}
	return cmd
}

func ServerVersionCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var serverFlag string
	var components bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Check server version",
		Long:  "Check the Haloy product version and proxy compatibility on a specific server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if serverFlag != "" {
				version, err := getServerVersion(ctx, nil, serverFlag, "")
				if err != nil {
					return err
				}
				printServerVersion(version, "", components)
				return nil
			}

			servers, err := resolveServerTargets(ctx, cmd, *configPath, flags)
			if err != nil {
				return err
			}

			g, ctx := errgroup.WithContext(ctx)
			for _, serverTarget := range servers {
				g.Go(func() error {
					prefix := ""
					if len(servers) > 1 {
						prefix = serverTarget.Server
					}
					version, err := getServerVersion(ctx, serverTarget.TargetConfig, serverTarget.Server, prefix)
					if err != nil {
						return err
					}

					if version.Version != constants.Version {
						ui.Warn("haloy version %s does not match haloyd (server) version %s", constants.Version, version.Version)
					}
					printServerVersion(version, prefix, components)
					return nil
				})
			}

			return g.Wait()
		},
	}
	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Server URL (overrides config file)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Get version for specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Get version for all targets")
	cmd.Flags().BoolVar(&components, "components", false, "Show component build and compatibility metadata")

	cmd.RegisterFlagCompletionFunc("targets", completeTargetNames)

	return cmd
}

func printServerVersion(version *apitypes.VersionResponse, prefix string, components bool) {
	pui := &ui.PrefixedUI{Prefix: prefix}
	pui.Info("Haloy server version: %s", version.Version)

	switch {
	case version.ProxyCompatible == nil && version.ProxyVersion == "":
		pui.Warn("haloy-proxy: unavailable")
	case version.ProxyCompatible == nil:
		pui.Info("haloy-proxy: connected (compatibility metadata unavailable)")
	case *version.ProxyCompatible:
		pui.Info("haloy-proxy: compatible")
	default:
		pui.Warn("haloy-proxy: incompatible; run the server upgrade script")
	}

	if !components {
		return
	}

	pui.Info("haloyd build version: %s", version.Version)
	if version.ProxyVersion != "" {
		pui.Info("haloy-proxy build version: %s", version.ProxyVersion)
	}
	if version.ProxyGeneration != 0 || version.RequiredProxyGeneration != 0 {
		pui.Info("haloy-proxy generation: %d (required: %d)", version.ProxyGeneration, version.RequiredProxyGeneration)
	}
	if version.ProxySchemaVersion != 0 || version.RequiredProxySchemaVersion != 0 {
		pui.Info("haloy-proxy schema: %d (required: %d)", version.ProxySchemaVersion, version.RequiredProxySchemaVersion)
	}
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
